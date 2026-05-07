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

// Invalidator publishes a cache-invalidation request to all replicas.
// action is one of "rotate", "participants_changed", or "rekey".
type Invalidator func(ctx context.Context, ctxID ContextID, action string, version, successorVersion uint32) error

// BindingResolver resolves a character's current binding_id.
type BindingResolver interface {
	Current(ctx context.Context, characterID string) (string, error)
}

// manager is the concrete impl.
type manager struct {
	provider   kek.Provider
	store      *Store
	cache      *Cache
	partCache  *ParticipantsCache
	invalidate Invalidator
	bindings   BindingResolver
}

// NewManager constructs a real Manager. Production callers (Phase 3+)
// pass a real KEK provider, pgxpool.Pool-backed Store, DEK material
// Cache, and participants Cache. All four collaborators are required;
// a nil argument returns DEK_MANAGER_DEPENDENCY_NIL rather than
// letting GetOrCreate/Resolve/Participants dereference nil at runtime.
func NewManager(
	provider kek.Provider,
	store *Store,
	cache *Cache,
	partCache *ParticipantsCache,
	invalidate Invalidator,
	bindings BindingResolver,
) (Manager, error) {
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
	case partCache == nil:
		return nil, oops.Code("DEK_MANAGER_DEPENDENCY_NIL").
			With("dependency", "partCache").
			Errorf("dek.NewManager requires a non-nil *ParticipantsCache")
	case invalidate == nil:
		return nil, oops.Code("DEK_MANAGER_DEPENDENCY_NIL").
			With("dependency", "invalidate").
			Errorf("dek.NewManager requires a non-nil Invalidator")
	case bindings == nil:
		return nil, oops.Code("DEK_MANAGER_DEPENDENCY_NIL").
			With("dependency", "bindings").
			Errorf("dek.NewManager requires a non-nil BindingResolver")
	}
	return &manager{
		provider: provider, store: store, cache: cache,
		partCache: partCache, invalidate: invalidate, bindings: bindings,
	}, nil
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
	if m.provider == nil || m.store == nil || m.cache == nil || m.partCache == nil {
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

	// Seed both caches: the DEK material cache for Resolve and the
	// participants cache for Participants. Both are derived from the
	// freshly minted row without an extra PG read.
	m.cache.Put(CacheKey{KeyID: keyID, Version: 1}, ctxID, material)
	m.partCache.Put(
		ParticipantsCacheKey{ContextType: ctxID.Type, ContextID: ctxID.ID, Version: 1},
		initial,
	)
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

// Participants returns the participant set for the (keyID, version)
// DEK. Reads from ParticipantsCache on hit; on miss, falls through to
// PG and seeds the cache. Phase 3c grounding doc Decision 3 + INV-59.
//
// PG-before-cache note: this body reads PG first (selectByID) to derive
// the cache key (ContextType, ContextID, Version), then checks the
// cache. The (keyID, version) → (ctxType, ctxID) mapping isn't
// available in-memory today, so the row read is required. unwrapAndCache
// seeds the cache on the Resolve / GetOrCreate paths so most reads still
// hit cache; Participants itself only avoids a redundant participants-
// list copy when the row is already cached. A future "(keyID, version)
// → (ctxType, ctxID)" reverse index would lift the PG read; out of
// scope for T7.
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
	pck := ParticipantsCacheKey{ContextType: r.ContextType, ContextID: r.ContextID, Version: version}
	if cached, ok := m.partCache.Get(pck); ok {
		return cached, nil
	}
	m.partCache.Put(pck, r.Participants)
	return r.Participants, nil
}

