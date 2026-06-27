// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// recordingPublishEventer is a publishEventer that records emitAttemptsExtended
// calls (the only method the E1 tests assert) and no-ops the rest via the
// embedded noopPublishEventer.
type recordingPublishEventer struct {
	noopPublishEventer
	extended []extendedCall
	// Per-event-type call counters for the D3 transition-wiring test.
	voteCastCount  int
	coolOffCount   int
	resolvedCount  int
	withdrawnCount int
	lastResolved   PublishedSceneStatus
}

type extendedCall struct {
	sceneID, adminID   string
	additional, newMax int
}

func (r *recordingPublishEventer) emitAttemptsExtended(_ context.Context, sceneID, adminID string, additional, newMax int) error {
	r.extended = append(r.extended, extendedCall{sceneID, adminID, additional, newMax})
	return nil
}

func (r *recordingPublishEventer) emitVoteCast(_ context.Context, _, _ string, _ *CastVoteResult) error {
	r.voteCastCount++
	return nil
}

func (r *recordingPublishEventer) emitCoolOffStarted(_ context.Context, _ string, _ time.Duration) error {
	r.coolOffCount++
	return nil
}

func (r *recordingPublishEventer) emitResolved(_ context.Context, _ string, finalStatus PublishedSceneStatus, _ *PublishFailureReason, _ *VoteTally) error {
	r.resolvedCount++
	r.lastResolved = finalStatus
	return nil
}

func (r *recordingPublishEventer) emitWithdrawn(_ context.Context, _, _ string) error {
	r.withdrawnCount++
	return nil
}

// startPublishFixture wires a SceneServiceImpl over a fakeStore seeded with a
// single scene in the given state, returning the store, service, and the
// scene/caller ULIDs the tests assert against.
func startPublishFixture(t *testing.T, state SceneState) (*fakeStore, *SceneServiceImpl, string, string) {
	t.Helper()
	sceneID := ulid.Make().String()
	callerID := ulid.Make().String()
	store := newFakeStore()
	store.scenes[sceneID] = &SceneRow{
		ID:      sceneID,
		OwnerID: callerID,
		State:   string(state),
	}
	svc := newTestService(t, store)
	return store, svc, sceneID, callerID
}

func TestStartScenePublishCreatesAttemptForEndedSceneWithinBudget(t *testing.T) {
	t.Parallel()
	store, svc, sceneID, callerID := startPublishFixture(t, SceneStateEnded)
	store.maxPublishAttempts[sceneID] = 3
	store.attemptCounts[sceneID] = AttemptCounts{Total: 0, Active: 0, Published: 0}

	resp, err := svc.StartScenePublish(context.Background(), &scenev1.StartScenePublishRequest{
		SceneId:           sceneID,
		CallerCharacterId: callerID,
	})

	require.NoError(t, err)
	assert.NotEmpty(t, resp.GetPublishedSceneId())
	assert.Equal(t, int32(1), resp.GetAttemptNumber())
	require.Len(t, store.createdAttempts, 1)
	assert.Equal(t, callerID, store.createdAttempts[0].InitiatedBy)
}

func TestStartScenePublishRejectsSceneNotInEndedState(t *testing.T) {
	t.Parallel()
	store, svc, sceneID, callerID := startPublishFixture(t, SceneStateActive)
	store.maxPublishAttempts[sceneID] = 3

	_, err := svc.StartScenePublish(context.Background(), &scenev1.StartScenePublishRequest{
		SceneId:           sceneID,
		CallerCharacterId: callerID,
	})

	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Equal(t, "SCENE_PUBLISH_INVALID_STATE", status.Convert(err).Message())
	assert.Empty(t, store.createdAttempts)
}

func TestStartScenePublishRejectsSceneWithExistingPublishedArchive(t *testing.T) {
	t.Parallel()
	store, svc, sceneID, callerID := startPublishFixture(t, SceneStateEnded)
	store.maxPublishAttempts[sceneID] = 3
	store.attemptCounts[sceneID] = AttemptCounts{Total: 1, Active: 0, Published: 1}

	_, err := svc.StartScenePublish(context.Background(), &scenev1.StartScenePublishRequest{
		SceneId:           sceneID,
		CallerCharacterId: callerID,
	})

	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Equal(t, "SCENE_PUBLISH_ALREADY_PUBLISHED", status.Convert(err).Message())
	assert.Empty(t, store.createdAttempts)
}

