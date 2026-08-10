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

	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/consumer"
	"github.com/holomush/holomush/internal/lifecycle"
)

// barrierTimeout bounds how long the shutdown goroutine waits on the consume
// context's Closed() barrier — that is, for the last in-flight process() to
// return. Mirrors the outbox relay subsystem's budget.
const barrierTimeout = 5 * time.Second

// stopTimeout bounds how long Stop waits to JOIN that goroutine. It is a LAST
// RESORT, not the mechanism that reports a wedged fanout.
//
// It MUST be strictly larger than barrierTimeout so the join can observe the
// barrier resolving rather than expiring alongside it. The consequence of that
// ordering, though, is that this arm is nearly unreachable: the goroutine's
// only blocking wait is the barrier itself, so s.done closes within
// barrierTimeout + ε on every path, wedged handler or not. What is left for
// this arm to catch is the residual case where the goroutine never scheduled at
// all. It therefore MUST NOT carry the report for the common wedged-fanout
// case — that is s.barrierOK's job, which is signalled by the arm that can
// actually observe it.
const stopTimeout = barrierTimeout + time.Second

// Config configures the retirement reactor subsystem.
//
// Every effect surface is a consumer-defined interface declared in reactor.go,
// and every live value the fanout needs at handle time arrives through a
// PROVIDER rather than eagerly at construction: cmd/holomush builds this
// subsystem before the database, event bus, world and bootstrap subsystems have
// started, so an eagerly-resolved handle would be nil (or, for
// StartLocationID, a panic).
type Config struct {
	Logger *slog.Logger

	// JetStream yields the JetStream handle at Prepare time.
	JetStream JetStreamProvider
	// Sessions is the session-teardown surface.
	Sessions SessionEnder
	// Presence is the leave / session_ended fanout surface.
	Presence PresenceEmitter
	// World is the ABAC-gated world surface (status read + move).
	World WorldSurface
	// Jobs is the background-job liveness registry. Optional: a nil registry
	// means no job attributes are ever stamped, so every job-gating seed
	// default-denies — the correct fail-closed state, not a bypass.
	Jobs JobRegistry
	// StartLocationID resolves the move destination at handle time. It MUST
	// NOT be called before the bootstrap subsystem's Prepare (it panics), which
	// is why DependsOn declares that edge.
	StartLocationID func() ulid.ULID

	// ConsumerName overrides DefaultConsumerName. Tests only — changing it in
	// production strands the existing durable consumer.
	ConsumerName string
	// AckWait, MaxAckPending and MaxDeliver mirror the audit projector's
	// defaults when zero.
	AckWait       time.Duration
	MaxAckPending int
	MaxDeliver    int
}

// Defaults fills any zero-valued consumer tuning field.
func (c Config) Defaults() Config {
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.ConsumerName == "" {
		c.ConsumerName = DefaultConsumerName
	}
	if c.AckWait == 0 {
		c.AckWait = defaultAckWait
	}
	if c.MaxAckPending == 0 {
		c.MaxAckPending = defaultMaxAckPending
	}
	if c.MaxDeliver == 0 {
		c.MaxDeliver = defaultMaxDeliver
	}
	return c
}

// JetStreamProvider yields the JetStream context at Prepare time. Same shape
// (and same rationale) as audit.JSProvider: the eventbus subsystem has not
// started when this subsystem is constructed, so the accessor call is deferred.
type JetStreamProvider interface {
	JS() jetstream.JetStream
}

// Consumer tuning defaults, mirroring the audit projector so the two durable
// consumers on the EVENTS stream age and redeliver alike.
const (
	defaultAckWait       = 5 * time.Second
	defaultMaxAckPending = 64
	// defaultMaxDeliver caps redelivery for a message the handler can never
	// complete. The handler acks every permanently-unhandleable message
	// itself, so reaching this cap means a genuinely stuck effect surface.
	defaultMaxDeliver = 10
)

// consumeStarter is the one jetstream.Consumer method the subsystem uses.
// Declared narrowly so a package test can substitute a fake without
// implementing the eight-method jetstream.Consumer surface; *the* production
// value is whatever consumer.CreateWithRetry returns.
type consumeStarter interface {
	Consume(handler jetstream.MessageHandler, opts ...jetstream.PullConsumeOpt) (jetstream.ConsumeContext, error)
}

