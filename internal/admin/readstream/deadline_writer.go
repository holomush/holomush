// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package readstream

import (
	"context"
	"errors"
	"time"
)

// ErrWriteDeadlineExceeded is returned by SendWithDeadline when the send
// function does not complete within the specified deadline and the parent
// context was not cancelled (i.e., the slow path is server-side, not a
// client disconnect).
var ErrWriteDeadlineExceeded = errors.New("readstream: write deadline exceeded")

// SendWithDeadline runs send(frame) in a goroutine and enforces a per-frame
// write deadline (INV-F14).
//
// Semantics:
//   - If send completes before the deadline, its return value is propagated.
//   - If the deadline elapses first and ctx is already cancelled (client
//     disconnect), ctx.Err() is returned (context.Canceled / DeadlineExceeded).
//   - If the deadline elapses first and ctx is NOT cancelled, ErrWriteDeadlineExceeded
//     is returned IMMEDIATELY without waiting for the orphaned goroutine.
//
// Goroutine lifecycle: when the deadline fires, the send goroutine becomes
// orphaned. It will complete when send(frame) eventually returns (or the
// parent context is cancelled). Because doneCh is buffered (cap 1), the
// orphan can always write its result without blocking, so it never leaks
// permanently. The next SendWithDeadline call allocates a fresh doneCh,
// so there is no race between the orphan and subsequent sends.
func SendWithDeadline[T any](ctx context.Context, send func(T) error, frame T, deadline time.Duration) error {
	doneCh := make(chan error, 1)

	timerCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	go func() {
		doneCh <- send(frame)
	}()

	select {
	case err := <-doneCh:
		// Send completed within deadline.
		return err

	case <-timerCtx.Done():
		// Either the per-frame deadline fired, or the parent ctx was cancelled.

		// Distinguish client disconnect from deadline expiry.
		if ctx.Err() != nil {
			// Parent context cancelled — client disconnected.
			return ctx.Err() //nolint:wrapcheck // context sentinel errors (Canceled/DeadlineExceeded) pass through verbatim; wrapping hides the semantic
		}
		// Only the timer fired; parent is still live.
		// Return immediately — do NOT drain doneCh. The orphaned goroutine will
		// write to doneCh (buffered) and exit without blocking, so it does not
		// leak. Blocking here would defeat the purpose of the deadline.
		return ErrWriteDeadlineExceeded
	}
}
