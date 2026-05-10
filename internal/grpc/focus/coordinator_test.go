// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/session"
)

// stubPolicy is a test KindPolicy that returns configurable streams.
type stubPolicy struct {
	kind      session.FocusKind
	streams   []string
	onJoin    []StreamWithMode
	onJoinErr error
	onRestore []StreamWithMode
}

func (p *stubPolicy) Kind() session.FocusKind                { return p.kind }
func (p *stubPolicy) StreamsFor(_ session.FocusKey) []string { return p.streams }
func (p *stubPolicy) OnJoin(_ PolicyContext) ([]StreamWithMode, error) { return p.onJoin, p.onJoinErr }

func (p *stubPolicy) OnRestore(_ PolicyContext) ([]StreamWithMode, error) {
	return p.onRestore, nil
}

// capturingSender records all Send calls for assertion.
type capturingSender struct {
	calls []sendCall
}

type sendCall struct {
	sessionID string
	stream    string
	add       bool
	mode      ReplayMode
}

func (s *capturingSender) Send(sessionID, stream string, add bool, mode ReplayMode) error {
	s.calls = append(s.calls, sendCall{sessionID, stream, add, mode})
	return nil
}

func newTestCoordinator(t *testing.T, sessions map[string]*session.Info, policies ...KindPolicy) (*defaultCoordinator, *capturingSender) {
	t.Helper()
	store := session.NewMemStore()
	ctx := context.Background()
	for id, info := range sessions {
		if info.ID == "" {
			info.ID = id
		}
		require.NoError(t, store.Set(ctx, id, info))
	}
	sender := &capturingSender{}
	opts := make([]CoordinatorOption, 0, 2+len(policies))
	opts = append(opts, WithSessionStore(store), WithStreamSender(sender))
	for _, p := range policies {
		opts = append(opts, WithKindPolicy(p))
	}
	coord, err := NewCoordinator(opts...)
	require.NoError(t, err)
	return coord.(*defaultCoordinator), sender
}

func TestNewCoordinatorSucceedsWithRequiredDeps(t *testing.T) {
	store := session.NewMemStore()
	coord, err := NewCoordinator(WithSessionStore(store))
	require.NoError(t, err)
	assert.NotNil(t, coord)
}

func TestNewCoordinatorFailsWithoutSessionStore(t *testing.T) {
	_, err := NewCoordinator()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session store")
}

func TestNewCoordinatorAcceptsAllOptions(t *testing.T) {
	store := session.NewMemStore()
	sender := &capturingSender{}
	coord, err := NewCoordinator(
		WithSessionStore(store),
		WithStreamSender(sender),
		WithGameSettings(nil),
		WithPlayerSettings(nil),
		WithCharacterSettings(nil),
		WithPlayerPreferences(nil),
		WithKindPolicy(NewNullPolicy(session.FocusKindScene)),
	)
	require.NoError(t, err)
	assert.NotNil(t, coord)
}

func TestNewCoordinatorRegistersKindPolicy(t *testing.T) {
	store := session.NewMemStore()
	null := NewNullPolicy(session.FocusKindScene)
	coord, err := NewCoordinator(WithSessionStore(store), WithKindPolicy(null))
	require.NoError(t, err)
	dc := coord.(*defaultCoordinator)
	policy, ok := dc.policies[session.FocusKindScene]
	assert.True(t, ok)
	assert.Equal(t, session.FocusKindScene, policy.Kind())
}
