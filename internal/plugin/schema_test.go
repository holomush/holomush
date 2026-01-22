// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugin_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/plugin"
)

func TestValidateSchema_ValidLuaManifest(t *testing.T) {
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
	err := plugin.ValidateSchema([]byte(yaml))
	assert.NoError(t, err)
}

func TestValidateSchema_ValidBinaryManifest(t *testing.T) {
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
  executable: combat-linux-amd64
`
	err := plugin.ValidateSchema([]byte(yaml))
	assert.NoError(t, err)
}

func TestValidateSchema_NameTooLong(t *testing.T) {
	// 65 characters - one over the 64 char limit (boundary test)
	yaml := `
name: a2345678901234567890123456789012345678901234567890123456789012345
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`
	err := plugin.ValidateSchema([]byte(yaml))
	assert.Error(t, err, "ValidateSchema() expected error for name exceeding 64 chars")
}

func TestValidateSchema_NameExactlyMaxLength(t *testing.T) {
	// Exactly 64 characters - should be valid (boundary test)
	yaml := `
name: a234567890123456789012345678901234567890123456789012345678901234
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`
	err := plugin.ValidateSchema([]byte(yaml))
	assert.NoError(t, err, "ValidateSchema() error for 64 char name")
}

func TestValidateSchema_MissingRequiredFields(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "missing name",
			yaml: `
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`,
		},
		{
			name: "missing version",
			yaml: `
name: test
type: lua
lua-plugin:
  entry: main.lua
`,
		},
		{
			name: "missing type",
			yaml: `
name: test
version: 1.0.0
lua-plugin:
  entry: main.lua
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := plugin.ValidateSchema([]byte(tt.yaml))
			assert.Error(t, err, "ValidateSchema() expected error for %s", tt.name)
		})
	}
}

