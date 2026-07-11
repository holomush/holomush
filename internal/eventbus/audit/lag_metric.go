// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

// LagSeconds tracks how far behind the audit projection is from the head
// of the EVENTS stream. Label projection distinguishes multiple
// projections if more are added later.
var LagSeconds = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Namespace: "holomush",
		Subsystem: "audit",
		Name:      "projection_lag_seconds",
		Help:      "Seconds between latest published seq and audit consumer's last-acked seq.",
	},
	[]string{"projection"},
)

// SkippedPluginOwnedTotal counts messages the host projection ack-and-skipped
// because their subject resolved to a plugin owner. Operators can use this
// to observe the host/plugin split at projection time and to alert when
// a plugin-owned consumer falls behind (plugin-skipped on host but not
// persisted downstream ⇒ investigate per-plugin consumer health).
var SkippedPluginOwnedTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "holomush",
		Subsystem: "audit",
		Name:      "projection_plugin_owned_skipped_total",
		Help:      "Messages the host audit projection acked-and-skipped because the subject is owned by a plugin.",
	},
	[]string{"plugin"},
)

// DLQMessagesTotal counts audit messages captured to the dead-letter
// stream (EVENTS_AUDIT_DLQ) after exhausting MaxDeliver. It increments
// exactly once per message the projection successfully publishes to the
// DLQ (D-11). Operators alert on this counter long before bounded DLQ
// retention (D-12) would age anything out — a rising count means poison
// messages (typically a Postgres outage) are accumulating dead letters.
var DLQMessagesTotal = prometheus.NewCounter(
	prometheus.CounterOpts{
		Namespace: "holomush",
		Subsystem: "audit",
		Name:      "dlq_messages_total",
		Help:      "Audit messages captured to the dead-letter stream after exhausting MaxDeliver.",
	},
)

// RegisterMetrics registers audit Prometheus collectors with reg.
// Duplicate registrations are silently ignored; other registration
// errors panic. Matches the pattern used by internal/lifecycle/metrics.go.
func RegisterMetrics(reg prometheus.Registerer) {
	for _, c := range []prometheus.Collector{LagSeconds, SkippedPluginOwnedTotal, DLQMessagesTotal} {
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
