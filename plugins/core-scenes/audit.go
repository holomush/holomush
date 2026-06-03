// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/pgnanos"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// defaultActorKind matches the host audit projection default. When the
// dispatcher's Row.Actor is nil (no actor set on the publisher's envelope),
// the audit row records "system".
//
// Pre-Phase-7 the plugin separately consumed App-Actor-Kind / App-Actor-ID /
// App-Codec / App-Schema-Version / App-Event-Type headers; with the wire
// reshape (INV-P7-1) those values now arrive on the AuditRow proto fields
// populated by the host dispatcher's buildAuditRow.
const defaultActorKind = "system"

// auditMaxPageSize mirrors the host-side cap (spec §5) so plugin-served
// QueryHistory responses never exceed the same per-call bound.
const auditMaxPageSize = 200

// auditDefaultPageSize is applied when the caller supplies PageSize <= 0.
// Matches the host history.DefaultPageSize.
const auditDefaultPageSize = 50

// directionForward / directionBackward mirror eventbus.Direction* to avoid a
// dependency on internal/eventbus from the plugin binary.
const (
	directionForward  = int32(1)
	directionBackward = int32(2)
)

// sceneAuditLogStore is the log-storage surface SceneAuditServer needs.
// Insert / queryLog signatures match *SceneAuditStore verbatim so the
// concrete store satisfies the interface without adapter shims.
type sceneAuditLogStore interface {
	Insert(
		ctx context.Context,
		id []byte,
		subject, eventType string,
		timestamp *timestamppb.Timestamp,
		actorKind string,
		actorID []byte,
		payload []byte,
		schemaVer int,
		codec string,
		dekRef *int64,
		dekVersion *int32,
	) error
	// InsertScenePose composes a scene_log INSERT (for scene_pose event
	// type) with maintained metadata UPDATEs in a single transaction,
	// per spec §9.4:
	//
	//   1. INSERT into scene_log (via insertSceneLogTx).
	//   2. UPDATE scenes SET total_pose_count = total_pose_count + 1
	//      WHERE id = sceneID; RETURNING total_pose_count.
	//   3. UPDATE scene_participants
	//      SET last_pose_at = <event-timestamp>, last_pose_seq = <new-total>
	//      WHERE scene_id = sceneID AND character_id = posedCharID.
	//
	// Either all three operations commit, or none do. Pins INV-SCENE-10
	// (transactional consistency).
	//
	// sceneID and posedCharID are caller-extracted from the audit
	// subject and actor_id; this method does no parsing.
	InsertScenePose(
		ctx context.Context,
		id []byte,
		subject, eventType string,
		timestamp *timestamppb.Timestamp,
		actorKind string,
		actorID []byte,
		payload []byte,
		schemaVer int,
		codec string,
		dekRef *int64,
		dekVersion *int32,
		sceneID string,
		posedCharID string,
	) error
	queryLog(
		ctx context.Context,
		subject string,
		after, before []byte,
		notBefore, notAfter *timestamppb.Timestamp,
		reverse bool,
		pageSize int,
	) ([]logRow, error)
}

// sceneMembershipLookup is the membership-check surface SceneAuditServer
// needs. *SceneStore (Task 8) satisfies this.
type sceneMembershipLookup interface {
	IsMember(ctx context.Context, sceneID, characterID string) (bool, error)
}

// SceneAuditServer implements PluginAuditService for core-scenes.
//
// AuditEvent is invoked by the host per-plugin audit consumer for every
// message the JetStream consumer delivers on events.*.scene.>. The
// implementation is purely an idempotent INSERT — the consumer's AckWait
// + MaxDeliver handle retry semantics.
//
// QueryHistory is invoked by the host's bus.QueryHistory when the
// OwnerMap routes a scene subject to this plugin. The plugin reads its
// own scene_log rows and streams them back via the same proto wire
// format used by the host events_audit table.
type SceneAuditServer struct {
	pluginv1.UnimplementedPluginAuditServiceServer
	store        sceneAuditLogStore    // queryLog only
	memberLookup sceneMembershipLookup // IsMember only
}

