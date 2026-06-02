// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"context"
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
