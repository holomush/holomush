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

// readBarrier is a one-shot broadcast result for the read barrier.
// Readers wait on done; err is written before close(done) so Go's
// memory model guarantees visibility without additional synchronization.
type readBarrier struct {
	done chan struct{}
	err  error
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

	// Read barrier: readers wait on barrier.done before reading snapshot.
	// A closed done channel = ready (fast path). An open channel = reload in progress.
	barrierMu sync.Mutex
	barrier   *readBarrier
	dirty     bool // true if Invalidate called during active reload
}

// NewCache creates a Cache with the given store, compiler, and options.
// The cache is not populated on construction; call Reload (or Invalidate) to
// load policies before first use.
func NewCache(s store.PolicyStore, compiler *Compiler, opts ...CacheOption) *Cache {
	var cfg cacheConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	ready := &readBarrier{done: make(chan struct{})}
	close(ready.done)

	return &Cache{
		store:    s,
		compiler: compiler,
		cfg:      cfg,
		snapshot: &Snapshot{},
		barrier:  ready,
	}
}

// Snapshot returns the current read-only policy snapshot. If a reload is
// in progress, it blocks until the reload completes or the context expires.
// Returns a copy of the slice to prevent callers from mutating the snapshot.
func (pc *Cache) Snapshot(ctx context.Context) (*Snapshot, error) {
	// Grab current barrier reference.
	pc.barrierMu.Lock()
	b := pc.barrier
	pc.barrierMu.Unlock()

	// Wait for barrier (fast path: already closed channel returns immediately).
	select {
	case <-b.done:
		// Barrier passed.
	case <-ctx.Done():
		return nil, fmt.Errorf("policy cache snapshot: context expired: %w", ctx.Err())
	}

	// If the reload that released this barrier failed, propagate the error.
	if b.err != nil {
		return nil, fmt.Errorf("policy cache reload failed: %w", b.err)
	}

	pc.mu.RLock()
	snap := pc.snapshot
	pc.mu.RUnlock()

	copied := &Snapshot{
		Policies:  make([]CachedPolicy, len(snap.Policies)),
		CreatedAt: snap.CreatedAt,
	}
	copy(copied.Policies, snap.Policies)
	return copied, nil
}

// Reload fetches enabled policies from the store, compiles them, and atomically
// swaps the snapshot. The write lock is held only during the pointer swap (~50us),
// not during the DB fetch + compilation (~50ms).
//
// Corruption handling is all-or-nothing: if any policy fails to compile, Reload
// returns an error and the snapshot pointer swap never happens, so the last-good
// snapshot is retained. A corrupt policy therefore never enters the cache and can
// grant nothing — default-deny holds. Both Reload callers (see internal/access/setup)
// log the failure and record it against the cache HealthTracker: the poller on its
// poll path, and the store-mutation OnMutate callback on the invalidation fast path.
// The OnMutate path reloads behind the read barrier, so a failure there propagates
// to concurrent Snapshot callers as an error (never stale data); the poll path
// absorbs the failure silently and keeps serving the last-good snapshot. Persistent
// failures escalate the health tier, which drives Engine.EnterDegradedMode (deny-all,
// fail-closed) and ClearDegradedMode on recovery.
func (pc *Cache) Reload(ctx context.Context) error {
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

	pc.mu.Lock()
	pc.snapshot = snap
	pc.mu.Unlock()

	if pc.cfg.lastUpdateGauge != nil {
		pc.cfg.lastUpdateGauge.Set(float64(time.Now().Unix()))
	}

	return nil
}

// Invalidate engages the read barrier and reloads the cache. Concurrent
// Snapshot() calls block until the reload completes. If a reload is already
// in progress, sets a dirty flag so another reload follows immediately.
func (pc *Cache) Invalidate(ctx context.Context) error {
	pc.barrierMu.Lock()

	select {
	case <-pc.barrier.done:
		// Barrier is closed (no active reload) — proceed.
	default:
		// Barrier is open (reload in progress) — queue the next generation now
		// so readers that start after this invalidation wait on the follow-up reload.
		if !pc.dirty {
			pc.barrier = &readBarrier{done: make(chan struct{})}
		}
		pc.dirty = true
		pc.barrierMu.Unlock()
		return nil
	}

	b := &readBarrier{done: make(chan struct{})}
	pc.barrier = b
	pc.dirty = false
	pc.barrierMu.Unlock()

	return pc.barrierReloadLoop(ctx, b)
}

// barrierReloadLoop runs reload cycles until no dirty flag is set.
func (pc *Cache) barrierReloadLoop(ctx context.Context, b *readBarrier) error {
	for {
		err := pc.Reload(ctx)

		pc.barrierMu.Lock()
		if pc.dirty {
			b.err = err
			close(b.done)

			b = pc.barrier // use the barrier already published by Invalidate
			pc.dirty = false
			pc.barrierMu.Unlock()
			continue
		}

		b.err = err
		close(b.done)
		pc.barrierMu.Unlock()
		return err
	}
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
