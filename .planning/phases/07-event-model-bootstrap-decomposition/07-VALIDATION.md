---
phase: 7
slug: event-model-bootstrap-decomposition
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-15
---

# Phase 7 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
>
> Derived from `07-RESEARCH.md` § Validation Architecture. This is a **refactor**
> phase — the whole risk is silent behavior drift, so "no behavior change" is
> operationalized below as observable, testable properties rather than left as prose.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` + `testify` (unit); Ginkgo/Gomega (integration, `//go:build integration`); Playwright (E2E) |
| **Config file** | `Taskfile.yaml` (task defs); `.golangci.yaml` (lint, v2) |
| **Quick run command** | `task test -- ./internal/<pkg>/` |
| **Full suite command** | `task test` **then** `task test:int` |
| **Estimated runtime** | ~60s unit; integration several minutes (Docker required) |

> ⚠️ **`task test` does NOT compile `//go:build integration` files.** A cross-package
> type refactor can be unit-green and integration-red. `task test:int` is mandatory on
> every commit touching a shared type — not just at the end of the phase.

---

## Sampling Rate

- **After every task commit:** `task test -- ./<touched-pkg>/` **and** `task lint`
- **After every shared-type edit (every ARCH-04 commit):** `task test:int` — non-negotiable
- **After every plan wave (D-12):** `task test` + `task test:int` + `task build` green; each wave independently reviewable
- **Before `/gsd-verify-work`:** full suite green
- **Phase gate:** `task pr-prep` (fast lane) green before push; `Integration Test` + `E2E Test` are required CI checks
- **Max feedback latency:** ~60s (unit); integration is wave-scoped

---

## What "no behavior change" MEANS — observable properties

| Req | Observable property | How it is observed |
|-----|---------------------|--------------------|
| **ARCH-04** | Bytes on the wire and in `events_audit` are identical before/after. `eventbus.Event` is already the published type; `core.Event` is never serialized (`busEventToCoreEvent` is read-path only). `AppSchemaVersion` stays `1`. | Existing crypto/audit integration suites assert byte-equality (INV-21 lineage: `audit/projection.go` writes `msg.Data()`). `task test:int` |
| **ARCH-04** | Emit gates fire identically — actor-kind / `emits` / `crypto.emits` manifest gates at `event_emitter.go::Emit` behave the same for Lua **and** binary after the 3 actor bridges collapse to 1. | `test/integration/pluginparity/`, `test/integration/crypto/` (non-ULID rejection: `emit_test.go:113`, `e2e_test.go:208`, `metadata_only_test.go:237,337`) |
| **ARCH-04** | No import cycle (FINDING-1). | `task build` — the compiler is the oracle. Pre-check: `go list -deps ./internal/eventbus \| rg 'holomush/internal/auth'` |
| **ARCH-03** | Boot succeeds with a KEK configured; topological start order unchanged except where D-14 deliberately changes it; shutdown completes within deadline. | Topo-order pin test (new) + `task test:int` full-stack boot via `integrationtest.Start(t)` |
| **ARCH-03** | `StopAll` terminates. Today `defer orch.StopAll(context.Background())` (`core.go:1105`) has no deadline (LOW-7); after, it honors a 5s ctx. | New unit test: `StopAll` returns under deadline with a deliberately-hanging subsystem |
| **ARCH-05** | `internal/telnet`'s transitive internal closure shrinks 47 → ~6 and contains no `world`/`access`/`command`/`store`. Objective, non-behavioral success metric. | `go list -deps` assertion (new test) |
| **ARCH-05** | Telnet/web runtime behavior byte-identical — same error classification (`SESSION_NOT_FOUND` vs `RPC_FAILED`), same connection IDs, same arrive/leave rendering. | `internal/telnet/gateway_handler_test.go` (uses `core.EventTypeCommandResponse` at :376,:442 — needs the vocabulary-leaf rename); `task test:e2e` |

---

## Per-Task Verification Map

> Populated by `/gsd-plan-phase` → per-plan tasks, then updated during execution.
> Task IDs do not exist until plans are written; the requirement→test mapping below is
> the binding contract each task must satisfy.

