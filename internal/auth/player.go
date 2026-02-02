// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
	"context"
	"regexp"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// DefaultMaxCharacters is the default character limit per player.
const DefaultMaxCharacters = 5

// Username validation constraints.
const (
	MinUsernameLength = 3
	MaxUsernameLength = 30
)

// usernameRegex matches usernames that:
// - Start with a letter (a-z, A-Z)
// - Contain only letters, numbers, and underscores
var usernameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)

// Player represents a player account.
type Player struct {
	ID                 ulid.ULID
	Username           string
	PasswordHash       string
	Email              *string
	EmailVerified      bool
	FailedAttempts     int
	LockedUntil        *time.Time
	DefaultCharacterID *ulid.ULID
	Preferences        PlayerPreferences
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// PlayerPreferences contains player-specific settings.
type PlayerPreferences struct {
	AutoLogin     bool   `json:"auto_login,omitempty"`
	MaxCharacters int    `json:"max_characters,omitempty"`
	Theme         string `json:"theme,omitempty"`
}

// EffectiveMaxCharacters returns the character limit, using default if not set.
func (p PlayerPreferences) EffectiveMaxCharacters() int {
	if p.MaxCharacters <= 0 {
		return DefaultMaxCharacters
	}
	return p.MaxCharacters
}

// IsLocked returns true if the player is currently locked out.
func (p *Player) IsLocked() bool {
	return IsLockedOut(p.LockedUntil)
}

// RecordFailure increments the failure counter and sets lockout if threshold reached.
func (p *Player) RecordFailure() {
	p.FailedAttempts++
	p.LockedUntil = ComputeLockoutTime(p.FailedAttempts)
	p.UpdatedAt = time.Now()
}

// RecordSuccess resets failure counter and lockout.
func (p *Player) RecordSuccess() {
	p.FailedAttempts = 0
	p.LockedUntil = nil
	p.UpdatedAt = time.Now()
}

// ValidateUsername validates a username against rules.
// Username requirements:
// - Length: MinUsernameLength to MaxUsernameLength characters
// - Must start with a letter
// - Can contain only letters (a-z, A-Z), numbers (0-9), and underscores (_)
func ValidateUsername(username string) error {
	if username == "" {
		return oops.Code("AUTH_INVALID_USERNAME").Errorf("username cannot be empty")
	}
	if len(username) < MinUsernameLength {
		return oops.Code("AUTH_INVALID_USERNAME").
			With("min", MinUsernameLength).
			Errorf("username must be at least %d characters", MinUsernameLength)
	}
	if len(username) > MaxUsernameLength {
		return oops.Code("AUTH_INVALID_USERNAME").
			With("max", MaxUsernameLength).
			Errorf("username must be at most %d characters", MaxUsernameLength)
	}
	if !usernameRegex.MatchString(username) {
		return oops.Code("AUTH_INVALID_USERNAME").
			Errorf("username must start with a letter and contain only letters, numbers, and underscores")
	}
	return nil
}

// PlayerRepository manages player persistence.
type PlayerRepository interface {
	// Create stores a new player.
	Create(ctx context.Context, player *Player) error

	// GetByID retrieves a player by ID.
	GetByID(ctx context.Context, id ulid.ULID) (*Player, error)

	// GetByUsername retrieves a player by username (case-insensitive).
	GetByUsername(ctx context.Context, username string) (*Player, error)

	// GetByEmail retrieves a player by email (case-insensitive).
	// Returns ErrNotFound if no player has the given email.
	GetByEmail(ctx context.Context, email string) (*Player, error)

	// Update updates an existing player.
	Update(ctx context.Context, player *Player) error

	// UpdatePassword updates only the password hash for a player.
	UpdatePassword(ctx context.Context, id ulid.ULID, passwordHash string) error

	// Delete removes a player.
	Delete(ctx context.Context, id ulid.ULID) error
}
