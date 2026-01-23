// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/world"
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
