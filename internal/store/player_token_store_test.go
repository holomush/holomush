// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/oklog/ulid/v2"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/pkg/errutil"
)

// testPlayerToken builds a non-expired PlayerToken for use in tests.
func testPlayerToken() *auth.PlayerToken {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return &auth.PlayerToken{
		Token:     ulid.Make().String(),
		PlayerID:  core.NewULID(),
		CreatedAt: now,
		ExpiresAt: now.Add(time.Hour),
	}
}

// playerTokenColumns returns the column names for player_tokens SELECT queries.
func playerTokenColumns() []string {
	return []string{"token", "player_id", "created_at", "expires_at"}
}

// playerTokenRow creates a pgxmock row from a PlayerToken.
func playerTokenRow(pt *auth.PlayerToken) []any {
	return []any{pt.Token, pt.PlayerID.String(), pt.CreatedAt, pt.ExpiresAt}
}

func TestPostgresPlayerTokenStore_CompileTimeCheck(_ *testing.T) {
	var _ auth.PlayerTokenRepository = (*PostgresPlayerTokenStore)(nil)
}

func TestPostgresPlayerTokenStore_Create(t *testing.T) {
	pt := testPlayerToken()

	tests := []struct {
		name      string
		token     *auth.PlayerToken
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errMsg    string
	}{
		{
			name:  "happy path",
			token: pt,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO player_tokens`).
					WithArgs(pt.Token, pt.PlayerID.String(), pt.CreatedAt, pt.ExpiresAt).
					WillReturnResult(pgxmock.NewResult("INSERT", 1))
			},
		},
		{
			name:  "database error",
			token: pt,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO player_tokens`).
					WithArgs(pt.Token, pt.PlayerID.String(), pt.CreatedAt, pt.ExpiresAt).
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

			s := NewPostgresPlayerTokenStore(mock)
			err = s.Create(context.Background(), tt.token)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPostgresPlayerTokenStore_GetByToken(t *testing.T) {
	pt := testPlayerToken()

	// Build an already-expired token for the expiry test.
	expiredPT := &auth.PlayerToken{
		Token:     ulid.Make().String(),
		PlayerID:  core.NewULID(),
		CreatedAt: time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Microsecond),
		ExpiresAt: time.Now().UTC().Add(-1 * time.Hour).Truncate(time.Microsecond),
	}

	tests := []struct {
		name      string
		token     string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errCode   string
		errMsg    string
		check     func(t *testing.T, got *auth.PlayerToken)
	}{
		{
			name:  "happy path",
			token: pt.Token,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(playerTokenColumns()).AddRow(playerTokenRow(pt)...)
				mock.ExpectQuery(`SELECT .+ FROM player_tokens WHERE token = \$1`).
					WithArgs(pt.Token).
					WillReturnRows(rows)
			},
			check: func(t *testing.T, got *auth.PlayerToken) {
				t.Helper()
				assert.Equal(t, pt.Token, got.Token)
				assert.Equal(t, pt.PlayerID, got.PlayerID)
				assert.Equal(t, pt.ExpiresAt, got.ExpiresAt)
			},
		},
		{
			name:  "not found",
			token: "no-such-token",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT .+ FROM player_tokens WHERE token = \$1`).
					WithArgs("no-such-token").
					WillReturnError(pgx.ErrNoRows)
			},
			wantErr: true,
			errCode: "TOKEN_NOT_FOUND",
		},
		{
			name:  "expired token",
			token: expiredPT.Token,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(playerTokenColumns()).AddRow(playerTokenRow(expiredPT)...)
				mock.ExpectQuery(`SELECT .+ FROM player_tokens WHERE token = \$1`).
					WithArgs(expiredPT.Token).
					WillReturnRows(rows)
				// Expect the cleanup DELETE after detecting expiry.
				mock.ExpectExec(`DELETE FROM player_tokens WHERE token = \$1`).
					WithArgs(expiredPT.Token).
					WillReturnResult(pgxmock.NewResult("DELETE", 1))
			},
			wantErr: true,
			errCode: "TOKEN_EXPIRED",
		},
		{
			name:  "database error",
			token: pt.Token,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT .+ FROM player_tokens WHERE token = \$1`).
					WithArgs(pt.Token).
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

			s := NewPostgresPlayerTokenStore(mock)
			got, err := s.GetByToken(context.Background(), tt.token)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errCode != "" {
					errutil.AssertErrorCode(t, err, tt.errCode)
				}
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				if tt.check != nil {
					tt.check(t, got)
				}
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPostgresPlayerTokenStore_DeleteByToken(t *testing.T) {
	tokenVal := ulid.Make().String()

	tests := []struct {
		name      string
		token     string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errMsg    string
	}{
		{
			name:  "happy path",
			token: tokenVal,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`DELETE FROM player_tokens WHERE token = \$1`).
					WithArgs(tokenVal).
					WillReturnResult(pgxmock.NewResult("DELETE", 1))
			},
		},
		{
			name:  "database error",
			token: tokenVal,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`DELETE FROM player_tokens WHERE token = \$1`).
					WithArgs(tokenVal).
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

			s := NewPostgresPlayerTokenStore(mock)
			err = s.DeleteByToken(context.Background(), tt.token)

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

func TestPostgresPlayerTokenStore_DeleteByPlayer(t *testing.T) {
	playerID := core.NewULID()

	tests := []struct {
		name      string
		playerID  ulid.ULID
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errMsg    string
	}{
		{
			name:     "happy path",
			playerID: playerID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`DELETE FROM player_tokens WHERE player_id = \$1`).
					WithArgs(playerID.String()).
					WillReturnResult(pgxmock.NewResult("DELETE", 3))
			},
		},
		{
			name:     "database error",
			playerID: playerID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`DELETE FROM player_tokens WHERE player_id = \$1`).
					WithArgs(playerID.String()).
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

			s := NewPostgresPlayerTokenStore(mock)
			err = s.DeleteByPlayer(context.Background(), tt.playerID)

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

func TestPostgresPlayerTokenStore_DeleteExpired(t *testing.T) {
	tests := []struct {
		name      string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantCount int64
		wantErr   bool
		errMsg    string
	}{
		{
			name: "deletes expired rows and returns count",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`DELETE FROM player_tokens WHERE expires_at < now\(\)`).
					WillReturnResult(pgxmock.NewResult("DELETE", 5))
			},
			wantCount: 5,
		},
		{
			name: "no expired rows",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`DELETE FROM player_tokens WHERE expires_at < now\(\)`).
					WillReturnResult(pgxmock.NewResult("DELETE", 0))
			},
			wantCount: 0,
		},
		{
			name: "database error",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`DELETE FROM player_tokens WHERE expires_at < now\(\)`).
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

			s := NewPostgresPlayerTokenStore(mock)
			count, err := s.DeleteExpired(context.Background())

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantCount, count)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
