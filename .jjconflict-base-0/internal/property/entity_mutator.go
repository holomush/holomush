// SPDX-License-Identifier: Apache-2.0
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
	"github.com/samber/oops"
)

// ErrInvalidEntityType indicates the entity type is empty or invalid.
var ErrInvalidEntityType = errors.New("entity type cannot be empty")

// ErrDuplicateEntityMutator indicates an entity mutator already exists.
var ErrDuplicateEntityMutator = errors.New("entity mutator already registered")

// EntityMutator defines entity-specific property operations.
type EntityMutator interface {
	EntityType() string
	GetName(ctx context.Context, querier WorldQuerier, entityID ulid.ULID) (string, error)
	SetName(ctx context.Context, querier WorldQuerier, mutator WorldMutator, subjectID string, entityID ulid.ULID, value string) error
	GetDescription(ctx context.Context, querier WorldQuerier, entityID ulid.ULID) (string, error)
	SetDescription(ctx context.Context, querier WorldQuerier, mutator WorldMutator, subjectID string, entityID ulid.ULID, value string) error
}

// EntityMutatorRegistry manages entity mutators.
// It is safe for concurrent use by multiple goroutines.
type EntityMutatorRegistry struct {
	mu       sync.RWMutex
	mutators map[string]EntityMutator
}

var (
	sharedEntityMutatorOnce     sync.Once
	sharedEntityMutatorRegistry *EntityMutatorRegistry
)

// NewEntityMutatorRegistry creates an empty entity mutator registry.
func NewEntityMutatorRegistry() *EntityMutatorRegistry {
	return &EntityMutatorRegistry{mutators: make(map[string]EntityMutator)}
}

// Register adds an entity mutator to the registry.
// Returns ErrInvalidEntityType for empty entity types and ErrDuplicateEntityMutator on duplicates.
func (r *EntityMutatorRegistry) Register(mutator EntityMutator) error {
	if mutator == nil {
		return errors.New("entity mutator cannot be nil")
	}

	entityType := strings.TrimSpace(mutator.EntityType())
	if entityType == "" {
		return ErrInvalidEntityType
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.mutators[entityType]; exists {
		return ErrDuplicateEntityMutator
	}
	if r.mutators == nil {
		r.mutators = make(map[string]EntityMutator)
	}
	r.mutators[entityType] = mutator
	return nil
}

// MustRegister adds an entity mutator to the registry, panicking on error.
// This is intended for package initialization only.
func (r *EntityMutatorRegistry) MustRegister(mutator EntityMutator) {
	if err := r.Register(mutator); err != nil {
		panic(err)
	}
}

// Lookup returns the entity mutator for the given type.
func (r *EntityMutatorRegistry) Lookup(entityType string) (EntityMutator, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	mutator, ok := r.mutators[entityType]
	return mutator, ok
}

// RegisteredTypes returns the sorted list of registered entity types.
func (r *EntityMutatorRegistry) RegisteredTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.mutators))
	for entityType := range r.mutators {
		types = append(types, entityType)
	}
	sort.Strings(types)
	return types
}

// DefaultEntityMutatorRegistry returns a registry with standard entity mutators registered.
func DefaultEntityMutatorRegistry() *EntityMutatorRegistry {
	r := NewEntityMutatorRegistry()
	r.MustRegister(locationEntityMutator{})
	r.MustRegister(objectEntityMutator{})
	return r
}

// SharedEntityMutatorRegistry returns a shared default registry instance.
// This is safe for concurrent access and avoids duplicate registrations.
func SharedEntityMutatorRegistry() *EntityMutatorRegistry {
	sharedEntityMutatorOnce.Do(func() {
		sharedEntityMutatorRegistry = DefaultEntityMutatorRegistry()
	})
	return sharedEntityMutatorRegistry
}

type locationEntityMutator struct{}

func (locationEntityMutator) EntityType() string {
	return "location"
}

func (locationEntityMutator) GetName(ctx context.Context, querier WorldQuerier, entityID ulid.ULID) (string, error) {
	loc, err := querier.GetLocation(ctx, entityID)
	if err != nil {
		return "", fmt.Errorf("get location: %w", err)
	}
	return loc.Name, nil
}

func (locationEntityMutator) SetName(ctx context.Context, querier WorldQuerier, mutator WorldMutator, subjectID string, entityID ulid.ULID, value string) error {
	loc, err := querier.GetLocation(ctx, entityID)
	if err != nil {
		return fmt.Errorf("get location: %w", err)
	}
	loc.Name = value
	if err := mutator.UpdateLocation(ctx, subjectID, loc); err != nil {
		return oops.Wrapf(err, "update location %s", entityID)
	}
	return nil
}

func (locationEntityMutator) GetDescription(ctx context.Context, querier WorldQuerier, entityID ulid.ULID) (string, error) {
	loc, err := querier.GetLocation(ctx, entityID)
	if err != nil {
		return "", fmt.Errorf("get location: %w", err)
	}
	return loc.Description, nil
}

func (locationEntityMutator) SetDescription(ctx context.Context, querier WorldQuerier, mutator WorldMutator, subjectID string, entityID ulid.ULID, value string) error {
	loc, err := querier.GetLocation(ctx, entityID)
	if err != nil {
		return fmt.Errorf("get location: %w", err)
	}
	loc.Description = value
	if err := mutator.UpdateLocation(ctx, subjectID, loc); err != nil {
		return oops.Wrapf(err, "update location %s", entityID)
	}
	return nil
}

type objectEntityMutator struct{}

func (objectEntityMutator) EntityType() string {
	return "object"
}

func (objectEntityMutator) GetName(ctx context.Context, querier WorldQuerier, entityID ulid.ULID) (string, error) {
	obj, err := querier.GetObject(ctx, entityID)
	if err != nil {
		return "", fmt.Errorf("get object: %w", err)
	}
	return obj.Name, nil
}

func (objectEntityMutator) SetName(ctx context.Context, querier WorldQuerier, mutator WorldMutator, subjectID string, entityID ulid.ULID, value string) error {
	obj, err := querier.GetObject(ctx, entityID)
	if err != nil {
		return fmt.Errorf("get object: %w", err)
	}
	obj.Name = value
	if err := mutator.UpdateObject(ctx, subjectID, obj); err != nil {
		return oops.Wrapf(err, "update object %s", entityID)
	}
	return nil
}

func (objectEntityMutator) GetDescription(ctx context.Context, querier WorldQuerier, entityID ulid.ULID) (string, error) {
	obj, err := querier.GetObject(ctx, entityID)
	if err != nil {
		return "", fmt.Errorf("get object: %w", err)
	}
	return obj.Description, nil
}

func (objectEntityMutator) SetDescription(ctx context.Context, querier WorldQuerier, mutator WorldMutator, subjectID string, entityID ulid.ULID, value string) error {
	obj, err := querier.GetObject(ctx, entityID)
	if err != nil {
		return fmt.Errorf("get object: %w", err)
	}
	obj.Description = value
	if err := mutator.UpdateObject(ctx, subjectID, obj); err != nil {
		return oops.Wrapf(err, "update object %s", entityID)
	}
	return nil
}
