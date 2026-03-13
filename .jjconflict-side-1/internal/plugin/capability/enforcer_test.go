// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package capability_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/plugin/capability"
)

func TestCapabilityEnforcer_Check(t *testing.T) {
	// Tests use gobwas/glob with '.' as separator.
	// Key semantics:
	//   - '*' matches exactly one segment (does not cross '.')
	//   - '**' matches one or more segments when trailing (e.g., "world.**")
	//   - '**' matches zero or more segments at root or in middle position
	tests := []struct {
		name       string
		grants     []string
		capability string
		want       bool
	}{
		// === Exact match ===
		{
			name:       "exact match",
			grants:     []string{"world.read.location"},
			capability: "world.read.location",
			want:       true,
		},
		{
			name:       "exact match single segment",
			grants:     []string{"world"},
			capability: "world",
			want:       true,
		},

		// === Single-segment wildcard (*) ===
		{
			name:       "single wildcard matches direct child",
			grants:     []string{"world.read.*"},
			capability: "world.read.location",
			want:       true,
		},
		{
			name:       "single wildcard does NOT match nested (key semantic)",
			grants:     []string{"world.read.*"},
			capability: "world.read.character.name",
			want:       false, // '*' only matches one segment
		},
		{
			name:       "single wildcard at root matches single segment",
			grants:     []string{"*"},
			capability: "world",
			want:       true,
		},
		{
			name:       "single wildcard at root does NOT match multi-segment",
			grants:     []string{"*"},
			capability: "world.read",
			want:       false,
		},

		// === Super-wildcard (**) - matches all descendants ===
		{
			name:       "super wildcard matches direct child",
			grants:     []string{"world.read.**"},
			capability: "world.read.location",
			want:       true,
		},
		{
			name:       "super wildcard matches nested descendants",
			grants:     []string{"world.read.**"},
			capability: "world.read.character.name",
			want:       true,
		},
		{
			name:       "super wildcard matches deeply nested",
			grants:     []string{"world.**"},
			capability: "world.read.character.name.first",
			want:       true,
		},
		{
			name:       "root super wildcard matches everything",
			grants:     []string{"**"},
			capability: "world.read.location",
			want:       true,
		},
		{
			name:       "root super wildcard matches single segment",
			grants:     []string{"**"},
			capability: "world",
			want:       true,
		},

		// === Trailing ** requires at least one segment ===
		{
			name:       "trailing super wildcard does NOT match prefix itself",
			grants:     []string{"world.read.**"},
			capability: "world.read",
			want:       false, // trailing ** requires at least one segment after prefix
		},
		{
			name:       "trailing super wildcard at single segment does NOT match that segment",
			grants:     []string{"world.**"},
			capability: "world",
			want:       false, // trailing ** requires at least one segment after prefix
		},

		// === Middle ** position (zero or more segments) ===
		{
			name:       "middle super wildcard matches zero segments",
			grants:     []string{"a.**.b"},
			capability: "a.b",
			want:       true, // ** in middle matches zero segments
		},
		{
			name:       "middle super wildcard matches one segment",
			grants:     []string{"a.**.b"},
			capability: "a.x.b",
			want:       true,
		},
		{
			name:       "middle super wildcard matches multiple segments",
			grants:     []string{"a.**.b"},
			capability: "a.x.y.z.b",
			want:       true,
		},
		{
			name:       "middle super wildcard in realistic capability",
			grants:     []string{"world.**.location"},
			capability: "world.read.character.location",
			want:       true,
		},

		// === Multiple grants ===
		{
			name:       "multiple grants second matches",
			grants:     []string{"other.capability", "world.read.location"},
			capability: "world.read.location",
			want:       true,
		},
		{
			name:       "multiple grants wildcard matches",
			grants:     []string{"events.*", "world.read.**"},
			capability: "world.read.character.name",
			want:       true,
		},

		// === Negative cases ===
		{
			name:       "no match returns false",
			grants:     []string{"world.read.character"},
			capability: "world.read.location",
			want:       false,
		},
		{
			name:       "empty grants returns false",
			grants:     []string{},
			capability: "world.read.location",
			want:       false,
		},
		{
			name:       "partial match not allowed",
			grants:     []string{"world.read"},
			capability: "world.read.location",
			want:       false,
		},
		{
			name:       "similar prefix boundary - single wildcard respects segments",
			grants:     []string{"world.read.*"},
			capability: "world.readonly",
			want:       false, // 'world.read.*' != 'world.readonly'
		},

		// === Boundary cases ===
		{
			name:       "empty capability returns false",
			grants:     []string{"world.read.*"},
			capability: "",
			want:       false,
		},
		{
			name:       "super wildcard does not match empty capability",
			grants:     []string{"**"},
			capability: "",
			want:       false,
		},
		{
			name:       "nil grants slice",
			grants:     nil,
			capability: "world.read.location",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := capability.NewEnforcer()
			err := e.SetGrants("test-plugin", tt.grants)
			require.NoError(t, err, "SetGrants() failed")

			got := e.Check("test-plugin", tt.capability)
			assert.Equal(t, tt.want, got, "Check() mismatch")
		})
	}
}

