// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

// SeedPolicy defines a system-installed default policy.
type SeedPolicy struct {
	Name        string
	Description string
	DSLText     string
	SeedVersion int
}

// SeedPolicies returns the complete set of 23 seed policies (22 permit, 1 forbid).
// The initial 18 (T22) plus 5 gap-fill policies (T22b: G1-G4).
// Default deny behavior is provided by EffectDefaultDeny (no matching policy = denied).
// See ADR 087 for rationale on default-deny instead of explicit forbid for system properties.
//
// Attribute paths use fully-qualified namespace.key syntax matching the resolver's
// storage format (e.g., principal.character.role, resource.location.id).
//
// Gap decisions (T22b):
//   - G5 (plugin commands): intentional default-deny; plugins define own policies at install time.
//   - G6 (MoveObject): intentional builder-only; players cannot move objects they don't own.
func SeedPolicies() []SeedPolicy {
	return []SeedPolicy{
		{
			Name:        "seed:player-self-access",
			Description: "Characters can read and write their own character",
			DSLText:     `permit(principal is character, action in ["read", "write"], resource is character) when { resource.character.id == principal.character.id };`,
			SeedVersion: 2,
		},
		{
			Name:        "seed:player-location-read",
			Description: "Characters can read their current location",
			DSLText:     `permit(principal is character, action in ["read"], resource is location) when { resource.location.id == principal.character.location };`,
			SeedVersion: 2,
		},
		{
			Name:        "seed:player-character-colocation",
			Description: "Characters can read co-located characters",
			DSLText:     `permit(principal is character, action in ["read"], resource is character) when { resource.character.location == principal.character.location };`,
			SeedVersion: 2,
		},
		{
			Name:        "seed:player-object-colocation",
			Description: "Characters can read co-located objects",
			DSLText:     `permit(principal is character, action in ["read"], resource is object) when { resource.object.location == principal.character.location };`,
			SeedVersion: 2,
		},
		{
			Name:        "seed:player-stream-emit",
			Description: "Characters can emit to co-located location streams",
			DSLText:     `permit(principal is character, action in ["emit"], resource is stream) when { resource.stream.name like "location:*" && resource.stream.location == principal.character.location };`,
			SeedVersion: 2,
		},
		{
			Name:        "seed:player-movement",
			Description: "Characters can enter any location (restrict via forbid policies)",
			DSLText:     `permit(principal is character, action in ["enter"], resource is location);`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:player-exit-use",
			Description: "Characters can use exits for navigation",
			DSLText:     `permit(principal is character, action in ["use"], resource is exit);`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:player-basic-commands",
			Description: "Characters can execute basic commands",
			DSLText:     `permit(principal is character, action in ["execute"], resource is command) when { resource.command.name in ["say", "pose", "look", "go"] };`,
			SeedVersion: 2,
		},
		{
			Name:        "seed:builder-location-write",
			Description: "Builders and admins can create/modify/delete locations",
			DSLText:     `permit(principal is character, action in ["write", "delete"], resource is location) when { principal.character.role in ["builder", "admin"] };`,
			SeedVersion: 2,
		},
		{
			Name:        "seed:builder-object-write",
			Description: "Builders and admins can create/modify/delete objects",
			DSLText:     `permit(principal is character, action in ["write", "delete"], resource is object) when { principal.character.role in ["builder", "admin"] };`,
			SeedVersion: 2,
		},
		{
			Name:        "seed:builder-commands",
			Description: "Builders and admins can execute builder commands",
			DSLText:     `permit(principal is character, action in ["execute"], resource is command) when { principal.character.role in ["builder", "admin"] && resource.command.name in ["dig", "create", "describe", "link"] };`,
			SeedVersion: 2,
		},
		{
			Name:        "seed:admin-full-access",
			Description: "Admins have full access to everything",
			DSLText:     `permit(principal is character, action, resource) when { principal.character.role == "admin" };`,
			SeedVersion: 2,
		},
		{
			Name:        "seed:property-public-read",
			Description: "Public properties readable by co-located characters",
			DSLText:     `permit(principal is character, action in ["read"], resource is property) when { resource.property.visibility == "public" && principal.character.location == resource.property.parent_location };`,
			SeedVersion: 2,
		},
		{
			Name:        "seed:property-private-read",
			Description: "Private properties readable only by owner",
			DSLText:     `permit(principal is character, action in ["read"], resource is property) when { resource.property.visibility == "private" && resource.property.owner == principal.character.id };`,
			SeedVersion: 2,
		},
		{
			Name:        "seed:property-admin-read",
			Description: "Admin properties readable only by admins",
			DSLText:     `permit(principal is character, action in ["read"], resource is property) when { resource.property.visibility == "admin" && principal.character.role == "admin" };`,
			SeedVersion: 2,
		},
		{
			Name:        "seed:property-owner-write",
			Description: "Property owners can write and delete their properties",
			DSLText:     `permit(principal is character, action in ["write", "delete"], resource is property) when { resource.property.owner == principal.character.id };`,
			SeedVersion: 2,
		},
		{
			Name:        "seed:property-restricted-visible-to",
			Description: "Restricted properties readable by characters in the visible_to list",
			DSLText:     `permit(principal is character, action in ["read"], resource is property) when { resource.property.visibility == "restricted" && resource has property.visible_to && principal.character.id in resource.property.visible_to };`,
			SeedVersion: 2,
		},
		{
			Name:        "seed:property-restricted-excluded",
			Description: "Restricted properties denied to characters in the excluded_from list",
			DSLText:     `forbid(principal is character, action in ["read"], resource is property) when { resource.property.visibility == "restricted" && resource has property.excluded_from && principal.character.id in resource.property.excluded_from };`,
			SeedVersion: 2,
		},

		// --- Gap-fill policies (T22b) ---

		// G1: Players can read exits (target matching only; exit provider is a stub).
		{
			Name:        "seed:player-exit-read",
			Description: "Characters can read exits for navigation",
			DSLText:     `permit(principal is character, action in ["read"], resource is exit);`,
			SeedVersion: 1,
		},
		// G2: Builders can create/update/delete exits.
		{
			Name:        "seed:builder-exit-write",
			Description: "Builders and admins can create/modify/delete exits",
			DSLText:     `permit(principal is character, action in ["write", "delete"], resource is exit) when { principal.character.role in ["builder", "admin"] };`,
			SeedVersion: 2,
		},
		// G3: Players can list characters in their current location (ADR #109).
		{
			Name:        "seed:player-location-list-characters",
			Description: "Characters can list other characters in their current location",
			DSLText:     `permit(principal is character, action in ["list_characters"], resource is location) when { resource.location.id == principal.character.location };`,
			SeedVersion: 2,
		},
		// G4: Scene participant access (target matching only; scene provider is a stub).
		{
			Name:        "seed:player-scene-participant",
			Description: "Characters can join and leave scenes",
			DSLText:     `permit(principal is character, action in ["write"], resource is scene);`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:player-scene-read",
			Description: "Characters can view scenes",
			DSLText:     `permit(principal is character, action in ["read"], resource is scene);`,
			SeedVersion: 1,
		},
	}
}
