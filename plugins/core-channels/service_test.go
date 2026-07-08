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
