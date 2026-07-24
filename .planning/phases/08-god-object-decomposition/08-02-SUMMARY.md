---
phase: 08-god-object-decomposition
plan: 02
subsystem: plugin-layering
tags: [refactor, dependency-inversion, layering, arch-02, d-09-seam-2, d-08]
status: complete
requires:
  - "internal/focuscontract (08-01)"
provides:
  - "internal/plugin with zero production imports of internal/grpc"
  - "internal/eventbus with zero production imports of internal/plugin"
  - "Fail-closed nil-receiver semantics on the two crypto manifest gates"
  - "internal/plugin/export_test.go — TestLoadPlugin out of the production binary"
affects:
  - internal/plugin
  - internal/eventbus/authguard
  - cmd/holomush
  - internal/testsupport/integrationtest
tech-stack:
  added: []
  patterns:
    - "Dependency inversion by structural satisfaction — delete the adapter, let the concrete type satisfy the consumer-declared interface"
    - "Locally-declared mirror interface + var _ assertion to catch signature drift without importing the consumer"
    - "Nil-receiver guard as the carrier of a fail-closed contract lost when an adapter is removed"
key-files:
  created:
    - internal/plugin/export_test.go
  modified:
    - internal/plugin/manager.go
    - internal/plugin/host.go
    - internal/plugin/goplugin/host.go
    - internal/plugin/hostcap/capabilities.go
    - internal/plugin/lua/host.go
    - internal/plugin/lua/focus_ops_adapter.go
    - internal/plugin/lua/hostcap_adapter.go
    - internal/plugin/hostfunc/functions.go
    - internal/plugin/setup/subsystem.go
    - internal/plugin/crypto_manifest_lookup_test.go
    - cmd/holomush/sub_grpc.go
    - internal/testsupport/integrationtest/crypto.go
  deleted:
    - internal/eventbus/authguard/adapter_manifest.go
    - internal/eventbus/authguard/adapter_manifest_test.go
decisions:
  - "Moved the deleted manifestAdapter's nil-guard onto the two Manager methods rather than relying on authguard.New's AUTHGUARD_DEPENDENCY_NIL check, which cannot see a typed-nil *Manager stored in an interface"
  - "Declared the two-method assertion interface locally in internal/plugin rather than importing authguard, avoiding a mirror-image edge"
  - "Took the export_test.go branch of D-08 — the post-seam-2 caller set is empty outside internal/plugin, so no build-tag plumbing was needed"
metrics:
  duration: ~40m
  completed: 2026-07-19
  tasks: 4
  commits: 4
  files_created: 1
  files_modified: 12
  files_deleted: 2
---

# Phase 8 Plan 02: Seam Completion & D-08 Settlement Summary

Closed both remaining D-09 seams — rewired the 7 `internal/plugin` files onto
`internal/focuscontract` and inverted the `eventbus → plugin` edge by deleting the
manifest adapter — then settled D-08 against the caller set that resulted. Both
cycle-forming directions are now gone.

## What Was Built

### Seam 1 — `plugin → grpc` eliminated (Task 1)

Seven production files rewired from `internal/grpc/focus` to `internal/focuscontract`.
Because 08-01 made the originals `=` aliases, these name the *identical* types: no
conversion, no adapter, no signature change, and every caller in `cmd/holomush` and
`internal/grpc` compiled untouched.

| File | Symbols re-qualified |
|---|---|
| `manager.go`, `host.go`, `goplugin/host.go`, `hostcap/capabilities.go`, `lua/host.go`, `lua/focus_ops_adapter.go` | `Coordinator` only |
| `lua/hostcap_adapter.go` | `Coordinator`, `RestorePlan`, `SetConnectionFocusResult`, `AutoFocusOnJoinResponse`, `AutoFocusFailure` |

`rg -IN 'holomush/internal/grpc' internal/plugin/ --glob '!*_test.go'` → **no matches**
(was 7).

### Seam 2 — `eventbus → plugin` eliminated (Task 2, TDD)

`*plugins.Manager` now satisfies `authguard.ManifestLookup` by structural satisfaction.
`adapter_manifest.go` (`NewPluginManifestLookup` + `manifestAdapter`) is deleted, and the
two callers pass the manager directly.

