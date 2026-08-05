// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package blocklist_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/charname/blocklist"
	"github.com/holomush/holomush/internal/lifecycle"
)

// fakeVersion is a blocklist.VersionQuerier double over the (updated_at,
// hash(value)) pair. The two signals are settable independently, because the
// whole point of the pair is that they move independently: a direct-SQL
// UPDATE moves the hash and leaves updated_at alone.
type fakeVersion struct {
	mu        sync.Mutex
	updatedAt int64
	hash      string
	absent    bool
	err       error
	calls     int
}

func (f *fakeVersion) SystemInfoVersion(_ context.Context, _ string) (int64, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return 0, "", f.err
	}
	if f.absent {
		return 0, "", nil
	}
	return f.updatedAt, f.hash, nil
}

func (f *fakeVersion) set(updatedAt int64, hash string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updatedAt, f.hash, f.absent = updatedAt, hash, false
}

func (f *fakeVersion) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// countingReloader counts Reload invocations so "an unchanged indicator does
// not reload" is asserted structurally rather than inferred.
type countingReloader struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (r *countingReloader) Reload(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return r.err
}

func (r *countingReloader) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func (r *countingReloader) setErr(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}

func newTracker() *lifecycle.HealthTracker {
	return lifecycle.NewHealthTracker(lifecycle.TrackerConfig{})
}

func TestNewPollerRejectsEveryNilCollaborator(t *testing.T) {
	base := blocklist.PollerConfig{
		Querier:  &fakeVersion{},
		Reloader: &countingReloader{},
		Tracker:  newTracker(),
		Key:      testKey,
	}

	// Paired positive control: the fully-populated config succeeds, so the
	// rejections below cannot pass because NewPoller rejects everything.
	ok, err := blocklist.NewPoller(base)
	require.NoError(t, err)
	require.NotNil(t, ok)

	for _, tt := range []struct {
		name  string
		mutIn func(*blocklist.PollerConfig)
	}{
		{"a nil querier", func(c *blocklist.PollerConfig) { c.Querier = nil }},
		{"a nil reloader", func(c *blocklist.PollerConfig) { c.Reloader = nil }},
		{"a nil tracker", func(c *blocklist.PollerConfig) { c.Tracker = nil }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base
			tt.mutIn(&cfg)

			_, err := blocklist.NewPoller(cfg)

			require.Error(t, err)
		})
	}
}

func TestNewPollerDefaultsANonPositiveIntervalToTenSeconds(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Second} {
		p, err := blocklist.NewPoller(blocklist.PollerConfig{
			Querier:  &fakeVersion{},
			Reloader: &countingReloader{},
			Tracker:  newTracker(),
			Key:      testKey,
			Interval: interval,
		})

		require.NoError(t, err)
		assert.Equal(t, 10*time.Second, p.Interval())
	}
}

func TestPollerRunPerformsAnImmediateFirstPollBeforeEnteringTheTickerLoop(t *testing.T) {
	q := &fakeVersion{updatedAt: 1, hash: "a"}
	r := &countingReloader{}
	// An interval far longer than the test's patience, so any observed poll
	// MUST be the immediate one rather than a tick.
	p := mustPoller(t, q, r, time.Hour)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := runPoller(ctx, p)

	require.Eventually(t, func() bool { return r.count() == 1 }, 2*time.Second, 5*time.Millisecond)
	cancel()
	<-done
}

func TestPollerMarksItselfInitializedOnlyAfterTheFirstReloadSucceedsSoAFailureIsRetried(t *testing.T) {
	q := &fakeVersion{updatedAt: 1, hash: "a"}
	r := &countingReloader{}
	r.setErr(errors.New("compile failed"))
	p := mustPoller(t, q, r, 5*time.Millisecond)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := runPoller(ctx, p)

	// The indicator NEVER changes. A poller that marked itself initialized on a
	// failed reload would poll forever and never reload again.
	require.Eventually(t, func() bool { return r.count() >= 3 }, 2*time.Second, 5*time.Millisecond)

	// Now let the reload succeed, and observe the poller settle: with the
	// baseline established and the indicator unchanged, reloads stop.
	r.setErr(nil)
	require.Eventually(t, func() bool {
		before := r.count()
		time.Sleep(50 * time.Millisecond)
		return r.count() == before
	}, 3*time.Second, 10*time.Millisecond)

	cancel()
	<-done
}

