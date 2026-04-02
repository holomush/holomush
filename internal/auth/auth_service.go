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
	players        PlayerRepository
	playerSessions PlayerSessionRepository
	hasher         PasswordHasher
	logger         *slog.Logger
}

// NewAuthService creates a new Service with a no-op logger.
// Returns an error if any required dependency is nil.
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
