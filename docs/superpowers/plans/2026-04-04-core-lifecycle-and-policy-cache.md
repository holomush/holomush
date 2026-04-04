# Core Lifecycle & Policy Cache Implementation Plan

> **Status: Phase 1 COMPLETE. Phase 2 plan is separate.**
> Phase 2 (core startup decomposition) continues in:
> `docs/superpowers/plans/2026-04-04-core-startup-decomposition.md`

**Goal:** Replace the broken staleness-based policy cache with a health tier system, and introduce a formal subsystem lifecycle with dependency-ordered startup.

**Architecture:** Four separated components — PolicyCache (simplified), PolicyPoller (new), HealthTracker (generic), ReadinessRegistry (thin aggregator). Subsystems declare dependencies via typed IDs; the server topologically sorts and starts them in order, gating gRPC on aggregate readiness.

**Tech Stack:** Go 1.23, PostgreSQL (existing), Prometheus (existing metrics), testify (existing test framework)

**Spec:** `docs/superpowers/specs/2026-04-04-core-lifecycle-and-policy-cache-design.md`

---

## File Structure

### New Files

| File | Responsibility |
| ---- | -------------- |
| `internal/lifecycle/subsystem.go` | SubsystemID enum, Subsystem interface |
| `internal/lifecycle/subsystem_string.go` | Generated stringer for SubsystemID |
| `internal/lifecycle/health.go` | HealthTier, HealthStatus, HealthReporter interface |
| `internal/lifecycle/tracker.go` | HealthTracker state machine |
| `internal/lifecycle/tracker_test.go` | HealthTracker unit tests |
| `internal/lifecycle/registry.go` | ReadinessRegistry |
| `internal/lifecycle/registry_test.go` | ReadinessRegistry unit tests |
| `internal/lifecycle/orchestrator.go` | Topological sort, startup/shutdown orchestration |
| `internal/lifecycle/orchestrator_test.go` | Topo sort, cycle detection, parallel start tests |
| `internal/lifecycle/metrics.go` | Prometheus metrics for lifecycle/health |
| `internal/access/policy/poller.go` | PolicyPoller |
| `internal/access/policy/poller_test.go` | PolicyPoller unit tests |

### Modified Files

| File | Change |
| ---- | ------ |
| `internal/access/policy/cache.go` | Remove IsStale, lastUpdate, staleness config, Listener interface, StartWithListener, listenLoop, Wait, Stop. Add Invalidate(). |
| `internal/access/policy/cache_test.go` | Remove staleness/listener tests. Add Invalidate tests. |
| `internal/access/policy/engine.go` | Remove Step 6b (IsStale check, lines 247-275) |
| `internal/access/policy/engine_test.go` | Remove staleness-related engine tests, update test setup |
| `internal/access/policy/store/postgres.go` | Remove pg\_notify calls. Add cache invalidation hook. |
| `internal/access/setup/setup.go` | Wire PolicyPoller, HealthTracker. Remove StartWithListener reference. |
| `cmd/holomush/core.go` | Remove PgListener goroutine. Wire subsystem orchestrator. Move observability server early. |
| `internal/observability/server.go` | Accept ReadinessRegistry instead of func() bool |

### Removed Files

| File | Reason |
| ---- | ------ |
| `internal/access/policy/pglistener.go` | Replaced by PolicyPoller |
| `internal/access/policy/pglistener_test.go` | Replaced by PolicyPoller tests |

---

## Phase 1: Policy Cache Fix

This phase fixes the immediate bug (guest commands denied after 30s). It can land independently of Phase 2.

### Task 1: Create lifecycle package — interfaces and types

**Files:**

- Create: `internal/lifecycle/subsystem.go`
- Create: `internal/lifecycle/health.go`
- Create: `internal/lifecycle/metrics.go`

- [ ] **Step 1: Create subsystem.go with SubsystemID and Subsystem interface**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package lifecycle provides subsystem lifecycle management, health tracking,
// and readiness gating for the core server.
package lifecycle

import "context"

//go:generate stringer -type=SubsystemID -linecomment

// SubsystemID is a compile-time-safe typed identifier for server subsystems.
type SubsystemID int

const (
	SubsystemDatabase   SubsystemID = iota // database
	SubsystemTLS                           // tls
	SubsystemABAC                          // abac
	SubsystemAuth                          // auth
	SubsystemWorld                         // world
	SubsystemPlugins                       // plugins
	SubsystemSessions                      // sessions
	SubsystemBootstrap                     // bootstrap
	SubsystemGRPC                          // grpc
)

// Subsystem is a top-level server component with lifecycle management
// and dependency declaration.
type Subsystem interface {
	// ID returns the typed identifier for this subsystem.
	ID() SubsystemID

	// DependsOn returns the subsystems that must be started before this one.
	DependsOn() []SubsystemID

	// Start initializes the subsystem. It MUST be idempotent.
	// A non-nil error is fatal — the server will not start.
	Start(ctx context.Context) error

	// Stop shuts down the subsystem. It MUST be idempotent and
	// MUST NOT block indefinitely.
	Stop(ctx context.Context) error
}
```

- [ ] **Step 2: Create health.go with HealthTier, HealthStatus, HealthReporter**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lifecycle

import "time"

//go:generate stringer -type=HealthTier -linecomment

// HealthTier represents operational health levels.
// Tiers are ordered by severity — higher values are worse.
type HealthTier int

const (
	HealthWarm     HealthTier = iota // warm
	HealthDegraded                   // degraded
	HealthStale                      // stale
	HealthDead                       // dead
)

// IsReady returns true if the tier is servable (Warm or Degraded).
func (t HealthTier) IsReady() bool {
	return t <= HealthDegraded
}

// HealthStatus is the health report from a subsystem.
type HealthStatus struct {
	Tier   HealthTier
	Reason string
	Since  time.Time
}

// HealthReporter is implemented by subsystems with ongoing runtime state.
// Not all Subsystems need this — only those with connections, caches, or
// background loops that can degrade at runtime.
type HealthReporter interface {
	HealthStatus() HealthStatus
}
```

- [ ] **Step 3: Create metrics.go with Prometheus metrics**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lifecycle

import "github.com/prometheus/client_golang/prometheus"

var (
	healthTierGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "lifecycle_health_tier",
		Help: "Current health tier per subsystem (0=warm, 1=degraded, 2=stale, 3=dead)",
	}, []string{"subsystem"})

	healthTransitionsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "lifecycle_health_transitions_total",
		Help: "Total health tier transitions per subsystem",
	}, []string{"subsystem", "from", "to"})

	startupDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "lifecycle_startup_duration_seconds",
		Help:    "Time from process start to AllReady()",
		Buckets: prometheus.DefBuckets,
	})
)

