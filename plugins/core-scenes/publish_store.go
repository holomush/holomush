// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
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
// from the roster (INV-SCENE-28). The partial unique index
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

	// Seed the roster from owner+member participants (NOT invited — INV-SCENE-28).
	tag, err := tx.Exec(ctx, `
		INSERT INTO published_scene_votes (published_scene_id, character_id)
		SELECT $1, character_id FROM scene_participants
		WHERE scene_id = $2 AND role IN ('owner', 'member')
	`, id, in.SceneID)
	if err != nil {
		return nil, oops.Code("SCENE_PUBLISH_SEED_ROSTER_FAILED").Wrap(err)
	}

	// Fail closed if the roster is empty — an attempt with no eligible voters
	// can never resolve (INV-SCENE-28). The INSERT...SELECT's affected-row count
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

// CastVoteResult reports the outcome of a CastVote upsert.
type CastVoteResult struct {
	Vote     bool
	IsChange bool
}

// CastVote upserts a voter's vote onto the frozen roster. The voter MUST
// already be on the roster (a published_scene_votes row exists); a caller
// not on the roster is rejected with SCENE_PUBLISH_NOT_A_VOTER. The roster
// is immutable after attempt creation, so the prior-vote lookup reliably
// distinguishes non-voters from pending voters.
//
// The whole cast runs in one transaction that first SELECTs the attempt row
// FOR UPDATE and re-validates its status is non-terminal. That lock is the
// same published_scenes row the snapshot pipeline takes in its write-tx
// (LockForSnapshot; snapshot atomicity, spec §11.3), so a vote serializes
// strictly against a concurrent resolution: if the attempt transitions to
// PUBLISHED/ATTEMPT_FAILED first, the cast is rejected with
// SCENE_PUBLISH_INVALID_STATE rather than landing on a terminal attempt; if the
// cast wins the lock, the snapshot's own in-tx re-validation — whose SELECT FOR
// UPDATE is the serialization point (INV-CRYPTO-33) — observes the vote consistently.
// This closes the TOCTOU between the handler's terminal-status check
// (publish_service.go CastPublishSceneVote) and the vote write (holomush-wn612).
// Status is checked before the roster lookup, so a vote on a terminal attempt
// yields INVALID_STATE for any caller — consistent with the handler, which also
// rejects a terminal attempt before it reaches this method.
//
// IsChange is true ONLY when a prior NON-NULL vote existed and differs from
// the new value — i.e. a genuine flip. The first cast (pending → value) and
// a re-affirmation (same value) both report IsChange=false. voted_at is set
// once (first cast) via COALESCE; last_changed_at advances on every cast.
// See spec §4.2, §4.3.
func (s *SceneStore) CastVote(ctx context.Context, publishedSceneID, characterID string, vote bool) (*CastVoteResult, error) {
	ctx, span := startSpan(ctx, "scene.store.cast_vote",
		attribute.String("published_scene_id", publishedSceneID),
		attribute.String("character_id", characterID))
	defer span.End()

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, oops.Code("SCENE_PUBLISH_CAST_TX_BEGIN_FAILED").Wrap(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	// Lock the attempt row and re-validate non-terminal status under the lock.
	var statusStr string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM published_scenes WHERE id = $1 FOR UPDATE`,
		publishedSceneID).Scan(&statusStr); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.Code("SCENE_PUBLISH_NOT_FOUND").
				With("published_scene_id", publishedSceneID).Wrap(err)
		}
		return nil, oops.Code("SCENE_PUBLISH_CAST_LOCK_FAILED").Wrap(err)
	}
	if PublishedSceneStatus(statusStr).IsTerminal() {
		return nil, oops.Code("SCENE_PUBLISH_INVALID_STATE").
			With("published_scene_id", publishedSceneID).
			With("status", statusStr).
			Errorf("vote on a terminal publication attempt is rejected")
	}

	var prior *bool
	if err := tx.QueryRow(ctx,
		`SELECT vote FROM published_scene_votes
		 WHERE published_scene_id = $1 AND character_id = $2`,
		publishedSceneID, characterID).Scan(&prior); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.Code("SCENE_PUBLISH_NOT_A_VOTER").
				With("published_scene_id", publishedSceneID).
				With("character_id", characterID).Wrap(err)
		}
		return nil, oops.Code("SCENE_PUBLISH_CAST_LOOKUP_FAILED").Wrap(err)
	}

	// A change is a flip of an existing vote — not the first cast.
	isChange := prior != nil && *prior != vote

	if _, err := tx.Exec(ctx, `
		UPDATE published_scene_votes
		SET vote = $1,
		    voted_at = COALESCE(voted_at, $2),
		    last_changed_at = $2
		WHERE published_scene_id = $3 AND character_id = $4
	`, vote, pgnanos.From(time.Now()), publishedSceneID, characterID); err != nil {
		return nil, oops.Code("SCENE_PUBLISH_CAST_UPDATE_FAILED").Wrap(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, oops.Code("SCENE_PUBLISH_CAST_COMMIT_FAILED").Wrap(err)
	}

	return &CastVoteResult{Vote: vote, IsChange: isChange}, nil
}

// VoteTally is the yes/no/pending breakdown across an attempt's roster.
type VoteTally struct {
	Yes     int
	No      int
	Pending int
}

// TallyVotes counts yes/no/pending across all voters for an attempt in a
// single query. Pending = roster rows whose vote is still NULL.
func (s *SceneStore) TallyVotes(ctx context.Context, publishedSceneID string) (*VoteTally, error) {
	var t VoteTally
	if err := s.pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE vote = true)  AS yes,
			count(*) FILTER (WHERE vote = false) AS no,
			count(*) FILTER (WHERE vote IS NULL) AS pending
		FROM published_scene_votes WHERE published_scene_id = $1
	`, publishedSceneID).Scan(&t.Yes, &t.No, &t.Pending); err != nil {
		return nil, oops.Code("SCENE_PUBLISH_TALLY_FAILED").Wrap(err)
	}
	return &t, nil
}

