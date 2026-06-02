// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster

import "github.com/prometheus/client_golang/prometheus"

// PillMetrics holds the Prometheus counters that Pill implementations
// emit on Trigger. Constructed once and shared across the production
// Pill.
type PillMetrics struct {
	PoisonedTotal *prometheus.CounterVec
}

// NewPillMetrics constructs PillMetrics and registers the counter
// with the supplied registerer.
func NewPillMetrics(reg prometheus.Registerer) *PillMetrics {
	m := &PillMetrics{
		PoisonedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "replica_poisoned_total",
			Help: "Pills received and acted upon, labelled by self member_id, reason, and source coordinator id.",
		}, []string{"member_id", "reason", "source_id"}),
	}
	reg.MustRegister(m.PoisonedTotal)
	return m
}

// SkewMetrics holds the gauge for cross-host clock skew detection
// (Decision 8). Skew is observability-only; no protocol decision
// reads from this metric.
type SkewMetrics struct {
	SkewSeconds *prometheus.GaugeVec
}

// NewSkewMetrics constructs SkewMetrics and registers the gauge.
func NewSkewMetrics(reg prometheus.Registerer) *SkewMetrics {
	m := &SkewMetrics{
		SkewSeconds: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "cluster_member_skew_seconds",
			Help: "Wall-clock skew between this member and the named source, in seconds. Observability only; no protocol behavior depends on this.",
		}, []string{"member_id", "source_id"}),
	}
	reg.MustRegister(m.SkewSeconds)
	return m
}

// SelfTimeoutMetrics tracks INVALIDATION_SELF_TIMEOUT occurrences
// (single-replica deployment with hung local handler).
type SelfTimeoutMetrics struct {
	SelfTimeoutTotal prometheus.Counter
}

// NewSelfTimeoutMetrics constructs SelfTimeoutMetrics and registers
// the counter.
func NewSelfTimeoutMetrics(reg prometheus.Registerer) *SelfTimeoutMetrics {
	m := &SelfTimeoutMetrics{
		SelfTimeoutTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "cluster_self_timeout_total",
			Help: "Coordinator missed-ack set after probe-and-pill phase contains only Self() (N=1 single-replica with hung local handler).",
		}),
	}
	reg.MustRegister(m.SelfTimeoutTotal)
	return m
}

// HeartbeatMetrics tracks heartbeat-publish-path failures. Increments
// when r.deps.Conn.Publish on the alive subject returns a non-nil
// error from the heartbeat ticker goroutine; first-publish failures
// during Start surface as a returned error and are not counted here.
type HeartbeatMetrics struct {
	HeartbeatPublishFailedTotal *prometheus.CounterVec
}

// NewHeartbeatMetrics constructs HeartbeatMetrics and registers the
// counter. Label is member_id (the publishing self).
func NewHeartbeatMetrics(reg prometheus.Registerer) *HeartbeatMetrics {
	m := &HeartbeatMetrics{
		HeartbeatPublishFailedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cluster_heartbeat_publish_failed_total",
			Help: "Heartbeat ticker observed a NATS publish failure on the alive subject; ticker continues but visibility into this peer is degraded for downstream subscribers until next successful publish.",
		}, []string{"member_id"}),
	}
	reg.MustRegister(m.HeartbeatPublishFailedTotal)
	return m
}

// DuplicateMemberIDMetrics tracks INV-CLUSTER-3 enforcement events: heartbeat
// receive observed a colliding MemberID with a different StartedAt
// (indicating a different process re-using the ULID — birthday-bound
// astronomical, defense-in-depth detection).
type DuplicateMemberIDMetrics struct {
	DuplicateMemberIDTotal *prometheus.CounterVec
}

// NewDuplicateMemberIDMetrics constructs DuplicateMemberIDMetrics and
// registers the counter.
func NewDuplicateMemberIDMetrics(reg prometheus.Registerer) *DuplicateMemberIDMetrics {
	m := &DuplicateMemberIDMetrics{
		DuplicateMemberIDTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cluster_duplicate_member_id_total",
			Help: "INV-CLUSTER-3 enforcement: heartbeat receive observed a colliding MemberID with a mismatched StartedAt; the duplicate heartbeat was rejected.",
		}, []string{"member_id"}),
	}
	reg.MustRegister(m.DuplicateMemberIDTotal)
	return m
}
