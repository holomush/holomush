// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/world"
)

// GuestSessionTTL is the time-to-live for guest player sessions.
const GuestSessionTTL = 2 * time.Hour

// maxGuestNameRetries is the maximum number of times to retry name generation
// when a name already exists in the database.
const maxGuestNameRetries = 10

// GuestNamer generates unique themed names for guest characters.
type GuestNamer interface {
	GenerateName() (string, error)
	ReleaseGuest(name string)
	StartLocation() ulid.ULID
}

// GuestCharacterRepository is the subset of character repo needed by GuestService.
type GuestCharacterRepository interface {
	Create(ctx context.Context, char *world.Character) error
	ExistsByName(ctx context.Context, name string) (bool, error)
}

// GuestTransactor begins a database transaction and calls fn with a
// Tx-scoped context. Commit on success, rollback on error.
type GuestTransactor interface {
	InTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// GuestBindingCreator creates a new active player↔character binding.
// Returns the new binding ID.
type GuestBindingCreator interface {
	Create(ctx context.Context, playerID, characterID, reason string) (string, error)
}

// GuestResult holds everything created during guest account setup.
type GuestResult struct {
	Player        *Player
	PlayerSession *PlayerSession
	RawToken      string
	Character     *world.Character
}

// GuestService creates ephemeral guest players.
type GuestService struct {
	namer      GuestNamer
	players    PlayerRepository
	chars      GuestCharacterRepository
	sessions   PlayerSessionRepository
	transactor GuestTransactor
	bindings   GuestBindingCreator
}

// NewGuestService creates a new GuestService.
// Returns an error if any required dependency is nil.
func NewGuestService(
	namer GuestNamer,
	players PlayerRepository,
	chars GuestCharacterRepository,
	sessions PlayerSessionRepository,
	transactor GuestTransactor,
	bindings GuestBindingCreator,
) (*GuestService, error) {
	if namer == nil {
		return nil, oops.Errorf("guest namer is required")
	}
	if players == nil {
		return nil, oops.Errorf("players repository is required")
	}
	if chars == nil {
		return nil, oops.Errorf("character repository is required")
	}
	if sessions == nil {
		return nil, oops.Errorf("player sessions repository is required")
	}
	if transactor == nil {
		return nil, oops.Errorf("transactor is required")
	}
	if bindings == nil {
		return nil, oops.Errorf("binding creator is required")
	}
	return &GuestService{
		namer:      namer,
		players:    players,
		chars:      chars,
		sessions:   sessions,
		transactor: transactor,
		bindings:   bindings,
	}, nil
}

// CreateGuest creates an ephemeral guest player with a character and session.
// Player, character, and binding creation are atomic (single transaction).
// Session creation is outside the transaction (separate concern).
func (s *GuestService) CreateGuest(ctx context.Context) (*GuestResult, error) {
	// Generate a unique name not already in the database.
	name, err := s.acquireUniqueName(ctx)
	if err != nil {
		return nil, err
	}

	// From here on, if we fail before persisting successfully we must release the name.
	player, err := NewGuestPlayer(name)
	if err != nil {
		s.namer.ReleaseGuest(name)
		return nil, oops.Code("GUEST_CREATE_FAILED").With("name", name).Wrap(err)
	}

	startLoc := s.namer.StartLocation()
	// Guest names from the namer are underscore-separated (e.g. "Sapphire_Diamond").
	// world.Character names must be letters and spaces only, so convert for display.
	charName := strings.ReplaceAll(name, "_", " ")
	char, err := world.NewCharacter(player.ID, charName)
	if err != nil {
		s.namer.ReleaseGuest(name)
		return nil, oops.Code("GUEST_CREATE_FAILED").With("name", name).Wrap(err)
	}
	char.LocationID = &startLoc

	// Atomically create player, character, and binding in a single transaction.
	// If any step fails, all three are rolled back — no orphan rows.
	if txErr := s.transactor.InTransaction(ctx, func(txCtx context.Context) error {
		if pErr := s.players.Create(txCtx, player); pErr != nil {
			return oops.Code("GUEST_CREATE_FAILED").With("player_id", player.ID.String()).Wrap(pErr)
		}
		if cErr := s.chars.Create(txCtx, char); cErr != nil {
			return oops.Code("GUEST_CREATE_FAILED").With("character_id", char.ID.String()).Wrap(cErr)
		}
		if _, bErr := s.bindings.Create(txCtx, player.ID.String(), char.ID.String(), "initial_bind_guest"); bErr != nil {
			return oops.Code("CHARACTER_CREATE_BINDING_FAILED").Wrap(bErr)
		}
		return nil
	}); txErr != nil {
		s.namer.ReleaseGuest(name)
		return nil, oops.Code("GUEST_CREATE_FAILED").With("name", name).Wrap(txErr)
	}

	// Best-effort: update the player's default character.
	// This is non-critical — guests can still log in even if this update fails.
	player.DefaultCharacterID = &char.ID
	if err = s.players.Update(ctx, player); err != nil {
		slog.WarnContext(
			ctx,
			"guest_service: failed to set default character on guest player",
			"player_id", player.ID.String(),
			"character_id", char.ID.String(),
			"error", err,
		)
	}

	rawToken, tokenHash, err := GenerateSessionToken()
	if err != nil {
		s.namer.ReleaseGuest(name)
		s.cleanupGuestPlayer(ctx, player.ID) // best-effort
		return nil, oops.Code("GUEST_CREATE_FAILED").With("player_id", player.ID.String()).Wrap(err)
	}

	session, err := NewPlayerSession(player.ID, tokenHash, "", "", GuestSessionTTL)
	if err != nil {
		s.namer.ReleaseGuest(name)
		s.cleanupGuestPlayer(ctx, player.ID) // best-effort
		return nil, oops.Code("GUEST_CREATE_FAILED").With("player_id", player.ID.String()).Wrap(err)
	}

	if err = s.sessions.Create(ctx, session); err != nil {
		s.namer.ReleaseGuest(name)
		s.cleanupGuestPlayer(ctx, player.ID) // best-effort
		return nil, oops.Code("GUEST_CREATE_FAILED").With("session_id", session.ID.String()).Wrap(err)
	}

	return &GuestResult{
		Player:        player,
		PlayerSession: session,
		RawToken:      rawToken,
		Character:     char,
	}, nil
}

// cleanupGuestPlayer best-effort deletes an orphaned guest player and its
// cascaded dependents (characters, player_sessions via FK CASCADE).
func (s *GuestService) cleanupGuestPlayer(ctx context.Context, playerID ulid.ULID) {
	if err := s.players.Delete(ctx, playerID); err != nil {
		slog.WarnContext(ctx, "guest_service: failed to clean up orphaned guest player",
			"player_id", playerID.String(), "error", err)
	}
}

// acquireUniqueName generates a guest name that is not already present in the
// database, retrying up to maxGuestNameRetries times.
// Returns the raw namer name (underscore form), which the caller converts to
// a character display name as needed.
func (s *GuestService) acquireUniqueName(ctx context.Context) (string, error) {
	for range maxGuestNameRetries {
		name, err := s.namer.GenerateName()
		if err != nil {
			return "", oops.Code("GUEST_NAME_GENERATE_FAILED").Wrap(err)
		}

		// Character names are stored with spaces; check using the display form.
		charName := strings.ReplaceAll(name, "_", " ")
		exists, err := s.chars.ExistsByName(ctx, charName)
		if err != nil {
			s.namer.ReleaseGuest(name)
			return "", oops.Code("GUEST_CREATE_FAILED").With("name", name).Wrap(err)
		}
		if !exists {
			return name, nil
		}

		// Name exists in DB from a previous server run — release and try again.
		s.namer.ReleaseGuest(name)
	}

	return "", oops.Code("GUEST_NAME_EXHAUSTED").
		With("retries", maxGuestNameRetries).
		Errorf("unable to find unique guest name after %d attempts", maxGuestNameRetries)
}
