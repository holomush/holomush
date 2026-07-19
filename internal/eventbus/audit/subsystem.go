// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package audit projects events from JetStream into the PostgreSQL
// events_audit table for forever-archive and historical query support.
//
// Phase A invariant: no plugins yet claim subjects, so the projection
// drains every message published to events.> into events_audit. The
// consumer is durable (resumes across restarts) with AckExplicitPolicy
// plus INSERT ... ON CONFLICT (id) DO NOTHING at the SQL level. Combined
// with Nats-Msg-Id dedup at the JetStream level, this provides
// effectively-exactly-once semantics within the dedup window.
package audit

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/samber/oops"

	retaudit "github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/internal/lifecycle"
)

// Default configuration values for the audit projection.
const (
	// DefaultConsumerName is the durable consumer name registered on the
	// EVENTS stream. Durable => resumes from last-acked seq on restart.
	DefaultConsumerName = "host_audit_projection"

	// DefaultBatchSize bounds how many unacked messages the consumer will
	// hold in-flight at a time.
	DefaultBatchSize = 64

	// DefaultMaxAckPending mirrors BatchSize; the consumer will not deliver
	// more than this many messages without an ack.
	DefaultMaxAckPending = 64

	// DefaultAckWait is how long the server waits for ack before redelivery.
	// Set short so a handler crash produces a visible redelivery within a
	// test timeout.
	DefaultAckWait = 5 * time.Second

	// DefaultDrainTimeout is how long Stop waits for the Consume loop to
	// finish processing in-flight messages before returning.
	DefaultDrainTimeout = 2 * time.Second

	// AwaitPollInterval is how often AwaitDrained polls ConsumerInfo when
	// waiting for the projection to catch up. Implemented via time.After
	// because time.Sleep is banned in the eventbus package tree.
	AwaitPollInterval = 20 * time.Millisecond

	// DefaultMaxDeliver caps consumer redelivery attempts for poison
	// messages (e.g., missing/malformed required headers). Without a
	// cap the default is unlimited, and a handful of permanently-bad
	// events would permanently occupy MaxAckPending slots. On the final
	// attempt the projection captures the message to the bounded
	// EVENTS_AUDIT_DLQ stream (see dlq.go) rather than dropping it.
	DefaultMaxDeliver = 10

	// DefaultRetainWindow is how long events_audit history is retained
	// before a partition fully older than the window is detached and
	// dropped (OPS-02 / D-02). Mirrors the ABAC RetentionConfig 90d denial
	// default. Operator-overridable via event_bus.audit.retain_window.
	DefaultRetainWindow = 90 * 24 * time.Hour

	// DefaultPurgeInterval is how often the periodic RetentionWorker runs its
	// Detach/Drop cycle. Also the delay before the FIRST destructive cycle
	// (WithSkipFirstRun defers it one tick past boot).
	DefaultPurgeInterval = 24 * time.Hour

	// auditForwardMonths is how many months forward EnsurePartitions covers
	// on the synchronous boot gate and each periodic cycle (matches the ABAC
	// worker's RunOnce horizon).
	auditForwardMonths = 3
)

// Config controls the audit projection worker.
type Config struct {
	// ConsumerName is the durable consumer name on the EVENTS stream.
	// Must remain stable across restarts for the consumer to resume.
	ConsumerName string

	// BatchSize bounds in-flight message count.
	BatchSize int

	// AckWait is the server-side redelivery timeout.
	AckWait time.Duration

	// MaxAckPending caps unacked messages.
	MaxAckPending int

	// MaxDeliver caps redelivery attempts for poison messages. Zero
	// disables the cap (unlimited redelivery — not recommended for
	// production). Defaults to DefaultMaxDeliver via Defaults.
	MaxDeliver int

	// Owners is the subject-ownership map built from plugin manifests.
	// The projection ack-and-skips messages whose subject resolves to a
	// plugin owner — per-plugin consumers (registered in F5) project
	// those into plugin-owned audit schemas independently.
	//
	// Nil means "no plugins declared ownership; host owns everything",
	// preserving Phase A behavior.
	Owners *OwnerMap

	// DLQ bounds and locates the dead-letter stream that captures
	// messages exhausting MaxDeliver (CLUSTER-04, D-09/D-12). Zero-valued
	// fields resolve via DLQConfig.Defaults().
	DLQ DLQConfig

	// RetainWindow is how long events_audit history is retained before an
	// entirely-older partition is detached and dropped (OPS-02 / D-02).
	// Zero resolves to DefaultRetainWindow. A distinct field from DLQ.MaxAge
	// (which bounds the dead-letter stream, not audit history).
	RetainWindow time.Duration

	// PurgeInterval is how often the periodic RetentionWorker runs its
	// Detach/Drop cycle. Zero resolves to DefaultPurgeInterval.
	PurgeInterval time.Duration
}

