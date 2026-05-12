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
//     is returned.
//
// Drain guarantee (F.23 hardening): after a timeout or cancellation, this
// function waits for the orphaned send goroutine to complete before returning.
// This prevents data races between the orphaned goroutine and any subsequent
// send operations sharing the same stream.
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
		var returnErr error
		if ctx.Err() != nil {
			// Parent context cancelled — client disconnected.
			returnErr = ctx.Err()
		} else {
			// Only the timer fired; parent is still live.
			returnErr = ErrWriteDeadlineExceeded
		}

		// Drain the orphaned goroutine before returning to prevent data races.
		<-doneCh

		return returnErr
	}
}
