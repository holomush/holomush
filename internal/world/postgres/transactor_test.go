// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world/postgres"
)

func TestTransactor_InTransaction_CommitsOnSuccess(t *testing.T) {
	ctx := context.Background()
	tr := postgres.NewTransactor(testPool)

	locID := "01TESTTXCOMMIT00000000000"

	err := tr.InTransaction(ctx, func(txCtx context.Context) error {
		tx := postgres.TxFromContext(txCtx)
		require.NotNil(t, tx, "expected transaction in context")
		_, err := tx.Exec(txCtx,
			`INSERT INTO locations (id, name, description) VALUES ($1, $2, $3)`, locID, "commit-test", "A test location")
		return err
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, locID)
	})

	var name string
	err = testPool.QueryRow(ctx, `SELECT name FROM locations WHERE id = $1`, locID).Scan(&name)
	require.NoError(t, err)
	assert.Equal(t, "commit-test", name)
}

func TestTransactor_InTransaction_RollsBackOnError(t *testing.T) {
	ctx := context.Background()
	tr := postgres.NewTransactor(testPool)

	locID := "01TESTTXROLLBK00000000000"

	err := tr.InTransaction(ctx, func(txCtx context.Context) error {
		tx := postgres.TxFromContext(txCtx)
		require.NotNil(t, tx, "expected transaction in context")
		_, err := tx.Exec(txCtx,
			`INSERT INTO locations (id, name, description) VALUES ($1, $2, $3)`, locID, "rollback-test", "A test location")
		if err != nil {
			return err
		}
		return errors.New("force rollback")
	})
	require.Error(t, err)

	err = testPool.QueryRow(ctx, `SELECT name FROM locations WHERE id = $1`, locID).Scan(new(string))
	assert.ErrorIs(t, err, pgx.ErrNoRows)
}

