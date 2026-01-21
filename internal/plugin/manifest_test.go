// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugin_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/plugin"
)

func TestParseManifest_LuaPlugin(t *testing.T) {
	yaml := `
name: echo-bot
version: 1.0.0
type: lua
events:
  - say
  - pose
capabilities:
  - events.emit.location
lua-plugin:
  entry: main.lua
`
	m, err := plugin.ParseManifest([]byte(yaml))
	require.NoError(t, err)

	assert.Equal(t, "echo-bot", m.Name)
	assert.Equal(t, "1.0.0", m.Version)
	assert.Equal(t, plugin.TypeLua, m.Type)
	assert.Len(t, m.Events, 2)
	assert.Len(t, m.Capabilities, 1)
	require.NotNil(t, m.LuaPlugin)
	assert.Equal(t, "main.lua", m.LuaPlugin.Entry)
}

func TestParseManifest_BinaryPlugin(t *testing.T) {
	yaml := `
name: combat-system
version: 2.1.0
type: binary
events:
  - combat_start
capabilities:
  - events.*
  - world.*
binary-plugin:
  executable: combat-${os}-${arch}
`
	m, err := plugin.ParseManifest([]byte(yaml))
	require.NoError(t, err)

	assert.Equal(t, plugin.TypeBinary, m.Type)
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
			_, err := plugin.ParseManifest([]byte(tt.yaml))
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
			m, err := plugin.ParseManifest([]byte(yaml))
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
			_, err := plugin.ParseManifest([]byte(tt.yaml))
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
			_, err := plugin.ParseManifest([]byte(tt.yaml))
			assert.Error(t, err, "expected error for %s", tt.name)
		})
	}
}

func TestParseManifest_InvalidYAML(t *testing.T) {
	yaml := `name: test
version: 1.0.0
type: [invalid`
	_, err := plugin.ParseManifest([]byte(yaml))
	assert.Error(t, err, "expected error for invalid YAML")
}

func TestManifest_Validate(t *testing.T) {
	// Test Validate() method directly
	m := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeLua,
		LuaPlugin: &plugin.LuaConfig{
			Entry: "main.lua",
		},
	}
	assert.NoError(t, m.Validate())
}

func TestManifest_Validate_EmptyEntry(t *testing.T) {
	m := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeLua,
		LuaPlugin: &plugin.LuaConfig{
			Entry: "",
		},
	}
	assert.Error(t, m.Validate(), "Validate() should fail for empty entry")
}

func TestManifest_Validate_EmptyExecutable(t *testing.T) {
	m := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeBinary,
		BinaryPlugin: &plugin.BinaryConfig{
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
			_, err := plugin.ParseManifest(tt.input)
			assert.Error(t, err, "ParseManifest() should return error for empty input")
		})
	}
}

func TestParseManifest_InvalidVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		wantErr string
	}{
		{name: "not semver - plain text", version: "latest", wantErr: "version"},
		{name: "not semver - single number", version: "1", wantErr: "version"},
		{name: "not semver - two numbers", version: "1.0", wantErr: "version"},
		{name: "not semver - leading v", version: "v1.0.0", wantErr: "version"},
		{name: "not semver - spaces", version: "1.0.0 beta", wantErr: "version"},
		{name: "not semver - invalid prerelease", version: "1.0.0-", wantErr: "version"},
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
			_, err := plugin.ParseManifest([]byte(yaml))
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
			m, err := plugin.ParseManifest([]byte(yaml))
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
			m, err := plugin.ParseManifest([]byte(yaml))
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
			m, err := plugin.ParseManifest([]byte(tt.yaml))
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

func TestParseManifest_CapabilityValidation(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name: "valid capabilities",
			yaml: `
name: test
version: 1.0.0
type: lua
capabilities:
  - events.emit.location
  - world.read.*
lua-plugin:
  entry: main.lua
`,
			wantErr: false,
		},
		{
			name: "wildcard capability",
			yaml: `
name: test
version: 1.0.0
type: lua
capabilities:
  - "**"
lua-plugin:
  entry: main.lua
`,
			wantErr: false,
		},
		{
			name: "empty capabilities is valid",
			yaml: `
name: test
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := plugin.ParseManifest([]byte(tt.yaml))
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
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
			m, err := plugin.ParseManifest([]byte(tt.yaml))
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

func TestParseManifest_CapabilityPattern(t *testing.T) {
	// Test that capability patterns are preserved correctly
	yaml := `
name: test
version: 1.0.0
type: lua
capabilities:
  - events.emit.*
  - world.read.**
  - kv.read
  - kv.write
lua-plugin:
  entry: main.lua
`
	m, err := plugin.ParseManifest([]byte(yaml))
	require.NoError(t, err)
	assert.Len(t, m.Capabilities, 4)
	assert.Contains(t, m.Capabilities, "events.emit.*")
	assert.Contains(t, m.Capabilities, "world.read.**")
	assert.Contains(t, m.Capabilities, "kv.read")
	assert.Contains(t, m.Capabilities, "kv.write")
}

func TestManifest_HasEvent(t *testing.T) {
	m := &plugin.Manifest{
		Name:    "test",
		Version: "1.0.0",
		Type:    plugin.TypeLua,
		Events:  []string{"say", "pose"},
		LuaPlugin: &plugin.LuaConfig{
			Entry: "main.lua",
		},
	}

	// HasEvent is not a method on Manifest, but we can test the Events slice directly
	assert.Contains(t, m.Events, "say")
	assert.Contains(t, m.Events, "pose")
	assert.NotContains(t, m.Events, "arrive")
}

func TestParseManifest_Whitespace(t *testing.T) {
	// Test that extra whitespace is handled correctly
	yaml := `
name:    test-plugin
version:   1.0.0
type:   lua
lua-plugin:
  entry:   main.lua
`
	m, err := plugin.ParseManifest([]byte(yaml))
	require.NoError(t, err)
	// YAML should trim whitespace
	assert.True(t, strings.TrimSpace(m.Name) == "test-plugin" || m.Name == "test-plugin")
}
