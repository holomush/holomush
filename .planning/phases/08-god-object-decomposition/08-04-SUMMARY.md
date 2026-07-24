---
phase: 08-god-object-decomposition
plan: 04
subsystem: plugin
tags: [arch-02, decomposition, identity, lock-split, manager]
status: complete
requires:
  - "08-02 (internal/plugin rewired onto focuscontract)"
provides:
  - "internal/plugin.IdentityStore — the plugin identity registry as a self-locking, independently constructible unit"
  - "Manager.identity — the single field replacing four identity fields"
affects:
  - "internal/plugin/manager.go (1876 -> 1845 LoC; m.mu no longer guards any identity state)"
  - "internal/plugin/manager_unload.go (identity deactivation hoisted out of the m.mu critical section)"
tech-stack:
  added: []
  patterns:
    - "Self-locking name-keyed registry modelled on ServiceRegistry (same-package idiom)"
    - "Compile-time interface assertion (var _ IdentityRegistry = (*IdentityStore)(nil))"
    - "Option routing slots retained on the facade because ManagerOption is func(*Manager) (D-07)"
key-files:
  created:
    - internal/plugin/identity_store.go
    - internal/plugin/identity_store_test.go
  modified:
    - internal/plugin/manager.go
    - internal/plugin/manager_unload.go
    - internal/plugin/identity_registry_test.go
decisions:
  - "UnloadPlugin's identity deactivation was hoisted OUT of the m.mu critical section it shared with the runtime deletes. This is the one path the plan's lock-split safety argument did not cover, and it means the extraction genuinely widens an interleaving window on the unload path rather than preserving it exactly."
  - "pluginRepo and retentionDays stay on Manager as inert option routing slots, on the same reasoning the plan already applied to retentionDaysSet. ManagerOption is func(*Manager), so D-07 requires a field to write into."
  - "IdentityStore allocates its maps in the constructor, not in Bootstrap, so it is usable without bootstrapping — which is what makes the D-02 proof test possible."
metrics:
  duration: ~55m
  tasks: 3
  files: 5
  completed: 2026-07-19
---

# Phase 8 Plan 04: Plugin Identity Registry Extraction Summary

Extracted `nameByID` / `activeByName` / `pluginRepo` / `retentionDays` off `Manager` into an
`IdentityStore` that owns its own `sync.RWMutex`. `Manager.NameByID` and `IDByName` are one-line
forwards, the exported method set and all 11 `ManagerOption` signatures are byte-identical, and
`m.mu` no longer guards any identity state. This is D-06 — the coupling that would have made the
load/runtime split (08-06, 08-08) fail.

## What Shipped

| Task | Outcome | Commit |
| --- | --- | --- |
| 1 — Create `IdentityStore` (TDD) | RED (compile failure) → GREEN; 12 tests in `plugins_test` | `631ea15b6`, `c2426b7cd` |
| 2 — Delegate Manager's identity surface | 4 fields → 1; `UnloadPlugin` lock structure corrected | `622c9f7fc` |
| 3 — Wave-boundary integration gate | `task test:int` + `-race` + `task lint` green | `1995a9741` (lint fix) |

## READ THIS FIRST — the plan's lock-split safety argument was incomplete

The plan states, as a settled and explicitly not-to-be-relitigated finding, that the extraction
"preserves the already-existing interleaving window *exactly*" because `loadPlugin` never holds
`m.mu` across an identity/runtime boundary.

**That is true of `loadPlugin`. It is not true of `UnloadPlugin`.**

`manager_unload.go:24-32` (pre-plan) deleted `activeByName` inside the *same* `m.mu.Lock()`
critical section as the `pluginHosts` and `loaded` deletes:

```go
m.mu.Lock()
delete(m.activeByName, name)        // identity state
host, hostLoaded := m.pluginHosts[name]
if hostLoaded {
    delete(m.loaded, name)          // runtime state
    delete(m.pluginHosts, name)     // runtime state
}
m.mu.Unlock()
```

This is the same class of miss 08-03 recorded: the research method→field matrix models field
access, so it saw `activeByName` on this path but not that the access was *nested inside a
critical section that also covers runtime fields*. The call-graph read found it.

**Resolution.** `m.identity.Deactivate(name)` now runs *before* the `m.mu` section. Two
consequences, stated plainly:

- **T-8-06 (deadlock) is genuinely mitigated.** No path holds both locks, so no lock ordering
  exists to violate. Verified exhaustively below.
- **T-8-05 (TOCTOU) is mitigated on `loadPlugin` but the unload path's window is unavoidably
  widened.** Identity and runtime state now live under different locks, so their deletion can no
  longer be one atomic step. A concurrent reader can briefly observe `IDByName(name)` missing
  while `IsPluginLoaded(name)` is still true. **This is inherent to D-06** — no implementation
  that splits the lock can preserve cross-lock atomicity — but the plan's blanket "preserves the
  window exactly" claim should be read as scoped to `loadPlugin`.

