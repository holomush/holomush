// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package content

import (
	"context"
	"path"
	"sort"

	"github.com/samber/oops"
)

// RoutingStore delegates content operations to backend stores based on content type.
// Routes are matched by exact content type or glob pattern (e.g. "image/*").
// If no route matches, the fallback store is used. If no fallback is set and no
// route matches, operations return an error.
type RoutingStore struct {
	routes   map[string]Store
	fallback Store
}

// NewRoutingStore creates a RoutingStore with the given fallback and route map.
// fallback may be nil, in which case unmatched content types return an error.
func NewRoutingStore(fallback Store, routes map[string]Store) *RoutingStore {
	return &RoutingStore{
		routes:   routes,
		fallback: fallback,
	}
}

// resolveStore returns the backend for a given content type.
// Priority: exact match → glob match → fallback → error.
func (r *RoutingStore) resolveStore(contentType string) (Store, error) {
	// Exact match first.
	if s, ok := r.routes[contentType]; ok {
		return s, nil
	}

	// Glob match: iterate deterministically sorted keys.
	keys := make([]string, 0, len(r.routes))
	for k := range r.routes {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, pattern := range keys {
		matched, err := path.Match(pattern, contentType)
		if err != nil {
			return nil, oops.With("pattern", pattern).With("content_type", contentType).Wrap(err)
		}
		if matched {
			return r.routes[pattern], nil
		}
	}

	if r.fallback != nil {
		return r.fallback, nil
	}

	return nil, oops.With("content_type", contentType).
		Errorf("no route matches content type and no fallback configured")
}

// allStores returns all backends in a deterministic order: sorted route values then fallback.
func (r *RoutingStore) allStores() []Store {
	keys := make([]string, 0, len(r.routes))
	for k := range r.routes {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	seen := make(map[Store]struct{})
	stores := make([]Store, 0, len(r.routes)+1)

	for _, k := range keys {
		s := r.routes[k]
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			stores = append(stores, s)
		}
	}

	if r.fallback != nil {
		if _, ok := seen[r.fallback]; !ok {
			stores = append(stores, r.fallback)
		}
	}

	return stores
}

// Put routes the item to the backend matching item.ContentType and delegates.
func (r *RoutingStore) Put(ctx context.Context, item *Item) error {
	s, err := r.resolveStore(item.ContentType)
	if err != nil {
		return err
	}
	if err := s.Put(ctx, item); err != nil {
		return oops.With("content_type", item.ContentType).With("key", item.Key).Wrap(err)
	}
	return nil
}

// Get searches all backends in deterministic order and returns the first hit.
// Returns nil, nil if the key is not found in any backend.
func (r *RoutingStore) Get(ctx context.Context, key string) (*Item, error) {
	for _, s := range r.allStores() {
		item, err := s.Get(ctx, key)
		if err != nil {
			return nil, oops.With("key", key).Wrap(err)
		}
		if item != nil {
			return item, nil
		}
	}
	return nil, nil
}

// Delete removes the key from all backends. Errors from any backend are returned
// immediately; prior successful deletes are not rolled back.
func (r *RoutingStore) Delete(ctx context.Context, key string) error {
	for _, s := range r.allStores() {
		if err := s.Delete(ctx, key); err != nil {
			return oops.With("key", key).Wrap(err)
		}
	}
	return nil
}

// List queries all backends with the given prefix, merges results sorted by key,
// deduplicates (first backend wins), then applies pagination via opts.
func (r *RoutingStore) List(ctx context.Context, prefix string, opts ListOptions) (*ListResult, error) {
	stores := r.allStores()

	// Collect all items from every backend without pagination so we can merge.
	allItems := make(map[string]*Item)
	orderedKeys := make([]string, 0)

	for _, s := range stores {
		result, err := s.List(ctx, prefix, ListOptions{})
		if err != nil {
			return nil, oops.With("prefix", prefix).Wrap(err)
		}
		for _, item := range result.Items {
			if _, exists := allItems[item.Key]; !exists {
				allItems[item.Key] = item
				orderedKeys = append(orderedKeys, item.Key)
			}
		}
	}

	// Sort by key for deterministic order.
	sort.Strings(orderedKeys)

	merged := make([]*Item, 0, len(orderedKeys))
	for _, k := range orderedKeys {
		merged = append(merged, allItems[k])
	}

	// Apply cursor-based pagination.
	start := 0
	if opts.Cursor != "" {
		for i, item := range merged {
			if item.Key == opts.Cursor {
				start = i
				break
			}
		}
	}
	merged = merged[start:]

	nextCursor := ""
	if opts.Limit > 0 && len(merged) > opts.Limit {
		nextCursor = merged[opts.Limit].Key
		merged = merged[:opts.Limit]
	}

	return &ListResult{
		Items:      merged,
		NextCursor: nextCursor,
	}, nil
}
