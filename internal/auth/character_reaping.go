// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
	"context"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/wmodel"
)

// kindCharacterDeleted mirrors the taxonomy-declared character-deletion tombstone
// kind (internal/world/outbox.KindCharacterDeleted / world.Service's local
// kindCharacterDeleted) exactly. It is duplicated as a local literal here — NOT
// imported — because internal/auth MUST NOT import internal/world/outbox (that
// edge closes an import cycle back through internal/admin/auth → internal/auth;
// the same reason the genesis service carries local kind constants). The reaping
// service REUSES this kind rather than declaring a new one, so consumers treat a
// reaped guest character's tombstone identically to an operator DeleteCharacter
// tombstone (05-11).
const kindCharacterDeleted = "character_deleted"

// reapingActor is the envelope actor stamped on a guest-reaping tombstone. A
// reap is a background, system-initiated deletion — there is no human/CLI
// subject — so the actor is the system subject. It mirrors
// access.SubjectSystem ("system") as a local literal to avoid an
// internal/access import from the auth deletion path.
const reapingActor = "system"

// ReapingCharacterLister lists every character owned by a player with each
// Character.Version populated from the stored row (round-6 R6-1). It MUST be the
// version-scanning worldpostgres.CharacterRepository.ListByPlayer (05-03), NOT a
// version-blind adapter list: a zero Version makes the guarded CAS Delete a
// permanent conflict and D-06 never closes.
type ReapingCharacterLister interface {
	ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*world.Character, error)
}

// ReapingCharacterDeleter is the guarded, tombstone-delta-returning world
// character delete (05-14). expectedVersion is the character's stored version
// (from the version-scanning list), so the CAS succeeds; it returns the tombstone
// wmodel.MutationDelta the OutboxWriter finalizes the envelope from. Satisfied by
// the concrete worldpostgres.CharacterRepository, injected at composition roots
// ONLY (the sanctioned out-of-world writer allowlist).
type ReapingCharacterDeleter interface {
	Delete(ctx context.Context, id ulid.ULID, expectedVersion int) (*wmodel.MutationDelta, error)
}

// ReapingPropertyDeleter removes an entity's properties. The per-character reap
// tx runs DeleteByParent("character", id) BEFORE the character delete, mirroring
// world.Service.DeleteCharacter's cascade (round-6 R6-3) — entity_properties has
// no FK to characters, so a bare character delete would orphan property rows and
// diverge from operator-delete semantics. Satisfied by the world property repo
// (on execerFromCtx, so it enrolls in the reap tx).
type ReapingPropertyDeleter interface {
	DeleteByParent(ctx context.Context, parentType string, parentID ulid.ULID) error
}

// PlayerReapMarker marks a guest player reaping BEFORE enumeration (round-6
// R6-2), so the genesis service rejects new character creation for it.
type PlayerReapMarker interface {
	MarkReaping(ctx context.Context, playerID ulid.ULID) error
}

// GuestPlayerDeleter deletes the guest player row (its own pool). It is called
// AFTER every character is tombstoned + deleted, so the characters.player_id FK
// cascade has no un-tombstoned character left to remove.
type GuestPlayerDeleter interface {
	DeleteGuestPlayer(ctx context.Context, playerID ulid.ULID) error
}

