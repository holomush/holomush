// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/internal/world/outbox"
	worldpostgres "github.com/holomush/holomush/internal/world/postgres"
	"github.com/holomush/holomush/internal/world/wmodel"
)

// defaultRelayGameID mirrors world.ServiceConfig's default so the relay drains
// the same per-game feed the writer populates.
const defaultRelayGameID = "main"

// referenceConsumerName is the durable name of the ONLY consumer Phase 5 ships
// (zero product projections).
const referenceConsumerName = "world-reference"

// relayStopTimeout bounds how long Stop waits for the relay loop to unwind.
const relayStopTimeout = 5 * time.Second

// EventBusProvider provides a Publisher from the event-bus subsystem without a
// direct import at the wiring site.
type EventBusProvider interface {
	Publisher(opts ...eventbus.PublishOption) eventbus.Publisher
}

// OutboxRelaySubsystemConfig configures the OutboxRelaySubsystem.
type OutboxRelaySubsystemConfig struct {
	DB       PoolProvider
	EventBus EventBusProvider
	GameID   string
	Logger   *slog.Logger
}

// OutboxRelaySubsystem is the dedicated lifecycle subsystem that runs the single
// leased outbox relay (05-07). It is SEPARATE from WorldSubsystem: WorldSubsystem
// gains no event-bus dependency; the relay owns the publish path. It DependsOn
// Database + EventBus.
type OutboxRelaySubsystem struct {
	cfg      OutboxRelaySubsystemConfig
	relay    *outbox.Relay
	consumer *outbox.Consumer
	waker    *outboxWaker
	cancel   context.CancelFunc
	done     chan struct{}
}

// NewOutboxRelaySubsystem constructs an OutboxRelaySubsystem.
func NewOutboxRelaySubsystem(cfg OutboxRelaySubsystemConfig) *OutboxRelaySubsystem {
	if cfg.GameID == "" {
		cfg.GameID = defaultRelayGameID
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &OutboxRelaySubsystem{cfg: cfg}
}

// ID returns SubsystemOutboxRelay.
func (s *OutboxRelaySubsystem) ID() lifecycle.SubsystemID { return lifecycle.SubsystemOutboxRelay }

// DependsOn returns [SubsystemDatabase, SubsystemEventBus].
func (s *OutboxRelaySubsystem) DependsOn() []lifecycle.SubsystemID {
	return []lifecycle.SubsystemID{lifecycle.SubsystemDatabase, lifecycle.SubsystemEventBus}
}

// Start constructs the leased relay + the reference consumer and launches the
// relay drain loop. codecov:ignore — exercised by integration and E2E tests.
func (s *OutboxRelaySubsystem) Start(ctx context.Context) error {
	pool := s.cfg.DB.Pool()
	store := worldpostgres.NewOutboxStore(pool)
	checkpoint := worldpostgres.NewConsumerCheckpointStore(pool)
	publisher := s.cfg.EventBus.Publisher()

	s.waker = newOutboxWaker(pool)
	s.relay = outbox.NewRelay(outbox.RelayConfig{
		Store:     leaseStoreAdapter{store: store},
		Publisher: publisher,
		GameID:    s.cfg.GameID,
		Waker:     s.waker,
		Logger:    s.cfg.Logger,
	})
	// The reference idempotent consumer is constructed here (it exists and is
	// wired); its live durable-JetStream consume loop + genesis snapshot land in
	// 05-11 (this plan wires the consumer/bootstrap + subsystem plumbing only).
	s.consumer = outbox.NewConsumer(referenceConsumerName, checkpointStoreAdapter{store: checkpoint}, nil, s.cfg.Logger)

	runCtx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		_ = s.relay.Run(runCtx) //nolint:errcheck // Run returns only the ctx-cancellation reason on Stop
	}()

	slog.InfoContext(ctx, "outbox relay subsystem started", "game_id", s.cfg.GameID)
	return nil
}

// Stop cancels the relay loop, closes the waker, and releases the lease.
// codecov:ignore — exercised by integration and E2E tests.
func (s *OutboxRelaySubsystem) Stop(ctx context.Context) error {
	if s.cancel != nil {
		s.cancel()
	}
	if s.waker != nil {
		s.waker.Close()
	}
	if s.done != nil {
		select {
		case <-s.done:
		case <-time.After(relayStopTimeout):
		}
	}
	if s.relay != nil {
		_ = s.relay.Stop(ctx) //nolint:errcheck // Stop only releases the lease; a release warning is already logged
	}
	return nil
}

// Consumer exposes the reference consumer (for bootstrap wiring / tests).
func (s *OutboxRelaySubsystem) Consumer() *outbox.Consumer { return s.consumer }