| Req ID | Behavior | Test Type | Automated Command | File Exists | Status |
|--------|----------|-----------|-------------------|-------------|--------|
| ARCH-04 | Event wire bytes unchanged after collapse | integration | `task test:int` | ✅ `test/integration/crypto/`, `internal/eventbus/audit/` | ⬜ pending |
| ARCH-04 | Actor-bridge collapse preserves non-ULID rejection | integration | `task test:int` | ✅ `test/integration/crypto/emit_test.go:113` | ⬜ pending |
| ARCH-04 | Lua/binary emit parity preserved | integration | `task test:int` | ✅ `test/integration/pluginparity/` | ⬜ pending |
| ARCH-04 | Broadcast payload `{"message":...}` shape preserved from one builder | unit+integration | `task test -- ./internal/command/ ./internal/plugin/hostcap/` | ✅ `hostcap/system_broadcaster_test.go`, `plugin/setup/system_broadcaster_test.go`, `test/integration/pluginparity/session_admin_broadcast_test.go` | ⬜ pending |
| ARCH-04 | No import cycle (FINDING-1) | build | `task build` + `task test:int` | ✅ compiler is the oracle | ⬜ pending |
| **D-07** | **Plugin history multipage walk advances on a QUIET stream (page 2 ≠ page 1) — the RED gate** | integration | `task test:int` | ❌ **W0 — NEW** | ⬜ pending |
| **D-07** | **Plugin history pages neither skip nor repeat under concurrent publishers** (post-green defence-in-depth: proves seq, not ID, advances the cursor) | integration | `task test:int` | ❌ **W0 — NEW** | ⬜ pending |
| **D-07** | **Lua hostfunc cursor round-trip carries the real Seq** (`stdlib_focus.go:441` hardcodes `Seq: 0` independently of hostcap — runtime-symmetry surface) | unit | `task test -- ./internal/plugin/hostfunc/` | ❌ **NEW** | ⬜ pending |
| D-08 | `hostv1.Event` still has no seq field; cursor stays opaque | unit (meta) | `task test -- ./internal/plugin/hostcap/` | ❌ **NEW** (guard) | ⬜ pending |
| ARCH-03 | Boot succeeds with KEK wired; zero pre-starts | integration | `task test:int` | ⚠️ partial — must confirm KEK wired | ⬜ pending |
| ARCH-03 | Topological start order pinned | unit | `task test -- ./cmd/holomush/` | ❌ **W0 — NEW** (D-14 MEDIUM-11) | ⬜ pending |
| ARCH-03 | `StopAll` honors deadline (LOW-7) | unit | `task test -- ./internal/lifecycle/ ./cmd/holomush/` | ❌ **NEW** | ⬜ pending |
| ARCH-03 | Prepare/Activate rollback semantics (D-11/D-13) | unit | `task test -- ./internal/lifecycle/` | ❌ **Wave B — NEW** | ⬜ pending |
| ARCH-03 | `productionSubsystems` set/count | unit | `task test -- ./cmd/holomush/` | ✅ 7 tests in `core_subsystems_test.go` — need updating | ⬜ pending |
| ARCH-05 | Gateway **direct** imports exclude core/session/grpc | unit (AST) | `task test -- ./cmd/holomush/` | ✅ `gateway_imports_test.go` — amend `forbidden` | ⬜ pending |
| ARCH-05 | Gateway **transitive closure** excludes domain (FINDING-3) | unit | `task test -- ./cmd/holomush/` | ❌ **NEW — makes INV-EVENTBUS-1's binding genuine** | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] **D-07 multipage pagination regression** — integration, `//go:build integration`.
      **⚠️ CORRECTED 2026-07-15** (external review challenge, re-verified live; see
      `07-08-PLAN.md` § `<red_framing_correction>`). This item previously read *"MUST reproduce
      the real failure: concurrent publishers … A quiet-stream page walk passes today and proves
      nothing."* **That was FALSE and is retracted.** `BeforeID` is only a **tripwire for
      `BeforeSeq`**, not a filter (`internal/eventbus/bus.go:98-104`); `matchesQuery` has no
      `BeforeID` branch (`hot_jetstream.go:392-402`) and the hot tier only advances on
      `BeforeSeq > 0` (`:338`), while the cold tier gates on `hasCursor := cursorSeq > 0`
      (`cold_postgres.go:132`). Since `ReplayTail` never sets `BeforeSeq`, **a quiet-stream
      multipage walk repeats the newest page forever** — deterministically, no concurrency.
      - **RED gate = Spec A:** quiet-stream multipage walk, pageSize ≪ total, assert no repeat
        and no skip. Iteration-bounded so a non-advancing cursor fails rather than hangs.
      - **Post-green defence-in-depth = Spec B:** concurrent publishers with per-event
        `crand` ULIDs — the proof the cursor advances by *seq*, not *ID*.
      - **Home: `cmd/holomush/`, `package main`** — NOT `test/integration/eventbus_e2e/`
        (`package eventbus_e2e_test` cannot reach the unexported `busHistoryReaderAdapter` in
        `package main`; the original location could not compile). Precedents in-package:
        `cmd_audit_dlq_replay_integration_test.go` (eventbustest under an integration tag) and
        `sub_grpc_adapters_test.go` (constructs the adapter). Use `eventbustest` embedded NATS
        (correct tier — not external-mode-specific).
      - **Both runtimes:** the Lua hostfunc path hardcodes `Seq: 0` independently
        (`stdlib_focus.go:441`) and decodes only `beforeID` (`:367-380`) — coverage MUST include
        a Lua cursor round-trip (`.claude/rules/plugin-runtime-symmetry.md`).