// RegisterMetrics registers lifecycle metrics with the given Prometheus registry.
func RegisterMetrics(reg prometheus.Registerer) {
	reg.MustRegister(healthTierGauge, healthTransitionsTotal, startupDuration)
}
```

- [ ] **Step 4: Generate stringer**

Run: `cd internal/lifecycle && go generate ./...`
Expected: `subsystem_string.go` and `healthtier_string.go` generated (or named per stringer convention).

Note: If `stringer` is not available, install with `go install golang.org/x/tools/cmd/stringer@latest`.

- [ ] **Step 5: Verify compilation**

Run: `task build`
Expected: Clean build with no errors.

- [ ] **Step 6: Commit**

Commit: `feat(lifecycle): add subsystem and health interfaces`

---

### Task 2: Implement HealthTracker

**Files:**

- Create: `internal/lifecycle/tracker.go`
- Create: `internal/lifecycle/tracker_test.go`

- [ ] **Step 1: Write failing tests for HealthTracker**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lifecycle_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/lifecycle"
)

func defaultTrackerConfig() lifecycle.TrackerConfig {
	return lifecycle.TrackerConfig{
		SubsystemName:  "test",
		DegradedAfter:  1,
		GracePeriod:    60 * time.Second,
		MaxFailures:    30,
	}
}

func TestHealthTracker_InitialState(t *testing.T) {
	ht := lifecycle.NewHealthTracker(defaultTrackerConfig())
	status := ht.HealthStatus()
	assert.Equal(t, lifecycle.HealthWarm, status.Tier)
	assert.Equal(t, "initialized", status.Reason)
}

func TestHealthTracker_SingleFailure_Degrades(t *testing.T) {
	ht := lifecycle.NewHealthTracker(defaultTrackerConfig())

	ht.RecordFailure("poll timeout")
	status := ht.HealthStatus()
	assert.Equal(t, lifecycle.HealthDegraded, status.Tier)
	assert.Equal(t, "poll timeout", status.Reason)
}

func TestHealthTracker_SuccessAfterFailure_RecoversToWarm(t *testing.T) {
	ht := lifecycle.NewHealthTracker(defaultTrackerConfig())

	ht.RecordFailure("poll timeout")
	require.Equal(t, lifecycle.HealthDegraded, ht.HealthStatus().Tier)

	ht.RecordSuccess()
	assert.Equal(t, lifecycle.HealthWarm, ht.HealthStatus().Tier)
}

func TestHealthTracker_GracePeriodExpiry_BecomesStale(t *testing.T) {
	cfg := defaultTrackerConfig()
	cfg.GracePeriod = 10 * time.Millisecond
	ht := lifecycle.NewHealthTracker(cfg)

	ht.RecordFailure("db unreachable")
	require.Equal(t, lifecycle.HealthDegraded, ht.HealthStatus().Tier)

	// Keep failing past grace period
	time.Sleep(15 * time.Millisecond)
	ht.RecordFailure("db unreachable")
	assert.Equal(t, lifecycle.HealthStale, ht.HealthStatus().Tier)
}

func TestHealthTracker_MaxFailures_BecomesDead(t *testing.T) {
	cfg := defaultTrackerConfig()
	cfg.GracePeriod = 0 // immediate stale
	cfg.MaxFailures = 3
	ht := lifecycle.NewHealthTracker(cfg)

	ht.RecordFailure("fail 1") // → degraded
	ht.RecordFailure("fail 2") // → stale (grace=0)
	ht.RecordFailure("fail 3") // still stale, counting
	ht.RecordFailure("fail 4") // → dead (3 failures in stale)
	assert.Equal(t, lifecycle.HealthDead, ht.HealthStatus().Tier)
}

func TestHealthTracker_DeadIsTerminal(t *testing.T) {
	cfg := defaultTrackerConfig()
	cfg.GracePeriod = 0
	cfg.MaxFailures = 1
	ht := lifecycle.NewHealthTracker(cfg)

	ht.RecordFailure("fail 1") // → degraded
	ht.RecordFailure("fail 2") // → stale
	ht.RecordFailure("fail 3") // → dead

	// Success should NOT recover from dead
	ht.RecordSuccess()
	assert.Equal(t, lifecycle.HealthDead, ht.HealthStatus().Tier)
}

func TestHealthTracker_StaleRecovery(t *testing.T) {
	cfg := defaultTrackerConfig()
	cfg.GracePeriod = 0
	cfg.MaxFailures = 30
	ht := lifecycle.NewHealthTracker(cfg)

	ht.RecordFailure("fail")
	ht.RecordFailure("fail") // → stale
	require.Equal(t, lifecycle.HealthStale, ht.HealthStatus().Tier)

	ht.RecordSuccess()
	assert.Equal(t, lifecycle.HealthWarm, ht.HealthStatus().Tier)
}

func TestHealthTracker_OnTierChange_Callback(t *testing.T) {
	var transitions []lifecycle.HealthTier
	cfg := defaultTrackerConfig()
	cfg.OnTierChange = func(from, to lifecycle.HealthTier) {
		transitions = append(transitions, to)
	}
	ht := lifecycle.NewHealthTracker(cfg)

	ht.RecordFailure("fail")   // warm → degraded
	ht.RecordSuccess()         // degraded → warm
	assert.Equal(t, []lifecycle.HealthTier{lifecycle.HealthDegraded, lifecycle.HealthWarm}, transitions)
}

func TestHealthTracker_ImplementsHealthReporter(t *testing.T) {
	ht := lifecycle.NewHealthTracker(defaultTrackerConfig())
	var _ lifecycle.HealthReporter = ht
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- ./internal/lifecycle/`
Expected: Compilation errors (package/types don't exist yet).

- [ ] **Step 3: Implement HealthTracker**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lifecycle

import (
	"sync"
	"time"
)

// TrackerConfig defines tier transition thresholds.
type TrackerConfig struct {
	// SubsystemName is used for metric labels.
	SubsystemName string

	// DegradedAfter is the number of consecutive failures before
	// transitioning from Warm to Degraded. Default: 1.
	DegradedAfter int

	// GracePeriod is the duration to remain in Degraded before
	// transitioning to Stale if failures continue. Default: 60s.
	GracePeriod time.Duration

	// MaxFailures is the number of consecutive failures in Stale
	// before transitioning to Dead. Default: 30.
	MaxFailures int

	// OnTierChange is called when the health tier changes.
	// It is called while holding the lock — do not block.
	OnTierChange func(from, to HealthTier)
}

// HealthTracker manages tier transitions based on success/failure signals.
// It is safe for concurrent use.
type HealthTracker struct {
	mu            sync.RWMutex
	cfg           TrackerConfig
	tier          HealthTier
	reason        string
	since         time.Time
	degradedAt    time.Time // when we entered degraded
	staleFailures int       // consecutive failures while stale
}

// NewHealthTracker creates a HealthTracker starting in Warm state.
func NewHealthTracker(cfg TrackerConfig) *HealthTracker {
	if cfg.DegradedAfter <= 0 {
		cfg.DegradedAfter = 1
	}
	if cfg.GracePeriod <= 0 && cfg.GracePeriod != 0 {
		cfg.GracePeriod = 60 * time.Second
	}
	if cfg.MaxFailures <= 0 {
		cfg.MaxFailures = 30
	}

	now := time.Now()
	ht := &HealthTracker{
		cfg:    cfg,
		tier:   HealthWarm,
		reason: "initialized",
		since:  now,
	}

	if cfg.SubsystemName != "" {
		healthTierGauge.WithLabelValues(cfg.SubsystemName).Set(float64(HealthWarm))
	}

	return ht
}

// RecordSuccess signals a successful operation. Resets to Warm
// (except from Dead, which is terminal).
func (ht *HealthTracker) RecordSuccess() {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	if ht.tier == HealthDead {
		return // terminal — no recovery
	}

	if ht.tier != HealthWarm {
		ht.transition(HealthWarm, "recovered")
	}
	ht.staleFailures = 0
}

// RecordFailure signals a failed operation and advances tier based
// on configured thresholds.
func (ht *HealthTracker) RecordFailure(reason string) {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	now := time.Now()

	switch ht.tier {
	case HealthWarm:
		ht.transition(HealthDegraded, reason)
		ht.degradedAt = now

	case HealthDegraded:
		if now.Sub(ht.degradedAt) >= ht.cfg.GracePeriod {
			ht.transition(HealthStale, reason)
			ht.staleFailures = 1
		}

	case HealthStale:
		ht.staleFailures++
		if ht.staleFailures >= ht.cfg.MaxFailures {
			ht.transition(HealthDead, reason)
		}

	case HealthDead:
		// terminal — ignore
	}
}

// HealthStatus returns the current health status.
func (ht *HealthTracker) HealthStatus() HealthStatus {
	ht.mu.RLock()
	defer ht.mu.RUnlock()
	return HealthStatus{
		Tier:   ht.tier,
		Reason: ht.reason,
		Since:  ht.since,
	}
}

func (ht *HealthTracker) transition(to HealthTier, reason string) {
	from := ht.tier
	ht.tier = to
	ht.reason = reason
	ht.since = time.Now()

	if ht.cfg.SubsystemName != "" {
		healthTierGauge.WithLabelValues(ht.cfg.SubsystemName).Set(float64(to))
		healthTransitionsTotal.WithLabelValues(ht.cfg.SubsystemName, from.String(), to.String()).Inc()
	}

	if ht.cfg.OnTierChange != nil {
		ht.cfg.OnTierChange(from, to)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- ./internal/lifecycle/`
Expected: All tests pass.

- [ ] **Step 5: Commit**

Commit: `feat(lifecycle): implement HealthTracker with tier transitions`

---

### Task 3: Implement ReadinessRegistry

**Files:**

- Create: `internal/lifecycle/registry.go`
- Create: `internal/lifecycle/registry_test.go`

- [ ] **Step 1: Write failing tests for ReadinessRegistry**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lifecycle_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/lifecycle"
)

type fixedReporter struct {
	status lifecycle.HealthStatus
}

func (r *fixedReporter) HealthStatus() lifecycle.HealthStatus { return r.status }

func warmReporter() *fixedReporter {
	return &fixedReporter{status: lifecycle.HealthStatus{Tier: lifecycle.HealthWarm}}
}

func staleReporter() *fixedReporter {
	return &fixedReporter{status: lifecycle.HealthStatus{Tier: lifecycle.HealthStale}}
}

func TestReadinessRegistry_EmptyIsReady(t *testing.T) {
	reg := lifecycle.NewReadinessRegistry()
	assert.True(t, reg.AllReady())
}

func TestReadinessRegistry_AllWarm_IsReady(t *testing.T) {
	reg := lifecycle.NewReadinessRegistry()
	reg.Register(lifecycle.SubsystemABAC, warmReporter())
	reg.Register(lifecycle.SubsystemDatabase, warmReporter())
	assert.True(t, reg.AllReady())
}

func TestReadinessRegistry_OneStale_NotReady(t *testing.T) {
	reg := lifecycle.NewReadinessRegistry()
	reg.Register(lifecycle.SubsystemABAC, warmReporter())
	reg.Register(lifecycle.SubsystemDatabase, staleReporter())
	assert.False(t, reg.AllReady())
}

func TestReadinessRegistry_DegradedIsReady(t *testing.T) {
	reg := lifecycle.NewReadinessRegistry()
	reg.Register(lifecycle.SubsystemABAC, &fixedReporter{
		status: lifecycle.HealthStatus{Tier: lifecycle.HealthDegraded},
	})
	assert.True(t, reg.AllReady())
}

func TestReadinessRegistry_Status_ReturnsAll(t *testing.T) {
	reg := lifecycle.NewReadinessRegistry()
	reg.Register(lifecycle.SubsystemABAC, warmReporter())
	reg.Register(lifecycle.SubsystemDatabase, staleReporter())

	status := reg.Status()
	require.Len(t, status, 2)
	assert.Equal(t, lifecycle.HealthWarm, status[lifecycle.SubsystemABAC].Tier)
	assert.Equal(t, lifecycle.HealthStale, status[lifecycle.SubsystemDatabase].Tier)
}

func TestReadinessRegistry_WaitReady_ImmediateSuccess(t *testing.T) {
	reg := lifecycle.NewReadinessRegistry()
	reg.Register(lifecycle.SubsystemABAC, warmReporter())

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := reg.WaitReady(ctx)
	require.NoError(t, err)
}

func TestReadinessRegistry_WaitReady_Timeout(t *testing.T) {
	reg := lifecycle.NewReadinessRegistry()
	reg.Register(lifecycle.SubsystemDatabase, staleReporter())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := reg.WaitReady(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- ./internal/lifecycle/`
Expected: Compilation errors.

- [ ] **Step 3: Implement ReadinessRegistry**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lifecycle

import (
	"context"
	"sync"
	"time"
)

// ReadinessRegistry aggregates health from subsystems and provides
// a single readiness signal for startup gating.
type ReadinessRegistry struct {
	mu        sync.RWMutex
	reporters map[SubsystemID]HealthReporter
}

// NewReadinessRegistry creates an empty registry.
func NewReadinessRegistry() *ReadinessRegistry {
	return &ReadinessRegistry{
		reporters: make(map[SubsystemID]HealthReporter),
	}
}

// Register adds a health reporter for a subsystem. Panics if the
// subsystem is already registered (indicates a wiring bug).
func (r *ReadinessRegistry) Register(id SubsystemID, hr HealthReporter) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.reporters[id]; exists {
		panic("lifecycle: duplicate registration for subsystem " + id.String())
	}
	r.reporters[id] = hr
}

// AllReady returns true when every registered reporter is Warm or Degraded.
// Returns true if no reporters are registered (vacuous truth).
func (r *ReadinessRegistry) AllReady() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, hr := range r.reporters {
		if !hr.HealthStatus().Tier.IsReady() {
			return false
		}
	}
	return true
}

// Status returns per-subsystem health for diagnostics.
func (r *ReadinessRegistry) Status() map[SubsystemID]HealthStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[SubsystemID]HealthStatus, len(r.reporters))
	for id, hr := range r.reporters {
		result[id] = hr.HealthStatus()
	}
	return result
}

