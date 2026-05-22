// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package setup

import (
	"context"
	"sort"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/test/testutil"
)

// TestBuildABACStack_SeedCoverageMatchesAcknowledged is the DRIFT DETECTOR
// for holomush-xxel: it builds the REAL BuildABACStack against a fresh
// Postgres pool and asserts that the registered providers leave EXACTLY the
// set of namespaces in setup.AcknowledgedMissingSeedNamespaces uncovered —
// no more (a NEW seed gap snuck in), no fewer (a tracked gap was fixed but
// the acknowledgment wasn't removed).
//
// Why this matters: the companion unit test
// (TestValidateSeedProviderCoverage_ProductionCorpusIsCovered) takes a
// hardcoded mirror of "what BuildABACStack registers." If a future refactor
// drops a provider registration from BuildABACStack, the hardcoded mirror
// still claims the namespace is registered and the unit test still passes
// — re-introducing the g776/xxel bug class with no signal. This integration
// test eliminates that drift by introspecting the actual stack.
//
// Per abac-reviewer finding #1 on holomush-xxel.
func TestBuildABACStack_SeedCoverageMatchesAcknowledged(t *testing.T) {
	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	stack, err := BuildABACStack(ctx, ABACConfig{
		Pool:          pool,
		CharacterRepo: worldpostgres.NewCharacterRepository(pool),
		LocationRepo:  worldpostgres.NewLocationRepository(pool),
		ObjectRepo:    worldpostgres.NewObjectRepository(pool),
		// RoleStore intentionally nil; CryptoOperators intentionally empty —
		// production wiring at subsystem.go always passes these too, but the
		// presence/absence does not affect provider registration coverage.
	})
	require.NoError(t, err, "BuildABACStack MUST succeed with the production-shape ABACConfig")
	t.Cleanup(func() { _ = stack.Close() })

	registered := stack.Resolver.RegisteredNamespaces()
	t.Logf("BuildABACStack registered namespaces: %v", registered)

	// Compute which seed-corpus namespaces are NOT covered by the actually-
	// registered providers, then verify it equals the acknowledged set.
	missing := validateSeedProviderCoverage(registered, policy.SeedPolicies())
	actualMissing := sortedKeysOf(missing)

	expectedMissing := make([]string, 0, len(AcknowledgedMissingSeedNamespaces))
	for ns := range AcknowledgedMissingSeedNamespaces {
		expectedMissing = append(expectedMissing, ns)
	}
	sort.Strings(expectedMissing)

	assert.ElementsMatch(t, expectedMissing, actualMissing,
		"DRIFT: BuildABACStack's actual provider registrations + the live seed corpus "+
			"produce a missing-namespace set that does NOT match "+
			"setup.AcknowledgedMissingSeedNamespaces. "+
			"Either (a) a provider was dropped from BuildABACStack — restore it, or "+
			"(b) a new gap appeared — file a follow-up bead and add it to "+
			"AcknowledgedMissingSeedNamespaces, or (c) a tracked gap was fixed — "+
			"remove it from AcknowledgedMissingSeedNamespaces. Per holomush-xxel.")
}

func sortedKeysOf(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
