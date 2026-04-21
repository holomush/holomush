// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbustest_test

import (
	"context"
	crand "crypto/rand"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
)

// Round 3 coverage: the Await* helpers in embedded.go underpin every
// subscriber integration test. They MUST themselves carry coverage so a
// regression in their polling loop does not silently mask a server-side
// stall the helpers exist to detect.

// testEntropy is a monotonic ULID entropy source — keeps ULIDs unique
// across back-to-back publishes in the same millisecond.
var testEntropy = ulid.Monotonic(cryptoRandReader{}, 0)

type cryptoRandReader struct{}

func (cryptoRandReader) Read(p []byte) (int, error) { return crand.Read(p) }

func mintEvent(subject eventbus.Subject) eventbus.Event {
	return eventbus.Event{
		ID:        ulid.MustNew(ulid.Timestamp(time.Now()), testEntropy),
		Subject:   subject,
		Type:      eventbus.Type("scene.pose"),
		Timestamp: time.Now().UTC(),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
		Payload:   []byte("p"),
	}
}

func TestAwaitStreamLastSeqReturnsOnReach(t *testing.T) {
	embedded := eventbustest.New(t)
	pub := embedded.Bus.Publisher()
	require.NotNil(t, pub)
	ctx := context.Background()
	require.NoError(t, pub.Publish(ctx, mintEvent(eventbus.Subject("events.main.await.seq"))))
	// DefaultAwaitTimeout flows when timeout==0 — exercise that branch too.
	embedded.AwaitStreamLastSeq(t, 1, 0)
}

func TestAwaitAckedSeqReachesTargetAfterServerConfirmedAck(t *testing.T) {
	embedded := eventbustest.New(t)
	pub := embedded.Bus.Publisher()
	sub := embedded.Bus.Subscriber()
	subject := eventbus.Subject("events.main.await.acked")
	sessionID := ulid.MustNew(ulid.Timestamp(time.Now()), testEntropy).String()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := sub.OpenSession(ctx, sessionID, []eventbus.Subject{subject})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	require.NoError(t, pub.Publish(ctx, mintEvent(subject)))
	d, err := stream.Next(ctx)
	require.NoError(t, err)
	require.NoError(t, eventbus.AckSyncForTest(ctx, d))

	// Consumer name matches sessionConsumerName(sessionID).
	embedded.AwaitAckedSeq(t, "session_"+sessionID, 1, 2*time.Second)
}

func TestAwaitDeliveredSeqReachesTargetAfterServerDelivers(t *testing.T) {
	embedded := eventbustest.New(t)
	pub := embedded.Bus.Publisher()
	sub := embedded.Bus.Subscriber()
	subject := eventbus.Subject("events.main.await.delivered")
	sessionID := ulid.MustNew(ulid.Timestamp(time.Now()), testEntropy).String()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := sub.OpenSession(ctx, sessionID, []eventbus.Subject{subject})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	require.NoError(t, pub.Publish(ctx, mintEvent(subject)))
	d, err := stream.Next(ctx)
	require.NoError(t, err)
	// Do NOT Ack — AwaitDeliveredSeq cares about Delivered.Stream, which
	// ticks on delivery independent of ack.
	_ = d

	embedded.AwaitDeliveredSeq(t, "session_"+sessionID, 1, 2*time.Second)
}

// failRecorderTB captures FailNow via panic-unwind so we can exercise the
// deadline-exceeded branch of Await* without aborting the test. The real
// testing.T.FailNow is runtime.Goexit-based; a plain bool flag is
// insufficient because the Await* poll loop unconditionally re-enters
// <-time.After() after calling require.FailNow.
type failRecorderTB struct {
	*testing.T
	failed bool
}

type failNowSentinel struct{}

func (f *failRecorderTB) FailNow()                  { f.failed = true; panic(failNowSentinel{}) }
func (f *failRecorderTB) Errorf(_ string, _ ...any) {}
func (f *failRecorderTB) Logf(_ string, _ ...any)   {}
func (f *failRecorderTB) Helper()                   {}
func (f *failRecorderTB) Cleanup(fn func())         { f.T.Cleanup(fn) }
func (f *failRecorderTB) TempDir() string           { return f.T.TempDir() }

// runExpectingFailNow invokes fn and recovers the failNowSentinel panic.
// Any other panic propagates normally so real bugs surface.
func runExpectingFailNow(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(failNowSentinel); ok {
				return
			}
			panic(r) // real panic — re-raise
		}
	}()
	fn()
}

func TestAwaitAckedSeqFailsWhenDeadlineElapses(t *testing.T) {
	embedded := eventbustest.New(t)
	rec := &failRecorderTB{T: t}
	runExpectingFailNow(t, func() {
		embedded.AwaitAckedSeq(rec, "nonexistent_consumer", 1, 50*time.Millisecond)
	})
	assert.True(t, rec.failed, "AwaitAckedSeq must FailNow on deadline")
}

func TestAwaitDeliveredSeqFailsWhenDeadlineElapses(t *testing.T) {
	embedded := eventbustest.New(t)
	rec := &failRecorderTB{T: t}
	runExpectingFailNow(t, func() {
		embedded.AwaitDeliveredSeq(rec, "nonexistent_consumer", 1, 50*time.Millisecond)
	})
	assert.True(t, rec.failed)
}

func TestAwaitStreamLastSeqFailsWhenDeadlineElapses(t *testing.T) {
	embedded := eventbustest.New(t)
	rec := &failRecorderTB{T: t}
	runExpectingFailNow(t, func() {
		embedded.AwaitStreamLastSeq(rec, 999999, 50*time.Millisecond)
	})
	assert.True(t, rec.failed)
}

func TestEmbeddedExposesBusAndJS(t *testing.T) {
	e := eventbustest.New(t)
	require.NotNil(t, e.Bus)
	require.NotNil(t, e.JS)
	require.NotNil(t, e.Conn)
}
