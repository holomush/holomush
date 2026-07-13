// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
	"context"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/world"
)

// CharacterRepository defines the READ persistence operations needed by
// CharacterService (name uniqueness, player queries, directory listing).
//
// It deliberately no longer exposes Create: the ONLY way to insert a character
// is the CharacterGenesisService, which commits the character + optional binding
// + genesis envelope atomically (INV-WORLD-4). This is the compile-level fence —
// no production package can create an envelope-less character (05-15).
type CharacterRepository interface {
	// ExistsByName checks if a character with the given name exists (case-insensitive).
	ExistsByName(ctx context.Context, name string) (bool, error)

	// CountByPlayer returns the number of characters owned by a player.
	CountByPlayer(ctx context.Context, playerID ulid.ULID) (int, error)

	// ListByPlayer returns all characters owned by a player.
	ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*world.Character, error)

	// ListAll returns ALL characters (id + name only) for the directory picker —
	// fetch-all, NO pagination, ordered by name ascending. Names only; no
	// connection state. Backs the membership-invite character directory.
	ListAll(ctx context.Context) ([]*world.Character, error)
}

// LocationRepository defines the location operations needed by CharacterService.
type LocationRepository interface {
	// GetStartingLocation returns the default location for new characters.
	GetStartingLocation(ctx context.Context) (*world.Location, error)
}

// CharacterGenesis is the atomic character-creation primitive CharacterService
// delegates persistence to. Its Create commits the character row, an optional
// player↔character binding (empty bindReason = no binding), and the
// character-genesis envelope in one transaction (INV-WORLD-4). Satisfied by
// *CharacterGenesisService.
type CharacterGenesis interface {
	Create(ctx context.Context, char *world.Character, bindReason string) error
}

// CharacterService handles character creation and management. It owns the
// validation pipeline (normalize, uniqueness, limit, starting location) and
// delegates the actual persistence + genesis envelope to CharacterGenesis.
type CharacterService struct {
	charRepo CharacterRepository
	locRepo  LocationRepository
	genesis  CharacterGenesis
}

// NewCharacterService creates a new CharacterService.
// Returns an error if any required dependency is nil.
func NewCharacterService(charRepo CharacterRepository, locRepo LocationRepository, genesis CharacterGenesis) (*CharacterService, error) {
	if charRepo == nil {
		return nil, oops.Errorf("character repository is required")
	}
	if locRepo == nil {
		return nil, oops.Errorf("location repository is required")
	}
	if genesis == nil {
		return nil, oops.Errorf("character genesis service is required")
	}
	return &CharacterService{
		charRepo: charRepo,
		locRepo:  locRepo,
		genesis:  genesis,
	}, nil
}

// Create creates a new character for a player with the default character limit
// and NO binding (the bootstrap-admin behavior; bootstrap.CharacterCreator
// signature, unchanged).
func (s *CharacterService) Create(ctx context.Context, playerID ulid.ULID, name string) (*world.Character, error) {
	return s.createWithMaxAndBind(ctx, playerID, name, DefaultMaxCharacters, "")
}

// CreateBound creates a new character for a player with the default character
// limit and binds it with bindReason (registered gRPC creation uses
// "initial_bind"). An empty bindReason creates no binding.
func (s *CharacterService) CreateBound(ctx context.Context, playerID ulid.ULID, name, bindReason string) (*world.Character, error) {
	return s.createWithMaxAndBind(ctx, playerID, name, DefaultMaxCharacters, bindReason)
}

// CreateWithMaxCharacters creates a new character for a player with a custom
// character limit and NO binding.
func (s *CharacterService) CreateWithMaxCharacters(ctx context.Context, playerID ulid.ULID, name string, maxCharacters int) (*world.Character, error) {
	return s.createWithMaxAndBind(ctx, playerID, name, maxCharacters, "")
}

// createWithMaxAndBind runs the validation pipeline then persists the character +
// optional binding + genesis envelope atomically through the genesis service.
func (s *CharacterService) createWithMaxAndBind(ctx context.Context, playerID ulid.ULID, name string, maxCharacters int, bindReason string) (*world.Character, error) {
	// Normalize the name (trims whitespace, collapses spaces, Initial Caps)
	normalizedName := world.NormalizeCharacterName(name)

	// Validate the normalized name
	if err := world.ValidateCharacterName(normalizedName); err != nil {
		return nil, oops.Code("CHARACTER_INVALID_NAME").With("name", name).Wrap(err)
	}

	// Check name uniqueness (case-insensitive, using normalized name)
	exists, err := s.charRepo.ExistsByName(ctx, normalizedName)
	if err != nil {
		return nil, oops.Code("CHARACTER_CREATE_FAILED").With("name", normalizedName).Wrap(err)
	}
	if exists {
		return nil, oops.Code("CHARACTER_NAME_TAKEN").
			With("name", normalizedName).
			Errorf("character name %q is already taken", normalizedName)
	}

	// Check player's character limit
	count, err := s.charRepo.CountByPlayer(ctx, playerID)
	if err != nil {
		return nil, oops.Code("CHARACTER_CREATE_FAILED").With("player_id", playerID.String()).Wrap(err)
	}
	if count >= maxCharacters {
		return nil, oops.Code("CHARACTER_LIMIT_REACHED").
			With("player_id", playerID.String()).
			With("current", count).
			With("max", maxCharacters).
			Errorf("player has reached the maximum of %d characters", maxCharacters)
	}

	// Get the starting location
	startingLoc, err := s.locRepo.GetStartingLocation(ctx)
	if err != nil {
		return nil, oops.Code("CHARACTER_NO_STARTING_LOCATION").Wrap(err)
	}

	// Create the character
	char, err := world.NewCharacter(playerID, normalizedName)
	if err != nil {
		return nil, oops.Code("CHARACTER_CREATE_FAILED").With("name", normalizedName).Wrap(err)
	}

	// Set the starting location
	char.LocationID = &startingLoc.ID

	// Persist the character + optional binding + genesis envelope atomically.
	if err := s.genesis.Create(ctx, char, bindReason); err != nil {
		return nil, oops.Code("CHARACTER_CREATE_FAILED").With("id", char.ID.String()).Wrap(err)
	}

	return char, nil
}
