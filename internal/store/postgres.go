// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package store provides storage implementations.
package store

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/core"
)

// ErrSystemInfoNotFound is returned when a system info key doesn't exist.
var ErrSystemInfoNotFound = errors.New("system info key not found")

// poolIface defines the pgxpool methods used by PostgresEventStore.
// This interface enables testing with mocks.
type poolIface interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Close()
}

// connIface defines the pgx.Conn methods used for LISTEN/NOTIFY subscriptions.
// This interface enables testing with mocks.
type connIface interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	WaitForNotification(ctx context.Context) (*pgconn.Notification, error)
	Close(ctx context.Context) error
}

// connectorFunc creates a new database connection for LISTEN/NOTIFY.
// This is a function type to enable test mocking.
type connectorFunc func(ctx context.Context, dsn string) (connIface, error)

// defaultConnector creates a real pgx connection.
func defaultConnector(ctx context.Context, dsn string) (connIface, error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, oops.With("dsn_length", len(dsn)).Wrap(err)
	}
	return conn, nil
}

// PostgresEventStore implements EventStore using PostgreSQL.
type PostgresEventStore struct {
	pool      poolIface
	dsn       string        // stored for creating new connections for LISTEN
	connector connectorFunc // for creating LISTEN connections (nil uses default)
}

// NewPostgresEventStore creates a new PostgreSQL event store.
func NewPostgresEventStore(ctx context.Context, dsn string) (*PostgresEventStore, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, oops.With("operation", "connect to database").Wrap(err)
	}
	return &PostgresEventStore{pool: pool, dsn: dsn}, nil
}

// Close closes the database connection pool.
func (s *PostgresEventStore) Close() {
	s.pool.Close()
}

// Pool returns the underlying database connection pool.
// This allows sharing the connection with other repositories.
// Returns nil if the pool is not a *pgxpool.Pool (e.g., in tests with mocks).
func (s *PostgresEventStore) Pool() *pgxpool.Pool {
	if pool, ok := s.pool.(*pgxpool.Pool); ok {
		return pool
	}
	return nil
}

// Append persists an event and notifies subscribers via NOTIFY.
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
	if err != nil {
		return oops.With("operation", "append event").With("event_id", event.ID.String()).With("stream", event.Stream).Wrap(err)
	}

	// Notify subscribers of the new event
	// Errors are logged but not returned - event is already persisted, subscribers catch up via Replay
	channel := streamToChannel(event.Stream)
	if _, notifyErr := s.pool.Exec(ctx, "SELECT pg_notify($1, $2)", channel, event.ID.String()); notifyErr != nil {
		slog.Warn("failed to notify subscribers of event",
			"event_id", event.ID.String(),
			"stream", event.Stream,
			"channel", channel,
			"error", notifyErr)
	}
	return nil
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
		return nil, oops.With("operation", "query events").With("stream", stream).Wrap(err)
	}
	defer rows.Close()

	var events []core.Event
	for rows.Next() {
		var e core.Event
		var idStr string
		var typeStr string
		if scanErr := rows.Scan(&idStr, &e.Stream, &typeStr, &e.Actor.Kind, &e.Actor.ID, &e.Payload, &e.Timestamp); scanErr != nil {
			return nil, oops.With("operation", "scan event row").With("stream", stream).Wrap(scanErr)
		}
		e.ID, err = ulid.Parse(idStr)
		if err != nil {
			return nil, oops.With("operation", "parse event ID").With("stream", stream).With("raw_id", idStr).Wrap(err)
		}
		e.Type = core.EventType(typeStr)
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.With("operation", "iterate events").With("stream", stream).Wrap(err)
	}
	return events, nil
}

