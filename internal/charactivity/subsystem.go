// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package charactivity hosts the character last_active_at tracker (IDENT-10).
//
// D-42 put last_active_at in a JetStream KV bucket fed by a durable
// activity listener plus a periodic flush ticker, rather than writing the
// column synchronously on every event. The deciding property is that the
// EMIT PATH NEVER TOUCHES POSTGRES: a character-actor event costs one KV
// Put, and the column is advanced later, in bulk, by the flush ticker.
//
// The accepted consequence is that the column LAGS by up to one flush
// interval, BY CONSTRUCTION. A reader that needs real-time activity must
// consult the buffer, not the column.
package charactivity

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/consumer"
	"github.com/holomush/holomush/internal/lifecycle"
)

// stopTimeout bounds how long Stop waits for the listener and flush ticker
// to unwind. Mirrors the outbox relay subsystem's budget.
const stopTimeout = 5 * time.Second

// BucketName is the JetStream KV bucket the listener buffers into. It is the
// repo's FIRST KV bucket; the name is a durable, not-safely-renamable
// identifier (a rename strands the existing bucket and its unflushed keys).
const BucketName = "character_activity"

// DefaultConsumerName is the durable JetStream consumer name. Durable ⇒ the
// listener resumes from the last acked sequence across restarts, so activity
// generated while the host was down is still buffered on the way back up.
const DefaultConsumerName = "character_activity_listener"

// ConsumerFilterSubject is the D-42 subscription: EVERY event on every game's
// feed. That breadth is the point — session start/end and character-generated
// activity both surface as ordinary events carrying a character Actor, so a
// broad filter answers "this character did something" with no per-domain
// enumeration to keep in sync. Same shape the audit projector uses.
const ConsumerFilterSubject = eventbus.SubjectFilter

// JobName is the background-job identity this subsystem declares at Activate.
//
// 02.2's D-68 is option A+D: EVERY background consumer registers an identity
// and a declared capability class; only EVENT-DRIVEN ones additionally carry
// per-execution instance scoping. This subsystem's write is timer-driven, so
// it stamps no per-execution attributes — and, as a fact about its current
// write path rather than a reason to skip the identity, that write lands at
// the INV-WORLD-4 writer boundary and crosses no ABAC chokepoint, so nothing
// consumes the identity at an Evaluate call today. Registering anyway means
// the identity and class are already correct the moment anything in this
// subsystem does cross one.
const JobName = "character_activity"

// jobWrites is the capability class declared at registration (D-50). It
// NARROWS: a seed must still grant the write, so the declaration alone
// authorizes nothing (D-51).
var jobWrites = []string{"character"}

// defaultFlushInterval is how often the ticker drains the buffer. It is the
// column's worst-case lag, and the planner-owned value from D-42.
const defaultFlushInterval = 5 * time.Minute

// ActivityWriter advances characters.last_active_at for one character.
//
// It is a NAMED FUNCTION TYPE rather than a method-bearing interface because
// its production implementation is a FREE function at the writer boundary
// (worldpostgres.UpdateCharacterLastActive), which no interface could satisfy.
// The composition root closes it over the world pool; this package therefore
// imports no database driver and holds no fenced SQL (INV-WORLD-4).
type ActivityWriter func(ctx context.Context, characterID ulid.ULID, lastActiveNanos int64) error

// JetStreamProvider yields the JetStream context at Prepare time. Same shape
// (and same rationale) as audit.JSProvider: the eventbus subsystem has not
// started when this subsystem is constructed, so the accessor call is deferred.
type JetStreamProvider interface {
	JS() jetstream.JetStream
}

// JobRegistry is the narrow view of internal/jobs.Registry this subsystem
// needs to declare its own liveness and capability class.
type JobRegistry interface {
	Register(name string, writes []string) error
	Unregister(name string)
}

// consumeStarter is the one jetstream.Consumer method the subsystem uses.
// Declared narrowly so a package test can substitute a fake without
// implementing the whole jetstream.Consumer surface.
type consumeStarter interface {
	Consume(handler jetstream.MessageHandler, opts ...jetstream.PullConsumeOpt) (jetstream.ConsumeContext, error)
}