func TestStartScenePublishRejectsSceneWithActiveAttempt(t *testing.T) {
	t.Parallel()
	store, svc, sceneID, callerID := startPublishFixture(t, SceneStateEnded)
	store.maxPublishAttempts[sceneID] = 3
	store.attemptCounts[sceneID] = AttemptCounts{Total: 1, Active: 1, Published: 0}

	_, err := svc.StartScenePublish(context.Background(), &scenev1.StartScenePublishRequest{
		SceneId:           sceneID,
		CallerCharacterId: callerID,
	})

	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Equal(t, "SCENE_PUBLISH_ALREADY_ACTIVE", status.Convert(err).Message())
	assert.Empty(t, store.createdAttempts)
}

func TestStartScenePublishRejectsSceneThatExhaustedItsAttemptBudget(t *testing.T) {
	t.Parallel()
	store, svc, sceneID, callerID := startPublishFixture(t, SceneStateEnded)
	store.maxPublishAttempts[sceneID] = 3
	store.attemptCounts[sceneID] = AttemptCounts{Total: 3, Active: 0, Published: 0}

	_, err := svc.StartScenePublish(context.Background(), &scenev1.StartScenePublishRequest{
		SceneId:           sceneID,
		CallerCharacterId: callerID,
	})

	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Equal(t, "SCENE_PUBLISH_ATTEMPTS_EXHAUSTED", status.Convert(err).Message())
	assert.Empty(t, store.createdAttempts)
}

func TestStartScenePublishReturnsNotFoundForMissingScene(t *testing.T) {
	t.Parallel()
	store := newFakeStore() // no scene installed → store.Get returns SCENE_NOT_FOUND
	svc := newTestService(t, store)

	_, err := svc.StartScenePublish(context.Background(), &scenev1.StartScenePublishRequest{
		SceneId:           ulid.Make().String(),
		CallerCharacterId: ulid.Make().String(),
	})

	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err), "a missing scene MUST map to NotFound, not Internal")
	assert.Equal(t, "SCENE_NOT_FOUND", status.Convert(err).Message())
	assert.Empty(t, store.createdAttempts)
}

// TestExtendScenePublishVoteAttemptsBumpsBudgetAndEmits covers the E1 happy
// path: the budget is bumped by `additional`, the new max is returned, and a
// scene_publish_vote_attempts_extended event fires carrying the admin id. The
// handler performs NO in-plugin admin check — admin is ABAC-gated by the host
// (spec §8), so this test does not (and cannot) assert a non-admin denial at
// the handler level; that is the host policy's job (verified by the policy
// declaration test below + at E2/integration).
func TestExtendScenePublishVoteAttemptsBumpsBudgetAndEmits(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	store.maxPublishAttempts["scene-e1"] = 3
	rec := &recordingPublishEventer{}
	svc := newTestService(t, store)
	svc.SetPublishEventer(rec)

	resp, err := svc.ExtendScenePublishVoteAttempts(context.Background(), &scenev1.ExtendScenePublishVoteAttemptsRequest{
		CallerCharacterId: "char-admin",
		SceneId:           "scene-e1",
		Additional:        2,
	})

	require.NoError(t, err)
	assert.Equal(t, int32(5), resp.GetNewMax(), "3 + 2 = 5")
	require.Len(t, rec.extended, 1, "exactly one scene_publish_vote_attempts_extended event must fire")
	assert.Equal(t, extendedCall{sceneID: "scene-e1", adminID: "char-admin", additional: 2, newMax: 5}, rec.extended[0])
}

