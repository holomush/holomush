// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/pkg/errutil"
	"github.com/holomush/holomush/test/testutil"
)

// setupSceneStore provisions a schema-isolated database and returns a connected
// SceneStore. The caller does not need to close the store; cleanup runs via
// t.Cleanup.
func setupSceneStore(t *testing.T) *SceneStore {
	t.Helper()
	ctx := context.Background()

	pgEnv, err := testutil.StartPostgres(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pgEnv.Terminate(ctx) })

	sp := plugins.NewSchemaProvisioner(pgEnv.ConnStr)
	require.NoError(t, sp.Init(ctx))
	t.Cleanup(sp.Close)

	connStr, err := sp.ProvisionSchema(ctx, "core-scenes")
	require.NoError(t, err)

	store, err := NewSceneStore(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(store.Close)

	return store
}

func TestCreateSceneAndGetSceneRoundTrip(t *testing.T) {
	store := setupSceneStore(t)
	ctx := context.Background()

	loc := "loc-100"
	tmpl := "tmpl-42"
	timeout := 600
	now := time.Now().UTC().Truncate(time.Microsecond)

	input := &SceneRow{
		ID:              "scene-rt-1",
		Title:           "Round Trip Scene",
		Description:     "Tests create and get",
		LocationID:      &loc,
		OwnerID:         "owner-rt",
		State:           "active",
		PoseOrder:       "round-robin",
		Visibility:      "private",
		IdleTimeoutSecs: &timeout,
		TemplateID:      &tmpl,
		ContentWarnings: []string{"mature", "violence"},
		Tags:            []string{"pvp", "arena"},
		CreatedAt:       now,
	}

	require.NoError(t, store.CreateScene(ctx, input))

	got, err := store.GetScene(ctx, "scene-rt-1")
	require.NoError(t, err)

	assert.Equal(t, input.ID, got.ID)
	assert.Equal(t, input.Title, got.Title)
	assert.Equal(t, input.Description, got.Description)
	assert.Equal(t, input.LocationID, got.LocationID)
	assert.Equal(t, input.OwnerID, got.OwnerID)
	assert.Equal(t, input.State, got.State)
	assert.Equal(t, input.PoseOrder, got.PoseOrder)
	assert.Equal(t, input.Visibility, got.Visibility)
	assert.Equal(t, input.IdleTimeoutSecs, got.IdleTimeoutSecs)
	assert.Equal(t, input.TemplateID, got.TemplateID)
	assert.Equal(t, input.ContentWarnings, got.ContentWarnings)
	assert.Equal(t, input.Tags, got.Tags)
	assert.Equal(t, input.CreatedAt, got.CreatedAt.UTC())
	assert.Nil(t, got.EndedAt)
	assert.Nil(t, got.ArchivedAt)
}

func TestCreateSceneWithMinimalFields(t *testing.T) {
	store := setupSceneStore(t)
	ctx := context.Background()

	input := &SceneRow{
		ID:              "scene-min-1",
		Title:           "Minimal",
		Description:     "",
		OwnerID:         "owner-min",
		State:           "active",
		PoseOrder:       "free",
		Visibility:      "open",
		ContentWarnings: []string{},
		Tags:            []string{},
		CreatedAt:       time.Now().UTC().Truncate(time.Microsecond),
	}

	require.NoError(t, store.CreateScene(ctx, input))

	got, err := store.GetScene(ctx, "scene-min-1")
	require.NoError(t, err)

	assert.Equal(t, "scene-min-1", got.ID)
	assert.Nil(t, got.LocationID)
	assert.Nil(t, got.TemplateID)
	assert.Nil(t, got.IdleTimeoutSecs)
}

func TestGetSceneReturnsErrorForMissingScene(t *testing.T) {
	store := setupSceneStore(t)
	ctx := context.Background()

	_, err := store.GetScene(ctx, "nonexistent")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_NOT_FOUND")
}

