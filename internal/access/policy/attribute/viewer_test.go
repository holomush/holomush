// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"context"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/pkg/errutil"
)

const testViewerPlayerULID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

// staticRoleLookup returns a PlayerRoleLookup that always answers with roles.
func staticRoleLookup(roles ...string) PlayerRoleLookup {
	return func(_ context.Context, _ string) ([]string, error) {
		return roles, nil
	}
}

// failingRoleLookup returns a PlayerRoleLookup that always errors.
func failingRoleLookup() PlayerRoleLookup {
	return func(_ context.Context, _ string) ([]string, error) {
		return nil, oops.Code("ROLE_LOOKUP_BOOM").Errorf("role store unavailable")
	}
}

func TestViewerTierProviderNamespaceIsViewer(t *testing.T) {
	assert.Equal(t, "viewer", NewViewerTierProvider().Namespace())
}

func TestViewerTierProviderResolveResourceAlwaysDeclines(t *testing.T) {
	// A viewer is a Subject, never a Resource (01-SPEC §8.4.1).
	attrs, err := NewViewerTierProvider().ResolveResource(context.Background(), "profile:"+testViewerPlayerULID)
	require.NoError(t, err)
	assert.Nil(t, attrs)
}

func TestViewerTierProviderDeclinesAForeignEntityType(t *testing.T) {
	attrs, err := NewViewerTierProvider().ResolveSubject(context.Background(), "character:"+testViewerPlayerULID)
	require.NoError(t, err)
	assert.Nil(t, attrs)
}

func TestViewerTierProviderResolvesEachRungOfTheTierLadder(t *testing.T) {
	tests := []struct {
		name          string
		subject       string
		wantTier      string
		wantPlayerID  string
		wantHasPlayer bool
	}{
		{
			name:          "anonymous rung resolves with no player identity",
			subject:       "viewer:anonymous",
			wantTier:      access.ViewerTierAnonymous,
			wantHasPlayer: false,
		},
		{
			name:          "guest rung resolves with its player ULID",
			subject:       "viewer:guest:" + testViewerPlayerULID,
			wantTier:      access.ViewerTierGuest,
			wantPlayerID:  testViewerPlayerULID,
			wantHasPlayer: true,
		},
		{
			name:          "player rung resolves with its player ULID",
			subject:       "viewer:player:" + testViewerPlayerULID,
			wantTier:      access.ViewerTierPlayer,
			wantPlayerID:  testViewerPlayerULID,
			wantHasPlayer: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs, err := NewViewerTierProvider().ResolveSubject(context.Background(), tt.subject)
			require.NoError(t, err)
			require.NotNil(t, attrs)

			assert.Equal(t, tt.wantTier, attrs["tier"])
			assert.Equal(t, tt.wantHasPlayer, attrs["has_player_id"])
			if tt.wantHasPlayer {
				assert.Equal(t, tt.wantPlayerID, attrs["player_id"])
			}
		})
	}
}

// TestViewerTierProviderOmitsPlayerIDForTheAnonymousRung pins the
// omit-don't-sentinel contract (ADR holomush-ti1b, .claude/rules/abac-providers.md)
// by KEY PRESENCE, not by value comparison: an empty-string sentinel satisfies
// `"" == ""` against any other unresolved peer attribute and creates a fail-open
// match in a default-deny system.
//
// The absence assertion is paired on the same fixture with a rung that DOES
// carry a player, so the absence cannot pass because the provider returns
// nothing at all.
func TestViewerTierProviderOmitsPlayerIDForTheAnonymousRung(t *testing.T) {
	p := NewViewerTierProvider()
	ctx := context.Background()

	anon, err := p.ResolveSubject(ctx, "viewer:anonymous")
	require.NoError(t, err)
	require.NotNil(t, anon)

	got, ok := anon["player_id"]
	assert.False(t, ok,
		"player_id MUST be ABSENT on the anonymous rung, not present-and-empty; got value %#v (present=%v)", got, ok)
	assert.Equal(t, false, anon["has_player_id"], "the has_player_id witness is emitted on every code path")

	// Positive control on the same fixture: the key IS present when there is a player.
	withPlayer, err := p.ResolveSubject(ctx, "viewer:player:"+testViewerPlayerULID)
	require.NoError(t, err)
	_, ok = withPlayer["player_id"]
	assert.True(t, ok, "player_id MUST be present on the player rung — otherwise the absence test above is vacuous")
}

func TestViewerTierProviderRejectsAnUnrecognizedTierToken(t *testing.T) {
	p := NewViewerTierProvider()
	ctx := context.Background()

	_, err := p.ResolveSubject(ctx, "viewer:spectator:"+testViewerPlayerULID)
	errutil.AssertErrorCode(t, err, "INVALID_VIEWER_TIER")

	// Positive control: a recognized tier on the same fixture resolves.
	attrs, err := p.ResolveSubject(ctx, "viewer:player:"+testViewerPlayerULID)
	require.NoError(t, err)
	assert.Equal(t, access.ViewerTierPlayer, attrs["tier"])
}

