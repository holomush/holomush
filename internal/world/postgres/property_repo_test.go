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
	"github.com/holomush/holomush/pkg/errutil"
)

func newTestProperty(parentType string, parentID ulid.ULID) *world.EntityProperty {
	return &world.EntityProperty{
		ID:         ulid.Make(),
		ParentType: parentType,
		ParentID:   parentID,
		Name:       "test-prop-" + ulid.Make().String(),
		Value:      strPtr("hello"),
		Owner:      strPtr("system"),
		Visibility: "public",
		Flags:      []string{"no-reset"},
		CreatedAt:  time.Now().UTC().Truncate(time.Microsecond),
		UpdatedAt:  time.Now().UTC().Truncate(time.Microsecond),
	}
}

func strPtr(s string) *string { return &s }

func TestPropertyRepository_Create_RoundTrip(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPropertyRepository(testPool)

	locationID := createTestLocation(ctx, t)

	prop := newTestProperty("location", locationID)

	err := repo.Create(ctx, prop)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = repo.Delete(ctx, prop.ID)
	})

	got, err := repo.Get(ctx, prop.ID)
	require.NoError(t, err)

	assert.Equal(t, prop.ID, got.ID)
	assert.Equal(t, prop.ParentType, got.ParentType)
	assert.Equal(t, prop.ParentID, got.ParentID)
	assert.Equal(t, prop.Name, got.Name)
	require.NotNil(t, got.Value)
	assert.Equal(t, *prop.Value, *got.Value)
	require.NotNil(t, got.Owner)
	assert.Equal(t, *prop.Owner, *got.Owner)
	assert.Equal(t, prop.Visibility, got.Visibility)
	assert.Equal(t, prop.Flags, got.Flags)
	assert.Nil(t, got.VisibleTo)
	assert.Nil(t, got.ExcludedFrom)
	assert.False(t, got.CreatedAt.IsZero())
	assert.False(t, got.UpdatedAt.IsZero())
}

func TestPropertyRepository_Create_NilValue(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPropertyRepository(testPool)

	locationID := createTestLocation(ctx, t)

	prop := newTestProperty("location", locationID)
	prop.Value = nil

	err := repo.Create(ctx, prop)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = repo.Delete(ctx, prop.ID)
	})

	got, err := repo.Get(ctx, prop.ID)
	require.NoError(t, err)
	assert.Nil(t, got.Value)
}

func TestPropertyRepository_Get_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPropertyRepository(testPool)

	_, err := repo.Get(ctx, ulid.Make())
	require.Error(t, err)
	assert.ErrorIs(t, err, world.ErrNotFound)
	errutil.AssertErrorCode(t, err, "PROPERTY_NOT_FOUND")
}

func TestPropertyRepository_ListByParent(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPropertyRepository(testPool)

	locationID := createTestLocation(ctx, t)

	prop1 := newTestProperty("location", locationID)
	prop1.Name = "aaa-first"
	prop2 := newTestProperty("location", locationID)
	prop2.Name = "bbb-second"

	require.NoError(t, repo.Create(ctx, prop1))
	require.NoError(t, repo.Create(ctx, prop2))

	t.Cleanup(func() {
		_ = repo.Delete(ctx, prop1.ID)
		_ = repo.Delete(ctx, prop2.ID)
	})

	results, err := repo.ListByParent(ctx, "location", locationID)
	require.NoError(t, err)
	assert.Len(t, results, 2)

	// Verify ordering by name
	assert.Equal(t, "aaa-first", results[0].Name)
	assert.Equal(t, "bbb-second", results[1].Name)
}

