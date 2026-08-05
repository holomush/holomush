// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"log/slog"
	"sort"
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

// CharacterOwnerResolver resolves, for every player owning at least one of the
// supplied character ids, that player's COMPLETE character-id set — keyed by
// player id.
//
// Declared here as a CONSUMER-SIDE interface (exactly as ParentLocationResolver
// is) rather than imported from internal/world/postgres, so this package does
// not depend on a storage package. The production implementation is
// postgres.NewCharacterOwnerResolver(pool).
//
// The complete set — not a flat list of owning players — is what the permit-side
// ALL direction needs; see resolveDerivedPlayerPeers.
type CharacterOwnerResolver interface {
	ResolveOwnerScopes(ctx context.Context, characterIDs []string) (map[string][]string, error)
}

// PropertyProvider resolves attributes for property entities.
type PropertyProvider struct {
	repo          world.PropertyRepository
	resolver      ParentLocationResolver
	ownerResolver CharacterOwnerResolver
}

// NewPropertyProvider creates a new property attribute provider.
//
// ownerResolver may be nil: the provider then emits no derived player-keyed
// peers at all (each has_X witness false, each value key omitted), which the
// omit branches already handle.
func NewPropertyProvider(
	repo world.PropertyRepository,
	resolver ParentLocationResolver,
	ownerResolver CharacterOwnerResolver,
) *PropertyProvider {
	return &PropertyProvider{
		repo:          repo,
		resolver:      resolver,
		ownerResolver: ownerResolver,
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

	// Handle optional Value.
	//
	// Per ADR holomush-ti1b (motivating bug holomush-9gtl): when
	// has_value=false the `value` key MUST be OMITTED from the bag — NOT
	// emitted as an empty-string sentinel. The DSL evaluator's
	// missing-attr-→-false semantics (ADR holomush-iv43 / 0010) then keep
	// every operator false for an unset value. Emitting "" would satisfy
	// `"" == ""` against any other unresolved peer attribute and create a
	// fail-open permit in a default-deny system.
	if prop.Value != nil {
		attrs["value"] = *prop.Value
		attrs["has_value"] = true
	} else {
		attrs["has_value"] = false
	}

	// Handle optional Owner.
	//
	// Per ADR holomush-ti1b (motivating bug holomush-9gtl): when
	// has_owner=false the `owner` key MUST be OMITTED — NOT emitted as an
	// empty-string sentinel. This one is directly load-bearing:
	// seed:property-private-read and seed:property-owner-write both gate on
	// `resource.property.owner == principal.character.id` (see
	// internal/access/policy/seed.go), so an ownerless property MUST NOT be
	// comparable at all. The has_owner witness stays on both branches.
	if prop.Owner != nil {
		attrs["owner"] = *prop.Owner
		attrs["has_owner"] = true
	} else {
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

	// Resolve the player-keyed peers of the three character-keyed row fields.
	p.resolveDerivedPlayerPeers(ctx, prop, attrs)

	return attrs, nil
}

// resolveDerivedPlayerPeers emits owner_player_id, visible_to_players and
// excluded_from_players — the PLAYER-keyed peers of the row's CHARACTER-keyed
// owner / visible_to / excluded_from fields — each with a has_X witness on every
// code path.
//
// # Why these exist at all
//
// Do NOT delete these as redundant duplication of owner/visible_to. A `viewer:`
// subject (01-SPEC §8.4.1) is PLAYER-flavored; `owner`, `visible_to` and
// `excluded_from` hold CHARACTER ids. Comparing a player id to a character id
// never matches, and a non-matching key evaluates FALSE
// (internal/access/policy/dsl/evaluator.go), so every private/restricted/admin
// field would go permanently invisible — fail-closed, with no error and no
// failing test.
//
// The obvious alternative — put the viewer's character ids in the SUBJECT bag —
// is only half expressible in the shipped DSL, and the half it cannot express is
// the half that matters. `X in Y` (evalInExpr) requires a SCALAR left operand and
// a slice right operand, so `resource.property.owner in principal.viewer.character_ids`
// would work for ownership; but visible_to/excluded_from need "any of my
// characters is in this list", a list-to-list intersection.
// containsAll/containsAny take a LITERAL needle list (ContainsCondition.List),
// never an attribute reference. There is NO DSL expression that intersects two
// attribute lists. So the character→player relation is resolved server-side,
// here, where every other cross-entity resolution in this file already happens
// (ParentLocationResolver is the precedent), and the policy compares player to
// player.
//
// # Why the two sides lean OPPOSITE ways (02-CONTEXT.md D-27)
//
// The asymmetry is deliberate and looks like a bug to anyone who has not read
// D-27. Each peer is computed in the direction that CANNOT WIDEN ITS OWN
// POLICY'S EFFECT, and permits and forbids widen in opposite directions:
//
//   - owner_player_id and visible_to_players feed PERMITS → the ALL direction. A
//     player enters only when EVERY character of theirs appears in the row's
//     field. For the single-valued owner that reduces to "the owning character is
//     that player's only character".
//   - excluded_from_players feeds a FORBID → the ANY direction. A player enters
//     when ANY character of theirs appears in excluded_from, because losing an
//     exclusion is the widening on that side.
//
// The plain player union ("a player is a member iff ANY of their characters is")
// was PROPOSED FOR BOTH SIDES and DELIBERATELY DECLINED for the permit side: it
// broadens a row-level grant an author made to a NAMED character into a grant to
// the human behind it, reachable from every one of that player's other
// characters. That is an authorization widening no artifact had decided, proposed
// inside the phase whose subject is privacy. Do not "simplify" the ALL branch
// into an ANY branch for symmetry — that one-line diff reads as cleanup and
// reintroduces the widening. See 02-CONTEXT.md D-27.
//
// # What these are NOT
//
// Every peer is derived from the ROW's own fields — prop.Owner, prop.VisibleTo,
// prop.ExcludedFrom — and NEVER from the requesting subject. A peer computed
// from the caller would make every row match its own reader: a total
// authorization bypass that reads as a working feature.
//
// On resolver error all THREE keys are omitted together, never a partial set: a
// bag carrying visible_to_players but not owner_player_id because one lookup
// failed would let a `restricted` twin permit while the `private` twin denies for
// the same outage — a verdict that depends on which query failed.
func (p *PropertyProvider) resolveDerivedPlayerPeers(
	ctx context.Context, prop *world.EntityProperty, attrs map[string]any,
) {
	// Collect every character id the ROW references, so all three peers resolve
	// from ONE ResolveOwnerScopes call — a row with a large visible_to list
	// costs one query, not N.
	refs := make([]string, 0, len(prop.VisibleTo)+len(prop.ExcludedFrom)+1)
	seen := make(map[string]struct{}, cap(refs))
	addRef := func(id string) {
		if id == "" {
			return
		}
		if _, dup := seen[id]; dup {
			return
		}
		seen[id] = struct{}{}
		refs = append(refs, id)
	}
	if prop.Owner != nil {
		addRef(*prop.Owner)
	}
	for _, id := range prop.VisibleTo {
		addRef(id)
	}
	for _, id := range prop.ExcludedFrom {
		addRef(id)
	}

	if p.ownerResolver == nil || len(refs) == 0 {
		omitDerivedPlayerPeers(attrs)
		return
	}

	scopes, err := p.ownerResolver.ResolveOwnerScopes(ctx, refs)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve character owner scopes — omitting all derived player peers",
			"property_id", prop.ID.String(),
			"character_ref_count", len(refs),
			"error", err)
		omitDerivedPlayerPeers(attrs)
		return
	}

	// owner_player_id — permit side, ALL direction.
	if prop.Owner != nil {
		owners := playersWhoseEveryCharacterIsIn(scopes, []string{*prop.Owner})
		if len(owners) == 1 {
			attrs["owner_player_id"] = owners[0]
			attrs["has_owner_player_id"] = true
		} else {
			// Either the owning character has no resolvable player, or its
			// player holds a second character the row did not name. An
			// unresolvable owner is NOT an ownerless row: the character-keyed
			// `owner`/`has_owner` pair above stays untouched so a policy can
			// still tell the two cases apart.
			attrs["has_owner_player_id"] = false
		}
	} else {
		attrs["has_owner_player_id"] = false
	}

	// visible_to_players — permit side, ALL direction.
	emitDerivedPlayerList(attrs, "visible_to_players",
		playersWhoseEveryCharacterIsIn(scopes, prop.VisibleTo))

	// excluded_from_players — forbid side, ANY direction.
	emitDerivedPlayerList(attrs, "excluded_from_players",
		playersWithAnyCharacterIn(scopes, prop.ExcludedFrom))
}

