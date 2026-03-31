// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"log/slog"
	"sort"

	"github.com/samber/oops"
)

// BootstrapPlugin is implemented by plugins that run once during server startup.
// They seed data, create initial state, or perform one-time setup.
// They are NOT runtime plugins — no commands, events, or lifecycle.
type BootstrapPlugin interface {
	// Priority determines execution order. Lower values run first.
	Priority() int

	// Bootstrap performs the one-time initialization.
	// manifest and pluginDir may be nil/empty for non-manifest bootstrappers.
	Bootstrap(ctx context.Context, manifest *Manifest, pluginDir string) error
}

// Bootstrap priority levels. Plugins at the same priority run in
// discovery order (lexicographic by plugin name).
const (
	BootstrapPrioritySchema  = 100 // DB migrations, schema changes
	BootstrapPriorityPolicy  = 200 // ABAC policies, access control seeds
	BootstrapPriorityWorld   = 300 // Locations, exits, world state (setting plugins)
	BootstrapPriorityContent = 400 // Content store items, themes, admin seed
	BootstrapPriorityAlias   = 500 // Command aliases (needs registry populated)
)

// BootstrapRunner collects and runs bootstrap plugins in priority order.
type BootstrapRunner struct {
	bootstrappers []BootstrapPlugin
	logger        *slog.Logger
}

// NewBootstrapRunner creates a BootstrapRunner with the given logger.
func NewBootstrapRunner(logger *slog.Logger) *BootstrapRunner {
	return &BootstrapRunner{logger: logger}
}

// Register adds a bootstrap plugin to the runner.
func (r *BootstrapRunner) Register(p BootstrapPlugin) {
	r.bootstrappers = append(r.bootstrappers, p)
}

// RunAll executes all registered bootstrap plugins in priority order.
// Plugins at equal priority run in registration order (stable sort).
// The first error is returned immediately; remaining plugins are not run.
func (r *BootstrapRunner) RunAll(ctx context.Context) error {
	sorted := make([]BootstrapPlugin, len(r.bootstrappers))
	copy(sorted, r.bootstrappers)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Priority() < sorted[j].Priority()
	})

	for _, p := range sorted {
		r.logger.InfoContext(ctx, "running bootstrap plugin", "priority", p.Priority())
		if err := p.Bootstrap(ctx, nil, ""); err != nil {
			r.logger.ErrorContext(ctx, "bootstrap plugin failed", "priority", p.Priority(), "error", err)
			return oops.With("priority", p.Priority()).Wrap(err)
		}
		r.logger.InfoContext(ctx, "bootstrap plugin completed", "priority", p.Priority())
	}
	return nil
}
