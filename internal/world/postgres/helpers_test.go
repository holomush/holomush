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
