<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Tier-Split Quality Gates Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `Integration Test` + `E2E Test` required CI checks via a quarantine-then-promote mechanism, and shrink the mandatory local `task pr-prep` gate to the fast deterministic tier.

**Architecture:** A `quarantinetest` env-gate helper + a Playwright tag let known-flaky specs self-exclude from gating CI runs; a `test/quarantine.yaml` registry plus a bijection meta-test keep the set governed and shrinking. CI's existing `Integration Test`/`E2E Test` jobs run the quarantine-excluded set (names unchanged → check identity preserved); `nightly-soak` runs the quarantined set for a health signal. `task pr-prep` splits into a fast mandatory lane (no Docker/flock) and an opt-in `pr-prep:full`. The `protect-main` ruleset gains the two required contexts **last**, after all mechanism + tooling lands.

**Tech Stack:** Go (`testing`, table-driven + `test/meta` regex meta-tests), Ginkgo (`Skip`), Playwright (`tag` + `--grep-invert`), go-task `Taskfile.yaml`, GitHub Actions, `golangci-lint` depguard, `bd`, jj.

**Spec:** `docs/superpowers/specs/2026-05-25-tier-split-quality-gates-design.md` · **Design bead:** `holomush-b4myw`

---

## File structure

| File | Responsibility | Task |
| --- | --- | --- |
| `internal/testsupport/quarantinetest/quarantinetest.go` | env-gate helper: `Enabled()`, `Skip(t, bead)` | 1 |
| `internal/testsupport/quarantinetest/quarantinetest_test.go` | unit test for the helper (INV-1 Go proof) | 1 |
| `.golangci.yaml` | new depguard deny entry for `quarantinetest` | 2 |
| `test/meta/depguard_config_test.go` | extend to assert the new deny entry (INV-2-adjacent) | 2 |
| `test/quarantine.yaml` | the quarantine registry (ledger) | 3 |
| `test/meta/quarantine_registry_test.go` | bijection meta-test: marker set == registry set (INV-2) | 3 |
| (seed flake test files) | apply markers; register each (INV-1) | 4 |
| `.github/workflows/ci.yaml` | `E2E Test` job excludes `@quarantine` | 5 |
| `Taskfile.yaml` | `quarantine:audit` task; `pr-prep` fast lane + new `pr-prep:full` | 5, 7 |
| `scripts/quarantine-audit.sh` | local/pre-`bd close` bead-liveness audit (INV-3) | 5 |
| `.github/workflows/nightly-soak.yml` | nightly quarantine-set run + health report | 6 |
| `test/meta/pr_prep_fast_lane_test.go` | INV-4 meta-test | 7 |
| `.claude/agents/branch-readiness-check.md` | rewrite pr-prep-evidence criterion | 8 |
| `.claude/commands/pr-prep.md`, `.claude/commands/landing-sequence.md` | reword for fast/full lanes | 8 |
| `.claude/hooks/remind-pre-action-review.sh` | path-triggered int/e2e nudge | 9 |
| `.claude/hooks/enforce-task-runner.sh` | `test:integration`→`test:int` string fix | 9 |
| `test/meta/tooling_no_mandatory_int_test.go` | INV-6 grep-lint over `.claude/` | 9 |
| `test/meta/ci_required_jobs_test.go` | INV-5 meta-test | 10 |
| `CLAUDE.md`, `.claude/rules/landing-the-plane.md`, `site/docs/contributing/pr-prep.md`, `.claude/rules/testing.md`, `site/docs/contributing/quarantine.md` | docs | 10 |
| `protect-main` ruleset (GitHub) | add 2 required contexts | 11 |

---

## Phase 1: Quarantine mechanism (nothing gates yet)

### Task 1: `quarantinetest` env-gate helper

**Files:**

- Create: `internal/testsupport/quarantinetest/quarantinetest.go`
- Test: `internal/testsupport/quarantinetest/quarantinetest_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/testsupport/quarantinetest/quarantinetest_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package quarantinetest_test

import (
	"testing"

	"github.com/holomush/holomush/internal/testsupport/quarantinetest"
	"github.com/stretchr/testify/assert"
)

func TestEnabledReflectsEnvVar(t *testing.T) {
	t.Run("false when env unset", func(t *testing.T) {
		t.Setenv("HOLOMUSH_RUN_QUARANTINED", "")
		assert.False(t, quarantinetest.Enabled())
	})
	t.Run("true when env is 1", func(t *testing.T) {
		t.Setenv("HOLOMUSH_RUN_QUARANTINED", "1")
		assert.True(t, quarantinetest.Enabled())
	})
	t.Run("false for any other value", func(t *testing.T) {
		t.Setenv("HOLOMUSH_RUN_QUARANTINED", "true")
		assert.False(t, quarantinetest.Enabled())
	})
}

func TestSkipSkipsWhenDisabled(t *testing.T) {
	t.Setenv("HOLOMUSH_RUN_QUARANTINED", "")
	ranPast := false
	t.Run("subject", func(t *testing.T) {
		quarantinetest.Skip(t, "holomush-q55b")
		ranPast = true // unreachable when Skip fires
	})
	assert.False(t, ranPast, "code after Skip must not run when quarantine disabled")
}

func TestSkipRunsWhenEnabled(t *testing.T) {
	t.Setenv("HOLOMUSH_RUN_QUARANTINED", "1")
	ranPast := false
	t.Run("subject", func(t *testing.T) {
		quarantinetest.Skip(t, "holomush-q55b")
		ranPast = true
	})
	assert.True(t, ranPast, "code after Skip must run when quarantine enabled")
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `task test -- ./internal/testsupport/quarantinetest/`
Expected: FAIL — `package quarantinetest is not in std` / undefined `quarantinetest.Enabled`.

- [ ] **Step 3: Write the helper**

Create `internal/testsupport/quarantinetest/quarantinetest.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package quarantinetest gates known-flaky integration/E2E specs. A spec
// marked with Skip self-skips in gating runs (env unset) and runs in the
// nightly lane (HOLOMUSH_RUN_QUARANTINED=1). Every Skip MUST cite an open
// bead and have a matching row in test/quarantine.yaml (enforced by the
// bijection meta-test in test/meta). Production code MUST NOT import this
// package (depguard-enforced). See
// docs/superpowers/specs/2026-05-25-tier-split-quality-gates-design.md.
package quarantinetest

