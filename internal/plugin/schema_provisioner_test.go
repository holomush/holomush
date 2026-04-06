// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPluginSchemaName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"prepends plugin prefix to simple name", "scenes", "plugin_scenes"},
		{"converts hyphens to underscores", "core-scenes", "plugin_core_scenes"},
		{"handles multiple hyphens", "my-cool-plugin", "plugin_my_cool_plugin"},
		{"preserves underscores in original name", "core_utils", "plugin_core_utils"},
		{"handles single character name", "x", "plugin_x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, pluginSchemaName(tt.input))
		})
	}
}

func TestPluginRoleName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"prepends holomush_plugin prefix", "scenes", "holomush_plugin_scenes"},
		{"converts hyphens to underscores", "core-scenes", "holomush_plugin_core_scenes"},
		{"handles multiple hyphens", "my-cool-plugin", "holomush_plugin_my_cool_plugin"},
		{"preserves underscores", "core_utils", "holomush_plugin_core_utils"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, pluginRoleName(tt.input))
		})
	}
}

func TestGeneratePasswordProduces256BitsOfEntropy(t *testing.T) {
	pw, err := generatePassword()
	require.NoError(t, err)

	// 32 bytes base64url-encoded without padding = 43 chars
	assert.Len(t, pw, 43)
}

func TestGeneratePasswordIsUnique(t *testing.T) {
	pw1, err := generatePassword()
	require.NoError(t, err)
	pw2, err := generatePassword()
	require.NoError(t, err)

	assert.NotEqual(t, pw1, pw2)
}

func TestPluginConnStringReplacesCredentialsAndSearchPath(t *testing.T) {
	base := "postgres://serveruser:serverpass@localhost:5432/dbname?sslmode=disable"
	got, err := pluginConnString(base, "plugin_core_scenes", "holomush_plugin_core_scenes", "s3cret")
	require.NoError(t, err)

	u, err := url.Parse(got)
	require.NoError(t, err)

	assert.Equal(t, "holomush_plugin_core_scenes", u.User.Username())
	pw, ok := u.User.Password()
	assert.True(t, ok)
	assert.Equal(t, "s3cret", pw)
	assert.Equal(t, "plugin_core_scenes", u.Query().Get("search_path"))
	assert.Equal(t, "disable", u.Query().Get("sslmode"))
	assert.Equal(t, "localhost:5432", u.Host)
	assert.Equal(t, "/dbname", u.Path)
}

func TestPluginConnStringRejectsInvalidURL(t *testing.T) {
	_, err := pluginConnString("://bad", "s", "r", "p")
	require.Error(t, err)
}

func TestNewSchemaProvisionerStoresBaseConnString(t *testing.T) {
	base := "postgres://user:pass@localhost:5432/db"
	sp := NewSchemaProvisioner(base)
	assert.Equal(t, base, sp.baseConnString)
	assert.Nil(t, sp.pool)
}

func TestCloseIsNoOpWithoutInit(t *testing.T) { //nolint:revive // t required by testing framework
	sp := NewSchemaProvisioner("postgres://localhost/db")
	sp.Close() // must not panic
}
