// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/idgen"
)

// bindingDB combines execer and querier into a single interface for
// binding repository methods that need both Exec and QueryRow.
// Both *pgxpool.Pool and pgx.Tx satisfy this interface.
type bindingDB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// bindingDBFromCtx returns the active transaction from context, or falls back to the pool.
func bindingDBFromCtx(ctx context.Context, pool *pgxpool.Pool) bindingDB {
	if tx := txFromContext(ctx); tx != nil {
		return tx
	}
	return pool
}

// BindingRepository persists player↔character bindings (Phase 3b
// grounding doc Decision 7 / master spec §4.3a). Bindings are long-lived
// tenures (weeks/months, spanning many sessions); binding_id is the
// load-bearing identifier in §7.2 Branch 1 AuthGuard decisions.
//
// Tx-composable via bindingDBFromCtx: callers may compose Create/End in a
// transaction opened by Transactor.InTransaction, participating in the
// same Tx that creates the character row.
type BindingRepository struct {
	pool *pgxpool.Pool
}

// NewBindingRepository creates a new BindingRepository backed by the given pool.
// Panics if pool is nil: a nil pool is a programmer error at wiring time, not
// a recoverable runtime condition. This matches the pattern used by sibling
// repositories (character_repo, exit_repo, location_repo).
func NewBindingRepository(pool *pgxpool.Pool) *BindingRepository {
	if pool == nil {
		panic("NewBindingRepository: pool must not be nil")
	}
	return &BindingRepository{pool: pool}
}

// Current returns the active binding_id for characterID. Returns
// BINDING_NOT_FOUND if no active binding exists.
func (s *BindingRepository) Current(ctx context.Context, characterID string) (string, error) {
	bindingID, _, err := s.currentBinding(ctx, characterID)
	return bindingID, err
}

// CurrentWithPlayer returns the active binding_id and the player_id it is bound
// to for characterID, both from the same active binding row. Returns
// BINDING_NOT_FOUND if no active binding exists. Satisfies dek.BindingResolver:
// the player_id is recorded on genesis-seeded DEK participants so the AuthGuard
// player-history branch matches after a later binding rotation
// (holomush-5rh.8.29.11).
func (s *BindingRepository) CurrentWithPlayer(ctx context.Context, characterID string) (bindingID, playerID string, err error) {
	return s.currentBinding(ctx, characterID)
}

// currentBinding is the shared query backing Current and CurrentWithPlayer: it
// selects the active binding row's id and player_id in one round-trip so the
// two values are atomically consistent.
func (s *BindingRepository) currentBinding(ctx context.Context, characterID string) (bindingID, playerID string, err error) {
	err = bindingDBFromCtx(ctx, s.pool).QueryRow(
		ctx,
		`SELECT id, player_id FROM player_character_bindings WHERE character_id = $1 AND ended_at IS NULL`,
		characterID,
	).Scan(&bindingID, &playerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", oops.Code("BINDING_NOT_FOUND").
				With("character_id", characterID).
				Errorf("no active binding for character %s", characterID)
		}
		return "", "", oops.Code("BINDING_STORE_QUERY_FAILED").Wrap(err)
	}
	return bindingID, playerID, nil
}

// Create inserts a new active binding for (playerID, characterID).
// Returns the new binding ID.
func (s *BindingRepository) Create(ctx context.Context, playerID, characterID, reason string) (string, error) {
	if playerID == "" || characterID == "" {
		return "", oops.Code("BINDING_STORE_INVALID_INPUT").
			Errorf("playerID and characterID required")
	}
	bindingID := idgen.New().String()
	_, err := bindingDBFromCtx(ctx, s.pool).Exec(
		ctx,
		`INSERT INTO player_character_bindings (id, player_id, character_id, ended_reason)
		 VALUES ($1, $2, $3, $4)`,
		bindingID, playerID, characterID, nullableString(reason),
	)
	if err != nil {
		return "", oops.Code("BINDING_STORE_INSERT_FAILED").
			With("character_id", characterID).
			With("player_id", playerID).
			Wrap(err)
	}
	return bindingID, nil
}

// End marks a binding as ended. Returns BINDING_NOT_FOUND if the
// binding doesn't exist; BINDING_ALREADY_ENDED if it's already ended.
func (s *BindingRepository) End(ctx context.Context, bindingID, reason string) error {
	db := bindingDBFromCtx(ctx, s.pool)
	cmdTag, err := db.Exec(
		ctx,
		`UPDATE player_character_bindings
		 SET ended_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT, ended_reason = $2
		 WHERE id = $1 AND ended_at IS NULL`,
		bindingID, reason,
	)
	if err != nil {
		return oops.Code("BINDING_STORE_UPDATE_FAILED").Wrap(err)
	}
	if cmdTag.RowsAffected() == 0 {
		var alreadyEnded bool
		if scanErr := db.QueryRow(
			ctx,
			`SELECT ended_at IS NOT NULL FROM player_character_bindings WHERE id = $1`,
			bindingID,
		).Scan(&alreadyEnded); scanErr != nil {
			if errors.Is(scanErr, pgx.ErrNoRows) {
				return oops.Code("BINDING_NOT_FOUND").
					With("binding_id", bindingID).
					Errorf("binding %s not found", bindingID)
			}
			return oops.Code("BINDING_STORE_QUERY_FAILED").Wrap(scanErr)
		}
		if alreadyEnded {
			return oops.Code("BINDING_ALREADY_ENDED").
				With("binding_id", bindingID).
				Errorf("binding %s already ended", bindingID)
		}
		// Defensive: row exists with ended_at IS NULL, yet UPDATE matched
		// zero rows. Indicates a race with a concurrent writer or unexpected
		// isolation behavior — fail loudly rather than silently succeeding.
		return oops.Code("BINDING_STORE_UPDATE_RACE").
			With("binding_id", bindingID).
			Errorf("End() observed row %s with ended_at IS NULL after UPDATE matched 0 rows", bindingID)
	}
	return nil
}
