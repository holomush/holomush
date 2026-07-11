# Issue Plan — Arch Review 2026-07-11

Proposed GitHub issues/epics derived from the review. **Nothing is filed until you approve this plan** (§ Approval gate at the bottom). All target `holomush/holomush`. Dedup verified against the 186-issue open snapshot (`evidence/open-issues.json`) — every item below is untracked unless it says otherwise.

Labels use the repo's taxonomy: `bug`/`enhancement`, `priority::critical|high|medium|low`, topical, `theme:*`, plus a review tag `review-finding`.

Bodies will carry the AI-authorship byline and link to the evidence file (`docs/reviews/arch-review/2026-07-11/findings/…` + `verification/…`).

---

## Epic E1 — "Assurance artifacts overstate reality" (the trust-gap theme)

> **Epic issue.** Title: `epic: close the assurance-gap — docs/tests/UI claims exceed what the code delivers`
> Labels: `enhancement`, `priority::medium`, `theme:assurance-integrity` (new theme — will also add a `docs/roadmap.md` narrative section per the CLAUDE.md theme rule), `review-finding`, `docs`
> Body: links the three child issues below (I2 PWA / I3 coverage / I4 DLQ-test); frames the pattern (F3-test/F6/F7 + the product-readiness cousin F5/I8); notes the fix is mostly docs + CI config, little runtime code. **Because this adds a `theme:*` label, the same PR MUST add a `docs/roadmap.md` section (MUST-NOT-orphan-labels rule).** *(F1 was originally the fourth child but is re-scoped to a standalone architecture High — see I1.)*

> **Note:** I1 (event sourcing) was **re-scoped OUT of this epic and promoted to a standalone High** (see below) after the reviewer challenged it — it is an architecture-decision investigation, not a doc fix. The epic now covers F3-test/F6/F7 only.

| ID | Title | Labels | Priority | Evidence |
|----|-------|--------|----------|----------|
| I2 (F6) | `docs: web client is not an offline PWA — no service worker/manifest exists` | `bug`,`docs`,`review-finding` | medium | `findings/d8a-ui-static.md` (H1), `verification/skeptic-d8-movement-pwa.md`. AC: either correct architecture.md:298/19 + operating/index.mdx:25 to drop "offline-capable PWA", OR ship a minimal `web/src/service-worker.ts` + manifest. Recommend correcting docs now, tracking real PWA separately. |
| I3 (F7) | `ci: ">80% per-package coverage" MUST is unenforced; main merged at 54.6% patch` | `bug`,`ci`,`test-coverage`,`review-finding` | high | `findings/d9a-testing-ci.md` (H1), `verification/spot-checks.md`. AC: either (a) add a per-package/per-flag Codecov gate to `codecov.yml` + branch-protection required check, OR (b) soften CLAUDE.md:187/testing.md:25 to the enforced reality + a ratchet plan. Related (not dup): #4631 (codecov backfill debt). |
| I4 (F3-test) | `test: DLQ replay "coverage" is tautological — hardcodes matching game_id both sides` | `bug`,`aspect:tests`,`review-finding` | medium | `verification/skeptic-d6-dlq.md`. `dlq_replay_integration_test.go:25-27` uses game "main" on server AND CLI, so it can't catch the F3 mismatch. AC: parameterize the test with divergent server/CLI game_id (reproduces F3), then make it pass via I7's fix. Pairs with I7. |

---

## Standalone High issues (architecture + operational + product readiness)

