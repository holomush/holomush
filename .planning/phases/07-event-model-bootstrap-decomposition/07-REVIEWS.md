---
phase: 7
round: 10
reviewers: [codex]
reviewed_at: 2026-07-17T03:16:38Z
plans_reviewed: [07-01-PLAN.md, 07-02-PLAN.md, 07-03-PLAN.md, 07-04-PLAN.md, 07-05-PLAN.md, 07-06-PLAN.md, 07-07-PLAN.md, 07-08-PLAN.md, 07-09-PLAN.md, 07-10-PLAN.md, 07-11-PLAN.md]
plans_revision: "rev 12 (9d0c4095d)"
notes: "Round 9 preserved at 9303b9fb9. FULL round: reviewers received all 11 plans with the standard source-grounded prompt; no prior-round findings shared. codex: default model. The second lane (opencode, openrouter/moonshotai/kimi-k3 per user request) was invoked THREE times and every attempt died on a provider 429 rate_limit_exceeded mid-review (280K/64K/285K tokens of agentic verification each) — the lane is recorded as FAILED, not substituted with another model. Consensus therefore rests on codex plus the orchestrator's source verification of every codex HIGH."
---

# Cross-AI Plan Review — Phase 7 (Round 10, rev 12, full set)

## Codex Review

# Cross-AI Plan Review — Phase 7

## Overall assessment

**Verdict: NOT READY to execute as written.**

The architecture is unusually well researched: import cycles, cursor semantics, plugin-runtime symmetry, rollback states, and lifecycle ordering are grounded in live code. However, several plans contain task boundaries or verification commands that cannot pass before later tasks run. The most serious blockers are in 07-01, 07-04, 07-05, 07-06, 07-07, 07-09, and 07-11.

Once those execution defects are corrected, the phase remains **MEDIUM-HIGH implementation risk** because it combines a repo-wide Event type migration with a 17-subsystem lifecycle rewrite.

---

## 07-01 — Extract `internal/grpcclient`

### Summary

The package boundary is correct and materially improves ARCH-05, but Task 1 cannot satisfy its own build gate because it deletes the old client before Task 2 rewires its callers.

### Strengths

- `internal/grpc/client.go` is genuinely suitable for a leaf move: its imports are protobuf/grpc/telemetry libraries, with no `internal/...` dependency ([client.go:5](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/grpc/client.go:5)).
- The plan preserves the important `SESSION_NOT_FOUND` versus `RPC_FAILED` mapping implemented by `TranslateSubscribeErr` ([client.go:114](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/grpc/client.go:114)).
- The symbol-first census correctly handles alternate aliases such as `grpcpkg` in the integration suite ([phase1_5_test.go:27](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/test/integration/phase1_5_test.go:27)).

### Concerns

- **HIGH — Task 1 is not independently executable.** It deletes `internal/grpc/client.go` at [07-01-PLAN.md:148](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-01-PLAN.md:148), then requires `task build` at [line 174](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-01-PLAN.md:174). Caller rewiring does not occur until Task 2. Meanwhile `cmd/holomush/gateway.go` still imports `internal/grpc` and calls `holoGRPC.NewClient` ([gateway.go:20](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/gateway.go:20), [gateway.go:152](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/gateway.go:152)).
- **HIGH — `git status --porcelain` cannot be clean before the task’s intended edits are committed**, yet it is a mandatory acceptance criterion at [07-01-PLAN.md:260](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-01-PLAN.md:260).

### Suggestions

- Merge Tasks 1 and 2 into one atomic task, or retain temporary forwarding aliases in `internal/grpc` until every caller is rewired.
- Replace the clean-worktree criterion with `task fmt` plus a formatter/check target that does not reject the intended task diff.

### Risk assessment

**HIGH as written; MEDIUM after task-boundary repair.**

---

## 07-02 — Extract `internal/eventvocab`

### Summary

The design cleanly separates wire vocabulary from the surviving Event representation. Its source census and integration coverage are strong; the main problem is an impossible cleanliness acceptance criterion.

### Strengths

