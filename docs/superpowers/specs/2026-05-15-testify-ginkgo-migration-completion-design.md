# Testify + Ginkgo Migration Completion Design

This document defines the execution plan for completing the testify + ginkgo
test-framework adoption tracked under `holomush-1hq.26`.

**Design bead:** holomush-rccc
**Parent epic feature:** holomush-1hq.26 (Implement testing libraries adoption)
**Predecessor design (Jan 2026):** docs/plans/2026-01-20-testing-libraries-design.md
**Date:** 2026-05-15
**Author:** Claude (with Sean)

## Summary

The Jan 2026 testify + ginkgo migration is ~80% executed by file count: 553 Go
files use testify (unit-test side fully adopted) and 88 files use Ginkgo
(integration side mostly adopted). **40 integration test files** repo-wide
remain on plain `testing.T` — 22 under `test/integration/` (cross-package
suites) and 18 in package-level `*_integration_test.go` files outside that
tree. 4 of the 22 are intentional skip-skeletons whose backing features are
deferred to real open feature beads.

The Jan 2026 design only counted 4 integration files; the 18 outside
`test/integration/` are mostly crypto-domain integration tests added after the
framework migration began, written in the prevailing plain-`testing.T` style
because the conversion treated `test/integration/` as a scope boundary.

This spec specifies the **aggressive uniform completion** path: convert all 40
remaining integration tests (including the 4 skeletons) to Ginkgo in two
phases:

- **Phase A — `test/integration/` (22 files, 4 PRs).** The original spec
  scope: small per-package PRs, smallest-first. Skeletons preserved via
  `Skip("TODO(holomush-X)...")` inside `It`.
- **Phase B — Package-level integration tests (18 files, 4 PRs).** New beads
  per directory cluster: crypto (dek + kek = 8 files), store (4 files,
  re-purposes `1hq.60`), admin (2 files), misc (4 files: cmd/holomush,
  internal/eventbus/history, internal/totp, plugins/core-scenes).

`holomush-1hq.26` closes only when both phases land.

## Goals

- Bring **every** integration test file (any `*_integration_test.go` outside
  the unit-test set OR any `_test.go` under `test/integration/`) under the
  Ginkgo / Gomega framework so the integration test suite has uniform style
  and lifecycle semantics.
- Preserve the existing skip-skeleton forcing function (skeleton tests block
  implementation of their backing feature beads from being declared "done"
  without a working test).
- Close the stale 1hq.* child beads whose target files have been converted by
  unrelated work since Jan 2026; re-purpose `1hq.60` for the (now-correctly
  inventoried) `internal/store/` integration test cluster.
- Land small, reviewable PRs grouped by directory cluster: 4 Phase A PRs +
  4 Phase B PRs = 8 total PRs, with each phase opening on its smallest
  pattern-establishing PR and closing on its largest.

## Non-goals

- Migrating unit tests off plain `testing.T`. testify layers on top of
  `testing.T`; the project pattern keeps `testing.T` as the entry point for
  unit tests.
- Changing the CLAUDE.md / `.claude/rules/testing.md` policy that mandates
  Ginkgo for integration tests. The user explicitly chose to execute the
  existing policy uniformly.
- Touching `internal/plugin/*_integration_test.go` (objects, help,
  communication, building, integration_test.go) — these are already Ginkgo.
- Implementing any of the 4 deferred features that own the skip-skeletons.
- Adopting `mockery` more broadly. This spec scopes to the testing-framework
  conversion only; any mockery adoption is tracked separately under the
  parent `holomush-1hq.26` feature.

## Scope inventory

The inventories below were re-derived by running
`rg --files-without-match 'ginkgo|gomega' -g '*_test.go' test/integration/`
plus
`rg --files-without-match 'ginkgo|gomega' -g '*_integration_test.go'`
from the workspace root on 2026-05-15. The earlier draft of this spec
undercounted by 5 files in Phase A and missed Phase B entirely.

