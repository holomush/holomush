---
phase: 01
slug: channels-subsystem
status: passed
nyquist_compliant: true
wave_0_complete: true
created: 2026-07-08
validated: 2026-07-09
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
| 01-02-01 | 02 | 1 | CHAN-01, CHAN-02 | T-01-07 | binary SDK SessionStreamsHandler hook routes QuerySessionStreams + manager isValidStreamName accepts dot subjects (HIGH-1) | unit | `task test -- ./pkg/plugin/ ./internal/plugin/` | ❌ | ⬜ pending |
| 01-02-02 | 02 | 1 | CHAN-01, CHAN-02 | T-01-09, T-01-22, T-01-08 | stream.subscription served with real LIVE_ONLY (HIGH-2, no history flood + FROM_CURSOR regression) + concrete-stream authz guard + seed:plugin-stream-subscribe (HIGH-3) | unit | `task test -- ./internal/plugin/hostcap/ ./internal/plugin/pluginauthz/ ./internal/grpc/ ./internal/access/` | ❌ | ⬜ pending |
| 01-02-03 | 02 | 1 | CHAN-01, CHAN-02 | T-01-07 | StreamSubscription client + undeclared-capability fails closed | unit | `task test -- ./pkg/plugin/` | ❌ | ⬜ pending |
| 01-03-01 | 03 | 1 | CHAN-01, CHAN-03 | T-01-10 | plugin skeleton + migrations + name regex + transition validation | unit | `task test -- ./plugins/core-channels/` | ❌ W0 | ⬜ pending |
| 01-03-02 | 03 | 1 | CHAN-01 | T-01-10, T-01-11 | store CRUD, idempotent join, case-insensitive name lookup, soft archive | unit + int | `task test:int` | ❌ | ⬜ pending |
| 01-03-03 | 03 | 1 | CHAN-01 (D-01) | T-01-13 | idempotent default-channel seed (`Public`, no dup, no membership rows) + ListDefaultChannels | unit + int | `task test:int` | ❌ | ⬜ pending |
| 01-04-01 | 04 | 2 | CHAN-02, CHAN-04 | T-01-01, T-01-12, T-01-13 | resource-side membership resolver, omit-don't-sentinel, uniform NotFound | unit | `task test -- ./plugins/core-channels/` | ❌ | ⬜ pending |
| 01-04-02 | 04 | 2 | CHAN-02, CHAN-04 | T-01-01, T-01-02, T-01-14 | default-deny Layer-1/2 policies, public-read=visibility (LOW/MED-7), write-channel-as-member (MED-5), owner moderation, faction seam | unit + lint | `task test -- ./plugins/core-channels/` | ❌ | ⬜ pending |
| 01-05-01 | 05 | 3 | CHAN-01, CHAN-02, CHAN-04 | T-01-02, T-01-12, T-01-15, T-01-16 | per-RPC ABAC, admin-gated + 5/hr rate-limited create, uniform not-found | unit | `task test -- ./plugins/core-channels/` | ❌ | ⬜ pending |
| 01-05-02 | 05 | 3 | CHAN-01 | — | ChannelService registered via ServiceProvider; eval capability wired | unit + lint | `task test -- ./plugins/core-channels/` | ❌ | ⬜ pending |
| 01-06-01 | 06 | 4 | CHAN-02, CHAN-03 | T-01-04 | CommunicationContent emit on dot subjects, qualified wire type, plaintext, no channel_name authz | unit | `task test -- ./plugins/core-channels/` | ❌ | ⬜ pending |
| 01-06-02 | 06 | 4 | CHAN-02, CHAN-03 | T-01-01, T-01-17, T-01-18, T-01-05 | idempotent AuditEvent, membership-gated QueryHistory (auth step-1, all types), joined_at floor, scrollback cap | int | `task test:int` | ❌ | ⬜ pending |
| 01-05b-01 | 05b | 5 | CHAN-02, CHAN-04 | T-01-14, T-01-12, T-01-16 | ChannelService moderation RPCs (invite/mute/ban/kick/transfer) owner+admin-only ABAC + notice emit (HIGH-4) | unit | `task test -- ./plugins/core-channels/` | ❌ | ⬜ pending |
| 01-05b-02 | 05b | 5 | CHAN-02 | T-01-02, T-01-01, T-01-12 | ChannelService PostToChannel/WhoInChannel/QueryChannelHistory membership-gated (HIGH-4) | unit + int | `task test:int` | ❌ | ⬜ pending |
| 01-07-01 | 07 | 6 | CHAN-01, CHAN-02, CHAN-04 | T-01-14, T-01-02 | command router delegates to complete service + `=name` manifest-alias routing (MED-6) + write cap↔policy (MED-5) + owner/admin-only moderation, uniform not-found | unit | `task test -- ./plugins/core-channels/` | ❌ | ⬜ pending |
| 01-07-02 | 07 | 6 | CHAN-02 | T-01-05, T-01-19 | retention prune sweep window boundary; unlimited-retention preserved | unit + int | `task test:int` | ❌ | ⬜ pending |
| 01-08-01 | 08 | 7 | CHAN-01, CHAN-02 (D-01) | T-01-01, T-01-20 | QuerySessionStreams memberships ∪ ListDefaultChannels; guest gets seeded only; banned excluded | unit | `task test -- ./plugins/core-channels/` | ❌ | ⬜ pending |
| 01-08-02 | 08 | 7 | CHAN-01, CHAN-02 | T-01-01, T-01-09 | mid-session join/leave AddStream/RemoveStream (LIVE_ONLY, via HIGH-2/HIGH-3-guarded 01-02 substrate) | unit | `task test -- ./plugins/core-channels/` | ❌ | ⬜ pending |
| 01-09-01 | 09 | 8 | CHAN-05 | T-01-21 | whole-system census loads core-channels fail-closed (INV-PLUGIN-54) | whole-system int | `task test:int` | ❌ | ⬜ pending |
| 01-09-02 | 09 | 8 | CHAN-01, CHAN-02, CHAN-03, CHAN-04 | T-01-01, T-01-12 | e2e join→post→live delivery; non-member denied + no delivery; private uniform not-found; `=Public hello` raw-input routing (MED-6) | int | `task test:int` | ❌ | ⬜ pending |
| 01-09-03 | 09 | 8 | CHAN-05 | — | INV-CHANNEL-1 (history-content, all types; LOW/MED-7 wording) /2 registered + genuinely bound; INV-S7 (N=2) validated | meta | `task test -- ./test/meta/` | ❌ | ⬜ pending |

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

