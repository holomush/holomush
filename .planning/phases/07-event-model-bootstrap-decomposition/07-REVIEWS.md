---
phase: 7
round: 7
reviewers: [codex, opencode]
reviewed_at: 2026-07-16T16:35:33Z
plans_reviewed: [07-01-PLAN.md, 07-02-PLAN.md, 07-03-PLAN.md, 07-04-PLAN.md, 07-05-PLAN.md, 07-06-PLAN.md, 07-07-PLAN.md, 07-08-PLAN.md, 07-09-PLAN.md, 07-10-PLAN.md, 07-11-PLAN.md]
plans_revision: "rev 9 (c3cb1e845)"
notes: "Round 6 preserved at 66906eb07. FOCUSED round: reviewers received all 11 plans plus a depth directive on 07-06/07-07/07-09/07-10/07-11 (the rev-9 change surface) with explicit can-go-green, implementability, and cross-plan-composition checks. No prior-round findings were shared. opencode: openrouter/x-ai/grok-4.5; codex: default model, codex-cli 0.144.4."
---

# Cross-AI Plan Review — Phase 7 (Round 7, rev 9, focused)

## Codex Review

# Summary

**Verdict: NOT READY. Overall risk: HIGH.**

The plan set is unusually well-grounded and most of 07-01 through 07-08 is executable. The wave ordering, import-cycle handling, integration-test coverage, and lifecycle threat analysis are strong. However, source verification found three blocking contradictions in 07-09/07-11, plus two behavior-preservation gaps in 07-07. The main bootstrap issue is that the proposed single game-ID provider omits the EventBus itself, while 07-10 explicitly pins the incompatible assumption that EventBus has no database dependency.

## Plan-by-plan assessment

| Plan | Assessment | Risk | Key observation |
|---|---|---:|---|
| 07-01 | Ready | LOW | `internal/grpc/client.go` is genuinely leaf-compatible: its imports are external libraries and generated proto packages only. [Source](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/grpc/client.go:7>) |
| 07-02 | Ready | LOW | Moving the event vocabulary resolves the real gateway/eventbus collision without moving actor or payload ownership. Current vocabulary is concentrated in `core/event.go`. [Source](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/event.go:35>) |
| 07-03 | Ready | MEDIUM | The value-leaf extractions are coherent and enforce D-15 without substituting `idgen` for monotonic session/event ULIDs. |
| 07-04 | Ready | LOW | The current gate inspects direct imports only; adding a build-graph closure assertion makes the invariant binding substantive. [Source](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/gateway_imports_test.go:140>) |
| 07-05 | Ready | MEDIUM | The auth-local interface is necessary: auth currently stores and accepts `*core.Engine` directly, including both disconnect and end-session calls. [Source](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/auth/auth_service.go:26>) |
| 07-06 | Ready | MEDIUM | The single broadcast builder removes a verified duplicate payload/event-construction path. [Command copy](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/command/types.go:619>), [hostcap copy](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/hostcap/system_broadcaster.go:45>) |
| 07-07 | Needs revision | HIGH | The collapse is directionally correct, but actor-ID formatting and publisher wrapping need stronger instructions and tests. |
| 07-08 | Ready after 07-07 | MEDIUM-HIGH | The Seq bug is real: the current adapter destroys Seq before cursor encoding. [Source](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/sub_grpc.go:903>) |
| 07-09 | Not ready | HIGH | The game-ID migration omits EventBus, and the “hoist whole/verbatim” instruction cannot coexist with pre-`StartAll` subsystem registration. |
| 07-10 | Conditionally sound | HIGH | Its shutdown design is good, but its `EventBus.DependsOn()==nil` pin conflicts with the correction 07-09 needs. |
| 07-11 | Not ready | HIGH | The audit Prepare guard cannot work after the proposed split; one focused test also lacks the required injection seam. |

# Strengths

- The ARCH-05 sequence is well designed. Extracting the client first removes the consequential `internal/grpc` closure before enforcing the stronger forbidden list. The current telnet dependency is exactly one client/error-classification import. [Source](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/telnet/gateway_handler.go:27>)

- 07-04 correctly distinguishes direct imports from transitive closure. The current AST gate only iterates `file.Imports`, so the added closure test is what genuinely enforces D-15. [Source](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/gateway_imports_test.go:148>)

- 07-05 preserves load-bearing presence behavior. `Engine` contains the typed-nil guard, while `EndSession` intentionally ignores the caller context and uses a bounded background commit. [Typed-nil guard](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/engine.go:37>), [terminal commit](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/engine_end_session.go:33>)

- 07-07/07-08 accurately trace the pagination defect. `busEventToCoreEvent` explicitly discards Seq, while the public cursor encoder hardcodes zero. [Conversion](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/sub_grpc.go:969>), [cursor](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/hostcap/servers.go:1279>)

- 07-10’s shutdown repair is strong. Today rollback inherits the startup context and `StopAll` invokes each `Stop` synchronously, so a cancelled boot or context-ignoring subsystem can strand cleanup. [Source](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/lifecycle/orchestrator.go:44>)

- 07-11’s phase semantics are grounded in actual mechanics:

  - Embedded NATS must run in Prepare because audit acquisition needs live JetStream.
  - The cluster subscribe/publish/goroutine block must remain under one lock. [Source](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/cluster/heartbeat.go:19>)
  - Admin lock acquisition and socket binding are atomic inside `Server.Start`, so keeping them together in Activate is correct. [Source](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/admin/socket/server.go:58>)

- The plans consistently recognize that `task test` does not compile integration-tagged callers. Requiring `task test:int` at each cross-package seam is appropriate.

