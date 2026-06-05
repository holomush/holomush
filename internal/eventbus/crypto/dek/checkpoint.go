// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/pgnanos"
)

// RequestID is owned by holomush-jxo8.7.18 (rekey.go). The dek package's
// RequestID type is defined there; CheckpointRepo consumes it via the
// shared package scope.

// CheckpointOpenRequest contains the arguments to open a new rekey
// checkpoint row. All fields are mandatory. Byte-slice fields are
// intentionally unexported (INV-CRYPTO-16); use NewCheckpointOpenRequest to
// construct a value.
type CheckpointOpenRequest struct {
	ContextType     string
	ContextID       string
	opArgsHash      []byte // exactly 32 bytes
	policyHash      []byte // exactly 32 bytes
	PrimaryPlayerID string
	OldDEKID        int64
	Justification   string
}

// NewCheckpointOpenRequest constructs a CheckpointOpenRequest. Both
// opArgsHash and policyHash must be exactly 32 bytes. Justification is the
// operator-supplied free-text reason recorded on the checkpoint row at
// Phase 1 (holomush-jxo8.7.55) so RunByRequestID can rehydrate it into the
// Phase 7 audit payload on the explicit-resume path.
func NewCheckpointOpenRequest(
	contextType, contextID string,
	opArgsHash, policyHash []byte,
	primaryPlayerID string,
	oldDEKID int64,
	justification string,
) (CheckpointOpenRequest, error) {
	if len(opArgsHash) != 32 {
		return CheckpointOpenRequest{}, oops.Code("DEK_REKEY_BAD_OP_ARGS_HASH").
			Errorf("op_args_hash must be 32 bytes, got %d", len(opArgsHash))
	}
	if len(policyHash) != 32 {
		return CheckpointOpenRequest{}, oops.Code("DEK_REKEY_BAD_POLICY_HASH").
			Errorf("policy_hash must be 32 bytes, got %d", len(policyHash))
	}
	return CheckpointOpenRequest{
		ContextType:     contextType,
		ContextID:       contextID,
		opArgsHash:      opArgsHash,
		policyHash:      policyHash,
		PrimaryPlayerID: primaryPlayerID,
		OldDEKID:        oldDEKID,
		Justification:   justification,
	}, nil
}

// Checkpoint mirrors a crypto_rekey_checkpoints row.
// Byte-slice fields are intentionally unexported (INV-CRYPTO-16): they are
// populated by scanCheckpoint and read only within this package.
type Checkpoint struct {
	RequestID            RequestID
	ContextType          string
	ContextID            string
	opArgsHash           []byte
	policyHash           []byte
	PrimaryPlayerID      string
	Justification        string
	Status               CheckpointStatus
	lastProcessedEventID []byte
	NewDEKID             *int64
	OldDEKID             int64
	Phase3RowsRewritten  int
	Phase5AttemptCount   int
	phase5MissingMembers []byte
	ForceDestroy         bool
	StartedAt            time.Time
	LastHeartbeatAt      time.Time
	CompletedAt          *time.Time
	AbortedAt            *time.Time
	AbortedReason        *string
}

// PolicyHash returns the policy_hash captured at Phase 1 (INV-CRYPTO-112) as a
// [32]byte array. Using a fixed-size array rather than []byte preserves
// INV-CRYPTO-16 (no exported []byte in the dek package). The array is zero-padded
// if the stored slice is shorter than 32 bytes (should not happen in
// production — the CHECK constraint enforces NOT NULL).
func (c *Checkpoint) PolicyHash() [32]byte {
	var out [32]byte
	copy(out[:], c.policyHash)
	return out
}

// LastProcessedEventID returns the Phase 3 batch cursor as a 16-byte
// ULID array (the cursor is always a stored events_audit.id which is a
// 16-byte ULID). The bool result distinguishes "cursor present"
// (true → batches committed) from "cursor unset" (false → initial scan,
// no batches yet). Using a fixed-size array rather than []byte preserves
// INV-CRYPTO-16 (no exported []byte in the dek package); the bool replaces the
// nil-slice nullability signal.
func (c *Checkpoint) LastProcessedEventID() ([16]byte, bool) {
	var out [16]byte
	if len(c.lastProcessedEventID) == 0 {
		return out, false
	}
	copy(out[:], c.lastProcessedEventID)
	return out, true
}

