---
phase: 08-god-object-decomposition
plan: 09
subsystem: meta
tags: [arch-01, arch-02, ratchet, invariant, census, closeout, d-11, d-12, d-14]
status: complete
requires:
  - "08-07 (ARCH-01 closeout record — four units, four proof tests, server.go 657)"
  - "08-08 (ARCH-02 closeout record — three units, three proof tests, manager.go 702)"
  - "08-02 (the two D-09 seams whose direction this ratchet pins)"
provides:
  - "test/meta/phase8_decomposition_test.go — the regrowth ratchet: import direction, size ceilings, facade shape, and the seven-unit census"
  - "INV-PLUGIN-56 — the plugin layering guarantee, bound"
  - "The phase's D-15 behavior-preservation record"
affects:
  - "docs/architecture/invariants.yaml + .md (INV-PLUGIN-56 registered and rendered)"
  - ".planning/phases/08-god-object-decomposition/08-VALIDATION.md (contract closed out)"
tech-stack:
  added: []
  patterns:
    - "Tree-prefix forbidden-edge assertion (guards internal/grpc entire, not one subpackage)"
    - "Ceiling comparison factored into a helper so boundary semantics are asserted with synthetic values"
    - "Structural facade assertion (field-set equality) as the primary regrowth guard, LoC as backstop"
key-files:
  created:
    - test/meta/phase8_decomposition_test.go
  modified:
    - docs/architecture/invariants.yaml
    - docs/architecture/invariants.md
    - .planning/phases/08-god-object-decomposition/08-VALIDATION.md
decisions:
  - "Forbidden-edge rows target the whole internal/grpc TREE rather than internal/grpc/focus. Strictly stronger, and the RED proof confirms the prefix rule catches the subpackage. Pinning only the package that happened to be cut would leave the invariant routable via any sibling."
  - "Added a sixth edge (internal/plugin -> internal/eventbus/authguard), the mirror of D-09 seam 2, per 08-02's carry-forward. 08-02 inverted that dependency by deleting an adapter; re-adding one on the plugin side restores the same coupling and nothing else would stop it."
  - "Ceilings carry 10-18% headroom over measured actuals, each actual committed inline. manager.go pinned at 800 (not 702) per 08-08's delegation-growth analysis; server.go calibrated from 657, not 08-05's 642, per 08-07's +15 finding."
  - "Added TestPhase8FacadesHoldNoExtractedState — the structural check 08-08 recommended as PRIMARY. A line ceiling cannot distinguish 80 lines of delegation from 80 lines of state creeping back; the field set can."
  - "Did NOT hard-code method counts. The plan's 'verified figures' (CoreServer 39, Manager 36) are the PRE-split origin/main totals; post-split they are 31 and 26. Baking either into a permanent gate was the T-8-31 failure mode."
metrics:
  duration: ~70m
  tasks: 3
  files: 4
  completed: 2026-07-19
---

# Phase 8 Plan 09: The Regrowth Ratchet, Census and INV-PLUGIN-56 Summary

Shipped the gate that makes Phase 8 stick: a committed meta-test pinning the import direction D-09
established, the size of the nine files Waves A and B produced, and the shape of the two facades —
plus a seven-unit census and a bound `INV-PLUGIN-56`. **All three assertion halves were observed
FAILING before commit.** `task test:int` exit 0 (10783 tests), `task lint` exit 0, `task pr-prep`
`status=pass`.

## READ THIS FIRST — the RED proofs, verbatim

A ratchet written after the work it guards passes on first run whether or not it can ever fail.
First-run green is the null result. All three halves were therefore made to fail deliberately, the
message recorded, and the scratch edit reverted.

### RED proof 1 — import direction

Scratch edit: `_ "github.com/holomush/holomush/internal/grpc/focus"` added to
`internal/plugin/host.go`, reintroducing the D-09 seam-1 edge.

```
=== FAIL: test/meta TestPhase8ImportDirectionHasNoUpwardOrCyclicEdges (0.02s)
    phase8_decomposition_test.go:96:
        Error:      Should be false
        Messages:   forbidden import edge re-established: internal/plugin must NOT import
                    internal/grpc (found "github.com/holomush/holomush/internal/grpc/focus").
                    Seam: manager.go / host.go imported focus.Coordinator (08-02).
                    This is the bidirectional coupling arch-review MEDIUM-4 identified and Phase 8
                    Wave 0 removed (INV-PLUGIN-56). Production imports only; _test.go is exempt.
```

