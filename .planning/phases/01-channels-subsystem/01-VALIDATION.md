---
phase: 01
slug: channels-subsystem
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-08
---

# Phase 01 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Authoritative requirement→test map lives in `01-RESEARCH.md § Validation Architecture`; this file is the sampling contract.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` + `testify` (unit); Ginkgo/Gomega (integration, `//go:build integration`) |
| **Config file** | none — Taskfile-driven; never run `go`/`golangci-lint` directly |
| **Quick run command** | `task test -- ./plugins/core-channels/` |
| **Full suite command** | `task test:int` (embedded NATS + Postgres testcontainers; needs Docker) |
| **Estimated runtime** | unit ~seconds; `task test:int` minutes (Docker startup-bound) |

---

## Sampling Rate

- **After every task commit:** `task test -- ./plugins/core-channels/` (+ `task lint`)
- **After every plan wave:** `task test:int` (Docker; exercises audit / migrations / live delivery)
- **Before `/gsd-verify-work`:** `task pr-prep` green
- **Max feedback latency:** unit < 30s; integration bounded by Docker startup

---

## Per-Task Verification Map

> Populated by the planner/executor with concrete task IDs. Behavior→tier mapping is fixed by
> `01-RESEARCH.md § Validation Architecture → Requirement → Test Map`; reproduce per task below.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 01-01-01 | 01 | 1 | CHAN-01, CHAN-02 | T-01-06 | ChannelService proto contract (no channel_name authz field; plaintext) | proto-lint | `task lint:proto` | ❌ | ⬜ pending |
| 01-01-02 | 01 | 1 | CHAN-01, CHAN-02 | T-01-06 | generated Go/web bindings committed, no stale-diff | proto-lint | `task lint:proto` | ❌ | ⬜ pending |
| 01-02-01 | 02 | 1 | CHAN-01, CHAN-02 | T-01-07 | binary SDK SessionStreamsHandler hook routes QuerySessionStreams | unit | `task test -- ./pkg/plugin/` | ❌ | ⬜ pending |
| 01-02-02 | 02 | 1 | CHAN-01, CHAN-02 | T-01-08, T-01-09 | stream.subscription served → host StreamRegistry, LIVE_ONLY mapping | unit | `task test -- ./internal/plugin/hostcap/` | ❌ | ⬜ pending |
| 01-02-03 | 02 | 1 | CHAN-01, CHAN-02 | T-01-07 | StreamSubscription client + undeclared-capability fails closed | unit | `task test -- ./pkg/plugin/` | ❌ | ⬜ pending |
| 01-03-01 | 03 | 1 | CHAN-01, CHAN-03 | T-01-10 | plugin skeleton + migrations + name regex + transition validation | unit | `task test -- ./plugins/core-channels/` | ❌ W0 | ⬜ pending |
| 01-03-02 | 03 | 1 | CHAN-01 | T-01-10, T-01-11 | store CRUD, idempotent join, case-insensitive name lookup, soft archive | unit + int | `task test:int` | ❌ | ⬜ pending |
| 01-03-03 | 03 | 1 | CHAN-01 (D-01) | T-01-13 | idempotent default-channel seed (`Public`, no dup, no membership rows) + ListDefaultChannels | unit + int | `task test:int` | ❌ | ⬜ pending |
| 01-04-01 | 04 | 2 | CHAN-02, CHAN-04 | T-01-01, T-01-12, T-01-13 | resource-side membership resolver, omit-don't-sentinel, uniform NotFound | unit | `task test -- ./plugins/core-channels/` | ❌ | ⬜ pending |
| 01-04-02 | 04 | 2 | CHAN-02, CHAN-04 | T-01-01, T-01-02, T-01-14 | default-deny Layer-1/2 policies, public-vs-private/admin, owner moderation, faction seam | unit + lint | `task test -- ./plugins/core-channels/` | ❌ | ⬜ pending |
| 01-05-01 | 05 | 3 | CHAN-01, CHAN-02, CHAN-04 | T-01-02, T-01-12, T-01-15, T-01-16 | per-RPC ABAC, admin-gated + 5/hr rate-limited create, uniform not-found | unit | `task test -- ./plugins/core-channels/` | ❌ | ⬜ pending |
| 01-05-02 | 05 | 3 | CHAN-01 | — | ChannelService registered via ServiceProvider; eval capability wired | unit + lint | `task test -- ./plugins/core-channels/` | ❌ | ⬜ pending |
| 01-06-01 | 06 | 4 | CHAN-02, CHAN-03 | T-01-04 | CommunicationContent emit on dot subjects, qualified wire type, plaintext, no channel_name authz | unit | `task test -- ./plugins/core-channels/` | ❌ | ⬜ pending |
| 01-06-02 | 06 | 4 | CHAN-02, CHAN-03 | T-01-01, T-01-17, T-01-18, T-01-05 | idempotent AuditEvent, membership-gated QueryHistory (auth step-1), joined_at floor, scrollback cap | int | `task test:int` | ❌ | ⬜ pending |
| 01-07-01 | 07 | 5 | CHAN-01, CHAN-02, CHAN-04 | T-01-14, T-01-02 | command router + `=name` shorthand + owner/admin-only moderation, uniform not-found | unit | `task test -- ./plugins/core-channels/` | ❌ | ⬜ pending |
| 01-07-02 | 07 | 5 | CHAN-02 | T-01-05, T-01-19 | retention prune sweep window boundary; unlimited-retention preserved | unit + int | `task test:int` | ❌ | ⬜ pending |
| 01-08-01 | 08 | 6 | CHAN-01, CHAN-02 (D-01) | T-01-01, T-01-20 | QuerySessionStreams memberships ∪ ListDefaultChannels; guest gets seeded only; banned excluded | unit | `task test -- ./plugins/core-channels/` | ❌ | ⬜ pending |
| 01-08-02 | 08 | 6 | CHAN-01, CHAN-02 | T-01-01, T-01-09 | mid-session join/leave AddStream/RemoveStream (LIVE_ONLY) | unit | `task test -- ./plugins/core-channels/` | ❌ | ⬜ pending |
| 01-09-01 | 09 | 7 | CHAN-05 | T-01-21 | whole-system census loads core-channels fail-closed (INV-PLUGIN-54) | whole-system int | `task test:int` | ❌ | ⬜ pending |
| 01-09-02 | 09 | 7 | CHAN-01, CHAN-02, CHAN-03, CHAN-04 | T-01-01, T-01-12 | e2e join→post→live delivery; non-member denied + no delivery; private uniform not-found | int | `task test:int` | ❌ | ⬜ pending |
| 01-09-03 | 09 | 7 | CHAN-05 | — | INV-CHANNEL-1/2 registered + genuinely bound; INV-S7 (N=2) validated | meta | `task test -- ./test/meta/` | ❌ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

(from `01-RESEARCH.md § Wave 0 Gaps`)

- [ ] `plugins/core-channels/core_channels_suite_test.go` — Ginkgo bootstrap
- [ ] `plugins/core-channels/*_test.go` unit stubs (store / service / resolver / audit / commands / prune)
- [ ] `test/integration/wholesystem/census_test.go` — add `core-channels` to `expectedPlugins`
- [ ] `api/proto/holomush/channel/v1/channel.proto` (+ `task proto && task web:generate`; commit generated `*.pb.go` in the same change)
- [ ] `QuerySessionStreams` SDK handler + `StreamRegistry` host-capability plumbing **if absent** (see RESEARCH Landmine 2)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| `=name message` telnet shorthand routing | CHAN-02 | terminal input routing over a real telnet connection | connect telnet, join a channel, type `=chan hello`, confirm live delivery to a second connected member |

*All other behaviors have automated verification (see map above).*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s (unit)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
