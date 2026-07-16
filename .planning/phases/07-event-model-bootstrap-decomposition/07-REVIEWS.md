---
phase: 7
round: 5
reviewers: [codex, opencode]
reviewed_at: 2026-07-16T13:04:08Z
plans_reviewed: [07-01-PLAN.md, 07-02-PLAN.md, 07-03-PLAN.md, 07-04-PLAN.md, 07-05-PLAN.md, 07-06-PLAN.md, 07-07-PLAN.md, 07-08-PLAN.md, 07-09-PLAN.md, 07-10-PLAN.md, 07-11-PLAN.md]
plans_revision: "rev 7 (f9ad8be40)"
notes: "Round 4 preserved at e5807eb30. Reviewers were given NO prior-round feedback (user decision: judge rev 7 on merits). opencode model: openrouter/x-ai/grok-4.5; codex: default model, codex-cli 0.144.4."
---

# Cross-AI Plan Review — Phase 7 (Round 5, rev 7)

## Codex Review

# Executive assessment

The phase is architecturally coherent and unusually well-grounded, but it is not execution-ready yet. I found four blocking areas:

- 07-01 omits an integration-tagged gRPC-client caller, so `task test:int` will not compile after the move.
- 07-09’s central `cryptoWiring` provider contract is not package-realizable as currently specified and leaves eager verifier construction unresolved.
- 07-11 omits two gRPC reaper loops from the Prepare/Activate disposition.
- 07-11 also leaves the disabled admin-socket path unsafe and does not actually settle phase-specific idempotency for all 17 subsystems.

Overall phase risk: **HIGH until those plans are amended**. The event-model sequence and gateway-boundary work are otherwise strong.

## 07-01 — Extract `internal/grpcclient`

### Summary

The package extraction is the right first move and materially reduces the gateway closure. However, the caller census is incomplete, creating a guaranteed integration compilation failure.

### Strengths

- `client.go` is already a clean extraction candidate: it imports only external libraries and generated protobuf packages, with no `internal/...` dependencies ([internal/grpc/client.go:7](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/grpc/client.go:7)).
- The plan correctly includes `cmd/holomush/gateway.go`, which directly imports `internal/grpc` and constructs `ClientConfig`/`NewClient` ([cmd/holomush/gateway.go:20](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/gateway.go:20), [cmd/holomush/gateway.go:152](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/gateway.go:152)).
- Preserving the error-classification implementation verbatim is appropriate for the gateway trust boundary.

### Concerns

- **HIGH — omitted integration caller.** `test/integration/phase1_5_test.go` imports the combined `internal/grpc` package and uses both server and client symbols. It calls `grpcpkg.NewClient` with `grpcpkg.ClientConfig` at three sites ([test/integration/phase1_5_test.go:27](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/test/integration/phase1_5_test.go:27), [test/integration/phase1_5_test.go:297](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/test/integration/phase1_5_test.go:297), [test/integration/phase1_5_test.go:374](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/test/integration/phase1_5_test.go:374), [test/integration/phase1_5_test.go:529](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/test/integration/phase1_5_test.go:529)). The file is integration-tagged ([test/integration/phase1_5_test.go:4](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/test/integration/phase1_5_test.go:4)) and absent from the plan manifest ([07-01-PLAN.md:7](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-01-PLAN.md:7)). Deleting `internal/grpc/client.go` therefore breaks `task test:int`.

### Suggestions

- Add `test/integration/phase1_5_test.go` to `files_modified`.
- Retain `grpcpkg` for `NewCoreServer`/`WithEventStore`, and add `grpcclient` for `NewClient`/`ClientConfig`.
- Re-run the census using symbol-specific patterns rather than aliases.

### Risk Assessment

**HIGH** until the missing caller is added; otherwise the design is low-risk mechanical relocation.

## 07-02 — Extract `internal/eventvocab`

### Summary

This plan cleanly separates gateway-safe wire vocabulary from the eventual `eventbus.Event` representation and properly sequences the ARCH-04/ARCH-05 collision.

### Strengths

- The current `core/event.go` genuinely mixes vocabulary, payload validation, payload structures, actors, and the duplicate Event model ([internal/core/event.go:14](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/event.go:14), [internal/core/event.go:35](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/event.go:35), [internal/core/event.go:63](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/event.go:63), [internal/core/event.go:232](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/event.go:232)).
- Telnet directly consumes arrive/leave vocabulary for rendering, validating the need for a gateway-safe leaf ([internal/telnet/gateway_handler.go:1246](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/telnet/gateway_handler.go:1246)).
- Pinning every string and JSON shape before relocating it is an appropriate behavior-preservation gate.

### Concerns

- **LOW — census volatility.** This is a wide symbol move across production, unit, and integration code. The plan acknowledges this and provides a reproducible census, so I found no specific missing consumer in the current tree.

### Suggestions

- Re-run the declared census immediately before editing and compare its exact file set with the manifest.
- Keep the leaf free of all internal imports; external `oops` usage for validation is reasonable.

### Risk Assessment

**MEDIUM** because of the mechanical blast radius, not because of an architectural flaw.

## 07-03 — Gateway value leaves

### Summary

The three leaves are appropriately scoped and preserve the critical distinction between event/session ULIDs and entity IDs. One moved test will retain a false historical rationale unless amended.

### Strengths

- The ULID generator has genuinely coupled entropy/lock/timestamp state, so moving the implementation once and retaining a forwarding shim avoids duplicated generators ([internal/core/ulid.go:16](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/ulid.go:16)).
- The gateway’s uses are exactly leaf-shaped: command parsing ([internal/telnet/gateway_handler.go:406](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/telnet/gateway_handler.go:406)), connection ID generation ([internal/telnet/gateway_handler.go:670](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/telnet/gateway_handler.go:670)), and the lease cadence ([internal/session/reaper.go:14](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/session/reaper.go:14)).
- Keeping `core.NewULID` as a temporary forwarder correctly avoids an unnecessary phase-wide rename.

