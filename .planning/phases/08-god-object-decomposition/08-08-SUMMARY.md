---
phase: 08-god-object-decomposition
plan: 08
subsystem: plugin
tags: [arch-02, decomposition, loader, facade, teardown, lock-split, d-07]
status: complete
requires:
  - "08-04 (IdentityStore — the second unit lock)"
  - "08-06 (PluginRuntime — the third unit lock; the TestLoadPlugin re-keying handoff)"
provides:
  - "internal/plugin.PluginLoader — discovery, ordering, host wiring, load orchestration and both teardown paths as a self-locking, independently constructible unit"
  - "internal/plugin.LoaderConfig — the exported construction-time carrier that makes the loader buildable without a Manager"
  - "Manager as a four-field facade over {loader, runtime, identity} + option plumbing"
affects:
  - "internal/plugin/manager.go (1618 -> 702 LoC; holds no plugin state)"
  - "internal/plugin/manager_unload.go (now hosts both UnloadPlugin methods)"
  - "internal/plugin/goplugin/host.go (doc comment only — SetIdentityRegistry now receives the IdentityStore)"
  - "docs/architecture/invariants.yaml (INV-PLUGIN-16/-33 provenance retargeted to loader.go)"
tech-stack:
  added: []
  patterns:
    - "Exported config carrier (LoaderConfig) so a unit is constructible from plugins_test without the facade"
    - "Embedded-config option holder (managerConfig embeds LoaderConfig) — options keep func(*Manager) signatures while writing into one struct"
    - "Sibling units held as concrete pointers, never a parent backpointer (D-02)"
    - "Test seams for unexported unit methods on the production type in export_test.go (TestResolveLoadOrder / TestComputeHashes)"
key-files:
  created:
    - internal/plugin/loader.go
    - internal/plugin/loader_test.go
  modified:
    - internal/plugin/manager.go
    - internal/plugin/manager_unload.go
    - internal/plugin/export_test.go
    - internal/plugin/manager_internal_test.go
    - internal/plugin/identity_registry_test.go
    - internal/plugin/goplugin/host.go
    - docs/architecture/invariants.yaml
decisions:
  - "Close and UnloadPlugin assigned to the LOAD unit, per the plan's settled decision. Both invert load-unit operations and need policyInstaller/hosts/luaHost, which are load-only; putting them on the runtime unit would create two mutually-pointing orchestrators."
  - "RegisterHost now passes l.identity to SetIdentityRegistry instead of the *Manager. This is the ONLY non-comment argument change across all 17 moved bodies. Manager.NameByID/IDByName are one-line forwards into that same store, so hosts observe an identical value — and the loader avoids the backpointer D-02 forbids."
  - "Runtime and identity are held as concrete pointers, not narrow interfaces: the loader uses eleven runtime operations (two unexported, one passed as a method value) and six identity operations. All three types are same-package, so a pointer adds no import edge. The plan explicitly permitted this with a SUMMARY note."
  - "The loader's lock is Manager.mu moved, not a new lock. It guards exactly what m.mu still guarded after 08-04 and 08-06: hosts, luaHost, loadedOrder."
  - "Manager's fourth field is the managerConfig option holder rather than a bare retentionDaysSet — retentionDaysSet is itself option plumbing and belongs with the rest of it."
  - "Tasks 1 and 2 landed in one commit. Splitting them would have required editing loadPlugin's 300-line body twice, which is the exact double-edit churn the 08-06 handoff warns produces silent semantic change."
metrics:
  duration: ~95m
  tasks: 3
  files: 9
  completed: 2026-07-19
---

# Phase 8 Plan 08: Plugin Loader Extraction and Manager Facade Summary

Extracted the load-time half of `Manager` into a `PluginLoader` carrying the mutex `Manager.mu`
became after 08-04 and 08-06 took their state away, assigned both teardown paths to it, and reduced
`Manager` to a facade over `{loader, runtime, identity}` plus its option holder. **`manager.go` fell
1618 → 702 LoC.** All **27** exported `Manager` methods and all **11** `ManagerOption` signatures are
diffed identical against `origin/main`. **This closes ARCH-02.**

## READ THIS FIRST — the 08-06 handoff is discharged, and it found something