import (
	"os"
	"testing"
)

// EnvVar toggles whether quarantined specs run. Set to "1" in the nightly
// lane; unset everywhere else (so quarantined specs self-skip in gating CI).
const EnvVar = "HOLOMUSH_RUN_QUARANTINED"

// Enabled reports whether quarantined specs should run.
func Enabled() bool { return os.Getenv(EnvVar) == "1" }

// Skip skips the test as quarantined unless Enabled(). bead MUST be the
// tracking bead id (e.g. "holomush-q55b") and MUST appear in
// test/quarantine.yaml.
func Skip(t *testing.T, bead string) {
	t.Helper()
	if !Enabled() {
		t.Skipf("quarantined: %s (set %s=1 to run)", bead, EnvVar)
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `task test -- ./internal/testsupport/quarantinetest/`
Expected: PASS (all three functions).

- [ ] **Step 5: Lint**

Run: `task lint:go`
Expected: clean (no wrapcheck/sloglint issues; pure helper).

- [ ] **Step 6: Commit**

```bash
jj describe -m "test(quarantine): add quarantinetest env-gate helper (holomush-b4myw)"
```

---

### Task 2: depguard deny entry for `quarantinetest`

**Files:**

- Modify: `.golangci.yaml` (depguard `deny` list, currently `.golangci.yaml:148-152`)
- Test: `test/meta/depguard_config_test.go:24-30` (extend the package loop)

- [ ] **Step 1: Extend the meta-test (failing first)**

In `test/meta/depguard_config_test.go`, add the new package to the loop in `TestDepguardTestOnlyConstructRulesPresent`:

```go
	for _, pkg := range []string{
		"github.com/holomush/holomush/internal/eventbus/eventbustest",
		"github.com/holomush/holomush/internal/core/coretest",
		"github.com/holomush/holomush/internal/testsupport/quarantinetest",
	} {
		require.Contains(t, cfg, pkg,
			"depguard deny rule for %q missing from .golangci.yaml", pkg)
	}
```

- [ ] **Step 2: Run the meta-test to verify it fails**

Run: `task test -- ./test/meta/ -run TestDepguardTestOnlyConstructRulesPresent`
Expected: FAIL — config does not contain `.../quarantinetest`.

- [ ] **Step 3: Add the deny entry**

In `.golangci.yaml`, append to the `deny:` list under `no-test-only-constructs-in-production` (after the `coretest` entry at line 152):

```yaml
            - pkg: github.com/holomush/holomush/internal/testsupport/quarantinetest
              desc: "quarantine skip helper; production code MUST NOT import it (holomush-b4myw)"
```

(The `files:` exclusion already lists `!**/internal/testsupport/**`, so the helper package itself is exempt from the rule — only production importers are denied.)

- [ ] **Step 4: Run the meta-test to verify it passes**

Run: `task test -- ./test/meta/ -run TestDepguardTestOnlyConstructRulesPresent`
Expected: PASS.

- [ ] **Step 5: Verify depguard actually fires (manual probe, then revert)**

Add a temporary USED import to a production file (NOT a blank import — `revive`'s blank-imports rule fires first and masks depguard; per `holomush-1eps2` lesson). In `internal/core/engine.go` add at package scope: `var _ = quarantinetest.Enabled` with the import, then:

Run: `task lint:go`
Expected: FAIL — depguard reports `quarantinetest` import denied in production.
Then **revert** the probe edit and re-run `task lint:go` → clean.

- [ ] **Step 6: Commit**

```bash
jj describe -m "lint(quarantine): deny quarantinetest import from production via depguard (holomush-b4myw)"
```

---

### Task 3: registry + bijection meta-test

**Files:**

- Create: `test/quarantine.yaml`
- Create: `test/meta/quarantine_registry_test.go`

- [ ] **Step 1: Create an empty-but-valid registry**

Create `test/quarantine.yaml`:

```yaml
# Quarantine registry — known-flaky integration/E2E specs.
#
# Each entry MUST reference an open/in-progress bead and have a matching
# in-code marker (quarantinetest.Skip / Ginkgo Skip / Playwright @quarantine
# tag). The bijection meta-test (test/meta/quarantine_registry_test.go)
# fails the build if a marker lacks a row or a row lacks a marker.
# `task quarantine:audit` (run where bd is reachable) flags rows whose bead
# is closed. See docs/superpowers/specs/2026-05-25-tier-split-quality-gates-design.md.
#
# entries: list of { id, kind (go|ginkgo|playwright), bead, since, reason }
entries: []
```

- [ ] **Step 2: Write the bijection meta-test (failing)**

Create `test/meta/quarantine_registry_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// beadRE matches a holomush bead id wherever it appears (registry rows or
// in-code markers).
var beadRE = regexp.MustCompile(`holomush-[a-z0-9.]+`)

// registryBeadRE pulls the bead id from a `bead:` key line in the registry.
var registryBeadRE = regexp.MustCompile(`(?m)^\s*bead:\s*(holomush-[a-z0-9.]+)`)

// markerLineRE matches an in-code quarantine marker line and is used to scope
// bead extraction to lines that actually mark a spec quarantined:
//   - Go:        quarantinetest.Skip(t, "holomush-xxxx")
//   - Ginkgo:    Skip("quarantined: holomush-xxxx")  (or Label("quarantine", "holomush-xxxx"))
//   - Playwright '@quarantine' tag line carries an adjacent '@holomush-xxxx' tag
var markerLineRE = regexp.MustCompile(`quarantinetest\.Skip\(|quarantined:|@quarantine|Label\("quarantine"`)

// TestQuarantineRegistryBijection enforces INV-2: every in-code quarantine
// marker maps to exactly one test/quarantine.yaml row and vice versa.
func TestQuarantineRegistryBijection(t *testing.T) {
	root := findRepoRoot(t)

	registry := registryBeads(t, root)
	markers := markerBeads(t, root)

	sort.Strings(registry)
	sort.Strings(markers)

	require.Equal(t, registry, markers,
		"quarantine marker set and test/quarantine.yaml must be identical "+
			"(registry=%v markers=%v)", registry, markers)
}

func registryBeads(t *testing.T, root string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "test", "quarantine.yaml"))
	require.NoError(t, err, "read test/quarantine.yaml")
	out := newQuarantineBeadSet()
	for _, m := range registryBeadRE.FindAllStringSubmatch(string(data), -1) {
		out.add(m[1])
	}
	return out.slice()
}

func markerBeads(t *testing.T, root string) []string {
	t.Helper()
	out := newQuarantineBeadSet()
	rootFS, err := os.OpenRoot(root)
	require.NoError(t, err, "open repo root")
	defer func() { _ = rootFS.Close() }()

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if _, skip := skipDirs[d.Name()]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		// Scan Go test files and Playwright specs only.
		isGoTest := strings.HasSuffix(name, "_test.go")
		isSpec := strings.HasSuffix(name, ".spec.ts")
		if !isGoTest && !isSpec {
			return nil
		}
		// Never count this meta-test's own regex literals as markers.
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if rel == filepath.Join("test", "meta", "quarantine_registry_test.go") {
			return nil
		}
		f, openErr := rootFS.Open(rel)
		if openErr != nil {
			return openErr
		}
		data, readErr := io.ReadAll(f)
		_ = f.Close()
		if readErr != nil {
			return readErr
		}
		for _, line := range strings.Split(string(data), "\n") {
			if !markerLineRE.MatchString(line) {
				continue
			}
			for _, b := range beadRE.FindAllString(line, -1) {
				out.add(b)
			}
		}
		return nil
	})
	require.NoError(t, err, "walk repo for markers")
	return out.slice()
}

// quarantineBeadSet is a tiny string set used to collect bead ids without duplicates.
type quarantineBeadSet struct{ m map[string]struct{} }

func newQuarantineBeadSet() *quarantineBeadSet { return &quarantineBeadSet{m: map[string]struct{}{}} }
func (s *quarantineBeadSet) add(v string)      { s.m[v] = struct{}{} }
func (s *quarantineBeadSet) slice() []string {
	out := make([]string, 0, len(s.m))
	for k := range s.m {
		out = append(out, k)
	}
	return out
}
```

- [ ] **Step 3: Run the meta-test to verify it passes on the empty registry**

Run: `task test -- ./test/meta/ -run TestQuarantineRegistryBijection`
Expected: PASS (empty registry, zero markers — both sets empty and equal).

- [ ] **Step 4: Commit**

```bash
jj describe -m "test(quarantine): registry + bijection meta-test (INV-2) (holomush-b4myw)"
```

---

### Task 4: apply quarantine markers to the seed flakes

**Files (verified):**

- Modify: `internal/eventbus/audit/projection_test.go:207` (`TestProjectionResumesAfterRestart` — `q55b`/`5zpf`), `:132` (`TestProjectionDrainsPublishedMessageToAuditTable` — `1nl7`)
- Modify: `web/e2e/terminal.spec.ts` (`0jzs` reconnect/session)
- Modify: `test/quarantine.yaml`

**Files (re-derive at execution — `rg`/`bd show` did not pin these by their bead-title strings; per spec §9 the set is re-derived from `bd`):** `holomush-7b9n` (`eventbus_e2e` Ginkgo, gates in `Integration Test`), `holomush-tmrv` (`test/integration/crypto/` Ginkgo/Go), `holomush-pqzv` (`TestMigrator_ConcurrentUp`), `holomush-h9fp` (telnet disconnect E2E).

- [ ] **Step 1: Mark the audit flakes (Go env-gate — concrete example)**

In `internal/eventbus/audit/projection_test.go`, add the import (the file is `package audit_test`, `//go:build integration`):

```go
import (
	// ... existing imports ...
	"github.com/holomush/holomush/internal/testsupport/quarantinetest"
)
```

As the first statement inside `TestProjectionResumesAfterRestart` (line 207) and `TestProjectionDrainsPublishedMessageToAuditTable` (line 132):

```go
func TestProjectionResumesAfterRestart(t *testing.T) {
	quarantinetest.Skip(t, "holomush-q55b")
	// ... existing body ...
}
```

`holomush-5zpf` is a duplicate report of `q55b` (same spec, same race). Close it as a dup rather than registering a second row for one marker:

```bash
bd close holomush-5zpf --reason "duplicate of holomush-q55b (same spec TestProjectionResumesAfterRestart); tracked there"
```

The seed-list is therefore **7** registry rows, not 8.

```go
func TestProjectionDrainsPublishedMessageToAuditTable(t *testing.T) {
	quarantinetest.Skip(t, "holomush-1nl7")
	// ... existing body ...
}
```

- [ ] **Step 2: Mark the remaining Go integration flakes**

For `holomush-7b9n`, `holomush-tmrv`, `holomush-pqzv`: run `bd show <bead>` to read the spec identity, then locate it:

```bash
bd show holomush-pqzv   # confirms TestMigrator_ConcurrentUp
rg -n "func TestMigrator_ConcurrentUp" --type go
```

Apply the matching marker:

- **Plain-`testing.T`** (e.g. `pqzv`, `tmrv` if Go test funcs): `quarantinetest.Skip(t, "holomush-pqzv")` as the first statement (add the import).
- **Ginkgo** (e.g. `7b9n` in `test/integration/eventbus_e2e/`): inside the `It(...)` block, first line:

```go
It("...", func() {
	if !quarantinetest.Enabled() {
		Skip("quarantined: holomush-7b9n")
	}
	// ... existing body ...
})
```

- [ ] **Step 3: Mark the Playwright flakes (tag — concrete example)**

In `web/e2e/terminal.spec.ts`, add the `@quarantine` tag plus the bead tag to the flaky test's options (locate the specific `test(...)` from `bd show holomush-0jzs`):

```ts
test('reconnects after a dropped session', { tag: ['@quarantine', '@holomush-0jzs'] }, async ({ page }) => {
	// ... existing body ...
});
```

Repeat for `holomush-h9fp` once its spec file is located (`bd show holomush-h9fp` → `rg` in `web/e2e`).

- [ ] **Step 4: Populate the registry**

In `test/quarantine.yaml`, replace `entries: []` with one row per marked spec:

```yaml
entries:
  - id: TestProjectionResumesAfterRestart
    kind: go
    bead: holomush-q55b
    since: 2026-05-25
    reason: consumer-info eventual-consistency race on restart (5zpf closed as dup)
  - id: TestProjectionDrainsPublishedMessageToAuditTable
    kind: go
    bead: holomush-1nl7
    since: 2026-05-25
    reason: AwaitDrained cold-start race
  - id: eventbus_e2e F-E12 chain verification
    kind: ginkgo
    bead: holomush-7b9n
    since: 2026-05-25
    reason: operator_read_completed audit row times out under load
  - id: crypto integration suite startup
    kind: go
    bead: holomush-tmrv
    since: 2026-05-25
    reason: Docker testcontainer startup timeout under load
  - id: TestMigrator_ConcurrentUp
    kind: go
    bead: holomush-pqzv
    since: 2026-05-25
    reason: docker port-map timeout
  - id: telnet disconnect via quit
    kind: playwright
    bead: holomush-h9fp
    since: 2026-05-25
    reason: flaky telnet disconnect E2E
  - id: terminal reconnect/session
    kind: playwright
    bead: holomush-0jzs
    since: 2026-05-25
    reason: flaky terminal.spec.ts reconnect/session
```

> `holomush-5zpf` was closed as a duplicate of `q55b` in Step 1, so it has **no** marker and **no** registry row. The bijection set is exactly the 7 beads marked in the test files = the 7 `bead:` rows here. Do **not** add a `5zpf` marker or a `5zpf` comment to the q55b marker line — `markerBeads` scans every `holomush-*` token on a marker line, so a stray `holomush-5zpf` in a comment would break the bijection (markers=8 vs registry=7).

- [ ] **Step 5: Verify bijection + gating-skip behavior**

```bash
task test -- ./test/meta/ -run TestQuarantineRegistryBijection
```

Expected: PASS (marker bead set == registry bead set).

```bash
task test:int -- ./internal/eventbus/audit/
```

Expected: PASS, with `TestProjectionResumesAfterRestart` and `TestProjectionDrainsPublishedMessageToAuditTable` reported **SKIP** (quarantined).

```bash
HOLOMUSH_RUN_QUARANTINED=1 task test:int -- ./internal/eventbus/audit/
```

Expected: the two specs now RUN (may flake — that's expected, this is the nightly behavior).

- [ ] **Step 6: Commit**

```bash
jj describe -m "test(quarantine): quarantine 7 known int/e2e flakes + register (holomush-b4myw)"
```

---

## Phase 2: CI wiring

### Task 5: E2E gating exclusion + `quarantine:audit` task + audit script

**Files:**

- Modify: `.github/workflows/ci.yaml:235` (E2E Test job run step)
- Create: `scripts/quarantine-audit.sh`
- Modify: `Taskfile.yaml` (add `quarantine:audit` task)

- [ ] **Step 1: Exclude `@quarantine` from the gating E2E job**

In `.github/workflows/ci.yaml`, change the E2E Test job's run step (line 235) from:

```yaml
        run: task test:e2e:cover
```

to:

```yaml
        run: task test:e2e:cover -- --grep-invert @quarantine
```

(The `Integration Test` job at line 185 needs **no change** — `HOLOMUSH_RUN_QUARANTINED` is unset there, so quarantined Go specs self-skip via the helper.)

- [ ] **Step 2: Write the audit script**

Create `scripts/quarantine-audit.sh`:

```bash
#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Fails if any bead referenced by test/quarantine.yaml is closed (fix landed
# but the spec was never un-quarantined). Run locally / before `bd close`;
# requires `bd` on PATH with a reachable beads DB. INV-3.
set -euo pipefail

REG="test/quarantine.yaml"
[ -f "$REG" ] || { echo "no $REG; nothing to audit"; exit 0; }

if ! command -v bd >/dev/null 2>&1; then
  echo "quarantine:audit: bd not on PATH — skipping (run where bd is reachable)" >&2
  exit 0
fi

rc=0
# Process substitution (not a pipe) keeps the loop in the MAIN shell so that
# rc mutations survive — a `... | while read` loop runs in a subshell and
# `exit "$rc"` would always be 0 (the audit would be a silent no-op).
while read -r bead; do
  [ -n "$bead" ] || continue
  status=$(bd show "$bead" --json 2>/dev/null | jq -r '.[0].status // "unknown"')
  if [ "$status" = "closed" ]; then
    echo "QUARANTINE AUDIT: $bead is closed but still quarantined — un-quarantine it." >&2
    rc=1
  fi
done < <(grep -oE 'bead:[[:space:]]*holomush-[a-z0-9.]+' "$REG" | awk '{print $2}' | sort -u)
exit "$rc"
```

Make it executable: `chmod +x scripts/quarantine-audit.sh`.

- [ ] **Step 3: Add the `quarantine:audit` task**

In `Taskfile.yaml`, add (near the other `test:*` tasks):

```yaml
  quarantine:audit:
    desc: Fail if any quarantined spec's bead is closed (needs bd; run locally / pre-bd-close). INV-3.
    cmds:
      - bash scripts/quarantine-audit.sh
```

- [ ] **Step 4: Verify the audit passes (all seed beads open)**

Run: `task quarantine:audit`
Expected: exit 0 (all 7 beads open). Temporarily edit `test/quarantine.yaml` to reference a known-closed bead (e.g. `holomush-1eps2`) → expect a non-zero "is closed but still quarantined" error; then revert.

- [ ] **Step 5: Lint the workflow + script**

Run: `task lint:actions` and `task lint` (shellcheck runs on scripts via lint).
Expected: clean. (Note: actionlint+shellcheck on workflow `run:` blocks is CI-only for some rules — keep the run-step change a single token.)

- [ ] **Step 6: Commit**

```bash
jj describe -m "ci(quarantine): exclude @quarantine from gating E2E; add quarantine:audit (holomush-b4myw)"
```

---

### Task 6: nightly quarantine run + health report

**Files:**

- Modify: `.github/workflows/nightly-soak.yml` (add a `quarantine` job)

- [ ] **Step 1: Add the quarantine job**

In `.github/workflows/nightly-soak.yml`, add a second job after `soak` (mirror its setup steps):

```yaml
  quarantine:
    name: Quarantine Health
    runs-on: namespace-profile-linux-amd64-4x8
    timeout-minutes: 30
    steps:
      - uses: actions/checkout@de0fac2e4500dabe0009e67214ff5f5447ce83dd # v6
      - name: Setup Testcontainers Cloud Client
        uses: atomicjar/testcontainers-cloud-setup-action@7d1bab3fdfe0027c91936deca6b924d8a8a7a04d # v1
        with:
          token: ${{ secrets.TC_CLOUD_TOKEN }}
      - name: Set up Go
        uses: actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c # v6
        with:
          go-version-file: go.mod
          cache: false
      - name: Cache Go modules and build
        uses: namespacelabs/nscloud-cache-action@15799a6b54e5765f85b2aac25b3f0df43ed571c0 # v1
        with:
          cache: go
      - name: Install Task
        uses: ./.github/actions/install-task
      # Run the quarantined Go specs (health signal only; never gates merge).
      # continue-on-error: known-flaky by definition — a red here is expected
      # and must not fail the nightly workflow.
      - name: Run quarantined integration specs
        continue-on-error: true
        env:
          HOLOMUSH_RUN_QUARANTINED: "1"
        run: task test:int
      - name: Run quarantined E2E specs
        continue-on-error: true
        run: task test:e2e:cover -- --grep @quarantine
```

> Rationale for `continue-on-error`: per the §3.3 design the nightly run is a **health report**, not a gate. A quarantined spec that now passes N consecutive nights is an un-quarantine candidate (read from the job's logs/annotations). The bead-closure audit (`task quarantine:audit`, Task 5) runs locally where `bd` is reachable — the spec's open-item is resolved in favor of **not** provisioning `bd` on the ephemeral runner (the Dolt shared-server isn't reachable from CI; the health signal needs no `bd`).

- [ ] **Step 2: Lint the workflow**

Run: `task lint:actions`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
jj describe -m "ci(quarantine): nightly quarantine-set health run (holomush-b4myw)"
```

---

## Phase 3: Local gate shrink + tooling + docs

### Task 7: split `pr-prep` into fast lane + `pr-prep:full`

**Files:**

- Modify: `Taskfile.yaml:718-799` (`pr-prep`), and add `pr-prep:full`
- Create: `test/meta/pr_prep_fast_lane_test.go`

- [ ] **Step 1: Write the INV-4 meta-test (failing)**

Create `test/meta/pr_prep_fast_lane_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// taskBlock returns the Taskfile body of `<name>:` up to the next 2-space task key.
func taskBlock(t *testing.T, tf, name string) string {
	t.Helper()
	loc := regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(name) + `:[ \t]*$`).FindStringIndex(tf)
	require.NotNil(t, loc, "%s target not found in Taskfile.yaml", name)
	after := tf[loc[1]:]
	if next := regexp.MustCompile(`(?m)^  \S`).FindStringIndex(after); next != nil {
		return after[:next[0]]
	}
	return after
}

// TestPrPrepFastLaneExcludesHeavyTiers enforces INV-4: the mandatory pr-prep
// (non-full) lane must not run test:int/test:e2e and must not flock; the
// heavy tiers live only in pr-prep:full.
func TestPrPrepFastLaneExcludesHeavyTiers(t *testing.T) {
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "Taskfile.yaml"))
	require.NoError(t, err, "read Taskfile.yaml")
	tf := string(data)

	fast := taskBlock(t, tf, "pr-prep")
	require.NotContains(t, fast, "test:int", "pr-prep (fast) must not run integration tests (INV-4)")
	require.NotContains(t, fast, "test:e2e", "pr-prep (fast) must not run E2E tests (INV-4)")
	require.NotContains(t, fast, "flock", "pr-prep (fast) must not acquire the flock (INV-4)")

	full := taskBlock(t, tf, "pr-prep:full")
	require.Contains(t, full, "flock", "pr-prep:full must keep the flock")
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `task test -- ./test/meta/ -run TestPrPrepFastLaneExcludesHeavyTiers`
Expected: FAIL — current `pr-prep` contains `flock`; `pr-prep:full` not found.

- [ ] **Step 3: Restructure the Taskfile**

Rename the current `pr-prep:run` body's heavy tail into `pr-prep:full`, and make `pr-prep` the fast lane. Concretely:

(a) Rename the existing `pr-prep:` task (the flock wrapper, `:718-799`) to `pr-prep:full:` — keep its body verbatim (docs detection, result-file `lane=full`/`docs`, flock, `pr-prep:run`). It remains the opt-in full gate.

(b) Add a new `pr-prep:` fast lane that writes a `lane=fast` result file and runs only the deterministic tier:

```yaml
  pr-prep:
    desc: Fast pre-push gate (schema/license/lint/fmt/unit/build/bats; no Docker, no flock). Mandatory before push. Use 'pr-prep:full' for int+e2e.
    cmds:
      - cmd: |
          set -euo pipefail
          LOCK_DIR="${TMPDIR:-/tmp}/holomush-pr-prep"
          RUN_DIR="$LOCK_DIR/runs"
          mkdir -p "$RUN_DIR"
          RESULT="$RUN_DIR/$(date -u +%Y%m%dT%H%M%SZ)-$$.result"
          printf '▸ pr-prep result: %s\n' "$RESULT"
          write_result() {
            printf 'status=%s\nlane=%s\nexit=%s\nfinished_at=%s\n' \
              "$1" "$2" "$3" "$(date -u +%FT%TZ)" > "$RESULT"
          }
          # Docs-only diffs still delegate to the docs lane.
          if [ "${HOLOMUSH_PR_PREP_FORCE_FULL:-}" = "1" ]; then
            exec task pr-prep:full
          fi
          timeout 5 git fetch -q origin main 2>/dev/null || true
          CHANGED=$(timeout 5 git diff --name-only origin/main...HEAD 2>/dev/null || true)
          if [ -n "$CHANGED" ]; then
            DOCS_REGEX=$(bash scripts/docs-paths-regex.sh)
            if ! printf '%s\n' "$CHANGED" | grep -vE "$DOCS_REGEX" >/dev/null; then
              echo "▸ docs-only diff detected; running pr-prep:docs"
              drc=0; task pr-prep:docs || drc=$?
              if [ "$drc" -eq 0 ]; then write_result pass docs 0; else write_result fail docs "$drc"; fi
              exit "$drc"
            fi
          fi
          rc=0; task pr-prep:fast:run || rc=$?
          if [ "$rc" -eq 0 ]; then write_result pass fast 0; else write_result fail fast "$rc"; fi
          exit "$rc"

  pr-prep:fast:run:
    desc: Inner body of the fast lane (no result file; called by pr-prep).
    cmds:
      - echo "▸ Running bats shell tests..."
      - task: test:bats
      - echo "▸ Verifying schema is current..."
      - cmd: |
          SCHEMA=schemas/plugin.schema.json
          BEFORE=$(sha256sum "$SCHEMA" | cut -d' ' -f1)
          go generate ./internal/plugin/
          AFTER=$(sha256sum "$SCHEMA" | cut -d' ' -f1)
          if [ "$BEFORE" != "$AFTER" ]; then
            echo "ERROR: Schema out of sync with Go types. Run 'task generate:schema' and commit."
            exit 1
          fi
      - echo "▸ Checking license headers..."
      - task: license:check
      - echo "▸ Building binary plugins..."
      - task: plugin:build-all
      - echo "▸ Running linters..."
      - task: lint
      - echo "▸ Checking formatting..."
      - task: fmt:check
      - echo "▸ Running unit tests..."
      - task: test
      - echo "▸ Building..."
      - task: build
      - echo "✓ Fast PR checks passed (run 'task pr-prep:full' for integration + E2E)."
```

(The `HOLOMUSH_PR_PREP_FORCE_FULL=1` env var now routes fast→full, preserving the existing escape hatch.)

- [ ] **Step 4: Run the meta-test + a real fast run**

Run: `task test -- ./test/meta/ -run TestPrPrepFastLaneExcludesHeavyTiers`
Expected: PASS.

Run: `task pr-prep`
Expected: runs bats→schema→license→plugins→lint→fmt→unit→build, prints `✓ Fast PR checks passed`, writes a `lane=fast` result file. No Docker, no flock-contention message.

- [ ] **Step 5: Commit**

```bash
jj describe -m "build(pr-prep): fast mandatory lane + opt-in pr-prep:full (INV-4) (holomush-b4myw)"
```

---

### Task 8: rewrite the readiness agent + pr-prep commands

**Files:**

- Modify: `.claude/agents/branch-readiness-check.md` (`### 4. pr-prep evidence` + `### 5`/`Do NOT run` mentions)
- Modify: `.claude/commands/pr-prep.md`
- Modify: `.claude/commands/landing-sequence.md:24`

- [ ] **Step 1: Rewrite `### 4. pr-prep evidence`**

Replace the `### 4. pr-prep evidence` section in `.claude/agents/branch-readiness-check.md` with:

```markdown
### 4. pr-prep evidence

- Search recent shell history / scrollback for `task pr-prep` (the fast lane) output / result file. If you can't find evidence the fast gate ran green, NOT READY — the fast `task pr-prep` (schema/license/lint/fmt/unit/build) MUST run before push.
- **Integration / E2E are CI-authoritative, not a local READY gate.** They run as required checks (`Integration Test`, `E2E Test`) in CI, which has not run at pre-push time. So do NOT require local `task test:int`/`task test:e2e` evidence. If the diff touches the int/e2e surface (`test/integration/**`, `web/e2e/**`, integration-tagged packages), targeted `task test:int -- ./<domain>` or `task pr-prep:full` is RECOMMENDED but not blocking.
- If the fast pr-prep was run and failed, NOT READY.
- For `.claude/` or doc-only changes, the docs lane runs automatically AND `task lint:docs-symmetry` MUST pass when `CLAUDE.md` or `AGENTS.md` were touched.
```

- [ ] **Step 2: Rewrite `.claude/commands/pr-prep.md`**

Replace the body to describe the lane split: the fast mandatory lane (schema/license/lint/fmt/unit/build) is the default `task pr-prep`; `task pr-prep:full` runs the full int+E2E gate behind the flock; int/E2E are CI-required. Keep the April-2026 "no partial-and-claim-green" lesson but scope it to whichever lane is run. (Full replacement text — write the whole file, no placeholders: front-matter `description:` updated to "Run the appropriate pr-prep lane (fast by default; full for int+e2e) and surface the first failure clearly"; Procedure section documents `task pr-prep` fast + `task pr-prep:full` + reading the `lane=` result file.)

- [ ] **Step 3: Fix `landing-sequence.md:24`**

In `.claude/commands/landing-sequence.md`, replace the pr-prep gate bullet (line 24) with:

```markdown
   - MUST run the fast `task pr-prep` (schema + license + lint + fmt + unit + build) green before push. Integration + E2E are required CI checks (`Integration Test`, `E2E Test`) — they gate the PR in CI, not locally. If you touched `test/integration/**`, `web/e2e/**`, or integration-tagged packages, run targeted `task test:int -- ./<domain>` or `task pr-prep:full` first (recommended, not mandatory).
```

- [ ] **Step 4: Verify no broken references**

Run: `rg -n "to full completion before push" .claude/`
Expected: zero matches (the banned phrase — INV-6, Task 9 enforces this).

- [ ] **Step 5: Commit**

```bash
jj describe -m "docs(tooling): readiness agent + pr-prep commands for lane split (holomush-b4myw)"
```

---

### Task 9: hook nudge + string fix + INV-6 meta-test

**Files:**

- Modify: `.claude/hooks/remind-pre-action-review.sh` (add path-triggered int/e2e nudge)
- Modify: `.claude/hooks/enforce-task-runner.sh:138` (`test:integration`→`test:int`)
- Create: `test/meta/tooling_no_mandatory_int_test.go`

- [ ] **Step 1: Fix the stale task name**

In `.claude/hooks/enforce-task-runner.sh` line 138, change:

```bash
            echo "Use 'task test:integration' instead of 'go test -tags=...'" >&2
```

to:

```bash
            echo "Use 'task test:int' instead of 'go test -tags=...'" >&2
```

- [ ] **Step 2: Add the path-triggered nudge**

In `.claude/hooks/remind-pre-action-review.sh`, after the `abac-reviewer` block (line 71), add:

```bash
# int/e2e surface nudge: handoff intent AND the diff touches integration/E2E
# paths. Integration+E2E are CI-required (not a local mandatory gate), but a
# targeted local run before push catches breakage a CI round-trip slower.
if [ "$handoff_intent" = "1" ] && printf '%s' "$changed_paths" | grep -qE '(test/integration/|web/e2e/|_integration_test\.go|\.spec\.ts)'; then
  reminders+=("**int/e2e surface touched:** \`Integration Test\` / \`E2E Test\` are CI-required checks. A targeted local run before push (\`task test:int -- ./<domain>\` or \`task pr-prep:full\`) catches failures a CI round-trip sooner — recommended, not mandatory (CI is authoritative).")
fi
```

- [ ] **Step 3: Write the INV-6 meta-test (failing if banned phrase present)**

Create `test/meta/tooling_no_mandatory_int_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestToolingDoesNotMandateLocalIntE2E enforces INV-6: no .claude/ tooling
// artifact may assert that local int/e2e is mandatory before push. The old
// "to full completion before push" phrasing encoded exactly that rule.
func TestToolingDoesNotMandateLocalIntE2E(t *testing.T) {
	root := findRepoRoot(t)
	const banned = "to full completion before push"
	claudeDir := filepath.Join(root, ".claude")

	var offenders []string
	err := filepath.WalkDir(claudeDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		// Skip this guard's own source so its literal doesn't self-trip.
		if strings.HasSuffix(path, "tooling_no_mandatory_int_test.go") {
			return nil
		}
		f, openErr := os.Open(path) //nolint:gosec // path from controlled WalkDir under .claude/
		if openErr != nil {
			return openErr
		}
		data, readErr := io.ReadAll(f)
		_ = f.Close()
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(data), banned) {
			rel, _ := filepath.Rel(root, path)
			offenders = append(offenders, rel)
		}
		return nil
	})
	require.NoError(t, err, "walk .claude/")
	require.Empty(t, offenders,
		"these .claude/ artifacts still assert the old full-gate-before-push rule (INV-6): %v", offenders)
}
```

- [ ] **Step 4: Run the meta-test**

Run: `task test -- ./test/meta/ -run TestToolingDoesNotMandateLocalIntE2E`
Expected: PASS (Task 8 already removed the banned phrase). If FAIL, the offenders list shows which file still has it — fix it.

- [ ] **Step 5: Smoke-test the hooks**

```bash
echo '{"prompt":"push this branch","cwd":"'"$PWD"'"}' | .claude/hooks/remind-pre-action-review.sh
```

Expected: emits the code-reviewer reminder (and, if the workspace diff touches int/e2e paths, the new surface nudge).

- [ ] **Step 6: Commit**

```bash
jj describe -m "hooks(gates): int/e2e surface nudge + test:int string fix + INV-6 guard (holomush-b4myw)"
```

---

### Task 10: docs + INV-5 meta-test

**Files:**

- Modify: `CLAUDE.md` (pr-prep MUST rule + Commands section), `.claude/rules/landing-the-plane.md` (step 2), `site/docs/contributing/pr-prep.md` (lane split), `.claude/rules/testing.md` (tier table + quarantine concept)
- Create: `site/docs/contributing/quarantine.md`
- Create: `test/meta/ci_required_jobs_test.go`

- [ ] **Step 1: Write the INV-5 meta-test (failing if a job name is missing)**

Create `test/meta/ci_required_jobs_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCIRequiredJobNamesPresent enforces INV-5: both the real CI workflow and
// the docs-skip workflow must define jobs named "Integration Test" and
// "E2E Test" so the required checks resolve (incl. on docs-only PRs).
func TestCIRequiredJobNamesPresent(t *testing.T) {
	root := findRepoRoot(t)
	for _, wf := range []string{
		filepath.Join(".github", "workflows", "ci.yaml"),
		filepath.Join(".github", "workflows", "ci-docs-skip.yaml"),
	} {
		data, err := os.ReadFile(filepath.Join(root, wf))
		require.NoError(t, err, "read %s", wf)
		body := string(data)
		require.Contains(t, body, "name: Integration Test", "%s missing 'Integration Test' job (INV-5)", wf)
		require.Contains(t, body, "name: E2E Test", "%s missing 'E2E Test' job (INV-5)", wf)
	}
}
```

- [ ] **Step 2: Run it (passes against current files)**

Run: `task test -- ./test/meta/ -run TestCIRequiredJobNamesPresent`
Expected: PASS (both workflows already define both names).

- [ ] **Step 3: Update `CLAUDE.md`**

In the `## Commands` section, replace the `MUST run task pr-prep before creating a PR` paragraph with the lane-split rule: the **fast** `task pr-prep` (schema/license/lint/fmt/unit/build, no Docker/flock) is mandatory before push; `task pr-prep:full` runs the full int+E2E gate (flock-serialized) and is opt-in / for when you touched that surface; **`Integration Test` + `E2E Test` are required CI checks** that protect `main`. Keep the docs-lane note.

- [ ] **Step 4: Update `.claude/rules/landing-the-plane.md`**

In step 2 ("Run `task pr-prep`"), change to: run the fast `task pr-prep` green before push; int+E2E gate in CI as required checks; `task pr-prep:full` recommended when the diff touches int/e2e. Preserve the April-2026 "single command, no subset" warning scoped to whichever lane runs.

- [ ] **Step 5: Update `site/docs/contributing/pr-prep.md`**

Add a "Lanes" section: fast (default, mandatory), full (`pr-prep:full`, flock), docs (auto). Document that int/E2E are CI-required and quarantine-excluded; cross-link `quarantine.md`. Keep the existing lock/contention content under the full lane.

- [ ] **Step 6: Update `.claude/rules/testing.md`**

In the Test Tiers section, add a "Quarantine" subsection: the three marker idioms (`quarantinetest.Skip` / Ginkgo `Skip` under `quarantinetest.Enabled()` / Playwright `@quarantine` tag), the `test/quarantine.yaml` registry, the bijection meta-test, `task quarantine:audit`, and that quarantined specs run only nightly + `HOLOMUSH_RUN_QUARANTINED=1`.

- [ ] **Step 7: Create `site/docs/contributing/quarantine.md`**

Write the contributor guide: when to quarantine (a flake with an open bead, never a real failure), how (per-stack marker + registry row), how it runs (excluded from gating CI, runs nightly + local with the env var), and how to un-quarantine (fix → remove marker + row → `task quarantine:audit` green). Follow the site voice (see `site/CLAUDE.md`).

- [ ] **Step 8: Verify docs lint + symmetry**

Run: `task lint:markdown` and `task lint:docs-symmetry`
Expected: clean (CLAUDE.md/AGENTS.md symlink symmetry intact).

- [ ] **Step 9: Commit**

```bash
jj describe -m "docs(gates): lane split + quarantine guide + INV-5 guard (holomush-b4myw)"
```

---

## Phase 4: Promotion (gated on Phases 1-3 merged to main)

### Task 11: flip the `protect-main` ruleset

**Files:** none in-repo — GitHub ruleset change via `gh api`. **Do this only after Phases 1-3 are merged to `main`.**

- [ ] **Step 1: Confirm the gating jobs are green on `main`**

```bash
gh run list --workflow=ci.yaml --branch main --limit 3 --json conclusion,displayTitle
```

Expected: recent `main` runs `success`, with `Integration Test`/`E2E Test` green (quarantine-excluded).

- [ ] **Step 2: Read current required contexts**

```bash
gh api repos/holomush/holomush/rulesets/11923801 \
  --jq '.rules[] | select(.type=="required_status_checks") | .parameters.required_status_checks[].context'
```

Expected: `Build`, `Lint`, `Test`, `CodeRabbit`.

- [ ] **Step 3: Add `Integration Test` + `E2E Test`**

Fetch the ruleset JSON, add the two contexts to the `required_status_checks` array (each `{"context":"Integration Test"}` / `{"context":"E2E Test"}`), and PUT it back:

```bash
gh api repos/holomush/holomush/rulesets/11923801 > /tmp/ruleset.json
# Edit /tmp/ruleset.json: append the two contexts to the required_status_checks
# rule's parameters.required_status_checks array (keep strict_required_status_checks_policy=false).
gh api -X PUT repos/holomush/holomush/rulesets/11923801 --input /tmp/ruleset.json
```

- [ ] **Step 4: Verify**

```bash
gh api repos/holomush/holomush/rulesets/11923801 \
  --jq '.rules[] | select(.type=="required_status_checks") | .parameters.required_status_checks[].context'
```

Expected: now includes `Integration Test` and `E2E Test`.

- [ ] **Step 5: End-to-end gate check**

Open a trivial **docs-only** PR → confirm it becomes mergeable (the `ci-docs-skip` no-op `Integration Test`/`E2E Test` satisfy the required checks). Open a trivial **code** PR → confirm `Integration Test`/`E2E Test` run the quarantine-excluded suite and gate merge. Close both.

- [ ] **Step 6: Close the epic**

```bash
bd close holomush-b4myw --reason "Tier-split gates shipped: int/e2e CI-required (quarantine-excluded), local pr-prep shrunk to fast lane, quarantine mechanism + governance live."
```

---

## Post-implementation checklist

- [ ] All meta-tests pass: `task test -- ./test/meta/`
- [ ] Fast `task pr-prep` runs green with no Docker/flock; `task pr-prep:full` still runs the full gate.
- [ ] `task quarantine:audit` green (all quarantined beads open).
- [ ] `rg "to full completion before push" .claude/` → zero hits (INV-6).
- [ ] Ruleset shows `Integration Test` + `E2E Test` required (Phase 4).
- [ ] Quarantine burn-down tracked: each of the 7 seed beads, when fixed, removes its marker + registry row (bijection test enforces).
<!-- adr-capture: sha256=57ee3a8e11afda1e; session=a5d87bb9; ts=2026-05-26T00:36:25Z; adrs=holomush-5k6au,holomush-5eqiv -->
