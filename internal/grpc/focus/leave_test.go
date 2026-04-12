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

func TestLeaveFocusRemovesMembershipAndUnsubscribes(t *testing.T) {
	targetID := ulid.MustNew(ulid.Now(), nil)
	target := session.FocusKey{Kind: session.FocusKindScene, TargetID: targetID}
	streamName := "scene:" + targetID.String() + ":ic"

	policy := &stubPolicy{
		kind:    session.FocusKindScene,
		streams: []string{streamName},
	}

	coord, sender := newTestCoordinator(t,
		map[string]*session.Info{
			"sess-1": {
				Status: session.StatusActive,
				FocusMemberships: []session.FocusMembership{
					{Kind: session.FocusKindScene, TargetID: targetID, JoinedAt: time.Now()},
				},
			},
		},
		policy,
	)

	err := coord.LeaveFocus(context.Background(), "sess-1", target)
	require.NoError(t, err)

	// Membership removed.
	info, getErr := coord.sessionStore.Get(context.Background(), "sess-1")
	require.NoError(t, getErr)
	assert.Empty(t, info.FocusMemberships)

	// Unsubscribe sent.
	require.Len(t, sender.calls, 1)
	assert.Equal(t, "sess-1", sender.calls[0].sessionID)
	assert.Equal(t, streamName, sender.calls[0].stream)
	assert.False(t, sender.calls[0].add)
}

func TestLeaveFocusClearsPresentingWhenPointingAtRemoved(t *testing.T) {
	targetID := ulid.MustNew(ulid.Now(), nil)
	target := session.FocusKey{Kind: session.FocusKindScene, TargetID: targetID}

	coord, _ := newTestCoordinator(t,
		map[string]*session.Info{
			"sess-1": {
				Status: session.StatusActive,
				FocusMemberships: []session.FocusMembership{
					{Kind: session.FocusKindScene, TargetID: targetID, JoinedAt: time.Now()},
				},
				PresentingFocus: &session.FocusKey{Kind: session.FocusKindScene, TargetID: targetID},
			},
		},
		NewNullPolicy(session.FocusKindScene),
	)

	err := coord.LeaveFocus(context.Background(), "sess-1", target)
	require.NoError(t, err)

	info, getErr := coord.sessionStore.Get(context.Background(), "sess-1")
	require.NoError(t, getErr)
	assert.Empty(t, info.FocusMemberships)
	assert.Nil(t, info.PresentingFocus)
}

func TestLeaveFocusIsIdempotentForNonMember(t *testing.T) {
	targetID := ulid.MustNew(ulid.Now(), nil)
	target := session.FocusKey{Kind: session.FocusKindScene, TargetID: targetID}

	coord, sender := newTestCoordinator(t,
		map[string]*session.Info{
			"sess-1": {Status: session.StatusActive},
		},
		NewNullPolicy(session.FocusKindScene),
	)

	err := coord.LeaveFocus(context.Background(), "sess-1", target)
	require.NoError(t, err)
	// No unsubscribe sent since not a member.
	assert.Empty(t, sender.calls)
}

func TestLeaveFocusRejectsExpiredSession(t *testing.T) {
	targetID := ulid.MustNew(ulid.Now(), nil)
	target := session.FocusKey{Kind: session.FocusKindScene, TargetID: targetID}

	coord, _ := newTestCoordinator(t,
		map[string]*session.Info{
			"sess-expired": {Status: session.StatusExpired},
		},
		NewNullPolicy(session.FocusKindScene),
	)

	err := coord.LeaveFocus(context.Background(), "sess-expired", target)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SESSION_EXPIRED")
}
