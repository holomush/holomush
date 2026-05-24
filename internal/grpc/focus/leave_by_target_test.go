// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/testsupport/sessiontest"
)

func TestLeaveFocusByTargetSweepsAllMatchingSessions(t *testing.T) {
	sceneID := ulid.Make()
	otherSceneID := ulid.Make()
	target := session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}
	streamName := "scene:" + sceneID.String() + ":ic"

	policy := &stubPolicy{
		kind:    session.FocusKindScene,
		streams: []string{streamName},
	}

	coord, sender := newTestCoordinator(
		t,
		map[string]*session.Info{
			"sess-a": {
				CharacterID: ulid.Make(), // distinct: idx_sessions_active_character covers active+detached
				Status:      session.StatusActive,
				FocusMemberships: []session.FocusMembership{
					{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: time.Now()},
				},
			},
			"sess-b": {
				CharacterID: ulid.Make(),
				Status:      session.StatusDetached,
				FocusMemberships: []session.FocusMembership{
					{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: time.Now()},
					{Kind: session.FocusKindScene, TargetID: otherSceneID, JoinedAt: time.Now()},
				},
			},
			"sess-other": {
				CharacterID: ulid.Make(),
				Status:      session.StatusActive,
				FocusMemberships: []session.FocusMembership{
					{Kind: session.FocusKindScene, TargetID: otherSceneID, JoinedAt: time.Now()},
				},
			},
		},
		policy,
	)

	result, err := coord.LeaveFocusByTarget(context.Background(), target)
	require.NoError(t, err)
	assert.Equal(t, 2, result.Succeeded)
	assert.Equal(t, 2, result.TotalScanned)
	assert.Empty(t, result.Failed)

	// Membership removed from both matching sessions; sess-other untouched.
	infoA, _ := coord.sessionStore.Get(context.Background(), "sess-a")
	assert.Empty(t, infoA.FocusMemberships)
	infoB, _ := coord.sessionStore.Get(context.Background(), "sess-b")
	require.Len(t, infoB.FocusMemberships, 1)
	assert.Equal(t, otherSceneID, infoB.FocusMemberships[0].TargetID)
	infoOther, _ := coord.sessionStore.Get(context.Background(), "sess-other")
	require.Len(t, infoOther.FocusMemberships, 1)
	assert.Equal(t, otherSceneID, infoOther.FocusMemberships[0].TargetID)

	// Unsubscribe sent for each session that actually left.
	sessionIDs := make([]string, 0, len(sender.calls))
	for _, c := range sender.calls {
		assert.False(t, c.add)
		assert.Equal(t, streamName, c.stream)
		sessionIDs = append(sessionIDs, c.sessionID)
	}
	assert.ElementsMatch(t, []string{"sess-a", "sess-b"}, sessionIDs)
}

func TestLeaveFocusByTargetReturnsEmptyResultWhenNoSessionsMatch(t *testing.T) {
	sceneID := ulid.Make()

	coord, sender := newTestCoordinator(
		t,
		map[string]*session.Info{
			"sess-a": {Status: session.StatusActive},
		},
		NewNullPolicy(session.FocusKindScene),
	)

	result, err := coord.LeaveFocusByTarget(context.Background(),
		session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID})
	require.NoError(t, err)
	assert.Equal(t, 0, result.Succeeded)
	assert.Equal(t, 0, result.TotalScanned)
	assert.Empty(t, result.Failed)
	assert.Empty(t, sender.calls)
}

func TestLeaveFocusByTargetSkipsExpiredSessions(t *testing.T) {
	sceneID := ulid.Make()
	target := session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}

	coord, _ := newTestCoordinator(
		t,
		map[string]*session.Info{
			"sess-expired": {
				Status: session.StatusExpired,
				FocusMemberships: []session.FocusMembership{
					{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: time.Now()},
				},
			},
		},
		NewNullPolicy(session.FocusKindScene),
	)

	result, err := coord.LeaveFocusByTarget(context.Background(), target)
	require.NoError(t, err)
	assert.Equal(t, 0, result.Succeeded)
	assert.Equal(t, 0, result.TotalScanned)
}

func TestLeaveFocusByTargetAcceptsZeroTargetIDWithoutPanic(t *testing.T) {
	coord, _ := newTestCoordinator(
		t,
		map[string]*session.Info{"sess-a": {Status: session.StatusActive}},
		NewNullPolicy(session.FocusKindScene),
	)

	result, err := coord.LeaveFocusByTarget(context.Background(),
		session.FocusKey{Kind: session.FocusKindScene, TargetID: ulid.ULID{}})
	require.NoError(t, err)
	assert.Equal(t, 0, result.Succeeded)
	assert.Empty(t, result.Failed)
}

