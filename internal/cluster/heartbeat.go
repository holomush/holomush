// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster

import (
	"context"
	"math"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/samber/oops"
)

// Start brings the registry online: subscribes to peer alive/bye/probe/poison
// subjects, publishes the first heartbeat, and starts the heartbeat
// ticker + eviction sweeper.
//
// Lock discipline: Start mutates the lifecycle state-machine fields
// (subAlive/subBye/subProbe/subPoison/hbTicker/hbDone/evTicker/evDone)
// under r.mu. Concurrent Stop() observes a consistent state.
func (r *registry) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.subAlive != nil {
		return nil // already started
	}

	sa, err := r.deps.Conn.Subscribe(SubjectAliveWildcard(r.cfg.ClusterID), r.handleAlive)
	if err != nil {
		return oops.Code("CLUSTER_SUBSCRIBE_ALIVE_FAILED").Wrap(err)
	}
	r.subAlive = sa

	sb, err := r.deps.Conn.Subscribe(SubjectByeWildcard(r.cfg.ClusterID), r.handleBye)
	if err != nil {
		_ = sa.Unsubscribe() //nolint:errcheck // best-effort rollback; primary error already returned
		r.subAlive = nil
		return oops.Code("CLUSTER_SUBSCRIBE_BYE_FAILED").Wrap(err)
	}
	r.subBye = sb

	sp, err := r.deps.Conn.Subscribe(SubjectProbeSelf(r.cfg.ClusterID, r.self), r.handleProbe)
	if err != nil {
		_ = sa.Unsubscribe() //nolint:errcheck // best-effort rollback
		_ = sb.Unsubscribe() //nolint:errcheck // best-effort rollback
		r.subAlive = nil
		r.subBye = nil
		return oops.Code("CLUSTER_SUBSCRIBE_PROBE_FAILED").Wrap(err)
	}
	r.subProbe = sp

	spo, err := r.deps.Conn.Subscribe(SubjectPoisonSelf(r.cfg.ClusterID, r.self), r.handlePoison)
	if err != nil {
		_ = sa.Unsubscribe() //nolint:errcheck // best-effort rollback
		_ = sb.Unsubscribe() //nolint:errcheck // best-effort rollback
		_ = sp.Unsubscribe() //nolint:errcheck // best-effort rollback
		r.subAlive = nil
		r.subBye = nil
		r.subProbe = nil
		return oops.Code("CLUSTER_SUBSCRIBE_POISON_FAILED").Wrap(err)
	}
	r.subPoison = spo

	// First publish runs while holding the write lock. We pass the
	// already-known seq so publishHeartbeatLocked does not need to
	// take RLock (which would deadlock against the write lock we
	// hold). r.lastInvSeq is 0 at construction and only mutated via
	// SetLastInvalidationSeq, which can only fire after Start
	// returns — so reading it here is race-free.
	if err := r.publishHeartbeatLocked(r.lastInvSeq); err != nil {
		// Subscriptions established; first publish failed. Roll back
		// (still under the write lock).
		r.unsubAllLocked()
		return err
	}

	r.hbDone = make(chan struct{})
	r.hbTicker = time.NewTicker(r.cfg.HeartbeatInterval)
	r.wg.Add(1)
	go r.runHeartbeatTicker(r.hbTicker, r.hbDone)

	r.evDone = make(chan struct{})
	r.evTicker = time.NewTicker(r.cfg.HeartbeatInterval)
	r.wg.Add(1)
	go r.runEvictionSweeper(r.evTicker, r.evDone)

	r.deps.Logger.InfoContext(
		ctx,
		"cluster.Registry started",
		"self", string(r.self),
		"cluster_id", r.cfg.ClusterID,
		"heartbeat_interval", r.cfg.HeartbeatInterval.String(),
	)
	return nil
}

