// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	plugins "github.com/holomush/holomush/internal/plugin"
	_ "github.com/holomush/holomush/internal/plugin/hostcap" // registers scope tokens via init()
	"github.com/holomush/holomush/pkg/errutil"
)

func TestParseManifestLuaPlugin(t *testing.T) {
	yaml := `
name: echo-bot
version: 1.0.0
type: lua
events:
  - say
  - pose
lua-plugin:
  entry: main.lua
`
	m, err := plugins.ParseManifest([]byte(yaml))
	require.NoError(t, err)

	assert.Equal(t, "echo-bot", m.Name)
	assert.Equal(t, "1.0.0", m.Version)
	assert.Equal(t, plugins.TypeLua, m.Type)
	assert.Len(t, m.Events, 2)
	require.NotNil(t, m.LuaPlugin)
	assert.Equal(t, "main.lua", m.LuaPlugin.Entry)
}

func TestManifestAcceptsLuaPluginWithEmits(t *testing.T) {
	data := []byte(`
name: emit-lua
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
emits: [scene, notifications]
history_scope: grid
`)

	manifest, err := plugins.ParseManifest(data)
	require.NoError(t, err)
	assert.Equal(t, []string{"scene", "notifications"}, manifest.Emits)
}

func TestManifestRejectsSettingPluginWithEmits(t *testing.T) {
	data := []byte(`
name: emit-setting
version: 1.0.0
type: setting
setting:
  display_name: Test
  content_dir: content
  starting_location: start
emits: [scene]
`)

	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "emits")
}

func TestManifestRejectsSettingPluginWithEmptyEmitsDeclaration(t *testing.T) {
	data := []byte(`
name: emit-setting-empty
version: 1.0.0
type: setting
setting:
  display_name: Test
  content_dir: content
  starting_location: start
emits: []
`)

	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "emits")
}

func TestManifestRejectsSettingPluginWithNullEmitsDeclaration(t *testing.T) {
	data := []byte(`
name: emit-setting-null
version: 1.0.0
type: setting
setting:
  display_name: Test
  content_dir: content
  starting_location: start
emits:
`)

	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "emits")
}

func TestManifestAcceptsBinaryPluginWithEmits(t *testing.T) {
	data := []byte(`
name: emit-binary
version: 1.0.0
type: binary
binary-plugin:
  executable: emit-binary
emits: [" scene ", notifications]
history_scope: scene
`)

	manifest, err := plugins.ParseManifest(data)
	require.NoError(t, err)
	assert.Equal(t, []string{"scene", "notifications"}, manifest.Emits)
}