// SceneAuditStore wraps the pgx pool with audit-specific SQL helpers. Kept
// separate from SceneStore so the two domains (scene domain-state vs
// plugin-audit-log) don't share a pool accessor; the plugin builds one of
// each in Init.
type SceneAuditStore struct {
	pool *pgxpool.Pool
}

// NewSceneAuditStore constructs a SceneAuditStore. The pool must already be
// open and pointed at the plugin's schema (search_path=plugin_core_scenes).
func NewSceneAuditStore(pool *pgxpool.Pool) *SceneAuditStore {
	return &SceneAuditStore{pool: pool}
}

// Insert persists one audit row. Uses ON CONFLICT (id) DO NOTHING so
// redelivery is idempotent — the same Nats-Msg-Id delivered twice (on
// restart before the ack reached the server) becomes a no-op, and the
// caller still Acks.
//
// dekRef / dekVersion are nil for identity-codec rows and non-nil for
// AEAD-codec rows. The plugin stores the values opaquely (INV-EVENTBUS-25); the
// host owns interpretation.
func (s *SceneAuditStore) Insert(
	ctx context.Context,
	id []byte,
	subject, eventType string,
	timestamp *timestamppb.Timestamp,
	actorKind string,
	actorID []byte,
	payload []byte,
	schemaVer int,
	codec string,
	dekRef *int64,
	dekVersion *int32,
) error {
	if err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		_, err := s.insertSceneLogTx(ctx, tx, id, subject, eventType, timestamp,
			actorKind, actorID, payload, schemaVer, codec, dekRef, dekVersion)
		return err
	}); err != nil {
		return oops.Code("SCENE_AUDIT_INSERT_FAILED").Wrap(err)
	}
	return nil
}

// InsertScenePose runs the scene_log INSERT + scenes.total_pose_count
// increment + scene_participants.last_pose_at/last_pose_seq UPDATE in
// a single transaction. Per spec §9.4. Pins INV-SCENE-10 (transactional
// consistency): either all three rows commit, or none do.
//
// The event timestamp (not wall clock) is stamped onto
// scene_participants.last_pose_at — this matches JetStream redelivery
// semantics where the audit row may arrive minutes after the publish
// time.
//
// Actor-not-participant edge case: if posedCharID isn't currently a
// row in scene_participants (e.g. their participation was removed
// between event publish and audit consumption), the participant
// UPDATE is a 0-row no-op. That is intentionally non-fatal — the
// scene_log INSERT and total_pose_count increment still commit, and
// per-participant metadata catches up on the next reconciliation /
// next pose by an actual participant.
func (s *SceneAuditStore) InsertScenePose(
	ctx context.Context,
	id []byte,
	subject, eventType string,
	timestamp *timestamppb.Timestamp,
	actorKind string,
	actorID []byte,
	payload []byte,
	schemaVer int,
	codec string,
	dekRef *int64,
	dekVersion *int32,
	sceneID string,
	posedCharID string,
) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Step 1: INSERT into scene_log (reuses the T6 helper). Returns
		// inserted=false when ON CONFLICT fired (redelivery); in that
		// case skip steps 2-3 so total_pose_count / last_pose_seq stay
		// a function of distinct scene_log rows (INV-SCENE-10).
		inserted, err := s.insertSceneLogTx(
			ctx, tx,
			id, subject, eventType, timestamp,
			actorKind, actorID, payload, schemaVer, codec,
			dekRef, dekVersion,
		)
		if err != nil {
			return err
		}
		if !inserted {
			return nil
		}

		// Step 2: Bump per-scene total_pose_count and capture the new
		// value. RETURNING ensures we observe a deterministic seq even
		// if a concurrent pose on the same scene also runs (Postgres
		// serialises the row-level UPDATE).
		var newSeq int
		if err := tx.QueryRow(
			ctx,
			`UPDATE scenes SET total_pose_count = total_pose_count + 1
			 WHERE id = $1 RETURNING total_pose_count`,
			sceneID,
		).Scan(&newSeq); err != nil {
			return oops.Code("SCENE_TOTAL_POSE_COUNT_UPDATE_FAILED").
				With("scene_id", sceneID).Wrap(err)
		}

		// Step 3: Stamp per-participant last_pose_at + last_pose_seq.
		// Use the canonical event timestamp (not wall clock) — this
		// matches JetStream redelivery semantics. Zero-row UPDATE is
		// intentionally non-fatal (see method doc).
		var ts any
		if timestamp != nil {
			ts = pgnanos.From(timestamp.AsTime())
		}
		if _, err := tx.Exec(
			ctx,
			`UPDATE scene_participants
			 SET last_pose_at = $1, last_pose_seq = $2
			 WHERE scene_id = $3 AND character_id = $4`,
			ts, newSeq, sceneID, posedCharID,
		); err != nil {
			return oops.Code("SCENE_PARTICIPANT_POSE_UPDATE_FAILED").
				With("scene_id", sceneID).
				With("character_id", posedCharID).Wrap(err)
		}
		return nil
	})
	if err != nil {
		// Inner closure errors are already wrapped with oops codes
		// (SCENE_AUDIT_INSERT_FAILED, SCENE_TOTAL_POSE_COUNT_UPDATE_FAILED,
		// or SCENE_PARTICIPANT_POSE_UPDATE_FAILED). pgx.BeginFunc only
		// adds a Begin/Commit error of its own if those phases fail.
		return oops.Code("SCENE_AUDIT_TX_FAILED").
			With("scene_id", sceneID).Wrap(err)
	}
	return nil
}