// TestExtendScenePublishVoteAttemptsRejectsNonPositiveCount verifies a zero or
// negative extension is rejected with InvalidArgument before any write — it
// would otherwise no-op or decrement the budget. No event fires.
func TestExtendScenePublishVoteAttemptsRejectsNonPositiveCount(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	store.maxPublishAttempts["scene-e1"] = 3
	rec := &recordingPublishEventer{}
	svc := newTestService(t, store)
	svc.SetPublishEventer(rec)

	for _, additional := range []int32{0, -5} {
		_, err := svc.ExtendScenePublishVoteAttempts(context.Background(), &scenev1.ExtendScenePublishVoteAttemptsRequest{
			CallerCharacterId: "char-admin",
			SceneId:           "scene-e1",
			Additional:        additional,
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err), "non-positive additional must be InvalidArgument")
	}
	assert.Empty(t, rec.extended, "no event fires on a rejected extension")
	assert.Equal(t, 3, store.maxPublishAttempts["scene-e1"], "budget unchanged on rejection")
}

// TestExtendScenePublishVoteAttemptsPropagatesStoreNotFound verifies a missing
// scene surfaces as NotFound (not Internal) and no event fires.
func TestExtendScenePublishVoteAttemptsPropagatesStoreNotFound(t *testing.T) {
	t.Parallel()
	store := &notFoundExtendStore{fakeStore: newFakeStore()}
	rec := &recordingPublishEventer{}
	svc := newTestService(t, store)
	svc.SetPublishEventer(rec)

	_, err := svc.ExtendScenePublishVoteAttempts(context.Background(), &scenev1.ExtendScenePublishVoteAttemptsRequest{
		CallerCharacterId: "char-admin",
		SceneId:           "missing",
		Additional:        1,
	})

	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
	assert.Empty(t, rec.extended, "no event fires when the bump fails")
}

// notFoundExtendStore overrides ExtendMaxPublishAttempts to return the store's
// not-found code, exercising the handler's error mapping.
type notFoundExtendStore struct {
	*fakeStore
}

func (s *notFoundExtendStore) ExtendMaxPublishAttempts(_ context.Context, sceneID string, _ int) (int, error) {
	return 0, oops.Code("SCENE_PUBLISH_NOT_FOUND").With("scene_id", sceneID).Errorf("scene not found")
}

// TestPluginManifestDeclaresAdminExtendPublishAttemptsPolicy pins the ABAC
// policy that gates the RPC: admin-only via "admin" in principal.character.roles
// on the extend_publish_attempts action (spec §8). Enforcement is host-side at
// command dispatch (E2); this asserts the declaration is correct and present.
func TestPluginManifestDeclaresAdminExtendPublishAttemptsPolicy(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("plugin.yaml")
	require.NoError(t, err)
	manifest := string(data)

	assert.Contains(t, manifest, "admin-extend-publish-attempts", "the E1 ABAC policy must be declared")
	assert.Contains(t, manifest, `action in ["extend_publish_attempts"]`, "policy must gate the extend_publish_attempts action")
	assert.Contains(t, manifest, "principal is character", "policy principal must be a character")
	assert.Contains(t, manifest, "resource is scene", "policy must target scene resources")
	assert.Contains(t, manifest, `"admin" in principal.character.roles`, "policy must be admin-role-only")
}

// newVoteFixture seeds a COLLECTING attempt with the given voter roster and
// returns the store + service for the B3 vote-cast tests.
func newVoteFixture(t *testing.T, attemptID, sceneID string, voters ...string) (*fakeStore, *SceneServiceImpl) {
	t.Helper()
	store := newFakeStore()
	store.installPublishedAttempt(attemptID, sceneID, StatusCollecting)
	store.installVoters(attemptID, voters...)
	return store, newTestService(t, store)
}

func castVote(t *testing.T, svc *SceneServiceImpl, caller, attemptID string, vote bool) *scenev1.CastPublishSceneVoteResponse {
	t.Helper()
	resp, err := svc.CastPublishSceneVote(context.Background(), &scenev1.CastPublishSceneVoteRequest{
		CallerCharacterId: caller,
		PublishedSceneId:  attemptID,
		Vote:              vote,
	})
	require.NoError(t, err)
	return resp
}