### Concerns

- **LOW — stale test rationale.** The plan moves `ulid_test.go` verbatim, but its monotonicity test says `PostgresEventStore.Replay` orders by event ULID and silently skips lexicographically inverted events ([internal/core/ulid_test.go:36](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/ulid_test.go:36)). That contradicts the settled JetStream-sequence ordering model. The production comment is scheduled for correction, but the moved test comment is not.

### Suggestions

- Update the moved test commentary to assert only the still-live monotonicity consumers. Remove the retired Postgres event-replay rationale.
- Preserve the actual strict-monotonicity test unless a separate decision retires that guarantee.

### Risk Assessment

**MEDIUM**, mainly due to identity-generation sensitivity.

## 07-04 — Gateway enforcement and invariant binding

### Summary

The direct and transitive gates together genuinely enforce ARCH-05, and the invariant correction is well designed. The implementation should avoid introducing two independent forbidden-package lists.

### Strengths

- The existing gate is demonstrably direct-import-only: it iterates `file.Imports` and compares literal paths ([cmd/holomush/gateway_imports_test.go:140](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/gateway_imports_test.go:140)).
- The current list contains the phantom `internal/auth/service` package ([cmd/holomush/gateway_imports_test.go:101](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/gateway_imports_test.go:101)).
- The registry is genuinely stale: the summary and `refs` token both name the obsolete paths/tokens, and the binding remains pending ([docs/architecture/invariants.yaml:2340](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/docs/architecture/invariants.yaml:2340)).
- Retaining the GW regex fixtures is correct; the migration is explicitly documented in the meta-test ([internal/gateway_invariants/meta_test.go:16](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/gateway_invariants/meta_test.go:16)).

### Concerns

- **MEDIUM — duplicated policy data.** The plan adds a new `forbiddenClosure` variable while retaining the existing `forbidden` slice. Because both tests are `package main`, they can share one canonical list. Two lists recreate the synchronization problem the plan otherwise tries to eliminate.

### Suggestions

- Rename the existing slice to a neutral `gatewayForbiddenPackages` and use it from both direct and transitive tests.
- Keep both tests in `asserted_by`; they prove different halves of the invariant.
- Retain the positive control, with the documented replace-don’t-delete instruction.

### Risk Assessment

**MEDIUM**. The enforcement design is strong once the package list is made single-source.

## 07-05 — Move `core.Engine` to `presence.Emitter`

### Summary

This plan correctly preserves the non-obvious behavior in `Engine` and resolves the auth import cycle through a consumer-owned interface.

### Strengths

- It explicitly carries the typed-nil fail-fast guard, which is real behavior rather than scaffolding ([internal/core/engine.go:37](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/engine.go:37)).
- It preserves `EndSession`’s audit-critical background timeout and cause-dependent actor selection ([internal/core/engine_end_session.go:20](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/engine_end_session.go:20), [internal/core/engine_end_session.go:56](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/engine_end_session.go:56), [internal/core/engine_end_session.go:68](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/core/engine_end_session.go:68)).
- The two-method auth interface matches actual auth usage: eviction calls disconnect and session-ended emission, but not arrival ([internal/auth/auth_service.go:225](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/auth/auth_service.go:225)).
- Reproducing qualification before publishing is essential because the old adapter currently owns that translation.

### Concerns

- No source-backed blocker found.

### Suggestions

- Keep explicit tests for an already-cancelled caller context and the non-quit system actor; these are the easiest behaviors to lose during the move.
- Ensure the auth interface uses the final renamed methods rather than retaining compatibility names.

### Risk Assessment

**MEDIUM**. Sensitive cross-package move, but the plan covers its critical behavior.

## 07-06 — Unified system broadcast builder

### Summary

The intent-shaped broadcaster removes a genuine duplicate payload contract while keeping `internal/command` independent of `eventbus`.

### Strengths

- The current hostcap implementation explicitly admits that it mirrors the command-layer JSON shape ([internal/plugin/hostcap/system_broadcaster.go:45](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/hostcap/system_broadcaster.go:45)).
- `SessionAdmin` is already an appropriate consumer-owned interface ([internal/plugin/hostcap/capabilities.go:54](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/hostcap/capabilities.go:54)).
- The plan correctly pins qualification and requires an exact subject assertion rather than recomputing the expected value through the same function.

### Concerns

- **LOW — retained interface method is implicit.** The task describes the new broadcast delegation but does not explicitly say to retain `DisconnectSession`, which is needed to continue satisfying `SessionAdmin` and currently returns the deliberate `ErrDisconnectUnsupported` result ([internal/plugin/hostcap/system_broadcaster.go:64](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/hostcap/system_broadcaster.go:64)). The compiler will catch deletion, but the plan should not rely on that.

### Suggestions

- Add one sentence: “Retain `DisconnectSession` unchanged.”
- Keep its existing unsupported-behavior test alongside the broadcast delegation tests.

### Risk Assessment

**MEDIUM** because it crosses command and plugin-runtime surfaces, though the core design is sound.

## 07-07 — Collapse to `eventbus.Event`

### Summary

This is a strong and complete collapse plan. It identifies the previously missing CoreServer publication replacement and preserves the package-private audit seam that constrains the survivor type.

### Strengths

