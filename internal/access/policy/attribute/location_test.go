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

// mockLocationRepository is a simple mock for testing.
type mockLocationRepository struct {
	getFunc func(ctx context.Context, id ulid.ULID) (*world.Location, error)
}

func (m *mockLocationRepository) Get(ctx context.Context, id ulid.ULID) (*world.Location, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, id)
	}
	return nil, errors.New("not implemented")
}

func (m *mockLocationRepository) Create(_ context.Context, _ *world.Location) error {
	return errors.New("not implemented")
}

func (m *mockLocationRepository) Update(_ context.Context, _ *world.Location) error {
	return errors.New("not implemented")
}

func (m *mockLocationRepository) Delete(_ context.Context, _ ulid.ULID) error {
	return errors.New("not implemented")
}

func (m *mockLocationRepository) ListByType(_ context.Context, _ world.LocationType) ([]*world.Location, error) {
	return nil, errors.New("not implemented")
}

func (m *mockLocationRepository) GetShadowedBy(_ context.Context, _ ulid.ULID) ([]*world.Location, error) {
	return nil, errors.New("not implemented")
}

func (m *mockLocationRepository) FindByName(_ context.Context, _ string) (*world.Location, error) {
	return nil, errors.New("not implemented")
}

func TestLocationProvider_Namespace(t *testing.T) {
	repo := &mockLocationRepository{}
	provider := NewLocationProvider(repo)

	assert.Equal(t, "location", provider.Namespace())
}

func TestLocationProvider_Schema(t *testing.T) {
	repo := &mockLocationRepository{}
	provider := NewLocationProvider(repo)

	schema := provider.Schema()
	require.NotNil(t, schema)
	require.NotNil(t, schema.Attributes)

	// Check expected attributes
	assert.Equal(t, types.AttrTypeString, schema.Attributes["id"])
	assert.Equal(t, types.AttrTypeString, schema.Attributes["type"])
	assert.Equal(t, types.AttrTypeString, schema.Attributes["name"])
	assert.Equal(t, types.AttrTypeString, schema.Attributes["description"])
	assert.Equal(t, types.AttrTypeString, schema.Attributes["owner_id"])
	assert.Equal(t, types.AttrTypeBool, schema.Attributes["has_owner"])
	assert.Equal(t, types.AttrTypeString, schema.Attributes["shadows_id"])
	assert.Equal(t, types.AttrTypeBool, schema.Attributes["is_shadow"])
	assert.Equal(t, types.AttrTypeString, schema.Attributes["replay_policy"])
	assert.Equal(t, types.AttrTypeBool, schema.Attributes["archived"])
}

func TestLocationProvider_ResolveSubject(t *testing.T) {
	// Locations are not subjects, should always return nil, nil
	repo := &mockLocationRepository{}
	provider := NewLocationProvider(repo)

	attrs, err := provider.ResolveSubject(context.Background(), "location:"+ulid.Make().String())
	require.NoError(t, err)
	assert.Nil(t, attrs)

	attrs, err = provider.ResolveSubject(context.Background(), "character:"+ulid.Make().String())
	require.NoError(t, err)
	assert.Nil(t, attrs)
}

