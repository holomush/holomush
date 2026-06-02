// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestLintNoMicrosecondTruncatePasses pins INV-STORE-3 via the lint task.
// If anyone reintroduces a microsecond-precision truncate call without a
// pgnanos-exempt annotation, this test fails along with the lint.
//
// The `task` shell-out is bounded by a 2-minute context so a stuck linter
// can't hang CI.
func TestLintNoMicrosecondTruncatePasses(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping shell-out lint test in short mode")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "task", "lint:no-microsecond-truncate")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("INV-STORE-3 violated: %s\n%s", err, strings.TrimSpace(string(out)))
	}
}

// TestLintNoMicrosecondTruncateScansAnExplicitPath is the regression guard for
// holomush-vpvu5. The INV-STORE-3 lint MUST hand rg an explicit search path. A
// path-less `rg ... --type=go` reads stdin whenever stdin is not a TTY (CI
// wires every run-step's stdin to /dev/null; background agents attach idle
// pipes), so it scans empty stdin instead of the repo — passing silently while
// a violation sits in the tree (CI no-op), or blocking forever on a pipe.
// TestLintNoMicrosecondTruncatePasses cannot catch this: a no-op also passes a
// clean repo.
//
// This is a STATIC assertion on the Taskfile, not a shell-out, by design: no CI
// job both runs Go tests and has `rg` on PATH (the unit-test job has no rg, so
// a shell-out would itself no-op with "rg: executable not found"), and a static
// check has zero dependency on rg, task, stdin semantics, or git ignore rules.
func TestLintNoMicrosecondTruncateScansAnExplicitPath(t *testing.T) {
	taskfile := filepath.Join(repoRoot(t), "Taskfile.yaml")
	data, err := os.ReadFile(taskfile)
	if err != nil {
		t.Fatalf("read %s: %v", taskfile, err)
	}

	// The lone rg invocation enforcing INV-STORE-3 is the only line carrying all
	// of "rg -n", "--type=go", and "Microsecond"; the surrounding comment/echo
	// lines that mention the pattern have neither "rg -n" nor "--type=go".
	var rgLine string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "rg -n") && strings.Contains(line, "--type=go") &&
			strings.Contains(line, "Microsecond") {
			rgLine = line
			break
		}
	}
	if rgLine == "" {
		t.Fatal("could not locate the INV-STORE-3 rg invocation in Taskfile.yaml")
	}

	// Capture the first token after --type=go. The path-less form leaves only
	// the line-continuation backslash (or a pipe) there; the fixed form carries
	// a search path such as {{.ROOT_DIR}}.
	m := regexp.MustCompile(`--type=go\s+(\S+)`).FindStringSubmatch(rgLine)
	if m == nil {
		t.Fatalf("INV-STORE-3 rg invocation has nothing after --type=go: %q", rgLine)
	}
	if tok := m[1]; tok == `\` || tok == "|" {
		t.Fatalf("INV-STORE-3 rg invocation is path-less (token %q after --type=go) — under a "+
			"non-TTY stdin rg reads stdin, not the repo: silent no-op in CI / hang on a pipe "+
			"(holomush-vpvu5). Line: %s", tok, strings.TrimSpace(rgLine))
	}
}

// repoRoot walks up from the test's working directory to the directory holding
// the root Taskfile.yaml.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "Taskfile.yaml")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find Taskfile.yaml above the test working directory")
		}
		dir = parent
	}
}
