// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestContainment_Constructors(t *testing.T) {
	locID := ulid.Make()
	charID := ulid.Make()
	objID := ulid.Make()

	t.Run("InLocation creates valid containment", func(t *testing.T) {
		c := world.InLocation(locID)
		require.NoError(t, c.Validate())
		assert.Equal(t, world.ContainmentTypeLocation, c.Type())
		assert.Equal(t, &locID, c.ID())
		assert.Equal(t, &locID, c.LocationID)
		assert.Nil(t, c.CharacterID)
		assert.Nil(t, c.ObjectID)
	})

	t.Run("HeldByCharacter creates valid containment", func(t *testing.T) {
		c := world.HeldByCharacter(charID)
		require.NoError(t, c.Validate())
		assert.Equal(t, world.ContainmentTypeCharacter, c.Type())
		assert.Equal(t, &charID, c.ID())
		assert.Nil(t, c.LocationID)
		assert.Equal(t, &charID, c.CharacterID)
		assert.Nil(t, c.ObjectID)
	})

	t.Run("ContainedInObject creates valid containment", func(t *testing.T) {
		c := world.ContainedInObject(objID)
		require.NoError(t, c.Validate())
		assert.Equal(t, world.ContainmentTypeObject, c.Type())
		assert.Equal(t, &objID, c.ID())
		assert.Nil(t, c.LocationID)
		assert.Nil(t, c.CharacterID)
		assert.Equal(t, &objID, c.ObjectID)
	})
}

func TestContainment_Validate(t *testing.T) {
	locID := ulid.Make()
	charID := ulid.Make()
	objID := ulid.Make()

	tests := []struct {
		name        string
		containment world.Containment
		expectErr   bool
	}{
		{
			name:        "in location",
			containment: world.Containment{LocationID: &locID},
			expectErr:   false,
		},
		{
			name:        "held by character",
			containment: world.Containment{CharacterID: &charID},
			expectErr:   false,
		},
		{
			name:        "in container",
			containment: world.Containment{ObjectID: &objID},
			expectErr:   false,
		},
		{
			name:        "nowhere - invalid",
			containment: world.Containment{},
			expectErr:   true,
		},
		{
			name:        "multiple places - invalid",
			containment: world.Containment{LocationID: &locID, CharacterID: &charID},
			expectErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.containment.Validate()
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestContainment_Type(t *testing.T) {
	locID := ulid.Make()
	charID := ulid.Make()
	objID := ulid.Make()

	tests := []struct {
		name        string
		containment world.Containment
		expected    world.ContainmentType
	}{
		{"location", world.Containment{LocationID: &locID}, world.ContainmentTypeLocation},
		{"character", world.Containment{CharacterID: &charID}, world.ContainmentTypeCharacter},
		{"object", world.Containment{ObjectID: &objID}, world.ContainmentTypeObject},
		{"empty", world.Containment{}, world.ContainmentTypeNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.containment.Type())
		})
	}
}

func TestContainment_ID(t *testing.T) {
	locID := ulid.Make()
	charID := ulid.Make()
	objID := ulid.Make()

	tests := []struct {
		name        string
		containment world.Containment
		expected    *ulid.ULID
	}{
		{"location", world.Containment{LocationID: &locID}, &locID},
		{"character", world.Containment{CharacterID: &charID}, &charID},
		{"object", world.Containment{ObjectID: &objID}, &objID},
		{"empty", world.Containment{}, nil},
		// Priority order: location > character > object
		{"location takes priority over character", world.Containment{LocationID: &locID, CharacterID: &charID}, &locID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.containment.ID()
			if tt.expected == nil {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
				assert.Equal(t, *tt.expected, *got)
			}
		})
	}
}

func TestObject_Containment(t *testing.T) {
	locID := ulid.Make()
	obj, err := world.NewObject("Sword", world.InLocation(locID))
	require.NoError(t, err)

	containment := obj.Containment()
	assert.NotNil(t, containment.LocationID)
	assert.Equal(t, locID, *containment.LocationID)
	assert.Nil(t, containment.CharacterID)
	assert.Nil(t, containment.ObjectID)
}

