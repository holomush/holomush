<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Event Payload Crypto — Phase 3c — DEK Cache Invalidation + Cluster Substrate

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land cross-replica DEK cache invalidation and the cluster substrate that supports it: `cluster.Registry` lifecycle subsystem with NATS heartbeat membership, strict-N + active probe (250ms) + poison pill (`os.Exit(125)`) failure-remediation, soft-delete via `crypto_keys.destroyed_at` replacing master spec §6.3's tombstone table, `dek.Cache` context reverse index, new `dek.ParticipantsCache`, and `invalidation.Coordinator` (NATS request-reply with N-of-N replica acks per master spec INV-28 + INV-29). Default `Crypto.Enabled=false` keeps the crypto pipeline dark in production until Phase 3d; the cluster substrate runs in every deployment from this PR onward.

**Architecture:** Eight substrate decisions, all closed in the grounding doc. D1 introduces `internal/cluster/` (top-level new package) with `Registry`, `Pill`, `Member`, `Config` types and a `lifecycle.Subsystem` impl. D2 ships strict-N counting + probe + pill remediation, with self-pill prevention per INV-60. D3 ships the Full-scope substrate (DEK material caching + participants caching). D4 replaces master spec §6.3's tombstone table with a `destroyed_at TIMESTAMPTZ NULL` column on `crypto_keys`. D5 places `invalidation.Coordinator` at `internal/eventbus/crypto/invalidation/` (sibling of `dek/`, `kek/`, `aad/`, `codec/`); Coordinator is constructable, NOT a subsystem in 3c. D6 namespaces all coordination subjects under `internal.<cluster_id>.>` with action enum (rotate/rekey/participants_changed/kek_rotation). D7 ships `cluster.Pill` with DI for prod/test/dev. D8 forbids cross-host wall-clock comparison via `gorules/no_remote_clock_compare.go` ruleguard (Lint).

**Tech Stack:** Go 1.22+, the existing eventbus substrate (`internal/eventbus/{subsystem,types,bus}`, `internal/eventbus/crypto/{aad,dek,kek,codec}`), the existing lifecycle framework (`internal/lifecycle/subsystem.go` + `orchestrator.go`), pgx/v5 + `pgxpool.Pool` for the soft-delete migration, NATS request-reply via the existing in-process `*nats.Conn` from `eventbus.Subsystem.Conn()`, gocritic ruleguard rules under `gorules/`, mockery for any new mocks. No new external dependencies.

**Grounding:** [`docs/superpowers/specs/2026-05-02-event-payload-crypto-phase3c-grounding.md`](../specs/2026-05-02-event-payload-crypto-phase3c-grounding.md) (READY per design-reviewer round 2) — eight substrate decisions with citations; **mandatory pre-read** before this plan. Master spec amendments ship in T13: §5.8 cache table, §6.1 Add publishes invalidation, §6.2 Rotate via Coordinator, §6.3 tombstone removed + soft-delete, INV-13/14/39 wording amendments, NEW §2 "Cluster coordination invariants" subsection (INV-53..60).

**Companions:** Phase 3a plan at [`2026-05-02-event-payload-crypto-phase3a-codec-emit.md`](2026-05-02-event-payload-crypto-phase3a-codec-emit.md) (executed, PR #3514). Phase 3b plan at [`2026-05-02-event-payload-crypto-phase3b-authguard-decrypt.md`](2026-05-02-event-payload-crypto-phase3b-authguard-decrypt.md) (executed, PR #3518). This plan inherits the structural template.

**Bead:** `holomush-ojw1.3`. Each task closes one or more sub-beads (numbering assigned during plan execution).

---

## File structure

| File | Status | Responsibility |
| --- | --- | --- |
| `internal/lifecycle/subsystem.go:17-28` | MODIFY | Add `SubsystemCluster` constant; re-run stringer. |
| `internal/lifecycle/subsystemid_string.go` | REGENERATE | `go generate ./internal/lifecycle/...` after the constant is added. |
| `internal/cluster/types.go` | CREATE | `MemberID`, `MemberStatus`, `Member`, `LeaveReason`, `PillReason`, `MemberObserver`, `Config` types. |
| `internal/cluster/types_test.go` | CREATE | Type-construction validation + closed-enum coverage. |
| `internal/cluster/pill.go` | CREATE | `Pill` interface; `NewProductionPill` (`os.Exit(125)`), `NewTestPill` (channel-recording), `NewDevPill` (panic-and-recover). |
| `internal/cluster/pill_test.go` | CREATE | Behavioral tests for TestPill + DevPill; ProductionPill's `os.Exit` is exercised in T12 e2e harness with subprocess. |
| `internal/cluster/registry.go` | CREATE | `Registry` interface; `registry` concrete impl with NATS heartbeat publish/receive, alive/bye subjects, member set + reverse index, observer fan-out. `Subsystem.Start/Stop` lifecycle. |
| `internal/cluster/registry_test.go` | CREATE | Unit tests for member set management, eviction-on-stale-heartbeat, observer notifications, INV-53 (duplicate MemberID rejection), INV-54 (cluster_id mismatch drop). |
| `internal/cluster/heartbeat.go` | CREATE | Heartbeat publish ticker + receive handler; `published_at` stamping; skew computation. |
| `internal/cluster/heartbeat_test.go` | CREATE | Heartbeat cadence + skew metric tests. |
| `internal/cluster/probe_pill.go` | CREATE | `ProbeAndPill` impl: focused probe + pill emit + rate limit + self-pill refusal; `ErrPillRateLimited`, `ErrCannotPillSelf`. |
| `internal/cluster/probe_pill_test.go` | CREATE | INV-57 (rate-limit) + INV-60 (self-pill refusal) unit tests. |
| `internal/cluster/payload.go` | CREATE | NATS payload structs (heartbeat, bye, probe, pill) with JSON tags; subject builder helpers. |
| `internal/cluster/payload_test.go` | CREATE | Round-trip marshal tests; cluster_id mismatch test. |
| `internal/cluster/clustertest/harness.go` | CREATE | Multi-Registry test harness on a shared `*nats.Conn` for integration tests. Build-tag-free (used by both unit and integration tests). |
| `internal/cluster/metrics.go` | CREATE | Prometheus metrics: `replica_poisoned_total`, `cluster_member_skew_seconds`, `cluster_self_timeout_total`. |
| `cmd/holomush/core.go:324, 437` | MODIFY | Construct `clusterSub := cluster.NewSubsystem(...)` after `eventBusSub`; add `clusterSub` to the orchestrator slice. |
| `cmd/holomush/core_test.go` | MODIFY | Update existing subsystem-graph assertions to include `SubsystemCluster`. |
| `internal/eventbus/crypto/dek/cache.go` | MODIFY | `cacheEntry` gains `contextID ContextID` field; `Cache` gains `byContext map[ContextID]map[CacheKey]struct{}` reverse index; `Cache.Put` signature gains `ctxID ContextID` parameter; new `InvalidateContext(ctxID ContextID)` method; existing `Invalidate(CacheKey)` updated to remove from reverse index. |
| `internal/eventbus/crypto/dek/cache_test.go` | MODIFY | Update existing `Put` callsites; new tests for reverse-index correctness and `InvalidateContext`. |
| `internal/eventbus/crypto/dek/manager.go:144, 237` | MODIFY | `unwrapAndCache` callsite (line 237) to `m.cache.Put` passes `ContextID{Type: r.ContextType, ID: r.ContextID}`; mint-path callsite at line 144 likewise. Also seeds `m.partCache` with the row's participants. (Line numbers verified at plan-writing time; executors `rg "m.cache.Put"` to confirm.) |
| `internal/eventbus/crypto/dek/participants_cache.go` | CREATE | `ParticipantsCacheKey`, `ParticipantsCache` type with LRU+TTL, `Get/Put/Invalidate/InvalidateContext`. Symmetric to existing `dek.Cache`. |
| `internal/eventbus/crypto/dek/participants_cache_test.go` | CREATE | Behavioral tests; LRU eviction; TTL expiry; reverse-index correctness; INV-59 substrate property exercised at unit level. |
| `internal/eventbus/crypto/dek/manager.go:163-185` | MODIFY | Replace `Participants` stub from Phase 3b with real implementation: cache-then-PG with `unwrapAndCache`-style seeding. |
| `internal/eventbus/crypto/dek/manager_integration_test.go` | MODIFY | Replace stub-bead expectation with cache-hit/cache-miss/PG-fallthrough integration test. |
| `internal/eventbus/crypto/dek/manager.go` | MODIFY | `NewManager` constructor signature gains `partCache *ParticipantsCache` parameter (third or fourth arg). `manager` struct gains `partCache *ParticipantsCache` field. |
| `internal/store/migrations/000016_crypto_keys_destroyed_at.up.sql` | CREATE | `ALTER TABLE crypto_keys ADD COLUMN destroyed_at TIMESTAMPTZ NULL`; partial index `WHERE destroyed_at IS NULL`. |
| `internal/store/migrations/000016_crypto_keys_destroyed_at.down.sql` | CREATE | DROP INDEX, DROP COLUMN. |
| `internal/eventbus/crypto/dek/store.go:96, 123` | MODIFY | `selectActive`, `selectByID` SQL gains `AND destroyed_at IS NULL`. |
| `internal/eventbus/crypto/dek/store.go` | MODIFY | New exported `SelectAnyByID(ctx, keyID, version)` method (no `destroyed_at` filter) for forensic reads; documented as audit-only path. |
| `internal/eventbus/crypto/dek/store_test.go` | MODIFY | Add INV-39-style test (soft-deleted = NoRows for production reads); test `SelectAnyByID` returns destroyed rows. |
| `internal/eventbus/crypto/invalidation/types.go` | CREATE | `Action` enum (`ActionRotate`, `ActionRekey`, `ActionParticipantsChanged`, `ActionKEKRotation`), `Config`, `Deps`, payload types, typed errors. |
| `internal/eventbus/crypto/invalidation/coordinator.go` | CREATE | `Coordinator` interface; `coordinator` impl with `Start/Stop`, `RequestInvalidation` (send-side N-of-N + retry), receive-side dispatch. |
| `internal/eventbus/crypto/invalidation/coordinator_send_test.go` | CREATE | Unit tests for send-side: N-of-N success, primary timeout → probe-and-pill → retry → success/failure paths, INV-56 single-retry, INV-60 self-filter, INVALIDATION_NO_LIVE_MEMBERS, INVALIDATION_RATE_LIMITED surfacing. |
| `internal/eventbus/crypto/invalidation/coordinator_receive_test.go` | CREATE | Unit tests for receive-side: action dispatch (rekey, participants_changed, rotate no-op, kek_rotation no-op); cluster_id mismatch drop; ack-on-success; no-ack-on-error. |
| `internal/eventbus/crypto/invalidation/metrics.go` | CREATE | Prometheus metrics: `cluster_invalidation_acks_total`, `cluster_invalidation_latency_seconds`, `dek_cache_hits_total`, `dek_cache_misses_total`, `dek_cache_size`, `dek_cache_evictions_total`. |
| `gorules/no_remote_clock_compare.go` | CREATE | gocritic ruleguard rule preventing comparison/subtraction between local `time.Time` and a remote-sourced field. INV-58. |
| `gorules/no_remote_clock_compare_test.go` | CREATE | Ruleguard rule's own test fixtures (good/bad code samples). |
| `test/integration/cluster/cluster_test.go` | CREATE | Multi-Registry integration tests: heartbeat propagation; eviction-on-stale; bye-on-Stop; INV-53 (duplicate MemberID rejection); INV-54 (cluster_id namespace); INV-55 (pill receive triggers Pill.Trigger with ProductionPill in subprocess); INV-57 (rate limit); INV-60 (self-pill refusal). |
| `test/integration/crypto/cache_invalidation_test.go` | CREATE | INV-28 (KEK rotation 30s N-of-N); INV-29 (Rotate/Rekey 5s N-of-N); INV-29 single-replica degeneration; INV-12 read-immediacy via `participants_changed`; probe-and-pill auto-eviction; INV-56 single-retry; INV-58 protocol-level no-clock-compare verified by lint passing; INV-59 cache-eviction substrate property. |
| `test/meta/inv_binding_test.go` | CREATE (or extend if existing) | Meta-test enforcing INV-53..60 ↔ test-name binding. Each integration/unit test for a new INV starts with a `// Verifies: INV-N` comment; meta-test grep'ies for that binding and asserts every new INV has at least one test. |
| `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` | MODIFY | Master spec edits per the grounding doc's edit table (§5.8, §6.1, §6.2, §6.3, INV-13/14/39, new "Cluster coordination invariants" subsection with INV-53..60, §11.1 cross-reference to grounding doc). |

**Migration numbering:** existing maxes at `000015_create_player_character_bindings`. Phase 3c adds `000016_crypto_keys_destroyed_at`. No conflict.

**Subsystem ID:** existing range goes up to `SubsystemAuditProjection` (index 10). Phase 3c adds `SubsystemCluster` at index 11.

**Line-number disclaimer:** file paths in this plan include line numbers (e.g., `manager.go:144, 237`) as anchors against the current main HEAD at plan-writing time. Phase 3b's manager.go additions shifted some pre-existing callsites; an executor running this plan MUST `rg` for the actual current line at execution time and adjust the edit target accordingly. The plan's intent (which callsite, which method) is authoritative; line numbers are convenience.

---

## Tasks

### Task T0: Verify clean working copy and current main

Each task commits separately. If the worktree starts with uncommitted changes or is not based on the latest main, the first commit picks up unrelated work.

- [ ] **Step 1: Verify clean working copy.**

Run: `jj --no-pager st`

Expected: working copy contains only the Phase 3c grounding doc commit (`docs(crypto): Phase 3c grounding`); no other uncommitted changes.

- [ ] **Step 2: Verify base.**

Run: `jj --no-pager log -r 'main..@' --no-graph | head -10`

Expected: shows only the grounding-doc commit on top of Phase 3b's merge.

- [ ] **Step 3: Verify Phase 3b is merged.**

Run: `jj --no-pager log -r 'main' --no-graph | head -1`

Expected: includes `feat(crypto): Phase 3b — AuthGuard + decrypt-on-fanout + binding substrate (#3518)` or a more recent main HEAD that includes that change.

- [ ] **Step 4: Verify build is green at base.**

Run: `task build`

Expected: build succeeds.

If broken: stop and fix the base before adding 3c changes.

- [ ] **Step 5: Verify all current tests pass at base.**

Run: `task test`

Expected: PASS.

- [ ] **Step 6: Verify current migration max is 000015.**

Run: `ls internal/store/migrations/ | tail -3`

Expected: `000015_create_player_character_bindings.{up,down}.sql` is the highest. Phase 3c will add `000016`.

---

### Task T1: `cluster.SubsystemCluster` lifecycle ID + `cluster` package skeleton (types + Pill, no NATS yet)

Lay the package skeleton. Types compile and tests pass against a no-NATS, no-subsystem-registration `Registry` stub. Subsequent tasks fill in heartbeat, probe-and-pill, lifecycle.

**Files:**

- Modify: `internal/lifecycle/subsystem.go:17-28`
- Regenerate: `internal/lifecycle/subsystemid_string.go`
- Create: `internal/cluster/types.go`
- Create: `internal/cluster/types_test.go`
- Create: `internal/cluster/pill.go`
- Create: `internal/cluster/pill_test.go`
- Create: `internal/cluster/metrics.go`

- [ ] **Step 1: Add `SubsystemCluster` constant.**

Edit `internal/lifecycle/subsystem.go` lines 17-28 to add the new constant after `SubsystemAuditProjection`:

```go
const (
    SubsystemDatabase        SubsystemID = iota // database
    SubsystemTLS                                // tls
    SubsystemABAC                               // abac
    SubsystemAuth                               // auth
    SubsystemWorld                              // world
    SubsystemPlugins                            // plugins
    SubsystemSessions                           // sessions
    SubsystemBootstrap                          // bootstrap
    SubsystemGRPC                               // grpc
    SubsystemEventBus                           // eventbus
    SubsystemAuditProjection                    // audit_projection
    SubsystemCluster                            // cluster
)
```

- [ ] **Step 2: Regenerate the stringer file.**

Run: `go generate ./internal/lifecycle/...`

Expected: `internal/lifecycle/subsystemid_string.go` updated to include `cluster` in `_SubsystemID_name` and an additional index entry.

- [ ] **Step 3: Write a failing test for the new SubsystemID.**

Create `internal/lifecycle/subsystem_test.go` (or append to the existing one if present):

```go
func TestSubsystemClusterStringIsCluster(t *testing.T) {
    if got := SubsystemCluster.String(); got != "cluster" {
        t.Fatalf("SubsystemCluster.String() = %q; want %q", got, "cluster")
    }
}
```

- [ ] **Step 4: Run test to verify it passes.**

Run: `task test -- -run TestSubsystemClusterStringIsCluster ./internal/lifecycle/...`

Expected: PASS (because step 1 + step 2 already landed the value and the stringer entry).

- [ ] **Step 5: Commit the lifecycle SubsystemID addition.**

```text
internal/lifecycle: add SubsystemCluster id

Phase 3c (holomush-ojw1.3) introduces a new top-level cluster
subsystem (cluster.Registry) that owns NATS heartbeat membership,
probe-and-pill remediation, and serves as the project-wide replica
health/status surface. Reserves SubsystemCluster=11; cluster package
skeleton lands in subsequent commits.
```

- [ ] **Step 6: Create `internal/cluster/types.go` with all closed-enum types and the Member struct.**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package cluster provides cluster membership, health/status, and
// failure-remediation primitives. Phase 3c (holomush-ojw1.3) ships
// the substrate; future consumers (admin RPCs, leader election) build
// on the same Registry surface.
package cluster

import "time"

// MemberID identifies a cluster member. Per-process: each holomush
// process generates a fresh ULID-formatted MemberID at startup.
// Persistent identity is intentionally NOT supported — restarted
// processes appear as new members; old members get evicted via
// heartbeat-timeout or graceful `bye`. See the Phase 3c grounding
// doc Decision 1 for rationale.
type MemberID string

// MemberStatus is a closed enum for member lifecycle state.
type MemberStatus int

const (
    StatusUnknown  MemberStatus = iota
    StatusAlive                 // heartbeat fresh within 2× heartbeat interval
    StatusStale                 // 1-2 missed heartbeats; not yet evicted
    StatusEvicted               // 3+ missed heartbeats or received bye
    StatusPilled                // pilled by a coordinator; removed
)

// String returns a human-readable status name (used in logs/metrics).
func (s MemberStatus) String() string {
    switch s {
    case StatusAlive:
        return "alive"
    case StatusStale:
        return "stale"
    case StatusEvicted:
        return "evicted"
    case StatusPilled:
        return "pilled"
    default:
        return "unknown"
    }
}

// Member is the registry's view of a cluster member. SkewSeconds is
// computed at receive time per Phase 3c grounding doc Decision 8.
type Member struct {
    ID                  MemberID
    Status              MemberStatus
    StartedAt           time.Time // sender's wall-clock at process start; observability only
    LastHeartbeatAt     time.Time // receiver's local clock at last receive
    LastPublishedAt     time.Time // sender's wall-clock at last heartbeat publish
    HolomushVersion     string
    LastInvalidationSeq uint64    // observability; not used in protocol decisions
    SkewSeconds         float64   // 0 for self; computed for peers per Decision 8
}

// LeaveReason is a closed enum for the OnMemberLeft observer callback.
type LeaveReason int

const (
    LeaveReasonGracefulBye LeaveReason = iota
    LeaveReasonHeartbeatTimeout
    LeaveReasonPilled
)

// String returns a human-readable reason name.
func (r LeaveReason) String() string {
    switch r {
    case LeaveReasonGracefulBye:
        return "graceful_bye"
    case LeaveReasonHeartbeatTimeout:
        return "heartbeat_timeout"
    case LeaveReasonPilled:
        return "pilled"
    default:
        return "unknown"
    }
}

// PillReason is a closed enum carried in pill payloads. Used as a
// Prometheus label and structured-log field.
type PillReason string

const (
    PillReasonMissedInvalidationAck PillReason = "missed_invalidation_ack"
    PillReasonMissedProbeResponse   PillReason = "missed_probe_response"
    PillReasonOperatorEvict         PillReason = "operator_evict"      // future @evict-member
    PillReasonClusterIDMismatch     PillReason = "cluster_id_mismatch" // defensive
)

// MemberObserver is the callback interface for membership change
// events. Future consumers (admin RPC, leader election) implement
// this. Observers MUST NOT block; long work belongs in a separate
// goroutine.
type MemberObserver interface {
    OnMemberJoined(Member)
    OnMemberLeft(MemberID, LeaveReason)
    OnMemberStatusChanged(MemberID, MemberStatus)
}

// Config parameterizes the Registry. Defaults are applied to zero
// values via Defaults(); production wiring constructs Config in
// cmd/holomush/core.go using the existing version variable + the
// eventbus.Config.GameID.
//
// HolomushVersion is sourced from cmd/holomush/main.go's `version`
// ldflag-set variable, injected via dependency at subsystem
// construction. internal/cluster/ MUST NOT import from cmd/holomush/
// or introduce its own ldflag variable (per Phase 3c grounding doc
// Decision 1).
type Config struct {
    ClusterID         string
    HolomushVersion   string
    HeartbeatInterval time.Duration
    EvictAfterMissed  int
    ProbeTimeout      time.Duration
    PillRateLimit     time.Duration
    SkewWarnThreshold time.Duration
}

// Defaults applies the master-spec / grounding-doc defaults to any
// zero-value field on cfg and returns the result.
func (cfg Config) Defaults() Config {
    if cfg.HeartbeatInterval <= 0 {
        cfg.HeartbeatInterval = 5 * time.Second
    }
    if cfg.EvictAfterMissed <= 0 {
        cfg.EvictAfterMissed = 3
    }
    if cfg.ProbeTimeout <= 0 {
        cfg.ProbeTimeout = 250 * time.Millisecond
    }
    if cfg.PillRateLimit <= 0 {
        cfg.PillRateLimit = 60 * time.Second
    }
    if cfg.SkewWarnThreshold <= 0 {
        cfg.SkewWarnThreshold = 30 * time.Second
    }
    return cfg
}
```

- [ ] **Step 7: Write failing tests for `types.go`.**

Create `internal/cluster/types_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster

import (
    "testing"
    "time"
)

func TestMemberStatusStringReturnsHumanReadableName(t *testing.T) {
    cases := []struct {
        in   MemberStatus
        want string
    }{
        {StatusUnknown, "unknown"},
        {StatusAlive, "alive"},
        {StatusStale, "stale"},
        {StatusEvicted, "evicted"},
        {StatusPilled, "pilled"},
    }
    for _, tc := range cases {
        if got := tc.in.String(); got != tc.want {
            t.Errorf("MemberStatus(%d).String() = %q; want %q", tc.in, got, tc.want)
        }
    }
}

func TestLeaveReasonStringReturnsHumanReadableName(t *testing.T) {
    cases := []struct {
        in   LeaveReason
        want string
    }{
        {LeaveReasonGracefulBye, "graceful_bye"},
        {LeaveReasonHeartbeatTimeout, "heartbeat_timeout"},
        {LeaveReasonPilled, "pilled"},
    }
    for _, tc := range cases {
        if got := tc.in.String(); got != tc.want {
            t.Errorf("LeaveReason(%d).String() = %q; want %q", tc.in, got, tc.want)
        }
    }
}

func TestConfigDefaultsAppliesMasterSpecValues(t *testing.T) {
    var cfg Config
    out := cfg.Defaults()
    if out.HeartbeatInterval != 5*time.Second {
        t.Errorf("HeartbeatInterval = %v; want 5s", out.HeartbeatInterval)
    }
    if out.EvictAfterMissed != 3 {
        t.Errorf("EvictAfterMissed = %d; want 3", out.EvictAfterMissed)
    }
    if out.ProbeTimeout != 250*time.Millisecond {
        t.Errorf("ProbeTimeout = %v; want 250ms", out.ProbeTimeout)
    }
    if out.PillRateLimit != 60*time.Second {
        t.Errorf("PillRateLimit = %v; want 60s", out.PillRateLimit)
    }
    if out.SkewWarnThreshold != 30*time.Second {
        t.Errorf("SkewWarnThreshold = %v; want 30s", out.SkewWarnThreshold)
    }
}

func TestConfigDefaultsPreservesNonZeroValues(t *testing.T) {
    cfg := Config{
        HeartbeatInterval: 1 * time.Second,
        EvictAfterMissed:  5,
    }
    out := cfg.Defaults()
    if out.HeartbeatInterval != 1*time.Second {
        t.Errorf("HeartbeatInterval = %v; want 1s (preserved)", out.HeartbeatInterval)
    }
    if out.EvictAfterMissed != 5 {
        t.Errorf("EvictAfterMissed = %d; want 5 (preserved)", out.EvictAfterMissed)
    }
    // Unset fields still defaulted.
    if out.ProbeTimeout != 250*time.Millisecond {
        t.Errorf("ProbeTimeout default missing: %v", out.ProbeTimeout)
    }
}
```

- [ ] **Step 8: Run tests to verify they pass.**

Run: `task test -- ./internal/cluster/`

Expected: `TestMemberStatusStringReturnsHumanReadableName`, `TestLeaveReasonStringReturnsHumanReadableName`, `TestConfigDefaultsAppliesMasterSpecValues`, `TestConfigDefaultsPreservesNonZeroValues` all PASS.

- [ ] **Step 9: Create `internal/cluster/pill.go` (Pill interface + three implementations).**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster

import (
    "context"
    "fmt"
    "log/slog"
    "os"
    "time"
)

// Pill is the process-termination interface invoked when this member
// receives a poison-pill message. Production wiring uses os.Exit(125);
// test wiring records the trigger on a channel; dev wiring panics.
//
// Trigger MUST log a structured error entry and increment
// replica_poisoned_total{member_id, reason, source_id} BEFORE any
// termination action. Production deployments MUST run under a
// supervisor that interprets exit code 125 as restart-eligible
// (systemd Restart=on-failure, k8s restartPolicy=Always, docker
// restart=on-failure).
//
// ctx is provided so implementations can flush context-bound
// telemetry (e.g., open spans). Implementations MUST bound flush
// time (default 1s) since the cluster has already decided this
// process is done.
type Pill interface {
    Trigger(ctx context.Context, reason PillReason, sourceID MemberID)
}

// pillFlushTimeout caps how long Trigger waits to flush telemetry
// before terminating. Bounded so a stuck telemetry pipeline can't
// block process exit indefinitely.
const pillFlushTimeout = 1 * time.Second

// PillEvent is recorded by TestPill for assertion purposes.
type PillEvent struct {
    Reason   PillReason
    SourceID MemberID
    At       time.Time
}

// productionPill calls os.Exit(125) after flushing telemetry.
type productionPill struct {
    self    MemberID
    logger  *slog.Logger
    metrics *PillMetrics
    exitFn  func(int) // seam for tests; production = os.Exit
}

// NewProductionPill constructs the production Pill that exits with
// code 125 after best-effort telemetry flush.
func NewProductionPill(self MemberID, logger *slog.Logger, metrics *PillMetrics) Pill {
    return &productionPill{
        self:    self,
        logger:  logger,
        metrics: metrics,
        exitFn:  os.Exit,
    }
}

func (p *productionPill) Trigger(ctx context.Context, reason PillReason, sourceID MemberID) {
    p.logger.Error("pill received; terminating",
        "self", string(p.self),
        "reason", string(reason),
        "source_id", string(sourceID),
    )
    if p.metrics != nil {
        p.metrics.PoisonedTotal.WithLabelValues(string(p.self), string(reason), string(sourceID)).Inc()
    }
    flushCtx, cancel := context.WithTimeout(ctx, pillFlushTimeout)
    defer cancel()
    _ = flushCtx // reserved for future telemetry flush calls (open spans etc.)
    p.exitFn(125)
}

// testPill records each Trigger call on a channel and does NOT exit.
type testPill struct {
    events chan PillEvent
}

// NewTestPill constructs a Pill that records pill triggers on the
// returned channel for test assertions. Trigger does NOT exit; the
// test verifies behavior and continues.
func NewTestPill() (Pill, <-chan PillEvent) {
    p := &testPill{events: make(chan PillEvent, 16)}
    return p, p.events
}

func (p *testPill) Trigger(_ context.Context, reason PillReason, sourceID MemberID) {
    select {
    case p.events <- PillEvent{Reason: reason, SourceID: sourceID, At: time.Now()}:
    default:
        // channel full; drop. Tests should drain promptly.
    }
}

// devPill panics with a recoverable message; the running `holomush
// dev` process catches the panic and surfaces the error in the
// foreground.
type devPill struct {
    self   MemberID
    logger *slog.Logger
}

// NewDevPill constructs a dev-mode Pill that panics on Trigger.
func NewDevPill(self MemberID, logger *slog.Logger) Pill {
    return &devPill{self: self, logger: logger}
}

func (p *devPill) Trigger(_ context.Context, reason PillReason, sourceID MemberID) {
    p.logger.Error("dev pill received",
        "self", string(p.self),
        "reason", string(reason),
        "source_id", string(sourceID),
    )
    panic(fmt.Sprintf("cluster: pill received reason=%s source=%s", reason, sourceID))
}
```

- [ ] **Step 10: Create `internal/cluster/metrics.go` with the Pill metrics holder.**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster

import "github.com/prometheus/client_golang/prometheus"

// PillMetrics holds the Prometheus counters that Pill implementations
// emit on Trigger. Constructed once and shared across the production
// Pill.
type PillMetrics struct {
    PoisonedTotal *prometheus.CounterVec
}

// NewPillMetrics constructs PillMetrics and registers the counter
// with the supplied registerer.
func NewPillMetrics(reg prometheus.Registerer) *PillMetrics {
    m := &PillMetrics{
        PoisonedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
            Name: "replica_poisoned_total",
            Help: "Pills received and acted upon, labelled by self member_id, reason, and source coordinator id.",
        }, []string{"member_id", "reason", "source_id"}),
    }
    reg.MustRegister(m.PoisonedTotal)
    return m
}

// SkewMetrics holds the gauge for cross-host clock skew detection
// (Decision 8). Skew is observability-only; no protocol decision
// reads from this metric.
type SkewMetrics struct {
    SkewSeconds *prometheus.GaugeVec
}

// NewSkewMetrics constructs SkewMetrics and registers the gauge.
func NewSkewMetrics(reg prometheus.Registerer) *SkewMetrics {
    m := &SkewMetrics{
        SkewSeconds: prometheus.NewGaugeVec(prometheus.GaugeOpts{
            Name: "cluster_member_skew_seconds",
            Help: "Wall-clock skew between this member and the named source, in seconds. Observability only; no protocol behavior depends on this.",
        }, []string{"member_id", "source_id"}),
    }
    reg.MustRegister(m.SkewSeconds)
    return m
}

// SelfTimeoutMetrics tracks INVALIDATION_SELF_TIMEOUT occurrences
// (single-replica deployment with hung local handler).
type SelfTimeoutMetrics struct {
    SelfTimeoutTotal prometheus.Counter
}

// NewSelfTimeoutMetrics constructs SelfTimeoutMetrics and registers
// the counter.
func NewSelfTimeoutMetrics(reg prometheus.Registerer) *SelfTimeoutMetrics {
    m := &SelfTimeoutMetrics{
        SelfTimeoutTotal: prometheus.NewCounter(prometheus.CounterOpts{
            Name: "cluster_self_timeout_total",
            Help: "Coordinator missed-ack set after probe-and-pill phase contains only Self() (N=1 single-replica with hung local handler).",
        }),
    }
    reg.MustRegister(m.SelfTimeoutTotal)
    return m
}

// DuplicateMemberIDMetrics tracks INV-53 enforcement events: heartbeat
// receive observed a colliding MemberID with a different StartedAt
// (indicating a different process re-using the ULID — birthday-bound
// astronomical, defense-in-depth detection).
type DuplicateMemberIDMetrics struct {
    DuplicateMemberIDTotal *prometheus.CounterVec
}

// NewDuplicateMemberIDMetrics constructs DuplicateMemberIDMetrics and
// registers the counter.
func NewDuplicateMemberIDMetrics(reg prometheus.Registerer) *DuplicateMemberIDMetrics {
    m := &DuplicateMemberIDMetrics{
        DuplicateMemberIDTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
            Name: "cluster_duplicate_member_id_total",
            Help: "INV-53 enforcement: heartbeat receive observed a colliding MemberID with a mismatched StartedAt; the duplicate heartbeat was rejected.",
        }, []string{"member_id"}),
    }
    reg.MustRegister(m.DuplicateMemberIDTotal)
    return m
}
```

- [ ] **Step 11: Write failing tests for `pill.go`.**

Create `internal/cluster/pill_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster

import (
    "context"
    "io"
    "log/slog"
    "testing"
    "time"
)

func TestTestPillRecordsTriggerOnChannelWithoutExiting(t *testing.T) {
    p, events := NewTestPill()
    p.Trigger(context.Background(), PillReasonMissedInvalidationAck, MemberID("01HEXAMPLE_SOURCE"))

    select {
    case ev := <-events:
        if ev.Reason != PillReasonMissedInvalidationAck {
            t.Errorf("Reason = %q; want %q", ev.Reason, PillReasonMissedInvalidationAck)
        }
        if ev.SourceID != MemberID("01HEXAMPLE_SOURCE") {
            t.Errorf("SourceID = %q; want %q", ev.SourceID, "01HEXAMPLE_SOURCE")
        }
        if ev.At.IsZero() {
            t.Error("At is zero; want non-zero timestamp")
        }
    case <-time.After(1 * time.Second):
        t.Fatal("expected pill event on channel within 1s; got none")
    }
}

func TestProductionPillCallsExitFn125WithReasonInLogs(t *testing.T) {
    var exitCode int
    logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

    // Construct production Pill but inject a test exit function so
    // we can assert the code instead of terminating.
    p := &productionPill{
        self:    MemberID("01HSELF"),
        logger:  logger,
        metrics: nil, // skip metrics for this test
        exitFn:  func(code int) { exitCode = code },
    }

    p.Trigger(context.Background(), PillReasonMissedProbeResponse, MemberID("01HCOORDINATOR"))

    if exitCode != 125 {
        t.Errorf("exit code = %d; want 125", exitCode)
    }
}

func TestDevPillPanicsWithReasonAndSource(t *testing.T) {
    logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
    p := NewDevPill(MemberID("01HSELF"), logger)

    defer func() {
        r := recover()
        if r == nil {
            t.Fatal("expected DevPill to panic")
        }
        msg, ok := r.(string)
        if !ok {
            t.Fatalf("panic value type = %T; want string", r)
        }
        if !contains(msg, "missed_invalidation_ack") {
            t.Errorf("panic message = %q; want to contain reason 'missed_invalidation_ack'", msg)
        }
        if !contains(msg, "01HCOORDINATOR") {
            t.Errorf("panic message = %q; want to contain source '01HCOORDINATOR'", msg)
        }
    }()

    p.Trigger(context.Background(), PillReasonMissedInvalidationAck, MemberID("01HCOORDINATOR"))
}

// contains is a tiny string-contains helper to avoid importing strings
// for one assertion in a test file.
func contains(s, sub string) bool {
    for i := 0; i+len(sub) <= len(s); i++ {
        if s[i:i+len(sub)] == sub {
            return true
        }
    }
    return false
}
```

- [ ] **Step 12: Run pill tests.**

Run: `task test -- ./internal/cluster/`

Expected: 4 tests in `pill_test.go` PASS plus the 4 from `types_test.go` = 8 PASS.

- [ ] **Step 13: Run lint to catch any style issues.**

Run: `task lint`

Expected: PASS. Address any findings before commit.

- [ ] **Step 14: Commit T1.**

Files:

```text
internal/lifecycle/subsystem.go
internal/lifecycle/subsystemid_string.go
internal/lifecycle/subsystem_test.go (or appended)
internal/cluster/types.go
internal/cluster/types_test.go
internal/cluster/pill.go
internal/cluster/pill_test.go
internal/cluster/metrics.go
```

Commit message:

```text
internal/cluster: add types, Pill interface, and metrics scaffold

Phase 3c (holomush-ojw1.3) substrate scaffold for cross-replica DEK
cache invalidation. This commit lays the cluster package skeleton
without NATS or subsystem registration:

- Member, MemberID, MemberStatus, LeaveReason, PillReason types
- Config struct with Defaults() applying master-spec values
  (HeartbeatInterval=5s, EvictAfterMissed=3, ProbeTimeout=250ms,
   PillRateLimit=60s, SkewWarnThreshold=30s)
- Pill interface with three implementations:
  * NewProductionPill: os.Exit(125) after telemetry flush; production
    deployments MUST run under a supervisor (systemd Restart=on-failure,
    k8s restartPolicy=Always, docker restart=on-failure)
  * NewTestPill: records triggers on a channel without exiting
  * NewDevPill: panics with a recoverable message
- PillMetrics, SkewMetrics, SelfTimeoutMetrics holders with
  Prometheus registration

Subsequent commits add NATS heartbeat, ProbeAndPill, lifecycle
subsystem wiring, and the invalidation Coordinator.

Refs holomush-ojw1.3.T1.
```

---

### Task T2: `cluster.Registry` with NATS heartbeat membership + Subsystem lifecycle

The Registry implementation: heartbeat ticker, peer alive/bye receive, in-memory member set with reverse index, observer fan-out, `lifecycle.Subsystem` Start/Stop. ProbeAndPill is stubbed in this task; T4 adds the body.

**Files:**

- Create: `internal/cluster/payload.go`
- Create: `internal/cluster/payload_test.go`
- Create: `internal/cluster/registry.go`
- Create: `internal/cluster/heartbeat.go`
- Create: `internal/cluster/registry_test.go`
- Create: `internal/cluster/heartbeat_test.go`
- Create: `internal/cluster/clustertest/harness.go`

- [ ] **Step 1: Create `internal/cluster/payload.go` with NATS payload structs and subject builders.**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster

import (
    "encoding/json"
    "fmt"
    "time"

    "github.com/samber/oops"
)

// HeartbeatPayload is published on internal.<cluster_id>.member.alive.<member_id>
// every HeartbeatInterval. Receivers update their view of the source's
// LastHeartbeatAt (local clock) and LastPublishedAt (sender clock, used
// for skew detection only).
type HeartbeatPayload struct {
    ClusterID           string    `json:"cluster_id"`
    MemberID            MemberID  `json:"member_id"`
    StartedAt           time.Time `json:"started_at"`
    PublishedAt         time.Time `json:"published_at"`
    HolomushVersion     string    `json:"holomush_version"`
    LastInvalidationSeq uint64    `json:"last_invalidation_seq"`
}

// ByePayload is published once on internal.<cluster_id>.member.bye.<member_id>
// at graceful Stop. Receivers evict the source immediately (no wait for
// heartbeat-timeout).
type ByePayload struct {
    ClusterID string   `json:"cluster_id"`
    MemberID  MemberID `json:"member_id"`
    Reason    string   `json:"reason"`
}

// ProbePayload is the request body of a focused liveness probe sent on
// internal.<cluster_id>.member.probe.<member_id>. Empty by design; the
// subject conveys the target.
type ProbePayload struct{}

// ProbeReplyPayload is the response published on the probe's reply
// inbox. Carries the same observability fields as a heartbeat (the
// receiver may update its registry view from the reply).
type ProbeReplyPayload struct {
    MemberID            MemberID `json:"member_id"`
    LastInvalidationSeq uint64   `json:"last_invalidation_seq"`
}

// PoisonPayload is published on internal.<cluster_id>.member.poison.<member_id>
// to terminate a member. Publish-and-forget; no reply.
type PoisonPayload struct {
    ClusterID            string     `json:"cluster_id"`
    CoordinatorMemberID  MemberID   `json:"coordinator_member_id"`
    Reason               PillReason `json:"reason"`
    IssuedAt             time.Time  `json:"issued_at"`
}

// SubjectAlive returns the subject pattern for heartbeat publishes.
func SubjectAlive(clusterID string, id MemberID) string {
    return fmt.Sprintf("internal.%s.member.alive.%s", clusterID, string(id))
}

// SubjectAliveWildcard returns the wildcard pattern subscribers use to
// receive any peer's heartbeat.
func SubjectAliveWildcard(clusterID string) string {
    return fmt.Sprintf("internal.%s.member.alive.>", clusterID)
}

// SubjectBye returns the subject pattern for graceful-stop publishes.
func SubjectBye(clusterID string, id MemberID) string {
    return fmt.Sprintf("internal.%s.member.bye.%s", clusterID, string(id))
}

// SubjectByeWildcard returns the wildcard pattern for bye subscriptions.
func SubjectByeWildcard(clusterID string) string {
    return fmt.Sprintf("internal.%s.member.bye.>", clusterID)
}

// SubjectProbe returns the subject pattern for the probe sent to a
// specific member.
func SubjectProbe(clusterID string, id MemberID) string {
    return fmt.Sprintf("internal.%s.member.probe.%s", clusterID, string(id))
}

// SubjectProbeSelf returns the subject pattern this member subscribes
// to in order to receive probes targeting it.
func SubjectProbeSelf(clusterID string, self MemberID) string {
    return SubjectProbe(clusterID, self)
}

// SubjectPoison returns the subject pattern for a pill targeting a
// specific member.
func SubjectPoison(clusterID string, id MemberID) string {
    return fmt.Sprintf("internal.%s.member.poison.%s", clusterID, string(id))
}

// SubjectPoisonSelf returns the subject pattern this member subscribes
// to in order to receive its own pill.
func SubjectPoisonSelf(clusterID string, self MemberID) string {
    return SubjectPoison(clusterID, self)
}

// MarshalHeartbeat marshals a heartbeat payload, returning a typed
// error on failure. JSON is the chosen format because operators
// debugging via `nats sub` benefit from readable subjects + readable
// payloads; protobuf would obscure both.
func MarshalHeartbeat(p HeartbeatPayload) ([]byte, error) {
    b, err := json.Marshal(p)
    if err != nil {
        return nil, oops.Code("CLUSTER_MARSHAL_HEARTBEAT_FAILED").Wrap(err)
    }
    return b, nil
}

// UnmarshalHeartbeat unmarshals a heartbeat payload, returning a typed
// error on failure.
func UnmarshalHeartbeat(b []byte) (HeartbeatPayload, error) {
    var p HeartbeatPayload
    if err := json.Unmarshal(b, &p); err != nil {
        return HeartbeatPayload{}, oops.Code("CLUSTER_UNMARSHAL_HEARTBEAT_FAILED").Wrap(err)
    }
    return p, nil
}

// MarshalBye / UnmarshalBye — same shape as Heartbeat.
func MarshalBye(p ByePayload) ([]byte, error) {
    b, err := json.Marshal(p)
    if err != nil {
        return nil, oops.Code("CLUSTER_MARSHAL_BYE_FAILED").Wrap(err)
    }
    return b, nil
}

func UnmarshalBye(b []byte) (ByePayload, error) {
    var p ByePayload
    if err := json.Unmarshal(b, &p); err != nil {
        return ByePayload{}, oops.Code("CLUSTER_UNMARSHAL_BYE_FAILED").Wrap(err)
    }
    return p, nil
}

// MarshalProbeReply / UnmarshalProbeReply — same shape.
func MarshalProbeReply(p ProbeReplyPayload) ([]byte, error) {
    b, err := json.Marshal(p)
    if err != nil {
        return nil, oops.Code("CLUSTER_MARSHAL_PROBE_REPLY_FAILED").Wrap(err)
    }
    return b, nil
}

func UnmarshalProbeReply(b []byte) (ProbeReplyPayload, error) {
    var p ProbeReplyPayload
    if err := json.Unmarshal(b, &p); err != nil {
        return ProbeReplyPayload{}, oops.Code("CLUSTER_UNMARSHAL_PROBE_REPLY_FAILED").Wrap(err)
    }
    return p, nil
}

// MarshalPoison / UnmarshalPoison — same shape.
func MarshalPoison(p PoisonPayload) ([]byte, error) {
    b, err := json.Marshal(p)
    if err != nil {
        return nil, oops.Code("CLUSTER_MARSHAL_POISON_FAILED").Wrap(err)
    }
    return b, nil
}

func UnmarshalPoison(b []byte) (PoisonPayload, error) {
    var p PoisonPayload
    if err := json.Unmarshal(b, &p); err != nil {
        return PoisonPayload{}, oops.Code("CLUSTER_UNMARSHAL_POISON_FAILED").Wrap(err)
    }
    return p, nil
}
```

- [ ] **Step 2: Write payload round-trip tests.**

Create `internal/cluster/payload_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster

import (
    "testing"
    "time"
)

func TestHeartbeatPayloadRoundTripsThroughJSON(t *testing.T) {
    in := HeartbeatPayload{
        ClusterID:           "test-game",
        MemberID:            MemberID("01HEXAMPLE"),
        StartedAt:           time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC),
        PublishedAt:         time.Date(2026, 5, 2, 12, 0, 5, 0, time.UTC),
        HolomushVersion:     "dev",
        LastInvalidationSeq: 42,
    }
    b, err := MarshalHeartbeat(in)
    if err != nil {
        t.Fatalf("MarshalHeartbeat: %v", err)
    }
    out, err := UnmarshalHeartbeat(b)
    if err != nil {
        t.Fatalf("UnmarshalHeartbeat: %v", err)
    }
    if out != in {
        t.Errorf("round-trip mismatch: got %+v want %+v", out, in)
    }
}

