// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/pkg/errutil"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// envelopeInput parametrises buildEnvelopeBytes for dispatcher tests that
// need to pin the envelope's projection fields and payload (typically to
// assert byte-equal forwarding under a non-identity codec).
type envelopeInput struct {
	Subject   string
	EventType string
	Payload   []byte
}

// buildEnvelopeBytes proto-marshals an eventbusv1.Event with the given
// projection fields. Used by ciphertext-forward tests so the dispatcher
// path can read projection fields out of msg.Data() while Payload is
// whatever opaque bytes the test wants the dispatcher to forward
// verbatim.
func buildEnvelopeBytes(t *testing.T, in envelopeInput) []byte {
	t.Helper()
	subj := in.Subject
	if subj == "" {
		subj = "events.test.scene.01ABC.ic"
	}
	typ := in.EventType
	if typ == "" {
		typ = "test-plugin:secret"
	}
	e := &eventbusv1.Event{
		Id:        ulid.Make().Bytes(),
		Subject:   subj,
		Type:      typ,
		Timestamp: timestamppb.New(time.Unix(1, 0)),
		Payload:   in.Payload,
	}
	b, err := proto.Marshal(e)
	require.NoError(t, err)
	return b
}

// stubPluginAuditClient is a minimal PluginAuditClient for dispatch tests.
type stubPluginAuditClient struct {
	called bool
	err    error
	gotReq *pluginv1.AuditEventRequest
}

func (s *stubPluginAuditClient) AuditEvent(_ context.Context, req *pluginv1.AuditEventRequest) (*pluginv1.AuditEventResponse, error) {
	s.called = true
	s.gotReq = req
	if s.err != nil {
		return nil, s.err
	}
	return &pluginv1.AuditEventResponse{}, nil
}

// validPluginHeaders builds a complete header set for the plugin dispatch path.
func validPluginHeaders(t *testing.T) nats.Header {
	t.Helper()
	h := nats.Header{}
	h.Set(headerMsgID, ulid.Make().String())
	h.Set(headerCodec, "identity")
	h.Set(headerEventType, "plugin.test")
	h.Set(headerSchemaVersion, "1")
	h.Set(headerActorKind, defaultActorKind)
	return h
}

// newValidEnvelopeBytes proto-marshals a valid Event envelope so
// unmarshalProjectionOnly returns success.
func newValidEnvelopeBytes(t *testing.T) []byte {
	t.Helper()
	e := &eventbusv1.Event{
		Id:        ulid.Make().Bytes(),
		Subject:   "events.main.plugin.test",
		Type:      "plugin.test",
		Timestamp: timestamppb.New(time.Unix(1, 0)),
		Payload:   []byte(`{"ok":1}`),
	}
	b, err := proto.Marshal(e)
	require.NoError(t, err)
	return b
}

func TestPluginConsumerDispatchSuccess(t *testing.T) {
	t.Parallel()
	cli := &stubPluginAuditClient{}
	pc := &pluginConsumer{
		cfg: PluginConsumerConfig{PluginName: "core-scenes", Client: cli},
	}
	msg := &stubMsg{
		headers: validPluginHeaders(t),
		subject: "events.main.plugin.test",
		data:    newValidEnvelopeBytes(t),
	}
	require.NoError(t, pc.dispatch(msg))
	assert.True(t, cli.called)
	require.NotNil(t, cli.gotReq)
	row := cli.gotReq.GetRow()
	require.NotNil(t, row)
	// INV-CRYPTO-38: Row carries projection fields parsed from the envelope.
	assert.Equal(t, "events.main.plugin.test", row.GetSubject())
	assert.Equal(t, "plugin.test", row.GetType())
	// INV-CRYPTO-39: Codec + SchemaVer come from header parser, byte-equal to host projection.
	assert.Equal(t, "identity", row.GetCodec())
	assert.Equal(t, int32(1), row.GetSchemaVer())
	// Identity codec ⇒ no DEK fields (M3 follow-up: Row assertions per INV-CRYPTO-38).
	assert.Nil(t, row.DekRef)
	assert.Nil(t, row.DekVersion)
}

func TestPluginConsumerDispatchMissingMsgID(t *testing.T) {
	t.Parallel()
	pc := &pluginConsumer{
		cfg: PluginConsumerConfig{PluginName: "p", Client: &stubPluginAuditClient{}},
	}
	h := validPluginHeaders(t)
	h.Del(headerMsgID)
	err := pc.dispatch(&stubMsg{headers: h, data: newValidEnvelopeBytes(t)})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_PLUGIN_MISSING_HEADER")
}

