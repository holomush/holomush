// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek

import (
	"context"
	"crypto/rand"
	"errors"
	"io"

	"github.com/jackc/pgx/v5"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
)

// DEKByteLength matches kek.KEKByteLength: chacha20poly1305 key size.
const DEKByteLength = 32

// Manager owns DEK lifecycle. Phase 2 ships a skeleton: GetOrCreate
// and Resolve are real; Add, Rotate, Rekey return tracking-bead-tagged
// stubs (Phase 4 + Phase 5).
type Manager interface {
	GetOrCreate(ctx context.Context, ctxID ContextID, initial []Participant) (codec.Key, error)
	Resolve(ctx context.Context, keyID codec.KeyID, version uint32) (codec.Key, error)

	// Participants returns the participant set for a (keyID, version) DEK.
	// Read by AuthGuard via the ParticipantLookup adapter (Phase 3b
	// grounding doc Decision 1). Phase 3b uses fetch-fresh-on-every-call;
	// caching lands in Phase 3c (DEK cache invalidation, holomush-ojw1.3).
	Participants(ctx context.Context, keyID codec.KeyID, version uint32) ([]Participant, error)

	// Phase 4 stub — see holomush-fi0n.
	Add(ctx context.Context, ctxID ContextID, p Participant) error
	Rotate(ctx context.Context, ctxID ContextID, newParticipants []Participant, reason string) error

	// Phase 5 stub — see holomush-jxo8.
	Rekey(ctx context.Context, ctxID ContextID, justification string, ops OperatorFactors) error
}

// manager is the concrete impl.
type manager struct {
	provider kek.Provider
	store    *Store
	cache    *Cache
}

// NewManager constructs a real Manager. Production callers (Phase 3+)
// pass a real KEK provider and pgxpool.Pool-backed Store. All three
// collaborators are required; a nil argument returns
// DEK_MANAGER_DEPENDENCY_NIL rather than letting GetOrCreate/Resolve
// dereference nil at runtime.
func NewManager(provider kek.Provider, store *Store, cache *Cache) (Manager, error) {
	switch {
	case provider == nil:
		return nil, oops.Code("DEK_MANAGER_DEPENDENCY_NIL").
			With("dependency", "provider").
			Errorf("dek.NewManager requires a non-nil kek.Provider")
	case store == nil:
		return nil, oops.Code("DEK_MANAGER_DEPENDENCY_NIL").
			With("dependency", "store").
			Errorf("dek.NewManager requires a non-nil *Store")
	case cache == nil:
		return nil, oops.Code("DEK_MANAGER_DEPENDENCY_NIL").
			With("dependency", "cache").
			Errorf("dek.NewManager requires a non-nil *Cache")
	}
	return &manager{provider: provider, store: store, cache: cache}, nil
}

// NewManagerForUnitTest constructs a Manager with no DB or KEK access.
// GetOrCreate/Resolve will return DEK_MANAGER_NOT_CONFIGURED at runtime;
// only the stub methods (Add/Rotate/Rekey) are exercisable. Used by
// api_test.go for stub-bead allow-set checking.
func NewManagerForUnitTest() Manager {
	return &manager{}
}

// configured returns DEK_MANAGER_NOT_CONFIGURED if any of the
// collaborators are nil. GetOrCreate/Resolve must call this before
// dereferencing m.provider/m.store/m.cache to avoid nil-panics on
// Managers built via NewManagerForUnitTest.
func (m *manager) configured() error {
	if m.provider == nil || m.store == nil || m.cache == nil {
		return oops.Code("DEK_MANAGER_NOT_CONFIGURED").
			Errorf("Manager built via NewManagerForUnitTest cannot perform DEK operations; " +
				"only the Add/Rotate/Rekey stubs are exercisable")
	}
	return nil
}