### Phase A — `test/integration/` cross-package suites (22 files)

| Package                          | Count | Of which skeletons |
| -------------------------------- | ----- | ------------------ |
| `test/integration/eventbus_e2e/` | 14    | 4                  |
| `test/integration/plugin/`       | 4     | 0                  |
| `test/integration/crypto/`       | 3     | 0                  |
| `test/integration/auth/`         | 1     | 0                  |
| **Phase A TOTAL**                | **22** | **4**             |

Note: `test/integration/eventbus_e2e/suite_test.go` (in the rg output above) is
helper-only and is handled by the suite-bootstrap discussion below; it is NOT
counted in the 14 figure.

#### `test/integration/eventbus_e2e/` (14 files)

- `audit_drift_detector_test.go` (skeleton, holomush-ecbg)
- `audit_only_channel_test.go` (live)
- `backfill_rebuild_test.go` (skeleton, holomush-l4kx)
- `cross_tier_query_test.go` (live)
- `dispatcher_selector_identity_test.go` (live)
- `js_storage_corruption_test.go` (skeleton, holomush-6nds)
- `multi_protocol_fanout_test.go` (skeleton, holomush-nko7)
- `plugin_audit_isolation_test.go` (live)
- `plugin_audit_round_trip_test.go` (live)
- `plugin_crash_resilience_test.go` (live)
- `plugin_downgrade_attacker_test.go` (live)
- `reconnect_resume_test.go` (live)
- `rendering_completeness_test.go` (live)
- `soak_test.go` (live)

#### `test/integration/plugin/` (4 files)

- `counting_proxy_test.go` (live)
- `plugin_migration_test.go` (live)
- `plugin_role_permissions_test.go` (live)
- `schema_isolation_test.go` (live)

#### `test/integration/crypto/` (3 files)

- `emit_test.go` (live)
- `metadata_only_test.go` (live)
- `plugin_decrypt_test.go` (live)

#### `test/integration/auth/` (1 file)

- `core_client_shim_test.go` (live)

### Phase B — Package-level integration tests (18 files)

| Directory cluster                  | Count | Owning bead          |
| ---------------------------------- | ----- | -------------------- |
| `internal/eventbus/crypto/`        | 8     | _new_ (dek + kek)    |
| `internal/store/`                  | 4     | `holomush-1hq.60` (re-purposed) |
| `internal/admin/`                  | 2     | _new_                |
| Misc (cmd/holomush, eventbus/history, totp, plugins/core-scenes) | 4 | _new_ |
| **Phase B TOTAL**                  | **18** |                     |

#### `internal/eventbus/crypto/` (8 files)

- `dek/checkpoint_integration_test.go`
- `dek/manager_integration_test.go`
- `dek/rekey_phase5_integration_test.go`
- `dek/rekey_phase6_integration_test.go`
- `dek/rekey_phase7_integration_test.go`
- `dek/store_integration_test.go`
- `kek/local_aead_integration_test.go`
- `kek/none_integration_test.go`

#### `internal/store/` (4 files — re-purposes `1hq.60`)

- `migrate_integration_test.go`
- `migrate_plugins_integration_test.go`
- `migrations_audit_shape_integration_test.go`
- `role_store_integration_test.go`

`internal/store/store_suite_test.go` already provides the `RunSpecs` entry
(`TestStore`); it currently runs an effectively-empty suite because the
integration files in the package are not Ginkgo specs. Conversion fills that
suite.

#### `internal/admin/` (2 files, 2 sub-packages)

- `internal/admin/approval/repo_integration_test.go` (`package approval_test`)
- `internal/admin/readstream/cold_reader_integration_test.go` (`package readstream`)

These are **separate sub-packages**, neither sharing a Ginkgo suite with the
other or with `internal/admin/socket/` / `internal/admin/policy/` (which
have their own existing suites). The `internal-admin-conversion` PR
therefore creates **two distinct `*_suite_test.go` files** — one in each
sub-package — and converts the corresponding integration test.

