// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lifecycle

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	healthTierGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "lifecycle_health_tier",
		Help: "Current health tier per subsystem (0=warm, 1=degraded, 2=stale, 3=dead)",
	}, []string{"subsystem"})

	healthTransitionsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "lifecycle_health_transitions_total",
		Help: "Total health tier transitions per subsystem",
	}, []string{"subsystem", "from", "to"})

	startupDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "lifecycle_startup_duration_seconds",
		Help:    "Time from process start to AllReady()",
		Buckets: prometheus.DefBuckets,
	})
)

// RegisterMetrics registers lifecycle metrics with the given Prometheus registry.
// Duplicate registrations are silently ignored (safe for tests and re-init).
func RegisterMetrics(reg prometheus.Registerer) {
	for _, c := range []prometheus.Collector{healthTierGauge, healthTransitionsTotal, startupDuration} {
		if err := reg.Register(c); err != nil {
			var are prometheus.AlreadyRegisteredError
			if errors.As(err, &are) {
				_ = are // already registered — no-op
			} else {
				panic(err)
			}
		}
	}
}
