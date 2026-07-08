// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	channelv1 "github.com/holomush/holomush/pkg/proto/holomush/channel/v1"
)

const (
	testCharID   = "char-01"
	testPlayerID = "player-01"
	// grantPolicyID stands in for an operator-added create grant to a
	// non-admin principal (distinct from adminCreatePolicyID). A create
	// authorised by this policy is rate-limited (D-06).
	grantPolicyID = "plugin:core-channels:operator-grant-create"
	readPolicyID  = "plugin:core-channels:read-channel-as-member"
)

// fakeServiceStore is a controllable channelServiceStorer for unit tests.
type fakeServiceStore struct {
	createErr, joinErr, leaveErr, listErr error
	created                               []*channelRow
	joined                                [][2]string
	left                                  [][2]string
	listFor                               map[string][]channelRow

	// 01-05b moderation + read surface.
	muteErr, banErr, kickErr    error
	transferErr, listMembersErr error
	isMutedErr, membershipErr   error
	setMuted, setBanned         [][2]string // {channelID, targetID}
	kicked, transferred         [][2]string // {channelID, target/newOwner}
	members                     map[string][]channelMemberRow
	mutedMap                    map[string]bool      // key channelID|characterID
	memberMap                   map[string]time.Time // key channelID|characterID ⇒ member (value = joined_at)
}

func (f *fakeServiceStore) CreateChannel(_ context.Context, row *channelRow) error {
	if f.createErr != nil {
		return f.createErr
	}
	if row.ID == "" {
		row.ID = "channel-" + row.Name
	}
	cp := *row
	f.created = append(f.created, &cp)
	return nil
}

func (f *fakeServiceStore) JoinChannel(_ context.Context, channelID, characterID string) error {
	if f.joinErr != nil {
		return f.joinErr
	}
	f.joined = append(f.joined, [2]string{channelID, characterID})
	return nil
}

func (f *fakeServiceStore) LeaveChannel(_ context.Context, channelID, characterID string) error {
	if f.leaveErr != nil {
		return f.leaveErr
	}
	f.left = append(f.left, [2]string{channelID, characterID})
	return nil
}

func (f *fakeServiceStore) ListForCharacter(_ context.Context, characterID string) ([]channelRow, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listFor[characterID], nil
}

func (f *fakeServiceStore) SetMuted(_ context.Context, channelID, characterID string, muted bool) error {
	if f.muteErr != nil {
		return f.muteErr
	}
	if muted {
		f.setMuted = append(f.setMuted, [2]string{channelID, characterID})
	}
	return nil
}

func (f *fakeServiceStore) SetBanned(_ context.Context, channelID, characterID string, banned bool) error {
	if f.banErr != nil {
		return f.banErr
	}
	if banned {
		f.setBanned = append(f.setBanned, [2]string{channelID, characterID})
	}
	return nil
}

func (f *fakeServiceStore) KickMember(_ context.Context, channelID, _, targetID string) error {
	if f.kickErr != nil {
		return f.kickErr
	}
	f.kicked = append(f.kicked, [2]string{channelID, targetID})
	return nil
}

func (f *fakeServiceStore) TransferOwnership(_ context.Context, channelID, _, newOwnerID string) error {
	if f.transferErr != nil {
		return f.transferErr
	}
	f.transferred = append(f.transferred, [2]string{channelID, newOwnerID})
	return nil
}

func (f *fakeServiceStore) ListMembers(_ context.Context, channelID string) ([]channelMemberRow, error) {
	if f.listMembersErr != nil {
		return nil, f.listMembersErr
	}
	return f.members[channelID], nil
}

func (f *fakeServiceStore) IsMuted(_ context.Context, channelID, characterID string) (bool, error) {
	if f.isMutedErr != nil {
		return false, f.isMutedErr
	}
	return f.mutedMap[channelID+"|"+characterID], nil
}

func (f *fakeServiceStore) MembershipForHistory(_ context.Context, channelID, characterID string) (bool, time.Time, error) {
	if f.membershipErr != nil {
		return false, time.Time{}, f.membershipErr
	}
	ts, ok := f.memberMap[channelID+"|"+characterID]
	return ok, ts, nil
}

// fakeHistory is a controllable channelHistoryFetcher recording its arguments.
type fakeHistory struct {
	rows       []channelLogRow
	err        error
	gotSubject string
	gotChannel string
	gotCaller  string
	gotLimit   int
}