// TallyVotesTx is TallyVotes scoped to the caller's transaction. The snapshot
// pipeline re-validates the all-yes invariant inside the write-tx (after the
// SELECT FOR UPDATE serialization point) using this so a vote-flip that landed
// before the lock is observed consistently (spec §11.3, INV-CRYPTO-33).
func (s *SceneStore) TallyVotesTx(ctx context.Context, tx pgx.Tx, publishedSceneID string) (*VoteTally, error) {
	var t VoteTally
	if err := tx.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE vote = true)  AS yes,
			count(*) FILTER (WHERE vote = false) AS no,
			count(*) FILTER (WHERE vote IS NULL) AS pending
		FROM published_scene_votes WHERE published_scene_id = $1
	`, publishedSceneID).Scan(&t.Yes, &t.No, &t.Pending); err != nil {
		return nil, oops.Code("SCENE_PUBLISH_TALLY_FAILED").Wrap(err)
	}
	return &t, nil
}

// publishedSceneHeaderColumns is the column list for a header read — every
// published_scenes column EXCEPT content_entries. The deliberate omission of
// content_entries is load-bearing for INV-SCENE-32: the participant gate runs
// between GetPublishedSceneHeader and GetPublishedSceneContent, so the header
// read MUST NOT carry IC content.
const publishedSceneHeaderColumns = `
	id, scene_id, attempt_number, status, initiated_by, initiated_at,
	cooloff_started_at, resolved_at, vote_window, cooloff_window,
	max_attempts_snapshot, title_snapshot, participants_snapshot,
	published_at, failure_reason`

// scanPublishedSceneHeader scans a row selected with publishedSceneHeaderColumns
// into a PublishedScene (ContentEntries left nil). Returns the raw scan error
// (so callers can test pgx.ErrNoRows) except for decode failures, which are
// wrapped with an oops code.
func scanPublishedSceneHeader(row pgx.Row) (*PublishedScene, error) {
	var (
		pub        PublishedScene
		statusStr  string
		voteIv     pgtype.Interval
		coolIv     pgtype.Interval
		titleSnap  *string
		partsRaw   []byte
		failureStr *string
	)
	if err := row.Scan(
		&pub.ID, &pub.SceneID, &pub.AttemptNumber, &statusStr, &pub.InitiatedBy,
		&pub.InitiatedAt, &pub.CoolOffStartedAt, &pub.ResolvedAt,
		&voteIv, &coolIv, &pub.MaxAttemptsSnapshot, &titleSnap, &partsRaw,
		&pub.PublishedAt, &failureStr,
	); err != nil {
		return nil, err //nolint:wrapcheck // raw scan error preserved so callers can test pgx.ErrNoRows
	}
	pub.Status = PublishedSceneStatus(statusStr)
	pub.VoteWindow = intervalToDuration(voteIv)
	pub.CoolOffWindow = intervalToDuration(coolIv)
	pub.TitleSnapshot = titleSnap
	if len(partsRaw) > 0 {
		if err := json.Unmarshal(partsRaw, &pub.ParticipantsSnapshot); err != nil {
			return nil, oops.Code("SCENE_PUBLISH_PARTICIPANTS_DECODE_FAILED").Wrap(err)
		}
	}
	if failureStr != nil {
		fr := PublishFailureReason(*failureStr)
		pub.FailureReason = &fr
	}
	return &pub, nil
}

// intervalToDuration decodes a pgtype.Interval to a Go duration. It mirrors
// durationToInterval (which encodes Microseconds only); Days is included
// defensively, Months ignored (never written, and month→duration is
// calendar-ambiguous).
func intervalToDuration(iv pgtype.Interval) time.Duration {
	return time.Duration(iv.Microseconds)*time.Microsecond +
		time.Duration(iv.Days)*24*time.Hour
}

// GetPublishedSceneHeader returns the attempt row WITHOUT content_entries, or
// (nil, nil) when no row exists. Callers needing content call
// GetPublishedSceneContent separately, after the participant gate (INV-SCENE-32).
func (s *SceneStore) GetPublishedSceneHeader(ctx context.Context, id string) (*PublishedScene, error) {
	ctx, span := startSpan(ctx, "scene.store.get_publish_header",
		attribute.String("published_scene_id", id))
	defer span.End()

	pub, err := scanPublishedSceneHeader(s.pool.QueryRow(ctx,
		`SELECT `+publishedSceneHeaderColumns+` FROM published_scenes WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, oops.Code("SCENE_PUBLISH_HEADER_READ_FAILED").Wrap(err)
	}
	return pub, nil
}