func TestManifestRejectsInvalidEmitsEntries(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "empty entry",
			yaml: `
name: emit-empty
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
emits: [scene, ""]
`,
			wantErr: "emits",
		},
		{
			name: "duplicate entry",
			yaml: `
name: emit-dup
version: 1.0.0
type: binary
binary-plugin:
  executable: emit-binary
emits: [scene, scene]
`,
			wantErr: "duplicate",
		},
		{
			name: "entry with colon",
			yaml: `
name: emit-colon
version: 1.0.0
type: binary
binary-plugin:
  executable: emit-binary
emits: [scene:local]
`,
			wantErr: ":",
		},
		{
			name: "whitespace only entry",
			yaml: `
name: emit-space
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
emits: ["   "]
`,
			wantErr: "empty",
		},
		{
			name: "null declaration",
			yaml: `
name: emit-null
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
emits:
`,
			wantErr: "sequence",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := plugins.ParseManifest([]byte(tt.yaml))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestParseManifestBinaryPlugin(t *testing.T) {
	yaml := `
name: combat-system
version: 2.1.0
type: binary
events:
  - combat_start
binary-plugin:
  executable: combat-${os}-${arch}
`
	m, err := plugins.ParseManifest([]byte(yaml))
	require.NoError(t, err)

	assert.Equal(t, plugins.TypeBinary, m.Type)
	require.NotNil(t, m.BinaryPlugin)
	assert.Equal(t, "combat-${os}-${arch}", m.BinaryPlugin.Executable)
}

func TestParseManifest_InvalidName(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "uppercase not allowed",
			yaml: `
name: Invalid_Name
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`,
			wantErr: "name",
		},
		{
			name: "underscore not allowed",
			yaml: `
name: invalid_name
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`,
			wantErr: "name",
		},
		{
			name: "starts with number",
			yaml: `
name: 1plugin
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`,
			wantErr: "name",
		},
		{
			name: "starts with dash",
			yaml: `
name: -plugin
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`,
			wantErr: "name",
		},
		{
			name: "empty name",
			yaml: `
name: ""
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`,
			wantErr: "name",
		},
		{
			name: "trailing hyphen",
			yaml: `
name: echo-
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`,
			wantErr: "name",
		},
		{
			name: "name too long",
			yaml: `
name: this-is-a-very-long-plugin-name-that-exceeds-the-maximum-allowed-length
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`,
			wantErr: "name",
		},
		{
			name: "consecutive hyphens",
			yaml: `
name: test--plugin
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`,
			wantErr: "name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := plugins.ParseManifest([]byte(tt.yaml))
			require.Error(t, err, "expected error for invalid name")
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestParseManifest_ValidNames(t *testing.T) {
	tests := []struct {
		name     string
		plugName string
	}{
		{name: "simple", plugName: "echo"},
		{name: "with dash", plugName: "echo-bot"},
		{name: "with numbers", plugName: "echo123"},
		{name: "mixed", plugName: "echo-bot-v2"},
		{name: "single char", plugName: "a"},
		{name: "exactly max length (64 chars)", plugName: "a234567890123456789012345678901234567890123456789012345678901234"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yaml := `
name: ` + tt.plugName + `
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`
			m, err := plugins.ParseManifest([]byte(yaml))
			require.NoError(t, err, "ParseManifest() error for name %q", tt.plugName)
			require.NotNil(t, m)
			assert.Equal(t, tt.plugName, m.Name)
		})
	}
}

func TestParseManifest_MissingRequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "missing name",
			yaml: `
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`,
			wantErr: "name",
		},
		{
			name: "missing version",
			yaml: `
name: test
type: lua
lua-plugin:
  entry: main.lua
`,
			wantErr: "version",
		},
		{
			name: "missing type",
			yaml: `
name: test
version: 1.0.0
lua-plugin:
  entry: main.lua
`,
			wantErr: "type",
		},
		{
			name: "invalid type",
			yaml: `
name: test
version: 1.0.0
type: wasm
lua-plugin:
  entry: main.lua
`,
			wantErr: "type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := plugins.ParseManifest([]byte(tt.yaml))
			require.Error(t, err, "expected error for %s", tt.name)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestParseManifest_MissingTypeSpecificConfig(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "lua type without lua-plugin",
			yaml: `
name: test
version: 1.0.0
type: lua
`,
		},
		{
			name: "lua type with empty lua-plugin",
			yaml: `
name: test
version: 1.0.0
type: lua
lua-plugin:
`,
		},
		{
			name: "lua type with missing entry",
			yaml: `
name: test
version: 1.0.0
type: lua
lua-plugin:
  something: else
`,
		},
		{
			name: "binary type without binary-plugin",
			yaml: `
name: test
version: 1.0.0
type: binary
`,
		},
		{
			name: "binary type with empty binary-plugin",
			yaml: `
name: test
version: 1.0.0
type: binary
binary-plugin:
`,
		},
		{
			name: "binary type with missing executable",
			yaml: `
name: test
version: 1.0.0
type: binary
binary-plugin:
  something: else
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := plugins.ParseManifest([]byte(tt.yaml))
			assert.Error(t, err, "expected error for %s", tt.name)
		})
	}
}

func TestParseManifestInvalidYAML(t *testing.T) {
	yaml := `name: test
version: 1.0.0
type: [invalid`
	_, err := plugins.ParseManifest([]byte(yaml))
	assert.Error(t, err, "expected error for invalid YAML")
}

func TestManifestValidate(t *testing.T) {
	// Test Validate() method directly
	m := &plugins.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{
			Entry: "main.lua",
		},
	}
	assert.NoError(t, m.Validate())
}

func TestManifestValidateEmptyEntry(t *testing.T) {
	m := &plugins.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{
			Entry: "",
		},
	}
	assert.Error(t, m.Validate(), "Validate() should fail for empty entry")
}

func TestManifestValidateEmptyExecutable(t *testing.T) {
	m := &plugins.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "",
		},
	}
	assert.Error(t, m.Validate(), "Validate() should fail for empty executable")
}

func TestParseManifest_EmptyInput(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{name: "nil input", input: nil},
		{name: "empty slice", input: []byte{}},
		{name: "whitespace only", input: []byte("   \n\t  ")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := plugins.ParseManifest(tt.input)
			assert.Error(t, err, "ParseManifest() should return error for empty input")
		})
	}
}

func TestParseManifest_InvalidVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		wantErr string // substring expected in underlying semver error
	}{
		{name: "not semver - plain text", version: "latest", wantErr: "invalid semantic version"},
		{name: "not semver - single number", version: "1", wantErr: "invalid semantic version"},
		{name: "not semver - two numbers", version: "1.0", wantErr: "invalid semantic version"},
		{name: "not semver - leading v", version: "v1.0.0", wantErr: "invalid characters"},
		{name: "not semver - spaces", version: "1.0.0 beta", wantErr: "invalid characters"},
		{name: "not semver - invalid prerelease", version: "1.0.0-", wantErr: "invalid prerelease"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yaml := `
name: test
version: ` + tt.version + `
type: lua
lua-plugin:
  entry: main.lua
`
			_, err := plugins.ParseManifest([]byte(yaml))
			require.Error(t, err, "expected error for version %q", tt.version)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestParseManifest_ValidVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
	}{
		{name: "basic semver", version: "1.0.0"},
		{name: "with prerelease", version: "1.0.0-alpha"},
		{name: "with prerelease and number", version: "1.0.0-alpha.1"},
		{name: "with build metadata", version: "1.0.0+build"},
		{name: "with prerelease and build", version: "1.0.0-beta.2+build.123"},
		{name: "zero version", version: "0.0.0"},
		{name: "large numbers", version: "100.200.300"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yaml := `
name: test
version: ` + tt.version + `
type: lua
lua-plugin:
  entry: main.lua
`
			m, err := plugins.ParseManifest([]byte(yaml))
			require.NoError(t, err, "ParseManifest() error for version %q", tt.version)
			require.NotNil(t, m)
			assert.Equal(t, tt.version, m.Version)
		})
	}
}

func TestParseManifest_EngineConstraint(t *testing.T) {
	tests := []struct {
		name       string
		engine     string
		wantErr    bool
		wantEngine string
	}{
		{name: "exact version", engine: "2.0.0", wantErr: false, wantEngine: "2.0.0"},
		{name: "greater than or equal", engine: ">= 1.0.0", wantErr: false, wantEngine: ">= 1.0.0"},
		{name: "less than", engine: "< 3.0.0", wantErr: false, wantEngine: "< 3.0.0"},
		{name: "range", engine: ">= 1.0.0, < 2.0.0", wantErr: false, wantEngine: ">= 1.0.0, < 2.0.0"},
		{name: "caret", engine: "^1.2.0", wantErr: false, wantEngine: "^1.2.0"},
		{name: "tilde", engine: "~1.2.0", wantErr: false, wantEngine: "~1.2.0"},
		{name: "wildcard", engine: "1.x", wantErr: false, wantEngine: "1.x"},
		{name: "invalid constraint", engine: "not-a-version", wantErr: true},
		{name: "empty is valid (optional)", engine: "", wantErr: false, wantEngine: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yaml := `
name: test
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`
			if tt.engine != "" {
				yaml = `
name: test
version: 1.0.0
type: lua
engine: "` + tt.engine + `"
lua-plugin:
  entry: main.lua
`
			}
			m, err := plugins.ParseManifest([]byte(yaml))
			if tt.wantErr {
				require.Error(t, err, "expected error for engine %q", tt.engine)
				return
			}
			require.NoError(t, err, "ParseManifest() error for engine %q", tt.engine)
			assert.Equal(t, tt.wantEngine, m.Engine)
		})
	}
}

func TestParseManifest_Dependencies(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
		wantDep map[string]string
	}{
		{
			name: "single dependency with exact version",
			yaml: `
name: test
version: 1.0.0
type: lua
dependencies:
  other-plugin: "1.0.0"
lua-plugin:
  entry: main.lua
`,
			wantErr: false,
			wantDep: map[string]string{"other-plugin": "1.0.0"},
		},
		{
			name: "dependency with constraint",
			yaml: `
name: test
version: 1.0.0
type: lua
dependencies:
  other-plugin: ">= 1.0.0"
lua-plugin:
  entry: main.lua
`,
			wantErr: false,
			wantDep: map[string]string{"other-plugin": ">= 1.0.0"},
		},
		{
			name: "multiple dependencies",
			yaml: `
name: test
version: 1.0.0
type: lua
dependencies:
  plugin-a: "^1.0.0"
  plugin-b: "~2.0.0"
lua-plugin:
  entry: main.lua
`,
			wantErr: false,
			wantDep: map[string]string{"plugin-a": "^1.0.0", "plugin-b": "~2.0.0"},
		},
		{
			name: "invalid dependency constraint",
			yaml: `
name: test
version: 1.0.0
type: lua
dependencies:
  bad-plugin: "not-valid"
lua-plugin:
  entry: main.lua
`,
			wantErr: true,
		},
		{
			name: "no dependencies is valid",
			yaml: `
name: test
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`,
			wantErr: false,
			wantDep: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := plugins.ParseManifest([]byte(tt.yaml))
			if tt.wantErr {
				require.Error(t, err, "expected error")
				return
			}
			require.NoError(t, err)
			assert.Len(t, m.Dependencies, len(tt.wantDep))
			for k, v := range tt.wantDep {
				assert.Equal(t, v, m.Dependencies[k], "Dependencies[%q]", k)
			}
		})
	}
}

func TestParseManifestWithPolicies(t *testing.T) {
	data := []byte(`
name: test-plugin
version: "1.0.0"
type: lua
policies:
  - name: "allow-emit"
    dsl: |
      permit(principal, action, resource) when {
        principal is plugin
        and action is "emit"
      };
  - name: "allow-kv"
    dsl: |
      permit(principal, action, resource) when {
        principal is plugin
        and resource like "kv:test-plugin:*"
      };
lua-plugin:
  entry: main.lua
`)
	m, err := plugins.ParseManifest(data)
	require.NoError(t, err)
	assert.Len(t, m.Policies, 2)
	assert.Equal(t, "allow-emit", m.Policies[0].Name)
	assert.Contains(t, m.Policies[0].DSL, "principal is plugin")
	assert.Equal(t, "allow-kv", m.Policies[1].Name)
}

func TestParseManifestNoPolicies(t *testing.T) {
	data := []byte(`
name: test-plugin
version: "1.0.0"
type: lua
lua-plugin:
  entry: main.lua
`)
	m, err := plugins.ParseManifest(data)
	require.NoError(t, err)
	assert.Empty(t, m.Policies)
}

func TestParseManifestPolicyEmptyName(t *testing.T) {
	data := []byte(`
name: test-plugin
version: "1.0.0"
type: lua
policies:
  - name: ""
    dsl: "permit(principal, action, resource);"
lua-plugin:
  entry: main.lua
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name cannot be empty")
}

func TestParseManifestPolicyEmptyDSL(t *testing.T) {
	data := []byte(`
name: test-plugin
version: "1.0.0"
type: lua
policies:
  - name: "allow-emit"
    dsl: ""
lua-plugin:
  entry: main.lua
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dsl cannot be empty")
}

func TestParseManifest_EventValidation(t *testing.T) {
	tests := []struct {
		name       string
		yaml       string
		wantErr    bool
		wantEvents []string
	}{
		{
			name: "valid events",
			yaml: `
name: test
version: 1.0.0
type: lua
events:
  - say
  - pose
  - arrive
lua-plugin:
  entry: main.lua
`,
			wantErr:    false,
			wantEvents: []string{"say", "pose", "arrive"},
		},
		{
			name: "empty events is valid",
			yaml: `
name: test
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`,
			wantErr:    false,
			wantEvents: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := plugins.ParseManifest([]byte(tt.yaml))
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.wantEvents == nil {
				assert.Empty(t, m.Events)
			} else {
				assert.Equal(t, tt.wantEvents, m.Events)
			}
		})
	}
}

func TestManifestHasEvent(t *testing.T) {
	// Test that Events slice correctly stores event subscriptions
	events := []string{"say", "pose"}
	m := &plugins.Manifest{
		Events: events,
	}

	// HasEvent is not a method on Manifest, but we can test the Events slice directly
	assert.Contains(t, m.Events, "say")
	assert.Contains(t, m.Events, "pose")
	assert.NotContains(t, m.Events, "arrive")
}

func TestParseManifestWhitespace(t *testing.T) {
	// Test that extra whitespace is handled correctly
	yaml := `
name:    test-plugin
version:   1.0.0
type:   lua
lua-plugin:
  entry:   main.lua
`
	m, err := plugins.ParseManifest([]byte(yaml))
	require.NoError(t, err)
	// YAML should trim whitespace
	assert.True(t, strings.TrimSpace(m.Name) == "test-plugin" || m.Name == "test-plugin")
}

func TestParseManifest_Commands(t *testing.T) {
	tests := []struct {
		name         string
		yaml         string
		wantErr      bool
		wantErrMsg   string
		wantCommands int
	}{
		{
			name: "single command with inline help",
			yaml: `
name: test
version: 1.0.0
type: lua
commands:
  - name: say
    help: Send a message to the room
    usage: "say <message>"
    helpText: |
      The say command sends a message to all players in the current room.

      Example: say Hello everyone!
lua-plugin:
  entry: main.lua
`,
			wantCommands: 1,
		},
		{
			name: "command with capabilities",
			yaml: `
name: test
version: 1.0.0
type: lua
commands:
  - name: teleport
    capabilities:
      - action: write
        resource: location
        scope: global
    help: Teleport to a location
    usage: "teleport <destination>"
lua-plugin:
  entry: main.lua
`,
			wantCommands: 1,
		},
		{
			name: "command with helpFile reference",
			yaml: `
name: test
version: 1.0.0
type: lua
commands:
  - name: combat
    help: Combat system commands
    usage: "combat <action>"
    helpFile: help/combat.md
lua-plugin:
  entry: main.lua
`,
			wantCommands: 1,
		},
		{
			name: "multiple commands",
			yaml: `
name: test
version: 1.0.0
type: lua
commands:
  - name: say
    help: Send a message
    usage: "say <message>"
  - name: pose
    help: Describe an action
    usage: "pose <action>"
  - name: ooc
    help: Out-of-character message
    usage: "ooc <message>"
lua-plugin:
  entry: main.lua
`,
			wantCommands: 3,
		},
		{
			name: "no commands is valid",
			yaml: `
name: test
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`,
			wantCommands: 0,
		},
		{
			name: "command with both helpText and helpFile is invalid",
			yaml: `
name: test
version: 1.0.0
type: lua
commands:
  - name: bad
    help: Invalid command
    usage: "bad"
    helpText: Some inline help
    helpFile: help/bad.md
lua-plugin:
  entry: main.lua
`,
			wantErr:    true,
			wantErrMsg: "cannot specify both helpText and helpFile",
		},
		{
			name: "command with empty name is invalid",
			yaml: `
name: test
version: 1.0.0
type: lua
commands:
  - name: ""
    help: Empty name
lua-plugin:
  entry: main.lua
`,
			wantErr:    true,
			wantErrMsg: "cannot be empty",
		},
		{
			name: "command missing name is invalid",
			yaml: `
name: test
version: 1.0.0
type: lua
commands:
  - help: Missing name field
lua-plugin:
  entry: main.lua
`,
			wantErr:    true,
			wantErrMsg: "cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := plugins.ParseManifest([]byte(tt.yaml))
			if tt.wantErr {
				require.Error(t, err, "expected error")
				assert.Contains(t, err.Error(), tt.wantErrMsg)
				return
			}
			require.NoError(t, err)
			assert.Len(t, m.Commands, tt.wantCommands)
		})
	}
}

func TestParseManifestCommandSpecFields(t *testing.T) {
	yamlData := `
name: test
version: 1.0.0
type: lua
commands:
  - name: teleport
    capabilities:
      - action: write
        resource: location
        scope: global
      - action: enter
        resource: location
        scope: global
    help: Teleport to a location
    usage: "teleport <destination>"
    helpText: |
      Teleport yourself or another player to a destination.

      Examples:
        teleport #123
        teleport The Garden
lua-plugin:
  entry: main.lua
`
	m, err := plugins.ParseManifest([]byte(yamlData))
	require.NoError(t, err)
	require.Len(t, m.Commands, 1)

	cmd := m.Commands[0]
	assert.Equal(t, "teleport", cmd.Name)
	assert.Len(t, cmd.Capabilities, 2)
	assert.Equal(t, "write", cmd.Capabilities[0].Action)
	assert.Equal(t, "location", cmd.Capabilities[0].Resource)
	assert.Equal(t, "global", cmd.Capabilities[0].Scope)
	assert.Equal(t, "enter", cmd.Capabilities[1].Action)
	assert.Equal(t, "Teleport to a location", cmd.Help)
	assert.Equal(t, "teleport <destination>", cmd.Usage)
	assert.Contains(t, cmd.HelpText, "Teleport yourself")
	assert.Empty(t, cmd.HelpFile)
}

func TestParseManifestCommandSpecHelpFile(t *testing.T) {
	yaml := `
name: test
version: 1.0.0
type: lua
commands:
  - name: combat
    help: Combat system
    usage: "combat <action>"
    helpFile: help/combat.md
lua-plugin:
  entry: main.lua
`
	m, err := plugins.ParseManifest([]byte(yaml))
	require.NoError(t, err)
	require.Len(t, m.Commands, 1)

	cmd := m.Commands[0]
	assert.Equal(t, "combat", cmd.Name)
	assert.Equal(t, "help/combat.md", cmd.HelpFile)
	assert.Empty(t, cmd.HelpText)
}

func TestCommandSpec_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cmd     plugins.CommandSpec
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid command minimal",
			cmd: plugins.CommandSpec{
				Name: "say",
			},
			wantErr: false,
		},
		{
			name: "valid command full inline help",
			cmd: plugins.CommandSpec{
				Name:         "teleport",
				Capabilities: []command.Capability{{Action: "write", Resource: "location", Scope: command.ScopeLocal}},
				Help:         "Teleport to a location",
				Usage:        "teleport <dest>",
				HelpText:     "Detailed help here",
			},
			wantErr: false,
		},
		{
			name: "valid command with helpFile",
			cmd: plugins.CommandSpec{
				Name:     "combat",
				Help:     "Combat system",
				HelpFile: "help/combat.md",
			},
			wantErr: false,
		},
		{
			name: "empty name",
			cmd: plugins.CommandSpec{
				Name: "",
				Help: "Some help",
			},
			wantErr: true,
			errMsg:  "cannot be empty",
		},
		{
			name: "whitespace-only name",
			cmd: plugins.CommandSpec{
				Name: "   ",
				Help: "Some help",
			},
			wantErr: true,
			errMsg:  "cannot be empty",
		},
		{
			name: "name too long",
			cmd: plugins.CommandSpec{
				Name: "thisisaverylongcommandname",
				Help: "Some help",
			},
			wantErr: true,
			errMsg:  "exceeds maximum length",
		},
		{
			name: "name starts with number",
			cmd: plugins.CommandSpec{
				Name: "1say",
				Help: "Some help",
			},
			wantErr: true,
			errMsg:  "must start with a letter",
		},
		{
			name: "name with invalid characters",
			cmd: plugins.CommandSpec{
				Name: "say*command",
				Help: "Some help",
			},
			wantErr: true,
			errMsg:  "must start with a letter",
		},
		{
			name: "both helpText and helpFile",
			cmd: plugins.CommandSpec{
				Name:     "bad",
				HelpText: "inline help",
				HelpFile: "help/bad.md",
			},
			wantErr: true,
			errMsg:  "cannot specify both helpText and helpFile",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cmd.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCommandSpec_Validate_HelpTextHelpFileMutualExclusivity(t *testing.T) {
	tests := []struct {
		name    string
		cmd     plugins.CommandSpec
		wantErr bool
		errMsg  string
	}{
		{
			name: "both helpText and helpFile rejects with error",
			cmd: plugins.CommandSpec{
				Name:     "bad",
				HelpText: "Some inline help text",
				HelpFile: "help/bad.md",
			},
			wantErr: true,
			errMsg:  "cannot specify both helpText and helpFile",
		},
		{
			name: "helpText only passes validation",
			cmd: plugins.CommandSpec{
				Name:     "say",
				HelpText: "Detailed inline help for the say command.",
			},
			wantErr: false,
		},
		{
			name: "helpFile only passes validation",
			cmd: plugins.CommandSpec{
				Name:     "combat",
				HelpFile: "help/combat.md",
			},
			wantErr: false,
		},
		{
			name: "neither helpText nor helpFile passes validation",
			cmd: plugins.CommandSpec{
				Name: "look",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cmd.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestManifest_Validate_DuplicateCommandNames(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
		errMsg  string
	}{
		{
			name: "duplicate command names",
			yaml: `
name: test
version: 1.0.0
type: lua
commands:
  - name: say
    help: First say command
  - name: pose
    help: Pose command
  - name: say
    help: Duplicate say command
lua-plugin:
  entry: main.lua
`,
			wantErr: true,
			errMsg:  "duplicate command name",
		},
		{
			name: "unique command names",
			yaml: `
name: test
version: 1.0.0
type: lua
commands:
  - name: say
    help: Say command
  - name: pose
    help: Pose command
  - name: ooc
    help: OOC command
lua-plugin:
  entry: main.lua
`,
			wantErr: false,
		},
		{
			name: "single command",
			yaml: `
name: test
version: 1.0.0
type: lua
commands:
  - name: say
    help: Say command
lua-plugin:
  entry: main.lua
`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := plugins.ParseManifest([]byte(tt.yaml))
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestManifest_LoadPriority(t *testing.T) {
	tests := []struct {
		name             string
		yaml             string
		wantErr          bool
		wantErrMsg       string
		wantLoadPriority int
	}{
		{
			name: "lua plugin with load_priority -999 is allowed",
			yaml: `
name: custom-say
version: 1.0.0
type: lua
priority: -999
lua-plugin:
  entry: main.lua
`,
			wantLoadPriority: -999,
		},
		{
			name: "lua plugin with load_priority -1000 is rejected",
			yaml: `
name: custom-say
version: 1.0.0
type: lua
priority: -1000
lua-plugin:
  entry: main.lua
`,
			wantErr:    true,
			wantErrMsg: "load_priority < -999 is reserved for core plugins",
		},
		{
			name: "binary plugin with load_priority -1001 is rejected",
			yaml: `
name: custom-combat
version: 1.0.0
type: binary
priority: -1001
binary-plugin:
  executable: combat
`,
			wantErr:    true,
			wantErrMsg: "load_priority < -999 is reserved for core plugins",
		},
		{
			name: "lua plugin with positive load_priority is allowed",
			yaml: `
name: custom-say
version: 1.0.0
type: lua
priority: 100
lua-plugin:
  entry: main.lua
`,
			wantLoadPriority: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := plugins.ParseManifest([]byte(tt.yaml))
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantLoadPriority, int(m.EffectivePriority()))
		})
	}
}

func TestTypeSettingConstant(t *testing.T) {
	assert.Equal(t, plugins.Type("setting"), plugins.TypeSetting)
}

func TestParseManifestSettingPlugin(t *testing.T) {
	yaml := `
name: my-setting
version: 1.0.0
type: setting
setting:
  display_name: My Setting
  description: A test setting
  content_dir: content
  world_dir: world
  theme: default
  starting_location: The Nexus
`
	m, err := plugins.ParseManifest([]byte(yaml))
	require.NoError(t, err)

	assert.Equal(t, plugins.TypeSetting, m.Type)
	require.NotNil(t, m.Setting)
	assert.Equal(t, "My Setting", m.Setting.DisplayName)
	assert.Equal(t, "A test setting", m.Setting.Description)
	assert.Equal(t, "content", m.Setting.ContentDir)
	assert.Equal(t, "world", m.Setting.WorldDir)
	assert.Equal(t, "default", m.Setting.Theme)
	assert.Equal(t, "The Nexus", m.Setting.StartingLocation)
}

func TestParseManifestSettingPluginMissingStanza(t *testing.T) {
	yaml := `
name: my-setting
version: 1.0.0
type: setting
`
	_, err := plugins.ParseManifest([]byte(yaml))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "setting")
}

func TestParseManifestSettingPluginMissingStartingLocation(t *testing.T) {
	yaml := `
name: my-setting
version: 1.0.0
type: setting
setting:
  display_name: My Setting
  content_dir: content
`
	_, err := plugins.ParseManifest([]byte(yaml))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "starting_location")
}

func TestParseManifestSettingPluginWithCommands(t *testing.T) {
	yaml := `
name: my-setting
version: 1.0.0
type: setting
setting:
  display_name: My Setting
  content_dir: content
  starting_location: The Nexus
commands:
  - name: look
    help: look around
`
	_, err := plugins.ParseManifest([]byte(yaml))
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "command") || strings.Contains(err.Error(), "setting"))
}

func TestParseManifestSettingPluginWithLuaPlugin(t *testing.T) {
	yaml := `
name: my-setting
version: 1.0.0
type: setting
setting:
  display_name: My Setting
  content_dir: content
  starting_location: The Nexus
lua-plugin:
  entry: main.lua
`
	_, err := plugins.ParseManifest([]byte(yaml))
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "lua") || strings.Contains(err.Error(), "setting"))
}

func TestCommandSpec_Validate_InvalidCapability(t *testing.T) {
	tests := []struct {
		name   string
		cmd    plugins.CommandSpec
		errMsg string
	}{
		// Note: unknown action strings are now allowed — Capability.Validate() only
		// requires non-empty action. Cross-plugin validation of action membership
		// happens during loadPlugin with context from plugin manifests.
		{
			name: "invalid scope in capability",
			cmd: plugins.CommandSpec{
				Name:         "teleport",
				Capabilities: []command.Capability{{Action: "write", Resource: "location", Scope: "everywhere"}},
			},
			errMsg: "scope",
		},
		{
			name: "empty action in capability",
			cmd: plugins.CommandSpec{
				Name:         "teleport",
				Capabilities: []command.Capability{{Action: "", Resource: "location"}},
			},
			errMsg: "action",
		},
		// Note: unknown resource type is NOT checked by Validate() — resource type
		// validation happens during loadPlugin with cross-plugin context.
		// "second capability invalid resource" removed — resource type validation
		// is deferred to loadPlugin with cross-plugin context.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cmd.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

func TestParseManifestCommandWithInvalidCapability(t *testing.T) {
	yamlData := `
name: test
version: 1.0.0
type: lua
commands:
  - name: teleport
    capabilities:
      - action: ""
        resource: location
lua-plugin:
  entry: main.lua
`
	_, err := plugins.ParseManifest([]byte(yamlData))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "action")
}

func TestParseManifestSettingPluginWithBinaryPlugin(t *testing.T) {
	yaml := `
name: my-setting
version: 1.0.0
type: setting
setting:
  display_name: My Setting
  content_dir: content
  starting_location: The Nexus
binary-plugin:
  executable: my-binary
`
	_, err := plugins.ParseManifest([]byte(yaml))
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "binary") || strings.Contains(err.Error(), "setting"))
}

func TestManifestRequiresProvides(t *testing.T) {
	t.Run("parses requires and provides fields", func(t *testing.T) {
		data := []byte(`
name: test-plugin
version: 1.0.0
type: binary
requires:
  - holomush.world.v1.WorldService
provides:
  - holomush.scene.v1.SceneService
binary-plugin:
  executable: ./plugin
commands:
  - name: test
    help: test command
`)
		m, err := plugins.ParseManifest(data)
		require.NoError(t, err)
		assert.Equal(t, []string{"holomush.world.v1.WorldService"}, m.RequiredServiceNames())
		assert.Equal(t, []string{"holomush.scene.v1.SceneService"}, m.Provides)
	})

	t.Run("rejects provides on lua plugins", func(t *testing.T) {
		data := []byte(`
name: test-plugin
version: 1.0.0
type: lua
provides:
  - holomush.scene.v1.SceneService
lua-plugin:
  entry: main.lua
commands:
  - name: test
    help: test command
`)
		_, err := plugins.ParseManifest(data)
		assert.Error(t, err)
	})

	t.Run("allows requires on lua plugins", func(t *testing.T) {
		data := []byte(`
name: test-plugin
version: 1.0.0
type: lua
requires:
  - holomush.world.v1.WorldService
lua-plugin:
  entry: main.lua
commands:
  - name: test
    help: test command
`)
		m, err := plugins.ParseManifest(data)
		require.NoError(t, err)
		assert.Equal(t, []string{"holomush.world.v1.WorldService"}, m.RequiredServiceNames())
	})
}

func TestParseManifestTypedRequiresAccessors(t *testing.T) {
	m, err := plugins.ParseManifest([]byte(`
name: t
version: 1.0.0
type: lua
requires:
  - capability: world.query
  - service: holomush.scene.v1.SceneService
lua-plugin:
  entry: main.lua
`))
	require.NoError(t, err)
	assert.Equal(t, []string{"world.query"}, m.RequiredCapabilities())
	assert.Equal(t, []string{"holomush.scene.v1.SceneService"}, m.RequiredServiceNames())
}

func TestParseManifestLegacyFlatRequiresParsesAsServices(t *testing.T) {
	m, err := plugins.ParseManifest([]byte(`
name: t
version: 1.0.0
type: lua
requires:
  - holomush.world.v1.WorldService
lua-plugin:
  entry: main.lua
`))
	require.NoError(t, err)
	assert.Empty(t, m.RequiredCapabilities())
	assert.Equal(t, []string{"holomush.world.v1.WorldService"}, m.RequiredServiceNames())
}

func TestManifestStorage(t *testing.T) {
	t.Run("parses storage field for binary plugins", func(t *testing.T) {
		data := []byte(`
name: test-plugin
version: 1.0.0
type: binary
storage: postgres
binary-plugin:
  executable: ./plugin
commands:
  - name: test
    help: test command
`)
		m, err := plugins.ParseManifest(data)
		require.NoError(t, err)
		assert.Equal(t, plugins.StoragePostgres, m.Storage)
	})

	t.Run("defaults storage to kv when not specified", func(t *testing.T) {
		data := []byte(`
name: test-plugin
version: 1.0.0
type: binary
binary-plugin:
  executable: ./plugin
commands:
  - name: test
    help: test command
`)
		m, err := plugins.ParseManifest(data)
		require.NoError(t, err)
		assert.Equal(t, plugins.StorageKV, m.Storage)
	})

	t.Run("rejects postgres storage on lua plugins", func(t *testing.T) {
		data := []byte(`
name: test-plugin
version: 1.0.0
type: lua
storage: postgres
lua-plugin:
  entry: main.lua
commands:
  - name: test
    help: test command
`)
		_, err := plugins.ParseManifest(data)
		assert.Error(t, err)
	})
}

func TestParseManifestCommandWithAliases(t *testing.T) {
	yamlData := `
name: test-comm
version: 1.0.0
type: lua
commands:
  - name: say
    aliases:
      - '"'
    help: Say something
  - name: pose
    aliases:
      - ":"
      - ";"
    help: Pose an action
lua-plugin:
  entry: main.lua
`
	m, err := plugins.ParseManifest([]byte(yamlData))
	require.NoError(t, err)
	require.Len(t, m.Commands, 2)

	assert.Equal(t, []string{`"`}, m.Commands[0].Aliases)
	assert.Equal(t, []string{":", ";"}, m.Commands[1].Aliases)
}

func TestCommandSpecValidateRejectsEmptyAlias(t *testing.T) {
	cmd := plugins.CommandSpec{Name: "say", Aliases: []string{""}}
	err := cmd.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestCommandSpecValidateRejectsDuplicateAlias(t *testing.T) {
	cmd := plugins.CommandSpec{Name: "pose", Aliases: []string{":", ":"}}
	err := cmd.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate alias")
}

func TestParseManifestCommandWithoutAliasesBackwardCompatible(t *testing.T) {
	yamlData := `
name: test-compat
version: 1.0.0
type: lua
commands:
  - name: look
    help: Look around
lua-plugin:
  entry: main.lua
`
	m, err := plugins.ParseManifest([]byte(yamlData))
	require.NoError(t, err)
	require.Len(t, m.Commands, 1)
	assert.Nil(t, m.Commands[0].Aliases)
}

// TestParseManifestResourceTypesAndTrust covers the four ParseManifest
// scenarios for the resource_types and trust manifest fields. Each case
// is named in ACE form (Action — Condition — Expectation) and checks both
// the error result and any field-level expectations on the parsed manifest.
func TestParseManifestResourceTypesAndTrust(t *testing.T) {
	tests := []struct {
		name    string // ACE: Action — Condition — Expectation
		yaml    string
		wantErr string // substring; empty means no error
		check   func(t *testing.T, m *plugins.Manifest)
	}{
		{
			name: "parses resource_types when declared on a binary plugin",
			yaml: `
name: test-plugin
version: 1.0.0
type: binary
binary-plugin:
  executable: test-plugin
resource_types: [widget]
`,
			check: func(t *testing.T, m *plugins.Manifest) {
				assert.Equal(t, []string{"widget"}, m.ResourceTypes)
			},
		},
		{
			name:    "rejects resource_types when declared on a Lua plugin",
			wantErr: "resource_types",
			yaml: `
name: test-plugin
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
resource_types: [widget]
`,
		},
		{
			name:    "rejects resource_types when an entry names a protected core type",
			wantErr: "protected",
			yaml: `
name: test-plugin
version: 1.0.0
type: binary
binary-plugin:
  executable: test-plugin
resource_types: [location]
`,
		},
		{
			name: "parses trust.all_principals when declared on a binary plugin",
			yaml: `
name: test-plugin
version: 1.0.0
type: binary
binary-plugin:
  executable: test-plugin
trust:
  all_principals: true
`,
			check: func(t *testing.T, m *plugins.Manifest) {
				require.NotNil(t, m.Trust)
				assert.True(t, m.Trust.AllPrincipals)
			},
		},
		{
			name: "parses actions when declared on a binary plugin",
			yaml: `
name: test-plugin
version: 1.0.0
type: binary
binary-plugin:
  executable: test-plugin
actions: [join, leave]
`,
			check: func(t *testing.T, m *plugins.Manifest) {
				assert.Equal(t, []string{"join", "leave"}, m.Actions)
			},
		},
		{
			name: "parses actions when declared on a Lua plugin",
			yaml: `
name: test-plugin
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
actions: [craft]
`,
			check: func(t *testing.T, m *plugins.Manifest) {
				assert.Equal(t, []string{"craft"}, m.Actions)
			},
		},
		{
			name:    "rejects actions with an empty entry",
			wantErr: "action",
			yaml: `
name: test-plugin
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
actions: [""]
`,
		},
		{
			name:    "rejects actions with duplicate entries",
			wantErr: "duplicate",
			yaml: `
name: test-plugin
version: 1.0.0
type: binary
binary-plugin:
  executable: test-plugin
actions: [join, join]
`,
		},
		{
			name: "accepts actions that re-declare a core action",
			yaml: `
name: test-plugin
version: 1.0.0
type: binary
binary-plugin:
  executable: test-plugin
actions: [read]
`,
			check: func(t *testing.T, m *plugins.Manifest) {
				assert.Equal(t, []string{"read"}, m.Actions)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := plugins.ParseManifest([]byte(tt.yaml))
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, m)
			if tt.check != nil {
				tt.check(t, m)
			}
		})
	}
}

func TestManifestResourceTypesRejectsDuplicate(t *testing.T) {
	data := []byte(`
name: dup-plugin
version: 1.0.0
type: binary
binary-plugin:
  executable: dup-plugin
resource_types: [widget, widget]
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

func TestManifestResourceTypesRejectsInvalidName(t *testing.T) {
	data := []byte(`
name: bad-rt-plugin
version: 1.0.0
type: binary
binary-plugin:
  executable: bad-rt-plugin
resource_types: [Bad-Name]
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resource type name")
}

func TestManifestEffectivePriorityDefaultsToLoadPriorityDefault(t *testing.T) {
	// When a manifest does not set priority, EffectivePriority should
	// fall back to the LoadPriorityDefault (100) constant.
	m := &plugins.Manifest{
		Name:      "p",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}
	assert.Equal(t, plugins.LoadPriorityDefault, m.EffectivePriority())
}

func TestSceneResourceTypeIsNotProtected(t *testing.T) {
	// Why: scenes are owned by the core-scenes plugin (Epic 9 v2), not the
	// server core. The plugin must be able to declare resource_types: [scene]
	// without trust escalation. See spec
	// docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md section 5.1.
	if plugins.ProtectedResourceTypes["scene"] {
		t.Fatal("scene MUST NOT be in ProtectedResourceTypes — owned by core-scenes plugin")
	}
}

// TestManifestTrustFieldParsed is folded into TestParseManifestResourceTypesAndTrust.

func TestParseManifestSessionStreamsValidation(t *testing.T) {
	tests := []struct {
		name      string
		data      string
		wantErr   bool
		errSubstr string
	}{
		{
			name: "accepts lua plugin with session_streams",
			data: `
name: my-plugin
version: 1.0.0
type: lua
session_streams: true
lua-plugin:
  entry: main.lua
`,
		},
		{
			name: "accepts binary plugin with session_streams",
			data: `
name: my-plugin
version: 1.0.0
type: binary
session_streams: true
binary-plugin:
  executable: plugin
`,
		},
		{
			name: "rejects setting plugin with session_streams",
			data: `
name: my-plugin
version: 1.0.0
type: setting
session_streams: true
setting:
  display_name: My World
  content_dir: content
  starting_location: start
`,
			wantErr:   true,
			errSubstr: "session_streams",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := plugins.ParseManifest([]byte(tt.data))
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
			} else {
				require.NoError(t, err)
				assert.True(t, m.SessionStreams)
			}
		})
	}
}

func TestManifestAcceptsLuaPluginWithVerbs(t *testing.T) {
	data := []byte(`
name: channels
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
verbs:
  - type: channels:say
    category: communication
    format: speech
    label: says
    display_target: terminal
  - type: channels:pose
    category: communication
    format: action
    display_target: both
`)
	m, err := plugins.ParseManifest(data)
	require.NoError(t, err)
	require.Len(t, m.Verbs, 2)

	assert.Equal(t, "channels:say", m.Verbs[0].Type)
	assert.Equal(t, "communication", m.Verbs[0].Category)
	assert.Equal(t, "speech", m.Verbs[0].Format)
	assert.Equal(t, "says", m.Verbs[0].Label)
	assert.Equal(t, "terminal", m.Verbs[0].DisplayTarget)

	assert.Equal(t, "channels:pose", m.Verbs[1].Type)
	assert.Equal(t, "action", m.Verbs[1].Format)
	assert.Equal(t, "both", m.Verbs[1].DisplayTarget)
}

func TestManifestAcceptsBinaryPluginWithVerbs(t *testing.T) {
	data := []byte(`
name: scenes
version: 1.0.0
type: binary
binary-plugin:
  executable: scenes
verbs:
  - type: scenes:narrate
    category: state
    format: narrative
    display_target: terminal
`)
	m, err := plugins.ParseManifest(data)
	require.NoError(t, err)
	require.Len(t, m.Verbs, 1)
	assert.Equal(t, "scenes:narrate", m.Verbs[0].Type)
}

func TestManifestRejectsSettingPluginWithVerbs(t *testing.T) {
	data := []byte(`
name: my-setting
version: 1.0.0
type: setting
setting:
  display_name: My Setting
  content_dir: content
  starting_location: start
verbs:
  - type: custom.say
    category: communication
    format: speech
    label: says
    display_target: terminal
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "setting plugins must not declare verbs")
}

func TestManifestRejectsVerbWithEmptyType(t *testing.T) {
	data := []byte(`
name: channels
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
verbs:
  - type: ""
    category: communication
    format: action
    display_target: terminal
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verb type must not be empty")
}

// Verifies: INV-PLUGIN-40
func TestManifestValidateRejectsUnqualifiedVerbType(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"bare":        "say",
		"foreign":     "other:say",
		"multi-colon": "demo:say:extra",
		"prefix-only": "demo:",
	}
	for name, verbType := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := &plugins.Manifest{
				Name: "demo", Version: "1.0.0", Type: plugins.TypeLua,
				LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
				Verbs: []plugins.VerbSpec{{
					Type: verbType, Category: "communication", Format: "action", DisplayTarget: "terminal",
				}},
			}
			err := m.Validate()
			require.Error(t, err)
			oopsErr, ok := oops.AsOops(err)
			require.True(t, ok, "error must be an oops error")
			require.Equal(t, "PLUGIN_WIRE_TYPE_NOT_QUALIFIED", oopsErr.Code())
		})
	}
}

