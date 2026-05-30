// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
)

// dotStyleSceneIC returns a NATS dot-style scene IC subject for testing,
// using the fixed game ID "test".
func dotStyleSceneIC(sceneID string) string {
	return "events.test.scene." + sceneID + ".ic"
}

// dotStyleSceneOOC returns a NATS dot-style scene OOC subject for testing,
// using the fixed game ID "test".
func dotStyleSceneOOC(sceneID string) string {
	return "events.test.scene." + sceneID + ".ooc"
}

// dotStyleCharacter returns a NATS dot-style personal character subject for
// testing, using the fixed game ID "test".
func dotStyleCharacter(charID string) string {
	return "events.test.character." + charID
}

// TestStreamClassifiersNonCollision asserts the dot-only classifiers
// (isPrivateStream / isSceneStream / isLocationStream) partition qualified
// subjects correctly and reject every colon-style legacy form (INV-ROPS-5).
func TestStreamClassifiersNonCollision(t *testing.T) {
	cases := []struct {
		name         string
		stream       string
		wantPrivate  bool
		wantScene    bool
		wantLocation bool
	}{
		{"qualified location is public-not-scene", "events.main.location.01LOC", false, false, true},
		{"qualified character is private-not-scene", "events.main.character.01CHR", true, false, false},
		{"qualified scene ic is private-and-scene", "events.main.scene.01SCN.ic", true, true, false},
		{"colon character is rejected (no longer private)", "character:01CHR", false, false, false},
		{"colon location is rejected (not location)", "location:01LOC", false, false, false},
		{"garbage is none", "nonsense", false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.wantPrivate, isPrivateStream(tc.stream))
			require.Equal(t, tc.wantScene, isSceneStream(tc.stream))
			require.Equal(t, tc.wantLocation, isLocationStream(tc.stream))
		})
	}
}

func TestIsPrivateStream(t *testing.T) {
	tests := []struct {
		name     string
		stream   string
		expected bool
	}{
		{"returns true for scene IC stream", dotStyleSceneIC("01ABC01ABC01ABC01ABC01ABC01"), true},
		{"returns true for scene OOC stream", dotStyleSceneOOC("01ABC01ABC01ABC01ABC01ABC01"), true},
		{"returns true for dot-style character stream", dotStyleCharacter("01ABC"), true},
		{"returns false for dot-style location stream", "events.test.location.01ABC", false},
		{"returns false for unknown type", "global", false},
		{"returns false for empty string", "", false},
		{"returns false for old colon-style scene stream", "scene:01ABC:ic", false},
		{"returns false for old colon-style character stream", "character:01ABC", false},
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
			stream:   dotStyleCharacter(ownCharID.String()),
			expected: true,
		},
		{
			name:     "denies other character's stream",
			info:     &session.Info{CharacterID: ownCharID},
			stream:   dotStyleCharacter(otherCharID.String()),
			expected: false,
		},
		{
			name:     "denies legacy colon-style character stream",
			info:     &session.Info{CharacterID: ownCharID},
			stream:   "character:" + ownCharID.String(),
			expected: false,
		},
		{
			name:     "permits scene IC stream when member",
			info:     &session.Info{FocusMemberships: activeMembership},
			stream:   dotStyleSceneIC(activeSceneID.String()),
			expected: true,
		},
		{
			name:     "permits scene OOC stream when member",
			info:     &session.Info{FocusMemberships: activeMembership},
			stream:   dotStyleSceneOOC(activeSceneID.String()),
			expected: true,
		},
		{
			name:     "denies scene stream when member of different scene",
			info:     &session.Info{FocusMemberships: activeMembership},
			stream:   dotStyleSceneIC(otherSceneID.String()),
			expected: false,
		},
		{
			name:     "denies scene stream with empty memberships",
			info:     &session.Info{},
			stream:   dotStyleSceneIC(otherSceneID.String()),
			expected: false,
		},
		{
			name:     "denies malformed scene stream ULID",
			info:     &session.Info{FocusMemberships: activeMembership},
			stream:   dotStyleSceneIC("not-a-ulid"),
			expected: false,
		},
		{
			name:     "denies nil info for character stream",
			info:     nil,
			stream:   dotStyleCharacter(ownCharID.String()),
			expected: false,
		},
		{
			name:     "denies nil info for scene stream",
			info:     nil,
			stream:   dotStyleSceneIC(activeSceneID.String()),
			expected: false,
		},
		{
			name:     "denies zero CharacterID against zero-ID character stream",
			info:     &session.Info{CharacterID: zeroID},
			stream:   dotStyleCharacter(zeroID.String()),
			expected: false,
		},
		{
			name: "denies scene stream when membership TargetID is zero",
			info: &session.Info{FocusMemberships: []session.FocusMembership{
				{Kind: session.FocusKindScene, TargetID: zeroID},
			}},
			stream:   dotStyleSceneIC(zeroID.String()),
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
			stream:     dotStyleSceneIC(validID.String()),
			wantKind:   session.FocusKindScene,
			wantTarget: validID,
		},
		{
			name:       "parses scene OOC stream",
			stream:     dotStyleSceneOOC(validID.String()),
			wantKind:   session.FocusKindScene,
			wantTarget: validID,
		},
		{
			name:    "rejects non-scene stream",
			stream:  "location:01ABC",
			wantErr: true,
		},
		{
			name:    "rejects old colon-style scene stream",
			stream:  "scene:" + validID.String() + ":ic",
			wantErr: true,
		},
		{
			name:    "rejects malformed ULID",
			stream:  dotStyleSceneIC("not-a-ulid"),
			wantErr: true,
		},
		{
			name:    "rejects missing facet",
			stream:  "events.test.scene." + validID.String(),
			wantErr: true,
		},
		{
			name:    "rejects unknown facet",
			stream:  "events.test.scene." + validID.String() + ".evil",
			wantErr: true,
		},
		{
			name:    "rejects too-short dot subject",
			stream:  "events.test.scene",
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

// TestRoleSplitCharacterStreamDotABACSubjectColon pins INV-ROPS-6: the two
// roles for a character ID are now distinct package boundaries with distinct
// delimiters. The STREAM builder (world.CharacterStream) is dot-relative; the
// ABAC SUBJECT builder (access.CharacterSubject) is colon. They MUST NOT
// collapse to the same form — that distinction is what lets INV-ROPS-3's scan
// allowlist colon literals only when they're ABAC-marked.
func TestRoleSplitCharacterStreamDotABACSubjectColon(t *testing.T) {
	id := ulid.MustParse("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	// Stream form: dot-relative (this migration).
	require.Equal(t, "character."+id.String(), world.CharacterStream(id))
	// ABAC subject form: colon — the existing access builder, unchanged.
	require.Equal(t, "character:"+id.String(), access.CharacterSubject(id.String()))
}
