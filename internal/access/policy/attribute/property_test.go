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

// mockCharacterOwnerResolver is a test double for CharacterOwnerResolver.
// scopes maps player id → that player's COMPLETE character-id set.
type mockCharacterOwnerResolver struct {
	scopes map[string][]string
	err    error
	calls  int
	gotIDs []string
}

func (m *mockCharacterOwnerResolver) ResolveOwnerScopes(_ context.Context, characterIDs []string) (map[string][]string, error) {
	m.calls++
	m.gotIDs = append(m.gotIDs, characterIDs...)
	if m.err != nil {
		return nil, m.err
	}
	// Mirror the real resolver: return only players who own at least one of the
	// supplied ids, with their COMPLETE character set.
	want := make(map[string]struct{}, len(characterIDs))
	for _, id := range characterIDs {
		want[id] = struct{}{}
	}
	out := map[string][]string{}
	for player, chars := range m.scopes {
		for _, c := range chars {
			if _, ok := want[c]; ok {
				out[player] = chars
				break
			}
		}
	}
	return out, nil
}

// withNoDerivedPeers stamps the three derived-peer WITNESSES onto an expected
// attribute map. Every table row below constructs the provider with a NIL
// CharacterOwnerResolver, so no derived peer resolves — but the has_X witnesses
// are emitted on EVERY code path per .claude/rules/abac-providers.md, including
// the no-resolver path. Only the VALUE keys are omitted.
//
// Wrapping rather than injecting inside the runner keeps the expectation
// explicit at each row: if the provider ever starts emitting a derived VALUE
// with a nil resolver, these rows go RED on the surplus key.
func withNoDerivedPeers(m map[string]any) map[string]any {
	m["has_owner_player_id"] = false
	m["has_visible_to_players"] = false
	m["has_excluded_from_players"] = false
	return m
}

