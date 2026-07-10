// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/samber/oops"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/pkg/errutil"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	channelv1 "github.com/holomush/holomush/pkg/proto/holomush/channel/v1"
)

// adminCreatePolicyID is the fully-qualified id of the admin-default channel
// creation policy declared in plugin.yaml as `seed-channel-admin-create`. The
// installer scopes every plugin policy id as `plugin:<name>:<policy-name>`
// (internal/plugin/policy_installer.go:95), so the id the host reports in
// EvaluateDecision.MatchedPolicy for an admin create is this constant. A create
// authorised by THIS policy is an admin create and bypasses the per-player rate
// limit (D-06); a create authorised by any other (operator-granted) policy is a
// non-admin create and is rate-limited.
const adminCreatePolicyID = "plugin:core-channels:seed-channel-admin-create"

// createSentinelResourceID is the placeholder channel id used at create time,
// before any channel row exists. The host requires a `type:id` ref with both
// halves non-empty (pluginauthz.splitResourceRef). Under the real seeded ABAC
// engine the ChannelResolver IS invoked for this ref, so it special-cases this
// id to an empty attribute bag (resolver.go) rather than a fail-closed
// CHANNEL_NOT_FOUND — the admin-create policy references only
// principal.character.roles. A real channel id is a ULID and never collides.
const createSentinelResourceID = "new"

// createRateResource is the sentinel resource ref passed to the host evaluator
// for the create gate. No channel instance exists at create time, so the
// createSentinelResourceID placeholder is used. The admin-create policy
// references only `principal.character.roles`; the resolver resolves the
// sentinel to an empty attribute bag (see resolver.go).
const createRateResource = "channel:" + createSentinelResourceID

// createRateWindow is the fixed sliding window for the per-player create rate
// limit (D-06: N creations per player per hour).
const createRateWindow = time.Hour

// channelServiceStorer is the narrow persistence dependency of channelService.
// The concrete *channelStore satisfies it; tests substitute a fake. Structural
// join/leave visibility is enforced by the host ABAC read gate; the moderation
// and read verbs (01-05b) additionally consult the plugin-owned membership store
// for the muted flag, the roster, and the who-membership gate.
type channelServiceStorer interface {
	CreateChannel(ctx context.Context, row *channelRow) error
	JoinChannel(ctx context.Context, channelID, characterID string) error
	LeaveChannel(ctx context.Context, channelID, characterID string) error
	ListForCharacter(ctx context.Context, characterID string) ([]channelRow, error)
	// Moderation + read surface (01-05b).
	SetMuted(ctx context.Context, channelID, characterID string, muted bool) error
	SetBanned(ctx context.Context, channelID, characterID string, banned bool) error
	KickMember(ctx context.Context, channelID, actorID, targetID string) error
	TransferOwnership(ctx context.Context, channelID, actorID, newOwnerID string) error
	ListMembers(ctx context.Context, channelID string) ([]channelMemberRow, error)
	IsMuted(ctx context.Context, channelID, characterID string) (bool, error)
	// MembershipForHistory is the who-membership gate for WhoInChannel: reports
	// whether characterID is an active member (existence-oracle-safe — a missing
	// channel and a non-member both return false).
	MembershipForHistory(ctx context.Context, channelID, characterID string) (bool, time.Time, error)
}

// channelHistoryFetcher is the membership-gated history read QueryChannelHistory
// delegates to. *ChannelAuditServer satisfies it via HistoryForMember, which
// shares the SAME membership fence (authorizeMember) as the streaming
// PluginAuditService.QueryHistory — history authorization stays in one place
// (01-06). The service NEVER re-implements the auth.
type channelHistoryFetcher interface {
	HistoryForMember(ctx context.Context, subject, channelID, callerCharID string, limit int) ([]channelLogRow, error)
}

