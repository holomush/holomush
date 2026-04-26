// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// cmd/lint-plugin-manifests verifies that every in-tree plugin.yaml with
// a character-reachable entry point declares "character" in
// actor_kinds_claimable. Spec ec22.1 §3.2 + acceptance criterion 8.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	plugins "github.com/holomush/holomush/internal/plugin"
)

func main() {
	matches, err := filepath.Glob("plugins/*/plugin.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "glob error: %v\n", err)
		os.Exit(2)
	}
	failures := 0
	for _, manifestPath := range matches {
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: read error: %v\n", manifestPath, err)
			failures++
			continue
		}
		m, err := plugins.ParseManifest(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: parse error: %v\n", manifestPath, err)
			failures++
			continue
		}
		if requiresCharacterClaim(m) && !containsCharacter(m.ActorKindsClaimable) {
			fmt.Fprintf(os.Stderr, "ERROR: plugin %q at %s has a character-reachable entry point but actor_kinds_claimable does not include \"character\".\n",
				m.Name, manifestPath)
			fmt.Fprintf(os.Stderr, "  Add 'actor_kinds_claimable: [plugin, character]' to the manifest.\n")
			failures++
		}
	}
	if failures > 0 {
		fmt.Fprintf(os.Stderr, "\n%d plugin manifest(s) failed the actor_kinds_claimable lint.\n", failures)
		os.Exit(1)
	}
	fmt.Println("All plugin manifests pass actor_kinds_claimable lint.")
}

// requiresCharacterClaim implements the spec §3.2 / criterion 8 heuristic:
// flag plugins where type != setting AND any of:
//
//	(a) emits: non-empty AND (commands: non-empty OR events: non-empty)
//	(b) events: non-empty (regardless of emits:)
func requiresCharacterClaim(m *plugins.Manifest) bool {
	if m.Type == plugins.TypeSetting {
		return false
	}
	hasEmits := len(m.Emits) > 0
	hasCommands := len(m.Commands) > 0
	hasEvents := len(m.Events) > 0
	if hasEvents {
		return true // clause (b)
	}
	if hasEmits && (hasCommands || hasEvents) {
		return true // clause (a)
	}
	return false
}

func containsCharacter(kinds []string) bool {
	for _, k := range kinds {
		if k == "character" {
			return true
		}
	}
	return false
}
