// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"strings"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/world"
	"github.com/oklog/ulid/v2"
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
}

// NewCharacterProvider creates a new character attribute provider.
// roleResolver may be nil, in which case all characters default to "player" role.
func NewCharacterProvider(repo world.CharacterRepository, roleResolver RoleResolver) *CharacterProvider {
	return &CharacterProvider{
		repo:         repo,
		roleResolver: roleResolver,
	}
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
func (p *CharacterProvider) resolve(ctx context.Context, entityID string) (map[string]any, error) {
	// Parse entity type and ID
	parts := strings.SplitN(entityID, ":", 2)
	if len(parts) != 2 {
		return nil, oops.
			Code("INVALID_ENTITY_ID").
			With("entity_id", entityID).
			Errorf("invalid subject ID format: expected 'type:id'")
	}

	entityType, idStr := parts[0], parts[1]

	// Return nil for non-character types
	if entityType != "character" {
		return nil, nil
	}

	// Parse ULID
	id, err := ulid.Parse(idStr)
	if err != nil {
		return nil, oops.
			Code("INVALID_CHARACTER_ID").
			With("entity_id", entityID).
			With("id_part", idStr).
			Wrapf(err, "invalid character ID")
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

	// Handle optional location — expose as both "location_id" (raw) and "location" (for seed policies)
	if char.LocationID != nil {
		locStr := char.LocationID.String()
		attrs["location_id"] = locStr
		attrs["location"] = locStr
		attrs["has_location"] = true
	} else {
		attrs["location_id"] = ""
		attrs["location"] = ""
		attrs["has_location"] = false
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
		},
	}
}
