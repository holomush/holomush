<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Phase 3c Grounding — Adversarial Design Review

**Date:** 2026-05-02
**Reviewer:** design-reviewer sub-agent
**Spec under review:** [`docs/superpowers/specs/2026-05-02-event-payload-crypto-phase3c-grounding.md`](../2026-05-02-event-payload-crypto-phase3c-grounding.md) (936 lines, eight decisions, ten master-spec edits, six new invariants INV-53..58)
**Verdict:** **NOT READY** — five blocking findings; seven non-blocking findings.

## Summary

Phase 3c grounding is a substantial, mostly well-reasoned design that resolves real seams the master spec left open (replica discovery, failure remediation, tombstone substrate, scope of cross-replica caching, package layout, subject taxonomy, pill mechanism, time-skew handling). The strict-N + probe-and-pill protocol, the soft-delete-replaces-tombstone substitution, and the cluster-package-as-shared-surface framing are all defensible. The Phase 3b TOCTOU walk-back is technically correct (the original framing did over-attribute to Resolve when Add() is the actual mutator).

That said, the spec ships **at least one signature-level inconsistency**, **at least one rate-limit-ownership ambiguity**, **a payload-schema mismatch on the `participants_changed` action**, **a pill-isolation gap that lets self-pill kill the cluster on single-replica deployments**, and **an absent invariant for the sole "actually correctness-load-bearing" effect of the entire phase**. The "Cost" / "where to wire" sections are thin in places where they need to be exact: subsystem registration in `cmd/holomush/main.go` is mentioned but not enumerated; the version-string sourcing (`cmd/holomush/main.go:14` `version = "dev"` ldflags) is referenced but never proves how `internal/cluster/` reaches that variable across the package boundary. Finally, the test-class assignment for INV-58 is genuinely a cop-out as drafted, but recoverable.

The spec is close enough that round-2 should be cheap. I'd estimate four of the five blockers can be resolved with a single edit pass and the fifth (signature/ownership consistency) is a 30-line refactor of the design tables.

## Blocking findings

### 1. [Severity: High] `InvalidateContext` signature inconsistency between `dek.Cache` and `dek.ParticipantsCache`

- **Location:** spec §Decision 3, lines 319 and 342; receive-side flow §Decision 5 lines 595-596.
- **Evidence — verbatim quotes:**
  - Line 319: `func (c *ParticipantsCache) InvalidateContext(ctxType, ctxID string)`
  - Line 342: `` `Cache.InvalidateContext(ctxID dek.ContextID)` new method ``
  - Lines 595-596:

    ```text
    case ActionRekey:
      c.dekCache.InvalidateContext(payload.ctxID)
      c.partCache.InvalidateContext(payload.ctxType, payload.ctxID)
    ```

- **Issue:** Two siblings in the same package have different signatures for the same conceptual method. `dek.Cache.InvalidateContext` takes `dek.ContextID` (a struct holding `Type` and `ID`); `dek.ParticipantsCache.InvalidateContext` takes `(ctxType, ctxID string)` — separate string args. The receive-side pseudocode treats `payload.ctxID` as something that satisfies both signatures, which is impossible: the `dek.Cache` call passes the entire ContextID struct (so `payload.ctxID` must be `ContextID`); the `partCache` call passes `payload.ctxType, payload.ctxID` separately (so `payload.ctxID` must be `string`). These are mutually exclusive.
- **Why it blocks:** Plan-writers will pick one and the implementation will diverge from the spec. Because `dek.ContextID` is the existing canonical type in `internal/eventbus/crypto/dek/store.go:21`, the `(ctxType, ctxID string)` shape is the regression. Worse: a plan-writer who reads §Decision 3 first picks the string-pair shape and a plan-writer who reads §Decision 5 receive-side picks the struct shape; either reviewer can wave through their version.
- **Required resolution:** Pick one canonical shape (`dek.ContextID`) and update both lines (319 + 342) plus the receive-side pseudocode to match. The `byContext` reverse index on `dek.Cache` (line 340: `map[dek.ContextID]map[CacheKey]struct{}`) is already keyed by `ContextID` — keep that. `ParticipantsCache.InvalidateContext` should also take `dek.ContextID` (or the spec must explain why the two caches use different key types). Update payload schema to make `ctxID` an explicit `ContextID` field.

