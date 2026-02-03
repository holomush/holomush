// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package holo

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPropertyRegistry_Resolve_ExactMatch(t *testing.T) {
	r := NewPropertyRegistry()
	require.NoError(t, r.Register(Property{Name: "description", Type: PropertyTypeText, Capability: "property.set.description"}))

	prop, err := r.Resolve("description")
	require.NoError(t, err)
	assert.Equal(t, "description", prop.Name)
	assert.Equal(t, PropertyTypeText, prop.Type)
	assert.Equal(t, "property.set.description", prop.Capability)
}

func TestPropertyRegistry_Resolve_PrefixMatch(t *testing.T) {
	r := NewPropertyRegistry()
	require.NoError(t, r.Register(Property{Name: "description", Type: PropertyTypeText, Capability: "property.set.description"}))

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
	require.NoError(t, r.Register(Property{Name: "description", Type: PropertyTypeText}))
	require.NoError(t, r.Register(Property{Name: "name", Type: PropertyTypeString}))
	require.NoError(t, r.Register(Property{Name: "notes", Type: PropertyTypeText}))

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
	require.NoError(t, r.Register(Property{Name: "description", Type: PropertyTypeText}))
	require.NoError(t, r.Register(Property{Name: "dark_mode", Type: PropertyTypeBool}))

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
	require.NoError(t, r.Register(Property{Name: "description", Type: PropertyTypeText}))

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
	require.NoError(t, r.Register(Property{Name: "desc", Type: PropertyTypeString, Capability: "property.set.desc"}))
	require.NoError(t, r.Register(Property{Name: "description", Type: PropertyTypeText, Capability: "property.set.description"}))

	prop, err := r.Resolve("desc")
	require.NoError(t, err)
	assert.Equal(t, "desc", prop.Name)
	assert.Equal(t, PropertyTypeString, prop.Type) // Confirms we got "desc", not "description"
}

func TestDefaultRegistry(t *testing.T) {
	r := DefaultRegistry()

	// Should have description property
	desc, err := r.Resolve("description")
	require.NoError(t, err)
	assert.Equal(t, "description", desc.Name)
	assert.Equal(t, PropertyTypeText, desc.Type)
	assert.Equal(t, "property.set.description", desc.Capability)
	assert.ElementsMatch(t, []string{"location", "object", "character", "exit"}, desc.AppliesTo)

	// Should have name property
	name, err := r.Resolve("name")
	require.NoError(t, err)
	assert.Equal(t, "name", name.Name)
	assert.Equal(t, PropertyTypeString, name.Type)
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
		Type:       PropertyTypeNumber,
		Capability: "property.set.test_prop",
		AppliesTo:  []string{"object", "character"},
	}

	assert.Equal(t, "test_prop", p.Name)
	assert.Equal(t, PropertyTypeNumber, p.Type)
	assert.Equal(t, "property.set.test_prop", p.Capability)
	assert.Equal(t, []string{"object", "character"}, p.AppliesTo)
}

func TestPropertyRegistry_ValidFor(t *testing.T) {
	tests := []struct {
		name       string
		entityType string
		property   string
		want       bool
	}{
		// Properties that apply to specific entity types
		{"description applies to location", "location", "description", true},
		{"description applies to object", "object", "description", true},
		{"description applies to character", "character", "description", true},
		{"description applies to exit", "exit", "description", true},
		{"name applies to location", "location", "name", true},
		{"name applies to object", "object", "name", true},
		{"name applies to exit", "exit", "name", true},

		// name does NOT apply to character (per DefaultRegistry)
		{"name does not apply to character", "character", "name", false},

		// Unknown entity types
		{"valid property unknown entity", "unknown", "description", false},
		{"valid property empty entity", "", "description", false},

		// Unknown properties
		{"unknown property", "location", "unknown_prop", false},
		{"empty property", "location", "", false},
	}

	r := DefaultRegistry()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.ValidFor(tt.entityType, tt.property)
			assert.Equal(t, tt.want, got, "ValidFor(%q, %q)", tt.entityType, tt.property)
		})
	}
}

