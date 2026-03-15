// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
)

// TestSeedPolicies_PluginSubjectsDefaultDeny verifies that plugin subjects are denied
// by the seed policy set. No seed policy grants access to plugin: subjects, so the
// default-deny behavior must apply. This test documents Gap G5 from seed.go:
// plugins define their own policies at install time via PolicyInstaller.
func TestSeedPolicies_PluginSubjectsDefaultDeny(t *testing.T) {
	engine := createSeedEngine(t, nil)

	tests := []struct {
		name     string
		subject  string
		action   string
		resource string
	}{
		{
			name:     "plugin read location",
			subject:  access.PluginSubject("unknown-plugin"),
			action:   "read",
			resource: access.LocationResource("01TESTLOCATION000000000000"),
		},
		{
			name:     "plugin execute command",
			subject:  access.PluginSubject("echo-bot"),
			action:   "execute",
			resource: access.CommandResource("say"),
		},
		{
			name:     "plugin write object",
			subject:  access.PluginSubject("world-builder"),
			action:   "write",
			resource: access.ObjectResource("01TESTOBJECT0000000000000"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, err := engine.Evaluate(context.Background(), types.AccessRequest{
				Subject:  tt.subject,
				Action:   tt.action,
				Resource: tt.resource,
			})
			require.NoError(t, err)
			assert.False(t, decision.IsAllowed(),
				"plugin subject %q should be denied by seed policies (default-deny, G5); got: %s — %s",
				tt.subject, decision.Effect(), decision.Reason())
			assert.Equal(t, types.EffectDefaultDeny, decision.Effect(),
				"effect must be default-deny, not explicit forbid")
		})
	}
}
