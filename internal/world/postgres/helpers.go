// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"
	"encoding/json"
	"strings"

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

// ulidsToStrings converts a slice of ULIDs to a slice of strings.
// Returns nil for an empty input slice.
func ulidsToStrings(ids []ulid.ULID) []string {
	if len(ids) == 0 {
		return nil
	}
	strs := make([]string, len(ids))
	for i, id := range ids {
		strs[i] = id.String()
	}
	return strs
}

// stringsToULIDs parses a slice of strings into a slice of ULIDs.
// Returns nil for an empty input slice. Trims whitespace from each string.
func stringsToULIDs(strs []string, fieldName string) ([]ulid.ULID, error) {
	if len(strs) == 0 {
		return nil, nil
	}
	ids := make([]ulid.ULID, 0, len(strs))
	for _, s := range strs {
		trimmed := strings.TrimSpace(s)
		id, err := ulid.Parse(trimmed)
		if err != nil {
			return nil, oops.With("operation", "parse "+fieldName+" ulid").
				With("value", trimmed).
				Wrap(err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// nullableString returns a pointer to s, or nil if s is empty.
func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// marshalLockData marshals a map to JSON bytes.
// Returns nil for an empty map.
func marshalLockData(data map[string]any) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(data)
	if err != nil {
		return nil, oops.With("operation", "marshal lock data").Wrap(err)
	}
	return b, nil
}

// unmarshalLockData unmarshals JSON bytes into a map.
// Returns nil for empty input.
func unmarshalLockData(data []byte) (map[string]any, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, oops.With("operation", "unmarshal lock data").Wrap(err)
	}
	return result, nil
}
