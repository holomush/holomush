// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package auth_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/auth/mocks"
	"github.com/holomush/holomush/internal/session"
	sessionmocks "github.com/holomush/holomush/internal/session/mocks"
	"github.com/holomush/holomush/pkg/errutil"
)

// playerAID and playerBID are stable fixture IDs. The session package does
// not export a sentinel ErrNotFound; instead Store.Get returns an oops error
// with code "SESSION_NOT_FOUND". sessionNotFoundOopsError constructs that
// shape for use in mock expectations.
var (
	playerAID = ulid.MustParseStrict("01HZZZZZZZZZZZZZZZZZZZZZZZ")
	playerBID = ulid.MustParseStrict("01J00000000000000000PYARBB")
)

// newSessionNotFoundErr mirrors the error shape returned by
// session.MemStore/PostgresSessionStore for a missing session.
func newSessionNotFoundErr(id string) error {
	return oops.Code("SESSION_NOT_FOUND").
		With("session_id", id).
		Errorf("session not found")
}

func TestValidateSessionOwnershipRejectsEmptyToken(t *testing.T) {
	ctx := context.Background()
	players := mocks.NewMockPlayerSessionRepository(t)
	store := sessionmocks.NewMockStore(t)

	_, err := auth.ValidateSessionOwnership(ctx, players, store, "", "sess-1")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SESSION_NOT_FOUND")
}

func TestValidateSessionOwnershipRejectsUnknownToken(t *testing.T) {
	ctx := context.Background()
	players := mocks.NewMockPlayerSessionRepository(t)
	store := sessionmocks.NewMockStore(t)

	players.EXPECT().GetByTokenHash(ctx, auth.HashSessionToken("tok-unknown")).
		Return(nil, auth.ErrNotFound)

	_, err := auth.ValidateSessionOwnership(ctx, players, store, "tok-unknown", "sess-1")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SESSION_NOT_FOUND")
}

func TestValidateSessionOwnershipRejectsExpiredToken(t *testing.T) {
	ctx := context.Background()
	players := mocks.NewMockPlayerSessionRepository(t)
	store := sessionmocks.NewMockStore(t)

	expired := newExpiredPlayerSessionFixture(t)
	players.EXPECT().GetByTokenHash(ctx, auth.HashSessionToken("tok-exp")).
		Return(expired, nil)

	_, err := auth.ValidateSessionOwnership(ctx, players, store, "tok-exp", "sess-1")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SESSION_NOT_FOUND")
}

func TestValidateSessionOwnershipRejectsMissingSession(t *testing.T) {
	ctx := context.Background()
	players := mocks.NewMockPlayerSessionRepository(t)
	store := sessionmocks.NewMockStore(t)

	ps := newValidPlayerSessionFixture(t)
	players.EXPECT().GetByTokenHash(ctx, auth.HashSessionToken("tok-valid")).
		Return(ps, nil)
	store.EXPECT().Get(ctx, "sess-missing").
		Return(nil, newSessionNotFoundErr("sess-missing"))

	_, err := auth.ValidateSessionOwnership(ctx, players, store, "tok-valid", "sess-missing")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SESSION_NOT_FOUND")
}

func TestValidateSessionOwnershipRejectsForeignSession(t *testing.T) {
	ctx := context.Background()
	players := mocks.NewMockPlayerSessionRepository(t)
	store := sessionmocks.NewMockStore(t)

	ps := newValidPlayerSessionFixture(t) // PlayerID = playerAID
	foreign := &session.Info{ID: "sess-B", PlayerID: playerBID}

	players.EXPECT().GetByTokenHash(ctx, auth.HashSessionToken("tok-A")).
		Return(ps, nil)
	store.EXPECT().Get(ctx, "sess-B").Return(foreign, nil)

	_, err := auth.ValidateSessionOwnership(ctx, players, store, "tok-A", "sess-B")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SESSION_NOT_FOUND")
}

