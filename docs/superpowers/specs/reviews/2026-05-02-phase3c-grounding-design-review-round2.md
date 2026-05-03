<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Phase 3c Grounding — Adversarial Design Review (Round 2)

**Date:** 2026-05-02
**Reviewer:** design-reviewer sub-agent
**Spec under review:** [`docs/superpowers/specs/2026-05-02-event-payload-crypto-phase3c-grounding.md`](../2026-05-02-event-payload-crypto-phase3c-grounding.md) (974 lines, eight decisions, ten master-spec edits, eight new invariants INV-53..60)
**Round 1 report:** [`2026-05-02-phase3c-grounding-design-review.md`](2026-05-02-phase3c-grounding-design-review.md)
**Verdict:** **READY** — all 5 round-1 blocking findings closed; 6 of 7 non-blocking findings closed; 1 non-blocking finding closed with mild residual prose inconsistency that does not block planning.

## Summary

Round 2 edits land cleanly. Every blocking finding has a concrete, well-grounded resolution: signature consistency on `dek.ContextID` (finding 1), pill-rate-limit ownership pinned to `cluster.Registry.ProbeAndPill` with `ErrPillRateLimited` typed error and `INVALIDATION_RATE_LIMITED` Coordinator surface (finding 2), payload schema renamed to `version`/`successor_version` with per-action semantics paragraph (finding 3), self-pill prevention added at both Coordinator filter AND Registry guard with new INV-60 + `ErrCannotPillSelf` (finding 4), and a new INV-59 binding "successful RequestInvalidation → no stale entry" as the correctness substrate (finding 5). Non-blocking edits land too: INV-58 promoted from "Documentary" to "Lint" with concrete ruleguard rule + carve-out annotation, version-DI path documented in `Config` comments, probe-pill race documented as benign, T2.5 added for SubsystemCluster wiring, GameID-misconfig paragraph added, Coordinator.Stop forward note added.

The only residual is one prose-vs-pseudocode mismatch: Decision 6's eviction-summary table (line 651) names the variable `active_version` while the receive-side code (line 632) uses `payload.version` (the actual schema field). This is a labeling inconsistency, not a structural one — the schema (lines 568, 680, 684) is uniformly `version`/`successor_version` and the prose paragraph at line 691 confirms `version` carries the active version semantics for `participants_changed`. A reasonable plan-writer will not be misled, but the table's variable name should be `version` for full consistency. Non-blocking.

No regressions detected. The Action enum (Decision 6) and receive-side dispatch (Decision 5 line 624-637) match: rotate, rekey, participants_changed, kek_rotation. INV numbering 53..60 is contiguous with no duplication (verified: INV-53 unique-MemberID, INV-54 cluster_id namespacing, INV-55 pill→Trigger, INV-56 single-retry, INV-57 rate-limit, INV-58 no-cross-host-clock, INV-59 RequestInvalidation post-condition, INV-60 self-pill prevention). The two `Config` structs (`cluster.Config` at line 165, `invalidation.Config` at line 518) are explicitly disambiguated: `invalidation.Config` carries a comment (lines 522-524) that `PillRateLimit` lives on `cluster.Config` and is enforced at Registry-level. `ErrPillRateLimited` is used consistently across §271, §552, §914 (Coordinator surfaces it as `INVALIDATION_RATE_LIMITED` — distinct surfaces for the same protocol failure, by design).

## Per-finding verdicts

### Blocking finding 1 — `InvalidateContext` signature inconsistency — **CLOSED**

- **Round-1 issue:** `dek.ParticipantsCache.InvalidateContext(ctxType, ctxID string)` (string-pair) and `dek.Cache.InvalidateContext(ctxID dek.ContextID)` (struct) had incompatible signatures; receive-side pseudocode treated `payload.ctxID` as both.
- **Spec line 342 (current):** `func (c *ParticipantsCache) InvalidateContext(ctxID ContextID)` — single canonical struct shape, matches `dek.Cache` per line 365.
- **Spec lines 626-627 (receive-side dispatch):**

  ```text
  case ActionRekey:
    c.dekCache.InvalidateContext(payload.ctxID)
    c.partCache.InvalidateContext(payload.ctxID)
  ```

  Both calls pass the same `payload.ctxID` to two methods with the same signature. Consistent.
