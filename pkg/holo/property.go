// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package holo

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
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
// Property is conceptually immutable after construction via NewProperty.
// Use GetAppliesTo() to access the entity types this property applies to (returns a defensive copy).
type Property struct {
	Name       string       // Full property name (e.g., "description")
	Type       PropertyType // Property type: "string", "text", "number", "bool"
	Capability string       // Required capability to set (e.g., "property.set.description")
	appliesTo  []string     // Entity types this property applies to - use GetAppliesTo() for access
}

// GetAppliesTo returns a defensive copy of the entity types this property applies to.
// This prevents external modification of the property's internal state.
// Returns nil if no entity types are set.
func (p *Property) GetAppliesTo() []string {
	if p.appliesTo == nil {
		return nil
	}
	result := make([]string, len(p.appliesTo))
	copy(result, p.appliesTo)
	return result
}

// NewProperty creates a validated Property.
// Returns ErrInvalidPropertyName if name is empty or whitespace-only.
// Returns ErrInvalidPropertyType if propType is not a valid PropertyType.
// The appliesTo slice is defensively copied to prevent external mutation.
func NewProperty(name string, propType PropertyType, capability string, appliesTo []string) (Property, error) {
	if strings.TrimSpace(name) == "" {
		return Property{}, ErrInvalidPropertyName
	}
	if !propType.IsValid() {
		return Property{}, ErrInvalidPropertyType
	}
	// Defensive copy of appliesTo to prevent external mutation
	var appliesCopy []string
	if appliesTo != nil {
		appliesCopy = make([]string, len(appliesTo))
		copy(appliesCopy, appliesTo)
	}
	return Property{
		Name:       name,
		Type:       propType,
		Capability: capability,
		appliesTo:  appliesCopy,
	}, nil
}

// PropertyRegistry manages known properties with prefix resolution.
// It is safe for concurrent use by multiple goroutines.
type PropertyRegistry struct {
	mu         sync.RWMutex
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
	r.mu.Lock()
	defer r.mu.Unlock()

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
	r.mu.RLock()
	defer r.mu.RUnlock()

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
	r.mu.RLock()
	defer r.mu.RUnlock()

	prop, ok := r.properties[propertyName]
	if !ok {
		return false
	}
	for _, et := range prop.appliesTo {
		if et == entityType {
			return true
		}
	}
	return false
}

// MustRegister adds a property to the registry, panicking if registration fails.
//
// This function is ONLY for use during program initialization, such as in init()
// functions or package-level variable declarations. It should never be called at
// runtime with user-provided or dynamic data, as any error will panic and crash
// the program.
//
// For runtime registration where errors are recoverable, use [PropertyRegistry.Register]
// instead.
//
// Example usage:
//
//	// Package-level initialization (safe - known-valid at compile time)
//	var gameRegistry = func() *PropertyRegistry {
//		r := NewPropertyRegistry()
//		p, _ := NewProperty("health", PropertyTypeNumber, "property.set.health", []string{"character"})
//		r.MustRegister(p)
//		return r
//	}()
//
//	// Or in an init function
//	func init() {
//		registry.MustRegister(Property{...})
//	}
//
//	// WRONG: Never use at runtime with dynamic data
//	// func handleUserInput(name string) {
//	//     registry.MustRegister(Property{Name: name, ...}) // DON'T DO THIS
//	// }
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
		appliesTo:  []string{"location", "object", "character", "exit"},
	})
	r.MustRegister(Property{
		Name:       "name",
		Type:       PropertyTypeString,
		Capability: "property.set.name",
		appliesTo:  []string{"location", "object", "exit"},
	})
	return r
}
