// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package charactivity hosts the character last_active_at tracker (IDENT-10).
//
// D-42 put last_active_at in a JetStream KV bucket fed by a durable
// activity listener plus a periodic flush ticker, rather than writing the
// column synchronously on every event.
//
// This file is the SKELETON only: the lifecycle contract, the SubsystemID
// and the dependency edges are FINAL, and plan 03-05 fills the Prepare and
// Activate bodies. The edges are declared here rather than discovered in
// 03-05 so the one-time SubsystemID composition cascade lands once.
package charactivity

import (
	"context"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/holomush/holomush/internal/lifecycle"
)

// stopTimeout bounds how long Stop waits for the listener and flush ticker
// to unwind. Mirrors the outbox relay subsystem's budget.
const stopTimeout = 5 * time.Second

// Config configures the character-activity subsystem.
type Config struct {
	Logger *slog.Logger
}

// Subsystem is the lifecycle.Subsystem that owns the last_active_at KV.
type Subsystem struct {
	cfg Config
	// storage selects the KV bucket's backing store. FileStorage is the
	// ZERO VALUE of jetstream.StorageType, so an unset bucket config is
	// file-backed EVERYWHERE — including under test. That is why the
	// constructor comes in a pair (D-42): tests MUST be able to force
	// MemoryStorage explicitly, because "leave it unset" silently selects
	// the disk-backed variant rather than an in-memory one.
	storage jetstream.StorageType
	// prepared guards Prepare. Plan 03-05 replaces the bare bool with the
	// jetstream.KeyValue handle (nil-field guard, as the outbox relay
	// guards on s.relay); the guard SEMANTICS do not change.
	prepared bool
	// done guards Activate and is closed when the listener and flush
	// ticker have exited. Nil until Activate has run.
	done chan struct{}
}

// NewSubsystem constructs the subsystem with the production backing store.
// FileStorage is the default; tests override via NewSubsystemWithStorage.
// It allocates nothing and touches no live resources — cmd/holomush's
// real-constructor graph tests build every production subsystem this way
// and never call Prepare.
func NewSubsystem(cfg Config) *Subsystem {
	return NewSubsystemWithStorage(cfg, jetstream.FileStorage)
}

// NewSubsystemWithStorage allows tests to force MemoryStorage. Mirrors
// eventbus.NewSubsystemWithStorage — see the storage field's comment for
// why this seam is required rather than merely convenient.
func NewSubsystemWithStorage(cfg Config, storage jetstream.StorageType) *Subsystem {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Subsystem{cfg: cfg, storage: storage}
}

// ID returns lifecycle.SubsystemCharacterActivity.
func (s *Subsystem) ID() lifecycle.SubsystemID { return lifecycle.SubsystemCharacterActivity }

// DependsOn returns [Database, EventBus]. Database because the periodic
// flush writes characters.last_active_at; EventBus because the KV bucket
// and the durable activity listener both live on the embedded JetStream.
func (s *Subsystem) DependsOn() []lifecycle.SubsystemID {
	return []lifecycle.SubsystemID{
		lifecycle.SubsystemDatabase,
		lifecycle.SubsystemEventBus,
	}
}

// Prepare creates or attaches the KV bucket and the durable listener
// consumer — acquisition, no work loop, per the Prepare/Activate contract's
// process-internal-substrate carve-out. Inert until plan 03-05; the
// idempotency guard is live now so the contract shape is fixed.
func (s *Subsystem) Prepare(ctx context.Context) error {
	if s.prepared {
		return nil // already prepared
	}
	s.prepared = true
	slog.DebugContext(ctx, "character activity subsystem prepared (no-op until 03-05)",
		"kv_storage", s.storage.String())
	return nil
}

// Activate starts the listener and the periodic flush ticker — domain
// traffic. Inert until plan 03-05. Idempotent behind the done-channel guard.
func (s *Subsystem) Activate(ctx context.Context) error {
	if s.done != nil {
		return nil // already activated
	}
	done := make(chan struct{})
	// No work loop yet: 03-05 launches the listener + flush ticker and
	// closes this once both exit. Closing it here keeps Stop's drain wait a
	// no-op today and lets 03-05 add the real wait without touching Stop.
	close(done)
	s.done = done
	slog.DebugContext(ctx, "character activity subsystem activated (no-op until 03-05)")
	return nil
}

// Stop drains the listener and flush ticker and resets BOTH guards so a
// legitimate Prepare/Activate retry after Stop reattaches the bucket and
// relaunches the loops rather than short-circuiting on a torn-down one.
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