// trustedOwningPlayerKey is the private context key carrying the host-vouched
// owning-player id used to bucket the create rate limit (R2-C). It is a
// plugin-local dispatch-context binding: the command layer (01-07) stamps it
// from the host-vouched pluginsdk.CommandRequest.PlayerID before delegating
// CreateChannel; a future typed-RPC/web-BFF path reuses the same seam once it
// surfaces BeginServiceDispatch's ownerPlayerID. It is NEVER read from a
// client-supplied proto field (CreateChannelRequest carries no player_id).
type trustedOwningPlayerKey struct{}

// withTrustedOwningPlayer binds the host-vouched owning-player id to ctx for the
// create rate limiter. Called by the command layer with req.PlayerID.
func withTrustedOwningPlayer(ctx context.Context, playerID string) context.Context {
	return context.WithValue(ctx, trustedOwningPlayerKey{}, playerID)
}

// trustedOwningPlayerFromContext recovers the host-vouched owning-player id.
// Returns ("", false) when absent or empty — the caller MUST fail closed
// (never bucket into an empty/shared key) for a non-admin create (R2-C).
func trustedOwningPlayerFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(trustedOwningPlayerKey{}).(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// createRateLimiter is an in-memory fixed-window per-player limiter for channel
// creation (D-06). It keys ONLY on the host-vouched owning-player id. The clock
// is injectable so tests can exercise the boundary and window rollover
// deterministically.
type createRateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	now    func() time.Time
	events map[string][]time.Time
}

// newCreateRateLimiter builds a limiter allowing limit creations per player per
// window. now defaults to time.Now when nil.
func newCreateRateLimiter(limit int, window time.Duration, now func() time.Time) *createRateLimiter {
	if now == nil {
		now = time.Now
	}
	return &createRateLimiter{
		limit:  limit,
		window: window,
		now:    now,
		events: make(map[string][]time.Time),
	}
}

// allow reports whether player may create a channel now, recording the creation
// when it does. It prunes timestamps older than the window, then admits (and
// records) only if the in-window count is below the limit.
func (l *createRateLimiter) allow(player string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	cutoff := now.Add(-l.window)
	kept := l.events[player][:0]
	for _, t := range l.events[player] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.limit {
		l.events[player] = kept
		return false
	}
	kept = append(kept, now)
	l.events[player] = kept
	return true
}

// relativeChannelStream maps a channel id to its domain-RELATIVE session-stream
// reference "channel.<id>" — the form BOTH QuerySessionStreams (establishment)
// and the mid-session AddStream/RemoveStream calls pass to the host (R2-A).
//
// It is DELIBERATELY NOT the emit-path dotStyleChannelSubject(gameID, id): the
// host owns qualification. It prepends events.<game>. via eventbus.Qualify
// (computeInitialFilters at establishment, applyFilterCtrl mid-session), so the
// qualified filter resolves to the SAME events.<game>.channel.<id> subject the
// emit path publishes on. Passing a pre-qualified "events." subject to the
// stream.subscription capability is rejected by 01-02's AuthorizeStreamSubscribe
// (STREAM_NOT_RELATIVE); the relative "channel" domain is core-channels' OWN
// declared emit domain, so the shared fence permits the plugin's own join.
func relativeChannelStream(channelID string) string {
	return "channel." + channelID
}

