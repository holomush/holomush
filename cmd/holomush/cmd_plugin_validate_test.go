// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPluginValidateManifestScenarios(t *testing.T) {
	tests := []struct {
		name               string
		manifestYAML       string
		pathOverride       string // when set, skip writing a temp manifest
		wantSuccess        bool
		wantOutContains    []string
		wantOutContainsAny []string // succeeds if any of these substrings is present
	}{
		{
			name: "accepts a valid manifest with crypto.emits",
			manifestYAML: `
name: ok-plugin
version: 1.0.0
type: lua
lua-plugin: { entry: main.lua }
crypto:
  emits:
    - event_type: foo
      sensitivity: always
`,
			wantSuccess:     true,
			wantOutContains: []string{"OK"},
		},
		{
			name: "rejects a manifest with invalid sensitivity",
			manifestYAML: `
name: bad-plugin
version: 1.0.0
type: lua
lua-plugin: { entry: main.lua }
crypto:
  emits:
    - event_type: foo
      sensitivity: kinda
`,
			wantSuccess: false,
			wantOutContainsAny: []string{
				"PLUGIN_CRYPTO_INVALID_SENSITIVITY",
				"kinda",
				"invalid sensitivity",
			},
		},
		{
			name: "accepts self-reference in consumes (no dependency required)",
			manifestYAML: `
name: self-ref-plugin
version: 1.0.0
type: lua
lua-plugin: { entry: main.lua }
crypto:
  emits:
    - event_type: whisper
      sensitivity: always
  consumes:
    - subjects: ["events.>"]
      requests_decryption: ["self-ref-plugin:whisper"]
`,
			wantSuccess:     true,
			wantOutContains: []string{"OK"},
		},
		{
			name:         "fails when manifest file does not exist",
			pathOverride: "__MISSING__",
			wantSuccess:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var manifestPath string
			switch {
			case tt.pathOverride == "__MISSING__":
				manifestPath = filepath.Join(t.TempDir(), "does-not-exist.yaml")
			case tt.pathOverride != "":
				manifestPath = tt.pathOverride
			default:
				tmp := t.TempDir()
				manifestPath = filepath.Join(tmp, "plugin.yaml")
				require.NoError(t, os.WriteFile(manifestPath, []byte(tt.manifestYAML), 0o600))
			}

			out, code := runCmd(t, []string{"plugin", "validate", manifestPath})
			if tt.wantSuccess {
				require.Equal(t, 0, code, "expected validate to exit 0; output:\n%s", out)
			} else {
				require.NotEqual(t, 0, code, "expected nonzero exit code; output:\n%s", out)
				assert.NotEmpty(t, out, "failure cases must produce diagnostic output")
			}
			for _, s := range tt.wantOutContains {
				assert.Contains(t, out, s)
			}
			if len(tt.wantOutContainsAny) > 0 {
				matched := false
				for _, s := range tt.wantOutContainsAny {
					if strings.Contains(out, s) {
						matched = true
						break
					}
				}
				assert.True(t, matched,
					"expected output to contain one of %v; got:\n%s",
					tt.wantOutContainsAny, out)
			}
		})
	}
}
