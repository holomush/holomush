// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package retirement hosts the character-retirement reactor (IDENT-04).
//
// D-36 made the retirement fanout an EVENT-DRIVEN host subsystem rather
// than a synchronous call out of world.Service: it consumes
// character_retired off events.<game>.character.> and performs the
// downstream effects (session eviction, move-to-start-location). The
// accepted consequences of that choice are at-least-once JetStream
// redelivery — so the handler MUST be idempotent — and eventual
// consistency of the fanout.
//
// This file is the SKELETON only: the lifecycle contract, the SubsystemID
// and the dependency edges are FINAL, and plan 03-04 fills the Prepare and
// Activate bodies. The edges are declared here rather than discovered in
// 03-04 so the one-time SubsystemID composition cascade lands once.
package retirement

import (
	"context"
	"log/slog"
	"time"

	"github.com/holomush/holomush/internal/lifecycle"
)

// stopTimeout bounds how long Stop waits for the consume loop to unwind.
// Mirrors the outbox relay subsystem's budget.
const stopTimeout = 5 * time.Second

// Config configures the retirement reactor subsystem.
type Config struct {
	Logger *slog.Logger
}

// Subsystem is the lifecycle.Subsystem that owns the retirement fanout.
type Subsystem struct {
	cfg Config
	// prepared guards Prepare. Plan 03-04 replaces the bare bool with the
	// durable jetstream.Consumer handle (nil-field guard, as the outbox
	// relay guards on s.relay); the guard SEMANTICS do not change.
	prepared bool
	// done guards Activate and is closed when the consume loop exits. Nil
	// until Activate has run.
	done chan struct{}
}

// NewSubsystem constructs the retirement reactor. It allocates nothing and
// touches no live resources — cmd/holomush's real-constructor graph tests
// build every production subsystem this way and never call Prepare.
func NewSubsystem(cfg Config) *Subsystem {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Subsystem{cfg: cfg}
}

// ID returns lifecycle.SubsystemRetirementReactor.
func (s *Subsystem) ID() lifecycle.SubsystemID { return lifecycle.SubsystemRetirementReactor }

// DependsOn returns [Database, EventBus, World, Sessions, Bootstrap].
//
// Database + EventBus are the substrate the durable consumer is created
// against. World and Sessions are the fanout's effect surfaces (the
// move-to-start-location write and the session eviction plan 03-04 wires).
// Bootstrap is the subtle one: the fanout's destination is
// StartLocationID(), which is not resolvable — it panics — before
// bootstrap's Prepare has run, so the edge is declared even though the
// skeleton does not yet read it.
func (s *Subsystem) DependsOn() []lifecycle.SubsystemID {
	return []lifecycle.SubsystemID{
		lifecycle.SubsystemDatabase,
		lifecycle.SubsystemEventBus,
		lifecycle.SubsystemWorld,
		lifecycle.SubsystemSessions,
		lifecycle.SubsystemBootstrap,
	}
}

// Prepare creates the durable JetStream consumer — acquisition, no work
// loop, per the Prepare/Activate contract's process-internal-substrate
// carve-out. Inert until plan 03-04; the idempotency guard is live now so
// the contract shape is fixed.
func (s *Subsystem) Prepare(ctx context.Context) error {
	if s.prepared {
		return nil // already prepared
	}
	s.prepared = true
	slog.DebugContext(ctx, "retirement reactor subsystem prepared (no-op until 03-04)")
	return nil
}

// Activate starts the consume loop — domain traffic. Inert until plan
// 03-04. Idempotent behind the done-channel guard.
func (s *Subsystem) Activate(ctx context.Context) error {
	if s.done != nil {
		return nil // already activated
	}
	done := make(chan struct{})
	// No work loop yet: 03-04 launches the Consume loop and closes this on
	// exit. Closing it here keeps Stop's drain wait a no-op today and lets
	// 03-04 add the real wait without touching Stop.
	close(done)
	s.done = done
	slog.DebugContext(ctx, "retirement reactor subsystem activated (no-op until 03-04)")
	return nil
}

// Stop drains the consume loop and resets BOTH guards so a legitimate
// Prepare/Activate retry after Stop rebuilds the consumer and relaunches
// the loop rather than short-circuiting on a torn-down one.
func (s *Subsystem) Stop(_ context.Context) error {
	if s.done != nil {
		select {
		case <-s.done:
		case <-time.After(stopTimeout):
		}
		s.done = nil
	}
	s.prepared = false
	return nil
}
