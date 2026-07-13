// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/wmodel"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockObjectRepository is a simple mock for testing.
type mockObjectRepository struct {
	getFunc func(ctx context.Context, id ulid.ULID) (*world.Object, error)
}

func (m *mockObjectRepository) Get(ctx context.Context, id ulid.ULID) (*world.Object, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, id)
	}
	return nil, errors.New("not implemented")
}

func (m *mockObjectRepository) Create(_ context.Context, _ *world.Object) (*wmodel.MutationDelta, error) {
	return nil, errors.New("not implemented")
}

func (m *mockObjectRepository) Update(_ context.Context, _ *world.Object) (*wmodel.MutationDelta, error) {
	return nil, errors.New("not implemented")
}

func (m *mockObjectRepository) Delete(_ context.Context, _ ulid.ULID, _ int) (*wmodel.MutationDelta, error) {
	return nil, errors.New("not implemented")
}

func (m *mockObjectRepository) ListAtLocation(_ context.Context, _ ulid.ULID) ([]*world.Object, error) {
	return nil, errors.New("not implemented")
}

func (m *mockObjectRepository) ListHeldBy(_ context.Context, _ ulid.ULID) ([]*world.Object, error) {
	return nil, errors.New("not implemented")
}

func (m *mockObjectRepository) ListContainedIn(_ context.Context, _ ulid.ULID) ([]*world.Object, error) {
	return nil, errors.New("not implemented")
}

func (m *mockObjectRepository) Move(_ context.Context, _ ulid.ULID, _ world.Containment, _ int) (*wmodel.MutationDelta, error) {
	return nil, errors.New("not implemented")
}

// newObjectInLocation builds a test Object directly in a location.
// Uses the unexported field-injection path via SetContainment to bypass NewObject's name validation.
func newObjectInLocation(t *testing.T, id, locationID ulid.ULID, name string) *world.Object {
	t.Helper()
	obj := &world.Object{
		ID:        id,
		Name:      name,
		CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, obj.SetContainment(world.InLocation(locationID)))
	return obj
}

func newObjectHeldByCharacter(t *testing.T, id, characterID ulid.ULID, name string) *world.Object {
	t.Helper()
	obj := &world.Object{
		ID:        id,
		Name:      name,
		CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, obj.SetContainment(world.HeldByCharacter(characterID)))
	return obj
}

func newObjectContainedIn(t *testing.T, id, containerID ulid.ULID, name string) *world.Object {
	t.Helper()
	obj := &world.Object{
		ID:        id,
		Name:      name,
		CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, obj.SetContainment(world.ContainedInObject(containerID)))
	return obj
}

func TestObjectProviderContract(t *testing.T) {
	assertProviderContract(t, NewObjectProvider(&mockObjectRepository{}, &mockCharacterRepository{}))
}

func TestObjectProviderNamespace(t *testing.T) {
	t.Parallel()
	provider := NewObjectProvider(&mockObjectRepository{}, &mockCharacterRepository{})
	assert.Equal(t, "object", provider.Namespace())
}

func TestObjectProviderSchema(t *testing.T) {
	t.Parallel()
	provider := NewObjectProvider(&mockObjectRepository{}, &mockCharacterRepository{})

	schema := provider.Schema()
	require.NotNil(t, schema)
	require.NotNil(t, schema.Attributes)

	// Per holomush-k3ud acceptance: object provider populates attrs needed by
	// seed:player-object-colocation (location) + future builder/admin seeds
	// that may gate on owner/container shape.
	assert.Equal(t, types.AttrTypeString, schema.Attributes["id"])
	assert.Equal(t, types.AttrTypeString, schema.Attributes["name"])
	assert.Equal(t, types.AttrTypeString, schema.Attributes["description"])
	assert.Equal(t, types.AttrTypeString, schema.Attributes["owner_id"])
	assert.Equal(t, types.AttrTypeBool, schema.Attributes["has_owner"])
	assert.Equal(t, types.AttrTypeString, schema.Attributes["location"])
	assert.Equal(t, types.AttrTypeBool, schema.Attributes["has_location"])
	assert.Equal(t, types.AttrTypeBool, schema.Attributes["is_container"])
	assert.Equal(t, types.AttrTypeString, schema.Attributes["held_by_character_id"])
	assert.Equal(t, types.AttrTypeBool, schema.Attributes["is_held"])
	assert.Equal(t, types.AttrTypeString, schema.Attributes["contained_in_object_id"])
	assert.Equal(t, types.AttrTypeBool, schema.Attributes["is_contained"])
}

