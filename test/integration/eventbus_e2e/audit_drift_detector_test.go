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

// TestAuditDriftDetectorReportsTamperedRow exercises spec §8 "Audit drift
// detector -> Tampered row reported with id". The detector is not yet
// implemented; this test:
//
//  1. Publishes a canonical event and waits for it to be projected into
//     events_audit.
//  2. Tampers the row (e.g. sets codec='not-a-real-codec' or corrupts the
//     payload).
//  3. TODO: invokes the drift detector and asserts the tampered row's id
//     is returned with a diagnostic reason.
//
// The setup is preserved so the follow-up bead only has to add the
// detector wiring and the final assertion, not rebuild the test.
//
// Follow-up: holomush-ecbg — eventbus audit drift detector.
func TestAuditDriftDetectorReportsTamperedRow(t *testing.T) {
	t.Skip("TODO(holomush-ecbg): drift detector not yet implemented — skeleton retained for the follow-up bead")

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	bus := eventbustest.New(t)
	pool := freshPool(t)

	// Stand up the host projection so publishes reach events_audit.
	hostSub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{})
	require.NoError(t, hostSub.Start(ctx))
	t.Cleanup(func() { _ = hostSub.Stop(context.Background()) })

	// Publish one event.
	pub := bus.Bus.Publisher()
	evt := mintEvent(eventbus.Subject("events.main.drift.s1"), "scene.pose", `{"x":1}`)
	require.NoError(t, pub.Publish(ctx, evt))
	hostSub.AwaitDrained(t, 10*time.Second)

	// Tamper: set codec to an unregistered value. The drift detector
	// must observe that codec resolution fails and report the id.
	_, err := pool.Exec(ctx,
		`UPDATE events_audit SET codec = 'not-a-real-codec' WHERE id = $1`,
		evt.ID.Bytes())
	require.NoError(t, err)

	// TODO(holomush-ecbg): invoke the detector and assert:
	//   reports, err := drift.Scan(ctx, pool, codec.DefaultRegistry)
	//   require.NoError(t, err)
	//   assert.Len(t, reports, 1)
	//   assert.Equal(t, evt.ID, reports[0].ID)
	//   assert.Contains(t, reports[0].Reason, "codec")
}