// Stop publishes the bye message, stops the heartbeat ticker + eviction
// sweeper, fences in-flight goroutines, and drains subscriptions.
// Idempotent (lifecycle.Subsystem contract permits — and tests assert —
// concurrent Stop calls).
//
// Lock discipline: phase 1 (locked) mutates lifecycle fields and snapshots
// channels/tickers/subscriptions into local copies, nilling the registry
// fields. Phase 2 (unlocked) calls wg.Wait() — goroutines need r.mu to
// progress (sweeper takes Lock; ticker's publishHeartbeatNow takes
// RLock), so holding mu across Wait would deadlock. Phase 3 drains
// subscriptions on the local copies (no shared-state access).
func (r *registry) Stop(ctx context.Context) error {
	// Phase 1 (locked): capture and clear lifecycle state.
	r.mu.Lock()
	if r.subAlive == nil {
		r.mu.Unlock()
		return nil // already stopped or never started
	}

	hbTicker := r.hbTicker
	hbDone := r.hbDone
	evTicker := r.evTicker
	evDone := r.evDone
	subs := []*nats.Subscription{r.subAlive, r.subBye, r.subProbe, r.subPoison}
	r.hbTicker = nil
	r.hbDone = nil
	r.evTicker = nil
	r.evDone = nil
	r.subAlive = nil
	r.subBye = nil
	r.subProbe = nil
	r.subPoison = nil
	r.mu.Unlock()

	// Stop tickers and signal goroutines to exit. Tickers stopped
	// before closing done channels so a final tick-fire racing the
	// shutdown is bounded to one extra publish/sweep at most.
	if hbTicker != nil {
		hbTicker.Stop()
	}
	if evTicker != nil {
		evTicker.Stop()
	}
	if hbDone != nil {
		close(hbDone)
	}
	if evDone != nil {
		close(evDone)
	}

	// Phase 2 (unlocked): fence goroutines.
	r.wg.Wait()

	// Best-effort bye publish AFTER ticker has quiesced; never raced
	// against another heartbeat publish.
	p := ByePayload{ClusterID: r.cfg.ClusterID, MemberID: r.self, Reason: ByeReasonGracefulStop}
	if b, err := MarshalBye(p); err == nil {
		_ = r.deps.Conn.Publish(SubjectBye(r.cfg.ClusterID, r.self), b) //nolint:errcheck // best-effort bye; failure does not block Stop
		_ = r.deps.Conn.Flush()                                         //nolint:errcheck // best-effort flush
	}

	// Phase 3 (unlocked, on local copies): drain subscriptions. Drain
	// (vs Unsubscribe) waits for in-flight callbacks so handleAlive /
	// handleBye / handleProbe / handlePoison cannot fire after Stop
	// returns.
	for _, s := range subs {
		if s != nil {
			_ = s.Drain() //nolint:errcheck // best-effort drain on shutdown; in-flight callbacks completed
		}
	}

	r.deps.Logger.InfoContext(ctx, "cluster.Registry stopped", "self", string(r.self))
	return nil
}

// unsubAllLocked unsubscribes all four subscriptions and clears the
// fields. MUST be called with r.mu held (Start's rollback path).
//
// Note: this rollback path uses Unsubscribe rather than Drain because
// it runs while holding r.mu and the subscriptions just opened — no
// in-flight callbacks to fence.
func (r *registry) unsubAllLocked() {
	if r.subAlive != nil {
		_ = r.subAlive.Unsubscribe() //nolint:errcheck // best-effort rollback during Start failure
		r.subAlive = nil
	}
	if r.subBye != nil {
		_ = r.subBye.Unsubscribe() //nolint:errcheck // best-effort rollback during Start failure
		r.subBye = nil
	}
	if r.subProbe != nil {
		_ = r.subProbe.Unsubscribe() //nolint:errcheck // best-effort rollback during Start failure
		r.subProbe = nil
	}
	if r.subPoison != nil {
		_ = r.subPoison.Unsubscribe() //nolint:errcheck // best-effort rollback during Start failure
		r.subPoison = nil
	}
}

func (r *registry) runHeartbeatTicker(ticker *time.Ticker, done chan struct{}) {
	defer r.wg.Done()
	for {
		select {
		case <-ticker.C:
			if err := r.publishHeartbeatNow(); err != nil {
				// cluster_heartbeat_publish_failed_total: surface the
				// publish failure so operators can alert before the
				// peer ages out via heartbeat-timeout.
				if r.deps.HeartbeatMetrics != nil {
					r.deps.HeartbeatMetrics.HeartbeatPublishFailedTotal.WithLabelValues(string(r.self)).Inc()
				}
				r.deps.Logger.Warn(
					"heartbeat publish failed",
					"self", string(r.self),
					"err", err.Error(),
				)
			}
		case <-done:
			return
		}
	}
}

// publishHeartbeatNow reads lastInvSeq under RLock and publishes a
// heartbeat. Used by the heartbeat ticker goroutine.
func (r *registry) publishHeartbeatNow() error {
	r.mu.RLock()
	seq := r.lastInvSeq
	r.mu.RUnlock()
	return r.publishHeartbeatLocked(seq)
}

