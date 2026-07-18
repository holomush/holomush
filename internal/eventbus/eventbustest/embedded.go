// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package eventbustest provides test helpers for the embedded JetStream
// event bus. Tests SHOULD use New(t) to get a fresh, in-memory bus per test.
//
// Each New(t) call boots its own embedded NATS server with MemoryStorage
// and DontListen + InProcessServer. There is no network and no shared state,
// so tests are safe to run in parallel.
package eventbustest

import (
	"context"
	"errors"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
)

// DefaultAwaitTimeout bounds Await* helpers. Five seconds is generous for
// embedded-server round trips; its only purpose is to fail loudly on a
// deadlock, not to hide latency (spec §8 — "synchronization points are
// observable metrics, not timers").
const DefaultAwaitTimeout = 5 * time.Second

// awaitPollInterval governs how often Await* helpers re-read JS state. Short
// enough that a well-behaved system adds negligible observation latency,
// long enough to avoid hammering the server.
const awaitPollInterval = 5 * time.Millisecond

// TB is the subset of testing.TB that eventbustest actually uses. Accepting
// this narrower interface lets Ginkgo's GinkgoT() interface value work as a
// drop-in without a parallel helper.
type TB interface {
	require.TestingT
	Helper()
	Cleanup(func())
	TempDir() string
	Logf(format string, args ...any)
}

// Embedded bundles the bus subsystem with its JetStream context and
// connection so tests can interact directly.
type Embedded struct {
	Bus  *eventbus.Subsystem
	JS   jetstream.JetStream
	Conn *nats.Conn
}

// New starts a fresh embedded NATS server with MemoryStorage and registers
// cleanup on t.Cleanup. Per-test isolation; safe for t.Parallel.
//
// Accepts TB (our subset of testing.TB) so Ginkgo's GinkgoT() works here as
// well as plain *testing.T.
//
// StoreDir is set to t.TempDir() even though MemoryStorage is in use: the
// NATS server still writes its JetStream metadata (streams, consumers)
// under StoreDir, and leaving it unset would race on the shared xdg path.
func New(t TB) *Embedded {
	t.Helper()
	cfg := eventbus.Config{
		StoreDir: t.TempDir(),
	}.Defaults()
	bus := eventbus.NewSubsystemWithStorage(cfg, jetstream.MemoryStorage)
	require.NoError(t, bus.Prepare(context.Background()))
	require.NoError(t, bus.Activate(context.Background())) // no-op today (D-13.3 row 2); called for interface completeness
	t.Cleanup(func() {
		if err := bus.Stop(context.Background()); err != nil {
			t.Logf("eventbustest: bus.Stop error: %v", err)
		}
	})
	return &Embedded{
		Bus:  bus,
		JS:   bus.JS(),
		Conn: bus.Conn(),
	}
}

// AwaitStreamLastSeq blocks until the EVENTS stream's LastSeq reaches (or
// exceeds) want, or until timeout. Tests use this as a publish barrier: a
// caller that just published N messages calls AwaitStreamLastSeq(t, N) to
// ensure the server has committed them before opening a subscriber or
// validating state.
//
// Uses JS StreamInfo polling, not time.Sleep (forbidigo-banned across
// internal/eventbus/**).
func (e *Embedded) AwaitStreamLastSeq(t TB, want uint64, timeout time.Duration) {
	t.Helper()
	if timeout <= 0 {
		timeout = DefaultAwaitTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		info, err := streamInfo(e.JS, time.Until(deadline))
		if err == nil && info.State.LastSeq >= want {
			return
		}
		if !time.Now().Before(deadline) {
			var got uint64
			if err == nil {
				got = info.State.LastSeq
			}
			t.Logf("AwaitStreamLastSeq: want>=%d got=%d err=%v", want, got, err)
			require.FailNow(t, "AwaitStreamLastSeq deadline exceeded")
		}
		<-time.After(awaitPollInterval)
	}
}

// AwaitAckedSeq blocks until the named durable consumer's AckFloor stream
// sequence reaches (or exceeds) want. Used by subscriber tests to establish
// a server-confirmed ack barrier before, e.g., closing + reopening a
// session, without racing the durable cursor.
//
// See spec §8 "Controllable test seams (no hidden waits)" —
// AwaitAckedSeq replaces raw Eventually-on-data-channels as the canonical
// synchronization primitive for subscriber assertions.
func (e *Embedded) AwaitAckedSeq(t TB, consumerName string, want uint64, timeout time.Duration) {
	t.Helper()
	if timeout <= 0 {
		timeout = DefaultAwaitTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		info, err := consumerInfo(e.JS, consumerName, time.Until(deadline))
		if err == nil && info.AckFloor.Stream >= want {
			return
		}
		if !time.Now().Before(deadline) {
			var got uint64
			if err == nil {
				got = info.AckFloor.Stream
			}
			t.Logf("AwaitAckedSeq: consumer=%s want>=%d got=%d err=%v", consumerName, want, got, err)
			require.FailNow(t, "AwaitAckedSeq deadline exceeded")
		}
		<-time.After(awaitPollInterval)
	}
}