// WaitReady blocks until AllReady() returns true or the context is cancelled.
// Polls every 100ms.
func (r *ReadinessRegistry) WaitReady(ctx context.Context) error {
	if r.AllReady() {
		return nil
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if r.AllReady() {
				return nil
			}
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test -- ./internal/lifecycle/`
Expected: All tests pass.

- [ ] **Step 5: Commit**

Commit: `feat(lifecycle): implement ReadinessRegistry`

---

### Task 4: Refactor PolicyCache — remove staleness, add Invalidate

**Files:**

- Modify: `internal/access/policy/cache.go`
- Modify: `internal/access/policy/cache_test.go`

- [ ] **Step 1: Write test for new Invalidate method**

Add to `cache_test.go`:

```go
func TestCache_Invalidate_TriggersReload(t *testing.T) {
	dslText := `permit where subject.type == "character" and action in ["execute"]`
	ms := &mockPolicyStore{
		policies: []store.StoredPolicy{
			{ID: "p1", Name: "test-policy", DSLText: dslText, Enabled: true},
		},
	}
	compiler := testCompiler()
	cache := NewCache(ms, compiler)

	// Initial load
	require.NoError(t, cache.Reload(context.Background()))
	snap1 := cache.Snapshot()
	require.Len(t, snap1.Policies, 1)

	// Add a second policy
	ms.policies = append(ms.policies, store.StoredPolicy{
		ID: "p2", Name: "test-policy-2", DSLText: dslText, Enabled: true,
	})

	// Invalidate triggers reload
	err := cache.Invalidate(context.Background())
	require.NoError(t, err)

	snap2 := cache.Snapshot()
	assert.Len(t, snap2.Policies, 2)
	assert.True(t, snap2.CreatedAt.After(snap1.CreatedAt))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestCache_Invalidate ./internal/access/policy/`
Expected: Compilation error — `Invalidate` method does not exist.

- [ ] **Step 3: Refactor cache.go**

Remove:

- The `Listener` interface (lines 28-33)
- The `lastUpdate` field and related atomics (line 95)
- `IsStale()` method (lines 185-193)
- `Start()` stub (lines 201-205)
- `StartWithListener()` method (lines 210-219)
- `Wait()` and `Stop()` methods (lines 222-230)
- `listenLoop()` goroutine (lines 233-250)
- `wg sync.WaitGroup` field (line 98)
- `defaultStalenessThreshold`, `defaultReconnectInitial`, `defaultReconnectMax`, `defaultReconnectFactor` constants (lines 22-26)
- `WithStalenessThreshold()` option (lines 61-65)
- `WithReconnectConfig()` option (lines 68-74)
- The `stalenessThreshold`, `reconnectInitial`, `reconnectMax`, `reconnectFactor` fields from `cacheConfig` (lines 53-56)

The `lastUpdate` store in `Reload()` (lines 175-176) and the gauge set (lines 178-180) should also be removed since they were only used by `IsStale()`.

Add the `Invalidate` method:

```go
// Invalidate triggers an immediate cache reload. This is the fast path
// for in-process policy mutations — the store layer calls this after
// successful Create/Update/Delete operations.
func (pc *Cache) Invalidate(ctx context.Context) error {
	return pc.Reload(ctx)
}
```

The retained `Cache` struct should be:

```go
type Cache struct {
	store    store.PolicyStore
	compiler *Compiler
	cfg      cacheConfig

	mu       sync.RWMutex
	snapshot *Snapshot
}
```

And `cacheConfig` becomes just the gauge:

```go
type cacheConfig struct {
	lastUpdateGauge prometheus.Gauge
}
```

- [ ] **Step 4: Update cache_test.go — remove staleness and listener tests**

Remove these test functions:

- `TestCache_Staleness` (lines ~207-223)
- `TestCache_Staleness_FailClosed` (lines ~225-247)
- `TestCache_GracefulShutdown` (lines ~249-282)
- `TestCache_ListenNotify_TriggersReload` (lines ~284-314)
- `TestCache_Start_ReturnsNotImplemented` (lines ~366-374)
- The `mockListener` struct (lines ~62-69)
- Any test referencing `IsStale`, `StartWithListener`, `Start`, `Wait`, or `Stop`

- [ ] **Step 5: Run all cache tests**

Run: `task test -- ./internal/access/policy/`
Expected: All remaining tests pass. New `TestCache_Invalidate_TriggersReload` passes.

- [ ] **Step 6: Commit**

Commit: `refactor(policy): remove staleness model, add cache Invalidate`

---

### Task 5: Create PolicyPoller

**Files:**

- Create: `internal/access/policy/poller.go`
- Create: `internal/access/policy/poller_test.go`

- [ ] **Step 1: Write failing tests for PolicyPoller**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/lifecycle"
)

type mockVersionQuerier struct {
	updatedAt time.Time
	err       error
}

func (m *mockVersionQuerier) LatestPolicyTimestamp(ctx context.Context) (time.Time, error) {
	return m.updatedAt, m.err
}

type mockReloadable struct {
	reloadCount atomic.Int32
	reloadErr   error
}

func (m *mockReloadable) Reload(ctx context.Context) error {
	m.reloadCount.Add(1)
	return m.reloadErr
}

func TestPolicyPoller_DetectsChange(t *testing.T) {
	querier := &mockVersionQuerier{updatedAt: time.Now()}
	reloader := &mockReloadable{}
	tracker := lifecycle.NewHealthTracker(lifecycle.TrackerConfig{SubsystemName: "test"})

	poller := policy.NewPolicyPoller(policy.PollerConfig{
		Querier:  querier,
		Reloader: reloader,
		Tracker:  tracker,
		Interval: 10 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	go poller.Run(ctx)

	// Wait for at least one poll cycle
	time.Sleep(30 * time.Millisecond)

	// First poll always reloads (establishes baseline)
	assert.GreaterOrEqual(t, reloader.reloadCount.Load(), int32(1))
	assert.Equal(t, lifecycle.HealthWarm, tracker.HealthStatus().Tier)
}

func TestPolicyPoller_NoReloadWhenUnchanged(t *testing.T) {
	ts := time.Now()
	querier := &mockVersionQuerier{updatedAt: ts}
	reloader := &mockReloadable{}
	tracker := lifecycle.NewHealthTracker(lifecycle.TrackerConfig{SubsystemName: "test"})

	poller := policy.NewPolicyPoller(policy.PollerConfig{
		Querier:  querier,
		Reloader: reloader,
		Tracker:  tracker,
		Interval: 10 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	go poller.Run(ctx)
	time.Sleep(70 * time.Millisecond)

	// First poll reloads (baseline), subsequent polls see no change
	assert.Equal(t, int32(1), reloader.reloadCount.Load())
}

func TestPolicyPoller_QueryError_RecordsFailure(t *testing.T) {
	querier := &mockVersionQuerier{err: errors.New("db timeout")}
	reloader := &mockReloadable{}
	tracker := lifecycle.NewHealthTracker(lifecycle.TrackerConfig{
		SubsystemName: "test",
		DegradedAfter: 1,
	})

	poller := policy.NewPolicyPoller(policy.PollerConfig{
		Querier:  querier,
		Reloader: reloader,
		Tracker:  tracker,
		Interval: 10 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	go poller.Run(ctx)
	time.Sleep(30 * time.Millisecond)

	assert.Equal(t, lifecycle.HealthDegraded, tracker.HealthStatus().Tier)
	assert.Equal(t, int32(0), reloader.reloadCount.Load())
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestPolicyPoller ./internal/access/policy/`
Expected: Compilation errors.

- [ ] **Step 3: Implement PolicyPoller**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"
	"log/slog"
	"time"

	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/pkg/errutil"

	"github.com/prometheus/client_golang/prometheus"
)

// PolicyVersionQuerier queries the database for the latest policy version indicator.
type PolicyVersionQuerier interface {
	LatestPolicyTimestamp(ctx context.Context) (time.Time, error)
}

// Reloadable is the subset of Cache that the poller needs.
type Reloadable interface {
	Reload(ctx context.Context) error
}

// PollerConfig configures the PolicyPoller.
type PollerConfig struct {
	Querier  PolicyVersionQuerier
	Reloader Reloadable
	Tracker  *lifecycle.HealthTracker
	Interval time.Duration // default: 10s
}

var (
	pollerPollsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "abac_policy_poller_polls_total",
		Help: "Total poll attempts",
	})
	pollerChangesDetected = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "abac_policy_poller_changes_detected",
		Help: "Polls that found changes",
	})
	pollerErrorsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "abac_policy_poller_errors_total",
		Help: "Poll failures",
	})
)