### 2. [Severity: High] Pill-rate-limit ownership ambiguous between `cluster.Registry` and `invalidation.Coordinator`

- **Location:** spec §Decision 1 lines 145-150, §Decision 2 line 254, §Decision 5 line 525, §INV-57 line 877.
- **Evidence — verbatim quotes:**
  - Decision 1, line 149: `` `ProbeAndPill(ctx context.Context, id MemberID, reason PillReason) error` ``
  - Decision 2, line 254: "A Coordinator MUST NOT issue more than one pill targeted at the same `(member_id, reason)` within `PillRateLimit` (default 60s, configurable via `cluster.Config.PillRateLimit`)."
  - Decision 5, line 525: `` `INVALIDATION_RATE_LIMITED` | Coordinator's pill rate-limit blocked an attempted pill within `PillRateLimit` window ``
  - INV-57, line 877: "A coordinator MUST NOT issue more than one pill targeted at the same `(member_id, reason)` within `PillRateLimit`"
  - Decision 7, line 785: "Pill rate-limit. Coordinator emits no more than one pill per `(member_id, reason)` per 60s"
  - Decision 2, line 272 (cost): "Pill-rate-limit storage: in-memory per-coordinator map of `(member_id, reason) → last_pill_time`"
- **Issue:** Decision 1 puts `ProbeAndPill` (the only pill-issuance API) on `cluster.Registry`. The pill-rate-limit is described as living on the *Coordinator* — but the Coordinator never issues a pill directly; it calls `Registry.ProbeAndPill`. Three readings exist:
  - (a) `Registry.ProbeAndPill` enforces the rate-limit (matches Decision 1's API surface but contradicts INV-57's "coordinator MUST NOT issue").
  - (b) Coordinator wraps `ProbeAndPill` in a rate-limit gate (consistent with INV-57 but means a *different* coordinator instance — or a future @evict-member admin RPC — could trivially bypass the gate by calling Registry directly).
  - (c) Both layers gate (defense-in-depth, but the spec doesn't say so and the cost line implies one map).
- **Why it blocks:** This is an authority-boundary question. INV-57 is written as a coordinator-level invariant, so it's testable only against the coordinator; but Decision 2's "Why pill authority lives with Registry, not Coordinator" (line 261-262) puts the pill primitive in `cluster.Registry` "so other consumers can reach for the same primitive." If the rate-limit is at the Coordinator, "other consumers" (Decision 2 names: operator @evict-member, future split-brain detector) can flood the wire with pills. If the rate-limit is at the Registry, the cost line is wrong (should be in `cluster.Config`, not coordinator-internal map) and the API in Decision 1 should expose `ErrRateLimited` from `ProbeAndPill`.
- **Required resolution:** Decide where the rate-limit lives. Recommendation: place it on `cluster.Registry.ProbeAndPill` (returns `ErrPillRateLimited`); have Coordinator surface it as `INVALIDATION_RATE_LIMITED`. Restate INV-57 as "`cluster.Registry.ProbeAndPill` MUST NOT issue more than one pill ..." — testable via Registry-level unit test. Update Decision 2 §272 cost line to say `cluster.Config.PillRateLimit` and "in-memory per-Registry map," not "per-coordinator map." Add a sentence to Decision 5 making clear the Coordinator is one of multiple potential rate-limit-protected callers.

### 3. [Severity: High] `participants_changed` payload uses `old_version` field that has no Add()-side meaning

- **Location:** spec §Decision 5 receive-side flow lines 597-602; §Decision 6 subject-taxonomy table line 649.
- **Evidence — verbatim quotes:**
  - Lines 597-602:

    ```text
    case ActionParticipantsChanged:
      c.partCache.Invalidate(ParticipantsCacheKey{
        ContextType: payload.ctxType,
        ContextID:   payload.ctxID,
        Version:     payload.old_version,
      })
    ```

  - Decision 6 invalidation subject row, line 649: payload `{seq, coordinator_member_id, ctx_type, ctx_id, action, issued_at, old_version, new_version}`
  - Master spec §6.1 Add semantics (verified at `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md:1307-1320`): Add appends to the active version's `participants` JSONB; **does not bump version**. There is no "old_version" or "new_version" in an Add() call — there is a single active version mutated in-place.
- **Issue:** The receive-side handler keys cache invalidation on `payload.old_version`, but Add() on the sender side has no `old_version` to populate. The spec's own §Decision 3 line 322 confirms version-keying matters because Rotate creates vN+1 with a new participants list (vN's set unchanged). A `participants_changed` invalidation is conceptually saying "vN's participant set was just mutated; evict the cached `(ctxType, ctxID, vN)` entry." So the *single* version field that matters is "current active version at time of Add" — `old_version` in the rekey/rotate sense doesn't apply. Plan-writers will either populate `old_version=current_active_version` (works but is a misnomer that obscures the mental model) or populate `old_version=0`/`new_version=0` (works in code but the receive-side cache key becomes `Version: 0`, which never hits a real entry).
- **Why it blocks:** The mismatch propagates into the implementation: a plan-writer reading "evict `Version: old_version`" without master-spec context will pass 0 (the proto/JSON zero value when old_version is irrelevant) and the eviction silently no-ops. INV-12 ("Add MUST grant immediate read access") becomes unenforced. This is the only correctness-load-bearing eviction in the entire Decision 3 "Full scope" rationale; if it silently no-ops, Decision 3 was scope creep without substance.
- **Required resolution:** Either (a) rename `old_version` to `active_version` in the payload schema and document that `participants_changed` populates it with the version whose participant set was mutated, leaving `new_version` empty/zero; or (b) use a single `version` field (the active version) and document that `rotate` sends old=N, new=N+1, `rekey` sends old=N, new=N+1, `participants_changed` sends old=N (the mutated version), `kek_rotation` sends both empty. Then add a unit-test invariant — call it INV-59 or fold into INV-56 — that "an `Add(ctxID, participant)` MUST publish a `participants_changed` invalidation whose payload's `version` field equals the `crypto_keys.version` of the row mutated by the same Add transaction."

