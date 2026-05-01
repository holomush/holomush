// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus/codec"
)

// ContextID names a DEK's social unit (scene, DM, channel, character,
// player). Per master spec §5.1.
type ContextID struct {
	Type string
	ID   string
}

// Participant is a member of a DEK's participant set. JSONB-encoded
// in crypto_keys.participants.
type Participant struct {
	PlayerID    string    `json:"player_id"`
	CharacterID string    `json:"character_id"`
	BindingID   string    `json:"binding_id"`
	JoinedAt    time.Time `json:"joined_at"`
	AddedVia    string    `json:"added_via,omitempty"`
}

// OperatorFactors captures the operator's identity for Rekey audit.
// Phase 2 declares the type but only Rekey (Phase 5 stub) consumes it.
type OperatorFactors struct {
	OSUser                     string
	PlayerID                   string
	TOTPVerified               bool
	AuthProviderName           string
	ProviderSpecificID         string
	DualControlPartnerPlayerID string
}

// row mirrors a crypto_keys row. Internal to dek/.
type row struct {
	ID           int64
	ContextType  string
	ContextID    string
	Version      uint32
	WrappedDEK   []byte
	WrapProvider string
	WrapKeyID    string
	Participants []Participant
	CreatedAt    time.Time
	RotatedAt    *time.Time
}

// Store persists wrapped DEKs in the crypto_keys table. The provider
// (KEK wrap/unwrap) is owned by Manager; Store is purely the SQL layer.
//
// Methods on Store are package-private because they return the internal
// row type. Manager (same package) is the sole caller.
type Store struct {
	pool *pgxpool.Pool
	// preInsertHook, when non-nil, is called immediately before each
	// INSERT in `insert`. Tests use it to coordinate concurrent
	// goroutines through the unique-violation race window. Production
	// code MUST NOT set this; it is exposed only via
	// SetPreInsertHookForTest.
	preInsertHook func()
}

// NewStore wraps a pgxpool.Pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// SetPreInsertHookForTest installs a hook called immediately before
// each INSERT in `insert`. Used exclusively by integration tests
// (`TestManager_GetOrCreate_ConcurrentMintRace`) to force two
// goroutines to reach the INSERT call simultaneously, guaranteeing
// the PG unique-violation recovery path runs. The hook field is
// unexported; this setter is the only way to install one. Production
// code MUST NOT call this method.
func (s *Store) SetPreInsertHookForTest(hook func()) {
	s.preInsertHook = hook
}

// selectActive returns the active (rotated_at IS NULL) row for ctxID,
// or pgx.ErrNoRows if none exists. The pgx.ErrNoRows sentinel is
// preserved through oops wrapping so callers can use errors.Is.
func (s *Store) selectActive(ctx context.Context, ctxID ContextID) (row, error) {
	var r row
	var participantsJSON []byte
	err := s.pool.QueryRow(ctx, `
        SELECT id, context_type, context_id, version, wrapped_dek,
               wrap_provider, wrap_key_id, participants, created_at, rotated_at
          FROM crypto_keys
         WHERE context_type=$1 AND context_id=$2 AND rotated_at IS NULL
         ORDER BY version DESC
         LIMIT 1
    `, ctxID.Type, ctxID.ID).Scan(
		&r.ID, &r.ContextType, &r.ContextID, &r.Version, &r.WrappedDEK,
		&r.WrapProvider, &r.WrapKeyID, &participantsJSON, &r.CreatedAt, &r.RotatedAt,
	)
	if err != nil {
		// Wrap with oops while preserving pgx.ErrNoRows for errors.Is.
		return row{}, oops.With("operation", "select_active_dek").
			With("context_type", ctxID.Type).
			With("context_id", ctxID.ID).Wrap(err)
	}
	if err := json.Unmarshal(participantsJSON, &r.Participants); err != nil {
		return row{}, oops.Code("DEK_PARTICIPANTS_UNMARSHAL_FAILED").Wrap(err)
	}
	return r, nil
}

