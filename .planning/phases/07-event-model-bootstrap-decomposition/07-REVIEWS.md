---
phase: 7
round: 9
reviewers: [codex, opencode]
reviewed_at: 2026-07-17T00:17:14Z
plans_reviewed: [07-01-PLAN.md, 07-02-PLAN.md, 07-03-PLAN.md, 07-04-PLAN.md, 07-05-PLAN.md, 07-06-PLAN.md, 07-07-PLAN.md, 07-08-PLAN.md, 07-09-PLAN.md, 07-10-PLAN.md, 07-11-PLAN.md]
plans_revision: "rev 11 (1e636f37b)"
notes: "Round 8 preserved at 6754b3a39. FULL round: reviewers received all 11 plans with the standard source-grounded prompt; no depth directive, no diff, and no prior-round findings were shared. opencode: openrouter/x-ai/grok-4.5; codex: default model. Orchestrator verified every Codex HIGH against source before the consensus (verdicts in the Consensus Summary)."
---

# Cross-AI Plan Review — Phase 7 (Round 9, rev 11, full set)

## Codex Review

# Cross-AI Plan Review — Phase 7

## Overall assessment

The plans are unusually well-grounded: they trace cross-package call sites, recognize integration-build gaps, preserve plugin-runtime symmetry, and settle most lifecycle semantics explicitly. However, the phase is **not ready to execute unchanged**. I found five material issues:

1. **07-06 contains assertions that will compile but fail at runtime after switching from `core.Event` to `eventbus.Event`.**
2. **07-08 requires manually controlled event IDs without defining a sanctioned test seam.**
3. **07-09 knowingly changes the persisted EventBus subject namespace without migration, conflicting with the phase’s behavior-preserving boundary.**
4. **07-10’s asynchronous `StopAll` can leave teardown mutating state after rollback returns.**
5. **07-11 overstates its lifecycle barrier and leaves a verified partial-Prepare leak in plugin setup.**

Recommendation: revise those five plans before execution. Plans 07-01 through 07-05 are otherwise executable; 07-07 is strong but high-risk due breadth.

---

## 07-01 — Extract `internal/grpcclient`

### Summary

A sound, mechanically bounded extraction that addresses the most consequential gateway dependency. The symbol-first census is especially valuable because this repository uses several aliases for `internal/grpc`.

### Strengths

- The extraction target is genuinely domain-free: `client.go` imports grpc-go, observability, `oops`, and generated protos, but no `internal/...` packages. [internal/grpc/client.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/grpc/client.go:10)
- It addresses both direct gateway edges: telnet imports `internal/grpc` for error translation, while `cmd/holomush/gateway.go` uses its client constructor. [gateway_handler.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/telnet/gateway_handler.go:29), [gateway.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/gateway.go:20)
- The revised census correctly catches `test/integration/phase1_5_test.go`, where the package is aliased as `grpcpkg` and used for both client and server symbols. [phase1_5_test.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/test/integration/phase1_5_test.go:27)

### Concerns

- **LOW:** No substantive source-grounded blocker found. The main residual risk is the 23-file mechanical rewire, particularly integration-tagged consumers.

### Suggestions

- Preserve the symbol-first census in the summary as required.
- Run `task test:int` in the same task that deletes `internal/grpc/client.go`; the plan correctly includes this gate.

### Risk Assessment

**MEDIUM.** Broad but mechanical, with good compile and integration coverage.

---

## 07-02 — Extract `internal/eventvocab`

### Summary

The plan correctly isolates event discriminators and payload shapes so both gateways and EventBus can consume them without importing `core`.

### Strengths

- The source vocabulary is concentrated in `internal/core/event.go`, including the nine host event types and payload-size validation. [event.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/event.go:19)
- Pinning literal wire strings and JSON tags is appropriate because these values flow into published events and audit records.
- The plan avoids a forwarding `core.EventType` alias, which would preserve the duplicated ownership it is meant to remove.

### Concerns

- **MEDIUM:** Task 2’s acceptance criteria require `task test:int`, but its automated command omits it. The plan itself identifies the integration harness as part of the blast radius, so task completion can be reported before integration compilation occurs. [07-02-PLAN.md](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-02-PLAN.md:336)
- **LOW:** “Dependency-free” should consistently mean “no internal dependencies.” The package will still depend on `oops` because `ValidatePayload` returns a structured error.

### Suggestions

- Change Task 2 automation to `task build && task test && task test:int && task lint`.
- Describe the package as an “internal dependency leaf” rather than literally dependency-free.

### Risk Assessment

**MEDIUM.** The move is well specified, but the integration gate should be made task-local.

---

## 07-03 — Gateway value leaves

### Summary

The three leaves are sensible boundaries, and retaining documented forwarders for core-side callers keeps the blast radius controlled.

### Strengths

- Moving the coupled ULID entropy state as a unit preserves the clock-clamp invariant. The current state and its lock are explicitly coupled. [ulid.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/ulid.go:16)
- `ParseCommand` is a pure, tiny leaf and can move without domain coupling. [command.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/command.go:9)
- Moving the lease interval preserves the current gateway behavior while preventing future `internal/session` drift. [reaper.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/session/reaper.go:14)

### Concerns

- **MEDIUM:** The threat model says lexicographically inverted ULIDs break `Nats-Msg-Id` dedup identity. Dedup requires a stable, nonzero unique ID; it does not require ULID ordering. The publisher uses the event ID as identity and explicitly rejects only the zero ID. [07-03-PLAN.md](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-03-PLAN.md:339), [publisher.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/publisher.go:161)
- **LOW:** Task 3 requires `task test:int` but omits it from the task-local automated command. [07-03-PLAN.md](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-03-PLAN.md:285)

### Suggestions

- Rewrite the ULID rationale: monotonicity is a generator property retained for compatible session/cursor behavior; EventBus ordering is exclusively JetStream sequence.
- Add `task test:int` to Task 3’s automated gate.

### Risk Assessment

**MEDIUM.** Low implementation complexity, but the documentation must not revive ULID-as-ordering semantics.

---

## 07-04 — Gateway boundary gate and invariant binding

### Summary

This plan converts the boundary rule from a direct-import check into an actual closure property and binds the invariant to real assertions. It directly achieves ARCH-05’s enforcement requirement.

### Strengths

