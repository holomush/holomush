// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lifecycle

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ReadinessRegistry aggregates health from subsystems and provides
// a single readiness signal for startup gating.
type ReadinessRegistry struct {
	mu        sync.RWMutex
	reporters map[SubsystemID]HealthReporter
}

// NewReadinessRegistry creates an empty registry.
func NewReadinessRegistry() *ReadinessRegistry {
	return &ReadinessRegistry{
		reporters: make(map[SubsystemID]HealthReporter),
	}
}

// Register adds a health reporter for a subsystem. Panics if the
// subsystem is already registered (indicates a wiring bug).
func (r *ReadinessRegistry) Register(id SubsystemID, hr HealthReporter) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.reporters[id]; exists {
		panic("lifecycle: duplicate registration for subsystem " + id.String())
	}
	r.reporters[id] = hr
}

// Unregister removes a previously registered health reporter for a
// subsystem. It is a no-op if the subsystem was never registered (or has
// already been unregistered), so it is safe to call unconditionally from a
// subsystem's Stop. This exists so a subsystem whose Prepare calls Register
// can be torn down and legitimately retried without hitting Register's
// duplicate-registration panic on the second Prepare (WR-02).
func (r *ReadinessRegistry) Unregister(id SubsystemID) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.reporters, id)
}

// AllReady returns true when every registered reporter is Warm or Degraded.
// Returns true if no reporters are registered (vacuous truth).
func (r *ReadinessRegistry) AllReady() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, hr := range r.reporters {
		if !hr.HealthStatus().Tier.IsReady() {
			return false
		}
	}
	return true
}

// Status returns per-subsystem health for diagnostics.
func (r *ReadinessRegistry) Status() map[SubsystemID]HealthStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[SubsystemID]HealthStatus, len(r.reporters))
	for id, hr := range r.reporters {
		result[id] = hr.HealthStatus()
	}
	return result
}

// WaitReady blocks until AllReady() returns true or the context is cancelled.
// Polls every 100ms.
func (r *ReadinessRegistry) WaitReady(ctx context.Context) error {
	if r.AllReady() {
		return nil
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait ready: %w", ctx.Err())
		case <-ticker.C:
			if r.AllReady() {
				return nil
			}
		}
	}
}
