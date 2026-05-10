// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package approval

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// Repo is the storage interface for admin_approvals rows.
type Repo interface {
	Open(ctx context.Context, req OpenRequest) (RequestID, error)
	Get(ctx context.Context, id RequestID) (Approval, error)
	MarkApproved(ctx context.Context, id RequestID, secondOpPlayerID string) error
	WaitForApproval(ctx context.Context, id RequestID, deadline time.Time) (Approval, error)
}

// PostgresRepo is the production Repo backed by Postgres.
type PostgresRepo struct {
	pool  *pgxpool.Pool
	clock Clock
}

// realClock returns time.Now().
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// NewPostgresRepo constructs a PostgresRepo. clock may be nil; nil
// substitutes time.Now-backed realClock.
func NewPostgresRepo(pool *pgxpool.Pool, clock Clock) *PostgresRepo {
	if clock == nil {
		clock = realClock{}
	}
	return &PostgresRepo{pool: pool, clock: clock}
}

// Open inserts a fresh pending approval row. expires_at = now() + 5 min
// (server-side, matching Get / MarkApproved predicates).
func (r *PostgresRepo) Open(ctx context.Context, req OpenRequest) (RequestID, error) {
	id := RequestID(ulid.MustNew(ulid.Now(), ulid.Monotonic(rand.Reader, 0)))
	_, err := r.pool.Exec(ctx, `
		INSERT INTO admin_approvals
			(request_id, primary_player_id, op_kind, op_args_hash, expires_at)
		VALUES ($1, $2, $3, $4, now() + interval '5 minutes')
	`, id[:], req.PrimaryPlayerID, req.OpKind, req.OpArgsHash)
	if err != nil {
		return RequestID{}, oops.Code("APPROVAL_OPEN_FAILED").
			With("primary_player_id", req.PrimaryPlayerID).
			With("op_kind", req.OpKind).Wrap(err)
	}
	return id, nil
}

// Get returns the row by request_id, filtering expired rows. INV-D5.
func (r *PostgresRepo) Get(ctx context.Context, id RequestID) (Approval, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT request_id, primary_player_id, op_kind, op_args_hash,
		       expires_at, approved_at, COALESCE(approved_by_player_id, ''),
		       created_at
		  FROM admin_approvals
		 WHERE request_id = $1
		   AND expires_at >= now()
	`, id[:])
	var a Approval
	var ridBytes []byte
	var approvedAt *time.Time
	if err := row.Scan(&ridBytes, &a.PrimaryPlayerID, &a.OpKind, &a.OpArgsHash,
		&a.ExpiresAt, &approvedAt, &a.ApprovedByPlayerID, &a.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Approval{}, oops.Code("APPROVAL_NOT_FOUND").
				With("request_id", id.String()).
				Errorf("admin_approvals row not found or expired")
		}
		return Approval{}, oops.Code("APPROVAL_GET_FAILED").
			With("request_id", id.String()).Wrap(err)
	}
	copy(a.RequestID[:], ridBytes)
	a.ApprovedAt = approvedAt
	return a, nil
}

// getRaw returns the row by request_id WITHOUT filtering expired rows.
// Used by WaitForApproval to detect expiry mid-poll and short-circuit
// with DENY_APPROVAL_EXPIRED rather than sleeping until the caller's
// deadline. The caller is responsible for the expiry check after fetch.
func (r *PostgresRepo) getRaw(ctx context.Context, id RequestID) (Approval, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT request_id, primary_player_id, op_kind, op_args_hash,
		       expires_at, approved_at, COALESCE(approved_by_player_id, ''),
		       created_at
		  FROM admin_approvals
		 WHERE request_id = $1
	`, id[:])
	var a Approval
	var ridBytes []byte
	var approvedAt *time.Time
	if err := row.Scan(&ridBytes, &a.PrimaryPlayerID, &a.OpKind, &a.OpArgsHash,
		&a.ExpiresAt, &approvedAt, &a.ApprovedByPlayerID, &a.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Approval{}, oops.Code("APPROVAL_NOT_FOUND").
				With("request_id", id.String()).
				Errorf("admin_approvals row not found")
		}
		return Approval{}, oops.Code("APPROVAL_GET_FAILED").
			With("request_id", id.String()).Wrap(err)
	}
	copy(a.RequestID[:], ridBytes)
	a.ApprovedAt = approvedAt
	return a, nil
}

