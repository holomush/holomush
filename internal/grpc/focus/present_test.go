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

func TestPresentFocusUpdatesPresentingPointer(t *testing.T) {
	targetID := ulid.MustNew(ulid.Now(), nil)
	target := session.FocusKey{Kind: session.FocusKindScene, TargetID: targetID}

	coord, _ := newTestCoordinator(
		t,
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

	err := coord.PresentFocus(context.Background(), "sess-1", target)
	require.NoError(t, err)

	info, getErr := coord.sessionStore.Get(context.Background(), "sess-1")
	require.NoError(t, getErr)
	require.NotNil(t, info.PresentingFocus)
	assert.Equal(t, session.FocusKindScene, info.PresentingFocus.Kind)
	assert.Equal(t, targetID, info.PresentingFocus.TargetID)
}

func TestPresentFocusRejectsNonMember(t *testing.T) {
	targetID := ulid.MustNew(ulid.Now(), nil)
	target := session.FocusKey{Kind: session.FocusKindScene, TargetID: targetID}

	// Session has no memberships.
	coord, _ := newTestCoordinator(
		t,
		map[string]*session.Info{
			"sess-1": {Status: session.StatusActive},
		},
		NewNullPolicy(session.FocusKindScene),
	)

	err := coord.PresentFocus(context.Background(), "sess-1", target)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "FOCUS_NOT_MEMBER")
}

func TestPresentFocusRejectsExpiredSession(t *testing.T) {
	targetID := ulid.MustNew(ulid.Now(), nil)
	target := session.FocusKey{Kind: session.FocusKindScene, TargetID: targetID}

	coord, _ := newTestCoordinator(
		t,
		map[string]*session.Info{
			"sess-expired": {Status: session.StatusExpired},
		},
		NewNullPolicy(session.FocusKindScene),
	)

	err := coord.PresentFocus(context.Background(), "sess-expired", target)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SESSION_EXPIRED")
}