// publishHeartbeatLocked publishes a heartbeat with the supplied
// invalidation seq. Holds no locks itself — callers that already hold
// r.mu (Start's first publish) call this directly to avoid a RLock /
// write-lock self-deadlock; callers without the lock use
// publishHeartbeatNow to snapshot seq under RLock first.
func (r *registry) publishHeartbeatLocked(seq uint64) error {
	p := HeartbeatPayload{
		ClusterID: r.cfg.ClusterID,
		MemberID:  r.self,
		// selfStartedAt is immutable after construction (set in
		// NewSubsystem); read without a lock. Stamping every
		// heartbeat from the immutable field decouples publishing
		// from the members map and removes a latent nil-deref panic
		// if some future code path evicts self from members.
		StartedAt:           r.selfStartedAt,
		PublishedAt:         time.Now(),
		HolomushVersion:     r.cfg.HolomushVersion,
		LastInvalidationSeq: seq,
	}
	b, err := MarshalHeartbeat(p)
	if err != nil {
		return err
	}
	if err := r.deps.Conn.Publish(SubjectAlive(r.cfg.ClusterID, r.self), b); err != nil {
		return oops.Code("CLUSTER_HEARTBEAT_PUBLISH_FAILED").Wrap(err)
	}
	return nil
}

// handleAlive processes a peer's heartbeat message.
func (r *registry) handleAlive(msg *nats.Msg) {
	p, err := UnmarshalHeartbeat(msg.Data)
	if err != nil {
		r.deps.Logger.Warn("heartbeat parse failed", "err", err.Error())
		return
	}
	if p.ClusterID != r.cfg.ClusterID {
		// INV-CLUSTER-4: drop messages from other clusters.
		r.deps.Logger.Warn("heartbeat cluster_id mismatch; dropping",
			"got", p.ClusterID, "want", r.cfg.ClusterID, "from", string(p.MemberID))
		return
	}
	if p.MemberID == r.self {
		return // ignore our own heartbeats reflected back
	}

	now := time.Now()
	skew := computeSkew(now, p.PublishedAt)

	r.mu.Lock()
	existing, present := r.members[p.MemberID]
	// INV-CLUSTER-3: duplicate-MemberID detection. ULID collision is birthday-
	// bound astronomical, but defense-in-depth: if we've already seen a
	// heartbeat from this MemberID with a different StartedAt, a
	// different process is re-using the ULID. Reject the new heartbeat
	// (preserve first-seen identity), log structured error, and emit
	// metric. CLUSTER_MEMBER_DUPLICATE_ID is the conceptual error code
	// (no Go error returned because handleAlive is fire-and-forget).
	if present && !existing.StartedAt.IsZero() && !p.StartedAt.Equal(existing.StartedAt) {
		r.mu.Unlock()
		r.deps.Logger.Warn(
			"CLUSTER_MEMBER_DUPLICATE_ID; rejecting duplicate heartbeat",
			"member_id", string(p.MemberID),
			"existing_started_at", existing.StartedAt,
			"duplicate_started_at", p.StartedAt,
		)
		if r.deps.DuplicateMemberID != nil {
			r.deps.DuplicateMemberID.DuplicateMemberIDTotal.WithLabelValues(string(p.MemberID)).Inc()
		}
		return
	}
	if !present {
		m := &Member{
			ID:                  p.MemberID,
			Status:              StatusAlive,
			StartedAt:           p.StartedAt,
			LastHeartbeatAt:     now,
			LastPublishedAt:     p.PublishedAt,
			HolomushVersion:     p.HolomushVersion,
			LastInvalidationSeq: p.LastInvalidationSeq,
			SkewSeconds:         skew,
		}
		r.members[p.MemberID] = m
		r.mu.Unlock()
		r.deps.Logger.Info(
			"cluster member joined",
			"member_id", string(p.MemberID),
			"version", p.HolomushVersion,
		)
		r.notifyJoined(*m)
		r.recordSkew(p.MemberID, skew)
		return
	}
	prevStatus := existing.Status
	existing.LastHeartbeatAt = now
	existing.LastPublishedAt = p.PublishedAt
	existing.HolomushVersion = p.HolomushVersion
	existing.LastInvalidationSeq = p.LastInvalidationSeq
	existing.SkewSeconds = skew
	if existing.Status == StatusStale || existing.Status == StatusEvicted {
		existing.Status = StatusAlive
	}
	statusChanged := prevStatus != existing.Status
	r.mu.Unlock()

	if statusChanged {
		r.notifyStatus(p.MemberID, StatusAlive)
	}
	r.recordSkew(p.MemberID, skew)
}

// handleBye processes a peer's graceful-shutdown message.
func (r *registry) handleBye(msg *nats.Msg) {
	p, err := UnmarshalBye(msg.Data)
	if err != nil {
		r.deps.Logger.Warn("bye parse failed", "err", err.Error())
		return
	}
	if p.ClusterID != r.cfg.ClusterID {
		return // INV-CLUSTER-4
	}
	if p.MemberID == r.self {
		return
	}
	r.mu.Lock()
	_, present := r.members[p.MemberID]
	if !present {
		r.mu.Unlock()
		return
	}
	delete(r.members, p.MemberID)
	r.mu.Unlock()
	r.deps.Logger.Info("cluster member left", "member_id", string(p.MemberID), "reason", "graceful_bye")
	r.notifyLeft(p.MemberID, LeaveReasonGracefulBye)
}