// channelService implements channelv1.ChannelServiceServer. This plan
// implements ONLY the structural operations create/join/leave/list; the
// remaining RPCs (post/who/history/invite/mute/ban/kick/transfer) remain
// UnimplementedChannelServiceServer stubs until 01-05b fills them on the SAME
// type. Each implemented RPC self-enforces ABAC via the host evaluator BEFORE
// mutating the store (INV-SCENE-65 analog), independent of the telnet command
// layer, and never trusts client-supplied identity.
type channelService struct {
	channelv1.UnimplementedChannelServiceServer
	store     channelServiceStorer
	evaluator pluginsdk.HostEvaluator
	limiter   *createRateLimiter
	// eventSink + emitter drive the live content/notice emit path (CHAN-03).
	// SetEventSink runs before Init in the SDK lifecycle; main.go builds the
	// emitter once the sink + gameID are known. The emit-consuming RPCs
	// (PostToChannel / moderation notices) land in 01-05b and reuse emitter.
	eventSink pluginsdk.EventSink
	gameID    string
	emitter   *channelEventEmitter
	// history is the membership-gated audit history read QueryChannelHistory
	// delegates to (01-06 ChannelAuditServer). Wired in Init; nil until then, so
	// QueryChannelHistory fails closed with Internal.
	history channelHistoryFetcher
	// streamSub is the host stream.subscription client used to add/remove a live
	// session's channel subscription mid-session (01-08). Wired via
	// channelPlugin.SetStreamSubscription (StreamSubscriptionAware) at Init. Nil
	// until then, and nil in out-of-session contexts — join/leave degrade
	// gracefully (delivery falls back to the next session-establishment refresh)
	// rather than failing the structural mutation.
	streamSub pluginsdk.StreamSubscription
}

// SetStreamSubscription installs the host stream.subscription client used for
// mid-session join/leave live delivery (LIVE_ONLY). Wired via
// channelPlugin.SetStreamSubscription (StreamSubscriptionAware) at Init.
func (s *channelService) SetStreamSubscription(sub pluginsdk.StreamSubscription) {
	s.streamSub = sub
}

// subscribeLive starts live delivery of channelID's stream to sessionID with
// LIVE_ONLY (no history flood, T-01-09) after a join commits. The stream arg is
// the domain-RELATIVE channel.<id> (R2-A) — a pre-qualified events. subject is
// rejected by 01-02's AuthorizeStreamSubscribe (STREAM_NOT_RELATIVE); the shared
// fence permits core-channels' own `channel` domain. A missing session id
// (out-of-session action) or an unwired client is skipped. A subscription error
// is LOGGED but NOT propagated: the membership is already committed, so failing
// the join would be misleading; delivery degrades to the next
// session-establishment refresh (QuerySessionStreams) — never silently dropped
// (graceful-degradation floor, holomush-l6std).
func (s *channelService) subscribeLive(ctx context.Context, sessionID, channelID string) {
	if sessionID == "" || s.streamSub == nil {
		return
	}
	if err := s.streamSub.AddStream(ctx, sessionID, relativeChannelStream(channelID), pluginsdk.ReplayModeLiveOnly); err != nil {
		errutil.LogErrorContext(ctx, "channel.service.join_channel live subscribe degraded; delivery falls back to next session establishment", err,
			"channel_id", channelID, "session_id", sessionID)
	}
}

// unsubscribeLive stops live delivery of channelID's stream to sessionID after a
// leave commits, using the same domain-RELATIVE channel.<id> form (R2-A). Skips
// a missing session id or unwired client; logs (does not propagate) an error so
// a stale subscription surfaces without failing the leave.
func (s *channelService) unsubscribeLive(ctx context.Context, sessionID, channelID string) {
	if sessionID == "" || s.streamSub == nil {
		return
	}
	if err := s.streamSub.RemoveStream(ctx, sessionID, relativeChannelStream(channelID)); err != nil {
		errutil.LogErrorContext(ctx, "channel.service.leave_channel live unsubscribe degraded", err,
			"channel_id", channelID, "session_id", sessionID)
	}
}

// NewChannelService builds a service backed by store with the given per-player
// create rate limit. The evaluator is wired later via SetHostEvaluator (before
// Init, mirroring scenes); until then all gated RPCs fail closed.
func NewChannelService(store channelServiceStorer, createRateLimit int, now func() time.Time) *channelService {
	return &channelService{
		store:   store,
		limiter: newCreateRateLimiter(createRateLimit, createRateWindow, now),
	}
}

