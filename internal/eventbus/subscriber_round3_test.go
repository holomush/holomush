// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	"github.com/holomush/holomush/pkg/errutil"
)

// Round 3: close remaining gaps in subscriber.go — AckSyncForTest
// fall-through, DeliveryMetadataForTest unknown-impl error, SetFilters
// against a subsystem whose JS was cleared, and Next when the iterator is
// already Closed (inbox draining branch).

// plainDelivery satisfies eventbus.Delivery but is not the jetStreamDelivery
// impl. Forces AckSyncForTest to take the fall-through branch where it
// calls d.Ack() instead of msg.DoubleAck.
type plainDelivery struct {
	acked   bool
	nacked  bool
	event   eventbus.Event
	ackErr  error
	nackErr error
}

func (p *plainDelivery) Event() eventbus.Event { return p.event }
func (p *plainDelivery) Ack() error            { p.acked = true; return p.ackErr }
func (p *plainDelivery) Nack() error           { p.nacked = true; return p.nackErr }
func (p *plainDelivery) InProgress() error     { return nil }

func TestAckSyncForTestFallsBackToPlainAckForNonJetstreamDelivery(t *testing.T) {
	t.Parallel()
	d := &plainDelivery{}
	require.NoError(t, eventbus.AckSyncForTest(context.Background(), d))
	assert.True(t, d.acked, "fall-through path delegates to Ack()")
}

func TestAckSyncForTestForwardsAckErrorForNonJetstreamDelivery(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("ack down")
	d := &plainDelivery{ackErr: sentinel}
	err := eventbus.AckSyncForTest(context.Background(), d)
	require.Error(t, err)
	require.ErrorIs(t, err, sentinel)
}

func TestDeliveryMetadataForTestRejectsUnknownImpl(t *testing.T) {
	t.Parallel()
	_, err := eventbus.DeliveryMetadataForTest(&plainDelivery{})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_DELIVERY_UNKNOWN_IMPL")
}

func TestDeliveryMetadataForTestReturnsMetadataForJetstreamImpl(t *testing.T) {
	// Exercise the happy path — metadata call against a real delivery.
	embedded := eventbustest.New(t)
	pub := embedded.Bus.Publisher()
	sub := embedded.Bus.Subscriber()
	subject := eventbus.Subject("events.main.metadata.test")
	sessionID := freshSessionID()
	ctx := context.Background()
	stream, err := sub.OpenSession(ctx, sessionID, []eventbus.Subject{subject})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })
	require.NoError(t, pub.Publish(ctx, newTestEnvelope(subject, []byte("m"))))
	d, err := stream.Next(ctx)
	require.NoError(t, err)
	md, err := eventbus.DeliveryMetadataForTest(d)
	require.NoError(t, err)
	require.NotNil(t, md)
	assert.NotZero(t, md.Sequence.Stream, "server-assigned stream seq must be non-zero")
}

// TestSessionStreamNextReturnsIteratorClosedErrorAfterClose exercises the
// "inbox closed, receiver in Next sees ok=false" branch that Close
// deliberately engineers. Previously the subscriber test suite only
// exercised the ctx.Done() branch of Next.
func TestSessionStreamNextReturnsIteratorClosedErrorAfterClose(t *testing.T) {
	embedded := eventbustest.New(t)
	sub := embedded.Bus.Subscriber()
	sessionID := freshSessionID()
	stream, err := sub.OpenSession(context.Background(), sessionID,
		[]eventbus.Subject{eventbus.Subject("events.main.close.next")})
	require.NoError(t, err)
	// Close first; next call observes the closed channel.
	require.NoError(t, stream.Close())

	// Next() MUST return the concrete closed-iterator sentinel, not any
	// error. Using errors.Is pins the branch that Close() deliberately
	// engineers (inbox closed → ok=false → wrap jetstream.ErrMsgIteratorClosed).
	_, err = stream.Next(context.Background())
	require.ErrorIs(t, err, jetstream.ErrMsgIteratorClosed)
}
