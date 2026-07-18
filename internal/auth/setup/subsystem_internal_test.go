// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeNilPool is a PoolProvider that hands out a nil *pgxpool.Pool.
// The repository / store constructors only stash the pointer (no I/O at
// construction time), and the auth service constructors only nil-check
// their arguments — neither path dereferences pool, so Prepare completes
// cleanly with this fake. Used to drive subsystem.Prepare in a unit test
// without standing up a real Postgres.
type fakeNilPool struct{}

func (fakeNilPool) Pool() *pgxpool.Pool { return nil }

// TestAuthSubsystemPrepareCommitsAtomically asserts the partial-init
// regression-lock from CodeRabbit #13: the idempotency guard only
// short-circuits when BOTH authService and resetService are non-nil. If
// resetService is unset (simulating a previous Prepare that errored after
// auth construction but before reset construction), Prepare MUST run again
// and populate resetService — not return nil immediately.
func TestAuthSubsystemPrepareCommitsAtomically(t *testing.T) {
	sub := NewAuthSubsystem(AuthSubsystemConfig{DB: fakeNilPool{}})

	// First Prepare: both services populated.
	require.NoError(t, sub.Prepare(context.Background()))
	require.NotNil(t, sub.authService, "authService must be set after first Prepare")
	require.NotNil(t, sub.resetService, "resetService must be set after first Prepare")
	firstAuth := sub.authService
	firstReset := sub.resetService

	// Idempotent second Prepare with both services intact: no rebuild.
	require.NoError(t, sub.Prepare(context.Background()))
	assert.Same(t, firstAuth, sub.authService, "Prepare MUST be idempotent when both services are populated")
	assert.Same(t, firstReset, sub.resetService, "Prepare MUST be idempotent when both services are populated")

	// Simulate the partial-state regression: manually clear resetService
	// to mimic the pre-fix bug where resetSvc construction failed after
	// authSvc was assigned. Old code returned nil here (s.authService != nil
	// short-circuit); new code MUST re-run construction and re-populate
	// resetService.
	sub.resetService = nil
	require.NoError(t, sub.Prepare(context.Background()))
	assert.NotNil(t, sub.resetService, "Prepare MUST recover from partial-init state by re-running construction")
}