func TestValidateSchema_InvalidNamePattern(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "uppercase not allowed",
			yaml: `
name: Invalid-Name
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`,
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
		},
		{
			name: "consecutive hyphens not allowed",
			yaml: `
name: test--plugin
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`,
		},
		{
			name: "trailing hyphen not allowed",
			yaml: `
name: test-plugin-
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := plugin.ValidateSchema([]byte(tt.yaml))
			assert.Error(t, err, "ValidateSchema() expected error for %s", tt.name)
		})
	}
}

func TestValidateSchema_InvalidType(t *testing.T) {
	yaml := `
name: test
version: 1.0.0
type: wasm
lua-plugin:
  entry: main.lua
`
	err := plugin.ValidateSchema([]byte(yaml))
	assert.Error(t, err, "ValidateSchema() expected error for invalid type")
}

func TestValidateSchema_EmptyInput(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{name: "nil input", input: nil},
		{name: "empty slice", input: []byte{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := plugin.ValidateSchema(tt.input)
			assert.Error(t, err, "ValidateSchema() expected error for empty input")
		})
	}
}

func TestGenerateSchema(t *testing.T) {
	schema, err := plugin.GenerateSchema()
	require.NoError(t, err)

	// Schema should be valid JSON
	assert.NotEmpty(t, schema, "GenerateSchema() returned empty schema")

	// Schema should contain expected fields
	schemaStr := string(schema)
	expectedFields := []string{
		`"name"`,
		`"version"`,
		`"type"`,
		`"lua-plugin"`,
		`"binary-plugin"`,
		`"$schema"`,
	}
	for _, field := range expectedFields {
		assert.Contains(t, schemaStr, field, "GenerateSchema() missing expected field %s", field)
	}
}

func TestResetSchemaCache(t *testing.T) {
	// First validation compiles and caches the schema
	yaml := `
name: test
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`
	err := plugin.ValidateSchema([]byte(yaml))
	require.NoError(t, err)

	// Reset cache
	plugin.ResetSchemaCache()

	// Validation should still work (recompiles schema)
	err = plugin.ValidateSchema([]byte(yaml))
	assert.NoError(t, err, "ValidateSchema() after reset")
}

func TestGetSchemaID(t *testing.T) {
	id := plugin.GetSchemaID()
	assert.NotEmpty(t, id, "GetSchemaID() returned empty string")
	assert.Contains(t, id, "holomush", "GetSchemaID() should contain 'holomush'")
}

func TestFormatSchemaError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		want    string
		wantLen int
	}{
		{
			name:    "nil error",
			err:     nil,
			want:    "",
			wantLen: 0,
		},
		{
			name:    "simple error",
			err:     fmt.Errorf("test error"),
			want:    "test error",
			wantLen: 10,
		},
		{
			name:    "schema validation error",
			err:     fmt.Errorf("schema validation failed: missing required field"),
			want:    "missing required field",
			wantLen: 22,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := plugin.FormatSchemaError(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValidateSchema_InvalidYAML(t *testing.T) {
	yaml := `name: test
version: 1.0.0
type: [invalid`
	err := plugin.ValidateSchema([]byte(yaml))
	assert.Error(t, err, "ValidateSchema() expected error for invalid YAML")
}

func TestValidateSchema_WithEngineField(t *testing.T) {
	yaml := `
name: test-plugin
version: 1.0.0
type: lua
engine: ">= 2.0.0"
lua-plugin:
  entry: main.lua
`
	err := plugin.ValidateSchema([]byte(yaml))
	assert.NoError(t, err, "ValidateSchema() for manifest with engine field")
}

func TestValidateSchema_WithDependenciesField(t *testing.T) {
	yaml := `
name: test-plugin
version: 1.0.0
type: lua
dependencies:
  auth-plugin: "^1.0.0"
  logging-plugin: "~2.0.0"
lua-plugin:
  entry: main.lua
`
	err := plugin.ValidateSchema([]byte(yaml))
	assert.NoError(t, err, "ValidateSchema() for manifest with dependencies field")
}

func TestValidateSchema_WithAllOptionalFields(t *testing.T) {
	yaml := `
name: full-plugin
version: 1.0.0
type: lua
engine: ">= 2.0.0, < 3.0.0"
dependencies:
  auth-plugin: "^1.0.0"
  logging-plugin: "~2.0.0"
events:
  - say
  - pose
capabilities:
  - events.emit.location
lua-plugin:
  entry: main.lua
`
	err := plugin.ValidateSchema([]byte(yaml))
	assert.NoError(t, err, "ValidateSchema() for manifest with all optional fields")
}

func TestGenerateSchema_ContainsRequiredFields(t *testing.T) {
	schema, err := plugin.GenerateSchema()
	require.NoError(t, err)

	schemaStr := string(schema)

	// Check for required fields marker
	assert.Contains(t, schemaStr, "required")

	// Check for type enum
	assert.Contains(t, schemaStr, "lua")
	assert.Contains(t, schemaStr, "binary")
}

func TestValidateSchema_ValidNamePatterns(t *testing.T) {
	validNames := []string{
		"a",
		"test",
		"test-plugin",
		"my-cool-plugin",
		"plugin123",
		"a234567890123456789012345678901234567890123456789012345678901234", // exactly 64 chars
	}

	for _, name := range validNames {
		t.Run(name, func(t *testing.T) {
			yaml := fmt.Sprintf(`
name: %s
version: 1.0.0
type: lua
lua-plugin:
  entry: main.lua
`, name)
			err := plugin.ValidateSchema([]byte(yaml))
			assert.NoError(t, err, "ValidateSchema() should accept valid name %q", name)
		})
	}
}

func TestValidateSchema_Capabilities(t *testing.T) {
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
	err := plugin.ValidateSchema([]byte(yaml))
	assert.NoError(t, err)
}

func TestValidateSchema_Events(t *testing.T) {
	yaml := `
name: test
version: 1.0.0
type: lua
events:
  - say
  - pose
  - arrive
  - leave
  - custom_event
lua-plugin:
  entry: main.lua
`
	err := plugin.ValidateSchema([]byte(yaml))
	assert.NoError(t, err)
}

func TestGetSchemaID_Format(t *testing.T) {
	id := plugin.GetSchemaID()

	// Should be a URI-like string
	assert.True(t, strings.HasPrefix(id, "https://") || strings.Contains(id, "holomush"),
		"Schema ID should be a URI or contain 'holomush': %s", id)
}