// TestCastPublishSceneVoteFirstYesIsNotAChange — a roster member's first cast
// is recorded but is not a vote change.
func TestCastPublishSceneVoteFirstYesIsNotAChange(t *testing.T) {
	t.Parallel()
	v1, v2 := ulid.Make().String(), ulid.Make().String()
	_, svc := newVoteFixture(t, "pub-v1", "scene-v1", v1, v2)

	resp := castVote(t, svc, v1, "pub-v1", true)

	assert.False(t, resp.GetIsChange(), "a first cast is not a change")
}

// TestCastPublishSceneVoteFlipYesToNoIsAChange — re-casting a different vote
// reports is_change.
func TestCastPublishSceneVoteFlipYesToNoIsAChange(t *testing.T) {
	t.Parallel()
	v1, v2 := ulid.Make().String(), ulid.Make().String()
	_, svc := newVoteFixture(t, "pub-v2", "scene-v2", v1, v2)

	castVote(t, svc, v1, "pub-v2", true)
	resp := castVote(t, svc, v1, "pub-v2", false)

	assert.True(t, resp.GetIsChange(), "flipping yes→no is a change")
}

// TestCastPublishSceneVoteRejectsNonRosterMember — INV-SCENE-28: a character not on
// the frozen roster cannot vote (PermissionDenied / SCENE_PUBLISH_NOT_A_VOTER).
func TestCastPublishSceneVoteRejectsNonRosterMember(t *testing.T) {
	t.Parallel()
	v1 := ulid.Make().String()
	outsider := ulid.Make().String()
	_, svc := newVoteFixture(t, "pub-v3", "scene-v3", v1)

	_, err := svc.CastPublishSceneVote(context.Background(), &scenev1.CastPublishSceneVoteRequest{
		CallerCharacterId: outsider,
		PublishedSceneId:  "pub-v3",
		Vote:              true,
	})

	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Equal(t, "SCENE_PUBLISH_NOT_A_VOTER", status.Convert(err).Message())
}

// TestCastPublishSceneVoteRejectsTerminalAttempt — holomush-wn612: the handler's
// terminal-status pre-check (CastPublishSceneVote) rejects a vote on a resolved
// attempt with FailedPrecondition / SCENE_PUBLISH_INVALID_STATE, before reaching
// the store. A roster member is the caller, so the rejection is the status guard,
// not the non-voter guard (INV-SCENE-29 terminal boundary).
func TestCastPublishSceneVoteRejectsTerminalAttempt(t *testing.T) {
	t.Parallel()
	v1 := ulid.Make().String()
	store := newFakeStore()
	store.installPublishedAttempt("pub-term", "scene-term", StatusAttemptFailed)
	store.installVoters("pub-term", v1)
	svc := newTestService(t, store)

	_, err := svc.CastPublishSceneVote(context.Background(), &scenev1.CastPublishSceneVoteRequest{
		CallerCharacterId: v1,
		PublishedSceneId:  "pub-term",
		Vote:              true,
	})

	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Equal(t, "SCENE_PUBLISH_INVALID_STATE", status.Convert(err).Message())
}

// TestCastPublishSceneVoteTriggersCoolOffOnAllYes — unanimous yes (all voted)
// transitions COLLECTING→COOLOFF and stamps the cool-off marker; a partial
// tally does not transition.
func TestCastPublishSceneVoteTriggersCoolOffOnAllYes(t *testing.T) {
	t.Parallel()
	v1, v2 := ulid.Make().String(), ulid.Make().String()
	store, svc := newVoteFixture(t, "pub-v4", "scene-v4", v1, v2)

	castVote(t, svc, v1, "pub-v4", true)
	assert.Equal(t, StatusCollecting, store.publishedScenes["pub-v4"].Status,
		"one of two voters is not yet a resolution")

	castVote(t, svc, v2, "pub-v4", true)
	assert.Equal(t, StatusCoolOff, store.publishedScenes["pub-v4"].Status,
		"unanimous yes enters cool-off")
	assert.NotNil(t, store.publishedScenes["pub-v4"].CoolOffStartedAt, "cool-off marker is stamped")
}

