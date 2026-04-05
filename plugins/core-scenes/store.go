// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/holomush/holomush/pkg/plugin/storage"
)

//go:embed migrations/*.up.sql
var migrationsFS embed.FS

// SceneRow represents a scene record in the database.
type SceneRow struct {
	ID              string
	Title           string
	Description     string
	LocationID      *string
	OwnerID         string
	State           string
	PoseOrder       string
	Visibility      string
	IdleTimeoutSecs *int
	TemplateID      *string
	ContentWarnings []string
	Tags            []string
	CreatedAt       time.Time
	EndedAt         *time.Time
	ArchivedAt      *time.Time
}

// ParticipantRow represents a scene participant record in the database.
type ParticipantRow struct {
	SceneID          string
	CharacterID      string
	Role             string
	OriginLocationID *string
	JoinedAt         time.Time
	PublishVote      *bool
}

// SceneStore provides PostgreSQL persistence for the scenes plugin.
type SceneStore struct {
	pool *pgxpool.Pool
}

// NewSceneStore connects to PostgreSQL and runs migrations.
func NewSceneStore(ctx context.Context, connString string) (*SceneStore, error) {
	pool, err := storage.Connect(ctx, connString)
	if err != nil {
		return nil, oops.Code("SCENE_CREATE_FAILED").Wrap(err)
	}

	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		pool.Close()
		return nil, oops.Code("SCENE_STORE_INIT_FAILED").Wrap(err)
	}
	if err := storage.RunMigrationsFS(ctx, pool, sub); err != nil {
		pool.Close()
		return nil, err
	}

	return &SceneStore{pool: pool}, nil
}

// Close releases the database connection pool.
func (s *SceneStore) Close() {
	s.pool.Close()
}

// CreateScene inserts a new scene record.
func (s *SceneStore) CreateScene(ctx context.Context, row *SceneRow) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO scenes (
			id, title, description, location_id, owner_id,
			state, pose_order, visibility, idle_timeout_secs, template_id,
			content_warnings, tags, created_at, ended_at, archived_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15
		)`,
		row.ID, row.Title, row.Description, row.LocationID, row.OwnerID,
		row.State, row.PoseOrder, row.Visibility, row.IdleTimeoutSecs, row.TemplateID,
		row.ContentWarnings, row.Tags, row.CreatedAt, row.EndedAt, row.ArchivedAt,
	)
	if err != nil {
		return oops.Code("SCENE_CREATE_FAILED").With("scene_id", row.ID).Wrap(err)
	}
	return nil
}

// GetScene retrieves a scene by ID.
func (s *SceneStore) GetScene(ctx context.Context, id string) (*SceneRow, error) {
	row := &SceneRow{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, title, description, location_id, owner_id,
			state, pose_order, visibility, idle_timeout_secs, template_id,
			content_warnings, tags, created_at, ended_at, archived_at
		FROM scenes WHERE id = $1`, id,
	).Scan(
		&row.ID, &row.Title, &row.Description, &row.LocationID, &row.OwnerID,
		&row.State, &row.PoseOrder, &row.Visibility, &row.IdleTimeoutSecs, &row.TemplateID,
		&row.ContentWarnings, &row.Tags, &row.CreatedAt, &row.EndedAt, &row.ArchivedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.Code("SCENE_NOT_FOUND").With("scene_id", id).Wrap(err)
		}
		return nil, oops.Code("SCENE_GET_FAILED").With("scene_id", id).Wrap(err)
	}
	return row, nil
}

