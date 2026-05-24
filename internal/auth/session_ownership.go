// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
	"context"
	"errors"
	"log/slog"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/session"
)

// sessionNotFoundErr is the single error code returned for ANY validation
// failure in ValidateSessionOwnership. Collapsing "token invalid",
// "session missing", and "ownership mismatch" into one response prevents
// attackers from distinguishing enumeration probes from authentic lookups.
const sessionNotFoundErr = "SESSION_NOT_FOUND"

// ValidateSessionOwnership verifies that a player_session_token is valid
// (exists, not expired) AND that it grants ownership of the target game
// session. On success, returns the loaded session.Info so callers don't
// need a second lookup.
//
// SECURITY: All failure paths return oops.Code("SESSION_NOT_FOUND") to
// prevent enumeration of valid session IDs or valid tokens. Ownership
// mismatches are logged at WARN server-side for security monitoring.
//
// The session package does not export a sentinel ErrNotFound; instead
// Store.Get returns an oops error with code "SESSION_NOT_FOUND" on miss.
// We detect that via oops.AsOops rather than errors.Is.
func ValidateSessionOwnership(
	ctx context.Context,
	playerSessions PlayerSessionRepository,
	sessions session.Store,
	playerToken string,
	sessionID string,
) (*session.Info, error) {
	if playerToken == "" {
		return nil, oops.Code(sessionNotFoundErr).
			With("reason", "empty_token").Errorf("session not found")
	}

	ps, err := playerSessions.GetByTokenHash(ctx, HashSessionToken(playerToken))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, oops.Code(sessionNotFoundErr).
				With("reason", "token_unknown").Errorf("session not found")
		}
		return nil, oops.Code(sessionNotFoundErr).
			With("reason", "token_lookup_failed").Wrap(err)
	}

	if ps.IsExpired() {
		return nil, oops.Code(sessionNotFoundErr).
			With("reason", "token_expired").
			With("player_id", ps.PlayerID.String()).
			Errorf("session not found")
	}

	info, err := sessions.Get(ctx, sessionID)
	if err != nil {
		if isSessionNotFound(err) {
			return nil, oops.Code(sessionNotFoundErr).
				With("reason", "session_missing").
				With("player_id", ps.PlayerID.String()).
				Errorf("session not found")
		}
		return nil, oops.Code(sessionNotFoundErr).
			With("reason", "session_lookup_failed").Wrap(err)
	}

	if info.PlayerID.Compare(ps.PlayerID) != 0 {
		slog.WarnContext(
			ctx, "session ownership mismatch",
			"player_id", ps.PlayerID.String(),
			"session_id", sessionID,
			"session_owner", info.PlayerID.String(),
		)
		return nil, oops.Code(sessionNotFoundErr).
			With("reason", "ownership_mismatch").
			With("player_id", ps.PlayerID.String()).
			Errorf("session not found")
	}

	return info, nil
}

// isSessionNotFound reports whether err is a "session not found" error
// returned by session.Store implementations. All Store implementations
// tag the error with oops.Code("SESSION_NOT_FOUND"),
// so we detect that rather than comparing to a sentinel.
func isSessionNotFound(err error) bool {
	oopsErr, ok := oops.AsOops(err)
	if !ok {
		return false
	}
	return oopsErr.Code() == sessionNotFoundErr
}