// omitDerivedPlayerPeers emits all three witnesses false and omits all three
// value keys — the shape every unresolved path produces.
func omitDerivedPlayerPeers(attrs map[string]any) {
	attrs["has_owner_player_id"] = false
	attrs["has_visible_to_players"] = false
	attrs["has_excluded_from_players"] = false
}

// emitDerivedPlayerList emits key + has_key, OMITTING key entirely when no
// player qualifies.
//
// Per ADR holomush-ti1b an empty list is the list-flavored empty-string
// sentinel: a RESOLVED value that a containsAny-shaped condition would evaluate
// against. Absence is what keeps the DSL's missing-attr→false semantics intact.
func emitDerivedPlayerList(attrs map[string]any, key string, players []string) {
	if len(players) == 0 {
		attrs["has_"+key] = false
		return
	}
	sort.Strings(players)
	out := make([]any, len(players))
	for i, pid := range players {
		out[i] = pid
	}
	attrs[key] = out
	attrs["has_"+key] = true
}

// playersWhoseEveryCharacterIsIn implements the PERMIT-side ALL direction: a
// player qualifies only when EVERY character in their complete scope set appears
// in field. See resolveDerivedPlayerPeers for why this is not a union (D-27).
func playersWhoseEveryCharacterIsIn(scopes map[string][]string, field []string) []string {
	if len(field) == 0 {
		return nil
	}
	member := make(map[string]struct{}, len(field))
	for _, id := range field {
		member[id] = struct{}{}
	}

	out := []string{}
	for playerID, chars := range scopes {
		if len(chars) == 0 {
			// A player with no characters cannot vacuously qualify for a
			// permit; the resolver never returns this, and it must not become
			// a permit if it ever does.
			continue
		}
		all := true
		for _, c := range chars {
			if _, ok := member[c]; !ok {
				all = false
				break
			}
		}
		if all {
			out = append(out, playerID)
		}
	}
	return out
}

