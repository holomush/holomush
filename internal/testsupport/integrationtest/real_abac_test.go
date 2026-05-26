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
	priorStream = "location:" + sess.LocationID.String()
	sess.MoveTo(ctx, ts.NewLocation(ctx))
	return sess, priorStream
}

// INV-RA-1: under WithRealABAC, a regular (non-staff) character is DENIED a
// read against a location it has left. Allow-all would permit this via the
// staffOverride bypass — so a passing assertion here proves the engine is the
// real seeded engine, not allowAllPolicyEngine.
func TestRealABAC_RegularPlayerDeniedNonColocatedRead(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ts := Start(t, WithRealABAC())
	mover, priorStream := movedAwayPlayer(t, ctx, ts, "Mover", nil)

	_, err := mover.QueryStreamHistory(ctx, priorStream)
	require.Error(t, err, "INV-RA-1: real engine MUST deny a regular player's non-colocated read")
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "denial must surface as an oops error")
	require.Equal(t, "STREAM_ACCESS_DENIED", oopsErr.Code(),
		"INV-RA-1: denial MUST collapse to STREAM_ACCESS_DENIED")
}

// INV-RA-2: without WithRealABAC, the harness retains the allow-all default —
// the same non-colocated read SUCCEEDS (staff bypass). Guards against an
// accidental flip of the default.
func TestRealABAC_DefaultEngineStillAllowAll(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ts := Start(t) // no WithRealABAC
	mover, priorStream := movedAwayPlayer(t, ctx, ts, "Mover", nil)

	_, err := mover.QueryStreamHistory(ctx, priorStream)
	require.NoError(t, err,
		"INV-RA-2: allow-all default MUST permit the non-colocated read via staff bypass")
}

// INV-RA-3 + INV-RA-5: under WithRealABAC, an admin-role character is PERMITTED
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
		"INV-RA-3/RA-5: seed:admin-full-access MUST permit an admin's non-colocated read; "+
			"a failure here means seeds weren't installed or the roles provider is unregistered")
}
