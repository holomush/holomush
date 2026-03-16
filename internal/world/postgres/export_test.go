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
