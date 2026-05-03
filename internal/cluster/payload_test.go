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
