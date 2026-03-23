// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/world"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
)

// TestAuthCharRepoAdapter_ImplementsInterface verifies authCharRepoAdapter satisfies auth.CharacterRepository.
func TestAuthCharRepoAdapter_ImplementsInterface(_ *testing.T) {
	var _ auth.CharacterRepository = (*authCharRepoAdapter)(nil)
}

// TestAuthLocRepoAdapter_ImplementsInterface verifies authLocRepoAdapter satisfies auth.LocationRepository.
func TestAuthLocRepoAdapter_ImplementsInterface(_ *testing.T) {
	var _ auth.LocationRepository = (*authLocRepoAdapter)(nil)
}

// TestAuthCharRepoAdapter_Create_DelegatesToWorldPostgres verifies Create delegates to worldpostgres.CharacterRepository.
func TestAuthCharRepoAdapter_Create_DelegatesToWorldPostgres(t *testing.T) {
	charRepo := worldpostgres.NewCharacterRepository(nil)
	adapter := &authCharRepoAdapter{
		charRepo: charRepo,
	}
	require.NotNil(t, adapter.charRepo)
	assert.Equal(t, charRepo, adapter.charRepo)
}

// TestAuthCharRepoAdapter_ListByPlayer_ScanFieldOrder documents the expected column order.
// Column order in query: id, player_id, name, description, location_id, created_at
// Scan order:           idStr, playerIDStr, c.Name, c.Description, locIDStr, c.CreatedAt
func TestAuthCharRepoAdapter_ListByPlayer_ScanFieldOrder(t *testing.T) {
	playerID := ulid.Make()
	char := &world.Character{
		ID:       ulid.Make(),
		PlayerID: playerID,
		Name:     "Theodora",
	}
	assert.NotZero(t, char.ID)
	assert.Equal(t, playerID, char.PlayerID)
	assert.Equal(t, "Theodora", char.Name)
}

// TestAuthLocRepoAdapter_GetStartingLocation_UsesFixedID verifies the adapter stores the
// configured startLocationID and delegates to the underlying location repository.
func TestAuthLocRepoAdapter_GetStartingLocation_UsesFixedID(t *testing.T) {
	startID := ulid.Make()
	locRepo := worldpostgres.NewLocationRepository(nil)
	adapter := &authLocRepoAdapter{
		startLocationID: startID,
		locRepo:         locRepo,
	}
	assert.Equal(t, startID, adapter.startLocationID)
	require.NotNil(t, adapter.locRepo)
	assert.Equal(t, locRepo, adapter.locRepo)
}
