// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/audit"
)

func TestRecordPluginAuditFailureIncrementsCounter(t *testing.T) {
	// The counter is process-global (registered via promauto in
	// plugin_metrics.go), so we read it from the default registry.
	// Capture the baseline first since other tests in the same process
	// may have bumped it.
	const metricName = "abac_audit_plugin_failures_total"

	before, err := readCounterValue(metricName)
	require.NoError(t, err, "expected to read counter %q from default registry", metricName)

	audit.RecordPluginAuditFailure()

	after, err := readCounterValue(metricName)
	require.NoError(t, err)
	assert.InDelta(t, before+1, after, 0.0001,
		"RecordPluginAuditFailure must increment %s by exactly 1", metricName)
}

// readCounterValue gathers the named counter from the default Prometheus
// registry and returns its current value. Returns 0 if the metric exists
// but has never been incremented; returns an error only if Gather fails.
func readCounterValue(name string) (float64, error) {
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return 0, err
	}
	for _, fam := range families {
		if fam.GetName() != name {
			continue
		}
		var sum float64
		for _, m := range fam.GetMetric() {
			sum += m.GetCounter().GetValue()
		}
		return sum, nil
	}
	// Metric not yet collected (counter at zero, never incremented in this
	// process). Treat as zero — the increment under test will create it.
	return 0, nil
}
