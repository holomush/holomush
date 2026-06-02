//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package integrationtest

import (
	"context"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/test/testutil"
)

// movedAwayPlayer connects charName, records its start location, then moves it
// to a fresh location. Querying the START location afterward is the ABAC-gated
// (different-location) path: permitted only for staff via read_unrestricted_history.
func movedAwayPlayer(t *testing.T, ctx context.Context, ts *Server, charName string, roles []string) (sess *Session, priorStream string) {
	t.Helper()
	if roles == nil {
		sess = ts.ConnectAuthed(ctx, charName)
	} else {
		sess = ts.ConnectAuthedWithRoles(ctx, charName, roles)
	}
	priorStream = "location." + sess.LocationID.String()
	sess.MoveTo(ctx, ts.NewLocation(ctx))
	return sess, priorStream
}

// INV-ACCESS-1: under WithRealABAC, a regular (non-staff) character is DENIED a
// read against a location it has left. Allow-all would permit this via the
// staffOverride bypass — so a passing assertion here proves the engine is the
// real seeded engine, not allowAllPolicyEngine.
func TestRealABAC_RegularPlayerDeniedNonColocatedRead(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ts := Start(t, WithRealABAC())
	mover, priorStream := movedAwayPlayer(t, ctx, ts, "Mover", nil)

	_, err := mover.QueryStreamHistory(ctx, priorStream)
	require.Error(t, err, "INV-ACCESS-1: real engine MUST deny a regular player's non-colocated read")
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "denial must surface as an oops error")
	require.Equal(t, "STREAM_ACCESS_DENIED", oopsErr.Code(),
		"INV-ACCESS-1: denial MUST collapse to STREAM_ACCESS_DENIED")
}

// INV-ACCESS-2: without WithRealABAC, the harness retains the allow-all default —
// the same non-colocated read SUCCEEDS (staff bypass). Guards against an
// accidental flip of the default.
func TestRealABAC_DefaultEngineStillAllowAll(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ts := Start(t) // no WithRealABAC
	mover, priorStream := movedAwayPlayer(t, ctx, ts, "Mover", nil)

	_, err := mover.QueryStreamHistory(ctx, priorStream)
	require.NoError(t, err,
		"INV-ACCESS-2: allow-all default MUST permit the non-colocated read via staff bypass")
}

// INV-ACCESS-3 + INV-ACCESS-5: under WithRealABAC, an admin-role character is PERMITTED
// the non-colocated read via seed:admin-full-access (read_unrestricted_history).
// This proves (a) the seed:* set is installed and a seeded permit works, and
// (b) the provider populating principal.character.roles is registered — an
// unregistered provider (the g776/xxel fingerprint) would silently default-deny
// and flip this to STREAM_ACCESS_DENIED, which allow-all would have masked.
func TestRealABAC_AdminPermittedNonColocatedRead_g776Sentinel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ts := Start(t, WithRealABAC())
	boss, priorStream := movedAwayPlayer(t, ctx, ts, "Boss", []string{"admin"})

	_, err := boss.QueryStreamHistory(ctx, priorStream)
	require.NoError(t, err,
		"INV-ACCESS-3/INV-ACCESS-5: seed:admin-full-access MUST permit an admin's non-colocated read; "+
			"a failure here means seeds weren't installed or the roles provider is unregistered")
}

// INV-ACCESS-4: when a real ABAC subsystem is present, pluginAttrSources MUST route
// the plugin layer to the subsystem's OWN resolver and plugin provider (pointer
// identity) — not freshly-allocated standalone instances — so plugin-declared
// attribute providers register on the resolver the engine evaluates against.
func TestRealABAC_PluginAttrSourcesUsesEngineInstances(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)
	evStore, err := store.NewPostgresEventStore(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(evStore.Close)

	abacSub := startRealABAC(t, ctx, evStore.Pool())
	res, pp, aud := pluginAttrSources(abacSub)

	require.Same(t, abacSub.AttributeResolver(), res,
		"INV-ACCESS-4: plugin resolver MUST be the engine's resolver instance")
	require.Same(t, abacSub.PluginProvider(), pp,
		"INV-ACCESS-4: plugin provider MUST be the engine's plugin-provider instance")
	require.Equal(t, abacSub.AuditLogger(), aud,
		"INV-ACCESS-4: plugin auditor MUST be the engine's auditor")

	// nil subsystem → fresh standalone instances (the allow-all default path).
	resStd, ppStd, audStd := pluginAttrSources(nil)
	require.NotNil(t, resStd, "standalone resolver must be non-nil")
	require.NotNil(t, ppStd, "standalone plugin provider must be non-nil")
	require.Nil(t, audStd, "standalone auditor is nil")
	require.NotSame(t, abacSub.AttributeResolver(), resStd,
		"standalone resolver must differ from the engine's")
}

// INV-ACCESS-6: option order MUST NOT affect the resulting stack. Both orderings
// must produce the same real-ABAC deny for a regular non-colocated read.
// Plugin-gated: skipped when binary plugins are unbuilt (HOLOMUSH_REQUIRE_PLUGINS
// forces failure instead — see plugins.go).
func TestRealABAC_OptionOrderIndependent(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []StartOption
	}{
		{"plugins-then-abac", []StartOption{WithInTreePlugins(), WithRealABAC()}},
		{"abac-then-plugins", []StartOption{WithRealABAC(), WithInTreePlugins()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			ts := Start(t, tc.opts...) // skips here if binary plugins unbuilt
			mover, priorStream := movedAwayPlayer(t, ctx, ts, "Mover", nil)

			_, err := mover.QueryStreamHistory(ctx, priorStream)
			require.Error(t, err, "INV-ACCESS-6: composed real engine MUST deny regardless of option order")
			oopsErr, ok := oops.AsOops(err)
			require.True(t, ok)
			require.Equal(t, "STREAM_ACCESS_DENIED", oopsErr.Code())
		})
	}
}
