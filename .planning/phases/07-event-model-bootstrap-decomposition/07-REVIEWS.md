---
phase: 7
round: 11
reviewers: [codex, opencode]
reviewed_at: 2026-07-17T15:45:55Z
plans_reviewed: [07-01-PLAN.md, 07-02-PLAN.md, 07-03-PLAN.md, 07-04-PLAN.md, 07-05-PLAN.md, 07-06-PLAN.md, 07-07-PLAN.md, 07-08-PLAN.md, 07-09-PLAN.md, 07-10-PLAN.md, 07-11-PLAN.md]
plans_revision: "rev 13 (ed6b72b2b)"
notes: "Round 10 preserved at 125c646b7. FULL round: both reviewers received all 11 plans with the standard source-grounded prompt plus a round-context note (rev 13, dispositions are settled — flag only defective dispositions). codex: default model. opencode: openrouter/moonshotai/kimi-k3 — the round-10 FAILED lane recovered this round; the first attempt failed instantly on an invalid model reference (the bare 'moonshotai/kimi-k3' is not a registered opencode provider/model pair — the registered slug is 'openrouter/moonshotai/kimi-k3'); with the corrected slug the lane completed on its first attempt with no 429s. The kimi output opens with its live verification narration (~65 claims traced against the tree); it is retained as part of the record. The orchestrator independently verified every HIGH and every gate-satisfiability claim against the live tree before writing the consensus."
---

# Cross-AI Plan Review — Phase 7 (Round 11, rev 13, full set)

## Codex Review

# Summary

Revision 13 is exceptionally well-grounded, and plans 07-01 through 07-07 plus 07-09 and 07-10 appear executable against the current tree. However, two unresolved compile-order defects remain in 07-08 and 07-11. Both occur at task boundaries and would make mandated gates fail on an otherwise correct implementation. The phase is therefore **not ready for execution as written**.

## Plan-by-plan assessment

| Plan | Verdict | Assessment |
|---|---|---|
| 07-01 | Ready | Correctly extracts the client seam used by telnet’s error classification at [gateway_handler.go:718](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/telnet/gateway_handler.go:718). The symbol-first census and integration-tagged `phase1_5_test.go` handling close the previous alias gap. |
| 07-02 | Ready | The vocabulary leaf is necessary because both Event representations currently carry overlapping type vocabularies, while the surviving type has host-private state at [types.go:141](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/types.go:141). Sequencing before the gateway ban is sound. |
| 07-03 | Ready | The three extracted values are genuine leaves: `NewULID` at [ulid.go:40](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/ulid.go:40), command parsing at [command.go:9](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/command.go:9), and the lease constant at [reaper.go:25](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/session/reaper.go:25). Keeping the ULID forwarder until 07-07 preserves compile order. |
| 07-04 | Ready | The existing gate only inspects direct `file.Imports` at [gateway_imports_test.go:148](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/gateway_imports_test.go:148); the planned `NeedDeps` closure test genuinely strengthens the invariant instead of duplicating it. |
| 07-05 | Ready | The auth-side consumer interface is necessary: auth stores `*core.Engine` at [auth_service.go:28](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/auth/auth_service.go:28) and invokes both disconnect and end-session behavior at [auth_service.go:235](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/auth/auth_service.go:235). The plan also preserves the typed-nil guard at [engine.go:44](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/engine.go:44). |
| 07-06 | Ready | The duplicate payload construction is real: command builds `{"message":…}` at [types.go:628](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/command/types.go:628), while hostcap explicitly mirrors it at [system_broadcaster.go:45](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/hostcap/system_broadcaster.go:45). |
| 07-07 | Ready | The survivor direction is correct. `eventbus.Event` owns `Seq` and the private `auditRow` crypto seam at [types.go:141](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/types.go:141), while the duplicate `core.Event` remains at [event.go:233](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/event.go:233). |
| 07-08 | **Not ready** | Task 2 changes the `ReplayTail` signature but omits the integration test created by Task 1 from its migration census. See Concern 1. |
| 07-09 | Ready, high execution risk | All five eager starts are present in `core.go`, including database at [core.go:287](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/core.go:287), eventbus at line 462, and ABAC/auth at lines 797–800. The provider and TLS-subsystem design addresses the actual cause. |
| 07-10 | Ready | The current rollback inherits the startup context and omits the failing subsystem at [orchestrator.go:58](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/lifecycle/orchestrator.go:58). The missing gRPC audit edge is also confirmed by [sub_grpc.go:170](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/sub_grpc.go:170). |
| 07-11 | **Not ready** | Its stated atomicity conflicts with its Task 2 commit/gate boundary and deferred test-implementation migration. See Concerns 2–3. |