- The current gate only iterates `file.Imports`, so it cannot detect transitive domain reach. [gateway_imports_test.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/gateway_imports_test.go:148)
- Using `packages.NeedDeps`, checking `packages.PrintErrors`, and adding a positive control are appropriate protections against a partial-load false green.
- Fixing `internal/auth/service` to the real `internal/auth` package repairs a currently dead rule. [gateway_imports_test.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/gateway_imports_test.go:101)

### Concerns

- **LOW:** The positive control permanently assumes `internal/grpc` reaches `internal/store`. A future successful decomposition will cause a maintenance failure unrelated to gateway safety. The plan documents how to replace it, which limits the risk.

### Suggestions

- Prefer a tiny test fixture package with a deliberate forbidden transitive dependency if the repository tolerates test-only fixture packages.
- Otherwise retain the live control and its replace-don’t-delete comment as planned.

### Risk Assessment

**LOW.** Focused enforcement work with strong anti-vacuity checks.

---

## 07-05 — Move `core.Engine` to `internal/presence`

### Summary

The plan correctly preserves the two load-bearing behaviors in `core.Engine`: typed-nil rejection and audit-critical session termination using a fresh bounded context. The auth-side consumer interface is the right cycle breaker.

### Strengths

- The typed-nil guard is real behavior, not boilerplate; `NewEngine` uses reflection to catch typed-nil interface values. [engine.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/engine.go:39)
- `EndSession` deliberately ignores the caller’s cancelled context and selects the actor based on termination cause. [engine_end_session.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/engine_end_session.go:33)
- Having `internal/auth` declare the two methods it consumes avoids the verified `auth → presence → eventbus → auth` cycle while keeping the interface narrow.

### Concerns

- **LOW:** “Bounds its own publish at 5s regardless of the caller” is stronger than the interface can guarantee. It supplies a five-second context, but a noncompliant `Publisher` can ignore it. The production JetStream publisher does use the passed context, so production behavior is acceptable. [publisher.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/publisher.go:161)

### Suggestions

- Phrase the contract as “publishes with a fresh five-second deadline” rather than guaranteeing wall-clock completion for arbitrary publisher implementations.
- Keep the cancelled-context and typed-nil tests exactly as planned.

### Risk Assessment

**MEDIUM.** Cross-package and audit-sensitive, but the behavior transfer is carefully specified.

---

## 07-06 — Consolidate system broadcast construction

### Summary

The production design is good: one builder, consumer-owned interfaces, and a thin hostcap adapter. The current test-migration instructions, however, contain a concrete runtime failure.

### Strengths

- There are currently two independent builders for the same `{"message": ...}` payload: command services and hostcap. [types.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/command/types.go:622), [system_broadcaster.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/hostcap/system_broadcaster.go:43)
- Keeping `DisconnectSession` fail-closed preserves the existing Lua capability behavior. [system_broadcaster.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/hostcap/system_broadcaster.go:64)
- The plan correctly retains command’s error-swallowing/logging behavior rather than changing the handler contract.

### Concerns

- **HIGH — blocker:** The plan changes the parity test’s capture type to `eventbus.Event` but instructs the executor to leave the surrounding type and actor assertions “unchanged.” [07-06-PLAN.md](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-06-PLAN.md:249)

  Those assertions are not type-compatible semantically:

  - `eventbus.Event.Type` is `eventbus.Type`, not `eventvocab.EventType`.
  - `eventbus.Actor.Kind` uses EventBus constants; system is value `3`, whereas `core.ActorSystem` is value `1`.
  - `eventbus.Actor.ID` is `ulid.ULID`, while `core.ActorSystemID` is a string. [eventbus types](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/types.go:79), [core actor](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/event.go:143)

  Gomega accepts these as `any`, so the test compiles and fails at runtime—an especially deceptive defect.

### Suggestions

Update the migrated parity assertions to:

- `ev.Type == eventbus.Type("system")` or the validated `eventbus.NewType(...)` result.
- `ev.Actor.Kind == eventbus.ActorKindSystem`.
- `ev.Actor.ID == core.SystemActorULID`.
- Retain the exact qualified subject and payload assertions.

### Risk Assessment

**HIGH until corrected.** The implementation architecture is sound, but the mandated integration test is currently specified to fail.

---

## 07-07 — Collapse `core.Event` into `eventbus.Event`

### Summary

This is a strong plan for the actual ARCH-04 collapse. It identifies both HistoryReader interfaces, preserves Lua/binary symmetry, explicitly replaces CoreServer’s publisher seam, and protects the audit rendering header.

### Strengths

- `eventbus.Event` is the correct survivor because it owns host-only `Seq` and the package-private audit-row seam. [types.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/types.go:141)
- Replacing `WithEventStore` with a publisher plus game-ID provider prevents silent command-response loss. CoreServer currently publishes only through its `eventStore` option. [sub_grpc.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/sub_grpc.go:521)
- Requiring the wrapped rendering publisher is essential: the audit projection rejects events missing `App-Rendering`. [projection.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/audit/projection.go:382)
- Zero-aware actor-ID formatting correctly preserves today’s empty-string behavior for an absent actor.

### Concerns

- **MEDIUM:** The change is intrinsically high-blast-radius: two interfaces, four implementations, four fakes, CoreServer options, integration harness adapters, and security-sensitive actor translation. There is no additional design defect evident, but this should not be treated as mechanical.
- **LOW:** The plan should explicitly name the final integration test file for the `command_response → events_audit` assertion, rather than leaving its placement implicit.

### Suggestions

- Name the exact integration test and query used to prove command-response audit persistence.
- Keep the crypto-reviewer gate mandatory because the surviving actor bridge remains in `internal/plugin/event_emitter.go`.
- Land this as one green commit or a tightly controlled sequence with every intermediate commit compiling.

### Risk Assessment

**HIGH.** Well planned, but it touches the central event, plugin, audit, and integration seams simultaneously.

---

## 07-08 — Seq-correct plugin history pagination

### Summary

The bug analysis and production fix are correct. The plan properly threads `(seq,id)` through both Lua and binary paths and preserves the oldest-event backward-pagination anchor. The deterministic inversion test needs a sanctioned ID-construction mechanism.

### Strengths

- The current query contract explicitly says pagination is sequence-based and that zero sequence means tail/start. [bus.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/bus.go:87)
- The live adapter supplies only `BeforeID`, while the hot tier advances only when `BeforeSeq > 0`; therefore the quiet-stream repeat reproduction is valid. [sub_grpc.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/sub_grpc.go:928), [hot_jetstream.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/history/hot_jetstream.go:334)
- The plan correctly fixes both Lua encode sites, not just per-event cursors. [stdlib_focus.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/hostfunc/stdlib_focus.go:437)
- Keeping index `0` as the oldest ascending event is correct. [servers.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/hostcap/servers.go:900)

