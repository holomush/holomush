// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dsl_test

import (
	"testing"

	"github.com/holomush/holomush/internal/access/policy/dsl"
)

// FuzzParse tests the parser against arbitrary input to ensure it never panics.
func FuzzParse(f *testing.F) {
	// Seed corpus: all 18 seed policies from the spec
	seeds := []string{
		// Seed policies 1-18
		`permit(principal is character, action in ["read", "write"], resource is character) when { resource.id == principal.id };`,
		`permit(principal is character, action in ["read"], resource is location) when { resource.id == principal.location };`,
		`permit(principal is character, action in ["read"], resource is character) when { resource.location == principal.location };`,
		`permit(principal is character, action in ["read"], resource is object) when { resource.location == principal.location };`,
		`permit(principal is character, action in ["emit"], resource is stream) when { resource.name like "location:*" && resource.location == principal.location };`,
		`permit(principal is character, action in ["enter"], resource is location);`,
		`permit(principal is character, action in ["use"], resource is exit);`,
		`permit(principal is character, action in ["execute"], resource is command) when { resource.name in ["say", "pose", "look", "go"] };`,
		`permit(principal is character, action in ["write", "delete"], resource is location) when { principal.role in ["builder", "admin"] };`,
		`permit(principal is character, action in ["write", "delete"], resource is object) when { principal.role in ["builder", "admin"] };`,
		`permit(principal is character, action in ["execute"], resource is command) when { principal.role in ["builder", "admin"] && resource.name in ["dig", "create", "describe", "link"] };`,
		`permit(principal is character, action, resource) when { principal.role == "admin" };`,
		`permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "public" && principal.location == resource.parent_location };`,
		`permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "private" && resource.owner == principal.id };`,
		`permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "admin" && principal.role == "admin" };`,
		`permit(principal is character, action in ["write", "delete"], resource is property) when { resource.owner == principal.id };`,
		`permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "restricted" && resource has visible_to && principal.id in resource.visible_to };`,
		`forbid(principal is character, action in ["read"], resource is property) when { resource.visibility == "restricted" && resource has excluded_from && principal.id in resource.excluded_from };`,

		// Operator coverage seeds
		// Equality
		`permit(principal, action, resource) when { principal.role == "admin" };`,
		// Not-equals
		`permit(principal, action, resource) when { principal.role != "guest" };`,
		// Greater than
		`permit(principal, action, resource) when { principal.level > 5 };`,
		// Greater or equal
		`permit(principal, action, resource) when { principal.level >= 5 };`,
		// Less than
		`permit(principal, action, resource) when { principal.level < 10 };`,
		// Less or equal
		`permit(principal, action, resource) when { principal.level <= 10 };`,
		// In list
		`permit(principal, action, resource) when { resource.name in ["say", "pose"] };`,
		// In expr
		`permit(principal, action, resource) when { principal.id in resource.visible_to };`,
		// Like
		`permit(principal, action, resource) when { resource.name like "location:*" };`,
		// Has simple
		`permit(principal, action, resource) when { principal has faction };`,
		// Has dotted
		`permit(principal, action, resource) when { resource has metadata.tags };`,
		// containsAll
		`permit(principal, action, resource) when { principal.flags.containsAll(["vip", "beta"]) };`,
		// containsAny
		`permit(principal, action, resource) when { principal.flags.containsAny(["admin", "builder"]) };`,
		// Negation
		`permit(principal, action, resource) when { !(principal.role == "banned") };`,
		// And
		`permit(principal, action, resource) when { principal.role == "admin" && resource.type == "location" };`,
		// Or
		`permit(principal, action, resource) when { principal.role == "admin" || principal.role == "builder" };`,
		// If-then-else
		`permit(principal, action, resource) when { if principal has faction then principal.faction == resource.faction else true };`,
		// Resource exact match
		`permit(principal, action, resource == "location:01XYZ") when { principal.role == "admin" };`,
		// Parenthesized
		`permit(principal, action, resource) when { (principal.role == "admin") };`,
		// Bare true
		`permit(principal, action, resource) when { true };`,
		// Bare false
		`permit(principal, action, resource) when { false };`,

		// Additional edge case seeds
		// Simple forbid
		`forbid(principal, action, resource);`,
		// Forbid with condition
		`forbid(principal, action, resource) when { principal.role == "banned" };`,
		// Deep nesting with parentheses
		`permit(principal, action, resource) when { ((principal.role == "admin")) };`,
		// Multiple ands
		`permit(principal, action, resource) when { principal.role == "admin" && resource.type == "location" && principal.level > 5 };`,
		// Multiple ors
		`permit(principal, action, resource) when { principal.role == "admin" || principal.role == "builder" || principal.role == "moderator" };`,
		// Negation with complex expr
		`permit(principal, action, resource) when { !(principal.role == "banned" && resource.type == "restricted") };`,
		// If-then-else with negation
		`permit(principal, action, resource) when { if !(principal has faction) then true else principal.faction == resource.faction };`,
		// Complex condition with multiple operators
		`permit(principal, action, resource) when { principal.role == "admin" && (resource.level > 5 || resource.level < 0) };`,
		// String comparison
		`permit(principal, action, resource) when { resource.type == "character" };`,
		// Numeric comparison chain
		`permit(principal, action, resource) when { principal.level >= 1 && principal.level <= 99 };`,
		// Property in list
		`permit(principal, action, resource) when { principal.faction in ["red", "blue", "green"] };`,
		// Long list
		`permit(principal, action, resource) when { resource.name in ["a", "b", "c", "d", "e", "f", "g", "h"] };`,
		// Wildcard pattern variations
		`permit(principal, action, resource) when { resource.name like "*:end" };`,
		`permit(principal, action, resource) when { resource.name like "start:*:end" };`,
		`permit(principal, action, resource) when { resource.name like "*middle*" };`,
		// Has with multiple segments
		`permit(principal, action, resource) when { resource has metadata.tags.special };`,
		// Contains with single value
		`permit(principal, action, resource) when { principal.tags.containsAll(["vip"]) };`,
		`permit(principal, action, resource) when { principal.tags.containsAny(["guest"]) };`,
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(_ *testing.T, input string) {
		_, _ = dsl.Parse(input)
	})
}
