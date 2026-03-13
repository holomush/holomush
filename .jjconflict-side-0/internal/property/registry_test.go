// Copyright 2026 HoloMUSH Contributors

package property

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testDefinition struct {
	validateErr error
	getValue    string
	setErr      error
}

func (d testDefinition) Validate(_ string) error {
	return d.validateErr
}

func (d testDefinition) Get(_ context.Context, _ WorldQuerier, _ string, _ ulid.ULID) (string, error) {
	return d.getValue, nil
}

func (d testDefinition) Set(_ context.Context, _ WorldQuerier, _ WorldMutator, _ string, _ string, _ ulid.ULID, _ string) error {
	return d.setErr
}

func TestPropertyRegistry_RegisterAndLookup(t *testing.T) {
	registry := NewRegistry()

	def := testDefinition{}
	require.NoError(t, registry.Register("name", def))

	got, ok := registry.Lookup("name")
	require.True(t, ok)
	assert.Equal(t, def, got)
}

func TestPropertyRegistry_Register_DuplicateName(t *testing.T) {
	registry := NewRegistry()
	def := testDefinition{}

	require.NoError(t, registry.Register("name", def))
	err := registry.Register("name", def)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDuplicateProperty)
}

func TestPropertyRegistry_Register_EmptyName(t *testing.T) {
	registry := NewRegistry()

	err := registry.Register("", testDefinition{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidPropertyName)
}

func TestPropertyRegistry_Register_NilDefinition(t *testing.T) {
	registry := NewRegistry()

	err := registry.Register("name", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "property definition cannot be nil")
}

func TestPropertyRegistry_DefaultRegistrations(t *testing.T) {
	registry := DefaultRegistry()

	def, ok := registry.Lookup("name")
	require.True(t, ok)
	assert.NoError(t, def.Validate("location"))

	def, ok = registry.Lookup("description")
	require.True(t, ok)
	assert.NoError(t, def.Validate("object"))
}

func TestPropertyRegistry_Resolve_PrefixMatch(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register("description", testDefinition{}))
	require.NoError(t, registry.Register("name", testDefinition{}))

	entry, err := registry.Resolve("desc")
	require.NoError(t, err)
	assert.Equal(t, "description", entry.Name)
}
