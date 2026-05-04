// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/samber/oops"
)

// HeartbeatPayload is published on internal.<cluster_id>.member.alive.<member_id>
// every HeartbeatInterval. Receivers update their view of the source's
// LastHeartbeatAt (local clock) and LastPublishedAt (sender clock, used
// for skew detection only).
type HeartbeatPayload struct {
	ClusterID           string    `json:"cluster_id"`
	MemberID            MemberID  `json:"member_id"`
	StartedAt           time.Time `json:"started_at"`
	PublishedAt         time.Time `json:"published_at"`
	HolomushVersion     string    `json:"holomush_version"`
	LastInvalidationSeq uint64    `json:"last_invalidation_seq"`
}

// ByeReason is the reason a member published a bye. Typed enum prevents
// magic-string drift between publisher (heartbeat.Stop) and any future
// consumer that branches on the reason.
type ByeReason string

const (
	// ByeReasonGracefulStop indicates the member published bye as part
	// of a graceful Stop().
	ByeReasonGracefulStop ByeReason = "graceful_stop"
)

// ByePayload is published once on internal.<cluster_id>.member.bye.<member_id>
// at graceful Stop. Receivers evict the source immediately (no wait for
// heartbeat-timeout).
type ByePayload struct {
	ClusterID string    `json:"cluster_id"`
	MemberID  MemberID  `json:"member_id"`
	Reason    ByeReason `json:"reason"`
}

// ProbePayload is the request body of a focused liveness probe sent on
// internal.<cluster_id>.member.probe.<member_id>. Empty by design; the
// subject conveys the target.
type ProbePayload struct{}

// ProbeReplyPayload is the response published on the probe's reply
// inbox. Carries the same observability fields as a heartbeat (the
// receiver may update its registry view from the reply).
type ProbeReplyPayload struct {
	MemberID            MemberID `json:"member_id"`
	LastInvalidationSeq uint64   `json:"last_invalidation_seq"`
}

// PoisonPayload is published on internal.<cluster_id>.member.poison.<member_id>
// to terminate a member. Publish-and-forget; no reply.
type PoisonPayload struct {
	ClusterID           string     `json:"cluster_id"`
	CoordinatorMemberID MemberID   `json:"coordinator_member_id"`
	Reason              PillReason `json:"reason"`
	IssuedAt            time.Time  `json:"issued_at"`
}

// SubjectAlive returns the subject pattern for heartbeat publishes.
func SubjectAlive(clusterID string, id MemberID) string {
	return fmt.Sprintf("internal.%s.member.alive.%s", clusterID, string(id))
}

// SubjectAliveWildcard returns the wildcard pattern subscribers use to
// receive any peer's heartbeat.
func SubjectAliveWildcard(clusterID string) string {
	return fmt.Sprintf("internal.%s.member.alive.>", clusterID)
}

// SubjectBye returns the subject pattern for graceful-stop publishes.
func SubjectBye(clusterID string, id MemberID) string {
	return fmt.Sprintf("internal.%s.member.bye.%s", clusterID, string(id))
}

// SubjectByeWildcard returns the wildcard pattern for bye subscriptions.
func SubjectByeWildcard(clusterID string) string {
	return fmt.Sprintf("internal.%s.member.bye.>", clusterID)
}

// SubjectProbe returns the subject pattern for the probe sent to a
// specific member.
func SubjectProbe(clusterID string, id MemberID) string {
	return fmt.Sprintf("internal.%s.member.probe.%s", clusterID, string(id))
}

// SubjectProbeSelf returns the subject pattern this member subscribes
// to in order to receive probes targeting it.
func SubjectProbeSelf(clusterID string, self MemberID) string {
	return SubjectProbe(clusterID, self)
}

// SubjectPoison returns the subject pattern for a pill targeting a
// specific member.
func SubjectPoison(clusterID string, id MemberID) string {
	return fmt.Sprintf("internal.%s.member.poison.%s", clusterID, string(id))
}

// SubjectPoisonSelf returns the subject pattern this member subscribes
// to in order to receive its own pill.
func SubjectPoisonSelf(clusterID string, self MemberID) string {
	return SubjectPoison(clusterID, self)
}

// MarshalHeartbeat marshals a heartbeat payload, returning a typed
// error on failure. JSON is the chosen format because operators
// debugging via `nats sub` benefit from readable subjects + readable
// payloads; protobuf would obscure both.
func MarshalHeartbeat(p HeartbeatPayload) ([]byte, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, oops.Code("CLUSTER_MARSHAL_HEARTBEAT_FAILED").Wrap(err)
	}
	return b, nil
}

// UnmarshalHeartbeat unmarshals a heartbeat payload, returning a typed
// error on failure.
func UnmarshalHeartbeat(b []byte) (HeartbeatPayload, error) {
	var p HeartbeatPayload
	if err := json.Unmarshal(b, &p); err != nil {
		return HeartbeatPayload{}, oops.Code("CLUSTER_UNMARSHAL_HEARTBEAT_FAILED").Wrap(err)
	}
	return p, nil
}

// MarshalBye marshals a bye payload.
func MarshalBye(p ByePayload) ([]byte, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, oops.Code("CLUSTER_MARSHAL_BYE_FAILED").Wrap(err)
	}
	return b, nil
}

// UnmarshalBye unmarshals a bye payload.
func UnmarshalBye(b []byte) (ByePayload, error) {
	var p ByePayload
	if err := json.Unmarshal(b, &p); err != nil {
		return ByePayload{}, oops.Code("CLUSTER_UNMARSHAL_BYE_FAILED").Wrap(err)
	}
	return p, nil
}

// MarshalProbeReply marshals a probe-reply payload.
func MarshalProbeReply(p ProbeReplyPayload) ([]byte, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, oops.Code("CLUSTER_MARSHAL_PROBE_REPLY_FAILED").Wrap(err)
	}
	return b, nil
}

// UnmarshalProbeReply unmarshals a probe-reply payload.
func UnmarshalProbeReply(b []byte) (ProbeReplyPayload, error) {
	var p ProbeReplyPayload
	if err := json.Unmarshal(b, &p); err != nil {
		return ProbeReplyPayload{}, oops.Code("CLUSTER_UNMARSHAL_PROBE_REPLY_FAILED").Wrap(err)
	}
	return p, nil
}

// MarshalPoison marshals a poison payload.
func MarshalPoison(p PoisonPayload) ([]byte, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, oops.Code("CLUSTER_MARSHAL_POISON_FAILED").Wrap(err)
	}
	return b, nil
}

// UnmarshalPoison unmarshals a poison payload.
func UnmarshalPoison(b []byte) (PoisonPayload, error) {
	var p PoisonPayload
	if err := json.Unmarshal(b, &p); err != nil {
		return PoisonPayload{}, oops.Code("CLUSTER_UNMARSHAL_POISON_FAILED").Wrap(err)
	}
	return p, nil
}
