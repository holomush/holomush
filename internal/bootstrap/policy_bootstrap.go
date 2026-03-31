// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package bootstrap

import (
	"context"

	plugins "github.com/holomush/holomush/internal/plugin"
)

// PolicyBootstrapper wraps a policy bootstrap function as a BootstrapPlugin.
type PolicyBootstrapper struct {
	bootstrapFn        func(ctx context.Context, skipSeedMigrations bool) error
	skipSeedMigrations bool
}

// Compile-time check.
var _ plugins.BootstrapPlugin = (*PolicyBootstrapper)(nil)

// NewPolicyBootstrapper creates a new PolicyBootstrapper.
func NewPolicyBootstrapper(fn func(ctx context.Context, skipSeedMigrations bool) error, skipSeedMigrations bool) *PolicyBootstrapper {
	return &PolicyBootstrapper{
		bootstrapFn:        fn,
		skipSeedMigrations: skipSeedMigrations,
	}
}

// Priority returns the bootstrap priority for ABAC policies and access control seeds.
func (b *PolicyBootstrapper) Priority() int {
	return plugins.BootstrapPriorityPolicy
}

// Bootstrap delegates to the wrapped policy bootstrap function.
func (b *PolicyBootstrapper) Bootstrap(ctx context.Context, _ *plugins.Manifest, _ string) error {
	return b.bootstrapFn(ctx, b.skipSeedMigrations)
}