### 4. [Severity: High] Single-replica self-pill on local handler hang terminates the entire cluster

- **Location:** spec §Decision 1 line 186 ("Self-as-member"), §Decision 2 lines 244-251 (Pill semantics).
- **Evidence — verbatim quotes:**
  - Line 186: "the local member subscribes on the cache_invalidate subject like any peer, processes the eviction, and replies on the inbox."
  - Line 248: "Receiving member subscribes only to its own pill subject."
  - Line 250: "On receive: the member writes a structured log entry, increments `replica_poisoned_total{member_id, reason, source_id}`, flushes telemetry (best-effort, bounded by 1s), and calls `cluster.Pill.Trigger()` (Decision 7)."
  - Line 715 (Pill behavior): "Production needs `os.Exit(125)`."
- **Issue:** On a single-replica deployment (N=1, self counts), the local Coordinator publishes the cache-invalidate request and self-receives. If the local invalidation handler hangs (e.g., GC pause, slow Mutex contention in the cache, ParticipantsCache lock held by an unrelated long-running Get), the 5s timeout fires; the Coordinator probes self via `internal.<cluster_id>.member.probe.<self>` and on probe timeout, publishes a pill on `internal.<cluster_id>.member.poison.<self>`. The self-subscriber receives its own pill and `os.Exit(125)`. The cluster has self-immolated with N=1.
- The spec at §Decision 8 line 821 acknowledges single-replica equivalence ("Single-replica deployments degenerate to N=1 (the local replica acks itself); the contract is identical"). The contract IS identical, but the *failure mode* is materially worse: in N≥2, pilling a hung member preserves the cluster; in N=1, it destroys it.
- **Why it blocks:** Phase 3d flips `Crypto.Enabled` and immediately operators on single-replica dev/staging hit this. A cache-handler hang for 5.25s = process restart loop. Master spec §1 ("threat model") does not list "GC pause longer than 5s" as a threat to defend against, so the substrate cost of self-induced restart is policy violation, not policy enforcement.
- **Required resolution:** Add an explicit invariant + handling rule: "A Coordinator MUST NOT issue a pill targeted at its own MemberID. On self-targeted ProbeAndPill, the Coordinator MUST log a structured WARN and return `INVALIDATION_SELF_TIMEOUT` instead of pilling self. Operator escalation path: investigate the local cache or handler hang." Add this as INV-59 or fold into INV-56. Restate Decision 2 line 271 ("Pill triggers `panic()` rather than `os.Exit(125)`: panic-and-recover is a debugging pattern...") with a paragraph addressing "what happens when the cluster has one member."