### Concerns

- **HIGH — blocker:** Spec B requires precomputed descending ULIDs, but the plan never defines how those IDs are placed onto events without violating the project’s canonical-construction rule. [07-08-PLAN.md](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-08-PLAN.md:355)

  `eventbus.NewEvent` always stamps a new `core.NewULID()`, and production rules prohibit manually supplied event IDs. [types.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/types.go:206) The publisher then serializes the event’s existing ID directly. [publisher.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/publisher.go:187)

- **LOW:** Task 1’s done text still calls Spec B a “concurrent-publisher spec,” although the action correctly replaced concurrency with deterministic sequential inversion. [07-08-PLAN.md](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-08-PLAN.md:409)

### Suggestions

- Add a plan-settled, test-only seam such as `eventbustest.NewEventWithID`, with explicit justification and a production-code prohibition.
- Alternatively publish a deliberately constructed wire envelope through a test helper that is clearly outside production event construction.
- Update the stale “concurrent-publisher” wording.

### Risk Assessment

**HIGH until the deterministic-ID seam is settled.** The production fix is otherwise excellent.

---

## 07-09 — Remove eager starts and introduce provider wiring

### Summary

The plan correctly diagnoses the eager-start failure class and moves live-resource resolution behind subsystem lifecycle boundaries. However, it also introduces a separate persisted namespace migration that exceeds the phase’s behavior-preserving contract.

### Strengths

- The five eager-start paths are real: database and EventBus are started manually before `StartAll`, and ownership is split between bootstrap and the orchestrator. [core.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/core.go:281), [core.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/core.go:454)
- Building a real TLS subsystem is well aligned with the existing `SubsystemTLS` identifier and makes the database dependency explicit.
- The KEK-wired boot regression is the correct behavioral gate because the problematic admin/crypto path is production-shaped only when crypto is active.

### Concerns

- **HIGH — scope/upgrade blocker:** The plan explicitly changes the effective EventBus subject namespace from `events.main.*` to `events.<db-ulid>.*` for existing installs with no global `game_id`, and accepts making old JetStream/audit history unreachable through exact-subject queries. [07-09-PLAN.md](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-09-PLAN.md:482)

  This is confirmed by current behavior:

  - `event_bus.game_id` defaults to `main`. [config.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/config.go:168)
  - That default is frozen at EventBus construction. [subsystem.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/subsystem.go:68)
  - The process separately derives the global ID from PostgreSQL. [core.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/core.go:300)

  This is a real data-addressing behavior change, not bootstrap decomposition. It also makes an existing configuration key dead on production boot.

- **MEDIUM:** The memoized `cryptoWiring` becomes an all-or-nothing service locator shared by five subsystems. The dependency-superset test mitigates missing edges, but it increases coupling and makes the first consumer responsible for a large, failure-cached construction.

### Suggestions

- Remove the EventBus game-ID migration from Phase 7. Keep EventBus on its configured/default namespace while moving resolution timing.
- Track namespace unification separately with an operator-visible migration, dual-read strategy, or explicit preflight requiring `game_id`.
- If retained here, amend the phase requirements and success criteria to acknowledge a deliberate behavior/data-namespace change; documentation alone is insufficient.

### Risk Assessment

**HIGH.** It solves the eager-start problem but also performs an unscoped, non-backward-compatible namespace cutover.

---

## 07-10 — Bounded shutdown and graph pinning

### Summary

The dependency-edge and topology work is strong. The shutdown mechanism returns on deadline even if a subsystem violates its contract, but using the same asynchronous-abandon behavior for non-terminal rollback is unsafe.

### Strengths

- The plan correctly catches the Go defer-timing trap and creates the timeout inside the deferred closure rather than at boot.
- Adding the missing gRPC → AuditProjection edge directly addresses a real audit window; gRPC currently depends only on Bootstrap, Sessions, Auth, and EventBus. [sub_grpc.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/sub_grpc.go:166)
- Pinning the real production dependency graph and adding an acyclicity test is valuable; the current comment claiming the verifier runs before EventBus is not enforced by registration order. [core.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/core.go:1473)

### Concerns

- **HIGH:** `StopAll` launches `Stop` in a goroutine and returns when the deadline expires. The plan itself acknowledges that `StartAll` rollback is not necessarily terminal. [07-10-PLAN.md](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-10-PLAN.md:309)

  After rollback returns, the abandoned goroutine can still mutate subsystem fields while tests—or a future retry—construct or start replacement state. A buffered result channel prevents a channel-send leak, but it does not prevent teardown/start races. The current orchestrator performs synchronous rollback, so this is a new semantic risk. [orchestrator.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/lifecycle/orchestrator.go:58)

- **LOW:** Task 1’s acceptance requires `task test:int`, but its automated command omits it. [07-10-PLAN.md](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-10-PLAN.md:389)

### Suggestions

- Split terminal shutdown from rollback:

  - `ShutdownAll` may abandon misbehaving stops at process exit.
  - Rollback must either wait for teardown, mark the orchestrator permanently unusable after timeout, or expose completion handles that prevent restart until abandoned stops finish.

- Add a test proving a timed-out rollback cannot be followed by `StartAll` or registration reuse while stop goroutines remain.
- Add `task test:int` to Task 1 automation.

### Risk Assessment

**HIGH until rollback semantics are separated from terminal shutdown.**

---

## 07-11 — Prepare/Activate lifecycle split

### Summary

The two-sweep design is coherent for host-owned listeners and loops, and the rollback/idempotency analysis is substantially better than a mechanical interface rename. Two verified source conditions still invalidate parts of the plan.

### Strengths

- Two complete sweeps enforce that no `Activate` occurs before all `Prepare` calls complete.
- Including the failing `Prepare` target in rollback fixes a real gap: the current orchestrator records a subsystem only after `Start` succeeds, so partial acquisition by the failing subsystem is never cleaned. [orchestrator.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/lifecycle/orchestrator.go:58)
- The gRPC reaper split is correct: both reaper goroutines currently start before the TCP listener and must move to `Activate`. [sub_grpc.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/sub_grpc.go:758)
- The audit plan correctly recognizes that the existing `worker != nil` guard cannot guard a prepared-only state. [audit subsystem](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/audit/subsystem.go:267)