func TestCapabilityEnforcer_Check_UnknownPlugin(t *testing.T) {
	e := capability.NewEnforcer()
	assert.False(t, e.Check("unknown", "any.capability"), "Check() should return false for unknown plugin")
}

func TestCapabilityEnforcer_SetGrants_Overwrite(t *testing.T) {
	e := capability.NewEnforcer()

	// Set initial grants
	err := e.SetGrants("plugin", []string{"world.read.*"})
	require.NoError(t, err, "SetGrants() failed")
	assert.True(t, e.Check("plugin", "world.read.location"), "initial grant should work")

	// Overwrite with different grants
	err = e.SetGrants("plugin", []string{"events.emit.*"})
	require.NoError(t, err, "SetGrants() failed")
	assert.False(t, e.Check("plugin", "world.read.location"), "old grant should no longer work after overwrite")
	assert.True(t, e.Check("plugin", "events.emit.location"), "new grant should work after overwrite")
}

func TestCapabilityEnforcer_SetGrants_DefensiveCopy(t *testing.T) {
	e := capability.NewEnforcer()

	grants := []string{"world.read.*"}
	err := e.SetGrants("plugin", grants)
	require.NoError(t, err, "SetGrants() failed")

	// Mutate the original slice
	grants[0] = "events.emit.*"

	// The enforcer should not be affected
	assert.True(t, e.Check("plugin", "world.read.location"), "enforcer should have copied the slice, not aliased it")
}

func TestCapabilityEnforcer_ZeroValue(t *testing.T) {
	var e capability.Enforcer

	// Zero value should not panic and should return false
	assert.False(t, e.Check("plugin", "any.capability"), "zero value Check() should return false")

	// SetGrants on zero value should work
	err := e.SetGrants("plugin", []string{"world.read.*"})
	require.NoError(t, err, "SetGrants() failed")
	assert.True(t, e.Check("plugin", "world.read.location"), "Check() should work after SetGrants on zero value")
}

func TestCapabilityEnforcer_ConcurrentAccess(t *testing.T) {
	e := capability.NewEnforcer()
	var wg sync.WaitGroup
	var successCount int32

	// Collect errors from goroutines (t.Errorf is not safe in goroutines)
	var errMu sync.Mutex
	var errs []string

	// Concurrent writers
	for i := range 10 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			plugin := fmt.Sprintf("plugin-%d", n)
			if err := e.SetGrants(plugin, []string{"world.read.*"}); err != nil {
				errMu.Lock()
				errs = append(errs, fmt.Sprintf("SetGrants failed for %s: %v", plugin, err))
				errMu.Unlock()
			}
		}(i)
	}

	// Concurrent readers
	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			plugin := fmt.Sprintf("plugin-%d", n%10)
			if e.Check(plugin, "world.read.location") {
				atomic.AddInt32(&successCount, 1)
			}
		}(i)
	}

	wg.Wait()

	// Report any errors after goroutines complete
	for _, errMsg := range errs {
		t.Error(errMsg)
	}

	// At least some checks should have succeeded (race means not all)
	assert.NotZero(t, successCount, "expected at least some successful checks")
}

func TestCapabilityEnforcer_SetGrants_RejectsEmptyPluginName(t *testing.T) {
	e := capability.NewEnforcer()
	err := e.SetGrants("", []string{"world.read.*"})
	assert.Error(t, err, "SetGrants() should reject empty plugin name")
}

