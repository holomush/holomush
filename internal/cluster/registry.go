// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster

import (
	"context"
	"log/slog"
	"reflect"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/eventbus/natsconn"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/lifecycle"
)

// Registry is the cluster membership and health surface. Phase 3c ships
// a single concrete implementation backed by an in-process NATS connection.
type Registry interface {
	// Lifecycle (called by subsystem orchestrator)
	ID() lifecycle.SubsystemID
	DependsOn() []lifecycle.SubsystemID
	Start(ctx context.Context) error
	Stop(ctx context.Context) error

	// Self returns this process's MemberID.
	Self() MemberID

	// LiveMembers returns a snapshot of currently-live members. O(N)
	// allocation; safe for concurrent use.
	LiveMembers() []Member

	// Member returns the registry's view of a specific member. Returns
	// false if the member is not in the live set.
	Member(id MemberID) (Member, bool)

	// LiveCount returns the size of the live set. O(1) atomic-style
	// read via the registry mutex; used by Coordinator (T9) to compute
	// N before each invalidation publish. Always >= 1 (self counts).
	LiveCount() int

	// ProbeAndPill issues a focused liveness probe (T4 implementation;
	// stubbed in T2 to return ErrNotImplemented).
	ProbeAndPill(ctx context.Context, id MemberID, reason PillReason) error

	// Subscribe registers an observer for membership change events.
	Subscribe(observer MemberObserver) (cancel func())
}

// Deps groups the dependencies cluster.Registry needs at construction.
//
// Conn is typed as natsconn.Conn (the narrow interface seam) rather
// than *nats.Conn so unit tests MAY substitute a mock. Production
// callers continue to pass eventbus.Subsystem.Conn() directly — the
// concrete *nats.Conn satisfies natsconn.Conn structurally. See
// internal/eventbus/natsconn for the interface and natsmock for a
// test-only mock implementation. (holomush-ojw1.3.23)
type Deps struct {
	Conn              natsconn.Conn // from eventbus.Subsystem.Conn(); test mocks via natsconn/natsmock
	Logger            *slog.Logger
	PillMetrics       *PillMetrics
	SkewMetrics       *SkewMetrics
	SelfTimeout       *SelfTimeoutMetrics
	DuplicateMemberID *DuplicateMemberIDMetrics // INV-CLUSTER-3 detection metric
	HeartbeatMetrics  *HeartbeatMetrics         // heartbeat-publish failures (ticker path)
	Pill              Pill                      // production / test / dev
	SelfIDForTest     MemberID                  // tests inject; production uses ulid.Make()
}

// NewSubsystem constructs a Registry-backed Subsystem. Production
// callers pass a real *nats.Conn (which satisfies natsconn.Conn
// structurally) and ProductionPill. Tests use the clustertest harness
// or, for unit-level error-path tests, a natsmock.Conn.
func NewSubsystem(cfg Config, deps Deps) (Registry, error) {
	cfg = cfg.Defaults()
	if cfg.ClusterID == "" {
		return nil, oops.Code("CLUSTER_CONFIG_MISSING_CLUSTER_ID").
			Errorf("cluster.NewSubsystem requires non-empty ClusterID; sourced from eventbus.Config.GameID")
	}
	if deps.Conn == nil || isNilConn(deps.Conn) {
		return nil, oops.Code("CLUSTER_DEPS_NIL").With("dep", "Conn").
			Errorf("cluster.NewSubsystem requires a non-nil natsconn.Conn (typically *nats.Conn from eventbus.Subsystem.Conn())")
	}
	if deps.Pill == nil {
		return nil, oops.Code("CLUSTER_DEPS_NIL").With("dep", "Pill").
			Errorf("cluster.NewSubsystem requires a non-nil Pill")
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	self := deps.SelfIDForTest
	if self == "" {
		self = MemberID(idgen.New().String())
	}
	startedAt := time.Now()
	r := &registry{
		cfg:           cfg,
		deps:          deps,
		self:          self,
		selfStartedAt: startedAt,
		members: map[MemberID]*Member{
			self: {ID: self, Status: StatusAlive, StartedAt: startedAt, HolomushVersion: cfg.HolomushVersion},
		},
		observers: map[*observerEntry]struct{}{},
	}
	return r, nil
}

type registry struct {
	cfg  Config
	deps Deps
	self MemberID
	// selfStartedAt is the wall-clock time at which this process's
	// registry was constructed. Immutable after NewSubsystem; safe to
	// read without a lock. publishHeartbeatNow stamps every outgoing
	// heartbeat with this value (decoupling heartbeat publishing from
	// the members map and removing a latent nil-deref panic if some
	// future change evicts self from members).
	selfStartedAt time.Time

	mu        sync.RWMutex
	members   map[MemberID]*Member
	observers map[*observerEntry]struct{}

	// Lifecycle state-machine fields. All writes (Start/Stop) and
	// reads of these MUST occur under mu — Subsystem callers MAY call
	// Stop concurrently and the lifecycle contract requires Stop be
	// idempotent. The exception is `publishHeartbeatNow`, which only
	// reads selfStartedAt (immutable) and lastInvSeq (under mu).
	//
	// Subscriptions held while Started. Cleaned up in Stop.
	subAlive  *nats.Subscription
	subBye    *nats.Subscription
	subProbe  *nats.Subscription
	subPoison *nats.Subscription

	// Heartbeat ticker control.
	hbTicker *time.Ticker
	hbDone   chan struct{}

	// Eviction sweeper control.
	evTicker *time.Ticker
	evDone   chan struct{}

	// wg fences the goroutines started in Start. Stop closes the done
	// channels then waits on wg before draining subscriptions, so no
	// goroutine is in flight when Stop returns. Critical for Subsystem
	// orderly-shutdown semantics — callers that Stop() then re-init a
	// new server in the same process MUST observe a fully quiesced
	// previous registry.
	wg sync.WaitGroup

	// Tracks last published invalidation seq for inclusion in
	// outgoing heartbeats. Updated by external setters in T9.
	lastInvSeq uint64

	// Pill rate-limit machinery (INV-CLUSTER-7). Tracks the timestamp of the
	// most-recent pill issued for each (member_id, reason) tuple. Lazy
	// map init in probeAndPill / issuePill — zero-value sync.Mutex and
	// nil map are safe.
	pillRateMu  sync.Mutex
	pillRateMap map[pillRateKey]time.Time
}

type observerEntry struct {
	obs MemberObserver
}

func (r *registry) ID() lifecycle.SubsystemID { return lifecycle.SubsystemCluster }
func (r *registry) DependsOn() []lifecycle.SubsystemID {
	return []lifecycle.SubsystemID{lifecycle.SubsystemEventBus}
}
func (r *registry) Self() MemberID { return r.self }

func (r *registry) LiveMembers() []Member {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Member, 0, len(r.members))
	for _, m := range r.members {
		if m.Status == StatusAlive || m.Status == StatusStale {
			out = append(out, *m)
		}
	}
	return out
}