### 5. [Severity: Critical] Phase 3c ships caches but no invariant binds production correctness

- **Location:** §INV-53..58 invariant table, lines 873-878.
- **Evidence — verbatim quote:** The new-invariants table lists six invariants. INV-53 is uniqueness of MemberID. INV-54 is cluster_id namespacing. INV-55 is pill termination. INV-56 is single-retry on Coordinator timeout. INV-57 is pill rate-limit. INV-58 is "no cross-host wall clock comparison" (documentary).
- **Issue:** None of these six new invariants binds the *actual correctness goal* the entire Decision 3 "Full scope" exists to satisfy: that `Add(participant)` mutates vN and the cached participant list on every replica reflects the mutation before the next `AuthGuard.Check` decides on that vN's events.
- The spec's own §Decision 3 line 284 frames this:
  > Without cross-replica invalidation, INV-12 ("Add MUST grant immediate read access to all existing DEK history without rotating the DEK") is violated.
  But INV-12 is a master-spec invariant that pre-existed Phase 3c. The spec amends master-spec wording around §6.1 (line 857: "`Add` MUST publish via `Coordinator.RequestInvalidation(ctx, ctxID, ActionParticipantsChanged)`") but creates no new INV that says "after `Add()` returns successfully on replica A, every replica B's `dek.Manager.Participants(...)` for that context MUST return the post-Add set within `InvalidateTimeout` (5s)."
- **Why it blocks:** Phase 3c ships ~14 implementation tasks. The blast radius is two new packages, a migration, and two cache types. If you cut all that work and only keep the cross-replica-invalidation-on-Add (i.e., the Phase 3b-style "Lean" path), INV-12 is still satisfied because Phase 3b reads PG fresh on every Check. So the only thing the entire Phase 3c shipped substrate buys above Phase 3b is **performance** (Decision 3 line 290-292 calls this out: "One PG query per delivered sensitive event per recipient — substantial under load"). Performance is not an invariant. There must be a correctness-binding invariant that justifies the substrate, OR the spec must say plainly "Phase 3c is performance work; correctness is unchanged from Phase 3b" and re-justify the substrate cost on that basis. As drafted, neither story is written.
- **Required resolution:** Add INV-59 (or rename / renumber): "After `dek.Manager.Add(ctx, ctxID, participant)` returns nil on any replica, every replica's `dek.Manager.Participants(ctx, keyID, version)` for that `(ctxID, version)` MUST return a participant set containing `participant` within `InvalidateTimeout` (5s, default per §Decision 5 Config). Test class: Integration. Verified by multi-Registry test harness." This is the load-bearing invariant. INV-56 catches the *protocol-failure* case; INV-59 catches the *protocol-success* case. Without INV-59, the substrate has no fail-closed semantic.

