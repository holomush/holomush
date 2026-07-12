// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/world/wmodel"
)

// OutboxNotifyChannel is the LISTEN/NOTIFY channel the outbox write signals and
// the relay's dedicated LISTEN connection waits on.
const OutboxNotifyChannel = "world_outbox"

// errStaleLease is the postgres-side stale-lease sentinel. It is stamped with the
// code the outbox package's ErrStaleLease carries so the relay's
// errors.Is(err, outbox.ErrStaleLease) check works structurally — but package
// postgres MUST NOT import internal/world/outbox (forbidden edge), so the two
// sentinels are matched by the stamped oops code, and the relay maps a
// code-stamped error back to outbox.ErrStaleLease via the setup adapter.
//
// To keep the relay's errors.Is check simple, the setup adapter wraps a
// postgres stale-lease error into outbox.ErrStaleLease. Here we expose the
// sentinel + a classifier the adapter uses.
var errStaleLease = errors.New("world/postgres: stale outbox lease")

// ErrStaleLease is the exported postgres stale-lease sentinel. The setup adapter
// translates it to outbox.ErrStaleLease (which the relay matches with errors.Is)
// so package postgres never imports internal/world/outbox.
var ErrStaleLease = errStaleLease

// CodeStaleLease is the oops code stamped on a stale-lease error.
const CodeStaleLease = "WORLD_OUTBOX_STALE_LEASE"

// advisoryLockKey derives a stable 64-bit session-advisory-lock key from the
// game id, namespaced so it cannot collide with other advisory-lock users.
func advisoryLockKey(gameID string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("world_outbox_relay:"))
	_, _ = h.Write([]byte(gameID))
	return int64(h.Sum64()) //nolint:gosec // intentional bit reinterpretation into a signed advisory-lock key
}

// AcquireLease pins a DEDICATED *pgxpool.Conn (NOT a per-call pooled borrow —
// round-6 R6-5: a session-level advisory lock MUST live on a pinned conn), takes
// the session-level per-game pg_advisory_lock on it, atomically BUMPS the durable
// world_feed_counter.lease_generation (round-4 A2), and returns an *OutboxLease
// BOUND to that pinned conn carrying the bumped generation. Every OutboxLease
// method runs its SQL on the bound conn; Release runs pg_advisory_unlock and
// returns the conn to the pool.
//
// *OutboxLease structurally satisfies the consumer-owned outbox.Lease interface
// (all method signatures use only wmodel/ulid/int64), so setup can inject it
// WITHOUT package postgres importing internal/world/outbox.
func (s *OutboxStore) AcquireLease(ctx context.Context, gameID string) (*OutboxLease, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, oops.Code("WORLD_OUTBOX_LEASE_ACQUIRE_CONN_FAILED").
			With("game_id", gameID).Wrap(err)
	}
	lockKey := advisoryLockKey(gameID)
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, lockKey); err != nil {
		conn.Release()
		return nil, oops.Code("WORLD_OUTBOX_LEASE_LOCK_FAILED").
			With("game_id", gameID).Wrap(err)
	}
	var generation int64
	if err := conn.QueryRow(ctx, `
		INSERT INTO world_feed_counter (game_id, next_position, epoch, lease_generation)
		VALUES ($1, 1, 1, 1)
		ON CONFLICT (game_id) DO UPDATE
		SET lease_generation = world_feed_counter.lease_generation + 1
		RETURNING lease_generation`, gameID).Scan(&generation); err != nil {
		_, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, lockKey)
		conn.Release()
		return nil, oops.Code("WORLD_OUTBOX_LEASE_BUMP_GENERATION_FAILED").
			With("game_id", gameID).Wrap(err)
	}
	return &OutboxLease{conn: conn, gameID: gameID, generation: generation, lockKey: lockKey}, nil
}

// OutboxLease is the connection-bound single-writer capability the relay holds.
// It carries the durable generation it acquired and runs every method on the
// pinned advisory-lock connection.
type OutboxLease struct {
	conn       *pgxpool.Conn
	gameID     string
	generation int64
	lockKey    int64
	released   bool
}

// Generation returns the durable lease generation acquired at AcquireLease time.
func (l *OutboxLease) Generation() int64 { return l.generation }

// alive reports whether the pinned connection is still usable; a dead/closed conn
// (connection loss → Postgres drops the session lock) means the lease is stale.
func (l *OutboxLease) alive() error {
	if l.released {
		return oops.Code(CodeStaleLease).Wrap(errStaleLease)
	}
	if l.conn == nil || l.conn.Conn() == nil || l.conn.Conn().PgConn().IsClosed() {
		return oops.Code(CodeStaleLease).Wrap(errStaleLease)
	}
	return nil
}

