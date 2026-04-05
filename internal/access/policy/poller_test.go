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
	count     int64
	err       error
}

func (m *mockVersionQuerier) LatestPolicyVersion(_ context.Context) (time.Time, int64, error) {
	return m.updatedAt, m.count, m.err
}

type mockReloadable struct {
	reloadCount atomic.Int32
	reloadErr   error
}

func (m *mockReloadable) Reload(_ context.Context) error {
	m.reloadCount.Add(1)
	return m.reloadErr
}

func TestPolicyPollerDetectsChange(t *testing.T) {
	querier := &mockVersionQuerier{updatedAt: time.Now(), count: 5}
	reloader := &mockReloadable{}
	tracker := lifecycle.NewHealthTracker(lifecycle.TrackerConfig{SubsystemName: "test"})

	poller, pollerErr := policy.NewPoller(policy.PollerConfig{
		Querier:  querier,
		Reloader: reloader,
		Tracker:  tracker,
		Interval: 10 * time.Millisecond,
	})
	require.NoError(t, pollerErr)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go poller.Run(ctx)

	// First poll always reloads (establishes baseline)
	require.Eventually(t, func() bool {
		return reloader.reloadCount.Load() >= 1
	}, 500*time.Millisecond, 5*time.Millisecond)
	assert.Equal(t, lifecycle.HealthWarm, tracker.HealthStatus().Tier)
}

func TestPolicyPollerNoReloadWhenUnchanged(t *testing.T) {
	ts := time.Now()
	querier := &mockVersionQuerier{updatedAt: ts, count: 5}
	reloader := &mockReloadable{}
	tracker := lifecycle.NewHealthTracker(lifecycle.TrackerConfig{SubsystemName: "test"})

	poller, pollerErr := policy.NewPoller(policy.PollerConfig{
		Querier:  querier,
		Reloader: reloader,
		Tracker:  tracker,
		Interval: 10 * time.Millisecond,
	})
	require.NoError(t, pollerErr)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go poller.Run(ctx)

	// Wait for baseline reload, then give several poll cycles to confirm no additional reloads.
	require.Eventually(t, func() bool {
		return reloader.reloadCount.Load() >= 1
	}, 500*time.Millisecond, 5*time.Millisecond)

	// Verify reload count never exceeds 1 over several poll cycles.
	require.Never(t, func() bool {
		return reloader.reloadCount.Load() > 1
	}, 50*time.Millisecond, 5*time.Millisecond)
}

func TestPolicyPollerQueryErrorRecordsFailure(t *testing.T) {
	querier := &mockVersionQuerier{err: errors.New("db timeout")}
	reloader := &mockReloadable{}
	tracker := lifecycle.NewHealthTracker(lifecycle.TrackerConfig{
		SubsystemName: "test",
		GracePeriod:   5 * time.Second, // stay Degraded (not Stale) during the test window
	})

	poller, pollerErr := policy.NewPoller(policy.PollerConfig{
		Querier:  querier,
		Reloader: reloader,
		Tracker:  tracker,
		Interval: 10 * time.Millisecond,
	})
	require.NoError(t, pollerErr)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go poller.Run(ctx)

	require.Eventually(t, func() bool {
		return tracker.HealthStatus().Tier == lifecycle.HealthDegraded
	}, 500*time.Millisecond, 5*time.Millisecond)

	assert.Equal(t, int32(0), reloader.reloadCount.Load())
}

