// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"
	"testing"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createSeedEngine builds an Engine loaded with ALL seed policies and the
// given attribute providers. This exercises the full evaluation pipeline:
// target matching → attribute resolution → condition evaluation → deny-overrides.
func createSeedEngine(t *testing.T, providers []attribute.AttributeProvider) *Engine {
	t.Helper()

	seeds := SeedPolicies()
	dslTexts := make([]string, len(seeds))
	for i, s := range seeds {
		dslTexts[i] = s.DSLText
	}

	return createTestEngineWithPolicies(t, dslTexts, providers)
}

// --- Mock providers that return realistic attribute data ---

func characterProvider(subjectAttrs, resourceAttrs map[string]any) *mockAttributeProvider {
	return &mockAttributeProvider{
		namespace:   "character",
		subjectMap:  subjectAttrs,
		resourceMap: resourceAttrs,
		schema: &types.NamespaceSchema{
			Attributes: map[string]types.AttrType{
				"id":           types.AttrTypeString,
				"player_id":    types.AttrTypeString,
				"name":         types.AttrTypeString,
				"role":         types.AttrTypeString,
				"location":     types.AttrTypeString,
				"location_id":  types.AttrTypeString,
				"has_location": types.AttrTypeBool,
			},
		},
	}
}

func locationProvider(resourceAttrs map[string]any) *mockAttributeProvider {
	return &mockAttributeProvider{
		namespace:   "location",
		resourceMap: resourceAttrs,
		schema: &types.NamespaceSchema{
			Attributes: map[string]types.AttrType{
				"id":   types.AttrTypeString,
				"name": types.AttrTypeString,
			},
		},
	}
}

func commandProvider(resourceAttrs map[string]any) *mockAttributeProvider {
	return &mockAttributeProvider{
		namespace:   "command",
		resourceMap: resourceAttrs,
		schema: &types.NamespaceSchema{
			Attributes: map[string]types.AttrType{
				"name": types.AttrTypeString,
			},
		},
	}
}

func streamProvider(resourceAttrs map[string]any) *mockAttributeProvider {
	return &mockAttributeProvider{
		namespace:   "stream",
		resourceMap: resourceAttrs,
		schema: &types.NamespaceSchema{
			Attributes: map[string]types.AttrType{
				"name":     types.AttrTypeString,
				"location": types.AttrTypeString,
			},
		},
	}
}

func objectProvider(resourceAttrs map[string]any) *mockAttributeProvider {
	return &mockAttributeProvider{
		namespace:   "object",
		resourceMap: resourceAttrs,
		schema: &types.NamespaceSchema{
			Attributes: map[string]types.AttrType{
				"id":       types.AttrTypeString,
				"location": types.AttrTypeString,
			},
		},
	}
}

func propertyProvider(resourceAttrs map[string]any) *mockAttributeProvider {
	return &mockAttributeProvider{
		namespace:   "property",
		resourceMap: resourceAttrs,
		schema: &types.NamespaceSchema{
			Attributes: map[string]types.AttrType{
				"visibility":      types.AttrTypeString,
				"owner":           types.AttrTypeString,
				"parent_location": types.AttrTypeString,
				"visible_to":      types.AttrTypeStringList,
				"excluded_from":   types.AttrTypeStringList,
			},
		},
	}
}

// --- Smoke tests ---

func TestSeedSmoke_PlayerSelfAccess(t *testing.T) {
	charID := "01CHARSELF0000000000000000"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": charID, "role": "player", "location": "01LOC000"},
			map[string]any{"id": charID, "role": "player", "location": "01LOC000"},
		),
	})

	// Character reading itself → permit (seed:player-self-access)
	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  access.CharacterSubject(charID),
		Action:   "read",
		Resource: access.CharacterSubject(charID),
	})
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed(), "player should read own character; got: %s — %s", decision.Effect(), decision.Reason)
}

func TestSeedSmoke_PlayerLocationRead(t *testing.T) {
	locID := "01LOC000AAAAAAAAAAAAAAAAAA"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "role": "player", "location": locID},
			nil,
		),
		locationProvider(map[string]any{"id": locID, "name": "Town Square"}),
	})

	// Character reading current location → permit (seed:player-location-read)
	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01CHAR01",
		Action:   "read",
		Resource: "location:" + locID,
	})
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed(), "player should read current location; got: %s — %s", decision.Effect(), decision.Reason)
}

