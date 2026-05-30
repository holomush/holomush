// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package focus

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/session"
)

func TestComputeFocusManagedStreams(t *testing.T) {
	t.Parallel()
	sceneID := ulid.Make()
	sceneFK := &session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}
	locID := ulid.Make()

	cases := []struct {
		name     string
		fk       *session.FocusKey
		locID    ulid.ULID
		gameID   string
		expected []string
	}{
		{
			name:     "GridFocus_LocationOnly",
			fk:       nil,
			locID:    locID,
			gameID:   "main",
			expected: []string{"location." + locID.String()},
		},
		{
			name:   "SceneFocus_ICAndOOC",
			fk:     sceneFK,
			locID:  locID,
			gameID: "main",
			expected: []string{
				"events.main.scene." + sceneID.String() + ".ic",
				"events.main.scene." + sceneID.String() + ".ooc",
			},
		},
		{
			name:   "SceneFocus_GameIDPropagated",
			fk:     sceneFK,
			locID:  locID,
			gameID: "altgame",
			expected: []string{
				"events.altgame.scene." + sceneID.String() + ".ic",
				"events.altgame.scene." + sceneID.String() + ".ooc",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			streams := ComputeFocusManagedStreams(tc.fk, tc.locID, tc.gameID)
			assert.ElementsMatch(t, tc.expected, streams)
		})
	}
}

func TestComputeFocusManagedStreamsDeterministic(t *testing.T) {
	t.Parallel()
	// INV-P5-3: same inputs → same streams (membership comparison).
	// Kept as a standalone test because the assertion shape (idempotence
	// loop) doesn't fit the input/output table above.
	sceneID := ulid.Make()
	fk := &session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID}
	locID := ulid.Make()
	for i := 0; i < 5; i++ {
		a := ComputeFocusManagedStreams(fk, locID, "main")
		b := ComputeFocusManagedStreams(fk, locID, "main")
		assert.Equal(t, a, b)
	}
}

func TestStreamDeltas(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		old     []string
		next    []string
		adds    []string
		removes []string
	}{
		{
			name:    "AddsAndRemoves",
			old:     []string{"a", "b", "c"},
			next:    []string{"b", "c", "d"},
			adds:    []string{"d"},
			removes: []string{"a"},
		},
		{
			name:    "NoChange",
			old:     []string{"a", "b"},
			next:    []string{"a", "b"},
			adds:    nil,
			removes: nil,
		},
		{
			name:    "AllAdded",
			old:     nil,
			next:    []string{"a", "b"},
			adds:    []string{"a", "b"},
			removes: nil,
		},
		{
			name:    "AllRemoved",
			old:     []string{"a", "b"},
			next:    nil,
			adds:    nil,
			removes: []string{"a", "b"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			adds, removes := StreamDeltas(tc.old, tc.next)
			assert.ElementsMatch(t, tc.adds, adds)
			assert.ElementsMatch(t, tc.removes, removes)
		})
	}
}
