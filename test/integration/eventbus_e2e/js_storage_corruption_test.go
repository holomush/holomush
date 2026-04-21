// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package eventbus_e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/audit"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
)

// TestJSStorageCorruptionRebuildFromPGAuditPreservesULIDs covers spec §8
// "Embedded JS storage corruption -> Rebuild from PG audit; ULIDs stable".
//
// Preserved ULIDs is the load-bearing invariant: the PG audit row id MUST
// equal the original publish ULID, so a rebuild that republishes via the
// Publisher with `Nats-Msg-Id = audit.id` will land back on the stream
// with the same seq-semantics AND the same ULID identifier. Consumers
// with pinned ULID cursors survive the corruption event transparently.
//
// The JS-rebuild tool is not yet implemented. This skeleton:
//
//  1. Publishes N events and lets them project into events_audit.
//  2. Simulates JS loss by purging the EVENTS stream (JetStream API).
//  3. TODO: invokes the rebuild tool.
//  4. TODO: asserts the new JS stream has the same N ULIDs in the same order.
//
// Follow-up: holomush-6nds — JS storage rebuild from PG audit.
func TestJSStorageCorruptionRebuildFromPGAuditPreservesULIDs(t *testing.T) {
	t.Skip("TODO(holomush-6nds): JS storage rebuild tool not yet implemented — skeleton retained for the follow-up bead")

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	bus := eventbustest.New(t)
	pool := freshPool(t)

	hostSub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{})
	require.NoError(t, hostSub.Start(ctx))
	t.Cleanup(func() { _ = hostSub.Stop(context.Background()) })

	pub := bus.Bus.Publisher()
	const count = 10
	originalIDs := make([][]byte, 0, count)
	for i := 0; i < count; i++ {
		evt := mintEvent(eventbus.Subject("events.main.jsrebuild.s1"), "scene.pose", `{"n":`+itoa(i)+`}`)
		require.NoError(t, pub.Publish(ctx, evt))
		originalIDs = append(originalIDs, evt.ID.Bytes())
	}
	hostSub.AwaitDrained(t, 10*time.Second)

	// Purge the EVENTS stream to simulate JS storage loss.
	stream, err := bus.JS.Stream(ctx, eventbus.StreamName)
	require.NoError(t, err)
	require.NoError(t, stream.Purge(ctx))

	// TODO(holomush-6nds): invoke rebuild tool, e.g.:
	//   require.NoError(t, rebuild.FromPGAudit(ctx, pool, bus.Bus.Publisher()))
	//
	// After rebuild:
	//   Assertion: stream.Info LastSeq == count
	//   Assertion: every original ULID is present (via audit OR via a drain)

	_ = originalIDs
}