// SetHostEvaluator installs the host ABAC evaluator used by every RPC's
// self-enforced authorization. Wired via channelPlugin.SetHostEvaluator before
// Init; nil until then (all gated RPCs fail closed).
func (s *channelService) SetHostEvaluator(ev pluginsdk.HostEvaluator) {
	s.evaluator = ev
}

// SetEventSink stores the SDK-injected event sink so the service can emit live
// channel content + notice events (CHAN-03). Wired via
// channelPlugin.SetEventSink before Init (emit is fence-self-gated, exempt from
// capability declaration). main.go builds the emitter after gameID is known.
func (s *channelService) SetEventSink(sink pluginsdk.EventSink) {
	s.eventSink = sink
}

// actorMismatch reports whether the host-vouched character actor on ctx
// contradicts the request's acting character id. A mismatch means a caller
// authenticated as one character is trying to act as another; reject fail-closed
// (mirrors core-scenes service.go:272). Absent metadata is not a mismatch — the
// host dispatch token remains the outer identity gate.
func actorMismatch(ctx context.Context, characterID string) bool {
	kind, id, ok := pluginsdk.ActorMetadataFromIncomingContext(ctx)
	return ok && kind == pluginsdk.ActorCharacter && id != characterID
}

// gateRead self-enforces the ABAC `read` (visibility) gate for a per-channel
// RPC BEFORE any store mutation. The read policies (01-04) admit a member of any
// channel and any character to a public channel; a private/admin non-member is
// denied. A denied read collapses to a uniform codes.NotFound so join/leave
// cannot be used to probe for a hidden channel's existence (T-01-12). A nil
// evaluator or an engine error fails closed.
func (s *channelService) gateRead(ctx context.Context, span trace.Span, channelID, op string) error {
	if s.evaluator == nil {
		slog.WarnContext(ctx, "channel service read gate: evaluator not configured", "op", op, "channel_id", channelID)
		return status.Error(codes.Internal, "permission check unavailable") //nolint:wrapcheck // opaque per grpc-errors.md
	}
	dec, err := s.evaluator.Evaluate(ctx, "read", "channel:"+channelID)
	if err != nil {
		recordError(span, err)
		errutil.LogErrorContext(ctx, "channel service read gate: evaluation failed", err, "op", op)
		return status.Error(codes.Internal, "internal error") //nolint:wrapcheck // opaque per grpc-errors.md
	}
	if !dec.Allowed {
		return status.Error(codes.NotFound, "channel not found") //nolint:wrapcheck // uniform hidden/absent per T-01-12
	}
	return nil
}

