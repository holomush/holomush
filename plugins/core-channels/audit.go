// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// channelAuditDefaultActorKind matches the host audit projection default: when
// the delivered Row.Actor is nil the audit row records "system".
const channelAuditDefaultActorKind = "system"

// channelAuditDefaultPageSize is applied when the caller supplies PageSize <= 0.
const channelAuditDefaultPageSize = 50

// channelScrollbackFallback bounds a history page when the configured
// scrollback cap is unset/non-positive (defensive; Init validates it > 0).
const channelScrollbackFallback = 500

// directionForward / directionBackward mirror eventbus.Direction* to avoid a
// dependency on internal/eventbus from the plugin binary.
const (
	channelDirectionForward  = int32(1)
	channelDirectionBackward = int32(2)
)

// channelLogRow is the scanned representation of one channel_log row. Channel
// events are plaintext (D-04): there are NO dek_ref / dek_version columns.
type channelLogRow struct {
	id        []byte
	subject   string
	eventType string
	timestamp time.Time
	actorKind string
	actorID   []byte
	payload   []byte
	schemaVer int
	codec     string
}

// channelAuditLogStore is the log-storage surface ChannelAuditServer needs.
type channelAuditLogStore interface {
	Insert(
		ctx context.Context,
		id []byte,
		subject, eventType string,
		timestamp *timestamppb.Timestamp,
		actorKind string,
		actorID, payload []byte,
		schemaVer int,
		codec string,
	) error
	queryLog(
		ctx context.Context,
		subject string,
		after, before []byte,
		notBefore, notAfter *timestamppb.Timestamp,
		reverse bool,
		pageSize int,
	) ([]channelLogRow, error)
}

// channelMembershipAuthLookup is the membership-check surface ChannelAuditServer
// needs: reports membership and the member's most-recent joined_at (the D-07
// history floor). *channelStore satisfies it.
type channelMembershipAuthLookup interface {
	MembershipForHistory(ctx context.Context, channelID, characterID string) (isMember bool, joinedAt time.Time, err error)
}

// ChannelAuditServer implements PluginAuditService for core-channels, mirroring
// SceneAuditServer minus the DEK columns (plaintext, D-04).
//
// AuditEvent is invoked by the host per-plugin audit consumer for every message
// delivered on events.*.channel.>; it is an idempotent INSERT.
//
// QueryHistory is invoked by the host when a channel subject routes to this
// plugin. Membership is enforced at auth step-1 BEFORE any DB work; reads never
// cross the member's most-recent joined_at (D-07) and a page is clamped to the
// scrollback cap.
type ChannelAuditServer struct {
	pluginv1.UnimplementedPluginAuditServiceServer
	store         channelAuditLogStore
	memberLookup  channelMembershipAuthLookup
	scrollbackCap int
}

// ChannelAuditStore wraps the pgx pool with channel_log SQL helpers. Kept
// separate from channelStore so the domain-state and audit-log surfaces don't
// share an accessor; both are built from the same pool in Init.
type ChannelAuditStore struct {
	pool *pgxpool.Pool
}

// NewChannelAuditStore constructs a ChannelAuditStore. The pool must already be
// open and pointed at the plugin's schema (search_path=plugin_core_channels).
func NewChannelAuditStore(pool *pgxpool.Pool) *ChannelAuditStore {
	return &ChannelAuditStore{pool: pool}
}

// Insert persists one audit row idempotently: ON CONFLICT (id) DO NOTHING makes
// a redelivery of the same bus dedup key (Nats-Msg-Id) a no-op, and the host
// still acks (T-01-17). Channel events carry no DEK (D-04).
func (s *ChannelAuditStore) Insert(
	ctx context.Context,
	id []byte,
	subject, eventType string,
	timestamp *timestamppb.Timestamp,
	actorKind string,
	actorID, payload []byte,
	schemaVer int,
	codec string,
) error {
	var ts any
	if timestamp != nil {
		ts = timestamp.AsTime()
	}
	_, err := s.pool.Exec(
		ctx, `
		INSERT INTO channel_log (
			id, subject, type, timestamp, actor_kind, actor_id, payload, schema_ver, codec
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO NOTHING`,
		id, subject, eventType, ts, actorKind, actorID, payload, schemaVer, codec,
	)
	if err != nil {
		return oops.Code("CHANNEL_AUDIT_INSERT_FAILED").
			With("subject", subject).With("type", eventType).Wrap(err)
	}
	return nil
}

