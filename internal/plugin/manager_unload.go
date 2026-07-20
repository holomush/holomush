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
//
// # Concurrency contract — unload is NOT atomic across identity and runtime
//
// Identity state and runtime state live under separate locks, so their two
// deletions cannot be one atomic step. Callers MUST NOT assume the two agree
// during an unload. Concretely, a concurrent reader can observe the window:
//
//	IDByName(name)        // false — identity already deactivated
//	IsPluginLoaded(name)  // true  — runtime entry not yet removed
//
// The reverse ordering never occurs: program order is identity-then-runtime.
// This window is accepted, not incidental — closing it means holding both unit
// locks at once, which is the lock-ordering hazard the loader/runtime/identity
// split exists to remove. Pre-split, both deletions shared one critical
// section and this window did not exist; widening it is the deliberate cost of
// that split (phase-08 threat T-8-05).
//
// A caller needing a consistent view MUST derive it from a single unit — query
// the runtime alone, or the identity store alone — rather than correlating the
// two across an unload.
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
//
// Unload is NOT atomic across identity and runtime state — see
// (*PluginLoader).UnloadPlugin for the concurrency contract callers must
// respect.
func (m *Manager) UnloadPlugin(ctx context.Context, name string) error {
	return m.loader.UnloadPlugin(ctx, name)
}
