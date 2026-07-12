// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/internal/world/wmodel"
	"github.com/holomush/holomush/pkg/errutil"
)

// locationDBVersion reads the raw version column for a location, for asserting a
// CAS write did or did not advance it.
func locationDBVersion(ctx context.Context, t *testing.T, id ulid.ULID) int {
	t.Helper()
	var v int
	err := testPool.QueryRow(ctx, `SELECT version FROM locations WHERE id = $1`, id.String()).Scan(&v)
	require.NoError(t, err)
	return v
}

// newTestLocation builds an unsaved persistent location with a fresh ID.
func newTestLocation(name string) *world.Location {
	return &world.Location{
		ID:           ulid.Make(),
		Type:         world.LocationTypePersistent,
		Name:         name,
		Description:  "A version-guard test location.",
		ReplayPolicy: "last:0",
		CreatedAt:    time.Now().UTC(),
	}
}

// TestLocationRepository_UpdateVersionGuard binds MODEL-03 for location Update:
// a successful update increments the stored version and refreshes the struct;
// a stale-version update surfaces WORLD_CONCURRENT_EDIT and does NOT overwrite.
func TestLocationRepository_UpdateVersionGuard(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewLocationRepository(testPool)

	t.Run("create populates version 1 and Get reads it back", func(t *testing.T) {
		loc := newTestLocation("Version One")
		delta, err := repo.Create(ctx, loc)
		require.NoError(t, err)
		t.Cleanup(func() { _ = delErr(repo.Delete(ctx, loc.ID, 0)) })

		assert.Equal(t, 1, loc.Version, "Create refreshes the struct to the committed version")
		assert.Equal(t, 1, delta.Primary.AfterVersion)
		assert.Equal(t, 0, delta.Primary.BeforeVersion)

		got, err := repo.Get(ctx, loc.ID)
		require.NoError(t, err)
		assert.Equal(t, 1, got.Version, "Get populates the version column")
	})

	t.Run("successful update increments version by 1 and refreshes struct", func(t *testing.T) {
		loc := newTestLocation("Guarded Update")
		require.NoError(t, delErr(repo.Create(ctx, loc)))
		t.Cleanup(func() { _ = delErr(repo.Delete(ctx, loc.ID, 0)) })
		require.Equal(t, 1, loc.Version)

		loc.Name = "Guarded Update v2"
		delta, err := repo.Update(ctx, loc)
		require.NoError(t, err)
		assert.Equal(t, 2, loc.Version, "struct version refreshed to committed value (finding 12)")
		assert.Equal(t, 1, delta.Primary.BeforeVersion)
		assert.Equal(t, 2, delta.Primary.AfterVersion)
		assert.Equal(t, 2, locationDBVersion(ctx, t, loc.ID))
	})

	t.Run("stale-version update returns WORLD_CONCURRENT_EDIT and does not overwrite", func(t *testing.T) {
		loc := newTestLocation("Concurrent Loser")
		require.NoError(t, delErr(repo.Create(ctx, loc)))
		t.Cleanup(func() { _ = delErr(repo.Delete(ctx, loc.ID, 0)) })

		// A concurrent writer advances the row's version behind this struct's back.
		_, err := testPool.Exec(ctx, `UPDATE locations SET version = version + 1, name = $2 WHERE id = $1`,
			loc.ID.String(), "Winner Wins")
		require.NoError(t, err)

		// loc still carries the stale version 1; its update must lose.
		loc.Name = "Loser Overwrites"
		_, err = repo.Update(ctx, loc)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrConcurrentEdit)
		errutil.AssertErrorCode(t, err, world.CodeConcurrentEdit)

		// The winner's value is intact — the stale writer did NOT overwrite.
		got, err := repo.Get(ctx, loc.ID)
		require.NoError(t, err)
		assert.Equal(t, "Winner Wins", got.Name)
		assert.Equal(t, 2, got.Version)
	})

	t.Run("update of an absent row returns LOCATION_NOT_FOUND", func(t *testing.T) {
		loc := newTestLocation("Never Existed")
		loc.Version = 7 // non-zero: still not-found, never a spurious conflict
		_, err := repo.Update(ctx, loc)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "LOCATION_NOT_FOUND")
	})
}