// CharacterReapingService is the ONE atomic tombstone-emitting guest
// character-deletion primitive — the DELETION-side counterpart to 05-15's
// CharacterGenesisService and the SECOND sanctioned out-of-world writer under
// INV-WORLD-4 (its census disposition is documented in 05-11). Both guest
// deletion paths — the guest reaper (guest_reaper.go) and failed-guest cleanup
// (guest_service.go cleanupGuestPlayer) — route their character deletion through
// it, so guest expiration cannot produce genesis-without-tombstone feed history
// (round-5 D-06).
//
// DeleteGuestPlayer, for a guest player: (1) MARKS the player reaping so the
// genesis service rejects any concurrent character creation for it (R6-2
// anti-TOCTOU); (2) lists the player's characters via the version-scanning
// lister (R6-1); (3) for EACH character runs its OWN re-entrant world
// transaction {propertyDeleter.DeleteByParent("character", id) (R6-3 cascade
// parity) + CharacterWriter.Delete(id, version) (tombstone delta) +
// OutboxWriter.WriteIntent (character_deleted tombstone envelope)}; (4) AFTER all
// characters are tombstoned, deletes the player row.
//
// Atomicity scope (round-4 B4 two-pool boundary, mirrors 05-15): the ATOMIC UNIT
// is exactly {property cascade + character delete + its tombstone envelope} in
// ONE re-entrant world tx PER CHARACTER. The player row (auth player_repo, its
// own pool) is NOT claimed atomic with any tombstone tx — the characters are
// tombstoned + deleted FIRST (committed), THEN the player is deleted, so the
// characters.player_id ON DELETE CASCADE becomes a no-op (no un-tombstoned row
// remains). Orphan-player (all characters tombstoned but the player DELETE
// failed) is an ACCEPTED, DOCUMENTED compensation gap reconciled by the next
// reap cycle — OUTSIDE INV-WORLD-4.
//
// Partial-reap is resumable (round-6 Codex MEDIUM): each character's tx is its
// own, so a WORLD_CONCURRENT_EDIT on character N does NOT roll back the
// already-committed tombstones for characters 1..N-1. On any per-character
// failure the reap aborts (the player is NOT deleted while a character remains
// un-tombstoned) and the player stays marked reaping; the next reap cycle
// resumes. A WORLD_CONCURRENT_EDIT is retriable operational degradation, never a
// silent tombstone-less success.
//
// It is fail-closed by construction: the constructor rejects nil deps, so there
// is no tombstone-less or cascade-less mode.
type CharacterReapingService struct {
	lister     ReapingCharacterLister
	deleter    ReapingCharacterDeleter
	props      ReapingPropertyDeleter
	transactor GenesisTransactor
	outbox     world.OutboxWriter
	players    GuestPlayerDeleter
	marker     PlayerReapMarker
	gameID     string
}

// NewCharacterReapingService constructs the reaping service. It fails closed on
// any nil dependency — a guest character can never be deleted through a partially
// wired service that would skip the property cascade, the tombstone, or the
// reaping mark.
func NewCharacterReapingService(
	lister ReapingCharacterLister,
	deleter ReapingCharacterDeleter,
	props ReapingPropertyDeleter,
	transactor GenesisTransactor,
	outboxWriter world.OutboxWriter,
	players GuestPlayerDeleter,
	marker PlayerReapMarker,
) (*CharacterReapingService, error) {
	if lister == nil {
		return nil, oops.Errorf("character lister is required")
	}
	if deleter == nil {
		return nil, oops.Errorf("character deleter is required")
	}
	if props == nil {
		return nil, oops.Errorf("property deleter is required")
	}
	if transactor == nil {
		return nil, oops.Errorf("transactor is required")
	}
	if outboxWriter == nil {
		return nil, oops.Errorf("outbox writer is required")
	}
	if players == nil {
		return nil, oops.Errorf("player deleter is required")
	}
	if marker == nil {
		return nil, oops.Errorf("reaping marker is required")
	}
	return &CharacterReapingService{
		lister:     lister,
		deleter:    deleter,
		props:      props,
		transactor: transactor,
		outbox:     outboxWriter,
		players:    players,
		marker:     marker,
		gameID:     genesisGameID,
	}, nil
}

