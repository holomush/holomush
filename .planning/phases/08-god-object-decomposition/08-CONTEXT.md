# Phase 8: God-Object Decomposition - Context

**Gathered:** 2026-07-19
**Status:** Ready for planning

<domain>
## Phase Boundary

Decompose the two named god objects — `internal/grpc/server.go` (`CoreServer`, 1891 LoC,
39 methods across 8 files, ~30 collaborator fields) and `internal/plugin/manager.go`
(`Manager`, 1869 LoC, **36** methods — 35 in `manager.go` plus `UnloadPlugin` in
`manager_unload.go` — and 11 `ManagerOption`s) — into cohesive, separately-testable
units **with no behavior change**, and pin the result against regrowth.

This is ARCH-01 + ARCH-02 only. It is a **behavior-preserving structural refactor**: no new
capabilities, no proto changes, no RPC additions/renames/removals, no semantic changes to
plugin load or lifecycle.

The one piece of *enabling* work admitted alongside the split is arch-review **MEDIUM-4**'s
bidirectional-coupling unwind — Phase 7 explicitly deferred it here, and splitting a god
object whose subpackages point both ways relocates the mess instead of settling it.

**Not in this phase:** `internal/core`'s grab-bag decomposition, `goplugin/host.go`,
`hostcap/servers.go`, the eventbus error-model unification, or any of the other #4674
basket items that are not the two named god objects. See `<deferred>`.

</domain>

<decisions>
## Implementation Decisions

### ARCH-01 — CoreServer decomposition

- **D-01: Split by RPC group into collaborator-scoped units behind a thin facade.**
  Continue the precedent already set in this package: `auth_handlers.go` (16 `CoreServer`
  methods) was split off previously and works. Extend that line to the remaining groups —
  subscribe/stream delivery (`Subscribe`, `runSubscribeLoop`, `dispatchDelivery`,
  `applyFilterCtrl`, `makeFilterUpdater`, `computeInitialFilters`, `toSubject`,
  `toProtoSubscribeResponse`), command execution (`HandleCommand`, `executeCommand`,
  `executeViaDispatcher`, `emitCommandResponse`), session lifecycle (`Disconnect`,
  `recomputeSessionLiveness`, `runDisconnectHooks`), and current-state queries
  (`GetCommandHistory`, `QueryStreamHistory`, `ListFocusPresence`, `ListSessionStreams`,
  `ListAvailableCommands`, `RefreshConnection`, `SessionIdentity`).

- **D-02: Each unit takes ONLY its own collaborators, never a parent backpointer.**
  Constructor-injected; no `*CoreServer` field on any extracted unit.
  This is the actual bar in success criterion 1
  ("separately-testable"). A unit that reaches back into the parent struct is a file split,
  not a decomposition, and will not satisfy the criterion. The ~30-field struct is the
  symptom; narrow per-unit dependency sets are the cure.

- **D-03: The proto service contract is the fixed facade.** `CoreServer` keeps implementing
  `corev1.CoreServiceServer` and its exported method set stays byte-identical pre/post. No
  `api/proto/**` edits, therefore no `task proto` / `task web:generate` regeneration in this
  phase. A proto diff in this phase's PR is a scope error.

- **D-04: `NewCoreServer` + the `CoreServerOption` set stay the public wiring surface.**
  Options route into sub-unit constructors internally. Existing callers — `cmd/holomush/sub_grpc.go`
  and `internal/testsupport/integrationtest/harness.go` — must not need semantic rewiring.
  (Compile-level adjustments are fine; a caller having to learn a new wiring *concept* means
  the facade leaked.)

### ARCH-02 — plugin/manager decomposition