- [ ] **Gateway transitive-closure assertion** — unit, `cmd/holomush/gateway_imports_test.go`.
      Asserts `go list -deps ./internal/telnet` and `./internal/web` contain no
      `internal/{world,access,command,store,grpc,plugin,eventbus,auth}`. Closes FINDING-3's
      gap; is the **genuine** binding for INV-EVENTBUS-1.
      ⚠️ **`internal/auth/service` does not exist** — the real package is `internal/auth`. The
      phantom is live in `gateway_imports_test.go:107` and `invariants.yaml:2345`; 07-04 Task 2
      fixes both.
- [ ] **Topological start-order pin** — unit, `cmd/holomush/core_subsystems_test.go`. Pins the actual
      `topoSort` sequence so MEDIUM-11's comment-vs-graph divergence cannot recur.
- [ ] **`StopAll` deadline test** — unit, `internal/lifecycle/`.
- [ ] **Prepare/Activate rollback test** — unit, `internal/lifecycle/` (Wave B; shape depends on D-13.1).
- [ ] Framework install: **none needed** — all frameworks present.

---

## Invariant Binding — INV-EVENTBUS-1

Per `.claude/rules/invariants.md`, a binding is genuine only when the annotated test **actually
asserts** the invariant. Never fabricate (the documented INV-RB-3 false-green bug); guarded by
`TestBoundInvariantsAreGenuinelyAsserted` (`test/meta/invariant_registry_test.go:1128`).

1. **Amend `summary`** (`docs/architecture/invariants.yaml:2340-2348`) to add `internal/core`,
   `internal/session`, `internal/grpc` (D-17). Keep in sync with the `forbidden` list.
2. **Fix the stale `refs` token** (FINDING-8): the entry claims token `INV-GW-1` in
   `cmd/holomush/gateway_imports_test.go`, but that file carries `INV-EVENTBUS-1` (lines 21, 111,
   122). ⚠️ Do **not** "fix" this by renaming to `INV-GW-1` — see D-18.
3. **Annotate** `// Verifies: INV-EVENTBUS-1` above `TestGatewayImportsAreOnlyProtocolTranslation`
   (`gateway_imports_test.go:114`) — it genuinely asserts, so the binding is legitimate.
4. **Also annotate the NEW closure test.** The AST test proves only the *direct-import* half; read
   as a transitive claim (D-15's principle) it is a **partial binding** — the exact hazard the rule
   warns needs human review. `asserted_by` should list **both** tests.
5. Set `binding: bound` + add `asserted_by:` (a `pending` entry MUST NOT carry `asserted_by` — both
   change together).
6. Run `go run ./cmd/inv-render` (never hand-edit the generated regions).
7. Verify: `task test -- -run 'TestEveryRegistryInvariantHasBinding|TestProvenanceGuard|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/`

---

## Manual-Only Verifications

*All phase behaviors have automated verification.* This phase is a pure internal refactor with no
new user-facing surface; the E2E Playwright suite (`task test:e2e`) covers the telnet/web
behavior-preservation claim without manual steps.

---

## Doc Amendments Required In-Phase

Not a test, but a phase-completeness gate the research surfaced:

- [ ] `CLAUDE.md` (§ ULID Generation / Event construction) and `.claude/rules/event-conventions.md`
      both **mandate `core.NewEvent()`** — which this phase deletes. Both MUST be amended in the same
      change, or the repo's own always-loaded rules instruct future work to call a deleted symbol.

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s (unit tier)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
