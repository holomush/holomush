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
func (l *PluginLoader) UnloadPlugin(ctx context.Context, name string) error {
	// 1. Cache cleanup FIRST and unconditionally.
	//
	// The identity deactivation runs BEFORE the runtime removal rather than
	// sharing one critical section with it. Identity state and runtime state
	// live under separate locks, so the two deletions can no longer be one
	// atomic step, and nesting them would hold both locks at once — the one
	// lock-ordering hazard this extraction exists to avoid. Program order is
	// preserved (identity first, then runtime), as is the unconditional
	// "cleanup first" contract: Deactivate runs whether or not a host is
	// registered.
	l.identity.Deactivate(name)
	// nameByID intentionally retained for historical resolution.

	// The runtime deletes live in PluginRuntime, which owns its own lock. This
	// site takes neither the loader's lock nor the identity lock, so the "no
	// path holds two unit locks" rule still holds with a third lock in play.
	// Program order (identity, then runtime) and the unconditional
	// cleanup-first contract are unchanged.
	host, hostLoaded := l.runtime.RemoveLoaded(name)

	if !hostLoaded {
		return nil // idempotent — no host to unload
	}

	// 2. Unload from the host.
	if err := host.Unload(ctx, name); err != nil {
		return oops.Code("PLUGIN_UNLOAD_HOST").
			With("plugin", name).Wrap(err)
	}

	// 3. Remove plugin policies.
	if l.policyInstaller != nil {
		if err := l.policyInstaller.RemovePluginPolicies(ctx, name); err != nil {
			return oops.Code("PLUGIN_UNLOAD_POLICIES").
				With("plugin", name).Wrap(err)
		}
	}

	return nil
}

// UnloadPlugin orderly-unloads a plugin: invokes host.Unload, removes
// installed ABAC policies via PluginPolicyInstaller, and clears the
// plugin from activeByName (the nameByID entry is intentionally retained
// to preserve historical resolution for events emitted before unload).
//
// Idempotent: cache cleanup runs FIRST and unconditionally.
func (m *Manager) UnloadPlugin(ctx context.Context, name string) error {
	return m.loader.UnloadPlugin(ctx, name)
}