*None.* Cross-AI review MED-6 resolved the `=name` routing to a concrete manifest-seeded system prefix alias (`aliases: ["="]` on the `channel` command), so the previously manual `=name` shorthand routing is now AUTOMATED: the parser/router mapping is covered by `commands_test.go` (01-07) and the live alias-seeded raw-input hop (`=Public hello` reaches core-channels and posts, with live delivery to a second member) is covered by the 01-09 whole-system e2e (`task test:int`). All behaviors have automated verification (see map above).

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s (unit)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending

---

## Validation Audit 2026-07-09

`gsd-nyquist-auditor` (post-execution). Verdict: **COVERED / nyquist_compliant** — every CHAN requirement and key behavior maps to a targeting, non-trivial automated test. Confirmed live: `task test -- ./plugins/core-channels/` → 189 tests green; full suite `task test:int` 10394 green (whole-system census + channels E2E).

| Metric | Count |
|--------|-------|
| Requirements audited | 5 (CHAN-01..05) + 7 key behaviors |
| Gaps found | 0 |
| Tests generated | 0 |
| Escalated | 0 |

### Requirement → test map (targeting tests)

| Req | Status | Targeting test (path:line) |
|-----|--------|----------------------------|
| CHAN-01 join/leave/list, location-independent | ✅ COVERED | `plugins/core-channels/store_test.go:116,126,136,144,169`; `service_test.go:470,483,522`; `test/integration/channels/channels_e2e_test.go:98` |
| CHAN-02 post + membership-gated history | ✅ COVERED | `service_test.go:701,725,736,797,821`; `audit_test.go:159,195,212`; `audit_integration_test.go:87`; e2e `:168` |
| CHAN-03 EventBus emit + durable audit (scene-parity) | ✅ COVERED | `publish_events_test.go:50,94,143`; `audit_integration_test.go:63`; e2e `:168` |
| CHAN-04 public/private/admin types | ✅ COVERED | `resolver_test.go:94,122,169`; `service_test.go:270`; `internal/access/policy/seed_test.go:82,152`; e2e `:226,248` |
| CHAN-05 substrate-parity / INV-S7 N=2 | ✅ COVERED | `test/integration/wholesystem/census_test.go:26` (core-channels + core-scenes; INV-PLUGIN-54) |

### Key behaviors (all ✅ COVERED)

default-deny ABAC (`seed_test.go:82`, `audit_test.go:227`, `service_test.go:230,457`) · non-member negatives (e2e `:131`, `audit_test.go:159`, `service_test.go:821`) · guest auto-join union (`session_streams_test.go:74,88,101`) · admin-gated + rate-limited create (`service_test.go:240,284,298,318`) · retention prune (`prune_test.go:60`, `prune_integration_test.go:59`) · mid-session live subscribe (`service_test.go:875,906`, `pluginauthz/streamsubscribe_test.go:20`) · INV-CHANNEL-1/2 bound (`invariants.yaml`, `channels_e2e_test.go:159,222`, `test/meta` bound-genuinely-asserted).

### Minor observations (non-gaps, no test owed)
- Rate-limit **time-window rollover** (bucket reset after 1h via clock injection) is untested; the durable 5/hr enforcement + admin bypass + 5/6 count boundary are covered. Not a CHAN requirement.
- WR-01 / WR-02 are behavioral WARNINGs already routed to human decision (beads holomush-0sc.13/.14), not test-coverage gaps.