- The proposed leaf corresponds to a coherent existing block: payload-size validation, nine event-type strings, and wire payload structures currently live together in `internal/core/event.go` ([event.go:14](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/event.go:14), [event.go:35](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/event.go:35), [event.go:63](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/event.go:63)).
- Exact string and JSON-tag tests protect actual wire compatibility rather than merely compilation.
- Task 2 includes full build, unit, integration, and lint gates ([07-02-PLAN.md:334](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-02-PLAN.md:334)).

### Concerns

- **HIGH — Mandatory acceptance cannot pass:** Task 2 intentionally changes dozens of files but requires `git status --porcelain` to be clean before its per-task commit ([07-02-PLAN.md:337](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-02-PLAN.md:337)).
- **LOW — Large mechanical census.** The live vocabulary is used in production and integration code, so a missed consumer would be costly. The repo-wide zero-reference grep and `task test:int` mitigate this substantially.

### Suggestions

- Replace the clean-status criterion with `task fmt`, `task lint`, and a scoped check that formatting is stable after a second formatting pass.
- Preserve the existing repo-wide consumer census as the authoritative completion check.

### Risk assessment

**HIGH as written because of the hard gate; MEDIUM after that correction.**

---

## 07-03 — Gateway value leaves

### Summary

This is the best-isolated plan in the phase. The three extractions correspond to genuinely pure values/helpers, and the retained `core.NewULID` forwarder avoids unnecessary blast radius.

### Strengths

- The ULID generator is already internally cohesive: the entropy source, lock, clock clamp, and generator are one coupled unit ([ulid.go:16](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/ulid.go:16), [ulid.go:40](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/ulid.go:40)).
- `ParseCommand` is a pure string helper with no domain dependencies ([command.go:6](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/command.go:6)).
- `DefaultLeaseRefreshInterval` is a value constant shared by both gateways, while its current documentation explicitly describes that shared contract ([reaper.go:14](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/session/reaper.go:14)).
- The end-of-plan closure checks directly prove the ARCH-05 result rather than relying on import-line inspection alone ([07-03-PLAN.md:301](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-03-PLAN.md:301)).

### Concerns

- **LOW — Monotonicity remains a broader contract than event ordering needs.** The plan correctly removes stale ULID-as-JetStream-ordering prose while retaining the generator behavior. Future documentation must preserve that distinction.
- No blocking plan defect found.

### Suggestions

- Keep the forwarder smoke test and leaf-level generator tests separate as planned.
- Record the final telnet/web transitive closures in the summary for later regression comparison.

### Risk assessment

**LOW-MEDIUM.**

---

## 07-04 — Gateway enforcement and invariant binding

### Summary

The enforcement design is strong: direct imports and transitive closure share one policy list, and the invariant binding names both tests. The generated-doc verification command is nevertheless unsatisfiable in the task that intentionally changes that generated file.

### Strengths

- The current test only inspects direct AST imports ([gateway_imports_test.go:148](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/gateway_imports_test.go:148)); adding a `NeedDeps` closure test closes a real enforcement gap.
- The plan corrects the nonexistent `internal/auth/service` rule visible in the live list ([gateway_imports_test.go:101](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/gateway_imports_test.go:101)).
- The registry is indeed pending, contains the same phantom path, and has a stale `INV-GW-1` token ([invariants.yaml:2340](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/docs/architecture/invariants.yaml:2340)).
- It correctly preserves the historical GW tokens as fixtures; the migration is documented in [meta_test.go:16](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/gateway_invariants/meta_test.go:16).

### Concerns

- **HIGH — Task 3 edits and regenerates `docs/architecture/invariants.md`, then immediately requires `git diff --exit-code docs/architecture/invariants.md`** ([07-04-PLAN.md:328](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-04-PLAN.md:328), [07-04-PLAN.md:349](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-04-PLAN.md:349)). The intended generated change itself makes that command fail.
- **LOW — The positive control intentionally depends on `internal/grpc` retaining a store dependency.** The plan documents replacement when that coupling disappears, but Phase 8 is likely to trigger exactly that maintenance event.

### Suggestions