// queryLog runs the channel_log SELECT with optional subject, cursor, and
// time-bound filters. ULIDs are time-ordered, so ORDER BY id gives chronological
// order within a subject.
func (s *ChannelAuditStore) queryLog(
	ctx context.Context,
	subject string,
	after, before []byte,
	notBefore, notAfter *timestamppb.Timestamp,
	reverse bool,
	pageSize int,
) ([]channelLogRow, error) {
	conds := []string{"subject = $1"}
	args := []any{subject}
	idx := 2
	if len(after) > 0 {
		conds = append(conds, "id > $"+strconv.Itoa(idx))
		args = append(args, after)
		idx++
	}
	if len(before) > 0 {
		conds = append(conds, "id < $"+strconv.Itoa(idx))
		args = append(args, before)
		idx++
	}
	if notBefore != nil {
		conds = append(conds, "timestamp >= $"+strconv.Itoa(idx))
		args = append(args, notBefore.AsTime())
		idx++
	}
	if notAfter != nil {
		conds = append(conds, "timestamp <= $"+strconv.Itoa(idx))
		args = append(args, notAfter.AsTime())
		idx++
	}
	order := "ASC"
	if reverse {
		order = "DESC"
	}
	args = append(args, pageSize)

	query := "SELECT id, subject, type, timestamp, actor_kind, actor_id, payload, schema_ver, codec FROM channel_log WHERE " +
		strings.Join(conds, " AND ") +
		" ORDER BY id " + order + " LIMIT $" + strconv.Itoa(idx)

	pgRows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, oops.Code("CHANNEL_AUDIT_QUERY_FAILED").With("subject", subject).Wrap(err)
	}
	defer pgRows.Close()

	var out []channelLogRow
	for pgRows.Next() {
		var r channelLogRow
		if err := pgRows.Scan(&r.id, &r.subject, &r.eventType, &r.timestamp, &r.actorKind, &r.actorID, &r.payload, &r.schemaVer, &r.codec); err != nil {
			return nil, oops.Code("CHANNEL_AUDIT_SCAN_FAILED").Wrap(err)
		}
		out = append(out, r)
	}
	if err := pgRows.Err(); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return out, nil
		}
		return nil, oops.Code("CHANNEL_AUDIT_SCAN_FAILED").Wrap(err)
	}
	return out, nil
}