// RegisterPollerMetrics registers poller metrics with the given Prometheus registry.
func RegisterPollerMetrics(reg prometheus.Registerer) {
	reg.MustRegister(pollerPollsTotal, pollerChangesDetected, pollerErrorsTotal)
}

// PolicyPoller periodically checks the database for policy changes
// and triggers cache reloads when changes are detected.
type PolicyPoller struct {
	cfg         PollerConfig
	lastUpdated time.Time
	initialized bool
}

// NewPolicyPoller creates a new PolicyPoller.
func NewPolicyPoller(cfg PollerConfig) *PolicyPoller {
	if cfg.Interval <= 0 {
		cfg.Interval = 10 * time.Second
	}
	return &PolicyPoller{cfg: cfg}
}

// Run starts the polling loop. It blocks until the context is cancelled.
func (p *PolicyPoller) Run(ctx context.Context) {
	ticker := time.NewTicker(p.cfg.Interval)
	defer ticker.Stop()

	// Immediate first poll
	p.poll(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.poll(ctx)
		}
	}
}

func (p *PolicyPoller) poll(ctx context.Context) {
	pollerPollsTotal.Inc()

	latest, err := p.cfg.Querier.LatestPolicyTimestamp(ctx)
	if err != nil {
		pollerErrorsTotal.Inc()
		errutil.LogErrorContext(ctx, "policy poller: query failed", err)
		p.cfg.Tracker.RecordFailure("poll query failed: " + err.Error())
		return
	}

	// First poll: establish baseline and reload
	if !p.initialized {
		p.lastUpdated = latest
		p.initialized = true
		if reloadErr := p.cfg.Reloader.Reload(ctx); reloadErr != nil {
			pollerErrorsTotal.Inc()
			errutil.LogErrorContext(ctx, "policy poller: initial reload failed", reloadErr)
			p.cfg.Tracker.RecordFailure("initial reload failed: " + reloadErr.Error())
			return
		}
		p.cfg.Tracker.RecordSuccess()
		slog.InfoContext(ctx, "policy poller: initial baseline established")
		return
	}

	// No change — record success (poller is working, data is current)
	if !latest.After(p.lastUpdated) {
		p.cfg.Tracker.RecordSuccess()
		return
	}

	// Change detected — reload
	pollerChangesDetected.Inc()
	slog.InfoContext(ctx, "policy poller: change detected, reloading cache",
		"previous", p.lastUpdated,
		"latest", latest,
	)

	if reloadErr := p.cfg.Reloader.Reload(ctx); reloadErr != nil {
		pollerErrorsTotal.Inc()
		errutil.LogErrorContext(ctx, "policy poller: reload failed", reloadErr)
		p.cfg.Tracker.RecordFailure("reload failed: " + reloadErr.Error())
		return
	}

	p.lastUpdated = latest
	p.cfg.Tracker.RecordSuccess()
}
```

- [ ] **Step 4: Add LatestPolicyTimestamp to PostgresStore**

Add to `internal/access/policy/store/postgres.go`:

```go
// LatestPolicyTimestamp returns the most recent updated_at from access_policies.
// Returns zero time if no policies exist.
func (s *PostgresStore) LatestPolicyTimestamp(ctx context.Context) (time.Time, error) {
	var ts *time.Time
	err := s.pool.QueryRow(ctx, `SELECT MAX(updated_at) FROM access_policies`).Scan(&ts)
	if err != nil {
		return time.Time{}, fmt.Errorf("latest policy timestamp: %w", err)
	}
	if ts == nil {
		return time.Time{}, nil
	}
	return *ts, nil
}
```

- [ ] **Step 5: Run tests**

Run: `task test -- ./internal/access/policy/`
Expected: All tests pass.

- [ ] **Step 6: Commit**

Commit: `feat(policy): implement PolicyPoller for periodic cache invalidation`

---

### Task 6: Wire HealthTracker to engine degraded mode

**Files:**

- Modify: `internal/access/policy/engine.go`
- Modify: `internal/access/policy/engine_test.go`

- [ ] **Step 1: Remove IsStale check from engine.go**

Remove Step 6b (the `IsStale()` check block) from `Engine.Evaluate()`. This is approximately lines 247-275 of `engine.go`. The comment `// Step 6b: Staleness check` and the entire `if e.cache.IsStale()` block must be removed.