### Concerns

- **HIGH — partial-Prepare leak:** `PluginSubsystem.Prepare` is assigned the entire current `Start` body and guarded by a field populated only later. But `goplugin.NewHost` immediately launches a token-store goroutine. [host.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/goplugin/host.go:342)

  If middleware construction or another step before `s.manager` assignment fails:

  - `cleanupOnError` closes alias/schema/world resources but not `binaryHost`. [plugin subsystem](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/setup/subsystem.go:234)
  - `Stop` returns immediately while `s.manager == nil`. [plugin subsystem](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/setup/subsystem.go:449)

  The newly specified partially-prepared rollback contract therefore does not hold for this subsystem.

- **HIGH — barrier overclaim:** The plan says no domain work loop runs before every subsystem prepares, yet plugin preparation constructs a binary host, launches its token-store loop, starts subprocesses, broker proxy goroutines, and invokes plugin initialization. [host.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/goplugin/host.go:782), [host.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/goplugin/host.go:980)

  Even if the token-store loop is classified as infrastructure, plugin-side initialization is outside the orchestrator’s ability to enforce. The structural guarantee applies only to host-owned `Activate` bodies.

- **MEDIUM:** The audit prepared-only rollback language says it “releases durable-consumer state,” but `newProjection` creates a durable JetStream consumer, while `projection.drain` is a no-op before `Consume` creates `cc`. [projection.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/audit/projection.go:108), [projection.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/audit/projection.go:467)

  The plan should explicitly decide whether durable consumers persist across failed preparation—which is likely correct—or are deleted. “Drain” does not delete them.

### Suggestions

- Add `binaryHost` or an equivalent closer to prepared plugin state immediately after construction; `Stop` and every error path must close it even before manager assignment.
- Add a fault-injection test for failure immediately after `goplugin.NewHost`, proving the token-store goroutine and any subprocess are closed.
- Narrow the guarantee to: “No host-owned externally reachable domain listener and no host-owned domain work loop starts before all subsystems prepare.”
- Clarify that audit rollback clears live in-memory/consume state but intentionally retains idempotently provisioned durable consumers unless deletion is explicitly designed.
- Re-run the caller census after upstream plans land; this plan’s ~54-file atomic migration remains highly drift-sensitive.

### Risk Assessment

**HIGH.** The lifecycle design is thoughtful, but plugin partial-state cleanup and the overstated barrier must be resolved before implementation.

---

## Final recommendation

Revise **07-06, 07-08, 07-09, 07-10, and 07-11**, then re-run plan review. The most urgent corrections are:

1. Fix 07-06’s migrated parity assertions.
2. Define 07-08’s sanctioned deterministic-ID seam.
3. Remove or formally scope/migrate 07-09’s subject-namespace cutover.
4. Separate terminal shutdown abandonment from rollback in 07-10.
5. Add plugin prepared-state cleanup and narrow 07-11’s lifecycle guarantee.
---

## OpenCode Review

Verifying load-bearing plan claims against the live codebase before scoring.
# Phase 7 Cross-AI Plan Review

**Verified against live tree @ `1e636f37b`.** Claims below site-checked with `go list -deps` / source reads; this is not plan-text-only.

---

## Summary

Phase 7 plans are **execution-ready at a high bar**: they collapse ARCH-04/03/05 into sequenced waves, bake in prior research findings (auth↔eventbus cycle, gateway `forbidden` phantom, quiet-stream hist bug, MEDIUM-11 cycle trap, DontListen obstacle), and gate each wave with mechanical greps + `task test:int`. Parallel pairs **07-03∥07-05** and **07-04∥07-06** have **empty `files_modified` intersections** (checked). Remaining risk is primarily **execution mass** (07-09/07-11 super-atomicity) and one **operational subject-namespace flip** (EventBus `game_id`), not a missing architectural decision.

---

## Verified foundations (evidence)

| Claim in plans | Live evidence |
|---|---|
| Telnet closes domain via `internal/grpc` client | `cmd/holomush/gateway.go:20,152–153`; `go list -deps ./internal/telnet` → **47** internals; `internal/grpc/client.go` has `TranslateSubscribeErr` |
| `gateway.go` not core-only | not in `coreOnlyFiles` (`gateway_imports_test.go:22+`) |
| `forbidden` phantom `internal/auth/service` | `gateway_imports_test.go:107`; package is `internal/auth/*` |
| INV-EVENTBUS-1 pending + stale `INV-GW-1` token | `docs/architecture/invariants.yaml:2340–2348` |
| `session` / `core` already leaves | `go list -deps` → only self |
| eventbus closure includes `auth` (FINDING-1) | `go list -deps ./internal/eventbus` retains `internal/auth` |
| Seq destroyed & cursor hardcoded 0 | `busEventToCoreEvent` `sub_grpc.go:976+`; `encodeHostEventCursor` Seq:0 `hostcap/servers.go:1290`; hostfunc **two** Seq:0 sites `stdlib_focus.go:441,463` |
| Quiet multipage how-before-seq is broken | `bus.go:89–104` (ID = tripwire); `matchesQuery` has **no BeforeID** (`hot_jetstream.go:392–406`); adapter sets only `BeforeID` (`sub_grpc.go:932–935`) |
| next_cursor = oldest `[0]` | `servers.go:905`; `stdlib_focus.go:463` |
| Eager starts + MEDIUM-11 comment | `core.go:287,462,797,800`; `:1105` unbounded StopAll; `:1476` “runs before EventBus”; `productionSubsystems` **16** params |
| EventBus DependsOn nil today | `subsystem.go:77` |
| DontListen + server Start atomic | `subsystem.go:153–164` |
| cluster Start off-file | `heartbeat.go:22` |
| guest reaper hard 1m | `sub_grpc.go:765–766` |
| dual HistoryReader structural typing | `host.go:136` + `stdlib_focus.go:50`; `lua/hostcap_adapter.go:226–228` |
| StartAll rollback inherits startup ctx | `orchestrator.go` StartAll → `o.StopAll(ctx)` on failure |

---

## Per-plan assessments

### 07-01 — `grpcclient` extract (ARCH-05 steps)

**Summary:** Correct first move; ~85% of gateway closure win.