- Verify renderer idempotence by copying the first generated result to a temporary file, rerunning the renderer, and comparing the two outputs.
- Keep the positive control’s replace-don’t-delete comment.

### Risk assessment

**HIGH as written; LOW-MEDIUM after fixing the generated-file gate.**

---

## 07-05 — Move `core.Engine` to `presence.Emitter`

### Summary

The target architecture and cycle break are correct, and the plan now preserves the Engine’s load-bearing behavior. Task 2, however, changes the auth interface before production callers implement it.

### Strengths

- The plan correctly preserves the typed-nil construction guard in `NewEngine` ([engine.go:39](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/engine.go:39)).
- It also preserves `EndSession`’s deliberate background commit and cause-dependent actor selection ([engine_end_session.go:22](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/engine_end_session.go:22), [engine_end_session.go:56](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/engine_end_session.go:56)).
- The consumer-defined auth interface is the right cycle breaker: auth only calls disconnect and session-ended behavior ([auth_service.go:235](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/auth/auth_service.go:235)).

### Concerns

- **HIGH — Task 2 changes auth to require `EmitLeave`/`EmitSessionEnded` and then runs `task build` before Task 3 rewires production.** The changed interface is specified at [07-05-PLAN.md:351](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-05-PLAN.md:351), with the build gate at [line 411](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-05-PLAN.md:411). The live caller still passes `*core.Engine` ([sub_grpc.go:295](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/sub_grpc.go:295)), which lacks the renamed methods.
- **HIGH — The clean-status criterion at [07-05-PLAN.md:488](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-05-PLAN.md:488) is impossible before committing the intended edits.**

### Suggestions

- Combine Tasks 2 and 3 into one atomic task, or introduce a temporary adapter that lets `*core.Engine` satisfy the new interface until the caller migration completes.
- Replace the clean-status check with stable-format/lint checks.

### Risk assessment

**HIGH.**

---

## 07-06 — Unified system broadcast builder

### Summary

The one-builder/two-caller design is sound and removes a real payload-shape duplication. The task ordering and task-level file manifest are not executable as written.

### Strengths

- The duplication is real: hostcap builds `{"message": ...}` independently ([system_broadcaster.go:45](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/hostcap/system_broadcaster.go:45)), while command builds the same shape separately ([types.go:619](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/command/types.go:619)).
- Carrying a game-ID provider is necessary because `eventbus.Qualify` rejects relative subjects without one ([qualify.go:23](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/qualify.go:23)).
- The retyped actor assertions correctly account for different enum values and ULID/string representations.

### Concerns

- **HIGH — Task 2 changes `ConfigureSystemBroadcaster` from one argument to two, then runs `task test:int` before Task 3 updates its caller.** The signature change is at [07-06-PLAN.md:232](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-06-PLAN.md:232), and the gate is at [line 310](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-06-PLAN.md:310). The live caller still passes one `eventStore` argument ([sub_grpc.go:292](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/sub_grpc.go:292)).
- **HIGH — Task 2 explicitly edits `test/integration/pluginparity/session_admin_broadcast_test.go` at [07-06-PLAN.md:259](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-06-PLAN.md:259), but that file is absent from Task 2’s `<files>` list at [line 202](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-06-PLAN.md:202).

### Suggestions

- Combine Tasks 2 and 3, or move the production caller rewire and plugin-parity test update into Task 2.
- Ensure each task-level `<files>` list includes every file its action mandates.

### Risk assessment

**HIGH.**

---

## 07-07 — Collapse the Event types

### Summary

This plan addresses the central ARCH-04 goal with excellent attention to wire compatibility, actor conversion, zero-ULID formatting, and publication seams. Its final documentation task has an impossible verification command.

### Strengths

- The duplication is concrete: `core.Event` lacks sequence and uses string stream/actor fields ([core/event.go:232](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/event.go:232)), while `eventbus.Event` carries host-internal `Seq` and the package-private audit row seam ([eventbus/types.go:136](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/types.go:136), [eventbus/types.go:195](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/types.go:195)).
- The plan explicitly replaces CoreServer’s publication path rather than merely constructing an event and forgetting to publish it.
- Zero actor ULIDs are deliberately preserved as empty plugin-facing strings rather than `"000…"`.

