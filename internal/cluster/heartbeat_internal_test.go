// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newRegistryForRecordSkew constructs a minimal *registry sufficient for
// exercising recordSkew. We do NOT call Start() so subscriptions /
// goroutines do not run; recordSkew only reads cfg.SkewWarnThreshold,
// deps.Logger, deps.SkewMetrics, and r.self.
func newRegistryForRecordSkew(t *testing.T, threshold time.Duration, metrics *SkewMetrics) (*registry, *bytes.Buffer) {
	t.Helper()
	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cfg := Config{
		ClusterID:         "test-game",
		HolomushVersion:   "test",
		SkewWarnThreshold: threshold,
	}.Defaults()
	r := &registry{
		cfg: cfg,
		deps: Deps{
			Logger:      logger,
			SkewMetrics: metrics,
		},
		self: MemberID("01HSELFAAAAAAAAAAAAAAAAAA"),
	}
	return r, logBuf
}

// TestRecordSkewWarnBehaviorByThreshold collapses the above-/below-threshold
// branches into one table. Each case runs with Metrics=nil to cover the
// round-4 fix invariant: the warn-log path MUST fire regardless of whether
// the Prometheus instrument is wired.
func TestRecordSkewWarnBehaviorByThreshold(t *testing.T) {
	const peer = "01HPEERAAAAAAAAAAAAAAAAA"
	cases := []struct {
		name       string
		skew       float64
		shouldWarn bool
	}{
		{name: "warns when above threshold (round-4 fix; metrics nil)", skew: 5.0, shouldWarn: true},
		{name: "stays silent when below threshold", skew: 0.1, shouldWarn: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, logBuf := newRegistryForRecordSkew(t, 1*time.Second, nil)
			r.recordSkew(MemberID(peer), tc.skew)

			out := logBuf.String()
			if tc.shouldWarn {
				assert.Contains(t, out, "skew exceeds threshold")
				assert.Contains(t, out, peer)
				return
			}
			assert.NotContains(t, out, "skew exceeds threshold")
		})
	}
}

func TestRecordSkewWritesGaugeWhenMetricsPresent(t *testing.T) {
	// Use an isolated Prometheus registerer so this test does not
	// collide with other tests' metrics registrations.
	reg := prometheus.NewRegistry()
	skewMetrics := NewSkewMetrics(reg)
	r, _ := newRegistryForRecordSkew(t, 30*time.Second, skewMetrics)

	const observed = 7.5
	r.recordSkew(MemberID("01HPEERAAAAAAAAAAAAAAAAA"), observed)

	g, err := skewMetrics.SkewSeconds.GetMetricWithLabelValues(string(r.self), "01HPEERAAAAAAAAAAAAAAAAA")
	require.NoError(t, err)
	var m dto.Metric
	require.NoError(t, g.Write(&m))
	assert.Equal(t, observed, m.GetGauge().GetValue())
}