| ID | Title | Labels | Priority | Evidence & acceptance |
|----|-------|--------|----------|------------------------|
| **I1 (F1)** | `architecture: event sourcing was never built for the world model — investigate the divergence, decide (ADR)` | `bug`,`architecture`,`priority::high`,`review-finding`,`design-needed` | **high** | `findings/d1-architecture.md` (H1), `verification/skeptic-d1-eventsourcing.md`, **`verification/f1-eventsourcing-why.md`** (archaeology). **NOT a doc fix.** World state is CRUD-canonical (`world/service.go`), events are a one-way notification log (`event_store_adapter.go:33`); no rebuild-from-events path ever existed; the removed "replay" (F7) was client-catch-up, not state derivation; **no ADR** in 197 files. It is the root cause of I18 (last-write-wins) and the dual-write gap. AC: (1) establish intent — was world-state event sourcing ever meant to be real, or was "event sourcing" always shorthand for "event-driven + audit log"? (2) DECIDE via ADR: **(A)** build a real projection/rebuild or transactional-outbox path, OR **(B)** formally adopt CRUD-canonical + optimistic-concurrency + transactional outbox (closes I18 + dual-write) and **downgrade the "event sourcing" principle in all 6 doc sites** (CLAUDE.md:274, architecture.md:79/305-309, coding-standards.md:344-347, index.mdx:40-42 public site). (3) capture the ADR — its absence *is* the finding. Pairs with I11 (resilience) and I18. |
| I5 (F2) | `security: gateway ConnectRPC handler has no request-body cap → unauthenticated OOM` | `bug`,`priority::high`,`review-finding` (+ security) | high | `findings/04-perimeter-platform-security.md` (H1), `verification/spot-checks.md`. `internal/web/server.go:68-76` sets no `WithReadMaxBytes`; connect-go v1.20.0 buffers unbounded when unset; core gRPC caps 4 MiB (`grpc/server.go:56`). AC: add `connect.WithReadMaxBytes(4<<20)` (match gRPC) + `http.Server.ReadTimeout`; regression test posting an oversized body → `resource_exhausted`. |
| I6 (F4) | `data: events_audit grows forever — extend RetentionWorker to it` | `bug`,`priority::high`,`review-finding` (+ data/observability) | high | `findings/d7-data.md` (H1), `verification/spot-checks.md`. `events_audit` (migration 000009) has no retention/partition; `internal/audit/retention.go` RetentionWorker+PartitionManager already serve the ABAC audit table. AC: extend retention/partition to events_audit (or document explicit unbounded-by-design + operator prune runbook); config for retention window; test. |
| I7 (F3) | `bug: audit-DLQ replay CLI can't recover external-NATS deployments (game_id split)` | `bug`,`priority::high`,`review-finding` | high | `findings/d6-reliability.md` (H1), `verification/skeptic-d6-dlq.md`. Server DLQ subject uses `core.game_id`/ULID (`core.go:300-304,567`); CLI reads `event_bus.game_id`→"main" (`cmd_audit.go:143-149,337-343`), no bridging flag. AC: CLI falls back to `holomush_system_info.game_id` via its open PG pool, OR add `--game-id`; scope note: external-NATS only, no data loss. Pairs with I4. |
| I8 (F5) | `bug: no player-facing movement command — characters cannot walk between locations` | `bug`,`priority::high`,`enhancement`,`review-finding` | high | `findings/d8b-ui-live.md` (H1), `verification/skeptic-d8-movement-pwa.md`. `MoveCharacter` (`world/service.go:773`) has 0 production callers; no move/go/walk in any manifest; `handleExitClick→sendCommand(direction)` hits a dead registry lookup; `look`/`who` unregistered. AC: (a) register a movement command that resolves exit direction → `MoveCharacter` (or a typed facade `Move` RPC the exit button calls, per gateway-boundary — see I9); (b) register `look`/`who` (or hint to panels); (c) command→move integration test. |
| I9 (F5-followup) | `refactor: exit navigation dispatches raw string via sendCommand — add typed Move RPC` | `enhancement`,`architecture`,`review-finding` | medium | `findings/d8b-ui-live.md` (M1). Gateway-boundary rule: machine-initiated navigation should use a typed facade RPC, not the human command parser. AC: typed movement RPC on the BFF facade; exit button calls it. Depends on I8. |
| I10 (F8) | `deps: bump nats-server to v2.14.3 (2 GHSA 2026-06-29; govulncheck-blind)` | `bug`,`priority::high`,`review-finding` | high | `findings/d9c-deps.md` (H1) + `evidence/deps/`. **Likely already an open Renovate PR** — AC: verify + merge that PR; file this issue ONLY if the PR is stalled/absent. Also add a vuln-scan CI gate (govulncheck) — see I15. |

---

## Investigation issue (the §7 blind spot — highest-value follow-up)

| ID | Title | Labels | Priority | Evidence |
|----|-------|--------|----------|----------|
| I11 | `investigate: resilience under concurrent play + broker flap + replica restart` | `enhancement`,`architecture`,`review-finding`,`theme:assurance-integrity` | high | `verification/codex-opinion.md`, REPORT §7. The review's biggest methodology gap: emergent cross-subsystem behavior was not exercised. AC: a chaos/resilience test harness driving two players + a NATS partition + a replica restart + client reconnect; specifically probe whether **D1-M12 last-write-wins world writes** corrupt state under two-replica concurrency (add optimistic-version guard if so). This is an investigation, not a fix — output is a report + any bugs it finds. |