### Concerns

- **HIGH — Task 3 edits three files and then runs `task fmt && git diff --exit-code`** ([07-07-PLAN.md:458](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-07-PLAN.md:458), [07-07-PLAN.md:528](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-07-PLAN.md:528)). The intended documentation/code-comment changes make the command fail.
- **HIGH — Task 2 also requires a clean worktree after formatting** while its intended Event migration is uncommitted ([07-07-PLAN.md:442](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-07-PLAN.md:442)).
- **MEDIUM — Security-sensitive blast radius.** The surviving actor bridge touches plugin emit validation; the mandatory crypto-reviewer gate should remain merge-blocking.

### Suggestions

- Replace whole-tree `git diff --exit-code` with `task fmt`, lint, and an idempotent second formatter pass.
- Keep the exact actor rejection and crypto integration tests mandatory.

### Risk assessment

**HIGH as written; MEDIUM-HIGH after verification repair.**

---

## 07-08 — Seq-correct plugin history pagination

### Summary

This is technically the strongest plan. It traces the defect end-to-end, creates a deterministic RED, covers both Lua and binary paths, and preserves `Seq` as host-internal.

### Strengths

- The live contract explicitly defines cursors as `(seq,id)` pairs and says zero sequence means tail/start ([bus.go:87](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/bus.go:87)).
- The hot tier advances only on `BeforeSeq`; `BeforeID` alone does not affect filtering ([hot_jetstream.go:334](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/history/hot_jetstream.go:334), [hot_jetstream.go:392](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/history/hot_jetstream.go:392)).
- `HostCursor` already contains both fields ([cursor.go:53](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/cursor/cursor.go:53)).
- The plan catches both independent Lua `Seq: 0` encode sites and preserves index `0` as the correct oldest-event anchor ([stdlib_focus.go:437](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/hostfunc/stdlib_focus.go:437), [stdlib_focus.go:452](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/hostfunc/stdlib_focus.go:452)).

### Concerns

- **LOW — Integration cost:** Task 1 runs the entire integration suite for the expected RED, which can be slow and noisy. This is not a correctness defect.
- No blocking design issue found.

### Suggestions

- Use a targeted integration invocation for the RED where the task runner permits it, followed by full `task test:int` at GREEN.
- Preserve the observed page ID sets in the summary as required; they are useful evidence that the anchor, not merely the sequence value, is correct.

### Risk assessment

**MEDIUM**, driven by history/cursor security sensitivity rather than plan weakness.

---

## 07-09 — Remove eager starts and add TLS subsystem

### Summary

The provider-based bootstrap design is correct, but this plan contains both manifest inconsistencies and a potentially breaking GameID configuration change that exceeds a behavior-preserving refactor without a migration policy.

### Strengths

- The eager-start problem is real: database starts before orchestration ([core.go:281](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/core.go:281)), and TLS then consumes its resolved GameID ([core.go:300](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/core.go:300)).
- The TLS test seam is real and correctly preserved through `CoreDeps.TLSCertEnsurer` ([deps.go:51](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/deps.go:51)).
- The plan’s KEK-wired boot gate is appropriate for the production-only panic shape.

### Concerns

- **HIGH — Task 1’s `<files>` list contains only the two new TLS files** ([07-09-PLAN.md:564](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-09-PLAN.md:564)), but the action mandates deleting `ensureTLSCerts` from `core.go` and modifying `deps.go` ([07-09-PLAN.md:613](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-09-PLAN.md:613), [07-09-PLAN.md:630](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-09-PLAN.md:630)).
- **HIGH — GameID semantics change.** Today `event_bus.game_id` is a live configuration field ([config.go:40](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/config.go:40)) loaded independently during boot ([core.go:136](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/core.go:136)); public documentation states that it controls the event subject namespace ([event-store.md:89](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/site/src/content/docs/contributing/explanation/event-store.md:89)). The plan proposes that the DB/global provider override even an explicitly configured, differing `event_bus.game_id`, with only a warning ([07-09-PLAN.md:428](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-09-PLAN.md:428)). That can shift live subject namespaces and make existing history appear absent.
- **HIGH — Multiple tasks require a clean worktree after formatting** before their intended changes are committed ([07-09-PLAN.md:1152](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-09-PLAN.md:1152)).