The load-bearing detail is the **nil-guard migration**. The deleted adapter returned
`false` on a nil manager; a typed-nil `*plugins.Manager` stored in a `ManifestLookup`
interface is not interface-nil, so `authguard.New`'s `AUTHGUARD_DEPENDENCY_NIL` check
cannot catch it. Without a guard, `PluginRequestsDecryption` reaches `m.mu.RLock()` inside
`lookupManifest` and panics — converting a denial into a crash on the decrypt path.

A local unexported `manifestLookup` interface plus `var _ manifestLookup = (*Manager)(nil)`
catches future signature drift in `internal/plugin` at compile time, declared locally so
no mirror-image `plugin → authguard` edge is created.

### D-08 settled (Task 3)

`TestLoadPlugin` moved verbatim from `manager.go` into a new `internal/plugin/export_test.go`.

## RED-First Proof (Task 2)

The nil-guard was developed RED-first, not asserted after the fact.

**RED** — commit `08f36a26c`, tests written before any guard existed:

```
=== FAIL: internal/plugin TestManagerPluginRequestsDecryptionReturnsFalseOnNilReceiver (0.00s)
    Error: func (assert.PanicTestFunc)(0x103cb6e10) should not panic
           Panic value: runtime error: invalid memory address or nil pointer dereference
=== FAIL: internal/plugin TestManagerPluginCanReadBackReturnsFalseOnNilReceiver (0.00s)
    Error: func (assert.PanicTestFunc)(0x103cb6fb0) should not panic
           Panic value: runtime error: invalid memory address or nil pointer dereference
DONE 2 tests, 2 failures in 0.535s        (rc=201)
```

Both failed by **panicking with a nil pointer dereference** — the exact predicted
condition, not a generic assertion miss. This confirms the test reproduced the real
hazard rather than a proxy for it.

**GREEN** — commit `78a3e6e9a`: `DONE 2 tests in 1.509s` (rc=0).

No REFACTOR step was needed; the guard is a two-line early return on each method.

Positive-path manifest-matching cases were **not** re-added — they are already covered
in-package by `internal/plugin/crypto_manifest_lookup_test.go`, which is precisely the
duplication the deleted authguard test represented.

## D-08 Caller Inventory (verbatim, taken after Task 2)

```
$ rg -n 'TestLoadPlugin\(' --type go
internal/plugin/manager.go:1472:func (m *Manager) TestLoadPlugin(name string, manifest *Manifest) {
internal/plugin/crypto_manifest_lookup_test.go:61:	mgr.TestLoadPlugin(m.Name, m)
internal/plugin/manager_audit_test.go:50:	m.TestLoadPlugin("core-scenes", scenes)
internal/plugin/manager_audit_test.go:51:	m.TestLoadPlugin("core-nochan", nochan)
internal/plugin/manager_audit_test.go:78:	m.TestLoadPlugin("plain", plain)
internal/plugin/manager_audit_test.go:107:	m.TestLoadPlugin("multi", multi)
internal/plugin/manager_test.go:1581:	m.TestLoadPlugin(name, manifest)
internal/plugin/manager_test.go:2068:	mgr.TestLoadPlugin("core-test", manifest)
internal/plugin/identity_registry_test.go:265:	mgr.TestLoadPlugin("core-scenes", manifest)
```

Package of each: `manager.go` → `plugins`; `identity_registry_test.go` → `plugins`;
`crypto_manifest_lookup_test.go`, `manager_audit_test.go`, `manager_test.go` →
`plugins_test`.

**Branch taken: `export_test.go`.** All 8 call sites are inside `internal/plugin`. The
only out-of-package caller before this plan was
`internal/eventbus/authguard/adapter_manifest_test.go:30,50` (package `authguard_test`),
deleted by Task 2 as predicted. `_test.go` symbols in package `plugins` are visible to
both the `plugins` and `plugins_test` test binaries, so all 8 call sites compile
untouched — verified: `git diff --stat -- internal/plugin/manager_test.go
internal/plugin/manager_audit_test.go internal/plugin/identity_registry_test.go` produces
no output. No build-tag plumbing through `task test` / `test:int` / `test:cover` / lint
was needed.

## Task Breakdown

| Task | Name | Commit |
|---|---|---|
| 1 | Rewire 7 plugin files off `internal/grpc/focus` | `f75e63eb4` |
| 2 | Invert seam 2 — RED | `08f36a26c` |
| 2 | Invert seam 2 — GREEN (guards + adapter deletion + caller rewire) | `78a3e6e9a` |
| 3 | Move `TestLoadPlugin` into `export_test.go` (D-08) | `a3c57d342` |
| 4 | Wave-boundary integration gate (verification only, no files) | — |