func TestObject_SetContainment(t *testing.T) {
	locID := ulid.Make()
	charID := ulid.Make()
	objID := ulid.Make()

	t.Run("set to location clears other fields", func(t *testing.T) {
		// Start with object held by character
		obj, err := world.NewObject("Test", world.HeldByCharacter(charID))
		require.NoError(t, err)

		err = obj.SetContainment(world.Containment{LocationID: &locID})
		require.NoError(t, err)

		c := obj.Containment()
		assert.NotNil(t, c.LocationID)
		assert.Equal(t, locID, *c.LocationID)
		assert.Nil(t, c.CharacterID)
		assert.Nil(t, c.ObjectID)
	})

	t.Run("set to character clears other fields", func(t *testing.T) {
		// Start with object in location
		obj, err := world.NewObject("Test", world.InLocation(locID))
		require.NoError(t, err)

		err = obj.SetContainment(world.Containment{CharacterID: &charID})
		require.NoError(t, err)

		c := obj.Containment()
		assert.Nil(t, c.LocationID)
		assert.NotNil(t, c.CharacterID)
		assert.Equal(t, charID, *c.CharacterID)
		assert.Nil(t, c.ObjectID)
	})

	t.Run("set to container clears other fields", func(t *testing.T) {
		// Start with object held by character
		obj, err := world.NewObject("Test", world.HeldByCharacter(charID))
		require.NoError(t, err)

		err = obj.SetContainment(world.Containment{ObjectID: &objID})
		require.NoError(t, err)

		c := obj.Containment()
		assert.Nil(t, c.LocationID)
		assert.Nil(t, c.CharacterID)
		assert.NotNil(t, c.ObjectID)
		assert.Equal(t, objID, *c.ObjectID)
	})

	t.Run("rejects invalid containment with multiple fields", func(t *testing.T) {
		obj, err := world.NewObject("Test", world.InLocation(locID))
		require.NoError(t, err)
		err = obj.SetContainment(world.Containment{LocationID: &locID, CharacterID: &charID})
		assert.ErrorIs(t, err, world.ErrInvalidContainment)
	})

	t.Run("rejects empty containment", func(t *testing.T) {
		obj, err := world.NewObject("Test", world.InLocation(locID))
		require.NoError(t, err)
		err = obj.SetContainment(world.Containment{})
		assert.ErrorIs(t, err, world.ErrInvalidContainment)
	})
}

func TestContainment_Validate_AllThreeSet(t *testing.T) {
	locID := ulid.Make()
	charID := ulid.Make()
	objID := ulid.Make()

	// Test case where all three are set (should be invalid)
	containment := world.Containment{
		LocationID:  &locID,
		CharacterID: &charID,
		ObjectID:    &objID,
	}
	err := containment.Validate()
	assert.Error(t, err)
	assert.ErrorIs(t, err, world.ErrInvalidContainment)
}

func TestContainment_Validate_ErrorContext(t *testing.T) {
	locID := ulid.Make()
	charID := ulid.Make()

	t.Run("no fields set provides context", func(t *testing.T) {
		containment := world.Containment{}
		err := containment.Validate()
		require.Error(t, err)
		errutil.AssertErrorContext(t, err, "location_set", false)
		errutil.AssertErrorContext(t, err, "character_set", false)
		errutil.AssertErrorContext(t, err, "object_set", false)
		errutil.AssertErrorContext(t, err, "count", 0)
	})

	t.Run("multiple fields set provides context", func(t *testing.T) {
		containment := world.Containment{LocationID: &locID, CharacterID: &charID}
		err := containment.Validate()
		require.Error(t, err)
		errutil.AssertErrorContext(t, err, "location_set", true)
		errutil.AssertErrorContext(t, err, "character_set", true)
		errutil.AssertErrorContext(t, err, "object_set", false)
		errutil.AssertErrorContext(t, err, "count", 2)
	})
}

func TestObject_Validate(t *testing.T) {
	t.Run("valid object", func(t *testing.T) {
		obj := &world.Object{
			ID:   ulid.Make(),
			Name: "Sword",
		}
		assert.NoError(t, obj.Validate())
	})

	t.Run("invalid name", func(t *testing.T) {
		obj := &world.Object{
			ID:   ulid.Make(),
			Name: "",
		}
		err := obj.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("valid with description", func(t *testing.T) {
		obj := &world.Object{
			ID:          ulid.Make(),
			Name:        "Sword",
			Description: "A shiny blade.",
		}
		assert.NoError(t, obj.Validate())
	})

	t.Run("name at exactly max length passes", func(t *testing.T) {
		exactName := make([]byte, world.MaxNameLength)
		for i := range exactName {
			exactName[i] = 'a'
		}
		obj := &world.Object{
			ID:   ulid.Make(),
			Name: string(exactName),
		}
		require.NoError(t, obj.Validate())
	})

	t.Run("name exceeds max length", func(t *testing.T) {
		longName := make([]byte, world.MaxNameLength+1)
		for i := range longName {
			longName[i] = 'a'
		}
		obj := &world.Object{
			ID:   ulid.Make(),
			Name: string(longName),
		}
		err := obj.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name")
	})

	t.Run("name with control characters fails", func(t *testing.T) {
		obj := &world.Object{
			ID:   ulid.Make(),
			Name: "Sword\x00",
		}
		err := obj.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name")
	})

	t.Run("description at exactly max length passes", func(t *testing.T) {
		exactDesc := make([]byte, world.MaxDescriptionLength)
		for i := range exactDesc {
			exactDesc[i] = 'a'
		}
		obj := &world.Object{
			ID:          ulid.Make(),
			Name:        "Sword",
			Description: string(exactDesc),
		}
		require.NoError(t, obj.Validate())
	})

	t.Run("description exceeds max length", func(t *testing.T) {
		longDesc := make([]byte, world.MaxDescriptionLength+1)
		for i := range longDesc {
			longDesc[i] = 'a'
		}
		obj := &world.Object{
			ID:          ulid.Make(),
			Name:        "Sword",
			Description: string(longDesc),
		}
		err := obj.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "description")
	})

	t.Run("description with control characters fails", func(t *testing.T) {
		obj := &world.Object{
			ID:          ulid.Make(),
			Name:        "Sword",
			Description: "A shiny\x00blade",
		}
		err := obj.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "description")
	})

	t.Run("zero id fails", func(t *testing.T) {
		obj := &world.Object{
			// ID is zero value (not set)
			Name: "Sword",
		}
		err := obj.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "id")
	})
}

