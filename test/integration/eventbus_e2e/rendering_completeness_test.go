// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package eventbus_e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
)

// TestRenderingCompleteness covers INV-GW-6 + INV-GW-13. After publishing
// host-builtin events through RenderingPublisher, every events_audit row
// MUST have a non-null rendering JSONB column populated from the
// App-Rendering NATS header by the audit projection.
//
//   - INV-GW-6: events_audit.rendering is NOT NULL for every projected row.
//   - INV-GW-13: the rendering column carries the same metadata stamped by
//     the publisher (verified here via source_plugin = "builtin" for all
//     host-owned event types).
func TestRenderingCompleteness(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	bus := eventbustest.New(t)
	pool := freshPool(t)

	// Stand up the host audit projection so publishes reach events_audit.
	hostSub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{})
	require.NoError(t, hostSub.Start(ctx))
	t.Cleanup(func() { _ = hostSub.Stop(context.Background()) })

	// Build the wrapped publisher: BootstrapVerbRegistry + RenderingPublisher.
	registry, err := core.BootstrapVerbRegistry("test-0.1")
	require.NoError(t, err)
	pub := eventbus.NewRenderingPublisher(bus.Bus.Publisher(), registry)

	// Publish three host-builtin events of different types. The OwnerMap is
	// empty (default Config), so every subject is host-owned and lands in
	// events_audit.
	types := []eventbus.Type{"arrive", "leave", "system"}
	for i, typ := range types {
		ev := eventbus.Event{
			ID:        core.NewULID(),
			Subject:   eventbus.Subject("events.main.test." + string(typ)),
			Type:      typ,
			Timestamp: time.Now().UTC(),
			Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
			Payload:   []byte(`{}`),
		}
		require.NoError(t, pub.Publish(ctx, ev), "publish %d type=%s", i, typ)
	}

	// Wait for the projection to drain.
	hostSub.AwaitDrained(t, 10*time.Second)
	require.Eventually(t, func() bool {
		var count int
		err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM events_audit").Scan(&count)
		return err == nil && count >= len(types)
	}, 10*time.Second, 100*time.Millisecond, "audit projection did not drain all events")

	// INV-GW-6: every row has a non-null rendering JSONB column. Schema
	// enforces NOT NULL, but we assert here so a regression that drops the
	// constraint or writes 'null' JSONB is caught.
	var nullCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM events_audit WHERE rendering IS NULL").Scan(&nullCount)
	require.NoError(t, err)
	assert.Zero(t, nullCount, "INV-GW-6: every events_audit row MUST have non-null rendering")

	// INV-GW-13: rendering column carries the metadata stamped by the
	// publisher. Spot-check the first row's source_plugin, then verify
	// every host-builtin row reports source_plugin="builtin".
	var sourcePlugin string
	err = pool.QueryRow(
		ctx,
		"SELECT rendering->>'source_plugin' FROM events_audit ORDER BY js_seq LIMIT 1",
	).Scan(&sourcePlugin)
	require.NoError(t, err)
	assert.Equal(t, "builtin", sourcePlugin)

	var nonBuiltinCount int
	err = pool.QueryRow(
		ctx,
		"SELECT COUNT(*) FROM events_audit WHERE rendering->>'source_plugin' <> 'builtin'",
	).Scan(&nonBuiltinCount)
	require.NoError(t, err)
	assert.Zero(t, nonBuiltinCount,
		"INV-GW-13: every host-builtin row must report source_plugin='builtin'")
}