# Concerns

- **HIGH — BLOCKER: 07-09’s “single game-ID provider” omits EventBus, and 07-10 pins the omission.**

  The database game ID is generated as a ULID and persisted. [Source](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/store/postgres.go:96>) `runCore` currently resolves the effective game ID from `core.game_id` or that database value. [Source](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/core.go:300>)

  EventBus, however, receives a separately loaded `eventBusConfig`; its defaults replace an empty game ID with literal `"main"`, and the subsystem freezes those defaults at construction. [Defaults](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/config.go:168>), [construction](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/subsystem.go:62>)

  Therefore the proposed provider migration can still produce:

  ```text
  EventBus: events.main.*
  world/plugin/TLS/outbox/crypto: events.<database-ULID>.*
  ```

  This contradicts 07-09’s own “one provider, one override site” truth. Fixing it requires EventBus to resolve the same provider at Start/Prepare and therefore declare `SubsystemDatabase`. That directly invalidates 07-10’s required `EventBus.DependsOn() == nil` test and its exact topo-order assumptions.

- **HIGH — BLOCKER: 07-09 cannot hoist the entire crypto block “whole/verbatim.”**

  The block currently constructs lifecycle subsystems:

  - `cryptoPolicySub` at [core.go:738](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/core.go:738>)
  - `rekeyCheckpointSweepSub` at [core.go:774](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/core.go:774>)
  - `cryptoChainVerifierSub` at [core.go:1053](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/core.go:1053>)

  But every subsystem must already exist and be registered before `StartAll`. [Source](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/core.go:1081>)

  A memoized builder first invoked by one of those subsystems cannot also create that subsystem. The provider table points toward the correct design, but the repeated “whole/verbatim” instruction contradicts it and is not implementable literally.

- **HIGH — BLOCKER: 07-11’s audit Prepare guard is tied to Activate-owned state.**

  The only current idempotency guard is `s.worker != nil`. [Source](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/audit/subsystem.go:267>) That field is assigned only after `p.start(workerCtx)`. [Source](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/audit/subsystem.go:313>)

  07-11 moves `p.start` into Activate while saying to keep the existing guard on Prepare. After that split:

  - Prepare never sets `worker`.
  - Repeated Prepare re-runs backfill, late initialization, and durable-consumer construction.
  - No specified field carries the prepared projection and partition manager into Activate.
  - Stop cannot distinguish prepared-only from never-prepared.

  This needs an explicit prepared-state design, not the current worker guard.

- **MEDIUM — 07-07 changes zero actor IDs unless conversion is zero-aware.**

  Today `busEventToCoreEvent` deliberately maps a zero ULID to an empty actor ID. [Source](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/sub_grpc.go:976>) The plugin proto and Lua paths then expose that string directly. [Proto conversion](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/hostcap/servers.go:1298>), [Lua conversion](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/hostfunc/stdlib_focus.go:426>)

  The plan’s prescribed `e.Actor.ID.String()` turns zero into `"00000000000000000000000000"`, which is observable behavior change. Use a zero-aware helper and add tests for zero and nonzero actor IDs.

- **MEDIUM — 07-07 must explicitly retain the wrapped publisher.**

  The current CoreServer publication path receives the publisher after `wrapPublisher`, not the raw bus publisher. [Source](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/sub_grpc.go:264>) This matters because audit persistence fails closed if `App-Rendering` is absent. [Source](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/audit/projection.go:382>)

  “Pass the underlying publisher” is ambiguous. It must say: pass the `publisher` returned by `wrapPublisher`, never `rawPublisher`.

- **MEDIUM — 07-11’s two-reaper focused test lacks a guest-reaper timing seam.**

  `grpcSubsystemConfig` exposes only the session `ReaperInterval`, while guest reaping is hardcoded to one minute. [Configuration](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/sub_grpc.go:80>), [hardcoded guest interval](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/sub_grpc.go:760>)

  The required “shortened interval, wait ≥3×, then observe both reapers” test cannot be implemented quickly and deterministically as written. A guest interval or runner/factory seam must be specified.

- **MEDIUM — one 07-11 acceptance gate repeats the under-matching regex defect the plan warns against.**

  The plan requires comparing file sets from patterns limited to arguments shaped like `ctx`, `context.*`, or `startCtx`. Current callers include `s.Start(t.Context())`, the exact form previous revisions missed. [Source](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/audit/subsystem_test.go:130>)

  This criterion can report success while omitting correctly migrated `Prepare(t.Context())` or `Activate(t.Context())` sites.

# Suggestions

- Add an EventBus game-ID provider in 07-09. The provider should win in core-process wiring while literal `Config.GameID` remains available for standalone tests and tools. Resolve it in EventBus Start/Prepare and add `SubsystemDatabase` to `DependsOn`.

- Revise 07-10 accordingly:

  - Replace the nil-dependency test with an exact `[SubsystemDatabase]` test.
  - Recompute the exact topological sequence.
  - Keep the prohibition on `EventBus → CryptoChainVerifier`; the database edge does not create that cycle.

- Replace “hoist the whole block verbatim” with an explicit split:

  - Before `StartAll`: construct `cryptoPolicySub`, sweep, verifier, admin socket, and gRPC using provider closures.
  - Inside `cryptoWiring`: build only shared resolved values such as repos, emitters, managers, handlers, and coordinator inputs.
  - Document the precise `cryptoWiring` fields and which pre-`StartAll` constructor consumes each provider.

- Give audit explicit phase state:

  ```go
  preparedProjection *projection
  partitionManager    *EventsAuditPartitionManager
  worker              *projection // activated/running
  cancel              context.CancelFunc
  ```

  Prepare should guard on prepared state; Activate should guard on running state; Stop should handle and clear both prepared-only and activated states.