func TestManifestValidateAcceptsQualifiedVerbType(t *testing.T) {
	t.Parallel()
	m := &plugins.Manifest{
		Name: "demo", Version: "1.0.0", Type: plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
		Verbs: []plugins.VerbSpec{{
			Type: "demo:say", Category: "communication", Format: "action", DisplayTarget: "terminal",
		}},
	}
	require.NoError(t, m.Validate())
}

func TestManifestRejectsVerbWithUnknownCategory(t *testing.T) {
	data := []byte(`
name: channels
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
verbs:
  - type: channels:say
    category: unknown-category
    format: speech
    label: says
    display_target: terminal
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown verb category")
}

func TestManifestRejectsVerbWithUnknownFormat(t *testing.T) {
	data := []byte(`
name: channels
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
verbs:
  - type: channels:say
    category: communication
    format: unknown-format
    display_target: terminal
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown verb format")
}

func TestManifestRejectsVerbWithUnknownDisplayTarget(t *testing.T) {
	data := []byte(`
name: channels
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
verbs:
  - type: channels:say
    category: communication
    format: action
    display_target: unknown-target
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown verb display_target")
}

func TestManifestRejectsSpeechVerbWithoutLabel(t *testing.T) {
	data := []byte(`
name: channels
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
verbs:
  - type: channels:say
    category: communication
    format: speech
    display_target: terminal
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "label is required when verb format is speech")
}

func TestManifestRejectsDuplicateVerbType(t *testing.T) {
	data := []byte(`
name: channels
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
verbs:
  - type: channels:say
    category: communication
    format: action
    display_target: terminal
  - type: channels:say
    category: communication
    format: narrative
    display_target: both
`)
	_, err := plugins.ParseManifest(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate verb type")
}

func TestManifestDisplayTargetCaseInsensitive(t *testing.T) {
	targets := []string{"terminal", "TERMINAL", "Terminal", "state", "STATE", "State", "both", "BOTH", "Both"}
	for _, target := range targets {
		t.Run(fmt.Sprintf("accepts display_target %s", target), func(t *testing.T) {
			data := []byte(fmt.Sprintf(`
name: channels
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
verbs:
  - type: channels:say
    category: communication
    format: action
    display_target: %s
`, target))
			_, err := plugins.ParseManifest(data)
			require.NoError(t, err, "display_target %q should be accepted", target)
		})
	}
}

func TestManifestAcceptsPluginWithNoVerbs(t *testing.T) {
	data := []byte(`
name: channels
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`)
	m, err := plugins.ParseManifest(data)
	require.NoError(t, err)
	assert.Empty(t, m.Verbs)
}

// TestManifestActorKindsClaimableValidation covers spec §3.2 validation
// rules for the actor_kinds_claimable manifest field.
func TestManifestActorKindsClaimableValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		yaml           string
		wantErr        bool
		wantErrCode    string
		wantErrSubstr  string
		wantNormalized []string
	}{
		{
			name: "absent field defaults to [plugin]",
			yaml: `
name: test
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`,
			wantNormalized: []string{"plugin"},
		},
		{
			name: "explicit [plugin] loads as canonical",
			yaml: `
name: test
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
actor_kinds_claimable: [plugin]
`,
			wantNormalized: []string{"plugin"},
		},
		{
			name: "[plugin, character] loads preserving order",
			yaml: `
name: test
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
actor_kinds_claimable: [plugin, character]
`,
			wantNormalized: []string{"plugin", "character"},
		},
		{
			name: "empty list rejected for missing plugin",
			yaml: `
name: test
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
actor_kinds_claimable: []
`,
			// Spec §5.2 + §2 G5: declared-empty MUST loudly error, not
			// silently default to [plugin]. Distinguishes operator's typed
			// `[]` (loud) from the field being absent (silent default).
			wantErr:     true,
			wantErrCode: "MANIFEST_ACTOR_KINDS_MISSING_PLUGIN",
		},
		{
			name: "[character] alone rejected for missing plugin",
			yaml: `
name: test
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
actor_kinds_claimable: [character]
`,
			wantErr:     true,
			wantErrCode: "MANIFEST_ACTOR_KINDS_MISSING_PLUGIN",
		},
		{
			name: "[plugin, system] rejected because system is forbidden",
			yaml: `
name: test
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
actor_kinds_claimable: [plugin, system]
`,
			wantErr:     true,
			wantErrCode: "MANIFEST_ACTOR_KIND_SYSTEM_FORBIDDEN",
		},
		{
			name: "[plugin, frobnicate] rejected as unknown kind",
			yaml: `
name: test
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
actor_kinds_claimable: [plugin, frobnicate]
`,
			wantErr:     true,
			wantErrCode: "MANIFEST_ACTOR_KIND_UNKNOWN",
		},
		{
			name: "duplicates silently dedup preserving first occurrence",
			yaml: `
name: test
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
actor_kinds_claimable: [plugin, plugin, character]
`,
			wantNormalized: []string{"plugin", "character"},
		},
		{
			name: "non-sequence yaml value is rejected as malformed",
			yaml: `
name: test
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
actor_kinds_claimable: plugin
`,
			wantErr:     true,
			wantErrCode: "MANIFEST_ACTOR_KINDS_MALFORMED",
			// Pre-Decode kind check produces a manifest-namespaced error
			// pointing at the offending key, rather than yaml.v3's generic
			// "cannot unmarshal !!str into []string" type error.
			wantErrSubstr: "actor_kinds_claimable must be declared as a YAML sequence",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, err := plugins.ParseManifest([]byte(tt.yaml))
			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrCode != "" {
					errutil.AssertErrorCode(t, err, tt.wantErrCode)
				}
				if tt.wantErrSubstr != "" {
					assert.Contains(t, err.Error(), tt.wantErrSubstr)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantNormalized, m.ActorKindsClaimable)
		})
	}
}

