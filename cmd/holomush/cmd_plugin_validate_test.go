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

func TestPluginValidateAcceptsValidManifest(t *testing.T) {
	tmp := t.TempDir()
	manifest := filepath.Join(tmp, "plugin.yaml")
	require.NoError(t, os.WriteFile(manifest, []byte(`
name: ok-plugin
version: 1.0.0
type: lua
lua-plugin: { entry: main.lua }
crypto:
  emits:
    - event_type: foo
      sensitivity: always
`), 0o600))

	out, code := runCmd(t, []string{"plugin", "validate", manifest})
	require.Equal(t, 0, code, "expected validate to exit 0; output:\n%s", out)
	assert.Contains(t, out, "OK")
}

func TestPluginValidateRejectsInvalidSensitivity(t *testing.T) {
	tmp := t.TempDir()
	manifest := filepath.Join(tmp, "plugin.yaml")
	require.NoError(t, os.WriteFile(manifest, []byte(`
name: bad-plugin
version: 1.0.0
type: lua
lua-plugin: { entry: main.lua }
crypto:
  emits:
    - event_type: foo
      sensitivity: kinda
`), 0o600))

	out, code := runCmd(t, []string{"plugin", "validate", manifest})
	require.NotEqual(t, 0, code, "expected nonzero exit code")
	assert.True(t,
		strings.Contains(out, "PLUGIN_CRYPTO_INVALID_SENSITIVITY") ||
			strings.Contains(out, "kinda") ||
			strings.Contains(out, "invalid sensitivity"),
		"expected error output to mention the invalid sensitivity; got:\n%s", out)
}

func TestPluginValidateAcceptsSelfReference(t *testing.T) {
	tmp := t.TempDir()
	manifest := filepath.Join(tmp, "plugin.yaml")
	require.NoError(t, os.WriteFile(manifest, []byte(`
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
`), 0o600))

	out, code := runCmd(t, []string{"plugin", "validate", manifest})
	require.Equal(t, 0, code, "self-references must validate at author time; output:\n%s", out)
	assert.Contains(t, out, "OK")
}

func TestPluginValidateFailsOnMissingFile(t *testing.T) {
	out, code := runCmd(t, []string{"plugin", "validate", "/does/not/exist.yaml"})
	require.NotEqual(t, 0, code)
	assert.NotEmpty(t, out)
}