Note what this proves beyond "the gate fires": the row targets `internal/grpc` and the violation
was in `internal/grpc/**focus**`. The tree-prefix rule caught a subpackage. An exact-path table —
which is what the plan specified — would have missed it.

### RED proof 2 — size ceiling

Scratch edit: 100 filler lines appended to `internal/plugin/manager.go` (702 → 802, ceiling 800).

```
=== FAIL: test/meta TestPhase8DecomposedFilesStayUnderTheirCeilings (0.00s)
    phase8_decomposition_test.go:162:
        Error:      Should be false
        Messages:   internal/plugin/manager.go has regrown to 802 lines, over its ceiling of 800.

                    This is the Phase 8 regrowth ratchet (ARCH-01/ARCH-02, D-11/D-12). It is not
                    arbitrary: both files this phase decomposed had ALREADY been filed as god
                    objects in issue #4674 and then grew anyway — server.go 1331 -> 1891,
                    manager.go 1222 -> 1869. The ceiling exists because that happened.

                    Do NOT fix this by raising the ceiling in the same change that tripped it.
                    Recalibrating a gate to accommodate the regression it just caught is how the
                    gate stops being a gate. Extract the new cluster into its own unit instead, as
                    Phase 8 did; if the growth is genuinely irreducible, raise the ceiling in a
                    SEPARATE, separately-reviewed commit that says why.
```

### RED proof 3 — the structural facade check (not required by the plan; proven to the same standard)

Scratch edit: `hosts map[PluginType]Host` added back to `type Manager struct`.

```
=== FAIL: test/meta TestPhase8FacadesHoldNoExtractedState (0.00s)
    phase8_decomposition_test.go:273:
        Error:      elements differ
                    extra elements in list B: (string) (len=5) "hosts"
        Messages:   Manager must remain a facade over its three units plus its option holder,
                    holding NO plugin state of its own (ARCH-02). Fields found:
                    [hosts loader runtime identity cfg]. A new field here is how the god object
                    comes back — it grew 1222 -> 1869 lines that way once already (#4674).
```

Working tree confirmed clean after each revert (`git status --short` showed only the new file).

## Why a third half exists — 08-08's carry-forward, taken seriously

08-08 warned that a ceiling pinned at `manager.go`'s 702 would fail on the third legitimately-added
method: ~430 of those lines are option declarations, one-line forwarders and package-level helpers
that grow **mechanically** (~6 lines per exported method, ~8 per option). Its recommendation was to
pin with headroom *and* add a structural check, treating the latter as primary.

That is what shipped. The distinction matters because the two halves fail on different things:

| Guard | Catches | Blind to |
| --- | --- | --- |
| Size ceiling | bulk regrowth of any kind | 80 lines of delegation vs 80 lines of state — indistinguishable |
| Field-set equality | state creeping back onto the facade | a unit file growing internally |

The field check is immune to delegation growth, which is exactly the growth 08-08 predicted would
otherwise force the ceiling to be either too tight to survive normal work or too loose to bite.

## Ceilings — calibration, with every actual committed inline

Each ceiling carries its measured actual in a trailing comment in the source, so a future bump reads
in review as a visibly widening gap rather than an opaque number change (T-8-30).

| File | Actual | Ceiling | Headroom | Note |
| --- | --- | --- | --- | --- |
| `internal/grpc/server.go` | 657 | 750 | 14% | calibrated from **657, not 08-05's 642** — 08-07's +15 delegation growth was correct |
| `internal/grpc/subscribe_handler.go` | 973 | 1100 | 13% | |
| `internal/grpc/command_handler.go` | 417 | 480 | 15% | |
| `internal/grpc/lifecycle_handler.go` | 386 | 445 | 15% | |
| `internal/grpc/query_handler.go` | 213 | 250 | 17% | small file; absolute slack modest |
| `internal/plugin/manager.go` | 702 | 800 | 14% | 08-08's suggested figure, adopted |
| `internal/plugin/loader.go` | 1142 | 1300 | 14% | largest unit; flagged in-source as next split candidate |
| `internal/plugin/runtime.go` | 593 | 680 | 15% | |
| `internal/plugin/identity_store.go` | 212 | 250 | 18% | |

`TestPhase8CeilingsAreCalibratedBelowThePreSplitSizes` asserts `server.go < 1891` and
`manager.go < 1869` — a guard against the ratchet being neutered wholesale rather than
incrementally. `TestPhase8CeilingComparisonIsInclusiveAtTheCeiling` pins the boundary with
synthetic values (699 pass / 700 pass / 701 fail), independent of any real file's size.

## Plan premises corrected

