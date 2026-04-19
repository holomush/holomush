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

// RegisterMetrics registers audit Prometheus collectors with reg.
// Duplicate registrations are silently ignored; other registration
// errors panic. Matches the pattern used by internal/lifecycle/metrics.go.
func RegisterMetrics(reg prometheus.Registerer) {
	for _, c := range []prometheus.Collector{LagSeconds} {
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
