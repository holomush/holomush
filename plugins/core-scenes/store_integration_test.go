// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
	"github.com/holomush/holomush/test/testutil"
)

// newTestStore starts a Postgres testcontainer and opens a SceneStore against
// it. Cleanup is registered via t.Cleanup in two phases (container, then
// store) so the container is released even if NewSceneStore fails. Container
// termination uses a fresh, short-lived context so teardown does not inherit
// the 2-minute setup deadline.
//
// Note: testutil.PostgresEnv exposes the connection string via the ConnStr
// field (no "ing" suffix). The holomush role owns the public schema, so
// the plugin's migrations create the scenes table directly in public.
// Schema isolation via SchemaProvisioner is exercised by the end-to-end
// test in test/integration/plugin/core_scenes_test.go (Task 13), not here.
func newTestStore(t *testing.T) *SceneStore {
	t.Helper()

	setupCtx, cancelSetup := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancelSetup)

	pgEnv, err := testutil.StartPostgres(setupCtx)
	require.NoError(t, err, "failed to start postgres testcontainer")
	// Register container termination immediately so a subsequent
	// NewSceneStore failure cannot leak the container.
	t.Cleanup(func() {
		termCtx, cancelTerm := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelTerm()
		_ = pgEnv.Terminate(termCtx)
	})

	store, err := NewSceneStore(setupCtx, pgEnv.ConnStr)
	require.NoError(t, err, "failed to open scene store")
	t.Cleanup(store.Close)

	return store
}

func TestSceneStoreCreatePersistsAllSceneFields(t *testing.T) {
	store := newTestStore(t)

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
	store := newTestStore(t)

	ctx := context.Background()

	_, err := store.Get(ctx, "scene-does-not-exist")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_NOT_FOUND")
	// Use errutil.AssertErrorContext so the scene_id context is asserted
	// unconditionally — a conditional errors.As block lets the test pass
	// silently if the context ever stops being attached.
	errutil.AssertErrorContext(t, err, "scene_id", "scene-does-not-exist")
}