// TestLocationRepository_DeleteVersionGuard binds MODEL-03 for location Delete:
// stale-version delete → conflict; absent row → not-found (two outcomes only).
func TestLocationRepository_DeleteVersionGuard(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewLocationRepository(testPool)

	t.Run("stale-version delete returns WORLD_CONCURRENT_EDIT and leaves the row", func(t *testing.T) {
		loc := newTestLocation("Delete Loser")
		require.NoError(t, delErr(repo.Create(ctx, loc)))
		t.Cleanup(func() { _ = delErr(repo.Delete(ctx, loc.ID, 0)) })

		// Advance the version so the caller's expectedVersion (1) is stale.
		_, err := testPool.Exec(ctx, `UPDATE locations SET version = version + 1 WHERE id = $1`, loc.ID.String())
		require.NoError(t, err)

		_, err = repo.Delete(ctx, loc.ID, 1)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrConcurrentEdit)
		errutil.AssertErrorCode(t, err, world.CodeConcurrentEdit)

		// The row is untouched.
		_, err = repo.Get(ctx, loc.ID)
		require.NoError(t, err)
	})

	t.Run("matched-version delete succeeds", func(t *testing.T) {
		loc := newTestLocation("Delete Winner")
		require.NoError(t, delErr(repo.Create(ctx, loc)))

		delta, err := repo.Delete(ctx, loc.ID, loc.Version)
		require.NoError(t, err)
		assert.True(t, delta.Primary.Tombstone)
		assert.Equal(t, 1, delta.Primary.BeforeVersion)

		_, err = repo.Get(ctx, loc.ID)
		assert.ErrorIs(t, err, world.ErrNotFound)
	})

	t.Run("absent-row delete returns LOCATION_NOT_FOUND (concurrent-delete-safe)", func(t *testing.T) {
		_, err := repo.Delete(ctx, ulid.Make(), 3)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "LOCATION_NOT_FOUND")
	})
}

// TestLocationRepository_DeleteCascadeDelta binds INV-WORLD-2 delta-parity for
// the location-cascade case (finding 4): a location DELETE's MutationDelta
// carries every exit the FK cascade removes as a tombstone AffectedAggregate.
func TestLocationRepository_DeleteCascadeDelta(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewLocationRepository(testPool)
	exitRepo := postgres.NewExitRepository(testPool)

	loc1ID, loc2ID := createTestLocations(ctx, t)

	// An exit FROM loc1 and an exit TO loc1 both cascade when loc1 is deleted.
	fromExit := &world.Exit{
		ID:             ulid.Make(),
		FromLocationID: loc1ID,
		ToLocationID:   loc2ID,
		Name:           "cascade-out",
		Bidirectional:  false,
		Visibility:     world.VisibilityAll,
	}
	toExit := &world.Exit{
		ID:             ulid.Make(),
		FromLocationID: loc2ID,
		ToLocationID:   loc1ID,
		Name:           "cascade-in",
		Bidirectional:  false,
		Visibility:     world.VisibilityAll,
	}
	require.NoError(t, delErr(exitRepo.Create(ctx, fromExit)))
	require.NoError(t, delErr(exitRepo.Create(ctx, toExit)))

	delta, err := repo.Delete(ctx, loc1ID, 0)
	require.NoError(t, err)

	got := make(map[ulid.ULID]wmodel.AffectedAggregate)
	for _, a := range delta.Affected {
		got[a.ID] = a
	}
	require.Len(t, delta.Affected, 2, "delta must account for both cascaded exits")
	require.Contains(t, got, fromExit.ID)
	require.Contains(t, got, toExit.ID)
	assert.True(t, got[fromExit.ID].Tombstone)
	assert.Equal(t, wmodel.AggregateExit, got[fromExit.ID].Type)
	assert.Equal(t, 1, got[fromExit.ID].BeforeVersion)
	assert.True(t, got[toExit.ID].Tombstone)

	// The FK cascade actually removed them.
	_, err = exitRepo.Get(ctx, fromExit.ID)
	assert.ErrorIs(t, err, world.ErrNotFound)
	_, err = exitRepo.Get(ctx, toExit.ID)
	assert.ErrorIs(t, err, world.ErrNotFound)
}

