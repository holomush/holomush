// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"
	"testing"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/plugin/hostcap"
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
				"roles":        types.AttrTypeStringList,
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
				"name":         types.AttrTypeString,
				"location":     types.AttrTypeString,
				"has_location": types.AttrTypeBool,
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

func TestSeedSmokePlayerSelfAccess(t *testing.T) {
	charID := "01CHARSELF0000000000000000"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": charID, "roles": []string{"player"}, "location": "01LOC000"},
			map[string]any{"id": charID, "roles": []string{"player"}, "location": "01LOC000"},
		),
	})

	// Character reading itself → permit (seed:player-self-access)
	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  access.CharacterSubject(charID),
		Action:   "read",
		Resource: access.CharacterResource(charID),
	})
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed(), "player should read own character; got: %s — %s", decision.Effect(), decision.Reason())
}

func TestSeedSmokePlayerLocationRead(t *testing.T) {
	locID := "01LOC000AAAAAAAAAAAAAAAAAA"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "roles": []string{"player"}, "location": locID},
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
	assert.True(t, decision.IsAllowed(), "player should read current location; got: %s — %s", decision.Effect(), decision.Reason())
}

func TestSeedSmokePlayerColocatedCharacter(t *testing.T) {
	locID := "01LOC000BBBBBBBBBBBBBBBBBB"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR_A", "roles": []string{"player"}, "location": locID},
			map[string]any{"id": "01CHAR_B", "roles": []string{"player"}, "location": locID},
		),
	})

	// Reading a co-located character → permit (seed:player-character-colocation)
	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01CHAR_A",
		Action:   "read",
		Resource: "character:01CHAR_B",
	})
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed(), "player should read co-located character; got: %s — %s", decision.Effect(), decision.Reason())
}

func TestSeedSmokePlayerColocatedObject(t *testing.T) {
	locID := "01LOC000CCCCCCCCCCCCCCCCCC"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "roles": []string{"player"}, "location": locID},
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
	assert.True(t, decision.IsAllowed(), "player should read co-located object; got: %s — %s", decision.Effect(), decision.Reason())
}

func TestSeedSmokePlayerStreamEmit(t *testing.T) {
	locID := "01LOC000DDDDDDDDDDDDDDDDDD"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "roles": []string{"player"}, "location": locID},
			nil,
		),
		streamProvider(map[string]any{"name": "events.main.location.01LOC000", "location": locID, "has_location": true}),
	})

	// Emitting to co-located location stream → permit (seed:player-stream-emit)
	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01CHAR01",
		Action:   "emit",
		Resource: "stream:events.main.location.01LOC000",
	})
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed(), "player should emit to co-located stream; got: %s — %s", decision.Effect(), decision.Reason())
}

func TestSeedSmokePlayerCanReadCoLocatedLocationStream(t *testing.T) {
	locID := "01LOC000GGGGGGGGGGGGGGGGGG"

	// Use the real StreamProvider so the test exercises parser + policy
	// together — catches regressions in StreamProvider registration or
	// resource ID parsing (the stub-based alternative would pass even if
	// the real provider were misconfigured, hiding bugs like the one
	// caught by B9's E2E test when StreamProvider was not registered).
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "roles": []string{"player"}, "location": locID},
			nil,
		),
		attribute.NewStreamProvider(),
	})

	// Reading history of co-located location stream → permit
	// (seed:player-location-stream-read) — dot-form subject: events.<gid>.location.<ULID>
	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01CHAR01",
		Action:   "read",
		Resource: "stream:events.main.location." + locID,
	})
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed(), "co-located character should read location stream; got: %s — %s", decision.Effect(), decision.Reason())
}

func TestSeedSmokePlayerCannotReadNonCoLocatedLocationStream(t *testing.T) {
	currentLocID := "01LOC000HHHHHHHHHHHHHHHHHH"
	otherLocID := "01LOC000IIIIIIIIIIIIIIIIII"

	// Real StreamProvider — see rationale in
	// TestSeedSmokePlayerCanReadCoLocatedLocationStream.
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "roles": []string{"player"}, "location": currentLocID},
			nil,
		),
		attribute.NewStreamProvider(),
	})

	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01CHAR01",
		Action:   "read",
		Resource: "stream:events.main.location." + otherLocID,
	})
	require.NoError(t, err)
	assert.False(t, decision.IsAllowed(), "non-co-located character should NOT read location stream; got: %s — %s", decision.Effect(), decision.Reason())
}