// TestManifestDeclaresActorKindClaimable verifies the helper used by the
// manifest gate (Task 5) returns the correct boolean for each ActorKind.
func TestManifestDeclaresActorKindClaimable(t *testing.T) {
	t.Parallel()

	t.Run("returns true for declared plugin and character kinds", func(t *testing.T) {
		t.Parallel()
		m := &plugins.Manifest{ActorKindsClaimable: []string{"plugin", "character"}}
		assert.True(t, m.DeclaresActorKindClaimable(core.ActorPlugin))
		assert.True(t, m.DeclaresActorKindClaimable(core.ActorCharacter))
		assert.False(t, m.DeclaresActorKindClaimable(core.ActorSystem))
	})

	t.Run("returns false for character when only plugin declared", func(t *testing.T) {
		t.Parallel()
		m := &plugins.Manifest{ActorKindsClaimable: []string{"plugin"}}
		assert.True(t, m.DeclaresActorKindClaimable(core.ActorPlugin))
		assert.False(t, m.DeclaresActorKindClaimable(core.ActorCharacter))
		assert.False(t, m.DeclaresActorKindClaimable(core.ActorSystem))
	})

	t.Run("returns false for system kind on validated manifests", func(t *testing.T) {
		t.Parallel()
		// Manifest validation rejects "system" in ActorKindsClaimable
		// (MANIFEST_ACTOR_KIND_SYSTEM_FORBIDDEN), so any manifest reaching
		// the gate cannot have "system" in its slice. The helper trusts the
		// validation contract and uses kind.String() for lookup; the
		// "system" case falls through the loop naturally.
		m := &plugins.Manifest{ActorKindsClaimable: []string{"plugin", "character"}}
		assert.False(t, m.DeclaresActorKindClaimable(core.ActorSystem))
	})

	t.Run("returns false when ActorKindsClaimable is empty", func(t *testing.T) {
		t.Parallel()
		m := &plugins.Manifest{}
		assert.False(t, m.DeclaresActorKindClaimable(core.ActorPlugin))
		assert.False(t, m.DeclaresActorKindClaimable(core.ActorCharacter))
		assert.False(t, m.DeclaresActorKindClaimable(core.ActorSystem))
	})
}

