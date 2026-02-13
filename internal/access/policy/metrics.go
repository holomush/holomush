// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"time"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics for ABAC policy evaluation.
var (
	// evaluateDuration tracks the latency of Evaluate() calls.
	evaluateDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "abac_evaluate_duration_seconds",
		Help:    "Histogram of ABAC policy evaluation latency in seconds",
		Buckets: prometheus.DefBuckets,
	})

	// policyEvaluations counts evaluations by source and effect.
	policyEvaluations = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "abac_policy_evaluations_total",
		Help: "Total number of ABAC policy evaluations",
	}, []string{"source", "effect"})

	// degradedModeGauge indicates if the engine is in degraded mode.
	// Not yet used - will be wired in future tasks when degraded mode is implemented.
	degradedModeGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "abac_degraded_mode",
		Help: "ABAC degraded mode status (0=normal, 1=degraded)",
	})

	// providerErrorsCounter counts errors from attribute providers.
	// Not yet used - will be wired when attribute provider error tracking is added.
	providerErrorsCounter = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "abac_provider_errors_total",
		Help: "Total number of attribute provider errors",
	}, []string{"namespace", "error_type"})

	// unregisteredAttributesCounter counts accesses to unregistered attributes.
	// Not yet used - will be wired when attribute validation tracking is added.
	unregisteredAttributesCounter = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "abac_unregistered_attributes_total",
		Help: "Total number of accesses to unregistered attributes",
	}, []string{"namespace", "key"})

	// circuitBreakerTripsCounter counts circuit breaker trips per provider.
	// Not yet used - will be wired when circuit breaker is implemented.
	circuitBreakerTripsCounter = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "abac_provider_circuit_breaker_trips_total",
		Help: "Total number of circuit breaker trips for attribute providers",
	}, []string{"provider"})
)

// RecordEvaluationMetrics records metrics for a completed evaluation.
// This should be called after each Evaluate() call with the duration and effect.
func RecordEvaluationMetrics(duration time.Duration, effect types.Effect) {
	// Record histogram
	evaluateDuration.Observe(duration.Seconds())

	// Record counter with source=unknown until adapter layer adds policy source tracking
	policyEvaluations.WithLabelValues("unknown", effect.String()).Inc()
}

func init() {
	// Force evaluation of unused metrics to ensure they are registered with Prometheus.
	// These metrics are defined for future use but not yet wired into the code.
	_ = degradedModeGauge
	_ = providerErrorsCounter
	_ = unregisteredAttributesCounter
	_ = circuitBreakerTripsCounter
}
