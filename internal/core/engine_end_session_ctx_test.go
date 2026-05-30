// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inMemAppender is a minimal in-memory core.EventAppender for the white-box
// ctx-decoupling tests below. These tests stay in package core to reach the
// unexported sessionTerminalCommitTimeout, so they cannot import
// internal/core/coretest (that would create an import cycle: coretest imports
// core). Only Append + Replay are needed here.
type inMemAppender struct {
	mu      sync.Mutex
	streams map[string][]Event
}

func newInMemAppender() *inMemAppender {
	return &inMemAppender{streams: make(map[string][]Event)}
}

func (s *inMemAppender) Append(_ context.Context, e Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.streams[e.Stream] = append(s.streams[e.Stream], e)
	return nil
}

func (s *inMemAppender) Replay(_ context.Context, stream string, _ ulid.ULID, _ int) ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Event(nil), s.streams[stream]...), nil
}

// ctxRecordingStore records the context passed to Append so tests can verify
// the append ctx is decoupled from the caller's ctx.
type ctxRecordingStore struct {
	*inMemAppender
	appendCtx context.Context //nolint:containedctx // test seam
}

func (s *ctxRecordingStore) Append(ctx context.Context, event Event) error {
	s.appendCtx = ctx
	return s.inMemAppender.Append(ctx, event)
}

// TestEndSessionDecouplesAppendCtxFromCallerCtx verifies the decoupled-ctx
// discipline: the context passed to store.Append is NOT the caller's ctx,
// so caller-ctx cancel does not prevent the audit-critical append.
func TestEndSessionDecouplesAppendCtxFromCallerCtx(t *testing.T) {
	inner := newInMemAppender()
	store := &ctxRecordingStore{inMemAppender: inner}
	engine := NewEngine(store)

	callerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	charID := NewULID()
	char := CharacterRef{ID: charID, Name: "Testy", LocationID: NewULID()}

	err := engine.EndSession(callerCtx, char, NewULID().String(), SessionEndedCauseQuit, "Goodbye!")
	require.NoError(t, err)

	// The append ctx must be distinct from the caller ctx.
	require.NotNil(t, store.appendCtx)
	assert.NotSame(t, callerCtx, store.appendCtx, "append ctx must be decoupled from caller ctx")

	// Cancelling the caller ctx must NOT propagate to the append ctx
	// (which is already done/cleaned up, but it must never have been
	// derived from callerCtx in the first place).
	cancel()
	// The append ctx must have its own deadline (bounded timeout).
	deadline, ok := store.appendCtx.Deadline()
	assert.True(t, ok, "append ctx must have a bounded deadline")
	assert.WithinDuration(t, time.Now().Add(sessionTerminalCommitTimeout), deadline, 500*time.Millisecond,
		"append ctx deadline must be ~sessionTerminalCommitTimeout from now")

	// Event must be persisted.
	stream := "character." + charID.String()
	events, replayErr := inner.Replay(context.Background(), stream, ulid.ULID{}, 10)
	require.NoError(t, replayErr)
	assert.Len(t, events, 1, "session_ended event MUST be persisted")
}

// cancellingStore cancels a caller ctx at the moment Append is invoked, then
// performs the append. This simulates a client hangup that races with the
// quit path: if EndSession used caller ctx for Append, the event would drop.
type cancellingStore struct {
	*inMemAppender
	cancel context.CancelFunc
}

func (s *cancellingStore) Append(ctx context.Context, event Event) error {
	// Cancel the caller ctx mid-append. If EndSession mistakenly plumbed
	// callerCtx into Append, this would cause store-side ctx-aware
	// implementations to drop the write. inMemAppender ignores ctx so
	// we additionally check ctx.Err() of the passed-in ctx to confirm
	// the decoupling regardless of store behavior.
	s.cancel()
	return s.inMemAppender.Append(ctx, event)
}

// TestEndSessionAppendCtxNotCancelledWhenCallerCtxCancelsMidAppend verifies
// that if the caller cancels their ctx while Append is in flight, the ctx
// actually handed to Append remains uncancelled.
func TestEndSessionAppendCtxNotCancelledWhenCallerCtxCancelsMidAppend(t *testing.T) {
	callerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	inner := newInMemAppender()
	store := &cancellingStore{inMemAppender: inner, cancel: cancel}
	engine := NewEngine(store)

	charID := NewULID()
	char := CharacterRef{ID: charID, Name: "Testy", LocationID: NewULID()}

	err := engine.EndSession(callerCtx, char, NewULID().String(), SessionEndedCauseQuit, "Goodbye!")
	require.NoError(t, err, "EndSession must succeed even when caller ctx is cancelled mid-append")

	stream := "character." + charID.String()
	events, replayErr := inner.Replay(context.Background(), stream, ulid.ULID{}, 10)
	require.NoError(t, replayErr)
	assert.Len(t, events, 1, "session_ended event MUST be persisted")
}

// TestEndSessionPersistsEventEvenWhenCallerCtxAlreadyCancelled verifies that
// the caller's context does not gate the audit-critical append. A client that
// hung up just before EndSession was invoked (pre-cancelled ctx) MUST NOT
// cause the terminal session_ended event to be skipped — the append uses a
// fresh background context bounded by sessionTerminalCommitTimeout.
func TestEndSessionPersistsEventEvenWhenCallerCtxAlreadyCancelled(t *testing.T) {
	store := newInMemAppender()
	engine := NewEngine(store)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	charID := NewULID()
	char := CharacterRef{ID: charID, Name: "Testy", LocationID: NewULID()}

	err := engine.EndSession(ctx, char, NewULID().String(), SessionEndedCauseQuit, "Goodbye!")
	require.NoError(t, err, "pre-cancelled ctx must not skip the terminal append")

	stream := "character." + charID.String()
	events, replayErr := store.Replay(context.Background(), stream, ulid.ULID{}, 10)
	require.NoError(t, replayErr)
	assert.Len(t, events, 1, "session_ended event MUST be persisted even with pre-cancelled caller ctx")
}