func TestSeedSmoke_PlayerColocatedCharacter(t *testing.T) {
	locID := "01LOC000BBBBBBBBBBBBBBBBBB"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR_A", "role": "player", "location": locID},
			map[string]any{"id": "01CHAR_B", "role": "player", "location": locID},
		),
	})

	// Reading a co-located character → permit (seed:player-character-colocation)
	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01CHAR_A",
		Action:   "read",
		Resource: "character:01CHAR_B",
	})
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed(), "player should read co-located character; got: %s — %s", decision.Effect(), decision.Reason)
}

func TestSeedSmoke_PlayerColocatedObject(t *testing.T) {
	locID := "01LOC000CCCCCCCCCCCCCCCCCC"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "role": "player", "location": locID},
			nil,
		),
		objectProvider(map[string]any{"id": "01OBJ001", "location": locID}),
	})

	// Reading a co-located object → permit (seed:player-object-colocation)
	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01CHAR01",
		Action:   "read",
		Resource: "object:01OBJ001",
	})
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed(), "player should read co-located object; got: %s — %s", decision.Effect(), decision.Reason)
}

func TestSeedSmoke_PlayerStreamEmit(t *testing.T) {
	locID := "01LOC000DDDDDDDDDDDDDDDDDD"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "role": "player", "location": locID},
			nil,
		),
		streamProvider(map[string]any{"name": "location:01LOC000", "location": locID}),
	})

	// Emitting to co-located location stream → permit (seed:player-stream-emit)
	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01CHAR01",
		Action:   "emit",
		Resource: "stream:location-01LOC000",
	})
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed(), "player should emit to co-located stream; got: %s — %s", decision.Effect(), decision.Reason)
}

func TestSeedSmoke_PlayerMovement(t *testing.T) {
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "role": "player", "location": "01LOC_A"},
			nil,
		),
		locationProvider(map[string]any{"id": "01LOC_B", "name": "Market"}),
	})

	// Entering any location → permit (seed:player-movement, no conditions)
	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01CHAR01",
		Action:   "enter",
		Resource: "location:01LOC_B",
	})
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed(), "player should enter location; got: %s — %s", decision.Effect(), decision.Reason)
}

func TestSeedSmoke_PlayerExitUse(t *testing.T) {
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "role": "player", "location": "01LOC000"},
			nil,
		),
	})

	// Using an exit → permit (seed:player-exit-use, no conditions)
	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01CHAR01",
		Action:   "use",
		Resource: "exit:01EXIT01",
	})
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed(), "player should use exit; got: %s — %s", decision.Effect(), decision.Reason)
}

func TestSeedSmoke_PlayerBasicCommands(t *testing.T) {
	commands := []string{"say", "pose", "look", "go"}
	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			engine := createSeedEngine(t, []attribute.AttributeProvider{
				characterProvider(
					map[string]any{"id": "01CHAR01", "role": "player", "location": "01LOC000"},
					nil,
				),
				commandProvider(map[string]any{"name": cmd}),
			})

			decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
				Subject:  "character:01CHAR01",
				Action:   "execute",
				Resource: "command:" + cmd,
			})
			require.NoError(t, err)
			assert.True(t, decision.IsAllowed(), "player should execute %s; got: %s — %s", cmd, decision.Effect(), decision.Reason)
		})
	}
}

func TestSeedSmoke_PlayerDeniedBuilderCommands(t *testing.T) {
	commands := []string{"dig", "create", "describe", "link"}
	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			engine := createSeedEngine(t, []attribute.AttributeProvider{
				characterProvider(
					map[string]any{"id": "01CHAR01", "role": "player", "location": "01LOC000"},
					nil,
				),
				commandProvider(map[string]any{"name": cmd}),
			})

			decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
				Subject:  "character:01CHAR01",
				Action:   "execute",
				Resource: "command:" + cmd,
			})
			require.NoError(t, err)
			assert.False(t, decision.IsAllowed(), "player should NOT execute builder command %s; got: %s — %s", cmd, decision.Effect(), decision.Reason)
		})
	}
}

func TestSeedSmoke_BuilderLocationWrite(t *testing.T) {
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "role": "builder", "location": "01LOC000"},
			nil,
		),
		locationProvider(map[string]any{"id": "01LOC001", "name": "Forest"}),
	})

	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01CHAR01",
		Action:   "write",
		Resource: "location:01LOC001",
	})
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed(), "builder should write locations; got: %s — %s", decision.Effect(), decision.Reason)
}

