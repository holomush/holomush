// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// commandPathIsReachable walks the command tree from root following the given
// path of command NAMES, and reports whether every hop resolves.
//
// It traverses rather than asserting a count, because the property under test is
// REACHABILITY: an operator types `holomush character name set`, and what makes
// that work is each command having been passed to its parent's AddCommand. A
// count assertion would pass with the right number of wrong commands, and would
// fail spuriously the next time an unrelated command is added.
func commandPathIsReachable(root *cobra.Command, path ...string) bool {
	current := root
	for _, name := range path {
		var next *cobra.Command
		for _, child := range current.Commands() {
			// cobra's Use line carries the argument spec too
			// ("set <character-id> <new-name>"); the command's NAME is the
			// first field.
			if strings.Fields(child.Use)[0] == name {
				next = child
				break
			}
		}
		if next == nil {
			return false
		}
		current = next
	}
	return true
}

// TestRootCommandReachesTheCharacterNameResolutionSubcommands pins the wiring
// that makes the duplicate-resolution CLI exist for an operator.
//
// A cobra command constructed in cmd_character_name.go and never passed to
// NewRootCmd's AddCommand block compiles, unit-tests green, and returns
// "unknown command" — during precisely the failed deployment that requires it,
// since migration 000055 aborts startup on a collision (D-22). Nothing else in
// the suite catches that, because every other test builds the subcommand
// directly.
func TestRootCommandReachesTheCharacterNameResolutionSubcommands(t *testing.T) {
	root := NewRootCmd()

	assert.True(t, commandPathIsReachable(root, "character", "name", "duplicates"),
		"`holomush character name duplicates` must be reachable from NewRootCmd()")
	assert.True(t, commandPathIsReachable(root, "character", "name", "set"),
		"`holomush character name set` must be reachable from NewRootCmd()")

	// Paired negative control on the same traversal: a path that does not exist
	// must report unreachable, so the assertions above cannot pass because the
	// helper returns true unconditionally.
	assert.False(t, commandPathIsReachable(root, "character", "name", "obliterate"),
		"a command that was never registered must not be reported as reachable")
}

// TestRootCommandStillReachesThePreexistingTopLevelCommands is the second half
// of the control: the traversal finds commands this plan did not add, so a
// helper that only ever finds `character` would be caught.
func TestRootCommandStillReachesThePreexistingTopLevelCommands(t *testing.T) {
	root := NewRootCmd()
	for _, name := range []string{"gateway", "core", "migrate", "status", "plugin", "admin", "crypto", "audit", "outbox", "world"} {
		require.Truef(t, commandPathIsReachable(root, name),
			"`holomush %s` must stay reachable from NewRootCmd()", name)
	}
}