#### Misc (4 files)

- `cmd/holomush/automigrate_integration_test.go`
- `internal/eventbus/history/cold_postgres_integration_test.go`
- `internal/totp/repo_integration_test.go`
- `plugins/core-scenes/store_integration_test.go`

Each is the only integration file in its package; each needs a
`*_suite_test.go` bootstrap.

### Skeleton validation

All 4 backing feature beads MUST exist, be open, and point to their skeleton
file before conversion begins. Validation outcome (recorded
2026-05-15):

| Skeleton file                       | Backing bead         | Status              | Verified-unimplemented |
| ----------------------------------- | -------------------- | ------------------- | ---------------------- |
| `audit_drift_detector_test.go`      | `holomush-ecbg` [P2] | OPEN, feature       | Yes                    |
| `backfill_rebuild_test.go`          | `holomush-l4kx` [P2] | OPEN, feature       | Yes                    |
| `js_storage_corruption_test.go`     | `holomush-6nds` [P3] | OPEN, feature       | Yes                    |
| `multi_protocol_fanout_test.go`     | `holomush-nko7` [P3] | OPEN, feature       | Yes                    |

"Verified-unimplemented" was checked by probing the codebase (`rg` against
`DriftDetector`, `audit-backfill`, `RebuildFromAudit`, multi-adapter harness)
and finding zero production matches. Each bead description explicitly
references its skeleton file path. The skeletons MUST be preserved through
conversion.

### Bead bookkeeping

Pre-verified evidence (2026-05-15) drives concrete actions, not "audit" placeholders:

| Bead        | Action                              | Evidence                                                                                      |
| ----------- | ----------------------------------- | --------------------------------------------------------------------------------------------- |
| `1hq.49`    | Close as done                       | `go.mod` requires testify v1.11.1, ginkgo/v2 v2.28.1, gomega v1.39.1. Mockery is adopted via `.mockery.yaml` + 5+ generated `*/mocks/mock_*.go` outputs (mockery is a CLI; not a go.mod requirement). |
| `1hq.59`    | Close as done                       | `test/integration/phase1_5_test.go` imports ginkgo/v2 + gomega                                |
| `1hq.60`    | **Re-purpose, do not close**        | Original target `internal/store/postgres_integration_test.go` does not exist (verified `ls`). The `store_suite_test.go:20` bootstrap exists but the 4 actual integration files (`migrate_integration_test.go`, `migrate_plugins_integration_test.go`, `migrations_audit_shape_integration_test.go`, `role_store_integration_test.go`) are all plain `testing.T`. New acceptance criteria: the 5 §Verification Per-PR gates pass for those 4 files. `bd note 1hq.60` records the re-scope rationale and links the historical target. |
| `1hq.61`    | Close as done                       | `internal/plugin/integration_test.go` imports ginkgo/v2 + gomega                              |
| `1hq.62`    | Close as done — verified by predicate below | `.claude/rules/testing.md` must contain working examples of all 4 patterns the bead enumerates: (a) testify `assert`/`require`; (b) mockery-generated mock usage; (c) ginkgo `Describe`/`Context`/`It`; (d) `Eventually`/`Consistently`. Spec author has spot-checked (a), (c), and (d) present in the rules file; (b) cross-reference to mockery configuration noted. Close attestation MUST cite the four section names. |
| `cz4s`      | Keep — owns Phase A `eventbus_e2e`  | Existing acceptance criteria align with the 14-file Phase A scope                             |
| Phase A _new_ × 3 | File one per remaining `test/integration/` package (auth, plugin, crypto) | Each new bead owns one PR                                                              |
| Phase B _new_ × 3 | File one per package-level cluster: `internal/eventbus/crypto` (dek+kek), `internal/admin` (approval+readstream), misc (cmd/holomush + history + totp + core-scenes) | `1hq.60` covers the fourth Phase B cluster (`internal/store`)                          |

All claims above are verifiable today with the cited grep / `ls` / `bd show`
commands.

