// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoLegacyIDReferencesInProductionCode enforces INV-PLUGIN-15: no
// LegacyID/legacy_id/App-Actor-Legacy-ID references in production code
// (excludes docs/, *.pb.go, and *_test.go). Uses filepath.WalkDir to be
// VCS-agnostic (works under both git and jj-colocated repos).
func TestNoLegacyIDReferencesInProductionCode(t *testing.T) {
	root := repoRoot(t)
	pattern := regexp.MustCompile(`\bLegacyID\b|\blegacy_id\b|App-Actor-Legacy-ID`)

	// Top-level dirs that contain production source code. Excluded by
	// design: docs/, web/ (TS/Svelte), node_modules/, .jj/, .git/, build/,
	// site/.
	includeDirs := []string{
		"api", "cmd", "internal", "pkg", "plugins", "scripts",
		"test", "tools",
	}

	var violations []string
	for _, top := range includeDirs {
		topDir := filepath.Join(root, top)
		if _, err := os.Stat(topDir); err != nil {
			continue
		}
		err := filepath.WalkDir(topDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				name := d.Name()
				if name == ".jj" || name == ".git" || name == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			ext := filepath.Ext(path)
			if ext != ".go" && ext != ".proto" {
				return nil
			}
			base := filepath.Base(path)
			if strings.HasSuffix(base, "_test.go") || strings.HasSuffix(base, ".pb.go") {
				return nil
			}
			contents, readErr := os.ReadFile(path) //nolint:gosec // test-only walk under repo root
			if readErr != nil {
				return readErr
			}
			for i, line := range strings.Split(string(contents), "\n") {
				if !pattern.MatchString(line) {
					continue
				}
				// Skip proto `reserved` declarations and explanatory comments —
				// these are part of the deletion-defense convention (preventing
				// future field-number reuse), not active references.
				trimmed := strings.TrimSpace(line)
				if ext == ".proto" && (strings.HasPrefix(trimmed, "reserved ") || strings.HasPrefix(trimmed, "//")) {
					continue
				}
				rel, _ := filepath.Rel(root, path)
				violations = append(violations,
					rel+":"+itoa(i+1)+": "+trimmed)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", topDir, err)
		}
	}
	if len(violations) > 0 {
		t.Fatalf("INV-PLUGIN-15 violation: LegacyID references in production code:\n%s",
			strings.Join(violations, "\n"))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
