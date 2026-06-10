// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package source_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/history/source"
)

type fakeColdReader struct {
	env       eventbus.Envelope
	found     bool
	lookupErr error
}

func (f *fakeColdReader) LookupByID(_ context.Context, _ eventbus.EventID) (eventbus.Envelope, bool, error) {
	return f.env, f.found, f.lookupErr
}

type dekResolverFn func(codec.KeyID, uint32) (codec.Key, error)

type fakeDEKManager struct {
	fn dekResolverFn
}

func (f *fakeDEKManager) Resolve(_ context.Context, k codec.KeyID, v uint32) (codec.Key, error) {
	return f.fn(k, v)
}

// Unused dek.Manager methods — panic if called (same pattern as stubDEKManager).
func (f *fakeDEKManager) GetOrCreate(context.Context, dek.ContextID, []dek.Participant) (codec.Key, error) {
	panic("unused")
}

func (f *fakeDEKManager) Participants(context.Context, codec.KeyID, uint32) ([]dek.Participant, error) {
	panic("unused")
}

func (f *fakeDEKManager) Add(context.Context, dek.ContextID, dek.Participant) error { panic("unused") }

func (f *fakeDEKManager) EnsureParticipant(context.Context, dek.ContextID, dek.Participant) error {
	panic("unused")
}

func (f *fakeDEKManager) Rotate(context.Context, dek.ContextID, []dek.Participant, string) error {
	panic("unused")
}

func (f *fakeDEKManager) Rekey(context.Context, dek.ContextID, string, dek.OperatorFactors) error {
	panic("unused")
}

func (f *fakeDEKManager) ActiveDEKRow(_ context.Context, _ dek.ContextID) (dek.ActiveDEKRecord, error) {
	panic("unused")
}

func (f *fakeDEKManager) MintNewDEKForRekey(context.Context, int64) (int64, error) {
	panic("unused")
}

func (f *fakeDEKManager) DestroyDEK(context.Context, int64) error { panic("unused") }

func (f *fakeDEKManager) EvictCachedDEK(context.Context, int64) error { panic("unused") }

func newTestMetrics() *source.Metrics {
	reg := prometheus.NewRegistry()
	return source.NewMetricsForTest(reg)
}

func makeCiphertextEnvelope(t *testing.T, keyID codec.KeyID, version uint32) eventbus.Envelope {
	t.Helper()
	return eventbus.NewEnvelopeForTest(eventbus.EnvelopeFields{
		EventID:    ulid.MustParse("01HXY000000000000000000001"),
		Codec:      codec.NameXChaCha20v1,
		KeyID:      keyID,
		KeyVersion: version,
	})
}

// Case 1: identity codec, no encryption.
func TestFallback_Case1_IdentityCodec_BypassesResolve(t *testing.T) {
	env := eventbus.NewEnvelopeForTest(eventbus.EnvelopeFields{Codec: codec.NameIdentity})
	r := source.NewFallbackResolver(&fakeDEKManager{}, &fakeColdReader{}, newTestMetrics(), slog.Default())
	got, err := r.Resolve(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, source.TierHot, got.SourceTier)
}

// Case 2: ciphertext, hot DEK present.
func TestFallback_Case2_HotDEKPresent(t *testing.T) {
	env := makeCiphertextEnvelope(t, 42, 3)
	dm := &fakeDEKManager{fn: func(k codec.KeyID, _ uint32) (codec.Key, error) {
		require.Equal(t, codec.KeyID(42), k)
		return codec.Key{ID: 1, Version: 1, Bytes: []byte{0xAA}}, nil
	}}
	r := source.NewFallbackResolver(dm, &fakeColdReader{}, newTestMetrics(), slog.Default())
	got, err := r.Resolve(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, source.TierHot, got.SourceTier)
	require.Equal(t, codec.KeyID(42), got.KeyID)
}

// Case 3: Rekey-destroyed hot DEK, cold present + resolvable.
func TestFallback_Case3_ColdFallbackSuccess(t *testing.T) {
	env := makeCiphertextEnvelope(t, 42, 3)
	coldEnv := makeCiphertextEnvelope(t, 99, 4)
	dm := &fakeDEKManager{fn: func(k codec.KeyID, _ uint32) (codec.Key, error) {
		if k == 42 {
			return codec.Key{}, oops.Code("DEK_DESTROYED").Errorf("rekey'd")
		}
		return codec.Key{ID: 2, Version: 1, Bytes: []byte{0xBB}}, nil
	}}
	cr := &fakeColdReader{env: coldEnv, found: true}
	r := source.NewFallbackResolver(dm, cr, newTestMetrics(), slog.Default())
	got, err := r.Resolve(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, source.TierColdFallback, got.SourceTier)
	require.Equal(t, codec.KeyID(99), got.KeyID)
}

