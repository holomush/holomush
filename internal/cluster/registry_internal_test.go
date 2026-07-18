// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package cluster

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/pkg/errutil"
)

// Compile-time interface check: *registry must satisfy lifecycle.Subsystem.
var _ lifecycle.Subsystem = (*registry)(nil)

// TestNewSubsystemAllowsNilConnAtConstruction proves construction no longer
// eagerly requires a live connection (D-09 / 07-09 item 1) — the nil-Conn
// check moves to Start, where the resolved value is actually needed.
func TestNewSubsystemAllowsNilConnAtConstruction(t *testing.T) {
	t.Parallel()

	deps := Deps{
		// Conn/ConnProvider both left zero.
		Pill: stubPill{},
	}
	cfg := Config{ClusterID: "test-cluster"}

	sub, err := NewSubsystem(cfg, deps)
	require.NoError(t, err)
	require.NotNil(t, sub)
}

// TestStartRejectsTypedNilConn locks Start's typed-nil rejection. After
// holomush-ojw1.3.23 introduced the natsconn.Conn interface, a plain
// `conn == nil` comparison only catches the interface-header nil case,
// missing typed-nil concrete values like (*nats.Conn)(nil) (see
// internal/eventbus/natsconn/natsconn_test.go for the runtime
// demonstration of typed-nil interface semantics).
//
// Without isNilConn's reflect-based check, a caller passing a typed-nil
// Conn would slip past validation and crash on first method call. This
// test asserts Start fails fast with CLUSTER_DEPS_NIL instead — relocated
// from NewSubsystem now that Conn resolution happens at Start (07-09 item 1;
// the nil-Conn check moved out of NewSubsystem into Start, which lives in
// heartbeat.go, not registry.go).
func TestStartRejectsTypedNilConn(t *testing.T) {
	t.Parallel()

	// (*nats.Conn)(nil) satisfies natsconn.Conn structurally but is
	// itself a typed-nil pointer. The interface header is non-nil
	// (carries the type pointer), so `conn == nil` is false here.
	// isNilConn must catch this via reflect.
	var typedNilConn *nats.Conn
	deps := Deps{
		Conn: typedNilConn,
		Pill: stubPill{},
	}
	cfg := Config{ClusterID: "test-cluster"}

	sub, err := NewSubsystem(cfg, deps)
	require.NoError(t, err)

	err = sub.Prepare(context.Background()) // Prepare-only: the nil-Conn check fails before Activate would run
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CLUSTER_DEPS_NIL")
	errutil.AssertErrorContext(t, err, "dep", "Conn")
}

// TestStartRejectsInterfaceNilConn covers the simpler case: a completely
// nil interface (no concrete type at all, and no ConnProvider either). This
// is what the pre-3.23 `== nil` check caught; the new `|| isNilConn(...)`
// clause must not regress this behavior.
func TestStartRejectsInterfaceNilConn(t *testing.T) {
	t.Parallel()

	deps := Deps{
		// Conn/ConnProvider left zero — interface header is nil.
		Pill: stubPill{},
	}
	cfg := Config{ClusterID: "test-cluster"}

	sub, err := NewSubsystem(cfg, deps)
	require.NoError(t, err)

	err = sub.Prepare(context.Background()) // Prepare-only: the nil-Conn check fails before Activate would run
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CLUSTER_DEPS_NIL")
	errutil.AssertErrorContext(t, err, "dep", "Conn")
}

// TestIsNilConnDetectsTypedNilPointer is the unit-level lock for the
// helper. Mirrors the pattern in internal/presence/emitter.go's
// isNilPublisher unit coverage.
func TestIsNilConnDetectsTypedNilPointer(t *testing.T) {
	t.Parallel()

	var typedNilConn *nats.Conn
	assert.True(t, isNilConn(typedNilConn),
		"isNilConn MUST return true for (*nats.Conn)(nil)")
}

// stubPill is a no-op Pill implementation used to satisfy the Deps
// constructor invariant for tests that focus on Conn validation.
type stubPill struct{}

func (stubPill) Trigger(context.Context, PillReason, MemberID) {}
