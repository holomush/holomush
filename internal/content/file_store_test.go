// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package content_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/content"
)

func newStore(t *testing.T) *content.FileStore {
	t.Helper()
	return content.NewFileStore(t.TempDir())
}

// TestFileStore_PutGet verifies a binary round-trip including metadata.
func TestFileStore_PutGet(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	item := &content.Item{
		Key:         "theme.logo",
		ContentType: "image/png",
		Body:        []byte{0x89, 0x50, 0x4E, 0x47}, // PNG magic bytes
		Metadata:    map[string]string{"alt": "Logo image"},
	}
	require.NoError(t, s.Put(ctx, item))

	got, err := s.Get(ctx, "theme.logo")
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, item.Key, got.Key)
	assert.Equal(t, item.ContentType, got.ContentType)
	assert.Equal(t, item.Body, got.Body)
	assert.Equal(t, item.Metadata, got.Metadata)
	assert.False(t, got.UpdatedAt.IsZero())
}

// TestFileStore_KeyToPath verifies dot-separated keys map to nested directories.
func TestFileStore_KeyToPath(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := content.NewFileStore(dir)

	require.NoError(t, s.Put(ctx, &content.Item{
		Key:         "theme.logo",
		ContentType: "image/png",
		Body:        []byte("data"),
	}))

	// Get succeeds only if the file was written at the correct nested path.
	got, err := s.Get(ctx, "theme.logo")
	require.NoError(t, err)
	require.NotNil(t, got)

	// Confirm the expected filesystem path exists.
	_, statErr := os.Stat(dir + "/theme/logo")
	assert.NoError(t, statErr, "expected body file at dir/theme/logo")
}

// TestFileStore_GetMissing returns nil, nil for a non-existent key.
func TestFileStore_GetMissing(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	got, err := s.Get(ctx, "does.not.exist")
	require.NoError(t, err)
	assert.Nil(t, got)
}

// TestFileStore_Delete removes body and sidecar.
func TestFileStore_Delete(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	require.NoError(t, s.Put(ctx, &content.Item{
		Key:  "a.b",
		Body: []byte("hello"),
	}))

	require.NoError(t, s.Delete(ctx, "a.b"))

	got, err := s.Get(ctx, "a.b")
	require.NoError(t, err)
	assert.Nil(t, got)
}

// TestFileStore_DeleteMissing is a no-op for non-existent keys.
func TestFileStore_DeleteMissing(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	assert.NoError(t, s.Delete(ctx, "ghost.key"))
}

// TestFileStore_ListPrefix returns only items matching the prefix.
func TestFileStore_ListPrefix(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	keys := []string{"theme.logo", "theme.banner", "config.main"}
	for _, k := range keys {
		require.NoError(t, s.Put(ctx, &content.Item{Key: k, Body: []byte(k)}))
	}

	result, err := s.List(ctx, "theme", content.ListOptions{})
	require.NoError(t, err)
	require.Len(t, result.Items, 2)
	assert.Equal(t, "theme.banner", result.Items[0].Key)
	assert.Equal(t, "theme.logo", result.Items[1].Key)
}

// TestFileStore_ListPagination verifies cursor and limit behaviour.
func TestFileStore_ListPagination(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	for _, k := range []string{"a.1", "a.2", "a.3", "a.4"} {
		require.NoError(t, s.Put(ctx, &content.Item{Key: k, Body: []byte(k)}))
	}

	// First page: limit 2.
	page1, err := s.List(ctx, "a", content.ListOptions{Limit: 2})
	require.NoError(t, err)
	require.Len(t, page1.Items, 2)
	assert.Equal(t, "a.1", page1.Items[0].Key)
	assert.Equal(t, "a.2", page1.Items[1].Key)
	assert.NotEmpty(t, page1.NextCursor)

	// Second page using cursor.
	page2, err := s.List(ctx, "a", content.ListOptions{Limit: 2, Cursor: page1.NextCursor})
	require.NoError(t, err)
	require.Len(t, page2.Items, 2)
	assert.Equal(t, "a.3", page2.Items[0].Key)
	assert.Equal(t, "a.4", page2.Items[1].Key)
	assert.Empty(t, page2.NextCursor)
}

// TestFileStore_PathTraversalRejected rejects keys that attempt to escape the
// root directory. Because dots are key separators, traversal attempts must use
// embedded slashes or null bytes.
func TestFileStore_PathTraversalRejected(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	tests := []struct {
		name string
		key  string
	}{
		// A key containing an embedded "/" is treated as a relative path by
		// filepath.Join; with enough "../" segments it escapes the root.
		{"slash traversal", "foo/../../etc/passwd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.Put(ctx, &content.Item{Key: tt.key, Body: []byte("x")})
			assert.Error(t, err, "expected error for key %q", tt.key)
		})
	}
}

// TestFileStore_AbsolutePathRejected rejects keys starting with / or \.
func TestFileStore_AbsolutePathRejected(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	for _, key := range []string{"/etc/passwd", "\\windows\\system32"} {
		err := s.Put(ctx, &content.Item{Key: key, Body: []byte("x")})
		assert.Error(t, err, "expected error for key %q", key)
	}
}

// TestFileStore_MetadataRoundTrip verifies all metadata fields survive a
// Put/Get cycle via the sidecar JSON.
func TestFileStore_MetadataRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	meta := map[string]string{
		"alt":   "Hero image",
		"order": "1",
		"title": "Welcome banner",
	}
	require.NoError(t, s.Put(ctx, &content.Item{
		Key:         "landing.hero",
		ContentType: "image/jpeg",
		Body:        []byte{0xFF, 0xD8, 0xFF}, // JPEG magic
		Metadata:    meta,
	}))

	got, err := s.Get(ctx, "landing.hero")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "image/jpeg", got.ContentType)
	assert.Equal(t, meta, got.Metadata)
}