// insertSceneLogTx executes the scene_log INSERT within a caller-provided
// transaction. Task 7 (InsertScenePose) calls this directly so the scene_log
// INSERT and pose-metadata UPDATEs can share one transaction without
// duplicating SQL.
//
// ON CONFLICT (id) DO NOTHING preserves idempotent redelivery semantics
// (INV-P7 plugin SDK contract) regardless of which caller opens the tx.
// Returns (inserted, err) where inserted is true when a row was actually
// written; false when the ON CONFLICT branch fired. Callers that maintain
// downstream counters (e.g. InsertScenePose) MUST gate those UPDATEs behind
// inserted so redelivery does not over-count (INV-SCENE-10).
func (s *SceneAuditStore) insertSceneLogTx(
	ctx context.Context,
	tx pgx.Tx,
	id []byte,
	subject, eventType string,
	timestamp *timestamppb.Timestamp,
	actorKind string,
	actorID []byte,
	payload []byte,
	schemaVer int,
	codec string,
	dekRef *int64,
	dekVersion *int32,
) (bool, error) {
	var ts any
	if timestamp != nil {
		ts = pgnanos.From(timestamp.AsTime())
	}
	cmd, err := tx.Exec(
		ctx, `
		INSERT INTO scene_log (
			id, subject, type, timestamp, actor_kind, actor_id,
			payload, schema_ver, codec, dek_ref, dek_version
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (id) DO NOTHING`,
		id, subject, eventType, ts, actorKind, actorID, payload, schemaVer, codec, dekRef, dekVersion,
	)
	if err != nil {
		return false, oops.Code("SCENE_AUDIT_INSERT_FAILED").
			With("subject", subject).
			With("type", eventType).
			Wrap(err)
	}
	return cmd.RowsAffected() == 1, nil
}