// playersWithAnyCharacterIn implements the FORBID-side ANY direction: a player
// qualifies when ANY character of theirs appears in field, so an exclusion is
// never lost. Do NOT "fix" this into an ALL rule for symmetry with the permit
// side — that would let a player evade a forbid by holding a second, unlisted
// character (threat T-02-86).
func playersWithAnyCharacterIn(scopes map[string][]string, field []string) []string {
	if len(field) == 0 {
		return nil
	}
	member := make(map[string]struct{}, len(field))
	for _, id := range field {
		member[id] = struct{}{}
	}

	out := []string{}
	for playerID, chars := range scopes {
		for _, c := range chars {
			if _, ok := member[c]; ok {
				out = append(out, playerID)
				break
			}
		}
	}
	return out
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
			// Player-keyed peers DERIVED from the three character-keyed fields
			// above (see resolveDerivedPlayerPeers, and 02-CONTEXT.md D-27 for
			// the derivation direction). They exist because a `viewer:` subject
			// is player-flavored and the DSL cannot intersect two attribute
			// lists. Declaring them here is load-bearing: the resolver DROPS
			// and counts any emitted key not in the schema, so an undeclared
			// owner_player_id would be silently absent — the same invisible
			// fail-closed the peers exist to remove.
			"owner_player_id":           types.AttrTypeString,
			"has_owner_player_id":       types.AttrTypeBool,
			"visible_to_players":        types.AttrTypeStringList,
			"has_visible_to_players":    types.AttrTypeBool,
			"excluded_from_players":     types.AttrTypeStringList,
			"has_excluded_from_players": types.AttrTypeBool,
		},
	}
}