- **Verdict:** CLOSED. The string-pair shape from round 1 is gone; both caches use `dek.ContextID`. The implementation can proceed without two diverging variant readings.

### Blocking finding 2 — Pill-rate-limit ownership ambiguity — **CLOSED**

- **Round-1 issue:** Decision 1 put `ProbeAndPill` on `cluster.Registry` while Decision 2 + INV-57 said "Coordinator MUST NOT issue more than one pill," contradicting the API surface and leaving "other consumers" of `ProbeAndPill` (operator @evict-member, future split-brain detector) able to bypass the rate-limit.
- **Spec line 271 (Decision 2 pill-rate-limit paragraph):**
  > `cluster.Registry.ProbeAndPill` MUST NOT issue more than one pill targeted at the same `(member_id, reason)` within `cluster.Config.PillRateLimit` (default 60s). Rate-limited attempts return `ErrPillRateLimited` and do not reach the wire. ... **The rate-limit lives on `cluster.Registry` rather than on `invalidation.Coordinator`** because `ProbeAndPill` is the single chokepoint for pill issuance; gating any one consumer would let other consumers bypass it. INV-57 records this as a testable invariant. Coordinator surfaces `ErrPillRateLimited` to its caller as `INVALIDATION_RATE_LIMITED`.
- **Spec line 295 (cost-line):** "Pill-rate-limit storage: in-memory **per-Registry** map of `(member_id, reason) → last_pill_time`" — corrected from round-1's "per-coordinator map."
- **Spec line 552 (Coordinator typed-error table):** `INVALIDATION_RATE_LIMITED` row says "`cluster.Registry.ProbeAndPill` returned `ErrPillRateLimited`; Coordinator surfaces it."
- **Spec line 914 (INV-57 restated):** "`cluster.Registry.ProbeAndPill` MUST NOT issue more than one pill ... Rate-limited attempts MUST return `ErrPillRateLimited` and not reach the wire. **The rate-limit gates ALL consumers of `ProbeAndPill`, not only Coordinator.**"
- **Verdict:** CLOSED. Authority pinned to Registry; typed error `ErrPillRateLimited` surfaces consistently in Decision 1 §271, cost-line §295, Coordinator typed-error table §552, security analysis §820, INV-57 §914. Coordinator's surface name `INVALIDATION_RATE_LIMITED` is consistently distinguished from the wire-side error `ErrPillRateLimited` — no naming-collision regression.

### Blocking finding 3 — `participants_changed` payload `old_version` field — **CLOSED**

- **Round-1 issue:** Receive-side handler keyed eviction on `payload.old_version`, but Add() has no old/new framing — Add() mutates the active version in-place. Plan-writers would populate `old_version=0` and the cache eviction would silently no-op on `Version: 0`.
- **Spec lines 567-571 (send-side payload construction):**

  ```text
  payload = {seq, coordinator: c.registry.Self(), ctxType, ctxID,
             action, issued_at, version, successor_version}
  // Per-action population (Decision 6): rotate sets both; rekey sets both;
  // participants_changed sets only `version` (the mutated active version);
  // kek_rotation sets neither.
  ```

  Field renamed from `old_version` to `version`/`successor_version`; per-action population rule explicit.
- **Spec lines 628-633 (receive-side ActionParticipantsChanged):**

  ```text
  case ActionParticipantsChanged:
    c.partCache.Invalidate(ParticipantsCacheKey{
      ContextType: payload.ctxType,
      ContextID:   payload.ctxID,
      Version:     payload.version,  // the mutated active version
    })
  ```

  Eviction keys on `payload.version` with inline comment binding to "mutated active version."
