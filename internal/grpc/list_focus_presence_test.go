// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/session"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

func TestListFocusPresenceReturnsInvalidArgumentOnNilRequest(t *testing.T) {
	// Defends against direct-call misuse (test or wrapping code passing nil).
	// ConnectRPC's wire path never delivers a nil request, but a direct nil
	// would panic on req.Meta access without this guard.
	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, nil),
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
	}
	_, err := s.ListFocusPresence(context.Background(), nil)
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "INVALID_ARGUMENT", o.Code())
}

func TestListFocusPresenceReturnsInvalidArgumentOnEmptySessionID(t *testing.T) {
	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, nil),
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
	}
	_, err := s.ListFocusPresence(context.Background(),
		&corev1.ListFocusPresenceRequest{SessionId: ""})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "INVALID_ARGUMENT", o.Code())
}

func TestListFocusPresenceReturnsSessionNotFoundOnUnknownSession(t *testing.T) {
	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, nil),
		playerSessionRepo: newFakePlayerSessionRepo(ulid.ULID{}),
	}
	_, err := s.ListFocusPresence(context.Background(),
		&corev1.ListFocusPresenceRequest{
			SessionId:          "missing",
			PlayerSessionToken: testPlayerSessionToken,
		})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "SESSION_NOT_FOUND", o.Code())
}

func TestListFocusPresenceCollapsesOwnershipMismatchToNotFound(t *testing.T) {
	// Caller's player_session_token resolves to a player session with a
	// different PlayerID than the target session's PlayerID — ownership
	// mismatch MUST collapse to SESSION_NOT_FOUND (enumeration-safe).
	future := time.Now().Add(time.Hour)
	foreignSess := &session.Info{
		ID:        "foreign-sess",
		PlayerID:  ulid.MustParse("01HYXPLAYER000000000000XYZ"),
		ExpiresAt: &future,
	}
	s := &CoreServer{
		sessionStore: newTestSessionStore(t,
			map[string]*session.Info{"foreign-sess": foreignSess}),
		// newFakePlayerSessionRepo binds testPlayerSessionToken to the given
		// player_id; passing a DIFFERENT ID below forces mismatch.
		playerSessionRepo: newFakePlayerSessionRepo(
			ulid.MustParse("01HYXPLAYER111111111111ABC"),
		),
	}
	_, err := s.ListFocusPresence(context.Background(),
		&corev1.ListFocusPresenceRequest{
			SessionId:          "foreign-sess",
			PlayerSessionToken: testPlayerSessionToken,
		})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "SESSION_NOT_FOUND", o.Code())
}

// ownedPlayerID is the player ID that newFakePlayerSessionRepo will return
// when called with this value, and the session.Info.PlayerID that mkOwnedSession
// seeds — ensuring ownership validation succeeds.
var ownedPlayerID = ulid.MustParse("01HYXOWN0000000000000000PS")

// mkOwnedSession returns a session.Info whose PlayerID matches ownedPlayerID,
// so that newFakePlayerSessionRepo(ownedPlayerID) passes ownership validation.
func mkOwnedSession(id string, opts ...func(*session.Info)) *session.Info {
	future := time.Now().Add(time.Hour)
	s := &session.Info{
		ID:        id,
		Status:    session.StatusActive,
		ExpiresAt: &future,
		PlayerID:  ownedPlayerID,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func TestListFocusPresenceReturnsSessionExpiredForExpiredSession(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	sess := mkOwnedSession("sess-expired")
	sess.ExpiresAt = &past
	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, map[string]*session.Info{"sess-expired": sess}),
		playerSessionRepo: newFakePlayerSessionRepo(ownedPlayerID),
	}
	_, err := s.ListFocusPresence(context.Background(), &corev1.ListFocusPresenceRequest{
		SessionId:          "sess-expired",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "SESSION_EXPIRED", o.Code())
}

func TestListFocusPresenceReturnsUnimplementedForSceneFocus(t *testing.T) {
	sess := mkOwnedSession("sess-1", func(s *session.Info) {
		s.CharacterID = ulid.MustParse("01HYXCHAR0000000000000000C")
		s.LocationID = ulid.MustParse("01HYXLOC00000000000000000L")
		s.FocusMemberships = []session.FocusMembership{
			{Kind: session.FocusKindScene, TargetID: ulid.MustParse("01HYXSCENE0000000000000000")},
		}
	})
	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, map[string]*session.Info{"sess-1": sess}),
		playerSessionRepo: newFakePlayerSessionRepo(ownedPlayerID),
	}
	_, err := s.ListFocusPresence(context.Background(), &corev1.ListFocusPresenceRequest{
		SessionId:          "sess-1",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "UNIMPLEMENTED", o.Code())
}

func TestListFocusPresenceReturnsEmptyEntriesWhenLocationUnset(t *testing.T) {
	sess := mkOwnedSession("sess-1", func(s *session.Info) {
		s.CharacterID = ulid.MustParse("01HYXCHAR0000000000000000C")
		// LocationID intentionally zero
	})
	s := &CoreServer{
		sessionStore:      newTestSessionStore(t, map[string]*session.Info{"sess-1": sess}),
		playerSessionRepo: newFakePlayerSessionRepo(ownedPlayerID),
	}
	resp, err := s.ListFocusPresence(context.Background(), &corev1.ListFocusPresenceRequest{
		SessionId:          "sess-1",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	assert.Equal(t, corev1.PresenceContext_PRESENCE_CONTEXT_LOCATION, resp.Context)
	assert.Equal(t, "", resp.ContextId)
	assert.Empty(t, resp.Entries)
}