func TestLocationProvider_ResolveResource(t *testing.T) {
	locID := ulid.Make()
	ownerID := ulid.Make()
	shadowsID := ulid.Make()
	createdAt := time.Now().UTC()
	archivedAt := time.Now().UTC()

	tests := []struct {
		name           string
		resourceID     string
		setupMock      func(*mockLocationRepository)
		expectAttrs    map[string]any
		expectNil      bool
		expectError    bool
		errorSubstring string
	}{
		{
			name:       "persistent location with owner",
			resourceID: "location:" + locID.String(),
			setupMock: func(m *mockLocationRepository) {
				m.getFunc = func(_ context.Context, id ulid.ULID) (*world.Location, error) {
					assert.Equal(t, locID, id)
					return &world.Location{
						ID:           locID,
						Type:         world.LocationTypePersistent,
						Name:         "Test Room",
						Description:  "A test location",
						OwnerID:      &ownerID,
						ReplayPolicy: "last:0",
						CreatedAt:    createdAt,
						ShadowsID:    nil,
						ArchivedAt:   nil,
					}, nil
				}
			},
			expectAttrs: map[string]any{
				"id":            locID.String(),
				"type":          "persistent",
				"name":          "Test Room",
				"description":   "A test location",
				"owner_id":      ownerID.String(),
				"has_owner":     true,
				"shadows_id":    "",
				"is_shadow":     false,
				"replay_policy": "last:0",
				"archived":      false,
			},
		},
		{
			name:       "scene without owner",
			resourceID: "location:" + locID.String(),
			setupMock: func(m *mockLocationRepository) {
				m.getFunc = func(_ context.Context, _ ulid.ULID) (*world.Location, error) {
					return &world.Location{
						ID:           locID,
						Type:         world.LocationTypeScene,
						Name:         "RP Scene",
						Description:  "",
						OwnerID:      nil,
						ReplayPolicy: "last:-1",
						CreatedAt:    createdAt,
						ShadowsID:    nil,
						ArchivedAt:   nil,
					}, nil
				}
			},
			expectAttrs: map[string]any{
				"id":            locID.String(),
				"type":          "scene",
				"name":          "RP Scene",
				"description":   "",
				"owner_id":      "",
				"has_owner":     false,
				"shadows_id":    "",
				"is_shadow":     false,
				"replay_policy": "last:-1",
				"archived":      false,
			},
		},
		{
			name:       "shadow location (archived)",
			resourceID: "location:" + locID.String(),
			setupMock: func(m *mockLocationRepository) {
				m.getFunc = func(_ context.Context, _ ulid.ULID) (*world.Location, error) {
					return &world.Location{
						ID:           locID,
						Type:         world.LocationTypeScene,
						Name:         "Shadow",
						Description:  "Shadow desc",
						OwnerID:      &ownerID,
						ShadowsID:    &shadowsID,
						ReplayPolicy: "last:100",
						CreatedAt:    createdAt,
						ArchivedAt:   &archivedAt,
					}, nil
				}
			},
			expectAttrs: map[string]any{
				"id":            locID.String(),
				"type":          "scene",
				"name":          "Shadow",
				"description":   "Shadow desc",
				"owner_id":      ownerID.String(),
				"has_owner":     true,
				"shadows_id":    shadowsID.String(),
				"is_shadow":     true,
				"replay_policy": "last:100",
				"archived":      true,
			},
		},
		{
			name:        "wrong entity type (character)",
			resourceID:  "character:" + ulid.Make().String(),
			setupMock:   func(_ *mockLocationRepository) {},
			expectNil:   true,
			expectError: false,
		},
		{
			name:        "wrong entity type (object)",
			resourceID:  "object:" + ulid.Make().String(),
			setupMock:   func(_ *mockLocationRepository) {},
			expectNil:   true,
			expectError: false,
		},
		{
			name:           "invalid ULID format",
			resourceID:     "location:not-a-ulid",
			setupMock:      func(_ *mockLocationRepository) {},
			expectError:    true,
			errorSubstring: "invalid location ID",
		},
		{
			name:           "missing colon separator",
			resourceID:     "location" + locID.String(),
			setupMock:      func(_ *mockLocationRepository) {},
			expectError:    true,
			errorSubstring: "invalid resource ID format",
		},
		{
			name:       "repository error",
			resourceID: "location:" + locID.String(),
			setupMock: func(m *mockLocationRepository) {
				m.getFunc = func(_ context.Context, _ ulid.ULID) (*world.Location, error) {
					return nil, errors.New("database error")
				}
			},
			expectError:    true,
			errorSubstring: "database error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockLocationRepository{}
			tt.setupMock(repo)
			provider := NewLocationProvider(repo)

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
