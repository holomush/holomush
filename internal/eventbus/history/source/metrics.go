// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package source

import "github.com/prometheus/client_golang/prometheus"

// Metrics holds the three INV-CRYPTO-22 fallback counters per spec §5.6.
type Metrics struct {
	HotDEKMiss          prometheus.Counter
	ColdFallbackSuccess prometheus.Counter
	ColdDEKMiss         prometheus.Counter
}

// NewMetrics registers and returns the three source-resolver counters.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		HotDEKMiss:          prometheus.NewCounter(prometheus.CounterOpts{Name: "crypto_hot_dek_miss_total"}),
		ColdFallbackSuccess: prometheus.NewCounter(prometheus.CounterOpts{Name: "crypto_cold_fallback_success_total"}),
		ColdDEKMiss:         prometheus.NewCounter(prometheus.CounterOpts{Name: "crypto_cold_dek_miss_total"}),
	}
	reg.MustRegister(m.HotDEKMiss, m.ColdFallbackSuccess, m.ColdDEKMiss)
	return m
}

// NewMetricsForTest avoids prometheus.DefaultRegisterer collisions in unit tests.
func NewMetricsForTest(reg *prometheus.Registry) *Metrics {
	return NewMetrics(reg)
}