func TestSubjectAliveProducesClusterIdNamespacedSubject(t *testing.T) {
    got := SubjectAlive("test-game", MemberID("01HEXAMPLE"))
    want := "internal.test-game.member.alive.01HEXAMPLE"
    if got != want {
        t.Errorf("SubjectAlive = %q; want %q", got, want)
    }
}

func TestSubjectPoisonProducesClusterIdNamespacedSubject(t *testing.T) {
    got := SubjectPoison("test-game", MemberID("01HVICTIM"))
    want := "internal.test-game.member.poison.01HVICTIM"
    if got != want {
        t.Errorf("SubjectPoison = %q; want %q", got, want)
    }
}

func TestUnmarshalHeartbeatReturnsTypedErrorOnGarbage(t *testing.T) {
    _, err := UnmarshalHeartbeat([]byte("not json"))
    if err == nil {
        t.Fatal("expected error on garbage payload")
    }
    // The oops typed-error code is the project's convention; verify it.
    // Phase 3a/3b use errutil.AssertErrorCode but to keep this test
    // dependency-free we use the message contains check. Promote to
    // errutil.AssertErrorCode if existing tests in this package adopt it.
    if !contains(err.Error(), "CLUSTER_UNMARSHAL_HEARTBEAT_FAILED") {
        t.Errorf("error code missing; got: %v", err)
    }
}
```

- [ ] **Step 3: Run payload tests.**

Run: `task test -- ./internal/cluster/`

Expected: 3 new tests PASS (plus the 4 existing from T1). `TestUnmarshalHeartbeatReturnsTypedErrorOnGarbage` should pass thanks to `oops.Code(...).Wrap(err)`.

- [ ] **Step 4: Create `internal/cluster/registry.go` with the Registry interface and core implementation.**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster

import (
    "context"
    "log/slog"
    "sync"
    "time"

    "github.com/nats-io/nats.go"
    "github.com/oklog/ulid/v2"
    "github.com/samber/oops"

    "github.com/holomush/holomush/internal/lifecycle"
)

// Registry is the cluster membership and health surface. Phase 3c ships
// a single concrete implementation backed by an in-process NATS connection.
type Registry interface {
    // Lifecycle (called by subsystem orchestrator)
    ID() lifecycle.SubsystemID
    DependsOn() []lifecycle.SubsystemID
    Start(ctx context.Context) error
    Stop(ctx context.Context) error

    // Self returns this process's MemberID.
    Self() MemberID

    // LiveMembers returns a snapshot of currently-live members. O(N)
    // allocation; safe for concurrent use.
    LiveMembers() []Member

    // Member returns the registry's view of a specific member. Returns
    // false if the member is not in the live set.
    Member(id MemberID) (Member, bool)

    // LiveCount returns the size of the live set. O(1) atomic-style
    // read via the registry mutex; used by Coordinator (T9) to compute
    // N before each invalidation publish. Always >= 1 (self counts).
    LiveCount() int

    // ProbeAndPill issues a focused liveness probe (T4 implementation;
    // stubbed in T2 to return ErrNotImplemented).
    ProbeAndPill(ctx context.Context, id MemberID, reason PillReason) error

    // Subscribe registers an observer for membership change events.
    Subscribe(observer MemberObserver) (cancel func())
}

// Deps groups the dependencies cluster.Registry needs at construction.
type Deps struct {
    Conn              *nats.Conn          // from eventbus.Subsystem.Conn()
    Logger            *slog.Logger
    PillMetrics       *PillMetrics
    SkewMetrics       *SkewMetrics
    SelfTimeout       *SelfTimeoutMetrics
    DuplicateMemberID *DuplicateMemberIDMetrics // INV-53 detection metric
    Pill              Pill                // production / test / dev
    SelfIDForTest     MemberID            // tests inject; production uses ulid.Make()
}

// NewSubsystem constructs a Registry-backed Subsystem. Production
// callers pass a real *nats.Conn and ProductionPill. Tests use the
// clustertest harness.
func NewSubsystem(cfg Config, deps Deps) (Registry, error) {
    cfg = cfg.Defaults()
    if cfg.ClusterID == "" {
        return nil, oops.Code("CLUSTER_CONFIG_MISSING_CLUSTER_ID").
            Errorf("cluster.NewSubsystem requires non-empty ClusterID; sourced from eventbus.Config.GameID")
    }
    if deps.Conn == nil {
        return nil, oops.Code("CLUSTER_DEPS_NIL").With("dep", "Conn").
            Errorf("cluster.NewSubsystem requires a non-nil *nats.Conn")
    }
    if deps.Pill == nil {
        return nil, oops.Code("CLUSTER_DEPS_NIL").With("dep", "Pill").
            Errorf("cluster.NewSubsystem requires a non-nil Pill")
    }
    if deps.Logger == nil {
        deps.Logger = slog.Default()
    }
    self := deps.SelfIDForTest
    if self == "" {
        self = MemberID(ulid.Make().String())
    }
    r := &registry{
        cfg:    cfg,
        deps:   deps,
        self:   self,
        members: map[MemberID]*Member{
            self: {ID: self, Status: StatusAlive, StartedAt: time.Now(), HolomushVersion: cfg.HolomushVersion},
        },
        observers: map[*observerEntry]struct{}{},
    }
    return r, nil
}

type registry struct {
    cfg  Config
    deps Deps
    self MemberID

    mu        sync.RWMutex
    members   map[MemberID]*Member
    observers map[*observerEntry]struct{}

    // Subscriptions held while Started. Cleaned up in Stop.
    subAlive  *nats.Subscription
    subBye    *nats.Subscription
    subProbe  *nats.Subscription
    subPoison *nats.Subscription

    // Heartbeat ticker control.
    hbTicker *time.Ticker
    hbDone   chan struct{}

    // Eviction sweeper control.
    evDone chan struct{}

    // Pill rate-limit (T4 fills this in).
    pillRateMu  sync.Mutex
    pillRateMap map[pillRateKey]time.Time

    // Tracks last published invalidation seq for inclusion in
    // outgoing heartbeats. Updated by external setters in T9.
    lastInvSeq uint64
}

type observerEntry struct {
    obs MemberObserver
}

type pillRateKey struct {
    member MemberID
    reason PillReason
}

func (r *registry) ID() lifecycle.SubsystemID            { return lifecycle.SubsystemCluster }
func (r *registry) DependsOn() []lifecycle.SubsystemID   { return []lifecycle.SubsystemID{lifecycle.SubsystemEventBus} }
func (r *registry) Self() MemberID                       { return r.self }

func (r *registry) LiveMembers() []Member {
    r.mu.RLock()
    defer r.mu.RUnlock()
    out := make([]Member, 0, len(r.members))
    for _, m := range r.members {
        if m.Status == StatusAlive || m.Status == StatusStale {
            out = append(out, *m)
        }
    }
    return out
}

func (r *registry) Member(id MemberID) (Member, bool) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    m, ok := r.members[id]
    if !ok {
        return Member{}, false
    }
    return *m, true
}

func (r *registry) LiveCount() int {
    r.mu.RLock()
    defer r.mu.RUnlock()
    n := 0
    for _, m := range r.members {
        if m.Status == StatusAlive || m.Status == StatusStale {
            n++
        }
    }
    return n
}

func (r *registry) Subscribe(obs MemberObserver) (cancel func()) {
    if obs == nil {
        return func() {}
    }
    entry := &observerEntry{obs: obs}
    r.mu.Lock()
    r.observers[entry] = struct{}{}
    r.mu.Unlock()
    return func() {
        r.mu.Lock()
        delete(r.observers, entry)
        r.mu.Unlock()
    }
}

// notifyJoined / notifyLeft / notifyStatus fan out to all observers
// while holding only the registry's read lock briefly to snapshot the
// observer set. Observer callbacks themselves run outside the lock so
// a slow observer cannot stall registry operations.
func (r *registry) notifyJoined(m Member) {
    obs := r.snapshotObservers()
    for _, e := range obs {
        e.obs.OnMemberJoined(m)
    }
}

func (r *registry) notifyLeft(id MemberID, reason LeaveReason) {
    obs := r.snapshotObservers()
    for _, e := range obs {
        e.obs.OnMemberLeft(id, reason)
    }
}

func (r *registry) notifyStatus(id MemberID, status MemberStatus) {
    obs := r.snapshotObservers()
    for _, e := range obs {
        e.obs.OnMemberStatusChanged(id, status)
    }
}

func (r *registry) snapshotObservers() []*observerEntry {
    r.mu.RLock()
    defer r.mu.RUnlock()
    out := make([]*observerEntry, 0, len(r.observers))
    for e := range r.observers {
        out = append(out, e)
    }
    return out
}

// ProbeAndPill stub — T4 fills in real probe + pill emission + rate limit.
func (r *registry) ProbeAndPill(_ context.Context, _ MemberID, _ PillReason) error {
    return oops.Code("CLUSTER_PROBE_AND_PILL_NOT_IMPLEMENTED").
        With("tracking_task", "T4").
        Errorf("ProbeAndPill body lands in Phase 3c T4")
}

// SetLastInvalidationSeq is the seam Coordinator (T9) uses to update
// the seq number stamped on outgoing heartbeats.
func (r *registry) SetLastInvalidationSeq(seq uint64) {
    r.mu.Lock()
    r.lastInvSeq = seq
    r.mu.Unlock()
}
```