func TestCapabilityEnforcer_IsRegistered(t *testing.T) {
	e := capability.NewEnforcer()

	// Unknown plugin should not be registered
	assert.False(t, e.IsRegistered("unknown"), "IsRegistered() should return false for unknown plugin")

	// After SetGrants, plugin should be registered
	err := e.SetGrants("my-plugin", []string{"world.read.*"})
	require.NoError(t, err, "SetGrants() failed")
	assert.True(t, e.IsRegistered("my-plugin"), "IsRegistered() should return true after SetGrants")

	// Other plugins should still not be registered
	assert.False(t, e.IsRegistered("other-plugin"), "IsRegistered() should return false for other plugins")
}

func TestCapabilityEnforcer_IsRegistered_ZeroValue(t *testing.T) {
	var e capability.Enforcer
	assert.False(t, e.IsRegistered("any"), "zero value IsRegistered() should return false")
}

func TestCapabilityEnforcer_PluginIsolation(t *testing.T) {
	e := capability.NewEnforcer()
	err := e.SetGrants("plugin-a", []string{"world.read.*"})
	require.NoError(t, err, "SetGrants() failed")
	err = e.SetGrants("plugin-b", []string{"events.emit.*"})
	require.NoError(t, err, "SetGrants() failed")

	assert.False(t, e.Check("plugin-a", "events.emit.foo"), "plugin-a should not have plugin-b's grants")
	assert.False(t, e.Check("plugin-b", "world.read.foo"), "plugin-b should not have plugin-a's grants")
}

func TestCapabilityEnforcer_Check_SimilarPrefixBoundary(t *testing.T) {
	e := capability.NewEnforcer()
	err := e.SetGrants("plugin", []string{"world.read.*"})
	require.NoError(t, err, "SetGrants() failed")

	// world.read.* should NOT match world.readonly.location
	// This tests that prefix matching respects segment boundaries
	assert.False(t, e.Check("plugin", "world.readonly.location"), "world.read.* should not match world.readonly.location")

	// But should match world.read.location
	assert.True(t, e.Check("plugin", "world.read.location"), "world.read.* should match world.read.location")
}

func TestCapabilityEnforcer_SetGrants_PatternValidation(t *testing.T) {
	// Pattern validation uses gobwas/glob with '.' as separator.
	// Most patterns are valid; only empty strings are rejected by us.
	// Invalid glob syntax is rejected by gobwas/glob.Compile.
	tests := []struct {
		name    string
		pattern string
		wantErr bool
	}{
		// Valid patterns - all of these work with gobwas/glob
		{name: "exact capability", pattern: "world.read.location", wantErr: false},
		{name: "single segment wildcard", pattern: "world.read.*", wantErr: false},
		{name: "super wildcard", pattern: "world.read.**", wantErr: false},
		{name: "root single wildcard", pattern: "*", wantErr: false},
		{name: "root super wildcard", pattern: "**", wantErr: false},
		{name: "single segment", pattern: "world", wantErr: false},
		{name: "middle wildcard", pattern: "world.*.read", wantErr: false},
		{name: "multiple wildcards", pattern: "world.*.read.*", wantErr: false},
		{name: "trailing dot with wildcard", pattern: "world.read.*", wantErr: false},

		// Invalid patterns
		{name: "empty string", pattern: "", wantErr: true},
		{name: "unclosed bracket", pattern: "world.[read", wantErr: true}, // invalid glob syntax
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := capability.NewEnforcer()
			err := e.SetGrants("plugin", []string{tt.pattern})
			if tt.wantErr {
				assert.Error(t, err, "SetGrants() should fail for pattern %q", tt.pattern)
			} else {
				assert.NoError(t, err, "SetGrants() should succeed for pattern %q", tt.pattern)
			}
		})
	}
}

func TestCapabilityEnforcer_SetGrants_MultiplePatterns_OneInvalid(t *testing.T) {
	e := capability.NewEnforcer()
	// If one pattern is invalid, the whole call should fail
	err := e.SetGrants("plugin", []string{"world.read.*", "", "events.emit.*"})
	assert.Error(t, err, "SetGrants() should fail if any pattern is invalid")

	// Plugin should not be registered after failed SetGrants
	assert.False(t, e.IsRegistered("plugin"), "plugin should not be registered after failed SetGrants")
}

func TestCapabilityEnforcer_IsRegistered_EmptyPluginName(t *testing.T) {
	e := capability.NewEnforcer()
	err := e.SetGrants("valid-plugin", []string{"world.read.*"})
	require.NoError(t, err, "SetGrants() failed")

	// Empty plugin name should return false
	assert.False(t, e.IsRegistered(""), "IsRegistered() should return false for empty plugin name")
}

