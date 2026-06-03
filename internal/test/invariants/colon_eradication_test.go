// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package invariants

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestINV_EVENTBUS_19_NoColonStreamLiterals asserts no production Go (or Lua) source
// contains a colon-style entity-prefix literal as a pub/sub STREAM name.
// Supersedes INV-SCENE-1 (scene-only) with a repo-wide scan. Roots MUST exist
// (fail, not skip).
//
// The stream-vs-ABAC ambiguity is solved structurally: ABAC subjects/resources
// are built ONLY via internal/access builders (access.CharacterSubject,
// access.PluginSubject, …), which live in the allowlisted internal/access
// package. Host code therefore has NO inline colon stream literal; the only
// residual colon literal in scanned roots is in plugins/core-scenes/ (which
// cannot import internal/access), marked with an "ABAC resource ref" comment.
// Any other hit is unambiguously a stream-producer bug. (Mirrors INV-SCENE-1's
// proven abacContextMarkers approach.)
//
// Why the regex covers exactly four domains (location|character|notifications|
// scene) and NOT "plugin": "plugin" is an ABAC SUBJECT type (built only via
// access.PluginSubject, in the allowlisted internal/access package), never a
// pub/sub STREAM. Including it would false-positive on the Go error-prefix
// idiom `"plugin: <message>"` (e.g. panic("plugin: ...") / errors.New("plugin:
// ...") in pkg/plugin/service.go and pkg/plugin/sdk.go). This scan targets
// STREAM literals; "plugin" is not a stream domain.
func TestINV_EVENTBUS_19_NoColonStreamLiterals(t *testing.T) {
	roots := []string{"../../../internal", "../../../pkg", "../../../plugins", "../../../cmd"}
	pattern := regexp.MustCompile(`"(location|character|notifications|scene):`)
	abacMarkers := []string{"Evaluate(", "CanPerformAction(", "NewAccessRequest(", ".Grant(", "ABAC resource ref"}
	for _, root := range roots {
		info, err := os.Stat(root)
		require.NoErrorf(t, err, "scan root missing: %s", root) // fail, never skip
		require.True(t, info.IsDir())
	}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if strings.Contains(path, "internal/access") {
					return filepath.SkipDir // sole sanctioned home of colon prefixes
				}
				return nil
			}
			// Scan Go AND Lua: a Lua plugin's main.lua may build stream
			// subjects inline (bypassing pkg/holo), so .lua must be covered.
			// Generated *.pb.go is excluded: it is not a hand-authored producer
			// surface, and its proto doc-comments legitimately cite the colon
			// ABAC-subject form — scanning it would couple this invariant to
			// proto-comment phrasing the migration cannot control.
			isGo := strings.HasSuffix(path, ".go") &&
				!strings.HasSuffix(path, "_test.go") &&
				!strings.HasSuffix(path, ".pb.go")
			isLua := strings.HasSuffix(path, ".lua")
			if !isGo && !isLua {
				return nil
			}
			// G122: this meta-test walks the repo's own fixed source roots
			// (internal/pkg/plugins/cmd) — a trusted tree with no
			// untrusted-symlink TOCTOU surface; os.Root scoping is unwarranted.
			data, rerr := os.ReadFile(path) //nolint:gosec // G122: trusted repo-source meta-test scan
			require.NoError(t, rerr)
			for i, raw := range strings.Split(string(data), "\n") {
				if !pattern.MatchString(raw) {
					continue
				}
				line := strings.TrimSpace(raw)
				if strings.HasPrefix(line, "//") || strings.HasPrefix(line, "*") {
					continue // doc/comment line, not a producer
				}
				skip := false
				for _, m := range abacMarkers {
					if strings.Contains(raw, m) {
						skip = true // ABAC subject/resource construction, not a stream
						break
					}
				}
				if skip {
					continue
				}
				t.Errorf("INV-EVENTBUS-19: colon-style stream literal in %s:%d:\n  %s", path, i+1, line)
			}
			return nil
		})
		require.NoError(t, err)
	}
}