// TestLocationRepository_DeleteCascadeInterleave binds INV-WORLD-2 adversarially
// (round-6 R6-4): with the deletion tx holding the parent location FOR UPDATE
// lock, a concurrent tx that inserts an exit referencing that location BLOCKS on
// the FK key-share lock, then FAILS once the parent row is deleted — so no
// phantom child escapes the MutationDelta.
func TestLocationRepository_DeleteCascadeInterleave(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewLocationRepository(testPool)
	exitRepo := postgres.NewExitRepository(testPool)
	tx := postgres.NewTransactor(testPool)

	loc1ID, loc2ID := createTestLocations(ctx, t)

	// One pre-existing exit referencing loc1 — it MUST appear in the delta.
	existing := &world.Exit{
		ID:             ulid.Make(),
		FromLocationID: loc1ID,
		ToLocationID:   loc2ID,
		Name:           "pre-existing",
		Bidirectional:  false,
		Visibility:     world.VisibilityAll,
	}
	require.NoError(t, delErr(exitRepo.Create(ctx, existing)))

	phantomID := ulid.Make()
	insertErrCh := make(chan error, 1)

	var delta *wmodel.MutationDelta
	err := tx.InTransaction(ctx, func(txCtx context.Context) error {
		// repo.Delete enrolls in this ambient tx: it locks loc1 FOR UPDATE,
		// preselects the pre-existing exit, and deletes loc1 — all uncommitted.
		d, derr := repo.Delete(txCtx, loc1ID, 0)
		if derr != nil {
			return derr
		}
		delta = d

		// While the ambient tx still holds loc1's parent lock, a concurrent
		// connection tries to INSERT a phantom exit referencing loc1. The FK
		// key-share lock it needs on loc1 conflicts with our FOR UPDATE, so it
		// blocks until we commit — then fails because loc1 is gone.
		go func() {
			_, e := testPool.Exec(ctx, `
				INSERT INTO exits (id, from_location_id, to_location_id, name, visibility, created_at)
				VALUES ($1, $2, $3, 'phantom', 'all', (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
			`, phantomID.String(), loc1ID.String(), loc2ID.String())
			insertErrCh <- e
		}()

		// Let the goroutine reach the blocking FK-lock acquisition before commit.
		time.Sleep(750 * time.Millisecond)
		return nil
	})
	require.NoError(t, err)

	// The phantom insert unblocked after commit and failed (parent gone).
	insertErr := <-insertErrCh
	require.Error(t, insertErr, "phantom exit insert must fail once the parent location is deleted")

	// The phantom exit never made it into the DB.
	_, err = exitRepo.Get(ctx, phantomID)
	assert.ErrorIs(t, err, world.ErrNotFound)

	// The manifest equals exactly the pre-existing exit — no phantom escaped.
	require.Len(t, delta.Affected, 1)
	assert.Equal(t, existing.ID, delta.Affected[0].ID)
	assert.True(t, delta.Affected[0].Tombstone)
}

// TestLocationRepository_ZeroRowClassifierNoDeadlockPoolSize1 binds finding 14:
// under a pool constrained to a single connection, the zero-row classifier's
// locked follow-up read reuses the caller's connection (it runs in the same tx),
// so it resolves to WORLD_CONCURRENT_EDIT instead of deadlocking on connection
// acquisition.
func TestLocationRepository_ZeroRowClassifierNoDeadlockPoolSize1(t *testing.T) {
	ctx := context.Background()

	cfg := testPool.Config().Copy()
	cfg.MaxConns = 1
	cfg.MinConns = 1
	smallPool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	defer smallPool.Close()

	repo := postgres.NewLocationRepository(smallPool)

	loc := newTestLocation("Pool Size One")
	require.NoError(t, delErr(repo.Create(ctx, loc)))
	t.Cleanup(func() { _ = delErr(repo.Delete(ctx, loc.ID, 0)) })

	// Advance the DB version so the caller's version becomes stale — forcing the
	// zero-row classifier to run its locked follow-up read.
	_, err = smallPool.Exec(ctx, `UPDATE locations SET version = version + 1 WHERE id = $1`, loc.ID.String())
	require.NoError(t, err)

	// A hard deadline: if the classifier tried to borrow a second connection from
	// the size-1 pool, this would block until the deadline and surface a ctx
	// error rather than the conflict code.
	guarded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	loc.Name = "Should Conflict"
	_, err = repo.Update(guarded, loc)
	require.Error(t, err)
	assert.ErrorIs(t, err, world.ErrConcurrentEdit)
	errutil.AssertErrorCode(t, err, world.CodeConcurrentEdit)
}

