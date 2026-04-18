// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"
	"strings"

	"github.com/samber/oops"
	lua "github.com/yuin/gopher-lua"
)

//nolint:gocritic // L is the idiomatic gopher-lua state name used throughout this package.
func (h *Host) invoke(parentCtx context.Context, L *lua.LState, plugin, handler string, p lua.P, args ...lua.LValue) error {
	var ctx context.Context
	var cancel context.CancelFunc
	if h.cpuTimeout > 0 {
		ctx, cancel = context.WithTimeout(parentCtx, h.cpuTimeout)
	} else {
		ctx, cancel = context.WithCancel(parentCtx)
	}
	defer cancel()
	L.SetContext(ctx)

	done := make(chan error, 1)
	go func() {
		done <- L.CallByParam(p, args...)
	}()

	select {
	case err := <-done:
		recordInvocationOutcome(plugin, handler, classifyError(err))
		return err
	case <-ctx.Done():
		// Wait for the goroutine to drain. This is bounded:
		// - pure Lua: gopher-lua's per-instruction ctx check fires
		//   L.RaiseError on the next instruction (microseconds);
		// - hostfunc: bounded by the hostfunc audit invariant that every
		//   registered hostfunc either is O(1) or respects L.Context().
		// Waiting here ensures the state is no longer in use when the
		// caller's defer L.Close() runs, which is essential for race
		// detector cleanliness and for correct state lifecycle.
		<-done
		recordInvocationOutcome(plugin, handler, outcomeTimeout)
		return oops.Code("PLUGIN_LUA_TIMEOUT").
			With("plugin", plugin).
			With("handler", handler).
			With("timeout", h.cpuTimeout).
			Wrap(ctx.Err())
	}
}

// classifyError maps a CallByParam error to an outcome label for metrics.
// gopher-lua's registry-overflow panic (caught by Protect=true) contains
// the substring "registry overflow" in its message; any other non-nil
// error is treated as a normal Lua-level error.
func classifyError(err error) string {
	if err == nil {
		return outcomeSuccess
	}
	// Match gopher-lua's specific panic text for RegistryMaxSize overflow
	// rather than any error mentioning "registry" (which would misattribute
	// legitimate plugin errors like error("registry lookup failed")).
	if strings.Contains(err.Error(), "registry overflow") {
		return outcomeRegistryFull
	}
	return outcomeError
}
