// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	grpcpkg "github.com/holomush/holomush/internal/grpc"
	"github.com/holomush/holomush/internal/session"
	sessionmocks "github.com/holomush/holomush/internal/session/mocks"
)

// TestNewLifecycleHandlerIsConstructibleWithOnlyItsOwnCollaborators is the SC1 /
// D-02 proof for this unit: the handler is built from `package grpc_test` with
// no *CoreServer, no integrationtest harness, and no integration build tag.
func TestNewLifecycleHandlerIsConstructibleWithOnlyItsOwnCollaborators(t *testing.T) {
	t.Parallel()

	h := grpcpkg.NewLifecycleHandler(grpcpkg.LifecycleDeps{
		SessionStore:    sessionmocks.NewMockStore(t),
		DisconnectHooks: []func(session.Info){func(session.Info) {}},
	})

	require.NotNil(t, h)
}

// TestLifecycleHandlerRunDisconnectHooksInvokesEveryHookOnceInRegistrationOrder
// pins the ordering guarantee the plan's prohibition protects. Hooks observe
// session teardown, so reordering them silently changes what each one sees —
// asserting the exact sequence, not mere membership, is what makes that a
// red test rather than a review note (T-8-18).
func TestLifecycleHandlerRunDisconnectHooksInvokesEveryHookOnceInRegistrationOrder(t *testing.T) {
	t.Parallel()

	var order []string
	mark := func(name string) func(session.Info) {
		return func(session.Info) { order = append(order, name) }
	}

	h := grpcpkg.NewLifecycleHandler(grpcpkg.LifecycleDeps{
		DisconnectHooks: []func(session.Info){mark("first"), mark("second"), mark("third")},
	})

	grpcpkg.ExportRunDisconnectHooks(context.Background(), h, session.Info{ID: "sess"})

	assert.Equal(t, []string{"first", "second", "third"}, order,
		"disconnect hooks must run in registration order, exactly once each")
}

// TestLifecycleHandlerRunDisconnectHooksWithNoHooksIsANoOp pins the
// empty-collaborator case: no panic, no work.
func TestLifecycleHandlerRunDisconnectHooksWithNoHooksIsANoOp(t *testing.T) {
	t.Parallel()

	h := grpcpkg.NewLifecycleHandler(grpcpkg.LifecycleDeps{})

	assert.NotPanics(t, func() {
		grpcpkg.ExportRunDisconnectHooks(context.Background(), h, session.Info{ID: "sess"})
	})
}

// TestLifecycleHandlerRunDisconnectHooksContinuesAfterAPanickingHook pins the
// per-hook panic recovery moved with the body: one misbehaving hook must not
// prevent the remaining hooks from observing the teardown.
func TestLifecycleHandlerRunDisconnectHooksContinuesAfterAPanickingHook(t *testing.T) {
	t.Parallel()

	var order []string
	h := grpcpkg.NewLifecycleHandler(grpcpkg.LifecycleDeps{
		DisconnectHooks: []func(session.Info){
			func(session.Info) { order = append(order, "before") },
			func(session.Info) { panic("hook exploded") },
			func(session.Info) { order = append(order, "after") },
		},
	})

	assert.NotPanics(t, func() {
		grpcpkg.ExportRunDisconnectHooks(context.Background(), h, session.Info{ID: "sess"})
	})
	assert.Equal(t, []string{"before", "after"}, order,
		"a panicking hook must be recovered and must not skip its successors")
}
