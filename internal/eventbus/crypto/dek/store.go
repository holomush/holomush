// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/pgnanos"
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
	CreatedAt    pgnanos.Time
	RotatedAt    *pgnanos.Time
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
         WHERE context_type=$1 AND context_id=$2 AND rotated_at IS NULL AND destroyed_at IS NULL
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
         WHERE id=$1 AND version=$2 AND destroyed_at IS NULL
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

// updateParticipants appends p to the active DEK's participant set.
// Idempotent on (player_id, binding_id) — duplicate returns the active
// row unchanged with added=false. Returns the active row wrapped in a
// pgx.ErrNoRows sentinel when no active DEK exists.
//
// Uses SELECT ... FOR UPDATE within a transaction to serialize concurrent
// Add calls on the same (context_type, context_id). Without the row lock,
// two concurrent Adds of distinct participants can both read the same
// participant list, each append their own entry, and the second write
// silently discards the first (holomush-fi0n.9).
func (s *Store) updateParticipants(ctx context.Context, ctxID ContextID, p Participant) (row, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return row{}, false, oops.Code("DEK_TX_BEGIN_FAILED").
			With("context_type", ctxID.Type).
			With("context_id", ctxID.ID).Wrap(err)
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck // no-op after Commit

	// SELECT the active row and lock it until the transaction commits.
	var active row
	var participantsJSON []byte
	err = tx.QueryRow(
		ctx, `
		SELECT id, context_type, context_id, version, wrapped_dek,
		       wrap_provider, wrap_key_id, participants, created_at, rotated_at
		  FROM crypto_keys
		 WHERE context_type=$1 AND context_id=$2
		   AND rotated_at IS NULL AND destroyed_at IS NULL
		 ORDER BY version DESC
		 LIMIT 1
		 FOR UPDATE`,
		ctxID.Type, ctxID.ID,
	).Scan(
		&active.ID, &active.ContextType, &active.ContextID, &active.Version,
		&active.WrappedDEK, &active.WrapProvider, &active.WrapKeyID,
		&participantsJSON, &active.CreatedAt, &active.RotatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return row{}, false, oops.With("operation", "select_active_dek_for_update").
				With("context_type", ctxID.Type).
				With("context_id", ctxID.ID).Wrap(err)
		}
		return row{}, false, oops.Code("DEK_STORE_SELECT_FAILED").
			With("context_type", ctxID.Type).
			With("context_id", ctxID.ID).Wrap(err)
	}
	if err = json.Unmarshal(participantsJSON, &active.Participants); err != nil {
		return row{}, false, oops.Code("DEK_PARTICIPANTS_UNMARSHAL_FAILED").Wrap(err)
	}

	// Check for existing duplicate.
	for _, existing := range active.Participants {
		if existing.PlayerID == p.PlayerID && existing.BindingID == p.BindingID {
			if err = tx.Commit(ctx); err != nil {
				return row{}, false, oops.Code("DEK_TX_COMMIT_FAILED").
					With("context_type", ctxID.Type).
					With("context_id", ctxID.ID).Wrap(err)
			}
			return active, false, nil
		}
	}

	// Append and write back.
	active.Participants = append(active.Participants, p)
	newJSON, err := json.Marshal(active.Participants)
	if err != nil {
		return row{}, false, oops.Code("DEK_PARTICIPANTS_MARSHAL_FAILED").Wrap(err)
	}

	_, err = tx.Exec(
		ctx, `
		UPDATE crypto_keys
		   SET participants = $3
		 WHERE id = $1 AND version = $2
		   AND rotated_at IS NULL AND destroyed_at IS NULL`,
		active.ID, active.Version, newJSON,
	)
	if err != nil {
		return row{}, false, oops.Code("DEK_PARTICIPANTS_UPDATE_FAILED").
			With("context_type", ctxID.Type).
			With("context_id", ctxID.ID).Wrap(err)
	}

	if err = tx.Commit(ctx); err != nil {
		return row{}, false, oops.Code("DEK_TX_COMMIT_FAILED").
			With("context_type", ctxID.Type).
			With("context_id", ctxID.ID).Wrap(err)
	}

	return active, true, nil
}