// Phase5MissingMembers returns the persisted list of cluster members that
// did not acknowledge the most recent Phase 5 invalidation fan-out. Returns
// nil when the column is NULL (initial state, or after a successful Phase 5
// run that cleared the column). A non-nil empty slice is also possible if
// the column was explicitly written as an empty JSON array.
//
// INV-CRYPTO-16 compliance: the underlying column is JSONB; this accessor decodes
// it on every call rather than exposing the raw []byte. Callers that need
// a stable view should snapshot the return value.
//
// The second result is non-nil only when the column held malformed JSON,
// which should not happen in production — the orchestrator always writes
// well-formed []string. Tests that build fixtures directly via SQL MUST
// pass valid JSON.
func (c *Checkpoint) Phase5MissingMembers() ([]string, error) {
	if len(c.phase5MissingMembers) == 0 {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal(c.phase5MissingMembers, &out); err != nil {
		return nil, oops.Code("DEK_REKEY_PHASE5_MISSING_MEMBERS_DECODE_FAILED").Wrap(err)
	}
	return out, nil
}

// Phase5HasMissingMembers reports whether phase5_missing_members is populated
// (a non-empty JSON value). Useful for the Phase 5 timeout discriminator
// without paying the JSON-decode cost. INV-CRYPTO-97's force-destroy gate consumes
// this: force-destroy MUST be rejected unless the checkpoint sits in the
// (status=phase5_invalidate, missing_members!=NULL) state.
func (c *Checkpoint) Phase5HasMissingMembers() bool {
	// A literal SQL NULL gives len()==0. A JSON "null" literal gives the
	// 4 bytes "null"; treat that as NULL too. An empty JSON array "[]"
	// (2 bytes) means "we wrote a result and zero members were missing"
	// — that path can't happen in production (RecordPhase5Success clears
	// the column to NULL on the zero-missing path) but we defensively
	// treat it as "no missing".
	if len(c.phase5MissingMembers) == 0 {
		return false
	}
	trimmed := bytes.TrimSpace(c.phase5MissingMembers)
	if bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte("[]")) {
		return false
	}
	return true
}

// CheckpointRepo is the SQL persistence layer for crypto_rekey_checkpoints.
type CheckpointRepo struct {
	pool *pgxpool.Pool
}

// NewCheckpointRepo wraps a pgxpool.Pool.
func NewCheckpointRepo(pool *pgxpool.Pool) *CheckpointRepo { return &CheckpointRepo{pool: pool} }

// Open inserts a new checkpoint row with status=pending and returns the
// generated RequestID. Returns DEK_REKEY_ALREADY_IN_PROGRESS if a non-
// terminal checkpoint already exists for the same (context_type, context_id)
// pair (INV-CRYPTO-92, enforced by the partial unique index).
// Construct req with NewCheckpointOpenRequest to satisfy hash-length
// invariants before calling Open.
func (r *CheckpointRepo) Open(ctx context.Context, req CheckpointOpenRequest) (RequestID, error) {
	if len(req.opArgsHash) != 32 {
		return RequestID{}, oops.Code("DEK_REKEY_BAD_OP_ARGS_HASH").
			Errorf("op_args_hash must be 32 bytes, got %d", len(req.opArgsHash))
	}
	if len(req.policyHash) != 32 {
		return RequestID{}, oops.Code("DEK_REKEY_BAD_POLICY_HASH").
			Errorf("policy_hash must be 32 bytes, got %d", len(req.policyHash))
	}
	rid := RequestID(idgen.New())
	_, err := r.pool.Exec(ctx, `
        INSERT INTO crypto_rekey_checkpoints
          (request_id, context_type, context_id, op_args_hash, policy_hash,
           primary_player_id, status, old_dek_id, justification)
        VALUES ($1, $2, $3, $4, $5, $6, 'pending', $7, $8)
    `, rid[:], req.ContextType, req.ContextID, req.opArgsHash, req.policyHash,
		req.PrimaryPlayerID, req.OldDEKID, req.Justification)
	if err != nil {
		if isUniqueViolation(err, "crypto_rekey_checkpoints_one_active_per_context") {
			return RequestID{}, oops.Code("DEK_REKEY_ALREADY_IN_PROGRESS").
				With("context_type", req.ContextType).
				With("context_id", req.ContextID).
				Wrap(err)
		}
		return RequestID{}, oops.Code("DEK_REKEY_CHECKPOINT_INSERT_FAILED").Wrap(err)
	}
	return rid, nil
}

