// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

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

	"github.com/holomush/holomush/internal/eventbus"
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
	ev, err := decodeJetStreamMessage(context.Background(), msg, nil)
	require.NoError(t, err)
	assert.Equal(t, id, ev.ID)
	assert.Equal(t, eventbus.Subject("events.main.audit"), ev.Subject)
}

func TestDecodeJetStreamMessageRejectsMissingMsgID(t *testing.T) {
	t.Parallel()
	h, _ := validHeaders(t)
	h.Del(eventbus.HeaderMsgID)
	msg := &stubMsg{headers: h, data: validPayload(t)}
	_, err := decodeJetStreamMessage(context.Background(), msg, nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_HISTORY_MISSING_HEADER")
}

func TestDecodeJetStreamMessageRejectsBadMsgID(t *testing.T) {
	t.Parallel()
	h, _ := validHeaders(t)
	h.Set(eventbus.HeaderMsgID, "nope")
	msg := &stubMsg{headers: h, data: validPayload(t)}
	_, err := decodeJetStreamMessage(context.Background(), msg, nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_HISTORY_BAD_MSG_ID")
}

func TestDecodeJetStreamMessageRejectsMissingCodec(t *testing.T) {
	t.Parallel()
	h, _ := validHeaders(t)
	h.Del(eventbus.HeaderCodec)
	msg := &stubMsg{headers: h, data: validPayload(t)}
	_, err := decodeJetStreamMessage(context.Background(), msg, nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_HISTORY_MISSING_HEADER")
}

func TestDecodeJetStreamMessageRejectsUnknownCodec(t *testing.T) {
	t.Parallel()
	h, _ := validHeaders(t)
	h.Set(eventbus.HeaderCodec, "bogus-codec")
	msg := &stubMsg{headers: h, data: validPayload(t)}
	_, err := decodeJetStreamMessage(context.Background(), msg, nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_HISTORY_UNKNOWN_CODEC")
}

func TestDecodeJetStreamMessageRejectsMalformedProto(t *testing.T) {
	t.Parallel()
	h, _ := validHeaders(t)
	msg := &stubMsg{headers: h, data: []byte("not-proto-bytes-at-all")}
	_, err := decodeJetStreamMessage(context.Background(), msg, nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_HISTORY_UNMARSHAL_FAILED")
}
