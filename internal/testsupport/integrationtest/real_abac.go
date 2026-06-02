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
	policytypes "github.com/holomush/holomush/internal/access/policy/types"
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
func startRealABAC(t *testing.T, ctx context.Context, pool *pgxpool.Pool) *abacsetup.ABACSubsystem {
	t.Helper()

	// Seed first: the subsystem's Start → BuildABACStack → cache.Reload reads
	// the policy store at construction. An unseeded store has zero policies and
	// default-denies everything.
	require.NoError(
		t,
		policy.Bootstrap(
			ctx,
			audit.NewPostgresPartitionCreator(pool),
			policystore.NewPostgresStore(pool),
			policy.NewCompiler(policytypes.NewAttributeSchema()),
			slog.Default(),
			policy.BootstrapOptions{},
		),
		"startRealABAC: seed policies",
	)

	abacSub := abacsetup.NewABACSubsystem(abacsetup.ABACSubsystemConfig{
		DB:       poolProvider{pool: pool},
		Registry: lifecycle.NewReadinessRegistry(),
	})
	require.NoError(t, abacSub.Start(ctx), "startRealABAC: ABAC subsystem start")
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
