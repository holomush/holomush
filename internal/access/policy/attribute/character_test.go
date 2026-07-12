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

// mockCharacterRepository is a simple mock for testing.
type mockCharacterRepository struct {
	getFunc func(ctx context.Context, id ulid.ULID) (*world.Character, error)
}

// mockRoleResolver is a simple mock for testing role resolution.
type mockRoleResolver struct {
	roles map[string][]string
}

func (m *mockRoleResolver) GetRoles(_ context.Context, subject string) []string {
	if m.roles == nil {
		return nil
	}
	return m.roles[subject]
}

func (m *mockCharacterRepository) Get(ctx context.Context, id ulid.ULID) (*world.Character, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, id)
	}
	return nil, errors.New("not implemented")
}

func (m *mockCharacterRepository) Create(_ context.Context, _ *world.Character) (*wmodel.MutationDelta, error) {
	return nil, errors.New("not implemented")
}

func (m *mockCharacterRepository) Update(_ context.Context, _ *world.Character) (*wmodel.MutationDelta, error) {
	return nil, errors.New("not implemented")
}

func (m *mockCharacterRepository) Delete(_ context.Context, _ ulid.ULID, _ int) (*wmodel.MutationDelta, error) {
	return nil, errors.New("not implemented")
}

func (m *mockCharacterRepository) GetByLocation(_ context.Context, _ ulid.ULID, _ world.ListOptions) ([]*world.Character, error) {
	return nil, errors.New("not implemented")
}

func (m *mockCharacterRepository) UpdateLocation(_ context.Context, _ ulid.ULID, _ *ulid.ULID, _ int) (*wmodel.MutationDelta, error) {
	return nil, errors.New("not implemented")
}

func (m *mockCharacterRepository) IsOwnedByPlayer(_ context.Context, _, _ ulid.ULID) (bool, error) {
	return false, errors.New("not implemented")
}

func (m *mockCharacterRepository) GetNamesByIDs(_ context.Context, _ []ulid.ULID) (map[ulid.ULID]string, error) {
	return nil, errors.New("not implemented")
}

func TestCharacterProviderContract(t *testing.T) {
	assertProviderContract(t, NewCharacterProvider(&mockCharacterRepository{}, nil))
}

func TestCharacterProviderSchema(t *testing.T) {
	repo := &mockCharacterRepository{}
	provider := NewCharacterProvider(repo, nil)

	schema := provider.Schema()
	require.NotNil(t, schema)
	require.NotNil(t, schema.Attributes)

	// Check expected attributes
	assert.Equal(t, types.AttrTypeString, schema.Attributes["id"])
	assert.Equal(t, types.AttrTypeString, schema.Attributes["player_id"])
	assert.Equal(t, types.AttrTypeString, schema.Attributes["name"])
	assert.Equal(t, types.AttrTypeString, schema.Attributes["description"])
	assert.Equal(t, types.AttrTypeStringList, schema.Attributes["roles"])
	assert.Equal(t, types.AttrTypeString, schema.Attributes["location_id"])
	assert.Equal(t, types.AttrTypeString, schema.Attributes["location"])
	assert.Equal(t, types.AttrTypeBool, schema.Attributes["has_location"])
	assert.Equal(t, types.AttrTypeBool, schema.Attributes["is_guest"])
	assert.Equal(t, types.AttrTypeBool, schema.Attributes["has_is_guest"])
}