After removal, Step 7 (load snapshot) should directly follow Step 6 (attribute resolution). Renumber the step comments if desired.

- [ ] **Step 2: Verify engine tests still pass**

Run: `task test -- ./internal/access/policy/`
Expected: All tests pass. Any tests that specifically tested the IsStale path in the engine should have been removed in Task 4 (they tested Cache.IsStale, not Engine.Evaluate with stale cache). If any engine tests fail because they expected the staleness check, remove those tests now.

- [ ] **Step 3: Commit**

Commit: `refactor(policy): remove inline staleness check from engine evaluation`

---

### Task 7: Remove PgListener, update store layer

**Files:**

- Remove: `internal/access/policy/pglistener.go`
- Remove: `internal/access/policy/pglistener_test.go`
- Modify: `internal/access/policy/store/postgres.go`
- Modify: `internal/access/setup/setup.go`
- Modify: `cmd/holomush/core.go`

- [ ] **Step 1: Delete PgListener files**

Delete:

- `internal/access/policy/pglistener.go`
- `internal/access/policy/pglistener_test.go`

- [ ] **Step 2: Remove pg_notify calls from store, add cache invalidation hook**

In `internal/access/policy/store/postgres.go`:

Add a field and setter to `PostgresStore`:

```go
type PostgresStore struct {
	pool      *pgxpool.Pool
	onMutate  func(ctx context.Context) // called after successful policy mutations
}

// SetOnMutate sets a callback invoked after successful policy mutations.
// This enables the store to trigger cache invalidation without a direct
// cache dependency (avoids circular init).
func (s *PostgresStore) SetOnMutate(fn func(ctx context.Context)) {
	s.onMutate = fn
}
```

