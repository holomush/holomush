// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
	"context"
	"encoding/json"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/wmodel"
)

// genesisGameID is the single-game identity the character-genesis envelope is
// stamped with. It MUST match world.Service's default game id ("main") so the
// character-genesis feed row lands on the same per-game feed counter, ordering,
// and watermark as every other world-change envelope. Single-game today; keyed
// for multi-game later (round-9 R6-5, resolving RESEARCH Open Question 2).
const genesisGameID = "main"

// kindCharacterGenesis and genesisSchemaVersion mirror the taxonomy-declared
// character-genesis contract in internal/world/outbox/taxonomy.go
// (outbox.KindCharacterGenesis / outbox.AppSchemaVersion) exactly. They are
// duplicated as local literals here — NOT imported — because internal/auth MUST
// NOT import internal/world/outbox (that edge pulls in the eventbus relay and
// closes an import cycle back through internal/admin/auth → internal/auth). This
// mirrors how internal/world/service.go carries local kind constants for the same
// reason; the 05-11 census asserts the emit site and the taxonomy agree.
const (
	kindCharacterGenesis = "character_genesis"
	genesisSchemaVersion = 1
)

// CharacterWriter persists a new character row inside the ambient transaction
// and returns the write's MutationDelta (05-14 signature). It is satisfied
// STRUCTURALLY by the concrete internal/world/postgres.CharacterRepository and is
// injected at composition roots ONLY — the auth-side narrow repo interfaces
// deliberately no longer expose Create, so no production package can insert a
// character except through this service (the compile-level fence, 05-15).
type CharacterWriter interface {
	Create(ctx context.Context, char *world.Character) (*wmodel.MutationDelta, error)
}