func TestSeedSmoke_BuilderCommands(t *testing.T) {
	commands := []string{"dig", "create", "describe", "link"}
	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			engine := createSeedEngine(t, []attribute.AttributeProvider{
				characterProvider(
					map[string]any{"id": "01CHAR01", "role": "builder", "location": "01LOC000"},
					nil,
				),
				commandProvider(map[string]any{"name": cmd}),
			})

			decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
				Subject:  "character:01CHAR01",
				Action:   "execute",
				Resource: "command:" + cmd,
			})
			require.NoError(t, err)
			assert.True(t, decision.IsAllowed(), "builder should execute %s; got: %s — %s", cmd, decision.Effect(), decision.Reason)
		})
	}
}

func TestSeedSmoke_AdminFullAccess(t *testing.T) {
	tests := []struct {
		name     string
		action   string
		resource string
	}{
		{"read location", "read", "location:01LOC001"},
		{"write location", "write", "location:01LOC001"},
		{"delete object", "delete", "object:01OBJ001"},
		{"execute any command", "execute", "command:teleport"},
		{"enter locked room", "enter", "location:01LOCKED"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Admin needs providers registered for target matching
			engine := createSeedEngine(t, []attribute.AttributeProvider{
				characterProvider(
					map[string]any{"id": "01ADMIN1", "role": "admin", "location": "01LOC000"},
					nil,
				),
				locationProvider(map[string]any{"id": "01LOC001"}),
				objectProvider(map[string]any{"id": "01OBJ001"}),
				commandProvider(map[string]any{"name": "teleport"}),
			})

			decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
				Subject:  "character:01ADMIN1",
				Action:   tt.action,
				Resource: tt.resource,
			})
			require.NoError(t, err)
			assert.True(t, decision.IsAllowed(), "admin should have access to %s; got: %s — %s", tt.name, decision.Effect(), decision.Reason)
		})
	}
}

func TestSeedSmoke_DefaultDenyNoMatchingPolicy(t *testing.T) {
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "role": "player", "location": "01LOC000"},
			nil,
		),
	})

	// Player trying to delete a location → no policy matches → default deny
	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01CHAR01",
		Action:   "delete",
		Resource: "location:01LOC001",
	})
	require.NoError(t, err)
	assert.False(t, decision.IsAllowed(), "default deny should apply; got: %s — %s", decision.Effect(), decision.Reason)
	assert.Equal(t, types.EffectDefaultDeny, decision.Effect())
}

func TestSeedSmoke_PropertyPublicRead(t *testing.T) {
	locID := "01LOC000EEEEEEEEEEEEEEEEEE"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "role": "player", "location": locID},
			nil,
		),
		propertyProvider(map[string]any{
			"visibility":      "public",
			"owner":           "01OWNER1",
			"parent_location": locID,
		}),
	})

	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01CHAR01",
		Action:   "read",
		Resource: "property:01PROP01",
	})
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed(), "co-located player should read public property; got: %s — %s", decision.Effect(), decision.Reason)
}

func TestSeedSmoke_PropertyPrivateReadOwner(t *testing.T) {
	charID := "01CHAROWNER00000000000000"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": charID, "role": "player", "location": "01LOC000"},
			nil,
		),
		propertyProvider(map[string]any{
			"visibility": "private",
			"owner":      charID,
		}),
	})

	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  access.CharacterSubject(charID),
		Action:   "read",
		Resource: "property:01PROP01",
	})
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed(), "owner should read private property; got: %s — %s", decision.Effect(), decision.Reason)
}

func TestSeedSmoke_PropertyPrivateReadDeniedNonOwner(t *testing.T) {
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "role": "player", "location": "01LOC000"},
			nil,
		),
		propertyProvider(map[string]any{
			"visibility": "private",
			"owner":      "01OTHERID",
		}),
	})

	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01CHAR01",
		Action:   "read",
		Resource: "property:01PROP01",
	})
	require.NoError(t, err)
	assert.False(t, decision.IsAllowed(), "non-owner should NOT read private property; got: %s — %s", decision.Effect(), decision.Reason)
}