- `eventbus.Event` carries host-internal fields that cannot safely move to a neutral package, especially the unexported `auditRow` used by crypto downgrade checks ([internal/eventbus/types.go:136](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/types.go:136), [internal/eventbus/types.go:195](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/types.go:195)).
- The current history bridge destroys `Seq` by converting to `core.Event` ([cmd/holomush/sub_grpc.go:959](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/sub_grpc.go:959)).
- The plan correctly supplies a real replacement for CoreServer’s only publication seam; today it stores `core.EventAppender` and publishes command responses through it ([internal/grpc/server.go:151](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/grpc/server.go:151), [internal/grpc/server.go:616](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/grpc/server.go:616)).
- Retaining the actor bridge with non-ULID rejection preserves an important spoofing control ([internal/plugin/event_emitter.go:299](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/event_emitter.go:299)).

### Concerns

- No source-backed blocker found. Execution remains broad and security-sensitive.

### Suggestions

- Keep the exact wire-subject assertion and nil-publisher test as mandatory.
- Treat `task test:int` and the crypto reviewer as hard gates, not end-of-phase cleanup.

### Risk Assessment

**HIGH execution risk**, but the plan itself is well specified.

## 07-08 — Sequence-correct plugin history

### Summary

The identified bug is real, and the proposed sequence threading fixes the correct layer without exposing sequence values to plugins. Binary-host coverage should be made as explicit as Lua coverage.

### Strengths

- `HostCursor` already carries both `Seq` and `ID` ([internal/eventbus/cursor/cursor.go:53](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/cursor/cursor.go:53)).
- The current adapter passes `BeforeID` but never `BeforeSeq` ([cmd/holomush/sub_grpc.go:913](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/sub_grpc.go:913)).
- Hostcap hardcodes `Seq: 0` ([internal/plugin/hostcap/servers.go:1279](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/hostcap/servers.go:1279)), while the Lua path independently does so at both per-event and next-page sites ([internal/plugin/hostfunc/stdlib_focus.go:436](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/hostfunc/stdlib_focus.go:436), [internal/plugin/hostfunc/stdlib_focus.go:452](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/hostfunc/stdlib_focus.go:452)).
- The public proto exposes only an opaque cursor and no sequence field ([api/proto/holomush/plugin/host/v1/stream.proto:43](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/api/proto/holomush/plugin/host/v1/stream.proto:43)).

### Concerns

- **MEDIUM — hostcap round-trip coverage is less explicit than Lua coverage.** Task 1 exercises `busHistoryReaderAdapter` directly, while Task 2 explicitly requires a multipage Lua walk. The hostcap encode/decode layer at [servers.go:871](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/hostcap/servers.go:871) and [servers.go:900](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/hostcap/servers.go:900) could still regress independently.

### Suggestions

- Add a hostcap `QueryStreamHistory` two-page test that feeds response `NextCursor` into the second request.
- Assert the fake reader receives the oldest event’s real `(beforeSeq, beforeID)` pair.
- Retain the existing Lua multipage test for runtime symmetry.

### Risk Assessment

**MEDIUM** after adding binary-host round-trip coverage.

## 07-09 — Bootstrap Wave A

### Summary

The plan accurately identifies all five eager-start causes and the dependency-cycle hazards. Its central wiring abstraction is still insufficiently specified to implement safely across package boundaries.

### Strengths

- The current bootstrap demonstrably starts DB and EventBus outside the orchestrator ([cmd/holomush/core.go:281](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/core.go:281), [cmd/holomush/core.go:450](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/core.go:450)).
- Accessor calls really do require live subsystems, and the current comments admit the pre-start workaround.
- The plan correctly identifies that verifier handler construction eventually depends on the bus and therefore forbids the reverse EventBus→Verifier edge.
- Moving TLS into a real DB-dependent subsystem directly addresses the first eager-start cause.

### Concerns

- **HIGH — `cryptoWiring` cannot be named by internal package configs as written.** The plan defines `cryptoWiring` in `package main` and says subsystem configs should take `func() (*cryptoWiring, error)` “or a narrow accessor” ([07-09-PLAN.md:190](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-09-PLAN.md:190)). Packages such as `internal/admin/policy` cannot import or name a `cmd/holomush` type. The fallback accessor design is load-bearing but not settled.
- **HIGH — verifier construction remains eager unless explicitly redesigned.** `NewVerifierSubsystem` immediately constructs `NewVerifier(cfg.Repo)` ([internal/eventbus/audit/chain/verifier_subsystem.go:50](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/audit/chain/verifier_subsystem.go:50)). Adding only `HandlersProvider` does not defer the repo dependency; a repo produced by the lazy builder is still unavailable at construction.
- **MEDIUM — failure semantics need per-consumer definition.** Policy, checkpoint sweep, verifier, admin socket, and gRPC currently hold different concrete dependency shapes ([internal/admin/policy/subsystem.go:23](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/admin/policy/subsystem.go:23), [internal/eventbus/crypto/dek/sweep.go:28](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/crypto/dek/sweep.go:28), [cmd/holomush/sub_grpc.go:64](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/sub_grpc.go:64)). “Narrow accessor” does not state the concrete API or when each consumer caches its result.

### Suggestions

Define the actual consumer-owned provider types in the plan, for example:

- verifier: provider returning repo plus handlers; construct `Verifier` in Start/Prepare;
- checkpoint sweep: provider returning repo and emitter;
- crypto policy: provider returning `policy.EmitDeps`;
- admin socket: provider returning concrete handlers;
- gRPC: provider returning its concrete crypto inputs.

The closures can close over `*cryptoWiring` inside `package main`, but their public signatures must use types owned by the consuming internal package.

### Risk Assessment

**HIGH** and currently blocking.

## 07-10 — Lifecycle ordering and bounded shutdown

### Summary

