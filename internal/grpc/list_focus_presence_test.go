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
	// different ID than the target session's PlayerSessionID — ownership
	// mismatch MUST collapse to SESSION_NOT_FOUND (enumeration-safe).
	future := time.Now().Add(time.Hour)
	foreignSess := &session.Info{
		ID:              "foreign-sess",
		PlayerSessionID: ulid.MustParse("01HYXPLAYER000000000000XYZ"),
		ExpiresAt:       &future,
	}
	s := &CoreServer{
		sessionStore: newTestSessionStore(t,
			map[string]*session.Info{"foreign-sess": foreignSess}),
		// newFakePlayerSessionRepo binds testPlayerSessionToken to the given
		// player_session_id; passing a DIFFERENT ID below forces mismatch.
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
