// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/content"
	holoGRPC "github.com/holomush/holomush/internal/grpc"
	contentv1 "github.com/holomush/holomush/pkg/proto/holomush/content/v1"
)

// memContentStore is an in-memory content.Store for testing.
type memContentStore struct {
	items map[string]*content.Item
}

func newMemContentStore(items ...*content.Item) *memContentStore {
	m := &memContentStore{items: make(map[string]*content.Item)}
	for _, item := range items {
		m.items[item.Key] = item
	}
	return m
}

func (m *memContentStore) Get(_ context.Context, key string) (*content.Item, error) {
	item, ok := m.items[key]
	if !ok {
		return nil, nil
	}
	return item, nil
}

func (m *memContentStore) List(_ context.Context, prefix string, opts content.ListOptions) (*content.ListResult, error) {
	var matched []*content.Item
	for _, item := range m.items {
		if prefix == "" || len(item.Key) >= len(prefix) && item.Key[:len(prefix)] == prefix {
			matched = append(matched, item)
		}
	}
	// Simple pagination: skip up to cursor (treated as start index string)
	// For tests we keep it straightforward: cursor is unused, just apply limit.
	if opts.Limit > 0 && len(matched) > opts.Limit {
		return &content.ListResult{
			Items:      matched[:opts.Limit],
			NextCursor: "next",
		}, nil
	}
	return &content.ListResult{Items: matched}, nil
}

func (m *memContentStore) Put(_ context.Context, item *content.Item) error {
	m.items[item.Key] = item
	return nil
}

func (m *memContentStore) Delete(_ context.Context, key string) error {
	delete(m.items, key)
	return nil
}

func TestContentServiceServer_GetContent(t *testing.T) {
	item := &content.Item{
		Key:         "landing.hero",
		ContentType: "text/markdown",
		Body:        []byte("# Welcome"),
		Metadata:    map[string]string{"title": "Hero"},
		UpdatedAt:   time.Now(),
	}
	store := newMemContentStore(item)
	svc := holoGRPC.NewContentServiceServer(store)

	resp, err := svc.GetContent(context.Background(), &contentv1.GetContentRequest{Key: "landing.hero"})
	require.NoError(t, err)
	require.NotNil(t, resp.GetItem())
	assert.Equal(t, "landing.hero", resp.GetItem().GetKey())
	assert.Equal(t, "text/markdown", resp.GetItem().GetContentType())
	assert.Equal(t, []byte("# Welcome"), resp.GetItem().GetBody())
	assert.Equal(t, map[string]string{"title": "Hero"}, resp.GetItem().GetMetadata())
}

func TestContentServiceServer_GetContent_NotFound(t *testing.T) {
	store := newMemContentStore()
	svc := holoGRPC.NewContentServiceServer(store)

	_, err := svc.GetContent(context.Background(), &contentv1.GetContentRequest{Key: "missing.key"})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestContentServiceServer_ListContent(t *testing.T) {
	items := []*content.Item{
		{Key: "docs.intro", ContentType: "text/markdown", Body: []byte("Intro")},
		{Key: "docs.guide", ContentType: "text/markdown", Body: []byte("Guide")},
		{Key: "landing.hero", ContentType: "text/html", Body: []byte("<h1>")},
	}
	store := newMemContentStore(items...)
	svc := holoGRPC.NewContentServiceServer(store)

	resp, err := svc.ListContent(context.Background(), &contentv1.ListContentRequest{Prefix: "docs."})
	require.NoError(t, err)
	assert.Len(t, resp.GetItems(), 2)
	assert.Empty(t, resp.GetNextCursor())
}

func TestContentServiceServer_ListContent_Pagination(t *testing.T) {
	items := []*content.Item{
		{Key: "docs.a", ContentType: "text/plain", Body: []byte("a")},
		{Key: "docs.b", ContentType: "text/plain", Body: []byte("b")},
		{Key: "docs.c", ContentType: "text/plain", Body: []byte("c")},
	}
	store := newMemContentStore(items...)
	svc := holoGRPC.NewContentServiceServer(store)

	resp, err := svc.ListContent(context.Background(), &contentv1.ListContentRequest{
		Prefix: "docs.",
		Limit:  2,
	})
	require.NoError(t, err)
	assert.Len(t, resp.GetItems(), 2)
	assert.Equal(t, "next", resp.GetNextCursor())
}