## Verification Results

| Gate | Result |
|---|---|
| `task build` | exit 0 |
| `task test -- ./internal/plugin/... ./internal/grpc/... ./cmd/holomush/...` (Task 1) | exit 0 — 3087 tests |
| `task test -- -run 'NilReceiver' ./internal/plugin/` (RED) | **exit 201 — 2 failures, both nil-pointer panics** |
| `task test -- -run 'NilReceiver' ./internal/plugin/` (GREEN) | exit 0 — 2 tests |
| `task test -- ./internal/plugin/... ./internal/eventbus/... ./cmd/holomush/... ./internal/testsupport/...` | exit 0 — 3375 tests, 1 skipped |
| `task test -- ./internal/plugin/... ./internal/eventbus/...` (Task 3) | exit 0 — 2819 tests |
| **`task test:int`** (D-17 mandatory gate) | **exit 0** — 10714 tests, 7 skipped |
| `task lint` | exit 0 |

**Downstream suites named by the plan, both green under `task test:int`:**

```
✓  test/integration/pluginparity (470ms)
✓  test/integration/plugincrypto (7.145s)
```

`pluginparity` is the D-20 Lua-vs-binary symmetry detector (both runtimes reach the same
alias-identical `Coordinator`); `plugincrypto` exercises the `PluginRequestsDecryption` /
`PluginCanReadBack` paths whose nil contract moved.

**D-15 zero-integration-churn record:** `git diff --stat origin/main...HEAD --
test/integration/` produces **no output**. No assertion under the integration tree was
edited, viewed, or reached.

## Acceptance Criteria Evidence

| Check | Result |
|---|---|
| `rg -IN 'holomush/internal/grpc' internal/plugin/ --glob '!*_test.go'` | no matches (was 7) |
| `rg -n 'focuscontract\.focuscontract\.\|focus\.focus\.' internal/plugin/` | no matches |
| `rg -n '\bfocus\.[A-Z]' internal/plugin/ --glob '!*_test.go'` | no matches |
| `rg -lN 'holomush/internal/plugin"' internal/eventbus/ --glob '!*_test.go'` | no matches (was `adapter_manifest.go`) |
| `rg -cN 'holomush/internal/eventbus/authguard' internal/plugin/` | 0 — no mirror-image edge |
| `rg -N 'NewPluginManifestLookup' --type go` | no matches anywhere |
| `rg -c 'AUTHGUARD_DEPENDENCY_NIL' internal/eventbus/authguard/guard.go` | 5 — unchanged, check untouched |
| `rg -c 'func (m \*Manager) TestLoadPlugin' internal/plugin/manager.go` | 0; declared in `export_test.go` |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Two comment-only `focus.Coordinator` references outside the 7 named files**

- **Found during:** Task 1, verifying the acceptance criterion `rg -n '\bfocus\.[A-Z]'
  internal/plugin/ --glob '!*_test.go'` → no matches.
- **Issue:** `internal/plugin/setup/subsystem.go:94` and
  `internal/plugin/hostfunc/functions.go:199` mention `focus.Coordinator` in doc comments.
  Neither file imports `internal/grpc/focus` — they were never seam edges — but their
  presence kept the criterion red.
- **Fix:** Updated both comments to name `focuscontract.Coordinator`, the canonical home.
  Same rationale as 08-01's registry retarget: when a type relocates, the references
  follow rather than pointing at a re-export.
- **What did NOT change:** no code, no import, no invariant token. The `INV-SCENE-38`
  token in `subsystem.go` is preserved verbatim; only the sentence wrapped to a second
  line.
- **Files modified:** `internal/plugin/setup/subsystem.go`,
  `internal/plugin/hostfunc/functions.go`
- **Commit:** `f75e63eb4`

No other deviation. No architectural decision required, no checkpoint hit, no
authentication gate. Contrary to 08-01's warning, `TestProvenanceGuard` did **not** fire —
this plan moved no INV-annotated comment out of a registry-recorded site.

## Threat Model Outcomes