// selectByID returns the row for keyID + version.
func (s *Store) selectByID(ctx context.Context, keyID codec.KeyID, version uint32) (row, error) {
	var r row
	var participantsJSON []byte
	err := s.pool.QueryRow(ctx, `
        SELECT id, context_type, context_id, version, wrapped_dek,
               wrap_provider, wrap_key_id, participants, created_at, rotated_at
          FROM crypto_keys
         WHERE id=$1 AND version=$2
    `, int64(keyID), version).Scan( //nolint:gosec // G115: codec.KeyID is a DB BIGSERIAL id; the int64↔uint64 conversion mirrors the column type and cannot overflow in practice (positive serial ids < 2^63).
		&r.ID, &r.ContextType, &r.ContextID, &r.Version, &r.WrappedDEK,
		&r.WrapProvider, &r.WrapKeyID, &participantsJSON, &r.CreatedAt, &r.RotatedAt,
	)
	if err != nil {
		return row{}, oops.With("operation", "select_dek_by_id").
			With("key_id", uint64(keyID)).
			With("version", version).Wrap(err)
	}
	if err := json.Unmarshal(participantsJSON, &r.Participants); err != nil {
		return row{}, oops.Code("DEK_PARTICIPANTS_UNMARSHAL_FAILED").Wrap(err)
	}
	return r, nil
}

// insert writes a fresh row. Returns the assigned id and the
// pg unique-violation error if the row already exists (caller
// re-runs selectActive). The unique-violation is detectable via
// IsUniqueViolation; the sentinel is preserved through oops wrapping
// (oops.Wrap preserves errors.As targets).
func (s *Store) insert(ctx context.Context, in row) (int64, error) {
	pj, err := json.Marshal(in.Participants)
	if err != nil {
		return 0, oops.Code("DEK_PARTICIPANTS_MARSHAL_FAILED").Wrap(err)
	}
	if s.preInsertHook != nil {
		s.preInsertHook()
	}
	var id int64
	err = s.pool.QueryRow(ctx, `
        INSERT INTO crypto_keys
            (context_type, context_id, version, wrapped_dek,
             wrap_provider, wrap_key_id, participants)
        VALUES ($1,$2,$3,$4,$5,$6,$7)
        RETURNING id
    `, in.ContextType, in.ContextID, in.Version, in.WrappedDEK,
		in.WrapProvider, in.WrapKeyID, pj).Scan(&id)
	if err != nil {
		return 0, oops.With("operation", "insert_dek").
			With("context_type", in.ContextType).
			With("context_id", in.ContextID).
			With("version", in.Version).Wrap(err)
	}
	return id, nil
}

// IsUniqueViolation returns true if err is a PG unique-constraint
// violation (used by Manager.GetOrCreate to detect concurrent INSERT
// races).
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

// SelectAllParticipants returns every row's participants list — used
// only by Phase 4+ lifecycle ops; exported here for forward stability
// (epic holomush-fi0n consumes this).
func (s *Store) SelectAllParticipants(ctx context.Context, ctxID ContextID) ([][]Participant, error) {
	rows, err := s.pool.Query(ctx, `
        SELECT participants FROM crypto_keys
         WHERE context_type=$1 AND context_id=$2
    `, ctxID.Type, ctxID.ID)
	if err != nil {
		return nil, oops.With("operation", "select_all_participants").
			With("context_type", ctxID.Type).
			With("context_id", ctxID.ID).Wrap(err)
	}
	defer rows.Close()
	var out [][]Participant
	for rows.Next() {
		var pj []byte
		if err := rows.Scan(&pj); err != nil {
			return nil, oops.With("operation", "scan_participants").Wrap(err)
		}
		var ps []Participant
		if err := json.Unmarshal(pj, &ps); err != nil {
			return nil, oops.Code("DEK_PARTICIPANTS_UNMARSHAL_FAILED").Wrap(err)
		}
		out = append(out, ps)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.With("operation", "iterate_participants").Wrap(err)
	}
	return out, nil
}