### Suggestions

- Make GameID mismatch **fail closed** with a coded configuration error, or define an explicit migration/compatibility policy. Do not silently override an operator-set subject namespace.
- Update the operator-facing documentation if `event_bus.game_id` is being deprecated or redefined.
- Correct every task-level `<files>` list and remove clean-status acceptance checks.

### Risk assessment

**HIGH.** This is the largest unacknowledged runtime/data-visibility risk outside 07-11.

---

## 07-10 — Stop deadlines and topology pins

### Summary

The topology work is carefully reasoned and avoids the previously discovered EventBus/verifier cycle. Stop deadlines are defensible, although the abandon-on-timeout mechanism leaves a bounded residual risk.

### Strengths

- The current `StopAll` is synchronously blocking and ignores context cancellation ([orchestrator.go:80](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/lifecycle/orchestrator.go:80)).
- The plan correctly uses a buffered one-shot result channel, avoiding a permanent sender leak when a timed-out `Stop` later returns.
- It correctly adds gRPC’s missing `AuditProjection` dependency: the live dependency list currently omits it ([sub_grpc.go:166](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/sub_grpc.go:166)).
- The production graph test reads real dependency sets and exercises them through public `StartAll`, avoiding hand-copied topology.

### Concerns

- **MEDIUM — Timed-out `Stop` operations continue concurrently after `StopAll` returns.** The plan documents orchestrator reuse as unsupported, but a late teardown can still mutate subsystem state during the interval before process exit ([07-10-PLAN.md:309](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-10-PLAN.md:309), [07-10-PLAN.md:344](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-10-PLAN.md:344)).
- **LOW — The live positive/topological controls will require deliberate updates whenever Phase 8 alters the dependency graph.** Exact ordering pins trade silent drift for intentional maintenance, which is reasonable.

### Suggestions

- Preserve the explicit “no orchestrator reuse after timed-out rollback” contract.
- Include abandoned subsystem IDs and deadline duration in the error log, and consider a metric if shutdown telemetry remains live long enough to emit it.

### Risk assessment

**MEDIUM.**

---

## 07-11 — Prepare/Activate split

### Summary

The lifecycle design is thoughtful and unusually complete, but the plan is not structurally executable under per-task commits. Task 1 changes the interface before Task 2 migrates implementations, and Task 2’s task-level file list omits most callers it explicitly requires changing.

### Strengths

- The two-sweep barrier genuinely prevents domain serving before all acquisition is complete.
- Rollback correctly includes the failing `Prepare` target; the live orchestrator currently records a subsystem only after successful `Start`, so it presently omits that target ([orchestrator.go:54](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/lifecycle/orchestrator.go:54)).
- The plan correctly retains idempotency where it protects real side effects, such as the ABAC poller ([access/setup/subsystem.go:69](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/access/setup/subsystem.go:69)).
- The plugin partial-prepare leak is real: `cleanupOnError` does not close either host ([plugin/setup/subsystem.go:234](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/setup/subsystem.go:234)), while `Stop` returns immediately until `manager` is assigned ([plugin/setup/subsystem.go:449](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/setup/subsystem.go:449)). The proposed goleak regression directly addresses it.
- The admin socket’s lock/bind/serve operation is genuinely atomic in one `Server.Start` call ([server.go:58](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/admin/socket/server.go:58)); keeping it entirely in `Activate` is appropriate.

### Concerns

