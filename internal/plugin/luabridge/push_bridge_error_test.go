// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package luabridge — internal test for pushBridgeError opacity.
//
// This file is package luabridge (not luabridge_test) so it can call the
// unexported pushBridgeError directly.
package luabridge

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestPushBridgeErrorStripsInnerDetail asserts the opacity property of
// pushBridgeError: the value surfaced to Lua is the gRPC status .Message()
// only — never the fuller err.Error() envelope (e.g. "rpc error: code =
// Internal desc = …") and never any inner detail that may be present in a
// wrapping error.
//
// Non-vacuity: if pushBridgeError were changed to push err.Error() instead of
// status.Convert(err).Message(), both cases would fail — err.Error() returns
// "rpc error: code = Internal desc = internal error" (case 1) and
// "rpc error: code = NotFound desc = scene not found" (case 2), both of which
// contain "rpc error".  Either regression makes the NotContains assertion red.
func TestPushBridgeErrorStripsInnerDetail(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantMsg     string
		notContains []string
	}{
		{
			// Case 1 — plain opaque gRPC status.
			// err.Error() returns "rpc error: code = Internal desc = internal error".
			// pushBridgeError MUST push only "internal error" (the .Message()), never
			// the full envelope.  This is the core opacity assertion.
			name:        "strips rpc-error envelope from a plain status error",
			err:         status.Error(codes.Internal, "internal error"),
			wantMsg:     "internal error",
			notContains: []string{"rpc error", "code = Internal"},
		},
		{
			// Case 2 — opaque message on a status error whose description is a constant.
			// Using a second distinct status error verifies the first case wasn't a
			// coincidence: pushBridgeError always strips the "rpc error: code = X desc
			// = Y" envelope, exposing only the status description string.
			name:        "strips rpc-error envelope from a NotFound status error",
			err:         status.Error(codes.NotFound, "scene not found"),
			wantMsg:     "scene not found",
			notContains: []string{"rpc error", "code = NotFound"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			L := lua.NewState()
			defer L.Close()

			n := pushBridgeError(L, tt.err)

			require.Equal(t, 2, n, "pushBridgeError must return 2 (nil + msg)")

			// Stack position -2 is the first pushed value (lua.LNil).
			first := L.Get(-2)
			assert.Equal(t, lua.LNil, first, "first return value must be nil")

			// Stack position -1 is the second pushed value (the message string).
			msg := L.ToString(-1)
			assert.Equal(t, tt.wantMsg, msg, "message must equal the opaque status message")

			for _, fragment := range tt.notContains {
				assert.NotContains(t, msg, fragment,
					"Lua message must not leak detail: %q found in %q", fragment, msg)
			}
		})
	}
}

// TestPushBridgeErrorNonStatusPassthrough documents that a plain non-status
// error surfaces its raw text as the Lua message.  This is expected: opacity
// relies on gRPC call sites returning sanitised status errors; a bare
// errors.New that reaches pushBridgeError was never sanitised and its text
// passes through as-is (status.Convert wraps it in codes.Unknown).
// This case is informational — it is NOT an opacity failure.
func TestPushBridgeErrorNonStatusPassthrough(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	err := errors.New("plain boom")
	n := pushBridgeError(L, err)

	require.Equal(t, 2, n)
	assert.Equal(t, lua.LNil, L.Get(-2))
	assert.Equal(t, "plain boom", L.ToString(-1))
}

// TestPushBridgeErrorFmtErrfWrappingLeaksFinding documents a FINDING:
// status.Convert DOES find the wrapped status in a fmt.Errorf("%s: %w", secret,
// statusErr) chain via errors.As (grpc-go v1.81.1 status/status.go), but it then
// explicitly sets the resulting Message to err.Error() — the full OUTER string,
// which includes the secret — rather than the inner status's message. So the
// secret becomes the Lua message.
//
// This means call sites MUST NOT use fmt.Errorf to wrap status errors when
// the outer message contains sensitive detail; they must sanitise BEFORE
// calling pushBridgeError (i.e. return a fresh opaque status.Error at the
// gRPC boundary rather than wrapping a status error in an fmt.Errorf).
//
// This test exists to pin the ACTUAL behavior so a future grpc-go upgrade
// that changes status.Convert's unwrapping semantics is immediately visible.
func TestPushBridgeErrorFmtErrfWrappingLeaksFinding(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// Construct a wrapping error whose outer layer contains a secret.
	wrapped := fmt.Errorf("%s: %w", "table users password=hunter2", status.Error(codes.Internal, "internal error"))

	// Verify the FINDING: status.Convert resolves to the outer error's full text.
	msg := status.Convert(wrapped).Message()
	assert.Contains(t, msg, "hunter2",
		"FINDING: status.Convert sets .Message() to the full outer err.Error(); secret leaks: %q", msg)

	// pushBridgeError pushes exactly .Message() — so the same leak reaches Lua.
	n := pushBridgeError(L, wrapped)
	require.Equal(t, 2, n)
	luaMsg := L.ToString(-1)
	assert.Equal(t, msg, luaMsg,
		"pushBridgeError must push exactly status.Convert(err).Message(), even when that leaks wrapping detail")
}
