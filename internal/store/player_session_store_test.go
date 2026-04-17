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

// testPlayerSession builds a non-expired PlayerSession for use in tests.
func testPlayerSession() *auth.PlayerSession {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return &auth.PlayerSession{
		ID:        core.NewULID(),
		PlayerID:  core.NewULID(),
		TokenHash: "abc123hash",
		UserAgent: "test-agent",
		IPAddress: "127.0.0.1",
		ExpiresAt: now.Add(time.Hour),
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// playerSessionColumns returns the column names for player_sessions SELECT queries.
func playerSessionColumns() []string {
	return []string{"id", "player_id", "token_hash", "user_agent", "ip_address", "expires_at", "created_at", "updated_at"}
}

// playerSessionRow creates a pgxmock row from a PlayerSession.
func playerSessionRow(s *auth.PlayerSession) []any {
	return []any{s.ID.String(), s.PlayerID.String(), s.TokenHash, s.UserAgent, s.IPAddress, s.ExpiresAt, s.CreatedAt, s.UpdatedAt}
}

func TestPostgresPlayerSessionStore_CompileTimeCheck(_ *testing.T) {
	// Compile-time interface satisfaction check.
	var _ auth.PlayerSessionRepository = (*PostgresPlayerSessionStore)(nil)
}

func TestPostgresPlayerSessionStore_Create(t *testing.T) {
	ps := testPlayerSession()

	tests := []struct {
		name      string
		session   *auth.PlayerSession
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errMsg    string
	}{
		{
			name:    "happy path",
			session: ps,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO player_sessions`).
					WithArgs(ps.ID.String(), ps.PlayerID.String(), ps.TokenHash, ps.UserAgent, ps.IPAddress, ps.ExpiresAt, ps.CreatedAt, ps.UpdatedAt).
					WillReturnResult(pgxmock.NewResult("INSERT", 1))
			},
		},
		{
			name:    "database error",
			session: ps,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO player_sessions`).
					WithArgs(ps.ID.String(), ps.PlayerID.String(), ps.TokenHash, ps.UserAgent, ps.IPAddress, ps.ExpiresAt, ps.CreatedAt, ps.UpdatedAt).
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

			s := NewPostgresPlayerSessionStore(mock)
			err = s.Create(context.Background(), tt.session)

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

func TestPostgresPlayerSessionStore_GetByTokenHash(t *testing.T) {
	ps := testPlayerSession()

	// Build an already-expired session for the expiry test.
	expiredPS := &auth.PlayerSession{
		ID:        core.NewULID(),
		PlayerID:  core.NewULID(),
		TokenHash: "expiredhash",
		UserAgent: "test-agent",
		IPAddress: "127.0.0.1",
		ExpiresAt: time.Now().UTC().Add(-1 * time.Hour).Truncate(time.Microsecond),
		CreatedAt: time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Microsecond),
		UpdatedAt: time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Microsecond),
	}

	tests := []struct {
		name      string
		tokenHash string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errCode   string
		errMsg    string
		check     func(t *testing.T, got *auth.PlayerSession)
	}{
		{
			name:      "happy path",
			tokenHash: ps.TokenHash,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(playerSessionColumns()).AddRow(playerSessionRow(ps)...)
				mock.ExpectQuery(`SELECT .+ FROM player_sessions WHERE token_hash = \$1`).
					WithArgs(ps.TokenHash).
					WillReturnRows(rows)
			},
			check: func(t *testing.T, got *auth.PlayerSession) {
				t.Helper()
				assert.Equal(t, ps.ID, got.ID)
				assert.Equal(t, ps.PlayerID, got.PlayerID)
				assert.Equal(t, ps.TokenHash, got.TokenHash)
				assert.Equal(t, ps.UserAgent, got.UserAgent)
				assert.Equal(t, ps.IPAddress, got.IPAddress)
			},
		},
		{
			name:      "not found",
			tokenHash: "no-such-hash",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT .+ FROM player_sessions WHERE token_hash = \$1`).
					WithArgs("no-such-hash").
					WillReturnError(pgx.ErrNoRows)
			},
			wantErr: true,
			errCode: "PLAYER_SESSION_NOT_FOUND",
		},
		{
			name:      "expired session",
			tokenHash: expiredPS.TokenHash,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(playerSessionColumns()).AddRow(playerSessionRow(expiredPS)...)
				mock.ExpectQuery(`SELECT .+ FROM player_sessions WHERE token_hash = \$1`).
					WithArgs(expiredPS.TokenHash).
					WillReturnRows(rows)
				// Expect the conditional cleanup DELETE after detecting expiry.
				mock.ExpectExec(`DELETE FROM player_sessions WHERE id = \$1 AND expires_at < now\(\)`).
					WithArgs(expiredPS.ID.String()).
					WillReturnResult(pgxmock.NewResult("DELETE", 1))
			},
			wantErr: true,
			errCode: "PLAYER_SESSION_EXPIRED",
		},
		{
			name:      "database error",
			tokenHash: ps.TokenHash,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT .+ FROM player_sessions WHERE token_hash = \$1`).
					WithArgs(ps.TokenHash).
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

			s := NewPostgresPlayerSessionStore(mock)
			got, err := s.GetByTokenHash(context.Background(), tt.tokenHash)

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

func TestPostgresPlayerSessionStore_GetByTokenHash_InvalidIDFormat(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	// Return a row where the "id" column is not a valid ULID.
	rows := pgxmock.NewRows(playerSessionColumns()).
		AddRow("not-a-ulid", core.NewULID().String(), "somehash", "agent", "127.0.0.1",
			time.Now().UTC().Add(time.Hour), time.Now().UTC(), time.Now().UTC())
	mock.ExpectQuery(`SELECT .+ FROM player_sessions WHERE token_hash = \$1`).
		WithArgs("somehash").
		WillReturnRows(rows)

	s := NewPostgresPlayerSessionStore(mock)
	got, err := s.GetByTokenHash(context.Background(), "somehash")
	require.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "bad data size")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPostgresPlayerSessionStore_GetByTokenHash_InvalidPlayerIDFormat(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	// Return a row where the "player_id" column is not a valid ULID.
	rows := pgxmock.NewRows(playerSessionColumns()).
		AddRow(core.NewULID().String(), "not-a-ulid", "somehash", "agent", "127.0.0.1",
			time.Now().UTC().Add(time.Hour), time.Now().UTC(), time.Now().UTC())
	mock.ExpectQuery(`SELECT .+ FROM player_sessions WHERE token_hash = \$1`).
		WithArgs("somehash").
		WillReturnRows(rows)

	s := NewPostgresPlayerSessionStore(mock)
	got, err := s.GetByTokenHash(context.Background(), "somehash")
	require.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "bad data size")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPostgresPlayerSessionStore_Delete(t *testing.T) {
	sessionID := core.NewULID()

	tests := []struct {
		name      string
		id        ulid.ULID
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "happy path",
			id:   sessionID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`DELETE FROM player_sessions WHERE id = \$1`).
					WithArgs(sessionID.String()).
					WillReturnResult(pgxmock.NewResult("DELETE", 1))
			},
		},
		{
			name: "database error",
			id:   sessionID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`DELETE FROM player_sessions WHERE id = \$1`).
					WithArgs(sessionID.String()).
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

			s := NewPostgresPlayerSessionStore(mock)
			err = s.Delete(context.Background(), tt.id)

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

func TestPostgresPlayerSessionStore_DeleteByPlayer(t *testing.T) {
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
				mock.ExpectExec(`DELETE FROM player_sessions WHERE player_id = \$1`).
					WithArgs(playerID.String()).
					WillReturnResult(pgxmock.NewResult("DELETE", 3))
			},
		},
		{
			name:     "database error",
			playerID: playerID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`DELETE FROM player_sessions WHERE player_id = \$1`).
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

			s := NewPostgresPlayerSessionStore(mock)
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

func TestPostgresPlayerSessionStore_DeleteExpired(t *testing.T) {
	tests := []struct {
		name      string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantCount int64
		wantErr   bool
		errMsg    string
	}{
		{
			name: "deletes 3 rows",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`DELETE FROM player_sessions WHERE expires_at < now\(\)`).
					WillReturnResult(pgxmock.NewResult("DELETE", 3))
			},
			wantCount: 3,
		},
		{
			name: "deletes 0 rows",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`DELETE FROM player_sessions WHERE expires_at < now\(\)`).
					WillReturnResult(pgxmock.NewResult("DELETE", 0))
			},
			wantCount: 0,
		},
		{
			name: "database error",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`DELETE FROM player_sessions WHERE expires_at < now\(\)`).
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

			s := NewPostgresPlayerSessionStore(mock)
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

