// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster

import (
	"context"
	"errors"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/samber/oops"
)

// pillRateKey is the composite key for the per-(member_id, reason) rate
// limit map maintained on registry. File-private; the rate-limit
// machinery is logically part of probe-and-pill semantics.
type pillRateKey struct {
	member MemberID
	reason PillReason
}

// ErrPillRateLimited is returned by ProbeAndPill when the same
// (member_id, reason) was pilled within PillRateLimit.
//
// NOTE: oops sentinels are NOT safe targets for errors.Is at the call
// site — samber/oops's OopsError.Is returns true for ANY OopsError,
// regardless of code. Discriminate by oops code instead (e.g. via
// errutil.AssertErrorCode in tests, or oops.AsOops + .Code() in
// production callers).
var ErrPillRateLimited = oops.Code("CLUSTER_PILL_RATE_LIMITED").
	Errorf("pill rate-limited for (member_id, reason)")

// ErrCannotPillSelf is returned by ProbeAndPill when the target id
// equals r.Self(). Defense-in-depth against a caller that bypasses
// Coordinator's missed-ack self-filter.
//
// See ErrPillRateLimited for the errors.Is caveat.
var ErrCannotPillSelf = oops.Code("CLUSTER_CANNOT_PILL_SELF").
	Errorf("cannot pill self; caller MUST filter Self() from missing-member set")

// ErrPillProbeSucceeded marks the not-an-error case where the probe
// succeeded and no pill was issued. Returned for caller-introspection;
// callers MAY treat this as nil for control-flow purposes.
//
// See ErrPillRateLimited for the errors.Is caveat.
var ErrPillProbeSucceeded = oops.Code("CLUSTER_PILL_PROBE_SUCCEEDED").
	Errorf("probe succeeded; member is alive but slow on cache_invalidate channel")

// probeAndPill is the body that replaces the T2 stub. Lives in this
// file rather than registry.go so the rate-limit + self-refusal logic
// stays close to the probe/pill semantics.
func (r *registry) probeAndPill(ctx context.Context, id MemberID, reason PillReason) error {
	// INV-CLUSTER-10: refuse self-targeted pills. We do NOT increment
	// SelfTimeoutMetrics here — that counter's contract (see
	// metrics.go cluster_self_timeout_total Help text) is "Coordinator
	// missed-ack set after probe-and-pill phase contains only Self()",
	// which is a different signal than "a buggy caller passed Self()
	// to ProbeAndPill". The Warn log below is sufficient for
	// fire-as-bug alerting.
	if id == r.self {
		r.deps.Logger.WarnContext(
			ctx,
			"cluster.ProbeAndPill self-pill refused (INV-CLUSTER-10)",
			"self", string(r.self),
			"reason", string(reason),
		)
		return ErrCannotPillSelf
	}

	// INV-CLUSTER-7: rate limit per (member_id, reason). Single-acquisition
	// "claim" pattern: check the window AND write the timestamp under
	// one lock acquisition so two concurrent ProbeAndPill calls cannot
	// both pass the gate. The check governs *attempts*, not
	// *successful pills* — concurrent racers all return
	// ErrPillRateLimited after the first claimant. issuePill below
	// does NOT re-record the timestamp (the claim is canonical).
	key := pillRateKey{member: id, reason: reason}
	r.pillRateMu.Lock()
	if r.pillRateMap == nil {
		r.pillRateMap = make(map[pillRateKey]time.Time)
	}
	if last, ok := r.pillRateMap[key]; ok {
		if time.Since(last) < r.cfg.PillRateLimit {
			r.pillRateMu.Unlock()
			return ErrPillRateLimited
		}
	}
	claimAt := time.Now()
	r.pillRateMap[key] = claimAt
	r.pillRateMu.Unlock()

	// Probe via NATS request-reply. Using RequestWithContext (vs the
	// fixed-timeout NextMsg loop) honors the caller's ctx (e.g.
	// Coordinator's deadline) and bounds the wait by ProbeTimeout via
	// a derived child context.
	probeCtx, cancel := context.WithTimeout(ctx, r.cfg.ProbeTimeout)
	defer cancel()
	msg, err := r.deps.Conn.RequestWithContext(probeCtx, SubjectProbe(r.cfg.ClusterID, id), nil)
	if err == nil {
		// Probe succeeded. Surface UnmarshalProbeReply failures via
		// Warn rather than swallowing them — a corrupt or
		// schema-drifted reply is operator-actionable.
		if reply, perr := UnmarshalProbeReply(msg.Data); perr == nil {
			r.updateMemberFromProbeReply(id, reply)
		} else {
			r.deps.Logger.WarnContext(
				ctx,
				"probe reply parse failed",
				"target", string(id),
				"err", perr.Error(),
			)
		}
		return ErrPillProbeSucceeded
	}

	// Distinguish parent-ctx cancellation from probeCtx timeout:
	// pilling a healthy peer because the caller cancelled would be a
	// spurious eviction.
	if cerr := ctx.Err(); cerr != nil {
		return oops.Code("CLUSTER_PROBE_AND_PILL_CTX_CANCELED").Wrap(cerr)
	}

	// Only timeout-on-no-reply or explicit no-responders should issue
	// a pill. Other RequestWithContext failures (ErrConnectionClosed,
	// ErrAuthorization, ErrMaxPayload, etc.) reflect local NATS/client
	// trouble — pilling the target on those would be a spurious
	// eviction of a healthy peer. nats.go returns timeout via
	// probeCtx.Err() == context.DeadlineExceeded (NOT nats.ErrTimeout,
	// which RequestWithContext does not surface).
	if errors.Is(probeCtx.Err(), context.DeadlineExceeded) ||
		errors.Is(err, nats.ErrNoResponders) {
		return r.issuePill(ctx, id, reason)
	}
	return oops.Code("CLUSTER_PROBE_AND_PILL_PROBE_FAILED").
		With("target", string(id)).
		Wrap(err)
}

