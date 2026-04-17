// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEventsTableCannotBeUpdatedOrDeletedByApplicationCode is a meta-test
// that asserts no production Go file in the module contains an UPDATE or
// DELETE statement targeting the events table. Append-only semantics are
// foundational to event sourcing: Replay orders by id, cursors advance via
// monotonic CAS, and every read path assumes an event, once written, is
// permanent. A stray mutation would silently corrupt derived state.
//
// Per project policy (CLAUDE.md: "MUST NOT use triggers or functions —
// All logic lives in Go; PostgreSQL is storage only") this static check
// is the authoritative enforcement. The plugin threat vector from the
// original finding (holomush-3saa) is mitigated separately by schema-role
// isolation (internal/plugin/schema_provisioner.go): plugins run as roles
// with REVOKE ALL ON SCHEMA public and cannot see the events table at all.
// The remaining realistic risk is a future core-code change that introduces
// a mutation statement — this test trips that before merge.
//
// Robustness notes (mirroring plugins/core-scenes TestOpsEventsCannotBeUpdated...):
//   - Walks the entire module tree so new packages are covered automatically.
//   - Uses os.Root + fs.WalkDir so file I/O is root-scoped and cannot escape
//     the module directory via symlinks (gosec G122 defense).
//   - Fails loudly on read errors; silent skips would let regressions land.
//   - Match is lowercase + whitespace-collapsed so multi-line SQL like
//     `DELETE\n  FROM events` or mixed case still trips.
//   - Uses \b word boundaries so `events` is not confused with suffixed
//     table names (e.g. scene_ops_events, event_subscriptions).
//   - Matches PostgreSQL's ONLY modifier (`UPDATE ONLY events`) and
//     schema-qualified references (`DELETE FROM public.events`).
//   - Also matches TRUNCATE, which bypasses DELETE triggers and is the
//     most destructive append-only violation possible.
//   - Excludes _test.go files because tests legitimately use DELETE for
//     cleanup/isolation; only production code is scrutinised.
func TestEventsTableCannotBeUpdatedOrDeletedByApplicationCode(t *testing.T) {
	moduleRoot, err := findModuleRoot()
	require.NoError(t, err, "locate module root")

	root, err := os.OpenRoot(moduleRoot)
	require.NoError(t, err, "open module root")
	t.Cleanup(func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Logf("close root: %v", closeErr)
		}
	})

	whitespace := regexp.MustCompile(`\s+`)
	updateRE := regexp.MustCompile(`\bupdate (?:only )?(?:\w+\.)?events\b`)
	deleteRE := regexp.MustCompile(`\bdelete from (?:only )?(?:\w+\.)?events\b`)
	truncateRE := regexp.MustCompile(`\btruncate (?:table )?(?:only )?(?:\w+\.)?events\b`)

	var scanned int
	err = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".jj", "vendor", "testdata", "node_modules", "build", ".worktrees":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := fs.ReadFile(root.FS(), path)
		require.NoError(t, readErr, "read %s", path)
		content := whitespace.ReplaceAllString(strings.ToLower(string(data)), " ")
		assert.Falsef(t, updateRE.MatchString(content),
			"%s contains UPDATE on events table — events must be append-only", path)
		assert.Falsef(t, deleteRE.MatchString(content),
			"%s contains DELETE on events table — events must be append-only", path)
		assert.Falsef(t, truncateRE.MatchString(content),
			"%s contains TRUNCATE on events table — events must be append-only", path)
		scanned++
		return nil
	})
	require.NoError(t, err, "walk module root")
	require.NotZero(t, scanned, "no production .go files scanned — test is misconfigured")
}

// findModuleRoot walks upward from the test working directory until it
// locates go.mod. Tests run with CWD set to their package directory, so
// this avoids hardcoding a relative path that would break if the file
// moves.
func findModuleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		_, statErr := os.Stat(filepath.Join(dir, "go.mod"))
		if statErr == nil {
			return dir, nil
		}
		if !os.IsNotExist(statErr) {
			return "", statErr
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