// GetPublishedSceneContent returns the frozen content entries for an attempt,
// or nil when the row does not exist or has no content (non-PUBLISHED rows
// have NULL content_entries). MUST only be called after the participant gate
// has approved the caller for a participant-gated RPC (INV-SCENE-32).
func (s *SceneStore) GetPublishedSceneContent(ctx context.Context, id string) ([]PublishedSceneEntry, error) {
	var raw []byte
	if err := s.pool.QueryRow(
		ctx,
		`SELECT content_entries FROM published_scenes WHERE id = $1`, id,
	).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, oops.Code("SCENE_PUBLISH_CONTENT_READ_FAILED").Wrap(err)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var entries []PublishedSceneEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, oops.Code("SCENE_PUBLISH_CONTENT_DECODE_FAILED").Wrap(err)
	}
	return entries, nil
}

// TransitionInput describes a status transition and its side effects.
type TransitionInput struct {
	To            PublishedSceneStatus
	FailureReason *PublishFailureReason // set when To == ATTEMPT_FAILED
	SetCoolOffAt  *time.Time            // set when entering COOLOFF
	ClearCoolOff  bool                  // true when leaving COOLOFF (flip-back)
	Resolved      bool                  // sets resolved_at (terminal transitions)
}

// legalFromStatuses returns the source statuses from which a transition to
// `to` is legal, per the spec §4.1 state machine. An empty result means `to`
// is not a legal transition target.
func legalFromStatuses(to PublishedSceneStatus) []string {
	switch to {
	case StatusCoolOff:
		return []string{string(StatusCollecting)}
	case StatusCollecting:
		return []string{string(StatusCoolOff)}
	case StatusPublished:
		return []string{string(StatusCoolOff)}
	case StatusAttemptFailed:
		return []string{string(StatusCollecting), string(StatusCoolOff)}
	default:
		return nil
	}
}

// TransitionStatus applies a status transition with its side effects in a
// single UPDATE. Legality is enforced by a precondition WHERE clause: the row
// is updated only when its current status is a legal source for the target.
// Zero rows updated → SCENE_PUBLISH_INVALID_TRANSITION (the row is missing or
// not in a legal source status). See spec §4.1.
func (s *SceneStore) TransitionStatus(ctx context.Context, id string, in TransitionInput) error {
	ctx, span := startSpan(ctx, "scene.store.transition_status",
		attribute.String("published_scene_id", id),
		attribute.String("to", string(in.To)))
	defer span.End()

	legalFrom := legalFromStatuses(in.To)
	if len(legalFrom) == 0 {
		return oops.Code("SCENE_PUBLISH_INVALID_TRANSITION").
			With("to", string(in.To)).Errorf("unknown transition target status")
	}

	now := pgnanos.From(time.Now())
	var coolOffAt *pgnanos.Time
	if in.SetCoolOffAt != nil {
		t := pgnanos.From(*in.SetCoolOffAt)
		coolOffAt = &t
	}
	var failure *string
	if in.FailureReason != nil {
		fr := string(*in.FailureReason)
		failure = &fr
	}

	tag, err := s.pool.Exec(ctx, `
		UPDATE published_scenes
		SET status = $2,
		    cooloff_started_at = CASE
		        WHEN $3::boolean THEN $4
		        WHEN $5::boolean THEN NULL
		        ELSE cooloff_started_at END,
		    resolved_at  = CASE WHEN $6::boolean THEN $7 ELSE resolved_at END,
		    published_at = CASE WHEN $2 = 'PUBLISHED' THEN $7 ELSE published_at END,
		    failure_reason = COALESCE($8, failure_reason)
		WHERE id = $1 AND status = ANY($9::text[])
	`, id, string(in.To), in.SetCoolOffAt != nil, coolOffAt, in.ClearCoolOff,
		in.Resolved, now, failure, legalFrom)
	if err != nil {
		return oops.Code("SCENE_PUBLISH_TRANSITION_FAILED").Wrap(err)
	}
	if tag.RowsAffected() == 0 {
		return oops.Code("SCENE_PUBLISH_INVALID_TRANSITION").
			With("id", id).With("to", string(in.To)).
			Errorf("row missing or not in a legal source status for this transition")
	}
	return nil
}

