// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package outbox

import (
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/world/wmodel"
)

// AppSchemaVersion is the versioned taxonomy schema registry's global
// App-Schema-Version — the ARCH-04 / Phase-7 input. It stamps the taxonomy
// REVISION that produced a world-change feed row; a consumer reads it to know
// which taxonomy shape a payload was encoded against. Bump it whenever the set of
// declared kinds or any per-type payload schema changes. Each declared KindSchema
// ALSO carries its own SchemaVersion (the per-type payload schema version), so a
// single kind's payload can evolve independently of the registry revision.
const AppSchemaVersion = 1

// The declared world-change envelope kinds. These are the taxonomy VOCABULARY the
// mechanical emission rollout (05-10/05-11) wires each world write command to; the
// census meta-test (05-11) asserts a bijection between the write commands and this
// set. State-change kinds ONLY — an examine is a read and is intentionally absent
// (RESEARCH Open Question 1). No scene-participant kind is declared: the vestigial
// world scene-participant write surface is removed in 05-14 (D-07), so there is no
// command to map — this resolves the D-01<->D-05 contradiction by removal.
const (
	// Location aggregate (locations don't move).
	KindLocationCreated = "location_created"
	KindLocationUpdated = "location_updated"
	KindLocationDeleted = "location_deleted"

	// Exit aggregate (exits don't move).
	KindExitCreated = "exit_created"
	KindExitUpdated = "exit_updated"
	KindExitDeleted = "exit_deleted"

	// Object aggregate.
	KindObjectCreated = "object_created"
	KindObjectUpdated = "object_updated"
	KindObjectDeleted = "object_deleted"
	KindObjectMoved   = "object_moved"

	// Character aggregate. KindCharacterGenesis is the character CREATE kind (Open
	// Question 3); its sole emitting site is the atomic character-genesis service
	// (05-15) covering all three production creation paths (registered gRPC, guest,
	// bootstrap-admin). KindCharacterDeleted is the single tombstone kind REUSED by
	// world.Service.DeleteCharacter (05-11) and the guest reaper's character-aware
	// deletion (05-16, D-06). KindCharacterPreferencesUpdate is the folded-in
	// character-settings write (round-4 C5 / D-05, Task 2).
	KindCharacterGenesis           = "character_genesis"
	KindCharacterUpdated           = "character_updated"
	KindCharacterDeleted           = "character_deleted"
	KindCharacterMoved             = "character_moved"
	KindCharacterPreferencesUpdate = "character_preferences_update"
)

// PayloadField describes one field of a kind's intent-level, new-values-only
// payload: its JSON key, a machine/human type tag, and whether it is optional.
// The registry is self-describing so a downstream consumer (ARCH-04) can validate
// a payload against its declared shape without importing the producing package.
type PayloadField struct {
	// Name is the JSON key of the payload field.
	Name string
	// Type is a type tag (e.g. "ulid", "string", "json") describing the field's
	// wire shape.
	Type string
	// Optional marks a field that MAY be absent (e.g. a from-location on a genesis).
	Optional bool
}

// KindSchema is the declared contract for one world-change envelope kind: the
// aggregate it targets, its per-type new-values-only payload schema, its schema
// version (the App-Schema-Version each declared kind carries), and whether it is a
// delete tombstone.
type KindSchema struct {
	// Kind is the taxonomy kind string (e.g. "character_moved").
	Kind string
	// Aggregate is the world aggregate the kind changes.
	Aggregate wmodel.AggregateType
	// SchemaVersion is the per-type payload schema version (the kind's
	// App-Schema-Version). Starts at 1; bump when a kind's payload changes shape.
	SchemaVersion int
	// Tombstone marks a delete kind (one tombstone per aggregate on delete).
	Tombstone bool
	// Payload is the declared, new-values-only payload schema for the kind.
	Payload []PayloadField
}

