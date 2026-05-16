# Testify + Ginkgo Migration Completion Implementation Plan

> **STATUS: SUPERSEDED — 2026-05-16.** Work complete; epic
> [`holomush-rccc`](https://github.com/holomush/holomush/issues/3884)
> closed (8 PRs merged: #3950, #3951, #4011, #4012, #4013, #4014, #4015,
> #4016). Retained for historical record.
>
> **CORRECTION (binding for any future migration work):**
>
> Canonical Pattern A's prescription to pass `GinkgoT()` to helpers
> taking `testing.TB` is **WRONG** and the errutil widening (Task A1
> "Prerequisite") was unnecessary. Go 1.26's `testing.TB` has an
> unexported `private()` method preventing third-party types (including
> `ginkgo.FullGinkgoTInterface`) from satisfying it.
>
> The **correct pattern** (already used by ~30 pre-existing Ginkgo
> specs in this codebase) is `suiteT` capture: the suite bootstrap
> declares `var suiteT *testing.T`, the `TestX` entry captures
> `suiteT = t`, and spec bodies pass `suiteT` to helpers. See PR
> [#3951](https://github.com/holomush/holomush/pull/3951)
> (`test/integration/plugin/plugin_role_permissions_test.go`) as the
> canonical worked example.
>
> Additional lessons learned (captured during execution):
>
> - `suiteT.Cleanup` / `suiteT.Setenv` are SUITE-scoped (`*testing.T`
>   from the entry func), not spec-scoped. Use `DeferCleanup` and
>   per-spec `os.Setenv` + restore for true per-spec isolation. See
>   PR [#4015](https://github.com/holomush/holomush/pull/4015) — the
>   sub-agent's `specTB` adapter and `freshBus()` / `freshPool()`
>   no-arg helpers prevent resource accumulation across specs.
> - INV-tagged test funcs that
>   `internal/eventbus/history/phase7_boundary_meta_test.go` pins are
>   invisible to its AST walk once converted to `Describe`/`It`.
>   Remap each INV to the Ginkgo suite entry (e.g., `TestBinaryPlugin`,
>   `TestEventbusE2E`); keep the INV reference greppable in the spec
>   `Describe` name.
> - Cluster-level state (Postgres roles especially) leaks across specs
>   sharing a Ginkgo `Describe`. Use unique-per-spec role names or
>   `DROP ROLE IF EXISTS` guards.
> - Plan's "40 files" inventory swept in 2 helper-only files (zero test
>   funcs): `test/integration/auth/core_client_shim_test.go` and
>   `test/integration/plugin/counting_proxy_test.go`. True conversion
>   scope was 38. Future inventory queries should also filter by
>   `rg -c 'func Test|t.Run'`.
> - **Sub-agent fan-out caveats:** parallel `task pr-prep` invocations
>   are serialized by a machine-wide flock, so parallel sub-agents
>   stall waiting for the lock. Parallel `task test:int` invocations
>   from different worktrees hit the SAME shared testcontainer and
>   produce cross-contamination failures that masquerade as real test
>   failures. For future fan-outs, sequence the verification steps
>   (`task test:int` and `task pr-prep`) explicitly rather than letting
>   each sub-agent run them in parallel.
> - **Verify sub-agent semantic changes:** one sub-agent silently
>   stripped `dek_ref` / `dek_version` handling from a test helper as
>   part of a refactor and the orchestrator-dispatched code-reviewer
>   missed it; CI surfaced the regression. Future sub-agent dispatch
>   prompts should explicitly require diff-vs-main verification, not
>   just `task pr-prep`.

---

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete the testify + ginkgo migration tracked under `holomush-1hq.26` by converting the remaining 40 plain-`testing.T` integration test files to Ginkgo / Gomega in two parallel-safe phases across 8 PRs.

**Architecture:** Stage 0 is a bead-DB sync (4 closes + 1 re-purpose of `1hq.60`'s title/description; the 6 new conversion beads were filed at plan-to-beads time, so Stage 0 does NOT re-file them). Stage A converts the 22 files under `test/integration/` (4 PRs, smallest-first by file count: A1 auth(1) → A2 plugin(4) → A3 crypto(3) → A4 eventbus_e2e(14)). Stage B converts the 18 package-level `*_integration_test.go` files outside `test/integration/` (4 PRs: B1 misc(4) → B2 admin(2) → B3 store(4) → B4 eventbus-crypto(8)). Stage C closes `holomush-1hq.26`. Stages A and B run in parallel because they touch disjoint trees and use disjoint test binaries.

**Tech Stack:** github.com/onsi/ginkgo/v2 v2.28.1, github.com/onsi/gomega v1.39.1, github.com/stretchr/testify v1.11.1 (already in `go.mod`); existing project conventions for `*_suite_test.go` bootstrap; `task test:int` discovers the `//go:build integration` files.

**Spec:** `docs/superpowers/specs/2026-05-15-testify-ginkgo-migration-completion-design.md` (r5, READY)

**Tracking:**

- Epic: `holomush-rccc` (promoted from design task at plan-to-beads time, 2026-05-15)
- 6 conversion child beads (`rccc.1`-`rccc.6` for A1/A2/A3/B1/B2/B4) and 2 re-parented existing beads (`cz4s` for A4, `1hq.60` for B3 — re-purposed for `internal/store/`) ALREADY filed at plan-to-beads time. Stage 0 does not create them.
- 4 sibling beads to close in Stage 0: `1hq.49`, `1hq.59`, `1hq.61`, `1hq.62` (stale; target files already migrated by unrelated work or covered by `.claude/rules/testing.md`).

## Bead chain structure

Each implementation task corresponds to one PR-bead. The design bead `holomush-rccc` is promoted to an epic at `plan-to-beads` time and becomes the parent of every conversion bead.

| Plan Task | Bead                | PR shape                                 |
|-----------|---------------------|------------------------------------------|
| 0         | n/a (bead-DB op)    | Bead-DB sync only; no code               |
| A1        | `holomush-rccc.1`   | `auth-conversion`                        |
| A2        | `holomush-rccc.2`   | `plugin-conversion`                      |
| A3        | `holomush-rccc.3`   | `crypto-conversion`                      |
| A4        | `holomush-cz4s`     | `eventbus-e2e-conversion`                |
| B1        | `holomush-rccc.4`   | `misc-package-integration-conversion`    |
| B2        | `holomush-rccc.5`   | `internal-admin-conversion`              |
| B3        | `holomush-1hq.60`   | `internal-store-conversion`              |
| B4        | `holomush-rccc.6`   | `internal-eventbus-crypto-conversion`    |
| C         | n/a                 | Final epic-close attestation             |

## Reviewer non-blocker decisions (pinned)

Spec deferred a few decisions to plan time. Pinned here:

1. **Workspace strategy.** Each Stage A/B PR runs in its own isolated jj workspace (`task workspace:new -- <stage-pr-name>`). Stage A and Stage B PRs may execute concurrently in distinct workspaces; Stage C runs in the default workspace as a single `bd close` operation.
2. **Single suite-rename pattern.** When renaming a narrow Ginkgo suite (A4 eventbus_e2e, B1 cmd/holomush), edit the `RunSpecs(t, "...")` string literal in-place; do NOT delete-and-recreate the bootstrap file.
3. **Skeleton conversion form.** Use `It` with a single `Skip(...)` call as the body; do NOT wrap in `Context` or use `Pending`. Preserves the existing one-test-per-skeleton-file convention for minimal noise on the test report.
4. **`BeforeEach` vs `BeforeAll`.** Use `BeforeEach` unless the existing plain-`testing.T` code uses package-level `init()` / `TestMain` / sync.Once for setup (in which case `BeforeAll` with an `Ordered` decorator on the container preserves semantics). Most files use per-test setup and translate cleanly to `BeforeEach`.
5. **Commit granularity inside a PR.** One commit per converted file. The PR squash-merge collapses them; per-file commits make code review tractable.

## File Structure

This plan modifies 40 files and creates 6 new `*_suite_test.go` bootstrap files. The full per-file inventory is in the spec §"Scope inventory". Summary table:

**Create (6 new bootstraps):**

| Path                                                          | Responsibility                                                                     |
|---------------------------------------------------------------|------------------------------------------------------------------------------------|
| `internal/eventbus/crypto/kek/kek_suite_test.go`              | Ginkgo bootstrap for the kek sub-package's 2 integration tests                     |
| `internal/admin/approval/approval_suite_test.go`              | Ginkgo bootstrap for `approval/repo_integration_test.go`                           |
| `internal/admin/readstream/readstream_suite_test.go`          | Ginkgo bootstrap for `readstream/cold_reader_integration_test.go`                  |
| `internal/eventbus/history/history_suite_test.go`             | Ginkgo bootstrap for `cold_postgres_integration_test.go`                           |
| `internal/totp/totp_suite_test.go`                            | Ginkgo bootstrap for `repo_integration_test.go`                                    |
| `plugins/core-scenes/core_scenes_suite_test.go`               | Ginkgo bootstrap for `store_integration_test.go`                                   |

**Modify (40 conversions + 2 in-place renames):**

| Category                                       | Paths                                                                       |
|------------------------------------------------|-----------------------------------------------------------------------------|
| `test/integration/auth/` (1 file)              | `core_client_shim_test.go`                                                  |
| `test/integration/plugin/` (4 files)           | `counting_proxy_test.go`, `plugin_migration_test.go`, `plugin_role_permissions_test.go`, `schema_isolation_test.go` |
| `test/integration/crypto/` (3 files)           | `emit_test.go`, `metadata_only_test.go`, `plugin_decrypt_test.go`           |
| `test/integration/eventbus_e2e/` (14 files + suite rename + delete TestSuiteCompiles) | All 14 files in spec §"`test/integration/eventbus_e2e/`" + rename at `cursor_concurrent_suite_test.go:30` + remove vacuous `TestSuiteCompiles` in `suite_test.go` |
| `internal/eventbus/crypto/dek/` (6 files)      | `checkpoint`, `manager`, `rekey_phase5`, `rekey_phase6`, `rekey_phase7`, `store` (all `_integration_test.go`) |
| `internal/eventbus/crypto/kek/` (2 files)      | `local_aead_integration_test.go`, `none_integration_test.go`                |
| `internal/store/` (4 files)                    | `migrate_integration_test.go`, `migrate_plugins_integration_test.go`, `migrations_audit_shape_integration_test.go`, `role_store_integration_test.go` |
| `internal/admin/approval/` (1 file)            | `repo_integration_test.go`                                                  |
| `internal/admin/readstream/` (1 file)          | `cold_reader_integration_test.go`                                           |
| `cmd/holomush/` (1 file + suite rename)        | `automigrate_integration_test.go` + rename at `admin_authenticate_e2e_suite_test.go:35` |
| `internal/eventbus/history/` (1 file)          | `cold_postgres_integration_test.go`                                         |
| `internal/totp/` (1 file)                      | `repo_integration_test.go`                                                  |
| `plugins/core-scenes/` (1 file)                | `store_integration_test.go`                                                 |

---

## Canonical conversion pattern (reference for every conversion task below)

This is the BEFORE → AFTER pattern every conversion task applies. Do not re-show the full pattern in each task — it's referenced by task title. Per-task notes call out variations.

### Prerequisite: widen `errutil.AssertErrorCode` to `testing.TB`

`pkg/errutil/testing.go:15` currently signs `AssertErrorCode(t *testing.T, ...)`. `GinkgoT()` returns `ginkgo.FullGinkgoTInterface`, which is NOT assignable to the concrete `*testing.T` struct. Any Ginkgo conversion that translates `errutil.AssertErrorCode(t, err, "X")` to `errutil.AssertErrorCode(GinkgoT(), err, "X")` will fail to compile.

Widen both helpers in `pkg/errutil/testing.go` from `*testing.T` to `testing.TB`:

```go
// BEFORE
func AssertErrorCode(t *testing.T, err error, code string) {
    t.Helper()
    oopsErr, ok := oops.AsOops(err)
    require.True(t, ok, "expected oops error, got %T", err)
    assert.Equal(t, code, oopsErr.Code())
}

func AssertErrorContext(t *testing.T, err error, key string, value any) {
    t.Helper()
    oopsErr, ok := oops.AsOops(err)
    require.True(t, ok, "expected oops error, got %T", err)
    ctx := oopsErr.Context()
    assert.Contains(t, ctx, key)
    assert.Equal(t, value, ctx[key])
}
```

```go
// AFTER
func AssertErrorCode(t testing.TB, err error, code string) {
    t.Helper()
    oopsErr, ok := oops.AsOops(err)
    require.True(t, ok, "expected oops error, got %T", err)
    assert.Equal(t, code, oopsErr.Code())
}

func AssertErrorContext(t testing.TB, err error, key string, value any) {
    t.Helper()
    oopsErr, ok := oops.AsOops(err)
    require.True(t, ok, "expected oops error, got %T", err)
    ctx := oopsErr.Context()
    assert.Contains(t, ctx, key)
    assert.Equal(t, value, ctx[key])
}
```

`*testing.T` and `*testing.B` already satisfy `testing.TB`, so all 155 existing callers continue to compile without source changes. `GinkgoT()`'s return type (which implements every `testing.TB` method) becomes a valid argument. This widening is paired with Task A1 (the smallest PR) — see Task A1 Step 4 below.

### A. Standard live test

A plain-`testing.T` table-driven or single test:

```go
// BEFORE
//go:build integration

package foo_test

import (
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestFooDoesX(t *testing.T) {
    env := setupTestEnv(t)
    defer env.teardown()

    got, err := env.foo.Do()
    require.NoError(t, err)
    assert.Equal(t, expectedValue, got)
}
```

Becomes a `var _ = Describe` registered into the package's existing Ginkgo suite (the suite bootstrap is either already there or created by this PR's first file — see per-task notes):

```go
// AFTER
//go:build integration

package foo_test

import (
    . "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
    . "github.com/onsi/gomega" //nolint:revive // ginkgo convention
)

var _ = Describe("Foo does X", func() {
    var env *testEnv

    BeforeEach(func() {
        env = setupTestEnv(GinkgoT())
    })

    AfterEach(func() {
        env.teardown()
    })

    It("returns expected value", func() {
        got, err := env.foo.Do()
        Expect(err).NotTo(HaveOccurred())
        Expect(got).To(Equal(expectedValue))
    })
})
```

Key translation rules:

| testing.T pattern                      | Ginkgo / Gomega pattern                                                                       |
|----------------------------------------|-----------------------------------------------------------------------------------------------|
| `func TestX(t *testing.T)`             | `var _ = Describe("X", func() { ... })`                                                       |
| `t.Run("subtest", func(t *testing.T))` | `Context("subtest", func() { ... })` OR nested `Describe`                                     |
| `setupTestEnv(t)`                      | `BeforeEach(func() { env = setupTestEnv(GinkgoT()) })`                                         |
| `defer env.teardown()`                 | `AfterEach(func() { env.teardown() })`                                                         |
| `require.NoError(t, err)`              | `Expect(err).NotTo(HaveOccurred())`                                                            |
| `require.Error(t, err)`                | `Expect(err).To(HaveOccurred())`                                                               |
| `require.ErrorIs(t, err, sentinel)`    | `Expect(err).To(MatchError(sentinel))` OR `Expect(errors.Is(err, sentinel)).To(BeTrue())`     |
| `errutil.AssertErrorCode(t, err, "X")` | `errutil.AssertErrorCode(GinkgoT(), err, "X")` — keep the project's existing helper           |
| `assert.Equal(t, want, got)`           | `Expect(got).To(Equal(want))` (NB: `Expect` puts the actual value FIRST, opposite of testify) |
| `assert.NotNil(t, x)`                  | `Expect(x).NotTo(BeNil())`                                                                    |
| `assert.True(t, b)`                    | `Expect(b).To(BeTrue())`                                                                      |
| Eventually-style polling               | `Eventually(func() T { ... }).Should(Equal(expected))`                                         |

### B. Skeleton (skip-only) test

The 4 skeletons in `test/integration/eventbus_e2e/` currently contain TWO `TODO(holomush-X)` references each: (1) the `t.Skip(...)` reason string, and (2) a deeper implementation-hint comment block (e.g. `// TODO(holomush-l4kx): Exec the subcommand:` followed by 5-15 lines of skeletal code showing what the test would look like once the backing feature ships). Both forms MUST be preserved through the Ginkgo conversion — the implementation hint is load-bearing context filed against the corresponding feature bead.

```go
// BEFORE — audit_drift_detector_test.go (illustrative; actual structure varies per file)
func TestAuditDriftDetectorReportsTamperedRow(t *testing.T) {
    t.Skip("TODO(holomush-ecbg): drift detector not yet implemented — skeleton retained for the follow-up bead")

    // TODO(holomush-ecbg): invoke the detector and assert:
    //   - drifted row id surfaces in the report
    //   - non-drifted rows do not appear
    //   - report includes codec name + reason code
    // <... possibly more skeletal code ...>
}
```

```go
// AFTER
var _ = Describe("Audit drift detector", func() {
    It("reports tampered rows (INV-…)", func() {
        Skip("TODO(holomush-ecbg): drift detector not yet implemented — skeleton retained for the follow-up bead")

        // TODO(holomush-ecbg): invoke the detector and assert:
        //   - drifted row id surfaces in the report
        //   - non-drifted rows do not appear
        //   - report includes codec name + reason code
        // <... possibly more skeletal code, copied verbatim from BEFORE ...>
    })
})
```

**Critical:** `Skip(...)` MUST be inside the `It` body (Run Phase). `Skip(...)` at the top level of a `Describe` body runs during Ginkgo's Tree Construction Phase and panics. The deeper implementation-hint comment lives INSIDE the `It` after `Skip` — the `Skip` returns immediately so the hint code never executes, but the comment + skeletal code stay grep-able and visible to the future implementer.

The verification grep gate at Stage-A-final checks that `rg 'TODO\(holomush-(ecbg|l4kx|6nds|nko7)\)' test/integration/eventbus_e2e/ | wc -l` returns exactly **8** — two hits per skeleton (Skip-reason line + deeper-hint line, both preserving the same `holomush-X` ID).

### C. New `*_suite_test.go` bootstrap (Stage B, 6 sub-packages)

For packages that do not already have a `RunSpecs` entry, create one new file:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package <pkg>_test  // or `package <pkg>` if the package's *_integration_test.go files declare it that way

import (
    "testing"

    . "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
    . "github.com/onsi/gomega" //nolint:revive // ginkgo convention
)

func Test<Pkg>(t *testing.T) {
    RegisterFailHandler(Fail)
    RunSpecs(t, "<Pkg> Suite")
}
```

Replace `<pkg>` and `<Pkg>` per package; suite name MUST be unique across the repo (mirror the existing patterns in `auth_suite_test.go`, `store_suite_test.go`, etc.).

### D. Suite rename (in-place, A4 + B1 only)

For packages with a narrowly-named existing `RunSpecs` (eventbus_e2e and cmd/holomush), the conversion PR renames the literal string in-place. Example for eventbus_e2e:

```go
// BEFORE — test/integration/eventbus_e2e/cursor_concurrent_suite_test.go:30
RunSpecs(t, "Cursor Concurrent Pagination Specs")
```

```go
// AFTER
RunSpecs(t, "EventbusE2E Suite")
```

Same shape for `cmd/holomush/admin_authenticate_e2e_suite_test.go:35`:

```go
// BEFORE
RunSpecs(t, "Admin Authenticate E2E Lifecycle Suite")
```

```go
// AFTER
RunSpecs(t, "Holomush Integration Suite")
```

In neither case do we add a second `RunSpecs`. The verification gate `rg -c 'RunSpecs' <package>/` MUST return exactly `1` for each package after the PR.

---

## Task 0: Stage 0 — Bead audit & cleanup

**Goal:** Close 4 stale `1hq.*` children with grounded evidence, re-purpose `1hq.60` for the `internal/store/` cluster, file 6 new conversion beads. No code changes.

**Files:** none (bead-DB only).

- [ ] **Step 1: Verify `holomush-rccc` is the current design bead and you have its ID**

Run: `bd show holomush-rccc 2>&1 | head -3`

Expected: title `Design: complete testify+ginkgo migration (1hq.26 execution rev)`, status `OPEN`.

- [ ] **Step 2: Close `holomush-1hq.49` with mockery+ginkgo evidence**

Run:

```bash
bd close holomush-1hq.49 --reason "go.mod requires testify v1.11.1, ginkgo/v2 v2.28.1, gomega v1.39.1; .mockery.yaml exists with 20+ generated */mocks/mock_*.go outputs. Mockery is a CLI tool (not a go.mod dep). All Task 1 acceptance criteria from docs/plans/2026-01-20-testing-libraries-implementation.md are met."
```

Expected: `✓ Closed holomush-1hq.49 — Add testify, ginkgo, gomega, and mockery dependencies: <reason>`.

- [ ] **Step 3: Close `holomush-1hq.59`**

Run:

```bash
bd close holomush-1hq.59 --reason "test/integration/phase1_5_test.go imports ginkgo/v2 + gomega (verified via rg). Migration target met."
```

- [ ] **Step 4: Close `holomush-1hq.61`**

Run:

```bash
bd close holomush-1hq.61 --reason "internal/plugin/integration_test.go imports ginkgo/v2 + gomega (verified via rg). Migration target met."
```

- [ ] **Step 5: Close `holomush-1hq.62`**

Run:

```bash
bd close holomush-1hq.62 --reason ".claude/rules/testing.md documents all 4 patterns enumerated in the bead's acceptance criteria: testify assert/require, mockery-generated mock usage, ginkgo Describe/Context/It, Eventually/Consistently. Verified via rg -c against the rules file."
```

- [ ] **Step 6: Re-purpose `holomush-1hq.60` (update title + description; do NOT close)**

Run:

```bash
bd update holomush-1hq.60 \
  --title "Migrate internal/store/*_integration_test.go (4 files) to Ginkgo" \
  --description "Migrate internal/store/*_integration_test.go to ginkgo per docs/superpowers/specs/2026-05-15-testify-ginkgo-migration-completion-design.md §'internal/store/' (Phase B). Re-scoped 2026-05-15: the original target file internal/store/postgres_integration_test.go was removed in unrelated work; the current 4 files (migrate_integration_test.go, migrate_plugins_integration_test.go, migrations_audit_shape_integration_test.go, role_store_integration_test.go) inherit the bead's intent. internal/store/store_suite_test.go already provides the RunSpecs entry (TestStore). Acceptance: the 5 per-PR §Verification gates pass for all 4 files; PR title 'internal-store-conversion'."
```

- [ ] **Step 7: Add a `bd note` documenting the re-purpose**

Run:

```bash
bd note holomush-1hq.60 "Re-scoped 2026-05-15 (design bead holomush-rccc, plan docs/superpowers/plans/2026-05-15-testify-ginkgo-migration-completion-plan.md). Original target internal/store/postgres_integration_test.go removed in unrelated work; current scope is the 4 *_integration_test.go files in internal/store/."
```

- [ ] **Step 8: Sync the beads DB**

Run: `bd dolt push 2>&1 | tail -3`

Expected: `Push complete.`

NOTE: At plan-write time (2026-05-15), `plan-to-beads` already promoted `holomush-rccc` to an epic, filed the 6 new conversion beads (A1, A2, A3, B1, B2, B4) as children of `holomush-rccc`, and re-parented the two existing conversion beads (`cz4s` for A4, `holomush-1hq.60` for B3) under `holomush-rccc`. Stage 0 does NOT need to create those beads; they already exist. Capture their actual IDs from `bd show holomush-rccc` before claiming them in Tasks A1-A4 and B1-B4.

- [ ] **Step 9: Verify Stage 0 completion**

Run: `bd show holomush-rccc 2>&1 | rg 'CHILDREN' -A 20 | head -25`

Expected: `holomush-rccc` (epic) lists 6 new conversion task beads + `cz4s` + `1hq.60` as children. The 4 closed-with-reason `1hq.*` siblings are visible via `bd show holomush-1hq.49` etc. No new commit.

---

## Task A1: `auth-conversion` PR (1 file + errutil prep)

**Goal:** Convert `test/integration/auth/core_client_shim_test.go` to a Ginkgo spec registered into the existing `auth_suite_test.go:39` `Player Session Lifecycle Integration Suite`. Bundled with the one-line `errutil.AssertErrorCode` / `AssertErrorContext` signature widening prerequisite — pairing it with the smallest PR minimizes the diff surface for that helper change.

**Files:**

- Modify: `pkg/errutil/testing.go` (widen both helpers to `testing.TB`)
- Modify: `test/integration/auth/core_client_shim_test.go`

**Conversion pattern:** Canonical pattern A (standard live test). NO new bootstrap (existing entry at `auth_suite_test.go:39`). MUST NOT add a second `RunSpecs`.

- [ ] **Step 1: Create the isolated workspace**

Run: `task workspace:new -- 1hq-A1-auth-conversion 2>&1 | tail -3`

Expected: `Created workspace in "../.worktrees/1hq-A1-auth-conversion"` and the printed absolute path.

`cd` to the printed workspace path before any further work in this task.

- [ ] **Step 2: Claim the bead**

Run: `bd update holomush-rccc.1 --claim 2>&1 | tail -1`

(Substitute the bead ID from Task 0 Step 8.)

Expected: `✓ Updated issue: holomush-<id> — auth-conversion: …`.

- [ ] **Step 3: Widen `errutil.AssertErrorCode` + `AssertErrorContext` to `testing.TB`**

Edit `pkg/errutil/testing.go`:

`old_string`:

```go
func AssertErrorCode(t *testing.T, err error, code string) {
```

`new_string`:

```go
func AssertErrorCode(t testing.TB, err error, code string) {
```

Then a second edit on the same file:

`old_string`:

```go
func AssertErrorContext(t *testing.T, err error, key string, value any) {
```

`new_string`:

```go
func AssertErrorContext(t testing.TB, err error, key string, value any) {
```

- [ ] **Step 4: Verify the widening compiles and all existing callers still pass**

Run: `task test -- ./pkg/errutil/... 2>&1 | tail -5`

Expected: pass (errutil's own tests use `*testing.T` which satisfies `testing.TB`).

Run: `task test -- ./internal/... ./plugins/... 2>&1 | tail -10` — broad check that all 155 existing callers still compile.

Expected: pass.

Commit the widening separately: `jj describe -m "errutil(testing): widen Assert* helpers to testing.TB

Both AssertErrorCode and AssertErrorContext take *testing.T concretely
today. Widen to testing.TB so the same helpers work in Ginkgo specs
(GinkgoT() returns ginkgo.FullGinkgoTInterface, not *testing.T).
*testing.T already satisfies testing.TB; all 155 existing callers
continue to compile without source changes.

Prerequisite for the 1hq.26 testify+ginkgo migration completion."`

Then `jj new` to start a fresh change on top for the test conversion.

- [ ] **Step 5: Read the existing test file**

Run: `wc -l test/integration/auth/core_client_shim_test.go` and `rg -n 'func Test|t\.Run|require\.|assert\.' test/integration/auth/core_client_shim_test.go | head -30`.

Use this output to enumerate the test funcs and assertion sites. Plan the `Describe`/`Context`/`It` shape: each top-level `TestX` becomes a `Describe("X", func() { ... })`; each `t.Run("y", ...)` inside becomes a `Context("y", func() { ... })`; each assertion translates per the canonical pattern A table.

- [ ] **Step 6: Convert the file**

Apply Canonical Pattern A. The file's `//go:build integration` line and SPDX header stay. Replace the `testing` + `testify/{assert,require}` imports with the `. "github.com/onsi/ginkgo/v2"` and `. "github.com/onsi/gomega"` dot imports (use the `//nolint:revive // ginkgo convention` suffix to match the project's existing Ginkgo files).

The (now-widened, post-Step 3) `errutil.AssertErrorCode` accepts `testing.TB`, so pass `GinkgoT()` directly where you'd pass `t`.

- [ ] **Step 7: Run the converted test**

Run: `task test:int -- -ginkgo.focus="<top-level Describe name>" 2>&1 | tail -10`

Expected: the focused spec runs and passes.

- [ ] **Step 8: Verify the per-PR gates**

Run each:

```bash
rg -c 'RunSpecs' test/integration/auth/    # expected: exactly 1
rg 't\.Skip\(' test/integration/auth/      # expected: zero hits
rg 'func Test|t\.Run' test/integration/auth/core_client_shim_test.go  # expected: zero hits (only the helper TestPlayerSessionLifecycle in auth_suite_test.go remains)
```

- [ ] **Step 9: Run the full `auth` integration suite**

Run: `task test:int -- ./test/integration/auth/... 2>&1 | tail -10`

Expected: all specs in the auth suite pass.

- [ ] **Step 10: Run `task pr-prep` to completion**

Run: `task pr-prep 2>&1 | tail -10`

Expected: `✓ All PR checks passed.`

- [ ] **Step 11: Describe the commit**

Run via jj (the workspace is jj-colocated):

```bash
jj describe -m "test(integration/auth): migrate core_client_shim to Ginkgo (1hq.26 A1)

Convert test/integration/auth/core_client_shim_test.go from plain
testing.T + testify to Ginkgo Describe/Context/It registered into the
existing auth_suite_test.go:39 'Player Session Lifecycle Integration
Suite'. No second RunSpecs; existing testify helpers preserved.

Part of holomush-1hq.26 testify+ginkgo migration completion (Stage A
PR 1 of 4). See spec docs/superpowers/specs/2026-05-15-testify-ginkgo-migration-completion-design.md
and plan docs/superpowers/plans/2026-05-15-testify-ginkgo-migration-completion-plan.md."
```

- [ ] **Step 12: Set bookmark + push**

Run:

```bash
jj bookmark set 1hq-A1-auth-conversion -r @
jj git push --branch 1hq-A1-auth-conversion 2>&1 | tail -5
```

- [ ] **Step 13: Open the PR**

Run:

```bash
GH_REPO='holomush/holomush' GIT_SSL_NO_VERIFY=1 gh pr create \
  --head 1hq-A1-auth-conversion \
  --title "test(integration/auth): migrate to Ginkgo + widen errutil helpers to testing.TB (1hq.26 A1)" \
  --body "Closes holomush-holomush-rccc.1. Part of holomush-1hq.26 / holomush-rccc Stage A. Two commits: (1) widen errutil.AssertErrorCode + AssertErrorContext from *testing.T to testing.TB so Ginkgo specs can use them via GinkgoT(); (2) convert test/integration/auth/core_client_shim_test.go to Ginkgo. See spec + plan for the canonical conversion pattern. Per-PR gates green: \`task pr-prep\` ✓, \`rg -c RunSpecs test/integration/auth/\` = 1, \`rg 't.Skip(' test/integration/auth/\` = 0."
```

Expected: PR URL printed.

- [ ] **Step 14: Close the bead on merge**

After the PR merges:

```bash
bd close holomush-rccc.1 --reason "PR #<n> merged"
bd dolt push
```

Clean up the workspace per Landing the Plane.

---

## Task A2: `plugin-conversion` PR (4 files)

**Goal:** Convert 4 files in `test/integration/plugin/` into the existing `plugin_suite_test.go:25` `Binary Plugin Integration Suite`.

**Files:**

- Modify: `test/integration/plugin/counting_proxy_test.go`
- Modify: `test/integration/plugin/plugin_migration_test.go`
- Modify: `test/integration/plugin/plugin_role_permissions_test.go`
- Modify: `test/integration/plugin/schema_isolation_test.go`

**Conversion pattern:** Canonical Pattern A applied per file. NO new bootstrap. MUST NOT add a second `RunSpecs`.

- [ ] **Step 1: Workspace + claim**

```bash
task workspace:new -- 1hq-A2-plugin-conversion
cd <printed path>
bd update holomush-rccc.2 --claim
```

- [ ] **Step 2: Inspect each file's shape**

For each of the 4 files run:

```bash
rg -n 'func Test|t\.Run|require\.|assert\.|setupTestEnv|TestMain' test/integration/plugin/<file>.go | head -30
```

Note any TestMain / package-level setup that would need `BeforeAll` instead of `BeforeEach` (see pinned decision #4).

- [ ] **Step 3: Convert `counting_proxy_test.go`**

Apply Canonical Pattern A. Commit before moving on (`jj describe -m "test(integration/plugin): convert counting_proxy to Ginkgo (1hq.26 A2)"`).

- [ ] **Step 4: Run focused test for `counting_proxy_test.go`**

Run: `task test:int -- -ginkgo.focus="<Describe name from converted file>" ./test/integration/plugin/... 2>&1 | tail -5`

Expected: focused spec passes.

- [ ] **Step 5: Convert `plugin_migration_test.go`, run focused, commit**

Same shape as Steps 3-4.

- [ ] **Step 6: Convert `plugin_role_permissions_test.go`, run focused, commit**

Same shape. This file references `INV-P7-13` per its existing comment; preserve that comment verbatim above the corresponding `Describe`.

- [ ] **Step 7: Convert `schema_isolation_test.go`, run focused, commit**

Same shape.

- [ ] **Step 8: Run the full plugin suite + gates**

```bash
task test:int -- ./test/integration/plugin/... 2>&1 | tail -10
rg -c 'RunSpecs' test/integration/plugin/    # expected: 1
rg 't\.Skip\(' test/integration/plugin/      # expected: 0
rg 'func Test|t\.Run' test/integration/plugin/{counting_proxy,plugin_migration,plugin_role_permissions,schema_isolation}_test.go  # expected: 0
```

- [ ] **Step 9: pr-prep + push + PR + close**

Same shape as Task A1 Steps 8-12. PR title: `test(integration/plugin): migrate to Ginkgo (1hq.26 A2)`.

---

## Task A3: `crypto-conversion` PR (3 files)

**Goal:** Convert 3 files in `test/integration/crypto/` into the existing `crypto_suite_test.go:26` `Crypto Integration Suite`.

**Files:**

- Modify: `test/integration/crypto/emit_test.go`
- Modify: `test/integration/crypto/metadata_only_test.go`
- Modify: `test/integration/crypto/plugin_decrypt_test.go`

**Conversion pattern:** Canonical Pattern A applied per file. NO new bootstrap. MUST NOT add a second `RunSpecs`.

- [ ] **Step 1: Workspace + claim**

```bash
task workspace:new -- 1hq-A3-crypto-conversion
cd <printed path>
bd update holomush-rccc.3 --claim
```

- [ ] **Step 2-4: Convert each of the 3 files**

One commit per file. Apply Canonical Pattern A. After each file, run a focused `task test:int -- -ginkgo.focus="<Describe name>" ./test/integration/crypto/...` and verify pass.

Note for `emit_test.go`: this file exercises the crypto-domain emit path. Verify that any AAD-binding assertions translate to `Expect(aadBytes).To(Equal(expected))` rather than byte-by-byte loops — Gomega's `Equal` does a deep comparison.

Note for `metadata_only_test.go`: contains assertions on `Event.MetadataOnly` and `Event.NoPlaintextReason` shape (per the recent Phase 7 changes). Preserve every assertion; do not collapse multiple `Expect`s into one.

- [ ] **Step 5: Full crypto suite + gates**

```bash
task test:int -- ./test/integration/crypto/... 2>&1 | tail -10
rg -c 'RunSpecs' test/integration/crypto/    # 1
rg 't\.Skip\(' test/integration/crypto/      # 0
rg 'func Test|t\.Run' test/integration/crypto/{emit,metadata_only,plugin_decrypt}_test.go  # 0
```

- [ ] **Step 6: pr-prep + push + PR + close**

Same shape as Task A1. PR title: `test(integration/crypto): migrate to Ginkgo (1hq.26 A3)`.

---

## Task A4: `eventbus-e2e-conversion` PR (14 files + suite rename + delete TestSuiteCompiles)

**Goal:** Convert all 14 plain-`testing.T` files in `test/integration/eventbus_e2e/` to Ginkgo, including 4 skip-skeletons. Rename the existing narrow suite from "Cursor Concurrent Pagination Specs" to "EventbusE2E Suite". Delete the vacuous `TestSuiteCompiles` in `suite_test.go`.

**Files:** 14 conversions + 1 rename + 1 deletion. Full list in spec §"`test/integration/eventbus_e2e/`".

**Conversion patterns:**

- Live files: Canonical Pattern A
- 4 skeletons (`audit_drift_detector_test.go`, `backfill_rebuild_test.go`, `js_storage_corruption_test.go`, `multi_protocol_fanout_test.go`): Canonical Pattern B
- Suite rename at `cursor_concurrent_suite_test.go:30`: Canonical Pattern D
- Delete `TestSuiteCompiles` from `suite_test.go` + remove `holomush-suos.2` follow-up comment at `cursor_concurrent_suite_test.go:20-22`

MUST NOT add a second `RunSpecs` to `test/integration/eventbus_e2e/`.

- [ ] **Step 1: Workspace + claim**

```bash
task workspace:new -- 1hq-A4-eventbus-e2e-conversion
cd <printed path>
bd update holomush-cz4s --claim
```

- [ ] **Step 2: Rename the suite AND retire the stale `holomush-suos.2` reference (commit alone)**

The `cursor_concurrent_suite_test.go` file today contains:

```go
// suiteT captures the testing.T from the Ginkgo bootstrap so spec bodies can
// invoke local helpers (freshPool, drainStream, currentStreamLastSeq,
// buildReader) which take *testing.T. Mirrors the world_suite_test.go pattern.
//
// Other test files in this package use plain testing.T directly; they don't
// run through the Ginkgo entry point and ignore suiteT. Filed as
// holomush-suos.2 to convert the rest of the directory.
var suiteT *testing.T

// TestCursorConcurrentSpecs is the Ginkgo entry point for the
// cursor_concurrent_test.go specs. Other Test* functions in this package
// run independently of this entry point.
func TestCursorConcurrentSpecs(t *testing.T) {
	suiteT = t
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cursor Concurrent Pagination Specs")
}
```

After this PR, the `holomush-suos.2` reference is stale (it was closed earlier as a duplicate of `cz4s`) and the "other test files use plain testing.T" claim is no longer true. But the `var suiteT *testing.T` declaration MUST stay — `cursor_concurrent_test.go` references `suiteT` in 10 places (verify via `rg -n 'suiteT' test/integration/eventbus_e2e/cursor_concurrent_test.go | wc -l` — expected: 10).

Make TWO precise edits to `cursor_concurrent_suite_test.go`:

**Edit 1** — tighten the suiteT doc comment to drop the stale claim. `old_string`:

```go
// suiteT captures the testing.T from the Ginkgo bootstrap so spec bodies can
// invoke local helpers (freshPool, drainStream, currentStreamLastSeq,
// buildReader) which take *testing.T. Mirrors the world_suite_test.go pattern.
//
// Other test files in this package use plain testing.T directly; they don't
// run through the Ginkgo entry point and ignore suiteT. Filed as
// holomush-suos.2 to convert the rest of the directory.
var suiteT *testing.T
```

`new_string`:

```go
// suiteT captures the testing.T from the Ginkgo bootstrap so spec bodies can
// invoke local helpers (freshPool, drainStream, currentStreamLastSeq,
// buildReader) which take *testing.T. Mirrors the world_suite_test.go pattern.
var suiteT *testing.T
```

**Edit 2** — rename the suite literal. `old_string`:

```go
	RunSpecs(t, "Cursor Concurrent Pagination Specs")
```

`new_string`:

```go
	RunSpecs(t, "EventbusE2E Suite")
```

DO NOT delete `var suiteT *testing.T`. DO NOT delete `suiteT = t`. DO NOT use a line-range deletion — the Edit tool's old_string/new_string anchoring is the right mechanism for both edits above.

Also update the doc comment on `TestCursorConcurrentSpecs` itself (currently says "Ginkgo entry point for the cursor_concurrent_test.go specs"). After this PR it's the entry point for the whole package. Replace `old_string`:

```go
// TestCursorConcurrentSpecs is the Ginkgo entry point for the
// cursor_concurrent_test.go specs. Other Test* functions in this package
// run independently of this entry point.
func TestCursorConcurrentSpecs(t *testing.T) {
```

`new_string`:

```go
// TestEventbusE2E is the Ginkgo entry point for the entire eventbus_e2e
// package. Renamed from TestCursorConcurrentSpecs when holomush-cz4s
// landed the rest of the directory's conversions; the variable `suiteT`
// (set on line below) is still required by cursor_concurrent_test.go's
// helpers which take *testing.T.
func TestEventbusE2E(t *testing.T) {
```

Commit: `jj describe -m "test(eventbus_e2e): rename suite to EventbusE2E + drop stale suos.2 comment (cz4s prep)"`

Run: `task test:int -- ./test/integration/eventbus_e2e/... 2>&1 | tail -5` — expected: existing specs still pass under the new suite name. Then `rg -c 'RunSpecs' test/integration/eventbus_e2e/` returns `1`.

- [ ] **Step 3: Delete `TestSuiteCompiles`**

Edit `test/integration/eventbus_e2e/suite_test.go`. Use `rg -n 'TestSuiteCompiles' test/integration/eventbus_e2e/suite_test.go` to find the exact lines. Remove the function AND the preceding doc comment if any. The package doc and shared helpers stay.

Commit: `jj describe -m "test(eventbus_e2e): drop vacuous TestSuiteCompiles (cz4s prep)"`

Run: `task test:int -- ./test/integration/eventbus_e2e/... 2>&1 | tail -5` — expected: discovery still works because `cursor_concurrent_suite_test.go:30` provides the entry.

- [ ] **Step 4: Convert each live file (one commit per file)**

In any order convenient. For each of the 10 live files:

- `audit_only_channel_test.go`
- `cross_tier_query_test.go`
- `dispatcher_selector_identity_test.go`
- `plugin_audit_isolation_test.go`
- `plugin_audit_round_trip_test.go`
- `plugin_crash_resilience_test.go`
- `plugin_downgrade_attacker_test.go`
- `reconnect_resume_test.go`
- `rendering_completeness_test.go`
- `soak_test.go`

apply Canonical Pattern A, run a focused test, commit. Example commit message: `test(eventbus_e2e): convert plugin_downgrade_attacker to Ginkgo (cz4s)`.

Note: `dispatcher_selector_identity_test.go` and `plugin_downgrade_attacker_test.go` are Phase 7 / Phase 7-adjacent — preserve any INV-* comments verbatim above the corresponding `Describe`.

Note: `soak_test.go` likely has long-running specs. If they use `t.Cleanup` for goroutine bookkeeping, translate to `AfterEach`. If they use `t.Parallel()`, drop it (Ginkgo specs are sequential by default; parallelism is opt-in at suite level with `--procs=N` and is out of scope for this PR).

- [ ] **Step 5: Convert the 4 skeletons (Canonical Pattern B)**

For each of:

- `audit_drift_detector_test.go` (holomush-ecbg)
- `backfill_rebuild_test.go` (holomush-l4kx)
- `js_storage_corruption_test.go` (holomush-6nds)
- `multi_protocol_fanout_test.go` (holomush-nko7)

apply Canonical Pattern B. The `Skip(...)` string MUST exactly preserve the existing `TODO(holomush-X)` reference verbatim.

Commit each separately: `test(eventbus_e2e): convert audit_drift_detector skeleton to Ginkgo (cz4s, holomush-ecbg deferred)` etc.

- [ ] **Step 6: Run the Stage-A-final gates**

```bash
# Skeleton grep — exactly 8 hits (Skip-reason line + deeper implementation-hint line per skeleton × 4 skeletons)
rg 'TODO\(holomush-(ecbg|l4kx|6nds|nko7)\)' test/integration/eventbus_e2e/ | wc -l   # expected: 8

# Zero plain-testing in eventbus_e2e EXCEPT suite_test.go helper
rg --files-without-match 'ginkgo|gomega' -g '*_test.go' test/integration/eventbus_e2e/   # expected: only test/integration/eventbus_e2e/suite_test.go

# RunSpecs uniqueness across the package
rg -c 'RunSpecs' test/integration/eventbus_e2e/   # expected: 1

# No t.Skip leaked through
rg 't\.Skip\(' test/integration/eventbus_e2e/   # expected: 0
```

If any gate fails, fix before continuing.

- [ ] **Step 7: Run the full eventbus_e2e suite**

Run: `task test:int -- ./test/integration/eventbus_e2e/... 2>&1 | tail -10`

Expected: all specs pass; the 4 skeleton specs report as SKIPPED with the TODO bead reference in the skip message.

- [ ] **Step 8: pr-prep + push + PR + close**

Same shape as Task A1 Steps 8-12. PR title: `test(eventbus_e2e): migrate 14 files + suite rename to Ginkgo (1hq.26 cz4s)`. After merge, close `holomush-cz4s`.

---

## Task B1: `misc-package-integration-conversion` PR (4 files, 4 packages)

**Goal:** Convert 4 isolated `*_integration_test.go` files spanning 4 distinct packages. Create 3 new bootstraps. Rename the cmd/holomush narrow suite.

**Files:**

- Create: `internal/eventbus/history/history_suite_test.go`
- Create: `internal/totp/totp_suite_test.go`
- Create: `plugins/core-scenes/core_scenes_suite_test.go`
- Modify: `cmd/holomush/admin_authenticate_e2e_suite_test.go` (rename literal at line 35)
- Modify: `cmd/holomush/automigrate_integration_test.go`
- Modify: `internal/eventbus/history/cold_postgres_integration_test.go`
- Modify: `internal/totp/repo_integration_test.go`
- Modify: `plugins/core-scenes/store_integration_test.go`

**Conversion patterns:** Pattern A per file, Pattern C for the 3 new bootstraps, Pattern D for the cmd/holomush rename.

MUST NOT add a second `RunSpecs` to `cmd/holomush/`.

- [ ] **Step 1: Workspace + claim**

```bash
task workspace:new -- 1hq-B1-misc-conversion
cd <printed path>
bd update holomush-rccc.4 --claim
```

- [ ] **Step 2: Rename cmd/holomush suite (commit alone)**

Edit `cmd/holomush/admin_authenticate_e2e_suite_test.go` line 35:

`old_string`: `RunSpecs(t, "Admin Authenticate E2E Lifecycle Suite")`
`new_string`: `RunSpecs(t, "Holomush Integration Suite")`

Commit: `jj describe -m "test(cmd/holomush): rename narrow suite to package-wide name (1hq.26 B1 prep)"`

Run: `task test:int -- ./cmd/holomush/... 2>&1 | tail -5` — expected: existing specs pass under new name.

- [ ] **Step 3: Create the `internal/eventbus/history/history_suite_test.go` bootstrap**

Use Canonical Pattern C with `<pkg>=history`, `<Pkg>=History`, suite name `"History Integration Suite"`. Package declaration MUST match `cold_postgres_integration_test.go`'s package — verify with `rg -n '^package ' internal/eventbus/history/cold_postgres_integration_test.go`.

- [ ] **Step 4: Convert `internal/eventbus/history/cold_postgres_integration_test.go`**

Apply Canonical Pattern A. Commit: `test(eventbus/history): convert cold_postgres to Ginkgo + add suite bootstrap (1hq.26 B1)`.

Run focused: `task test:int -- ./internal/eventbus/history/... -ginkgo.focus="<Describe name>" 2>&1 | tail -5` — expected: pass.

- [ ] **Step 5: Create `internal/totp/totp_suite_test.go` + convert `repo_integration_test.go`**

Pattern C for the bootstrap, Pattern A for the conversion. Single commit: `test(totp): convert repo_integration to Ginkgo + add suite bootstrap (1hq.26 B1)`.

Run focused: `task test:int -- ./internal/totp/... -ginkgo.focus="<Describe name>" 2>&1 | tail -5` — expected: pass.

- [ ] **Step 6: Create `plugins/core-scenes/core_scenes_suite_test.go` + convert `store_integration_test.go`**

Pattern C + Pattern A. Single commit. Run focused test. Note: `plugins/core-scenes/` is `package main` (binary plugin); the bootstrap MUST declare `package main` not `package <pkg>_test`. Verify with `rg -n '^package ' plugins/core-scenes/store_integration_test.go` before creating.

- [ ] **Step 7: Convert `cmd/holomush/automigrate_integration_test.go`**

Apply Canonical Pattern A. The file is `package main`. The dot imports work the same; no new RunSpecs (existing entry at `admin_authenticate_e2e_suite_test.go:35`, just renamed in Step 2).

Commit: `test(cmd/holomush): convert automigrate_integration to Ginkgo (1hq.26 B1)`.

- [ ] **Step 8: Per-package gates**

```bash
# cmd/holomush
rg -c 'RunSpecs' cmd/holomush/   # expected: 1
rg 't\.Skip\(' cmd/holomush/automigrate_integration_test.go   # 0
rg 'func Test|t\.Run' cmd/holomush/automigrate_integration_test.go   # 0

# internal/eventbus/history
rg -c 'RunSpecs' internal/eventbus/history/   # 1
rg 't\.Skip\(' internal/eventbus/history/cold_postgres_integration_test.go   # 0

# internal/totp
rg -c 'RunSpecs' internal/totp/   # 1
rg 't\.Skip\(' internal/totp/repo_integration_test.go   # 0

# plugins/core-scenes
rg -c 'RunSpecs' plugins/core-scenes/   # 1
rg 't\.Skip\(' plugins/core-scenes/store_integration_test.go   # 0
```

- [ ] **Step 9: pr-prep + push + PR + close**

Same shape as Task A1. PR title: `test: migrate 4 misc package-level integration tests to Ginkgo (1hq.26 B1)`. After merge, close the B1 bead.

---

## Task B2: `internal-admin-conversion` PR (2 files, 2 sub-packages)

**Goal:** Convert 2 files in `internal/admin/approval/` and `internal/admin/readstream/` to Ginkgo. Each sub-package gets its own new bootstrap.

**Files:**

- Create: `internal/admin/approval/approval_suite_test.go`
- Create: `internal/admin/readstream/readstream_suite_test.go`
- Modify: `internal/admin/approval/repo_integration_test.go`
- Modify: `internal/admin/readstream/cold_reader_integration_test.go`

**Conversion patterns:** Pattern C × 2 (separate bootstraps per sub-package) + Pattern A × 2.

- [ ] **Step 1: Workspace + claim**

```bash
task workspace:new -- 1hq-B2-admin-conversion
cd <printed path>
bd update holomush-rccc.5 --claim
```

- [ ] **Step 2: Create `internal/admin/approval/approval_suite_test.go`**

Apply Pattern C with `<pkg>=approval`, `<Pkg>=Approval`, suite name `"Approval Integration Suite"`. Package declaration MUST match `repo_integration_test.go` — verify via `rg -n '^package ' internal/admin/approval/repo_integration_test.go` (spec asserts `package approval_test`).

- [ ] **Step 3: Convert `internal/admin/approval/repo_integration_test.go`**

Apply Canonical Pattern A. Commit (one for steps 2+3 combined): `test(admin/approval): convert repo_integration to Ginkgo + add suite bootstrap (1hq.26 B2)`.

Run focused: `task test:int -- ./internal/admin/approval/... -ginkgo.focus="<Describe name>" 2>&1 | tail -5` — expected: pass.

- [ ] **Step 4: Create `internal/admin/readstream/readstream_suite_test.go`**

Pattern C with `<pkg>=readstream`, `<Pkg>=Readstream`, suite name `"Readstream Integration Suite"`. Package declaration MUST match `cold_reader_integration_test.go` — spec asserts `package readstream` (NOT `_test`).

- [ ] **Step 5: Convert `internal/admin/readstream/cold_reader_integration_test.go`**

Pattern A. Single combined commit with Step 4: `test(admin/readstream): convert cold_reader_integration to Ginkgo + add suite bootstrap (1hq.26 B2)`.

Run focused. Expected: pass.

- [ ] **Step 6: Per-sub-package gates**

```bash
rg -c 'RunSpecs' internal/admin/approval/   # 1
rg -c 'RunSpecs' internal/admin/readstream/   # 1
rg 't\.Skip\(' internal/admin/approval/repo_integration_test.go internal/admin/readstream/cold_reader_integration_test.go   # 0
```

- [ ] **Step 7: pr-prep + push + PR + close**

Same shape. PR title: `test(internal/admin): migrate approval + readstream to Ginkgo (1hq.26 B2)`. After merge, close the B2 bead.

---

## Task B3: `internal-store-conversion` PR (4 files)

**Goal:** Convert 4 `*_integration_test.go` files in `internal/store/` into the existing `store_suite_test.go:20` `Store Suite`. Closes the re-purposed `holomush-1hq.60`.

**Files:**

- Modify: `internal/store/migrate_integration_test.go`
- Modify: `internal/store/migrate_plugins_integration_test.go`
- Modify: `internal/store/migrations_audit_shape_integration_test.go`
- Modify: `internal/store/role_store_integration_test.go`

**Conversion patterns:** Pattern A per file. NO new bootstrap. MUST NOT add a second `RunSpecs`.

- [ ] **Step 1: Workspace + claim**

```bash
task workspace:new -- 1hq-B3-store-conversion
cd <printed path>
bd update holomush-1hq.60 --claim
```

- [ ] **Step 2-5: Convert each file (one commit per file)**

For each of the 4 files: apply Pattern A, run focused test, commit. Example: `test(store): convert migrate_integration to Ginkgo (1hq.60 / 1hq.26 B3)`.

Notes:

- These files use a real PG testcontainer via `testutil.SharedPostgres`. Setup is per-test today via `setupTestDB(t)` or similar; in Ginkgo, lift to `BeforeEach` with `setupTestDB(GinkgoT())`. If teardown is per-test, use `AfterEach`. Verify the existing fixture helper signature with `rg -n 'func setupTestDB' internal/store/`.
- `migrations_audit_shape_integration_test.go` exercises the events_audit projection — preserve every assertion ordering; these can be schema-sensitive.

- [ ] **Step 6: Gates**

```bash
rg -c 'RunSpecs' internal/store/   # 1
rg 't\.Skip\(' internal/store/{migrate,migrate_plugins,migrations_audit_shape,role_store}_integration_test.go   # 0
rg 'func Test|t\.Run' internal/store/{migrate,migrate_plugins,migrations_audit_shape,role_store}_integration_test.go   # 0
task test:int -- ./internal/store/... 2>&1 | tail -10   # all pass
```

- [ ] **Step 7: pr-prep + push + PR + close**

Same shape. PR title: `test(internal/store): migrate 4 integration tests to Ginkgo (1hq.60 / 1hq.26 B3)`. After merge, close `holomush-1hq.60`.

---

## Task B4: `internal-eventbus-crypto-conversion` PR (8 files, dek + kek)

**Goal:** Convert 6 `dek/*_integration_test.go` files into the existing `dek_integration_suite_test.go:25` entry. Create 1 new bootstrap for `kek/` and convert 2 `kek/*_integration_test.go` files into it. Largest Stage B PR.

**Files:**

- Create: `internal/eventbus/crypto/kek/kek_suite_test.go`
- Modify: `internal/eventbus/crypto/dek/checkpoint_integration_test.go`
- Modify: `internal/eventbus/crypto/dek/manager_integration_test.go`
- Modify: `internal/eventbus/crypto/dek/rekey_phase5_integration_test.go`
- Modify: `internal/eventbus/crypto/dek/rekey_phase6_integration_test.go`
- Modify: `internal/eventbus/crypto/dek/rekey_phase7_integration_test.go`
- Modify: `internal/eventbus/crypto/dek/store_integration_test.go`
- Modify: `internal/eventbus/crypto/kek/local_aead_integration_test.go`
- Modify: `internal/eventbus/crypto/kek/none_integration_test.go`

**Conversion patterns:** Pattern A × 8 + Pattern C × 1 (kek bootstrap).

MUST NOT add a second `RunSpecs` to `internal/eventbus/crypto/dek/`.

- [ ] **Step 1: Workspace + claim**

```bash
task workspace:new -- 1hq-B4-eventbus-crypto-conversion
cd <printed path>
bd update holomush-rccc.6 --claim
```

- [ ] **Step 2-7: Convert each `dek/*_integration_test.go` file (one commit per file)**

For each of the 6 dek files: apply Pattern A, run focused test, commit. Notes:

- These files are crypto-domain. Per CLAUDE.md, changes to `internal/eventbus/crypto/` trigger `crypto-reviewer`. The PR MUST run that reviewer before push.
- `manager_integration_test.go` and `checkpoint_integration_test.go` reference DEK lifecycle invariants. Preserve every INV-* comment verbatim above the corresponding `Describe`.
- `rekey_phase{5,6,7}_integration_test.go` are phase-specific. Each phase's INV-Ex comments stay; the `Describe` name should mirror the phase.

- [ ] **Step 8: Create `internal/eventbus/crypto/kek/kek_suite_test.go`**

Pattern C with `<pkg>=kek`, `<Pkg>=KEK`, suite name `"KEK Integration Suite"`. Verify package declaration via `rg -n '^package ' internal/eventbus/crypto/kek/local_aead_integration_test.go`.

- [ ] **Step 9: Convert `kek/local_aead_integration_test.go` + `kek/none_integration_test.go`**

Pattern A per file. One commit per file. Combined with Step 8's bootstrap into a single 3-file commit IS acceptable here since the bootstrap is necessary for the converted files to discover.

- [ ] **Step 10: Per-sub-package gates**

```bash
rg -c 'RunSpecs' internal/eventbus/crypto/dek/   # 1
rg -c 'RunSpecs' internal/eventbus/crypto/kek/   # 1
rg 't\.Skip\(' internal/eventbus/crypto/{dek,kek}/   # 0
rg 'func Test|t\.Run' internal/eventbus/crypto/{dek,kek}/*_integration_test.go   # 0
task test:int -- ./internal/eventbus/crypto/dek/... ./internal/eventbus/crypto/kek/... 2>&1 | tail -10   # all pass
```

- [ ] **Step 11: Run crypto-reviewer (REQUIRED for this PR)**

Per CLAUDE.md, changes to `internal/eventbus/crypto/` require `crypto-reviewer` BEFORE `code-reviewer`. Dispatch:

Invoke the `/review-crypto` skill (or the `crypto-reviewer` agent directly) with target = current branch diff. Address any NOT READY findings before continuing.

- [ ] **Step 12: pr-prep + push + PR + close**

Same shape. PR title: `test(eventbus/crypto): migrate dek + kek integration tests to Ginkgo (1hq.26 B4)`. After merge, close the B4 bead.

---

## Stage-B-final gate (run after all 4 Stage B PRs merge)

- [ ] **Step 1: Verify zero plain-testing integration files repo-wide**

Run: `rg --files-without-match 'ginkgo|gomega' -g '*_integration_test.go' 2>&1`

Expected: empty output. If any file is still listed, identify which Stage B PR missed it and re-open the corresponding bead.

---

## Task C: Close `holomush-1hq.26` (Stage C)

**Goal:** With all 8 conversion PRs merged and all child beads closed, close the umbrella `holomush-1hq.26` feature bead with a grounded attestation.

**Files:** none.

- [ ] **Step 1: Verify all 12 dependent beads closed**

Run:

```bash
for bd_id in 1hq.49 1hq.59 1hq.60 1hq.61 1hq.62 cz4s; do
  bd show holomush-$bd_id 2>&1 | rg '^○|^●|Close reason' | head -2
done
# Plus the 6 new beads filed in Task 0 — replace with their actual IDs:
for bd_id in <A1-id> <A2-id> <A3-id> <B1-id> <B2-id> <B4-id>; do
  bd show holomush-$bd_id 2>&1 | rg '^○|^●|Close reason' | head -2
done
```

Expected: every line shows `● closed` (or `✓` in some bd renderings) with a close reason.

- [ ] **Step 2: Run the Stage-A-final + Stage-B-final gates one more time**

```bash
# Stage A
rg --files-without-match 'ginkgo|gomega' -g '*_test.go' test/integration/   # expected: only test/integration/eventbus_e2e/suite_test.go
rg 'TODO\(holomush-(ecbg|l4kx|6nds|nko7)\)' test/integration/eventbus_e2e/ | wc -l   # 8

# Stage B
rg --files-without-match 'ginkgo|gomega' -g '*_integration_test.go' 2>&1   # empty
```

- [ ] **Step 3: Verify `.claude/rules/testing.md` covers the 4 documented patterns**

Run:

```bash
rg -c 'testify|assert\.|require\.' .claude/rules/testing.md   # > 0
rg -c 'mockery|mocks\.New' .claude/rules/testing.md   # > 0
rg -c 'Describe|Context|It' .claude/rules/testing.md   # > 0
rg -c 'Eventually|Consistently' .claude/rules/testing.md   # > 0
```

Expected: all 4 patterns return positive counts. If any returns 0, update `.claude/rules/testing.md` in a follow-up PR before closing.

- [ ] **Step 4: Close `holomush-1hq.26`**

Run:

```bash
bd close holomush-1hq.26 --reason "All 40 plain-testing.T integration files migrated to Ginkgo (22 test/integration/ + 18 package-level). All 4 skeletons preserved with grep-able backing-bead refs (ecbg, l4kx, 6nds, nko7). Dependencies testify v1.11.1 + ginkgo/v2 v2.28.1 + gomega v1.39.1 in go.mod. Mockery configured via .mockery.yaml with 20+ generated mocks. Testify/ginkgo patterns documented in .claude/rules/testing.md. Spec: docs/superpowers/specs/2026-05-15-testify-ginkgo-migration-completion-design.md. Plan: docs/superpowers/plans/2026-05-15-testify-ginkgo-migration-completion-plan.md."
```

- [ ] **Step 5: Close `holomush-rccc` (design bead)**

Run:

```bash
bd close holomush-rccc --reason "Design + plan + execution complete. holomush-1hq.26 closed with grounded attestation."
bd dolt push
```

- [ ] **Step 6: Update memory with the migration completion**

Optional but recommended: append a `bd note` to the umbrella `holomush-1hq` epic recording the migration completion date and the spec/plan paths. Bead `1hq` (Epic 2: Plugin System) remains open for unrelated work.

---

## Post-implementation checklist

- [ ] All 8 PRs merged to `main`.
- [ ] All conversion beads closed with PR references.
- [ ] `holomush-1hq.26` closed with the grounded attestation above.
- [ ] `holomush-rccc` (design bead) closed.
- [ ] `bd dolt push` ran after every bead state change.
- [ ] No `t.Skip(` remains in any `*_integration_test.go` file repo-wide.
- [ ] No plain-`testing.T` integration files remain (Stage-B-final gate empty).
- [ ] The 4 skeleton TODO references (`holomush-{ecbg,l4kx,6nds,nko7}`) are still grep-able in `test/integration/eventbus_e2e/`.
- [ ] `task test:int` is green on `main`.

<!-- adr-capture: sha256=6a2a56a70030199c; session=cli; ts=2026-05-15T17:36:38Z; adrs= -->

<!-- adr-capture: sha256=3084f160f0e9f060; session=cli; ts=2026-05-16T22:31:10Z; adrs=holomush-1f1w,holomush-iv7l -->