// GetOrCreate returns the active DEK for ctxID, minting v1 if no row
// exists. On concurrent INSERT race, the loser re-SELECTs and uses
// the winner's row (PG unique constraint guarantees one winner).
func (m *manager) GetOrCreate(ctx context.Context, ctxID ContextID, initial []Participant) (codec.Key, error) {
	if err := m.configured(); err != nil {
		return codec.Key{}, err
	}
	// Try the active row first.
	if r, err := m.store.selectActive(ctx, ctxID); err == nil {
		return m.unwrapAndCache(ctx, r)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return codec.Key{}, oops.Code("DEK_STORE_SELECT_FAILED").Wrap(err)
	}

	// Mint a fresh DEK and INSERT.
	dekBytes := make([]byte, DEKByteLength)
	if _, err := io.ReadFull(rand.Reader, dekBytes); err != nil {
		return codec.Key{}, oops.Code("DEK_RNG_FAILED").Wrap(err)
	}
	wrapped, kekKeyID, err := m.provider.Wrap(ctx, dekBytes)
	if err != nil {
		return codec.Key{}, oops.Code("DEK_WRAP_FAILED").Wrap(err)
	}
	if validateErr := validateProviderWrapOutput(wrapped, kekKeyID); validateErr != nil {
		return codec.Key{}, validateErr
	}
	in := row{
		ContextType:  ctxID.Type,
		ContextID:    ctxID.ID,
		Version:      1,
		WrappedDEK:   wrapped,
		WrapProvider: m.provider.Name(),
		WrapKeyID:    kekKeyID,
		Participants: initial,
	}
	id, err := m.store.insert(ctx, in)
	if err != nil {
		if IsUniqueViolation(err) {
			// Race: someone else minted v1 first. Re-SELECT and use theirs.
			existing, selErr := m.store.selectActive(ctx, ctxID)
			if selErr != nil {
				return codec.Key{}, oops.Code("DEK_STORE_SELECT_FAILED").Wrap(selErr)
			}
			return m.unwrapAndCache(ctx, existing)
		}
		return codec.Key{}, oops.Code("DEK_STORE_INSERT_FAILED").Wrap(err)
	}
	in.ID = id
	material := NewMaterial(dekBytes)
	keyID := codec.KeyID(id) //nolint:gosec // G115: id is a DB BIGSERIAL value; positive serial ids fit in uint64.
	m.cache.Put(CacheKey{KeyID: keyID, Version: 1}, material)
	return material.AsCodecKey(keyID, 1), nil
}

// Resolve returns the DEK for (keyID, version). Cache → DB → unwrap.
func (m *manager) Resolve(ctx context.Context, keyID codec.KeyID, version uint32) (codec.Key, error) {
	if err := m.configured(); err != nil {
		return codec.Key{}, err
	}
	if material, ok := m.cache.Get(CacheKey{KeyID: keyID, Version: version}); ok {
		return material.AsCodecKey(keyID, version), nil
	}
	r, err := m.store.selectByID(ctx, keyID, version)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return codec.Key{}, oops.Code("DEK_NOT_FOUND").
				With("key_id", uint64(keyID)).
				With("version", version).
				Errorf("crypto_keys row %d v%d not found", keyID, version)
		}
		return codec.Key{}, oops.Code("DEK_STORE_SELECT_FAILED").Wrap(err)
	}
	return m.unwrapAndCache(ctx, r)
}

// Participants returns the participant list for the row identified by
// (keyID, version). Reads via store.selectByID — same path as Resolve —
// but returns the Participants field rather than unwrapping the DEK.
// AuthGuard never holds DEK material; ParticipantLookup is the right
// boundary.
//
// Two-SELECT note: AuthGuard.Check calls Participants, then on permit
// calls Resolve. Resolve hits the cache; Participants does NOT.
// Phase 3b accepts this redundancy; Phase 3c (holomush-ojw1.3) revisits
// caching policy. TOCTOU concern: if Rotate happens between the two
// calls, AuthGuard checks new participants but Resolve returns the
// (now-stale) cached old key. Phase 3c's cache invalidation closes
// this; Phase 3b's Rotate is stubbed (lifecycle ops are Phase 4),
// so the TOCTOU is vacuous in 3b production.
func (m *manager) Participants(ctx context.Context, keyID codec.KeyID, version uint32) ([]Participant, error) {
	if err := m.configured(); err != nil {
		return nil, err
	}
	r, err := m.store.selectByID(ctx, keyID, version)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.Code("DEK_NOT_FOUND").
				With("key_id", uint64(keyID)).
				With("version", version).
				Errorf("crypto_keys row %d v%d not found", keyID, version)
		}
		return nil, oops.Code("DEK_STORE_SELECT_FAILED").Wrap(err)
	}
	return r.Participants, nil
}