### 1. The census method counts were pre-split figures

The plan states as **verified** that `CoreServer` has 39 methods and `Manager` 36 — and warns
(T-8-31) that CONTEXT.md's "37" is off by one and hard-coding it "would bake a wrong number into a
permanent gate". Both figures are themselves wrong for the post-split tree. Measured:

| Type | `origin/main` (pre-split) | HEAD (post-split) |
| --- | --- | --- |
| `CoreServer` methods | **39** | **31** (23 exported) |
| `Manager` methods | **36** | **26** |

39 and 36 are the pre-split totals. A census asserting either would have failed on first run — the
exact failure mode T-8-31 describes, arriving through the figures the plan supplied to prevent it.

Resolution: **no method count is hard-coded.** The plan's own action says "prefer deriving counts
over hard-coding them where that is straightforward", and the derived, meaningful assertion is the
field-set equality above. A magic number here would need updating on every legitimate method
addition, which is how gates get deleted.

(Note also a 1-line discrepancy against 08-08's "27 exported Manager methods"; I measure 26.
Not chased — nothing depends on it, precisely because no count was baked in.)

### 2. The forbidden-edge table is six rows, not five

Five named in the plan; the sixth is 08-02's carry-forward mirror edge
(`internal/plugin ↛ internal/eventbus/authguard`). 08-02 closed seam 2 by *deleting*
`authguard/adapter_manifest.go` and inverting the dependency through structural satisfaction. The
symmetric re-coupling — an adapter on the plugin side — would look like an entirely reasonable fix
to anyone who does not know why the original was deleted, and no other check in the repo would stop
it. The row carries that rationale in an in-source comment.

### 3. Rows target the `internal/grpc` tree, not `internal/grpc/focus`

Widened, as documented under RED proof 1. The invariant summary I registered says "the
`internal/grpc` tree", so the assertion and the registry entry now say the same thing.

## INV-PLUGIN-56 — bound to the import half only

```yaml
- id: INV-PLUGIN-56
  scope: INV-PLUGIN
  origin_spec: "docs/reviews/arch-review/2026-07-11/findings/d1-architecture.md"
  summary: "The plugin layer sits BELOW its consumers and beside the event bus: no production
    package in the internal/plugin tree may import the internal/grpc tree, and internal/eventbus
    and internal/plugin MUST NOT depend on one another in either direction. Test files are exempt
    (they may hold concrete fixtures)."
  binding: bound
  asserted_by:
    - "test/meta/phase8_decomposition_test.go"
```

The binding is **earned, not fabricated**: the annotated test asserts exactly the property the
summary states, and RED proof 1 demonstrates it fails when the property is violated.

**`origin_spec` needed no invented document.** The plan anticipated the registry might require a
`specs/` path; it does not — `docs/adr/…` and `docs/superpowers/plans/…` are already in use
(`invariants.yaml:2190,3445,4849,4910`), so the arch-review finding that is the actual origin is a
conforming value.

**The size half is deliberately unbound** (D-14). `TestPhase8DecomposedFilesStayUnderTheirCeilings`
carries no `// Verifies:` annotation, and no second invariant was minted
(`rg -c 'id: INV-PLUGIN-57'` → 0). A registry entry backed by a line counter claims a guarantee the
test does not make.

`docs/architecture/invariants.md` regenerated via `task invariants:render`;
`go run ./cmd/inv-render -check` exits 0. `TestProvenanceGuard` passed without needing a
`shared_files` amendment — the entry carries `asserted_by`, not `refs`, so the seven-plan provenance
pattern did not recur here.

## The census

Seven units, seven proof tests, each asserted to exist **and** to be in an external test package —
the D-02 property that makes "separately testable" a fact rather than a claim.

| Unit | File | Proof test | Package |
| --- | --- | --- | --- |
| `SubscribeHandler` | `internal/grpc/subscribe_handler.go` | `subscribe_handler_test.go` | `grpc_test` |
| `CommandHandler` | `internal/grpc/command_handler.go` | `command_handler_test.go` | `grpc_test` |
| `LifecycleHandler` | `internal/grpc/lifecycle_handler.go` | `lifecycle_handler_test.go` | `grpc_test` |
| `QueryHandler` | `internal/grpc/query_handler.go` | `query_handler_test.go` | `grpc_test` |
| `IdentityStore` | `internal/plugin/identity_store.go` | `identity_store_test.go` | `plugins_test` |
| `PluginRuntime` | `internal/plugin/runtime.go` | `runtime_test.go` | `plugins_test` |
| `PluginLoader` | `internal/plugin/loader.go` | `loader_test.go` | `plugins_test` |

