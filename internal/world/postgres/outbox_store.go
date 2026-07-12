// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/world/wmodel"
)

// OutboxStore is the same-transaction outbox row writer. It is the WRITER
// BOUNDARY: the outbox INSERT SQL lives here in internal/world/postgres, never in
// internal/world/outbox (finding 6 — internal/world/outbox holds the relay/
// consumer and reads finalized envelopes back, so it never writes world/outbox
// rows and does not import postgres).
//
// The store owns the storage-stamped envelope fields (round-3 blocker #1): its
// WriteIntent allocates (epoch, feed_position) from the locked per-game counter,
// finalizes the envelope via the pure wmodel.Finalize, persists the row, and
// returns the finalized envelope. No caller ever constructs a finalized Envelope.
type OutboxStore struct {
	pool    *pgxpool.Pool
	counter *FeedCounter
}

// NewOutboxStore constructs an OutboxStore. The pool is only the fallback
// execer — WriteIntent is expected to run inside a caller-owned mutation
// transaction and enrolls via execerFromCtx.
func NewOutboxStore(pool *pgxpool.Pool) *OutboxStore {
	return &OutboxStore{pool: pool, counter: NewFeedCounter(pool)}
}

// WriteIntent allocates the next (epoch, feed_position) from the locked counter
// for intent.GameID (acquired LATE — right before the insert), finalizes the
// envelope via wmodel.Finalize(intent, delta, epoch, position), inserts exactly
// one outbox row through the ambient transaction (execerFromCtx), and RETURNS the
// finalized *wmodel.Envelope.
//
// It MUST NOT open its own connection or transaction: the caller (the mutation
// executor / character-genesis service, 05-06/05-15) owns the InTransaction, and
// the store enrolls via execerFromCtx so the outbox row commits atomically with
// the state change. This method structurally satisfies the world.OutboxWriter
// interface the executor declares (05-06).
func (s *OutboxStore) WriteIntent(
	ctx context.Context,
	intent wmodel.EnvelopeIntent,
	delta *wmodel.MutationDelta,
) (*wmodel.Envelope, error) {
	epoch, position, err := s.counter.Allocate(ctx, intent.GameID)
	if err != nil {
		return nil, err
	}

	env := wmodel.Finalize(intent, delta, epoch, position)

	affectedJSON, err := json.Marshal(env.Affected)
	if err != nil {
		return nil, oops.With("operation", "marshal affected manifest").
			With("event_id", env.EventID.String()).Wrap(err)
	}

	payload := env.Payload
	if len(payload) == 0 {
		// The payload column is JSONB NOT NULL; an absent intent payload persists
		// as the JSON null literal (a valid JSONB value, not SQL NULL).
		payload = []byte("null")
	}

	e := execerFromCtx(ctx, s.pool)
	if _, err := e.Exec(ctx, `
		INSERT INTO outbox (
			event_id, game_id, feed_position, epoch, kind, schema_version,
			actor, causation_id, correlation_id, aggregate_id, aggregate_type,
			affected, payload
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		env.EventID.String(),
		env.GameID,
		env.FeedPosition,
		env.Epoch,
		env.Kind,
		env.SchemaVersion,
		env.Actor,
		nullableString(env.CausationID),
		nullableString(env.CorrelationID),
		env.AggregateID.String(),
		string(env.AggregateType),
		affectedJSON,
		payload,
	); err != nil {
		return nil, oops.With("operation", "insert outbox row").
			With("event_id", env.EventID.String()).
			With("game_id", env.GameID).Wrap(err)
	}

	return env, nil
}
