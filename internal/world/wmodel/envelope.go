// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package wmodel

import (
	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/core"
)

// EnvelopeIntent is the caller-supplied half of a world-change envelope: the
// fields a command can produce BEFORE the write commits. It deliberately
// EXCLUDES feed_position, epoch, and the affected-aggregates manifest — those are
// only knowable AFTER the write and are owned exclusively by the postgres
// WriteIntent writer (round-3 blocker #1: each envelope field has exactly one
// owner). Game identity is EXPLICIT here (round-9 R6-5 MEDIUM) because outbox
// ordering, the per-game feed counter, and the (consumer_name, game_id)
// watermark are all per-game — the command knows its game (single `main` today,
// keyed for multi-game later, resolving Open Question 2).
type EnvelopeIntent struct {
	// EventID is the fresh ULID stamped by NewEnvelopeIntent; it is the outbox
	// dedup key and the Nats-Msg-Id the relay publishes with.
	EventID ulid.ULID
	// GameID is the caller-supplied game identity that keys the feed counter and
	// the outbox row's game_id column.
	GameID string
	// Kind is the taxonomy kind of the change (e.g. "location_updated").
	Kind string
	// SchemaVersion is the payload schema version.
	SchemaVersion int
	// Actor identifies who caused the change (already-authorized, committed fact).
	Actor string
	// CausationID / CorrelationID are optional trace linkage ids.
	CausationID   string
	CorrelationID string
	// AggregateType / AggregateID identify the aggregate the command targeted.
	AggregateType AggregateType
	AggregateID   ulid.ULID
	// Payload is the intent-level, new-values-only JSON (erasure-safe; no secrets).
	Payload []byte
}

// IntentParams carries the caller fields for NewEnvelopeIntent. It omits EventID
// so the constructor is the single mint site for the event ULID.
type IntentParams struct {
	GameID        string
	Kind          string
	SchemaVersion int
	Actor         string
	CausationID   string
	CorrelationID string
	AggregateType AggregateType
	AggregateID   ulid.ULID
	Payload       []byte
}

// NewEnvelopeIntent constructs an EnvelopeIntent, stamping a fresh monotonic
// event ULID via core.NewULID(). It MUST be the only mint site for the intent's
// EventID — never hand-mint an id and never use idgen.New() (idgen is for entity
// primary keys, not events; per .claude/rules/event-conventions.md the event
// identity/dedup key comes from core.NewULID()).
func NewEnvelopeIntent(p IntentParams) EnvelopeIntent {
	return EnvelopeIntent{
		EventID:       core.NewULID(),
		GameID:        p.GameID,
		Kind:          p.Kind,
		SchemaVersion: p.SchemaVersion,
		Actor:         p.Actor,
		CausationID:   p.CausationID,
		CorrelationID: p.CorrelationID,
		AggregateType: p.AggregateType,
		AggregateID:   p.AggregateID,
		Payload:       p.Payload,
	}
}

// Envelope is the finalized, persisted world-change shape: every EnvelopeIntent
// field plus the storage-owned epoch + feed_position and the affected-aggregates
// manifest built from the write's MutationDelta.
type Envelope struct {
	EventID       ulid.ULID
	GameID        string
	Kind          string
	SchemaVersion int
	Actor         string
	CausationID   string
	CorrelationID string
	AggregateType AggregateType
	AggregateID   ulid.ULID
	Payload       []byte
	// Epoch and FeedPosition are allocated by the postgres WriteIntent writer from
	// the locked per-game counter — never insert-time / auto-increment.
	Epoch        int64
	FeedPosition int64
	// Affected is the manifest of every aggregate the write touched (primary
	// first, then cascades), each carrying its before/after versions.
	Affected []AffectedAggregate
}

// Finalize is the PURE constructor that turns an EnvelopeIntent into a persisted
// Envelope, given the write's MutationDelta and the writer-allocated (epoch,
// position). It builds the affected-aggregates manifest from the delta (NOT from
// command inputs) and carries intent.GameID through to Envelope.GameID unchanged.
//
// Its ONLY production caller is the postgres WriteIntent writer (05-05 Task 3):
// the writer allocates epoch/position from the locked counter and finalizes in
// one place. Executors (05-06) and commands never construct a finalized Envelope
// — they pass (intent, delta) to WriteIntent and receive the finalized envelope
// back (round-3 blocker #1: exactly one owner per field).
func Finalize(intent EnvelopeIntent, delta *MutationDelta, epoch, position int64) *Envelope {
	return &Envelope{
		EventID:       intent.EventID,
		GameID:        intent.GameID,
		Kind:          intent.Kind,
		SchemaVersion: intent.SchemaVersion,
		Actor:         intent.Actor,
		CausationID:   intent.CausationID,
		CorrelationID: intent.CorrelationID,
		AggregateType: intent.AggregateType,
		AggregateID:   intent.AggregateID,
		Payload:       intent.Payload,
		Epoch:         epoch,
		FeedPosition:  position,
		Affected:      manifestFromDelta(delta),
	}
}

// manifestFromDelta flattens a MutationDelta into the affected-aggregates
// manifest: the primary aggregate first, then every cascade/affected aggregate.
// A nil delta yields a nil manifest.
func manifestFromDelta(delta *MutationDelta) []AffectedAggregate {
	if delta == nil {
		return nil
	}
	manifest := make([]AffectedAggregate, 0, 1+len(delta.Affected))
	manifest = append(manifest, delta.Primary)
	manifest = append(manifest, delta.Affected...)
	return manifest
}