func (f *fakeHistory) HistoryForMember(_ context.Context, subject, channelID, callerCharID string, limit int) ([]channelLogRow, error) {
	f.gotSubject, f.gotChannel, f.gotCaller, f.gotLimit = subject, channelID, callerCharID, limit
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

// newFullServiceForTest builds a service with an emitter (backed by a capturing
// sink) and a game id, for the 01-05b content/notice-emitting RPCs.
func newFullServiceForTest(store channelServiceStorer, ev pluginsdk.HostEvaluator) (*channelService, *fakeEventSink) {
	s := newServiceForTest(store, 5, nil, ev)
	sink := &fakeEventSink{}
	s.gameID = testGameID
	s.emitter = newChannelEventEmitter(sink, testGameID)
	return s, sink
}

const testTargetID = "char-target-02"

// fakeEvaluator is a controllable HostEvaluator for unit tests.
type fakeEvaluator struct {
	allowed bool
	matched string
	err     error
}

func (f fakeEvaluator) Evaluate(_ context.Context, _, _ string) (pluginsdk.EvaluateDecision, error) {
	if f.err != nil {
		return pluginsdk.EvaluateDecision{}, f.err
	}
	return pluginsdk.EvaluateDecision{Allowed: f.allowed, MatchedPolicy: f.matched}, nil
}

var (
	adminEvaluator = fakeEvaluator{allowed: true, matched: adminCreatePolicyID}
	grantEvaluator = fakeEvaluator{allowed: true, matched: grantPolicyID}
	allowEvaluator = fakeEvaluator{allowed: true, matched: readPolicyID}
	denyEvaluator  = fakeEvaluator{allowed: false}
)

func newServiceForTest(store channelServiceStorer, limit int, now func() time.Time, ev pluginsdk.HostEvaluator) *channelService {
	s := NewChannelService(store, limit, now)
	s.SetHostEvaluator(ev)
	return s
}

// actorCtx builds an incoming gRPC context carrying host-vouched actor metadata.
//
//nolint:unparam // kind is kept general (the metadata carries any actor kind); current callers all assert ActorCharacter mismatches.
func actorCtx(kind pluginsdk.ActorKind, id string) context.Context {
	md := metadata.New(map[string]string{
		"x-holomush-actor-kind": strconv.Itoa(int(kind)),
		"x-holomush-actor-id":   id,
	})
	return metadata.NewIncomingContext(context.Background(), md)
}

// trustedCtx binds a host-vouched owning-player id for the create rate limiter.
func trustedCtx(playerID string) context.Context {
	return withTrustedOwningPlayer(context.Background(), playerID)
}

// ── CreateChannel ─────────────────────────────────────────────────────────

func TestCreateChannelRejectsInvalidNameWithInvalidArgument(t *testing.T) {
	svc := newServiceForTest(&fakeServiceStore{}, 5, nil, adminEvaluator)
	_, err := svc.CreateChannel(context.Background(), &channelv1.CreateChannelRequest{
		CharacterId: testCharID,
		Name:        "bad name!",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestCreateChannelNilEvaluatorFailsClosed(t *testing.T) {
	svc := NewChannelService(&fakeServiceStore{}, 5, nil) // no evaluator wired
	_, err := svc.CreateChannel(context.Background(), &channelv1.CreateChannelRequest{
		CharacterId: testCharID,
		Name:        "General",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestCreateChannelNonAdminDenied(t *testing.T) {
	svc := newServiceForTest(&fakeServiceStore{}, 5, nil, denyEvaluator)
	_, err := svc.CreateChannel(context.Background(), &channelv1.CreateChannelRequest{
		CharacterId: testCharID,
		Name:        "General",
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestCreateChannelAdminPermittedPersistsOwnerAndDefaultType(t *testing.T) {
	store := &fakeServiceStore{}
	svc := newServiceForTest(store, 5, nil, adminEvaluator)
	resp, err := svc.CreateChannel(context.Background(), &channelv1.CreateChannelRequest{
		CharacterId: testCharID,
		Name:        "General",
	})
	require.NoError(t, err)
	require.Len(t, store.created, 1)
	assert.Equal(t, testCharID, store.created[0].OwnerID)
	assert.Equal(t, string(channelTypePublic), store.created[0].Type, "empty type defaults to public")
	require.NotNil(t, resp.GetChannel())
	assert.Equal(t, "General", resp.GetChannel().GetName())
	assert.Equal(t, testCharID, resp.GetChannel().GetOwnerId())
	// The creator is projected as the sole owner member.
	require.Len(t, resp.GetChannel().GetMembers(), 1)
	assert.Equal(t, testCharID, resp.GetChannel().GetMembers()[0].GetCharacterId())
	assert.Equal(t, "owner", resp.GetChannel().GetMembers()[0].GetRole())
}

func TestCreateChannelPersistsPrivateType(t *testing.T) {
	store := &fakeServiceStore{}
	svc := newServiceForTest(store, 5, nil, adminEvaluator)
	resp, err := svc.CreateChannel(context.Background(), &channelv1.CreateChannelRequest{
		CharacterId: testCharID,
		Name:        "Council",
		Type:        string(channelTypePrivate),
	})
	require.NoError(t, err)
	require.Len(t, store.created, 1)
	assert.Equal(t, string(channelTypePrivate), store.created[0].Type)
	assert.Equal(t, string(channelTypePrivate), resp.GetChannel().GetType())
}

func TestCreateChannelAdminBypassesRateLimit(t *testing.T) {
	store := &fakeServiceStore{}
	svc := newServiceForTest(store, 5, nil, adminEvaluator)
	// 7 > limit of 5, but admin bypasses; no trusted player id needed.
	for i := 0; i < 7; i++ {
		_, err := svc.CreateChannel(context.Background(), &channelv1.CreateChannelRequest{
			CharacterId: testCharID,
			Name:        "Ch" + strconv.Itoa(i),
		})
		require.NoError(t, err, "admin create %d must bypass the rate limit", i)
	}
	assert.Len(t, store.created, 7)
}

func TestCreateChannelNonAdminRateLimitedOnTrustedPlayer(t *testing.T) {
	store := &fakeServiceStore{}
	svc := newServiceForTest(store, 5, nil, grantEvaluator)
	ctx := trustedCtx(testPlayerID)
	for i := 0; i < 5; i++ {
		_, err := svc.CreateChannel(ctx, &channelv1.CreateChannelRequest{
			CharacterId: testCharID,
			Name:        "Ch" + strconv.Itoa(i),
		})
		require.NoError(t, err, "non-admin create %d within limit must succeed", i)
	}
	_, err := svc.CreateChannel(ctx, &channelv1.CreateChannelRequest{
		CharacterId: testCharID,
		Name:        "Overflow",
	})
	require.Error(t, err, "the 6th create must be denied")
	assert.Equal(t, codes.ResourceExhausted, status.Code(err))
	assert.Len(t, store.created, 5)
}

func TestCreateChannelRateLimitBucketsPerPlayer(t *testing.T) {
	store := &fakeServiceStore{}
	svc := newServiceForTest(store, 2, nil, grantEvaluator)
	ctxA := trustedCtx("player-A")
	ctxB := trustedCtx("player-B")

	for i := 0; i < 2; i++ {
		_, err := svc.CreateChannel(ctxA, &channelv1.CreateChannelRequest{CharacterId: testCharID, Name: "A" + strconv.Itoa(i)})
		require.NoError(t, err)
	}
	_, err := svc.CreateChannel(ctxA, &channelv1.CreateChannelRequest{CharacterId: testCharID, Name: "AOverflow"})
	require.Error(t, err)
	assert.Equal(t, codes.ResourceExhausted, status.Code(err))

	// player-B has an independent bucket and is unaffected.
	for i := 0; i < 2; i++ {
		_, err := svc.CreateChannel(ctxB, &channelv1.CreateChannelRequest{CharacterId: testCharID, Name: "B" + strconv.Itoa(i)})
		require.NoError(t, err, "player-B bucket is independent of player-A")
	}
}

func TestCreateChannelRateLimitWindowRollover(t *testing.T) {
	store := &fakeServiceStore{}
	clock := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(store, 1, func() time.Time { return clock }, grantEvaluator)
	ctx := trustedCtx(testPlayerID)

	_, err := svc.CreateChannel(ctx, &channelv1.CreateChannelRequest{CharacterId: testCharID, Name: "First"})
	require.NoError(t, err)
	_, err = svc.CreateChannel(ctx, &channelv1.CreateChannelRequest{CharacterId: testCharID, Name: "Second"})
	require.Error(t, err, "second create in the same window is denied")
	assert.Equal(t, codes.ResourceExhausted, status.Code(err))

	// Advance past the window; the bucket rolls over.
	clock = clock.Add(createRateWindow + time.Minute)
	_, err = svc.CreateChannel(ctx, &channelv1.CreateChannelRequest{CharacterId: testCharID, Name: "Third"})
	require.NoError(t, err, "create after the window rollover must succeed")
}

func TestCreateChannelNonAdminWithoutTrustedPlayerFailsClosed(t *testing.T) {
	store := &fakeServiceStore{}
	svc := newServiceForTest(store, 5, nil, grantEvaluator)
	// No trusted owning-player id bound to ctx: a non-admin create must fail
	// closed (never bucket into an empty key nor bypass) — R2-C.
	_, err := svc.CreateChannel(context.Background(), &channelv1.CreateChannelRequest{
		CharacterId: testCharID,
		Name:        "General",
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Empty(t, store.created, "no channel is created when the owning-player id is absent")
}

func TestCreateChannelActorMismatchDenied(t *testing.T) {
	svc := newServiceForTest(&fakeServiceStore{}, 5, nil, adminEvaluator)
	ctx := actorCtx(pluginsdk.ActorCharacter, "char-other")
	_, err := svc.CreateChannel(ctx, &channelv1.CreateChannelRequest{
		CharacterId: testCharID,
		Name:        "General",
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestCreateChannelNameTakenReturnsAlreadyExists(t *testing.T) {
	store := &fakeServiceStore{createErr: oops.Code("CHANNEL_NAME_TAKEN").Errorf("taken")}
	svc := newServiceForTest(store, 5, nil, adminEvaluator)
	_, err := svc.CreateChannel(context.Background(), &channelv1.CreateChannelRequest{
		CharacterId: testCharID,
		Name:        "General",
	})
	require.Error(t, err)
	assert.Equal(t, codes.AlreadyExists, status.Code(err))
}

// ── JoinChannel ───────────────────────────────────────────────────────────

func TestJoinChannelPublicAdmitsCharacter(t *testing.T) {
	store := &fakeServiceStore{}
	svc := newServiceForTest(store, 5, nil, allowEvaluator)
	_, err := svc.JoinChannel(context.Background(), &channelv1.JoinChannelRequest{
		CharacterId: testCharID,
		ChannelId:   "channel-general",
		SessionId:   "sess-01",
	})
	require.NoError(t, err)
	require.Len(t, store.joined, 1)
	assert.Equal(t, [2]string{"channel-general", testCharID}, store.joined[0])
}

func TestJoinChannelPrivateNonInviteeReturnsNotFound(t *testing.T) {
	store := &fakeServiceStore{}
	svc := newServiceForTest(store, 5, nil, denyEvaluator) // read denied → cannot see
	_, err := svc.JoinChannel(context.Background(), &channelv1.JoinChannelRequest{
		CharacterId: testCharID,
		ChannelId:   "channel-council",
		SessionId:   "sess-01",
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
	assert.Empty(t, store.joined, "a hidden channel must not be mutated")
}

func TestJoinChannelAbsentReturnsNotFound(t *testing.T) {
	store := &fakeServiceStore{joinErr: oops.Code("CHANNEL_NOT_FOUND").Errorf("absent")}
	svc := newServiceForTest(store, 5, nil, allowEvaluator)
	_, err := svc.JoinChannel(context.Background(), &channelv1.JoinChannelRequest{
		CharacterId: testCharID,
		ChannelId:   "channel-missing",
		SessionId:   "sess-01",
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestJoinChannelIdempotentForMember(t *testing.T) {
	// The store's JoinChannel is a no-op for an existing member (returns nil).
	store := &fakeServiceStore{}
	svc := newServiceForTest(store, 5, nil, allowEvaluator)
	_, err := svc.JoinChannel(context.Background(), &channelv1.JoinChannelRequest{
		CharacterId: testCharID,
		ChannelId:   "channel-general",
		SessionId:   "sess-01",
	})
	require.NoError(t, err)
}

func TestJoinChannelActorMismatchDenied(t *testing.T) {
	svc := newServiceForTest(&fakeServiceStore{}, 5, nil, allowEvaluator)
	ctx := actorCtx(pluginsdk.ActorCharacter, "char-other")
	_, err := svc.JoinChannel(ctx, &channelv1.JoinChannelRequest{
		CharacterId: testCharID,
		ChannelId:   "channel-general",
		SessionId:   "sess-01",
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestJoinChannelNilEvaluatorFailsClosed(t *testing.T) {
	svc := NewChannelService(&fakeServiceStore{}, 5, nil)
	_, err := svc.JoinChannel(context.Background(), &channelv1.JoinChannelRequest{
		CharacterId: testCharID,
		ChannelId:   "channel-general",
		SessionId:   "sess-01",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

// ── LeaveChannel ──────────────────────────────────────────────────────────

func TestLeaveChannelMemberSucceeds(t *testing.T) {
	store := &fakeServiceStore{}
	svc := newServiceForTest(store, 5, nil, allowEvaluator)
	_, err := svc.LeaveChannel(context.Background(), &channelv1.LeaveChannelRequest{
		CharacterId: testCharID,
		ChannelId:   "channel-general",
		SessionId:   "sess-01",
	})
	require.NoError(t, err)
	require.Len(t, store.left, 1)
	assert.Equal(t, [2]string{"channel-general", testCharID}, store.left[0])
}

func TestLeaveChannelNonMemberReturnsUniformNotFound(t *testing.T) {
	store := &fakeServiceStore{leaveErr: oops.Code("CHANNEL_MEMBERSHIP_NOT_FOUND").Errorf("not a member")}
	svc := newServiceForTest(store, 5, nil, allowEvaluator)
	_, err := svc.LeaveChannel(context.Background(), &channelv1.LeaveChannelRequest{
		CharacterId: testCharID,
		ChannelId:   "channel-general",
		SessionId:   "sess-01",
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestLeaveChannelOwnerCannotLeave(t *testing.T) {
	store := &fakeServiceStore{leaveErr: oops.Code("CHANNEL_OWNER_CANNOT_LEAVE").Errorf("owner")}
	svc := newServiceForTest(store, 5, nil, allowEvaluator)
	_, err := svc.LeaveChannel(context.Background(), &channelv1.LeaveChannelRequest{
		CharacterId: testCharID,
		ChannelId:   "channel-general",
		SessionId:   "sess-01",
	})
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

func TestLeaveChannelHiddenReturnsNotFound(t *testing.T) {
	store := &fakeServiceStore{}
	svc := newServiceForTest(store, 5, nil, denyEvaluator) // read denied → cannot see
	_, err := svc.LeaveChannel(context.Background(), &channelv1.LeaveChannelRequest{
		CharacterId: testCharID,
		ChannelId:   "channel-council",
		SessionId:   "sess-01",
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
	assert.Empty(t, store.left)
}

// ── ListChannels ──────────────────────────────────────────────────────────

func TestListChannelsReturnsCallerMemberships(t *testing.T) {
	store := &fakeServiceStore{listFor: map[string][]channelRow{
		testCharID: {
			{ID: "channel-general", Name: "General", Type: string(channelTypePublic), OwnerID: "sys"},
			{ID: "channel-council", Name: "Council", Type: string(channelTypePrivate), OwnerID: testCharID},
		},
	}}
	svc := newServiceForTest(store, 5, nil, allowEvaluator)
	resp, err := svc.ListChannels(context.Background(), &channelv1.ListChannelsRequest{CharacterId: testCharID})
	require.NoError(t, err)
	require.Len(t, resp.GetChannels(), 2)
	assert.Equal(t, "channel-general", resp.GetChannels()[0].GetId())
	assert.Equal(t, "channel-council", resp.GetChannels()[1].GetId())
}

func TestListChannelsActorMismatchDenied(t *testing.T) {
	svc := newServiceForTest(&fakeServiceStore{}, 5, nil, allowEvaluator)
	ctx := actorCtx(pluginsdk.ActorCharacter, "char-other")
	_, err := svc.ListChannels(ctx, &channelv1.ListChannelsRequest{CharacterId: testCharID})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

// ── 01-05b: moderation RPCs (Invite/Mute/Ban/Kick/Transfer) ────────────────

func TestInviteToChannelNonOwnerDeniedUniformNotFound(t *testing.T) {
	store := &fakeServiceStore{}
	svc, _ := newFullServiceForTest(store, denyEvaluator)
	_, err := svc.InviteToChannel(context.Background(), &channelv1.InviteToChannelRequest{
		CharacterId: testCharID, ChannelId: "ch-1", TargetCharacterId: testTargetID,
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err), "a denied caller cannot distinguish hidden vs absent")
	assert.Empty(t, store.joined, "no membership recorded when denied")
}

func TestInviteToChannelOwnerAdmitsTargetAndEmitsJoin(t *testing.T) {
	store := &fakeServiceStore{}
	svc, sink := newFullServiceForTest(store, allowEvaluator)
	_, err := svc.InviteToChannel(context.Background(), &channelv1.InviteToChannelRequest{
		CharacterId: testCharID, ChannelId: "ch-1", TargetCharacterId: testTargetID,
	})
	require.NoError(t, err)
	require.Len(t, store.joined, 1)
	assert.Equal(t, [2]string{"ch-1", testTargetID}, store.joined[0], "invite adds the TARGET as a member")
	require.Len(t, sink.intents, 1)
	assert.Equal(t, channelJoinType, sink.intents[0].Type)
}

func TestMuteMemberNonOwnerDeniedUniformNotFound(t *testing.T) {
	store := &fakeServiceStore{}
	svc, _ := newFullServiceForTest(store, denyEvaluator)
	_, err := svc.MuteMember(context.Background(), &channelv1.MuteMemberRequest{
		CharacterId: testCharID, ChannelId: "ch-1", TargetCharacterId: testTargetID,
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
	assert.Empty(t, store.setMuted)
}

func TestMuteMemberOwnerMutesAndEmits(t *testing.T) {
	store := &fakeServiceStore{}
	svc, sink := newFullServiceForTest(store, allowEvaluator)
	_, err := svc.MuteMember(context.Background(), &channelv1.MuteMemberRequest{
		CharacterId: testCharID, ChannelId: "ch-1", TargetCharacterId: testTargetID,
	})
	require.NoError(t, err)
	require.Len(t, store.setMuted, 1)
	assert.Equal(t, [2]string{"ch-1", testTargetID}, store.setMuted[0])
	require.Len(t, sink.intents, 1)
	assert.Equal(t, channelMuteType, sink.intents[0].Type)
}

func TestMuteMemberAdminOverridePermitted(t *testing.T) {
	// The plugin only observes Allowed — the owner-vs-admin distinction is the
	// host's; an admin-override permit reaches the service as allowed=true.
	store := &fakeServiceStore{}
	svc, _ := newFullServiceForTest(store, fakeEvaluator{allowed: true, matched: "plugin:core-channels:admin-override-channel"})
	_, err := svc.MuteMember(context.Background(), &channelv1.MuteMemberRequest{
		CharacterId: "admin-char", ChannelId: "ch-1", TargetCharacterId: testTargetID,
	})
	require.NoError(t, err)
	require.Len(t, store.setMuted, 1)
}

func TestBanMemberOwnerBansAndEmits(t *testing.T) {
	store := &fakeServiceStore{}
	svc, sink := newFullServiceForTest(store, allowEvaluator)
	_, err := svc.BanMember(context.Background(), &channelv1.BanMemberRequest{
		CharacterId: testCharID, ChannelId: "ch-1", TargetCharacterId: testTargetID,
	})
	require.NoError(t, err)
	require.Len(t, store.setBanned, 1)
	assert.Equal(t, [2]string{"ch-1", testTargetID}, store.setBanned[0])
	require.Len(t, sink.intents, 1)
	assert.Equal(t, channelBanType, sink.intents[0].Type)
}

func TestKickMemberOwnerKicksAndEmits(t *testing.T) {
	store := &fakeServiceStore{}
	svc, sink := newFullServiceForTest(store, allowEvaluator)
	_, err := svc.KickMember(context.Background(), &channelv1.KickMemberRequest{
		CharacterId: testCharID, ChannelId: "ch-1", TargetCharacterId: testTargetID,
	})
	require.NoError(t, err)
	require.Len(t, store.kicked, 1)
	assert.Equal(t, [2]string{"ch-1", testTargetID}, store.kicked[0])
	require.Len(t, sink.intents, 1)
	assert.Equal(t, channelKickType, sink.intents[0].Type)
}

func TestKickMemberOwnerCannotBeKicked(t *testing.T) {
	store := &fakeServiceStore{kickErr: oops.Code("CHANNEL_OWNER_CANNOT_KICK").Errorf("owner")}
	svc, _ := newFullServiceForTest(store, allowEvaluator)
	_, err := svc.KickMember(context.Background(), &channelv1.KickMemberRequest{
		CharacterId: testCharID, ChannelId: "ch-1", TargetCharacterId: "the-owner",
	})
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

func TestKickMemberTargetNotMemberUniformNotFound(t *testing.T) {
	store := &fakeServiceStore{kickErr: oops.Code("CHANNEL_MEMBERSHIP_NOT_FOUND").Errorf("absent")}
	svc, _ := newFullServiceForTest(store, allowEvaluator)
	_, err := svc.KickMember(context.Background(), &channelv1.KickMemberRequest{
		CharacterId: testCharID, ChannelId: "ch-1", TargetCharacterId: "ghost",
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestTransferOwnershipToMemberSucceedsAndEmits(t *testing.T) {
	store := &fakeServiceStore{}
	svc, sink := newFullServiceForTest(store, allowEvaluator)
	_, err := svc.TransferOwnership(context.Background(), &channelv1.TransferOwnershipRequest{
		CharacterId: testCharID, ChannelId: "ch-1", NewOwnerCharacterId: testTargetID,
	})
	require.NoError(t, err)
	require.Len(t, store.transferred, 1)
	assert.Equal(t, [2]string{"ch-1", testTargetID}, store.transferred[0])
	require.Len(t, sink.intents, 1)
	assert.Equal(t, channelRenameType, sink.intents[0].Type)
}

func TestTransferOwnershipToNonMemberRejected(t *testing.T) {
	store := &fakeServiceStore{transferErr: oops.Code("CHANNEL_TRANSFER_TARGET_NOT_MEMBER").Errorf("not a member")}
	svc, _ := newFullServiceForTest(store, allowEvaluator)
	_, err := svc.TransferOwnership(context.Background(), &channelv1.TransferOwnershipRequest{
		CharacterId: testCharID, ChannelId: "ch-1", NewOwnerCharacterId: "outsider",
	})
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

func TestTransferOwnershipNonOwnerDeniedUniformNotFound(t *testing.T) {
	store := &fakeServiceStore{}
	svc, _ := newFullServiceForTest(store, denyEvaluator)
	_, err := svc.TransferOwnership(context.Background(), &channelv1.TransferOwnershipRequest{
		CharacterId: testCharID, ChannelId: "ch-1", NewOwnerCharacterId: testTargetID,
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
	assert.Empty(t, store.transferred)
}

func TestModerationActorMismatchDenied(t *testing.T) {
	store := &fakeServiceStore{}
	svc, _ := newFullServiceForTest(store, allowEvaluator)
	ctx := actorCtx(pluginsdk.ActorCharacter, "char-other")
	_, err := svc.MuteMember(ctx, &channelv1.MuteMemberRequest{
		CharacterId: testCharID, ChannelId: "ch-1", TargetCharacterId: testTargetID,
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Empty(t, store.setMuted)
}

// ── 01-05b: content + read RPCs (Post/Who/History) ─────────────────────────

func TestPostToChannelMemberEmitsSayNoChannelName(t *testing.T) {
	store := &fakeServiceStore{}
	svc, sink := newFullServiceForTest(store, allowEvaluator)
	_, err := svc.PostToChannel(context.Background(), &channelv1.PostToChannelRequest{
		CharacterId: testCharID, ChannelId: "ch-1", Text: "hello channel",
	})
	require.NoError(t, err)
	require.Len(t, sink.intents, 1)
	assert.Equal(t, channelSayType, sink.intents[0].Type)
	assert.False(t, sink.intents[0].Sensitive, "channel content is plaintext (D-04)")
	assertNoChannelNameField(t, sink.intents[0].Payload)
}

func TestPostToChannelPoseKind(t *testing.T) {
	store := &fakeServiceStore{}
	svc, sink := newFullServiceForTest(store, allowEvaluator)
	_, err := svc.PostToChannel(context.Background(), &channelv1.PostToChannelRequest{
		CharacterId: testCharID, ChannelId: "ch-1", Kind: "pose", Text: "waves",
	})
	require.NoError(t, err)
	require.Len(t, sink.intents, 1)
	assert.Equal(t, channelPoseType, sink.intents[0].Type)
}

func TestPostToChannelNonMemberDeniedUniformNotFound(t *testing.T) {
	store := &fakeServiceStore{}
	svc, sink := newFullServiceForTest(store, denyEvaluator)
	_, err := svc.PostToChannel(context.Background(), &channelv1.PostToChannelRequest{
		CharacterId: testCharID, ChannelId: "ch-1", Text: "hi",
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
	assert.Empty(t, sink.intents)
}

func TestPostToChannelMutedMemberDenied(t *testing.T) {
	store := &fakeServiceStore{mutedMap: map[string]bool{"ch-1|" + testCharID: true}}
	svc, sink := newFullServiceForTest(store, allowEvaluator)
	_, err := svc.PostToChannel(context.Background(), &channelv1.PostToChannelRequest{
		CharacterId: testCharID, ChannelId: "ch-1", Text: "hi",
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err), "a muted member is a member — not an existence oracle")
	assert.Empty(t, sink.intents)
}

func TestPostToChannelOOCRejected(t *testing.T) {
	store := &fakeServiceStore{}
	svc, _ := newFullServiceForTest(store, allowEvaluator)
	_, err := svc.PostToChannel(context.Background(), &channelv1.PostToChannelRequest{
		CharacterId: testCharID, ChannelId: "ch-1", Kind: "ooc", Text: "hi",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestPostToChannelEmptyTextRejected(t *testing.T) {
	store := &fakeServiceStore{}
	svc, _ := newFullServiceForTest(store, allowEvaluator)
	_, err := svc.PostToChannel(context.Background(), &channelv1.PostToChannelRequest{
		CharacterId: testCharID, ChannelId: "ch-1", Text: "   ",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestWhoInChannelMemberReturnsRoster(t *testing.T) {
	now := time.Now()
	store := &fakeServiceStore{
		memberMap: map[string]time.Time{"ch-1|" + testCharID: now},
		members: map[string][]channelMemberRow{"ch-1": {
			{CharacterID: testCharID, Role: "owner", JoinedAt: now},
			{CharacterID: testTargetID, Role: "member", Muted: true, JoinedAt: now},
		}},
	}
	svc, _ := newFullServiceForTest(store, allowEvaluator)
	resp, err := svc.WhoInChannel(context.Background(), &channelv1.WhoInChannelRequest{
		CharacterId: testCharID, ChannelId: "ch-1",
	})
	require.NoError(t, err)
	require.Len(t, resp.GetMembers(), 2)
	assert.Equal(t, testCharID, resp.GetMembers()[0].GetCharacterId())
	assert.Equal(t, "owner", resp.GetMembers()[0].GetRole())
	assert.True(t, resp.GetMembers()[1].GetMuted())
}

func TestWhoInChannelNonMemberUniformNotFound(t *testing.T) {
	store := &fakeServiceStore{} // memberMap empty ⇒ non-member
	svc, _ := newFullServiceForTest(store, allowEvaluator)
	_, err := svc.WhoInChannel(context.Background(), &channelv1.WhoInChannelRequest{
		CharacterId: testCharID, ChannelId: "ch-1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestQueryChannelHistoryMemberReturnsEntries(t *testing.T) {
	hist := &fakeHistory{rows: []channelLogRow{{
		id:        make([]byte, 16),
		eventType: string(channelSayType),
		timestamp: time.Now(),
		payload:   []byte(`{"actor_id":"char-01","actor_display_name":"Alice","text":"hi"}`),
	}}}
	svc, _ := newFullServiceForTest(&fakeServiceStore{}, allowEvaluator)
	svc.history = hist
	resp, err := svc.QueryChannelHistory(context.Background(), &channelv1.QueryChannelHistoryRequest{
		CharacterId: testCharID, ChannelId: "ch-1", Limit: 25,
	})
	require.NoError(t, err)
	require.Len(t, resp.GetEntries(), 1)
	assert.Equal(t, string(channelSayType), resp.GetEntries()[0].GetType())
	assert.Equal(t, "hi", resp.GetEntries()[0].GetContent())
	assert.Equal(t, "Alice", resp.GetEntries()[0].GetActorName())
	// The service builds the qualified subject + forwards the limit to the fence.
	assert.Equal(t, dotStyleChannelSubject(testGameID, "ch-1"), hist.gotSubject)
	assert.Equal(t, "ch-1", hist.gotChannel)
	assert.Equal(t, testCharID, hist.gotCaller)
	assert.Equal(t, 25, hist.gotLimit)
}

func TestQueryChannelHistoryNonMemberUniformNotFound(t *testing.T) {
	hist := &fakeHistory{err: status.Error(codes.PermissionDenied, "not a member")}
	svc, _ := newFullServiceForTest(&fakeServiceStore{}, allowEvaluator)
	svc.history = hist
	_, err := svc.QueryChannelHistory(context.Background(), &channelv1.QueryChannelHistoryRequest{
		CharacterId: testCharID, ChannelId: "ch-1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err), "the fence's PermissionDenied is presented as a uniform not-found")
}

func TestQueryChannelHistoryNilFetcherFailsClosed(t *testing.T) {
	svc, _ := newFullServiceForTest(&fakeServiceStore{}, allowEvaluator)
	svc.history = nil
	_, err := svc.QueryChannelHistory(context.Background(), &channelv1.QueryChannelHistoryRequest{
		CharacterId: testCharID, ChannelId: "ch-1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}