The shutdown and ordering corrections address real operational defects, but the topology-test seam and abandoned-stop behavior need tighter definition.

### Strengths

- `StopAll` currently calls each `Stop` synchronously and never checks context cancellation ([internal/lifecycle/orchestrator.go:80](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/lifecycle/orchestrator.go:80)).
- Production currently defers it with an unbounded background context ([cmd/holomush/core.go:1101](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/core.go:1101)).
- The plan correctly uses a deferred closure so the timeout starts at shutdown rather than at defer registration.
- The false ordering comment is real: the slice claims verifier-before-EventBus although topo order is derived independently ([cmd/holomush/core.go:1459](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/core.go:1459)).
- gRPC genuinely lacks the AuditProjection dependency ([cmd/holomush/sub_grpc.go:166](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/sub_grpc.go:166)).

### Concerns

- **MEDIUM — the topology test’s access mechanism is unclear.** `topoSort` is unexported ([internal/lifecycle/orchestrator.go:98](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/lifecycle/orchestrator.go:98)), while the planned tests live in `cmd/holomush`, a different package. The tests can infer order through `StartAll`, but the plan repeatedly says they directly assert `topoSort` results without specifying that mechanism.
- **MEDIUM — abandoned Stop goroutines can overlap later StopAll calls.** The plan deliberately returns while a context-ignoring `Stop` goroutine continues. The interface promises idempotency and boundedness, but not concurrent-call safety ([internal/lifecycle/subsystem.go:55](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/lifecycle/subsystem.go:55)). A subsequent rollback or test call could invoke `Stop` again while the first is still running.

### Suggestions

- Specify that topology tests call `StartAll` on no-op recording wrappers whose dependencies are read from live subsystem instances; do not imply direct access to `topoSort`.
- Alternatively add a narrow read-only validation/order method to `internal/lifecycle`.
- Snapshot and clear `startOrder` before launching stop goroutines, or track per-subsystem stopping state, so a second `StopAll` cannot re-enter an abandoned Stop.

### Risk Assessment

**HIGH** because it changes shutdown and boot graph behavior, though its main mechanisms are sound.

## 07-11 — Prepare/Activate Wave B

### Summary

The two-sweep design and rollback semantics are conceptually strong, but this plan still has multiple execution blockers. The settled 17-row table is incomplete for live gRPC work, unsafe for disabled admin sockets, and does not actually settle phase-specific idempotency.

### Strengths

- The corrected eventbus/audit relationship is source-accurate: embedded NATS starts as part of connection acquisition with `DontListen: true` ([internal/eventbus/subsystem.go:149](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/subsystem.go:149)), while audit fails closed if JetStream is not already live ([internal/eventbus/audit/subsystem.go:267](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/audit/subsystem.go:267)).
- Separating audit’s synchronous partition boot gate from projection/consumer loops follows the existing code’s own seam ([internal/eventbus/audit/subsystem.go:286](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/audit/subsystem.go:286), [internal/eventbus/audit/subsystem.go:313](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/audit/subsystem.go:313)).
- Rolling back every prepared subsystem, including the one whose Prepare partially failed, is the correct resource-safety model.
- The broad direct-caller census and mandatory integration compilation gate are appropriate for this interface migration.

### Concerns

- **HIGH — two gRPC domain loops are absent from the settled table.** Current `grpcSubsystem.Start` launches both the session reaper and guest reaper before binding the listener ([cmd/holomush/sub_grpc.go:758](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/sub_grpc.go:758), [cmd/holomush/sub_grpc.go:769](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/sub_grpc.go:769)). Row 16 assigns only listener/Serve and coordinator start to Activate. Following the table literally either drops the reapers or leaves domain loops in Prepare, violating D-13.0.
- **HIGH — disabled admin socket becomes unsafe.** Today an empty `SocketPath` returns before creating a server ([internal/admin/socket/subsystem.go:73](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/admin/socket/subsystem.go:73)). Under two sweeps, returning from Prepare does not skip Activate. The plan then tells Activate to call `srv.Start()` without specifying a disabled-state guard. The existing test only exercises the single-phase behavior ([internal/admin/socket/subsystem_test.go:110](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/admin/socket/subsystem_test.go:110)).
- **HIGH — D-13.2 is not actually settled per subsystem.** The plan declares both phases MUST be idempotent, but delegates the 17 verdicts to the executor/SUMMARY. Several live methods have clearly non-idempotent effects:
  - policy publishes on every invocation ([internal/admin/policy/subsystem.go:47](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/admin/policy/subsystem.go:47));
  - outbox relay creates another cancellation context and goroutine every invocation ([internal/world/setup/relay_subsystem.go:99](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/world/setup/relay_subsystem.go:99));
  - checkpoint sweep starts another loop every invocation ([internal/eventbus/crypto/dek/sweep.go:86](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/crypto/dek/sweep.go:86));
  - admin socket binds/serves on every Start ([internal/admin/socket/subsystem.go:89](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/admin/socket/subsystem.go:89)).
- **LOW — SessionSubsystem row remains conditional rather than settled.** Its current Start only constructs a store and launches no reaper ([internal/session/setup/subsystem.go:51](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/session/setup/subsystem.go:51)). Row 7 should state Activate is a no-op, not “if its Start launches one.”

### Suggestions

- Amend row 16:
  - Prepare constructs both reapers and stores their context/cancel state.
  - Activate starts both `Run` goroutines, then coordinator, listener, and Serve.
  - Stop cancels/drains all owned activity.
  - Add a test proving neither reaper runs after Prepare alone.