func TestPropertyProviderContract(t *testing.T) {
	assertProviderContract(t, NewPropertyProvider(nil, nil, nil))
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
			expectedAttrs: withNoDerivedPeers(map[string]any{
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
			}),
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
			expectedAttrs: withNoDerivedPeers(map[string]any{
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
			}),
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
			expectedAttrs: withNoDerivedPeers(map[string]any{
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
			}),
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
			// Per ADR holomush-ti1b: the value key is OMITTED when
			// has_value=false. An empty-string sentinel would satisfy
			// `"" == ""` against any other unresolved peer attribute,
			// producing a fail-open permit (motivating bug holomush-9gtl).
			expectedAttrs: withNoDerivedPeers(map[string]any{
				"id":                  propID.String(),
				"parent_type":         "location",
				"parent_id":           parentID.String(),
				"name":                "test-prop",
				"has_value":           false,
				"owner":               "owner:01ABC",
				"has_owner":           true,
				"visibility":          "public",
				"parent_location":     parentID.String(),
				"has_parent_location": true,
			}),
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
			// Per ADR holomush-ti1b: the owner key is OMITTED when
			// has_owner=false. This one is load-bearing for a live seed —
			// seed:property-private-read and seed:property-owner-write both
			// gate on `resource.property.owner == principal.character.id`
			// (internal/access/policy/seed.go:119,131) — so an unresolved
			// owner MUST NOT be comparable (motivating bug holomush-9gtl).
			expectedAttrs: withNoDerivedPeers(map[string]any{
				"id":                  propID.String(),
				"parent_type":         "location",
				"parent_id":           parentID.String(),
				"name":                "test-prop",
				"value":               "test-value",
				"has_value":           true,
				"has_owner":           false,
				"visibility":          "public",
				"parent_location":     parentID.String(),
				"has_parent_location": true,
			}),
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
			// Per ADR holomush-ti1b: parent_location key OMITTED when
			// has_parent_location=false (un-locatable parent).
			expectedAttrs: withNoDerivedPeers(map[string]any{
				"id":                  propID.String(),
				"parent_type":         "character",
				"parent_id":           parentID.String(),
				"name":                "test-prop",
				"value":               "test-value",
				"has_value":           true,
				"owner":               "owner:01ABC",
				"has_owner":           true,
				"visibility":          "public",
				"has_parent_location": false,
			}),
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
			// Per ADR holomush-ti1b: parent_location key OMITTED when
			// has_parent_location=false (resolver error treated as
			// un-locatable parent).
			expectedAttrs: withNoDerivedPeers(map[string]any{
				"id":                  propID.String(),
				"parent_type":         "character",
				"parent_id":           parentID.String(),
				"name":                "test-prop",
				"value":               "test-value",
				"has_value":           true,
				"owner":               "owner:01ABC",
				"has_owner":           true,
				"visibility":          "public",
				"has_parent_location": false,
			}),
			expectResolverCall: true,
		},
		{
			name:          "wrong entity type - character",
			resourceID:    access.CharacterResource(propID.String()),
			expectedAttrs: nil,
		},
		{
			name:          "wrong entity type - location",
			resourceID:    "location:" + propID.String(),
			expectedAttrs: nil,
		},
		{
			// holomush-o8g6 BEHAVIOR CHANGE: was INVALID_PROPERTY_ID.
			// Now wildcard-tolerant — matches Location/Character/Object
			// peer behavior via parseEntityResource. The engine evaluates
			// target-type seed matches without per-instance attrs (see
			// holomush-g776). No production caller emits "property:<non-ulid>"
			// today; the new behavior is uniformly safer for future
			// wildcard-emitting capability checks.
			name:          "invalid ULID — wildcard-tolerant (holomush-o8g6)",
			resourceID:    "property:invalid",
			expectedAttrs: nil,
		},
		{
			// holomush-o8g6 BEHAVIOR CHANGE: was (nil, nil) via the old
			// parseEntityID prefix check (silent "wrong type"). The unified
			// parseEntityResource treats a missing colon as a malformed
			// grammar error — distinct from peer-type or wildcard cases.
			// A caller emitting "propertyinvalid" with no colon is buggy
			// (access.PropertyResource always emits "property:<id>") and
			// the error makes that explicit instead of masking it.
			name:        "missing colon separator — grammar error (holomush-o8g6)",
			resourceID:  "propertyinvalid",
			expectedErr: "INVALID_RESOURCE_ID",
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

			provider := NewPropertyProvider(repo, resolver, nil)
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

func TestPropertyProviderResolveResourceTimeout(t *testing.T) {
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

	provider := NewPropertyProvider(repo, resolver, nil)
	attrs, err := provider.ResolveResource(context.Background(), "property:"+propID.String())

	require.NoError(t, err)
	_, present := attrs["parent_location"]
	assert.False(t, present,
		"ADR holomush-ti1b: parent_location key MUST be absent on timeout (un-locatable parent)")
	assert.Equal(t, false, attrs["has_parent_location"], "has_parent_location should be false on timeout")
}

func TestPropertyProviderSchema(t *testing.T) {
	provider := NewPropertyProvider(nil, nil, nil)
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
		// Registered for restricted-visibility seeds (visible_to/excluded_from
		// are populated into the bag when non-empty; omitted otherwise per ti1b).
		"visible_to":    types.AttrTypeStringList,
		"excluded_from": types.AttrTypeStringList,
		// Plan 02-13: player-keyed peers DERIVED from the three character-keyed
		// fields above. Declaring them is load-bearing — the resolver drops any
		// emitted key not in the schema, so an undeclared owner_player_id would
		// be silently absent from the bag a policy evaluates.
		"owner_player_id":           types.AttrTypeString,
		"has_owner_player_id":       types.AttrTypeBool,
		"visible_to_players":        types.AttrTypeStringList,
		"has_visible_to_players":    types.AttrTypeBool,
		"excluded_from_players":     types.AttrTypeStringList,
		"has_excluded_from_players": types.AttrTypeBool,
	}, schema.Attributes)
}

