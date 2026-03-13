// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"
)

// Transactor implements world.Transactor using a pgxpool connection pool.
// It stores the active pgx.Tx in context so that transaction-aware repository
// methods (Delete, DeleteByParent) participate in the same transaction.
type Transactor struct {
	pool *pgxpool.Pool
}

// NewTransactor creates a Transactor backed by the given connection pool.
func NewTransactor(pool *pgxpool.Pool) *Transactor {
	return &Transactor{pool: pool}
}

// InTransaction begins a transaction, stores it in context, and calls fn.
// If fn returns nil, the transaction is committed. Otherwise it is rolled back.
func (t *Transactor) InTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := t.pool.Begin(ctx)
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
