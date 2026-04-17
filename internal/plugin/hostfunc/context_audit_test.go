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
// call returns within 50ms. This locks the hostfunc audit conclusion
// (every registered host function either completes in O(1) time or
// respects L.Context() for any potentially-blocking work) against future
// regressions.
//
// The test is intentionally coarse. It doesn't validate semantics of the
// return value, only that the call returns promptly. Calling a hostfunc
// with zero args will usually raise a Lua argument error (fast path);
// context-respecting hostfuncs that do accept zero args and block will
// exit on L.Context().Done(). Either way, the call returns within 50ms.
// A hostfunc that blocks unboundedly without respecting context will
// trip this test.
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

			assert.Less(t, elapsed, 50*time.Millisecond,
				"hostfunc %s must return within 50ms under a cancelled context; took %s",
				entry.Name, elapsed)
		})
	}
}