func TestObjectProviderResolveSubjectAlwaysNil(t *testing.T) {
	t.Parallel()
	provider := NewObjectProvider(&mockObjectRepository{}, &mockCharacterRepository{})

	attrs, err := provider.ResolveSubject(context.Background(), "object:"+ulid.Make().String())
	require.NoError(t, err)
	assert.Nil(t, attrs, "objects are never subjects — ResolveSubject MUST always return nil")
}

func TestObjectProviderResolveResource(t *testing.T) {
	objID := ulid.Make()
	locID := ulid.Make()
	ownerID := ulid.Make()
	charID := ulid.Make()
	containerID := ulid.Make()

	tests := []struct {
		name         string
		resourceID   string
		setupObjRepo func(*mockObjectRepository)
		setupChrRepo func(*mockCharacterRepository)
		expectAttrs  map[string]any
		// Only a subset of attrs are asserted via expectSubset; this keeps
		// table cases focused on what each case is actually verifying.
		expectSubset map[string]any
		// expectAbsent lists keys that MUST NOT be present in attrs.
		// Per ADR holomush-ti1b: providers omit optional attrs when
		// unresolved (e.g., `location` when has_location=false), so the
		// DSL evaluator's missing-attr-→-false semantics preserve
		// default-deny on colocation seeds.
		expectAbsent []string
		expectNil    bool
		expectErr    bool
		errSubstring string
	}{
		{
			name:       "object directly in location",
			resourceID: "object:" + objID.String(),
			setupObjRepo: func(m *mockObjectRepository) {
				m.getFunc = func(_ context.Context, id ulid.ULID) (*world.Object, error) {
					assert.Equal(t, objID, id)
					obj := newObjectInLocation(t, objID, locID, "Lantern")
					obj.Description = "A brass lantern."
					obj.OwnerID = &ownerID
					obj.IsContainer = false
					return obj, nil
				}
			},
			expectAttrs: map[string]any{
				"id":                     objID.String(),
				"name":                   "Lantern",
				"description":            "A brass lantern.",
				"owner_id":               ownerID.String(),
				"has_owner":              true,
				"location":               locID.String(),
				"has_location":           true,
				"is_container":           false,
				"held_by_character_id":   "",
				"is_held":                false,
				"contained_in_object_id": "",
				"is_contained":           false,
			},
		},
		{
			name:       "container object in location",
			resourceID: "object:" + objID.String(),
			setupObjRepo: func(m *mockObjectRepository) {
				m.getFunc = func(_ context.Context, _ ulid.ULID) (*world.Object, error) {
					obj := newObjectInLocation(t, objID, locID, "Chest")
					obj.IsContainer = true
					return obj, nil
				}
			},
			expectSubset: map[string]any{
				"is_container": true,
				"has_owner":    false,
				"owner_id":     "",
				"location":     locID.String(),
			},
		},
		{
			name:       "object held by character — resolves to character's location",
			resourceID: "object:" + objID.String(),
			setupObjRepo: func(m *mockObjectRepository) {
				m.getFunc = func(_ context.Context, id ulid.ULID) (*world.Object, error) {
					assert.Equal(t, objID, id)
					return newObjectHeldByCharacter(t, objID, charID, "Note"), nil
				}
			},
			setupChrRepo: func(m *mockCharacterRepository) {
				m.getFunc = func(_ context.Context, id ulid.ULID) (*world.Character, error) {
					assert.Equal(t, charID, id)
					return &world.Character{
						ID:         charID,
						PlayerID:   ulid.Make(),
						Name:       "Holder",
						LocationID: &locID,
					}, nil
				}
			},
			expectSubset: map[string]any{
				"location":             locID.String(),
				"has_location":         true,
				"held_by_character_id": charID.String(),
				"is_held":              true,
			},
		},
		{
			name:       "object held by character without location → has_location=false",
			resourceID: "object:" + objID.String(),
			setupObjRepo: func(m *mockObjectRepository) {
				m.getFunc = func(_ context.Context, _ ulid.ULID) (*world.Object, error) {
					return newObjectHeldByCharacter(t, objID, charID, "Pocket note"), nil
				}
			},
			setupChrRepo: func(m *mockCharacterRepository) {
				m.getFunc = func(_ context.Context, _ ulid.ULID) (*world.Character, error) {
					return &world.Character{
						ID:         charID,
						PlayerID:   ulid.Make(),
						Name:       "Wanderer",
						LocationID: nil,
					}, nil
				}
			},
			expectSubset: map[string]any{
				"has_location":         false,
				"held_by_character_id": charID.String(),
				"is_held":              true,
			},
			expectAbsent: []string{"location"},
		},
		{
			name:       "object inside a container — walks one level to find location",
			resourceID: "object:" + objID.String(),
			setupObjRepo: func(m *mockObjectRepository) {
				m.getFunc = func(_ context.Context, id ulid.ULID) (*world.Object, error) {
					switch id {
					case objID:
						return newObjectContainedIn(t, objID, containerID, "Coin"), nil
					case containerID:
						return newObjectInLocation(t, containerID, locID, "Chest"), nil
					default:
						t.Fatalf("unexpected object lookup: %s", id)
						return nil, nil
					}
				}
			},
			expectSubset: map[string]any{
				"location":               locID.String(),
				"has_location":           true,
				"contained_in_object_id": containerID.String(),
				"is_contained":           true,
			},
		},
		{
			name:       "object inside container inside container — multi-level walk",
			resourceID: "object:" + objID.String(),
			setupObjRepo: func(m *mockObjectRepository) {
				outer := ulid.Make()
				m.getFunc = func(_ context.Context, id ulid.ULID) (*world.Object, error) {
					switch id {
					case objID:
						return newObjectContainedIn(t, objID, containerID, "Coin"), nil
					case containerID:
						return newObjectContainedIn(t, containerID, outer, "Pouch"), nil
					case outer:
						return newObjectInLocation(t, outer, locID, "Chest"), nil
					default:
						t.Fatalf("unexpected object lookup: %s", id)
						return nil, nil
					}
				}
			},
			expectSubset: map[string]any{
				"location":     locID.String(),
				"has_location": true,
			},
		},
		{
			name:       "object inside a container held by a character — walks through both",
			resourceID: "object:" + objID.String(),
			setupObjRepo: func(m *mockObjectRepository) {
				m.getFunc = func(_ context.Context, id ulid.ULID) (*world.Object, error) {
					switch id {
					case objID:
						return newObjectContainedIn(t, objID, containerID, "Coin"), nil
					case containerID:
						return newObjectHeldByCharacter(t, containerID, charID, "Pouch"), nil
					default:
						t.Fatalf("unexpected object lookup: %s", id)
						return nil, nil
					}
				}
			},
			setupChrRepo: func(m *mockCharacterRepository) {
				m.getFunc = func(_ context.Context, _ ulid.ULID) (*world.Character, error) {
					return &world.Character{
						ID:         charID,
						PlayerID:   ulid.Make(),
						Name:       "Holder",
						LocationID: &locID,
					}, nil
				}
			},
			expectSubset: map[string]any{
				"location":     locID.String(),
				"has_location": true,
			},
		},
		{
			name:       "container chain breaks (parent not found) → has_location=false but other attrs populated",
			resourceID: "object:" + objID.String(),
			setupObjRepo: func(m *mockObjectRepository) {
				m.getFunc = func(_ context.Context, id ulid.ULID) (*world.Object, error) {
					if id == objID {
						return newObjectContainedIn(t, objID, containerID, "Orphan coin"), nil
					}
					return nil, errors.New("not found")
				}
			},
			expectSubset: map[string]any{
				"id":                     objID.String(),
				"contained_in_object_id": containerID.String(),
				"is_contained":           true,
				"has_location":           false,
			},
			expectAbsent: []string{"location"},
		},
		{
			name:       "character holder lookup fails → has_location=false but other attrs populated",
			resourceID: "object:" + objID.String(),
			setupObjRepo: func(m *mockObjectRepository) {
				m.getFunc = func(_ context.Context, _ ulid.ULID) (*world.Object, error) {
					return newObjectHeldByCharacter(t, objID, charID, "Note"), nil
				}
			},
			setupChrRepo: func(m *mockCharacterRepository) {
				m.getFunc = func(_ context.Context, _ ulid.ULID) (*world.Character, error) {
					return nil, errors.New("character not found")
				}
			},
			expectSubset: map[string]any{
				"id":                   objID.String(),
				"held_by_character_id": charID.String(),
				"is_held":              true,
				"has_location":         false,
			},
			expectAbsent: []string{"location"},
		},
		{
			name:       "circular containment → bounded by visited-set, has_location=false",
			resourceID: "object:" + objID.String(),
			setupObjRepo: func(m *mockObjectRepository) {
				// A is contained in B; B is contained in A — a corrupted DB
				// state the engine MUST tolerate without infinite recursion.
				m.getFunc = func(_ context.Context, id ulid.ULID) (*world.Object, error) {
					switch id {
					case objID:
						return newObjectContainedIn(t, objID, containerID, "A"), nil
					case containerID:
						return newObjectContainedIn(t, containerID, objID, "B"), nil
					}
					return nil, errors.New("unexpected")
				}
			},
			expectSubset: map[string]any{
				"has_location": false,
			},
			expectAbsent: []string{"location"},
		},
		{
			name:         "wrong entity type (location)",
			resourceID:   "location:" + ulid.Make().String(),
			setupObjRepo: func(_ *mockObjectRepository) {},
			expectNil:    true,
		},
		{
			name:         "wrong entity type (character)",
			resourceID:   access.CharacterResource(ulid.Make().String()),
			setupObjRepo: func(_ *mockObjectRepository) {},
			expectNil:    true,
		},
		{
			// Mirror of holomush-g776 LocationProvider wildcard tolerance —
			// service.go:449 emits access.ObjectResource("*") for the
			// CreateObject capability check. Returning a parse error would
			// fail-closed every CreateObject call. The seed selects via
			// `resource is object` target match, no per-instance attrs
			// needed.
			name:         "wildcard ID — bypass (mirrors holomush-g776)",
			resourceID:   "object:*",
			setupObjRepo: func(_ *mockObjectRepository) {},
			expectNil:    true,
		},
		{
			name:         "invalid ULID — bypass",
			resourceID:   "object:not-a-ulid",
			setupObjRepo: func(_ *mockObjectRepository) {},
			expectNil:    true,
		},
		{
			name:         "missing colon separator",
			resourceID:   "object" + objID.String(),
			setupObjRepo: func(_ *mockObjectRepository) {},
			expectErr:    true,
			errSubstring: "invalid entity ID format",
		},
		{
			name:       "repository error on top-level Get",
			resourceID: "object:" + objID.String(),
			setupObjRepo: func(m *mockObjectRepository) {
				m.getFunc = func(_ context.Context, _ ulid.ULID) (*world.Object, error) {
					return nil, errors.New("database error")
				}
			},
			expectErr:    true,
			errSubstring: "database error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objRepo := &mockObjectRepository{}
			chrRepo := &mockCharacterRepository{}
			tt.setupObjRepo(objRepo)
			if tt.setupChrRepo != nil {
				tt.setupChrRepo(chrRepo)
			}
			provider := NewObjectProvider(objRepo, chrRepo)

			attrs, err := provider.ResolveResource(context.Background(), tt.resourceID)

			if tt.expectErr {
				require.Error(t, err)
				if tt.errSubstring != "" {
					assert.Contains(t, err.Error(), tt.errSubstring)
				}
				return
			}

			require.NoError(t, err)
			if tt.expectNil {
				assert.Nil(t, attrs)
				return
			}

			require.NotNil(t, attrs)
			if tt.expectAttrs != nil {
				assert.Equal(t, tt.expectAttrs, attrs)
				return
			}
			for k, want := range tt.expectSubset {
				assert.Equal(t, want, attrs[k], "attribute %q", k)
			}
			for _, k := range tt.expectAbsent {
				_, present := attrs[k]
				assert.False(t, present,
					"attribute %q MUST be absent per ADR holomush-ti1b (un-locatable → omit, not empty-string sentinel)", k)
			}
		})
	}
}