// nowNS returns the current time as epoch nanoseconds (INV-STORE-1 columns).
func nowNS() int64 { return time.Now().UnixNano() }

// NextUnpublished returns the lowest (epoch, feed_position) unpublished row for
// the game, or (nil, nil) when the feed is fully drained.
func (l *OutboxLease) NextUnpublished(ctx context.Context) (*wmodel.Envelope, error) {
	if err := l.alive(); err != nil {
		return nil, err
	}
	var (
		eventIDStr, gameID, kind, actor  string
		aggregateIDStr, aggregateTypeStr string
		causation, correlation           *string
		feedPosition, epoch              int64
		schemaVersion                    int
		affectedBytes, payloadBytes      []byte
	)
	err := l.conn.QueryRow(ctx, `
		SELECT event_id, game_id, feed_position, epoch, kind, schema_version,
		       actor, causation_id, correlation_id, aggregate_id, aggregate_type,
		       affected, payload
		FROM outbox
		WHERE game_id = $1 AND published_at IS NULL
		ORDER BY epoch, feed_position
		LIMIT 1`, l.gameID).Scan(
		&eventIDStr, &gameID, &feedPosition, &epoch, &kind, &schemaVersion,
		&actor, &causation, &correlation, &aggregateIDStr, &aggregateTypeStr,
		&affectedBytes, &payloadBytes,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, oops.Code("WORLD_OUTBOX_NEXT_UNPUBLISHED_QUERY_FAILED").
			With("game_id", l.gameID).Wrap(err)
	}

	eventID, err := ulid.Parse(eventIDStr)
	if err != nil {
		return nil, oops.Code("WORLD_OUTBOX_ROW_BAD_EVENT_ID").With("event_id", eventIDStr).Wrap(err)
	}
	aggregateID, err := ulid.Parse(aggregateIDStr)
	if err != nil {
		return nil, oops.Code("WORLD_OUTBOX_ROW_BAD_AGGREGATE_ID").With("aggregate_id", aggregateIDStr).Wrap(err)
	}
	var affected []wmodel.AffectedAggregate
	if len(affectedBytes) > 0 {
		if err := json.Unmarshal(affectedBytes, &affected); err != nil {
			return nil, oops.Code("WORLD_OUTBOX_ROW_BAD_AFFECTED").With("event_id", eventIDStr).Wrap(err)
		}
	}
	var payload []byte
	if len(payloadBytes) > 0 && string(payloadBytes) != "null" {
		payload = payloadBytes
	}
	return &wmodel.Envelope{
		EventID:       eventID,
		GameID:        gameID,
		Kind:          kind,
		SchemaVersion: schemaVersion,
		Actor:         actor,
		CausationID:   derefString(causation),
		CorrelationID: derefString(correlation),
		AggregateType: wmodel.AggregateType(aggregateTypeStr),
		AggregateID:   aggregateID,
		Payload:       payload,
		Epoch:         epoch,
		FeedPosition:  feedPosition,
		Affected:      affected,
	}, nil
}

// MarkPublished marks the row for eventID published, but only if the durable
// lease_generation still equals generation (the fencing comparison, round-4 A2).
// A stale generation → a stale-lease error; the stale holder's DB ack is rejected.
func (l *OutboxLease) MarkPublished(ctx context.Context, eventID ulid.ULID, generation int64) error {
	if err := l.alive(); err != nil {
		return err
	}
	var stored int64
	if err := l.conn.QueryRow(ctx,
		`SELECT lease_generation FROM world_feed_counter WHERE game_id = $1`, l.gameID).Scan(&stored); err != nil {
		return oops.Code("WORLD_OUTBOX_MARK_READ_GENERATION_FAILED").With("game_id", l.gameID).Wrap(err)
	}
	if stored != generation {
		return oops.Code(CodeStaleLease).
			With("game_id", l.gameID).
			With("lease_generation", generation).
			With("stored_generation", stored).
			Wrap(errStaleLease)
	}
	tag, err := l.conn.Exec(ctx,
		`UPDATE outbox SET published_at = $2 WHERE event_id = $1 AND published_at IS NULL`,
		eventID.String(), nowNS())
	if err != nil {
		return oops.Code("WORLD_OUTBOX_MARK_PUBLISHED_UPDATE_FAILED").
			With("event_id", eventID.String()).Wrap(err)
	}
	_ = tag // a 0-row update is a benign re-mark (already published); not an error.
	return nil
}

