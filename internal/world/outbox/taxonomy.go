// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package outbox

import (
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/world/wmodel"
)

// AppSchemaVersion is a BUILD-TIME marker of this taxonomy registry's revision.
//
// IT IS NOT ON THE WIRE. Nothing stamps it onto a NATS header, a wmodel.Envelope,
// or an outbox row, and no consumer can read it. The App-Schema-Version HEADER is
// stamped from a DIFFERENT constant — eventbus.SchemaVersion, the proto envelope's
// major version (internal/eventbus/publisher.go:62,304) — and wmodel.Envelope
// carries only the per-kind SchemaVersion below. Bumping this constant therefore
// documents a vocabulary change to a reader of this package; it does not signal
// one to any consumer, and it changes no byte on the wire or on disk.
//
// Bump it whenever the set of declared kinds or any per-type payload schema
// changes. Each declared KindSchema ALSO carries its own SchemaVersion (the
// per-type payload schema version), so a single kind's payload can evolve
// independently of this registry revision. Revision 2 (plan 03-01) adds the
// character lifecycle kinds KindCharacterRetired and KindCharacterUnretired.
// Revision 3 (plan 04-09) adds KindCharacterProfileUpdate, the character
// profile-attribute write. Revision 4 (v0.13 phase-06 plan 05) widens BOTH
// character-payload shapes for the admin write surface: characterLifecyclePayload
// gains before_status plus the evaluated admin section/action, and
// characterProfilePayload gains the same section/action. All three affected kinds
// move to SchemaVersion 2 in the same change — the ratchet is all-or-none.
const AppSchemaVersion = 4

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
	// KindCharacterRetired is the SOFT-retire lifecycle kind (D-31/D-32, plan
	// 03-01). It is NOT a tombstone: the row, its properties and its name
	// reservation all survive (INV-WORLD-6), and the operation is reversible via
	// KindCharacterUnretired. It carries its own kind rather than reusing
	// character_updated because the census bijection is one-producer-of-record
	// and character_updated already belongs to UpdateCharacterDescription.
	KindCharacterRetired = "character_retired"
	// KindCharacterUnretired is the reversal of KindCharacterRetired. It is a
	// SEPARATE kind rather than a second producer of character_retired because
	// the census bijection is one-producer-of-record, and separate kinds are
	// what let a consumer react to the two directions differently.
	KindCharacterUnretired = "character_unretired"
	// KindCharacterProfileUpdate is the character PROFILE-ATTRIBUTE write: the
	// entity_properties rows under the profile.* name prefix (01-SPEC §7.1/§7.2),
	// which are part of the character aggregate but are NOT columns on
	// characters. It carries its own kind for the same reason
	// KindCharacterPreferencesUpdate does — the census bijection is
	// one-producer-of-record and character_updated already belongs to
	// UpdateCharacterDescription.
	KindCharacterProfileUpdate = "character_profile_update"
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
		// SchemaVersion 2 on these three: v0.13 phase-06 plan 05 widened both
		// payload shapes below (before_status, section, action). Reverting any
		// one of the three back to 1 fails a taxonomy test on its own.
		{Kind: KindCharacterRetired, Aggregate: wmodel.AggregateCharacter, SchemaVersion: 2, Payload: characterLifecyclePayload},
		{Kind: KindCharacterUnretired, Aggregate: wmodel.AggregateCharacter, SchemaVersion: 2, Payload: characterLifecyclePayload},
		{Kind: KindCharacterProfileUpdate, Aggregate: wmodel.AggregateCharacter, SchemaVersion: 2, Payload: characterProfilePayload},
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
	// characterLifecyclePayload is the shape both lifecycle kinds declare: the
	// character id, the COMMITTED new status, the status the character LEFT, and
	// the admin section/action the transition was evaluated under (§10.7).
	// characterUpdatePayload is not reusable — it declares a description field
	// these kinds never carry.
	//
	// before_status is the ONE deliberate exception to the registry's
	// new-values-only rule (D-103), and it is exactly as wide as its
	// justification: a lifecycle status is a closed enum the server assigns, not
	// player-authored content, so recording the value it left copies nothing a
	// player wrote into the retained audit trail. The prose half of the same
	// decision goes the other way — see characterProfilePayload.
	//
	// section and action are EMPTY for a player-initiated transition.
	characterLifecyclePayload = []PayloadField{
		{Name: "character_id", Type: "ulid"},
		{Name: "status", Type: "string"},
		{Name: "before_status", Type: "string"},
		{Name: "section", Type: "string"},
		{Name: "action", Type: "string"},
	}
	// characterProfilePayload declares the character id plus the NAMES of the
	// profile attributes the write changed — never their values. Profile prose is
	// player-authored personal content, and the registry rule for every payload is
	// new-values-only AND erasure-safe: a consumer needs to know THAT a profile
	// changed and which fields, and can read the current values through the
	// authorized read path, where the per-attribute tier floor still applies.
	//
	// It is REUSED by the admin profile write rather than split into an
	// admin-flavoured kind: the envelope Actor already records who acted, and one
	// kind for "a character's profile changed" is a better audit model than two.
	// The player path emits EMPTY section and action on it, which is correct.
	// When an admin edits the in-world description alongside the twelve profile
	// fields, "description" appears in changed_attributes as a NAME; its value
	// reaches no field here.
	characterProfilePayload = []PayloadField{
		{Name: "character_id", Type: "ulid"},
		{Name: "changed_attributes", Type: "json"},
		{Name: "section", Type: "string"},
		{Name: "action", Type: "string"},
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