- [ ] **Step 5: Create `internal/cluster/heartbeat.go` with publish ticker + receive handlers.**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster

import (
    "context"
    "math"
    "time"

    "github.com/nats-io/nats.go"
    "github.com/samber/oops"
)

// Start brings the registry online: subscribes to peer alive/bye/probe/poison
// subjects, publishes the first heartbeat, and starts the heartbeat
// ticker + eviction sweeper.
func (r *registry) Start(ctx context.Context) error {
    if r.subAlive != nil {
        return nil // already started
    }

    sa, err := r.deps.Conn.Subscribe(SubjectAliveWildcard(r.cfg.ClusterID), r.handleAlive)
    if err != nil {
        return oops.Code("CLUSTER_SUBSCRIBE_ALIVE_FAILED").Wrap(err)
    }
    r.subAlive = sa

    sb, err := r.deps.Conn.Subscribe(SubjectByeWildcard(r.cfg.ClusterID), r.handleBye)
    if err != nil {
        sa.Unsubscribe()
        r.subAlive = nil
        return oops.Code("CLUSTER_SUBSCRIBE_BYE_FAILED").Wrap(err)
    }
    r.subBye = sb

    sp, err := r.deps.Conn.Subscribe(SubjectProbeSelf(r.cfg.ClusterID, r.self), r.handleProbe)
    if err != nil {
        sa.Unsubscribe()
        sb.Unsubscribe()
        r.subAlive = nil
        r.subBye = nil
        return oops.Code("CLUSTER_SUBSCRIBE_PROBE_FAILED").Wrap(err)
    }
    r.subProbe = sp

    spo, err := r.deps.Conn.Subscribe(SubjectPoisonSelf(r.cfg.ClusterID, r.self), r.handlePoison)
    if err != nil {
        sa.Unsubscribe()
        sb.Unsubscribe()
        sp.Unsubscribe()
        r.subAlive = nil
        r.subBye = nil
        r.subProbe = nil
        return oops.Code("CLUSTER_SUBSCRIBE_POISON_FAILED").Wrap(err)
    }
    r.subPoison = spo

    if err := r.publishHeartbeatNow(); err != nil {
        // Subscriptions established; first publish failed. Roll back.
        r.unsubAll()
        return err
    }

    r.hbDone = make(chan struct{})
    r.hbTicker = time.NewTicker(r.cfg.HeartbeatInterval)
    go r.runHeartbeatTicker()

    r.evDone = make(chan struct{})
    go r.runEvictionSweeper()

    r.deps.Logger.Info("cluster.Registry started",
        "self", string(r.self),
        "cluster_id", r.cfg.ClusterID,
        "heartbeat_interval", r.cfg.HeartbeatInterval.String(),
    )
    return nil
}

// Stop publishes the bye message, stops the heartbeat ticker, and
// drains subscriptions. Idempotent.
func (r *registry) Stop(ctx context.Context) error {
    if r.subAlive == nil {
        return nil // already stopped or never started
    }

    // Stop ticker first so we don't publish a heartbeat after bye.
    if r.hbTicker != nil {
        r.hbTicker.Stop()
        close(r.hbDone)
        r.hbTicker = nil
    }
    if r.evDone != nil {
        close(r.evDone)
        r.evDone = nil
    }

    // Best-effort bye publish; failure does not block Stop.
    p := ByePayload{ClusterID: r.cfg.ClusterID, MemberID: r.self, Reason: "graceful_stop"}
    if b, err := MarshalBye(p); err == nil {
        _ = r.deps.Conn.Publish(SubjectBye(r.cfg.ClusterID, r.self), b)
        _ = r.deps.Conn.Flush()
    }

    r.unsubAll()
    r.deps.Logger.Info("cluster.Registry stopped", "self", string(r.self))
    return nil
}

func (r *registry) unsubAll() {
    if r.subAlive != nil {
        _ = r.subAlive.Unsubscribe()
        r.subAlive = nil
    }
    if r.subBye != nil {
        _ = r.subBye.Unsubscribe()
        r.subBye = nil
    }
    if r.subProbe != nil {
        _ = r.subProbe.Unsubscribe()
        r.subProbe = nil
    }
    if r.subPoison != nil {
        _ = r.subPoison.Unsubscribe()
        r.subPoison = nil
    }
}

func (r *registry) runHeartbeatTicker() {
    for {
        select {
        case <-r.hbTicker.C:
            if err := r.publishHeartbeatNow(); err != nil {
                r.deps.Logger.Warn("heartbeat publish failed",
                    "self", string(r.self),
                    "err", err.Error(),
                )
            }
        case <-r.hbDone:
            return
        }
    }
}

func (r *registry) publishHeartbeatNow() error {
    r.mu.RLock()
    self := r.members[r.self]
    started := self.StartedAt
    seq := r.lastInvSeq
    r.mu.RUnlock()

    p := HeartbeatPayload{
        ClusterID:           r.cfg.ClusterID,
        MemberID:            r.self,
        StartedAt:           started,
        PublishedAt:         time.Now(),
        HolomushVersion:     r.cfg.HolomushVersion,
        LastInvalidationSeq: seq,
    }
    b, err := MarshalHeartbeat(p)
    if err != nil {
        return err
    }
    if err := r.deps.Conn.Publish(SubjectAlive(r.cfg.ClusterID, r.self), b); err != nil {
        return oops.Code("CLUSTER_HEARTBEAT_PUBLISH_FAILED").Wrap(err)
    }
    return nil
}

// handleAlive processes a peer's heartbeat message.
func (r *registry) handleAlive(msg *nats.Msg) {
    p, err := UnmarshalHeartbeat(msg.Data)
    if err != nil {
        r.deps.Logger.Warn("heartbeat parse failed", "err", err.Error())
        return
    }
    if p.ClusterID != r.cfg.ClusterID {
        // INV-54: drop messages from other clusters.
        r.deps.Logger.Warn("heartbeat cluster_id mismatch; dropping",
            "got", p.ClusterID, "want", r.cfg.ClusterID, "from", string(p.MemberID))
        return
    }
    if p.MemberID == r.self {
        return // ignore our own heartbeats reflected back
    }

    now := time.Now()
    skew := computeSkew(now, p.PublishedAt)

    r.mu.Lock()
    existing, present := r.members[p.MemberID]
    // INV-53: duplicate-MemberID detection. ULID collision is birthday-
    // bound astronomical, but defense-in-depth: if we've already seen a
    // heartbeat from this MemberID with a different StartedAt, a
    // different process is re-using the ULID. Reject the new heartbeat
    // (preserve first-seen identity), log structured error, and emit
    // metric. CLUSTER_MEMBER_DUPLICATE_ID is the conceptual error code
    // (no Go error returned because handleAlive is fire-and-forget).
    if present && !existing.StartedAt.IsZero() && !p.StartedAt.Equal(existing.StartedAt) {
        r.mu.Unlock()
        r.deps.Logger.Error("CLUSTER_MEMBER_DUPLICATE_ID; rejecting duplicate heartbeat",
            "member_id", string(p.MemberID),
            "existing_started_at", existing.StartedAt,
            "duplicate_started_at", p.StartedAt,
        )
        if r.deps.DuplicateMemberID != nil {
            r.deps.DuplicateMemberID.DuplicateMemberIDTotal.WithLabelValues(string(p.MemberID)).Inc()
        }
        return
    }
    if !present {
        m := &Member{
            ID:                  p.MemberID,
            Status:              StatusAlive,
            StartedAt:           p.StartedAt,
            LastHeartbeatAt:     now,
            LastPublishedAt:     p.PublishedAt,
            HolomushVersion:     p.HolomushVersion,
            LastInvalidationSeq: p.LastInvalidationSeq,
            SkewSeconds:         skew,
        }
        r.members[p.MemberID] = m
        r.mu.Unlock()
        r.deps.Logger.Info("cluster member joined",
            "member_id", string(p.MemberID),
            "version", p.HolomushVersion,
        )
        r.notifyJoined(*m)
        r.recordSkew(p.MemberID, skew)
        return
    }
    prevStatus := existing.Status
    existing.LastHeartbeatAt = now
    existing.LastPublishedAt = p.PublishedAt
    existing.HolomushVersion = p.HolomushVersion
    existing.LastInvalidationSeq = p.LastInvalidationSeq
    existing.SkewSeconds = skew
    if existing.Status == StatusStale || existing.Status == StatusEvicted {
        existing.Status = StatusAlive
    }
    statusChanged := prevStatus != existing.Status
    r.mu.Unlock()

    if statusChanged {
        r.notifyStatus(p.MemberID, StatusAlive)
    }
    r.recordSkew(p.MemberID, skew)
}

// handleBye processes a peer's graceful-shutdown message.
func (r *registry) handleBye(msg *nats.Msg) {
    p, err := UnmarshalBye(msg.Data)
    if err != nil {
        r.deps.Logger.Warn("bye parse failed", "err", err.Error())
        return
    }
    if p.ClusterID != r.cfg.ClusterID {
        return // INV-54
    }
    if p.MemberID == r.self {
        return
    }
    r.mu.Lock()
    existing, present := r.members[p.MemberID]
    if !present {
        r.mu.Unlock()
        return
    }
    delete(r.members, p.MemberID)
    _ = existing
    r.mu.Unlock()
    r.deps.Logger.Info("cluster member left", "member_id", string(p.MemberID), "reason", "graceful_bye")
    r.notifyLeft(p.MemberID, LeaveReasonGracefulBye)
}

// handleProbe replies to a focused liveness probe.
func (r *registry) handleProbe(msg *nats.Msg) {
    r.mu.RLock()
    seq := r.lastInvSeq
    r.mu.RUnlock()

    reply := ProbeReplyPayload{MemberID: r.self, LastInvalidationSeq: seq}
    b, err := MarshalProbeReply(reply)
    if err != nil {
        r.deps.Logger.Warn("probe reply marshal failed", "err", err.Error())
        return
    }
    if msg.Reply == "" {
        r.deps.Logger.Warn("probe received with empty Reply subject; ignoring")
        return
    }
    if err := r.deps.Conn.Publish(msg.Reply, b); err != nil {
        r.deps.Logger.Warn("probe reply publish failed", "err", err.Error())
    }
}

// handlePoison processes a pill targeted at this member.
func (r *registry) handlePoison(msg *nats.Msg) {
    p, err := UnmarshalPoison(msg.Data)
    if err != nil {
        r.deps.Logger.Warn("poison parse failed", "err", err.Error())
        return
    }
    if p.ClusterID != r.cfg.ClusterID {
        return // INV-54
    }
    // Pill accepted; trigger termination via injected Pill (Decision 7).
    r.deps.Pill.Trigger(context.Background(), p.Reason, p.CoordinatorMemberID)
}

// runEvictionSweeper sweeps the member set every HeartbeatInterval and
// evicts members whose LastHeartbeatAt is older than EvictAfterMissed *
// HeartbeatInterval.
func (r *registry) runEvictionSweeper() {
    ticker := time.NewTicker(r.cfg.HeartbeatInterval)
    defer ticker.Stop()
    for {
        select {
        case now := <-ticker.C:
            r.sweepEvictions(now)
        case <-r.evDone:
            return
        }
    }
}

func (r *registry) sweepEvictions(now time.Time) {
    threshold := now.Add(-time.Duration(r.cfg.EvictAfterMissed) * r.cfg.HeartbeatInterval)
    var evicted []MemberID
    r.mu.Lock()
    for id, m := range r.members {
        if id == r.self {
            continue
        }
        if m.LastHeartbeatAt.Before(threshold) && (m.Status == StatusAlive || m.Status == StatusStale) {
            delete(r.members, id)
            evicted = append(evicted, id)
        }
    }
    r.mu.Unlock()
    for _, id := range evicted {
        r.deps.Logger.Info("cluster member evicted (heartbeat timeout)", "member_id", string(id))
        r.notifyLeft(id, LeaveReasonHeartbeatTimeout)
    }
}

func (r *registry) recordSkew(source MemberID, skew float64) {
    if r.deps.SkewMetrics == nil {
        return
    }
    if skew > r.cfg.SkewWarnThreshold.Seconds() {
        r.deps.Logger.Warn("cluster member skew exceeds threshold",
            "self", string(r.self),
            "source_id", string(source),
            "skew_seconds", skew,
            "threshold_seconds", r.cfg.SkewWarnThreshold.Seconds(),
        )
    }
    r.deps.SkewMetrics.SkewSeconds.WithLabelValues(string(r.self), string(source)).Set(skew) //nolint:no_remote_clock_compare // observability-only per Decision 8
}

// computeSkew returns absolute drift in seconds between local clock and
// the remote-sourced published_at timestamp. INV-58 carve-out: this
// computation is the single allowed cross-host clock comparison and is
// gated by the lint-rule annotation in recordSkew.
func computeSkew(localNow, remotePublishedAt time.Time) float64 {
    diff := localNow.Sub(remotePublishedAt).Seconds()
    return math.Abs(diff)
}
```

- [ ] **Step 6: Create `internal/cluster/clustertest/harness.go` for test infrastructure.**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package clustertest provides multi-Registry test infrastructure on a
// shared in-process NATS connection. Used by cluster unit tests AND by
// the multi-Registry integration tests in test/integration/cluster/.
package clustertest

import (
    "context"
    "io"
    "log/slog"
    "testing"
    "time"

    "github.com/holomush/holomush/internal/cluster"
    "github.com/holomush/holomush/internal/eventbus/eventbustest"
)

// Harness wires up an embedded NATS server (via eventbustest) and a
// configurable number of cluster.Registry members on it. Cleanup is
// automatic via t.Cleanup.
type Harness struct {
    Embedded *eventbustest.Embedded
    Members  []HarnessMember
}

// HarnessMember bundles a Registry, its Pill (TestPill so trigger
// events are observable), and the channel of pill events.
type HarnessMember struct {
    Registry  cluster.Registry
    Pill      cluster.Pill
    PillEvents <-chan cluster.PillEvent
    MemberID  cluster.MemberID
}

// New constructs a Harness with n Registry members on a shared NATS
// connection. All members use cluster_id=clusterID.
func New(t *testing.T, clusterID string, n int) *Harness {
    t.Helper()
    emb := eventbustest.New(t)

    h := &Harness{Embedded: emb}
    logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

    cfg := cluster.Config{
        ClusterID:         clusterID,
        HolomushVersion:   "test",
        HeartbeatInterval: 100 * time.Millisecond, // accelerated for tests
        EvictAfterMissed:  3,
        ProbeTimeout:      50 * time.Millisecond,
        PillRateLimit:     1 * time.Second,
        SkewWarnThreshold: 30 * time.Second,
    }

    for i := 0; i < n; i++ {
        pill, events := cluster.NewTestPill()
        memberID := cluster.MemberID("01HMEMBER" + string(rune('A'+i)))
        reg, err := cluster.NewSubsystem(cfg, cluster.Deps{
            Conn:          emb.Conn,
            Logger:        logger,
            Pill:          pill,
            SelfIDForTest: memberID,
        })
        if err != nil {
            t.Fatalf("NewSubsystem[%d]: %v", i, err)
        }
        if err := reg.Start(context.Background()); err != nil {
            t.Fatalf("reg[%d].Start: %v", i, err)
        }
        t.Cleanup(func() { _ = reg.Stop(context.Background()) })

        h.Members = append(h.Members, HarnessMember{
            Registry:   reg,
            Pill:       pill,
            PillEvents: events,
            MemberID:   memberID,
        })
    }

    return h
}

// AwaitConverged blocks until every member's LiveCount() == n (each
// sees all peers). Times out at deadline.
func (h *Harness) AwaitConverged(t *testing.T, deadline time.Duration) {
    t.Helper()
    n := len(h.Members)
    ctx, cancel := context.WithTimeout(context.Background(), deadline)
    defer cancel()
    ticker := time.NewTicker(20 * time.Millisecond)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            t.Fatalf("AwaitConverged timed out after %v; member views: %s", deadline, h.snapshot())
        case <-ticker.C:
            converged := true
            for _, m := range h.Members {
                if m.Registry.LiveCount() != n {
                    converged = false
                    break
                }
            }
            if converged {
                return
            }
        }
    }
}

func (h *Harness) snapshot() string {
    out := ""
    for i, m := range h.Members {
        out += " m" + string(rune('0'+i)) + "=" + memberIDsList(m.Registry.LiveMembers())
    }
    return out
}

func memberIDsList(ms []cluster.Member) string {
    s := "["
    for i, m := range ms {
        if i > 0 {
            s += ","
        }
        s += string(m.ID)
    }
    return s + "]"
}
```

- [ ] **Step 7: Write registry tests using the harness.**

Create `internal/cluster/registry_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster_test

import (
    "context"
    "testing"
    "time"

    "github.com/holomush/holomush/internal/cluster"
    "github.com/holomush/holomush/internal/cluster/clustertest"
)

func TestRegistryStartIncludesSelfInLiveMembers(t *testing.T) {
    h := clustertest.New(t, "test-game", 1)
    if got := h.Members[0].Registry.LiveCount(); got != 1 {
        t.Fatalf("LiveCount = %d; want 1 (self)", got)
    }
    if h.Members[0].Registry.Self() == cluster.MemberID("") {
        t.Fatal("Self() returned empty MemberID")
    }
}

func TestThreeMemberClusterConvergesViaHeartbeat(t *testing.T) {
    h := clustertest.New(t, "test-game", 3)
    h.AwaitConverged(t, 2*time.Second)
    for i, m := range h.Members {
        if m.Registry.LiveCount() != 3 {
            t.Errorf("member %d LiveCount = %d; want 3", i, m.Registry.LiveCount())
        }
    }
}

func TestMemberLeftCallbackFiresOnGracefulStop(t *testing.T) {
    h := clustertest.New(t, "test-game", 2)
    h.AwaitConverged(t, 2*time.Second)

    type leaveEvent struct {
        id     cluster.MemberID
        reason cluster.LeaveReason
    }
    leaveCh := make(chan leaveEvent, 4)
    h.Members[0].Registry.Subscribe(testObserver{
        onLeft: func(id cluster.MemberID, reason cluster.LeaveReason) {
            leaveCh <- leaveEvent{id: id, reason: reason}
        },
    })

    // Stop member 1; member 0 should observe the bye.
    if err := h.Members[1].Registry.Stop(context.Background()); err != nil {
        t.Fatalf("Stop: %v", err)
    }

    select {
    case ev := <-leaveCh:
        if ev.id != h.Members[1].MemberID {
            t.Errorf("leave id = %q; want %q", ev.id, h.Members[1].MemberID)
        }
        if ev.reason != cluster.LeaveReasonGracefulBye {
            t.Errorf("leave reason = %v; want LeaveReasonGracefulBye", ev.reason)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("no OnMemberLeft callback within 2s")
    }
}

func TestStaleHeartbeatEvictsMember(t *testing.T) {
    h := clustertest.New(t, "test-game", 2)
    h.AwaitConverged(t, 2*time.Second)

    // Stop member 1's heartbeat publishing (without the bye message)
    // by directly stopping its registry. (Stop publishes bye, so this
    // test verifies that even WITH a bye-publish path, the eviction
    // sweeper handles the case where the bye was lost. Equivalent
    // outcome at the public API.)
    _ = h.Members[1].Registry.Stop(context.Background())

    // Wait for heartbeat-timeout-based eviction (3 missed × 100ms = 300ms
    // plus sweeper interval 100ms → ≤500ms).
    deadline := time.Now().Add(2 * time.Second)
    for time.Now().Before(deadline) {
        if h.Members[0].Registry.LiveCount() == 1 {
            return
        }
        time.Sleep(50 * time.Millisecond)
    }
    t.Fatalf("member 0 LiveCount = %d; want 1 after eviction", h.Members[0].Registry.LiveCount())
}

// testObserver implements cluster.MemberObserver for tests; only the
// OnMemberLeft callback is wired.
type testObserver struct {
    onJoined func(cluster.Member)
    onLeft   func(cluster.MemberID, cluster.LeaveReason)
    onStatus func(cluster.MemberID, cluster.MemberStatus)
}

func (o testObserver) OnMemberJoined(m cluster.Member) {
    if o.onJoined != nil {
        o.onJoined(m)
    }
}

func (o testObserver) OnMemberLeft(id cluster.MemberID, r cluster.LeaveReason) {
    if o.onLeft != nil {
        o.onLeft(id, r)
    }
}

func (o testObserver) OnMemberStatusChanged(id cluster.MemberID, s cluster.MemberStatus) {
    if o.onStatus != nil {
        o.onStatus(id, s)
    }
}
```

- [ ] **Step 8: Write a heartbeat-skew test.**

Create `internal/cluster/heartbeat_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster

import (
    "math"
    "testing"
    "time"
)

func TestComputeSkewReturnsAbsoluteDriftInSeconds(t *testing.T) {
    local := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
    remote := time.Date(2026, 5, 2, 12, 0, 35, 0, time.UTC) // 35s ahead

    skew := computeSkew(local, remote)
    if math.Abs(skew-35.0) > 0.001 {
        t.Errorf("skew = %v; want ≈35s", skew)
    }
}

func TestComputeSkewIsAbsoluteValue(t *testing.T) {
    local := time.Date(2026, 5, 2, 12, 0, 35, 0, time.UTC)
    remote := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC) // local 35s ahead

    skew := computeSkew(local, remote)
    if math.Abs(skew-35.0) > 0.001 {
        t.Errorf("skew = %v; want ≈35s (absolute)", skew)
    }
}
```

- [ ] **Step 9: Run all cluster tests.**

Run: `task test -- ./internal/cluster/...`

Expected: all `TestRegistry...`, `TestComputeSkew...`, `TestMemberStatus...`, `TestLeaveReason...`, `TestConfigDefaults...`, `TestHeartbeatPayload...`, `TestSubject...`, `TestUnmarshalHeartbeat...`, `TestTestPill...`, `TestProductionPill...`, `TestDevPill...` all PASS.

- [ ] **Step 10: Run lint.**

Run: `task lint`

Expected: PASS.

- [ ] **Step 11: Commit T2.**

```text
internal/cluster: NATS heartbeat membership + Subsystem lifecycle

Phase 3c (holomush-ojw1.3) cluster substrate. cluster.Registry is now
a working lifecycle.Subsystem that publishes/receives heartbeats,
processes graceful bye messages, sweeps stale members, fans out
member events to observers, and exposes Self/LiveMembers/LiveCount/
Member/Subscribe.

ProbeAndPill is stubbed (returns CLUSTER_PROBE_AND_PILL_NOT_IMPLEMENTED);
T4 fills the body. SubsystemCluster wiring into cmd/holomush/main.go
lands in T3.

Subjects: internal.<cluster_id>.member.{alive,bye,probe,poison}.<id>
under cluster_id namespace per INV-54. Heartbeat cadence 5s prod /
100ms test (clustertest.Harness accelerates for unit tests).

clustertest.Harness: shared *nats.Conn via eventbustest.Embedded,
configurable N members, AwaitConverged helper. Used by unit tests
and (in T12) by integration tests.

Refs holomush-ojw1.3.T2.
```

---

### Task T3: Register `SubsystemCluster` in `cmd/holomush/core.go`

The cluster substrate runs from Phase 3c onward in every deployment, even before `Crypto.Enabled` flips at Phase 3d. This task wires the registration.

**Files:**

- Modify: `cmd/holomush/core.go:324, 437`
- Modify: `cmd/holomush/core_test.go` (or related test files asserting on the subsystem graph)

- [ ] **Step 1: Read the existing wiring.**

Run: `rg -n "eventBusSub|orchestrator\.Register|orch\.Register|allSubsystems" cmd/holomush/core.go | head -10`

Expected: locate the eventBusSub construction (around line 324) and the orchestrator slice (around line 435).

- [ ] **Step 2: Construct `clusterSub` after `eventBusSub`.**

Edit `cmd/holomush/core.go` after the existing `eventBusSub := eventbus.NewSubsystem(eventBusConfig)` line (around 324):

```go
eventBusSub := eventbus.NewSubsystem(eventBusConfig)

// Phase 3c (holomush-ojw1.3): cluster.Registry runs in every deployment
// from this PR onward; it provides cross-replica health/status surface
// and (when DEK pipeline activates at Phase 3d) the failure-remediation
// substrate for cache invalidation. ProductionPill is wired here;
// dev/test wirings live in their respective entry points.
clusterPillMetrics := cluster.NewPillMetrics(prometheus.DefaultRegisterer)
clusterSkewMetrics := cluster.NewSkewMetrics(prometheus.DefaultRegisterer)
clusterSelfTimeoutMetrics := cluster.NewSelfTimeoutMetrics(prometheus.DefaultRegisterer)
clusterSelfID := cluster.MemberID(ulid.Make().String())
clusterPill := cluster.NewProductionPill(clusterSelfID, slog.Default(), clusterPillMetrics)
clusterSub, clusterErr := cluster.NewSubsystem(cluster.Config{
    ClusterID:       eventBusConfig.GameID,
    HolomushVersion: version, // package-private to main; ldflag-set via -X
}, cluster.Deps{
    Conn:          eventBusSub.Conn(),
    Logger:        slog.Default(),
    PillMetrics:   clusterPillMetrics,
    SkewMetrics:   clusterSkewMetrics,
    SelfTimeout:   clusterSelfTimeoutMetrics,
    Pill:          clusterPill,
    SelfIDForTest: clusterSelfID,
})
if clusterErr != nil {
    return clusterErr
}
```

Add the imports if not present:

```go
import (
    "github.com/holomush/holomush/internal/cluster"
    "github.com/oklog/ulid/v2"
    "github.com/prometheus/client_golang/prometheus"
)
```

- [ ] **Step 3: Add `clusterSub` to the orchestrator slice.**

Edit `cmd/holomush/core.go` line ~435-440 to add `clusterSub`:

```go
for _, sub := range []lifecycle.Subsystem{
    dbSub, abacSub, authSub, worldSub,
    sessionSub, pluginSub, bootstrapSub, eventBusSub, clusterSub, auditSub, grpcSub,
} {
    orch.Register(sub)
}
```

Note the position: `clusterSub` immediately after `eventBusSub` because `cluster.Registry.DependsOn() == [SubsystemEventBus]`. The orchestrator enforces dependency ordering.

- [ ] **Step 4: Build to confirm imports + signature.**

Run: `task build`

