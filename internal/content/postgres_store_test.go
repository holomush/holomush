// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package content_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/content"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/test/testutil"
)

// setupPool starts a Postgres container, runs migrations, and returns a pool
// with a cleanup function.
func setupPool(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	ctx := context.Background()

	pgEnv, err := testutil.StartPostgres(ctx)
	require.NoError(t, err)

	migrator, err := store.NewMigrator(pgEnv.ConnStr)
	require.NoError(t, err)
	require.NoError(t, migrator.Up())
	_ = migrator.Close()

	pool, err := pgxpool.New(ctx, pgEnv.ConnStr)
	require.NoError(t, err)

	cleanup := func() {
		pool.Close()
		_ = pgEnv.Terminate(ctx)
	}
	return pool, cleanup
}

func TestPostgresStore_PutGet_TextMarkdown(t *testing.T) {
	pool, cleanup := setupPool(t)
	defer cleanup()

	s := content.NewPostgresStore(pool)
	ctx := context.Background()

	item := &content.Item{
		Key:         "guide.intro",
		ContentType: "text/markdown",
		Body:        []byte("# Welcome\n\nHello world."),
		Metadata:    map[string]string{"title": "Introduction"},
	}
	require.NoError(t, s.Put(ctx, item))

	got, err := s.Get(ctx, "guide.intro")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, item.Key, got.Key)
	assert.Equal(t, item.ContentType, got.ContentType)
	assert.Equal(t, item.Body, got.Body)
	assert.Equal(t, item.Metadata, got.Metadata)
	assert.False(t, got.UpdatedAt.IsZero())
}

func TestPostgresStore_PutGet_ApplicationJSON(t *testing.T) {
	pool, cleanup := setupPool(t)
	defer cleanup()

	s := content.NewPostgresStore(pool)
	ctx := context.Background()

	item := &content.Item{
		Key:         "config.settings",
		ContentType: "application/json",
		Body:        []byte(`{"theme":"dark"}`),
		Metadata:    map[string]string{},
	}
	require.NoError(t, s.Put(ctx, item))

	got, err := s.Get(ctx, "config.settings")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, item.ContentType, got.ContentType)
	assert.Equal(t, item.Body, got.Body)
}

