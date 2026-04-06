// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseSchemaFromConnString(t *testing.T) {
	t.Run("extracts schema from search_path parameter", func(t *testing.T) {
		connStr := "postgres://user:pass@localhost/db?search_path=plugin_scenes"
		schema, err := ParseSchemaFromConnString(connStr)
		assert.NoError(t, err)
		assert.Equal(t, "plugin_scenes", schema)
	})

	t.Run("returns error when search_path is missing", func(t *testing.T) {
		connStr := "postgres://user:pass@localhost/db"
		_, err := ParseSchemaFromConnString(connStr)
		assert.Error(t, err)
	})

	t.Run("handles connection string with multiple parameters", func(t *testing.T) {
		connStr := "postgres://user:pass@localhost/db?sslmode=disable&search_path=plugin_channels"
		schema, err := ParseSchemaFromConnString(connStr)
		assert.NoError(t, err)
		assert.Equal(t, "plugin_channels", schema)
	})
}

func TestParseMigrationVersion(t *testing.T) {
	t.Run("extracts version number from migration filename", func(t *testing.T) {
		assert.Equal(t, 1, parseMigrationVersion("000001_create_scenes.up.sql"))
		assert.Equal(t, 12, parseMigrationVersion("000012_add_index.up.sql"))
		assert.Equal(t, 0, parseMigrationVersion("invalid"))
	})
}