// AwaitDeliveredSeq blocks until the named durable consumer's
// Delivered.Stream sequence reaches (or exceeds) want. Useful when the test
// cares that JS has handed a specific seq to the client (as opposed to
// acked; see AwaitAckedSeq for the stricter barrier).
func (e *Embedded) AwaitDeliveredSeq(t TB, consumerName string, want uint64, timeout time.Duration) {
	t.Helper()
	if timeout <= 0 {
		timeout = DefaultAwaitTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		info, err := consumerInfo(e.JS, consumerName, time.Until(deadline))
		if err == nil && info.Delivered.Stream >= want {
			return
		}
		if !time.Now().Before(deadline) {
			var got uint64
			if err == nil {
				got = info.Delivered.Stream
			}
			t.Logf("AwaitDeliveredSeq: consumer=%s want>=%d got=%d err=%v", consumerName, want, got, err)
			require.FailNow(t, "AwaitDeliveredSeq deadline exceeded")
		}
		<-time.After(awaitPollInterval)
	}
}

// RawMessagesOnSubject returns up to limit messages from the EVENTS stream
// matching the given subject. Uses an ephemeral ordered consumer so multiple
// calls are independent. Polls until the consumer drains; no time.Sleep.
func (e *Embedded) RawMessagesOnSubject(t TB, subject string, limit int, timeout time.Duration) []*nats.Msg {
	t.Helper()
	if limit < 0 {
		require.FailNow(t, "RawMessagesOnSubject requires a non-negative limit")
	}
	if limit == 0 {
		return nil
	}
	if timeout <= 0 {
		timeout = DefaultAwaitTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cons, err := e.JS.OrderedConsumer(ctx, eventbus.StreamName, jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{subject},
		DeliverPolicy:  jetstream.DeliverAllPolicy,
	})
	require.NoError(t, err)

	const fetchMaxWait = 200 * time.Millisecond
	out := make([]*nats.Msg, 0, limit)
	for len(out) < limit {
		// Honor the overall timeout: if the deadline is closer than
		// fetchMaxWait, shrink the per-fetch wait so we exit promptly.
		wait := fetchMaxWait
		if d, ok := ctx.Deadline(); ok {
			if remaining := time.Until(d); remaining <= 0 {
				break
			} else if remaining < wait {
				wait = remaining
			}
		}
		msgs, fetchErr := cons.Fetch(limit-len(out), jetstream.FetchMaxWait(wait))
		if fetchErr != nil && !errors.Is(fetchErr, nats.ErrTimeout) {
			require.NoError(t, fetchErr)
		}
		for msg := range msgs.Messages() {
			raw := &nats.Msg{
				Subject: msg.Subject(),
				Reply:   msg.Reply(),
				Header:  msg.Headers(),
				Data:    msg.Data(),
			}
			out = append(out, raw)
			require.NoError(t, msg.Ack())
		}
		// Continue polling until the timeout elapses or limit is reached.
		// An empty fetch is not a stopping signal — messages may arrive in
		// the next fetch window. The for-loop's len(out) < limit guard +
		// ctx.Deadline() check above are the only termination conditions.
	}
	return out
}

// infoAttemptTimeout caps a single JS info RPC. Independent of the caller's
// overall Await* deadline; see attemptCtx below for how they compose.
const infoAttemptTimeout = time.Second

// attemptCtx returns a context bound to min(infoAttemptTimeout, remaining).
// Callers with a short overall deadline get a correspondingly short per-poll
// attempt instead of blocking ~1s on a single info RPC — which would defeat
// these helpers' role as deadlock guards.
func attemptCtx(remaining time.Duration) (context.Context, context.CancelFunc) {
	attempt := infoAttemptTimeout
	if remaining < attempt {
		attempt = remaining
	}
	if attempt <= 0 {
		attempt = time.Millisecond
	}
	return context.WithTimeout(context.Background(), attempt)
}

func streamInfo(js jetstream.JetStream, remaining time.Duration) (*jetstream.StreamInfo, error) {
	ctx, cancel := attemptCtx(remaining)
	defer cancel()
	s, err := js.Stream(ctx, eventbus.StreamName)
	if err != nil {
		//nolint:wrapcheck // test helper — surface raw error
		return nil, err
	}
	//nolint:wrapcheck // test helper — surface raw error
	return s.Info(ctx)
}

func consumerInfo(js jetstream.JetStream, name string, remaining time.Duration) (*jetstream.ConsumerInfo, error) {
	ctx, cancel := attemptCtx(remaining)
	defer cancel()
	s, err := js.Stream(ctx, eventbus.StreamName)
	if err != nil {
		//nolint:wrapcheck // test helper — surface raw error
		return nil, err
	}
	c, err := s.Consumer(ctx, name)
	if err != nil {
		//nolint:wrapcheck // test helper — surface raw error
		return nil, err
	}
	//nolint:wrapcheck // test helper — surface raw error
	return c.Info(ctx)
}