// AuditEvent is the per-message ingestion RPC. The host per-plugin consumer
// forwards each JetStream delivery here as a *pluginv1.AuditRow built by
// `internal/eventbus/audit.buildAuditRow` (Phase 7 widening, INV-P7-1 +
// INV-P7-11). A successful return ⇒ host acks the JS message.
//
// The Row shape guarantees crypto + projection fields at the wire level —
// no fallback to `req.Event` / `req.Headers` is needed (those legacy
// fields no longer exist on the proto). Validation still mirrors the
// host projection's contract checks (spec §5).
func (s *SceneAuditServer) AuditEvent(ctx context.Context, req *pluginv1.AuditEventRequest) (*pluginv1.AuditEventResponse, error) {
	if req == nil || req.GetRow() == nil {
		return nil, oops.Code("SCENE_AUDIT_MISSING_ROW").Errorf("AuditEventRequest.row required")
	}
	row := req.GetRow()

	codec := row.GetCodec()
	if codec == "" {
		return nil, oops.Code("SCENE_AUDIT_MISSING_FIELD").With("field", "codec").
			Errorf("missing field")
	}

	schemaVer := int(row.GetSchemaVer())
	if schemaVer < 0 || schemaVer > 32767 {
		return nil, oops.Code("SCENE_AUDIT_BAD_SCHEMA_VERSION").With("value", schemaVer).
			Errorf("schema version out of range")
	}

	eventType := row.GetType()
	if eventType == "" {
		return nil, oops.Code("SCENE_AUDIT_MISSING_FIELD").With("field", "type").
			Errorf("missing field")
	}

	subject := row.GetSubject()
	if subject == "" {
		return nil, oops.Code("SCENE_AUDIT_MISSING_FIELD").With("field", "subject").
			Errorf("missing field")
	}

	if len(row.GetId()) != 16 {
		return nil, oops.Code("SCENE_AUDIT_MISSING_ID").Errorf("row.id required (16-byte ULID)")
	}

	// Reject nil timestamp at ingest time. scene_log.timestamp is a
	// non-null TIMESTAMPTZ and queryLog scans it into a non-null
	// time.Time; a single row with nil ts persisted as SQL NULL would
	// turn every subsequent QueryHistory page that includes it into
	// SCENE_AUDIT_SCAN_FAILED. Fail-fast at the boundary.
	if row.GetTimestamp() == nil {
		return nil, oops.Code("SCENE_AUDIT_MISSING_FIELD").With("field", "timestamp").
			Errorf("missing field")
	}

	var actorKind string
	var actorID []byte
	if a := row.GetActor(); a != nil {
		actorKind = a.GetKind().String()
		actorID = a.GetId()
	}
	if actorKind == "" {
		actorKind = defaultActorKind
	}

	var dekRef *int64
	if row.DekRef != nil {
		v := int64(*row.DekRef) //nolint:gosec // scene_log.dek_ref column is BIGINT (signed); uint64→int64 matches column shape
		dekRef = &v
	}
	var dekVersion *int32
	if row.DekVersion != nil {
		v := int32(*row.DekVersion) //nolint:gosec // scene_log.dek_version column is INTEGER (signed); uint32→int32 matches column shape
		dekVersion = &v
	}

	// Dispatch on event type per spec §9.4: scene_pose routes through
	// InsertScenePose (which composes the scene_log INSERT +
	// scenes.total_pose_count UPDATE + scene_participants metadata UPDATE
	// transactionally per T7/INV-SCENE-10); all other event types route
	// through plain Insert (existing behaviour).
	if eventType == "scene_pose" {
		// scene_pose MUST come from a character actor carrying a full
		// 16-byte ULID. Earlier code copied actorID into a fixed array
		// with `copy(posedCharULID[:], actorID)`, which silently zero-
		// pads short payloads (or truncates long ones); the participant
		// UPDATE then no-ops by design while total_pose_count still
		// committed. Reject malformed inputs at ingest so the audit row
		// never lands in scene_log.
		if actorKind != eventbusv1.ActorKind_ACTOR_KIND_CHARACTER.String() {
			return nil, oops.Code("SCENE_AUDIT_INVALID_ACTOR_KIND").
				With("event_type", eventType).
				With("actor_kind", actorKind).
				Errorf("scene_pose requires character actor")
		}
		if len(actorID) != 16 {
			return nil, oops.Code("SCENE_AUDIT_INVALID_ACTOR_ID").
				With("event_type", eventType).
				With("actor_id_len", len(actorID)).
				Errorf("scene_pose requires 16-byte character ULID")
		}

		sceneID, err := parseSceneSubject(subject)
		if err != nil {
			// parseSceneSubject already wraps with SCENE_AUDIT_SUBJECT_INVALID
			// and includes the subject in context — propagate as-is.
			return nil, err
		}
		var posedCharULID ulid.ULID
		copy(posedCharULID[:], actorID)
		posedCharID := posedCharULID.String()

		if err := s.store.InsertScenePose(
			ctx,
			row.GetId(),
			subject,
			eventType,
			row.GetTimestamp(),
			actorKind,
			actorID,
			row.GetPayload(),
			schemaVer,
			codec,
			dekRef,
			dekVersion,
			sceneID,
			posedCharID,
		); err != nil {
			// InsertScenePose already wraps with SCENE_AUDIT_TX_FAILED.
			return nil, err //nolint:wrapcheck // already wrapped by InsertScenePose with SCENE_AUDIT_TX_FAILED
		}
	} else {
		if err := s.store.Insert(
			ctx,
			row.GetId(),
			subject,
			eventType,
			row.GetTimestamp(),
			actorKind,
			actorID,
			row.GetPayload(),
			schemaVer,
			codec,
			dekRef,
			dekVersion,
		); err != nil {
			// SceneAuditStore.Insert already wraps with SCENE_AUDIT_INSERT_FAILED
			// and the same subject/type context — propagate as-is.
			return nil, err //nolint:wrapcheck // already wrapped by Insert with SCENE_AUDIT_INSERT_FAILED
		}
	}

	return &pluginv1.AuditEventResponse{}, nil
}

