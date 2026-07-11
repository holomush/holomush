// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus_test

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	"github.com/holomush/holomush/internal/lifecycle"
)

func TestSubsystemStartsAndExposesJetStream(t *testing.T) {
	t.Parallel()
	e := eventbustest.New(t)
	require.NotNil(t, e.JS)
	info, err := e.JS.Stream(context.Background(), eventbus.StreamName)
	require.NoError(t, err)
	require.NotNil(t, info)
}

func TestSubsystemStreamDeclarationIsIdempotent(t *testing.T) {
	t.Parallel()
	e := eventbustest.New(t)
	require.NoError(t, e.Bus.EnsureStream(context.Background()))
	require.NoError(t, e.Bus.EnsureStream(context.Background()))
}

func TestSubsystemStopDrainsAndShutsDown(t *testing.T) {
	t.Parallel()
	e := eventbustest.New(t)
	require.NoError(t, e.Bus.Stop(context.Background()))
	// After Stop, the JS context is cleared and the server is down.
	require.Nil(t, e.Bus.JS())
	require.Nil(t, e.Bus.Conn())
}

func TestSubsystemStopIsIdempotent(t *testing.T) {
	t.Parallel()
	e := eventbustest.New(t)
	require.NoError(t, e.Bus.Stop(context.Background()))
	require.NoError(t, e.Bus.Stop(context.Background()))
}

func TestSubsystemIDIsEventBus(t *testing.T) {
	t.Parallel()
	s := eventbus.NewSubsystem(eventbus.Config{}.Defaults())
	assert.Equal(t, lifecycle.SubsystemEventBus, s.ID())
}

func TestSubsystemDependsOnNothing(t *testing.T) {
	t.Parallel()
	s := eventbus.NewSubsystem(eventbus.Config{}.Defaults())
	require.Empty(t, s.DependsOn())
}

func TestSubsystemStartIsBounded(t *testing.T) {
	t.Parallel()
	start := time.Now()
	e := eventbustest.New(t)
	elapsed := time.Since(start)
	require.NotNil(t, e.Bus)
	// Bound is aligned with (readyTimeout in subsystem.go) + headroom so
	// the test does not flake on slow CI hosts while the implementation
	// is still correct within its own 10s readiness window.
	require.Less(t, elapsed, 11*time.Second, "Subsystem.Start exceeded readiness window")
}

func TestEnsureStreamBeforeStartReturnsError(t *testing.T) {
	t.Parallel()
	s := eventbus.NewSubsystem(eventbus.Config{}.Defaults())
	err := s.EnsureStream(context.Background())
	require.Error(t, err)
}

func TestConfigDefaultsFillZeroValues(t *testing.T) {
	t.Parallel()
	c := eventbus.Config{}.Defaults()
	assert.Equal(t, eventbus.ModeEmbedded, c.Mode)
	assert.Equal(t, "main", c.GameID)
	assert.Equal(t, 30*24*time.Hour, c.StreamMaxAge)
	assert.Equal(t, 30*time.Minute, c.DupeWindow)
	// StoreDir is intentionally left blank so the subsystem resolves it
	// via xdg.DataDir() at Start time.
	assert.Empty(t, c.StoreDir)
}

func TestSubsystemStartAfterStartIsNoop(t *testing.T) {
	t.Parallel()
	e := eventbustest.New(t)
	// Second Start on an already-started subsystem returns nil without
	// spinning up a second server.
	require.NoError(t, e.Bus.Start(context.Background()))
}

func TestSubsystemResolvesXDGStoreDirWhenBlank(t *testing.T) {
	// t.Setenv is incompatible with t.Parallel; this test runs sequentially.
	// Point XDG_DATA_HOME at a tempdir so xdg.DataDir() succeeds without
	// touching the real user home. Config.StoreDir is blank, exercising
	// the resolveStoreDir xdg branch.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	cfg := eventbus.Config{}.Defaults()
	bus := eventbus.NewSubsystemWithStorage(cfg, jetstream.MemoryStorage)
	require.NoError(t, bus.Start(context.Background()))
	t.Cleanup(func() { _ = bus.Stop(context.Background()) })
	require.NotNil(t, bus.JS())
}

