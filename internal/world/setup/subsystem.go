// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package setup provides the world subsystem lifecycle wrapper.
package setup

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/internal/world"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
)

// PoolProvider provides a database connection pool. Implemented by the
// database subsystem without requiring a direct import.
type PoolProvider interface {
	Pool() *pgxpool.Pool
}

// EngineProvider provides an ABAC policy engine. Implemented by the
// ABAC subsystem without requiring a direct import.
type EngineProvider interface {
	Engine() types.AccessPolicyEngine
}

// WorldSubsystemConfig configures the world subsystem.
type WorldSubsystemConfig struct {
	DB   PoolProvider
	ABAC EngineProvider
	// GameID resolves the game ID at Start time (07-09 item 7) — a provider,
	// not a live value. Keys the outbox feed counter + the outbox row's
	// game_id. A nil provider, or one resolving to "", leaves
	// world.ServiceConfig's own "main" default in effect. MUST resolve to
	// the same value as the OutboxRelaySubsystem's GameID so the writer and
	// the relay share one feed.
	GameID func() string
}

// WorldSubsystem manages the WorldService and all world repositories.
type WorldSubsystem struct {
	cfg        WorldSubsystemConfig
	service    *world.Service
	transactor world.Transactor
}

// NewWorldSubsystem creates a WorldSubsystem using the provided WorldSubsystemConfig.
// It does not allocate or start any runtime resources; call Start to initialize the service and transactor.
func NewWorldSubsystem(cfg WorldSubsystemConfig) *WorldSubsystem {
	return &WorldSubsystem{cfg: cfg}
}

// ID returns SubsystemWorld.
func (s *WorldSubsystem) ID() lifecycle.SubsystemID { return lifecycle.SubsystemWorld }

// DependsOn returns [SubsystemDatabase, SubsystemABAC].
func (s *WorldSubsystem) DependsOn() []lifecycle.SubsystemID {
	return []lifecycle.SubsystemID{lifecycle.SubsystemDatabase, lifecycle.SubsystemABAC}
}

// Start creates all world repositories, transactor, and WorldService.
// codecov:ignore — tested by integration and E2E tests
func (s *WorldSubsystem) Start(ctx context.Context) error {
	pool := s.cfg.DB.Pool()
	engine := s.cfg.ABAC.Engine()

	var gameID string
	if s.cfg.GameID != nil {
		gameID = s.cfg.GameID()
	}

	transactor := worldpostgres.NewTransactor(pool)

	s.service = world.NewService(world.ServiceConfig{
		LocationRepo:  worldpostgres.NewLocationRepository(pool),
		ExitRepo:      worldpostgres.NewExitRepository(pool),
		ObjectRepo:    worldpostgres.NewObjectRepository(pool),
		SceneRepo:     worldpostgres.NewSceneRepository(pool),
		CharacterRepo: worldpostgres.NewCharacterRepository(pool),
		PropertyRepo:  worldpostgres.NewPropertyRepository(pool),
		Engine:        engine,
		Transactor:    transactor,
		// The production world.Service finally gets a real OutboxWriter (05-07):
		// the postgres outbox store, replacing the dead no-emitter leg. The relay
		// is a SEPARATE subsystem; the writer only persists the same-tx envelope.
		OutboxWriter: worldpostgres.NewOutboxStore(pool),
		GameID:       gameID,
	})
	s.transactor = transactor

	slog.InfoContext(ctx, "world subsystem started")
	return nil
}

// Stop is a no-op — world services are stateless after init.
// codecov:ignore — tested by integration and E2E tests
func (s *WorldSubsystem) Stop(_ context.Context) error { return nil }

// Service returns the WorldService. Panics if called before Start().
func (s *WorldSubsystem) Service() *world.Service {
	if s.service == nil {
		panic("world/setup: Service() called before Start()")
	}
	return s.service
}

// Transactor returns the world Transactor. Panics if called before Start().
func (s *WorldSubsystem) Transactor() world.Transactor {
	if s.transactor == nil {
		panic("world/setup: Transactor() called before Start()")
	}
	return s.transactor
}
