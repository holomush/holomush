// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/chacha20poly1305"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/aad"
	"github.com/holomush/holomush/pkg/errutil"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// stubMsg implements the minimal jetstream.Msg surface decodeJetStreamMessage needs.
type stubMsg struct {
	headers nats.Header
	data    []byte
}

func (s *stubMsg) Headers() nats.Header                      { return s.headers }
func (s *stubMsg) Data() []byte                              { return s.data }
func (s *stubMsg) Subject() string                           { return "" }
func (s *stubMsg) Reply() string                             { return "" }
func (s *stubMsg) Metadata() (*jetstream.MsgMetadata, error) { return nil, nil }
func (s *stubMsg) Ack() error                                { return nil }
func (s *stubMsg) AckSync() error                            { return nil }
func (s *stubMsg) DoubleAck(_ context.Context) error         { return nil }
func (s *stubMsg) Nak() error                                { return nil }
func (s *stubMsg) NakWithDelay(_ time.Duration) error        { return nil }
func (s *stubMsg) InProgress() error                         { return nil }
func (s *stubMsg) Term() error                               { return nil }
func (s *stubMsg) TermWithReason(_ string) error             { return nil }

func validHeaders(t *testing.T) (nats.Header, ulid.ULID) {
	t.Helper()
	id := ulid.MustNew(ulid.Timestamp(time.Now()), nil)
	h := nats.Header{}
	h.Set(eventbus.HeaderMsgID, id.String())
	h.Set(eventbus.HeaderCodec, "identity")
	return h, id
}

func validPayload(t *testing.T) []byte {
	t.Helper()
	env := &eventbusv1.Event{
		Subject:   "events.main.audit",
		Type:      "audit.event",
		Timestamp: timestamppb.New(time.Unix(1, 0)),
	}
	b, err := proto.Marshal(env)
	require.NoError(t, err)
	return b
}

func TestDecodeJetStreamMessageHappyPath(t *testing.T) {
	t.Parallel()
	h, id := validHeaders(t)
	msg := &stubMsg{headers: h, data: validPayload(t)}
	ev, err := decodeJetStreamMessage(context.Background(), msg, nil, eventbus.SessionIdentity{}, nil, nil, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, id, ev.ID)
	assert.Equal(t, eventbus.Subject("events.main.audit"), ev.Subject)
}