**Strengths**
- Destination package is a true leaf candidate (client.go imports only proto/grpc/oops).
- Symbol-first census + dual-use `phase1_5_test.go` rewiring (`:297,:374,:529` client vs server) match a real miss mode.
- Closes D-17 fail-without-extract (`gateway.go` not allowlisted).

**Concerns**
- **LOW** — 23-file blast is large but predominantly import rewrites; still needs green `task test:int` in-task (called out correctly after round 6).

**Risk:** **LOW** (design) / **MEDIUM** (integration surface)

---

### 07-02 — `eventvocab` leaf (D-05)

**Summary:** Correctly separates wire vocabulary from `eventbus.Event` so ARCH-04 does not re-poison ARCH-05.

**Strengths**
- Scope re-cut: **does not** claim zero `internal/core` in gateway (ParseCommand/NewULID stay until 07-03).
- CommandResponsePayload properly treated as vocab, not presence cargo.
- Re-census command for 34+ consumers; EventStore.Append doc reword avoids stale API names (`event.go:20–24`).

**Concerns**
- **LOW** — `files_modified` frontmatter is comment-noisy; executors must treat body/census grep as SoT if tooling strips YAML comments poorly.

**Risk:** **LOW**

---

### 07-03 — gateway leaves (`ulidgen` / `cmdparse` / `sessionlease`)

**Summary:** Completes ARCH-05 leaf work; honest drift-prevention framing for forbidding already-leaf packages.

**Strengths**
- Forwarder on `core.NewULID` preserves eventbus→core edge (D-03).
- End-of-plan zero-closure gate for core/session correctly **owns** what 07-02 dropped.
- Naming left alone after leaf check.

**Concerns**
- **LOW** — hindermost gate is brittle if some unnoticed non-leaf import remains; plan correctly says stop and fix here, not in 07-04.

**Risk:** **LOW–MEDIUM**

---

### 07-04 — forbidden + closure gate + bind INV-EVENTBUS-1

**Summary:** Makes ARCH-05 durable and binding genuine (partial binding avoided by dual `asserted_by`).

**Strengths**
- Direct-onlyodor of `checkFile` closed by NeedDeps walk + PrintErrors + positive control on `internal/grpc`→store.
- Shared `gatewayForbiddenPackages` list (no dual policy).
- D-18 obeyed (no `INV-GW-1` rename); phantom `auth/service` fixed in both list and YAML.

**Concerns**
- **MEDIUM** — live positive control on `internal/grpc`→store can go **false-red** if a future phase legitimately leaf-ifies grpc; plan documents replace-don't-delete (good), reviewers must keep that instruction.

**Risk:** **LOW** (for this phase)

---

### 07-05 — `presence.Emitter` + auth interface (FINDING-1)

**Summary:** Load-bearing ARCH-04 precursor; settlements match code (Engine logic, gameID function, typed-nil).

**Strengths**
- Consumer interface only what auth calls (Leave + SessionEnded; Arrive not used — `auth_service.go:235,243`).
- Qualifies via same shape as `busHistoryReaderAdapter` (`sub_grpc.go:906–924`: `gameID func()`, `""→main`).
- Carries EndSession ctx-ignore + 5s timeout (must not drop).

**Concerns**
- **MEDIUM** — subject/payload byte-identity vs Engine+busEventAppender needs strong unit pins (literal subjects); plan requires them.
- **LOW** — operation label rename (`append_*`→`publish_*`) must not rename error **code** `SESSION_ENDED_APPEND_FAILED` (called out).

**Risk:** **MEDIUM** (cycle/refactor)

---

### 07-06 — one `sysbroadcast` builder

**Summary:** Correct area for D-01/D-02; intentional subject representation change in tests is well-flagged.

**Strengths**
- Command never imports eventbus/sysbroadcast; consumer port only.
- Marketplace shape de-dups true parenthetical at `hostcap/system_broadcaster.go:46`.
- FINDING-5 wired with **two-arg** `NewBroadcaster` (publisher + gameID) — earlier wording bug fixed.
- Scoped “one marshal site” criteriа (not repo-wide) fixed after false red on unrelated `json.Marshal(map…)`.

**Concerns**
- **LOW** — parity test rewrite (`Stream`→qualified `Subject`) is behavior-preserving only if harness captures bus events; plan requires literal pins.

**Risk:** **LOW–MEDIUM**

---

### 07-07 — delete `core.Event` / bridges / rule amend

**Summary:** ARCH-04 core; heavily revised and aligned with a live emit path.

**Strengths**
- Re-types **both** HistoryReaders in lockstep (symmetry).
- CoreServer → `WithEventPublisher(pub, gameID)` not silent “construct event only”; nil publisher no-op kept (`server.go:638–641` today).
- Wrapped publisher (`wrapPublisher` at `sub_grpc.go:270,522`) explicitly called out for audit header — real persistence hazard.
- Zero-aware actor ID for plugins; one general bridge stays private to plugins.
- Rules amended same change as symbol delete.

**Concerns**
- **MEDIUM** — measured by mass (30+ files) + crypto-reviewer surface (`event_emitter.go`); fails closed only if `task test:int` (crypto + pluginparity) is non-optional green.
- **LOW** — must `gh issue` PROJECT.md/ARCHITECTURE event-sourcing drift (explicitly deferred; do not dirty Phase 7 scope).

**Risk:** **MEDIUM**

---

### 07-08 — D-07 seq pagination (behavior change + D-08 guard)

**Summary:** Correctness plan; original framing (only concurrent) correctly **retracted**; quiet multipage RED is real.

**Strengths**
- Test lives in `package main` where unexported adapter lives.
- Page anchor = oldest `[0]` documented with exclusive-`BeforeSeq` mechanics.
- Lua path has **two** Seq:0 encode sites; decode currently **drops Seq** (`stdlib_focus.go:368–389` only `beforeID`) — plan enumerates all three.
- Legacy zero-seq = tail restart (status quo), no fictional ID-only fallback.
- Spec B deterministic inverted ULID lex order vs stream seq (not racey concurrent).

**Concerns**
- **MEDIUM** — must observe Spec A **RED before GREEN** or the remaining cycle is unproven.
- **LOW** — cold-tier `BeforeSeq`-only behavior still tripwire-coupled; keep `BeforeID` with `BeforeSeq` as planned.

**Risk:** **MEDIUM** (real bugfix; good tests lower residual)

---

### 07-09 — Wave A handles / kill eager starts / TLS / named set

**Summary:** Design is now deep enough to implement without “executor invents dispositions”; residual risk is size and composition.

