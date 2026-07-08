// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"sync"
	"time"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	channelv1 "github.com/holomush/holomush/pkg/proto/holomush/channel/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

// createRateResource is the sentinel resource ref passed to the host evaluator
// for the create gate. No channel instance exists at create time, so a
// non-empty placeholder id is used; the host requires `type:id` with both
// halves non-empty (pluginauthz.splitResourceRef). The admin-create policy
// references only `principal.character.roles`, so no channel attribute is
// resolved for this ref (the resolver is never called).
const createRateResource = "channel:new"

// createRateWindow is the fixed sliding window for the per-player create rate
// limit (D-06: N creations per player per hour).
const createRateWindow = time.Hour

// channelServiceStorer is the narrow persistence dependency of channelService.
// The concrete *channelStore satisfies it; tests substitute a fake. The service
// deliberately does NOT depend on the wider store surface — join/leave
// visibility is enforced by the host ABAC read gate, not a service-side
// membership read.
type channelServiceStorer interface {
	CreateChannel(ctx context.Context, row *channelRow) error
	JoinChannel(ctx context.Context, channelID, characterID string) error
	LeaveChannel(ctx context.Context, channelID, characterID string) error
	ListForCharacter(ctx context.Context, characterID string) ([]channelRow, error)
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

// CreateChannel is UNIMPLEMENTED in the RED skeleton.
func (s *channelService) CreateChannel(_ context.Context, _ *channelv1.CreateChannelRequest) (*channelv1.CreateChannelResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented") //nolint:wrapcheck // RED skeleton
}

// JoinChannel is UNIMPLEMENTED in the RED skeleton.
func (s *channelService) JoinChannel(_ context.Context, _ *channelv1.JoinChannelRequest) (*channelv1.JoinChannelResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented") //nolint:wrapcheck // RED skeleton
}

// LeaveChannel is UNIMPLEMENTED in the RED skeleton.
func (s *channelService) LeaveChannel(_ context.Context, _ *channelv1.LeaveChannelRequest) (*channelv1.LeaveChannelResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented") //nolint:wrapcheck // RED skeleton
}

// ListChannels is UNIMPLEMENTED in the RED skeleton.
func (s *channelService) ListChannels(_ context.Context, _ *channelv1.ListChannelsRequest) (*channelv1.ListChannelsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented") //nolint:wrapcheck // RED skeleton
}
