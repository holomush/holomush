// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package setup

import (
	"bytes"
	"context"
	"log/slog"
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
		Pool:                   pool,
		CharacterRepo:          worldpostgres.NewCharacterRepository(pool),
		LocationRepo:           worldpostgres.NewLocationRepository(pool),
		ObjectRepo:             worldpostgres.NewObjectRepository(pool),
		PropertyRepo:           worldpostgres.NewPropertyRepository(pool),
		ParentLocationResolver: worldpostgres.NewParentLocationResolver(pool),
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

// TestBuildABACStackRegistersTheJobNamespaceInTheProductionResolver asserts the
// job provider's registration on the REAL stack, by name.
//
// This is the production-path half of
// TestJobProviderOwnsTheJobNamespaceInTheResolver (setup_test.go), which can
// only build a bare resolver because BuildABACStack opens a SQL bridge and pings
// it. Between them the claim is complete: the provider owns `job`, and the stack
// production actually builds registers it.
//
// It also asserts the JobRegistry the config carried reached a live provider:
// ABACStack.JobProvider is non-nil, so cmd/holomush's single jobs.Registry has
// somewhere to land.
func TestBuildABACStackRegistersTheJobNamespaceInTheProductionResolver(t *testing.T) {
	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	// JobRegistry intentionally nil: a nil registry is the documented
	// fail-closed shape (02.2-CONTEXT D-49), and REGISTRATION — not liveness —
	// is what this test pins. A provider that only registered when handed a
	// registry would leave `job` unowned on every job-less entrypoint.
	stack, err := BuildABACStack(ctx, ABACConfig{
		Pool:                   pool,
		CharacterRepo:          worldpostgres.NewCharacterRepository(pool),
		LocationRepo:           worldpostgres.NewLocationRepository(pool),
		ObjectRepo:             worldpostgres.NewObjectRepository(pool),
		PropertyRepo:           worldpostgres.NewPropertyRepository(pool),
		ParentLocationResolver: worldpostgres.NewParentLocationResolver(pool),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stack.Close() })

	assert.Contains(t, stack.Resolver.RegisteredNamespaces(), "job",
		"BuildABACStack MUST register the job attribute provider: without it no provider owns "+
			"the `job` namespace and every seed gating on principal.job.* silently default-denies")
	assert.NotNil(t, stack.JobProvider,
		"ABACStack MUST expose the constructed job provider — it is what the injected "+
			"jobs.Registry is read through")
}

func sortedKeysOf(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestBuildABACStackSeedCoverageIsSilentAboutTheViewerNamespace discharges
// 01-SPEC §8.4.1's Phase-2 obligation 1: Phase 2 must CONFIRM the viewer
// namespace is covered, not assume it.
//
// Why it lives in this wave rather than in plan 02-03, which registered nothing,
// or 02-13, which registered ViewerTierProvider: warnOnMissingSeedCoverage scans
// policy.SeedPolicies() only (seed_coverage.go). Before a viewer-REFERENCING
// seed existed there was nothing for it to warn about, so the assertion would
// have passed whether or not the provider was registered — a gate that cannot
// fail. Plan 02-07's seeds are the first to reference the namespace, so this is
// the first wave in which the assertion means anything.
//
// The paired positive control is load-bearing for the same reason: proving no
// WARN names `viewer` proves nothing unless the corpus actually references
// `viewer`, which the control establishes on the same corpus.
func TestBuildABACStackSeedCoverageIsSilentAboutTheViewerNamespace(t *testing.T) {
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	stack, err := BuildABACStack(ctx, ABACConfig{
		Pool:                   pool,
		CharacterRepo:          worldpostgres.NewCharacterRepository(pool),
		LocationRepo:           worldpostgres.NewLocationRepository(pool),
		ObjectRepo:             worldpostgres.NewObjectRepository(pool),
		PropertyRepo:           worldpostgres.NewPropertyRepository(pool),
		ParentLocationResolver: worldpostgres.NewParentLocationResolver(pool),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stack.Close() })

	// Paired positive control, on the SAME corpus: with nothing registered, the
	// validator MUST flag `viewer`. If it does not, the corpus stopped
	// referencing the namespace and the assertion below is vacuous.
	uncovered := validateSeedProviderCoverage(nil, policy.SeedPolicies())
	require.Contains(t, uncovered, "viewer",
		"positive control: the seed corpus MUST reference the viewer namespace, or the no-WARN "+
			"assertion below cannot fail and proves nothing")

	registered := stack.Resolver.RegisteredNamespaces()
	assert.NotContains(t, validateSeedProviderCoverage(registered, policy.SeedPolicies()), "viewer",
		"BuildABACStack MUST register a provider for the `viewer` namespace: without it every "+
			"seed:profile-tier-floor-* and seed:viewer-property-* policy silently default-denies, "+
			"with no error anywhere (§8.4.1 Phase-2 obligation 1)")

	assert.NotContains(t, buf.String(), `namespace=viewer`,
		"no seed-coverage WARN may name the viewer namespace")
}