// Get returns the checkpoint row for rid, or DEK_REKEY_CHECKPOINT_NOT_FOUND
// if no row exists for that request_id.
func (r *CheckpointRepo) Get(ctx context.Context, rid RequestID) (Checkpoint, error) {
	row := r.pool.QueryRow(ctx, `
        SELECT request_id, context_type, context_id, op_args_hash, policy_hash,
               primary_player_id, justification, status, last_processed_event_id, new_dek_id,
               old_dek_id, phase3_rows_rewritten, phase5_attempt_count, phase5_missing_members,
               force_destroy, started_at, last_heartbeat_at, completed_at,
               aborted_at, aborted_reason
          FROM crypto_rekey_checkpoints
         WHERE request_id = $1
    `, rid[:])
	c, err := scanCheckpoint(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Checkpoint{}, oops.Code("DEK_REKEY_CHECKPOINT_NOT_FOUND").
				With("request_id", rid.String()).
				Errorf("rekey checkpoint not found")
		}
		return Checkpoint{}, err
	}
	return c, nil
}

// UpdateStatus transitions status from → to using a CAS UPDATE that
// atomically guards against stale writers. Returns DEK_REKEY_STALE_TRANSITION
// if the row was not in state `from` at commit time (INV-CRYPTO-88).
// AssertTransitionAllowed gates the FSM semantics before issuing SQL.
func (r *CheckpointRepo) UpdateStatus(ctx context.Context, rid RequestID, from, to CheckpointStatus) error {
	if err := AssertTransitionAllowed(from, to); err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `
        UPDATE crypto_rekey_checkpoints
           SET status = $2, last_heartbeat_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT
         WHERE request_id = $1 AND status = $3
    `, rid[:], to, from)
	if err != nil {
		return oops.Code("DEK_REKEY_UPDATE_STATUS_FAILED").Wrap(err)
	}
	if tag.RowsAffected() != 1 {
		return oops.Code("DEK_REKEY_STALE_TRANSITION").
			With("request_id", rid.String()).
			With("expected_from", from).
			With("to", to).
			Errorf("CAS predicate failed (row not in expected state)")
	}
	return nil
}

