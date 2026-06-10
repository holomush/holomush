// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
)

// --- stub types (compile-time interface satisfaction) ---

type stubAuthGuard struct{}

var _ eventbus.SessionAuthGuard = (*stubAuthGuard)(nil)

func (*stubAuthGuard) Check(_ context.Context, _ eventbus.SessionCheckRequest) (eventbus.SessionDecision, error) {
	return eventbus.SessionDecision{Permit: true}, nil
}

type stubDEKManager struct{}

var _ eventbus.SessionDEKManager = (*stubDEKManager)(nil)

func (*stubDEKManager) Resolve(_ context.Context, _ codec.KeyID, _ uint32) (codec.Key, error) {
	return codec.Key{}, nil
}

type stubAuditEmitter struct{}

var _ eventbus.SessionAuditEmitter = (*stubAuditEmitter)(nil)

func (*stubAuditEmitter) EmitPluginDecrypt(_ context.Context, _ eventbus.PluginDecryptRecord) error {
	return nil
}

// TestWithHistoryAuthProducesSameColdOptsAsCryptoCold asserts INV-CRYPTO-1:
// WithHistoryAuth(g, m, em) populates coldOpts identically to calling
// WithCryptoCold with the matching per-tier constructors.
//
// Verifies: INV-CRYPTO-1
func TestWithHistoryAuthProducesSameColdOptsAsCryptoCold(t *testing.T) {
	g := &stubAuthGuard{}
	m := &stubDEKManager{}
	em := &stubAuditEmitter{}

	bundleR := &Reader{}
	WithHistoryAuth(g, m, em)(bundleR)

	explicitR := &Reader{}
	WithCryptoCold(
		WithColdHistoryAuthGuard(g),
		WithColdHistoryDEKManager(m),
		WithColdHistoryDecryptAuditEmitter(em),
	)(explicitR)

	require.Len(t, bundleR.coldOpts, 3, "WithHistoryAuth produces 3 coldOpts")
	require.Len(t, explicitR.coldOpts, 3, "WithCryptoCold produces 3 coldOpts")

	bundleCT := &postgresColdTier{}
	for _, o := range bundleR.coldOpts {
		o(bundleCT)
	}
	explicitCT := &postgresColdTier{}
	for _, o := range explicitR.coldOpts {
		o(explicitCT)
	}

	assert.Equal(t, explicitCT.authGuard, bundleCT.authGuard, "authGuard must match")
	assert.Equal(t, explicitCT.dekManager, bundleCT.dekManager, "dekManager must match")
	assert.Equal(t, explicitCT.auditEmitter, bundleCT.auditEmitter, "auditEmitter must match")
}

// TestWithHistoryAuthProducesSameHotOptsAsCryptoHot asserts INV-CRYPTO-2:
// WithHistoryAuth(g, m, em) populates hotOpts identically to calling
// WithCryptoHot with the matching per-tier constructors.
//
// Verifies: INV-CRYPTO-2
func TestWithHistoryAuthProducesSameHotOptsAsCryptoHot(t *testing.T) {
	g := &stubAuthGuard{}
	m := &stubDEKManager{}
	em := &stubAuditEmitter{}

	bundleR := &Reader{}
	WithHistoryAuth(g, m, em)(bundleR)

	explicitR := &Reader{}
	WithCryptoHot(
		WithHistoryAuthGuard(g),
		WithHistoryDEKManager(m),
		WithHistoryDecryptAuditEmitter(em),
	)(explicitR)

	require.Len(t, bundleR.hotOpts, 3, "WithHistoryAuth produces 3 hotOpts")
	require.Len(t, explicitR.hotOpts, 3, "WithCryptoHot produces 3 hotOpts")

	bundleHT := &jetStreamHotTier{}
	for _, o := range bundleR.hotOpts {
		o(bundleHT)
	}
	explicitHT := &jetStreamHotTier{}
	for _, o := range explicitR.hotOpts {
		o(explicitHT)
	}

	assert.Equal(t, explicitHT.authGuard, bundleHT.authGuard, "authGuard must match")
	assert.Equal(t, explicitHT.dekManager, bundleHT.dekManager, "dekManager must match")
	assert.Equal(t, explicitHT.auditEmitter, bundleHT.auditEmitter, "auditEmitter must match")
}

// TestNewReaderForwardsHotOptsToHotTier asserts INV-CRYPTO-3: when NewReader builds
// the default hot tier, HotTierOption values accumulated via WithCryptoHot are
// forwarded to newJetStreamHotTier. A sentinel option sets a detectable field
// on the hot tier; after NewReader returns, the sentinel must be visible.
//
// Verifies: INV-CRYPTO-3
func TestNewReaderForwardsHotOptsToHotTier(t *testing.T) {
	embedded := eventbustest.New(t)

	g := &stubAuthGuard{}
	m := &stubDEKManager{}
	em := &stubAuditEmitter{}

	reader := NewReader(
		embedded.JS,
		nil,
		24*time.Hour,
		time.Now,
		WithCryptoHot(
			WithHistoryAuthGuard(g),
			WithHistoryDEKManager(m),
			WithHistoryDecryptAuditEmitter(em),
		),
	)

	ht, ok := reader.hot.(*jetStreamHotTier)
	require.True(t, ok, "default hot tier must be *jetStreamHotTier")
	assert.Equal(t, g, ht.authGuard, "authGuard forwarded to hot tier")
	assert.Equal(t, m, ht.dekManager, "dekManager forwarded to hot tier")
	assert.Equal(t, em, ht.auditEmitter, "auditEmitter forwarded to hot tier")
}

// TestWithCryptoHotIgnoredWhenCustomHotTier asserts INV-CRYPTO-4:
// WithCryptoHot options are not forwarded to a custom tier supplied
// via WithHotTier. The custom tier retains its original fields.
//
// Verifies: INV-CRYPTO-4
func TestWithCryptoHotIgnoredWhenCustomHotTier(t *testing.T) {
	customTier := &jetStreamHotTier{}

	r := &Reader{}
	WithCryptoHot(WithHistoryAuthGuard(&stubAuthGuard{}))(r)
	WithHotTier(customTier)(r)

	assert.Len(t, r.hotOpts, 1, "hotOpts are accumulated regardless")
	assert.Same(t, customTier, r.hot, "custom tier is installed")
	assert.Nil(t, customTier.authGuard, "custom tier authGuard unchanged — crypto not forwarded")
}

// TestNewReaderDefaultNoAuthOptions asserts INV-CRYPTO-5 (internal check):
// NewReader without WithCryptoHot, WithCryptoCold, or WithHistoryAuth
// must produce a Reader whose hotOpts and coldOpts are empty — the
// zero-value nil-auth passthrough path.
//
// Verifies: INV-CRYPTO-5
func TestNewReaderDefaultNoAuthOptions(t *testing.T) {
	embedded := eventbustest.New(t)

	reader := NewReader(
		embedded.JS,
		nil,
		24*time.Hour,
		time.Now,
		// No WithCryptoHot, WithCryptoCold, or WithHistoryAuth
	)

	assert.Empty(t, reader.hotOpts, "hotOpts must be empty when no crypto options passed")
	assert.Empty(t, reader.coldOpts, "coldOpts must be empty when no crypto options passed")
	assert.NotNil(t, reader.hot, "default hot tier still constructed")
}
