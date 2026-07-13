// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"context"
	"errors"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/world/wmodel"
)

// Mutator extends the read operations with write operations.
// This interface represents the full set of authorized world operations
// available to plugins that need to modify world state.
//
// All methods accept a subjectID parameter for ABAC authorization.
// Plugins use "plugin:<name>" as their subject ID (via access.PluginSubject).
type Mutator interface {
	// Read operations (from Service)

	// GetLocation retrieves a location by ID after checking read authorization.
	GetLocation(ctx context.Context, subjectID string, id ulid.ULID) (*Location, error)

	// GetCharacter retrieves a character by ID after checking read authorization.
	GetCharacter(ctx context.Context, subjectID string, id ulid.ULID) (*Character, error)

	// GetCharactersByLocation retrieves characters at a location with pagination
	// after checking read authorization.
	GetCharactersByLocation(ctx context.Context, subjectID string, locationID ulid.ULID, opts ListOptions) ([]*Character, error)

	// GetObject retrieves an object by ID after checking read authorization.
	GetObject(ctx context.Context, subjectID string, id ulid.ULID) (*Object, error)

	// Write operations

	// CreateLocation creates a new location after checking write authorization.
	CreateLocation(ctx context.Context, subjectID string, loc *Location) error

	// CreateExit creates a new exit between locations after checking write authorization.
	CreateExit(ctx context.Context, subjectID string, exit *Exit) error

	// CreateObject creates a new object with the given containment after checking write authorization.
	CreateObject(ctx context.Context, subjectID string, obj *Object) error

	// UpdateLocation updates an existing location after checking write authorization.
	UpdateLocation(ctx context.Context, subjectID string, loc *Location) error

	// UpdateObject updates an existing object after checking write authorization.
	UpdateObject(ctx context.Context, subjectID string, obj *Object) error

	// FindLocationByName searches for a location by name after checking read authorization.
	// Returns ErrNotFound if no location matches.
	FindLocationByName(ctx context.Context, subjectID, name string) (*Location, error)
}

// Compile-time check that Service implements Mutator.
var _ Mutator = (*Service)(nil)

// WriteCommandDescriptor names one world.Service write command AND the single
// taxonomy kind its envelope declares. It is one member of the EXPLICIT closed
// write-command construct the census (05-11) derives the write-command surface
// from — NOT name-prefix inference over method declarations, and NOT the
// incomplete world.Mutator interface subset (Codex finding 10).
type WriteCommandDescriptor struct {
	// Command is the stable world.Service write-command method name.
	Command string
	// Kind is the single taxonomy kind the command's envelope declares. Each Kind
	// string mirrors internal/world/outbox/taxonomy.go exactly (package world MUST
	// NOT import internal/world/outbox); the census asserts the bijection across
	// that boundary.
	Kind string
}

// writeCommands is the EXPLICIT, closed set of world.Service write commands and
// the single taxonomy kind each emits. Membership is mechanically discoverable
// here (not inferred): a new Service write command that isn't registered here has
// no declared kind, so the census fails. No scene-participant descriptor exists —
// the vestigial surface was removed in 05-14 (D-07). Character CREATE
// (character_genesis) is NOT here: its producer is the out-of-Service
// character-genesis application service (05-15), asserted in 05-12.
var writeCommands = []WriteCommandDescriptor{
	{Command: "CreateLocation", Kind: kindLocationCreated},
	{Command: "UpdateLocation", Kind: kindLocationUpdated},
	{Command: "DeleteLocation", Kind: kindLocationDeleted},
	{Command: "CreateExit", Kind: kindExitCreated},
	{Command: "UpdateExit", Kind: kindExitUpdated},
	{Command: "DeleteExit", Kind: kindExitDeleted},
	{Command: "CreateObject", Kind: kindObjectCreated},
	{Command: "UpdateObject", Kind: kindObjectUpdated},
	{Command: "DeleteObject", Kind: kindObjectDeleted},
	{Command: "MoveObject", Kind: kindObjectMoved},
	{Command: "DeleteCharacter", Kind: kindCharacterDeleted},
	{Command: "UpdateCharacterDescription", Kind: kindCharacterUpdated},
	{Command: "MoveCharacter", Kind: kindCharacterMoved},
	{Command: "UpdateCharacterPreferences", Kind: kindCharacterPreferencesUpdate},
}

// WriteCommands returns the explicit closed write-command descriptor set (a copy),
// the in-Service half of the D-01 rollout the census (05-11) asserts a bijection
// over. Each per-operation executor method and each command site keys off this
// set; a registered command without a declared kind — or a kind with no registered
// in-Service producer — fails the census.
func WriteCommands() []WriteCommandDescriptor {
	out := make([]WriteCommandDescriptor, len(writeCommands))
	copy(out, writeCommands)
	return out
}