func TestPostgresStore_Get_NotFound(t *testing.T) {
	pool, cleanup := setupPool(t)
	defer cleanup()

	s := content.NewPostgresStore(pool)
	ctx := context.Background()

	got, err := s.Get(ctx, "nonexistent.key")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestPostgresStore_Put_Upsert(t *testing.T) {
	pool, cleanup := setupPool(t)
	defer cleanup()

	s := content.NewPostgresStore(pool)
	ctx := context.Background()

	original := &content.Item{
		Key:         "upsert.test",
		ContentType: "text/plain",
		Body:        []byte("original"),
		Metadata:    map[string]string{},
	}
	require.NoError(t, s.Put(ctx, original))

	first, err := s.Get(ctx, "upsert.test")
	require.NoError(t, err)
	require.NotNil(t, first)

	// Small sleep to ensure updated_at changes (1ms resolution at minimum).
	time.Sleep(10 * time.Millisecond)

	updated := &content.Item{
		Key:         "upsert.test",
		ContentType: "text/plain",
		Body:        []byte("updated"),
		Metadata:    map[string]string{"version": "2"},
	}
	require.NoError(t, s.Put(ctx, updated))

	second, err := s.Get(ctx, "upsert.test")
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.Equal(t, []byte("updated"), second.Body)
	assert.Equal(t, map[string]string{"version": "2"}, second.Metadata)
	assert.True(t, second.UpdatedAt.After(first.UpdatedAt) || second.UpdatedAt.Equal(first.UpdatedAt),
		"updated_at should be >= original")
}

func TestPostgresStore_List_WithPrefix(t *testing.T) {
	pool, cleanup := setupPool(t)
	defer cleanup()

	s := content.NewPostgresStore(pool)
	ctx := context.Background()

	items := []*content.Item{
		{Key: "guide.intro", ContentType: "text/markdown", Body: []byte("intro"), Metadata: map[string]string{}},
		{Key: "guide.advanced", ContentType: "text/markdown", Body: []byte("advanced"), Metadata: map[string]string{}},
		{Key: "reference.api", ContentType: "text/markdown", Body: []byte("api"), Metadata: map[string]string{}},
	}
	for _, item := range items {
		require.NoError(t, s.Put(ctx, item))
	}

	result, err := s.List(ctx, "guide.", content.ListOptions{})
	require.NoError(t, err)
	require.Len(t, result.Items, 2)
	assert.Equal(t, "guide.advanced", result.Items[0].Key)
	assert.Equal(t, "guide.intro", result.Items[1].Key)
	assert.Empty(t, result.NextCursor)
}

func TestPostgresStore_List_EmptyPrefix(t *testing.T) {
	pool, cleanup := setupPool(t)
	defer cleanup()

	s := content.NewPostgresStore(pool)
	ctx := context.Background()

	for _, key := range []string{"a.one", "b.two", "c.three"} {
		require.NoError(t, s.Put(ctx, &content.Item{
			Key: key, ContentType: "text/plain", Body: []byte("x"), Metadata: map[string]string{},
		}))
	}

	result, err := s.List(ctx, "", content.ListOptions{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result.Items), 3)
	assert.Empty(t, result.NextCursor)
}

func TestPostgresStore_List_Pagination(t *testing.T) {
	pool, cleanup := setupPool(t)
	defer cleanup()

	s := content.NewPostgresStore(pool)
	ctx := context.Background()

	for i, key := range []string{"page.a", "page.b", "page.c", "page.d"} {
		require.NoError(t, s.Put(ctx, &content.Item{
			Key:         key,
			ContentType: "text/plain",
			Body:        []byte("body"),
			Metadata:    map[string]string{"i": string(rune('0' + i))},
		}))
	}

	// First page
	page1, err := s.List(ctx, "page.", content.ListOptions{Limit: 2})
	require.NoError(t, err)
	require.Len(t, page1.Items, 2)
	assert.Equal(t, "page.a", page1.Items[0].Key)
	assert.Equal(t, "page.b", page1.Items[1].Key)
	assert.Equal(t, "page.b", page1.NextCursor)

	// Second page
	page2, err := s.List(ctx, "page.", content.ListOptions{Limit: 2, Cursor: page1.NextCursor})
	require.NoError(t, err)
	require.Len(t, page2.Items, 2)
	assert.Equal(t, "page.c", page2.Items[0].Key)
	assert.Equal(t, "page.d", page2.Items[1].Key)
	assert.Empty(t, page2.NextCursor)
}

func TestPostgresStore_List_NoMatches(t *testing.T) {
	pool, cleanup := setupPool(t)
	defer cleanup()

	s := content.NewPostgresStore(pool)
	ctx := context.Background()

	result, err := s.List(ctx, "zzz.nonexistent.", content.ListOptions{})
	require.NoError(t, err)
	assert.Empty(t, result.Items)
	assert.Empty(t, result.NextCursor)
}

func TestPostgresStore_Delete(t *testing.T) {
	pool, cleanup := setupPool(t)
	defer cleanup()

	s := content.NewPostgresStore(pool)
	ctx := context.Background()

	require.NoError(t, s.Put(ctx, &content.Item{
		Key:         "delete.me",
		ContentType: "text/plain",
		Body:        []byte("bye"),
		Metadata:    map[string]string{},
	}))

	// Verify it exists
	got, err := s.Get(ctx, "delete.me")
	require.NoError(t, err)
	require.NotNil(t, got)

	// Delete and verify gone
	require.NoError(t, s.Delete(ctx, "delete.me"))
	got, err = s.Get(ctx, "delete.me")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestPostgresStore_Delete_NonExistent(t *testing.T) {
	pool, cleanup := setupPool(t)
	defer cleanup()

	s := content.NewPostgresStore(pool)
	ctx := context.Background()

	// Should not return an error
	err := s.Delete(ctx, "does.not.exist")
	assert.NoError(t, err)
}

func TestPostgresStore_SearchVector_TextMarkdown(t *testing.T) {
	pool, cleanup := setupPool(t)
	defer cleanup()

	s := content.NewPostgresStore(pool)
	ctx := context.Background()

	require.NoError(t, s.Put(ctx, &content.Item{
		Key:         "search.vector.text",
		ContentType: "text/markdown",
		Body:        []byte("The quick brown fox jumps over the lazy dog"),
		Metadata:    map[string]string{},
	}))

	var sv *string
	err := pool.QueryRow(ctx,
		`SELECT search_vector::text FROM content_items WHERE key = $1`,
		"search.vector.text",
	).Scan(&sv)
	require.NoError(t, err)
	require.NotNil(t, sv, "search_vector should be populated for text/markdown")
	assert.NotEmpty(t, *sv)
}

func TestPostgresStore_SearchVector_ImagePNG(t *testing.T) {
	pool, cleanup := setupPool(t)
	defer cleanup()

	s := content.NewPostgresStore(pool)
	ctx := context.Background()

	require.NoError(t, s.Put(ctx, &content.Item{
		Key:         "search.vector.image",
		ContentType: "image/png",
		Body:        []byte{0x89, 0x50, 0x4E, 0x47}, // PNG magic bytes
		Metadata:    map[string]string{},
	}))

	var sv *string
	err := pool.QueryRow(ctx,
		`SELECT search_vector::text FROM content_items WHERE key = $1`,
		"search.vector.image",
	).Scan(&sv)
	require.NoError(t, err)
	assert.Nil(t, sv, "search_vector should be NULL for image/png")
}

func TestPostgresStore_Metadata_RoundTrip(t *testing.T) {
	pool, cleanup := setupPool(t)
	defer cleanup()

	s := content.NewPostgresStore(pool)
	ctx := context.Background()

	meta := map[string]string{
		"title": "My Title",
		"icon":  "star",
		"order": "42",
		"alt":   "some alt text",
	}
	require.NoError(t, s.Put(ctx, &content.Item{
		Key:         "meta.roundtrip",
		ContentType: "text/markdown",
		Body:        []byte("content"),
		Metadata:    meta,
	}))

	got, err := s.Get(ctx, "meta.roundtrip")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, meta, got.Metadata)
}
