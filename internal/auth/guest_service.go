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

	"github.com/holomush/holomush/internal/charname"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/pkg/errutil"
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
	// ExistsByNormalizedName is the §6.1.1 uniqueness pre-check — a UX
	// affordance, not the guarantee, which is migration 000056's UNIQUE index.
	// excluding is nil on every creation path.
	ExistsByNormalizedName(ctx context.Context, key string, excluding *ulid.ULID) (bool, error)
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

	// gate is the SAME character-name admission decision the registered create
	// path runs. Guest provisioning is automatic and high-volume and never ran
	// the pipeline at all before this plan; a gate installed only in
	// CharacterService would have left it open (T-02-31).
	gate *charname.Gate
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
	gate *charname.Gate,
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
	if gate == nil {
		return nil, oops.Errorf("character name gate is required")
	}
	return &GuestService{
		namer:    namer,
		players:  players,
		chars:    chars,
		sessions: sessions,
		genesis:  genesis,
		cleaner:  cleaner,
		gate:     gate,
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
	// Generate a unique, ADMITTED name not already in the database. The token
	// comes back with the raw name so it has a path out of the retry loop.
	name, admitted, err := s.acquireUniqueName(ctx)
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
	// The display name is the TOKEN's, never re-derived here. Re-deriving it
	// from the raw name would be a second normalization of a string the gate
	// already normalized — exactly what charname.Admitted exists to prevent.
	char, err := world.NewCharacter(player.ID, admitted.Display())
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
	if gErr := s.genesis.Create(ctx, char, admitted, "initial_bind_guest"); gErr != nil {
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
// It returns the raw namer name (underscore form) AND the admission token minted
// for its display form. The token is a return value rather than something the
// caller re-derives: this path is automatic and high-volume, and a gate
// installed only in CharacterService would have left it wide open (T-02-31).
func (s *GuestService) acquireUniqueName(ctx context.Context) (rawName string, admitted charname.Admitted, err error) {
	// lastRefusal carries the most recent per-candidate gate refusal into the
	// exhaustion error, so GUEST_NAME_EXHAUSTED names WHY every candidate was
	// rejected instead of reporting a bare count.
	var lastRefusal error

	for range maxGuestNameRetries {
		name, genErr := s.namer.GenerateName()
		if genErr != nil {
			return "", charname.Admitted{}, oops.Code("GUEST_NAME_GENERATE_FAILED").Wrap(genErr)
		}

		// Namer names are underscore-separated (e.g. "Sapphire_Diamond"); the
		// display form uses spaces. This is the ONLY place that conversion
		// happens — everything downstream reads the token.
		charName := strings.ReplaceAll(name, "_", " ")

		token, admitErr := s.gate.Admit(ctx, charName)
		if admitErr != nil {
			s.namer.ReleaseGuest(name)

			// Not every gate refusal is about THIS candidate, and retrying the
			// ones that are not is how a database outage came to be reported as
			// name exhaustion. NAME_SKELETON_LOOKUP_FAILED (the corpus query
			// failed) and NAME_SKELETON_UNVERIFIABLE (the documented fail-closed
			// window between migrations 000054 and 000055, where some row still
			// has a NULL skeleton) refuse EVERY candidate identically, so the
			// loop burns all ten attempts and returns GUEST_NAME_EXHAUSTED —
			// "unable to find unique guest name" — for a fault that has nothing
			// to do with names. Surface those immediately, with the real cause
			// attached.
			if isCorpusUnavailable(admitErr) {
				return "", charname.Admitted{}, oops.Code("GUEST_CREATE_FAILED").
					With("name", name).
					Hint("the character-name corpus could not be adjudicated; this is not name exhaustion").
					Wrap(admitErr)
			}

			// A genuine per-candidate refusal — a block-list pattern matches it,
			// or its skeleton collides with a live character. Retrying a
			// different candidate can succeed, so the loop continues and
			// exhaustion keeps its EXISTING caller-visible contract. It is
			// logged rather than discarded: a block-list pattern broad enough to
			// match most generated names produces exactly this, and it was
			// previously invisible.
			lastRefusal = admitErr
			slog.DebugContext(ctx, "guest name candidate refused by the name gate",
				"candidate", charName, "code", refusalCode(admitErr))
			continue
		}

		exists, existsErr := s.chars.ExistsByNormalizedName(ctx, token.Key(), nil)
		if existsErr != nil {
			s.namer.ReleaseGuest(name)
			return "", charname.Admitted{}, oops.Code("GUEST_CREATE_FAILED").With("name", name).Wrap(existsErr)
		}
		if !exists {
			return name, token, nil
		}

		// Name exists in DB from a previous server run — release and try again.
		s.namer.ReleaseGuest(name)
	}

	exhausted := oops.Code("GUEST_NAME_EXHAUSTED").With("retries", maxGuestNameRetries)
	if lastRefusal != nil {
		exhausted = exhausted.With("last_refusal_code", refusalCode(lastRefusal))
		errutil.LogErrorContext(ctx, "guest name generation exhausted its retries", lastRefusal,
			"retries", maxGuestNameRetries)
	}
	return "", charname.Admitted{}, exhausted.
		Errorf("unable to find unique guest name after %d attempts", maxGuestNameRetries)
}

// corpusUnavailableCodes are the Gate.Admit refusals that are properties of the
// CORPUS rather than of the candidate. Retrying a different name cannot clear
// either of them.
var corpusUnavailableCodes = map[string]struct{}{
	"NAME_SKELETON_LOOKUP_FAILED": {},
	"NAME_SKELETON_UNVERIFIABLE":  {},
}

// isCorpusUnavailable reports whether a gate refusal means the corpus could not
// be adjudicated at all.
func isCorpusUnavailable(err error) bool {
	_, unavailable := corpusUnavailableCodes[refusalCode(err)]
	return unavailable
}

// refusalCode extracts an error's oops code, or "" when it carries none.
func refusalCode(err error) string {
	if err == nil {
		return ""
	}
	oopsErr, ok := oops.AsOops(err)
	if !ok {
		return ""
	}
	code, ok := oopsErr.Code().(string)
	if !ok {
		return ""
	}
	return code
}
