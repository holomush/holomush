// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package kek

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"

	"github.com/samber/oops"
	"golang.org/x/crypto/chacha20poly1305"
)

// LocalAEADProvider does Wrap/Unwrap locally using a master KEK
// fetched from a pluggable KEKSource. The KEK lives in process memory
// for the lifetime of the provider; loadable on construction and on
// RotateKEK.
//
// kekKeyID is sha256(KEK) as 64 hex chars — a deterministic
// fingerprint of the KEK material. Stable across restarts as long as
// the KEK material does not change.
type LocalAEADProvider struct {
	source          KEKSource
	sourceName      string
	currentKEKKeyID string
	// kekByID maps fingerprint → KEK bytes. After RotateKEK the old
	// KEK is retained for the lifetime of the rotation operation;
	// Phase 2 doesn't ship rotation, so this map has at most one
	// entry. Phase 4+ may grow it.
	kekByID map[string][]byte
}

// NewLocalAEADProvider constructs a LocalAEADProvider, loading the KEK
// from the source and running INV-CRYPTO-19 against db (refuses startup if
// any crypto_keys row references a wrap_key_id this provider cannot
// unwrap). Pass a *pgx.Conn or *pgxpool.Pool for db.
//
// Returns KEK_LOCAL_AEAD_DEPENDENCY_NIL if source or db is nil, so a
// wiring bug surfaces as a typed startup error rather than a panic on
// the first Wrap/Unwrap call.
func NewLocalAEADProvider(ctx context.Context, source KEKSource, db PGQuerier) (*LocalAEADProvider, error) {
	if source == nil {
		return nil, oops.Code("KEK_LOCAL_AEAD_DEPENDENCY_NIL").
			With("dependency", "source").
			Errorf("NewLocalAEADProvider requires a non-nil KEKSource")
	}
	if db == nil {
		return nil, oops.Code("KEK_LOCAL_AEAD_DEPENDENCY_NIL").
			With("dependency", "db").
			Errorf("NewLocalAEADProvider requires a non-nil PGQuerier")
	}
	p, err := buildLocalAEADProvider(ctx, source)
	if err != nil {
		return nil, err
	}
	if err := p.startupIntegrityCheck(ctx, db); err != nil {
		return nil, err
	}
	return p, nil
}

// NewLocalAEADProviderForUnitTest constructs a LocalAEADProvider
// without the INV-CRYPTO-19 DB check. For unit tests of Wrap/Unwrap;
// integration tests use NewLocalAEADProvider.
//
// Returns KEK_LOCAL_AEAD_DEPENDENCY_NIL if source is nil.
func NewLocalAEADProviderForUnitTest(ctx context.Context, source KEKSource) (*LocalAEADProvider, error) {
	if source == nil {
		return nil, oops.Code("KEK_LOCAL_AEAD_DEPENDENCY_NIL").
			With("dependency", "source").
			Errorf("NewLocalAEADProviderForUnitTest requires a non-nil KEKSource")
	}
	return buildLocalAEADProvider(ctx, source)
}

func buildLocalAEADProvider(ctx context.Context, source KEKSource) (*LocalAEADProvider, error) {
	kekBytes, err := source.Load(ctx)
	if err != nil {
		return nil, oops.Code("KEK_SOURCE_LOAD_FAILED").
			With("source", source.Name()).
			Wrap(err)
	}
	if len(kekBytes) != KEKByteLength {
		return nil, oops.Code("KEK_BYTE_LENGTH_INVALID").
			With("source", source.Name()).
			With("expected", KEKByteLength).
			With("got", len(kekBytes)).
			Errorf("KEK from %s must be %d bytes; got %d", source.Name(), KEKByteLength, len(kekBytes))
	}
	// Defensive copy: KEKSource implementations are pluggable and may
	// reuse or mutate their internal buffer after Load returns. Owning
	// our own slice prevents a misbehaving source from silently
	// corrupting wrap/unwrap once we've cached the fingerprint.
	owned := make([]byte, len(kekBytes))
	copy(owned, kekBytes)
	fingerprint := fingerprintKEK(owned)
	return &LocalAEADProvider{
		source:          source,
		sourceName:      source.Name(),
		currentKEKKeyID: fingerprint,
		kekByID:         map[string][]byte{fingerprint: owned},
	}, nil
}

// Name returns the source's name (e.g., "local-aead/env").
func (p *LocalAEADProvider) Name() string { return p.sourceName }