func TestCharacterProvider_ResolveSubject(t *testing.T) {
	charID := ulid.Make()
	playerID := ulid.Make()
	locationID := ulid.Make()
	createdAt := time.Now().UTC()

	tests := []struct {
		name           string
		subjectID      string
		setupMock      func(*mockCharacterRepository)
		expectAttrs    map[string]any
		expectNil      bool
		expectError    bool
		errorSubstring string
	}{
		{
			name:      "valid character ID",
			subjectID: access.CharacterSubject(charID.String()),
			setupMock: func(m *mockCharacterRepository) {
				m.getFunc = func(_ context.Context, id ulid.ULID) (*world.Character, error) {
					assert.Equal(t, charID, id)
					return &world.Character{
						ID:          charID,
						PlayerID:    playerID,
						Name:        "TestChar",
						Description: "A test character",
						LocationID:  &locationID,
						CreatedAt:   createdAt,
					}, nil
				}
			},
			expectAttrs: map[string]any{
				"id":           charID.String(),
				"player_id":    playerID.String(),
				"name":         "TestChar",
				"description":  "A test character",
				"roles":        []string{"player"},
				"location_id":  locationID.String(),
				"location":     locationID.String(),
				"has_location": true,
				// nil kindLookup → is_guest omitted, witness false (ADR holomush-ti1b).
				"has_is_guest": false,
			},
		},
		{
			name:      "character without location",
			subjectID: access.CharacterSubject(charID.String()),
			setupMock: func(m *mockCharacterRepository) {
				m.getFunc = func(_ context.Context, _ ulid.ULID) (*world.Character, error) {
					return &world.Character{
						ID:          charID,
						PlayerID:    playerID,
						Name:        "NoLocChar",
						Description: "",
						LocationID:  nil,
						CreatedAt:   createdAt,
					}, nil
				}
			},
			// Per ADR holomush-ti1b: location and location_id keys are
			// OMITTED from the bag when has_location=false. The DSL
			// evaluator's missing-attr-→-false semantics preserve
			// default-deny on colocation seeds.
			expectAttrs: map[string]any{
				"id":           charID.String(),
				"player_id":    playerID.String(),
				"name":         "NoLocChar",
				"description":  "",
				"roles":        []string{"player"},
				"has_location": false,
				// nil kindLookup → is_guest omitted, witness false (ADR holomush-ti1b).
				"has_is_guest": false,
			},
		},
		{
			name:        "wrong entity type (location)",
			subjectID:   "location:" + ulid.Make().String(),
			setupMock:   func(_ *mockCharacterRepository) {},
			expectNil:   true,
			expectError: false,
		},
		{
			name:        "wrong entity type (object)",
			subjectID:   "object:" + ulid.Make().String(),
			setupMock:   func(_ *mockCharacterRepository) {},
			expectNil:   true,
			expectError: false,
		},
		{
			// Per holomush-xxel: non-ULID character refs (canonical case is
			// "character:*" wildcard) MUST be skipped gracefully — symmetric
			// with LocationProvider's tolerance from g776. Returning the
			// parse error here would fail-closed the engine for any future
			// capability check that emits the wildcard form.
			name:        "invalid ULID format — bypass (holomush-xxel)",
			subjectID:   "character:not-a-ulid",
			setupMock:   func(_ *mockCharacterRepository) {},
			expectNil:   true,
			expectError: false,
		},
		{
			// Companion to the case above — literal wildcard form a future
			// capability-grant check would emit. Same expectation: provider
			// politely declines, engine handles the pattern at the target
			// layer.
			name:        "wildcard ID — bypass (holomush-xxel)",
			subjectID:   "character:*",
			setupMock:   func(_ *mockCharacterRepository) {},
			expectNil:   true,
			expectError: false,
		},
		{
			name:           "missing colon separator",
			subjectID:      "character" + charID.String(),
			setupMock:      func(_ *mockCharacterRepository) {},
			expectError:    true,
			errorSubstring: "invalid entity ID format",
		},
		{
			name:      "repository error",
			subjectID: access.CharacterSubject(charID.String()),
			setupMock: func(m *mockCharacterRepository) {
				m.getFunc = func(_ context.Context, _ ulid.ULID) (*world.Character, error) {
					return nil, errors.New("database connection failed")
				}
			},
			expectError:    true,
			errorSubstring: "database connection failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockCharacterRepository{}
			tt.setupMock(repo)
			provider := NewCharacterProvider(repo, nil)

			attrs, err := provider.ResolveSubject(context.Background(), tt.subjectID)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorSubstring != "" {
					assert.Contains(t, err.Error(), tt.errorSubstring)
				}
				return
			}

			require.NoError(t, err)

			if tt.expectNil {
				assert.Nil(t, attrs)
				return
			}

			require.NotNil(t, attrs)
			assert.Equal(t, tt.expectAttrs, attrs)
		})
	}
}

// --- is_guest attribute (omit-don't-sentinel, ADR holomush-ti1b) -------------
//
// The character namespace carries is_guest so a Layer-1 command-execution
// policy can gate on the acting character's owning-player kind (guests have a
// character: principal at command dispatch, never a player: one — so the gate
// must live on the character bag, not the player bag). Per holomush-5rh.23.

