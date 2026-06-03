// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/aad"
	"github.com/holomush/holomush/internal/eventbus/history/source"
	"github.com/holomush/holomush/pkg/errutil"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// TestDispatcher_AuthGuardNil_NonIdentityCodec asserts that a non-identity
// codec read with a nil AuthGuard returns EVENTBUS_HISTORY_AUTH_GUARD_NIL
// rather than panicking (dispatcher.go:95-99).
func TestDispatcher_AuthGuardNil_NonIdentityCodec(t *testing.T) {
	d := newDispatcher()
	hotProto := &eventbusv1.Event{
		Id:      makeULIDBytes(t),
		Subject: "events.g1.scene.scn001.pose",
		Type:    "scene.pose",
		Payload: []byte("ciphertext"),
	}

	_, _, err := d.DispatchFor(
		context.Background(),
		hotProto,
		codec.NameXChaCha20v1,
		codec.KeyID(1),
		uint32(1),
		eventbus.SessionIdentity{Kind: eventbus.IdentityKindCharacter},
		nil, // guard is nil — must fail closed
		nil,
	)
	errutil.AssertErrorCode(t, err, "EVENTBUS_HISTORY_AUTH_GUARD_NIL")
}

// TestDispatcher_AuthGuardCheckFailed verifies that an AuthGuard returning
// an error is wrapped as EVENTBUS_AUTHGUARD_CHECK_FAILED (dispatcher.go:111).
func TestDispatcher_AuthGuardCheckFailed(t *testing.T) {
	d := newDispatcher(WithSourceResolver(&dispatcherStubResolver{}))
	hotProto := &eventbusv1.Event{
		Id:      makeULIDBytes(t),
		Subject: "events.g1.scene.scn001.pose",
		Type:    "scene.pose",
		Payload: []byte("ciphertext"),
	}

	guard := dispatcherErroringGuard{err: errors.New("guard backend down")}

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
	errutil.AssertErrorCode(t, err, "EVENTBUS_AUTHGUARD_CHECK_FAILED")
}

// TestDispatcher_AuthGuardDeniesProducesMetadataOnly asserts that an
// AuthGuard returning Permit=false produces a metadata-only event with no
// error and no resolver call (dispatcher.go:116-118), and stamps
// NoPlaintextReasonAuthGuardDeny (holomush-ojw1.6).
func TestDispatcher_AuthGuardDeniesProducesMetadataOnly(t *testing.T) {
	stub := &dispatcherStubResolver{}
	d := newDispatcher(WithSourceResolver(stub))
	hotProto := &eventbusv1.Event{
		Id:      makeULIDBytes(t),
		Subject: "events.g1.scene.scn001.pose",
		Type:    "scene.pose",
		Payload: []byte("ciphertext"),
	}

	guard := dispatcherDenyGuard{}

	ev, metaOnly, err := d.DispatchFor(
		context.Background(),
		hotProto,
		codec.NameXChaCha20v1,
		codec.KeyID(1),
		uint32(1),
		eventbus.SessionIdentity{Kind: eventbus.IdentityKindCharacter},
		guard,
		nil,
	)
	require.NoError(t, err)
	assert.True(t, metaOnly, "deny must produce metadata-only delivery")
	assert.Empty(t, ev.Payload, "deny must redact payload")
	assert.False(t, stub.called, "resolver must NOT be called when guard denies")
	assert.Equal(t, eventbus.NoPlaintextReasonAuthGuardDeny, ev.NoPlaintextReason,
		"AuthGuard deny must stamp NoPlaintextReasonAuthGuardDeny")
}

// TestDispatcher_SourceResolveFailed verifies that a resolver returning a
// non-ErrMetadataOnly error surfaces as EVENTBUS_SOURCE_RESOLVE_FAILED
// (dispatcher.go:143-147).
func TestDispatcher_SourceResolveFailed(t *testing.T) {
	stub := &dispatcherStubResolver{err: errors.New("dek backend unavailable")}
	d := newDispatcher(WithSourceResolver(stub))
	hotProto := &eventbusv1.Event{
		Id:      makeULIDBytes(t),
		Subject: "events.g1.scene.scn001.pose",
		Type:    "scene.pose",
		Payload: []byte("ciphertext"),
	}

	_, _, err := d.DispatchFor(
		context.Background(),
		hotProto,
		codec.NameXChaCha20v1,
		codec.KeyID(1),
		uint32(1),
		eventbus.SessionIdentity{Kind: eventbus.IdentityKindCharacter},
		&dispatcherAlwaysPermitGuard{},
		nil,
	)
	errutil.AssertErrorCode(t, err, "EVENTBUS_SOURCE_RESOLVE_FAILED")
}