func TestSceneStoreCreateRejectsDuplicateID(t *testing.T) {
	store := newTestStore(t)

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

func TestSceneStoreEndTransitionsActiveToEnded(t *testing.T) {
	store := newTestStore(t)

	ctx := context.Background()
	row := &SceneRow{
		ID:              "scene-end-active",
		Title:           "End from active",
		OwnerID:         "char-alice",
		State:           string(SceneStateActive),
		PoseOrder:       string(PoseOrderModeFree),
		Visibility:      string(SceneVisibilityOpen),
		ContentWarnings: []string{},
		Tags:            []string{},
	}
	require.NoError(t, store.Create(ctx, row))

	got, err := store.End(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, string(SceneStateEnded), got.State)
	require.NotNil(t, got.EndedAt, "ended_at should be set")

	reread, err := store.Get(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, got.State, reread.State)
}

func TestSceneStoreEndTransitionsPausedToEnded(t *testing.T) {
	store := newTestStore(t)

	ctx := context.Background()
	row := &SceneRow{
		ID:              "scene-end-paused",
		Title:           "End from paused",
		OwnerID:         "char-alice",
		State:           string(SceneStatePaused),
		PoseOrder:       string(PoseOrderModeFree),
		Visibility:      string(SceneVisibilityOpen),
		ContentWarnings: []string{},
		Tags:            []string{},
	}
	require.NoError(t, store.Create(ctx, row))

	got, err := store.End(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, string(SceneStateEnded), got.State)
	require.NotNil(t, got.EndedAt)
}

func TestSceneStoreEndRejectsAlreadyEnded(t *testing.T) {
	store := newTestStore(t)

	ctx := context.Background()
	row := &SceneRow{
		ID:              "scene-end-twice",
		Title:           "Already ended",
		OwnerID:         "char-alice",
		State:           string(SceneStateEnded),
		PoseOrder:       string(PoseOrderModeFree),
		Visibility:      string(SceneVisibilityOpen),
		ContentWarnings: []string{},
		Tags:            []string{},
	}
	require.NoError(t, store.Create(ctx, row))

	_, err := store.End(ctx, row.ID)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_TRANSITION_FORBIDDEN")
}

func TestSceneStoreEndReturnsNotFoundForMissingScene(t *testing.T) {
	store := newTestStore(t)

	ctx := context.Background()
	_, err := store.End(ctx, "scene-does-not-exist")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_NOT_FOUND")
}

func TestSceneStorePauseTransitionsActiveToPaused(t *testing.T) {
	store := newTestStore(t)

	ctx := context.Background()
	row := &SceneRow{
		ID:              "scene-pause",
		Title:           "Pause from active",
		OwnerID:         "char-alice",
		State:           string(SceneStateActive),
		PoseOrder:       string(PoseOrderModeFree),
		Visibility:      string(SceneVisibilityOpen),
		ContentWarnings: []string{},
		Tags:            []string{},
	}
	require.NoError(t, store.Create(ctx, row))

	got, err := store.Pause(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, string(SceneStatePaused), got.State)
}

func TestSceneStorePauseRejectsAlreadyPaused(t *testing.T) {
	store := newTestStore(t)

	ctx := context.Background()
	row := &SceneRow{
		ID:              "scene-pause-twice",
		Title:           "Already paused",
		OwnerID:         "char-alice",
		State:           string(SceneStatePaused),
		PoseOrder:       string(PoseOrderModeFree),
		Visibility:      string(SceneVisibilityOpen),
		ContentWarnings: []string{},
		Tags:            []string{},
	}
	require.NoError(t, store.Create(ctx, row))

	_, err := store.Pause(ctx, row.ID)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_TRANSITION_FORBIDDEN")
}

func TestSceneStoreResumeTransitionsPausedToActive(t *testing.T) {
	store := newTestStore(t)

	ctx := context.Background()
	row := &SceneRow{
		ID:              "scene-resume",
		Title:           "Resume from paused",
		OwnerID:         "char-alice",
		State:           string(SceneStatePaused),
		PoseOrder:       string(PoseOrderModeFree),
		Visibility:      string(SceneVisibilityOpen),
		ContentWarnings: []string{},
		Tags:            []string{},
	}
	require.NoError(t, store.Create(ctx, row))

	got, err := store.Resume(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, string(SceneStateActive), got.State)
}

func TestSceneStoreResumeRejectsActiveScene(t *testing.T) {
	store := newTestStore(t)

	ctx := context.Background()
	row := &SceneRow{
		ID:              "scene-resume-active",
		Title:           "Already active",
		OwnerID:         "char-alice",
		State:           string(SceneStateActive),
		PoseOrder:       string(PoseOrderModeFree),
		Visibility:      string(SceneVisibilityOpen),
		ContentWarnings: []string{},
		Tags:            []string{},
	}
	require.NoError(t, store.Create(ctx, row))

	_, err := store.Resume(ctx, row.ID)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_TRANSITION_FORBIDDEN")
}

func TestSceneStoreUpdateAppliesTitleOnly(t *testing.T) {
	store := newTestStore(t)

	ctx := context.Background()
	row := &SceneRow{
		ID:              "scene-update-title",
		Title:           "Original",
		Description:     "Original description",
		OwnerID:         "char-alice",
		State:           string(SceneStateActive),
		PoseOrder:       string(PoseOrderModeFree),
		Visibility:      string(SceneVisibilityOpen),
		ContentWarnings: []string{"violence"},
		Tags:            []string{"plot"},
	}
	require.NoError(t, store.Create(ctx, row))

	newTitle := "Renamed"
	update := &SceneUpdate{Title: &newTitle}
	_, err := store.Update(ctx, row.ID, update)
	require.NoError(t, err)

	got, err := store.Get(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, "Renamed", got.Title)
	assert.Equal(t, "Original description", got.Description)
	assert.ElementsMatch(t, []string{"violence"}, got.ContentWarnings)
	assert.ElementsMatch(t, []string{"plot"}, got.Tags)
}

func TestSceneStoreUpdateAppliesMultipleFields(t *testing.T) {
	store := newTestStore(t)

	ctx := context.Background()
	row := &SceneRow{
		ID:              "scene-update-many",
		Title:           "Title 1",
		OwnerID:         "char-alice",
		State:           string(SceneStateActive),
		PoseOrder:       string(PoseOrderModeFree),
		Visibility:      string(SceneVisibilityOpen),
		ContentWarnings: []string{},
		Tags:            []string{},
	}
	require.NoError(t, store.Create(ctx, row))

	title := "Title 2"
	desc := "New description"
	vis := "private"
	update := &SceneUpdate{
		Title:       &title,
		Description: &desc,
		Visibility:  &vis,
	}
	_, err := store.Update(ctx, row.ID, update)
	require.NoError(t, err)

	got, err := store.Get(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, "Title 2", got.Title)
	assert.Equal(t, "New description", got.Description)
	assert.Equal(t, "private", got.Visibility)
}

func TestSceneStoreUpdateRepeatedFieldsRespectFlag(t *testing.T) {
	store := newTestStore(t)

	ctx := context.Background()
	row := &SceneRow{
		ID:              "scene-update-repeated",
		Title:           "T",
		OwnerID:         "char-alice",
		State:           string(SceneStateActive),
		PoseOrder:       string(PoseOrderModeFree),
		Visibility:      string(SceneVisibilityOpen),
		ContentWarnings: []string{"violence"},
		Tags:            []string{"plot", "social"},
	}
	require.NoError(t, store.Create(ctx, row))

	// Only update content_warnings; leave tags alone.
	update := &SceneUpdate{
		ContentWarnings:       []string{"violence", "death"},
		UpdateContentWarnings: true,
		Tags:                  nil,
		UpdateTags:            false, // explicitly NOT updating
	}
	_, err := store.Update(ctx, row.ID, update)
	require.NoError(t, err)

	got, err := store.Get(ctx, row.ID)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"violence", "death"}, got.ContentWarnings)
	assert.ElementsMatch(t, []string{"plot", "social"}, got.Tags, "tags should be unchanged")
}