08-06 flagged that "fold two call sites into one" moves had twice introduced silent semantic changes
this phase, and told me to **diff the actual argument expressions, not just the resulting behavior**.

I did that mechanically for all 17 moved methods, normalizing only the receiver name and receiver
type. Result:

| Method | vs pre-plan `HEAD~1` |
| --- | --- |
| `Discover`, `warnUnknownTrustAllowlistEntries`, `seedAliases`, `BuildFocusRedirects`, `resolveLoadOrder`, `unregisterPluginProviders`, `computeHashes`, `discoverAndRegisterAttributes`, `ConfigureFocusDeps`, `ConfigureReadbackDecryptor`, `ConfigureSettingsDeps`, `LoadAll` | **ZERO DIFF** (byte-identical) |
| `ConfigureEventEmitter`, `loadPlugin`, `Close`, `UnloadPlugin` | comment-only (0 non-comment diff lines) |
| `RegisterHost` | **1 non-comment change — see below** |

**16 of 17 are byte-identical or comment-only. The single argument change is deliberate:**

```go
-		configurer.SetIdentityRegistry(RCV)          // the *Manager
+		configurer.SetIdentityRegistry(RCV.identity) // the *IdentityStore
```

This is exactly the class the handoff warned about, and it is worth stating why it is safe rather
than asserting that it is:

- `SetIdentityRegistry` takes a `plugins.IdentityRegistry` **interface**. Its sole implementation
  (`goplugin/host.go:599`) stores the value under `h.mu` and does no type assertion — grepped and
  read, not assumed.
- `*IdentityStore` satisfies that interface by an existing compile-time assertion
  (`identity_store.go:56`).
- `Manager.NameByID` / `IDByName` are **one-line forwards into `m.identity`**. So the two values are
  behaviorally indistinguishable through the interface.
- `m.identity` is assigned once, inside `NewManager`, which returns before any `RegisterHost` call
  can happen. There is no window where the forwarding target differs.

The alternative — keeping `RegisterHost` on `Manager` — would have forced the loader to hold a
`*Manager` backpointer, forfeiting D-02. The doc comment at `goplugin/host.go` claiming "the Manager
itself implements IdentityRegistry" was updated in the same change so it does not go stale.

Also verified per the handoff: `TestLoadPlugin`'s dedicated `testCommitNamed(name, ...)` seam — the
subject of 08-06's bug — is **untouched**. Only its field selectors were re-pointed
(`m.hosts` → `m.loader.hosts`); the `name` parameter still keys the maps.

## Lock discipline — 18/18 cross-unit call sites audited

Derived from the **call graph**, not 08-RESEARCH.md's field matrix, per the standing warning. Every
`l.runtime.*` / `l.identity.*` call site was checked against its enclosing lock state by a script
that tracks `l.mu` depth per function (including `defer`-form unlocks):

```
cross-unit call sites from the loader: 18
  no lock held   loader.go:184   RegisterHost                  runtime.CacheHostCapabilities
  no lock held   loader.go:185   RegisterHost                  runtime.EventEmitter
  no lock held   loader.go:222   ConfigureEventEmitter         runtime.lookupManifest
  no lock held   loader.go:223   ConfigureEventEmitter         runtime.SetEventEmitter
  no lock held   loader.go:601   discoverAndRegisterAttributes runtime.capabilitiesFor
  no lock held   loader.go:761   LoadAll                       identity.Sweep
  no lock held   loader.go:844   loadPlugin                    runtime.ClaimInflight
  no lock held   loader.go:847   loadPlugin                    runtime.ReleaseInflight
  no lock held   loader.go:854   loadPlugin                    identity.HasRepo
  no lock held   loader.go:859   loadPlugin                    identity.Upsert
  no lock held   loader.go:879   loadPlugin                    identity.Register
  no lock held   loader.go:889   loadPlugin                    identity.Unregister
  no lock held   loader.go:1025  loadPlugin                    runtime.capabilitiesFor
  no lock held   loader.go:1077  loadPlugin                    runtime.CommitLoaded
  no lock held   loader.go:1107  Close                         runtime.ListPlugins
  no lock held   loader.go:1128  Close                         runtime.Clear
  no lock held   unload.go:33    UnloadPlugin                  identity.Deactivate
  no lock held   unload.go:41    UnloadPlugin                  runtime.RemoveLoaded

VIOLATIONS (cross-unit call inside an l.mu critical section): 0
```

