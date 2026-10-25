// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
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
	// GameID resolves the game ID at Start time (07-09 item 7) — a provider,
	// not a live value, since the resolved id is not known until the
	// database subsystem's InitGameID has run. A nil provider, or one
	// resolving to "", falls back to defaultRelayGameID at Start (the
	// default application moved out of the constructor).
	GameID func() string
	Logger *slog.Logger
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

// NewOutboxRelaySubsystem constructs an OutboxRelaySubsystem. It does not
// allocate or start any runtime resources; call Start to construct the relay
// and launch the drain loop.
func NewOutboxRelaySubsystem(cfg OutboxRelaySubsystemConfig) *OutboxRelaySubsystem {
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

// Prepare constructs the leased relay + the reference consumer — lease/conn
// acquisition and wiring, no work loop (D-13.3 row 6). Idempotent: guarded
// on s.relay so a second Prepare does not rebuild the waker/relay.
// codecov:ignore — exercised by integration and E2E tests.
func (s *OutboxRelaySubsystem) Prepare(ctx context.Context) error {
	if s.relay != nil {
		return nil // already prepared
	}

	pool := s.cfg.DB.Pool()
	store := worldpostgres.NewOutboxStore(pool)
	checkpoint := worldpostgres.NewConsumerCheckpointStore(pool)
	publisher := s.cfg.EventBus.Publisher()

	gameID := s.resolveGameID()

	s.waker = newOutboxWaker(pool)
	s.relay = outbox.NewRelay(outbox.RelayConfig{
		Store:     leaseStoreAdapter{store: store},
		Publisher: publisher,
		GameID:    gameID,
		Waker:     s.waker,
		Logger:    s.cfg.Logger,
	})
	// The reference idempotent consumer is constructed here (it exists and is
	// wired); its live durable-JetStream consume loop + genesis snapshot land in
	// 05-11 (this plan wires the consumer/bootstrap + subsystem plumbing only).
	s.consumer = outbox.NewConsumer(referenceConsumerName, checkpointStoreAdapter{store: checkpoint}, nil, s.cfg.Logger)

	slog.InfoContext(ctx, "outbox relay subsystem prepared", "game_id", gameID)
	return nil
}

// Activate launches the relay drain loop — domain traffic (D-13.3 row 6).
// Idempotent: guarded on s.done, a phase-owned field set only by Activate,
// so a repeated Activate does not launch a second drain goroutine.
// codecov:ignore — exercised by integration and E2E tests.
func (s *OutboxRelaySubsystem) Activate(ctx context.Context) error {
	if s.done != nil {
		return nil // already activated
	}

	runCtx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	done := make(chan struct{})
	s.done = done
	relay := s.relay
	go func(done chan struct{}, relay *outbox.Relay) {
		defer close(done)
		_ = relay.Run(runCtx) //nolint:errcheck // Run returns only the ctx-cancellation reason on Stop
	}(done, relay)

	slog.InfoContext(ctx, "outbox relay subsystem activated")
	return nil
}

// Stop cancels the relay loop, closes the waker, and releases the lease. It
// resets the Prepare guard (relay) and Activate guard (done), plus the
// waker/cancel/consumer fields, to nil so a legitimate retry of
// Prepare/Activate after Stop reacquires a fresh lease and relaunches the
// drain loop rather than short-circuiting on an already-released one
// (WR-01).
// codecov:ignore — exercised by integration and E2E tests.
func (s *OutboxRelaySubsystem) Stop(ctx context.Context) error {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.waker != nil {
		s.waker.Close()
		s.waker = nil
	}
	if s.done != nil {
		select {
		case <-s.done:
		case <-time.After(relayStopTimeout):
		}
		s.done = nil
	}
	if s.relay != nil {
		_ = s.relay.Stop(ctx) //nolint:errcheck // Stop only releases the lease; a release warning is already logged
		s.relay = nil
	}
	s.consumer = nil
	return nil
}

// Consumer exposes the reference consumer (for bootstrap wiring / tests).
func (s *OutboxRelaySubsystem) Consumer() *outbox.Consumer { return s.consumer }

// resolveGameID applies the GameID provider (if any), falling back to
// defaultRelayGameID when the provider is nil or resolves to "" — the same
// default OutboxRelaySubsystemConfig's constructor used to apply eagerly
// (07-09 item 7 moves the default application to Start-time resolution).
func (s *OutboxRelaySubsystem) resolveGameID() string {
	gameID := ""
	if s.cfg.GameID != nil {
		gameID = s.cfg.GameID()
	}
	if gameID == "" {
		gameID = defaultRelayGameID
	}
	return gameID
}

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

// listenConn is the slice of a pinned pool connection the waker uses. It is
// an interface so the Close/Wait ordering contract can be unit-tested against
// a fake instead of a live Postgres.
type listenConn interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	WaitForNotification(ctx context.Context) (*pgconn.Notification, error)
	Release()
}