- **HIGH — Task 1 leaves the repository uncompilable.** It replaces `Subsystem.Start` with `Prepare`/`Activate` ([07-11-PLAN.md:710](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-11-PLAN.md:710)) and verifies only `internal/lifecycle` ([line 825](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-11-PLAN.md:825)). The 17 production implementations still expose the live `Start` interface ([subsystem.go:42](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/lifecycle/subsystem.go:42)) until Task 2. This conflicts with the plan’s own “every pushed commit builds” requirement.
- **HIGH — Task 2’s `<files>` list names implementations but omits most direct caller files** ([07-11-PLAN.md:843](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-11-PLAN.md:843)). Those omitted callers are explicitly enumerated in frontmatter, including numerous integration suites ([07-11-PLAN.md:57](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-11-PLAN.md:57)).
- **HIGH — The clean-status acceptance criterion is impossible before committing approximately 54 intended file changes** ([07-11-PLAN.md:1282](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-11-PLAN.md:1282)).
- **MEDIUM — Scope and reviewability.** Despite the plan’s extensive settlement tables, one task changes lifecycle semantics, 17 implementations, roughly 27 callers, cleanup behavior, timing seams, and multiple error paths. A single implementation error can remain integration-only.

### Suggestions

- Make the interface change, all implementations, and all callers one atomic execution task/commit. Keep RED tests as local steps rather than a separately committed broken interface state.
- Expand Task 2’s `<files>` to include every caller already listed in frontmatter.
- Remove the clean-status criterion and retain `task build`, `task test`, `task test:int`, KEK boot E2Es, topology tests, and crypto review as the real gates.
- Consider splitting only by a temporary compatibility interface if independent green commits are mandatory; do not leave a `Start` shim in the final state.

### Risk assessment

**HIGH**, even after plan repair, because this is the phase’s largest semantic and concurrency change.

---

## Final recommendation

Revise the plans before execution:

1. Repair atomic task boundaries in 07-01, 07-05, 07-06, and 07-11.
2. Remove or replace impossible clean-worktree/`git diff --exit-code` gates in 07-01, 07-02, 07-04, 07-05, 07-07, 07-09, and 07-11.
3. Settle `event_bus.game_id` compatibility in 07-09 with fail-closed validation or an explicit migration.
4. Correct task-level file manifests in 07-06, 07-09, and 07-11.
5. Then execute with mandatory `task test:int`, KEK-wired boot E2Es, and crypto-reviewer gates.

The architectural direction is strong; the current blockers are predominantly plan executability and one significant configuration-compatibility issue.
---

## OpenCode Review (openrouter/moonshotai/kimi-k3) — LANE FAILED

Three consecutive invocations died on OpenRouter provider `429 rate_limit_exceeded` before emitting a final review (attempts at ~280K, ~64K, and ~285K tokens of agentic source verification; 10-minute cooldown before the final attempt). No verdict exists for this lane; do not count it in consensus.

Salvaged from the partial narration of the first attempt (progress notes only, no findings text):
- It independently re-verified the round-9 07-11 plugin partial-Prepare leak claims against source ("All round-9 plugin-leak claims verified") before dying.
- It flagged `internal/web/translate_test.go:467,482` (`core.NewULID()` calls) as a "likely gap" in 07-03's census. **Orchestrator disposition: non-issue** — 07-03 deliberately retains `core.NewULID` as a forwarder (D-03), so the test keeps compiling, and the plan's closure gates are `go list -deps` based (production imports only; test files are invisible to them). At most a cosmetic post-phase cleanup.

---

## Consensus Summary

**Single grounded lane this round** (codex; the kimi-k3 lane failed on provider rate limits), so consensus weight rests on the orchestrator's source verification of every codex HIGH, per the round-5+ protocol.

**Codex verdict: NOT READY.** Its HIGHs cluster into four classes; verification results below. The headline is real: **three task-boundary blockers (07-01, 07-05, 07-06) where a task's own automated gate cannot pass until a LATER task rewires the callers** — all pre-existing since early revisions, all missed by every prior round (including codex's own round 9, which called 07-01→07-05 "executable"). The new lens — "can each task's gate pass at its own task boundary?" — found a defect class nine prior rounds never probed. Consistent with the round-6 lesson: convergence is measured by coverage, not streak.

### Orchestrator verification of the HIGHs

