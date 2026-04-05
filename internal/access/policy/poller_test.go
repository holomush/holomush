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