// FailAttemptTx transitions an attempt to ATTEMPT_FAILED with the given
// failure_reason inside the caller's transaction. Used by the snapshot pipeline
// for COOLOFF→ATTEMPT_FAILED on decrypt/render failure or a broken all-yes
// invariant (spec §11.4). The precondition WHERE status='COOLOFF' makes a
// flipped row a no-op (zero rows → SCENE_PUBLISH_INVALID_TRANSITION); resolved_at
// is stamped because ATTEMPT_FAILED is terminal.
func (s *SceneStore) FailAttemptTx(ctx context.Context, tx pgx.Tx, id string, reason PublishFailureReason) error {
	ctx, span := startSpan(ctx, "scene.store.fail_attempt_tx",
		attribute.String("published_scene_id", id),
		attribute.String("failure_reason", string(reason)))
	defer span.End()

	now := pgnanos.From(time.Now())
	tag, err := tx.Exec(ctx, `
		UPDATE published_scenes
		SET status = 'ATTEMPT_FAILED',
		    failure_reason = $2,
		    resolved_at = $3
		WHERE id = $1 AND status = 'COOLOFF'
	`, id, string(reason), now)
	if err != nil {
		return oops.Code("SCENE_PUBLISH_TRANSITION_FAILED").Wrap(err)
	}
	if tag.RowsAffected() == 0 {
		return oops.Code("SCENE_PUBLISH_INVALID_TRANSITION").
			With("id", id).With("reason", string(reason)).
			Errorf("row missing or no longer in COOLOFF for fail transition")
	}
	return nil
}

// LockForSnapshot SELECTs the attempt row FOR UPDATE within the caller's
// transaction and re-validates that the status is COOLOFF. The row lock is
// held until the caller's transaction commits/rolls back, serializing the
// snapshot pipeline against concurrent transitions (spec §11.3).
func (s *SceneStore) LockForSnapshot(ctx context.Context, tx pgx.Tx, id string) (*PublishedScene, error) {
	pub, err := scanPublishedSceneHeader(tx.QueryRow(ctx,
		`SELECT `+publishedSceneHeaderColumns+` FROM published_scenes WHERE id = $1 FOR UPDATE`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.Code("SCENE_PUBLISH_NOT_FOUND").With("id", id).Wrap(err)
		}
		return nil, oops.Code("SCENE_PUBLISH_LOCK_FAILED").Wrap(err)
	}
	if pub.Status != StatusCoolOff {
		return nil, oops.Code("SCENE_PUBLISH_INVALID_STATE").
			With("id", id).With("status", string(pub.Status)).
			Errorf("snapshot requires COOLOFF status")
	}
	return pub, nil
}

// AttemptCounts is the per-scene attempt breakdown used by StartScenePublish
// preconditions (attempt budget + one-and-done checks).
type AttemptCounts struct {
	Total     int
	Active    int // status IN (COLLECTING, COOLOFF)
	Published int // status == PUBLISHED
}