| # | Codex finding | Verdict | Evidence |
|---|---|---|---|
| 1 | **07-01 Task 1 not independently executable** | **CONFIRMED — blocker** | Task 1 moves `client.go`/`client_test.go` to `internal/grpcclient` and DELETES the originals, with `task build` in its acceptance criteria and `<automated>` gate. But `task build` compiles the main binary (`Taskfile.yaml:290-296`), whose graph includes `cmd/holomush/gateway.go` — still importing `internal/grpc` and calling `holoGRPC.NewClient` (`gateway.go:20,152`) until Task 2 rewires it. Task 1's gate cannot pass. |
| 2 | **07-05 Task 2 gate fails before Task 3** | **CONFIRMED — blocker** | Task 2 retypes auth's fanout params to `PresenceEmitter` (`EmitLeave`/`EmitSessionEnded`) with a whole-repo-reaching `task build` gate. The live caller `cmd/holomush/sub_grpc.go:295-299` passes `engine := core.NewEngine(eventStore)` — `*core.Engine`'s methods are `HandleDisconnect`/`EndSession`, so it does not satisfy the interface; Task 1 only CREATES `internal/presence` (new files). Compile fails until Task 3 ("Repoint grpc, cmd/holomush and the harness"). |
| 3 | **07-06 Task 2 gate fails before Task 3; files manifest incomplete** | **CONFIRMED — blocker** | Task 2 changes `ConfigureSystemBroadcaster` to `(pub eventbus.Publisher, gameID func() string)` and gates on `task test:int` — which compiles `./...` (`Taskfile.yaml:189-190`, "No package enumeration"). The caller `sub_grpc.go:292` still passes one argument; the two-arg call lands only in Task 3's criteria (07-06:417). Additionally Task 2's `<files>` (:203) omits `test/integration/pluginparity/session_admin_broadcast_test.go`, which its own action (:259 area) edits — the file is in plan frontmatter but not the task manifest. |
| 4 | **07-04 Task 3 `git diff --exit-code` on the file it regenerates** | **DOWNGRADED → MEDIUM (Class-5 phrasing)** | The criterion `go run ./cmd/inv-render && git diff --exit-code docs/architecture/invariants.md` is the standard regenerate-and-confirm-current idempotence check — satisfiable AFTER the per-task commit, unsatisfiable before it. Not impossible; ambiguous ordering. Folds into the phrasing class below. |
| 5 | **"Clean worktree" acceptance criteria impossible** (07-01:260, 07-02:337, 07-05:488, 07-07:442/528, 07-09:1152, 07-11:1282) | **DOWNGRADED → MEDIUM (one phrasing fix, 7 plans)** | Every instance reads "\`git status --porcelain\` is clean **after \`task fmt\`**" (or the diff-gate variant) — a fmt-stability/regen-currency check whose only satisfiable reading is post-commit (CLAUDE.md itself warns "task fmt mutates files — commit those edits"). Codex's pre-commit reading fails on the task's own uncommitted edits. Fix: prefix each with "after the per-task commit," so a literal executor cannot stall on a red gate. Mechanical, repeated. |
| 6 | **07-09 Task 1 `<files>` omits `core.go`/`deps.go`** | **CONFIRMED — blocker (manifest-vs-action)** | Task 1's `<files>` (:565) lists only `internal/tls/subsystem.go` + test, while its action moves `ensureTLSCerts` OUT of `cmd/holomush/core.go` (:613) and edits `deps.go:71` (:630-632) — the action even contains a warning box about an earlier revision omitting these files, yet the task manifest still omits them. |
| 7 | **07-09 gameID: provider silently overrides an explicit `event_bus.game_id` (wants fail-closed)** | **REFUTED as blocker; one CONFIRMED doc fold-in** | The warn-on-conflict disposition is the deliberately ratified round-8/rev-11 settlement (explicit value loses to the provider WITH a WARN; the GLOBAL `game_id` key keeps its semantics; escape hatch documented). Re-litigation, not a regression. HOWEVER codex surfaced one new fact the settlement's "no operator-doc contradiction" claim missed: `site/src/content/docs/contributing/explanation/event-store.md:89` publicly documents "`<game_id>` is set via `event_bus.game_id` in server config" — post-plan that key is dead on the production boot path and NO plan updates the doc. CONFIRMED as a small doc fold-in for 07-09. |
| 8 | **07-11 Task 1 leaves the repo uncompilable** | **REFUTED** | The plan already handles this exact concern: :1230-1234 retracts the earlier known-broken-commit instruction and mandates working in units but **squashing into ONE commit that builds**; :1276 pins "Every commit pushed builds." Task 1's `<automated>` gate is deliberately scoped to `./internal/lifecycle/` (compiles standalone mid-unit). The mechanism codex asks for is present in the plan text it cites past. |
| 9 | **07-11 Task 2 `<files>` omits most callers** | **REFUTED** | Task 3 ("Migrate the test implementations…") owns the test-side callers; Task 2's manifest carries the 17 production impls + clustertest harnesses + `sub_grpc.go`. The census criteria (:1255-1256) explicitly require any caller not covered by `files_modified` to be reported, not silently migrated. |

