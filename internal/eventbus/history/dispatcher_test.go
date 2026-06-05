// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	"context"
	"crypto/rand"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/chacha20poly1305"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/aad"
	"github.com/holomush/holomush/internal/eventbus/history/source"
	"github.com/holomush/holomush/pkg/errutil"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// TestDecodeAuthorizeAndDispatchIdentityCodecPasses asserts that an
// identity-codec event passes through the dispatcher unchanged: no
// AuthGuard gate, no decryption, payload preserved byte-for-byte.
func TestDecodeAuthorizeAndDispatchIdentityCodecPasses(t *testing.T) {
	envelope := &eventbusv1.Event{
		Id:      makeULIDBytes(t),
		Subject: "events.game1.world.location.loc-01ABC.test",
		Type:    "core-test:hello",
		Payload: []byte("hello, world"),
	}

	ev, metaOnly, err := decodeAuthorizeAndDispatch(
		context.Background(),
		envelope,
		codec.NameIdentity,         // identity codec — no AuthGuard
		codec.KeyID(0),             // unused for identity
		uint32(0),                  // unused for identity
		eventbus.SessionIdentity{}, // identity (not consulted on identity codec)
		nil,                        // guard (not consulted on identity codec)
		nil,                        // dekMgr (not consulted on identity codec)
		nil,                        // auditEm (not consulted on identity codec)
		false,                      // readBack (not consulted on identity codec)
	)
	require.NoError(t, err)
	assert.False(t, metaOnly, "identity codec must not be metadata-only")
	assert.Equal(t, []byte("hello, world"), ev.Payload, "payload bytes preserved")
}

// TestDispatcherIdentityCodecPassesThroughResolver asserts that the dispatcher
// struct's DispatchFor method handles identity codec the same as the free
// function: no resolver call, payload preserved.
func TestDispatcherIdentityCodecPassesThroughResolver(t *testing.T) {
	envelope := &eventbusv1.Event{
		Id:      makeULIDBytes(t),
		Subject: "events.game1.world.location.loc-01ABC.test",
		Type:    "core-test:hello",
		Payload: []byte("hello via resolver path"),
	}
	// Resolver is nil — identity codec must not call it.
	d := newDispatcher()

	ev, metaOnly, err := d.DispatchFor(
		context.Background(),
		envelope,
		codec.NameIdentity,
		codec.KeyID(0),
		uint32(0),
		eventbus.SessionIdentity{},
		nil, // guard — not consulted on identity codec
		nil, // auditEm — not consulted on identity codec
	)
	require.NoError(t, err)
	assert.False(t, metaOnly)
	assert.Equal(t, []byte("hello via resolver path"), ev.Payload)
}