## Conversion pattern

### Live test files

```go
// BEFORE
func TestFoo(t *testing.T) {
    setup := mustSetup(t)
    t.Run("does X", func(t *testing.T) {
        got, err := setup.Do()
        require.NoError(t, err)
        assert.Equal(t, want, got)
    })
}
```

```go
// AFTER
var _ = Describe("Foo", func() {
    var setup *fooSetup
    BeforeEach(func() { setup = mustSetup(GinkgoT()) })

    It("does X", func() {
        got, err := setup.Do()
        Expect(err).NotTo(HaveOccurred())
        Expect(got).To(Equal(want))
    })
})
```

### Skeleton files

Preserve the TODO bead reference verbatim — it is a contract with the future
implementer of the backing feature.

```go
// BEFORE
func TestAuditDriftDetectorReportsTamperedRow(t *testing.T) {
    t.Skip("TODO(holomush-ecbg): drift detector not yet implemented — skeleton retained for the follow-up bead")
}
```

```go
// AFTER
var _ = Describe("Audit drift detector", func() {
    It("reports tampered rows (INV-…)", func() {
        Skip("TODO(holomush-ecbg): drift detector not yet implemented — skeleton retained for the follow-up bead")
    })
})
```

Ginkgo's `Skip()` MUST be called only from a Run-Phase node (`It`, `BeforeEach`,
`BeforeAll`, `BeforeSuite`, etc.). A `Skip()` call at top-level / inside a
`var _ = Describe(...)` body runs during Ginkgo's Tree Construction Phase,
which forbids `Skip()` and panics the suite. The grep gate in §Verification
proves the TODO marker string is present in source; the panic-on-misuse is
itself a loud failure mode (not silent), so the runtime contract is
self-enforcing as long as `Skip()` lives inside an `It`. Reviewers should
also confirm no `Skip()` accidentally ends up inside a `BeforeAll` /
`BeforeSuite`, which would silently skip a wider scope than intended.

### Suite bootstrap

Pre-existing `RunSpecs` entries across all 13 target packages (verified
2026-05-15 by `rg -n 'RunSpecs' <pkg>/`):

| Package                                   | Existing `RunSpecs`?  | File:line                                | Suite name                                     |
| ----------------------------------------- | --------------------- | ---------------------------------------- | ---------------------------------------------- |
| `test/integration/auth/`                  | yes                   | `auth_suite_test.go:39`                  | "Player Session Lifecycle Integration Suite"   |
| `test/integration/crypto/`                | yes                   | `crypto_suite_test.go:26`                | "Crypto Integration Suite"                     |
| `test/integration/plugin/`                | yes                   | `plugin_suite_test.go:25`                | "Binary Plugin Integration Suite"              |
| `test/integration/eventbus_e2e/`          | yes (narrow)          | `cursor_concurrent_suite_test.go:30`     | "Cursor Concurrent Pagination Specs"           |
| `internal/store/`                         | yes                   | `store_suite_test.go:20`                 | "Store Suite"                                  |
| `internal/eventbus/crypto/dek/`           | yes                   | `dek_integration_suite_test.go:25`       | "DEK Integration Suite"                        |
| `cmd/holomush/`                           | yes (narrow)          | `admin_authenticate_e2e_suite_test.go:35` | "Admin Authenticate E2E Lifecycle Suite"      |
| `internal/eventbus/crypto/kek/`           | **no**                | —                                        | —                                              |
| `internal/admin/approval/`                | **no**                | —                                        | —                                              |
| `internal/admin/readstream/`              | **no**                | —                                        | —                                              |
| `internal/eventbus/history/`              | **no**                | —                                        | —                                              |
| `internal/totp/`                          | **no**                | —                                        | —                                              |
| `plugins/core-scenes/`                    | **no**                | —                                        | —                                              |

**Implications per package:**

