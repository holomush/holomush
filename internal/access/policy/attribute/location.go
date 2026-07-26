// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/world"
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
// Returns (nil, nil) for non-location entity types AND for non-ULID IDs
// (notably "location:*" wildcard from bootstrap permission grants).
// See [parseEntityResource] for the three-branch grammar; the wildcard
// bypass exists because the engine evaluates target-type seed matches
// without per-instance attributes (holomush-g776). If a future seed adds
// a `when` clause comparing `resource.location.X` and is expected to
// match the wildcard path, the provider MUST populate sentinel values
// for X (or the seed MUST narrow its target via `resource ==`).
func (p *LocationProvider) ResolveResource(ctx context.Context, resourceID string) (map[string]any, error) {
	id, ok, err := parseEntityResource(resourceID, "location")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
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

	// Per ADR holomush-ti1b (motivating bug holomush-9gtl): when
	// has_owner=false the `owner_id` key MUST be OMITTED from the bag — NOT
	// emitted as an empty-string sentinel. The DSL evaluator's
	// missing-attr-→-false semantics (ADR holomush-iv43 / 0010) then keep
	// every operator false for an unresolved owner. Emitting "" would
	// satisfy `"" == ""` against any other unresolved peer attribute and
	// create a fail-open permit in a default-deny system.
	if loc.OwnerID != nil {
		attrs["owner_id"] = loc.OwnerID.String()
		attrs["has_owner"] = true
	} else {
		attrs["has_owner"] = false
	}

	// Per ADR holomush-ti1b (motivating bug holomush-9gtl): when
	// is_shadow=false the `shadows_id` key MUST be OMITTED — NOT emitted as
	// an empty-string sentinel. Two non-shadow locations carrying "" would
	// compare equal on shadows_id, which is exactly the fail-open equality
	// the missing-attr-→-false semantics (ADR holomush-iv43 / 0010) exist to
	// prevent. The is_shadow witness stays on both branches so a seed can
	// still test existence via `has`.
	if loc.ShadowsID != nil {
		attrs["shadows_id"] = loc.ShadowsID.String()
		attrs["is_shadow"] = true
	} else {
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
