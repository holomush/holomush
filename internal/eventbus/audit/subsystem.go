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
	c.DLQ = c.DLQ.Defaults()
	return c
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
// Start creates (or updates) the durable consumer and spawns the
// Consume loop. Stop cancels the loop and waits up to DefaultDrainTimeout
// for in-flight messages to finish processing.
//
// mu guards cancel and worker so a concurrent Stop (e.g., orchestrator
// shutdown racing with a signal handler) cannot observe worker = nil
// while the first Stop is still inside drain().
type Subsystem struct {
	mu        sync.Mutex
	jsProv    JSProvider
	poolProv  PoolProvider
	cfg       Config
	cancel    context.CancelFunc
	worker    *projection
	pluginMgr *PluginConsumerManager
	// lateInit is called once from Start (before newProjection) so the
	// owner map and per-plugin consumer manager can be built from plugin
	// manifests that are only available after SubsystemPlugins has
	// started. The provider MUST NOT call back into Subsystem setters
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

// Start creates the durable consumer and attaches the Consume callback.
// Any Consume error from the underlying JetStream client is propagated
// so the orchestrator does not observe a silently-dead worker as
// "started successfully" — the spec's operability contract (§6).
func (s *Subsystem) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.worker != nil {
		return nil // already started
	}
	js := s.jsProv.JS()
	if js == nil {
		return oops.Code("AUDIT_DEP_NOT_STARTED").Errorf("eventbus JetStream not available at audit.Start")
	}
	pool := s.poolProv.Pool()
	if pool == nil {
		return oops.Code("AUDIT_DEP_NOT_STARTED").Errorf("database pool not available at audit.Start")
	}
	// Late-bind the owner map + per-plugin consumer manager if a
	// provider was installed (F5): plugin manifests are only available
	// after the plugin subsystem has started, which is guaranteed by the
	// DependsOn on SubsystemPlugins.
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
		return oops.Code("AUDIT_PROJECTION_START_FAILED").Wrap(err)
	}
	workerCtx, cancel := context.WithCancel(context.Background())
	if err := p.start(workerCtx); err != nil {
		cancel()
		return err
	}
	s.worker = p
	s.cancel = cancel
	// F5: start per-plugin audit consumers. Failure here is treated as a
	// hard Start failure because a misconfigured plugin consumer would
	// leave plugin-owned subjects without any audit sink (the host
	// projection skips them). The error path rolls back the host
	// projection we just started so lifecycle is all-or-nothing.
	if s.pluginMgr != nil {
		if err := s.pluginMgr.Start(workerCtx); err != nil {
			cancel()
			// Bound the rollback drain by DefaultDrainTimeout so a slow
			// host projection cannot block Start() indefinitely on the
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
	return nil
}

// Stop cancels the Consume loop and waits for in-flight messages to
// drain (bounded by DefaultDrainTimeout). Idempotent; safe to call
// multiple times even concurrently.
//
// The worker reference is cleared AFTER drain returns so a second Stop
// racing with the first cannot observe worker=nil and report clean
// shutdown while the first drain is still pending.
func (s *Subsystem) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.worker == nil {
		return nil
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
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