// AuditEvent is the per-message ingestion RPC. The host per-plugin consumer
// forwards each JetStream delivery here as a *pluginv1.AuditRow. Validation
// mirrors the host projection contract checks; a successful return ⇒ the host
// acks the JS message. Channel rows carry no DEK (D-04), so dek_ref/dek_version
// are never read.
func (s *ChannelAuditServer) AuditEvent(ctx context.Context, req *pluginv1.AuditEventRequest) (*pluginv1.AuditEventResponse, error) {
	if req == nil || req.GetRow() == nil {
		return nil, oops.Code("CHANNEL_AUDIT_MISSING_ROW").Errorf("AuditEventRequest.row required")
	}
	row := req.GetRow()

	if row.GetCodec() == "" {
		return nil, oops.Code("CHANNEL_AUDIT_MISSING_FIELD").With("field", "codec").Errorf("missing field")
	}
	schemaVer := int(row.GetSchemaVer())
	if schemaVer < 0 || schemaVer > 32767 {
		return nil, oops.Code("CHANNEL_AUDIT_BAD_SCHEMA_VERSION").With("value", schemaVer).Errorf("schema version out of range")
	}
	if row.GetType() == "" {
		return nil, oops.Code("CHANNEL_AUDIT_MISSING_FIELD").With("field", "type").Errorf("missing field")
	}
	if row.GetSubject() == "" {
		return nil, oops.Code("CHANNEL_AUDIT_MISSING_FIELD").With("field", "subject").Errorf("missing field")
	}
	if len(row.GetId()) != 16 {
		return nil, oops.Code("CHANNEL_AUDIT_MISSING_ID").Errorf("row.id required (16-byte ULID)")
	}
	// channel_log.timestamp is a non-null TIMESTAMPTZ; reject nil at ingest so a
	// row can never persist as SQL NULL and break every subsequent page scan.
	if row.GetTimestamp() == nil {
		return nil, oops.Code("CHANNEL_AUDIT_MISSING_FIELD").With("field", "timestamp").Errorf("missing field")
	}

	actorKind := channelAuditDefaultActorKind
	var actorID []byte
	if a := row.GetActor(); a != nil {
		if k := a.GetKind().String(); k != "" {
			actorKind = k
		}
		actorID = a.GetId()
	}

	if err := s.store.Insert(
		ctx,
		row.GetId(), row.GetSubject(), row.GetType(), row.GetTimestamp(),
		actorKind, actorID, row.GetPayload(), schemaVer, row.GetCodec(),
	); err != nil {
		return nil, err //nolint:wrapcheck // already wrapped by Insert with CHANNEL_AUDIT_INSERT_FAILED
	}
	return &pluginv1.AuditEventResponse{}, nil
}

