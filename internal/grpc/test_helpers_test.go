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

// newHandleCommandServer creates a CoreServer wired with the unified command
// dispatcher. Tests that call HandleCommand MUST use this helper.
//
// The store is used for both Engine and dispatcher Services.Events.
// Pass a custom sessStore to pre-populate sessions; nil uses a fresh Postgres-backed store.
func newHandleCommandServer(t *testing.T, store core.EventAppender, sessStore session.Store, opts ...CoreServerOption) *CoreServer {
	t.Helper()
	engine := core.NewEngine(store)
	if sessStore == nil {
		sessStore = sessiontest.NewStore(t)
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
	allOpts = append(
		allOpts,
		WithEventStore(store),
		WithPlayerSessionRepo(newFakePlayerSessionRepo(ulid.ULID{})),
	)
	allOpts = append(allOpts, opts...)

	return NewCoreServer(engine, sessStore, dispatcher, svc, allOpts...)
}

// mockEventStore implements core.EventAppender for testing.
type mockEventStore struct {
	appendFunc func(ctx context.Context, event core.Event) error
}

func (m *mockEventStore) Append(ctx context.Context, event core.Event) error {
	if m.appendFunc != nil {
		return m.appendFunc(ctx, event)
	}
	return nil
}

var _ core.EventAppender = (*mockEventStore)(nil)
