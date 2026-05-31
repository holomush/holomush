// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/idgen"
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

// NewPlayer creates a new Player with validated inputs.
// Username is validated using ValidateUsername rules.
// Email is optional (nil allowed).
// PasswordHash must be non-empty and non-whitespace.
func NewPlayer(username string, email *string, passwordHash string) (*Player, error) {
	if err := ValidateUsername(username); err != nil {
		return nil, err
	}
	if strings.TrimSpace(passwordHash) == "" {
		return nil, oops.Code("AUTH_INVALID_PASSWORD").Errorf("password hash cannot be empty")
	}

	now := time.Now().UTC()
	return &Player{
		ID:           idgen.New(),
		Username:     username,
		Email:        email,
		PasswordHash: passwordHash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

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
	IsGuest            bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// NewGuestPlayer creates an ephemeral guest player. Guests have no password
// or email — their identity is the auto-generated username (themed character name).
func NewGuestPlayer(username string) (*Player, error) {
	if username == "" {
		return nil, oops.Code("AUTH_INVALID_USERNAME").Errorf("guest username cannot be empty")
	}
	now := time.Now().UTC()
	return &Player{
		ID:        idgen.New(),
		Username:  username,
		IsGuest:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// PlayerPreferences contains player-specific settings.
type PlayerPreferences struct {
	AutoLogin     bool                   `json:"auto_login,omitempty"`
	MaxCharacters int                    `json:"max_characters,omitempty"`
	Theme         string                 `json:"theme,omitempty"`
	Scenes        ScenePlayerPreferences `json:"scenes,omitempty"`
	// Plugins is an opaque, plugin-partitioned settings bag. The host never
	// interprets its contents (INV-10); each key is a plugin name and
	// each value is that plugin's serialized settings partition. Whole-struct
	// JSON (de)marshaling carries it to/from the players.preferences JSONB
	// column alongside the typed fields above, so the bag and the typed fields
	// round-trip together without clobbering one another.
	Plugins map[string]json.RawMessage `json:"plugins,omitempty"`
}

// ScenePlayerPreferences holds scene-related per-player configuration.
type ScenePlayerPreferences struct {
	// FocusReplayTail is the number of most recent IC contributions
	// replayed to the session when it focus-switches into a scene.
	// Pointer type distinguishes "unset" (nil) from "explicitly 0
	// (disable catch-up)."
	FocusReplayTail *int `json:"focus_replay_tail,omitempty"`
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

	// Count returns the total number of players.
	Count(ctx context.Context) (int, error)

	// Update updates an existing player.
	Update(ctx context.Context, player *Player) error

	// UpdatePassword updates only the password hash for a player.
	UpdatePassword(ctx context.Context, id ulid.ULID, passwordHash string) error

	// UpdatePasswordAndClearLockout atomically updates the password hash and
	// clears lockout state (failed_attempts = 0, locked_until = NULL).
	UpdatePasswordAndClearLockout(ctx context.Context, id ulid.ULID, passwordHash string) error

	// Delete removes a player.
	Delete(ctx context.Context, id ulid.ULID) error

	// ListIdleGuests returns guest players whose updated_at is before idleSince.
	ListIdleGuests(ctx context.Context, idleSince time.Time) ([]*Player, error)

	// DeleteGuestPlayer removes a guest player. The is_guest=true guard prevents
	// accidental deletion of registered players. FK cascades delete characters
	// and player sessions.
	DeleteGuestPlayer(ctx context.Context, playerID ulid.ULID) error
}