- **Packages with an existing entry, no rename required (5 of 7): auth,
  crypto, plugin, internal/store, internal/eventbus/crypto/dek** — the
  conversion PR adds Ginkgo specs into the existing suite via
  `var _ = Describe(...)`. It MUST NOT add a second `RunSpecs` entry. No
  bootstrap edit required. (The other 2 packages with existing entries —
  eventbus_e2e and cmd/holomush — also have existing `RunSpecs` but
  additionally need a suite rename; see the dedicated bullets below.)
- **`test/integration/eventbus_e2e/`** (existing entry with narrow name) —
  the conversion PR renames the suite to "EventbusE2E Suite" in the
  `cursor_concurrent_suite_test.go:30` call, drops the vacuous
  `TestSuiteCompiles` in `suite_test.go`, and removes the `holomush-suos.2`
  follow-up comment in `cursor_concurrent_suite_test.go:20-22` which becomes
  inert. MUST NOT add a second `RunSpecs` entry.
- **`cmd/holomush/`** (existing entry with narrow name) — same shape as
  eventbus_e2e: the conversion PR for `automigrate_integration_test.go`
  renames the suite to "Holomush Integration Suite" (or similar
  package-scoped name) at `admin_authenticate_e2e_suite_test.go:35`. MUST
  NOT add a second `RunSpecs` entry.
- **Packages without an existing entry (6 of 13): internal/eventbus/crypto/kek,
  internal/admin/approval, internal/admin/readstream, internal/eventbus/history,
  internal/totp, plugins/core-scenes** — a new `*_suite_test.go` file is
  required, with the canonical bootstrap:

  ```go
  func TestX(t *testing.T) {
      RegisterFailHandler(Fail)
      RunSpecs(t, "X Suite")
  }
  ```

  Note that `internal/admin/approval/` and `internal/admin/readstream/` are
  **separate sub-packages**, each requiring its own bootstrap; the
  `internal-admin-conversion` PR therefore creates two new
  `*_suite_test.go` files.

Every conversion PR MUST verify (via `rg -c 'RunSpecs' <package>/`) that
the package has exactly **one** `RunSpecs` entry after the change.

## Sequencing

### Stage 0 — Bead audit & cleanup (one bead-DB sync, no code)

- Close `1hq.49`, `1hq.59`, `1hq.61`, `1hq.62` per the §Bead bookkeeping
  evidence table.
- Re-purpose `1hq.60` for `internal/store/`: update title + description +
  acceptance criteria to match the current 4-file scope.
- File 6 new conversion beads:
  - Phase A × 3: `test/integration/auth`, `test/integration/plugin`,
    `test/integration/crypto`.
  - Phase B × 3: `internal/eventbus/crypto` (dek + kek), `internal/admin`,
    `misc-package-integration` (cmd/holomush + eventbus/history + totp +
    plugins/core-scenes).

No code changes. Verifiable via `bd show 1hq.26` after.

### Stage A — `test/integration/` conversion (4 PRs, smallest first → largest last)

| Order | PR                            | Bead         | Files | Notes                                              |
| ----- | ----------------------------- | ------------ | ----- | -------------------------------------------------- |
| A1    | `auth-conversion`             | new          | 1     | Smallest — establishes the pattern. Existing `RunSpecs` at `auth_suite_test.go:39`; MUST NOT add a second |
| A2    | `plugin-conversion`           | new          | 4     | Pattern is now proven. Existing `RunSpecs` at `plugin_suite_test.go:25`; MUST NOT add a second |
| A3    | `crypto-conversion`           | new          | 3     | Uses the proven pattern. Existing `RunSpecs` at `crypto_suite_test.go:26`; MUST NOT add a second |
| A4    | `eventbus-e2e-conversion`     | `cz4s`       | 14    | Largest in Stage A; includes 4 skeletons, suite rename (existing entry "Cursor Concurrent Pagination Specs" → "EventbusE2E Suite"), drop `TestSuiteCompiles`; MUST NOT add a second `RunSpecs` to `test/integration/eventbus_e2e/` |

