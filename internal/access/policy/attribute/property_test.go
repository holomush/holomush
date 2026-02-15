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
	"github.com/holomush/holomush/pkg/errutil"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPropertyRepository is a test double for PropertyRepository.
type mockPropertyRepository struct {
	getFunc func(ctx context.Context, id ulid.ULID) (*world.EntityProperty, error)
}

func (m *mockPropertyRepository) Create(_ context.Context, _ *world.EntityProperty) error {
	return errors.New("not implemented")
}

func (m *mockPropertyRepository) Get(ctx context.Context, id ulid.ULID) (*world.EntityProperty, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, id)
	}
	return nil, errors.New("not implemented")
}

func (m *mockPropertyRepository) ListByParent(_ context.Context, _ string, _ ulid.ULID) ([]*world.EntityProperty, error) {
	return nil, errors.New("not implemented")
}

func (m *mockPropertyRepository) Update(_ context.Context, _ *world.EntityProperty) error {
	return errors.New("not implemented")
}

func (m *mockPropertyRepository) Delete(_ context.Context, _ ulid.ULID) error {
	return errors.New("not implemented")
}

func (m *mockPropertyRepository) DeleteByParent(_ context.Context, _ string, _ ulid.ULID) error {
	return errors.New("not implemented")
}

// mockParentLocationResolver is a test double for ParentLocationResolver.
type mockParentLocationResolver struct {
	resolveFunc func(ctx context.Context, parentType string, parentID ulid.ULID) (*ulid.ULID, error)
}

func (m *mockParentLocationResolver) ResolveParentLocation(ctx context.Context, parentType string, parentID ulid.ULID) (*ulid.ULID, error) {
	if m.resolveFunc != nil {
		return m.resolveFunc(ctx, parentType, parentID)
	}
	return nil, errors.New("not implemented")
}

func TestPropertyProvider_Namespace(t *testing.T) {
	provider := NewPropertyProvider(nil, nil)
	assert.Equal(t, "property", provider.Namespace())
}

func TestPropertyProvider_ResolveSubject(t *testing.T) {
	provider := NewPropertyProvider(nil, nil)
	attrs, err := provider.ResolveSubject(context.Background(), "property:01ABC")
	require.NoError(t, err)
	assert.Nil(t, attrs, "properties are not subjects")
}

