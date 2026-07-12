// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package wmodel holds cycle-neutral value types shared across the world model
// layers (world, world/postgres, world/outbox). It is a LEAF package: it imports
// only the standard library and the ULID type, and MUST import none of
// internal/world, internal/world/postgres, or internal/world/outbox. Keeping
// these contract types here is what prevents the world -> outbox -> world and
// outbox -> postgres -> outbox import cycles from forming.
package wmodel

import "github.com/oklog/ulid/v2"

// AggregateType identifies which world aggregate an AffectedAggregate refers to.
type AggregateType string

const (
	// AggregateLocation is a world location aggregate.
	AggregateLocation AggregateType = "location"
	// AggregateExit is a world exit aggregate.
	AggregateExit AggregateType = "exit"
	// AggregateCharacter is a world character aggregate.
	AggregateCharacter AggregateType = "character"
	// AggregateObject is a world object aggregate.
	AggregateObject AggregateType = "object"
	// AggregateScene is a world scene aggregate.
	AggregateScene AggregateType = "scene"
)

// AffectedAggregate describes a single aggregate row that a write touched,
// carrying its before/after optimistic-concurrency versions and a tombstone flag
// for deletes. Repository-internal cascade rows (e.g. a bidirectional exit's
// reverse exit, or DB-cascaded children) are reported as additional
// AffectedAggregate entries so the outbox manifest can be built from the rows the
// command actually touched rather than from command inputs.
type AffectedAggregate struct {
	// Type is the aggregate kind (location/exit/character/object/scene).
	Type AggregateType
	// ID is the aggregate's primary key.
	ID ulid.ULID
	// BeforeVersion is the version guard read before the write (0 for creates).
	BeforeVersion int
	// AfterVersion is the version after the write (0 for tombstones/pre-guard).
	AfterVersion int
	// Tombstone marks that this aggregate was deleted by the write.
	Tombstone bool
}

// MutationDelta is the contract a guarded write method returns: the primary
// aggregate it targeted plus every additional aggregate it affected (including
// repository-internal cascade IDs). It is the source the outbox manifest is built
// from (05-10/05-11) — not the command inputs.
type MutationDelta struct {
	// Primary is the aggregate the command directly targeted.
	Primary AffectedAggregate
	// Affected lists additional aggregates the write touched (cascades, reverse
	// exits, children). Empty when the write touched only the primary.
	Affected []AffectedAggregate
}
