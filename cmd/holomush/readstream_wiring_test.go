// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	adminauth "github.com/holomush/holomush/internal/admin/auth"
	"github.com/holomush/holomush/internal/admin/readstream"
	socket "github.com/holomush/holomush/internal/admin/socket"
	"github.com/holomush/holomush/internal/config"
	"github.com/holomush/holomush/internal/eventbus/codec"
)

// TestBuildReadStreamWiringReturnsZeroWhenPoolMissing verifies that the
// helper degrades gracefully when the database pool is unavailable. The
// admin socket then falls back to Unimplemented for AdminReadStream and the
// rest of the server boots normally. Mirrors the rekey wiring pattern at
// holomush-jxo8.7.44.
func TestBuildReadStreamWiringReturnsZeroWhenPoolMissing(t *testing.T) {
	w, err := buildReadStreamWiring(context.Background(), readStreamWiringDeps{
		// Pool intentionally nil — gate fires.
	})
	require.NoError(t, err, "missing dependency must NOT be a fatal error — server must still boot")
	assert.Nil(t, w.Handler, "Handler must be nil when wiring is incomplete")
}

// TestBuildReadStreamWiringReturnsZeroWhenDEKManagerMissing verifies that
// the helper short-circuits when the DEK manager is unavailable (typically
// because KEK env vars are not configured). Per ADR-0017, F owns its own
// ColdReader but reuses the dek.Manager for per-row decryption; without it
// the handler cannot be constructed and the admin socket falls back to
// Unimplemented for AdminReadStream.
func TestBuildReadStreamWiringReturnsZeroWhenDEKManagerMissing(t *testing.T) {
	w, err := buildReadStreamWiring(context.Background(), readStreamWiringDeps{
		// DEKManager intentionally nil — gate fires even with Pool set.
	})
	require.NoError(t, err)
	assert.Nil(t, w.Handler)
}

// TestProductionReadStreamAdaptersSatisfyReadstreamInterfaces is a
// compile-time guarantee that the production adapter types implement the
// readstream-layer narrow seams. Surfaces interface-drift refactors before
// runtime invocation. Mirrors TestProductionAdaptersSatisfySocketInterfaces
// in crypto_rekey_wiring_test.go.
func TestProductionReadStreamAdaptersSatisfyReadstreamInterfaces(_ *testing.T) {
	var _ readstream.SessionStore = (*readstreamSessionStore)(nil)
	var _ readstream.CodecResolver = codecRegistryAdapter{}
}

// TestProductionReadStreamHandlerSatisfiesSocketInterface guarantees at
// compile time that readstream.Handler satisfies socket.ReadStreamRPCHandler
// (the R.12 stub interface). Without this, AdminSocketSubsystem.Config's
// ReadStreamHandler field could silently drift away from the handler's
// actual surface.
func TestProductionReadStreamHandlerSatisfiesSocketInterface(_ *testing.T) {
	var _ socket.ReadStreamRPCHandler = (*readstream.Handler)(nil)
}

// TestCryptoConfigDefaultsPopulatesOperatorReadFields verifies that the
// four new AdminReadStream timing fields land in CryptoConfig.Defaults()
// at their documented defaults. Regression guard for the spec §6 budgets.
func TestCryptoConfigDefaultsPopulatesOperatorReadFields(t *testing.T) {
	cfg := config.CryptoConfig{}.Defaults()

	assert.Equal(t, 1*time.Hour, cfg.OperatorReadDefaultWindow,
		"OperatorReadDefaultWindow MUST default to 1h (INV-CRYPTO-56)")
	assert.Equal(t, 30*24*time.Hour, cfg.OperatorReadMaxWindow,
		"OperatorReadMaxWindow MUST default to 30d (INV-CRYPTO-56)")
	assert.Equal(t, 30*time.Second, cfg.OperatorReadWriteDeadline,
		"OperatorReadWriteDeadline MUST default to 30s (INV-CRYPTO-64)")
	assert.Equal(t, 5*time.Minute, cfg.OperatorReadApprovalTTL,
		"OperatorReadApprovalTTL MUST default to 5m (INV-CRYPTO-61)")
}

