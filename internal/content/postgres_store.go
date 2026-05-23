// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package content

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/pgnanos"
)

// PostgresStore implements Store using PostgreSQL.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore creates a new PostgresStore backed by the given pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// Put creates or updates a content item. For text/* and application/json content
// types the search_vector column is populated; all others set it to NULL.
func (s *PostgresStore) Put(ctx context.Context, item *Item) error {
	if item == nil {
		return oops.New("content item must not be nil")
	}
	meta, err := json.Marshal(item.Metadata)
	if err != nil {
		return oops.With("key", item.Key).With("operation", "marshal metadata").Wrap(err)
	}

	var query string
	if isSearchable(item.ContentType) {
		query = `
INSERT INTO content_items (key, content_type, body, metadata, search_vector, updated_at)
VALUES ($1, $2, $3, $4, to_tsvector('english', convert_from($3, 'UTF8')), (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
ON CONFLICT (key) DO UPDATE SET
    content_type   = EXCLUDED.content_type,
    body           = EXCLUDED.body,
    metadata       = EXCLUDED.metadata,
    search_vector  = EXCLUDED.search_vector,
    updated_at     = (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT`
	} else {
		query = `
INSERT INTO content_items (key, content_type, body, metadata, search_vector, updated_at)
VALUES ($1, $2, $3, $4, NULL, (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT)
ON CONFLICT (key) DO UPDATE SET
    content_type   = EXCLUDED.content_type,
    body           = EXCLUDED.body,
    metadata       = EXCLUDED.metadata,
    search_vector  = NULL,
    updated_at     = (EXTRACT(EPOCH FROM NOW()) * 1e9)::BIGINT`
	}

	_, err = s.pool.Exec(ctx, query, item.Key, item.ContentType, item.Body, meta)
	if err != nil {
		return oops.With("key", item.Key).With("operation", "put content item").Wrap(err)
	}
	return nil
}

// Get retrieves a content item by key. Returns nil, nil when not found.
func (s *PostgresStore) Get(ctx context.Context, key string) (*Item, error) {
	row := s.pool.QueryRow(
		ctx,
		`SELECT key, content_type, body, metadata, updated_at
		   FROM content_items
		  WHERE key = $1`,
		key,
	)

	item, err := scanItem(row)
	if err != nil {
		return nil, oops.With("key", key).With("operation", "get content item").Wrap(err)
	}
	return item, nil
}

// escapeLike escapes LIKE special characters in s so it can be used as a
// literal prefix in a LIKE pattern (with ESCAPE '\').
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// List returns content items whose key starts with prefix, with optional
// cursor-based pagination.
func (s *PostgresStore) List(ctx context.Context, prefix string, opts ListOptions) (*ListResult, error) {
	args := []any{escapeLike(prefix) + "%"}
	query := `SELECT key, content_type, body, metadata, updated_at
	            FROM content_items
	           WHERE key LIKE $1 ESCAPE '\'`

	if opts.Cursor != "" {
		args = append(args, opts.Cursor)
		query += ` AND key > $2`
	}
	query += ` ORDER BY key`

	limit := opts.Limit
	if limit > 0 {
		args = append(args, limit+1)
		query += ` LIMIT $` + itoa(len(args))
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, oops.With("prefix", prefix).With("operation", "list content items").Wrap(err)
	}
	defer rows.Close()

	var items []*Item
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, oops.With("prefix", prefix).With("operation", "scan content item").Wrap(err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.With("prefix", prefix).With("operation", "iterate content items").Wrap(err)
	}

	result := &ListResult{Items: items}
	if limit > 0 && len(items) > limit {
		result.NextCursor = items[limit-1].Key
		result.Items = items[:limit]
	}
	return result, nil
}

// Delete removes a content item. It is a no-op when the key does not exist.
func (s *PostgresStore) Delete(ctx context.Context, key string) error {
	_, err := s.pool.Exec(
		ctx,
		`DELETE FROM content_items WHERE key = $1`,
		key,
	)
	if err != nil {
		return oops.With("key", key).With("operation", "delete content item").Wrap(err)
	}
	return nil
}

// isSearchable returns true when the content type warrants full-text indexing.
func isSearchable(contentType string) bool {
	return strings.HasPrefix(contentType, "text/") || contentType == "application/json"
}

// scanner is satisfied by both pgx.Row and pgx.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// scanItem reads a content item row from s. Returns nil, nil on pgx.ErrNoRows.
func scanItem(s scanner) (*Item, error) {
	var (
		key         string
		contentType string
		body        []byte
		metaRaw     []byte
		updatedAt   pgnanos.Time
	)

	err := s.Scan(&key, &contentType, &body, &metaRaw, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, oops.With("operation", "scan row").Wrap(err)
	}

	var meta map[string]string
	if len(metaRaw) > 0 {
		if err := json.Unmarshal(metaRaw, &meta); err != nil {
			return nil, oops.With("operation", "unmarshal metadata").Wrap(err)
		}
	}

	return &Item{
		Key:         key,
		ContentType: contentType,
		Body:        body,
		Metadata:    meta,
		UpdatedAt:   updatedAt.Time(),
	}, nil
}

// itoa converts a small integer to its decimal string representation without
// importing strconv for a single use.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 3)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}
