// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"

	"github.com/holomush/holomush/internal/access/policy/types"
)

// SceneProvider resolves attributes for scene resources.
// This is a stub that returns only type and ID.
type SceneProvider struct{}

// NewSceneProvider creates a new scene attribute provider.
func NewSceneProvider() *SceneProvider {
	return &SceneProvider{}
}

// Namespace returns "scene".
func (p *SceneProvider) Namespace() string {
	return "scene"
}

// ResolveSubject returns nil — scenes are not subjects.
func (p *SceneProvider) ResolveSubject(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}

// ResolveResource resolves scene attributes for a resource.
func (p *SceneProvider) ResolveResource(_ context.Context, resourceID string) (map[string]any, error) {
	id, ok := parseEntityID(resourceID, "scene")
	if !ok {
		return nil, nil
	}

	return map[string]any{
		"type": "scene",
		"id":   id,
	}, nil
}

// Schema returns the namespace schema for scene attributes.
func (p *SceneProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"type": types.AttrTypeString,
			"id":   types.AttrTypeString,
		},
	}
}