// Config configures the character-activity subsystem.
//
// Every live value arrives through a PROVIDER or a function value rather than
// eagerly at construction: cmd/holomush builds this subsystem before the
// database and event bus have started.
type Config struct {
	Logger *slog.Logger

	// JetStream yields the JetStream handle at Prepare time.
	JetStream JetStreamProvider
	// Writer advances characters.last_active_at. Required.
	Writer ActivityWriter
	// Jobs is the background-job liveness registry. Optional: a nil registry
	// means no job attributes are ever stamped — the correct fail-closed state,
	// not a bypass.
	Jobs JobRegistry

	// FlushInterval is how often the buffer is drained. Defaults to
	// defaultFlushInterval; tests shorten it.
	FlushInterval time.Duration
	// ConsumerName overrides DefaultConsumerName. Tests only — changing it in
	// production strands the existing durable consumer.
	ConsumerName string
	// AckWait and MaxAckPending mirror the audit projector's defaults when zero.
	AckWait       time.Duration
	MaxAckPending int
}

// Consumer tuning defaults, mirroring the audit projector so the durable
// consumers on the EVENTS stream age and redeliver alike.
const (
	defaultAckWait       = 5 * time.Second
	defaultMaxAckPending = 64
)

// Defaults fills any zero-valued tuning field.
func (c Config) Defaults() Config {
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.ConsumerName == "" {
		c.ConsumerName = DefaultConsumerName
	}
	if c.FlushInterval <= 0 {
		c.FlushInterval = defaultFlushInterval
	}
	if c.AckWait == 0 {
		c.AckWait = defaultAckWait
	}
	if c.MaxAckPending == 0 {
		c.MaxAckPending = defaultMaxAckPending
	}
	return c
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

	// kv is the buffer AND the Prepare guard. Nil until Prepare has run.
	kv activityKV
	// cons is the durable listener consumer, created by Prepare.
	cons consumeStarter
	// cc is the live consume context, stopped by Stop.
	cc jetstream.ConsumeContext
	// cancel cancels the context both producers run under.
	cancel context.CancelFunc
	// done guards Activate and is closed when the listener wrapper and the
	// flush ticker have BOTH exited. Nil until Activate has run.
	done chan struct{}

	// createKV and createConsumer are the acquisition seams. Nil in
	// production, where Prepare builds the real bucket and durable consumer;
	// package tests substitute fakes so the lifecycle contract can be
	// exercised without a live JetStream.
	createKV       func(ctx context.Context) (activityKV, error)
	createConsumer func(ctx context.Context) (consumeStarter, error)
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
	return &Subsystem{cfg: cfg.Defaults(), storage: storage}
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
// process-internal-substrate carve-out. Idempotent behind the s.kv guard.
func (s *Subsystem) Prepare(ctx context.Context) error {
	if s.kv != nil {
		return nil // already prepared
	}
	if s.cfg.Writer == nil {
		return oops.Code("CHARACTER_ACTIVITY_UNWIRED").Errorf("activity writer is required")
	}
	createKV := s.createKV
	if createKV == nil {
		createKV = s.createBucket
	}
	createConsumer := s.createConsumer
	if createConsumer == nil {
		createConsumer = s.createDurableConsumer
	}
	kv, err := createKV(ctx)
	if err != nil {
		return err
	}
	cons, err := createConsumer(ctx)
	if err != nil {
		return err
	}
	s.kv = kv
	s.cons = cons
	slog.InfoContext(ctx, "character activity subsystem prepared",
		"bucket", BucketName,
		"consumer", s.cfg.ConsumerName,
		"filter_subject", ConsumerFilterSubject,
		"kv_storage", s.storage.String(),
		"flush_interval", s.cfg.FlushInterval.String())
	return nil
}

// js resolves the live JetStream handle, rejecting an unwired composition root
// at Prepare rather than nil-dereferencing on the first delivered message.
func (s *Subsystem) js() (jetstream.JetStream, error) {
	if s.cfg.JetStream == nil {
		return nil, oops.Code("CHARACTER_ACTIVITY_UNWIRED").Errorf("jetstream provider is required")
	}
	js := s.cfg.JetStream.JS()
	if js == nil {
		return nil, oops.Code("CHARACTER_ACTIVITY_UNWIRED").Errorf("jetstream handle is not available")
	}
	return js, nil
}

// createBucket is the production KV-acquisition path.
//
// CreateOrUpdateKeyValue, never CreateKeyValue: the lifecycle contract requires
// Prepare to be re-runnable, and CreateKeyValue answers ErrBucketExists on the
// second boot. Storage is passed EXPLICITLY on both branches — a KV bucket
// carries its own storage config and does NOT inherit the stream's, and
// FileStorage is the zero value, so an omitted field is silently disk-backed
// even inside a memory-only test harness (D-42's corrected hazard).
func (s *Subsystem) createBucket(ctx context.Context) (activityKV, error) {
	js, err := s.js()
	if err != nil {
		return nil, err
	}
	kv, err := js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      BucketName,
		Description: "buffered characters.last_active_at epoch-nanos, flushed periodically",
		History:     1, // only the latest activity per character matters
		Storage:     s.storage,
		Replicas:    1,
	})
	if err != nil {
		return nil, oops.Code("CHARACTER_ACTIVITY_BUCKET_CREATE_FAILED").
			With("bucket", BucketName).
			Wrap(err)
	}
	return jsKV{kv: kv}, nil
}

