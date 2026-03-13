// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
)

func TestULIDToStringPtr(t *testing.T) {
	t.Run("nil returns nil", func(t *testing.T) {
		result := ulidToStringPtr(nil)
		assert.Nil(t, result)
	})

	t.Run("valid ULID returns string pointer", func(t *testing.T) {
		id := ulid.Make()
		result := ulidToStringPtr(&id)
		assert.NotNil(t, result)
		assert.Equal(t, id.String(), *result)
	})
}

func TestParseOptionalULID(t *testing.T) {
	t.Run("nil returns nil", func(t *testing.T) {
		result, err := parseOptionalULID(nil, "test_field")
		assert.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("valid ULID string returns parsed ULID", func(t *testing.T) {
		id := ulid.Make()
		idStr := id.String()
		result, err := parseOptionalULID(&idStr, "test_field")
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, id, *result)
	})

	t.Run("invalid ULID string returns error", func(t *testing.T) {
		invalid := "not-a-ulid"
		result, err := parseOptionalULID(&invalid, "my_field")
		assert.Error(t, err)
		assert.Nil(t, result)
		// Error is wrapped with oops context (my_field) - the context is available
		// via oops.AsOops(err) but the base error message comes from ulid.Parse
		assert.Contains(t, err.Error(), "bad data size")
	})
}
