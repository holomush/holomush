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
		expected    string
	}{
		{"location", world.Containment{LocationID: &locID}, "location"},
		{"character", world.Containment{CharacterID: &charID}, "character"},
		{"object", world.Containment{ObjectID: &objID}, "object"},
		{"empty", world.Containment{}, ""},
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
	obj := &world.Object{
		ID:         ulid.Make(),
		Name:       "Sword",
		LocationID: &locID,
	}

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
		obj := &world.Object{
			ID:                ulid.Make(),
			Name:              "Test",
			HeldByCharacterID: &charID,
		}
		err := obj.SetContainment(world.Containment{LocationID: &locID})
		require.NoError(t, err)

		assert.NotNil(t, obj.LocationID)
		assert.Equal(t, locID, *obj.LocationID)
		assert.Nil(t, obj.HeldByCharacterID)
		assert.Nil(t, obj.ContainedInObjectID)
	})

	t.Run("set to character clears other fields", func(t *testing.T) {
		obj := &world.Object{
			ID:         ulid.Make(),
			Name:       "Test",
			LocationID: &locID,
		}
		err := obj.SetContainment(world.Containment{CharacterID: &charID})
		require.NoError(t, err)

		assert.Nil(t, obj.LocationID)
		assert.NotNil(t, obj.HeldByCharacterID)
		assert.Equal(t, charID, *obj.HeldByCharacterID)
		assert.Nil(t, obj.ContainedInObjectID)
	})

	t.Run("set to container clears other fields", func(t *testing.T) {
		obj := &world.Object{
			ID:                ulid.Make(),
			Name:              "Test",
			HeldByCharacterID: &charID,
		}
		err := obj.SetContainment(world.Containment{ObjectID: &objID})
		require.NoError(t, err)

		assert.Nil(t, obj.LocationID)
		assert.Nil(t, obj.HeldByCharacterID)
		assert.NotNil(t, obj.ContainedInObjectID)
		assert.Equal(t, objID, *obj.ContainedInObjectID)
	})

	t.Run("rejects invalid containment with multiple fields", func(t *testing.T) {
		obj := &world.Object{ID: ulid.Make(), Name: "Test"}
		err := obj.SetContainment(world.Containment{LocationID: &locID, CharacterID: &charID})
		assert.ErrorIs(t, err, world.ErrInvalidContainment)
	})

	t.Run("rejects empty containment", func(t *testing.T) {
		obj := &world.Object{ID: ulid.Make(), Name: "Test"}
		err := obj.SetContainment(world.Containment{})
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
			Name: "Sword",
		}
		assert.NoError(t, obj.Validate())
	})

	t.Run("invalid name", func(t *testing.T) {
		obj := &world.Object{
			Name: "",
		}
		err := obj.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("valid with description", func(t *testing.T) {
		obj := &world.Object{
			Name:        "Sword",
			Description: "A shiny blade.",
		}
		assert.NoError(t, obj.Validate())
	})
}

func TestObject_ValidateContainment(t *testing.T) {
	locID := ulid.Make()
	charID := ulid.Make()

	t.Run("valid object with location", func(t *testing.T) {
		obj := &world.Object{
			ID:         ulid.Make(),
			Name:       "Sword",
			LocationID: &locID,
		}
		assert.NoError(t, obj.ValidateContainment())
	})

	t.Run("invalid object with no containment", func(t *testing.T) {
		obj := &world.Object{
			ID:   ulid.Make(),
			Name: "Orphan",
		}
		err := obj.ValidateContainment()
		assert.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidContainment)
	})

	t.Run("invalid object with multiple containment", func(t *testing.T) {
		obj := &world.Object{
			ID:                ulid.Make(),
			Name:              "Confused",
			LocationID:        &locID,
			HeldByCharacterID: &charID,
		}
		err := obj.ValidateContainment()
		assert.Error(t, err)
		assert.ErrorIs(t, err, world.ErrInvalidContainment)
	})
}