An in-package test can reach unexported state and prove nothing about the seam; the external-package
assertion is what makes the census meaningful rather than decorative.

## D-15 behavior-preservation record

The phase's headline claim, evidenced rather than asserted. All six diff-stats, verbatim:

```
$ git diff --stat origin/main...HEAD -- test/integration/
(no output)

$ git diff --stat origin/main...HEAD -- api/proto/ pkg/proto/
(no output)

$ git diff --stat origin/main...HEAD -- internal/grpc/mocks/
(no output)

$ git diff --stat origin/main...HEAD -- internal/plugin/event_emitter.go
(no output)

$ git diff --stat origin/main...HEAD -- .golangci.yaml
(no output)

$ git diff --stat origin/main...HEAD -- internal/access/
(no output)
```

**`test/integration/` has no diff at all across the entire phase branch.** The plan asks for every
hunk to be classified as an import/constructor rewire rather than an assertion change; there are
**zero hunks to classify**. That is the strongest available result — the classification procedure
has an empty input set, so the "zero assertion churn" claim needs no judgment call.

Each empty diff and what it settles:

| Diff | Decision | Settles |
| --- | --- | --- |
| `test/integration/` | D-15 | Zero assertion churn; a nine-plan cross-package refactor moved no behavior |
| `api/proto/` `pkg/proto/` | D-03 | No wire-format change; a proto diff here would be a scope error |
| `internal/grpc/mocks/` | D-04 | `CoreServerOption` signature unchanged |
| `internal/plugin/event_emitter.go` | D-20 | The Lua/binary common emit path is untouched — no privilege gradient |
| `.golangci.yaml` | D-13 | No repo-wide `funlen`/`gocognit`/`gocyclo`/`cyclop`; the gate stays scoped to this phase's artifacts |
| `internal/access/` | D-19 | `abac-reviewer` is NOT required (see below) |

## ROADMAP success criteria — consolidated evidence

### SC1 — CoreServer decomposed, units independently testable

Four units, four `package grpc_test` proof tests (table above). `server.go` **1891 → 657**
(trajectory 1891 → 1154 → 642 → 657; the final +15 is delegation stubs plus a constructor, and is
correct growth). Exported method set **identical at 23**, diffed against `origin/main`.

### SC2 — Manager decomposed, load/runtime/identity each standalone

Three units, three `package plugins_test` proof tests. `manager.go` **1876 → 702**
(1876 → 1845 → 1616 → 702). `Manager` reduced to exactly four fields
(`loader`, `runtime`, `identity`, `cfg`), none of them plugin state — now mechanically pinned.
Exported method set and all 11 `ManagerOption` signatures identical against `origin/main`.
The whole-system plugin census still enumerates all 9 plugins
(`test/integration/wholesystem` green).

### SC3 — regrowth ratchet

Both halves shipped with ceilings as tabled, plus the facade structural check.
`INV-PLUGIN-56` bound. The pre-existing gates remain green and were not modified:

```
✓  test/integration/pluginparity   — D-20 symmetry detector (T-8-04)
✓  test/integration/plugincrypto   — crypto manifest gates
✓  test/integration/wholesystem    — 9-plugin census
✓  cmd/holomush TestGatewayImportsAreOnlyProtocolTranslation
```

## Pre-push review gates (D-18, D-19)

- **`crypto-reviewer` — REQUIRED for the phase PR.** Not because of this plan (which touches only
  `test/meta/`, the registry, and planning artifacts) but because ARCH-02 relocated the crypto
  manifest gates and `ConfigureEventEmitter` sits adjacent to `event_emitter.go::Emit`. 08-06 and
  08-08 both flagged it. `event_emitter.go` is provably untouched, but adjacency is an explicit
  trigger — do not skip it.
- **`abac-reviewer` — NOT required.** Verified, not assumed:
  `git diff --stat origin/main...HEAD -- internal/access/` produces **no output** across the entire
  phase branch (recorded verbatim above).

## Threat model outcomes

