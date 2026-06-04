// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package readstream_test

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/admin/readstream"
)

// recordingSetDeadline returns a setDeadline stub plus a pointer to the slice
// that captures every time argument passed to it. Used by tests that assert
// on the deadline-set call pattern (set, then clear).
func recordingSetDeadline() (func(time.Time) error, *[]time.Time) {
	calls := &[]time.Time{}
	return func(t time.Time) error {
		*calls = append(*calls, t)
		return nil
	}, calls
}

// noopSetDeadline is the canonical "I don't care about deadlines" stub used by
// tests that aren't asserting on the deadline-set call pattern.
func noopSetDeadline(_ time.Time) error { return nil }

// TestSendWithDeadline_FastPasses verifies that a sender completing
// successfully returns nil AND that the deadline is set + cleared
// across the call.
func TestSendWithDeadline_FastPasses(t *testing.T) {
	setDeadline, calls := recordingSetDeadline()
	err := readstream.SendWithDeadline(
		context.Background(),
		func(_ string) error { return nil },
		"hello",
		500*time.Millisecond,
		setDeadline,
	)
	require.NoError(t, err)
	require.Len(t, *calls, 2, "setDeadline MUST be called twice: once to set, once to clear")
	assert.False(t, (*calls)[0].IsZero(), "first call sets a real (non-zero) deadline")
	assert.True(t, (*calls)[1].IsZero(), "second call clears the deadline (zero time)")
}

// TestSendWithDeadline_PassesDeadlineToSetter verifies the first call to
// setDeadline supplies now+deadline (within a tolerance window).
func TestSendWithDeadline_PassesDeadlineToSetter(t *testing.T) {
	setDeadline, calls := recordingSetDeadline()
	const dur = 100 * time.Millisecond
	before := time.Now()
	_ = readstream.SendWithDeadline(
		context.Background(),
		func(_ string) error { return nil },
		"frame",
		dur,
		setDeadline,
	)
	after := time.Now()
	require.Len(t, *calls, 2)
	assert.False(t, (*calls)[0].Before(before.Add(dur)),
		"first call must set deadline ≥ before+dur; got %v (before+dur=%v)", (*calls)[0], before.Add(dur))
	assert.False(t, (*calls)[0].After(after.Add(dur)),
		"first call must set deadline ≤ after+dur; got %v (after+dur=%v)", (*calls)[0], after.Add(dur))
}

// TestINV_CRYPTO_64_SendWithDeadlineTrips verifies that when send returns
// os.ErrDeadlineExceeded (the kernel-level signal that the write deadline
// tripped at the conn), SendWithDeadline returns ErrWriteDeadlineExceeded.
func TestINV_CRYPTO_64_SendWithDeadlineTrips(t *testing.T) {
	err := readstream.SendWithDeadline(
		context.Background(),
		func(_ string) error { return os.ErrDeadlineExceeded },
		"slow",
		50*time.Millisecond,
		noopSetDeadline,
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, readstream.ErrWriteDeadlineExceeded),
		"expected ErrWriteDeadlineExceeded, got %v", err)
}

// TestSendWithDeadline_PropagatesSenderError verifies a non-deadline sender
// error is returned verbatim.
func TestSendWithDeadline_PropagatesSenderError(t *testing.T) {
	sentinelErr := errors.New("send failed: stream closed")
	err := readstream.SendWithDeadline(
		context.Background(),
		func(_ string) error { return sentinelErr },
		"payload",
		500*time.Millisecond,
		noopSetDeadline,
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinelErr)
	assert.False(t, errors.Is(err, readstream.ErrWriteDeadlineExceeded))
}

