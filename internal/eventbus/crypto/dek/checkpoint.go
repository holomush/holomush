// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/idgen"
)

// RequestID is owned by holomush-jxo8.7.18 (rekey.go). The dek package's
// RequestID type is defined there; CheckpointRepo consumes it via the
// shared package scope.

// CheckpointOpenRequest contains the arguments to open a new rekey
// checkpoint row. All fields are mandatory. Byte-slice fields are
// intentionally unexported (INV-27); use NewCheckpointOpenRequest to
// construct a value.
type CheckpointOpenRequest struct {
	ContextType     string
	ContextID       string
	opArgsHash      []byte // exactly 32 bytes
	policyHash      []byte // exactly 32 bytes
	PrimaryPlayerID string
	OldDEKID        int64
}

// NewCheckpointOpenRequest constructs a CheckpointOpenRequest. Both
// opArgsHash and policyHash must be exactly 32 bytes.
func NewCheckpointOpenRequest(
	contextType, contextID string,
	opArgsHash, policyHash []byte,
	primaryPlayerID string,
	oldDEKID int64,
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
	}, nil
}

// Checkpoint mirrors a crypto_rekey_checkpoints row.
// Byte-slice fields are intentionally unexported (INV-27): they are
// populated by scanCheckpoint and read only within this package.
type Checkpoint struct {
	RequestID            RequestID
	ContextType          string
	ContextID            string
	opArgsHash           []byte
	policyHash           []byte
	PrimaryPlayerID      string
	Status               CheckpointStatus
	lastProcessedEventID []byte
	NewDEKID             *int64
	OldDEKID             int64
	Phase5AttemptCount   int
	phase5MissingMembers []byte
	ForceDestroy         bool
	StartedAt            time.Time
	LastHeartbeatAt      time.Time
	CompletedAt          *time.Time
	AbortedAt            *time.Time
	AbortedReason        *string
}

