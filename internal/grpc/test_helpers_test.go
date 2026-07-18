// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/presence"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/testsupport/sessiontest"
)

// testPlayerSessionToken is the canonical token unit tests pass in
// HandleCommandRequest.PlayerSessionToken. The fakePlayerSessionRepo
// installed by newHandleCommandServer is seeded to match this token
// hash and return a PlayerSession whose PlayerID is the zero ULID —
// which lines up with the zero PlayerID on session.Info seeded by the
// map literals used throughout the unit tests. That makes ownership
// validation succeed for these tests without requiring them to seed
// matching PlayerIDs.
const testPlayerSessionToken = "unit-test-player-session-token"

// fakePlayerSessionRepo is a minimal auth.PlayerSessionRepository impl
// used by the HandleCommand unit test helpers.
type fakePlayerSessionRepo struct {
	tokenHash string
	playerID  ulid.ULID
}

// newFakePlayerSessionRepo constructs a fakePlayerSessionRepo seeded to
// accept testPlayerSessionToken and return the given PlayerID.
func newFakePlayerSessionRepo(playerID ulid.ULID) *fakePlayerSessionRepo {
	return &fakePlayerSessionRepo{
		tokenHash: auth.HashSessionToken(testPlayerSessionToken),
		playerID:  playerID,
	}
}

func (f *fakePlayerSessionRepo) Create(_ context.Context, _ *auth.PlayerSession) error {
	panic("fakePlayerSessionRepo: Create not implemented")
}

func (f *fakePlayerSessionRepo) CreateWithCap(_ context.Context, _ *auth.PlayerSession, _ int) ([]ulid.ULID, error) {
	panic("fakePlayerSessionRepo: CreateWithCap not implemented")
}

func (f *fakePlayerSessionRepo) GetByTokenHash(_ context.Context, tokenHash string) (*auth.PlayerSession, error) {
	if tokenHash != f.tokenHash {
		return nil, auth.ErrNotFound
	}
	return &auth.PlayerSession{
		ID:        ulid.ULID{},
		PlayerID:  f.playerID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}, nil
}

func (f *fakePlayerSessionRepo) GetByID(_ context.Context, _ ulid.ULID) (*auth.PlayerSession, error) {
	panic("fakePlayerSessionRepo: GetByID not implemented")
}

func (f *fakePlayerSessionRepo) CountActiveByPlayer(_ context.Context, _ ulid.ULID) (int, error) {
	panic("fakePlayerSessionRepo: CountActiveByPlayer not implemented")
}

func (f *fakePlayerSessionRepo) ListByPlayer(_ context.Context, _ ulid.ULID) ([]*auth.PlayerSession, error) {
	panic("fakePlayerSessionRepo: ListByPlayer not implemented")
}

func (f *fakePlayerSessionRepo) Delete(_ context.Context, _ ulid.ULID) error {
	panic("fakePlayerSessionRepo: Delete not implemented")
}

func (f *fakePlayerSessionRepo) DeleteByPlayer(_ context.Context, _ ulid.ULID) error {
	panic("fakePlayerSessionRepo: DeleteByPlayer not implemented")
}

func (f *fakePlayerSessionRepo) DeleteOldestForPlayer(_ context.Context, _ ulid.ULID) (*auth.PlayerSession, error) {
	panic("fakePlayerSessionRepo: DeleteOldestForPlayer not implemented")
}

func (f *fakePlayerSessionRepo) DeleteExpired(_ context.Context) (int64, error) {
	panic("fakePlayerSessionRepo: DeleteExpired not implemented")
}

func (f *fakePlayerSessionRepo) RefreshTTL(_ context.Context, _ ulid.ULID, _ time.Duration) error {
	panic("fakePlayerSessionRepo: RefreshTTL not implemented")
}

// Compile-time interface check.
var _ auth.PlayerSessionRepository = (*fakePlayerSessionRepo)(nil)

// newTestSessionStore creates a Postgres-backed session.Store pre-populated with the given sessions.
// Drift fix (holomush-9mxr Task 10): The former in-memory store accepted any session.Info regardless of Status;
// PostgresSessionStore requires a valid Status on read-back (IsValid() rejects "").
// Sessions with no Status set default to StatusActive so callers that only care about
// the session existing don't need to specify it.
func newTestSessionStore(t *testing.T, sessions map[string]*session.Info) session.Store {
	t.Helper()
	store := sessiontest.NewStore(t)
	ctx := context.Background()
	for id, info := range sessions {
		if info.ID == "" {
			info.ID = id
		}
		if info.Status == "" {
			info.Status = session.StatusActive
		}
		require.NoError(t, store.Set(ctx, id, info))
		// ListActiveByLocation requires EXISTS(terminal/telnet connection) as of
		// holomush-5rh.8.9. Add a synthetic terminal row for every grid-present
		// session so that helpers calling newTestSessionStore do not need to be
		// individually updated. Non-grid-present sessions (GridPresent=false)
		// intentionally receive no connection row so the filter still excludes them.
		if info.GridPresent {
			require.NoError(t, store.AddConnection(ctx, &session.Connection{
				ID:         ulid.Make(),
				SessionID:  info.ID,
				ClientType: "terminal",
				Streams:    []string{},
			}))
		}
	}
	return store
}

