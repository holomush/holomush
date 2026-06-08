// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package integrationtest

import (
	"context"
	"sync"

	"google.golang.org/grpc/metadata"

	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// defaultEventBufferSize is the default capacity of the per-session event
// channel. Sized generously so the production dispatchDelivery broadcaster
// is unlikely to encounter a full buffer (which would force Send to drop
// events with a warning); tests with high event volume can size up.
const defaultEventBufferSize = 256

// subscribeStream implements grpc.ServerStreamingServer[corev1.SubscribeResponse]
// for in-process Subscribe RPC use in the integrationtest harness.
//
// The production CoreServer.Subscribe writes both Event frames and Control
// frames (REPLAY_COMPLETE plus per-stream replay-state signals) to the
// stream. The harness:
//
//   - Push Event frames onto the events channel for downstream WaitForEvent reads.
//   - Push CONTROL_SIGNAL_SCENE_ACTIVITY frames onto the sceneActivityBadges
//     channel for downstream WaitForSceneActivityBadge reads.
//   - Signal replayDone the first time CONTROL_SIGNAL_REPLAY_COMPLETE arrives so
//     Attach can block until the durable consumer is fully wired (avoids races
//     between "Attach returned" and "events published just after" disappearing
//     into a not-yet-created durable).
//   - Drop other Control frames silently — none of the harness's invariant
//     assertions depend on them.
//
// Send is non-blocking on both channels: if a buffer is full, the frame is
// dropped and an overflow counter increments. Tests that need every frame MUST
// size the buffer to fit; tests that don't care about volume see no impact.
type subscribeStream struct {
	ctx                 context.Context //nolint:containedctx // gRPC ServerStreamingServer interface contract
	events              chan *corev1.EventFrame
	sceneActivityBadges chan *corev1.ControlFrame
	replayDone          chan struct{}

	mu             sync.Mutex
	closed         bool
	overflowed     int   // number of Event frames dropped due to full events channel
	replayed       bool  // guards single-close of replayDone
	attachMomentMs int64 // captured from REPLAY_COMPLETE ControlFrame (holomush-iu8j)
}

// newSubscribeStream constructs a stream with the given parent context.
// The caller is responsible for cancelling ctx to drive Subscribe-goroutine
// exit (see Session.attach in session.go).
//
// bufferSize is the per-session event channel capacity. Pass 0 to use the
// package default.
func newSubscribeStream(ctx context.Context, bufferSize int) *subscribeStream {
	if bufferSize <= 0 {
		bufferSize = defaultEventBufferSize
	}
	return &subscribeStream{
		ctx:                 ctx,
		events:              make(chan *corev1.EventFrame, bufferSize),
		sceneActivityBadges: make(chan *corev1.ControlFrame, bufferSize),
		replayDone:          make(chan struct{}),
	}
}

// --- grpc.ServerStream interface ---

func (s *subscribeStream) Context() context.Context       { return s.ctx }
func (s *subscribeStream) SendHeader(_ metadata.MD) error { return nil }
func (s *subscribeStream) SetHeader(_ metadata.MD) error  { return nil }
func (s *subscribeStream) SetTrailer(_ metadata.MD)       {}
func (s *subscribeStream) SendMsg(_ any) error            { return nil }
func (s *subscribeStream) RecvMsg(_ any) error            { return nil }

// Send is called by the production Subscribe loop for every outbound frame.
// Routes Event frames to the events channel, SCENE_ACTIVITY control frames to
// the sceneActivityBadges channel (non-blocking), and signals replayDone on
// CONTROL_SIGNAL_REPLAY_COMPLETE.
func (s *subscribeStream) Send(r *corev1.SubscribeResponse) error {
	if ev := r.GetEvent(); ev != nil {
		s.pushEvent(ev)
		return nil
	}
	if ctrl := r.GetControl(); ctrl != nil {
		switch ctrl.GetSignal() {
		case corev1.ControlSignal_CONTROL_SIGNAL_REPLAY_COMPLETE:
			// Capture attach_moment_ms BEFORE signaling so a racing
			// AttachMomentMs() reader who wakes on replayDone observes the
			// stamped value, not the zero default.
			s.mu.Lock()
			s.attachMomentMs = ctrl.GetAttachMomentMs()
			s.mu.Unlock()
			s.signalReplayComplete()
		case corev1.ControlSignal_CONTROL_SIGNAL_SCENE_ACTIVITY:
			s.pushSceneActivityBadge(ctrl)
		}
	}
	// Other control frames: drop silently.
	return nil
}

// pushEvent enqueues ev on the events channel. Non-blocking: drops on full
// buffer and increments the overflow counter. The closed check guards against
// races where DetachTransport cancels the stream while the production
// broadcaster is mid-Send — that Send shouldn't block on a buffer no one is
// listening to.
func (s *subscribeStream) pushEvent(ev *corev1.EventFrame) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	select {
	case s.events <- ev:
	case <-s.ctx.Done():
		// Stream context canceled — drop and return.
	default:
		// Buffer full — drop and count.
		s.mu.Lock()
		s.overflowed++
		s.mu.Unlock()
	}
}

// pushSceneActivityBadge enqueues ctrl on the sceneActivityBadges channel.
// Non-blocking: drops on full buffer (mirrors pushEvent behaviour).
func (s *subscribeStream) pushSceneActivityBadge(ctrl *corev1.ControlFrame) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	select {
	case s.sceneActivityBadges <- ctrl:
	case <-s.ctx.Done():
	default:
		s.mu.Lock()
		s.overflowed++
		s.mu.Unlock()
	}
}

// signalReplayComplete closes replayDone idempotently so multiple receivers
// or a re-trigger (defensive) doesn't panic on double-close.
func (s *subscribeStream) signalReplayComplete() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.replayed {
		return
	}
	s.replayed = true
	close(s.replayDone)
}

// close marks the stream as no longer accepting Sends. Idempotent.
// Does NOT close the events channel — outstanding readers may still want
// to drain; Go's GC reaps the channel when nothing references it.
func (s *subscribeStream) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
}

// overflowCount returns how many Event frames were dropped due to a full
// events buffer. Tests can assert this is zero to catch any silent loss.
func (s *subscribeStream) overflowCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.overflowed
}

// getAttachMomentMs returns the attach_moment_ms captured from the
// REPLAY_COMPLETE ControlFrame (holomush-iu8j). Returns 0 if
// REPLAY_COMPLETE has not yet arrived or if the server sent 0
// (legacy/back-compat signal). Callers MUST sync on replayDone
// before reading this; otherwise the value may still be the
// pre-capture zero.
func (s *subscribeStream) getAttachMomentMs() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attachMomentMs
}
