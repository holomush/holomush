// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/samber/oops"
	"go.opentelemetry.io/otel/attribute"

	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/pgnanos"
)

// CreatePublishAttemptInput is the argument to CreatePublishAttempt. The
// windows are frozen onto the attempt row so a later config change does
// not retroactively alter an in-flight attempt (spec §3.1).
type CreatePublishAttemptInput struct {
	SceneID       string
	AttemptNumber int
	InitiatedBy   string
	VoteWindow    time.Duration
	CoolOffWindow time.Duration
	MaxAttempts   int
}

// CreatePublishAttempt creates a published_scenes row in COLLECTING status
// and seeds published_scene_votes from the scene's owner+member
// participants in a single transaction. Invited participants are excluded
// from the roster (INV-P6-1). The partial unique index
// published_scenes_one_active_per_scene enforces "at most one active
// attempt per scene"; published_scenes_one_published_per_scene enforces
// one-and-done. Fails closed with SCENE_PUBLISH_NO_ELIGIBLE_VOTERS when the
// roster would be empty. See spec §3.3, §4.1.
func (s *SceneStore) CreatePublishAttempt(ctx context.Context, in CreatePublishAttemptInput) (*PublishedScene, error) {
	ctx, span := startSpan(ctx, "scene.store.create_publish_attempt",
		attribute.String("scene_id", in.SceneID))
	defer span.End()

	id := idgen.New().String()
	initiatedAt := time.Now()

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, oops.Code("SCENE_PUBLISH_TX_BEGIN_FAILED").Wrap(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	// Insert the attempt. The partial unique indexes catch a concurrent or
	// duplicate active/published attempt for the same scene.
	_, err = tx.Exec(ctx, `
		INSERT INTO published_scenes (
			id, scene_id, attempt_number, status, initiated_by, initiated_at,
			vote_window, cooloff_window, max_attempts_snapshot
		) VALUES ($1, $2, $3, 'COLLECTING', $4, $5, $6, $7, $8)
	`, id, in.SceneID, in.AttemptNumber, in.InitiatedBy, pgnanos.From(initiatedAt),
		durationToInterval(in.VoteWindow), durationToInterval(in.CoolOffWindow), in.MaxAttempts)
	if err != nil {
		switch {
		case isUniqueViolation(err, "published_scenes_one_active_per_scene"):
			return nil, oops.Code("SCENE_PUBLISH_ALREADY_ACTIVE").
				With("scene_id", in.SceneID).Wrap(err)
		case isUniqueViolation(err, "published_scenes_one_published_per_scene"):
			return nil, oops.Code("SCENE_PUBLISH_ALREADY_PUBLISHED").
				With("scene_id", in.SceneID).Wrap(err)
		case isUniqueViolation(err, "published_scenes_attempt_unique"):
			return nil, oops.Code("SCENE_PUBLISH_ATTEMPT_NUMBER_TAKEN").
				With("scene_id", in.SceneID).
				With("attempt_number", in.AttemptNumber).Wrap(err)
		}
		return nil, oops.Code("SCENE_PUBLISH_CREATE_FAILED").Wrap(err)
	}

	// Seed the roster from owner+member participants (NOT invited — INV-P6-1).
	tag, err := tx.Exec(ctx, `
		INSERT INTO published_scene_votes (published_scene_id, character_id)
		SELECT $1, character_id FROM scene_participants
		WHERE scene_id = $2 AND role IN ('owner', 'member')
	`, id, in.SceneID)
	if err != nil {
		return nil, oops.Code("SCENE_PUBLISH_SEED_ROSTER_FAILED").Wrap(err)
	}

	// Fail closed if the roster is empty — an attempt with no eligible voters
	// can never resolve (INV-P6-1). The INSERT...SELECT's affected-row count
	// is the roster size, so no separate count query is needed.
	if tag.RowsAffected() == 0 {
		return nil, oops.Code("SCENE_PUBLISH_NO_ELIGIBLE_VOTERS").
			With("scene_id", in.SceneID).
			Errorf("scene has no owner or member participants to seed the vote roster")
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, oops.Code("SCENE_PUBLISH_COMMIT_FAILED").Wrap(err)
	}

	return &PublishedScene{
		ID:                  id,
		SceneID:             in.SceneID,
		AttemptNumber:       in.AttemptNumber,
		Status:              StatusCollecting,
		InitiatedBy:         in.InitiatedBy,
		InitiatedAt:         pgnanos.From(initiatedAt),
		VoteWindow:          in.VoteWindow,
		CoolOffWindow:       in.CoolOffWindow,
		MaxAttemptsSnapshot: in.MaxAttempts,
	}, nil
}

// ListPublishVoters returns all voter rows for an attempt, including
// pending (vote IS NULL) rows, ordered by character ID. Used by the
// resolution check and observability.
func (s *SceneStore) ListPublishVoters(ctx context.Context, publishedSceneID string) ([]PublishedSceneVote, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT published_scene_id, character_id, vote, voted_at, last_changed_at
		FROM published_scene_votes
		WHERE published_scene_id = $1
		ORDER BY character_id
	`, publishedSceneID)
	if err != nil {
		return nil, oops.Code("SCENE_PUBLISH_LIST_VOTERS_FAILED").Wrap(err)
	}
	defer rows.Close()

	var out []PublishedSceneVote
	for rows.Next() {
		var v PublishedSceneVote
		// voted_at / last_changed_at are BIGINT epoch-nanos columns;
		// *pgnanos.Time satisfies the pgx Scanner and handles NULL.
		if err := rows.Scan(
			&v.PublishedSceneID, &v.CharacterID, &v.Vote, &v.VotedAt, &v.LastChangedAt,
		); err != nil {
			return nil, oops.Code("SCENE_PUBLISH_LIST_VOTERS_SCAN_FAILED").Wrap(err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("SCENE_PUBLISH_LIST_VOTERS_ITER_FAILED").Wrap(err)
	}
	return out, nil
}

// durationToInterval encodes a Go duration as a pgtype.Interval using
// microsecond precision. Vote and cool-off windows are minutes-to-days, so
// microsecond precision is ample; encoding purely as Microseconds (no
// Months/Days components) avoids calendar/DST ambiguity on read-back.
func durationToInterval(d time.Duration) pgtype.Interval {
	return pgtype.Interval{Microseconds: d.Microseconds(), Valid: true}
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation (SQLSTATE 23505) raised by the named index/constraint.
func isUniqueViolation(err error, constraintName string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && pgErr.ConstraintName == constraintName
}