// TestCastPublishSceneVoteTriggersAttemptFailedOnAnyNo — once all have voted, a
// single no fails the attempt with failure_reason ANY_NO.
func TestCastPublishSceneVoteTriggersAttemptFailedOnAnyNo(t *testing.T) {
	t.Parallel()
	v1, v2 := ulid.Make().String(), ulid.Make().String()
	store, svc := newVoteFixture(t, "pub-v5", "scene-v5", v1, v2)

	castVote(t, svc, v1, "pub-v5", true)
	castVote(t, svc, v2, "pub-v5", false)

	assert.Equal(t, StatusAttemptFailed, store.publishedScenes["pub-v5"].Status)
	require.NotNil(t, store.publishedScenes["pub-v5"].FailureReason)
	assert.Equal(t, FailureAnyNo, *store.publishedScenes["pub-v5"].FailureReason)
}

// TestCastPublishSceneVoteFlipToNoDuringCoolOffReturnsToCollecting — a no cast
// during COOLOFF flips the attempt back to COLLECTING and clears the cool-off
// marker (spec §4.1).
func TestCastPublishSceneVoteFlipToNoDuringCoolOffReturnsToCollecting(t *testing.T) {
	t.Parallel()
	v1, v2 := ulid.Make().String(), ulid.Make().String()
	store, svc := newVoteFixture(t, "pub-v6", "scene-v6", v1, v2)

	castVote(t, svc, v1, "pub-v6", true)
	castVote(t, svc, v2, "pub-v6", true)
	require.Equal(t, StatusCoolOff, store.publishedScenes["pub-v6"].Status, "precondition: in cool-off")

	castVote(t, svc, v1, "pub-v6", false)

	assert.Equal(t, StatusCollecting, store.publishedScenes["pub-v6"].Status,
		"a flip-to-no during cool-off reopens COLLECTING")
	assert.Nil(t, store.publishedScenes["pub-v6"].CoolOffStartedAt, "cool-off marker cleared on flip-back")
}

// newWithdrawFixture seeds an attempt in the given status plus a scene owned by
// `owner`, for the B4 withdraw tests.
func newWithdrawFixture(t *testing.T, attemptID, sceneID, owner string, status PublishedSceneStatus) (*fakeStore, *SceneServiceImpl) {
	t.Helper()
	store := newFakeStore()
	store.installPublishedAttempt(attemptID, sceneID, status)
	store.scenes[sceneID] = &SceneRow{ID: sceneID, OwnerID: owner, State: string(SceneStateEnded)}
	return store, newTestService(t, store)
}

// TestWithdrawScenePublishByOwnerFailsAttempt — the owner withdraws an active
// attempt, transitioning it to ATTEMPT_FAILED with failure_reason WITHDRAWN.
func TestWithdrawScenePublishByOwnerFailsAttempt(t *testing.T) {
	t.Parallel()
	owner := ulid.Make().String()
	store, svc := newWithdrawFixture(t, "pub-w1", "scene-w1", owner, StatusCollecting)

	_, err := svc.WithdrawScenePublish(context.Background(), &scenev1.WithdrawScenePublishRequest{
		CallerCharacterId: owner,
		PublishedSceneId:  "pub-w1",
	})

	require.NoError(t, err)
	assert.Equal(t, StatusAttemptFailed, store.publishedScenes["pub-w1"].Status)
	require.NotNil(t, store.publishedScenes["pub-w1"].FailureReason)
	assert.Equal(t, FailureWithdrawn, *store.publishedScenes["pub-w1"].FailureReason)
}

