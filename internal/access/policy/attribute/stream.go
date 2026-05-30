// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"strings"

	"github.com/holomush/holomush/internal/access/policy/types"
)

// StreamProvider resolves attributes for stream resources.
// Streams use the format "stream:<name>" where name is a fully-qualified dot
// subject (e.g. "stream:events.<gid>.location.<ULID>").
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

// ResolveResource resolves stream attributes for a resource. The resource ID is
// "stream:<name>" where <name> is a fully-qualified dot subject
// (e.g. "events.<gid>.location.<ULID>"). The location attribute is emitted ONLY
// for location subjects; the has_location witness is always present (true/false)
// per .claude/rules/abac-providers.md (omit value, never sentinel).
func (p *StreamProvider) ResolveResource(_ context.Context, resourceID string) (map[string]any, error) {
	id, ok := parseEntityID(resourceID, "stream")
	if !ok {
		return nil, nil
	}

	attrs := map[string]any{
		"type": "stream",
		"name": id,
	}

	// Location subjects are "events.<gid>.location.<ULID>": parts[2]=="location".
	parts := strings.Split(id, ".")
	if len(parts) == 4 && parts[0] == "events" && parts[2] == "location" && parts[3] != "" {
		attrs["location"] = parts[3]
		attrs["has_location"] = true
	} else {
		attrs["has_location"] = false
		// location key INTENTIONALLY ABSENT (ADR holomush-9gtl fail-safe).
	}

	return attrs, nil
}

// Schema returns the namespace schema for stream attributes.
func (p *StreamProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"type":         types.AttrTypeString,
			"name":         types.AttrTypeString,
			"location":     types.AttrTypeString,
			"has_location": types.AttrTypeBool,
		},
	}
}