func TestPropertyProvider_ResolveResource(t *testing.T) {
	propID := ulid.MustNew(ulid.Now(), nil)
	parentID := ulid.MustNew(ulid.Now(), nil)
	locationID := ulid.MustNew(ulid.Now(), nil)
	value := "test-value"
	owner := "owner:01ABC"

	tests := []struct {
		name               string
		resourceID         string
		property           *world.EntityProperty
		repoErr            error
		resolverResult     *ulid.ULID
		resolverErr        error
		expectedAttrs      map[string]any
		expectedErr        string
		expectResolverCall bool
	}{
		{
			name:       "full property with location parent",
			resourceID: "property:" + propID.String(),
			property: &world.EntityProperty{
				ID:         propID,
				ParentType: "location",
				ParentID:   parentID,
				Name:       "test-prop",
				Value:      &value,
				Owner:      &owner,
				Visibility: "public",
			},
			expectedAttrs: map[string]any{
				"id":                  propID.String(),
				"parent_type":         "location",
				"parent_id":           parentID.String(),
				"name":                "test-prop",
				"value":               "test-value",
				"has_value":           true,
				"owner":               "owner:01ABC",
				"has_owner":           true,
				"visibility":          "public",
				"parent_location":     parentID.String(),
				"has_parent_location": true,
			},
			expectResolverCall: false, // location parent doesn't need resolver
		},
		{
			name:       "property on character parent",
			resourceID: "property:" + propID.String(),
			property: &world.EntityProperty{
				ID:         propID,
				ParentType: "character",
				ParentID:   parentID,
				Name:       "test-prop",
				Value:      &value,
				Owner:      &owner,
				Visibility: "public",
			},
			resolverResult: &locationID,
			expectedAttrs: map[string]any{
				"id":                  propID.String(),
				"parent_type":         "character",
				"parent_id":           parentID.String(),
				"name":                "test-prop",
				"value":               "test-value",
				"has_value":           true,
				"owner":               "owner:01ABC",
				"has_owner":           true,
				"visibility":          "public",
				"parent_location":     locationID.String(),
				"has_parent_location": true,
			},
			expectResolverCall: true,
		},
		{
			name:       "property on object parent",
			resourceID: "property:" + propID.String(),
			property: &world.EntityProperty{
				ID:         propID,
				ParentType: "object",
				ParentID:   parentID,
				Name:       "test-prop",
				Value:      &value,
				Owner:      &owner,
				Visibility: "public",
			},
			resolverResult: &locationID,
			expectedAttrs: map[string]any{
				"id":                  propID.String(),
				"parent_type":         "object",
				"parent_id":           parentID.String(),
				"name":                "test-prop",
				"value":               "test-value",
				"has_value":           true,
				"owner":               "owner:01ABC",
				"has_owner":           true,
				"visibility":          "public",
				"parent_location":     locationID.String(),
				"has_parent_location": true,
			},
			expectResolverCall: true,
		},
		{
			name:       "property without value",
			resourceID: "property:" + propID.String(),
			property: &world.EntityProperty{
				ID:         propID,
				ParentType: "location",
				ParentID:   parentID,
				Name:       "test-prop",
				Value:      nil,
				Owner:      &owner,
				Visibility: "public",
			},
			expectedAttrs: map[string]any{
				"id":                  propID.String(),
				"parent_type":         "location",
				"parent_id":           parentID.String(),
				"name":                "test-prop",
				"value":               "",
				"has_value":           false,
				"owner":               "owner:01ABC",
				"has_owner":           true,
				"visibility":          "public",
				"parent_location":     parentID.String(),
				"has_parent_location": true,
			},
			expectResolverCall: false,
		},
		{
			name:       "property without owner",
			resourceID: "property:" + propID.String(),
			property: &world.EntityProperty{
				ID:         propID,
				ParentType: "location",
				ParentID:   parentID,
				Name:       "test-prop",
				Value:      &value,
				Owner:      nil,
				Visibility: "public",
			},
			expectedAttrs: map[string]any{
				"id":                  propID.String(),
				"parent_type":         "location",
				"parent_id":           parentID.String(),
				"name":                "test-prop",
				"value":               "test-value",
				"has_value":           true,
				"owner":               "",
				"has_owner":           false,
				"visibility":          "public",
				"parent_location":     parentID.String(),
				"has_parent_location": true,
			},
			expectResolverCall: false,
		},
		{
			name:       "resolver returns nil (unresolvable)",
			resourceID: "property:" + propID.String(),
			property: &world.EntityProperty{
				ID:         propID,
				ParentType: "character",
				ParentID:   parentID,
				Name:       "test-prop",
				Value:      &value,
				Owner:      &owner,
				Visibility: "public",
			},
			resolverResult: nil,
			expectedAttrs: map[string]any{
				"id":                  propID.String(),
				"parent_type":         "character",
				"parent_id":           parentID.String(),
				"name":                "test-prop",
				"value":               "test-value",
				"has_value":           true,
				"owner":               "owner:01ABC",
				"has_owner":           true,
				"visibility":          "public",
				"parent_location":     "",
				"has_parent_location": false,
			},
			expectResolverCall: true,
		},
		{
			name:       "resolver returns error",
			resourceID: "property:" + propID.String(),
			property: &world.EntityProperty{
				ID:         propID,
				ParentType: "character",
				ParentID:   parentID,
				Name:       "test-prop",
				Value:      &value,
				Owner:      &owner,
				Visibility: "public",
			},
			resolverErr: errors.New("resolver error"),
			expectedAttrs: map[string]any{
				"id":                  propID.String(),
				"parent_type":         "character",
				"parent_id":           parentID.String(),
				"name":                "test-prop",
				"value":               "test-value",
				"has_value":           true,
				"owner":               "owner:01ABC",
				"has_owner":           true,
				"visibility":          "public",
				"parent_location":     "",
				"has_parent_location": false,
			},
			expectResolverCall: true,
		},
		{
			name:          "wrong entity type - character",
			resourceID:    access.CharacterSubject(propID.String()),
			expectedAttrs: nil,
		},
		{
			name:          "wrong entity type - location",
			resourceID:    "location:" + propID.String(),
			expectedAttrs: nil,
		},
		{
			name:        "invalid ULID",
			resourceID:  "property:invalid",
			expectedErr: "INVALID_PROPERTY_ID",
		},
		{
			name:          "missing colon separator",
			resourceID:    "propertyinvalid",
			expectedAttrs: nil, // parseEntityID returns nil, nil for non-matching types
		},
		{
			name:        "repository error",
			resourceID:  "property:" + propID.String(),
			repoErr:     errors.New("db error"),
			expectedErr: "PROPERTY_FETCH_FAILED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockPropertyRepository{
				getFunc: func(_ context.Context, _ ulid.ULID) (*world.EntityProperty, error) {
					if tt.repoErr != nil {
						return nil, tt.repoErr
					}
					return tt.property, nil
				},
			}

			var resolverCalled bool
			resolver := &mockParentLocationResolver{
				resolveFunc: func(_ context.Context, _ string, _ ulid.ULID) (*ulid.ULID, error) {
					resolverCalled = true
					if tt.resolverErr != nil {
						return nil, tt.resolverErr
					}
					return tt.resolverResult, nil
				},
			}

			provider := NewPropertyProvider(repo, resolver)
			attrs, err := provider.ResolveResource(context.Background(), tt.resourceID)

			if tt.expectedErr != "" {
				require.Error(t, err)
				errutil.AssertErrorCode(t, err, tt.expectedErr)
				return
			}

			require.NoError(t, err)
			if tt.expectedAttrs == nil {
				assert.Nil(t, attrs)
			} else {
				assert.Equal(t, tt.expectedAttrs, attrs)
			}

			assert.Equal(t, tt.expectResolverCall, resolverCalled, "resolver call mismatch")
		})
	}
}

