// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
)

func TestPostgresEventStore_GetSystemInfo(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantValue string
		wantErr   bool
		errIs     error
		errMsg    string
	}{
		{
			name: "successful get",
			key:  "game_id",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"value"}).AddRow("test-value-123")
				mock.ExpectQuery(`SELECT value FROM holomush_system_info WHERE key = \$1`).
					WithArgs("game_id").
					WillReturnRows(rows)
			},
			wantValue: "test-value-123",
			wantErr:   false,
		},
		{
			name: "key not found",
			key:  "nonexistent",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT value FROM holomush_system_info WHERE key = \$1`).
					WithArgs("nonexistent").
					WillReturnError(pgx.ErrNoRows)
			},
			wantErr: true,
			errIs:   ErrSystemInfoNotFound,
		},
		{
			name: "database error",
			key:  "game_id",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT value FROM holomush_system_info WHERE key = \$1`).
					WithArgs("game_id").
					WillReturnError(errors.New("connection timeout"))
			},
			wantErr: true,
			errMsg:  "connection timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err, "failed to create mock")
			defer mock.Close()

			tt.setupMock(mock)

			store := &PostgresEventStore{pool: mock}
			value, err := store.GetSystemInfo(context.Background(), tt.key)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errIs != nil {
					assert.ErrorIs(t, err, tt.errIs)
				}
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantValue, value)
			}

			assert.NoError(t, mock.ExpectationsWereMet(), "unfulfilled expectations")
		})
	}
}

func TestPostgresEventStore_SetSystemInfo(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		value     string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errMsg    string
	}{
		{
			name:  "successful insert",
			key:   "game_id",
			value: "test-game-id",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO holomush_system_info`).
					WithArgs("game_id", "test-game-id").
					WillReturnResult(pgxmock.NewResult("INSERT", 1))
			},
			wantErr: false,
		},
		{
			name:  "successful upsert (update existing)",
			key:   "game_id",
			value: "updated-game-id",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO holomush_system_info`).
					WithArgs("game_id", "updated-game-id").
					WillReturnResult(pgxmock.NewResult("UPDATE", 1))
			},
			wantErr: false,
		},
		{
			name:  "database error",
			key:   "game_id",
			value: "test-value",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO holomush_system_info`).
					WithArgs("game_id", "test-value").
					WillReturnError(errors.New("disk full"))
			},
			wantErr: true,
			errMsg:  "disk full",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err, "failed to create mock")
			defer mock.Close()

			tt.setupMock(mock)

			store := &PostgresEventStore{pool: mock}
			err = store.SetSystemInfo(context.Background(), tt.key, tt.value)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}

			assert.NoError(t, mock.ExpectationsWereMet(), "unfulfilled expectations")
		})
	}
}

func TestPostgresEventStore_InitGameID(t *testing.T) {
	existingID := core.NewULID().String()

	tests := []struct {
		name      string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errMsg    string
		checkID   func(t *testing.T, id string)
	}{
		{
			name: "returns existing game_id",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"value"}).AddRow(existingID)
				mock.ExpectQuery(`SELECT value FROM holomush_system_info WHERE key = \$1`).
					WithArgs("game_id").
					WillReturnRows(rows)
			},
			wantErr: false,
			checkID: func(t *testing.T, id string) {
				t.Helper()
				assert.Equal(t, existingID, id)
			},
		},
		{
			name: "generates new game_id when not found",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				// GetSystemInfo returns not found
				mock.ExpectQuery(`SELECT value FROM holomush_system_info WHERE key = \$1`).
					WithArgs("game_id").
					WillReturnError(pgx.ErrNoRows)
				// SetSystemInfo succeeds
				mock.ExpectExec(`INSERT INTO holomush_system_info`).
					WithArgs("game_id", pgxmock.AnyArg()).
					WillReturnResult(pgxmock.NewResult("INSERT", 1))
			},
			wantErr: false,
			checkID: func(t *testing.T, id string) {
				t.Helper()
				assert.NotEmpty(t, id)
				assert.Len(t, id, 26, "ULID should be 26 characters")
			},
		},
		{
			name: "returns error on GetSystemInfo failure (non-NotFound)",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT value FROM holomush_system_info WHERE key = \$1`).
					WithArgs("game_id").
					WillReturnError(errors.New("connection refused"))
			},
			wantErr: true,
			errMsg:  "connection refused",
		},
		{
			name: "returns error on SetSystemInfo failure",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT value FROM holomush_system_info WHERE key = \$1`).
					WithArgs("game_id").
					WillReturnError(pgx.ErrNoRows)
				mock.ExpectExec(`INSERT INTO holomush_system_info`).
					WithArgs("game_id", pgxmock.AnyArg()).
					WillReturnError(errors.New("write failed"))
			},
			wantErr: true,
			errMsg:  "write failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err, "failed to create mock")
			defer mock.Close()

			tt.setupMock(mock)

			store := &PostgresEventStore{pool: mock}
			id, err := store.InitGameID(context.Background())

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				if tt.checkID != nil {
					tt.checkID(t, id)
				}
			}

			assert.NoError(t, mock.ExpectationsWereMet(), "unfulfilled expectations")
		})
	}
}

func TestPostgresEventStoreCloseDoesNotPanic(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err, "failed to create mock")

	store := &PostgresEventStore{pool: mock}

	// Close delegates to pool.Close() which is a void method.
	// Verify it completes without panic.
	require.NotPanics(t, func() {
		store.Close()
	})
}

func TestErrSystemInfoNotFoundHasExpectedMessage(t *testing.T) {
	assert.Equal(t, "system info key not found", ErrSystemInfoNotFound.Error())
}