// QueryHistory streams scene_log rows matching the request after enforcing
// scene membership at the plugin boundary. Authorisation is step 1 and runs
// BEFORE cursor decoding or any DB query — the early-rejection ordering
// avoids timing oracles and is pinned by audit_test.go's
// TestQueryHistoryDeniesNonMemberWithoutHittingLogStore.
//
// The caller (req.Caller) is forwarded verbatim from the host's
// CoreServer.QueryStreamHistory handler (which derives it from the
// authenticated session). Plugins MUST NOT trust client-supplied identity;
// see spec §3.2 for the trust model.
//
// Membership policy: only owner and member roles see rows. Invited rows
// return PERMISSION_DENIED — invitation grants join rights, not passive
// read rights (spec §5.4). Non-CHARACTER caller kinds are rejected;
// admin / system / cross-plugin reads are deferred to a future RPC.
//
// Errors:
//   - codes.PermissionDenied — caller missing, kind unsupported, or non-member
//   - codes.InvalidArgument  — subject empty or malformed
//   - codes.Internal         — store / DB error
func (s *SceneAuditServer) QueryHistory(req *pluginv1.QueryHistoryRequest, stream pluginv1.PluginAuditService_QueryHistoryServer) error {
	if req == nil || req.GetSubject() == "" {
		return status.Error(codes.InvalidArgument, "subject required") //nolint:wrapcheck // intentional: gRPC status is the documented contract for this handler; wrapping would shadow the code visible to mapHistoryError
	}

	// Auth — step 1, before any other work.
	caller := req.GetCaller()
	if caller == nil {
		slog.InfoContext(stream.Context(), "scene audit denied — caller missing",
			"subject", req.GetSubject(), "code", "SCENE_AUDIT_AUTH_REQUIRED")
		return status.Error(codes.PermissionDenied, "caller required") //nolint:wrapcheck // preserve gRPC status code for mapHistoryError
	}
	if caller.GetKind() != eventbusv1.ActorKind_ACTOR_KIND_CHARACTER {
		slog.InfoContext(stream.Context(), "scene audit denied — non-character caller",
			"subject", req.GetSubject(), "kind", caller.GetKind().String(),
			"code", "SCENE_AUDIT_AUTH_REQUIRED")
		return status.Error(codes.PermissionDenied, "unsupported caller kind") //nolint:wrapcheck // preserve gRPC status code for mapHistoryError
	}
	callerIDBytes := caller.GetId()
	if len(callerIDBytes) != 16 {
		slog.InfoContext(stream.Context(), "scene audit denied — caller id wrong length",
			"subject", req.GetSubject(), "code", "SCENE_AUDIT_AUTH_REQUIRED")
		return status.Error(codes.PermissionDenied, "caller id required") //nolint:wrapcheck // preserve gRPC status code for mapHistoryError
	}
	var callerULID ulid.ULID
	copy(callerULID[:], callerIDBytes)
	if callerULID == (ulid.ULID{}) {
		slog.InfoContext(stream.Context(), "scene audit denied — caller id zero",
			"subject", req.GetSubject(), "code", "SCENE_AUDIT_AUTH_REQUIRED")
		return status.Error(codes.PermissionDenied, "caller id required") //nolint:wrapcheck // preserve gRPC status code for mapHistoryError
	}
	callerCharID := callerULID.String()

	// Subject parse.
	sceneID, err := parseSceneSubject(req.GetSubject())
	if err != nil {
		slog.InfoContext(stream.Context(), "scene audit denied — subject malformed",
			"subject", req.GetSubject(), "code", "SCENE_AUDIT_SUBJECT_INVALID")
		return status.Error(codes.InvalidArgument, err.Error()) //nolint:wrapcheck // preserve gRPC status code for mapHistoryError
	}

	// Membership check. Fail closed if memberLookup wasn't wired — the
	// server uses field injection (main.go:108-109), so a missed setup
	// would otherwise panic on the first audit read.
	if s.memberLookup == nil {
		return status.Error(codes.Internal, "membership lookup not configured") //nolint:wrapcheck // gRPC status is the contract; oops would shadow the code
	}
	ok, err := s.memberLookup.IsMember(stream.Context(), sceneID, callerCharID)
	if err != nil {
		// Log the underlying error server-side; return a generic message
		// so internal store/transport details don't leak past the plugin
		// boundary. This path is not rewritten by host's mapHistoryError.
		slog.ErrorContext(stream.Context(), "scene audit membership lookup failed",
			"subject", req.GetSubject(), "scene_id", sceneID,
			"character_id", callerCharID, "err", err.Error())
		return status.Error(codes.Internal, "membership lookup failed") //nolint:wrapcheck // gRPC status is the contract; oops would shadow the code
	}
	if !ok {
		slog.InfoContext(stream.Context(), "scene audit denied — non-member",
			"subject", req.GetSubject(), "scene_id", sceneID,
			"character_id", callerCharID, "code", "SCENE_AUDIT_ACCESS_DENIED")
		return status.Error(codes.PermissionDenied, "not a participant") //nolint:wrapcheck // preserve gRPC status code for mapHistoryError
	}

	// From here, the existing pagination + streaming logic runs unchanged.
	ctx := stream.Context()
	pageSize := int(req.GetPageSize())
	if pageSize <= 0 {
		pageSize = auditDefaultPageSize
	}
	if pageSize > auditMaxPageSize {
		pageSize = auditMaxPageSize
	}

	dir := req.GetDirection()
	if dir == 0 {
		dir = directionForward
	}

	var (
		afterCursor  []byte
		beforeCursor []byte
	)
	if v := req.GetAfter(); len(v) > 0 {
		afterCursor = v
	}
	if v := req.GetBefore(); len(v) > 0 {
		beforeCursor = v
	}

	rows, err := s.store.queryLog(ctx, req.GetSubject(), afterCursor, beforeCursor,
		req.GetNotBefore(), req.GetNotAfter(), dir == directionBackward, pageSize)
	if err != nil {
		return err
	}

	for i := range rows {
		r := &rows[i]
		var dekRefU64 *uint64
		if r.dekRef != nil {
			v := uint64(*r.dekRef) //nolint:gosec // dek_ref originates as crypto_keys.id (always >= 0); int64→uint64 widening is safe
			dekRefU64 = &v
		}
		var dekVerU32 *uint32
		if r.dekVersion != nil {
			v := uint32(*r.dekVersion) //nolint:gosec // dek_version is a 1-based counter (always >= 0); int32→uint32 widening is safe
			dekVerU32 = &v
		}
		resp := &pluginv1.QueryHistoryResponse{
			Row: &pluginv1.AuditRow{
				Id:         r.id,
				Subject:    r.subject,
				Type:       r.eventType,
				Timestamp:  timestamppb.New(r.timestamp.Time()),
				Actor:      actorProtoFromRow(r.actorKind, r.actorID),
				Codec:      r.codec,
				Payload:    r.payload,
				DekRef:     dekRefU64,
				DekVersion: dekVerU32,
				SchemaVer:  int32(r.schemaVer), //nolint:gosec // schema_ver column is SMALLINT (validated <= 32767 at insert); int→int32 is safe
			},
		}
		if err := stream.Send(resp); err != nil {
			// Log server-side; return a generic message so transport details
			// don't leak past the plugin boundary (path not rewritten by host).
			slog.ErrorContext(ctx, "scene audit stream send failed",
				"subject", req.GetSubject(), "err", err.Error())
			return status.Error(codes.Internal, "stream send failed") //nolint:wrapcheck // gRPC status is the contract; oops would shadow the code
		}
	}
	return nil
}