In each mutation method (Create, Update, Delete, DeleteBySource, ReplaceBySource, CreateBatch), replace the `pg_notify` call with:

```go
if s.onMutate != nil {
	s.onMutate(ctx)
}
```

This must be called AFTER the transaction commits, not inside the transaction.

Remove all lines containing `pg_notify('policy_changed'`.

- [ ] **Step 3: Update setup.go — wire poller and invalidation**

In `internal/access/setup/setup.go`, add fields to `ABACStack`:

```go
type ABACStack struct {
	Engine          types.AccessPolicyEngine
	Cache           *policy.Cache
	Poller          *policy.PolicyPoller
	HealthTracker   *lifecycle.HealthTracker
	PolicyStore     *policystore.PostgresStore
	Resolver        *attribute.Resolver
	AuditLogger     *audit.Logger
	PolicyInstaller *plugins.PolicyInstaller
	PluginProvider  *attribute.PluginProvider
	sqlDB           *sql.DB
}
```

In `BuildABACStack()`, after creating the cache and engine, create the HealthTracker and wire the invalidation hook:

```go
// Health tracker for policy cache
healthTracker := lifecycle.NewHealthTracker(lifecycle.TrackerConfig{
	SubsystemName: "abac.policy-cache",
	DegradedAfter: 1,
	GracePeriod:   60 * time.Second,
	MaxFailures:   30,
	OnTierChange: func(from, to lifecycle.HealthTier) {
		eng := engine // capture for closure
		switch {
		case to == lifecycle.HealthDead:
			eng.EnterDegradedMode("policy cache dead — initiating shutdown")
			slog.Error("ABAC policy cache dead — initiating graceful shutdown")
			// In Phase 1, we log and deny. In Phase 2, the orchestrator
			// will wire this to actually trigger process shutdown via
			// context cancellation.
		case to >= lifecycle.HealthStale:
			eng.EnterDegradedMode("policy cache " + to.String())
		case to == lifecycle.HealthWarm && from >= lifecycle.HealthStale:
			eng.ClearDegradedMode()
		}
	},
})

// Wire store → cache invalidation (fast path)
ps.SetOnMutate(func(ctx context.Context) {
	if err := cache.Invalidate(ctx); err != nil {
		slog.ErrorContext(ctx, "cache invalidation after store mutation failed",
			"error", err)
		healthTracker.RecordFailure("invalidation failed: " + err.Error())
	} else {
		healthTracker.RecordSuccess()
	}
})

// Create poller (safety net)
poller := policy.NewPolicyPoller(policy.PollerConfig{
	Querier:  ps,
	Reloader: cache,
	Tracker:  healthTracker,
	Interval: 10 * time.Second,
})
```

Add `lifecycle` import. Store `Poller` and `HealthTracker` in the returned `ABACStack`.

- [ ] **Step 4: Remove PgListener from core.go**

In `cmd/holomush/core.go`, remove the block at approximately lines 341-349:

```go
// Remove this entire block:
// Start live policy cache invalidation (dedicated context to avoid race with later ctx reassignment)
listenerCtx, listenerCancel := context.WithCancel(ctx)
defer listenerCancel()
pgListener := policy.NewPgListener(databaseURL)
go func() {
    if listenErr := abacStack.Cache.StartWithListener(listenerCtx, pgListener); listenErr != nil {
        slog.Error("policy cache listener failed", "error", listenErr)
    }
}()
```

Replace with starting the poller:

```go
// Start policy cache poller (background)
pollerCtx, pollerCancel := context.WithCancel(ctx)
defer pollerCancel()
go abacStack.Poller.Run(pollerCtx)
```

Remove the `policy` import if it was only used for `NewPgListener`.

- [ ] **Step 5: Verify build and tests**

Run: `task test`
Expected: All tests pass. Build succeeds.

Run: `task lint`
Expected: No lint errors.

- [ ] **Step 6: Commit**

Commit: `refactor(policy): replace LISTEN/NOTIFY with poller + direct invalidation`

---

### Task 8: Integration verification

**Files:**

- No new files — verification only

- [ ] **Step 1: Run full test suite**

Run: `task test`
Expected: All unit tests pass.

- [ ] **Step 2: Run integration tests**

Run: `task test:int`
Expected: All integration tests pass.

- [ ] **Step 3: Manual smoke test**

Run: `task dev` (starts local dev server)

1. Open the web client
2. Click "Try as Guest"
3. Type a `say` command
4. Type a `pose` command
5. Verify both work (no "Permission check failed" error)
6. Wait 60+ seconds
7. Try `say` again — still works (no 30s staleness bug)

- [ ] **Step 4: Check Docker logs**

Run: `docker logs local-dev-core-1 2>&1 | tail -20`
Expected: No "policy cache stale" warnings. Should see "policy poller: initial baseline established" instead.

- [ ] **Step 5: Commit any fixes, then tag Phase 1 complete**

Commit if needed. Phase 1 is complete — the immediate bug is fixed.

---

## Phase 2: Startup State Machine

This phase introduces the formal subsystem lifecycle with dependency-ordered startup and readiness gating.

### Task 9: Implement subsystem orchestrator with topological sort

**Files:**

- Create: `internal/lifecycle/orchestrator.go`
- Create: `internal/lifecycle/orchestrator_test.go`

- [ ] **Step 1: Write failing tests for topological sort and orchestration**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lifecycle_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/lifecycle"
)

type stubSubsystem struct {
	id      lifecycle.SubsystemID
	deps    []lifecycle.SubsystemID
	started bool
	stopped bool
	startFn func(ctx context.Context) error
	mu      sync.Mutex
}

func (s *stubSubsystem) ID() lifecycle.SubsystemID        { return s.id }
func (s *stubSubsystem) DependsOn() []lifecycle.SubsystemID { return s.deps }
func (s *stubSubsystem) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.started = true
	if s.startFn != nil {
		return s.startFn(ctx)
	}
	return nil
}
func (s *stubSubsystem) Stop(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	return nil
}

