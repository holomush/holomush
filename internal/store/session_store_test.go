// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/pashagolub/pgxmock/v5"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/pkg/errutil"
)

// TestPostgresSessionStore_AddConnectionRejectsInvalidClientType pins INV-SESSION-3:
// AddConnection rejects a client_type outside the allowed set
// (terminal, comms_hub, telnet) before issuing any SQL. The pgxmock pool
// registers no expectations because the validation short-circuits ahead of the
// INSERT — a stray Exec would fail the test.
func TestPostgresSessionStore_AddConnectionRejectsInvalidClientType(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	s := NewPostgresSessionStore(mock)
	err = s.AddConnection(context.Background(), &session.Connection{
		ID:         ulid.Make(),
		SessionID:  "sess-1",
		ClientType: "websocket", // not in validClientTypes
	})

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "INVALID_CLIENT_TYPE")

	require.NoError(t, mock.ExpectationsWereMet())
}

// TestRemoveConnectionAndCountLockOrder pins the D11 / INV-SCENE-27 lock order
// for the atomic remove+count primitive (holomush-cizj) without Docker: the
// ordered pgxmock expectations require the sessions row to be locked FOR
// UPDATE BEFORE the connection DELETE, and the post-delete COUNT to run inside
// the same transaction before COMMIT. A reordering would fail the ordered
// expectation set.
func TestRemoveConnectionAndCountLockOrder(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	sessionID := "sess-1"
	connID := ulid.Make()

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id FROM sessions WHERE id = \$1 FOR UPDATE`).
		WithArgs(sessionID).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(sessionID))
	mock.ExpectExec(`DELETE FROM session_connections WHERE id = \$1 AND session_id = \$2`).
		WithArgs(connID.String(), sessionID).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectQuery(`SELECT\s+COUNT\(\*\)`).
		WithArgs(sessionID).
		WillReturnRows(pgxmock.NewRows([]string{"total", "grid"}).AddRow(2, 1))
	mock.ExpectCommit()

	s := NewPostgresSessionStore(mock)
	counts, removed, err := s.RemoveConnectionAndCount(context.Background(), sessionID, connID)
	require.NoError(t, err)
	require.True(t, removed, "a DELETE affecting one row reports removed=true")
	require.Equal(t, 2, counts.Total)
	require.Equal(t, 1, counts.Grid)

	require.NoError(t, mock.ExpectationsWereMet())
}

// TestRemoveConnectionAndCountErrorPaths pins that every transaction stage of
// RemoveConnectionAndCount wraps its error and reports removed=false, and that
// the deferred Rollback fires on each failure after the tx is begun.
func TestRemoveConnectionAndCountErrorPaths(t *testing.T) {
	sessionID := "sess-1"
	connID := ulid.Make()
	boom := errors.New("boom")

	lockOK := func(m pgxmock.PgxPoolIface) {
		m.ExpectQuery(`SELECT id FROM sessions WHERE id = \$1 FOR UPDATE`).
			WithArgs(sessionID).
			WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(sessionID))
	}
	deleteOK := func(m pgxmock.PgxPoolIface) {
		m.ExpectExec(`DELETE FROM session_connections WHERE id = \$1 AND session_id = \$2`).
			WithArgs(connID.String(), sessionID).
			WillReturnResult(pgxmock.NewResult("DELETE", 1))
	}

	tests := []struct {
		name  string
		setup func(m pgxmock.PgxPoolIface)
	}{
		{"begin error", func(m pgxmock.PgxPoolIface) {
			m.ExpectBegin().WillReturnError(boom)
		}},
		{"lock error", func(m pgxmock.PgxPoolIface) {
			m.ExpectBegin()
			m.ExpectQuery(`SELECT id FROM sessions WHERE id = \$1 FOR UPDATE`).
				WithArgs(sessionID).WillReturnError(boom)
			m.ExpectRollback()
		}},
		{"delete error", func(m pgxmock.PgxPoolIface) {
			m.ExpectBegin()
			lockOK(m)
			m.ExpectExec(`DELETE FROM session_connections WHERE id = \$1 AND session_id = \$2`).
				WithArgs(connID.String(), sessionID).WillReturnError(boom)
			m.ExpectRollback()
		}},
		{"count error", func(m pgxmock.PgxPoolIface) {
			m.ExpectBegin()
			lockOK(m)
			deleteOK(m)
			m.ExpectQuery(`SELECT\s+COUNT\(\*\)`).WithArgs(sessionID).WillReturnError(boom)
			m.ExpectRollback()
		}},
		{"commit error", func(m pgxmock.PgxPoolIface) {
			m.ExpectBegin()
			lockOK(m)
			deleteOK(m)
			m.ExpectQuery(`SELECT\s+COUNT\(\*\)`).WithArgs(sessionID).
				WillReturnRows(pgxmock.NewRows([]string{"total", "grid"}).AddRow(1, 1))
			m.ExpectCommit().WillReturnError(boom)
			m.ExpectRollback()
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()
			tt.setup(mock)

			s := NewPostgresSessionStore(mock)
			_, removed, err := s.RemoveConnectionAndCount(context.Background(), sessionID, connID)
			require.Error(t, err)
			require.False(t, removed, "a failed removal reports removed=false")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
