<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Event Payload Cryptography — Phase 3c Substrate Grounding

## Status

**APPROVED** — resolves eight substrate seams for cross-replica DEK cache invalidation. Modifies the master spec inline at the sections cited below. Companion document, not a replacement: the master spec at [`2026-04-25-event-payload-crypto-design.md`](2026-04-25-event-payload-crypto-design.md) remains authoritative for everything else, the Phase 3a grounding at [`2026-05-02-event-payload-crypto-phase3a-grounding.md`](2026-05-02-event-payload-crypto-phase3a-grounding.md) remains authoritative for the emit-side substrate it closed, and the Phase 3b grounding at [`2026-05-02-event-payload-crypto-phase3b-grounding.md`](2026-05-02-event-payload-crypto-phase3b-grounding.md) remains authoritative for the subscriber-side substrate it closed.

Normative requirements use RFC2119 keywords (MUST, MUST NOT, SHOULD, SHOULD NOT, MAY) per the project's CLAUDE.md convention. Descriptive passages explaining decisions, alternatives considered, and future phases are not normative.

## Authors

- Sean Brandt
- Claude (collaborator)

## Date

2026-05-02

## Context

Phase 3a (epic `holomush-ojw1.1`, PR #3514) shipped the emit-side substrate: codec interface change to `Encode/Decode(ctx, plaintext, key, aad []byte)`, `xchacha20poly1305-v1` codec, `EmitIntent.Sensitive bool`, `dek.Manager.GetOrCreate` wired into `JetStreamPublisher.Publish`, sensitivity fence at `internal/plugin/event_emitter.go::Emit` (INV-6 + INV-7).

Phase 3b (epic `holomush-ojw1.2`, PR #3518) shipped the subscriber-side substrate: `AuthGuard` four-branch decision tree, `Identity` types and constructors, `DecryptAuditEmitter` with bounded queues and backpressure signal, `EventFrame.metadata_only` per-delivery flag, `accesstypes.AccessRequest.Attributes` Action-bag overlay for ABAC, `player_character_bindings` table for INV-10/11 binding entity, `dek.Manager.Participants(keyID, version)` method (uncached PG read on every `AuthGuard.Check`).

Phase 3b explicitly deferred cross-replica DEK cache invalidation to Phase 3c with the following note (Decision 1 substrate-edit table):

> AuthGuard cache policy for Participants — Defer caching to Phase 3c (`holomush-ojw1.3`) — 3b fetches fresh on every Check via the adapter; same staleness contract as `Resolve` itself. INV-28 / INV-29 cache-invalidation rules apply once Phase 3c lands; Phase 3b's adapter calls `Participants` synchronously.

The master spec specifies the invalidation channel at §5.8:

> NATS request-reply on `internal.cache_invalidate.dek.<context>`. Cross-replica; payload is `{context_id, old_key_id, new_key_id, action}`; reply payload is `{replica_id, ack: true}`; sender waits for N-of-N replica acks (INV-28, INV-29).

Three substrate elements the master spec leaves unspecified:

1. **Replica discovery** — what mechanism produces the "N of N expected replicas" number that the sender waits on, and how does it degenerate on single-replica deployments?
2. **Failure remediation** — when a replica fails to ack within the invariant timeout (5s for Rotate/Rekey, 30s for KEK rotation), what protocol step recovers the cluster to a state where the operation can succeed?
3. **Tombstone substrate** — master spec §6.3 Phase 5 step 5.3 mentions a "small tombstone table" populated by replicas on Rekey-driven invalidation, but the table schema, lifecycle, and relationship to the post-Rekey hard delete in Phase 6 are unspecified.

Five additional seams surfaced during Phase 3c grounding work:

4. **Phase 3c scope** — the original Phase 3b deferral note implied participant-set caching would be added in 3c, but Phase 3b's own analysis showed that vN's participant set can be mutated in-place by `Add(participant)` (master spec §6.1, INV-12), making cache invalidation for the participant set non-trivial. Whether 3c ships participant caching alongside DEK material caching shapes the entire decision space.

5. **Package layout** — the eventbus crypto subpackages (`codec/`, `aad/`, `dek/`, `kek/`) are siblings under `internal/eventbus/crypto/`. Cross-replica coordination is not eventbus-specific; future consumers (admin RPCs, future leader election, in-game `@status` command) would benefit from a project-wide registry surface, but eventbus internals also need the cache-invalidation transport.

6. **Subject taxonomy and payload shapes** — the master spec gives one subject pattern and one payload schema; Phase 3c needs an action enum (rotate, rekey, participants_changed, kek_rotation), invalidation seq numbers for observability, and a cluster_id prefix that prevents cross-cluster confusion when an operator points two HoloMUSH instances at the same NATS server.

7. **Pill mechanism interface** — the failure-remediation primitive (option C above) needs to terminate processes in production, write a forensic record before terminating, and let dev/test exercise the protocol without killing the test harness.

8. **Time-drift handling** — multi-host scenarios raise the question of whether the protocol depends on cross-host wall-clock agreement and whether a counter oracle (Lamport, HLC, etc.) is needed.

This grounding doc resolves each seam against the actual Phase 3a + Phase 3b surface, edits the master spec where wording becomes inconsistent, and lists the bead updates needed.

---

## Decision 1 — `cluster.Registry` subsystem with NATS heartbeat membership

**Decision:** Phase 3c MUST introduce a new top-level package `internal/cluster/` containing a `cluster.Registry` interface and concrete implementation. `Registry` is its own `lifecycle.Subsystem` with ID `SubsystemCluster`, depending on `SubsystemEventBus` (needs `eventbus.Subsystem.Conn()`). Membership is maintained via NATS heartbeat publishes on `internal.<cluster_id>.member.alive.<member_id>` at a 5-second cadence; members evict peers from the live set after 3 missed heartbeats (15 seconds total). Graceful shutdown publishes `internal.<cluster_id>.member.bye.<member_id>` once for fast eviction.

**Why a new top-level package and not a subpackage of eventbus:**

The Registry is reframed (per Q1 of the brainstorming pass) as a **project-wide replica health and status surface**, not a private detail of the crypto package. Future consumers — an admin RPC for `@cluster status`, an in-game `@cluster` ops command, future leader-election if it ever lands, a future split-brain detector — depend on Registry without depending on eventbus internals. The Coordinator (Decision 5) is one of many consumers, not the owner. Naming the package `cluster/` rather than `replica/` is honest about what the package coordinates: it's about coordination between members of a cluster, not about what one replica is locally; future non-replica members (e.g., a sidecar gateway process) generalize naturally without renames.

**Interface shape (canonical):**

```go
// internal/cluster/registry.go
package cluster

import (
    "context"
    "time"

    "github.com/holomush/holomush/internal/lifecycle"
)

// MemberID identifies a cluster member. Per-process: each holomush process
// generates a fresh ULID-formatted MemberID at startup. Persistent identity
// is intentionally NOT supported in Phase 3c — restarted processes appear
// as new members; old members get evicted via heartbeat-timeout or graceful
// `bye`. This matches the heartbeat-as-liveness contract; persistent IDs
// would introduce "this member was running yesterday" semantics that nothing
// in 3c needs.
type MemberID string

type MemberStatus int

const (
    StatusUnknown  MemberStatus = iota
    StatusAlive                 // heartbeat fresh within 2× heartbeat interval
    StatusStale                 // 1-2 missed heartbeats; not yet evicted from LiveMembers
    StatusEvicted               // 3+ missed heartbeats or received bye; removed
    StatusPilled                // pilled by a coordinator (Decision 2); removed
)

// Member is the registry's view of a member. SkewSeconds is computed by
// the receiver from the heartbeat's published_at field (Decision 8).
type Member struct {
    ID                  MemberID
    Status              MemberStatus
    StartedAt           time.Time   // sender's wall-clock; observability only
    LastHeartbeatAt     time.Time   // receiver's local clock at last receive
    LastPublishedAt     time.Time   // sender's wall-clock at last heartbeat publish
    HolomushVersion     string
    LastInvalidationSeq uint64      // observability; not used in protocol decisions
    SkewSeconds         float64     // 0 for self; computed for peers per Decision 8
}

// LeaveReason is a closed enum for the OnMemberLeft observer callback.
type LeaveReason int

const (
    LeaveReasonGracefulBye LeaveReason = iota
    LeaveReasonHeartbeatTimeout
    LeaveReasonPilled
)

// Registry is the cluster membership and health surface.
type Registry interface {
    // Lifecycle (called by subsystem orchestrator)
    ID() lifecycle.SubsystemID
    DependsOn() []lifecycle.SubsystemID
    Start(ctx context.Context) error
    Stop(ctx context.Context) error

    // Self returns this process's MemberID. Stamped on heartbeats, on
    // outgoing invalidation requests, and on pills issued by this process.
    Self() MemberID

    // LiveMembers returns a snapshot of currently-live members. O(N)
    // allocation; safe for concurrent use.
    LiveMembers() []Member

    // Member returns the registry's view of a specific member. Returns
    // false if the member is not in the live set.
    Member(id MemberID) (Member, bool)

    // LiveCount returns the size of the live set. O(1) atomic read; used
    // by Coordinator (Decision 5) to compute N before each invalidation
    // publish. Always >= 1 (self counts).
    LiveCount() int

    // ProbeAndPill issues a focused liveness probe to the named member
    // (Decision 2). On probe success, returns nil (member alive, just
    // slow on the invalidation channel). On probe timeout, publishes a
    // pill on internal.<cluster_id>.member.poison.<id>, removes the
    // member from LiveMembers synchronously, and returns ErrPilled.
    ProbeAndPill(ctx context.Context, id MemberID, reason PillReason) error

    // Subscribe registers an observer for membership change events.
    // Observers receive callbacks on join/leave/status-change, in the
    // goroutine that processes the underlying NATS message. Observers
    // MUST NOT block; long work belongs in a separate goroutine.
    Subscribe(observer MemberObserver) (cancel func())
}

// Config parameterizes the Registry. Defaults applied to zero values:
// HeartbeatInterval=5s, EvictAfterMissed=3, ProbeTimeout=250ms,
// PillRateLimit=60s, SkewWarnThreshold=30s. ClusterID is sourced from
// eventbus.Config.GameID by the wiring layer; HolomushVersion is sourced
// from cmd/holomush/main.go's `version` ldflag-set variable, injected via
// dependency at subsystem construction. internal/cluster/ MUST NOT import
// from cmd/holomush/ or introduce its own ldflag variable.
type Config struct {
    ClusterID         string
    HolomushVersion   string
    HeartbeatInterval time.Duration
    EvictAfterMissed  int
    ProbeTimeout      time.Duration
    PillRateLimit     time.Duration
    SkewWarnThreshold time.Duration
}

// MemberObserver is the callback interface for membership change events.
// Future consumers (admin RPC, leader election) implement this to react.
type MemberObserver interface {
    OnMemberJoined(Member)
    OnMemberLeft(MemberID, LeaveReason)
    OnMemberStatusChanged(MemberID, MemberStatus)
}
```

**Lifecycle ordering:**

```text
Start order:
  SubsystemEventBus.Start  (existing)
    └── owns *nats.Conn
  SubsystemCluster.Start   (NEW — depends on SubsystemEventBus)
    ├── publishes first heartbeat on internal.<cluster_id>.member.alive.<self>
    ├── subscribes peer alive/bye/probe/poison subjects
    └── starts heartbeat ticker (5s)

Stop order (reverse):
  SubsystemCluster.Stop
    ├── stops heartbeat ticker
    ├── publishes internal.<cluster_id>.member.bye.<self> once
    └── unsubscribes
  SubsystemEventBus.Stop  (existing)
```

**Self-as-member:** the local process is its own member. `LiveCount()` is always at least 1 because self counts. Single-replica deployments have N=1 and the local member acks its own publishes via NATS's loopback in-process subscribe-on-own-publish semantics. INV-29's "Single-replica deployments degenerate to N=1 (the local replica acks itself); the contract is identical" reads literally — the local member subscribes on the cache_invalidate subject like any peer, processes the eviction, and replies on the inbox.

**Why NATS heartbeat over PG-backed registry:**

| Mechanism | Pros | Cons |
| --- | --- | --- |
| **NATS heartbeat (chosen)** | Operator-visible via `nats sub 'internal.>'` for debugging; latency = NATS RTT (sub-ms in-process); no DB load; works during transient PG outages | Requires NATS auth boundary to enforce subject privacy (Phase 3d) |
| PG-backed registry | Survives NATS partitions; authoritative across DB clusters | DB write per heartbeat; heartbeat tied to DB latency; new migration |
| Inbox semaphore (Q1 option A) | No registry needed | Can't distinguish "no replica responded" from "no replicas exist"; rules out N-of-N as a meaningful contract |
| Static config | Smallest substrate footprint | Operator must update config on every scale event; brittle |

**Why per-process MemberID over persistent ID:**

A restarted process appears as a new MemberID. The old MemberID's heartbeat stops; after 15s it gets evicted naturally or via the bye message published on graceful Stop. Persistent IDs would give operators "this replica has been running through 5 restarts" continuity, but nothing in the protocol uses cross-restart identity. Operators who want continuity look at `holomush_version` + `started_at` fields in the heartbeat payload.

**Why heartbeat at 5s with 3-missed eviction (15s total):**

| Cadence | Eviction window | Cost per minute per member | Operator visibility under failure |
| --- | --- | --- | --- |
| 1s heartbeat / 3s eviction | Fast eviction; tight feedback loop | 60 publishes × N members | Operator sees a hung member in <3s |
| **5s heartbeat / 15s eviction (chosen)** | Balanced | 12 publishes × N members | Operator sees a hung member in <15s; well within human reaction time for a manual `@status` query |
| 10s heartbeat / 30s eviction | Low chatter | 6 publishes × N members | Slow feedback; matches typical health-check cadence in unmanaged ops setups |

5s/15s matches the "5s invalidation timeout" of INV-29 — a member that misses one invalidation is also one heartbeat behind, so the probe-and-pill mechanism (Decision 2) and the heartbeat-eviction mechanism are loosely synchronized. Tighter cadences add NATS chatter without measurable correctness benefit; looser cadences delay operator feedback.

**Why over alternatives:**

- *Coupled registry inside Coordinator package:* makes the registry a private detail. Future consumers (admin RPCs, future split-brain detector) have to either depend on Coordinator's package (wrong direction — they don't care about cache invalidation) or register a duplicate registry. Keeping `cluster.Registry` standalone preserves the project-wide health-surface framing.
- *Inline registry in `eventbus.Subsystem`:* couples cluster membership to eventbus lifetime. If Phase 7 ever introduces a non-eventbus consumer of cluster status (e.g., a control-plane subsystem), it would have to reach into eventbus internals.
- *Persistent MemberID via XDG data dir:* introduces "old self vs new self" disambiguation that nothing in 3c needs. Operators get continuity from `holomush_version` and `started_at` fields. Defer to operator-tooling phase if real demand emerges.
- *Heartbeat over JetStream events stream:* would conflate cluster control-plane traffic with game events traffic. Subject-namespace separation (`internal.>` vs `events.>`) is operator-visible cleanly today; mixing the two would force operators to filter when debugging.

**Cost:** new top-level package `internal/cluster/` (~600 LOC including tests). New subsystem registration in `cmd/holomush/main.go` lifecycle wiring (~5 LOC). New subsystem ID `lifecycle.SubsystemCluster` in `internal/lifecycle/subsystem.go`. Heartbeat traffic: 1 publish per 5s per member on a control-plane subject; total cluster chatter scales as N² (each member receives N-1 peer heartbeats per cycle), which at 5s/N=10 is 20 messages/sec/cluster — negligible.

---

## Decision 2 — Strict-N liveness + active probe + poison pill for missed acks

**Decision:** Phase 3c MUST count N strictly from `Registry.LiveCount()` at the time of each invalidation publish (Q3 of the brainstorming pass; option A). When a replica fails to ack within the invariant timeout (5s for Rotate/Rekey/participants_changed, 30s for KEK rotation per INV-28/INV-29), the Coordinator (Decision 5) MUST issue a focused liveness probe on `internal.<cluster_id>.member.probe.<member_id>` with a 250ms timeout. On probe success, the Coordinator records the probe but does NOT retry the invalidation (the member is alive but slow on the cache_invalidate channel; the original publish was lost or queued behind a slow handler). On probe timeout, the Coordinator publishes a poison pill on `internal.<cluster_id>.member.poison.<member_id>`; the receiving member terminates via `cluster.Pill.Trigger()` (Decision 7); the Registry synchronously evicts the pilled member from `LiveMembers`; the Coordinator recomputes N from the updated registry and retries the invalidation once. Two timeouts (initial + post-pill retry) without success returns `INVALIDATION_PARTIAL_FAILURE` with the missing-member set in the error context for operator escalation.

**Why "strict N from registry" and not "live N at request time":**

| Liveness model | Behavior on transient lapse (replica B GC-paused 8s) | Failure mode interpretation |
| --- | --- | --- |
| **A — Strict (chosen)** | N=registry size at publish; probe-and-pill auto-evicts genuinely dead replicas; transient lapses get pilled (false-positive cost: one process restart) | Self-healing under permanent failures; conservative under transient ones |
| B — Permissive (heartbeat-fresh-only) | N filtered to "heartbeat fresh within 2× cadence"; transient lapse → smaller N → invalidation succeeds → lapsed replica recovers with stale cache | Tolerates transient lapses but creates a hidden stale-cache window — exactly the boundary the protocol exists to forbid |
| C — Reconciled (B + catch-up) | Same N as B; resuming replica detects gap via `last_invalidation_seq` and flushes its caches | Permissive without the stale-cache window; **costs**: per-replica seq + replay protocol + window-tuning |

A is correct; B violates the master spec's correctness guarantee under exactly the scenario the protocol exists to address. C is the right "permissive" answer but adds a protocol surface (replay) that has no operational data justifying its design constants. Strict + probe-and-pill gets us self-healing under permanent failures (auto-evict and retry) and bounded false-positive cost under transient ones (one process restart). Pre-release means we ship A first; if operational data shows transient lapses are common enough that false-positive restarts are disruptive, C lands as a follow-up bead with empirical tuning.

**Probe semantics:**

- Sent on `internal.<cluster_id>.member.probe.<member_id>` with reply-inbox.
- 250ms timeout (configurable via `cluster.Config.ProbeTimeout`).
- Empty request payload; reply payload is `{member_id, last_invalidation_seq}`.
- Probe response updates the registry's view of the member's `LastHeartbeatAt` and `LastInvalidationSeq` — useful even when the probe was unnecessary.
- The probe is **a focused heartbeat-on-demand**, not a separate liveness mechanism. The continuous heartbeat (Decision 1) answers "is this member a registry member?" at coarse cadence; the probe answers "is this *specific* registered member responsive *right now*?" at the moment of ambiguity.

**Pill semantics:**

- Sent on `internal.<cluster_id>.member.poison.<member_id>` (publish-and-forget; no reply expected).
- Payload: `{coordinator_member_id, reason, issued_at}` where `reason` is a closed enum (`missed_invalidation_ack`, `missed_probe_response`, `operator_evict`, `cluster_id_mismatch`).
- Receiving member subscribes only to its own pill subject. Subject naming is the authority boundary: a member that publishes on `internal.<cluster_id>.member.poison.<other_id>` reaches `<other_id>`'s subscriber, but Phase 3d's NATS account-level deny rules will block plugin/character accounts from publishing on `internal.>` wholesale.
- On receive: the member writes a structured log entry, increments `replica_poisoned_total{member_id, reason, source_id}`, flushes telemetry (best-effort, bounded by 1s), and calls `cluster.Pill.Trigger()` (Decision 7).
- `Registry.markPilled(member_id)` removes the member from `LiveMembers` synchronously on the issuing side — **without waiting for natural heartbeat eviction** — so the Coordinator's retry phase sees `LiveCount() == N-1` immediately. This is the only mechanism (besides graceful `bye`) that shrinks membership outside the heartbeat-timeout path.

**Pill rate limit:**

`cluster.Registry.ProbeAndPill` MUST NOT issue more than one pill targeted at the same `(member_id, reason)` within `cluster.Config.PillRateLimit` (default 60s). Rate-limited attempts return `ErrPillRateLimited` and do not reach the wire. This bounds damage from any consumer of `ProbeAndPill` (Coordinator on missed-ack; future operator `@evict-member`; future split-brain detector) that retries pill emission in a loop. **The rate-limit lives on `cluster.Registry` rather than on `invalidation.Coordinator`** because `ProbeAndPill` is the single chokepoint for pill issuance; gating any one consumer would let other consumers bypass it. INV-57 records this as a testable invariant. Coordinator surfaces `ErrPillRateLimited` to its caller as `INVALIDATION_RATE_LIMITED`.

**Single-retry semantics on send-side timeout:**

The Coordinator MUST attempt at most one probe-and-pill + retry cycle per `RequestInvalidation` call. After the second timeout, the call MUST return `INVALIDATION_PARTIAL_FAILURE` with the missing-member set in the error context. Further retry is the caller's choice (Phase 4 Rotate / Phase 5 Rekey). INV-56 records this; the rationale is that an extended retry storm would hide the failure from the operator who initiated the destructive operation.

**Probe-pill race (benign):** the probe-then-pill sequence is not atomic. A probe sent at t=5.0s with a 250ms timeout can have a reply arrive at t=5.249s — depending on goroutine scheduler, either the probe-success returns first (no pill) OR the timeout fires first (pill goes out, then the probe-reply arrives milliseconds later when the target member is already mid-`Pill.Trigger`). The latter case wastes one cache eviction on the dying member but doesn't violate correctness — the dying member's eviction is a no-op and `os.Exit(125)` follows. The Coordinator's retry phase recomputes N from the post-pill registry, so the dead member doesn't affect the retry's success criterion. Documented for forensic clarity; no in-flight cancellation mechanism is added.

**Self-pill prevention:**

The Coordinator's missed-ack handler MUST filter `cluster.Registry.Self()` out of the missing-member set before invoking `ProbeAndPill`. `cluster.Registry.ProbeAndPill` itself MUST also refuse `id == r.Self()` and return `ErrCannotPillSelf` — defense in depth against any future caller that bypasses the Coordinator's filter. INV-60 records this. Rationale: on single-replica deployments (N=1), the local Coordinator publishes the cache-invalidate request and self-receives. If the local invalidation handler hangs (GC pause, slow Mutex contention, ParticipantsCache lock held by an unrelated long-running Get), the 5s timeout fires; without the self-pill filter, the Coordinator probes self via NATS loopback, the same hung handler also fails to respond to the probe within 250ms, and a pill on `internal.<cluster_id>.member.poison.<self>` arrives at the local subscriber, triggering `os.Exit(125)`. The cluster has self-immolated with N=1. Master spec §1 does not list "GC pause longer than 5s" as a threat; the substrate cost of self-induced restart is policy violation, not policy enforcement. The operator-facing failure mode under self-timeout becomes `INVALIDATION_PARTIAL_FAILURE` with a single-member missing set, a structured WARN log, and a `cluster_self_timeout_total` metric increment — the operator investigates the local handler hang rather than the supervisor restarting the process in a loop.

**Why pill authority lives with Registry, not Coordinator:**

`ProbeAndPill` is a cluster-membership concern: "evict member X from the cluster." Coordinator is one consumer; future consumers (operator @evict-member command, future leader-election split-brain resolver, future health-check escalation) reach for the same primitive. Putting the probe-and-pill mechanism in `cluster.Registry` rather than `invalidation.Coordinator` preserves the package boundary: cluster owns membership, invalidation owns cache eviction, the invalidation protocol's failure remediation is just a caller of the cluster's eviction mechanism.

**Why over alternatives:**

- *Probe success counts as ack (Coordinator treats it as if the cache invalidation succeeded):* unsafe — probe success only proves the member's NATS subscriber is alive, not that the member's invalidation handler ran. The cache might still be stale.
- *Pill-without-probe (treat first missed ack as authoritative for eviction):* over-aggressive. A 5s timeout is well above NATS RTT but not above legitimate slow handlers (a handler doing a synchronous PG read on a temporarily-loaded DB might exceed 5s briefly). The probe gives a 250ms second-chance to distinguish "slow handler" from "dead process."
- *Probe-without-pill (probe failure logged but member stays in registry):* silent stale-cache risk identical to liveness model B. Defeats the purpose of strict-N.
- *Multiple retries with exponential backoff:* hides failure from operator. The destructive operations (Rekey, Rotate) should fail fast and let the operator decide whether to retry manually after investigation.
- *Pill triggers `panic()` rather than `os.Exit(125)`:* panic-and-recover is a debugging pattern; production wants explicit termination. `os.Exit(125)` is the conventional supervisor-restart exit code (systemd `Restart=on-failure`, k8s `restartPolicy=Always`, docker `restart=on-failure` all interpret non-zero exit as restart-eligible). See Decision 7 for the DI shape that lets dev/test override.

**Cost:** ~150 LOC for `Registry.ProbeAndPill` implementation + rate-limit + pill-receive handler. Pill-rate-limit storage: in-memory per-Registry map of `(member_id, reason) → last_pill_time`, bounded by member-set size; configured via `cluster.Config.PillRateLimit`. Probe traffic: zero in steady state (probes only fire on missed-ack failure paths).

---

## Decision 3 — Full scope: participants caching shipped alongside DEK material caching

**Decision:** Phase 3c MUST ship participant-set caching (`internal/eventbus/crypto/dek/ParticipantsCache`) alongside the DEK material cache, and MUST wire `dek.Manager.Participants` to use the new cache. The cross-replica invalidation protocol MUST support the `participants_changed` action (Decision 6) so Phase 4's `Add(participant)` mutations propagate before the next `AuthGuard.Check` on any replica.

**Why "Full" over "Lean" (no participants caching):**

The Phase 3b grounding doc deferred participants caching to Phase 3c and noted: "AuthGuard.Check calls dek.Manager.Participants → on permit the subscriber calls dek.Manager.Resolve. Resolve hits the cache; Participants does NOT. If Rotate happens between, AuthGuard checks NEW participants but Resolve returns the (now-stale) cached OLD key." On closer reading during Phase 3c grounding, the TOCTOU described doesn't actually bite: `Resolve(keyID, version)` is version-keyed, and `Rotate` doesn't mutate vN's participants — it inserts vN+1 with new participants. vN's cached key is still correct for vN's participants; AuthGuard reading vN's participants and Resolve returning vN's key are consistent.

The TOCTOU **does** bite under a different mutation: `Add(participant)` (master spec §6.1 + INV-12) mutates vN's participant set in-place. With participants cached on each replica, an `Add` that completes on replica A leaves replica B's cached participant set stale until invalidation propagates. Without cross-replica invalidation, INV-12 ("Add MUST grant immediate read access to all existing DEK history without rotating the DEK") is violated.

Two paths to satisfy INV-12:

| Path | What ships in 3c | Hot-path cost on AuthGuard.Check |
| --- | --- | --- |
| **Lean — no participants caching** | `dek.Manager.Participants` continues to query PG on every Check (Phase 3b's substrate). INV-12 is satisfied because every Check sees fresh PG. | One PG query per delivered sensitive event per recipient — substantial under load |
| **Full (chosen) — participants caching with invalidation** | `dek.Manager.Participants` caches `(ctxType, ctxID, version) → []Participant`. Phase 4's `Add` publishes `participants_changed` invalidation. INV-12 is satisfied via cross-replica invalidation. | One map lookup per Check; PG read only on cache miss |

Pre-release-substrate-correctness argument: the entire Phase 3a/3b/3c arc exists to make encryption invisible to performance. Hot-path participants caching is part of that promise, not scope creep. Adding it later means Phase 4 (`Add` callers) lands without the substrate, plugin event consumers (Phase 7) pay the PG load on every decrypt-permit, and a cross-cutting refactor lands under time pressure when production is already enabled.

**`ParticipantsCache` shape:**

```go
// internal/eventbus/crypto/dek/participants_cache.go (NEW)
package dek

type ParticipantsCacheKey struct {
    ContextType string
    ContextID   string
    Version     uint32
}

// ParticipantsCache holds per-version participant lists with LRU + TTL.
// Symmetric to dek.Cache; separate type because the value shape differs
// ([]Participant vs *Material) and because eviction semantics differ
// (participants invalidate per-version on Add; DEK material invalidates
// per-context on Rekey).
type ParticipantsCache struct {
    cap, ttl, clock, mu, list, byKey, byContext  // same shape as Cache
}

func (c *ParticipantsCache) Get(key ParticipantsCacheKey) ([]Participant, bool)
func (c *ParticipantsCache) Put(key ParticipantsCacheKey, ps []Participant)
func (c *ParticipantsCache) Invalidate(key ParticipantsCacheKey)
func (c *ParticipantsCache) InvalidateContext(ctxID ContextID)
```

**Why version-keyed:** Rotate creates vN+1 with a different participant list while vN's set stays unchanged. Caching `(ctxType, ctxID) → []Participant` (no version) would lose Rotate's per-version semantics: a Check against an event encrypted under vN would see vN+1's participants. Version-keyed pinning (consistent with the existing `dek.Cache` and matching the per-event `(KeyID, Version)` pinning the AAD already provides) keeps each version's participant set independent.

**Why a separate cache type and not a union value type on `dek.Cache`:**

| Approach | Pros | Cons |
| --- | --- | --- |
| **Separate `ParticipantsCache` (chosen)** | Independent eviction; capacity sized per-cache; clear value type at API boundary; coverage clean per cache | Two types to test, two LRU lists |
| Union value type on `dek.Cache` | One LRU list; one capacity to tune | Material entries compete with Participant lists for slots; spec defaults (1024) sized for Material only; ambiguous return type at API boundary |

`dek.Cache`'s capacity (1024) was sized in Phase 2 for Material entries (~32 bytes of unwrapped DEK each). Participant lists are larger and grow with scene size. Mixed semantics would force a re-tuning every time scene-population profiles change.

**`dek.Cache` context reverse index:**

Today `cache.go:62-65` holds `byKey map[CacheKey]*list.Element`. Phase 3c's `InvalidateContext(ctxID)` (used by the `rekey` action) requires evicting every cached `(KeyID, Version)` belonging to a context. The cache currently has no notion of "context" per entry. Phase 3c MUST add:

| Change | Where | Shape |
| --- | --- | --- |
| `cacheEntry` gains `contextID dek.ContextID` field | `internal/eventbus/crypto/dek/cache.go:67-71` | One additional field per entry |
| `Cache` gains `byContext map[dek.ContextID]map[CacheKey]struct{}` reverse index | `internal/eventbus/crypto/dek/cache.go:57-65` | Maintained on Put / LRU-eviction / Invalidate |
| `Cache.Put` signature gains `ctxID ContextID` parameter | Same | Caller is `dek.Manager.unwrapAndCache` (`manager.go:187`) which already has the row's ContextType/ContextID — no upstream API change |
| `Cache.InvalidateContext(ctxID dek.ContextID)` new method | Same file | Walks `byContext[ctxID]`, removes each `(KeyID, Version)` from `byKey` and `list`, deletes the `byContext[ctxID]` entry |
| `Cache.Invalidate(CacheKey)` (existing) | Same | Updated to also remove from reverse index |

The reverse index is `map[ContextID]map[CacheKey]struct{}` rather than `map[ContextID][]CacheKey` — set semantics on the inner gives O(1) removal of a single CacheKey when LRU evicts an entry, instead of O(N) slice scan.

**`dek.Manager.Participants` integration:**

```go
// Phase 3b grounding Decision 1 declared this method; Phase 3b shipped a
// stub that read PG on every call. Phase 3c wires it to use ParticipantsCache.
func (m *manager) Participants(ctx context.Context, keyID codec.KeyID, version uint32) ([]Participant, error) {
    if err := m.configured(); err != nil {
        return nil, err
    }
    // The cache key requires ctxType+ctxID, but callers only have keyID+version.
    // Resolve the (keyID, version) → (ctxType, ctxID) mapping inline since it's
    // invariant. Phase 3c MAY use a tiny secondary cache keyed by (keyID, version)
    // → (ctxType, ctxID); for simplicity, the Phase 3c implementation reads the
    // row's metadata via store.selectByID on cache miss.
    r, err := m.store.selectByID(ctx, keyID, version)
    if err != nil {
        if errors.Is(err, pgx.ErrNoRows) {
            return nil, oops.Code("DEK_NOT_FOUND").With("key_id", uint64(keyID)).With("version", version).Errorf(...)
        }
        return nil, oops.Code("DEK_STORE_SELECT_FAILED").Wrap(err)
    }
    pck := ParticipantsCacheKey{ContextType: r.ContextType, ContextID: r.ContextID, Version: version}
    if ps, ok := m.partCache.Get(pck); ok {
        return ps, nil
    }
    m.partCache.Put(pck, r.Participants)
    return r.Participants, nil
}

// unwrapAndCache (existing, manager.go:187) is updated to also seed
// ParticipantsCache from the row read it already does. This avoids a
// second PG read when the next AuthGuard.Check needs participants.
func (m *manager) unwrapAndCache(ctx context.Context, r row) (codec.Key, error) {
    // ... existing unwrap logic ...
    m.cache.Put(
        CacheKey{KeyID: keyID, Version: r.Version},
        ContextID{Type: r.ContextType, ID: r.ContextID},  // NEW: pass context
        material,
    )
    m.partCache.Put(
        ParticipantsCacheKey{r.ContextType, r.ContextID, r.Version},
        r.Participants,
    )
    return material.AsCodecKey(keyID, r.Version), nil
}
```

**Why seed participants cache during unwrap:** the PG row already contains the participants list (`r.Participants` is read in `selectByID`). Seeding both caches from one PG read is free; the alternative — separate `Participants(...)` calls each doing their own PG SELECT — doubles the read load on the AuthGuard hot path under Crypto.Enabled=true.

**Cost:** new `ParticipantsCache` type (~250 LOC including tests). `dek.Cache` reverse index (~80 LOC; signature change to `Put` ripples to two callers, both inside `manager.go`). `dek.Manager.Participants` body (~30 LOC; existing stub at `manager.go` is replaced). One additional cache instance constructed wherever `dek.Manager` is constructed (Phase 3d's wiring layer).

---

## Decision 4 — Soft-delete via `crypto_keys.destroyed_at` (replaces tombstone table)

**Decision:** Phase 3c MUST add a `destroyed_at TIMESTAMPTZ NULL` column to `crypto_keys`. Production read paths (`selectActive`, `selectByID`) MUST filter `WHERE destroyed_at IS NULL`. Phase 5's Rekey caller MUST replace the master-spec-§6.3-Phase-6 hard `DELETE` with `UPDATE crypto_keys SET destroyed_at = NOW()` performed in the same Rekey transaction. The master-spec-§6.3-Phase-5 "small tombstone table" is **removed from the design** — replaced by cache eviction (via Decision 5's Coordinator) and the soft-delete column.

**Why soft-delete over the master spec's tombstone table:**

The master spec §6.3 Phase 5 step 5.3 describes a tombstone table populated by replicas on Rekey-driven invalidation, used to "fail-closed on subsequent decrypt attempts." Phase 3c grounding analysis of this requirement against the actual rekey lifecycle shows:

1. **Tombstones only matter during the Phase 5 → Phase 6 window.** Between cache-invalidation-acked (Phase 5) and PG-row-deleted (Phase 6), a replica that re-fetches the old DEK from PG would still find it. Tombstones bridge that race.

2. **After Phase 6 hard-delete, fresh PG reads return `NoRows` naturally.** The tombstone is redundant once the row is deleted.

3. **Hard-delete destroys forensic evidence.** Master spec §1 line 80-81 + INV-11 require preserving DEK history for "previous-tenure player_history_read" forensics. Hard-deleting the row destroys evidence of which DEK was rekey'd-when. Soft-delete with `destroyed_at` is forensics-friendly.

4. **Tombstones-as-separate-substrate is two sources of truth that have to stay consistent.** PG is already source of truth for everything DEK-related; tombstones in a sibling table introduce a consistency invariant the substrate has to enforce.

5. **In-memory tombstones need a startup repopulation source.** Replica restart loses in-memory state. Repopulating requires querying "which DEKs were rekey'd?" — but if Phase 6 hard-deleted the row, there's no PG record to query. We'd need an *additional* `rekey_history` table just to reseed in-memory tombstones across restarts.

6. **Crash-recovery is free under soft-delete.** Rekey crashing between Phase 5 and Phase 6 leaves the old DEK row visible to all replicas via the next `selectByID` PG read. With soft-delete, the column is set in the same Rekey transaction as the new DEK row insertion, so partial completion either has both rows (atomic commit) or neither (atomic rollback). No bridging mechanism needed.

**Soft-delete column:**

| Change | Where | Shape |
| --- | --- | --- |
| `destroyed_at TIMESTAMPTZ NULL` column | New migration `internal/store/migrations/00001N_crypto_keys_destroyed_at.up.sql` | Default NULL; non-NULL means "Rekey destroyed this DEK at the recorded time." Indexed via partial index `WHERE destroyed_at IS NULL` for active-row lookup performance |
| `selectActive`, `selectByID` gain `AND destroyed_at IS NULL` | `internal/eventbus/crypto/dek/store.go:96, 123` | Production reads treat destroyed rows as `NoRows` — same fallback semantics as hard-delete |
| `selectAnyByID` (NEW, exported) without the destroyed filter | Same file | Audit/forensic reads can see destroyed rows; no production caller wires this in 3c — Phase 5's Rekey audit emit will use it; future operator forensic tools likewise |
| Migration's `down.sql` drops the column and partial index | Companion `.down.sql` | |

**INV interpretations:**

- **INV-13** ("Rotate preserves the old DEK record unchanged") holds without edit. Rotate does not touch `destroyed_at`.
- **INV-14** ("Rekey re-encrypts historical ciphertext under the new DEK and destroys the old DEK record") wording amended: "destroys" is reinterpreted operationally as `destroyed_at = NOW()`. Production read paths filter `destroyed_at IS NULL` so the operational effect on production is identical; forensic read paths can see the destroyed row.
- **INV-39** ("Reads of historical events whose `dek_ref` no longer exists in `crypto_keys` MUST automatically fall back to the cold tier") holds without edit. Production `selectByID` returns `NoRows` for destroyed rows; the caller's existing fallback path runs unchanged.
- **INV-23** ("Switching crypto providers MUST NOT require re-encrypting payload ciphertext") unaffected — soft-delete doesn't touch `wrapped_dek`.
- **INV-24** ("KEK rotation MUST be performable while the system is live") unaffected.

**Why over alternatives:**

- *In-memory tombstone table per replica:* needs startup repopulation source; can't repopulate after hard-delete; introduces consistency invariant between memory and PG that has to be enforced; fails to preserve forensic evidence after hard-delete.
- *Persistent `tombstone` table in PG (per master spec literal):* two PG tables of truth (`crypto_keys` + `tombstones`); the tombstone-row's lifetime is independent of the crypto_keys-row's lifetime, requiring lifecycle invariants. One column on the existing table is the simpler shape.
- *Defense-in-depth: both PG soft-delete and in-memory tombstone:* tombstone is redundant once the soft-delete column is filtered. The "in-memory tombstone as cache-side fast-path" framing is a microoptimization on a fail-closed path that's only ever traversed during the Phase 5 → Phase 6 window, which is bounded to the Rekey transaction's duration. Not worth the substrate cost.

**Cost:** one new migration (~20 LOC). Two SQL filter additions in store.go (~6 LOC). One new `selectAnyByID` exported method (~25 LOC). Master-spec edit to §6.3 Phase 5 + Phase 6 (Rekey caller logic; lands in Phase 5 PR but the column lands in Phase 3c).

---

## Decision 5 — Package layout: `internal/cluster/` + `internal/eventbus/crypto/invalidation/`; Coordinator is constructable, not a subsystem

**Decision:** Phase 3c MUST split the coordination substrate across two new packages:

- `internal/cluster/` — top-level new package owning `Registry`, `Pill`, member types. Lifecycle subsystem; project-wide health/status surface.
- `internal/eventbus/crypto/invalidation/` — sibling of `dek/`, `kek/`, `codec/`, `aad/`. Owns `Coordinator`. Constructable type, not a subsystem in 3c.

Existing types extended in place:

- `internal/eventbus/crypto/dek/` — gains `ParticipantsCache` (new file) and reverse index on `Cache` (modified file).

**Why split rather than co-locate:**

| Concern | `internal/cluster/` | `internal/eventbus/crypto/invalidation/` |
| --- | --- | --- |
| What it coordinates | Member identity + liveness + eviction | Cache state across members |
| Future consumers | Admin RPCs, future split-brain detector, leader election, `@cluster status` command | Cache invalidation for any future cross-replica cache (settings cache, plugin manifest cache, ...) |
| Imports | `internal/eventbus` (for `Conn`), `internal/lifecycle` | `internal/cluster`, `internal/eventbus/crypto/dek` |
| Imported by | Future admin/control-plane subsystems | EventBus crypto pipeline only (today) |

The invalidation protocol payload is crypto-specific (`{context_id, old_key_id, new_key_id, action}` with the action enum from Decision 6). Other subsystems that one day need cluster-wide invalidation would have their own coordinator with their own payload shape; only the cluster registry is shared. Keeping `cluster/` independent of crypto preserves the right dependency direction.

**Why `Coordinator` is NOT a subsystem in Phase 3c:**

Production wiring of `dek.Manager` (which the Coordinator's receive side evicts) lands in Phase 3d when `Crypto.Enabled` flips. In 3c, dek caches are constructed only inside test fixtures and integration harnesses. Forcing Coordinator into the subsystem graph at 3c would mean the graph has a subsystem whose production dependencies (DEKCache, ParticipantsCache) are not wired. Cleaner to leave Coordinator as a constructable type with `Start(ctx)`/`Stop(ctx)` methods that the higher-level wiring invokes. The 3d wiring layer (alongside DEK pipeline activation) decides whether to promote Coordinator to a subsystem then.

**Coordinator lifecycle ordering (forward note for Phase 3d):** when Phase 3d wires `Coordinator.Start`/`Stop`, the ordering MUST be `Coordinator.Stop` BEFORE `EventBus.Stop`. The EventBus subsystem owns `*nats.Conn`; if `EventBus.Stop` drains the conn first, Coordinator's `sub.Drain()` operates on a closed connection and surfaces `nats.ErrConnectionClosed`. Phase 3d will either (a) promote Coordinator to a `lifecycle.Subsystem` with `DependsOn() []SubsystemID{SubsystemEventBus, SubsystemCluster}` (subsystem orchestrator handles reverse-dependency Stop ordering automatically), or (b) wire Coordinator inside EventBus's `Start` such that EventBus's own `Stop` calls `Coordinator.Stop()` before draining. Phase 3c's substrate doesn't constrain the choice; the requirement is recorded here so the 3d plan inherits it.

**Coordinator construction:**

```go
// internal/eventbus/crypto/invalidation/coordinator.go (NEW)
package invalidation

import (
    "github.com/holomush/holomush/internal/cluster"
    "github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

type Action string

const (
    ActionRotate              Action = "rotate"
    ActionRekey               Action = "rekey"
    ActionParticipantsChanged Action = "participants_changed"
    ActionKEKRotation         Action = "kek_rotation"
)

type Config struct {
    ClusterID         string         // sourced from eventbus.Config.GameID
    InvalidateTimeout time.Duration  // 5s; 30s only when action=ActionKEKRotation
    SeqStart          uint64         // 0 in production; configurable in tests
    // Note: PillRateLimit lives on cluster.Config (Decision 1) — the rate-limit
    // is enforced at cluster.Registry.ProbeAndPill, not at the Coordinator
    // level, so that all consumers of ProbeAndPill are gated uniformly.
}

type Deps struct {
    Conn      *nats.Conn         // from eventbus.Subsystem.Conn()
    Registry  cluster.Registry   // from cluster subsystem
    DEKCache  *dek.Cache         // from dek.Manager construction
    PartCache *dek.ParticipantsCache
    Logger    *slog.Logger
    Metrics   Metrics
}

type Coordinator interface {
    Start(ctx context.Context) error  // subscribes; called by external wiring
    Stop(ctx context.Context) error   // unsubscribes; drains in-flight requests
    RequestInvalidation(ctx context.Context, ctxID dek.ContextID, action Action) error
}

func New(cfg Config, deps Deps) (Coordinator, error)
```

**`RequestInvalidation` typed errors:**

| `oops.Code` | When | What caller does |
| --- | --- | --- |
| `INVALIDATION_TIMEOUT_PRIMARY` | First N-of-N attempt timed out before probe-and-pill phase begins | Substrate-internal — caller never sees this; transitions to probe-and-pill |
| `INVALIDATION_PARTIAL_FAILURE` | Probe-and-pill cycle ran; retry attempt also timed out | Caller (Phase 4 Rotate / Phase 5 Rekey) rolls back per master spec §6.3 step 5.4. Error context contains the missing-member set |
| `INVALIDATION_NO_LIVE_MEMBERS` | `Registry.LiveCount() == 0` | Defense-in-depth — should be impossible since self counts. Caller logs + escalates; substrate bug if hit |
| `INVALIDATION_RATE_LIMITED` | `cluster.Registry.ProbeAndPill` returned `ErrPillRateLimited`; Coordinator surfaces it | Caller waits + retries the operation |
| `INVALIDATION_CROSS_CLUSTER` | The Coordinator received a message whose payload `cluster_id` mismatches its configured cluster | Drop + log + metric; never bubbles to caller |
| `INVALIDATION_SELF_TIMEOUT` | Coordinator's missed-ack set after probe-and-pill phase contains only `cluster.Registry.Self()` (N=1 single-replica with hung local handler) | Caller logs + escalates; `cluster_self_timeout_total` metric increments. Operator investigates local cache or handler hang rather than process being restarted in a loop |

**Send-side flow:**

```text
Coordinator.RequestInvalidation(ctx, ctxID, action):

  1. seq = atomic.AddUint64(&c.seq, 1)
  2. N1 = c.registry.LiveCount()
     if N1 == 0: return INVALIDATION_NO_LIVE_MEMBERS

  3. timeout = if action == ActionKEKRotation then 30s else 5s
  4. inbox = c.conn.NewRespInbox(); sub = c.conn.SubscribeSync(inbox); defer sub.Drain()
  5. payload = {seq, coordinator: c.registry.Self(), ctxType, ctxID,
                action, issued_at, version, successor_version}
     // Per-action population (Decision 6): rotate sets both; rekey sets both;
     // participants_changed sets only `version` (the mutated active version);
     // kek_rotation sets neither.
  6. c.conn.PublishRequest(
       "internal." + cfg.ClusterID + ".cache_invalidate.dek." + ctxType + "." + ctxID,
       inbox, marshal(payload))

  7. Collect acks until len(acks) == N1 or timeout:
       acks = make(map[MemberID]struct{})
       for now() < deadline AND len(acks) < N1:
         msg, err = sub.NextMsg(deadline - now())
         if err == nats.ErrTimeout: break
         if parseErr: log+metric, continue
         acks[parsed.member_id] = struct{}{}

  8. If len(acks) == N1:
       metrics.AcksTotal.WithLabels(action, "success").Inc()
       return nil

  9. // Probe-and-pill phase:
     missing = c.registry.LiveMembers().Filter(m -> _, ok = acks[m.ID]; !ok)
     for member in missing in parallel:
       err = c.registry.ProbeAndPill(ctx, member.ID, PillReasonMissedInvalidationAck)
       // err may be: nil (probe succeeded), ErrPilled (pill issued), RATE_LIMITED

  10. // Retry phase (single retry):
      N2 = c.registry.LiveCount()  // smaller after pills
      if N2 == 0: return INVALIDATION_NO_LIVE_MEMBERS

      [re-publish + re-collect with fresh deadline; same shape as 5-7]

      if len(acks2) == N2:
        metrics.AcksTotal.WithLabels(action, "success_after_retry").Inc()
        return nil

  11. metrics.AcksTotal.WithLabels(action, "partial_failure").Inc()
      return INVALIDATION_PARTIAL_FAILURE.With("missing_members", missing2)
```

**Receive-side flow:**

```text
Coordinator.receiveLoop (started by Start):

  Subscribe "internal." + cfg.ClusterID + ".cache_invalidate.dek.>"
  // No queue group: every member receives every message.

  for msg = sub.Next():
    payload, err = parse(msg)
    if err: log+metric "parse_error"; do NOT ack (sender treats as missed)
    if payload.cluster_id != cfg.ClusterID:
      log.warn "cross-cluster invalidation"
      metrics.CrossClusterMessages.Inc()
      continue  // do NOT ack

    switch payload.action:
      case ActionRekey:
        c.dekCache.InvalidateContext(payload.ctxID)
        c.partCache.InvalidateContext(payload.ctxID)
      case ActionParticipantsChanged:
        c.partCache.Invalidate(ParticipantsCacheKey{
          ContextType: payload.ctxType,
          ContextID:   payload.ctxID,
          Version:     payload.version,  // the mutated active version
        })
      case ActionRotate, ActionKEKRotation:
        // No-op eviction (today's cache shape has no entry these actions
        // make stale); protocol-only ack per INV-29 / INV-28 contract.
      default:
        log.warn "unknown action"; metrics.UnknownActions.Inc()
        continue  // do NOT ack

    atomic.StoreUint64(&c.lastInvalidationSeq, payload.seq)
    metrics.LatencyHistogram.WithLabel(action).Observe(now() - payload.issued_at)
    msg.Respond(marshal({member_id: c.registry.Self(), ack: true}))
```

**Receive-side action eviction summary (settles Section 3.5 of brainstorming):**

| Action | Receive-side eviction | Why kept in protocol |
| --- | --- | --- |
| `rekey` | Both caches: `InvalidateContext(ctxID)` — wipes all versions | Rekey destroys vN entirely; soft-delete + cache flush together implement the fail-closed behavior |
| `participants_changed` | `ParticipantsCache.Invalidate({ctxType, ctxID, version})` only (where `version` is the payload's primary affected version — the active version mutated by Add) | Add() mutates vN participants in-place; INV-12 read-immediacy requires cache reflects new set on this replica before the next AuthGuard.Check |
| `rotate` | **No-op eviction** — protocol acks but no cache change | Rotate does not mutate vN's data; vN+1 isn't cached yet (just created); kept in protocol for INV-29 contract + future-proofing if "active version pointer" caching is ever added |
| `kek_rotation` | **No-op eviction** — protocol acks, no cache change | Today's cache holds unwrapped material; KEK rotation only changes `wrap_provider`/`wrap_key_id` columns which the cache doesn't store. Kept in protocol for INV-28 contract |

The two no-op actions still publish + collect N-of-N acks because **INV-28 and INV-29 are protocol-level contracts**, not cache-effect contracts. The protocol must complete; whether eviction is meaningful is determined by today's cache shape, which can grow in future phases.

**Why over alternatives:**

- *Coordinator inside `internal/cluster/` (one package for all coordination):* couples cluster-membership to crypto-specific payload types. Future non-crypto invalidation consumers would either depend on `cluster/` (which then depends on `dek/` types), or duplicate the protocol. Splitting at the right layer keeps each package narrowly scoped.
- *Coordinator inside `internal/eventbus/` (sibling of `crypto/`):* Coordinator's payload references DEK types; placing it outside `crypto/` while DEK lives inside creates an inconsistent layout. The `invalidation/` package belongs next to the cache types it acts on.
- *Coordinator as its own subsystem in 3c:* would require constructing dek caches in 3c production wiring; that's Phase 3d scope. Forcing it now creates dangling dependencies.
- *Separate subjects per action (`internal.<cluster_id>.cache_invalidate.dek.rotate.<ctx>`, etc.):* receive-side dispatches on action anyway; having one subscription is simpler than four. Operator-readability also favors one subject pattern.

**Cost:** new package `internal/eventbus/crypto/invalidation/` (~400 LOC including tests). Coordinator's send-side and receive-side flows + retry logic + cluster-id filtering (~250 LOC of which). Tests rely on the existing `eventbustest.New(t)` harness pattern + new `clustertest` harness for multi-Registry simulation.

---

## Decision 6 — Subject taxonomy with `cluster_id` namespace and action enum

**Decision:** Phase 3c MUST namespace all coordination subjects under `internal.<cluster_id>.>` where `<cluster_id>` is sourced from existing `eventbus.Config.GameID` (already produced as the events stream prefix at `internal/eventbus/subsystem.go:280`). The cache_invalidation subject pattern carries an action enum in the payload rather than splitting actions into separate subjects.

**Subject taxonomy:**

| Subject | Direction | Payload | Timeout |
| --- | --- | --- | --- |
| `internal.<cluster_id>.member.alive.<member_id>` | Member → all | `{member_id, started_at, holomush_version, last_invalidation_seq, published_at}` | Heartbeat every 5s; member evicted from registry after 3 missed heartbeats (15s) |
| `internal.<cluster_id>.member.bye.<member_id>` | Member → all | `{member_id, reason}` | Published once on graceful Stop |
| `internal.<cluster_id>.member.probe.<member_id>` | Coord → member; reply via inbox | `{}` (empty) → reply `{member_id, last_invalidation_seq}` | 250ms (configurable via `cluster.Config.ProbeTimeout`) |
| `internal.<cluster_id>.member.poison.<member_id>` | Coord → member | `{coordinator_member_id, reason, issued_at}` | Publish-and-forget |
| `internal.<cluster_id>.cache_invalidate.dek.<ctx_type>.<ctx_id>` | Coord → all; replies via inbox | `{seq, coordinator_member_id, ctx_type, ctx_id, action, issued_at, version, successor_version}` → reply `{member_id, ack: true}` | 5s for `rotate`/`rekey`/`participants_changed`; 30s for `kek_rotation` |

**Action enum:**

| Action | Triggered by | Payload `version` | Payload `successor_version` | Cache scope to evict | Tombstone behavior |
| --- | --- | --- | --- | --- | --- |
| `rotate` | Phase 4 `Rotate(context)` | old (deactivated) version | new active version | None (no-op eviction; protocol ack only) | None |
| `rekey` | Phase 5 `Rekey(context)` | destroyed version | replacement version | `(ctxType, ctxID, *)` — all versions across both caches | Soft-delete `crypto_keys.destroyed_at` (Decision 4) set in same Rekey transaction |
| `participants_changed` | Phase 4 `Add(participant)` | mutated active version (the version whose `crypto_keys.participants` JSONB was just updated) | unused (zero) | ParticipantsCache: `(ctxType, ctxID, version)` only | None |
| `kek_rotation` | Phase 6 KEK rotation | unused (zero) | unused (zero) | None (today's cache shape unaffected) | None |

**Per-action payload semantics:** the `version` field is the **primary affected version** — the version whose state is being invalidated. `successor_version` is the **new version** that replaces it (only meaningful for `rotate` and `rekey`). For `participants_changed`, only `version` is populated because Add() mutates the active version in place — there is no "successor." For `kek_rotation`, neither version is meaningful because the action invalidates wrap-provider metadata, not a specific DEK version.

**Why one subject pattern with action enum, not per-action subjects:**

- The receive-side Coordinator dispatches on the `action` field anyway — branching on payload is the same code regardless of subject.
- One subscription on the receive side, not four.
- Subject pattern stays operator-readable: `nats sub 'internal.<cluster_id>.cache_invalidate.>'` shows the entire cache-invalidation traffic; you don't need to know action names to debug.

**`cluster_id` namespace rationale:**

Cross-cluster confusion (operator points two HoloMUSH instances at one NATS server) is structurally impossible when subjects carry the cluster_id prefix AND payloads include a `cluster_id` field that the receiver checks. INV-54 records this as a testable invariant. Without the prefix, two clusters' invalidation traffic would interleave: cluster A's `RequestInvalidation` would be acked by cluster B's members, who would then evict caches they don't own.

Sourcing `cluster_id` from existing `eventbus.Config.GameID` avoids inventing a new config knob. Operators who run multiple HoloMUSH instances on shared NATS already configure distinct GameIDs (the events stream is namespaced by game_id); coordination subjects ride the same namespace.

**Misconfigured shared GameID:** if two operationally-distinct HoloMUSH instances misconfigure with the same GameID and share a NATS server, they will appear as one cluster. Each instance's `LiveCount()` will return 2 (each thinks the other is a peer). A `RequestInvalidation` from instance A waits for instance B's ack; B has a different `crypto_keys` table and a different DEK cache; B will correctly evict the requested cache key (which doesn't exist in B's cache — eviction is a no-op) and ack. No correctness violation per se, but operator-confusing diagnostics. **This is operator error, not a substrate gap**: operators MUST configure distinct GameIDs across operationally-distinct instances. The Phase 3d follow-up bead `holomush-ojw1.3.X3` (NATS account-level deny rules under `internal.>`) is the architectural reinforcement for multi-tenancy on shared NATS if that becomes a real deployment shape; not Phase 3c scope.

**Heartbeat `published_at` field:**

The heartbeat payload's `published_at` field is the sender's wall-clock at publish time. Receivers compute `skew = abs(local_now - published_at - estimated_propagation)` to detect NTP drift between hosts (Decision 8). This is observability-only; no protocol decision uses it.

**Invalidation `seq` field:**

Every Coordinator maintains a monotonic counter, incremented per published invalidation. The seq value:

- Travels in the invalidation payload.
- Heartbeat payload carries the receiving member's `last_invalidation_seq` (highest seq processed = received + evicted + acked).
- Probe response carries the same — value at probe-response time.
- Phase 3c uses these for **observability only** — operator can run `@cluster status` and see "member B is 3 invalidations behind, member C is current."

A future catch-up replay protocol (filed as `holomush-ojw1.3.X2`) would use these seq values for replay window calculation. Phase 3c wires the seq now so the future protocol doesn't need a wire-format edit.

**Pill `reason` closed enum:**

| Reason | When |
| --- | --- |
| `missed_invalidation_ack` | Coordinator's invalidation timeout fired and probe also timed out |
| `missed_probe_response` | Standalone probe (future operator-issued) timed out without invalidation context |
| `operator_evict` | Future operator `@evict-member` admin command (not Phase 3c) |
| `cluster_id_mismatch` | Member published heartbeat under a cluster_id that doesn't match its peers' (defensive — should be impossible if config is consistent) |

Closed enum lets Prometheus labels stay bounded; structured-log entries stay consistent across replicas. INV-55 ties pill receive to `Pill.Trigger(ctx, reason, sourceID)` invocation.

**Why over alternatives:**

- *No cluster_id prefix; rely on operator running separate NATS servers per cluster:* operator-correct-by-construction in production but defeats local-dev (one operator running two clusters on shared NATS for testing). Defense-in-depth at the subject layer is cheap and forgiving.
- *Per-action subjects (`internal.<cluster_id>.cache_invalidate.dek.rotate.<ctx>`):* four subscriptions per coordinator; receive code branches on action regardless; debuggability suffers (operator has to know action names to filter). One subject pattern is simpler.
- *Free-form `reason` strings on pills:* unbounded Prometheus label cardinality; cross-replica reason-string drift; harder to alert on. Closed enum is the right shape.
- *Sequence numbers on the heartbeat payload but not on the invalidation payload:* heartbeat-side seq tells operator-level "member is N behind" but loses per-event ordering. Wiring both costs nothing extra now and unblocks the future replay protocol.

**Cost:** new subject patterns documented in package-level godoc (~50 LOC of comments). Payload Go structs in `internal/cluster/` and `internal/eventbus/crypto/invalidation/` with JSON tags (~80 LOC). Subject construction helpers (~40 LOC). Cluster-id mismatch detection in receive paths (~10 LOC each in cluster + invalidation packages).

---

## Decision 7 — `cluster.Pill` interface with DI for production / test / dev

**Decision:** Phase 3c MUST introduce a `cluster.Pill` interface for process termination. Production wiring uses `os.Exit(125)` with audit telemetry flushed before exit. Test wiring records the pill request on a channel and returns without exiting. Dev wiring panics with a recoverable message that the running `holomush dev` process surfaces in foreground. Three implementations ship; the constructor takes the implementation as a dependency.

**Why DI rather than a single behavior:**

- **Production needs `os.Exit(125)`.** Exit code 125 is the conventional "framework reserved" range that supervisors (systemd `Restart=on-failure`, k8s `restartPolicy=Always`, docker `restart=on-failure`) interpret as restart-eligible. The replica doesn't restart itself — it exits and the supervisor brings it back. This is the only behavior that satisfies INV-55's "production Pill MUST terminate the process."
- **Tests can't use `os.Exit`.** It would kill the test harness. Test wiring records the pill and continues, letting the test assert the pill fired without terminating.
- **Dev needs visible failure.** `holomush dev` runs in foreground and operator wants to see the error. Panic-and-recover surfaces the failure with a stack trace; the dev-mode supervisor (none — operator's terminal) handles re-launch manually.

**Interface shape:**

```go
// internal/cluster/pill.go (NEW)
package cluster

type Pill interface {
    // Trigger flushes audit telemetry then terminates the process. Production
    // implementations MUST NOT return; test implementations MAY return for
    // assertion purposes. Implementations MUST log a structured error entry
    // and increment replica_poisoned_total{member_id, reason, source_id} BEFORE
    // any termination action.
    //
    // ctx is provided so implementations can flush context-bound telemetry
    // (e.g., open spans). Implementations MUST bound flush time (default 1s)
    // since the cluster has already decided this process is done.
    //
    // reason is the closed-enum PillReason from Decision 6.
    // sourceID is the coordinator's MemberID (for forensic record).
    Trigger(ctx context.Context, reason PillReason, sourceID MemberID)
}

// NewProductionPill returns a Pill that exits with code 125 after flushing
// telemetry. Production deployments MUST run under a supervisor that
// interprets exit code 125 as restart-eligible (systemd Restart=on-failure,
// k8s restartPolicy=Always, docker restart=on-failure).
func NewProductionPill(logger *slog.Logger, metrics PillMetrics) Pill

// PillEvent captures a pill-trigger call for test assertion.
type PillEvent struct {
    Reason   PillReason
    SourceID MemberID
    At       time.Time
}

// NewTestPill returns a Pill that records the trigger call on the returned
// channel and does NOT exit. Tests assert via channel receive.
func NewTestPill() (Pill, <-chan PillEvent)

// NewDevPill returns a Pill that panics with a recoverable message. The
// `holomush dev` runtime catches the panic and surfaces it as a foreground
// error; dev operators re-launch manually.
func NewDevPill(logger *slog.Logger) Pill
```

**Why pill ownership lives with Registry, not Coordinator:**

Pill is a cluster-membership concern: "this process is no longer a cluster member; it must terminate." Coordinator is one consumer of the membership-eviction primitive (when a missed-ack triggers a pill); other consumers (operator @evict-member command, future split-brain detection, future health-check escalation) are equally valid. Putting `Pill` in `cluster/` rather than `invalidation/` preserves the right ownership: `cluster.Registry.ProbeAndPill` is the high-level API; `cluster.Pill` is the termination primitive `Registry` invokes when a peer subscribes to its own poison subject and receives a pill message.

**Pill security analysis (Q2 of brainstorming pass):**

The pill subject is `internal.<cluster_id>.member.poison.<member_id>`. Threats and mitigations:

| Threat | Mitigated by NATS auth (Phase 3d) | Mitigated by message signing |
| --- | --- | --- |
| Compromised plugin publishes pill | Yes — `deny_publish ["internal.>"]` on plugin accounts | Yes (redundant under Phase 3d auth) |
| Misconfigured NATS creds | No — auth misconfig defeats account isolation | Yes — signing key isn't in the misconfigured creds |
| Compromised host process | No — host has the publish credentials | Also no — host has the signing key too |
| Network MitM (NATS connection unencrypted) | No — Phase 3d adds TLS | Yes, but TLS is the right answer |
| Replay of an old captured pill | No — NATS doesn't dedup at content level | Yes (with nonce/timestamp) |
| Cross-cluster cross-talk | Subject prefix mostly handles this | Yes (signing key per-cluster) |

**Phase 3c ships four cheaper protections instead of message signing:**

1. **Pill audit trail.** Every pilled member writes a structured log entry (`pill received: from=<source_id> reason=<...> at=<ts>`) AND increments `replica_poisoned_total{member_id, reason, source_id}` BEFORE `os.Exit(125)`. Forensic trail survives the pill.
2. **Pill source reporting.** Pill payload carries `coordinator_member_id`. Receiving member logs and counts by source. A buggy/malicious coordinator becomes operator-visible immediately.
3. **Pill rate-limit.** `cluster.Registry.ProbeAndPill` issues no more than one pill per `(member_id, reason)` per 60s (configurable via `cluster.Config.PillRateLimit`). The rate-limit lives at Registry-level so all consumers of `ProbeAndPill` (Coordinator + future operator @evict-member + future split-brain detector) are gated uniformly. Prevents runaway pill loops from a buggy caller. INV-57 records this.
4. **Cluster ID in subject.** `internal.<cluster_id>.member.poison.<member_id>` instead of `internal.member.poison.<member_id>`. Cross-cluster confusion is structurally impossible.

**Why defer message signing:**

- The threat model that's *uncovered* by Phase 3d's NATS account-level deny rules is "compromised host process," and signing doesn't help there (the attacker has the signing key).
- Signing requires new substrate: cluster signing key + distribution + rotation + key-management. That's an epic's worth of design surface, not 3c scope.
- A wrongly-pilled member is **self-healing**: the supervisor restarts it, and it rejoins the cluster. False-positive cost is bounded.
- Pre-release argument cuts both ways: do the right thing, but only when the threat model justifies the substrate cost. "Compromised plugin pills replicas" isn't currently in master spec §1's documented threats.

**Filed as follow-up bead** (`holomush-ojw1.3.X1`): pill message signing, with the threat-model rationale for deferral preserved in the bead description.

**Why over alternatives:**

- *Single Pill behavior with environment branching (`if env == "production" then os.Exit else log`):* couples the package to environment-detection logic; tests have to manipulate env; impossible to inject a custom test behavior that asserts via channel. DI is the cleaner shape.
- *Pill as a method on Registry (not a separate interface):* ties production-vs-test branching to Registry construction; muddies the boundary. Pill-as-interface keeps Registry's API uniform.
- *Defer pill mechanism to Phase 3d alongside flag flip:* Phase 3d already has a large surface (cold-tier crypto + NATS deny rules + flag flip + E2E). Pill belongs with the strict-N + probe machinery, which is 3c.

**Cost:** new file `internal/cluster/pill.go` (~150 LOC including all three implementations + tests). Constructor wiring in `cmd/holomush/main.go` (~5 LOC) selects production Pill; `internal/dev/` selects DevPill if such a path exists (TBD; Phase 3c may default to ProductionPill and let dev mode surface the exit code).

---

## Decision 8 — No distributed clock primitive; skew detection is observability-only

**Decision:** Phase 3c protocol MUST NOT condition any decision on cross-host wall-clock comparison. Heartbeat eviction, invalidation timeouts, probe/pill timeouts, cache TTL — all use receiver-local-clock or sender-local-clock; skew between hosts is invisible to all decision points. Sequence numbers carry the only cross-host ordering the protocol uses (observability of "members behind on invalidations"). Skew is detected and reported via the `cluster_member_skew_seconds` metric but never enforced. Operators set up NTP; clusters tolerate up to NTP-failure-window of skew without protocol degradation.

**Audit of every time-touching protocol site:**

| Site | Whose clock | Cross-host comparison? | Skew risk |
| --- | --- | --- | --- |
| Heartbeat eviction (member B records "last heard from A at receive_time") | Receiver (B's local) | No — receiver compares its own past observation to its own present | None |
| Invalidation timeout (5s/30s for N-of-N acks) | Coordinator (sender's local) | No — coordinator counts acks against its own deadline | None |
| Probe timeout (250ms) | Coordinator (sender's local) | No | None |
| Cache TTL (5min) | Cache-owner (each replica's local) | No — TTL is per-replica; both replicas use same duration; wall-clock expiry is coherent regardless of skew | None |
| Pill `issued_at` field | Sender's clock | Logged only, never compared | None functional; logs may be confusing under skew |
| Invalidation `issued_at` field | Sender's clock | Logged only, never compared | None functional; logs may be confusing under skew |
| Heartbeat `started_at` field | Sender's clock | Receiver displays as "uptime"; no decision uses it | None functional; status output may show "uptime" wrong under skew |
| Sequence numbers (invalidation seq, last_invalidation_seq) | Per-coordinator monotonic counter | Compared across coordinators only as observability | None — counters, not clocks |

**Skew detection metric:**

The heartbeat payload carries a `published_at` field (sender's wall-clock at publish time). On receive, the registry computes `skew = abs(local_now - published_at - estimated_propagation)`. For in-process NATS, propagation is treated as 0; for cluster NATS in future deployments, the registry maintains a per-source moving average of recent `local_now - published_at` values, and skew is the deviation from that average.

If `skew > 30s` for 3 consecutive heartbeats from the same source, the registry emits `cluster_member_skew_seconds{member_id, source_id}` Prometheus gauge + a structured WARN log. **No protocol behavior changes** based on skew — it's purely operator-visible signal.

INV-58 (no cross-host clock comparison) is enforced by a `gorules/no_remote_clock_compare.go` ruleguard rule (similar to the existing `gorules/dek_no_serialize.go` from INV-27) that fails `task lint` on any subtraction or comparison between a local `time.Time` and one deserialized from a remote-sourced field. The protocol doesn't depend on this invariant for correctness — the skew-detection metric below is the operator-visible signal that catches NTP failures.

**Skew metric carve-out from INV-58:** the skew-detection computation in this Decision intentionally subtracts a remote-sourced `published_at` from a local clock to compute drift. INV-58's prohibition applies to **protocol decisions**, not observability metrics; the skew computation is gated by an explicit `// nolint:no_remote_clock_compare // observability-only per Decision 8` annotation that the ruleguard accepts as the single allowed exception. A future maintainer who extends `cluster.Registry` MUST NOT remove this annotation without re-deriving the metric without remote clocks (e.g., heartbeat sequence drift instead of timestamp drift).

**Why YAGNI on counter oracle / Lamport / HLC:**

- **Counter oracle** (Spanner TrueTime style) — solves "totally ordered events across cluster." We don't need total ordering: invalidations are idempotent on eviction, PG row locks serialize concurrent ops on the same context, and we don't reason about causality between invalidations on different contexts.
- **Lamport clock** — solves "happened-before relation." We don't reason about happened-before; each invalidation stands alone.
- **HLC (Hybrid Logical Clock)** — solves "monotonic-across-hosts under skew." Useful for distributed-DB write ordering. We're not a distributed DB; PG is the source of truth for everything DEK-related, and PG already provides serialization.

The instinct that's worth honoring: **the seq field IS the counter oracle for the only case 3c actually needs it** — observability of "are members keeping up with invalidations." If a future Phase needs replay or causality, extending the seq is the natural shape, not adding a separate clock primitive.

**Why over alternatives:**

- *Lamport clock for invalidation ordering:* not needed because invalidations are idempotent on eviction, and concurrent invalidations on the same context are PG-serialized.
- *HLC for cache TTL coherence across hosts:* not needed because TTL is per-replica safety net (master spec §5.8 "Explicit invalidation is the correctness mechanism; TTL is the safety net"), not a correctness mechanism. Stale-cache-on-skew is bounded by invalidation-on-action.
- *Reject heartbeats with implausible `published_at`:* introduces protocol behavior dependent on skew; turns a benign NTP failure into a cluster outage. No-op + metric is the right shape.

**Cost:** one additional field on heartbeat payload (`published_at`). Skew computation in `Registry.handleHeartbeat` (~30 LOC). One Prometheus gauge. One structured log line.

---

## Master spec edits required

| Master spec section | Current text | Phase 3c edit |
| --- | --- | --- |
| §5.8 Cache table — "Storage location" row | "In-process memory ONLY" | Unchanged. Soft-delete column lives in PG (separate substrate); the cache itself remains in-memory per INV-27. |
| §5.8 Cache table — "Invalidation triggers" row | "KEK rotation, provider migration, Rekey, Rotate" | Add `Add(participant)` (per `participants_changed` action). The previous spec wording in §6.1 that said "Add does NOT publish on the cache-invalidation subject" is the inversion that flips under Decision 3 (Full scope). |
| §5.8 Cache table — "Invalidation channel" row | "NATS request-reply on `internal.cache_invalidate.dek.<context>`" | Subject pattern updated to `internal.<cluster_id>.cache_invalidate.dek.<ctx_type>.<ctx_id>` per Decision 6. Mechanism reference: `invalidation.Coordinator` (Decision 5) orchestrates the protocol; `cluster.Registry.LiveCount()` provides N. |
| §6.1 `Add(participant)` mechanics step 4 | "AuthGuard reads `participants` from PG at decrypt time; participants are NOT cached. So `Add` does NOT publish on the cache-invalidation subject — there is no cached participant list to invalidate." | **Replaced.** Phase 3c caches participants. `Add` MUST publish via `Coordinator.RequestInvalidation(ctx, ctxID, ActionParticipantsChanged)`. The previous "no cached list" rationale no longer holds. |
| §6.2 `Rotate` mechanics step 2 | "Publish `internal.cache_invalidate.dek.<context_id>` (carries: context_id, OLD version, NEW version)" | Reword: "Publish via `invalidation.Coordinator.RequestInvalidation(ctx, ctxID, ActionRotate)`. Receive-side eviction is no-op for today's cache shape (vN's data unchanged; vN+1 not cached yet); kept in protocol per INV-29 contract for future-proofing if 'active version' caching is added." |
| §6.3 Phase 5 step 5.3 — "Tombstone table" | "Marks (context_id, old_key_id) as DESTROYED in a small `tombstone` table to fail-closed on subsequent decrypt attempts" | **Removed.** Tombstones replaced by soft-delete on `crypto_keys.destroyed_at` (Decision 4). Phase 5 receive-side action `rekey` evicts both caches; subsequent reads filter `destroyed_at IS NULL` and return `NoRows` (same fallback path as hard-delete). |
| §6.3 Phase 6 step 6.1 — "DELETE FROM crypto_keys" | `DELETE FROM crypto_keys WHERE id = DEK_old.id` | **Replaced** with `UPDATE crypto_keys SET destroyed_at = NOW() WHERE id = DEK_old.id`. Forensic preservation per INV-11 strengthens. |
| INV-13 ("Rotate preserves the old DEK record unchanged") | As-written | Holds without edit; Rotate does not touch `destroyed_at`. |
| INV-14 ("Rekey re-encrypts historical ciphertext under the new DEK and destroys the old DEK record") | As-written; "destroys" interpreted as hard-delete | Wording amended: "destroys" reinterpreted operationally as `destroyed_at = NOW()`. Production read paths filter `destroyed_at IS NULL` so the operational effect on production is identical; forensic read paths can see the destroyed row via `selectAnyByID`. |
| INV-28 / INV-29 | As-written; describe N-of-N replica acks via NATS request-reply | Wording amended to reference `invalidation.Coordinator` and `cluster.Registry.LiveCount()` as the mechanism providing the "N expected replicas" property. Subject pattern updated. Rollback failure modes unchanged. |
| INV-39 ("dek_ref no longer in crypto_keys → fall back to cold tier") | As-written | Holds without edit; soft-deleted rows are filtered to `NoRows` in production reads, hitting the same fallback path. |
| §11.1 Phase 3 row | Add reference to this grounding doc. | Note that the cluster-coordination substrate (Decision 1) lands in 3c as a prerequisite to safe operation under multi-replica deployments. |

### New invariants Phase 3c introduces

To be added to master spec §2 as a new "Cluster coordination invariants" subsection (between "Cache invariants" and "Provider implementation invariants"):

| Inv | Statement | Test class |
| --- | --- | --- |
| **INV-53** | Every member of a `cluster.Registry` MUST have a unique `MemberID`; concurrent registration with a colliding MemberID MUST be rejected with `CLUSTER_MEMBER_DUPLICATE_ID`. | Unit |
| **INV-54** | All Phase 3c internal coordination subjects (`internal.<cluster_id>.member.*`, `internal.<cluster_id>.cache_invalidate.*`) MUST be prefixed with `<cluster_id>`. Members MUST drop messages whose payload `cluster_id` field disagrees with their configured cluster_id. | Integration |
| **INV-55** | A pill received on `internal.<cluster_id>.member.poison.<self_id>` MUST cause the receiving member to call `Pill.Trigger(ctx, reason, sourceID)` after flushing audit telemetry; production `Pill` impl MUST terminate the process with exit code 125. | Unit (with TestPill) + e2e (with ProductionPill in supervised harness) |
| **INV-56** | The `invalidation.Coordinator` MUST attempt at most one probe-and-pill + retry cycle per `RequestInvalidation` call. After the second timeout, the call MUST return `INVALIDATION_PARTIAL_FAILURE` carrying the missing-member set; further retry is the caller's choice. | Unit |
| **INV-57** | `cluster.Registry.ProbeAndPill` MUST NOT issue more than one pill targeted at the same `(member_id, reason)` within `cluster.Config.PillRateLimit` (default 60s). Rate-limited attempts MUST return `ErrPillRateLimited` and not reach the wire. The rate-limit gates ALL consumers of `ProbeAndPill`, not only Coordinator. | Unit |
| **INV-58** | The Phase 3c protocol MUST NOT condition any decision on cross-host wall-clock comparison. Enforced by a `gorules/no_remote_clock_compare.go` ruleguard that fails `task lint` on any subtraction or comparison where one operand is a `time.Time` deserialized from a remote-sourced field (heartbeat `published_at`, invalidation `issued_at`, pill `issued_at`, member `started_at`). The skew-detection metric in Decision 8 is the single allowed exception, gated by a ruleguard `// nolint:no_remote_clock_compare // observability-only per Decision 8` annotation. | Lint |
| **INV-59** | A successful `Coordinator.RequestInvalidation(ctx, ctxID, ActionParticipantsChanged)` MUST result in every other live member's `dek.ParticipantsCache` having no entry for `(ctxType, ctxID, version)` upon return. Equivalently: after `RequestInvalidation` returns nil for `participants_changed`, every other replica's next `dek.Manager.Participants(keyID, version)` call for that `(ctxID, version)` MUST re-fetch from PG. This is the correctness substrate that supports master spec INV-12 ("Add MUST grant immediate read access to all existing DEK history without rotating the DEK") under Decision 3's Full-scope participant caching. Phase 4's `Add(participant)` caller invokes the substrate; Phase 3c ships the substrate property and tests it via the multi-Registry harness without a production Add caller. | Integration |
| **INV-60** | `cluster.Registry.ProbeAndPill(ctx, id, reason)` MUST refuse `id == r.Self()` and return `ErrCannotPillSelf` without issuing a probe or pill. Coordinator's missed-ack handler MUST filter `r.Self()` out of the missing-member set before calling `ProbeAndPill`. On single-replica deployments (N=1), this prevents the local Coordinator from self-pilling when the local invalidation handler hangs; the operator-facing failure mode becomes `INVALIDATION_PARTIAL_FAILURE` with a single-member `missing` set, surfaced as a structured WARN log + `cluster_self_timeout_total` metric increment. | Unit |

INV-15, INV-19, INV-21, INV-25, INV-27 (in-memory-only cache storage) all unaffected. Decision 3's participants caching is consistent with INV-27 — both `dek.Cache` and `dek.ParticipantsCache` are in-memory and never persisted.

---

## Bead updates required

| Bead | Change |
| --- | --- |
| `holomush-ojw1.3` (Phase 3c sub-epic) | Description: add Decisions 1-8 from this grounding doc as substrate prerequisites. Add reference to this grounding doc as the design source-of-truth. The previous description's "DEK cache (LRU, configurable size + TTL)" and "Request-reply cache-invalidation protocol" are subsumed; the description should reference Phase 3c's Full scope (DEK material caching + participants caching) and the cluster registry substrate. |
| `holomush-ojw1.3.T0` ... `T15` (sub-tasks; numbering TBD by writing-plans) | Anticipated decomposition. Consumes plan-reviewer Round 1 findings + this grounding doc. Task list preview: **T0** (preflight); **T1** (`cluster.Registry` types + `cluster.Pill` interface + Subsystem skeleton, no NATS yet); **T2** (`cluster.Registry` NATS heartbeat publish + receive + alive/bye); **T2.5** (register `SubsystemCluster` in `cmd/holomush/main.go` lifecycle wiring; cluster substrate runs from Phase 3c onward in all deployments — `invalidation.Coordinator` registration deferred to Phase 3d alongside `Crypto.Enabled` flip); **T3** (`cluster.Registry.ProbeAndPill` + rate limit + self-pill prevention per INV-60); **T4** (`dek.Cache` context reverse index + `InvalidateContext` + `Cache.Put` signature edit); **T5** (`dek.ParticipantsCache` new type + tests); **T6** (`dek.Manager.Participants` method body using ParticipantsCache); **T7** (`crypto_keys.destroyed_at` migration + filter on selectActive/selectByID + new `selectAnyByID`); **T8** (`invalidation.Coordinator` types + send-side `RequestInvalidation` + N-of-N collection + missed-ack set self-filter per INV-60); **T9** (`invalidation.Coordinator` receive-side + action dispatch + ack); **T10** (skew detection metric + heartbeat `published_at` field + `gorules/no_remote_clock_compare.go` ruleguard for INV-58); **T11** (integration tests: multi-Registry harness; INV-28/29/53-60); **T12** (master spec edits in-PR); **T13** (meta-test enforcing INV-53..60 ↔ test-name binding); **T14** (file follow-up beads X1-X5 with rationale text from §Bead updates); **T15** (godoc + package-level documentation comments for `internal/cluster/` and `internal/eventbus/crypto/invalidation/`). |
| `holomush-ojw1.3.X1` (follow-up; not in 3c PR) | NEW. "Pill message signing." Description records the threat-model rationale for deferral; revisit if compromised-host or replay threat materializes. |
| `holomush-ojw1.3.X2` (follow-up; not in 3c PR) | NEW. "Catch-up replay protocol via `last_invalidation_seq`." Description records the operational-data prerequisite for tuning replay window correctly. |
| `holomush-ojw1.3.X3` (follow-up; not in 3c PR) | NEW. "NATS account-level deny rules for `internal.>`." Description notes this lands in Phase 3d's PR alongside the `audit.>` deny rules for atomicity. Cross-references `holomush-ojw1.4`. |
| `holomush-ojw1.3.X4` (follow-up; not in 3c PR) | NEW. "Cluster admin RPCs (`@cluster status`, `@evict-member`)." Operator UX, not protocol; depends on the admin-RPC framework that lands separately. |
| `holomush-ojw1.3.X5` (follow-up; not in 3c PR) | NEW. "Cluster operations site documentation." Phase 8 docs. |
| `holomush-ojw1.4` (Phase 3d epic) | Description: confirm 3d ships `internal.>` NATS deny rules alongside the existing `audit.>` deny rules; `holomush-ojw1.3.X3` is the cross-reference. No edit beyond this confirmation. |

The Phase 3c plan (`docs/superpowers/plans/2026-05-02-event-payload-crypto-phase3c-...`) is to be written against this grounding by the next `superpowers:writing-plans` pass, after design-reviewer approval.

---

## Out of scope

Phase 3c does NOT address:

- **Production wiring of `dek.Manager` into the emit/decrypt path** — Phase 3d (alongside `Crypto.Enabled` flag flip).
- **Rotate caller** (wizard-transfer triggers, scene-policy, scheduled rotation, participant-removal trigger) — Phase 4 (`holomush-fi0n`).
- **`Add(participant)` caller** (scene join, channel invite, DM auto-create on first message, character-private context creation) — Phase 4.
- **Rekey CLI tool + UNIX admin socket** — Phase 5 (`holomush-jxo8`).
- **KEK rotation caller** — Phase 6.
- **`AdminReadStream` / operator break-glass** — Phase 5.
- **Cold-tier `QueryStreamHistory` crypto path** — Phase 3d.
- **NATS deny rules for `internal.>`** — Phase 3d (filed as `holomush-ojw1.3.X3` follow-up; lands alongside `audit.>` deny rules for atomicity).
- **Pill message signing** — filed as `holomush-ojw1.3.X1` follow-up; defer pending real threat-model evolution.
- **Catch-up replay protocol** — filed as `holomush-ojw1.3.X2` follow-up; defer pending operational data on benign lapse frequency.
- **Cluster ops admin RPCs (`@cluster status`, `@evict-member`)** — filed as `holomush-ojw1.3.X4` follow-up; depends on admin-RPC framework.
- **Cluster ops site docs** — Phase 8 / `holomush-ojw1.3.X5` follow-up.
- **Multi-process production deployment manifests** (k8s, systemd unit files for clustered HoloMUSH) — Phase 8 ops docs.
- **Vault provider** — Phase 6.
- **Plugin SDK helpers and plugin-owned audit (`PluginAuditService.QueryHistory`)** — Phase 7.
- **Wire-side `Sensitive` surfacing on Lua + binary SDK (`holomush-ojw1.4.1`)** — independent track.
- **Persistent MemberID across restarts** — defer to operator-tooling phase if real demand emerges; not load-bearing for any 3c invariant.
- **Tombstone table per master spec §6.3 literal text** — explicitly removed from the design (Decision 4); soft-delete column replaces it.

---

## References

- Master spec: [`2026-04-25-event-payload-crypto-design.md`](2026-04-25-event-payload-crypto-design.md)
- Phase 3a grounding: [`2026-05-02-event-payload-crypto-phase3a-grounding.md`](2026-05-02-event-payload-crypto-phase3a-grounding.md)
- Phase 3a plan (executed, PR #3514): [`../plans/2026-05-02-event-payload-crypto-phase3a-codec-emit.md`](../plans/2026-05-02-event-payload-crypto-phase3a-codec-emit.md)
- Phase 3b grounding: [`2026-05-02-event-payload-crypto-phase3b-grounding.md`](2026-05-02-event-payload-crypto-phase3b-grounding.md)
- Phase 3b plan (executed, PR #3518): [`../plans/2026-05-02-event-payload-crypto-phase3b-authguard-decrypt.md`](../plans/2026-05-02-event-payload-crypto-phase3b-authguard-decrypt.md)
- JetStream substrate spec: [`2026-04-18-jetstream-event-log-design.md`](2026-04-18-jetstream-event-log-design.md)
- HoloMUSH lifecycle subsystem framework: `internal/lifecycle/subsystem.go`
- EventBus subsystem (transport for cluster + invalidation): `internal/eventbus/subsystem.go`
- DEK substrate (extended by Decisions 3 + 4): `internal/eventbus/crypto/dek/`
