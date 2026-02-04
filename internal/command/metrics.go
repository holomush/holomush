// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Status constants for command execution metrics.
const (
	StatusSuccess          = "success"
	StatusError            = "error"
	StatusNotFound         = "not_found"
	StatusPermissionDenied = "permission_denied"
	StatusRateLimited      = "rate_limited"
)

// CommandExecutions is the counter for command executions.
// Use RegisterMetrics to register this with a Prometheus registry.
var CommandExecutions = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "holomush_command_executions_total",
		Help: "Total number of command executions",
	},
	[]string{"command", "source", "status"},
)

// CommandDuration is the histogram for command execution duration.
// Use RegisterMetrics to register this with a Prometheus registry.
var CommandDuration = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "holomush_command_duration_seconds",
		Help:    "Command execution duration in seconds",
		Buckets: prometheus.DefBuckets,
	},
	[]string{"command", "source"},
)

// AliasExpansions is the counter for alias expansions.
// Use RegisterMetrics to register this with a Prometheus registry.
var AliasExpansions = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "holomush_alias_expansions_total",
		Help: "Total number of alias expansions",
	},
	[]string{"alias"},
)

// RegisterMetrics registers command package metrics with the given Prometheus registry.
// This must be called at startup to make metrics available on /metrics.
// Panics if registration fails (following prometheus convention).
func RegisterMetrics(reg prometheus.Registerer) {
	reg.MustRegister(CommandExecutions)
	reg.MustRegister(CommandDuration)
	reg.MustRegister(AliasExpansions)
}

// RecordCommandExecution increments the command execution counter with the given attributes.
// Parameters:
//   - command: the command name that was executed
//   - source: where the command is defined (e.g., "core", "lua")
//   - status: execution result (use Status* constants)
func RecordCommandExecution(command, source, status string) {
	CommandExecutions.WithLabelValues(command, source, status).Inc()
}

// RecordCommandDuration records the duration of a command execution.
// Parameters:
//   - command: the command name that was executed
//   - source: where the command is defined (e.g., "core", "lua")
//   - duration: how long the command took to execute
func RecordCommandDuration(command, source string, duration time.Duration) {
	CommandDuration.WithLabelValues(command, source).Observe(duration.Seconds())
}

// RecordAliasExpansion increments the alias expansion counter.
// Parameters:
//   - alias: the alias that was expanded
func RecordAliasExpansion(alias string) {
	AliasExpansions.WithLabelValues(alias).Inc()
}