// parseSceneSubject extracts sceneID from a JetStream-native scene subject.
// Expected: events.<gameID>.scene.<sceneID>.<channel>[.<...>]. Rejects
// wildcard tokens and malformed shapes. See spec §5.3.
func parseSceneSubject(subject string) (string, error) {
	parts := strings.Split(subject, ".")
	if len(parts) < 5 {
		return "", oops.Code("SCENE_AUDIT_SUBJECT_INVALID").
			With("subject", subject).
			Errorf("subject does not match events.<game>.scene.<id>.<channel>")
	}
	if parts[0] != "events" || parts[2] != "scene" {
		return "", oops.Code("SCENE_AUDIT_SUBJECT_INVALID").
			With("subject", subject).
			Errorf("subject not owned by core-scenes")
	}
	for _, p := range parts {
		// Empty token (e.g., "events.main.scene..ic") MUST also be rejected;
		// otherwise parts[3] returns "" and falls through to membership
		// denial instead of the InvalidArgument the contract specifies.
		if p == "" || strings.ContainsAny(p, "*>") {
			return "", oops.Code("SCENE_AUDIT_SUBJECT_INVALID").
				With("subject", subject).
				Errorf("empty or wildcard subject tokens not permitted for QueryHistory")
		}
	}
	return parts[3], nil
}

