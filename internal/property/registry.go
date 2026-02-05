// Copyright 2026 HoloMUSH Contributors

package property

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/world"
)

// ErrInvalidPropertyName indicates the property name is empty or invalid.
var ErrInvalidPropertyName = errors.New("property name cannot be empty")

// ErrDuplicateProperty indicates a property with the same name already exists.
var ErrDuplicateProperty = errors.New("property already registered")

// ErrPropertyNotFound indicates no property matched a prefix.
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

// WorldQuerier provides read access to world data.
type WorldQuerier interface {
	GetLocation(ctx context.Context, id ulid.ULID) (*world.Location, error)
	GetObject(ctx context.Context, id ulid.ULID) (*world.Object, error)
}

// WorldMutator provides write access for property updates.
type WorldMutator interface {
	UpdateLocation(ctx context.Context, subjectID string, loc *world.Location) error
	UpdateObject(ctx context.Context, subjectID string, obj *world.Object) error
}

// PropertyDefinition defines behavior for a settable property.
type PropertyDefinition interface {
	Validate(entityType string) error
	Get(ctx context.Context, querier WorldQuerier, entityType string, entityID ulid.ULID) (string, error)
	Set(ctx context.Context, querier WorldQuerier, mutator WorldMutator, subjectID string, entityType string, entityID ulid.ULID, value string) error
}

// PropertyEntry ties a property name to its definition.
type PropertyEntry struct {
	Name       string
	Definition PropertyDefinition
}

// PropertyRegistry manages property definitions.
// It is safe for concurrent use by multiple goroutines.
type PropertyRegistry struct {
	mu         sync.RWMutex
	properties map[string]PropertyDefinition
}

var sharedRegistryOnce sync.Once
var sharedRegistry *PropertyRegistry

// NewPropertyRegistry creates an empty property registry.
func NewPropertyRegistry() *PropertyRegistry {
	return &PropertyRegistry{properties: make(map[string]PropertyDefinition)}
}

// Register adds a property definition to the registry.
// Returns ErrInvalidPropertyName for empty names and ErrDuplicateProperty on duplicates.
func (r *PropertyRegistry) Register(name string, definition PropertyDefinition) error {
	if strings.TrimSpace(name) == "" {
		return ErrInvalidPropertyName
	}
	if definition == nil {
		return errors.New("property definition cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.properties[name]; exists {
		return ErrDuplicateProperty
	}
	if r.properties == nil {
		r.properties = make(map[string]PropertyDefinition)
	}
	r.properties[name] = definition
	return nil
}

// MustRegister adds a property definition to the registry, panicking on error.
// This is intended for package initialization only.
func (r *PropertyRegistry) MustRegister(name string, definition PropertyDefinition) {
	if err := r.Register(name, definition); err != nil {
		panic(err)
	}
}

// Lookup returns the property definition for the given name.
func (r *PropertyRegistry) Lookup(name string) (PropertyDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	definition, ok := r.properties[name]
	return definition, ok
}

// Resolve finds a property by exact name or unique prefix.
// Returns AmbiguousPropertyError if multiple properties match.
// Returns ErrPropertyNotFound if no properties match.
func (r *PropertyRegistry) Resolve(nameOrPrefix string) (PropertyEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if def, ok := r.properties[nameOrPrefix]; ok {
		return PropertyEntry{Name: nameOrPrefix, Definition: def}, nil
	}

	var matches []string
	for name := range r.properties {
		if strings.HasPrefix(name, nameOrPrefix) {
			matches = append(matches, name)
		}
	}

	switch len(matches) {
	case 0:
		return PropertyEntry{}, ErrPropertyNotFound
	case 1:
		def := r.properties[matches[0]]
		return PropertyEntry{Name: matches[0], Definition: def}, nil
	default:
		return PropertyEntry{}, &AmbiguousPropertyError{Prefix: nameOrPrefix, Matches: matches}
	}
}

// DefaultRegistry returns a registry with standard properties registered.
func DefaultRegistry() *PropertyRegistry {
	r := NewPropertyRegistry()
	r.MustRegister("description", descriptionPropertyDefinition{})
	r.MustRegister("name", namePropertyDefinition{})
	return r
}

// SharedRegistry returns a shared default registry instance.
// This is safe for concurrent access and avoids duplicate registrations.
func SharedRegistry() *PropertyRegistry {
	sharedRegistryOnce.Do(func() {
		sharedRegistry = DefaultRegistry()
	})
	return sharedRegistry
}
