# Phase 8: God-Object Decomposition - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-19
**Phase:** 8-God-Object Decomposition
**Mode:** `--auto` (all gray areas auto-selected; recommended option chosen per question)
**Areas discussed:** CoreServer split seam, plugin/manager split seam, bidirectional-coupling unwind scope, complexity threshold & regrowth ratchet, behavior-preservation proof, delivery shape

---

## CoreServer split seam (ARCH-01)

| Option | Description | Selected |
|--------|-------------|----------|
| RPC-group extraction behind a thin facade | Continue the `auth_handlers.go` precedent — split remaining methods into collaborator-scoped units; `CoreServer` stays the `corev1.CoreServiceServer` facade | ✓ |
| Full interface-driven decomposition | Extract domain services; `CoreServer` becomes pure protocol translation | |
| Field-grouping only | Bundle the ~30 collaborators into sub-structs, keep all methods on `CoreServer` | |

**Choice:** RPC-group extraction behind a thin facade *(recommended default)*
**Notes:** The precedent already exists in-package — `auth_handlers.go` holds 16 of 39 methods and six more files hold one each. Field-grouping was rejected because it does not satisfy success criterion 1's "separately-testable" bar. Full interface-driven decomposition exceeds a behavior-preserving refactor's mandate. Reinforced by D-02: each unit takes only its own collaborators, no `*CoreServer` backpointer — a backpointer would make this a file split rather than a decomposition.

---

## plugin/manager split seam (ARCH-02)

| Option | Description | Selected |
|--------|-------------|----------|
| Load-time wiring vs runtime delivery | The axis already named in issue #4674 and arch-review MEDIUM-4 | ✓ |
| Split by collaborator | Separate units per identity registry / service registry / host registry | |
| Extract only the loader | Pull `loadPlugin` + `LoadAll` out, leave the rest | |

**Choice:** Load-time wiring vs runtime delivery *(recommended default)*
**Notes:** Adopting the axis the tracking issue already identified rather than inventing one. Amended during analysis: a strict two-way split fails because the identity registry (`nameByID`/`activeByName`/`pluginRepo`) is written by load and read by runtime under one shared mutex (`manager.go:95-99`) — so it becomes a **third** unit with its own lock (D-06). `TestLoadPlugin` moving behind a build tag was folded in from #4674 and MEDIUM-4's shared recommendation.

---

## Bidirectional-coupling unwind scope (MEDIUM-4)

| Option | Description | Selected |
|--------|-------------|----------|
| In scope, sequenced first as enabling substrate | Extract the three shared seams before splitting, so the arrows point one way | ✓ |
| Defer entirely to 999.9 | Split the god objects; leave layering as-is | |
| Partial — only what blocks | Extract seams opportunistically as the split hits them | |

**Choice:** In scope, sequenced first *(recommended default)*
**Notes:** Phase 7's CONTEXT explicitly deferred MEDIUM-4 "to Phase 8 (ARCH-01/ARCH-02)". Splitting a god object whose subpackages point both ways relocates the mess rather than settling it — seven `internal/plugin` files import up into `internal/grpc/focus`, and `internal/eventbus/authguard` imports `internal/plugin`. The third seam (`TranslateSubscribeErr`) additionally closes arch-review LOW-6, which bears directly on this phase's own success criterion 3. `cryptowiring`'s three-subpackage reach was excluded (D-10) — already one-way.

---

## Complexity threshold & regrowth ratchet (success criterion 3)

| Option | Description | Selected |
|--------|-------------|----------|
| Committed meta-test ratchet | Pin per-unit size ceilings + import direction as a CI-enforced gate | ✓ |
| One-time review judgment | Agree a threshold, verify at review, no persistent gate | |
| Repo-wide golangci thresholds | Enable `funlen`/`gocognit`/`cyclop` across the codebase | |