// OutboxWriter persists a finalized world-change envelope inside the caller-owned
// mutation transaction and returns it. Its sole production implementation is the
// internal/world/postgres.OutboxStore (05-05), INJECTED via ServiceConfig — so
// package world imports NEITHER internal/world/outbox NOR internal/world/postgres,
// dissolving the round-1 world -> outbox -> world import cycle (round-2 Codex).
//
// The writer owns the storage-stamped envelope fields (round-3 blocker #1): its
// WriteIntent allocates (epoch, feed_position) from the locked per-game counter,
// finalizes the envelope via the pure wmodel.Finalize, persists the row, and
// returns the finalized envelope. The executor never finalizes and never stamps
// epoch/feed_position.
type OutboxWriter interface {
	// WriteIntent allocates (epoch, feed_position), finalizes the envelope from the
	// returned MutationDelta, persists exactly one outbox row through the ambient
	// transaction, and returns the finalized envelope.
	WriteIntent(ctx context.Context, intent wmodel.EnvelopeIntent, delta *wmodel.MutationDelta) (*wmodel.Envelope, error)
}

// worldMutator is the world write executor and the home of the compile-time
// write-requires-envelope seam. It OWNS the write-capable repositories as PRIVATE
// fields (reachable only through the executor's per-operation closure-builder
// methods), the transactor, and the injected OutboxWriter. A caller outside
// package world cannot construct a write closure that reaches a writer repo, so a
// state write cannot happen without the accompanying envelope.
//
// As of 05-10 the character writer plus the location/exit/property writers are
// owned here (MoveCharacter was the first command through the seam; the
// location/exit/object write commands migrate in 05-10/05-11). The object writer
// is added in 05-10 Task 2. The genuine compile-time fence (Service holds only
// reader views, no directly-callable write repo) closes in 05-11 once every
// command routes through mutate() — closing it here would break compilation of the
// un-migrated commands.
type worldMutator struct {
	characterWriter CharacterRepository
	locationWriter  LocationRepository
	exitWriter      ExitRepository
	objectWriter    ObjectRepository
	propertyWriter  PropertyRepository
	transactor      Transactor
	outbox          OutboxWriter
}

// newWorldMutator constructs the write executor. The *Writer args are the private
// write-capable repositories (reachable only through the per-operation
// closure-builder methods); transactor is the re-entrant transaction seam (05-14);
// outbox is the injected OutboxWriter the executor persists through. propertyWriter
// is used by the delete closures for the same-tx property cascade.
func newWorldMutator(
	characterWriter CharacterRepository,
	locationWriter LocationRepository,
	exitWriter ExitRepository,
	objectWriter ObjectRepository,
	propertyWriter PropertyRepository,
	transactor Transactor,
	outbox OutboxWriter,
) *worldMutator {
	return &worldMutator{
		characterWriter: characterWriter,
		locationWriter:  locationWriter,
		exitWriter:      exitWriter,
		objectWriter:    objectWriter,
		propertyWriter:  propertyWriter,
		transactor:      transactor,
		outbox:          outbox,
	}
}

// mutate is the write-requires-envelope seam. BOTH parameters are non-optional —
// an intent-less OR closure-less call does not type-check. In ONE re-entrant
// Transactor.InTransaction it runs, in order:
//
//  1. delta := write(txCtx) — the closure runs the SINGLE version-guarded
//     writer-repo method for the operation (carrying its own expectedVersion) and
//     RETURNS the MutationDelta;
//  2. OutboxWriter.WriteIntent(txCtx, intent, delta) — the WRITER allocates
//     (epoch, feed_position) LATE, finalizes the envelope's manifest from the
//     RETURNED delta (never from command inputs), persists the outbox row, and
//     returns the finalized envelope.
//
// The executor performs NO finalization and NO epoch/feed_position handling
// (round-3 blocker #1). The operation is identified by WHICH per-operation method
// built the closure, never by switching on intent.Kind (round-5 finding 1). The
// state change and its one envelope commit or roll back together. Returns the
// delta.
func (m *worldMutator) mutate(
	ctx context.Context,
	intent wmodel.EnvelopeIntent,
	write func(ctx context.Context) (*wmodel.MutationDelta, error),
) (*wmodel.MutationDelta, error) {
	var delta *wmodel.MutationDelta
	if err := m.transactor.InTransaction(ctx, func(txCtx context.Context) error {
		d, err := write(txCtx)
		if err != nil {
			return oops.Wrap(err)
		}
		if _, err := m.outbox.WriteIntent(txCtx, intent, d); err != nil {
			return oops.Wrap(err)
		}
		delta = d
		return nil
	}); err != nil {
		return nil, oops.Wrap(err)
	}
	return delta, nil
}