---

## Medium cluster (proposed as individual issues; group at your discretion)

| ID | Title | Labels | Pri | Evidence (dim) |
|----|-------|--------|-----|----------------|
| I12 | `perf: DEK caches bypassed on encrypted read path (~P+1 crypto_keys reads/event)` | `enhancement`,`review-finding` | medium | d5-performance M1 (`dek/manager.go:221-230,348-368`) |
| I13 | `security: 3–6 attribute providers emit empty-string sentinel — latent fail-open (ADR ti1b)` | `bug`,`abac`,`review-finding` | medium | d2-abac M1 (`location.go:72,80`, `object.go:117,125,133`, `property.go:93,102`). Related #4773 |
| I14 | `security: secure-cookie/HSTS/CSP gated on default-false flag — insecure behind proxy` | `bug`,`review-finding` | medium | d4-perimeter M1 (`gateway.go:120,314`, `security_headers.go:80-83`) |
| I15 | `ci: no automated vuln-scanning gate (govulncheck/pnpm-audit absent)` | `enhancement`,`ci`,`review-finding` | medium | d9c M2 — F8 proves it matters |
| I16 | `data: sessions.location_id unindexed — presence/"who's here" query` | `bug`,`data`,`review-finding` | medium | d7 M / d5 LOW (inconsistent with every other location table) |
| I17 | `reliability: plugin-decrypt audit emitter silently drops records (no log/metric)` | `bug`,`review-finding` | medium | d6 M (contradicts own doc comment) |
| I18 | `bug: world writes are last-write-wins, no version guard (two-replica lost-update)` | `bug`,`architecture`,`review-finding` | medium | d1 M12 — the code-level hint behind I11 |
| I19 | `docs: architecture.md self-contradicts on event ordering; mislabels transport as WebSocket` | `bug`,`docs`,`review-finding` | medium | d9b M — partially #4667 (reference, extend) |
| I20 | `docs: roadmap has orphaned theme:plugin-trust label (violates CLAUDE.md theme rule)` | `bug`,`docs`,`review-finding` | low | d9b M |
| I21 | `bug: boot-order guarantee (verifier before EventBus) unenforced by dep graph` | `bug`,`architecture`,`review-finding` | medium | d1 M11 (whole-boot fail-closed still holds) |
| I22 | `enhancement: Channels subsystem has full backend but no GUI` | `enhancement`,`review-finding` | medium | d8a M |

**Low findings** (~26) — I recommend NOT filing individually; roll the actionable ones into the relevant epic/issue above and drop pure-polish items. Notable Lows worth a line in their parent issue: argon2 `t=1` below RFC 9106 (I5), cold-read identity-DEK asymmetry (crypto defense-in-depth), gateway tripwire omits `internal/grpc`/`internal/core` (I1-adjacent), Lua per-event recompile (I12-adjacent).

---

## Dedup ledger (already-tracked — NOT re-filed)

AnsiRenderer XSS #4600 · restoreSession/onMount #4760 · mobile terminal #4618 · light-theme contrast #4728 · per-IP throttle #4606/#4676 · Lua watchdog #4675 · plugin pool sizing #4693/#4713 · crypto boot-asym #4649 · crypto E2E encryption proof #4701 · architecture.md drift #4667 (I19 extends) · unserved plugin RPCs #4691 · encrypted DLQ round-trip #4780 (I4 adjacent) · codecov backfill #4631 (I3 adjacent).

---

## Proposed filing summary

- **1 epic** (E1) + **3 epic children** (I2–I4)
- **7 standalone High** (I1 architecture + I5–I10) + **1 investigation** (I11)
- **11 Medium/Low** (I12–I22)
- **Total: ~22 new issues.** F8/I10 conditional on the Renovate PR state.
- **Of the 8 verified Highs:** I1 (F1 architecture-integrity) + I5 (F2) + I6 (F4) + I7 (F3) + I8 (F5) + I10 (F8) are High-priority; I2 (F6) + I3 (F7) sit under epic E1 at medium.

## Approval gate

**I will not run any `gh issue create` until you approve.** Options:
1. **File all** as above.
2. **File High-only** (E1 + I1–I11), defer Mediums.
3. **File a curated subset** — tell me which IDs.
4. **Adjust** labels/priorities/grouping first.

Tell me which, and whether to create the `theme:assurance-integrity` label + roadmap section (E1) or drop the theme and keep the issues flat.