| Threat | Disposition | Outcome |
|---|---|---|
| T-8-03 — nil-guard lost with the adapter (Information Disclosure, high) | mitigated | Guard moved onto both `Manager` methods with a genuine RED-first proof: both tests panicked with `invalid memory address or nil pointer dereference` before the guard existed, and pass after. Manifest-matching logic is byte-identical — the only diff hunks in those two methods are the `if m == nil { return false }` prologues and doc comments. `test/integration/plugincrypto` green. |
| T-8-04 — per-runtime `Coordinator` divergence (EoP/Tampering, high) | mitigated | Both runtime adapters (`lua/hostcap_adapter.go`, `goplugin/host.go`) rewired in one commit to the same alias-identical `focuscontract.Coordinator`; neither can land on a different interface. `test/integration/pluginparity` (D-20 detector) green. |
| T-8-08 — mirror-image `plugin → authguard` edge (Tampering, medium) | mitigated | The assertion interface is declared locally as unexported `manifestLookup` in `manager.go`. `rg -cN 'holomush/internal/eventbus/authguard' internal/plugin/` → 0. |
| T-8-09 — `sub_grpc.go` guard wiring (DoS, medium) | accepted as planned | Argument-shape change only; the `INV-CRYPTO-22 fallback disabled` warn-path and `authguard.New`'s nil rejection are untouched. Residual risk stayed compile-time. |
| T-8-10 — `TestLoadPlugin` relocation (Repudiation, low) | accepted as planned | Verbatim move; behavior including the no-host path preserved and documented at the new site. Attack surface strictly reduced. |

## Key Decisions

1. **The guard belongs on the methods, not at the construction site.** `authguard.New`'s
   `AUTHGUARD_DEPENDENCY_NIL` check is structurally incapable of catching a typed-nil
   concrete pointer in an interface. Strengthening that check would not have helped; the
   fail-closed behavior has to live where the receiver is dereferenced.
2. **Local interface, not an imported one.** Importing `authguard` to write the assertion
   would have deleted an edge and immediately recreated its mirror. The unexported local
   interface gets the same compile-time drift protection at zero coupling.
3. **`export_test.go`, decided after the fact.** D-08 was left conditional on purpose. The
   caller set was re-enumerated post-Task-2 and found empty outside `internal/plugin`,
   which is what made the cheap branch legitimate rather than assumed.

## Known Stubs

None. No placeholder, TODO, or unwired component was produced.

## Threat Flags

None. No network endpoint, deserialization site, auth path, file access pattern, or schema
change was introduced. The two crypto authorization gates were made strictly more
defensive.

## Notes for Next Plan

- **Both D-09 seams are now complete cuts.** Waves A and B can proceed on genuinely
  disjoint packages: `internal/plugin` and `internal/eventbus` both sit below
  `internal/grpc` with no upward edge.
- **The Wave C ratchet has real edges to pin.** The forbidden-edge table should carry
  `internal/plugin ↛ internal/grpc` and `internal/eventbus ↛ internal/plugin`, plus the
  mirror `internal/plugin ↛ internal/eventbus/authguard` — that last one is a live
  regression risk, since re-adding the adapter would look like a natural fix to anyone who
  does not know why it was deleted. Per D-10, do **not** add `cryptowiring`'s eventbus
  reach to that table.
- **The nil-guards are load-bearing and non-obvious.** If ARCH-02's runtime-delivery split
  relocates `PluginRequestsDecryption` / `PluginCanReadBack` onto a new type, the
  `if m == nil` prologues and their doc comments MUST travel with them, and the local
  `manifestLookup` assertion must be retargeted to whatever type authguard ends up
  receiving. Dropping either silently reintroduces T-8-03.
- **`internal/plugin/export_test.go` now exists** as the home for test-only seams on
  production types. Further `Test*`-prefixed production methods found during ARCH-02
  belong there rather than in a new file.
- `internal/testsupport/integrationtest/crypto.go` is `//go:build integration` tagged, so
  its rewire compiles only under `task test:int` — another reason D-17 is not optional
  here.

## Self-Check: PASSED

Created files verified present on disk:
- `internal/plugin/export_test.go` — FOUND

Deleted files verified absent:
- `internal/eventbus/authguard/adapter_manifest.go` — GONE
- `internal/eventbus/authguard/adapter_manifest_test.go` — GONE

Commits verified in `git log`:
- `f75e63eb4` — FOUND
- `08f36a26c` — FOUND
- `78a3e6e9a` — FOUND
- `a3c57d342` — FOUND
