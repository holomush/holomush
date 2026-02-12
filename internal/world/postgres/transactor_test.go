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
	tx := postgres.NewTransactor(testPool)

	locID := "01TESTTXCOMMIT00000000000"

	err := tx.InTransaction(ctx, func(txCtx context.Context) error {
		_, err := testPool.Exec(txCtx,
			`INSERT INTO locations (id, name) VALUES ($1, $2)`, locID, "commit-test")
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
	tx := postgres.NewTransactor(testPool)

	locID := "01TESTTXROLLBK00000000000"

	err := tx.InTransaction(ctx, func(txCtx context.Context) error {
		_, err := testPool.Exec(txCtx,
			`INSERT INTO locations (id, name) VALUES ($1, $2)`, locID, "rollback-test")
		if err != nil {
			return err
		}
		return errors.New("force rollback")
	})
	require.Error(t, err)

	err = testPool.QueryRow(ctx, `SELECT name FROM locations WHERE id = $1`, locID).Scan(new(string))
	assert.ErrorIs(t, err, pgx.ErrNoRows)
}