// Defaults fills any zero-valued fields with defaults.
func (c Config) Defaults() Config {
	if c.ConsumerName == "" {
		c.ConsumerName = DefaultConsumerName
	}
	if c.BatchSize == 0 {
		c.BatchSize = DefaultBatchSize
	}
	if c.AckWait == 0 {
		c.AckWait = DefaultAckWait
	}
	if c.MaxAckPending == 0 {
		c.MaxAckPending = DefaultMaxAckPending
	}
	if c.MaxDeliver == 0 {
		c.MaxDeliver = DefaultMaxDeliver
	}
	if c.RetainWindow == 0 {
		c.RetainWindow = DefaultRetainWindow
	}
	if c.PurgeInterval == 0 {
		c.PurgeInterval = DefaultPurgeInterval
	}
	c.DLQ = c.DLQ.Defaults()
	return c
}

// Validate rejects a retention config that would make the RetentionWorker
// destructive-by-accident (round-3 finding 3). Defaults() only fills ZERO
// values, so a NEGATIVE survives: a negative RetainWindow makes the detach
// cutoff future-facing (now.Add(-RetainWindow)) and would detach EVERY
// partition; a non-positive PurgeInterval panics time.NewTicker. This runs in
// Subsystem.Start before the projection accepts traffic (and is unit-tested).
func (c Config) Validate() error {
	if c.RetainWindow <= 0 {
		return oops.Code("AUDIT_CONFIG_INVALID").
			With("retain_window", c.RetainWindow).
			Errorf("audit retain_window must be positive")
	}
	if c.PurgeInterval <= 0 {
		return oops.Code("AUDIT_CONFIG_INVALID").
			With("purge_interval", c.PurgeInterval).
			Errorf("audit purge_interval must be positive")
	}
	return nil
}

// retentionConfig maps the audit Config onto the internal/audit
// RetentionConfig consumed by the RetentionWorker. RetainAllows is unused by
// events_audit (PurgeExpiredAllows is a no-op) but set for completeness.
func (c Config) retentionConfig() retaudit.RetentionConfig {
	return retaudit.RetentionConfig{
		RetainDenials: c.RetainWindow,
		RetainAllows:  c.RetainWindow,
		PurgeInterval: c.PurgeInterval,
	}
}

// JSProvider yields the JetStream context at Start time. Constructed
// before the eventbus subsystem has started, so we defer the accessor
// call rather than capturing the value eagerly (which would be nil).
type JSProvider interface {
	JS() jetstream.JetStream
}

// PoolProvider yields the pgxpool at Start time. See JSProvider for
// the rationale behind the indirection.
type PoolProvider interface {
	Pool() *pgxpool.Pool
}