// DeleteGuestPlayer tombstones every character owned by the guest player, then
// deletes the player. It satisfies auth.GuestCleaner, so the guest reaper and
// failed-guest cleanup inject it in place of the raw player-cascade delete.
func (s *CharacterReapingService) DeleteGuestPlayer(ctx context.Context, playerID ulid.ULID) error {
	// (1) MARK reaping FIRST (R6-2): from here on the genesis service rejects any
	// character creation for this player, so no new character can slip past
	// enumeration into the player-delete cascade untombstoned.
	if err := s.marker.MarkReaping(ctx, playerID); err != nil {
		return oops.Code("GUEST_REAP_FAILED").
			With("player_id", playerID.String()).
			With("stage", "mark_reaping").Wrap(err)
	}

	// (2) LIST via the version-scanning lister (R6-1) so each Character.Version is
	// the stored version and the guarded CAS Delete matches.
	chars, err := s.lister.ListByPlayer(ctx, playerID)
	if err != nil {
		return oops.Code("GUEST_REAP_FAILED").
			With("player_id", playerID.String()).
			With("stage", "list_characters").Wrap(err)
	}

	// (3) For EACH character run its OWN re-entrant world tx. A failure (incl. a
	// retriable WORLD_CONCURRENT_EDIT) aborts THIS reap with the player left
	// marked + un-deleted; already-committed tombstones survive (resumable).
	for _, char := range chars {
		if rErr := s.reapCharacter(ctx, char); rErr != nil {
			return rErr
		}
	}

	// (4) Delete the player AFTER all characters are tombstoned + deleted, so the
	// FK cascade removes no un-tombstoned character.
	if err := s.players.DeleteGuestPlayer(ctx, playerID); err != nil {
		return oops.Code("GUEST_REAP_FAILED").
			With("player_id", playerID.String()).
			With("stage", "delete_player").Wrap(err)
	}
	return nil
}

// reapCharacter runs the atomic per-character unit: property cascade + guarded
// tombstone delete + the character_deleted envelope, in ONE re-entrant world
// transaction. With an ambient tx already in ctx it enrolls (no second Begin);
// standalone it commits/rolls back on its own. A WORLD_CONCURRENT_EDIT from the
// guarded delete is propagated unwrapped (its code preserved) so the reaper
// treats it as retriable — the character is retried next cycle, never deleted
// without a tombstone.
func (s *CharacterReapingService) reapCharacter(ctx context.Context, char *world.Character) error {
	intent, err := s.buildTombstoneIntent(char)
	if err != nil {
		return err
	}
	return oops.Wrap(s.transactor.InTransaction(ctx, func(txCtx context.Context) error {
		// Property cascade BEFORE the character delete (R6-3 parity with
		// world.Service.DeleteCharacter): entity_properties has no FK to characters.
		if pErr := s.props.DeleteByParent(txCtx, "character", char.ID); pErr != nil {
			return oops.Code("GUEST_REAP_FAILED").
				With("character_id", char.ID.String()).
				With("stage", "delete_properties").Wrap(pErr)
		}
		delta, dErr := s.deleter.Delete(txCtx, char.ID, char.Version)
		if dErr != nil {
			// Preserve the delete's code (WORLD_CONCURRENT_EDIT / CHARACTER_NOT_FOUND)
			// so the retriable signal survives; do NOT mask it as a success.
			return oops.Wrap(dErr)
		}
		if _, oErr := s.outbox.WriteIntent(txCtx, intent, delta); oErr != nil {
			return oops.Code("GUEST_REAP_FAILED").
				With("character_id", char.ID.String()).
				With("stage", "write_tombstone").Wrap(oErr)
		}
		return nil
	}))
}

// buildTombstoneIntent constructs the character_deleted tombstone EnvelopeIntent.
// The event identity is minted by wmodel.NewEnvelopeIntent via core.NewULID()
// (never the entity-id generator); the actor is the system subject; the kind is
// the SAME character_deleted kind world.Service.DeleteCharacter emits (NOT a new
// kind). The intent omits epoch/feed_position — the OutboxWriter owns those.
func (s *CharacterReapingService) buildTombstoneIntent(char *world.Character) (wmodel.EnvelopeIntent, error) {
	payload, err := world.BuildTombstonePayload(char.ID)
	if err != nil {
		return wmodel.EnvelopeIntent{}, oops.Code("GUEST_REAP_FAILED").
			With("character_id", char.ID.String()).
			Wrapf(err, "build character tombstone payload")
	}
	return wmodel.NewEnvelopeIntent(wmodel.IntentParams{
		GameID:        s.gameID,
		Kind:          kindCharacterDeleted,
		SchemaVersion: genesisSchemaVersion,
		Actor:         reapingActor,
		AggregateType: wmodel.AggregateCharacter,
		AggregateID:   char.ID,
		Payload:       payload,
	}), nil
}
