// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc_test

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
	authmocks "github.com/holomush/holomush/internal/auth/mocks"
	"github.com/holomush/holomush/internal/eventbus"
	grpcpkg "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/session"
	sessionmocks "github.com/holomush/holomush/internal/session/mocks"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// --- stubs -----------------------------------------------------------------
//
// The query cluster's collaborators are narrow enough that generated mocks plus
// one hand-rolled reader cover everything these proofs need. That is the point
// of the extraction (D-02): the unit is exercisable without the facade's wiring
// and without the integration harness.

// stubHistoryReader is a non-nil eventbus.HistoryReader. Tests that assert an
// authorization denial need the Step 0 "reader configured" guard to pass while
// never reaching a fetch — a call here means the gate under test let the
// request through, so the stub fails the test rather than returning data.
type stubHistoryReader struct{ t *testing.T }

func (r *stubHistoryReader) QueryHistory(_ context.Context, _ eventbus.HistoryQuery) (eventbus.HistoryStream, error) {
	r.t.Helper()
	r.t.Fatal("QueryHistory must not be reached: the authorization gate under test should have denied first")
	return nil, nil
}

const queryTestToken = "query-handler-test-token" //nolint:gosec // test fixture, not a credential

// ownedSession returns a non-expired session owned by playerID.
func ownedSession(id string, playerID, characterID ulid.ULID) *session.Info {
	future := time.Now().Add(time.Hour)
	return &session.Info{
		ID:          id,
		Status:      session.StatusActive,
		ExpiresAt:   &future,
		PlayerID:    playerID,
		CharacterID: characterID,
	}
}

// ownershipPasses wires the player-session repo so auth.ValidateSessionOwnership
// succeeds for queryTestToken against a session owned by playerID.
func ownershipPasses(t *testing.T, playerID ulid.ULID) *authmocks.MockPlayerSessionRepository {
	t.Helper()
	repo := authmocks.NewMockPlayerSessionRepository(t)
	future := time.Now().Add(time.Hour)
	repo.EXPECT().
		GetByTokenHash(mock.Anything, auth.HashSessionToken(queryTestToken)).
		Return(&auth.PlayerSession{
			ID:        ulid.MustParse("01HYXPSESS000000000000000A"),
			PlayerID:  playerID,
			TokenHash: auth.HashSessionToken(queryTestToken),
			ExpiresAt: future,
		}, nil)
	return repo
}

// --- Test 1: SC1 proof ------------------------------------------------------

// TestNewQueryHandlerConstructsFromOnlyItsOwnCollaborators is the operational
// proof of ARCH-01 success criterion 1 (D-02) for the query cluster: the
// extracted unit is constructible from an external test package with narrow
// collaborators only — no facade type, no integrationtest harness, no build tag.
func TestNewQueryHandlerConstructsFromOnlyItsOwnCollaborators(t *testing.T) {
	h := grpcpkg.NewQueryHandler(grpcpkg.QueryDeps{
		SessionStore:      sessionmocks.NewMockStore(t),
		PlayerSessionRepo: authmocks.NewMockPlayerSessionRepository(t),
		GameID:            func() string { return "main" },
	})

	require.NotNil(t, h)
}

// --- Test 2: accessEngine nil DENIES a public stream read ------------------

// TestQueryStreamHistoryDeniesPublicStreamWhenAccessEngineIsNil pins the
// fail-closed default recorded on the accessEngine field comment. A nil engine
// is "ABAC not configured", and the Layer-3 public-stream branch MUST deny
// rather than fall through to an unauthorized read. Asserting the deny (not
// merely "no panic") is what makes an accidental permissive default a red test.
func TestQueryStreamHistoryDeniesPublicStreamWhenAccessEngineIsNil(t *testing.T) {
	playerID := ulid.MustParse("01HYXPLAYER00000000000001")
	charID := ulid.MustParse("01HYXCHAR00000000000000001")

	store := sessionmocks.NewMockStore(t)
	store.EXPECT().
		Get(mock.Anything, "sess-deny-1").
		Return(ownedSession("sess-deny-1", playerID, charID), nil)

	h := grpcpkg.NewQueryHandler(grpcpkg.QueryDeps{
		SessionStore:  store,
		HistoryReader: &stubHistoryReader{t: t},
		AccessEngine:  nil, // explicit: ABAC not configured
		GameID:        func() string { return "main" },
	})

	_, err := h.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "sess-deny-1",
		Stream:    "global",
	})

	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "STREAM_ACCESS_DENIED", o.Code())
}

