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
// Returns nil, nil for non-property entity types AND for non-ULID IDs
// (wildcard tolerance — see [parseEntityResource]). The wildcard
// tolerance is dormant defense-in-depth for PropertyProvider today:
// production callers always emit a fully-qualified ULID. The unified
// helper extends the same fail-open behavior here as Location, Character,
// and Object — see holomush-o8g6.
//
// BEHAVIOR CHANGE (holomush-o8g6): previously a property resource ID
// with a non-ULID ID part (e.g. "property:not-a-ulid") returned an
// oops.Code("INVALID_PROPERTY_ID") error. After the helper unification
// it returns (nil, nil) — consistent with the three peer providers and
// with the holomush-g776 wildcard-tolerance pattern. No production
// caller depends on the old error code (verified via rg).
func (p *PropertyProvider) ResolveResource(ctx context.Context, resourceID string) (map[string]any, error) {
	id, ok, err := parseEntityResource(resourceID, "property")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
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

	// Emit visible_to and excluded_from for restricted-visibility seeds.
	// seed:property-restricted-visible-to gates on `resource has property.visible_to`
	// and seed:property-restricted-excluded gates on `resource has property.excluded_from`.
	// Without these entries in the attribute bag, both seeds silently skip (the `has`
	// check short-circuits to false) regardless of the stored lists.
	// Only emit when non-nil/non-empty — the `resource has property.visible_to` DSL
	// expression mirrors the ti1b pattern: omit the key entirely when the list is
	// absent so the `has` guard evaluates to false (default-deny preserving).
	if len(prop.VisibleTo) > 0 {
		vt := make([]any, len(prop.VisibleTo))
		for i, s := range prop.VisibleTo {
			vt[i] = s
		}
		attrs["visible_to"] = vt
	}
	if len(prop.ExcludedFrom) > 0 {
		ef := make([]any, len(prop.ExcludedFrom))
		for i, s := range prop.ExcludedFrom {
			ef[i] = s
		}
		attrs["excluded_from"] = ef
	}

	// Resolve parent_location
	p.resolveParentLocation(ctx, prop, attrs)

	return attrs, nil
}

// resolveParentLocation resolves the parent_location attribute.
// For location parents, parent_location = parent_id.
// For character/object parents, uses the ParentLocationResolver.
//
// Per ADR holomush-ti1b (motivating bug holomush-9gtl): when the parent location cannot be resolved
// (timeout, error, character without location, etc.), the
// `parent_location` key MUST be OMITTED from the bag — NOT emitted as
// an empty-string sentinel. This leverages the DSL evaluator's
// missing-attr-→-false semantics (ADR holomush-iv43 / 0010) to preserve
// default-deny on seed:property-public-read when either side is
// un-locatable. has_parent_location stays as a boolean witness so the
// DSL can still check existence via `has` if a future seed needs it.
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
		// Log warning and omit parent_location key (un-locatable parent)
		slog.WarnContext(ctx, "failed to resolve parent location",
			"property_id", prop.ID.String(),
			"parent_type", prop.ParentType,
			"parent_id", prop.ParentID.String(),
			"error", err)
		attrs["has_parent_location"] = false
		return
	}

	if locationID == nil {
		// Unresolvable (e.g., character not in a location)
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
			// visible_to and excluded_from are populated for restricted-visibility
			// properties and used by seed:property-restricted-visible-to and
			// seed:property-restricted-excluded. Omitted from the bag (and thus
			// from `resource has property.visible_to`) when the lists are empty,
			// matching the ti1b omit-when-unresolvable pattern.
			"visible_to":    types.AttrTypeStringList,
			"excluded_from": types.AttrTypeStringList,
		},
	}
}
