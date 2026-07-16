---
phase: 7
round: 6
reviewers: [codex, opencode]
reviewed_at: 2026-07-16T15:02:02Z
plans_reviewed: [07-01-PLAN.md, 07-02-PLAN.md, 07-03-PLAN.md, 07-04-PLAN.md, 07-05-PLAN.md, 07-06-PLAN.md, 07-07-PLAN.md, 07-08-PLAN.md, 07-09-PLAN.md, 07-10-PLAN.md, 07-11-PLAN.md]
plans_revision: "rev 8 (0fdd1867f)"
notes: "Round 5 preserved at 6ec967b63. Reviewers were given NO prior-round feedback (standing user decision). opencode model: openrouter/x-ai/grok-4.5; codex: default model, codex-cli 0.144.4."
---

# Cross-AI Plan Review — Phase 7 (Round 6, rev 8)

## Codex Review

# Phase 7 Plan Review

## Overall assessment

The phase is carefully researched and mostly traces real dependencies, error semantics, integration-only callers, and prior review corrections. The wave sequence can achieve ARCH-03/04/05. I would not approve execution unchanged, however: 07-06 contains an impossible repository-wide acceptance check; 07-07 prescribes calling an unexported actor bridge across package boundaries; 07-10 requires a `cmd/holomush` test to call private `lifecycle.topoSort`; and 07-11 contradicts 07-09 over cluster provider resolution. These are plan defects rather than speculative implementation risks.

---

## 07-01 — Extract `internal/grpcclient`

### Summary

A strong, low-risk package relocation. It targets the import that causes most of telnet’s dependency expansion while preserving the wire-error classification and accounting for dual client/server consumers.

### Strengths

- `internal/grpc/client.go` is a good leaf candidate: its client surface depends on generated protos and external gRPC packages, not HoloMUSH domain packages (`internal/grpc/client.go:20-23,26-51`).
- The plan correctly catches `cmd/holomush/gateway.go`, which directly constructs `ClientConfig` and calls `NewClient` (`cmd/holomush/gateway.go:20,151-154`).
- The symbol-first census fixes a real alias trap: `test/integration/phase1_5_test.go` aliases the package as `grpcpkg` and invokes the client at `:297-300` while retaining server uses.
- Moving `TranslateSubscribeErr` verbatim preserves the enumeration-safe `SESSION_NOT_FOUND` mapping (`internal/grpc/client.go:114-139`).

### Concerns

- **LOW:** Task 2’s automated verification omits `task test:int`, even though the plan correctly recognizes that `phase1_5_test.go` is integration-tagged and otherwise invisible (`07-01-PLAN.md:240,259,263`). The plan-level verification eventually runs it, but task completion can be reported prematurely.

### Suggestions

- Add `task test:int` directly to Task 2’s `<automated>` command.
- Retain the symbol-first census output in the summary as planned.

### Risk Assessment

**LOW.** Mechanical relocation with good caller coverage and strong behavioral preservation.

---

## 07-02 — Extract `internal/eventvocab`

### Summary

The dependency-free vocabulary package is the correct resolution to the ARCH-04/ARCH-05 collision. The symbol inventory appears complete and preserves wire strings and JSON shapes.

### Strengths

- The nine host event constants are clearly isolated in `internal/core/event.go:35-60`.
- Payload validation is self-contained and suitable for a leaf (`internal/core/event.go:19-32`).
- Moving vocabulary separately from `eventbus.Event` avoids forcing telnet to import the event bus while still allowing the duplicate Event struct to disappear later.

### Concerns

- **LOW:** The plan says to move the vocabulary “verbatim,” but `ValidatePayload` currently tells callers to invoke it before `EventStore.Append` (`internal/core/event.go:21-24`). That API/model is retired by the phase. The stale wording will survive under a new package and is not caught by a search for `core.EventAppender` or `core.NewEvent`.

### Suggestions

- Preserve behavior and identifiers verbatim, but update the comment to say validation occurs before publishing or accepting an event.
- Add a documentation acceptance check excluding `EventStore.Append` from `internal/eventvocab`.

### Risk Assessment

**LOW.** Correct architecture with a small documentation-drift issue.

---

## 07-03 — Gateway value leaves

### Summary

This plan cleanly removes the remaining legitimate gateway dependencies on `core` and `session` while retaining a compatibility forwarder where the eventbus→core edge makes a mass rename undesirable.

### Strengths

- The ULID generator’s coupled entropy, lock, and timestamp state genuinely needs one home (`internal/core/ulid.go:16-43`); moving it whole and keeping `core.NewULID` as a forwarder avoids duplicate entropy sources.
- `ParseCommand` is a pure 13-line helper suitable for a leaf (`internal/core/command.go:8-20`).
- `DefaultLeaseRefreshInterval` is only a value-level protocol timing contract (`internal/session/reaper.go:14-25`), so extracting it prevents future gateway coupling without changing the value.
- Keeping `core.NewULID` as a forwarder respects the existing eventbus dependency while letting gateways use the leaf directly.

### Concerns

- **LOW:** Moving all generator tests into `internal/ulidgen` removes direct behavioral coverage of the retained `core.NewULID` compatibility seam. Compilation catches most errors, but not an accidental future change to a different generator.

### Suggestions

