// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestJoinFocusAddsMembershipAndSendsStreams(t *testing.T) {
	targetID := ulid.MustNew(ulid.Now(), nil)
	target := session.FocusKey{Kind: session.FocusKindScene, TargetID: targetID}
	policy := &stubPolicy{
		kind:    session.FocusKindScene,
		streams: []string{"scene:" + targetID.String() + ":ic"},
		onJoin: []StreamWithMode{
			{Stream: "scene:" + targetID.String() + ":ic", Mode: ReplayModeFromCursor},
			{Stream: "scene:" + targetID.String() + ":ooc", Mode: ReplayModeLiveOnly},
		},
	}

	coord, sender := newTestCoordinator(t,
		map[string]*session.Info{
			"sess-1": {Status: session.StatusActive},
		},
		policy,
	)

	err := coord.JoinFocus(context.Background(), "sess-1", target)
	require.NoError(t, err)

	// Verify membership was persisted.
	info, getErr := coord.sessionStore.Get(context.Background(), "sess-1")
	require.NoError(t, getErr)
	require.Len(t, info.FocusMemberships, 1)
	assert.Equal(t, session.FocusKindScene, info.FocusMemberships[0].Kind)
	assert.Equal(t, targetID, info.FocusMemberships[0].TargetID)

	// Verify streams were sent.
	require.Len(t, sender.calls, 2)
	assert.Equal(t, "sess-1", sender.calls[0].sessionID)
	assert.True(t, sender.calls[0].add)
	assert.Equal(t, ReplayModeFromCursor, sender.calls[0].mode)
	assert.Equal(t, "sess-1", sender.calls[1].sessionID)
	assert.True(t, sender.calls[1].add)
	assert.Equal(t, ReplayModeLiveOnly, sender.calls[1].mode)
}

func TestJoinFocusRejectsDuplicateMembership(t *testing.T) {
	targetID := ulid.MustNew(ulid.Now(), nil)
	target := session.FocusKey{Kind: session.FocusKindScene, TargetID: targetID}

	coord, _ := newTestCoordinator(t,
		map[string]*session.Info{
			"sess-1": {
				Status: session.StatusActive,
				FocusMemberships: []session.FocusMembership{
					{Kind: session.FocusKindScene, TargetID: targetID, JoinedAt: time.Now()},
				},
			},
		},
		NewNullPolicy(session.FocusKindScene),
	)

	err := coord.JoinFocus(context.Background(), "sess-1", target)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "FOCUS_ALREADY_MEMBER")
}

func TestJoinFocusRejectsExpiredSession(t *testing.T) {
	targetID := ulid.MustNew(ulid.Now(), nil)
	target := session.FocusKey{Kind: session.FocusKindScene, TargetID: targetID}

	coord, _ := newTestCoordinator(t,
		map[string]*session.Info{
			"sess-expired": {Status: session.StatusExpired},
		},
		NewNullPolicy(session.FocusKindScene),
	)

	err := coord.JoinFocus(context.Background(), "sess-expired", target)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SESSION_EXPIRED")
}

func TestJoinFocusRejectsUnregisteredKind(t *testing.T) {
	targetID := ulid.MustNew(ulid.Now(), nil)
	target := session.FocusKey{Kind: session.FocusKindScene, TargetID: targetID}

	// No policies registered.
	coord, _ := newTestCoordinator(t,
		map[string]*session.Info{
			"sess-1": {Status: session.StatusActive},
		},
	)

	err := coord.JoinFocus(context.Background(), "sess-1", target)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "FOCUS_KIND_UNREGISTERED")
}

func TestJoinFocusRejectsNotFoundSession(t *testing.T) {
	targetID := ulid.MustNew(ulid.Now(), nil)
	target := session.FocusKey{Kind: session.FocusKindScene, TargetID: targetID}

	coord, _ := newTestCoordinator(t, map[string]*session.Info{},
		NewNullPolicy(session.FocusKindScene),
	)

	err := coord.JoinFocus(context.Background(), "nonexistent", target)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SESSION_NOT_FOUND")
}
