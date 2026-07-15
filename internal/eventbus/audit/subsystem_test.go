// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/pkg/errutil"
)

// stubJS/stubPool satisfy the Subsystem's provider interfaces for
// unit tests that never actually Start the subsystem. They return nil,
// which exercises the "dependency not started" guard in Subsystem.Start.
type stubJS struct{}

func (stubJS) JS() jetstream.JetStream { return nil }

type stubPool struct{}

func (stubPool) Pool() *pgxpool.Pool { return nil }

func TestSubsystemIDReturnsAuditProjection(t *testing.T) {
	t.Parallel()
	s := audit.NewSubsystem(stubJS{}, stubPool{}, audit.Config{})
	assert.Equal(t, lifecycle.SubsystemAuditProjection, s.ID())
}

func TestSubsystemDependsOnDatabaseAndEventBus(t *testing.T) {
	t.Parallel()
	s := audit.NewSubsystem(stubJS{}, stubPool{}, audit.Config{})
	deps := s.DependsOn()
	// F5 added a Plugins dependency so the per-plugin audit consumers
	// can resolve their PluginAuditService clients via the plugin
	// manager. Existing (Database, EventBus) deps are preserved.
	require.Len(t, deps, 3)
	assert.Contains(t, deps, lifecycle.SubsystemDatabase)
	assert.Contains(t, deps, lifecycle.SubsystemEventBus)
	assert.Contains(t, deps, lifecycle.SubsystemPlugins)
}

func TestConfigDefaultsFillZeroFields(t *testing.T) {
	t.Parallel()
	c := audit.Config{}.Defaults()
	assert.Equal(t, audit.DefaultConsumerName, c.ConsumerName)
	assert.Equal(t, audit.DefaultBatchSize, c.BatchSize)
	assert.Equal(t, audit.DefaultAckWait, c.AckWait)
	assert.Equal(t, audit.DefaultMaxAckPending, c.MaxAckPending)
}

func TestConfigDefaultsPreservesSetFields(t *testing.T) {
	t.Parallel()
	c := audit.Config{
		ConsumerName:  "custom",
		BatchSize:     128,
		AckWait:       10 * time.Second,
		MaxAckPending: 256,
		MaxDeliver:    42,
	}.Defaults()
	assert.Equal(t, "custom", c.ConsumerName)
	assert.Equal(t, 128, c.BatchSize)
	assert.Equal(t, 10*time.Second, c.AckWait)
	assert.Equal(t, 256, c.MaxAckPending)
	assert.Equal(t, 42, c.MaxDeliver)
}

func TestConfigDefaultsFillsMaxDeliver(t *testing.T) {
	t.Parallel()
	c := audit.Config{}.Defaults()
	assert.Equal(t, audit.DefaultMaxDeliver, c.MaxDeliver)
}

func TestConfigDefaultsFillsRetentionFields(t *testing.T) {
	t.Parallel()
	c := audit.Config{}.Defaults()
	assert.Equal(t, audit.DefaultRetainWindow, c.RetainWindow)
	assert.Equal(t, audit.DefaultPurgeInterval, c.PurgeInterval)
}

func TestConfigDefaultsPreservesSetRetention(t *testing.T) {
	t.Parallel()
	c := audit.Config{
		RetainWindow:  30 * 24 * time.Hour,
		PurgeInterval: 6 * time.Hour,
	}.Defaults()
	assert.Equal(t, 30*24*time.Hour, c.RetainWindow, "operator retain_window survives Defaults")
	assert.Equal(t, 6*time.Hour, c.PurgeInterval, "operator purge_interval survives Defaults")
}

func TestConfigValidateAcceptsDefaults(t *testing.T) {
	t.Parallel()
	require.NoError(t, audit.Config{}.Defaults().Validate())
}

func TestConfigValidateRejectsNonPositiveRetainWindow(t *testing.T) {
	t.Parallel()
	// A negative window would make the detach cutoff future-facing and detach
	// EVERY partition (retention.go:83) — must be rejected.
	err := audit.Config{RetainWindow: -1 * time.Hour, PurgeInterval: 24 * time.Hour}.Validate()
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_CONFIG_INVALID")

	require.Error(t, audit.Config{RetainWindow: 0, PurgeInterval: 24 * time.Hour}.Validate(),
		"a zero retain_window is also rejected")
}

func TestConfigValidateRejectsNonPositivePurgeInterval(t *testing.T) {
	t.Parallel()
	// A non-positive interval panics time.NewTicker (retention.go:130) — reject.
	err := audit.Config{RetainWindow: 90 * 24 * time.Hour, PurgeInterval: -1 * time.Second}.Validate()
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_CONFIG_INVALID")

	require.Error(t, audit.Config{RetainWindow: 90 * 24 * time.Hour, PurgeInterval: 0}.Validate(),
		"a zero purge_interval is also rejected")
}

func TestStartWithNilJSReturnsDepNotStartedError(t *testing.T) {
	t.Parallel()
	s := audit.NewSubsystem(stubJS{}, stubPool{}, audit.Config{})
	err := s.Start(t.Context())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_DEP_NOT_STARTED")
}

type stubJSValid struct{ js jetstream.JetStream }

func (s stubJSValid) JS() jetstream.JetStream { return s.js }

func TestStartWithNilPoolReturnsDepNotStartedError(t *testing.T) {
	t.Parallel()
	// JS provider returns non-nil; pool provider returns nil. Start
	// must reject on the pool guard.
	fakeJS := stubJSValid{js: fakeJetStream{}}
	s := audit.NewSubsystem(fakeJS, stubPool{}, audit.Config{})
	err := s.Start(t.Context())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_DEP_NOT_STARTED")
}

// fakeJetStream is the minimum jetstream.JetStream surface needed to pass
// the nil-check in Subsystem.Start. All methods return zero/err — the
// test exits before any are called.
type fakeJetStream struct{ jetstream.JetStream }

func TestStopWithoutStartIsNoop(t *testing.T) {
	t.Parallel()
	s := audit.NewSubsystem(stubJS{}, stubPool{}, audit.Config{})
	// Stop before Start must not panic or error.
	require.NoError(t, s.Stop(t.Context()))
}

func TestAwaitDrainedBeforeStartIsNoop(t *testing.T) {
	t.Parallel()
	s := audit.NewSubsystem(stubJS{}, stubPool{}, audit.Config{})
	// Pre-start AwaitDrained returns immediately without using t.Fatalf;
	// test passes simply by not hanging or panicking.
	s.AwaitDrained(t, 10*time.Millisecond)
}

func TestRegisterMetricsRegistersLagGauge(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	audit.RegisterMetrics(reg)
	// Registering again with the same registry must be idempotent.
	audit.RegisterMetrics(reg)
	// GaugeVecs with no observed label sets produce no MetricFamily rows
	// in Gather; touch a label to make the metric visible. The Set value
	// is throw-away — the test only asserts registration, not the value.
	audit.LagSeconds.WithLabelValues("test").Set(0)
	mfs, err := reg.Gather()
	require.NoError(t, err)
	names := make([]string, 0, len(mfs))
	for _, mf := range mfs {
		names = append(names, mf.GetName())
	}
	assert.Contains(t, names, "holomush_audit_projection_lag_seconds")
}