| Threat | Disposition | Outcome |
| --- | --- | --- |
| T-8-28 — a vacuous ratchet whose ceilings can never fire (**high**) | mitigated | RED proofs 2 and 3 observed the gate failing. Ceilings sit 10-18% over measured actuals, each actual committed inline, and `TestPhase8CeilingsAreCalibratedBelowThePreSplitSizes` blocks a wholesale bump to pre-split levels. |
| T-8-29 — a fabricated `// Verifies:` on the size test (**high**) | mitigated | The annotation is file-level and covers the import-direction property only; the size test is unannotated; no second invariant minted. `TestBoundInvariantsAreGenuinelyAsserted` passes, and RED proof 1 is the substantive evidence the binding is earned. |
| T-8-30 — silencing a future failure by raising the ceiling (medium) | mitigated | Each actual is committed beside its ceiling, so a bump reads as a widening gap. The failure message itself instructs against same-change recalibration and names the separate-commit alternative. |
| T-8-31 — baking a wrong method count into a permanent gate (low) | mitigated | Found the plan's own figures were pre-split (39/36 vs actual 31/26) and hard-coded **no** count. |
| T-8-01..04 (carried) — the phase's moved security gates (**high**) | mitigated | Consolidated evidence checkpoint: six empty diffs above, `event_emitter.go` untouched, `pluginparity` + `plugincrypto` + `access` suites green, `crypto-reviewer` obligation recorded. |

## Verification

| Gate | Exit |
| --- | --- |
| `task test -- ./test/meta/` | 0 (**102 tests**) |
| `task test -- -run 'TestEveryRegistryInvariantHasBinding\|TestProvenanceGuard\|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/` | 0 (7 tests) |
| `go run ./cmd/inv-render -check` | 0 (no drift) |
| **`task test:int`** (D-17) | **0 — 10783 tests, 7 skipped/quarantined** |
| `task lint` | 0 |
| `task license:check` | 0 |
| `task pr-prep` (inline, final gate) | **0 — result file `status=pass lane=fast exit=0`** |
| `task fmt` | no residual diff |

Judged by exit code throughout, per the repo rule; the `pr-prep` verdict was read from its
`▸ pr-prep result:` file, never from a matched output string.

## Acceptance criteria

| Criterion | Result |
| --- | --- |
| `task test -- ./test/meta/` exits 0 | ✅ |
| No helper redeclared (`modulePath` / `findRepoRoot` / `worldPkgImports`) | ✅ grep → 0; `worldPkgImports` reused (2 refs) |
| Forbidden-edge table + helper reuse | ✅ **six** rows (five planned + mirror edge) |
| Every ceiling carries its actual inline; none ≥ 1891 / 1869 | ✅ and mechanically asserted |
| Both RED proofs recorded verbatim | ✅ **three** recorded |
| Census covers seven units + seven proof tests | ✅ plus external-package assertion |
| `.golangci.yaml` untouched | ✅ empty diff |
| SPDX header present | ✅ `task license:check` 0 |
| `INV-PLUGIN-56` registered, bound, `asserted_by` correct | ✅ |
| `INV-PLUGIN-57` not minted | ✅ 0 |
| `08-VALIDATION.md` has no `*TBD*` rows; `nyquist_compliant: true` | ✅ 28 task rows |

## Known Stubs

None.

## Threat Flags

None. This plan adds a meta-test, a registry entry and planning artifacts. No production code path,
input, endpoint, auth path, file-access pattern or schema changed.

## Notes for the phase PR

- **`loader.go` at 1142 LoC is now the largest unit in either tree** and the obvious next split
  candidate (`loadPlugin` alone is ~300 lines with six rollback branches). Its ceiling is set at
  1300 with an in-source comment naming it as next in line. Not fixed here — out of scope, and
  splitting it would have meant relocating code in the same change that ships the gate guarding
  relocation.
- The pre-existing outbox-relay data race (#4828, proven pre-existing by 08-08 against the baseline
  tree) is unaffected by this plan; `RACE=-race` was not re-run here since no production code moved.
- Three follow-up issues from 08-08 remain open: #4826, #4827, #4828.

## Self-Check: PASSED

Created files exist:
- `test/meta/phase8_decomposition_test.go` — FOUND
- `.planning/phases/08-god-object-decomposition/08-09-SUMMARY.md` — FOUND

Modified files exist:
- `docs/architecture/invariants.yaml` — FOUND (`id: INV-PLUGIN-56` → 1)
- `docs/architecture/invariants.md` — FOUND (regenerated, `-check` clean)
- `.planning/phases/08-god-object-decomposition/08-VALIDATION.md` — FOUND

Commits exist on `gsd/phase-08-god-object-decomposition`:
- `302fb1cd4` test(08-09): add the Phase 8 regrowth ratchet, size ceilings and census — FOUND
- `829453512` docs(08-09): register INV-PLUGIN-56 for the plugin layering guarantee — FOUND
- `88aca5032` docs(08-09): close out the Phase 8 validation contract — FOUND
