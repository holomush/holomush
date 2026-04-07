// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewULIDGeneratesUniqueMonotonicallyIncreasingIDs(t *testing.T) {
	id1 := NewULID()
	id2 := NewULID()

	assert.NotEmpty(t, id1.String(), "ULID should not be empty")
	assert.NotEqual(t, id1.String(), id2.String(), "Two ULIDs should be different")
	// ULIDs should be lexicographically sortable by time
	assert.LessOrEqual(t, id1.String(), id2.String(), "Later ULID should sort after earlier ULID")
}

func TestParseULIDRoundTripsValidString(t *testing.T) {
	original := NewULID()
	parsed, err := ParseULID(original.String())
	require.NoError(t, err)
	assert.Equal(t, original, parsed)
}

func TestParseULIDInvalidInputReturnsError(t *testing.T) {
	_, err := ParseULID("invalid")
	assert.Error(t, err, "ParseULID should fail on invalid input")
}

func TestNewULIDRemainsStrictlyMonotonicUnderRapidSuccessiveCalls(t *testing.T) {
	// core.NewULID must be monotonic across calls, including within
	// the same millisecond. Two downstream invariants depend on this:
	//   1. PostgresEventStore.Replay uses `WHERE id > afterID ORDER BY id`,
	//      which silently skips events whose IDs sort below a preceding event.
	//   2. PostgresSessionStore.UpdateCursors uses a monotonicity CAS that
	//      rejects cursor writes lex-smaller than the stored value.
	// A non-monotonic generator produces lex-inverted IDs within a millisecond
	// under load, breaking both invariants.
	const n = 10_000
	var prev ulid.ULID
	for i := 0; i < n; i++ {
		cur := NewULID()
		if i > 0 {
			// String comparison (not prev.Compare(cur)) is deliberate: the downstream
			// invariants we guard are PostgresEventStore.Replay's ORDER BY id (text-column
			// string comparison) and PostgresSessionStore.UpdateCursors' COLLATE "C"
			// byte-wise CAS. The test must exercise the same comparison shape the SQL
			// relies on, not binary ULID comparison.
			require.True(t, prev.String() < cur.String(),
				"non-monotonic ULIDs at index %d: prev=%s cur=%s",
				i, prev.String(), cur.String())
		}
		prev = cur
	}
}