func (r *registry) Member(id MemberID) (Member, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.members[id]
	if !ok {
		return Member{}, false
	}
	return *m, true
}

func (r *registry) LiveCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n := 0
	for _, m := range r.members {
		if m.Status == StatusAlive || m.Status == StatusStale {
			n++
		}
	}
	return n
}

func (r *registry) Subscribe(obs MemberObserver) (cancel func()) {
	if obs == nil {
		return func() {}
	}
	entry := &observerEntry{obs: obs}
	r.mu.Lock()
	r.observers[entry] = struct{}{}
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		delete(r.observers, entry)
		r.mu.Unlock()
	}
}

// notifyJoined / notifyLeft / notifyStatus fan out to all observers
// while holding only the registry's read lock briefly to snapshot the
// observer set. Observer callbacks themselves run outside the lock so
// a slow observer cannot stall registry operations.
func (r *registry) notifyJoined(m Member) {
	obs := r.snapshotObservers()
	for _, e := range obs {
		e.obs.OnMemberJoined(m)
	}
}

func (r *registry) notifyLeft(id MemberID, reason LeaveReason) {
	obs := r.snapshotObservers()
	for _, e := range obs {
		e.obs.OnMemberLeft(id, reason)
	}
}

func (r *registry) notifyStatus(id MemberID, status MemberStatus) {
	obs := r.snapshotObservers()
	for _, e := range obs {
		e.obs.OnMemberStatusChanged(id, status)
	}
}

func (r *registry) snapshotObservers() []*observerEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*observerEntry, 0, len(r.observers))
	for e := range r.observers {
		out = append(out, e)
	}
	return out
}

// ProbeAndPill issues a focused liveness probe and pills on timeout.
// Body lives in probe_pill.go to keep the rate-limit + self-refusal
// logic close to the probe/pill semantics.
func (r *registry) ProbeAndPill(ctx context.Context, id MemberID, reason PillReason) error {
	return r.probeAndPill(ctx, id, reason)
}

// SetLastInvalidationSeq is the seam Coordinator (T9) uses to update
// the seq number stamped on outgoing heartbeats.
func (r *registry) SetLastInvalidationSeq(seq uint64) {
	r.mu.Lock()
	r.lastInvSeq = seq
	r.mu.Unlock()
}

// isNilConn detects typed-nil interface values whose underlying concrete
// kind is nilable (pointer, slice, map, chan, func, interface). Required
// because Deps.Conn is an interface (natsconn.Conn introduced in
// holomush-ojw1.3.23) — a plain `== nil` comparison only checks the
// interface header, missing typed-nil values like (*nats.Conn)(nil)
// (see internal/eventbus/natsconn/natsconn_test.go:33-37 for the
// runtime demonstration). Mirrors the pattern in
// internal/core/engine.go::isNilEventAppender so callers truly fail
// fast at construction rather than crashing on first method call.
func isNilConn(c natsconn.Conn) bool {
	v := reflect.ValueOf(c)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