// Subsystem is the lifecycle.Subsystem that owns the retirement fanout.
type Subsystem struct {
	cfg     Config
	reactor *reactor
	// cons is the durable consumer AND the Prepare guard (the outbox relay
	// guards on s.relay the same way). Nil until Prepare has run.
	cons consumeStarter
	// cc is the live consume context, stopped by Stop.
	cc jetstream.ConsumeContext
	// cancel cancels the context every handler effect runs under.
	cancel context.CancelFunc
	// done guards Activate and is closed when the consume loop exits. Nil
	// until Activate has run.
	done chan struct{}
	// barrierOK is closed by the shutdown goroutine ONLY on the arm where
	// cc.Closed() resolved — that is, only when no handler is in flight. It is
	// what lets Stop distinguish "the goroutine exited" (which s.done records,
	// and which is true on BOTH arms) from "the handler was joined". Without it
	// Stop can only infer the barrier's outcome from its own join, and the join
	// succeeds either way.
	barrierOK chan struct{}
	// barrier overrides barrierTimeout. Zero in production; package tests
	// shorten it so the abandoned-barrier path is reachable in a unit test
	// rather than only after five real seconds.
	barrier time.Duration

	// createConsumer is the consumer-create seam. Nil in production, where
	// Prepare builds the real durable consumer through
	// consumer.CreateWithRetry; package tests substitute a fake so the
	// lifecycle contract can be exercised without a live JetStream.
	createConsumer func(ctx context.Context) (consumeStarter, error)
}

