// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package consumer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// shortBackoffs is the per-call schedule the retry tests pass so they do
// not pay the production schedule's wall time. The SHAPE (number of
// retries) matches defaultBackoffs so attempt counts match the production
// path; only the per-step sleep shrinks.
//
// Passed as an option rather than swapped into the package-level
// defaultBackoffs: a global swap needed a t.Cleanup restore, which made
// "MUST NOT call t.Parallel()" a constraint on every test that used it.
var shortBackoffs = []time.Duration{1 * time.Millisecond, 2 * time.Millisecond}

func TestCreateWithRetrySucceedsOnFirstAttempt(t *testing.T) {
	var attempts int
	cons, err := CreateWithRetry(context.Background(), func(_ context.Context) (jetstream.Consumer, error) {
		attempts++
		return nil, nil //nolint:nilnil // test stub
	}, WithBackoffs(shortBackoffs...))
	require.NoError(t, err)
	require.Nil(t, cons, "test stub returns nil consumer; production returns a real one")
	require.Equal(t, 1, attempts, "no retries when first attempt succeeds")
}

func TestCreateWithRetrySucceedsAfterTransientFailures(t *testing.T) {
	transient := errors.New("nats: no responders")
	var attempts int
	cons, err := CreateWithRetry(context.Background(), func(_ context.Context) (jetstream.Consumer, error) {
		attempts++
		if attempts <= 2 {
			return nil, transient
		}
		return nil, nil //nolint:nilnil // test stub: success after 2 transient errors
	}, WithBackoffs(shortBackoffs...))
	require.NoError(t, err)
	require.Nil(t, cons)
	require.Equal(t, 3, attempts, "should retry through the two-step backoff schedule")
}

func TestCreateWithRetryGivesUpAfterBudgetExhausted(t *testing.T) {
	permanent := errors.New("nats: stream not found")
	var attempts int
	_, err := CreateWithRetry(context.Background(), func(_ context.Context) (jetstream.Consumer, error) {
		attempts++
		return nil, permanent
	}, WithBackoffs(shortBackoffs...))
	require.ErrorIs(t, err, permanent, "final error MUST be the underlying NATS error so the caller can surface it")
	require.Equal(t, 1+len(shortBackoffs), attempts, "should attempt initial call + one per backoff entry")
	// CONTENTS, not just length: a length check passes even when both
	// durations have been swapped for different values, which is precisely
	// the mutation this assertion exists to catch.
	require.Equal(t,
		[]time.Duration{100 * time.Millisecond, 250 * time.Millisecond},
		DefaultBackoffs(),
		"the shared default MUST be untouched by a per-call override")
}

// TestDefaultBackoffsHandsBackACopySoACallerCannotMutateTheSharedSchedule is
// the reason the schedule is unexported with an accessor rather than an
// exported slice carrying a doc line that asks nicely.
func TestDefaultBackoffsHandsBackACopySoACallerCannotMutateTheSharedSchedule(t *testing.T) {
	got := DefaultBackoffs()
	require.NotEmpty(t, got)
	got[0] = 99 * time.Hour

	require.Equal(t, 100*time.Millisecond, DefaultBackoffs()[0],
		"mutating the returned slice MUST NOT reach the shared default")
}

func TestCreateWithRetryRespectsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel
	transient := errors.New("transient")
	var attempts int
	_, err := CreateWithRetry(ctx, func(_ context.Context) (jetstream.Consumer, error) {
		attempts++
		return nil, transient
	}, WithBackoffs(shortBackoffs...))
	require.ErrorIs(t, err, transient)
	require.Equal(t, 1, attempts,
		"ctx.Err() check between attempts MUST short-circuit further retries; only the initial attempt should run")
}

func TestCreateWithRetryHonorsCtxDeadlineDuringBackoff(t *testing.T) {
	// Production backoffs (100ms, 250ms) outlast a 1ms ctx deadline, so
	// the retry loop should exit via the ctx.Done() select arm rather
	// than waiting out the full backoff.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	transient := errors.New("transient")
	start := time.Now()
	_, err := CreateWithRetry(ctx, func(_ context.Context) (jetstream.Consumer, error) {
		return nil, transient
	})
	elapsed := time.Since(start)
	require.ErrorIs(t, err, transient)
	require.Less(t, elapsed, 50*time.Millisecond,
		"ctx deadline MUST short-circuit the backoff sleep; production 100ms backoff would otherwise dominate")
}

// TestCreateWithRetryCodesNothing pins the D-46 relocation contract: the
// shared helper MUST return the create func's error UNWRAPPED, so each
// caller's own oops Code (AUDIT_CONSUMER_CREATE_FAILED /
// AUDIT_PLUGIN_CONSUMER_CREATE_FAILED) remains the outermost code. A wrap
// added here would silently change both audit callers' error surfaces.
func TestCreateWithRetryReturnsTheUnderlyingErrorUnwrapped(t *testing.T) {
	permanent := errors.New("nats: no stream matches subject")
	_, err := CreateWithRetry(context.Background(), func(_ context.Context) (jetstream.Consumer, error) {
		return nil, permanent
	}, WithBackoffs(shortBackoffs...))
	require.Equal(t, permanent, err, //nolint:testifylint // identity, not just errors.Is: the helper MUST NOT wrap
		"the helper MUST return the create error itself so callers own the error code")
}