// moveCharacter builds the character-move write closure — capturing the PRIVATE
// character writer plus the operation args — and routes it through mutate(). The
// closure IS the operation identity (a character location update); there is no
// intent.Kind dispatch. expectedVersion is the character's read version (the CAS
// guard threaded from service.go).
func (m *worldMutator) moveCharacter(
	ctx context.Context,
	intent wmodel.EnvelopeIntent,
	characterID, toLocationID ulid.ULID,
	expectedVersion int,
) (*wmodel.MutationDelta, error) {
	return m.mutate(ctx, intent, func(txCtx context.Context) (*wmodel.MutationDelta, error) {
		return m.characterWriter.UpdateLocation(txCtx, characterID, &toLocationID, expectedVersion)
	})
}

// updateCharacterPreferences builds the character-preferences write closure —
// capturing the PRIVATE character writer plus the pre-marshaled preferences bag —
// and routes it through mutate() (round-4 C5 / D-05 — the folded-in
// character-settings write). The closure IS the operation identity (a character
// preferences update); there is no intent.Kind dispatch. expectedVersion is the
// character's read version (the CAS guard) so a concurrent conflicting settings
// write surfaces WORLD_CONCURRENT_EDIT rather than a silent last-write-wins. This
// is the write-command descriptor the 05-11 census bijection includes for the
// character_preferences_update kind.
func (m *worldMutator) updateCharacterPreferences(
	ctx context.Context,
	intent wmodel.EnvelopeIntent,
	characterID ulid.ULID,
	prefs []byte,
	expectedVersion int,
) (*wmodel.MutationDelta, error) {
	return m.mutate(ctx, intent, func(txCtx context.Context) (*wmodel.MutationDelta, error) {
		return m.characterWriter.UpdatePreferences(txCtx, characterID, prefs, expectedVersion)
	})
}

// updateCharacter routes a character update through mutate() (character_updated).
// char carries the read Version as the CAS guard; the character writer's Update
// finalizes the character_updated envelope from the returned delta in the same tx.
func (m *worldMutator) updateCharacter(ctx context.Context, intent wmodel.EnvelopeIntent, char *Character) (*wmodel.MutationDelta, error) {
	return m.mutate(ctx, intent, func(txCtx context.Context) (*wmodel.MutationDelta, error) {
		return m.characterWriter.Update(txCtx, char)
	})
}

// deleteCharacter routes a character delete + its property cascade through
// mutate() (character_deleted tombstone — the SAME kind the guest
// CharacterReapingService reuses, 05-16/D-06; consumers treat all character
// tombstones uniformly). The closure deletes the character's properties then the
// character row; the single tombstone envelope's manifest is finalized from the
// repo delta.
func (m *worldMutator) deleteCharacter(ctx context.Context, intent wmodel.EnvelopeIntent, id ulid.ULID) (*wmodel.MutationDelta, error) {
	return m.mutate(ctx, intent, func(txCtx context.Context) (*wmodel.MutationDelta, error) {
		if err := m.propertyWriter.DeleteByParent(txCtx, "character", id); err != nil {
			return nil, oops.Code("CHARACTER_DELETE_FAILED").
				With("operation", "delete_character_properties").
				Wrapf(err, "delete properties for character %s", id)
		}
		return m.characterWriter.Delete(txCtx, id, 0)
	})
}

// createLocation routes a location create through mutate(): the closure runs the
// guarded location Create and returns its delta; the writer finalizes the
// location_created envelope from that delta in the same tx.
func (m *worldMutator) createLocation(ctx context.Context, intent wmodel.EnvelopeIntent, loc *Location) (*wmodel.MutationDelta, error) {
	return m.mutate(ctx, intent, func(txCtx context.Context) (*wmodel.MutationDelta, error) {
		return m.locationWriter.Create(txCtx, loc)
	})
}

// updateLocation routes a location update through mutate() (location_updated).
func (m *worldMutator) updateLocation(ctx context.Context, intent wmodel.EnvelopeIntent, loc *Location) (*wmodel.MutationDelta, error) {
	return m.mutate(ctx, intent, func(txCtx context.Context) (*wmodel.MutationDelta, error) {
		return m.locationWriter.Update(txCtx, loc)
	})
}

// deleteLocation routes a location delete + its property cascade through mutate().
// The closure deletes the location's properties then the location row (whose repo
// delta carries the DB-cascaded exit tombstones preselected under lock, 05-02), so
// the single location_deleted tombstone envelope's manifest covers the cascade
// (INV-WORLD-2 parity) — one envelope per command, not per cascaded row.
func (m *worldMutator) deleteLocation(ctx context.Context, intent wmodel.EnvelopeIntent, id ulid.ULID) (*wmodel.MutationDelta, error) {
	return m.mutate(ctx, intent, func(txCtx context.Context) (*wmodel.MutationDelta, error) {
		if err := m.propertyWriter.DeleteByParent(txCtx, "location", id); err != nil {
			return nil, oops.Code("LOCATION_DELETE_FAILED").
				With("operation", "delete_location_properties").
				Wrapf(err, "delete properties for location %s", id)
		}
		return m.locationWriter.Delete(txCtx, id, 0)
	})
}