- Amend 07-07 to require:

  - zero-aware actor-ID formatting;
  - the wrapped RenderingPublisher;
  - a test proving `actor_id == ""` for a zero actor;
  - an integration assertion that a command-response event reaches `events_audit` with `App-Rendering`.

- Add a deterministic guest-reaper test seam, preferably a configurable interval or injected runner/factory, instead of relying on a one-minute production constant and wall-clock sleeps.

- Replace the argument-shaped Prepare/Activate grep with either `go/packages` receiver-type classification or the plan’s own broad `rg '\.(Prepare|Activate)\('` plus recorded receiver classification.

# Risk Assessment

**Overall: HIGH / NOT READY.**

- **ARCH-05:** LOW–MEDIUM. Plans 07-01 through 07-04 are coherent and substantially ready.
- **ARCH-04:** MEDIUM–HIGH. The structural direction is sound, but 07-07 needs two explicit behavior-preservation corrections.
- **ARCH-03:** HIGH. The EventBus game-ID omission, crypto-hoist contradiction, and audit phase-state defect can respectively cause subject partitioning, an unimplementable bootstrap graph, and non-idempotent lifecycle behavior.

After those corrections and recomputation of the 07-10 topology assertions, the phase should drop to MEDIUM execution risk.

---

## OpenCode Review (grok-4.5)

# Phase 7 Cross-AI Plan Review

**Overall:** HIGH quality plans after heavy prior review. Load-bearing claims below were checked against this worktree; key traps (auth↔presence cycle, BeforeID-is-tripwire, reverse EventBus→Verifier cycle, atomic admin-socket Start) are correctly handled. Residual risk is **execution size / bootstrap complexity**, not missing architecture decisions.

---

## Phase-level assessment

| Area | Verdict |
|------|---------|
| Phase goals (ARCH-03/04/05) | Covered end-to-end with sensible waves |
| Locked D-01…D-18 | Honored; known false rationales corrected in `must_settle` / drift blocks |
| Wave DAG + empty parallel overlaps | `07-03∥07-05` and `07-04∥07-06` share **0** `files_modified` (verified); wave 1→2 sharing `gateway_handler.go` correctly serializes via `depends_on` |
| Requirement mapping | ARCH-05 → 01–04; ARCH-04 → 02–08; ARCH-03 → 09–11 |
| Risk | **MEDIUM** (delivery blast radius of 09–11), not HIGH design risk |

---

## 07-01 — `internal/grpcclient` extraction

### Summary
Sound first cut: moves the real domain-closing import (`internal/grpc`) out of the gateway before littering the tree with vocabulary leaves. Drift fix on `cmd/holomush/gateway.go` is real.

### Strengths
- Live closure numbers match: `go list -deps ./internal/telnet` → **47** internal pkgs; `gateway.go:20` imports `internal/grpc` and is outside `coreOnlyFiles`.
- Verbatim move + leaf-only imports on client package is right-sized.
- Step-0 symbol-first census (not alias-first) correctly addresses the `phase1_5_test.go` / `grpcpkg` miss.
- Threat T-07-02 keeps `SESSION_NOT_FOUND` classification byte-stable.

### Concerns
- **LOW:** Acceptance greps are solid; residual miss risk is any third alias in a brand-new file—Step 0 mitigates it.
- **LOW:** Task 2 `task test:int` in the per-task gate is correctly mandatory for integration-tagged dual-use files.

### Suggestions
- None blocking. Keep Step 0 output in SUMMARY as written.

### Risk
**LOW**

---

## 07-02 — `internal/eventvocab`

### Summary
Correct collision fix: vocabulary leaf before Event collapse so telnet never needs `eventbus`. Scope note that this plan does **not** zero gateway `core` imports is accurate and important.

### Strengths
- Live sites: `telnet/gateway_handler.go` still uses arrive/leave EventTypes; plan moves them without claiming full core ban.
- Excluding `Actor` / `ActorKind` preserves D-01 (`command` must not import `eventbus`).
- Explicit retraction of unachievable “zero core in telnet/web” greps avoids executor scope collapse.

### Concerns
- **LOW:** 34 consumers is wide; census re-run instruction is the right mitigation.
- **LOW:** Doc rewrite for `ValidatePayload` (drop stale `EventStore.Append`) is good hygiene.

### Suggestions
- When repointing, fail the planned repo-wide `core.EventType*` grep once—don’t accept “looks local.”

### Risk
**LOW**

---

## 07-03 — gateway leaves (`ulidgen` / `cmdparse` / `sessionlease`)

### Summary
Completes ARCH-05 leaf work so 07-04 can forbid wholesale. Drift-prevention rationale for forbidding already-leaf `core`/`session` is correct (live: both have no internal deps).

### Strengths
- Forwarder for `core.NewULID` preserves D-03 eventbus→core import and notifier Docs.
- `session` deps verified leaf-only; plan’s D-16 justification correction is accurate.
- End-of-plan full gateway closure gate correctly owned here (not 07-02).

### Concerns
- **MEDIUM:** CLAUDE.md ULID section is touched here, then rewritten again in 07-07—short shared-doc window for merge conflict / half-updated rule text.
- **LOW:** Lease value pin (`15s`) is correctly treated as ASVS V3-sensitive.

### Suggestions
- In 07-03 SUMMARY, note “07-07 owns NewEvent prose” so executors do not “finish” ULID docs early.

### Risk
**LOW–MEDIUM**

---