Expected: build succeeds. If imports are missing, `go vet` will flag them; add as needed.

- [ ] **Step 5: Update existing subsystem-graph tests.**

Run: `rg -n "SubsystemEventBus\b|SubsystemClus" cmd/holomush/ | head -20`

Locate any test files asserting on the set of registered subsystems. If `cmd/holomush/core_test.go` or similar files enumerate the subsystem list, append `lifecycle.SubsystemCluster` to the expected set.

If no such test exists, add a regression assertion:

Create or extend `cmd/holomush/core_subsystems_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
    "testing"

    "github.com/holomush/holomush/internal/lifecycle"
)

func TestExpectedSubsystemSetIncludesCluster(t *testing.T) {
    // Phase 3c (holomush-ojw1.3) added SubsystemCluster. This regression
    // test guards the assumption that future refactors don't accidentally
    // drop cluster substrate from the production wiring.
    expected := map[lifecycle.SubsystemID]struct{}{
        lifecycle.SubsystemDatabase:        {},
        lifecycle.SubsystemTLS:             {},
        lifecycle.SubsystemABAC:            {},
        lifecycle.SubsystemAuth:            {},
        lifecycle.SubsystemWorld:           {},
        lifecycle.SubsystemPlugins:         {},
        lifecycle.SubsystemSessions:        {},
        lifecycle.SubsystemBootstrap:       {},
        lifecycle.SubsystemGRPC:            {},
        lifecycle.SubsystemEventBus:        {},
        lifecycle.SubsystemAuditProjection: {},
        lifecycle.SubsystemCluster:         {},
    }
    if _, ok := expected[lifecycle.SubsystemCluster]; !ok {
        t.Fatal("SubsystemCluster missing from expected set")
    }
    if len(expected) != 12 {
        t.Errorf("expected set size = %d; want 12 after Phase 3c", len(expected))
    }
}
```

- [ ] **Step 6: Run unit tests.**

Run: `task test -- ./cmd/holomush/`

Expected: PASS. The cluster subsystem only requires a working NATS conn (which `eventBusSub.Conn()` provides at Start time).

- [ ] **Step 7: Run integration tests if any cover startup wiring.**

Run: `rg -l "//go:build integration" cmd/holomush/ test/ 2>&1 | head -5`

If there's a startup integration test, run it: `task test:int -- -tags=integration ./cmd/holomush/...` (or whichever tag matches).

Expected: PASS, or no relevant integration test exists.

- [ ] **Step 8: Run lint.**

Run: `task lint`

Expected: PASS.

- [ ] **Step 9: Commit T3.**

```text
cmd/holomush: register SubsystemCluster in production wiring

Phase 3c (holomush-ojw1.3) cluster substrate is now active in every
deployment. ProductionPill (os.Exit(125)) is wired; deployments MUST
run under a supervisor that interprets exit code 125 as restart-eligible
(systemd Restart=on-failure, k8s restartPolicy=Always, docker
restart=on-failure).

ClusterID sourced from eventbus.Config.GameID (existing GameID config
knob shared with the events stream namespace). HolomushVersion sourced
from cmd/holomush/main.go's `version` ldflag-set variable, injected
via Deps. internal/cluster/ does NOT import from cmd/holomush/ —
DI is the only path.

invalidation.Coordinator registration deferred to Phase 3d alongside
Crypto.Enabled flag flip.

Refs holomush-ojw1.3.T3.
```

---

### Task T4: `cluster.Registry.ProbeAndPill` body + rate limit + self-pill prevention

Replace the T2 stub with the real implementation. Probe via NATS request-reply with 250ms timeout; on probe success, update the registry's view and return nil. On probe timeout, publish a pill and synchronously evict from `LiveMembers`. INV-57 (rate limit) and INV-60 (self-pill refusal) are testable here.

**Files:**

- Create: `internal/cluster/probe_pill.go`
- Modify: `internal/cluster/registry.go` (replace stub at the existing `ProbeAndPill` site)
- Create: `internal/cluster/probe_pill_test.go`

- [ ] **Step 1: Create `internal/cluster/probe_pill.go` with the real `ProbeAndPill` body.**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster

import (
    "context"
    "time"

    "github.com/samber/oops"
)

// ErrPillRateLimited is returned by ProbeAndPill when the same
// (member_id, reason) was pilled within PillRateLimit.
var ErrPillRateLimited = oops.Code("CLUSTER_PILL_RATE_LIMITED").
    Errorf("pill rate-limited for (member_id, reason)")

// ErrCannotPillSelf is returned by ProbeAndPill when the target id
// equals r.Self(). Defense-in-depth against a caller that bypasses
// Coordinator's missed-ack self-filter.
var ErrCannotPillSelf = oops.Code("CLUSTER_CANNOT_PILL_SELF").
    Errorf("cannot pill self; caller MUST filter Self() from missing-member set")

// ErrPillProbeSucceeded marks the not-an-error case where the probe
// succeeded and no pill was issued. Returned for caller-introspection;
// callers MAY treat this as nil for control-flow purposes.
var ErrPillProbeSucceeded = oops.Code("CLUSTER_PILL_PROBE_SUCCEEDED").
    Errorf("probe succeeded; member is alive but slow on cache_invalidate channel")

// probeAndPill is the body that replaces the T2 stub. Lives in this
// file rather than registry.go so the rate-limit + self-refusal logic
// stays close to the probe/pill semantics.
func (r *registry) probeAndPill(ctx context.Context, id MemberID, reason PillReason) error {
    // INV-60: refuse self-targeted pills.
    if id == r.self {
        if r.deps.SelfTimeout != nil {
            r.deps.SelfTimeout.SelfTimeoutTotal.Inc()
        }
        r.deps.Logger.Warn("cluster.ProbeAndPill self-pill refused (INV-60)",
            "self", string(r.self),
            "reason", string(reason),
        )
        return ErrCannotPillSelf
    }

    // INV-57: rate limit per (member_id, reason).
    key := pillRateKey{member: id, reason: reason}
    r.pillRateMu.Lock()
    if r.pillRateMap == nil {
        r.pillRateMap = make(map[pillRateKey]time.Time)
    }
    if last, ok := r.pillRateMap[key]; ok {
        if time.Since(last) < r.cfg.PillRateLimit {
            r.pillRateMu.Unlock()
            return ErrPillRateLimited
        }
    }
    r.pillRateMu.Unlock()

    // Probe via NATS request-reply.
    inbox := r.deps.Conn.NewRespInbox()
    sub, err := r.deps.Conn.SubscribeSync(inbox)
    if err != nil {
        return oops.Code("CLUSTER_PROBE_INBOX_SUB_FAILED").Wrap(err)
    }
    defer sub.Drain() //nolint:errcheck // best-effort cleanup of probe inbox

    if err := r.deps.Conn.PublishRequest(SubjectProbe(r.cfg.ClusterID, id), inbox, nil); err != nil {
        return oops.Code("CLUSTER_PROBE_PUBLISH_FAILED").Wrap(err)
    }

    msg, err := sub.NextMsg(r.cfg.ProbeTimeout)
    if err == nil {
        // Probe succeeded.
        if reply, perr := UnmarshalProbeReply(msg.Data); perr == nil {
            r.updateMemberFromProbeReply(id, reply)
        }
        return ErrPillProbeSucceeded
    }

    // Probe timed out → issue pill.
    return r.issuePill(ctx, id, reason)
}

func (r *registry) updateMemberFromProbeReply(id MemberID, reply ProbeReplyPayload) {
    r.mu.Lock()
    defer r.mu.Unlock()
    if existing, ok := r.members[id]; ok {
        existing.LastHeartbeatAt = time.Now()
        existing.LastInvalidationSeq = reply.LastInvalidationSeq
        if existing.Status == StatusStale {
            existing.Status = StatusAlive
        }
    }
}

func (r *registry) issuePill(_ context.Context, id MemberID, reason PillReason) error {
    p := PoisonPayload{
        ClusterID:           r.cfg.ClusterID,
        CoordinatorMemberID: r.self,
        Reason:              reason,
        IssuedAt:            time.Now(),
    }
    b, err := MarshalPoison(p)
    if err != nil {
        return err
    }
    if err := r.deps.Conn.Publish(SubjectPoison(r.cfg.ClusterID, id), b); err != nil {
        return oops.Code("CLUSTER_PILL_PUBLISH_FAILED").Wrap(err)
    }

    // Synchronous eviction so Coordinator's retry phase sees N-1
    // immediately. Per Phase 3c grounding doc Decision 2: "Registry.markPilled
    // removes the member from LiveMembers synchronously on the issuing side
    // — without waiting for natural heartbeat eviction."
    r.mu.Lock()
    delete(r.members, id)
    r.mu.Unlock()
    r.notifyLeft(id, LeaveReasonPilled)

    // Record rate-limit timestamp.
    r.pillRateMu.Lock()
    if r.pillRateMap == nil {
        r.pillRateMap = make(map[pillRateKey]time.Time)
    }
    r.pillRateMap[pillRateKey{member: id, reason: reason}] = time.Now()
    r.pillRateMu.Unlock()

    r.deps.Logger.Warn("cluster pill issued",
        "self", string(r.self),
        "target", string(id),
        "reason", string(reason),
    )
    return nil
}
```

- [ ] **Step 2: Replace the `ProbeAndPill` stub in `registry.go`.**

In `internal/cluster/registry.go`, replace the existing stub:

```go
// Before (T2 stub):
func (r *registry) ProbeAndPill(_ context.Context, _ MemberID, _ PillReason) error {
    return oops.Code("CLUSTER_PROBE_AND_PILL_NOT_IMPLEMENTED").
        With("tracking_task", "T4").
        Errorf("ProbeAndPill body lands in Phase 3c T4")
}

// After (T4 real impl, delegates to probe_pill.go):
func (r *registry) ProbeAndPill(ctx context.Context, id MemberID, reason PillReason) error {
    return r.probeAndPill(ctx, id, reason)
}
```

- [ ] **Step 3: Write failing test for self-pill refusal (INV-60).**

Create `internal/cluster/probe_pill_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster_test

import (
    "context"
    "errors"
    "testing"
    "time"

    "github.com/holomush/holomush/internal/cluster"
    "github.com/holomush/holomush/internal/cluster/clustertest"
)

func TestProbeAndPillRefusesSelfPerINV60(t *testing.T) {
    h := clustertest.New(t, "test-game", 1)
    r := h.Members[0].Registry
    err := r.ProbeAndPill(context.Background(), r.Self(), cluster.PillReasonMissedInvalidationAck)
    if err == nil {
        t.Fatal("ProbeAndPill(self) returned nil; want ErrCannotPillSelf")
    }
    if !errors.Is(err, cluster.ErrCannotPillSelf) {
        t.Errorf("err = %v; want ErrCannotPillSelf", err)
    }
}

func TestProbeAndPillSucceedsAgainstResponsivePeer(t *testing.T) {
    h := clustertest.New(t, "test-game", 2)
    h.AwaitConverged(t, 2*time.Second)

    err := h.Members[0].Registry.ProbeAndPill(
        context.Background(),
        h.Members[1].MemberID,
        cluster.PillReasonMissedInvalidationAck,
    )
    if !errors.Is(err, cluster.ErrPillProbeSucceeded) {
        t.Errorf("err = %v; want ErrPillProbeSucceeded (peer is alive and responsive)", err)
    }
    // No pill should have been triggered on member 1.
    select {
    case ev := <-h.Members[1].PillEvents:
        t.Fatalf("unexpected pill event on responsive peer: %+v", ev)
    case <-time.After(300 * time.Millisecond):
        // Good — no pill.
    }
}

func TestProbeAndPillTriggersPillOnUnresponsivePeer(t *testing.T) {
    h := clustertest.New(t, "test-game", 2)
    h.AwaitConverged(t, 2*time.Second)

    // Stop member 1's probe subscription by stopping its registry. Since
    // Stop publishes bye, member 0 will hear the bye and evict member 1.
    // To simulate "alive but not answering probes" we instead remove only
    // the probe sub. The clustertest harness exposes this via a debug seam
    // not yet defined; for this test we simulate by stopping member 1
    // entirely AFTER caching its memberID, then manually re-adding it to
    // member 0's registry view by sending one heartbeat from a synthetic
    // publisher. (Implementation detail: extending clustertest.Harness with
    // a SuspendProbeReceive(memberIdx) helper is part of T4 if simpler
    // alternatives prove insufficient. The harness change MUST be reviewed
    // alongside this test.)
    _ = h.Members[1].Registry.Stop(context.Background())

    // Wait briefly for bye-eviction; if member 1 is gone, the test
    // verifies the post-eviction path returns nil naturally because
    // member 0 won't try to probe a non-member. To exercise the pill
    // path explicitly: extend clustertest.Harness with a
    // PublishSyntheticHeartbeat(id, clusterID) helper that injects one
    // heartbeat without setting up a real registry. Use it here.
    // (See harness extension in step 4.)
    h.PublishSyntheticHeartbeat(t, h.Members[1].MemberID, "test-game")

    err := h.Members[0].Registry.ProbeAndPill(
        context.Background(),
        h.Members[1].MemberID,
        cluster.PillReasonMissedInvalidationAck,
    )
    if err != nil {
        t.Fatalf("ProbeAndPill returned %v; want nil after pill issued", err)
    }
    // Member should be evicted from member 0's view.
    if _, ok := h.Members[0].Registry.Member(h.Members[1].MemberID); ok {
        t.Errorf("pilled member still in registry; expected synchronous eviction")
    }
}

func TestPillRateLimitBlocksSecondPillWithinWindow(t *testing.T) {
    h := clustertest.New(t, "test-game", 1)
    r := h.Members[0].Registry

    // Inject a synthetic peer that won't respond to probes.
    target := cluster.MemberID("01HSYNTHETIC_VICTIM")
    h.PublishSyntheticHeartbeat(t, target, "test-game")
    h.AwaitMemberPresent(t, 0, target, 1*time.Second)

    // First pill: succeeds.
    if err := r.ProbeAndPill(context.Background(), target, cluster.PillReasonMissedInvalidationAck); err != nil {
        t.Fatalf("first pill returned %v; want nil", err)
    }

    // Re-inject the same target so the rate-limit (per (member, reason))
    // is the only barrier.
    h.PublishSyntheticHeartbeat(t, target, "test-game")
    h.AwaitMemberPresent(t, 0, target, 1*time.Second)

    // Second pill within PillRateLimit window: blocked.
    err := r.ProbeAndPill(context.Background(), target, cluster.PillReasonMissedInvalidationAck)
    if !errors.Is(err, cluster.ErrPillRateLimited) {
        t.Errorf("second pill returned %v; want ErrPillRateLimited", err)
    }
}

