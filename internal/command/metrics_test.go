// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestRecordCommandDuration(t *testing.T) {
	// Record durations - use unique command names for this test.
	// Use unique names to avoid interference from other test runs.
	RecordCommandDuration("duration_look_t2", "core", 50*time.Millisecond)
	RecordCommandDuration("duration_say_t2", "lua", 100*time.Millisecond)
	RecordCommandDuration("duration_look_t2", "core", 75*time.Millisecond) // Second observation

	// Collect the histogram directly and inspect sample counts.
	// testutil.ToFloat64 panics on multi-value collectors; instead we
	// collect through the Collector interface and read the dto.Metric.
	collectSampleCount := func(labels prometheus.Labels) uint64 {
		t.Helper()
		obs := CommandDuration.With(labels)
		mCh := make(chan prometheus.Metric, 1)
		obs.(prometheus.Collector).Collect(mCh)
		close(mCh)
		m := <-mCh
		if m == nil {
			return 0
		}
		var pb dto.Metric
		require.NoError(t, m.Write(&pb))
		return pb.GetHistogram().GetSampleCount()
	}

	lookCount := collectSampleCount(prometheus.Labels{"command": "duration_look_t2", "source": "core"})
	sayCount := collectSampleCount(prometheus.Labels{"command": "duration_say_t2", "source": "lua"})

	assert.GreaterOrEqual(t, lookCount, uint64(2), "expected at least 2 observations for duration_look_t2/core")
	assert.GreaterOrEqual(t, sayCount, uint64(1), "expected at least 1 observation for duration_say_t2/lua")
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

func TestRecordAliasRollbackFailure(t *testing.T) {
	// Get current value
	before := testutil.ToFloat64(AliasRollbackFailures)

	// Record rollback failures
	RecordAliasRollbackFailure()
	RecordAliasRollbackFailure()

	// Verify counter incremented
	after := testutil.ToFloat64(AliasRollbackFailures)
	assert.Equal(t, before+2, after, "expected counter to increment by 2")
}

func TestMetricsStatusConstants(t *testing.T) {
	// Verify status constants are defined as expected
	assert.Equal(t, "success", StatusSuccess)
	assert.Equal(t, "error", StatusError)
	assert.Equal(t, "not_found", StatusNotFound)
	assert.Equal(t, "permission_denied", StatusPermissionDenied)
	assert.Equal(t, "rate_limited", StatusRateLimited)
	assert.Equal(t, "engine_failure", StatusEngineFailure)
}
