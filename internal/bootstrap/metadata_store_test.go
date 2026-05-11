// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package bootstrap

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostgresMetadataStore_CompileTimeCheck(_ *testing.T) {
	var _ MetadataStore = (*PostgresMetadataStore)(nil)
}

func TestPostgresMetadataStore_Get(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantValue string
		wantFound bool
		wantErr   bool
		errMsg    string
	}{
		{
			name: "found",
			key:  "bootstrap_complete",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"value"}).AddRow("true")
				mock.ExpectQuery(`SELECT value FROM setting_bootstrap_state WHERE key = \$1`).
					WithArgs("bootstrap_complete").
					WillReturnRows(rows)
			},
			wantValue: "true",
			wantFound: true,
		},
		{
			name: "not found",
			key:  "nonexistent_key",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT value FROM setting_bootstrap_state WHERE key = \$1`).
					WithArgs("nonexistent_key").
					WillReturnError(pgx.ErrNoRows)
			},
			wantValue: "",
			wantFound: false,
		},
		{
			name: "database error",
			key:  "bootstrap_complete",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT value FROM setting_bootstrap_state WHERE key = \$1`).
					WithArgs("bootstrap_complete").
					WillReturnError(errors.New("connection lost"))
			},
			wantErr: true,
			errMsg:  "connection lost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			s := NewPostgresMetadataStore(mock)
			value, found, err := s.Get(context.Background(), tt.key)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantValue, value)
				assert.Equal(t, tt.wantFound, found)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPostgresMetadataStore_Set(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		value     string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errMsg    string
	}{
		{
			name:  "insert new key",
			key:   "bootstrap_complete",
			value: "true",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO setting_bootstrap_state`).
					WithArgs("bootstrap_complete", "true").
					WillReturnResult(pgxmock.NewResult("INSERT", 1))
			},
		},
		{
			name:  "update existing key",
			key:   "bootstrap_complete",
			value: "false",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO setting_bootstrap_state`).
					WithArgs("bootstrap_complete", "false").
					WillReturnResult(pgxmock.NewResult("INSERT", 0))
			},
		},
		{
			name:  "database error",
			key:   "bootstrap_complete",
			value: "true",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO setting_bootstrap_state`).
					WithArgs("bootstrap_complete", "true").
					WillReturnError(errors.New("connection lost"))
			},
			wantErr: true,
			errMsg:  "connection lost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			s := NewPostgresMetadataStore(mock)
			err = s.Set(context.Background(), tt.key, tt.value)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPostgresMetadataStore_Delete(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "delete existing key",
			key:  "bootstrap_complete",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`DELETE FROM setting_bootstrap_state WHERE key = \$1`).
					WithArgs("bootstrap_complete").
					WillReturnResult(pgxmock.NewResult("DELETE", 1))
			},
		},
		{
			name: "delete nonexistent key",
			key:  "nonexistent_key",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`DELETE FROM setting_bootstrap_state WHERE key = \$1`).
					WithArgs("nonexistent_key").
					WillReturnResult(pgxmock.NewResult("DELETE", 0))
			},
		},
		{
			name: "database error",
			key:  "bootstrap_complete",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`DELETE FROM setting_bootstrap_state WHERE key = \$1`).
					WithArgs("bootstrap_complete").
					WillReturnError(errors.New("connection lost"))
			},
			wantErr: true,
			errMsg:  "connection lost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			s := NewPostgresMetadataStore(mock)
			err = s.Delete(context.Background(), tt.key)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
