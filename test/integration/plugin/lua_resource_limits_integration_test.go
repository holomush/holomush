// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugin_test

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// loadTestPlugin constructs a minimal Host and loads a plugin from the
// testdata directory. Used by both scenario tests.
func loadTestPlugin(t *testing.T, dir, name string, opts ...pluginlua.HostOption) *pluginlua.Host {
	t.Helper()
	h := pluginlua.NewHost(opts...)
	t.Cleanup(func() { _ = h.Close(context.Background()) })

	manifest := &plugins.Manifest{
		Name:    name,
		Version: "0.0.1",
		Type:    plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{
			Entry: "plugin.lua",
		},
	}
	err := h.Load(context.Background(), manifest, dir)
	require.NoError(t, err, "load %s", name)
	return h
}

func TestLuaPluginCPUBombDoesNotHangDispatcher(t *testing.T) {
	h := loadTestPlugin(t, "testdata/lua/cpu_bomb", "cpu-bomb",
		pluginlua.WithCPUTimeout(200*time.Millisecond),
	)

	start := time.Now()
	_, err := h.DeliverEvent(context.Background(), "cpu-bomb", pluginsdk.Event{
		ID:     "e1",
		Stream: "test",
		Type:   "test_event",
	})
	elapsed := time.Since(start)

	assert.Error(t, err, "cpu_bomb must surface a timeout error")
	assert.Less(t, elapsed, 1*time.Second,
		"dispatcher must return within CPU timeout + generous buffer")

	// Verify the dispatcher remains responsive: a second event should return
	// in the same timeout window.
	start = time.Now()
	_, err = h.DeliverEvent(context.Background(), "cpu-bomb", pluginsdk.Event{
		ID:     "e2",
		Stream: "test",
		Type:   "test_event",
	})
	secondElapsed := time.Since(start)
	assert.Error(t, err)
	assert.Less(t, secondElapsed, 1*time.Second,
		"second event must also return within the timeout (dispatcher not hung)")
}

func TestLuaPluginMemoryBombDoesNotOOM(t *testing.T) {
	// Note: RegistryMaxSize < RegistrySize (default 5120) is silently
	// zeroed by gopher-lua. A table-allocation bomb does NOT stress the
	// registry — tables grow on the heap, are popped from the value
	// stack, and never hit the cap. The reliable trigger is a deeply
	// recursive function with many locals per frame (see state_test
	// TestNewStateRegistryMaxSizeApplied, which uses this same shape at
	// RegistryMaxSize=1024). Use a small cap here so recursion + 8
	// locals per frame overruns the value stack quickly.
	h := loadTestPlugin(t, "testdata/lua/memory_bomb", "memory-bomb",
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

	assert.Error(t, err, "memory_bomb must surface a registry-overflow error")

	// Delta should be bounded. Allow 50MB of headroom for test
	// infrastructure and any transient allocations during the bomb
	// before gopher-lua catches it.
	const maxDelta = 50 * 1024 * 1024
	delta := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	assert.Less(t, delta, int64(maxDelta),
		"memory delta after bomb+GC must be bounded; got %d bytes", delta)
}
