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

// TestSendWithDeadline_DrainsAfterDeadline verifies that after
// SendWithDeadline returns (due to deadline expiry), the orphaned send
// goroutine is drained before the function returns — preventing data races
// in subsequent send calls.
func TestSendWithDeadline_DrainsAfterDeadline(t *testing.T) {
	ctx := context.Background()
	frame := "slow"

	var completed atomic.Bool
	err := readstream.SendWithDeadline(ctx, func(_ string) error {
		time.Sleep(150 * time.Millisecond)
		completed.Store(true)
		return nil
	}, frame, 30*time.Millisecond)

	// Deadline must have tripped.
	require.Error(t, err)
	assert.True(t, errors.Is(err, readstream.ErrWriteDeadlineExceeded))

	// After SendWithDeadline returns, the goroutine must have completed
	// (drain was awaited). We allow a generous upper bound to absorb CI
	// scheduling jitter, but the drain should happen well within 500ms.
	deadline := time.Now().Add(500 * time.Millisecond)
	for !completed.Load() {
		if time.Now().After(deadline) {
			t.Fatal("goroutine was not drained within 500ms after SendWithDeadline returned")
		}
		time.Sleep(5 * time.Millisecond)
	}
	assert.True(t, completed.Load(), "sender goroutine must have completed after drain")
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
