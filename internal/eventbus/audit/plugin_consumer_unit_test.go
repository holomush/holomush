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

	"github.com/holomush/holomush/pkg/errutil"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

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

// newValidEnvelopeBytes proto-marshals a valid Event envelope so decodeEnvelope
// returns success.
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
	assert.NotEmpty(t, cli.gotReq.GetHeaders())
	assert.Equal(t, "events.main.plugin.test", cli.gotReq.GetEvent().GetSubject())
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

func TestPluginConsumerDispatchRejectsNonIdentityCodec(t *testing.T) {
	t.Parallel()
	pc := &pluginConsumer{
		cfg: PluginConsumerConfig{PluginName: "p", Client: &stubPluginAuditClient{}},
	}
	h := validPluginHeaders(t)
	h.Set(headerCodec, "aes-gcm-v1")
	err := pc.dispatch(&stubMsg{
		headers: h,
		subject: "events.main.plugin.test",
		data:    newValidEnvelopeBytes(t),
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AUDIT_PLUGIN_CODEC_UNSUPPORTED")
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
	errutil.AssertErrorCode(t, err, "AUDIT_PLUGIN_MISSING_HEADER")
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
	// Innermost oops code surfaces — AUDIT_PLUGIN_ENVELOPE_UNMARSHAL_FAILED
	// is the direct cause; the outer AUDIT_PLUGIN_DECODE_FAILED wraps it.
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
