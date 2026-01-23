// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/world"
)

// SceneRepository implements world.SceneRepository using PostgreSQL.
type SceneRepository struct {
	pool *pgxpool.Pool
}

// NewSceneRepository creates a new SceneRepository.
func NewSceneRepository(pool *pgxpool.Pool) *SceneRepository {
	return &SceneRepository{pool: pool}
}

// AddParticipant adds a character to a scene.
func (r *SceneRepository) AddParticipant(ctx context.Context, sceneID, characterID ulid.ULID, role string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO scene_participants (scene_id, character_id, role, joined_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (scene_id, character_id) DO UPDATE SET role = $3
	`, sceneID.String(), characterID.String(), role, time.Now())
	if err != nil {
		return oops.
			With("operation", "add participant").
			With("scene_id", sceneID.String()).
			With("character_id", characterID.String()).
			Wrap(err)
	}
	return nil
}

// RemoveParticipant removes a character from a scene.
func (r *SceneRepository) RemoveParticipant(ctx context.Context, sceneID, characterID ulid.ULID) error {
	result, err := r.pool.Exec(ctx, `
		DELETE FROM scene_participants WHERE scene_id = $1 AND character_id = $2
	`, sceneID.String(), characterID.String())
	if err != nil {
		return oops.
			With("operation", "remove participant").
			With("scene_id", sceneID.String()).
			With("character_id", characterID.String()).
			Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.
			With("scene_id", sceneID.String()).
			With("character_id", characterID.String()).
			Wrap(ErrNotFound)
	}
	return nil
}

// ListParticipants returns all participants in a scene.
func (r *SceneRepository) ListParticipants(ctx context.Context, sceneID ulid.ULID) ([]world.SceneParticipant, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT character_id, role
		FROM scene_participants
		WHERE scene_id = $1
		ORDER BY joined_at
	`, sceneID.String())
	if err != nil {
		return nil, oops.
			With("operation", "list participants").
			With("scene_id", sceneID.String()).
			Wrap(err)
	}
	defer rows.Close()

	participants := make([]world.SceneParticipant, 0)
	for rows.Next() {
		var p world.SceneParticipant
		var charIDStr string
		if err := rows.Scan(&charIDStr, &p.Role); err != nil {
			return nil, oops.With("operation", "scan participant").Wrap(err)
		}
		var err error
		p.CharacterID, err = ulid.Parse(charIDStr)
		if err != nil {
			return nil, oops.With("operation", "parse character_id").With("character_id", charIDStr).Wrap(err)
		}
		participants = append(participants, p)
	}

	if err := rows.Err(); err != nil {
		return nil, oops.With("operation", "iterate participants").Wrap(err)
	}

	return participants, nil
}

// GetScenesFor returns all scenes a character is participating in.
func (r *SceneRepository) GetScenesFor(ctx context.Context, characterID ulid.ULID) ([]*world.Location, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT l.id, l.type, l.shadows_id, l.name, l.description, l.owner_id, l.replay_policy, l.created_at, l.archived_at
		FROM locations l
		INNER JOIN scene_participants sp ON l.id = sp.scene_id
		WHERE sp.character_id = $1
		ORDER BY l.created_at DESC
	`, characterID.String())
	if err != nil {
		return nil, oops.
			With("operation", "get scenes for character").
			With("character_id", characterID.String()).
			Wrap(err)
	}
	defer rows.Close()

	return scanLocations(rows)
}

// Compile-time interface check.
var _ world.SceneRepository = (*SceneRepository)(nil)
