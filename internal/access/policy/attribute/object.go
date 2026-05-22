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

// maxObjectChainDepth bounds the containment-chain walk inside
// resolveEffectiveLocation. The postgres layer enforces a smaller
// nesting depth (DefaultMaxNestingDepth = 3) on writes; the resolver
// uses a more generous bound because (a) it must tolerate any data the
// database might already hold and (b) the per-step cost is one indexed
// primary-key Get. The visited-set in resolveEffectiveLocation guards
// against cycles independently of this cap.
const maxObjectChainDepth = 16

// objectChainTimeout caps total chain-walk wall time so a slow database
// cannot stall ABAC eval indefinitely. Mirrors PropertyProvider's
// 100ms timeout on its resolver call, with extra headroom for the
// (worst-case) multi-step walk through container chains and one
// terminal character.Get.
const objectChainTimeout = 250 * time.Millisecond

// ObjectProvider resolves attributes for object entities.
//
// The effective `location` attribute walks the containment chain:
// LocationID set → that location; HeldByCharacterID → the holder's
// location; ContainedInObjectID → recurse on the container. The walk
// is bounded by maxObjectChainDepth and a visited-set to defend
// against cycles or pathologically deep data.
type ObjectProvider struct {
	repo     world.ObjectRepository
	charRepo world.CharacterRepository
}

// NewObjectProvider creates a new object attribute provider.
//
// charRepo MAY be nil — the provider will tolerate the gap by treating
// held-by-character objects as un-locatable (has_location=false). The
// LocationProvider precedent (holomush-g776) is for repositories to be
// optional with a documented WARN at construction time in BuildABACStack.
func NewObjectProvider(repo world.ObjectRepository, charRepo world.CharacterRepository) *ObjectProvider {
	return &ObjectProvider{repo: repo, charRepo: charRepo}
}

// Namespace returns "object".
func (p *ObjectProvider) Namespace() string {
	return "object"
}

// ResolveSubject returns nil — objects are never subjects.
func (p *ObjectProvider) ResolveSubject(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}

// ResolveResource resolves object attributes for a resource.
// Returns (nil, nil) for non-object entity types AND for non-ULID IDs
// (notably "object:*" — the wildcard CreateObject emits at
// internal/world/service.go:449). See [parseEntityResource] for the
// three-branch grammar; the wildcard bypass exists because the engine
// evaluates target-type seed matches without per-instance attributes
// (holomush-g776).
func (p *ObjectProvider) ResolveResource(ctx context.Context, resourceID string) (map[string]any, error) {
	id, ok, err := parseEntityResource(resourceID, "object")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	// CodeRabbit #2 (PR #4163): bound the top-level Get with the same
	// 250ms budget as the chain walk. Previously only the chain walk
	// (resolveEffectiveLocation) was time-bounded, so a slow DB on the
	// initial lookup could stall ABAC eval beyond the chain's budget.
	getCtx, cancel := context.WithTimeout(ctx, objectChainTimeout)
	defer cancel()

	obj, err := p.repo.Get(getCtx, id)
	if err != nil {
		return nil, oops.Code("OBJECT_FETCH_FAILED").
			With("object_id", id.String()).
			Wrapf(err, "fetch object %s", id)
	}
	// CodeRabbit #3 (PR #4163): defensive guard against the repository
	// contract violation case `(nil, nil)`. The postgres impl at
	// internal/world/postgres/object_repo.go:46-51 returns `(nil, err)`
	// or `(obj, nil)` and never `(nil, nil)`, but the interface contract
	// does not enforce non-nil-on-nil-err. Fail fast with a clear code
	// instead of panicking on the obj.ID dereference below.
	if obj == nil {
		return nil, oops.Code("OBJECT_FETCH_FAILED").
			With("object_id", id.String()).
			Errorf("object repository returned nil with no error")
	}

	attrs := map[string]any{
		"id":           obj.ID.String(),
		"name":         obj.Name,
		"description":  obj.Description,
		"is_container": obj.IsContainer,
	}

	if obj.OwnerID != nil {
		attrs["owner_id"] = obj.OwnerID.String()
		attrs["has_owner"] = true
	} else {
		attrs["owner_id"] = ""
		attrs["has_owner"] = false
	}

	if held := obj.HeldByCharacterID(); held != nil {
		attrs["held_by_character_id"] = held.String()
		attrs["is_held"] = true
	} else {
		attrs["held_by_character_id"] = ""
		attrs["is_held"] = false
	}

	if container := obj.ContainedInObjectID(); container != nil {
		attrs["contained_in_object_id"] = container.String()
		attrs["is_contained"] = true
	} else {
		attrs["contained_in_object_id"] = ""
		attrs["is_contained"] = false
	}

	locStr, ok := p.resolveEffectiveLocation(ctx, obj)
	attrs["location"] = locStr
	attrs["has_location"] = ok

	return attrs, nil
}