func TestPropertyRepository_ListByParent_Empty(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPropertyRepository(testPool)

	results, err := repo.ListByParent(ctx, "location", ulid.Make())
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestPropertyRepository_Update(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPropertyRepository(testPool)

	locationID := createTestLocation(ctx, t)

	prop := newTestProperty("location", locationID)
	require.NoError(t, repo.Create(ctx, prop))

	t.Cleanup(func() {
		_ = repo.Delete(ctx, prop.ID)
	})

	newVal := "updated-value"
	prop.Value = &newVal
	prop.Visibility = "private"
	prop.Flags = []string{"no-reset", "wizard"}

	err := repo.Update(ctx, prop)
	require.NoError(t, err)

	got, err := repo.Get(ctx, prop.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Value)
	assert.Equal(t, "updated-value", *got.Value)
	assert.Equal(t, "private", got.Visibility)
	assert.Equal(t, []string{"no-reset", "wizard"}, got.Flags)
}

func TestPropertyRepository_Update_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPropertyRepository(testPool)

	prop := &world.EntityProperty{
		ID:         ulid.Make(),
		ParentType: "location",
		ParentID:   ulid.Make(),
		Name:       "ghost",
		Visibility: "public",
		Flags:      []string{},
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	err := repo.Update(ctx, prop)
	require.Error(t, err)
	assert.ErrorIs(t, err, world.ErrNotFound)
	errutil.AssertErrorCode(t, err, "PROPERTY_NOT_FOUND")
}

func TestPropertyRepository_Delete(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPropertyRepository(testPool)

	locationID := createTestLocation(ctx, t)

	prop := newTestProperty("location", locationID)
	require.NoError(t, repo.Create(ctx, prop))

	err := repo.Delete(ctx, prop.ID)
	require.NoError(t, err)

	_, err = repo.Get(ctx, prop.ID)
	assert.ErrorIs(t, err, world.ErrNotFound)
}

func TestPropertyRepository_Delete_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPropertyRepository(testPool)

	err := repo.Delete(ctx, ulid.Make())
	require.Error(t, err)
	assert.ErrorIs(t, err, world.ErrNotFound)
	errutil.AssertErrorCode(t, err, "PROPERTY_NOT_FOUND")
}

func TestPropertyRepository_DeleteByParent(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPropertyRepository(testPool)

	locationID := createTestLocation(ctx, t)

	prop1 := newTestProperty("location", locationID)
	prop2 := newTestProperty("location", locationID)

	require.NoError(t, repo.Create(ctx, prop1))
	require.NoError(t, repo.Create(ctx, prop2))

	err := repo.DeleteByParent(ctx, "location", locationID)
	require.NoError(t, err)

	results, err := repo.ListByParent(ctx, "location", locationID)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestPropertyRepository_DeleteByParent_NoRows(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPropertyRepository(testPool)

	err := repo.DeleteByParent(ctx, "location", ulid.Make())
	require.NoError(t, err)
}

func TestPropertyRepository_Visibility_RestrictedDefaults(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPropertyRepository(testPool)

	locationID := createTestLocation(ctx, t)

	prop := newTestProperty("location", locationID)
	prop.Visibility = "restricted"
	prop.VisibleTo = nil
	prop.ExcludedFrom = nil

	err := repo.Create(ctx, prop)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = repo.Delete(ctx, prop.ID)
	})

	got, err := repo.Get(ctx, prop.ID)
	require.NoError(t, err)
	assert.Equal(t, "restricted", got.Visibility)
	require.NotNil(t, got.VisibleTo)
	assert.Equal(t, []string{*prop.Owner}, got.VisibleTo)
	require.NotNil(t, got.ExcludedFrom)
	assert.Empty(t, got.ExcludedFrom)
}

func TestPropertyRepository_Visibility_RestrictedWithExplicitLists(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPropertyRepository(testPool)

	locationID := createTestLocation(ctx, t)

	prop := newTestProperty("location", locationID)
	prop.Visibility = "restricted"
	prop.VisibleTo = []string{"alice", "bob"}
	prop.ExcludedFrom = []string{"charlie"}

	err := repo.Create(ctx, prop)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = repo.Delete(ctx, prop.ID)
	})

	got, err := repo.Get(ctx, prop.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"alice", "bob"}, got.VisibleTo)
	assert.Equal(t, []string{"charlie"}, got.ExcludedFrom)
}