func TestLeaveFocusByTargetCarriesPerSessionFailuresInResult(t *testing.T) {
	sceneID := ulid.Make()
	target := session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}

	// Wrap the store so UpdateFocusMemberships fails for one specific session.
	memStore := sessiontest.NewStore(t)
	ctx := context.Background()
	for _, id := range []string{"sess-ok", "sess-fail"} {
		require.NoError(t, memStore.Set(ctx, id, &session.Info{
			ID:          id,
			CharacterID: ulid.Make(), // distinct: idx_sessions_active_character covers active+detached
			Status:      session.StatusActive,
			FocusMemberships: []session.FocusMembership{
				{Kind: session.FocusKindScene, TargetID: sceneID, JoinedAt: time.Now()},
			},
		}))
	}
	store := &injectingStore{Store: memStore, failFor: "sess-fail"}

	coord, err := NewCoordinator(
		WithSessionStore(store),
		WithStreamSender(&capturingSender{}),
		WithKindPolicy(NewNullPolicy(session.FocusKindScene)),
	)
	require.NoError(t, err)

	result, sweepErr := coord.LeaveFocusByTarget(ctx, target)
	require.NoError(t, sweepErr, "enumeration succeeded; per-session failure rides on result.Failed")
	assert.Equal(t, 2, result.TotalScanned)
	assert.Equal(t, 1, result.Succeeded)
	require.Len(t, result.Failed, 1)
	assert.Equal(t, "sess-fail", result.Failed[0].SessionID)
	assert.ErrorContains(t, result.Failed[0].Err, "sess-fail")

	// sess-ok membership actually cleared; sess-fail still holds it.
	infoOK, _ := memStore.Get(ctx, "sess-ok")
	assert.Empty(t, infoOK.FocusMemberships, "sess-ok must still be cleared even though sess-fail errored")
	infoFail, _ := memStore.Get(ctx, "sess-fail")
	assert.Len(t, infoFail.FocusMemberships, 1, "sess-fail retains stale membership since its leave errored")
}

func TestLeaveFocusByTargetRejectsUnregisteredKind(t *testing.T) {
	// No policies registered → every kind is unregistered.
	store := sessiontest.NewStore(t)
	coord, err := NewCoordinator(WithSessionStore(store))
	require.NoError(t, err)

	result, sweepErr := coord.LeaveFocusByTarget(context.Background(),
		session.FocusKey{Kind: session.FocusKind("bogus"), TargetID: ulid.Make()})
	require.Error(t, sweepErr, "unregistered kinds must fail rather than silently return empty")
	assert.Equal(t, session.LeaveByTargetResult{}, result)
	oe, ok := oops.AsOops(sweepErr)
	require.True(t, ok, "error should be oops-coded")
	assert.Equal(t, "FOCUS_KIND_UNREGISTERED", oe.Code(),
		"same code JoinFocus/LeaveFocus return for unregistered kinds — prevents raw-string "+
			"Lua callers from silently no-oping")
}

func TestLeaveFocusByTargetReturnsEnumerationErrorOnListFailure(t *testing.T) {
	sceneID := ulid.Make()
	target := session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}

	store := &listFailingStore{Store: sessiontest.NewStore(t)}
	coord, err := NewCoordinator(
		WithSessionStore(store),
		WithKindPolicy(NewNullPolicy(session.FocusKindScene)),
	)
	require.NoError(t, err)

	result, sweepErr := coord.LeaveFocusByTarget(context.Background(), target)
	require.Error(t, sweepErr)
	assert.Equal(t, session.LeaveByTargetResult{}, result, "zero result on enumeration failure")
	oe, ok := oops.AsOops(sweepErr)
	require.True(t, ok, "error should be oops-coded")
	assert.Equal(t, "FOCUS_SWEEP_LIST_FAILED", oe.Code())
}

// injectingStore wraps a Store, failing UpdateFocusMemberships for a
// specific session ID.
type injectingStore struct {
	session.Store
	failFor string
}

func (s *injectingStore) UpdateFocusMemberships(ctx context.Context, sessionID string, m session.FocusMutator) error {
	if sessionID == s.failFor {
		return errors.New("synthetic update failure for " + sessionID)
	}
	return s.Store.UpdateFocusMemberships(ctx, sessionID, m)
}

// listFailingStore returns an error from ListByFocus to exercise the
// enumeration-failure branch of the sweep.
type listFailingStore struct {
	session.Store
}

func (s *listFailingStore) ListByFocus(_ context.Context, _ session.FocusKey) ([]*session.Info, error) {
	return nil, errors.New("synthetic list failure")
}
