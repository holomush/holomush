// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
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

// InTransaction runs fn within a transaction using re-entrant semantics.
//
// If an ambient transaction is already present in ctx, fn is executed within it
// and the outermost call owns commit/rollback — no second pool.Begin is issued.
// This means a repository method that itself calls InTransaction (or withTx)
// while its caller already holds a transaction reuses that transaction and its
// connection, rather than nesting/escaping. Only when no ambient transaction is
// present does InTransaction begin a new one, commit on a nil return, and roll
// back on error — behavior-identical to the previous non-reentrant version.
func (t *Transactor) InTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return withTx(ctx, t.pool, fn)
}
