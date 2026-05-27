<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Test-Tier Taxonomy & Test-Only-Construct Isolation — Design

- **Status:** Draft (pending design-reviewer)
- **Design bead:** holomush-1eps2
- **Precursor:** holomush-tcf7 (closed — removed the false `//go:build !integration` enforcement claim from `.claude/rules/testing.md`)
- **Absorbs:** holomush-bmtd (`test:int` explicit-package-list fragility) — closed into this bead
- **Related:** holomush-s5ts (External NATS deploy epic — owns the real-NATS test matrix)
- **Date:** 2026-05-25

## 1. Context

`holomush-tcf7` removed a documentation invariant that asserted a compile-time
enforcement which never existed: `.claude/rules/testing.md` claimed
*"the `//go:build !integration` tag on the harness file enforces"* that the
embedded NATS harness (`internal/eventbus/eventbustest`) is not used in E2E
tests. No such tag exists on `eventbustest/embedded.go`, and the harness is in
fact used by every non-unit test tier.

Triaging that exposed a tangle of three confusions this design resolves:

1. **An overloaded build tag.** The single `integration` tag spans bus-level
   tests and full-stack tests that all legitimately import `eventbustest`, so no
   build tag can separate them without breaking compilation.
2. **An overloaded word.** "E2E" means three different things across the repo:
   the Ginkgo `test:int` suite (`CLAUDE.md`), the Playwright suite (`pr-prep.md`
   - CI), and the top Go tier of the JetStream design spec.
3. **A fragile scoping mechanism.** `Taskfile.yaml`'s `test:int` target
   enumerates an explicit package list because `internal/core/store_memory.go`
   carries `//go:build !integration` (it is the **only** non-test file that
   does). Under `-tags=integration` that file is excluded, so any package whose
   unit tests reference `core.NewMemoryEventStore` fails to compile under
   `./...`. New integration tests in unlisted packages silently never run
   (this is `holomush-bmtd`).

### Grounding that reshaped the design

- **Production runs embedded NATS.** `internal/eventbus/subsystem.go` boots an
  in-process `nats-server/v2` via `DontListen` + `nats.InProcessServer` — the
  same path `eventbustest` uses. The only difference is `FileStorage` (prod) vs
  `MemoryStorage` (test). `ModeCluster`/external NATS is a reserved-but-
  unimplemented constant (`internal/eventbus/config.go`). **There is no
  embedded-vs-real fidelity gap today**; it opens only after the external-NATS
  pivot (`holomush-s5ts`). Building NATS-testcontainer infra now would be
  prod-shape-for-an-undeployed-system.
- **`eventbustest` is the only NATS in the entire test suite.** The sole
  testcontainer in use is Postgres.
- **A 4-tier model already exists** in `docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md`
  §8 (unit / bus-integration / audit-integration / E2E, embedded NATS at all
  non-unit tiers) — it just never propagated into `CLAUDE.md`/`testing.md`.

## 2. Goals

- Establish a single canonical test-tier taxonomy and reserve "E2E" for one
  meaning, propagated into `.claude/rules/testing.md` and `CLAUDE.md`.
- Replace the never-existent build-tag "enforcement" with a *real*,
  lint-enforced invariant: test-only constructs never leak into production code.
- Remove the root cause of the `test:int` package-list fragility so
  `task test:int -- ./...` works and the enumeration is deleted
  (absorbs `holomush-bmtd`).
- Define what "full-stack integration" means with respect to the in-tree
  plugin layer.

## 3. Non-Goals

