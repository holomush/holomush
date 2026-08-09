// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/attribute"
)

// TestEveryInTreePluginPolicyCompilesUnderTheLiveActionGate is the plugin-source
// analogue of TestEveryShippedSeedCompilesUnderTheLiveActionGate
// (internal/access/setup/setup_test.go).
//
// Phase 02.2-04 made the compiler's `action` branch fatal. There are THREE
// policy sources, not two: in-tree seeds, operator-authored database rows, and
// plugin-manifest policies installed by plugins.PolicyInstaller. The seed
// corpus has a whole-corpus gate; this is the plugin corpus's.
//
// The gate now also runs at INSTALL time (internal/plugin/policy_installer.go
// actionGate), so a bad in-tree manifest would fail the plugin load rather than
// bricking the corpus. This test is the earlier, cheaper signal: it turns RED in
// the untagged `task test` lane instead of waiting for a plugin-loading
// integration test, and it reads the SHIPPED yaml rather than a fixture, so a
// manifest edit cannot pass by never being loaded in a test.
//
// The registry is `action` ONLY, for the same reason its seed-side sibling gives:
// the compiler validates by DSL ROOT, never by provider name, so `action` is the
// only branch this scope can exercise and the only one it needs to. Plugin
// resource attributes are covered separately by
// plugins.ValidateManifestPolicySchemas against each plugin's own schema.
func TestEveryInTreePluginPolicyCompilesUnderTheLiveActionGate(t *testing.T) {
	t.Parallel()

	root := findRepoRoot(t)
	manifests, err := filepath.Glob(filepath.Join(root, "plugins", "*", "plugin.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, manifests, "control: an empty manifest set would make this test vacuous")

	compiler := policy.NewCompiler(attribute.NewActionOnlySchemaRegistry().Schema())

	seen := 0
	for _, path := range manifests {
		data, err := os.ReadFile(path)
		require.NoError(t, err)

		var m struct {
			Name     string `yaml:"name"`
			Policies []struct {
				Name string `yaml:"name"`
				DSL  string `yaml:"dsl"`
			} `yaml:"policies"`
		}
		require.NoError(t, yaml.Unmarshal(data, &m), "parse %s", path)

		for _, p := range m.Policies {
			seen++
			t.Run(m.Name+"/"+p.Name, func(t *testing.T) {
				_, _, err := compiler.Compile(p.DSL)
				require.NoError(t, err,
					"in-tree plugin policy %q in %s MUST compile under the live action gate — if this "+
						"is an unregistered action.* key, that is a REAL finding: fix the manifest or "+
						"add the key to 02.2-ACTION-AUDIT.md and attribute.ActionNamespaceSchema, do "+
						"NOT widen the schema to make this pass", p.Name, path)
			})
		}
	}

	require.NotZero(t, seen,
		"control: no in-tree plugin declares a policy, so every assertion above was vacuous")
}