// registry is the versioned taxonomy schema registry: kind string -> declared
// contract. It is the single source of truth the rollout and census read against.
var registry = func() map[string]KindSchema {
	entries := []KindSchema{
		// Locations.
		{Kind: KindLocationCreated, Aggregate: wmodel.AggregateLocation, SchemaVersion: 1, Payload: locationPayload},
		{Kind: KindLocationUpdated, Aggregate: wmodel.AggregateLocation, SchemaVersion: 1, Payload: locationPayload},
		{Kind: KindLocationDeleted, Aggregate: wmodel.AggregateLocation, SchemaVersion: 1, Tombstone: true, Payload: tombstonePayload},
		// Exits.
		{Kind: KindExitCreated, Aggregate: wmodel.AggregateExit, SchemaVersion: 1, Payload: exitPayload},
		{Kind: KindExitUpdated, Aggregate: wmodel.AggregateExit, SchemaVersion: 1, Payload: exitPayload},
		{Kind: KindExitDeleted, Aggregate: wmodel.AggregateExit, SchemaVersion: 1, Tombstone: true, Payload: tombstonePayload},
		// Objects.
		{Kind: KindObjectCreated, Aggregate: wmodel.AggregateObject, SchemaVersion: 1, Payload: objectPayload},
		{Kind: KindObjectUpdated, Aggregate: wmodel.AggregateObject, SchemaVersion: 1, Payload: objectPayload},
		{Kind: KindObjectDeleted, Aggregate: wmodel.AggregateObject, SchemaVersion: 1, Tombstone: true, Payload: tombstonePayload},
		{Kind: KindObjectMoved, Aggregate: wmodel.AggregateObject, SchemaVersion: 1, Payload: movePayload},
		// Characters.
		{Kind: KindCharacterGenesis, Aggregate: wmodel.AggregateCharacter, SchemaVersion: 1, Payload: characterGenesisPayload},
		{Kind: KindCharacterUpdated, Aggregate: wmodel.AggregateCharacter, SchemaVersion: 1, Payload: characterUpdatePayload},
		{Kind: KindCharacterDeleted, Aggregate: wmodel.AggregateCharacter, SchemaVersion: 1, Tombstone: true, Payload: tombstonePayload},
		{Kind: KindCharacterMoved, Aggregate: wmodel.AggregateCharacter, SchemaVersion: 1, Payload: movePayload},
		{Kind: KindCharacterPreferencesUpdate, Aggregate: wmodel.AggregateCharacter, SchemaVersion: 1, Payload: characterPreferencesPayload},
	}
	m := make(map[string]KindSchema, len(entries))
	for _, e := range entries {
		m[e.Kind] = e
	}
	return m
}()

// Per-type payload schemas (new-values-only, erasure-safe; no secrets). These
// declare the SHAPE the rollout constructs against; the exact bytes are built at
// each command site.
var (
	tombstonePayload = []PayloadField{
		{Name: "id", Type: "ulid"},
	}
	locationPayload = []PayloadField{
		{Name: "id", Type: "ulid"},
		{Name: "name", Type: "string"},
		{Name: "description", Type: "string"},
	}
	exitPayload = []PayloadField{
		{Name: "id", Type: "ulid"},
		{Name: "name", Type: "string"},
		{Name: "from_location_id", Type: "ulid"},
		{Name: "to_location_id", Type: "ulid"},
	}
	objectPayload = []PayloadField{
		{Name: "id", Type: "ulid"},
		{Name: "name", Type: "string"},
		{Name: "description", Type: "string"},
	}
	movePayload = []PayloadField{
		{Name: "character_id", Type: "ulid"},
		{Name: "to_location_id", Type: "ulid"},
		{Name: "from_location_id", Type: "ulid", Optional: true},
	}
	characterGenesisPayload = []PayloadField{
		{Name: "character_id", Type: "ulid"},
		{Name: "player_id", Type: "ulid"},
		{Name: "name", Type: "string"},
		{Name: "location_id", Type: "ulid", Optional: true},
	}
	characterUpdatePayload = []PayloadField{
		{Name: "character_id", Type: "ulid"},
		{Name: "description", Type: "string"},
	}
	characterPreferencesPayload = []PayloadField{
		{Name: "character_id", Type: "ulid"},
		{Name: "preferences", Type: "json"},
	}
)

// Lookup returns the declared schema for a world-change kind, or an error coded
// WORLD_TAXONOMY_UNKNOWN_KIND for an undeclared kind. An undeclared kind is
// REJECTED, never silently accepted — the enforcement the census (05-11) relies
// on so an unregistered kind cannot leak onto the feed.
func Lookup(kind string) (KindSchema, error) {
	schema, ok := registry[kind]
	if !ok {
		return KindSchema{}, oops.Code("WORLD_TAXONOMY_UNKNOWN_KIND").
			With("kind", kind).
			Errorf("undeclared world-change kind %q", kind)
	}
	return schema, nil
}

// IsDeclared reports whether kind is a declared world-change kind.
func IsDeclared(kind string) bool {
	_, ok := registry[kind]
	return ok
}

// Kinds returns every declared world-change kind. Order is unspecified.
func Kinds() []string {
	kinds := make([]string, 0, len(registry))
	for kind := range registry {
		kinds = append(kinds, kind)
	}
	return kinds
}