// --- Test 3: commandQuerier nil fails closed -------------------------------

// TestListAvailableCommandsFailsClosedWhenCommandQuerierIsNil pins the second
// fail-closed default in this cluster. The assertion reads the TOP-LEVEL oops
// code (per .claude/rules/grpc-errors.md) rather than matching a message
// substring, so a differently-worded permissive return cannot pass.
func TestListAvailableCommandsFailsClosedWhenCommandQuerierIsNil(t *testing.T) {
	playerID := ulid.MustParse("01HYXPLAYER00000000000002")
	charID := ulid.MustParse("01HYXCHAR00000000000000002")

	store := sessionmocks.NewMockStore(t)
	store.EXPECT().
		Get(mock.Anything, "sess-cq-1").
		Return(ownedSession("sess-cq-1", playerID, charID), nil)

	h := grpcpkg.NewQueryHandler(grpcpkg.QueryDeps{
		SessionStore:      store,
		PlayerSessionRepo: ownershipPasses(t, playerID),
		CommandQuerier:    nil, // explicit: querier not wired
		GameID:            func() string { return "main" },
	})

	_, err := h.ListAvailableCommands(context.Background(), &corev1.ListAvailableCommandsRequest{
		SessionId:          "sess-cq-1",
		PlayerSessionToken: queryTestToken,
	})

	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "PERMISSION_DENIED", o.Code())
}

// --- Test 4: historyReader nil returns INTERNAL ----------------------------

// TestQueryStreamHistoryReturnsInternalWhenHistoryReaderIsNil pins the third
// nil default: an unwired reader is a misconfiguration, surfaced as INTERNAL.
// It is deliberately NOT an empty success — a silent empty page would look like
// "this stream has no history" to every caller.
func TestQueryStreamHistoryReturnsInternalWhenHistoryReaderIsNil(t *testing.T) {
	h := grpcpkg.NewQueryHandler(grpcpkg.QueryDeps{
		SessionStore:  sessionmocks.NewMockStore(t),
		HistoryReader: nil, // explicit: reader not wired
		GameID:        func() string { return "main" },
	})

	resp, err := h.QueryStreamHistory(context.Background(), &corev1.QueryStreamHistoryRequest{
		SessionId: "sess-nohist-1",
		Stream:    "global",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "INTERNAL", o.Code())
}

// --- Test 5: RefreshConnection argument and enumeration safety -------------

// TestRefreshConnectionRejectsEmptyIdentifiers pins the argument guard that
// runs before any store or repo access.
func TestRefreshConnectionRejectsEmptyIdentifiers(t *testing.T) {
	h := grpcpkg.NewQueryHandler(grpcpkg.QueryDeps{GameID: func() string { return "main" }})

	for _, tc := range []struct {
		name         string
		sessionID    string
		connectionID string
	}{
		{"empty session id", "", "01HYXCONN00000000000000001"},
		{"empty connection id", "sess-refresh-1", ""},
		{"both empty", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := h.RefreshConnection(context.Background(), &corev1.RefreshConnectionRequest{
				SessionId:    tc.sessionID,
				ConnectionId: tc.connectionID,
			})

			require.Error(t, err)
			o, ok := oops.AsOops(err)
			require.True(t, ok)
			assert.Equal(t, "INVALID_ARGUMENT", o.Code())
		})
	}
}

// TestRefreshConnectionCollapsesOwnershipFailureToSessionNotFound pins the
// enumeration-safety property recorded on RefreshConnection's doc comment: an
// ownership failure MUST NOT be distinguishable from an absent session, or the
// RPC becomes a session-existence oracle.
func TestRefreshConnectionCollapsesOwnershipFailureToSessionNotFound(t *testing.T) {
	h := grpcpkg.NewQueryHandler(grpcpkg.QueryDeps{
		SessionStore:      sessionmocks.NewMockStore(t),
		PlayerSessionRepo: authmocks.NewMockPlayerSessionRepository(t),
		GameID:            func() string { return "main" },
	})

	_, err := h.RefreshConnection(context.Background(), &corev1.RefreshConnectionRequest{
		SessionId:    "sess-refresh-2",
		ConnectionId: "01HYXCONN00000000000000001",
		// No player_session_token: ownership validation fails before any
		// repository call, so the response must be the same SESSION_NOT_FOUND
		// a genuinely absent session produces.
	})

	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "SESSION_NOT_FOUND", o.Code())
}