- Add explicit disabled state to AdminSocketSubsystem, or make Activate return nil when `s.server == nil`/`SocketPath == ""`. Migrate the empty-path test to call both phases.
- Put the full 17-row idempotency verdict in the plan, not the execution summary. Specify phase-owned state flags and repeated-call tests for every non-idempotent side effect.
- Change SessionSubsystem Activate to an explicit documented no-op.

### Risk Assessment

**HIGH** and currently blocking.

# Final recommendation

Revise 07-01, 07-09, and 07-11 before execution. Clarify 07-10’s topology-test seam and shutdown re-entry behavior, and strengthen 07-08’s binary-host pagination coverage. After those changes, the phase should achieve all three goals: one Event model, orchestrator-owned bootstrap, and enforced gateway isolation.

---

## OpenCode Review (grok-4.5)

# Phase 7 Cross-AI Plan Review

**Verdict: READY with execution risk concentrated in Waves 7–9**  
Overall risk: **MEDIUM** (design risk LOW–MEDIUM; implementation blast radius HIGH)

I verified the load-bearing mechanisms against live source in this worktree. Claims about gateway drift, seq cursor bug mechanics, engine→auth cycle, DontListen/atomic admin socket, productionSubsystems count (16), and the phantom `internal/auth/service` entry all check out.

---

## 1. Summary

These plans are unusually strong for a phase this size: 11 plans, 9 waves, three requirements (ARCH-03/04/05), with decisions already hardened through multi-round review. ARCH-05 is sequenced correctly (extract client leaf → vocab leaf → value leaves → permanent gates). ARCH-04 avoids deepening MEDIUM-4 by keeping `command` off `eventbus`, and correctly treats D-07 as a real correctness fix with a deterministic quiet-stream RED. ARCH-03 (07-09→11) is the hard part: the `cryptoWiring` hoist, gameID dual-path migration, MEDIUM-11 edge/cycle analysis, and Prepare/Activate barrier are *settled designs*, not implementer-invented. Residual risk is mostly “can one executor land ~50-file rewrites without partial-fix modes,” not “is the architecture wrong.” Phase goals are achievable if wave gates and `task test:int` (with KEK) are treated as non-negotiable.

---

## 2. Strengths

- **Grounded drift corrections, not folklore**
  - `cmd/holomush/gateway.go:20,152-153` imports `internal/grpc` and is **not** in `coreOnlyFiles` (`gateway_imports_test.go:22-99`) — so 07-01’s extract-before-forbid is mandatory, not optional.
  - `forbidden` still lists nonexistent `internal/auth/service` (`gateway_imports_test.go:107`); `INV-EVENTBUS-1` summary/`refs` still say `auth/service` and token `INV-GW-1` (`invariants.yaml:2344-2348`) — 07-04 Task 2–3 owns the real fix.
  - D-04/FINDING-1 cycle is real: `auth` holds `*core.Engine` (`auth_service.go:engine field, 39,115,235,243`); `go list -deps ./internal/eventbus` contains `internal/auth`.

- **Correct collapse direction & constraints**
  - `eventbus.Event` stays put (auditRow/crypto fence rationale matches D-01).
  - `command` must not import `eventbus` (MEDIUM-4) — D-02/`sysbroadcast` is the right pattern; already used in-tree (`hostcap.SessionAdmin`).

- **D-07 bug chain verified hop-by-hop**
  - `HistoryQuery` treats `BeforeID` as tripwire (`bus.go:88-105`); `matchesQuery` has no `BeforeID` branch (`hot_jetstream.go:392-402`); `BeforeSeq==0` → tail read (`:348-350`).
  - Both runtimes hardcode `Seq:0`: hostcap `encodeHostEventCursor` (`servers.go:1285-1290`) **and** Lua sites (`stdlib_focus.go:441` and `:463`).
  - `next_cursor` correctly uses **oldest / index 0** (`servers.go:900-906`, `stdlib_focus.go:452-463`). Plans correctly retract the false “last event” and “ID-only fallback” framings.

- **Wave DAG is worktree-safe**
  - Parallel pairs (07-03∥07-05, 07-04∥07-06) have empty `files_modified` intersections (checked mechanically).
  - 07-07 depends on **both** 07-04 and 07-06 so ARCH-05 gates exist before `core.Event` deletion.

- **ARCH-03 design depth matches the risk**
  - Five eager starts at `core.go:287,462,797,800,977`; unbounded `StopAll` at `:1105`; false “runs before EventBus” at `:1476`; 16 positional params in `productionSubsystems` (`:1462-1472`); `SubsystemTLS` already at `subsystem.go:18`.
  - MEDIUM-11 reverse edge correctly **retracted**: verifier handler set is built from `eventBusSub.Publisher()` path; EventBus→Verifier would cycle after 07-09’s consumer rule **and** risk unregistering `operator_read`.
  - Wave B barrier scoped by DontListen (`eventbus/subsystem.go:153`) + audit’s hard require on live JS — earlier “universal barrier” would not boot.

- **Enforcement rasters match the claims**
  - Direct AST gate is insufficient alone; 07-04’s transitive closure + positive control on `internal/grpc→store` is what makes INV-EVENTBUS-1’s binding genuine.
  - D-18 / no INV-GW rename + keep INV-GW regex fixtures is correct (`gateway_invariants/meta_test.go` migration story).

---

## 3. Concerns

### HIGH

- **None as design blockers.** No plan today would reintroduce a known compile-cycle or reverse a finished migration if executed as written.

### MEDIUM

1. **07-01 under-declares a real client consumer**  
   `test/integration/phase1_5_test.go:297,374,529` calls `grpcpkg.NewClient` / `ClientConfig` while also using `NewCoreServer`. That file is **not** in 07-01 `files_modified` (it appears later for Engine/`WithEventStore`).  
   Impact: `task test:int` will break until dual-import rewire. Acceptance runs `test:int`, so it is *caught*, but the plan’s “authoritative consumer list” is incomplete and invites a late scramble.