// TestSendWithDeadline_NoOrphanGoroutine is the regression test for
// holomush-v0fy. The previous design spawned a goroutine that could continue
// writing to the HTTP response after SendWithDeadline returned, racing the
// conn.serve teardown. This test verifies that send completes BEFORE
// SendWithDeadline returns — synchronous semantics, no orphan, no race.
func TestSendWithDeadline_NoOrphanGoroutine(t *testing.T) {
	var sendCompleted atomic.Bool
	start := time.Now()
	err := readstream.SendWithDeadline(
		context.Background(),
		func(_ string) error {
			// Simulate a slow Write that the kernel cuts off after the
			// deadline. In production, conn.SetWriteDeadline does the
			// cutting; here we approximate with a Sleep + ErrDeadlineExceeded.
			time.Sleep(40 * time.Millisecond)
			sendCompleted.Store(true)
			return os.ErrDeadlineExceeded
		},
		"frame",
		10*time.Millisecond,
		noopSetDeadline,
	)
	elapsed := time.Since(start)
	require.Error(t, err)
	assert.True(t, errors.Is(err, readstream.ErrWriteDeadlineExceeded))
	assert.True(t, sendCompleted.Load(),
		"send MUST complete before SendWithDeadline returns; orphan-goroutine pattern FORBIDDEN per holomush-v0fy")
	assert.GreaterOrEqual(t, elapsed, 40*time.Millisecond,
		"SendWithDeadline must block until send returns (no premature return); elapsed=%v", elapsed)
}

// TestSendWithDeadline_ClientDisconnect verifies that when the parent
// context is cancelled and the sender returns ctx.Err(), SendWithDeadline
// returns context.Canceled — NOT ErrWriteDeadlineExceeded.
func TestSendWithDeadline_ClientDisconnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := readstream.SendWithDeadline(
		ctx,
		func(_ string) error { return ctx.Err() },
		"payload",
		500*time.Millisecond,
		noopSetDeadline,
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled,
		"client disconnect must return context.Canceled, not ErrWriteDeadlineExceeded")
	assert.False(t, errors.Is(err, readstream.ErrWriteDeadlineExceeded))
}

// TestSendWithDeadline_ClientDisconnectWithDeadlineErr verifies that if send
// returns os.ErrDeadlineExceeded BUT ctx is also cancelled, the context
// error wins (so callers can distinguish operator timeout from a client
// drop that incidentally also tripped the conn deadline).
func TestSendWithDeadline_ClientDisconnectWithDeadlineErr(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := readstream.SendWithDeadline(
		ctx,
		func(_ string) error { return os.ErrDeadlineExceeded },
		"payload",
		500*time.Millisecond,
		noopSetDeadline,
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled,
		"cancelled ctx MUST win over kernel deadline error; attribution must be client-side")
	assert.False(t, errors.Is(err, readstream.ErrWriteDeadlineExceeded))
}

// TestSendWithDeadline_SetDeadlineErrorSurfaces verifies that a failure to
// set the deadline is wrapped + returned WITHOUT invoking send.
func TestSendWithDeadline_SetDeadlineErrorSurfaces(t *testing.T) {
	sentinel := errors.New("setsockopt: bad descriptor")
	var sendCalled atomic.Bool
	err := readstream.SendWithDeadline(
		context.Background(),
		func(_ string) error {
			sendCalled.Store(true)
			return nil
		},
		"payload",
		100*time.Millisecond,
		func(_ time.Time) error { return sentinel },
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel, "setDeadline error must surface to the caller")
	assert.False(t, sendCalled.Load(), "send MUST NOT be called when setDeadline fails")
}

// TestSendWithDeadline_DeadlineClearedAfterSendError verifies the
// defer-clear fires even when send returns a non-deadline error, so
// subsequent writes on the same conn start with no stale deadline.
func TestSendWithDeadline_DeadlineClearedAfterSendError(t *testing.T) {
	setDeadline, calls := recordingSetDeadline()
	_ = readstream.SendWithDeadline(
		context.Background(),
		func(_ string) error { return errors.New("send err") },
		"payload",
		100*time.Millisecond,
		setDeadline,
	)
	require.Len(t, *calls, 2, "setDeadline must be cleared even on send error")
	assert.True(t, (*calls)[1].IsZero(), "deadline cleared (zero time)")
}