func TestPollerDoesNotReloadWhenTheIndicatorIsUnchanged(t *testing.T) {
	q := &fakeVersion{updatedAt: 1, hash: "a"}
	r := &countingReloader{}
	p := mustPoller(t, q, r, 5*time.Millisecond)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := runPoller(ctx, p)

	// Wait until the poller has polled many times over the same indicator.
	require.Eventually(t, func() bool { return q.count() >= 10 }, 2*time.Second, 5*time.Millisecond)
	cancel()
	<-done

	assert.Equal(t, 1, r.count(),
		"only the baseline reload; an unchanged indicator triggers none")
}

func TestPollerReloadsWhenTheValueHashMovesEvenThoughUpdatedAtDoesNot(t *testing.T) {
	// The RESEARCH § Concerns 3 assertion. updated_at is maintained only by
	// SetSystemInfo's ON CONFLICT clause, migrations forbid triggers, and
	// v0.13's only edit path for this key is direct SQL — which leaves
	// updated_at untouched. A bare updated_at indicator passes every OTHER
	// test in this file and never observes the only edit that happens.
	q := &fakeVersion{updatedAt: 1, hash: "a"}
	r := &countingReloader{}
	p := mustPoller(t, q, r, 5*time.Millisecond)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := runPoller(ctx, p)

	require.Eventually(t, func() bool { return r.count() == 1 }, 2*time.Second, 5*time.Millisecond)

	q.set(1, "b") // SAME updated_at, different content hash

	require.Eventually(t, func() bool { return r.count() >= 2 }, 2*time.Second, 5*time.Millisecond)
	cancel()
	<-done
}

func TestPollerReloadsWhenUpdatedAtMovesEvenThoughTheHashDoesNot(t *testing.T) {
	// The other half of the pair, so "two signals" is not one signal wearing a
	// second name.
	q := &fakeVersion{updatedAt: 1, hash: "a"}
	r := &countingReloader{}
	p := mustPoller(t, q, r, 5*time.Millisecond)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := runPoller(ctx, p)

	require.Eventually(t, func() bool { return r.count() == 1 }, 2*time.Second, 5*time.Millisecond)

	q.set(2, "a")

	require.Eventually(t, func() bool { return r.count() >= 2 }, 2*time.Second, 5*time.Millisecond)
	cancel()
	<-done
}

func TestPollerTreatsNoSuchRowAsTheZeroIndicatorRatherThanAnError(t *testing.T) {
	q := &fakeVersion{absent: true}
	r := &countingReloader{}
	p := mustPoller(t, q, r, 5*time.Millisecond)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := runPoller(ctx, p)

	// A missing row still establishes a baseline and reloads (to the empty
	// list). It is not a poll failure.
	require.Eventually(t, func() bool { return r.count() == 1 }, 2*time.Second, 5*time.Millisecond)
	cancel()
	<-done
}

func TestPollerRunReturnsWhenItsContextIsCancelled(t *testing.T) {
	p := mustPoller(t, &fakeVersion{}, &countingReloader{}, time.Hour)

	ctx, cancel := context.WithCancel(t.Context())
	done := runPoller(ctx, p)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
}

func mustPoller(t *testing.T, q blocklist.VersionQuerier, r blocklist.Reloadable, interval time.Duration) *blocklist.Poller {
	t.Helper()
	p, err := blocklist.NewPoller(blocklist.PollerConfig{
		Querier:  q,
		Reloader: r,
		Tracker:  newTracker(),
		Key:      testKey,
		Interval: interval,
	})
	require.NoError(t, err)
	return p
}

func runPoller(ctx context.Context, p *blocklist.Poller) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.Run(ctx)
	}()
	return done
}