func TestPluginConsumerDispatchBadMsgID(t *testing.T) {
	t.Parallel()
	pc := &pluginConsumer{
		cfg: PluginConsumerConfig{PluginName: "p", Client: &stubPluginAuditClient{}},
	}
	h := validPluginHeaders(t)
	h.Set(headerMsgID, "not-a-ulid")
	err := pc.dispatch(&stubMsg{headers: h, data: newValidEnvelopeBytes(t)})
	require.Error(t, err)
	// errutil unwraps to the innermost oops code — decodeULIDString wraps
	// AUDIT_BAD_ULID which is then wrapped by AUDIT_PLUGIN_BAD_MSG_ID.
	errutil.AssertErrorCode(t, err, "AUDIT_BAD_ULID")
}

// TestDispatchForwardsCiphertextByteEqual — INV-CRYPTO-38. Per Phase 7 spec
// §3 + §5.1, the dispatcher MUST forward the envelope's payload bytes
// byte-equal to the plugin (no decryption before forward).
func TestDispatchForwardsCiphertextByteEqual(t *testing.T) {
	t.Parallel()
	cli := &stubPluginAuditClient{}
	pc := &pluginConsumer{
		cfg: PluginConsumerConfig{PluginName: "p", Client: cli},
	}

	ciphertext := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	envelopeBytes := buildEnvelopeBytes(t, envelopeInput{
		Subject:   "events.test.scene.01ABC.ic",
		EventType: "test-plugin:secret",
		Payload:   ciphertext,
	})
	h := validPluginHeaders(t)
	h.Set(headerCodec, "xchacha20poly1305-v1")
	h.Set(eventbus.HeaderDekRef, "42")
	h.Set(eventbus.HeaderDekVersion, "7")

	require.NoError(t, pc.dispatch(&stubMsg{
		headers: h,
		subject: "events.test.scene.01ABC.ic",
		data:    envelopeBytes,
	}))

	require.True(t, cli.called)
	row := cli.gotReq.GetRow()
	require.NotNil(t, row)
	assert.Equal(t, ciphertext, row.GetPayload(),
		"INV-CRYPTO-38: dispatcher MUST forward ciphertext byte-equal")
	assert.Equal(t, "xchacha20poly1305-v1", row.GetCodec())
	require.NotNil(t, row.DekRef)
	assert.Equal(t, uint64(42), row.GetDekRef())
	require.NotNil(t, row.DekVersion)
	assert.Equal(t, uint32(7), row.GetDekVersion())
	assert.Equal(t, int32(1), row.GetSchemaVer())
}

// TestDispatchDoesNotDecryptBeforeForward — INV-CRYPTO-46. The dispatcher
// MUST NOT invoke any codec.Decode path before forwarding to the plugin.
// We verify this by setting the App-Codec header to a non-identity value
// AND providing a payload that would NOT round-trip through Decode (no
// real DEK is wired). With the widened dispatcher the bytes are
// forwarded verbatim regardless.
func TestDispatchDoesNotDecryptBeforeForward(t *testing.T) {
	t.Parallel()
	cli := &stubPluginAuditClient{}
	pc := &pluginConsumer{
		cfg: PluginConsumerConfig{PluginName: "p", Client: cli},
	}

	ciphertext := []byte("garbage-not-a-real-aead-payload")
	envelopeBytes := buildEnvelopeBytes(t, envelopeInput{
		Subject:   "events.test.scene.01ABC.ic",
		EventType: "test-plugin:secret",
		Payload:   ciphertext,
	})
	h := validPluginHeaders(t)
	h.Set(headerCodec, "xchacha20poly1305-v1")

	require.NoError(t, pc.dispatch(&stubMsg{
		headers: h,
		subject: "events.test.scene.01ABC.ic",
		data:    envelopeBytes,
	}))
	require.True(t, cli.called)
	assert.Equal(t, ciphertext, cli.gotReq.GetRow().GetPayload())
}

