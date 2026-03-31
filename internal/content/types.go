// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package content provides a general-purpose content store interface.
package content

import "time"

// Item represents a single piece of managed content.
type Item struct {
	Key         string            // hierarchical key, e.g. "landing.hero"
	ContentType string            // IANA media type: "text/markdown", "application/json", etc.
	Body        []byte            // the content
	Metadata    map[string]string // arbitrary k/v (title, icon, order, alt text)
	UpdatedAt   time.Time         // last modification timestamp
}

// ListOptions controls pagination for List queries.
type ListOptions struct {
	Limit  int    // 0 = no limit
	Cursor string // empty = start from beginning
}

// ListResult contains paginated results.
type ListResult struct {
	Items      []*Item
	NextCursor string // empty = no more results
}
