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

	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/store"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/pkg/errutil"
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
func (m *mockPolicyStore) DeleteBySource(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}

func (m *mockPolicyStore) CreateBatch(_ context.Context, _ []*store.StoredPolicy) error { return nil }

func (m *mockPolicyStore) ReplaceBySource(_ context.Context, _, _ string, _ []*store.StoredPolicy) error {
	return nil
}

func (m *mockPolicyStore) List(_ context.Context, _ store.ListOptions) ([]*store.StoredPolicy, error) {
	return nil, nil
}

// --- slowPolicyStore ---

type slowPolicyStore struct {
	policies []*store.StoredPolicy
	err      error
	delay    time.Duration
	calls    atomic.Int64
}

func (m *slowPolicyStore) ListEnabled(ctx context.Context) ([]*store.StoredPolicy, error) {
	m.calls.Add(1)
	select {
	case <-time.After(m.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return m.policies, m.err
}

func (m *slowPolicyStore) Create(_ context.Context, _ *store.StoredPolicy) error { return nil }
func (m *slowPolicyStore) Get(_ context.Context, _ string) (*store.StoredPolicy, error) {
	return nil, nil
}

func (m *slowPolicyStore) GetByID(_ context.Context, _ string) (*store.StoredPolicy, error) {
	return nil, nil
}
func (m *slowPolicyStore) Update(_ context.Context, _ *store.StoredPolicy) error { return nil }
func (m *slowPolicyStore) Delete(_ context.Context, _ string) error              { return nil }
func (m *slowPolicyStore) DeleteBySource(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}

func (m *slowPolicyStore) CreateBatch(_ context.Context, _ []*store.StoredPolicy) error { return nil }

func (m *slowPolicyStore) ReplaceBySource(_ context.Context, _, _ string, _ []*store.StoredPolicy) error {
	return nil
}

func (m *slowPolicyStore) List(_ context.Context, _ store.ListOptions) ([]*store.StoredPolicy, error) {
	return nil, nil
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

func TestCacheReload(t *testing.T) {
	ms := &mockPolicyStore{policies: testPolicies()}
	compiler := testCompiler()
	cache := NewCache(ms, compiler)

	// Before reload, snapshot should be nil or empty.
	snap, err := cache.Snapshot(context.Background())
	require.NoError(t, err)
	require.NotNil(t, snap, "snapshot should never be nil (zero value)")
	assert.Empty(t, snap.Policies, "snapshot should have no policies before reload")

	// Reload.
	err = cache.Reload(context.Background())
	require.NoError(t, err)

	// Snapshot should now contain compiled policies.
	snap, err = cache.Snapshot(context.Background())
	require.NoError(t, err)
	require.NotNil(t, snap)
	assert.Len(t, snap.Policies, 2, "snapshot should contain 2 compiled policies")
	assert.Equal(t, "pol-1", snap.Policies[0].ID)
	assert.Equal(t, "pol-2", snap.Policies[1].ID)
	assert.NotNil(t, snap.Policies[0].Compiled)
	assert.NotNil(t, snap.Policies[1].Compiled)

	// Store should have been called once.
	assert.Equal(t, int64(1), ms.calls.Load())
}

func TestCacheReloadFailsOnCompilationError(t *testing.T) {
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
	snap, err := cache.Snapshot(context.Background())
	require.NoError(t, err)
	assert.Empty(t, snap.Policies)
}

func TestCacheReloadFailsOnStoreError(t *testing.T) {
	ms := &mockPolicyStore{
		err: assert.AnError,
	}
	compiler := testCompiler()
	cache := NewCache(ms, compiler)

	err := cache.Reload(context.Background())
	assert.Error(t, err, "reload should propagate store errors")
}

func TestCacheSnapshotIsSafeConcurrently(t *testing.T) {
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
				snap, err := cache.Snapshot(context.Background())
				require.NoError(t, err)
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

func TestCacheReloadUpdatesMetric(t *testing.T) {
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

func TestSnapshotIsImmutable(t *testing.T) {
	ms := &mockPolicyStore{policies: testPolicies()}
	compiler := testCompiler()
	cache := NewCache(ms, compiler)

	require.NoError(t, cache.Reload(context.Background()))

	snap1, err := cache.Snapshot(context.Background())
	require.NoError(t, err)
	snap2, err := cache.Snapshot(context.Background())
	require.NoError(t, err)

	// Both snapshots should reference the same underlying data.
	assert.Equal(t, len(snap1.Policies), len(snap2.Policies))

	// Modifying the returned slice should not affect the snapshot.
	if len(snap1.Policies) > 0 {
		snap1.Policies[0] = CachedPolicy{}
		assert.NotEqual(t, snap1.Policies[0].ID, snap2.Policies[0].ID,
			"snapshots should be independent copies")
	}
}

func TestCacheInvalidateTriggersReload(t *testing.T) {
	dslText := `permit(principal, action, resource);`
	ms := &mockPolicyStore{
		policies: []*store.StoredPolicy{
			{ID: "p1", Name: "test-policy", DSLText: dslText, Enabled: true},
		},
	}
	compiler := testCompiler()
	cache := NewCache(ms, compiler)

	// Initial load
	require.NoError(t, cache.Reload(context.Background()))
	snap1, err := cache.Snapshot(context.Background())
	require.NoError(t, err)
	require.Len(t, snap1.Policies, 1)

	// Add a second policy
	ms.policies = append(ms.policies, &store.StoredPolicy{
		ID: "p2", Name: "test-policy-2", DSLText: dslText, Enabled: true,
	})

	// Invalidate triggers reload
	err = cache.Invalidate(context.Background())
	require.NoError(t, err)

	snap2, err := cache.Snapshot(context.Background())
	require.NoError(t, err)
	assert.Len(t, snap2.Policies, 2)
	assert.False(t, snap2.CreatedAt.Before(snap1.CreatedAt),
		"snap2.CreatedAt should not be before snap1.CreatedAt")
}

// TestCacheInvalidatePropagatesStoreError verifies that a store error returned
// during Invalidate is forwarded to the caller.
func TestCacheInvalidatePropagatesStoreError(t *testing.T) {
	ms := &mockPolicyStore{err: assert.AnError}
	compiler := testCompiler()
	cache := NewCache(ms, compiler)

	err := cache.Invalidate(context.Background())

	assert.Error(t, err)
}

// TestCacheInvalidatePreservesSnapshotOnError verifies that a failed Invalidate
// propagates the error through Snapshot. With the read barrier design, once a
// reload fails the barrier carries the error so Snapshot returns it — callers
// must handle the error and may trigger a fresh Reload to recover.
func TestCacheInvalidatePreservesSnapshotOnError(t *testing.T) {
	dslText := `permit(principal, action, resource);`
	ms := &mockPolicyStore{
		policies: []*store.StoredPolicy{
			{ID: "p1", Name: "test-policy", DSLText: dslText, Enabled: true},
		},
	}
	compiler := testCompiler()
	cache := NewCache(ms, compiler)

	// Load a valid snapshot first.
	require.NoError(t, cache.Reload(context.Background()))
	snapBefore, err := cache.Snapshot(context.Background())
	require.NoError(t, err)
	require.Len(t, snapBefore.Policies, 1)

	// Now cause the store to return an error.
	ms.err = assert.AnError

	err = cache.Invalidate(context.Background())
	assert.Error(t, err)

	// After a failed Invalidate the read barrier carries the reload error, so
	// Snapshot returns an error rather than stale data.
	_, snapErr := cache.Snapshot(context.Background())
	assert.Error(t, snapErr, "Snapshot must return error after failed Invalidate")
	assert.ErrorIs(t, snapErr, assert.AnError)
}

// TestCacheInvalidateConcurrentSafe verifies that concurrent Invalidate calls
// do not race or corrupt the snapshot.
func TestCacheInvalidateConcurrentSafe(t *testing.T) {
	dslText := `permit(principal, action, resource);`
	ms := &mockPolicyStore{
		policies: []*store.StoredPolicy{
			{ID: "p1", Name: "test-policy", DSLText: dslText, Enabled: true},
		},
	}
	compiler := testCompiler()
	cache := NewCache(ms, compiler)

	require.NoError(t, cache.Reload(context.Background()))

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()
			_ = cache.Invalidate(context.Background())
			snap, err := cache.Snapshot(context.Background())
			require.NoError(t, err)
			assert.NotNil(t, snap)
		}()
	}

	wg.Wait()
}

func TestSnapshotBlocksDuringInvalidation(t *testing.T) {
	delay := 100 * time.Millisecond
	ms := &slowPolicyStore{policies: testPolicies(), delay: delay}
	cache := NewCache(ms, testCompiler())
	require.NoError(t, cache.Reload(context.Background()))

	invalidateDone := make(chan error, 1)
	go func() { invalidateDone <- cache.Invalidate(context.Background()) }()
	time.Sleep(10 * time.Millisecond)

	start := time.Now()
	snap, err := cache.Snapshot(context.Background())
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Len(t, snap.Policies, 2)
	assert.GreaterOrEqual(t, elapsed, delay/2, "Snapshot should have blocked during reload")
	require.NoError(t, <-invalidateDone)
}

func TestSnapshotReturnsErrorWhenBarrierReloadFails(t *testing.T) {
	ms := &slowPolicyStore{policies: testPolicies(), delay: 50 * time.Millisecond}
	cache := NewCache(ms, testCompiler())
	require.NoError(t, cache.Reload(context.Background()))

	ms.err = assert.AnError
	invalidateDone := make(chan error, 1)
	go func() { invalidateDone <- cache.Invalidate(context.Background()) }()
	time.Sleep(10 * time.Millisecond)

	_, err := cache.Snapshot(context.Background())
	assert.Error(t, err)
	assert.ErrorIs(t, err, assert.AnError)
	assert.Error(t, <-invalidateDone)
}

func TestSnapshotReturnsContextErrorOnTimeout(t *testing.T) {
	ms := &slowPolicyStore{policies: testPolicies(), delay: 500 * time.Millisecond}
	cache := NewCache(ms, testCompiler())
	require.NoError(t, cache.Reload(context.Background()))

	go func() { _ = cache.Invalidate(context.Background()) }()
	time.Sleep(10 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := cache.Snapshot(ctx)
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestMultipleReadersBlockedOnBarrierGetFreshData(t *testing.T) {
	ms := &slowPolicyStore{delay: 50 * time.Millisecond}
	cache := NewCache(ms, testCompiler())
	require.NoError(t, cache.Reload(context.Background()))

	ms.policies = testPolicies()
	go func() { _ = cache.Invalidate(context.Background()) }()
	time.Sleep(10 * time.Millisecond)

	const readers = 10
	results := make(chan int, readers)
	for range readers {
		go func() {
			snap, err := cache.Snapshot(context.Background())
			if err != nil {
				results <- -1
				return
			}
			results <- len(snap.Policies)
		}()
	}

	for range readers {
		select {
		case n := <-results:
			assert.Equal(t, 2, n, "all readers should see fresh data after barrier")
		case <-time.After(2 * time.Second):
			t.Fatal("reader timed out")
		}
	}
}

func TestSnapshotFastPathWhenNoReloadInProgress(t *testing.T) {
	ms := &mockPolicyStore{policies: testPolicies()}
	cache := NewCache(ms, testCompiler())
	require.NoError(t, cache.Reload(context.Background()))

	start := time.Now()
	snap, err := cache.Snapshot(context.Background())
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Len(t, snap.Policies, 2)
	assert.Less(t, elapsed, 5*time.Millisecond, "fast path should complete in under 5ms")
}

func TestCoalescingOverlappingInvalidations(t *testing.T) {
	ms := &slowPolicyStore{policies: testPolicies(), delay: 100 * time.Millisecond}
	cache := NewCache(ms, testCompiler())
	require.NoError(t, cache.Reload(context.Background()))
	initialCalls := ms.calls.Load()

	done1 := make(chan error, 1)
	go func() { done1 <- cache.Invalidate(context.Background()) }()
	time.Sleep(10 * time.Millisecond)

	done2 := make(chan error, 1)
	done3 := make(chan error, 1)
	go func() { done2 <- cache.Invalidate(context.Background()) }()
	go func() { done3 <- cache.Invalidate(context.Background()) }()

	require.NoError(t, <-done2)
	require.NoError(t, <-done3)
	require.NoError(t, <-done1)

	assert.Equal(t, initialCalls+2, ms.calls.Load(),
		"overlapping invalidations should coalesce into one re-reload")
}

// --- The live `action` gate at the action-only compilation sites (02.2-04) ---
//
// Sites 2 and 3 of the D-66 inventory — the bootstrap seed installer
// (internal/bootstrap/setup/subsystem.go) and the WithRealABAC harness
// (internal/testsupport/integrationtest/real_abac.go) — build their compiler on
// attribute.NewActionOnlySchemaRegistry().Schema(). Neither has a provider set to
// draw on: at both, seeding runs before (or independently of) any provider
// registration. These tests pin what that registry does and, just as importantly,
// what it does NOT do.

// TestActionOnlyCompilerRejectsAnUndeclaredActionKey pins the seed-installation
// half of D-67: the compiler sites 2 and 3 build carries the live gate, so a seed
// with a typo'd action.* key fails at INSTALL rather than installing and only
// blowing up at a later cache.Reload.
//
// Why this asserts the compiler rather than driving policy.Bootstrap with a bad
// seed: Bootstrap compiles policy.SeedPolicies(), a fixed function with no
// injection seam, so "a seed set containing one bad policy" is not constructible.
// The two halves that compose the property are each pinned — that a compile error
// is FATAL to Bootstrap is TestBootstrapCompilationErrorIsFatal; that this
// compiler produces one for an undeclared action.* key is here.
func TestActionOnlyCompilerRejectsAnUndeclaredActionKey(t *testing.T) {
	t.Parallel()

	compiler := NewCompiler(attribute.NewActionOnlySchemaRegistry().Schema())

	_, _, err := compiler.Compile(
		`permit(principal, action, resource) when { action.typo_key == "x" };`,
	)

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "POLICY_UNREGISTERED_ACTION_ATTRIBUTE")
	assert.Contains(t, err.Error(), "action.typo_key")
}

// TestCacheReloadRejectsAnOperatorAuthoredRowWithAnUndeclaredActionKey is the
// whole content of D-67: the branch is fatal for ALL policy sources, not just
// in-tree seeds.
//
// The distinguishing detail is Source — this row did not come from
// policy.SeedPolicies(), it is the shape an operator's INSERT into the policies
// table produces, and it is driven through the same Cache.Reload the production
// poller and invalidation paths use. A deployment carrying such a row fails to
// boot on upgrade; that consequence was accepted knowingly, and the compensating
// control is that the failure names the row precisely enough to fix by hand.
func TestCacheReloadRejectsAnOperatorAuthoredRowWithAnUndeclaredActionKey(t *testing.T) {
	t.Parallel()

	compiler := NewCompiler(attribute.NewActionOnlySchemaRegistry().Schema())
	ms := &mockPolicyStore{policies: []*store.StoredPolicy{{
		ID:      "01JQ00000000000000000000D1",
		Name:    "operator-authored-audit-grant",
		Source:  "admin",
		DSLText: `permit(principal, action, resource) when { action.event_type == "core-comm:whisper" };`,
		Enabled: true,
	}}}

	err := NewCache(ms, compiler).Reload(context.Background())

	require.Error(t, err, "an operator-authored row is NOT exempt from the action gate (D-67)")
	errutil.AssertErrorCode(t, err, "POLICY_UNREGISTERED_ACTION_ATTRIBUTE")
	got := err.Error()
	assert.Contains(t, got, "operator-authored-audit-grant", "the failure MUST name the offending policy")
	assert.Contains(t, got, "01JQ00000000000000000000D1", "the failure MUST name the row's DB id")
	assert.Contains(t, got, "action.event_type", "the failure MUST name the offending key")
}

// TestActionOnlyRegistrySkipsEveryRootItDoesNotCarry pins the mechanism that
// makes an action-only registry the CORRECT scope for sites 2 and 3 rather than a
// lossy stand-in for the full production stack.
//
// The compiler validates by DSL ROOT — the grammar constrains every root to
// exactly principal | resource | action | env, and collectAttrRefs sets namespace
// to that root with key as the whole dotted remainder. So
// `resource.character.anything` is looked up as namespace "resource", key
// "character.anything"; the mechanism is NOT HasNamespace("character"), and a
// provider name is never a namespace the compiler looks up at all.
//
// Two consequences this test asserts together:
//
//	(a) `action` IS a grammar root, so registering it makes the hard-error branch
//	    live — identically at every compilation site.
//	(b) Every OTHER root is unregistered here, so references under them are
//	    skipped, producing neither an error nor a warning. Do not restate this as
//	    "fatal is uniform across sites, WARN is not" — that is false. WARN fires
//	    only for an unregistered KEY inside a REGISTERED ROOT (see
//	    TestCompileUnknownAttributeInRegisteredNamespaceWarns, which registers a
//	    namespace literally named "resource").
func TestActionOnlyRegistrySkipsEveryRootItDoesNotCarry(t *testing.T) {
	t.Parallel()

	reg := attribute.NewActionOnlySchemaRegistry()
	require.True(t, reg.HasNamespace("action"), "(a) the action root MUST be registered")
	require.False(t, reg.HasNamespace("resource"),
		"(b) control: no `resource` ROOT namespace here — the warning path below is unreachable "+
			"by construction, which is what makes the zero-warning assertion meaningful")

	policy, warnings, err := NewCompiler(reg.Schema()).Compile(
		`permit(principal, action, resource) when { resource.character.anything == "x" };`,
	)

	require.NoError(t, err)
	require.NotNil(t, policy)
	assert.Empty(t, warnings,
		"a reference under an unregistered root is neither rejected nor warned about")
}