func TestSeedSmokePlayerMovement(t *testing.T) {
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "roles": []string{"player"}, "location": "01LOC_A"},
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
	assert.True(t, decision.IsAllowed(), "player should enter location; got: %s — %s", decision.Effect(), decision.Reason())
}

func TestSeedSmokePlayerExitUse(t *testing.T) {
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "roles": []string{"player"}, "location": "01LOC000"},
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
	assert.True(t, decision.IsAllowed(), "player should use exit; got: %s — %s", decision.Effect(), decision.Reason())
}

func TestSeedSmoke_PlayerBasicCommands(t *testing.T) {
	commands := []string{"quit", "look", "go", "who"}
	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			engine := createSeedEngine(t, []attribute.AttributeProvider{
				characterProvider(
					map[string]any{"id": "01CHAR01", "roles": []string{"player"}, "location": "01LOC000"},
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
			assert.True(t, decision.IsAllowed(), "player should execute %s; got: %s — %s", cmd, decision.Effect(), decision.Reason())
		})
	}
}

func TestSeedSmoke_PlayerDeniedBuilderCommands(t *testing.T) {
	commands := []string{"dig", "create", "describe", "link"}
	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			engine := createSeedEngine(t, []attribute.AttributeProvider{
				characterProvider(
					map[string]any{"id": "01CHAR01", "roles": []string{"player"}, "location": "01LOC000"},
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
			assert.False(t, decision.IsAllowed(), "player should NOT execute builder command %s; got: %s — %s", cmd, decision.Effect(), decision.Reason())
		})
	}
}

func TestSeedSmokeBuilderLocationWrite(t *testing.T) {
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "roles": []string{"builder"}, "location": "01LOC000"},
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
	assert.True(t, decision.IsAllowed(), "builder should write locations; got: %s — %s", decision.Effect(), decision.Reason())
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
					map[string]any{"id": "01ADMIN1", "roles": []string{"admin"}, "location": "01LOC000"},
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
			assert.True(t, decision.IsAllowed(), "admin should have access to %s; got: %s — %s", tt.name, decision.Effect(), decision.Reason())
		})
	}
}

func TestSeedSmokeDefaultDenyNoMatchingPolicy(t *testing.T) {
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "roles": []string{"player"}, "location": "01LOC000"},
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
	assert.False(t, decision.IsAllowed(), "default deny should apply; got: %s — %s", decision.Effect(), decision.Reason())
	assert.Equal(t, types.EffectDefaultDeny, decision.Effect())
}

func TestSeedSmokePropertyPublicRead(t *testing.T) {
	locID := "01LOC000EEEEEEEEEEEEEEEEEE"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "roles": []string{"player"}, "location": locID},
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
	assert.True(t, decision.IsAllowed(), "co-located player should read public property; got: %s — %s", decision.Effect(), decision.Reason())
}

func TestSeedSmokePropertyPrivateReadOwner(t *testing.T) {
	charID := "01CHAROWNER00000000000000"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": charID, "roles": []string{"player"}, "location": "01LOC000"},
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
	assert.True(t, decision.IsAllowed(), "owner should read private property; got: %s — %s", decision.Effect(), decision.Reason())
}

func TestSeedSmokePropertyPrivateReadDeniedNonOwner(t *testing.T) {
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "roles": []string{"player"}, "location": "01LOC000"},
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
	assert.False(t, decision.IsAllowed(), "non-owner should NOT read private property; got: %s — %s", decision.Effect(), decision.Reason())
}

func TestSeedSmokePropertyRestrictedForbid(t *testing.T) {
	charID := "01CHAREXCLUDED00000000000"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": charID, "roles": []string{"player"}, "location": "01LOC000"},
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
	assert.False(t, decision.IsAllowed(), "forbid should override permit for excluded character; got: %s — %s", decision.Effect(), decision.Reason())
}