// PolicyHash returns the policy_hash captured at Phase 1 (INV-E25) as a
// [32]byte array. Using a fixed-size array rather than []byte preserves
// INV-27 (no exported []byte in the dek package). The array is zero-padded
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
// INV-27 (no exported []byte in the dek package); the bool replaces the
// nil-slice nullability signal.
func (c *Checkpoint) LastProcessedEventID() ([16]byte, bool) {
	var out [16]byte
	if len(c.lastProcessedEventID) == 0 {
		return out, false
	}
	copy(out[:], c.lastProcessedEventID)
	return out, true
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
// pair (INV-E5, enforced by the partial unique index).
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
           primary_player_id, status, old_dek_id)
        VALUES ($1, $2, $3, $4, $5, $6, 'pending', $7)
    `, rid[:], req.ContextType, req.ContextID, req.opArgsHash, req.policyHash,
		req.PrimaryPlayerID, req.OldDEKID)
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

// Get returns the checkpoint row for rid, or an error if not found.
func (r *CheckpointRepo) Get(ctx context.Context, rid RequestID) (Checkpoint, error) {
	row := r.pool.QueryRow(ctx, `
        SELECT request_id, context_type, context_id, op_args_hash, policy_hash,
               primary_player_id, status, last_processed_event_id, new_dek_id,
               old_dek_id, phase5_attempt_count, phase5_missing_members,
               force_destroy, started_at, last_heartbeat_at, completed_at,
               aborted_at, aborted_reason
          FROM crypto_rekey_checkpoints
         WHERE request_id = $1
    `, rid[:])
	return scanCheckpoint(row)
}

// UpdateStatus transitions status from → to using a CAS UPDATE that
// atomically guards against stale writers. Returns DEK_REKEY_STALE_TRANSITION
// if the row was not in state `from` at commit time (INV-E1).
// AssertTransitionAllowed gates the FSM semantics before issuing SQL.
func (r *CheckpointRepo) UpdateStatus(ctx context.Context, rid RequestID, from, to CheckpointStatus) error {
	if err := AssertTransitionAllowed(from, to); err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `
        UPDATE crypto_rekey_checkpoints
           SET status = $2, last_heartbeat_at = now()
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
		`UPDATE crypto_rekey_checkpoints SET last_heartbeat_at = now() WHERE request_id = $1`,
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
               primary_player_id, status, last_processed_event_id, new_dek_id,
               old_dek_id, phase5_attempt_count, phase5_missing_members,
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
               primary_player_id, status, last_processed_event_id, new_dek_id,
               old_dek_id, phase5_attempt_count, phase5_missing_members,
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
// (INV-E18).
func (r *CheckpointRepo) ListExpired(ctx context.Context, ttl time.Duration) ([]Checkpoint, error) {
	rows, err := r.pool.Query(ctx, `
        SELECT request_id, context_type, context_id, op_args_hash, policy_hash,
               primary_player_id, status, last_processed_event_id, new_dek_id,
               old_dek_id, phase5_attempt_count, phase5_missing_members,
               force_destroy, started_at, last_heartbeat_at, completed_at,
               aborted_at, aborted_reason
          FROM crypto_rekey_checkpoints
         WHERE status NOT IN ('complete', 'aborted')
           AND last_heartbeat_at < now() - $1::interval
    `, ttl.String())
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
           SET status = 'aborted', aborted_at = now(), aborted_reason = $2
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
           SET status = 'complete', completed_at = now()
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
// (INV-E10). The CAS predicate requires both status='phase5_invalidate' AND
// force_destroy=true.
func (r *CheckpointRepo) UpdateStatusForceDestroy(ctx context.Context, rid RequestID) error {
	if err := AssertTransitionAllowed(CheckpointStatusPhase5Invalidate, CheckpointStatusPhase6DestroyOld); err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `
        UPDATE crypto_rekey_checkpoints
           SET status = 'phase6_destroy_old', last_heartbeat_at = now()
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

// AdvanceCursor updates last_processed_event_id within the caller's active
// pgx.Tx so that the cursor advance commits atomically with the events_audit
// row UPDATEs (INV-E7). The CAS predicate requires status='phase3_reencrypt_cold'.
func (r *CheckpointRepo) AdvanceCursor(ctx context.Context, tx pgx.Tx, rid RequestID, eventID []byte) error {
	tag, err := tx.Exec(ctx, `
        UPDATE crypto_rekey_checkpoints
           SET last_processed_event_id = $2, last_heartbeat_at = now()
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
           SET new_dek_id = $2, status = 'phase2_mint_dek', last_heartbeat_at = now()
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
            SET phase5_attempt_count = phase5_attempt_count + 1, last_heartbeat_at = now()
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
            SET status = 'phase5_invalidate', phase5_missing_members = $2::jsonb, last_heartbeat_at = now()
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
            SET status = 'phase5_invalidate', phase5_missing_members = NULL, last_heartbeat_at = now()
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

// List returns all checkpoint rows (no filter). For operator inspection
// and the admin list command. Ordered by started_at DESC.
func (r *CheckpointRepo) List(ctx context.Context) ([]Checkpoint, error) {
	rows, err := r.pool.Query(ctx, `
        SELECT request_id, context_type, context_id, op_args_hash, policy_hash,
               primary_player_id, status, last_processed_event_id, new_dek_id,
               old_dek_id, phase5_attempt_count, phase5_missing_members,
               force_destroy, started_at, last_heartbeat_at, completed_at,
               aborted_at, aborted_reason
          FROM crypto_rekey_checkpoints
         ORDER BY started_at DESC
    `)
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
func scanCheckpoint(s checkpointScanner) (Checkpoint, error) {
	var c Checkpoint
	var rid []byte
	err := s.Scan(
		&rid, &c.ContextType, &c.ContextID, &c.opArgsHash, &c.policyHash,
		&c.PrimaryPlayerID, &c.Status, &c.lastProcessedEventID, &c.NewDEKID,
		&c.OldDEKID, &c.Phase5AttemptCount, &c.phase5MissingMembers,
		&c.ForceDestroy, &c.StartedAt, &c.LastHeartbeatAt, &c.CompletedAt,
		&c.AbortedAt, &c.AbortedReason,
	)
	if err != nil {
		return Checkpoint{}, oops.Code("DEK_REKEY_CHECKPOINT_SCAN_FAILED").Wrap(err)
	}
	copy(c.RequestID[:], rid)
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