- **No real/external NATS in tests.** Deferred to `holomush-s5ts`
  (its scope-item-5: "external-NATS test matrix; nats-server in test
  containers"). Embedded NATS stays at every tier because production is
  embedded NATS today.
- **No build-tag scheme** (`busint`/`!e2e` etc.). The honest invariant is a
  package-import rule, not a compilation-lane rule.
- **Not fixing the `integrationtest` harness ABAC bypass.** The harness wires
  `allowAllPolicyEngine` by default — a *separate* fidelity gap that masked a P0
  (`holomush-g776`) for 6+ weeks. Tracked in **holomush-f5t07** (sibling to the
  harness-loads-no-plugins gap this design addresses in §6/§7); not owned here.
- **Not building the whole-system plugin suite in this bead.** The *decision*
  is made here; the harness capability + suite are a discovered-from follow-up
  (see §7).

## 4. The Canonical Test-Tier Taxonomy

"E2E" is reserved for tests that cross the **real user boundary** (a browser,
or telnet) against a real running binary. Breadth of stack does not make a test
E2E — boundary crossed does. The Ginkgo `test:int` suite stands up the full
server in-process and calls Go/gRPC APIs directly, so it is **integration**, not
E2E, regardless of how much it wires up.

| Tier | Dependencies | Runner | Build tag |
| --- | --- | --- | --- |
| unit | none | `task test` | (none) |
| bus-integration | embedded NATS (`eventbustest`) | `task test:int` | `//go:build integration` |
| audit-integration | embedded NATS + Postgres testcontainer | `task test:int` | `//go:build integration` |
| full-stack integration | embedded NATS + Postgres + `CoreServer` (+ optional in-tree plugins, §6) | `task test:int` | `//go:build integration` |
| **E2E** | full Docker stack driven through a real client (browser) | `task test:e2e` | (Playwright, no Go tag) |

This adopts the JetStream-spec model as the operational SSOT. The two outliers
to correct: `CLAUDE.md`'s "MUST use Ginkgo/Gomega for E2E" phrasing and the
JetStream spec's "E2E" Go-tier name (rename to "full-stack integration").

## 5. The Real Invariant: depguard, Not a Build Tag

The property actually worth proving is **test-only constructs never leak into
production code** — production (non-test) packages must not import the embedded
test harness or the in-memory event store. This is a package-import rule,
enforced uniformly across every build lane by **depguard** (a `golangci-lint`
linter, not currently enabled).

### 5.1 Mechanical change (absorbs holomush-bmtd)

1. **Extract** `NewMemoryEventStore` (and the `MemoryEventStore` type) out of
   `internal/core/store_memory.go` into a test-only package
   `internal/core/coretest/`; **drop** the `//go:build !integration` tag.
2. **Enable depguard** in `.golangci.yaml` with deny rules forbidding imports of
   `internal/core/coretest` and `internal/eventbus/eventbustest`, scoped to
   production files only. **Allowlist:** `_test.go` files, `internal/testsupport/**`,
   and `internal/cluster/clustertest` (the test-support packages that
   legitimately wire harnesses in non-`_test.go` files).
3. **Delete the explicit package list** in `Taskfile.yaml`'s `test:int` target.
   With no `!integration` symbols remaining, `./...` compiles under
   `-tags=integration`. The recipe becomes
   `gotestsum -- -race -tags=integration ... {{.CLI_ARGS | default "./..."}}`,
   which **also fixes** bmtd's CLI_ARGS-passthrough gap
   (`task test:int -- -run X ./pkg/`) and kills the "new integration test
   silently never runs" drift class at the root.

`store_memory.go` is the only non-test `//go:build !integration` file in the
repo, so removing it is sufficient — no other enumeration cause exists.

## 6. Full-Stack Integration and the Plugin Layer

Plugins are part of the real stack: scenes, channels, objects, aliases, and
communication are **plugin-provided**. The 10 in-tree plugins break down as
6 Lua (including the `echo-bot` example), 2 binary (`core-scenes` and the
`test-abac-widget` fixture), and 2 setting/config-only. A full-stack
test that loads zero plugins exercises a hollow core. Production loads all of
them via `plugin.Manager.LoadAll` (DAG-resolved).

**Decision:** plugin-loading is a **harness capability**, not mandatory on every
full-stack test. Targeted full-stack tests (privacy floor, presence snapshot)
stay lean; a single **whole-system suite** loads *all* in-tree plugins via
`Manager.LoadAll`, mirroring production — the highest-fidelity Go-level tier,
the closest thing to E2E-minus-the-browser, where manifest-DAG / load-order /
cross-plugin-ABAC regressions surface.

Cost asymmetry that makes this affordable: 6 of 8 functional plugins are
in-process Lua (no subprocess, no build artifact). Only `core-scenes` and
`test-abac-widget` are hashicorp/go-plugin subprocesses requiring `build/plugins`
artifacts (which `task test:int` already builds via `plugin:build-all`). So the
binary layer is the only part warranting an availability gate.

The harness capability (`integrationtest.WithInTreePlugins()`) and the
whole-system suite are **implemented in a follow-up** (§7); this design only
fixes the *meaning* of the tier.

## 7. Follow-Up Work (filed, not done here)

- **Whole-system plugin suite + harness capability** (discovered-from 1eps2):
  add `integrationtest.WithInTreePlugins()` driving `Manager.LoadAll`, a
  binary-plugin availability gate, and one whole-system suite loading every
  in-tree plugin.
- **`integrationtest` ABAC-bypass fidelity gap** — the harness defaults to
  `allowAllPolicyEngine`; tracked in **holomush-f5t07** (not owned here).
- **Real/testcontainer NATS for E2E**: owned by `holomush-s5ts` scope-item-5;
  linked, not duplicated.

## 8. Invariants (RFC2119)

| # | Invariant | Enforcement / Test |
| --- | --- | --- |
| INV-1 | Production (non-test) packages **MUST NOT** import `internal/eventbus/eventbustest`. | depguard deny rule; fails `task lint`. |
| INV-2 | Production (non-test) packages **MUST NOT** import `internal/core/coretest` (the in-memory event store). | depguard deny rule; fails `task lint`. |
| INV-3 | `task test:int -- ./...` **MUST** compile and run with **no** explicit package enumeration in `Taskfile.yaml`. | CI integration job runs `./...`; a Taskfile-grep meta-test asserts no hard-coded package list remains. |
| INV-4 | Repo documentation **MUST** use "E2E" only for the Playwright/browser suite; Go Ginkgo suites **MUST** be called "integration". | A new CI grep-lint step asserting the tier vocabulary (no "Ginkgo … E2E" phrasing) in `.claude/rules/testing.md` + `CLAUDE.md`. NOTE: existing `lint:docs-symmetry` only checks the CLAUDE.md↔AGENTS.md symlink, not vocabulary — this is a *new* check. The plan **MAY** instead implement INV-4 as a documented convention (no machine check) if it judges a grep too brittle; this decision is deferred to the plan. |
| INV-5 | The whole-system suite (when implemented per §7) **MUST** load all in-tree plugins via `plugin.Manager.LoadAll`. | Asserted in the follow-up bead's tests; declared here. |

### Meta-test

`TestDepguardTestOnlyConstructRulesPresent` reads `.golangci.yaml` and asserts
the deny entries for `eventbustest` and `core/coretest` exist — guarding the
guard so INV-1/INV-2 cannot be silently deleted (the failure mode that produced
this whole saga). This is the meta-test required for non-trivial specs
(`feedback_invariants_and_docs_as_spec_acceptance`).

## 9. Documentation (PR-blocking)

- `.claude/rules/testing.md` — replace the EventBus-harness section with the §4
  tier table; document the depguard invariant (§5) and the full-stack/plugin
  meaning (§6).
- `CLAUDE.md` — fix "Ginkgo/Gomega for E2E" → "Ginkgo/Gomega for full-stack
  integration"; add a short pointer to the canonical taxonomy in `testing.md`.
- `docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md` §8 — rename
  the "E2E" Go tier to "full-stack integration" (or add a note reconciling it).
- `site/docs/contributing/integration-tests.md` — adopt the tier vocabulary;
  note that full-stack integration may load in-tree plugins.

## 10. Acceptance Criteria

- `task test:int -- ./...` succeeds; `Taskfile.yaml` `test:int` has no package
  enumeration and honors `CLI_ARGS`.
- `task lint` fails if any production package imports `eventbustest` or
  `core/coretest`; passes on the migrated tree.
- `NewMemoryEventStore` lives in `internal/core/coretest/`; `store_memory.go`
  carries no build tag (or is removed).
- The meta-test (`TestDepguardTestOnlyConstructRulesPresent`) passes.
- Docs in §9 carry the tiered taxonomy; "E2E" means Playwright everywhere.
- `holomush-bmtd` closed as "absorbed into holomush-1eps2"; the whole-system
  plugin suite filed as a discovered-from follow-up.

## 11. ADR-Worthy Decisions

1. **Test-tier taxonomy + "E2E" reserved for Playwright** (a tier-naming /
   vocabulary decision shaping how all future tests are classified).
2. **depguard package-import rule replaces build-tag isolation** for keeping
   test-only constructs out of production (a cross-cutting enforcement-mechanism
   decision).
3. **Full-stack integration includes the plugin layer via an opt-in capability +
   a whole-system suite** (a test-architecture decision about fidelity tiers).
<!-- adr-capture: sha256=1a54caf4300e2204; session=cli; ts=2026-05-25T16:11:18Z; adrs=holomush-qti5d,holomush-1r2hp,holomush-vjg7z -->
