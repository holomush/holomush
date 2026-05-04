// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster

import (
	"testing"
	"time"

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
	if err != nil {
		t.Fatalf("MarshalHeartbeat: %v", err)
	}
	out, err := UnmarshalHeartbeat(b)
	if err != nil {
		t.Fatalf("UnmarshalHeartbeat: %v", err)
	}
	if out != in {
		t.Errorf("round-trip mismatch: got %+v want %+v", out, in)
	}
}

func TestSubjectAliveProducesClusterIdNamespacedSubject(t *testing.T) {
	got := SubjectAlive("test-game", MemberID("01HEXAMPLE"))
	want := "internal.test-game.member.alive.01HEXAMPLE"
	if got != want {
		t.Errorf("SubjectAlive = %q; want %q", got, want)
	}
}

func TestSubjectPoisonProducesClusterIdNamespacedSubject(t *testing.T) {
	got := SubjectPoison("test-game", MemberID("01HVICTIM"))
	want := "internal.test-game.member.poison.01HVICTIM"
	if got != want {
		t.Errorf("SubjectPoison = %q; want %q", got, want)
	}
}

func TestUnmarshalHeartbeatReturnsTypedErrorOnGarbage(t *testing.T) {
	_, err := UnmarshalHeartbeat([]byte("not json"))
	if err == nil {
		t.Fatal("expected error on garbage payload")
	}
	errutil.AssertErrorCode(t, err, "CLUSTER_UNMARSHAL_HEARTBEAT_FAILED")
}

func TestByePayloadRoundTripsThroughJSON(t *testing.T) {
	in := ByePayload{
		ClusterID: "test-game",
		MemberID:  MemberID("01HEXAMPLE"),
		Reason:    ByeReasonGracefulStop,
	}
	b, err := MarshalBye(in)
	if err != nil {
		t.Fatalf("MarshalBye: %v", err)
	}
	out, err := UnmarshalBye(b)
	if err != nil {
		t.Fatalf("UnmarshalBye: %v", err)
	}
	if out != in {
		t.Errorf("round-trip mismatch: got %+v want %+v", out, in)
	}
}

func TestUnmarshalByeReturnsTypedErrorOnGarbage(t *testing.T) {
	_, err := UnmarshalBye([]byte("not json"))
	if err == nil {
		t.Fatal("expected error on garbage payload")
	}
	errutil.AssertErrorCode(t, err, "CLUSTER_UNMARSHAL_BYE_FAILED")
}

func TestProbeReplyPayloadRoundTripsThroughJSON(t *testing.T) {
	in := ProbeReplyPayload{
		MemberID:            MemberID("01HEXAMPLE"),
		LastInvalidationSeq: 7,
	}
	b, err := MarshalProbeReply(in)
	if err != nil {
		t.Fatalf("MarshalProbeReply: %v", err)
	}
	out, err := UnmarshalProbeReply(b)
	if err != nil {
		t.Fatalf("UnmarshalProbeReply: %v", err)
	}
	if out != in {
		t.Errorf("round-trip mismatch: got %+v want %+v", out, in)
	}
}

func TestUnmarshalProbeReplyReturnsTypedErrorOnGarbage(t *testing.T) {
	_, err := UnmarshalProbeReply([]byte("not json"))
	if err == nil {
		t.Fatal("expected error on garbage payload")
	}
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
	if err != nil {
		t.Fatalf("MarshalPoison: %v", err)
	}
	out, err := UnmarshalPoison(b)
	if err != nil {
		t.Fatalf("UnmarshalPoison: %v", err)
	}
	if out != in {
		t.Errorf("round-trip mismatch: got %+v want %+v", out, in)
	}
}

func TestUnmarshalPoisonReturnsTypedErrorOnGarbage(t *testing.T) {
	_, err := UnmarshalPoison([]byte("not json"))
	if err == nil {
		t.Fatal("expected error on garbage payload")
	}
	errutil.AssertErrorCode(t, err, "CLUSTER_UNMARSHAL_POISON_FAILED")
}

func TestSubjectAliveWildcardProducesClusterIdNamespacedSubject(t *testing.T) {
	got := SubjectAliveWildcard("test-game")
	want := "internal.test-game.member.alive.>"
	if got != want {
		t.Errorf("SubjectAliveWildcard = %q; want %q", got, want)
	}
}

func TestSubjectByeProducesClusterIdNamespacedSubject(t *testing.T) {
	got := SubjectBye("test-game", MemberID("01HEXAMPLE"))
	want := "internal.test-game.member.bye.01HEXAMPLE"
	if got != want {
		t.Errorf("SubjectBye = %q; want %q", got, want)
	}
}

func TestSubjectByeWildcardProducesClusterIdNamespacedSubject(t *testing.T) {
	got := SubjectByeWildcard("test-game")
	want := "internal.test-game.member.bye.>"
	if got != want {
		t.Errorf("SubjectByeWildcard = %q; want %q", got, want)
	}
}

func TestSubjectProbeProducesClusterIdNamespacedSubject(t *testing.T) {
	got := SubjectProbe("test-game", MemberID("01HEXAMPLE"))
	want := "internal.test-game.member.probe.01HEXAMPLE"
	if got != want {
		t.Errorf("SubjectProbe = %q; want %q", got, want)
	}
}

func TestSubjectProbeSelfMatchesSubjectProbe(t *testing.T) {
	self := MemberID("01HSELF")
	if got, want := SubjectProbeSelf("test-game", self), SubjectProbe("test-game", self); got != want {
		t.Errorf("SubjectProbeSelf = %q; want %q (must match SubjectProbe to subscribe to own probe inbox)", got, want)
	}
}

func TestSubjectPoisonSelfMatchesSubjectPoison(t *testing.T) {
	self := MemberID("01HSELF")
	if got, want := SubjectPoisonSelf("test-game", self), SubjectPoison("test-game", self); got != want {
		t.Errorf("SubjectPoisonSelf = %q; want %q (must match SubjectPoison to subscribe to own pill inbox)", got, want)
	}
}