// CreateChannel allocates a new channel owned by the calling character. It
// self-enforces the admin-gated create policy (D-06) via the host evaluator,
// then applies the per-player rate limit keyed on the host-vouched
// owning-player id from the trusted dispatch binding (R2-C) — admins (create
// authorised by adminCreatePolicyID) bypass the limit. A create with no trusted
// owning-player id fails closed for a non-admin. The name is validated, the type
// defaults to public, and the creator becomes owner.
func (s *channelService) CreateChannel(ctx context.Context, req *channelv1.CreateChannelRequest) (*channelv1.CreateChannelResponse, error) {
	ctx, span := startSpan(ctx, "channel.service.create_channel",
		attribute.String("subject_id", req.GetCharacterId()))
	defer span.End()

	// Actor-binding identity cross-check: a caller authenticated as one
	// character cannot create a channel owned by another (req.character_id
	// becomes the owner). Mirrors core-scenes CreateScene.
	if actorMismatch(ctx, req.GetCharacterId()) {
		slog.WarnContext(ctx, "channel.service.create_channel actor metadata mismatch",
			"request_character_id", req.GetCharacterId())
		return nil, status.Error(codes.PermissionDenied, "not permitted to create for this character") //nolint:wrapcheck // opaque per grpc-errors.md
	}

	// Input validation: trim + regex. protovalidate enforces this at the host
	// boundary, but a direct handler caller (and the command layer) bypasses it.
	name := strings.TrimSpace(req.GetName())
	if !validateChannelName(name) {
		return nil, status.Error(codes.InvalidArgument, "channel name does not match the accepted pattern") //nolint:wrapcheck // opaque per grpc-errors.md
	}

	// Self-enforced ABAC create gate (INV-SCENE-65 analog). Fail closed when the
	// evaluator is not wired.
	if s.evaluator == nil {
		slog.WarnContext(ctx, "channel.service.create_channel evaluator not configured",
			"subject_id", req.GetCharacterId())
		return nil, status.Error(codes.Internal, "permission check unavailable") //nolint:wrapcheck // opaque per grpc-errors.md
	}
	dec, evalErr := s.evaluator.Evaluate(ctx, "create", createRateResource)
	if evalErr != nil {
		recordError(span, evalErr)
		errutil.LogErrorContext(ctx, "channel.service.create_channel evaluation failed", evalErr)
		return nil, status.Error(codes.Internal, "internal error") //nolint:wrapcheck // opaque per grpc-errors.md
	}
	if !dec.Allowed {
		return nil, status.Error(codes.PermissionDenied, "not permitted to create channels") //nolint:wrapcheck // opaque per grpc-errors.md
	}

	// Per-player create rate limit (D-06). Admins bypass; a non-admin create
	// keys ONLY on the trusted owning-player id and fails closed when absent
	// (R2-C — never bucket into an empty/shared key nor allow through).
	if dec.MatchedPolicy != adminCreatePolicyID {
		player, ok := trustedOwningPlayerFromContext(ctx)
		if !ok {
			slog.WarnContext(ctx,
				"channel.service.create_channel denied: no trusted owning-player id (fail-closed, R2-C)",
				"subject_id", req.GetCharacterId())
			return nil, status.Error(codes.PermissionDenied, "not permitted to create channels") //nolint:wrapcheck // opaque per grpc-errors.md
		}
		if !s.limiter.allow(player) {
			return nil, status.Error(codes.ResourceExhausted, "channel creation rate limit exceeded") //nolint:wrapcheck // opaque per grpc-errors.md
		}
	}

	// Default the type here (not only in the store) so the persisted row and the
	// response projection agree even when the store does not backfill (CHAN-04).
	channelType := req.GetType()
	if channelType == "" {
		channelType = string(channelTypePublic)
	}
	row := &channelRow{
		Name:    name,
		Type:    channelType,
		OwnerID: req.GetCharacterId(),
	}
	if rd := req.GetRetentionDays(); rd > 0 {
		v := int(rd)
		row.RetentionDays = &v
	}
	if err := s.store.CreateChannel(ctx, row); err != nil {
		return nil, mapStoreError(ctx, span, err, "channel.service.create_channel")
	}
	span.SetAttributes(attribute.String("channel_id", row.ID))

	slog.InfoContext(ctx, "channel.service.create_channel ok",
		"subject_id", req.GetCharacterId(), "channel_id", row.ID, "type", row.Type)

	now := time.Now().UTC()
	members := []*channelv1.MemberInfo{{
		CharacterId:   row.OwnerID,
		CharacterName: row.OwnerID, // best-effort: no name resolver wired
		Role:          "owner",
		JoinedAt:      timestamppb.New(now),
	}}
	return &channelv1.CreateChannelResponse{Channel: rowToChannelInfo(row, members, now)}, nil
}