// Subsystem manages the host audit projection worker lifecycle.
//
// Prepare runs the synchronous boot gate (partition Backfill +
// EnsurePartitions) and constructs (or re-attaches to) the durable
// consumer, storing it on preparedProjection. Activate spawns the Consume
// loop from that prepared projection. Stop cancels the loop and waits up to
// DefaultDrainTimeout for in-flight messages to finish processing — or, if
// Prepare succeeded but Activate never ran, clears the in-memory prepared
// aggregate without draining anything (D-13.2 row 10).
//
// mu guards cancel/worker/preparedProjection/partitionManager so a
// concurrent Stop (e.g., orchestrator shutdown racing with a signal
// handler) cannot observe partial state while the first Stop is still
// inside drain().
type Subsystem struct {
	mu       sync.Mutex
	jsProv   JSProvider
	poolProv PoolProvider
	cfg      Config
	cancel   context.CancelFunc
	worker   *projection
	// preparedProjection is set by Prepare (the newProjection result) and
	// consumed by Activate, which calls p.start(workerCtx) and assigns the
	// result to worker. Prepare guards on this field — NOT on worker, which
	// is assigned only after p.start, now an Activate-owned step (D-13.2 row
	// 10, round 7 BLOCKER: the old s.worker != nil guard cannot guard
	// Prepare after the split).
	preparedProjection *projection
	// partitionManager is set by Prepare and consumed by Activate to build
	// the periodic RetentionWorker.
	partitionManager *EventsAuditPartitionManager
	pluginMgr        *PluginConsumerManager
	retentionWorker  *retaudit.RetentionWorker
	// lateInit is called once from Prepare (before newProjection) so the
	// owner map and per-plugin consumer manager can be built from plugin
	// manifests that are only available after SubsystemPlugins has
	// prepared. The provider MUST NOT call back into Subsystem setters
	// (would deadlock s.mu). A nil lateInit keeps Phase A behavior.
	lateInit func() (*OwnerMap, *PluginConsumerManager)
}

// NewSubsystem constructs an audit projection subsystem. jsProv and
// poolProv are resolved at Start time (not construction time) so that
// callers can wire this subsystem before its dependencies have started.
// The orchestrator's DependsOn ordering guarantees that DB and EventBus
// are Start()-ed before this subsystem's Start runs.
func NewSubsystem(jsProv JSProvider, poolProv PoolProvider, cfg Config) *Subsystem {
	return &Subsystem{jsProv: jsProv, poolProv: poolProv, cfg: cfg.Defaults()}
}

// ID returns lifecycle.SubsystemAuditProjection.
func (s *Subsystem) ID() lifecycle.SubsystemID { return lifecycle.SubsystemAuditProjection }

// SetLateInitProvider registers a single late-bound provider that
// returns both the OwnerMap (for host projection subject-exclusion) and
// the PluginConsumerManager (for dispatching plugin-owned messages to
// the plugin's PluginAuditService). The provider is evaluated once at
// Start, after the plugin subsystem has started per DependsOn.
//
// Either return value may be nil: a nil OwnerMap preserves the Phase A
// "host owns everything" behavior, and a nil manager disables per-
// plugin dispatch. The provider MUST NOT call back into Subsystem
// setters — Start evaluates it under s.mu.
func (s *Subsystem) SetLateInitProvider(p func() (*OwnerMap, *PluginConsumerManager)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lateInit = p
}

// DependsOn returns the subsystems that must be started first:
// database (events_audit target table), eventbus (JetStream source),
// and plugins (needed so per-plugin audit consumers can resolve their
// PluginAuditService clients via the plugin manager's registered hosts).
func (s *Subsystem) DependsOn() []lifecycle.SubsystemID {
	return []lifecycle.SubsystemID{
		lifecycle.SubsystemDatabase,
		lifecycle.SubsystemEventBus,
		lifecycle.SubsystemPlugins,
	}
}