2. **Execution complexity of 07-09 + 07-11 dominates phase risk**  
   Verified complexity deserves this weight:
   - ~26 live-value reads on `core.go`’s pre-start path (`rg` count was 26).
   - `cryptoWiring` memoized hoist of the `:705–1060` block is necessary but easy to half-apply (drop a consumer from THE RULE, route ABAC through wiring again, turn Coordinator start failures fatal).
   - 07-11 migrates ~17 impls + ~27 callers, most integration-tagged → only `task test:int` sees them. A partial migrate (Prepare without Activate) compiles and goes green while loops never run. Plans name this; discipline still required.

3. **“No behavior change” is slightly oversold for ARCH-03**  
   Phase success criterion #2 says startup/shutdown behavior is unchanged. 07-09/10 **intentionally** change topo order (AuditProjection before gRPC; Verifier before gRPC/admin.sock; Cluster/Legacy TLS edges). That is correct bug-fix behavior, not pure rename. Risk is review/comms: executors omit a new edge to “preserve order” and re-open the accept-before-verify window.

4. **07-05 interim dual path is subtle**  
   `presence.Emitter` publishes via `eventbus.Publisher`, while `busEventAppender` (`sub_grpc.go:821+`) remains until 07-07 for other appenders. Plan is explicit, but subject construction must match `busEventAppender` (`Qualify` + `""→main` + `NewType` + `NewEvent`) or arrive/leave subjects silently diverge prior to collapse. Criteria pin exact-literal subjects — good; easy to violate under schedule pressure.

5. **StopAll abandon-in-goroutine residual (07-10)**  
   Design is correct for process exit + one boot rollback. Doc comment requirement is right. Residual: a misbehaving subsystem `Stop` after repeated orchestrator use in long-lived test harnesses can still leak goroutines; accepted, not new.

### LOW

6. **07-02/serialization on `gateway_handler.go`**  
   depends_on 07-01 purely for worktree collision is correct process design, not type need. Mild wall-clock cost; fine.

7. **07-04 positive control coupling to `internal/grpc→store`**  
   Settled as live control with replace-dont-delete; residual is future false red if grpc becomes a leaf — acceptable and loud.

8. **Leaf forwarders (`core.NewULID`, `session.DefaultLeaseRefreshInterval`)**  
   Drift risk long-term if someone “simplifies” by deleting forwarders mid-call-site churn; 07-03/07 documents why they stay.

9. **Forbidden list growth vs. `coreOnlyFiles`**  
   Plan correctly rejects growing `coreOnlyFiles`. Residual: other CLI/e2e files not in the allowlist must remain free of `core/session/grpc` or the shared gate fails — research claimed zero; re-check at 07-04 wall-clock.

---

## 4. Suggestions

1. **Pre-execution: add to 07-01 Task 2 files / acceptance census**  
   ```text
   test/integration/phase1_5_test.go   # NewClient + ClientConfig + CoreServer dual import
   ```  
   Optional: re-run `rg -n 'holomush/internal/grpc\.(NewClient|ClientConfig|Client\b|TranslateSubscribeErr)'` and treat any hit outside host/server packages as mandatory rewire.

2. **07-09 boat-anchor: one integration checkpoint before Task 3**  
   After Task 2 (eager starts dead + cryptoWiring), require green:
   - `task test:int -- -run 'TestAdminAuthenticate|AdminRekey' ./cmd/holomush/`
   - the THE RULE DependsOn-superset test  
   before renaming `productionSubsystems`. Prevents stacking LOW-8 refactors on a boot that already panics.

3. **Explicit “topo order may change” note on ARCH-03 success criteria**  
   Pin: “observable player/API behavior unchanged; boot *order* may change to match real DependsOn (MEDIUM-11, LOW-7, AuditProjection edge).” Reduces “don’t change DependsOn” regressions.

4. **07-08 Spec A must round-trip the same dusk path hostcap uses**  
   Cursor from **index 0**; pageSize ≪ N; iteration-bounded. Plan says this — make SUMMARY require observed page1 IDs ∩ page2 IDs empty as the human-readable anchor proof.

5. **Do not split 07-11 further into “impl plan / caller plan”**  
   Compile atomicity argument is correct. If context limits force a split, only acceptable intermediate is a **temporary dual-method backward-compat shim**, which D-11 rejects — i.e. better keep one atomic wave.

6. **crypto-reviewer scheduling**  
   Plans flag 07-06/07-07 (`event_emitter`) and 07-08 (history path). Run domain gates **after each of those waves**, not only pre-merge, so regressions don’t compound into Wave 9.

7. **Deferred doc drift issue (CONTEXT.md)**  
   Still create the MODEL-02 completeness issue for `.planning/PROJECT.md` / ARCHITECTURE.md event-sourcing text during 07-07 Task 3 as planned — otherwise the next planning agent re-litigates MODEL-01.

---

## 5. Risk Assessment