func TestPropertyRegistry_ValidFor_EmptyRegistry(t *testing.T) {
	r := NewPropertyRegistry()

	// Empty registry should return false for any property
	assert.False(t, r.ValidFor("location", "description"))
	assert.False(t, r.ValidFor("object", "name"))
}

func TestPropertyRegistry_ValidFor_CustomProperty(t *testing.T) {
	r := NewPropertyRegistry()
	require.NoError(t, r.Register(Property{
		Name:      "custom",
		Type:      PropertyTypeString,
		AppliesTo: []string{"location", "character"},
	}))

	// Custom property applies to registered entity types
	assert.True(t, r.ValidFor("location", "custom"))
	assert.True(t, r.ValidFor("character", "custom"))

	// Custom property does not apply to unregistered entity types
	assert.False(t, r.ValidFor("object", "custom"))
	assert.False(t, r.ValidFor("exit", "custom"))
}

// Tests for Property validation

func TestPropertyType_Constants(t *testing.T) {
	// Verify property type constants exist and have expected values
	assert.Equal(t, PropertyType("string"), PropertyTypeString)
	assert.Equal(t, PropertyType("text"), PropertyTypeText)
	assert.Equal(t, PropertyType("number"), PropertyTypeNumber)
	assert.Equal(t, PropertyType("bool"), PropertyTypeBool)
}

func TestPropertyType_IsValid(t *testing.T) {
	tests := []struct {
		name    string
		pt      PropertyType
		isValid bool
	}{
		{"string is valid", PropertyTypeString, true},
		{"text is valid", PropertyTypeText, true},
		{"number is valid", PropertyTypeNumber, true},
		{"bool is valid", PropertyTypeBool, true},
		{"empty is invalid", PropertyType(""), false},
		{"unknown is invalid", PropertyType("unknown"), false},
		{"integer is invalid", PropertyType("integer"), false},
		{"STRING is invalid (case sensitive)", PropertyType("STRING"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.isValid, tt.pt.IsValid())
		})
	}
}

func TestPropertyType_String(t *testing.T) {
	assert.Equal(t, "string", PropertyTypeString.String())
	assert.Equal(t, "text", PropertyTypeText.String())
	assert.Equal(t, "number", PropertyTypeNumber.String())
	assert.Equal(t, "bool", PropertyTypeBool.String())
}

func TestNewProperty_Valid(t *testing.T) {
	tests := []struct {
		name       string
		propName   string
		propType   PropertyType
		capability string
		appliesTo  []string
	}{
		{
			name:       "string property",
			propName:   "name",
			propType:   PropertyTypeString,
			capability: "property.set.name",
			appliesTo:  []string{"object"},
		},
		{
			name:       "text property",
			propName:   "description",
			propType:   PropertyTypeText,
			capability: "property.set.description",
			appliesTo:  []string{"object", "location"},
		},
		{
			name:       "number property",
			propName:   "count",
			propType:   PropertyTypeNumber,
			capability: "property.set.count",
			appliesTo:  []string{"object"},
		},
		{
			name:       "bool property",
			propName:   "visible",
			propType:   PropertyTypeBool,
			capability: "property.set.visible",
			appliesTo:  []string{"object"},
		},
		{
			name:       "empty capability is allowed",
			propName:   "test",
			propType:   PropertyTypeString,
			capability: "",
			appliesTo:  []string{"object"},
		},
		{
			name:       "empty appliesTo is allowed",
			propName:   "test",
			propType:   PropertyTypeString,
			capability: "test",
			appliesTo:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := NewProperty(tt.propName, tt.propType, tt.capability, tt.appliesTo)
			require.NoError(t, err)
			assert.Equal(t, tt.propName, p.Name)
			assert.Equal(t, tt.propType, p.Type)
			assert.Equal(t, tt.capability, p.Capability)
			assert.Equal(t, tt.appliesTo, p.AppliesTo)
		})
	}
}

func TestNewProperty_InvalidName(t *testing.T) {
	tests := []struct {
		name     string
		propName string
	}{
		{"empty name", ""},
		{"whitespace only", "   "},
		{"tab only", "\t"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewProperty(tt.propName, PropertyTypeString, "cap", []string{"object"})
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalidPropertyName)
		})
	}
}