// poolListenConn adapts *pgxpool.Conn to listenConn: WaitForNotification lives
// on the underlying *pgx.Conn, not on the pool wrapper.
type poolListenConn struct{ conn *pgxpool.Conn }

func (c poolListenConn) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return c.conn.Exec(ctx, sql, args...) //nolint:wrapcheck // adapter pass-through
}

func (c poolListenConn) WaitForNotification(ctx context.Context) (*pgconn.Notification, error) {
	return c.conn.Conn().WaitForNotification(ctx) //nolint:wrapcheck // adapter pass-through
}

func (c poolListenConn) Release() { c.conn.Release() }

// outboxWaker is the relay's dedicated LISTEN connection. It holds one pinned
// pool connection LISTENing on the outbox NOTIFY channel and blocks in Wait until
// a transaction-side pg_notify arrives or the context is done (the relay wraps
// Wait with its sweep interval, so a missed NOTIFY still sweeps).
//
// Close MUST NOT release the pinned connection while a Wait is still blocked in
// WaitForNotification on it: pgxpool.Conn.Release hands the connection back to
// the pool (or destroys it) while the other goroutine is still reading from it,
// which is a data race inside pgconn (surfaced by the nightly -race
// integration lane). Close therefore cancels closeCtx so the in-flight Wait
// unblocks, waits for it to return, and only then releases.
type outboxWaker struct {
	acquire func(ctx context.Context) (listenConn, error)
	mu      sync.Mutex
	conn    listenConn
	// closeCtx is cancelled by Close so an in-flight Wait unblocks; inflight
	// counts Wait calls currently holding conn so Close can wait for them.
	closeCtx    context.Context //nolint:containedctx // lifecycle ctx, not request ctx
	closeCancel context.CancelFunc
	inflight    sync.WaitGroup
}

func newOutboxWaker(pool *pgxpool.Pool) *outboxWaker {
	return newOutboxWakerWithAcquire(func(ctx context.Context) (listenConn, error) {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			return nil, err //nolint:wrapcheck // transient acquire failure; relay falls back to sweep
		}
		return poolListenConn{conn: conn}, nil
	})
}

// newOutboxWakerWithAcquire is the injection seam for tests that need a fake
// LISTEN connection.
func newOutboxWakerWithAcquire(acquire func(ctx context.Context) (listenConn, error)) *outboxWaker {
	closeCtx, closeCancel := context.WithCancel(context.Background())
	return &outboxWaker{acquire: acquire, closeCtx: closeCtx, closeCancel: closeCancel}
}

// Wait blocks until a NOTIFY on the outbox channel or ctx is done. Once Close
// has run no NOTIFY can ever arrive, so Wait degrades to blocking on ctx alone:
// the relay wraps Wait in its sweep-interval ctx, so a closed waker makes the
// relay sweep-only rather than spinning Drain→Wait→Drain at CPU speed (which an
// instant return here would cause for any caller still running).
func (w *outboxWaker) Wait(ctx context.Context) error {
	w.mu.Lock()
	if w.closeCtx.Err() != nil {
		w.mu.Unlock()
		<-ctx.Done()
		return ctx.Err() //nolint:wrapcheck // ctx-deadline signal; relay ignores it and re-drains
	}
	if w.conn == nil {
		conn, err := w.acquire(ctx)
		if err != nil {
			w.mu.Unlock()
			return err
		}
		if _, err := conn.Exec(ctx, "LISTEN "+worldpostgres.OutboxNotifyChannel); err != nil {
			conn.Release()
			w.mu.Unlock()
			return err //nolint:wrapcheck // transient listen failure; relay falls back to sweep
		}
		w.conn = conn
	}
	conn := w.conn
	// Add under mu, where closeCtx is known un-cancelled, so Close's cancel
	// (also under mu) and its inflight.Wait happen-after this Add.
	w.inflight.Add(1)
	w.mu.Unlock()
	defer w.inflight.Done()

	// Unblock on Close as well as on the caller's ctx.
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(w.closeCtx, cancel)
	defer stop()

	_, err := conn.WaitForNotification(waitCtx)
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

// Close unblocks any in-flight Wait, waits for it to return, then releases the
// pinned LISTEN connection. Idempotent.
func (w *outboxWaker) Close() {
	w.mu.Lock()
	w.closeCancel()
	w.mu.Unlock()
	w.inflight.Wait()

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.conn != nil {
		w.conn.Release()
		w.conn = nil
	}
}