// Wrap encrypts dek under the current KEK using XChaCha20-Poly1305.
// kekKeyID is the current KEK fingerprint.
func (p *LocalAEADProvider) Wrap(_ context.Context, dek []byte) (wrapped []byte, kekKeyID string, err error) {
	aead, aeadErr := chacha20poly1305.NewX(p.kekByID[p.currentKEKKeyID])
	if aeadErr != nil {
		return nil, "", oops.Code("KEK_AEAD_CONSTRUCT_FAILED").Wrap(aeadErr)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, rngErr := io.ReadFull(rand.Reader, nonce); rngErr != nil {
		return nil, "", oops.Code("KEK_WRAP_RNG_FAILED").Wrap(rngErr)
	}
	sealed := aead.Seal(nil, nonce, dek, nil)
	// Wrapped layout: nonce || sealed (sealed includes the AEAD tag).
	out := make([]byte, 0, len(nonce)+len(sealed))
	out = append(out, nonce...)
	out = append(out, sealed...)
	return out, p.currentKEKKeyID, nil
}

// Unwrap decrypts wrapped using the KEK identified by kekKeyID.
func (p *LocalAEADProvider) Unwrap(_ context.Context, wrapped []byte, kekKeyID string) ([]byte, error) {
	kekBytes, ok := p.kekByID[kekKeyID]
	if !ok {
		return nil, oops.Code("KEK_UNWRAP_KEY_ID_UNKNOWN").
			With("kek_key_id", kekKeyID).
			With("source", p.sourceName).
			Errorf("provider does not hold KEK with fingerprint %q", kekKeyID)
	}
	aead, err := chacha20poly1305.NewX(kekBytes)
	if err != nil {
		return nil, oops.Code("KEK_AEAD_CONSTRUCT_FAILED").Wrap(err)
	}
	if len(wrapped) < aead.NonceSize() {
		return nil, oops.Code("KEK_WRAPPED_TOO_SHORT").
			With("min_size", aead.NonceSize()).
			With("got_size", len(wrapped)).
			Errorf("wrapped DEK shorter than nonce size")
	}
	nonce := wrapped[:aead.NonceSize()]
	sealed := wrapped[aead.NonceSize():]
	dek, err := aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, oops.Code("KEK_UNWRAP_AEAD_TAG_MISMATCH").
			With("kek_key_id", kekKeyID).
			Errorf("AEAD open failed — wrapped DEK tampered or wrong KEK")
	}
	return dek, nil
}

// RotateKEK is a Phase 4+ surface. Phase 2 ships a stub that returns
// an unimplemented error pointing at the Phase 4 epic.
func (p *LocalAEADProvider) RotateKEK(_ context.Context) (string, error) {
	return "", oops.Code("KEK_ROTATE_NOT_IMPLEMENTED").
		With("tracking_bead", "holomush-fi0n").
		With("phase", 4).
		Errorf("LocalAEADProvider.RotateKEK lands in Phase 4 (epic holomush-fi0n)")
}

// HealthCheck returns nil — the KEK is in process memory.
func (p *LocalAEADProvider) HealthCheck(_ context.Context) error { return nil }

// startupIntegrityCheck enforces INV-CRYPTO-19: no crypto_keys row may
// reference a wrap_key_id this provider cannot unwrap.
func (p *LocalAEADProvider) startupIntegrityCheck(ctx context.Context, db PGQuerier) error {
	rows, err := db.Query(ctx, "SELECT DISTINCT wrap_key_id FROM crypto_keys WHERE wrap_provider = $1", p.sourceName)
	if err != nil {
		return oops.Code("KEK_PROVIDER_INTEGRITY_QUERY_FAILED").Wrap(err)
	}
	defer rows.Close()
	var unrecoverable []string
	for rows.Next() {
		var kid string
		if scanErr := rows.Scan(&kid); scanErr != nil {
			return oops.Code("KEK_PROVIDER_INTEGRITY_QUERY_FAILED").Wrap(scanErr)
		}
		if _, ok := p.kekByID[kid]; !ok {
			unrecoverable = append(unrecoverable, kid)
		}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return oops.Code("KEK_PROVIDER_INTEGRITY_QUERY_FAILED").Wrap(rowsErr)
	}
	if len(unrecoverable) > 0 {
		return oops.Code("KEK_PROVIDER_CANNOT_UNWRAP_EXISTING_DEKS").
			With("source", p.sourceName).
			With("unrecoverable_kek_key_ids", unrecoverable).
			Errorf("provider cannot unwrap %d existing crypto_keys rows; "+
				"the master KEK has changed since those rows were written. "+
				"Restore the original KEK or run `holomush crypto provider-migrate` (Phase 6).",
				len(unrecoverable))
	}
	return nil
}

func fingerprintKEK(kekBytes []byte) string {
	sum := sha256.Sum256(kekBytes)
	return hex.EncodeToString(sum[:])
}
