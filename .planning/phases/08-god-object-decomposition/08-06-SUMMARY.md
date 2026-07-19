---
phase: 08-god-object-decomposition
plan: 06
subsystem: plugin
tags: [arch-02, decomposition, runtime-delivery, crypto-gates, lock-split, d-20]
status: complete
requires:
  - "08-02 (crypto-gate nil guards; export_test.go seam home)"
  - "08-04 (IdentityStore; the second of three unit locks)"
provides:
  - "internal/plugin.PluginRuntime — the loaded-plugin registry + delivery/read-side cluster as a self-locking, independently constructible unit"
  - "Manager.runtime — the single field replacing loaded/inflight/pluginHosts/hostCaps/eventEmitter"
  - "Relocated crypto manifest gates with their fail-closed nil guards intact at BOTH receivers"
affects:
  - "internal/plugin/manager.go (1845 -> 1616 LoC; m.mu no longer guards any runtime-delivery state)"
  - "internal/plugin/manager_unload.go (runtime deletes now take neither m.mu nor the identity lock)"
tech-stack:
  added: []
  patterns:
    - "Self-locking name-keyed registry modelled on ServiceRegistry / IdentityStore (same-package idiom)"
    - "Return-a-flag instead of nesting locks (CommitLoaded reports `existed` so the loadedOrder append happens under the other lock)"
    - "Paired lock-taking / caller-holds-lock accessors (capabilitiesFor vs capabilitiesForLocked)"
    - "Dedicated test-only seam when a production operation's key semantics differ from the test helper's"
key-files:
  created:
    - internal/plugin/runtime.go
    - internal/plugin/runtime_test.go
  modified:
    - internal/plugin/manager.go
    - internal/plugin/manager_unload.go
    - internal/plugin/export_test.go
    - internal/plugin/identity_registry_test.go
decisions:
  - "FOUR m.mu critical sections spanned both clusters and had to have their runtime call hoisted out: RegisterHost, ConfigureEventEmitter, loadPlugin's commit section, and Close. None was visible in the research field matrix. This is the third consecutive plan to find method-level edges the matrix cannot model."
  - "loadPlugin's commit section coupled a READ of `loaded` (moving) to a conditional WRITE of `loadedOrder` (staying). CommitLoaded returns `existed` so the append happens under m.mu afterwards — the two writes are no longer atomic, which is inherent to D-06."
  - "TestLoadPlugin keys its maps off its `name` PARAMETER, not manifest.Name. Routing it through CommitLoaded silently re-keyed them. Caught by revive's unused-parameter, not by any test — every current caller passes a matching pair."
  - "The nil guard is on BOTH receivers and on lookupManifest. Manager's forwarder guard is the one AuthGuard needs (it holds *Manager); PluginRuntime's guard restores &Manager{} fixture behavior, whose runtime field is now nil where the loaded map was merely empty."
  - "Two loadPlugin sites called capabilitiesFor holding NO lock pre-split, reading hostCaps unsynchronized. They now go through an RLock-taking accessor — a latent race closed, no observable change."
metrics:
  duration: ~85m
  tasks: 3
  files: 6
  completed: 2026-07-19
---

# Phase 8 Plan 06: Plugin Runtime-Delivery Extraction Summary

Extracted `loaded` / `inflight` / `pluginHosts` / `hostCaps` (plus `eventEmitter`) off `Manager`
into a `PluginRuntime` owning its own `sync.RWMutex`, together with the 15-method
delivery-and-read-side cluster D-05 names. `Manager` keeps all **27** exported methods and all
**11** `ManagerOption` signatures byte-identical and forwards. `manager.go` fell **1845 → 1616**
LoC. This is the second of ARCH-02's three units; 08-08 takes the load-time remainder.

## READ THIS FIRST — the crypto-gate handoff from 08-02 is discharged

