// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/lifecycle"
)

// SubsystemConfig configures the database subsystem.
type SubsystemConfig struct {
	// DatabaseURL is the PostgreSQL connection string.
	DatabaseURL string

	// EventStoreFactory creates an event store. If nil, uses NewPostgresEventStore.
	EventStoreFactory func(ctx context.Context, url string) (*PostgresEventStore, error)
}

// DatabaseSubsystem manages the database connection pool and event store.
type DatabaseSubsystem struct {
	cfg        SubsystemConfig
	eventStore *PostgresEventStore
	pool       *pgxpool.Pool
	gameID     string
}

// NewSubsystem creates a DatabaseSubsystem configured with cfg.
// It does not allocate connections or other live resources; Start must be called to initialize the event store, connection pool, and game ID.
func NewSubsystem(cfg SubsystemConfig) *DatabaseSubsystem {
	return &DatabaseSubsystem{cfg: cfg}
}

// ID returns SubsystemDatabase.
func (s *DatabaseSubsystem) ID() lifecycle.SubsystemID { return lifecycle.SubsystemDatabase }

// DependsOn returns nil — database has no dependencies.
func (s *DatabaseSubsystem) DependsOn() []lifecycle.SubsystemID { return nil }

// Prepare connects to the database, creates the event store, and initializes
// the game ID. A pgxpool is a handle — nothing external reaches it — so the
// entire acquisition sequence belongs in Prepare (D-13.3 row 1).
// Prepare is idempotent: if the subsystem is already prepared, it returns
// nil immediately (a real guard: the database subsystem's own event store
// launches no goroutine, but re-running InitGameID would be wasted work and
// the guard keeps the accessors' single-assignment contract intact).
// codecov:ignore — tested by integration and E2E tests
func (s *DatabaseSubsystem) Prepare(ctx context.Context) error {
	if s.eventStore != nil {
		return nil // already prepared
	}

	factory := s.cfg.EventStoreFactory
	if factory == nil {
		factory = NewPostgresEventStore
	}

	es, err := factory(ctx, s.cfg.DatabaseURL)
	if err != nil {
		return oops.Code("DB_CONNECT_FAILED").Wrap(err)
	}
	s.eventStore = es
	s.pool = es.Pool()

	gameID, err := es.InitGameID(ctx)
	if err != nil {
		es.Close()
		s.eventStore = nil
		s.pool = nil
		return oops.Code("GAME_ID_INIT_FAILED").Wrap(err)
	}
	s.gameID = gameID

	slog.InfoContext(ctx, "database subsystem prepared", "game_id", gameID)
	return nil
}

// Activate is a no-op — the database subsystem serves no external surface;
// a pgxpool handle is acquired, not activated (D-13.3 row 1).
func (s *DatabaseSubsystem) Activate(_ context.Context) error {
	return nil
}

// Stop closes the event store and its connection pool.
// codecov:ignore — tested by integration and E2E tests
func (s *DatabaseSubsystem) Stop(_ context.Context) error {
	if s.eventStore != nil {
		s.eventStore.Close()
	}
	return nil
}

// Pool returns the database connection pool. Panics if called before Prepare().
func (s *DatabaseSubsystem) Pool() *pgxpool.Pool {
	if s.pool == nil {
		panic("store: Pool() called before Prepare()")
	}
	return s.pool
}

// EventStore returns the PostgresEventStore. Panics if called before Prepare().
func (s *DatabaseSubsystem) EventStore() *PostgresEventStore {
	if s.eventStore == nil {
		panic("store: EventStore() called before Prepare()")
	}
	return s.eventStore
}

// GameID returns the initialized game ID. Panics if called before Prepare().
func (s *DatabaseSubsystem) GameID() string {
	if s.gameID == "" {
		panic("store: GameID() called before Prepare()")
	}
	return s.gameID
}