// createDurableConsumer is the production consumer-create path. The create call
// goes through consumer.CreateWithRetry (the shared D-46 helper) so this
// consumer absorbs the same JetStream warmup window the audit projector does.
// The helper codes nothing, so the wrap below is the whole error surface.
func (s *Subsystem) createDurableConsumer(ctx context.Context) (consumeStarter, error) {
	js, err := s.js()
	if err != nil {
		return nil, err
	}
	cons, err := consumer.CreateWithRetry(ctx, func(ctx context.Context) (jetstream.Consumer, error) {
		return js.CreateOrUpdateConsumer(ctx, eventbus.StreamName, jetstream.ConsumerConfig{
			Durable:       s.cfg.ConsumerName,
			Name:          s.cfg.ConsumerName,
			FilterSubject: ConsumerFilterSubject,
			AckPolicy:     jetstream.AckExplicitPolicy,
			AckWait:       s.cfg.AckWait,
			MaxAckPending: s.cfg.MaxAckPending,
		})
	})
	if err != nil {
		return nil, oops.Code("CHARACTER_ACTIVITY_CONSUMER_CREATE_FAILED").
			With("stream", eventbus.StreamName).
			With("consumer", s.cfg.ConsumerName).
			With("nats_err", err.Error()).
			Wrap(err)
	}
	return cons, nil
}

