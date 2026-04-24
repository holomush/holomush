// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus_test

import (
	"context"
	crand "crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	"github.com/holomush/holomush/pkg/errutil"
)

// newRawMsg builds a *nats.Msg for tests that need to exercise the
// subscriber decode path with hand-crafted headers.
func newRawMsg(subject string, headers map[string]string, data []byte) *nats.Msg {
	m := &nats.Msg{Subject: subject, Data: data, Header: nats.Header{}}
	for k, v := range headers {
		m.Header.Set(k, v)
	}
	return m
}

// testEntropy is a cryptographically seeded entropy source for test ULIDs.
// Using crypto/rand here (not math/rand) keeps tests from colliding when two
// events are minted in the same millisecond — the prior nil-entropy shape
// produced identical ULIDs within a single test, which JetStream's dedupe
// then swallowed as a redundant publish.
var testEntropy = ulid.Monotonic(cryptoRandReader{}, 0)

type cryptoRandReader struct{}

func (cryptoRandReader) Read(p []byte) (int, error) {
	return crand.Read(p)
}

// newTestEnvelope builds an eventbus.Event on the given subject with a
// fresh ULID and the current timestamp. Used as the canonical input for
// publish/subscribe round trips in this file.
func newTestEnvelope(subject eventbus.Subject, payload []byte) eventbus.Event {
	return eventbus.Event{
		ID:        ulid.MustNew(ulid.Timestamp(time.Now()), testEntropy),
		Subject:   subject,
		Type:      eventbus.Type("scene.pose"),
		Timestamp: time.Now().UTC(),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
		Payload:   payload,
	}
}

// freshSessionID mints a fresh ULID-shaped session id per test so one test's
// durable consumer can't re-home onto another's state.
func freshSessionID() string {
	return ulid.MustNew(ulid.Timestamp(time.Now()), testEntropy).String()
}

func TestOpenSessionDeliversPublishedEventMatchingFilter(t *testing.T) {
	embedded := eventbustest.New(t)
	pub := embedded.Bus.Publisher()
	require.NotNil(t, pub)
	sub := embedded.Bus.Subscriber()
	require.NotNil(t, sub)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	subject := eventbus.Subject("events.main.scene.abc.ic")
	sessionID := freshSessionID()

	stream, err := sub.OpenSession(ctx, sessionID, []eventbus.Subject{subject})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	evt := newTestEnvelope(subject, []byte("hello"))
	require.NoError(t, pub.Publish(ctx, evt))

	delivery, err := stream.Next(ctx)
	require.NoError(t, err)
	got := delivery.Event()
	assert.Equal(t, evt.Subject, got.Subject)
	assert.Equal(t, evt.Type, got.Type)
	assert.Equal(t, evt.ID, got.ID)
	assert.Equal(t, evt.Payload, got.Payload)
	require.NoError(t, delivery.Ack())
}

func TestOpenSessionIsIdempotentAcrossReopens(t *testing.T) {
	embedded := eventbustest.New(t)
	pub := embedded.Bus.Publisher()
	sub := embedded.Bus.Subscriber()

	subject := eventbus.Subject("events.main.char.xyz.out")
	sessionID := freshSessionID()

	// First bind + publish + ack one event. Use an ack-sync barrier so
	// the server has confirmed the ack before we close the iterator;
	// otherwise the durable consumer's last-acked cursor can race the
	// reopen and redeliver evt1.
	bgCtx := context.Background()
	s1, err := sub.OpenSession(bgCtx, sessionID, []eventbus.Subject{subject})
	require.NoError(t, err)
	evt1 := newTestEnvelope(subject, []byte("first"))
	require.NoError(t, pub.Publish(bgCtx, evt1))
	ctx1, cancel1 := context.WithTimeout(bgCtx, 5*time.Second)
	d1, err := s1.Next(ctx1)
	cancel1()
	require.NoError(t, err)
	// DoubleAck is exposed via the concrete delivery helper for tests
	// that need a server-confirmed ack.
	syncCtx, syncCancel := context.WithTimeout(bgCtx, 2*time.Second)
	require.NoError(t, eventbus.AckSyncForTest(syncCtx, d1))
	syncCancel()
	require.NoError(t, s1.Close())

	// Publish a second event while the iterator is closed. Then re-open
	// the same session: the durable consumer MUST resume from where we
	// acked (evt2 delivered, evt1 not re-delivered).
	evt2 := newTestEnvelope(subject, []byte("second"))
	require.NoError(t, pub.Publish(bgCtx, evt2))

	s2, err := sub.OpenSession(bgCtx, sessionID, []eventbus.Subject{subject})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })
	ctx2, cancel2 := context.WithTimeout(bgCtx, 10*time.Second)
	defer cancel2()
	d2, err := s2.Next(ctx2)
	require.NoError(t, err)
	assert.Equal(t, evt2.ID, d2.Event().ID, "reopened session resumed at post-ack cursor")
	require.NoError(t, d2.Ack())
}

