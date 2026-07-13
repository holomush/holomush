// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package outbox

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// relayPublished counts world-change envelopes the relay PubAcked, per game.
var relayPublished = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "holomush_world_relay_published_total",
		Help: "Total world-change envelopes the outbox relay published (PubAck received)",
	},
	[]string{"game"},
)

// relayHalts counts halt-on-poison events (mirrors observability.RecordEngineFailure
// style). A halt means the ordered feed stopped at an unpublishable position and
// requires an operator skip (holomush outbox skip) to resume — the reason relay
// alerting lands in this slice (a halted ordered feed is otherwise silent).
var relayHalts = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "holomush_world_relay_halts_total",
		Help: "Total times the outbox relay halted on a poison (unpublishable) envelope",
	},
	[]string{"game"},
)

// relayHaltPosition exposes the (epoch*offset-free) feed_position the relay is
// halted at, per game. Zero when the relay is not halted. Operators alert on a
// non-zero value: a stalled ordered feed is visible, not silent.
var relayHaltPosition = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "holomush_world_relay_halt_position",
		Help: "The feed_position the outbox relay is currently halted at (0 = not halted)",
	},
	[]string{"game"},
)

// relaySkipResolved counts operator skip-marker recoveries that PubAcked and
// resolved a poison row, per game.
var relaySkipResolved = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "holomush_world_relay_skips_resolved_total",
		Help: "Total operator skip-marker recoveries that resolved a poison outbox row",
	},
	[]string{"game"},
)
