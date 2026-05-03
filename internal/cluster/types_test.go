// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster

import (
	"testing"
	"time"
)

func TestMemberStatusStringReturnsHumanReadableName(t *testing.T) {
	cases := []struct {
		in   MemberStatus
		want string
	}{
		{StatusUnknown, "unknown"},
		{StatusAlive, "alive"},
		{StatusStale, "stale"},
		{StatusEvicted, "evicted"},
		{StatusPilled, "pilled"},
	}
	for _, tc := range cases {
		if got := tc.in.String(); got != tc.want {
			t.Errorf("MemberStatus(%d).String() = %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestLeaveReasonStringReturnsHumanReadableName(t *testing.T) {
	cases := []struct {
		in   LeaveReason
		want string
	}{
		{LeaveReasonGracefulBye, "graceful_bye"},
		{LeaveReasonHeartbeatTimeout, "heartbeat_timeout"},
		{LeaveReasonPilled, "pilled"},
	}
	for _, tc := range cases {
		if got := tc.in.String(); got != tc.want {
			t.Errorf("LeaveReason(%d).String() = %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestConfigDefaultsAppliesMasterSpecValues(t *testing.T) {
	var cfg Config
	out := cfg.Defaults()
	if out.HeartbeatInterval != 5*time.Second {
		t.Errorf("HeartbeatInterval = %v; want 5s", out.HeartbeatInterval)
	}
	if out.EvictAfterMissed != 3 {
		t.Errorf("EvictAfterMissed = %d; want 3", out.EvictAfterMissed)
	}
	if out.ProbeTimeout != 250*time.Millisecond {
		t.Errorf("ProbeTimeout = %v; want 250ms", out.ProbeTimeout)
	}
	if out.PillRateLimit != 60*time.Second {
		t.Errorf("PillRateLimit = %v; want 60s", out.PillRateLimit)
	}
	if out.SkewWarnThreshold != 30*time.Second {
		t.Errorf("SkewWarnThreshold = %v; want 30s", out.SkewWarnThreshold)
	}
}

func TestConfigDefaultsPreservesNonZeroValues(t *testing.T) {
	cfg := Config{
		HeartbeatInterval: 1 * time.Second,
		EvictAfterMissed:  5,
	}
	out := cfg.Defaults()
	if out.HeartbeatInterval != 1*time.Second {
		t.Errorf("HeartbeatInterval = %v; want 1s (preserved)", out.HeartbeatInterval)
	}
	if out.EvictAfterMissed != 5 {
		t.Errorf("EvictAfterMissed = %d; want 5 (preserved)", out.EvictAfterMissed)
	}
	// Unset fields still defaulted.
	if out.ProbeTimeout != 250*time.Millisecond {
		t.Errorf("ProbeTimeout default missing: %v", out.ProbeTimeout)
	}
}
