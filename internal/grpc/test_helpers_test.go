// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/session"
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

// newTestSessionStore creates a session.MemStore pre-populated with the given sessions.
func newTestSessionStore(t *testing.T, sessions map[string]*session.Info) session.Store {
	t.Helper()
	store := session.NewMemStore()
	ctx := context.Background()
	for id, info := range sessions {
		if info.ID == "" {
			info.ID = id
		}
		require.NoError(t, store.Set(ctx, id, info))
	}
	return store
}

// newHandleCommandServer creates a CoreServer wired with the unified command
// dispatcher. Tests that call HandleCommand MUST use this helper.
//
// The store is used for both Engine and dispatcher Services.Events.
// Pass a custom sessStore to pre-populate sessions; nil uses a fresh MemStore.
func newHandleCommandServer(t *testing.T, store core.EventStore, sessStore session.Store, opts ...CoreServerOption) *CoreServer {
	t.Helper()
	engine := core.NewEngine(store)
	if sessStore == nil {
		sessStore = session.NewMemStore()
	}

	reg := command.NewRegistry()
	registerTestCommands(t, reg)

	policyEngine := policytest.AllowAllEngine()
	svc := command.NewTestServices(command.ServicesConfig{
		World:   nil,
		Session: sessStore,
		Engine:  policyEngine,
		Events:  store,
	})

	dispatcher, err := command.NewDispatcher(reg, policyEngine)
	require.NoError(t, err)

	allOpts := make([]CoreServerOption, 0, 2+len(opts))
	allOpts = append(allOpts,
		WithEventStore(store),
		WithPlayerSessionRepo(newFakePlayerSessionRepo(ulid.ULID{})),
	)
	allOpts = append(allOpts, opts...)

	return NewCoreServer(engine, sessStore, dispatcher, svc, allOpts...)
}

// mockEventStore implements core.EventStore for testing.
type mockEventStore struct {
	appendFunc      func(ctx context.Context, event core.Event) error
	replayFunc      func(ctx context.Context, stream string, afterID ulid.ULID, limit int) ([]core.Event, error)
	lastEventIDFunc func(ctx context.Context, stream string) (ulid.ULID, error)
	subscribeFunc   func(ctx context.Context, stream string) (<-chan ulid.ULID, <-chan error, error)
}

func (m *mockEventStore) Append(ctx context.Context, event core.Event) error {
	if m.appendFunc != nil {
		return m.appendFunc(ctx, event)
	}
	return nil
}

func (m *mockEventStore) Replay(ctx context.Context, stream string, afterID ulid.ULID, limit int) ([]core.Event, error) {
	if m.replayFunc != nil {
		return m.replayFunc(ctx, stream, afterID, limit)
	}
	return nil, nil
}

func (m *mockEventStore) LastEventID(ctx context.Context, stream string) (ulid.ULID, error) {
	if m.lastEventIDFunc != nil {
		return m.lastEventIDFunc(ctx, stream)
	}
	return ulid.ULID{}, core.ErrStreamEmpty
}

func (m *mockEventStore) Subscribe(ctx context.Context, stream string) (<-chan ulid.ULID, <-chan error, error) {
	if m.subscribeFunc != nil {
		return m.subscribeFunc(ctx, stream)
	}
	eventCh := make(chan ulid.ULID)
	errCh := make(chan error)
	go func() {
		<-ctx.Done()
		close(eventCh)
		close(errCh)
	}()
	return eventCh, errCh, nil
}

func (m *mockEventStore) ReplayTail(_ context.Context, _ string, _ int, _ time.Time, _ ulid.ULID) ([]core.Event, error) {
	return nil, nil
}

func (m *mockEventStore) SubscribeSession(_ context.Context) (core.Subscription, error) {
	return nil, nil
}