// --- Plan 02-13 Task 2: derived player-keyed peers ---------------------------
//
// The row's `owner`, `visible_to` and `excluded_from` fields are CHARACTER-keyed.
// A `viewer:` subject is PLAYER-flavored. The DSL cannot bridge them: `in`
// (evalInExpr) needs a SCALAR left operand and a slice right operand, and
// containsAll/containsAny take a LITERAL needle list, so no DSL expression can
// intersect two attribute lists. The relation is therefore resolved server-side,
// in the provider, and the policy compares player to player.
//
// Per 02-CONTEXT.md D-27 each peer is derived in the direction that CANNOT widen
// its own policy's effect: ALL on the permit side, ANY on the forbid side.

const (
	// One player, two characters. Only charA is ever named in a row's field —
	// which is exactly what separates the ALL direction from a plain union.
	twoCharPlayer = "01J0P0P0P0P0P0P0P0P0P0P0P1"
	twoCharA      = "01J0C0C0C0C0C0C0C0C0C0C0A1"
	twoCharB      = "01J0C0C0C0C0C0C0C0C0C0C0B1"

	// A player holding exactly one character — the shape that DOES qualify
	// under the permit-side ALL direction.
	soloPlayer = "01J0P0P0P0P0P0P0P0P0P0P0S1"
	soloChar   = "01J0C0C0C0C0C0C0C0C0C0C0S1"
)

func twoPlayerScopes() map[string][]string {
	return map[string][]string{
		twoCharPlayer: {twoCharA, twoCharB},
		soloPlayer:    {soloChar},
	}
}

func resolveTestProperty(
	t *testing.T, prop *world.EntityProperty, owners CharacterOwnerResolver,
) map[string]any {
	t.Helper()
	repo := &mockPropertyRepository{
		getFunc: func(_ context.Context, _ ulid.ULID) (*world.EntityProperty, error) {
			return prop, nil
		},
	}
	provider := NewPropertyProvider(repo, &mockParentLocationResolver{
		resolveFunc: func(_ context.Context, _ string, _ ulid.ULID) (*ulid.ULID, error) {
			return nil, nil
		},
	}, owners)
	attrs, err := provider.ResolveResource(context.Background(), "property:"+prop.ID.String())
	require.NoError(t, err)
	require.NotNil(t, attrs)
	return attrs
}

func testProperty(owner *string, visibleTo, excludedFrom []string) *world.EntityProperty {
	return &world.EntityProperty{
		ID:           ulid.MustNew(ulid.Now(), nil),
		ParentType:   "location",
		ParentID:     ulid.MustNew(ulid.Now(), nil),
		Name:         "bio",
		Visibility:   "restricted",
		Owner:        owner,
		VisibleTo:    visibleTo,
		ExcludedFrom: excludedFrom,
	}
}

// TestPropertyProviderDerivesTheTwoDirectionsOppositelyOnOneRow is the single
// assertion that distinguishes D-27's rule from the plain player union, and the
// mechanical guard on that decision.
//
// ONE fixture: a player with TWO characters, exactly ONE of which (charA) is
// listed in BOTH visible_to and excluded_from.
//
//   - visible_to_players (permit side, ALL): the player is ABSENT — charB was
//     never granted, so the row's grant to charA does not generalize to the human.
//   - excluded_from_players (forbid side, ANY): the player is PRESENT — one of
//     their characters was excluded, and losing an exclusion is the widening on
//     this side.
//
// Under a plain union the player appears in BOTH. This test is what goes RED if
// the derivation ever drifts back — the "simplification" of the ALL branch into
// an ANY branch is one token away at all times and reads as cleanup.
func TestPropertyProviderDerivesTheTwoDirectionsOppositelyOnOneRow(t *testing.T) {
	owners := &mockCharacterOwnerResolver{scopes: twoPlayerScopes()}
	attrs := resolveTestProperty(t,
		testProperty(nil, []string{twoCharA}, []string{twoCharA}), owners)

	// Permit side, ALL: absent, and since NO player qualifies the key is omitted
	// entirely (never an empty list — that is the list-flavored sentinel).
	_, present := attrs["visible_to_players"]
	assert.False(t, present,
		"D-27 ALL direction: a player whose OTHER character was not granted MUST NOT "+
			"enter visible_to_players, and with no qualifying player the key is OMITTED")
	assert.Equal(t, false, attrs["has_visible_to_players"])

	// Forbid side, ANY: present.
	assert.Equal(t, []any{twoCharPlayer}, attrs["excluded_from_players"],
		"D-27 ANY direction: any excluded character excludes the player")
	assert.Equal(t, true, attrs["has_excluded_from_players"])
}