// --- setup-layer adapters: bind the concrete postgres impls to the
// consumer-owned outbox interfaces so package postgres never imports
// internal/world/outbox and internal/world/outbox never imports postgres. ---

// NewOutboxStore adapts the concrete postgres outbox store to the consumer-owned
// outbox.OutboxStore interface. Used by the relay subsystem AND the `holomush
// outbox skip` admin CLI so neither has to re-derive the interface binding.
func NewOutboxStore(store *worldpostgres.OutboxStore) outbox.OutboxStore {
	return leaseStoreAdapter{store: store}
}

// leaseStoreAdapter binds *worldpostgres.OutboxStore to outbox.OutboxStore. The
// concrete *worldpostgres.OutboxLease structurally satisfies outbox.Lease.
type leaseStoreAdapter struct {
	store *worldpostgres.OutboxStore
}

func (a leaseStoreAdapter) AcquireLease(ctx context.Context, gameID string) (outbox.Lease, error) {
	lease, err := a.store.AcquireLease(ctx, gameID)
	if err != nil {
		return nil, err //nolint:wrapcheck // adapter pass-through; the postgres impl already codes the error
	}
	return lease, nil
}

// checkpointStoreAdapter binds *worldpostgres.ConsumerCheckpointStore to
// outbox.ConsumerCheckpointStore, bridging the tx-bound executor type
// (worldpostgres.TxExecutor → outbox.TxExecutor, structurally identical).
type checkpointStoreAdapter struct {
	store *worldpostgres.ConsumerCheckpointStore
}

func (a checkpointStoreAdapter) ApplyOnce(
	ctx context.Context,
	consumer string,
	envelope wmodel.Envelope,
	effect func(effCtx context.Context, exec outbox.TxExecutor) error,
) (bool, error) {
	//nolint:wrapcheck // adapter pass-through; the postgres impl already codes the error
	return a.store.ApplyOnce(ctx, consumer, envelope, func(effCtx context.Context, exec worldpostgres.TxExecutor) error {
		return effect(effCtx, exec)
	})
}

func (a checkpointStoreAdapter) InitWatermark(ctx context.Context, consumer, gameID string, epoch, position int64) error {
	return a.store.InitWatermark(ctx, consumer, gameID, epoch, position) //nolint:wrapcheck // adapter pass-through
}

func (a checkpointStoreAdapter) Watermark(ctx context.Context, consumer, gameID string) (epoch, position int64, ok bool, err error) {
	return a.store.Watermark(ctx, consumer, gameID) //nolint:wrapcheck // adapter pass-through
}

// outboxWaker is the relay's dedicated LISTEN connection. It holds one pinned
// pool connection LISTENing on the outbox NOTIFY channel and blocks in Wait until
// a transaction-side pg_notify arrives or the context is done (the relay wraps
// Wait with its sweep interval, so a missed NOTIFY still sweeps).
type outboxWaker struct {
	pool *pgxpool.Pool
	mu   sync.Mutex
	conn *pgxpool.Conn
}

func newOutboxWaker(pool *pgxpool.Pool) *outboxWaker { return &outboxWaker{pool: pool} }

// Wait blocks until a NOTIFY on the outbox channel or ctx is done.
func (w *outboxWaker) Wait(ctx context.Context) error {
	w.mu.Lock()
	if w.conn == nil {
		conn, err := w.pool.Acquire(ctx)
		if err != nil {
			w.mu.Unlock()
			return err //nolint:wrapcheck // transient acquire failure; relay falls back to sweep
		}
		if _, err := conn.Exec(ctx, "LISTEN "+worldpostgres.OutboxNotifyChannel); err != nil {
			conn.Release()
			w.mu.Unlock()
			return err //nolint:wrapcheck // transient listen failure; relay falls back to sweep
		}
		w.conn = conn
	}
	conn := w.conn
	w.mu.Unlock()

	_, err := conn.Conn().WaitForNotification(ctx)
	if err != nil {
		// A benign ctx-deadline/cancel AND a dead pinned connection (Postgres
		// failover/restart, proxy idle-kill) both land here. Release the pinned
		// conn so the next Wait re-acquires a live one and re-LISTENs; without
		// this reset a dead connection is reused forever, WaitForNotification
		// errors instantly every call, and the relay busy-loops on NextUnpublished
		// scans (CR-01). Releasing on a benign deadline just re-acquires next
		// tick — cheap and correct.
		w.mu.Lock()
		if w.conn == conn {
			w.conn.Release()
			w.conn = nil
		}
		w.mu.Unlock()
	}
	return err //nolint:wrapcheck // ctx-deadline/notify signal; relay ignores it and re-drains
}

// Close releases the pinned LISTEN connection.
func (w *outboxWaker) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.conn != nil {
		w.conn.Release()
		w.conn = nil
	}
}
