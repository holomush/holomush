// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import "context"

// This file holds test-only seams on production types. Living in a _test.go
// file keeps them out of the production binary while remaining visible to both
// the `plugins` and `plugins_test` test binaries (D-08).

// TestLoadPlugin injects a plugin directly for unit testing.
//
// Relocated here from manager.go by phase 08 plan 02: it previously shipped
// untagged in the production binary. Behavior is unchanged — note in
// particular that with no host registered for the manifest's type and no
// luaHost fallback, it inserts into m.loaded but NOT m.pluginHosts. Existing
// tests rely on that path.
func (m *Manager) TestLoadPlugin(name string, manifest *Manifest) {
	m.loader.mu.Lock()
	host, ok := m.loader.hosts[manifest.Type]
	if !ok && manifest.Type == TypeLua && m.loader.luaHost != nil {
		host, ok = m.loader.luaHost, true
	}
	m.loader.mu.Unlock()

	if ok {
		if err := host.Load(context.Background(), manifest, ""); err != nil {
			panic("TestLoadPlugin: host.Load failed: " + err.Error())
		}
	}

	// The loaded/pluginHosts write moved to PluginRuntime (phase 08 plan 06).
	// It runs OUTSIDE m.mu: no path may hold Manager.mu and the runtime lock at
	// once.
	//
	// This does NOT go through CommitLoaded, deliberately. CommitLoaded keys the
	// maps off dp.Manifest.Name, but TestLoadPlugin keys them off its `name`
	// PARAMETER, which callers may pass independently of the manifest's own
	// name. Routing through CommitLoaded would silently change the key whenever
	// the two differ. Every caller happens to pass a matching pair today, so no
	// test would have caught the drift — hence the dedicated seam.
	var committedHost Host
	if ok {
		committedHost = host
	}
	m.runtime.testCommitNamed(name, &DiscoveredPlugin{Manifest: manifest}, committedHost)
}

// testCommitNamed writes a loaded/pluginHosts pair under an EXPLICIT key,
// preserving TestLoadPlugin's parameter semantics. Production code has no such
// operation: loadPlugin always keys off the manifest name.
func (r *PluginRuntime) testCommitNamed(name string, dp *DiscoveredPlugin, host Host) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loaded[name] = dp
	if host != nil {
		r.pluginHosts[name] = host
	}
}

// TestHasHost reports whether a pluginHosts entry exists for name. The map is
// unexported and has no production read-only accessor; UnloadPlugin's tests
// assert its cleanup directly.
func (r *PluginRuntime) TestHasHost(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.pluginHosts[name]
	return ok
}

// TestLookupManifest exposes PluginRuntime's unexported manifest lookup so
// package plugins_test can assert its loaded-then-inflight fallback order
// directly, rather than inferring it through a crypto gate.
func (r *PluginRuntime) TestLookupManifest(name string) *Manifest {
	return r.lookupManifest(name)
}

// TestResolveLoadOrder exposes the loader's unexported dependency-ordering
// entry point so package plugins_test can assert determinism without driving
// LoadAll.
func (l *PluginLoader) TestResolveLoadOrder(discovered []*DiscoveredPlugin) (*ResolveResult, error) {
	return l.resolveLoadOrder(discovered)
}

// TestComputeHashes exposes the loader's unexported artifact hashing so package
// plugins_test can pin its output for a fixed input.
func (l *PluginLoader) TestComputeHashes(dp *DiscoveredPlugin) (manifestHash, contentHash []byte, err error) {
	return l.computeHashes(dp)
}
