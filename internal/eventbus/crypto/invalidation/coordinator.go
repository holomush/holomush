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
// 30s per INV-28; everything else uses cfg.InvalidateTimeout (default 5s).
func (c *coordinator) timeoutFor(action Action) time.Duration {
	if action == ActionKEKRotation {
		return 30 * time.Second
	}
	return c.cfg.InvalidateTimeout
}

// Start subscribes to the cache_invalidate.dek.> wildcard and runs the
// receive loop. Receive-side handler body lives in T10 (this commit
// stubs the handler and lets Start succeed).
func (c *coordinator) Start(_ context.Context) error {
	if c.sub != nil {
		return nil
	}
	sub, err := c.deps.Conn.Subscribe(SubjectCacheInvalidateWildcard(c.cfg.ClusterID), c.handleInvalidate)
	if err != nil {
		return oops.Code("INVALIDATION_SUBSCRIBE_FAILED").Wrap(err)
	}
	c.sub = sub
	c.deps.Logger.Info("invalidation.Coordinator started", "cluster_id", c.cfg.ClusterID)
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
// missing members and retries once. INV-28, INV-29, INV-56, INV-60.
func (c *coordinator) RequestInvalidation(
	ctx context.Context,
	ctxID dek.ContextID,
	action Action,
	version, successorVersion uint32,
) error {
	seq := atomic.AddUint64(&c.seq, 1)
	timeout := c.timeoutFor(action)

	n1 := c.deps.Registry.LiveCount()
	if n1 == 0 {
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

	acks, err := c.publishAndCollect(ctx, payload, n1, timeout)
	if err != nil {
		return err
	}
	if len(acks) == n1 {
		c.recordSuccess(action, "success", payload.IssuedAt)
		return nil
	}

	// Probe-and-pill phase.
	missing := c.computeMissing(acks)
	// INV-60: filter Self() from missing set.
	selfFiltered := make([]cluster.MemberID, 0, len(missing))
	for _, m := range missing {
		if m == c.deps.Registry.Self() {
			continue
		}
		selfFiltered = append(selfFiltered, m)
	}
	if len(selfFiltered) == 0 && len(missing) > 0 {
		// Only self was missing → SELF_TIMEOUT
		c.deps.Logger.Warn("invalidation: only Self() missing from acks; not pilling self",
			"self", string(c.deps.Registry.Self()),
			"action", string(action),
		)
		c.recordSuccess(action, "self_timeout", payload.IssuedAt)
		return ErrSelfTimeout
	}

	for _, member := range selfFiltered {
		ppErr := c.deps.Registry.ProbeAndPill(ctx, member, cluster.PillReasonMissedInvalidationAck)
		// Discriminate cluster.ErrPillRateLimited by oops error code.
		// errors.Is is tautological for samber/oops sentinels — see
		// probe_pill.go for the same caveat.
		if ppErr != nil {
			if oerr, ok := oops.AsOops(ppErr); ok && oerr.Code() == "CLUSTER_PILL_RATE_LIMITED" {
				return ErrRateLimited
			}
		}
		// ErrPillProbeSucceeded or nil: continue. Pilled member already
		// removed from registry synchronously.
	}

	// Retry once.
	n2 := c.deps.Registry.LiveCount()
	if n2 == 0 {
		return ErrNoLiveMembers
	}
	acks2, err := c.publishAndCollect(ctx, payload, n2, timeout)
	if err != nil {
		return err
	}
	if len(acks2) == n2 {
		c.recordSuccess(action, "success_after_retry", payload.IssuedAt)
		return nil
	}

	missing2 := c.computeMissing(acks2)
	c.recordSuccess(action, "partial_failure", payload.IssuedAt)
	return oops.Code("INVALIDATION_PARTIAL_FAILURE").
		With("missing_members", missing2).
		With("action", string(action)).
		With("ctx_type", ctxID.Type).
		With("ctx_id", ctxID.ID).
		Errorf("invalidation timed out after probe-and-pill retry")
}

// publishAndCollect publishes one invalidation request, opens a reply
// inbox, and collects acks until len(acks)==expected or timeout fires.
func (c *coordinator) publishAndCollect(
	_ context.Context,
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

	acks := make(map[cluster.MemberID]struct{}, expected)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) && len(acks) < expected {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		msg, err := sub.NextMsg(remaining)
		if err != nil {
			if errors.Is(err, nats.ErrTimeout) {
				break
			}
			// Other errors surface immediately.
			return acks, oops.Code("INVALIDATION_INBOX_READ_FAILED").Wrap(err)
		}
		reply, perr := UnmarshalReply(msg.Data)
		if perr != nil {
			c.deps.Logger.Warn("invalidation: parse reply failed", "err", perr.Error())
			continue
		}
		if reply.Ack {
			acks[reply.MemberID] = struct{}{}
		}
	}
	return acks, nil
}

// computeMissing returns the live members that did not ack.
func (c *coordinator) computeMissing(acks map[cluster.MemberID]struct{}) []cluster.MemberID {
	members := c.deps.Registry.LiveMembers()
	missing := make([]cluster.MemberID, 0, len(members))
	for i := range members {
		id := members[i].ID
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

// handleInvalidate is the receive-side stub; T10 implements the body.
func (c *coordinator) handleInvalidate(msg *nats.Msg) {
	// T10: parse, dispatch on action, evict caches, ack.
	_ = msg
}
