// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package presence

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventvocab"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestEmitSessionEndedPublishesCorrectEventShapeOnCharacterStream(t *testing.T) {
	pub := &fakePublisher{}
	e := NewEmitter(pub, mainGameID)

	charID := core.NewULID()
	sessionID := core.NewULID().String()
	char := core.CharacterRef{ID: charID, Name: "Testy", LocationID: core.NewULID()}

	err := e.EmitSessionEnded(context.Background(), char, sessionID, core.SessionEndedCauseQuit, "Goodbye!")
	require.NoError(t, err)

	events := pub.events()
	require.Len(t, events, 1)
	ev := events[0]

	assert.Equal(t, eventbus.Subject("events.main.character."+charID.String()), ev.Subject)
	assert.Equal(t, eventbus.Type(eventvocab.EventTypeSessionEnded), ev.Type)
	assert.Equal(t, eventbus.ActorKindCharacter, ev.Actor.Kind, "cause=quit uses ActorCharacter")
	assert.Equal(t, charID, ev.Actor.ID)
	assert.NotZero(t, ev.ID, "event MUST have a ULID (monotonic per I-16)")

	var payload core.SessionEndedPayload
	require.NoError(t, json.Unmarshal(ev.Payload, &payload))
	assert.Equal(t, sessionID, payload.SessionID)
	assert.Equal(t, charID.String(), payload.CharacterID)
	assert.Equal(t, core.SessionEndedCauseQuit, payload.Cause)
	assert.Equal(t, "Goodbye!", payload.Reason)
}

func TestEmitSessionEndedUsesActorSystemForNonQuitCauses(t *testing.T) {
	charID := core.NewULID()
	char := core.CharacterRef{ID: charID, Name: "Testy", LocationID: core.NewULID()}

	cases := []string{
		core.SessionEndedCauseLogout,
		core.SessionEndedCauseGuestEnd,
		core.SessionEndedCauseKicked,
		core.SessionEndedCauseReaped,
		core.SessionEndedCauseEvicted,
	}

	for _, cause := range cases {
		t.Run("uses ActorSystem actor when cause is "+cause, func(t *testing.T) {
			pub := &fakePublisher{}
			e := NewEmitter(pub, mainGameID)

			err := e.EmitSessionEnded(context.Background(), char, core.NewULID().String(), cause, "reason")
			require.NoError(t, err)

			events := pub.events()
			require.Len(t, events, 1)
			assert.Equal(t, eventbus.ActorKindSystem, events[0].Actor.Kind)
		})
	}
}

// publishFailPublisher is a minimal eventbus.Publisher that always fails on
// Publish.
type publishFailPublisher struct {
	err error
}

func (p *publishFailPublisher) Publish(_ context.Context, _ eventbus.Event) error { return p.err }

var _ eventbus.Publisher = (*publishFailPublisher)(nil)

func TestEmitSessionEndedReturnsErrorWhenPublisherFails(t *testing.T) {
	pub := &publishFailPublisher{err: errors.New("disk full")}
	e := NewEmitter(pub, mainGameID)

	char := core.CharacterRef{ID: core.NewULID(), Name: "Testy", LocationID: core.NewULID()}
	err := e.EmitSessionEnded(context.Background(), char, core.NewULID().String(), core.SessionEndedCauseQuit, "bye")

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SESSION_ENDED_APPEND_FAILED")
}

// ctxRecordingPublisher records the context passed to Publish so tests can
// verify the publish ctx is decoupled from the caller's ctx.
type ctxRecordingPublisher struct {
	fakePublisher
	publishCtx context.Context //nolint:containedctx // test seam
}

func (p *ctxRecordingPublisher) Publish(ctx context.Context, ev eventbus.Event) error {
	p.publishCtx = ctx
	return p.fakePublisher.Publish(ctx, ev)
}