**Strengths**
- Correct diagnosis: all five eager starts are live-resource constructors.
- `cryptoWiring` = memoized **resolution**, subsystem **constructors stay pre-StartAll** (BLOCKER-2 fix).
- THE RULE (consumer DependsOn ⊇ wiring deps) + second cycle ban (ABAC not a consumer).
- gameID funnel + EventBus itself into provider (closes `events.main.*` vs `events.<ulid>.*` split) with warn-on-stale `event_bus.game_id`.
- cluster Start is in `heartbeat.go:22` (called out).
- T-07-51 re-scope: forward edges **surfaces → verifier**, not reverse EventBus→verifier.
- KEK boot gate via **integration** E2Es (not vacuous `task test -run`).

**Concerns**
- **HIGH (execution):** largest compressive design in the phase; “stop and report” on census gaps is correct but can stall autonomy.
- **MEDIUM (ops):** subject-namespace flip for installs with implicit `game_id` — plan accepts/documents; escape hatch `game_id: main` must appear in SUMMARY/ops note, not only PLAN.
- **MEDIUM:** THE RULE + many dual-path providers best-effort will fight I-am-green-if-compile-only; task gates are heavy but necessary.
- **LOW:** proposed A/B commit split inside Task 2 — only safe if each commit builds.

**Risk:** **HIGH** operationally, **MEDIUM** if design held under executing polls

---

### 07-10 — StopAll deadline, MEDIUM-11 truth, AuditProjection edge, topo + acyclicity

**Summary:** Excellent post-mortеm of prior self-report (rev-4 reverse edge cycle); settlements match current code for why reverse edge cannot exist.

**Strengths**
- Closure-defer for 5s shutdown (`core.go:255–261` precedent); never arm timeout at boot.
- StopAll deadline + hang abandonment + **buffered** result channel.
- Rollback fresh ctx in **this** plan (D-12: Wave B deferrable).
- MEDIUM-11 closed by **comment deletion + pin**, not unbootable edge; acyclicity test is the durable ensino for ramp-on plan interactions.
- gRPC gains real `AuditProjection` (`DependsOn` currently omits it at `sub_grpc.go:170–176`).

**Concerns**
- **MEDIUM** — topo pin after 07-09 EventBus→Database changes seed set; plan says derive live sequence, but human RED checklist heavy.
- **LOW** — residual accept on NATS monitor / external dial pre-verifystan is subjective; adequately written as residual not silent ignore.

**Risk:** **MEDIUM**

---

### 07-11 — Prepare/Activate Wave B

**Summary:** D-13.0/1/2/3 settlements fix an earlier incoherent universal barrier; this is the right structural enforcement of acquire-before-serve.

**Strengths**
- DontListen reframes “serve” (cannot put server Start in Activate without starving audit).
- Two full sweeps + single Stop teardown; rollback includes failed method target (append-before-Prepare).
- 17-row Prepare/Activate + idempotency tables decide, not implementer.
- Side-note seams called honestly: admin.lock atomic in Activate only; plugin Activate no-op; reapers construct/activate placement; guest interval inject.
- Caller blast (~27) treated as first-class (integration blindness).

**Concerns**
- **HIGH (execution):** ~54-file atomic interface break; one green commit or dead branch for days.
- **MEDIUM** — review-enforced “no domain bind in Prepare” is residual hole (threat table admits); only code review + SUMMARY table, not compile rule.
- **MEDIUM** — dual readiness of audit prepared aggregate (lateInit/owners) is subtle; under-tested would leak consumers.
- **LOW** — property test can be green while a single Activate still mis-placed if recording stubs diverge from real bodies; plan mitigates with four focused real-subsystem tests.

**Risk:** **HIGH** operationally, design **MEDIUM**

---

## Cross-cutting strengths

- Decision canonicity: MODEL-01 benefit is live wire already `eventbus.Event`; collapse is delete + retype, no schema bump (`AppSchemaVersion = 1`).
- Cycles solved with consumer-defined interfaces (auth, command) rather than package inversion of ULID/VerbRegistry.
- Enforcement asymmetry fixed: AST + transitive closure for gateway.
- D-07/D-08 carefully separated: correctness without plugin contract break.
- Wave A alone useful if Wave B delayed (D-12 honored).
- Integration landmine and KEK-false-green outed repeatedly — necessary discipline.

---

## Cross-cutting concerns

| Sev | Issue |
|-----|--------|
| **HIGH** | **07-09 + 07-11 size** — cognitive + WIP merge risk for autonomous execution; bisect depends on green intermediate commits only |
| **HIGH** | **game_id subject flip** (07-09 EventBus into provider) — sandbox history under exact-subject filters may go invisible; ops note + escape hatch must ship in SUMMARY, not only PLAN prose |
| **MEDIUM** | **Full `task test:int` tax** every shared-type wave (01/02/05–08, especially 07/08); skipping = silent harness drift (`harness.go` still mirrors production) |
| **MEDIUM** | Residual **observability bind / external dial** before chain verify (accepted) can look like incomplete T-07-51 in adversarial review |
| **MEDIUM** | **crypto-reviewer** mandatory on 07-07/07-08 surfaces; schedule before push, not after |
| **LOW** | Frontmatter/`files_modified` YAML hotspots (comments mid-list) may under-count for tooling; census greps in tasks are true SoT |
| **LOW** | Always-loaded rules + host comments that still say ULID order for history (`ReplayTail` doc `sub_grpc.go:914–915`) need the same **stale doc** discipline as CLAUDE.md |

---

## Suggestions

1. **Before execute:** one dry run of Step 0 censuses for 07-01/07-09/07-11 only; paste hit counts into starting SUMMARY stubs.
2. **Land 07-09 immediately followed by** a sandbox smoke (boot+KEK + subject quality sample) before 07-10 so EventBus provider divergences fail early.
3. **Keep Wave A release train:** 07-09 green is sensoriously complete even if 07-11 slips (plan promises this; pipeline/PR structure should reflect it).
4. **File MODEL-02 drip** (`PROJECT.md` / `ARCHITECTURE.md`) as issue in 07-07 as planned—do not re-scope.
5. Treat **Spec A RED transcript** (07-08) and **acyclicity RED vs rev-4 cycle** (07-10 Task 4) as merge-blocking evidence artifacts.
6. Optional later (not Phase 7): GO analysis AST check that no `Prepare` binds `:net` domain listeners—harder residual in T-07-62.

