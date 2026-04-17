// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/session"
)

func TestIsPrivateStream(t *testing.T) {
	tests := []struct {
		name     string
		stream   string
		expected bool
	}{
		{"returns true for scene IC stream", "scene:01ABC:ic", true},
		{"returns true for scene OOC stream", "scene:01ABC:ooc", true},
		{"returns true for character stream", "character:01ABC", true},
		{"returns false for location stream", "location:01ABC", false},
		{"returns false for unknown type", "global", false},
		{"returns false for empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isPrivateStream(tt.stream))
		})
	}
}

func TestSessionHasMembership(t *testing.T) {
	ownCharID := ulid.Make()
	otherCharID := ulid.Make()
	activeSceneID := ulid.Make()
	otherSceneID := ulid.Make()
	zeroID := ulid.ULID{}

	activeMembership := []session.FocusMembership{
		{Kind: session.FocusKindScene, TargetID: activeSceneID},
	}

	tests := []struct {
		name     string
		info     *session.Info
		stream   string
		expected bool
	}{
		{
			name:     "permits own character stream",
			info:     &session.Info{CharacterID: ownCharID},
			stream:   "character:" + ownCharID.String(),
			expected: true,
		},
		{
			name:     "denies other character's stream",
			info:     &session.Info{CharacterID: ownCharID},
			stream:   "character:" + otherCharID.String(),
			expected: false,
		},
		{
			name:     "permits scene IC stream when member",
			info:     &session.Info{FocusMemberships: activeMembership},
			stream:   "scene:" + activeSceneID.String() + ":ic",
			expected: true,
		},
		{
			name:     "permits scene OOC stream when member",
			info:     &session.Info{FocusMemberships: activeMembership},
			stream:   "scene:" + activeSceneID.String() + ":ooc",
			expected: true,
		},
		{
			name:     "denies scene stream when member of different scene",
			info:     &session.Info{FocusMemberships: activeMembership},
			stream:   "scene:" + otherSceneID.String() + ":ic",
			expected: false,
		},
		{
			name:     "denies scene stream with empty memberships",
			info:     &session.Info{},
			stream:   "scene:" + otherSceneID.String() + ":ic",
			expected: false,
		},
		{
			name:     "denies malformed scene stream ULID",
			info:     &session.Info{FocusMemberships: activeMembership},
			stream:   "scene:not-a-ulid:ic",
			expected: false,
		},
		{
			name:     "denies nil info for character stream",
			info:     nil,
			stream:   "character:" + ownCharID.String(),
			expected: false,
		},
		{
			name:     "denies nil info for scene stream",
			info:     nil,
			stream:   "scene:" + activeSceneID.String() + ":ic",
			expected: false,
		},
		{
			name:     "denies zero CharacterID against zero-ID character stream",
			info:     &session.Info{CharacterID: zeroID},
			stream:   "character:" + zeroID.String(),
			expected: false,
		},
		{
			name: "denies scene stream when membership TargetID is zero",
			info: &session.Info{FocusMemberships: []session.FocusMembership{
				{Kind: session.FocusKindScene, TargetID: zeroID},
			}},
			stream:   "scene:" + zeroID.String() + ":ic",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, sessionHasMembership(tt.info, tt.stream))
		})
	}
}

func TestStreamToFocusKey(t *testing.T) {
	validID := ulid.Make()

	tests := []struct {
		name       string
		stream     string
		wantErr    bool
		wantKind   session.FocusKind
		wantTarget ulid.ULID
	}{
		{
			name:       "parses scene IC stream",
			stream:     "scene:" + validID.String() + ":ic",
			wantKind:   session.FocusKindScene,
			wantTarget: validID,
		},
		{
			name:       "parses scene OOC stream",
			stream:     "scene:" + validID.String() + ":ooc",
			wantKind:   session.FocusKindScene,
			wantTarget: validID,
		},
		{
			name:    "rejects non-scene stream",
			stream:  "location:01ABC",
			wantErr: true,
		},
		{
			name:    "rejects malformed ULID",
			stream:  "scene:not-a-ulid:ic",
			wantErr: true,
		},
		{
			name:    "rejects missing suffix",
			stream:  "scene:" + validID.String(),
			wantErr: true,
		},
		{
			name:    "rejects unknown suffix",
			stream:  "scene:" + validID.String() + ":evil",
			wantErr: true,
		},
		{
			name:    "rejects empty suffix",
			stream:  "scene:" + validID.String() + ":",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fk, err := streamToFocusKey(tt.stream)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.NotNil(t, fk)
			assert.Equal(t, tt.wantKind, fk.Kind)
			assert.Equal(t, tt.wantTarget, fk.TargetID)
		})
	}
}