func TestPostgresPlayerSessionStore_GetByID(t *testing.T) {
	ps := testPlayerSession()

	tests := []struct {
		name      string
		id        ulid.ULID
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errCode   string
		errMsg    string
		check     func(t *testing.T, got *auth.PlayerSession)
	}{
		{
			name: "happy path",
			id:   ps.ID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(playerSessionColumns()).AddRow(playerSessionRow(ps)...)
				mock.ExpectQuery(`SELECT .+ FROM player_sessions WHERE id = \$1`).
					WithArgs(ps.ID.String()).
					WillReturnRows(rows)
			},
			check: func(t *testing.T, got *auth.PlayerSession) {
				t.Helper()
				assert.Equal(t, ps.ID, got.ID)
				assert.Equal(t, ps.PlayerID, got.PlayerID)
				assert.Equal(t, ps.TokenHash, got.TokenHash)
			},
		},
		{
			name: "not found",
			id:   ps.ID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT .+ FROM player_sessions WHERE id = \$1`).
					WithArgs(ps.ID.String()).
					WillReturnError(pgx.ErrNoRows)
			},
			wantErr: true,
			errCode: "PLAYER_SESSION_NOT_FOUND",
		},
		{
			name: "database error",
			id:   ps.ID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT .+ FROM player_sessions WHERE id = \$1`).
					WithArgs(ps.ID.String()).
					WillReturnError(errors.New("connection lost"))
			},
			wantErr: true,
			errCode: "PLAYER_SESSION_GET_BY_ID_FAILED",
			errMsg:  "connection lost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			s := NewPostgresPlayerSessionStore(mock)
			got, err := s.GetByID(context.Background(), tt.id)

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

