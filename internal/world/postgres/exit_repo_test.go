// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/postgres"
)

// createTestLocations creates two test locations for exit tests.
func createTestLocations(ctx context.Context, t *testing.T) (ulid.ULID, ulid.ULID) {
	t.Helper()

	loc1ID := ulid.Make()
	loc2ID := ulid.Make()

	_, err := testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, loc1ID.String(), "Test Room 1", "First test room", "persistent", "last:0", time.Now())
	require.NoError(t, err)

	_, err = testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, loc2ID.String(), "Test Room 2", "Second test room", "persistent", "last:0", time.Now())
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM exits WHERE from_location_id = $1 OR to_location_id = $1`, loc1ID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM exits WHERE from_location_id = $1 OR to_location_id = $1`, loc2ID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, loc1ID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, loc2ID.String())
	})

	return loc1ID, loc2ID
}

func TestExitRepository_CRUD(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewExitRepository(testPool)

	t.Run("create and get", func(t *testing.T) {
		loc1ID, loc2ID := createTestLocations(ctx, t)

		exit := &world.Exit{
			ID:             ulid.Make(),
			FromLocationID: loc1ID,
			ToLocationID:   loc2ID,
			Name:           "north",
			Aliases:        []string{"n", "forward"},
			Bidirectional:  false,
			Visibility:     world.VisibilityAll,
		}

		err := repo.Create(ctx, exit)
		require.NoError(t, err)

		got, err := repo.Get(ctx, exit.ID)
		require.NoError(t, err)
		assert.Equal(t, exit.Name, got.Name)
		assert.Equal(t, exit.FromLocationID, got.FromLocationID)
		assert.Equal(t, exit.ToLocationID, got.ToLocationID)
		assert.Equal(t, exit.Aliases, got.Aliases)
		assert.Equal(t, exit.Bidirectional, got.Bidirectional)
		assert.Equal(t, exit.Visibility, got.Visibility)
	})

	t.Run("create bidirectional", func(t *testing.T) {
		loc1ID, loc2ID := createTestLocations(ctx, t)

		exit := &world.Exit{
			ID:             ulid.Make(),
			FromLocationID: loc1ID,
			ToLocationID:   loc2ID,
			Name:           "east",
			Aliases:        []string{"e"},
			Bidirectional:  true,
			ReturnName:     "west",
			Visibility:     world.VisibilityAll,
		}

		err := repo.Create(ctx, exit)
		require.NoError(t, err)

		// Check that the return exit was created
		returnExit, err := repo.FindByName(ctx, loc2ID, "west")
		require.NoError(t, err)
		assert.Equal(t, "west", returnExit.Name)
		assert.Equal(t, loc2ID, returnExit.FromLocationID)
		assert.Equal(t, loc1ID, returnExit.ToLocationID)
		assert.True(t, returnExit.Bidirectional)
		assert.Equal(t, "east", returnExit.ReturnName)
	})

	t.Run("update", func(t *testing.T) {
		loc1ID, loc2ID := createTestLocations(ctx, t)

		exit := &world.Exit{
			ID:             ulid.Make(),
			FromLocationID: loc1ID,
			ToLocationID:   loc2ID,
			Name:           "south",
			Bidirectional:  false,
			Visibility:     world.VisibilityAll,
		}

		err := repo.Create(ctx, exit)
		require.NoError(t, err)

		exit.Name = "southeast"
		exit.Aliases = []string{"se"}
		err = repo.Update(ctx, exit)
		require.NoError(t, err)

		got, err := repo.Get(ctx, exit.ID)
		require.NoError(t, err)
		assert.Equal(t, "southeast", got.Name)
		assert.Equal(t, []string{"se"}, got.Aliases)
	})

	t.Run("delete", func(t *testing.T) {
		loc1ID, loc2ID := createTestLocations(ctx, t)

		exit := &world.Exit{
			ID:             ulid.Make(),
			FromLocationID: loc1ID,
			ToLocationID:   loc2ID,
			Name:           "west",
			Bidirectional:  false,
			Visibility:     world.VisibilityAll,
		}

		err := repo.Create(ctx, exit)
		require.NoError(t, err)

		err = repo.Delete(ctx, exit.ID)
		require.NoError(t, err)

		_, err = repo.Get(ctx, exit.ID)
		assert.Error(t, err)
	})

	t.Run("delete bidirectional", func(t *testing.T) {
		loc1ID, loc2ID := createTestLocations(ctx, t)

		exit := &world.Exit{
			ID:             ulid.Make(),
			FromLocationID: loc1ID,
			ToLocationID:   loc2ID,
			Name:           "up",
			Bidirectional:  true,
			ReturnName:     "down",
			Visibility:     world.VisibilityAll,
		}

		err := repo.Create(ctx, exit)
		require.NoError(t, err)

		// Verify return exit exists
		returnExit, err := repo.FindByName(ctx, loc2ID, "down")
		require.NoError(t, err)
		require.NotNil(t, returnExit)

		// Delete the forward exit
		err = repo.Delete(ctx, exit.ID)
		require.NoError(t, err)

		// Both exits should be gone
		_, err = repo.Get(ctx, exit.ID)
		assert.Error(t, err)

		_, err = repo.FindByName(ctx, loc2ID, "down")
		assert.Error(t, err)
	})

	t.Run("get not found", func(t *testing.T) {
		_, err := repo.Get(ctx, ulid.Make())
		assert.Error(t, err)
		assert.ErrorIs(t, err, postgres.ErrNotFound)
	})

	t.Run("update not found", func(t *testing.T) {
		exit := &world.Exit{
			ID:             ulid.Make(),
			FromLocationID: ulid.Make(),
			ToLocationID:   ulid.Make(),
			Name:           "nonexistent",
			Visibility:     world.VisibilityAll,
		}
		err := repo.Update(ctx, exit)
		assert.Error(t, err)
		assert.ErrorIs(t, err, postgres.ErrNotFound)
	})

	t.Run("delete not found", func(t *testing.T) {
		err := repo.Delete(ctx, ulid.Make())
		assert.Error(t, err)
		assert.ErrorIs(t, err, postgres.ErrNotFound)
	})
}