// NewSubsystem constructs the retirement reactor. It allocates nothing and
// touches no live resources — cmd/holomush's real-constructor graph tests
// build every production subsystem this way and never call Prepare.
func NewSubsystem(cfg Config) *Subsystem {
	return &Subsystem{cfg: cfg.Defaults()}
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

// Prepare builds the handler and creates the durable JetStream consumer —
// acquisition, no work loop, per the Prepare/Activate contract's
// process-internal-substrate carve-out. Idempotent behind the s.cons guard.
func (s *Subsystem) Prepare(ctx context.Context) error {
	if s.cons != nil {
		return nil // already prepared
	}
	r, err := newReactor(s.cfg)
	if err != nil {
		return err
	}
	create := s.createConsumer
	if create == nil {
		create = s.createDurableConsumer
	}
	cons, err := create(ctx)
	if err != nil {
		return err
	}
	s.reactor = r
	s.cons = cons
	slog.InfoContext(ctx, "retirement reactor subsystem prepared",
		"consumer", s.cfg.ConsumerName, "filter_subject", ConsumerFilterSubject)
	return nil
}

// createDurableConsumer is the production consumer-create path.
//
// The create call goes through consumer.CreateWithRetry (the shared D-46
// helper) rather than a fresh backoff loop, so this consumer absorbs the same
// JetStream warmup window the audit projector does. The helper codes nothing,
// so the RETIREMENT_CONSUMER_CREATE_FAILED wrap below is the whole error
// surface.
func (s *Subsystem) createDurableConsumer(ctx context.Context) (consumeStarter, error) {
	if s.cfg.JetStream == nil {
		return nil, oops.Code("RETIREMENT_REACTOR_UNWIRED").Errorf("jetstream provider is required")
	}
	js := s.cfg.JetStream.JS()
	if js == nil {
		return nil, oops.Code("RETIREMENT_REACTOR_UNWIRED").Errorf("jetstream handle is not available")
	}
	cons, err := consumer.CreateWithRetry(ctx, func(ctx context.Context) (jetstream.Consumer, error) {
		return js.CreateOrUpdateConsumer(ctx, eventbus.StreamName, jetstream.ConsumerConfig{
			Durable:       s.cfg.ConsumerName,
			Name:          s.cfg.ConsumerName,
			FilterSubject: ConsumerFilterSubject,
			AckPolicy:     jetstream.AckExplicitPolicy,
			AckWait:       s.cfg.AckWait,
			MaxAckPending: s.cfg.MaxAckPending,
			MaxDeliver:    s.cfg.MaxDeliver,
		})
	})
	if err != nil {
		return nil, oops.Code("RETIREMENT_CONSUMER_CREATE_FAILED").
			With("stream", eventbus.StreamName).
			With("consumer", s.cfg.ConsumerName).
			With("nats_err", err.Error()).
			Wrap(err)
	}
	return cons, nil
}

// Activate starts the consume loop — domain traffic. Idempotent behind the
// done-channel guard.
//
// It REFUSES to run without a prepared consumer rather than no-op'ing. The
// orchestrator always runs the whole Prepare sweep before the whole Activate
// sweep, so this never fires in production; if it ever did, a silent success
// would leave retirement permanently ineffective with nothing to point at.
func (s *Subsystem) Activate(ctx context.Context) error {
	if s.done != nil {
		return nil // already activated
	}
	if s.cons == nil {
		return oops.Code("RETIREMENT_REACTOR_NOT_PREPARED").
			Errorf("activate called before prepare; no durable consumer exists")
	}

	// Declare the job LIVE before a single message can be delivered. Authority
	// is tied to liveness (D-49): until this returns, attribute.JobProvider
	// stamps nothing for "job:retirement" and every world write the handler
	// attempts default-denies. Registering after Consume would open a window
	// where the first delivered retirement is denied for no reason an operator
	// could diagnose.
	if s.cfg.Jobs != nil {
		if err := s.cfg.Jobs.Register(JobName, jobWrites); err != nil {
			// Wrapped for context only, NOT re-coded: the registry already
			// codes its rejections (JOB_REGISTRATION_INVALID), and stacking a
			// second code on top would push the diagnostic one deeper into the
			// chain for no gain.
			return oops.With("job", JobName).Wrap(err)
		}
	}

	// The handler runs on JetStream's own goroutines with no context of its
	// own, so the effect context is stored on the reactor BEFORE Consume
	// registers the callback — assigning it after registration is a data race
	// (the audit projector carries the same note).
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.cancel = cancel
	s.reactor.ctx = runCtx

	cc, err := s.cons.Consume(s.reactor.handle)
	if err != nil {
		cancel()
		s.cancel = nil
		s.reactor.ctx = nil
		s.unregisterJob()
		return oops.Code("RETIREMENT_CONSUME_FAILED").
			With("consumer", s.cfg.ConsumerName).
			Wrap(err)
	}
	s.cc = cc

	done := make(chan struct{})
	s.done = done
	barrierOK := make(chan struct{})
	s.barrierOK = barrierOK
	budget := s.barrierBudget()
	go func() {
		defer close(done)
		<-runCtx.Done()
		cc.Stop()
		// Stop() IS NOT A BARRIER. In nats.go v1.52.0 it only does
		// closed.CompareAndSwap(0,1) + close(s.done) and returns
		// (jetstream/pull.go:768); the handler is invoked from the NATS
		// subscription's own dispatch goroutine, which Stop never joins. So a
		// process() can still be mid-fanout after Stop returns — and Stop()
		// then retracts the job's liveness, at which point a still-running
		// MoveCharacter loses its ABAC attributes and classifyWorldError would
		// ACK the resulting deny as terminal, permanently abandoning a
		// half-applied fanout (session already deleted, character never moved).
		//
		// Closed() is the real barrier: nats.go invokes the subscription's
		// closed handler at the very END of waitForMsgs (nats.go:3656), after
		// the delivery loop has exited, so once it fires no handler is in
		// flight. Waiting on it here is what makes Stop's <-s.done a genuine
		// join rather than a flag read.
		select {
		case <-cc.Closed():
			// The barrier resolved: no handler is in flight, so Stop's
			// unregisterJob() cannot strip ABAC attributes out from under a
			// live process(). Signalling that to Stop is the ONLY way it can
			// tell this arm from the one below — s.done closes on both.
			close(barrierOK)
		case <-time.After(budget):
			// A handler is wedged. Bounded rather than blocking shutdown
			// forever; the unacked message is redelivered on the next boot.
			// barrierOK is deliberately left OPEN, which is what makes Stop
			// report the liveness retraction that follows.
			slog.WarnContext(runCtx,
				"retirement reactor barrier timed out; a fanout may still be in flight",
				"subsystem", s.ID().String(), "timeout", budget.String())
		}
	}()

	slog.InfoContext(ctx, "retirement reactor subsystem activated", "consumer", s.cfg.ConsumerName)
	return nil
}

// Stop drains the consume loop and resets BOTH guards so a legitimate
// Prepare/Activate retry after Stop rebuilds the consumer and relaunches
// the loop rather than short-circuiting on a torn-down one.
func (s *Subsystem) Stop(ctx context.Context) error {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.done != nil {
		select {
		case <-s.done:
		case <-time.After(stopTimeout):
			// Last resort only: the goroutine's sole blocking wait is the
			// barrier, so reaching here means it never scheduled at all. The
			// wedged-fanout case does NOT come through this arm — it joins
			// cleanly and is reported by the barrierOK check below.
			slog.WarnContext(ctx,
				"retirement reactor shutdown goroutine never exited within its budget",
				"subsystem", s.ID().String(), "timeout", stopTimeout.String())
		}
		s.done = nil
	}

	// Report the consequence from the place that can observe it. A successful
	// join proves only that the goroutine exited; barrierOK proves the handler
	// was joined. When it is still open, unregisterJob() below is about to
	// retract the job's ABAC liveness under a possibly-live MoveCharacter —
	// the half-applied-fanout window, and it MUST NOT be silent.
	if s.barrierOK != nil {
		select {
		case <-s.barrierOK:
		default:
			slog.WarnContext(ctx,
				"retirement reactor retracting job liveness under a possible in-flight fanout",
				"subsystem", s.ID().String(), "barrier_timeout", s.barrierBudget().String())
		}
		s.barrierOK = nil
	}
	s.unregisterJob()
	s.cc = nil
	s.cons = nil
	s.reactor = nil
	return nil
}

// barrierBudget returns the Closed() barrier's budget: the production constant
// unless a package test shortened it.
func (s *Subsystem) barrierBudget() time.Duration {
	if s.barrier > 0 {
		return s.barrier
	}
	return barrierTimeout
}

// unregisterJob drops the job's liveness declaration, so a stopped reactor's
// identity resolves to no attributes and any residual in-flight write
// default-denies. Registry.Unregister is idempotent, so calling it from both
// the Activate rollback path and Stop is safe.
func (s *Subsystem) unregisterJob() {
	if s.cfg.Jobs != nil {
		s.cfg.Jobs.Unregister(JobName)
	}
}
