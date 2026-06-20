// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"log/slog"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/world"
	"github.com/samber/oops"
)

// RoleResolver resolves roles for subjects.
// This interface allows CharacterProvider to resolve roles without
// coupling to a specific access control implementation.
//
// When GetRoles returns nil or empty, CharacterProvider falls back to
// the default ["player"] role set. Implementors should return a non-empty
// slice only when the subject has explicit role assignments.
type RoleResolver interface {
	// GetRoles returns the roles assigned to a subject, or nil if none.
	// Returning nil/empty is equivalent to no role assignment; CharacterProvider
	// will fall back to the default role set (["player"]).
	GetRoles(ctx context.Context, subject string) []string
}

// CharacterProvider resolves attributes for character entities.
type CharacterProvider struct {
	repo         world.CharacterRepository
	roleResolver RoleResolver
	// kindLookup optionally resolves whether a character's owning player is an
	// ephemeral guest. When nil the provider omits the is_guest key
	// (has_is_guest=false) per the omit-don't-sentinel rule (ADR holomush-ti1b).
	// Shares the PlayerKindLookup type with PlayerAttributeProvider; it is keyed
	// on the character's PlayerID, so the guest gate is reachable from a
	// character: principal (the subject command dispatch evaluates against —
	// player: subjects never reach Layer-1 command auth). Per holomush-5rh.23.
	kindLookup PlayerKindLookup
}

// CharacterProviderOption configures optional behaviour on CharacterProvider at
// construction time.
type CharacterProviderOption func(*CharacterProvider)

// WithCharacterKindLookup supplies an optional guest-lookup keyed on the
// character's owning-player ID. Without it the provider omits is_guest
// (has_is_guest=false) per the omit-don't-sentinel rule (ADR holomush-ti1b).
func WithCharacterKindLookup(fn PlayerKindLookup) CharacterProviderOption {
	return func(p *CharacterProvider) { p.kindLookup = fn }
}

// NewCharacterProvider creates a new character attribute provider.
// roleResolver may be nil, in which case all characters default to "player" role.
// Optional CharacterProviderOption values configure additional behaviour such as
// the guest-kind lookup (WithCharacterKindLookup).
func NewCharacterProvider(repo world.CharacterRepository, roleResolver RoleResolver, opts ...CharacterProviderOption) *CharacterProvider {
	p := &CharacterProvider{
		repo:         repo,
		roleResolver: roleResolver,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Namespace returns "character".
func (p *CharacterProvider) Namespace() string {
	return "character"
}

// ResolveSubject resolves character attributes for a subject.
// Returns nil, nil if the subject is not a character type.
// Subject ID format: "character:01ABC..."
func (p *CharacterProvider) ResolveSubject(ctx context.Context, subjectID string) (map[string]any, error) {
	return p.resolve(ctx, subjectID)
}

// ResolveResource resolves character attributes for a resource.
// Characters can be resources (e.g., checking permissions on another character).
// Returns nil, nil if the resource is not a character type.
// Resource ID format: "character:01ABC..."
func (p *CharacterProvider) ResolveResource(ctx context.Context, resourceID string) (map[string]any, error) {
	return p.resolve(ctx, resourceID)
}

// resolve is the shared implementation for both subject and resource resolution.
//
// Wildcard tolerance ("character:*") is dormant defense-in-depth (no production
// call emits it today, per holomush-xxel) and is supplied uniformly by
// [parseEntityResource].
func (p *CharacterProvider) resolve(ctx context.Context, entityID string) (map[string]any, error) {
	id, ok, err := parseEntityResource(entityID, "character")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	// Fetch character from repository
	char, err := p.repo.Get(ctx, id)
	if err != nil {
		return nil, oops.
			Code("CHARACTER_FETCH_FAILED").
			With("character_id", id.String()).
			Wrapf(err, "failed to fetch character")
	}

	// Resolve roles from role resolver
	var roles []string
	if p.roleResolver != nil {
		subjectID := access.CharacterSubject(char.ID.String())
		roles = p.roleResolver.GetRoles(ctx, subjectID)
	}
	if len(roles) == 0 {
		roles = []string{access.RolePlayer}
	}

	// Map character fields to attributes
	attrs := map[string]any{
		"id":          char.ID.String(),
		"player_id":   char.PlayerID.String(),
		"name":        char.Name,
		"description": char.Description,
		"roles":       roles,
	}

	// Handle optional location — expose as both "location_id" (raw) and "location" (for seed policies).
	//
	// Per ADR holomush-ti1b (motivating bug holomush-9gtl): when has_location=false the `location` and
	// `location_id` keys MUST be OMITTED from the bag (not emitted as
	// empty-string sentinels). This leverages the DSL evaluator's
	// missing-attr-→-false semantics (ADR holomush-iv43 / 0010) to
	// preserve default-deny on colocation seeds when either side is
	// un-locatable. Emitting "" would satisfy `"" == ""` and create a
	// fail-open match (the original 9gtl reproducer).
	if char.LocationID != nil {
		locStr := char.LocationID.String()
		attrs["location_id"] = locStr
		attrs["location"] = locStr
		attrs["has_location"] = true
	} else {
		attrs["has_location"] = false
	}

	// Resolve is_guest for the owning player per ADR holomush-ti1b
	// (omit-don't-sentinel): when has_is_guest=false the is_guest key MUST be
	// OMITTED (not emitted as a false sentinel). The DSL evaluator's
	// missing-attr-→-false comparison semantics then keep the fail-closed
	// scene-command gate (plugins/core-scenes execute-scene-commands:
	// `principal.character.is_guest == false`) denying when guest-ness cannot
	// be determined. Emitting a false sentinel would satisfy `false == false`
	// and let a guest whose lookup failed slip through. Per holomush-5rh.23.
	if p.kindLookup != nil {
		isGuest, lookupErr := p.kindLookup(ctx, char.PlayerID.String())
		if lookupErr != nil {
			// Lookup failure → omit is_guest, emit witness false (fail-safe).
			slog.WarnContext(
				ctx,
				"character kind lookup failed — omitting is_guest attribute (fail-safe)",
				"character_id", id.String(),
				"player_id", char.PlayerID.String(),
				"err", lookupErr,
			)
			attrs["has_is_guest"] = false
		} else {
			attrs["is_guest"] = isGuest
			attrs["has_is_guest"] = true
		}
	} else {
		// No lookup configured → omit is_guest key entirely (ADR holomush-ti1b).
		attrs["has_is_guest"] = false
	}

	return attrs, nil
}

// Schema returns the namespace schema for character attributes.
func (p *CharacterProvider) Schema() *types.NamespaceSchema {
	return &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"id":           types.AttrTypeString,
			"player_id":    types.AttrTypeString,
			"name":         types.AttrTypeString,
			"description":  types.AttrTypeString,
			"roles":        types.AttrTypeStringList,
			"location_id":  types.AttrTypeString,
			"location":     types.AttrTypeString,
			"has_location": types.AttrTypeBool,
			"is_guest":     types.AttrTypeBool,
			"has_is_guest": types.AttrTypeBool,
		},
	}
}
