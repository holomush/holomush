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

// ErrInvalidPropertyName indicates the property name is empty or invalid.
var ErrInvalidPropertyName = errors.New("property name cannot be empty")

// ErrInvalidPropertyType indicates the property type is not a valid PropertyType.
var ErrInvalidPropertyType = errors.New("invalid property type")

// ErrDuplicateProperty indicates a property with the same name already exists.
var ErrDuplicateProperty = errors.New("property already registered")

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

// PropertyType defines the valid types for property values.
type PropertyType string

// Property type constants for valid property value types.
const (
	PropertyTypeString PropertyType = "string" // Short single-line text
	PropertyTypeText   PropertyType = "text"   // Multi-line text
	PropertyTypeNumber PropertyType = "number" // Numeric value
	PropertyTypeBool   PropertyType = "bool"   // Boolean value
)

// validPropertyTypes is the set of allowed property types.
var validPropertyTypes = map[PropertyType]struct{}{
	PropertyTypeString: {},
	PropertyTypeText:   {},
	PropertyTypeNumber: {},
	PropertyTypeBool:   {},
}

// IsValid returns true if the PropertyType is one of the allowed values.
func (pt PropertyType) IsValid() bool {
	_, ok := validPropertyTypes[pt]
	return ok
}

// String returns the string representation of the PropertyType.
func (pt PropertyType) String() string {
	return string(pt)
}

// Property defines a settable property on game entities.
type Property struct {
	Name       string       // Full property name (e.g., "description")
	Type       PropertyType // Property type: "string", "text", "number", "bool"
	Capability string       // Required capability to set (e.g., "property.set.description")
	AppliesTo  []string     // Entity types this property applies to
}

// NewProperty creates a validated Property.
// Returns ErrInvalidPropertyName if name is empty or whitespace-only.
// Returns ErrInvalidPropertyType if propType is not a valid PropertyType.
func NewProperty(name string, propType PropertyType, capability string, appliesTo []string) (Property, error) {
	if strings.TrimSpace(name) == "" {
		return Property{}, ErrInvalidPropertyName
	}
	if !propType.IsValid() {
		return Property{}, ErrInvalidPropertyType
	}
	return Property{
		Name:       name,
		Type:       propType,
		Capability: capability,
		AppliesTo:  appliesTo,
	}, nil
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
// Returns ErrDuplicateProperty if a property with the same name already exists.
func (r *PropertyRegistry) Register(p Property) error {
	if _, exists := r.properties[p.Name]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateProperty, p.Name)
	}
	r.properties[p.Name] = p
	return nil
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

// ValidFor checks if a property is valid for a given entity type.
// Returns false if the property doesn't exist or doesn't apply to the entity type.
func (r *PropertyRegistry) ValidFor(entityType, propertyName string) bool {
	prop, ok := r.properties[propertyName]
	if !ok {
		return false
	}
	for _, et := range prop.AppliesTo {
		if et == entityType {
			return true
		}
	}
	return false
}

// MustRegister adds a property to the registry, panicking if registration fails.
// Use this only for known-valid properties during initialization.
func (r *PropertyRegistry) MustRegister(p Property) {
	if err := r.Register(p); err != nil {
		panic(err)
	}
}

// DefaultRegistry returns a registry with standard properties.
func DefaultRegistry() *PropertyRegistry {
	r := NewPropertyRegistry()
	r.MustRegister(Property{
		Name:       "description",
		Type:       PropertyTypeText,
		Capability: "property.set.description",
		AppliesTo:  []string{"location", "object", "character", "exit"},
	})
	r.MustRegister(Property{
		Name:       "name",
		Type:       PropertyTypeString,
		Capability: "property.set.name",
		AppliesTo:  []string{"location", "object", "exit"},
	})
	return r
}
