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

// TestLintNoTimestamptzScansTheStoreMigrationsAsSingleFileSQL pins the glob the
// INV-STORE-1 lint uses to walk the host migration corpus.
//
// Under goose a migration is ONE file per version, NNNNNN_name.sql; the
// golang-migrate-era *.up.sql / *.down.sql pairs are gone. A lint loop still
// globbing *.up.sql therefore matches nothing — and a `for` loop over zero files
// runs zero iterations, counts zero violations, and exits 0. INV-STORE-1's
// enforcement would retire silently, reported as clean.
//
// This is a STATIC assertion on the Taskfile text, not a shell-out, for the same
// reason its sibling above gives and one more: running `task lint:no-timestamptz`
// cannot distinguish "no violations" from "the glob matched no files", so a
// shell-out passes the exact failure mode this test exists to catch. (The
// unit-test CI job also has no `rg` on PATH, so a shell-out would no-op there
// regardless.)
func TestLintNoTimestamptzScansTheStoreMigrationsAsSingleFileSQL(t *testing.T) {
	const want = "internal/store/migrations/*.sql"

	got := soleTaskfileLoopGlob(t, "internal/store/migrations/")
	if got != want {
		t.Fatalf("INV-STORE-1 lint globs %q, want %q — goose migrations are single files "+
			"(NNNNNN_name.sql); a *.up.sql glob matches nothing, iterates zero times, and "+
			"exits 0, retiring the timestamp discipline silently", got, want)
	}
}

// TestLintNoTimestamptzStillScansPluginUpMigrationsSeparately pins the OTHER glob
// in the same lint — and it is a guard against a well-meaning sweep, not a
// leftover from the goose conversion.
//
// plugins/*/migrations/ is a SEPARATE migration system. It is applied by
// pkg/plugin/storage/storage.go, which reads *.up.sql files directly; goose never
// sees that directory and the conversion did not touch it. Widening this glob to
// *.sql to "match" the host corpus would make the lint scan .down.sql files the
// plugin migrator also carries, and — worse — would signal that the two systems
// converged when they have not. Narrowing or renaming it breaks the plugin
// migrator's timestamp discipline outright.
func TestLintNoTimestamptzStillScansPluginUpMigrationsSeparately(t *testing.T) {
	const want = "plugins/*/migrations/*.up.sql"

	got := soleTaskfileLoopGlob(t, "plugins/*/migrations/")
	if got != want {
		t.Fatalf("plugin migration lint globs %q, want %q — plugins/*/migrations/ is a "+
			"separate migrator (pkg/plugin/storage/storage.go applies *.up.sql only) that "+
			"goose never sees; it must NOT be swept along with the host corpus", got, want)
	}
}

// soleTaskfileLoopGlob returns the glob of the one `for f in <prefix>...; do` loop
// in Taskfile.yaml whose target begins with prefix.
//
// It fails when there is no such loop, and when there is more than one: either
// case means the caller's exact-string comparison would be asserting against
// something other than the loop it names, and a pin that is not observing its
// subject is indistinguishable from one that cannot fail.
func soleTaskfileLoopGlob(t *testing.T, prefix string) string {
	t.Helper()

	taskfile := filepath.Join(repoRoot(t), "Taskfile.yaml")
	data, err := os.ReadFile(taskfile)
	if err != nil {
		t.Fatalf("read %s: %v", taskfile, err)
	}

	loopPattern := regexp.MustCompile(`for\s+\S+\s+in\s+(` + regexp.QuoteMeta(prefix) + `\S*)\s*;`)

	var globs []string
	for _, line := range strings.Split(string(data), "\n") {
		if m := loopPattern.FindStringSubmatch(line); m != nil {
			globs = append(globs, m[1])
		}
	}

	switch len(globs) {
	case 1:
		return globs[0]
	case 0:
		t.Fatalf("no `for ... in %s...; do` loop found in %s — the INV-STORE-1 lint no longer "+
			"walks this corpus, so nothing enforces the timestamp discipline over it", prefix, taskfile)
	default:
		t.Fatalf("found %d `for ... in %s...; do` loops in %s (%v); this pin asserts against a "+
			"single loop and cannot tell which one it is guarding", len(globs), prefix, taskfile, globs)
	}
	return "" // unreachable: every switch arm above either returns or calls t.Fatalf.
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
