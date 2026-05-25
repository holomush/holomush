// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
)

// adminExtendDSL mirrors the admin-extend-publish-attempts policy declared in
// plugins/core-scenes/plugin.yaml (holomush-8kkv5.7) — keep the two in sync. The
// test installs it into a real Engine so the gate is exercised through real DSL
// parsing + condition evaluation against principal.character.roles, via
// pluginauthz.Evaluate.
const adminExtendDSL = `permit(principal is character, action in ["extend_publish_attempts"], resource is scene) when { "admin" in principal.character.roles };`

func TestPluginEvaluate_AdminExtendGate_RealEngine(t *testing.T) {
	cases := []struct {
		name      string
		roles     []string
		wantAllow bool
	}{
		{"admin allowed", []string{"admin"}, true},
		{"plain player denied", []string{"player"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// characterProvider(subjectAttrs, resourceAttrs): subjectMap is returned
			// for any principal lookup regardless of ID, so roles resolve correctly.
			prov := characterProvider(map[string]any{
				"id":    "01CHAR",
				"roles": tc.roles,
			}, nil)
			eng := createTestEngineWithPolicies(t, []string{adminExtendDSL},
				[]attribute.AttributeProvider{prov})

			dec, err := pluginauthz.Evaluate(context.Background(), pluginauthz.Input{
				Engine:     eng,
				PluginName: "core-scenes",
				OwnedTypes: map[string]bool{"scene": true},
				Subject:    "character:01CHAR",
				Action:     "extend_publish_attempts",
				Resource:   "scene:01SCENE0000000000000000000",
			})
			require.NoError(t, err)
			assert.Equal(t, tc.wantAllow, dec.Allowed)
		})
	}
}
