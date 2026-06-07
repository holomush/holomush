// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package meta

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestEveryInTreePluginVerbTypeIsQualified pins holomush-aneim: every plugin's
// verbs[].type MUST be "<plugin-name>:<verb>" (exactly one colon, non-empty
// verb), so RenderingPublisher.Lookup resolves the emitted wire type instead of
// hard-failing EMIT_UNKNOWN_VERB in production. It deliberately asserts nothing
// about crypto.emits / the registered-emit set — those stay bare (INV-PLUGIN-32).
//
// The prefix is keyed on the manifest `name` field, NOT the directory basename,
// to mirror Manifest.Validate()'s gate (`want := m.Name + ":"`). The two diverge
// for plugins whose name != dir (e.g. setting-crossroads/name:crossroads); keying
// on dir here would demand a prefix the gate would reject at load.
//
// Verifies: INV-PLUGIN-40
func TestEveryInTreePluginVerbTypeIsQualified(t *testing.T) {
	root := findRepoRoot(t)
	manifests, err := filepath.Glob(filepath.Join(root, "plugins", "*", "plugin.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, manifests, "expected at least one in-tree plugin manifest")

	for _, path := range manifests {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		var m struct {
			Name  string `yaml:"name"`
			Verbs []struct {
				Type string `yaml:"type"`
			} `yaml:"verbs"`
		}
		require.NoError(t, yaml.Unmarshal(data, &m), "parse %s", path)
		require.NotEmptyf(t, m.Name, "%s: manifest name must not be empty", path)

		want := m.Name + ":"
		for _, v := range m.Verbs {
			require.Truef(t,
				strings.HasPrefix(v.Type, want) &&
					strings.Count(v.Type, ":") == 1 &&
					len(v.Type) > len(want),
				"%s: verbs[].type %q must be %q-prefixed (<plugin-name>:<verb>, one colon)",
				path, v.Type, m.Name)
		}
	}
}