## 07-04 — forbidden + closure gate + INV-EVENTBUS-1 bind

### Summary
Makes ARCH-05 permanent. FINDING-3 (AST = direct only) is real; phantom `internal/auth/service` is real (`gateway_imports_test.go:107`, `invariants.yaml:2345`; no such package under `internal/auth/`).

### Strengths
- Live positive control: grpc still reaches store (used by Test 3).
- Single shared `gatewayForbiddenPackages` list prevents dual-list drift.
- D-18 handled: bind `INV-EVENTBUS-1`, fix refs token, leave GW regex fixtures alone.
- Binding both AST + closure tests avoids partial INV-RB-3-class binding.

### Concerns
- **LOW:** Positive control durability depends on grpc→store surviving; plan’s replace-don’t-delete comment is adequate.
- **LOW:** `packages.PrintErrors` after every Load is correctly required (partial load = false green).

### Suggestions
- None blocking.

### Risk
**LOW**

---

## 07-05 — `presence.Emitter` + auth cycle break

### Summary
Blocking FINDING-1 and FINDING-5 are verified and settled correctly. Move (not port) of Engine with real EndSession semantics exceeds the original false “no logic” rationale.

### Strengths
- Cycle proof holds: `go list -deps ./internal/eventbus` includes `internal/auth`; `auth_service.go:28,39,115,235,243` hold `*core.Engine`.
- Live load-bearing logic: typed-nil guard (`engine.go`), EndSession ctx ignore + 5s background + cause-dependent actor (`engine_end_session.go:33–59`) must travel—and the plan requires that.
- `gameID func() string` mirrors `busHistoryReaderAdapter` (`sub_grpc.go:906–924`) and `Qualify`+`NewEvent`+`NewType` path of `busEventAppender` (`:821–845`)—right contract to keep subjects wired.
- Auth two-method consumer interface (no `EmitArrive`) matches actual call sites.

### Concerns
- **MEDIUM:** `CharacterRef` stays in `core`; if a later phase moves it, auth’s interface cycles again—deferred residue, acceptable for Phase 7.
- **LOW:** Operation-label rename (`append_*` → `publish_*`) vs stable error code `SESSION_ENDED_APPEND_FAILED` is the right split.

### Suggestions
- Before coding, re-run the cycle pre-flight listed in the plan; treat it as a hard stop.

### Risk
**MEDIUM** (import-cycle/subject-qualification surface if FINDING-5 is skipped)

---

## 07-06 — one system broadcast builder ⭐ deep

### Summary
Correct collapse of the admitted drift contract between `command.BroadcastSystemMessage` and `hostcap/system_broadcaster`. FINDING-5 wiring (Publisher alone cannot qualify) is applied consistently with 07-05.

### Strengths
- Live duplicate:
  - `internal/command/types.go:622–641` marshals `{"message":...}` + `core.NewEvent` + Append
  - `hostcap/system_broadcaster.go:51–59` same shape + same `SYSTEM_BROADCAST_FAILED`
- Consumer ports keep `command` free of `eventbus` (D-01 / MEDIUM-4).
- Scoped one-builder proof (not repo-wide `json.Marshal(map[string]string{`) is correct: live count ≈ **10**, many legitimate non-broadcast sites.
- `ServicesConfig.Events` required-field cascade (`types.go:597–601`) is explicitly redesignated `Broadcaster`.
- Pluginparity subject rewrite bare `"system"` → qualified `events.<gameID>.system` matches how emission works after appender death.
- Lua-only SessionAdmin asymmetry left untouched (runtime symmetry rule).

### Concerns
- **MEDIUM:** `NewServices` requires non-nil Events today; plan keeps nilbroadcaster early-return in method while validating config—fine for `NewTestServices`, but documents should keep both paths explicit so tests don’t flip to panics.
- **LOW:** Wave 4 parallel with 07-04 is safe (empty intersection); depends on 07-05 only for presence/gameID patterns—type-hard? No; process-order for shared `sub_grpc` wiring is fine via wave order.
- **LOW:** `DisconnectSession` retain is correctly pinned after prior review miss.

### Suggestions
- Acceptance criterion on `bus.GameID()` count ≥ 2 in `sub_grpc.go` is good; also assert **one closure binding** feeds presence + broadcaster + SessionAdmin so three literal closures do not drift.

### Risk
**MEDIUM**

---

## 07-07 — delete `core.Event` / collapse bridges ⭐ deep

### Summary
This is the ARCH-04 core volume. Constraints (stay in `eventbus` for `auditRow`; keep `core.Actor`; dual `HistoryReader` interfaces; explicit CoreServer publication seam) match live code. Round-6 “direct system actor literal, don’t import unexported plugin bridge” is **necessary**: `coreActorToEventbusActor` lives unexported in `internal/plugin` (`event_emitter.go:306`) and `internal/grpc` cannot call it.

### Strengths
- Verified chain D-07 depends on: QueryHistory has Seq → `busEventToCoreEvent` (`sub_grpc.go:976`) drops it → `encodeHostEventCursor` hardcodes `Seq: 0` (`servers.go:1285–1290`) → `ReplayTail` never sets `BeforeSeq` (`sub_grpc.go:930–936`).
- Dual interface + structural typing (`lua/hostcap_adapter.go:226–231`) correctly forced lockstep retype.
- `WithEventStore` sites: 11 live matches—plan enumerates them.
- Non-ULID rejection stays on the **one** surviving bridge; crypto suites still pin it.
- Rules rewrite of always-loaded `core.NewEvent` mandate (`CLAUDE.md:146`, `.claude/rules/event-conventions.md:49–50`) is mandatory, not tidy-up.
- `AppSchemaVersion = 1` not bumped—aligns with MODEL-01/Phase 5.