func TestSeedSmokePropertyOwnerWrite(t *testing.T) {
	charID := "01CHAROWNWRITE00000000000"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": charID, "roles": []string{"player"}, "location": "01LOC000"},
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
	assert.True(t, decision.IsAllowed(), "owner should write property; got: %s — %s", decision.Effect(), decision.Reason())
}

func TestSeedSmokePlayerExitRead(t *testing.T) {
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "roles": []string{"player"}, "location": "01LOC000"},
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
	assert.True(t, decision.IsAllowed(), "player should read exit (G1); got: %s — %s", decision.Effect(), decision.Reason())
}

func TestSeedSmoke_BuilderExitWrite(t *testing.T) {
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01BUILD1", "roles": []string{"builder"}, "location": "01LOC000"},
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
			assert.True(t, decision.IsAllowed(), "builder should %s exit (G2); got: %s — %s", action, decision.Effect(), decision.Reason())
		})
	}
}

func TestSeedSmokePlayerLocationListCharacters(t *testing.T) {
	locID := "01LOC000FFFFFFFFFFFFFFFF"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "roles": []string{"player"}, "location": locID},
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
	assert.True(t, decision.IsAllowed(), "player should list characters at current location (G3); got: %s — %s", decision.Effect(), decision.Reason())
}

func TestSeedSmokePlayerDeniedListCharactersNonCurrentLocation(t *testing.T) {
	currentLocID := "01LOC000AAAAAAAAAAAAAA"
	otherLocID := "01LOC000BBBBBBBBBBBBBB"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "roles": []string{"player"}, "location": currentLocID},
			nil,
		),
		locationProvider(map[string]any{"id": otherLocID, "name": "Other Room"}),
	})

	// Player at currentLocID should NOT be able to list_characters at otherLocID.
	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01CHAR01",
		Action:   "list_characters",
		Resource: "location:" + otherLocID,
	})
	require.NoError(t, err)
	assert.False(t, decision.IsAllowed(), "player should NOT list characters at non-current location; got: %s — %s", decision.Effect(), decision.Reason())
}

func TestSeedSmokeAdminLocationListCharacters(t *testing.T) {
	locID := "01LOC000FFFFFFFFFFFFFFFF"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01ADMIN1", "roles": []string{"admin"}, "location": locID},
			nil,
		),
		locationProvider(map[string]any{"id": locID, "name": "Plaza"}),
	})

	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01ADMIN1",
		Action:   "list_characters",
		Resource: "location:" + locID,
	})
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed(), "admin should list characters at location; got: %s — %s", decision.Effect(), decision.Reason())
}

func TestSeedSmokeBuilderLocationListCharacters(t *testing.T) {
	locID := "01LOC000FFFFFFFFFFFFFFFF"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01BUILD1", "roles": []string{"builder"}, "location": locID},
			nil,
		),
		locationProvider(map[string]any{"id": locID, "name": "Plaza"}),
	})

	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01BUILD1",
		Action:   "list_characters",
		Resource: "location:" + locID,
	})
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed(), "builder should list characters at location; got: %s — %s", decision.Effect(), decision.Reason())
}

func TestSeedSmokeAdminReadsNonCoLocatedLocationStream(t *testing.T) {
	// Admin should be able to read history of ANY public (location) stream
	// via seed:admin-full-access, even when not co-located. This closes a
	// coverage gap in the B9 QueryStreamHistory integration tests, which
	// stub ABAC with AllowAllEngine.
	adminLocID := "01ADMINLOC000FFFFFFFFFFFF"
	targetLocID := "01TARGETLOC00FFFFFFFFFFFF"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01ADMIN2", "roles": []string{"admin"}, "location": adminLocID},
			nil,
		),
		attribute.NewStreamProvider(),
	})

	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01ADMIN2",
		Action:   "read",
		Resource: "stream:events.main.location." + targetLocID,
	})
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed(), "admin should read non-co-located location stream; got: %s — %s", decision.Effect(), decision.Reason())
}

func TestSeedSmoke_PlayerSceneAccess(t *testing.T) {
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "roles": []string{"player"}, "location": "01LOC000"},
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
			assert.True(t, decision.IsAllowed(), "player should %s scene (G4); got: %s — %s", action, decision.Effect(), decision.Reason())
		})
	}
}