**No path holds two of the three unit locks, so no lock ordering exists to violate — T-8-06 is
mitigated by construction.** Combined with 08-04's identity audit and 08-06's 26-site runtime audit,
all three units are now covered.

### `Close` and `UnloadPlugin` — the lock re-read the brief asked for

Both were re-read independently rather than inheriting any prior claim.

**`UnloadPlugin`** — the three-step order is preserved exactly, and the plan's prohibition holds:

1. Cache cleanup **first and unconditionally**: `l.identity.Deactivate(name)` (identity lock), then
   `l.runtime.RemoveLoaded(name)` (runtime lock). Two locks, taken and released in sequence, never
   nested. Program order is identity-then-runtime, as 08-04 established.
2. `if !hostLoaded { return nil }` — the idempotent early return, still **after** cleanup.
3. `host.Unload` wrapped in `oops.Code("PLUGIN_UNLOAD_HOST").With("plugin", name)`, then policy
   removal.

The loader's own lock is **never taken** in `UnloadPlugin`. That is a change from `main` (where the
whole thing sat under one `m.mu`) but not from the pre-plan tree — 08-04 and 08-06 had already moved
both deletions out. T-8-25 (cleanup-before-early-return) is intact; the four existing `UnloadPlugin`
tests pass unedited and remain the oracle.

**`Close`** — program order preserved exactly: names read (runtime lock, hoisted above), policies
removed and hosts closed (loader lock), maps cleared (runtime lock, **between** two loader-lock
sections), legacy `luaHost` closed (loader lock). The runtime lock is never held inside the loader
lock or vice versa. The sorted-vs-map-order iteration noted by 08-06 as deviation 4 carries forward
unchanged; each name still gets exactly one `RemovePluginPolicies` call.

### `loadPlugin`'s critical sections

Still four disjoint acquisitions across three locks, none nested, none merged, none hoisted into
another: claim/release inflight (runtime), identity register/rollback (identity), commit (runtime),
and the `loadedOrder` append (loader). The `CommitLoaded`-returns-`existed` split 08-06 introduced is
carried over verbatim, including its honest T-8-05 note — I did **not** take up 08-06's suggestion to
move `loadedOrder` into the runtime unit and collapse the split. That is a behavior-adjacent
ownership change and this plan was scoped to relocation; it belongs to whoever revisits D-06.

## D-02 / SC2: the bar was met for the third unit

`loader_test.go` is `package plugins_test` and builds the loader with `plugins.NewPluginLoader`,
`plugins.NewPluginRuntime()` and `plugins.NewIdentityStore(nil, 0)` — no `*Manager`, no
`plugins.NewManager`, no harness, so `ErrMissingVerbRegistry` (INV-EVENTBUS-11) is never in play.

**The acceptance greps report non-zero because they match comments. This is now the fifth consecutive
plan where that criterion is wrong as stated**, so per the standing instruction I did not delete the
explanatory comments to make them go green:

| Check | Raw grep | Code-only | Verdict |
| --- | --- | --- | --- |
| `NewManager\|integrationtest` in `loader_test.go` | 2 | **0** | Both are in the helper's doc comment; one literally documents the absence |
| `\*Manager` in `loader.go` | 2 | **0** | Both in the type doc ("holds NO backpointer to it") — no field, no parameter |
| `func (l *PluginLoader)` in `loader.go` | 17 | — | The plan predicted 13; it is 17 because Tasks 1 and 2 landed together (13 cluster + `LoadAll` + `loadPlugin` + `Close` + `Registry`), plus `UnloadPlugin` in `manager_unload.go` = **18 production methods** |

9 tests cover the plan's five behaviors plus three it did not enumerate (absent directory, malformed
manifest with a surviving valid sibling, directory with no manifest).

## Two plan premises corrected

1. **"a malformed manifest surfaces the same error code it does today."** It does not. `Discover`'s
   contract is "invalid plugins are logged and skipped" — `ParseManifest` failure hits
   `slog.WarnContext(...); continue` and `Discover` returns `nil` error. The test asserts the real
   behavior (skipped, valid sibling still discovered) plus the one path that *does* error
   (unreadable directory) and the one that returns `nil, nil` (absent directory).