- **Spec line 691 (Decision 6 explicit per-action paragraph):**
  > the `version` field is the **primary affected version** ... For `participants_changed`, only `version` is populated because Add() mutates the active version in place — there is no "successor." For `kek_rotation`, neither version is meaningful because the action invalidates wrap-provider metadata, not a specific DEK version.
- **Spec line 684 (Decision 6 action enum table):** "Triggered by | Payload `version` | Payload `successor_version` | Cache scope to evict" — every row populates these consistently with the per-action paragraph.
- **Verdict:** CLOSED. Schema field renamed canonically across the spec; per-action semantics documented; receive-side eviction keys on the populated field. INV-59 (new, see finding 5) provides the load-bearing assertion that ties `Add(participant)` → `RequestInvalidation` post-condition.
- **Residual (non-blocking):** Decision 6 line 651 still uses the *descriptor* `active_version` in the eviction-summary table, while the schema and pseudocode use `version`. Prose-vs-pseudocode labeling drift; not load-bearing because the surrounding Decision 6 paragraph at line 691 binds them explicitly. Worth a one-token edit in round 3 if any other revision touches this section, but does not block planning.

### Blocking finding 4 — Single-replica self-pill — **CLOSED**

- **Round-1 issue:** N=1 + hung handler → probe self → pill self → `os.Exit(125)`. Cluster self-immolates.
- **Spec lines 279-281 (NEW "Self-pill prevention" subsection in Decision 2):**
  > The Coordinator's missed-ack handler MUST filter `cluster.Registry.Self()` out of the missing-member set before invoking `ProbeAndPill`. `cluster.Registry.ProbeAndPill` itself MUST also refuse `id == r.Self()` and return `ErrCannotPillSelf` — defense in depth against any future caller that bypasses the Coordinator's filter. INV-60 records this. ... The cluster has self-immolated with N=1. Master spec §1 does not list "GC pause longer than 5s" as a threat ... the operator-facing failure mode under self-timeout becomes `INVALIDATION_PARTIAL_FAILURE` with a single-member missing set, a structured WARN log, and a `cluster_self_timeout_total` metric increment — the operator investigates the local handler hang rather than the supervisor restarting the process in a loop.
- **Spec line 554 (Coordinator typed-error table — new row):** `INVALIDATION_SELF_TIMEOUT` row added: "Coordinator's missed-ack set after probe-and-pill phase contains only `cluster.Registry.Self()` ... Caller logs + escalates; `cluster_self_timeout_total` metric increments. Operator investigates local cache or handler hang rather than process being restarted in a loop."
- **Spec line 917 (INV-60):**
  > `cluster.Registry.ProbeAndPill(ctx, id, reason)` MUST refuse `id == r.Self()` and return `ErrCannotPillSelf` without issuing a probe or pill. Coordinator's missed-ack handler MUST filter `r.Self()` out of the missing-member set before calling `ProbeAndPill`. On single-replica deployments (N=1), this prevents the local Coordinator from self-pilling when the local invalidation handler hangs ... | Unit |
- **Verdict:** CLOSED. Defense-in-depth (Coordinator filter + Registry guard) addresses the round-1 attack vector. The new INV-60 is testable at unit level. The existing INV-55 ("production Pill MUST terminate the process with exit code 125") and INV-60 ("ProbeAndPill MUST refuse self") are non-overlapping — INV-55 is what happens *after* a pill arrives; INV-60 is what stops a pill from being issued in the first place. No regression.

### Blocking finding 5 — Missing correctness-binding INV — **CLOSED**