func TestExitRepository_ListFromLocation(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewExitRepository(testPool)
	loc1ID, loc2ID := createTestLocations(ctx, t)

	// Create multiple exits from the same location
	exits := []*world.Exit{
		{
			ID:             ulid.Make(),
			FromLocationID: loc1ID,
			ToLocationID:   loc2ID,
			Name:           "alpha",
			Bidirectional:  false,
			Visibility:     world.VisibilityAll,
		},
		{
			ID:             ulid.Make(),
			FromLocationID: loc1ID,
			ToLocationID:   loc2ID,
			Name:           "beta",
			Bidirectional:  false,
			Visibility:     world.VisibilityAll,
		},
	}

	for _, exit := range exits {
		err := repo.Create(ctx, exit)
		require.NoError(t, err)
	}

	// List exits from loc1
	got, err := repo.ListFromLocation(ctx, loc1ID)
	require.NoError(t, err)
	assert.Len(t, got, 2)

	// Exits should be ordered by name
	assert.Equal(t, "alpha", got[0].Name)
	assert.Equal(t, "beta", got[1].Name)
}

func TestExitRepository_FindByName(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewExitRepository(testPool)
	loc1ID, loc2ID := createTestLocations(ctx, t)

	exit := &world.Exit{
		ID:             ulid.Make(),
		FromLocationID: loc1ID,
		ToLocationID:   loc2ID,
		Name:           "north",
		Aliases:        []string{"n", "forward"},
		Bidirectional:  false,
		Visibility:     world.VisibilityAll,
	}

	err := repo.Create(ctx, exit)
	require.NoError(t, err)

	tests := []struct {
		name      string
		input     string
		expectErr bool
	}{
		{"exact name", "north", false},
		{"name case insensitive", "North", false},
		{"name uppercase", "NORTH", false},
		{"alias n", "n", false},
		{"alias N", "N", false},
		{"alias forward", "forward", false},
		{"alias Forward", "Forward", false},
		{"not found", "south", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repo.FindByName(ctx, loc1ID, tt.input)
			if tt.expectErr {
				assert.Error(t, err)
				assert.ErrorIs(t, err, postgres.ErrNotFound)
			} else {
				require.NoError(t, err)
				assert.Equal(t, exit.ID, got.ID)
			}
		})
	}
}

