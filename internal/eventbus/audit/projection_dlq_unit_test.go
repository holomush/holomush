// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit

import (
	"context"
	"errors"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDLQCapturer records Capture invocations and returns a configurable
// error, so handle()'s Term/Nak decision is deterministic without a broker.
type fakeDLQCapturer struct {
	calls int
	err   error
}

func (f *fakeDLQCapturer) Capture(_ context.Context, _ jetstream.Msg) error {
	f.calls++
	return f.err
}

// recordingMsg augments stubMsg with Term/Nak/Ack counters so branch
// tests can assert exactly which acknowledgement path handle() took.
type recordingMsg struct {
	stubMsg
	termCalls int
	nakCalls  int
	ackCalls  int
}

func (m *recordingMsg) Term() error { m.termCalls++; return nil }
func (m *recordingMsg) Nak() error  { m.nakCalls++; return nil }
func (m *recordingMsg) Ack() error  { m.ackCalls++; return nil }

// poisonMsg builds a message that fails persist() deterministically and
// early (missing Nats-Msg-Id, so pool is never touched) with the given
// delivery count.
func poisonMsg(numDelivered uint64) *recordingMsg {
	return &recordingMsg{
		stubMsg: stubMsg{
			headers: nats.Header{}, // no Nats-Msg-Id ⇒ persist fails immediately
			subject: "events.main.poison",
			meta:    &jetstream.MsgMetadata{NumDelivered: numDelivered},
		},
	}
}

func newDLQTestProjection(dlq dlqCapturer, maxDeliver int) *projection {
	return &projection{
		cfg:       Config{MaxDeliver: maxDeliver},
		dlq:       dlq,
		workerCtx: context.Background(),
	}
}

func TestHandleCapturesAndTermsOnFinalAttempt(t *testing.T) {
	t.Parallel()
	dlq := &fakeDLQCapturer{}
	p := newDLQTestProjection(dlq, 3)

	msg := poisonMsg(3) // NumDelivered == MaxDeliver ⇒ final attempt
	p.handle(msg)

	assert.Equal(t, 1, dlq.calls, "Capture must be called once on the final attempt")
	assert.Equal(t, 1, msg.termCalls, "successful DLQ capture must Term the original")
	assert.Equal(t, 0, msg.nakCalls, "no Nak when DLQ capture succeeds")
	assert.Equal(t, 0, msg.ackCalls, "poison messages are never Ack'd")
}

func TestHandleLeavesMessageUnackedWhenDLQPublishFails(t *testing.T) {
	t.Parallel()
	dlq := &fakeDLQCapturer{err: errors.New("dlq publish failed")}
	p := newDLQTestProjection(dlq, 3)

	msg := poisonMsg(4) // past MaxDeliver
	p.handle(msg)

	// On a failed DLQ capture the message is left un-acked: never Term'd
	// (Term would drop the only copy without a durable capture) and never
	// Nak'd (past the MaxDeliver ceiling a Nak buys no redelivery). It stays
	// in the source EVENTS stream until StreamMaxAge (never silently dropped).
	require.Equal(t, 1, dlq.calls, "Capture is attempted on the final attempt")
	assert.Equal(t, 0, msg.termCalls, "MUST NOT Term before a successful DLQ publish (would drop)")
	assert.Equal(t, 0, msg.nakCalls, "MUST NOT Nak past MaxDeliver — redelivery is impossible there")
	assert.Equal(t, 0, msg.ackCalls, "poison message stays un-acked, retained in source stream until MaxAge")
}

func TestHandleLeavesSubCapPoisonUnacked(t *testing.T) {
	t.Parallel()
	dlq := &fakeDLQCapturer{}
	p := newDLQTestProjection(dlq, 3)

	msg := poisonMsg(1) // below the cap
	p.handle(msg)

	assert.Equal(t, 0, dlq.calls, "no DLQ capture below MaxDeliver")
	assert.Equal(t, 0, msg.termCalls, "no Term below the cap (unchanged AckWait backoff)")
	assert.Equal(t, 0, msg.nakCalls, "MUST NOT Nak on the ordinary sub-cap poison path")
	assert.Equal(t, 0, msg.ackCalls, "sub-cap poison stays unacked for AckWait redelivery")
}