// TestDispatcher_ColdUnmarshalFailed verifies that a TierColdFallback result
// whose Envelope.Payload() is not a valid marshaled eventbusv1.Event proto
// surfaces as EVENTBUS_SOURCE_COLD_UNMARSHAL_FAILED (dispatcher.go:161).
func TestDispatcher_ColdUnmarshalFailed(t *testing.T) {
	testKey := makeDispatcherTestKey(t, codec.KeyID(99), 4)
	coldID := makeTestULID(t)

	// Wrap garbage bytes (not a valid Event proto) as the cold envelope payload.
	garbageColdEnv := eventbus.NewEnvelopeForTest(eventbus.EnvelopeFields{
		EventID:    coldID,
		Codec:      codec.NameXChaCha20v1,
		KeyID:      testKey.ID,
		KeyVersion: testKey.Version,
		Payload:    []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, // not a valid proto
	})
	stub := &dispatcherStubResolver{
		result: source.ResolvedSource{
			Envelope:   garbageColdEnv,
			Key:        testKey,
			KeyID:      testKey.ID,
			KeyVersion: testKey.Version,
			SourceTier: source.TierColdFallback,
		},
	}
	d := newDispatcher(WithSourceResolver(stub))

	hotProto := &eventbusv1.Event{
		Id:      makeULIDBytes(t),
		Subject: "events.g1.scene.scn001.pose",
		Type:    "scene.pose",
		Payload: []byte("ciphertext"),
	}

	_, _, err := d.DispatchFor(
		context.Background(),
		hotProto,
		codec.NameXChaCha20v1,
		codec.KeyID(1),
		uint32(1),
		eventbus.SessionIdentity{Kind: eventbus.IdentityKindCharacter},
		&dispatcherAlwaysPermitGuard{},
		nil,
	)
	errutil.AssertErrorCode(t, err, "EVENTBUS_SOURCE_COLD_UNMARSHAL_FAILED")
}