| Axis | Level | Justification |
|------|-------|---------------|
| **Design correctness** | **LOW** | Settlements match live code; false rationales corrected advertising-style instead of silently shipping lies. |
| **Requirement coverage** | **LOW risk of gaps** | ARCH-05 → 01..04; ARCH-04 → 02,05..08; ARCH-03 → 09..11. Phase success criteria each have an automated pin (closure, wire-type greps, topo pin, two-sweep property test, KEK boot). |
| **Dependency ordering** | **LOW–MEDIUM** | Wave DAG sound; 07-07 convergence on ARCH-05 is critical and present. Parallelism only where files don’t collide. |
| **Silent integration breakage** | **MEDIUM** | Class of risk is named repeatedly; residual is human skip of `task test:int` / KEK-less false greens (plans already ban `task test` for those E2Es). |
| **Security regress** | **LOW–MEDIUM** | Crypto/identity surfaces handled deliberately (AuditRowOf, non-ULID bridge, no plugin Seq, verifier-before-bind, AuditProjection edge). Highest residual is mis-wired Activate error swallowing for Coordinator degraded mode. |
| **Over-engineering** | **LOW** | Large, but driven by proven cycles/bugs (pre-starts, MEDIUM-11 cycle, seq zero). Not greenfield inventiveness. |
| **Overall** | **MEDIUM** | Plans are execution-ready. Risk is recipe following + burnout size of 07-09/11, not bad strategy. |

---

## 6. Per-plan snapshot

| Plan | Wave | Goal hit? | Notes |
|------|------|-----------|-------|
| **07-01** | 1 | Yes (~85% ARCH-05) | client extract verified necessary; add `phase1_5_test.go` to consumer list |
| **07-02** | 2 | Yes | Vocab-only exit criteria correctly scoped; doesn’t falsely claim core ban |
| **07-03** | 3 | Yes | End-of-plan full gateway ban ownership is correct placement |
| **04** | 4 | Yes | Closure + genuine INV binding; phantom auth fix required |
| **05** | 3 | Yes | FINDING-1/5/6 settled; carry EndSession discipline for real |
| **06** | 4 | Yes | One payload builder; FINDING-5 parity with presence; Lua-only asymmetry preserved |
| **07** | 5 | Yes | Depends on 04+06; WithEventPublisher specified (not “build event without publish”); rules amended same change |
| **08** | 6 | Yes | Quiet multipage RED is correct; all three Lua encode/decode sites enumerated |
| **09** | 7 | Yes | Wave A design table is the plan; THE RULE + no-ABAC-consumer + TLS + 16→17 |
| **10** | 8 | Yes | MEDIUM-11 = delete false claim + pin (not reverse edge); rollback fresh ctx; acyclicity test vs rev4 cycle |
| **11** | 9 | Yes | D-13.0 barrier scoped correctly; Plugin Activate no-op; admin.sock atomic; caller census mandatory |

---

## 7. Do the plans achieve the phase goals?

| Success criterion | Achieved by |
|-------------------|-------------|
| Single Event representation; callers migrated; no *wire* behavior change | 07-07 (+05/06 prep, +02 vocab); `AppSchemaVersion=1` asserted; wire already `eventbus.Event` |
| Bootstrap via Orchestrator; unified start/stop; boot works | 07-09 (zero pre-starts) + 07-10 (honest edges/deadline) + 07-11 (Prepare/Activate structure) |
| Gateway protocol-translation-only; boundary green | 07-01→03 leaves + 07-04 direct+transitive gates + bound INV-EVENTBUS-1 |

**Bottom line:** Approve for execution. Treat 07-01 consumer census as a quick patch before Wave 1 starts; treat Waves 7–9 as a single high-attention sequence with mandatory KEK `task test:int` checkpoints, not a bulk autonomous grind.

---

## Consensus Summary

**Verdict: NOT execution-ready — rev 8 required. All blocking findings are narrow plan amendments, not design reversals.**

The reviewers split hard on the headline: OpenCode says **READY** (zero HIGH design blockers); Codex says
**HIGH risk until amended** (four blocking areas). Grounding checks resolve the split in Codex's favor on
three of its four areas — each was verified against the live tree and the rev-7 plan text by the
orchestrator before this summary was written. Per standing practice, consensus below is weighted by
grounding, not headcount.

### Blockers (orchestrator-verified against tree + plan text)

1. **BLOCKER (agreed: Codex HIGH, OpenCode MEDIUM — VERIFIED) — 07-01's caller census misses
   `test/integration/phase1_5_test.go`.** The file imports `internal/grpc` as `grpcpkg`
   (`test/integration/phase1_5_test.go:27`) and calls `grpcpkg.NewClient`/`ClientConfig` at `:297`,
   `:374`, `:529`; it is integration-tagged and absent from 07-01's `files_modified` (it appears only in
   07-05/07-07 for later, unrelated edits). Moving `client.go` breaks `task test:int` compile. Caught by
   07-01's own acceptance gate, but the plan's "authoritative consumer list" claim is false as written.
   *Fix:* add the file to `files_modified` + a dual-import rewire step (`grpcpkg` stays for
   `NewCoreServer`; new `grpcclient` import for the client symbols).

2. **BLOCKER (Codex HIGH — VERIFIED) — 07-11 row 16 omits two live gRPC domain loops.** Current
   `grpcSubsystem.Start` launches the session reaper (`go s.sessionReaper.Run(reaperCtx)`,
   `cmd/holomush/sub_grpc.go:758`) and guest reaper (`go s.guestReaper.Run(reaperCtx)`, `:769`) before
   the listener bind. Both are domain loops (lease sweeps call `engine.HandleDisconnect`; guest reaping
   deletes characters + emits tombstones). Row 16 (`07-11-PLAN.md:377`) assigns Prepare = build/register/
   wire/construct-Coordinator and Activate = bind+serve+`coordinator.Start()` — the reapers are assigned
   to neither sweep. By the plan's own stop-and-report rule an executor must halt here; the only other
   `reaper` mention is row 7 (SessionSubsystem). This is the round-4 census-class defect recurring at
   row granularity. *Fix:* amend row 16 — Prepare constructs both reapers + their ctx/cancel state;
   Activate starts both `Run` goroutines, then coordinator, listener, Serve; Stop cancels/drains; add a
   test that neither reaper runs after Prepare alone.