// MarkApproved is the atomic single-statement second-op signoff per spec
// §6 Approve flow. INV-D5 (TTL), INV-D6 (self-approval rejection),
// INV-D7 (already-approved rejection).
func (r *PostgresRepo) MarkApproved(ctx context.Context, id RequestID, secondOpPlayerID string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE admin_approvals
		   SET approved_at = now(), approved_by_player_id = $2
		 WHERE request_id = $1
		   AND approved_at IS NULL
		   AND expires_at >= now()
		   AND primary_player_id != $2
	`, id[:], secondOpPlayerID)
	if err != nil {
		return oops.Code("APPROVAL_MARK_FAILED").
			With("request_id", id.String()).Wrap(err)
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	// Differentiator: which predicate failed?
	row := r.pool.QueryRow(ctx, `
		SELECT primary_player_id, approved_at, expires_at
		  FROM admin_approvals
		 WHERE request_id = $1
	`, id[:])
	var primary string
	var approvedAt *time.Time
	var expiresAt time.Time
	if err := row.Scan(&primary, &approvedAt, &expiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.Code("DENY_APPROVAL_NOT_FOUND").
				With("request_id", id.String()).
				Errorf("admin_approvals row not found")
		}
		return oops.Code("APPROVAL_DIFFERENTIATE_FAILED").
			With("request_id", id.String()).Wrap(err)
	}
	switch {
	case primary == secondOpPlayerID:
		return oops.Code("DENY_DUAL_CONTROL_SELF").
			With("request_id", id.String()).
			With("player_id", secondOpPlayerID).
			Errorf("second-op player_id equals primary_player_id")
	case approvedAt != nil:
		return oops.Code("DENY_APPROVAL_ALREADY_APPROVED").
			With("request_id", id.String()).
			Errorf("approval already granted")
	case !expiresAt.After(r.clock.Now()):
		return oops.Code("DENY_APPROVAL_EXPIRED").
			With("request_id", id.String()).
			With("expires_at", expiresAt).
			Errorf("approval window expired")
	default:
		// Race-window fallback: between the UPDATE and the SELECT another
		// caller may have mutated the row. Atomicity of the UPDATE is the
		// load-bearing property; this code is operator-experience polish.
		return oops.Code("DENY_APPROVAL_FAILED").
			With("request_id", id.String()).
			Errorf("approval failed; race-window fallback")
	}
}

// WaitForApproval polls until approved_at is non-nil, the row expires, or
// the caller's deadline passes. Uses getRaw (unfiltered) so expiry is
// detected as soon as expires_at < now() and surfaces immediately as
// DENY_APPROVAL_EXPIRED — Get() would hide expired rows behind the
// APPROVAL_NOT_FOUND code and the loop would sleep past the
// server-enforced TTL until the caller's deadline.
func (r *PostgresRepo) WaitForApproval(ctx context.Context, id RequestID, deadline time.Time) (Approval, error) {
	const pollInterval = 500 * time.Millisecond
	for {
		if !r.clock.Now().Before(deadline) {
			return Approval{}, oops.Code("APPROVAL_WAIT_DEADLINE").
				With("request_id", id.String()).
				Errorf("WaitForApproval deadline passed")
		}
		a, err := r.getRaw(ctx, id)
		if err == nil {
			// Server-enforced TTL: surface expiry immediately rather
			// than polling past it. Mirrors MarkApproved's expiry path.
			if !a.ExpiresAt.After(r.clock.Now()) {
				return Approval{}, oops.Code("DENY_APPROVAL_EXPIRED").
					With("request_id", id.String()).
					With("expires_at", a.ExpiresAt).
					Errorf("approval window expired")
			}
			if a.ApprovedAt != nil {
				return a, nil
			}
		}
		// On APPROVAL_NOT_FOUND, keep polling — the row may still be
		// pending and visible on the next tick (race with Open). On
		// other errors, return.
		if err != nil && !isApprovalNotFound(err) {
			return Approval{}, err
		}
		select {
		case <-ctx.Done():
			return Approval{}, oops.Code("APPROVAL_WAIT_CANCELLED").
				With("request_id", id.String()).Wrap(ctx.Err())
		case <-time.After(pollInterval):
		}
	}
}

func isApprovalNotFound(err error) bool {
	if err == nil {
		return false
	}
	oopsErr, ok := oops.AsOops(err)
	if !ok {
		return false
	}
	return oopsErr.Code() == "APPROVAL_NOT_FOUND"
}