// JoinChannel adds the calling character to a channel. A public channel admits
// any character; a private/admin channel admits only a member (the read gate
// denies a non-member, yielding a uniform not-found). A banned character is
// refused; a repeat join by an existing member is an idempotent success.
func (s *channelService) JoinChannel(ctx context.Context, req *channelv1.JoinChannelRequest) (*channelv1.JoinChannelResponse, error) {
	ctx, span := startSpan(ctx, "channel.service.join_channel",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("channel_id", req.GetChannelId()))
	defer span.End()

	if actorMismatch(ctx, req.GetCharacterId()) {
		slog.WarnContext(ctx, "channel.service.join_channel actor metadata mismatch",
			"request_character_id", req.GetCharacterId(), "channel_id", req.GetChannelId())
		return nil, status.Error(codes.PermissionDenied, "not permitted to join for this character") //nolint:wrapcheck // opaque per grpc-errors.md
	}

	if err := s.gateRead(ctx, span, req.GetChannelId(), "channel.service.join_channel"); err != nil {
		return nil, err
	}

	if err := s.store.JoinChannel(ctx, req.GetChannelId(), req.GetCharacterId()); err != nil {
		return nil, mapStoreError(ctx, span, err, "channel.service.join_channel")
	}

	// Start live delivery mid-session (LIVE_ONLY) after the membership commits.
	s.subscribeLive(ctx, req.GetSessionId(), req.GetChannelId())

	slog.InfoContext(ctx, "channel.service.join_channel ok",
		"subject_id", req.GetCharacterId(), "channel_id", req.GetChannelId())
	return &channelv1.JoinChannelResponse{}, nil
}

// LeaveChannel removes the calling character's membership. The read gate yields
// a uniform not-found for a hidden channel; the owner cannot leave
// (FailedPrecondition); leaving a channel the caller is not in returns the same
// uniform not-found.
func (s *channelService) LeaveChannel(ctx context.Context, req *channelv1.LeaveChannelRequest) (*channelv1.LeaveChannelResponse, error) {
	ctx, span := startSpan(ctx, "channel.service.leave_channel",
		attribute.String("subject_id", req.GetCharacterId()),
		attribute.String("channel_id", req.GetChannelId()))
	defer span.End()

	if actorMismatch(ctx, req.GetCharacterId()) {
		slog.WarnContext(ctx, "channel.service.leave_channel actor metadata mismatch",
			"request_character_id", req.GetCharacterId(), "channel_id", req.GetChannelId())
		return nil, status.Error(codes.PermissionDenied, "not permitted to leave for this character") //nolint:wrapcheck // opaque per grpc-errors.md
	}

	if err := s.gateRead(ctx, span, req.GetChannelId(), "channel.service.leave_channel"); err != nil {
		return nil, err
	}

	if err := s.store.LeaveChannel(ctx, req.GetChannelId(), req.GetCharacterId()); err != nil {
		return nil, mapStoreError(ctx, span, err, "channel.service.leave_channel")
	}

	// Stop live delivery mid-session after the membership is removed.
	s.unsubscribeLive(ctx, req.GetSessionId(), req.GetChannelId())

	slog.InfoContext(ctx, "channel.service.leave_channel ok",
		"subject_id", req.GetCharacterId(), "channel_id", req.GetChannelId())
	return &channelv1.LeaveChannelResponse{}, nil
}

// ListChannels returns exactly the channels the calling character is an active
// (non-banned, non-archived) member of (CHAN-01) — a self-scoped membership
// query (ListForCharacter), never a world/location query. The ABAC
// self-enforcement here is the actor-identity binding: a caller can only list
// their own memberships (the query is keyed on the host-vouched character), so
// the listing cannot probe another character's or a hidden channel's membership.
func (s *channelService) ListChannels(ctx context.Context, req *channelv1.ListChannelsRequest) (*channelv1.ListChannelsResponse, error) {
	ctx, span := startSpan(ctx, "channel.service.list_channels",
		attribute.String("subject_id", req.GetCharacterId()))
	defer span.End()

	if actorMismatch(ctx, req.GetCharacterId()) {
		slog.WarnContext(ctx, "channel.service.list_channels actor metadata mismatch",
			"request_character_id", req.GetCharacterId())
		return nil, status.Error(codes.PermissionDenied, "not permitted to list for this character") //nolint:wrapcheck // opaque per grpc-errors.md
	}

	rows, err := s.store.ListForCharacter(ctx, req.GetCharacterId())
	if err != nil {
		return nil, mapStoreError(ctx, span, err, "channel.service.list_channels")
	}

	out := make([]*channelv1.ChannelInfo, 0, len(rows))
	for i := range rows {
		out = append(out, rowToChannelInfo(&rows[i], nil, rows[i].CreatedAt.Time()))
	}
	return &channelv1.ListChannelsResponse{Channels: out}, nil
}