// Case 4: hot destroyed, cold present, cold DEK also missing.
func TestFallback_Case4_ColdDEKAlsoMissing(t *testing.T) {
	env := makeCiphertextEnvelope(t, 42, 3)
	coldEnv := makeCiphertextEnvelope(t, 99, 4)
	dm := &fakeDEKManager{fn: func(codec.KeyID, uint32) (codec.Key, error) {
		return codec.Key{}, oops.Code("DEK_DESTROYED").Errorf("both gone")
	}}
	cr := &fakeColdReader{env: coldEnv, found: true}
	r := source.NewFallbackResolver(dm, cr, newTestMetrics(), slog.Default())
	_, err := r.Resolve(context.Background(), env)
	require.ErrorIs(t, err, source.ErrMetadataOnly)
}

// Case 5: hot destroyed, no cold row.
func TestFallback_Case5_NoColdRow(t *testing.T) {
	env := makeCiphertextEnvelope(t, 42, 3)
	dm := &fakeDEKManager{fn: func(codec.KeyID, uint32) (codec.Key, error) {
		return codec.Key{}, oops.Code("DEK_DESTROYED").Errorf("gone")
	}}
	r := source.NewFallbackResolver(dm, &fakeColdReader{found: false}, newTestMetrics(), slog.Default())
	_, err := r.Resolve(context.Background(), env)
	require.ErrorIs(t, err, source.ErrMetadataOnly)
}

// Case 6: DEK_NOT_FOUND (orphan ref), no cold row.
func TestFallback_Case6_OrphanRef_NoCold(t *testing.T) {
	env := makeCiphertextEnvelope(t, 42, 3)
	dm := &fakeDEKManager{fn: func(codec.KeyID, uint32) (codec.Key, error) {
		return codec.Key{}, oops.Code("DEK_NOT_FOUND").Errorf("orphan")
	}}
	r := source.NewFallbackResolver(dm, &fakeColdReader{found: false}, newTestMetrics(), slog.Default())
	_, err := r.Resolve(context.Background(), env)
	require.ErrorIs(t, err, source.ErrMetadataOnly)
}

// Case 7: DB transient error propagates.
func TestFallback_Case7_TransientError_Propagates(t *testing.T) {
	env := makeCiphertextEnvelope(t, 42, 3)
	transient := errors.New("connection reset")
	dm := &fakeDEKManager{fn: func(codec.KeyID, uint32) (codec.Key, error) {
		return codec.Key{}, transient
	}}
	r := source.NewFallbackResolver(dm, &fakeColdReader{}, newTestMetrics(), slog.Default())
	_, err := r.Resolve(context.Background(), env)
	require.Error(t, err)
	require.NotErrorIs(t, err, source.ErrMetadataOnly)
}

// Case 8: cold reader transient error.
func TestFallback_Case8_ColdReaderError_Wrapped(t *testing.T) {
	env := makeCiphertextEnvelope(t, 42, 3)
	dm := &fakeDEKManager{fn: func(codec.KeyID, uint32) (codec.Key, error) {
		return codec.Key{}, oops.Code("DEK_DESTROYED").Errorf("rekey'd")
	}}
	cr := &fakeColdReader{lookupErr: errors.New("cold tier down")}
	r := source.NewFallbackResolver(dm, cr, newTestMetrics(), slog.Default())
	_, err := r.Resolve(context.Background(), env)
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "cold lookup error MUST be wrapped as oops error")
	assert.Equal(t, "EVENTBUS_SOURCE_COLD_LOOKUP_FAILED", oopsErr.Code())
}

// Case 9: cold DEK resolution returns a transient (non-typed) error.
// The error MUST propagate as DEK_RESOLVE_TRANSIENT, NOT be masked as ErrMetadataOnly.
func TestFallback_Case9_ColdTransientError_Propagates(t *testing.T) {
	env := makeCiphertextEnvelope(t, 42, 3)
	coldEnv := makeCiphertextEnvelope(t, 99, 4)
	transient := errors.New("pg: connection reset by peer")
	dm := &fakeDEKManager{fn: func(k codec.KeyID, _ uint32) (codec.Key, error) {
		if k == 42 {
			// Hot DEK is destroyed — triggers cold-tier fallback.
			return codec.Key{}, oops.Code("DEK_DESTROYED").Errorf("rekey'd")
		}
		// Cold DEK resolution hits a transient DB error (not typed as DEK_NOT_FOUND / DEK_DESTROYED).
		return codec.Key{}, transient
	}}
	cr := &fakeColdReader{env: coldEnv, found: true}
	r := source.NewFallbackResolver(dm, cr, newTestMetrics(), slog.Default())
	_, err := r.Resolve(context.Background(), env)
	require.Error(t, err)
	require.NotErrorIs(t, err, source.ErrMetadataOnly, "transient cold-DEK error MUST NOT be masked as ErrMetadataOnly")
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok, "transient cold-DEK error MUST be wrapped as oops error")
	assert.Equal(t, "DEK_RESOLVE_TRANSIENT", oopsErr.Code(), "transient cold-DEK error MUST carry DEK_RESOLVE_TRANSIENT code")
}
