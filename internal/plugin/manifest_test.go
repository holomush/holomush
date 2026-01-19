// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugin_test

import (
	"strings"
	"testing"

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
	if err != nil {
		t.Fatalf("ParseManifest() error = %v", err)
	}

	if m.Name != "echo-bot" {
		t.Errorf("Name = %q, want %q", m.Name, "echo-bot")
	}
	if m.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", m.Version, "1.0.0")
	}
	if m.Type != plugin.TypeLua {
		t.Errorf("Type = %v, want %v", m.Type, plugin.TypeLua)
	}
	if len(m.Events) != 2 {
		t.Errorf("len(Events) = %d, want 2", len(m.Events))
	}
	if len(m.Capabilities) != 1 {
		t.Errorf("len(Capabilities) = %d, want 1", len(m.Capabilities))
	}
	if m.LuaPlugin == nil || m.LuaPlugin.Entry != "main.lua" {
		t.Errorf("LuaPlugin.Entry not set correctly")
	}
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
	if err != nil {
		t.Fatalf("ParseManifest() error = %v", err)
	}

	if m.Type != plugin.TypeBinary {
		t.Errorf("Type = %v, want %v", m.Type, plugin.TypeBinary)
	}
	if m.BinaryPlugin == nil || m.BinaryPlugin.Executable != "combat-${os}-${arch}" {
		t.Errorf("BinaryPlugin.Executable not set correctly")
	}
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
			if err == nil {
				t.Fatal("expected error for invalid name")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want error containing %q", err, tt.wantErr)
			}
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
			if err != nil {
				t.Errorf("ParseManifest() error = %v for name %q", err, tt.plugName)
			}
			if m != nil && m.Name != tt.plugName {
				t.Errorf("Name = %q, want %q", m.Name, tt.plugName)
			}
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
			if err == nil {
				t.Errorf("expected error for %s", tt.name)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want error containing %q", err, tt.wantErr)
			}
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
			if err == nil {
				t.Errorf("expected error for %s", tt.name)
			}
		})
	}
}

func TestParseManifest_InvalidYAML(t *testing.T) {
	yaml := `name: test
version: 1.0.0
type: [invalid`
	_, err := plugin.ParseManifest([]byte(yaml))
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
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
	if err := m.Validate(); err != nil {
		t.Errorf("Validate() error = %v", err)
	}
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
	if err := m.Validate(); err == nil {
		t.Error("Validate() should fail for empty entry")
	}
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
	if err := m.Validate(); err == nil {
		t.Error("Validate() should fail for empty executable")
	}
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
			if err == nil {
				t.Error("ParseManifest() should return error for empty input")
			}
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
			if err == nil {
				t.Fatalf("expected error for version %q", tt.version)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want error containing %q", err, tt.wantErr)
			}
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
			if err != nil {
				t.Errorf("ParseManifest() error = %v for version %q", err, tt.version)
			}
			if m != nil && m.Version != tt.version {
				t.Errorf("Version = %q, want %q", m.Version, tt.version)
			}
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
				if err == nil {
					t.Fatalf("expected error for engine %q", tt.engine)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseManifest() error = %v for engine %q", err, tt.engine)
			}
			if m.Engine != tt.wantEngine {
				t.Errorf("Engine = %q, want %q", m.Engine, tt.wantEngine)
			}
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
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseManifest() error = %v", err)
			}
			if len(m.Dependencies) != len(tt.wantDep) {
				t.Errorf("len(Dependencies) = %d, want %d", len(m.Dependencies), len(tt.wantDep))
			}
			for k, v := range tt.wantDep {
				if m.Dependencies[k] != v {
					t.Errorf("Dependencies[%q] = %q, want %q", k, m.Dependencies[k], v)
				}
			}
		})
	}
}
