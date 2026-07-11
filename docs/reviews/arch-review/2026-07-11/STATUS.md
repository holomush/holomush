<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Review Task Ledger

Recovery point for the 2026-07-11 arch review. Re-orient after context loss by reading
`00-review-plan.md` (method) + this file (state). States: `pending → dispatched → returned → persisted → verified`.

## Phase 0 — Ground truth

| ID | Task | State | Output | Outcome |
|----|------|-------|--------|---------|
| P0.1 | Read arch docs + invariant registry | persisted | — | arch.md read; 341 invariants |
| P0.2 | CodeGraph/probe system map | persisted | `01-system-map.md` | publish flow verified from source |
| P0.3 | Open-issues snapshot for dedup | persisted | `evidence/open-issues.json` | 186 open; 63 bug/32 high |
| P0.4 | Stand up app (`task dev:obs`, background) | done (torn down) | — | stack ran during live UI pass, then `docker compose down` |
| P0.5 | Briefing pack written | persisted | `01-system-map.md` | given to all agents |

## Phase 1 — Evidence fan-out

| ID | Dimension | Agent | Model | State | Output | Outcome |
|----|-----------|-------|-------|-------|--------|---------|
| D1 | Architecture | architect-review | opus | persisted (2-pass merged) | `findings/d1-architecture.md` | **H1: event-sourcing claim FALSE (CRUD+post-commit events)**; 6M (incl M11 boot-order guarantee false, M12 world writes last-write-wins) /5L; 7 strengths. Both passes agreed on H1. |
| D2 | ABAC | abac-reviewer (repo) | opus | persisted | `findings/d2-abac.md` | SOUND/default-deny; 2M (empty-str sentinel providers; CB fail-open) 3L |
| D3 | Event crypto | crypto-reviewer (repo) | opus | persisted | `findings/d3-crypto.md` | READY; no plaintext leak/no nonce reuse; 2M (#4649 boot-asym; #4701 no E2E) 2L |
| D4 | Perimeter security | security-auditor | opus | persisted (2 files, DISAGREE) | `findings/d4-perimeter.md` (parent, 0H) + `findings/04-perimeter-platform-security.md` (worker, H1-OOM) | **ADJUDICATED (verification/spot-checks.md): OOM finding UPHELD as High** (connect-go v1.20.0 confirms unbounded body w/o WithReadMaxBytes). Merge: worker H1 + parent's 13 strengths & extra Lows. Both agree: secure-cookies default-false (M), Lua watchdog #4675, per-IP #4606 |
| D5 | Performance | performance-engineer | opus | persisted | `findings/d5-performance.md` | 0B/0H; 2M (DEK cache bypass on read; Lua recompile/event) 3L 1I |
| D6 | Reliability/observability | golang-pro | sonnet | persisted | `findings/d6-reliability.md` | **H1: audit-DLQ replay CLI broken in default deploy (game_id split core.game_id vs event_bus.game_id=main; no --game-id flag)**; 4M (silent plugin-decrypt drop; sloglint goroutine blindspot; no NATS post-boot health; no eventbus OTel spans) 2L; strengths: DLQ never-drop, fail-closed boot |
| D7 | Data layer | database-optimizer | sonnet | persisted | `findings/d7-data.md` | **H1: events_audit unbounded (no retention)**; 3M (sessions.location_id no idx; settings lost-update; pool) 3L |
| D8a | UI static audit | gsd-ui-auditor | sonnet | persisted | `findings/d8a-ui-static.md` | H1: PWA "offline" claim false (no SW/manifest); 4M (channels no GUI) 3L; #4600 XSS/#4760/#4618/#4728 |
| D8b | UI live verification | main loop + agent-browser | — | persisted | `findings/d8b-ui-live.md` + `evidence/ui/` | **H1: NO player movement command (can't walk); look/who unknown**; 2M 1L; 6 strengths |
| D9a | Testing & CI | test-automator | sonnet | persisted | `findings/d9a-testing-ci.md` | **H1: >80% per-pkg coverage MUST NOT CI-enforced; main merged @54.6% patch, cmd/holomush @53.4%**; 1M 1L; 9 strengths. buf-action deduped w/ D9c |
| D9b | Docs accuracy | general-purpose | sonnet | persisted | `findings/d9b-docs.md` | 0B/0H; 5M (architecture.md ULID-ordering self-contradiction; Channels "(future)" but shipped; "WebSocket" but ConnectRPC #4667; orphan theme:plugin-trust; plugin-manifest.md required-fields wrong; linkcheck ungated) 3L; strengths: Phase3 runbook+Sentry exhaustively accurate, 0 broken links |
| D9c | Dependencies | general-purpose | sonnet | persisted | `findings/d9c-deps.md` + `evidence/deps/` | **H1: nats-server v2.14.2 vuln (2 GHSA 2026-06-29, fix v2.14.3, govulncheck-blind); Renovate PR open**; 2M (buf-action floating tag; no vuln-scan CI gate) 5L; 10 strengths |

**Cross-cutting note:** D1-H1 (no event-replay/CRUD) and D8b-H1 (no movement) and D8b-M1/gateway-boundary interlink — movement events fire post-commit (D1-M2 dual-write). Candidate Blocker-adjacent theme: "core gameplay loop + architecture-doc honesty."

## Phase 2 — Adversarial verification

| ID | Task | State | Output | Outcome |
|----|------|-------|--------|---------|
| V1 | Skeptic wave over High findings | done | `verification/skeptic-*.md` | D1/D6/D8 skeptics complete — all UPHELD |
| V2 | Main-loop citation spot-checks | 4/8 done | `verification/spot-checks.md` | UPHELD by me: D4-OOM (connect-go src), D7-events_audit, D9c-nats, D9a-coverage-enforcement. D4 two-pass disagreement adjudicated. |
| V3 | Codex second opinion | done | `verification/codex-opinion.md` | dual-rubric + blind-spot adopted |

### High-findings verification matrix (8 High, 0 Blocker)

| Finding | Verified by | Status |
|---|---|---|
| D1-H1 event-sourcing claim false | skeptic a7db4d71 | **UPHELD+strengthened** (CRUD emits 0 events; +2 doc claims incl public marketing index.mdx:40-42; 1 orig citation weaker — swap jetstream-spec:144) |
| D4-H1 gateway OOM (no ReadMaxBytes) | ME (connect-go v1.20.0 src) | UPHELD |
| D6-H1 DLQ replay CLI game_id split | skeptic aabd531c | **UPHELD, scope corrected** (NOT zero-config default — CLI can't connect to embedded NATS; scope=external-NATS runbook. "Covering" test tautological: hardcodes matching game "main" both sides) |
| D7-H1 events_audit unbounded | ME (migrations) | UPHELD |
| D8a-H1 PWA offline claim false | skeptic a960432c | **UPHELD** (no SW/manifest/PWA dep; adapter-static; docs verbatim) |
| D8b-H1 no movement command | skeptic a960432c + my live/probe | **UPHELD** (0 prod callers; correction: MoveCharacter unit-tested only, NOT integ-tested — my finding overstated, fixed in d8b) |
| D9a-H1 coverage MUST unenforced | ME (rulesets api) | UPHELD |
| D9c-H1 nats-server v2.14.2 vuln | ME (go.mod + ops docs) | UPHELD |

**Verification scorecard: 8/8 High UPHELD (2 strengthened, 2 self-corrections applied). Zero refuted.**

## Phase 3–4 — Synthesis & issue plan

| ID | Task | State | Output | Outcome |
|----|------|-------|--------|---------|
| S1 | REPORT.md | persisted | `REPORT.md` | dual-rubric severity, C4+puml diagrams, codex §7, strengths, limitations |
| S2 | issue-plan.md | persisted | `issue-plan.md` | ~22 proposed issues (1 epic+4 children, 6 High, 1 investigation, 11 Med/Low); dedup done |
| S3 | Sign-off gate → file issues | **BLOCKED on user approval** | GitHub | present issue-plan.md; awaiting filing decision |
| S4 | Commit, push, PR | commit done; push held for filing | — | docs committed as checkpoint; push+PR after filing decision |

## Running stack

| What | Value |
|------|-------|
| `task dev:obs` task ID | — |
| Web URL | http://localhost:8080 (during live pass; stack since torn down) |
| Telnet | :4201 (during live pass; stack since torn down) |