// Heartbeat bumps last_heartbeat_at to now(), resetting the sweep-TTL
// expiry clock.
func (r *CheckpointRepo) Heartbeat(ctx context.Context, rid RequestID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE crypto_rekey_checkpoints SET last_heartbeat_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT WHERE request_id = $1`,
		rid[:])
	if err != nil {
		return oops.Code("DEK_REKEY_HEARTBEAT_FAILED").Wrap(err)
	}
	return nil
}

// FindByContextAndArgs returns the non-terminal checkpoint for
// (ctxType, ctxID) that matches opArgsHash, or (_, false, nil) if none
// exists. Used by the orchestrator resume path.
func (r *CheckpointRepo) FindByContextAndArgs(ctx context.Context, ctxType, ctxID string, opArgsHash []byte) (Checkpoint, bool, error) {
	row := r.pool.QueryRow(ctx, `
        SELECT request_id, context_type, context_id, op_args_hash, policy_hash,
               primary_player_id, justification, status, last_processed_event_id, new_dek_id,
               old_dek_id, phase3_rows_rewritten, phase5_attempt_count, phase5_missing_members,
               force_destroy, started_at, last_heartbeat_at, completed_at,
               aborted_at, aborted_reason
          FROM crypto_rekey_checkpoints
         WHERE context_type = $1 AND context_id = $2 AND op_args_hash = $3
           AND status NOT IN ('complete', 'aborted')
         LIMIT 1
    `, ctxType, ctxID, opArgsHash)
	ckpt, err := scanCheckpoint(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Checkpoint{}, false, nil
		}
		return Checkpoint{}, false, err
	}
	return ckpt, true, nil
}

// FindNonTerminalByContext returns the non-terminal checkpoint for (ctxType,
// ctxID), if any. Used by the sweep subsystem and orchestrator to detect
// in-flight rekeys.
func (r *CheckpointRepo) FindNonTerminalByContext(ctx context.Context, ctxType, ctxID string) (Checkpoint, bool, error) {
	row := r.pool.QueryRow(ctx, `
        SELECT request_id, context_type, context_id, op_args_hash, policy_hash,
               primary_player_id, justification, status, last_processed_event_id, new_dek_id,
               old_dek_id, phase3_rows_rewritten, phase5_attempt_count, phase5_missing_members,
               force_destroy, started_at, last_heartbeat_at, completed_at,
               aborted_at, aborted_reason
          FROM crypto_rekey_checkpoints
         WHERE context_type = $1 AND context_id = $2
           AND status NOT IN ('complete', 'aborted')
         LIMIT 1
    `, ctxType, ctxID)
	ckpt, err := scanCheckpoint(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Checkpoint{}, false, nil
		}
		return Checkpoint{}, false, err
	}
	return ckpt, true, nil
}

// ListExpired returns all non-terminal checkpoints whose
// last_heartbeat_at is older than ttl ago. Called by the sweep subsystem
// (INV-CRYPTO-105).
func (r *CheckpointRepo) ListExpired(ctx context.Context, ttl time.Duration) ([]Checkpoint, error) {
	// Bind ttl.Nanoseconds() directly against the BIGINT-ns last_heartbeat_at
	// column. The prior int64(ttl.Seconds()) truncated sub-second durations
	// (e.g. 500ms → 0), which made every in-flight checkpoint look expired
	// immediately. After the gfo6 BIGINT-ns migration, ns-precision is the
	// canonical resolution for time arithmetic in this table.
	rows, err := r.pool.Query(ctx, `
        SELECT request_id, context_type, context_id, op_args_hash, policy_hash,
               primary_player_id, justification, status, last_processed_event_id, new_dek_id,
               old_dek_id, phase3_rows_rewritten, phase5_attempt_count, phase5_missing_members,
               force_destroy, started_at, last_heartbeat_at, completed_at,
               aborted_at, aborted_reason
          FROM crypto_rekey_checkpoints
         WHERE status NOT IN ('complete', 'aborted')
           AND last_heartbeat_at < (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT - $1::BIGINT
    `, ttl.Nanoseconds())
	if err != nil {
		return nil, oops.Code("DEK_REKEY_LIST_EXPIRED_FAILED").Wrap(err)
	}
	defer rows.Close()
	var out []Checkpoint
	for rows.Next() {
		ckpt, err := scanCheckpoint(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ckpt)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("DEK_REKEY_LIST_EXPIRED_ROWS_ERR").Wrap(err)
	}
	return out, nil
}

// MarkAborted transitions the checkpoint to aborted, recording the
// abort reason. Returns DEK_REKEY_CHECKPOINT_TERMINAL if the row is
// already in a terminal state.
func (r *CheckpointRepo) MarkAborted(ctx context.Context, rid RequestID, reason string) error {
	tag, err := r.pool.Exec(ctx, `
        UPDATE crypto_rekey_checkpoints
           SET status = 'aborted', aborted_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT, aborted_reason = $2
         WHERE request_id = $1 AND status NOT IN ('complete', 'aborted')
    `, rid[:], reason)
	if err != nil {
		return oops.Code("DEK_REKEY_MARK_ABORTED_FAILED").Wrap(err)
	}
	if tag.RowsAffected() != 1 {
		return oops.Code("DEK_REKEY_CHECKPOINT_TERMINAL").
			With("request_id", rid.String()).
			Errorf("checkpoint not in abortable state")
	}
	return nil
}

// MarkComplete transitions the checkpoint from phase7_audit to complete,
// recording completed_at. Returns DEK_REKEY_STALE_TRANSITION if the row
// is not at phase7_audit.
func (r *CheckpointRepo) MarkComplete(ctx context.Context, rid RequestID) error {
	tag, err := r.pool.Exec(ctx, `
        UPDATE crypto_rekey_checkpoints
           SET status = 'complete', completed_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT
         WHERE request_id = $1 AND status = 'phase7_audit'
    `, rid[:])
	if err != nil {
		return oops.Code("DEK_REKEY_MARK_COMPLETE_FAILED").Wrap(err)
	}
	if tag.RowsAffected() != 1 {
		return oops.Code("DEK_REKEY_STALE_TRANSITION").
			With("request_id", rid.String()).
			Errorf("phase7_audit predicate failed")
	}
	return nil
}

// SetForceDestroy sets force_destroy=true on the checkpoint row.
// Called by Phase 5 resume path when --force-destroy is passed.
func (r *CheckpointRepo) SetForceDestroy(ctx context.Context, rid RequestID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE crypto_rekey_checkpoints SET force_destroy = true WHERE request_id = $1`,
		rid[:])
	if err != nil {
		return oops.Code("DEK_REKEY_SET_FORCE_DESTROY_FAILED").Wrap(err)
	}
	return nil
}