## Non-blocking findings

### 1. [Severity: Medium] INV-58 "documentary" classification is a real cop-out as drafted

- **Location:** §Decision 8 line 830, §INV-58 line 878.
- **Evidence:** Line 878: "Documentary" test class; "no automated test possible since it's an absence-of-thing property."
- **Issue:** "No cross-host wall-clock comparison" is testable: a static analyzer (semgrep, comby, custom AST walker via `go/analysis`) can detect any code in `internal/cluster/`, `internal/eventbus/crypto/invalidation/`, and `internal/eventbus/crypto/dek/` that compares two `time.Time` values where one was deserialized from a remote-sourced field. A simpler test: a meta-test that grep's for `time.Now()` and any deserialized `published_at`/`issued_at`/`started_at`/`last_heartbeat_at` field reaching a `Before(`/`After(`/`Sub(` call across packages. Lint, not unit, is the right test class.
- **Why non-blocking:** INV-58 is genuinely a "negative space" property and a static-grep is enforcing-on-best-effort, not enforcing-by-construction. But "documentary" alone bypasses Phase 3a/3b's discipline of binding every invariant to a concrete test. CLAUDE.md "Spec invariants + docs as PR-blocking acceptance criteria" memory entry calls this out as a recurring weakness.
- **Required resolution (non-blocking):** Reframe INV-58 as "Lint" with a `gorules/no_remote_clock_compare.go` ruleguard rule that fails `task lint` on any subtraction of a published-by-remote time field from a local clock. Or add a meta-test that walks the AST of the three packages and fails on any `(receiver_local_time).Sub(remote_sourced_time)` shape. Either is enforceable.

### 2. [Severity: Medium] `cmd/holomush/main.go:14` `version = "dev"` cited but cross-package access path unstated

- **Location:** §Decision 1 lines 99-108 (Member.HolomushVersion field), §Decision 1 line 199.
- **Evidence:** The spec mentions `HolomushVersion string` on `Member`, says "Operators who want continuity look at `holomush_version` + `started_at`", and the user prompt calls out `cmd/holomush/main.go:14` (`version = "dev"`) as the source. Verified at the file: `version = "dev"` is package-private to `main`. `internal/cluster/` cannot import `main`. Either the `Subsystem` constructor takes `version string` as a Deps parameter (not stated) or `internal/cluster/` introduces its own `version` variable wired via `-ldflags -X`.
- **Issue:** The spec's "Cost: ~5 LOC" for `cmd/holomush/main.go` lifecycle wiring (line 218) doesn't enumerate the version-string injection path. Plan-writers will guess. Likely outcome: a copy-paste of the `formatVersion` shape into `internal/cluster/version.go` with a parallel `version = "dev"` and `task` ldflags update.
- **Required resolution (non-blocking):** Add one sentence to §Decision 1's "Cost" paragraph: "`cluster.Config.HolomushVersion` is sourced via dependency injection from `cmd/holomush/main.go` at subsystem construction (the `version` ldflag value); `internal/cluster/` MUST NOT introduce its own version variable." Or — alternative path — inject via `runtime/debug.ReadBuildInfo()`, but that loses ldflag-set values. State the choice.

### 3. [Severity: Medium] Heartbeat propagation-estimate `0` for in-process NATS is wrong on cluster NATS

