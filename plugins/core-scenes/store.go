// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"
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

	if err := runPluginMigrations(ctx, pool); err != nil {
		pool.Close()
		return nil, oops.Code("SCENE_CREATE_FAILED").Wrap(err)
	}

	return &SceneStore{pool: pool}, nil
}

// runPluginMigrations extracts the migrations subdirectory from the embed FS
// and delegates to the plugin storage SDK.
// storage.RunMigrations expects files at the FS root, but //go:embed nests
// them under migrations/, so we use fs.Sub to strip the prefix.
func runPluginMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return oops.Code("SCENE_CREATE_FAILED").
			With("detail", "failed to open migrations sub-FS").Wrap(err)
	}
	return runMigrationsFromFS(ctx, pool, sub)
}

// runMigrationsFromFS mirrors storage.RunMigrations but accepts fs.FS instead
// of embed.FS, enabling the fs.Sub workaround for nested embeds.
func runMigrationsFromFS(ctx context.Context, pool *pgxpool.Pool, migrations fs.FS) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS plugin_migrations (
			version INTEGER PRIMARY KEY,
			name    TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return oops.Code("PLUGIN_MIGRATION_TABLE_FAILED").Wrap(err)
	}

	var currentVersion int
	err = pool.QueryRow(ctx, "SELECT COALESCE(MAX(version), 0) FROM plugin_migrations").Scan(&currentVersion)
	if err != nil {
		return oops.Code("PLUGIN_MIGRATION_VERSION_FAILED").Wrap(err)
	}

	entries, err := fs.ReadDir(migrations, ".")
	if err != nil {
		return oops.Code("PLUGIN_MIGRATION_READ_FAILED").Wrap(err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		version := parseMigrationVersion(e.Name())
		if version <= currentVersion {
			continue
		}
		sql, readErr := fs.ReadFile(migrations, e.Name())
		if readErr != nil {
			return oops.Code("PLUGIN_MIGRATION_READ_FAILED").With("file", e.Name()).Wrap(readErr)
		}
		if _, execErr := pool.Exec(ctx, string(sql)); execErr != nil {
			return oops.Code("PLUGIN_MIGRATION_EXEC_FAILED").With("file", e.Name()).Wrap(execErr)
		}
		if _, trackErr := pool.Exec(ctx,
			"INSERT INTO plugin_migrations (version, name) VALUES ($1, $2)",
			version, e.Name(),
		); trackErr != nil {
			return oops.Code("PLUGIN_MIGRATION_TRACK_FAILED").With("file", e.Name()).Wrap(trackErr)
		}
	}
	return nil
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

// parseMigrationVersion extracts the numeric prefix from a migration filename.
func parseMigrationVersion(name string) int {
	parts := strings.SplitN(name, "_", 2)
	if len(parts) == 0 {
		return 0
	}
	var v int
	_, _ = fmt.Sscanf(parts[0], "%d", &v)
	return v
}

// itoa converts an int to its decimal string representation.
func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
