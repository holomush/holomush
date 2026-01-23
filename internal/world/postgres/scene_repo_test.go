// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/postgres"
)

// createTestSceneForSceneRepo creates a scene in the database for testing.
func createTestSceneForSceneRepo(ctx context.Context, t *testing.T, repo *postgres.LocationRepository, name string) *world.Location {
	t.Helper()
	scene := &world.Location{
		ID:           core.NewULID(),
		Type:         world.LocationTypeScene,
		Name:         name,
		Description:  "A test scene",
		ReplayPolicy: "last:-1",
		CreatedAt:    time.Now().UTC().Truncate(time.Microsecond),
	}
	err := repo.Create(ctx, scene)
	require.NoError(t, err)
	return scene
}

// createTestCharacterForSceneRepo creates a character in the database for testing.
func createTestCharacterForSceneRepo(ctx context.Context, t *testing.T, name string) string {
	t.Helper()

	// First create a test player with unique username
	playerID := core.NewULID()
	_, err := testPool.Exec(ctx, `
		INSERT INTO players (id, username, password_hash, created_at)
		VALUES ($1, $2, 'testhash', NOW())
	`, playerID.String(), "player_"+playerID.String())
	require.NoError(t, err)

	// Create a test location for the character
	locationID := core.NewULID()
	_, err = testPool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy, created_at)
		VALUES ($1, 'Test Loc', 'Test location', 'persistent', 'last:0', NOW())
	`, locationID.String())
	require.NoError(t, err)

	// Create the character
	charID := core.NewULID()
	_, err = testPool.Exec(ctx, `
		INSERT INTO characters (id, player_id, name, location_id, created_at)
		VALUES ($1, $2, $3, $4, NOW())
	`, charID.String(), playerID.String(), name, locationID.String())
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, charID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locationID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, playerID.String())
	})

	return charID.String()
}

func TestSceneRepository_AddParticipant(t *testing.T) {
	ctx := context.Background()
	sceneRepo := postgres.NewSceneRepository(testPool)
	locationRepo := postgres.NewLocationRepository(testPool)

	scene := createTestSceneForSceneRepo(ctx, t, locationRepo, "Test Scene")
	charIDStr := createTestCharacterForSceneRepo(ctx, t, "TestChar")
	charID, err := core.ParseULID(charIDStr)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM scene_participants WHERE scene_id = $1`, scene.ID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, charIDStr)
		_ = locationRepo.Delete(ctx, scene.ID)
	})

	t.Run("adds new participant", func(t *testing.T) {
		err := sceneRepo.AddParticipant(ctx, scene.ID, charID, "member")
		require.NoError(t, err)

		participants, err := sceneRepo.ListParticipants(ctx, scene.ID)
		require.NoError(t, err)
		assert.Len(t, participants, 1)
		assert.Equal(t, charID, participants[0].CharacterID)
		assert.Equal(t, "member", participants[0].Role)
	})

	t.Run("updates role on conflict", func(t *testing.T) {
		err := sceneRepo.AddParticipant(ctx, scene.ID, charID, "owner")
		require.NoError(t, err)

		participants, err := sceneRepo.ListParticipants(ctx, scene.ID)
		require.NoError(t, err)
		assert.Len(t, participants, 1)
		assert.Equal(t, "owner", participants[0].Role)
	})
}

