// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package sysbroadcast

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/pkg/errutil"
)

// fakePublisher is a hand-rolled eventbus.Publisher recording every published
// event — modeled on internal/presence's fakePublisher, extended with the
// same record/err-injection shape.
type fakePublisher struct {
	mu        sync.Mutex
	published []eventbus.Event
	err       error
}

func (f *fakePublisher) Publish(_ context.Context, ev eventbus.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.published = append(f.published, ev)
	return nil
}

func (f *fakePublisher) events() []eventbus.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]eventbus.Event(nil), f.published...)
}

func mainGameID() string { return "main" }

func TestNewBroadcasterPanicsOnNilPublisher(t *testing.T) {
	assert.Panics(t, func() {
		NewBroadcaster(nil, mainGameID)
	}, "NewBroadcaster must reject a nil Publisher so callers fail fast at construction")
}

func TestNewBroadcasterPanicsOnTypedNilPublisher(t *testing.T) {
	// A typed-nil (*fakePublisher)(nil) is NOT caught by a naive `== nil`
	// guard because the interface wraps a non-nil type descriptor. The
	// constructor uses reflection (isNilPublisher) to detect this so
	// misconfiguration surfaces at construction time rather than on first
	// Broadcast call (WR-02).
	var nilPub *fakePublisher
	assert.Panics(t, func() {
		NewBroadcaster(nilPub, mainGameID)
	}, "typed-nil publisher must panic at construction, not on first use")
}

func TestNewBroadcasterPanicsOnNilGameID(t *testing.T) {
	assert.Panics(t, func() {
		NewBroadcaster(&fakePublisher{}, nil)
	}, "NewBroadcaster must reject a nil gameID func so callers fail fast at construction")
}

// TestBroadcastPublishesSystemEventWithExactPayloadBytes proves the payload
// is byte-identical to today's json.Marshal(map[string]string{"message": ...}).
func TestBroadcastPublishesSystemEventWithExactPayloadBytes(t *testing.T) {
	pub := &fakePublisher{}
	b := NewBroadcaster(pub, mainGameID)

	err := b.Broadcast(context.Background(), core.SystemBroadcastSubject, "hello")
	require.NoError(t, err)

	events := pub.events()
	require.Len(t, events, 1)
	assert.Equal(t, []byte(`{"message":"hello"}`), events[0].Payload)
}

// TestBroadcastStampsSystemActorAndType proves the published event carries
// the eventbus-typed system actor and event type.
func TestBroadcastStampsSystemActorAndType(t *testing.T) {
	pub := &fakePublisher{}
	b := NewBroadcaster(pub, mainGameID)

	err := b.Broadcast(context.Background(), core.SystemBroadcastSubject, "hello")
	require.NoError(t, err)

	events := pub.events()
	require.Len(t, events, 1)
	ev := events[0]
	assert.Equal(t, eventbus.Type("system"), ev.Type)
	assert.Equal(t, eventbus.Actor{Kind: eventbus.ActorKindSystem, ID: core.SystemActorULID}, ev.Actor)
}

// TestBroadcastQualifiesSubjectByExactLiteral is the FINDING-5 pin: the
// published Subject MUST be the fully-qualified form of the caller's
// subject, asserted here against a literal string — NOT by recomputing
// eventbus.Qualify(...) in the test (which would be tautological and would
// pass even if the builder skipped qualification).
func TestBroadcastQualifiesSubjectByExactLiteral(t *testing.T) {
	pub := &fakePublisher{}
	b := NewBroadcaster(pub, mainGameID)

	err := b.Broadcast(context.Background(), core.SystemBroadcastSubject, "hello")
	require.NoError(t, err)

	events := pub.events()
	require.Len(t, events, 1)
	assert.Equal(t, eventbus.Subject("events.main.system"), events[0].Subject,
		"a relative subject must never reach Publish — it must be fully qualified first")
}

// TestBroadcastFallsBackToMainGameIDWhenGameIDFuncReturnsEmpty proves the
// gameID()=="" -> "main" fallback, matching presence.Emitter's fallback.
func TestBroadcastFallsBackToMainGameIDWhenGameIDFuncReturnsEmpty(t *testing.T) {
	pub := &fakePublisher{}
	b := NewBroadcaster(pub, func() string { return "" })

	err := b.Broadcast(context.Background(), core.SystemBroadcastSubject, "hello")
	require.NoError(t, err)

	events := pub.events()
	require.Len(t, events, 1)
	assert.Equal(t, eventbus.Subject("events.main.system"), events[0].Subject)
}

// TestBroadcastWrapsPublishFailureAsSystemBroadcastFailed proves a publish
// failure surfaces the SYSTEM_BROADCAST_FAILED oops code hostcap used
// before this collapse.
func TestBroadcastWrapsPublishFailureAsSystemBroadcastFailed(t *testing.T) {
	pub := &fakePublisher{err: errors.New("bus unavailable")}
	b := NewBroadcaster(pub, mainGameID)

	err := b.Broadcast(context.Background(), core.SystemBroadcastSubject, "hello")

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SYSTEM_BROADCAST_FAILED")
}
