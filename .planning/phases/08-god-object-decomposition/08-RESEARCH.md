# Phase 8: God-Object Decomposition - Research

**Researched:** 2026-07-19
**Domain:** Behavior-preserving structural refactor of two Go god objects (`internal/grpc.CoreServer`, `internal/plugin.Manager`) + import-cycle unwind + regrowth ratchet
**Confidence:** HIGH (all claims below are `path:line`-grounded against the phase-08 worktree at `cce89c702`; no external/library research was required)

---

## Summary

This phase needs almost no external research — it is entirely repo-internal. What a planner
needs is the **verified substrate**: which methods touch which fields (that matrix *is* the
decomposition), what the seams actually cost, and which recorded landmines are still true.
This document supplies all three, computed mechanically rather than by inspection.

Three findings materially change the shape of the phase versus what CONTEXT.md assumes:

1. **D-09 seam 3 is already done.** `TranslateSubscribeErr` does not live in `internal/grpc`;
   it lives in `internal/grpcclient/client.go:133`, and `internal/telnet/gateway_handler.go:720`
   already calls it through `grpcclient`. Phase 7's `internal/grpcclient` extraction closed
   this. `internal/grpc` is **already** in the gateway forbidden list
   (`cmd/holomush/gateway_imports_test.go:162`). Wave 0 shrinks from three seams to two.
2. **`Manager.UnloadPlugin` now exists** (`internal/plugin/manager_unload.go:22`). The landmine
   in `.claude/rules/references/plan-review-learnings.md` saying it does not is **stale** and
   should be corrected. Two other landmines in that file remain accurate.
3. **`loadPlugin` never holds `m.mu` across identity and runtime mutations** — it takes and
   releases the lock in four short, disjoint critical sections. D-06's separate-lock extraction
   is therefore mechanically safe and does not change lock ordering. This substantially de-risks
   the highest-risk decision in the phase.

The remaining two seams are **complete cuts**: `internal/plugin` → `internal/grpc` has exactly
7 production edges and *all 7* are `internal/grpc/focus`; `internal/eventbus` → `internal/plugin`
has exactly 1 production edge. Extracting the focus types and the manifest contract removes both
directions entirely, with no residual edges.

**Primary recommendation:** Sequence Wave 0 as two type-only extractions (focus types, manifest
contract), then drive both Wave A and Wave B from the method→field matrices in §Architecture
Patterns — the clusters are already latent in the data and require no judgment calls except at
the four cross-cutting `Manager` methods named in §Common Pitfalls.

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

Copied verbatim from `.planning/phases/08-god-object-decomposition/08-CONTEXT.md` `## Implementation Decisions`.

**ARCH-01 — CoreServer decomposition**

- **D-01: Split by RPC group into collaborator-scoped units behind a thin facade.** Continue the precedent already set in this package: `auth_handlers.go` (16 `CoreServer` methods) was split off previously and works. Extend that line to the remaining groups — subscribe/stream delivery (`Subscribe`, `runSubscribeLoop`, `dispatchDelivery`, `applyFilterCtrl`, `makeFilterUpdater`, `computeInitialFilters`, `toSubject`, `toProtoSubscribeResponse`), command execution (`HandleCommand`, `executeCommand`, `executeViaDispatcher`, `emitCommandResponse`), session lifecycle (`Disconnect`, `recomputeSessionLiveness`, `runDisconnectHooks`), and current-state queries (`GetCommandHistory`, `QueryStreamHistory`, `ListFocusPresence`, `ListSessionStreams`, `ListAvailableCommands`, `RefreshConnection`, `SessionIdentity`).
- **D-02: Each extracted unit takes ONLY the collaborators it uses, constructor-injected — not a `*CoreServer` backpointer.** This is the actual bar in success criterion 1 ("separately-testable"). A unit that reaches back into the parent struct is a file split, not a decomposition, and will not satisfy the criterion. The ~30-field struct is the symptom; narrow per-unit dependency sets are the cure.
- **D-03: The proto service contract is the fixed facade.** `CoreServer` keeps implementing `corev1.CoreServiceServer` and its exported method set stays byte-identical pre/post. No `api/proto/**` edits, therefore no `task proto` / `task web:generate` regeneration in this phase. A proto diff in this phase's PR is a scope error.
- **D-04: `NewCoreServer` + the `CoreServerOption` set stay the public wiring surface.** Options route into sub-unit constructors internally. Existing callers — `cmd/holomush/sub_grpc.go` and `internal/testsupport/integrationtest/harness.go` — must not need semantic rewiring. (Compile-level adjustments are fine; a caller having to learn a new wiring *concept* means the facade leaked.)

**ARCH-02 — plugin/manager decomposition**

- **D-05: Split along the load-time-wiring vs runtime-delivery line named in #4674 and MEDIUM-4.** That line is already identified in the tracking issue; adopt it rather than inventing a new axis.
  - **Load-time:** `Discover`, `computeHashes`, `resolveLoadOrder`, `loadPlugin`, `LoadAll`, `seedAliases`, `discoverAndRegisterAttributes`, `unregisterPluginProviders`, `warnUnknownTrustAllowlistEntries`, `BuildFocusRedirects`, `RegisterPluginCommands`, the retention sweep.
  - **Runtime-delivery:** `DeliverEvent`, `DeliverCommand`, `EmitPluginEvent`, `BeginServiceDispatch`, `QuerySessionStreams`, and the read-side lookups (`IsPluginLoaded`, `GetLoadedPlugin`, `lookupManifest`, `PluginRequestsDecryption`, `PluginCanReadBack`, `AuditSubjects`, `PluginAuditClient`).
- **D-06: The identity registry is a THIRD unit, not half of either.** `nameByID` / `activeByName` / `pluginRepo` / `retentionDays` are *written* by load and *read* by runtime — the current code says so explicitly ("Both are guarded by the existing `m.mu` RWMutex", `manager.go:95-99`). That shared mutex is precisely the coupling that makes a two-way split fail. Extract it as its own type owning its own lock; `NameByID` / `IDByName` / `ListPlugins` delegate.
- **D-07: `Manager` remains the public type and facade.** `NewManager` and all 11 `ManagerOption`s keep their signatures. `internal/plugin/setup/subsystem.go`, `cmd/holomush`, and every existing test compile unchanged.
- **D-08: `TestLoadPlugin` moves behind a test build tag or into `export_test.go`** — named explicitly in both #4674 and MEDIUM-4's recommendation; it currently ships in the production binary with no build tag (`manager.go:1472`). ⚠️ **Trap:** `_test.go` symbols do not cross package boundaries. Before choosing `export_test.go`, enumerate the callers — any caller outside `package plugins` needs a non-test file or must itself move.

**MEDIUM-4 — shared-seam extraction (sequenced FIRST)**

- **D-09: The bidirectional-coupling unwind is IN scope and is the prerequisite wave.** Three seams, all cited in `docs/reviews/arch-review/2026-07-11/findings/d1-architecture.md` MEDIUM-4 / LOW-6:
  1. **`internal/grpc/focus` types → a neutral lower package.** Seven `internal/plugin` files import *up* into the grpc tree (`manager.go`, `host.go`, `goplugin/host.go`, `hostcap/capabilities.go`, `lua/{host,focus_ops_adapter,hostcap_adapter}.go`). ARCH-02 cannot produce a clean layering while the manager imports its own consumer's subpackage.
  2. **`internal/eventbus/authguard/adapter_manifest.go:7`'s `internal/plugin` import → a neutral manifest contract.** eventbus both underlies and depends on plugin today.
  3. **`TranslateSubscribeErr` → a neutral wire-error package.** `internal/telnet/gateway_handler.go:29` imports `internal/grpc` for this one helper (used at `:718`), dragging the entire CoreServer monolith — and transitively `internal/{world,access,command}` — into the gateway's closure while passing the tripwire (LOW-6). Fixing this is *also* the cheapest way to keep success criterion 3's "no new gateway-boundary violations" honest.
- **D-10: `internal/plugin/cryptowiring`'s reach into three eventbus subpackages is NOT unwound here.** It is a wiring package doing wiring and its direction is already one-way (`cryptowiring.go:20-22`). Noted, not touched.

**Complexity threshold + regrowth ratchet (success criterion 3)**

- **D-11: The "agreed threshold" is enforced by a committed meta-test ratchet, not a one-time review judgment.** The decisive evidence is MEDIUM-4's own: both files **grew** after #4674 filed them — `manager.go` 1222 → 1869, `server.go` 1331 → 1891. A split with no gate demonstrably regrows in this repo. In-repo precedents to follow:
  - `test/meta/plugin_host_capability_decomp_test.go` — a decomposition census meta-test (asserts a god-service is gone and every former member is rehomed; `// Verifies: INV-PLUGIN-47`).
  - `test/meta/world_import_graph_test.go` — a production-only import-direction gate built on `go/build` (`build.Package.Imports` excludes `_test.go`, so it guards production imports without false-flagging test fixtures).
  - `cmd/holomush/gateway_closure_test.go` — Phase 7's transitive-closure gate.
- **D-12: The ratchet pins BOTH the size ceilings for the decomposed units AND the import direction established by D-09.** Direction without size regrows the files; size without direction lets the arrows re-tangle.
- **D-13: Do NOT enable repo-wide `funlen` / `gocognit` / `gocyclo` / `cyclop` in `.golangci.yaml`.** None are configured today; turning them on repo-wide is a separate change with large unrelated blast radius across a mature codebase. Scope the gate to this phase's own artifacts.
- **D-14: Consider binding an invariant.** A decomposition that ships a census meta-test is exactly the shape the registry wants (`.claude/rules/invariants.md`). If the ratchet genuinely pins a durable guarantee, allocate `INV-<SCOPE>-N` and annotate `// Verifies:`. Do **not** fabricate a binding on a test that only counts lines.

**Behavior-preservation proof**

