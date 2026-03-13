// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
)

func TestPostgresAliasRepository_GetSystemAliases(t *testing.T) {
	tests := []struct {
		name      string
		setupMock func(mock pgxmock.PgxPoolIface)
		want      map[string]string
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful get with aliases",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"alias", "command"}).
					AddRow("n", "north").
					AddRow("s", "south").
					AddRow("e", "east")
				mock.ExpectQuery(`SELECT alias, command FROM system_aliases`).
					WillReturnRows(rows)
			},
			want: map[string]string{
				"n": "north",
				"s": "south",
				"e": "east",
			},
			wantErr: false,
		},
		{
			name: "successful get with no aliases",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"alias", "command"})
				mock.ExpectQuery(`SELECT alias, command FROM system_aliases`).
					WillReturnRows(rows)
			},
			want:    map[string]string{},
			wantErr: false,
		},
		{
			name: "database error",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT alias, command FROM system_aliases`).
					WillReturnError(errors.New("connection refused"))
			},
			want:    nil,
			wantErr: true,
			errMsg:  "connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err, "failed to create mock")
			defer mock.Close()

			tt.setupMock(mock)

			repo := NewPostgresAliasRepository(mock)
			got, err := repo.GetSystemAliases(context.Background())

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}

			assert.NoError(t, mock.ExpectationsWereMet(), "unfulfilled expectations")
		})
	}
}

func TestPostgresAliasRepository_SetSystemAlias(t *testing.T) {
	tests := []struct {
		name      string
		alias     string
		command   string
		createdBy string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "successful insert",
			alias:     "n",
			command:   "north",
			createdBy: "player-123",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO system_aliases`).
					WithArgs("n", "north", "player-123").
					WillReturnResult(pgxmock.NewResult("INSERT", 1))
			},
			wantErr: false,
		},
		{
			name:      "successful upsert (update existing)",
			alias:     "n",
			command:   "go north",
			createdBy: "player-456",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO system_aliases`).
					WithArgs("n", "go north", "player-456").
					WillReturnResult(pgxmock.NewResult("UPDATE", 1))
			},
			wantErr: false,
		},
		{
			name:      "empty created_by allowed",
			alias:     "look",
			command:   "l",
			createdBy: "",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO system_aliases`).
					WithArgs("look", "l", pgxmock.AnyArg()).
					WillReturnResult(pgxmock.NewResult("INSERT", 1))
			},
			wantErr: false,
		},
		{
			name:      "database error",
			alias:     "n",
			command:   "north",
			createdBy: "player-123",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO system_aliases`).
					WithArgs("n", "north", "player-123").
					WillReturnError(errors.New("constraint violation"))
			},
			wantErr: true,
			errMsg:  "constraint violation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err, "failed to create mock")
			defer mock.Close()

			tt.setupMock(mock)

			repo := NewPostgresAliasRepository(mock)
			err = repo.SetSystemAlias(context.Background(), tt.alias, tt.command, tt.createdBy)

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

func TestPostgresAliasRepository_DeleteSystemAlias(t *testing.T) {
	tests := []struct {
		name      string
		alias     string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errMsg    string
	}{
		{
			name:  "successful delete",
			alias: "n",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`DELETE FROM system_aliases WHERE alias = \$1`).
					WithArgs("n").
					WillReturnResult(pgxmock.NewResult("DELETE", 1))
			},
			wantErr: false,
		},
		{
			name:  "delete non-existent alias (no error)",
			alias: "nonexistent",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`DELETE FROM system_aliases WHERE alias = \$1`).
					WithArgs("nonexistent").
					WillReturnResult(pgxmock.NewResult("DELETE", 0))
			},
			wantErr: false,
		},
		{
			name:  "database error",
			alias: "n",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`DELETE FROM system_aliases WHERE alias = \$1`).
					WithArgs("n").
					WillReturnError(errors.New("connection lost"))
			},
			wantErr: true,
			errMsg:  "connection lost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err, "failed to create mock")
			defer mock.Close()

			tt.setupMock(mock)

			repo := NewPostgresAliasRepository(mock)
			err = repo.DeleteSystemAlias(context.Background(), tt.alias)

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

func TestPostgresAliasRepository_GetPlayerAliases(t *testing.T) {
	playerID := core.NewULID()

	tests := []struct {
		name      string
		playerID  ulid.ULID
		setupMock func(mock pgxmock.PgxPoolIface)
		want      map[string]string
		wantErr   bool
		errMsg    string
	}{
		{
			name:     "successful get with aliases",
			playerID: playerID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"alias", "command"}).
					AddRow("n", "north").
					AddRow("attack", "kill $1")
				mock.ExpectQuery(`SELECT alias, command FROM player_aliases WHERE player_id = \$1`).
					WithArgs(playerID.String()).
					WillReturnRows(rows)
			},
			want: map[string]string{
				"n":      "north",
				"attack": "kill $1",
			},
			wantErr: false,
		},
		{
			name:     "successful get with no aliases",
			playerID: playerID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{"alias", "command"})
				mock.ExpectQuery(`SELECT alias, command FROM player_aliases WHERE player_id = \$1`).
					WithArgs(playerID.String()).
					WillReturnRows(rows)
			},
			want:    map[string]string{},
			wantErr: false,
		},
		{
			name:     "database error",
			playerID: playerID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`SELECT alias, command FROM player_aliases WHERE player_id = \$1`).
					WithArgs(playerID.String()).
					WillReturnError(errors.New("timeout"))
			},
			want:    nil,
			wantErr: true,
			errMsg:  "timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err, "failed to create mock")
			defer mock.Close()

			tt.setupMock(mock)

			repo := NewPostgresAliasRepository(mock)
			got, err := repo.GetPlayerAliases(context.Background(), tt.playerID)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}

			assert.NoError(t, mock.ExpectationsWereMet(), "unfulfilled expectations")
		})
	}
}

func TestPostgresAliasRepository_SetPlayerAlias(t *testing.T) {
	playerID := core.NewULID()

	tests := []struct {
		name      string
		playerID  ulid.ULID
		alias     string
		command   string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errMsg    string
	}{
		{
			name:     "successful insert",
			playerID: playerID,
			alias:    "n",
			command:  "north",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO player_aliases`).
					WithArgs(playerID.String(), "n", "north").
					WillReturnResult(pgxmock.NewResult("INSERT", 1))
			},
			wantErr: false,
		},
		{
			name:     "successful upsert (update existing)",
			playerID: playerID,
			alias:    "n",
			command:  "go north",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO player_aliases`).
					WithArgs(playerID.String(), "n", "go north").
					WillReturnResult(pgxmock.NewResult("UPDATE", 1))
			},
			wantErr: false,
		},
		{
			name:     "database error",
			playerID: playerID,
			alias:    "n",
			command:  "north",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`INSERT INTO player_aliases`).
					WithArgs(playerID.String(), "n", "north").
					WillReturnError(errors.New("foreign key violation"))
			},
			wantErr: true,
			errMsg:  "foreign key violation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err, "failed to create mock")
			defer mock.Close()

			tt.setupMock(mock)

			repo := NewPostgresAliasRepository(mock)
			err = repo.SetPlayerAlias(context.Background(), tt.playerID, tt.alias, tt.command)

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

func TestPostgresAliasRepository_DeletePlayerAlias(t *testing.T) {
	playerID := core.NewULID()

	tests := []struct {
		name      string
		playerID  ulid.ULID
		alias     string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   bool
		errMsg    string
	}{
		{
			name:     "successful delete",
			playerID: playerID,
			alias:    "n",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`DELETE FROM player_aliases WHERE player_id = \$1 AND alias = \$2`).
					WithArgs(playerID.String(), "n").
					WillReturnResult(pgxmock.NewResult("DELETE", 1))
			},
			wantErr: false,
		},
		{
			name:     "delete non-existent alias (no error)",
			playerID: playerID,
			alias:    "nonexistent",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`DELETE FROM player_aliases WHERE player_id = \$1 AND alias = \$2`).
					WithArgs(playerID.String(), "nonexistent").
					WillReturnResult(pgxmock.NewResult("DELETE", 0))
			},
			wantErr: false,
		},
		{
			name:     "database error",
			playerID: playerID,
			alias:    "n",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec(`DELETE FROM player_aliases WHERE player_id = \$1 AND alias = \$2`).
					WithArgs(playerID.String(), "n").
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

			repo := NewPostgresAliasRepository(mock)
			err = repo.DeletePlayerAlias(context.Background(), tt.playerID, tt.alias)

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

func TestPostgresAliasRepository_ScanError(t *testing.T) {
	// Test row scanning errors for GetSystemAliases
	t.Run("system aliases scan error", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err, "failed to create mock")
		defer mock.Close()

		// Return a row with wrong column count to trigger scan error
		rows := pgxmock.NewRows([]string{"alias"}).
			AddRow("only-one-column")
		mock.ExpectQuery(`SELECT alias, command FROM system_aliases`).
			WillReturnRows(rows)

		repo := NewPostgresAliasRepository(mock)
		_, err = repo.GetSystemAliases(context.Background())

		require.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet(), "unfulfilled expectations")
	})

	// Test row scanning errors for GetPlayerAliases
	t.Run("player aliases scan error", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err, "failed to create mock")
		defer mock.Close()

		playerID := core.NewULID()

		// Return a row with wrong column count to trigger scan error
		rows := pgxmock.NewRows([]string{"alias"}).
			AddRow("only-one-column")
		mock.ExpectQuery(`SELECT alias, command FROM player_aliases WHERE player_id = \$1`).
			WithArgs(playerID.String()).
			WillReturnRows(rows)

		repo := NewPostgresAliasRepository(mock)
		_, err = repo.GetPlayerAliases(context.Background(), playerID)

		require.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet(), "unfulfilled expectations")
	})
}

func TestPostgresAliasRepository_RowsErr(t *testing.T) {
	// Test rows.Err() for GetSystemAliases
	t.Run("system aliases rows error", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err, "failed to create mock")
		defer mock.Close()

		rows := pgxmock.NewRows([]string{"alias", "command"}).
			AddRow("n", "north").
			RowError(0, errors.New("row iteration error"))
		mock.ExpectQuery(`SELECT alias, command FROM system_aliases`).
			WillReturnRows(rows)

		repo := NewPostgresAliasRepository(mock)
		_, err = repo.GetSystemAliases(context.Background())

		require.Error(t, err)
		assert.Contains(t, err.Error(), "row iteration error")
		assert.NoError(t, mock.ExpectationsWereMet(), "unfulfilled expectations")
	})

	// Test rows.Err() for GetPlayerAliases
	t.Run("player aliases rows error", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err, "failed to create mock")
		defer mock.Close()

		playerID := core.NewULID()

		rows := pgxmock.NewRows([]string{"alias", "command"}).
			AddRow("n", "north").
			RowError(0, errors.New("row iteration error"))
		mock.ExpectQuery(`SELECT alias, command FROM player_aliases WHERE player_id = \$1`).
			WithArgs(playerID.String()).
			WillReturnRows(rows)

		repo := NewPostgresAliasRepository(mock)
		_, err = repo.GetPlayerAliases(context.Background(), playerID)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "row iteration error")
		assert.NoError(t, mock.ExpectationsWereMet(), "unfulfilled expectations")
	})
}

// Test that the interface is correctly implemented
func TestAliasRepositoryInterface(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err, "failed to create mock")
	defer mock.Close()

	var _ AliasRepository = NewPostgresAliasRepository(mock)
}

// Test the constructor works with nil (for edge case coverage)
func TestNewPostgresAliasRepository(t *testing.T) {
	// Test with mock pool
	mock, err := pgxmock.NewPool()
	require.NoError(t, err, "failed to create mock")
	defer mock.Close()

	repo := NewPostgresAliasRepository(mock)
	require.NotNil(t, repo)
}
