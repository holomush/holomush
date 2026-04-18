// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/plugin/hostfunc"
)

// TestHostFuncsRespectContextOnPreCancelledCtx is a meta-test: for every
// host function registered by hostfunc.Functions.Register, invoke it
// under a Lua state whose context is already cancelled, and assert the
// call returns within 250ms.
//
// SCOPE AND LIMITATION (important):
//
// Every audited hostfunc except holomush.new_request_id calls
// L.CheckString(1) (or similar) as its first action, which raises a Lua
// error and returns immediately when called with zero args — BEFORE any
// KV/world/session backend call that would consult L.Context(). Under a
// pre-cancelled context the 250ms threshold is therefore satisfied
// trivially by the argument-check path for nearly all entries.
//
// What this test actually locks:
//   - New hostfuncs that block UNBOUNDEDLY WITHOUT respecting context AND
//     accept zero args will trip this test (the dispatcher would hang
//     waiting for the goroutine under Host.invoke's wait-for-drain).
//   - A hostfunc whose argument validation is itself slow (> 250 ms)
//     will trip this test.
//
// What this test does NOT cover:
//   - A hostfunc that validates args first and then does a blocking call
//     without respecting L.Context(). Such a hostfunc would pass this
//     meta-test but break Host.invoke's bounded-drain guarantee under
//     legitimate traffic.
//
// To close that gap, a future contributor should add a companion subtest
// that wires each blocking-capable hostfunc (kv_*, query_*, etc.) against
// a fake backend which only unblocks on ctx.Done(), invoked with
// valid-shaped arguments. The argument shapes are not uniform across
// hostfuncs (some take strings, some take tables), so this requires
// per-hostfunc test fixtures — deferred until a follow-up.
//
// In the meantime, the invariant is enforced by code review guided by
// site/docs/contributing/hostfunc-context-audit.md.
func TestHostFuncsRespectContextOnPreCancelledCtx(t *testing.T) {
	hf := hostfunc.New(nil)
	require.NotNil(t, hf)

	entries := hf.RegisteredFunctionsForAudit()
	require.NotEmpty(t, entries, "audit list must not be empty")

	for _, entry := range entries {
		entry := entry
		t.Run(entry.Name, func(t *testing.T) {
			L := lua.NewState()
			defer L.Close()

			hf.Register(L, "audit-test-plugin")

			ctx, cancel := context.WithCancel(context.Background())
			cancel() // pre-cancel
			L.SetContext(ctx)

			start := time.Now()
			_ = L.DoString(entry.Name + "()") // ignore the Lua-level error
			elapsed := time.Since(start)

			assert.Less(t, elapsed, 250*time.Millisecond,
				"hostfunc %s must return within 250ms under a cancelled context; took %s",
				entry.Name, elapsed)
		})
	}
}