func TestSeedSmokePlayerDeniedLocationWrite(t *testing.T) {
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "roles": []string{"player"}, "location": "01LOC000"},
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
	assert.False(t, decision.IsAllowed(), "player should NOT write location; got: %s — %s", decision.Effect(), decision.Reason())
}

func TestSeedSmokePlayerDeniedOtherLocationRead(t *testing.T) {
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "roles": []string{"player"}, "location": "01LOC_A0"},
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
	assert.False(t, decision.IsAllowed(), "player should NOT read non-current location; got: %s — %s", decision.Effect(), decision.Reason())
}

// Phase-2 command smoke tests

func TestSeedSmoke_PlayerTeleportAndHomeCommands(t *testing.T) {
	commands := []string{"teleport", "home"}
	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			engine := createSeedEngine(t, []attribute.AttributeProvider{
				characterProvider(
					map[string]any{"id": "01CHAR01", "roles": []string{"player"}, "location": "01LOC000"},
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
			assert.True(t, decision.IsAllowed(), "player should execute %s; got: %s — %s", cmd, decision.Effect(), decision.Reason())
		})
	}
}

func TestSeedSmokePemitDeniedForPlayers(t *testing.T) {
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "roles": []string{"player"}, "location": "01LOC000"},
			nil,
		),
		commandProvider(map[string]any{"name": "pemit"}),
	})

	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01CHAR01",
		Action:   "execute",
		Resource: "command:pemit",
	})
	require.NoError(t, err)
	assert.False(t, decision.IsAllowed(), "regular player should NOT execute pemit; got: %s — %s", decision.Effect(), decision.Reason())
}

func TestSeedSmokePemitAllowedForAdmin(t *testing.T) {
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01ADMIN1", "roles": []string{"admin"}, "location": "01LOC000"},
			nil,
		),
		commandProvider(map[string]any{"name": "pemit"}),
	})

	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01ADMIN1",
		Action:   "execute",
		Resource: "command:pemit",
	})
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed(), "admin should execute pemit; got: %s — %s", decision.Effect(), decision.Reason())
}

// Phase-5 sub-epic E ABAC-layer enforcement smoke tests (A16 / INV-ACCESS-7 extension)
//
// These tests verify that the ABAC engine (with seed policies loaded) denies
// character and plugin principals from reading events.*.system.rekey.* streams.
// The deny is enforced by seed:deny-events-system-read-{character,plugin} which
// matches the broad events.*.system.* pattern. This is the authoritative ABAC
// gate per master spec §4.6 + §7.7; the AUDIT_ONLY rendering filter is defense-in-depth.

func TestSeedSmokeCharacterDeniedEventsSystemRekeyStream(t *testing.T) {
	// A regular player character must NOT read the rekey audit stream.
	// The broad seed:deny-events-system-read-character forbid must fire.
	const streamName = "events.01GAME01.system.rekey.01CT000.01CID00"
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "roles": []string{"player"}, "location": "01LOC000"},
			nil,
		),
		streamProvider(map[string]any{"name": streamName}),
	})

	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01CHAR01",
		Action:   "read",
		Resource: "stream:" + streamName,
	})
	require.NoError(t, err)
	assert.False(t, decision.IsAllowed(),
		"character must NOT read events.*.system.rekey.* stream (ABAC seed gate A16/INV-ACCESS-7); got: %s — %s",
		decision.Effect(), decision.Reason())
}

func TestSeedSmokePlayerLocationListPresence(t *testing.T) {
	locID := "01LOC000PPPPPPPPPPPPPPPPP"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "roles": []string{"player"}, "location": locID},
			nil,
		),
		locationProvider(map[string]any{"id": locID, "name": "Plaza"}),
	})

	// list_presence on current location → permit (list_presence_same_location)
	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01CHAR01",
		Action:   "list_presence",
		Resource: "location:" + locID,
	})
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed(), "player should list presence at current location; got: %s — %s", decision.Effect(), decision.Reason())
}

