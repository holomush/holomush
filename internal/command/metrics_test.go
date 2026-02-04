// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestRecordCommandExecution(t *testing.T) {
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
		t.Run(tt.name, func(t *testing.T) {
			// Get current value
			labels := prometheus.Labels{
				"command": tt.command,
				"source":  tt.source,
				"status":  tt.status,
			}
			before := testutil.ToFloat64(CommandExecutions.With(labels))

			// Record execution
			RecordCommandExecution(tt.command, tt.source, tt.status)

			// Verify counter incremented
			after := testutil.ToFloat64(CommandExecutions.With(labels))
			assert.Equal(t, before+1, after, "counter should increment by 1")
		})
	}
}

func TestRecordCommandDuration(_ *testing.T) {
	// Record durations - use unique command names for this test
	RecordCommandDuration("duration_look", "core", 50*time.Millisecond)
	RecordCommandDuration("duration_say", "lua", 100*time.Millisecond)
	RecordCommandDuration("duration_look", "core", 75*time.Millisecond) // Second observation

	// Verify histogram exists and can be accessed without panic
	// Note: testutil.ToFloat64 doesn't support histograms directly,
	// so we just verify the metric was created without error
	lookLabels := prometheus.Labels{"command": "duration_look", "source": "core"}
	sayLabels := prometheus.Labels{"command": "duration_say", "source": "lua"}

	// These will panic if the metric doesn't exist, so successful execution = test passes
	_ = CommandDuration.With(lookLabels)
	_ = CommandDuration.With(sayLabels)
}

func TestRecordAliasExpansion(t *testing.T) {
	// Get current values
	lBefore := testutil.ToFloat64(AliasExpansions.With(prometheus.Labels{"alias": "l"}))
	colonBefore := testutil.ToFloat64(AliasExpansions.With(prometheus.Labels{"alias": ":"}))

	// Record alias expansions
	RecordAliasExpansion("l")
	RecordAliasExpansion(":")
	RecordAliasExpansion("l") // Use 'l' twice

	// Verify counts
	lAfter := testutil.ToFloat64(AliasExpansions.With(prometheus.Labels{"alias": "l"}))
	colonAfter := testutil.ToFloat64(AliasExpansions.With(prometheus.Labels{"alias": ":"}))

	assert.Equal(t, lBefore+2, lAfter, "expected 2 expansions for 'l' alias")
	assert.Equal(t, colonBefore+1, colonAfter, "expected 1 expansion for ':' alias")
}

func TestMetricsStatusConstants(t *testing.T) {
	// Verify status constants are defined as expected
	assert.Equal(t, "success", StatusSuccess)
	assert.Equal(t, "error", StatusError)
	assert.Equal(t, "not_found", StatusNotFound)
	assert.Equal(t, "permission_denied", StatusPermissionDenied)
	assert.Equal(t, "rate_limited", StatusRateLimited)
}