- **Round-1 issue:** The 6 new INVs (INV-53..58) covered protocol mechanics; none bound the load-bearing "after Add() returns, every replica's Participants() reflects the new participant within timeout" promise. The Phase 3c substrate would be ~14 tasks of performance work without any correctness invariant to justify the cost above Phase 3b's uncached path.
- **Spec line 916 (INV-59, NEW):**
  > A successful `Coordinator.RequestInvalidation(ctx, ctxID, ActionParticipantsChanged)` MUST result in every other live member's `dek.ParticipantsCache` having no entry for `(ctxType, ctxID, version)` upon return. Equivalently: after `RequestInvalidation` returns nil for `participants_changed`, every other replica's next `dek.Manager.Participants(keyID, version)` call for that `(ctxID, version)` MUST re-fetch from PG. **This is the correctness substrate that supports master spec INV-12** ("Add MUST grant immediate read access to all existing DEK history without rotating the DEK") under Decision 3's Full-scope participant caching. Phase 4's `Add(participant)` caller invokes the substrate; Phase 3c ships the substrate property and tests it via the multi-Registry harness without a production Add caller. | Integration |
- **Spec line 928 (T11):** "integration tests: multi-Registry harness; INV-28/29/53-60" — wires INV-59 into the test plan.
- **Verdict:** CLOSED. The post-condition is now a testable property (multi-Registry integration test). The phrasing is slightly different from the round-1 suggestion ("no entry" rather than "Participants returns the post-Add set") but is logically equivalent and arguably stronger: forcing the cache to MISS on the next call eliminates any subtle "the cache was lazily updated to the right value" loophole. The "Phase 4 caller invokes the substrate; 3c ships the substrate property" disposition resolves the round-1 concern that 3c had no production-side trigger.

### Non-blocking finding 1 — INV-58 cop-out — **CLOSED**

- **Round-1 issue:** "Documentary" test class for INV-58 was vague.
- **Spec line 915:** Test class is now **Lint**, with a concrete ruleguard rule `gorules/no_remote_clock_compare.go`. Spec line 865 names the rule + cites the precedent (`gorules/dek_no_serialize.go` from INV-27), spec line 928 wires it into T10.
- **Verdict:** CLOSED.

### Non-blocking finding 2 — Version-string DI path — **CLOSED**

- **Spec lines 161-164 (Config comment):**
  > ClusterID is sourced from `eventbus.Config.GameID` by the wiring layer; `HolomushVersion` is sourced from `cmd/holomush/main.go`'s `version` ldflag-set variable, injected via dependency at subsystem construction. **`internal/cluster/` MUST NOT import from `cmd/holomush/` or introduce its own ldflag variable.**
- **Verdict:** CLOSED. Explicit DI rule + explicit anti-pattern.

### Non-blocking finding 3 — Skew metric vs INV-58 — **CLOSED**

- **Spec line 867 (Decision 8 carve-out paragraph):**
  > **Skew metric carve-out from INV-58:** the skew-detection computation in this Decision intentionally subtracts a remote-sourced `published_at` from a local clock to compute drift. INV-58's prohibition applies to **protocol decisions**, not observability metrics; the skew computation is gated by an explicit `// nolint:no_remote_clock_compare // observability-only per Decision 8` annotation that the ruleguard accepts as the single allowed exception. A future maintainer who extends `cluster.Registry` MUST NOT remove this annotation without re-deriving the metric without remote clocks (e.g., heartbeat sequence drift instead of timestamp drift).
- **INV-58 itself (line 915):** "The skew-detection metric in Decision 8 is the single allowed exception, gated by a ruleguard `// nolint:no_remote_clock_compare // observability-only per Decision 8` annotation."
- **Verdict:** CLOSED. Carve-out documented at both the Decision and Invariant level.

### Non-blocking finding 4 — Probe-pill race — **CLOSED**

- **Spec line 277 (Decision 2 NEW paragraph):**
  > **Probe-pill race (benign):** the probe-then-pill sequence is not atomic. ... Documented for forensic clarity; no in-flight cancellation mechanism is added.
- **Verdict:** CLOSED. Benign race acknowledged; the round-1 finding's resolution-path-1 (document race) was taken.

### Non-blocking finding 5 — Bead taxonomy missing main.go wiring — **CLOSED**

