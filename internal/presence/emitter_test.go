// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package presence

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventvocab"
)

// fakePublisher is a hand-rolled eventbus.Publisher recording every
// published event — modeled on the noop-fake idiom in
// internal/plugin/setup/system_broadcaster_test.go (coretest retires in
// 07-07 and production code may not import it anyway — depguard), extended
// to record instead of discard since these tests assert published shape.
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

func TestNewEmitterAcceptsPublisher(t *testing.T) {
	e := NewEmitter(&fakePublisher{}, mainGameID)
	assert.NotNil(t, e)
}

func TestNewEmitterPanicsOnNilPublisher(t *testing.T) {
	assert.Panics(t, func() {
		NewEmitter(nil, mainGameID)
	}, "NewEmitter must reject a nil Publisher so callers fail fast at construction")
}

func TestNewEmitterPanicsOnTypedNilPublisher(t *testing.T) {
	// A typed-nil (*fakePublisher)(nil) is NOT caught by a naive `== nil`
	// guard because the interface wraps a non-nil type descriptor. The
	// constructor uses reflection (isNilPublisher) to detect this so
	// misconfiguration surfaces at construction time rather than on first
	// Emit* call.
	var nilPub *fakePublisher
	assert.Panics(t, func() {
		_ = NewEmitter(nilPub, mainGameID)
	}, "typed-nil publisher must panic at construction, not on first use")
}

func TestNewEmitterPanicsOnNilGameID(t *testing.T) {
	assert.Panics(t, func() {
		NewEmitter(&fakePublisher{}, nil)
	}, "NewEmitter must reject a nil gameID func so callers fail fast at construction")
}

func TestEmitArrivePublishesArriveEventWithCharacterPayload(t *testing.T) {
	pub := &fakePublisher{}
	e := NewEmitter(pub, mainGameID)

	charID := core.NewULID()
	locationID := core.NewULID()
	char := core.CharacterRef{ID: charID, Name: "Alyssa", LocationID: locationID}

	err := e.EmitArrive(context.Background(), char)
	require.NoError(t, err)

	events := pub.events()
	require.Len(t, events, 1)
	ev := events[0]
	assert.Equal(t, eventbus.Type(eventvocab.EventTypeArrive), ev.Type)
	assert.Equal(t, eventbus.Subject("events.main.location."+locationID.String()), ev.Subject)
	assert.Equal(t, eventbus.ActorKindCharacter, ev.Actor.Kind)
	assert.Equal(t, charID, ev.Actor.ID)

	var payload ArrivePayload
	require.NoError(t, json.Unmarshal(ev.Payload, &payload))
	assert.Equal(t, "Alyssa", payload.CharacterName)
}

func TestEmitLeavePublishesLeaveEventWithReasonPayload(t *testing.T) {
	pub := &fakePublisher{}
	e := NewEmitter(pub, mainGameID)

	charID := core.NewULID()
	locationID := core.NewULID()
	char := core.CharacterRef{ID: charID, Name: "Alyssa", LocationID: locationID}

	err := e.EmitLeave(context.Background(), char, "quit")
	require.NoError(t, err)

	events := pub.events()
	require.Len(t, events, 1)
	ev := events[0]
	assert.Equal(t, eventbus.Type(eventvocab.EventTypeLeave), ev.Type)
	assert.Equal(t, eventbus.Subject("events.main.location."+locationID.String()), ev.Subject)
	assert.Equal(t, eventbus.ActorKindCharacter, ev.Actor.Kind)
	assert.Equal(t, charID, ev.Actor.ID)

	var payload LeavePayload
	require.NoError(t, json.Unmarshal(ev.Payload, &payload))
	assert.Equal(t, "Alyssa", payload.CharacterName)
	assert.Equal(t, "quit", payload.Reason)
}

// TestEmitArrivePublishesOnFullyQualifiedSubject is the FINDING-5 pin: the
// published Subject MUST be exactly eventbus.Qualify(gameID(), relRef) —
// asserted here against a literal string built by concatenation, NOT by
// recomputing eventbus.Qualify(...) in the test (which would be tautological
// and would pass even if the emitter skipped qualification).
func TestEmitArrivePublishesOnFullyQualifiedSubject(t *testing.T) {
	pub := &fakePublisher{}
	e := NewEmitter(pub, mainGameID)

	locationID := core.NewULID()
	char := core.CharacterRef{ID: core.NewULID(), Name: "Alyssa", LocationID: locationID}

	err := e.EmitArrive(context.Background(), char)
	require.NoError(t, err)

	events := pub.events()
	require.Len(t, events, 1)
	wantSubject := eventbus.Subject("events.main.location." + locationID.String())
	assert.Equal(t, wantSubject, events[0].Subject,
		"a relative subject must never reach Publish — it must be fully qualified first")
}

// TestEmitArriveFallsBackToMainGameIDWhenGameIDFuncReturnsEmpty proves the
// gameID()=="" -> "main" fallback, the same fallback the retired JetStream
// event-appender had.
func TestEmitArriveFallsBackToMainGameIDWhenGameIDFuncReturnsEmpty(t *testing.T) {
	pub := &fakePublisher{}
	e := NewEmitter(pub, func() string { return "" })

	locationID := core.NewULID()
	char := core.CharacterRef{ID: core.NewULID(), Name: "Alyssa", LocationID: locationID}

	err := e.EmitArrive(context.Background(), char)
	require.NoError(t, err)

	events := pub.events()
	require.Len(t, events, 1)
	assert.True(t, strings.HasPrefix(string(events[0].Subject), "events.main."),
		"an empty gameID() must fall back to 'main'")
}
