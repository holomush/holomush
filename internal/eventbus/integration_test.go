// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package eventbus_test

import (
	"context"
	crand "crypto/rand"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
)

// TestCrossSubjectSequenceOrderingUnderConcurrentPublishers ports the
// "Variant A Go/No-Go" invariant test that was deleted with F7 (it lived in
// the legacy postgres_integration_test.go:574-683 against the pgnotify
// event store).
//
// Original invariant: under N concurrent publishers across M subjects and
// P subscribers, every subscriber sees a strictly-ascending sequence AND
// both subscribers see IDENTICAL sequences. The JetStream cutover preserves
// this via JS's per-stream monotonically-increasing sequence: all events
// published to the EVENTS stream receive a global stream seq, so every
// subscriber (durable consumer) reading the same stream sees the same seq
// values in the same order, regardless of which subject they filter on.
//
// Shape: 1000 events × 4 subjects × 10 goroutines × 2 subscribers.
//
// Synchronization: NO time.Sleep. We use:
//
//   - AwaitStreamLastSeq as the publish barrier (server has committed all
//     messages before we start draining — prevents subscriber racing the
//     last publish and returning early).
//   - Context deadlines on the per-subscriber drain loops.
//
// Spec reference: §8 "Invariant tests (I-14 cross-subject sequence
// ordering under concurrent publishers)" + §8 "Controllable test seams
// (no hidden waits)".
func TestCrossSubjectSequenceOrderingUnderConcurrentPublishers(t *testing.T) {
	const (
		totalEvents       = 1000
		subjectCount      = 4
		publisherRoutines = 10
		subscriberCount   = 2
		// Test-tuned ack wait: short enough that a handler pause surfaces
		// fast, long enough that a loaded CI box doesn't redeliver under
		// normal processing.
		testAckWait = 2 * time.Second
	)

	embedded := eventbustest.New(t)
	pub := embedded.Bus.Publisher()
	require.NotNil(t, pub)

	// Build the subject universe. All subjects match the stream filter
	// "events.>" so the durable consumers below will see every event.
	subjects := make([]eventbus.Subject, subjectCount)
	for i := range subjects {
		subjects[i] = eventbus.Subject(fmt.Sprintf("events.main.loadgen.s%d", i))
	}

	// Open subscribers BEFORE publishing so the durable consumers start
	// from seq 1. Without this, a DeliverAllPolicy consumer still sees all
	// events (JS replays from seq 1), but the test is clearer when the
	// consumer existed throughout.
	subSvc := embedded.Bus.Subscriber(eventbus.WithSessionAckWait(testAckWait))
	require.NotNil(t, subSvc)

	sessionIDs := make([]string, subscriberCount)
	streams := make([]eventbus.SessionStream, subscriberCount)
	for i := range subscriberCount {
		// Fresh crypto/rand entropy per session ID — the package-level
		// testEntropy is monotonic and racey; this loop doesn't need
		// monotonicity (just uniqueness) so use unseeded crypto/rand.
		sid := ulid.MustNew(ulid.Timestamp(time.Now()), crand.Reader).String()
		sessionIDs[i] = sid
		s, err := subSvc.OpenSession(t.Context(), sid, testIdentity(), subjects)
		require.NoError(t, err)
		t.Cleanup(func() { _ = s.Close() })
		streams[i] = s
	}

	// Fan out publishers. Each goroutine owns a disjoint slice of the
	// [0,totalEvents) range so there are no duplicate ULIDs; subjects are
	// assigned round-robin so the load is balanced across subjects.
	perRoutine := totalEvents / publisherRoutines
	var pubWG sync.WaitGroup
	pubCtx, pubCancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer pubCancel()

	var pubErr atomicErr
	for r := range publisherRoutines {
		pubWG.Add(1)
		go func(routine int) {
			defer pubWG.Done()
			// Per-goroutine monotonic entropy: within a single ms, a
			// single goroutine's ULIDs stay strictly ascending. The
			// package-level testEntropy can't be used concurrently
			// (ulid.MonotonicEntropy has shared state and no lock).
			entropy := ulid.Monotonic(crand.Reader, 0)
			for j := range perRoutine {
				idx := routine*perRoutine + j
				subj := subjects[idx%subjectCount]
				evt := eventbus.Event{
					ID:        ulid.MustNew(ulid.Timestamp(time.Now()), entropy),
					Subject:   subj,
					Type:      eventbus.Type("scene.pose"),
					Timestamp: time.Now().UTC(),
					Actor:     eventbus.Actor{Kind: eventbus.ActorKindSystem},
					Payload:   []byte(fmt.Sprintf(`{"n":%d}`, idx)),
				}
				if err := pub.Publish(pubCtx, evt); err != nil {
					pubErr.set(err)
					return
				}
			}
		}(r)
	}
	pubWG.Wait()
	require.NoError(t, pubErr.get(), "publisher goroutine errored")

	// Barrier: the server has committed all messages. Without this, a
	// subscriber drain loop can time out before the last publish arrives
	// on the stream.
	embedded.AwaitStreamLastSeq(t, totalEvents, 10*time.Second)

	// Drain each subscriber. Each collects the full (seq, ULID) sequence
	// it observes. After both complete, we assert:
	//
	//  1) Each subscriber saw exactly totalEvents events.
	//  2) Each subscriber's seq sequence is strictly ascending.
	//  3) Both subscribers saw the IDENTICAL sequence of (seq, ULID) —
	//     same seq order, same ULIDs, same subjects.
	type observation struct {
		seq     uint64
		id      ulid.ULID
		subject eventbus.Subject
	}

	observations := make([][]observation, subscriberCount)
	var drainWG sync.WaitGroup
	var drainErr atomicErr
	for i := range subscriberCount {
		drainWG.Add(1)
		go func(idx int) {
			defer drainWG.Done()
			stream := streams[idx]
			obs := make([]observation, 0, totalEvents)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			for range totalEvents {
				d, err := stream.Next(ctx)
				if err != nil {
					drainErr.set(fmt.Errorf("subscriber %d: %w", idx, err))
					return
				}
				meta, err := jetStreamMeta(d)
				if err != nil {
					drainErr.set(fmt.Errorf("subscriber %d metadata: %w", idx, err))
					return
				}
				if err := d.Ack(); err != nil {
					drainErr.set(fmt.Errorf("subscriber %d ack: %w", idx, err))
					return
				}
				obs = append(obs, observation{
					seq:     meta.Sequence.Stream,
					id:      d.Event().ID,
					subject: d.Event().Subject,
				})
			}
			observations[idx] = obs
		}(i)
	}
	drainWG.Wait()
	require.NoError(t, drainErr.get())

	// Assertion 1: both subscribers saw all events.
	for i, obs := range observations {
		require.Len(t, obs, totalEvents, "subscriber %d delivered count", i)
	}

	// Assertion 2: strictly ascending seq per subscriber.
	for i, obs := range observations {
		for k := 1; k < len(obs); k++ {
			require.Greater(t, obs[k].seq, obs[k-1].seq,
				"subscriber %d saw non-monotonic seq at position %d: %d then %d",
				i, k, obs[k-1].seq, obs[k].seq)
		}
	}

	// Assertion 3: both subscribers observed identical sequences. Same
	// seq, same ULID, same subject at every position. This is the core
	// JetStream invariant that replaces the pgnotify "every subscriber
	// sees the same global event id sequence" contract.
	for k := range totalEvents {
		a := observations[0][k]
		b := observations[1][k]
		assert.Equal(t, a.seq, b.seq, "subscribers diverged at position %d (seq)", k)
		assert.Equal(t, a.id, b.id, "subscribers diverged at position %d (id)", k)
		assert.Equal(t, a.subject, b.subject, "subscribers diverged at position %d (subject)", k)
	}
}

// jetStreamMeta pulls the jetstream.MsgMetadata from a Delivery. The
// Delivery interface does not expose seq directly (production code doesn't
// need it — ordering is a JS contract), but the test needs the seq to
// assert the invariant. We reach through the concrete type to get it.
func jetStreamMeta(d eventbus.Delivery) (*jetstream.MsgMetadata, error) {
	return eventbus.DeliveryMetadataForTest(d)
}

// atomicErr captures the first error from a fan-out of goroutines; further
// writes are ignored so one failure's message doesn't overwrite another.
type atomicErr struct {
	mu  sync.Mutex
	err error
}

func (a *atomicErr) set(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err == nil {
		a.err = err
	}
}

func (a *atomicErr) get() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.err
}