// Prepare runs the synchronous boot gate and constructs (or re-attaches to)
// the durable consumer — everything the code's own prior comment already
// named as "ALL BEFORE the projection starts accepting traffic" (D-13.3 row
// 10). It does NOT spawn the Consume loop; that is Activate's job.
//
// Prepare is idempotent: guarded on s.preparedProjection, a phase-owned
// field this method itself sets (the old s.worker != nil guard cannot guard
// Prepare — worker is assigned only after p.start, which now lives in
// Activate).
//
// A Prepare failure after lateInit restores s.cfg.Owners/s.pluginMgr to
// their pre-lateInit values (D-13.2 row 10, round 8): lateInit writes both
// BEFORE the fallible newProjection call, so without this restoration a
// retried Prepare whose lateInit provider returns nil would silently keep
// stale ownership/consumer wiring from the failed attempt.
func (s *Subsystem) Prepare(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.preparedProjection != nil {
		return nil // already prepared
	}
	js := s.jsProv.JS()
	if js == nil {
		return oops.Code("AUDIT_DEP_NOT_STARTED").Errorf("eventbus JetStream not available at audit.Prepare")
	}
	pool := s.poolProv.Pool()
	if pool == nil {
		return oops.Code("AUDIT_DEP_NOT_STARTED").Errorf("database pool not available at audit.Prepare")
	}
	// Resolve the DLQ subject provider, if any, before config validation /
	// DLQ setup — it wins over the Defaults()-substituted Subject (07-09
	// item 7). The AuditProjection -> Database edge (already declared)
	// guarantees the gameID this provider reads is resolvable by now.
	if s.cfg.DLQ.SubjectProvider != nil {
		if subject := s.cfg.DLQ.SubjectProvider(); subject != "" {
			s.cfg.DLQ.Subject = subject
		}
	}
	// Reject a destructive-by-accident retention config (negative window /
	// non-positive interval) before anything prepares (round-3 finding 3).
	if err := s.cfg.Validate(); err != nil {
		return err
	}
	// SYNCHRONOUS BOOT GATE (findings 9 & 10): construct the events_audit
	// PartitionManager, re-home the legacy events_audit_unpartitioned history
	// (Backfill), and ensure partition coverage — ALL BEFORE the projection
	// starts accepting traffic. This is Backfill + EnsurePartitions ONLY, never
	// a full RunOnce (no Detach/Drop on boot — a red first-boot must not prune).
	// A gate failure returns from Prepare before any projection exists, so
	// there is nothing to roll back and the rename/backfill cannot race a
	// DLQ replay.
	partitionMgr := NewEventsAuditPartitionManager(pool, s.cfg.RetainWindow)
	if err := partitionMgr.Backfill(ctx); err != nil {
		return oops.Code("AUDIT_BACKFILL_BOOT_GATE_FAILED").Wrap(err)
	}
	if err := partitionMgr.EnsurePartitions(ctx, auditForwardMonths); err != nil {
		return oops.Code("AUDIT_ENSURE_BOOT_GATE_FAILED").Wrap(err)
	}
	// Late-bind the owner map + per-plugin consumer manager if a
	// provider was installed (F5): plugin manifests are only available
	// after the plugin subsystem has prepared, which is guaranteed by the
	// DependsOn on SubsystemPlugins. Capture the pre-lateInit values first
	// so a Prepare failure below can restore them.
	prevOwners := s.cfg.Owners
	prevPluginMgr := s.pluginMgr
	if s.lateInit != nil {
		owners, pcm := s.lateInit()
		if owners != nil {
			s.cfg.Owners = owners
		}
		if pcm != nil {
			s.pluginMgr = pcm
		}
	}
	p, err := newProjection(ctx, js, pool, s.cfg)
	if err != nil {
		s.cfg.Owners = prevOwners
		s.pluginMgr = prevPluginMgr
		return oops.Code("AUDIT_PROJECTION_START_FAILED").Wrap(err)
	}

	s.preparedProjection = p
	s.partitionManager = partitionMgr
	return nil
}