- **Spec line 928 (T2.5 NEW):** "`T2.5` (register `SubsystemCluster` in `cmd/holomush/main.go` lifecycle wiring; cluster substrate runs from Phase 3c onward in all deployments — `invalidation.Coordinator` registration deferred to Phase 3d alongside `Crypto.Enabled` flip)"
- **Verdict:** CLOSED. T2.5 added; SubsystemCluster wiring vs Coordinator-deferral distinction explicit.

### Non-blocking finding 6 — Misconfigured shared GameID — **CLOSED**

- **Spec line 705 (Decision 6 NEW paragraph):**
  > **Misconfigured shared GameID:** if two operationally-distinct HoloMUSH instances misconfigure with the same GameID and share a NATS server, they will appear as one cluster. ... No correctness violation per se, but operator-confusing diagnostics. **This is operator error, not a substrate gap**: operators MUST configure distinct GameIDs across operationally-distinct instances. The Phase 3d follow-up bead `holomush-ojw1.3.X3` (NATS account-level deny rules under `internal.>`) is the architectural reinforcement for multi-tenancy on shared NATS if that becomes a real deployment shape; not Phase 3c scope.
- **Verdict:** CLOSED. Operator-error acknowledgment + cross-reference to X3.

### Non-blocking finding 7 — Coordinator.Stop ordering — **CLOSED**

- **Spec line 496 (Decision 5 NEW paragraph):**
  > **Coordinator lifecycle ordering (forward note for Phase 3d):** when Phase 3d wires `Coordinator.Start`/`Stop`, the ordering MUST be `Coordinator.Stop` BEFORE `EventBus.Stop`. The EventBus subsystem owns `*nats.Conn`; if `EventBus.Stop` drains the conn first, Coordinator's `sub.Drain()` operates on a closed connection and surfaces `nats.ErrConnectionClosed`. Phase 3d will either (a) promote Coordinator to a `lifecycle.Subsystem` with `DependsOn() []SubsystemID{SubsystemEventBus, SubsystemCluster}` ... or (b) wire Coordinator inside EventBus's `Start` such that EventBus's own `Stop` calls `Coordinator.Stop()` before draining. Phase 3c's substrate doesn't constrain the choice; the requirement is recorded here so the 3d plan inherits it.
- **Verdict:** CLOSED. Forward note added with both options explicit.

## Cross-cutting consistency re-checks

The user prompt asked for verification that round-2 edits did not regress unrelated parts. I checked four explicit cross-cuts:

1. **Action enum (Decision 6) ↔ receive-side dispatch (Decision 5):** Decision 6 line 684 enumerates `rotate`, `rekey`, `participants_changed`, `kek_rotation`. Decision 5 line 624-637 dispatch covers `ActionRekey`, `ActionParticipantsChanged`, `ActionRotate`, `ActionKEKRotation`. Match.

2. **INV-53..60 numbering:** verified contiguous and unique. INV-53 (uniqueness), INV-54 (cluster_id namespace), INV-55 (pill→Trigger), INV-56 (single-retry), INV-57 (rate-limit), INV-58 (no cross-host clock), INV-59 (RequestInvalidation post-condition), INV-60 (self-pill prevention). No duplication.

3. **`cluster.Config` (Decision 1, line 165) vs `invalidation.Config` (Decision 5, line 518):** explicitly disambiguated. Decision 5's `invalidation.Config` carries no `PillRateLimit` field; the comment at lines 522-524 says "PillRateLimit lives on cluster.Config (Decision 1) — the rate-limit is enforced at cluster.Registry.ProbeAndPill, not at the Coordinator level, so that all consumers of ProbeAndPill are gated uniformly." No regression.

4. **`ErrPillRateLimited` typed error name:** consistent across §271, §295, §552, §820, §914. The Coordinator's protocol-error surface `INVALIDATION_RATE_LIMITED` is a distinct name (deliberately) and is also consistent across §271, §552. No spec section says `PILL_RATE_LIMITED` (would have been a regression).

