// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package bootstrap

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/samber/oops"
)

// MetadataStore provides access to bootstrap state.
type MetadataStore interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, value string) error
	Delete(ctx context.Context, key string) error
}

// poolIface defines the pgxpool methods used by PostgresMetadataStore.
// This interface enables testing with mocks.
type poolIface interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// PostgresMetadataStore implements MetadataStore backed by bootstrap_metadata table.
type PostgresMetadataStore struct {
	pool poolIface
}

// Compile-time check.
var _ MetadataStore = (*PostgresMetadataStore)(nil)

// NewPostgresMetadataStore creates a new PostgresMetadataStore.
func NewPostgresMetadataStore(pool poolIface) *PostgresMetadataStore {
	return &PostgresMetadataStore{pool: pool}
}

// Get retrieves a value by key. Returns (value, true, nil) if found, ("", false, nil) if not found.
func (s *PostgresMetadataStore) Get(ctx context.Context, key string) (value string, found bool, err error) {
	err = s.pool.QueryRow(ctx, "SELECT value FROM bootstrap_metadata WHERE key = $1", key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, oops.With("key", key).Wrap(err)
	}
	return value, true, nil
}

// Set stores a key-value pair. If the key already exists, it is updated.
func (s *PostgresMetadataStore) Set(ctx context.Context, key, value string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO bootstrap_metadata (key, value, updated_at) VALUES ($1, $2, NOW())
		 ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = NOW()`, key, value)
	if err != nil {
		return oops.With("key", key).Wrap(err)
	}
	return nil
}

// Delete removes a key-value pair by key.
func (s *PostgresMetadataStore) Delete(ctx context.Context, key string) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM bootstrap_metadata WHERE key = $1", key)
	if err != nil {
		return oops.With("key", key).Wrap(err)
	}
	return nil
}