3. **BLOCKER (Codex HIGH — VERIFIED) — disabled admin socket nil-panics in Activate.** Row 14 settles
   Prepare = "the `SocketPath == \"\"` disabled-mode early return … and `srv := NewServer(Config{…})`"
   (`07-11-PLAN.md:428-431`) and Activate = "`errCh, err := srv.Start()` … unchanged". In disabled mode
   Prepare early-returns before `NewServer`, so `s.server` is nil — and the two-sweep orchestrator still
   calls Activate, which the plan directs to call `srv.Start()` with **no nil/disabled guard specified
   anywhere** (checked rows, settlement text, and acceptance criteria at `:1017`). Boot panic in exactly
   the degraded environment (no XDG runtime dir) the disabled mode exists to survive. *Fix:* one guard —
   Activate returns nil when `s.server == nil` — and migrate the empty-path test
   (`internal/admin/socket/subsystem_test.go:110`) to drive both phases.

4. **BLOCKER (Codex HIGH — VERIFIED, NARROWED) — 07-09's consumer provider contract is not
   package-realizable as primarily stated, and the verifier's `Repo` deferral is unspecified.** The
   settled design says each consumer config "takes `func() (*cryptoWiring, error)` (or a narrow accessor
   off it)" (`07-09-PLAN.md:206-208`) — but `cryptoWiring` is a `package main` type, unnameable from
   `internal/admin/policy`, `dek`, `chain`, or `socket` configs; only the gRPC subsystem (package main)
   can hold that type. The hedge is real but unsettled: of the five consumers, only the verifier's
   `Handlers` gets a concrete new provider type (`HandlersProvider func() []chain.Handler`, `:298`).
   Meanwhile `NewVerifierSubsystem` constructs `NewVerifier(cfg.Repo)` **at construction time**
   (`internal/eventbus/audit/chain/verifier_subsystem.go:52-56`) and the plan routes
   `chain.NewPostgresRepo` **inside the hoist** (`07-09-PLAN.md:288`) — post-07-09 there is no pool
   before `StartAll`, so a resolved `Repo` cannot exist when the subsystem is constructed. The plan
   converts `Handlers` but never says how `Repo` defers or that `NewVerifier` moves into Start.
   *Fix:* settle the per-consumer provider signatures in the plan (policy → `func() (policy.EmitDeps,
   error)`; sweep → repo+emitter provider; verifier → repo+handlers providers with `NewVerifier` moved
   to Start; admin socket → concrete handler provider; gRPC may close over `*cryptoWiring` directly).
   Closures live in `package main`; public signatures use consumer-owned types.

### Downgraded / refuted findings (checked, not blockers)

- **Codex HIGH "D-13.2 idempotency not settled per-subsystem" → MEDIUM.** The contract is stated
  per-interface with the corrected rationale (`07-11-PLAN.md:245,286-287`), and Codex's cited
  non-idempotent bodies are real — but the orchestrator calls each phase once per boot, so this is a
  robustness-contract completeness gap, not a boot-correctness defect. Still worth fixing in rev 8
  (per-row idempotency verdicts belong in the plan under this phase's zero-executor-decides standard),
  at WARNING severity.
- **Codex MEDIUM "07-10's topology-test access mechanism is unclear" → REFUTED.** The plan specifies
  dep-carrying stub subsystems driven through a real orchestrator (`07-10-PLAN.md:486`, `:649` — "a
  dep-carrying stub subsystem in `package main` (or an exported one from `internal/lifecycle`)").
  Codex missed the mechanism; no amendment required.
- **OpenCode's READY verdict** is overridden by the three verified Codex blockers above. Its review is
  genuinely grounded (drift corrections, D-07 chain, eager-start inventory all check out) but it did not
  descend to row-level disposition or provider-type realizability, which is where rev 7's remaining
  defects live.

### Agreed strengths (both reviewers, independently verified)

- The round-4 fixes all held: no reviewer re-found the ABAC↔cryptoWiring cycle, the gameID migration
  design, the census method, or the T-07-51 scoping — and both endorse the surface→verifier edges and
  the MEDIUM-11 comment-deletion + topo-pin settlement (`EventBus → Verifier` stays impossible).
- ARCH-04/05 (07-01…07-08) remain converged: the only blocker there is the 07-01 census file; everything
  else is LOW/suggestion-grade (07-03 stale test rationale, 07-04 single-source forbidden list, 07-06
  "retain `DisconnectSession`" sentence, 07-08 hostcap two-page round-trip test).
- Wave DAG is sound; parallel pairs file-disjoint; 07-07's convergence gating (depends on both 07-04
  and 07-06) is correct; D-07's bug chain is verified hop-by-hop by both reviewers.

### Divergence

Same complementarity pattern as rounds 2–4: near-zero HIGH overlap between grounded reviewers. OpenCode
audited breadth (all 11 plans, phase goals, DAG) and found the census gap; Codex audited depth (row-level
dispositions, type realizability, constructor timing) and found the other three. Neither is wrong about
what it checked; a single grounded reviewer would have missed half the picture again.

### Recommendation

**Rev 8, five amendments, no split, no design changes:** (1) 07-01 add `phase1_5_test.go` + rewire step;
(2) 07-09 settle per-consumer provider types + verifier `Repo`/`NewVerifier` deferral; (3) 07-11 row 16
reapers; (4) 07-11 row 14 disabled-mode Activate guard + two-phase test; (5) 07-11 per-row idempotency
verdicts (WARNING-grade). Optionally fold in the four LOW/suggestion items above while touching those
plans. The blockers are all specification precision, not architecture — the round-4 signal ("ARCH-03
scope") has resolved into ordinary plan-amendment work.