// logRow is the scanned representation of one scene_log row.
type logRow struct {
	id         []byte
	subject    string
	eventType  string
	timestamp  pgnanos.Time
	actorKind  string
	actorID    []byte
	payload    []byte
	schemaVer  int
	codec      string
	dekRef     *int64
	dekVersion *int32
}

// queryLog runs the scene_log SELECT with optional subject, cursor, and
// time-bound filters. The sort order mirrors history.DirectionForward
// (ASC by id) and history.DirectionBackward (DESC by id); ULIDs are time-
// ordered so id ordering == chronological ordering within a subject.
func (s *SceneAuditStore) queryLog(
	ctx context.Context,
	subject string,
	after, before []byte,
	notBefore, notAfter *timestamppb.Timestamp,
	reverse bool,
	pageSize int,
) ([]logRow, error) {
	var (
		conds []string
		args  []any
	)
	args = append(args, subject)
	conds = append(conds, "subject = $1")

	idx := 2
	if len(after) > 0 {
		conds = append(conds, "id > $"+itoa(idx))
		args = append(args, after)
		idx++
	}
	if len(before) > 0 {
		conds = append(conds, "id < $"+itoa(idx))
		args = append(args, before)
		idx++
	}
	if notBefore != nil {
		conds = append(conds, "timestamp >= $"+itoa(idx))
		args = append(args, pgnanos.From(notBefore.AsTime()))
		idx++
	}
	if notAfter != nil {
		conds = append(conds, "timestamp <= $"+itoa(idx))
		args = append(args, pgnanos.From(notAfter.AsTime()))
		idx++
	}

	order := "ASC"
	if reverse {
		order = "DESC"
	}

	args = append(args, pageSize)
	limitIdx := itoa(idx)

	query := "SELECT id, subject, type, timestamp, actor_kind, actor_id, payload, schema_ver, codec, dek_ref, dek_version FROM scene_log WHERE " +
		strings.Join(conds, " AND ") +
		" ORDER BY id " + order + " LIMIT $" + limitIdx

	pgRows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, oops.Code("SCENE_AUDIT_QUERY_FAILED").
			With("subject", subject).
			Wrap(err)
	}
	defer pgRows.Close()

	var out []logRow
	for pgRows.Next() {
		var r logRow
		if err := pgRows.Scan(&r.id, &r.subject, &r.eventType, &r.timestamp, &r.actorKind, &r.actorID, &r.payload, &r.schemaVer, &r.codec, &r.dekRef, &r.dekVersion); err != nil {
			return nil, oops.Code("SCENE_AUDIT_SCAN_FAILED").Wrap(err)
		}
		out = append(out, r)
	}
	if err := pgRows.Err(); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return out, nil
		}
		return nil, oops.Code("SCENE_AUDIT_SCAN_FAILED").Wrap(err)
	}
	return out, nil
}