---

## Phase goal achievement

| Success criterion | Plan coverage | Gap? |
|---|---|---|
| Single Event type; no `core.Event`/`eventbus.Event` split | 07-02→07-07 (+ D-07 in 07-08) | No design gap |
| Bootstrap via Orchestrator; start/stop “unchanged” (player-observable) | 07-09 Wave A → 07-11 Wave B | Order *changes* deliberately; player SEL should hold if tests hold |
| Gateway protocol-only; zero boundary violations | 07-01→07-04 | No design gap |

**D-07 is intentional behavior change** (pages stop repeating); phase “no behavior change” for ARCH-04 is wire/audit/gate parity—correctly carved.

---

## Overall risk assessment

**MEDIUM–HIGH**, skew **HIGH only on 07-09/07-11 execution load**, **MEDIUM on design soundness**.

Justification: Load-bearing mechanisms are source-verified; prior self-inconsistencies (FINDING-1 cycle, quiet-seq bug, reverse EventBus→Verifier cycle, DontListen barrier, partial ID-fallback fiction) are repaired in-plan rather than left to implementers. Residual risk is scale (monolithic refactors, int test burden, one namespace flip) rather than undecided architecture.

**Ready to execute** if:
1. Integrators accept Wave A ÷ Wave B as shippable slices, and  
2. Every wave runs **`task test:int`** + KEK admin E2Es where claimed, and  
3. 07-07/08 schedule `crypto-reviewer` before push.

No **new** architectural blocker found beyond what the plans already settle.

---

## Consensus Summary

**Verdict split (fifth consecutive round of depth-vs-breadth):** Codex says **NOT execution-ready** (5 material issues); OpenCode (grok-4.5) says **execution-ready at a high bar** (conditions: Wave A/Wave B treated as shippable slices, mandatory `task test:int` per shared-type wave, crypto-reviewer before push). Neither reviewer saw prior rounds. The orchestrator verified every Codex HIGH against source before this consensus.

### Orchestrator verification of the HIGHs

