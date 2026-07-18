// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	"context"
	"errors"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	natsexporter "github.com/nats-io/prometheus-nats-exporter/exporter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

// TestStartRollsBackWhenExporterFails exercises the
// EVENTBUS_EXPORTER_START_FAILED rollback branch by injecting a
// failing startExporterFn. Covers the 6-line rollback block that
// cannot otherwise be exercised without a genuine exporter port
// collision.
//
// Uses MonitorPort=-1 so nats-server binds a random monitor port,
// MonitorAddr() returns a valid *net.TCPAddr, and the code reaches
// startExporterFn (rather than early-exiting at the MONITOR_UNBOUND
// branch).
func TestStartRollsBackWhenExporterFails(t *testing.T) {
	orig := startExporterFn
	t.Cleanup(func() { startExporterFn = orig })
	sentinel := errors.New("injected exporter failure")
	startExporterFn = func(_, _ int) (*natsexporter.NATSExporter, error) {
		return nil, sentinel
	}

	cfg := Config{
		StoreDir:           t.TempDir(),
		PrometheusExporter: true,
		MonitorPort:        -1, // random; nats-server binds a free port
	}.Defaults()
	sub := NewSubsystemWithStorage(cfg, jetstream.MemoryStorage)
	err := sub.Prepare(context.Background())
	require.Error(t, err)
	require.ErrorIs(t, err, sentinel, "prepare error should wrap the injected failure")
	// Also assert the structured error code so this branch's API-boundary
	// contract is locked — callers distinguishing rollback reasons rely on it.
	errutil.AssertErrorCode(t, err, "EVENTBUS_EXPORTER_START_FAILED")

	// Rollback invariants: no dangling server/conn/js; safe to retry.
	assert.Nil(t, sub.JS(), "JS should be nil after failed Prepare rollback")
	assert.Nil(t, sub.Conn(), "Conn should be nil after failed Prepare rollback")
	// Idempotent Stop after failed Prepare.
	require.NoError(t, sub.Stop(context.Background()))
}