func TestSeedSmoke_PropertyRestrictedForbid(t *testing.T) {
	charID := "01CHAREXCLUDED00000000000"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": charID, "role": "player", "location": "01LOC000"},
			nil,
		),
		propertyProvider(map[string]any{
			"visibility":    "restricted",
			"owner":         "01OWNER1",
			"visible_to":    []any{charID},
			"excluded_from": []any{charID},
		}),
	})

	// Character is in both visible_to and excluded_from — forbid overrides permit
	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  access.CharacterSubject(charID),
		Action:   "read",
		Resource: "property:01PROP01",
	})
	require.NoError(t, err)
	assert.False(t, decision.IsAllowed(), "forbid should override permit for excluded character; got: %s — %s", decision.Effect(), decision.Reason)
}

func TestSeedSmoke_PropertyOwnerWrite(t *testing.T) {
	charID := "01CHAROWNWRITE00000000000"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": charID, "role": "player", "location": "01LOC000"},
			nil,
		),
		propertyProvider(map[string]any{
			"owner": charID,
		}),
	})

	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  access.CharacterSubject(charID),
		Action:   "write",
		Resource: "property:01PROP01",
	})
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed(), "owner should write property; got: %s — %s", decision.Effect(), decision.Reason)
}

func TestSeedSmoke_PlayerExitRead(t *testing.T) {
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "role": "player", "location": "01LOC000"},
			nil,
		),
	})

	// G1: players can read exits
	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01CHAR01",
		Action:   "read",
		Resource: "exit:01EXIT01",
	})
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed(), "player should read exit (G1); got: %s — %s", decision.Effect(), decision.Reason)
}

func TestSeedSmoke_BuilderExitWrite(t *testing.T) {
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01BUILD1", "role": "builder", "location": "01LOC000"},
			nil,
		),
	})

	for _, action := range []string{"write", "delete"} {
		t.Run(action, func(t *testing.T) {
			decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
				Subject:  "character:01BUILD1",
				Action:   action,
				Resource: "exit:01EXIT01",
			})
			require.NoError(t, err)
			assert.True(t, decision.IsAllowed(), "builder should %s exit (G2); got: %s — %s", action, decision.Effect(), decision.Reason)
		})
	}
}

func TestSeedSmoke_PlayerLocationListCharacters(t *testing.T) {
	locID := "01LOC000FFFFFFFFFFFFFFFF"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "role": "player", "location": locID},
			nil,
		),
		locationProvider(map[string]any{"id": locID, "name": "Plaza"}),
	})

	// G3: list_characters on current location
	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01CHAR01",
		Action:   "list_characters",
		Resource: "location:" + locID,
	})
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed(), "player should list characters at current location (G3); got: %s — %s", decision.Effect(), decision.Reason)
}

func TestSeedSmoke_PlayerSceneAccess(t *testing.T) {
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "role": "player", "location": "01LOC000"},
			nil,
		),
	})

	// G4: read and write scenes
	for _, action := range []string{"read", "write"} {
		t.Run(action, func(t *testing.T) {
			decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
				Subject:  "character:01CHAR01",
				Action:   action,
				Resource: "scene:01SCENE01",
			})
			require.NoError(t, err)
			assert.True(t, decision.IsAllowed(), "player should %s scene (G4); got: %s — %s", action, decision.Effect(), decision.Reason)
		})
	}
}

func TestSeedSmoke_PlayerDeniedLocationWrite(t *testing.T) {
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "role": "player", "location": "01LOC000"},
			nil,
		),
		locationProvider(map[string]any{"id": "01LOC001", "name": "Forest"}),
	})

	// Player (non-builder) writing a location → denied
	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01CHAR01",
		Action:   "write",
		Resource: "location:01LOC001",
	})
	require.NoError(t, err)
	assert.False(t, decision.IsAllowed(), "player should NOT write location; got: %s — %s", decision.Effect(), decision.Reason)
}

func TestSeedSmoke_PlayerDeniedOtherLocationRead(t *testing.T) {
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "role": "player", "location": "01LOC_A0"},
			nil,
		),
		locationProvider(map[string]any{"id": "01LOC_B0", "name": "Elsewhere"}),
	})

	// Player reading a location they are NOT at → denied
	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01CHAR01",
		Action:   "read",
		Resource: "location:01LOC_B0",
	})
	require.NoError(t, err)
	assert.False(t, decision.IsAllowed(), "player should NOT read non-current location; got: %s — %s", decision.Effect(), decision.Reason)
}
