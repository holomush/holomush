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

// TestAuditOnlyChannelPersistsToEventsAudit is the regression-lock for
// holomush-jxo8.6.26: sub-epic D's host-emit security audit events
// (crypto.totp_locked, crypto.totp_cleared, crypto.totp_recovery_code_consumed,
// crypto.policy_set) MUST persist to events_audit when published through
// RenderingPublisher.
//
// Pre-fix bug: cmd/holomush/core.go wired bare eventBusSub.Publisher() to
// the totpAuditSvc and cryptoPolicySub subsystems, so events reached
// JetStream without the App-Rendering header. The audit projection's
// persist() check (audit/projection.go:headerRendering) rejected every
// such event with AUDIT_MISSING_HEADER and the row never landed.
//
// Restores INV-D14 (audit emission persists) and INV-D17 (chain genesis
// emit persists). The test exercises the full publish→persist round-trip
// for every host-emit AUDIT_ONLY event type registered in builtins.go.
func TestAuditOnlyChannelPersistsToEventsAudit(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	bus := eventbustest.New(t)
	pool := freshPool(t)

	hostSub := audit.NewSubsystem(fixedJS{js: bus.JS}, fixedPool{pool: pool}, audit.Config{})
	require.NoError(t, hostSub.Start(ctx))
	t.Cleanup(func() { _ = hostSub.Stop(context.Background()) })

	registry, err := core.BootstrapVerbRegistry("test-0.1")
	require.NoError(t, err)
	pub := eventbus.NewRenderingPublisher(bus.Bus.Publisher(), registry)

	// One event per host-emit AUDIT_ONLY type. Subjects mirror the real
	// patterns produced by internal/totp/audit.go and internal/admin/policy/emitter.go
	// so the audit row's subject column carries production-shaped values.
	//
	// gameID is unique per test run (suffixed with a fresh ULID) so the
	// downstream events_audit assertions only match rows produced by THIS
	// run — a fixed "main" would let prior test runs pollute the result
	// set when Postgres state persists across runs.
	gameID := "main_" + core.NewULID().String()
	auditEvents := []struct {
		subject eventbus.Subject
		typ     eventbus.Type
	}{
		{eventbus.Subject("events." + gameID + ".system.crypto_totp.01TESTPLAYERLOCKED.locked"), "crypto.totp_locked"},
		{eventbus.Subject("events." + gameID + ".system.crypto_totp.01TESTPLAYERCLEARD.cleared"), "crypto.totp_cleared"},
		{eventbus.Subject("events." + gameID + ".system.crypto_totp.01TESTPLAYERRECOVD.recovery_consumed"), "crypto.totp_recovery_code_consumed"},
		{eventbus.Subject("events." + gameID + ".system.crypto_policy.dual_control_required"), "crypto.policy_set"},
	}

	for _, ae := range auditEvents {
		ev := eventbus.Event{
			ID:        core.NewULID(),
			Subject:   ae.subject,
			Type:      ae.typ,
			Timestamp: time.Now().UTC(),
			Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
			Payload:   []byte(`{}`),
		}
		require.NoError(t, pub.Publish(ctx, ev), "publish %s", ae.typ)
	}

	hostSub.AwaitDrained(t, 10*time.Second)
	require.Eventually(t, func() bool {
		var count int
		err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM events_audit WHERE subject LIKE $1`,
			"events."+gameID+".%",
		).Scan(&count)
		return err == nil && count >= len(auditEvents)
	}, 10*time.Second, 100*time.Millisecond, "audit projection did not drain all AUDIT_ONLY events")

	// Every AUDIT_ONLY event lands with display_target = "EVENT_CHANNEL_AUDIT_ONLY"
	// in the rendering JSONB, source_plugin="builtin", and category="system".
	for _, ae := range auditEvents {
		var (
			displayTarget string
			sourcePlugin  string
			category      string
		)
		err := pool.QueryRow(
			ctx,
			`SELECT rendering->>'display_target',
			        rendering->>'source_plugin',
			        rendering->>'category'
			   FROM events_audit
			  WHERE type = $1
			    AND subject LIKE $2
			  ORDER BY js_seq DESC
			  LIMIT 1`,
			string(ae.typ),
			"events."+gameID+".%",
		).Scan(&displayTarget, &sourcePlugin, &category)
		require.NoError(t, err, "no events_audit row for type=%s — pre-fix bug?", ae.typ)
		assert.Equal(t, "EVENT_CHANNEL_AUDIT_ONLY", displayTarget,
			"type=%s rendering.display_target", ae.typ)
		assert.Equal(t, "builtin", sourcePlugin,
			"type=%s rendering.source_plugin", ae.typ)
		assert.Equal(t, "system", category,
			"type=%s rendering.category", ae.typ)
	}
}