## Strengths

- Cross-plan composition is unusually careful. ARCH-05’s leaves and vocabulary migrate before `core.Event` deletion, avoiding an intermediate gateway→eventbus dependency.
- The event collapse preserves security-sensitive internals. The plan explicitly retains the unexported audit row used by the downgrade fence rather than moving `Event` to an attractive but invalid neutral package.
- The bootstrap plans distinguish acquisition, domain serving, host-owned loops, observability, and plugin subprocess initialization. That precision makes the two-sweep guarantee satisfiable.
- Tests target actual historical failure modes: KEK-configured boot, seq/ULID disagreement, runtime symmetry, cancelled-context rollback, partial preparation, and disabled admin-socket mode.
- Previously disputed decisions are documented with dispositions and source-grounded reasons, substantially reducing executor discretion.

## Concerns

- **HIGH — 07-08 Task 2 does not migrate the regression test created by Task 1 to the new `ReplayTail` signature.**

  Task 1 creates `cmd/holomush/plugin_replaytail_pagination_integration_test.go` and directly calls `busHistoryReaderAdapter.ReplayTail` ([07-08 plan:310–342](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-08-PLAN.md:310)). Task 2 adds `beforeSeq uint64` to that method, but its task-level file list and “full site list” omit the newly created test ([07-08 plan:440](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-08-PLAN.md:440), [07-08 plan:250](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-08-PLAN.md:250)).

  The current method has the old five-argument shape at [sub_grpc.go:916](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/sub_grpc.go:916). Once Task 2 adds `beforeSeq`, Task 1’s test calls no longer compile, and Task 2’s own `task test:int` gate will compile that file and fail.

- **HIGH — 07-11’s Task 2 cannot satisfy its own `task test` gate before Task 3.**

  Task 1 replaces `lifecycle.Subsystem.Start` with `Prepare`/`Activate`; the current interface is at [subsystem.go:44](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/lifecycle/subsystem.go:44). Task 2 then migrates production implementations and explicitly commits after `task build`, but Task 3 defers two live test implementations:

  - `cmd/holomush.stubSubsystem` still implements only `Start` at [core_subsystems_test.go:23](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/core_subsystems_test.go:23) and is passed where `lifecycle.Subsystem` is required at line 57.
  - `stubRegistry` still implements only `Start` at [coordinator_error_test.go:36](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/crypto/invalidation/coordinator_error_test.go:36). Task 2 also changes the `cluster.Registry` lifecycle surface currently declared at [registry.go:23](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/cluster/registry.go:23).

  Nevertheless, Task 2 requires `task test` and says to commit at its boundary ([07-11 plan:1276–1288](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-11-PLAN.md:1276)). Those test packages cannot compile until Task 3.

  This also contradicts the plan’s later statement that the entire interface migration, all implementations, and every caller must land atomically ([07-11 plan:1391](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-11-PLAN.md:1391)).

- **MEDIUM — 07-11 Task 2’s task-level file manifest omits files its own action requires changing.**

  Task 2’s `<files>` list contains the implementation files and two cluster harness callers ([07-11 plan:843](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-11-PLAN.md:843)), but its acceptance criteria require edits and new tests in `cmd/holomush/sub_grpc_test.go`, plugin setup tests, audit tests, admin socket tests, policy tests, and relay/sweep tests. These appear in the plan-level frontmatter, but not the task that performs them. That weakens task scoping and makes omission more likely during the largest plan in the phase.

## Suggestions

