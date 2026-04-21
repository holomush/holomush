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

// TestAuditBackfillProducesMatchingCounts covers spec §8 "Backfill
// rebuild -> bin/holomush audit-backfill produces matching counts".
// The audit-backfill CLI subcommand does not exist yet; this test
// stands up the JS stream with content and defers the CLI invocation
// + post-invocation count parity assertion to the follow-up bead.
//
// Follow-up: holomush-l4kx — holomush audit-backfill CLI subcommand.
func TestAuditBackfillProducesMatchingCounts(t *testing.T) {
	t.Skip("TODO(holomush-l4kx): audit-backfill CLI not yet implemented — skeleton retained for the follow-up bead")

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	bus := eventbustest.New(t)
	pool := freshPool(t)
	pub := bus.Bus.Publisher()

	// Publish N events while the audit projection is NOT running, so
	// events_audit stays empty while JS accumulates the stream.
	const count = 20
	for i := 0; i < count; i++ {
		require.NoError(t, pub.Publish(ctx, mintEvent(
			eventbus.Subject("events.main.backfill.s1"),
			"scene.pose",
			`{"n":`+itoa(i)+`}`)))
	}
	// Sanity: events_audit is empty right now (no projection running).

	// TODO(holomush-l4kx): Exec the subcommand:
	//   cmd := exec.Command("bin/holomush", "audit-backfill", "--dsn", connStr)
	//   out, err := cmd.CombinedOutput()
	//   require.NoError(t, err, "%s", out)
	//
	// After:
	//   Assertion: events_audit row count == JS stream LastSeq.

	_ = audit.DefaultConsumerName // silence unused import if imports change
	_ = pool
}