// Prune deletes published rows for the game and returns the count deleted.
func (l *OutboxLease) Prune(ctx context.Context) (int64, error) {
	if err := l.alive(); err != nil {
		return 0, err
	}
	tag, err := l.conn.Exec(ctx,
		`DELETE FROM outbox WHERE game_id = $1 AND published_at IS NOT NULL`, l.gameID)
	if err != nil {
		return 0, oops.Code("WORLD_OUTBOX_PRUNE_FAILED").With("game_id", l.gameID).Wrap(err)
	}
	return tag.RowsAffected(), nil
}

// MarkSkipResolved marks the lowest unpublished poison row at position resolved.
func (l *OutboxLease) MarkSkipResolved(ctx context.Context, position int64) error {
	if err := l.alive(); err != nil {
		return err
	}
	_, err := l.conn.Exec(ctx, `
		UPDATE outbox SET published_at = $3
		WHERE ctid = (
			SELECT ctid FROM outbox
			WHERE game_id = $1 AND feed_position = $2 AND published_at IS NULL
			ORDER BY epoch LIMIT 1
		)`, l.gameID, position, nowNS())
	if err != nil {
		return oops.Code("WORLD_OUTBOX_SKIP_RESOLVE_UPDATE_FAILED").
			With("game_id", l.gameID).With("position", position).Wrap(err)
	}
	return nil
}

// CurrentEpoch returns the game's current feed epoch.
func (l *OutboxLease) CurrentEpoch(ctx context.Context) (int64, error) {
	if err := l.alive(); err != nil {
		return 0, err
	}
	var epoch int64
	if err := l.conn.QueryRow(ctx,
		`SELECT epoch FROM world_feed_counter WHERE game_id = $1`, l.gameID).Scan(&epoch); err != nil {
		return 0, oops.Code("WORLD_OUTBOX_CURRENT_EPOCH_FAILED").With("game_id", l.gameID).Wrap(err)
	}
	return epoch, nil
}

// PersistSkipMarkerID stores the stable skip-marker id on the lowest unpublished
// row at position (round-4 A1).
func (l *OutboxLease) PersistSkipMarkerID(ctx context.Context, position int64, eventID ulid.ULID) error {
	if err := l.alive(); err != nil {
		return err
	}
	_, err := l.conn.Exec(ctx, `
		UPDATE outbox SET skip_marker_event_id = $3
		WHERE ctid = (
			SELECT ctid FROM outbox
			WHERE game_id = $1 AND feed_position = $2 AND published_at IS NULL
			ORDER BY epoch LIMIT 1
		)`, l.gameID, position, eventID.String())
	if err != nil {
		return oops.Code("WORLD_OUTBOX_PERSIST_SKIP_MARKER_FAILED").
			With("game_id", l.gameID).With("position", position).Wrap(err)
	}
	return nil
}

// SkipMarkerID reads back the stable skip-marker id on the lowest unpublished row
// at position, reporting whether one was already persisted.
func (l *OutboxLease) SkipMarkerID(ctx context.Context, position int64) (ulid.ULID, bool, error) {
	if err := l.alive(); err != nil {
		return ulid.ULID{}, false, err
	}
	var markerStr *string
	err := l.conn.QueryRow(ctx, `
		SELECT skip_marker_event_id FROM outbox
		WHERE game_id = $1 AND feed_position = $2 AND published_at IS NULL
		ORDER BY epoch LIMIT 1`, l.gameID, position).Scan(&markerStr)
	if errors.Is(err, pgx.ErrNoRows) {
		return ulid.ULID{}, false, nil
	}
	if err != nil {
		return ulid.ULID{}, false, oops.Code("WORLD_OUTBOX_READ_SKIP_MARKER_FAILED").
			With("game_id", l.gameID).With("position", position).Wrap(err)
	}
	if markerStr == nil || *markerStr == "" {
		return ulid.ULID{}, false, nil
	}
	id, err := ulid.Parse(*markerStr)
	if err != nil {
		return ulid.ULID{}, false, oops.Code("WORLD_OUTBOX_BAD_SKIP_MARKER_ID").With("marker_id", *markerStr).Wrap(err)
	}
	return id, true, nil
}

// Release runs pg_advisory_unlock and returns the pinned connection to the pool.
// Idempotent.
func (l *OutboxLease) Release(ctx context.Context) error {
	if l.released {
		return nil
	}
	l.released = true
	defer l.conn.Release()
	if l.conn.Conn() != nil && !l.conn.Conn().PgConn().IsClosed() {
		if _, err := l.conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, l.lockKey); err != nil {
			return oops.Code("WORLD_OUTBOX_LEASE_UNLOCK_FAILED").With("game_id", l.gameID).Wrap(err)
		}
	}
	return nil
}

// derefString returns the pointed-to string or "".
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
