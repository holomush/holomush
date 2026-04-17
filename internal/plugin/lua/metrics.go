// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// outcome label values used with InvocationsTotal.
const (
	outcomeSuccess      = "success"
	outcomeTimeout      = "timeout"
	outcomeRegistryFull = "registry_full"
	outcomeError        = "error"
)

// InvocationsTotal counts every dispatcher invocation of a Lua handler,
// labelled by plugin, handler name, and outcome. Serves as the denominator
// for outcome-rate dashboards.
var InvocationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "holomush_plugin_lua_invocations_total",
	Help: "Total Lua plugin invocations by plugin, handler, and outcome",
}, []string{"plugin", "handler", "outcome"})

// TimeoutsTotal counts invocations that hit the per-invocation CPU
// deadline. Rises under adversarial or buggy plugins.
var TimeoutsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "holomush_plugin_lua_timeouts_total",
	Help: "Total Lua plugin invocations disconnected for exceeding the CPU deadline",
}, []string{"plugin", "handler"})

// RegistryFullTotal counts invocations killed by Lua registry overflow
// (RegistryMaxSize). Rises under memory-bomb plugins.
var RegistryFullTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "holomush_plugin_lua_registry_full_total",
	Help: "Total Lua plugin invocations killed by registry overflow (memory cap)",
}, []string{"plugin", "handler"})

// recordInvocationOutcome increments the invocations counter with the
// given outcome label, and increments the corresponding specific counter
// (timeouts_total or registry_full_total) when the outcome indicates one
// of the resource-cap paths.
func recordInvocationOutcome(plugin, handler, outcome string) {
	InvocationsTotal.WithLabelValues(plugin, handler, outcome).Inc()
	switch outcome {
	case outcomeTimeout:
		TimeoutsTotal.WithLabelValues(plugin, handler).Inc()
	case outcomeRegistryFull:
		RegistryFullTotal.WithLabelValues(plugin, handler).Inc()
	}
}
