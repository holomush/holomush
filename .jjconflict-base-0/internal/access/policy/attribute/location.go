// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"fmt"
	"strings"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/world"
	"github.com/oklog/ulid/v2"
)

// LocationProvider resolves attributes for location entities.
type LocationProvider struct {
	repo world.LocationRepository
}

// NewLocationProvider creates a new location attribute provider.
func NewLocationProvider(repo world.LocationRepository) *LocationProvider {
	return &LocationProvider{repo: repo}
}

// Namespace returns "location".
func (p *LocationProvider) Namespace() string {
	return "location"
}

// ResolveSubject returns nil — locations are not subjects.
func (p *LocationProvider) ResolveSubject(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}

// ResolveResource resolves location attributes for a resource.
func (p *LocationProvider) ResolveResource(ctx context.Context, resourceID string) (map[string]any, error) {
	parts := strings.SplitN(resourceID, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid resource ID format: expected 'type:id'")
	}

	entityType, idStr := parts[0], parts[1]
	if entityType != "location" {
		return nil, nil
	}

	id, err := ulid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid location ID: %w", err)
	}

	loc, err := p.repo.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("fetch location %s: %w", id, err)
	}

	attrs := map[string]any{
		"id":            loc.ID.String(),
		"type":          loc.Type.String(),
		"name":          loc.Name,
		"description":   loc.Description,
		"replay_policy": loc.ReplayPolicy,
		"archived":      loc.ArchivedAt != nil,
	}

	if loc.OwnerID != nil {
		attrs["owner_id"] = loc.OwnerID.String()
		attrs["has_owner"] = true
	} else {
		attrs["owner_id"] = ""
		attrs["has_owner"] = false
	}

	if loc.ShadowsID != nil {
		attrs["shadows_id"] = loc.ShadowsID.String()
		attrs["is_shadow"] = true
	} else {
		attrs["shadows_id"] = ""
		attrs["is_shadow"] = false
	}

	return attrs, nil
}

// Schema returns the namespace schema for location attributes.
func (p *LocationProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"id":            types.AttrTypeString,
			"type":          types.AttrTypeString,
			"name":          types.AttrTypeString,
			"description":   types.AttrTypeString,
			"owner_id":      types.AttrTypeString,
			"has_owner":     types.AttrTypeBool,
			"shadows_id":    types.AttrTypeString,
			"is_shadow":     types.AttrTypeBool,
			"replay_policy": types.AttrTypeString,
			"archived":      types.AttrTypeBool,
		},
	}
}
