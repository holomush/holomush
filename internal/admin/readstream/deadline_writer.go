// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package readstream

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/samber/oops"
)

// ErrWriteDeadlineExceeded is returned by SendWithDeadline when the per-frame
// write deadline elapses before send completes and the parent context was
// not cancelled (i.e., the slow path is server-side, not a client disconnect).
var ErrWriteDeadlineExceeded = errors.New("readstream: write deadline exceeded")

// SendWithDeadline enforces a per-frame write deadline (INV-CRYPTO-64) using a
// hybrid mechanism:
//
//  1. setDeadline(now+deadline) primes the underlying conn's write deadline so
//     a blocked production Write fails at the kernel I/O layer.
//  2. send(frame) runs in a goroutine so an internal timer can also enforce
//     the deadline at the application layer (this matters for in-process tests
//     that bypass the conn — they don't see the kernel deadline).
//  3. When the timer fires before send returns, setDeadline(now) is called to
//     force any in-flight conn Write to abort immediately; we then wait for
//     the goroutine to exit before returning. The synchronous wait is the
//     architectural fix for holomush-v0fy — the prior orphan-goroutine pattern
//     let the goroutine outlive its caller and race the conn.serve teardown
//     on the HTTP response writer.
//
// Semantics:
//   - send completes successfully within the deadline → nil is returned.
//   - send returns os.ErrDeadlineExceeded (the kernel-level signal that the
//     write deadline tripped) AND ctx is live → ErrWriteDeadlineExceeded.
//   - send returns os.ErrDeadlineExceeded BUT ctx is also cancelled →
//     ctx.Err() wins (attribution: client disconnect, not server timeout).
//   - The internal timer fires before send returns AND ctx is live → wait for
//     send to exit (forced via setDeadline(now)), then return
//     ErrWriteDeadlineExceeded.
//   - The internal timer fires AND ctx is cancelled → wait for send to exit,
//     return ctx.Err().
//   - ctx fires before timer and send → wait for send to exit (forced via
//     setDeadline(now)), return ctx.Err().
//   - send returns any non-deadline error → that error passes through
//     verbatim (unless ctx is cancelled, in which case ctx.Err() wins).
//   - setDeadline itself fails on the initial call → wrapped + returned
//     WITHOUT invoking send.
//
// setDeadline is REQUIRED and MUST be safe to call from a different goroutine
// than the one that started the send (because the timer/ctx path calls it to
// abort the in-flight send). Production callers obtain it from
// socket.UnixConnFromContext(ctx).SetWriteDeadline, which is safe per
// net.Conn's documented thread-safety. Unit tests inject a recording or
// no-op stub.
//
// Goroutine lifecycle: SendWithDeadline always waits for the send goroutine
// to exit before returning. No orphan, no response-writer race.
func SendWithDeadline[T any](
	ctx context.Context,
	send func(T) error,
	frame T,
	deadline time.Duration,
	setDeadline func(time.Time) error,
) error {
	if err := setDeadline(time.Now().Add(deadline)); err != nil {
		return oops.Code("READSTREAM_SET_WRITE_DEADLINE_FAILED").Wrap(err)
	}
	// Clear the deadline before returning so subsequent writes on the same
	// conn (e.g., the terminator frame after the per-row loop) start fresh.
	defer func() { _ = setDeadline(time.Time{}) }() //nolint:errcheck // best-effort deadline clear; subsequent writes can re-set as needed

	doneCh := make(chan error, 1)
	go func() { doneCh <- send(frame) }()

	timer := time.NewTimer(deadline)
	defer timer.Stop()

	select {
	case err := <-doneCh:
		// send completed within the deadline window. Map kernel deadline
		// error → sentinel; otherwise pass through (ctx error wins).
		if ctx.Err() != nil {
			return ctx.Err() //nolint:wrapcheck // context sentinels pass through verbatim; wrapping hides the semantic
		}
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return ErrWriteDeadlineExceeded
		}
		return err

	case <-timer.C:
		// Application-level deadline fired before send returned. Force the
		// conn to abort the in-flight Write so the goroutine exits quickly,
		// then wait for it. In production, conn.SetWriteDeadline(now) makes
		// the next Write call return os.ErrDeadlineExceeded immediately; in
		// in-process tests where setDeadline is a no-op, we wait for send
		// to complete naturally. Either way: NO ORPHAN.
		_ = setDeadline(time.Now()) //nolint:errcheck // best-effort force-abort; we wait for the goroutine regardless
		<-doneCh
		if ctx.Err() != nil {
			return ctx.Err() //nolint:wrapcheck // context sentinels pass through verbatim
		}
		return ErrWriteDeadlineExceeded

	case <-ctx.Done():
		// Parent ctx fired (client disconnect / server shutdown). Force the
		// in-flight Write to abort, then wait for the goroutine to exit.
		_ = setDeadline(time.Now()) //nolint:errcheck // best-effort force-abort
		<-doneCh
		return ctx.Err() //nolint:wrapcheck // context sentinels pass through verbatim
	}
}