// actorProtoFromRow reconstructs an Actor proto from the stored row. Empty
// actor kind returns nil, which matches how the host projection records
// system-origin events without a concrete actor ID.
func actorProtoFromRow(kind string, id []byte) *eventbusv1.Actor {
	if kind == "" && len(id) == 0 {
		return nil
	}
	return &eventbusv1.Actor{
		Kind: actorKindFromString(kind),
		Id:   id,
	}
}

// actorKindFromString maps the stored string back to the proto enum.
// AuditEvent writes the enum's String() form (e.g. "ACTOR_KIND_PLAYER"),
// while older or external publishers may write the lowercase variant
// ("player"); both are accepted here so the read path round-trips every
// kind the write path can produce. Unknown values fall through to
// ACTOR_KIND_UNSPECIFIED, matching the spec's tolerance for publisher
// contract drift.
func actorKindFromString(s string) eventbusv1.ActorKind {
	switch s {
	case "ACTOR_KIND_CHARACTER", "character":
		return eventbusv1.ActorKind_ACTOR_KIND_CHARACTER
	case "ACTOR_KIND_SYSTEM", "system":
		return eventbusv1.ActorKind_ACTOR_KIND_SYSTEM
	case "ACTOR_KIND_PLUGIN", "plugin":
		return eventbusv1.ActorKind_ACTOR_KIND_PLUGIN
	case "ACTOR_KIND_PLAYER", "player":
		return eventbusv1.ActorKind_ACTOR_KIND_PLAYER
	default:
		return eventbusv1.ActorKind_ACTOR_KIND_UNSPECIFIED
	}
}

// itoa formats a small non-negative int without strconv — the query
// builder inlines placeholder indices ≤ 16 (max 4 filters + LIMIT), so a
// hand-rolled path avoids allocations in the hot path.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [4]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