// TestTransactor_InTransaction_ReusesAmbientTx proves re-entrancy: a nested
// InTransaction call executes within the OUTER transaction rather than beginning
// a second one. If the nested call opened + committed its own transaction, the
// nested write would persist even when the outer rolls back. We assert the
// nested write does NOT persist after the outer rolls back — proving the shared
// transaction and shared connection.
func TestTransactor_InTransaction_ReusesAmbientTx(t *testing.T) {
	ctx := context.Background()
	tr := postgres.NewTransactor(testPool)

	outerID := "01TESTTXREENTOUTER0000000"
	innerID := "01TESTTXREENTINNER0000000"

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = ANY($1)`, []string{outerID, innerID})
	})

	var innerTxSameAsOuter bool
	err := tr.InTransaction(ctx, func(txCtx context.Context) error {
		outerTx := postgres.TxFromContext(txCtx)
		require.NotNil(t, outerTx, "expected outer transaction in context")
		if _, err := outerTx.Exec(txCtx,
			`INSERT INTO locations (id, name, description) VALUES ($1, $2, $3)`,
			outerID, "reent-outer", "outer"); err != nil {
			return err
		}

		// Nested call must reuse the ambient tx (no second Begin, no commit).
		if err := tr.InTransaction(txCtx, func(innerCtx context.Context) error {
			innerTx := postgres.TxFromContext(innerCtx)
			require.NotNil(t, innerTx, "expected inner transaction in context")
			innerTxSameAsOuter = innerTx == outerTx
			_, err := innerTx.Exec(innerCtx,
				`INSERT INTO locations (id, name, description) VALUES ($1, $2, $3)`,
				innerID, "reent-inner", "inner")
			return err
		}); err != nil {
			return err
		}

		// Force the OUTER (owning) transaction to roll back.
		return errors.New("force outer rollback")
	})
	require.Error(t, err)
	assert.True(t, innerTxSameAsOuter, "nested InTransaction must reuse the ambient tx (same *pgx.Tx)")

	// Neither write persists: the nested call did NOT independently commit.
	err = testPool.QueryRow(ctx, `SELECT name FROM locations WHERE id = $1`, innerID).Scan(new(string))
	assert.ErrorIs(t, err, pgx.ErrNoRows, "nested write must roll back with the outer tx")
	err = testPool.QueryRow(ctx, `SELECT name FROM locations WHERE id = $1`, outerID).Scan(new(string))
	assert.ErrorIs(t, err, pgx.ErrNoRows, "outer write must roll back")
}

// TestTransactor_InTransaction_NestedErrorPropagates proves that an error from a
// nested InTransaction surfaces to the outer call, which owns the rollback.
func TestTransactor_InTransaction_NestedErrorPropagates(t *testing.T) {
	ctx := context.Background()
	tr := postgres.NewTransactor(testPool)

	outerID := "01TESTTXNESTEDERR00000000"
	sentinel := errors.New("nested failure")

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, outerID)
	})

	err := tr.InTransaction(ctx, func(txCtx context.Context) error {
		tx := postgres.TxFromContext(txCtx)
		require.NotNil(t, tx)
		if _, err := tx.Exec(txCtx,
			`INSERT INTO locations (id, name, description) VALUES ($1, $2, $3)`,
			outerID, "nested-err", "outer"); err != nil {
			return err
		}
		return tr.InTransaction(txCtx, func(innerCtx context.Context) error {
			return sentinel
		})
	})
	require.ErrorIs(t, err, sentinel, "nested error must propagate to the outer caller")

	// Outer write rolled back because the nested error propagated.
	err = testPool.QueryRow(ctx, `SELECT name FROM locations WHERE id = $1`, outerID).Scan(new(string))
	assert.ErrorIs(t, err, pgx.ErrNoRows)
}

// TestWithTx_ReusesAmbientTx proves the package-level withTx seam is re-entrant:
// when an ambient tx is present it reuses it (no independent commit), so a
// withTx-based repo method enrolls in the caller's transaction.
func TestWithTx_ReusesAmbientTx(t *testing.T) {
	ctx := context.Background()
	tr := postgres.NewTransactor(testPool)

	id := "01TESTWITHTXREENTRANT0000"

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, id)
	})

	err := tr.InTransaction(ctx, func(txCtx context.Context) error {
		// WithTxForTest mirrors a repo method using the withTx seam: it must
		// reuse the ambient tx and NOT commit independently.
		if err := postgres.WithTxForTest(txCtx, testPool, func(innerCtx context.Context) error {
			_, err := postgres.ExecerFromCtxForTest(innerCtx, testPool).Exec(innerCtx,
				`INSERT INTO locations (id, name, description) VALUES ($1, $2, $3)`,
				id, "withtx-reent", "inner")
			return err
		}); err != nil {
			return err
		}
		return errors.New("force rollback")
	})
	require.Error(t, err)

	err = testPool.QueryRow(ctx, `SELECT name FROM locations WHERE id = $1`, id).Scan(new(string))
	assert.ErrorIs(t, err, pgx.ErrNoRows, "withTx write must roll back with the ambient tx")
}

// TestWithTx_TopLevelCommits proves withTx begins-and-commits its own transaction
// when no ambient tx is present (standalone repo-method call path).
func TestWithTx_TopLevelCommits(t *testing.T) {
	ctx := context.Background()

	id := "01TESTWITHTXTOPLEVEL00000"

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, id)
	})

	err := postgres.WithTxForTest(ctx, testPool, func(innerCtx context.Context) error {
		_, err := postgres.ExecerFromCtxForTest(innerCtx, testPool).Exec(innerCtx,
			`INSERT INTO locations (id, name, description) VALUES ($1, $2, $3)`,
			id, "withtx-top", "top")
		return err
	})
	require.NoError(t, err)

	var name string
	err = testPool.QueryRow(ctx, `SELECT name FROM locations WHERE id = $1`, id).Scan(&name)
	require.NoError(t, err)
	assert.Equal(t, "withtx-top", name)
}