// TestPropertyProviderVisibleToPlayersRequiresEveryCharacterOfThePlayer is the
// paired POSITIVE CONTROL for the absence above: on the same resolver fixture, a
// row that names BOTH of the two-character player's characters DOES yield the
// player, and the single-character player qualifies from one listing.
func TestPropertyProviderVisibleToPlayersRequiresEveryCharacterOfThePlayer(t *testing.T) {
	owners := &mockCharacterOwnerResolver{scopes: twoPlayerScopes()}
	attrs := resolveTestProperty(t,
		testProperty(nil, []string{twoCharA, twoCharB, soloChar}, nil), owners)

	got, ok := attrs["visible_to_players"].([]any)
	require.True(t, ok, "visible_to_players must be emitted when players qualify")
	assert.ElementsMatch(t, []any{twoCharPlayer, soloPlayer}, got,
		"a player qualifies once EVERY character of theirs appears in visible_to")
	assert.Equal(t, true, attrs["has_visible_to_players"])
}

// TestPropertyProviderOwnerPlayerIDFollowsThePermitSideAllDirection states the
// permit-side rule as BEHAVIOR rather than as prose, on one fixture carrying
// both the qualifying and non-qualifying shape.
func TestPropertyProviderOwnerPlayerIDFollowsThePermitSideAllDirection(t *testing.T) {
	t.Run("single-character player: owner_player_id IS emitted", func(t *testing.T) {
		owner := soloChar
		owners := &mockCharacterOwnerResolver{scopes: twoPlayerScopes()}
		attrs := resolveTestProperty(t, testProperty(&owner, nil, nil), owners)

		assert.Equal(t, soloPlayer, attrs["owner_player_id"])
		assert.Equal(t, true, attrs["has_owner_player_id"])
	})

	t.Run("player holding a second character: owner_player_id is ABSENT", func(t *testing.T) {
		owner := twoCharA
		owners := &mockCharacterOwnerResolver{scopes: twoPlayerScopes()}
		attrs := resolveTestProperty(t, testProperty(&owner, nil, nil), owners)

		_, present := attrs["owner_player_id"]
		assert.False(t, present,
			"D-27: the row named a CHARACTER; generalizing it to the human behind a "+
				"second, unnamed character is the widening the permit side declines")
		assert.Equal(t, false, attrs["has_owner_player_id"])
	})
}

