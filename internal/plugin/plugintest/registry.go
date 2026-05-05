// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package plugintest provides test-only helpers for plugin fixtures.
//
// PluginULIDFromName generates a deterministic ULID from a plugin name —
// useful when test assertions depend on a stable plugin actor identity
// across test runs. Fixture-only: production code MUST use idgen.New()
// via repo.Upsert (which generates fresh entropy ULIDs and persists them
// in the plugins table).
package plugintest

import (
	"crypto/sha256"

	"github.com/oklog/ulid/v2"

	plugins "github.com/holomush/holomush/internal/plugin"
)

// PluginULIDFromName returns a stable ULID derived from the plugin name.
// The first 16 bytes of sha256(name) are reinterpreted as a ULID.
//
// Two calls with the same name always return the same ULID. Two calls
// with different names almost always return different ULIDs (sha256
// collision probability is cryptographic).
//
// This is a TEST-ONLY helper. Production plugin identity is generated
// fresh via idgen.New() at first Upsert and persisted in the plugins
// table.
func PluginULIDFromName(name string) ulid.ULID {
	sum := sha256.Sum256([]byte(name))
	var id ulid.ULID
	copy(id[:], sum[:16])
	return id
}

// StubRegistry implements plugins.IdentityRegistry for tests. It maps
// plugin names to deterministic ULIDs via PluginULIDFromName, supporting
// both NameByID and IDByName resolution.
//
// TEST-ONLY: production plugin identity flows through Manager + repo
// (see internal/plugin/manager.go). Use this in fixtures that construct
// a goplugin.Host via WithIdentityRegistry without standing up the full
// Manager.
type StubRegistry struct {
	names map[ulid.ULID]string
	ids   map[string]ulid.ULID
}

// NewStubRegistry constructs a stub registry pre-populated with the
// given plugin names. Each name's ULID is PluginULIDFromName(name).
func NewStubRegistry(names ...string) *StubRegistry {
	r := &StubRegistry{
		names: make(map[ulid.ULID]string, len(names)),
		ids:   make(map[string]ulid.ULID, len(names)),
	}
	for _, n := range names {
		id := PluginULIDFromName(n)
		r.names[id] = n
		r.ids[n] = id
	}
	return r
}

// NameByID returns the name registered for the given ULID.
func (r *StubRegistry) NameByID(id ulid.ULID) (string, bool) {
	name, ok := r.names[id]
	return name, ok
}

// IDByName returns the ULID for the registered plugin name.
func (r *StubRegistry) IDByName(name string) (ulid.ULID, bool) {
	id, ok := r.ids[name]
	return id, ok
}

// Compile-time conformance.
var _ plugins.IdentityRegistry = (*StubRegistry)(nil)