08-02 flagged that if ARCH-02 relocated `PluginRequestsDecryption` / `PluginCanReadBack`, their
`if m == nil` prologues MUST travel with them or threat **T-8-03 silently returns as a panic on the
decrypt path**. Discharged, and verified three ways:

**1. Both gate bodies are byte-identical after normalizing only the receiver name and type.**
Mechanically diffed `git show HEAD:internal/plugin/manager.go` against `runtime.go`:

```
==================== PluginRequestsDecryption ====================
>>> ZERO DIFF after normalizing receiver name+type — byte-identical
    lines: 17 | nil-guard present: True
    emitEntryMatchesWireType preserved: True
==================== PluginCanReadBack ====================
>>> ZERO DIFF after normalizing receiver name+type — byte-identical
    lines: 15 | nil-guard present: True
    emitEntryMatchesWireType preserved: True
```

No re-ordering, no "simplification", no change to the `manifest == nil || manifest.Crypto == nil`
guards or the `emitEntryMatchesWireType` call.

> Method note for a re-verifier: an earlier attempt used `sed 's/\bm\b/RCV/g'`, which **silently
> no-ops on macOS** — BSD sed does not support `\b`. It produced a diff that looked like a real
> receiver mismatch. The Python `re` version above is the authoritative check. Do not trust a
> `\b`-based sed normalization on this repo's default shell.

**2. The guard exists at BOTH receivers, and on `lookupManifest`.** This is not redundancy — the
two receivers guard against different faults:

| Receiver | Guard | What it catches |
| --- | --- | --- |
| `*Manager` (forwarder) | `if m == nil` | The 08-02 case verbatim: a typed-nil `*Manager` in an `authguard.ManifestLookup`. **This is the receiver AuthGuard actually holds**, so removing it would reintroduce T-8-03 exactly. |
| `*PluginRuntime` (relocated) | `if r == nil` | A `&Manager{}` fixture whose `runtime` field is nil. Pre-split that fixture read a nil `loaded` map and returned false; post-split it would panic inside `r.mu.RLock()`. The guard **restores exact pre-split behavior**, it is not new defensive code. |
| `PluginRuntime.lookupManifest` | `if r == nil` | Same fault via the emitter's `lookupManifest` method value, which `ConfigureEventEmitter` hands to `NewPluginEventEmitter`. |

Verified live at all five sites (`runtime.go:441,474,499`; `manager.go:1544,1558`).

**3. Behavior 5 in `runtime_test.go` pins it at the new receiver** —
`TestPluginRuntimeCryptoGatesReturnFalseOnNilReceiver`, `require.NotPanics` + `assert.False` on
both gates. The 08-02 tests on `*Manager` are untouched and still pass.

`var _ manifestLookup = (*Manager)(nil)` (`manager.go:1527`) still targets `*Manager` and still
compiles, so the signature-drift tripwire 08-02 installed is intact.

## FOUR lock-nesting hazards — the field matrix saw none of them

Per the standing warning, the dependency set was derived from the **call graph**, not
08-RESEARCH.md's method→field matrix. The matrix correctly listed which methods touch the four
fields. It could not see that **four `m.mu` critical sections span both clusters** — each would
have nested the runtime lock inside `m.mu`, violating the prohibition 08-04 established. This is
the third consecutive plan to hit this class (08-03 helpers, 08-04 `UnloadPlugin`, now four sites).

