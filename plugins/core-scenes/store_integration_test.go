// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
	"github.com/holomush/holomush/test/testutil"
)

// newTestStore starts a Postgres testcontainer, opens a SceneStore against
// it, and returns the store with a cleanup function.
//
// Note: testutil.PostgresEnv exposes the connection string via the ConnStr
// field (no "ing" suffix). The holomush role owns the public schema, so
// the plugin's migrations create the scenes table directly in public.
// Schema isolation via SchemaProvisioner is exercised by the end-to-end
// test in test/integration/plugin/core_scenes_test.go (Task 13), not here.
func newTestStore(t *testing.T) (*SceneStore, func()) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	pgEnv, err := testutil.StartPostgres(ctx)
	require.NoError(t, err, "failed to start postgres testcontainer")

	store, err := NewSceneStore(ctx, pgEnv.ConnStr)
	require.NoError(t, err, "failed to open scene store")

	cleanup := func() {
		store.Close()
		_ = pgEnv.Terminate(ctx)
		cancel()
	}
	return store, cleanup
}

func TestSceneStoreCreatePersistsAllSceneFields(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()
	locationID := "loc-01"
	row := &SceneRow{
		ID:              "scene-01HXYZ",
		Title:           "A Decades-Crossed Meeting",
		Description:     "Off-grid private meeting",
		LocationID:      &locationID,
		OwnerID:         "char-alice",
		State:           string(SceneStateActive),
		PoseOrder:       string(PoseOrderModeFree),
		Visibility:      string(SceneVisibilityOpen),
		ContentWarnings: []string{},
		Tags:            []string{"plot", "social"},
	}

	err := store.Create(ctx, row)
	require.NoError(t, err)

	got, err := store.Get(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, row.ID, got.ID)
	assert.Equal(t, row.Title, got.Title)
	assert.Equal(t, row.Description, got.Description)
	require.NotNil(t, got.LocationID)
	assert.Equal(t, locationID, *got.LocationID)
	assert.Equal(t, row.OwnerID, got.OwnerID)
	assert.Equal(t, row.State, got.State)
	assert.Equal(t, row.PoseOrder, got.PoseOrder)
	assert.Equal(t, row.Visibility, got.Visibility)
	assert.ElementsMatch(t, row.Tags, got.Tags)
	assert.NotZero(t, got.CreatedAt)
}

func TestSceneStoreGetReturnsNotFoundForMissingScene(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()

	_, err := store.Get(ctx, "scene-does-not-exist")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_NOT_FOUND")

	var oopsErr oops.OopsError
	if errors.As(err, &oopsErr) {
		assert.Equal(t, "scene-does-not-exist", oopsErr.Context()["scene_id"])
	}
}

func TestSceneStoreCreateRejectsDuplicateID(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()
	row := &SceneRow{
		ID:              "scene-dup",
		Title:           "Original",
		OwnerID:         "char-bob",
		State:           string(SceneStateActive),
		PoseOrder:       string(PoseOrderModeFree),
		Visibility:      string(SceneVisibilityOpen),
		ContentWarnings: []string{},
		Tags:            []string{},
	}

	err := store.Create(ctx, row)
	require.NoError(t, err)

	err = store.Create(ctx, row)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_CREATE_FAILED")
}
