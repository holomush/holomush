// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
	"context"
	"errors"
	"log/slog"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// Service provides authentication operations.
type Service struct {
	players              PlayerRepository
	playerSessions       PlayerSessionRepository
	hasher               PasswordHasher
	logger               *slog.Logger
	maxSessionsPerPlayer int
}

// NewAuthService creates a new Service with a no-op logger.
// Returns an error if any required dependency is nil.
// Session cap enforcement is disabled (use SetMaxSessionsPerPlayer to enable).
func NewAuthService(players PlayerRepository, playerSessions PlayerSessionRepository, hasher PasswordHasher) (*Service, error) {
	if players == nil {
		return nil, oops.Errorf("players repository is required")
	}
	if playerSessions == nil {
		return nil, oops.Errorf("player sessions repository is required")
	}
	if hasher == nil {
		return nil, oops.Errorf("password hasher is required")
	}
	return &Service{
		players:        players,
		playerSessions: playerSessions,
		hasher:         hasher,
		logger:         slog.New(slog.DiscardHandler),
	}, nil
}

// NewAuthServiceWithLogger creates a new Service with the provided logger.
// Returns an error if any required dependency is nil.
// Session cap enforcement is disabled (use SetMaxSessionsPerPlayer to enable).
func NewAuthServiceWithLogger(players PlayerRepository, playerSessions PlayerSessionRepository, hasher PasswordHasher, logger *slog.Logger) (*Service, error) {
	if players == nil {
		return nil, oops.Errorf("players repository is required")
	}
	if playerSessions == nil {
		return nil, oops.Errorf("player sessions repository is required")
	}
	if hasher == nil {
		return nil, oops.Errorf("password hasher is required")
	}
	if logger == nil {
		return nil, oops.Errorf("logger is required")
	}
	return &Service{
		players:        players,
		playerSessions: playerSessions,
		hasher:         hasher,
		logger:         logger,
	}, nil
}

// SetMaxSessionsPerPlayer configures the per-player active session cap.
// A value <= 0 disables cap enforcement. When enabled, AuthenticatePlayer
// will evict the oldest active PlayerSession for the player before creating
// a new one whenever the player already has maxSessionsPerPlayer active
// sessions.
func (s *Service) SetMaxSessionsPerPlayer(n int) {
	s.maxSessionsPerPlayer = n
}

// dummyPasswordHash is used when a user doesn't exist to prevent timing attacks.
// We still run password verification to make response time consistent.
// This is NOT a real credential - it's a fake hash that will never match any password.
//
// SECURITY: The parameters in this hash (m=65536,t=1,p=4) MUST match the real argon2id
// parameters in hasher.go. If they differ, an attacker could distinguish non-existent
// users from real users by measuring response time differences.
//
//nolint:gosec // G101: This is an intentionally fake hash for timing attack prevention, not a credential.
const dummyPasswordHash = "$argon2id$v=19$m=65536,t=1,p=4$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// AuthenticatePlayer validates credentials and creates a new PlayerSession.
// When the per-player session cap is enabled (maxSessionsPerPlayer > 0),
// CreateWithCap atomically inserts the new session and trims the oldest
// non-expired sessions so the total active count is at most the configured
// cap. The INSERT + trim run in a single transaction which closes three
// correctness gaps of a separate Count + DeleteOldest + Create flow:
//
//   - Two concurrent logins at cap each observe count == cap, each evict
//     once, each insert, leaving the player at cap + 1.
//   - Lowering the operator-configured cap below the current count only
//     trims one session per login instead of catching up.
//   - A Create failure after a successful eviction silently strands the
//     player below cap with no replacement session.
//
// The ON DELETE CASCADE on sessions.player_session_id still ensures game
// sessions spawned by a trimmed PlayerSession are removed atomically,
// terminating their Subscribe streams.
//
// Returns the raw (plaintext) session token and the authenticated Player on
// success. The caller is responsible for returning the raw token to the client
// exactly once; only the hash is persisted server-side.
func (s *Service) AuthenticatePlayer(ctx context.Context, username, password, userAgent, ipAddress string) (string, *Player, error) {
	player, err := s.ValidateCredentials(ctx, username, password)
	if err != nil {
		// ValidateCredentials already produces oops errors with codes
		// (AUTH_INVALID_CREDENTIALS, AUTH_ACCOUNT_LOCKED, AUTH_LOGIN_FAILED);
		// preserve them verbatim so callers can discriminate on code.
		return "", nil, err
	}

	rawToken, tokenHash, err := GenerateSessionToken()
	if err != nil {
		return "", nil, oops.Code("AUTH_LOGIN_FAILED").
			With("operation", "generate session token").
			Wrap(err)
	}

	session, err := NewPlayerSession(player.ID, tokenHash, userAgent, ipAddress, PlayerSessionTTL)
	if err != nil {
		return "", nil, oops.Code("AUTH_LOGIN_FAILED").
			With("operation", "create player session").
			Wrap(err)
	}

	trimmed, err := s.playerSessions.CreateWithCap(ctx, session, s.maxSessionsPerPlayer)
	if err != nil {
		return "", nil, oops.Code("AUTH_LOGIN_FAILED").
			With("operation", "persist player session with cap").
			Wrap(err)
	}
	if trimmed > 0 {
		s.logger.InfoContext(ctx, "session cap trimmed oldest sessions",
			"event", "session_cap_trimmed",
			"player_id", player.ID.String(),
			"trimmed_count", trimmed,
			"cap", s.maxSessionsPerPlayer,
		)
	}

	return rawToken, player, nil
}

// Logout invalidates a player session by token hash.
// Returns the player ID of the logged-out session.
func (s *Service) Logout(ctx context.Context, tokenHash string) (ulid.ULID, error) {
	session, err := s.playerSessions.GetByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ulid.ULID{}, oops.Code("SESSION_NOT_FOUND").
				With("operation", "get session by token hash").
				Wrap(err)
		}
		return ulid.ULID{}, oops.Code("AUTH_LOGOUT_FAILED").
			With("operation", "get session by token hash").
			Wrap(err)
	}

	if err := s.playerSessions.Delete(ctx, session.ID); err != nil {
		return ulid.ULID{}, oops.Code("AUTH_LOGOUT_FAILED").
			With("operation", "delete session").
			With("session_id", session.ID.String()).
			Wrap(err)
	}

	return session.PlayerID, nil
}
