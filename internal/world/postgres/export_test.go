// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// TxFromContext exports txFromContext for testing in the external test package.
func TxFromContext(ctx context.Context) pgx.Tx {
	return txFromContext(ctx)
}

// WithTxForTest exports withTx for testing in the external test package.
func WithTxForTest(ctx context.Context, pool txBeginner, fn func(ctx context.Context) error) error {
	return withTx(ctx, pool, fn)
}

// ExecerFromCtxForTest exports execerFromCtx for testing in the external test package.
func ExecerFromCtxForTest(ctx context.Context, pool execer) execer {
	return execerFromCtx(ctx, pool)
}