func TestParticipantAddListRemoveCycle(t *testing.T) {
	store := setupSceneStore(t)
	ctx := context.Background()

	// Create a scene first (FK constraint).
	require.NoError(t, store.CreateScene(ctx, &SceneRow{
		ID:              "scene-part-1",
		Title:           "Participant Test",
		OwnerID:         "owner-part",
		State:           "active",
		PoseOrder:       "free",
		Visibility:      "open",
		ContentWarnings: []string{},
		Tags:            []string{},
		CreatedAt:       time.Now().UTC().Truncate(time.Microsecond),
	}))

	origin := "loc-origin"
	vote := true
	now := time.Now().UTC().Truncate(time.Microsecond)

	// Add two participants.
	p1 := &ParticipantRow{
		SceneID:          "scene-part-1",
		CharacterID:      "char-1",
		Role:             "owner",
		OriginLocationID: &origin,
		JoinedAt:         now,
		PublishVote:      &vote,
	}
	p2 := &ParticipantRow{
		SceneID:     "scene-part-1",
		CharacterID: "char-2",
		Role:        "member",
		JoinedAt:    now.Add(time.Second),
	}

	require.NoError(t, store.AddParticipant(ctx, p1))
	require.NoError(t, store.AddParticipant(ctx, p2))

	// List — should return both, ordered by joined_at.
	list, err := store.ListParticipants(ctx, "scene-part-1")
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "char-1", list[0].CharacterID)
	assert.Equal(t, "char-2", list[1].CharacterID)
	assert.Equal(t, "owner", list[0].Role)
	assert.Equal(t, &origin, list[0].OriginLocationID)
	assert.Equal(t, &vote, list[0].PublishVote)
	assert.Nil(t, list[1].OriginLocationID)
	assert.Nil(t, list[1].PublishVote)

	// Get single participant.
	got, err := store.GetParticipant(ctx, "scene-part-1", "char-1")
	require.NoError(t, err)
	assert.Equal(t, "owner", got.Role)

	// Remove char-1 and verify.
	require.NoError(t, store.RemoveParticipant(ctx, "scene-part-1", "char-1"))

	list, err = store.ListParticipants(ctx, "scene-part-1")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "char-2", list[0].CharacterID)

	// Removing again should fail.
	err = store.RemoveParticipant(ctx, "scene-part-1", "char-1")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_NOT_FOUND")
}

func TestAddParticipantUpsertsOnConflict(t *testing.T) {
	store := setupSceneStore(t)
	ctx := context.Background()

	require.NoError(t, store.CreateScene(ctx, &SceneRow{
		ID:              "scene-upsert-1",
		Title:           "Upsert Test",
		OwnerID:         "owner-u",
		State:           "active",
		PoseOrder:       "free",
		Visibility:      "open",
		ContentWarnings: []string{},
		Tags:            []string{},
		CreatedAt:       time.Now().UTC().Truncate(time.Microsecond),
	}))

	now := time.Now().UTC().Truncate(time.Microsecond)
	p := &ParticipantRow{
		SceneID:     "scene-upsert-1",
		CharacterID: "char-up",
		Role:        "member",
		JoinedAt:    now,
	}

	require.NoError(t, store.AddParticipant(ctx, p))

	// Upsert: promote to owner.
	p.Role = "owner"
	require.NoError(t, store.AddParticipant(ctx, p))

	got, err := store.GetParticipant(ctx, "scene-upsert-1", "char-up")
	require.NoError(t, err)
	assert.Equal(t, "owner", got.Role)
}

func TestMigrationIdempotency(t *testing.T) {
	ctx := context.Background()

	pgEnv, err := testutil.StartPostgres(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pgEnv.Terminate(ctx) })

	sp := plugins.NewSchemaProvisioner(pgEnv.ConnStr)
	require.NoError(t, sp.Init(ctx))
	t.Cleanup(sp.Close)

	connStr, err := sp.ProvisionSchema(ctx, "core-scenes")
	require.NoError(t, err)

	// First run — creates tables.
	store1, err := NewSceneStore(ctx, connStr)
	require.NoError(t, err)
	store1.Close()

	// Second run — must succeed without error (migrations already applied).
	store2, err := NewSceneStore(ctx, connStr)
	require.NoError(t, err)
	store2.Close()
}

func TestSceneTablesExistInPluginSchemaNotPublic(t *testing.T) {
	ctx := context.Background()

	pgEnv, err := testutil.StartPostgres(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pgEnv.Terminate(ctx) })

	sp := plugins.NewSchemaProvisioner(pgEnv.ConnStr)
	require.NoError(t, sp.Init(ctx))
	t.Cleanup(sp.Close)

	connStr, err := sp.ProvisionSchema(ctx, "core-scenes")
	require.NoError(t, err)

	_, err = NewSceneStore(ctx, connStr)
	require.NoError(t, err)

	// Query information_schema as the holomush admin role to see where
	// the scenes table landed.
	adminConn, err := pgx.Connect(ctx, pgEnv.ConnStr)
	require.NoError(t, err)
	defer adminConn.Close(ctx)

	// The scenes table MUST exist in plugin_core_scenes schema.
	var pluginCount int
	err = adminConn.QueryRow(ctx,
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = $1 AND table_name = $2",
		"plugin_core_scenes", "scenes",
	).Scan(&pluginCount)
	require.NoError(t, err)
	assert.Equal(t, 1, pluginCount, "scenes table must exist in plugin_core_scenes schema")

	// The scenes table MUST NOT exist in the public schema.
	var publicCount int
	err = adminConn.QueryRow(ctx,
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'scenes'",
	).Scan(&publicCount)
	require.NoError(t, err)
	assert.Equal(t, 0, publicCount, "scenes table must not exist in public schema")
}
