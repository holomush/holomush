// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"

	"github.com/samber/oops"
)

// UnloadPlugin orderly-unloads a plugin: invokes host.Unload, removes
// installed ABAC policies via PluginPolicyInstaller, and clears the
// plugin from activeByName (the nameByID entry is intentionally retained
// to preserve historical resolution for events emitted before unload).
//
// Idempotent: cache cleanup runs FIRST and unconditionally, so calling
// UnloadPlugin on a name with no host (e.g., a registry-only test
// fixture or after a load-failure rollback) still removes the cache
// entry. Host unload + policy removal then run only if a host is
// actually registered for the name.
func (m *Manager) UnloadPlugin(ctx context.Context, name string) error {
	// 1. Cache cleanup FIRST and unconditionally.
	//
	// The identity deactivation runs BEFORE the m.mu section rather than
	// inside it. Identity state and runtime state now live under separate
	// locks, so the two deletions can no longer be one atomic step, and
	// nesting them would hold both locks at once — the one lock-ordering
	// hazard this extraction exists to avoid. Program order is preserved
	// (identity first, then runtime), as is the unconditional "cleanup
	// first" contract: Deactivate runs whether or not a host is registered.
	m.identity.Deactivate(name)
	// nameByID intentionally retained for historical resolution.

	m.mu.Lock()
	host, hostLoaded := m.pluginHosts[name]
	if hostLoaded {
		delete(m.loaded, name)
		delete(m.pluginHosts, name)
	}
	m.mu.Unlock()

	if !hostLoaded {
		return nil // idempotent — no host to unload
	}

	// 2. Unload from the host.
	if err := host.Unload(ctx, name); err != nil {
		return oops.Code("PLUGIN_UNLOAD_HOST").
			With("plugin", name).Wrap(err)
	}

	// 3. Remove plugin policies.
	if m.policyInstaller != nil {
		if err := m.policyInstaller.RemovePluginPolicies(ctx, name); err != nil {
			return oops.Code("PLUGIN_UNLOAD_POLICIES").
				With("plugin", name).Wrap(err)
		}
	}

	return nil
}
