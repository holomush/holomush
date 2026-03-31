// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package content

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockStore is an in-memory Store for testing.
type mockStore struct {
	items map[string]*Item
	name  string
}

func newMockStore(name string) *mockStore {
	return &mockStore{name: name, items: make(map[string]*Item)}
}

func (m *mockStore) Get(_ context.Context, key string) (*Item, error) {
	return m.items[key], nil
}

func (m *mockStore) List(_ context.Context, prefix string, _ ListOptions) (*ListResult, error) {
	var result []*Item
	for _, item := range m.items {
		if prefix == "" || len(item.Key) >= len(prefix) && item.Key[:len(prefix)] == prefix {
			result = append(result, item)
		}
	}
	return &ListResult{Items: result}, nil
}

func (m *mockStore) Put(_ context.Context, item *Item) error {
	m.items[item.Key] = item
	return nil
}

func (m *mockStore) Delete(_ context.Context, key string) error {
	delete(m.items, key)
	return nil
}

func item(key, contentType string) *Item {
	return &Item{Key: key, ContentType: contentType, Body: []byte("body")}
}

func TestRoutingStore_Put_ExactMatch(t *testing.T) {
	mdStore := newMockStore("markdown")
	rs := NewRoutingStore(nil, map[string]Store{
		"text/markdown": mdStore,
	})

	it := item("landing.hero", "text/markdown")
	require.NoError(t, rs.Put(context.Background(), it))
	assert.Equal(t, it, mdStore.items["landing.hero"])
}

func TestRoutingStore_Put_GlobMatch(t *testing.T) {
	imgStore := newMockStore("image")
	rs := NewRoutingStore(nil, map[string]Store{
		"image/*": imgStore,
	})

	it := item("hero.png", "image/png")
	require.NoError(t, rs.Put(context.Background(), it))
	assert.Equal(t, it, imgStore.items["hero.png"])
}

func TestRoutingStore_Put_Fallback(t *testing.T) {
	fallback := newMockStore("fallback")
	rs := NewRoutingStore(fallback, map[string]Store{
		"text/markdown": newMockStore("markdown"),
	})

	it := item("data.csv", "text/csv")
	require.NoError(t, rs.Put(context.Background(), it))
	assert.Equal(t, it, fallback.items["data.csv"])
}

func TestRoutingStore_Put_ErrorNoFallbackNoMatch(t *testing.T) {
	rs := NewRoutingStore(nil, map[string]Store{
		"text/markdown": newMockStore("markdown"),
	})

	err := rs.Put(context.Background(), item("data.csv", "text/csv"))
	assert.Error(t, err)
}

func TestRoutingStore_Get_FindsInRouteBackend(t *testing.T) {
	mdStore := newMockStore("markdown")
	it := item("page.hero", "text/markdown")
	mdStore.items["page.hero"] = it

	rs := NewRoutingStore(newMockStore("fallback"), map[string]Store{
		"text/markdown": mdStore,
	})

	got, err := rs.Get(context.Background(), "page.hero")
	require.NoError(t, err)
	assert.Equal(t, it, got)
}

func TestRoutingStore_Get_ReturnsNilWhenNotFound(t *testing.T) {
	rs := NewRoutingStore(newMockStore("fallback"), map[string]Store{
		"text/markdown": newMockStore("markdown"),
	})

	got, err := rs.Get(context.Background(), "missing.key")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestRoutingStore_Delete_RemovesFromBackend(t *testing.T) {
	mdStore := newMockStore("markdown")
	mdStore.items["page.body"] = item("page.body", "text/markdown")

	rs := NewRoutingStore(newMockStore("fallback"), map[string]Store{
		"text/markdown": mdStore,
	})

	require.NoError(t, rs.Delete(context.Background(), "page.body"))
	assert.Nil(t, mdStore.items["page.body"])
}

func TestRoutingStore_List_MergesAllBackends(t *testing.T) {
	mdStore := newMockStore("markdown")
	mdStore.items["a"] = item("a", "text/markdown")
	mdStore.items["c"] = item("c", "text/markdown")

	jsonStore := newMockStore("json")
	jsonStore.items["b"] = item("b", "application/json")

	rs := NewRoutingStore(nil, map[string]Store{
		"text/markdown":    mdStore,
		"application/json": jsonStore,
	})

	result, err := rs.List(context.Background(), "", ListOptions{})
	require.NoError(t, err)
	require.Len(t, result.Items, 3)
	assert.Equal(t, "a", result.Items[0].Key)
	assert.Equal(t, "b", result.Items[1].Key)
	assert.Equal(t, "c", result.Items[2].Key)
}

func TestRoutingStore_List_DeduplicatesByKey(t *testing.T) {
	storeA := newMockStore("a")
	storeA.items["shared"] = &Item{Key: "shared", ContentType: "text/plain", Body: []byte("from-a")}

	storeB := newMockStore("b")
	storeB.items["shared"] = &Item{Key: "shared", ContentType: "text/plain", Body: []byte("from-b")}

	// storeA is registered under "text/plain", storeB under "text/html".
	// Both will be queried by List; routes are iterated in sorted key order,
	// so "text/html" (storeB) is queried before "text/plain" (storeA) — storeB wins.
	rs := NewRoutingStore(nil, map[string]Store{
		"text/plain": storeA,
		"text/html":  storeB,
	})

	result, err := rs.List(context.Background(), "", ListOptions{})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	assert.Equal(t, []byte("from-b"), result.Items[0].Body)
}

func TestRoutingStore_List_Pagination(t *testing.T) {
	mdStore := newMockStore("markdown")
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		mdStore.items[k] = item(k, "text/markdown")
	}

	rs := NewRoutingStore(nil, map[string]Store{"text/markdown": mdStore})

	// First page: limit 2
	page1, err := rs.List(context.Background(), "", ListOptions{Limit: 2})
	require.NoError(t, err)
	require.Len(t, page1.Items, 2)
	assert.Equal(t, "a", page1.Items[0].Key)
	assert.Equal(t, "b", page1.Items[1].Key)
	assert.Equal(t, "c", page1.NextCursor)

	// Second page: cursor = "c", limit 2
	page2, err := rs.List(context.Background(), "", ListOptions{Limit: 2, Cursor: page1.NextCursor})
	require.NoError(t, err)
	require.Len(t, page2.Items, 2)
	assert.Equal(t, "c", page2.Items[0].Key)
	assert.Equal(t, "d", page2.Items[1].Key)
	assert.Equal(t, "e", page2.NextCursor)

	// Last page
	page3, err := rs.List(context.Background(), "", ListOptions{Limit: 2, Cursor: page2.NextCursor})
	require.NoError(t, err)
	require.Len(t, page3.Items, 1)
	assert.Equal(t, "e", page3.Items[0].Key)
	assert.Empty(t, page3.NextCursor)
}

func TestRoutingStore_ExactMatchBeforeGlob(t *testing.T) {
	exactStore := newMockStore("exact")
	globStore := newMockStore("glob")

	rs := NewRoutingStore(nil, map[string]Store{
		"text/markdown": exactStore,
		"text/*":        globStore,
	})

	it := item("page", "text/markdown")
	require.NoError(t, rs.Put(context.Background(), it))

	assert.Equal(t, it, exactStore.items["page"])
	assert.Empty(t, globStore.items)
}