// CountAttempts returns total / active / published attempt counts for a scene
// in a single query.
func (s *SceneStore) CountAttempts(ctx context.Context, sceneID string) (AttemptCounts, error) {
	var c AttemptCounts
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) AS total,
		       count(*) FILTER (WHERE status IN ('COLLECTING','COOLOFF')) AS active,
		       count(*) FILTER (WHERE status = 'PUBLISHED') AS published
		FROM published_scenes WHERE scene_id = $1
	`, sceneID).Scan(&c.Total, &c.Active, &c.Published); err != nil {
		return c, oops.Code("SCENE_PUBLISH_COUNT_FAILED").Wrap(err)
	}
	return c, nil
}

// GetSceneMaxPublishAttempts returns scenes.max_publish_attempts for a scene.
func (s *SceneStore) GetSceneMaxPublishAttempts(ctx context.Context, sceneID string) (int, error) {
	var maxAttempts int
	if err := s.pool.QueryRow(
		ctx,
		`SELECT max_publish_attempts FROM scenes WHERE id = $1`, sceneID,
	).Scan(&maxAttempts); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, oops.Code("SCENE_PUBLISH_NOT_FOUND").With("scene_id", sceneID).Wrap(err)
		}
		return 0, oops.Code("SCENE_PUBLISH_MAX_ATTEMPTS_READ_FAILED").Wrap(err)
	}
	return maxAttempts, nil
}

// ExtendMaxPublishAttempts atomically bumps a scene's max_publish_attempts by
// `additional` and returns the new budget. Backs ExtendScenePublishVoteAttempts
// (E1, admin-only via ABAC). A missing scene returns SCENE_PUBLISH_NOT_FOUND.
func (s *SceneStore) ExtendMaxPublishAttempts(ctx context.Context, sceneID string, additional int) (int, error) {
	ctx, span := startSpan(ctx, "scene.store.extend_max_publish_attempts",
		attribute.String("scene_id", sceneID), attribute.Int("additional", additional))
	defer span.End()

	var newMax int
	if err := s.pool.QueryRow(
		ctx,
		`UPDATE scenes SET max_publish_attempts = max_publish_attempts + $2 WHERE id = $1 RETURNING max_publish_attempts`,
		sceneID, additional,
	).Scan(&newMax); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, oops.Code("SCENE_PUBLISH_NOT_FOUND").With("scene_id", sceneID).Wrap(err)
		}
		return 0, oops.Code("SCENE_PUBLISH_EXTEND_FAILED").Wrap(err)
	}
	return newMax, nil
}

// LogRow is a minimal scene_log row for the publication snapshot pipeline
// (C7). It carries the event type (to discriminate pose/say/emit), the acting
// character (actor_id — the speaker), and the payload + codec/DEK fields the
// pipeline needs to decrypt the IC content. ID is the ULID bytes, used for
// chronological ordering.
//
// Timestamp and ActorKind are carried because the host read-back decrypt
// primitive rebuilds the per-event AAD from the row's authoritative fields
// (id/subject/type/timestamp/actor.kind/actor.id/codec/dek_ref/dek_version) via
// AuditRowToEvent + aad.Build — the INV-STORE-5 byte-equal round-trip. A LogRow
// missing these fields would reconstruct a non-matching AAD and fail AEAD
// tag-check on every sensitive row (read-back design INV-CRYPTO-29). Timestamp is the
// BIGINT epoch-ns scene_log column (INV-STORE-1).
type LogRow struct {
	ID         []byte
	Type       string
	Timestamp  pgnanos.Time
	ActorKind  string
	ActorID    []byte
	Payload    []byte
	SchemaVer  int16
	Codec      string
	DEKRef     *int64 // nil for identity-codec rows
	DEKVersion *int32 // nil for identity-codec rows
}

// ReadSceneLogForSnapshot reads a scene's IC content rows from scene_log for
// the publication snapshot pipeline (C7), filtered to the three publishable
// content kinds (scene_pose / scene_say / scene_emit) and ordered by ULID id
// (chronological). OOC, ops, and system events are excluded by the type filter
// (spec §11.1, §12). This is a DIRECT in-transaction DB read — NOT the gRPC
// QueryHistory path. The caller passes the full IC subject
// (events.<game_id>.scene.<scene_id>.ic); the store has no game_id of its own.
func (s *SceneStore) ReadSceneLogForSnapshot(ctx context.Context, tx pgx.Tx, subject string) ([]LogRow, error) {
	ctx, span := startSpan(ctx, "scene.store.read_scene_log_for_snapshot",
		attribute.String("subject", subject))
	defer span.End()

	rows, err := tx.Query(ctx, `
		SELECT id, type, timestamp, actor_kind, actor_id, payload, schema_ver, codec, dek_ref, dek_version
		FROM scene_log
		WHERE subject = $1 AND type IN ('core-scenes:scene_pose', 'core-scenes:scene_say', 'core-scenes:scene_emit')
		ORDER BY id ASC
	`, subject)
	if err != nil {
		return nil, oops.Code("SCENE_PUBLISH_LOG_READ_FAILED").Wrap(err)
	}
	defer rows.Close()

	var out []LogRow
	for rows.Next() {
		var r LogRow
		if err := rows.Scan(
			&r.ID, &r.Type, &r.Timestamp, &r.ActorKind, &r.ActorID, &r.Payload, &r.SchemaVer, &r.Codec, &r.DEKRef, &r.DEKVersion,
		); err != nil {
			return nil, oops.Code("SCENE_PUBLISH_LOG_SCAN_FAILED").Wrap(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("SCENE_PUBLISH_LOG_ITER_FAILED").Wrap(err)
	}
	return out, nil
}

// ListSceneAttempts returns all publish attempts for a scene, header only (no
// content_entries — same column set as GetPublishedSceneHeader), ordered by
// attempt_number. Backs the participant-gated ListScenePublishAttempts audit
// list (B7).
func (s *SceneStore) ListSceneAttempts(ctx context.Context, sceneID string) ([]PublishedScene, error) {
	ctx, span := startSpan(ctx, "scene.store.list_scene_attempts",
		attribute.String("scene_id", sceneID))
	defer span.End()

	rows, err := s.pool.Query(ctx,
		`SELECT `+publishedSceneHeaderColumns+` FROM published_scenes WHERE scene_id = $1 ORDER BY attempt_number ASC`,
		sceneID)
	if err != nil {
		return nil, oops.Code("SCENE_PUBLISH_LIST_ATTEMPTS_FAILED").Wrap(err)
	}
	defer rows.Close()

	var out []PublishedScene
	for rows.Next() {
		pub, scanErr := scanPublishedSceneHeader(rows)
		if scanErr != nil {
			return nil, oops.Code("SCENE_PUBLISH_LIST_ATTEMPTS_SCAN_FAILED").Wrap(scanErr)
		}
		out = append(out, *pub)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("SCENE_PUBLISH_LIST_ATTEMPTS_ITER_FAILED").Wrap(err)
	}
	return out, nil
}

// ListPublishedScenesQuery parameterises the archive-browse listing.
type ListPublishedScenesQuery struct {
	// Limit is the page size. 0 means server-default (50), capped at 200.
	Limit int
	// Offset is the zero-based row skip. Negative values are clamped to 0.
	Offset int
	// Tags, when non-empty, restricts results to scenes carrying all listed
	// tags (array containment: scenes.tags @> $tags).
	Tags []string
}

// PublishedSceneArchiveSummary is the persistence-layer projection for
// ListPublishedScenes — the public-safe archive fields plus the source
// scene's tags for client-side filtering.
type PublishedSceneArchiveSummary struct {
	ID                   string
	TitleSnapshot        string
	ParticipantsSnapshot []string
	ContentEntries       []PublishedSceneEntry
	PublishedAtNS        *pgnanos.Time
	Tags                 []string // from scenes.tags at list time
}

const (
	defaultPublishedLimit = 50
	maxPublishedLimit     = 200
)

// ListPublishedScenes returns PUBLISHED scene archive summaries in
// published_at DESC order. Only PUBLISHED rows are returned (same status
// gate as GetPublicSceneArchive / INV-SCENE-35). Tags is applied as an
// array-containment predicate on scenes.tags. Supports LIMIT/OFFSET paging.
func (s *SceneStore) ListPublishedScenes(ctx context.Context, q ListPublishedScenesQuery) ([]PublishedSceneArchiveSummary, error) {
	ctx, span := startSpan(ctx, "scene.store.list_published_scenes")
	defer span.End()

	if q.Limit <= 0 {
		q.Limit = defaultPublishedLimit
	} else if q.Limit > maxPublishedLimit {
		q.Limit = maxPublishedLimit
	}
	if q.Offset < 0 {
		q.Offset = 0
	}

	var tagsArg interface{}
	if len(q.Tags) > 0 {
		tagsArg = q.Tags
	}

	const query = `
		SELECT ps.id, ps.title_snapshot, ps.participants_snapshot,
		       ps.content_entries, ps.published_at, s.tags
		FROM published_scenes ps
		JOIN scenes s ON s.id = ps.scene_id
		WHERE ps.status = 'PUBLISHED'
		  AND ($1::text[] IS NULL OR s.tags @> $1)
		ORDER BY ps.published_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := s.pool.Query(ctx, query, tagsArg, q.Limit, q.Offset)
	if err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_PUBLISH_LIST_PUBLISHED_FAILED").Wrap(err)
	}
	defer rows.Close()

	var out []PublishedSceneArchiveSummary
	for rows.Next() {
		var item PublishedSceneArchiveSummary
		var titleSnapshot *string
		var participantsRaw []byte
		var contentRaw []byte
		if err := rows.Scan(
			&item.ID, &titleSnapshot, &participantsRaw,
			&contentRaw, &item.PublishedAtNS, &item.Tags,
		); err != nil {
			recordError(span, err)
			return nil, oops.Code("SCENE_PUBLISH_LIST_PUBLISHED_SCAN_FAILED").Wrap(err)
		}
		if titleSnapshot != nil {
			item.TitleSnapshot = *titleSnapshot
		}
		if len(participantsRaw) > 0 {
			if err := json.Unmarshal(participantsRaw, &item.ParticipantsSnapshot); err != nil {
				return nil, oops.Code("SCENE_PUBLISH_LIST_PUBLISHED_PARTICIPANTS_DECODE_FAILED").Wrap(err)
			}
		}
		if len(contentRaw) > 0 {
			if err := json.Unmarshal(contentRaw, &item.ContentEntries); err != nil {
				return nil, oops.Code("SCENE_PUBLISH_LIST_PUBLISHED_CONTENT_DECODE_FAILED").Wrap(err)
			}
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		recordError(span, err)
		return nil, oops.Code("SCENE_PUBLISH_LIST_PUBLISHED_ITER_FAILED").Wrap(err)
	}
	return out, nil
}