// TestNewPoller_NilQuerier verifies that NewPoller returns an error when Querier is nil.
func TestNewPoller_NilQuerier(t *testing.T) {
	tracker := lifecycle.NewHealthTracker(lifecycle.TrackerConfig{SubsystemName: "test"})
	_, err := policy.NewPoller(policy.PollerConfig{
		Querier:  nil,
		Reloader: &mockReloadable{},
		Tracker:  tracker,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Querier")
}

// TestNewPoller_NilReloader verifies that NewPoller returns an error when Reloader is nil.
func TestNewPoller_NilReloader(t *testing.T) {
	tracker := lifecycle.NewHealthTracker(lifecycle.TrackerConfig{SubsystemName: "test"})
	_, err := policy.NewPoller(policy.PollerConfig{
		Querier:  &mockVersionQuerier{},
		Reloader: nil,
		Tracker:  tracker,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Reloader")
}

// TestNewPoller_NilTracker verifies that NewPoller returns an error when Tracker is nil.
func TestNewPoller_NilTracker(t *testing.T) {
	_, err := policy.NewPoller(policy.PollerConfig{
		Querier:  &mockVersionQuerier{},
		Reloader: &mockReloadable{},
		Tracker:  nil,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Tracker")
}

// TestNewPoller_ZeroIntervalDefaultsTo10s verifies that a zero Interval is replaced with 10s.
// We cannot inspect the internal field directly from the external test package, so we verify
// indirectly: NewPoller must succeed and the returned value must be non-nil.
func TestNewPoller_ZeroIntervalDefaultsTo10s(t *testing.T) {
	tracker := lifecycle.NewHealthTracker(lifecycle.TrackerConfig{SubsystemName: "test"})
	poller, err := policy.NewPoller(policy.PollerConfig{
		Querier:  &mockVersionQuerier{},
		Reloader: &mockReloadable{},
		Tracker:  tracker,
		Interval: 0,
	})
	require.NoError(t, err)
	require.NotNil(t, poller)
}

// TestPolicyPoller_ContextCancellationStopsLoop verifies Run exits when context is cancelled.
func TestPolicyPoller_ContextCancellationStopsLoop(t *testing.T) {
	querier := &mockVersionQuerier{updatedAt: time.Now(), count: 1}
	reloader := &mockReloadable{}
	tracker := lifecycle.NewHealthTracker(lifecycle.TrackerConfig{SubsystemName: "test"})

	poller, err := policy.NewPoller(policy.PollerConfig{
		Querier:  querier,
		Reloader: reloader,
		Tracker:  tracker,
		Interval: 50 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		poller.Run(ctx)
		close(done)
	}()

	// Let it complete the first poll
	require.Eventually(t, func() bool {
		return reloader.reloadCount.Load() >= 1
	}, 500*time.Millisecond, 5*time.Millisecond)

	cancel()

	select {
	case <-done:
		// Run() returned — good
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run() did not exit after context cancellation")
	}
}

// TestPolicyPoller_InitialReloadFailureIsRetried verifies that a failed initial reload
// does NOT mark the poller as initialized, so the next poll retries.
func TestPolicyPoller_InitialReloadFailureIsRetried(t *testing.T) {
	querier := &mockVersionQuerier{updatedAt: time.Now(), count: 3}
	reloader := &mockReloadable{reloadErr: errors.New("transient error")}
	tracker := lifecycle.NewHealthTracker(lifecycle.TrackerConfig{
		SubsystemName: "test",
		GracePeriod:   5 * time.Second,
	})

	poller, err := policy.NewPoller(policy.PollerConfig{
		Querier:  querier,
		Reloader: reloader,
		Tracker:  tracker,
		Interval: 20 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	go poller.Run(ctx)

	// Reload should be attempted multiple times because initialisation never succeeds
	require.Eventually(t, func() bool {
		return reloader.reloadCount.Load() >= 2
	}, 300*time.Millisecond, 5*time.Millisecond, "expected retried reload attempts on initial failure")

	// Health should be degraded due to repeated failures
	assert.Equal(t, lifecycle.HealthDegraded, tracker.HealthStatus().Tier)
}

// TestPolicyPoller_ChangeDetectedByCountOnly verifies that a count change
// (same timestamp, different count) triggers a reload.
func TestPolicyPoller_ChangeDetectedByCountOnly(t *testing.T) {
	ts := time.Now()
	querier := &mockVersionQuerier{updatedAt: ts, count: 5}
	reloader := &mockReloadable{}
	tracker := lifecycle.NewHealthTracker(lifecycle.TrackerConfig{SubsystemName: "test"})

	poller, err := policy.NewPoller(policy.PollerConfig{
		Querier:  querier,
		Reloader: reloader,
		Tracker:  tracker,
		Interval: 20 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go poller.Run(ctx)

	// Wait for baseline reload
	require.Eventually(t, func() bool {
		return reloader.reloadCount.Load() >= 1
	}, 500*time.Millisecond, 5*time.Millisecond)

	// Change only the count (same timestamp) — simulates a policy deletion
	querier.count = 4

	require.Eventually(t, func() bool {
		return reloader.reloadCount.Load() >= 2
	}, 500*time.Millisecond, 10*time.Millisecond, "reload should be triggered by count change alone")
}

// TestPolicyPoller_ChangeDetectedByTimestamp verifies that a newer timestamp
// (same count) triggers a reload.
func TestPolicyPoller_ChangeDetectedByTimestamp(t *testing.T) {
	ts := time.Now()
	querier := &mockVersionQuerier{updatedAt: ts, count: 5}
	reloader := &mockReloadable{}
	tracker := lifecycle.NewHealthTracker(lifecycle.TrackerConfig{SubsystemName: "test"})

	poller, err := policy.NewPoller(policy.PollerConfig{
		Querier:  querier,
		Reloader: reloader,
		Tracker:  tracker,
		Interval: 20 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go poller.Run(ctx)

	// Wait for baseline reload
	require.Eventually(t, func() bool {
		return reloader.reloadCount.Load() >= 1
	}, 500*time.Millisecond, 5*time.Millisecond)

	// Advance timestamp — simulates an update to an existing policy
	querier.updatedAt = ts.Add(time.Second)

	require.Eventually(t, func() bool {
		return reloader.reloadCount.Load() >= 2
	}, 500*time.Millisecond, 10*time.Millisecond, "reload should be triggered by timestamp advance")
}

// TestPolicyPoller_ReloadErrorAfterBaselineRecordsFailure verifies that a reload
// error on a subsequent (non-initial) poll records a failure on the tracker.
func TestPolicyPoller_ReloadErrorAfterBaselineRecordsFailure(t *testing.T) {
	ts := time.Now()
	querier := &mockVersionQuerier{updatedAt: ts, count: 1}
	reloader := &mockReloadable{} // first reload succeeds
	tracker := lifecycle.NewHealthTracker(lifecycle.TrackerConfig{
		SubsystemName: "test",
		GracePeriod:   5 * time.Second,
	})

	poller, err := policy.NewPoller(policy.PollerConfig{
		Querier:  querier,
		Reloader: reloader,
		Tracker:  tracker,
		Interval: 20 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go poller.Run(ctx)

	// Wait for healthy baseline
	require.Eventually(t, func() bool {
		return reloader.reloadCount.Load() >= 1
	}, 500*time.Millisecond, 5*time.Millisecond)
	assert.Equal(t, lifecycle.HealthWarm, tracker.HealthStatus().Tier)

	// Now inject a reload error and trigger a change
	reloader.reloadErr = errors.New("reload failed")
	querier.count = 2

	require.Eventually(t, func() bool {
		return tracker.HealthStatus().Tier == lifecycle.HealthDegraded
	}, 500*time.Millisecond, 5*time.Millisecond, "tracker should degrade after reload error on change")
}