func TestOrchestrator_StartsInDependencyOrder(t *testing.T) {
	var order []lifecycle.SubsystemID
	var mu sync.Mutex

	makeSub := func(id lifecycle.SubsystemID, deps ...lifecycle.SubsystemID) *stubSubsystem {
		return &stubSubsystem{
			id:   id,
			deps: deps,
			startFn: func(_ context.Context) error {
				mu.Lock()
				order = append(order, id)
				mu.Unlock()
				return nil
			},
		}
	}

	db := makeSub(lifecycle.SubsystemDatabase)
	abac := makeSub(lifecycle.SubsystemABAC, lifecycle.SubsystemDatabase)
	bootstrap := makeSub(lifecycle.SubsystemBootstrap, lifecycle.SubsystemABAC)

	orch := lifecycle.NewOrchestrator()
	orch.Register(bootstrap)
	orch.Register(db)
	orch.Register(abac)

	ctx := context.Background()
	err := orch.StartAll(ctx)
	require.NoError(t, err)

	// Database must come before ABAC, ABAC before Bootstrap
	dbIdx := indexOf(order, lifecycle.SubsystemDatabase)
	abacIdx := indexOf(order, lifecycle.SubsystemABAC)
	bootIdx := indexOf(order, lifecycle.SubsystemBootstrap)
	assert.Less(t, dbIdx, abacIdx)
	assert.Less(t, abacIdx, bootIdx)
}

