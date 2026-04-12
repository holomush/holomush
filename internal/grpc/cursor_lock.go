// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"sync"

	"github.com/holomush/holomush/internal/grpc/focus"
)

// sessionCursorLock is a per-session mutex with a reference count.
//
// The mutex serializes the "Send + commit cursor" critical section in
// replayAndSend against a concurrent Subscribe's cursor read on
// reconnect, deterministically closing the Finding 1 race documented in
// holomush-9ues. The race window — between an event being delivered to
// the client and the persisted cursor reflecting it — is the smallest
// region that requires mutual exclusion for correctness.
//
// refCount is protected by cursorLockMap.mu (NOT by mu). It tracks the
// number of callers that have acquired this lock entry but not yet
// released it, so the entry is only deleted from the map after the last
// holder departs. Without refcounting, an Acquire+Release pair from one
// caller could delete the entry while a concurrent Acquire is between
// the map lookup and the lock.Lock() call, leading to two callers
// holding *different* mutexes for the same session — defeating the
// whole point.
type sessionCursorLock struct {
	mu       sync.Mutex
	refCount int
}

// cursorLockMap is a refcounted per-session mutex registry. Entries are
// auto-deleted when the last holder releases, so steady-state memory
// usage is bounded by the number of sessions currently *inside* a
// critical section, not by the historical session count.
//
// The map's outer mutex (cursorLockMap.mu) is held only for O(1) work:
// a map lookup, a possible insert, and a refcount bump. It is never
// held while waiting on a per-session mu.
type cursorLockMap struct {
	mu    sync.Mutex
	locks map[string]*sessionCursorLock
}

// newCursorLockMap returns an empty cursor lock registry.
func newCursorLockMap() *cursorLockMap {
	return &cursorLockMap{locks: make(map[string]*sessionCursorLock)}
}

// lock acquires the per-session mutex for sessionID and returns a
// function that releases it. The returned function MUST be called
// exactly once, typically via defer.
//
// Concurrent callers for the same sessionID serialize: the second
// caller's lock.Lock() blocks until the first caller's release()
// returns. Concurrent callers for *different* sessionIDs do not
// interfere with each other beyond brief contention on the map's outer
// mutex during acquire/release.
//
// A nil receiver is treated as a no-op and returns a no-op release.
// This is a deliberate concession to the unit-test surface in
// internal/grpc/, which constructs CoreServer literals directly
// (~60 sites) rather than going through NewCoreServer. Production
// always initializes cursorLocks via NewCoreServer, so the nil case
// only fires for tests that have no concurrent reconnect scenario
// to begin with — they correctly observe "no lock contention".
func (m *cursorLockMap) lock(sessionID string) func() {
	if m == nil {
		return func() {}
	}
	entry := m.acquire(sessionID)
	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		m.release(sessionID)
	}
}

// acquire returns the lock entry for sessionID, creating it if
// necessary, and increments its reference count. Held only for the
// duration of one map operation; the per-session mu is NOT taken here.
func (m *cursorLockMap) acquire(sessionID string) *sessionCursorLock {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.locks[sessionID]
	if !ok {
		entry = &sessionCursorLock{}
		m.locks[sessionID] = entry
	}
	entry.refCount++
	return entry
}

// release decrements the reference count for sessionID. When the count
// reaches zero the map entry is deleted, freeing the lock memory.
//
// Callers MUST have already released the per-session mu before calling
// release. The lock helper enforces this ordering via the closure it
// returns.
func (m *cursorLockMap) release(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.locks[sessionID]
	if !ok {
		return
	}
	entry.refCount--
	if entry.refCount <= 0 {
		delete(m.locks, sessionID)
	}
}

// refCount returns the current reference count for sessionID's lock
// entry, or 0 if no entry exists. Read under the map's outer mutex.
//
// This is a test-only synchronization helper used by integration specs
// that need to detect when a concurrent Subscribe handler has queued
// behind an in-flight commit (see holomush-9ues). Production code MUST
// NOT call this — a refcount snapshot has no semantic meaning outside
// the lock map's internals.
func (m *cursorLockMap) refCount(sessionID string) int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry, ok := m.locks[sessionID]; ok {
		return entry.refCount
	}
	return 0
}

// CursorLockerAdapter wraps cursorLockMap to satisfy focus.CursorLocker.
type CursorLockerAdapter struct {
	locks *cursorLockMap
}

// NewCursorLockerAdapter creates a CursorLockerAdapter.
func NewCursorLockerAdapter(m *cursorLockMap) *CursorLockerAdapter {
	return &CursorLockerAdapter{locks: m}
}

// Lock implements focus.CursorLocker.
func (a *CursorLockerAdapter) Lock(sessionID string) func() {
	return a.locks.lock(sessionID)
}

// Ensure CursorLockerAdapter satisfies the focus.CursorLocker interface at compile time.
var _ focus.CursorLocker = (*CursorLockerAdapter)(nil)
