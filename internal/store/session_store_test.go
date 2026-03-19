// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/oklog/ulid/v2"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/pkg/errutil"
)

func testSessionInfo() *session.Info {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return &session.Info{
		ID:            "sess-abc",
		CharacterID:   core.NewULID(),
		CharacterName: "TestChar",
		LocationID:    core.NewULID(),
		IsGuest:       false,
		Status:        session.StatusActive,
		GridPresent:   true,
		EventCursors:  map[string]ulid.ULID{"location:room-1": core.NewULID()},
		CommandHistory: []string{"look", "say hello"},
		TTLSeconds:    3600,
		MaxHistory:    50,
		DetachedAt:    nil,
		ExpiresAt:     nil,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

// sessionColumns returns the column names for session SELECT queries.
func sessionColumns() []string {
	return []string{
		"id", "character_id", "character_name", "location_id",
		"is_guest", "status", "grid_present", "event_cursors",
		"command_history", "ttl_seconds", "max_history",
		"detached_at", "expires_at", "created_at", "updated_at",
	}
}

// sessionRow creates a pgxmock row from a session.Info.
func sessionRow(info *session.Info) []any {
	cursorsJSON, _ := json.Marshal(info.EventCursors)
	return []any{
		info.ID,
		info.CharacterID.String(),
		info.CharacterName,
		info.LocationID.String(),
		info.IsGuest,
		string(info.Status),
		info.GridPresent,
		cursorsJSON,
		info.CommandHistory,
		info.TTLSeconds,
		info.MaxHistory,
		info.DetachedAt,
		info.ExpiresAt,
		info.CreatedAt,
		info.UpdatedAt,
	}
}

func TestPostgresSessionStore_CompileTimeCheck(_ *testing.T) {
	var _ session.Store = (*PostgresSessionStore)(nil)
}

func TestPostgresSessionStore_Get(t *testing.T) {
	info := testSessionInfo()

	tests := []struct {
		name      string
		id        string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errCode   string
		errMsg    string
		check     func(t *testing.T, got *session.Info)
	}{
		{
			name: "happy path",
			id:   "sess-abc",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(sessionColumns()).AddRow(sessionRow(info)...)
				mock.ExpectQuery(`SELECT .+ FROM sessions WHERE id = \$1`).
					WithArgs("sess-abc").
					WillReturnRows(rows)
			},
			check: func(t *testing.T, got *session.Info) {
				t.Helper()
				assert.Equal(t, info.ID, got.ID)
				assert.Equal(t, info.CharacterName, got.CharacterName)
				assert.Equal(t, info.Status, got.Status)
			},
		},
		{
			name: "not found",
			id:   "sess-missing",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT .+ FROM sessions WHERE id = \$1`).
					WithArgs("sess-missing").
					WillReturnError(pgx.ErrNoRows)
			},
			wantErr: true,
			errCode: "SESSION_NOT_FOUND",
		},
		{
			name: "database error",
			id:   "sess-abc",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT .+ FROM sessions WHERE id = \$1`).
					WithArgs("sess-abc").
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

			store := NewPostgresSessionStore(mock)
			got, err := store.Get(context.Background(), tt.id)

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

func TestPostgresSessionStore_Set(t *testing.T) {
	info := testSessionInfo()

	tests := []struct {
		name      string
		id        string
		info      *session.Info
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful upsert",
			id:   "sess-abc",
			info: info,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO sessions`).
					WithArgs(
						pgxmock.AnyArg(), // id
						pgxmock.AnyArg(), // character_id
						pgxmock.AnyArg(), // character_name
						pgxmock.AnyArg(), // location_id
						pgxmock.AnyArg(), // is_guest
						pgxmock.AnyArg(), // status
						pgxmock.AnyArg(), // grid_present
						pgxmock.AnyArg(), // event_cursors
						pgxmock.AnyArg(), // command_history
						pgxmock.AnyArg(), // ttl_seconds
						pgxmock.AnyArg(), // max_history
						pgxmock.AnyArg(), // detached_at
						pgxmock.AnyArg(), // expires_at
						pgxmock.AnyArg(), // created_at
					).
					WillReturnResult(pgxmock.NewResult("INSERT", 1))
			},
		},
		{
			name: "database error",
			id:   "sess-abc",
			info: info,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO sessions`).
					WithArgs(
						pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
						pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
						pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
						pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
						pgxmock.AnyArg(), pgxmock.AnyArg(),
					).
					WillReturnError(errors.New("disk full"))
			},
			wantErr: true,
			errMsg:  "disk full",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			store := NewPostgresSessionStore(mock)
			err = store.Set(context.Background(), tt.id, tt.info)

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

func TestPostgresSessionStore_Delete(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "happy path",
			id:   "sess-abc",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`DELETE FROM sessions WHERE id = \$1`).
					WithArgs("sess-abc").
					WillReturnResult(pgxmock.NewResult("DELETE", 1))
			},
		},
		{
			name: "database error",
			id:   "sess-abc",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`DELETE FROM sessions WHERE id = \$1`).
					WithArgs("sess-abc").
					WillReturnError(errors.New("connection refused"))
			},
			wantErr: true,
			errMsg:  "connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			store := NewPostgresSessionStore(mock)
			err = store.Delete(context.Background(), tt.id)

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

