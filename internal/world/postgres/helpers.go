// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// querier is an interface that abstracts query execution for both *pgxpool.Pool and pgx.Tx.
// This allows helper methods to work within or outside of transactions.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
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

// parseOptionalULID parses an optional ULID string pointer into a ULID pointer.
// Returns nil if the input is nil. Wraps parse errors with the field name for context.
func parseOptionalULID(strPtr *string, fieldName string) (*ulid.ULID, error) {
	if strPtr == nil {
		return nil, nil
	}
	id, err := ulid.Parse(*strPtr)
	if err != nil {
		return nil, oops.With("operation", "parse "+fieldName).With(fieldName, *strPtr).Wrap(err)
	}
	return &id, nil
}