// QueryHistory streams channel_log rows matching the request after enforcing
// channel membership at the plugin boundary. Authorisation is step 1 and runs
// BEFORE any DB query — the early-rejection ordering avoids timing oracles.
// Membership gating applies to EVERY channel type (public included): the 01-04
// public-read permit is visibility/discoverability, NOT history content
// (INV-CHANNEL-1). Reads never cross the member's most-recent joined_at (D-07)
// and a page is clamped to the scrollback cap.
//
// Errors:
//   - codes.PermissionDenied — caller missing, kind unsupported, or non-member
//   - codes.InvalidArgument  — subject empty or malformed
//   - codes.Internal         — lookup unconfigured / store error
func (s *ChannelAuditServer) QueryHistory(req *pluginv1.QueryHistoryRequest, stream pluginv1.PluginAuditService_QueryHistoryServer) error {
	ctx := stream.Context()
	if req == nil || req.GetSubject() == "" {
		return status.Error(codes.InvalidArgument, "subject required") //nolint:wrapcheck // gRPC status is the documented contract
	}

	// Auth — step 1, before any other work.
	caller := req.GetCaller()
	if caller == nil {
		slog.InfoContext(ctx, "channel audit denied — caller missing",
			"subject", req.GetSubject(), "code", "CHANNEL_AUDIT_AUTH_REQUIRED")
		return status.Error(codes.PermissionDenied, "caller required") //nolint:wrapcheck // preserve gRPC status code
	}
	if caller.GetKind() != eventbusv1.ActorKind_ACTOR_KIND_CHARACTER {
		slog.InfoContext(ctx, "channel audit denied — non-character caller",
			"subject", req.GetSubject(), "kind", caller.GetKind().String(),
			"code", "CHANNEL_AUDIT_AUTH_REQUIRED")
		return status.Error(codes.PermissionDenied, "unsupported caller kind") //nolint:wrapcheck // preserve gRPC status code
	}
	callerIDBytes := caller.GetId()
	if len(callerIDBytes) != 16 {
		slog.InfoContext(ctx, "channel audit denied — caller id wrong length",
			"subject", req.GetSubject(), "code", "CHANNEL_AUDIT_AUTH_REQUIRED")
		return status.Error(codes.PermissionDenied, "caller id required") //nolint:wrapcheck // preserve gRPC status code
	}
	var callerULID ulid.ULID
	copy(callerULID[:], callerIDBytes)
	if callerULID == (ulid.ULID{}) {
		slog.InfoContext(ctx, "channel audit denied — caller id zero",
			"subject", req.GetSubject(), "code", "CHANNEL_AUDIT_AUTH_REQUIRED")
		return status.Error(codes.PermissionDenied, "caller id required") //nolint:wrapcheck // preserve gRPC status code
	}
	callerCharID := callerULID.String()

	// Subject parse.
	channelID, err := parseChannelSubject(req.GetSubject())
	if err != nil {
		slog.InfoContext(ctx, "channel audit denied — subject malformed",
			"subject", req.GetSubject(), "code", "CHANNEL_AUDIT_SUBJECT_INVALID")
		return status.Error(codes.InvalidArgument, err.Error()) //nolint:wrapcheck // preserve gRPC status code
	}

	// Membership fence — the single authorization step shared with the
	// service-layer QueryChannelHistory (HistoryForMember). Fails closed.
	joinedAt, err := s.authorizeMember(ctx, channelID, callerCharID)
	if err != nil {
		return err
	}

	// joined_at floor (D-07): history never crosses the member's most-recent
	// join. Apply it as a not_before lower bound, taking the later of the
	// caller-supplied not_before and the floor.
	floor := timestamppb.New(joinedAt)
	notBefore := req.GetNotBefore()
	if notBefore == nil || floor.AsTime().After(notBefore.AsTime()) {
		notBefore = floor
	}

	// Page size: default when unset, clamp to the scrollback cap (D-07).
	pageSize := s.clampHistoryPageSize(int(req.GetPageSize()))

	dir := req.GetDirection()
	if dir == 0 {
		dir = channelDirectionForward
	}

	rows, err := s.store.queryLog(ctx, req.GetSubject(), req.GetAfter(), req.GetBefore(),
		notBefore, req.GetNotAfter(), dir == channelDirectionBackward, pageSize)
	if err != nil {
		slog.ErrorContext(ctx, "channel audit query failed",
			"subject", req.GetSubject(), "err", err.Error())
		return status.Error(codes.Internal, "history query failed") //nolint:wrapcheck // gRPC status is the contract
	}

	for i := range rows {
		r := &rows[i]
		resp := &pluginv1.QueryHistoryResponse{
			Row: &pluginv1.AuditRow{
				Id:        r.id,
				Subject:   r.subject,
				Type:      r.eventType,
				Timestamp: timestamppb.New(r.timestamp),
				Actor:     channelActorProtoFromRow(r.actorKind, r.actorID),
				Codec:     r.codec,
				Payload:   r.payload,
				SchemaVer: int32(r.schemaVer), //nolint:gosec // schema_ver is SMALLINT (validated <= 32767 at insert)
			},
		}
		if err := stream.Send(resp); err != nil {
			slog.ErrorContext(ctx, "channel audit stream send failed",
				"subject", req.GetSubject(), "err", err.Error())
			return status.Error(codes.Internal, "stream send failed") //nolint:wrapcheck // gRPC status is the contract
		}
	}
	return nil
}

// authorizeMember is the single channel-history membership fence, shared by the
// streaming QueryHistory and the service-layer QueryChannelHistory
// (HistoryForMember) so history authorization lives in ONE place. It returns the
// member's most-recent joined_at (the D-07 history floor) on success, or a gRPC
// status error: Internal when the lookup is unwired or fails, PermissionDenied
// for a non-member. MembershipForHistory returns the same false for an absent
// channel and a non-member, so denial is existence-oracle-safe.
func (s *ChannelAuditServer) authorizeMember(ctx context.Context, channelID, callerCharID string) (time.Time, error) {
	if s.memberLookup == nil {
		return time.Time{}, status.Error(codes.Internal, "membership lookup not configured") //nolint:wrapcheck // gRPC status is the contract
	}
	isMember, joinedAt, err := s.memberLookup.MembershipForHistory(ctx, channelID, callerCharID)
	if err != nil {
		slog.ErrorContext(ctx, "channel audit membership lookup failed",
			"channel_id", channelID, "character_id", callerCharID, "err", err.Error())
		return time.Time{}, status.Error(codes.Internal, "membership lookup failed") //nolint:wrapcheck // gRPC status is the contract
	}
	if !isMember {
		slog.InfoContext(ctx, "channel audit denied — non-member",
			"channel_id", channelID, "character_id", callerCharID, "code", "CHANNEL_AUDIT_ACCESS_DENIED")
		return time.Time{}, status.Error(codes.PermissionDenied, "not a member") //nolint:wrapcheck // preserve gRPC status code
	}
	return joinedAt, nil
}