- Keep a small `internal/core` smoke test asserting successive `core.NewULID()` results are valid and increasing; leave the exhaustive generator tests in `ulidgen`.

### Risk Assessment

**LOW.** Small, well-contained moves with deliberate compatibility seams.

---

## 07-04 — Gateway closure gate and invariant binding

### Summary

This is the right enforcement design: the existing AST test only sees direct imports, while ARCH-05 is explicitly a transitive property. A shared forbidden set, positive control, and genuine invariant binding are all strong choices.

### Strengths

- The current gate walks `file.Imports` only (`cmd/holomush/gateway_imports_test.go:142-158`), confirming that a closure test is necessary.
- The existing test already handles test variants and checks package-load diagnostics (`cmd/holomush/gateway_imports_test.go:111-140`).
- A positive control using `internal/grpc → internal/store` is valuable protection against a vacuous traversal.
- Replacing the phantom `internal/auth/service` entry visible at `cmd/holomush/gateway_imports_test.go:103-109` with the real package is necessary.
- Binding both the direct and transitive tests avoids a partial invariant assertion.

### Concerns

- **MEDIUM:** The new closure-test instructions specify `packages.Load` with `NeedDeps`, but do not explicitly require checking `packages.PrintErrors`. `packages.Load` can return a nil Go error while individual packages contain load/type errors. The existing test correctly uses both `require.NoError` and `require.Empty(packages.PrintErrors(pkgs))` (`cmd/holomush/gateway_imports_test.go:132-133`); the new oracle should do the same.

### Suggestions

- Require both load checks in Task 1 and add an acceptance criterion for `packages.PrintErrors`.
- Keep the positive control, but do not treat it as a substitute for checking each telnet/web load result.

### Risk Assessment

**MEDIUM.** The enforcement model is sound, but an unchecked partial package load could create a security-relevant false green.

---

## 07-05 — Move Engine to `internal/presence`

### Summary

This plan correctly preserves the two genuinely load-bearing Engine behaviors and uses a consumer-defined auth interface to avoid the proven import cycle. It is substantially better grounded than a simple rename.

### Strengths

- The typed-nil construction guard is real and should move intact (`internal/core/engine.go:37-61`).
- The audit-critical context behavior and cause-dependent actor selection are explicit in `EndSession` (`internal/core/engine_end_session.go:33-75`).
- The auth injection seam already exists (`internal/auth/auth_service.go:35-45,110-119`), making a local interface a low-churn cycle break.
- Auth only uses disconnect and session-ended behavior (`internal/auth/auth_service.go:235-245`), supporting the proposed narrow interface.
- Subject qualification currently occurs in `busEventAppender` (`cmd/holomush/sub_grpc.go:828-853`), so moving it into the emitter is the correct replacement rather than silently publishing relative subjects.

### Concerns

- **LOW:** If the plan preserves operation metadata such as `append_arrive_event` and `append_leave_event` (`internal/core/engine.go:80-104`) after replacing Append with Publish, observability terminology remains misleading. Error codes and caller-visible behavior should stay stable, but internal operation labels need not describe a deleted mechanism.

### Suggestions

- Preserve externally asserted error codes while renaming internal operation/log labels from “append” to “publish” where tests do not treat those labels as contract.
- Add a direct test that `EmitSessionEnded` still ignores a pre-cancelled caller context.

### Risk Assessment

**MEDIUM.** The move touches session audit semantics, but the plan identifies and tests the dangerous behaviors.

---

## 07-06 — Single system-broadcast builder

### Summary

The architecture is correct: one intent-level builder eliminates the duplicated payload contract and lets `internal/command` shed its event dependency. One acceptance criterion, however, is impossible in the current repository and must be fixed before execution.

### Strengths

- The duplication is real: command builds `{"message":...}` at `internal/command/types.go:619-641`, while hostcap mirrors it independently at `internal/plugin/hostcap/system_broadcaster.go:45-60`.
- Keeping `hostcap.systemBroadcaster` as the subject-pinning adapter preserves the existing `SessionAdmin` shape, including unsupported disconnect behavior.
- Replacing `ServicesConfig.Events` and `Services.Events()` removes the actual command→event dependency (`internal/command/types.go:518-561`).
- Exact subject and payload assertions avoid tautological qualification tests.

### Concerns

- **HIGH:** The repo-wide criterion in `07-06-PLAN.md:384,441` requires  
  `rg -c 'json.Marshal\(map\[string\]string\{' --type go` to return 1. It already matches unrelated legitimate code, including `plugins/core-scenes/service.go:420,1350,1569` and `cmd/holomush/phase7_fence_wiring.go:90`. A correct implementation cannot pass without unrelated scope creep.

### Suggestions

- Replace the check with a broadcast-specific assertion, such as searching for the literal `"message"` construction only in the two retired sites plus `internal/sysbroadcast`.
- Better still, use the behavioral tests and assert that the former builder files contain no `json.Marshal` or event construction.
- Do not modify unrelated map-marshalling code to satisfy the count.

### Risk Assessment

**HIGH until corrected; MEDIUM afterward.** The design is good, but the current acceptance gate is deterministically red.

---

## 07-07 — Delete `core.Event` and collapse adapters

### Summary

This plan addresses the central ARCH-04 goal and correctly keeps `eventbus.Event` in place because of its package-private audit seam. Its CoreServer actor-conversion instruction is not currently implementable as written.

