// Package store provides storage implementations.
package store

import (
	"context"
	_ "embed"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/core"
)

//go:embed migrations/001_initial.sql
var migrationSQL string

// PostgresEventStore implements EventStore using PostgreSQL.
type PostgresEventStore struct {
	pool *pgxpool.Pool
}

// NewPostgresEventStore creates a new PostgreSQL event store.
func NewPostgresEventStore(ctx context.Context, dsn string) (*PostgresEventStore, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	return &PostgresEventStore{pool: pool}, nil
}

// Close closes the database connection pool.
func (s *PostgresEventStore) Close() {
	s.pool.Close()
}

// Migrate runs database migrations.
func (s *PostgresEventStore) Migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, migrationSQL)
	return err
}

// Append persists an event.
func (s *PostgresEventStore) Append(ctx context.Context, event core.Event) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO events (id, stream, type, actor_kind, actor_id, payload, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		event.ID.String(),
		event.Stream,
		string(event.Type),
		event.Actor.Kind,
		event.Actor.ID,
		event.Payload,
		event.Timestamp,
	)
	return err
}

// Replay returns events from a stream after the given ID.
func (s *PostgresEventStore) Replay(ctx context.Context, stream string, afterID ulid.ULID, limit int) ([]core.Event, error) {
	var rows pgx.Rows
	var err error

	if afterID.Compare(ulid.ULID{}) == 0 {
		rows, err = s.pool.Query(ctx,
			`SELECT id, stream, type, actor_kind, actor_id, payload, created_at
			 FROM events WHERE stream = $1 ORDER BY id LIMIT $2`,
			stream, limit)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT id, stream, type, actor_kind, actor_id, payload, created_at
			 FROM events WHERE stream = $1 AND id > $2 ORDER BY id LIMIT $3`,
			stream, afterID.String(), limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []core.Event
	for rows.Next() {
		var e core.Event
		var idStr string
		var typeStr string
		if err := rows.Scan(&idStr, &e.Stream, &typeStr, &e.Actor.Kind, &e.Actor.ID, &e.Payload, &e.Timestamp); err != nil {
			return nil, err
		}
		e.ID, _ = ulid.Parse(idStr)
		e.Type = core.EventType(typeStr)
		events = append(events, e)
	}
	return events, rows.Err()
}

// LastEventID returns the most recent event ID for a stream.
func (s *PostgresEventStore) LastEventID(ctx context.Context, stream string) (ulid.ULID, error) {
	var idStr string
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM events WHERE stream = $1 ORDER BY id DESC LIMIT 1`,
		stream).Scan(&idStr)
	if err == pgx.ErrNoRows {
		return ulid.ULID{}, core.ErrStreamEmpty
	}
	if err != nil {
		return ulid.ULID{}, err
	}
	return ulid.Parse(idStr)
}
