// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/pkg/errutil"
)

// EnsureNotReaping returns nil for a player whose reaping_at is NULL.
func TestReapingGuardEnsureNotReapingAllowsUnmarkedPlayer(t *testing.T) {
	ctx := context.Background()
	guard := postgres.NewReapingGuard(testPool)
	playerID := createTestPlayer(ctx, t)

	require.NoError(t, guard.EnsureNotReaping(ctx, playerID))
}

// EnsureNotReaping returns PLAYER_REAPING once reaping_at is set.
func TestReapingGuardEnsureNotReapingRejectsMarkedPlayer(t *testing.T) {
	ctx := context.Background()
	guard := postgres.NewReapingGuard(testPool)
	playerID := createTestPlayer(ctx, t)

	_, err := testPool.Exec(ctx,
		`UPDATE players SET reaping_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT WHERE id = $1`,
		playerID.String())
	require.NoError(t, err)

	err = guard.EnsureNotReaping(ctx, playerID)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PLAYER_REAPING")
}

// EnsureNotReaping treats a missing player as not-reaping (the genesis INSERT
// would fail on the player_id FK anyway).
func TestReapingGuardEnsureNotReapingAllowsMissingPlayer(t *testing.T) {
	ctx := context.Background()
	guard := postgres.NewReapingGuard(testPool)

	assert.NoError(t, guard.EnsureNotReaping(ctx, ulid.Make()))
}
