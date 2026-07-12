// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"context"

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
// As of 05-06 only the character writer is owned here (MoveCharacter is the first
// command through the seam). The remaining write commands migrate in 05-10/05-11,
// and the genuine compile-time fence (Service holds only reader views, no
// directly-callable write repo) closes in 05-11 once every command routes through
// mutate() — closing it here would break compilation of the un-migrated commands.
type worldMutator struct {
	characterWriter CharacterRepository
	transactor      Transactor
	outbox          OutboxWriter
}

// newWorldMutator constructs the write executor. characterWriter is the private
// write-capable character repository; transactor is the re-entrant transaction
// seam (05-14); outbox is the injected OutboxWriter the executor persists through.
func newWorldMutator(characterWriter CharacterRepository, transactor Transactor, outbox OutboxWriter) *worldMutator {
	return &worldMutator{characterWriter: characterWriter, transactor: transactor, outbox: outbox}
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