func TestManifestCarriesCryptoSection(t *testing.T) {
	src := `
name: test-plugin
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
crypto:
  emits:
    - event_type: foo
      sensitivity: always
`
	m, err := plugins.ParseManifest([]byte(src))
	require.NoError(t, err)
	require.NotNil(t, m.Crypto)
	require.Len(t, m.Crypto.Emits, 1)
	assert.Equal(t, "foo", m.Crypto.Emits[0].EventType)
	assert.Equal(t, plugins.SensitivityAlways, m.Crypto.Emits[0].Sensitivity)
}

func TestManifestWithoutCryptoSectionLeavesCryptoNil(t *testing.T) {
	src := `
name: test-plugin
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`
	m, err := plugins.ParseManifest([]byte(src))
	require.NoError(t, err)
	assert.Nil(t, m.Crypto)
}

func TestParseManifestConfigBlock(t *testing.T) {
	data := []byte(`
name: demo
version: 1.0.0
type: binary
binary-plugin:
  executable: demo
config:
  vote_window:
    type: duration
    default: 168h
    required: true
    description: "vote collection window"
  max_attempts:
    type: int
    default: "3"
`)
	m, err := plugins.ParseManifest(data)
	require.NoError(t, err)
	require.Len(t, m.Config, 2)
	require.Equal(t, "duration", m.Config["vote_window"].Type)
	require.Equal(t, "168h", m.Config["vote_window"].Default)
	require.True(t, m.Config["vote_window"].Required)
	require.Equal(t, "vote collection window", m.Config["vote_window"].Description)
	require.Equal(t, "int", m.Config["max_attempts"].Type)
	require.False(t, m.Config["max_attempts"].Required)
}

