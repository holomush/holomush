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

// createTestSceneForSceneRepo creates a scene in the database for testing.
func createTestSceneForSceneRepo(ctx context.Context, t *testing.T, repo *postgres.LocationRepository, name string) *world.Location {
	t.Helper()
	scene := &world.Location{
		ID:           ulid.Make(),
		Type:         world.LocationTypeScene,
		Name:         name,
		Description:  "A test scene",
		ReplayPolicy: "last:-1",
		CreatedAt:    time.Now().UTC(),
	}
	_, err := repo.Create(ctx, scene)
	require.NoError(t, err)
	return scene
}

// seedParticipant inserts a scene_participants row directly. The world-layer
// SceneRepository write surface (AddParticipant) was removed in 05-14 (D-07);
// the read methods still SELECT/JOIN public.scene_participants, so read tests
// seed the kept table via SQL.
func seedParticipant(ctx context.Context, t *testing.T, sceneID, characterID ulid.ULID, role world.ParticipantRole) {
	t.Helper()
	_, err := testPool.Exec(ctx, `
		INSERT INTO scene_participants (scene_id, character_id, role, joined_at)
		VALUES ($1, $2, $3, (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT)
		ON CONFLICT (scene_id, character_id) DO UPDATE SET role = $3
	`, sceneID.String(), characterID.String(), role.String())
	require.NoError(t, err)
}

// createTestCharacterForSceneRepo creates a character in the database for testing.
func createTestCharacterForSceneRepo(ctx context.Context, t *testing.T, name string) string {
	t.Helper()

	// First create a test player with unique username
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
		VALUES ($1, 'Test Loc', 'Test location', 'persistent', 'last:0', (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
	`, locationID.String())
	require.NoError(t, err)

	// Create the character
	charID := ulid.Make()
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

	return charID.String()
}

func TestSceneRepository_ListParticipants(t *testing.T) {
	ctx := context.Background()
	sceneRepo := postgres.NewSceneRepository(testPool)
	locationRepo := postgres.NewLocationRepository(testPool)

	scene := createTestSceneForSceneRepo(ctx, t, locationRepo, "List Test Scene")
	char1Str := createTestCharacterForSceneRepo(ctx, t, "ListChar1")
	char1, err := ulid.Parse(char1Str)
	require.NoError(t, err)

	char2Str := createTestCharacterForSceneRepo(ctx, t, "ListChar2")
	char2, err := ulid.Parse(char2Str)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM scene_participants WHERE scene_id = $1`, scene.ID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, char1Str)
		_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, char2Str)
		_, _ = locationRepo.Delete(ctx, scene.ID, 0)
	})

	t.Run("returns all participants", func(t *testing.T) {
		seedParticipant(ctx, t, scene.ID, char1, world.RoleOwner)
		seedParticipant(ctx, t, scene.ID, char2, world.RoleMember)

		participants, err := sceneRepo.ListParticipants(ctx, scene.ID)
		require.NoError(t, err)
		assert.Len(t, participants, 2)
	})

	t.Run("returns empty for no participants", func(t *testing.T) {
		emptyScene := createTestSceneForSceneRepo(ctx, t, locationRepo, "Empty Scene")
		t.Cleanup(func() {
			_, _ = locationRepo.Delete(ctx, emptyScene.ID, 0)
		})

		participants, err := sceneRepo.ListParticipants(ctx, emptyScene.ID)
		require.NoError(t, err)
		assert.Empty(t, participants)
	})

	t.Run("returns ErrNotFound for non-existent scene", func(t *testing.T) {
		nonExistentSceneID := ulid.Make()
		participants, err := sceneRepo.ListParticipants(ctx, nonExistentSceneID)
		assert.Nil(t, participants)
		assert.Error(t, err)
		assert.ErrorIs(t, err, world.ErrNotFound)
	})
}

func TestSceneRepository_GetScenesFor(t *testing.T) {
	ctx := context.Background()
	sceneRepo := postgres.NewSceneRepository(testPool)
	locationRepo := postgres.NewLocationRepository(testPool)

	scene1 := createTestSceneForSceneRepo(ctx, t, locationRepo, "Scene 1")
	scene2 := createTestSceneForSceneRepo(ctx, t, locationRepo, "Scene 2")
	charIDStr := createTestCharacterForSceneRepo(ctx, t, "GetScenesChar")
	charID, err := ulid.Parse(charIDStr)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM scene_participants WHERE character_id = $1`, charIDStr)
		_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, charIDStr)
		_, _ = locationRepo.Delete(ctx, scene1.ID, 0)
		_, _ = locationRepo.Delete(ctx, scene2.ID, 0)
	})

	t.Run("returns all scenes for character", func(t *testing.T) {
		seedParticipant(ctx, t, scene1.ID, charID, world.RoleMember)
		seedParticipant(ctx, t, scene2.ID, charID, world.RoleOwner)

		scenes, err := sceneRepo.GetScenesFor(ctx, charID)
		require.NoError(t, err)
		assert.Len(t, scenes, 2)

		sceneIDs := make(map[string]bool)
		for _, s := range scenes {
			sceneIDs[s.ID.String()] = true
		}
		assert.True(t, sceneIDs[scene1.ID.String()])
		assert.True(t, sceneIDs[scene2.ID.String()])
	})

	t.Run("returns empty when no scenes", func(t *testing.T) {
		lonelyCharStr := createTestCharacterForSceneRepo(ctx, t, "LonelyChar")
		lonelyChar, err := ulid.Parse(lonelyCharStr)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, lonelyCharStr)
		})

		scenes, err := sceneRepo.GetScenesFor(ctx, lonelyChar)
		require.NoError(t, err)
		assert.Empty(t, scenes)
	})

	t.Run("excludes scenes after removal", func(t *testing.T) {
		removeScene := createTestSceneForSceneRepo(ctx, t, locationRepo, "Remove Scene")
		removeCharStr := createTestCharacterForSceneRepo(ctx, t, "RemoveChar")
		removeChar, err := ulid.Parse(removeCharStr)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM scene_participants WHERE scene_id = $1`, removeScene.ID.String())
			_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, removeCharStr)
			_, _ = locationRepo.Delete(ctx, removeScene.ID, 0)
		})

		seedParticipant(ctx, t, removeScene.ID, removeChar, world.RoleMember)
		_, err = testPool.Exec(ctx, `DELETE FROM scene_participants WHERE scene_id = $1 AND character_id = $2`,
			removeScene.ID.String(), removeChar.String())
		require.NoError(t, err)

		scenes, err := sceneRepo.GetScenesFor(ctx, removeChar)
		require.NoError(t, err)
		assert.Empty(t, scenes)
	})
}
