// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

// TestIsUniqueViolation_Detects23505 verifies the helper recognises the
// PostgreSQL unique-constraint violation SQLSTATE used by
// dek.Manager.GetOrCreate to detect concurrent INSERT races.
func TestIsUniqueViolation_Detects23505(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23505"}
	assert.True(t, dek.IsUniqueViolation(pgErr))
}

// TestIsUniqueViolation_NonViolationCodeReturnsFalse covers the path
// where the error is a pgconn.PgError but a different code (e.g.,
// 23502 not-null violation, 42P01 undefined-table).
func TestIsUniqueViolation_NonViolationCodeReturnsFalse(t *testing.T) {
	for _, code := range []string{"23502", "42P01", "08006", "00000"} {
		t.Run("code_"+code, func(t *testing.T) {
			pgErr := &pgconn.PgError{Code: code}
			assert.False(t, dek.IsUniqueViolation(pgErr))
		})
	}
}

// TestIsUniqueViolation_NonPgErrorReturnsFalse covers the path where
// the error chain doesn't unwrap to pgconn.PgError (any other error
// type or a plain string error).
func TestIsUniqueViolation_NonPgErrorReturnsFalse(t *testing.T) {
	assert.False(t, dek.IsUniqueViolation(errors.New("not a pg error")))
	assert.False(t, dek.IsUniqueViolation(nil))
}

// TestIsUniqueViolation_WrappedPgErrorReturnsTrue covers errors.As
// traversal through a wrapping chain.
func TestIsUniqueViolation_WrappedPgErrorReturnsTrue(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23505"}
	wrapped := errors.Join(errors.New("outer context"), pgErr)
	assert.True(t, dek.IsUniqueViolation(wrapped))
}
