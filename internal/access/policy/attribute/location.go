// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"strings"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/world"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
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
// Returns (nil, nil) for non-location entity types AND for non-ULID IDs.
//
// The canonical non-ULID case is "location:*" — the literal wildcard the
// bootstrap chain emits for type-level capability checks (CreateLocation,
// FindLocationByName). Such checks select seeds via DSL `resource is
// location` (target-type match in engine.findApplicablePolicies, NOT a
// `when`-clause pattern), so they do NOT need per-instance attributes.
// Returning the parse error here would fail-closed the entire bootstrap
// chain (observed in holomush-g776 once this provider was first wired).
//
// CAVEAT: if a future seed adds a `when` clause that compares
// `resource.location.X` and is expected to match the wildcard path, the
// provider MUST populate sentinel values for X (or the seed MUST narrow
// its target via `resource ==`). The bypass below is a target-type-match
// concession, not a generic wildcard facility.
func (p *LocationProvider) ResolveResource(ctx context.Context, resourceID string) (map[string]any, error) {
	parts := strings.SplitN(resourceID, ":", 2)
	if len(parts) != 2 {
		return nil, oops.Code("INVALID_RESOURCE_ID").
			With("resource_id", resourceID).
			Errorf("invalid resource ID format: expected 'type:id'")
	}

	entityType, idStr := parts[0], parts[1]
	if entityType != "location" {
		return nil, nil
	}

	id, err := ulid.Parse(idStr)
	if err != nil {
		// Non-ULID location reference (e.g., "location:*" wildcard from
		// bootstrap permission grants). Skip attribute resolution; the
		// engine evaluates wildcard patterns without per-instance attrs.
		// Returning the parse error here would fail-closed the entire
		// bootstrap chain (observed in holomush-g776).
		return nil, nil //nolint:nilerr // wildcard refs intentionally bypass provider; documented above
	}

	loc, err := p.repo.Get(ctx, id)
	if err != nil {
		return nil, oops.Code("LOCATION_FETCH_FAILED").
			With("location_id", id.String()).
			Wrapf(err, "fetch location %s", id)
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
