// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

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

	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/aad"
	"github.com/holomush/holomush/pkg/errutil"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// stubMsg is a minimal jetstream.Msg for decodeDelivery unit tests.
type stubMsg struct {
	headers nats.Header
	data    []byte
	subject string
}

func (s *stubMsg) Headers() nats.Header                      { return s.headers }
func (s *stubMsg) Data() []byte                              { return s.data }
func (s *stubMsg) Subject() string                           { return s.subject }
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

// validSubscriberHeaders builds a minimally-valid header set matching what
// Publisher stamps on wire. Tests mutate/strip one at a time to cover branches.
func validSubscriberHeaders(t *testing.T) (nats.Header, ulid.ULID) {
	t.Helper()
	id := ulid.MustNew(ulid.Timestamp(time.Now()), nil)
	h := nats.Header{}
	h.Set(HeaderMsgID, id.String())
	h.Set(HeaderSchemaVersion, SchemaVersion)
	h.Set(HeaderCodec, "identity")
	return h, id
}

// validPayload returns a proto-marshaled eventbusv1.Event, same path publisher uses.
func validPayload(t *testing.T) []byte {
	t.Helper()
	env := &eventbusv1.Event{
		Subject:   "events.main.test",
		Type:      "test.event",
		Timestamp: timestamppb.New(time.Unix(1, 0)),
		Payload:   []byte("{}"),
	}
	b, err := proto.Marshal(env)
	require.NoError(t, err)
	return b
}

func TestDecodeDeliveryHappyPathReturnsEvent(t *testing.T) {
	t.Parallel()
	h, id := validSubscriberHeaders(t)
	msg := &stubMsg{headers: h, data: validPayload(t)}
	ev, err := decodeDelivery(context.Background(), msg, nil)
	require.NoError(t, err)
	assert.Equal(t, id, ev.ID)
	assert.Equal(t, Subject("events.main.test"), ev.Subject)
	assert.Equal(t, Type("test.event"), ev.Type)
}

func TestDecodeDeliveryRejectsMissingHeaders(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		strip   string
		wantCod string
	}{
		{"missing msg id", HeaderMsgID, "EVENTBUS_SUBSCRIBE_MISSING_HEADER"},
		{"missing schema version", HeaderSchemaVersion, "EVENTBUS_SUBSCRIBE_MISSING_HEADER"},
		{"missing codec", HeaderCodec, "EVENTBUS_SUBSCRIBE_MISSING_HEADER"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, _ := validSubscriberHeaders(t)
			h.Del(tc.strip)
			msg := &stubMsg{headers: h, data: validPayload(t)}
			_, err := decodeDelivery(context.Background(), msg, nil)
			require.Error(t, err)
			errutil.AssertErrorCode(t, err, tc.wantCod)
		})
	}
}