// LastEventID returns the most recent event ID for a stream.
func (s *PostgresEventStore) LastEventID(ctx context.Context, stream string) (ulid.ULID, error) {
	var idStr string
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM events WHERE stream = $1 ORDER BY id DESC LIMIT 1`,
		stream).Scan(&idStr)
	if errors.Is(err, pgx.ErrNoRows) {
		return ulid.ULID{}, core.ErrStreamEmpty
	}
	if err != nil {
		return ulid.ULID{}, oops.With("operation", "query last event ID").With("stream", stream).Wrap(err)
	}
	id, err := ulid.Parse(idStr)
	if err != nil {
		return ulid.ULID{}, oops.With("operation", "parse last event ID").With("stream", stream).With("raw_id", idStr).Wrap(err)
	}
	return id, nil
}

// GetSystemInfo retrieves a system info value by key.
func (s *PostgresEventStore) GetSystemInfo(ctx context.Context, key string) (string, error) {
	var value string
	err := s.pool.QueryRow(ctx,
		`SELECT value FROM holomush_system_info WHERE key = $1`,
		key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", oops.With("key", key).Wrap(ErrSystemInfoNotFound)
	}
	if err != nil {
		return "", oops.With("operation", "get system info").With("key", key).Wrap(err)
	}
	return value, nil
}

// SetSystemInfo sets a system info value (upsert).
func (s *PostgresEventStore) SetSystemInfo(ctx context.Context, key, value string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO holomush_system_info (key, value) VALUES ($1, $2)
		 ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = NOW()`,
		key, value)
	if err != nil {
		return oops.With("operation", "set system info").With("key", key).Wrap(err)
	}
	return nil
}

// InitGameID ensures a game_id exists, generating one if needed.
func (s *PostgresEventStore) InitGameID(ctx context.Context) (string, error) {
	gameID, err := s.GetSystemInfo(ctx, "game_id")
	if err == nil {
		return gameID, nil
	}
	// Only generate new ID if key genuinely doesn't exist
	if !errors.Is(err, ErrSystemInfoNotFound) {
		return "", oops.With("operation", "check existing game_id").Wrap(err)
	}

	// Generate new game_id
	gameID = core.NewULID().String()
	if err := s.SetSystemInfo(ctx, "game_id", gameID); err != nil {
		return "", err
	}
	return gameID, nil
}

// streamToChannel converts a stream name to a PostgreSQL notification channel name.
// Replaces colons and hyphens with underscores since PG channel names must be valid identifiers.
func streamToChannel(stream string) string {
	s := strings.ReplaceAll(stream, ":", "_")
	s = strings.ReplaceAll(s, "-", "_")
	return "events_" + s
}

// Subscribe starts listening for events on the given stream via PostgreSQL LISTEN/NOTIFY.
// Returns a channel of event IDs and an error channel. The caller should use Replay()
// to fetch full events by ID. Channels are closed when context is cancelled.
func (s *PostgresEventStore) Subscribe(ctx context.Context, stream string) (eventCh <-chan ulid.ULID, errCh <-chan error, err error) {
	// Create a dedicated connection for LISTEN (can't use pooled connections)
	connector := s.connector
	if connector == nil {
		connector = defaultConnector
	}
	conn, err := connector(ctx, s.dsn)
	if err != nil {
		return nil, nil, oops.With("operation", "connect for subscription").With("stream", stream).Wrap(err)
	}

	channel := streamToChannel(stream)

	// Start listening on the channel
	// Use pgx.Identifier to safely quote the channel name, preventing SQL injection
	_, err = conn.Exec(ctx, "LISTEN "+pgx.Identifier{channel}.Sanitize())
	if err != nil {
		_ = conn.Close(ctx) //nolint:errcheck // cleanup on error path
		return nil, nil, oops.With("operation", "listen").With("channel", channel).Wrap(err)
	}

	events := make(chan ulid.ULID, 100)
	errs := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errs)
		defer func() { _ = conn.Close(context.Background()) }() //nolint:errcheck // cleanup in goroutine

		for {
			notification, err := conn.WaitForNotification(ctx)
			if err != nil {
				// Context cancelled is normal shutdown
				if ctx.Err() != nil {
					return
				}
				errs <- oops.With("operation", "wait for notification").With("stream", stream).Wrap(err)
				return
			}

			// Parse event ID from notification payload
			eventID, err := ulid.Parse(notification.Payload)
			if err != nil {
				errs <- oops.With("operation", "parse event ID from notification").With("payload", notification.Payload).Wrap(err)
				return
			}

			select {
			case events <- eventID:
			case <-ctx.Done():
				return
			}
		}
	}()

	return events, errs, nil
}