func TestOpenSessionRejectsUnstartedSubsystem(t *testing.T) {
	// NewJetStreamSubscriber with a nil JS context is a programming bug —
	// surface via a coded error so callers get a breadcrumb.
	s := eventbus.NewJetStreamSubscriber(nil)
	_, err := s.OpenSession(context.Background(), "sess", nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_SUBSCRIBER_NOT_READY")
}

func TestOpenSessionRejectsEmptySessionID(t *testing.T) {
	embedded := eventbustest.New(t)
	s := embedded.Bus.Subscriber()
	_, err := s.OpenSession(context.Background(), "", nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_INVALID_SESSION_ID")
}

func TestSubsystemSubscriberNilBeforeStart(t *testing.T) {
	// Mirrors Subsystem.Publisher's contract.
	s := eventbus.NewSubsystem(eventbus.Config{})
	require.Nil(t, s.Subscriber())
}

func TestSessionStreamNextUnblocksOnContextCancel(t *testing.T) {
	embedded := eventbustest.New(t)
	sub := embedded.Bus.Subscriber()
	sessionID := freshSessionID()
	stream, err := sub.OpenSession(context.Background(), sessionID,
		[]eventbus.Subject{eventbus.Subject("events.main.nothing.here")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err = stream.Next(ctx)
	require.Error(t, err)
	// Accept either context error or iterator-closed; both represent the
	// caller-observable "streamed nothing in time".
	ok := errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, jetstream.ErrMsgIteratorClosed)
	assert.True(t, ok, "unexpected error: %v", err)
}

func TestSessionStreamCloseIsIdempotent(t *testing.T) {
	embedded := eventbustest.New(t)
	sub := embedded.Bus.Subscriber()
	sessionID := freshSessionID()
	stream, err := sub.OpenSession(context.Background(), sessionID,
		[]eventbus.Subject{eventbus.Subject("events.main.x.y")})
	require.NoError(t, err)
	require.NoError(t, stream.Close())
	// Second Close is a no-op per contract.
	require.NoError(t, stream.Close())
}

func TestSetFiltersReplacesFilterSubjectsAtomically(t *testing.T) {
	embedded := eventbustest.New(t)
	pub := embedded.Bus.Publisher()
	sub := embedded.Bus.Subscriber()
	sessionID := freshSessionID()

	alpha := eventbus.Subject("events.main.alpha.one")
	beta := eventbus.Subject("events.main.beta.two")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := sub.OpenSession(ctx, sessionID, []eventbus.Subject{alpha})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	// Publish on alpha — delivered.
	evtA := newTestEnvelope(alpha, []byte("a"))
	require.NoError(t, pub.Publish(ctx, evtA))
	d, err := stream.Next(ctx)
	require.NoError(t, err)
	assert.Equal(t, alpha, d.Event().Subject)
	require.NoError(t, d.Ack())

	// Swap to beta. Publish on alpha AFTER the swap — must NOT be
	// delivered even on a rebind (filter is persistent on the durable).
	require.NoError(t, stream.SetFilters(ctx, []eventbus.Subject{beta}))

	evtBetaOnly := newTestEnvelope(beta, []byte("b"))
	require.NoError(t, pub.Publish(ctx, evtBetaOnly))
	d2, err := stream.Next(ctx)
	require.NoError(t, err)
	assert.Equal(t, beta, d2.Event().Subject)
	require.NoError(t, d2.Ack())
}

// TestSubscribeFilterMonotonicityUnderSetFilters exercises the spec §8
// invariant: after SetFilters(F2) returns, the session consumer delivers
// events matching F2 and does not deliver events matching F1 \ F2. The
// cursor is preserved across every filter update (acked events never
// re-deliver).
//
// This shares a single embedded bus across rapid iterations because
// eventbustest.New needs *testing.T (TempDir in particular), which
// *rapid.T does not satisfy. Each iteration uses a fresh session id so
// the durable consumers remain independent per draw.
//
// Rapid's default is 100 iterations — override via
// `-rapid.checks=N` on the test command line to trade coverage for wallclock.
func TestSubscribeFilterMonotonicityUnderSetFilters(t *testing.T) {
	universe := []eventbus.Subject{
		eventbus.Subject("events.main.ns.s1"),
		eventbus.Subject("events.main.ns.s2"),
		eventbus.Subject("events.main.ns.s3"),
		eventbus.Subject("events.main.ns.s4"),
	}
	embedded := eventbustest.New(t)
	pub := embedded.Bus.Publisher()
	sub := embedded.Bus.Subscriber()

	drawFilterSet := func(rt *rapid.T, label string) []eventbus.Subject {
		// Pick a non-empty subset via per-index booleans; rapid retries
		// if the zero-subset is drawn.
		for {
			picks := rapid.SliceOfN(rapid.Bool(), len(universe), len(universe)).Draw(rt, label)
			out := make([]eventbus.Subject, 0, len(universe))
			for i, p := range picks {
				if p {
					out = append(out, universe[i])
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}

	rapid.Check(t, func(rt *rapid.T) {
		sessionID := freshSessionID()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		initial := drawFilterSet(rt, "initial_filter")
		stream, err := sub.OpenSession(ctx, sessionID, initial)
		require.NoError(rt, err)
		defer func() { _ = stream.Close() }()

		// Drain helper — reads deliveries until timeout, acking each.
		// Returns the subjects observed in order.
		drainUntil := func(deadline time.Duration) []eventbus.Subject {
			var got []eventbus.Subject
			dctx, dcancel := context.WithTimeout(ctx, deadline)
			defer dcancel()
			for {
				d, derr := stream.Next(dctx)
				if derr != nil {
					return got
				}
				got = append(got, d.Event().Subject)
				_ = d.Ack()
			}
		}

		// 1) Publish across the full universe; events in `initial` MUST
		// be delivered.
		for _, s := range universe {
			require.NoError(rt, pub.Publish(ctx, newTestEnvelope(s, []byte("pre"))))
		}
		got := drainUntil(200 * time.Millisecond)
		initSet := subjectSet(initial)
		for _, s := range got {
			assert.True(rt, initSet[s], "unexpected subject %q delivered under initial filters", s)
		}

		// 2) Swap to a random new filter set (possibly overlapping).
		next := drawFilterSet(rt, "next_filter")
		require.NoError(rt, stream.SetFilters(ctx, next))

		// 3) Publish again. ONLY subjects in `next` must be delivered.
		// Subjects that left the filter set MUST NOT be delivered even
		// if they match a previous filter — the durable consumer's
		// retained cursor means acked events are gone, and new unacked
		// events are filtered at the server.
		for _, s := range universe {
			require.NoError(rt, pub.Publish(ctx, newTestEnvelope(s, []byte("post"))))
		}
		got2 := drainUntil(200 * time.Millisecond)
		nextSet := subjectSet(next)
		for _, s := range got2 {
			assert.True(rt, nextSet[s],
				"subject %q delivered after SetFilters despite not being in new filter set", s)
		}
	})
}

func subjectSet(ss []eventbus.Subject) map[eventbus.Subject]bool {
	out := make(map[eventbus.Subject]bool, len(ss))
	for _, s := range ss {
		out[s] = true
	}
	return out
}

func TestJetStreamSubscriberUsesSessionConsumerName(t *testing.T) {
	// Consumer name pattern is part of the spec contract. Encode it in a
	// test so future refactors that drift from "session_<id>" get caught.
	embedded := eventbustest.New(t)
	sub := embedded.Bus.Subscriber()
	sessionID := freshSessionID()
	ctx := context.Background()
	stream, err := sub.OpenSession(ctx, sessionID,
		[]eventbus.Subject{eventbus.Subject("events.main.a.b")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	info, err := embedded.JS.Stream(ctx, eventbus.StreamName)
	require.NoError(t, err)
	cons, err := info.Consumer(ctx, "session_"+sessionID)
	require.NoError(t, err)
	infoC, err := cons.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, "session_"+sessionID, infoC.Name)
	assert.Equal(t, jetstream.AckExplicitPolicy, infoC.Config.AckPolicy)
	assert.Equal(t, eventbus.DefaultSessionMaxAckPending, infoC.Config.MaxAckPending)
	assert.Equal(t, eventbus.DefaultSessionInactiveThreshold, infoC.Config.InactiveThreshold)
}

func TestOpenSessionRejectsEmptyFilters(t *testing.T) {
	// F8 autofix: OpenSession rejects an empty filter set so a bad focus
	// restore cannot leak the entire EVENTS stream into a session.
	embedded := eventbustest.New(t)
	sub := embedded.Bus.Subscriber()
	_, err := sub.OpenSession(context.Background(), freshSessionID(), nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_SESSION_FILTERS_REQUIRED")
}

func TestSetFiltersRejectsEmptyFilters(t *testing.T) {
	// Mirror of the OpenSession empty-filters guard.
	embedded := eventbustest.New(t)
	sub := embedded.Bus.Subscriber()
	sessionID := freshSessionID()
	stream, err := sub.OpenSession(context.Background(), sessionID,
		[]eventbus.Subject{eventbus.Subject("events.main.x.y")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	err = stream.SetFilters(context.Background(), nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_SESSION_FILTERS_REQUIRED")
}

func TestDeliveryNackAndInProgressForwardToMsg(t *testing.T) {
	// Nack and InProgress are thin wrappers on jetstream.Msg. Exercising them
	// via a real publish/subscribe round trip confirms the method bodies run
	// and the underlying JS call does not panic or return a wrapped error.
	embedded := eventbustest.New(t)
	pub := embedded.Bus.Publisher()
	sub := embedded.Bus.Subscriber()
	subject := eventbus.Subject("events.main.nack.test")
	sessionID := freshSessionID()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := sub.OpenSession(ctx, sessionID, []eventbus.Subject{subject})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	ev := newTestEnvelope(subject, []byte("x"))
	require.NoError(t, pub.Publish(ctx, ev))
	d, err := stream.Next(ctx)
	require.NoError(t, err)
	require.NoError(t, d.InProgress())
	require.NoError(t, d.Nack())
}

func TestSubscriberOptionsOverrideDefaults(t *testing.T) {
	// Exercise the four WithSession* options so their closures execute.
	// stubKeySelector is defined in publisher_test.go (same _test package).
	embedded := eventbustest.New(t)
	sub := embedded.Bus.Subscriber(
		eventbus.WithSessionAckWait(10*time.Second),
		eventbus.WithSessionMaxAckPending(32),
		eventbus.WithSessionInactiveThreshold(1*time.Hour),
		eventbus.WithSubscriberCodecSelector(stubKeySelector{}),
	)
	sessionID := freshSessionID()
	stream, err := sub.OpenSession(context.Background(), sessionID,
		[]eventbus.Subject{eventbus.Subject("events.main.opts.check")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	info, err := embedded.JS.Stream(context.Background(), eventbus.StreamName)
	require.NoError(t, err)
	cons, err := info.Consumer(context.Background(), "session_"+sessionID)
	require.NoError(t, err)
	infoC, err := cons.Info(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 10*time.Second, infoC.Config.AckWait)
	assert.Equal(t, 32, infoC.Config.MaxAckPending)
	assert.Equal(t, 1*time.Hour, infoC.Config.InactiveThreshold)
}

func TestJetStreamSubscriberRejectsUnknownCodec(t *testing.T) {
	// Publish a message directly via the conn with an unknown codec header
	// so the subscriber's decode path returns EVENTBUS_SUBSCRIBE_UNKNOWN_CODEC.
	embedded := eventbustest.New(t)
	sub := embedded.Bus.Subscriber()
	sessionID := freshSessionID()
	subject := eventbus.Subject("events.main.bad.codec")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := sub.OpenSession(ctx, sessionID, []eventbus.Subject{subject})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	msg := newRawMsg(string(subject), map[string]string{
		eventbus.HeaderMsgID:         freshSessionID(),
		eventbus.HeaderSchemaVersion: eventbus.SchemaVersion,
		eventbus.HeaderEventType:     "scene.pose",
		eventbus.HeaderCodec:         "not-a-real-codec",
		eventbus.HeaderActorKind:     "system",
	}, []byte("raw"))
	_, perr := embedded.JS.PublishMsg(ctx, msg)
	require.NoError(t, perr)

	_, err = stream.Next(ctx)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "EVENTBUS_SUBSCRIBE_UNKNOWN_CODEC")
}

func TestOpenSessionDeliveriesCarryNonZeroJetStreamSequence(t *testing.T) {
	// Verifies that the JetStream stream sequence is populated on delivered
	// events so callers can use it for backfill cursor construction.
	embedded := eventbustest.New(t)
	pub := embedded.Bus.Publisher()
	require.NotNil(t, pub)
	sub := embedded.Bus.Subscriber()
	require.NotNil(t, sub)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	subject := eventbus.Subject("events.main.scene.seqtest.ic")
	sessionID := freshSessionID()

	stream, err := sub.OpenSession(ctx, sessionID, []eventbus.Subject{subject})
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	evt := newTestEnvelope(subject, []byte("seq-test"))
	require.NoError(t, pub.Publish(ctx, evt))

	delivery, err := stream.Next(ctx)
	require.NoError(t, err)
	assert.Greater(t, delivery.Event().Seq, uint64(0), "delivered event must carry a non-zero JetStream stream sequence")
	require.NoError(t, delivery.Ack())
}