### Strengths

- `eventbus.Event` contains the host-internal `Seq` and unexported `auditRow` required by the downgrade fence (`internal/eventbus/types.go:141-203`).
- `AuditRowOf` and `StampAuditRow` confirm why the type must remain in `internal/eventbus` (`internal/eventbus/audit_row_access.go:10-49`).
- The canonical constructor already stamps identity and timestamp (`internal/eventbus/types.go:206-223`).
- The plan correctly names the CoreServer publisher and game-ID replacement; today `emitCommandResponse` only builds and appends a `core.Event` (`internal/grpc/server.go:616-652`).
- The three actor bridges and their different failure behavior are real (`cmd/holomush/sub_grpc.go:856-873`, `internal/plugin/event_emitter.go:299-319`, `internal/testsupport/integrationtest/harness.go:1362-1364`).

### Concerns

- **HIGH:** Task 2 says CoreServer’s system actor should be mapped “through the surviving bridge” (`07-07-PLAN.md:332-335`), but that bridge is the unexported `coreActorToEventbusActor` in package `plugins` (`internal/plugin/event_emitter.go:306`). Code in package `internal/grpc` cannot call it. Exporting a plugin-owned conversion utility would be a new API and questionable ownership; duplicating it violates the one-bridge requirement.
- **LOW:** The rules rewrite should say publication sites **MUST use `eventbus.NewEvent`**, not merely require an ID stamped with `core.NewULID`. The constructor’s current documentation already makes that stronger guarantee (`internal/eventbus/types.go:206-214`).

### Suggestions

- Keep the strict bridge private to the plugin emit path, where arbitrary `core.Actor` input exists.
- For CoreServer’s known system actor, construct `eventbus.Actor{Kind: ActorKindSystem, ID: core.SystemActorULID}` directly. No conversion bridge is necessary.
- Rewrite the plan’s “one bridge” criterion to mean one remaining general `core.Actor → eventbus.Actor` converter, allowing direct typed actor construction elsewhere.
- Mandate `eventbus.NewEvent` explicitly in the updated rules.

### Risk Assessment

**HIGH.** Broad type deletion is inherently risky, and the prescribed cross-package bridge mechanism currently cannot compile.

---

## 07-08 — Seq-correct plugin history pagination

### Summary

The plan accurately traces the cursor bug and its revised quiet-stream RED case is especially strong. The concurrent-publisher defense test should be made deterministic.

### Strengths

- The bus contract explicitly says pagination is by `(seq,id)`, with zero seq meaning tail/start (`internal/eventbus/bus.go:87-104`).
- The current hostcap path decodes only ID, calls `ReplayTail` without seq, and encodes `Seq: 0` (`internal/plugin/hostcap/servers.go:871-905,1279-1295`).
- The current adapter passes only `BeforeID` and then destroys Seq in the `core.Event` conversion (`cmd/holomush/sub_grpc.go:913-966,969-991`).
- The repository itself states that ULID order and JetStream sequence differ under concurrency (`internal/eventbus/history/hot_jetstream.go:424-432`).
- Keeping Seq opaque avoids changing the plugin proto contract.

### Concerns

- **MEDIUM:** Spec B uses two concurrent publishers with random ULIDs and assumes this will make ULID order disagree with stream order (`07-08-PLAN.md:307`). That outcome is highly likely but not guaranteed; a run can accidentally preserve the same order and fail to prove the property.
- **LOW:** The plan’s claims that legacy zero-seq cursors are only in memory are stronger than the API guarantees. Opaque cursors can be persisted by a plugin even if no in-tree plugin does so.

### Suggestions

- Build Spec B from deliberately inverted precomputed IDs: publish a lexically higher ULID first and a lower ULID second while recording the resulting stream sequences.
- Keep the quiet-stream Spec A as the required RED gate.
- Describe zero-seq handling as backward compatibility for any legacy token, without assuming tokens were never persisted.

### Risk Assessment

**MEDIUM.** The fix is narrow but affects both Lua and binary history paths and requires careful pagination testing.

---

## 07-09 — Wave A bootstrap migration

### Summary

This plan correctly identifies constructor-time live-resource resolution as the cause of the eager starts and provides a detailed provider-based migration. It is also the largest and most failure-prone plan in the phase.

### Strengths

- The eager resource accesses are real: DB access occurs immediately after pre-start (`cmd/holomush/core.go:287-302`), eventbus live values are consumed at `:475,547`, and auth/ABAC accessors at `:828,866-878`.
- The crypto/admin block is a contiguous set of provider-sensitive wiring (`cmd/holomush/core.go:713-1019`), supporting a single memoized resolver rather than scattered ad hoc closures.
- Moving cluster’s nil connection and game-ID resolution out of construction is correct because `NewSubsystem` currently rejects both before orchestration (`internal/cluster/registry.go:78-90`).
- The plan accurately preserves cluster’s lock discipline and full locked startup body (`internal/cluster/heartbeat.go:22-96`).
- Building the existing phantom TLS subsystem is justified by the already-declared lifecycle ID.

### Concerns

