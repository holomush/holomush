// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package readstream_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/admin/readstream"
)

// TestSendWithDeadline_FastPasses verifies that a sender completing before the
// deadline returns nil.
func TestSendWithDeadline_FastPasses(t *testing.T) {
	ctx := context.Background()
	frame := "hello"
	err := readstream.SendWithDeadline(ctx, func(_ string) error {
		return nil // instant
	}, frame, 500*time.Millisecond)
	require.NoError(t, err)
}

// TestINV_F14_SendWithDeadlineTrips verifies that a slow sender triggers
// ErrWriteDeadlineExceeded when the deadline elapses before the send
// completes.
func TestINV_F14_SendWithDeadlineTrips(t *testing.T) {
	ctx := context.Background()
	frame := "slow"
	err := readstream.SendWithDeadline(ctx, func(_ string) error {
		time.Sleep(200 * time.Millisecond) // longer than deadline
		return nil
	}, frame, 50*time.Millisecond)
	require.Error(t, err)
	assert.True(t, errors.Is(err, readstream.ErrWriteDeadlineExceeded),
		"expected ErrWriteDeadlineExceeded, got %v", err)
}

// TestSendWithDeadline_PropagatesSenderError verifies that the sender's error
// is returned verbatim when it completes before the deadline.
func TestSendWithDeadline_PropagatesSenderError(t *testing.T) {
	ctx := context.Background()
	sentinelErr := errors.New("send failed: stream closed")
	frame := "payload"
	err := readstream.SendWithDeadline(ctx, func(_ string) error {
		return sentinelErr
	}, frame, 500*time.Millisecond)
	require.Error(t, err)
	assert.Equal(t, sentinelErr, err)
	assert.False(t, errors.Is(err, readstream.ErrWriteDeadlineExceeded))
}

// TestSendWithDeadline_DeadlineReturnsImmediately verifies that
// SendWithDeadline returns ErrWriteDeadlineExceeded promptly — without
// blocking on the orphaned send goroutine completing. The orphaned goroutine
// is allowed to complete asynchronously after the function returns.
func TestSendWithDeadline_DeadlineReturnsImmediately(t *testing.T) {
	ctx := context.Background()
	frame := "slow"
	const deadlineDur = 30 * time.Millisecond
	const sendDur = 300 * time.Millisecond // 10× the deadline

	start := time.Now()
	err := readstream.SendWithDeadline(ctx, func(_ string) error {
		time.Sleep(sendDur)
		return nil
	}, frame, deadlineDur)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.True(t, errors.Is(err, readstream.ErrWriteDeadlineExceeded),
		"expected ErrWriteDeadlineExceeded, got %v", err)

	// Must return well before the slow send completes. Allow 5× the deadline
	// to absorb CI scheduling jitter; still far less than sendDur.
	assert.Less(t, elapsed, 5*deadlineDur,
		"SendWithDeadline must return promptly after deadline fires, not block on orphan: elapsed=%v", elapsed)
}

// TestSendWithDeadline_OrphanedGoroutineEventuallyCompletes verifies the
// buffered-channel guarantee: the orphaned goroutine can always write its
// result to doneCh (cap 1) and exits without blocking, even after the caller
// has long since returned.
func TestSendWithDeadline_OrphanedGoroutineEventuallyCompletes(t *testing.T) {
	ctx := context.Background()
	frame := "slow"

	var completed atomic.Bool
	err := readstream.SendWithDeadline(ctx, func(_ string) error {
		time.Sleep(150 * time.Millisecond)
		completed.Store(true)
		return nil
	}, frame, 30*time.Millisecond)

	require.Error(t, err)
	assert.True(t, errors.Is(err, readstream.ErrWriteDeadlineExceeded))

	// The goroutine completes asynchronously after SendWithDeadline returns.
	// Poll for up to 500ms to let it finish without blocking the test.
	deadline := time.Now().Add(500 * time.Millisecond)
	for !completed.Load() {
		if time.Now().After(deadline) {
			t.Fatal("orphaned goroutine did not complete within 500ms")
		}
		time.Sleep(5 * time.Millisecond)
	}
	assert.True(t, completed.Load(), "orphaned goroutine must eventually complete (buffered channel)")
}

// TestSendWithDeadline_ClientDisconnect verifies that when the parent context
// is cancelled (client disconnect) while the sender is running, the function
// returns context.Canceled — NOT ErrWriteDeadlineExceeded.
func TestSendWithDeadline_ClientDisconnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	started := make(chan struct{})
	frame := "payload"

	go func() {
		<-started
		// Cancel the parent context while the sender is mid-flight.
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := readstream.SendWithDeadline(ctx, func(_ string) error {
		close(started)
		time.Sleep(300 * time.Millisecond) // slow send
		return nil
	}, frame, 500*time.Millisecond) // deadline is generous — cancel fires first

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled,
		"client disconnect must return context.Canceled, not ErrWriteDeadlineExceeded")
	assert.False(t, errors.Is(err, readstream.ErrWriteDeadlineExceeded),
		"must NOT return ErrWriteDeadlineExceeded on client disconnect")
}