### Concerns
- **HIGH (execution):** Task 2 volume (~34 files + descriptive retypes). Plan is complete, but one missed consumer of `core.Event` fails late in int. Grep criteria help; still the largest ARCH-04 commit.
- **MEDIUM:** Task 1 intentionally leaves `Seq: 0` until 07-08—any interim integration that thinks pagination is fixed will false-green; keep SUMMARY loud.
- **MEDIUM:** CoreServer system actor literal: plan says construct `eventbus.Actor{Kind: ActorKindSystem, ID: core.SystemActorULID}`. Live map is `ActorSystemID = SystemActorULID.String()` (`core/event.go:172–183`) and bridge parses string→ULID. Literal on ULID sentinel is correct **if** implementer mirrors `bridgeActorKind`+System ULID exactly; instruct “read live bridge + `TestActorSystemIDIsSystemActorULIDString` before coding.”
- **LOW:** Comment-only fixing of `core.NewEvent` mentions outside RULE files (ulid.go, idle_scheduler, etc.) will break plan greps if skipped—listed correctly in `files_modified`.

### Suggestions
- Run crypto-reviewer as gate **before** claim complete (touches `event_emitter.go`).
- Prefer single `task test:int` after every shared-type edit, not only plan end.

### Risk
**MEDIUM–HIGH** (blast radius / crypto surface, design is sound)

---

## 07-08 — Seq pagination + D-08 guard ⭐ deep

### Summary
Correct live bugfix. **RED framing correction is authoritative**: with `BeforeSeq == 0`, `BeforeID` is only a tripwire (`bus.go:88–105`); hot `matchesQuery` has **no BeforeID branch** (`hot_jetstream.go:392–402`); buildConfig only takes cursor path when `BeforeSeq > 0` (`:338`). Multipage on a quiet stream **repeats the newest page**—verified mechanism.

### Strengths
- Three independent Lua encode hardcodes (`stdlib_focus.go:441` per-event, `:463` next_cursor) + hostcap (`servers.go:1285–1290`) catalogued.
- Page-advance anchor = oldest = index 0 (`servers.go:900–906`; stdlib comments)—plan forbids “last event” trap that reintroduces repeats while grepping nonzero Seq green.
- Legacy zero-seq policy = tail restart (not fictional ID fallback)—matches bus contract.
- Spec B deterministic inverted ULID order is better than concurrent race.
- Package-main integration test locates unexported `busHistoryReaderAdapter`—correct FINDING-7.
- D-08 census test without fabricated `// Verifies:` is right.

### Concerns
- **MEDIUM:** Spec A is the only assertion that fails a wrong-but-nonzero anchor—if Spec A is weakened by an over-eager coordinator, bug returns. SUMMARY must paste disjoint page ID sets.
- **LOW:** Signature option (`beforeSeq` param vs cursor struct) is fine; lockstep on both interfaces is mandatory (compile symmetry gate).
- **LOW:** crypto-reviewer on history path before push—already noted.

### Suggestions
- Do **not** collapse Spec B after Spec A greens; Spec B alone proves advance-by-seq.

### Risk
**MEDIUM** (behavior change; TDD RED gate is clear)

---

## 07-09 — Wave A eager-start removal ⭐ deep

### Summary
Addresses a documented production boot-panic class. Five eager starts exist at live lines (`core.go:287,462,797,800,977`); `eventBusOwnedByOrchestrator` juggling exists; `ensureTLSCerts` is still an inline function; `productionSubsystems` is **16** positional params; `SubsystemTLS` is already an unused iota. Settlements (cryptoWiring, gameID funnel census, THE RULE, orphan+crypto-operator whole-file moves) are necessary and mostly implementable.

### Strengths
- Root causes named match comments in `core.go` (DB/TLS gameID, cluster Conn at construct, admin handlers needing Hasher/Resolver).
- Cluster `Start`/`Stop` live in `heartbeat.go:22/110` (not `registry.go`)—plan hard-codes that landmine.
- THE RULE → verifier grows EventBus dep; reverse edge forbidden—actually grounded in handler construction (`core.go:1047–1057` + `readstream_wiring.go:118` needs AuditPublisher).
- Coordinor stays non-subsystem owned by grpc—matches reality (interface Start/Stop, no ID/DependsOn).
- File moves for orphan/crypto-operator correctly forbid duplicate “definitions left in main”.
- T-07-51 re-scope: both external domains DependsOn chain verifier—addresses Wave-A bind-before-verify under ID tie-break.

### Concerns
- **HIGH (delivery):** Task 2 is the hardest plan in the phase: ~20 live accessors, five consumer-owned providers, dual-path opts, two whole-package moves, gameID migration across many types, KEK-wired E2E as the only true regression. Design is settled; **executor skip-or-improvise is the failure mode**.
- **MEDIUM:** `cryptoWiring` + dual-path fields increase abstract types; dual-path must not silent-prefer concrete zero values over providers—doc “provider wins when non-nil” must be mechanical in Start.
- **MEDIUM:** KEK boot gate must use **`task test:int`**, not `task test` (integration tag)—plan already corrected prior false-green; do not regress.
- **LOW:** Named struct for production subsystems (16→17) cascades hard-coded `[16]stub` tests—plan owns it.

### Suggestions
- Suggested A/B commit split inside Task 2 is good; if intermediate cannot build, one green commit is better than a red “bisectable” commit.
- Phase-wide gameID census table in SUMMARY is non-optional quality bar.

### Risk
**HIGH** (process/execution), **MEDIUM** if executed strictly by settlements