// UpdateScene updates an existing scene record.
func (s *SceneStore) UpdateScene(ctx context.Context, row *SceneRow) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE scenes SET
			title = $2, description = $3, location_id = $4, owner_id = $5,
			state = $6, pose_order = $7, visibility = $8, idle_timeout_secs = $9,
			template_id = $10, content_warnings = $11, tags = $12,
			ended_at = $13, archived_at = $14
		WHERE id = $1`,
		row.ID, row.Title, row.Description, row.LocationID, row.OwnerID,
		row.State, row.PoseOrder, row.Visibility, row.IdleTimeoutSecs,
		row.TemplateID, row.ContentWarnings, row.Tags,
		row.EndedAt, row.ArchivedAt,
	)
	if err != nil {
		return oops.Code("SCENE_UPDATE_FAILED").With("scene_id", row.ID).Wrap(err)
	}
	if tag.RowsAffected() == 0 {
		return oops.Code("SCENE_NOT_FOUND").With("scene_id", row.ID).
			Errorf("scene not found")
	}
	return nil
}

// ListScenes retrieves scenes with optional state and visibility filters.
func (s *SceneStore) ListScenes(ctx context.Context, state *string, visibility *string, limit, offset int) ([]*SceneRow, error) {
	query := `
		SELECT id, title, description, location_id, owner_id,
			state, pose_order, visibility, idle_timeout_secs, template_id,
			content_warnings, tags, created_at, ended_at, archived_at
		FROM scenes WHERE 1=1`
	args := []any{}
	argIdx := 1

	if state != nil {
		query += " AND state = $" + itoa(argIdx)
		args = append(args, *state)
		argIdx++
	}
	if visibility != nil {
		query += " AND visibility = $" + itoa(argIdx)
		args = append(args, *visibility)
		argIdx++
	}

	query += " ORDER BY created_at DESC"
	query += " LIMIT $" + itoa(argIdx)
	args = append(args, limit)
	argIdx++
	query += " OFFSET $" + itoa(argIdx)
	args = append(args, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, oops.Code("SCENE_LIST_FAILED").Wrap(err)
	}
	defer rows.Close()

	var result []*SceneRow
	for rows.Next() {
		r := &SceneRow{}
		if scanErr := rows.Scan(
			&r.ID, &r.Title, &r.Description, &r.LocationID, &r.OwnerID,
			&r.State, &r.PoseOrder, &r.Visibility, &r.IdleTimeoutSecs, &r.TemplateID,
			&r.ContentWarnings, &r.Tags, &r.CreatedAt, &r.EndedAt, &r.ArchivedAt,
		); scanErr != nil {
			return nil, oops.Code("SCENE_LIST_FAILED").Wrap(scanErr)
		}
		result = append(result, r)
	}
	if rows.Err() != nil {
		return nil, oops.Code("SCENE_LIST_FAILED").Wrap(rows.Err())
	}
	return result, nil
}

// AddParticipant upserts a participant into a scene.
func (s *SceneStore) AddParticipant(ctx context.Context, row *ParticipantRow) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO scene_participants (scene_id, character_id, role, origin_location_id, joined_at, publish_vote)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (scene_id, character_id)
		DO UPDATE SET role = EXCLUDED.role, origin_location_id = EXCLUDED.origin_location_id, publish_vote = EXCLUDED.publish_vote`,
		row.SceneID, row.CharacterID, row.Role, row.OriginLocationID, row.JoinedAt, row.PublishVote,
	)
	if err != nil {
		return oops.Code("SCENE_ADD_PARTICIPANT_FAILED").
			With("scene_id", row.SceneID).
			With("character_id", row.CharacterID).Wrap(err)
	}
	return nil
}

// RemoveParticipant deletes a participant from a scene.
func (s *SceneStore) RemoveParticipant(ctx context.Context, sceneID, characterID string) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM scene_participants WHERE scene_id = $1 AND character_id = $2`,
		sceneID, characterID,
	)
	if err != nil {
		return oops.Code("SCENE_REMOVE_PARTICIPANT_FAILED").
			With("scene_id", sceneID).
			With("character_id", characterID).Wrap(err)
	}
	if tag.RowsAffected() == 0 {
		return oops.Code("SCENE_NOT_FOUND").
			With("scene_id", sceneID).
			With("character_id", characterID).
			Errorf("participant not found")
	}
	return nil
}

// ListParticipants retrieves all participants in a scene.
func (s *SceneStore) ListParticipants(ctx context.Context, sceneID string) ([]*ParticipantRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT scene_id, character_id, role, origin_location_id, joined_at, publish_vote
		FROM scene_participants WHERE scene_id = $1
		ORDER BY joined_at ASC`, sceneID,
	)
	if err != nil {
		return nil, oops.Code("SCENE_LIST_FAILED").With("scene_id", sceneID).Wrap(err)
	}
	defer rows.Close()

	var result []*ParticipantRow
	for rows.Next() {
		r := &ParticipantRow{}
		if scanErr := rows.Scan(
			&r.SceneID, &r.CharacterID, &r.Role, &r.OriginLocationID, &r.JoinedAt, &r.PublishVote,
		); scanErr != nil {
			return nil, oops.Code("SCENE_LIST_FAILED").With("scene_id", sceneID).Wrap(scanErr)
		}
		result = append(result, r)
	}
	if rows.Err() != nil {
		return nil, oops.Code("SCENE_LIST_FAILED").With("scene_id", sceneID).Wrap(rows.Err())
	}
	return result, nil
}

// itoa converts an int to its decimal string representation.
func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