// UpdateStatusForceDestroy transitions phase5_invalidate → phase6_destroy_old
// when force_destroy=true, bypassing the normal Phase 5 invalidation wait
// (INV-CRYPTO-97). The CAS predicate requires both status='phase5_invalidate' AND
// force_destroy=true.
func (r *CheckpointRepo) UpdateStatusForceDestroy(ctx context.Context, rid RequestID) error {
	if err := AssertTransitionAllowed(CheckpointStatusPhase5Invalidate, CheckpointStatusPhase6DestroyOld); err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `
        UPDATE crypto_rekey_checkpoints
           SET status = 'phase6_destroy_old', last_heartbeat_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT
         WHERE request_id = $1 AND status = 'phase5_invalidate' AND force_destroy = true
    `, rid[:])
	if err != nil {
		return oops.Code("DEK_REKEY_FORCE_DESTROY_UPDATE_FAILED").Wrap(err)
	}
	if tag.RowsAffected() != 1 {
		return oops.Code("DEK_REKEY_STALE_TRANSITION").
			With("request_id", rid.String()).
			Errorf("force_destroy predicate failed")
	}
	return nil
}

// IncrementPhase3Count atomically increments the cumulative Phase 3
// row-rewrite count on the checkpoint row by delta. Called by
// processPhase3Batch INSIDE the batch transaction so the count, the row
// rewrites, and the cursor advance all commit atomically (INV-CRYPTO-94).
// Crash-resume correctness: any successfully-committed batch is reflected
// in the count; a crashed-mid-batch run leaves the count consistent with
// the cursor (both unchanged from before the batch began).
// The CAS predicate requires status='phase3_reencrypt_cold'.
//
// holomush-jxo8.7.54 — crypto-reviewer crash-resume correctness fix.
func (r *CheckpointRepo) IncrementPhase3Count(ctx context.Context, tx pgx.Tx, rid RequestID, delta int) error {
	tag, err := tx.Exec(ctx, `
        UPDATE crypto_rekey_checkpoints
           SET phase3_rows_rewritten = phase3_rows_rewritten + $2,
               last_heartbeat_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT
         WHERE request_id = $1 AND status = 'phase3_reencrypt_cold'
    `, rid[:], delta)
	if err != nil {
		return oops.Code("DEK_REKEY_PHASE3_COUNT_INCREMENT_FAILED").Wrap(err)
	}
	if tag.RowsAffected() != 1 {
		// Same code as AdvanceCursor / UpdateStatus / MarkAborted / etc. —
		// the CAS predicate (status='phase3_reencrypt_cold') is the canonical
		// stale-transition guard and operator-facing dashboards already
		// classify this code class. Don't proliferate per-method codes.
		return oops.Code("DEK_REKEY_STALE_TRANSITION").
			With("request_id", rid.String()).
			With("operation", "IncrementPhase3Count").
			Errorf("expected 1 row affected, got %d (status drift from phase3_reencrypt_cold)", tag.RowsAffected())
	}
	return nil
}

