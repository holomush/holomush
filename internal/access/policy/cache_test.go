// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/store"
	"github.com/holomush/holomush/internal/access/policy/types"
)

// --- Mock PolicyStore ---

type mockPolicyStore struct {
	policies []*store.StoredPolicy
	err      error
	calls    atomic.Int64
}

func (m *mockPolicyStore) ListEnabled(_ context.Context) ([]*store.StoredPolicy, error) {
	m.calls.Add(1)
	return m.policies, m.err
}

func (m *mockPolicyStore) Create(_ context.Context, _ *store.StoredPolicy) error { return nil }
func (m *mockPolicyStore) Get(_ context.Context, _ string) (*store.StoredPolicy, error) {
	return nil, nil
}

func (m *mockPolicyStore) GetByID(_ context.Context, _ string) (*store.StoredPolicy, error) {
	return nil, nil
}
func (m *mockPolicyStore) Update(_ context.Context, _ *store.StoredPolicy) error { return nil }
func (m *mockPolicyStore) Delete(_ context.Context, _ string) error              { return nil }
func (m *mockPolicyStore) List(_ context.Context, _ store.ListOptions) ([]*store.StoredPolicy, error) {
	return nil, nil
}

// --- Mock Listener ---

type mockListener struct {
	ch  chan string
	err error
}

func (m *mockListener) Listen(_ context.Context) (<-chan string, error) {
	return m.ch, m.err
}

// --- Test helpers ---

func testCompiler() *Compiler {
	return NewCompiler(emptySchema())
}

func testPolicies() []*store.StoredPolicy {
	return []*store.StoredPolicy{
		{
			ID:      "pol-1",
			Name:    "allow-read",
			Enabled: true,
			Effect:  types.PolicyEffectPermit,
			DSLText: `permit(principal, action, resource);`,
		},
		{
			ID:      "pol-2",
			Name:    "deny-delete",
			Enabled: true,
			Effect:  types.PolicyEffectForbid,
			DSLText: `forbid(principal, action in ["delete"], resource);`,
		},
	}
}

// newTestGauge returns a fresh gauge for test isolation.
func newTestGauge() prometheus.Gauge {
	return prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "test_abac_policy_cache_last_update",
		Help: "Test gauge",
	})
}

// --- Tests ---

func TestCache_Reload(t *testing.T) {
	ms := &mockPolicyStore{policies: testPolicies()}
	compiler := testCompiler()
	cache := NewCache(ms, compiler)

	// Before reload, snapshot should be nil or empty.
	snap := cache.Snapshot()
	require.NotNil(t, snap, "snapshot should never be nil (zero value)")
	assert.Empty(t, snap.Policies, "snapshot should have no policies before reload")

	// Reload.
	err := cache.Reload(context.Background())
	require.NoError(t, err)

	// Snapshot should now contain compiled policies.
	snap = cache.Snapshot()
	require.NotNil(t, snap)
	assert.Len(t, snap.Policies, 2, "snapshot should contain 2 compiled policies")
	assert.Equal(t, "pol-1", snap.Policies[0].ID)
	assert.Equal(t, "pol-2", snap.Policies[1].ID)
	assert.NotNil(t, snap.Policies[0].Compiled)
	assert.NotNil(t, snap.Policies[1].Compiled)

	// Store should have been called once.
	assert.Equal(t, int64(1), ms.calls.Load())
}

func TestCache_Reload_CompilationError(t *testing.T) {
	ms := &mockPolicyStore{
		policies: []*store.StoredPolicy{
			{
				ID:      "pol-bad",
				Name:    "bad-policy",
				Enabled: true,
				Effect:  types.PolicyEffectPermit,
				DSLText: `this is not valid DSL`,
			},
		},
	}
	compiler := testCompiler()
	cache := NewCache(ms, compiler)

	err := cache.Reload(context.Background())
	assert.Error(t, err, "reload should fail when a policy cannot compile")

	// Snapshot should still be empty (no partial update).
	snap := cache.Snapshot()
	assert.Empty(t, snap.Policies)
}

func TestCache_Reload_StoreError(t *testing.T) {
	ms := &mockPolicyStore{
		err: assert.AnError,
	}
	compiler := testCompiler()
	cache := NewCache(ms, compiler)

	err := cache.Reload(context.Background())
	assert.Error(t, err, "reload should propagate store errors")
}

func TestCache_Snapshot_Concurrent(t *testing.T) {
	ms := &mockPolicyStore{policies: testPolicies()}
	compiler := testCompiler()
	cache := NewCache(ms, compiler)

	// Initial load.
	require.NoError(t, cache.Reload(context.Background()))

	const goroutines = 50
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines + 1) // readers + 1 reloader

	// Concurrent readers.
	for range goroutines {
		go func() {
			defer wg.Done()
			for range iterations {
				snap := cache.Snapshot()
				require.NotNil(t, snap)
				// Snapshot should be consistent: either 0 or 2 policies.
				n := len(snap.Policies)
				assert.True(t, n == 0 || n == 2,
					"snapshot should be atomic, got %d policies", n)
			}
		}()
	}

	// Concurrent reloader.
	go func() {
		defer wg.Done()
		for range iterations {
			_ = cache.Reload(context.Background())
		}
	}()

	wg.Wait()
}

