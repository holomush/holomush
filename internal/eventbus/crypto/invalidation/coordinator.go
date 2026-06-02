// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package invalidation

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/cluster"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

// Coordinator orchestrates cross-replica DEK cache invalidation.
type Coordinator interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	RequestInvalidation(ctx context.Context, ctxID dek.ContextID, action Action, version, successorVersion uint32) error
}

// New constructs a Coordinator.
func New(cfg Config, deps Deps) (Coordinator, error) {
	cfg = cfg.Defaults()
	if cfg.ClusterID == "" {
		return nil, oops.Code("INVALIDATION_CONFIG_MISSING_CLUSTER_ID").
			Errorf("Coordinator requires non-empty ClusterID")
	}
	if deps.Conn == nil || deps.Registry == nil || deps.DEKCache == nil || deps.PartCache == nil {
		return nil, oops.Code("INVALIDATION_DEPS_NIL").
			Errorf("Coordinator requires non-nil Conn, Registry, DEKCache, PartCache")
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &coordinator{
		cfg:  cfg,
		deps: deps,
		seq:  cfg.SeqStart,
	}, nil
}

type coordinator struct {
	cfg  Config
	deps Deps

	seq uint64

	sub *nats.Subscription
}

// timeoutFor returns the action-specific timeout. KEK rotation uses
// 30s per INV-CLUSTER-1; everything else uses cfg.InvalidateTimeout (default 5s).
func (c *coordinator) timeoutFor(action Action) time.Duration {
	if action == ActionKEKRotation {
		return 30 * time.Second
	}
	return c.cfg.InvalidateTimeout
}

// Start subscribes to the cache_invalidate.dek.> wildcard and runs the
// receive loop. Receive-side handler body lives in T10 (this commit
// stubs the handler and lets Start succeed).
func (c *coordinator) Start(ctx context.Context) error {
	if c.sub != nil {
		return nil
	}
	sub, err := c.deps.Conn.Subscribe(SubjectCacheInvalidateWildcard(c.cfg.ClusterID), c.handleInvalidate)
	if err != nil {
		return oops.Code("INVALIDATION_SUBSCRIBE_FAILED").Wrap(err)
	}
	c.sub = sub
	c.deps.Logger.InfoContext(ctx, "invalidation.Coordinator started", "cluster_id", c.cfg.ClusterID)
	return nil
}

// Stop drains the subscription. Idempotent.
func (c *coordinator) Stop(_ context.Context) error {
	if c.sub == nil {
		return nil
	}
	if err := c.sub.Drain(); err != nil {
		return oops.Code("INVALIDATION_DRAIN_FAILED").Wrap(err)
	}
	c.sub = nil
	return nil
}

// RequestInvalidation publishes an invalidation request and waits for
// N-of-N replica acks; on partial timeout, runs probe-and-pill on
// missing members and retries once. INV-CLUSTER-1, INV-CLUSTER-2, INV-CLUSTER-6, INV-CLUSTER-10.
func (c *coordinator) RequestInvalidation(
	ctx context.Context,
	ctxID dek.ContextID,
	action Action,
	version, successorVersion uint32,
) error {
	seq := atomic.AddUint64(&c.seq, 1)
	timeout := c.timeoutFor(action)

	// Snapshot live members ONCE per publish attempt. Both `expected`
	// (size) and `missing` (set difference vs acks) MUST come from the
	// same snapshot, otherwise a member that joined after publish can
	// be misclassified as "missing" and probed/pilled even though it
	// never had a chance to ack this request.
	snapshot1 := c.deps.Registry.LiveMembers()
	if len(snapshot1) == 0 {
		return ErrNoLiveMembers
	}

	payload := Payload{
		Seq:                 seq,
		CoordinatorMemberID: c.deps.Registry.Self(),
		ClusterID:           c.cfg.ClusterID,
		ContextType:         ctxID.Type,
		ContextID:           ctxID.ID,
		Action:              action,
		IssuedAt:            time.Now(),
		Version:             version,
		SuccessorVersion:    successorVersion,
	}

	acks, err := c.publishAndCollect(ctx, payload, len(snapshot1), timeout)
	if err != nil {
		return err
	}
	if len(acks) == len(snapshot1) {
		c.recordSuccess(action, "success", payload.IssuedAt)
		return nil
	}

	// Probe-and-pill phase. Compute missing against the SAME snapshot
	// used to derive the expected ack count.
	missing := computeMissingFromSnapshot(snapshot1, acks)
	// INV-CLUSTER-10: filter Self() from missing set.
	self := c.deps.Registry.Self()
	selfFiltered := make([]cluster.MemberID, 0, len(missing))
	for _, m := range missing {
		if m == self {
			continue
		}
		selfFiltered = append(selfFiltered, m)
	}
	if len(selfFiltered) == 0 && len(missing) > 0 {
		// Only self was missing → SELF_TIMEOUT
		c.deps.Logger.WarnContext(
			ctx,
			"invalidation: only Self() missing from acks; not pilling self",
			"self", string(self),
			"action", string(action),
		)
		c.recordSuccess(action, "self_timeout", payload.IssuedAt)
		return ErrSelfTimeout
	}

	for _, member := range selfFiltered {
		ppErr := c.deps.Registry.ProbeAndPill(ctx, member, cluster.PillReasonMissedInvalidationAck)
		if ppErr == nil {
			// Pill issued; member already removed from registry synchronously.
			continue
		}
		// Discriminate cluster sentinels by oops error code (errors.Is
		// is tautological for samber/oops sentinels — see probe_pill.go
		// for the same caveat).
		oerr, isOops := oops.AsOops(ppErr)
		if !isOops {
			// Non-oops error from ProbeAndPill: surface and abort the
			// retry phase so the caller can decide whether to retry.
			return oops.Code("INVALIDATION_PROBE_AND_PILL_FAILED").Wrap(ppErr)
		}
		switch oerr.Code() {
		case "CLUSTER_PILL_PROBE_SUCCEEDED":
			// Probe succeeded; member is alive, no pill issued. Continue
			// the retry phase — the next publish round may catch it.
			continue
		case "CLUSTER_PILL_RATE_LIMITED":
			// Per-member rate limit hit; caller should retry after the
			// PillRateLimit window.
			return ErrRateLimited
		default:
			// CLUSTER_PROBE_AND_PILL_CTX_CANCELED, CLUSTER_PROBE_AND_PILL_PROBE_FAILED,
			// CLUSTER_CANNOT_PILL_SELF (defensive trip), or any other oops
			// error from ProbeAndPill — propagate so the caller sees the
			// failure instead of silently continuing into the retry phase.
			//
			// CAVEAT (samber/oops v1.21+): OopsError.Code() walks to the
			// DEEPEST code in the chain via getDeepestErrorCode. So
			// `oops.Code(OUTER).Wrap(innerOopsErr)` would silently surface
			// the inner code, hiding the outer code we just attached. To
			// preserve INVALIDATION_PROBE_AND_PILL_FAILED as the surfaced
			// code, we use Errorf (which constructs a fresh OopsError
			// whose .err is a plain fmt error containing the inner's
			// formatted message) and stash the inner code + target in
			// With() context. Callers that need the inner code read it
			// from `inner_code` rather than walking the error chain.
			// See holomush-ojw1.3.22.
			return oops.Code("INVALIDATION_PROBE_AND_PILL_FAILED").
				With("probe_target", string(member)).
				With("inner_code", oerr.Code()).
				Errorf("probe-and-pill failed for member %s: %s", string(member), ppErr.Error())
		}
	}

	// Retry once. Re-snapshot because ProbeAndPill may have evicted
	// members, and we want the retry's expected count to match the
	// post-eviction reality.
	snapshot2 := c.deps.Registry.LiveMembers()
	if len(snapshot2) == 0 {
		return ErrNoLiveMembers
	}
	acks2, err := c.publishAndCollect(ctx, payload, len(snapshot2), timeout)
	if err != nil {
		return err
	}
	if len(acks2) == len(snapshot2) {
		c.recordSuccess(action, "success_after_retry", payload.IssuedAt)
		return nil
	}

	missing2 := computeMissingFromSnapshot(snapshot2, acks2)
	c.recordSuccess(action, "partial_failure", payload.IssuedAt)
	return oops.Code("INVALIDATION_PARTIAL_FAILURE").
		With("missing_members", missing2).
		With("action", string(action)).
		With("ctx_type", ctxID.Type).
		With("ctx_id", ctxID.ID).
		Errorf("invalidation timed out after probe-and-pill retry")
}

// publishAndCollect publishes one invalidation request, opens a reply
// inbox, and collects acks until len(acks)==expected, the derived
// timeout fires, or the caller's ctx is canceled.
//
// On caller-ctx cancellation: returns the partial acks and a wrapped
// ctx.Err() so callers can distinguish shutdown from timeout. On
// timeout (no parent cancellation): returns the partial acks and nil
// error — callers compare len(acks) to expected to decide success.
func (c *coordinator) publishAndCollect(
	ctx context.Context,
	payload Payload,
	expected int,
	timeout time.Duration,
) (map[cluster.MemberID]struct{}, error) {
	body, err := MarshalPayload(payload)
	if err != nil {
		return nil, err
	}
	inbox := c.deps.Conn.NewRespInbox()
	sub, err := c.deps.Conn.SubscribeSync(inbox)
	if err != nil {
		return nil, oops.Code("INVALIDATION_INBOX_SUB_FAILED").Wrap(err)
	}
	defer sub.Drain() //nolint:errcheck // best-effort cleanup of inbox subscription

	if err := c.deps.Conn.PublishRequest(
		SubjectCacheInvalidate(c.cfg.ClusterID, payload.ContextType, payload.ContextID),
		inbox, body,
	); err != nil {
		return nil, oops.Code("INVALIDATION_PUBLISH_FAILED").Wrap(err)
	}

	// Child context combines the caller's deadline with our derived
	// timeout. NextMsgWithContext returns whichever fires first.
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	acks := make(map[cluster.MemberID]struct{}, expected)
	for len(acks) < expected {
		msg, err := sub.NextMsgWithContext(waitCtx)
		if err != nil {
			// Distinguish parent-ctx cancellation (caller wants to
			// stop) from our derived-timeout expiry (collect-window
			// closed). Parent cancellation propagates as error;
			// derived timeout returns partial acks with nil error.
			if ctx.Err() != nil {
				return acks, oops.Code("INVALIDATION_INBOX_READ_FAILED").Wrap(ctx.Err())
			}
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, nats.ErrTimeout) {
				break
			}
			// Other errors surface immediately.
			return acks, oops.Code("INVALIDATION_INBOX_READ_FAILED").Wrap(err)
		}
		reply, perr := UnmarshalReply(msg.Data)
		if perr != nil {
			c.deps.Logger.WarnContext(ctx, "invalidation: parse reply failed", "err", perr.Error())
			continue
		}
		if reply.Ack {
			acks[reply.MemberID] = struct{}{}
		}
	}
	return acks, nil
}