Pattern-establishing PR first, largest PR last. The `auth-conversion` PR
establishes the project's canonical "convert one file into an existing
Ginkgo suite" pattern (1 file, smallest possible) that A2–A4 follow. The
middle two PRs (plugin = 4, crypto = 3) are not strictly monotonic but are
both small enough that their relative order is irrelevant; what matters is
that A1 is cheapest (establishes pattern) and A4 is largest (proves the
pattern at scale, includes skeleton handling and suite rename).

### Stage B — Package-level conversion (4 PRs, smallest first → largest last)

| Order | PR                                       | Bead          | Files | Notes                                              |
| ----- | ---------------------------------------- | ------------- | ----- | -------------------------------------------------- |
| B1    | `misc-package-integration-conversion`    | new           | 4     | 3 new bootstraps (`internal/eventbus/history/`, `internal/totp/`, `plugins/core-scenes/`) + `cmd/holomush/` suite rename (existing entry "Admin Authenticate E2E Lifecycle Suite" → "Holomush Integration Suite"); MUST NOT add a second `RunSpecs` to `cmd/holomush/` |
| B2    | `internal-admin-conversion`              | new           | 2     | Two sub-packages (approval, readstream); 2 new bootstraps (one per sub-package); no existing entries in either |
| B3    | `internal-store-conversion`              | `1hq.60`      | 4     | Existing bootstrap at `store_suite_test.go:20`; just add specs; MUST NOT add a second `RunSpecs` |
| B4    | `internal-eventbus-crypto-conversion`    | new           | 8     | Largest in Stage B; dek (6 files, existing bootstrap at `dek_integration_suite_test.go:25`) + kek (2 files, NEW bootstrap); MUST NOT add a second `RunSpecs` to `dek/` |

Stage B may run in parallel with Stage A — there is no code-level dependency
between `test/integration/` cross-package suites and package-level
`*_integration_test.go` files. Both stages converge on the same Ginkgo runtime.

Each PR MUST run `task pr-prep` to full completion before push. PRs touching
skeleton files (A4) get extra reviewer attention on the `Skip()` preservation
in the 4 skeleton files.

### Stage C — Close `1hq.26`

After all 4 Stage A PRs AND all 4 Stage B PRs land AND all child beads close,
close `1hq.26` with attestation: "all 40 plain-`testing.T` integration files
migrated to Ginkgo (22 `test/integration/` + 18 package-level); all 4
skeletons preserved with grep-able backing-bead references; testify + ginkgo

- gomega adopted in `go.mod`; mockery configured via `.mockery.yaml`; the
testify/ginkgo patterns are documented in `.claude/rules/testing.md`."

## Verification

Per-PR gates (apply to every Stage A and Stage B conversion PR):

| Gate                            | Check                                                                                                                |
| ------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| `task test:int`                 | Converted files compile under `//go:build integration` and pass                                                       |
| `task pr-prep`                  | Full CI mirror (lint, format, schema, license, unit, integration, E2E) green                                          |
| `t.Skip` audit                  | `rg 't\.Skip\(' <converted-package>/` returns zero hits — no plain-testing skip leaked through                        |
| `RunSpecs` uniqueness           | `rg -c 'RunSpecs' <converted-package>/` returns exactly `1` after the change                                          |
| Suite discoverability           | `go test -tags=integration ./<converted-package>/...` runs at least one Ginkgo spec                                   |

Stage-A-final gate (after A4 / `cz4s` lands):

| Gate                  | Check                                                                                                            |
| --------------------- | ---------------------------------------------------------------------------------------------------------------- |
| Skeleton grep         | `rg 'TODO\(holomush-(ecbg\|l4kx\|6nds\|nko7)\)' test/integration/eventbus_e2e/` returns 4 hits                  |
| `test/integration/` zero plain-testing | `rg --files-without-match 'ginkgo\|gomega' -g '*_test.go' test/integration/` returns ONLY `suite_test.go` |

Stage-B-final gate (after all 4 Stage B PRs land):

