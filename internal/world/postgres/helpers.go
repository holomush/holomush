// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/wmodel"
)

// querier is an interface that abstracts query execution for both *pgxpool.Pool and pgx.Tx.
// This allows helper methods to work within or outside of transactions.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// execer abstracts Exec for both *pgxpool.Pool and pgx.Tx.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// txKey is the context key for an active pgx.Tx.
type txKey struct{}

// txFromContext returns the active pgx.Tx stored in context by InTransaction,
// or nil if no transaction is active.
func txFromContext(ctx context.Context) pgx.Tx {
	tx, ok := ctx.Value(txKey{}).(pgx.Tx)
	if !ok {
		return nil
	}
	return tx
}

// execerFromCtx returns the active transaction from context, or falls back to the pool.
func execerFromCtx(ctx context.Context, pool execer) execer {
	if tx := txFromContext(ctx); tx != nil {
		return tx
	}
	return pool
}

// querierFromCtx returns the active transaction from context (for a QueryRow that
// must observe/participate in the ambient mutation transaction, e.g. an
// INSERT ... RETURNING or a locked follow-up read), or falls back to the pool.
// Both *pgxpool.Pool and pgx.Tx satisfy querier, so a write's RETURNING clause
// enrolls in the caller's transaction rather than escaping it (Pitfall 1).
func querierFromCtx(ctx context.Context, pool querier) querier {
	if tx := txFromContext(ctx); tx != nil {
		return tx
	}
	return pool
}

// classifyCASZeroRow runs a locked follow-up read to classify why a
// version-predicated CAS write/delete affected zero rows. It is the MODEL-03
// zero-row classifier: exactly TWO outcomes, never three (round-5 Codex MEDIUM —
// the ID-only APIs carry no caller existence token, so a concurrent delete that
// already committed is observed as an absent row and correctly reported
// not-found, not a distinct third outcome):
//
//   - an existing row (its version has moved past the caller's expectedVersion)
//     → WORLD_CONCURRENT_EDIT wrapping world.ErrConcurrentEdit;
//   - an absent row → the caller's notFound sentinel.
//
// query MUST be a `SELECT version FROM <table> WHERE id = $1 FOR UPDATE` bound to
// id. The caller MUST pass the tx-scoped querier (from within withTx) so the read
// runs on the SAME connection as the CAS — under a pool constrained to size 1 it
// reuses the caller's connection and cannot deadlock on connection acquisition
// (finding 14).
func classifyCASZeroRow(ctx context.Context, q querier, query string, id ulid.ULID, notFound error) error {
	var currentVersion int
	err := q.QueryRow(ctx, query, id.String()).Scan(&currentVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return notFound
	}
	if err != nil {
		return oops.With("operation", "classify cas zero row").With("id", id.String()).Wrap(err)
	}
	return oops.Code(world.CodeConcurrentEdit).
		With("id", id.String()).
		With("current_version", currentVersion).
		Wrap(world.ErrConcurrentEdit)
}

// txBeginner abstracts Begin for a connection pool (satisfied by *pgxpool.Pool).
type txBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// withTx runs fn inside a transaction with re-entrant semantics.
//
// If an ambient transaction is already present in ctx (stashed by an outer
// withTx / Transactor.InTransaction), fn is executed within it and the OUTERMOST
// caller owns commit/rollback — no second Begin is issued and no commit/rollback
// happens here. This lets a repository write method enroll in an ambient mutation
// transaction so it commits atomically with the caller's other statements (e.g.
// the outbox row) and shares a single connection (so a locked follow-up read in
// the same tx reuses the caller's connection).
//
// When no ambient transaction is present, withTx begins a new transaction, stashes
// it via txKey{}, commits on a nil return, and rolls back on error — the standalone
// repo-method path, behavior-identical to the previous per-method pool.Begin.
func withTx(ctx context.Context, pool txBeginner, fn func(ctx context.Context) error) error {
	if txFromContext(ctx) != nil {
		return fn(ctx)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return oops.Code("TX_BEGIN_FAILED").Wrap(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	txCtx := context.WithValue(ctx, txKey{}, tx)
	if err := fn(txCtx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return oops.Code("TX_COMMIT_FAILED").Wrap(err)
	}
	return nil
}

// primaryDeltaVersioned builds a primary-only MutationDelta carrying the
// before/after optimistic-concurrency versions of the row transition (MODEL-03,
// finding 12). before is the version guard read before the write (0 for a
// create); after is the committed version (0 for a tombstone).
func primaryDeltaVersioned(t wmodel.AggregateType, id ulid.ULID, tombstone bool, before, after int) *wmodel.MutationDelta {
	return &wmodel.MutationDelta{
		Primary: wmodel.AffectedAggregate{
			Type:          t,
			ID:            id,
			Tombstone:     tombstone,
			BeforeVersion: before,
			AfterVersion:  after,
		},
	}
}

// maxCTERecursionDepth limits recursion in CTEs to prevent infinite loops.
// This is a safety guard; actual nesting is limited by business rules (maxNestingDepth).
const maxCTERecursionDepth = 100

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
