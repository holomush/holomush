// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package setup_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/access/setup"
	"github.com/holomush/holomush/internal/store"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/test/testutil"
)

// buildProductionShapeStack builds the stack with the FULL production-shape
// config — the same fields internal/access/setup/subsystem.go passes.
func buildProductionShapeStack(t *testing.T, pool *pgxpool.Pool) *setup.ABACStack {
	t.Helper()
	roleStore := store.NewPostgresRoleStore(pool)

	stack, err := setup.BuildABACStack(context.Background(), setup.ABACConfig{
		Pool:                   pool,
		CharacterRepo:          worldpostgres.NewCharacterRepository(pool),
		LocationRepo:           worldpostgres.NewLocationRepository(pool),
		ObjectRepo:             worldpostgres.NewObjectRepository(pool),
		PropertyRepo:           worldpostgres.NewPropertyRepository(pool),
		ParentLocationResolver: worldpostgres.NewParentLocationResolver(pool),
		CharacterOwnerResolver: worldpostgres.NewCharacterOwnerResolver(pool),
		RoleStore:              roleStore,
		PlayerRoleLookup:       roleStore.PlayerRoles,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stack.Close() })
	return stack
}

func freshPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)
	pool, err := pgxpool.New(context.Background(), connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// TestBuildABACStackRegistersTheViewerNamespace closes 01-SPEC §8.4.1's Phase-2
// obligation 1.
//
// Skipping this registration is the exact failure the obligation names: an
// unregistered namespace does not error, principal.viewer.tier is simply absent,
// and the whole tier-floor family default-denies in production while unit tests
// that stub the bag stay green. Plan 02-03 shipped ViewerTierProvider
// deliberately UNREGISTERED; this is where it joins the stack.
func TestBuildABACStackRegistersTheViewerNamespace(t *testing.T) {
	stack := buildProductionShapeStack(t, freshPool(t))

	assert.Contains(t, stack.Resolver.RegisteredNamespaces(), "viewer",
		"BuildABACStack MUST register ViewerTierProvider — an unregistered namespace "+
			"default-denies its whole policy family with no error and no failing test")
}

// TestBuildABACStackResolvesViewerTierThroughTheRealStack asserts the viewer
// namespace resolves through the resolver BuildABACStack actually returns —
// not through a hand-built one, which is the drift a registration test that
// stubs the stack would miss.
func TestBuildABACStackResolvesViewerTierThroughTheRealStack(t *testing.T) {
	pool := freshPool(t)
	stack := buildProductionShapeStack(t, pool)
	ctx := context.Background()

	playerID := ulid.Make()
	_, err := pool.Exec(ctx, `
		INSERT INTO players (id, username, password_hash)
		VALUES ($1, $2, 'hash')`, playerID.String(), "viewer_"+playerID.String())
	require.NoError(t, err)

	subj, err := stack.Resolver.ResolveSubject(ctx,
		access.ViewerSubject(access.ViewerTierPlayer, playerID.String()))
	require.NoError(t, err)

	assert.Equal(t, access.ViewerTierPlayer, subj["viewer.tier"])
	assert.Equal(t, playerID.String(), subj["viewer.player_id"])
	assert.Equal(t, true, subj["viewer.has_player_id"])

	// The role lookup is populated from the concrete *PostgresRoleStore, so
	// viewer.roles resolves (to an empty list here — the key is PRESENT because
	// the lookup SUCCEEDED, which is a different fact from "unresolved").
	assert.Equal(t, true, subj["viewer.has_roles"],
		"PlayerRoleLookup is populated, so the lookup resolves rather than omitting")
	assert.Empty(t, subj["viewer.roles"])
}

// TestBuildABACStackResolvesViewerRolesForAnAdminPlayer is the paired positive
// control: a player whose character holds admin resolves a non-empty
// viewer.roles through the real stack. Without it, the empty-list assertion
// above could pass because the lookup was never wired at all.
func TestBuildABACStackResolvesViewerRolesForAnAdminPlayer(t *testing.T) {
	pool := freshPool(t)
	stack := buildProductionShapeStack(t, pool)
	ctx := context.Background()

	playerID := ulid.Make()
	charID := ulid.Make()
	_, err := pool.Exec(ctx, `
		INSERT INTO players (id, username, password_hash)
		VALUES ($1, $2, 'hash')`, playerID.String(), "admin_"+playerID.String())
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO characters (id, player_id, name)
		VALUES ($1, $2, $3)`, charID.String(), playerID.String(), "Admin_"+charID.String())
	require.NoError(t, err)
	require.NoError(t, store.NewPostgresRoleStore(pool).AddRole(ctx, charID.String(), access.RoleAdmin))

	subj, err := stack.Resolver.ResolveSubject(ctx,
		access.ViewerSubject(access.ViewerTierPlayer, playerID.String()))
	require.NoError(t, err)

	assert.Equal(t, true, subj["viewer.has_roles"])
	assert.Equal(t, []string{access.RoleAdmin}, subj["viewer.roles"],
		"viewer.roles resolves PER PLAYER through the concrete *PostgresRoleStore")
}

// TestBuildABACStackResolvesDerivedOwnerPlayerIDThroughTheRealStack proves the
// CharacterOwnerResolver seam is FILLED, not merely declared — the production
// composition root is the only place the pool is reachable, and a plan that
// owned setup.go without subsystem.go could declare the seam and never fill it.
//
// FIXTURE SHAPE IS DELIBERATE: the owning player holds EXACTLY ONE character.
// Under 02-CONTEXT.md D-27's permit-side ALL direction, a two-character player
// would correctly report owner_player_id ABSENT — which would read here as a
// wiring failure rather than as the decided semantics. The two-character case is
// asserted directly in the provider's unit tests.
func TestBuildABACStackResolvesDerivedOwnerPlayerIDThroughTheRealStack(t *testing.T) {
	pool := freshPool(t)
	stack := buildProductionShapeStack(t, pool)
	ctx := context.Background()

	playerID := ulid.Make()
	charID := ulid.Make()
	locID := ulid.Make()
	propID := ulid.Make()

	_, err := pool.Exec(ctx, `
		INSERT INTO players (id, username, password_hash)
		VALUES ($1, $2, 'hash')`, playerID.String(), "owner_"+playerID.String())
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO locations (id, name, description, type, replay_policy)
		VALUES ($1, 'Lobby', '', 'persistent', 'last:0')`, locID.String())
	require.NoError(t, err)
	// Exactly ONE character for this player — see the doc comment above.
	_, err = pool.Exec(ctx, `
		INSERT INTO characters (id, player_id, name, location_id)
		VALUES ($1, $2, $3, $4)`,
		charID.String(), playerID.String(), "Owner_"+charID.String(), locID.String())
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO entity_properties (id, parent_type, parent_id, name, value, owner, visibility)
		VALUES ($1, 'character', $2, 'bio', 'hello', $3, 'private')`,
		propID.String(), charID.String(), charID.String())
	require.NoError(t, err)

	bags, err := stack.Resolver.Resolve(ctx, types.AccessRequest{
		Subject:  access.ViewerSubject(access.ViewerTierPlayer, playerID.String()),
		Action:   "read",
		Resource: access.PropertyResource(propID.String()),
	})
	require.NoError(t, err)

	assert.Equal(t, playerID.String(), bags.Resource["property.owner_player_id"],
		"the derived peer resolves through the stack BuildABACStack returns — the "+
			"CharacterOwnerResolver seam is populated at the production root")
	assert.Equal(t, true, bags.Resource["property.has_owner_player_id"])

	// The character-keyed original is untouched: the peers are ADDITIVE, so the
	// six shipped character-flavored seed:property-* policies still resolve.
	assert.Equal(t, charID.String(), bags.Resource["property.owner"])
	assert.Equal(t, true, bags.Resource["property.has_owner"])

	// And the viewer subject resolves in the SAME request — player id on one
	// side, player id on the other. That is the whole point of the derivation.
	assert.Equal(t, playerID.String(), bags.Subject["viewer.player_id"])
}