func TestValidateSessionOwnershipSucceedsForOwner(t *testing.T) {
	ctx := context.Background()
	players := mocks.NewMockPlayerSessionRepository(t)
	store := sessionmocks.NewMockStore(t)

	ps := newValidPlayerSessionFixture(t)
	owned := &session.Info{ID: "sess-A", PlayerID: ps.PlayerID}

	players.EXPECT().GetByTokenHash(ctx, auth.HashSessionToken("tok-A")).
		Return(ps, nil)
	store.EXPECT().Get(ctx, "sess-A").Return(owned, nil)

	got, err := auth.ValidateSessionOwnership(ctx, players, store, "tok-A", "sess-A")
	require.NoError(t, err)
	assert.Equal(t, owned, got)
}

func TestValidateSessionOwnershipCollapsesTokenLookupErrors(t *testing.T) {
	ctx := context.Background()
	players := mocks.NewMockPlayerSessionRepository(t)
	store := sessionmocks.NewMockStore(t)

	// Simulate a DB outage: GetByTokenHash returns a non-ErrNotFound error.
	dbErr := errors.New("connection refused")
	players.EXPECT().GetByTokenHash(ctx, auth.HashSessionToken("tok-valid")).
		Return(nil, dbErr)

	_, err := auth.ValidateSessionOwnership(ctx, players, store, "tok-valid", "sess-1")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SESSION_NOT_FOUND")
}

func TestValidateSessionOwnershipCollapsesSessionLookupErrors(t *testing.T) {
	ctx := context.Background()
	players := mocks.NewMockPlayerSessionRepository(t)
	store := sessionmocks.NewMockStore(t)

	ps := newValidPlayerSessionFixture(t)
	players.EXPECT().GetByTokenHash(ctx, auth.HashSessionToken("tok-A")).
		Return(ps, nil)

	dbErr := errors.New("query timeout")
	store.EXPECT().Get(ctx, "sess-A").Return(nil, dbErr)

	_, err := auth.ValidateSessionOwnership(ctx, players, store, "tok-A", "sess-A")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SESSION_NOT_FOUND")
}

func TestValidateSessionOwnershipLogsWarnOnOwnershipMismatch(t *testing.T) {
	ctx := context.Background()

	// Capture slog output for assertion.
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	players := mocks.NewMockPlayerSessionRepository(t)
	store := sessionmocks.NewMockStore(t)

	ps := newValidPlayerSessionFixture(t) // PlayerID = playerAID
	foreign := &session.Info{ID: "sess-B", PlayerID: playerBID}
	players.EXPECT().GetByTokenHash(ctx, auth.HashSessionToken("tok-A")).Return(ps, nil)
	store.EXPECT().Get(ctx, "sess-B").Return(foreign, nil)

	_, err := auth.ValidateSessionOwnership(ctx, players, store, "tok-A", "sess-B")
	require.Error(t, err)

	got := buf.String()
	assert.Contains(t, got, "level=WARN")
	assert.Contains(t, got, "session ownership mismatch")
	assert.Contains(t, got, playerAID.String()) // caller's player_id
	assert.Contains(t, got, playerBID.String()) // session_owner
	assert.Contains(t, got, "sess-B")           // session_id
}

func TestValidateSessionOwnershipDoesNotLogOnTokenFailures(t *testing.T) {
	ctx := context.Background()

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	players := mocks.NewMockPlayerSessionRepository(t)
	store := sessionmocks.NewMockStore(t)

	players.EXPECT().GetByTokenHash(ctx, auth.HashSessionToken("tok-bad")).
		Return(nil, auth.ErrNotFound)

	_, err := auth.ValidateSessionOwnership(ctx, players, store, "tok-bad", "sess-1")
	require.Error(t, err)

	// No ownership-mismatch log should fire for a bad-token path.
	assert.NotContains(t, buf.String(), "session ownership mismatch")
}

// Fixture helpers.

func newValidPlayerSessionFixture(t *testing.T) *auth.PlayerSession {
	t.Helper()
	ps, err := auth.NewPlayerSession(
		playerAID,
		auth.HashSessionToken("tok-A"),
		"test-agent", "127.0.0.1",
		auth.PlayerSessionTTL,
	)
	require.NoError(t, err)
	return ps
}

func newExpiredPlayerSessionFixture(t *testing.T) *auth.PlayerSession {
	t.Helper()
	ps := newValidPlayerSessionFixture(t)
	ps.ExpiresAt = time.Now().Add(-time.Hour)
	return ps
}
