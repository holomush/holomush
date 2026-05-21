// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package privacytest

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/require"

	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// Session wraps an authenticated or guest game session for privacy integration
// testing. It holds the session metadata set at connect time and exposes
// helpers that delegate to the in-process CoreServer.
//
// Fields marked as "set at connect; NOT mutated by MoveTo" preserve their
// original values so tests can compare before/after state.
type Session struct {
	server *Server

	// SessionID is the game session identifier returned by SelectCharacter.
	SessionID string
	// CharacterID is the ULID of the in-game character for this session.
	CharacterID ulid.ULID
	// CharacterName is the display name of the character.
	CharacterName string
	// LocationID is the current location. Updated by MoveTo.
	LocationID ulid.ULID
	// OriginalLocationID is the location at connect time; NOT mutated by MoveTo.
	OriginalLocationID ulid.ULID
	// LocationArrivedAt is the time the session arrived at the current location.
	LocationArrivedAt time.Time
	// SessionCreatedAt is the time the game session was created.
	SessionCreatedAt time.Time
	// LastReattachAt is the time of the most recent DetachTransport+ReattachTransport
	// cycle (zero if none has occurred).
	LastReattachAt time.Time

	// playerSessionToken is the raw bearer token for player-session auth.
	// Kept internal; used by SendCommand / Logout.
	playerSessionToken string
}

// SendCommand dispatches a text command via HandleCommand. Returns the RPC
// transport error if the call itself failed, or a wrapped error if the server
// rejected the command (resp.Success == false). Tests that expect rejection
// should assert on the returned error rather than ignoring it.
func (s *Session) SendCommand(ctx context.Context, cmd string) error {
	resp, err := s.server.coreServer.HandleCommand(ctx, &corev1.HandleCommandRequest{
		PlayerSessionToken: s.playerSessionToken,
		SessionId:          s.SessionID,
		Command:            cmd,
	})
	if err != nil {
		return oops.With("operation", "send_command").With("command", cmd).Wrap(err)
	}
	if !resp.GetSuccess() {
		return oops.Code("COMMAND_REJECTED").
			With("operation", "send_command").
			With("command", cmd).
			With("error_message", resp.GetError()).
			Errorf("command rejected by server: %s", resp.GetError())
	}
	return nil
}

// WaitForEvent blocks until an event matching eventType is received on the
// session's event stream, or the context is cancelled. Returns the first
// matching event.
//
// The underlying Subscribe stream is opened lazily on the first WaitForEvent
// call and shared across calls on the same Session.
//
// TODO(iwzt-9): wire the Subscribe goroutine and fan-out channel. For now
// this panics — downstream tests that need WaitForEvent must implement this
// body once iwzt-9 lands.
func (s *Session) WaitForEvent(_ context.Context, _ string) *corev1.EventFrame {
	s.server.t.Fatalf("privacytest.Session.WaitForEvent: TODO iwzt-9 — Subscribe goroutine not yet wired")
	return nil
}

// DrainEvents returns all events received within timeout. If no events arrive
// it returns nil (not an error). The smoke test calls this to exercise the
// event-delivery path; an empty result is acceptable.
//
// Honors ctx cancellation: returns immediately if ctx is done before the
// timeout elapses.
//
// Current implementation: waits up to timeout (or ctx cancellation), since
// the Subscribe goroutine fan-out is not yet wired (see TODO iwzt-9).
// Downstream tests that need real event delivery must implement the goroutine
// in that bead.
func (s *Session) DrainEvents(ctx context.Context, timeout time.Duration) []*corev1.EventFrame {
	select {
	case <-ctx.Done():
	case <-time.After(timeout):
	}
	return nil
}

// Logout logs out the player session, invalidating the bearer token and
// deleting the game session.
func (s *Session) Logout(ctx context.Context) {
	s.server.t.Helper()
	_, err := s.server.coreServer.Logout(ctx, &corev1.LogoutRequest{
		PlayerSessionToken: s.playerSessionToken,
	})
	require.NoError(s.server.t, err, "privacytest.Session.Logout")
}

// MoveTo moves the character to a new location. Updates LocationID.
// OriginalLocationID is never mutated.
//
// TODO(iwzt-9): invoke MoveCharacter RPC once the world movement path is
// wired into the test harness.
func (s *Session) MoveTo(_ context.Context, newLocationID ulid.ULID) {
	// Update the harness-side LocationID so tests can assert on it.
	s.LocationID = newLocationID
	s.server.t.Fatalf("privacytest.Session.MoveTo: TODO iwzt-9 — MoveCharacter RPC not yet wired")
}

