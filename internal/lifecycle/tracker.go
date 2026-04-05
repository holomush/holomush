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

	// GracePeriod is the duration to remain in Degraded before
	// transitioning to Stale if failures continue. Default: 60s.
	GracePeriod time.Duration

	// MaxFailures is the number of consecutive failures in Stale
	// before transitioning to Dead. Default: 30.
	MaxFailures int

	// OnTierChange is called when the health tier changes.
	// It is invoked outside the lock so callbacks may safely read HealthTracker.
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

// NewHealthTracker creates a HealthTracker configured by cfg.
// If cfg.GracePeriod is zero it defaults to 60s; if cfg.MaxFailures is <= 0 it defaults to 30.
// The tracker starts in the HealthWarm tier with reason "initialized" and the current time as the since timestamp.
// If cfg.SubsystemName is non-empty the corresponding health metric gauge is initialized to the warm tier.
func NewHealthTracker(cfg TrackerConfig) *HealthTracker {
	if cfg.GracePeriod == 0 {
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
	var change *tierChange

	ht.mu.Lock()
	if ht.tier == HealthDead {
		ht.mu.Unlock()
		return // terminal — no recovery
	}
	if ht.tier != HealthWarm {
		change = ht.transitionLocked(HealthWarm, "recovered")
	}
	ht.staleFailures = 0
	ht.mu.Unlock()

	if change != nil {
		ht.notifyTierChange(change)
	}
}

// RecordFailure signals a failed operation and advances tier based
// on configured thresholds.
func (ht *HealthTracker) RecordFailure(reason string) {
	var change *tierChange

	ht.mu.Lock()
	now := time.Now()

	switch ht.tier {
	case HealthWarm:
		change = ht.transitionLocked(HealthDegraded, reason)
		ht.degradedAt = now

	case HealthDegraded:
		if now.Sub(ht.degradedAt) >= ht.cfg.GracePeriod {
			change = ht.transitionLocked(HealthStale, reason)
			ht.staleFailures = 1
		}

	case HealthStale:
		ht.staleFailures++
		if ht.staleFailures >= ht.cfg.MaxFailures {
			change = ht.transitionLocked(HealthDead, reason)
		}

	case HealthDead:
		// terminal — ignore
	}
	ht.mu.Unlock()

	if change != nil {
		ht.notifyTierChange(change)
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

// tierChange captures a tier transition for deferred callback invocation.
type tierChange struct {
	from, to HealthTier
}

// transitionLocked updates state under lock and returns the transition info.
// The caller MUST hold ht.mu.Lock().
func (ht *HealthTracker) transitionLocked(to HealthTier, reason string) *tierChange {
	from := ht.tier
	ht.tier = to
	ht.reason = reason
	ht.since = time.Now()

	if ht.cfg.SubsystemName != "" {
		healthTierGauge.WithLabelValues(ht.cfg.SubsystemName).Set(float64(to))
		healthTransitionsTotal.WithLabelValues(ht.cfg.SubsystemName, from.String(), to.String()).Inc()
	}

	return &tierChange{from: from, to: to}
}

// notifyTierChange invokes the OnTierChange callback outside the lock.
func (ht *HealthTracker) notifyTierChange(change *tierChange) {
	if ht.cfg.OnTierChange != nil {
		ht.cfg.OnTierChange(change.from, change.to)
	}
}
