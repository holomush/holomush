// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deprecatedStreamKeyPattern matches a deprecated `stream =` emit-table-key
// write at the start of a line (possibly indented; any nesting of opening
// braces). Anchoring to start-of-line ensures `event.stream` field reads
// (which are still part of the input-side host→Lua event-table contract,
// out of scope for fz9h) are not falsely flagged.
var deprecatedStreamKeyPattern = regexp.MustCompile(`^[\s{]*stream\s*=`)

// In-tree Lua plugins MUST emit using the canonical `subject =` key, never
// the deprecated `stream =` alias accepted by `parseEmitEvents` for backward
// compatibility. The alias logs WARN on every emit (host.go:548) and is
// scheduled for removal under holomush-zxmo once this migration ships.
//
// Pattern is anchored to start-of-line so it matches table-key writes
// (`stream = "..."` or `{stream = "..."`) without matching field reads
// (`x = event.stream`).
func TestNoInTreeLuaPluginUsesDeprecatedStreamEmitKey(t *testing.T) {
	root := repoRoot(t)
	pluginsDir := filepath.Join(root, "plugins")

	var offenders []string
	err := filepath.WalkDir(pluginsDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || filepath.Ext(path) != ".lua" {
			return nil
		}
		raw, readErr := os.ReadFile(path) //nolint:gosec // test-only scan of in-tree plugin sources
		if readErr != nil {
			return readErr
		}
		rel, _ := filepath.Rel(root, path)
		for i, line := range strings.Split(string(raw), "\n") {
			if deprecatedStreamKeyPattern.MatchString(line) {
				offenders = append(offenders, rel+":"+strconv.Itoa(i+1)+"  "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	require.NoError(t, err, "walk plugins/ directory")

	require.Empty(t, offenders,
		"in-tree Lua plugins must use canonical `subject = ...` key in emit tables, "+
			"not deprecated `stream = ...` alias (holomush-fz9h). Offenders:\n  %s",
		strings.Join(offenders, "\n  "))
}

// TestDeprecatedStreamKeyPatternCatchesKnownOffenderShapesAndIgnoresFieldReads
// guards the regex itself against silent breakage. If a future cleanup pass
// loosens the anchor (e.g. drops the `^` or `[\s{]*`) the matcher could miss
// genuine offenders OR start flagging permitted `event.stream` reads, in
// either case making the directory-walk assertion above silently useless.
func TestDeprecatedStreamKeyPatternCatchesKnownOffenderShapesAndIgnoresFieldReads(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{"single-brace emit-table write", `        {stream = "location:01ABC", type = "say"}`, true},
		{"double-brace emit-table write (page/pemit shape)", `        {{stream = "character:01ABC", type = "page"}},`, true},
		{"bare key (echo-bot pre-migration shape)", `            stream = event.stream,`, true},
		{"canonical subject= key", `        {subject = "location:01ABC", type = "say"}`, false},
		{"event.stream field read on right-hand side", `        local s = event.stream`, false},
		{"comment mentioning stream", `    -- emit to the location stream`, false},
		{"payload assembly referencing event.stream", `        payload = "id=" .. event.stream`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, deprecatedStreamKeyPattern.MatchString(tt.line))
		})
	}
}

// repoRoot walks up from the test's cwd until it finds a directory containing
// go.mod, returning that path. Tests run from the package directory by
// default, so we walk up a few levels.
func repoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	dir := cwd
	for i := 0; i < 10; i++ {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not locate go.mod above %s", cwd)
	return ""
}