// computeMissingFromSnapshot returns the members in `snapshot` that
// did not appear in `acks`. Working from a caller-provided snapshot
// (rather than re-querying LiveMembers) ensures the expected-ack set
// and the observed-missing set come from the same membership view —
// closes the late-joiner false-positive race.
func computeMissingFromSnapshot(snapshot []cluster.Member, acks map[cluster.MemberID]struct{}) []cluster.MemberID {
	missing := make([]cluster.MemberID, 0, len(snapshot))
	for i := range snapshot {
		id := snapshot[i].ID
		if _, ok := acks[id]; !ok {
			missing = append(missing, id)
		}
	}
	return missing
}

// recordSuccess increments the acks-total and observes latency. Latency
// is computed from the local clock at IssuedAt; observability-only per
// Decision 8.
func (c *coordinator) recordSuccess(action Action, outcome string, issuedAt time.Time) {
	if c.deps.Metrics == nil {
		return
	}
	c.deps.Metrics.AcksTotal.WithLabelValues(string(action), outcome).Inc()
	c.deps.Metrics.LatencySeconds.WithLabelValues(string(action)).Observe(time.Since(issuedAt).Seconds())
}

// handleInvalidate is the receive-side handler. Parses payload,
// dispatches on action enum, evicts caches per Phase 3c grounding doc
// Decision 5/6, and acks via msg.Respond. INV-CLUSTER-4: cluster_id-mismatch
// messages dropped without ack.
//
// Note on time.Since(payload.IssuedAt): in single-process tests the
// publisher's clock IS the consumer's clock, but in production the
// publisher is a different replica. This call therefore compares
// remote clocks. Per Decision 8 the metric is observability-only and
// not used in any control flow, so the cross-clock skew is acceptable.
// T11 introduces a real ruleguard for cross-clock comparisons in
// control paths; this site is intentionally exempt.
func (c *coordinator) handleInvalidate(msg *nats.Msg) {
	payload, err := UnmarshalPayload(msg.Data)
	if err != nil {
		c.deps.Logger.Warn("invalidation: parse failed", "err", err.Error())
		return
	}
	if payload.ClusterID != c.cfg.ClusterID {
		c.deps.Logger.Warn(
			"invalidation: cross-cluster message dropped",
			"got", payload.ClusterID, "want", c.cfg.ClusterID,
		)
		if c.deps.Metrics != nil {
			c.deps.Metrics.CrossClusterDrops.Inc()
		}
		return
	}

	ctxID := dek.ContextID{Type: payload.ContextType, ID: payload.ContextID}
	switch payload.Action {
	case ActionRekey:
		c.deps.DEKCache.InvalidateContext(ctxID)
		c.deps.PartCache.InvalidateContext(ctxID)

	case ActionParticipantsChanged:
		c.deps.PartCache.Invalidate(dek.ParticipantsCacheKey{
			ContextType: payload.ContextType,
			ContextID:   payload.ContextID,
			Version:     payload.Version,
		})

	case ActionRotate, ActionKEKRotation:
		// No-op eviction per Decision 5; protocol ack still required.

	default:
		c.deps.Logger.Warn(
			"invalidation: unknown action; not acking",
			"action", string(payload.Action),
		)
		if c.deps.Metrics != nil {
			c.deps.Metrics.UnknownActions.Inc()
		}
		return
	}

	if r, ok := c.deps.Registry.(interface{ SetLastInvalidationSeq(uint64) }); ok {
		r.SetLastInvalidationSeq(payload.Seq)
	}

	if c.deps.Metrics != nil {
		c.deps.Metrics.LatencySeconds.WithLabelValues(string(payload.Action)).
			Observe(time.Since(payload.IssuedAt).Seconds()) //nolint:noremoteclockcompare // observability-only per Decision 8: latency histogram does not feed protocol decisions; cross-clock skew is acceptable here.
	}

	reply := Reply{MemberID: c.deps.Registry.Self(), Ack: true}
	body, err := MarshalReply(reply)
	if err != nil {
		c.deps.Logger.Warn("invalidation: marshal reply failed", "err", err.Error())
		return
	}
	if msg.Reply == "" {
		c.deps.Logger.Warn("invalidation: msg.Reply empty; cannot ack")
		return
	}
	if err := c.deps.Conn.Publish(msg.Reply, body); err != nil {
		c.deps.Logger.Warn("invalidation: ack publish failed", "err", err.Error())
	}
}