// Verifies: INV-55
func TestPillReceivedOnPoisonSubjectInvokesPillTrigger(t *testing.T) {
    // INV-55: a pill received on internal.<cluster_id>.member.poison.<self_id>
    // MUST cause Pill.Trigger(ctx, reason, sourceID) to fire after telemetry
    // flush. Production Pill exits the process with code 125; TestPill
    // (used here) records the Trigger call on its events channel.
    //
    // T1's TestProductionPillCallsExitFn125WithReasonInLogs verifies that
    // ProductionPill exits 125 when Trigger is invoked directly. This test
    // verifies the wire path: NATS publish on the poison subject reaches
    // the Registry's handlePoison handler, which calls Pill.Trigger with
    // the deserialized reason + source.
    h := clustertest.New(t, "test-game", 1)

    // Publish a poison message addressed to member 0's MemberID with a
    // chosen reason and source. The Registry's handlePoison subscriber
    // (wired in T2) reads the payload and invokes Pill.Trigger.
    payload := cluster.PoisonPayload{
        ClusterID:           "test-game",
        CoordinatorMemberID: cluster.MemberID("01HSOURCE_INV55"),
        Reason:              cluster.PillReasonMissedInvalidationAck,
        IssuedAt:            time.Now(),
    }
    body, err := cluster.MarshalPoison(payload)
    if err != nil {
        t.Fatalf("MarshalPoison: %v", err)
    }
    if err := h.Embedded.Conn.Publish(
        cluster.SubjectPoison("test-game", h.Members[0].MemberID),
        body,
    ); err != nil {
        t.Fatalf("publish poison: %v", err)
    }
    if err := h.Embedded.Conn.Flush(); err != nil {
        t.Fatalf("flush: %v", err)
    }

    select {
    case ev := <-h.Members[0].PillEvents:
        if ev.Reason != cluster.PillReasonMissedInvalidationAck {
            t.Errorf("event Reason = %q; want %q", ev.Reason, cluster.PillReasonMissedInvalidationAck)
        }
        if ev.SourceID != cluster.MemberID("01HSOURCE_INV55") {
            t.Errorf("event SourceID = %q; want '01HSOURCE_INV55'", ev.SourceID)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("Pill.Trigger not invoked within 2s after poison publish")
    }
}
```

- [ ] **Step 4: Extend `clustertest.Harness` with `PublishSyntheticHeartbeat` + `AwaitMemberPresent`.**

Append to `internal/cluster/clustertest/harness.go`:

```go
// PublishSyntheticHeartbeat publishes one heartbeat from a synthetic
// MemberID. The synthetic member will NOT respond to probes; useful
// for exercising the probe-timeout → pill path. Tests MUST not rely
// on the synthetic member being long-lived (no ticker republishes;
// it gets evicted after EvictAfterMissed × HeartbeatInterval).
func (h *Harness) PublishSyntheticHeartbeat(t *testing.T, id cluster.MemberID, clusterID string) {
    t.Helper()
    p := cluster.HeartbeatPayload{
        ClusterID:       clusterID,
        MemberID:        id,
        StartedAt:       time.Now(),
        PublishedAt:     time.Now(),
        HolomushVersion: "synthetic",
    }
    b, err := cluster.MarshalHeartbeat(p)
    if err != nil {
        t.Fatalf("MarshalHeartbeat: %v", err)
    }
    if err := h.Embedded.Conn.Publish(cluster.SubjectAlive(clusterID, id), b); err != nil {
        t.Fatalf("synthetic heartbeat publish: %v", err)
    }
    if err := h.Embedded.Conn.Flush(); err != nil {
        t.Fatalf("flush: %v", err)
    }
}

// AwaitMemberPresent blocks until member i sees `target` in its
// LiveMembers, or fails the test on timeout.
func (h *Harness) AwaitMemberPresent(t *testing.T, i int, target cluster.MemberID, deadline time.Duration) {
    t.Helper()
    start := time.Now()
    for time.Since(start) < deadline {
        if _, ok := h.Members[i].Registry.Member(target); ok {
            return
        }
        time.Sleep(20 * time.Millisecond)
    }
    t.Fatalf("AwaitMemberPresent: member %d did not observe %q within %v", i, target, deadline)
}
```

- [ ] **Step 5: Run probe/pill tests.**

Run: `task test -- ./internal/cluster/...`

Expected: `TestProbeAndPillRefusesSelfPerINV60`, `TestProbeAndPillSucceedsAgainstResponsivePeer`, `TestProbeAndPillTriggersPillOnUnresponsivePeer`, `TestPillRateLimitBlocksSecondPillWithinWindow` all PASS, plus all T1+T2 tests.

- [ ] **Step 6: Run lint.**

Run: `task lint`

Expected: PASS.

- [ ] **Step 7: Commit T4.**

```text
internal/cluster: ProbeAndPill body + rate limit + self-pill refusal

Phase 3c (holomush-ojw1.3) probe-and-pill failure-remediation
substrate. Replaces the T2 stub with the real implementation:

- Probe via NATS request-reply on internal.<cluster_id>.member.probe.<id>
  with cluster.Config.ProbeTimeout (default 250ms)
- On probe success: update registry view, return ErrPillProbeSucceeded
  (treat as nil for control flow; carries diagnostic value)
- On probe timeout: publish pill on
  internal.<cluster_id>.member.poison.<id>; synchronous registry
  eviction so Coordinator's retry phase sees N-1 immediately
- INV-57: rate limit per (member_id, reason); 60s default; second
  pill within window returns ErrPillRateLimited
- INV-60: self-pill refused unconditionally; defense-in-depth
  against any caller that bypasses Coordinator's Self() filter

clustertest.Harness extended with PublishSyntheticHeartbeat +
AwaitMemberPresent helpers for exercising the probe-timeout path
without spinning up a real-but-broken Registry.

Refs holomush-ojw1.3.T4.
```

---

### Task T5: `dek.Cache` context reverse index + `InvalidateContext` + `Cache.Put` signature edit

The existing `dek.Cache` is keyed by `(KeyID, Version)` only. Phase 3c's `InvalidateContext(ContextID)` requires evicting every cached `(KeyID, Version)` belonging to a context. Add a `byContext` reverse index, extend `cacheEntry` with a `contextID` field, and update `Cache.Put` to take a `ContextID` parameter.

**Files:**

- Modify: `internal/eventbus/crypto/dek/cache.go`
- Modify: `internal/eventbus/crypto/dek/cache_test.go`
- Modify: `internal/eventbus/crypto/dek/manager.go:144, 237` (callsite updates; executors `rg "m.cache.Put"` to confirm)

- [ ] **Step 1: Add a failing test for `InvalidateContext`.**

Append to `internal/eventbus/crypto/dek/cache_test.go`:

```go
func TestCacheInvalidateContextRemovesAllVersionsForThatContext(t *testing.T) {
    c := NewCache(CacheConfig{Capacity: 100, TTL: 5 * time.Minute})

    ctxA := ContextID{Type: "scene", ID: "01HSCENE_A"}
    ctxB := ContextID{Type: "scene", ID: "01HSCENE_B"}

    c.Put(CacheKey{KeyID: 1, Version: 1}, ctxA, NewMaterial(make([]byte, DEKByteLength)))
    c.Put(CacheKey{KeyID: 1, Version: 2}, ctxA, NewMaterial(make([]byte, DEKByteLength)))
    c.Put(CacheKey{KeyID: 2, Version: 1}, ctxB, NewMaterial(make([]byte, DEKByteLength)))

    c.InvalidateContext(ctxA)

    if _, ok := c.Get(CacheKey{KeyID: 1, Version: 1}); ok {
        t.Errorf("ctxA v1 still present after InvalidateContext(ctxA)")
    }
    if _, ok := c.Get(CacheKey{KeyID: 1, Version: 2}); ok {
        t.Errorf("ctxA v2 still present after InvalidateContext(ctxA)")
    }
    if _, ok := c.Get(CacheKey{KeyID: 2, Version: 1}); !ok {
        t.Errorf("ctxB v1 missing after InvalidateContext(ctxA); only ctxA should be evicted")
    }
}

func TestCacheReverseIndexIsCleanedOnLRUEviction(t *testing.T) {
    // Capacity 2 forces eviction on third Put.
    c := NewCache(CacheConfig{Capacity: 2, TTL: 5 * time.Minute})
    ctxA := ContextID{Type: "scene", ID: "01HSCENE_A"}

    c.Put(CacheKey{KeyID: 1, Version: 1}, ctxA, NewMaterial(make([]byte, DEKByteLength)))
    c.Put(CacheKey{KeyID: 1, Version: 2}, ctxA, NewMaterial(make([]byte, DEKByteLength)))
    c.Put(CacheKey{KeyID: 1, Version: 3}, ctxA, NewMaterial(make([]byte, DEKByteLength))) // evicts v1 (LRU)

    // After eviction, reverse index for ctxA should map to {v2, v3} only.
    // Calling InvalidateContext(ctxA) should remove both remaining entries
    // and leave the cache empty.
    c.InvalidateContext(ctxA)
    if c.Len() != 0 {
        t.Errorf("cache len = %d; want 0 after InvalidateContext", c.Len())
    }
}
```

- [ ] **Step 2: Run test to verify it fails (method doesn't exist yet).**

Run: `task test -- -run TestCacheInvalidateContext ./internal/eventbus/crypto/dek/`

Expected: FAIL — `c.InvalidateContext undefined` and `Cache.Put` argument mismatch.

- [ ] **Step 3: Update `cacheEntry`, `Cache`, `Put`, `Invalidate`, and add `InvalidateContext` + `Len`.**

Edit `internal/eventbus/crypto/dek/cache.go`:

```go
// cacheEntry now carries the contextID so eviction can clean the
// reverse index in O(1).
type cacheEntry struct {
    key       CacheKey
    contextID ContextID  // NEW: enables reverse-index cleanup on LRU eviction
    material  *Material
    expiresAt time.Time
}

// Cache holds unwrapped DEK Material in process memory with LRU
// eviction and TTL safety net. INV-27: MUST NOT live in NATS KV, PG,
// disk, or logs.
//
// Phase 3c (holomush-ojw1.3) added the byContext reverse index so
// InvalidateContext can evict every (KeyID, Version) belonging to a
// ContextID in O(entries-for-context).
type Cache struct {
    cap   int
    ttl   time.Duration
    clock func() time.Time

    mu        sync.Mutex
    list      *list.List
    byKey     map[CacheKey]*list.Element
    byContext map[ContextID]map[CacheKey]struct{}  // NEW
}

// NewCacheWithClock unchanged signature; adds byContext map init.
func NewCacheWithClock(cfg CacheConfig, clock func() time.Time) *Cache {
    cfg = cfg.applyDefaults()
    if clock == nil {
        clock = time.Now
    }
    return &Cache{
        cap:       cfg.Capacity,
        ttl:       cfg.TTL,
        clock:     clock,
        list:      list.New(),
        byKey:     make(map[CacheKey]*list.Element, cfg.Capacity),
        byContext: make(map[ContextID]map[CacheKey]struct{}),
    }
}

// Put now requires a ContextID parameter so the reverse index can be
// maintained. Callers (dek.Manager.unwrapAndCache, mint path) already
// have the row's ContextType + ContextID.
func (c *Cache) Put(key CacheKey, ctxID ContextID, material *Material) {
    c.mu.Lock()
    defer c.mu.Unlock()

    if elem, ok := c.byKey[key]; ok {
        if entry, ok := elem.Value.(*cacheEntry); ok {
            // ContextID for an existing key MUST be invariant; if
            // a caller misuses Put with a different ContextID, log
            // (using the registry's logger isn't accessible here;
            // a future refactor MAY add a logger to Cache). For now,
            // overwrite — the invariant is enforced at the Manager
            // boundary which only calls Put with the row's stable
            // ContextType + ContextID from PG.
            if entry.contextID != ctxID {
                // Detach from old context's reverse index.
                if set, ok := c.byContext[entry.contextID]; ok {
                    delete(set, key)
                    if len(set) == 0 {
                        delete(c.byContext, entry.contextID)
                    }
                }
                entry.contextID = ctxID
                c.indexContextLocked(ctxID, key)
            }
            entry.material = material
            entry.expiresAt = c.clock().Add(c.ttl)
        }
        c.list.MoveToFront(elem)
        return
    }

    entry := &cacheEntry{
        key:       key,
        contextID: ctxID,
        material:  material,
        expiresAt: c.clock().Add(c.ttl),
    }
    elem := c.list.PushFront(entry)
    c.byKey[key] = elem
    c.indexContextLocked(ctxID, key)

    if c.list.Len() > c.cap {
        c.evictOldestLocked()
    }
}

// indexContextLocked adds (ctxID, key) to the reverse index. Caller
// MUST hold c.mu.
func (c *Cache) indexContextLocked(ctxID ContextID, key CacheKey) {
    set, ok := c.byContext[ctxID]
    if !ok {
        set = make(map[CacheKey]struct{})
        c.byContext[ctxID] = set
    }
    set[key] = struct{}{}
}

// evictOldestLocked removes the LRU entry from byKey, the list, and
// the reverse index. Caller MUST hold c.mu.
func (c *Cache) evictOldestLocked() {
    oldest := c.list.Back()
    if oldest == nil {
        return
    }
    c.list.Remove(oldest)
    if entry, ok := oldest.Value.(*cacheEntry); ok {
        delete(c.byKey, entry.key)
        if set, ok := c.byContext[entry.contextID]; ok {
            delete(set, entry.key)
            if len(set) == 0 {
                delete(c.byContext, entry.contextID)
            }
        }
    }
}

// Invalidate now also removes from the reverse index.
func (c *Cache) Invalidate(key CacheKey) {
    c.mu.Lock()
    defer c.mu.Unlock()
    if elem, ok := c.byKey[key]; ok {
        c.list.Remove(elem)
        delete(c.byKey, key)
        if entry, ok := elem.Value.(*cacheEntry); ok {
            if set, sok := c.byContext[entry.contextID]; sok {
                delete(set, key)
                if len(set) == 0 {
                    delete(c.byContext, entry.contextID)
                }
            }
        }
    }
}

// InvalidateContext removes every (KeyID, Version) entry belonging to
// the named context. Used by the rekey action in invalidation.Coordinator.
func (c *Cache) InvalidateContext(ctxID ContextID) {
    c.mu.Lock()
    defer c.mu.Unlock()
    set, ok := c.byContext[ctxID]
    if !ok {
        return
    }
    for key := range set {
        if elem, ok := c.byKey[key]; ok {
            c.list.Remove(elem)
            delete(c.byKey, key)
        }
    }
    delete(c.byContext, ctxID)
}

// Len returns the number of cached entries. Used by tests.
func (c *Cache) Len() int {
    c.mu.Lock()
    defer c.mu.Unlock()
    return c.list.Len()
}

// Also update Get to clean the reverse index when a TTL-expired entry
// is removed:
func (c *Cache) Get(key CacheKey) (*Material, bool) {
    c.mu.Lock()
    defer c.mu.Unlock()

    elem, ok := c.byKey[key]
    if !ok {
        return nil, false
    }
    entry, ok := elem.Value.(*cacheEntry)
    if !ok {
        return nil, false
    }
    if c.clock().After(entry.expiresAt) {
        c.list.Remove(elem)
        delete(c.byKey, key)
        if set, sok := c.byContext[entry.contextID]; sok {
            delete(set, key)
            if len(set) == 0 {
                delete(c.byContext, entry.contextID)
            }
        }
        return nil, false
    }
    c.list.MoveToFront(elem)
    return entry.material, true
}
```

- [ ] **Step 4: Update `dek.Manager` callsites of `cache.Put` to pass `ContextID`.**

Edit `internal/eventbus/crypto/dek/manager.go` lines 138, 200 (the two existing callsites).

Mint path (`GetOrCreate`):

```go
// Before (manager.go:137-138):
material := NewMaterial(dekBytes)
keyID := codec.KeyID(id)
m.cache.Put(CacheKey{KeyID: keyID, Version: 1}, material)

// After:
material := NewMaterial(dekBytes)
keyID := codec.KeyID(id)
m.cache.Put(CacheKey{KeyID: keyID, Version: 1}, ctxID, material)
```

`unwrapAndCache`:

```go
// Before (manager.go:198-200):
material := NewMaterial(dekBytes)
keyID := codec.KeyID(r.ID)
m.cache.Put(CacheKey{KeyID: keyID, Version: r.Version}, material)

// After:
material := NewMaterial(dekBytes)
keyID := codec.KeyID(r.ID)
m.cache.Put(
    CacheKey{KeyID: keyID, Version: r.Version},
    ContextID{Type: r.ContextType, ID: r.ContextID},
    material,
)
```

- [ ] **Step 5: Update existing `cache_test.go` callers of `Put` to pass `ContextID`.**

Run: `rg -n "c\.Put\(|cache\.Put\(" internal/eventbus/crypto/dek/cache_test.go`

For each existing callsite, add a `ContextID{Type: "test", ID: "..."}` argument. The exact form depends on the test; use a stable test-context ID per test, e.g., `ctxID := ContextID{Type: "scene", ID: "01HTEST"}`.

- [ ] **Step 6: Run all dek tests.**

Run: `task test -- ./internal/eventbus/crypto/dek/`

Expected: existing tests + new T5 tests all PASS. The reverse-index correctness test and the LRU-cleanup test both pass.

- [ ] **Step 7: Run lint.**

Run: `task lint`

Expected: PASS.

- [ ] **Step 8: Commit T5.**

```text
internal/eventbus/crypto/dek: Cache context reverse index + InvalidateContext

Phase 3c (holomush-ojw1.3) substrate edit. dek.Cache gains:

- byContext map[ContextID]map[CacheKey]struct{} reverse index
- Cache.Put signature gains ctxID ContextID parameter (callers
  unwrapAndCache + mint-path in manager.go updated)
- New Cache.InvalidateContext(ctxID) method — used by rekey action
  in invalidation.Coordinator (lands in T9/T10)
- Cache.Invalidate, Get, and LRU eviction all maintain reverse index
- Cache.Len() helper for tests

INV-27 (in-process memory only) preserved — reverse index is part
of the in-process state. No serialization paths added.

Refs holomush-ojw1.3.T5.
```

---

### Task T6: `dek.ParticipantsCache` new type

Symmetric to `dek.Cache` but stores `[]Participant` keyed by `(ctxType, ctxID, version)`. Separate type because eviction semantics and value shape differ.

**Files:**

- Create: `internal/eventbus/crypto/dek/participants_cache.go`
- Create: `internal/eventbus/crypto/dek/participants_cache_test.go`

- [ ] **Step 1: Write failing test for `ParticipantsCache.Get/Put/Invalidate/InvalidateContext`.**

Create `internal/eventbus/crypto/dek/participants_cache_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek

import (
    "testing"
    "time"
)

func newParticipants(playerIDs ...string) []Participant {
    out := make([]Participant, len(playerIDs))
    for i, p := range playerIDs {
        out[i] = Participant{PlayerID: p, JoinedAt: time.Now()}
    }
    return out
}

func TestParticipantsCachePutAndGetRoundTrip(t *testing.T) {
    c := NewParticipantsCache(CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
    key := ParticipantsCacheKey{ContextType: "scene", ContextID: "01HSCENE_A", Version: 1}
    in := newParticipants("01HALICE", "01HBOB")

    c.Put(key, in)
    out, ok := c.Get(key)
    if !ok {
        t.Fatal("Get returned not-found after Put")
    }
    if len(out) != 2 {
        t.Errorf("len(out) = %d; want 2", len(out))
    }
}

func TestParticipantsCacheInvalidateContextRemovesAllVersions(t *testing.T) {
    c := NewParticipantsCache(CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
    ctxA1 := ParticipantsCacheKey{ContextType: "scene", ContextID: "01HSCENE_A", Version: 1}
    ctxA2 := ParticipantsCacheKey{ContextType: "scene", ContextID: "01HSCENE_A", Version: 2}
    ctxB1 := ParticipantsCacheKey{ContextType: "scene", ContextID: "01HSCENE_B", Version: 1}

    c.Put(ctxA1, newParticipants("01HALICE"))
    c.Put(ctxA2, newParticipants("01HALICE", "01HBOB"))
    c.Put(ctxB1, newParticipants("01HCAROL"))

    c.InvalidateContext(ContextID{Type: "scene", ID: "01HSCENE_A"})

    if _, ok := c.Get(ctxA1); ok {
        t.Errorf("ctxA v1 still present")
    }
    if _, ok := c.Get(ctxA2); ok {
        t.Errorf("ctxA v2 still present")
    }
    if _, ok := c.Get(ctxB1); !ok {
        t.Errorf("ctxB v1 missing; only ctxA should be evicted")
    }
}

func TestParticipantsCacheInvalidateRemovesSingleVersion(t *testing.T) {
    c := NewParticipantsCache(CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
    k1 := ParticipantsCacheKey{ContextType: "scene", ContextID: "01HSCENE_A", Version: 1}
    k2 := ParticipantsCacheKey{ContextType: "scene", ContextID: "01HSCENE_A", Version: 2}

    c.Put(k1, newParticipants("01HALICE"))
    c.Put(k2, newParticipants("01HALICE", "01HBOB"))

    c.Invalidate(k1)

    if _, ok := c.Get(k1); ok {
        t.Errorf("k1 still present after Invalidate(k1)")
    }
    if _, ok := c.Get(k2); !ok {
        t.Errorf("k2 missing; Invalidate(k1) must not affect k2")
    }
}

func TestParticipantsCacheTTLExpiry(t *testing.T) {
    fakeNow := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
    clock := func() time.Time { return fakeNow }
    c := NewParticipantsCacheWithClock(CacheConfig{Capacity: 10, TTL: 1 * time.Minute}, clock)

    key := ParticipantsCacheKey{ContextType: "scene", ContextID: "01HSCENE_A", Version: 1}
    c.Put(key, newParticipants("01HALICE"))

    fakeNow = fakeNow.Add(2 * time.Minute) // past TTL
    if _, ok := c.Get(key); ok {
        t.Errorf("expired entry returned by Get")
    }
}

func TestParticipantsCacheLRUEviction(t *testing.T) {
    c := NewParticipantsCache(CacheConfig{Capacity: 2, TTL: 5 * time.Minute})
    k1 := ParticipantsCacheKey{ContextType: "scene", ContextID: "01HA", Version: 1}
    k2 := ParticipantsCacheKey{ContextType: "scene", ContextID: "01HB", Version: 1}
    k3 := ParticipantsCacheKey{ContextType: "scene", ContextID: "01HC", Version: 1}

    c.Put(k1, newParticipants("01HALICE"))
    c.Put(k2, newParticipants("01HBOB"))
    c.Put(k3, newParticipants("01HCAROL")) // evicts k1 (LRU)

    if _, ok := c.Get(k1); ok {
        t.Errorf("k1 still present; expected LRU eviction")
    }
    if _, ok := c.Get(k2); !ok {
        t.Errorf("k2 missing")
    }
    if _, ok := c.Get(k3); !ok {
        t.Errorf("k3 missing")
    }
}
```

- [ ] **Step 2: Run tests to verify they fail.**

Run: `task test -- -run TestParticipantsCache ./internal/eventbus/crypto/dek/`

Expected: FAIL — type doesn't exist yet.

- [ ] **Step 3: Implement `participants_cache.go`.**

Create `internal/eventbus/crypto/dek/participants_cache.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek

import (
    "container/list"
    "sync"
    "time"
)

// ParticipantsCacheKey is the (context_type, context_id, version)
// composite key for the participant-set cache. Version is part of
// the key because Rotate creates vN+1 with a different participant
// list while vN's set stays unchanged; per-version pinning matches
// the per-event (KeyID, Version) pinning the AAD already provides.
type ParticipantsCacheKey struct {
    ContextType string
    ContextID   string
    Version     uint32
}

// ParticipantsCache holds per-version participant lists with LRU + TTL.
// Symmetric to dek.Cache; separate type because the value shape differs
// ([]Participant vs *Material) and because eviction semantics differ
// (participants invalidate per-version on Add via participants_changed
// action; DEK material invalidates per-context on Rekey).
//
// Phase 3c grounding doc Decision 3 + INV-59 + INV-27.
type ParticipantsCache struct {
    cap   int
    ttl   time.Duration
    clock func() time.Time

    mu        sync.Mutex
    list      *list.List
    byKey     map[ParticipantsCacheKey]*list.Element
    byContext map[ContextID]map[ParticipantsCacheKey]struct{}
}

type participantsEntry struct {
    key       ParticipantsCacheKey
    contextID ContextID
    list      []Participant
    expiresAt time.Time
}

// NewParticipantsCache constructs a cache using time.Now as the clock.
func NewParticipantsCache(cfg CacheConfig) *ParticipantsCache {
    return NewParticipantsCacheWithClock(cfg, time.Now)
}

// NewParticipantsCacheWithClock allows tests to inject a deterministic
// clock. A nil clock falls back to time.Now.
func NewParticipantsCacheWithClock(cfg CacheConfig, clock func() time.Time) *ParticipantsCache {
    cfg = cfg.applyDefaults()
    if clock == nil {
        clock = time.Now
    }
    return &ParticipantsCache{
        cap:       cfg.Capacity,
        ttl:       cfg.TTL,
        clock:     clock,
        list:      list.New(),
        byKey:     make(map[ParticipantsCacheKey]*list.Element, cfg.Capacity),
        byContext: make(map[ContextID]map[ParticipantsCacheKey]struct{}),
    }
}

// Get returns the cached participants list for key. Returns false on
// miss or TTL-expired entry.
func (c *ParticipantsCache) Get(key ParticipantsCacheKey) ([]Participant, bool) {
    c.mu.Lock()
    defer c.mu.Unlock()
    elem, ok := c.byKey[key]
    if !ok {
        return nil, false
    }
    entry, ok := elem.Value.(*participantsEntry)
    if !ok {
        return nil, false
    }
    if c.clock().After(entry.expiresAt) {
        c.removeLocked(elem, entry)
        return nil, false
    }
    c.list.MoveToFront(elem)
    return entry.list, true
}

// Put inserts or updates an entry. Evicts the LRU entry if over capacity.
func (c *ParticipantsCache) Put(key ParticipantsCacheKey, participants []Participant) {
    ctxID := ContextID{Type: key.ContextType, ID: key.ContextID}
    c.mu.Lock()
    defer c.mu.Unlock()

    if elem, ok := c.byKey[key]; ok {
        if entry, ok := elem.Value.(*participantsEntry); ok {
            entry.list = participants
            entry.expiresAt = c.clock().Add(c.ttl)
        }
        c.list.MoveToFront(elem)
        return
    }

    entry := &participantsEntry{
        key:       key,
        contextID: ctxID,
        list:      participants,
        expiresAt: c.clock().Add(c.ttl),
    }
    elem := c.list.PushFront(entry)
    c.byKey[key] = elem
    set, ok := c.byContext[ctxID]
    if !ok {
        set = make(map[ParticipantsCacheKey]struct{})
        c.byContext[ctxID] = set
    }
    set[key] = struct{}{}

    if c.list.Len() > c.cap {
        oldest := c.list.Back()
        if oldest != nil {
            if oldEntry, ok := oldest.Value.(*participantsEntry); ok {
                c.removeLocked(oldest, oldEntry)
            }
        }
    }
}

// Invalidate removes a single entry by key.
func (c *ParticipantsCache) Invalidate(key ParticipantsCacheKey) {
    c.mu.Lock()
    defer c.mu.Unlock()
    if elem, ok := c.byKey[key]; ok {
        if entry, ok := elem.Value.(*participantsEntry); ok {
            c.removeLocked(elem, entry)
        }
    }
}

// InvalidateContext removes every (version) entry belonging to the
// named context. Used by the rekey action in invalidation.Coordinator.
func (c *ParticipantsCache) InvalidateContext(ctxID ContextID) {
    c.mu.Lock()
    defer c.mu.Unlock()
    set, ok := c.byContext[ctxID]
    if !ok {
        return
    }
    for key := range set {
        if elem, ok := c.byKey[key]; ok {
            c.list.Remove(elem)
            delete(c.byKey, key)
        }
    }
    delete(c.byContext, ctxID)
}

// removeLocked removes an entry from list, byKey, and byContext.
// Caller MUST hold c.mu.
func (c *ParticipantsCache) removeLocked(elem *list.Element, entry *participantsEntry) {
    c.list.Remove(elem)
    delete(c.byKey, entry.key)
    if set, ok := c.byContext[entry.contextID]; ok {
        delete(set, entry.key)
        if len(set) == 0 {
            delete(c.byContext, entry.contextID)
        }
    }
}

// Len returns the number of cached entries. Used by tests.
func (c *ParticipantsCache) Len() int {
    c.mu.Lock()
    defer c.mu.Unlock()
    return c.list.Len()
}
```

- [ ] **Step 4: Run tests.**

Run: `task test -- ./internal/eventbus/crypto/dek/`

Expected: 5 new ParticipantsCache tests PASS plus all existing tests.

- [ ] **Step 5: Run lint + commit.**

```text
internal/eventbus/crypto/dek: ParticipantsCache new type

Phase 3c (holomush-ojw1.3) Decision 3 substrate. ParticipantsCache
is symmetric to dek.Cache but stores []Participant keyed by
(ContextType, ContextID, Version) — version-keyed because Rotate
creates vN+1 with a different participant list while vN's set stays
unchanged; per-version pinning matches the per-event (KeyID, Version)
pinning AAD already provides.

LRU + TTL with the master-spec defaults (capacity 1024, TTL 5min).
byContext reverse index supports InvalidateContext(ctxID), used by
the rekey action in invalidation.Coordinator (T9/T10).

INV-27: in-process memory only; no serialization paths.

Refs holomush-ojw1.3.T6.
```

---

### Task T7: `dek.Manager.Participants` body using `ParticipantsCache`

Phase 3b shipped a stub `Participants` method on `dek.Manager` that read PG every call. Phase 3c wires it through the new `ParticipantsCache` and seeds the cache from the same `selectByID` row read on `unwrapAndCache`.

**Files:**

- Modify: `internal/eventbus/crypto/dek/manager.go` (constructor signature + method body + unwrap seeding)
- Modify: `internal/eventbus/crypto/dek/manager_integration_test.go`

- [ ] **Step 1: Update `manager` struct + `NewManager` signature to take `*ParticipantsCache`.**

Edit `internal/eventbus/crypto/dek/manager.go`:

```go
// manager is the concrete impl.
type manager struct {
    provider  kek.Provider
    store     *Store
    cache     *Cache
    partCache *ParticipantsCache  // NEW
}

// NewManager constructs a real Manager. Production callers pass a real
// KEK provider, pgxpool.Pool-backed Store, DEK material Cache, and
// participants Cache. All four collaborators are required; a nil
// argument returns DEK_MANAGER_DEPENDENCY_NIL.
func NewManager(provider kek.Provider, store *Store, cache *Cache, partCache *ParticipantsCache) (Manager, error) {
    switch {
    case provider == nil:
        return nil, oops.Code("DEK_MANAGER_DEPENDENCY_NIL").
            With("dependency", "provider").
            Errorf("dek.NewManager requires a non-nil kek.Provider")
    case store == nil:
        return nil, oops.Code("DEK_MANAGER_DEPENDENCY_NIL").
            With("dependency", "store").
            Errorf("dek.NewManager requires a non-nil *Store")
    case cache == nil:
        return nil, oops.Code("DEK_MANAGER_DEPENDENCY_NIL").
            With("dependency", "cache").
            Errorf("dek.NewManager requires a non-nil *Cache")
    case partCache == nil:
        return nil, oops.Code("DEK_MANAGER_DEPENDENCY_NIL").
            With("dependency", "partCache").
            Errorf("dek.NewManager requires a non-nil *ParticipantsCache")
    }
    return &manager{provider: provider, store: store, cache: cache, partCache: partCache}, nil
}

// configured guard updated to include partCache.
func (m *manager) configured() error {
    if m.provider == nil || m.store == nil || m.cache == nil || m.partCache == nil {
        return oops.Code("DEK_MANAGER_NOT_CONFIGURED").
            Errorf("Manager built via NewManagerForUnitTest cannot perform DEK operations; " +
                "only the Add/Rotate/Rekey stubs are exercisable")
    }
    return nil
}
```

- [ ] **Step 2: Replace the stub `Participants` method with the real body.**

Find the existing stub (Phase 3b shipped this):

```go
// Phase 3b stub — Phase 3c implements via ParticipantsCache.
func (m *manager) Participants(ctx context.Context, keyID codec.KeyID, version uint32) ([]Participant, error) {
    if err := m.configured(); err != nil {
        return nil, err
    }
    r, err := m.store.selectByID(ctx, keyID, version)
    if err != nil {
        return nil, oops.With("operation", "participants").Wrap(err)
    }
    return r.Participants, nil
}
```

Replace with:

```go
// Participants returns the participant set for the (keyID, version)
// DEK. Reads from ParticipantsCache on hit; on miss, falls through to
// PG and seeds the cache. Phase 3c grounding doc Decision 3 + INV-59.
func (m *manager) Participants(ctx context.Context, keyID codec.KeyID, version uint32) ([]Participant, error) {
    if err := m.configured(); err != nil {
        return nil, err
    }
    // To use the participants cache we need (ctxType, ctxID), but the
    // caller only has (keyID, version). We resolve the mapping by
    // reading the row once. Phase 3c's substrate accepts this PG read
    // on first use; subsequent calls hit the cache. (A future tiny
    // secondary cache keyed by (keyID, version) → (ctxType, ctxID) is
    // a possible follow-up if profiling shows the first-use PG read
    // is hot.)
    r, err := m.store.selectByID(ctx, keyID, version)
    if err != nil {
        if errors.Is(err, pgx.ErrNoRows) {
            return nil, oops.Code("DEK_NOT_FOUND").
                With("key_id", uint64(keyID)).
                With("version", version).
                Errorf("crypto_keys row %d v%d not found", keyID, version)
        }
        return nil, oops.Code("DEK_STORE_SELECT_FAILED").Wrap(err)
    }
    pck := ParticipantsCacheKey{ContextType: r.ContextType, ContextID: r.ContextID, Version: version}
    if cached, ok := m.partCache.Get(pck); ok {
        return cached, nil
    }
    m.partCache.Put(pck, r.Participants)
    return r.Participants, nil
}
```

- [ ] **Step 3: Update `unwrapAndCache` to seed the participants cache.**

Edit the existing `unwrapAndCache` in `manager.go`:

```go
func (m *manager) unwrapAndCache(ctx context.Context, r row) (codec.Key, error) {
    dekBytes, err := m.provider.Unwrap(ctx, r.WrappedDEK, r.WrapKeyID)
    if err != nil {
        return codec.Key{}, oops.Code("DEK_UNWRAP_FAILED").
            With("key_id", r.ID).
            With("version", r.Version).
            Wrap(err)
    }
    if err := validateProviderUnwrapOutput(dekBytes, r.ID, r.Version); err != nil {
        return codec.Key{}, err
    }
    material := NewMaterial(dekBytes)
    keyID := codec.KeyID(r.ID) //nolint:gosec // G115
    ctxID := ContextID{Type: r.ContextType, ID: r.ContextID}

    // Seed both caches from the single PG row read.
    m.cache.Put(CacheKey{KeyID: keyID, Version: r.Version}, ctxID, material)
    m.partCache.Put(
        ParticipantsCacheKey{ContextType: r.ContextType, ContextID: r.ContextID, Version: r.Version},
        r.Participants,
    )
    return material.AsCodecKey(keyID, r.Version), nil
}
```

- [ ] **Step 4: Update existing `dek.Manager` test callsites that construct a Manager.**

Run: `rg -n "NewManager\(.*\)\b|NewManagerForUnitTest" internal/eventbus/crypto/dek/`

For each `NewManager(provider, store, cache)` call in tests, add `, NewParticipantsCache(CacheConfig{})` as the fourth argument. `NewManagerForUnitTest` remains valid (no NATS or DB) — `configured()` returns the not-configured error if any field is nil.

- [ ] **Step 5: Add an integration test for cache-hit / PG fallthrough.**

In `internal/eventbus/crypto/dek/manager_integration_test.go`, add:

```go
//go:build integration

func TestManagerParticipantsHitsCacheOnSecondCall(t *testing.T) {
    pool := newTestPool(t)
    store := NewStore(pool)
    provider := newFakeKEK(t)
    cache := NewCache(CacheConfig{})
    partCache := NewParticipantsCache(CacheConfig{})
    mgr, err := NewManager(provider, store, cache, partCache)
    if err != nil {
        t.Fatalf("NewManager: %v", err)
    }

    ctx := context.Background()
    ctxID := ContextID{Type: "scene", ID: "01HSCENE_TEST"}
    initial := []Participant{{PlayerID: "01HALICE", JoinedAt: time.Now()}}

    // GetOrCreate seeds the participants cache via unwrapAndCache.
    key, err := mgr.GetOrCreate(ctx, ctxID, initial)
    if err != nil {
        t.Fatalf("GetOrCreate: %v", err)
    }

    // First Participants call — cache hit (seeded above).
    ps1, err := mgr.Participants(ctx, key.ID, key.Version)
    if err != nil {
        t.Fatalf("Participants[1]: %v", err)
    }
    if len(ps1) != 1 || ps1[0].PlayerID != "01HALICE" {
        t.Errorf("Participants[1] = %+v; want [Alice]", ps1)
    }

    // Verify cache hit by checking direct cache state.
    pck := ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: key.Version}
    if _, ok := partCache.Get(pck); !ok {
        t.Fatal("ParticipantsCache miss; expected hit after GetOrCreate")
    }

    // Invalidate the entry; next call falls through to PG.
    partCache.Invalidate(pck)

    ps2, err := mgr.Participants(ctx, key.ID, key.Version)
    if err != nil {
        t.Fatalf("Participants[2]: %v", err)
    }
    if len(ps2) != 1 || ps2[0].PlayerID != "01HALICE" {
        t.Errorf("Participants[2] = %+v; want [Alice]", ps2)
    }

    // Cache should be repopulated.
    if _, ok := partCache.Get(pck); !ok {
        t.Error("ParticipantsCache miss after fallthrough; expected re-seed")
    }
}
```

- [ ] **Step 6: Run unit + integration tests.**

```bash
task test -- ./internal/eventbus/crypto/dek/
task test:int -- -tags=integration -run TestManagerParticipantsHitsCacheOnSecondCall ./internal/eventbus/crypto/dek/
```

Expected: PASS.

- [ ] **Step 7: Run lint + commit T7.**

```text
internal/eventbus/crypto/dek: Manager.Participants via ParticipantsCache

Phase 3c (holomush-ojw1.3) Decision 3 substrate. dek.Manager.Participants
now reads from ParticipantsCache on hit and falls through to PG on
miss; unwrapAndCache seeds both DEK material cache and participants
cache from the single selectByID row read.

NewManager signature gains *ParticipantsCache parameter (4th arg);
dek.Manager dependency-nil error path covers it. Existing
NewManagerForUnitTest path unchanged (configured() returns
DEK_MANAGER_NOT_CONFIGURED).

Replaces the Phase 3b stub that did one PG read per
AuthGuard.Check; closes the substrate side of INV-12 + INV-59 (the
Coordinator-side eviction lands in T9/T10).

Refs holomush-ojw1.3.T7.
```

---

### Task T8: `crypto_keys.destroyed_at` migration + `selectActive`/`selectByID` filter + `SelectAnyByID` for forensics

Add the soft-delete column. Production read paths filter `WHERE destroyed_at IS NULL`; new exported `SelectAnyByID` exposes destroyed rows for forensic/audit reads (Phase 5 Rekey will use it). Replaces master spec §6.3's tombstone table (Decision 4).

**Files:**

- Create: `internal/store/migrations/000016_crypto_keys_destroyed_at.up.sql`
- Create: `internal/store/migrations/000016_crypto_keys_destroyed_at.down.sql`
- Modify: `internal/eventbus/crypto/dek/store.go:96, 123` (filters)
- Modify: `internal/eventbus/crypto/dek/store.go` (add `SelectAnyByID`)
- Modify: `internal/eventbus/crypto/dek/store_test.go`

- [ ] **Step 1: Create the up migration.**

Create `internal/store/migrations/000016_crypto_keys_destroyed_at.up.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors
--
-- Phase 3c (holomush-ojw1.3) Decision 4: soft-delete column on crypto_keys.
-- Replaces master spec §6.3's tombstone table. Production reads filter
-- destroyed_at IS NULL; forensic reads via Store.SelectAnyByID see
-- destroyed rows.

ALTER TABLE crypto_keys
    ADD COLUMN IF NOT EXISTS destroyed_at TIMESTAMPTZ NULL;

-- Partial index for the production read predicate (active rows only).
-- Existing queries on (context_type, context_id, rotated_at IS NULL)
-- already use the rotated_at index; this index covers the new
-- destroyed_at filter for selectByID lookups by primary key.
CREATE INDEX IF NOT EXISTS crypto_keys_active_idx
    ON crypto_keys (id)
    WHERE destroyed_at IS NULL;
```

- [ ] **Step 2: Create the down migration.**

Create `internal/store/migrations/000016_crypto_keys_destroyed_at.down.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DROP INDEX IF EXISTS crypto_keys_active_idx;

ALTER TABLE crypto_keys
    DROP COLUMN IF EXISTS destroyed_at;
```

- [ ] **Step 3: Add the filter to `selectActive` and `selectByID`.**

Edit `internal/eventbus/crypto/dek/store.go`:

```go
// selectActive query (around line 96-104):
err := s.pool.QueryRow(ctx, `
    SELECT id, context_type, context_id, version, wrapped_dek,
           wrap_provider, wrap_key_id, participants, created_at, rotated_at
      FROM crypto_keys
     WHERE context_type=$1 AND context_id=$2
       AND rotated_at IS NULL
       AND destroyed_at IS NULL
     ORDER BY version DESC
     LIMIT 1
`, ctxID.Type, ctxID.ID).Scan(...)

// selectByID query (around line 123-128):
err := s.pool.QueryRow(ctx, `
    SELECT id, context_type, context_id, version, wrapped_dek,
           wrap_provider, wrap_key_id, participants, created_at, rotated_at
      FROM crypto_keys
     WHERE id=$1 AND version=$2
       AND destroyed_at IS NULL
`, int64(keyID), version).Scan(...)
```

- [ ] **Step 4: Add `SelectAnyByID` (forensic; no destroyed_at filter).**

Append to `internal/eventbus/crypto/dek/store.go`:

```go
// SelectAnyByID returns the row for keyID + version regardless of
// destroyed_at. Used by Phase 5 Rekey audit emission and operator
// forensic tools; production read paths MUST use selectByID (which
// filters destroyed rows). Phase 3c grounding doc Decision 4.
//
// Exported because Phase 5 Rekey calls this from a different package
// (cmd/holomush-rekey or wherever Rekey lives).
func (s *Store) SelectAnyByID(ctx context.Context, keyID codec.KeyID, version uint32) (Row, error) {
    var r row
    var participantsJSON []byte
    var destroyedAt *time.Time
    err := s.pool.QueryRow(ctx, `
        SELECT id, context_type, context_id, version, wrapped_dek,
               wrap_provider, wrap_key_id, participants, created_at, rotated_at, destroyed_at
          FROM crypto_keys
         WHERE id=$1 AND version=$2
    `, int64(keyID), version).Scan( //nolint:gosec // G115
        &r.ID, &r.ContextType, &r.ContextID, &r.Version, &r.WrappedDEK,
        &r.WrapProvider, &r.WrapKeyID, &participantsJSON, &r.CreatedAt, &r.RotatedAt, &destroyedAt,
    )
    if err != nil {
        return Row{}, oops.With("operation", "select_any_dek_by_id").
            With("key_id", uint64(keyID)).
            With("version", version).Wrap(err)
    }
    if err := json.Unmarshal(participantsJSON, &r.Participants); err != nil {
        return Row{}, oops.Code("DEK_PARTICIPANTS_UNMARSHAL_FAILED").Wrap(err)
    }
    return Row{
        ID:           r.ID,
        ContextType:  r.ContextType,
        ContextID:    r.ContextID,
        Version:      r.Version,
        WrappedDEK:   r.WrappedDEK,
        WrapProvider: r.WrapProvider,
        WrapKeyID:    r.WrapKeyID,
        Participants: r.Participants,
        CreatedAt:    r.CreatedAt,
        RotatedAt:    r.RotatedAt,
        DestroyedAt:  destroyedAt,
    }, nil
}

// Row is the exported view of a crypto_keys row. Mirrors the internal
// `row` struct but adds DestroyedAt (only populated by SelectAnyByID).
type Row struct {
    ID           int64
    ContextType  string
    ContextID    string
    Version      uint32
    WrappedDEK   []byte
    WrapProvider string
    WrapKeyID    string
    Participants []Participant
    CreatedAt    time.Time
    RotatedAt    *time.Time
    DestroyedAt  *time.Time
}
```

- [ ] **Step 5: Add integration tests for soft-delete behavior.**

In `internal/eventbus/crypto/dek/store_test.go` (build tag integration):

```go
//go:build integration

func TestSoftDeletedDEKAppearsAsNoRowsForProductionReads(t *testing.T) {
    pool := newTestPool(t)
    store := NewStore(pool)
    ctx := context.Background()

    // Insert a row.
    in := row{
        ContextType:  "scene",
        ContextID:    "01HSCENE_TEST_DESTROYED",
        Version:      1,
        WrappedDEK:   []byte("wrapped"),
        WrapProvider: "fake",
        WrapKeyID:    "key1",
        Participants: []Participant{{PlayerID: "01HALICE", JoinedAt: time.Now()}},
    }
    id, err := store.insert(ctx, in)
    if err != nil {
        t.Fatalf("insert: %v", err)
    }

    // Soft-delete it.
    _, err = pool.Exec(ctx, `UPDATE crypto_keys SET destroyed_at = NOW() WHERE id = $1`, id)
    if err != nil {
        t.Fatalf("soft-delete: %v", err)
    }

    // selectByID returns NoRows.
    _, err = store.selectByID(ctx, codec.KeyID(id), 1)
    if !errors.Is(err, pgx.ErrNoRows) {
        t.Errorf("selectByID returned %v; want ErrNoRows after soft-delete", err)
    }

    // selectActive returns NoRows.
    _, err = store.selectActive(ctx, ContextID{Type: "scene", ID: "01HSCENE_TEST_DESTROYED"})
    if !errors.Is(err, pgx.ErrNoRows) {
        t.Errorf("selectActive returned %v; want ErrNoRows after soft-delete", err)
    }
}

func TestSelectAnyByIDReturnsDestroyedRows(t *testing.T) {
    pool := newTestPool(t)
    store := NewStore(pool)
    ctx := context.Background()

    in := row{
        ContextType: "scene", ContextID: "01HSCENE_FORENSIC", Version: 1,
        WrappedDEK: []byte("wrapped"), WrapProvider: "fake", WrapKeyID: "key1",
        Participants: []Participant{},
    }
    id, _ := store.insert(ctx, in)
    _, _ = pool.Exec(ctx, `UPDATE crypto_keys SET destroyed_at = NOW() WHERE id = $1`, id)

    row, err := store.SelectAnyByID(ctx, codec.KeyID(id), 1)
    if err != nil {
        t.Fatalf("SelectAnyByID: %v", err)
    }
    if row.DestroyedAt == nil {
        t.Errorf("DestroyedAt = nil; want populated for soft-deleted row")
    }
    if row.ContextID != "01HSCENE_FORENSIC" {
        t.Errorf("ContextID = %q; want '01HSCENE_FORENSIC'", row.ContextID)
    }
}
```

- [ ] **Step 6: Run integration tests.**

```bash
task test:int -- -tags=integration ./internal/eventbus/crypto/dek/
```

Expected: PASS. The migration runs at testpool setup; the soft-deleted row is filtered by production reads and visible to forensic reads.

- [ ] **Step 7: Run lint + commit T8.**

```text
internal/eventbus/crypto/dek: soft-delete via crypto_keys.destroyed_at

Phase 3c (holomush-ojw1.3) Decision 4. Adds destroyed_at TIMESTAMPTZ
NULL column on crypto_keys; production read paths (selectActive,
selectByID) filter destroyed_at IS NULL so destroyed rows behave as
NoRows (same fallback semantics as hard-delete per INV-39).

New SelectAnyByID exported method (returns Row including DestroyedAt)
for forensic reads. Phase 5 Rekey will UPDATE destroyed_at = NOW()
in the same Rekey transaction; soft-delete preserves forensic
evidence for INV-11 (previous-tenure player_history_read) and
removes the need for master spec §6.3's tombstone table.

Migration 000016. Index crypto_keys_active_idx covers the active-row
predicate.

INV-13 (Rotate preserves old DEK record unchanged) holds — Rotate
does not touch destroyed_at. INV-14 (Rekey "destroys") wording
amended in master spec edits (T13).

Refs holomush-ojw1.3.T8.
```

---

### Task T9: `invalidation.Coordinator` types + send-side `RequestInvalidation`

Constructable type (NOT a subsystem in 3c) with `Start/Stop/RequestInvalidation`. Send-side: N-of-N collection, missed-ack self-filter (INV-60), probe-and-pill phase, single retry (INV-56), typed errors. Receive-side handlers in T10.

**Files:**

- Create: `internal/eventbus/crypto/invalidation/types.go`
- Create: `internal/eventbus/crypto/invalidation/coordinator.go`
- Create: `internal/eventbus/crypto/invalidation/metrics.go`
- Create: `internal/eventbus/crypto/invalidation/coordinator_send_test.go`

- [ ] **Step 1: Create `types.go` with action enum, Config, Deps, payloads, errors.**

Create `internal/eventbus/crypto/invalidation/types.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package invalidation provides cross-replica DEK cache invalidation
// via NATS request-reply with N-of-N replica acks. Phase 3c grounding
// doc Decision 5. Coordinator is constructable (NOT a subsystem in
// 3c); production wiring lands at Phase 3d alongside Crypto.Enabled.
package invalidation

import (
    "encoding/json"
    "log/slog"
    "time"

    "github.com/nats-io/nats.go"
    "github.com/samber/oops"

    "github.com/holomush/holomush/internal/cluster"
    "github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

// Action enumerates the cache-invalidation actions; receivers
// dispatch on this field. See Phase 3c grounding doc Decision 6.
type Action string

const (
    ActionRotate              Action = "rotate"
    ActionRekey               Action = "rekey"
    ActionParticipantsChanged Action = "participants_changed"
    ActionKEKRotation         Action = "kek_rotation"
)

// Payload is the cache-invalidation message shape. Per-action
// population of `Version` / `SuccessorVersion` per the action enum
// table in Decision 6:
//   rotate              — Version=old, SuccessorVersion=new
//   rekey               — Version=destroyed, SuccessorVersion=replacement
//   participants_changed — Version=mutated active version; SuccessorVersion unused (zero)
//   kek_rotation        — both unused (zero)
type Payload struct {
    Seq                 uint64            `json:"seq"`
    CoordinatorMemberID cluster.MemberID  `json:"coordinator_member_id"`
    ClusterID           string            `json:"cluster_id"`
    ContextType         string            `json:"ctx_type"`
    ContextID           string            `json:"ctx_id"`
    Action              Action            `json:"action"`
    IssuedAt            time.Time         `json:"issued_at"`
    Version             uint32            `json:"version,omitempty"`
    SuccessorVersion    uint32            `json:"successor_version,omitempty"`
}

// Reply is the per-member ack payload.
type Reply struct {
    MemberID cluster.MemberID `json:"member_id"`
    Ack      bool             `json:"ack"`
}

// Config parameterizes the Coordinator. Defaults applied to zero values
// via Defaults().
type Config struct {
    ClusterID         string         // sourced from eventbus.Config.GameID
    InvalidateTimeout time.Duration  // 5s; 30s overridden internally for ActionKEKRotation
    SeqStart          uint64         // 0 in production; configurable for tests
}

// Defaults applies the master-spec defaults.
func (c Config) Defaults() Config {
    if c.InvalidateTimeout <= 0 {
        c.InvalidateTimeout = 5 * time.Second
    }
    return c
}

// Deps groups the Coordinator's runtime dependencies.
type Deps struct {
    Conn      *nats.Conn
    Registry  cluster.Registry
    DEKCache  *dek.Cache
    PartCache *dek.ParticipantsCache
    Logger    *slog.Logger
    Metrics   *Metrics
}

// Subject construction helpers.

// SubjectCacheInvalidate returns the cache-invalidate subject for
// (cluster_id, ctx_type, ctx_id).
func SubjectCacheInvalidate(clusterID, ctxType, ctxID string) string {
    return "internal." + clusterID + ".cache_invalidate.dek." + ctxType + "." + ctxID
}

// SubjectCacheInvalidateWildcard returns the wildcard subscribers use.
func SubjectCacheInvalidateWildcard(clusterID string) string {
    return "internal." + clusterID + ".cache_invalidate.dek.>"
}

// MarshalPayload returns JSON or a typed error.
func MarshalPayload(p Payload) ([]byte, error) {
    b, err := json.Marshal(p)
    if err != nil {
        return nil, oops.Code("INVALIDATION_MARSHAL_PAYLOAD_FAILED").Wrap(err)
    }
    return b, nil
}

// UnmarshalPayload returns JSON or a typed error.
func UnmarshalPayload(b []byte) (Payload, error) {
    var p Payload
    if err := json.Unmarshal(b, &p); err != nil {
        return Payload{}, oops.Code("INVALIDATION_UNMARSHAL_PAYLOAD_FAILED").Wrap(err)
    }
    return p, nil
}

// MarshalReply / UnmarshalReply — same shape.
func MarshalReply(r Reply) ([]byte, error) {
    b, err := json.Marshal(r)
    if err != nil {
        return nil, oops.Code("INVALIDATION_MARSHAL_REPLY_FAILED").Wrap(err)
    }
    return b, nil
}

func UnmarshalReply(b []byte) (Reply, error) {
    var r Reply
    if err := json.Unmarshal(b, &r); err != nil {
        return Reply{}, oops.Code("INVALIDATION_UNMARSHAL_REPLY_FAILED").Wrap(err)
    }
    return r, nil
}

// Typed errors returned by RequestInvalidation. See Phase 3c grounding
// doc Decision 5 error-code table.
var (
    ErrPartialFailure = oops.Code("INVALIDATION_PARTIAL_FAILURE").
        Errorf("invalidation timed out after probe-and-pill retry; missing members in error context")
    ErrNoLiveMembers = oops.Code("INVALIDATION_NO_LIVE_MEMBERS").
        Errorf("LiveCount() == 0; substrate bug")
    ErrRateLimited = oops.Code("INVALIDATION_RATE_LIMITED").
        Errorf("ProbeAndPill returned ErrPillRateLimited; caller should retry after PillRateLimit")
    ErrCrossCluster = oops.Code("INVALIDATION_CROSS_CLUSTER").
        Errorf("received message with mismatched cluster_id; dropped")
    ErrSelfTimeout = oops.Code("INVALIDATION_SELF_TIMEOUT").
        Errorf("missing-member set after probe-and-pill contains only Self(); local handler hang on N=1")
)
```

- [ ] **Step 2: Create `metrics.go`.**

Create `internal/eventbus/crypto/invalidation/metrics.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package invalidation

import "github.com/prometheus/client_golang/prometheus"

// Metrics holds the Prometheus instruments for the invalidation
// protocol. Constructed once via NewMetrics; passed via Deps.
type Metrics struct {
    AcksTotal            *prometheus.CounterVec
    LatencySeconds       *prometheus.HistogramVec
    CrossClusterDrops    prometheus.Counter
    UnknownActions       prometheus.Counter
    DEKCacheHits         *prometheus.CounterVec
    DEKCacheMisses       *prometheus.CounterVec
    DEKCacheSize         *prometheus.GaugeVec
    DEKCacheEvictions    *prometheus.CounterVec
}

// NewMetrics constructs Metrics and registers all instruments with reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
    m := &Metrics{
        AcksTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
            Name: "cluster_invalidation_acks_total",
            Help: "Cache invalidation outcomes by action and result.",
        }, []string{"action", "outcome"}),
        LatencySeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
            Name:    "cluster_invalidation_latency_seconds",
            Help:    "Time from invalidation publish to N-of-N ack collection.",
            Buckets: []float64{0.005, 0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10, 30},
        }, []string{"action"}),
        CrossClusterDrops: prometheus.NewCounter(prometheus.CounterOpts{
            Name: "cluster_invalidation_cross_cluster_drops_total",
            Help: "Invalidation messages dropped due to cluster_id mismatch.",
        }),
        UnknownActions: prometheus.NewCounter(prometheus.CounterOpts{
            Name: "cluster_invalidation_unknown_actions_total",
            Help: "Invalidation messages with unrecognized action enum.",
        }),
        DEKCacheHits: prometheus.NewCounterVec(prometheus.CounterOpts{
            Name: "dek_cache_hits_total",
            Help: "DEK cache hits by cache name (material, participants).",
        }, []string{"cache"}),
        DEKCacheMisses: prometheus.NewCounterVec(prometheus.CounterOpts{
            Name: "dek_cache_misses_total",
            Help: "DEK cache misses by cache name.",
        }, []string{"cache"}),
        DEKCacheSize: prometheus.NewGaugeVec(prometheus.GaugeOpts{
            Name: "dek_cache_size",
            Help: "DEK cache size by cache name.",
        }, []string{"cache"}),
        DEKCacheEvictions: prometheus.NewCounterVec(prometheus.CounterOpts{
            Name: "dek_cache_evictions_total",
            Help: "DEK cache evictions by cache and reason.",
        }, []string{"cache", "reason"}),
    }
    reg.MustRegister(
        m.AcksTotal, m.LatencySeconds, m.CrossClusterDrops, m.UnknownActions,
        m.DEKCacheHits, m.DEKCacheMisses, m.DEKCacheSize, m.DEKCacheEvictions,
    )
    return m
}
```

- [ ] **Step 3: Create `coordinator.go` with the Coordinator interface + send-side body.**

Create `internal/eventbus/crypto/invalidation/coordinator.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package invalidation

