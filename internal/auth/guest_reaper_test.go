// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
)

// stubLister is a test double for GuestPlayerLister.
type stubLister struct {
	mu     sync.Mutex
	guests []*auth.Player
	err    error
	calls  int
}

func (s *stubLister) ListIdleGuests(_ context.Context, _ time.Time) ([]*auth.Player, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return s.guests, s.err
}

// stubCleaner is a test double for GuestCleaner.
type stubCleaner struct {
	mu      sync.Mutex
	deleted []ulid.ULID
	errs    map[ulid.ULID]error
}

func (s *stubCleaner) DeleteGuestPlayer(_ context.Context, playerID ulid.ULID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err, ok := s.errs[playerID]; ok {
		return err
	}
	s.deleted = append(s.deleted, playerID)
	return nil
}

func (s *stubCleaner) deletedIDs() []ulid.ULID {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ulid.ULID, len(s.deleted))
	copy(out, s.deleted)
	return out
}

func newTestGuest(t *testing.T, username string) *auth.Player {
	t.Helper()
	p, err := auth.NewGuestPlayer(username)
	require.NoError(t, err)
	return p
}

func TestGuestReaper_ReapsIdleGuests(t *testing.T) {
	guest := newTestGuest(t, "GuestA")

	lister := &stubLister{guests: []*auth.Player{guest}}
	cleaner := &stubCleaner{}

	var (
		mu      sync.Mutex
		reaped  []ulid.ULID
		done    = make(chan struct{})
	)

	config := auth.GuestReaperConfig{
		Interval: 50 * time.Millisecond,
		IdleTTL:  1 * time.Millisecond,
		OnReaped: func(id ulid.ULID) {
			mu.Lock()
			reaped = append(reaped, id)
			if len(reaped) == 1 {
				close(done)
			}
			mu.Unlock()
		},
	}

	reaper := auth.NewGuestReaper(config, lister, cleaner)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go reaper.Run(ctx)

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("timed out waiting for OnReaped to be called")
	}

	mu.Lock()
	assert.Equal(t, []ulid.ULID{guest.ID}, reaped)
	mu.Unlock()

	deleted := cleaner.deletedIDs()
	assert.Equal(t, []ulid.ULID{guest.ID}, deleted)
}

func TestGuestReaper_SkipsOnListError(t *testing.T) {
	lister := &stubLister{err: errors.New("db unavailable")}
	cleaner := &stubCleaner{}

	config := auth.GuestReaperConfig{
		Interval: 50 * time.Millisecond,
		IdleTTL:  1 * time.Millisecond,
	}

	reaper := auth.NewGuestReaper(config, lister, cleaner)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	reaper.Run(ctx)

	assert.Empty(t, cleaner.deletedIDs(), "cleaner should not be called when listing fails")
}

func TestGuestReaper_ContinuesOnDeleteError(t *testing.T) {
	guest1 := newTestGuest(t, "GuestB")
	guest2 := newTestGuest(t, "GuestC")

	lister := &stubLister{guests: []*auth.Player{guest1, guest2}}
	cleaner := &stubCleaner{
		errs: map[ulid.ULID]error{
			guest1.ID: errors.New("delete failed"),
		},
	}

	var (
		mu     sync.Mutex
		reaped []ulid.ULID
		done   = make(chan struct{})
	)

	config := auth.GuestReaperConfig{
		Interval: 50 * time.Millisecond,
		IdleTTL:  1 * time.Millisecond,
		OnReaped: func(id ulid.ULID) {
			mu.Lock()
			reaped = append(reaped, id)
			// Only guest2 should be reaped successfully
			if len(reaped) == 1 {
				close(done)
			}
			mu.Unlock()
		},
	}

	reaper := auth.NewGuestReaper(config, lister, cleaner)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go reaper.Run(ctx)

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("timed out waiting for second guest to be reaped")
	}

	deleted := cleaner.deletedIDs()
	assert.Equal(t, []ulid.ULID{guest2.ID}, deleted, "only guest2 should be deleted")

	mu.Lock()
	assert.Equal(t, []ulid.ULID{guest2.ID}, reaped, "OnReaped should only be called for successful deletes")
	mu.Unlock()
}

func TestGuestReaper_DefaultConfig(t *testing.T) {
	lister := &stubLister{}
	cleaner := &stubCleaner{}

	reaper := auth.NewGuestReaper(auth.GuestReaperConfig{}, lister, cleaner)
	require.NotNil(t, reaper)

	// Run with a very short context — the reaper should not tick at all
	// (default interval is 1m) but should return promptly on cancel.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	reaper.Run(ctx)
	elapsed := time.Since(start)

	// Should have returned quickly (context cancelled, not a 1-minute wait)
	assert.Less(t, elapsed, 500*time.Millisecond)

	// Lister should not have been called (interval >> context timeout)
	lister.mu.Lock()
	assert.Equal(t, 0, lister.calls)
	lister.mu.Unlock()
}