- **HIGH:** The change combines provider migrations, a roughly 350-line crypto/admin hoist, cluster constructor redesign, TLS subsystem creation, coordinator ownership, dependency-edge changes, and the 16→17 registration rewrite. The current straight-line block has many fatal and degraded branches (`cmd/holomush/core.go:814-1039`); moving all of them under `sync.Once` materially changes when errors and side effects occur. A single implementation mistake can make every cryptoWiring consumer fail at boot.
- **MEDIUM:** The safety of the shared provider depends entirely on every consumer declaring the complete dependency set. The compiler cannot enforce that relation; only the proposed superset test does.

### Suggestions

- Keep one plan if atomicity requires it, but implement as two independently green commits:
  1. TLS, game-ID/cluster/provider migrations, named subsystem set.
  2. cryptoWiring hoist, consumer projections, coordinator ownership.
- Run the KEK-wired boot and full production graph test after each commit.
- Ensure the memoized resolver caches both result and error explicitly and documents that no retry occurs within one process.

### Risk Assessment

**HIGH.** The design is defensible, but this is effectively a bootstrap rewrite with many security-sensitive degraded/fatal branches.

---

## 07-10 — Shutdown deadline and graph ordering

### Summary

The shutdown and ordering problems are real, and the plan correctly rejects the EventBus→Verifier cycle. Two implementation details need redesign: private graph access and the goroutine result channel.

### Strengths

- Current rollback passes the startup context directly and only stops successfully started subsystems (`internal/lifecycle/orchestrator.go:50-67`), so a cancelled boot can indeed defeat cleanup.
- Current `StopAll` trusts every `Stop` synchronously (`internal/lifecycle/orchestrator.go:80-95`) despite the interface’s “MUST NOT block indefinitely” contract (`internal/lifecycle/subsystem.go:55-57`).
- The graph implementation has deterministic ID tie-breaking and detects missing edges/cycles (`internal/lifecycle/orchestrator.go:98-148`).
- Adding the missing AuditProjection dependency to gRPC is grounded in the serving-before-audit risk.
- The explicit acyclicity guard is the right response to the previously planned EventBus↔Verifier cycle.

### Concerns

- **HIGH:** Tasks 3 and 4 require tests in `cmd/holomush` to invoke and inspect `topoSort` (`07-10-PLAN.md:564,590-600`). `topoSort` is private to package `internal/lifecycle` (`internal/lifecycle/orchestrator.go:99`), so a `package main` test cannot call it or inspect its returned order. The plan names no exported validation seam.
- **MEDIUM:** The plan says to run each `Stop` in a goroutine and select against `ctx.Done()` (`07-10-PLAN.md:305-321`) but does not require a buffered one-shot result channel. With an unbuffered channel, a Stop that returns after the deadline blocks forever trying to report its result, converting a temporary overrun into a permanent goroutine leak.
- **LOW:** The exact 17-element order pin will be intentionally brittle. That is acceptable here, but every legitimate dependency change will require consciously updating it.

### Suggestions

- Extract graph sorting into an exported internal helper such as `lifecycle.TopologicalOrder([]Subsystem)` or add `Orchestrator.ValidateGraph/Order`. Have `StartAll` and the production graph tests call the same implementation.
- Alternatively, test acyclicity indirectly through `StartAll` with recording no-op stubs, but then remove claims about directly inspecting `topoSort`’s returned length.
- Require `result := make(chan error, 1)` for each asynchronous Stop.
- Log both the timed-out subsystem and all skipped subsystem IDs.

### Risk Assessment

**HIGH.** The plan currently asks for an inaccessible mechanism, and shutdown concurrency needs one more precise constraint.

---

## 07-11 — Prepare/Activate lifecycle split

### Summary

The two-sweep model and rollback semantics are well reasoned, but the plan has a cross-wave contradiction in cluster lifecycle placement and an incomplete idempotency design for multi-policy emission.

### Strengths

- The global Prepare sweep followed by the global Activate sweep directly enforces the intended barrier; today `StartAll` is one pass (`internal/lifecycle/orchestrator.go:53-75`).
- Including the failing Prepare target in rollback fixes an existing gap: current code appends to `startOrder` only after success (`internal/lifecycle/orchestrator.go:58-67`).
- The eventbus/audit exception is grounded: eventbus must be live enough for audit to obtain JetStream, while the embedded client listener is disabled.
- The plan correctly identifies the audit seam already present in code: partition setup precedes `p.start` (`internal/eventbus/audit/subsystem.go:283-320`).
- The admin socket split respects the atomic `Server.Start` API and protects disabled mode (`internal/admin/socket/subsystem.go:73-101,131-136`).
- The caller census and mandatory `task test:int` gate address the integration-tag landmine well.

### Concerns

- **HIGH:** 07-09 moves cluster `ConnProvider` and `ClusterIDProvider` resolution into `registry.Start`, because construction currently rejects missing values (`internal/cluster/registry.go:78-90`). But 07-11 mandates `cluster.Prepare` be a no-op and moves the **entire** Start body to Activate (`07-11-PLAN.md:414`). After 07-09, that body includes provider resolution. This means cluster can fail to acquire its connection/game ID during Activate, after the global acquisition barrier—the opposite of D-13.0.
- **MEDIUM:** The proposed crypto-policy idempotency guard is one boolean (`07-11-PLAN.md:327`), while the implementation loops over an arbitrary `PolicyNames` slice and can fail partway (`internal/admin/policy/subsystem.go:23-26,44-54`). Setting the boolean after the loop duplicates earlier emissions on retry; setting it before the loop can suppress policies that were never emitted.
- **MEDIUM:** The 54-file atomic migration is justified by the interface break, but partial-Prepare cleanup must be verified against real Stops, not only documented. Plugin setup, for example, allocates several resources before `manager` is assigned (`internal/plugin/setup/subsystem.go:225-363`), although it already has local cleanup on many error paths.