| Gate                  | Check                                                                                                           |
| --------------------- | --------------------------------------------------------------------------------------------------------------- |
| Repo-wide zero plain-testing | `rg --files-without-match 'ginkgo\|gomega' -g '*_integration_test.go'` returns empty                       |

Pre-`1hq.26`-close gate (Stage C):

- All 8 conversion PRs merged.
- All child beads (`1hq.49`, `1hq.59`, `1hq.60`, `1hq.61`, `1hq.62`, `cz4s`,
  6 new conversion beads) closed.
- `.claude/rules/testing.md` contains working examples of: testify
  `assert`/`require`; mockery-generated mock usage; ginkgo
  `Describe`/`Context`/`It`; `Eventually`/`Consistently`.

## Risks

| Risk                                                                                       | Mitigation                                                                                                |
| ------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------- |
| `Skip()` at top-level Tree Construction Phase panics the suite                              | Code review checklist: every `Skip()` call MUST be inside an `It` or `Before*` block. The Stage-A-final skeleton-grep gate proves the TODO string is present in source but not where it runs.                                              |
| `Skip()` accidentally placed inside `BeforeAll` / `BeforeSuite` silently skips broader scope | Code review: skeleton tests use `Skip()` inside `It`, not `Before*`. PR template flags this on any `Before*` containing `Skip()`. |
| Per-`It` `BeforeEach` resets expensive setup (testcontainer PG) and blows up wall time     | Use `BeforeAll` (Ginkgo v2 Ordered containers) for one-time setup; mirror the pre-conversion fixture shape                                                                                                                                  |
| Shared `var` scoping across `It` blocks regresses test isolation                            | Code review: declare shared state at `var _ = Describe` body level, assign in `BeforeEach`, never inline                                                                                                                                   |
| 14-file `eventbus_e2e` diff is large                                                       | Per-file commits within the single PR; each file is independent. Reviewer can step through commits rather than reading the squash diff.                                                                                                    |
| Stage A and Stage B run in parallel and trip over each other's bead numbering / branch names | Distinct bead chains (Stage A under `cz4s` + 3 new; Stage B under `1hq.60` + 3 new); distinct branch-name prefixes (`stage-a-*` and `stage-b-*`). No file-level overlap (Stage A is `test/integration/`, Stage B is everything else).      |
| Stage B suite-bootstrap regression: a new `*_suite_test.go` introduces a second `RunSpecs` for a package that already had one | Per-PR `RunSpecs` uniqueness gate above. The 7 packages that already have entries are listed in §"Suite bootstrap"; reviewers cross-check.                                                                                                                                        |
| 1hq.60 re-purpose changes its title and acceptance criteria, surprising historical readers  | `bd note 1hq.60` lines preserve the original target (`postgres_integration_test.go`) and the reason for re-scope (file removed in unrelated work; current 4 `*_integration_test.go` files inherit the bead's intent).                       |

## Out of scope

- Migrating unit tests off `testing.T`. Unit tests use testify on top of
  `testing.T`; that is the project pattern.
- Changing the "MUST Ginkgo for integration tests" policy.
- Reviewing whether `internal/plugin/*_integration_test.go` (objects, help,
  communication, building) need any cleanup — already Ginkgo.
- Implementing any of the 4 deferred features (`holomush-ecbg`,
  `holomush-l4kx`, `holomush-6nds`, `holomush-nko7`).
- Broader `mockery` adoption.

## Open questions

None at design time. Section answered by user decisions captured in
`bd note holomush-rccc` history:

- Aggressive uniform conversion chosen over policy-relax or pragmatic-finish
  (2026-05-15).
- All 4 skeletons MUST be preserved with their backing-bead TODO refs intact
  (validation 2026-05-15).
- Scope expanded from `test/integration/` only (22 files) to repo-wide
  `*_integration_test.go` (40 files total) after re-grounding surfaced 18
  additional plain-`testing.T` integration tests outside the original tree
  (2026-05-15).