// TestDispatcher_AADFromResolvedEnvelope verifies INV-CRYPTO-107: when the resolver
// returns a TierColdFallback result, the dispatcher builds AAD from the cold
// (resolved) envelope's fields, not the original hot envelope's fields.
//
// Proof via decryption: the payload is encrypted with AAD derived from the
// cold proto's fields (keyID=99, keyVersion=4). If the dispatcher uses hot
// fields (keyID=42, keyVersion=3) for AAD construction, decryption fails with
// an AEAD tag-mismatch error. A successful NoError assertion proves the cold
// fields were used.
func TestDispatcher_AADFromResolvedEnvelope(t *testing.T) {
	testKey := makeDispatcherTestKey(t, codec.KeyID(99), 4)
	plaintext := []byte("sensitive-payload-requiring-cold-aad")

	// Build the cold proto: this is what cold_postgres.LookupByID would
	// return as the envelope bytes in events_audit.
	coldID := makeTestULID(t)
	coldProto := &eventbusv1.Event{
		Id:        coldID[:],
		Subject:   "events.g1.scene.scn001.pose",
		Type:      "scene.pose",
		Timestamp: timestamppb.New(dispatcherTestEpoch()),
	}
	// Build AAD from cold proto's fields (INV-CRYPTO-107 requires dispatcher to use these).
	coldAADBytes, err := aad.Build(coldProto, string(codec.NameXChaCha20v1), uint64(testKey.ID), testKey.Version)
	require.NoError(t, err)

	c := codec.NewXChaCha20Poly1305v1()
	ciphertext, err := c.Encode(context.Background(), plaintext, testKey, coldAADBytes)
	require.NoError(t, err)

	// Marshal the cold proto (with ciphertext payload) — this is what
	// Envelope.Payload() carries for a cold-tier row.
	coldProto.Payload = ciphertext
	coldProtoBytes, err := proto.Marshal(coldProto)
	require.NoError(t, err)

	// Wrap cold proto bytes in an eventbus.Envelope for the ResolvedSource.
	coldEnv := eventbus.NewEnvelopeForTest(eventbus.EnvelopeFields{
		EventID:    coldID,
		Codec:      codec.NameXChaCha20v1,
		KeyID:      testKey.ID,
		KeyVersion: testKey.Version,
		Payload:    coldProtoBytes, // marshaled proto (cold-tier format)
	})

	// stubResolver always returns the cold ResolvedSource.
	stub := &dispatcherStubResolver{
		result: source.ResolvedSource{
			Envelope:   coldEnv,
			Key:        testKey,
			KeyID:      testKey.ID,
			KeyVersion: testKey.Version,
			SourceTier: source.TierColdFallback,
		},
	}
	d := newDispatcher(WithSourceResolver(stub))

	// Hot envelope has DIFFERENT keyID=42, keyVersion=3. If dispatcher uses
	// hot fields for AAD → AEAD tag mismatch → error. Using cold fields → success.
	hotID := makeTestULID(t)
	hotProto := &eventbusv1.Event{
		Id:        hotID[:],
		Subject:   "events.g1.scene.scn001.pose",
		Type:      "scene.pose",
		Timestamp: timestamppb.New(dispatcherTestEpoch()),
		// Payload is the raw ciphertext for a hot message (hot tier hasn't
		// been rekey'd yet, but resolver returns cold substitute).
		Payload: ciphertext,
	}
	guard := &dispatcherAlwaysPermitGuard{}

	_, _, dispErr := d.DispatchFor(
		context.Background(),
		hotProto,
		codec.NameXChaCha20v1,
		codec.KeyID(42), // hot keyID — different from cold
		uint32(3),       // hot keyVersion — different from cold
		eventbus.SessionIdentity{Kind: eventbus.IdentityKindCharacter},
		guard,
		nil,
	)
	// INV-CRYPTO-107: must succeed — cold AAD was used.
	require.NoError(t, dispErr, "INV-CRYPTO-107: dispatcher must use cold envelope fields for AAD after fallback")
}

// TestDispatcher_MetadataOnlyDeliveryOnDoubleMiss verifies INV-CRYPTO-108: when the
// resolver returns ErrMetadataOnly (double miss), the dispatcher delivers
// the event with MetadataOnly=true and empty Payload, with no error.
func TestDispatcher_MetadataOnlyDeliveryOnDoubleMiss(t *testing.T) {
	stub := &dispatcherStubResolver{err: source.ErrMetadataOnly}
	d := newDispatcher(WithSourceResolver(stub))

	hotProto := &eventbusv1.Event{
		Id:        makeULIDBytes(t),
		Subject:   "events.g1.scene.scn001.pose",
		Type:      "scene.pose",
		Timestamp: timestamppb.New(dispatcherTestEpoch()),
		Payload:   []byte("ciphertext-bytes"),
	}
	guard := &dispatcherAlwaysPermitGuard{}

	out, ok, err := d.DispatchFor(
		context.Background(),
		hotProto,
		codec.NameXChaCha20v1,
		codec.KeyID(42),
		uint32(3),
		eventbus.SessionIdentity{Kind: eventbus.IdentityKindCharacter},
		guard,
		nil,
	)
	// INV-CRYPTO-108: ErrMetadataOnly MUST produce (event, metadataOnly=true, nil).
	require.NoError(t, err, "INV-CRYPTO-108: ErrMetadataOnly must not surface as an error")
	assert.True(t, ok, "metadataOnly flag must be true on double miss")
	assert.Empty(t, out.Payload, "INV-CRYPTO-108: payload must be empty on double miss")
}

// TestDispatcherResolverNilAfterPermitFailsClosed asserts that a permit with no
// resolver wired returns EVENTBUS_HISTORY_RESOLVER_NIL rather than panicking.
func TestDispatcherResolverNilAfterPermitFailsClosed(t *testing.T) {
	d := newDispatcher() // no resolver injected
	hotProto := &eventbusv1.Event{
		Id:      makeULIDBytes(t),
		Subject: "events.g1.scene.scn001.pose",
		Type:    "scene.pose",
		Payload: []byte("ciphertext"),
	}
	guard := &dispatcherAlwaysPermitGuard{}

	_, _, err := d.DispatchFor(
		context.Background(),
		hotProto,
		codec.NameXChaCha20v1,
		codec.KeyID(1),
		uint32(1),
		eventbus.SessionIdentity{Kind: eventbus.IdentityKindCharacter},
		guard,
		nil,
	)
	errutil.AssertErrorCode(t, err, "EVENTBUS_HISTORY_RESOLVER_NIL")
}