// Add lands in Phase 4 (epic holomush-fi0n).
func (m *manager) Add(_ context.Context, _ ContextID, _ Participant) error {
	return oops.Code("DEK_ADD_NOT_IMPLEMENTED").
		With("tracking_bead", "holomush-fi0n").
		With("phase", 4).
		Errorf("Manager.Add lands in Phase 4 (epic holomush-fi0n)")
}

// Rotate lands in Phase 4 (epic holomush-fi0n).
func (m *manager) Rotate(_ context.Context, _ ContextID, _ []Participant, _ string) error {
	return oops.Code("DEK_ROTATE_NOT_IMPLEMENTED").
		With("tracking_bead", "holomush-fi0n").
		With("phase", 4).
		Errorf("Manager.Rotate lands in Phase 4 (epic holomush-fi0n)")
}

// Rekey lands in Phase 5 (epic holomush-jxo8).
func (m *manager) Rekey(_ context.Context, _ ContextID, _ string, _ OperatorFactors) error {
	return oops.Code("DEK_REKEY_NOT_IMPLEMENTED").
		With("tracking_bead", "holomush-jxo8").
		With("phase", 5).
		Errorf("Manager.Rekey lands in Phase 5 (epic holomush-jxo8)")
}

func (m *manager) unwrapAndCache(ctx context.Context, r row) (codec.Key, error) {
	dekBytes, err := m.provider.Unwrap(ctx, r.WrappedDEK, r.WrapKeyID)
	if err != nil {
		return codec.Key{}, oops.Code("DEK_UNWRAP_FAILED").
			With("key_id", r.ID).
			With("version", r.Version).
			Wrap(err)
	}
	if err := validateProviderUnwrapOutput(dekBytes, r.ID, r.Version); err != nil {
		return codec.Key{}, err
	}
	material := NewMaterial(dekBytes)
	keyID := codec.KeyID(r.ID) //nolint:gosec // G115: r.ID is a DB BIGSERIAL value; positive serial ids fit in uint64.
	m.cache.Put(CacheKey{KeyID: keyID, Version: r.Version}, material)
	return material.AsCodecKey(keyID, r.Version), nil
}

// validateProviderWrapOutput rejects malformed Wrap return values.
// kek.Provider's interface contract is silent on length/non-emptiness;
// without these checks a buggy or future provider could insert an
// unreadable crypto_keys row (wrapped == nil, kekKeyID == ""). Bail
// before the INSERT.
func validateProviderWrapOutput(wrapped []byte, kekKeyID string) error {
	if len(wrapped) == 0 {
		return oops.Code("DEK_WRAP_OUTPUT_INVALID").
			With("reason", "wrapped_empty").
			Errorf("kek.Provider.Wrap returned an empty wrapped DEK")
	}
	if kekKeyID == "" {
		return oops.Code("DEK_WRAP_OUTPUT_INVALID").
			With("reason", "kek_key_id_empty").
			Errorf("kek.Provider.Wrap returned an empty kek_key_id")
	}
	return nil
}

// validateProviderUnwrapOutput rejects malformed Unwrap return values
// (wrong length DEK material). DEKs are 32 bytes (chacha20poly1305 key
// size); a different length means the row was written by an
// incompatible codec or the provider corrupted the unwrap.
func validateProviderUnwrapOutput(dekBytes []byte, rowID int64, version uint32) error {
	if len(dekBytes) != DEKByteLength {
		return oops.Code("DEK_UNWRAP_OUTPUT_INVALID").
			With("key_id", rowID).
			With("version", version).
			With("expected_bytes", DEKByteLength).
			With("got_bytes", len(dekBytes)).
			Errorf("provider.Unwrap returned %d bytes; expected %d", len(dekBytes), DEKByteLength)
	}
	return nil
}
