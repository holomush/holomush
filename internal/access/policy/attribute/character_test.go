// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/world"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCharacterRepository is a simple mock for testing.
type mockCharacterRepository struct {
	getFunc func(ctx context.Context, id ulid.ULID) (*world.Character, error)
}

func (m *mockCharacterRepository) Get(ctx context.Context, id ulid.ULID) (*world.Character, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, id)
	}
	return nil, errors.New("not implemented")
}

func (m *mockCharacterRepository) Create(_ context.Context, _ *world.Character) error {
	return errors.New("not implemented")
}

func (m *mockCharacterRepository) Update(_ context.Context, _ *world.Character) error {
	return errors.New("not implemented")
}

func (m *mockCharacterRepository) Delete(_ context.Context, _ ulid.ULID) error {
	return errors.New("not implemented")
}

func (m *mockCharacterRepository) GetByLocation(_ context.Context, _ ulid.ULID, _ world.ListOptions) ([]*world.Character, error) {
	return nil, errors.New("not implemented")
}

func (m *mockCharacterRepository) UpdateLocation(_ context.Context, _ ulid.ULID, _ *ulid.ULID) error {
	return errors.New("not implemented")
}

func (m *mockCharacterRepository) IsOwnedByPlayer(_ context.Context, _, _ ulid.ULID) (bool, error) {
	return false, errors.New("not implemented")
}

func TestCharacterProvider_Namespace(t *testing.T) {
	repo := &mockCharacterRepository{}
	provider := NewCharacterProvider(repo)

	assert.Equal(t, "character", provider.Namespace())
}

func TestCharacterProvider_Schema(t *testing.T) {
	repo := &mockCharacterRepository{}
	provider := NewCharacterProvider(repo)

	schema := provider.Schema()
	require.NotNil(t, schema)
	require.NotNil(t, schema.Attributes)

	// Check expected attributes
	assert.Equal(t, types.AttrTypeString, schema.Attributes["id"])
	assert.Equal(t, types.AttrTypeString, schema.Attributes["player_id"])
	assert.Equal(t, types.AttrTypeString, schema.Attributes["name"])
	assert.Equal(t, types.AttrTypeString, schema.Attributes["description"])
	assert.Equal(t, types.AttrTypeString, schema.Attributes["location_id"])
	assert.Equal(t, types.AttrTypeBool, schema.Attributes["has_location"])
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
			subjectID: "character:" + charID.String(),
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
				"location_id":  locationID.String(),
				"has_location": true,
			},
		},
		{
			name:      "character without location",
			subjectID: "character:" + charID.String(),
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
			expectAttrs: map[string]any{
				"id":           charID.String(),
				"player_id":    playerID.String(),
				"name":         "NoLocChar",
				"description":  "",
				"location_id":  "",
				"has_location": false,
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
			name:           "invalid ULID format",
			subjectID:      "character:not-a-ulid",
			setupMock:      func(_ *mockCharacterRepository) {},
			expectError:    true,
			errorSubstring: "invalid character ID",
		},
		{
			name:           "missing colon separator",
			subjectID:      "character" + charID.String(),
			setupMock:      func(_ *mockCharacterRepository) {},
			expectError:    true,
			errorSubstring: "invalid subject ID format",
		},
		{
			name:      "repository error",
			subjectID: "character:" + charID.String(),
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
			provider := NewCharacterProvider(repo)

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
			resourceID: "character:" + charID.String(),
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
				"location_id":  locationID.String(),
				"has_location": true,
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
			resourceID: "character:" + charID.String(),
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
			provider := NewCharacterProvider(repo)

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