// SnapshotPool exposes the underlying connection pool for the C7 snapshot
// pipeline, which orchestrates a read-tx → decrypt(outside tx) → write-tx
// sequence (read-back design §6). It is the same pool as Pool(); the distinct
// name documents the snapshot-tx-orchestration use on the sceneStorer interface.
func (s *SceneStore) SnapshotPool() *pgxpool.Pool {
	return s.pool
}

// MarkPublishedInput carries the frozen-at-publish-time snapshot fields the
// COOLOFF→PUBLISHED transition writes onto the published_scenes row (spec §4.1,
// §11). All four are immutable once written.
type MarkPublishedInput struct {
	ContentEntries       []PublishedSceneEntry
	TitleSnapshot        string
	ParticipantsSnapshot []string
	PublishedAt          time.Time
}

// MarkPublished applies the COOLOFF→PUBLISHED state write inside the caller's
// transaction: status → PUBLISHED, content_entries / title_snapshot /
// participants_snapshot / published_at / resolved_at set in one UPDATE. Legality
// is enforced by a precondition WHERE clause (status = 'COOLOFF') so a row that
// flipped out of COOLOFF between the lock and this write is NOT clobbered — zero
// rows updated → SCENE_PUBLISH_INVALID_TRANSITION (the snapshot caller treats
// this as the vote-flip no-op / COOLOFF_INVARIANT_BROKEN path). The lock from
// LockForSnapshot is held for the whole tx, so in practice the precondition is a
// belt-and-suspenders guard alongside the in-tx re-validation.
func (s *SceneStore) MarkPublished(ctx context.Context, tx pgx.Tx, id string, in MarkPublishedInput) error {
	ctx, span := startSpan(ctx, "scene.store.mark_published",
		attribute.String("published_scene_id", id))
	defer span.End()

	entriesJSON, err := json.Marshal(in.ContentEntries)
	if err != nil {
		return oops.Code("SCENE_PUBLISH_CONTENT_ENCODE_FAILED").Wrap(err)
	}
	partsJSON, err := json.Marshal(in.ParticipantsSnapshot)
	if err != nil {
		return oops.Code("SCENE_PUBLISH_PARTICIPANTS_ENCODE_FAILED").Wrap(err)
	}

	publishedAt := pgnanos.From(in.PublishedAt)
	tag, err := tx.Exec(ctx, `
		UPDATE published_scenes
		SET status                = 'PUBLISHED',
		    content_entries       = $2,
		    title_snapshot        = $3,
		    participants_snapshot = $4,
		    published_at          = $5,
		    resolved_at           = $5
		WHERE id = $1 AND status = 'COOLOFF'
	`, id, entriesJSON, in.TitleSnapshot, partsJSON, publishedAt)
	if err != nil {
		return oops.Code("SCENE_PUBLISH_MARK_PUBLISHED_FAILED").Wrap(err)
	}
	if tag.RowsAffected() == 0 {
		return oops.Code("SCENE_PUBLISH_INVALID_TRANSITION").
			With("id", id).
			Errorf("published_scenes row missing or no longer in COOLOFF at publish write")
	}
	return nil
}

