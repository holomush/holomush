// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"

	"github.com/holomush/holomush/internal/access/policy/types"
)

// CommandProvider resolves attributes for command resources.
// Commands use the format "command:<name>" (e.g., "command:say", "command:@dig").
type CommandProvider struct{}

// NewCommandProvider creates a new command attribute provider.
func NewCommandProvider() *CommandProvider {
	return &CommandProvider{}
}

// Namespace returns "command".
func (p *CommandProvider) Namespace() string {
	return "command"
}

// ResolveSubject returns nil — commands are not subjects.
func (p *CommandProvider) ResolveSubject(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}

// ResolveResource resolves command attributes for a resource.
func (p *CommandProvider) ResolveResource(_ context.Context, resourceID string) (map[string]any, error) {
	id, ok := parseEntityID(resourceID, "command")
	if !ok {
		return nil, nil
	}

	return map[string]any{
		"type": "command",
		"name": id,
	}, nil
}

// Schema returns the namespace schema for command attributes.
func (p *CommandProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"type": types.AttrTypeString,
			"name": types.AttrTypeString,
		},
	}
}
