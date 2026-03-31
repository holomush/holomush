// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package bootstrap

import (
	"context"

	"github.com/holomush/holomush/internal/command"
	plugins "github.com/holomush/holomush/internal/plugin"
)

// AliasBootstrapper wraps system alias seeding as a BootstrapPlugin.
type AliasBootstrapper struct {
	repo  AliasSeeder
	cache *command.AliasCache
}

// Compile-time check.
var _ plugins.BootstrapPlugin = (*AliasBootstrapper)(nil)

// NewAliasBootstrapper creates a new AliasBootstrapper.
func NewAliasBootstrapper(repo AliasSeeder, cache *command.AliasCache) *AliasBootstrapper {
	return &AliasBootstrapper{
		repo:  repo,
		cache: cache,
	}
}

// Priority returns the bootstrap priority for command aliases.
func (b *AliasBootstrapper) Priority() int {
	return plugins.BootstrapPriorityAlias
}

// Bootstrap delegates to SeedSystemAliases.
func (b *AliasBootstrapper) Bootstrap(ctx context.Context, _ *plugins.Manifest, _ string) error {
	return SeedSystemAliases(ctx, b.repo, b.cache)
}