// TestWithdrawScenePublishRejectsNonOwner — a non-owner is denied with
// SCENE_PUBLISH_NOT_OWNER (PermissionDenied) and the attempt is unchanged
// (INV-SCENE-30: only the scene owner may withdraw an active attempt; participants
// opposed to publication must vote no, not withdraw).
func TestWithdrawScenePublishRejectsNonOwner(t *testing.T) {
	t.Parallel()
	owner := ulid.Make().String()
	outsider := ulid.Make().String()
	store, svc := newWithdrawFixture(t, "pub-w2", "scene-w2", owner, StatusCollecting)

	_, err := svc.WithdrawScenePublish(context.Background(), &scenev1.WithdrawScenePublishRequest{
		CallerCharacterId: outsider,
		PublishedSceneId:  "pub-w2",
	})

	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Equal(t, "SCENE_PUBLISH_NOT_OWNER", status.Convert(err).Message())
	assert.Equal(t, StatusCollecting, store.publishedScenes["pub-w2"].Status, "a denied withdraw must not change state")
}

// TestWithdrawScenePublishRejectsTerminalAttempt — withdrawing an already-
// terminal attempt is a FailedPrecondition (SCENE_PUBLISH_INVALID_STATE).
func TestWithdrawScenePublishRejectsTerminalAttempt(t *testing.T) {
	t.Parallel()
	owner := ulid.Make().String()
	_, svc := newWithdrawFixture(t, "pub-w3", "scene-w3", owner, StatusPublished)

	_, err := svc.WithdrawScenePublish(context.Background(), &scenev1.WithdrawScenePublishRequest{
		CallerCharacterId: owner,
		PublishedSceneId:  "pub-w3",
	})

	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Equal(t, "SCENE_PUBLISH_INVALID_STATE", status.Convert(err).Message())
}

// TestWithdrawScenePublishRejectsUnknownAttempt covers the nil-header branch: a
// nonexistent attempt id surfaces as NotFound (SCENE_PUBLISH_NOT_FOUND).
func TestWithdrawScenePublishRejectsUnknownAttempt(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, newFakeStore())

	_, err := svc.WithdrawScenePublish(context.Background(), &scenev1.WithdrawScenePublishRequest{
		CallerCharacterId: ulid.Make().String(),
		PublishedSceneId:  "nonexistent",
	})

	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
	assert.Equal(t, "SCENE_PUBLISH_NOT_FOUND", status.Convert(err).Message())
}

// TestPluginManifestDeclaresWithdrawPublishAsOwnerPolicy pins the B4 ABAC
// policy: owner-only via resource.scene.owner == principal.id on the
// withdraw_publish action (spec §8).
func TestPluginManifestDeclaresWithdrawPublishAsOwnerPolicy(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("plugin.yaml")
	require.NoError(t, err)
	manifest := string(data)

	assert.Contains(t, manifest, "withdraw-publish-as-owner", "the B4 ABAC policy must be declared")
	assert.Contains(t, manifest, `action in ["withdraw_publish"]`, "policy must gate the withdraw_publish action")
	assert.Contains(t, manifest, "principal is character", "policy principal must be a character")
	assert.Contains(t, manifest, "resource is scene", "policy must target scene resources")
	assert.Contains(t, manifest, "resource.scene.owner == principal.id", "policy must be owner-only")
}

// TestPluginManifestDeclaresInviteAsParticipantPolicy pins the 5rh.24
// relaxation of invite-to-scene from owner-only to participant-wide: any
// scene participant may invite (Cedar `in` membership over
// resource.scene.participants), while a non-participant is denied by the
// absence of a permit. The active/paused state clause is retained because
// the InviteParticipant store method does not enforce scene state.
func TestPluginManifestDeclaresInviteAsParticipantPolicy(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("plugin.yaml")
	require.NoError(t, err)
	manifest := string(data)

	assert.Contains(t, manifest, "invite-to-scene", "the invite-to-scene ABAC policy must be declared")
	assert.Contains(t, manifest, `action in ["invite"]`, "policy must gate the invite action")
	assert.Contains(t, manifest, "principal is character", "policy principal must be a character")
	assert.Contains(t, manifest, "resource is scene", "policy must target scene resources")
	// Teeth: this exact clause fails if the policy is reverted to owner-only.
	assert.Contains(t, manifest,
		`permit(principal is character, action in ["invite"], resource is scene) when { principal.id in resource.scene.participants`,
		"invite must be participant-wide, not owner-only")
	assert.Contains(t, manifest, `resource.scene.state in ["active", "paused"]`,
		"invite must retain the active/paused state clause")
}

