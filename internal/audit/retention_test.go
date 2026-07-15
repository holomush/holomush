// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPartitionManager is a mock implementation of PartitionManager for testing.
type mockPartitionManager struct {
	mu                  sync.Mutex
	ensureCalls         int
	purgeCalls          int
	detachCalls         int
	dropCalls           int
	healthCalls         int
	ensureErr           error
	purgeErr            error
	detachErr           error
	dropErr             error
	healthErr           error
	lastPurgeTime       time.Time
	lastDetachTime      time.Time
	lastDropGracePeriod time.Duration
	lastEnsureMonths    int
	purgedRows          int64
	detachedPartitions  []string
	droppedPartitions   []string
}

func (m *mockPartitionManager) EnsurePartitions(_ context.Context, months int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureCalls++
	m.lastEnsureMonths = months
	return m.ensureErr
}

func (m *mockPartitionManager) PurgeExpiredAllows(_ context.Context, olderThan time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.purgeCalls++
	m.lastPurgeTime = olderThan
	if m.purgeErr != nil {
		return 0, m.purgeErr
	}
	return m.purgedRows, nil
}

func (m *mockPartitionManager) DetachExpiredPartitions(_ context.Context, olderThan time.Time) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.detachCalls++
	m.lastDetachTime = olderThan
	if m.detachErr != nil {
		return nil, m.detachErr
	}
	return m.detachedPartitions, nil
}

func (m *mockPartitionManager) DropDetachedPartitions(_ context.Context, gracePeriod time.Duration) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dropCalls++
	m.lastDropGracePeriod = gracePeriod
	if m.dropErr != nil {
		return nil, m.dropErr
	}
	return m.droppedPartitions, nil
}

func (m *mockPartitionManager) HealthCheck(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.healthCalls++
	return m.healthErr
}

func (m *mockPartitionManager) getCalls() (ensure, purge, detach, drop, health int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ensureCalls, m.purgeCalls, m.detachCalls, m.dropCalls, m.healthCalls
}

func TestDefaultRetentionConfig(t *testing.T) {
	cfg := DefaultRetentionConfig()

	assert.Equal(t, 90*24*time.Hour, cfg.RetainDenials, "default denial retention should be 90 days")
	assert.Equal(t, 7*24*time.Hour, cfg.RetainAllows, "default allow retention should be 7 days")
	assert.Equal(t, 24*time.Hour, cfg.PurgeInterval, "default purge interval should be 24 hours")
}

func TestRetentionWorkerRunOnceHappyPath(t *testing.T) {
	cfg := RetentionConfig{
		RetainDenials: 90 * 24 * time.Hour,
		RetainAllows:  7 * 24 * time.Hour,
		PurgeInterval: 24 * time.Hour,
	}

	mock := &mockPartitionManager{
		purgedRows:         42,
		detachedPartitions: []string{"access_audit_log_2025_01", "access_audit_log_2025_02"},
		droppedPartitions:  []string{"access_audit_log_2024_12"},
	}

	now := time.Date(2026, 2, 12, 10, 0, 0, 0, time.UTC)
	worker := NewRetentionWorker(cfg, mock)
	worker.clock = func() time.Time { return now }

	err := worker.RunOnce(context.Background())
	require.NoError(t, err)

	// Verify all operations were called
	ensure, purge, detach, drop, _ := mock.getCalls()
	assert.Equal(t, 1, ensure, "EnsurePartitions should be called once")
	assert.Equal(t, 1, purge, "PurgeExpiredAllows should be called once")
	assert.Equal(t, 1, detach, "DetachExpiredPartitions should be called once")
	assert.Equal(t, 1, drop, "DropDetachedPartitions should be called once")

	// Verify correct parameters
	assert.Equal(t, 3, mock.lastEnsureMonths, "should ensure partitions for 3 months")

	expectedPurgeTime := now.Add(-7 * 24 * time.Hour)
	assert.Equal(t, expectedPurgeTime, mock.lastPurgeTime, "purge cutoff should be now - RetainAllows")

	expectedDetachTime := now.Add(-90 * 24 * time.Hour)
	assert.Equal(t, expectedDetachTime, mock.lastDetachTime, "detach cutoff should be now - RetainDenials")

	assert.Equal(t, 7*24*time.Hour, mock.lastDropGracePeriod, "drop grace period should be 7 days")
}