// Activate starts the listener consume loop and the flush ticker — domain
// traffic. Idempotent behind the done-channel guard.
//
// It REFUSES to run without a prepared bucket rather than no-op'ing. The
// orchestrator always runs the whole Prepare sweep before the whole Activate
// sweep, so this never fires in production; if it ever did, a silent success
// would leave last_active_at permanently unwritten with nothing to point at.
func (s *Subsystem) Activate(ctx context.Context) error {
	if s.done != nil {
		return nil // already activated
	}
	if s.kv == nil || s.cons == nil {
		return oops.Code("CHARACTER_ACTIVITY_NOT_PREPARED").
			Errorf("activate called before prepare; no KV bucket exists")
	}

	// Declare the job LIVE before a single message can be delivered. Authority
	// is tied to liveness (D-49): registering after the producers start would
	// open a window in which the job's attributes are absent.
	if s.cfg.Jobs != nil {
		if err := s.cfg.Jobs.Register(JobName, jobWrites); err != nil {
			// Wrapped for context only, NOT re-coded: the registry already
			// codes its rejections.
			return oops.With("job", JobName).Wrap(err)
		}
	}

	// The handler runs on JetStream's own goroutines with no context of its
	// own, so the effect context is stored on the listener BEFORE Consume
	// registers the callback — assigning it after registration is a data race.
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.cancel = cancel

	l := &listener{cfg: s.cfg, kv: s.kv, ctx: runCtx}
	cc, err := s.cons.Consume(l.handle)
	if err != nil {
		cancel()
		s.cancel = nil
		s.unregisterJob()
		return oops.Code("CHARACTER_ACTIVITY_CONSUME_FAILED").
			With("consumer", s.cfg.ConsumerName).
			Wrap(err)
	}
	s.cc = cc

	// done closes only after BOTH producers have exited, which is what makes
	// Stop's join real (R2b).
	done := make(chan struct{})
	s.done = done
	f := &flusher{cfg: s.cfg, kv: s.kv}
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-runCtx.Done()
			cc.Stop()
			// Stop() IS NOT A BARRIER. In nats.go v1.52.0 it only does
			// closed.CompareAndSwap(0,1) + close(s.done) and returns
			// (jetstream/pull.go:768); the handler runs on the NATS
			// subscription's own dispatch goroutine, which Stop never joins.
			// Without the wait below a listener Put could land concurrently
			// with — or after — Stop's final drain, which is exactly what the
			// R2b claim on Stop forbids.
			//
			// Closed() is the real barrier: nats.go invokes the subscription's
			// closed handler at the very END of waitForMsgs (nats.go:3656),
			// after the delivery loop has exited, so once it fires no handler
			// is in flight.
			select {
			case <-cc.Closed():
			case <-time.After(stopTimeout):
				// A handler is wedged. Bounded rather than blocking shutdown
				// forever; the buffer is durable, so the next boot flushes it.
			}
		}()
		go func() {
			defer wg.Done()
			f.run(runCtx)
		}()
		wg.Wait()
	}()

	slog.InfoContext(ctx, "character activity subsystem activated",
		"consumer", s.cfg.ConsumerName, "flush_interval", s.cfg.FlushInterval.String())
	return nil
}

// Stop halts both producers, JOINS them, and only then runs one final drain,
// so no listener Put can race the shutdown flush (R2b). Both guards are reset
// so a legitimate Prepare/Activate retry after Stop reattaches the bucket and
// relaunches the loops rather than short-circuiting on torn-down state.
func (s *Subsystem) Stop(ctx context.Context) error {
	kv := s.kv

	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	// ACTIVATED and JOINED are distinct states, and conflating them is a bug.
	// s.done is nil both when Activate never ran AND after a successful join,
	// so treating nil as "joined" made a subsystem that only reached Prepare
	// run a full drain — listing every key in the bucket and issuing one
	// UPDATE characters per key. That is exactly the state the orchestrator's
	// rollback path produces (internal/lifecycle/orchestrator.go:77-83 calls
	// rollback -> Stop on everything already prepared) when some OTHER
	// subsystem fails to prepare or activate. A failed boot MUST NOT mutate
	// characters.
	activated := s.done != nil
	joined := false
	if activated {
		select {
		case <-s.done:
			joined = true
		case <-time.After(stopTimeout):
			// A producer is wedged. Skipping the final drain is the safe
			// answer: the buffer is durable, so the next boot flushes it.
		}
		s.done = nil
	}

	// One last drain, on a context detached from the (already cancelled)
	// producer context — and ONLY once both producers are known to be gone.
	if activated && joined && kv != nil {
		drainCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
		(&flusher{cfg: s.cfg, kv: kv}).drain(drainCtx)
		cancel()
	}

	s.unregisterJob()
	s.cc = nil
	s.cons = nil
	s.kv = nil
	return nil
}

// unregisterJob drops the job's liveness declaration, so a stopped job's
// identity resolves to no attributes. Registry.Unregister is idempotent, so
// calling it from both the Activate rollback path and Stop is safe.
func (s *Subsystem) unregisterJob() {
	if s.cfg.Jobs != nil {
		s.cfg.Jobs.Unregister(JobName)
	}
}