**Mild prose-vs-pseudocode mismatch found (non-blocking):** Decision 6 line 651 names the variable `active_version` in the eviction-summary table while the schema fields are `version`/`successor_version`. The receive-side code at line 632 uses `payload.version` correctly. The Decision 6 paragraph at line 691 binds `version` to the active-version semantics for `participants_changed`. Plan-writers will not be misled because the schema and pseudocode are uniform; the table cell is just an informal descriptor. Worth a one-token edit (`active_version` → `version`) if any other revision touches Decision 6.

## Verification evidence

- **Read:**
  - Spec under review: full read of `docs/superpowers/specs/2026-05-02-event-payload-crypto-phase3c-grounding.md` (974 lines)
  - Round-1 review: `docs/superpowers/specs/reviews/2026-05-02-phase3c-grounding-design-review.md` (193 lines)
- **Searched (focused on round-2 edits):**
  - `InvalidateContext` — 6 occurrences; all use `dek.ContextID` (finding 1)
  - `old_version` / `active_version` / `version` / `successor_version` — schema canonicalized to `version`/`successor_version`; one residual `active_version` in Decision 6 table cell (finding 3)
  - `ErrPillRateLimited` / `INVALIDATION_RATE_LIMITED` / `PillRateLimit` / `PILL_RATE_LIMITED` — verified consistent typed-error naming; no `PILL_RATE_LIMITED` regression (finding 2, cross-cut 4)
  - `INV-5[3-9]` / `INV-60` / `INV-12` — verified INV-53..60 contiguous, INV-59 binds INV-12 (finding 5, cross-cut 2)
  - `ErrCannotPillSelf` / `INVALIDATION_SELF_TIMEOUT` / `cluster_self_timeout_total` — verified self-pill defense surface (finding 4)
  - `cluster.Config` / `invalidation.Config` / `type Config struct` — verified two distinct Configs with explicit disambiguation comment (cross-cut 3)
  - `gorules/no_remote_clock_compare` / `nolint:no_remote_clock_compare` — verified ruleguard rule named in INV-58 + carve-out annotation (non-blocking 1, 3)
  - `T2.5` — verified new bead task (non-blocking 5)
  - `Probe-pill race` / `benign` — verified §277 race paragraph (non-blocking 4)
  - `Coordinator lifecycle ordering` / `Coordinator.Stop BEFORE EventBus.Stop` — verified §496 forward note (non-blocking 7)
  - `Misconfigured shared GameID` — verified §705 paragraph (non-blocking 6)
  - `version = "dev"` / `HolomushVersion` / `ldflag` — verified Config comment §161-164 (non-blocking 2)
- **Section coverage:** all required sections present and unchanged from round 1 (problem, goals, non-goals/out-of-scope, testable acceptance criteria, named interfaces). No regression.
- **Testability:** Of 8 INVs (INV-53..60): 6 Unit, 1 Integration (INV-54), 1 Lint (INV-58), plus INV-55 has dual Unit+e2e and INV-59 is Integration. Every invariant has a testable shape. INV-58's "Lint" classification is now concrete (the ruleguard rule is named).

## Verdict

- [x] **READY** — no blocking findings remain; planning may proceed
- [ ] NOT READY

The 5 blocking findings from round 1 are all resolved with concrete, well-grounded edits. The 7 non-blocking findings are either fully closed or, in one case (the `active_version` vs `version` table-cell label drift), reduced to a token-level prose inconsistency that does not affect implementation. The spec is ready for `superpowers:writing-plans`.

**Optional round-3 cleanup (does NOT block planning):** rename Decision 6 line 651's `active_version` to `version` for full schema-vs-prose uniformity. Worth doing only if another edit touches Decision 6.

## Persisted report

Absolute path: `/Volumes/Code/github.com/holomush/.worktrees/ojw1.3-phase3c/docs/superpowers/specs/reviews/2026-05-02-phase3c-grounding-design-review-round2.md`
