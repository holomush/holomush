// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lifecycle

import "time"

//go:generate stringer -type=HealthTier -linecomment

// HealthTier represents operational health levels.
// Tiers are ordered by severity — higher values are worse.
type HealthTier int

// HealthTier constants represent the ordered severity levels for subsystem health.
const (
	HealthWarm     HealthTier = iota // warm
	HealthDegraded                   // degraded
	HealthStale                      // stale
	HealthDead                       // dead
)

// IsReady returns true if the tier is servable (Warm or Degraded).
func (t HealthTier) IsReady() bool {
	return t <= HealthDegraded
}

// HealthStatus is the health report from a subsystem.
type HealthStatus struct {
	Tier   HealthTier
	Reason string
	Since  time.Time
}

// HealthReporter is implemented by subsystems with ongoing runtime state.
// Not all Subsystems need this — only those with connections, caches, or
// background loops that can degrade at runtime.
type HealthReporter interface {
	HealthStatus() HealthStatus
}
