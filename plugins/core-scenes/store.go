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

// CreateWithOwner is the transactional Phase 3 replacement for Create.
//
// All-or-nothing: inserts the scene row, the owner's participant row
// (role='owner'), and a lifecycle.created ops event in a single transaction.
// If any step fails, none of the rows persist.
//
// Per design decision P3.D6, this exists because Phase 3's ABAC policies
// use `principal.id in resource.scene.participants`. An owner without a
// participant row would lose access to their own scene under the new
// policies. The "create + insert owner row + emit ops event" trio MUST
// be atomic.
func (s *SceneStore) CreateWithOwner(ctx context.Context, row *SceneRow) error {
	ctx, span := startSpan(ctx, "scene.store.create_with_owner",
		attribute.String("scene_id", row.ID),
	)
	defer span.End()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		recordError(span, err)
		return oops.Code("SCENE_CREATE_FAILED").With("scene_id", row.ID).Wrap(err)
	}
	defer tx.Rollback(ctx) // no-op after Commit

	// 1. Insert the scene row.
	_, err = tx.Exec(ctx, `
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

	// 2. Insert the owner participant row.
	_, err = tx.Exec(ctx, `
		INSERT INTO scene_participants (scene_id, character_id, role, joined_at)
		VALUES ($1, $2, 'owner', NOW())`,
		row.ID, row.OwnerID,
	)
	if err != nil {
		recordError(span, err)
		return oops.Code("SCENE_CREATE_OWNER_PARTICIPANT_FAILED").
			With("scene_id", row.ID).
			With("owner_id", row.OwnerID).
			Wrap(err)
	}

	// 3. Record the lifecycle.created ops event.
	payload := map[string]any{
		"visibility":    row.Visibility,
		"from_template": row.TemplateID != nil,
	}
	if err := recordOpsEventTx(ctx, tx, row.ID, OpsKindLifecycleCreated, row.OwnerID, "", payload); err != nil {
		recordError(span, err)
		return oops.Code("SCENE_CREATE_OPS_EVENT_FAILED").
			With("scene_id", row.ID).
			Wrap(err)
	}

	if err := tx.Commit(ctx); err != nil {
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

// GetWithMembership returns the scene row plus its participants and invitees
// lists in a single SQL round trip. Used by the resolver to materialise ABAC
// attributes without two separate queries.
//
// participants contains all character IDs where role IN ('owner', 'member').
// invitees contains all character IDs where role = 'invited'.
//
// Per design decision P3.D9, this uses two array_agg subselects on the
// indexed scene_participants(scene_id, role) index. No caching layer in
// Phase 3.
func (s *SceneStore) GetWithMembership(ctx context.Context, id string) (*SceneRow, []string, []string, error) {
	ctx, span := startSpan(ctx, "scene.store.get_with_membership",
		attribute.String("scene_id", id),
	)
	defer span.End()

	row := &SceneRow{}
	var participants, invitees []string
	err := s.pool.QueryRow(ctx, `
		SELECT
			s.id, s.title, s.description, s.location_id, s.owner_id,
			s.state, s.pose_order, s.visibility, s.idle_timeout_secs,
			s.template_id, s.content_warnings, s.tags,
			s.created_at, s.ended_at, s.archived_at,
			COALESCE(
				(SELECT array_agg(character_id) FROM scene_participants
				 WHERE scene_id = s.id AND role IN ('owner', 'member')),
				'{}'::TEXT[]
			) AS participants,
			COALESCE(
				(SELECT array_agg(character_id) FROM scene_participants
				 WHERE scene_id = s.id AND role = 'invited'),
				'{}'::TEXT[]
			) AS invitees
		FROM scenes s
		WHERE s.id = $1`,
		id,
	).Scan(
		&row.ID, &row.Title, &row.Description, &row.LocationID, &row.OwnerID,
		&row.State, &row.PoseOrder, &row.Visibility, &row.IdleTimeoutSecs,
		&row.TemplateID, &row.ContentWarnings, &row.Tags,
		&row.CreatedAt, &row.EndedAt, &row.ArchivedAt,
		&participants, &invitees,
	)
	if err != nil {
		recordError(span, err)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, nil, oops.Code("SCENE_NOT_FOUND").With("scene_id", id).Wrap(err)
		}
		return nil, nil, nil, oops.Code("SCENE_GET_FAILED").With("scene_id", id).Wrap(err)
	}
	return row, participants, invitees, nil
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

// AddParticipant attempts to add characterID to sceneID. The operation is
// idempotent on identity match (calling it twice for the same character is
// a no-op) and atomically promotes invited→member.
//
// Returns:
//   - OpInserted: a fresh member row was created
//   - OpPromoted: an existing invited row was flipped to member
//   - OpNoChange: the caller was already a member or owner
//
// The single SELECT-WHERE-guarded UPSERT enforces all join eligibility
// checks at the SQL layer:
//   - Scene must exist
//   - Scene must be in active or paused state
//   - Either the scene is open OR there's an invited row for this character
//
// If the eligibility check fails, RETURNING is empty and we issue a
// diagnostic SELECT (classifyJoinMiss) to figure out the precise reason.
func (s *SceneStore) AddParticipant(ctx context.Context, sceneID, characterID string) (*ParticipantRow, ParticipantOpResult, error) {
	ctx, span := startSpan(ctx, "scene.store.add_participant",
		attribute.String("scene_id", sceneID),
		attribute.String("character_id", characterID),
	)
	defer span.End()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		recordError(span, err)
		return nil, OpNoChange, oops.Code("SCENE_JOIN_FAILED").
			With("scene_id", sceneID).With("character_id", characterID).Wrap(err)
	}
	defer tx.Rollback(ctx)

	row := &ParticipantRow{}
	var wasInserted bool
	err = tx.QueryRow(ctx, `
		INSERT INTO scene_participants (scene_id, character_id, role, joined_at)
		SELECT $1, $2, 'member', NOW()
		FROM scenes
		WHERE id = $1
		  AND state IN ('active', 'paused')
		  AND (
		    visibility = 'open'
		    OR EXISTS (
		      SELECT 1 FROM scene_participants
		      WHERE scene_id = $1 AND character_id = $2 AND role = 'invited'
		    )
		  )
		ON CONFLICT (scene_id, character_id) DO UPDATE
		  SET role = CASE WHEN scene_participants.role = 'invited' THEN 'member' ELSE scene_participants.role END,
		      joined_at = CASE WHEN scene_participants.role = 'invited' THEN NOW() ELSE scene_participants.joined_at END
		RETURNING scene_id, character_id, role, joined_at, (xmax = 0) AS was_inserted`,
		sceneID, characterID,
	).Scan(&row.SceneID, &row.CharacterID, &row.Role, &row.JoinedAt, &wasInserted)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Eligibility check failed. Classify the precise reason.
			return nil, OpNoChange, s.classifyJoinMiss(ctx, sceneID, characterID, span)
		}
		recordError(span, err)
		return nil, OpNoChange, oops.Code("SCENE_JOIN_FAILED").
			With("scene_id", sceneID).With("character_id", characterID).Wrap(err)
	}

	// Determine the result. wasInserted=true → OpInserted. Otherwise either
	// promoted (the existing row was 'invited' and is now 'member') or no
	// change (the existing row was already 'member' or 'owner').
	var result ParticipantOpResult
	if wasInserted {
		result = OpInserted
	} else {
		// We need to figure out if this was a promotion or no-op. The post-
		// update row's role is 'member' in both cases, so we can't tell from
		// the row itself. Compare the row's joined_at to the transaction
		// start time: if joined_at >= transaction_timestamp(), the CASE
		// branch fired within THIS txn (promotion). If joined_at predates
		// the txn start, the row was already a member (no-change).
		//
		// transaction_timestamp() (alias for xact_start()) is fixed for the
		// duration of the txn and is exact — it does not suffer the false-
		// positive problem of statement_timestamp() heuristics under rapid
		// back-to-back retries.
		var promoted bool
		err = tx.QueryRow(ctx, `
			SELECT joined_at >= transaction_timestamp()
			FROM scene_participants
			WHERE scene_id = $1 AND character_id = $2`,
			sceneID, characterID,
		).Scan(&promoted)
		if err != nil {
			recordError(span, err)
			return nil, OpNoChange, oops.Code("SCENE_JOIN_CLASSIFY_FAILED").
				With("scene_id", sceneID).With("character_id", characterID).Wrap(err)
		}
		if promoted {
			result = OpPromoted
		} else {
			result = OpNoChange
		}
	}

	// Emit ops event ONLY for OpInserted and OpPromoted; OpNoChange must
	// not pollute the audit log with retry events.
	if result != OpNoChange {
		// Determine visibility for the payload by reading the scene row.
		var visibility string
		err = tx.QueryRow(ctx, `SELECT visibility FROM scenes WHERE id = $1`, sceneID).Scan(&visibility)
		if err != nil {
			recordError(span, err)
			return nil, OpNoChange, oops.Code("SCENE_JOIN_OPS_EVENT_FAILED").Wrap(err)
		}
		payload := map[string]any{
			"visibility":   visibility,
			"from_invited": result == OpPromoted,
		}
		if err := recordOpsEventTx(ctx, tx, sceneID, OpsKindMembershipJoin, characterID, characterID, payload); err != nil {
			recordError(span, err)
			return nil, OpNoChange, oops.Code("SCENE_JOIN_OPS_EVENT_FAILED").Wrap(err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		recordError(span, err)
		return nil, OpNoChange, oops.Code("SCENE_JOIN_FAILED").Wrap(err)
	}
	return row, result, nil
}

// RemoveParticipant deletes the participant row for characterID in sceneID.
//
// The DELETE has a `WHERE role <> 'owner'` filter for defense-in-depth: the
// service layer rejects owner-leave first, but this prevents accidental
// owner removal if the service-layer check is ever bypassed (direct store
// call, future bug, etc.).
//
// Returns the removed row via RETURNING. Distinguishes "owner cannot leave"
// from "participant not found" via a follow-up SELECT in the error path.
func (s *SceneStore) RemoveParticipant(ctx context.Context, sceneID, characterID string) (*ParticipantRow, error) {
	ctx, span := startSpan(ctx, "scene.store.remove_participant",
		attribute.String("scene_id", sceneID),
		attribute.String("character_id", characterID),
	)
	defer span.End()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_LEAVE_FAILED").
			With("scene_id", sceneID).With("character_id", characterID).Wrap(err)
	}
	defer tx.Rollback(ctx)

	row := &ParticipantRow{}
	err = tx.QueryRow(ctx, `
		DELETE FROM scene_participants
		WHERE scene_id = $1 AND character_id = $2 AND role <> 'owner'
		RETURNING scene_id, character_id, role, joined_at`,
		sceneID, characterID,
	).Scan(&row.SceneID, &row.CharacterID, &row.Role, &row.JoinedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Either the row doesn't exist or it was the owner. Distinguish.
			var existingRole string
			err2 := tx.QueryRow(ctx, `
				SELECT role FROM scene_participants WHERE scene_id = $1 AND character_id = $2`,
				sceneID, characterID,
			).Scan(&existingRole)
			if err2 != nil {
				if errors.Is(err2, pgx.ErrNoRows) {
					return nil, oops.Code("SCENE_PARTICIPANT_NOT_FOUND").
						With("scene_id", sceneID).
						With("character_id", characterID).
						Wrap(err)
				}
				recordError(span, err2)
				return nil, oops.Code("SCENE_LEAVE_CLASSIFY_FAILED").
					With("scene_id", sceneID).Wrap(err2)
			}
			if existingRole == "owner" {
				return nil, oops.Code("SCENE_OWNER_CANNOT_LEAVE").
					With("scene_id", sceneID).
					With("character_id", characterID).
					Errorf("scene owners cannot leave; use scene end or transfer ownership")
			}
			// Shouldn't happen — DELETE matched no row but SELECT did?
			return nil, oops.Code("SCENE_LEAVE_FAILED").Errorf("unexpected state")
		}
		recordError(span, err)
		return nil, oops.Code("SCENE_LEAVE_FAILED").Wrap(err)
	}

	payload := map[string]any{"prior_role": row.Role}
	if err := recordOpsEventTx(ctx, tx, sceneID, OpsKindMembershipLeave, characterID, characterID, payload); err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_LEAVE_OPS_EVENT_FAILED").Wrap(err)
	}

	if err := tx.Commit(ctx); err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_LEAVE_FAILED").Wrap(err)
	}
	return row, nil
}

// InviteParticipant inserts a participant row with role='invited'. Idempotent
// on identity match (re-inviting an already-invited character is a no-op,
// no second ops event). Rejected for existing members or owners with
// SCENE_INVITE_TARGET_ALREADY_MEMBER.
func (s *SceneStore) InviteParticipant(ctx context.Context, sceneID, inviterID, targetID string) (*ParticipantRow, error) {
	ctx, span := startSpan(ctx, "scene.store.invite_participant",
		attribute.String("scene_id", sceneID),
		attribute.String("inviter_id", inviterID),
		attribute.String("target_id", targetID),
	)
	defer span.End()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_INVITE_FAILED").Wrap(err)
	}
	defer tx.Rollback(ctx)

	// Check existing role for target — distinguish "already invited" (no-op),
	// "already member/owner" (error), and "not present" (insert).
	var existingRole string
	err = tx.QueryRow(ctx,
		`SELECT role FROM scene_participants WHERE scene_id = $1 AND character_id = $2`,
		sceneID, targetID,
	).Scan(&existingRole)

	switch {
	case err == nil:
		if existingRole == "invited" {
			// Idempotent no-op — return the existing row, no new ops event.
			row := &ParticipantRow{SceneID: sceneID, CharacterID: targetID, Role: "invited"}
			return row, tx.Commit(ctx)
		}
		// Already member or owner — reject.
		return nil, oops.Code("SCENE_INVITE_TARGET_ALREADY_MEMBER").
			With("scene_id", sceneID).
			With("target_id", targetID).
			With("current_role", existingRole).
			Errorf("character is already a %s", existingRole)
	case errors.Is(err, pgx.ErrNoRows):
		// Not present — fall through to insert.
	default:
		recordError(span, err)
		return nil, oops.Code("SCENE_INVITE_FAILED").Wrap(err)
	}

	row := &ParticipantRow{}
	err = tx.QueryRow(ctx, `
		INSERT INTO scene_participants (scene_id, character_id, role, joined_at)
		VALUES ($1, $2, 'invited', NOW())
		RETURNING scene_id, character_id, role, joined_at`,
		sceneID, targetID,
	).Scan(&row.SceneID, &row.CharacterID, &row.Role, &row.JoinedAt)
	if err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_INVITE_FAILED").
			With("scene_id", sceneID).With("target_id", targetID).Wrap(err)
	}

	if err := recordOpsEventTx(ctx, tx, sceneID, OpsKindMembershipInvite, inviterID, targetID, nil); err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_INVITE_OPS_EVENT_FAILED").Wrap(err)
	}

	if err := tx.Commit(ctx); err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_INVITE_FAILED").Wrap(err)
	}
	return row, nil
}

// KickParticipant deletes the target's participant row. The DELETE filter is
// `WHERE role <> 'owner'` so the owner cannot be kicked even by themselves.
// Removes both 'member' and 'invited' rows in a single statement (i.e., kick
// also withdraws pending invitations).
//
// Returns SCENE_KICK_FORBIDDEN if the target is the owner; SCENE_PARTICIPANT_NOT_FOUND
// if the target isn't in the scene at all.
func (s *SceneStore) KickParticipant(ctx context.Context, sceneID, kickerID, targetID string) (*ParticipantRow, error) {
	ctx, span := startSpan(ctx, "scene.store.kick_participant",
		attribute.String("scene_id", sceneID),
		attribute.String("kicker_id", kickerID),
		attribute.String("target_id", targetID),
	)
	defer span.End()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_KICK_FAILED").Wrap(err)
	}
	defer tx.Rollback(ctx)

	row := &ParticipantRow{}
	err = tx.QueryRow(ctx, `
		DELETE FROM scene_participants
		WHERE scene_id = $1 AND character_id = $2 AND role <> 'owner'
		RETURNING scene_id, character_id, role, joined_at`,
		sceneID, targetID,
	).Scan(&row.SceneID, &row.CharacterID, &row.Role, &row.JoinedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Distinguish "owner" from "not present".
			var existing string
			err2 := tx.QueryRow(ctx,
				`SELECT role FROM scene_participants WHERE scene_id = $1 AND character_id = $2`,
				sceneID, targetID,
			).Scan(&existing)
			if err2 != nil {
				if errors.Is(err2, pgx.ErrNoRows) {
					return nil, oops.Code("SCENE_PARTICIPANT_NOT_FOUND").
						With("scene_id", sceneID).With("target_id", targetID).Wrap(err)
				}
				recordError(span, err2)
				return nil, oops.Code("SCENE_KICK_CLASSIFY_FAILED").Wrap(err2)
			}
			if existing == "owner" {
				return nil, oops.Code("SCENE_KICK_FORBIDDEN").
					With("scene_id", sceneID).
					With("target_id", targetID).
					Errorf("scene owner cannot be kicked")
			}
			return nil, oops.Code("SCENE_KICK_FAILED").Errorf("unexpected state")
		}
		recordError(span, err)
		return nil, oops.Code("SCENE_KICK_FAILED").Wrap(err)
	}

	payload := map[string]any{"prior_role": row.Role}
	if err := recordOpsEventTx(ctx, tx, sceneID, OpsKindMembershipKick, kickerID, targetID, payload); err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_KICK_OPS_EVENT_FAILED").Wrap(err)
	}

	if err := tx.Commit(ctx); err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_KICK_FAILED").Wrap(err)
	}
	return row, nil
}

// TransferOwnership performs a 3-statement transactional ownership swap:
//  1. Demote current owner: UPDATE scene_participants SET role='member' WHERE owner
//  2. Promote target: UPDATE scene_participants SET role='owner' WHERE member
//  3. Update denormalised owner_id: UPDATE scenes SET owner_id = $new
//
// All three statements MUST succeed (non-empty RETURNING). Rolls back otherwise.
// Idempotent when newOwnerID == currentOwnerID (returns nil without changes).
func (s *SceneStore) TransferOwnership(ctx context.Context, sceneID, currentOwnerID, newOwnerID string) error {
	ctx, span := startSpan(ctx, "scene.store.transfer_ownership",
		attribute.String("scene_id", sceneID),
		attribute.String("current_owner", currentOwnerID),
		attribute.String("new_owner", newOwnerID),
	)
	defer span.End()

	if currentOwnerID == newOwnerID {
		return nil // idempotent no-op
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		recordError(span, err)
		return oops.Code("SCENE_TRANSFER_FAILED").Wrap(err)
	}
	defer tx.Rollback(ctx)

	// Statement 1: demote current owner.
	var demotedID string
	err = tx.QueryRow(ctx, `
		UPDATE scene_participants SET role = 'member'
		WHERE scene_id = $1 AND character_id = $2 AND role = 'owner'
		RETURNING character_id`,
		sceneID, currentOwnerID,
	).Scan(&demotedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return s.classifyTransferMiss(ctx, sceneID, currentOwnerID, newOwnerID, span)
		}
		recordError(span, err)
		return oops.Code("SCENE_TRANSFER_FAILED").Wrap(err)
	}

	// Statement 2: promote target (must currently be a member, not invited).
	var promotedID string
	err = tx.QueryRow(ctx, `
		UPDATE scene_participants SET role = 'owner'
		WHERE scene_id = $1 AND character_id = $2 AND role = 'member'
		RETURNING character_id`,
		sceneID, newOwnerID,
	).Scan(&promotedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return s.classifyTransferMiss(ctx, sceneID, currentOwnerID, newOwnerID, span)
		}
		recordError(span, err)
		return oops.Code("SCENE_TRANSFER_FAILED").Wrap(err)
	}

	// Statement 3: update denormalised owner_id, gated on state.
	var sceneIDOut string
	err = tx.QueryRow(ctx, `
		UPDATE scenes SET owner_id = $1
		WHERE id = $2 AND owner_id = $3 AND state IN ('active', 'paused')
		RETURNING id`,
		newOwnerID, sceneID, currentOwnerID,
	).Scan(&sceneIDOut)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return s.classifyTransferMiss(ctx, sceneID, currentOwnerID, newOwnerID, span)
		}
		recordError(span, err)
		return oops.Code("SCENE_TRANSFER_FAILED").Wrap(err)
	}

	payload := map[string]any{"from": currentOwnerID}
	if err := recordOpsEventTx(ctx, tx, sceneID, OpsKindMembershipOwnershipTransferred, currentOwnerID, newOwnerID, payload); err != nil {
		recordError(span, err)
		return oops.Code("SCENE_TRANSFER_OPS_EVENT_FAILED").Wrap(err)
	}

	if err := tx.Commit(ctx); err != nil {
		recordError(span, err)
		return oops.Code("SCENE_TRANSFER_FAILED").Wrap(err)
	}
	return nil
}

// classifyTransferMiss diagnoses why TransferOwnership's UPDATE chain failed.
// Issues a single SELECT to figure out the precise reason.
func (s *SceneStore) classifyTransferMiss(ctx context.Context, sceneID, currentOwnerID, newOwnerID string, span trace.Span) error {
	// Check the scene exists and its state.
	var (
		state         string
		actualOwnerID string
	)
	err := s.pool.QueryRow(ctx,
		`SELECT state, owner_id FROM scenes WHERE id = $1`,
		sceneID,
	).Scan(&state, &actualOwnerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.Code("SCENE_NOT_FOUND").With("scene_id", sceneID).Wrap(err)
		}
		recordError(span, err)
		return oops.Code("SCENE_TRANSFER_CLASSIFY_FAILED").Wrap(err)
	}
	if state != "active" && state != "paused" {
		return oops.Code("SCENE_TRANSITION_FORBIDDEN").
			With("scene_id", sceneID).With("op", "transfer-ownership").
			With("current_state", state).
			Errorf("scene in state %q cannot have ownership transferred", state)
	}
	if actualOwnerID != currentOwnerID {
		return oops.Code("SCENE_NOT_OWNER").
			With("scene_id", sceneID).
			With("caller", currentOwnerID).
			With("actual_owner", actualOwnerID).
			Errorf("caller is not the current owner")
	}
	// State is OK and caller IS the owner; the failure must be the target.
	var targetRole string
	err = s.pool.QueryRow(ctx,
		`SELECT role FROM scene_participants WHERE scene_id = $1 AND character_id = $2`,
		sceneID, newOwnerID,
	).Scan(&targetRole)
	if err != nil || targetRole != "member" {
		return oops.Code("SCENE_TRANSFER_TARGET_NOT_MEMBER").
			With("scene_id", sceneID).
			With("target_id", newOwnerID).
			Errorf("transfer target must be an existing member")
	}
	return oops.Code("SCENE_TRANSFER_FAILED").Errorf("unexpected classify state")
}

// ListParticipants returns all participants for a scene, ordered by joined_at
// ASC (so the owner appears first since CreateWithOwner inserts them at scene
// creation).
func (s *SceneStore) ListParticipants(ctx context.Context, sceneID string) ([]ParticipantRow, error) {
	ctx, span := startSpan(ctx, "scene.store.list_participants",
		attribute.String("scene_id", sceneID),
	)
	defer span.End()

	rows, err := s.pool.Query(ctx, `
		SELECT scene_id, character_id, role, joined_at
		FROM scene_participants
		WHERE scene_id = $1
		ORDER BY joined_at ASC`,
		sceneID,
	)
	if err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_LIST_PARTICIPANTS_FAILED").
			With("scene_id", sceneID).Wrap(err)
	}
	defer rows.Close()

	var out []ParticipantRow
	for rows.Next() {
		var p ParticipantRow
		if err := rows.Scan(&p.SceneID, &p.CharacterID, &p.Role, &p.JoinedAt); err != nil {
			recordError(span, err)
			return nil, oops.Code("SCENE_LIST_PARTICIPANTS_FAILED").Wrap(err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_LIST_PARTICIPANTS_FAILED").Wrap(err)
	}
	return out, nil
}

// GetParticipant returns a single participant row, or SCENE_PARTICIPANT_NOT_FOUND.
func (s *SceneStore) GetParticipant(ctx context.Context, sceneID, characterID string) (*ParticipantRow, error) {
	ctx, span := startSpan(ctx, "scene.store.get_participant",
		attribute.String("scene_id", sceneID),
		attribute.String("character_id", characterID),
	)
	defer span.End()

	p := &ParticipantRow{}
	err := s.pool.QueryRow(ctx, `
		SELECT scene_id, character_id, role, joined_at
		FROM scene_participants
		WHERE scene_id = $1 AND character_id = $2`,
		sceneID, characterID,
	).Scan(&p.SceneID, &p.CharacterID, &p.Role, &p.JoinedAt)
	if err != nil {
		recordError(span, err)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.Code("SCENE_PARTICIPANT_NOT_FOUND").
				With("scene_id", sceneID).
				With("character_id", characterID).Wrap(err)
		}
		return nil, oops.Code("SCENE_GET_PARTICIPANT_FAILED").
			With("scene_id", sceneID).
			With("character_id", characterID).Wrap(err)
	}
	return p, nil
}

// classifyJoinMiss issues one diagnostic SELECT to figure out which
// precondition failed when AddParticipant's RETURNING was empty. Pays the
// extra round trip ONLY in the error path; the happy path is single-statement.
//
// Returns one of:
//   - SCENE_NOT_FOUND
//   - SCENE_TRANSITION_FORBIDDEN (with current_state in context)
//   - SCENE_JOIN_NOT_INVITED (private scene, no invitation)
func (s *SceneStore) classifyJoinMiss(ctx context.Context, sceneID, characterID string, span trace.Span) error {
	var (
		state      string
		visibility string
	)
	err := s.pool.QueryRow(ctx, `
		SELECT state, visibility FROM scenes WHERE id = $1`,
		sceneID,
	).Scan(&state, &visibility)
	if err != nil {
		recordError(span, err)
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.Code("SCENE_NOT_FOUND").
				With("scene_id", sceneID).
				With("op", "join").
				Wrap(err)
		}
		return oops.Code("SCENE_JOIN_CLASSIFY_FAILED").
			With("scene_id", sceneID).
			With("op", "join").
			Wrap(err)
	}

	if state != "active" && state != "paused" {
		return oops.Code("SCENE_TRANSITION_FORBIDDEN").
			With("scene_id", sceneID).
			With("op", "join").
			With("current_state", state).
			Errorf("scene in state %q cannot be joined", state)
	}

	// State is OK. The remaining reason is private scene without invitation.
	return oops.Code("SCENE_JOIN_NOT_INVITED").
		With("scene_id", sceneID).
		With("character_id", characterID).
		With("visibility", visibility).
		Errorf("character not invited to private scene")
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