// clampHistoryPageSize applies the default page size when limit <= 0 and clamps
// to the scrollback cap (D-07). Shared by both history read paths.
func (s *ChannelAuditServer) clampHistoryPageSize(limit int) int {
	pageSize := limit
	if pageSize <= 0 {
		pageSize = channelAuditDefaultPageSize
	}
	maxPage := s.scrollbackCap
	if maxPage <= 0 {
		maxPage = channelScrollbackFallback
	}
	if pageSize > maxPage {
		pageSize = maxPage
	}
	return pageSize
}

// HistoryForMember is the membership-gated history read the service-layer
// QueryChannelHistory delegates to. It reuses the SAME fence (authorizeMember)
// and floor/cap logic as the streaming QueryHistory — the auth is NOT
// re-implemented. subject is the fully-qualified channel subject the caller's
// channel maps to (events.<game>.channel.<id>); channelID is that channel's id
// (fence key); callerCharID is the reading character; limit is the requested
// page size (clamped). Returns rows oldest-first, or a gRPC status error.
func (s *ChannelAuditServer) HistoryForMember(ctx context.Context, subject, channelID, callerCharID string, limit int) ([]channelLogRow, error) {
	joinedAt, err := s.authorizeMember(ctx, channelID, callerCharID)
	if err != nil {
		return nil, err
	}
	notBefore := timestamppb.New(joinedAt)
	pageSize := s.clampHistoryPageSize(limit)
	rows, err := s.store.queryLog(ctx, subject, nil, nil, notBefore, nil, false, pageSize)
	if err != nil {
		slog.ErrorContext(ctx, "channel history query failed", "subject", subject, "err", err.Error())
		return nil, status.Error(codes.Internal, "history query failed") //nolint:wrapcheck // gRPC status is the contract
	}
	return rows, nil
}

// parseChannelSubject extracts channelID from a JetStream-native channel
// subject: events.<gameID>.channel.<channelID>. Rejects wildcard tokens, empty
// tokens, and malformed shapes (InvalidArgument mapping at the caller).
func parseChannelSubject(subject string) (string, error) {
	parts := strings.Split(subject, ".")
	if len(parts) < 4 {
		return "", oops.Code("CHANNEL_AUDIT_SUBJECT_INVALID").With("subject", subject).
			Errorf("subject does not match events.<game>.channel.<id>")
	}
	if parts[0] != "events" || parts[2] != "channel" {
		return "", oops.Code("CHANNEL_AUDIT_SUBJECT_INVALID").With("subject", subject).
			Errorf("subject not owned by core-channels")
	}
	for _, p := range parts {
		if p == "" || strings.ContainsAny(p, "*>") {
			return "", oops.Code("CHANNEL_AUDIT_SUBJECT_INVALID").With("subject", subject).
				Errorf("empty or wildcard subject tokens not permitted for QueryHistory")
		}
	}
	return parts[3], nil
}

// channelActorProtoFromRow reconstructs an Actor proto from a stored row. Empty
// kind + empty id returns nil (system-origin events without a concrete actor).
func channelActorProtoFromRow(kind string, id []byte) *eventbusv1.Actor {
	if kind == "" && len(id) == 0 {
		return nil
	}
	return &eventbusv1.Actor{Kind: channelActorKindFromString(kind), Id: id}
}

// channelActorKindFromString maps the stored string back to the proto enum.
// Both enum String() form ("ACTOR_KIND_PLAYER") and the lowercase variant
// ("player") round-trip; unknown values fall through to UNSPECIFIED.
func channelActorKindFromString(s string) eventbusv1.ActorKind {
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