// DetachTransport simulates a client disconnect by cancelling the Subscribe
// stream (e.g., client closed the connection). The session remains in
// StatusDetached in Postgres.
//
// TODO(iwzt-9): wire subscribe goroutine cancel.
func (s *Session) DetachTransport(_ context.Context) {
	s.server.t.Fatalf("privacytest.Session.DetachTransport: TODO iwzt-9 — Subscribe goroutine not yet wired")
}

// ReattachTransport reopens the Subscribe stream after a DetachTransport,
// recording LastReattachAt. Mirrors the client's reconnection flow.
//
// TODO(iwzt-9): wire subscribe goroutine restart.
func (s *Session) ReattachTransport(_ context.Context) {
	s.LastReattachAt = time.Now()
	s.server.t.Fatalf("privacytest.Session.ReattachTransport: TODO iwzt-9 — Subscribe goroutine not yet wired")
}

// CreateScene creates a new scene (focus session) and returns its ULID.
//
// TODO(iwzt-9): invoke FocusCoordinator.CreateScene once wired.
func (s *Session) CreateScene(_ context.Context) ulid.ULID {
	s.server.t.Fatalf("privacytest.Session.CreateScene: TODO iwzt-9 — scene creation RPC not yet wired")
	return ulid.ULID{}
}

// JoinScene joins the character to an existing scene.
//
// TODO(iwzt-9): invoke FocusCoordinator.JoinScene once wired.
func (s *Session) JoinScene(_ context.Context, _ ulid.ULID) {
	s.server.t.Fatalf("privacytest.Session.JoinScene: TODO iwzt-9 — scene join RPC not yet wired")
}

// QueryStreamHistory fetches the event history for the given stream subject.
// Returns the events from the response (may be empty if no history exists).
// The caller must pass a stream the session is authorized to read; access
// denials propagate as test failures via the returned error.
func (s *Session) QueryStreamHistory(ctx context.Context, stream string) ([]*corev1.EventFrame, error) {
	resp, err := s.server.coreServer.QueryStreamHistory(ctx, &corev1.QueryStreamHistoryRequest{
		SessionId: s.SessionID,
		Stream:    stream,
	})
	if err != nil {
		return nil, err
	}
	return resp.GetEvents(), nil
}

// ListFocusPresence calls the snapshot RPC for the session's current focus
// (location-scoped) and returns the response. The caller is responsible for
// asserting on response.Entries / response.Context / response.ContextId.
// Access denials propagate as test failures via the returned error.
func (s *Session) ListFocusPresence(ctx context.Context) (*corev1.ListFocusPresenceResponse, error) {
	return s.server.coreServer.ListFocusPresence(ctx, &corev1.ListFocusPresenceRequest{
		SessionId:          s.SessionID,
		PlayerSessionToken: s.playerSessionToken,
	})
}

// AuthedPlayer represents a named player (with hashed password) that can
// open multiple concurrent game sessions for the same character, used by
// multi-session continuity tests (iwzt.9+).
type AuthedPlayer struct {
	// PlayerID is the ULID of the player account.
	PlayerID ulid.ULID
	// CharacterID is the ULID of the player's primary character.
	CharacterID ulid.ULID
	// LocationID is the starting location of the character.
	LocationID ulid.ULID

	server *Server

	// rawToken is the active player-session bearer token.
	rawToken string
}

// OpenWebSession opens a new game session simulating a web (ConnectRPC) client.
//
// TODO(iwzt-9): implement multi-session open path.
func (p *AuthedPlayer) OpenWebSession(_ context.Context) *Session {
	p.server.t.Fatalf("privacytest.AuthedPlayer.OpenWebSession: TODO iwzt-9 — authed multi-session not yet wired")
	return nil
}

// OpenTelnetSession opens a new game session simulating a telnet client.
//
// TODO(iwzt-9): implement multi-session open path.
func (p *AuthedPlayer) OpenTelnetSession(_ context.Context) *Session {
	p.server.t.Fatalf("privacytest.AuthedPlayer.OpenTelnetSession: TODO iwzt-9 — authed multi-session not yet wired")
	return nil
}