Program order (identity first, then runtime) and the unconditional "cache cleanup FIRST" contract
are both preserved, so the change is minimal. The reasoning is recorded verbatim at the call site
so a future reader does not "tidy" it back inside the lock. I did not treat this as a
stop-and-report architectural decision because the resolution is forced and mechanical rather than
a design choice — but it is a correction to a plan premise, not a detail, hence the placement here.

## Lock discipline — exhaustively verified

All 9 production `m.identity.*` call sites, each checked against the enclosing `m.mu` state:

| Site | Location | `m.mu` held? |
| --- | --- | --- |
| `Bootstrap` | `manager.go:252` (NewManager) | no — pre-publication |
| `Sweep` | `manager.go:708` (LoadAll) | no — was per-row `m.mu` before |
| `HasRepo` / `Upsert` | `manager.go:1127`, `:1132` | no |
| `Register` | `manager.go:1152` | no |
| `Unregister` | `manager.go:1162` (rollback defer) | no |
| `NameByID` / `IDByName` | `manager.go:1809`, `:1814` | no — `m.mu` dropped entirely |
| `Deactivate` | `manager_unload.go:32` | no — hoisted (see above) |

In `loadPlugin` specifically, every identity call falls strictly between the `m.mu.Unlock()` at
`:1119` and the `m.mu.Lock()` at `:1337`. The runtime commit (`:1337-1344`) contains no identity
call. The four-critical-section structure is intact.

**Empirical confirmation:** the integration lane defers `-race` to nightly but supports opt-in, so
`RACE=-race task test:int` was run — **exit 0, 10741 tests, zero data races**. Unit `task test`
runs `-race` unconditionally and is also clean.

## D-02 / SC2: the bar was met

`internal/plugin/identity_store_test.go` is `package plugins_test` and constructs the store with
only a `store.PluginRepo` (or `nil`):

- `rg -c 'NewManager|integrationtest|WithVerbRegistry' identity_store_test.go` → **0 matches**
- No `*Manager` anywhere in the file; `ErrMissingVerbRegistry` (INV-EVENTBUS-11) never in play
- No harness, no `//go:build integration`
- `rg -n 'Manager' identity_store.go` excluding comments → **none** (no parent backpointer)

12 tests cover the six planned behaviors plus three error paths the plan did not enumerate
(sentinel-collision rejection, `ListAll` failure wrapping, `SweepInactive` failure wrapping) —
all pre-existing behaviors moved with the code, asserted via `errutil.AssertErrorCode`.

## Contract invariants — all verified

| Check | Result |
| --- | --- |
| Exported `Manager` method set pre vs post | **identical** (27 methods, diffed) |
| All 11 `ManagerOption` signatures | **identical** (diffed) |
| `nameByID` / `activeByName` on `Manager` | gone |
| Stale `guarded by the existing m.mu` comment | gone (rewritten to point at `IdentityStore`) |
| `var _ IdentityRegistry = (*IdentityStore)(nil)` | present |
| `git diff --stat origin/main...HEAD -- test/integration/` | **empty — zero churn (D-15)** |
| `wc -l internal/plugin/manager.go` | **1845** (from 1876; feeds Wave C calibration) |
| `task license:check` | clean (3779 files, 0 invalid) |

INV-PLUGIN-17 retention is pinned by two tests asserting *both* halves in a single test body
(`...ClearsActiveNameButRetainsHistoricalIDResolution`, `Sweep...RetainsHistoricalIDResolution`),
so a future edit cannot satisfy half of it. `Unregister` is the only operation that removes a
`nameByID` entry, and its doc comment states why that is correct (the identity was never
successfully published).

## Deviations from Plan

### 1. [Rule 3 — Blocking] `UnloadPlugin`'s identity deactivation hoisted out of `m.mu`

Fully described above. The plan said only "call the store's deactivate operation" at `:24`; doing
that literally would have nested the identity lock inside `m.mu`, violating the plan's own
prohibition.

### 2. [Rule 3 — Blocking] `identity_registry_test.go` could not pass unedited

The plan requires `git diff -- internal/plugin/identity_registry_test.go` to be empty. That is
unsatisfiable: the file is `package plugins` (in-package) and poked the removed fields directly at
4 sites:

```go
mgr.mu.Lock()
mgr.nameByID[id] = "core-scenes"
mgr.activeByName["core-scenes"] = id
mgr.mu.Unlock()
```

Each collapsed to `mgr.identity.Register(id, "core-scenes")`. Both sites are **arrange-phase
setup, not assertions** — the diff is 2 insertions / 8 deletions and every `assert`/`require` line
is untouched. This is the same class as 08-03's deviation 4, and the file sits outside
`test/integration/`, so D-15 does not govern it. The four `UnloadPlugin` tests pass unedited and
remain the behavior-preservation oracle.

### 3. `pluginRepo` and `retentionDays` remain on `Manager` as option routing slots

The plan's acceptance grep expects all four field names gone from `manager.go`. Two must stay, for
the reason the plan itself already accepted for `retentionDaysSet`: `ManagerOption` is
`func(*Manager)` and D-07 pins `WithPluginRepo` / `WithRetentionDays` byte-identical, so each
option needs a `Manager` field to write into between the option loop and the `NewIdentityStore`
call. The plan's own action text mandates this ordering ("Construct the store **after** the option
loop"), so the action and the grep are in tension; I followed the action.