// resolveEffectiveLocation walks the containment chain to find the
// terminal location for an object. Returns ("", false) if the chain
// cannot be resolved (cycle detected, exceeded max depth, repo error,
// holder character missing, holder has no location, charRepo nil, etc.).
//
// Bounded by maxObjectChainDepth and a visited-set; total wall time
// bounded by objectChainTimeout via a derived context.
//
// Diagnostic WARNs use the parent ctx so they still emit even when the
// derived resolveCtx has been cancelled by the timeout — otherwise a
// timed-out walk would silently lose both the resolution and the audit
// trail for why (per abac-reviewer #2 on holomush-k3ud).
func (p *ObjectProvider) resolveEffectiveLocation(ctx context.Context, obj *world.Object) (string, bool) {
	resolveCtx, cancel := context.WithTimeout(ctx, objectChainTimeout)
	defer cancel()

	visited := map[ulid.ULID]struct{}{obj.ID: {}}
	cur := obj

	for depth := 0; depth < maxObjectChainDepth; depth++ {
		switch {
		case cur.LocationID() != nil:
			return cur.LocationID().String(), true

		case cur.HeldByCharacterID() != nil:
			if p.charRepo == nil {
				slog.WarnContext(ctx,
					"object provider: charRepo nil — cannot resolve held-by-character location",
					"object_id", obj.ID.String(),
					"character_id", cur.HeldByCharacterID().String())
				return "", false
			}
			char, err := p.charRepo.Get(resolveCtx, *cur.HeldByCharacterID())
			if err != nil {
				slog.WarnContext(ctx,
					"object provider: failed to fetch holder character",
					"object_id", obj.ID.String(),
					"character_id", cur.HeldByCharacterID().String(),
					"error", err)
				return "", false
			}
			// CodeRabbit #3 (PR #4163): defensive guard for the
			// `(nil, nil)` repo contract violation case. The postgres
			// CharacterRepository.Get never returns this, but the
			// interface does not enforce it — fail-closed instead of
			// panicking on char.LocationID below.
			if char == nil {
				slog.WarnContext(ctx,
					"object provider: holder character lookup returned nil",
					"object_id", obj.ID.String(),
					"character_id", cur.HeldByCharacterID().String())
				return "", false
			}
			if char.LocationID == nil {
				return "", false
			}
			return char.LocationID.String(), true

		case cur.ContainedInObjectID() != nil:
			parentID := *cur.ContainedInObjectID()
			if _, seen := visited[parentID]; seen {
				slog.WarnContext(ctx,
					"object provider: cycle detected in containment chain",
					"object_id", obj.ID.String(),
					"cycle_at", parentID.String())
				return "", false
			}
			visited[parentID] = struct{}{}
			parent, err := p.repo.Get(resolveCtx, parentID)
			if err != nil {
				slog.WarnContext(ctx,
					"object provider: failed to fetch parent container",
					"object_id", obj.ID.String(),
					"parent_id", parentID.String(),
					"error", err)
				return "", false
			}
			// CodeRabbit #3 (PR #4163): defensive guard for the
			// `(nil, nil)` repo contract violation case. Parallel to
			// the holder-character guard above.
			if parent == nil {
				slog.WarnContext(ctx,
					"object provider: parent container lookup returned nil",
					"object_id", obj.ID.String(),
					"parent_id", parentID.String())
				return "", false
			}
			cur = parent

		default:
			// Object has no containment set. SetContainment / DB
			// constraints should prevent this; treat as un-locatable.
			return "", false
		}
	}

	slog.WarnContext(ctx,
		"object provider: containment chain exceeded max depth",
		"object_id", obj.ID.String(),
		"max_depth", maxObjectChainDepth)
	return "", false
}

// Schema returns the namespace schema for object attributes.
func (p *ObjectProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"id":                     types.AttrTypeString,
			"name":                   types.AttrTypeString,
			"description":            types.AttrTypeString,
			"owner_id":               types.AttrTypeString,
			"has_owner":              types.AttrTypeBool,
			"location":               types.AttrTypeString,
			"has_location":           types.AttrTypeBool,
			"is_container":           types.AttrTypeBool,
			"held_by_character_id":   types.AttrTypeString,
			"is_held":                types.AttrTypeBool,
			"contained_in_object_id": types.AttrTypeString,
			"is_contained":           types.AttrTypeBool,
		},
	}
}
