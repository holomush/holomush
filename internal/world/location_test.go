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
}
