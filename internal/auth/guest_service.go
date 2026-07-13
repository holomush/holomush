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
//
// It deliberately no longer exposes Create: guest characters are created only
// through the CharacterGenesisService, which commits the character + binding +
// genesis envelope atomically (the compile-level fence, 05-15). Only the
// name-uniqueness read remains.
type GuestCharacterRepository interface {
	ExistsByName(ctx context.Context, name string) (bool, error)
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
	namer    GuestNamer
	players  PlayerRepository
	chars    GuestCharacterRepository
	sessions PlayerSessionRepository
	genesis  CharacterGenesis
	cleaner  GuestCleaner
}

// NewGuestService creates a new GuestService.
// Returns an error if any required dependency is nil.
//
// cleaner is the tombstone-emitting CharacterReapingService (05-16 / round-5
// D-06): failed-guest cleanup routes character deletion through it so a
// partially-created guest's character is tombstoned through the world boundary
// before the player is deleted — never removed by a silent FK cascade.
func NewGuestService(
	namer GuestNamer,
	players PlayerRepository,
	chars GuestCharacterRepository,
	sessions PlayerSessionRepository,
	genesis CharacterGenesis,
	cleaner GuestCleaner,
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
	if genesis == nil {
		return nil, oops.Errorf("character genesis service is required")
	}
	if cleaner == nil {
		return nil, oops.Errorf("guest cleaner is required")
	}
	return &GuestService{
		namer:    namer,
		players:  players,
		chars:    chars,
		sessions: sessions,
		genesis:  genesis,
		cleaner:  cleaner,
	}, nil
}

// CreateGuest creates an ephemeral guest player with a character and session.
//
// Ordering (round-4 B4): the guest PLAYER is committed FIRST on its own pool
// (auth/postgres.PlayerRepository does not enroll in the world transaction), so
// the character's player_id FK targets a committed row. Then the CharacterGenesis
// service commits the character + initial_bind_guest binding + genesis envelope
// ATOMICALLY (the sound narrow atomic unit — an outer rollback removes those
// three together). The player is NOT part of that transaction: if genesis fails
// AFTER the player commit, an orphan guest player remains (no character) — an
// accepted, documented compensation gap reconciled by re-run / guest cleanup;
// it is OUTSIDE INV-WORLD-4 (which binds the character↔genesis-envelope pairing).
// Session creation is outside the genesis transaction (separate concern).
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

	// Commit the guest player FIRST (its own pool) so the character's player_id
	// FK targets a committed row (round-4 B4 ordering).
	if pErr := s.players.Create(ctx, player); pErr != nil {
		s.namer.ReleaseGuest(name)
		return nil, oops.Code("GUEST_CREATE_FAILED").With("player_id", player.ID.String()).Wrap(pErr)
	}

	// Then create the character + initial_bind_guest binding + genesis envelope
	// ATOMICALLY through the genesis service (the narrow sound atomic unit). On
	// failure the character/binding/envelope roll back together; the already-
	// committed player is cleaned up best-effort (orphan-player compensation).
	if gErr := s.genesis.Create(ctx, char, "initial_bind_guest"); gErr != nil {
		s.namer.ReleaseGuest(name)
		s.cleanupGuestPlayer(ctx, player.ID) // best-effort orphan-player compensation
		return nil, oops.Code("GUEST_CREATE_FAILED").With("name", name).Wrap(gErr)
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

// cleanupGuestPlayer best-effort cleans up an orphaned/partial guest player
// through the tombstone-emitting reaping service (round-5 D-06): each of the
// guest's characters is deleted through the world CharacterWriter.Delete AND a
// character_deleted tombstone envelope, then the player is deleted — so a
// character committed by a successful genesis but abandoned by a later
// token/session failure never leaves the feed via genesis-without-tombstone.
// (For a genesis that failed before committing a character, the reaping service
// simply marks + deletes the player with zero characters to tombstone.)
// Best-effort: failures are logged, not propagated.
func (s *GuestService) cleanupGuestPlayer(ctx context.Context, playerID ulid.ULID) {
	if err := s.cleaner.DeleteGuestPlayer(ctx, playerID); err != nil {
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
