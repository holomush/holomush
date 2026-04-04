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

// NewSubsystem creates a database subsystem. No live resources are allocated.
func NewSubsystem(cfg SubsystemConfig) *DatabaseSubsystem {
	return &DatabaseSubsystem{cfg: cfg}
}

// ID returns SubsystemDatabase.
func (s *DatabaseSubsystem) ID() lifecycle.SubsystemID { return lifecycle.SubsystemDatabase }

// DependsOn returns nil — database has no dependencies.
func (s *DatabaseSubsystem) DependsOn() []lifecycle.SubsystemID { return nil }

// Start connects to the database, creates the event store, and initializes the game ID.
// Start is idempotent: if the subsystem is already started, it returns nil immediately.
// This allows the database subsystem to be started early (before the orchestrator)
// while still being registered for dependency resolution and reverse-order shutdown.
// codecov:ignore — tested by integration and E2E tests
func (s *DatabaseSubsystem) Start(ctx context.Context) error {
	if s.eventStore != nil {
		return nil // already started
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

	slog.Info("database subsystem started", "game_id", gameID)
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

// Pool returns the database connection pool. Panics if called before Start().
func (s *DatabaseSubsystem) Pool() *pgxpool.Pool {
	if s.pool == nil {
		panic("store: Pool() called before Start()")
	}
	return s.pool
}

// EventStore returns the PostgresEventStore. Panics if called before Start().
func (s *DatabaseSubsystem) EventStore() *PostgresEventStore {
	if s.eventStore == nil {
		panic("store: EventStore() called before Start()")
	}
	return s.eventStore
}

// GameID returns the initialized game ID. Panics if called before Start().
func (s *DatabaseSubsystem) GameID() string {
	if s.gameID == "" {
		panic("store: GameID() called before Start()")
	}
	return s.gameID
}