// Activate spawns the Consume loop from the projection Prepare constructed,
// starts per-plugin audit consumers, and launches the periodic retention
// worker (D-13.3 row 10). Any Consume error from the underlying JetStream
// client is propagated so the orchestrator does not observe a silently-dead
// worker as "activated successfully" — the spec's operability contract
// (§6).
//
// Activate is idempotent: guarded on s.worker. It fails closed with a typed
// error if Prepare never ran (s.preparedProjection == nil).
//
// The worker's context is deliberately parented on context.Background(), not
// Activate's ctx — the worker must outlive the boot ctx.
func (s *Subsystem) Activate(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.worker != nil {
		return nil // already activated
	}
	if s.preparedProjection == nil {
		return oops.Code("AUDIT_NOT_PREPARED").Errorf("audit.Activate called before Prepare")
	}
	p := s.preparedProjection

	workerCtx, cancel := context.WithCancel(context.Background())
	if err := p.start(workerCtx); err != nil {
		cancel()
		return err
	}
	s.worker = p
	s.cancel = cancel
	// F5: start per-plugin audit consumers. Failure here is treated as a
	// hard Activate failure because a misconfigured plugin consumer would
	// leave plugin-owned subjects without any audit sink (the host
	// projection skips them). The error path rolls back the host
	// projection we just started so lifecycle is all-or-nothing.
	if s.pluginMgr != nil {
		if err := s.pluginMgr.Start(workerCtx); err != nil {
			cancel()
			// Bound the rollback drain by DefaultDrainTimeout so a slow
			// host projection cannot block Activate() indefinitely on the
			// plugin-manager failure path. Matches the normal Stop()
			// drain contract.
			rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), DefaultDrainTimeout)
			defer rollbackCancel()
			_ = p.drain(rollbackCtx) //nolint:errcheck // best-effort
			s.worker = nil
			s.cancel = nil
			return err
		}
	}
	// Periodic retention worker (OPS-02): runs the Detach/Drop cycle on
	// PurgeInterval. WithSkipFirstRun defers the FIRST destructive cycle to the
	// first tick (round-4 MEDIUM) so a subsystem that fails after the boot gate
	// cannot prune on a red deploy — the boot gate above ran only the
	// non-destructive Backfill + EnsurePartitions. Consequently the first prune
	// is ~one PurgeInterval (24h default) after boot; operators must NOT expect
	// an immediate detach on deploy (round-5 LOW). Activate always returns nil
	// for the worker's own Start (it spawns the loop); later RunOnce failures
	// are logged non-fatally.
	worker := retaudit.NewRetentionWorker(s.cfg.retentionConfig(), s.partitionManager, retaudit.WithSkipFirstRun())
	_ = worker.Start(workerCtx) //nolint:errcheck // Start always returns nil
	s.retentionWorker = worker
	return nil
}

// Stop is the single teardown path for all three states this subsystem can
// be in: never prepared, prepared-but-never-activated, and activated
// (D-13.1). Idempotent; safe to call multiple times even concurrently.
//
// Prepared-only: clears the in-memory prepared aggregate
// (preparedProjection/partitionManager). The durable JetStream consumer
// newProjection created is intentionally RETAINED server-side — drain is a
// no-op before Consume exists, and CreateOrUpdateConsumer is idempotent, so
// a retried Prepare re-attaches to the same durable rather than duplicating
// it (D-13.2 row 10, round 9).
//
// Activated: cancels the Consume loop and waits for in-flight messages to
// drain (bounded by DefaultDrainTimeout), same as before the split. The
// worker/prepared references are cleared AFTER drain returns so a second
// Stop racing with the first cannot observe nil state and report clean
// shutdown while the first drain is still pending.
func (s *Subsystem) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.worker == nil {
		if s.preparedProjection != nil {
			// Prepared but never activated: nothing running to drain.
			s.preparedProjection = nil
			s.partitionManager = nil
		}
		return nil
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	// Stop the periodic retention worker (bounded; waits for any in-flight
	// RunOnce to finish) before draining the projection.
	if s.retentionWorker != nil {
		s.retentionWorker.Stop()
		s.retentionWorker = nil
	}
	// Drain per-plugin consumers before the host projection so a plugin
	// cannot keep dispatching while the host projection is tearing down.
	var pluginErr error
	if s.pluginMgr != nil {
		pluginErr = s.pluginMgr.Stop(ctx)
	}
	w := s.worker
	err := w.drain(ctx)
	s.worker = nil
	s.preparedProjection = nil
	s.partitionManager = nil
	if err != nil {
		return err
	}
	return pluginErr
}

// AwaitDrained is a test-only helper that blocks until the consumer has
// no pending messages and no acks outstanding, or until timeout.
//
// Uses time.After instead of time.Sleep because the forbidigo linter
// bans time.Sleep across the entire eventbus package tree.
func (s *Subsystem) AwaitDrained(t AwaitT, timeout time.Duration) {
	s.mu.Lock()
	w := s.worker
	s.mu.Unlock()
	if w == nil {
		return
	}
	w.awaitDrained(t, timeout)
}

// AwaitT is the minimal testing.T subset AwaitDrained depends on.
// Extracting it lets non-_test files reference this helper without
// pulling in testing.T.
type AwaitT interface {
	Helper()
	Fatalf(format string, args ...any)
}