func TestExitRepository_WithLockData(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewExitRepository(testPool)
	loc1ID, loc2ID := createTestLocations(ctx, t)

	exit := &world.Exit{
		ID:             ulid.Make(),
		FromLocationID: loc1ID,
		ToLocationID:   loc2ID,
		Name:           "locked-door",
		Bidirectional:  false,
		Visibility:     world.VisibilityAll,
		Locked:         true,
		LockType:       world.LockTypeKey,
		LockData: map[string]any{
			"key_id":  "some-key-id",
			"message": "You need a key to open this door.",
		},
	}

	err := repo.Create(ctx, exit)
	require.NoError(t, err)

	got, err := repo.Get(ctx, exit.ID)
	require.NoError(t, err)
	assert.True(t, got.Locked)
	assert.Equal(t, world.LockTypeKey, got.LockType)
	assert.Equal(t, "some-key-id", got.LockData["key_id"])
	assert.Equal(t, "You need a key to open this door.", got.LockData["message"])
}

func TestExitRepository_WithVisibleToList(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewExitRepository(testPool)
	loc1ID, loc2ID := createTestLocations(ctx, t)

	charID1 := ulid.Make()
	charID2 := ulid.Make()

	exit := &world.Exit{
		ID:             ulid.Make(),
		FromLocationID: loc1ID,
		ToLocationID:   loc2ID,
		Name:           "secret-door",
		Bidirectional:  false,
		Visibility:     world.VisibilityList,
		VisibleTo:      []ulid.ULID{charID1, charID2},
	}

	err := repo.Create(ctx, exit)
	require.NoError(t, err)

	got, err := repo.Get(ctx, exit.ID)
	require.NoError(t, err)
	assert.Equal(t, world.VisibilityList, got.Visibility)
	assert.Len(t, got.VisibleTo, 2)
	assert.Contains(t, got.VisibleTo, charID1)
	assert.Contains(t, got.VisibleTo, charID2)
}

func TestExitRepository_FindBySimilarity(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewExitRepository(testPool)
	loc1ID, loc2ID := createTestLocations(ctx, t)

	// Create exits with various names for fuzzy matching tests
	exits := []*world.Exit{
		{
			ID:             ulid.Make(),
			FromLocationID: loc1ID,
			ToLocationID:   loc2ID,
			Name:           "north",
			Aliases:        []string{"n", "northward"},
			Bidirectional:  false,
			Visibility:     world.VisibilityAll,
		},
		{
			ID:             ulid.Make(),
			FromLocationID: loc1ID,
			ToLocationID:   loc2ID,
			Name:           "northeast",
			Aliases:        []string{"ne"},
			Bidirectional:  false,
			Visibility:     world.VisibilityAll,
		},
		{
			ID:             ulid.Make(),
			FromLocationID: loc1ID,
			ToLocationID:   loc2ID,
			Name:           "south",
			Aliases:        []string{"s", "southward"},
			Bidirectional:  false,
			Visibility:     world.VisibilityAll,
		},
	}

	for _, exit := range exits {
		err := repo.Create(ctx, exit)
		require.NoError(t, err)
	}

	tests := []struct {
		name        string
		searchTerm  string
		threshold   float64
		wantName    string
		wantErr     bool
		errContains string
	}{
		{
			name:       "exact match",
			searchTerm: "north",
			threshold:  0.3,
			wantName:   "north",
			wantErr:    false,
		},
		{
			name:       "typo match - nroth for north",
			searchTerm: "nroth",
			threshold:  0.2, // pg_trgm similarity for nroth/north is ~0.25
			wantName:   "north",
			wantErr:    false,
		},
		{
			name:       "typo match - soth for south",
			searchTerm: "soth",
			threshold:  0.3,
			wantName:   "south",
			wantErr:    false,
		},
		{
			name:       "partial match - nor",
			searchTerm: "nor",
			threshold:  0.3,
			wantName:   "north",
			wantErr:    false,
		},
		{
			name:       "alias fuzzy match - northwrd for northward",
			searchTerm: "northwrd",
			threshold:  0.3,
			wantName:   "north",
			wantErr:    false,
		},
		{
			name:        "below threshold - no match",
			searchTerm:  "xyz",
			threshold:   0.5,
			wantErr:     true,
			errContains: "not found",
		},
		{
			name:        "high threshold - no match",
			searchTerm:  "nroth",
			threshold:   0.9,
			wantErr:     true,
			errContains: "not found",
		},
		{
			name:        "invalid threshold - negative",
			searchTerm:  "north",
			threshold:   -0.1,
			wantErr:     true,
			errContains: "threshold must be between 0.0 and 1.0",
		},
		{
			name:        "invalid threshold - too high",
			searchTerm:  "north",
			threshold:   1.5,
			wantErr:     true,
			errContains: "threshold must be between 0.0 and 1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repo.FindBySimilarity(ctx, loc1ID, tt.searchTerm, tt.threshold)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantName, got.Name)
		})
	}
}