// createTestCharacter creates a character in the database for testing.
func createTestCharacter(ctx context.Context, t *testing.T, name string) ulid.ULID {
	t.Helper()
	charID := ulid.Make()
	// First create a test player
	playerID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO players (id, username, password_hash, created_at, updated_at)
		VALUES ($1, $2, 'testhash', (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT, (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT)
	`, playerID.String(), "player_"+playerID.String())
	require.NoError(t, err)

	// Create a test location for the character
	locationID := ulid.Make()
	_, err = testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'Test Loc', 'Test', 'persistent', 'last:0', (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
	`, locationID.String())
	require.NoError(t, err)

	// Create the character
	_, err = testPool.Exec(ctx, `
		INSERT INTO characters (id, player_id, name, location_id, created_at)
		VALUES ($1, $2, $3, $4, (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
	`, charID.String(), playerID.String(), name, locationID.String())
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, charID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locationID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, playerID.String())
	})

	return charID
}

func TestLocationRepository_CRUD(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewLocationRepository(testPool)

	t.Run("create and get", func(t *testing.T) {
		loc := &world.Location{
			ID:           ulid.Make(),
			Type:         world.LocationTypePersistent,
			Name:         "Test Room",
			Description:  "A test room for testing.",
			ReplayPolicy: "last:0",
			CreatedAt:    time.Now().UTC(),
		}

		err := delErr(repo.Create(ctx, loc))
		require.NoError(t, err)

		got, err := repo.Get(ctx, loc.ID)
		require.NoError(t, err)
		assert.Equal(t, loc.Name, got.Name)
		assert.Equal(t, loc.Description, got.Description)
		assert.Equal(t, loc.Type, got.Type)
		assert.Equal(t, loc.ReplayPolicy, got.ReplayPolicy)

		// Cleanup
		_ = delErr(repo.Delete(ctx, loc.ID, 0))
	})

	t.Run("create with optional fields", func(t *testing.T) {
		// Create a valid character to use as owner
		ownerID := createTestCharacter(ctx, t, "SceneOwner")
		loc := &world.Location{
			ID:           ulid.Make(),
			Type:         world.LocationTypeScene,
			Name:         "Private Scene",
			Description:  "A private scene.",
			OwnerID:      &ownerID,
			ReplayPolicy: "last:-1",
			CreatedAt:    time.Now().UTC(),
		}

		err := delErr(repo.Create(ctx, loc))
		require.NoError(t, err)

		got, err := repo.Get(ctx, loc.ID)
		require.NoError(t, err)
		assert.NotNil(t, got.OwnerID)
		assert.Equal(t, ownerID, *got.OwnerID)

		// Cleanup
		_ = delErr(repo.Delete(ctx, loc.ID, 0))
	})

	t.Run("update", func(t *testing.T) {
		loc := &world.Location{
			ID:           ulid.Make(),
			Type:         world.LocationTypePersistent,
			Name:         "Original Name",
			Description:  "Original description.",
			ReplayPolicy: "last:0",
			CreatedAt:    time.Now().UTC(),
		}

		err := delErr(repo.Create(ctx, loc))
		require.NoError(t, err)

		loc.Name = "Updated Name"
		loc.Description = "Updated description."
		err = delErr(repo.Update(ctx, loc))
		require.NoError(t, err)

		got, err := repo.Get(ctx, loc.ID)
		require.NoError(t, err)
		assert.Equal(t, "Updated Name", got.Name)
		assert.Equal(t, "Updated description.", got.Description)

		// Cleanup
		_ = delErr(repo.Delete(ctx, loc.ID, 0))
	})

	t.Run("update with shadows_id", func(t *testing.T) {
		// Create a parent location to shadow
		parent := &world.Location{
			ID:           ulid.Make(),
			Type:         world.LocationTypePersistent,
			Name:         "Parent Location",
			Description:  "The parent.",
			ReplayPolicy: "last:0",
			CreatedAt:    time.Now().UTC(),
		}
		err := delErr(repo.Create(ctx, parent))
		require.NoError(t, err)

		// Create a scene without shadows_id
		scene := &world.Location{
			ID:           ulid.Make(),
			Type:         world.LocationTypeScene,
			Name:         "Scene Without Shadow",
			Description:  "A scene.",
			ReplayPolicy: "last:-1",
			CreatedAt:    time.Now().UTC(),
		}
		err = delErr(repo.Create(ctx, scene))
		require.NoError(t, err)

		// Update to add shadows_id
		scene.ShadowsID = &parent.ID
		err = delErr(repo.Update(ctx, scene))
		require.NoError(t, err)

		got, err := repo.Get(ctx, scene.ID)
		require.NoError(t, err)
		require.NotNil(t, got.ShadowsID)
		assert.Equal(t, parent.ID, *got.ShadowsID)

		// Cleanup
		_ = delErr(repo.Delete(ctx, scene.ID, 0))
		_ = delErr(repo.Delete(ctx, parent.ID, 0))
	})

	t.Run("update with owner_id", func(t *testing.T) {
		ownerID := createTestCharacter(ctx, t, "UpdateOwner")

		// Create a location without owner
		loc := &world.Location{
			ID:           ulid.Make(),
			Type:         world.LocationTypeScene,
			Name:         "Scene Without Owner",
			Description:  "A scene.",
			ReplayPolicy: "last:-1",
			CreatedAt:    time.Now().UTC(),
		}
		err := delErr(repo.Create(ctx, loc))
		require.NoError(t, err)

		// Update to add owner
		loc.OwnerID = &ownerID
		err = delErr(repo.Update(ctx, loc))
		require.NoError(t, err)

		got, err := repo.Get(ctx, loc.ID)
		require.NoError(t, err)
		require.NotNil(t, got.OwnerID)
		assert.Equal(t, ownerID, *got.OwnerID)

		// Cleanup
		_ = delErr(repo.Delete(ctx, loc.ID, 0))
	})

	t.Run("delete", func(t *testing.T) {
		loc := &world.Location{
			ID:           ulid.Make(),
			Type:         world.LocationTypePersistent,
			Name:         "To Delete",
			Description:  "Will be deleted.",
			ReplayPolicy: "last:0",
			CreatedAt:    time.Now().UTC(),
		}

		err := delErr(repo.Create(ctx, loc))
		require.NoError(t, err)

		err = delErr(repo.Delete(ctx, loc.ID, 0))
		require.NoError(t, err)

		_, err = repo.Get(ctx, loc.ID)
		assert.Error(t, err)
	})

	t.Run("get not found", func(t *testing.T) {
		_, err := repo.Get(ctx, ulid.Make())
		assert.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "LOCATION_NOT_FOUND")
	})

	t.Run("update not found", func(t *testing.T) {
		loc := &world.Location{
			ID:           ulid.Make(),
			Type:         world.LocationTypePersistent,
			Name:         "Nonexistent",
			Description:  "Does not exist.",
			ReplayPolicy: "last:0",
		}

		err := delErr(repo.Update(ctx, loc))
		assert.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "LOCATION_NOT_FOUND")
	})

	t.Run("delete not found", func(t *testing.T) {
		err := delErr(repo.Delete(ctx, ulid.Make(), 0))
		assert.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
		errutil.AssertErrorCode(t, err, "LOCATION_NOT_FOUND")
	})

	// Validation tests removed: Repository expects pre-validated data.
	// Validation is handled at the service layer (see service.go).
}

