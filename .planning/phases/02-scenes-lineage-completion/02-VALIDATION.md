---
phase: 2
slug: scenes-lineage-completion
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-08
---

# Phase 2 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from `02-RESEARCH.md` § Validation Architecture.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go — testify (unit) + Ginkgo/Gomega (integration, `//go:build integration`) |
| **Config file** | `Taskfile.yaml` task targets; no separate test config |
| **Quick run command** | `task test -- ./plugins/core-scenes/...` |
| **Full suite command** | `task test` then `task test:int` (needs Docker) |
| **Estimated runtime** | quick ~30–90s; full integration several min (Docker) |

---

## Sampling Rate

- **After every task commit:** Run `task test -- ./plugins/core-scenes/...` (scoped quick run)
- **After every plan wave:** Run `task test` (full unit); add `task test:int` on any wave touching the telnet gateway, reconnect/focus-restore, or ABAC policies
- **Before `/gsd-verify-work`:** `task test` and `task test:int` both green
- **Max feedback latency:** ~90s (unit); integration on wave boundaries

---

## Per-Task Verification Map

> Populated by the planner/executor as tasks are defined. Every task with
> business logic (throttle/coalesce, ABAC gate, store, idle transition, focus
> restore, multi-char routing) MUST carry an `<automated>` verify command.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 2-01-01 | 01 | 1 | SCENEFWD-02 | — | (fill during planning) | unit | `task test -- ./plugins/core-scenes/...` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] Table-driven unit stubs for the nudge throttle/coalesce policy (SCENEFWD-02)
- [ ] Ginkgo integration stubs for telnet `SCENE_ACTIVITY` rendering + reconnect focus-restore wiring (SCENEFWD-02/03)
- [ ] Existing `task test` / `task test:int` infra covers the rest — no new framework

*Framework already present; Wave 0 adds test stubs only.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Telnet `[>GAME: …]` nudge renders + coalesces under a busy scene | SCENEFWD-02 | Terminal render / timing feel | Join scene on telnet; generate rapid poses from another char; confirm ≤1 nudge per debounce window |
| Multi-character-per-connection focus routing | SCENEFWD-03 | Interactive multi-char session | Bind 2 chars to one telnet connection; confirm render targeting + focus routing |

*Automated coverage is primary; the above are UX/timing confirmations.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 90s (unit)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
