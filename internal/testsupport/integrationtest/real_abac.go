//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package integrationtest

import (
	"context"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	policystore "github.com/holomush/holomush/internal/access/policy/store"
	abacsetup "github.com/holomush/holomush/internal/access/setup"
	"github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
)

// poolProvider adapts a *pgxpool.Pool to abacsetup.PoolProvider so the harness
// can hand the test pool to the production ABAC subsystem.
type poolProvider struct{ pool *pgxpool.Pool }

func (p poolProvider) Pool() *pgxpool.Pool { return p.pool }

// startRealABAC seeds the production seed:* policy set and boots the real ABAC
// subsystem (production's abacsetup.NewABACSubsystem path, the same constructor
// cmd/holomush/core.go:380 uses). It returns the started subsystem; callers read
// Engine()/AttributeResolver()/PluginProvider()/AuditLogger() and the poller is
// stopped via t.Cleanup.
//
// jobRegistry is the harness's single background-job liveness registry, passed
// through verbatim exactly as cmd/holomush/core.go passes its one instance. It
// is what lets a job subsystem booted by another option (WithRetirementReactor)
// be seen as RUNNING by this engine's job attribute provider — without it every
// job-gating seed default-denies, which would make a denial spec pass for the
// wrong reason and its paired positive control fail.
func startRealABAC(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	jobRegistry attribute.JobRegistry,
) *abacsetup.ABACSubsystem {
	t.Helper()

	// Seed first: the subsystem's Start → BuildABACStack → cache.Reload reads
	// the policy store at construction. An unseeded store has zero policies and
	// default-denies everything.
	//
	// Because seeding runs BEFORE abacSub.Prepare/Activate, no populated provider
	// registry exists at this point BY CONSTRUCTION — the registry the providers
	// fill is created inside BuildABACStack, which has not run yet. So this
	// compiler is built on an action-only registry (02.2-04, D-66 site 3), which
	// is what keeps the harness's validation behaviour identical to production's
	// for `action`: the compiler validates by DSL ROOT, never by provider name, so
	// the roots this registry does not carry are skipped and only the `action`
	// hard-error branch differs from a bare schema — which is exactly the branch
	// the harness must not silently disarm.
	require.NoError(
		t,
		policy.Bootstrap(
			ctx,
			audit.NewPostgresPartitionCreator(pool),
			policystore.NewPostgresStore(pool),
			policy.NewCompiler(attribute.NewActionOnlySchemaRegistry().Schema()),
			slog.Default(),
			policy.BootstrapOptions{},
		),
		"startRealABAC: seed policies",
	)

	abacSub := abacsetup.NewABACSubsystem(abacsetup.ABACSubsystemConfig{
		DB:          poolProvider{pool: pool},
		Registry:    lifecycle.NewReadinessRegistry(),
		JobRegistry: jobRegistry,
	})
	require.NoError(t, abacSub.Prepare(ctx), "startRealABAC: ABAC subsystem prepare")
	require.NoError(t, abacSub.Activate(ctx), "startRealABAC: ABAC subsystem activate")
	t.Cleanup(func() { _ = abacSub.Stop(context.Background()) })
	return abacSub
}

// pluginAttrSources returns the attribute resolver, plugin provider, and auditor
// the plugin subsystem should register against. With a real ABAC subsystem, these
// are the subsystem's OWN instances so plugin-declared providers (e.g. core-scenes'
// "scene" namespace) register on the resolver the engine evaluates against
// (INV-ACCESS-4). With no real engine (allow-all default), fresh standalone instances
// are correct — allow-all ignores attributes, so the #4275 behavior is preserved.
func pluginAttrSources(abacSub *abacsetup.ABACSubsystem) (*attribute.Resolver, *attribute.PluginProvider, pluginauthz.Auditor) {
	if abacSub != nil {
		return abacSub.AttributeResolver(), abacSub.PluginProvider(), abacSub.AuditLogger()
	}
	return attribute.NewResolver(attribute.NewSchemaRegistry()), attribute.NewPluginProvider(nil), nil
}
