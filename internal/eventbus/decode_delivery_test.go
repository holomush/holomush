// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

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
