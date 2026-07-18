// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package setup provides the session subsystem lifecycle wrapper.
// It lives in a sub-package to avoid import cycles: internal/store imports
// internal/session (for the session.Store interface), so the subsystem that
// imports internal/store cannot reside in internal/session itself.
package setup

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/internal/store"
)

// PoolProvider provides a database connection pool. Implemented by the
// database subsystem without requiring a direct import.
type PoolProvider interface {
	Pool() *pgxpool.Pool
}

// SessionSubsystemConfig configures the session subsystem.
type SessionSubsystemConfig struct {
	DB PoolProvider
}

// SessionSubsystem manages the PostgresSessionStore.
type SessionSubsystem struct {
	cfg          SessionSubsystemConfig
	sessionStore *store.PostgresSessionStore
}

// NewSessionSubsystem constructs a SessionSubsystem configured with the provided SessionSubsystemConfig.
// It does not allocate or start any live resources; Start must be called to initialize the session store.
func NewSessionSubsystem(cfg SessionSubsystemConfig) *SessionSubsystem {
	return &SessionSubsystem{cfg: cfg}
}

// ID returns SubsystemSessions.
func (s *SessionSubsystem) ID() lifecycle.SubsystemID { return lifecycle.SubsystemSessions }

// DependsOn returns [SubsystemDatabase].
func (s *SessionSubsystem) DependsOn() []lifecycle.SubsystemID {
	return []lifecycle.SubsystemID{lifecycle.SubsystemDatabase}
}

// Prepare creates the PostgresSessionStore from the database pool — the
// entire current body (D-13.3 row 7). No idempotency guard is needed:
// reassignment is benign.
// codecov:ignore — tested by integration and E2E tests
func (s *SessionSubsystem) Prepare(ctx context.Context) error {
	s.sessionStore = store.NewPostgresSessionStore(s.cfg.DB.Pool())
	slog.InfoContext(ctx, "session subsystem prepared")
	return nil
}

// Activate is a documented no-op (D-13.3 row 7, cross-AI round 5 settled):
// this Start launches no reaper, ticker, or goroutine. The session reaper is
// a grpcSubsystem-owned loop (D-13.3 row 16) — do not relocate it here.
func (s *SessionSubsystem) Activate(_ context.Context) error { return nil }

// Stop is a no-op — the session store requires no explicit cleanup.
// codecov:ignore — tested by integration and E2E tests
func (s *SessionSubsystem) Stop(_ context.Context) error { return nil }

// Store returns the PostgresSessionStore. Panics if called before Prepare().
func (s *SessionSubsystem) Store() *store.PostgresSessionStore {
	if s.sessionStore == nil {
		panic("session/setup: Store() called before Prepare()")
	}
	return s.sessionStore
}