func TestPropertyRepository_Visibility_VisibleToMax100(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPropertyRepository(testPool)

	locationID := createTestLocation(ctx, t)

	prop := newTestProperty("location", locationID)
	prop.Visibility = "restricted"
	prop.ExcludedFrom = []string{}

	// Generate 101 entries
	prop.VisibleTo = make([]string, 101)
	for i := range prop.VisibleTo {
		prop.VisibleTo[i] = ulid.Make().String()
	}

	err := repo.Create(ctx, prop)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PROPERTY_VISIBLE_TO_LIMIT")
}

func TestPropertyRepository_Visibility_ExcludedFromMax100(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPropertyRepository(testPool)

	locationID := createTestLocation(ctx, t)

	prop := newTestProperty("location", locationID)
	prop.Visibility = "restricted"
	prop.VisibleTo = []string{"someone"}

	// Generate 101 entries
	prop.ExcludedFrom = make([]string, 101)
	for i := range prop.ExcludedFrom {
		prop.ExcludedFrom[i] = ulid.Make().String()
	}

	err := repo.Create(ctx, prop)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PROPERTY_EXCLUDED_FROM_LIMIT")
}

func TestPropertyRepository_Visibility_OverlapError(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPropertyRepository(testPool)

	locationID := createTestLocation(ctx, t)

	prop := newTestProperty("location", locationID)
	prop.Visibility = "restricted"
	prop.VisibleTo = []string{"alice", "bob"}
	prop.ExcludedFrom = []string{"bob", "charlie"}

	err := repo.Create(ctx, prop)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PROPERTY_VISIBILITY_OVERLAP")
}

func TestPropertyRepository_ParentNameUniqueness(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPropertyRepository(testPool)

	locationID := createTestLocation(ctx, t)

	prop1 := newTestProperty("location", locationID)
	prop1.Name = "unique-name"
	require.NoError(t, repo.Create(ctx, prop1))

	t.Cleanup(func() {
		_ = repo.Delete(ctx, prop1.ID)
	})

	prop2 := newTestProperty("location", locationID)
	prop2.Name = "unique-name" // same name, same parent

	err := repo.Create(ctx, prop2)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PROPERTY_DUPLICATE_NAME")
}

func TestPropertyRepository_Update_VisibleToMax100(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPropertyRepository(testPool)

	locationID := createTestLocation(ctx, t)

	prop := newTestProperty("location", locationID)
	require.NoError(t, repo.Create(ctx, prop))

	t.Cleanup(func() {
		_ = repo.Delete(ctx, prop.ID)
	})

	prop.Visibility = "restricted"
	prop.ExcludedFrom = []string{}
	prop.VisibleTo = make([]string, 101)
	for i := range prop.VisibleTo {
		prop.VisibleTo[i] = ulid.Make().String()
	}

	err := repo.Update(ctx, prop)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PROPERTY_VISIBLE_TO_LIMIT")
}

func TestPropertyRepository_Update_RestrictedDefaults(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPropertyRepository(testPool)

	locationID := createTestLocation(ctx, t)

	prop := newTestProperty("location", locationID)
	require.NoError(t, repo.Create(ctx, prop))

	t.Cleanup(func() {
		_ = repo.Delete(ctx, prop.ID)
	})

	prop.Visibility = "restricted"
	prop.VisibleTo = nil
	prop.ExcludedFrom = nil

	err := repo.Update(ctx, prop)
	require.NoError(t, err)

	got, err := repo.Get(ctx, prop.ID)
	require.NoError(t, err)
	assert.Equal(t, "restricted", got.Visibility)
	require.NotNil(t, got.VisibleTo)
	assert.Equal(t, []string{*prop.Owner}, got.VisibleTo)
	require.NotNil(t, got.ExcludedFrom)
	assert.Empty(t, got.ExcludedFrom)
}

func TestPropertyRepository_EmptyFlags(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPropertyRepository(testPool)

	locationID := createTestLocation(ctx, t)

	prop := newTestProperty("location", locationID)
	prop.Flags = []string{}

	err := repo.Create(ctx, prop)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = repo.Delete(ctx, prop.ID)
	})

	got, err := repo.Get(ctx, prop.ID)
	require.NoError(t, err)
	assert.Empty(t, got.Flags)
}