func TestLocationRepository_ListByType(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewLocationRepository(testPool)

	// Create test locations
	persistent := &world.Location{
		ID:           ulid.Make(),
		Type:         world.LocationTypePersistent,
		Name:         "Persistent Room",
		Description:  "A persistent room.",
		ReplayPolicy: "last:0",
		CreatedAt:    time.Now().UTC(),
	}

	scene := &world.Location{
		ID:           ulid.Make(),
		Type:         world.LocationTypeScene,
		Name:         "Test Scene",
		Description:  "A scene.",
		ReplayPolicy: "last:-1",
		CreatedAt:    time.Now().UTC(),
	}

	require.NoError(t, delErr(repo.Create(ctx, persistent)))
	require.NoError(t, delErr(repo.Create(ctx, scene)))

	t.Cleanup(func() {
		_ = delErr(repo.Delete(ctx, persistent.ID, 0))
		_ = delErr(repo.Delete(ctx, scene.ID, 0))
	})

	t.Run("list scenes", func(t *testing.T) {
		scenes, err := repo.ListByType(ctx, world.LocationTypeScene)
		require.NoError(t, err)
		assert.NotEmpty(t, scenes)

		found := false
		for _, s := range scenes {
			if s.ID == scene.ID {
				found = true
				break
			}
		}
		assert.True(t, found, "created scene should be in list")
	})

	t.Run("list persistent", func(t *testing.T) {
		persistentLocs, err := repo.ListByType(ctx, world.LocationTypePersistent)
		require.NoError(t, err)
		assert.NotEmpty(t, persistentLocs)

		found := false
		for _, p := range persistentLocs {
			if p.ID == persistent.ID {
				found = true
				break
			}
		}
		assert.True(t, found, "created persistent location should be in list")
	})

	t.Run("list instances returns empty when none", func(t *testing.T) {
		instances, err := repo.ListByType(ctx, world.LocationTypeInstance)
		require.NoError(t, err)
		// May or may not be empty depending on other test data
		_ = instances
	})
}