// markRotated sets rotated_at and superseded_by on the target row.
func (s *Store) markRotated(ctx context.Context, keyID codec.KeyID, version uint32, supersededBy int64) error {
	tag, err := s.pool.Exec(
		ctx, `
		UPDATE crypto_keys
		   SET rotated_at = $4, superseded_by = $3
		 WHERE id = $1 AND version = $2 AND rotated_at IS NULL AND destroyed_at IS NULL`,
		//nolint:gosec // G115: keyID is a DB BIGSERIAL value; positive serial ids fit in int64
		int64(keyID), version, supersededBy, pgnanos.From(time.Now()),
	)
	if err != nil {
		return oops.Code("DEK_MARK_ROTATED_FAILED").
			With("key_id", uint64(keyID)).
			With("version", version).Wrap(err)
	}
	if tag.RowsAffected() == 0 {
		return oops.Code("DEK_MARK_ROTATED_NOT_FOUND").
			With("key_id", uint64(keyID)).
			With("version", version).
			Errorf("no unrotated row for key %d v%d", keyID, version)
	}
	return nil
}

// markDestroyed sets destroyed_at on the target row. Best-effort
// rollback helper — used when Rotate's invalidation fails.
func (s *Store) markDestroyed(ctx context.Context, keyID codec.KeyID, version uint32) error {
	_, err := s.pool.Exec(
		ctx, `
		UPDATE crypto_keys
		   SET destroyed_at = $3
		 WHERE id = $1 AND version = $2 AND destroyed_at IS NULL`,
		//nolint:gosec // G115: keyID is a DB BIGSERIAL value; positive serial ids fit in int64
		int64(keyID), version, pgnanos.From(time.Now()),
	)
	if err != nil {
		return oops.Code("DEK_MARK_DESTROYED_FAILED").
			With("key_id", uint64(keyID)).
			With("version", version).Wrap(err)
	}
	return nil
}

// markDestroyedByPK sets destroyed_at on the crypto_keys row with the
// given primary key id. Idempotent: a row already destroyed (destroyed_at IS
// NOT NULL) is unaffected — zero rows updated is a no-op success, satisfying
// INV-E12-PHASE6-IDEMPOTENT. Used by Phase 6 of the Rekey orchestrator.
func (s *Store) markDestroyedByPK(ctx context.Context, dekID int64) error {
	_, err := s.pool.Exec(
		ctx, `
		UPDATE crypto_keys
		   SET destroyed_at = $2
		 WHERE id = $1 AND destroyed_at IS NULL`,
		dekID, pgnanos.From(time.Now()),
	)
	if err != nil {
		return oops.Code("DEK_MARK_DESTROYED_FAILED").
			With("dek_id", dekID).Wrap(err)
	}
	return nil
}

