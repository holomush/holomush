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
	// Friendly uniqueness pre-check (§6.1.3), and it runs BEFORE the gate on
	// purpose.
	//
	// An EXACT duplicate has an identical skeleton, so charname.Gate.Check's
	// step 5 intercepts it and returns NAME_CONFUSABLE. When this check sat
	// AFTER the gate it was unreachable for the very case it exists to serve:
	// a player retyping a name that is already taken got "too similar to an
	// existing one" — and, because internal/grpc had no case for that code, the
	// web client rendered the generic "request failed". That is the regression
	// PR #4941's E2E run caught, and it is why the order here is load-bearing
	// rather than incidental.
	//
	// The check is keyed on the §6.1.1 uniqueness key, so a case or
	// NFKC-collapsible variant of an existing name is the SAME name and is
	// reported as taken. A genuinely DIFFERENT name that merely LOOKS like an
	// existing one has a different key, falls through to the gate, and is
	// refused NAME_CONFUSABLE — claiming it was "already taken" would assert
	// something untrue and disclose more about the corpus than §6.1.2 intends.
	//
	// This is still only a UX affordance. The guarantee is guardSkeleton's
	// in-transaction advisory lock (D-30 part 2) and the unique index plan
	// 02-12 creates, whose 23505 the handler below surfaces.
	normalized, err := charname.Normalize(name)
	if err != nil {
		// The only failure is an empty normal form, which mapGateError already
		// translates to the pre-existing CHARACTER_INVALID_NAME.
		return nil, mapGateError(name, err)
	}
	taken, err := s.charRepo.ExistsByNormalizedName(ctx, normalized.Key, nil)
	if err != nil {
		return nil, oops.Code("CHARACTER_CREATE_FAILED").With("name", normalized.Display).Wrap(err)
	}
	if taken {
		return nil, oops.Code("CHARACTER_NAME_TAKEN").
			With("name", normalized.Display).
			Errorf("character name %q is already taken", normalized.Display)
	}

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

	// The same check again, now on the ADMITTED key. It is not redundant: the
	// pre-check above and the gate are two round trips, and a concurrent create
	// can land between them. This is the second-cheapest net; guardSkeleton and
	// the 23505 handler are the ones that actually close the race.
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
// NAME_SKELETON_UNVERIFIABLE, NAME_MIXED_SCRIPT — passes through untouched and
// is sanitized for the client by internal/grpc's sanitizeAuthError, which
// carries a case for each.
//
// An earlier form of this comment justified that pass-through by claiming those
// four are "NEW refusals with no legacy code to preserve". That premise was
// FALSE for one case, and the falsehood shipped: an exact duplicate reaches
// step 5 with an identical skeleton, so NAME_CONFUSABLE was what a player
// retyping a taken name actually got — and "already taken" is the OLDEST
// refusal on this path, which certainly did have a code.
//
// The fix is NOT to map NAME_CONFUSABLE onto CHARACTER_NAME_TAKEN here. That
// would make a name confusable with a DIFFERENT player's character claim to be
// "already taken" — untrue, and a broader disclosure than §6.1.2 intends. The
// duplicate is caught by the uniqueness pre-check that now runs BEFORE the
// gate, so by the time a NAME_CONFUSABLE reaches this function the name really
// is a different one.
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
