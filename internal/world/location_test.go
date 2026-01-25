// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/world"
)

func TestLocationType_String(t *testing.T) {
	tests := []struct {
		name     string
		locType  world.LocationType
		expected string
	}{
		{"persistent", world.LocationTypePersistent, "persistent"},
		{"scene", world.LocationTypeScene, "scene"},
		{"instance", world.LocationTypeInstance, "instance"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.locType.String())
		})
	}
}

func TestLocationType_Validate(t *testing.T) {
	tests := []struct {
		name    string
		locType world.LocationType
		wantErr bool
	}{
		{"persistent is valid", world.LocationTypePersistent, false},
		{"scene is valid", world.LocationTypeScene, false},
		{"instance is valid", world.LocationTypeInstance, false},
		{"empty string is invalid", world.LocationType(""), true},
		{"arbitrary string is invalid", world.LocationType("dungeon"), true},
		{"similar but wrong is invalid", world.LocationType("Persistent"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.locType.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.ErrorIs(t, err, world.ErrInvalidLocationType)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestParseReplayPolicy(t *testing.T) {
	tests := []struct {
		name     string
		policy   string
		expected int
	}{
		{"none", "last:0", 0},
		{"ten", "last:10", 10},
		{"fifty", "last:50", 50},
		{"unlimited", "last:-1", -1},
		{"invalid prefix", "recent:10", 0},
		{"empty", "", 0},
		{"malformed non-integer", "last:abc", 0},
		{"malformed float", "last:1.5", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, world.ParseReplayPolicy(tt.policy))
		})
	}
}

func TestDefaultReplayPolicy(t *testing.T) {
	tests := []struct {
		name     string
		locType  world.LocationType
		expected string
	}{
		{"persistent", world.LocationTypePersistent, "last:0"},
		{"scene", world.LocationTypeScene, "last:-1"},
		{"instance", world.LocationTypeInstance, "last:0"},
		{"unknown type defaults to no replay", world.LocationType("unknown"), "last:0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, world.DefaultReplayPolicy(tt.locType))
		})
	}
}

func TestLocation_EffectiveDescription(t *testing.T) {
	parentID := ulid.Make()
	parent := &world.Location{
		ID:          parentID,
		Type:        world.LocationTypePersistent,
		Name:        "The Tavern",
		Description: "A cozy tavern with a roaring fire.",
	}

	t.Run("no shadow returns own description", func(t *testing.T) {
		loc := &world.Location{
			ID:          ulid.Make(),
			Type:        world.LocationTypePersistent,
			Name:        "Town Square",
			Description: "The center of town.",
		}
		assert.Equal(t, "The center of town.", loc.EffectiveDescription(nil))
	})

	t.Run("scene with shadow returns parent description", func(t *testing.T) {
		loc := &world.Location{
			ID:          ulid.Make(),
			Type:        world.LocationTypeScene,
			ShadowsID:   &parentID,
			Name:        "",
			Description: "",
		}
		assert.Equal(t, "A cozy tavern with a roaring fire.", loc.EffectiveDescription(parent))
	})

	t.Run("scene with override returns own description", func(t *testing.T) {
		loc := &world.Location{
			ID:          ulid.Make(),
			Type:        world.LocationTypeScene,
			ShadowsID:   &parentID,
			Name:        "Private Room",
			Description: "A private back room in the tavern.",
		}
		assert.Equal(t, "A private back room in the tavern.", loc.EffectiveDescription(parent))
	})

	t.Run("shadows_id set but parent nil returns empty description", func(t *testing.T) {
		loc := &world.Location{
			ID:          ulid.Make(),
			Type:        world.LocationTypeScene,
			ShadowsID:   &parentID,
			Name:        "Orphan Scene",
			Description: "",
		}
		// When parent is not loaded/passed, returns own (empty) description
		assert.Equal(t, "", loc.EffectiveDescription(nil))
	})
}

func TestLocation_EffectiveName(t *testing.T) {
	parentID := ulid.Make()
	parent := &world.Location{
		ID:          parentID,
		Type:        world.LocationTypePersistent,
		Name:        "The Tavern",
		Description: "A cozy tavern with a roaring fire.",
	}

	t.Run("no shadow returns own name", func(t *testing.T) {
		loc := &world.Location{
			ID:   ulid.Make(),
			Type: world.LocationTypePersistent,
			Name: "Town Square",
		}
		assert.Equal(t, "Town Square", loc.EffectiveName(nil))
	})

	t.Run("scene with shadow and empty name returns parent name", func(t *testing.T) {
		loc := &world.Location{
			ID:        ulid.Make(),
			Type:      world.LocationTypeScene,
			ShadowsID: &parentID,
			Name:      "",
		}
		assert.Equal(t, "The Tavern", loc.EffectiveName(parent))
	})

	t.Run("scene with override returns own name", func(t *testing.T) {
		loc := &world.Location{
			ID:        ulid.Make(),
			Type:      world.LocationTypeScene,
			ShadowsID: &parentID,
			Name:      "Private Room",
		}
		assert.Equal(t, "Private Room", loc.EffectiveName(parent))
	})

	t.Run("shadows_id set but parent nil returns own name", func(t *testing.T) {
		loc := &world.Location{
			ID:        ulid.Make(),
			Type:      world.LocationTypeScene,
			ShadowsID: &parentID,
			Name:      "",
		}
		// When parent is not loaded/passed, returns own (empty) name
		assert.Equal(t, "", loc.EffectiveName(nil))
	})
}

func TestNewLocation(t *testing.T) {
	t.Run("creates valid location", func(t *testing.T) {
		loc, err := world.NewLocation("Test Room", "A test room", world.LocationTypePersistent)
		assert.NoError(t, err)
		assert.False(t, loc.ID.IsZero())
		assert.Equal(t, "Test Room", loc.Name)
		assert.Equal(t, "A test room", loc.Description)
		assert.Equal(t, world.LocationTypePersistent, loc.Type)
		assert.Equal(t, world.DefaultReplayPolicy(world.LocationTypePersistent), loc.ReplayPolicy)
		assert.False(t, loc.CreatedAt.IsZero())
	})

	t.Run("returns error for empty name", func(t *testing.T) {
		_, err := world.NewLocation("", "description", world.LocationTypePersistent)
		assert.Error(t, err)
	})

	t.Run("returns error for invalid type", func(t *testing.T) {
		_, err := world.NewLocation("Room", "desc", world.LocationType("invalid"))
		assert.Error(t, err)
	})
}

func TestNewLocationWithID(t *testing.T) {
	t.Run("creates location with provided ID", func(t *testing.T) {
		id := ulid.Make()
		loc, err := world.NewLocationWithID(id, "Test Room", "desc", world.LocationTypeScene)
		assert.NoError(t, err)
		assert.Equal(t, id, loc.ID)
		assert.Equal(t, "last:-1", loc.ReplayPolicy) // Scene default
	})

	t.Run("returns error for zero ID", func(t *testing.T) {
		_, err := world.NewLocationWithID(ulid.ULID{}, "Room", "desc", world.LocationTypePersistent)
		assert.Error(t, err)
		var ve *world.ValidationError
		assert.ErrorAs(t, err, &ve)
		assert.Equal(t, "id", ve.Field)
	})
}

func TestLocation_Validate_ZeroID(t *testing.T) {
	loc := &world.Location{
		ID:   ulid.ULID{}, // zero
		Name: "Test",
		Type: world.LocationTypePersistent,
	}
	err := loc.Validate()
	assert.Error(t, err)
	var ve *world.ValidationError
	assert.ErrorAs(t, err, &ve)
	assert.Equal(t, "id", ve.Field)
}

func TestLocation_Validate(t *testing.T) {
	t.Run("valid location", func(t *testing.T) {
		loc := &world.Location{
			ID:   ulid.Make(),
			Name: "Town Square",
			Type: world.LocationTypePersistent,
		}
		assert.NoError(t, loc.Validate())
	})

	t.Run("invalid name", func(t *testing.T) {
		loc := &world.Location{
			ID:   ulid.Make(),
			Name: "",
			Type: world.LocationTypePersistent,
		}
		err := loc.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("invalid type", func(t *testing.T) {
		loc := &world.Location{
			ID:   ulid.Make(),
			Name: "Town Square",
			Type: world.LocationType("invalid"),
		}
		err := loc.Validate()
		assert.Error(t, err)
	})

	t.Run("valid with description", func(t *testing.T) {
		loc := &world.Location{
			ID:          ulid.Make(),
			Name:        "Town Square",
			Type:        world.LocationTypePersistent,
			Description: "A bustling town square.",
		}
		assert.NoError(t, loc.Validate())
	})
}