2. **Every `path:line` citation in the plan's `<read_first>` blocks is stale** by 50–200 lines
   (`Discover` cited at `:516`, actually `:461`; `LoadAll` at `:643`, actually `:588`; `Close` at
   `:1614`, actually `:1429`; `manager_unload.go` called "53-line", actually 61). 08-04 and 08-06 had
   already shifted the file. Symbols were located by name; no citation was trusted.

## Contract invariants — all verified

| Check | Result |
| --- | --- |
| Exported `Manager` method set pre vs post | **identical** (27 methods, diffed against `origin/main`) |
| All 11 `ManagerOption` signatures | **identical** (diffed against `origin/main`) |
| `NewManager` signature | unchanged (`func NewManager(pluginsDir string, opts ...ManagerOption) (*Manager, error)`) |
| `ErrMissingVerbRegistry` guard (INV-EVENTBUS-11) | at the tail of `NewManager`, fires identically; `MissingVerbRegistry` tests pass |
| `var _ manifestLookup = (*Manager)(nil)` | retained, compiles |
| `Manager` struct fields | 4: `loader`, `runtime`, `identity`, `cfg` |
| `git diff --stat -- internal/plugin/setup/ internal/plugin/event_emitter.go` | **empty** |
| `git diff -- internal/testsupport/integrationtest/plugins.go` | **empty**; `plugins.NewManager(` count **0** — INV-PLUGIN-18 intact |
| `git diff --stat origin/main...HEAD -- test/integration/` | **empty — zero churn (D-15)** |
| `git diff --stat origin/main...HEAD -- internal/access/` | **empty** |

## Task Breakdown

| Task | Outcome | Commit |
| --- | --- | --- |
| RED gate (Task 1, `tdd="true"`) | 3 `undefined: plugins.PluginLoader / NewPluginLoader / LoaderConfig` | `3eecbe44c` |
| 1+2 — Extract `PluginLoader`, reduce `Manager` to a facade | 17 methods + 14 fields relocated; 16/17 bodies byte-identical or comment-only | `bc8cb81a5` |
| 3 — Provenance retarget (integration gate finding) | INV-PLUGIN-16/-33 refs + scope paths → `loader.go` | `29cb31464` |
| 3 — Deferred-item log | pre-existing outbox race recorded | `99eed22d5` |

**Tasks 1 and 2 landed in one commit**, following 08-05 and 08-06's precedent but for a different
reason. A split *was* mechanically achievable here (Task 1's `Manager` could have reached through
`m.loader.<field>` for one commit). I rejected it because it would have required editing
`loadPlugin`'s ~300-line body **twice** — once to re-point selectors at `m.loader.*`, once to move
it. That is precisely the repeated-mechanical-edit churn the 08-06 handoff identifies as the vector
for silent semantic change. One careful move, mechanically verified byte-identical, is safer than two.

## Deviations from Plan

### 1. [Rule 3 — Blocking] `Manager`'s fourth field is the option holder, not `retentionDaysSet`

The plan's acceptance criterion expects `loader`, `runtime`, `identity`, `retentionDaysSet` "and
nothing else". `ManagerOption` is `func(*Manager)` and D-07 freezes all eleven signatures, so every
option needs a `Manager` field to write into between the option loop and unit construction. Thirteen
loose routing fields would defeat the point of the facade.

Resolved with one `cfg managerConfig` field embedding the exported `LoaderConfig` plus
`pluginRepo` / `retentionDays` / `retentionDaysSet`. `retentionDaysSet` is itself option plumbing, so
it belongs *inside* the holder rather than beside it. **Manager therefore has exactly four fields**,
and the criterion's substance — no plugin state on the facade — holds strictly. This is the same
tension 08-04 recorded as its deviation 3, resolved more completely now that all thirteen fields
could move at once.

### 2. [Rule 3 — Blocking] Three in-package test files could not pass unedited

