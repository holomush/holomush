// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"

	"github.com/holomush/holomush/internal/access/policy/types"
)

// ExitProvider resolves attributes for exit resources.
// This is a stub that returns only type and ID.
type ExitProvider struct{}

// NewExitProvider creates a new exit attribute provider.
func NewExitProvider() *ExitProvider {
	return &ExitProvider{}
}

// Namespace returns "exit".
func (p *ExitProvider) Namespace() string {
	return "exit"
}

// ResolveSubject returns nil — exits are not subjects.
func (p *ExitProvider) ResolveSubject(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}

// ResolveResource resolves exit attributes for a resource.
func (p *ExitProvider) ResolveResource(_ context.Context, resourceID string) (map[string]any, error) {
	id, ok := parseEntityID(resourceID, "exit")
	if !ok {
		return nil, nil
	}

	return map[string]any{
		"type": "exit",
		"id":   id,
	}, nil
}

// Schema returns the namespace schema for exit attributes.
func (p *ExitProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"type": types.AttrTypeString,
			"id":   types.AttrTypeString,
		},
	}
}
