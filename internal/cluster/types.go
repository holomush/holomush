// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package cluster provides cluster membership, health/status, and
// failure-remediation primitives. Phase 3c (holomush-ojw1.3) ships
// the substrate; future consumers (admin RPCs, leader election) build
// on the same Registry surface.
package cluster

import "time"

// MemberID identifies a cluster member. Per-process: each holomush
// process generates a fresh ULID-formatted MemberID at startup.
// Persistent identity is intentionally NOT supported — restarted
// processes appear as new members; old members get evicted via
// heartbeat-timeout or graceful `bye`. See the Phase 3c grounding
// doc Decision 1 for rationale.
type MemberID string

// MemberStatus is a closed enum for member lifecycle state.
type MemberStatus int

// MemberStatus values enumerate the lifecycle states a cluster member
// may be in from this process's perspective.
const (
	StatusUnknown MemberStatus = iota
	StatusAlive                // heartbeat fresh within 2× heartbeat interval
	StatusStale                // 1-2 missed heartbeats; not yet evicted
	StatusEvicted              // 3+ missed heartbeats or received bye
	StatusPilled               // pilled by a coordinator; removed
)

// String returns a human-readable status name (used in logs/metrics).
func (s MemberStatus) String() string {
	switch s {
	case StatusAlive:
		return "alive"
	case StatusStale:
		return "stale"
	case StatusEvicted:
		return "evicted"
	case StatusPilled:
		return "pilled"
	default:
		return "unknown"
	}
}

// Member is the registry's view of a cluster member. SkewSeconds is
// computed at receive time per Phase 3c grounding doc Decision 8.
type Member struct {
	ID                  MemberID
	Status              MemberStatus
	StartedAt           time.Time // sender's wall-clock at process start; observability only
	LastHeartbeatAt     time.Time // receiver's local clock at last receive
	LastPublishedAt     time.Time // sender's wall-clock at last heartbeat publish
	HolomushVersion     string
	LastInvalidationSeq uint64  // observability; not used in protocol decisions
	SkewSeconds         float64 // 0 for self; computed for peers per Decision 8
}

// LeaveReason is a closed enum for the OnMemberLeft observer callback.
type LeaveReason int

// LeaveReason values enumerate why a member left the cluster from
// this process's perspective.
const (
	LeaveReasonGracefulBye LeaveReason = iota
	LeaveReasonHeartbeatTimeout
	LeaveReasonPilled
)

// String returns a human-readable reason name.
func (r LeaveReason) String() string {
	switch r {
	case LeaveReasonGracefulBye:
		return "graceful_bye"
	case LeaveReasonHeartbeatTimeout:
		return "heartbeat_timeout"
	case LeaveReasonPilled:
		return "pilled"
	default:
		return "unknown"
	}
}

// PillReason is a closed enum carried in pill payloads. Used as a
// Prometheus label and structured-log field.
type PillReason string

// PillReason values enumerate why a coordinator issued a pill against
// a peer member.
const (
	PillReasonMissedInvalidationAck PillReason = "missed_invalidation_ack"
	PillReasonMissedProbeResponse   PillReason = "missed_probe_response"
	PillReasonOperatorEvict         PillReason = "operator_evict"      // future @evict-member
	PillReasonClusterIDMismatch     PillReason = "cluster_id_mismatch" // defensive
)

// MemberObserver is the callback interface for membership change
// events. Future consumers (admin RPC, leader election) implement
// this. Observers MUST NOT block; long work belongs in a separate
// goroutine.
type MemberObserver interface {
	OnMemberJoined(Member)
	OnMemberLeft(MemberID, LeaveReason)
	OnMemberStatusChanged(MemberID, MemberStatus)
}

// Config parameterizes the Registry. Defaults are applied to zero
// values via Defaults(); production wiring constructs Config in
// cmd/holomush/core.go using the existing version variable + the
// eventbus.Config.GameID.
//
// HolomushVersion is sourced from cmd/holomush/main.go's `version`
// ldflag-set variable, injected via dependency at subsystem
// construction. internal/cluster/ MUST NOT import from cmd/holomush/
// or introduce its own ldflag variable (per Phase 3c grounding doc
// Decision 1).
type Config struct {
	// ClusterID is a given value for callers that already know it at
	// construction (test literals). ClusterIDProvider, resolved once at
	// Start, is the production path — it wins when non-nil. At least one
	// of the two MUST resolve to a non-empty string; NewSubsystem rejects
	// only the both-unset case, and Start rejects a both-resolve-empty
	// case (CLUSTER_CONFIG_MISSING_CLUSTER_ID either way).
	ClusterID         string
	ClusterIDProvider func() string
	HolomushVersion   string
	HeartbeatInterval time.Duration
	EvictAfterMissed  int
	ProbeTimeout      time.Duration
	PillRateLimit     time.Duration
	SkewWarnThreshold time.Duration
}

// Defaults applies the master-spec / grounding-doc defaults to any
// zero-value field on cfg and returns the result.
func (cfg Config) Defaults() Config {
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 5 * time.Second
	}
	if cfg.EvictAfterMissed <= 0 {
		cfg.EvictAfterMissed = 3
	}
	if cfg.ProbeTimeout <= 0 {
		cfg.ProbeTimeout = 250 * time.Millisecond
	}
	if cfg.PillRateLimit <= 0 {
		cfg.PillRateLimit = 60 * time.Second
	}
	if cfg.SkewWarnThreshold <= 0 {
		cfg.SkewWarnThreshold = 30 * time.Second
	}
	return cfg
}