func TestSeedSmokePlayerDeniedListPresenceNonCurrentLocation(t *testing.T) {
	currentLocID := "01LOC000QQQQQQQQQQQQQQQQQ"
	otherLocID := "01LOC000RRRRRRRRRRRRRRRRR"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01CHAR01", "roles": []string{"player"}, "location": currentLocID},
			nil,
		),
		locationProvider(map[string]any{"id": otherLocID, "name": "Other Room"}),
	})

	// Player at currentLocID should NOT list_presence at otherLocID.
	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01CHAR01",
		Action:   "list_presence",
		Resource: "location:" + otherLocID,
	})
	require.NoError(t, err)
	assert.False(t, decision.IsAllowed(), "player should NOT list presence at non-current location; got: %s — %s", decision.Effect(), decision.Reason())
}

func TestSeedSmokeAdminRemoteListPresence(t *testing.T) {
	adminLocID := "01LOC000SSSSSSSSSSSSSSSSS"
	remoteLocID := "01LOC000TTTTTTTTTTTTTTTTT"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		characterProvider(
			map[string]any{"id": "01ADMIN1", "roles": []string{"admin"}, "location": adminLocID},
			nil,
		),
		locationProvider(map[string]any{"id": remoteLocID, "name": "Remote Room"}),
	})

	// Admin at a different location → permit via seed:admin-full-access super-rule
	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "character:01ADMIN1",
		Action:   "list_presence",
		Resource: "location:" + remoteLocID,
	})
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed(), "admin should list presence at remote location via super-rule; got: %s — %s", decision.Effect(), decision.Reason())
}

// pluginProvider builds a "plugin"-namespace mock provider with the given
// subject attributes (e.g. {"name": "builder-bot"}).
func pluginProvider(subjectAttrs map[string]any) *mockAttributeProvider {
	return &mockAttributeProvider{
		namespace:  "plugin",
		subjectMap: subjectAttrs,
		schema: &types.NamespaceSchema{
			Attributes: map[string]types.AttrType{
				"name": types.AttrTypeString,
			},
		},
	}
}

// Verifies: INV-PLUGIN-50
func TestSeedSmokePluginWorldMutationOwnLocationPermitsMatch(t *testing.T) {
	locID := "01LOC000WWWWWWWWWWWWWWWWWW"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		pluginProvider(map[string]any{"name": "builder-bot"}),
		locationProvider(map[string]any{"id": locID, "name": "Workshop"}),
	})

	// A plugin writing to a location that IS the acting character's dispatch
	// location → permit (seed:plugin-world-mutation-own-location). The
	// dispatch_location action attribute is the host-vouched acting-character
	// location, overlaid onto bags.Action by the engine (as the interceptor
	// supplies it via CapabilityInput.Context).
	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:    access.PluginSubject("builder-bot"),
		Action:     "write",
		Resource:   "location:" + locID,
		Attributes: map[string]any{"dispatch_location": locID},
	})
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed(),
		"plugin should write its own dispatch location; got: %s — %s", decision.Effect(), decision.Reason())
}

// Verifies: INV-PLUGIN-50
func TestSeedSmokePluginWorldMutationOwnLocationDeniesMismatch(t *testing.T) {
	dispatchLoc := "01LOC000XXXXXXXXXXXXXXXXXX"
	otherLoc := "01LOC000YYYYYYYYYYYYYYYYYY"

	engine := createSeedEngine(t, []attribute.AttributeProvider{
		pluginProvider(map[string]any{"name": "builder-bot"}),
		locationProvider(map[string]any{"id": otherLoc, "name": "Elsewhere"}),
	})

	// Plugin writing to a location that is NOT the dispatch location → default deny.
	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:    access.PluginSubject("builder-bot"),
		Action:     "write",
		Resource:   "location:" + otherLoc,
		Attributes: map[string]any{"dispatch_location": dispatchLoc},
	})
	require.NoError(t, err)
	assert.False(t, decision.IsAllowed(),
		"plugin must NOT write a location other than its dispatch location; got: %s — %s", decision.Effect(), decision.Reason())
}

