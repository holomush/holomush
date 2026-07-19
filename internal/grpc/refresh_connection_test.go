// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/testsupport/sessiontest"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// TestRefreshConnection covers the RefreshConnection RPC handler:
// argument validation, ownership collapse, connection lookup, and the success
// path. Backed by a real Postgres session store (sessiontest).
// Verifies: I-SEC-1
func TestRefreshConnection(t *testing.T) {
	const sessionID = "sess-1"

	tests := []struct {
		name          string
		seedSession   bool // seed an owned active session at sessionID
		seedConn      bool // seed a connection on that session (implies seedSession)
		reqSessionID  string
		malformedConn bool   // send a non-ULID connection_id
		emptyConn     bool   // send an empty connection_id
		wantCode      string // expected top-level oops code; "" → success
	}{
		{name: "empty session_id is rejected", reqSessionID: "", wantCode: "INVALID_ARGUMENT"},
		{name: "empty connection_id is rejected", reqSessionID: sessionID, emptyConn: true, wantCode: "INVALID_ARGUMENT"},
		{name: "ownership failure collapses to SESSION_NOT_FOUND (I-SEC-1)", reqSessionID: "missing", wantCode: "SESSION_NOT_FOUND"},
		{name: "malformed connection_id is rejected", seedSession: true, reqSessionID: sessionID, malformedConn: true, wantCode: "INVALID_ARGUMENT"},
		{name: "missing connection returns CONNECTION_NOT_FOUND", seedSession: true, reqSessionID: sessionID, wantCode: "CONNECTION_NOT_FOUND"},
		{name: "owned connection refreshes successfully", seedSession: true, seedConn: true, reqSessionID: sessionID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := sessiontest.NewStore(t)
			if tt.seedSession {
				require.NoError(t, store.Set(ctx, sessionID, mkActiveAt(sessionID, ulid.Make(), ulid.Make())))
			}

			// Resolve the request connection_id per the case flags.
			connID := ulid.Make().String()
			switch {
			case tt.emptyConn:
				connID = ""
			case tt.malformedConn:
				connID = "not-a-ulid"
			case tt.seedConn:
				cid := ulid.Make()
				require.NoError(t, store.AddConnection(ctx, &session.Connection{
					ID: cid, SessionID: sessionID, ClientType: "terminal",
				}))
				connID = cid.String()
			}

			s := &CoreServer{
				sessionStore:      store,
				playerSessionRepo: newFakePlayerSessionRepo(ownedPlayerID),
			}
			s.buildHandlers()
			resp, err := s.RefreshConnection(ctx, &corev1.RefreshConnectionRequest{
				SessionId:          tt.reqSessionID,
				ConnectionId:       connID,
				PlayerSessionToken: testPlayerSessionToken,
			})

			if tt.wantCode != "" {
				require.Error(t, err)
				// Top-level code assertion per .claude/rules/grpc-errors.md — the
				// handler/store return the oops code un-wrapped.
				o, ok := oops.AsOops(err)
				require.True(t, ok)
				assert.Equal(t, tt.wantCode, o.Code())
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, resp)
		})
	}
}
