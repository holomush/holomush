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

// SeedPolicies returns the complete set of 18 seed policies (17 permit, 1 forbid).
// Default deny behavior is provided by EffectDefaultDeny (no matching policy = denied).
// See ADR 087 for rationale on default-deny instead of explicit forbid for system properties.
func SeedPolicies() []SeedPolicy {
	return []SeedPolicy{
		{
			Name:        "seed:player-self-access",
			Description: "Characters can read and write their own character",
			DSLText:     `permit(principal is character, action in ["read", "write"], resource is character) when { resource.id == principal.id };`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:player-location-read",
			Description: "Characters can read their current location",
			DSLText:     `permit(principal is character, action in ["read"], resource is location) when { resource.id == principal.location };`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:player-character-colocation",
			Description: "Characters can read co-located characters",
			DSLText:     `permit(principal is character, action in ["read"], resource is character) when { resource.location == principal.location };`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:player-object-colocation",
			Description: "Characters can read co-located objects",
			DSLText:     `permit(principal is character, action in ["read"], resource is object) when { resource.location == principal.location };`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:player-stream-emit",
			Description: "Characters can emit to co-located location streams",
			DSLText:     `permit(principal is character, action in ["emit"], resource is stream) when { resource.name like "location:*" && resource.location == principal.location };`,
			SeedVersion: 1,
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
			DSLText:     `permit(principal is character, action in ["execute"], resource is command) when { resource.name in ["say", "pose", "look", "go"] };`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:builder-location-write",
			Description: "Builders and admins can create/modify/delete locations",
			DSLText:     `permit(principal is character, action in ["write", "delete"], resource is location) when { principal.role in ["builder", "admin"] };`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:builder-object-write",
			Description: "Builders and admins can create/modify/delete objects",
			DSLText:     `permit(principal is character, action in ["write", "delete"], resource is object) when { principal.role in ["builder", "admin"] };`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:builder-commands",
			Description: "Builders and admins can execute builder commands",
			DSLText:     `permit(principal is character, action in ["execute"], resource is command) when { principal.role in ["builder", "admin"] && resource.name in ["dig", "create", "describe", "link"] };`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:admin-full-access",
			Description: "Admins have full access to everything",
			DSLText:     `permit(principal is character, action, resource) when { principal.role == "admin" };`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:property-public-read",
			Description: "Public properties readable by co-located characters",
			DSLText:     `permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "public" && principal.location == resource.parent_location };`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:property-private-read",
			Description: "Private properties readable only by owner",
			DSLText:     `permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "private" && resource.owner == principal.id };`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:property-admin-read",
			Description: "Admin properties readable only by admins",
			DSLText:     `permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "admin" && principal.role == "admin" };`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:property-owner-write",
			Description: "Property owners can write and delete their properties",
			DSLText:     `permit(principal is character, action in ["write", "delete"], resource is property) when { resource.owner == principal.id };`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:property-restricted-visible-to",
			Description: "Restricted properties readable by characters in the visible_to list",
			DSLText:     `permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "restricted" && resource has visible_to && principal.id in resource.visible_to };`,
			SeedVersion: 1,
		},
		{
			Name:        "seed:property-restricted-excluded",
			Description: "Restricted properties denied to characters in the excluded_from list",
			DSLText:     `forbid(principal is character, action in ["read"], resource is property) when { resource.visibility == "restricted" && resource has excluded_from && principal.id in resource.excluded_from };`,
			SeedVersion: 1,
		},
	}
}
