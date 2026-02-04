// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Status constants for command execution metrics.
const (
	StatusSuccess          = "success"
	StatusError            = "error"
	StatusNotFound         = "not_found"
	StatusPermissionDenied = "permission_denied"
	StatusRateLimited      = "rate_limited"
)

// Package-level metric instruments, initialized lazily via InitMetrics or on first use.
var (
	commandExecutions metric.Int64Counter
	commandDuration   metric.Float64Histogram
	aliasExpansions   metric.Int64Counter
)

// InitMetrics initializes the command metrics using the provided meter provider.
// This should be called at startup with the configured meter provider.
// If not called, metrics will be recorded to the global NoOp meter.
func InitMetrics(provider metric.MeterProvider) {
	meter := provider.Meter("holomush/command")
	initMetricsWithMeter(meter)
}

// initMetricsWithMeter initializes metrics with a specific meter instance.
// Any errors during metric creation are logged but not fatal - the global
// meter will provide NoOp implementations that safely do nothing.
func initMetricsWithMeter(meter metric.Meter) {
	// Note: OTel meter methods only return errors for invalid names/configurations.
	// With valid constant names and options, errors are extremely unlikely.
	// We ignore errors here since the returned instruments are safe to use
	// even when an error occurs (they become NoOp implementations).
	commandExecutions, _ = meter.Int64Counter( //nolint:errcheck // NoOp fallback is safe
		"holomush.command.executions",
		metric.WithDescription("Number of command executions"),
		metric.WithUnit("{execution}"),
	)

	commandDuration, _ = meter.Float64Histogram( //nolint:errcheck // NoOp fallback is safe
		"holomush.command.duration",
		metric.WithDescription("Command execution duration"),
		metric.WithUnit("s"),
	)

	aliasExpansions, _ = meter.Int64Counter( //nolint:errcheck // NoOp fallback is safe
		"holomush.alias.expansions",
		metric.WithDescription("Number of alias expansions"),
		metric.WithUnit("{expansion}"),
	)
}

// ensureMetricsInitialized initializes metrics using the global meter if not already done.
func ensureMetricsInitialized() {
	if commandExecutions == nil {
		initMetricsWithMeter(otel.Meter("holomush/command"))
	}
}

// RecordCommandExecution increments the command execution counter with the given attributes.
// Parameters:
//   - command: the command name that was executed
//   - source: where the command is defined (e.g., "core", "lua")
//   - status: execution result (use Status* constants)
func RecordCommandExecution(command, source, status string) {
	ensureMetricsInitialized()
	commandExecutions.Add(context.Background(), 1,
		metric.WithAttributes(
			attribute.String("command", command),
			attribute.String("source", source),
			attribute.String("status", status),
		),
	)
}

// RecordCommandDuration records the duration of a command execution.
// Parameters:
//   - command: the command name that was executed
//   - source: where the command is defined (e.g., "core", "lua")
//   - duration: how long the command took to execute
func RecordCommandDuration(command, source string, duration time.Duration) {
	ensureMetricsInitialized()
	commandDuration.Record(context.Background(), duration.Seconds(),
		metric.WithAttributes(
			attribute.String("command", command),
			attribute.String("source", source),
		),
	)
}

// RecordAliasExpansion increments the alias expansion counter.
// Parameters:
//   - alias: the alias that was expanded
func RecordAliasExpansion(alias string) {
	ensureMetricsInitialized()
	aliasExpansions.Add(context.Background(), 1,
		metric.WithAttributes(
			attribute.String("alias", alias),
		),
	)
}
