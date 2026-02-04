// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// setupTestMeterProvider creates a test meter provider that captures metrics
// in memory for verification. Returns the reader to access recorded metrics.
func setupTestMeterProvider(t *testing.T) *metric.ManualReader {
	t.Helper()
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))

	// Initialize the command metrics with this provider
	InitMetrics(provider)

	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
	})

	return reader
}

// findMetric searches for a metric by name in the collected data.
func findMetric(rm metricdata.ResourceMetrics, name string) *metricdata.Metrics {
	for _, sm := range rm.ScopeMetrics {
		for i := range sm.Metrics {
			if sm.Metrics[i].Name == name {
				return &sm.Metrics[i]
			}
		}
	}
	return nil
}

// getCounterValue extracts the counter value for given attributes.
func getCounterValue(m *metricdata.Metrics, attrs ...attribute.KeyValue) int64 {
	if m == nil {
		return 0
	}
	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		return 0
	}
	attrSet := attribute.NewSet(attrs...)
	for _, dp := range sum.DataPoints {
		if dp.Attributes.Equals(&attrSet) {
			return dp.Value
		}
	}
	return 0
}

// getHistogramCount extracts the count of observations for given attributes.
func getHistogramCount(m *metricdata.Metrics, attrs ...attribute.KeyValue) uint64 {
	if m == nil {
		return 0
	}
	hist, ok := m.Data.(metricdata.Histogram[float64])
	if !ok {
		return 0
	}
	attrSet := attribute.NewSet(attrs...)
	for _, dp := range hist.DataPoints {
		if dp.Attributes.Equals(&attrSet) {
			return dp.Count
		}
	}
	return 0
}

func TestRecordCommandExecution(t *testing.T) {
	reader := setupTestMeterProvider(t)

	tests := []struct {
		name    string
		command string
		source  string
		status  string
	}{
		{
			name:    "successful execution",
			command: "look",
			source:  "core",
			status:  StatusSuccess,
		},
		{
			name:    "error execution",
			command: "say",
			source:  "lua",
			status:  StatusError,
		},
		{
			name:    "not found",
			command: "unknown",
			source:  "",
			status:  StatusNotFound,
		},
		{
			name:    "permission denied",
			command: "admin",
			source:  "core",
			status:  StatusPermissionDenied,
		},
		{
			name:    "rate limited",
			command: "spam",
			source:  "core",
			status:  StatusRateLimited,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(_ *testing.T) {
			RecordCommandExecution(tt.command, tt.source, tt.status)
		})
	}

	// Collect metrics
	var rm metricdata.ResourceMetrics
	err := reader.Collect(context.Background(), &rm)
	require.NoError(t, err)

	// Verify command executions counter
	executions := findMetric(rm, "holomush.command.executions")
	require.NotNil(t, executions, "holomush.command.executions metric not found")

	// Check each recorded execution
	for _, tt := range tests {
		t.Run(tt.name+" counter", func(t *testing.T) {
			value := getCounterValue(executions,
				attribute.String("command", tt.command),
				attribute.String("source", tt.source),
				attribute.String("status", tt.status),
			)
			assert.Equal(t, int64(1), value, "expected 1 execution for %s", tt.command)
		})
	}
}

func TestRecordCommandDuration(t *testing.T) {
	reader := setupTestMeterProvider(t)

	// Record durations
	RecordCommandDuration("look", "core", 50*time.Millisecond)
	RecordCommandDuration("say", "lua", 100*time.Millisecond)
	RecordCommandDuration("look", "core", 75*time.Millisecond) // Second observation

	// Collect metrics
	var rm metricdata.ResourceMetrics
	err := reader.Collect(context.Background(), &rm)
	require.NoError(t, err)

	// Verify command duration histogram
	duration := findMetric(rm, "holomush.command.duration")
	require.NotNil(t, duration, "holomush.command.duration metric not found")

	// Check observations count
	lookCount := getHistogramCount(duration,
		attribute.String("command", "look"),
		attribute.String("source", "core"),
	)
	assert.Equal(t, uint64(2), lookCount, "expected 2 observations for look")

	sayCount := getHistogramCount(duration,
		attribute.String("command", "say"),
		attribute.String("source", "lua"),
	)
	assert.Equal(t, uint64(1), sayCount, "expected 1 observation for say")
}

func TestRecordAliasExpansion(t *testing.T) {
	reader := setupTestMeterProvider(t)

	// Record alias expansions
	RecordAliasExpansion("l")
	RecordAliasExpansion(":")
	RecordAliasExpansion("l") // Use 'l' twice

	// Collect metrics
	var rm metricdata.ResourceMetrics
	err := reader.Collect(context.Background(), &rm)
	require.NoError(t, err)

	// Verify alias expansions counter
	expansions := findMetric(rm, "holomush.alias.expansions")
	require.NotNil(t, expansions, "holomush.alias.expansions metric not found")

	// Check counts
	lValue := getCounterValue(expansions,
		attribute.String("alias", "l"),
	)
	assert.Equal(t, int64(2), lValue, "expected 2 expansions for 'l' alias")

	colonValue := getCounterValue(expansions,
		attribute.String("alias", ":"),
	)
	assert.Equal(t, int64(1), colonValue, "expected 1 expansion for ':' alias")
}

func TestMetricsStatusConstants(t *testing.T) {
	// Verify status constants are defined as expected
	assert.Equal(t, "success", StatusSuccess)
	assert.Equal(t, "error", StatusError)
	assert.Equal(t, "not_found", StatusNotFound)
	assert.Equal(t, "permission_denied", StatusPermissionDenied)
	assert.Equal(t, "rate_limited", StatusRateLimited)
}

// hasCounterWithStatus checks if any counter datapoint has the given status attribute.
func hasCounterWithStatus(m *metricdata.Metrics, status string) bool {
	if m == nil {
		return false
	}
	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		return false
	}
	for _, dp := range sum.DataPoints {
		statusAttr, found := dp.Attributes.Value(attribute.Key("status"))
		if found && statusAttr.AsString() == status {
			return true
		}
	}
	return false
}

// hasHistogramWithCommand checks if any histogram datapoint has the given command attribute.
func hasHistogramWithCommand(m *metricdata.Metrics, command string) bool {
	if m == nil {
		return false
	}
	hist, ok := m.Data.(metricdata.Histogram[float64])
	if !ok {
		return false
	}
	for _, dp := range hist.DataPoints {
		cmdAttr, found := dp.Attributes.Value(attribute.Key("command"))
		if found && cmdAttr.AsString() == command {
			return true
		}
	}
	return false
}