The plan requires `git diff -- internal/plugin/manager_test.go internal/plugin/manager_audit_test.go
internal/plugin/identity_registry_test.go crypto_manifest_lookup_test.go` to show no assertion
change. `manager_test.go`, `manager_audit_test.go` and `crypto_manifest_lookup_test.go` are indeed
**untouched**. Three other in-package files could not be:

| File | Change | Assertions touched |
| --- | --- | --- |
| `export_test.go` | `m.mu`/`m.hosts`/`m.luaHost` → `m.loader.*`; two new test seams appended | none (no assertions in the file) |
| `manager_internal_test.go` | `&Manager{registry:…, capVocab:…}` → `&PluginLoader{…}` at 3 sites (`resolveLoadOrder` moved) | **none** — arrange-phase only |
| `identity_registry_test.go` | `mgr.computeHashes(dp)` → `mgr.loader.computeHashes(dp)` at 8 sites | **none** — receiver expression only |

All three are `package plugins` and outside `test/integration/`, so D-15 does not govern. Same class
as 08-04's deviation 2 and 08-06's deviation 2.

### 3. [Rule 1 — Bug] Invariant-provenance regression (predicted, seventh occurrence)

`task test:int` failed `TestProvenanceGuard` — and only that — after the extraction:

```
INV-PLUGIN-16: canonical token absent at recorded site internal/plugin/manager.go
INV-PLUGIN-33: canonical token absent at recorded site internal/plugin/manager.go
```

The `LoadAll` sweep-ordering comment (INV-PLUGIN-16 / legacy `INV-W9ML-8`) and the `loadPlugin`
emit-type scope comment (INV-PLUGIN-33 / legacy `INV-M1`) travelled to `loader.go` with their
methods. Retargeting the two `refs` produced a **second** failure —
`ref internal/plugin/loader.go not in INV-PLUGIN owned_paths/shared_files` — exactly the two-step
shape the standing warning describes. `loader.go` was added to **both** `INV-PLUGIN.shared_files` and
`INV-EVENTBUS.shared_files`, mirroring `manager.go` whose load-time half it is (it carries PLUGIN
tokens plus the `INV-EVENTBUS-11` cross-reference). Regenerated with `go run ./cmd/inv-render`.
Committed separately (`29cb31464`).

`task test` stayed green throughout, as predicted.

### 4. [Out of scope] Pre-existing data race in the outbox relay

`RACE=-race task test:int` fails `cmd/holomush TestAdminAuthenticateE2E` on a data race in
`internal/world/setup.(*outboxWaker).Close` — it releases a pooled pgx conn while the relay goroutine
is still parked in `WaitForNotification` on that same conn.