// TestPropertyProviderOmitsDerivedPeersOnEveryUnresolvedPath asserts absence by
// KEY PRESENCE (comma-ok), never by comparing a value to its zero — an empty
// string or empty list is a RESOLVED value the DSL evaluates against, which is
// exactly the fail-open shape .claude/rules/abac-providers.md forbids.
func TestPropertyProviderOmitsDerivedPeersOnEveryUnresolvedPath(t *testing.T) {
	t.Run("nil owner: owner_player_id absent", func(t *testing.T) {
		owners := &mockCharacterOwnerResolver{scopes: twoPlayerScopes()}
		attrs := resolveTestProperty(t, testProperty(nil, nil, nil), owners)

		_, present := attrs["owner_player_id"]
		assert.False(t, present)
		assert.Equal(t, false, attrs["has_owner_player_id"])
	})

	t.Run("owner names a character with no resolvable player: absent, and the row stays ownED", func(t *testing.T) {
		owner := "01J0C0C0C0C0C0C0C0C0C0C0Z1" // in no player's scope set
		owners := &mockCharacterOwnerResolver{scopes: twoPlayerScopes()}
		attrs := resolveTestProperty(t, testProperty(&owner, nil, nil), owners)

		_, present := attrs["owner_player_id"]
		assert.False(t, present)
		assert.Equal(t, false, attrs["has_owner_player_id"])

		// An unresolvable owner is NOT an ownerless row — a different
		// authorization case. The character-keyed pair MUST stay intact so a
		// policy can still tell them apart.
		assert.Equal(t, owner, attrs["owner"])
		assert.Equal(t, true, attrs["has_owner"])
	})

	t.Run("no resolver configured: all three absent, all three witnesses false", func(t *testing.T) {
		owner := soloChar
		attrs := resolveTestProperty(t,
			testProperty(&owner, []string{soloChar}, []string{soloChar}), nil)

		for _, k := range []string{"owner_player_id", "visible_to_players", "excluded_from_players"} {
			_, present := attrs[k]
			assert.False(t, present, "%s MUST be absent with no resolver", k)
		}
		assert.Equal(t, false, attrs["has_owner_player_id"])
		assert.Equal(t, false, attrs["has_visible_to_players"])
		assert.Equal(t, false, attrs["has_excluded_from_players"])
	})
}

// TestPropertyProviderOmitsAllThreeDerivedPeersTogetherOnResolverError pins that
// a resolution outage never produces a PARTIAL derived set.
//
// A bag carrying visible_to_players but not owner_player_id because one lookup
// failed would let a `restricted` twin permit while the `private` twin denies
// during the SAME outage — a verdict that depends on which query failed.
func TestPropertyProviderOmitsAllThreeDerivedPeersTogetherOnResolverError(t *testing.T) {
	owner := soloChar
	owners := &mockCharacterOwnerResolver{err: errors.New("owner resolver down")}
	attrs := resolveTestProperty(t,
		testProperty(&owner, []string{soloChar}, []string{soloChar}), owners)

	for _, k := range []string{"owner_player_id", "visible_to_players", "excluded_from_players"} {
		_, present := attrs[k]
		assert.False(t, present, "%s MUST be omitted on resolver error", k)
	}
	assert.Equal(t, false, attrs["has_owner_player_id"])
	assert.Equal(t, false, attrs["has_visible_to_players"])
	assert.Equal(t, false, attrs["has_excluded_from_players"])

	// Fail-safe, not fatal: the rest of the bag still resolves.
	assert.Equal(t, "bio", attrs["name"])
	assert.Equal(t, "restricted", attrs["visibility"])
}

// TestPropertyProviderDerivedPeersComeFromTheRowNeverFromTheCaller closes
// T-02-83: a peer populated from the REQUESTING subject would make every row
// match its own reader — a total authorization bypass that reads as a working
// feature. The provider takes no subject at all on this path; this pins that the
// emitted value tracks the ROW's owner.
func TestPropertyProviderDerivedPeersComeFromTheRowNeverFromTheCaller(t *testing.T) {
	owner := soloChar
	owners := &mockCharacterOwnerResolver{scopes: twoPlayerScopes()}
	attrs := resolveTestProperty(t, testProperty(&owner, nil, nil), owners)

	assert.Equal(t, soloPlayer, attrs["owner_player_id"],
		"owner_player_id is the player behind the ROW's owning character")
	assert.NotEqual(t, twoCharPlayer, attrs["owner_player_id"],
		"it is never some other player in scope — least of all the reader's")
}

// TestPropertyProviderCharacterKeyedFieldsAreUnchangedByTheDerivedPeers pins
// that the derived peers are strictly ADDITIVE, so the six shipped
// character-flavored seed:property-* policies still resolve identically.
func TestPropertyProviderCharacterKeyedFieldsAreUnchangedByTheDerivedPeers(t *testing.T) {
	owner := twoCharA
	owners := &mockCharacterOwnerResolver{scopes: twoPlayerScopes()}
	attrs := resolveTestProperty(t,
		testProperty(&owner, []string{twoCharA, soloChar}, []string{twoCharB}), owners)

	assert.Equal(t, twoCharA, attrs["owner"])
	assert.Equal(t, true, attrs["has_owner"])
	assert.Equal(t, []any{twoCharA, soloChar}, attrs["visible_to"])
	assert.Equal(t, []any{twoCharB}, attrs["excluded_from"])
}