- **D-05: Split along the load-time-wiring vs runtime-delivery line.**
  That axis is named in #4674 and MEDIUM-4 — adopt it rather than inventing a new one.
  - **Load-time:** `Discover`, `computeHashes`, `resolveLoadOrder`, `loadPlugin`, `LoadAll`,
    `seedAliases`, `discoverAndRegisterAttributes`, `unregisterPluginProviders`,
    `warnUnknownTrustAllowlistEntries`, `BuildFocusRedirects`, `RegisterPluginCommands`,
    the retention sweep.
  - **Runtime-delivery:** `DeliverEvent`, `DeliverCommand`, `EmitPluginEvent`,
    `BeginServiceDispatch`, `QuerySessionStreams`, and the read-side lookups
    (`IsPluginLoaded`, `GetLoadedPlugin`, `lookupManifest`, `PluginRequestsDecryption`,
    `PluginCanReadBack`, `AuditSubjects`, `PluginAuditClient`).

- **D-06: The identity registry is a THIRD unit, not half of either.** `nameByID` /
  `activeByName` / `pluginRepo` / `retentionDays` are *written* by load and *read* by
  runtime — the current code says so explicitly ("Both are guarded by the existing `m.mu`
  RWMutex", `manager.go:95-99`). That shared mutex is precisely the coupling that makes a
  two-way split fail. Extract it as its own type owning its own lock; `NameByID` / `IDByName`
  / `ListPlugins` delegate.

- **D-07: `Manager` remains the public type and facade.** `NewManager` and all 11
  `ManagerOption`s keep their signatures. `internal/plugin/setup/subsystem.go`,
  `cmd/holomush`, and every existing test compile unchanged.

- **D-08: `TestLoadPlugin` moves behind a test build tag or into `export_test.go`** — named
  explicitly in both #4674 and MEDIUM-4's recommendation; it currently ships in the
  production binary with no build tag (`manager.go:1472`).
  **The choice is CONDITIONAL on the post-seam-2 caller set** (research finding, verified):
  today there is exactly **one** out-of-package caller —
  `internal/eventbus/authguard/adapter_manifest_test.go:30,50` (`package authguard_test`);
  the other 8 call sites are all inside `internal/plugin`. That test's subject is the very
  adapter seam 2 deletes, so **sequence seam 2 before D-08** — doing so likely removes the
  only blocker and makes `export_test.go` viable. Prefer `export_test.go` if the caller set
  is empty post-seam-2; a custom build tag would otherwise have to be threaded through
  `task test` / `test:int` / `test:cover` / lint (Go sets no automatic test tag), which is
  real plumbing cost for a cosmetic win. Re-check the caller set after seam 2 lands rather
  than assuming either outcome.

### MEDIUM-4 — shared-seam extraction (sequenced FIRST)

- **D-09: The bidirectional-coupling unwind is IN scope and is the prerequisite wave.**
  **TWO** seams, cited in `docs/reviews/arch-review/2026-07-11/findings/d1-architecture.md`
  MEDIUM-4:
  1. **`internal/grpc/focus` types → a neutral lower package.** Seven `internal/plugin` files
     import *up* into the grpc tree (`manager.go`, `host.go`, `goplugin/host.go`,
     `hostcap/capabilities.go`, `lua/{host,focus_ops_adapter,hostcap_adapter}.go`). ARCH-02
     cannot produce a clean layering while the manager imports its own consumer's subpackage.
     Research finding: this is a **types-only** cut — 6 of the 7 files need only
     `focus.Coordinator`.
  2. **`internal/eventbus/authguard/adapter_manifest.go:7`'s `internal/plugin` import → a
     neutral manifest contract.** eventbus both underlies and depends on plugin today. This
     is a **complete cut** — it is the single `eventbus → plugin` edge.

  > **CORRECTED 2026-07-19 (post-research).** A third seam was originally listed here —
  > `TranslateSubscribeErr` → neutral wire-error package, to stop `internal/telnet` importing
  > `internal/grpc` (arch-review LOW-6). **That work is already done.** Phase 7's plan 07-01
  > extracted `internal/grpcclient`; the helper now lives at `internal/grpcclient/client.go:133`,
  > `internal/telnet` has **no** `internal/grpc` import, and `internal/grpc` is **already** in
  > `gatewayForbiddenPackages` (`cmd/holomush/gateway_imports_test.go:146`, with an in-code
  > comment crediting the 07-01 extraction). The original entry was lifted from the 2026-07-11
  > arch-review without re-checking it against post-Phase-7 `main`. **Do not budget a task for
  > it.** Verified live: `rg '"github.com/holomush/holomush/internal/grpc"' internal/telnet/`
  > returns nothing.

- **D-10 [informational]: `cryptowiring`'s reach into three eventbus subpackages is NOT unwound here.**
  It is a wiring package doing wiring and its direction is already one-way
  (`cryptowiring.go:20-22`). Noted, not touched.

### Complexity threshold + regrowth ratchet (success criterion 3)

- **D-11: The threshold is a committed meta-test ratchet, not a review judgment.**
  The decisive evidence is MEDIUM-4's own: both files **grew**
  after #4674 filed them — `manager.go` 1222 → 1869, `server.go` 1331 → 1891. A split with
  no gate demonstrably regrows in this repo. In-repo precedents to follow:
  - `test/meta/plugin_host_capability_decomp_test.go` — a decomposition census meta-test
    (asserts a god-service is gone and every former member is rehomed; `// Verifies: INV-PLUGIN-47`).
  - `test/meta/world_import_graph_test.go` — a production-only import-direction gate built
    on `go/build` (`build.Package.Imports` excludes `_test.go`, so it guards production
    imports without false-flagging test fixtures).
  - `cmd/holomush/gateway_closure_test.go` — Phase 7's transitive-closure gate.

- **D-12: The ratchet pins BOTH size ceilings AND import direction.**
  Size ceilings for the decomposed units, plus the import direction established by D-09.
  Direction without size regrows the files; size without direction lets the arrows re-tangle.

- **D-13: Do NOT enable repo-wide complexity linters in `.golangci.yaml`.**
  No `funlen` / `gocognit` / `gocyclo` / `cyclop` — turning them on repo-wide is a separate
  change with large unrelated blast radius across a mature codebase. Scope the gate to this
  phase's own artifacts.

- **D-14: Consider binding an invariant.** A decomposition that ships a census meta-test is
  exactly the shape the registry wants (`.claude/rules/invariants.md`). If the ratchet
  genuinely pins a durable guarantee, allocate `INV-<SCOPE>-N` and annotate `// Verifies:`.
  Do **not** fabricate a binding on a test that only counts lines.

### Behavior-preservation proof

- **D-15: The contract is ZERO edits to assertions anywhere under the integration tree.**
  That is `test/integration/` and all its subdirectories.
  `task test:int` and the whole-system plugin census pass unchanged. An assertion edit is a
  review-blocking signal that requires written justification, because it means behavior
  moved. This is the operational reading of criteria 1 and 2 ("suites pass *unchanged*").

- **D-16: No up-front characterization/golden tests.** The existing integration +
  whole-system suites already ARE the characterization layer — that is why the success
  criteria name them. New tests in this phase should assert the **new seams' structure and
  unit-testability** (each extracted unit constructible and exercisable with only its own
  narrow dependencies), not re-assert behavior the integration suite already pins.

- **D-17: The integration suite is a MANDATORY gate, not optional.**
  `task test:int` runs at every wave boundary. This is a cross-package type/wiring
  refactor — the exact shape where `task test` stays green while integration breaks
  silently, because `task test` does not compile `//go:build integration` files. Same
  landmine Phase 7 flagged and hit.

### Delivery shape

- **D-18: One phase PR** (Phase 5 D-04 and Phase 7 precedent). Wave structure:
  - **Wave 0** — D-09 shared-seam extraction (prerequisite; unblocks clean layering).
  - **Waves A / B** — ARCH-01 (CoreServer) and ARCH-02 (manager) **in parallel**. After
    Wave 0 they touch disjoint packages with no shared dependency.
  - **Wave C** — ratchet meta-test + census + any invariant binding.

- **D-19: Pre-push review gates — expect `crypto-reviewer` to fire on ARCH-02.** The
  runtime-delivery half moves `PluginRequestsDecryption` / `PluginCanReadBack` (crypto
  manifest gates) and sits adjacent to `internal/plugin/event_emitter.go::Emit` — an
  explicitly listed crypto-reviewer trigger and the plugin-runtime-symmetry chokepoint.
  `abac-reviewer` fires only if `internal/access/` is actually touched (the CoreServer
  split rewires `accessEngine` but should not edit that tree — verify, don't assume).

- **D-20: Plugin-runtime symmetry is a hard constraint on ARCH-02.** Any host-side gate that
  moves during the split MUST still apply identically to Lua and binary plugins. The shared
  chokepoint is `event_emitter.go::Emit`; a split that puts a gate on one runtime's path
  only is a symmetry violation, not a refactor (`.claude/rules/plugin-runtime-symmetry.md`).

### Claude's Discretion

- Exact package names and paths for the extracted units and the three neutral seam packages.
- Whether CoreServer sub-units are separate packages or same-package types (D-02's
  narrow-dependency rule is the constraint; the packaging is not).
- Whether the identity registry (D-06) becomes a new package or a same-package type.
- Numeric ceilings in the ratchet — set from the post-split *actual* with modest headroom,
  not an aspirational round number.
- `TestLoadPlugin`: build tag vs `export_test.go` (subject to D-08's visibility check).
- Internal wave decomposition within Waves A and B; the Wave 0 → A/B → C ordering is fixed.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### The findings this phase closes
- `docs/reviews/arch-review/2026-07-11/findings/d1-architecture.md` § **MEDIUM-4** — the
  primary source: bidirectional subpackage coupling + god-object growth, with per-file
  evidence and the load-time-vs-runtime split recommendation. **Read this first.**
- `docs/reviews/arch-review/2026-07-11/findings/d1-architecture.md` § **LOW-6** — gateway
  tripwire coverage gap; `internal/telnet` → `internal/grpc` via `TranslateSubscribeErr`
  (D-09 seam 3).
- GitHub issue **#4674** — "Architecture cleanup: dead engine handlers + EventStoreAdapter +
  manager/server god-objects" (`gh issue view 4674 -R holomush/holomush`). The original
  god-object filing with the baseline LoC figures the growth is measured against, plus the
  `TestLoadPlugin` and `m.hosts[TypeLua]` vs `m.luaHost` duplication items.

### Requirements & roadmap
- `.planning/REQUIREMENTS.md` — ARCH-01 (line 35), ARCH-02 (line 36); phase mapping at
  lines 95-96.
- `.planning/ROADMAP.md` § "Phase 8: God-Object Decomposition" (line 213) — the three
  success criteria.
- `.planning/phases/07-event-model-bootstrap-decomposition/07-CONTEXT.md` § `<deferred>` —
  the three items Phase 7 explicitly handed to Phase 8 (MEDIUM-4 unwind; `internal/core`
  grab-bag; `coreOnlyFiles` allowlist).

### Rules governing this phase
- `.claude/rules/plugin-runtime-symmetry.md` — hard constraint on ARCH-02 (D-20). Note its
  "permitted asymmetry" section: transport differences reaching the same policy chokepoint
  are fine; do NOT "fix" the nil binary `WorldQuerier`.
- `.claude/rules/gateway-boundary.md` — success criterion 3's "no new gateway-boundary
  violations"; D-09 seam 3 improves compliance.
- `.claude/rules/event-interfaces.md` — the three narrow eventbus interfaces
  (`Publisher` / `Subscriber` / `HistoryReader`) that the CoreServer split's stream units
  consume.
- `.claude/rules/invariants.md` — if the ratchet binds a durable guarantee (D-14).
- `.claude/rules/references/plan-review-learnings.md` — the `_test.go` cross-package
  visibility trap (D-08), the `NewManager` requires `WithVerbRegistry` gotcha, and the
  `Manager.UnloadPlugin` / `loadManifest` non-existence notes.
- Root `CLAUDE.md` § Testing — `task test:int` mandatory on refactors (D-17).

### Key source seams
- `internal/grpc/server.go:154-232` — the `CoreServer` struct; the ~30-field collaborator
  set that D-01/D-02 decompose.
- `internal/grpc/auth_handlers.go` — the existing 16-method split-off; the precedent D-01
  extends.
- `internal/plugin/manager.go:73-105` — the `Manager` struct; note the identity-registry
  comment at `:95-99` naming the shared-mutex coupling D-06 breaks.
- `internal/plugin/manager.go:1472` — `TestLoadPlugin`, untagged in the production file (D-08).
- `internal/eventbus/authguard/adapter_manifest.go:7` — D-09 seam 2.
- `internal/telnet/gateway_handler.go:29,718` — D-09 seam 3.
- `internal/plugin/cryptowiring/cryptowiring.go:20-22` — D-10, noted-not-touched.

### Ratchet precedents (D-11)
- `test/meta/plugin_host_capability_decomp_test.go` — decomposition census meta-test.
- `test/meta/world_import_graph_test.go` — production-only import-direction gate via `go/build`.
- `cmd/holomush/gateway_closure_test.go` — Phase 7's transitive-closure gate.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **The split precedent already exists in-package.** `auth_handlers.go` holds 16 of the 39
  `CoreServer` methods and `session_identity.go` / `refresh_connection.go` /
  `query_stream_history.go` / `list_session_streams.go` / `list_focus_presence.go` /
  `list_available_commands.go` hold one each. ARCH-01 is not "invent a split" — it is
  "finish the split that started, and give the pieces their own dependencies."
- **`test/meta/` already hosts the exact gate shapes needed.** The decomposition census
  (`plugin_host_capability_decomp_test.go`) and the `go/build` import-direction gate
  (`world_import_graph_test.go`) are working templates; D-11's ratchet is composition, not
  invention.
- **The `hostCapabilities` cache (`manager.go:77`, `capabilitiesFor` at `:305`)** already
  models "optional interfaces resolved once at registration" — the shape the runtime-delivery
  unit wants for its host lookups.

### Established Patterns
- **Structural enforcement over discipline.** Phase 7's stated philosophy, and the reason
  D-11 exists: the census meta-test, the fail-closed-at-load manifest gates, the
  transitive-closure gate. A refactor that relies on future reviewers noticing regrowth is
  the pattern this repo has already rejected.
- **Consumer-defined interfaces.** The Go idiom that makes D-02 cheap — an extracted unit
  declares the narrow interface it needs; the facade satisfies it by passing the existing
  concrete collaborator. No new shared abstraction package required.
- **Options as the public wiring surface.** `CoreServerOption` / `ManagerOption` already
  decouple callers from struct shape (D-04, D-07) — the facade can be re-plumbed internally
  without touching `cmd/holomush` or the harness.

### Integration Points
- `cmd/holomush/sub_grpc.go` — constructs `CoreServer`; the wiring blast radius for ARCH-01.
- `internal/plugin/setup/subsystem.go` (673 LoC) — constructs and configures `Manager`
  (`ConfigureEventEmitter`, `ConfigureFocusDeps`, `ConfigureReadbackDecryptor`,
  `ConfigureSettingsDeps`); the wiring blast radius for ARCH-02.
- `internal/testsupport/integrationtest/harness.go` — hand-mirrors production wiring in
  several places; Phase 7 recorded that each mirror must move in lockstep with production.
- `internal/grpc/focus` — the shared seam seven `internal/plugin` files import upward (D-09).

### Landmines
- **`task test` does NOT compile `//go:build integration` files.** Cross-package refactor =
  the exact shape that breaks integration silently. `task test:int` is mandatory (D-17).
- **⚠ CORRECTED 2026-07-19 (during 08-04): the D-06 lock-split safety argument is SCOPED, not
  global.** Research concluded — and this document originally repeated — that splitting `m.mu`
  "preserves the current interleaving window exactly," on the evidence that `loadPlugin` takes and
  releases `m.mu` in four disjoint short critical sections. **That holds for `loadPlugin` only.**
  `UnloadPlugin` (`manager_unload.go`) deleted `activeByName` inside the *same* `m.mu` critical
  section as the `pluginHosts`/`loaded` deletes. 08-04 hoisted `m.identity.Deactivate(name)` above
  `m.mu.Lock()` to avoid nesting the identity lock inside `m.mu`. Consequences:
  - **T-8-06 (deadlock) is genuinely mitigated** — no path holds both locks.
  - **T-8-05: the unload path's interleaving window IS unavoidably widened.** Once identity and
    runtime live under different locks their deletion cannot be atomic. This is inherent to D-06,
    not an implementation defect — but it is a real behavioral delta on the unload path.
  - **08-06 and 08-08 MUST NOT inherit "preserves the window exactly" as a global property.**
    Derive each unit's lock story from its own call-graph read, the way 08-04 did.
  Root cause is the same blind spot 08-03 found: the research method→field matrix records *field
  access* but not that an access sits nested inside a critical section covering other fields.
- **`NewManager` returns `ErrMissingVerbRegistry` when `verbRegistry == nil`**
  (`manager.go:181-201`). Any new test helper constructing a Manager must pass
  `WithVerbRegistry(...)` or it fails at `require.NoError`. **Note:** the invariant id is
  **INV-EVENTBUS-11**, not INV-GW-10 as `plan-review-learnings.md` states.
- **`Manager.TestLoadPlugin` silently no-ops without a registered host** — it checks
  `m.hosts[manifest.Type]` and falls back to `m.luaHost` for `TypeLua`; with neither
  configured it inserts into `m.loaded` but NOT `m.pluginHosts`. (Confirmed still true.)
- ~~**`Manager.UnloadPlugin` does not exist.**~~ **STALE — CORRECTED 2026-07-19.**
  `UnloadPlugin` **does** exist at `internal/plugin/manager_unload.go:22` with 4 tests, and it
  already implements the idempotent cache-cleanup-before-early-return shape that
  `plan-review-learnings.md` recommended. The learnings file's entry predates it. Plans MAY
  reference and modify `Manager.UnloadPlugin`. (`plan-review-learnings.md` should be corrected
  separately — see `<deferred>`.)
- **`m.hosts[TypeLua]` vs `m.luaHost` duplication** (#4674) — backward-compat with no
  external surface to preserve. Tempting to collapse during the split; it IS behavior-adjacent,
  so treat it as an explicit decision, not an incidental cleanup.
- **`cmd/holomush/*_test.go` is `package main`**, not `package main_test`.
- **`internal/plugin/hostcap/servers.go` (1360 LoC) is `internal/grpc/focus`-coupled** —
  Phase 7 flagged that touching it surfaces Phase 8 territory. D-09 touches it for the seam
  only; decomposing it is deferred.

</code_context>

<specifics>
## Specific Ideas

- **The ratchet must measure what actually regrew.** #4674's baseline is 1222 / 1331; today
  is 1869 / 1891. A gate that only asserts "the split happened" (files exist, methods
  rehomed) would have passed at every point during that growth. Pin the *ceiling*, not just
  the shape.
- **Decision D-02 is testable as a property, not a vibe.** "Separately-testable" means a unit test can
  construct the extracted unit with only its own collaborators — no `CoreServer`, no
  `Manager`, no harness. If a unit's test still needs the full wiring, the split did not
  land and criterion 1 is not met. Write at least one such test per extracted unit as the
  proof.
- **The zero-assertion-edit rule from D-15 needs to be checkable.** `git diff --stat test/integration/`
  on the phase branch should show no assertion churn; a plan step that runs and records this
  is cheaper than arguing about it in review.
- **Criterion 3's gateway-boundary half is already banked.** The `internal/grpc` entry in
  `gatewayForbiddenPackages` landed in Phase 7, so "no NEW gateway-boundary violations" is
  now a *regression* check against an existing gate, not new work — Wave 0's two remaining
  seams serve ARCH-02's layering, not the gateway. (Superseded the original LOW-6 bullet;
  see D-09's correction note.)

</specifics>

<deferred>
## Deferred Ideas

- **`internal/core` grab-bag decomposition** — after Phase 7 removed `Event`/`Engine`, it
  still holds Actor, Character, ParseCommand, VerbRegistry, ULID, actor_context,
  session_ended_payload. Phase 7 deferred this "to Phase 8/999.9", but ARCH-01/ARCH-02 name
  `CoreServer` and `plugin/manager` only — `internal/core` is a third god object needing its
  own requirement. → 999.9 or its own phase.
- **`internal/plugin/goplugin/host.go` (1615 LoC) and `internal/plugin/hostcap/servers.go`
  (1360 LoC)** — genuine hotspots per `.planning/codebase/CONCERNS.md`, but not named by
  ARCH-01/02. Touched in this phase only where D-09's seam extraction requires.
- **`cmd/holomush`'s `coreOnlyFiles` allowlist (~30 entries)** — Phase 7 deferred it here;
  still out. It governs `cmd/holomush`, not the two god objects.
- **Eventbus error-model inconsistency** (`fmt.Errorf` + sentinel at the plugin-crossing
  boundary vs `oops` wrapping in the host) — in the same #4674 basket, but a cross-cutting
  error-model change, not a god-object split. File separately.
- **MEDIUM-5: promote `internal/pgnanos` + `internal/idgen` to `pkg/`, move `auditheader`
  out of the SDK's internal deps** — third-party plugin extensibility; unrelated axis.
- **LOW-7: unbounded `orch.StopAll(context.Background())` shutdown** and **LOW-8:
  `productionSubsystems`' 15 same-typed positional params** — bootstrap-shaped (Phase 7
  territory, now shipped). File as issues if not already tracked.
- **`ReadinessRegistry.AllReady` vacuous-truth fail-open** (returns true with zero
  reporters) — noticed in Phase 7 scouting, still unaddressed. File if it becomes
  load-bearing.
- **Doc drift (already tracked):** `.planning/PROJECT.md` "Key Decisions" #3 and
  `.planning/codebase/ARCHITECTURE.md` still assert event-sourcing after the MODEL-01
  reversal. Tracked by issue **#4820** — do not re-file, and do not let those documents
  mislead planning for this phase.
- **`.planning/codebase/CONCERNS.md` LoC figures are stale** (`manager.go` listed at 1838;
  actual 1869). Cosmetic. More broadly the whole `.planning/codebase/` map set predates
  Phases 4–7 (the drift gate reports 379 changed structural elements) — a
  `/gsd-map-codebase` refresh is overdue but is not Phase 8 work.
- **`.claude/rules/references/plan-review-learnings.md` carries two stale entries**, found
  while researching this phase: it claims `Manager.UnloadPlugin` does not exist (it does —
  `internal/plugin/manager_unload.go:22`, 4 tests), and it labels the `NewManager`
  verb-registry guard INV-GW-10 (actual: INV-EVENTBUS-11). Correcting that file is a docs fix
  outside ARCH-01/02 scope — file it, don't fold it in.
- **`gsd-tools intel api-surface` returns `symbolCount: 0`** on this repo. An empty API
  surface is worse than none — absence reads as "this symbol doesn't exist", the exact
  fabrication trap this repo keeps hitting — so it was deliberately withheld from the planner.
  Worth investigating why the extractor finds nothing in a large Go codebase; not Phase 8 work.

</deferred>

---

*Phase: 8-God-Object Decomposition*
*Context gathered: 2026-07-19*
*Amended 2026-07-19 post-research: D-09 reduced from 3 seams to 2 (seam 3 already closed by
Phase 7's `internal/grpcclient` extraction); D-08 made conditional on the post-seam-2 caller
set; `Manager.UnloadPlugin` landmine retracted (it exists); Manager method count 37 → 36.*