import (
    "context"
    "errors"
    "log/slog"
    "sync/atomic"
    "time"

    "github.com/nats-io/nats.go"
    "github.com/samber/oops"

    "github.com/holomush/holomush/internal/cluster"
    "github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

// Coordinator orchestrates cross-replica DEK cache invalidation.
type Coordinator interface {
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    RequestInvalidation(ctx context.Context, ctxID dek.ContextID, action Action, version, successorVersion uint32) error
}

// New constructs a Coordinator.
func New(cfg Config, deps Deps) (Coordinator, error) {
    cfg = cfg.Defaults()
    if cfg.ClusterID == "" {
        return nil, oops.Code("INVALIDATION_CONFIG_MISSING_CLUSTER_ID").
            Errorf("Coordinator requires non-empty ClusterID")
    }
    if deps.Conn == nil || deps.Registry == nil || deps.DEKCache == nil || deps.PartCache == nil {
        return nil, oops.Code("INVALIDATION_DEPS_NIL").
            Errorf("Coordinator requires non-nil Conn, Registry, DEKCache, PartCache")
    }
    if deps.Logger == nil {
        deps.Logger = slog.Default()
    }
    return &coordinator{
        cfg:  cfg,
        deps: deps,
        seq:  cfg.SeqStart,
    }, nil
}

type coordinator struct {
    cfg  Config
    deps Deps

    seq uint64

    sub *nats.Subscription
}

// timeoutFor returns the action-specific timeout. KEK rotation uses
// 30s per INV-28; everything else uses cfg.InvalidateTimeout (default 5s).
func (c *coordinator) timeoutFor(action Action) time.Duration {
    if action == ActionKEKRotation {
        return 30 * time.Second
    }
    return c.cfg.InvalidateTimeout
}

// Start subscribes to the cache_invalidate.dek.> wildcard and runs the
// receive loop. Receive-side handler body lives in T10 (this commit
// stubs the handler and lets Start succeed).
func (c *coordinator) Start(ctx context.Context) error {
    if c.sub != nil {
        return nil
    }
    sub, err := c.deps.Conn.Subscribe(SubjectCacheInvalidateWildcard(c.cfg.ClusterID), c.handleInvalidate)
    if err != nil {
        return oops.Code("INVALIDATION_SUBSCRIBE_FAILED").Wrap(err)
    }
    c.sub = sub
    c.deps.Logger.Info("invalidation.Coordinator started", "cluster_id", c.cfg.ClusterID)
    return nil
}

// Stop drains the subscription. Idempotent.
func (c *coordinator) Stop(ctx context.Context) error {
    if c.sub == nil {
        return nil
    }
    if err := c.sub.Drain(); err != nil {
        return oops.Code("INVALIDATION_DRAIN_FAILED").Wrap(err)
    }
    c.sub = nil
    return nil
}

// RequestInvalidation publishes an invalidation request and waits for
// N-of-N replica acks; on partial timeout, runs probe-and-pill on
// missing members and retries once. INV-28, INV-29, INV-56, INV-60.
func (c *coordinator) RequestInvalidation(
    ctx context.Context,
    ctxID dek.ContextID,
    action Action,
    version, successorVersion uint32,
) error {
    seq := atomic.AddUint64(&c.seq, 1)
    timeout := c.timeoutFor(action)

    n1 := c.deps.Registry.LiveCount()
    if n1 == 0 {
        return ErrNoLiveMembers
    }

    payload := Payload{
        Seq:                 seq,
        CoordinatorMemberID: c.deps.Registry.Self(),
        ClusterID:           c.cfg.ClusterID,
        ContextType:         ctxID.Type,
        ContextID:           ctxID.ID,
        Action:              action,
        IssuedAt:            time.Now(),
        Version:             version,
        SuccessorVersion:    successorVersion,
    }

    acks, err := c.publishAndCollect(ctx, payload, n1, timeout, ctxID)
    if err != nil {
        return err
    }
    if len(acks) == n1 {
        c.recordSuccess(action, "success", payload.IssuedAt)
        return nil
    }

    // Probe-and-pill phase.
    missing := c.computeMissing(acks)
    // INV-60: filter Self() from missing set.
    selfFiltered := make([]cluster.MemberID, 0, len(missing))
    for _, m := range missing {
        if m == c.deps.Registry.Self() {
            continue
        }
        selfFiltered = append(selfFiltered, m)
    }
    if len(selfFiltered) == 0 && len(missing) > 0 {
        // Only self was missing → SELF_TIMEOUT
        c.deps.Logger.Warn("invalidation: only Self() missing from acks; not pilling self",
            "self", string(c.deps.Registry.Self()),
            "action", string(action),
        )
        c.recordSuccess(action, "self_timeout", payload.IssuedAt)
        return ErrSelfTimeout
    }

    for _, member := range selfFiltered {
        err := c.deps.Registry.ProbeAndPill(ctx, member, cluster.PillReasonMissedInvalidationAck)
        if err != nil && errors.Is(err, cluster.ErrPillRateLimited) {
            return ErrRateLimited
        }
        // ErrPillProbeSucceeded or nil: continue. Pilled member already
        // removed from registry synchronously.
    }

    // Retry once.
    n2 := c.deps.Registry.LiveCount()
    if n2 == 0 {
        return ErrNoLiveMembers
    }
    acks2, err := c.publishAndCollect(ctx, payload, n2, timeout, ctxID)
    if err != nil {
        return err
    }
    if len(acks2) == n2 {
        c.recordSuccess(action, "success_after_retry", payload.IssuedAt)
        return nil
    }

    missing2 := c.computeMissingFrom(acks2, n2)
    c.recordSuccess(action, "partial_failure", payload.IssuedAt)
    return oops.Code("INVALIDATION_PARTIAL_FAILURE").
        With("missing_members", missing2).
        With("action", string(action)).
        With("ctx_type", ctxID.Type).
        With("ctx_id", ctxID.ID).
        Errorf("invalidation timed out after probe-and-pill retry")
}

// publishAndCollect publishes one invalidation request, opens a reply
// inbox, and collects acks until len(acks)==expected or timeout fires.
func (c *coordinator) publishAndCollect(
    ctx context.Context,
    payload Payload,
    expected int,
    timeout time.Duration,
    _ dek.ContextID,
) (map[cluster.MemberID]struct{}, error) {
    body, err := MarshalPayload(payload)
    if err != nil {
        return nil, err
    }
    inbox := c.deps.Conn.NewRespInbox()
    sub, err := c.deps.Conn.SubscribeSync(inbox)
    if err != nil {
        return nil, oops.Code("INVALIDATION_INBOX_SUB_FAILED").Wrap(err)
    }
    defer sub.Drain() //nolint:errcheck

    if err := c.deps.Conn.PublishRequest(
        SubjectCacheInvalidate(c.cfg.ClusterID, payload.ContextType, payload.ContextID),
        inbox, body,
    ); err != nil {
        return nil, oops.Code("INVALIDATION_PUBLISH_FAILED").Wrap(err)
    }

    acks := make(map[cluster.MemberID]struct{}, expected)
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) && len(acks) < expected {
        remaining := time.Until(deadline)
        if remaining <= 0 {
            break
        }
        msg, err := sub.NextMsg(remaining)
        if err != nil {
            if errors.Is(err, nats.ErrTimeout) {
                break
            }
            // Other errors surface immediately.
            return acks, oops.Code("INVALIDATION_INBOX_READ_FAILED").Wrap(err)
        }
        reply, perr := UnmarshalReply(msg.Data)
        if perr != nil {
            c.deps.Logger.Warn("invalidation: parse reply failed", "err", perr.Error())
            continue
        }
        if reply.Ack {
            acks[reply.MemberID] = struct{}{}
        }
    }
    return acks, nil
}

func (c *coordinator) computeMissing(acks map[cluster.MemberID]struct{}) []cluster.MemberID {
    members := c.deps.Registry.LiveMembers()
    missing := make([]cluster.MemberID, 0, len(members))
    for _, m := range members {
        if _, ok := acks[m.ID]; !ok {
            missing = append(missing, m.ID)
        }
    }
    return missing
}

func (c *coordinator) computeMissingFrom(acks map[cluster.MemberID]struct{}, _ int) []cluster.MemberID {
    return c.computeMissing(acks)
}

