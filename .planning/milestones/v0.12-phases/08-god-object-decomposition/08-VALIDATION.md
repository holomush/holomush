---
phase: 8
slug: god-object-decomposition
# status lifecycle: draft (seeded by plan-phase) → validated (set by validate-phase §6)
status: validated
nyquist_compliant: true
wave_0_complete: true
created: 2026-07-19
---

# Phase 8 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from `08-RESEARCH.md` § Validation Architecture (all rows `[VERIFIED]` there).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` + `testify` (unit/meta); Ginkgo v2 + Gomega (integration) |
| **Config file** | `Taskfile.yaml` (`test`, `test:int`, `test:cover`); `.golangci.yaml` for lint |
| **Quick run command** | `task test -- ./internal/grpc/... ./internal/plugin/...` |
| **Full suite command** | `task test` then **`task test:int`** |
| **Estimated runtime** | quick ~60–90s; `task test:int` several minutes (Docker required) |

> **`task test` does NOT compile `//go:build integration` files.** This phase is a
> cross-package refactor — the exact shape that keeps `task test` green while integration
> breaks silently. `task test:int` is MANDATORY per wave (D-17), not optional.

---

## Sampling Rate

- **After every task commit:** `task test -- ./internal/grpc/... ./internal/plugin/...` + `task lint`
- **After every plan wave:** **`task test:int`** — non-negotiable (D-17). A wave that ran only
  `task test` has not been verified.
- **Before `/gsd-verify-work`:** full suite green
- **Phase gate:** `task pr-prep` green inline in the parent session before push
- **Max feedback latency:** ~90s for the quick loop

---

## Success Criteria → Test Map

| Criterion | Behavior | Test Type | Automated Command | Exists? |
|---|---|---|---|---|
| **SC1** CoreServer units independently testable | Each extracted unit constructible + exercisable with ONLY its own collaborators — no `*CoreServer`, no harness | unit | `task test -- ./internal/grpc/...` | ❌ Wave A |
| **SC1** behavior preserved | Integration + whole-system suites green, **zero assertion edits** | integration | `task test:int` | ✅ existing |
| **SC2** Manager decomposed | load / runtime / identity each constructible standalone | unit | `task test -- ./internal/plugin/...` | ❌ Wave B |
| **SC2** plugin load/lifecycle unchanged | Whole-system plugin census green | integration | `task test:int` → `test/integration/wholesystem/census_test.go` | ✅ |
| **SC3** size ceilings | Decomposed units do not regrow | meta | `task test -- -run TestPhase8DecomposedFilesStayUnderTheirCeilings ./test/meta/` | ✅ `test/meta/phase8_decomposition_test.go` |
| **SC3** import direction | The 2 unwound edges never return | meta | `task test -- -run TestPhase8ImportDirectionHasNoUpwardOrCyclicEdges ./test/meta/` | ✅ same file (`INV-PLUGIN-56`) |
| **SC3** facade holds no state | `Manager` stays a 4-field facade | meta | `task test -- -run TestPhase8FacadesHoldNoExtractedState ./test/meta/` | ✅ same file |
| **SC3** no new gateway-boundary violations | Gateway closure holds | meta | `task test -- -run TestGatewayImportsAreOnlyProtocolTranslation ./cmd/holomush/` | ✅ `cmd/holomush/gateway_imports_test.go` |
| **SC3** plugin-runtime symmetry (D-20) | Lua and binary reach the same gates | integration | `task test:int` → `test/integration/pluginparity/` (7 specs) | ✅ |
| **D-15** zero assertion churn | `test/integration/**` untouched | CI/manual | `git diff --stat origin/main...HEAD -- test/integration/` | ❌ plan step |

---

## Existing Coverage Inventory

Established by research item 7 — this is what *"passes unchanged"* concretely means.
`task test:int` runs `./...` under `-tags=integration` (`Taskfile.yaml:184-190`), so every
suite below compiles and runs; an assertion edit in any of them is the D-15 blocking signal.

**CoreServer RPC paths:**

| Suite | Exercises |
|---|---|
| `test/integration/auth/auth_suite_test.go:164` | Real `NewCoreServer` — auth RPC group |
| `test/integration/auth/multi_tab_test.go:589-596` | `Subscribe` + session round-trip |
| `test/integration/phase1_5_test.go:270,341,503` | Three `NewCoreServer` — command execution |
| `test/integration/presence/reattach_presence_test.go` | `Disconnect` + `SelectCharacter` |
| `test/integration/stream_history/`, `list_session_streams/`, `streams/` | Current-state query cluster |
| `test/integration/command/`, `session/`, `scenes/` | Command + lifecycle + focus clusters |

**Plugin load/lifecycle paths:**

