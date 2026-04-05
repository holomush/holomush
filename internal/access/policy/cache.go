// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/holomush/holomush/internal/access/policy/store"
)

// CachedPolicy pairs a stored policy with its compiled form.
type CachedPolicy struct {
	ID       string
	Name     string
	Compiled *CompiledPolicy
}

// Snapshot is an immutable, read-only view of compiled policies.
// It is safe for concurrent reads without locking.
type Snapshot struct {
	Policies  []CachedPolicy
	CreatedAt time.Time
}

// CacheOption configures Cache behavior.
type CacheOption func(*cacheConfig)

type cacheConfig struct {
	lastUpdateGauge prometheus.Gauge
}

// WithLastUpdateGauge returns a CacheOption that sets the cache's last-update Prometheus gauge.
// If non-nil, the cache will set this gauge to the Unix timestamp of the last successful reload.
func WithLastUpdateGauge(g prometheus.Gauge) CacheOption {
	return func(c *cacheConfig) {
		c.lastUpdateGauge = g
	}
}

// Cache provides concurrent access to compiled ABAC policies.
// Call Reload or Invalidate to refresh the snapshot.
type Cache struct {
	store    store.PolicyStore
	compiler *Compiler
	cfg      cacheConfig

	mu       sync.RWMutex
	snapshot *Snapshot
}

// NewCache creates a Cache with the given store, compiler, and options.
// The cache is not populated on construction; call Reload (or Invalidate) to
// load policies before first use.
func NewCache(s store.PolicyStore, compiler *Compiler, opts ...CacheOption) *Cache {
	var cfg cacheConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	return &Cache{
		store:    s,
		compiler: compiler,
		cfg:      cfg,
		snapshot: &Snapshot{}, // empty, non-nil snapshot
	}
}

// Snapshot returns the current read-only policy snapshot.
// The returned snapshot is safe for concurrent use.
// Returns a copy of the slice to prevent callers from mutating the snapshot.
func (pc *Cache) Snapshot() *Snapshot {
	pc.mu.RLock()
	snap := pc.snapshot
	pc.mu.RUnlock()

	// Return a copy with its own slice to prevent mutation.
	copied := &Snapshot{
		Policies:  make([]CachedPolicy, len(snap.Policies)),
		CreatedAt: snap.CreatedAt,
	}
	copy(copied.Policies, snap.Policies)
	return copied
}

// Reload fetches enabled policies from the store, compiles them, and atomically
// swaps the snapshot. The write lock is held only during the pointer swap (~50us),
// not during the DB fetch + compilation (~50ms).
func (pc *Cache) Reload(ctx context.Context) error {
	// Fetch and compile without holding the lock.
	stored, err := pc.store.ListEnabled(ctx)
	if err != nil {
		return fmt.Errorf("policy cache reload: list enabled: %w", err)
	}

	policies := make([]CachedPolicy, 0, len(stored))
	for _, sp := range stored {
		compiled, _, compileErr := pc.compiler.Compile(sp.DSLText)
		if compileErr != nil {
			return fmt.Errorf("policy cache reload: compile %q (id=%s): %w", sp.Name, sp.ID, compileErr)
		}
		policies = append(policies, CachedPolicy{
			ID:       sp.ID,
			Name:     sp.Name,
			Compiled: compiled,
		})
	}

	snap := &Snapshot{
		Policies:  policies,
		CreatedAt: time.Now(),
	}

	// Atomic swap — write lock held only for pointer assignment.
	pc.mu.Lock()
	pc.snapshot = snap
	pc.mu.Unlock()

	if pc.cfg.lastUpdateGauge != nil {
		pc.cfg.lastUpdateGauge.Set(float64(time.Now().Unix()))
	}

	return nil
}

// Invalidate triggers an immediate cache reload. This is the fast path
// for in-process policy mutations — the store layer calls this after
// successful Create/Update/Delete operations.
func (pc *Cache) Invalidate(ctx context.Context) error {
	return pc.Reload(ctx)
}

// CacheLastUpdate is the default Prometheus gauge for tracking the last
// successful policy cache reload. Register with your Prometheus registry at startup.
var CacheLastUpdate = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: "abac_policy_cache_last_update",
	Help: "Unix timestamp of the last successful policy cache reload",
})

// RegisterCacheMetrics registers policy cache metrics with the given Prometheus registry.
func RegisterCacheMetrics(reg prometheus.Registerer) {
	reg.MustRegister(CacheLastUpdate)
}