func (r *registry) updateMemberFromProbeReply(id MemberID, reply ProbeReplyPayload) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.members[id]; ok {
		existing.LastHeartbeatAt = time.Now()
		existing.LastInvalidationSeq = reply.LastInvalidationSeq
		if existing.Status == StatusStale {
			existing.Status = StatusAlive
		}
	}
}

// issuePill publishes the poison pill, evicts synchronously, and
// notifies LeaveReasonPilled subscribers. The ctx parameter carries
// trace context for the log call only — the pill publish itself remains
// fire-and-forget per Decision 2 in the Phase 3c grounding doc, and the
// rate-limit timestamp was already claimed in probeAndPill. Honoring ctx
// for the publish would require switching from Conn.Publish
// (fire-and-forget) to a flush-with-context idiom; deferred until a
// caller demonstrably needs cancellation during the publish itself.
func (r *registry) issuePill(ctx context.Context, id MemberID, reason PillReason) error {
	p := PoisonPayload{
		ClusterID:           r.cfg.ClusterID,
		CoordinatorMemberID: r.self,
		Reason:              reason,
		IssuedAt:            time.Now(),
	}
	b, err := MarshalPoison(p)
	if err != nil {
		return err
	}
	if err := r.deps.Conn.Publish(SubjectPoison(r.cfg.ClusterID, id), b); err != nil {
		return oops.Code("CLUSTER_PILL_PUBLISH_FAILED").Wrap(err)
	}

	// Synchronous eviction so Coordinator's retry phase sees N-1
	// immediately. Per Phase 3c grounding doc Decision 2: "Registry.markPilled
	// removes the member from LiveMembers synchronously on the issuing side
	// — without waiting for natural heartbeat eviction."
	r.mu.Lock()
	delete(r.members, id)
	r.mu.Unlock()
	r.notifyLeft(id, LeaveReasonPilled)

	r.deps.Logger.WarnContext(
		ctx,
		"cluster pill issued",
		"self", string(r.self),
		"target", string(id),
		"reason", string(reason),
	)
	return nil
}