func TestCapabilityEnforcer_Check_EmptyPluginName(t *testing.T) {
	e := capability.NewEnforcer()
	err := e.SetGrants("real-plugin", []string{"world.read.*"})
	require.NoError(t, err, "SetGrants() failed")

	// Empty plugin name should return false (deny by default)
	assert.False(t, e.Check("", "world.read.location"), "Check() should return false for empty plugin name")
}

func TestCapabilityEnforcer_RemoveGrants(t *testing.T) {
	e := capability.NewEnforcer()

	// Setup: register a plugin
	err := e.SetGrants("plugin", []string{"world.read.*"})
	require.NoError(t, err, "SetGrants() failed")
	require.True(t, e.IsRegistered("plugin"), "plugin should be registered before removal")
	require.True(t, e.Check("plugin", "world.read.location"), "plugin should have grant before removal")

	// Remove grants
	err = e.RemoveGrants("plugin")
	require.NoError(t, err, "RemoveGrants() failed")

	// Verify plugin is no longer registered
	assert.False(t, e.IsRegistered("plugin"), "plugin should not be registered after RemoveGrants")
	assert.False(t, e.Check("plugin", "world.read.location"), "plugin should not have grants after RemoveGrants")
}

func TestCapabilityEnforcer_RemoveGrants_UnknownPlugin(t *testing.T) {
	e := capability.NewEnforcer()
	// Should not panic or error for unknown plugin
	err := e.RemoveGrants("unknown")
	assert.NoError(t, err, "RemoveGrants() for unknown plugin should not error")
	// Verify enforcer still works after removing unknown plugin
	assert.False(t, e.IsRegistered("unknown"), "unknown plugin should not be registered")
}

func TestCapabilityEnforcer_RemoveGrants_ZeroValue(t *testing.T) {
	var e capability.Enforcer
	// Should not panic or error on zero value
	err := e.RemoveGrants("any")
	assert.NoError(t, err, "RemoveGrants() on zero value should not error")
	// Verify enforcer still works after operation on zero value
	assert.False(t, e.IsRegistered("any"), "plugin should not be registered on zero value")
}

func TestCapabilityEnforcer_GetGrants(t *testing.T) {
	e := capability.NewEnforcer()
	grants := []string{"world.read.*", "events.emit.*"}
	err := e.SetGrants("plugin", grants)
	require.NoError(t, err, "SetGrants() failed")

	got := e.GetGrants("plugin")
	require.Len(t, got, len(grants), "GetGrants() returned wrong number of grants")
	for i, want := range grants {
		assert.Equal(t, want, got[i], "GetGrants()[%d] mismatch", i)
	}
}

func TestCapabilityEnforcer_GetGrants_UnknownPlugin(t *testing.T) {
	e := capability.NewEnforcer()
	got := e.GetGrants("unknown")
	assert.Nil(t, got, "GetGrants() for unknown plugin should return nil")
}

func TestCapabilityEnforcer_GetGrants_ZeroValue(t *testing.T) {
	var e capability.Enforcer
	got := e.GetGrants("any")
	assert.Nil(t, got, "GetGrants() on zero value should return nil")
}

func TestCapabilityEnforcer_GetGrants_DefensiveCopy(t *testing.T) {
	e := capability.NewEnforcer()
	err := e.SetGrants("plugin", []string{"world.read.*"})
	require.NoError(t, err, "SetGrants() failed")

	// Get grants and mutate the returned slice
	got := e.GetGrants("plugin")
	got[0] = "mutated"

	// Enforcer should not be affected
	assert.True(t, e.Check("plugin", "world.read.location"), "GetGrants() should return a defensive copy")
}

func TestCapabilityEnforcer_RemoveGrants_EmptyPluginName(t *testing.T) {
	e := capability.NewEnforcer()
	err := e.SetGrants("valid-plugin", []string{"world.read.*"})
	require.NoError(t, err, "SetGrants() failed")

	// Removing empty plugin name should return error for consistency with SetGrants
	err = e.RemoveGrants("")
	assert.Error(t, err, "RemoveGrants(\"\") should return error")

	// Valid plugin should still be registered
	assert.True(t, e.IsRegistered("valid-plugin"), "valid-plugin should still be registered after removing empty string")
}

