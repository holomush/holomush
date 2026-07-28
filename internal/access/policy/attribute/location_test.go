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

func (m *mockLocationRepository) Create(_ context.Context, _ *world.Location) (*wmodel.MutationDelta, error) {
	return nil, errors.New("not implemented")
}

func (m *mockLocationRepository) Update(_ context.Context, _ *world.Location) (*wmodel.MutationDelta, error) {
	return nil, errors.New("not implemented")
}

func (m *mockLocationRepository) Delete(_ context.Context, _ ulid.ULID, _ int) (*wmodel.MutationDelta, error) {
	return nil, errors.New("not implemented")
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

func TestLocationProviderContract(t *testing.T) {
	assertProviderContract(t, NewLocationProvider(&mockLocationRepository{}))
}

func TestLocationProviderSchema(t *testing.T) {
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
			// Per ADR holomush-ti1b: the shadows_id key is OMITTED from the
			// bag when is_shadow=false. The DSL evaluator's
			// missing-attr-→-false semantics preserve default-deny; an
			// empty-string sentinel would satisfy `"" == ""` against any
			// other unresolved peer attribute (motivating bug holomush-9gtl).
			expectAttrs: map[string]any{
				"id":            locID.String(),
				"type":          "persistent",
				"name":          "Test Room",
				"description":   "A test location",
				"owner_id":      ownerID.String(),
				"has_owner":     true,
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
			// Per ADR holomush-ti1b: both owner_id and shadows_id are OMITTED
			// when their witnesses are false. Emitting "" for either would
			// make two unresolved locations compare equal to each other (and
			// to any other unresolved peer attribute), producing a fail-open
			// permit in a default-deny system (motivating bug holomush-9gtl).
			expectAttrs: map[string]any{
				"id":            locID.String(),
				"type":          "scene",
				"name":          "RP Scene",
				"description":   "",
				"has_owner":     false,
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
			resourceID:  access.CharacterResource(ulid.Make().String()),
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
			// Per holomush-g776: non-ULID location refs (the canonical
			// case is "location:*" wildcard from bootstrap permission
			// grants) MUST be skipped gracefully — returning the parse
			// error here would fail-closed the bootstrap chain because
			// the resolver propagates the error and the engine evaluates
			// fail-closed. The wildcard pattern matching is handled at
			// the engine layer without per-instance attributes, so the
			// provider's job is to politely decline.
			name:        "invalid ULID format — bypass (holomush-g776)",
			resourceID:  "location:not-a-ulid",
			setupMock:   func(_ *mockLocationRepository) {},
			expectNil:   true,
			expectError: false,
		},
		{
			// Companion to the case above — the literal wildcard form
			// the bootstrap actually emits when granting "write any
			// location" capability. Same expectation: provider declines,
			// engine handles the pattern.
			name:        "wildcard ID — bypass (holomush-g776)",
			resourceID:  "location:*",
			setupMock:   func(_ *mockLocationRepository) {},
			expectNil:   true,
			expectError: false,
		},
		{
			name:           "missing colon separator",
			resourceID:     "location" + locID.String(),
			setupMock:      func(_ *mockLocationRepository) {},
			expectError:    true,
			errorSubstring: "invalid entity ID format",
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

// TestTwoLocationsWithUnresolvedOptionalAttributesDoNotCompareEqualToEachOther
// pins the security property behind the omit-don't-sentinel rule, not merely
// the shape of a single bag.
//
// Motivating issue: holomush/holomush#4793 ("attribute providers emit
// empty-string sentinel — latent fail-open"). Per ADR holomush-ti1b providers
// MUST omit an unresolved optional attribute; per ADR holomush-iv43 the DSL
// evaluator's fail-safe semantics then render every operator false for that
// missing attribute. The two ADRs only compose if the key is genuinely absent:
// a sentinel of ANY value (empty string or otherwise) makes two independently
// unresolved resources satisfy an equality comparison, which is what a
// colocation-style permit seed would match on. If you are here because you want
// to reintroduce a placeholder value so a seed "sees" the attribute, that is the
// bug this test exists to prevent — add a `has_X` witness check to the seed
// instead.
func TestTwoLocationsWithUnresolvedOptionalAttributesDoNotCompareEqualToEachOther(t *testing.T) {
	t.Parallel()

	// Two DISTINCT locations, each unowned and each shadowing nothing —
	// the exact pair a fail-open equality would wrongly match.
	firstID := ulid.Make()
	secondID := ulid.Make()

	resolve := func(t *testing.T, id ulid.ULID) map[string]any {
		t.Helper()
		repo := &mockLocationRepository{
			getFunc: func(_ context.Context, got ulid.ULID) (*world.Location, error) {
				require.Equal(t, id, got)
				return &world.Location{
					ID:           id,
					Type:         world.LocationTypeScene,
					Name:         "Unowned " + id.String(),
					ReplayPolicy: "last:0",
					OwnerID:      nil,
					ShadowsID:    nil,
				}, nil
			},
		}
		attrs, err := NewLocationProvider(repo).ResolveResource(context.Background(), "location:"+id.String())
		require.NoError(t, err)
		require.NotNil(t, attrs)
		return attrs
	}

	first := resolve(t, firstID)
	second := resolve(t, secondID)

	for _, key := range []string{"owner_id", "shadows_id"} {
		_, inFirst := first[key]
		_, inSecond := second[key]

		// The guarantee: a seed comparing resource.location.<key> across two
		// unresolved locations must find nothing to compare. Both sides
		// carrying the key is the fail-open condition regardless of value.
		assert.False(t, inFirst && inSecond,
			"both bags carry %q while unresolved — a seed comparing this attribute "+
				"across the two locations would match (ADR holomush-ti1b / holomush-iv43, issue #4793)", key)
		assert.False(t, inFirst, "%q MUST be omitted when its witness is false", key)
		assert.False(t, inSecond, "%q MUST be omitted when its witness is false", key)
	}

	// Witnesses are still emitted on the unresolved path, so a seed can test
	// existence explicitly rather than reaching for a sentinel.
	assert.Equal(t, false, first["has_owner"])
	assert.Equal(t, false, first["is_shadow"])

	// Positive control: the fix omits the key only on the unresolved branch.
	// A resolved owner must still be present and must still differ between two
	// distinct locations, proving the attribute was not dropped unconditionally.
	ownerID := ulid.Make()
	repo := &mockLocationRepository{
		getFunc: func(_ context.Context, got ulid.ULID) (*world.Location, error) {
			return &world.Location{
				ID:           got,
				Type:         world.LocationTypeScene,
				Name:         "Owned",
				ReplayPolicy: "last:0",
				OwnerID:      &ownerID,
			}, nil
		},
	}
	owned, err := NewLocationProvider(repo).ResolveResource(context.Background(), "location:"+firstID.String())
	require.NoError(t, err)
	assert.Equal(t, ownerID.String(), owned["owner_id"])
	assert.Equal(t, true, owned["has_owner"])
}