// rowToChannelInfo projects a channelRow into the wire ChannelInfo. members may
// be nil for list results (roster is fetched via WhoInChannel, 01-05b). created
// is the timestamp to stamp when the row's own CreatedAt is not populated (e.g.
// fresh from CreateChannel, whose store call does not return the DB timestamp).
func rowToChannelInfo(row *channelRow, members []*channelv1.MemberInfo, created time.Time) *channelv1.ChannelInfo {
	ts := created
	if !row.CreatedAt.IsZero() {
		ts = row.CreatedAt.Time()
	}
	retention := int32(0)
	if row.RetentionDays != nil {
		retention = int32(*row.RetentionDays) //nolint:gosec // retention days is a small config-bounded value
	}
	return &channelv1.ChannelInfo{
		Id:            row.ID,
		Name:          row.Name,
		Type:          row.Type,
		OwnerId:       row.OwnerID,
		Archived:      row.Archived,
		RetentionDays: retention,
		CreatedAt:     timestamppb.New(ts),
		Members:       members,
	}
}

// mapStoreError translates plugin store oops codes to opaque gRPC status errors
// with generic messages, logging inner detail via errutil (grpc-errors.md). A
// hidden or absent channel collapses to a uniform codes.NotFound so operations
// cannot be used to probe for a channel's existence (T-01-12).
func mapStoreError(ctx context.Context, span trace.Span, err error, op string) error {
	recordError(span, err)
	var oe oops.OopsError
	if errors.As(err, &oe) {
		switch oe.Code() {
		case "CHANNEL_NOT_FOUND", "CHANNEL_MEMBERSHIP_NOT_FOUND":
			return status.Error(codes.NotFound, "channel not found") //nolint:wrapcheck // opaque per grpc-errors.md
		case "CHANNEL_NAME_TAKEN":
			return status.Error(codes.AlreadyExists, "a channel with that name already exists") //nolint:wrapcheck // opaque per grpc-errors.md
		case "CHANNEL_NAME_INVALID", "CHANNEL_TYPE_INVALID", "CHANNEL_OWNER_REQUIRED":
			return status.Error(codes.InvalidArgument, "invalid channel request") //nolint:wrapcheck // opaque per grpc-errors.md
		case "CHANNEL_OWNER_CANNOT_LEAVE":
			return status.Error(codes.FailedPrecondition, "channel owners cannot leave; transfer ownership or archive") //nolint:wrapcheck // opaque per grpc-errors.md
		case "CHANNEL_OWNER_CANNOT_KICK":
			return status.Error(codes.FailedPrecondition, "the channel owner cannot be kicked; transfer ownership first") //nolint:wrapcheck // opaque per grpc-errors.md
		case "CHANNEL_TRANSFER_TARGET_NOT_MEMBER":
			return status.Error(codes.FailedPrecondition, "the new owner must already be a member of the channel") //nolint:wrapcheck // opaque per grpc-errors.md
		case "CHANNEL_BANNED":
			return status.Error(codes.PermissionDenied, "not permitted to join this channel") //nolint:wrapcheck // opaque per grpc-errors.md
		}
	}
	errutil.LogErrorContext(ctx, "channel service: store error", err, "op", op)
	return status.Error(codes.Internal, "internal error") //nolint:wrapcheck // opaque per grpc-errors.md
}