func TestRetentionWorkerRunOnceEnsurePartitionsError(t *testing.T) {
	cfg := DefaultRetentionConfig()
	mock := &mockPartitionManager{
		ensureErr:          fmt.Errorf("database error"),
		purgedRows:         10,
		detachedPartitions: []string{"partition_1"},
		droppedPartitions:  []string{"partition_2"},
	}

	worker := NewRetentionWorker(cfg, mock)
	err := worker.RunOnce(context.Background())

	// Should return error from EnsurePartitions
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database error")

	// But other operations should still be attempted
	ensure, purge, detach, drop, _ := mock.getCalls()
	assert.Equal(t, 1, ensure)
	assert.Equal(t, 1, purge, "purge should still be attempted after ensure fails")
	assert.Equal(t, 1, detach, "detach should still be attempted after ensure fails")
	assert.Equal(t, 1, drop, "drop should still be attempted after ensure fails")
}

func TestRetentionWorkerRunOncePurgesExpiredAllows(t *testing.T) {
	cfg := RetentionConfig{
		RetainDenials: 90 * 24 * time.Hour,
		RetainAllows:  14 * 24 * time.Hour, // 2 weeks
		PurgeInterval: 24 * time.Hour,
	}

	mock := &mockPartitionManager{
		purgedRows: 100,
	}

	now := time.Date(2026, 2, 12, 15, 30, 0, 0, time.UTC)
	worker := NewRetentionWorker(cfg, mock)
	worker.clock = func() time.Time { return now }

	err := worker.RunOnce(context.Background())
	require.NoError(t, err)

	// Verify correct cutoff time passed to PurgeExpiredAllows
	expectedCutoff := now.Add(-14 * 24 * time.Hour)
	assert.Equal(t, expectedCutoff, mock.lastPurgeTime)
}

func TestRetentionWorkerRunOnceDetachesExpiredPartitions(t *testing.T) {
	cfg := RetentionConfig{
		RetainDenials: 60 * 24 * time.Hour, // 60 days
		RetainAllows:  7 * 24 * time.Hour,
		PurgeInterval: 24 * time.Hour,
	}

	mock := &mockPartitionManager{
		detachedPartitions: []string{"access_audit_log_2025_12", "access_audit_log_2025_11"},
	}

	now := time.Date(2026, 2, 12, 8, 0, 0, 0, time.UTC)
	worker := NewRetentionWorker(cfg, mock)
	worker.clock = func() time.Time { return now }

	err := worker.RunOnce(context.Background())
	require.NoError(t, err)

	// Verify correct cutoff time passed to DetachExpiredPartitions
	expectedCutoff := now.Add(-60 * 24 * time.Hour)
	assert.Equal(t, expectedCutoff, mock.lastDetachTime)
}

func TestRetentionWorkerStartStopLifecycle(t *testing.T) {
	cfg := RetentionConfig{
		RetainDenials: 90 * 24 * time.Hour,
		RetainAllows:  7 * 24 * time.Hour,
		PurgeInterval: 100 * time.Millisecond, // Short interval for testing
	}

	mock := &mockPartitionManager{
		purgedRows: 1,
	}

	worker := NewRetentionWorker(cfg, mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := worker.Start(ctx)
	require.NoError(t, err)

	// Wait for at least 2 cycles
	time.Sleep(250 * time.Millisecond)

	// Stop the worker
	worker.Stop()

	// Verify worker ran multiple times
	ensure, purge, detach, drop, _ := mock.getCalls()
	assert.GreaterOrEqual(t, ensure, 2, "should run at least 2 cycles")
	assert.GreaterOrEqual(t, purge, 2, "should run at least 2 cycles")
	assert.GreaterOrEqual(t, detach, 2, "should run at least 2 cycles")
	assert.GreaterOrEqual(t, drop, 2, "should run at least 2 cycles")
}

func TestRetentionWorkerWithSkipFirstRunDefersDestructiveCycle(t *testing.T) {
	cfg := RetentionConfig{
		RetainDenials: 90 * 24 * time.Hour,
		RetainAllows:  7 * 24 * time.Hour,
		PurgeInterval: 120 * time.Millisecond,
	}
	mock := &mockPartitionManager{}

	worker := NewRetentionWorker(cfg, mock, WithSkipFirstRun())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, worker.Start(ctx))

	// Before the first tick (PurgeInterval=120ms) NO destructive cycle runs.
	// Assert the condition STAYS false for a window comfortably inside one
	// interval, rather than sleeping a fixed duration and sampling once (a
	// delayed goroutine would otherwise flake).
	require.Never(t, func() bool {
		_, _, detach, drop, _ := mock.getCalls()
		return detach > 0 || drop > 0
	}, 80*time.Millisecond, 10*time.Millisecond, "no destructive cycle before the first tick")

	// After the first tick, at least one destructive cycle runs. Poll with a
	// timeout comfortably larger than one PurgeInterval instead of a fixed sleep.
	require.Eventually(t, func() bool {
		_, _, detach, drop, _ := mock.getCalls()
		return detach >= 1 && drop >= 1
	}, 2*time.Second, 10*time.Millisecond, "detach and drop fire after the first tick")
	worker.Stop()
}

