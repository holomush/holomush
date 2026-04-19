// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package eventbustest provides test helpers for the embedded JetStream
// event bus. Tests SHOULD use New(t) to get a fresh, in-memory bus per test.
//
// Each New(t) call boots its own embedded NATS server with MemoryStorage
// and DontListen + InProcessServer. There is no network and no shared state,
// so tests are safe to run in parallel.
package eventbustest

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
)

// Embedded bundles the bus subsystem with its JetStream context and
// connection so tests can interact directly.
type Embedded struct {
	Bus  *eventbus.Subsystem
	JS   jetstream.JetStream
	Conn *nats.Conn
}

// New starts a fresh embedded NATS server with MemoryStorage and registers
// cleanup on t.Cleanup. Per-test isolation; safe for t.Parallel.
//
// StoreDir is set to t.TempDir() even though MemoryStorage is in use: the
// NATS server still writes its JetStream metadata (streams, consumers)
// under StoreDir, and leaving it unset would race on the shared xdg path.
func New(t *testing.T) *Embedded {
	t.Helper()
	cfg := eventbus.Config{
		StoreDir: t.TempDir(),
	}.Defaults()
	bus := eventbus.NewSubsystemWithStorage(cfg, jetstream.MemoryStorage)
	require.NoError(t, bus.Start(context.Background()))
	t.Cleanup(func() {
		if err := bus.Stop(context.Background()); err != nil {
			t.Logf("eventbustest: bus.Stop error: %v", err)
		}
	})
	return &Embedded{
		Bus:  bus,
		JS:   bus.JS(),
		Conn: bus.Conn(),
	}
}
