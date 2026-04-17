// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

// newInvokeTestHost returns a Host configured with a short CPU timeout.
// Tests use this to exercise invoke() without a full plugin stack.
func newInvokeTestHost(t *testing.T, cpuTimeout time.Duration) *Host {
	t.Helper()
	h := &Host{
		factory:    NewStateFactory(),
		cpuTimeout: cpuTimeout,
		plugins:    map[string]*luaPlugin{},
	}
	return h
}

func TestInvokeReturnsCallByParamResultWhenFast(t *testing.T) {
	h := newInvokeTestHost(t, 500*time.Millisecond)
	L, err := h.factory.NewState(context.Background())
	require.NoError(t, err)
	defer L.Close()

	// A Lua function that returns 42.
	require.NoError(t, L.DoString(`function handler() return 42 end`))
	fn := L.GetGlobal("handler")
	require.Equal(t, lua.LTFunction, fn.Type())

	err = h.invoke(context.Background(), L, "test", "on_event", lua.P{
		Fn:      fn,
		NRet:    1,
		Protect: true,
	})
	assert.NoError(t, err)
	assert.Equal(t, lua.LNumber(42), L.Get(-1))
}

func TestInvokeSurfacesCPUTimeoutOnTightLoop(t *testing.T) {
	h := newInvokeTestHost(t, 100*time.Millisecond)
	L, err := h.factory.NewState(context.Background())
	require.NoError(t, err)
	defer L.Close()

	require.NoError(t, L.DoString(`function handler() while true do end end`))
	fn := L.GetGlobal("handler")

	before := testutil.ToFloat64(TimeoutsTotal.WithLabelValues("test", "on_event"))

	start := time.Now()
	err = h.invoke(context.Background(), L, "test", "on_event", lua.P{
		Fn:      fn,
		NRet:    0,
		Protect: true,
	})
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Less(t, elapsed, 400*time.Millisecond,
		"invoke must return within ~timeout (+ jitter budget)")
	assert.Equal(t, before+1, testutil.ToFloat64(TimeoutsTotal.WithLabelValues("test", "on_event")),
		"timeouts_total must increment")
}

func TestInvokeReleasesDispatcherWhenHostFuncBlocks(t *testing.T) {
	h := newInvokeTestHost(t, 100*time.Millisecond)
	L, err := h.factory.NewState(context.Background())
	require.NoError(t, err)
	defer L.Close()

	// Register a context-aware hostfunc that blocks on a channel but also
	// observes L.Context().Done(). This matches the hostfunc audit
	// invariant: every registered hostfunc either is O(1) or respects
	// L.Context() for potentially-blocking work. invoke's ctx.Done branch
	// waits for the goroutine to drain; a well-behaved hostfunc drains
	// promptly when its context is cancelled.
	unblock := make(chan struct{})
	t.Cleanup(func() { close(unblock) })

	L.SetGlobal("blocker", L.NewFunction(func(L *lua.LState) int {
		ctx := L.Context()
		select {
		case <-unblock:
		case <-ctx.Done():
		}
		return 0
	}))
	require.NoError(t, L.DoString(`function handler() blocker() end`))
	fn := L.GetGlobal("handler")

	start := time.Now()
	err = h.invoke(context.Background(), L, "test", "on_event", lua.P{
		Fn:      fn,
		NRet:    0,
		Protect: true,
	})
	elapsed := time.Since(start)

	assert.Error(t, err, "dispatcher must surface timeout error")
	assert.Less(t, elapsed, 400*time.Millisecond,
		"dispatcher must not wait significantly beyond the CPU timeout; the context-aware hostfunc exits promptly on ctx cancellation, letting the goroutine drain")
}

func TestClassifyError(t *testing.T) {
	assert.Equal(t, outcomeSuccess, classifyError(nil))

	registryErr := errors.New("registry size limit reached")
	assert.Equal(t, outcomeRegistryFull, classifyError(registryErr))

	assert.Equal(t, outcomeError, classifyError(errors.New("something else")))
}

func TestClassifyErrorRecognizesWrappedRegistryOverflow(t *testing.T) {
	// gopher-lua's registry overflow message contains "registry" substring.
	wrapped := errors.New("lua: pcall: " + "registry overflow occurred")
	assert.Equal(t, outcomeRegistryFull, classifyError(wrapped))
}

func TestInvokeReturnsLuaErrorAsOutcomeError(t *testing.T) {
	h := newInvokeTestHost(t, 500*time.Millisecond)
	L, err := h.factory.NewState(context.Background())
	require.NoError(t, err)
	defer L.Close()

	require.NoError(t, L.DoString(`function handler() error("boom") end`))
	fn := L.GetGlobal("handler")

	beforeErr := testutil.ToFloat64(InvocationsTotal.WithLabelValues("test", "on_event", outcomeError))

	err = h.invoke(context.Background(), L, "test", "on_event", lua.P{
		Fn:      fn,
		NRet:    0,
		Protect: true,
	})
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "boom"))
	assert.Equal(t, beforeErr+1,
		testutil.ToFloat64(InvocationsTotal.WithLabelValues("test", "on_event", outcomeError)),
		"Lua error must count as outcome=error")
}