**The criterion's substance holds:** the identity *state* (`nameByID`, `activeByName`) and the
lock coupling are gone. What remains is inert plumbing read exactly once, in `NewManager`, and
documented as such at the struct. I deliberately did not rename the fields to pass the grep —
that would game the check without changing anything real. Same shape as 08-03's finding that the
`CoreServerOption` routing slots could not be deleted.

### 4. [Rule 1 — Bug] govet shadow regression from the sweep rewrite

`task lint` failed (exit 201, 2 `govet` shadow issues) after Task 2. The sweep rewrite introduced
`swept, err := m.identity.Sweep(ctx)` in `LoadAll`, which made two **pre-existing** benign shadows
at `:672` and `:699` newly reportable — govet only reports a shadow when the outer variable is
used after the inner block, and the new `err` use created that condition.

Fixed by naming it `sweepErr`, leaving the two unrelated blocks alone. Committed separately
(`1995a9741`), and `task test:int` was re-run afterward because the fix touched `LoadAll`.

### 5. `rg -c 'sync.RWMutex'` reports 2, not 1

One is the `mu sync.RWMutex` field (line 41); the other is prose in the type doc comment (line 31)
explaining that the store carries its own lock. Exactly one mutex field exists. Same
comment-vs-code grep artifact 08-03 recorded.

## Call-graph derivation (the 08-03 lesson applied)

Per the prior-wave warning, the dependency set was derived from the call graph rather than the
research field matrix. A repo-wide `rg` for all four field names found exactly 11 production
sites, all inside `internal/plugin`, matching the plan's enumeration. **One method-level edge the
matrix could not see** was found — `UnloadPlugin`'s shared critical section (deviation 1). No
`IdentityStore` operation calls back into `Manager`; the type has no parent pointer and no
`Manager`-typed parameter, so no function-value injection (the 08-03 remedy) was needed here.

## Verification

| Gate | Exit |
| --- | --- |
| `task build` | 0 |
| `task test -- ./internal/plugin/` (with `-race`) | 0 (778 tests) |
| `task test -- ./internal/plugin/... ./internal/grpc/... ./cmd/holomush/...` | 0 (3116 tests) |
| `task test:int` (final tree) | 0 (**10741 tests**, 7 skipped) |
| `RACE=-race task test:int` | 0 (10741 tests, **zero data races**) |
| `task lint` | 0 |
| `task license:check` | 0 |
| `task fmt` | no residual diff |

Named downstream suites, all green on the final tree:

```
✓  test/integration/pluginparity   (496ms)   ← D-20 symmetry detector
✓  test/integration/plugincrypto   (7.237s)
✓  test/integration/wholesystem    (6.277s)  ← 9-plugin census intact
✓  test/integration/plugin         (19.551s)
```

The `wholesystem` census `expectedPlugins` list is unchanged and still enumerates the 9 expected
plugins (`core-aliases`, `core-building`, `core-channels`, `core-communication`, `core-help`,
`core-objects`, `core-scenes`, `echo-bot`, `test-abac-widget`).

`git diff --stat origin/main...HEAD -- test/integration/` → **empty**.

## Known Stubs

None. `IdentityStore` is fully wired; a nil `repo` is a supported, documented, test-pinned
configuration (the `WithPluginRepo` test seam), not a stub.

## Threat Flags

None. No new network endpoint, auth path, file-access pattern, or schema change. The crypto
manifest gates (`PluginRequestsDecryption` / `PluginCanReadBack`) were not touched by this plan —
they belong to 08-08's runtime half.

## Notes for Wave C

- `manager.go` is **1845 LoC** post-plan (from 1876). The load and runtime clusters (08-06, 08-08)
  still have to come out before the ratchet ceiling is set. The modest drop is expected: this plan
  removed ~45 lines of inline identity logic and added ~15 of delegation and documentation.
- `identity_store.go` is ~205 LoC and is a natural candidate for its own ceiling entry.
- **`m.hosts[TypeLua]` vs `m.luaHost` was NOT collapsed**, per the plan's explicit instruction.
  A follow-up GitHub issue should be filed at phase close (08-08 records the prohibition).
- The lock-split safety argument should be **restated in 08-06/08-08 as scoped to the paths
  actually audited**, not as a global property. This plan found one counterexample; the load and
  runtime splits should each do their own call-graph read rather than inheriting the claim.

## Self-Check: PASSED

Created files exist:
- `internal/plugin/identity_store.go` — FOUND
- `internal/plugin/identity_store_test.go` — FOUND

Commits exist on `gsd/phase-08-god-object-decomposition`:
- `631ea15b6` test(08-04): add failing tests for the IdentityStore seam — FOUND
- `c2426b7cd` feat(08-04): add IdentityStore owning its own lock — FOUND
- `622c9f7fc` refactor(08-04): delegate Manager's identity surface to IdentityStore — FOUND
- `1995a9741` fix(08-04): avoid shadowing LoadAll's err in the sweep call — FOUND