---

## 07-10 — StopAll deadline + honest graph ⭐ deep

### Summary
Closes D-14 ride-alongs with a hard correction of rev-4’s unbootable reverse edge. Live graph facts: `eventbus.DependsOn() == nil` (`subsystem.go:77`); false comment `// runs before EventBus` at `core.go:1476`; gRPC DependsOn lacks AuditProjection (`sub_grpc.go:170`); StartAll rollback `o.StopAll(ctx)` on startup ctx (`orchestrator.go:58–60`); StopAll never checks deadline (`:81–96`); `defer orch.StopAll(context.Background())` still at `:1105`.

### Strengths
- Deferred-args trap and required **closure + WithTimeout inside defer** matches existing telemetry/obsidianshape (`core.go:255–261`).
- Rollback must use fresh bounded background ctx after deadline-aware StopAll (**amplifies abandon-on-cancel** otherwise)—correct.
- Append-to-startOrder-only-on-success today omits the failing subsystem from teardown—plan later pairs with 07-11 partial-prepare fix; 07-10 still improves cancel inheritance.
- Task 4 acyclicity over **live** DependsOn would have caught EventBus⇄Verifier; cycle message generic, so dumping ID→deps map is essential.
- Verifier-before-grpc/admin pins encode T-07-51 intent without reverse edge.

### Concerns
- **MEDIUM:** Task 1 “run Stop in goroutine + select on ctx.Done()” permanently abandons hung Stops; doc must keep “process-terminal + at most one boot-rollback” assumption (07-11 rewrite must not accumulate leaks).
- **MEDIUM:** Buffered `make(chan error, 1)` is load-bearing against permanent goroutine block—easy to reimplement wrong.
- **LOW:** Task 3 must not reuse nil-deps `stubSubsystem` (`core_subsystems_test.go:22`)—plan correctly bans it.
- **LOW:** Observing topo via StartAll recording stubs (not calling unexported `topoSort`) is required—plan got this right after earlier uncompilable greps.

### Suggestions
- RED for Task 4 must actually add the reverse edge temporarily and capture the failure text.
- Keep comment on `eventbus.DependsOn` as the **primary** anti-reintroduce control (name constants in comment is OK; nil-test pins runtime).

### Risk
**MEDIUM**

---

## 07-11 — Prepare/Activate ⭐ deep

### Summary
Wave B structuralizes acquire-before-serve. D-13.0 redefinition after identifying the incoherent “universal barrier” is **correct**: `DontListen: true` + `HTTPPort: MonitorPort` + `go s.server.Start()` inside connect (`eventbus/subsystem.go:153–164`) means embedded NATS is substrate, not domain serve; `audit` hard-fails without JS (`AUDIT_DEP_NOT_STARTED`). Admin socket `Server.Start` is atomic lock+bind+serve (`server.go:58–113`)—Prepare-only-construct + Activate-call-atomic is the only non-redesign option.

### Strengths
- Two full sweeps + single Stop teardown is the only design that matches D-11 intent.
- 17-row D-13.3 dispositions are planner-set, not executor-discretion (critical improvement vs early drafts).
- Cluster: after 07-09, provider resolution in **Prepare**; locked critical section whole in **Activate**—avoids post-barrier fallible acquisition.
- gRPC reapers at `:758/:769` are real domain loops launched before listen—plan assigns construct Prep / Run Act.
- Caller blast radius + integration invisibility under `task test` is correctly treated as landmine; ~27 files, receiver-type census (not arg-shape regex).
- Rollback bugs: include failing subsystem in preparedOrder; fresh stop ctx—both real vs current orchestrator.
- Policy multi-emit Activate needs **per-name** completion map (not a single bool)—sound.
- Disabled-mode admin: `Activate` nil-server return avoids panic under two sweeps.
- Property test: no Activate before last Prepare over production graph.

### Concerns
- **HIGH (delivery):** ~54 `files_modified`; migration is atomic (interface breaks everything). One green mega-change or carefully sharded local work then squash—PR review load is large.
- **MEDIUM:** Barrier residual T-07-62—domain surface smuggled into Prepare is review-enforced only. Summary 17-row table + reading all Activate bodies is the real control.
- **MEDIUM:** Compatibility `Start` shim is correctly forbidden; any such shim would gut D-11.
- **MEDIUM:** D-13.2 guards + four repeated-Activate tests are easy to half-implement; plan names them—enforce in SUMMARY.
- **LOW:** Plugin Activate documented no-op—verified no delivery loop in setup.Start (`manager.go:458` sync delivery). Good that Phase-8/MEDIUM-4 dust is not disturbed.
- **LOW:** Non-subsystem Stops (RetentionWorker, PluginConsumerManager, invalidation) correctly exempted from zero-`Start` greps.

### Suggestions
- Gate claiming done solely on **`task test:int` + KEK E2Es + 07-10 pin still green**.
- Stop-and-report if any D-13.3 row disagrees with live code—do not “fix the table.”

### Risk
**HIGH** (delivery/size), **MEDIUM** design risk after settlements

---

## Cross-plan composition (deep focus findings)

| Interaction | Status |
|-------------|--------|
| 07-05/06/07 share gameID `bus.GameID()` funnel vs 07-09 `gameIDProvider` | Correct two-funnel model; both resolve configured game id—SUMMARY census establishes drift detection |
| 07-09 mutates verifier DependsOn; 07-10 retches reverse edge | Composition fixed; Task 4 acyclicity is permanent guard |
| 07-07 unlocks Seq on events; 07-08 consumes | Ordered; intermediate Seq=0 intentional |
| 07-10 StopAll deadline; 07-11 two-sweep rollback | 07-10 lands cancel-safe rollback first; 07-11 must **preserve** fresh stop ctx |
| 07-01–03 leave gate permanentable before 07-07 edits core | Explicit `depends_on: [07-04,07-06]` on 07-07 is correct (rev 3) |
| Plugin Runtime symmetry fronts (HistoryReader×2, SessionAdmin Lua, seq encode×2 runtimes) | Consistently called out |

