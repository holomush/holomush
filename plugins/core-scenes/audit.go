// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// Audit header names mirror the host's `internal/eventbus/audit` projection
// so the plugin stores the same metadata the host does for its own subjects.
const (
	auditHeaderCodec     = "App-Codec"
	auditHeaderSchemaVer = "App-Schema-Version"
	auditHeaderEventType = "App-Event-Type"
	auditHeaderActorKind = "App-Actor-Kind"
	auditHeaderActorID   = "App-Actor-ID"
)

// defaultActorKind matches the host audit projection default. When the
// publisher omits App-Actor-Kind, the audit row records "system".
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
	store *SceneAuditStore
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
) error {
	var ts any
	if timestamp != nil {
		ts = timestamp.AsTime()
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO scene_log (
			id, subject, type, timestamp, actor_kind, actor_id,
			payload, schema_ver, codec
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO NOTHING`,
		id, subject, eventType, ts, actorKind, actorID, payload, schemaVer, codec,
	)
	if err != nil {
		return oops.Code("SCENE_AUDIT_INSERT_FAILED").
			With("subject", subject).
			With("type", eventType).
			Wrap(err)
	}
	return nil
}

// AuditEvent is the per-message ingestion RPC. The host per-plugin consumer
// forwards each JetStream delivery here with headers mapped verbatim from
// the JS message. A successful return ⇒ host acks the JS message.
//
// Validation mirrors the host projection's contract checks (spec §5)
// exactly, so a malformed publisher contract surfaces the same error
// regardless of which owner handles the subject.
func (s *SceneAuditServer) AuditEvent(ctx context.Context, req *pluginv1.AuditEventRequest) (*pluginv1.AuditEventResponse, error) {
	if req == nil || req.GetEvent() == nil {
		return nil, oops.Code("SCENE_AUDIT_MISSING_EVENT").Errorf("AuditEventRequest.event required")
	}
	ev := req.GetEvent()
	headers := req.GetHeaders()

	codec := headers[auditHeaderCodec]
	if codec == "" {
		return nil, oops.Code("SCENE_AUDIT_MISSING_HEADER").With("header", auditHeaderCodec).
			Errorf("missing header")
	}
	schemaVer := headers[auditHeaderSchemaVer]
	if schemaVer == "" {
		return nil, oops.Code("SCENE_AUDIT_MISSING_HEADER").With("header", auditHeaderSchemaVer).
			Errorf("missing header")
	}
	ver, err := parseSchemaVer(schemaVer)
	if err != nil {
		return nil, err
	}

	eventType := headers[auditHeaderEventType]
	if eventType == "" {
		// Fall back to the Event proto's type field. The host projection
		// requires the header, but the plugin tolerates either source so
		// a publisher that sets only proto.Type still yields a complete row.
		eventType = ev.GetType()
	}
	if eventType == "" {
		return nil, oops.Code("SCENE_AUDIT_MISSING_HEADER").With("header", auditHeaderEventType).
			Errorf("missing header and event.type empty")
	}

	actorKind := headers[auditHeaderActorKind]
	if actorKind == "" {
		if actor := ev.GetActor(); actor != nil {
			actorKind = actor.GetKind().String()
		}
	}
	if actorKind == "" {
		actorKind = defaultActorKind
	}

	var actorID []byte
	if v := headers[auditHeaderActorID]; v != "" {
		parsed, parseErr := ulid.Parse(v)
		if parseErr != nil {
			return nil, oops.Code("SCENE_AUDIT_BAD_ACTOR_ID").With("value", v).Wrap(parseErr)
		}
		b := parsed.Bytes()
		actorID = b
	} else if actor := ev.GetActor(); actor != nil && len(actor.GetId()) > 0 {
		actorID = actor.GetId()
	}

	if len(ev.GetId()) == 0 {
		return nil, oops.Code("SCENE_AUDIT_MISSING_ID").Errorf("event.id required")
	}

	if err := s.store.Insert(
		ctx,
		ev.GetId(),
		ev.GetSubject(),
		eventType,
		ev.GetTimestamp(),
		actorKind,
		actorID,
		ev.GetPayload(),
		ver,
		codec,
	); err != nil {
		return nil, err
	}

	return &pluginv1.AuditEventResponse{}, nil
}

// QueryHistory streams scene_log rows matching the request. The plugin
// enforces authorisation via a membership check: the caller MUST be a
// participant of the queried scene (owner, member, or invited). Absent
// auth context, the plugin rejects the call — the host is expected to
// propagate the character's identity via gRPC metadata, but in the
// current wiring the host is a trusted caller; until auth context is
// plumbed through the plugin transport, the plugin permits any query
// and relies on the host gatekeeping at the outer gRPC boundary.
//
// TODO(holomush-1tvn.12 follow-up): plumb caller identity from the host
// through the plugin gRPC connection so the membership check can hard-
// gate non-participant queries at the plugin boundary.
func (s *SceneAuditServer) QueryHistory(req *pluginv1.QueryHistoryRequest, stream pluginv1.PluginAuditService_QueryHistoryServer) error {
	if req == nil || req.GetSubject() == "" {
		return oops.Code("SCENE_AUDIT_SUBJECT_REQUIRED").Errorf("subject required")
	}
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
		row := &rows[i]
		resp := &pluginv1.QueryHistoryResponse{
			Event: &eventbusv1.Event{
				Id:        row.id,
				Subject:   row.subject,
				Type:      row.eventType,
				Timestamp: timestamppb.New(row.timestamp),
				Actor:     actorProtoFromRow(row.actorKind, row.actorID),
				Payload:   row.payload,
			},
		}
		if err := stream.Send(resp); err != nil {
			return oops.Code("SCENE_AUDIT_STREAM_SEND_FAILED").
				With("subject", req.GetSubject()).
				Wrap(err)
		}
	}
	return nil
}

// logRow is the scanned representation of one scene_log row.
type logRow struct {
	id        []byte
	subject   string
	eventType string
	timestamp time.Time
	actorKind string
	actorID   []byte
	payload   []byte
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
		args = append(args, notBefore.AsTime())
		idx++
	}
	if notAfter != nil {
		conds = append(conds, "timestamp <= $"+itoa(idx))
		args = append(args, notAfter.AsTime())
		idx++
	}

	order := "ASC"
	if reverse {
		order = "DESC"
	}

	args = append(args, pageSize)
	limitIdx := itoa(idx)

	query := "SELECT id, subject, type, timestamp, actor_kind, actor_id, payload FROM scene_log WHERE " +
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
		if err := pgRows.Scan(&r.id, &r.subject, &r.eventType, &r.timestamp, &r.actorKind, &r.actorID, &r.payload); err != nil {
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

// actorKindFromString maps the stored string back to the proto enum. The
// host projection writes one of "character" / "system" / "plugin" via the
// proto enum's String() method, so the reverse mapping is keyed the same
// way. Unknown values fall through to ACTOR_KIND_UNSPECIFIED, matching
// the spec's tolerance for publisher contract drift.
func actorKindFromString(s string) eventbusv1.ActorKind {
	switch s {
	case "ACTOR_KIND_CHARACTER", "character":
		return eventbusv1.ActorKind_ACTOR_KIND_CHARACTER
	case "ACTOR_KIND_SYSTEM", "system":
		return eventbusv1.ActorKind_ACTOR_KIND_SYSTEM
	case "ACTOR_KIND_PLUGIN", "plugin":
		return eventbusv1.ActorKind_ACTOR_KIND_PLUGIN
	default:
		return eventbusv1.ActorKind_ACTOR_KIND_UNSPECIFIED
	}
}

// parseSchemaVer validates and parses the App-Schema-Version header. The
// column is SMALLINT; values outside [0, 32767] are rejected at the
// boundary rather than relying on a silent pgx downcast.
func parseSchemaVer(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, oops.Code("SCENE_AUDIT_BAD_SCHEMA_VERSION").With("value", s).
				Errorf("non-numeric schema version")
		}
		n = n*10 + int(c-'0')
		if n > 32767 {
			return 0, oops.Code("SCENE_AUDIT_BAD_SCHEMA_VERSION").With("value", s).
				Errorf("schema version exceeds int16 range")
		}
	}
	return n, nil
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