func TestLocationRepository_GetShadowedBy(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewLocationRepository(testPool)

	// Create parent location
	parent := &world.Location{
		ID:           ulid.Make(),
		Type:         world.LocationTypePersistent,
		Name:         "Parent Room",
		Description:  "A parent room.",
		ReplayPolicy: "last:0",
		CreatedAt:    time.Now().UTC(),
	}
	require.NoError(t, delErr(repo.Create(ctx, parent)))

	// Create scene that shadows parent
	scene := &world.Location{
		ID:           ulid.Make(),
		Type:         world.LocationTypeScene,
		ShadowsID:    &parent.ID,
		Name:         "Shadow Scene",
		Description:  "A scene that shadows parent.",
		ReplayPolicy: "last:-1",
		CreatedAt:    time.Now().UTC(),
	}
	require.NoError(t, delErr(repo.Create(ctx, scene)))

	t.Cleanup(func() {
		_ = delErr(repo.Delete(ctx, scene.ID, 0))
		_ = delErr(repo.Delete(ctx, parent.ID, 0))
	})

	t.Run("find scenes shadowing location", func(t *testing.T) {
		shadows, err := repo.GetShadowedBy(ctx, parent.ID)
		require.NoError(t, err)
		assert.NotEmpty(t, shadows)

		found := false
		for _, s := range shadows {
			if s.ID == scene.ID {
				found = true
				assert.NotNil(t, s.ShadowsID)
				assert.Equal(t, parent.ID, *s.ShadowsID)
				break
			}
		}
		assert.True(t, found, "scene should be in shadowed by list")
	})

	t.Run("no shadows returns empty", func(t *testing.T) {
		shadows, err := repo.GetShadowedBy(ctx, scene.ID)
		require.NoError(t, err)
		assert.Empty(t, shadows)
	})
}

func TestLocationRepository_FindByName(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewLocationRepository(testPool)

	// Create test location
	loc := &world.Location{
		ID:           ulid.Make(),
		Name:         "Unique Test Location",
		Description:  "A location with a unique name.",
		Type:         world.LocationTypePersistent,
		ReplayPolicy: "last:-1",
		CreatedAt:    time.Now().UTC(),
	}
	require.NoError(t, delErr(repo.Create(ctx, loc)))

	t.Cleanup(func() {
		_ = delErr(repo.Delete(ctx, loc.ID, 0))
	})

	t.Run("finds location by exact name", func(t *testing.T) {
		found, err := repo.FindByName(ctx, "Unique Test Location")
		require.NoError(t, err)
		assert.Equal(t, loc.ID, found.ID)
		assert.Equal(t, loc.Name, found.Name)
	})

	t.Run("returns ErrNotFound for non-existent name", func(t *testing.T) {
		_, err := repo.FindByName(ctx, "Non-Existent Location")
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_NOT_FOUND")
	})

	t.Run("name match is case-sensitive", func(t *testing.T) {
		_, err := repo.FindByName(ctx, "unique test location") // lowercase
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "LOCATION_NOT_FOUND")
	})
}
