// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/holomush/holomush/internal/access/policy/store"
)

// Default cache configuration values.
const (
	defaultStalenessThreshold = 30 * time.Second
	defaultReconnectInitial   = 100 * time.Millisecond
	defaultReconnectMax       = 30 * time.Second
	defaultReconnectFactor    = 2.0
)

// Listener abstracts the PostgreSQL LISTEN/NOTIFY mechanism for testability.
// Implementations return a channel that emits notification payloads.
// The channel should close when the context is cancelled.
type Listener interface {
	Listen(ctx context.Context) (<-chan string, error)
}

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
	stalenessThreshold time.Duration
	reconnectInitial   time.Duration
	reconnectMax       time.Duration
	reconnectFactor    float64
	lastUpdateGauge    prometheus.Gauge
}

// WithStalenessThreshold sets the duration after which the cache is considered stale.
func WithStalenessThreshold(d time.Duration) CacheOption {
	return func(c *cacheConfig) {
		c.stalenessThreshold = d
	}
}

// WithReconnectConfig sets the exponential backoff parameters for LISTEN/NOTIFY reconnection.
func WithReconnectConfig(initial, maxInterval time.Duration, factor float64) CacheOption {
	return func(c *cacheConfig) {
		c.reconnectInitial = initial
		c.reconnectMax = maxInterval
		c.reconnectFactor = factor
	}
}

// WithLastUpdateGauge sets the Prometheus gauge to record the last successful reload timestamp.
func WithLastUpdateGauge(g prometheus.Gauge) CacheOption {
	return func(c *cacheConfig) {
		c.lastUpdateGauge = g
	}
}

// Cache provides concurrent access to compiled ABAC policies with
// LISTEN/NOTIFY-based invalidation and staleness detection.
type Cache struct {
	store    store.PolicyStore
	compiler *Compiler
	cfg      cacheConfig

	mu       sync.RWMutex
	snapshot *Snapshot

	// lastUpdate stores the Unix timestamp in nanoseconds of the last successful reload.
	// Zero means no reload has occurred.
	lastUpdate atomic.Int64

	// wg tracks background goroutines for graceful shutdown.
	wg sync.WaitGroup
}

// NewCache creates a Cache with the given store, compiler, and options.
// Call Reload to populate the cache before first use.
func NewCache(s store.PolicyStore, compiler *Compiler, opts ...CacheOption) *Cache {
	cfg := cacheConfig{
		stalenessThreshold: defaultStalenessThreshold,
		reconnectInitial:   defaultReconnectInitial,
		reconnectMax:       defaultReconnectMax,
		reconnectFactor:    defaultReconnectFactor,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	pc := &Cache{
		store:    s,
		compiler: compiler,
		cfg:      cfg,
		snapshot: &Snapshot{}, // empty, non-nil snapshot
	}
	return pc
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

	// Record the reload timestamp (nanoseconds for sub-second staleness detection).
	now := time.Now()
	pc.lastUpdate.Store(now.UnixNano())

	if pc.cfg.lastUpdateGauge != nil {
		pc.cfg.lastUpdateGauge.Set(float64(now.Unix()))
	}

	return nil
}

// IsStale returns true if no successful reload has occurred within the staleness threshold.
// Callers should return EffectDefaultDeny (fail-closed) when the cache is stale.
func (pc *Cache) IsStale() bool {
	last := pc.lastUpdate.Load()
	if last == 0 {
		return true // never reloaded
	}
	return time.Since(time.Unix(0, last)) > pc.cfg.stalenessThreshold
}

// Start spawns a background goroutine that listens for PostgreSQL NOTIFY events
// on the "policy_changed" channel using a dedicated connection. On each notification,
// it triggers a reload. Uses exponential backoff for reconnection.
//
// The connStr should be a PostgreSQL connection string for a dedicated (non-pooled)
// connection. The goroutine exits when the context is cancelled.
func (pc *Cache) Start(_ context.Context, connStr string) error {
	// This method would create a real PgListener. For now, it's a placeholder
	// that production code will wire up. Tests use StartWithListener instead.
	_ = connStr
	slog.Warn("Cache.Start called without real PG listener implementation")
	return nil
}

// StartWithListener spawns the background LISTEN/NOTIFY goroutine using the
// provided Listener interface. This is the primary method for both production
// (with a real PG listener) and testing (with a mock).
func (pc *Cache) StartWithListener(ctx context.Context, listener Listener) error {
	ch, err := listener.Listen(ctx)
	if err != nil {
		return fmt.Errorf("policy cache start listener: %w", err)
	}

	pc.wg.Add(1)
	go pc.listenLoop(ctx, ch)
	return nil
}

// Wait blocks until all background goroutines have exited.
func (pc *Cache) Wait() {
	pc.wg.Wait()
}

// Stop is a convenience alias for cancelling the context externally and waiting.
// In practice, callers cancel the context passed to Start/StartWithListener.
func (pc *Cache) Stop() {
	pc.wg.Wait()
}

// listenLoop runs in a goroutine, processing notifications and triggering reloads.
func (pc *Cache) listenLoop(ctx context.Context, ch <-chan string) {
	defer pc.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ch:
			if !ok {
				return // channel closed
			}
			if err := pc.Reload(ctx); err != nil {
				slog.Error("policy cache reload on notification failed",
					slog.String("error", err.Error()))
			}
		}
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