func TestPropertyProvider_ResolveResource_Timeout(t *testing.T) {
	propID := ulid.MustNew(ulid.Now(), nil)
	parentID := ulid.MustNew(ulid.Now(), nil)
	value := "test-value"
	owner := "owner:01ABC"

	repo := &mockPropertyRepository{
		getFunc: func(_ context.Context, _ ulid.ULID) (*world.EntityProperty, error) {
			return &world.EntityProperty{
				ID:         propID,
				ParentType: "character",
				ParentID:   parentID,
				Name:       "test-prop",
				Value:      &value,
				Owner:      &owner,
				Visibility: "public",
			}, nil
		},
	}

	resolver := &mockParentLocationResolver{
		resolveFunc: func(ctx context.Context, _ string, _ ulid.ULID) (*ulid.ULID, error) {
			// Simulate slow resolution
			select {
			case <-time.After(200 * time.Millisecond):
				locationID := ulid.MustNew(ulid.Now(), nil)
				return &locationID, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}

	provider := NewPropertyProvider(repo, resolver)
	attrs, err := provider.ResolveResource(context.Background(), "property:"+propID.String())

	require.NoError(t, err)
	assert.Equal(t, "", attrs["parent_location"], "parent_location should be empty on timeout")
	assert.Equal(t, false, attrs["has_parent_location"], "has_parent_location should be false on timeout")
}

func TestPropertyProvider_Schema(t *testing.T) {
	provider := NewPropertyProvider(nil, nil)
	schema := provider.Schema()

	require.NotNil(t, schema)
	assert.Equal(t, map[string]types.AttrType{
		"id":                  types.AttrTypeString,
		"parent_type":         types.AttrTypeString,
		"parent_id":           types.AttrTypeString,
		"name":                types.AttrTypeString,
		"value":               types.AttrTypeString,
		"has_value":           types.AttrTypeBool,
		"owner":               types.AttrTypeString,
		"has_owner":           types.AttrTypeBool,
		"visibility":          types.AttrTypeString,
		"parent_location":     types.AttrTypeString,
		"has_parent_location": types.AttrTypeBool,
	}, schema.Attributes)
}