---

## Phase goal fit

| Success criterion | Plan coverage |
|-------------------|---------------|
| Single Event representation | 07-02,05,06,07 (+08 behavior fix) |
| Bootstrap via Orchestrator, order owned by graph | 09 (eager outs) → 10 (honest edges) → 11 (structure) |
| Gateway protocol-translation only | 01–04; naming stays leaf |

No major missing requirement for ARCH-03–05. D-07 deliberate behavior change is scoped with RED tests. Out-of-scope items (MEDIUM-4 full unwind, plugin Seq exposure, core grab-bag residual) stay out.

---

## Top residual risks (actionable)

1. **07-09 Task 2 executor discipline** — Improvising cryptoWiring provider wiring recreates boot panics THE RULE prevents.
2. **07-11 mega-migration** — Integration callers invisible to `task test`; only `task test:int` proves completeness.
3. **07-07 crypto emit path** — Must keep non-ULID rejection + param gate green; crypto-reviewer required.
4. **07-08 anchor correctness** — Spec A ID-disjointness is the real pin; do not accept “Seq nonzero” alone.
5. **Doc dual-touch (CLAUDE.md)** — 07-03 then 07-07; avoid half-applied NewEvent/ULID rules.

---

## Verdict

| Plan | Risk | Ready to execute? |
|------|------|-------------------|
| 07-01 | LOW | Yes |
| 07-02 | LOW | Yes |
| 07-03 | LOW–MED | Yes |
| 07-04 | LOW | Yes |
| 07-05 | MED | Yes (cycle + FINDING-5 non-optional) |
| 07-06 | MED | Yes |
| 07-07 | MED–HIGH | Yes (biggest ARCH-04 blast radius) |
| 07-08 | MED | Yes (TDD sequence mandatory) |
| 07-09 | HIGH (exec) | Yes **only if Task 2 is applied as settlements, not redesign** |
| 07-10 | MED | Yes |
| 07-11 | HIGH (exec) | Yes **after 09–10 green; treat as structural, not mechanical rename** |

**Phase package:** Ready for execution. Design decisions are above typical draft quality; remaining risk is disciplined implementation under size, not open architecture ambiguity. Prioritize KEK-wired boots, `task test:int` on every shared-type wave, and the 07-08/07-10 RED proofs as non-negotiable gates.

---

## Consensus Summary

**Verdict: NOT execution-ready — rev 10 required. Three verified blockers plus four verified MEDIUMs,
concentrated in 07-07/07-09/07-10/07-11. Every rev-9 (round-6) amendment held. Codex itself estimates
the phase drops to MEDIUM execution risk after these corrections.**

The now-familiar split: OpenCode says **ready for execution** (all 11 plans pass; its HIGHs are
execution-size discipline, and it independently confirmed every rev-9 fix on its deep pass). Codex says
**NOT READY** with three blockers and four MEDIUMs. The orchestrator verified every Codex finding against
the live tree and plan text: **all seven confirmed.** Consensus weighted by grounding. Sixth consecutive
round of near-zero HIGH overlap between grounded reviewers.

This round's blocker class is new again — the trajectory is architecture (r3-4) → spec precision (r5) →
criterion wording (r6) → **composition with real config/state semantics (r7)**. Two of the three
blockers are grounded partly in pre-existing system behavior the plans overclaim about, not in plan
wording alone.

### Blockers (orchestrator-verified)

1. **BLOCKER (Codex HIGH — VERIFIED) — 07-09's gameID funnel omits the EventBus itself, and 07-10 pins
   the omission.** The resolved gameID (`cfg.GameID`, else `dbSub.GameID()`, `cmd/holomush/core.go:300-302`)
   feeds world/plugin/relay/dek/EmitDeps — but never the eventbus config: `eventBusConfig` is loaded
   independently (`core.go:136`), `eventbus.Config.Defaults()` substitutes literal `"main"` for an empty
   GameID (`internal/eventbus/config.go:~170`), and the subsystem freezes it at construction. Nothing in
   `core.go` copies the resolved value in — the DLQ comment at `core.go:573-574` already fights exactly
   this mismatch. So with `game_id` unset, the bus qualifies subjects as `events.main.*` while
   DB-derived consumers use `events.<db-ulid>.*` — a **pre-existing divergence** that 07-09's own
   "one provider, one override site" truth and 12-row census paper over by omitting the bus. And the
   07-05/06/07 emit paths deliberately funnel through `bus.GameID()`, so the bus's value is the
   qualification source for exactly the events this phase migrates.
   *Fix:* add EventBus to the provider migration — dual-path `GameIDProvider` resolved at Start (Wave A)
   / Prepare (Wave B), literal `Config.GameID` retained for standalone tools/tests, provider wins when
   non-nil — which adds `SubsystemDatabase` to `EventBus.DependsOn()`. **Cross-plan consequence for
   07-10:** replace the pinned `EventBus.DependsOn() == nil` test with an exact `[SubsystemDatabase]`
   assertion, recompute the topo-order pin (EventBus is no longer an in-degree-0 seed), and KEEP the
   `EventBus → CryptoChainVerifier` prohibition — the Database edge does not create that cycle.

