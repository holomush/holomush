// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package content

import "context"

// Store is the interface all content backends implement.
type Store interface {
	// Get retrieves a content item by key. Returns nil, nil if not found.
	Get(ctx context.Context, key string) (*Item, error)

	// List returns content items matching a key prefix with optional pagination.
	List(ctx context.Context, prefix string, opts ListOptions) (*ListResult, error)

	// Put creates or updates a content item.
	Put(ctx context.Context, item *Item) error

	// Delete removes a content item by key.
	Delete(ctx context.Context, key string) error
}