// TestPublishVoteTransitionsEmitLifecycleEvents pins D3's wiring: each vote cast
// emits scene_publish_vote_cast, an all-yes resolution emits
// scene_publish_cooloff_started (the COLLECTING→COOLOFF transition), and a
// subsequent flip-to-no terminal emits scene_publish_resolved — all driven
// through the real publishEventer set on the service.
func TestPublishVoteTransitionsEmitLifecycleEvents(t *testing.T) {
	t.Parallel()
	v1, v2 := ulid.Make().String(), ulid.Make().String()
	store := newFakeStore()
	store.installPublishedAttempt("pub-em", "scene-em", StatusCollecting)
	store.installVoters("pub-em", v1, v2)
	rec := &recordingPublishEventer{}
	svc := newTestService(t, store)
	svc.SetPublishEventer(rec)

	castVote(t, svc, v1, "pub-em", true)
	castVote(t, svc, v2, "pub-em", true) // all-yes → COOLOFF

	assert.Equal(t, 2, rec.voteCastCount, "each cast emits scene_publish_vote_cast")
	assert.Equal(t, 1, rec.coolOffCount, "the all-yes COLLECTING→COOLOFF transition emits cooloff_started")
	assert.Zero(t, rec.resolvedCount, "no terminal transition yet")

	// A no during COOLOFF flips back to COLLECTING (no terminal, no resolved).
	castVote(t, svc, v1, "pub-em", false)
	require.Equal(t, StatusCollecting, store.publishedScenes["pub-em"].Status)
	assert.Zero(t, rec.resolvedCount, "a flip-back is not a terminal resolution")
}

// TestPublishTerminalTransitionEmitsResolved pins that a terminal transition
// (any-no after all voted) emits scene_publish_resolved with the terminal status.
func TestPublishTerminalTransitionEmitsResolved(t *testing.T) {
	t.Parallel()
	v1, v2 := ulid.Make().String(), ulid.Make().String()
	store := newFakeStore()
	store.installPublishedAttempt("pub-em2", "scene-em2", StatusCollecting)
	store.installVoters("pub-em2", v1, v2)
	rec := &recordingPublishEventer{}
	svc := newTestService(t, store)
	svc.SetPublishEventer(rec)

	castVote(t, svc, v1, "pub-em2", true)
	castVote(t, svc, v2, "pub-em2", false) // any-no → ATTEMPT_FAILED

	assert.Equal(t, 1, rec.resolvedCount, "the terminal transition emits scene_publish_resolved")
	assert.Equal(t, StatusAttemptFailed, rec.lastResolved)
	assert.Zero(t, rec.coolOffCount, "no cool-off on a failed attempt")
}

// TestWithdrawEmitsWithdrawnAndResolved pins B4's emit wiring through the real
// emitter: a withdraw fires BOTH scene_publish_withdrawn and the
// scene_publish_resolved that the terminal transition produces, so renderers
// can distinguish a withdrawal from a vote failure (spec §7 event table).
func TestWithdrawEmitsWithdrawnAndResolved(t *testing.T) {
	t.Parallel()
	owner := ulid.Make().String()
	store := newFakeStore()
	store.installPublishedAttempt("pub-em3", "scene-em3", StatusCollecting)
	store.scenes["scene-em3"] = &SceneRow{ID: "scene-em3", OwnerID: owner, State: string(SceneStateEnded)}
	rec := &recordingPublishEventer{}
	svc := newTestService(t, store)
	svc.SetPublishEventer(rec)

	_, err := svc.WithdrawScenePublish(context.Background(), &scenev1.WithdrawScenePublishRequest{
		CallerCharacterId: owner,
		PublishedSceneId:  "pub-em3",
	})
	require.NoError(t, err)

	assert.Equal(t, 1, rec.withdrawnCount, "withdraw emits scene_publish_withdrawn")
	assert.Equal(t, 1, rec.resolvedCount, "withdraw's terminal transition emits scene_publish_resolved")
	assert.Equal(t, StatusAttemptFailed, rec.lastResolved)
}