func TestManifest_HistoryScopeValidation(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		wantErr     bool
		errContains string
	}{
		{
			name: "plugin with no emits does not require history_scope",
			yaml: `
name: no-emits
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`,
			wantErr: false,
		},
		{
			name: "plugin emitting events with history_scope=grid is accepted",
			yaml: `
name: grid-plugin
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
emits: [location]
history_scope: grid
`,
			wantErr: false,
		},
		{
			name: "plugin emitting events with history_scope=scene is accepted",
			yaml: `
name: scene-plugin
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
emits: [scene]
history_scope: scene
`,
			wantErr: false,
		},
		{
			name: "plugin emitting events with history_scope=custom is accepted",
			yaml: `
name: custom-plugin
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
emits: [custom-ns]
history_scope: custom
`,
			wantErr: false,
		},
		{
			name: "plugin emitting events without history_scope is rejected",
			yaml: `
name: missing-scope
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
emits: [location]
`,
			wantErr:     true,
			errContains: "history_scope",
		},
		{
			name: "plugin with unknown history_scope value is rejected",
			yaml: `
name: bad-scope
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
emits: [location]
history_scope: unknown
`,
			wantErr:     true,
			errContains: "history_scope",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := plugins.ParseManifest([]byte(tt.yaml))
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// validBaseManifest builds a minimal Lua manifest (bypassing ParseManifest so
// negative-case tests can call Validate() and assert specific error codes rather
// than having ParseManifest swallow the error and return nil).
func validBaseManifest(t *testing.T, frag string) *plugins.Manifest {
	t.Helper()
	base := "name: test-plugin\nversion: 1.0.0\ntype: lua\nlua-plugin:\n  entry: main.lua\n"
	var m plugins.Manifest
	require.NoError(t, yaml.Unmarshal([]byte(base+frag), &m))
	return &m
}

// Verifies: INV-PLUGIN-53
func TestManifestRejectsLeastPrivilegeParamsOnServiceEntry(t *testing.T) {
	for _, tc := range []struct{ name, yamlFrag, code string }{
		{"scope on service", "requires:\n  - service: holomush.scene.v1.SceneService\n    scope: own-location\n", "LEAST_PRIVILEGE_PARAM_ON_SERVICE"},
		{"access on service", "requires:\n  - service: holomush.scene.v1.SceneService\n    access: read\n", "LEAST_PRIVILEGE_PARAM_ON_SERVICE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := validBaseManifest(t, tc.yamlFrag)
			errutil.AssertErrorCode(t, m.Validate(), tc.code)
		})
	}
}

func TestManifestRejectsUnknownAccessValue(t *testing.T) {
	m := validBaseManifest(t, "requires:\n  - capability: kv\n    access: delete\n")
	errutil.AssertErrorCode(t, m.Validate(), "INVALID_ACCESS_VALUE")
}

func TestManifestAcceptsAccessReadOnCapability(t *testing.T) {
	m := validBaseManifest(t, "requires:\n  - capability: kv\n    access: read\n")
	require.NoError(t, m.Validate())
}

func TestManifestRejectsUnknownScopeToken(t *testing.T) {
	m := validBaseManifest(t, "requires:\n  - capability: world.mutation\n    scope: own-galaxy\n")
	errutil.AssertErrorCode(t, m.Validate(), "UNKNOWN_SCOPE_TOKEN")
}

func TestManifestAcceptsKnownScopeTokenOnCapability(t *testing.T) {
	m := validBaseManifest(t, "requires:\n  - capability: world.mutation\n    scope: own-location\n")
	require.NoError(t, m.Validate())
}