- **D-15: The contract is — `task test:int` and the whole-system plugin census pass with ZERO edits to assertions under `test/integration/**`.** An assertion edit is a review-blocking signal that requires written justification, because it means behavior moved. This is the operational reading of criteria 1 and 2 ("suites pass *unchanged*").
- **D-16: No up-front characterization/golden tests.** The existing integration + whole-system suites already ARE the characterization layer — that is why the success criteria name them. New tests in this phase should assert the **new seams' structure and unit-testability** (each extracted unit constructible and exercisable with only its own narrow dependencies), not re-assert behavior the integration suite already pins.
- **D-17: `task test:int` is MANDATORY, not optional.** This is a cross-package type/wiring refactor — the exact shape where `task test` stays green while integration breaks silently, because `task test` does not compile `//go:build integration` files. Same landmine Phase 7 flagged and hit.

**Delivery shape**

- **D-18: One phase PR** (Phase 5 D-04 and Phase 7 precedent). Wave structure:
  - **Wave 0** — D-09 shared-seam extraction (prerequisite; unblocks clean layering).
  - **Waves A / B** — ARCH-01 (CoreServer) and ARCH-02 (manager) **in parallel**. After Wave 0 they touch disjoint packages with no shared dependency.
  - **Wave C** — ratchet meta-test + census + any invariant binding.