// TestDispatcher_UnknownCodec verifies that a non-identity codec name not in
// the codec registry surfaces as EVENTBUS_HISTORY_UNKNOWN_CODEC
// (dispatcher.go:182). codec.Resolve is consulted only after AAD build, so
// we need a valid resolved key to reach this branch.
func TestDispatcher_UnknownCodec(t *testing.T) {
	testKey := makeDispatcherTestKey(t, codec.KeyID(1), 1)
	hotID := makeTestULID(t)
	hotProto := &eventbusv1.Event{
		Id:        hotID[:],
		Subject:   "events.g1.scene.scn001.pose",
		Type:      "scene.pose",
		Timestamp: timestamppb.New(dispatcherTestEpoch()),
		Payload:   []byte("ciphertext"),
	}
	hotEnv := eventbus.NewEnvelopeForTest(eventbus.EnvelopeFields{
		EventID:    hotID,
		Codec:      codec.Name("never-registered-codec"),
		KeyID:      testKey.ID,
		KeyVersion: testKey.Version,
		Payload:    []byte("ciphertext"),
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

	_, _, err := d.DispatchFor(
		context.Background(),
		hotProto,
		codec.Name("never-registered-codec"),
		testKey.ID,
		testKey.Version,
		eventbus.SessionIdentity{Kind: eventbus.IdentityKindCharacter},
		&dispatcherAlwaysPermitGuard{},
		nil,
	)
	errutil.AssertErrorCode(t, err, "EVENTBUS_HISTORY_UNKNOWN_CODEC")
}

// TestDispatcher_CodecDecodeFailed verifies that a tag-mismatch (corrupted
// ciphertext) surfaces as EVENTBUS_CODEC_DECODE_FAILED (dispatcher.go:188).
func TestDispatcher_CodecDecodeFailed(t *testing.T) {
	testKey := makeDispatcherTestKey(t, codec.KeyID(1), 1)
	hotID := makeTestULID(t)
	hotProto := &eventbusv1.Event{
		Id:        hotID[:],
		Subject:   "events.g1.scene.scn001.pose",
		Type:      "scene.pose",
		Timestamp: timestamppb.New(dispatcherTestEpoch()),
		// Deliberately garbage ciphertext — AEAD decode will fail with tag mismatch.
		Payload: []byte("this-is-not-valid-ciphertext-for-aead-decode-and-must-fail"),
	}
	hotEnv := eventbus.NewEnvelopeForTest(eventbus.EnvelopeFields{
		EventID:    hotID,
		Codec:      codec.NameXChaCha20v1,
		KeyID:      testKey.ID,
		KeyVersion: testKey.Version,
		Payload:    hotProto.Payload,
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

	_, _, err := d.DispatchFor(
		context.Background(),
		hotProto,
		codec.NameXChaCha20v1,
		testKey.ID,
		testKey.Version,
		eventbus.SessionIdentity{Kind: eventbus.IdentityKindCharacter},
		&dispatcherAlwaysPermitGuard{},
		nil,
	)
	errutil.AssertErrorCode(t, err, "EVENTBUS_CODEC_DECODE_FAILED")
}

// TestDispatcher_PluginAuditEmitterNil verifies that a plugin-identity
// decrypt with a nil audit emitter (INV-CRYPTO-11 violation) fails closed with
// EVENTBUS_HISTORY_AUDIT_EMITTER_NIL (dispatcher.go:196).
func TestDispatcher_PluginAuditEmitterNil(t *testing.T) {
	testKey, hotProto, hotEnv := buildEncryptedDispatchInputs(t, []byte("plaintext-for-plugin"))
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

	_, _, err := d.DispatchFor(
		context.Background(),
		hotProto,
		codec.NameXChaCha20v1,
		testKey.ID,
		testKey.Version,
		eventbus.SessionIdentity{
			Kind:       eventbus.IdentityKindPlugin,
			PluginName: "mod-filter",
			InstanceID: "inst-1",
		},
		&dispatcherAlwaysPermitGuard{},
		nil, // auditEm is nil for a plugin recipient — must fail closed
	)
	errutil.AssertErrorCode(t, err, "EVENTBUS_HISTORY_AUDIT_EMITTER_NIL")
}

// TestDispatcher_PluginAuditEmitFailedPropagates verifies that a plugin
// decrypt where the audit emitter returns a non-AUDIT_QUEUE_FULL error
// surfaces as EVENTBUS_HISTORY_AUDIT_EMIT_FAILED (dispatcher.go:216).
func TestDispatcher_PluginAuditEmitFailedPropagates(t *testing.T) {
	testKey, hotProto, hotEnv := buildEncryptedDispatchInputs(t, []byte("plaintext-for-plugin"))
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

	_, _, err := d.DispatchFor(
		context.Background(),
		hotProto,
		codec.NameXChaCha20v1,
		testKey.ID,
		testKey.Version,
		eventbus.SessionIdentity{
			Kind:       eventbus.IdentityKindPlugin,
			PluginName: "mod-filter",
			InstanceID: "inst-1",
		},
		&dispatcherAlwaysPermitGuard{},
		historyFailingAuditEmitterWithCode{code: "AUDIT_EMITTER_FAILED"},
	)
	errutil.AssertErrorCode(t, err, "EVENTBUS_HISTORY_AUDIT_EMIT_FAILED")
}

// TestDispatcher_PluginAuditQueueFullProducesMetadataOnly verifies the
// TOCTOU defense (Decision 3): an AUDIT_QUEUE_FULL emitter error must
// produce a metadata-only event with empty payload, NOT an error
// (dispatcher.go:210-215), and stamps NoPlaintextReasonAuditQueueFull
// (holomush-ojw1.6).
func TestDispatcher_PluginAuditQueueFullProducesMetadataOnly(t *testing.T) {
	testKey, hotProto, hotEnv := buildEncryptedDispatchInputs(t, []byte("plaintext-for-plugin"))
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

	ev, metaOnly, err := d.DispatchFor(
		context.Background(),
		hotProto,
		codec.NameXChaCha20v1,
		testKey.ID,
		testKey.Version,
		eventbus.SessionIdentity{
			Kind:       eventbus.IdentityKindPlugin,
			PluginName: "mod-filter",
			InstanceID: "inst-1",
		},
		&dispatcherAlwaysPermitGuard{},
		historyFailingAuditEmitterWithCode{code: "AUDIT_QUEUE_FULL"},
	)
	require.NoError(t, err, "AUDIT_QUEUE_FULL must not surface as an error (TOCTOU defense)")
	assert.True(t, metaOnly, "AUDIT_QUEUE_FULL must stamp metadataOnly=true")
	assert.Empty(t, ev.Payload, "AUDIT_QUEUE_FULL must zero/empty the payload")
	assert.Equal(t, eventbus.NoPlaintextReasonAuditQueueFull, ev.NoPlaintextReason,
		"AUDIT_QUEUE_FULL must stamp NoPlaintextReasonAuditQueueFull")
}

// TestDispatcher_StaleDEKProducesMetadataOnlyWithStaleDEKReason verifies
// INV-E21: when the resolver returns ErrMetadataOnly (hot+cold double miss),
// the event is delivered with metadataOnly=true and NoPlaintextReasonStaleDEK
// (holomush-ojw1.6, dispatcher.go:141-143).
func TestDispatcher_StaleDEKProducesMetadataOnlyWithStaleDEKReason(t *testing.T) {
	t.Parallel()

	stub := &dispatcherStubResolver{err: source.ErrMetadataOnly}
	d := newDispatcher(WithSourceResolver(stub))
	hotProto := &eventbusv1.Event{
		Id:        makeULIDBytes(t),
		Subject:   "events.g1.scene.scn001.pose",
		Type:      "scene.pose",
		Timestamp: timestamppb.New(dispatcherTestEpoch()),
		Payload:   []byte("ciphertext"),
	}

	ev, metaOnly, err := d.DispatchFor(
		context.Background(),
		hotProto,
		codec.NameXChaCha20v1,
		codec.KeyID(1),
		uint32(1),
		eventbus.SessionIdentity{Kind: eventbus.IdentityKindCharacter},
		&dispatcherAlwaysPermitGuard{},
		nil,
	)
	require.NoError(t, err, "INV-E21: ErrMetadataOnly must not surface as an error")
	assert.True(t, metaOnly, "stale-DEK double miss must stamp metadataOnly=true")
	assert.Empty(t, ev.Payload, "stale-DEK double miss must redact payload")
	assert.Equal(t, eventbus.NoPlaintextReasonStaleDEK, ev.NoPlaintextReason,
		"stale-DEK double miss must stamp NoPlaintextReasonStaleDEK")
}

// ─── Helpers ────────────────────────────────────────────────────────────────

// dispatcherDenyGuard always denies (Permit=false).
type dispatcherDenyGuard struct{}

func (dispatcherDenyGuard) Check(_ context.Context, _ eventbus.SessionCheckRequest) (eventbus.SessionDecision, error) {
	return eventbus.SessionDecision{Permit: false}, nil
}

// dispatcherErroringGuard returns a configurable error from Check.
type dispatcherErroringGuard struct {
	err error
}

func (g dispatcherErroringGuard) Check(_ context.Context, _ eventbus.SessionCheckRequest) (eventbus.SessionDecision, error) {
	return eventbus.SessionDecision{}, g.err
}

// historyFailingAuditEmitterWithCode is a SessionAuditEmitter that always
// returns an oops error with the given error code. Defined locally for the
// dispatcher tests (decode_jetstream_test.go has the same fake but the
// package-internal types are not re-exported across test files conditionally).
//
// NOTE: this is the same type as the one in decode_jetstream_test.go; Go
// compiles all *_test.go files in the same package together, so we re-use
// that definition implicitly. To avoid a redeclaration error here we don't
// redefine it. If the existing fake is ever renamed, this file's tests will
// fail to compile — surface that as a regression rather than silently masking.

// buildEncryptedDispatchInputs returns a key + hot proto + envelope where the
// hot proto's Payload is a valid xchacha20poly1305 ciphertext for the supplied
// plaintext, AAD-bound to the hot proto's identity. Used by the plugin-audit
// arm tests that need a successful decrypt before reaching the audit emitter.
func buildEncryptedDispatchInputs(t *testing.T, plaintext []byte) (codec.Key, *eventbusv1.Event, eventbus.Envelope) {
	t.Helper()
	testKey := makeDispatcherTestKey(t, codec.KeyID(1), 1)
	hotID := makeTestULID(t)
	hotProto := &eventbusv1.Event{
		Id:        hotID[:],
		Subject:   "events.g1.scene.scn001.pose",
		Type:      "scene.pose",
		Timestamp: timestamppb.New(dispatcherTestEpoch()),
	}
	aadBytes, err := aad.Build(hotProto, string(codec.NameXChaCha20v1), uint64(testKey.ID), testKey.Version)
	require.NoError(t, err)

	c := codec.NewXChaCha20Poly1305v1()
	ciphertext, err := c.Encode(context.Background(), plaintext, testKey, aadBytes)
	require.NoError(t, err)
	hotProto.Payload = ciphertext

	hotEnv := eventbus.NewEnvelopeForTest(eventbus.EnvelopeFields{
		EventID:    hotID,
		Codec:      codec.NameXChaCha20v1,
		KeyID:      testKey.ID,
		KeyVersion: testKey.Version,
		Payload:    ciphertext,
	})
	return testKey, hotProto, hotEnv
}
