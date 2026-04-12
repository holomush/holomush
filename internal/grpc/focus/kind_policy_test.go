// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/session"
)

func TestReplayModeStringReturnsHumanReadableName(t *testing.T) {
	tests := []struct {
		name     string
		mode     ReplayMode
		expected string
	}{
		{"from cursor", ReplayModeFromCursor, "from_cursor"},
		{"bounded tail", ReplayModeBoundedTail, "bounded_tail"},
		{"live only", ReplayModeLiveOnly, "live_only"},
		{"unknown value", ReplayMode(99), "unknown(99)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.mode.String())
		})
	}
}

func TestNullPolicyKindReturnsRegisteredKind(t *testing.T) {
	p := NewNullPolicy(session.FocusKindScene)
	assert.Equal(t, session.FocusKindScene, p.Kind())
}

func TestNullPolicyStreamsForReturnsEmpty(t *testing.T) {
	p := NewNullPolicy(session.FocusKindScene)
	streams := p.StreamsFor(session.FocusKey{
		Kind:     session.FocusKindScene,
		TargetID: ulid.Make(),
	})
	assert.Empty(t, streams)
}

func TestNullPolicyOnJoinReturnsEmpty(t *testing.T) {
	p := NewNullPolicy(session.FocusKindScene)
	result, err := p.OnJoin(FocusPolicyContext{
		SessionID: "sess-1",
		Target:    session.FocusKey{Kind: session.FocusKindScene, TargetID: ulid.Make()},
	})
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestNullPolicyOnRestoreReturnsEmpty(t *testing.T) {
	p := NewNullPolicy(session.FocusKindScene)
	result, err := p.OnRestore(FocusPolicyContext{
		SessionID: "sess-1",
		Target:    session.FocusKey{Kind: session.FocusKindScene, TargetID: ulid.Make()},
	})
	require.NoError(t, err)
	assert.Empty(t, result)
}
