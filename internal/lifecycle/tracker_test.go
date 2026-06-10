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

// Compile-time interface check: *lifecycle.HealthTracker must satisfy lifecycle.HealthReporter.
var _ lifecycle.HealthReporter = (*lifecycle.HealthTracker)(nil)

func defaultTrackerConfig() lifecycle.TrackerConfig {
	return lifecycle.TrackerConfig{
		SubsystemName: "test",
		GracePeriod:   60 * time.Second,
		MaxFailures:   30,
	}
}

func TestHealthTrackerInitialState(t *testing.T) {
	ht := lifecycle.NewHealthTracker(defaultTrackerConfig())
	status := ht.HealthStatus()
	assert.Equal(t, lifecycle.HealthWarm, status.Tier)
	assert.Equal(t, "initialized", status.Reason)
}

func TestHealthTrackerSingleFailureDegrades(t *testing.T) {
	ht := lifecycle.NewHealthTracker(defaultTrackerConfig())

	ht.RecordFailure("poll timeout")
	status := ht.HealthStatus()
	assert.Equal(t, lifecycle.HealthDegraded, status.Tier)
	assert.Equal(t, "poll timeout", status.Reason)
}

func TestHealthTrackerSuccessAfterFailureRecoversToWarm(t *testing.T) {
	ht := lifecycle.NewHealthTracker(defaultTrackerConfig())

	ht.RecordFailure("poll timeout")
	require.Equal(t, lifecycle.HealthDegraded, ht.HealthStatus().Tier)

	ht.RecordSuccess()
	assert.Equal(t, lifecycle.HealthWarm, ht.HealthStatus().Tier)
}

func TestHealthTrackerGracePeriodExpiryBecomesStale(t *testing.T) {
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

func TestHealthTrackerMaxFailuresBecomesDead(t *testing.T) {
	cfg := defaultTrackerConfig()
	cfg.GracePeriod = 1 * time.Millisecond // near-immediate stale
	cfg.MaxFailures = 3
	ht := lifecycle.NewHealthTracker(cfg)

	ht.RecordFailure("fail 1") // → degraded
	time.Sleep(2 * time.Millisecond)
	ht.RecordFailure("fail 2") // → stale (grace expired)
	ht.RecordFailure("fail 3") // still stale, counting
	ht.RecordFailure("fail 4") // → dead (3 failures in stale)
	assert.Equal(t, lifecycle.HealthDead, ht.HealthStatus().Tier)
}

func TestHealthTrackerDeadIsTerminal(t *testing.T) {
	cfg := defaultTrackerConfig()
	cfg.GracePeriod = 1 * time.Millisecond
	cfg.MaxFailures = 1
	ht := lifecycle.NewHealthTracker(cfg)

	ht.RecordFailure("fail 1") // → degraded
	time.Sleep(2 * time.Millisecond)
	ht.RecordFailure("fail 2") // → stale
	ht.RecordFailure("fail 3") // → dead

	// Success should NOT recover from dead
	ht.RecordSuccess()
	assert.Equal(t, lifecycle.HealthDead, ht.HealthStatus().Tier)
}

func TestHealthTrackerStaleRecovery(t *testing.T) {
	cfg := defaultTrackerConfig()
	cfg.GracePeriod = 1 * time.Millisecond
	cfg.MaxFailures = 30
	ht := lifecycle.NewHealthTracker(cfg)

	ht.RecordFailure("fail")
	time.Sleep(2 * time.Millisecond)
	ht.RecordFailure("fail") // → stale
	require.Equal(t, lifecycle.HealthStale, ht.HealthStatus().Tier)

	ht.RecordSuccess()
	assert.Equal(t, lifecycle.HealthWarm, ht.HealthStatus().Tier)
}

func TestHealthTrackerOnTierChangeCallback(t *testing.T) {
	var transitions []lifecycle.HealthTier
	cfg := defaultTrackerConfig()
	cfg.OnTierChange = func(_, to lifecycle.HealthTier) {
		transitions = append(transitions, to)
	}
	ht := lifecycle.NewHealthTracker(cfg)

	ht.RecordFailure("fail") // warm → degraded
	ht.RecordSuccess()       // degraded → warm
	assert.Equal(t, []lifecycle.HealthTier{lifecycle.HealthDegraded, lifecycle.HealthWarm}, transitions)
}