// createExit routes an exit create through mutate() (exit_created).
func (m *worldMutator) createExit(ctx context.Context, intent wmodel.EnvelopeIntent, exit *Exit) (*wmodel.MutationDelta, error) {
	return m.mutate(ctx, intent, func(txCtx context.Context) (*wmodel.MutationDelta, error) {
		return m.exitWriter.Create(txCtx, exit)
	})
}

// updateExit routes an exit update through mutate() (exit_updated).
func (m *worldMutator) updateExit(ctx context.Context, intent wmodel.EnvelopeIntent, exit *Exit) (*wmodel.MutationDelta, error) {
	return m.mutate(ctx, intent, func(txCtx context.Context) (*wmodel.MutationDelta, error) {
		return m.exitWriter.Update(txCtx, exit)
	})
}

// deleteExit routes an exit delete through mutate() (exit_deleted tombstone). The
// exit repo's Delete atomically removes the bidirectional reverse exit and reports
// it in the delta's Affected list, so the single tombstone envelope's manifest
// covers the cascade.
//
// A NON-severe BidirectionalCleanupResult (the reverse exit was already gone) means
// the primary delete committed — the closure captures it and returns (delta, nil)
// so the envelope IS written and the tx commits; the notice is surfaced to the
// caller post-commit. A SEVERE cleanup result (or any other error) rolls the tx
// back with no envelope. deleteExit returns (delta, non-severe-notice-or-nil, err).
func (m *worldMutator) deleteExit(ctx context.Context, intent wmodel.EnvelopeIntent, id ulid.ULID) (*wmodel.MutationDelta, *BidirectionalCleanupResult, error) {
	var notice *BidirectionalCleanupResult
	delta, err := m.mutate(ctx, intent, func(txCtx context.Context) (*wmodel.MutationDelta, error) {
		d, delErr := m.exitWriter.Delete(txCtx, id, 0)
		if delErr != nil {
			var cleanup *BidirectionalCleanupResult
			if errors.As(delErr, &cleanup) && !cleanup.IsSevere() {
				// Non-severe: the primary delete committed; surface the notice after
				// commit and still write the tombstone envelope from the returned delta.
				notice = cleanup
				return d, nil
			}
			return nil, oops.Wrap(delErr)
		}
		return d, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return delta, notice, nil
}

// createObject routes an object create through mutate() (object_created).
func (m *worldMutator) createObject(ctx context.Context, intent wmodel.EnvelopeIntent, obj *Object) (*wmodel.MutationDelta, error) {
	return m.mutate(ctx, intent, func(txCtx context.Context) (*wmodel.MutationDelta, error) {
		return m.objectWriter.Create(txCtx, obj)
	})
}

// updateObject routes an object update through mutate() (object_updated).
func (m *worldMutator) updateObject(ctx context.Context, intent wmodel.EnvelopeIntent, obj *Object) (*wmodel.MutationDelta, error) {
	return m.mutate(ctx, intent, func(txCtx context.Context) (*wmodel.MutationDelta, error) {
		return m.objectWriter.Update(txCtx, obj)
	})
}

// deleteObject routes an object delete + its property cascade through mutate()
// (object_deleted tombstone). The closure deletes the object's properties then the
// object row; the single tombstone envelope's manifest is finalized from the repo
// delta.
func (m *worldMutator) deleteObject(ctx context.Context, intent wmodel.EnvelopeIntent, id ulid.ULID) (*wmodel.MutationDelta, error) {
	return m.mutate(ctx, intent, func(txCtx context.Context) (*wmodel.MutationDelta, error) {
		if err := m.propertyWriter.DeleteByParent(txCtx, "object", id); err != nil {
			return nil, oops.Code("OBJECT_DELETE_FAILED").
				With("operation", "delete_object_properties").
				Wrapf(err, "delete properties for object %s", id)
		}
		return m.objectWriter.Delete(txCtx, id, 0)
	})
}

// moveObject routes an object containment change through mutate() (object_moved).
func (m *worldMutator) moveObject(ctx context.Context, intent wmodel.EnvelopeIntent, id ulid.ULID, to Containment) (*wmodel.MutationDelta, error) {
	return m.mutate(ctx, intent, func(txCtx context.Context) (*wmodel.MutationDelta, error) {
		return m.objectWriter.Move(txCtx, id, to, 0)
	})
}