// TestDispatchRejectsBadSchemaVersion exercises the AUDIT_BAD_SCHEMA_VERSION
// branch through ParseAuditHeaders → AUDIT_PLUGIN_HEADER_PARSE_FAILED wrap.
// (M2 follow-up from 1r0v.1: cover the schema_ver branch.)
func TestDispatchRejectsBadSchemaVersion(t *testing.T) {
	t.Parallel()
	pc := &pluginConsumer{
		cfg: PluginConsumerConfig{PluginName: "p", Client: &stubPluginAuditClient{}},
	}
	h := validPluginHeaders(t)
	h.Set(headerSchemaVersion, "not-a-number")
	err := pc.dispatch(&stubMsg{
		headers: h,
		subject: "events.main.plugin.test",
		data:    newValidEnvelopeBytes(t),
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_BAD_SCHEMA_VERSION")
}

func TestPluginConsumerDispatchRejectsMissingCodecHeader(t *testing.T) {
	t.Parallel()
	pc := &pluginConsumer{
		cfg: PluginConsumerConfig{PluginName: "p", Client: &stubPluginAuditClient{}},
	}
	h := validPluginHeaders(t)
	h.Del(headerCodec)
	err := pc.dispatch(&stubMsg{
		headers: h,
		subject: "events.main.plugin.test",
		data:    newValidEnvelopeBytes(t),
	})
	require.Error(t, err)
	// errutil unwraps to the innermost oops code — ParseAuditHeaders returns
	// AUDIT_MISSING_HEADER which is wrapped by AUDIT_PLUGIN_HEADER_PARSE_FAILED.
	errutil.AssertErrorCode(t, err, "AUDIT_MISSING_HEADER")
}

func TestPluginConsumerDispatchPropagatesClientError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("plugin RPC down")
	cli := &stubPluginAuditClient{err: sentinel}
	pc := &pluginConsumer{
		cfg: PluginConsumerConfig{PluginName: "p", Client: cli},
	}
	err := pc.dispatch(&stubMsg{
		headers: validPluginHeaders(t),
		subject: "events.main.plugin.test",
		data:    newValidEnvelopeBytes(t),
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_PLUGIN_DISPATCH_FAILED")
	require.ErrorIs(t, err, sentinel)
}

func TestPluginConsumerDispatchRejectsUnmarshalableEnvelope(t *testing.T) {
	t.Parallel()
	pc := &pluginConsumer{
		cfg: PluginConsumerConfig{PluginName: "p", Client: &stubPluginAuditClient{}},
	}
	// Garbage bytes will fail proto.Unmarshal.
	err := pc.dispatch(&stubMsg{
		headers: validPluginHeaders(t),
		subject: "events.main.plugin.test",
		data:    []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
	})
	require.Error(t, err)
	// Phase 7: buildAuditRow returns AUDIT_PLUGIN_ENVELOPE_UNMARSHAL_FAILED
	// directly (no longer wrapped by the removed AUDIT_PLUGIN_DECODE_FAILED).
	errutil.AssertErrorCode(t, err, "AUDIT_PLUGIN_ENVELOPE_UNMARSHAL_FAILED")
}

func TestPluginDurableNameMatchesSpecConvention(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "plugin_audit_core-scenes", pluginDurableName("core-scenes"))
}

func TestAddRejectsEmptyPluginName(t *testing.T) {
	t.Parallel()
	m := NewPluginConsumerManager(nil)
	err := m.Add(context.Background(), PluginConsumerConfig{})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_PLUGIN_CONSUMER_INVALID_CONFIG")
}

func TestAddRejectsEmptySubjects(t *testing.T) {
	t.Parallel()
	m := NewPluginConsumerManager(nil)
	err := m.Add(context.Background(), PluginConsumerConfig{
		PluginName: "p",
		Client:     &stubPluginAuditClient{},
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_PLUGIN_CONSUMER_INVALID_CONFIG")
}

func TestAddRejectsNilClient(t *testing.T) {
	t.Parallel()
	m := NewPluginConsumerManager(nil)
	err := m.Add(context.Background(), PluginConsumerConfig{
		PluginName: "p",
		Subjects:   []string{"events.main.p.*"},
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_PLUGIN_CONSUMER_INVALID_CONFIG")
}

// TestWrapPluginConsumerCreateErrorSurfacesUnderlyingNATSError pins the
// observability contract for plugin Add(): when CreateOrUpdateConsumer
// ultimately fails, the wrapped error MUST surface the underlying NATS
// error message via a structured `nats_err` field. Without this, oops
// Code() / structured-field consumers (Ginkgo's failure summary,
// errutil.AssertErrorContext) see only AUDIT_PLUGIN_CONSUMER_CREATE_FAILED
// and cannot diagnose root cause — the same defect that blocked l015
// diagnosis on the host-projection side, now closed on the plugin side
// (holomush-ghg1 follow-up).
func TestWrapPluginConsumerCreateErrorSurfacesUnderlyingNATSError(t *testing.T) {
	t.Parallel()
	natsErr := errors.New("nats: no stream matches subject")
	wrapped := wrapPluginConsumerCreateError(natsErr, "core-scenes", "plugin_audit_core-scenes")

	errutil.AssertErrorCode(t, wrapped, "AUDIT_PLUGIN_CONSUMER_CREATE_FAILED")
	require.ErrorIs(t, wrapped, natsErr,
		"chain MUST preserve the underlying NATS error for errors.Is/As consumers")
	require.ErrorContains(t, wrapped, "no stream matches subject",
		"wrapped error chain MUST contain the underlying NATS message")
	errutil.AssertErrorContext(t, wrapped, "stream", eventbus.StreamName)
	errutil.AssertErrorContext(t, wrapped, "plugin", "core-scenes")
	errutil.AssertErrorContext(t, wrapped, "consumer", "plugin_audit_core-scenes")
	errutil.AssertErrorContext(t, wrapped, "nats_err", natsErr.Error())
}