### Suggestions

- Change cluster’s settlement to:
  - `Prepare`: resolve and validate ConnProvider and ClusterIDProvider, storing stable values.
  - `Activate`: retain the entire subscription/publish/goroutine critical section under one lock.
- For crypto policy, either:
  - validate that exactly one policy is allowed and document that invariant, or
  - track completion per policy name so retry cannot duplicate a successful prefix or skip a failed suffix.
- Add partial-Prepare failure tests for subsystems that allocate resources before their final guard field is set, especially plugin and audit.
- Keep the disabled-admin and reaper phase-placement tests; they target genuine silent failure modes.

### Risk Assessment

**HIGH.** This is a repository-wide lifecycle contract change, and the cluster contradiction currently violates the plan’s central barrier guarantee.

---

## Final recommendation

Revise before execution:

1. Fix 07-06’s impossible repo-wide grep.
2. Resolve 07-07’s unexported actor-bridge instruction.
3. Add an accessible shared graph-validation seam for 07-10.
4. Make 07-10’s Stop result channel buffered.
5. Move cluster provider resolution into `Prepare` in 07-11.
6. Replace the crypto-policy single boolean with a sound partial-failure strategy.

After those changes, the phase plan is comprehensive enough to meet ARCH-03, ARCH-04, and ARCH-05. I performed this as a read-only source review; no tests were executed.

---

## OpenCode Review (grok-4.5)

# Phase 7 Plan Review — Event-Model & Bootstrap Decomposition

Verified against live tree (current worktree). Citations below are from current source unless noted.

---

## Summary

These 11 plans are **execution-grade** after multiple cross-AI rounds: locked decisions are preserved, earlier false rationales are explicitly retracted, and several real bugs (D-07 quiet-stream pageset repeat; `defer StopAll` premature timeout; EventBus↔Verifier cycle) are correctly re-framed from live code. Wave DAG is sound for shared-file safety (e.g. 03∥05 and 04∥06 empty intersections). The phase is still **HIGH operational risk**, almost entirely because waves 7–9 (`07-09`–`07-11`) are large, highlyesophageal, and will fail only under `task test:int` + KEK-wired boots if execution discipline slips—not because the design is incoherent.

---

## Strengths

- **Grounded diagnostics, not theory**
  - Telnet closure today is 47 packages and **does** pull `internal/grpc` + domain (`go list -deps ./internal/telnet`).
  - `internal/grpc/client.go` has **zero** `holomush/internal` imports → `internal/grpcclient` leaf is achievable.
  - `cmd/holomush/gateway.go:20` imports `internal/grpc` and is **not** in `coreOnlyFiles` — 07-01’s drift correction is right.
  - Phantom forbid entry `internal/auth/service` still present (`cmd/holomush/gateway_imports_test.go:107`; `INV-EVENTBUS-1` summary `docs/architecture/invariants.yaml:2344-2348`).
  - D-07 RED mechanism confirmed: `ReplayTail` sets only `BeforeID` (`cmd/holomush/sub_grpc.go:928-936`); `matchesQuery` has **no** BeforeID branch (`internal/eventbus/history/hot_jetstream.go:392-402`); bus contract: zero Seq = from-end (`internal/eventbus/bus.go:94-104`). `Seq: 0` at **three** encode sites (`hostcap/servers.go:1290`, `stdlib_focus.go:441`, `:463`).
  - Auth uses `*core.Engine` and `eventbus` deps include `internal/auth` → FINDING-1 is real; consumer interface is correct.
  - Eager starts at `core.go:287,462,797,800` + non-orchestrator `c.Start` at `:977`; unbounded `defer orch.StopAll(context.Background())` at `:1105`; false comment at `:1476`; `grpcSubsystem.DependsOn` missing AuditProjection (`sub_grpc.go:170-178`); `productionSubsystems` is **16** params (ends `outboxRelaySub`); `SubsystemTLS` exists at `lifecycle/subsystem.go:18`.
  - `DontListen: true` + `go s.server.Start()` spectatorship (`eventbus/subsystem.go:153-164`) validates D-13.0 barrier scoping.

- **Decision settlements that matter are pinned, not hand-waved**
  - Auth cycle (07-05), gameID/`Qualify` (07-05/06/07), dual `HistoryReader` interfaces (07-07/08), StopAll **closure** vs deferred args (07-10), MEDIUM-11 as **delete comment + pin** not reverse edge (07-10), two-sweep Prepare/Activate with fallback ctx on rollback (07-11), reaper/row-16 + disabled admin socket (07-11).

- **Behavior-preservation is operationalized**
  - Wire bytes / `AppSchemaVersion = 1`, command must not import eventbus, crypto non-ULID rejection survives on one bridge, KEK-wired boot via `task test:int` (not bare `task test`), step-0 census before mechanical rewires.