// testEventStore is a fake eventbus.Publisher that records published events
// per (game-relative) stream name, replacing the retired EventAppender-backed
// in-memory store for this package's test suite now that the core-package
// Event type and its appender interface are deleted (ARCH-04). It backs both
// the presence emitter and CoreServer.publisher (WithEventPublisher)
// in tests, and the say/pose/ooc test command handlers (registerTestCommands),
// all writing into ONE shared in-memory log so tests can Replay-assert across
// dispatcher-emitted and presence-emitted events — matching this package's
// pre-migration (internal/core game-engine) test shape.
type testEventStore struct {
	mu          sync.RWMutex
	streams     map[string][]eventbus.Event
	publishFunc func(ctx context.Context, event eventbus.Event) error
}

func newTestEventStore() *testEventStore {
	return &testEventStore{streams: make(map[string][]eventbus.Event)}
}

var _ eventbus.Publisher = (*testEventStore)(nil)

// Publish satisfies eventbus.Publisher. The subject is stored under its
// game-relative stream name (trimming the "events.main." qualification
// prefix WithEventPublisher's gameID="main" always produces in this
// package's fixtures) so Replay callers can look it up by the same relative
// stream string production/test code emits with (e.g. "location.01ABC").
func (s *testEventStore) Publish(ctx context.Context, event eventbus.Event) error {
	if s.publishFunc != nil {
		return s.publishFunc(ctx, event)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stream := strings.TrimPrefix(string(event.Subject), "events.main.")
	s.streams[stream] = append(s.streams[stream], event)
	return nil
}

// Replay returns events recorded for stream (game-relative form, e.g.
// "character.01ABC") with ID greater than afterID, up to limit — mirroring
// the retired coretest.MemoryEventStore.Replay test-inspection helper's
// shape so existing assertions keep working against []eventbus.Event.
func (s *testEventStore) Replay(_ context.Context, stream string, afterID ulid.ULID, limit int) ([]eventbus.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	events := s.streams[stream]
	if len(events) == 0 {
		return nil, nil
	}

	startIdx := 0
	if afterID.Compare(ulid.ULID{}) != 0 {
		found := false
		for i := range events {
			if events[i].ID == afterID {
				startIdx = i + 1
				found = true
				break
			}
		}
		if !found {
			return nil, nil
		}
	}

	endIdx := min(startIdx+limit, len(events))
	result := make([]eventbus.Event, endIdx-startIdx)
	copy(result, events[startIdx:endIdx])
	return result, nil
}

// newTestPresenceEmitter builds a *presence.Emitter directly over store
// (an eventbus.Publisher), with gameID fixed to "main" (matching this
// package's test fixtures, which build relative streams like
// "location."+id.String() and expect them to land unqualified — i.e.
// stripped of "events.main." — in the shared store).
func newTestPresenceEmitter(store eventbus.Publisher) *presence.Emitter {
	return presence.NewEmitter(store, func() string { return "main" })
}

// newHandleCommandServer creates a CoreServer wired with the unified command
// dispatcher. Tests that call HandleCommand MUST use this helper.
//
// The store is used for both the presence emitter and the say/pose/ooc
// stub command handlers registerTestCommands registers. Pass a custom
// sessStore to pre-populate sessions; nil uses a fresh Postgres-backed store.
func newHandleCommandServer(t *testing.T, store eventbus.Publisher, sessStore session.Store, opts ...CoreServerOption) *CoreServer {
	t.Helper()
	pres := newTestPresenceEmitter(store)
	if sessStore == nil {
		sessStore = sessiontest.NewStore(t)
	}

	reg := command.NewRegistry()
	registerTestCommands(t, reg, store)

	policyEngine := policytest.AllowAllEngine()
	svc := command.NewTestServices(command.ServicesConfig{
		World:   nil,
		Session: sessStore,
		Engine:  policyEngine,
	})

	dispatcher, err := command.NewDispatcher(reg, policyEngine)
	require.NoError(t, err)

	allOpts := make([]CoreServerOption, 0, 2+len(opts))
	allOpts = append(
		allOpts,
		WithEventPublisher(store, func() string { return "main" }),
		WithPlayerSessionRepo(newFakePlayerSessionRepo(ulid.ULID{})),
	)
	allOpts = append(allOpts, opts...)

	return NewCoreServer(pres, sessStore, dispatcher, svc, allOpts...)
}

// mockEventStore implements eventbus.Publisher for testing.
type mockEventStore struct {
	publishFunc func(ctx context.Context, event eventbus.Event) error
}

func (m *mockEventStore) Publish(ctx context.Context, event eventbus.Event) error {
	if m.publishFunc != nil {
		return m.publishFunc(ctx, event)
	}
	return nil
}

var _ eventbus.Publisher = (*mockEventStore)(nil)