func TestSceneStoreUpdateClearsRepeatedFieldWithEmptySlice(t *testing.T) {
	store := newTestStore(t)

	ctx := context.Background()
	row := &SceneRow{
		ID:              "scene-update-clear",
		Title:           "T",
		OwnerID:         "char-alice",
		State:           string(SceneStateActive),
		PoseOrder:       string(PoseOrderModeFree),
		Visibility:      string(SceneVisibilityOpen),
		ContentWarnings: []string{"violence"},
		Tags:            []string{},
	}
	require.NoError(t, store.Create(ctx, row))

	update := &SceneUpdate{
		ContentWarnings:       []string{},
		UpdateContentWarnings: true, // explicit clear
	}
	_, err := store.Update(ctx, row.ID, update)
	require.NoError(t, err)

	got, err := store.Get(ctx, row.ID)
	require.NoError(t, err)
	assert.Empty(t, got.ContentWarnings, "content_warnings should be cleared to empty slice")
}

func TestSceneStoreUpdateRejectsEndedScene(t *testing.T) {
	store := newTestStore(t)

	ctx := context.Background()
	row := &SceneRow{
		ID:              "scene-update-ended",
		Title:           "Ended",
		OwnerID:         "char-alice",
		State:           string(SceneStateEnded),
		PoseOrder:       string(PoseOrderModeFree),
		Visibility:      string(SceneVisibilityOpen),
		ContentWarnings: []string{},
		Tags:            []string{},
	}
	require.NoError(t, store.Create(ctx, row))

	title := "Try to rename"
	update := &SceneUpdate{Title: &title}
	_, err := store.Update(ctx, row.ID, update)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_TRANSITION_FORBIDDEN")
}

func TestSceneStoreUpdateReturnsNotFoundForMissingScene(t *testing.T) {
	store := newTestStore(t)

	ctx := context.Background()
	title := "Anything"
	update := &SceneUpdate{Title: &title}
	_, err := store.Update(ctx, "scene-does-not-exist", update)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_NOT_FOUND")
}

func TestSceneStoreUpdateNoFieldsIsNoOp(t *testing.T) {
	store := newTestStore(t)

	ctx := context.Background()
	row := &SceneRow{
		ID:              "scene-update-noop",
		Title:           "Unchanged",
		OwnerID:         "char-alice",
		State:           string(SceneStateActive),
		PoseOrder:       string(PoseOrderModeFree),
		Visibility:      string(SceneVisibilityOpen),
		ContentWarnings: []string{},
		Tags:            []string{},
	}
	require.NoError(t, store.Create(ctx, row))

	// Empty update — no fields specified
	update := &SceneUpdate{}
	_, err := store.Update(ctx, row.ID, update)
	require.NoError(t, err)

	got, err := store.Get(ctx, row.ID)
	require.NoError(t, err)
	assert.Equal(t, "Unchanged", got.Title)
}
