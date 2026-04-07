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
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/holomush/holomush/pkg/plugin/storage"
)

// sceneSelectColumns is the column list shared by every statement that
// reads a full SceneRow. Adding a new column only requires touching this
// constant plus the scanner below.
const sceneSelectColumns = `id, title, description, location_id, owner_id,
    state, pose_order, visibility, idle_timeout_secs, template_id,
    content_warnings, tags, created_at, ended_at, archived_at`

// scanSceneRow scans a single row into the provided SceneRow. The column
// order MUST match sceneSelectColumns.
func scanSceneRow(scanner pgx.Row, row *SceneRow) error {
	return scanner.Scan(
		&row.ID, &row.Title, &row.Description, &row.LocationID, &row.OwnerID,
		&row.State, &row.PoseOrder, &row.Visibility, &row.IdleTimeoutSecs,
		&row.TemplateID, &row.ContentWarnings, &row.Tags,
		&row.CreatedAt, &row.EndedAt, &row.ArchivedAt,
	)
}

//go:embed migrations/*.up.sql
var migrationsFS embed.FS

// SceneRow is the persistence-layer representation of a scene. The shape
// matches the scenes table column-for-column.
//
// Pointer types (LocationID, IdleTimeoutSecs, TemplateID, EndedAt, ArchivedAt)
// represent nullable columns. ContentWarnings and Tags are non-null TEXT[]
// columns; an empty slice corresponds to '{}' in the database.
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

// SceneStore provides PostgreSQL persistence for scenes.
//
// Phase 1 implements only Create and Get. Subsequent phases extend the store
// with state transitions (Phase 2), participant operations (Phase 3),
// publish-vote and log archival (Phase 6), and templates (Phase 7).
type SceneStore struct {
	pool *pgxpool.Pool
}

// NewSceneStore opens a connection pool and runs the embedded migrations.
//
// The connection string is the one provided by the host's SchemaProvisioner
// in ServiceConfig.ConnectionString — it has search_path=plugin_core_scenes
// pre-configured, so all queries automatically target the plugin's schema.
func NewSceneStore(ctx context.Context, connString string) (*SceneStore, error) {
	pool, err := storage.Connect(ctx, connString)
	if err != nil {
		return nil, oops.Code("SCENE_STORE_CONNECT_FAILED").Wrap(err)
	}

	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		pool.Close()
		return nil, oops.Code("SCENE_STORE_INIT_FAILED").Wrap(err)
	}
	if err := storage.RunMigrationsFS(ctx, pool, sub); err != nil {
		pool.Close()
		return nil, oops.Code("SCENE_STORE_MIGRATIONS_FAILED").Wrap(err)
	}

	return &SceneStore{pool: pool}, nil
}

// Close releases the underlying connection pool. Safe to call from a defer
// in main(); idempotent if pool is already nil-ish (pgxpool guards internally).
func (s *SceneStore) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

// Create inserts a new scene row. The caller MUST populate ID, Title,
// OwnerID, State, PoseOrder, and Visibility; defaults from the schema apply
// for unset nullable fields.
func (s *SceneStore) Create(ctx context.Context, row *SceneRow) error {
	ctx, span := startSpan(ctx, "scene.store.create",
		attribute.String("scene_id", row.ID),
	)
	defer span.End()

	_, err := s.pool.Exec(ctx, `
        INSERT INTO scenes (
            id, title, description, location_id, owner_id, state, pose_order,
            visibility, idle_timeout_secs, template_id, content_warnings, tags
        ) VALUES (
            $1, $2, $3, $4, $5, $6, $7,
            $8, $9, $10, $11, $12
        )`,
		row.ID, row.Title, row.Description, row.LocationID, row.OwnerID,
		row.State, row.PoseOrder, row.Visibility, row.IdleTimeoutSecs,
		row.TemplateID, row.ContentWarnings, row.Tags,
	)
	if err != nil {
		recordError(span, err)
		return oops.Code("SCENE_CREATE_FAILED").With("scene_id", row.ID).Wrap(err)
	}
	return nil
}

// Get loads a single scene by ID. Returns a SCENE_NOT_FOUND error code if
// the row does not exist.
func (s *SceneStore) Get(ctx context.Context, id string) (*SceneRow, error) {
	ctx, span := startSpan(ctx, "scene.store.get",
		attribute.String("scene_id", id),
	)
	defer span.End()

	row := &SceneRow{}
	err := scanSceneRow(s.pool.QueryRow(ctx, `
        SELECT `+sceneSelectColumns+`
        FROM scenes
        WHERE id = $1`,
		id,
	), row)
	if err != nil {
		recordError(span, err)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.Code("SCENE_NOT_FOUND").With("scene_id", id).Wrap(err)
		}
		return nil, oops.Code("SCENE_GET_FAILED").With("scene_id", id).Wrap(err)
	}
	return row, nil
}