### Also verified

- **07-10 MEDIUMs**: codex endorses the rev-12 no-reuse doc contract and buffered-channel design; its residual (late teardown before process exit) is the accepted bounded risk. No action.
- **07-08**: no blocking issue from codex (its LOW about RED-run cost is a workflow preference; the plan already scopes what it can).
- **07-02/07-03**: only the Class-5 phrasing instance plus already-covered census gates. 07-03 judged "best-isolated plan in the phase."

### Agreed strengths (codex + salvaged kimi narration + prior-round continuity)

- Architectural grounding remains strong: import cycles, cursor semantics, plugin-runtime symmetry, rollback states, and lifecycle ordering all verified against live code (codex: "unusually well researched").
- The rev-12 fixes all HELD: the retyped 07-06 parity assertions (codex: "correctly account for different enum values and ULID/string representations"), the 07-11 plugin-leak cleanup + goleak test (codex confirms the leak against the same lines and endorses the fix; kimi's partial run independently re-verified it), the 07-10 no-reuse contract, and the 07-08 deterministic-ID idiom.
- 07-08 again rated the technically strongest plan; 07-03 the best isolated.

### Divergent views

- No second grounded verdict this round (kimi-k3 lane failed). Divergence is codex-vs-orchestrator: of codex's 12 HIGH citations, 4 survive as blockers (three task-boundary gates + one manifest), 2 fold into one MEDIUM phrasing class, and the rest are refuted by plan text codex read past (07-11 squash guidance, Task 3 ownership) or by settled dispositions (gameID warn-on-conflict).

### Recommendation

**Rev 13 — task-gate mechanics only; zero design changes.**

1. **07-01 (blocker):** make the move+delete and the caller rewire one atomic execution unit — either merge Tasks 1+2, or adopt 07-11's exact pattern (:1230-1276): tasks as work units, squash into one green commit, with Task 1's automated gate scoped to `task test -- ./internal/grpcclient/` and the whole-repo gates moving to the unit boundary.
2. **07-05 (blocker):** same treatment for Tasks 2+3 (interface retype + caller repoint).
3. **07-06 (blocker):** move the `sub_grpc.go:292` caller rewire into Task 2 (the file is already in plan frontmatter) OR defer the `task test:int` gate to Task 3 with squash guidance; add `test/integration/pluginparity/session_admin_broadcast_test.go` to Task 2's `<files>`.
4. **07-09 (blocker):** add `cmd/holomush/core.go` and `cmd/holomush/deps.go` to Task 1's `<files>`.
5. **Phrasing class (MEDIUM, 7 plans):** prefix every "\`git status --porcelain\` is clean after \`task fmt\`" and regen-`git diff --exit-code` criterion with "after the per-task commit," (07-01, 07-02, 07-04, 07-05, 07-07, 07-09, 07-11).
6. **07-09 doc fold-in (LOW/MEDIUM):** update `site/src/content/docs/contributing/explanation/event-store.md:89` in the same change that kills `event_bus.game_id` on the boot path — state the global `game_id`/provider resolution and the escape hatch.

Nothing in this round touches design: every confirmed finding is task-boundary/gate/manifest mechanics. All rev-12 fixes held. After rev 13, execute — the two lenses that found new defect classes (rounds 9 and 10) are now both swept across all 11 plans.