// handleProbe replies to a focused liveness probe.
func (r *registry) handleProbe(msg *nats.Msg) {
	r.mu.RLock()
	seq := r.lastInvSeq
	r.mu.RUnlock()

	reply := ProbeReplyPayload{MemberID: r.self, LastInvalidationSeq: seq}
	b, err := MarshalProbeReply(reply)
	if err != nil {
		r.deps.Logger.Warn("probe reply marshal failed", "err", err.Error())
		return
	}
	if msg.Reply == "" {
		r.deps.Logger.Warn("probe received with empty Reply subject; ignoring")
		return
	}
	if err := r.deps.Conn.Publish(msg.Reply, b); err != nil {
		r.deps.Logger.Warn("probe reply publish failed", "err", err.Error())
	}
}

// handlePoison processes a pill targeted at this member.
func (r *registry) handlePoison(msg *nats.Msg) {
	p, err := UnmarshalPoison(msg.Data)
	if err != nil {
		r.deps.Logger.Warn("poison parse failed", "err", err.Error())
		return
	}
	if p.ClusterID != r.cfg.ClusterID {
		return // INV-CLUSTER-4
	}
	// Pill accepted; trigger termination via injected Pill (Decision 7).
	r.deps.Pill.Trigger(context.Background(), p.Reason, p.CoordinatorMemberID)
}

// runEvictionSweeper sweeps the member set every HeartbeatInterval and
// evicts members whose LastHeartbeatAt is older than EvictAfterMissed *
// HeartbeatInterval. Ticker lifecycle is owned by Start/Stop (mirrors
// the heartbeat ticker pattern).
func (r *registry) runEvictionSweeper(ticker *time.Ticker, done chan struct{}) {
	defer r.wg.Done()
	for {
		select {
		case now := <-ticker.C:
			r.sweepEvictions(now)
		case <-done:
			return
		}
	}
}

func (r *registry) sweepEvictions(now time.Time) {
	threshold := now.Add(-time.Duration(r.cfg.EvictAfterMissed) * r.cfg.HeartbeatInterval)
	var evicted []MemberID
	r.mu.Lock()
	for id, m := range r.members {
		if id == r.self {
			continue
		}
		if m.LastHeartbeatAt.Before(threshold) && (m.Status == StatusAlive || m.Status == StatusStale) {
			delete(r.members, id)
			evicted = append(evicted, id)
		}
	}
	r.mu.Unlock()
	for _, id := range evicted {
		r.deps.Logger.Info("cluster member evicted (heartbeat timeout)", "member_id", string(id))
		r.notifyLeft(id, LeaveReasonHeartbeatTimeout)
	}
}

func (r *registry) recordSkew(source MemberID, skew float64) {
	// The threshold-cross WARN is the operator-facing signal and MUST
	// fire regardless of metrics wiring — deployments without
	// Prometheus (or tests with a nil SkewMetrics dep) still need the
	// log breadcrumb. The Prometheus gauge is the silent-observable
	// path; gate ONLY the Set() call on the nil check.
	if skew > r.cfg.SkewWarnThreshold.Seconds() {
		r.deps.Logger.Warn(
			"cluster member skew exceeds threshold",
			"self", string(r.self),
			"source_id", string(source),
			"skew_seconds", skew,
			"threshold_seconds", r.cfg.SkewWarnThreshold.Seconds(),
		)
	}
	if r.deps.SkewMetrics != nil {
		r.deps.SkewMetrics.SkewSeconds.WithLabelValues(string(r.self), string(source)).Set(skew)
	}
}

// computeSkew returns absolute drift in seconds between local clock and
// the remote-sourced published_at timestamp. INV-CLUSTER-8 carve-out: this
// computation is the single allowed cross-host clock comparison; the
// result feeds an observability gauge only and never gates protocol
// decisions (Phase 3c grounding doc Decision 8).
//
// The Sub call below operates on the parameter `remotePublishedAt`
// rather than a struct selector, so the noremoteclockcompare analyzer
// (which is purely syntactic) does not fire here. The conceptual
// carve-out is documented for human reviewers; the actual cross-clock
// surface is `computeSkew(now, p.PublishedAt)` at the call site.
func computeSkew(localNow, remotePublishedAt time.Time) float64 {
	diff := localNow.Sub(remotePublishedAt).Seconds()
	return math.Abs(diff)
}
