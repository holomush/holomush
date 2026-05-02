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

// seedBindingTestData inserts a player and character row for binding tests.
// Returns the string IDs used to create the binding.
func seedBindingTestData(ctx context.Context, t *testing.T) (playerID, characterID string) {
	t.Helper()
	playerULID := ulid.Make()
	charULID := ulid.Make()
	playerID = playerULID.String()
	characterID = charULID.String()

	_, err := testPool.Exec(ctx, `
		INSERT INTO players (id, username, password_hash, created_at)
		VALUES ($1, $2, 'stub-hash', NOW())
	`, playerID, "binding_test_user_"+playerID)
	require.NoError(t, err)

	_, err = testPool.Exec(ctx, `
		INSERT INTO characters (id, player_id, name, created_at)
		VALUES ($1, $2, $3, NOW())
	`, characterID, playerID, "BindingTestChar"+charULID.String()[:6])
	require.NoError(t, err)

	t.Cleanup(func() {
		// Delete bindings before characters (FK constraint).
		_, _ = testPool.Exec(context.Background(), `DELETE FROM player_character_bindings WHERE character_id = $1`, characterID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM characters WHERE id = $1`, characterID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM players WHERE id = $1`, playerID)
	})

	return playerID, characterID
}

func TestBindingRepositoryCreateReturnsIDAndCurrentReadsItBack(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewBindingRepository(testPool)

	playerID, characterID := seedBindingTestData(ctx, t)

	bindingID, err := repo.Create(ctx, playerID, characterID, "initial_bind")
	require.NoError(t, err)
	require.NotEmpty(t, bindingID)

	got, err := repo.Current(ctx, characterID)
	require.NoError(t, err)
	assert.Equal(t, bindingID, got)
}

func TestBindingRepositoryCurrentReturnsNotFoundForCharacterWithoutBinding(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewBindingRepository(testPool)

	_, err := repo.Current(ctx, ulid.Make().String())
	errutil.AssertErrorCode(t, err, "BINDING_NOT_FOUND")
}

func TestBindingRepositoryEndMarksBindingEndedAndCurrentNoLongerFindsIt(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewBindingRepository(testPool)

	playerID, characterID := seedBindingTestData(ctx, t)

	bindingID, err := repo.Create(ctx, playerID, characterID, "initial_bind")
	require.NoError(t, err)

	require.NoError(t, repo.End(ctx, bindingID, "wizard_transfer"))

	_, err = repo.Current(ctx, characterID)
	errutil.AssertErrorCode(t, err, "BINDING_NOT_FOUND")
}

func TestBindingRepositoryEndOnAlreadyEndedReturnsTypedError(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewBindingRepository(testPool)

	playerID, characterID := seedBindingTestData(ctx, t)

	bindingID, err := repo.Create(ctx, playerID, characterID, "initial_bind")
	require.NoError(t, err)
	require.NoError(t, repo.End(ctx, bindingID, "wizard_transfer"))

	err = repo.End(ctx, bindingID, "wizard_transfer")
	errutil.AssertErrorCode(t, err, "BINDING_ALREADY_ENDED")
}
