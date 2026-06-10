// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// TestNoDeleteFromPluginsInCodebase enforces INV-PLUGIN-17: plugin rows are
// never DELETEd. SweepInactive sets gc_at instead. This static guard
// catches future changes that would reintroduce DELETE.
//
// Verifies: INV-PLUGIN-17
//
// Uses filepath.WalkDir (not `git grep`) so the search is VCS-agnostic —
// works under both git and jj-colocated worktrees, and reliably surfaces
// rather than fail-opening if the underlying VCS query errors.
func TestNoDeleteFromPluginsInCodebase(t *testing.T) {
	root := repoRootForTest(t)
	pattern := regexp.MustCompile(`DELETE\s+FROM\s+plugins\b`)

	// Production source dirs only. _test.go files are excluded (this very
	// file's t.Fatalf message would otherwise self-match the pattern).
	includeDirs := []string{"api", "cmd", "internal", "pkg", "plugins", "scripts"}

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
			if filepath.Ext(path) != ".go" {
				return nil
			}
			base := filepath.Base(path)
			if strings.HasSuffix(base, "_test.go") {
				return nil
			}
			contents, readErr := os.ReadFile(path) //nolint:gosec // test-only walk under repo root
			if readErr != nil {
				return readErr
			}
			for i, line := range strings.Split(string(contents), "\n") {
				if pattern.MatchString(line) {
					rel, _ := filepath.Rel(root, path)
					violations = append(violations,
						rel+":"+itoaForTest(i+1)+": "+strings.TrimSpace(line))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", topDir, err)
		}
	}
	if len(violations) > 0 {
		t.Fatalf("INV-PLUGIN-17 violation: matches in production code:\n%s",
			strings.Join(violations, "\n"))
	}
}

// repoRootForTest derives the repository root from the test source file's
// path. The `_` runtime caller resolves to this file inside the source
// tree; we walk up to find the dir containing go.mod.
func repoRootForTest(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not resolve test source file via runtime.Caller")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate go.mod from " + file)
		}
		dir = parent
	}
}

func itoaForTest(n int) string {
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