func TestDecodeJetStreamMessageRejectsMissingMsgID(t *testing.T) {
	t.Parallel()
	h, _ := validHeaders(t)
	h.Del(eventbus.HeaderMsgID)
	msg := &stubMsg{headers: h, data: validPayload(t)}
	_, err := decodeJetStreamMessage(context.Background(), msg, nil, eventbus.SessionIdentity{}, nil, nil, nil, nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_HISTORY_MISSING_HEADER")
}

func TestDecodeJetStreamMessageRejectsBadMsgID(t *testing.T) {
	t.Parallel()
	h, _ := validHeaders(t)
	h.Set(eventbus.HeaderMsgID, "nope")
	msg := &stubMsg{headers: h, data: validPayload(t)}
	_, err := decodeJetStreamMessage(context.Background(), msg, nil, eventbus.SessionIdentity{}, nil, nil, nil, nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_HISTORY_BAD_MSG_ID")
}

func TestDecodeJetStreamMessageRejectsMissingCodec(t *testing.T) {
	t.Parallel()
	h, _ := validHeaders(t)
	h.Del(eventbus.HeaderCodec)
	msg := &stubMsg{headers: h, data: validPayload(t)}
	_, err := decodeJetStreamMessage(context.Background(), msg, nil, eventbus.SessionIdentity{}, nil, nil, nil, nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_HISTORY_MISSING_HEADER")
}

func TestDecodeJetStreamMessageRejectsUnknownCodec(t *testing.T) {
	t.Parallel()
	h, _ := validHeaders(t)
	h.Set(eventbus.HeaderCodec, "bogus-codec")
	msg := &stubMsg{headers: h, data: validPayload(t)}
	_, err := decodeJetStreamMessage(context.Background(), msg, nil, eventbus.SessionIdentity{}, nil, nil, nil, nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_HISTORY_UNKNOWN_CODEC")
}

func TestDecodeJetStreamMessageRejectsMalformedProto(t *testing.T) {
	t.Parallel()
	h, _ := validHeaders(t)
	msg := &stubMsg{headers: h, data: []byte("not-proto-bytes-at-all")}
	_, err := decodeJetStreamMessage(context.Background(), msg, nil, eventbus.SessionIdentity{}, nil, nil, nil, nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_HISTORY_UNMARSHAL_FAILED")
}

// --- Permit-path fakes (history package local) ---

// historyAlwaysDenyGuard is a SessionAuthGuard stub that always denies.
type historyAlwaysDenyGuard struct{}

func (historyAlwaysDenyGuard) Check(_ context.Context, _ eventbus.SessionCheckRequest) (eventbus.SessionDecision, error) {
	return eventbus.SessionDecision{Permit: false}, nil
}

// historyAlwaysPermitGuard is a SessionAuthGuard stub that always permits.
type historyAlwaysPermitGuard struct{}

func (historyAlwaysPermitGuard) Check(_ context.Context, _ eventbus.SessionCheckRequest) (eventbus.SessionDecision, error) {
	return eventbus.SessionDecision{Permit: true}, nil
}

// historyErrorGuard is a SessionAuthGuard that always returns an error.
type historyErrorGuard struct{}

func (historyErrorGuard) Check(_ context.Context, _ eventbus.SessionCheckRequest) (eventbus.SessionDecision, error) {
	return eventbus.SessionDecision{}, errors.New("fake: guard check error")
}

// historyKeyDEKManager is a SessionDEKManager that returns a fixed key.
type historyKeyDEKManager struct{ key codec.Key }

func (m historyKeyDEKManager) Resolve(_ context.Context, _ codec.KeyID, _ uint32) (codec.Key, error) {
	return m.key, nil
}

// historyErrorDEKManager always returns an error from Resolve.
type historyErrorDEKManager struct{}

func (historyErrorDEKManager) Resolve(_ context.Context, _ codec.KeyID, _ uint32) (codec.Key, error) {
	return codec.Key{}, errors.New("fake: DEK resolve failed")
}

// historyRecordingAuditEmitter captures EmitPluginDecrypt calls.
type historyRecordingAuditEmitter struct {
	records []eventbus.PluginDecryptRecord
	retErr  error
}

func (r *historyRecordingAuditEmitter) EmitPluginDecrypt(_ context.Context, rec eventbus.PluginDecryptRecord) error {
	r.records = append(r.records, rec)
	return r.retErr
}

// newHistoryTestXChachaKey returns a random xchacha20poly1305 codec.Key for tests.
func newHistoryTestXChachaKey(t *testing.T) codec.Key {
	t.Helper()
	km := make([]byte, chacha20poly1305.KeySize)
	_, err := rand.Read(km)
	require.NoError(t, err)
	return codec.Key{ID: 1, Version: 1, Bytes: km}
}

// encryptedHistoryMsg builds a stubMsg whose payload is a real xchacha20poly1305-v1
// ciphertext of plaintext, using the supplied key. Headers include DekRef=1,
// DekVersion=1. The AAD is built to match what decodeAndAuthorizeHistory expects.
func encryptedHistoryMsg(t *testing.T, key codec.Key, plaintext []byte) *stubMsg {
	t.Helper()
	c := codec.NewXChaCha20Poly1305v1()

	id := ulid.MustNew(ulid.Timestamp(time.Now()), nil)
	envelope := &eventbusv1.Event{
		Id:        id[:],
		Subject:   "events.main.scene.hist",
		Type:      "scene.pose",
		Timestamp: timestamppb.New(time.Unix(1, 0)),
	}
	const (
		dekRef     = 1
		dekVersion = 1
	)
	aadBytes, err := aad.Build(envelope, string(codec.NameXChaCha20v1), dekRef, dekVersion)
	require.NoError(t, err)

	ciphertext, err := c.Encode(context.Background(), plaintext, key, aadBytes)
	require.NoError(t, err)

	envelope.Payload = ciphertext
	data, err := proto.Marshal(envelope)
	require.NoError(t, err)

	h := nats.Header{}
	h.Set(eventbus.HeaderMsgID, id.String())
	h.Set(eventbus.HeaderCodec, string(codec.NameXChaCha20v1))
	h.Set(eventbus.HeaderDekRef, "1")
	h.Set(eventbus.HeaderDekVersion, "1")
	return &stubMsg{headers: h, data: data}
}

// validSensitiveHistoryHeaders returns headers for a sensitive-codec history message
// with placeholder (non-encrypted) payload.
func validSensitiveHistoryHeaders(t *testing.T) nats.Header {
	t.Helper()
	id := ulid.MustNew(ulid.Timestamp(time.Now()), nil)
	h := nats.Header{}
	h.Set(eventbus.HeaderMsgID, id.String())
	h.Set(eventbus.HeaderCodec, string(codec.NameXChaCha20v1))
	h.Set(eventbus.HeaderDekRef, "1")
	h.Set(eventbus.HeaderDekVersion, "1")
	return h
}

// validSensitiveHistoryPayload returns a proto-marshaled envelope with
// placeholder (non-encrypted) payload bytes.
func validSensitiveHistoryPayload(t *testing.T) []byte {
	t.Helper()
	env := &eventbusv1.Event{
		Subject:   "events.main.scene.hist",
		Type:      "scene.pose",
		Timestamp: timestamppb.New(time.Unix(1, 0)),
		Payload:   []byte("ciphertext-placeholder"),
	}
	b, err := proto.Marshal(env)
	require.NoError(t, err)
	return b
}

// TestDecodeJetStreamMessageAuthGuardDenyStampsMetadataOnly verifies that when
// AuthGuard denies a sensitive-codec history message, MetadataOnly=true,
// Payload is empty, and NoPlaintextReason is AUTHGUARD_DENY (holomush-ojw1.6).
func TestDecodeJetStreamMessageAuthGuardDenyStampsMetadataOnly(t *testing.T) {
	t.Parallel()

	h := validSensitiveHistoryHeaders(t)
	msg := &stubMsg{headers: h, data: validSensitiveHistoryPayload(t)}
	identity := eventbus.SessionIdentity{Kind: eventbus.IdentityKindCharacter}

	ev, err := decodeJetStreamMessage(context.Background(), msg, nil, identity,
		historyAlwaysDenyGuard{}, historyErrorDEKManager{}, nil, nil)
	require.NoError(t, err)
	assert.True(t, ev.MetadataOnly, "deny decision must stamp MetadataOnly=true")
	assert.Empty(t, ev.Payload, "deny decision must return empty payload")
	assert.Equal(t, eventbus.NoPlaintextReasonAuthGuardDeny, ev.NoPlaintextReason,
		"AuthGuard deny must stamp NoPlaintextReasonAuthGuardDeny")
}

// TestDecodeJetStreamMessageAuthGuardPermitDecryptsAndDelivers verifies that
// when AuthGuard permits for a character identity, decodeJetStreamMessage
// decrypts the payload and returns MetadataOnly=false with the original plaintext.
func TestDecodeJetStreamMessageAuthGuardPermitDecryptsAndDelivers(t *testing.T) {
	t.Parallel()

	key := newHistoryTestXChachaKey(t)
	plaintext := []byte(`{"say":"history hello"}`)
	msg := encryptedHistoryMsg(t, key, plaintext)
	identity := eventbus.SessionIdentity{Kind: eventbus.IdentityKindCharacter}

	ev, err := decodeJetStreamMessage(context.Background(), msg, nil, identity,
		historyAlwaysPermitGuard{}, historyKeyDEKManager{key: key}, nil, nil)
	require.NoError(t, err)
	assert.False(t, ev.MetadataOnly, "permit path must not stamp MetadataOnly")
	assert.Equal(t, plaintext, ev.Payload, "permit path must return decrypted plaintext")
}

// TestDecodeJetStreamMessageAuthGuardPermitForPluginEmitsAuditAndDelivers
// verifies that when a plugin identity receives a permit decision, the audit
// emitter is called and the event is delivered with the decrypted payload.
func TestDecodeJetStreamMessageAuthGuardPermitForPluginEmitsAuditAndDelivers(t *testing.T) {
	t.Parallel()

	key := newHistoryTestXChachaKey(t)
	plaintext := []byte(`{"pose":"history pose"}`)
	msg := encryptedHistoryMsg(t, key, plaintext)
	identity := eventbus.SessionIdentity{
		Kind:       eventbus.IdentityKindPlugin,
		PluginName: "mod-filter",
		InstanceID: "inst-01",
	}
	audit := &historyRecordingAuditEmitter{}

	ev, err := decodeJetStreamMessage(context.Background(), msg, nil, identity,
		historyAlwaysPermitGuard{}, historyKeyDEKManager{key: key}, audit, nil)
	require.NoError(t, err)
	assert.False(t, ev.MetadataOnly)
	assert.Equal(t, plaintext, ev.Payload)
	require.Len(t, audit.records, 1)
	assert.Equal(t, "mod-filter", audit.records[0].PluginName)
}

// TestDecodeJetStreamMessageTOCTOUDefenseZerosPlaintextOnAuditQueueFull
// verifies the TOCTOU defense on the history path: when AUDIT_QUEUE_FULL is
// returned by the audit emitter, the plaintext buffer is zeroed before
// returning a metadata-only event.
func TestDecodeJetStreamMessageTOCTOUDefenseZerosPlaintextOnAuditQueueFull(t *testing.T) {
	t.Parallel()

	key := newHistoryTestXChachaKey(t)
	plaintext := []byte(`{"secret":"history do not leak"}`)
	msg := encryptedHistoryMsg(t, key, plaintext)
	identity := eventbus.SessionIdentity{
		Kind:       eventbus.IdentityKindPlugin,
		PluginName: "mod-filter",
	}

	// Use oops-coded AUDIT_QUEUE_FULL so isHistoryAuditQueueFull recognises it
	// as the TOCTOU-fallback trigger (plain errors.New would propagate instead).
	auditErr := oops.Code("AUDIT_QUEUE_FULL").Errorf("fake: audit queue full")
	audit := &historyRecordingAuditEmitter{retErr: auditErr}

	ev, err := decodeJetStreamMessage(context.Background(), msg, nil, identity,
		historyAlwaysPermitGuard{}, historyKeyDEKManager{key: key}, audit, nil)
	require.NoError(t, err)
	assert.True(t, ev.MetadataOnly, "AUDIT_QUEUE_FULL must stamp MetadataOnly=true")
	assert.Empty(t, ev.Payload, "AUDIT_QUEUE_FULL must return empty payload")
	require.Len(t, audit.records, 1, "audit emitter must have been attempted")
}

// TestDecodeJetStreamMessageAuthGuardCheckErrorPropagates verifies that when
// AuthGuard.Check returns an error, decodeJetStreamMessage propagates it with
// EVENTBUS_AUTHGUARD_CHECK_FAILED.
func TestDecodeJetStreamMessageAuthGuardCheckErrorPropagates(t *testing.T) {
	t.Parallel()

	h := validSensitiveHistoryHeaders(t)
	msg := &stubMsg{headers: h, data: validSensitiveHistoryPayload(t)}
	identity := eventbus.SessionIdentity{Kind: eventbus.IdentityKindCharacter}

	_, err := decodeJetStreamMessage(context.Background(), msg, nil, identity,
		historyErrorGuard{}, historyErrorDEKManager{}, nil, nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_AUTHGUARD_CHECK_FAILED")
}

// TestDecodeJetStreamMessageAuthGuardPermitDEKResolveErrorPropagates verifies
// that when the DEK manager returns an error after a permit decision, the error
// propagates with EVENTBUS_DEK_RESOLVE_FAILED.
func TestDecodeJetStreamMessageAuthGuardPermitDEKResolveErrorPropagates(t *testing.T) {
	t.Parallel()

	h := validSensitiveHistoryHeaders(t)
	msg := &stubMsg{headers: h, data: validSensitiveHistoryPayload(t)}
	identity := eventbus.SessionIdentity{Kind: eventbus.IdentityKindCharacter}

	_, err := decodeJetStreamMessage(context.Background(), msg, nil, identity,
		historyAlwaysPermitGuard{}, historyErrorDEKManager{}, nil, nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_DEK_RESOLVE_FAILED")
}

// TestDecodeJetStreamMessageAuthGuardPermitDecodeErrorPropagates verifies that
// when the codec.Decode step fails (wrong key / tampered ciphertext), the error
// propagates with EVENTBUS_CODEC_DECODE_FAILED.
func TestDecodeJetStreamMessageAuthGuardPermitDecodeErrorPropagates(t *testing.T) {
	t.Parallel()

	realKey := newHistoryTestXChachaKey(t)
	wrongKey := newHistoryTestXChachaKey(t)
	msg := encryptedHistoryMsg(t, realKey, []byte("secret"))
	identity := eventbus.SessionIdentity{Kind: eventbus.IdentityKindCharacter}

	_, err := decodeJetStreamMessage(context.Background(), msg, nil, identity,
		historyAlwaysPermitGuard{}, historyKeyDEKManager{key: wrongKey}, nil, nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_CODEC_DECODE_FAILED")
}

// --- Round 5: nil-dekMgr and INV-CRYPTO-11 error paths (history) ---

// historyFailingAuditEmitterWithCode is a SessionAuditEmitter that always
// returns an oops error with the given error code. Used to test the narrowed
// AUDIT_QUEUE_FULL fallback vs. non-queue-full propagation.
type historyFailingAuditEmitterWithCode struct {
	code string
}

func (f historyFailingAuditEmitterWithCode) EmitPluginDecrypt(_ context.Context, _ eventbus.PluginDecryptRecord) error {
	return oops.Code(f.code).Errorf("fake: audit emit failed with code %s", f.code)
}

// TestDecodeJetStreamMessageNilDEKManagerAfterPermitFailsClosed verifies that
// when the AuthGuard permits but dekMgr is nil (misconfiguration),
// decodeAndAuthorizeHistory fails closed with EVENTBUS_HISTORY_DEK_MANAGER_NIL.
func TestDecodeJetStreamMessageNilDEKManagerAfterPermitFailsClosed(t *testing.T) {
	t.Parallel()

	h := validSensitiveHistoryHeaders(t)
	msg := &stubMsg{headers: h, data: validSensitiveHistoryPayload(t)}
	identity := eventbus.SessionIdentity{Kind: eventbus.IdentityKindCharacter}

	_, err := decodeJetStreamMessage(context.Background(), msg, nil, identity,
		historyAlwaysPermitGuard{}, nil, nil, nil) // nil dekMgr
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_HISTORY_DEK_MANAGER_NIL")
}

// TestDecodeJetStreamMessagePluginIdentityWithNilAuditEmitterFailsClosed
// verifies INV-CRYPTO-11: when a plugin identity receives a permit decision but the
// audit emitter is nil, decodeAndAuthorizeHistory must fail closed with
// EVENTBUS_HISTORY_AUDIT_EMITTER_NIL rather than delivering plaintext.
func TestDecodeJetStreamMessagePluginIdentityWithNilAuditEmitterFailsClosed(t *testing.T) {
	t.Parallel()

	key := newHistoryTestXChachaKey(t)
	plaintext := []byte(`{"secret":"must not leak"}`)
	msg := encryptedHistoryMsg(t, key, plaintext)
	identity := eventbus.SessionIdentity{
		Kind:       eventbus.IdentityKindPlugin,
		PluginName: "mod-filter",
	}

	_, err := decodeJetStreamMessage(context.Background(), msg, nil, identity,
		historyAlwaysPermitGuard{}, historyKeyDEKManager{key: key}, nil, nil) // nil auditEm
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_HISTORY_AUDIT_EMITTER_NIL")
}

// TestDecodeJetStreamMessagePluginNonQueueFullAuditErrorPropagates verifies
// that when the audit emitter returns a non-AUDIT_QUEUE_FULL error,
// decodeAndAuthorizeHistory propagates it as EVENTBUS_HISTORY_AUDIT_EMIT_FAILED.
func TestDecodeJetStreamMessagePluginNonQueueFullAuditErrorPropagates(t *testing.T) {
	t.Parallel()

	key := newHistoryTestXChachaKey(t)
	plaintext := []byte(`{"secret":"must not leak"}`)
	msg := encryptedHistoryMsg(t, key, plaintext)
	identity := eventbus.SessionIdentity{
		Kind:       eventbus.IdentityKindPlugin,
		PluginName: "mod-filter",
	}

	_, err := decodeJetStreamMessage(context.Background(), msg, nil, identity,
		historyAlwaysPermitGuard{}, historyKeyDEKManager{key: key},
		historyFailingAuditEmitterWithCode{code: "AUDIT_EMITTER_FAILED"}, nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_HISTORY_AUDIT_EMIT_FAILED")
}

// TestDecodeJetStreamMessagePluginAuditQueueFullStillStampsMetadataOnly
// verifies that the AUDIT_QUEUE_FULL code (specifically) triggers the TOCTOU
// plaintext-zero + metadata_only fallback in the history path, and stamps
// NoPlaintextReasonAuditQueueFull (holomush-ojw1.6).
func TestDecodeJetStreamMessagePluginAuditQueueFullStillStampsMetadataOnly(t *testing.T) {
	t.Parallel()

	key := newHistoryTestXChachaKey(t)
	plaintext := []byte(`{"secret":"do not leak"}`)
	msg := encryptedHistoryMsg(t, key, plaintext)
	identity := eventbus.SessionIdentity{
		Kind:       eventbus.IdentityKindPlugin,
		PluginName: "mod-filter",
	}

	ev, err := decodeJetStreamMessage(context.Background(), msg, nil, identity,
		historyAlwaysPermitGuard{}, historyKeyDEKManager{key: key},
		historyFailingAuditEmitterWithCode{code: "AUDIT_QUEUE_FULL"}, nil)
	require.NoError(t, err)
	assert.True(t, ev.MetadataOnly, "AUDIT_QUEUE_FULL must stamp MetadataOnly=true")
	assert.Empty(t, ev.Payload, "AUDIT_QUEUE_FULL must return empty payload")
	assert.Equal(t, eventbus.NoPlaintextReasonAuditQueueFull, ev.NoPlaintextReason,
		"AUDIT_QUEUE_FULL must stamp NoPlaintextReasonAuditQueueFull")
}
