// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
	"context"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/world"
)

// CharacterRepository defines the persistence operations needed by CharacterService.
// This extends world.CharacterRepository with methods for name uniqueness and player queries.
type CharacterRepository interface {
	// Create persists a new character.
	Create(ctx context.Context, char *world.Character) error

	// ExistsByName checks if a character with the given name exists (case-insensitive).
	ExistsByName(ctx context.Context, name string) (bool, error)

	// CountByPlayer returns the number of characters owned by a player.
	CountByPlayer(ctx context.Context, playerID ulid.ULID) (int, error)
}

// LocationRepository defines the location operations needed by CharacterService.
type LocationRepository interface {
	// GetStartingLocation returns the default location for new characters.
	GetStartingLocation(ctx context.Context) (*world.Location, error)
}

// CharacterService handles character creation and management.
type CharacterService struct {
	charRepo CharacterRepository
	locRepo  LocationRepository
}

// NewCharacterService creates a new CharacterService.
func NewCharacterService(charRepo CharacterRepository, locRepo LocationRepository) *CharacterService {
	return &CharacterService{
		charRepo: charRepo,
		locRepo:  locRepo,
	}
}

// Create creates a new character for a player with the default character limit.
func (s *CharacterService) Create(ctx context.Context, playerID ulid.ULID, name string) (*world.Character, error) {
	return s.CreateWithMaxCharacters(ctx, playerID, name, DefaultMaxCharacters)
}

// CreateWithMaxCharacters creates a new character for a player with a custom character limit.
func (s *CharacterService) CreateWithMaxCharacters(ctx context.Context, playerID ulid.ULID, name string, maxCharacters int) (*world.Character, error) {
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

	// Persist the character
	if err := s.charRepo.Create(ctx, char); err != nil {
		return nil, oops.Code("CHARACTER_CREATE_FAILED").With("id", char.ID.String()).Wrap(err)
	}

	return char, nil
}
