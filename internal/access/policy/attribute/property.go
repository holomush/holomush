// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"log/slog"
	"time"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/world"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// ParentLocationResolver resolves the parent location for an entity.
// For objects, this walks the containment chain via recursive CTE.
type ParentLocationResolver interface {
	ResolveParentLocation(ctx context.Context, parentType string, parentID ulid.ULID) (*ulid.ULID, error)
}

// PropertyProvider resolves attributes for property entities.
type PropertyProvider struct {
	repo     world.PropertyRepository
	resolver ParentLocationResolver
}

// NewPropertyProvider creates a new property attribute provider.
func NewPropertyProvider(repo world.PropertyRepository, resolver ParentLocationResolver) *PropertyProvider {
	return &PropertyProvider{
		repo:     repo,
		resolver: resolver,
	}
}

// Namespace returns "property".
func (p *PropertyProvider) Namespace() string {
	return "property"
}

// ResolveSubject returns nil, nil — properties are not subjects.
func (p *PropertyProvider) ResolveSubject(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}

// ResolveResource resolves property attributes for a resource.
// Returns nil, nil if the resource is not a property type.
// Resource ID format: "property:01ABC..."
func (p *PropertyProvider) ResolveResource(ctx context.Context, resourceID string) (map[string]any, error) {
	// Parse entity ID using helper from helpers.go
	idStr, ok := parseEntityID(resourceID, "property")
	if !ok {
		// Not a property resource
		return nil, nil
	}

	// Parse ULID
	id, err := ulid.Parse(idStr)
	if err != nil {
		return nil, oops.
			Code("INVALID_PROPERTY_ID").
			With("entity_id", resourceID).
			With("id_part", idStr).
			Wrapf(err, "invalid property ID")
	}

	// Fetch property from repository
	prop, err := p.repo.Get(ctx, id)
	if err != nil {
		return nil, oops.
			Code("PROPERTY_FETCH_FAILED").
			With("property_id", id.String()).
			Wrapf(err, "failed to fetch property")
	}

	// Build base attributes
	attrs := map[string]any{
		"id":          prop.ID.String(),
		"parent_type": prop.ParentType,
		"parent_id":   prop.ParentID.String(),
		"name":        prop.Name,
		"visibility":  prop.Visibility,
	}

	// Handle optional Value
	if prop.Value != nil {
		attrs["value"] = *prop.Value
		attrs["has_value"] = true
	} else {
		attrs["value"] = ""
		attrs["has_value"] = false
	}

	// Handle optional Owner
	if prop.Owner != nil {
		attrs["owner"] = *prop.Owner
		attrs["has_owner"] = true
	} else {
		attrs["owner"] = ""
		attrs["has_owner"] = false
	}

	// Resolve parent_location
	p.resolveParentLocation(ctx, prop, attrs)

	return attrs, nil
}

// resolveParentLocation resolves the parent_location attribute.
// For location parents, parent_location = parent_id.
// For character/object parents, uses the ParentLocationResolver.
// Sets parent_location="" and has_parent_location=false on timeout or error.
func (p *PropertyProvider) resolveParentLocation(ctx context.Context, prop *world.EntityProperty, attrs map[string]any) {
	// If parent is a location, parent_location = parent_id
	if prop.ParentType == "location" {
		attrs["parent_location"] = prop.ParentID.String()
		attrs["has_parent_location"] = true
		return
	}

	// For character/object parents, use resolver with timeout
	resolveCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	locationID, err := p.resolver.ResolveParentLocation(resolveCtx, prop.ParentType, prop.ParentID)
	if err != nil {
		// Log warning and set parent_location as unresolvable
		slog.WarnContext(ctx, "failed to resolve parent location",
			"property_id", prop.ID.String(),
			"parent_type", prop.ParentType,
			"parent_id", prop.ParentID.String(),
			"error", err)
		attrs["parent_location"] = ""
		attrs["has_parent_location"] = false
		return
	}

	if locationID == nil {
		// Unresolvable (e.g., character not in a location)
		attrs["parent_location"] = ""
		attrs["has_parent_location"] = false
		return
	}

	// Successfully resolved
	attrs["parent_location"] = locationID.String()
	attrs["has_parent_location"] = true
}

// Schema returns the namespace schema for property attributes.
func (p *PropertyProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"id":                  types.AttrTypeString,
			"parent_type":         types.AttrTypeString,
			"parent_id":           types.AttrTypeString,
			"name":                types.AttrTypeString,
			"value":               types.AttrTypeString,
			"has_value":           types.AttrTypeBool,
			"owner":               types.AttrTypeString,
			"has_owner":           types.AttrTypeBool,
			"visibility":          types.AttrTypeString,
			"parent_location":     types.AttrTypeString,
			"has_parent_location": types.AttrTypeBool,
		},
	}
}
