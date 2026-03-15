// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"

	"github.com/holomush/holomush/internal/access/policy/types"
)

// PluginRegistry checks whether a plugin is currently loaded.
type PluginRegistry interface {
	IsPluginLoaded(name string) bool
}

// PluginProvider resolves attributes for plugin subjects.
type PluginProvider struct {
	registry PluginRegistry
}

// NewPluginProvider creates a provider that resolves plugin subject attributes.
func NewPluginProvider(registry PluginRegistry) *PluginProvider {
	return &PluginProvider{registry: registry}
}

// SetRegistry sets the plugin registry for two-phase initialization.
// Called after plugin.Manager is constructed to break the circular
// dependency between Engine and Manager. Safe during startup before
// any concurrent evaluations.
func (p *PluginProvider) SetRegistry(r PluginRegistry) {
	p.registry = r
}

// Namespace returns the attribute namespace for plugin subjects.
func (p *PluginProvider) Namespace() string { return "plugin" }

// ResolveSubject returns plugin attributes from the subject ID.
// Returns nil if the plugin is not loaded (unknown principal).
func (p *PluginProvider) ResolveSubject(_ context.Context, subjectID string) (map[string]any, error) {
	if subjectID == "" {
		return nil, nil
	}
	if p.registry == nil || !p.registry.IsPluginLoaded(subjectID) {
		return nil, nil
	}
	return map[string]any{"name": subjectID}, nil
}

// ResolveResource returns nil — plugins are not a resource type.
func (p *PluginProvider) ResolveResource(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}

// Schema returns the attribute schema for plugin subjects.
func (p *PluginProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"name": types.AttrTypeString,
		},
	}
}
