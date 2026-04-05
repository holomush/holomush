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

func TestScopedConnStringAppendsSearchPath(t *testing.T) {
	base := "postgres://user:pass@localhost:5432/dbname?sslmode=disable"
	got, err := scopedConnString(base, "plugin_core_scenes")
	require.NoError(t, err)

	u, err := url.Parse(got)
	require.NoError(t, err)

	assert.Equal(t, "plugin_core_scenes", u.Query().Get("search_path"))
	assert.Equal(t, "disable", u.Query().Get("sslmode"))
	assert.Equal(t, "localhost:5432", u.Host)
	assert.Equal(t, "/dbname", u.Path)
}

func TestScopedConnStringReplacesExistingSearchPath(t *testing.T) {
	base := "postgres://user:pass@host:5432/db?search_path=public"
	got, err := scopedConnString(base, "plugin_foo")
	require.NoError(t, err)

	u, err := url.Parse(got)
	require.NoError(t, err)

	assert.Equal(t, "plugin_foo", u.Query().Get("search_path"))
}

func TestScopedConnStringRejectsInvalidURL(t *testing.T) {
	_, err := scopedConnString("://not-a-url", "plugin_foo")
	require.Error(t, err)
}

func TestNewSchemaProvisionerStoresBaseConnString(t *testing.T) {
	base := "postgres://user:pass@localhost:5432/db"
	sp := NewSchemaProvisioner(base)
	assert.Equal(t, base, sp.baseConnString)
	assert.Nil(t, sp.pool)
}

func TestCloseIsNoOpWithoutInit(t *testing.T) {
	sp := NewSchemaProvisioner("postgres://localhost/db")
	sp.Close() // must not panic
}