func TestExitRepository_FindBySimilarity_BestMatch(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewExitRepository(testPool)
	loc1ID, loc2ID := createTestLocations(ctx, t)

	// Create exits with similar names to test best match selection
	exits := []*world.Exit{
		{
			ID:             ulid.Make(),
			FromLocationID: loc1ID,
			ToLocationID:   loc2ID,
			Name:           "door",
			Bidirectional:  false,
			Visibility:     world.VisibilityAll,
		},
		{
			ID:             ulid.Make(),
			FromLocationID: loc1ID,
			ToLocationID:   loc2ID,
			Name:           "doorway",
			Bidirectional:  false,
			Visibility:     world.VisibilityAll,
		},
	}

	for _, exit := range exits {
		err := repo.Create(ctx, exit)
		require.NoError(t, err)
	}

	// "door" should match "door" better than "doorway"
	got, err := repo.FindBySimilarity(ctx, loc1ID, "door", 0.3)
	require.NoError(t, err)
	assert.Equal(t, "door", got.Name)

	// "doorwa" should match "doorway" better than "door"
	got, err = repo.FindBySimilarity(ctx, loc1ID, "doorwa", 0.3)
	require.NoError(t, err)
	assert.Equal(t, "doorway", got.Name)
}

// createTestLocationWithOwner creates a test location with an owner.
// If ownerID is provided, the character must already exist in the database.
func createTestLocationWithOwner(ctx context.Context, t *testing.T, name string, ownerID *ulid.ULID) ulid.ULID {
	t.Helper()

	locID := ulid.Make()
	var ownerStr *string
	if ownerID != nil {
		s := ownerID.String()
		ownerStr = &s
	}

	_, err := testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, owner_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, locID.String(), name, "Test location", "persistent", "last:0", ownerStr, time.Now())
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM exits WHERE from_location_id = $1 OR to_location_id = $1`, locID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locID.String())
	})

	return locID
}

func TestExitRepository_ListVisibleExits(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewExitRepository(testPool)

	// Create test characters (required for owner_id FK constraint)
	ownerID := createTestCharacter(ctx, t, "Owner")
	allowedCharID := createTestCharacter(ctx, t, "AllowedChar")
	otherCharID := createTestCharacter(ctx, t, "OtherChar")

	// Create location with owner
	loc1ID := createTestLocationWithOwner(ctx, t, "Owned Room", &ownerID)
	_, loc2ID := createTestLocations(ctx, t)

	// Create exits with different visibility settings
	publicExit := &world.Exit{
		ID:             ulid.Make(),
		FromLocationID: loc1ID,
		ToLocationID:   loc2ID,
		Name:           "public-door",
		Visibility:     world.VisibilityAll,
	}
	ownerExit := &world.Exit{
		ID:             ulid.Make(),
		FromLocationID: loc1ID,
		ToLocationID:   loc2ID,
		Name:           "owner-door",
		Visibility:     world.VisibilityOwner,
	}
	listExit := &world.Exit{
		ID:             ulid.Make(),
		FromLocationID: loc1ID,
		ToLocationID:   loc2ID,
		Name:           "list-door",
		Visibility:     world.VisibilityList,
		VisibleTo:      []ulid.ULID{allowedCharID},
	}

	for _, exit := range []*world.Exit{publicExit, ownerExit, listExit} {
		err := repo.Create(ctx, exit)
		require.NoError(t, err)
	}

	t.Run("owner sees all exits", func(t *testing.T) {
		exits, err := repo.ListVisibleExits(ctx, loc1ID, ownerID)
		require.NoError(t, err)
		assert.Len(t, exits, 2) // public + owner exits (not list exit unless owner is in list)

		names := make([]string, len(exits))
		for i, e := range exits {
			names[i] = e.Name
		}
		assert.Contains(t, names, "public-door")
		assert.Contains(t, names, "owner-door")
	})

	t.Run("allowed char sees public and list exits", func(t *testing.T) {
		exits, err := repo.ListVisibleExits(ctx, loc1ID, allowedCharID)
		require.NoError(t, err)
		assert.Len(t, exits, 2) // public + list exits

		names := make([]string, len(exits))
		for i, e := range exits {
			names[i] = e.Name
		}
		assert.Contains(t, names, "public-door")
		assert.Contains(t, names, "list-door")
	})

	t.Run("other char sees only public exit", func(t *testing.T) {
		exits, err := repo.ListVisibleExits(ctx, loc1ID, otherCharID)
		require.NoError(t, err)
		assert.Len(t, exits, 1)
		assert.Equal(t, "public-door", exits[0].Name)
	})

	t.Run("returns empty for non-existent location", func(t *testing.T) {
		exits, err := repo.ListVisibleExits(ctx, ulid.Make(), otherCharID)
		require.NoError(t, err)
		assert.Empty(t, exits)
	})
}

func TestExitRepository_ListVisibleExits_NoOwner(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewExitRepository(testPool)

	charID := ulid.Make()

	// Create location without owner
	loc1ID := createTestLocationWithOwner(ctx, t, "Unowned Room", nil)
	_, loc2ID := createTestLocations(ctx, t)

	// Create owner-only exit
	ownerExit := &world.Exit{
		ID:             ulid.Make(),
		FromLocationID: loc1ID,
		ToLocationID:   loc2ID,
		Name:           "owner-only",
		Visibility:     world.VisibilityOwner,
	}
	publicExit := &world.Exit{
		ID:             ulid.Make(),
		FromLocationID: loc1ID,
		ToLocationID:   loc2ID,
		Name:           "public",
		Visibility:     world.VisibilityAll,
	}

	err := repo.Create(ctx, ownerExit)
	require.NoError(t, err)
	err = repo.Create(ctx, publicExit)
	require.NoError(t, err)

	// When location has no owner, owner-only exits should be invisible to everyone
	exits, err := repo.ListVisibleExits(ctx, loc1ID, charID)
	require.NoError(t, err)
	assert.Len(t, exits, 1)
	assert.Equal(t, "public", exits[0].Name)
}

func TestExitRepository_ListVisibleExits_UnknownVisibility(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewExitRepository(testPool)

	charID := ulid.Make()
	loc1ID, loc2ID := createTestLocations(ctx, t)

	// Insert exit with unknown visibility directly via SQL
	exitID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO exits (id, from_location_id, to_location_id, name, visibility, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, exitID.String(), loc1ID.String(), loc2ID.String(), "mystery-door", "unknown", time.Now())
	require.NoError(t, err)

	// Unknown visibility should not be visible (fail-closed)
	exits, err := repo.ListVisibleExits(ctx, loc1ID, charID)
	require.NoError(t, err)
	assert.Empty(t, exits)
}