- **Threat models and invariants track real STRIDE surfaces**
  - INV-EVENTBUS-1 dual bound tests (direct + closure), D-08 census without fabricated `// Verifies:`, crypto-reviewer callouts on emit/history.

- **Wave engineering**
  - ARCH-05 converges before ARCH-04 deletions (`07-07 depends_on: ["07-04","07-06"]`).
  - Serial shared-file chain 01→02→03 on `gateway_handler.go` is intentional and correct.

---

## Concerns

### HIGH

1. **07-09 + 07-11 size / cognitive load (accepted W-4, still the phase killer)**  
   Real blast: ~import graph of bootstrap + ~27√ lifecycle callers later. green `task build`/`task test` systematically **lie** (integration-tagged E2Es; harness hand-mirrors). One missed Step-0 census row or Prepare-only caller collapse = silent boot/reap/audit failure. Plans document this well—**execution still HV risk**.

2. **07-09 `cryptoWiring` is correct and fragile**  
   THE RULE cannot detect cycles (plans admit this); Task 4 acyclicity is the guard. Any implementer “helpfully” adding `EventBus → CryptoChainVerifier` reopens rev-4 unbootable graph. Residual depends on humans reading settlements, not just greps.

3. **07-08 is a real behavior change under production load**  
   Design is now right (quiet-stream multipage RED; oldest-index anchor; zero-seq = tail; no ID fallback fiction), but miss on Lua `next_cursor` encode site 2 alone restores full repeat for Lua while binary looks fixed (runtime asymmetry / audit skip). Spec A must stay the gate.

### MEDIUM

4. **07-05: build `eventbus.Event` via `NewEvent`, while current `busEventAppender` uses raw `Event{}` and copies engine-stamped ID** (`cmd/holomush/sub_grpc.go:845-854`).  
   Plan requires `eventbus.NewEvent` + `Qualify` + `NewType`. Behavior should match for new publishes, but any accidental path that double-stamps, skips Qualify, or loses cause-dependent actor on `EmitSessionEnded` is a silent audit sporoy. Criteria are strong—stay intolerant of “close enough”.

5. **07-01 Step-0 still pathway-misses nonstandard client call sites**  
   plan includes phase1_5 after prior miss; `rg` of symbol-after-dot is good. Residual: generated/pkg code using `google.golang.org/grpc.NewClient` is fine (not a move target); double-check `gorules/` / analyzer tests don’t confuse the review of “complete rewire.”

6. **07-07 deletes a large type surface while many tests still invent `core.NewEngine`**  
   Live call sites are numerous (`rg core.NewEngine` — auth tests, grpc tests, harness, Phase1_5). Plan lists them; under-count risk is residual workflow, not design. Crypto-reviewer on `event_emitter.go` is non-optional.

7. **07-03 retains `core.NewULID` forwarder** (correct for D-03) while amending CLAUDE.md — residue for agents sprawl until fully earlier-docamsed is low if Task 3 docs are exact.

8. **Parallel branch 03∥05 is file-disjoint but both touch `task build`/`test:int` semantics**  
   No write conflict, still contend for green CI inertia; prefer wave-level serialization only if agents thrash.

### LOW

9. **07-02 census “34”** matches order-of-magnitude of live `core.EventType*` consumers (~34 files); plan correctly mandates re-census and forbids premature “no core in gateway” greps—good, keep that discipline.

10. **Admin socket lock moves to Activate** (07-11 row 14) — consciously accepted narrower early-detection; document in PR so reviewers don’t re-open (b).

11. **Observability HTTPPort during Prepare** (exception 1) can look like a BARRIER violation if must_haves lose the word “domain”—plans already fix that; do not unload “domain” under review pressure.

---

## Suggestions

1. **Phase delivery gate (recommended process, not new design):** after 07-09, before 07-10, require: `task test:int -- -run 'TestAdminAuthenticate|AdminRekey' ./cmd/holomush/` + THE RULE superset test + “zero eager starts” greps green as a hard gate (07-09 Task 3 already gates on Task 2 boot—extend cue to PR checklist).

2. **07-08:** SUMMARY must paste observed Spec-A page ID sets (page1 ∩ page2 = ∅) and name “oldest / index 0” for `next_cursor`—plans require it; treat as merge-blocking evidence.

3. **07-05/06/07:** one shared table in phase VALIDATION of “gameID provider sites” (presence, sysbroadcast, CoreServer publisher, hostcap, audit DLQ, cluster, TLS, world, plugin, relay) with `bus.GameID()` / `gameIDProvider` only—catch third independent `"main"` closure.

4. **07-01 execution:** actually store pipe output of Step-0 two-step census in SUMMARY; never alias-first.

5. **After phase:** file MODEL-02 doc gap issues (PROJECT.md / ARCHITECTURE.md event-sourcing drift) as plans defer—do not leave unfiled.

6. **Do not** reintroduce latch, plug-facing Seq, INV-GW rename’s, postgresEventStore bloom, or per-symbol gateway allowlists.

---

## Risk Assessment