func TestCapabilityEnforcer_GetGrants_EmptyPluginName(t *testing.T) {
	e := capability.NewEnforcer()
	err := e.SetGrants("valid-plugin", []string{"world.read.*"})
	require.NoError(t, err, "SetGrants() failed")

	// Getting grants for empty plugin name should return nil
	got := e.GetGrants("")
	assert.Nil(t, got, "GetGrants(\"\") should return nil")
}

func TestCapabilityEnforcer_ConcurrentAccess_AllMethods(t *testing.T) {
	e := capability.NewEnforcer()
	var wg sync.WaitGroup

	// Pre-register some plugins
	for i := range 5 {
		plugin := fmt.Sprintf("plugin-%d", i)
		err := e.SetGrants(plugin, []string{"world.read.*"})
		require.NoError(t, err, "SetGrants() failed")
	}

	// Concurrent SetGrants
	for i := range 10 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			plugin := fmt.Sprintf("new-plugin-%d", n)
			_ = e.SetGrants(plugin, []string{"events.emit.*"})
		}(i)
	}

	// Concurrent Check
	for i := range 50 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			plugin := fmt.Sprintf("plugin-%d", n%5)
			_ = e.Check(plugin, "world.read.location")
		}(i)
	}

	// Concurrent IsRegistered
	for i := range 20 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			plugin := fmt.Sprintf("plugin-%d", n%5)
			_ = e.IsRegistered(plugin)
		}(i)
	}

	// Concurrent GetGrants
	for i := range 20 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			plugin := fmt.Sprintf("plugin-%d", n%5)
			_ = e.GetGrants(plugin)
		}(i)
	}

	// Concurrent RemoveGrants
	for i := range 5 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			plugin := fmt.Sprintf("plugin-%d", n)
			_ = e.RemoveGrants(plugin)
		}(i)
	}

	// Concurrent ListPlugins
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = e.ListPlugins()
		}()
	}

	wg.Wait()
	// If we get here without race detector errors, the test passes
}

func FuzzCapabilityEnforcer_GetGrants(f *testing.F) {
	// Seed corpus with interesting plugin names
	f.Add("plugin")
	f.Add("")
	f.Add("a")
	f.Add("plugin-with-dashes")
	f.Add("plugin.with.dots")
	f.Add("plugin_with_underscores")
	f.Add("UPPERCASE")
	f.Add("mixed-CASE_123")
	f.Add("unicode-плагін")
	f.Add(string([]byte{0x00, 0x01, 0x02})) // binary data

	f.Fuzz(func(t *testing.T, pluginName string) {
		e := capability.NewEnforcer()

		// GetGrants on unknown plugin should return nil, never panic
		got := e.GetGrants(pluginName)
		assert.Nil(t, got, "GetGrants(%q) on empty enforcer should return nil", pluginName)

		// If plugin name is valid for SetGrants, test round-trip
		if pluginName != "" {
			grants := []string{"world.read.*"}
			if err := e.SetGrants(pluginName, grants); err != nil {
				// SetGrants doesn't validate plugin names beyond empty, so this shouldn't fail
				t.Errorf("SetGrants(%q) failed unexpectedly: %v", pluginName, err)
				return
			}

			got = e.GetGrants(pluginName)
			require.Len(t, got, 1, "GetGrants(%q) returned wrong length", pluginName)
			assert.Equal(t, "world.read.*", got[0], "GetGrants(%q) returned wrong value", pluginName)
		}
	})
}

func FuzzCapabilityEnforcer_RemoveGrants(f *testing.F) {
	// Seed corpus
	f.Add("plugin")
	f.Add("")
	f.Add("unknown")
	f.Add("unicode-плагін")

	f.Fuzz(func(t *testing.T, pluginName string) {
		e := capability.NewEnforcer()

		// RemoveGrants should never panic; returns error only for empty plugin name
		err := e.RemoveGrants(pluginName)
		if pluginName == "" {
			assert.Error(t, err, "RemoveGrants(\"\") should return error")
			return // Empty plugin name is not valid, skip remaining tests
		}
		assert.NoError(t, err, "RemoveGrants(%q) failed unexpectedly", pluginName)

		// After removal, plugin should not be registered
		assert.False(t, e.IsRegistered(pluginName), "IsRegistered(%q) after RemoveGrants should be false", pluginName)

		// Test removal of registered plugin
		err = e.SetGrants(pluginName, []string{"world.read.*"})
		require.NoError(t, err, "SetGrants(%q) failed", pluginName)
		assert.True(t, e.IsRegistered(pluginName), "IsRegistered(%q) after SetGrants should be true", pluginName)

		err = e.RemoveGrants(pluginName)
		require.NoError(t, err, "RemoveGrants(%q) failed", pluginName)

		assert.False(t, e.IsRegistered(pluginName), "IsRegistered(%q) after RemoveGrants should be false", pluginName)
	})
}

