// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/charname"
	"github.com/holomush/holomush/internal/charname/syntax"
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
	// ExistsByNormalizedName reports whether any character holds the given
	// §6.1.1 uniqueness key, excluding the character named by excluding (nil
	// when creating). It is a UX affordance, not the uniqueness guarantee.
	ExistsByNormalizedName(ctx context.Context, key string, excluding *ulid.ULID) (bool, error)

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
	Create(ctx context.Context, char *world.Character, name charname.Admitted, bindReason string) error
}

// CharacterService handles character creation and management. It owns the
// validation pipeline (normalize, uniqueness, limit, starting location) and
// delegates the actual persistence + genesis envelope to CharacterGenesis.
type CharacterService struct {
	charRepo CharacterRepository
	locRepo  LocationRepository
	genesis  CharacterGenesis

	// gate is the character-name admission decision. It is a NEW constructor
	// dependency rather than something reached through an existing field:
	// CharacterService holds no pool and no writer, by design (its repository
	// interface deliberately exposes no Create — the INV-WORLD-4 compile fence).
	gate *charname.Gate
}

// NewCharacterService creates a new CharacterService.
// Returns an error if any required dependency is nil.
func NewCharacterService(charRepo CharacterRepository, locRepo LocationRepository, genesis CharacterGenesis, gate *charname.Gate) (*CharacterService, error) {
	if charRepo == nil {
		return nil, oops.Errorf("character repository is required")
	}
	if locRepo == nil {
		return nil, oops.Errorf("location repository is required")
	}
	if genesis == nil {
		return nil, oops.Errorf("character genesis service is required")
	}
	if gate == nil {
		return nil, oops.Errorf("character name gate is required")
	}
	return &CharacterService{
		charRepo: charRepo,
		locRepo:  locRepo,
		genesis:  genesis,
		gate:     gate,
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
	// ONE admission decision. Gate.Admit runs §6.1.1 normalization, the
	// syntactic rules, the mixed-script rule, the block list and the skeleton
	// corpus check, and mints the token the writer requires.
	//
	// There is deliberately NO separate world.ValidateCharacterName call here:
	// Gate.Check already runs it on the normalized display form, so a second
	// call would re-establish the two-proofs convention charname.Admitted exists
	// to replace — and the next writer added elsewhere would copy the shape
	// without the gate.
	admitted, err := s.gate.Admit(ctx, name)
	if err != nil {
		return nil, mapGateError(name, err)
	}
	normalizedName := admitted.Display()

	// Friendly uniqueness pre-check (§6.1.3): a UX affordance, not the
	// guarantee. The guarantee is the database constraint plan 02-12 creates,
	// and the 23505 handler below is what surfaces it.
	exists, err := s.charRepo.ExistsByNormalizedName(ctx, admitted.Key(), nil)
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
	if err := s.genesis.Create(ctx, char, admitted, bindReason); err != nil {
		if isNormalizedNameUniqueViolation(err) {
			// The pre-check above is racy by construction; this is where the
			// database's own answer arrives. Surfacing it as the SAME
			// CHARACTER_NAME_TAKEN code keeps the caller-visible contract
			// identical whichever path wins.
			return nil, oops.Code("CHARACTER_NAME_TAKEN").
				With("name", normalizedName).
				Errorf("character name %q is already taken", normalizedName)
		}
		return nil, oops.Code("CHARACTER_CREATE_FAILED").With("id", char.ID.String()).Wrap(err)
	}

	return char, nil
}

// characterNormalizedNameConstraint is the UNIQUE index migration 000056
// creates over characters.normalized_name. It is named explicitly so a 23505
// from some OTHER constraint is never swallowed as a taken name.
const characterNormalizedNameConstraint = "characters_normalized_name_key"

// isNormalizedNameUniqueViolation reports whether err is a Postgres
// unique-violation (SQLSTATE 23505) on the normalized-name index specifically.
//
// The index that makes this fire is created by plan 02-12; the handler lands
// here so the create path is complete the moment it does.
func isNormalizedNameUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == characterNormalizedNameConstraint
}

// pgUniqueViolation is SQLSTATE 23505.
const pgUniqueViolation = "23505"

// mapGateError translates a charname.Gate refusal into the caller-visible code
// the create path already returns.
//
// Two verdicts are remapped, and both are refusals this path ALREADY had a code
// for before the gate existed:
//
//   - NAME_INVALID_SYNTAX — a digit, punctuation mark or out-of-bounds rune
//     count. world.ValidateCharacterName produced CHARACTER_INVALID_NAME here.
//   - NAME_EMPTY_NORMAL_FORM — a blank or invisible-only submission. The old
//     "cannot be empty" syntactic rule produced CHARACTER_INVALID_NAME too.
//
// Everything else the gate can say — NAME_CONFUSABLE, NAME_BLOCKED,
// NAME_SKELETON_UNVERIFIABLE, NAME_MIXED_SCRIPT — is a NEW refusal with no
// legacy code to preserve, so it passes through untouched.
//
// The remap REPLACES the code rather than wrapping it. errutil.AssertErrorCode
// and oops.AsOops(...).Code() both resolve the DEEPEST code in the chain
// (issue #4902), so wrapping a NAME_INVALID_SYNTAX oops in a
// CHARACTER_INVALID_NAME one would leave callers still seeing the inner code —
// the caller-visible contract would silently change. The underlying
// *syntax.ValidationError is carried forward instead, so the message and the
// errors.As chain both survive.
func mapGateError(submitted string, err error) error {
	oopsErr, ok := oops.AsOops(err)
	if !ok {
		return err
	}
	code, isStr := oopsErr.Code().(string)
	if !isStr {
		return err
	}
	switch code {
	case "NAME_INVALID_SYNTAX":
		var verr *syntax.ValidationError
		if errors.As(err, &verr) {
			return oops.Code("CHARACTER_INVALID_NAME").With("name", submitted).Wrap(verr)
		}
		return oops.Code("CHARACTER_INVALID_NAME").With("name", submitted).Errorf("%s", oopsErr.Error())
	case "NAME_EMPTY_NORMAL_FORM":
		return oops.Code("CHARACTER_INVALID_NAME").
			With("name", submitted).
			Errorf("%s", oopsErr.Error())
	default:
		return err
	}
}
