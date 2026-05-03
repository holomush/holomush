// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package invalidation

import "github.com/prometheus/client_golang/prometheus"

// Metrics holds the Prometheus instruments for the invalidation
// protocol. Constructed once via NewMetrics; passed via Deps.
type Metrics struct {
	AcksTotal         *prometheus.CounterVec
	LatencySeconds    *prometheus.HistogramVec
	CrossClusterDrops prometheus.Counter
	UnknownActions    prometheus.Counter
	DEKCacheHits      *prometheus.CounterVec
	DEKCacheMisses    *prometheus.CounterVec
	DEKCacheSize      *prometheus.GaugeVec
	DEKCacheEvictions *prometheus.CounterVec
}

// NewMetrics constructs Metrics and registers all instruments with reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		AcksTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cluster_invalidation_acks_total",
			Help: "Cache invalidation outcomes by action and result.",
		}, []string{"action", "outcome"}),
		LatencySeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "cluster_invalidation_latency_seconds",
			Help:    "Time from invalidation publish to N-of-N ack collection.",
			Buckets: []float64{0.005, 0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10, 30},
		}, []string{"action"}),
		CrossClusterDrops: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "cluster_invalidation_cross_cluster_drops_total",
			Help: "Invalidation messages dropped due to cluster_id mismatch.",
		}),
		UnknownActions: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "cluster_invalidation_unknown_actions_total",
			Help: "Invalidation messages with unrecognized action enum.",
		}),
		DEKCacheHits: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dek_cache_hits_total",
			Help: "DEK cache hits by cache name (material, participants).",
		}, []string{"cache"}),
		DEKCacheMisses: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dek_cache_misses_total",
			Help: "DEK cache misses by cache name.",
		}, []string{"cache"}),
		DEKCacheSize: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "dek_cache_size",
			Help: "DEK cache size by cache name.",
		}, []string{"cache"}),
		DEKCacheEvictions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dek_cache_evictions_total",
			Help: "DEK cache evictions by cache and reason.",
		}, []string{"cache", "reason"}),
	}
	reg.MustRegister(
		m.AcksTotal, m.LatencySeconds, m.CrossClusterDrops, m.UnknownActions,
		m.DEKCacheHits, m.DEKCacheMisses, m.DEKCacheSize, m.DEKCacheEvictions,
	)
	return m
}