func TestViewerTierProviderRejectsAnEmptyIdentifierHalf(t *testing.T) {
	tests := []struct {
		name    string
		subject string
	}{
		{name: "bare viewer prefix", subject: "viewer:"},
		{name: "player rung with no ULID", subject: "viewer:player:"},
		{name: "guest rung with no ULID", subject: "viewer:guest:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewViewerTierProvider().ResolveSubject(context.Background(), tt.subject)
			errutil.AssertErrorCode(t, err, "INVALID_ENTITY_ID")
		})
	}
}

func TestViewerTierProviderRejectsANonULIDPlayerIdentifier(t *testing.T) {
	_, err := NewViewerTierProvider().ResolveSubject(context.Background(), "viewer:guest:not-a-ulid")
	errutil.AssertErrorCode(t, err, "INVALID_VIEWER_PLAYER_ID")
}

func TestViewerTierProviderSchemaDeclaresEveryKeyItEmits(t *testing.T) {
	// The resolver drops and counts any provider attribute whose key is not in
	// the namespace schema, so an undeclared key is silently absent rather than
	// loudly wrong.
	schema := NewViewerTierProvider().Schema()
	require.NotNil(t, schema)

	assert.Equal(t, map[string]types.AttrType{
		"tier":          types.AttrTypeString,
		"player_id":     types.AttrTypeString,
		"has_player_id": types.AttrTypeBool,
		"roles":         types.AttrTypeStringList,
		"has_roles":     types.AttrTypeBool,
	}, schema.Attributes)
}

func TestViewerTierProviderResolvesRolesPerPlayerWhenALookupIsConfigured(t *testing.T) {
	tests := []struct {
		name    string
		subject string
	}{
		{name: "player rung resolves roles", subject: "viewer:player:" + testViewerPlayerULID},
		// A guest is a player row too, and §10.5's verdict is per player.
		{name: "guest rung resolves roles", subject: "viewer:guest:" + testViewerPlayerULID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewViewerTierProvider(WithViewerRoleLookup(staticRoleLookup("admin", "player")))
			attrs, err := p.ResolveSubject(context.Background(), tt.subject)
			require.NoError(t, err)

			assert.Equal(t, []string{"admin", "player"}, attrs["roles"])
			assert.Equal(t, true, attrs["has_roles"])
		})
	}
}

// TestViewerTierProviderOmitsRolesOnEveryUnresolvedPath asserts the list-flavored
// omit-don't-sentinel rule: an empty list is a RESOLVED value a containsAny-shaped
// condition would evaluate against, so "unknown" must be absence, never []string{}.
//
// Each omit case is paired with a configured-lookup control on the same fixture,
// so an omission cannot pass because the provider emits nothing.
func TestViewerTierProviderOmitsRolesOnEveryUnresolvedPath(t *testing.T) {
	ctx := context.Background()
	playerSubject := "viewer:player:" + testViewerPlayerULID

	tests := []struct {
		name     string
		provider *ViewerTierProvider
		subject  string
	}{
		{
			name:     "anonymous rung has no player to resolve roles for",
			provider: NewViewerTierProvider(WithViewerRoleLookup(staticRoleLookup("admin"))),
			subject:  "viewer:anonymous",
		},
		{
			name:     "no role lookup is configured",
			provider: NewViewerTierProvider(),
			subject:  playerSubject,
		},
		{
			name:     "the role lookup errors",
			provider: NewViewerTierProvider(WithViewerRoleLookup(failingRoleLookup())),
			subject:  playerSubject,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs, err := tt.provider.ResolveSubject(ctx, tt.subject)
			require.NoError(t, err, "an unresolved roles lookup MUST NOT fail the whole resolution")
			require.NotNil(t, attrs)

			got, ok := attrs["roles"]
			assert.False(t, ok,
				"roles MUST be ABSENT, not present-and-empty; got value %#v (present=%v)", got, ok)
			assert.Equal(t, false, attrs["has_roles"], "the has_roles witness is emitted on every code path")

			// Positive control on the same fixture shape: a configured lookup on a
			// player rung DOES emit roles, so the absence above is not vacuous.
			control := NewViewerTierProvider(WithViewerRoleLookup(staticRoleLookup("admin")))
			controlAttrs, err := control.ResolveSubject(ctx, playerSubject)
			require.NoError(t, err)
			_, ok = controlAttrs["roles"]
			assert.True(t, ok, "roles MUST be present when a lookup resolves them")
		})
	}
}
