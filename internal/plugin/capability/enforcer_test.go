// internal/plugin/capability/enforcer_test.go
package capability_test

import (
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