func TestCache_Staleness(t *testing.T) {
	ms := &mockPolicyStore{policies: testPolicies()}
	compiler := testCompiler()
	threshold := 50 * time.Millisecond
	cache := NewCache(ms, compiler, WithStalenessThreshold(threshold))

	// Before any reload, cache is stale.
	assert.True(t, cache.IsStale(), "cache should be stale before first reload")

	// After reload, cache is fresh.
	require.NoError(t, cache.Reload(context.Background()))
	assert.False(t, cache.IsStale(), "cache should be fresh immediately after reload")

	// Wait for staleness threshold to pass.
	time.Sleep(threshold + 10*time.Millisecond)
	assert.True(t, cache.IsStale(), "cache should be stale after threshold")
}

func TestCache_Staleness_FailClosed(t *testing.T) {
	ms := &mockPolicyStore{policies: testPolicies()}
	compiler := testCompiler()
	threshold := 50 * time.Millisecond
	cache := NewCache(ms, compiler, WithStalenessThreshold(threshold))

	// Load policies.
	require.NoError(t, cache.Reload(context.Background()))

	// Wait for staleness.
	time.Sleep(threshold + 10*time.Millisecond)
	require.True(t, cache.IsStale())

	// When stale, callers should treat the cache as unreliable and default deny.
	// The cache itself returns a snapshot, but IsStale() signals fail-closed.
	snap := cache.Snapshot()
	assert.NotNil(t, snap, "snapshot should still be returned even when stale")
	assert.True(t, cache.IsStale(), "IsStale should remain true until next reload")

	// After a fresh reload, staleness clears.
	require.NoError(t, cache.Reload(context.Background()))
	assert.False(t, cache.IsStale())
}

func TestCache_GracefulShutdown(t *testing.T) {
	ch := make(chan string, 1)
	listener := &mockListener{ch: ch}

	ms := &mockPolicyStore{policies: testPolicies()}
	compiler := testCompiler()
	cache := NewCache(ms, compiler)

	ctx, cancel := context.WithCancel(context.Background())

	// Start the background listener.
	err := cache.StartWithListener(ctx, listener)
	require.NoError(t, err)

	// Give the goroutine a moment to start.
	time.Sleep(20 * time.Millisecond)

	// Cancel the context — goroutine should exit cleanly.
	cancel()

	// Wait for the goroutine to finish (with timeout).
	done := make(chan struct{})
	go func() {
		cache.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Goroutine exited cleanly.
	case <-time.After(2 * time.Second):
		t.Fatal("goroutine did not exit within timeout after context cancellation")
	}
}

func TestCache_ListenNotify_TriggersReload(t *testing.T) {
	ch := make(chan string, 1)
	listener := &mockListener{ch: ch}

	ms := &mockPolicyStore{policies: testPolicies()}
	compiler := testCompiler()
	cache := NewCache(ms, compiler)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initial reload so we have a baseline.
	require.NoError(t, cache.Reload(context.Background()))
	assert.Equal(t, int64(1), ms.calls.Load())

	// Start listener.
	err := cache.StartWithListener(ctx, listener)
	require.NoError(t, err)

	// Send a notification.
	ch <- "policy_changed"

	// Wait for the reload to happen.
	require.Eventually(t, func() bool {
		return ms.calls.Load() >= 2
	}, 2*time.Second, 10*time.Millisecond,
		"store should be called again after NOTIFY")

	cancel()
	cache.Wait()
}

func TestCache_ReloadMetric(t *testing.T) {
	ms := &mockPolicyStore{policies: testPolicies()}
	compiler := testCompiler()
	gauge := newTestGauge()
	cache := NewCache(ms, compiler, WithLastUpdateGauge(gauge))

	// Before reload, gauge should be 0.
	assert.Equal(t, float64(0), testutil.ToFloat64(gauge))

	// After reload, gauge should be set to a recent Unix timestamp.
	before := time.Now().Unix()
	require.NoError(t, cache.Reload(context.Background()))
	after := time.Now().Unix()

	val := testutil.ToFloat64(gauge)
	assert.GreaterOrEqual(t, val, float64(before), "gauge should be >= reload start time")
	assert.LessOrEqual(t, val, float64(after), "gauge should be <= reload end time")
}

func TestSnapshot_Immutable(t *testing.T) {
	ms := &mockPolicyStore{policies: testPolicies()}
	compiler := testCompiler()
	cache := NewCache(ms, compiler)

	require.NoError(t, cache.Reload(context.Background()))

	snap1 := cache.Snapshot()
	snap2 := cache.Snapshot()

	// Both snapshots should reference the same underlying data.
	assert.Equal(t, len(snap1.Policies), len(snap2.Policies))

	// Modifying the returned slice should not affect the snapshot.
	if len(snap1.Policies) > 0 {
		snap1.Policies[0] = CachedPolicy{}
		assert.NotEqual(t, snap1.Policies[0].ID, snap2.Policies[0].ID,
			"snapshots should be independent copies")
	}
}

func TestCacheOption_WithStalenessThreshold(t *testing.T) {
	ms := &mockPolicyStore{policies: testPolicies()}
	compiler := testCompiler()

	// Very long threshold — should not be stale after reload.
	cache := NewCache(ms, compiler, WithStalenessThreshold(1*time.Hour))
	require.NoError(t, cache.Reload(context.Background()))
	assert.False(t, cache.IsStale())
}