func TestNewProperty_InvalidType(t *testing.T) {
	tests := []struct {
		name     string
		propType PropertyType
	}{
		{"empty type", PropertyType("")},
		{"unknown type", PropertyType("unknown")},
		{"integer type", PropertyType("integer")},
		{"uppercase STRING", PropertyType("STRING")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewProperty("test", tt.propType, "cap", []string{"object"})
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalidPropertyType)
		})
	}
}

func TestPropertyRegistry_Register_ReturnsDuplicateError(t *testing.T) {
	r := NewPropertyRegistry()

	p1, err := NewProperty("description", PropertyTypeText, "cap1", []string{"object"})
	require.NoError(t, err)

	p2, err := NewProperty("description", PropertyTypeString, "cap2", []string{"location"})
	require.NoError(t, err)

	// First registration should succeed
	err = r.Register(p1)
	require.NoError(t, err)

	// Second registration with same name should return error
	err = r.Register(p2)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDuplicateProperty)

	// Original property should still be in the registry
	prop, err := r.Resolve("description")
	require.NoError(t, err)
	assert.Equal(t, PropertyTypeText, prop.Type) // Original type, not overwritten
}

func TestPropertyRegistry_Register_DifferentNamesSucceed(t *testing.T) {
	r := NewPropertyRegistry()

	p1, err := NewProperty("name", PropertyTypeString, "cap1", []string{"object"})
	require.NoError(t, err)

	p2, err := NewProperty("description", PropertyTypeText, "cap2", []string{"object"})
	require.NoError(t, err)

	err = r.Register(p1)
	require.NoError(t, err)

	err = r.Register(p2)
	require.NoError(t, err)

	// Both should be in registry
	_, err = r.Resolve("name")
	require.NoError(t, err)
	_, err = r.Resolve("description")
	require.NoError(t, err)
}

func TestPropertyRegistry_MustRegister_Panics(t *testing.T) {
	r := NewPropertyRegistry()

	// First registration should succeed
	r.MustRegister(Property{Name: "test", Type: PropertyTypeString})

	// Second registration with same name should panic
	assert.Panics(t, func() {
		r.MustRegister(Property{Name: "test", Type: PropertyTypeString})
	})
}

func TestPropertyRegistry_MustRegister_Success(t *testing.T) {
	r := NewPropertyRegistry()

	// Should not panic for valid registration
	assert.NotPanics(t, func() {
		r.MustRegister(Property{Name: "test", Type: PropertyTypeString})
	})

	// Verify it was registered
	prop, err := r.Resolve("test")
	require.NoError(t, err)
	assert.Equal(t, "test", prop.Name)
}

func TestPropertyRegistry_ConcurrentAccess(t *testing.T) {
	r := NewPropertyRegistry()

	// Pre-register some properties for reading
	for i := 0; i < 10; i++ {
		p := Property{
			Name:      fmt.Sprintf("initial_%d", i),
			Type:      PropertyTypeString,
			AppliesTo: []string{"object", "location"},
		}
		require.NoError(t, r.Register(p))
	}

	const goroutines = 50
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()

			for i := 0; i < opsPerGoroutine; i++ {
				// Mix of read and write operations
				switch i % 4 {
				case 0:
					// Register new property (write)
					p := Property{
						Name:      fmt.Sprintf("prop_%d_%d", id, i),
						Type:      PropertyTypeString,
						AppliesTo: []string{"object"},
					}
					_ = r.Register(p) // Ignore duplicate errors
				case 1:
					// Resolve existing property (read)
					_, _ = r.Resolve("initial_5")
				case 2:
					// Resolve with prefix (read)
					_, _ = r.Resolve("initial_")
				case 3:
					// ValidFor check (read)
					_ = r.ValidFor("object", "initial_3")
				}
			}
		}(g)
	}

	wg.Wait()

	// Verify registry is still functional after concurrent access
	prop, err := r.Resolve("initial_0")
	require.NoError(t, err)
	assert.Equal(t, "initial_0", prop.Name)
}