| Site | The coupling the matrix missed | Resolution |
| --- | --- | --- |
| `RegisterHost` | Writes `hosts` (stays) **and** `hostCaps` (moves) **and** reads `eventEmitter` (moves) in one `m.mu.Lock()`. | `CacheHostCapabilities` + `EventEmitter()` hoisted above `m.mu`. Program order preserved. Wiring-phase method ("must be called before LoadAll"), and `capabilitiesFor` already carries a discover-on-demand fallback for a missing entry. |
| `ConfigureEventEmitter` | Constructs the emitter **from `m.lookupManifest`** and stores it, while iterating `hosts`/`luaHost`, all under `m.mu.Lock()`. | Emitter construction + `SetEventEmitter` hoisted above `m.mu`; host iteration stays inside. |
| `loadPlugin` commit (§5) | A **read of `loaded`** (moving) guards a **conditional append to `loadedOrder`** (staying), inside one `m.mu.Lock()`. | `CommitLoaded` **returns `existed`**; the append runs under `m.mu` afterwards. See the honest cost below. |
| `Close` | Reads `loaded`, clears `loaded`/`pluginHosts`, **and** iterates `hosts`, calls `policyInstaller` and closes `luaHost` — all inside a single `m.mu.Lock()` held across I/O. | Name read hoisted above; `Clear()` placed between two `m.mu` sections. Program order preserved exactly: policies → hosts closed → maps cleared → luaHost closed. |

### The honest cost (T-8-05), stated plainly

The plan's threat register says this extraction "preserves the existing window rather than widening
it." **That is true of the delivery and read-side methods — which move wholesale, lock and all —
and false of two sites.** Per 08-04's instruction, I did not inherit the claim:

- **`loadPlugin` §5:** `loaded` and `loadedOrder` are no longer written atomically. A concurrent
  reader can briefly observe a plugin in `loaded` before it appears in `loadedOrder`. Both
  `loadedOrder` readers (`seedAliases`, `BuildFocusRedirects`) run at load time on the same
  goroutine, so this is not reachable today — but it is a real widening, not a no-op.
- **`RegisterHost`:** `hosts` and `hostCaps` are no longer written atomically.

Both are **inherent to D-06** — no lock split can preserve cross-lock atomicity — and both sit on
startup/wiring paths. This is the same shape 08-04 recorded for the unload path.

**Conversely, two windows genuinely NARROWED.** `BeginServiceDispatch` reads `pluginHosts` **and**
`hostCaps` in one critical section; because both fields moved to the *same* new lock, that
atomicity is preserved exactly rather than split. And `loadPlugin`'s two `capabilitiesFor` calls
previously read `m.hostCaps` while holding **no lock at all** — an unsynchronized read that now
goes through an `RLock`.

### Lock discipline — exhaustively verified

All **26** production `m.runtime.*` call sites were scanned against their enclosing `m.mu` state;
**every one holds no `m.mu`**. Combined with 08-04's identity audit, no path holds two of the three
unit locks at once, so no lock ordering exists to violate (**T-8-06 mitigated by construction**).

Empirical confirmation: **`RACE=-race task test:int` → exit 0, 10760 tests, ZERO data races.**

## D-20 plugin-runtime symmetry — preserved structurally

```
$ git diff --stat origin/main...HEAD -- internal/plugin/event_emitter.go
(no output)
```

`event_emitter.go::Emit` is the single chokepoint where the Lua return-value path and the binary
gRPC `EmitEvent` path both hit `actor_kinds_claimable`, `emits` and `crypto.emits`. This plan moves
the *receiver* of the `EmitPluginEvent` wrapper and the `lookupManifest` **func value** the emitter
is built with — one value shared by both runtimes. There is no per-runtime branch anywhere in
`runtime.go`. Since the gate file is provably untouched, there is no mechanism by which one runtime
could land on a different gate.

Detector: **`test/integration/pluginparity` (7 specs) green.**

## D-02 / SC2: the bar was met

`runtime_test.go` is `package plugins_test` and builds the runtime with `plugins.NewPluginRuntime()`
and a local stub host — no `*Manager`, no `NewManager`, no harness, so `ErrMissingVerbRegistry`
(INV-EVENTBUS-11) is never in play.

The plan's greps report non-zero because they match **comments**. Code-only verification (comments
stripped) for both new files:

| Check | Raw grep | Code-only | Verdict |
| --- | --- | --- | --- |
| `NewManager\|integrationtest\|WithVerbRegistry` in `runtime_test.go` | 2 | **0** | Both matches are comments — one literally documents the absence ("no `plugins.NewManager` call"). |
| `\*Manager` in `runtime.go` | 4 | **0** | All four are in the crypto-gate provenance doc comment. No backpointer field or parameter. |
| `sync.RWMutex` in `runtime.go` | 2 | **1** | One field, one prose mention. |

Per the standing instruction I did **not** delete the explanatory comments to make the greps go
green — they carry the reason the guards exist, which is the most safety-critical context in the
file. Same grep artifact 08-03 and 08-04 recorded (08-04 deviation 5).

11 tests cover the six planned behaviors plus two the plan did not enumerate (unknown-plugin
lookup; positive routing to the owning host).

## Task Breakdown

| Task | Outcome | Commit |
| --- | --- | --- |
| RED gate (Task 1, `tdd="true"`) | 8 `undefined: plugins.PluginRuntime / NewPluginRuntime` errors | `a6ffaf60a` |
| 1+2 — Extract `PluginRuntime`, delegate `Manager`'s runtime surface | 15 methods + 5 fields relocated; gates verified byte-identical | `bd2b6d9cd` |
| 3 — Wave-boundary integration gate | `task test:int` exit 0, `-race` exit 0, `task lint` exit 0 | (verification only) |

**Tasks 1 and 2 landed in one commit**, following 08-05's precedent. A clean split is not
achievable: `runtime_test.go` needs the `TestLookupManifest` seam in `export_test.go`, and that
file's `TestLoadPlugin` body cannot compile against both the old (`m.loaded`) and new
(`m.runtime`) `Manager` shape. Any intermediate would be contrived and non-bisectable; a
fabricated commit boundary is worse than an honest single one.

## Contract invariants — all verified

| Check | Result |
| --- | --- |
| Exported `Manager` method set pre vs post | **identical** (27 methods, diffed) |
| All 11 `ManagerOption` signatures | **identical** (diffed) |
| `loaded`/`inflight`/`pluginHosts`/`hostCaps` on `Manager` | gone (`rg` → no match) |
| `var _ manifestLookup = (*Manager)(nil)` | present at `manager.go:1527`, compiles |
| `git diff --stat -- internal/plugin/event_emitter.go` | **empty** |
| `git diff --stat -- internal/plugin/setup/` | **empty** |
| `git diff --stat origin/main...HEAD -- test/integration/` | **empty — zero churn (D-15)** |
| `wc -l internal/plugin/manager.go` | **1616** (from 1845) |
| `wc -l internal/plugin/runtime.go` | 601 |
| `task license:check` | clean (3787 files, 0 invalid) |

## Deviations from Plan

### 1. [Rule 1 — Bug] `TestLoadPlugin` silently re-keyed its maps

**Found during:** `task lint` after Task 2 (revive `unused-parameter` on `name`).

`TestLoadPlugin(name string, manifest *Manifest)` keys `loaded`/`pluginHosts` off its **`name`
parameter**. I initially routed it through `CommitLoaded`, which keys off `dp.Manifest.Name`. Where
the two differ, the entry lands under a different key.

**No test caught this** — all six call sites happen to pass a matching pair, so the suite passed by
luck rather than correctness. `revive` caught it only because the parameter became unused, which is
the *symptom*, not the defect. Left in place it would have silently mis-keyed the next caller that
passed a divergent name.

**Fix:** a dedicated `testCommitNamed(name, dp, host)` seam in `export_test.go` that writes under
the explicit key, restoring byte-faithful semantics. `CommitLoaded` was then simplified back to
loadPlugin's unconditional write (its `if host != nil` branch existed only for this caller).

Worth flagging for 08-08: this is the second time a "fold two call sites into one operation" move
introduced a silent semantic change. Prefer a separate seam over a conditional inside a shared one.

### 2. [Rule 3 — Blocking] `identity_registry_test.go` could not pass unedited

