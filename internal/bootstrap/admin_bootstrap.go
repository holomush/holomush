// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package bootstrap

import (
	"context"

	plugins "github.com/holomush/holomush/internal/plugin"
)

// AdminBootstrapper wraps admin seeding as a BootstrapPlugin.
type AdminBootstrapper struct {
	deps SeedAdminDeps
}

// Compile-time check.
var _ plugins.BootstrapPlugin = (*AdminBootstrapper)(nil)

// NewAdminBootstrapper creates a new AdminBootstrapper.
func NewAdminBootstrapper(deps SeedAdminDeps) *AdminBootstrapper {
	return &AdminBootstrapper{
		deps: deps,
	}
}

// Priority returns the bootstrap priority for content initialization (admin seed).
func (b *AdminBootstrapper) Priority() int {
	return plugins.BootstrapPriorityContent
}

// Bootstrap delegates to SeedAdmin.
func (b *AdminBootstrapper) Bootstrap(ctx context.Context, _ *plugins.Manifest, _ string) error {
	return SeedAdmin(ctx, b.deps)
}
