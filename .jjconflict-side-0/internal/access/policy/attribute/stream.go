// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"strings"

	"github.com/holomush/holomush/internal/access/policy/types"
)

// StreamProvider resolves attributes for stream resources.
// Streams use the format "stream:<name>" where name may contain colons
// (e.g., "stream:global", "stream:location:01XYZ").
type StreamProvider struct{}

// NewStreamProvider creates a new stream attribute provider.
func NewStreamProvider() *StreamProvider {
	return &StreamProvider{}
}

// Namespace returns "stream".
func (p *StreamProvider) Namespace() string {
	return "stream"
}

// ResolveSubject returns nil — streams are not subjects.
func (p *StreamProvider) ResolveSubject(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}

// ResolveResource resolves stream attributes for a resource.
func (p *StreamProvider) ResolveResource(_ context.Context, resourceID string) (map[string]any, error) {
	id, ok := parseEntityID(resourceID, "stream")
	if !ok {
		return nil, nil
	}

	attrs := map[string]any{
		"type": "stream",
		"name": id,
	}

	// Extract location ID from "location:ULID" pattern
	if strings.HasPrefix(id, "location:") {
		attrs["location"] = id[len("location:"):]
	}

	return attrs, nil
}

// Schema returns the namespace schema for stream attributes.
func (p *StreamProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"type":     types.AttrTypeString,
			"name":     types.AttrTypeString,
			"location": types.AttrTypeString,
		},
	}
}
