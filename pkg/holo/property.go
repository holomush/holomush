// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package holo

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrPropertyNotFound indicates no property matched the given name/prefix.
var ErrPropertyNotFound = errors.New("property not found")

// AmbiguousPropertyError indicates multiple properties match a prefix.
type AmbiguousPropertyError struct {
	Prefix  string
	Matches []string
}

func (e *AmbiguousPropertyError) Error() string {
	sorted := make([]string, len(e.Matches))
	copy(sorted, e.Matches)
	sort.Strings(sorted)
	return fmt.Sprintf("ambiguous property '%s' - matches: %s", e.Prefix, strings.Join(sorted, ", "))
}

// Property defines a settable property on game entities.
type Property struct {
	Name       string   // Full property name (e.g., "description")
	Type       string   // Property type: "string", "text", "number", "bool"
	Capability string   // Required capability to set (e.g., "property.set.description")
	AppliesTo  []string // Entity types this property applies to
}

// PropertyRegistry manages known properties with prefix resolution.
type PropertyRegistry struct {
	properties map[string]Property
}

// NewPropertyRegistry creates an empty property registry.
func NewPropertyRegistry() *PropertyRegistry {
	return &PropertyRegistry{
		properties: make(map[string]Property),
	}
}

// Register adds a property to the registry.
func (r *PropertyRegistry) Register(p Property) {
	r.properties[p.Name] = p
}

// Resolve finds a property by exact name or unique prefix.
// Returns AmbiguousPropertyError if multiple properties match.
// Returns ErrPropertyNotFound if no properties match.
func (r *PropertyRegistry) Resolve(nameOrPrefix string) (Property, error) {
	// Exact match first
	if p, ok := r.properties[nameOrPrefix]; ok {
		return p, nil
	}

	// Prefix matching
	var matches []string
	for name := range r.properties {
		if strings.HasPrefix(name, nameOrPrefix) {
			matches = append(matches, name)
		}
	}

	switch len(matches) {
	case 0:
		return Property{}, ErrPropertyNotFound
	case 1:
		return r.properties[matches[0]], nil
	default:
		return Property{}, &AmbiguousPropertyError{Prefix: nameOrPrefix, Matches: matches}
	}
}

// DefaultRegistry returns a registry with standard properties.
func DefaultRegistry() *PropertyRegistry {
	r := NewPropertyRegistry()
	r.Register(Property{
		Name:       "description",
		Type:       "text",
		Capability: "property.set.description",
		AppliesTo:  []string{"location", "object", "character", "exit"},
	})
	r.Register(Property{
		Name:       "name",
		Type:       "string",
		Capability: "property.set.name",
		AppliesTo:  []string{"location", "object", "exit"},
	})
	return r
}