func TestPostgresPlayerSessionStore_CountActiveByPlayer(t *testing.T) {
	playerID := core.NewULID()

	tests := []struct {
		name      string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantCount int
		wantErr   bool
		errCode   string
	}{
		{
			name: "returns active session count",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"count"}).AddRow(3)
				mock.ExpectQuery(`SELECT COUNT\(\*\) FROM player_sessions WHERE player_id = \$1 AND expires_at > now\(\)`).
					WithArgs(playerID.String()).
					WillReturnRows(rows)
			},
			wantCount: 3,
		},
		{
			name: "returns zero for no sessions",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"count"}).AddRow(0)
				mock.ExpectQuery(`SELECT COUNT\(\*\) FROM player_sessions WHERE player_id = \$1 AND expires_at > now\(\)`).
					WithArgs(playerID.String()).
					WillReturnRows(rows)
			},
			wantCount: 0,
		},
		{
			name: "database error",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT COUNT\(\*\) FROM player_sessions WHERE player_id = \$1 AND expires_at > now\(\)`).
					WithArgs(playerID.String()).
					WillReturnError(errors.New("connection lost"))
			},
			wantErr: true,
			errCode: "PLAYER_SESSION_COUNT_FAILED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			s := NewPostgresPlayerSessionStore(mock)
			count, err := s.CountActiveByPlayer(context.Background(), playerID)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errCode != "" {
					errutil.AssertErrorCode(t, err, tt.errCode)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantCount, count)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPostgresPlayerSessionStore_ListByPlayer(t *testing.T) {
	playerID := core.NewULID()
	now := time.Now().UTC().Truncate(time.Microsecond)

	newer := &auth.PlayerSession{
		ID: core.NewULID(), PlayerID: playerID, TokenHash: "hash-new",
		UserAgent: "agent", IPAddress: "127.0.0.1",
		ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now,
	}
	older := &auth.PlayerSession{
		ID: core.NewULID(), PlayerID: playerID, TokenHash: "hash-old",
		UserAgent: "agent", IPAddress: "127.0.0.1",
		ExpiresAt: now.Add(time.Hour), CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
	}

	tests := []struct {
		name      string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantLen   int
		wantErr   bool
		errCode   string
		check     func(t *testing.T, got []*auth.PlayerSession)
	}{
		{
			name: "returns sessions newest-first",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(playerSessionColumns()).
					AddRow(playerSessionRow(newer)...).
					AddRow(playerSessionRow(older)...)
				mock.ExpectQuery(`SELECT .+ FROM player_sessions\s+WHERE player_id = \$1 AND expires_at > now\(\)\s+ORDER BY created_at DESC`).
					WithArgs(playerID.String()).
					WillReturnRows(rows)
			},
			wantLen: 2,
			check: func(t *testing.T, got []*auth.PlayerSession) {
				t.Helper()
				assert.Equal(t, newer.ID, got[0].ID)
				assert.Equal(t, older.ID, got[1].ID)
			},
		},
		{
			name: "returns empty list for no sessions",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(playerSessionColumns())
				mock.ExpectQuery(`SELECT .+ FROM player_sessions\s+WHERE player_id = \$1`).
					WithArgs(playerID.String()).
					WillReturnRows(rows)
			},
			wantLen: 0,
		},
		{
			name: "database error",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT .+ FROM player_sessions\s+WHERE player_id = \$1`).
					WithArgs(playerID.String()).
					WillReturnError(errors.New("connection lost"))
			},
			wantErr: true,
			errCode: "PLAYER_SESSION_LIST_FAILED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			s := NewPostgresPlayerSessionStore(mock)
			got, err := s.ListByPlayer(context.Background(), playerID)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errCode != "" {
					errutil.AssertErrorCode(t, err, tt.errCode)
				}
			} else {
				require.NoError(t, err)
				assert.Len(t, got, tt.wantLen)
				if tt.check != nil {
					tt.check(t, got)
				}
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPostgresPlayerSessionStore_DeleteOldestForPlayer(t *testing.T) {
	playerID := core.NewULID()
	sessionID := core.NewULID()

	tests := []struct {
		name      string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantNil   bool
		wantID    ulid.ULID
		wantErr   bool
		errCode   string
	}{
		{
			name: "deletes and returns oldest session",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"id"}).AddRow(sessionID.String())
				mock.ExpectQuery(`DELETE FROM player_sessions\s+WHERE id = \(\s+SELECT id FROM player_sessions\s+WHERE player_id = \$1 AND expires_at > now\(\)\s+ORDER BY created_at ASC\s+LIMIT 1\s+\)\s+RETURNING id`).
					WithArgs(playerID.String()).
					WillReturnRows(rows)
			},
			wantID: sessionID,
		},
		{
			name: "returns nil,nil when player has no active sessions",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`DELETE FROM player_sessions`).
					WithArgs(playerID.String()).
					WillReturnError(pgx.ErrNoRows)
			},
			wantNil: true,
		},
		{
			name: "database error",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`DELETE FROM player_sessions`).
					WithArgs(playerID.String()).
					WillReturnError(errors.New("connection lost"))
			},
			wantErr: true,
			errCode: "PLAYER_SESSION_DELETE_OLDEST_FAILED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			s := NewPostgresPlayerSessionStore(mock)
			got, err := s.DeleteOldestForPlayer(context.Background(), playerID)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errCode != "" {
					errutil.AssertErrorCode(t, err, tt.errCode)
				}
			} else {
				require.NoError(t, err)
				if tt.wantNil {
					assert.Nil(t, got)
				} else {
					require.NotNil(t, got)
					assert.Equal(t, tt.wantID, got.ID)
					assert.Equal(t, playerID, got.PlayerID)
				}
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPostgresPlayerSessionStore_RefreshTTL(t *testing.T) {
	sessionID := core.NewULID()
	ttl := 24 * time.Hour

	tests := []struct {
		name      string
		id        ulid.ULID
		ttl       time.Duration
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errCode   string
		errMsg    string
	}{
		{
			name: "happy path",
			id:   sessionID,
			ttl:  ttl,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`UPDATE player_sessions SET expires_at = \$1, updated_at = \$2 WHERE id = \$3`).
					WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), sessionID.String()).
					WillReturnResult(pgxmock.NewResult("UPDATE", 1))
			},
		},
		{
			name:      "zero ttl rejected",
			id:        sessionID,
			ttl:       0,
			setupMock: func(_ pgxmock.PgxPoolIface) {},
			wantErr:   true,
			errCode:   "SESSION_INVALID_TTL",
			errMsg:    "ttl must be positive",
		},
		{
			name:      "negative ttl rejected",
			id:        sessionID,
			ttl:       -time.Minute,
			setupMock: func(_ pgxmock.PgxPoolIface) {},
			wantErr:   true,
			errCode:   "SESSION_INVALID_TTL",
			errMsg:    "ttl must be positive",
		},
		{
			name: "database error",
			id:   sessionID,
			ttl:  ttl,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`UPDATE player_sessions SET expires_at = \$1, updated_at = \$2 WHERE id = \$3`).
					WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), sessionID.String()).
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

			s := NewPostgresPlayerSessionStore(mock)
			err = s.RefreshTTL(context.Background(), tt.id, tt.ttl)

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
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPostgresPlayerSessionStore_CreateWithCap(t *testing.T) {
	ps := testPlayerSession()

	t.Run("inserts and trims within a single transaction when cap is positive", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		const capN = 3
		mock.ExpectBegin()
		mock.ExpectExec(`SELECT pg_advisory_xact_lock`).
			WithArgs(ps.PlayerID.String()).
			WillReturnResult(pgxmock.NewResult("SELECT", 1))
		mock.ExpectExec(`INSERT INTO player_sessions`).
			WithArgs(ps.ID.String(), ps.PlayerID.String(), ps.TokenHash, ps.UserAgent, ps.IPAddress, ps.ExpiresAt, ps.CreatedAt, ps.UpdatedAt).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))
		mock.ExpectExec(`DELETE FROM player_sessions`).
			WithArgs(ps.PlayerID.String(), ps.ID.String(), capN-1).
			WillReturnResult(pgxmock.NewResult("DELETE", 2))
		mock.ExpectCommit()
		mock.ExpectRollback()

		store := NewPostgresPlayerSessionStore(mock)
		trimmed, err := store.CreateWithCap(context.Background(), ps, capN)
		require.NoError(t, err)
		assert.Equal(t, 2, trimmed)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("skips trim when cap is zero (disabled)", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		mock.ExpectBegin()
		mock.ExpectExec(`SELECT pg_advisory_xact_lock`).
			WithArgs(ps.PlayerID.String()).
			WillReturnResult(pgxmock.NewResult("SELECT", 1))
		mock.ExpectExec(`INSERT INTO player_sessions`).
			WithArgs(ps.ID.String(), ps.PlayerID.String(), ps.TokenHash, ps.UserAgent, ps.IPAddress, ps.ExpiresAt, ps.CreatedAt, ps.UpdatedAt).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))
		// No DELETE expected when cap <= 0.
		mock.ExpectCommit()
		mock.ExpectRollback()

		store := NewPostgresPlayerSessionStore(mock)
		trimmed, err := store.CreateWithCap(context.Background(), ps, 0)
		require.NoError(t, err)
		assert.Equal(t, 0, trimmed)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("rolls back on insert failure", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		mock.ExpectBegin()
		mock.ExpectExec(`SELECT pg_advisory_xact_lock`).
			WithArgs(ps.PlayerID.String()).
			WillReturnResult(pgxmock.NewResult("SELECT", 1))
		mock.ExpectExec(`INSERT INTO player_sessions`).
			WithArgs(ps.ID.String(), ps.PlayerID.String(), ps.TokenHash, ps.UserAgent, ps.IPAddress, ps.ExpiresAt, ps.CreatedAt, ps.UpdatedAt).
			WillReturnError(errors.New("insert failed"))
		mock.ExpectRollback()

		store := NewPostgresPlayerSessionStore(mock)
		trimmed, err := store.CreateWithCap(context.Background(), ps, 3)
		require.Error(t, err)
		assert.Equal(t, 0, trimmed)
		errutil.AssertErrorCode(t, err, "PLAYER_SESSION_CREATE_FAILED")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("rolls back on trim failure", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		mock.ExpectBegin()
		mock.ExpectExec(`SELECT pg_advisory_xact_lock`).
			WithArgs(ps.PlayerID.String()).
			WillReturnResult(pgxmock.NewResult("SELECT", 1))
		mock.ExpectExec(`INSERT INTO player_sessions`).
			WithArgs(ps.ID.String(), ps.PlayerID.String(), ps.TokenHash, ps.UserAgent, ps.IPAddress, ps.ExpiresAt, ps.CreatedAt, ps.UpdatedAt).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))
		mock.ExpectExec(`DELETE FROM player_sessions`).
			WithArgs(ps.PlayerID.String(), ps.ID.String(), 2).
			WillReturnError(errors.New("delete failed"))
		mock.ExpectRollback()

		store := NewPostgresPlayerSessionStore(mock)
		trimmed, err := store.CreateWithCap(context.Background(), ps, 3)
		require.Error(t, err)
		assert.Equal(t, 0, trimmed)
		errutil.AssertErrorCode(t, err, "PLAYER_SESSION_TRIM_FAILED")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("returns begin error", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		mock.ExpectBegin().WillReturnError(errors.New("cannot begin tx"))

		store := NewPostgresPlayerSessionStore(mock)
		trimmed, err := store.CreateWithCap(context.Background(), ps, 3)
		require.Error(t, err)
		assert.Equal(t, 0, trimmed)
		errutil.AssertErrorCode(t, err, "PLAYER_SESSION_TX_BEGIN_FAILED")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("returns commit error", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		mock.ExpectBegin()
		mock.ExpectExec(`SELECT pg_advisory_xact_lock`).
			WithArgs(ps.PlayerID.String()).
			WillReturnResult(pgxmock.NewResult("SELECT", 1))
		mock.ExpectExec(`INSERT INTO player_sessions`).
			WithArgs(ps.ID.String(), ps.PlayerID.String(), ps.TokenHash, ps.UserAgent, ps.IPAddress, ps.ExpiresAt, ps.CreatedAt, ps.UpdatedAt).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))
		mock.ExpectExec(`DELETE FROM player_sessions`).
			WithArgs(ps.PlayerID.String(), ps.ID.String(), 2).
			WillReturnResult(pgxmock.NewResult("DELETE", 0))
		mock.ExpectCommit().WillReturnError(errors.New("commit failed"))
		mock.ExpectRollback()

		store := NewPostgresPlayerSessionStore(mock)
		trimmed, err := store.CreateWithCap(context.Background(), ps, 3)
		require.Error(t, err)
		assert.Equal(t, 0, trimmed)
		errutil.AssertErrorCode(t, err, "PLAYER_SESSION_TX_COMMIT_FAILED")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