// GenesisTransactor is the re-entrant transaction seam (05-14). It MUST be the
// same transactor/tx-context convention the CharacterWriter, the binding repo,
// and the OutboxWriter all read (the world postgres txKey), or the character,
// binding, and envelope silently split across separate transactions. When an
// ambient transaction is already present in ctx it ENROLLS (no second Begin), so
// a caller (e.g. guest creation) that opened an outer transaction gets a single
// commit for character + binding + envelope.
type GenesisTransactor interface {
	InTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// GenesisBindingCreator creates an active player↔character binding and returns
// its id. It participates in the ambient world transaction (BindingRepository
// reads the world txKey), so the binding commits with the character + envelope
// or none do.
type GenesisBindingCreator interface {
	Create(ctx context.Context, playerID, characterID, reason string) (string, error)
}

// CharacterGenesisService is the ONE atomic character-creation primitive. It
// owns the character insert, an OPTIONAL player↔character binding, and the
// character-genesis outbox envelope in a SINGLE re-entrant transaction — so a
// character row can never commit without its genesis envelope (INV-WORLD-4 for
// character creation). All THREE production creation paths (registered gRPC,
// guest, bootstrap-admin) route through it.
//
// This is the CREATION-side sanctioned out-of-world writer under INV-WORLD-4
// (its DELETION-side counterpart is the CharacterReapingService, 05-16 /
// round-5 D-06); its census descriptor is registered in 05-11.
//
// Atomicity scope (round-4 B4): the ATOMIC UNIT is exactly character + optional
// binding + envelope — the three enroll in the world txKey. The player row
// (auth/postgres.PlayerRepository, its own pool) and the bootstrap admin role
// (store.PostgresRoleStore, its own pool) do NOT enroll here and MUST NOT be
// claimed atomic with the genesis tx; callers order those writes (guest commits
// the player BEFORE calling this service; bootstrap assigns the role AFTER it
// returns) to resolve the FK-blocking hazard.
//
// It is fail-closed by construction: the constructor rejects nil deps, so there
// is no envelope-less degraded mode.
type CharacterGenesisService struct {
	writer     CharacterWriter
	transactor GenesisTransactor
	bindings   GenesisBindingCreator
	outbox     world.OutboxWriter
	gameID     string
}

// NewCharacterGenesisService constructs the genesis service. It fails closed on
// any nil dependency — a character can never be created through a partially
// wired service that would skip the binding or the envelope.
func NewCharacterGenesisService(
	writer CharacterWriter,
	transactor GenesisTransactor,
	bindings GenesisBindingCreator,
	outboxWriter world.OutboxWriter,
) (*CharacterGenesisService, error) {
	if writer == nil {
		return nil, oops.Errorf("character writer is required")
	}
	if transactor == nil {
		return nil, oops.Errorf("transactor is required")
	}
	if bindings == nil {
		return nil, oops.Errorf("binding creator is required")
	}
	if outboxWriter == nil {
		return nil, oops.Errorf("outbox writer is required")
	}
	return &CharacterGenesisService{
		writer:     writer,
		transactor: transactor,
		bindings:   bindings,
		outbox:     outboxWriter,
		gameID:     genesisGameID,
	}, nil
}

// characterGenesisPayload is the intent-level, new-values-only genesis payload
// (erasure-safe; no secrets). Its shape mirrors the taxonomy's declared
// characterGenesisPayload field list (05-09).
type characterGenesisPayload struct {
	CharacterID string  `json:"character_id"`
	PlayerID    string  `json:"player_id"`
	Name        string  `json:"name"`
	LocationID  *string `json:"location_id,omitempty"`
}

// Create persists the character, creates a binding when bindReason is non-empty
// (empty = no binding, the bootstrap-admin mode), and writes exactly one
// character-genesis envelope — all in ONE re-entrant transaction. A failure of
// any step rolls the whole creation back; with an ambient transaction already in
// ctx it enrolls (no second Begin), so an outer rollback removes the character,
// the binding, and the envelope together.
func (s *CharacterGenesisService) Create(ctx context.Context, char *world.Character, bindReason string) error {
	if char == nil {
		return oops.Code("CHARACTER_GENESIS_FAILED").Errorf("character is required")
	}
	intent, err := s.buildIntent(char)
	if err != nil {
		return err
	}
	return oops.Wrap(s.transactor.InTransaction(ctx, func(txCtx context.Context) error {
		delta, wErr := s.writer.Create(txCtx, char)
		if wErr != nil {
			return oops.Code("CHARACTER_GENESIS_FAILED").
				With("character_id", char.ID.String()).Wrap(wErr)
		}
		if bindReason != "" {
			if _, bErr := s.bindings.Create(txCtx, char.PlayerID.String(), char.ID.String(), bindReason); bErr != nil {
				return oops.Code("CHARACTER_GENESIS_BINDING_FAILED").
					With("character_id", char.ID.String()).Wrap(bErr)
			}
		}
		if _, oErr := s.outbox.WriteIntent(txCtx, intent, delta); oErr != nil {
			return oops.Code("CHARACTER_GENESIS_ENVELOPE_FAILED").
				With("character_id", char.ID.String()).Wrap(oErr)
		}
		return nil
	}))
}

// buildIntent constructs the character-genesis EnvelopeIntent from the created
// character. The event identity is minted by wmodel.NewEnvelopeIntent via
// core.NewULID() (never the entity-id generator); the actor is the owning player
// (the already-authorized, committed fact that caused the genesis). The intent
// deliberately omits epoch/feed_position/manifest — the OutboxWriter owns those
// (round-3 blocker #1).
func (s *CharacterGenesisService) buildIntent(char *world.Character) (wmodel.EnvelopeIntent, error) {
	p := characterGenesisPayload{
		CharacterID: char.ID.String(),
		PlayerID:    char.PlayerID.String(),
		Name:        char.Name,
	}
	if char.LocationID != nil {
		loc := char.LocationID.String()
		p.LocationID = &loc
	}
	payload, err := json.Marshal(p)
	if err != nil {
		return wmodel.EnvelopeIntent{}, oops.Code("CHARACTER_GENESIS_FAILED").
			Wrapf(err, "marshal character genesis payload")
	}
	return wmodel.NewEnvelopeIntent(wmodel.IntentParams{
		GameID:        s.gameID,
		Kind:          kindCharacterGenesis,
		SchemaVersion: genesisSchemaVersion,
		Actor:         char.PlayerID.String(),
		AggregateType: wmodel.AggregateCharacter,
		AggregateID:   char.ID,
		Payload:       payload,
	}), nil
}
