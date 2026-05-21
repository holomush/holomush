// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package setup_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/setup"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/test/testutil"
)

// freshABACStack builds a real ABAC stack against a fresh test database,
// optionally seeded with the given crypto operator allow-list. Caller MUST
// invoke the returned cleanup func.
func freshABACStack(t *testing.T, operators []string) (*setup.ABACStack, func()) {
	t.Helper()
	env := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, env)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)

	stack, err := setup.BuildABACStack(ctx, setup.ABACConfig{
		Pool:            pool,
		CharacterRepo:   nil, // optional per setup.go
		// LocationRepo wired so the build does NOT emit the missing-provider
		// WARN (holomush-g776). The PlayerProvider tests don't exercise the
		// location seeds, but the production wiring always supplies the
		// repo — mirror it here to keep test logs clean and to model the
		// production shape that catches future g776-class regressions.
		LocationRepo:    worldpostgres.NewLocationRepository(pool),
		RoleStore:       nil,
		CryptoOperators: operators,
	})
	require.NoError(t, err)
	return stack, func() {
		_ = stack.Close()
		pool.Close()
	}
}

// TestPlayerProviderRegisteredWithResolver verifies that BuildABACStack wires
// the PlayerAttributeProvider so a "player:<ulid>" subject resolves to
// player.id and player.grants attributes via the Resolver.
func TestPlayerProviderRegisteredWithResolver(t *testing.T) {
	operatorID := "01HZAVGE83MGFEXQQH5SP9NXKF"
	stack, cleanup := freshABACStack(t, []string{operatorID})
	defer cleanup()

	bags, err := stack.Resolver.ResolveSubjectAttributes(context.Background(),
		access.PlayerSubject(operatorID), "")
	require.NoError(t, err)
	assert.Equal(t, []string{access.CapabilityCryptoOperator}, bags.Subject["player.grants"])
	assert.Equal(t, operatorID, bags.Subject["player.id"])
}

// TestPlayerProviderEmptyOperators verifies that an empty operator list
// resolves to a non-operator player with empty grants. Satisfies INV-B7.
func TestPlayerProviderEmptyOperators(t *testing.T) {
	stack, cleanup := freshABACStack(t, nil)
	defer cleanup()

	nonOpID := "01HZAVGE83MGFEXQQH5SP9NXKG"
	bags, err := stack.Resolver.ResolveSubjectAttributes(context.Background(),
		access.PlayerSubject(nonOpID), "")
	require.NoError(t, err)
	grants, ok := bags.Subject["player.grants"].([]string)
	require.True(t, ok, "player.grants must be []string, got %T", bags.Subject["player.grants"])
	assert.Empty(t, grants)
}

// TestPlayerProviderNamespaceNonColliding verifies the player namespace
// doesn't collide with any other registered Subject namespace. Satisfies
// INV-B10. If the player namespace clashed with an existing one,
// RegisterProvider would return "already registered" and BuildABACStack
// would fail.
func TestPlayerProviderNamespaceNonColliding(t *testing.T) {
	s1, c1 := freshABACStack(t, nil)
	defer c1()
	require.NotNil(t, s1.Resolver)

	s2, c2 := freshABACStack(t, []string{"01HZAVGE83MGFEXQQH5SP9NXKF"})
	defer c2()
	require.NotNil(t, s2.Resolver)
}