- **Location:** §Decision 8 line 826.
- **Evidence:** "For in-process NATS, propagation is treated as 0; for cluster NATS in future deployments, the registry maintains a per-source moving average of recent `local_now - published_at` values, and skew is the deviation from that average."
- **Issue:** The "moving-average propagation estimate" is a clock-comparison construction that the same paragraph forbids in the next sentence (INV-58, line 830: "documentary invariant — enforced by code review"). The moving-average is *not* a clock comparison in the strict sense (it's a steady-state estimator), but it does subtract `published_at` (remote) from `local_now` (local). A code reviewer applying INV-58 strictly would reject this.
- **Why non-blocking:** INV-58 says "MUST NOT condition any decision" — and the skew metric is observability-only (line 829). So strictly, moving-average is fine because no protocol decision uses it. But the cohabitation of "no comparison" + "moving average comparison" without an explicit carve-out invites a future maintainer to either delete the moving-average (breaking the metric) or condition on skew (breaking INV-58).
- **Required resolution (non-blocking):** Add a sentence to Decision 8: "The skew metric uses a remote-time computation (steady-state average); INV-58's prohibition applies to protocol decisions, not observability metrics." Or: drop the moving-average and use raw `(local_now - published_at)` reported as a gauge with documentation.

### 4. [Severity: Medium] Probe response races with pill issue (Decision 2 edge case)

- **Location:** §Decision 2 lines 222-272, §Decision 5 receive-side / probe.
- **Evidence:** §Decision 2 line 224: "On probe success, the Coordinator records the probe but does NOT retry the invalidation". §Decision 1 line 144-149: `ProbeAndPill` "On probe timeout, publishes a pill". The probe-then-pill is not atomic.
- **Issue:** Race: Coordinator sends probe at t=5.0s. Member B replies at t=5.249s (just before 250ms timeout). Coordinator's `NextMsg(250ms)` reads at t=5.25s — depending on scheduler, either the probe-success returns first (then no pill) OR the timeout fires first (then pill goes out, then probe-reply arrives milliseconds later when B is already mid-Pill.Trigger). The latter case: B has logged "received probe; replying" and then "received pill; flushing telemetry; os.Exit(125)" — fine. But the Coordinator has retried the invalidation under "B is dead" assumption while B is alive and receives the retry.
- **Why non-blocking:** The retry is N2-of-N2 where N2 excludes B. B receiving a retry message (subscribed to `cache_invalidate.dek.>` until the moment of `os.Exit`) wastes one cache eviction but doesn't cause a correctness violation — B's eviction is a no-op since B is exiting.
- **Required resolution (non-blocking):** Either document the race explicitly in Decision 2's edge-case discussion, OR reframe: "Probe response received within the timeout window cancels the pending pill" — which would require an in-flight cancellation mechanism in `Registry.ProbeAndPill`. Probably not worth the substrate cost; document the benign race instead.

### 5. [Severity: Medium] Bead taxonomy — wiring update to `cmd/holomush/main.go` is implicit, not enumerated

- **Location:** §Bead updates, line 889.
- **Evidence:** "T0 (preflight); T1 (`cluster.Registry` types + `cluster.Pill` interface + Subsystem skeleton, no NATS yet)"
- **Issue:** Skipping a SubsystemCluster registration task. T1 ships the types but not the registration in `cmd/holomush/main.go`. Phase 3c does NOT flip `Crypto.Enabled` (Phase 3d does), but cluster heartbeat is independent of crypto and would presumably be wired *now* so dev/test environments exercise the substrate before 3d's flag flip. Or — alternative — the spec wants cluster/Registry NOT registered until 3d alongside the crypto flag; but that contradicts §Decision 5 line 470: "In 3c, dek caches are constructed only inside test fixtures and integration harnesses" — which is about Coordinator, not Registry. Registry doesn't depend on dek caches, so Registry can be wired now.
- **Required resolution (non-blocking):** Add task `T2.5` or fold into T2: "Register `SubsystemCluster` in `cmd/holomush/main.go` lifecycle wiring; cluster substrate runs from Phase 3c onward in all deployments. `invalidation.Coordinator` registration deferred to Phase 3d alongside Crypto.Enabled flip." Make this distinction explicit in §Decision 5 and Bead taxonomy.

### 6. [Severity: Low] `cluster_id` sourced from `eventbus.Config.GameID` — what about misconfigured shared GameID

- **Location:** §Decision 6 line 670.
- **Evidence:** "Sourcing `cluster_id` from existing `eventbus.Config.GameID` avoids inventing a new config knob. Operators who run multiple HoloMUSH instances on shared NATS already configure distinct GameIDs (the events stream is namespaced by game_id); coordination subjects ride the same namespace."
- **Issue:** If two HoloMUSH instances misconfigure with the same GameID and share a NATS server, they will appear as one cluster. Both will receive each other's heartbeats; both `LiveCount()` will return 2 (each thinks the other is a peer). A `RequestInvalidation` from instance A waits for B's ack; B is a different instance with a *different* `crypto_keys` table and a *different* DEK cache; B will correctly evict the cache key... that doesn't exist in B's cache. Eviction is a no-op, ack returns; A returns success. No correctness violation per se, but operator-confusing.
- **Why non-blocking:** This is operator-error territory and the spec's Decision 6 already acknowledges (line 700): "operator-correct-by-construction in production but defeats local-dev". The cluster-id namespace is a defense-in-depth, not a correctness mechanism.
- **Required resolution (non-blocking):** Add a paragraph to Decision 6 or the out-of-scope list: "Misconfigured shared `GameID` across operationally-distinct HoloMUSH instances is undefined behavior; operators MUST configure distinct GameIDs. Phase 3d's NATS account-level deny rules can include per-instance subject scoping if multi-tenancy on shared NATS becomes a real deployment shape; not Phase 3c scope." (The spec already files X3 for the deny rules; this would be a footnote on X3.)

### 7. [Severity: Low] `Coordinator.Stop` lifecycle ordering relative to `EventBus.Stop` is undefined

- **Location:** §Decision 5 line 471 ("Coordinator is NOT a subsystem in Phase 3c").
- **Evidence:** "Cleaner to leave Coordinator as a constructable type with `Start(ctx)`/`Stop(ctx)` methods that the higher-level wiring invokes."
- **Issue:** EventBus subsystem owns the `*nats.Conn`. If EventBus.Stop drains the conn before Coordinator.Stop unsubscribes, Coordinator's `sub.Drain()` operates on a closed connection (NATS `nats.ErrConnectionClosed`). If Coordinator.Stop runs first, fine. The spec doesn't say which.
- **Why non-blocking:** Phase 3c doesn't wire Coordinator into production. The bug surfaces in Phase 3d when wiring lands. The Phase 3d spec or plan can address.
- **Required resolution (non-blocking):** Add one sentence: "Coordinator.Stop, when wired in Phase 3d, MUST run before EventBus.Stop. The wiring layer is responsible for this ordering; the substrate spec defers explicit subsystem registration to 3d." Or — promote Coordinator to a subsystem in 3c with `DependsOn() []SubsystemID{SubsystemEventBus, SubsystemCluster}`, accepting that it's constructed-but-not-driven until 3d wires its dek cache deps. The first option is cleaner.

## Verification evidence

- **Read:**
  - Spec under review: `docs/superpowers/specs/2026-05-02-event-payload-crypto-phase3c-grounding.md` (full, 936 lines)
  - Master spec: `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` (§§1-9, especially §2 invariants table to confirm INV-52 max, §5.8 cache, §6.1 Add semantics, §6.3 Rekey phases)
  - Phase 3a grounding: `docs/superpowers/specs/2026-05-02-event-payload-crypto-phase3a-grounding.md` (verified no INV-53+)
  - Phase 3b grounding: `docs/superpowers/specs/2026-05-02-event-payload-crypto-phase3b-grounding.md` (Decision 1 substrate-edit table at line 230 confirming TOCTOU framing 3c walks back; Decision 2-3 deferral notes; verified no INV-53+ added)
  - Substrate code: `internal/lifecycle/subsystem.go` (SubsystemID enum, SubsystemEventBus exists, no SubsystemCluster); `internal/eventbus/subsystem.go:276,280` (Conn() and GameID() exported); `internal/eventbus/crypto/dek/manager.go:25-238` (Manager interface, existing Participants stub at 183, unwrapAndCache at 224, cache.Put callsites at 144/237); `internal/eventbus/crypto/dek/cache.go:18-162` (CacheKey shape, no byContext index, Put signature `(key CacheKey, material *Material)`); `internal/eventbus/crypto/dek/store.go:21,90-141,185-215` (ContextID struct, selectActive/selectByID shape, no destroyed_at column); `internal/eventbus/crypto/dek/material.go:6-43` (AsCodecKey signature); `cmd/holomush/main.go:14` (`version = "dev"` is package-private to main)
  - Migration history: `internal/store/migrations/000013_create_crypto_keys.up.sql` (schema confirmed: `rotated_at`, `superseded_by`, no `destroyed_at`); current max migration = 000015
  - Go module: `go.mod:23` confirms `github.com/nats-io/nats.go v1.51.0`
- **Searched:**
  - INV-53..58 in master spec + Phase 3a/3b grounding: confirmed no prior use; Phase 3c is correct to start at 53.
  - `GameID`, `Conn` on `eventbus.Subsystem`: both exported (lines 276/280) — Decision 1's dependency claim verified.
  - `InvalidateContext` signatures across spec: 3 occurrences, two distinct signatures (finding 1 evidence).
  - `old_version` references: receive-side flow assumes it's populated for `participants_changed`, which has no Add()-side meaning (finding 3 evidence).
  - `crypto_keys` schema: `rotated_at TIMESTAMPTZ` exists (master-spec-§4.3); no `destroyed_at` (Decision 4 adds it correctly).
  - `Self()` MemberID on Registry: Decision 1 confirms self counts in `LiveCount()`; finding 4 derives from this.
- **Section coverage** (against design-reviewer required-section pass):
  - Stated problem: present (§Context lines 22-53; eight numbered seams enumerated)
  - Goals: implicit in each Decision; stated as "Phase 3c MUST..."
  - Non-goals: present (§Out of scope, lines 901-923; explicit phase-deferral list)
  - Testable acceptance criteria: present per-Decision and consolidated as INV-53..58 (with finding 5 caveat: missing the load-bearing INV)
  - Named interfaces verified against repo: ContextID ✅, Manager.Participants ✅ (existing stub), Cache shape ✅, lifecycle.Subsystem ✅, eventbus.Conn ✅, GameID ✅; new types (cluster.Registry, cluster.Pill, invalidation.Coordinator, ParticipantsCache) declared but not in repo (correct — substrate is what 3c ships)
- **Testability:** Of 6 new INVs: 4 unit/integration testable as drafted, 1 is "documentary" (finding-1 non-blocking, recoverable as Lint), 1 missing (finding 5 blocking).

## Verdict

- [ ] READY — no blocking findings, planning may proceed
- [x] **NOT READY** — see blocking findings above (5 blocking, 7 non-blocking)

The spec is one round-2 edit pass away from READY. The five blockers cluster around (a) signature/payload consistency between sibling caches and the protocol payload, (b) authority-boundary clarity on pill rate-limit, (c) self-pill failure mode on N=1, and (d) a missing correctness-binding INV that would justify the Phase 3c substrate cost above Phase 3b. Findings 1-4 are 1-3 line edits each; finding 5 is one new invariant declaration. Recommend re-review after the round-2 edits land.

## Persisted report

Absolute path: `/Volumes/Code/github.com/holomush/.worktrees/ojw1.3-phase3c/docs/superpowers/specs/reviews/2026-05-02-phase3c-grounding-design-review.md`