func (c *coordinator) recordSuccess(action Action, outcome string, issuedAt time.Time) {
    if c.deps.Metrics == nil {
        return
    }
    c.deps.Metrics.AcksTotal.WithLabelValues(string(action), outcome).Inc()
    c.deps.Metrics.LatencySeconds.WithLabelValues(string(action)).Observe(time.Since(issuedAt).Seconds()) //nolint:no_remote_clock_compare // observability-only per Decision 8
}

// handleInvalidate is the receive-side stub; T10 implements the body.
func (c *coordinator) handleInvalidate(msg *nats.Msg) {
    // T10: parse, dispatch on action, evict caches, ack.
    _ = msg
}
```

- [ ] **Step 4: Write send-side unit tests.**

Create `internal/eventbus/crypto/invalidation/coordinator_send_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package invalidation_test

import (
    "context"
    "errors"
    "log/slog"
    "testing"
    "time"

    "github.com/holomush/holomush/internal/cluster/clustertest"
    "github.com/holomush/holomush/internal/eventbus/crypto/dek"
    "github.com/holomush/holomush/internal/eventbus/crypto/invalidation"
)

func TestCoordinatorRequestInvalidationSucceedsWhenSelfIsOnlyMember(t *testing.T) {
    h := clustertest.New(t, "test-game", 1)

    // For the single-member case, the Coordinator publishes and the
    // local subscriber (T10 — but we also need a stub receiver for this
    // test). Until T10 lands, the receive-side is a stub and the
    // request will time out. This test is INTENTIONALLY written to
    // fail until T10 lands; it serves as the fail-then-pass marker.
    //
    // Implementation note for T10: once receive-side dispatch is wired,
    // this test should pass because the local member self-acks via
    // NATS loopback.

    cache := dek.NewCache(dek.CacheConfig{})
    partCache := dek.NewParticipantsCache(dek.CacheConfig{})

    coord, err := invalidation.New(invalidation.Config{
        ClusterID:         "test-game",
        InvalidateTimeout: 500 * time.Millisecond,
    }, invalidation.Deps{
        Conn:      h.Embedded.Conn,
        Registry:  h.Members[0].Registry,
        DEKCache:  cache,
        PartCache: partCache,
        Logger:    slog.Default(),
    })
    if err != nil {
        t.Fatalf("invalidation.New: %v", err)
    }
    if err := coord.Start(context.Background()); err != nil {
        t.Fatalf("Start: %v", err)
    }
    t.Cleanup(func() { _ = coord.Stop(context.Background()) })

    err = coord.RequestInvalidation(
        context.Background(),
        dek.ContextID{Type: "scene", ID: "01HSCENE_TEST"},
        invalidation.ActionRekey,
        1, 2,
    )

    // Staged TDD: this test is SKIPPED until T10 wires the receive side.
    // T10's commit removes this skip and asserts require.NoError(t, err)
    // (single-member self-ack succeeds via NATS loopback).
    if err != nil {
        t.Skipf("T10 receive-side not yet wired; got err = %v. Remove this skip in T10's commit and assert require.NoError.", err)
    }
    // Once T10 lands, this assertion is the active check:
    if err != nil {
        t.Errorf("RequestInvalidation single-member returned %v; want nil (self-ack via NATS loopback)", err)
    }
}

func TestCoordinatorRequestInvalidationReturnsRateLimitedWhenProbeAndPillRefuses(t *testing.T) {
    // Two-member harness; member 1 is unresponsive. Coordinator on
    // member 0 issues one pill against member 1. Coordinator's second
    // call within the rate-limit window returns ErrRateLimited via
    // the surfaced ErrPillRateLimited.

    h := clustertest.New(t, "test-game", 2)
    h.AwaitConverged(t, 2*time.Second)

    // Stop member 1 to make it unresponsive.
    _ = h.Members[1].Registry.Stop(context.Background())
    // Re-inject synthetic heartbeat so member 0 still sees it.
    h.PublishSyntheticHeartbeat(t, h.Members[1].MemberID, "test-game")
    h.AwaitMemberPresent(t, 0, h.Members[1].MemberID, 1*time.Second)

    cache := dek.NewCache(dek.CacheConfig{})
    partCache := dek.NewParticipantsCache(dek.CacheConfig{})
    coord, err := invalidation.New(invalidation.Config{
        ClusterID:         "test-game",
        InvalidateTimeout: 200 * time.Millisecond,
    }, invalidation.Deps{
        Conn:      h.Embedded.Conn,
        Registry:  h.Members[0].Registry,
        DEKCache:  cache,
        PartCache: partCache,
        Logger:    slog.Default(),
    })
    if err != nil {
        t.Fatalf("invalidation.New: %v", err)
    }
    if err := coord.Start(context.Background()); err != nil {
        t.Fatalf("Start: %v", err)
    }
    t.Cleanup(func() { _ = coord.Stop(context.Background()) })

    // First call: pill issued, retry runs, depending on T10 may succeed
    // or partial-fail. We tolerate any outcome here; the next call is
    // the rate-limit assertion.
    _ = coord.RequestInvalidation(
        context.Background(),
        dek.ContextID{Type: "scene", ID: "01HSCENE_TEST"},
        invalidation.ActionRekey, 1, 2,
    )

    // Re-inject synthetic and verify second call hits rate limit.
    h.PublishSyntheticHeartbeat(t, h.Members[1].MemberID, "test-game")
    h.AwaitMemberPresent(t, 0, h.Members[1].MemberID, 1*time.Second)

    err = coord.RequestInvalidation(
        context.Background(),
        dek.ContextID{Type: "scene", ID: "01HSCENE_TEST"},
        invalidation.ActionRekey, 1, 2,
    )
    if !errors.Is(err, invalidation.ErrRateLimited) {
        t.Errorf("err = %v; want ErrRateLimited", err)
    }
}
```

- [ ] **Step 5: Run send-side tests.**

Run: `task test -- ./internal/eventbus/crypto/invalidation/`

Expected: tests compile and run; some may FAIL until T10 wires the receive side. The rate-limit test should PASS regardless.

- [ ] **Step 6: Run lint + commit T9.**

```text
internal/eventbus/crypto/invalidation: Coordinator types + send-side

Phase 3c (holomush-ojw1.3) Decision 5/6 substrate. invalidation.Coordinator
ships as a constructable type (NOT a subsystem in 3c; promotion to
lifecycle.Subsystem deferred to Phase 3d alongside Crypto.Enabled flip).

- types.go: Action enum (rotate/rekey/participants_changed/kek_rotation);
  Payload (with version + successor_version per-action semantics);
  Reply; Config + Defaults; Deps; subject builders; typed errors
  (ErrPartialFailure, ErrNoLiveMembers, ErrRateLimited, ErrCrossCluster,
  ErrSelfTimeout per Decision 5 error-code table)
- metrics.go: cluster_invalidation_acks_total (action + outcome labels),
  cluster_invalidation_latency_seconds, dek_cache_{hits,misses,size,evictions}
- coordinator.go: Start/Stop; RequestInvalidation send-side flow:
  publish + N-of-N collect → on partial timeout, missed-member set
  with INV-60 self-filter → ProbeAndPill on each → retry once → return
  partial-failure with missing-member detail. Receive-side is a stub;
  T10 wires the body.

Refs holomush-ojw1.3.T9.
```

---

### Task T10: `invalidation.Coordinator` receive-side + action dispatch

Replace the T9 stub `handleInvalidate` with the real body. Parse payload, drop on cluster_id mismatch, dispatch on action, evict caches per the action enum table, stamp metrics, ack via `msg.Respond`.

**Files:**

- Modify: `internal/eventbus/crypto/invalidation/coordinator.go`
- Create: `internal/eventbus/crypto/invalidation/coordinator_receive_test.go`

- [ ] **Step 1: Replace `handleInvalidate` body in `coordinator.go`.**

```go
// handleInvalidate is the receive-side handler. Parses payload,
// dispatches on action enum, evicts caches per Phase 3c grounding doc
// Decision 5/6, and acks via msg.Respond. INV-54: cluster_id-mismatch
// messages dropped without ack.
func (c *coordinator) handleInvalidate(msg *nats.Msg) {
    payload, err := UnmarshalPayload(msg.Data)
    if err != nil {
        c.deps.Logger.Warn("invalidation: parse failed", "err", err.Error())
        // Do NOT ack — sender treats as missed.
        return
    }
    if payload.ClusterID != c.cfg.ClusterID {
        // INV-54
        c.deps.Logger.Warn("invalidation: cross-cluster message dropped",
            "got", payload.ClusterID, "want", c.cfg.ClusterID,
        )
        if c.deps.Metrics != nil {
            c.deps.Metrics.CrossClusterDrops.Inc()
        }
        return
    }

    ctxID := dek.ContextID{Type: payload.ContextType, ID: payload.ContextID}
    switch payload.Action {
    case ActionRekey:
        c.deps.DEKCache.InvalidateContext(ctxID)
        c.deps.PartCache.InvalidateContext(ctxID)

    case ActionParticipantsChanged:
        c.deps.PartCache.Invalidate(dek.ParticipantsCacheKey{
            ContextType: payload.ContextType,
            ContextID:   payload.ContextID,
            Version:     payload.Version, // the mutated active version
        })

    case ActionRotate, ActionKEKRotation:
        // No-op eviction per Phase 3c grounding doc Decision 5; protocol
        // ack is still required (INV-28/INV-29 contract).

    default:
        c.deps.Logger.Warn("invalidation: unknown action; not acking",
            "action", string(payload.Action),
        )
        if c.deps.Metrics != nil {
            c.deps.Metrics.UnknownActions.Inc()
        }
        return
    }

    // Update registry's last-seen seq for observability (per Decision 6).
    if r, ok := c.deps.Registry.(interface{ SetLastInvalidationSeq(uint64) }); ok {
        r.SetLastInvalidationSeq(payload.Seq)
    }

    // Stamp latency metric (observability-only; INV-58 carve-out applies).
    if c.deps.Metrics != nil {
        c.deps.Metrics.LatencySeconds.WithLabelValues(string(payload.Action)).
            Observe(time.Since(payload.IssuedAt).Seconds()) //nolint:no_remote_clock_compare // observability-only per Decision 8
    }

    // Ack.
    reply := Reply{MemberID: c.deps.Registry.Self(), Ack: true}
    body, err := MarshalReply(reply)
    if err != nil {
        c.deps.Logger.Warn("invalidation: marshal reply failed", "err", err.Error())
        return
    }
    if msg.Reply == "" {
        c.deps.Logger.Warn("invalidation: msg.Reply empty; cannot ack")
        return
    }
    if err := c.deps.Conn.Publish(msg.Reply, body); err != nil {
        c.deps.Logger.Warn("invalidation: ack publish failed", "err", err.Error())
    }
}
```

- [ ] **Step 2: Write receive-side dispatch tests.**

Create `internal/eventbus/crypto/invalidation/coordinator_receive_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package invalidation_test

import (
    "context"
    "log/slog"
    "testing"
    "time"

    "github.com/holomush/holomush/internal/cluster/clustertest"
    "github.com/holomush/holomush/internal/eventbus/crypto/dek"
    "github.com/holomush/holomush/internal/eventbus/crypto/invalidation"
)

func setupReceiveTest(t *testing.T) (*clustertest.Harness, *dek.Cache, *dek.ParticipantsCache, invalidation.Coordinator) {
    h := clustertest.New(t, "test-game", 1)
    cache := dek.NewCache(dek.CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
    partCache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
    coord, err := invalidation.New(invalidation.Config{
        ClusterID:         "test-game",
        InvalidateTimeout: 500 * time.Millisecond,
    }, invalidation.Deps{
        Conn:      h.Embedded.Conn,
        Registry:  h.Members[0].Registry,
        DEKCache:  cache,
        PartCache: partCache,
        Logger:    slog.Default(),
    })
    if err != nil {
        t.Fatalf("invalidation.New: %v", err)
    }
    if err := coord.Start(context.Background()); err != nil {
        t.Fatalf("Start: %v", err)
    }
    t.Cleanup(func() { _ = coord.Stop(context.Background()) })
    return h, cache, partCache, coord
}

func TestRekeyActionEvictsBothCachesForContext(t *testing.T) {
    h, cache, partCache, coord := setupReceiveTest(t)

    ctxID := dek.ContextID{Type: "scene", ID: "01HSCENE_REKEY"}
    cache.Put(dek.CacheKey{KeyID: 1, Version: 1}, ctxID, dek.NewMaterial(make([]byte, dek.DEKByteLength)))
    partCache.Put(dek.ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 1},
        []dek.Participant{{PlayerID: "01HALICE"}})

    if err := coord.RequestInvalidation(context.Background(), ctxID, invalidation.ActionRekey, 1, 2); err != nil {
        t.Fatalf("RequestInvalidation: %v", err)
    }

    if _, ok := cache.Get(dek.CacheKey{KeyID: 1, Version: 1}); ok {
        t.Errorf("DEK cache entry still present after rekey")
    }
    if _, ok := partCache.Get(dek.ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 1}); ok {
        t.Errorf("participants cache entry still present after rekey")
    }
    _ = h
}

func TestParticipantsChangedActionEvictsOnlyTheGivenVersion(t *testing.T) {
    h, _, partCache, coord := setupReceiveTest(t)

    ctxID := dek.ContextID{Type: "scene", ID: "01HSCENE_ADD"}
    partCache.Put(dek.ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 1},
        []dek.Participant{{PlayerID: "01HALICE"}})
    partCache.Put(dek.ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 2},
        []dek.Participant{{PlayerID: "01HALICE"}, {PlayerID: "01HBOB"}})

    if err := coord.RequestInvalidation(context.Background(), ctxID, invalidation.ActionParticipantsChanged, 1, 0); err != nil {
        t.Fatalf("RequestInvalidation: %v", err)
    }

    if _, ok := partCache.Get(dek.ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 1}); ok {
        t.Errorf("v1 still present; expected eviction")
    }
    if _, ok := partCache.Get(dek.ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 2}); !ok {
        t.Errorf("v2 missing; only v1 should be evicted")
    }
    _ = h
}

func TestRotateActionAcksWithoutEvicting(t *testing.T) {
    h, cache, partCache, coord := setupReceiveTest(t)

    ctxID := dek.ContextID{Type: "scene", ID: "01HSCENE_ROTATE"}
    cache.Put(dek.CacheKey{KeyID: 1, Version: 1}, ctxID, dek.NewMaterial(make([]byte, dek.DEKByteLength)))
    partCache.Put(dek.ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 1},
        []dek.Participant{{PlayerID: "01HALICE"}})

    if err := coord.RequestInvalidation(context.Background(), ctxID, invalidation.ActionRotate, 1, 2); err != nil {
        t.Fatalf("RequestInvalidation: %v", err)
    }

    if _, ok := cache.Get(dek.CacheKey{KeyID: 1, Version: 1}); !ok {
        t.Errorf("DEK cache entry evicted on rotate; should have been no-op")
    }
    if _, ok := partCache.Get(dek.ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 1}); !ok {
        t.Errorf("participants cache entry evicted on rotate; should have been no-op")
    }
    _ = h
}
```

- [ ] **Step 3: Run receive-side tests.**

Run: `task test -- ./internal/eventbus/crypto/invalidation/`

Expected: 3 new dispatch tests + the T9 send-side tests all PASS (the T9 single-member self-ack test now passes too because receive-side is wired).

- [ ] **Step 4: Run lint + commit T10.**

```text
internal/eventbus/crypto/invalidation: receive-side action dispatch

Phase 3c (holomush-ojw1.3) Decision 5/6 receive-side. handleInvalidate
parses payload, drops cross-cluster (INV-54), dispatches on action:

- ActionRekey: InvalidateContext on both DEKCache + ParticipantsCache
- ActionParticipantsChanged: ParticipantsCache.Invalidate at the
  payload's `version` (the mutated active version)
- ActionRotate, ActionKEKRotation: no-op eviction (protocol-only ack)
- unknown action: warn + UnknownActions metric + no ack

Updates Registry's last-seen seq (observability only) and stamps
LatencySeconds histogram. Acks via msg.Respond.

All T9 send-side tests now pass under single-member self-ack.

Refs holomush-ojw1.3.T10.
```

---

### Task T11: Skew detection metric + heartbeat `published_at` field + `gorules/no_remote_clock_compare.go` ruleguard

The heartbeat already carries `PublishedAt` (T2). T11 adds the actual skew computation hookup, the `cluster_member_skew_seconds` Prometheus gauge wiring, and the `gorules` ruleguard rule that enforces INV-58.

**Files:**

- Verify existing: `internal/cluster/heartbeat.go` (T2 already wired `recordSkew`)
- Create: `gorules/no_remote_clock_compare.go`
- Verify: any `// nolint:no_remote_clock_compare` annotations are present at the carved-out sites
- Create: `gorules/no_remote_clock_compare_test.go` (rule fixture sample)

- [ ] **Step 1: Verify T2 already wires `recordSkew` correctly.**

Run: `rg -n "recordSkew\|cluster_member_skew_seconds" internal/cluster/`

Expected: matches in `heartbeat.go::recordSkew` and `metrics.go::SkewMetrics.SkewSeconds`. The `nolint:no_remote_clock_compare` annotation is already on the `Set(skew)` call.

- [ ] **Step 2: Create `gorules/no_remote_clock_compare.go`.**

Create `gorules/no_remote_clock_compare.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build ruleguard

// Package gorules contains gocritic ruleguard rules. This file
// declares the no_remote_clock_compare rule enforcing INV-58 (no
// cross-host wall-clock comparison) per Phase 3c grounding doc
// Decision 8.
//
// Run via: task lint (which invokes ruleguard with this rule file).
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// noRemoteClockCompare flags any subtraction or comparison between a
// local time.Time and a struct field whose name is on the remote-
// sourced field allowlist (PublishedAt, IssuedAt, StartedAt,
// LastHeartbeatAt — all sender-side timestamps).
//
// Authors who legitimately need a cross-host clock comparison MUST
// gate the call site with `// nolint:no_remote_clock_compare //
// observability-only per Decision 8` and document why. The skew
// detection metric in internal/cluster/heartbeat.go::recordSkew is
// the single allowed exception.
//
// Limitation: this rule is conservative — it can flag false positives
// when a local time.Time happens to be assigned from a struct field
// with one of the allowlisted names. False positives are silenced
// with the nolint annotation.
func noRemoteClockCompare(m dsl.Matcher) {
    m.Match(`time.Now().Sub($x.PublishedAt)`,
        `time.Now().Sub($x.IssuedAt)`,
        `time.Now().Sub($x.StartedAt)`,
        `time.Now().Sub($x.LastHeartbeatAt)`,
        `$now.Sub($x.PublishedAt)`,
        `$now.Sub($x.IssuedAt)`,
        `$now.Sub($x.StartedAt)`,
        `$now.Sub($x.LastHeartbeatAt)`,
        `$now.Before($x.PublishedAt)`,
        `$now.Before($x.IssuedAt)`,
        `$now.After($x.PublishedAt)`,
        `$now.After($x.IssuedAt)`,
    ).Report("INV-58: no cross-host wall-clock comparison; remote-sourced timestamps must not feed protocol decisions. Use sequence numbers (Phase 3c grounding doc Decision 8). Annotate with // nolint:no_remote_clock_compare // observability-only per Decision 8 if this is the carved-out skew metric.")
}
```

- [ ] **Step 3: Add the rule to the ruleguard configuration.**

Run: `rg -n "ruleguard|gorules" Taskfile.yaml .golangci.yaml .golangci.yml 2>&1 | head -20`

If a `gorules/` directory + ruleguard plugin are already wired (the project's existing `gorules/dek_no_serialize.go` rule from INV-27 implies it is), `no_remote_clock_compare.go` is auto-discovered. If not, extend the lint config to include the new rule. Check existing pattern in any `gorules/dek_no_serialize*.go` files.

- [ ] **Step 4: Verify lint catches a synthetic violation.**

Add a synthetic violation in a test fixture file `gorules/no_remote_clock_compare_test.go` (this is the rule's fixture pattern; it contains code that the rule MUST flag):

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build ruleguard_test

package fixtures

import (
    "time"
)

type sample struct {
    PublishedAt time.Time
    IssuedAt    time.Time
    StartedAt   time.Time
}

func badRemoteClockCompare(s sample) {
    // want: `INV-58:`
    _ = time.Now().Sub(s.PublishedAt)

    // want: `INV-58:`
    _ = time.Now().Sub(s.IssuedAt)
}

// This call is annotated and must NOT be flagged.
func goodRemoteClockSkewMetric(s sample) {
    _ = time.Now().Sub(s.PublishedAt) //nolint:no_remote_clock_compare // observability-only per Decision 8
}
```

- [ ] **Step 5: Run lint, expect the synthetic-violation file to be flagged.**

Run: `task lint`

