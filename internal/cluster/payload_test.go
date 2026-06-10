// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

func TestHeartbeatPayloadRoundTripsThroughJSON(t *testing.T) {
	in := HeartbeatPayload{
		ClusterID:           "test-game",
		MemberID:            MemberID("01HEXAMPLE"),
		StartedAt:           time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC),
		PublishedAt:         time.Date(2026, 5, 2, 12, 0, 5, 0, time.UTC),
		HolomushVersion:     "dev",
		LastInvalidationSeq: 42,
	}
	b, err := MarshalHeartbeat(in)
	require.NoError(t, err)
	out, err := UnmarshalHeartbeat(b)
	require.NoError(t, err)
	assert.Equal(t, in, out)
}

func TestSubjectAliveProducesClusterIdNamespacedSubject(t *testing.T) {
	got := SubjectAlive("test-game", MemberID("01HEXAMPLE"))
	want := "internal.test-game.member.alive.01HEXAMPLE"
	assert.Equal(t, want, got)
}

func TestSubjectPoisonProducesClusterIdNamespacedSubject(t *testing.T) {
	got := SubjectPoison("test-game", MemberID("01HVICTIM"))
	want := "internal.test-game.member.poison.01HVICTIM"
	assert.Equal(t, want, got)
}

func TestUnmarshalHeartbeatReturnsTypedErrorOnGarbage(t *testing.T) {
	_, err := UnmarshalHeartbeat([]byte("not json"))
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CLUSTER_UNMARSHAL_HEARTBEAT_FAILED")
}

func TestByePayloadRoundTripsThroughJSON(t *testing.T) {
	in := ByePayload{
		ClusterID: "test-game",
		MemberID:  MemberID("01HEXAMPLE"),
		Reason:    ByeReasonGracefulStop,
	}
	b, err := MarshalBye(in)
	require.NoError(t, err)
	out, err := UnmarshalBye(b)
	require.NoError(t, err)
	assert.Equal(t, in, out)
}

func TestUnmarshalByeReturnsTypedErrorOnGarbage(t *testing.T) {
	_, err := UnmarshalBye([]byte("not json"))
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CLUSTER_UNMARSHAL_BYE_FAILED")
}

func TestProbeReplyPayloadRoundTripsThroughJSON(t *testing.T) {
	in := ProbeReplyPayload{
		MemberID:            MemberID("01HEXAMPLE"),
		LastInvalidationSeq: 7,
	}
	b, err := MarshalProbeReply(in)
	require.NoError(t, err)
	out, err := UnmarshalProbeReply(b)
	require.NoError(t, err)
	assert.Equal(t, in, out)
}

func TestUnmarshalProbeReplyReturnsTypedErrorOnGarbage(t *testing.T) {
	_, err := UnmarshalProbeReply([]byte("not json"))
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CLUSTER_UNMARSHAL_PROBE_REPLY_FAILED")
}

func TestPoisonPayloadRoundTripsThroughJSON(t *testing.T) {
	in := PoisonPayload{
		ClusterID:           "test-game",
		CoordinatorMemberID: MemberID("01HSOURCE"),
		Reason:              PillReasonMissedInvalidationAck,
		IssuedAt:            time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC),
	}
	b, err := MarshalPoison(in)
	require.NoError(t, err)
	out, err := UnmarshalPoison(b)
	require.NoError(t, err)
	assert.Equal(t, in, out)
}

func TestUnmarshalPoisonReturnsTypedErrorOnGarbage(t *testing.T) {
	_, err := UnmarshalPoison([]byte("not json"))
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CLUSTER_UNMARSHAL_POISON_FAILED")
}

func TestSubjectAliveWildcardProducesClusterIdNamespacedSubject(t *testing.T) {
	got := SubjectAliveWildcard("test-game")
	want := "internal.test-game.member.alive.>"
	assert.Equal(t, want, got)
}

func TestSubjectByeProducesClusterIdNamespacedSubject(t *testing.T) {
	got := SubjectBye("test-game", MemberID("01HEXAMPLE"))
	want := "internal.test-game.member.bye.01HEXAMPLE"
	assert.Equal(t, want, got)
}

func TestSubjectByeWildcardProducesClusterIdNamespacedSubject(t *testing.T) {
	got := SubjectByeWildcard("test-game")
	want := "internal.test-game.member.bye.>"
	assert.Equal(t, want, got)
}

func TestSubjectProbeProducesClusterIdNamespacedSubject(t *testing.T) {
	got := SubjectProbe("test-game", MemberID("01HEXAMPLE"))
	want := "internal.test-game.member.probe.01HEXAMPLE"
	assert.Equal(t, want, got)
}

func TestSubjectProbeSelfMatchesSubjectProbe(t *testing.T) {
	self := MemberID("01HSELF")
	assert.Equal(t, SubjectProbe("test-game", self), SubjectProbeSelf("test-game", self),
		"SubjectProbeSelf must match SubjectProbe to subscribe to own probe inbox")
}

func TestSubjectPoisonSelfMatchesSubjectPoison(t *testing.T) {
	self := MemberID("01HSELF")
	assert.Equal(t, SubjectPoison("test-game", self), SubjectPoisonSelf("test-game", self),
		"SubjectPoisonSelf must match SubjectPoison to subscribe to own pill inbox")
}