| # | Codex finding | Verdict | Evidence |
|---|---|---|---|
| 1 | **07-06 parity assertions runtime-fail after capture retype** | **CONFIRMED — blocker** | The plan mandates "Keep the surrounding `ev.Type` / `ev.Actor.Kind` / `ev.Actor.ID` assertions … **unchanged**" while the capture becomes `eventbus.Event`. Current assertions (`test/integration/pluginparity/session_admin_broadcast_test.go:79-83`) compare against `core.EventTypeSystem` (`core.EventType`), `core.ActorSystem` (value **1**, `internal/core/event.go:146-150`), and `core.ActorSystemID` (string) — but `eventbus.Type` is a distinct type, `eventbus.ActorKindSystem` is value **3** (`internal/eventbus/types.go:88-97`), and `eventbus.Actor.ID` is `ulid.ULID`. Gomega `Equal` takes `any`: the migrated test compiles and fails at runtime. Classic R-fix regression — the "keep unchanged" sentence arrived with the round-6 subject-assertion fix; 07-06 has been untouched since rev 9, so rounds 7-8 (scoped to other plans) never re-read it. |
| 2 | **07-08 Spec B has no sanctioned deterministic-ID seam** | **DOWNGRADED → MEDIUM** | The mechanism exists and is precedented in the exact tier: `test/integration/eventbus_e2e/suite_test.go:104` constructs `eventbus.Event{ID: ulid.MustNew(ulid.Timestamp(time.Now()), crand.Reader), …}` literals, and `internal/eventbus/publisher_test.go:129` overrides `ev.ID` after construction; the publisher rejects only the zero ID. No new seam is needed. The real gap is explicitness under this phase's zero-executor-decides bar: Spec B never names the construction, and `eventbus.NewEvent`'s doc comment MUSTs a raw-literal ban, so an executor could stall. Fix = one sentence naming the sanctioned idiom (NewEvent-then-override-ID, or the `suite_test.go:104` literal pattern). |
| 3 | **07-09 subject-namespace cutover is unscoped scope creep** | **REFUTED as blocker (settled disposition)** | The plan's rev-11 "Round 8 … SETTLED HERE" section states the exact consequence Codex describes, the **accept-and-document** disposition, its grounds (only live deployment is the dev sandbox with a restore runbook; `events_audit` rows remain SQL-queryable; zero-code escape hatch = set global `game_id: main`), a WARN when an explicit `event_bus.game_id` loses to the provider, and a SUMMARY-verbatim ops-visibility criterion. Codex — reviewing blind — independently re-derived round 8's finding, which confirms the issue is real, but the disposition was deliberately ratified in rev 11. LOW residual: phase-level success-criteria text doesn't name the deliberate namespace change (Codex's "amend the phase requirements" sliver). |
| 4 | **07-10 rollback races with abandoned Stop goroutines** | **PARTIALLY CONFIRMED → MEDIUM** | Mechanism real: `StopAll` abandons a hung `Stop` into a buffered-one-shot goroutine, and 07-11 makes `StartAll` rollback (non-terminal) call it — an abandoned teardown can outlive rollback's return. But the plan already acknowledges non-terminal rollback and bounds abandonment in the mandated doc comment ("one process exit + at most one boot-rollback"), and production topology contains it: a failed `StartAll` is followed by process exit; no retry path exists in-tree. Hardening fold-in, not a blocker: forbid orchestrator reuse after a timed-out rollback (flag + test) or state it as unsupported in the same doc comment. |
| 5a | **07-11 plugin partial-Prepare leak** | **CONFIRMED — blocker** | The plan's own contract: Stop is "Single teardown for prepared-only, partially-prepared, and activated subsystems" (07-11:173), rollback stops the failing subsystem "because a failed Prepare may have partially acquired resources" (:685), and Stop's doc contract is amended to cover partially-prepared (:810). That contract does not hold for `PluginSubsystem`: `Stop` no-ops while `s.manager == nil` (`internal/plugin/setup/subsystem.go:451-453`); `cleanupOnError` (:234-251) closes aliasPool/schemaProvisioner/worldConn but never `binaryHost` or `luaHost`; and `goplugin.NewHost` launches the token-store sweeper goroutine at construction (`internal/plugin/goplugin/host.go:345-358`). Three verified error paths between construction (:322) and `s.manager =` (:384) — `BINARY_HOST_MW_FAILED` (:333), `ALIAS_POOL_FAILED` (:341), manager-construction (:381) — leak the goroutine and Lua-host state, and the rollback Stop the plan promises releases nothing. Audit got exactly this prepared-aggregate treatment in rounds 7-8 (row 10); row 9 never did. Fix: close `binaryHost`/`luaHost` on those error paths (or make Stop handle the pre-manager state), plus a fault-injection test failing right after `goplugin.NewHost`. |
| 5b | **07-11 barrier overclaim** | **REFUTED (already scoped)** | D-13.0 (07-11:135-231) already states the barrier as "externally-reachable DOMAIN traffic + domain work loops," records the observability carve-out as falsifiable, and row 9 (:412) explicitly *decides* plugin subprocess launch is acquisition with stated grounds (host-controlled child over host-brokered mTLS; audit's Prepare depends on loaded manifests). Codex's suggested narrowing is essentially what D-13.0 already says. LOW residual: add "host-owned" to the work-loop wording, since plugin-side Init behavior is outside orchestrator enforcement. |

### Additional verified findings

- **Automation-vs-criteria `task test:int` mismatches (Codex MEDIUM/LOW; confirmed):** acceptance criteria require `task test:int` green but the task-local `<automated>` command omits it in 07-02 Task 2 (`:341`), 07-03 Task 3 (`:301`), and 07-10 Task 1 (`:392`). Mechanical one-line fixes; without them a task can report done before integration compilation runs.
- **07-11 row 10 "releases durable-consumer state" wording (Codex MEDIUM; confirmed):** `newProjection` creates a **durable** JetStream consumer at Prepare-time (`internal/eventbus/audit/projection.go:109-117`, `CreateOrUpdateConsumer` with `Durable`), and `drain` is a no-op before `Consume` exists (`projection.go:467-470`, `p.cc == nil` early return). A prepared-only Stop cannot "release" the durable — it persists in JetStream, and idempotent re-provision on retry is likely the *correct* behavior. The plan should say retained-not-deleted explicitly instead of "releases … durable-consumer state".
- **07-03 ULID rationale (Codex MEDIUM; agreed):** `Nats-Msg-Id` dedup needs a stable nonzero unique ID, not lex order (`internal/eventbus/publisher.go` rejects only the zero ID). Reword the threat model so it does not revive ULID-as-ordering semantics.

### Agreed strengths (both reviewers)

- **Grounding quality:** Codex calls the plans "unusually well-grounded"; OpenCode's verified-foundations table independently confirms the load-bearing claims (eager-start census, quiet-stream repeat mechanism, dual HistoryReader, DontListen barrier, cluster Start off-file).
- **07-01 through 07-05 are executable as written** — both score them LOW/MEDIUM with no design defects.
- **07-07's wrapped-publisher constraint** closes a real silent-audit hazard (both cite the `App-Rendering` rejection in `audit/projection.go` and the `wrapPublisher` path).
- **07-08's bug diagnosis is correct and the deterministic Spec B retraction was right** — both confirm the quiet-stream repeat is real and the concurrent-race construction was correctly replaced.
- **07-11's two-sweep design with rollback-includes-failing-subsystem** is coherent structural enforcement (Codex: "substantially better than a mechanical interface rename"; OpenCode: "the right structural enforcement of acquire-before-serve").

### Agreed concerns (both reviewers)

- **game_id namespace flip needs ops-surface visibility** (Codex HIGH-as-scope, OpenCode MEDIUM-as-ops): both want the escape hatch and consequence surfaced beyond plan prose. The plan already mandates a SUMMARY-verbatim reproduction; the residual delta is phase-level success-criteria wording (LOW, see verification row 3).
- **07-09/07-11 execution mass** (Codex: "should not be treated as mechanical" / "~54-file atomic migration remains highly drift-sensitive"; OpenCode: HIGH execution risk on both): single-green-commit discipline and post-upstream census re-runs are the mitigation both endorse.
- **`task test:int` as the fail-closed gate on every shared-type wave** — Codex wants it task-local everywhere it's an acceptance criterion; OpenCode calls skipping it "silent harness drift".
- **crypto-reviewer must run before push** on the 07-07/07-08 surfaces (`internal/plugin/event_emitter.go` bridge survives there).

### Divergent views

- **Overall verdict:** Codex revise-five-plans-first vs OpenCode ready-with-conditions. Orchestrator weighting (grounding over headcount, as in rounds 5-8): two Codex HIGHs survive verification as genuine blockers, so rev 12 precedes execution.
- **07-06:** Codex HIGH (runtime-failing assertions) vs OpenCode LOW ("plan requires literal pins"). OpenCode missed the cross-type assertion mismatch; verification sides with Codex.
- **07-11:** Codex HIGH (partial-Prepare leak) vs OpenCode design-MEDIUM (no leak finding). Verification sides with Codex on 5a; sides with the plan against Codex on 5b (barrier already scoped).
- **07-10:** Codex HIGH (rollback semantics) vs OpenCode MEDIUM (topo-pin recompute burden). Verification lands between: real mechanism, bounded blast radius, fold-in hardening.

### Recommendation

**Rev 12, scoped to two confirmed blockers plus mechanical fold-ins — then execute.**

1. **07-06 (blocker):** retype the three migrated parity assertions to eventbus values (`eventbus.Type("system")` / `eventbus.ActorKindSystem` / `core.SystemActorULID`), keeping the subject/payload/HaveLen assertions as planned.
2. **07-11 (blocker):** add plugin prepared-state cleanup — close `binaryHost` (token-store goroutine) and `luaHost` on the three pre-manager error paths or extend Stop to the pre-manager state — plus a fault-injection test; also the two wording fixes (durables retained-not-deleted; "host-owned" work loops).
3. **Fold-ins (non-blocking):** 07-08 one-sentence sanctioned ID construction; 07-02/07-03/07-10 automation-gate one-liners; 07-03 ULID rationale reword; 07-10 rollback-reuse hardening sentence/test; 07-09 phase-criteria namespace-change mention.

No design reversals surfaced anywhere; every rev-9→11 fix held (neither reviewer re-found a prior-round fix as broken — Codex's 07-09 finding is a blind re-derivation of the settled round-8 disposition, not a regression). The two confirmed blockers live in plan surfaces untouched since rev 9 (07-06) and a subsystem row that never received the audit-style prepared-aggregate treatment (07-11 row 9) — consistent with the round-6 lesson that convergence is measured by coverage, not streak.
