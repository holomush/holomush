package capability_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/holomush/holomush/internal/plugin/capability"
)

func TestCapabilityEnforcer_Check(t *testing.T) {
	tests := []struct {
		name       string
		grants     []string
		capability string
		want       bool
	}{
		// Positive cases
		{
			name:       "exact match",
			grants:     []string{"world.read.location"},
			capability: "world.read.location",
			want:       true,
		},
		{
			name:       "wildcard suffix matches child",
			grants:     []string{"world.read.*"},
			capability: "world.read.location",
			want:       true,
		},
		{
			name:       "wildcard suffix matches nested",
			grants:     []string{"world.*"},
			capability: "world.read.location",
			want:       true,
		},
		{
			name:       "multiple grants second matches",
			grants:     []string{"other.capability", "world.read.location"},
			capability: "world.read.location",
			want:       true,
		},
		{
			name:       "root wildcard matches everything",
			grants:     []string{".*"},
			capability: "world.read.location",
			want:       true,
		},
		{
			name:       "root wildcard matches single-segment capability",
			grants:     []string{".*"},
			capability: "world",
			want:       true,
		},
		{
			name:       "wildcard matches exact prefix boundary",
			grants:     []string{"world.read.*"},
			capability: "world.read.",
			want:       true,
		},

		// Negative cases
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

		// Boundary cases
		{
			name:       "empty capability returns false",
			grants:     []string{"world.read.*"},
			capability: "",
			want:       false,
		},
		{
			name:       "root wildcard does not match empty capability",
			grants:     []string{".*"},
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

	// Concurrent writers
	for i := range 10 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			plugin := fmt.Sprintf("plugin-%d", n)
			if err := e.SetGrants(plugin, []string{"world.read.*"}); err != nil {
				t.Errorf("SetGrants failed for %s: %v", plugin, err)
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
	tests := []struct {
		name    string
		pattern string
		wantErr bool
	}{
		// Valid patterns
		{name: "exact capability", pattern: "world.read.location", wantErr: false},
		{name: "wildcard suffix", pattern: "world.read.*", wantErr: false},
		{name: "root wildcard", pattern: ".*", wantErr: false},
		{name: "single segment", pattern: "world", wantErr: false},

		// Invalid patterns
		{name: "empty string", pattern: "", wantErr: true},
		{name: "bare star", pattern: "*", wantErr: true},
		{name: "double wildcard", pattern: "world.*.*", wantErr: true},
		{name: "middle wildcard", pattern: "world.*.read", wantErr: true},
		{name: "star without dot prefix", pattern: "world*", wantErr: true},
		{name: "missing dot before star", pattern: "worldread*", wantErr: true},
		{name: "trailing dot", pattern: "world.read.", wantErr: true},
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
	err := e.SetGrants("plugin", []string{"world.read.*", "*", "events.emit.*"})
	if err == nil {
		t.Error("SetGrants() should fail if any pattern is invalid")
	}

	// Plugin should not be registered after failed SetGrants
	if e.IsRegistered("plugin") {
		t.Error("plugin should not be registered after failed SetGrants")
	}
}