| Area | Risk | Why |
|------|------|-----|
| ARCH-05 leaves + gate (01–04) | **LOW–MEDIUM** | Well scoped; largest value is 01 client extract (verified); 04 binding integrity if both tests annotate |
| Event model (02,05–08) | **MEDIUM** | 05 cycle + Qualify; 07 type collapse; 08 deliberate behavior fix with multi-site Seq hardcodes |
| Bootstrap Orchestrator (09–11) | **HIGH** | Correct design, extreme blast radius, int-only regressions, cryptoWiring + Prepare/Activate discipline |
| **Phase overall** | **HIGH** | Goals are achievable and plans are unusually rigorous; residual risk is execution/ops, not missing architecture. Without ironclad `test:int`/KEK gates at every wave, silent production regressions are plausible |

---

## Phase goals check

| Success criterion | Covered? | Plans |
|-------------------|----------|-------|
| Single Event representation; core.Event gone; no behavior change on wire | Yes (deliberate D-07 pagination fix only) | 02,05–08 |
| Bootstrap via Orchestrator; start/stop order unified | Yes (Wave A + B; topo pin; bounded StopAll) | 09–11 |
| Gateway imports only protocol-translation deps | Yes (extract + leaves + forbid + closure + bind) | 01–04 |

**Verdict:** Approve for execution **with HIGH risk ops**. Do not re-litigate settlements; enforce must_haves/acceptance greps literally; prefer smaller PR slices only if atomic intermediate states stay green (07-11 already forbids red commits).

---

## Consensus Summary

**Verdict: NOT execution-ready — rev 9 required. Four verified blockers, all specification defects with
small fixes; zero design reversals; every rev-8 (round-5) amendment held.**

Same split as round 5, same resolution: OpenCode approves (HIGH *operational* risk, zero plan-amendment
HIGHs — its three HIGHs are execution-discipline notes about risks the plans already document); Codex
refuses unchanged with six revision items. The orchestrator verified each Codex HIGH against the live
tree and plan text: three confirmed outright, one confirmed-narrowed, and the supporting MEDIUMs check
out. Consensus is weighted by grounding, not headcount.

### Blockers (orchestrator-verified)

1. **BLOCKER (Codex HIGH — VERIFIED) — 07-06's repo-wide payload-shape count is deterministically red.**
   Two criteria (`07-06-PLAN.md` acceptance + `<verification>`) require
   `rg -c 'json.Marshal\(map\[string\]string\{' --type go` to return **1** repo-wide. The live tree has
   **10** matches; at least 8 survive a correct implementation untouched
   (`plugins/core-scenes/service.go:420,1350,1569`, `cmd/holomush/phase7_fence_wiring.go:90`,
   `internal/grpc/dispatcher_test.go:45,62`, `plugins/core-scenes/publish_snapshot_integration_test.go:471`,
   `test/integration/plugin/command_introspection_parity_test.go:212`). Correct code cannot pass; the
   only paths to green are unrelated scope creep or skipping the gate. Same guaranteed-fail-count class
   as round 4's "exactly 17 files". *Fix:* scope the assertion to the two retired builder sites
   (`internal/command/types.go:629`, `internal/plugin/hostcap/system_broadcaster.go:51` — must lose the
   construct) plus exactly one occurrence inside `internal/sysbroadcast/`.