2. **BLOCKER (Codex HIGH — VERIFIED) — 07-09's "hoist whole/verbatim" cannot coexist with pre-StartAll
   subsystem registration.** The `:705-1060` block the plan says is "hoisted whole into a memoized
   builder" / "the block's body moves verbatim" itself CONSTRUCTS three lifecycle subsystems:
   `cryptoPolicySub` (`core.go:738`), `rekeyCheckpointSweepSub` (`:774`), `cryptoChainVerifierSub`
   (`:1053`). Every subsystem must exist and be registered before `StartAll` (`:1081`) — a lazy builder
   first invoked by a consumer's Start cannot also create the consumers. The rev-8 per-consumer provider
   signatures already imply the correct shape, but the plan contains no carve-out language and the
   verbatim instruction contradicts it. *Fix:* explicit split — subsystem constructions stay on
   runCore's straight-line path with provider-shaped configs; `cryptoWiring` holds only shared resolved
   values (repos, emitters, managers, handlers, coordinator inputs); enumerate the `cryptoWiring` struct
   fields and which pre-StartAll constructor consumes each provider; retract "whole/verbatim" in favor
   of "the block's RESOLUTION LOGIC moves; its subsystem constructions stay".

3. **BLOCKER (Codex HIGH — VERIFIED) — 07-11 row 10 keys audit's Prepare guard on Activate-owned
   state.** The only live guard is `if s.worker != nil` (`internal/eventbus/audit/subsystem.go:~270`),
   and `worker` is assigned only after `p.start(workerCtx)` (`:313`) — which row 10 moves to Activate
   while instructing "KEEP existing (`:271`) on Prepare". After the split, Prepare never sets `worker`:
   Prepare is unguarded (re-runs Backfill/EnsurePartitions — re-run-safe per the table, but the verdict
   is still wrong as written), no specified fields carry the prepared projection/partition manager into
   Activate, and Stop cannot distinguish prepared-only from never-prepared. *Fix:* explicit phase-state
   design in the row — e.g. `preparedProjection`/`partitionManager` fields set by Prepare (Prepare
   guards on them), `worker`/cancel set by Activate (Activate guards on running state), Stop handles and
   clears both states.

### MEDIUMs (all orchestrator-verified; fix in rev 10)

- **07-07 — zero-actor IDs change observably.** `busEventToCoreEvent` deliberately maps a zero ULID to
  `""` (`cmd/holomush/sub_grpc.go:976-979`) and plugins see that string. The plan's criterion requires
  `Actor.ID.String()` (`07-07-PLAN.md:232`) — applied naively, a zero actor becomes
  `"00000000000000000000000000"`. This is the repo's own known "zero-ULID guard is load-bearing" class.
  Mandate a zero-aware helper + tests for zero and nonzero actors.
- **07-07 — the wrapped publisher must be named.** CoreServer's replacement publisher must be the one
  returned by `wrapPublisher` (`cmd/holomush/sub_grpc.go:264`), never `rawPublisher` — audit persistence
  fails closed without the `App-Rendering` header (`internal/eventbus/audit/projection.go:382`). The
  plan never mentions wrapPublisher (verified by grep). Add the explicit sentence + an integration
  assertion that a command-response event lands in `events_audit`.
- **07-11 — the two-reaper focused test lacks a guest-reaper timing seam.** `grpcSubsystemConfig`
  exposes only the session `ReaperInterval` (`sub_grpc.go:81`); the guest reaper hardcodes
  `Interval: 1 * time.Minute`. The criterion's "shortened interval" test (`07-11-PLAN.md:1152`) cannot
  be deterministic for the guest reaper as written. Specify a config field or injected factory seam.
- **07-11 — the "both phases" criterion re-introduces the retracted arg-shape whitelist.** `:1145`
  compares file sets from `rg -n '\.Prepare\((ctx|context\.|startCtx)'` — the exact shape-class the
  same plan retracts at `:42` for missing `s.Start(t.Context())` (live at
  `internal/eventbus/audit/subsystem_test.go:130`). Use broad `\.Prepare\(`/`\.Activate\(` sweeps with
  receiver-type classification, like the Step-0 census.

### What held

All four round-6 blocker fixes survived scrutiny — OpenCode's deep pass independently endorsed each
(scoped 07-06 proof, direct actor literal, StartAll-over-stubs, cluster resolution-in-Prepare), and
Codex found no fault with any of them. Codex rates 07-01 through 07-06 **Ready** and 07-08 **Ready after
07-07**; ARCH-05 (01-04) is LOW-MEDIUM risk in both reviews. The unresolved surface has narrowed to the
ARCH-03 tail plus 07-07's two behavior-preservation gaps.

### Divergence

Unchanged pattern: OpenCode measures coherence and grounding (all its checks pass — it is not wrong);
Codex measures whether the instructions compose and execute exactly as written. Six rounds running,
only the second lens produces amendments, and this round it took the composition lens all the way into
config-loading semantics that pre-date the phase.

### Recommendation

**Rev 10: three blocker amendments + four MEDIUMs.** The gameID/EventBus fix (blocker 1) is the only
one with cross-plan reach (07-09 census + truth, 07-10 nil-pin/topo-pin recompute); the other two are
single-plan precision work. After rev 10, Codex's own assessment — MEDIUM execution risk — plus six
rounds of held fixes argues for **executing** rather than a round 8: the remaining findings have
descended below the plan/design layer into implementation guidance that execution-time gates
(`task test:int`, KEK-wired boots, crypto-reviewer, the RED proofs) are the right instruments for. If a
round 8 runs at all, scope it to 07-07/07-09/07-10/07-11 diffs only.
