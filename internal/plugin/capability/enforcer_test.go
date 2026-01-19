package capability_test

import (
	"fmt"
	"sync"
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
		{
			name:       "single star without dot prefix does not match",
			grants:     []string{"*"},
			capability: "world.read",
			want:       false,
		},
		{
			name:       "star in middle is literal not wildcard",
			grants:     []string{"world.*.read"},
			capability: "world.foo.read",
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
			name:       "empty grant string in array",
			grants:     []string{"", "world.read.location"},
			capability: "world.read.location",
			want:       true,
		},
		{
			name:       "nil grants slice",
			grants:     nil,
			capability: "world.read.location",
			want:       false,
		},
		{
			name:       "trailing dot without star",
			grants:     []string{"world.read."},
			capability: "world.read.location",
			want:       false,
		},
		{
			name:       "double wildcard suffix",
			grants:     []string{"world.*.*"},
			capability: "world.read.location",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := capability.NewEnforcer()
			e.SetGrants("test-plugin", tt.grants)

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
	e.SetGrants("plugin", []string{"world.read.*"})
	if !e.Check("plugin", "world.read.location") {
		t.Error("initial grant should work")
	}

	// Overwrite with different grants
	e.SetGrants("plugin", []string{"events.emit.*"})
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
	e.SetGrants("plugin", grants)

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
	e.SetGrants("plugin", []string{"world.read.*"})
	if !e.Check("plugin", "world.read.location") {
		t.Error("Check() should work after SetGrants on zero value")
	}
}

func TestCapabilityEnforcer_ConcurrentAccess(t *testing.T) {
	e := capability.NewEnforcer()
	var wg sync.WaitGroup

	// Concurrent writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			plugin := fmt.Sprintf("plugin-%d", n)
			e.SetGrants(plugin, []string{"world.read.*"})
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			plugin := fmt.Sprintf("plugin-%d", n%10)
			_ = e.Check(plugin, "world.read.location")
		}(i)
	}

	wg.Wait()
}

func TestCapabilityEnforcer_EmptyPluginName(t *testing.T) {
	e := capability.NewEnforcer()
	e.SetGrants("", []string{"world.read.*"})

	// Empty plugin name should still work (no validation)
	if !e.Check("", "world.read.location") {
		t.Error("empty plugin name should work")
	}
}
