---
phase: 8
slug: god-object-decomposition
# status lifecycle: draft (seeded by plan-phase) → validated (set by validate-phase §6)
status: draft
nyquist_compliant: false
wave_0_complete: false
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
| **SC3** size ceilings | Decomposed units do not regrow | meta | `task test -- -run TestPhase8SizeCeilings ./test/meta/` | ❌ Wave C |
| **SC3** import direction | The 2 unwound edges never return | meta | `task test -- -run TestPhase8ImportDirection ./test/meta/` | ❌ Wave C |
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

*Seeded at plan time — populated by the planner once PLAN.md task IDs exist. Every task MUST
land in this table before `nyquist_compliant: true` can be set.*

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| *TBD* | — | 0 | ARCH-02 | T-8-01 | Seam extraction preserves ABAC chokepoint | unit+int | `task test:int` | ✅ | ⬜ pending |
| *TBD* | — | A | ARCH-01 | T-8-02 | `ValidateSessionOwnership` preamble preserved on every moved method | unit+int | `task test -- ./internal/grpc/...` | ❌ W0 | ⬜ pending |
| *TBD* | — | B | ARCH-02 | T-8-03 | Crypto manifest gates preserved on relocation | unit+int | `task test:int` | ✅ | ⬜ pending |
| *TBD* | — | C | ARCH-01, ARCH-02 | — | Ratchet pins size + direction | meta | `task test -- ./test/meta/` | ❌ W0 | ⬜ pending |

---

## Wave 0 Requirements

- [ ] `test/meta/phase8_ratchet_test.go` — SC3 both halves (size ceilings + import direction). New file.
- [ ] One separately-testable unit test per extracted CoreServer unit — SC1. New files, Wave A.
- [ ] One separately-testable unit test per extracted Manager unit — SC2. New files, Wave B.
- [ ] A plan step recording `git diff --stat origin/main...HEAD -- test/integration/` — D-15.
- [ ] Framework install: **none needed** — testify, Ginkgo, Gomega all present.

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

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 90s (quick loop)
- [ ] `task test:int` run at every wave boundary (D-17)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