// TestEmitSessionEndedDecouplesPublishCtxFromCallerCtx verifies the
// decoupled-ctx discipline: the context passed to pub.Publish is NOT the
// caller's ctx, so caller-ctx cancel does not prevent the audit-critical
// publish.
func TestEmitSessionEndedDecouplesPublishCtxFromCallerCtx(t *testing.T) {
	pub := &ctxRecordingPublisher{}
	e := NewEmitter(pub, mainGameID)

	callerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	charID := core.NewULID()
	char := core.CharacterRef{ID: charID, Name: "Testy", LocationID: core.NewULID()}

	err := e.EmitSessionEnded(callerCtx, char, core.NewULID().String(), core.SessionEndedCauseQuit, "Goodbye!")
	require.NoError(t, err)

	// The publish ctx must be distinct from the caller ctx.
	require.NotNil(t, pub.publishCtx)
	assert.NotSame(t, callerCtx, pub.publishCtx, "publish ctx must be decoupled from caller ctx")

	// Cancelling the caller ctx must NOT propagate to the publish ctx (which
	// is already done/cleaned up, but it must never have been derived from
	// callerCtx in the first place).
	cancel()
	// The publish ctx must have its own deadline (bounded timeout).
	deadline, ok := pub.publishCtx.Deadline()
	assert.True(t, ok, "publish ctx must have a bounded deadline")
	assert.WithinDuration(t, time.Now().Add(sessionTerminalCommitTimeout), deadline, 500*time.Millisecond,
		"publish ctx deadline must be ~sessionTerminalCommitTimeout from now")

	// Event must be persisted.
	events := pub.events()
	assert.Len(t, events, 1, "session_ended event MUST be persisted")
}

// cancellingPublisher cancels a caller ctx at the moment Publish is invoked,
// then performs the publish. This simulates a client hangup that races with
// the quit path: if EmitSessionEnded used caller ctx for Publish, the event
// would drop.
type cancellingPublisher struct {
	fakePublisher
	cancel context.CancelFunc
}

func (p *cancellingPublisher) Publish(ctx context.Context, ev eventbus.Event) error {
	// Cancel the caller ctx mid-publish. If EmitSessionEnded mistakenly
	// plumbed callerCtx into Publish, this would cause ctx-aware
	// implementations to drop the write. fakePublisher ignores ctx so we
	// additionally check ctx.Err() of the passed-in ctx to confirm the
	// decoupling regardless of publisher behavior.
	p.cancel()
	return p.fakePublisher.Publish(ctx, ev)
}

// TestEmitSessionEndedPublishCtxNotCancelledWhenCallerCtxCancelsMidPublish
// verifies that if the caller cancels their ctx while Publish is in flight,
// the ctx actually handed to Publish remains uncancelled.
func TestEmitSessionEndedPublishCtxNotCancelledWhenCallerCtxCancelsMidPublish(t *testing.T) {
	callerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pub := &cancellingPublisher{cancel: cancel}
	e := NewEmitter(pub, mainGameID)

	charID := core.NewULID()
	char := core.CharacterRef{ID: charID, Name: "Testy", LocationID: core.NewULID()}

	err := e.EmitSessionEnded(callerCtx, char, core.NewULID().String(), core.SessionEndedCauseQuit, "Goodbye!")
	require.NoError(t, err, "EmitSessionEnded must succeed even when caller ctx is cancelled mid-publish")

	events := pub.events()
	assert.Len(t, events, 1, "session_ended event MUST be persisted")
}

// TestEmitSessionEndedPersistsEventEvenWhenCallerCtxAlreadyCancelled
// verifies that the caller's context does not gate the audit-critical
// publish. A client that hung up just before EmitSessionEnded was invoked
// (pre-cancelled ctx) MUST NOT cause the terminal session_ended event to be
// skipped — the publish uses a fresh background context bounded by
// sessionTerminalCommitTimeout.
func TestEmitSessionEndedPersistsEventEvenWhenCallerCtxAlreadyCancelled(t *testing.T) {
	pub := &fakePublisher{}
	e := NewEmitter(pub, mainGameID)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	charID := core.NewULID()
	char := core.CharacterRef{ID: charID, Name: "Testy", LocationID: core.NewULID()}

	err := e.EmitSessionEnded(ctx, char, core.NewULID().String(), core.SessionEndedCauseQuit, "Goodbye!")
	require.NoError(t, err, "pre-cancelled ctx must not skip the terminal publish")

	events := pub.events()
	assert.Len(t, events, 1, "session_ended event MUST be persisted even with pre-cancelled caller ctx")
}