// End transitions a scene to the `ended` state and returns the post-update
// row. Only scenes currently in `active` or `paused` states can be ended;
// the WHERE clause enforces this at the database level so concurrent
// transitions cannot corrupt the state machine.
//
// Sets `state = 'ended'` and `ended_at = NOW()` atomically and returns the
// resulting row via Postgres RETURNING. Returns SCENE_NOT_FOUND if no row
// matches the ID at all, or SCENE_TRANSITION_FORBIDDEN if the row exists
// but is in a state that cannot be ended.
func (s *SceneStore) End(ctx context.Context, id string) (*SceneRow, error) {
	ctx, span := startSpan(ctx, "scene.store.end",
		attribute.String("scene_id", id),
	)
	defer span.End()

	row := &SceneRow{}
	err := scanSceneRow(s.pool.QueryRow(ctx, `
        UPDATE scenes
        SET state = 'ended', ended_at = NOW()
        WHERE id = $1 AND state IN ('active', 'paused')
        RETURNING `+sceneSelectColumns,
		id,
	), row)
	if err != nil {
		recordError(span, err)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, s.classifyTransitionMiss(ctx, id, span, "end")
		}
		return nil, oops.Code("SCENE_END_FAILED").With("scene_id", id).Wrap(err)
	}
	return row, nil
}

// Pause transitions an active scene to the paused state and returns the
// post-update row. Only scenes currently in `active` state can be paused.
func (s *SceneStore) Pause(ctx context.Context, id string) (*SceneRow, error) {
	ctx, span := startSpan(ctx, "scene.store.pause",
		attribute.String("scene_id", id),
	)
	defer span.End()

	row := &SceneRow{}
	err := scanSceneRow(s.pool.QueryRow(ctx, `
        UPDATE scenes
        SET state = 'paused'
        WHERE id = $1 AND state = 'active'
        RETURNING `+sceneSelectColumns,
		id,
	), row)
	if err != nil {
		recordError(span, err)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, s.classifyTransitionMiss(ctx, id, span, "pause")
		}
		return nil, oops.Code("SCENE_PAUSE_FAILED").With("scene_id", id).Wrap(err)
	}
	return row, nil
}

// Resume transitions a paused scene to the active state and returns the
// post-update row. Only scenes currently in `paused` state can be resumed.
func (s *SceneStore) Resume(ctx context.Context, id string) (*SceneRow, error) {
	ctx, span := startSpan(ctx, "scene.store.resume",
		attribute.String("scene_id", id),
	)
	defer span.End()

	row := &SceneRow{}
	err := scanSceneRow(s.pool.QueryRow(ctx, `
        UPDATE scenes
        SET state = 'active'
        WHERE id = $1 AND state = 'paused'
        RETURNING `+sceneSelectColumns,
		id,
	), row)
	if err != nil {
		recordError(span, err)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, s.classifyTransitionMiss(ctx, id, span, "resume")
		}
		return nil, oops.Code("SCENE_RESUME_FAILED").With("scene_id", id).Wrap(err)
	}
	return row, nil
}

// SceneUpdate captures a partial update to a scene. Each scalar field is
// a pointer: nil means "don't update this field", non-nil means "set the
// field to the pointed-to value."
//
// Slice fields use a paired boolean (UpdateContentWarnings, UpdateTags)
// because Go's `nil slice` is indistinguishable from "empty slice" for
// the purposes of "is this field being changed?". When the boolean is
// true, the slice value (possibly empty) is written; when false, the
// slice is unchanged.
//
// This struct mirrors UpdateSceneRequest but lives in the store package
// so the store layer doesn't depend on proto-generated types.
type SceneUpdate struct {
	Title       *string
	Description *string
	Visibility  *string
	PoseOrder   *string
	LocationID  *string

	ContentWarnings       []string
	UpdateContentWarnings bool

	Tags       []string
	UpdateTags bool
}

// HasChanges reports whether the update specifies any field changes.
func (u *SceneUpdate) HasChanges() bool {
	return u.Title != nil ||
		u.Description != nil ||
		u.Visibility != nil ||
		u.PoseOrder != nil ||
		u.LocationID != nil ||
		u.UpdateContentWarnings ||
		u.UpdateTags
}

