// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/oklog/ulid/v2"
)

// querier is an interface that abstracts query execution for both *pgxpool.Pool and pgx.Tx.
// This allows helper methods to work within or outside of transactions.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// ulidToStringPtr converts a ULID pointer to a string pointer for SQL parameters.
// Returns nil if the input is nil.
func ulidToStringPtr(id *ulid.ULID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}