| Suite | Exercises |
|---|---|
| `test/integration/wholesystem/census_test.go:25-29` | **The census** — `ListPlugins()` must contain the 9 expected manifest names, loaded via `Manager.LoadAll` |
| `test/integration/wholesystem/abac_test.go` | Whole-system ABAC over loaded plugins |
| `test/integration/plugin/` (15 files) | Load-time unit behavior |
| `test/integration/pluginparity/` (7 files) | **D-20 symmetry proof** |
| `test/integration/plugincrypto/` | `PluginRequestsDecryption` / `PluginCanReadBack` (D-19 crypto surface) |

---

## Per-Task Verification Map

Populated at phase close from the nine executed plans. Every task has an automated verify.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 08-01.T1 | 08-01 | 1 | ARCH-02 | — | Neutral focus contract introduced with no behavior change | unit | `task test -- ./internal/focuscontract/...` | ✅ | ✅ done |
| 08-01.T2 | 08-01 | 1 | ARCH-02 | — | `internal/grpc/focus` originals become aliases — identical types, no conversion | unit | `task test -- ./internal/grpc/...` | ✅ | ✅ done |
| 08-01.T3 | 08-01 | 1 | ARCH-02 | T-8-01 | Wave gate: ABAC chokepoint intact | integration | `task test:int` | ✅ | ✅ done |
| 08-02.T1 | 08-02 | 2 | ARCH-02 | — | 7 plugin files rewired off `internal/grpc/focus` (D-09 seam 1) | unit | `task test -- ./internal/plugin/...` | ✅ | ✅ done |
| 08-02.T2 | 08-02 | 2 | ARCH-02 | T-8-03 | Seam 2 inverted; fail-closed nil-receiver guard carries the deleted adapter's contract | unit+int | `task test:int` | ✅ | ✅ done |
| 08-02.T3 | 08-02 | 2 | ARCH-02 | — | `TestLoadPlugin` out of the production binary (D-08) | unit | `task test -- ./internal/plugin/...` | ✅ | ✅ done |
| 08-02.T4 | 08-02 | 2 | ARCH-02 | T-8-01 | Wave gate | integration | `task test:int` | ✅ | ✅ done |
| 08-03.T1 | 08-03 | 3 | ARCH-01 | T-8-02 | `SubscribeHandler` constructible with only its own collaborators | unit | `task test -- ./internal/grpc/...` | ✅ `subscribe_handler_test.go` | ✅ done |
| 08-03.T2 | 08-03 | 3 | ARCH-01 | T-8-02 | `Subscribe` preamble preserved verbatim under delegation | unit+int | `task test:int` | ✅ | ✅ done |
| 08-03.T3 | 08-03 | 3 | ARCH-01 | T-8-02 | Wave gate | integration | `task test:int` | ✅ | ✅ done |
| 08-04.T1 | 08-04 | 3 | ARCH-02 | T-8-05, T-8-06 | `IdentityStore` owns its own lock; no nested acquisition | unit | `task test -- ./internal/plugin/...` | ✅ `identity_store_test.go` | ✅ done |
| 08-04.T2 | 08-04 | 3 | ARCH-02 | T-8-05 | Identity surface delegated; mutation window unchanged | unit | `task test -- ./internal/plugin/...` | ✅ | ✅ done |
| 08-04.T3 | 08-04 | 3 | ARCH-02 | T-8-06 | Wave gate with race detection | integration | `RACE=-race task test:int` | ✅ | ✅ done |
| 08-05.T1 | 08-05 | 4 | ARCH-01 | T-8-02 | `CommandHandler` separately testable | unit | `task test -- ./internal/grpc/...` | ✅ `command_handler_test.go` | ✅ done |
| 08-05.T2 | 08-05 | 4 | ARCH-01 | T-8-02 | `LifecycleHandler` separately testable; both clusters delegated | unit | `task test -- ./internal/grpc/...` | ✅ `lifecycle_handler_test.go` | ✅ done |
| 08-05.T3 | 08-05 | 4 | ARCH-01 | T-8-02 | Wave gate | integration | `task test:int` | ✅ | ✅ done |
| 08-06.T1 | 08-06 | 4 | ARCH-02 | T-8-04 | `PluginRuntime` separately testable; Lua/binary parity preserved | unit | `task test -- ./internal/plugin/...` | ✅ `runtime_test.go` | ✅ done |
| 08-06.T2 | 08-06 | 4 | ARCH-02 | T-8-03 | Runtime surface delegated; crypto manifest gates intact | unit+int | `task test:int` | ✅ | ✅ done |
| 08-06.T3 | 08-06 | 4 | ARCH-02 | T-8-04 | Wave gate, parity + crypto focus | integration | `task test:int` → `pluginparity/`, `plugincrypto/` | ✅ | ✅ done |
| 08-07.T1 | 08-07 | 5 | ARCH-01 | T-8-02 | `QueryHandler` separately testable; `ValidateSessionOwnership` preserved on all 5 sites | unit | `task test -- ./internal/grpc/...` | ✅ `query_handler_test.go` | ✅ done |
| 08-07.T2 | 08-07 | 5 | ARCH-01 | T-8-01 | CoreServer is a 4-unit facade; fail-closed nil semantics preserved | unit | `task test -- ./internal/grpc/...` | ✅ | ✅ done |
| 08-07.T3 | 08-07 | 5 | ARCH-01 | T-8-01, T-8-02 | Wave gate + ARCH-01 closeout | integration | `task test:int` | ✅ | ✅ done |
| 08-08.T1 | 08-08 | 5 | ARCH-02 | T-8-06 | `PluginLoader` separately testable; 18/18 cross-unit sites hold no loader lock | unit | `task test -- ./internal/plugin/...` | ✅ `loader_test.go` | ✅ done |
| 08-08.T2 | 08-08 | 5 | ARCH-02 | T-8-25 | `UnloadPlugin` cleanup-before-early-return preserved; Manager is a 4-field facade | unit+int | `task test:int` | ✅ | ✅ done |
| 08-08.T3 | 08-08 | 5 | ARCH-02 | T-8-04 | Wave gate + ARCH-02 closeout; provenance retarget | integration | `task test:int` | ✅ | ✅ done |
| 08-09.T1 | 08-09 | 6 | ARCH-01, ARCH-02 | T-8-28, T-8-30, T-8-31 | Ratchet pins size + direction + facade shape; all three halves observed failing | meta | `task test -- ./test/meta/` | ✅ `phase8_decomposition_test.go` | ✅ done |
| 08-09.T2 | 08-09 | 6 | ARCH-01, ARCH-02 | T-8-29 | `INV-PLUGIN-56` bound genuinely; size half deliberately unbound | meta | `task test -- -run 'TestEveryRegistryInvariantHasBinding\|TestProvenanceGuard\|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/` | ✅ | ✅ done |
| 08-09.T3 | 08-09 | 6 | ARCH-01, ARCH-02 | T-8-01..04 | D-15 behavior preservation: zero integration-test churn | integration | `task test:int` + `git diff --stat origin/main...HEAD -- test/integration/` | ✅ | ✅ done |