func TestPostgresSessionStore_FindByCharacter(t *testing.T) {
	info := testSessionInfo()
	charID := info.CharacterID

	tests := []struct {
		name    string
		charID  ulid.ULID
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr bool
		errCode string
		check   func(t *testing.T, got *session.Info)
	}{
		{
			name:   "happy path",
			charID: charID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(sessionColumns()).AddRow(sessionRow(info)...)
				mock.ExpectQuery(`SELECT .+ FROM sessions WHERE character_id = \$1 AND status IN`).
					WithArgs(charID.String()).
					WillReturnRows(rows)
			},
			check: func(t *testing.T, got *session.Info) {
				t.Helper()
				assert.Equal(t, info.ID, got.ID)
				assert.Equal(t, charID, got.CharacterID)
			},
		},
		{
			name:   "not found",
			charID: charID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT .+ FROM sessions WHERE character_id = \$1 AND status IN`).
					WithArgs(charID.String()).
					WillReturnError(pgx.ErrNoRows)
			},
			wantErr: true,
			errCode: "SESSION_NOT_FOUND",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			store := NewPostgresSessionStore(mock)
			got, err := store.FindByCharacter(context.Background(), tt.charID)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errCode != "" {
					errutil.AssertErrorCode(t, err, tt.errCode)
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

func TestPostgresSessionStore_ListByPlayer(t *testing.T) {
	info := testSessionInfo()
	playerID := core.NewULID()

	tests := []struct {
		name      string
		playerID  ulid.ULID
		setupMock func(mock pgxmock.PgxPoolIface)
		wantCount int
		wantErr   bool
		errMsg    string
	}{
		{
			name:     "returns non-expired sessions",
			playerID: playerID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(sessionColumns()).
					AddRow(sessionRow(info)...)
				mock.ExpectQuery(`SELECT .+ FROM sessions WHERE status != 'expired'`).
					WillReturnRows(rows)
			},
			wantCount: 1,
		},
		{
			name:     "empty result",
			playerID: playerID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(sessionColumns())
				mock.ExpectQuery(`SELECT .+ FROM sessions WHERE status != 'expired'`).
					WillReturnRows(rows)
			},
			wantCount: 0,
		},
		{
			name:     "database error",
			playerID: playerID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT .+ FROM sessions WHERE status != 'expired'`).
					WillReturnError(errors.New("timeout"))
			},
			wantErr: true,
			errMsg:  "timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			store := NewPostgresSessionStore(mock)
			got, err := store.ListByPlayer(context.Background(), tt.playerID)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
				assert.Len(t, got, tt.wantCount)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPostgresSessionStore_ListExpired(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	pastExpiry := now.Add(-1 * time.Hour)
	detachedAt := now.Add(-2 * time.Hour)

	expiredInfo := testSessionInfo()
	expiredInfo.Status = session.StatusDetached
	expiredInfo.DetachedAt = &detachedAt
	expiredInfo.ExpiresAt = &pastExpiry

	tests := []struct {
		name      string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantCount int
		wantErr   bool
		errMsg    string
	}{
		{
			name: "returns detached sessions past expiry",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(sessionColumns()).
					AddRow(sessionRow(expiredInfo)...)
				mock.ExpectQuery(`SELECT .+ FROM sessions WHERE status = 'detached' AND expires_at < now\(\)`).
					WillReturnRows(rows)
			},
			wantCount: 1,
		},
		{
			name: "no expired sessions",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(sessionColumns())
				mock.ExpectQuery(`SELECT .+ FROM sessions WHERE status = 'detached' AND expires_at < now\(\)`).
					WillReturnRows(rows)
			},
			wantCount: 0,
		},
		{
			name: "database error",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT .+ FROM sessions WHERE status = 'detached' AND expires_at < now\(\)`).
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

			store := NewPostgresSessionStore(mock)
			got, err := store.ListExpired(context.Background())

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
				assert.Len(t, got, tt.wantCount)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPostgresSessionStore_UpdateStatus(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	expiresAt := now.Add(time.Hour)

	tests := []struct {
		name       string
		id         string
		status     session.Status
		detachedAt *time.Time
		expiresAt  *time.Time
		setupMock  func(mock pgxmock.PgxPoolIface)
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "transition to detached",
			id:         "sess-abc",
			status:     session.StatusDetached,
			detachedAt: &now,
			expiresAt:  &expiresAt,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`UPDATE sessions SET status = \$1, detached_at = \$2, expires_at = \$3, updated_at = now\(\) WHERE id = \$4`).
					WithArgs(string(session.StatusDetached), &now, &expiresAt, "sess-abc").
					WillReturnResult(pgxmock.NewResult("UPDATE", 1))
			},
		},
		{
			name:       "database error",
			id:         "sess-abc",
			status:     session.StatusExpired,
			detachedAt: nil,
			expiresAt:  nil,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`UPDATE sessions SET status = \$1, detached_at = \$2, expires_at = \$3, updated_at = now\(\) WHERE id = \$4`).
					WithArgs(string(session.StatusExpired), pgxmock.AnyArg(), pgxmock.AnyArg(), "sess-abc").
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

			store := NewPostgresSessionStore(mock)
			err = store.UpdateStatus(context.Background(), tt.id, tt.status, tt.detachedAt, tt.expiresAt)

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

func TestPostgresSessionStore_ReattachCAS(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		setupMock func(mock pgxmock.PgxPoolIface)
		want      bool
		wantErr   bool
		errMsg    string
	}{
		{
			name: "success - row updated",
			id:   "sess-abc",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`UPDATE sessions SET status = 'active', detached_at = NULL, expires_at = NULL, updated_at = now\(\) WHERE id = \$1 AND status = 'detached'`).
					WithArgs("sess-abc").
					WillReturnResult(pgxmock.NewResult("UPDATE", 1))
			},
			want: true,
		},
		{
			name: "race lost - 0 rows affected",
			id:   "sess-abc",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`UPDATE sessions SET status = 'active', detached_at = NULL, expires_at = NULL, updated_at = now\(\) WHERE id = \$1 AND status = 'detached'`).
					WithArgs("sess-abc").
					WillReturnResult(pgxmock.NewResult("UPDATE", 0))
			},
			want: false,
		},
		{
			name: "database error",
			id:   "sess-abc",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`UPDATE sessions SET status = 'active', detached_at = NULL, expires_at = NULL, updated_at = now\(\) WHERE id = \$1 AND status = 'detached'`).
					WithArgs("sess-abc").
					WillReturnError(errors.New("deadlock"))
			},
			wantErr: true,
			errMsg:  "deadlock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			store := NewPostgresSessionStore(mock)
			got, err := store.ReattachCAS(context.Background(), tt.id)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPostgresSessionStore_UpdateCursors(t *testing.T) {
	cursorID := core.NewULID()
	cursors := map[string]ulid.ULID{"location:room-1": cursorID}

	tests := []struct {
		name      string
		id        string
		cursors   map[string]ulid.ULID
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errMsg    string
	}{
		{
			name:    "partial update",
			id:      "sess-abc",
			cursors: cursors,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`UPDATE sessions SET event_cursors = event_cursors \|\| \$1::jsonb, updated_at = now\(\) WHERE id = \$2`).
					WithArgs(pgxmock.AnyArg(), "sess-abc").
					WillReturnResult(pgxmock.NewResult("UPDATE", 1))
			},
		},
		{
			name:    "database error",
			id:      "sess-abc",
			cursors: cursors,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`UPDATE sessions SET event_cursors = event_cursors \|\| \$1::jsonb, updated_at = now\(\) WHERE id = \$2`).
					WithArgs(pgxmock.AnyArg(), "sess-abc").
					WillReturnError(errors.New("write error"))
			},
			wantErr: true,
			errMsg:  "write error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			store := NewPostgresSessionStore(mock)
			err = store.UpdateCursors(context.Background(), tt.id, tt.cursors)

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

func TestPostgresSessionStore_AppendCommand(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		command    string
		maxHistory int
		setupMock  func(mock pgxmock.PgxPoolIface)
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "appends to history",
			id:         "sess-abc",
			command:    "look",
			maxHistory: 50,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`UPDATE sessions SET command_history`).
					WithArgs("look", 50, "sess-abc").
					WillReturnResult(pgxmock.NewResult("UPDATE", 1))
			},
		},
		{
			name:       "database error",
			id:         "sess-abc",
			command:    "look",
			maxHistory: 50,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`UPDATE sessions SET command_history`).
					WithArgs("look", 50, "sess-abc").
					WillReturnError(errors.New("timeout"))
			},
			wantErr: true,
			errMsg:  "timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			store := NewPostgresSessionStore(mock)
			err = store.AppendCommand(context.Background(), tt.id, tt.command, tt.maxHistory)

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

func TestPostgresSessionStore_GetCommandHistory(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		setupMock func(mock pgxmock.PgxPoolIface)
		want      []string
		wantErr   bool
		errCode   string
		errMsg    string
	}{
		{
			name: "returns history array",
			id:   "sess-abc",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"command_history"}).
					AddRow([]string{"look", "say hello", "go north"})
				mock.ExpectQuery(`SELECT command_history FROM sessions WHERE id = \$1`).
					WithArgs("sess-abc").
					WillReturnRows(rows)
			},
			want: []string{"look", "say hello", "go north"},
		},
		{
			name: "not found",
			id:   "sess-missing",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT command_history FROM sessions WHERE id = \$1`).
					WithArgs("sess-missing").
					WillReturnError(pgx.ErrNoRows)
			},
			wantErr: true,
			errCode: "SESSION_NOT_FOUND",
		},
		{
			name: "database error",
			id:   "sess-abc",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT command_history FROM sessions WHERE id = \$1`).
					WithArgs("sess-abc").
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

			store := NewPostgresSessionStore(mock)
			got, err := store.GetCommandHistory(context.Background(), tt.id)

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
				assert.Equal(t, tt.want, got)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPostgresSessionStore_AddConnection(t *testing.T) {
	connID := core.NewULID()
	now := time.Now().UTC().Truncate(time.Microsecond)
	conn := &session.Connection{
		ID:          connID,
		SessionID:   "sess-abc",
		ClientType:  "telnet",
		Streams:     []string{"location:room-1", "system:global"},
		ConnectedAt: now,
	}

	tests := []struct {
		name      string
		conn      *session.Connection
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "inserts connection row",
			conn: conn,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO session_connections`).
					WithArgs(
						connID.String(),
						"sess-abc",
						"telnet",
						pgxmock.AnyArg(), // streams
						now,
					).
					WillReturnResult(pgxmock.NewResult("INSERT", 1))
			},
		},
		{
			name: "database error",
			conn: conn,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO session_connections`).
					WithArgs(
						connID.String(),
						"sess-abc",
						"telnet",
						pgxmock.AnyArg(),
						now,
					).
					WillReturnError(errors.New("constraint violation"))
			},
			wantErr: true,
			errMsg:  "constraint violation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			store := NewPostgresSessionStore(mock)
			err = store.AddConnection(context.Background(), tt.conn)

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

func TestPostgresSessionStore_RemoveConnection(t *testing.T) {
	connID := core.NewULID()

	tests := []struct {
		name      string
		connID    ulid.ULID
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errMsg    string
	}{
		{
			name:   "deletes connection row",
			connID: connID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`DELETE FROM session_connections WHERE id = \$1`).
					WithArgs(connID.String()).
					WillReturnResult(pgxmock.NewResult("DELETE", 1))
			},
		},
		{
			name:   "database error",
			connID: connID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`DELETE FROM session_connections WHERE id = \$1`).
					WithArgs(connID.String()).
					WillReturnError(errors.New("connection refused"))
			},
			wantErr: true,
			errMsg:  "connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			store := NewPostgresSessionStore(mock)
			err = store.RemoveConnection(context.Background(), tt.connID)

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

func TestPostgresSessionStore_CountConnections(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		setupMock func(mock pgxmock.PgxPoolIface)
		want      int
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "counts connections",
			sessionID: "sess-abc",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"count"}).AddRow(3)
				mock.ExpectQuery(`SELECT COUNT\(\*\) FROM session_connections WHERE session_id = \$1`).
					WithArgs("sess-abc").
					WillReturnRows(rows)
			},
			want: 3,
		},
		{
			name:      "zero connections",
			sessionID: "sess-abc",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"count"}).AddRow(0)
				mock.ExpectQuery(`SELECT COUNT\(\*\) FROM session_connections WHERE session_id = \$1`).
					WithArgs("sess-abc").
					WillReturnRows(rows)
			},
			want: 0,
		},
		{
			name:      "database error",
			sessionID: "sess-abc",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT COUNT\(\*\) FROM session_connections WHERE session_id = \$1`).
					WithArgs("sess-abc").
					WillReturnError(errors.New("timeout"))
			},
			wantErr: true,
			errMsg:  "timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			store := NewPostgresSessionStore(mock)
			got, err := store.CountConnections(context.Background(), tt.sessionID)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPostgresSessionStore_UpdateGridPresent(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		present   bool
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errMsg    string
	}{
		{
			name:    "sets grid_present true",
			id:      "sess-abc",
			present: true,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`UPDATE sessions SET grid_present = \$2, updated_at = NOW\(\) WHERE id = \$1`).
					WithArgs("sess-abc", true).
					WillReturnResult(pgxmock.NewResult("UPDATE", 1))
			},
		},
		{
			name:    "sets grid_present false",
			id:      "sess-abc",
			present: false,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`UPDATE sessions SET grid_present = \$2, updated_at = NOW\(\) WHERE id = \$1`).
					WithArgs("sess-abc", false).
					WillReturnResult(pgxmock.NewResult("UPDATE", 1))
			},
		},
		{
			name:    "database error",
			id:      "sess-abc",
			present: true,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`UPDATE sessions SET grid_present = \$2, updated_at = NOW\(\) WHERE id = \$1`).
					WithArgs("sess-abc", true).
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

			store := NewPostgresSessionStore(mock)
			err = store.UpdateGridPresent(context.Background(), tt.id, tt.present)

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

