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

// stubContributor implements StreamContributor for dedup tests.
type stubContributor struct {
	streams []string
}

func (c *stubContributor) QuerySessionStreams(_ context.Context, _ StreamContributorRequest) []string {
	return c.streams
}

func TestRestoreFocusDeduplicatesAmbientStreamsAgainstPolicyStreams(t *testing.T) {
	charID := ulid.MustNew(ulid.Now(), nil)
	locID := ulid.MustNew(ulid.Now(), nil)
	targetID := ulid.MustNew(ulid.Now(), nil)

	// The policy returns a stream that matches the character ambient stream.
	charStream := "character:" + charID.String()
	locStream := "location:" + locID.String()

	policy := &stubPolicy{
		kind:    session.FocusKindScene,
		streams: []string{charStream},
		onRestore: []StreamWithMode{
			{Stream: charStream, Mode: ReplayModeFromCursor},
			{Stream: locStream, Mode: ReplayModeFromCursor},
		},
	}

	// Plugin also returns the character stream — should be deduplicated.
	contributor := &stubContributor{streams: []string{charStream, "plugin:extra"}}

	store := session.NewMemStore()
	ctx := context.Background()
	info := &session.Info{
		ID:          "sess-dedup",
		Status:      session.StatusActive,
		CharacterID: charID,
		LocationID:  locID,
		FocusMemberships: []session.FocusMembership{
			{Kind: session.FocusKindScene, TargetID: targetID, JoinedAt: time.Now()},
		},
	}
	require.NoError(t, store.Set(ctx, info.ID, info))

	coord, err := NewCoordinator(
		WithSessionStore(store),
		WithKindPolicy(policy),
		WithStreamContributor(contributor),
	)
	require.NoError(t, err)

	plan, planErr := coord.RestoreFocus(ctx, info.ID)
	require.NoError(t, planErr)

	// Count how many times each stream appears.
	counts := make(map[string]int)
	for _, sm := range plan.Streams {
		counts[sm.Stream]++
	}

	// Character and location streams should appear exactly once (no duplicates).
	assert.Equal(t, 1, counts[charStream], "character stream should not be duplicated")
	assert.Equal(t, 1, counts[locStream], "location stream should not be duplicated")
	assert.Equal(t, 1, counts["plugin:extra"], "plugin stream should appear once")
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
