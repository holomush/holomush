// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewULID(t *testing.T) {
	id1 := NewULID()
	id2 := NewULID()

	assert.NotEmpty(t, id1.String(), "ULID should not be empty")
	assert.NotEqual(t, id1.String(), id2.String(), "Two ULIDs should be different")
	// ULIDs should be lexicographically sortable by time
	assert.LessOrEqual(t, id1.String(), id2.String(), "Later ULID should sort after earlier ULID")
}

func TestParseULID(t *testing.T) {
	original := NewULID()
	parsed, err := ParseULID(original.String())
	require.NoError(t, err)
	assert.Equal(t, original, parsed)
}

func TestParseULID_Invalid(t *testing.T) {
	_, err := ParseULID("invalid")
	assert.Error(t, err, "ParseULID should fail on invalid input")
}