2. **BLOCKER (Codex HIGH — VERIFIED) — 07-07 prescribes calling an unexported cross-package bridge.**
   Task 2's CoreServer rewrite says the system actor is "mapped through the surviving bridge"
   (`07-07-PLAN.md:332-335`), but the surviving bridge is `coreActorToEventbusActor` — **unexported, in
   package `plugins`** (`internal/plugin/event_emitter.go:306`). `internal/grpc` cannot call it;
   exporting it creates a new plugin-owned API for a host caller; duplicating it violates the plan's own
   one-bridge criterion. *Fix (Codex's, sound):* CoreServer constructs the known system actor directly
   (`eventbus.Actor{Kind: ActorKindSystem, ...}` with the same ID mapping the bridge applies to
   `core.ActorSystem`); the strict bridge stays private to the plugin emit path where arbitrary
   `core.Actor` input exists; re-scope the "one bridge" criterion to "one general
   `core.Actor -> eventbus.Actor` converter" so direct typed construction elsewhere is legal.

3. **BLOCKER (Codex HIGH — VERIFIED, and it corrects round 5) — 07-10 Task 4 asserts on a private
   method.** Task 4's behavior/action text requires running the production graph "through `topoSort`"
   and inspecting its returned order/length — but `topoSort` is an unexported **method** on
   `*Orchestrator` (`internal/lifecycle/orchestrator.go:99`), unreachable from
   `cmd/holomush/core_topo_order_test.go` (package `main`). Round 5 refuted the adjacent finding by
   citing Task 3's dep-carrying-stub mechanism (`:486`, `:649`) — that refutation was **incomplete**: the
   stub route exists and Task 4 even says to reuse it, but Task 4's literal assertions ("topoSort returns
   no error", "the returned order contains...") cannot compile as written. All three assertions ARE
   observable through `Orchestrator.StartAll` over the dep-carrying stubs (error surfaces, start order =
   topo order, started-count = emitted-node count). *Fix:* either restate Task 4's assertions in
   StartAll-over-stubs terms, or export a narrow validation seam (e.g.
   `lifecycle.TopologicalOrder([]Subsystem)` / `Orchestrator.ValidateGraph`) used by both `StartAll` and
   the test. Wording-level change; the test's purpose and RED proof are untouched.

4. **BLOCKER (Codex HIGH — VERIFIED) — 07-11 row 13 composes with 07-09 to put cluster acquisition
   AFTER the barrier.** 07-09 (correctly) moves `ConnProvider`/`ClusterIDProvider` resolution out of
   `cluster.NewSubsystem` into the Start body (construction rejects missing values today,
   `internal/cluster/registry.go:78-90`). 07-11 row 13 then declares cluster `Prepare` a **no-op** and
   moves "the **entire** locked body" to `Activate` — so after both plans, fallible provider resolution
   (acquisition) runs in the Activate sweep, when gRPC/admin.sock may already be serving. That violates
   D-13.0's own Prepare contract ("acquire and wire everything the subsystem needs", `07-11-PLAN.md:163`)
   and is precisely the cross-plan-composition class round 3 named. *Fix:* row 13 splits cleanly —
   `Prepare` resolves and validates both providers, storing stable values; `Activate` keeps the
   subscribe/publish/goroutine critical section whole under `r.mu` (the no-split settlement governs the
   critical section, not resolution). The row-13 idempotency verdict then needs a Prepare-guard sentence
   too.

### Warnings (verified; fix in rev 9 alongside)

- **07-10 Stop goroutines need a buffered one-shot result channel** (`make(chan error, 1)`). As
  specified (goroutine + select on `ctx.Done()`), an abandoned `Stop` blocks forever on its send — the
  doc comment's "leaks until it returns" claim becomes false, and the goroutine leak is permanent, not
  transient. One-line criterion.
- **07-11 row 15's single-bool policy guard is unsound for multi-policy configs.** Live `Start` loops
  over `cfg.PolicyNames` with short-circuit-on-error (`internal/admin/policy/subsystem.go:47-56`); one
  `s.emitted` bool set after the loop re-emits the successful prefix on retry (duplicate audit events),
  set before suppresses never-emitted policies. Track completion per policy name, or validate-and-document
  a single-policy invariant.
- **07-04's closure test must check `packages.PrintErrors`, not just the `packages.Load` error.** The
  plan specifies `NeedName|NeedImports|NeedDeps` but no per-package error check; `packages.Load` returns
  nil while individual packages fail to load — a partial load is a security-relevant false green. The
  existing direct gate already does both checks (`gateway_imports_test.go:132-133`); mirror it.
- **07-08 Spec B is probabilistic as written.** Two concurrent random-ULID publishers make
  ULID-order-vs-stream-order divergence likely, not guaranteed. Build the RED from deliberately inverted
  precomputed IDs (publish the lexically higher ULID first, record the resulting stream sequences).

### Lesser findings (fold in where cheap)

- 07-01: add `task test:int` to Task 2's `<automated>` (the plan itself established `phase1_5_test.go`
  is invisible to `task test`). — 07-02: the moved `ValidatePayload` doc comment still tells callers to
  validate "before `EventStore.Append`", an API this phase deletes. — 07-03: keep a small
  `core.NewULID` forwarder smoke test in `internal/core`. — 07-05: rename internal `append_*` operation
  labels where tests don't treat them as contract. — 07-07: the amended rules should mandate
  `eventbus.NewEvent` (not merely a `core.NewULID`-stamped ID). — 07-08: describe zero-seq handling as
  compatibility for any legacy token rather than asserting cursors were never persisted. — OpenCode
  carries: paste Spec-A page-ID evidence into SUMMARY; a shared gameID-provider-site table in
  VALIDATION.md; file the MODEL-02 doc-drift issue during 07-07 as planned.

### What held, and what "converged" turned out to mean

Every round-5 amendment survived round 6 untouched: the row-16 reaper split and row-14 nil guard are
cited by Codex as strengths; the per-consumer provider signatures drew no finding; the 07-01 census fix
is called out as fixing "a real alias trap". But the round-4/5 claim that ARCH-04/05 plans had
"converged" is now falsified in an instructive way: blockers 1 and 2 live in 07-06 and 07-07 — plans
untouched since rev 6 — in acceptance-criteria semantics no prior round examined. Convergence measured
reviewer coverage, not plan correctness. The round-6 blocker class continues the trajectory:
architecture (rounds 3-4) → spec precision (round 5) → criterion/wording semantics (round 6). Blockers
are getting cheaper each round; none of the four requires touching the design.

### Divergence

OpenCode's approve-with-HIGH-ops-risk is not wrong about what it measured — its groundings all check
out, and its three HIGHs correctly name the phase's residual risk (07-09/11 blast radius under
integration-only visibility). It reviewed whether the plans are *coherent and grounded*; Codex reviewed
whether every gate *can go green on correct code*. Both questions matter; only the second produces
amendments. Fifth consecutive round of near-zero HIGH overlap between grounded reviewers.

### Recommendation

**Rev 9: four blocker amendments + four warnings + the lesser fold-ins.** All are text/criterion-level;
none reopens a settled design. Codex's 07-09 two-green-commits sequencing note is execution guidance the
planner may fold into 07-09's task ordering as a checkpoint. After rev 9, the shape of the remaining
findings (criterion semantics on untouched plans) suggests the marginal value of a full round 7 is
dropping — a scoped re-check of the four amended plans (07-06/07-07/07-10/07-11) would cover the change
surface.
