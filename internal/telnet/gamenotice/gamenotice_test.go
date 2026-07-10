// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package gamenotice

import "testing"

// TestBuildersReturnContentFreeGameLeaders verifies each notice-type builder
// returns the exact `[>GAME: …]` leader for a given scene id, and that the
// empty-id edge yields a well-formed leader with an empty slot (no panic).
// The builders are pure string transforms: they take only the bare scene id
// and never a title or content.
func TestBuildersReturnContentFreeGameLeaders(t *testing.T) {
	tests := []struct {
		name    string
		build   func(string) string
		sceneID string
		want    string
	}{
		{"activity leader carries scene id only", Activity, "01ABC", "[>GAME: Scene #01ABC has new activity]"},
		{"idle leader carries scene id only", Idle, "01ABC", "[>GAME: Scene #01ABC is now idle]"},
		{"invite leader carries scene id only", Invite, "01ABC", "[>GAME: You were invited to Scene #01ABC]"},
		{"activity leader with empty id is well-formed", Activity, "", "[>GAME: Scene # has new activity]"},
		{"idle leader with empty id is well-formed", Idle, "", "[>GAME: Scene # is now idle]"},
		{"invite leader with empty id is well-formed", Invite, "", "[>GAME: You were invited to Scene #]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.build(tt.sceneID); got != tt.want {
				t.Errorf("build(%q) = %q, want %q", tt.sceneID, got, tt.want)
			}
		})
	}
}