// Update applies a partial update to a scene and returns the post-update
// row. The update parameter specifies which fields to change; nil/false
// fields are left unchanged.
//
// The state of the scene is checked: ended and archived scenes cannot be
// updated. The check is enforced via the WHERE clause `state IN ('active',
// 'paused')` so concurrent updates from a transition cannot race.
//
// If `update.HasChanges()` is false, the call is a no-op: the function
// reads the current row via Get and returns it (so callers always get a
// valid SceneRow back, matching the End/Pause/Resume contract). This costs
// one query for the no-op case but keeps the API surface uniform.
//
// Returns SCENE_NOT_FOUND if the scene doesn't exist, or
// SCENE_TRANSITION_FORBIDDEN if it exists but is in a non-updatable state.
func (s *SceneStore) Update(ctx context.Context, id string, update *SceneUpdate) (*SceneRow, error) {
	ctx, span := startSpan(ctx, "scene.store.update",
		attribute.String("scene_id", id),
	)
	defer span.End()

	if update == nil || !update.HasChanges() {
		// No-op: read the current row and return it. Maintains the
		// (*SceneRow, error) API contract without an UPDATE.
		return s.Get(ctx, id)
	}

	// Build the SET clause dynamically based on which fields are present.
	//
	// SQL injection note: column names come from hard-coded string literals
	// below — never from user input — so building the SET clause via
	// fmt.Sprintf is safe. All VALUES are parameterized via $1, $2, etc.
	var (
		setParts []string
		args     []any
		argIdx   = 1
	)
	addSet := func(col string, value any) {
		setParts = append(setParts, fmt.Sprintf("%s = $%d", col, argIdx))
		args = append(args, value)
		argIdx++
	}

	if update.Title != nil {
		addSet("title", *update.Title)
	}
	if update.Description != nil {
		addSet("description", *update.Description)
	}
	if update.Visibility != nil {
		addSet("visibility", *update.Visibility)
	}
	if update.PoseOrder != nil {
		addSet("pose_order", *update.PoseOrder)
	}
	if update.LocationID != nil {
		// Empty string means "clear the location" → store NULL
		if *update.LocationID == "" {
			setParts = append(setParts, fmt.Sprintf("location_id = $%d", argIdx))
			args = append(args, nil)
			argIdx++
		} else {
			addSet("location_id", *update.LocationID)
		}
	}
	if update.UpdateContentWarnings {
		addSet("content_warnings", update.ContentWarnings)
	}
	if update.UpdateTags {
		addSet("tags", update.Tags)
	}

	// Append the WHERE-clause parameter (the scene ID) at the end.
	args = append(args, id)
	sceneIDIdx := argIdx

	query := fmt.Sprintf(
		`UPDATE scenes
         SET %s
         WHERE id = $%d AND state IN ('active', 'paused')
         RETURNING %s`,
		strings.Join(setParts, ", "),
		sceneIDIdx,
		sceneSelectColumns,
	)

	row := &SceneRow{}
	err := scanSceneRow(s.pool.QueryRow(ctx, query, args...), row)
	if err != nil {
		recordError(span, err)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, s.classifyTransitionMiss(ctx, id, span, "update")
		}
		return nil, oops.Code("SCENE_UPDATE_FAILED").With("scene_id", id).Wrap(err)
	}
	return row, nil
}

// classifyTransitionMiss is called when a transition UPDATE returned no
// row (RETURNING produced ErrNoRows). It distinguishes between two cases
// by issuing a SELECT:
//
//  1. The row doesn't exist at all → SCENE_NOT_FOUND
//  2. The row exists but is in a state that doesn't match the WHERE
//     clause → SCENE_TRANSITION_FORBIDDEN
//
// The caller passes `op` ("end", "pause", "resume", "update") for
// inclusion in the error context so consumers can tell which transition
// was attempted.
//
// This is a second round-trip in the error path, but the happy path is
// already optimal (one round trip via RETURNING). We pay the second
// query only when something went wrong, where the diagnostic value is
// worth the cost.
func (s *SceneStore) classifyTransitionMiss(ctx context.Context, id string, span trace.Span, op string) error {
	var currentState string
	err := s.pool.QueryRow(ctx, `SELECT state FROM scenes WHERE id = $1`, id).Scan(&currentState)
	if err != nil {
		recordError(span, err)
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.Code("SCENE_NOT_FOUND").
				With("scene_id", id).
				With("op", op).
				Wrap(err)
		}
		return oops.Code("SCENE_TRANSITION_CLASSIFY_FAILED").
			With("scene_id", id).
			With("op", op).
			Wrap(err)
	}
	return oops.Code("SCENE_TRANSITION_FORBIDDEN").
		With("scene_id", id).
		With("op", op).
		With("current_state", currentState).
		Errorf("scene in state %q cannot be %sed", currentState, op)
}
