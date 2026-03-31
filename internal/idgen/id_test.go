// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package idgen

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_ReturnsValidULID(t *testing.T) {
	got := New()
	require.NotEqual(t, ulid.ULID{}, got)
}

func TestNew_ReturnsUniqueValues(t *testing.T) {
	seen := make(map[ulid.ULID]struct{}, 100)
	for range 100 {
		id := New()
		_, duplicate := seen[id]
		assert.False(t, duplicate, "generated duplicate ULID: %s", id)
		seen[id] = struct{}{}
	}
}

func TestNew_HasNonDecreasingTimestamps(t *testing.T) {
	prev := New()
	for range 10 {
		next := New()
		assert.True(t, next.Time() >= prev.Time(), "next.Time() should be >= prev.Time()")
		prev = next
	}
}
