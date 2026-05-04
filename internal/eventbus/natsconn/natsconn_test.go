// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package natsconn_test

import (
	"testing"

	"github.com/nats-io/nats.go"

	"github.com/holomush/holomush/internal/eventbus/natsconn"
)

// TestStarConnAssignableToConn pins the structural-typing contract
// between *nats.Conn and natsconn.Conn at the test layer (in addition
// to the compile-time `var _ natsconn.Conn = (*nats.Conn)(nil)` check
// in natsconn.go). If a future nats.go release renames or removes one
// of the methods we depend on, both the compile-time assertion AND
// this test will fail — the test path catches the failure even in
// builds that bypass the package's own compilation (rare but
// possible: build tags, alternate vendor trees).
//
// The test does NOT exercise any methods — it only confirms the
// structural-typing contract. Behavioral tests for the seam live with
// each consumer (cluster, invalidation) where they exercise the real
// (or mock) implementation against the consumer's call patterns.
func TestStarConnAssignableToConn(t *testing.T) {
	t.Parallel()
	// The assignment alone is the assertion: if (*nats.Conn) does not
	// satisfy natsconn.Conn this file won't compile. We keep the
	// (now-typed-nil-bearing) interface variable around so go vet
	// doesn't complain about an unused declaration.
	var c natsconn.Conn = (*nats.Conn)(nil)
	// An interface holding a typed-nil concrete value is itself
	// non-nil at the comparison level (Go interface internals carry
	// the type pointer). That's expected; we assert the runtime
	// shape rather than nil-ness to make the contract explicit.
	if _, ok := c.(*nats.Conn); !ok {
		t.Fatalf("expected interface to carry concrete *nats.Conn, got %T", c)
	}
}