The plan requires `git diff -- internal/plugin/identity_registry_test.go` to be empty. Unsatisfiable
for the same reason 08-04 recorded: the file is `package plugins` and poked the removed fields
directly at 6 sites.

Diff is **4 insertions / 12 deletions**, and **every `assert` / `require` / `errutil.AssertErrorCode`
line is untouched** — only arrange-phase setup and two read-back extractions changed:

```go
-	mgr.mu.Lock()
-	mgr.pluginHosts["broken"] = host
-	mgr.loaded["broken"] = &DiscoveredPlugin{Manifest: &Manifest{Name: "broken"}}
-	mgr.mu.Unlock()
+	mgr.runtime.CommitLoaded(&DiscoveredPlugin{Manifest: &Manifest{Name: "broken"}}, host)
```

The file is outside `test/integration/`, so D-15 does not govern it. The four `UnloadPlugin` tests
still pass unedited and remain the behavior-preservation oracle. A `TestHasHost` accessor was added
to `export_test.go` because `pluginHosts` has no production read-only accessor.

### 3. [Rule 1 — Bug] `CapabilitiesFor` exported an unexported return type

revive `unexported-return`. Resolved by unexporting it (`capabilitiesFor`, lock-taking) and
renaming the caller-holds-lock variant to `capabilitiesForLocked`, per Go's `...Locked` convention.
Both are same-package, so `manager.go` calls the unexported form directly.

### 4. `Close` iterates plugin names in sorted rather than map order

`Close` now reads names via `ListPlugins()`, which sorts. Each name still gets exactly one
`RemovePluginPolicies` call; this only makes shutdown logging deterministic. Noted because it is a
visible (if benign) change to a body the plan asked to move verbatim.

### 5. Three acceptance greps report comment matches

Documented in the D-02 table above. The criteria are stated as raw `rg` counts; their **substance**
holds under code-only verification. Not gamed by deleting comments.

## Threat Model Outcomes

| Threat | Disposition | Outcome |
| --- | --- | --- |
| T-8-03 — crypto-gate relocation (Info Disclosure, **high**) | mitigated | Both bodies byte-identical (mechanical diff, zero lines). Nil guards present at both receivers **and** `lookupManifest`. Behaviors 3-5 pin them at the new receiver. `test/integration/plugincrypto` green. `crypto-reviewer` flagged as required below. |
| T-8-04 — Lua vs binary reaching the gates (EoP/Tampering, **high**) | mitigated | Structural: `event_emitter.go` provably untouched (`git diff --stat` empty). One shared `lookupManifest` func value, no per-runtime branch in `runtime.go`. `test/integration/pluginparity` (7 specs) green. |
| T-8-05 — TOCTOU across unit locks (Tampering, medium) | **mitigated with a stated residual** | Delivery/read-side methods move wholesale, preserving their windows exactly; `BeginServiceDispatch`'s two-field atomicity is preserved because both fields share the new lock. **Two sites widen** (`loadPlugin` §5, `RegisterHost`) — inherent to D-06, both on load/wiring paths, both documented at the call site. |
| T-8-06 — lock-ordering deadlock (DoS, medium) | mitigated | 26/26 production runtime call sites hold no `m.mu` (audited mechanically). No path holds two of three locks. `RACE=-race task test:int` exit 0, zero data races. |
| T-8-20 — `lookupManifest` fallback order (Spoofing, medium) | mitigated | Body byte-identical; Behavior 2 asserts all three branches (loaded / inflight-only / unknown) in one test body so the order cannot be half-preserved. |
| T-8-21 — `*Manager` ceasing to satisfy `ManifestLookup` (EoP, medium) | mitigated | `var _ manifestLookup = (*Manager)(nil)` retained and compiling; all 27 exported signatures diffed identical. |

## Verification