func TestRetentionWorkerDefaultRunsImmediately(t *testing.T) {
	cfg := RetentionConfig{
		RetainDenials: 90 * 24 * time.Hour,
		RetainAllows:  7 * 24 * time.Hour,
		PurgeInterval: 10 * time.Second, // long: only the immediate run should fire
	}
	mock := &mockPartitionManager{}

	worker := NewRetentionWorker(cfg, mock) // no option → immediate run preserved
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, worker.Start(ctx))
	// The immediate RunOnce fires quickly (no tick needed).
	require.Eventually(t, func() bool {
		_, _, detach, _, _ := mock.getCalls()
		return detach >= 1
	}, 2*time.Second, 10*time.Millisecond, "default worker runs a destructive cycle immediately on Start")
	worker.Stop()
}

func TestRetentionWorkerHealthCheckDelegation(t *testing.T) {
	cfg := DefaultRetentionConfig()
	mock := &mockPartitionManager{
		healthErr: fmt.Errorf("partition missing"),
	}

	worker := NewRetentionWorker(cfg, mock)
	err := worker.HealthCheck(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "partition missing")

	_, _, _, _, health := mock.getCalls()
	assert.Equal(t, 1, health, "HealthCheck should delegate to manager")
}

func TestRetentionWorkerRunOnceAllErrorsCombined(t *testing.T) {
	cfg := DefaultRetentionConfig()
	mock := &mockPartitionManager{
		ensureErr: fmt.Errorf("ensure failed"),
		purgeErr:  fmt.Errorf("purge failed"),
		detachErr: fmt.Errorf("detach failed"),
		dropErr:   fmt.Errorf("drop failed"),
	}

	worker := NewRetentionWorker(cfg, mock)
	err := worker.RunOnce(context.Background())

	require.Error(t, err)
	// Should contain all error messages
	assert.Contains(t, err.Error(), "ensure failed")
	assert.Contains(t, err.Error(), "purge failed")
	assert.Contains(t, err.Error(), "detach failed")
	assert.Contains(t, err.Error(), "drop failed")

	// All operations should still be attempted
	ensure, purge, detach, drop, _ := mock.getCalls()
	assert.Equal(t, 1, ensure)
	assert.Equal(t, 1, purge)
	assert.Equal(t, 1, detach)
	assert.Equal(t, 1, drop)
}

func TestRetentionWorkerStartStopGracefulShutdown(t *testing.T) {
	cfg := RetentionConfig{
		RetainDenials: 90 * 24 * time.Hour,
		RetainAllows:  7 * 24 * time.Hour,
		PurgeInterval: 1 * time.Second, // Longer interval
	}

	mock := &mockPartitionManager{
		purgedRows: 1,
	}

	worker := NewRetentionWorker(cfg, mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := worker.Start(ctx)
	require.NoError(t, err)

	// Stop immediately
	worker.Stop()

	// Should complete without hanging
	// (test will timeout if Stop doesn't work properly)
}