// TestCharacterProviderEmitsIsGuestWitnessWhenLookupConfigured verifies that
// when a PlayerKindLookup is configured, ResolveSubject emits both is_guest and
// has_is_guest on every code path: true/true when the owning player is a guest,
// false/true when registered. The lookup is keyed on the character's PlayerID.
func TestCharacterProviderEmitsIsGuestWitnessWhenLookupConfigured(t *testing.T) {
	charID := ulid.Make()
	guestPlayerID := ulid.Make()
	registeredPlayerID := ulid.Make()

	// Typed as PlayerKindLookup so its always-nil error result is dictated by the
	// named signature (keeps unparam from flagging the happy-path stub).
	var lookup PlayerKindLookup = func(_ context.Context, playerID string) (bool, error) {
		return playerID == guestPlayerID.String(), nil
	}

	newProvider := func(playerID ulid.ULID) *CharacterProvider {
		repo := &mockCharacterRepository{
			getFunc: func(_ context.Context, _ ulid.ULID) (*world.Character, error) {
				return &world.Character{ID: charID, PlayerID: playerID, Name: "C"}, nil
			},
		}
		return NewCharacterProvider(repo, nil, WithCharacterKindLookup(lookup))
	}

	t.Run("guest player's character emits is_guest true and has_is_guest true", func(t *testing.T) {
		attrs, err := newProvider(guestPlayerID).ResolveSubject(context.Background(), access.CharacterSubject(charID.String()))
		require.NoError(t, err)
		require.NotNil(t, attrs)
		assert.Equal(t, true, attrs["is_guest"], "is_guest must be true when the owning player is a guest")
		assert.Equal(t, true, attrs["has_is_guest"], "has_is_guest must always be present when lookup configured")
	})

	t.Run("registered player's character emits is_guest false and has_is_guest true", func(t *testing.T) {
		attrs, err := newProvider(registeredPlayerID).ResolveSubject(context.Background(), access.CharacterSubject(charID.String()))
		require.NoError(t, err)
		require.NotNil(t, attrs)
		assert.Equal(t, false, attrs["is_guest"], "is_guest must be false for a registered player's character")
		assert.Equal(t, true, attrs["has_is_guest"], "has_is_guest must always be present when lookup configured")
	})
}

// TestCharacterProviderOmitsIsGuestWhenLookupAbsentOrFails verifies the
// omit-don't-sentinel invariant (ADR holomush-ti1b): when no lookup is
// configured or it returns an error, the is_guest key MUST be absent (never a
// false sentinel) and the witness has_is_guest MUST be false. This keeps the
// fail-closed scene-command gate denying when guest-ness cannot be determined.
func TestCharacterProviderOmitsIsGuestWhenLookupAbsentOrFails(t *testing.T) {
	charID := ulid.Make()
	playerID := ulid.Make()
	repoFor := func() *mockCharacterRepository {
		return &mockCharacterRepository{
			getFunc: func(_ context.Context, _ ulid.ULID) (*world.Character, error) {
				return &world.Character{ID: charID, PlayerID: playerID, Name: "C"}, nil
			},
		}
	}

	t.Run("no lookup configured: is_guest key absent and has_is_guest false", func(t *testing.T) {
		p := NewCharacterProvider(repoFor(), nil)
		attrs, err := p.ResolveSubject(context.Background(), access.CharacterSubject(charID.String()))
		require.NoError(t, err)
		require.NotNil(t, attrs)
		_, ok := attrs["is_guest"]
		assert.False(t, ok, "is_guest key MUST be absent when no lookup configured (omit-don't-sentinel, ADR holomush-ti1b)")
		assert.Equal(t, false, attrs["has_is_guest"], "has_is_guest must be false when lookup not configured")
	})

	t.Run("lookup returns error: is_guest key absent and has_is_guest false", func(t *testing.T) {
		lookup := func(_ context.Context, _ string) (bool, error) {
			return false, errors.New("player not found")
		}
		p := NewCharacterProvider(repoFor(), nil, WithCharacterKindLookup(lookup))
		attrs, err := p.ResolveSubject(context.Background(), access.CharacterSubject(charID.String()))
		require.NoError(t, err, "lookup errors must not bubble out of ResolveSubject")
		require.NotNil(t, attrs)
		_, ok := attrs["is_guest"]
		assert.False(t, ok, "is_guest key MUST be absent when lookup errors (omit-don't-sentinel, ADR holomush-ti1b)")
		assert.Equal(t, false, attrs["has_is_guest"], "has_is_guest must be false when lookup fails")
	})
}

