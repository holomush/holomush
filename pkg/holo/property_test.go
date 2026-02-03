// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package holo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPropertyRegistry_Resolve_ExactMatch(t *testing.T) {
	r := NewPropertyRegistry()
	r.Register(Property{Name: "description", Type: "text", Capability: "property.set.description"})

	prop, err := r.Resolve("description")
	require.NoError(t, err)
	assert.Equal(t, "description", prop.Name)
	assert.Equal(t, "text", prop.Type)
	assert.Equal(t, "property.set.description", prop.Capability)
}

func TestPropertyRegistry_Resolve_PrefixMatch(t *testing.T) {
	r := NewPropertyRegistry()
	r.Register(Property{Name: "description", Type: "text", Capability: "property.set.description"})

	tests := []struct {
		prefix   string
		expected string
	}{
		{"desc", "description"},
		{"descr", "description"},
		{"descrip", "description"},
		{"descriptio", "description"},
	}

	for _, tt := range tests {
		t.Run(tt.prefix, func(t *testing.T) {
			prop, err := r.Resolve(tt.prefix)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, prop.Name)
		})
	}
}

func TestPropertyRegistry_Resolve_PrefixMatchMultipleProperties(t *testing.T) {
	r := NewPropertyRegistry()
	r.Register(Property{Name: "description", Type: "text"})
	r.Register(Property{Name: "name", Type: "string"})
	r.Register(Property{Name: "notes", Type: "text"})

	// "na" should uniquely match "name"
	prop, err := r.Resolve("na")
	require.NoError(t, err)
	assert.Equal(t, "name", prop.Name)

	// "n" should be ambiguous (name, notes)
	_, err = r.Resolve("n")
	require.Error(t, err)
	var ambigErr *AmbiguousPropertyError
	require.ErrorAs(t, err, &ambigErr)
	assert.ElementsMatch(t, []string{"name", "notes"}, ambigErr.Matches)
}

func TestPropertyRegistry_Resolve_Ambiguous(t *testing.T) {
	r := NewPropertyRegistry()
	r.Register(Property{Name: "description", Type: "text"})
	r.Register(Property{Name: "dark_mode", Type: "bool"})

	_, err := r.Resolve("d")
	require.Error(t, err)

	var ambigErr *AmbiguousPropertyError
	require.ErrorAs(t, err, &ambigErr)
	assert.Equal(t, "d", ambigErr.Prefix)
	assert.ElementsMatch(t, []string{"dark_mode", "description"}, ambigErr.Matches)

	// Error message should be formatted properly
	assert.Contains(t, ambigErr.Error(), "ambiguous property 'd'")
	assert.Contains(t, ambigErr.Error(), "dark_mode")
	assert.Contains(t, ambigErr.Error(), "description")
}

func TestPropertyRegistry_Resolve_NotFound(t *testing.T) {
	r := NewPropertyRegistry()
	r.Register(Property{Name: "description", Type: "text"})

	_, err := r.Resolve("xyz")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPropertyNotFound)
}

func TestPropertyRegistry_Resolve_NotFoundEmptyRegistry(t *testing.T) {
	r := NewPropertyRegistry()

	_, err := r.Resolve("anything")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPropertyNotFound)
}

func TestPropertyRegistry_Resolve_ExactMatchTakesPriority(t *testing.T) {
	r := NewPropertyRegistry()
	// Register a property named "desc" and one named "description"
	// An exact match for "desc" should return "desc", not "description"
	r.Register(Property{Name: "desc", Type: "string", Capability: "property.set.desc"})
	r.Register(Property{Name: "description", Type: "text", Capability: "property.set.description"})

	prop, err := r.Resolve("desc")
	require.NoError(t, err)
	assert.Equal(t, "desc", prop.Name)
	assert.Equal(t, "string", prop.Type) // Confirms we got "desc", not "description"
}

func TestDefaultRegistry(t *testing.T) {
	r := DefaultRegistry()

	// Should have description property
	desc, err := r.Resolve("description")
	require.NoError(t, err)
	assert.Equal(t, "description", desc.Name)
	assert.Equal(t, "text", desc.Type)
	assert.Equal(t, "property.set.description", desc.Capability)
	assert.ElementsMatch(t, []string{"location", "object", "character", "exit"}, desc.AppliesTo)

	// Should have name property
	name, err := r.Resolve("name")
	require.NoError(t, err)
	assert.Equal(t, "name", name.Name)
	assert.Equal(t, "string", name.Type)
	assert.Equal(t, "property.set.name", name.Capability)
	assert.ElementsMatch(t, []string{"location", "object", "exit"}, name.AppliesTo)
}

func TestDefaultRegistry_PrefixResolution(t *testing.T) {
	r := DefaultRegistry()

	// "desc" should resolve to "description"
	prop, err := r.Resolve("desc")
	require.NoError(t, err)
	assert.Equal(t, "description", prop.Name)

	// "na" should resolve to "name"
	prop, err = r.Resolve("na")
	require.NoError(t, err)
	assert.Equal(t, "name", prop.Name)
}

func TestAmbiguousPropertyError_SortedMatches(t *testing.T) {
	// Verify that matches are sorted in the error message
	err := &AmbiguousPropertyError{
		Prefix:  "x",
		Matches: []string{"zebra", "apple", "mango"},
	}

	errMsg := err.Error()
	// Should be sorted: apple, mango, zebra
	assert.Contains(t, errMsg, "apple, mango, zebra")
}

func TestProperty_Fields(t *testing.T) {
	p := Property{
		Name:       "test_prop",
		Type:       "number",
		Capability: "property.set.test_prop",
		AppliesTo:  []string{"object", "character"},
	}

	assert.Equal(t, "test_prop", p.Name)
	assert.Equal(t, "number", p.Type)
	assert.Equal(t, "property.set.test_prop", p.Capability)
	assert.Equal(t, []string{"object", "character"}, p.AppliesTo)
}