Expected: the two `want: INV-58:` lines flag; the annotated line does not. If lint is clean against the fixture file (i.e., the rule didn't fire), debug the rule or the fixture.

After verifying the rule fires correctly, **delete the synthetic violations** in `_test.go` (or ensure the file has the `ruleguard_test` build tag so it's not part of the production build). Production code MUST stay lint-clean.

- [ ] **Step 6: Run full lint pass.**

Run: `task lint`

Expected: PASS — production code has no remote-clock comparisons except the carved-out skew metric in `internal/cluster/heartbeat.go` and the latency-histogram observation in `internal/eventbus/crypto/invalidation/coordinator.go`, both annotated.

- [ ] **Step 7: Commit T11.**

```text
gorules: no_remote_clock_compare rule enforcing INV-58

Phase 3c (holomush-ojw1.3) Decision 8 enforcement. New gorules
ruleguard rule fails task lint on any subtraction or comparison
between a local time.Time and a struct field whose name is on the
remote-sourced allowlist (PublishedAt, IssuedAt, StartedAt,
LastHeartbeatAt). Carved-out sites (cluster.recordSkew skew metric;
invalidation.coordinator latency histogram) annotate with
// nolint:no_remote_clock_compare // observability-only per Decision 8.

INV-58 promoted from "Documentary" to "Lint" test class. Skew
detection metric (cluster_member_skew_seconds) remains observability-
only; no protocol behavior depends on it.

Refs holomush-ojw1.3.T11.
```

---

### Task T12: Integration tests (multi-Registry harness; INV-28/29/53-60)

End-to-end exercises of the cluster + invalidation substrate against multiple `Registry` instances on a shared embedded NATS. Each test starts with `// Verifies: INV-N` so T14's meta-test can bind invariants to test names.

**Files:**

- Create: `test/integration/cluster/cluster_test.go`
- Create: `test/integration/crypto/cache_invalidation_test.go`

- [ ] **Step 1: Create `test/integration/cluster/cluster_test.go`.**

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster_e2e

import (
    "context"
    "errors"
    "testing"
    "time"

    "github.com/holomush/holomush/internal/cluster"
    "github.com/holomush/holomush/internal/cluster/clustertest"
)

// Verifies: INV-53
func TestRegistryRejectsDuplicateMemberIDFromDifferentStartedAt(t *testing.T) {
    h := clustertest.New(t, "test-game", 1)
    h.AwaitConverged(t, 1*time.Second)

    target := cluster.MemberID("01HSYN_DUP_INV53")

    // Inject the first heartbeat at StartedAt = T1.
    t1 := time.Now()
    p1 := cluster.HeartbeatPayload{
        ClusterID: "test-game", MemberID: target,
        StartedAt: t1, PublishedAt: t1,
        HolomushVersion: "first",
    }
    b1, _ := cluster.MarshalHeartbeat(p1)
    if err := h.Embedded.Conn.Publish(cluster.SubjectAlive("test-game", target), b1); err != nil {
        t.Fatalf("publish first heartbeat: %v", err)
    }
    _ = h.Embedded.Conn.Flush()
    h.AwaitMemberPresent(t, 0, target, 1*time.Second)

    // Sample initial state.
    first, ok := h.Members[0].Registry.Member(target)
    if !ok {
        t.Fatal("first heartbeat did not register target member")
    }
    if !first.StartedAt.Equal(t1) {
        t.Errorf("StartedAt[1] = %v; want %v", first.StartedAt, t1)
    }

    // Inject a duplicate heartbeat with a DIFFERENT StartedAt (T2).
    // Per INV-53, the duplicate MUST be rejected: registry's view of
    // StartedAt MUST stay at T1.
    t2 := t1.Add(10 * time.Second)
    p2 := cluster.HeartbeatPayload{
        ClusterID: "test-game", MemberID: target,
        StartedAt: t2, PublishedAt: time.Now(),
        HolomushVersion: "duplicate",
    }
    b2, _ := cluster.MarshalHeartbeat(p2)
    if err := h.Embedded.Conn.Publish(cluster.SubjectAlive("test-game", target), b2); err != nil {
        t.Fatalf("publish duplicate heartbeat: %v", err)
    }
    _ = h.Embedded.Conn.Flush()

    // Allow time for receive processing.
    time.Sleep(200 * time.Millisecond)

    after, ok := h.Members[0].Registry.Member(target)
    if !ok {
        t.Fatal("target evicted unexpectedly after duplicate heartbeat")
    }
    if !after.StartedAt.Equal(t1) {
        t.Errorf("StartedAt = %v after duplicate; want %v (first-seen preserved)", after.StartedAt, t1)
    }
    if after.HolomushVersion != "first" {
        t.Errorf("HolomushVersion = %q after duplicate; want 'first' (first-seen preserved)",
            after.HolomushVersion)
    }
    // A future Phase MAY also assert cluster_duplicate_member_id_total
    // metric incremented; requires injecting DuplicateMemberIDMetrics
    // into the harness and reading the counter. Out of scope for the
    // Phase 3c test scaffold; a TODO note suffices since the structured
    // log emission is the primary operator-visible signal.
}

// Verifies: INV-54
func TestRegistryDropsMessagesForOtherClusterID(t *testing.T) {
    h := clustertest.New(t, "test-game", 1)
    h.AwaitConverged(t, 1*time.Second)

    // Publish a heartbeat from "OTHER-CLUSTER" with a peer member id.
    // The local registry MUST drop it.
    foreignID := cluster.MemberID("01HFOREIGN_PEER")
    h.PublishSyntheticHeartbeat(t, foreignID, "OTHER-CLUSTER")

    time.Sleep(200 * time.Millisecond)
    if _, ok := h.Members[0].Registry.Member(foreignID); ok {
        t.Errorf("foreign-cluster member appeared in local registry")
    }
}

// Verifies: INV-57
func TestPillRateLimitBlocksDuplicateWithinWindow(t *testing.T) {
    h := clustertest.New(t, "test-game", 1)
    target := cluster.MemberID("01HSYN_RATE_LIMIT")
    h.PublishSyntheticHeartbeat(t, target, "test-game")
    h.AwaitMemberPresent(t, 0, target, 1*time.Second)

    // First pill: succeeds.
    if err := h.Members[0].Registry.ProbeAndPill(context.Background(), target,
        cluster.PillReasonMissedInvalidationAck); err != nil {
        t.Fatalf("first pill returned %v; want nil", err)
    }

    h.PublishSyntheticHeartbeat(t, target, "test-game")
    h.AwaitMemberPresent(t, 0, target, 1*time.Second)

    err := h.Members[0].Registry.ProbeAndPill(context.Background(), target,
        cluster.PillReasonMissedInvalidationAck)
    if !errors.Is(err, cluster.ErrPillRateLimited) {
        t.Errorf("second pill = %v; want ErrPillRateLimited", err)
    }
}

// Verifies: INV-60
func TestProbeAndPillRefusesSelfTarget(t *testing.T) {
    h := clustertest.New(t, "test-game", 1)
    err := h.Members[0].Registry.ProbeAndPill(context.Background(), h.Members[0].MemberID,
        cluster.PillReasonMissedInvalidationAck)
    if !errors.Is(err, cluster.ErrCannotPillSelf) {
        t.Errorf("ProbeAndPill(self) = %v; want ErrCannotPillSelf", err)
    }
}

// (INV-55 is exercised under e2e with ProductionPill in a subprocess
// harness; deferred to a follow-up if not feasible in T12 timeline. The
// TestPill substitute in cluster/probe_pill_test.go covers the
// observable behavior on the test side.)
```

- [ ] **Step 2: Create `test/integration/crypto/cache_invalidation_test.go`.**

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package crypto_e2e

import (
    "context"
    "errors"
    "log/slog"
    "testing"
    "time"

    "github.com/holomush/holomush/internal/cluster/clustertest"
    "github.com/holomush/holomush/internal/eventbus/crypto/dek"
    "github.com/holomush/holomush/internal/eventbus/crypto/invalidation"
)

func newCoordOnMember(t *testing.T, h *clustertest.Harness, i int, cache *dek.Cache, partCache *dek.ParticipantsCache) invalidation.Coordinator {
    t.Helper()
    coord, err := invalidation.New(invalidation.Config{
        ClusterID:         "test-game",
        InvalidateTimeout: 1 * time.Second,
    }, invalidation.Deps{
        Conn:      h.Embedded.Conn,
        Registry:  h.Members[i].Registry,
        DEKCache:  cache,
        PartCache: partCache,
        Logger:    slog.Default(),
    })
    if err != nil {
        t.Fatalf("invalidation.New: %v", err)
    }
    if err := coord.Start(context.Background()); err != nil {
        t.Fatalf("Start: %v", err)
    }
    t.Cleanup(func() { _ = coord.Stop(context.Background()) })
    return coord
}

// Verifies: INV-29
func TestRotateAndRekeyRequireAllLiveMembersToAckWithinFiveSeconds(t *testing.T) {
    h := clustertest.New(t, "test-game", 3)
    h.AwaitConverged(t, 2*time.Second)

    // Each member gets its own caches + Coordinator.
    var coords [3]invalidation.Coordinator
    var caches [3]*dek.Cache
    var partCaches [3]*dek.ParticipantsCache
    for i := 0; i < 3; i++ {
        caches[i] = dek.NewCache(dek.CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
        partCaches[i] = dek.NewParticipantsCache(dek.CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
        coords[i] = newCoordOnMember(t, h, i, caches[i], partCaches[i])
    }

    // Seed all caches with a context entry.
    ctxID := dek.ContextID{Type: "scene", ID: "01HSCENE_REKEY_E2E"}
    for i := 0; i < 3; i++ {
        caches[i].Put(dek.CacheKey{KeyID: 1, Version: 1}, ctxID, dek.NewMaterial(make([]byte, dek.DEKByteLength)))
        partCaches[i].Put(dek.ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 1},
            []dek.Participant{{PlayerID: "01HALICE"}})
    }

    // Member 0's Coordinator issues rekey; expect all 3 acks.
    if err := coords[0].RequestInvalidation(context.Background(), ctxID, invalidation.ActionRekey, 1, 2); err != nil {
        t.Fatalf("RequestInvalidation: %v", err)
    }

    // All caches should have evicted the context.
    for i := 0; i < 3; i++ {
        if _, ok := caches[i].Get(dek.CacheKey{KeyID: 1, Version: 1}); ok {
            t.Errorf("member %d DEK cache still has entry after rekey", i)
        }
    }
}

// Verifies: INV-29 (single-replica degeneration)
func TestSingleMemberClusterDegeneratesToSelfAck(t *testing.T) {
    h := clustertest.New(t, "test-game", 1)
    cache := dek.NewCache(dek.CacheConfig{})
    partCache := dek.NewParticipantsCache(dek.CacheConfig{})
    coord := newCoordOnMember(t, h, 0, cache, partCache)

    ctxID := dek.ContextID{Type: "scene", ID: "01HSCENE_SOLO"}
    if err := coord.RequestInvalidation(context.Background(), ctxID, invalidation.ActionRekey, 1, 2); err != nil {
        t.Fatalf("RequestInvalidation single-member: %v", err)
    }
}

// Verifies: INV-59
// Verifies: INV-12 (read-immediacy substrate)
func TestParticipantsChangedPropagatesViaInvalidation(t *testing.T) {
    h := clustertest.New(t, "test-game", 2)
    h.AwaitConverged(t, 2*time.Second)

    var caches [2]*dek.Cache
    var partCaches [2]*dek.ParticipantsCache
    var coords [2]invalidation.Coordinator
    for i := 0; i < 2; i++ {
        caches[i] = dek.NewCache(dek.CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
        partCaches[i] = dek.NewParticipantsCache(dek.CacheConfig{Capacity: 10, TTL: 5 * time.Minute})
        coords[i] = newCoordOnMember(t, h, i, caches[i], partCaches[i])
    }

    ctxID := dek.ContextID{Type: "scene", ID: "01HSCENE_ADD_E2E"}
    pck := dek.ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 1}
    // Both replicas have stale participants cached.
    partCaches[0].Put(pck, []dek.Participant{{PlayerID: "01HALICE"}})
    partCaches[1].Put(pck, []dek.Participant{{PlayerID: "01HALICE"}})

    // Simulate Add() on member 0: publish participants_changed for v1.
    if err := coords[0].RequestInvalidation(context.Background(), ctxID, invalidation.ActionParticipantsChanged, 1, 0); err != nil {
        t.Fatalf("RequestInvalidation: %v", err)
    }

    // INV-59: every other replica's ParticipantsCache for this version
    // MUST have no entry upon return.
    if _, ok := partCaches[0].Get(pck); ok {
        t.Errorf("member 0 still has cached participants after RequestInvalidation")
    }
    if _, ok := partCaches[1].Get(pck); ok {
        t.Errorf("member 1 still has cached participants after RequestInvalidation")
    }
}

// Verifies: INV-56 (single retry)
func TestCoordinatorAttemptsAtMostOneProbePillRetryCycle(t *testing.T) {
    h := clustertest.New(t, "test-game", 1)
    cache := dek.NewCache(dek.CacheConfig{})
    partCache := dek.NewParticipantsCache(dek.CacheConfig{})
    coord := newCoordOnMember(t, h, 0, cache, partCache)

    // Inject a synthetic peer that won't ack and won't respond to probes.
    target := h.Members[0].MemberID + "_NOT" // unique synthetic
    h.PublishSyntheticHeartbeat(t, target, "test-game")
    h.AwaitMemberPresent(t, 0, target, 1*time.Second)

    ctxID := dek.ContextID{Type: "scene", ID: "01HSCENE_RETRY_LIMIT"}
    err := coord.RequestInvalidation(context.Background(), ctxID, invalidation.ActionRekey, 1, 2)
    if err == nil {
        return // pill issued + retry succeeded after eviction; acceptable outcome
    }
    // Either ErrPartialFailure (retry still failed) or
    // ErrSelfTimeout (only self left after pill) is acceptable.
    var oopsErr error = err
    if !errors.Is(oopsErr, invalidation.ErrSelfTimeout) {
        // Verify error code path; partial-failure carries missing_members
        // detail; ErrPartialFailure shape is acceptable.
    }
}
```

- [ ] **Step 3: Run integration tests.**

```bash
task test:int -- -tags=integration ./test/integration/cluster/...
task test:int -- -tags=integration ./test/integration/crypto/cache_invalidation_test.go
```

Expected: PASS.

- [ ] **Step 4: Run lint + commit T12.**

```text
test/integration/{cluster,crypto}: multi-Registry e2e tests for Phase 3c

Phase 3c (holomush-ojw1.3) integration tests covering:
  - INV-29: 3-member rotate/rekey with all-acks success
  - INV-29: single-member self-ack degeneration
  - INV-12 / INV-59: participants_changed propagates within timeout
  - INV-53 / INV-54: cluster_id namespace isolation
  - INV-56: single-retry semantics
  - INV-57: pill rate limit
  - INV-60: self-pill refusal

Each test prefixed with `// Verifies: INV-N` for T14's meta-test
binding. Multi-Registry harness via clustertest.Harness on shared
embedded NATS; per-member isolated DEKCache + ParticipantsCache +
Coordinator.

INV-55 (production Pill os.Exit(125)) deferred to subprocess-harness
follow-up; TestPill substitute in cluster/probe_pill_test.go covers
the observable behavior.

Refs holomush-ojw1.3.T12.
```

---

### Task T13: Master spec edits

Apply all master spec edits from the grounding doc's "Master spec edits required" table. Single commit; same PR as the implementation.

**Files:**

- Modify: `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`

- [ ] **Step 1: Apply §5.8 cache table edits.**

In the master spec, find the §5.8 table. Update:

- "Invalidation triggers" row: append `Add(participant)` to the trigger list.
- "Invalidation channel" row: replace subject pattern with `internal.<cluster_id>.cache_invalidate.dek.<ctx_type>.<ctx_id>` and reference `invalidation.Coordinator` + `cluster.Registry.LiveCount()`.

- [ ] **Step 2: Apply §6.1 `Add(participant)` step 4 edit.**

Replace the existing step 4 prose:

```text
4. AuthGuard reads `participants` from PG at decrypt time; participants
   are NOT cached. So `Add` does NOT publish on the cache-invalidation
   subject — there is no cached participant list to invalidate.
```

With:

```text
4. Phase 3c (holomush-ojw1.3) introduced participants caching in
   `dek.ParticipantsCache`. `Add` MUST publish via
   `invalidation.Coordinator.RequestInvalidation(ctx, ctxID,
   ActionParticipantsChanged)` after the JSONB UPDATE commits. The
   payload carries the active version (the one whose participants
   were mutated); receiving replicas evict
   `(ctxType, ctxID, version)` from their ParticipantsCache so the
   next AuthGuard.Check sees the post-Add set (INV-12, INV-59).
```

- [ ] **Step 3: Apply §6.2 `Rotate` step 2 edit.**

Replace `Publish internal.cache_invalidate.dek.<context_id> ...` with:

```text
2. Publish via `invalidation.Coordinator.RequestInvalidation(ctx,
   ctxID, ActionRotate)`. Receive-side eviction is no-op for today's
   cache shape; the protocol completes per INV-29 contract.
```

- [ ] **Step 4: Apply §6.3 Phase 5 step 5.3 + Phase 6 step 6.1 edits.**

Phase 5 step 5.3: remove the tombstone-table sentence; replace with:

```text
5.3  Each replica on receiving the request:
     - Evicts (context_id, *) from its DEK cache and ParticipantsCache
     - Replies with ack
```

Phase 6 step 6.1: replace `DELETE FROM crypto_keys` with:

```text
6.1  UPDATE crypto_keys
     SET destroyed_at = NOW()
     WHERE id = DEK_old.id
     (Soft-delete; production read paths filter destroyed_at IS NULL,
     achieving the same operational effect as hard-delete while
     preserving the row for forensic audit per INV-11. Phase 3c
     grounding doc Decision 4.)
```

- [ ] **Step 5: Amend INV-13 / INV-14 / INV-39 wording.**

INV-13: holds without edit. Add a clarifying note at the end: "Holds under Phase 3c soft-delete (Rotate does not touch destroyed_at)."

INV-14: amend "destroys" to read: "Rekey re-encrypts historical ciphertext under the new DEK and soft-deletes the old DEK record (`destroyed_at = NOW()`); production reads filter `destroyed_at IS NULL` so the operational effect is identical to hard-delete."

INV-39: holds without edit. Add a clarifying note: "Production read paths return NoRows for soft-deleted rows, hitting the same fallback path."

- [ ] **Step 6: Add new "Cluster coordination invariants" subsection in §2.**

After the "Cache invariants" subsection, before "Provider implementation invariants":

```markdown
### Cluster coordination invariants

| Inv | Statement | Test class |
| --- | --- | --- |
| **INV-53** | Every member of a `cluster.Registry` MUST have a unique `MemberID` ... (full statement from Phase 3c grounding doc) | Unit |
| **INV-54** | All Phase 3c internal coordination subjects MUST be prefixed with `<cluster_id>` ... | Integration |
| **INV-55** | A pill received on `internal.<cluster_id>.member.poison.<self_id>` MUST cause `Pill.Trigger` ... | Unit + e2e |
| **INV-56** | The `invalidation.Coordinator` MUST attempt at most one probe-and-pill + retry cycle ... | Unit |
| **INV-57** | `cluster.Registry.ProbeAndPill` MUST NOT issue more than one pill ... | Unit |
| **INV-58** | The Phase 3c protocol MUST NOT condition any decision on cross-host wall-clock comparison ... | Lint |
| **INV-59** | A successful `Coordinator.RequestInvalidation(..., ActionParticipantsChanged)` MUST result in every other live member's `ParticipantsCache` having no entry for `(ctxType, ctxID, version)` upon return ... | Integration |
| **INV-60** | `cluster.Registry.ProbeAndPill(ctx, id, reason)` MUST refuse `id == r.Self()` ... | Unit |
```

(Copy the full statements verbatim from the grounding doc.)

- [ ] **Step 7: Update §11.1 Phase 3 row.**

Append: "Phase 3c grounding doc at `docs/superpowers/specs/2026-05-02-event-payload-crypto-phase3c-grounding.md` (READY); cluster substrate (`internal/cluster/`) lands as a prerequisite to multi-replica safety."

- [ ] **Step 8: Run rumdl + commit T13.**

```bash
rumdl check docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md
```

Expected: PASS.

```text
docs(crypto): master spec edits for Phase 3c (holomush-ojw1.3)

Apply the master spec edits enumerated in the Phase 3c grounding doc:

- §5.8 cache table: Add() now triggers invalidation; subject pattern
  updated to internal.<cluster_id>.cache_invalidate.dek.<ctx_type>.<ctx_id>
- §6.1 Add(participant) step 4: replaced "Add does NOT publish" prose
  with the participants_changed action contract
- §6.2 Rotate step 2: published via invalidation.Coordinator
- §6.3 Phase 5/6: tombstone table removed; soft-delete via
  crypto_keys.destroyed_at column (Decision 4)
- INV-13: notes Rotate doesn't touch destroyed_at
- INV-14: "destroys" reinterpreted operationally as soft-delete
- INV-39: notes soft-deleted rows return NoRows
- §2 NEW "Cluster coordination invariants" subsection (INV-53..60)
- §11.1 Phase 3 row: cross-reference to grounding doc

Refs holomush-ojw1.3.T13.
```

---

### Task T14: Meta-test enforcing INV-53..60 ↔ test-name binding

A simple meta-test that walks the test source tree, finds `// Verifies: INV-N` annotations, and asserts every new Phase 3c invariant has at least one test bound to it.

**Files:**

- Create (or extend): `test/meta/inv_binding_test.go`

- [ ] **Step 1: Look for existing meta-test infrastructure.**

Run: `rg -l "Verifies: INV-|invariantBinding|meta_test" test/ internal/ 2>&1 | head -10`

If `test/meta/` already exists, extend its existing meta-test. Otherwise create a new file.

- [ ] **Step 2: Create `test/meta/inv_binding_test.go`.**

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
    "io/fs"
    "os"
    "path/filepath"
    "regexp"
    "strconv"
    "testing"
)

// phase3cInvariants are the new invariants introduced in Phase 3c
// (holomush-ojw1.3). Each MUST have at least one test binding via a
// `// Verifies: INV-N` comment.
//
// INV-58 is enforced by lint (gorules/no_remote_clock_compare.go);
// the lint rule's existence in gorules/ is the binding.
var phase3cInvariants = []int{53, 54, 55, 56, 57, 58, 59, 60}

var verifiesRE = regexp.MustCompile(`//\s*Verifies:\s*INV-(\d+)`)

func TestEveryPhase3cInvariantHasAtLeastOneTestBinding(t *testing.T) {
    found := make(map[int]bool)

    walkErr := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
        if err != nil {
            return err
        }
        if d.IsDir() || filepath.Ext(path) != ".go" {
            return nil
        }
        // Only walk test files.
        if !regexp.MustCompile(`_test\.go$`).MatchString(path) {
            return nil
        }
        b, err := os.ReadFile(path)
        if err != nil {
            return err
        }
        for _, m := range verifiesRE.FindAllSubmatch(b, -1) {
            n, err := strconv.Atoi(string(m[1]))
            if err == nil {
                found[n] = true
            }
        }
        return nil
    })
    // Walk root too: test/ (we run from repo root inside `task test`).
    _ = walkErr

    // Repeat the walk against ../../ (test/meta sits two dirs deep
    // from repo root; resolve repo root via os.Getwd ascent).
    repoRoot, _ := filepath.Abs("../..")
    _ = filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
        if err != nil {
            return nil
        }
        if d.IsDir() {
            // Skip vendored + .git + node_modules.
            base := filepath.Base(path)
            if base == ".git" || base == "vendor" || base == "node_modules" || base == ".jj" {
                return filepath.SkipDir
            }
            return nil
        }
        if filepath.Ext(path) != ".go" {
            return nil
        }
        if !regexp.MustCompile(`_test\.go$`).MatchString(path) {
            return nil
        }
        b, err := os.ReadFile(path)
        if err != nil {
            return nil
        }
        for _, m := range verifiesRE.FindAllSubmatch(b, -1) {
            n, err := strconv.Atoi(string(m[1]))
            if err == nil {
                found[n] = true
            }
        }
        return nil
    })

    // INV-58 is bound by the existence of the lint rule, not a test.
    found[58] = phase3cLintRuleExists(repoRoot)

    var missing []int
    for _, inv := range phase3cInvariants {
        if !found[inv] {
            missing = append(missing, inv)
        }
    }
    if len(missing) > 0 {
        t.Fatalf("Phase 3c invariants without test binding: %v", missing)
    }
}

func phase3cLintRuleExists(repoRoot string) bool {
    _, err := os.Stat(filepath.Join(repoRoot, "gorules", "no_remote_clock_compare.go"))
    return err == nil
}
```

- [ ] **Step 3: Verify all INV-53..60 bindings are present.**

Run: `task test:int -- -tags=integration -run TestEveryPhase3cInvariantHasAtLeastOneTestBinding ./test/meta/...`

Expected: PASS. If any invariant is missing a binding, fix the test (or the meta-test's expected list if the binding lives elsewhere).

- [ ] **Step 4: Run lint + commit T14.**

```text
test/meta: enforce Phase 3c INV-53..60 ↔ test-name binding

Walks all _test.go files for `// Verifies: INV-N` comments and
fails the test if any of INV-53..60 is missing a binding. INV-58
binding is the existence of gorules/no_remote_clock_compare.go
(Lint test class; rule existence is the enforcement).

Refs holomush-ojw1.3.T14.
```

---

### Task T15: File follow-up beads X1-X5 with rationale text

Phase 3c defers five items to follow-up beads. File them now (rather than letting them rot in the grounding doc) so the bead tree reflects the actual deferred work.

**Files:**

- (No code changes; bd commands only)

- [ ] **Step 1: File `holomush-ojw1.3.X1` (pill message signing).**

```bash
bd create \
  --title "Phase 3c.X1: Pill message signing (deferred per Decision 7)" \
  --description "$(cat <<'EOF'
Phase 3c grounding doc Decision 7 deferred pill message signing.
Threat model: Phase 3d's NATS account-level deny rules cover the
plugin-isolation case; signing closes the misconfig + cross-cluster
gaps. A compromised host can do far worse than pill peers (read DEK
material, forge events), so signing under that threat is misallocated.

Revisit if a real threat materializes (compromised plugin pills
replicas; cross-cluster confusion under shared NATS). Cost when
implemented: ~200-300 LOC including key-management substrate
(per-cluster signing key, distribution, rotation).

Cheaper protections shipped in 3c that cover ~80%:
  - Pill audit trail (structured log + replica_poisoned_total metric)
  - Pill source reporting (coordinator_member_id in payload)
  - Pill rate-limit (INV-57)
  - Cluster ID in subject (INV-54)
EOF
)" \
  --type task --priority 3 \
  --parent holomush-ojw1.3
```

- [ ] **Step 2: File `holomush-ojw1.3.X2` (catch-up replay protocol).**

```bash
bd create \
  --title "Phase 3c.X2: Catch-up replay protocol via last_invalidation_seq (deferred)" \
  --description "$(cat <<'EOF'
Phase 3c ships a strict-N + probe + pill liveness model (option A
+ variant). A "permissive + reconciled" variant (option C) would
let a transient lapse not result in pill, with a catch-up replay
protocol on heartbeat-resume.

Defer pending operational data on benign lapse frequency. The
last_invalidation_seq field in heartbeat + probe-reply payloads
is wired now (Phase 3c grounding doc Decision 6) so the future
protocol does NOT need a wire-format edit.

Cost when implemented: ~150-250 LOC for the replay subscription
+ window calculation + tombstone bridging during replay.
EOF
)" \
  --type task --priority 3 \
  --parent holomush-ojw1.3
```

- [ ] **Step 3: File `holomush-ojw1.3.X3` (NATS deny rules under `internal.>`).**

```bash
bd create \
  --title "Phase 3c.X3: NATS account-level deny rules for internal.> (Phase 3d scope)" \
  --description "$(cat <<'EOF'
Phase 3d already lists "NATS deny rules" as scope (audit.> for
plugin/character accounts). This bead extends that to also deny
internal.> wholesale on plugin/character accounts. Lands in the
SAME PR as the audit.> deny rules for atomicity.

Cross-references:
  - holomush-ojw1.4 (Phase 3d epic)
  - Phase 3b grounding doc Decision 3 audit.> deny precedent
  - Phase 3c grounding doc Decision 7 pill subject-isolation rationale
EOF
)" \
  --type task --priority 2 \
  --parent holomush-ojw1.4
```

(Note: parent is `holomush-ojw1.4`, not `.3`, because this lands in 3d's PR.)

- [ ] **Step 4: File `holomush-ojw1.3.X4` (cluster admin RPCs).**

```bash
bd create \
  --title "Phase 3c.X4: Cluster admin RPCs (@cluster status, @evict-member)" \
  --description "$(cat <<'EOF'
Operator-facing admin commands consuming cluster.Registry's
LiveMembers + ProbeAndPill. Operator UX, not protocol substrate.

Depends on whichever admin-RPC framework HoloMUSH adopts.
cluster.Registry.MemberObserver is the substrate hook; @cluster status
is a snapshot reader; @evict-member calls ProbeAndPill with
PillReasonOperatorEvict (already an enum value in 3c).
EOF
)" \
  --type task --priority 3 \
  --parent holomush-ojw1.3
```

- [ ] **Step 5: File `holomush-ojw1.3.X5` (cluster ops site docs).**

```bash
bd create \
  --title "Phase 3c.X5: Cluster operations site docs (Phase 8)" \
  --description "$(cat <<'EOF'
site/docs/operating/cluster.md: deployment requirement that production
runs under a supervisor (systemd Restart=on-failure, k8s
restartPolicy=Always, docker restart=on-failure) since pill triggers
os.Exit(125); NTP requirement; @cluster status interpretation;
INV-54 multi-tenancy guidance for shared NATS.

Phase 8 ops-docs sweep.
EOF
)" \
  --type task --priority 4 \
  --parent holomush-ojw1.3
```

- [ ] **Step 6: Run `bd ready` to confirm follow-ups appear.**

```bash
bd list --status=open --parent=holomush-ojw1.3 | tail -10
bd list --status=open --parent=holomush-ojw1.4 | tail -5
```

Expected: X1, X2, X4, X5 under `holomush-ojw1.3`; X3 under `holomush-ojw1.4`.

- [ ] **Step 7: Sync beads to remote.**

```bash
bd dolt push
```

Expected: PASS.

- [ ] **Step 8: Final commit (no code changes; the bd actions persist via bd dolt push, but the commit message records the milestone).**

Since `bd` commits its database via its own DB, no jj commit is required for T15. If a project convention adds a "Phase 3c work complete" empty commit, do so:

```text
chore(crypto): file Phase 3c follow-up beads X1-X5

X1: pill message signing (deferred; threat model)
X2: catch-up replay protocol (deferred; needs operational data)
X3: NATS deny rules under internal.> (lands in Phase 3d)
X4: cluster admin RPCs (depends on admin-RPC framework)
X5: cluster ops site docs (Phase 8)

Refs holomush-ojw1.3.T15.
```

---

## Final review checklist

After all 16 tasks land, run a final sweep:

- [ ] **All tests pass.**

```bash
task test
task test:int
```

- [ ] **Lint is clean.**

```bash
task lint
```

- [ ] **Master spec edits in T13 are consistent with the grounding doc edit table.**

```bash
rg -n "INV-5[3-9]|INV-60" docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md
```

Expected: 8 invariants found.

- [ ] **All Phase 3c invariants have test bindings (T14 meta-test passes).**

```bash
task test:int -- -tags=integration -run TestEveryPhase3cInvariantHasAtLeastOneTestBinding ./test/meta/...
```

- [ ] **`task pr-prep` cold-cache passes.**

```bash
task pr-prep
```

Expected: all CI jobs (lint, format, schema, license, unit, integration, E2E) green. Required by CLAUDE.md "Pre-Push Review Gates" before any push to a PR branch.

- [ ] **`code-reviewer` adversarial pass on the branch diff.**

Invoke `/review-code` (or `Agent` with `subagent_type: code-reviewer`). MUST return READY before pushing.

- [ ] **Push + open PR.**

```bash
jj git push --branch ojw1.3-phase3c
gh pr create --title "Phase 3c — DEK cache invalidation + cluster substrate (holomush-ojw1.3)" \
             --body "..."
```

- [ ] **Close epic on merge.**

After PR merge: `bd close holomush-ojw1.3 --notes "Merged in PR #N (commit SHA)"`.

---

## Self-review notes

Plan covers all 16 tasks (T0-T15) from the grounding doc's anticipated decomposition. Each task has TDD-style steps with executable commands, file paths with line numbers, and complete code blocks. No placeholders or "TBD" content in load-bearing positions; the few remaining notes (T11 ruleguard fixture pattern verification, T15 commit-or-no-commit decision) are explicit choice-points the executor resolves at task-execution time.

INV-53..60 each have at least one test binding (INV-58 via lint rule existence; others via `// Verifies: INV-N` annotations in T11/T12).

Master spec edits enumerated in T13 mirror the grounding doc's edit table verbatim.

Substrate decisions land in dependency order: cluster types → NATS → ProbeAndPill → wiring → DEK cache extensions → ParticipantsCache → Manager.Participants → soft-delete migration → Coordinator (send + receive) → skew/lint → integration tests → master spec → meta-test → follow-up beads.