**Choice:** Committed meta-test ratchet *(recommended default)*
**Notes:** The decisive evidence is MEDIUM-4's own measurement — both files **grew** after #4674 filed them (`manager.go` 1222→1869, `server.go` 1331→1891). In this repo, a split with no gate has already been observed to regrow, so the one-time-judgment option is refuted by history rather than by preference. Repo-wide linters rejected (D-13): none configured today, unrelated blast radius across a mature codebase. Three in-repo gate templates identified: `test/meta/plugin_host_capability_decomp_test.go`, `test/meta/world_import_graph_test.go`, `cmd/holomush/gateway_closure_test.go`.

---

## Behavior-preservation proof

| Option | Description | Selected |
|--------|-------------|----------|
| Existing suites unchanged as the contract | Full `test:int` + whole-system census green with zero assertion edits under `test/integration/**` | ✓ |
| Characterization/golden tests written first | Capture current behavior in new tests before refactoring | |
| Just run the suites | Run and eyeball | |

**Choice:** Existing suites unchanged as the contract *(recommended default)*
**Notes:** The success criteria already name the integration and whole-system suites passing *unchanged* — this decision makes that operationally checkable: an assertion edit under `test/integration/**` is a review-blocking signal requiring written justification. Up-front characterization tests were rejected as redundant (D-16) — those suites already are the characterization layer. New tests should instead prove the seams (each unit constructible with only its own dependencies), which is the actual claim in criterion 1.

---

## Delivery shape

| Option | Description | Selected |
|--------|-------------|----------|
| One phase PR, Wave 0 → A/B parallel → C | Seam extraction first, then ARCH-01 and ARCH-02 in parallel, then ratchet | ✓ |
| Two PRs (one per requirement) | Ship ARCH-01 and ARCH-02 separately | |

**Choice:** One phase PR with a three-stage wave structure *(recommended default)*
**Notes:** Matches Phase 5 (D-04) and Phase 7 precedent. ARCH-01 and ARCH-02 touch disjoint packages once Wave 0 lands, so they parallelize cleanly. Pre-push gate expectation recorded (D-19): `crypto-reviewer` should fire on ARCH-02 because the runtime-delivery half moves `PluginRequestsDecryption`/`PluginCanReadBack` and sits adjacent to `event_emitter.go::Emit`; `abac-reviewer` only if `internal/access/` is genuinely touched.

---

## Claude's Discretion

- Exact package names/paths for extracted units and the three neutral seam packages.
- Whether CoreServer sub-units are separate packages or same-package types.
- Whether the identity registry becomes a new package or a same-package type.
- Numeric ceilings in the ratchet (set from post-split actuals, not aspirational figures).
- `TestLoadPlugin`: build tag vs `export_test.go`, subject to the cross-package visibility check.
- Internal wave decomposition within Waves A and B (the Wave 0 → A/B → C ordering is fixed).

## Deferred Ideas

- `internal/core` grab-bag decomposition — a third god object, not named by ARCH-01/02.
- `internal/plugin/goplugin/host.go` (1615 LoC), `internal/plugin/hostcap/servers.go` (1360 LoC).
- `cmd/holomush`'s `coreOnlyFiles` allowlist (~30 entries) — re-deferred from Phase 7.
- Eventbus error-model inconsistency (`fmt.Errorf`+sentinel vs `oops`) from the same #4674 basket.
- MEDIUM-5: promote `internal/pgnanos` + `internal/idgen` to `pkg/`; move `auditheader` out of SDK internals.
- LOW-7 (unbounded `StopAll` shutdown), LOW-8 (`productionSubsystems` 15 positional params).
- `ReadinessRegistry.AllReady` vacuous-truth fail-open.
- Doc drift in `.planning/PROJECT.md` + `.planning/codebase/ARCHITECTURE.md` — already tracked by issue #4820.
- Stale LoC figures in `.planning/codebase/CONCERNS.md` (`manager.go` listed 1838, actual 1869).