// AdvanceCursor updates last_processed_event_id within the caller's active
// pgx.Tx so that the cursor advance commits atomically with the events_audit
// row UPDATEs (INV-CRYPTO-94). The CAS predicate requires status='phase3_reencrypt_cold'.
func (r *CheckpointRepo) AdvanceCursor(ctx context.Context, tx pgx.Tx, rid RequestID, eventID []byte) error {
	tag, err := tx.Exec(ctx, `
        UPDATE crypto_rekey_checkpoints
           SET last_processed_event_id = $2, last_heartbeat_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT
         WHERE request_id = $1 AND status = 'phase3_reencrypt_cold'
    `, rid[:], eventID)
	if err != nil {
		return oops.Code("DEK_REKEY_ADVANCE_CURSOR_FAILED").Wrap(err)
	}
	if tag.RowsAffected() != 1 {
		return oops.Code("DEK_REKEY_STALE_TRANSITION").
			With("request_id", rid.String()).
			Errorf("cursor advance predicate failed")
	}
	return nil
}

// SetNewDEKAndAdvance updates new_dek_id and transitions status from
// phase1_auth to phase2_mint_dek in a single CAS UPDATE.
func (r *CheckpointRepo) SetNewDEKAndAdvance(ctx context.Context, rid RequestID, newDEKID int64) error {
	tag, err := r.pool.Exec(ctx, `
        UPDATE crypto_rekey_checkpoints
           SET new_dek_id = $2, status = 'phase2_mint_dek', last_heartbeat_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT
         WHERE request_id = $1 AND status = 'phase1_auth'
    `, rid[:], newDEKID)
	if err != nil {
		return oops.Code("DEK_REKEY_PHASE2_UPDATE_FAILED").Wrap(err)
	}
	if tag.RowsAffected() != 1 {
		return oops.Code("DEK_REKEY_STALE_TRANSITION").
			Errorf("Phase 2 CAS predicate failed")
	}
	return nil
}

// IncrementPhase5Attempt increments phase5_attempt_count. Called by Phase 5
// before each invalidation fan-out attempt.
func (r *CheckpointRepo) IncrementPhase5Attempt(ctx context.Context, rid RequestID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE crypto_rekey_checkpoints
            SET phase5_attempt_count = phase5_attempt_count + 1, last_heartbeat_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT
          WHERE request_id = $1 AND status IN ('phase3_reencrypt_cold', 'phase5_invalidate')`,
		rid[:])
	if err != nil {
		return oops.Code("DEK_REKEY_PHASE5_ATTEMPT_INC_FAILED").Wrap(err)
	}
	return nil
}

// RecordPhase5Timeout records that Phase 5 timed out waiting for all replicas
// to acknowledge DEK invalidation, persisting the set of missing members.
func (r *CheckpointRepo) RecordPhase5Timeout(ctx context.Context, rid RequestID, missingJSON []byte) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE crypto_rekey_checkpoints
            SET status = 'phase5_invalidate', phase5_missing_members = $2::jsonb, last_heartbeat_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT
          WHERE request_id = $1 AND status IN ('phase3_reencrypt_cold', 'phase5_invalidate')`,
		rid[:], missingJSON)
	if err != nil {
		return oops.Code("DEK_REKEY_PHASE5_RECORD_TIMEOUT_FAILED").Wrap(err)
	}
	if tag.RowsAffected() != 1 {
		return oops.Code("DEK_REKEY_STALE_TRANSITION").Errorf("Phase 5 timeout predicate failed")
	}
	return nil
}