| Gate | Exit |
| --- | --- |
| `task build` | 0 |
| `task test -- ./internal/plugin/` | 0 (787 tests, `-race`) |
| `task test -- ./internal/plugin/... ./internal/eventbus/... ./internal/grpc/... ./cmd/holomush/...` | 0 (**4007 tests**) |
| **`task test:int`** (D-17 mandatory) | **0 — 10760 tests, 7 skipped** |
| **`RACE=-race task test:int`** | **0 — 10760 tests, ZERO data races** |
| `task lint` | 0 |
| `task license:check` | 0 |
| `task fmt` | no residual diff |

Named suites, all green:

```
✓  test/integration/pluginparity   (493ms)    ← D-20 symmetry detector (T-8-04)
✓  test/integration/plugincrypto   (9.133s)   ← crypto manifest gates (T-8-03)
✓  test/integration/wholesystem    (5.728s)   ← 9-plugin census intact
✓  test/integration/plugin         (20.133s)
```

Zero failing packages across the whole integration run. The `wholesystem` census still enumerates
the 9 expected plugins (`core-aliases`, `core-building`, `core-channels`, `core-communication`,
`core-help`, `core-objects`, `core-scenes`, `echo-bot`, `test-abac-widget`).

## Pre-Push Review Gates (D-19)

- **`crypto-reviewer` — REQUIRED.** This plan relocates both crypto manifest gates and sits
  adjacent to `event_emitter.go::Emit`. Both are explicit triggers. Do not skip it at PR time. The
  byte-identity diff, the nil-guard table, and the untouched-`event_emitter.go` proof above are
  written so a reviewer can audit the move without re-deriving it.
- **`abac-reviewer` — NOT required.** Verified rather than assumed:
  `git diff --stat origin/main...HEAD -- internal/access/` produces **no output** across the entire
  phase branch. `internal/access/` is untouched.

## Known Stubs

None. `PluginRuntime` is fully wired; every relocated method has a live production caller through
`Manager`'s forwarders.

## Threat Flags

None. No new network endpoint, deserialization site, auth path, file-access pattern, or schema
change. The two crypto authorization gates moved with zero body change and gained a second
fail-closed guard.

## Notes for 08-08 (the load unit)

- **`Manager.mu` still guards real state** and must not be deleted: `hosts`, `luaHost`,
  `loadedOrder`, and the `Configure*` host-iteration sections. All are load-time wiring, which is
  08-08's cluster.
- **`Close` and `UnloadPlugin` remain on `Manager`** as the plan directed. Both now touch only
  `hosts`/`luaHost`/`policyInstaller` plus one runtime call each, so 08-08's assignment is easier
  than it was pre-plan.
- **`loadedOrder` is coupled to `loaded` by a read-then-conditional-write.** 08-08 should decide
  whether it belongs with the loader (as today) or with the runtime registry it mirrors. If it
  moves, the `CommitLoaded`-returns-`existed` split collapses back into one atomic section — which
  would *undo* the T-8-05 widening recorded above. Worth doing.
- **The `capabilitiesFor` / `capabilitiesForLocked` pair** is the shape to copy if 08-08 needs
  another cached-lookup accessor.
- **`m.hosts[TypeLua]` vs `m.luaHost` was NOT collapsed**, per the standing prohibition. 08-08
  should file the follow-up GitHub issue at phase close.
- **`manager.go` is 1616 LoC**; `runtime.go` 601, `identity_store.go` 212. Wave C's ratchet should
  set ceilings from the post-08-08 actuals, not these.

## Self-Check: PASSED

Created files exist:
- `internal/plugin/runtime.go` — FOUND
- `internal/plugin/runtime_test.go` — FOUND

Commits exist on `gsd/phase-08-god-object-decomposition`:
- `a6ffaf60a` test(08-06): add failing tests for the PluginRuntime seam — FOUND
- `bd2b6d9cd` refactor(08-06): extract PluginRuntime and delegate Manager's runtime surface — FOUND