func TestDecodeDeliveryRejectsBadMsgID(t *testing.T) {
	t.Parallel()
	h, _ := validSubscriberHeaders(t)
	h.Set(HeaderMsgID, "not-a-ulid")
	msg := &stubMsg{headers: h, data: validPayload(t)}
	_, err := decodeDelivery(context.Background(), msg, nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_SUBSCRIBE_BAD_MSG_ID")
}

func TestDecodeDeliveryRejectsSchemaMismatch(t *testing.T) {
	t.Parallel()
	h, _ := validSubscriberHeaders(t)
	h.Set(HeaderSchemaVersion, "99")
	msg := &stubMsg{headers: h, data: validPayload(t)}
	_, err := decodeDelivery(context.Background(), msg, nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_SUBSCRIBE_SCHEMA_MISMATCH")
}

func TestDecodeDeliveryRejectsUnknownCodec(t *testing.T) {
	t.Parallel()
	h, _ := validSubscriberHeaders(t)
	h.Set(HeaderCodec, "not-a-real-codec")
	msg := &stubMsg{headers: h, data: validPayload(t)}
	_, err := decodeDelivery(context.Background(), msg, nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_SUBSCRIBE_UNKNOWN_CODEC")
}

func TestDecodeDeliveryRejectsMalformedProto(t *testing.T) {
	t.Parallel()
	h, _ := validSubscriberHeaders(t)
	// Identity codec passes bytes through verbatim — so invalid proto
	// reaches proto.Unmarshal and surfaces the unmarshal error path.
	msg := &stubMsg{headers: h, data: []byte("not-a-valid-proto-message-just-bytes")}
	_, err := decodeDelivery(context.Background(), msg, nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_SUBSCRIBE_UNMARSHAL_FAILED")
}

// stubMsgWithSeq is stubMsg with a Metadata() implementation that returns a
// non-nil MsgMetadata so the Seq-plumbing path in decodeDelivery is exercised.
type stubMsgWithSeq struct {
	stubMsg
	seq uint64
}

func (s *stubMsgWithSeq) Metadata() (*jetstream.MsgMetadata, error) {
	return &jetstream.MsgMetadata{
		Sequence: jetstream.SequencePair{Stream: s.seq},
	}, nil
}

func TestDecodeDeliveryPopulatesSeqFromMetadata(t *testing.T) {
	t.Parallel()
	h, _ := validSubscriberHeaders(t)
	msg := &stubMsgWithSeq{
		stubMsg: stubMsg{headers: h, data: validPayload(t)},
		seq:     42,
	}
	ev, err := decodeDelivery(context.Background(), msg, nil)
	require.NoError(t, err)
	assert.Equal(t, uint64(42), ev.Seq)
}

// fakeAlwaysDenyGuard is a SessionAuthGuard stub that always denies. Used to
// test the AuthGuard-deny → metadata_only path in decodeAndAuthorize.
type fakeAlwaysDenyGuard struct{}

func (fakeAlwaysDenyGuard) Check(_ context.Context, _ SessionCheckRequest) (SessionDecision, error) {
	return SessionDecision{Permit: false}, nil
}

// fakeDEKManager is a minimal SessionDEKManager stub for decodeAndAuthorize
// tests. Resolve is unreachable on the deny path.
type fakeDEKManager struct{}

func (fakeDEKManager) Resolve(_ context.Context, _ codec.KeyID, _ uint32) (codec.Key, error) {
	return codec.Key{}, errors.New("fake DEK manager: not implemented")
}

// fakeNilAuditEmitter is a SessionAuditEmitter that always returns nil.
type fakeNilAuditEmitter struct{}

func (fakeNilAuditEmitter) EmitPluginDecrypt(_ context.Context, _ PluginDecryptRecord) error {
	return nil
}

// validSensitiveHeaders builds a header set for a sensitive (non-identity) codec
// message. The codec field is set to xchacha20poly1305-v1 so that decodeAndAuthorize
// recognises it as a sensitive delivery and calls AuthGuard.Check.
func validSensitiveHeaders(t *testing.T) nats.Header {
	t.Helper()
	id := ulid.MustNew(ulid.Timestamp(time.Now()), nil)
	h := nats.Header{}
	h.Set(HeaderMsgID, id.String())
	h.Set(HeaderSchemaVersion, SchemaVersion)
	h.Set(HeaderCodec, string(codec.NameXChaCha20v1))
	// DEK headers required for sensitive messages.
	h.Set(HeaderDekRef, "1")
	h.Set(HeaderDekVersion, "1")
	return h
}

// validSensitivePayload returns a proto-marshaled eventbusv1.Event with a
// non-empty payload field. The payload bytes are NOT actually encrypted; the
// test only exercises the auth path (deny branch stops before decryption).
func validSensitivePayload(t *testing.T) []byte {
	t.Helper()
	env := &eventbusv1.Event{
		Subject:   "events.main.scene.sensitive",
		Type:      "scene.pose",
		Timestamp: timestamppb.New(time.Unix(1, 0)),
		Payload:   []byte("ciphertext-placeholder"),
	}
	b, err := proto.Marshal(env)
	require.NoError(t, err)
	return b
}

// TestDecodeDeliveryAuthGuardDenyStampsMetadataOnly verifies that when
// AuthGuard.Check denies a sensitive-codec delivery, decodeAndAuthorize stamps
// metadataOnly=true and returns an Event with an empty payload.
func TestDecodeDeliveryAuthGuardDenyStampsMetadataOnly(t *testing.T) {
	t.Parallel()

	h := validSensitiveHeaders(t)
	msg := &stubMsg{headers: h, data: validSensitivePayload(t)}

	identity := SessionIdentity{
		Kind:        IdentityKindCharacter,
		PlayerID:    "01TESTPLAYER01234567890A",
		CharacterID: "01TESTCHARACTER0123456A",
		BindingID:   "01TESTBINDING01234567AB",
	}

	// Wrap the raw stubMsg in a proto envelope for decodeAndAuthorize.
	// We also need to pass through the proto-unmarshaled envelope.
	// Use the internal decode path via decodeDeliveryWithAuth with a guard set.
	event, metaOnly, decodeErr := decodeDeliveryWithAuth(
		context.Background(),
		msg,
		nil,
		identity,
		fakeAlwaysDenyGuard{},
		fakeDEKManager{},
		fakeNilAuditEmitter{},
	)
	require.NoError(t, decodeErr)
	assert.True(t, metaOnly, "deny decision must stamp metadataOnly=true")
	assert.Empty(t, event.Payload, "deny decision must return empty payload")
}

// --- Permit-path helpers ---

// newTestXChachaKey returns a random xchacha20poly1305 codec.Key for unit tests.
func newTestXChachaKey(t *testing.T) codec.Key {
	t.Helper()
	km := make([]byte, chacha20poly1305.KeySize)
	_, err := rand.Read(km)
	require.NoError(t, err)
	return codec.Key{ID: 1, Version: 1, Bytes: km}
}

// encryptedSensitiveMsg builds a stubMsg whose payload is a real
// xchacha20poly1305-v1 ciphertext of plaintext, using the supplied key.
// The returned headers include DekRef=1, DekVersion=1.
//
// The AAD is derived from the proto envelope using aad.Build so that the
// Permit path in decodeAndAuthorize can verify the tag.
func encryptedSensitiveMsg(t *testing.T, key codec.Key, plaintext []byte) *stubMsg {
	t.Helper()
	c := codec.NewXChaCha20Poly1305v1()

	// Build the proto envelope first (without payload — we need it to build AAD).
	id := ulid.MustNew(ulid.Timestamp(time.Now()), nil)
	envelope := &eventbusv1.Event{
		Id:        id[:],
		Subject:   "events.main.scene.sensitive",
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

	// Stamp ciphertext into envelope and marshal.
	envelope.Payload = ciphertext
	data, err := proto.Marshal(envelope)
	require.NoError(t, err)

	h := nats.Header{}
	h.Set(HeaderMsgID, id.String())
	h.Set(HeaderSchemaVersion, SchemaVersion)
	h.Set(HeaderCodec, string(codec.NameXChaCha20v1))
	h.Set(HeaderDekRef, "1")
	h.Set(HeaderDekVersion, "1")

	return &stubMsg{headers: h, data: data}
}

// fakeAlwaysPermitGuard is a SessionAuthGuard stub that always permits.
type fakeAlwaysPermitGuard struct{}

func (fakeAlwaysPermitGuard) Check(_ context.Context, _ SessionCheckRequest) (SessionDecision, error) {
	return SessionDecision{Permit: true}, nil
}

// fakeKeyDEKManager is a SessionDEKManager stub that returns a fixed key.
type fakeKeyDEKManager struct{ key codec.Key }

func (f fakeKeyDEKManager) Resolve(_ context.Context, _ codec.KeyID, _ uint32) (codec.Key, error) {
	return f.key, nil
}

// fakeErrorDEKManager is a SessionDEKManager that always returns an error.
type fakeErrorDEKManager struct{}

func (fakeErrorDEKManager) Resolve(_ context.Context, _ codec.KeyID, _ uint32) (codec.Key, error) {
	return codec.Key{}, errors.New("fake: DEK resolve failed")
}

// fakeErrorGuard is a SessionAuthGuard that always returns an error from Check.
type fakeErrorGuard struct{}

func (fakeErrorGuard) Check(_ context.Context, _ SessionCheckRequest) (SessionDecision, error) {
	return SessionDecision{}, errors.New("fake: guard check failed")
}

// recordingAuditEmitter captures EmitPluginDecrypt calls and returns the
// configured error on each call.
type recordingAuditEmitter struct {
	records []PluginDecryptRecord
	retErr  error
}

func (r *recordingAuditEmitter) EmitPluginDecrypt(_ context.Context, rec PluginDecryptRecord) error {
	r.records = append(r.records, rec)
	return r.retErr
}

// TestDecodeDeliveryAuthGuardPermitDecryptsAndDelivers verifies that when
// AuthGuard permits for a character identity, decodeDeliveryWithAuth decrypts
// the payload using the resolved DEK key and returns metadataOnly=false with
// the original plaintext.
func TestDecodeDeliveryAuthGuardPermitDecryptsAndDelivers(t *testing.T) {
	t.Parallel()

	key := newTestXChachaKey(t)
	plaintext := []byte(`{"say":"hello, world"}`)
	msg := encryptedSensitiveMsg(t, key, plaintext)

	identity := SessionIdentity{
		Kind:        IdentityKindCharacter,
		PlayerID:    "01TESTPLAYER01234567890A",
		CharacterID: "01TESTCHARACTER0123456A",
		BindingID:   "01TESTBINDING01234567AB",
	}

	event, metaOnly, err := decodeDeliveryWithAuth(
		context.Background(),
		msg,
		nil,
		identity,
		fakeAlwaysPermitGuard{},
		fakeKeyDEKManager{key: key},
		fakeNilAuditEmitter{},
	)
	require.NoError(t, err)
	assert.False(t, metaOnly, "permit decision must not stamp metadataOnly")
	assert.Equal(t, plaintext, event.Payload, "permit path must return decrypted plaintext")
}

// TestDecodeDeliveryAuthGuardPermitForPluginEmitsAuditAndDelivers verifies
// that when a plugin identity receives a permit decision, the audit emitter is
// called with the correct PluginDecryptRecord and the event is delivered with
// the decrypted payload.
func TestDecodeDeliveryAuthGuardPermitForPluginEmitsAuditAndDelivers(t *testing.T) {
	t.Parallel()

	key := newTestXChachaKey(t)
	plaintext := []byte(`{"pose":"waves hello"}`)
	msg := encryptedSensitiveMsg(t, key, plaintext)

	identity := SessionIdentity{
		Kind:       IdentityKindPlugin,
		PluginName: "mod-filter",
		InstanceID: "inst-01",
	}
	auditRec := &recordingAuditEmitter{}

	event, metaOnly, err := decodeDeliveryWithAuth(
		context.Background(),
		msg,
		nil,
		identity,
		fakeAlwaysPermitGuard{},
		fakeKeyDEKManager{key: key},
		auditRec,
	)
	require.NoError(t, err)
	assert.False(t, metaOnly, "plugin permit path must not stamp metadataOnly when audit succeeds")
	assert.Equal(t, plaintext, event.Payload, "plugin permit path must deliver plaintext")
	require.Len(t, auditRec.records, 1, "audit emitter must have been called once")
	assert.Equal(t, "mod-filter", auditRec.records[0].PluginName)
}

// TestDecodeDeliveryAuthGuardPermitForPluginQueueFullZerosPlaintextAndStampsMetadataOnly
// verifies the TOCTOU defense: when AUDIT_QUEUE_FULL is returned by the audit
// emitter, decodeDeliveryWithAuth must zero the plaintext buffer before
// returning a metadata-only event, ensuring the decrypted bytes cannot escape.
func TestDecodeDeliveryAuthGuardPermitForPluginQueueFullZerosPlaintextAndStampsMetadataOnly(t *testing.T) {
	t.Parallel()

	key := newTestXChachaKey(t)
	plaintext := []byte(`{"secret":"do not leak"}`)
	msg := encryptedSensitiveMsg(t, key, plaintext)

	identity := SessionIdentity{
		Kind:       IdentityKindPlugin,
		PluginName: "mod-filter",
	}

	// Inject a DEK manager that returns a key we can observe, and a
	// capturing audit emitter that returns AUDIT_QUEUE_FULL (oops-coded so
	// isAuditQueueFull recognises it as the TOCTOU-fallback trigger).
	auditErr := oops.Code("AUDIT_QUEUE_FULL").Errorf("fake: audit queue full")
	auditRec := &recordingAuditEmitter{retErr: auditErr}

	event, metaOnly, err := decodeDeliveryWithAuth(
		context.Background(),
		msg,
		nil,
		identity,
		fakeAlwaysPermitGuard{},
		fakeKeyDEKManager{key: key},
		auditRec,
	)
	require.NoError(t, err)
	assert.True(t, metaOnly, "AUDIT_QUEUE_FULL must stamp metadataOnly=true")
	assert.Empty(t, event.Payload, "AUDIT_QUEUE_FULL must return empty payload")
	// The audit emitter was called once (attempt was made).
	require.Len(t, auditRec.records, 1)
}

// TestDecodeDeliveryAuthGuardCheckErrorPropagates verifies that when
// AuthGuard.Check returns a non-nil error, decodeDeliveryWithAuth propagates
// it with the EVENTBUS_AUTHGUARD_CHECK_FAILED error code.
func TestDecodeDeliveryAuthGuardCheckErrorPropagates(t *testing.T) {
	t.Parallel()

	h := validSensitiveHeaders(t)
	msg := &stubMsg{headers: h, data: validSensitivePayload(t)}

	_, _, err := decodeDeliveryWithAuth(
		context.Background(),
		msg,
		nil,
		SessionIdentity{Kind: IdentityKindCharacter},
		fakeErrorGuard{},
		fakeDEKManager{},
		fakeNilAuditEmitter{},
	)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_AUTHGUARD_CHECK_FAILED")
}

// TestDecodeDeliveryAuthGuardPermitDEKResolveErrorPropagates verifies that
// when the DEK manager returns an error after a permit decision,
// decodeDeliveryWithAuth propagates the error with EVENTBUS_DEK_RESOLVE_FAILED.
func TestDecodeDeliveryAuthGuardPermitDEKResolveErrorPropagates(t *testing.T) {
	t.Parallel()

	h := validSensitiveHeaders(t)
	msg := &stubMsg{headers: h, data: validSensitivePayload(t)}

	_, _, err := decodeDeliveryWithAuth(
		context.Background(),
		msg,
		nil,
		SessionIdentity{Kind: IdentityKindCharacter},
		fakeAlwaysPermitGuard{},
		fakeErrorDEKManager{},
		fakeNilAuditEmitter{},
	)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_DEK_RESOLVE_FAILED")
}

// TestDecodeDeliveryAuthGuardPermitDecodeErrorPropagates verifies that when
// the codec.Decode step fails (wrong key / tampered ciphertext), the error
// propagates with EVENTBUS_CODEC_DECODE_FAILED.
func TestDecodeDeliveryAuthGuardPermitDecodeErrorPropagates(t *testing.T) {
	t.Parallel()

	// Build a valid encrypted message, then supply a different key so AEAD
	// tag verification fails.
	realKey := newTestXChachaKey(t)
	wrongKey := newTestXChachaKey(t)
	msg := encryptedSensitiveMsg(t, realKey, []byte("secret"))

	_, _, err := decodeDeliveryWithAuth(
		context.Background(),
		msg,
		nil,
		SessionIdentity{Kind: IdentityKindCharacter},
		fakeAlwaysPermitGuard{},
		fakeKeyDEKManager{key: wrongKey},
		fakeNilAuditEmitter{},
	)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_CODEC_DECODE_FAILED")
}

// --- Round 5: nil-dekMgr and INV-CRYPTO-11 error paths ---

// TestDecodeDeliveryNilDEKManagerAfterPermitFailsClosed verifies that when
// the AuthGuard permits but dekMgr is nil (misconfiguration), decodeAndAuthorize
// fails closed with EVENTBUS_DEK_MANAGER_NIL rather than panicking.
func TestDecodeDeliveryNilDEKManagerAfterPermitFailsClosed(t *testing.T) {
	t.Parallel()

	h := validSensitiveHeaders(t)
	msg := &stubMsg{headers: h, data: validSensitivePayload(t)}

	_, _, err := decodeDeliveryWithAuth(
		context.Background(),
		msg,
		nil,
		SessionIdentity{Kind: IdentityKindCharacter},
		fakeAlwaysPermitGuard{},
		nil, // nil dekMgr — the misconfiguration under test
		fakeNilAuditEmitter{},
	)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_DEK_MANAGER_NIL")
}

// TestDecodeDeliveryPluginIdentityWithNilAuditEmitterFailsClosed verifies
// INV-CRYPTO-11: when a plugin identity receives a permit decision but the audit
// emitter is nil (configuration error), decodeAndAuthorize must fail closed
// with EVENTBUS_AUDIT_EMITTER_NIL rather than delivering plaintext without
// an audit record.
func TestDecodeDeliveryPluginIdentityWithNilAuditEmitterFailsClosed(t *testing.T) {
	t.Parallel()

	key := newTestXChachaKey(t)
	plaintext := []byte(`{"secret":"must not leak"}`)
	msg := encryptedSensitiveMsg(t, key, plaintext)

	identity := SessionIdentity{
		Kind:       IdentityKindPlugin,
		PluginName: "mod-filter",
	}

	_, _, err := decodeDeliveryWithAuth(
		context.Background(),
		msg,
		nil,
		identity,
		fakeAlwaysPermitGuard{},
		fakeKeyDEKManager{key: key},
		nil, // nil auditEm — the misconfiguration under test
	)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_AUDIT_EMITTER_NIL")
}

// failingAuditEmitterWithCode is a SessionAuditEmitter that always returns an
// oops error with the given error code. Used to distinguish AUDIT_QUEUE_FULL
// (metadata-only fallback) from unexpected errors (fail closed).
type failingAuditEmitterWithCode struct {
	code string
}

func (f failingAuditEmitterWithCode) EmitPluginDecrypt(_ context.Context, _ PluginDecryptRecord) error {
	return oops.Code(f.code).Errorf("fake: audit emit failed with code %s", f.code)
}

// TestDecodeDeliveryPluginNonQueueFullAuditErrorPropagates verifies that when
// the audit emitter returns a non-AUDIT_QUEUE_FULL error, decodeAndAuthorize
// propagates it as EVENTBUS_AUDIT_EMIT_FAILED (fail closed). This distinguishes
// the "audit might not have landed" case from the narrow TOCTOU fallback.
func TestDecodeDeliveryPluginNonQueueFullAuditErrorPropagates(t *testing.T) {
	t.Parallel()

	key := newTestXChachaKey(t)
	plaintext := []byte(`{"secret":"must not leak"}`)
	msg := encryptedSensitiveMsg(t, key, plaintext)

	identity := SessionIdentity{
		Kind:       IdentityKindPlugin,
		PluginName: "mod-filter",
	}

	_, _, err := decodeDeliveryWithAuth(
		context.Background(),
		msg,
		nil,
		identity,
		fakeAlwaysPermitGuard{},
		fakeKeyDEKManager{key: key},
		failingAuditEmitterWithCode{code: "AUDIT_EMITTER_FAILED"},
	)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_AUDIT_EMIT_FAILED")
}

// TestDecodeDeliveryPluginAuditQueueFullStillStampsMetadataOnly verifies that
// the AUDIT_QUEUE_FULL code (specifically) triggers the TOCTOU plaintext-zero +
// metadata_only fallback, and distinguishes it from the non-queue-full path.
func TestDecodeDeliveryPluginAuditQueueFullStillStampsMetadataOnly(t *testing.T) {
	t.Parallel()

	key := newTestXChachaKey(t)
	plaintext := []byte(`{"secret":"do not leak"}`)
	msg := encryptedSensitiveMsg(t, key, plaintext)

	identity := SessionIdentity{
		Kind:       IdentityKindPlugin,
		PluginName: "mod-filter",
	}

	event, metaOnly, err := decodeDeliveryWithAuth(
		context.Background(),
		msg,
		nil,
		identity,
		fakeAlwaysPermitGuard{},
		fakeKeyDEKManager{key: key},
		failingAuditEmitterWithCode{code: "AUDIT_QUEUE_FULL"},
	)
	require.NoError(t, err)
	assert.True(t, metaOnly, "AUDIT_QUEUE_FULL must stamp metadataOnly=true")
	assert.Empty(t, event.Payload, "AUDIT_QUEUE_FULL must return empty payload")
}
