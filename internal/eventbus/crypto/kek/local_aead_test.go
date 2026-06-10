// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package kek_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/pkg/errutil"
)

// TestLocalAEADProvider_RotateKEK_StubReturnsTrackingBead verifies the
// Phase 2 RotateKEK stub returns the documented tracking_bead context
// pointing at the Phase 4 epic. This guarantees ops surface a real
// bead ID in error logs the moment a caller hits the stub.
func TestLocalAEADProvider_RotateKEK_StubReturnsTrackingBead(t *testing.T) {
	ctx := context.Background()
	provider, err := kek.NewLocalAEADProviderForUnitTest(ctx, envSourceWith(t, newKEKBytes(t)))
	require.NoError(t, err)

	_, err = provider.RotateKEK(ctx)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "KEK_ROTATE_NOT_IMPLEMENTED")
	errutil.AssertErrorContext(t, err, "tracking_bead", "holomush-fi0n")
	errutil.AssertErrorContext(t, err, "phase", 4)
}

// TestNoneProvider_RotateKEK_Refuses verifies the NoneProvider's
// RotateKEK refusal path.
func TestNoneProvider_RotateKEK_Refuses(t *testing.T) {
	provider := kek.NewNoneProviderForUnitTest()
	_, err := provider.RotateKEK(context.Background())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CRYPTO_NONE_PROVIDER_ROTATE_REFUSED")
}

// TestNewLocalAEADProviderForUnitTest_RejectsNilSource verifies the
// constructor surfaces a typed startup error rather than panicking on
// the first source.Load call.
func TestNewLocalAEADProviderForUnitTest_RejectsNilSource(t *testing.T) {
	_, err := kek.NewLocalAEADProviderForUnitTest(context.Background(), nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "KEK_LOCAL_AEAD_DEPENDENCY_NIL")
	errutil.AssertErrorContext(t, err, "dependency", "source")
}

func newKEKBytes(t *testing.T) []byte {
	t.Helper()
	b := make([]byte, kek.KEKByteLength)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return b
}

// envSourceWith returns an EnvSource backed by a per-test env var.
// kekBytes is hex-encoded into the env var to satisfy EnvSource's
// strict-hex parser.
func envSourceWith(t *testing.T, kekBytes []byte) *kek.EnvSource {
	t.Helper()
	name := "TEST_KEK_" + sanitizeTestName(t.Name())
	t.Setenv(name, hex.EncodeToString(kekBytes))
	return kek.NewEnvSource(name, false)
}

// sanitizeTestName strips characters that env var names disallow
// (slashes from t.Run subtest names, etc.).
func sanitizeTestName(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '_':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}

// Verifies: INV-CRYPTO-17
func TestLocalAEADProvider_WrapUnwrap_Roundtrip(t *testing.T) {
	// INV-CRYPTO-17: Wrap then Unwrap recovers the original DEK byte-for-byte.
	ctx := context.Background()
	kekBytes := newKEKBytes(t)
	provider, err := kek.NewLocalAEADProviderForUnitTest(ctx, envSourceWith(t, kekBytes))
	require.NoError(t, err)

	dek := newKEKBytes(t) // any 32 bytes
	wrapped, kekKeyID, err := provider.Wrap(ctx, dek)
	require.NoError(t, err)
	require.NotEmpty(t, kekKeyID)
	require.NotEqual(t, dek, wrapped)

	unwrapped, err := provider.Unwrap(ctx, wrapped, kekKeyID)
	require.NoError(t, err)
	assert.Equal(t, dek, unwrapped)
}

func TestLocalAEADProvider_Unwrap_TamperedWrappedBytes_Fails(t *testing.T) {
	ctx := context.Background()
	kekBytes := newKEKBytes(t)
	provider, err := kek.NewLocalAEADProviderForUnitTest(ctx, envSourceWith(t, kekBytes))
	require.NoError(t, err)

	wrapped, kekKeyID, err := provider.Wrap(ctx, newKEKBytes(t))
	require.NoError(t, err)

	// Flip a bit in the ciphertext.
	wrapped[len(wrapped)/2] ^= 0xFF

	_, err = provider.Unwrap(ctx, wrapped, kekKeyID)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "KEK_UNWRAP_AEAD_TAG_MISMATCH")
}

func TestLocalAEADProvider_Unwrap_WithUnknownKEKKeyID_Fails(t *testing.T) {
	ctx := context.Background()
	kekBytes := newKEKBytes(t)
	provider, err := kek.NewLocalAEADProviderForUnitTest(ctx, envSourceWith(t, kekBytes))
	require.NoError(t, err)

	wrapped, _, err := provider.Wrap(ctx, newKEKBytes(t))
	require.NoError(t, err)

	_, err = provider.Unwrap(ctx, wrapped, "totally-different-kek-id")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "KEK_UNWRAP_KEY_ID_UNKNOWN")
}

func TestLocalAEADProvider_Name_DerivesFromSource(t *testing.T) {
	ctx := context.Background()
	provider, err := kek.NewLocalAEADProviderForUnitTest(ctx, envSourceWith(t, newKEKBytes(t)))
	require.NoError(t, err)
	assert.Equal(t, "local-aead/env", provider.Name())
}

func TestLocalAEADProvider_HealthCheck_Succeeds(t *testing.T) {
	ctx := context.Background()
	provider, err := kek.NewLocalAEADProviderForUnitTest(ctx, envSourceWith(t, newKEKBytes(t)))
	require.NoError(t, err)
	assert.NoError(t, provider.HealthCheck(ctx))
}