- Amend 07-08 Task 2 to include `cmd/holomush/plugin_replaytail_pagination_integration_test.go` in both `<files>` and the signature census. Explicitly change its page-advance calls to pass the prior page’s oldest `(Seq, ID)` pair.
- Make 07-11 Tasks 1–3 one explicit atomic execution unit and one commit, mirroring 07-01 and 07-05. Keep scoped RED/inner-loop checks, but place the first whole-tree `task build`, `task test`, and `task test:int` gate only after Task 3.
- Alternatively, move the two remaining test implementations and the production-graph property test into Task 2 before its whole-repo gate. Remove Task 2’s premature commit instruction.
- Expand 07-11 Task 2’s `<files>` to include every test file named by its action and acceptance criteria.

## Risk Assessment

**Overall risk: HIGH until the two compile-order blockers are corrected.**

The underlying architecture and behavioral coverage are strong; the remaining problems are execution sequencing rather than design flaws. Once 07-08’s new test is migrated with the signature and 07-11’s interface change is made genuinely atomic, residual phase risk falls to **MEDIUM**, driven mainly by the 50-plus-file lifecycle migration and the integration-only caller surface.
---

## OpenCode Review (openrouter/moonshotai/kimi-k3)

I'll verify the plans' load-bearing claims against the live source tree. Starting with git state and the highest-risk claims.
Tree is pre-execution at the stated commit; `internal/grpc/client.go` is exactly 598 lines as claimed. Now verifying the load-bearing claims in parallel.
All five eager starts, the ownership flag, the 16-param `productionSubsystems`, the false "runs before EventBus" comment, and `SubsystemTLS` at iota=1 verified. Continuing with the orchestrator and event-model claims.
Orchestrator mechanics verified (rollback at `:60` passes startup ctx — the thing 07-10 fixes). Eventbus closure contains `internal/auth` (FINDING-1 real); telnet closure = 47, web = 7. Verifying per-package leaf claims and the event-model files.
Leaf claims verified (core/session/naming each = 1 internal dep, themselves). Engine guards confirmed. Now the 07-08 load-bearing claims: the cursor contract and both encode sites.
07-08's core claims all verified (tripwire contract, no `BeforeID` branch in `matchesQuery`, `protoEvents[0]` anchor, hardcoded `Seq: 0`). Now the Lua-side dual encode sites and the adapter.
Lua dual `Seq: 0` sites, decode-side Seq blindness, and the adapter's no-seq query all verified. Now the auth cycle seam, invariant registry, and gateway test claims.
FINDING-1's auth consumer sites all verified. INV-EVENTBUS-1 entry, phantom `internal/auth/service` in the forbidden list, and direct-import-only `checkFile` confirmed. Continuing with bootstrap-graph claims.
Bootstrap-graph claims verified (nil eventbus deps, phantom auth/service, missing AuditProjection edge, verifier ctor-time `NewVerifier`). Checking cluster split, gateway import, and the plugin-cleanup blocker.
Cluster split, gateway import, phase1_5 dual-use, and the round-9 plugin-leak blocker all verified against live source. Now the audit/gRPC/emitter claims.
Audit/gRPC/emitter claims verified. Now checking CoreServer's publication seam, the wrapped-publisher wiring, and the remaining structural claims.
CoreServer seam, wrapped-publisher flow, socket atomic Start, cursor shape, and proto 8-field census all verified. Final batch: the vocabulary sources, interface coupling, and eventbus construction claims.
All interface-coupling and construction claims verified. Final verification batch: compile-assertion census, deps seam, telnet call sites, and the crypto/audit fences.
One last check on the telnet import alias and a couple of remaining census claims.
Excellent — all structural claims verified. Now checking a few execution-critical details: how the bus GameID closure would actually spell in `sub_grpc.go`, remaining cluster.Registry implementers, and the harness/relay claims.
One potential gate-satisfiability issue emerging around the `bus.GameID()` grep. Checking the production adapter construction to resolve it.
Found a likely gate-satisfiability nit. Verifying the remaining closure-critical packages and the end-of-plan gateway gates.
Verifying the last cycle-safety claim and a few remaining feasibility details.
One discrepancy to resolve: `TestAdminAuthenticate` grep didn't match `admin_authenticate_e2e_test.go`. Checking the test entrypoints and the cited lines.
One last precise check on 07-09's `rawBusGameID` capture-point instruction against the actual config-load code.
One final spot-check on 07-11's row-16 reaper claims.
Verifying one last potential gap — whether `sub_grpc.go` is the only live caller of the auth fanout seams (07-05's atomic-unit assumption).
Verification complete. I traced ~65 distinct load-bearing claims across all 11 plans against the live tree at `ed6b72b2b`. Here is the review.

---

# Cross-AI Plan Review — Round 11 (rev 13, `ed6b72b2b`)

## Summary

This revision is execution-ready. After ten rounds, the plans' factual basis is unusually solid: every claim I traced against the live source tree — the five eager starts, the FINDING-1 auth cycle, the 47-package telnet closure, the phantom `internal/auth/service`, the dual `Seq: 0` encode sites in the Lua path, the `protoEvents[0]` pagination anchor, the 16-param `productionSubsystems`, the plugin `cleanupOnError` host-leak, the atomic `socket.Server.Start()`, the 10-vs-7 `var _ lifecycle.Subsystem` census, and the entire 07-08 tripwire/anchor mechanism — checked out verbatim, in most cases at the exact cited line numbers. The wave/DAG structure composes correctly (shared-file overlaps across same-wave plans are empty; the 07-11 `grpc→Plugins` edge is transitively redundant and cannot perturb 07-10's pinned topo order). I found no execution-breaking defects. Three LOW findings remain: one gate-satisfiability nit in 07-06 (a grep that encodes an unstated variable-naming requirement) and two informational notes.

