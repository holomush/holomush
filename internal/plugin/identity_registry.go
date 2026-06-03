// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import "github.com/oklog/ulid/v2"

// IdentityRegistry resolves between a plugin's stable ULID and its
// registered name. Both lookups are O(1) in-memory map accesses backed by
// the plugins table.
//
// Consumers (plugin emit stamp sites in internal/plugin/goplugin/host.go,
// actor display in internal/grpc/server.go::actorIDString and
// internal/grpc/query_stream_history.go::eventbusEventToEventFrame)
// depend on this interface, not on the full Manager.
//
// The ABAC engine is NOT an IdentityRegistry consumer (Subject strings
// are constructed at call sites by code that already has the plugin name).
type IdentityRegistry interface {
	// NameByID returns the name registered for the given ULID. Resolves
	// THREE populations:
	//   1. Currently-active plugins (rows with gc_at IS NULL).
	//   2. Historically-registered plugins (rows with gc_at IS NOT NULL —
	//      preserved across the registry's lifetime per INV-PLUGIN-17).
	//   3. Compile-time system actor sentinels registered at Manager
	//      bootstrap (e.g., SystemActorULID -> "system",
	//      WorldServiceActorULID -> "world-service"). Sentinels are NOT
	//      subject to GC sweep.
	//
	// ok=false only if the ULID has never been minted/registered.
	NameByID(id ulid.ULID) (name string, ok bool)

	// IDByName returns the ULID for the currently-active plugin with
	// the given name. Does NOT resolve to historical (deactivated) ULIDs;
	// emit stamp sites only care about live registrations. Does NOT
	// resolve system sentinel labels (system stamp sites use the
	// compile-time constants directly).
	//
	// ok=false if no currently-active plugin with that name is registered.
	IDByName(name string) (id ulid.ULID, ok bool)
}
