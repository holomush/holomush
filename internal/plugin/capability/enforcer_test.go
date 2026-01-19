package capability_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/holomush/holomush/internal/plugin/capability"
)

func TestCapabilityEnforcer_Check(t *testing.T) {
	// Tests use gobwas/glob with '.' as separator.
	// Key semantics:
	//   - '*' matches a single segment (does not cross '.')
	//   - '**' matches zero or more segments (crosses '.')
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
			if err := e.SetGrants("test-plugin", tt.grants); err != nil {
				t.Fatalf("SetGrants() failed: %v", err)
			}

			got := e.Check("test-plugin", tt.capability)
			if got != tt.want {
				t.Errorf("Check() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCapabilityEnforcer_Check_UnknownPlugin(t *testing.T) {
	e := capability.NewEnforcer()
	if e.Check("unknown", "any.capability") {
		t.Error("Check() should return false for unknown plugin")
	}
}

func TestCapabilityEnforcer_SetGrants_Overwrite(t *testing.T) {
	e := capability.NewEnforcer()

	// Set initial grants
	if err := e.SetGrants("plugin", []string{"world.read.*"}); err != nil {
		t.Fatalf("SetGrants() failed: %v", err)
	}
	if !e.Check("plugin", "world.read.location") {
		t.Error("initial grant should work")
	}

	// Overwrite with different grants
	if err := e.SetGrants("plugin", []string{"events.emit.*"}); err != nil {
		t.Fatalf("SetGrants() failed: %v", err)
	}
	if e.Check("plugin", "world.read.location") {
		t.Error("old grant should no longer work after overwrite")
	}
	if !e.Check("plugin", "events.emit.location") {
		t.Error("new grant should work after overwrite")
	}
}

func TestCapabilityEnforcer_SetGrants_DefensiveCopy(t *testing.T) {
	e := capability.NewEnforcer()

	grants := []string{"world.read.*"}
	if err := e.SetGrants("plugin", grants); err != nil {
		t.Fatalf("SetGrants() failed: %v", err)
	}

	// Mutate the original slice
	grants[0] = "events.emit.*"

	// The enforcer should not be affected
	if !e.Check("plugin", "world.read.location") {
		t.Error("enforcer should have copied the slice, not aliased it")
	}
}

func TestCapabilityEnforcer_ZeroValue(t *testing.T) {
	var e capability.Enforcer

	// Zero value should not panic and should return false
	if e.Check("plugin", "any.capability") {
		t.Error("zero value Check() should return false")
	}

	// SetGrants on zero value should work
	if err := e.SetGrants("plugin", []string{"world.read.*"}); err != nil {
		t.Fatalf("SetGrants() failed: %v", err)
	}
	if !e.Check("plugin", "world.read.location") {
		t.Error("Check() should work after SetGrants on zero value")
	}
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
	if successCount == 0 {
		t.Error("expected at least some successful checks")
	}
}

func TestCapabilityEnforcer_SetGrants_RejectsEmptyPluginName(t *testing.T) {
	e := capability.NewEnforcer()
	err := e.SetGrants("", []string{"world.read.*"})
	if err == nil {
		t.Error("SetGrants() should reject empty plugin name")
	}
}

func TestCapabilityEnforcer_IsRegistered(t *testing.T) {
	e := capability.NewEnforcer()

	// Unknown plugin should not be registered
	if e.IsRegistered("unknown") {
		t.Error("IsRegistered() should return false for unknown plugin")
	}

	// After SetGrants, plugin should be registered
	if err := e.SetGrants("my-plugin", []string{"world.read.*"}); err != nil {
		t.Fatalf("SetGrants() failed: %v", err)
	}
	if !e.IsRegistered("my-plugin") {
		t.Error("IsRegistered() should return true after SetGrants")
	}

	// Other plugins should still not be registered
	if e.IsRegistered("other-plugin") {
		t.Error("IsRegistered() should return false for other plugins")
	}
}

func TestCapabilityEnforcer_IsRegistered_ZeroValue(t *testing.T) {
	var e capability.Enforcer
	if e.IsRegistered("any") {
		t.Error("zero value IsRegistered() should return false")
	}
}

func TestCapabilityEnforcer_PluginIsolation(t *testing.T) {
	e := capability.NewEnforcer()
	if err := e.SetGrants("plugin-a", []string{"world.read.*"}); err != nil {
		t.Fatalf("SetGrants() failed: %v", err)
	}
	if err := e.SetGrants("plugin-b", []string{"events.emit.*"}); err != nil {
		t.Fatalf("SetGrants() failed: %v", err)
	}

	if e.Check("plugin-a", "events.emit.foo") {
		t.Error("plugin-a should not have plugin-b's grants")
	}
	if e.Check("plugin-b", "world.read.foo") {
		t.Error("plugin-b should not have plugin-a's grants")
	}
}

func TestCapabilityEnforcer_Check_SimilarPrefixBoundary(t *testing.T) {
	e := capability.NewEnforcer()
	if err := e.SetGrants("plugin", []string{"world.read.*"}); err != nil {
		t.Fatalf("SetGrants() failed: %v", err)
	}

	// world.read.* should NOT match world.readonly.location
	// This tests that prefix matching respects segment boundaries
	if e.Check("plugin", "world.readonly.location") {
		t.Error("world.read.* should not match world.readonly.location")
	}

	// But should match world.read.location
	if !e.Check("plugin", "world.read.location") {
		t.Error("world.read.* should match world.read.location")
	}
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
			if (err != nil) != tt.wantErr {
				t.Errorf("SetGrants() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCapabilityEnforcer_SetGrants_MultiplePatterns_OneInvalid(t *testing.T) {
	e := capability.NewEnforcer()
	// If one pattern is invalid, the whole call should fail
	err := e.SetGrants("plugin", []string{"world.read.*", "", "events.emit.*"})
	if err == nil {
		t.Error("SetGrants() should fail if any pattern is invalid")
	}

	// Plugin should not be registered after failed SetGrants
	if e.IsRegistered("plugin") {
		t.Error("plugin should not be registered after failed SetGrants")
	}
}

func TestCapabilityEnforcer_IsRegistered_EmptyPluginName(t *testing.T) {
	e := capability.NewEnforcer()
	if err := e.SetGrants("valid-plugin", []string{"world.read.*"}); err != nil {
		t.Fatalf("SetGrants() failed: %v", err)
	}

	// Empty plugin name should return false
	if e.IsRegistered("") {
		t.Error("IsRegistered() should return false for empty plugin name")
	}
}

func TestCapabilityEnforcer_Check_EmptyPluginName(t *testing.T) {
	e := capability.NewEnforcer()
	if err := e.SetGrants("real-plugin", []string{"world.read.*"}); err != nil {
		t.Fatalf("SetGrants() failed: %v", err)
	}

	// Empty plugin name should return false (deny by default)
	if e.Check("", "world.read.location") {
		t.Error("Check() should return false for empty plugin name")
	}
}

func TestCapabilityEnforcer_RemoveGrants(t *testing.T) {
	e := capability.NewEnforcer()

	// Setup: register a plugin
	if err := e.SetGrants("plugin", []string{"world.read.*"}); err != nil {
		t.Fatalf("SetGrants() failed: %v", err)
	}
	if !e.IsRegistered("plugin") {
		t.Fatal("plugin should be registered before removal")
	}
	if !e.Check("plugin", "world.read.location") {
		t.Fatal("plugin should have grant before removal")
	}

	// Remove grants
	e.RemoveGrants("plugin")

	// Verify plugin is no longer registered
	if e.IsRegistered("plugin") {
		t.Error("plugin should not be registered after RemoveGrants")
	}
	if e.Check("plugin", "world.read.location") {
		t.Error("plugin should not have grants after RemoveGrants")
	}
}

func TestCapabilityEnforcer_RemoveGrants_UnknownPlugin(t *testing.T) {
	e := capability.NewEnforcer()
	// Should not panic for unknown plugin
	e.RemoveGrants("unknown")
	// Verify enforcer still works after removing unknown plugin
	if e.IsRegistered("unknown") {
		t.Error("unknown plugin should not be registered")
	}
}

func TestCapabilityEnforcer_RemoveGrants_ZeroValue(t *testing.T) {
	var e capability.Enforcer
	// Should not panic on zero value
	e.RemoveGrants("any")
	// Verify enforcer still works after operation on zero value
	if e.IsRegistered("any") {
		t.Error("plugin should not be registered on zero value")
	}
}

func TestCapabilityEnforcer_GetGrants(t *testing.T) {
	e := capability.NewEnforcer()
	grants := []string{"world.read.*", "events.emit.*"}
	if err := e.SetGrants("plugin", grants); err != nil {
		t.Fatalf("SetGrants() failed: %v", err)
	}

	got := e.GetGrants("plugin")
	if len(got) != len(grants) {
		t.Errorf("GetGrants() returned %d grants, want %d", len(got), len(grants))
	}
	for i, want := range grants {
		if got[i] != want {
			t.Errorf("GetGrants()[%d] = %q, want %q", i, got[i], want)
		}
	}
}

func TestCapabilityEnforcer_GetGrants_UnknownPlugin(t *testing.T) {
	e := capability.NewEnforcer()
	got := e.GetGrants("unknown")
	if got != nil {
		t.Errorf("GetGrants() for unknown plugin = %v, want nil", got)
	}
}

func TestCapabilityEnforcer_GetGrants_ZeroValue(t *testing.T) {
	var e capability.Enforcer
	got := e.GetGrants("any")
	if got != nil {
		t.Errorf("GetGrants() on zero value = %v, want nil", got)
	}
}

func TestCapabilityEnforcer_GetGrants_DefensiveCopy(t *testing.T) {
	e := capability.NewEnforcer()
	if err := e.SetGrants("plugin", []string{"world.read.*"}); err != nil {
		t.Fatalf("SetGrants() failed: %v", err)
	}

	// Get grants and mutate the returned slice
	got := e.GetGrants("plugin")
	got[0] = "mutated"

	// Enforcer should not be affected
	if !e.Check("plugin", "world.read.location") {
		t.Error("GetGrants() should return a defensive copy")
	}
}

func TestCapabilityEnforcer_RemoveGrants_EmptyPluginName(t *testing.T) {
	e := capability.NewEnforcer()
	if err := e.SetGrants("valid-plugin", []string{"world.read.*"}); err != nil {
		t.Fatalf("SetGrants() failed: %v", err)
	}

	// Removing empty plugin name should not affect other plugins
	e.RemoveGrants("")

	// Valid plugin should still be registered
	if !e.IsRegistered("valid-plugin") {
		t.Error("valid-plugin should still be registered after removing empty string")
	}
}

func TestCapabilityEnforcer_GetGrants_EmptyPluginName(t *testing.T) {
	e := capability.NewEnforcer()
	if err := e.SetGrants("valid-plugin", []string{"world.read.*"}); err != nil {
		t.Fatalf("SetGrants() failed: %v", err)
	}

	// Getting grants for empty plugin name should return nil
	got := e.GetGrants("")
	if got != nil {
		t.Errorf("GetGrants(\"\") = %v, want nil", got)
	}
}

func TestCapabilityEnforcer_ConcurrentAccess_AllMethods(t *testing.T) {
	e := capability.NewEnforcer()
	var wg sync.WaitGroup

	// Pre-register some plugins
	for i := range 5 {
		plugin := fmt.Sprintf("plugin-%d", i)
		if err := e.SetGrants(plugin, []string{"world.read.*"}); err != nil {
			t.Fatalf("SetGrants() failed: %v", err)
		}
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
			e.RemoveGrants(plugin)
		}(i)
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
		if got != nil {
			t.Errorf("GetGrants(%q) on empty enforcer = %v, want nil", pluginName, got)
		}

		// If plugin name is valid for SetGrants, test round-trip
		if pluginName != "" {
			grants := []string{"world.read.*"}
			if err := e.SetGrants(pluginName, grants); err != nil {
				// SetGrants doesn't validate plugin names beyond empty, so this shouldn't fail
				t.Errorf("SetGrants(%q) failed unexpectedly: %v", pluginName, err)
				return
			}

			got = e.GetGrants(pluginName)
			if len(got) != 1 || got[0] != "world.read.*" {
				t.Errorf("GetGrants(%q) = %v, want %v", pluginName, got, grants)
			}
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

		// RemoveGrants should never panic, even on unknown/empty plugins
		e.RemoveGrants(pluginName)

		// After removal, plugin should not be registered
		if e.IsRegistered(pluginName) {
			t.Errorf("IsRegistered(%q) after RemoveGrants = true, want false", pluginName)
		}

		// If plugin name is valid, test removal of registered plugin
		if pluginName != "" {
			if err := e.SetGrants(pluginName, []string{"world.read.*"}); err != nil {
				t.Errorf("SetGrants(%q) failed: %v", pluginName, err)
				return
			}
			if !e.IsRegistered(pluginName) {
				t.Errorf("IsRegistered(%q) after SetGrants = false, want true", pluginName)
			}

			e.RemoveGrants(pluginName)

			if e.IsRegistered(pluginName) {
				t.Errorf("IsRegistered(%q) after RemoveGrants = true, want false", pluginName)
			}
		}
	})
}

func TestCapabilityEnforcer_ListPlugins(t *testing.T) {
	e := capability.NewEnforcer()

	// Empty enforcer should return empty slice
	got := e.ListPlugins()
	if len(got) != 0 {
		t.Errorf("ListPlugins() on empty enforcer = %v, want empty slice", got)
	}

	// Register some plugins
	if err := e.SetGrants("plugin-a", []string{"world.read.*"}); err != nil {
		t.Fatalf("SetGrants() failed: %v", err)
	}
	if err := e.SetGrants("plugin-b", []string{"events.emit.*"}); err != nil {
		t.Fatalf("SetGrants() failed: %v", err)
	}

	got = e.ListPlugins()
	if len(got) != 2 {
		t.Errorf("ListPlugins() returned %d plugins, want 2", len(got))
	}

	// Check both plugins are present (order not guaranteed)
	found := make(map[string]bool)
	for _, p := range got {
		found[p] = true
	}
	if !found["plugin-a"] || !found["plugin-b"] {
		t.Errorf("ListPlugins() = %v, want [plugin-a, plugin-b]", got)
	}
}

func TestCapabilityEnforcer_ListPlugins_ZeroValue(t *testing.T) {
	var e capability.Enforcer
	got := e.ListPlugins()
	if got == nil {
		t.Error("ListPlugins() on zero value should return empty slice, not nil")
	}
	if len(got) != 0 {
		t.Errorf("ListPlugins() on zero value = %v, want empty slice", got)
	}
}

func TestCapabilityEnforcer_ListPlugins_AfterRemove(t *testing.T) {
	e := capability.NewEnforcer()

	if err := e.SetGrants("plugin-a", []string{"world.read.*"}); err != nil {
		t.Fatalf("SetGrants() failed: %v", err)
	}
	if err := e.SetGrants("plugin-b", []string{"events.emit.*"}); err != nil {
		t.Fatalf("SetGrants() failed: %v", err)
	}

	// Remove one plugin
	e.RemoveGrants("plugin-a")

	got := e.ListPlugins()
	if len(got) != 1 {
		t.Errorf("ListPlugins() after removal = %v, want 1 plugin", got)
	}
	if len(got) == 1 && got[0] != "plugin-b" {
		t.Errorf("ListPlugins() = %v, want [plugin-b]", got)
	}
}

func TestCapabilityEnforcer_ListPlugins_DefensiveCopy(t *testing.T) {
	e := capability.NewEnforcer()
	if err := e.SetGrants("plugin", []string{"world.read.*"}); err != nil {
		t.Fatalf("SetGrants() failed: %v", err)
	}

	// Get list and mutate it
	got := e.ListPlugins()
	if len(got) > 0 {
		got[0] = "mutated"
	}

	// Enforcer should not be affected
	if !e.IsRegistered("plugin") {
		t.Error("ListPlugins() should return a defensive copy")
	}
}