// selectByBindingID returns all active DEK rows whose participants
// array contains an element with the given binding_id. Used by the
// wizard-transfer rebind handler to find affected DEKs.
//
//nolint:unused // used by rebind handler (Phase 4 integration, TBD)
func (s *Store) selectByBindingID(ctx context.Context, bindingID string) ([]row, error) {
	probe := []map[string]string{{"binding_id": bindingID}}
	probeJSON, err := json.Marshal(probe)
	if err != nil {
		return nil, oops.Code("DEK_BINDING_PROBE_MARSHAL_FAILED").Wrap(err)
	}

	rows, err := s.pool.Query(
		ctx, `
		SELECT id, context_type, context_id, version, wrapped_dek,
		       wrap_provider, wrap_key_id, participants, created_at, rotated_at
		  FROM crypto_keys
		 WHERE participants @> $1::jsonb
		   AND rotated_at IS NULL AND destroyed_at IS NULL
		 ORDER BY id`,
		probeJSON,
	)
	if err != nil {
		return nil, oops.Code("DEK_SELECT_BY_BINDING_FAILED").Wrap(err)
	}
	defer rows.Close()

	var out []row
	for rows.Next() {
		var r row
		var participantsJSON []byte
		if err := rows.Scan(
			&r.ID, &r.ContextType, &r.ContextID, &r.Version, &r.WrappedDEK,
			&r.WrapProvider, &r.WrapKeyID, &participantsJSON, &r.CreatedAt, &r.RotatedAt,
		); err != nil {
			return nil, oops.Code("DEK_SELECT_BY_BINDING_SCAN_FAILED").Wrap(err)
		}
		if err := json.Unmarshal(participantsJSON, &r.Participants); err != nil {
			return nil, oops.Code("DEK_PARTICIPANTS_UNMARSHAL_FAILED").Wrap(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("DEK_SELECT_BY_BINDING_ROWS_ERR").Wrap(err)
	}
	return out, nil
}

// selectByPK returns the crypto_keys row whose primary key id equals id,
// regardless of rotated_at or destroyed_at. Used by Phase 2 rekey to load
// the old DEK row (participants + version) without knowing the version
// up-front. Package-private: only the Orchestrator's MintNewDEKForRekey path
// calls this.
func (s *Store) selectByPK(ctx context.Context, id int64) (row, error) {
	var r row
	var participantsJSON []byte
	err := s.pool.QueryRow(ctx, `
        SELECT id, context_type, context_id, version, wrapped_dek,
               wrap_provider, wrap_key_id, participants, created_at, rotated_at
          FROM crypto_keys
         WHERE id = $1
    `, id).Scan(
		&r.ID, &r.ContextType, &r.ContextID, &r.Version, &r.WrappedDEK,
		&r.WrapProvider, &r.WrapKeyID, &participantsJSON, &r.CreatedAt, &r.RotatedAt,
	)
	if err != nil {
		return row{}, oops.With("operation", "select_dek_by_pk").
			With("id", id).Wrap(err)
	}
	if err := json.Unmarshal(participantsJSON, &r.Participants); err != nil {
		return row{}, oops.Code("DEK_PARTICIPANTS_UNMARSHAL_FAILED").Wrap(err)
	}
	return r, nil
}

// insertRekeyed INSERTs a crypto_keys row at version = old.Version+1 with
// the same participants column bytes as old (re-marshaled from the same Go
// slice). Used by Phase 2 to mint the new DEK with INV-E6-PARTICIPANT-INVARIANCE.
// Returns the new row's primary key id.
func (s *Store) insertRekeyed(ctx context.Context, old row, wrapped []byte, wrapKeyID string) (int64, error) {
	participantsJSON, err := json.Marshal(old.Participants)
	if err != nil {
		return 0, oops.Code("DEK_PARTICIPANTS_MARSHAL_FAILED").Wrap(err)
	}
	var id int64
	err = s.pool.QueryRow(ctx, `
        INSERT INTO crypto_keys (context_type, context_id, version, wrapped_dek,
                                  wrap_provider, wrap_key_id, participants, created_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8)
        RETURNING id
    `, old.ContextType, old.ContextID, old.Version+1, wrapped,
		old.WrapProvider /* same provider */, wrapKeyID, participantsJSON, pgnanos.From(time.Now())).Scan(&id)
	if err != nil {
		return 0, oops.Code("DEK_REKEY_INSERT_NEW_ROW_FAILED").Wrap(err)
	}
	return id, nil
}

// ResolveIntegrity finds contexts with multiple unrotated, undestroyed
// rows (crashed Rotate) and marks the earlier versions rotated, keeping
// only the max version active. Idempotent — safe to run at every startup.
func (s *Store) ResolveIntegrity(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `
		SELECT context_type, context_id
		  FROM crypto_keys
		 WHERE rotated_at IS NULL AND destroyed_at IS NULL
		 GROUP BY context_type, context_id
		HAVING COUNT(*) > 1`)
	if err != nil {
		return oops.Code("DEK_INTEGRITY_QUERY_FAILED").Wrap(err)
	}
	defer rows.Close()

	type ctxKey struct {
		ctxType, ctxID string
	}
	var conflicted []ctxKey
	for rows.Next() {
		var ck ctxKey
		if err := rows.Scan(&ck.ctxType, &ck.ctxID); err != nil {
			return oops.Code("DEK_INTEGRITY_SCAN_FAILED").Wrap(err)
		}
		conflicted = append(conflicted, ck)
	}
	if err := rows.Err(); err != nil {
		return oops.Code("DEK_INTEGRITY_ROWS_ERR").Wrap(err)
	}

	for _, ck := range conflicted {
		// Mark all but the max-version row as rotated.
		_, err := s.pool.Exec(
			ctx, `
			UPDATE crypto_keys
			   SET rotated_at = $3
			 WHERE context_type = $1 AND context_id = $2
			   AND rotated_at IS NULL AND destroyed_at IS NULL
			   AND version < (
			       SELECT MAX(version) FROM crypto_keys
			        WHERE context_type = $1 AND context_id = $2
			          AND rotated_at IS NULL AND destroyed_at IS NULL
			   )`,
			ck.ctxType, ck.ctxID, pgnanos.From(time.Now()),
		)
		if err != nil {
			return oops.Code("DEK_INTEGRITY_RESOLVE_FAILED").
				With("context_type", ck.ctxType).
				With("context_id", ck.ctxID).Wrap(err)
		}
	}
	return nil
}

// Row is the exported view of a crypto_keys row. Mirrors the internal
// `row` struct but adds DestroyedAt (only populated by SelectAnyByID).
//
// INV-CRYPTO-16 note: the wrapped DEK ciphertext is intentionally held in an
// unexported `wrappedDEK` field rather than an exported `WrappedDEK
// []byte` — the static API guard in api_test.go rejects exported
// []byte fields and methods returning []byte across the dek package
// surface, treating wrapped bytes as a key-material adjacent surface
// (a buggy caller could log/marshal them, complicating forensic
// reasoning about what state ever left the package). Phase 5 Rekey
// audit emission consumes Row inside the dek package and reads
// `wrappedDEK` directly.
type Row struct {
	ID           int64
	ContextType  string
	ContextID    string
	Version      uint32
	wrappedDEK   []byte
	WrapProvider string
	WrapKeyID    string
	Participants []Participant
	CreatedAt    time.Time
	RotatedAt    *time.Time
	DestroyedAt  *time.Time
}

// SelectAnyByID returns the row for keyID + version regardless of
// destroyed_at. Used by Phase 5 Rekey audit emission and operator
// forensic tools; production read paths MUST use selectByID (which
// filters destroyed rows). Phase 3c grounding doc Decision 4.
func (s *Store) SelectAnyByID(ctx context.Context, keyID codec.KeyID, version uint32) (Row, error) {
	var r row
	var participantsJSON []byte
	var destroyedAt *pgnanos.Time
	err := s.pool.QueryRow(ctx, `
        SELECT id, context_type, context_id, version, wrapped_dek,
               wrap_provider, wrap_key_id, participants, created_at, rotated_at, destroyed_at
          FROM crypto_keys
         WHERE id=$1 AND version=$2
    `, int64(keyID), version).Scan( //nolint:gosec // G115: codec.KeyID is a DB BIGSERIAL id; the int64↔uint64 conversion mirrors the column type and cannot overflow in practice (positive serial ids < 2^63).
		&r.ID, &r.ContextType, &r.ContextID, &r.Version, &r.WrappedDEK,
		&r.WrapProvider, &r.WrapKeyID, &participantsJSON, &r.CreatedAt, &r.RotatedAt, &destroyedAt,
	)
	if err != nil {
		return Row{}, oops.With("operation", "select_any_dek_by_id").
			With("key_id", uint64(keyID)).
			With("version", version).Wrap(err)
	}
	if err := json.Unmarshal(participantsJSON, &r.Participants); err != nil {
		return Row{}, oops.Code("DEK_PARTICIPANTS_UNMARSHAL_FAILED").Wrap(err)
	}
	var rotatedAt *time.Time
	if r.RotatedAt != nil {
		t := r.RotatedAt.Time()
		rotatedAt = &t
	}
	var destroyedAtTime *time.Time
	if destroyedAt != nil {
		t := destroyedAt.Time()
		destroyedAtTime = &t
	}
	return Row{
		ID:           r.ID,
		ContextType:  r.ContextType,
		ContextID:    r.ContextID,
		Version:      r.Version,
		wrappedDEK:   r.WrappedDEK,
		WrapProvider: r.WrapProvider,
		WrapKeyID:    r.WrapKeyID,
		Participants: r.Participants,
		CreatedAt:    r.CreatedAt.Time(),
		RotatedAt:    rotatedAt,
		DestroyedAt:  destroyedAtTime,
	}, nil
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