**Method note:** all findings below were verified by reading the live tree (`sed`/`grep`/`go list -deps` on this worktree), not inferred from the plan text. Where a concern from earlier rounds has a documented disposition, I re-verified the disposition's *factual premise* rather than re-litigating it — every premise I checked held.

## Strengths

- **The atomic-unit commit pattern is correctly engineered at every mid-unit boundary.** I traced each one: 07-01 Tasks 1+2 (`internal/grpcclient` move + caller rewire — `cmd/holomush/gateway.go:20,152-153,227` confirmed still importing `internal/grpc`), 07-05 Tasks 2+3 (auth interface retype — `cmd/holomush/sub_grpc.go:299` confirmed as the **only** production caller of `ConfigureGameSessionFanout`; `WithGameSessionFanout` has zero production callers, so the mid-unit `task test -- ./internal/auth/` gate genuinely compiles standalone with the in-package fake), and 07-06 Task 2 absorbing the `sub_grpc.go:292` caller rewire because its own `task test:int` gate compiles `./...`. Each task-scoped gate is satisfiable exactly where the plan places it; each unit boundary carries the whole-repo gates. This is the correct resolution of "every commit builds" vs "mid-unit doesn't compile."
- **The cycle analysis is provably right, and the dispositions match the graph.** `go list -deps ./internal/eventbus` contains `internal/auth` (FINDING-1 is real) and does **not** contain `internal/grpc`; `go list -deps ./internal/auth` contains no `internal/eventbus`. 07-05's repair (2-method `PresenceEmitter` consumer interface in `internal/auth`) covers exactly the two methods auth calls (`HandleDisconnect` at `auth_service.go:235`, `EndSession` at `:243`) — no wider, no narrower.
- **07-08's mechanism is verified end-to-end and the red-framing correction is accurate.** `bus.go:88-105` ("The id field is a **tripwire**… Zero seq means 'from the end' (backward)"), `matchesQuery` (`hot_jetstream.go:392-402`) having no `BeforeID` branch, and `buildConfig`'s `if q.BeforeSeq > 0` gate (`:338`) together prove the quiet-stream repeat is deterministic — the corrected RED framing is right and the retracted "ID-only fallback" never existed. The anchor analysis is also right: `hostcap/servers.go:900-906` (`nextCursor = protoEvents[0].GetCursor()`, "index 0 is the boundary") and `stdlib_focus.go:452-463` (`events[0].ID`, "oldest event… is the backward-pagination anchor") confirm oldest-not-last, and the Lua path does hardcode `Seq: 0` at **two** independent sites (`:437-441` per-event, `:459-463` next_cursor) with a Seq-blind decode (`:367-380`) — the "three sites, not two" settlement is necessary, not cautious.
- **The round-9 plugin-leak blocker is real and the fix shape is exact.** `cleanupOnError` (`plugin/setup/subsystem.go:237-250`) closes aliasPool/schemaProvisioner/worldConn but never the hosts; `goplugin.NewHost` launches `go h.tokenStore.Run(h.tokenStoreCtx)` at construction (`goplugin/host.go:359`); three error paths (`:333`, `:341`, `:381`) sit between host construction (`:322`) and `s.manager = mgr` (`:384`); `Stop` no-ops on `s.manager == nil` (`:451-453`). The plan's instruction to declare `binaryHost` before the closure is the correct minimal repair (Go closure capture semantics).
- **Cross-plan wave composition is clean.** 07-03∥07-05 and 07-04∥07-06 have empty `files_modified` intersections (verified by comparing the manifests against the actual edit targets — e.g. 07-03 owns `telnet/gateway_handler.go`'s `ParseCommand`/`NewULID` repoints at `:406,:670` while 07-02 owns only the vocabulary at `:1247-1249`). Post-07-03, the telnet closure resolves to genuine leaves only (`telemetry→xdg/config`, `gatewaymetrics`, `gamenotice`, `config→xdg` all verified leaf-clean via `go list -deps`), so 07-04's closure gate is achievable rather than aspirational.
- **07-10's MEDIUM-11 settlement is forced by data flow, and the plan proves it twice.** `auditPublisher = eventbus.NewRenderingPublisher(eventBusSub.Publisher(), …)` (`core.go:735`) feeding `buildReadStreamWiring` which hard-requires non-nil `AuditPublisher` (`readstream_wiring.go:118`) — the verifier's handler set genuinely cannot be built before the bus. The reverse edge would be both a `topoSort` cycle (Kahn's at `orchestrator.go:98-149`, error at `:108`) and a silent `operator_read` unregistration. The doc-comment-as-guardrail disposition is the right call.
- **The invariant work is honest.** INV-EVENTBUS-1's `binding: pending` with stale `refs` token `INV-GW-1` (`invariants.yaml:2340-2348`), the phantom `internal/auth/service` in the `forbidden` list (`gateway_imports_test.go:107`, confirmed nonexistent via `go list ./internal/auth/...`), the deliberate regex fixtures (`:483-486`), and the direct-import-only `checkFile` (`:148`) are all as described. Binding to both the direct and closure gates avoids the partial-binding hazard.
- **Every "X is already a leaf" drift correction is true.** `go list -deps` on `internal/core`, `internal/session`, `internal/naming` returns exactly one holomush-internal package (themselves). The drift-prevention rationale the plans substitute for the false "reaches the DB" claim is the defensible one.

## Concerns

**LOW — 07-06 Task 3's `bus.GameID\(\)` grep encodes an unstated variable-naming requirement (gate satisfiability).** The criterion requires `rg -c 'bus.GameID\(\)' cmd/holomush/sub_grpc.go` ≥ 2, intending to prove one shared game-id source. But the production bus is `s.cfg.EventBus`: the existing in-file idiom is a method value, `gameID: s.cfg.EventBus.GameID` (`sub_grpc.go:602`). The lowercase literal `bus.GameID()` appears exactly once today — `b.bus.GameID()` at `:833` inside `busEventAppender` (which 07-07 deletes; in 07-06's window it still exists, so baseline count = 1). `s.cfg.EventBus.GameID()` contains `Bus.GameID()` (capital B), not `bus.GameID()`. An executor writing the semantically correct `func() string { return s.cfg.EventBus.GameID() }` — or the method value matching `:602` — produces count 1 and fails the gate on correct code. The plan's action text suggests `func() string { return bus.GameID() }`, but no local `bus` exists in `grpcSubsystem.Start`'s scope; that snippet only compiles if the executor introduces `bus := s.cfg.EventBus`, which the plan never states. The gate passes only under an unstated naming choice.

**LOW (informational) — the KEW-wired boot gate's `-run 'TestAdminAuthenticate|AdminRekey'` alternation is half-no-op at the Go level.** `admin_rekey_e2e_test.go` declares no `^func Test` — its Ginkgo `Describe`s execute under the single `TestAdminAuthenticateE2E` RunSpecs entrypoint (`admin_authenticate_e2e_suite_test.go:32`). The gate is **not** vacuous: that one entrypoint runs the whole admin suite, which boots the full server with a KEK file (`admin_authenticate_e2e_test.go:215`, `:265` — both citations verified) including the rekey specs. But `AdminRekey` matches nothing at the `go test -run` level; the plan's phrasing ("`admin_authenticate_e2e_test.go` and `admin_rekey_e2e_test.go` boot the full server") slightly overstates the pattern's role. The coverage is real; the regex is just ornamental in its second half.

**LOW (informational) — 07-08's new test file is missing from Task 2's task-level `<files>` list.** `cmd/holomush/plugin_replaytail_pagination_integration_test.go` is in the plan frontmatter (first entry) but not in Task 2's `<files>`. Task 2's signature change (`beforeSeq uint64` added to `ReplayTail`) forces an edit to that file: the RED test written at Task 1 against the old signature passes only `beforeID`, which under the new code still yields a tail read — so Task 2's own "Task 1's regression is now GREEN" criterion is unsatisfiable until the test's cursor round-trip is updated to thread `beforeSeq`. The plan frontmatter covers it and `task test:int` catches it immediately, so this is a manifest tidiness nit, not a trap — but the round-9/10 pattern was to close exactly this criteria-vs-files class.

## Suggestions

1. **07-06 Task 3:** either state the local in the action text — "introduce `bus := s.cfg.EventBus` once and write all three closures as `func() string { return bus.GameID() }`" — or relax the criterion to `rg -c 'EventBus\.GameID|\.bus\.GameID' cmd/holomush/sub_grpc.go` ≥ 3 (counting the source expressions rather than a variable name). The former is more consistent with the plan's own illustrative snippets.
2. **07-09/07-10/07-11 KEK gates:** drop the `|AdminRekey` alternation or rename the criterion to describe what it actually runs ("the admin Ginkgo suite via `TestAdminAuthenticateE2E`, which includes the rekey Describes"). Keeps future readers from believing the pattern scopes the run.
3. **07-08 Task 2:** add `cmd/holomush/plugin_replaytail_pagination_integration_test.go` to the task's `<files>` list and one sentence to the action — "update the Task 1 test's cursor round-trip to extract `HostCursor.Seq` and pass it as `beforeSeq`; without this the walk stays RED under the new signature." This closes the last criteria-vs-files gap in the phase.

## Risk Assessment

**LOW.** This is the first round where I could not find a single execution-breaking defect. The three remaining findings are a naming-dependent grep (recoverable in minutes by any executor, with the correct semantics obvious), an ornamental regex alternation (coverage is real regardless), and a task-manifest omission that the plan's own frontmatter and integration gate cover. The prior rounds' structural work — the atomic-commit units, the cycle dispositions, the retracted reverse edge, the page-anchor correction, the plugin-leak repair — all verified as grounded in the actual tree. The plans achieve the phase's three success criteria: the event-model collapse chain (07-02→07-05→07-06→07-07→07-08) is complete with no dangling type references; the bootstrap migration (07-09→07-10→07-11) has satisfiable gates at every commit boundary including the mid-unit states; and the gateway boundary (07-01→07-03→07-04) lands on a verified achievable leaf-only closure. Ship it.

---

## Consensus Summary

Two grounded lanes this round — the first two-lane round since round 9. The verdicts split: **codex says NOT ready (2 HIGH)**, **kimi-k3 says execution-ready (LOW, "ship it")**. The orchestrator verified every HIGH and every gate-satisfiability claim against the live tree at `ed6b72b2b`; the verified result sides with codex on the verdict while confirming kimi's unique gate finding. **Net: NOT execution-ready by 2 verified blockers, both task-gate mechanics, zero design defects** — the same class (and smaller count) as rounds 9–10.

### Orchestrator verification of the headline findings

1. **CONFIRMED — BLOCKER (codex HIGH 1; kimi found the same fact, rated LOW): 07-08 Task 2 omits the Task-1 test from its `<files>` and site census.** Task 1 creates `cmd/holomush/plugin_replaytail_pagination_integration_test.go` whose specs call `busHistoryReaderAdapter.ReplayTail` directly, round-tripping the cursor (07-08-PLAN.md:322, :344). Task 2 adds `beforeSeq uint64` to the method — but the "Full site list (verified live)" (:250-257) and Task 2's `<files>` (:440) both omit the new test file. A direct method call with the old 5-arg shape is a **compile error** once the signature grows (codex's mechanism is correct; kimi's "compiles but stays RED" understates it — either way the same fix is forced). Task 2's own gate `task build && task test && task test:int` (:572) fails at `task test:int` on a file the manifest never tells the executor to touch. This is precisely the criteria-vs-files class round 10 closed in 07-09.
   **Fix (rev 14):** add the test file to Task 2's `<files>` and the census, plus one action sentence: update the test's cursor round-trip to extract the page-anchor's `(Seq, ID)` pair and pass `beforeSeq`.

2. **CONFIRMED — BLOCKER (codex HIGH 2; kimi's contrary claim falsified): 07-11 Task 2 cannot satisfy its own `task test` gate before Task 3.** Task 1 replaces `lifecycle.Subsystem.Start` with `Prepare`/`Activate`; Task 2 migrates the 17 production impls **and the `cluster.Registry` interface** (criterion at 07-11-PLAN.md: "`rg -c 'Start\(ctx context\.Context\) error' internal/cluster/registry.go` returns 0"), demands `task build && task test && task test:int` exit 0 (:1276-1289), and commits at its boundary ("After the task's single squashed commit…", :1282). But two Start-only stubs survive until Task 3 (`<files>` at :1299):
   - `cmd/holomush/core_subsystems_test.go:23` — `stubSubsystem` implements only `Start`, is `//go:build !integration` (compiled by `task test`), and is passed where `lifecycle.Subsystem` is required (`productionSubsystems(...)`).
   - `internal/eventbus/crypto/invalidation/coordinator_error_test.go:35-37` — `stubRegistry` implements only `Start`, has **no** build tag (compiled by `task test`), and is used as a `cluster.Registry`.
   Both packages compile-fail at Task 2's boundary; the gate is unsatisfiable on correct code. This also contradicts the plan's own atomicity note (:1391-1395, "the tree does not compile until all 17 impls **and** every caller land together").
   **Fix (rev 14):** make Tasks 1–3 (or at minimum 2–3) one explicit atomic execution unit with one commit — mirroring the rev-13 07-01/07-05 squash pattern — with scoped inner-loop checks and the whole-repo `task test`/`task test:int` gates only at the unit end; or move the two stub migrations (and the property test's home file) into Task 2 before its whole-repo gate.

3. **CONFIRMED — MEDIUM (codex): 07-11 Task 2's `<files>` omits test files its own acceptance criteria require editing/creating** — the row-16 reaper focused test (`cmd/holomush/sub_grpc_test.go`), the plugin fault-injection RED test (`internal/plugin/setup/subsystem_test.go`), the admin-socket disabled-mode two-phase test, the policy repeated-call tests, and the audit repeated-Prepare/Stop-after-Prepare tests. Task 2's `<files>` (:843) lists only production files plus the two clustertest harness callers. Folding Tasks 2–3 into one unit (fix 2) should merge and complete the manifests in the same edit.

4. **CONFIRMED — LOW but a real gate defect (kimi, unique): 07-06 Task 3's `rg -c 'bus.GameID\(\)' cmd/holomush/sub_grpc.go ≥ 2` criterion encodes an unstated variable-naming requirement.** Verified live: the file's idiom is `s.cfg.EventBus.GameID` (method value, :602/:277/:547) or `s.cfg.EventBus.GameID()` (direct, :459/:514/:580) — neither matches the case-sensitive lowercase pattern; the only live match is `b.bus.GameID()` (:833, inside `busEventAppender`, which still exists in 07-06's window), so the baseline count is 1. An executor writing the semantically correct `func() string { return s.cfg.EventBus.GameID() }` fails the gate; the plan's own illustrative snippet `func() string { return bus.GameID() }` (:400) only compiles if the executor invents `bus := s.cfg.EventBus`, which no action text states.
   **Fix (rev 14):** state the local (`bus := s.cfg.EventBus`) in the action text, or relax the criterion to count source expressions (e.g. `rg -c 'EventBus\.GameID|\.bus\.GameID'`).

5. **NOTED — LOW informational (kimi, unique, not independently re-verified): the KEK-boot gate's `-run 'TestAdminAuthenticate|AdminRekey'` alternation is half-ornamental** — `admin_rekey_e2e_test.go` declares no top-level `Test*` func; the whole admin Ginkgo suite (including the rekey Describes) runs under the single `TestAdminAuthenticateE2E` entrypoint. Coverage is real; only the pattern's second half matches nothing. Optional phrasing cleanup in 07-09/10/11.

### Agreed Strengths

- **The rev-13 atomic-unit commit pattern works where it was applied.** Both reviewers independently verified the 07-01 (Tasks 1+2), 07-05 (Tasks 2+3) and 07-06 (caller-rewire-in-Task-2) units, with kimi tracing the mid-unit gate satisfiability claim-by-claim (e.g. `sub_grpc.go:299` as the only production caller of `ConfigureGameSessionFanout`). The two blockers this round are exactly the two places the pattern was *not* applied (07-08 Task 2, 07-11 Tasks 2–3).
- **The factual base of the plans is sound.** Kimi traced ~65 load-bearing claims (eager starts, FINDING-1 auth cycle, telnet closure = 47, phantom `internal/auth/service`, dual Lua `Seq: 0` sites, `protoEvents[0]` anchor, plugin `cleanupOnError` host-leak, 10-vs-7 compile-assertion census) and found zero factual drift; codex's per-plan table independently confirms the same anchor points (07-01 telnet seam, 07-02 vocabulary leaf, 07-03 leaves, 07-04 `NeedDeps` strengthening, 07-05 consumer interface, 07-07 survivor direction, 07-09 eager starts, 07-10 rollback-context defect).
- **Cross-plan composition is clean.** Same-wave `files_modified` intersections are empty; the 07-11 `grpc→Plugins` edge is transitively redundant to 07-10's pinned topo order; the event-collapse chain (07-02→07-05→07-06→07-07→07-08) has no dangling type references.
- **Settled dispositions held.** Neither reviewer overturned any rev-13 disposition; kimi explicitly re-verified the factual premises of the dispositions it checked (07-10 MEDIUM-11 doc-comment guardrail, 07-08 red-framing correction, round-9 plugin-leak fix shape) and found every premise true.

### Agreed Concerns

- **07-08 Task 2's missing test-file migration** — found by both lanes independently (codex HIGH, kimi LOW-informational). The only concern this round with full cross-reviewer agreement on the fact pattern; orchestrator-verified as a blocker (finding 1 above).

### Divergent Views

- **Overall verdict.** Codex: NOT ready (HIGH until the two compile-order blockers are fixed, then MEDIUM). Kimi: ready ("LOW … Ship it"). Resolution: the orchestrator's source verification confirms both codex HIGHs, including falsifying kimi's specific claim that "the bootstrap migration (07-09→07-10→07-11) has satisfiable gates at every commit boundary" — kimi verified the 07-01/05/06 units in depth but did not check 07-11 Task 2's whole-repo `task test` gate against the two Start-only stubs Task 3 owns. The verified verdict is codex's: **NOT execution-ready until rev 14 lands the two gate fixes.** The depth-vs-breadth pattern of rounds 5–10 (codex finds the surviving blockers; the second lane endorses) recurs — though this round kimi's lane materially contributed a genuine unique gate defect (07-06 grep) that codex missed, the first time the second lane has done so.
- **07-08 severity.** Codex HIGH (compile-break at the Task 2 gate) vs kimi LOW (manifest tidiness caught by `task test:int`). The mechanism check favors codex: the direct 5-arg call cannot compile after the signature change, and the gate blocks the task's own commit. Same fix either way.

### Recommended next step

`/gsd-plan-phase 7 --reviews` for a **rev 14 scoped to gate mechanics only** (zero design changes): (a) 07-08 Task 2 census + `<files>` + action sentence; (b) 07-11 Tasks 2–3 atomic-unit merge (or stub-migration hoist) + manifest completion; (c) 07-06 Task 3 grep criterion fix; optionally (d) the `|AdminRekey` phrasing cleanup in 07-09/10/11. Then execute — both lanes and eleven rounds agree the design surface is exhausted.