func TestSceneRepository_RemoveParticipant(t *testing.T) {
	ctx := context.Background()
	sceneRepo := postgres.NewSceneRepository(testPool)
	locationRepo := postgres.NewLocationRepository(testPool)

	scene := createTestSceneForSceneRepo(ctx, t, locationRepo, "Remove Test Scene")
	charIDStr := createTestCharacterForSceneRepo(ctx, t, "RemoveTestChar")
	charID, err := core.ParseULID(charIDStr)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM scene_participants WHERE scene_id = $1`, scene.ID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, charIDStr)
		_ = locationRepo.Delete(ctx, scene.ID)
	})

	t.Run("removes existing participant", func(t *testing.T) {
		err := sceneRepo.AddParticipant(ctx, scene.ID, charID, "member")
		require.NoError(t, err)

		err = sceneRepo.RemoveParticipant(ctx, scene.ID, charID)
		require.NoError(t, err)

		participants, err := sceneRepo.ListParticipants(ctx, scene.ID)
		require.NoError(t, err)
		assert.Empty(t, participants)
	})

	t.Run("returns error when not found", func(t *testing.T) {
		nonExistentID := core.NewULID()
		err := sceneRepo.RemoveParticipant(ctx, scene.ID, nonExistentID)
		assert.Error(t, err)
		assert.ErrorIs(t, err, postgres.ErrNotFound)
	})
}

func TestSceneRepository_ListParticipants(t *testing.T) {
	ctx := context.Background()
	sceneRepo := postgres.NewSceneRepository(testPool)
	locationRepo := postgres.NewLocationRepository(testPool)

	scene := createTestSceneForSceneRepo(ctx, t, locationRepo, "List Test Scene")
	char1Str := createTestCharacterForSceneRepo(ctx, t, "ListChar1")
	char1, err := core.ParseULID(char1Str)
	require.NoError(t, err)

	char2Str := createTestCharacterForSceneRepo(ctx, t, "ListChar2")
	char2, err := core.ParseULID(char2Str)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM scene_participants WHERE scene_id = $1`, scene.ID.String())
		_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, char1Str)
		_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, char2Str)
		_ = locationRepo.Delete(ctx, scene.ID)
	})

	t.Run("returns all participants", func(t *testing.T) {
		err := sceneRepo.AddParticipant(ctx, scene.ID, char1, "owner")
		require.NoError(t, err)
		err = sceneRepo.AddParticipant(ctx, scene.ID, char2, "member")
		require.NoError(t, err)

		participants, err := sceneRepo.ListParticipants(ctx, scene.ID)
		require.NoError(t, err)
		assert.Len(t, participants, 2)
	})

	t.Run("returns empty for no participants", func(t *testing.T) {
		emptyScene := createTestSceneForSceneRepo(ctx, t, locationRepo, "Empty Scene")
		t.Cleanup(func() {
			_ = locationRepo.Delete(ctx, emptyScene.ID)
		})

		participants, err := sceneRepo.ListParticipants(ctx, emptyScene.ID)
		require.NoError(t, err)
		assert.Empty(t, participants)
	})
}

func TestSceneRepository_GetScenesFor(t *testing.T) {
	ctx := context.Background()
	sceneRepo := postgres.NewSceneRepository(testPool)
	locationRepo := postgres.NewLocationRepository(testPool)

	scene1 := createTestSceneForSceneRepo(ctx, t, locationRepo, "Scene 1")
	scene2 := createTestSceneForSceneRepo(ctx, t, locationRepo, "Scene 2")
	charIDStr := createTestCharacterForSceneRepo(ctx, t, "GetScenesChar")
	charID, err := core.ParseULID(charIDStr)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM scene_participants WHERE character_id = $1`, charIDStr)
		_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, charIDStr)
		_ = locationRepo.Delete(ctx, scene1.ID)
		_ = locationRepo.Delete(ctx, scene2.ID)
	})

	t.Run("returns all scenes for character", func(t *testing.T) {
		err := sceneRepo.AddParticipant(ctx, scene1.ID, charID, "member")
		require.NoError(t, err)
		err = sceneRepo.AddParticipant(ctx, scene2.ID, charID, "owner")
		require.NoError(t, err)

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
		lonelyChar, err := core.ParseULID(lonelyCharStr)
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
		removeChar, err := core.ParseULID(removeCharStr)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM scene_participants WHERE scene_id = $1`, removeScene.ID.String())
			_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, removeCharStr)
			_ = locationRepo.Delete(ctx, removeScene.ID)
		})

		err = sceneRepo.AddParticipant(ctx, removeScene.ID, removeChar, "member")
		require.NoError(t, err)
		err = sceneRepo.RemoveParticipant(ctx, removeScene.ID, removeChar)
		require.NoError(t, err)

		scenes, err := sceneRepo.GetScenesFor(ctx, removeChar)
		require.NoError(t, err)
		assert.Empty(t, scenes)
	})
}