- **D-19: Pre-push review gates — expect `crypto-reviewer` to fire on ARCH-02.** The runtime-delivery half moves `PluginRequestsDecryption` / `PluginCanReadBack` (crypto manifest gates) and sits adjacent to `internal/plugin/event_emitter.go::Emit` — an explicitly listed crypto-reviewer trigger and the plugin-runtime-symmetry chokepoint. `abac-reviewer` fires only if `internal/access/` is actually touched (the CoreServer split rewires `accessEngine` but should not edit that tree — verify, don't assume).
- **D-20: Plugin-runtime symmetry is a hard constraint on ARCH-02.** Any host-side gate that moves during the split MUST still apply identically to Lua and binary plugins. The shared chokepoint is `event_emitter.go::Emit`; a split that puts a gate on one runtime's path only is a symmetry violation, not a refactor (`.claude/rules/plugin-runtime-symmetry.md`).

### Claude's Discretion

- Exact package names and paths for the extracted units and the three neutral seam packages.
- Whether CoreServer sub-units are separate packages or same-package types (D-02's narrow-dependency rule is the constraint; the packaging is not).
- Whether the identity registry (D-06) becomes a new package or a same-package type.
- Numeric ceilings in the ratchet — set from the post-split *actual* with modest headroom, not an aspirational round number.
- `TestLoadPlugin`: build tag vs `export_test.go` (subject to D-08's visibility check).
- Internal wave decomposition within Waves A and B; the Wave 0 → A/B → C ordering is fixed.

### Deferred Ideas (OUT OF SCOPE)

- **`internal/core` grab-bag decomposition** — after Phase 7 removed `Event`/`Engine`, it still holds Actor, Character, ParseCommand, VerbRegistry, ULID, actor_context, session_ended_payload. → 999.9 or its own phase.
- **`internal/plugin/goplugin/host.go` (1615 LoC) and `internal/plugin/hostcap/servers.go` (1360 LoC)** — genuine hotspots, but not named by ARCH-01/02. Touched in this phase only where D-09's seam extraction requires.
- **`cmd/holomush`'s `coreOnlyFiles` allowlist (~30 entries)** — still out. It governs `cmd/holomush`, not the two god objects.
- **Eventbus error-model inconsistency** — cross-cutting error-model change, not a god-object split. File separately.
- **MEDIUM-5: promote `internal/pgnanos` + `internal/idgen` to `pkg/`, move `auditheader` out of the SDK's internal deps** — unrelated axis.
- **LOW-7: unbounded `orch.StopAll(context.Background())` shutdown** and **LOW-8: `productionSubsystems`' 15 same-typed positional params** — bootstrap-shaped. File as issues if not already tracked.
- **`ReadinessRegistry.AllReady` vacuous-truth fail-open** — file if it becomes load-bearing.
- **Doc drift (already tracked):** `.planning/PROJECT.md` "Key Decisions" #3 and `.planning/codebase/ARCHITECTURE.md` still assert event-sourcing after the MODEL-01 reversal. Tracked by issue **#4820** — do not re-file.
- **`.planning/codebase/CONCERNS.md` LoC figures are stale.** Cosmetic; refresh opportunistically at phase close.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| ARCH-01 | The `CoreServer` god object is decomposed into cohesive, separately-testable units with no behavior change (epics `holomush-1bft`/`wm0fi`) — `.planning/REQUIREMENTS.md:35` | §Architecture Patterns "CoreServer method→collaborator matrix" gives the complete 39×30 mapping and derives five clusters with their exact narrow dependency sets; §Code Examples gives the constructor-injection shape; §Validation Architecture gives the per-unit separately-testable proof |
| ARCH-02 | The `plugin/manager` god object is decomposed similarly, with no behavior change (epic `holomush-dj95`) — `.planning/REQUIREMENTS.md:36` | §Architecture Patterns "Manager method→field matrix" gives the complete 36×25 mapping, confirms D-06's three-way split is forced *and* safe (disjoint critical sections), and names the four cross-cutting methods that need explicit decisions |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

These are actionable directives extracted from the root `CLAUDE.md` and the auto-loading
`.claude/rules/` that apply to this phase. The planner must verify compliance.

| Constraint | Source | Applies here because |
|---|---|---|
| `task` is the ONLY entry point for build/test/lint/fmt — never raw `go test` / `golangci-lint` | CLAUDE.md § Commands | Every verification step in every task |
| **`task test` does NOT compile `//go:build integration` files** — `task test:int` MUST run on refactors | CLAUDE.md § Testing; D-17 | This is a cross-package type/wiring refactor: the exact failure shape |
| TDD — tests written before implementation | CLAUDE.md § Development Principles | Applies to the new seam/unit tests (D-16), not to re-asserting existing behavior |
| `main` is protected; feature branch + PR + squash merge | CLAUDE.md § Protected Branch Policy | One phase PR (D-18) |
| Run `task fmt` after touching any aligned Go `const`/`var`/`struct` block — and **commit the result** | CLAUDE.md § Commands | The `CoreServer`/`Manager` struct literals are aligned blocks; a field removal reflows them |
| SPDX Apache-2.0 header on every new `.go` file | CLAUDE.md § License Headers | Every new file in Wave 0 / A / B / C |
| `slog.*Context(ctx, …)` / `errutil.LogErrorContext` whenever a `ctx` is in scope — enforced by `sloglint` `context: scope` | `.claude/rules/logging.md` | Moved method bodies carry their log calls; a mechanical move preserves compliance, a rewrite may not |
| `oops` for structured errors; call accessor methods with `()` | CLAUDE.md § Error Handling | Moved error-construction sites |
| Line-scoped `//nolint:<rule>` only — never widen `.golangci.yaml` | `.claude/rules/subagent-briefing.md` | Reinforces D-13 from the opposite direction |
| Binary and Lua plugins MUST be treated identically by the host; gates belong at the common path | `.claude/rules/plugin-runtime-symmetry.md`; D-20 | ARCH-02 moves `event_emitter.go::Emit`-adjacent code |
| Gateway holds protocol-translation deps only | `.claude/rules/gateway-boundary.md` | Success criterion 3 |
| A new `INV-<SCOPE>-N` MUST be registered in `docs/architecture/invariants.yaml`; regenerate with `go run ./cmd/inv-render`; NEVER fabricate a `// Verifies:` binding | `.claude/rules/invariants.md`; D-14 | Wave C |
| Concurrent sessions work in separate worktrees; sub-agents inherit the parent's worktree and MUST NOT be dispatched in parallel over the same files | CLAUDE.md § Session isolation | Waves A and B are parallel — they touch disjoint packages (verified §Blast Radius), so this is satisfiable |

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| gRPC RPC handling / protocol translation | API / Backend (`internal/grpc`) | — | `CoreServer` implements `corev1.CoreServiceServer`; D-03 fixes this as the facade |
| Session-ownership authentication preamble | API / Backend (`internal/auth`) | — | Already a free function `auth.ValidateSessionOwnership` (see below) — no tier change |
| Plugin discovery / load ordering / DAG | API / Backend (`internal/plugin`) | Database (`store.PluginRepo`) | Load-time unit (D-05) |
| Plugin runtime delivery / dispatch | API / Backend (`internal/plugin`) | — | Runtime unit (D-05); the `pluginHosts` map is the whole dependency |
| Plugin identity resolution (name ↔ ULID) | API / Backend (`internal/plugin`) | Database (`store.PluginRepo`) | Identity unit (D-06); written by load, read by runtime |
| Focus coordination contract types | API / Backend — **currently mis-tiered** in `internal/grpc/focus` | — | D-09 seam 1: the *types* belong below both `grpc` and `plugin`; the *implementation* stays put |
| Manifest crypto-gate lookup contract | API / Backend — **currently mis-tiered** (eventbus depends up into plugin) | — | D-09 seam 2 |

**Note on the third seam:** `TranslateSubscribeErr` is already correctly tiered in
`internal/grpcclient` — see §State of the Art.

---

## Standard Stack

This phase introduces **no new dependencies**. It is a pure in-repo structural refactor. The
"stack" is the existing toolchain and the two meta-test precedents.

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `go/build` (stdlib) | Go toolchain | Production-only import-graph inspection for the ratchet | `build.Package.Imports` **excludes** `_test.go` files, so it guards production imports without false-flagging test fixtures — the exact property the ratchet needs `[VERIFIED: test/meta/world_import_graph_test.go:20-31]` |
| `golang.org/x/tools/go/packages` | (in `go.mod`) | Transitive-closure + syntax-level import checking | Used by the existing gateway tripwire with `Tests: true` to also see `_test.go` imports `[VERIFIED: cmd/holomush/gateway_imports_test.go:173-185]` |
| `github.com/stretchr/testify` | (in `go.mod`) | `require`/`assert` in meta-tests | Repo convention `[VERIFIED: test/meta/world_import_graph_test.go:14]` |

### Supporting

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/samber/oops` | (in `go.mod`) | Structured errors on moved error sites | Preserve existing codes verbatim when relocating |
| Ginkgo/Gomega | (in `go.mod`) | Integration suites (unchanged) | Read-only in this phase per D-15 |

**Installation:** none — no `go get` in this phase.

## Package Legitimacy Audit

**Not applicable.** This phase installs **zero** external packages. No registry lookups were
performed and none are needed; every symbol referenced in this document is in-repo or stdlib.

- Packages removed due to `[SLOP]` verdict: none
- Packages flagged as suspicious `[SUS]`: none

---

## Architecture Patterns

### System Architecture Diagram

Current production import direction across the four packages this phase touches. `==>` marks
the two cycle-forming edges Wave 0 removes.

```
                    ┌──────────────────────┐
   telnet/web ─────►│  internal/grpcclient │  (TranslateSubscribeErr lives HERE
   (gateway)        └──────────────────────┘   — seam 3 already closed, Phase 7)
                                │
                                ▼  (wire types only)
   ┌───────────────────────────────────────────────────────┐
   │  internal/grpc  (CoreServer facade, 39 methods)        │
   │    ├─ auth_handlers.go   (16)                          │
   │    ├─ server.go          (17)                          │
   │    └─ 6 × one-method files                             │
   │  internal/grpc/focus  (Coordinator + 4 result types)   │
   └───────────────────────────────────────────────────────┘
        ▲                                        │
        ║ (7 production edges — ALL focus)       │ uses
        ║  ==> Wave 0 seam 1 removes             ▼
   ┌────╨──────────────────────────────┐   ┌──────────────────┐
   │  internal/plugin  (Manager, 36)   │   │ internal/session │
   │    ├─ load-time    (12 methods)   │   │  (leaf; no grpc  │
   │    ├─ runtime      (12 methods)   │   │   import)        │
   │    └─ identity     ( 3 methods)   │   └──────────────────┘
   └───────────────────────────────────┘
        ▲
        ║ (1 production edge: adapter_manifest.go)
        ║  ==> Wave 0 seam 2 removes
   ┌────╨────────────────────────────┐
   │  internal/eventbus/authguard    │
   └─────────────────────────────────┘
```

After Wave 0, both `║` edges are gone: a new neutral package (below `grpc`, `plugin`, and
`eventbus`; above `session`) carries the focus contract types, and a neutral manifest-lookup
contract carries the two crypto-gate predicates.

### Recommended Project Structure

Package placement is Claude's discretion per CONTEXT.md. The **constraint** the matrices impose:

```
internal/
├── focuscontract/          # NEW (Wave 0 seam 1) — types only, no behavior
│                           #   deps: context, ulid, internal/session
├── plugincontract/         # NEW (Wave 0 seam 2) — ManifestLookup contract
│                           #   deps: none beyond stdlib
├── grpc/
│   ├── server.go           # facade: NewCoreServer + CoreServerOption + struct
│   ├── auth_handlers.go    # EXISTING precedent (16 methods)
│   ├── <stream unit>       # Subscribe cluster
│   ├── <command unit>      # HandleCommand cluster
│   ├── <lifecycle unit>    # Disconnect cluster
│   └── <query unit>        # current-state RPCs
└── plugin/
    ├── manager.go          # facade: NewManager + 11 ManagerOptions + struct
    ├── <loader>            # load-time unit
    ├── <runtime>           # runtime-delivery unit
    └── <identity>          # identity registry (own lock)
```

### Pattern 1: The method→collaborator matrix drives the split

Computed mechanically over all non-test files in each package by extracting each method body
(brace-balanced) and intersecting `s.<ident>` / `m.<ident>` references with the declared field
set. `[VERIFIED: computed from internal/grpc/*.go and internal/plugin/*.go at cce89c702]`

#### CoreServer — 39 methods × 30 fields

Struct: `internal/grpc/server.go:154-232`. Method count **39**, matching CONTEXT.md.
Distribution: `auth_handlers.go` 16, `server.go` 17, plus 6 one-method files.

| File:Line | Method | #Fields | Fields touched |
|---|---|---|---|
| `auth_handlers.go:189` | `resolvePlayerSession` | 1 | playerSessionRepo |
| `auth_handlers.go:194` | `AuthenticatePlayer` | 2 | authService, playerSessionRepo |
| `auth_handlers.go:243` | `SelectCharacter` | 6 | charRepo, newSessionID, playerRepo, presence, sessionDefaults, sessionStore |
| `auth_handlers.go:419` | `CreatePlayer` | 2 | authService, playerSessionRepo |
| `auth_handlers.go:465` | `CreateCharacter` | 1 | characterService |
| `auth_handlers.go:510` | `ListCharacters` | 0 | *(none)* |
| `auth_handlers.go:533` | `ListAllCharacters` | 2 | accessEngine, charRepo |
| `auth_handlers.go:611` | `RequestPasswordReset` | 1 | resetService |
| `auth_handlers.go:628` | `ConfirmPasswordReset` | 1 | resetService |
| `auth_handlers.go:662` | `Logout` | 4 | authService, playerSessionRepo, presence, sessionStore |
| `auth_handlers.go:720` | `CheckPlayerSession` | 1 | playerRepo |
| `auth_handlers.go:761` | `CreateGuest` | 1 | guestService |
| `auth_handlers.go:802` | `ListPlayerSessions` | 1 | playerSessionRepo |
| `auth_handlers.go:840` | `RevokePlayerSession` | 1 | playerSessionRepo |
| `auth_handlers.go:883` | `RevokeOtherPlayerSessions` | 1 | playerSessionRepo |
| `auth_handlers.go:917` | `buildCharacterSummaries` | 3 | charRepo, sessionStore, worldQuerier |
| `list_available_commands.go:21` | `ListAvailableCommands` | 3 | commandQuerier, playerSessionRepo, sessionStore |
| `list_focus_presence.go:28` | `ListFocusPresence` | 4 | accessEngine, characterNameResolver, playerSessionRepo, sessionStore |
| `list_session_streams.go:30` | `ListSessionStreams` | 4 | focusCoordinator, playerSessionRepo, sessionStore, streamContributor |
| `query_stream_history.go:49` | `QueryStreamHistory` | 4 | accessEngine, historyReader, identityRegistry, sessionStore |
| `refresh_connection.go:19` | `RefreshConnection` | 2 | playerSessionRepo, sessionStore |
| `session_identity.go:31` | `buildCharacterIdentity` | 2 | bindings, cryptoActive |
| `server.go:373` | `currentGameID` | 1 | gameID |
| `server.go:390` | `HandleCommand` | 2 | playerSessionRepo, sessionStore |
| `server.go:458` | `executeCommand` | 0 | *(none)* |
| `server.go:467` | `executeViaDispatcher` | 4 | cmdServices, dispatcher, presence, sessionStore |
| `server.go:625` | `emitCommandResponse` | 1 | publisher |
| `server.go:673` | `runDisconnectHooks` | 1 | disconnectHooks |
| `server.go:700` | `toProtoSubscribeResponse` | 1 | identityRegistry |
| `server.go:746` | `computeInitialFilters` | 0 | *(none)* |
| `server.go:765` | `toSubject` | 0 | *(none)* |
| `server.go:808` | `Subscribe` | **8** | focusCoordinator, playerSessionRepo, sessionStore, streamContributor, streamRegistry, subscriber, verbRegistry, worldQuerier |
| `server.go:1167` | `runSubscribeLoop` | 0 | *(none)* |
| `server.go:1265` | `dispatchDelivery` | 2 | sceneMute, sessionStore |
| `server.go:1454` | `applyFilterCtrl` | 0 | *(none)* |
| `server.go:1487` | `makeFilterUpdater` | 0 | *(none)* |
| `server.go:1532` | `Disconnect` | 3 | playerSessionRepo, presence, sessionStore |
| `server.go:1738` | `recomputeSessionLiveness` | 1 | sessionStore |
| `server.go:1811` | `GetCommandHistory` | 2 | playerSessionRepo, sessionStore |

**Key structural facts this table establishes:**

1. **The matrix is extremely sparse.** Max fan-out is 8 (`Subscribe`); the median is 1–2.
   26 of 30 fields are touched by ≤ 4 methods. This is a *good* god object to split — the
   clusters are nearly disjoint already.
2. **Five methods touch ZERO fields** (`ListCharacters`, `executeCommand`, `computeInitialFilters`,
   `toSubject`, `runSubscribeLoop`, `applyFilterCtrl`, `makeFilterUpdater` — 7 in total). These are
   pure functions or pure delegators and can move anywhere at zero cost. `toSubject`,
   `computeInitialFilters`, `runSubscribeLoop`, `applyFilterCtrl`, `makeFilterUpdater` should
   arguably stop being methods entirely.
3. **`playerSessionRepo` (15 methods) and `sessionStore` (15 methods) are NOT a decomposition
   axis — they are a shared authentication preamble.** Verified: the co-occurrence is the call
   `auth.ValidateSessionOwnership(ctx, s.playerSessionRepo, s.sessionStore, token, sessionID)`
   `[VERIFIED: internal/grpc/server.go:403-409]`, repeated at `server.go:405,845,1550,1822`,
   `list_focus_presence.go:47`, `refresh_connection.go:24`. It is **already a free function in
   `internal/auth`** — nothing needs extracting. Each unit holds these two fields (or a tiny
   value bundling them) and calls the existing helper.

**Derived clusters** (matching D-01's named groups), with each unit's dependency set *after*
factoring out the shared auth preamble:

| Unit | Methods | Own collaborators (beyond the auth preamble pair) |
|---|---|---|
| **auth** (exists) | 16 | authService, resetService, characterService, guestService, playerRepo, charRepo, accessEngine, presence, sessionDefaults, newSessionID, worldQuerier |
| **stream/subscribe** | Subscribe, runSubscribeLoop, dispatchDelivery, applyFilterCtrl, makeFilterUpdater, computeInitialFilters, toSubject, toProtoSubscribeResponse | focusCoordinator, streamContributor, streamRegistry, subscriber, verbRegistry, worldQuerier, sceneMute, identityRegistry |
| **command** | HandleCommand, executeCommand, executeViaDispatcher, emitCommandResponse | dispatcher, cmdServices, presence, publisher |
| **lifecycle** | Disconnect, recomputeSessionLiveness, runDisconnectHooks | presence, disconnectHooks |
| **query** | GetCommandHistory, QueryStreamHistory, ListFocusPresence, ListSessionStreams, ListAvailableCommands, RefreshConnection, buildCharacterIdentity | accessEngine, historyReader, identityRegistry, characterNameResolver, commandQuerier, focusCoordinator, streamContributor, bindings, cryptoActive |

**Methods whose field set spans two proposed groups (the hard cases):**

| Method | Spans | Assessment |
|---|---|---|
| `Subscribe` (`server.go:808`, 8 fields) | stream + query (`focusCoordinator`, `streamContributor` are also query-unit deps) | **Not actually hard** — these are *shared read-only collaborators*, not shared mutable state. Both units take their own reference to the same `focus.Coordinator`. No coordination needed. |
| `toProtoSubscribeResponse` (`server.go:700`) | stream + query (`identityRegistry`) | Same — a shared read-only resolver. Consider making it a free function taking the registry. |
| `executeViaDispatcher` (`server.go:467`) | command + lifecycle (`presence`) | Same — `presence.Emitter` is a shared collaborator, not shared state. |
| `buildCharacterSummaries` (`auth_handlers.go:917`) | auth + query (`worldQuerier`) | Stays with auth (its only caller is in auth_handlers). |

**Conclusion for ARCH-01: there are no genuinely conflicted methods.** Every apparent overlap is
a *shared collaborator* (an interface or emitter safely referenced by two units), not shared
mutable state. D-02's constructor injection resolves all of them by simply passing the same
value to both constructors. This is the single most important planning fact in this document —
the CoreServer split has no coordination problem.

#### Manager — 36 methods × 25 fields

Struct: `internal/plugin/manager.go:73-105`. Method count is **36**, not 37: 35 in
`manager.go` + 1 (`UnloadPlugin`) in `manager_unload.go`. `[VERIFIED: rg -c '^func \(m \*Manager\)' internal/plugin/*.go]`
CONTEXT.md's "37 methods" is off by one — a cosmetic drift, flagged so a census meta-test does
not hard-code the wrong number.

| File:Line | Method | #Fields | Fields touched |
|---|---|---|---|
| `manager.go:209` | `Registry` | 1 | registry |
| `manager.go:284` | `RegisterHost` | 4 | eventEmitter, hostCaps, hosts, mu |
| `manager.go:305` | `capabilitiesFor` | 1 | hostCaps |
| `manager.go:314` | `DeliverCommand` | 2 | mu, pluginHosts |
| `manager.go:338` | `BeginServiceDispatch` | 2 | mu, pluginHosts |
| `manager.go:377` | `ConfigureEventEmitter` | 4 | eventEmitter, hosts, luaHost, mu |
| `manager.go:397` | `ConfigureFocusDeps` | 3 | hosts, luaHost, mu |
| `manager.go:419` | `ConfigureReadbackDecryptor` | 3 | hosts, luaHost, mu |
| `manager.go:439` | `ConfigureSettingsDeps` | 3 | hosts, luaHost, mu |
| `manager.go:459` | `DeliverEvent` | 2 | mu, pluginHosts |
| `manager.go:477` | `EmitPluginEvent` | 2 | eventEmitter, mu |
| `manager.go:516` | `Discover` | 1 | pluginsDir |
| `manager.go:602` | `warnUnknownTrustAllowlistEntries` | 1 | trustAllowlist |
| `manager.go:643` | `LoadAll` | **9** | activeByName, aliasCache, aliasSeeder, gracefulDegradation, hosts, luaHost, mu, pluginRepo, retentionDays |
| `manager.go:752` | `seedAliases` | 4 | aliasCache, aliasSeeder, loadedOrder, mu |
| `manager.go:875` | `BuildFocusRedirects` | 1 | loadedOrder |
| `manager.go:907` | `resolveLoadOrder` | 2 | capVocab, registry |
| `manager.go:960` | `unregisterPluginProviders` | 2 | registerProvider, unregisterProvider |
| `manager.go:992` | `computeHashes` | 0 | *(none)* |
| `manager.go:1065` | `loadPlugin` | **13** | activeByName, hosts, inflight, loaded, loadedOrder, luaHost, mu, nameByID, pluginHosts, pluginRepo, policyInstaller, registry, verbRegistry |
| `manager.go:1388` | `discoverAndRegisterAttributes` | 1 | registerProvider |
| `manager.go:1456` | `ListPlugins` | 2 | loaded, mu |
| `manager.go:1472` | `TestLoadPlugin` | 5 | hosts, loaded, luaHost, mu, pluginHosts |
| `manager.go:1525` | `QuerySessionStreams` | 3 | loaded, mu, pluginHosts |
| `manager.go:1614` | `Close` | 6 | hosts, loaded, luaHost, mu, pluginHosts, policyInstaller |
| `manager.go:1664` | `PluginAuditClient` | 2 | mu, pluginHosts |
| `manager.go:1690` | `AuditSubjects` | 2 | loaded, mu |
| `manager.go:1712` | `IsPluginLoaded` | 2 | loaded, mu |
| `manager.go:1721` | `GetLoadedPlugin` | 2 | loaded, mu |
| `manager.go:1728` | `lookupManifest` | 3 | inflight, loaded, mu |
| `manager.go:1749` | `PluginRequestsDecryption` | 0 | *(delegates to lookupManifest)* |
| `manager.go:1768` | `PluginCanReadBack` | 0 | *(delegates to lookupManifest)* |
| `manager.go:1792` | `RegisterPluginCommands` | 2 | loaded, mu |
| `manager.go:1826` | `NameByID` | 2 | mu, nameByID |
| `manager.go:1834` | `IDByName` | 2 | activeByName, mu |
| `manager_unload.go:22` | `UnloadPlugin` | 5 | activeByName, loaded, mu, pluginHosts, policyInstaller |

**Field → method fan-in (the coupling picture):**

| Field | Fan-in | Read/write character |
|---|---|---|
| `mu` | **25** | The single lock over everything |
| `loaded` | 11 | written by loadPlugin/TestLoadPlugin/Close/UnloadPlugin; read by 7 |
| `pluginHosts` | 9 | written by loadPlugin/TestLoadPlugin/Close/UnloadPlugin; read by 5 |
| `hosts` | 9 | written by RegisterHost; read by Configure*/LoadAll/loadPlugin/TestLoadPlugin/Close |
| `luaHost` | 8 | read-only after construction |
| `activeByName` | 4 | written by LoadAll/loadPlugin/UnloadPlugin; read by IDByName |
| `loadedOrder` | 3 | written by loadPlugin; read by seedAliases/BuildFocusRedirects |
| `eventEmitter`, `policyInstaller`, `registry` | 3 each | — |
| `nameByID`, `pluginRepo`, `inflight`, `aliasCache`, `aliasSeeder`, `hostCaps`, `registerProvider` | 2 each | — |
| `pluginsDir`, `capVocab`, `verbRegistry`, `trustAllowlist`, `gracefulDegradation`, `retentionDays`, `unregisterProvider` | 1 each | — |
| `retentionDaysSet` | **0** | Read only in `NewManager` (`manager.go:~/if !m.retentionDaysSet`) — not in any method |

**D-06 verification — the requested answer, in three parts:**

*(a) Which methods write `nameByID` / `activeByName` / `pluginRepo`?*

| Field | Writers | Readers |
|---|---|---|
| `nameByID` | `loadPlugin` (`manager.go:1172-1173` region: `m.nameByID[pluginID] = …`), plus `NewManager` bootstrap | `NameByID` |
| `activeByName` | `loadPlugin`, `LoadAll`, `UnloadPlugin` (`manager_unload.go:24`), plus `NewManager` bootstrap | `IDByName` |
| `pluginRepo` | never written post-construction (set by `WithPluginRepo`) | `LoadAll`, `loadPlugin` |

So the write side is exactly `{loadPlugin, LoadAll, UnloadPlugin}` — all load-time-family — and
the read side is exactly `{NameByID, IDByName}` — the identity API. **D-06's premise is confirmed.**

*(b) Does `m.mu` genuinely guard both the identity fields and the load/runtime fields?*

Yes — one `sync.RWMutex` (`manager.go:89`) covers all mutable state, exactly as the comment at
`manager.go:95-99` states.

*(c) **The critical refinement — is the three-way split therefore risky?** No.*

`loadPlugin` does **not** hold `mu` across identity and runtime mutations. It takes and releases
the lock in **four short, disjoint critical sections**
`[VERIFIED: internal/plugin/manager.go, offsets relative to the func at :1065]`:

| # | Region (abs. line ≈) | Holds | Mutates |
|---|---|---|---|
| 1 | 1125–1137 | Lock/Unlock | `loaded` (dup check), `inflight` (claim) |
| 2 | (deferred) 1139–1141 | Lock/Unlock | `inflight` (release) |
| 3 | 1171–1174 | Lock/Unlock | **`nameByID`, `activeByName`** (identity write) |
| 4 (rollback defer) | 1182–1187 | Lock/Unlock | `nameByID`, `activeByName` (identity rollback) |
| 5 | 1362–1369 | Lock/Unlock | `inflight`, **`loaded`, `pluginHosts`** (commit) |

Sections 3 and 5 are **separate acquisitions**. The lock is released between the identity write
and the runtime commit. Consequently:

- Splitting `mu` into a load lock, a runtime lock, and an identity lock **does not change lock
  ordering**, because no code path currently holds the lock across a boundary. There is no
  nested-acquisition hazard to introduce.
- The interleaving window that already exists (identity populated at §3 but `loaded` not
  populated until §5) is **preserved exactly** by the extraction. This is behavior-preserving
  in the strict sense D-15 requires.

This is the strongest de-risking finding for ARCH-02, and it should be stated in the plan so a
reviewer does not re-litigate lock-ordering safety.

**Methods that span all three proposed units (the genuinely hard cases):**

| Method | Load | Runtime | Identity | Note |
|---|---|---|---|---|
| `loadPlugin` (13 fields) | ✔ | ✔ (`pluginHosts`) | ✔ (`nameByID`,`activeByName`) | **The orchestrator.** Stays in the load unit; calls into runtime + identity units through narrow interfaces. Its four critical sections map 1:1 onto those calls. |
| `LoadAll` (9 fields) | ✔ | — | ✔ (`activeByName`, `pluginRepo`, `retentionDays`) | Load unit; delegates identity + retention sweep. |
| `Close` (6 fields) | ✔ (`hosts`,`luaHost`,`policyInstaller`) | ✔ (`loaded`,`pluginHosts`) | — | Lifecycle teardown — **assign explicitly**; it is neither pure-load nor pure-runtime. |
| `UnloadPlugin` (5 fields) | ✔ (`policyInstaller`) | ✔ (`loaded`,`pluginHosts`) | ✔ (`activeByName`) | Same. Mirrors `loadPlugin` in reverse. |
| `TestLoadPlugin` (5 fields) | ✔ | ✔ | — | See D-08 analysis below. |

`Close` and `UnloadPlugin` are the two methods requiring an explicit planner decision. Everything
else falls cleanly on one side.

### Pattern 2: Consumer-defined narrow interface + constructor injection (D-02)

This is the idiom that makes D-02 cheap and is already used in-repo. `focus.StreamSender` is a
worked example of a consumer declaring exactly what it needs to avoid an import cycle:

```go
// Source: internal/grpc/focus/coordinator.go:22-25 (verbatim comment)
// StreamSender delivers stream subscription updates to the live loop.
// Decouples the coordinator from the concrete SessionStreamRegistry
// type in internal/grpc (avoiding an import cycle).
type StreamSender interface {
```

### Anti-Patterns to Avoid

- **`*CoreServer` / `*Manager` backpointer in an extracted unit.** Explicitly forbidden by D-02.
  It converts the refactor into a file split and fails success criterion 1.
- **Collapsing `m.hosts[TypeLua]` and `m.luaHost` opportunistically.** CONTEXT.md flags this as
  behavior-adjacent. `luaHost` has fan-in 8 and participates in the `TestLoadPlugin` fallback
  (`manager.go:1474-1477`). Treat as an explicit decision or leave alone.
- **Hard-coding "37 methods" in a census meta-test.** The real count is 36 (see above).
- **Assuming `internal/telnet` imports `internal/grpc`.** It does not. See §State of the Art.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Production-only import-direction gate | A custom AST walker skipping `_test.go` | `go/build`'s `bctx.ImportDir(dir, 0).Imports` | `build.Package.Imports` **already** excludes `_test.go` (they land in `TestImports`/`XTestImports`) — the exclusion is free `[VERIFIED: test/meta/world_import_graph_test.go:20-31]` |
| Transitive closure of a package's deps | Manual BFS over `go list` output | `cmd/holomush/gateway_closure_test.go`'s existing helper `transitiveInternalClosure` | Already written and used at `cmd/holomush/gateway_closure_test.go:126` |
| Repo-root discovery in a meta-test | Walking up for `.git` inline | `findRepoRoot(t)` | Already exists at `test/meta/meta_helpers_test.go:33` |
| Session-ownership auth preamble | A new `sessionResolver` type in each unit | `auth.ValidateSessionOwnership(ctx, repo, store, token, sessionID)` | Already a free function; used at 6+ sites `[VERIFIED: internal/grpc/server.go:403-409]` |
| Whole-system plugin-load assertion | A new census | `test/integration/wholesystem/census_test.go` | Already asserts the 9 expected plugins load via `Manager.LoadAll` |

**Key insight:** every mechanism this phase needs already exists in `test/meta/` or
`cmd/holomush/`. D-11's ratchet is composition of two working templates, not invention.

---

## Runtime State Inventory

This is a **refactor** phase, so this section is required. The question: *after every file in the
repo is updated, what runtime systems still hold the old shape?*

| Category | Items Found | Action Required |
|----------|-------------|-----------------|
| **Stored data** | **None.** The refactor changes no schema, no column, no key, no serialized payload. `store.PluginRepo` rows are keyed by plugin name/ULID and are untouched by a Go-side type split. Verified: no `internal/store/migrations/` change is implied by any decision D-01..D-20. | none |
| **Live service config** | **None.** No n8n/Datadog/Tailscale/Cloudflare surface. Plugin manifests (`plugins/*/plugin.yaml`) are **not** edited — D-03 forbids proto changes and the manifest schema is untouched. | none |
| **OS-registered state** | **None.** No task-scheduler / pm2 / systemd registration embeds `CoreServer` or `Manager`. | none |
| **Secrets / env vars** | **None.** No env var or SOPS key names any Go type being moved. `HOLOMUSH_REQUIRE_PLUGINS` (`Taskfile.yaml`, `test:int` env) refers to plugin *loading*, not to the `Manager` type name — unaffected. | none |
| **Build artifacts / generated code** | **Two.** (1) `internal/grpc/mocks/mock_CoreServerOption.go:9` is a **mockery-generated** mock of `CoreServerOption`. If the option type's signature changes, `mockery` must be re-run and the result committed. D-04 keeps the signature, so this should be a no-op — **verify, don't assume**. (2) `pkg/proto/**` is NOT regenerated: D-03 forbids proto edits, so `task proto` / `task web:generate` must NOT appear in this phase. A proto diff is a scope error. | Re-run `mockery` only if `CoreServerOption` changes; assert no `api/proto/**` or `pkg/proto/**` diff in the PR |

**Nothing found in a category is stated explicitly above rather than left blank.**

---

## The three D-09 seams — exact extraction cost

### Seam 1: `internal/grpc/focus` types → neutral package

**Every importing file and every symbol crossed** `[VERIFIED: rg -oN 'focus\.[A-Z]\w*' per file]`:

| `internal/plugin` file | Symbols used |
|---|---|
| `internal/plugin/manager.go` | `focus.Coordinator` |
| `internal/plugin/host.go` | `focus.Coordinator` |
| `internal/plugin/goplugin/host.go` | `focus.Coordinator` |
| `internal/plugin/hostcap/capabilities.go` | `focus.Coordinator` |
| `internal/plugin/lua/host.go` | `focus.Coordinator` |
| `internal/plugin/lua/focus_ops_adapter.go` | `focus.Coordinator` |
| `internal/plugin/lua/hostcap_adapter.go` | `focus.Coordinator`, `focus.RestorePlan`, `focus.SetConnectionFocusResult`, `focus.AutoFocusOnJoinResponse`, `focus.AutoFocusFailure` |

**Answer to "does the neutral package need behavior or only types?" — TYPES ONLY.**
Six of seven files need only the `Coordinator` interface. The seventh adds four result structs.
No function, no method implementation, no constructor crosses the seam.

**What the neutral package must contain (5 declarations):**

| Symbol | Current location |
|---|---|
| `Coordinator` (interface, 9 methods) | `internal/grpc/focus/coordinator.go:44` |
| `RestorePlan` (struct) | `internal/grpc/focus/coordinator.go:108` |
| `SetConnectionFocusResult` (struct) | `internal/grpc/focus/coordinator.go:116` |
| `AutoFocusOnJoinResponse` (struct) | `internal/grpc/focus/auto_focus_on_join.go:19` |
| `AutoFocusFailure` (struct) | `internal/grpc/focus/auto_focus_on_join.go:43` |

**Neutral package's own dependency set:** `context`, `github.com/oklog/ulid/v2`, and
`internal/session` (for `session.FocusKey` and `session.LeaveByTargetResult` in the `Coordinator`
signature). **`internal/session` is safe** — it has **no** production import of `internal/grpc`
`[VERIFIED: rg -n 'holomush/internal/grpc' internal/session/*.go --glob '!*_test.go' → no match]`.
The many `grpc/focus` mentions in `internal/session/session.go:43,64,92,98,100,118,136,162` are
**comments only**, describing the compile-time "only grpc/focus may call this" enforcement — not
imports. (I checked this specifically because a naive `rg -l` makes it look like a cycle.)

**Does extraction actually break the cycle? YES — completely.**
`internal/plugin` → `internal/grpc` has **exactly 7 production import edges, and all 7 are
`internal/grpc/focus`** `[VERIFIED: rg -IN 'holomush/internal/grpc' internal/plugin/ --glob '!*_test.go' | sort | uniq -c → "7 .../internal/grpc/focus"]`.
No residual edge remains after the move. This is a complete cut, not a partial one.

### Seam 2: `internal/eventbus/authguard` → `internal/plugin`

**The entire file is 29 lines.** `[VERIFIED: internal/eventbus/authguard/adapter_manifest.go]`
It uses exactly **three** symbols from `internal/plugin`:

| Symbol | Use |
|---|---|
| `*plugins.Manager` | struct field + constructor param |
| `Manager.PluginRequestsDecryption(pluginName, eventType string) bool` | `adapter_manifest.go:22` |
| `Manager.PluginCanReadBack(pluginName, eventType string) bool` | `adapter_manifest.go:28` |

**What the neutral contract must contain:** a single two-method interface. The `ManifestLookup`
interface **already exists in `authguard`** — `NewPluginManifestLookup` returns it
(`adapter_manifest.go:14`). So the "neutral manifest contract" may already be satisfiable by
inverting the dependency: have `internal/plugin` (or a tiny neutral package) declare/satisfy the
interface, and delete the adapter, rather than creating anything new.

**Cheapness note:** both methods are **pure delegation with zero direct field access** — they
call `m.lookupManifest(name)` and read the manifest `[VERIFIED: internal/plugin/manager.go:1749-1778]`.
Nothing about the `Manager`'s internal state is exposed by the contract.

**Does extraction break the edge? YES — completely.**
`internal/eventbus` → `internal/plugin` has **exactly one** production import edge, this file
`[VERIFIED: rg -lN 'holomush/internal/plugin"' internal/eventbus/ --glob '!*_test.go' → adapter_manifest.go only]`.

### Seam 3: `TranslateSubscribeErr` — **ALREADY RESOLVED, NO WORK REQUIRED**

CONTEXT.md D-09 seam 3 (and its `<specifics>` follow-up) describe a state of the world that no
longer exists. Verified facts:

| Claim in CONTEXT.md | Actual state |
|---|---|
| "`TranslateSubscribeErr` [lives in `internal/grpc`]" | It lives in **`internal/grpcclient/client.go:133`** `[VERIFIED]` |
| "`internal/telnet/gateway_handler.go:29` imports `internal/grpc`" | Line 29 is `"github.com/holomush/holomush/internal/grpcclient"`. `internal/telnet` has **no** import of `internal/grpc` `[VERIFIED: full import block, gateway_handler.go:8-34]` |
| "used at `:718`" | The call is at **`gateway_handler.go:720`**, and it reads `grpcclient.TranslateSubscribeErr(recvErr)` `[VERIFIED]` |
| `<specifics>`: "Extracting … lets `internal/grpc` be added to `gateway_imports_test.go`'s `forbidden` list" | `"github.com/holomush/holomush/internal/grpc"` is **already** the last entry in `gatewayForbiddenPackages` `[VERIFIED: cmd/holomush/gateway_imports_test.go:162]` |

The in-repo comment states the cause directly: *"internal/grpc is the one entry that reaches the
DB (a 41-package closure through world/access/command/store) and **was removed from the gateway
by 07-01's internal/grpcclient extraction**"* `[VERIFIED: cmd/holomush/gateway_imports_test.go:141-146]`.

**Planning consequence:** Wave 0 has **two** seams, not three. LOW-6 is closed. Success criterion
3's gateway-boundary clause is *already* satisfied on this axis and needs only a
no-regression check, not new work. A plan that budgets a task for seam 3 is planning work that
does not exist.

---

## Common Pitfalls

### Pitfall 1: `TestLoadPlugin` has a caller outside `package plugins` — `export_test.go` is blocked *today*

**What goes wrong:** D-08 offers "build tag or `export_test.go`". Choosing `export_test.go` fails
to compile.

**Evidence:** `TestLoadPlugin` is called from **`package authguard_test`**
`[VERIFIED: internal/eventbus/authguard/adapter_manifest_test.go:1 declares 'package authguard_test'; calls at :30 and :50]`.
A `_test.go` symbol in `internal/plugin` is invisible to any other package's test binary. Full
caller inventory:

| Caller file | Package | Blocks `export_test.go`? |
|---|---|---|
| `internal/plugin/manager_test.go:1581,2068` | `plugins` / `plugins_test` | No |
| `internal/plugin/manager_audit_test.go:50,51,78,107` | same | No |
| `internal/plugin/identity_registry_test.go:265` | same | No |
| `internal/plugin/crypto_manifest_lookup_test.go:61` | same | No |
| **`internal/eventbus/authguard/adapter_manifest_test.go:30,50`** | **`authguard_test`** | **YES** |

**Why the build-tag option is also expensive:** Go has **no** build tag automatically set during
`go test`. A custom tag (e.g. `//go:build testhooks`) must be threaded through *every* test
invocation — `task test`, `task test:int` (which runs `./...` under `-tags=integration`
`[VERIFIED: Taskfile.yaml:188]`), `task test:cover`, and golangci-lint's build context — or the
authguard test stops compiling. That is a wide, error-prone blast radius for a cosmetic win.

**How to avoid — the sequencing insight:** **Wave 0 seam 2 likely removes the only blocking
caller.** That authguard test exists to test `NewPluginManifestLookup`, the adapter seam 2
deletes. Once `authguard` no longer imports `internal/plugin`, the test is rewritten against a
fake `ManifestLookup` and no longer needs a real `Manager` — at which point `export_test.go`
becomes viable with zero tag plumbing. **Recommendation: order seam 2 before the D-08 decision,
and make D-08 conditional on the post-seam-2 caller set.** If for any reason the authguard caller
survives, keep `TestLoadPlugin` where it is rather than pay the tag cost; note it as accepted debt.

### Pitfall 2: The harness is meta-test-guarded against constructing a `Manager` directly

**What goes wrong:** a plan rewires `internal/testsupport/integrationtest` to call
`plugins.NewManager(...)` and trips an existing invariant test.

**Evidence:** `TestWithInTreePluginsReusesSubsystem` reads `plugins.go` as **text** and asserts
`require.NotContains(t, body, "plugins.NewManager(")` — enforcing INV-PLUGIN-18
`[VERIFIED: internal/testsupport/integrationtest/invariants_test.go:22-30]`. The harness must
keep going through `pluginsetup.NewPluginSubsystem(`.

**How to avoid:** the harness's plugin path must remain `setup.PluginSubsystem`-mediated. D-07
(keep `NewManager` + all 11 options) satisfies this automatically.

### Pitfall 3: `mockery`-generated `CoreServerOption` mock can silently go stale

**Evidence:** `internal/grpc/mocks/mock_CoreServerOption.go:9` imports `internal/grpc`. CI runs a
stale-generated-code check for proto; treat the mock the same way.

**How to avoid:** if `CoreServerOption`'s signature changes, run `mockery` and commit. D-04 says
it should not change — verify with a diff rather than assuming.

### Pitfall 4: `NewManager` fails without `WithVerbRegistry` — landmine CONFIRMED still true

**Evidence:** `NewManager` ends with `if m.verbRegistry == nil { return nil, ErrMissingVerbRegistry }`
`[VERIFIED: internal/plugin/manager.go, tail of NewManager]`; `ErrMissingVerbRegistry` is declared
at `manager.go:69-71` with code `MISSING_VERB_REGISTRY` (INV-EVENTBUS-11). Every in-repo test
constructor passes `plugins.WithVerbRegistry(core.NewVerbRegistry())` — e.g.
`internal/plugin/manager_routing_test.go:140`, `internal/eventbus/authguard/adapter_manifest_test.go:28`.

**Note the drift:** `.claude/rules/references/plan-review-learnings.md` cites this as INV-GW-10 at
`manager.go:181-201`. The **behavior** is unchanged but the **invariant id is INV-EVENTBUS-11**
and the guard is at the end of `NewManager`, not at `:181-201`. Cite the code, not the memo.

### Pitfall 5: `TestLoadPlugin` silently no-ops without a registered host — landmine CONFIRMED

**Evidence:** `manager.go:1473-1491` — it looks up `m.hosts[manifest.Type]`, falls back to
`m.luaHost` for `TypeLua`, and only sets `m.pluginHosts[name]` `if ok`. With neither configured
it still inserts into `m.loaded` but **not** `m.pluginHosts`. Verbatim, `manager.go:1489-1491`:
`m.loaded[name] = &DiscoveredPlugin{Manifest: manifest}` then `if ok { m.pluginHosts[name] = host }`.

### Pitfall 6: `Manager.loadManifest` does not exist — landmine CONFIRMED

`[VERIFIED: rg -n 'func .*loadManifest' --type go → no match]`. Manifest parsing is inlined in
`Discover`. A plan that "modifies `loadManifest`" is fabricating. The nearest real symbol is
`lookupManifest` (`manager.go:1728`), which is a *cache read*, not a parser.

### Pitfall 7: `Manager.UnloadPlugin` DOES exist — landmine is **STALE**, correct the record

**Evidence:** `internal/plugin/manager_unload.go:22`,
`func (m *Manager) UnloadPlugin(ctx context.Context, name string) error`. It is a real, exported,
documented method with four tests
(`internal/plugin/identity_registry_test.go:260,284,341,364`).

Notably it already implements the idempotency shape that the plan-review-learnings file
*recommended* for a hypothetical future version — cache cleanup runs first and unconditionally,
before the `if !hostLoaded { return nil }` early return (`manager_unload.go:24-36`). Someone
implemented the advice; the memo was never updated.

**Action for the planner:** `UnloadPlugin` is one of the four cross-cutting `Manager` methods
(load + runtime + identity) and needs an explicit unit assignment. Also recommend refreshing
`.claude/rules/references/plan-review-learnings.md` at phase close so the stale note stops
misleading future reviews.

### Pitfall 8: Method-count drift in a census meta-test

`Manager` has **36** methods, not the 37 in CONTEXT.md. `CoreServer` has **39**, matching. If the
census asserts counts, derive them or use the verified numbers.

---

## Code Examples

### The production-only import gate (reuse verbatim shape)

```go
// Source: test/meta/world_import_graph_test.go:20-31 (verbatim)
// worldPkgImports returns the PRODUCTION import list of the package rooted at rel
// (relative to the repo root). build.Package.Imports excludes _test.go files
// (those are TestImports/XTestImports) — so this guards production imports only,
// per round-3 MEDIUM (test files may legitimately hold concrete fixtures).
func worldPkgImports(t *testing.T, root, rel string) []string {
	t.Helper()
	bctx := build.Default
	bctx.CgoEnabled = false
	pkg, err := bctx.ImportDir(filepath.Join(root, rel), 0)
	require.NoErrorf(t, err, "import package %s", rel)
	return pkg.Imports
}
```

### The forbidden-edge assertion (reuse verbatim shape)

```go
// Source: test/meta/world_import_graph_test.go:56-62 (verbatim)
	for _, e := range forbidden {
		imports := worldPkgImports(t, root, e.fromRel)
		toPath := modulePath + "/" + e.toRel
		require.NotContainsf(t, imports, toPath,
			"forbidden import edge: %s must NOT import %s (production imports only)", e.fromRel, e.toRel)
	}
```

### Proposed Phase-8 ratchet — direction half

Grounded directly in the two templates above. The forbidden set follows from §Seam analysis:

```go
// Verifies: INV-<SCOPE>-N   (only if D-14's binding is genuinely earned)
func TestPhase8ImportDirection(t *testing.T) {
	root := findRepoRoot(t) // test/meta/meta_helpers_test.go:33

	forbidden := []struct{ fromRel, toRel string }{
		// D-09 seam 1: plugin must never again import up into the grpc tree.
		{"internal/plugin", "internal/grpc/focus"},
		{"internal/plugin/lua", "internal/grpc/focus"},
		{"internal/plugin/goplugin", "internal/grpc/focus"},
		{"internal/plugin/hostcap", "internal/grpc/focus"},
		// D-09 seam 2: eventbus must never again depend on plugin.
		{"internal/eventbus/authguard", "internal/plugin"},
	}
	for _, e := range forbidden {
		imports := pkgProductionImports(t, root, e.fromRel)
		require.NotContainsf(t, imports, modulePath+"/"+e.toRel,
			"forbidden import edge: %s must NOT import %s (production imports only)", e.fromRel, e.toRel)
	}
}
```

### Proposed Phase-8 ratchet — size half (D-12)

No in-repo precedent measures LoC, so this half is new. Keep it dead simple and derive the
ceilings from the post-split actuals (Claude's discretion per CONTEXT.md):

```go
func TestPhase8SizeCeilings(t *testing.T) {
	root := findRepoRoot(t)
	// Ceilings are set from the POST-SPLIT actual + modest headroom, not a round number.
	ceilings := map[string]int{
		"internal/grpc/server.go":       0, // FILL from actual
		"internal/plugin/manager.go":    0, // FILL from actual
		// ... one entry per extracted unit
	}
	for rel, max := range ceilings {
		b, err := os.ReadFile(filepath.Join(root, rel))
		require.NoError(t, err)
		n := bytes.Count(b, []byte("\n"))
		require.LessOrEqualf(t, n, max,
			"%s is %d lines, ceiling %d — decomposed units must not regrow (ARCH-01/02, #4674: "+
				"server.go 1331→1891 and manager.go 1222→1869 regrew with no gate)", rel, n, max)
	}
}
```

**Baseline for the ceiling conversation** `[VERIFIED: wc -l]`:

| File | #4674 baseline | Today | Growth |
|---|---|---|---|
| `internal/grpc/server.go` | 1331 | **1891** | +42% |
| `internal/plugin/manager.go` | 1222 | **1869** | +53% |
| `internal/grpc/auth_handlers.go` | — | 960 | (the existing split-off) |

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|---|---|---|---|
| `internal/telnet` imports `internal/grpc` for `TranslateSubscribeErr` (LOW-6) | `TranslateSubscribeErr` lives in `internal/grpcclient`; telnet imports that | Phase 7, task 07-01 | **D-09 seam 3 is already done.** Wave 0 = 2 seams |
| `internal/grpc` absent from the gateway forbidden list | Present as the last entry | Phase 7 | The `<specifics>` follow-up is already banked |
| `Manager.UnloadPlugin` does not exist | Exists at `manager_unload.go:22` with 4 tests | (after the memo was written) | Landmine memo is stale; method needs a unit assignment |
| `NewManager` guard cited as INV-GW-10 at `manager.go:181-201` | Guard is INV-**EVENTBUS**-11 at the tail of `NewManager` | — | Behavior unchanged; cite code not memo |

**Deprecated/outdated:**
- `.claude/rules/references/plan-review-learnings.md`'s "`Manager.UnloadPlugin` does NOT exist"
  note — **factually wrong today**. Refresh at phase close.
- `.planning/codebase/CONCERNS.md` LoC figures (`manager.go` listed 1838; actual 1869).

---

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `testify` (unit/meta); Ginkgo v2 + Gomega (integration) |
| Config file | `Taskfile.yaml` (`test`, `test:int`, `test:cover`); `.golangci.yaml` for lint |
| Quick run command | `task test -- ./internal/grpc/... ./internal/plugin/...` |
| Full suite command | `task test` then **`task test:int`** (mandatory, D-17) |

### Phase Requirements → Test Map

Success criteria come from `.planning/ROADMAP.md` § Phase 8 (line 213).

| Criterion | Behavior | Test Type | Automated Command | File Exists? |
|---|---|---|---|---|
| **SC1** — CoreServer split into independently-testable units | Each extracted unit is constructible + exercisable with ONLY its own collaborators (no `*CoreServer`, no harness) | unit | `task test -- ./internal/grpc/...` | ❌ Wave A — one new test per unit (D-16, and the `<specifics>` "testable as a property" note) |
| **SC1** — behavior preserved | Full integration + whole-system suites green, **zero assertion edits** | integration | `task test:int` | ✅ existing (see suite map below) |
| **SC2** — manager decomposed | Each of load / runtime / identity constructible standalone | unit | `task test -- ./internal/plugin/...` | ❌ Wave B |
| **SC2** — plugin load/lifecycle unchanged | Whole-system plugin census stays green | integration | `task test:int -- ./test/integration/wholesystem/...` | ✅ `test/integration/wholesystem/census_test.go` |
| **SC3** — size ceilings | Decomposed units do not regrow | meta | `task test -- -run TestPhase8SizeCeilings ./test/meta/` | ❌ Wave C |
| **SC3** — import direction | The 2 unwound edges never return | meta | `task test -- -run TestPhase8ImportDirection ./test/meta/` | ❌ Wave C |
| **SC3** — no new gateway-boundary violations | Gateway closure holds | meta | `task test -- -run TestGatewayImportsAreOnlyProtocolTranslation ./cmd/holomush/` | ✅ `cmd/holomush/gateway_imports_test.go` (INV-EVENTBUS-1) |
| **SC3** — no plugin-runtime-symmetry violation (D-20) | Lua and binary reach the same gates | integration | `task test:int -- ./test/integration/pluginparity/...` | ✅ `test/integration/pluginparity/` (7 specs) |
| **D-15** — zero assertion churn | `test/integration/**` untouched | manual/CI | `git diff --stat origin/main...HEAD -- test/integration/` | ❌ add as a plan step (per `<specifics>`) |

### Which suites actually cover these paths (research item 7)

**CoreServer RPC coverage** `[VERIFIED: rg -lN 'CoreServiceClient|coreServer|CoreClient' test/integration/]`:

| Suite | What it exercises |
|---|---|
| `test/integration/auth/auth_suite_test.go:164` | Constructs a real `CoreServer` via `NewCoreServer` — the auth RPC group |
| `test/integration/auth/multi_tab_test.go` | `Subscribe` + session/`TranslateSubscribeErr` round-trip (`:589-596`) |
| `test/integration/auth/core_client_shim_test.go` | Client-side shim over the Core service |
| `test/integration/phase1_5_test.go:270,341,503` | Three `NewCoreServer` constructions — command execution path |
| `test/integration/presence/reattach_presence_test.go` | Presence/reattach across `Disconnect` + `SelectCharacter` |
| `test/integration/stream_history/`, `test/integration/list_session_streams/`, `test/integration/streams/` | The current-state query cluster |
| `test/integration/command/`, `test/integration/session/`, `test/integration/scenes/` | Command + lifecycle + focus clusters |

**Plugin load/lifecycle coverage:**

| Suite | What it exercises |
|---|---|
| `test/integration/wholesystem/census_test.go` | **The census.** `integrationtest.Start(suiteT, WithInTreePlugins())` → `srv.PluginManager().ListPlugins()` must contain the 9 expected manifest names (`:25-29`), loaded via `Manager.LoadAll` |
| `test/integration/wholesystem/abac_test.go` | Whole-system ABAC over loaded plugins |
| `test/integration/plugin/` (15 files) | `binary_plugin_test.go`, `alias_seeder_test.go`, `verb_registration_test.go`, `plugin_migration_test.go`, `actor_authentication_test.go`, `schema_isolation_test.go`, … — load-time unit's behavior |
| `test/integration/pluginparity/` (7 files) | **D-20's symmetry proof** — `parity_test.go`, `lua_emit_test.go`, `gated_endpoints_test.go`, `least_privilege_gate_test.go`, `nonscoped_capability_deny_test.go` |
| `test/integration/plugincrypto/` | `PluginRequestsDecryption` / `PluginCanReadBack` paths (D-19's crypto-reviewer surface) |

**"Passes unchanged" concretely means:** `task test:int` runs `./...` under `-tags=integration`
`[VERIFIED: Taskfile.yaml:184-190]`, so *every* suite above compiles and runs. An assertion edit
in any of them is the D-15 review-blocking signal.

### Sampling Rate

- **Per task commit:** `task test -- ./internal/grpc/... ./internal/plugin/...` + `task lint`
- **Per wave merge:** **`task test:int`** — non-negotiable (D-17); a wave that only ran `task test`
  has not been verified, because `task test` does not compile `//go:build integration` files
- **Phase gate:** `task pr-prep` green inline before push; full `task test:int` green

### Wave 0 Gaps

- [ ] `test/meta/phase8_ratchet_test.go` (or similar) — SC3, both halves. New file.
- [ ] One separately-testable unit test per extracted CoreServer unit — SC1. New files.
- [ ] One separately-testable unit test per extracted Manager unit — SC2. New files.
- [ ] A plan step recording `git diff --stat origin/main...HEAD -- test/integration/` — D-15.
- [ ] Framework install: **none needed** — testify, Ginkgo, Gomega all present.

---

## Security Domain

`security_enforcement` is not disabled in `.planning/config.json`, so this section is included.
This is a **behavior-preserving refactor**: it introduces no new trust boundary, no new input,
no new endpoint. The security question is entirely *"does moving code weaken an existing gate?"*

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|------------------|
| V1 Architecture | **yes** | The two D-09 seams change the import graph; the ratchet (D-12) is the control preventing re-tangling |
| V2 Authentication | **yes (preservation only)** | `auth.ValidateSessionOwnership` is the session-ownership gate invoked by 15 CoreServer methods. It MUST remain on every path it is on today — moving a method must not drop the call |
| V3 Session Management | **yes (preservation only)** | `Disconnect` / `recomputeSessionLiveness` / session liveness semantics unchanged |
| V4 Access Control | **yes (preservation only)** | `accessEngine` (ABAC, default-deny) is consumed by `ListAllCharacters`, `ListFocusPresence`, `QueryStreamHistory`. D-19: `abac-reviewer` fires only if `internal/access/` is edited — the split rewires the *field*, it should not edit that tree. **Verify, don't assume.** |
| V5 Input Validation | no (new) | No new input surface; no proto change (D-03) |
| V6 Cryptography | **yes (preservation only)** | `PluginRequestsDecryption` / `PluginCanReadBack` are crypto manifest gates moving to the runtime unit → `crypto-reviewer` MUST run (D-19) |

### Known Threat Patterns for this refactor

| Pattern | STRIDE | Standard Mitigation |
|---|---|---|
| A moved RPC method silently loses its `auth.ValidateSessionOwnership` preamble | Elevation of Privilege | Move method bodies **verbatim**; the integration auth suite (`test/integration/auth/`) is the detector; D-15's zero-assertion-edit rule makes a silent drop visible as a *failure*, not a diff |
| A moved ABAC check loses its `accessEngine` wiring (nil engine) | Elevation of Privilege | `accessEngine` nil ⇒ public stream reads denied (fail-closed, per `server.go:191-193` comment). Fail-closed default limits blast radius; `test/integration/access/` covers it |
| The plugin gate moves onto one runtime's path only | Elevation of Privilege / Tampering | D-20 hard constraint; `test/integration/pluginparity/` (7 specs) is the detector; `.claude/rules/plugin-runtime-symmetry.md` |
| Splitting `m.mu` introduces a TOCTOU between identity and runtime state | Tampering | **Mitigated by construction** — the existing code already releases the lock between those mutations (§Pattern 1(c)), so the extraction preserves the current window exactly rather than widening it |
| Splitting `m.mu` introduces a lock-ordering deadlock | Denial of Service | Same finding: no path holds the lock across a boundary today, so no nested acquisition is introduced. `RACE=-race task test:int` is the empirical check |
| A crypto manifest gate is weakened while relocating | Information Disclosure | `crypto-reviewer` gate (D-19) + `test/integration/plugincrypto/` |

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|---|---|---|
| A1 | Rewriting `adapter_manifest_test.go` against a fake `ManifestLookup` during seam 2 removes the last cross-package `TestLoadPlugin` caller, unblocking `export_test.go` | Pitfall 1 | If the rewritten test still needs a real `Manager`, D-08 falls back to accepted debt (keep as-is) rather than paying the build-tag cost. **Low risk** — the test's subject is the adapter, which seam 2 deletes. Planner should re-check the caller set after seam 2 lands rather than pre-committing. |
| A2 | The proposed cluster boundaries in §Pattern 1 are the right *grouping*; the matrix is fact, the clustering is judgment | Architecture Patterns | The clusters follow D-01's named groups exactly, so divergence risk is low. The *dependency sets* per cluster are computed, not assumed. |
| A3 | `mockery` regeneration is a no-op because `CoreServerOption`'s signature is unchanged (D-04) | Pitfall 3 / Runtime State | If the signature does change, a stale committed mock could fail CI. Cheap to verify with a diff. |
| A4 | No `internal/store/migrations/` change is implied | Runtime State Inventory | Derived from "no schema/serialization change in any of D-01..D-20", not from an exhaustive migration audit. Very low risk for a type-split refactor. |

Everything else in this document is `[VERIFIED]` against the working tree.

---

## Open Questions

1. **Where does `Close` belong (load vs runtime)?**
   - What we know: it touches `hosts`, `luaHost`, `policyInstaller` (load-family) *and*
     `loaded`, `pluginHosts` (runtime-family) — `manager.go:1614`.
   - What's unclear: whether teardown is conceptually load-time inverse or a fourth concern.
   - Recommendation: assign to the **load unit** (it is `LoadAll`'s inverse) and have it call the
     runtime unit's own shutdown through a narrow interface. Same treatment for `UnloadPlugin`.
     Make this an explicit, written decision in the plan — it is one of only two real judgment
     calls in ARCH-02.

2. **Does the ratchet earn an invariant binding (D-14)?**
   - What we know: `.claude/rules/invariants.md` wants durable guarantees, not line counts;
     D-14 explicitly says *"Do not fabricate a binding on a test that only counts lines."*
   - Recommendation: bind the **import-direction** half (a durable architectural guarantee, the
     same shape as INV-WORLD-4). Leave the **size-ceiling** half unbound — it is a regrowth
     ratchet, not a system invariant. If bound, register in `docs/architecture/invariants.yaml`
     and run `go run ./cmd/inv-render`; scope is likely `PLUGIN` or a new `ARCH`.

3. **Does `m.hosts[TypeLua]` vs `m.luaHost` get collapsed?**
   - What we know: `luaHost` has fan-in 8 and participates in the `TestLoadPlugin` fallback.
   - Recommendation: **do not collapse in this phase.** CONTEXT.md flags it behavior-adjacent;
     a behavior-preserving phase is the wrong place. File as a follow-up issue.

---

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | everything | ✓ | per `go.mod` | — |
| `task` (go-task) | all build/test/lint | ✓ | `Taskfile.yaml` present | none — mandatory per CLAUDE.md |
| Docker | `task test:int` (testcontainers: Postgres) | **assumed ✓** | — | none — D-17 makes `test:int` mandatory |
| Host-arch plugin binaries | `task test:int` | ✓ (auto) | `task plugin:build:host` runs as a `test:int` pre-step `[VERIFIED: Taskfile.yaml:181-183]` | — |
| `mockery` | only if `CoreServerOption` changes | assumed ✓ | `.mockery.yaml` present | skip if signature unchanged |

**Missing dependencies with no fallback:** Docker, if unavailable, blocks `task test:int` and
therefore blocks the phase's primary behavior-preservation proof. The planner should confirm
Docker availability before Wave A/B rather than discovering it at the wave gate.

**Missing dependencies with fallback:** none.

---

## Sources

### Primary (HIGH confidence) — in-repo, verified this session

- `internal/grpc/server.go:154-232` (struct), `:352` (`NewCoreServer`), all 39 method sites
- `internal/plugin/manager.go:73-105` (struct), `NewManager`, all 35 method sites
- `internal/plugin/manager_unload.go:22` (`UnloadPlugin`)
- `internal/grpc/focus/coordinator.go:44,108,116`; `auto_focus_on_join.go:19,43`
- `internal/eventbus/authguard/adapter_manifest.go` (whole file, 29 lines) + `adapter_manifest_test.go:1,28,30,48,50`
- `internal/grpcclient/client.go:118,133,150`; `internal/telnet/gateway_handler.go:8-34,680,694,720`
- `cmd/holomush/gateway_imports_test.go:141-163`
- `test/meta/world_import_graph_test.go` (whole file); `test/meta/plugin_host_capability_decomp_test.go:1-52`; `test/meta/meta_helpers_test.go:33`
- `test/integration/wholesystem/census_test.go:1-45`
- `internal/testsupport/integrationtest/invariants_test.go:22-30`
- `Taskfile.yaml:160-208` (`test:int` definition and tags)
- `.golangci.yaml` (linter enable list; complexity linters absent)
- Method→field matrices: computed by brace-balanced body extraction over all non-test `.go` files in `internal/grpc` and `internal/plugin`

### Secondary (MEDIUM confidence)

- `.planning/phases/08-god-object-decomposition/08-CONTEXT.md` — the 20 locked decisions (authoritative for *intent*; three of its factual citations are corrected above)
- `.claude/rules/references/plan-review-learnings.md` — landmine list (4 checked: 3 confirmed, 1 stale)
- `.planning/REQUIREMENTS.md:35-36` — ARCH-01/ARCH-02 text

### Tertiary (LOW confidence)

- None. No web search or external documentation was used; this phase required none.

---

## Metadata

**Confidence breakdown:**
- Method→field matrices: **HIGH** — computed mechanically from source, not read by eye
- Seam analysis: **HIGH** — edge counts verified in both directions with explicit negative checks
- Landmine corrections: **HIGH** — each confirmed or refuted against current source
- Lock-structure safety finding: **HIGH** — read the actual critical sections in `loadPlugin`
- Cluster *grouping* (as opposed to dependency sets): **MEDIUM** — follows D-01's named groups; judgment, flagged as A2
- Ratchet shape: **HIGH** for the direction half (two working templates); **MEDIUM** for the size half (no in-repo precedent; new code)

**Research date:** 2026-07-19
**Valid until:** ~30 days, but **invalidated immediately by any merge touching `internal/grpc`
or `internal/plugin`** — line numbers in the matrices will drift. Re-run the matrix computation
if `main` moves before planning.
