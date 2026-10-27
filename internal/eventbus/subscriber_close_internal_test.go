// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// TestSessionStreamCloseUnblocksHandleWithoutRacingTheInbox pins the
// teardown ordering between Close and the Consume callback. nats.go runs
// handle() on the subscription's waitForMsgs goroutine and does NOT join it
// before ConsumeContext.Closed fires, so Close can run while handle is
// blocked on a full inbox. Closing the inbox at that moment is a data race
// (close vs. send) that the race detector reports regardless of the
// recover() that used to swallow the resulting panic. The fix signals
// teardown through a separate done channel instead; this test fails under
// -race on the old shape and passes on the new one (issue #5057).
func TestSessionStreamCloseUnblocksHandleWithoutRacingTheInbox(t *testing.T) {
	j := newDetachedSessionStream(0)

	returned := make(chan struct{})
	go func() {
		defer close(returned)
		j.handle(nil) // unbuffered inbox, no receiver: blocks until teardown
	}()
	// No synchronisation with the sender on purpose: whether Close lands
	// before or after the send parks, the old shape is a close-vs-send race
	// (or a send on a closed channel) that -race reports via happens-before,
	// and the new shape must let handle() return either way.
	require.NoError(t, j.Close())

	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("handle() did not return after Close()")
	}

	_, err := j.Next(context.Background())
	require.ErrorIs(t, err, jetstream.ErrMsgIteratorClosed)
}

// TestSessionStreamHandleDropsMessageAfterClose asserts a late delivery
// arriving after teardown is dropped promptly (JetStream redelivers after
// AckWait) rather than blocking the Consume goroutine forever.
func TestSessionStreamHandleDropsMessageAfterClose(t *testing.T) {
	j := newDetachedSessionStream(0)
	require.NoError(t, j.Close())

	returned := make(chan struct{})
	go func() {
		defer close(returned)
		j.handle(nil)
	}()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("handle() blocked on a closed stream")
	}
}