// TestObjectProviderResolveResource_NilCharacterRepoIsTolerated guards a
// production wiring slip: the LocationProvider/PropertyProvider precedent is
// for repositories to be optional with a documented WARN at construction
// time. The provider MUST NOT panic if charRepo is nil — it should treat
// held-by-character objects as un-locatable (has_location=false) and let
// the seed-coverage validator surface the wiring gap. Defensive shape only;
// production wiring at subsystem.go ALWAYS supplies both repos.
func TestObjectProviderResolveResource_NilCharacterRepoIsTolerated(t *testing.T) {
	t.Parallel()
	objID := ulid.Make()
	charID := ulid.Make()
	objRepo := &mockObjectRepository{
		getFunc: func(_ context.Context, _ ulid.ULID) (*world.Object, error) {
			return newObjectHeldByCharacter(t, objID, charID, "Item"), nil
		},
	}
	provider := NewObjectProvider(objRepo, nil)

	attrs, err := provider.ResolveResource(context.Background(), "object:"+objID.String())
	require.NoError(t, err)
	require.NotNil(t, attrs)
	assert.Equal(t, false, attrs["has_location"])
	_, present := attrs["location"]
	assert.False(t, present, "ADR holomush-ti1b: un-locatable → omit 'location' key")
	assert.Equal(t, charID.String(), attrs["held_by_character_id"])
}