// RecordPhase5Success marks Phase 5 as complete: clears phase5_missing_members
// and transitions to phase5_invalidate (all replicas acknowledged).
func (r *CheckpointRepo) RecordPhase5Success(ctx context.Context, rid RequestID) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE crypto_rekey_checkpoints
            SET status = 'phase5_invalidate', phase5_missing_members = NULL, last_heartbeat_at = (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT
          WHERE request_id = $1 AND status IN ('phase3_reencrypt_cold', 'phase5_invalidate')`,
		rid[:])
	if err != nil {
		return oops.Code("DEK_REKEY_PHASE5_RECORD_SUCCESS_FAILED").Wrap(err)
	}
	if tag.RowsAffected() != 1 {
		return oops.Code("DEK_REKEY_STALE_TRANSITION").Errorf("Phase 5 success predicate failed")
	}
	return nil
}

// CheckpointListFilter parameterises ListFiltered. Limit defaults to 100
// when zero or negative. ContextPattern, if non-nil, is used as a LIKE
// pattern against context_id. Since, if non-nil, filters to rows with
// started_at >= Since.
type CheckpointListFilter struct {
	IncludeTerminal bool
	ContextPattern  *string
	Since           *time.Time
	Limit           int
}

// ListFiltered returns checkpoint rows matching the filter, ordered by
// started_at DESC. Replaces the former no-arg List method. Used by the
// production adapter that implements socket.CheckpointStatusReader.
func (r *CheckpointRepo) ListFiltered(ctx context.Context, f CheckpointListFilter) ([]Checkpoint, error) {
	args := []any{}
	where := []string{"1=1"}
	if !f.IncludeTerminal {
		where = append(where, "status NOT IN ('complete','aborted')")
	}
	if f.ContextPattern != nil {
		args = append(args, *f.ContextPattern)
		where = append(where, fmt.Sprintf("context_id LIKE $%d", len(args)))
	}
	if f.Since != nil {
		args = append(args, pgnanos.From(*f.Since))
		where = append(where, fmt.Sprintf("started_at >= $%d", len(args)))
	}
	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	args = append(args, limit)
	q := fmt.Sprintf(`
        SELECT request_id, context_type, context_id, op_args_hash, policy_hash,
               primary_player_id, justification, status, last_processed_event_id, new_dek_id,
               old_dek_id, phase3_rows_rewritten, phase5_attempt_count, phase5_missing_members,
               force_destroy, started_at, last_heartbeat_at, completed_at,
               aborted_at, aborted_reason
          FROM crypto_rekey_checkpoints
         WHERE %s
         ORDER BY started_at DESC
         LIMIT $%d`,
		strings.Join(where, " AND "), len(args))
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, oops.Code("DEK_REKEY_LIST_FAILED").Wrap(err)
	}
	defer rows.Close()
	var out []Checkpoint
	for rows.Next() {
		ckpt, err := scanCheckpoint(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ckpt)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("DEK_REKEY_LIST_ROWS_ERR").Wrap(err)
	}
	return out, nil
}

// scanCheckpoint reads a pgx Rows or Row into a Checkpoint.
// pgx.ErrNoRows is returned as-is so callers can distinguish not-found
// from other scan errors.
func scanCheckpoint(s checkpointScanner) (Checkpoint, error) {
	var c Checkpoint
	var rid []byte
	var startedAt, lastHeartbeatAt pgnanos.Time
	var completedAt, abortedAt *pgnanos.Time
	err := s.Scan(
		&rid, &c.ContextType, &c.ContextID, &c.opArgsHash, &c.policyHash,
		&c.PrimaryPlayerID, &c.Justification, &c.Status, &c.lastProcessedEventID, &c.NewDEKID,
		&c.OldDEKID, &c.Phase3RowsRewritten, &c.Phase5AttemptCount, &c.phase5MissingMembers,
		&c.ForceDestroy, &startedAt, &lastHeartbeatAt, &completedAt,
		&abortedAt, &c.AbortedReason,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Checkpoint{}, pgx.ErrNoRows
		}
		return Checkpoint{}, oops.Code("DEK_REKEY_CHECKPOINT_SCAN_FAILED").Wrap(err)
	}
	copy(c.RequestID[:], rid)
	c.StartedAt = startedAt.Time()
	c.LastHeartbeatAt = lastHeartbeatAt.Time()
	if completedAt != nil {
		t := completedAt.Time()
		c.CompletedAt = &t
	}
	if abortedAt != nil {
		t := abortedAt.Time()
		c.AbortedAt = &t
	}
	return c, nil
}

// checkpointScanner is satisfied by both pgx.Row and pgx.Rows.
type checkpointScanner interface {
	Scan(dest ...any) error
}

// isUniqueViolation returns true if err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505) mentioning the given constraint/index name.
// ConstraintName may be empty for partial indexes created via CREATE UNIQUE
// INDEX rather than CONSTRAINT UNIQUE; we fall back to message substring
// matching in that case.
func isUniqueViolation(err error, constraintName string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return false
	}
	if pgErr.ConstraintName == constraintName {
		return true
	}
	// Partial indexes use the index name in the message even when ConstraintName
	// is empty. Match the constraint name in the error detail or message.
	return strings.Contains(pgErr.Message, constraintName) ||
		strings.Contains(pgErr.Detail, constraintName)
}