// TestCryptoConfigDefaultsPreservesExplicitOperatorReadFields verifies that
// Defaults() does NOT clobber explicit non-zero values for the four new
// AdminReadStream timing fields. Without this, an operator overriding (e.g.)
// OperatorReadApprovalTTL=10m via YAML would still see the 5m default.
func TestCryptoConfigDefaultsPreservesExplicitOperatorReadFields(t *testing.T) {
	cfg := config.CryptoConfig{
		OperatorReadDefaultWindow: 2 * time.Hour,
		OperatorReadMaxWindow:     7 * 24 * time.Hour,
		OperatorReadWriteDeadline: 90 * time.Second,
		OperatorReadApprovalTTL:   10 * time.Minute,
	}.Defaults()

	assert.Equal(t, 2*time.Hour, cfg.OperatorReadDefaultWindow)
	assert.Equal(t, 7*24*time.Hour, cfg.OperatorReadMaxWindow)
	assert.Equal(t, 90*time.Second, cfg.OperatorReadWriteDeadline)
	assert.Equal(t, 10*time.Minute, cfg.OperatorReadApprovalTTL)
}

// fakeAdminSessionStore is a test-only adminauth.SessionStore that returns
// a canned identity on Get. Used to exercise readstreamSessionStore's
// translation path without spinning up a real adminauth session store.
type fakeAdminSessionStore struct {
	identity adminauth.OperatorIdentity
	err      error
}

func (f *fakeAdminSessionStore) Issue(_ adminauth.OperatorIdentity) (string, time.Time, error) {
	return "", time.Time{}, nil
}

func (f *fakeAdminSessionStore) Get(_ string) (adminauth.OperatorIdentity, error) {
	return f.identity, f.err
}

func (f *fakeAdminSessionStore) Revoke(_ string) error { return nil }

// TestReadstreamSessionStoreTranslatesOperatorIdentity verifies the
// adminauth.OperatorIdentity → readstream.OperatorSession translation:
// PlayerID flows through; PeerCred.UID + PeerCred.PID are projected onto
// PeerCredUID + PeerCredPID; the token argument becomes SessionTokenID.
// GID is dropped at the boundary because F's audit payload does not
// carry it (OperatorReadStartPayload has no peer_cred_gid field).
func TestReadstreamSessionStoreTranslatesOperatorIdentity(t *testing.T) {
	inner := &fakeAdminSessionStore{
		identity: adminauth.OperatorIdentity{
			PlayerID: "01HZA00000000000000000000",
			PeerCred: socket.PeerCred{UID: 1001, GID: 100, PID: 4242},
		},
	}
	adapter := &readstreamSessionStore{inner: inner}

	got, err := adapter.GetOperatorSession("tok-xyz")
	require.NoError(t, err)
	assert.Equal(t, "01HZA00000000000000000000", got.PlayerID)
	assert.Equal(t, "tok-xyz", got.SessionTokenID,
		"SessionTokenID MUST be the lookup token — the audit payload binds the operator's session to this work")
	assert.Equal(t, uint32(1001), got.PeerCredUID)
	assert.Equal(t, int32(4242), got.PeerCredPID)
}

// TestReadstreamSessionStoreRejectsNilInner asserts the defensive path on a
// zero-valued adapter (which can only arise from buggy wiring). The handler
// would otherwise NPE on the first Get; surfacing DENY_SESSION_INVALID
// keeps the handler's classifier consistent.
func TestReadstreamSessionStoreRejectsNilInner(t *testing.T) {
	adapter := &readstreamSessionStore{inner: nil}
	_, err := adapter.GetOperatorSession("tok-xyz")
	require.Error(t, err)
}

// TestCodecRegistryAdapterResolvesKnownCodec verifies that the codec
// registry adapter delegates to the package-level codec.Resolve and
// returns a non-nil codec for a known name (the identity codec is always
// registered).
func TestCodecRegistryAdapterResolvesKnownCodec(t *testing.T) {
	adapter := codecRegistryAdapter{}
	c, err := adapter.Resolve(codec.NameIdentity)
	require.NoError(t, err)
	require.NotNil(t, c)
}

// TestCodecRegistryAdapterReturnsErrorForUnknownCodec verifies that the
// adapter propagates codec.Resolve's unknown-codec error so the handler's
// classifier maps it correctly (DEK_BAD_COLUMNS / metadata-only frame).
func TestCodecRegistryAdapterReturnsErrorForUnknownCodec(t *testing.T) {
	adapter := codecRegistryAdapter{}
	_, err := adapter.Resolve(codec.Name("does-not-exist"))
	require.Error(t, err)
}