// TestDispatcherHotTierSuccessPath asserts the hot-tier (TierHot) happy path:
// resolver returns the hot envelope, dispatcher decrypts with hot AAD, delivers
// plaintext.
func TestDispatcherHotTierSuccessPath(t *testing.T) {
	testKey := makeDispatcherTestKey(t, codec.KeyID(1), 1)
	plaintext := []byte("hot-tier-plaintext")

	hotID := makeTestULID(t)
	hotProto := &eventbusv1.Event{
		Id:        hotID[:],
		Subject:   "events.g1.scene.scn001.pose",
		Type:      "scene.pose",
		Timestamp: timestamppb.New(dispatcherTestEpoch()),
	}
	hotAADBytes, err := aad.Build(hotProto, string(codec.NameXChaCha20v1), uint64(testKey.ID), testKey.Version)
	require.NoError(t, err)

	c := codec.NewXChaCha20Poly1305v1()
	ciphertext, err := c.Encode(context.Background(), plaintext, testKey, hotAADBytes)
	require.NoError(t, err)
	hotProto.Payload = ciphertext

	// Hot envelope: Payload = ciphertext (not marshaled proto).
	hotEnv := eventbus.NewEnvelopeForTest(eventbus.EnvelopeFields{
		EventID:    hotID,
		Codec:      codec.NameXChaCha20v1,
		KeyID:      testKey.ID,
		KeyVersion: testKey.Version,
		Payload:    ciphertext,
	})
	stub := &dispatcherStubResolver{
		result: source.ResolvedSource{
			Envelope:   hotEnv,
			Key:        testKey,
			KeyID:      testKey.ID,
			KeyVersion: testKey.Version,
			SourceTier: source.TierHot,
		},
	}
	d := newDispatcher(WithSourceResolver(stub))
	guard := &dispatcherAlwaysPermitGuard{}

	out, metaOnly, err := d.DispatchFor(
		context.Background(),
		hotProto,
		codec.NameXChaCha20v1,
		testKey.ID,
		testKey.Version,
		eventbus.SessionIdentity{Kind: eventbus.IdentityKindCharacter},
		guard,
		nil,
	)
	require.NoError(t, err)
	assert.False(t, metaOnly)
	assert.Equal(t, plaintext, out.Payload)
}

// ─── Test helpers ───────────────────────────────────────────────────────────

// dispatcherStubResolver is a test double for source.SourceResolver.
// It returns a fixed result or error, and records whether Resolve was called.
type dispatcherStubResolver struct {
	result source.ResolvedSource
	err    error
	called bool
}

func (s *dispatcherStubResolver) Resolve(_ context.Context, _ eventbus.Envelope) (source.ResolvedSource, error) {
	s.called = true
	return s.result, s.err
}

// dispatcherAlwaysPermitGuard always permits.
type dispatcherAlwaysPermitGuard struct{}

func (dispatcherAlwaysPermitGuard) Check(_ context.Context, _ eventbus.SessionCheckRequest) (eventbus.SessionDecision, error) {
	return eventbus.SessionDecision{Permit: true}, nil
}

// makeDispatcherTestKey returns a random xchacha20poly1305 codec.Key.
func makeDispatcherTestKey(t *testing.T, id codec.KeyID, version uint32) codec.Key {
	t.Helper()
	km := make([]byte, chacha20poly1305.KeySize)
	_, err := rand.Read(km)
	require.NoError(t, err)
	return codec.Key{ID: id, Version: version, Bytes: km}
}

// makeTestULID returns a fresh random ULID.
func makeTestULID(t *testing.T) ulid.ULID {
	t.Helper()
	id, err := ulid.New(ulid.Now(), rand.Reader)
	require.NoError(t, err)
	return id
}

// dispatcherTestEpoch returns a fixed timestamp for deterministic test protos.
func dispatcherTestEpoch() time.Time {
	return time.Unix(1_000_000, 0)
}

// makeULIDBytes returns a 16-byte ULID for test envelopes.
func makeULIDBytes(t *testing.T) []byte {
	t.Helper()
	return []byte{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 1}
}