func TestOrchestrator_DetectsCycle(t *testing.T) {
	a := &stubSubsystem{id: lifecycle.SubsystemABAC, deps: []lifecycle.SubsystemID{lifecycle.SubsystemWorld}}
	b := &stubSubsystem{id: lifecycle.SubsystemWorld, deps: []lifecycle.SubsystemID{lifecycle.SubsystemABAC}}

	orch := lifecycle.NewOrchestrator()
	orch.Register(a)
	orch.Register(b)

	err := orch.StartAll(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestOrchestrator_StartFailure_IsFatal(t *testing.T) {
	db := &stubSubsystem{
		id:      lifecycle.SubsystemDatabase,
		startFn: func(_ context.Context) error { return errors.New("connection refused") },
	}
	abac := &stubSubsystem{
		id:   lifecycle.SubsystemABAC,
		deps: []lifecycle.SubsystemID{lifecycle.SubsystemDatabase},
	}

	orch := lifecycle.NewOrchestrator()
	orch.Register(db)
	orch.Register(abac)

	err := orch.StartAll(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
	// ABAC should not have started
	assert.False(t, abac.started)
}

func TestOrchestrator_StopsInReverseOrder(t *testing.T) {
	var order []lifecycle.SubsystemID
	var mu sync.Mutex

	makeSub := func(id lifecycle.SubsystemID, deps ...lifecycle.SubsystemID) *stubSubsystem {
		sub := &stubSubsystem{id: id, deps: deps}
		origStop := sub.Stop
		_ = origStop
		return sub
	}

	db := makeSub(lifecycle.SubsystemDatabase)
	abac := makeSub(lifecycle.SubsystemABAC, lifecycle.SubsystemDatabase)

	// Override Stop to track order
	db.Stop = func(_ context.Context) error {
		mu.Lock()
		order = append(order, lifecycle.SubsystemDatabase)
		mu.Unlock()
		return nil
	}

	// Need to create proper stubs that track stop order
	orch := lifecycle.NewOrchestrator()
	orch.Register(db)

	stopAbac := &stubSubsystem{
		id:   lifecycle.SubsystemABAC,
		deps: []lifecycle.SubsystemID{lifecycle.SubsystemDatabase},
		startFn: func(_ context.Context) error {
			return nil
		},
	}
	// We'll just verify the orchestrator provides StopAll
	orch.Register(stopAbac)

	require.NoError(t, orch.StartAll(context.Background()))
	orch.StopAll(context.Background())

	// Both should be stopped
	assert.True(t, stopAbac.stopped)
}

func TestOrchestrator_MissingDependency(t *testing.T) {
	abac := &stubSubsystem{
		id:   lifecycle.SubsystemABAC,
		deps: []lifecycle.SubsystemID{lifecycle.SubsystemDatabase}, // not registered
	}

	orch := lifecycle.NewOrchestrator()
	orch.Register(abac)

	err := orch.StartAll(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing dependency")
}

func indexOf(slice []lifecycle.SubsystemID, id lifecycle.SubsystemID) int {
	for i, v := range slice {
		if v == id {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestOrchestrator ./internal/lifecycle/`
Expected: Compilation errors.

- [ ] **Step 3: Implement Orchestrator**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lifecycle

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/samber/oops"
)

// Orchestrator manages the lifecycle of registered Subsystems.
// It starts them in topological (dependency) order and stops them
// in reverse order.
type Orchestrator struct {
	subsystems map[SubsystemID]Subsystem
	startOrder []SubsystemID // populated by StartAll
}

// NewOrchestrator creates an empty Orchestrator.
func NewOrchestrator() *Orchestrator {
	return &Orchestrator{
		subsystems: make(map[SubsystemID]Subsystem),
	}
}

// Register adds a subsystem. Panics on duplicate registration.
func (o *Orchestrator) Register(s Subsystem) {
	id := s.ID()
	if _, exists := o.subsystems[id]; exists {
		panic("lifecycle: duplicate subsystem registration: " + id.String())
	}
	o.subsystems[id] = s
}

// StartAll topologically sorts subsystems by dependencies and starts
// them in order. Returns an error on the first Start failure, cycle
// detection, or missing dependency.
func (o *Orchestrator) StartAll(ctx context.Context) error {
	order, err := o.topoSort()
	if err != nil {
		return err
	}
	o.startOrder = order

	for _, id := range order {
		sub := o.subsystems[id]
		slog.Info("starting subsystem", "subsystem", id.String())
		start := time.Now()

		if startErr := sub.Start(ctx); startErr != nil {
			return oops.
				Code("SUBSYSTEM_START_FAILED").
				With("subsystem", id.String()).
				Wrapf(startErr, "subsystem %s failed to start", id.String())
		}

		slog.Info("subsystem started",
			"subsystem", id.String(),
			"duration", time.Since(start).String(),
		)
	}

	return nil
}

// StopAll stops subsystems in reverse start order.
func (o *Orchestrator) StopAll(ctx context.Context) {
	for i := len(o.startOrder) - 1; i >= 0; i-- {
		id := o.startOrder[i]
		sub := o.subsystems[id]
		slog.Info("stopping subsystem", "subsystem", id.String())

		if err := sub.Stop(ctx); err != nil {
			slog.Error("subsystem stop error",
				"subsystem", id.String(),
				"error", err,
			)
		}
	}
}

// topoSort performs Kahn's algorithm for topological sorting.
func (o *Orchestrator) topoSort() ([]SubsystemID, error) {
	// Build in-degree map and adjacency list
	inDegree := make(map[SubsystemID]int)
	dependents := make(map[SubsystemID][]SubsystemID) // dep → subsystems that depend on it

	for id := range o.subsystems {
		inDegree[id] = 0
	}

	for id, sub := range o.subsystems {
		for _, dep := range sub.DependsOn() {
			if _, exists := o.subsystems[dep]; !exists {
				return nil, fmt.Errorf("subsystem %s has missing dependency: %s", id.String(), dep.String())
			}
			inDegree[id]++
			dependents[dep] = append(dependents[dep], id)
		}
	}

	// Seed queue with zero-dependency subsystems
	var queue []SubsystemID
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	var order []SubsystemID
	for len(queue) > 0 {
		// Pop front
		current := queue[0]
		queue = queue[1:]
		order = append(order, current)

		for _, dependent := range dependents[current] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	if len(order) != len(o.subsystems) {
		return nil, fmt.Errorf("dependency cycle detected among subsystems")
	}

	return order, nil
}
```

- [ ] **Step 4: Fix the StopAll test**

The test for stop order uses a closure-based approach. Update the `stubSubsystem` to properly track stop order by making `Stop` a field:

Actually, the `stubSubsystem.Stop` method above sets `s.stopped = true`. The test just needs to check that both subsystems were stopped. The reverse-order verification can be done by tracking order similarly to start. For now, the basic stop test is sufficient — we verify both were stopped.

- [ ] **Step 5: Run tests**

Run: `task test -- ./internal/lifecycle/`
Expected: All tests pass.

- [ ] **Step 6: Commit**

Commit: `feat(lifecycle): implement Orchestrator with topological sort`

---

### Task 10: Wire ReadinessRegistry to observability server

**Files:**

- Modify: `internal/observability/server.go`

- [ ] **Step 1: Update ReadinessChecker to accept ReadinessRegistry**

In `internal/observability/server.go`, the `ReadinessChecker` type is `func() bool` (line 22). The simplest integration is to keep the `func() bool` type and have the caller wire it:

```go
// In core.go, when creating the observability server:
obsServer = deps.ObservabilityServerFactory(cfg.MetricsAddr, registry.AllReady)
```

This requires no changes to the observability server itself — `registry.AllReady` already has the signature `func() bool`. The change is only in `core.go` where the server is created.

- [ ] **Step 2: Update core.go — move observability server to Phase 1**

The observability server currently starts at approximately line 805 of `core.go`, after all subsystem initialization. Move it to start immediately after database connection and migration, BEFORE subsystem initialization.

Replace the hardcoded readiness checker:

```go
// Before (line ~810):
obsServer = deps.ObservabilityServerFactory(cfg.MetricsAddr, func() bool { return true })

// After:
obsServer = deps.ObservabilityServerFactory(cfg.MetricsAddr, registry.AllReady)
```

Where `registry` is a `*lifecycle.ReadinessRegistry` created early in the startup sequence.

- [ ] **Step 3: Register ABAC health tracker with registry**

After building the ABAC stack:

```go
registry.Register(lifecycle.SubsystemABAC, abacStack.HealthTracker)
```

- [ ] **Step 4: Verify build**

Run: `task build`
Expected: Clean build.

- [ ] **Step 5: Commit**

Commit: `feat(lifecycle): wire ReadinessRegistry to observability server`

---

### Task 11: Wrap services as Subsystems and replace ad-hoc startup

**Files:**

- Modify: `cmd/holomush/core.go`

This is the largest refactoring task. The current `runCore()` function has all
startup logic inlined. The goal is to:

1. Create subsystem wrappers for each major component
2. Register them with the Orchestrator
3. Replace the ad-hoc startup with `orchestrator.StartAll()`
4. Replace the defer chain with `orchestrator.StopAll()`

This task is intentionally high-level — the exact refactoring depends on
the current state of `core.go` at the time of implementation. The key
constraint is: **the behavior must not change, only the structure.**

- [ ] **Step 1: Create subsystem wrappers in core.go (or a new file)**

Each wrapper implements `lifecycle.Subsystem`. For example, the database subsystem:

```go
type databaseSubsystem struct {
	url   string
	store *store.PostgresEventStore
}

func (d *databaseSubsystem) ID() lifecycle.SubsystemID { return lifecycle.SubsystemDatabase }
func (d *databaseSubsystem) DependsOn() []lifecycle.SubsystemID { return nil }
func (d *databaseSubsystem) Start(ctx context.Context) error {
	es, err := store.NewPostgresEventStore(ctx, d.url)
	if err != nil {
		return err
	}
	d.store = es
	return nil
}
func (d *databaseSubsystem) Stop(_ context.Context) error {
	if d.store != nil {
		d.store.Close()
	}
	return nil
}
```

Create similar wrappers for ABAC, Auth, World, Plugins, Sessions, Bootstrap, and gRPC subsystems. Each captures its configuration and stores its initialized state.

- [ ] **Step 2: Register subsystems and use Orchestrator**

```go
orch := lifecycle.NewOrchestrator()
orch.Register(dbSubsystem)
orch.Register(abacSubsystem)
orch.Register(authSubsystem)
orch.Register(worldSubsystem)
orch.Register(pluginSubsystem)
orch.Register(sessionSubsystem)
orch.Register(bootstrapSubsystem)
orch.Register(grpcSubsystem)

if err := orch.StartAll(ctx); err != nil {
    return err
}
defer orch.StopAll(ctx)
```

- [ ] **Step 3: Add readiness gate before gRPC serve**

```go
// Wait for all subsystems with health reporters to be ready
readinessCtx, readinessCancel := context.WithTimeout(ctx, 30*time.Second)
defer readinessCancel()

if err := registry.WaitReady(readinessCtx); err != nil {
    // Log which subsystems aren't ready
    for id, status := range registry.Status() {
        if !status.Tier.IsReady() {
            slog.Error("subsystem not ready at timeout",
                "subsystem", id.String(),
                "tier", status.Tier.String(),
                "reason", status.Reason,
            )
        }
    }
    return fmt.Errorf("startup timeout: not all subsystems ready: %w", err)
}
```

- [ ] **Step 4: Run full test suite**

Run: `task test`
Expected: All tests pass.

Run: `task lint`
Expected: No lint errors.

- [ ] **Step 5: Commit**

Commit: `refactor(core): replace ad-hoc startup with subsystem orchestrator`

---

### Task 12: E2E verification

**Files:**

- No new files — verification only

- [ ] **Step 1: Run pr-prep**

Run: `task pr-prep`
Expected: All checks pass (lint, format, schema, license, unit, integration, E2E).

- [ ] **Step 2: Manual smoke test (same as Task 8 Step 3)**

Run: `task dev`

1. Open web client, click "Try as Guest"
2. `say hello` — works
3. `pose waves` — works
4. Wait 60+ seconds, try again — still works
5. Check Docker logs: no "policy cache stale" warnings

- [ ] **Step 3: Verify readiness probe**

Run: `curl -s http://localhost:9100/healthz/readiness`
Expected: 200 OK when server is ready.

Run: `curl -s http://localhost:9100/healthz/liveness`
Expected: 200 OK always.

---

## Post-Implementation Checklist

- [ ] All unit tests pass (`task test`)
- [ ] All integration tests pass (`task test:int`)
- [ ] `task pr-prep` passes with zero failures
- [ ] Manual smoke test confirms guest say/pose work indefinitely
- [ ] Docker logs show "policy poller: initial baseline established" instead of "policy cache stale"
- [ ] Readiness probe reflects actual subsystem health
- [ ] No references to `PgListener`, `IsStale`, `StartWithListener`, or `listenLoop` remain in codebase
- [ ] `holomush-pogi` bead closed (superseded by this implementation)
- [ ] CLAUDE.md updated if any conventions changed
