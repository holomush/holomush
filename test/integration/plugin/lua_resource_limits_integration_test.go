// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugin_test

import (
	"context"
	"runtime"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// loadLuaResourceLimitsPlugin constructs a minimal Host and loads a plugin
// from the testdata directory. Used by the Lua resource-limits scenarios.
func loadLuaResourceLimitsPlugin(dir, name string, opts ...pluginlua.HostOption) *pluginlua.Host {
	h := pluginlua.NewHost(opts...)
	DeferCleanup(func() { _ = h.Close(context.Background()) })

	manifest := &plugins.Manifest{
		Name:    name,
		Version: "0.0.1",
		Type:    plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{
			Entry: "plugin.lua",
		},
	}
	Expect(h.Load(context.Background(), manifest, dir)).To(Succeed(), "load %s", name)
	return h
}

var _ = Describe("Lua plugin resource limits", func() {
	Describe("CPU bomb", func() {
		It("does not hang the dispatcher and remains responsive to subsequent events", func() {
			h := loadLuaResourceLimitsPlugin("testdata/lua/cpu_bomb", "cpu-bomb",
				pluginlua.WithCPUTimeout(200*time.Millisecond),
			)

			start := time.Now()
			_, err := h.DeliverEvent(context.Background(), "cpu-bomb", pluginsdk.Event{
				ID:     "e1",
				Stream: "test",
				Type:   "test_event",
			})
			elapsed := time.Since(start)

			Expect(err).To(HaveOccurred(), "cpu-bomb must surface a timeout error")
			Expect(elapsed).To(BeNumerically("<", 1*time.Second),
				"dispatcher must return within CPU timeout + generous buffer")

			// Verify the dispatcher remains responsive: a second event should
			// return in the same timeout window.
			start = time.Now()
			_, err = h.DeliverEvent(context.Background(), "cpu-bomb", pluginsdk.Event{
				ID:     "e2",
				Stream: "test",
				Type:   "test_event",
			})
			secondElapsed := time.Since(start)
			Expect(err).To(HaveOccurred())
			Expect(secondElapsed).To(BeNumerically("<", 1*time.Second),
				"second event must also return within the timeout (dispatcher not hung)")
		})
	})

	Describe("memory bomb", func() {
		It("bounds allocation growth via RegistryMaxSize rather than OOM-ing", func() {
			// Note: RegistryMaxSize < RegistrySize (default 5120) is silently
			// zeroed by gopher-lua. A table-allocation bomb does NOT stress
			// the registry — tables grow on the heap, are popped from the
			// value stack, and never hit the cap. The reliable trigger is a
			// deeply recursive function with many locals per frame (see
			// state_test TestNewStateRegistryMaxSizeApplied, which uses the
			// same shape at RegistryMaxSize=1024). Use a small cap here so
			// recursion + 8 locals per frame overruns the value stack
			// quickly.
			h := loadLuaResourceLimitsPlugin("testdata/lua/memory_bomb", "memory-bomb",
				pluginlua.WithCPUTimeout(5*time.Second), // generous — want the registry cap to fire first
				pluginlua.WithStateFactory(pluginlua.NewStateFactory(
					pluginlua.WithRegistryMaxSize(1024),
				)),
			)

			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)

			_, err := h.DeliverEvent(context.Background(), "memory-bomb", pluginsdk.Event{
				ID:     "e1",
				Stream: "test",
				Type:   "test_event",
			})

			runtime.GC()
			runtime.ReadMemStats(&after)

			Expect(err).To(HaveOccurred(), "memory-bomb must surface a registry-overflow error")

			// TotalAlloc is cumulative and monotonic, so delta can't go
			// negative and produce a silent pass. Allow 50MB of headroom
			// for test infrastructure and any transient allocations during
			// the bomb before gopher-lua catches it.
			const maxDelta = uint64(50 * 1024 * 1024)
			delta := after.TotalAlloc - before.TotalAlloc
			Expect(delta).To(BeNumerically("<", maxDelta),
				"allocation delta during bomb must be bounded; got %d bytes", delta)
		})
	})
})