---

## Wave 0 Requirements

- [x] `test/meta/phase8_decomposition_test.go` — SC3 both halves plus the facade structural check.
      (Shipped under this name, not the seeded `phase8_ratchet_test.go`; it carries a census too.)
- [x] One separately-testable unit test per extracted CoreServer unit — SC1, all four `package grpc_test`.
- [x] One separately-testable unit test per extracted Manager unit — SC2, all three `package plugins_test`.
- [x] A plan step recording `git diff --stat origin/main...HEAD -- test/integration/` — D-15 (08-09.T3; **empty**).
- [x] Framework install: **none needed** — testify, Ginkgo, Gomega all present.

---

## Threat Model Reference

This is a behavior-preserving refactor: no new trust boundary, no new input, no new endpoint.
The security question is entirely *"does moving code weaken an existing gate?"*

| ID | Threat | STRIDE | Detector |
|---|---|---|---|
| T-8-01 | A moved ABAC check loses its `accessEngine` wiring (nil engine) | Elevation of Privilege | Fail-closed by default (`server.go:191-193`); `test/integration/access/` |
| T-8-02 | A moved RPC method silently loses its `auth.ValidateSessionOwnership` preamble | Elevation of Privilege | Move bodies **verbatim**; `test/integration/auth/`; D-15 makes a silent drop a *failure*, not a diff |
| T-8-03 | A crypto manifest gate weakened while relocating | Information Disclosure | `crypto-reviewer` (D-19) + `test/integration/plugincrypto/` |
| T-8-04 | A plugin gate moves onto one runtime's path only | Elevation of Privilege / Tampering | D-20 hard constraint; `test/integration/pluginparity/` (7 specs) |
| T-8-05 | Splitting `m.mu` introduces TOCTOU between identity and runtime state | Tampering | **Mitigated by construction** — existing code already releases the lock between those mutations, so extraction preserves the current window rather than widening it |
| T-8-06 | Splitting `m.mu` introduces a lock-ordering deadlock | Denial of Service | No path holds the lock across a boundary today ⇒ no nested acquisition introduced. Empirical check: `task test:int` with `-race` |

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Zero assertion churn under `test/integration/**` | D-15 | Requires diffing intent, not just content — a *file* may legitimately change (import path) while its *assertions* must not | `git diff origin/main...HEAD -- test/integration/` and confirm every hunk is an import/constructor rewire, never an `Expect(...)` / `assert.*` / `require.*` change. Any assertion hunk needs written justification in the PR. |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies — 28 tasks, all mapped above
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 90s (quick loop)
- [x] `task test:int` run at every wave boundary (D-17) — every plan's Task 3/4 is that gate
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** closed out by 08-09 (Wave 6). The D-15 manual verification resolved to the
strongest available result: `test/integration/` has **no diff at all** across the phase branch,
so there are no hunks to classify — the intent-diffing procedure has an empty input set.
