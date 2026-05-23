// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package store provides storage implementations.
package store

import (
	"context"
	"errors"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/core"
)

// ErrSystemInfoNotFound is returned when a system info key doesn't exist.
var ErrSystemInfoNotFound = errors.New("system info key not found")

// poolIface defines the pgxpool methods used by PostgresEventStore.
// This interface enables testing with mocks.
type poolIface interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Close()
}

// PostgresEventStore provides system-info and game-ID persistence backed by PostgreSQL.
// Event append/replay is handled by the JetStream event bus (F7+).
type PostgresEventStore struct {
	pool poolIface
}

// NewPostgresEventStore creates a new PostgreSQL store.
func NewPostgresEventStore(ctx context.Context, dsn string) (*PostgresEventStore, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, oops.With("operation", "parse database config").Wrap(err)
	}
	cfg.ConnConfig.Tracer = otelpgx.NewTracer()

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, oops.With("operation", "connect to database").Wrap(err)
	}
	return &PostgresEventStore{pool: pool}, nil
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
		 ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT`,
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
