// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package invalidation provides cross-replica DEK cache invalidation
// via NATS request-reply with N-of-N replica acks. Phase 3c grounding
// doc Decision 5. Coordinator is constructable (NOT a subsystem in
// 3c); production wiring lands at Phase 3d alongside Crypto.Enabled.
package invalidation

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/cluster"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

// Action enumerates the cache-invalidation actions; receivers
// dispatch on this field. See Phase 3c grounding doc Decision 6.
type Action string

// Action enum values per the Decision 6 action table.
const (
	ActionRotate              Action = "rotate"
	ActionRekey               Action = "rekey"
	ActionParticipantsChanged Action = "participants_changed"
	ActionKEKRotation         Action = "kek_rotation"
)

// Payload is the cache-invalidation message shape. Per-action
// population of `Version` / `SuccessorVersion` per the action enum
// table in Decision 6:
//
//	rotate              — Version=old, SuccessorVersion=new
//	rekey               — Version=destroyed, SuccessorVersion=replacement
//	participants_changed — Version=mutated active version; SuccessorVersion unused (zero)
//	kek_rotation        — both unused (zero)
type Payload struct {
	Seq                 uint64           `json:"seq"`
	CoordinatorMemberID cluster.MemberID `json:"coordinator_member_id"`
	ClusterID           string           `json:"cluster_id"`
	ContextType         string           `json:"ctx_type"`
	ContextID           string           `json:"ctx_id"`
	Action              Action           `json:"action"`
	IssuedAt            time.Time        `json:"issued_at"`
	Version             uint32           `json:"version,omitempty"`
	SuccessorVersion    uint32           `json:"successor_version,omitempty"`
}

// Reply is the per-member ack payload.
type Reply struct {
	MemberID cluster.MemberID `json:"member_id"`
	Ack      bool             `json:"ack"`
}

// Config parameterizes the Coordinator. Defaults applied to zero values
// via Defaults().
type Config struct {
	ClusterID         string        // sourced from eventbus.Config.GameID
	InvalidateTimeout time.Duration // 5s; 30s overridden internally for ActionKEKRotation
	SeqStart          uint64        // 0 in production; configurable for tests
}

// Defaults applies the master-spec defaults.
func (c Config) Defaults() Config {
	if c.InvalidateTimeout <= 0 {
		c.InvalidateTimeout = 5 * time.Second
	}
	return c
}

// Deps groups the Coordinator's runtime dependencies.
type Deps struct {
	Conn      *nats.Conn
	Registry  cluster.Registry
	DEKCache  *dek.Cache
	PartCache *dek.ParticipantsCache
	Logger    *slog.Logger
	Metrics   *Metrics
}

// SubjectCacheInvalidate returns the cache-invalidate subject for
// (cluster_id, ctx_type, ctx_id).
func SubjectCacheInvalidate(clusterID, ctxType, ctxID string) string {
	return "internal." + clusterID + ".cache_invalidate.dek." + ctxType + "." + ctxID
}

// SubjectCacheInvalidateWildcard returns the wildcard subscribers use.
func SubjectCacheInvalidateWildcard(clusterID string) string {
	return "internal." + clusterID + ".cache_invalidate.dek.>"
}

// MarshalPayload returns JSON or a typed error.
func MarshalPayload(p Payload) ([]byte, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, oops.Code("INVALIDATION_MARSHAL_PAYLOAD_FAILED").Wrap(err)
	}
	return b, nil
}

// UnmarshalPayload returns JSON or a typed error.
func UnmarshalPayload(b []byte) (Payload, error) {
	var p Payload
	if err := json.Unmarshal(b, &p); err != nil {
		return Payload{}, oops.Code("INVALIDATION_UNMARSHAL_PAYLOAD_FAILED").Wrap(err)
	}
	return p, nil
}

// MarshalReply returns JSON or a typed error.
func MarshalReply(r Reply) ([]byte, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, oops.Code("INVALIDATION_MARSHAL_REPLY_FAILED").Wrap(err)
	}
	return b, nil
}

// UnmarshalReply returns the parsed Reply or a typed error.
func UnmarshalReply(b []byte) (Reply, error) {
	var r Reply
	if err := json.Unmarshal(b, &r); err != nil {
		return Reply{}, oops.Code("INVALIDATION_UNMARSHAL_REPLY_FAILED").Wrap(err)
	}
	return r, nil
}

// Typed errors returned by RequestInvalidation. See Phase 3c grounding
// doc Decision 5 error-code table.
//
// CAVEAT 1 (errors.Is): samber/oops's OopsError.Is returns true for ANY
// OopsError, regardless of code. Do NOT use errors.Is(err, ErrFoo) to
// discriminate these sentinels — it is tautological and matches every
// oops error. In tests, use errutil.AssertErrorCode(t, err, "CODE"). In
// production code, use oops.AsOops(err) and compare oopsErr.Code() to
// the string literal of the error code.
//
// CAVEAT 2 (deepest-Code traversal, holomush-ojw1.3.22): in
// samber/oops@v1.21+, OopsError.Code() walks to the DEEPEST code in
// the chain via getDeepestErrorCode. This means
// `oops.Code(OUTER).Wrap(innerOopsErr)` SILENTLY surfaces the inner
// code, not the outer one. To preserve the outer code as the surfaced
// .Code(), use `oops.Code(OUTER).With("inner_code", inner.Code()).Errorf(...)`
// which constructs a fresh OopsError whose .err is a plain fmt error,
// breaking the chain walk. Wrapping NON-oops errors (e.g., NATS errors,
// json errors, ctx.Err()) is fine — the deepest-walk only traverses
// through oops-typed children.
var (
	// ErrPartialFailure — code "INVALIDATION_PARTIAL_FAILURE"
	ErrPartialFailure = oops.Code("INVALIDATION_PARTIAL_FAILURE").
				Errorf("invalidation timed out after probe-and-pill retry; missing members in error context")
	// ErrNoLiveMembers — code "INVALIDATION_NO_LIVE_MEMBERS"
	ErrNoLiveMembers = oops.Code("INVALIDATION_NO_LIVE_MEMBERS").
				Errorf("LiveCount() == 0; substrate bug")
	// ErrRateLimited — code "INVALIDATION_RATE_LIMITED"
	ErrRateLimited = oops.Code("INVALIDATION_RATE_LIMITED").
			Errorf("ProbeAndPill returned ErrPillRateLimited; caller should retry after PillRateLimit")
	// ErrCrossCluster — code "INVALIDATION_CROSS_CLUSTER"
	ErrCrossCluster = oops.Code("INVALIDATION_CROSS_CLUSTER").
			Errorf("received message with mismatched cluster_id; dropped")
	// ErrSelfTimeout — code "INVALIDATION_SELF_TIMEOUT"
	ErrSelfTimeout = oops.Code("INVALIDATION_SELF_TIMEOUT").
			Errorf("missing-member set after probe-and-pill contains only Self(); local handler hang on N=1")
)