// Add appends a participant to the active DEK's set without rotating.
func (m *manager) Add(ctx context.Context, ctxID ContextID, p Participant) error {
	if err := m.configured(); err != nil {
		return err
	}
	if m.invalidate == nil {
		return oops.Code("DEK_MANAGER_DEPENDENCY_NIL").
			With("dependency", "Invalidator").
			Errorf("Add requires a non-nil Invalidator — pass invalidation.Coordinator adapter")
	}
	if m.bindings == nil {
		return oops.Code("DEK_MANAGER_DEPENDENCY_NIL").
			With("dependency", "BindingResolver").
			Errorf("Add requires a non-nil BindingResolver")
	}

	if p.BindingID == "" {
		bindingID, err := m.bindings.Current(ctx, p.CharacterID)
		if err != nil {
			return oops.Code("DEK_BINDING_RESOLVE_FAILED").
				With("character_id", p.CharacterID).Wrap(err)
		}
		p.BindingID = bindingID
	}

	activeRow, err := m.store.updateParticipants(ctx, ctxID, p)
	if err != nil {
		return err
	}

	return m.invalidate(ctx, ctxID, "participants_changed", activeRow.Version, 0)
}

// Rotate mints a new DEK version and marks the old one rotated.
func (m *manager) Rotate(ctx context.Context, ctxID ContextID,
	newParticipants []Participant, _ string) error {

	if err := m.configured(); err != nil {
		return err
	}
	if m.invalidate == nil {
		return oops.Code("DEK_MANAGER_DEPENDENCY_NIL").
			With("dependency", "Invalidator").
			Errorf("Rotate requires a non-nil Invalidator")
	}

	activeRow, err := m.store.selectActive(ctx, ctxID)
	if err != nil {
		return err
	}

	dekBytes := make([]byte, DEKByteLength)
	if _, err = io.ReadFull(rand.Reader, dekBytes); err != nil {
		return oops.Code("DEK_RNG_FAILED").Wrap(err)
	}
	wrapped, kekKeyID, err := m.provider.Wrap(ctx, dekBytes)
	if err != nil {
		return oops.Code("DEK_WRAP_FAILED").Wrap(err)
	}
	err = validateProviderWrapOutput(wrapped, kekKeyID)
	if err != nil {
		return err
	}

	newRow := row{
		ContextType: ctxID.Type, ContextID: ctxID.ID,
		Version:      activeRow.Version + 1,
		WrappedDEK:   wrapped,
		WrapProvider: m.provider.Name(),
		WrapKeyID:    kekKeyID,
		Participants: newParticipants,
	}
	newID, err := m.store.insert(ctx, newRow)
	if err != nil {
		return oops.Code("DEK_STORE_INSERT_FAILED").Wrap(err)
	}

	//nolint:gosec // G115: newID is a DB BIGSERIAL value
	newKeyID := codec.KeyID(newID)
	newVersion := newRow.Version

	material := NewMaterial(dekBytes)
	m.cache.Put(CacheKey{KeyID: newKeyID, Version: newVersion}, ctxID, material)
	m.partCache.Put(ParticipantsCacheKey{
		ContextType: ctxID.Type, ContextID: ctxID.ID, Version: newVersion,
	}, newParticipants)

	if err := m.invalidate(ctx, ctxID, "rotate",
		activeRow.Version, newVersion); err != nil {
		// Rollback: evict caches + mark new row destroyed.
		m.cache.Invalidate(CacheKey{KeyID: newKeyID, Version: newVersion})
		m.partCache.Invalidate(ParticipantsCacheKey{
			ContextType: ctxID.Type, ContextID: ctxID.ID, Version: newVersion,
		})
		//nolint:errcheck // best-effort rollback; don't mask the original error
		_ = m.store.markDestroyed(ctx, newKeyID, newVersion)
		return err
	}

	return m.store.markRotated(ctx,
		//nolint:gosec // G115: activeRow.ID is a DB BIGSERIAL value
		codec.KeyID(activeRow.ID), activeRow.Version, newID)
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
	ctxID := ContextID{Type: r.ContextType, ID: r.ContextID}

	// Seed both caches from the single PG row read so subsequent
	// Resolve and Participants calls hit cache without re-reading PG.
	m.cache.Put(CacheKey{KeyID: keyID, Version: r.Version}, ctxID, material)
	m.partCache.Put(
		ParticipantsCacheKey{ContextType: r.ContextType, ContextID: r.ContextID, Version: r.Version},
		r.Participants,
	)
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