func TestSubsystemPrometheusExporterStartsWhenMonitorPortSet(t *testing.T) {
	t.Parallel()
	cfg := eventbus.Config{
		StoreDir:           t.TempDir(),
		MonitorPort:        -1, // random port; subsystem reads back via MonitorAddr.
		PrometheusExporter: true,
		ExporterPort:       0, // random exporter port.
	}.Defaults()
	bus := eventbus.NewSubsystemWithStorage(cfg, jetstream.MemoryStorage)
	require.NoError(t, bus.Start(context.Background()))
	t.Cleanup(func() { _ = bus.Stop(context.Background()) })

	// The subsystem must expose JetStream and the exporter must be stoppable
	// without error — regression for the "exporter leaked after Stop" bug.
	require.NotNil(t, bus.JS())
	require.NoError(t, bus.Stop(context.Background()))
	require.Nil(t, bus.JS())
}

func TestSubsystemPrometheusExporterRequiresMonitorPort(t *testing.T) {
	t.Parallel()
	cfg := eventbus.Config{
		StoreDir:           t.TempDir(),
		MonitorPort:        0, // disabled
		PrometheusExporter: true,
	}.Defaults()
	bus := eventbus.NewSubsystemWithStorage(cfg, jetstream.MemoryStorage)
	err := bus.Start(context.Background())
	require.Error(t, err, "PrometheusExporter=true + MonitorPort=0 must fail at Start")
	// No leaks: a second Stop must be a clean no-op.
	require.NoError(t, bus.Stop(context.Background()))
}

func TestConfigDefaultsPreserveExplicitValues(t *testing.T) {
	t.Parallel()
	c := eventbus.Config{
		Mode:         eventbus.ModeExternal,
		GameID:       "custom",
		StoreDir:     "/tmp/custom",
		StreamMaxAge: 1 * time.Hour,
		DupeWindow:   5 * time.Minute,
	}.Defaults()
	assert.Equal(t, eventbus.ModeExternal, c.Mode)
	assert.Equal(t, "custom", c.GameID)
	assert.Equal(t, "/tmp/custom", c.StoreDir)
	assert.Equal(t, 1*time.Hour, c.StreamMaxAge)
	assert.Equal(t, 5*time.Minute, c.DupeWindow)
}

// TestSubsystemStartRollsBackWhenPrometheusExporterRequiresUnboundMonitor
// exercises the rollback branch when the operator has enabled the
// Prometheus exporter but the embedded NATS server's HTTP monitor port
// is unbound (MonitorPort == 0 disables monitoring entirely). Start MUST
// reject with EVENTBUS_EXPORTER_MONITOR_UNBOUND and leave no server /
// conn / js state behind — otherwise a subsequent retry would collide
// with the stranded NATS filestore.
func TestSubsystemStartRollsBackWhenPrometheusExporterRequiresUnboundMonitor(t *testing.T) {
	t.Parallel()
	cfg := eventbus.Config{
		StoreDir:           t.TempDir(),
		PrometheusExporter: true,
		MonitorPort:        0, // explicitly disabled → rollback path
	}.Defaults()
	sub := eventbus.NewSubsystemWithStorage(cfg, jetstream.MemoryStorage)
	err := sub.Start(context.Background())
	require.Error(t, err)
	// The oops code is metadata; the error message carries the hint.
	assert.Contains(t, err.Error(), "MonitorPort")
	// Rollback: after a failed Start the subsystem MUST be safe to retry
	// (no dangling server, conn, or js reference).
	assert.Nil(t, sub.JS(), "JS should be nil after failed Start rollback")
	assert.Nil(t, sub.Conn(), "Conn should be nil after failed Start rollback")
	// Stop on a failed Start must be a safe no-op (idempotent).
	require.NoError(t, sub.Stop(context.Background()))
}