func FuzzCapabilityEnforcer_Check(f *testing.F) {
	// Seed corpus with interesting plugin/capability combinations
	f.Add("plugin", "world.read.location")
	f.Add("", "capability")
	f.Add("plugin", "")
	f.Add("", "")
	f.Add("plugin", "world.read.character.name")
	f.Add("unicode-плагін", "world.читання.локація")

	f.Fuzz(func(t *testing.T, pluginName, capabilityName string) {
		e := capability.NewEnforcer()

		// Check on empty enforcer should never panic
		got := e.Check(pluginName, capabilityName)

		// Empty plugin or capability should always return false
		if pluginName == "" || capabilityName == "" {
			assert.False(t, got, "Check(%q, %q) should be false for empty input", pluginName, capabilityName)
			return
		}

		// Unknown plugin should return false
		assert.False(t, got, "Check(%q, %q) should be false for unknown plugin", pluginName, capabilityName)

		// Register plugin with super wildcard and verify Check returns true
		if err := e.SetGrants(pluginName, []string{"**"}); err != nil {
			// SetGrants might fail for unusual plugin names, that's OK
			return
		}

		// With ** grant, any non-empty capability should return true
		got = e.Check(pluginName, capabilityName)
		assert.True(t, got, "Check(%q, %q) should be true with ** grant", pluginName, capabilityName)
	})
}

func TestCapabilityEnforcer_ListPlugins(t *testing.T) {
	e := capability.NewEnforcer()

	// Empty enforcer should return empty slice
	got := e.ListPlugins()
	assert.Empty(t, got, "ListPlugins() on empty enforcer should return empty slice")

	// Register some plugins
	err := e.SetGrants("plugin-a", []string{"world.read.*"})
	require.NoError(t, err, "SetGrants() failed")
	err = e.SetGrants("plugin-b", []string{"events.emit.*"})
	require.NoError(t, err, "SetGrants() failed")

	got = e.ListPlugins()
	assert.Len(t, got, 2, "ListPlugins() returned wrong number of plugins")

	// Check both plugins are present (order not guaranteed)
	assert.Contains(t, got, "plugin-a", "ListPlugins() should contain plugin-a")
	assert.Contains(t, got, "plugin-b", "ListPlugins() should contain plugin-b")
}

func TestCapabilityEnforcer_ListPlugins_ZeroValue(t *testing.T) {
	var e capability.Enforcer
	got := e.ListPlugins()
	assert.NotNil(t, got, "ListPlugins() on zero value should return empty slice, not nil")
	assert.Empty(t, got, "ListPlugins() on zero value should return empty slice")
}

func TestCapabilityEnforcer_ListPlugins_AfterRemove(t *testing.T) {
	e := capability.NewEnforcer()

	err := e.SetGrants("plugin-a", []string{"world.read.*"})
	require.NoError(t, err, "SetGrants() failed")
	err = e.SetGrants("plugin-b", []string{"events.emit.*"})
	require.NoError(t, err, "SetGrants() failed")

	// Remove one plugin
	err = e.RemoveGrants("plugin-a")
	require.NoError(t, err, "RemoveGrants() failed")

	got := e.ListPlugins()
	assert.Len(t, got, 1, "ListPlugins() after removal should have 1 plugin")
	assert.Equal(t, "plugin-b", got[0], "ListPlugins() should contain plugin-b after removing plugin-a")
}

func TestCapabilityEnforcer_ListPlugins_DefensiveCopy(t *testing.T) {
	e := capability.NewEnforcer()
	err := e.SetGrants("plugin", []string{"world.read.*"})
	require.NoError(t, err, "SetGrants() failed")

	// Get list and mutate it
	got := e.ListPlugins()
	if len(got) > 0 {
		got[0] = "mutated"
	}

	// Enforcer should not be affected
	assert.True(t, e.IsRegistered("plugin"), "ListPlugins() should return a defensive copy")
}