func TestCharacterProvider_RoleResolution(t *testing.T) {
	charID := ulid.Make()
	playerID := ulid.Make()
	locationID := ulid.Make()
	createdAt := time.Now().UTC()

	tests := []struct {
		name          string
		roleResolver  RoleResolver
		expectedRoles []string
	}{
		{
			name:          "nil role resolver defaults to player",
			roleResolver:  nil,
			expectedRoles: []string{"player"},
		},
		{
			name: "empty roles from resolver defaults to player",
			roleResolver: &mockRoleResolver{
				roles: map[string][]string{},
			},
			expectedRoles: []string{"player"},
		},
		{
			name: "nil roles from resolver defaults to player",
			roleResolver: &mockRoleResolver{
				roles: map[string][]string{
					"character:" + charID.String(): nil,
				},
			},
			expectedRoles: []string{"player"},
		},
		{
			name: "admin role from resolver",
			roleResolver: &mockRoleResolver{
				roles: map[string][]string{
					"character:" + charID.String(): {"admin"},
				},
			},
			expectedRoles: []string{"admin"},
		},
		{
			name: "builder role from resolver",
			roleResolver: &mockRoleResolver{
				roles: map[string][]string{
					"character:" + charID.String(): {"builder"},
				},
			},
			expectedRoles: []string{"builder"},
		},
		{
			name: "multiple roles from resolver",
			roleResolver: &mockRoleResolver{
				roles: map[string][]string{
					"character:" + charID.String(): {"builder", "admin"},
				},
			},
			expectedRoles: []string{"builder", "admin"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockCharacterRepository{
				getFunc: func(_ context.Context, id ulid.ULID) (*world.Character, error) {
					assert.Equal(t, charID, id)
					return &world.Character{
						ID:          charID,
						PlayerID:    playerID,
						Name:        "TestChar",
						Description: "A test character",
						LocationID:  &locationID,
						CreatedAt:   createdAt,
					}, nil
				},
			}

			provider := NewCharacterProvider(repo, tt.roleResolver)

			attrs, err := provider.ResolveSubject(context.Background(), access.CharacterSubject(charID.String()))
			require.NoError(t, err)
			require.NotNil(t, attrs)
			assert.Equal(t, tt.expectedRoles, attrs["roles"])
		})
	}
}

func TestCharacterProvider_ResolveResource(t *testing.T) {
	charID := ulid.Make()
	playerID := ulid.Make()
	locationID := ulid.Make()
	createdAt := time.Now().UTC()

	tests := []struct {
		name           string
		resourceID     string
		setupMock      func(*mockCharacterRepository)
		expectAttrs    map[string]any
		expectNil      bool
		expectError    bool
		errorSubstring string
	}{
		{
			name:       "valid character resource",
			resourceID: access.CharacterResource(charID.String()),
			setupMock: func(m *mockCharacterRepository) {
				m.getFunc = func(_ context.Context, _ ulid.ULID) (*world.Character, error) {
					return &world.Character{
						ID:          charID,
						PlayerID:    playerID,
						Name:        "ResourceChar",
						Description: "Character as resource",
						LocationID:  &locationID,
						CreatedAt:   createdAt,
					}, nil
				}
			},
			expectAttrs: map[string]any{
				"id":           charID.String(),
				"player_id":    playerID.String(),
				"name":         "ResourceChar",
				"description":  "Character as resource",
				"roles":        []string{"player"},
				"location_id":  locationID.String(),
				"location":     locationID.String(),
				"has_location": true,
				// nil kindLookup → is_guest omitted, witness false (ADR holomush-ti1b).
				"has_is_guest": false,
			},
		},
		{
			name:        "wrong entity type",
			resourceID:  "location:" + ulid.Make().String(),
			setupMock:   func(_ *mockCharacterRepository) {},
			expectNil:   true,
			expectError: false,
		},
		{
			name:       "repository error",
			resourceID: access.CharacterResource(charID.String()),
			setupMock: func(m *mockCharacterRepository) {
				m.getFunc = func(_ context.Context, _ ulid.ULID) (*world.Character, error) {
					return nil, errors.New("repo error")
				}
			},
			expectError:    true,
			errorSubstring: "repo error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockCharacterRepository{}
			tt.setupMock(repo)
			provider := NewCharacterProvider(repo, nil)

			attrs, err := provider.ResolveResource(context.Background(), tt.resourceID)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorSubstring != "" {
					assert.Contains(t, err.Error(), tt.errorSubstring)
				}
				return
			}

			require.NoError(t, err)

			if tt.expectNil {
				assert.Nil(t, attrs)
				return
			}

			require.NotNil(t, attrs)
			assert.Equal(t, tt.expectAttrs, attrs)
		})
	}
}