func TestSeedSmokePluginDeniedEventsSystemRekeyStream(t *testing.T) {
	// A plugin principal must NOT read the rekey audit stream.
	// The broad seed:deny-events-system-read-plugin forbid must fire.
	const streamName = "events.01GAME01.system.rekey.01CT000.01CID00"
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		&mockAttributeProvider{
			namespace:  "plugin",
			subjectMap: map[string]any{"name": "echo-bot"},
			schema: &types.NamespaceSchema{
				Attributes: map[string]types.AttrType{
					"name": types.AttrTypeString,
				},
			},
		},
		streamProvider(map[string]any{"name": streamName}),
	})

	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  "plugin:echo-bot",
		Action:   "read",
		Resource: "stream:" + streamName,
	})
	require.NoError(t, err)
	assert.False(t, decision.IsAllowed(),
		"plugin must NOT read events.*.system.rekey.* stream (ABAC seed gate A16/INV-ACCESS-7); got: %s — %s",
		decision.Effect(), decision.Reason())
}

// Verifies: INV-PLUGIN-50
func TestSeedSmokePluginNonScopedCapabilityPermittedByDefaultSeed(t *testing.T) {
	// A non-scoped host capability (kv read) is evaluated at the capability type
	// level (resource "kv:*", as the interceptor supplies it). The per-capability
	// default-permit seed (seed:plugin-cap-kv) MUST permit it so a declared,
	// undifferentiated capability call succeeds absent an operator forbid.
	engine := createSeedEngine(t, []attribute.AttributeProvider{
		pluginProvider(map[string]any{"name": "core-objects"}),
	})

	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  access.PluginSubject("core-objects"),
		Action:   "read",
		Resource: "kv:*",
	})
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed(),
		"default-permit seed should authorize a declared non-scoped capability; got: %s — %s",
		decision.Effect(), decision.Reason())
}

// Verifies: INV-PLUGIN-50
func TestSeedSmokePluginNonScopedCapabilityDeniedByOperatorForbid(t *testing.T) {
	// Declaration is necessary but NOT sufficient: an operator forbid policy on a
	// capability resource type overrides the default-permit seed, making a
	// declared non-scoped capability unreachable (INV-PLUGIN-50).
	seeds := SeedPolicies()
	dslTexts := make([]string, 0, len(seeds)+1)
	for _, s := range seeds {
		dslTexts = append(dslTexts, s.DSLText)
	}
	dslTexts = append(dslTexts,
		`forbid(principal is plugin, action in ["read", "write"], resource is kv);`)

	engine := createTestEngineWithPolicies(t, dslTexts, []attribute.AttributeProvider{
		pluginProvider(map[string]any{"name": "core-objects"}),
	})

	decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
		Subject:  access.PluginSubject("core-objects"),
		Action:   "read",
		Resource: "kv:*",
	})
	require.NoError(t, err)
	assert.False(t, decision.IsAllowed(),
		"operator forbid must override the default-permit seed for a declared capability; got: %s — %s",
		decision.Effect(), decision.Reason())
}

// Verifies: INV-PLUGIN-50
func TestEverySeededCapabilityResourceHasDefaultPermit(t *testing.T) {
	// Drift guard: every served, non-exempt, NON-scope-eligible capability method
	// in hostcap.Descriptors MUST be authorized by a default-permit seed at the
	// type level (resource "<type>:*", exactly how the interceptor evaluates a
	// non-scoped call). A capability added later without its seed would fail
	// closed at runtime; this test catches that at build time — the seed-side
	// analogue of the INV-PLUGIN-52 extractor-completeness meta-test.
	//
	// Scope-eligible methods are intentionally skipped: they are gated by the
	// own-location seed and proven by the scoped smoke tests, which require a
	// concrete resource + dispatch_location this type-level probe does not supply.
	// Exempt (self-gated) capabilities short-circuit before the ABAC gate.
	engine := createSeedEngine(t, nil) // unconditional permits resolve without providers

	for token, desc := range hostcap.Descriptors {
		if hostcap.IsDeclarationExempt(token) {
			continue
		}
		for method, md := range desc.Methods {
			if len(md.Scopes) > 0 {
				continue
			}
			t.Run(token+"/"+method, func(t *testing.T) {
				decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
					Subject:  access.PluginSubject("drift-probe"),
					Action:   md.Action,
					Resource: md.Resource + ":*",
				})
				require.NoError(t, err)
				assert.True(t, decision.IsAllowed(),
					"non-exempt non-scoped capability %s/%s (action=%q resource=%q) has no default-permit seed — it would fail closed at the interceptor; add a seed:plugin-cap-* permit for resource %q",
					token, method, md.Action, md.Resource, md.Resource)
			})
		}
	}
}
