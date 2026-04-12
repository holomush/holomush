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

func TestRestoreFocusReturnsEmptyPlanForNewSession(t *testing.T) {
	coord, _ := newTestCoordinator(t,
		map[string]*session.Info{
			"sess-1": {Status: session.StatusActive},
		},
	)

	plan, err := coord.RestoreFocus(context.Background(), "sess-1")
	require.NoError(t, err)
	assert.Empty(t, plan.Streams)
	assert.Empty(t, plan.PresentingStream)
}

func TestRestoreFocusDispatchesToKindPolicyOnRestore(t *testing.T) {
	targetID := ulid.MustNew(ulid.Now(), nil)
	streamName := "scene:" + targetID.String() + ":ic"

	policy := &stubPolicy{
		kind:    session.FocusKindScene,
		streams: []string{streamName},
		onRestore: []StreamWithMode{
			{Stream: streamName, Mode: ReplayModeFromCursor},
		},
	}

	coord, _ := newTestCoordinator(t,
		map[string]*session.Info{
			"sess-detached": {
				Status: session.StatusDetached,
				FocusMemberships: []session.FocusMembership{
					{Kind: session.FocusKindScene, TargetID: targetID, JoinedAt: time.Now()},
				},
			},
		},
		policy,
	)

	plan, err := coord.RestoreFocus(context.Background(), "sess-detached")
	require.NoError(t, err)
	require.Len(t, plan.Streams, 1)
	assert.Equal(t, streamName, plan.Streams[0].Stream)
	assert.Equal(t, ReplayModeFromCursor, plan.Streams[0].Mode)
}

func TestRestoreFocusIncludesPresentingStream(t *testing.T) {
	targetID := ulid.MustNew(ulid.Now(), nil)
	streamName := "scene:" + targetID.String() + ":ic"

	policy := &stubPolicy{
		kind:    session.FocusKindScene,
		streams: []string{streamName},
		onRestore: []StreamWithMode{
			{Stream: streamName, Mode: ReplayModeFromCursor},
		},
	}

	presenting := session.FocusKey{Kind: session.FocusKindScene, TargetID: targetID}
	coord, _ := newTestCoordinator(t,
		map[string]*session.Info{
			"sess-1": {
				Status: session.StatusActive,
				FocusMemberships: []session.FocusMembership{
					{Kind: session.FocusKindScene, TargetID: targetID, JoinedAt: time.Now()},
				},
				PresentingFocus: &presenting,
			},
		},
		policy,
	)

	plan, err := coord.RestoreFocus(context.Background(), "sess-1")
	require.NoError(t, err)
	assert.Equal(t, streamName, plan.PresentingStream)
}

func TestRestoreFocusRejectsExpiredSession(t *testing.T) {
	coord, _ := newTestCoordinator(t,
		map[string]*session.Info{
			"sess-expired": {Status: session.StatusExpired},
		},
	)

	_, err := coord.RestoreFocus(context.Background(), "sess-expired")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SESSION_EXPIRED")
}