func TestPostgresSessionStore_AddConnection_InvalidClientType(t *testing.T) {
	connID := core.NewULID()
	now := time.Now().UTC().Truncate(time.Microsecond)
	conn := &session.Connection{
		ID:          connID,
		SessionID:   "sess-abc",
		ClientType:  "invalid_type",
		Streams:     []string{},
		ConnectedAt: now,
	}

	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	store := NewPostgresSessionStore(mock)
	err = store.AddConnection(context.Background(), conn)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_type")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPostgresSessionStore_CountConnectionsByType(t *testing.T) {
	tests := []struct {
		name       string
		sessionID  string
		clientType string
		setupMock  func(mock pgxmock.PgxPoolIface)
		want       int
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "filters by type",
			sessionID:  "sess-abc",
			clientType: "telnet",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"count"}).AddRow(2)
				mock.ExpectQuery(`SELECT COUNT\(\*\) FROM session_connections WHERE session_id = \$1 AND client_type = \$2`).
					WithArgs("sess-abc", "telnet").
					WillReturnRows(rows)
			},
			want: 2,
		},
		{
			name:       "database error",
			sessionID:  "sess-abc",
			clientType: "telnet",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT COUNT\(\*\) FROM session_connections WHERE session_id = \$1 AND client_type = \$2`).
					WithArgs("sess-abc", "telnet").
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

			store := NewPostgresSessionStore(mock)
			got, err := store.CountConnectionsByType(context.Background(), tt.sessionID, tt.clientType)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
