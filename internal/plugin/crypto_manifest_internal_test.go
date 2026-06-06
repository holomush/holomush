// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import "testing"

// TestEmitEntryMatchesWireType exercises the shared crypto.emits matcher
// directly, including the degenerate inputs that the LookupEmitSensitivity /
// PluginCanReadBack table tests reach only indirectly (holomush-50zqs). The
// matcher must never compose with an empty plugin name and must never match a
// foreign plugin's qualifier.
func TestEmitEntryMatchesWireType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		pluginName string
		entryType  string
		wireType   string
		want       bool
	}{
		{"exact bare match", "core-scenes", "scene_pose", "scene_pose", true},
		{"composed match against own-qualified wire", "core-communication", "page", "core-communication:page", true},
		{"fully-qualified entry matches exactly", "core-communication", "core-communication:page", "core-communication:page", true},
		{"foreign qualifier does not match", "core-communication", "page", "other:page", false},
		{"empty plugin name never composes", "", "page", ":page", false},
		{"empty plugin name still allows exact match", "", "page", "page", true},
		{"non-matching bare verbs", "core-scenes", "scene_pose", "scene_say", false},
		{"empty entry only matches empty wire", "core-scenes", "", "", true},
		{"empty entry composes to bare name prefix", "core-scenes", "", "core-scenes:", true},
		{"empty wire matches empty entry only", "core-scenes", "scene_pose", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := emitEntryMatchesWireType(tt.pluginName, tt.entryType, tt.wireType)
			if got != tt.want {
				t.Errorf("emitEntryMatchesWireType(%q, %q, %q) = %v, want %v",
					tt.pluginName, tt.entryType, tt.wireType, got, tt.want)
			}
		})
	}
}