// TestObjectProviderResolveResource_NilRepoReturnsAreGuarded pins the
// CodeRabbit #3 defensive guards (PR #4163): when a repository violates
// the (obj|nil, err|nil) contract by returning (nil, nil), the provider
// must fail-closed (top-level Get) or treat as un-locatable (chain walk)
// instead of panicking on a nil-pointer dereference. The production
// repo impls at internal/world/postgres/{object,character}_repo.go
// never return (nil, nil); these tests lock the defensive guards.
func TestObjectProviderResolveResource_NilRepoReturnsAreGuarded(t *testing.T) {
	objID := ulid.Make()
	charID := ulid.Make()
	containerID := ulid.Make()

	t.Run("top-level Get returns (nil, nil) → fail-closed with OBJECT_FETCH_FAILED", func(t *testing.T) {
		objRepo := &mockObjectRepository{
			getFunc: func(_ context.Context, _ ulid.ULID) (*world.Object, error) {
				return nil, nil //nolint:nilnil // intentional contract violation under test
			},
		}
		provider := NewObjectProvider(objRepo, &mockCharacterRepository{})

		attrs, err := provider.ResolveResource(context.Background(), "object:"+objID.String())
		require.Error(t, err)
		assert.Nil(t, attrs)
		assert.Contains(t, err.Error(), "object repository returned nil")
	})

	t.Run("holder character Get returns (nil, nil) → un-locatable, no panic", func(t *testing.T) {
		objRepo := &mockObjectRepository{
			getFunc: func(_ context.Context, _ ulid.ULID) (*world.Object, error) {
				return newObjectHeldByCharacter(t, objID, charID, "Item"), nil
			},
		}
		charRepo := &mockCharacterRepository{
			getFunc: func(_ context.Context, _ ulid.ULID) (*world.Character, error) {
				return nil, nil //nolint:nilnil // intentional contract violation under test
			},
		}
		provider := NewObjectProvider(objRepo, charRepo)

		attrs, err := provider.ResolveResource(context.Background(), "object:"+objID.String())
		require.NoError(t, err)
		require.NotNil(t, attrs)
		assert.Equal(t, false, attrs["has_location"])
		_, present := attrs["location"]
		assert.False(t, present, "ADR holomush-ti1b: un-locatable → omit 'location' key")
	})

	t.Run("parent container Get returns (nil, nil) → un-locatable, no panic", func(t *testing.T) {
		objRepo := &mockObjectRepository{
			getFunc: func(_ context.Context, id ulid.ULID) (*world.Object, error) {
				if id == objID {
					return newObjectContainedIn(t, objID, containerID, "Coin"), nil
				}
				return nil, nil //nolint:nilnil // intentional contract violation under test
			},
		}
		provider := NewObjectProvider(objRepo, &mockCharacterRepository{})

		attrs, err := provider.ResolveResource(context.Background(), "object:"+objID.String())
		require.NoError(t, err)
		require.NotNil(t, attrs)
		assert.Equal(t, false, attrs["has_location"])
		_, present := attrs["location"]
		assert.False(t, present, "ADR holomush-ti1b: un-locatable → omit 'location' key")
	})
}