func TestObject_ValidateContainment(t *testing.T) {
	locID := ulid.Make()

	t.Run("valid object with location", func(t *testing.T) {
		obj, err := world.NewObject("Sword", world.InLocation(locID))
		require.NoError(t, err)
		assert.NoError(t, obj.ValidateContainment())
	})

	// Note: Invalid containment states (no containment, multiple containment) cannot be
	// created from outside the world package due to unexported fields. These invariants
	// are now enforced at the type level through NewObject/SetContainment.
	// Containment.Validate() tests cover the validation logic itself.
}

func TestNewObject(t *testing.T) {
	locID := ulid.Make()
	charID := ulid.Make()

	t.Run("valid construction succeeds", func(t *testing.T) {
		containment := world.InLocation(locID)
		obj, err := world.NewObject("Sword", containment)
		require.NoError(t, err)
		assert.NotNil(t, obj)
		assert.False(t, obj.ID.IsZero(), "ID should be generated")
		assert.Equal(t, "Sword", obj.Name)
		assert.NotNil(t, obj.LocationID())
		assert.Equal(t, locID, *obj.LocationID())
		assert.False(t, obj.CreatedAt.IsZero(), "CreatedAt should be set")
	})

	t.Run("empty name fails with validation error", func(t *testing.T) {
		containment := world.InLocation(locID)
		obj, err := world.NewObject("", containment)
		assert.Nil(t, obj)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name")
	})

	t.Run("invalid containment fails", func(t *testing.T) {
		// Empty containment - no field set
		containment := world.Containment{}
		obj, err := world.NewObject("Sword", containment)
		assert.Nil(t, obj)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidContainment)
	})

	t.Run("held by character containment succeeds", func(t *testing.T) {
		containment := world.HeldByCharacter(charID)
		obj, err := world.NewObject("Shield", containment)
		require.NoError(t, err)
		assert.NotNil(t, obj.HeldByCharacterID())
		assert.Equal(t, charID, *obj.HeldByCharacterID())
	})

	t.Run("generates unique IDs", func(t *testing.T) {
		containment := world.InLocation(locID)
		obj1, err1 := world.NewObject("Sword", containment)
		require.NoError(t, err1)
		obj2, err2 := world.NewObject("Shield", containment)
		require.NoError(t, err2)
		assert.NotEqual(t, obj1.ID, obj2.ID, "IDs should be unique")
	})
}

func TestNewObjectWithID(t *testing.T) {
	locID := ulid.Make()
	objID := ulid.Make()

	t.Run("valid construction succeeds", func(t *testing.T) {
		containment := world.InLocation(locID)
		obj, err := world.NewObjectWithID(objID, "Sword", containment)
		require.NoError(t, err)
		assert.NotNil(t, obj)
		assert.Equal(t, objID, obj.ID, "ID should match provided ID")
		assert.Equal(t, "Sword", obj.Name)
		assert.NotNil(t, obj.LocationID())
		assert.Equal(t, locID, *obj.LocationID())
		assert.False(t, obj.CreatedAt.IsZero(), "CreatedAt should be set")
	})

	t.Run("empty name fails with validation error", func(t *testing.T) {
		containment := world.InLocation(locID)
		obj, err := world.NewObjectWithID(objID, "", containment)
		assert.Nil(t, obj)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name")
	})

	t.Run("invalid containment fails", func(t *testing.T) {
		containment := world.Containment{} // Empty
		obj, err := world.NewObjectWithID(objID, "Sword", containment)
		assert.Nil(t, obj)
		require.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidContainment)
	})

	t.Run("zero ID fails with validation error", func(t *testing.T) {
		var zeroID ulid.ULID
		containment := world.InLocation(locID)
		obj, err := world.NewObjectWithID(zeroID, "Sword", containment)
		assert.Nil(t, obj)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "id")
	})

	t.Run("uses provided ID exactly", func(t *testing.T) {
		specificID := ulid.Make()
		containment := world.InLocation(locID)
		obj, err := world.NewObjectWithID(specificID, "Sword", containment)
		require.NoError(t, err)
		assert.Equal(t, specificID, obj.ID)
	})
}