// TestPropertyProviderResolvesEveryDerivedPeerFromOneResolverCall pins the query
// budget: a row with a large visible_to list costs ONE call, not N.
func TestPropertyProviderResolvesEveryDerivedPeerFromOneResolverCall(t *testing.T) {
	owner := soloChar
	owners := &mockCharacterOwnerResolver{scopes: twoPlayerScopes()}
	_ = resolveTestProperty(t,
		testProperty(&owner, []string{twoCharA, twoCharB}, []string{soloChar}), owners)

	assert.Equal(t, 1, owners.calls,
		"all three derived peers resolve from ONE ResolveOwnerScopes call")
	assert.ElementsMatch(t,
		[]string{soloChar, twoCharA, twoCharB},
		dedupe(owners.gotIDs),
		"the single call carries the union of every character id the row references")
}

func dedupe(in []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// TestPropertyProviderEmitsTheResourceAttributeName is 01-SPEC §8.5's remaining
// Phase-2 obligation, stated as a TEST rather than as a read instruction.
//
// Plans 02-07 and 02-08 both build their permit conjunction on `name` being
// present. Without this assertion a later refactor that drops the emission would
// silently void both — the conjunction's term would simply be false forever, with
// no error and no failing test.
func TestPropertyProviderEmitsTheResourceAttributeName(t *testing.T) {
	owners := &mockCharacterOwnerResolver{scopes: twoPlayerScopes()}
	attrs := resolveTestProperty(t, testProperty(nil, nil, nil), owners)

	name, present := attrs["name"]
	require.True(t, present, "01-SPEC §8.5: the property resource bag MUST carry `name`")
	assert.Equal(t, "bio", name)

	schema := NewPropertyProvider(nil, nil, nil).Schema()
	assert.Equal(t, types.AttrTypeString, schema.Attributes["name"],
		"an emitted-but-undeclared key is DROPPED by the resolver — silently absent")
}

// TestPropertyProviderSchemaDeclaresEveryKeyItEmits is the emitted-vs-declared
// COHERENCE gate, by symmetric difference over sets derived from the code on
// both sides.
//
// The resolver drops and counts any provider attribute whose key is not in the
// namespace schema, so an emitted-but-undeclared `owner_player_id` is silently
// absent — the same invisible fail-closed this plan exists to remove.
func TestPropertyProviderSchemaDeclaresEveryKeyItEmits(t *testing.T) {
	owner := soloChar
	owners := &mockCharacterOwnerResolver{scopes: twoPlayerScopes()}
	// A row shaped to emit EVERY optional key at once.
	value := "she/her"
	prop := testProperty(&owner, []string{soloChar}, []string{twoCharA})
	prop.Value = &value
	prop.ParentType = "location"
	attrs := resolveTestProperty(t, prop, owners)

	declared := NewPropertyProvider(nil, nil, nil).Schema().Attributes

	var emittedNotDeclared, declaredNotEmitted []string
	for k := range attrs {
		if _, ok := declared[k]; !ok {
			emittedNotDeclared = append(emittedNotDeclared, k)
		}
	}
	for k := range declared {
		if _, ok := attrs[k]; !ok {
			declaredNotEmitted = append(declaredNotEmitted, k)
		}
	}

	assert.Empty(t, emittedNotDeclared,
		"every emitted key MUST be declared in Schema() — an undeclared key is DROPPED "+
			"by the resolver and silently absent from the bag a policy evaluates")
	// This fixture is built to emit every optional key, so nothing declared may
	// go unemitted either — that would mean a schema entry no code path fills.
	assert.Empty(t, declaredNotEmitted,
		"this fixture emits every optional key; a declared-but-unemitted key means a "+
			"schema entry no code path fills")
}
