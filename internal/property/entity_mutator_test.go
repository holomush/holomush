// Copyright 2026 HoloMUSH Contributors

package property

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testEntityMutator struct {
	entityType string
}

func (t testEntityMutator) EntityType() string {
	return t.entityType
}

func (t testEntityMutator) GetName(_ context.Context, _ WorldQuerier, _ ulid.ULID) (string, error) {
	return "", nil
}

func (t testEntityMutator) SetName(_ context.Context, _ WorldQuerier, _ WorldMutator, _ string, _ ulid.ULID, _ string) error {
	return nil
}

func (t testEntityMutator) GetDescription(_ context.Context, _ WorldQuerier, _ ulid.ULID) (string, error) {
	return "", nil
}

func (t testEntityMutator) SetDescription(_ context.Context, _ WorldQuerier, _ WorldMutator, _ string, _ ulid.ULID, _ string) error {
	return nil
}

func TestEntityMutatorRegistry_RegisterAndLookup(t *testing.T) {
	registry := NewEntityMutatorRegistry()

	mutator := testEntityMutator{entityType: "location"}
	require.NoError(t, registry.Register(mutator))

	got, ok := registry.Lookup("location")
	require.True(t, ok)
	assert.Equal(t, mutator, got)
}

func TestEntityMutatorRegistry_Register_Duplicate(t *testing.T) {
	registry := NewEntityMutatorRegistry()
	mutator := testEntityMutator{entityType: "location"}

	require.NoError(t, registry.Register(mutator))
	err := registry.Register(mutator)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDuplicateEntityMutator)
}

func TestEntityMutatorRegistry_Register_NilMutator(t *testing.T) {
	registry := NewEntityMutatorRegistry()

	err := registry.Register(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "entity mutator cannot be nil")
}

func TestEntityMutatorRegistry_Register_EmptyEntityType(t *testing.T) {
	registry := NewEntityMutatorRegistry()

	err := registry.Register(testEntityMutator{entityType: ""})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidEntityType)
}

func TestEntityMutatorRegistry_DefaultRegistrations(t *testing.T) {
	registry := DefaultEntityMutatorRegistry()

	_, ok := registry.Lookup("location")
	assert.True(t, ok)

	_, ok = registry.Lookup("object")
	assert.True(t, ok)
}

func TestEntityMutatorRegistry_RegisteredTypes_Sorted(t *testing.T) {
	registry := NewEntityMutatorRegistry()
	require.NoError(t, registry.Register(testEntityMutator{entityType: "widget"}))
	require.NoError(t, registry.Register(testEntityMutator{entityType: "location"}))

	assert.Equal(t, []string{"location", "widget"}, registry.RegisteredTypes())
}
