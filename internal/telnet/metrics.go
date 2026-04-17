// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ConnectionsActive tracks the current number of live telnet handler
// goroutines. Rises toward MaxConns under load; a gauge pinned at the cap
// is the primary DoS signal for operators.
var ConnectionsActive = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "holomush_telnet_connections_active",
	Help: "Current number of active telnet connections",
})

// ConnectionsRefusedTotal counts accepts that were immediately closed
// because the global connection cap was full.
var ConnectionsRefusedTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "holomush_telnet_connections_refused_total",
	Help: "Total telnet connections refused due to MaxConns cap",
})

// PreAuthTimeoutsTotal counts connections disconnected because the
// pre-auth timer fired before successful character selection.
var PreAuthTimeoutsTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "holomush_telnet_preauth_timeouts_total",
	Help: "Total telnet connections disconnected for exceeding the pre-auth timeout",
})

// IdleTimeoutsTotal counts connections disconnected because the read
// deadline expired (Slowloris / idle client).
var IdleTimeoutsTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "holomush_telnet_idle_timeouts_total",
	Help: "Total telnet connections disconnected due to idle read timeout",
})

// IncConnectionsActive increments the active-connection gauge.
func IncConnectionsActive() { ConnectionsActive.Inc() }

// DecConnectionsActive decrements the active-connection gauge.
func DecConnectionsActive() { ConnectionsActive.Dec() }

// RecordConnectionRefused increments the refused counter.
func RecordConnectionRefused() { ConnectionsRefusedTotal.Inc() }

// RecordPreAuthTimeout increments the pre-auth timeout counter.
func RecordPreAuthTimeout() { PreAuthTimeoutsTotal.Inc() }

// RecordIdleTimeout increments the idle timeout counter.
func RecordIdleTimeout() { IdleTimeoutsTotal.Inc() }
