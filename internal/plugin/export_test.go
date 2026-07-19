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
	m.mu.Lock()
	host, ok := m.hosts[manifest.Type]
	if !ok && manifest.Type == TypeLua && m.luaHost != nil {
		host, ok = m.luaHost, true
	}
	m.mu.Unlock()

	if ok {
		if err := host.Load(context.Background(), manifest, ""); err != nil {
			panic("TestLoadPlugin: host.Load failed: " + err.Error())
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.loaded[name] = &DiscoveredPlugin{Manifest: manifest}
	if ok {
		m.pluginHosts[name] = host
	}
}