I did **not** accept "unrelated, move on" on inspection alone, because 08-06 recorded
`RACE=-race task test:int` as exit 0 and that discrepancy needed resolving. Checked out the pre-plan
baseline (`3fff6576b`, 08-07's final tree) in a throwaway worktree and ran the same gate:
**identical failure, identical race, 1/1**. Combined with `git diff --stat origin/main...HEAD --
internal/world/ internal/lifecycle/ internal/admin/ internal/eventbus/audit/` being **empty** across
the whole phase branch, this is conclusively pre-existing. Most plausibly introduced by the Phase 7
Orchestrator lifecycle unification (`cce89c702`).

Not fixed (scope boundary). Logged to `deferred-items.md` and filed as **#4828** with the full stack
and reproduction. The throwaway worktree was removed.

## Threat Model Outcomes

| Threat | Disposition | Outcome |
| --- | --- | --- |
| T-8-25 — `UnloadPlugin` cleanup-before-early-return (Tampering, **high**) | mitigated | Order preserved exactly; body comment-only diff. Re-read independently and documented above. The four existing `UnloadPlugin` tests pass unedited. |
| T-8-06 — lock-ordering deadlock across three locks (DoS, **high**) | mitigated | 18/18 cross-unit call sites hold no loader lock (mechanical audit above). No speculative loader lock added — the one present is `Manager.mu` moved with its remaining state. |
| T-8-05 — TOCTOU between identity and runtime during load (Tampering, medium) | mitigated, residual unchanged | The extraction relocates; it does not re-split. The two widenings 08-06 recorded (`loadPlugin` §5, `RegisterHost`) carry over verbatim and are documented at their call sites. No new widening. |
| T-8-26 — `trustAllowlist` handling (EoP, medium) | mitigated | `warnUnknownTrustAllowlistEntries` is **byte-identical**. `test/integration/plugin/` and the whole-system census green. |
| T-8-16 — `NewManager` verb-registry guard (EoP, medium) | mitigated | Still at the tail of `NewManager`, unchanged; `MissingVerbRegistry` tests pass. Every in-repo constructor exercises it. |
| T-8-27 — harness bypassing `pluginsetup.NewPluginSubsystem` (Tampering, medium) | mitigated | `integrationtest/plugins.go` diff **empty**; `plugins.NewManager(` count **0**. INV-PLUGIN-18 intact. |
| T-8-04 — Lua vs binary parity across the finished split (EoP/Tampering, **high**) | mitigated | `event_emitter.go` diff **empty** across the whole phase. `ConfigureEventEmitter` builds one `lookupManifest` func value shared by both runtimes; `RegisterHost` and all four `Configure*` push dependencies into `hosts` **and** `luaHost` identically, bodies unchanged. `test/integration/pluginparity` (7 specs) green. |

## ARCH-02 closeout record

**The three units, each with its `package plugins_test` D-02 proof test:**

| Unit | File | LoC | Proof test (constructs with only its own collaborators) |
| --- | --- | --- | --- |
| `IdentityStore` | `internal/plugin/identity_store.go` | 212 | `internal/plugin/identity_store_test.go` (08-04) |
| `PluginRuntime` | `internal/plugin/runtime.go` | 593 | `internal/plugin/runtime_test.go` (08-06) |
| `PluginLoader` | `internal/plugin/loader.go` | 1142 | `internal/plugin/loader_test.go` (this plan) |

**`manager.go` LoC across ARCH-02:**

| Point | LoC |
| --- | --- |
| pre-phase (`origin/main`) | **1876** |
| after 08-04 (IdentityStore) | 1845 |
| after 08-06 (PluginRuntime) | 1616 |
| **post-08-08 (final)** | **702** |

`manager_unload.go` is 72 LoC (both `UnloadPlugin` methods).

**Method-set equality:** confirmed identical, 27 exported `Manager` methods and 11 `ManagerOption`
signatures, diffed against `origin/main`.

**Zero integration-test churn:** `git diff --stat origin/main...HEAD -- test/integration/` produces
no output across the entire phase branch (D-15).

## Note for Wave C — ratchet calibration

**`manager.go` finishes at 702 LoC.** Two cautions before pinning a ceiling to that number, following
08-07's finding that `server.go` *grew* 642 → 657 from delegation stubs:

- **~430 of the 702 lines are facade boilerplate and package-level helpers that will legitimately
  grow.** Roughly 190 lines are the 11 `ManagerOption` declarations (frozen by D-07, each ~8 lines
  with its doc comment), ~120 lines are one-line forwarding methods with their doc comments, and
  ~120 lines are package-level functions that never belonged to any unit (`findOptional`,
  `CollectResourceTypes`, `CollectActions`, `CollectFocusRedirects`, `prioritySort`,
  `defaultResolvePolicy`, `displayTargetFromString`, `manifestDeclaredEmitTypes`, `isValidStreamName`).
  **Every new exported `Manager` method adds ~6 lines of pure delegation, and every new option adds
  ~8.** A ceiling pinned at 702 would fail on the third added method through no fault of the author.
- **`loader.go` at 1142 LoC is the largest of the three units** and is itself a plausible future
  split candidate (`loadPlugin` alone is ~300 lines with six rollback branches). If Wave C sets a
  per-file ceiling, `loader.go` needs a higher one than `runtime.go`/`identity_store.go`, or an
  explicit note that it is next in line.

Suggested shape: pin the ceiling with headroom (e.g. 800 for `manager.go`) and treat the ratchet as
a regression guard against *state* creeping back onto the facade rather than a line-count target.
A structural check — "`Manager` has exactly four fields, none of them plugin state" — would catch
the actual regression that matters and is immune to delegation growth.

## Verification

| Gate | Exit |
| --- | --- |
| `task build` | 0 |
| `task test -- ./internal/plugin/...` | 0 (1983 tests, `-race`) |
| `task test -- ./internal/plugin/... ./internal/eventbus/... ./internal/grpc/... ./cmd/holomush/... ./internal/testsupport/...` | 0 (**4035 tests**) |
| **`task test:int`** (D-17 mandatory, final tree) | **0 — 10777 tests, 7 skipped** |
| `RACE=-race task test:int` | 201 — **one pre-existing race outside this phase's blast radius**; reproduced 1/1 on the pre-plan baseline. See deviation 4 / #4828. |
| `task lint` | 0 |
| `task license:check` | 0 |
| `task fmt` | no residual diff |

Named suites, all green on the final tree:

```
✓  test/integration/pluginparity   (597ms)    ← D-20 symmetry detector (T-8-04)
✓  test/integration/plugincrypto   (7.433s)   ← crypto manifest gates
✓  test/integration/plugin         (19.122s)  ← 15 files, unedited
✓  test/integration/wholesystem    (6.754s)   ← 9-plugin census intact
```

The `wholesystem` census still enumerates the 9 expected plugins (`core-aliases`, `core-building`,
`core-channels`, `core-communication`, `core-help`, `core-objects`, `core-scenes`, `echo-bot`,
`test-abac-widget`) — all still loading through `LoadAll`, which now lives on the loader.

**`-race` note:** the integration lane supports opt-in `-race` and it was run. It surfaces exactly one
race, characterized above as pre-existing by direct baseline comparison. Every plugin suite is green
under `-race` with zero races.

## Follow-up issues filed

| # | Title | Why |
| --- | --- | --- |
| **#4826** | plugin: collapse the `m.hosts[TypeLua]` / `m.luaHost` duplication | Deliberately not collapsed — behavior-adjacent, prohibited by the plan. Originally an item on #4674. |
| **#4827** | docs: two stale claims in `plan-review-learnings.md` about the plugin Manager | Claims `Manager.UnloadPlugin` does not exist (it does) and labels the verb-registry guard INV-GW-10 (actual: INV-EVENTBUS-11). |
| **#4828** | data race: `outboxWaker.Close` releases a pooled pgx conn while `Wait` blocks | Out-of-scope discovery, proven pre-existing against the baseline tree. |

## Pre-Push Review Gates (D-19)

- **`crypto-reviewer` — REQUIRED for the phase PR.** This plan does not itself relocate a crypto
  gate (08-06 did that, and its byte-identity proof stands), but it moves `ConfigureEventEmitter`,
  which constructs the emitter with the `lookupManifest` func value behind every gate
  `event_emitter.go::Emit` enforces. The body is comment-only-diff and `event_emitter.go` is provably
  untouched, but the adjacency is an explicit trigger. Do not skip it.
- **`abac-reviewer` — NOT required.** Verified rather than assumed:
  `git diff --stat origin/main...HEAD -- internal/access/` produces **no output** across the entire
  phase branch.

## Known Stubs

None. `PluginLoader` is fully wired; every relocated method has a live production caller, either
directly (`loadPlugin`, `seedAliases`, `resolveLoadOrder`, `computeHashes`,
`warnUnknownTrustAllowlistEntries`, `unregisterPluginProviders`, `discoverAndRegisterAttributes`) or
through a `Manager` forwarder (the ten exported ones).

## Threat Flags

None. No new network endpoint, deserialization site, auth path, file-access pattern, or schema
change. The one argument-expression change (`SetIdentityRegistry`) narrows what a host receives from
the whole `*Manager` to just the identity registry it actually uses — a reduction in surface, not an
expansion, and behaviorally identical through the interface.

## Self-Check: PASSED

Created files exist:
- `internal/plugin/loader.go` — FOUND
- `internal/plugin/loader_test.go` — FOUND

Commits exist on `gsd/phase-08-god-object-decomposition`:
- `3eecbe44c` test(08-08): add failing tests for the PluginLoader seam — FOUND
- `bc8cb81a5` refactor(08-08): extract PluginLoader and reduce Manager to a facade — FOUND
- `29cb31464` fix(08-08): retarget INV-PLUGIN-16/-33 provenance to loader.go — FOUND
- `99eed22d5` docs(08-08): log the pre-existing outbox-relay data race as deferred — FOUND