// ListExpiredCollecting returns COLLECTING attempts whose vote window has
// elapsed: initiated_at + vote_window (in nanoseconds) ≤ nowNs.
//
// nowNs is supplied by the Go-clock caller as epoch nanoseconds
// rather than using SQL now() so the comparison is always against the same Go
// clock used to write initiated_at (INV-STORE-1, noremoteclockcompare gorule).
//
// vote_window is an INTERVAL stored as microseconds; it is converted to
// nanoseconds inline: EXTRACT(EPOCH FROM vote_window)::BIGINT * 1000000000.
func (s *SceneStore) ListExpiredCollecting(ctx context.Context, nowNs int64) ([]scheduledAttempt, error) {
	ctx, span := startSpan(ctx, "scene.store.list_expired_collecting")
	defer span.End()

	rows, err := s.pool.Query(ctx, `
		SELECT id, scene_id
		FROM published_scenes
		WHERE status = 'COLLECTING'
		  AND initiated_at + EXTRACT(EPOCH FROM vote_window)::BIGINT * 1000000000 <= $1
	`, nowNs)
	if err != nil {
		return nil, oops.Code("SCENE_SCHEDULER_LIST_EXPIRED_COLLECTING_FAILED").Wrap(err)
	}
	defer rows.Close()

	var out []scheduledAttempt
	for rows.Next() {
		var a scheduledAttempt
		if err := rows.Scan(&a.ID, &a.SceneID); err != nil {
			return nil, oops.Code("SCENE_SCHEDULER_LIST_EXPIRED_COLLECTING_SCAN_FAILED").Wrap(err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("SCENE_SCHEDULER_LIST_EXPIRED_COLLECTING_ITER_FAILED").Wrap(err)
	}
	return out, nil
}

// ListExpiredCoolOff returns COOLOFF attempts whose cool-off window has
// elapsed: cooloff_started_at + cooloff_window (in nanoseconds) ≤ nowNs.
//
// cooloff_started_at is always set for COOLOFF rows (TransitionStatus sets it
// when entering COOLOFF); the IS NOT NULL guard is belt-and-suspenders. See
// ListExpiredCollecting for the clock-domain and unit-conversion rationale.
func (s *SceneStore) ListExpiredCoolOff(ctx context.Context, nowNs int64) ([]scheduledAttempt, error) {
	ctx, span := startSpan(ctx, "scene.store.list_expired_cooloff")
	defer span.End()

	rows, err := s.pool.Query(ctx, `
		SELECT id, scene_id
		FROM published_scenes
		WHERE status = 'COOLOFF'
		  AND cooloff_started_at IS NOT NULL
		  AND cooloff_started_at + EXTRACT(EPOCH FROM cooloff_window)::BIGINT * 1000000000 <= $1
	`, nowNs)
	if err != nil {
		return nil, oops.Code("SCENE_SCHEDULER_LIST_EXPIRED_COOLOFF_FAILED").Wrap(err)
	}
	defer rows.Close()

	var out []scheduledAttempt
	for rows.Next() {
		var a scheduledAttempt
		if err := rows.Scan(&a.ID, &a.SceneID); err != nil {
			return nil, oops.Code("SCENE_SCHEDULER_LIST_EXPIRED_COOLOFF_SCAN_FAILED").Wrap(err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("SCENE_SCHEDULER_LIST_EXPIRED_COOLOFF_ITER_FAILED").Wrap(err)
	}
	return out, nil
}

// ArchiveSceneStateForPublish sets scenes.state = 'archived' for the published
// scene inside the caller's transaction and reports whether a row was affected.
//
// FK soft-no-op (read-back design / ADR holomush-jrefa): published_scenes has NO
// foreign key to scenes(id), so the scene row may have been deleted between
// COOLOFF entry and snapshot fire. A 0-row UPDATE is therefore NOT an error —
// the archive intentionally outlives its source. The caller logs a warning and
// STILL finalizes the publication to PUBLISHED. ok=false signals the soft no-op.
func (s *SceneStore) ArchiveSceneStateForPublish(ctx context.Context, tx pgx.Tx, sceneID string) (bool, error) {
	ctx, span := startSpan(ctx, "scene.store.archive_scene_state_for_publish",
		attribute.String("scene_id", sceneID))
	defer span.End()

	tag, err := tx.Exec(ctx,
		`UPDATE scenes SET state = $2 WHERE id = $1`,
		sceneID, string(SceneStateArchived))
	if err != nil {
		return false, oops.Code("SCENE_PUBLISH_ARCHIVE_FAILED").Wrap(err)
	}
	return tag.RowsAffected() > 0, nil
}

// SnapshotSceneMeta carries the scene metadata frozen onto the publication at
// PUBLISHED time: the scene title and the owner+member participant character
// IDs (invited excluded — INV-SCENE-28). Read inside the snapshot's write-tx for a
// consistent snapshot. Name resolution is a follow-up; character IDs are the
// available identity surface (see commands.go handleLog speaker note).
type SnapshotSceneMeta struct {
	Title        string
	Participants []string
}

// ReadSceneMetaForSnapshot reads the scene title and the owner+member
// participant character IDs inside the caller's transaction. A missing scene
// (deleted mid-publication) returns (zero-meta, nil): the publication still
// finalizes; the FK soft-no-op in ArchiveSceneStateForPublish handles the
// scene-state side. Participant order is stable (joined_at, character_id).
func (s *SceneStore) ReadSceneMetaForSnapshot(ctx context.Context, tx pgx.Tx, sceneID string) (SnapshotSceneMeta, error) {
	ctx, span := startSpan(ctx, "scene.store.read_scene_meta_for_snapshot",
		attribute.String("scene_id", sceneID))
	defer span.End()

	var meta SnapshotSceneMeta
	err := tx.QueryRow(ctx, `SELECT title FROM scenes WHERE id = $1`, sceneID).Scan(&meta.Title)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Scene deleted mid-publication: title is empty, participants nil.
			// The publication still finalizes (ADR holomush-jrefa).
			return SnapshotSceneMeta{}, nil
		}
		return SnapshotSceneMeta{}, oops.Code("SCENE_PUBLISH_META_READ_FAILED").Wrap(err)
	}

	rows, err := tx.Query(ctx, `
		SELECT character_id FROM scene_participants
		WHERE scene_id = $1 AND role IN ('owner', 'member')
		ORDER BY joined_at ASC, character_id ASC
	`, sceneID)
	if err != nil {
		return SnapshotSceneMeta{}, oops.Code("SCENE_PUBLISH_META_PARTICIPANTS_FAILED").Wrap(err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid string
		if err := rows.Scan(&cid); err != nil {
			return SnapshotSceneMeta{}, oops.Code("SCENE_PUBLISH_META_PARTICIPANTS_SCAN_FAILED").Wrap(err)
		}
		meta.Participants = append(meta.Participants, cid)
	}
	if err := rows.Err(); err != nil {
		return SnapshotSceneMeta{}, oops.Code("SCENE_PUBLISH_META_PARTICIPANTS_ITER_FAILED").Wrap(err)
	}
	return meta, nil
}
