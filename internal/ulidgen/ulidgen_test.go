// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package ulidgen

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGeneratesUniqueMonotonicallyIncreasingIDs(t *testing.T) {
	id1 := New()
	id2 := New()

	assert.NotEmpty(t, id1.String(), "ULID should not be empty")
	assert.NotEqual(t, id1.String(), id2.String(), "Two ULIDs should be different")
	// ULIDs should be lexicographically sortable by time
	assert.LessOrEqual(t, id1.String(), id2.String(), "Later ULID should sort after earlier ULID")
}

func TestParseRoundTripsValidString(t *testing.T) {
	original := New()
	parsed, err := Parse(original.String())
	require.NoError(t, err)
	assert.Equal(t, original, parsed)
}

func TestParseInvalidInputReturnsError(t *testing.T) {
	_, err := Parse("invalid")
	assert.Error(t, err, "Parse should fail on invalid input")
}

func TestNewULIDRemainsStrictlyMonotonicUnderRapidSuccessiveCalls(t *testing.T) {
	// ulidgen.New must be monotonic across calls, including within the same
	// millisecond. Two properties depend on this, kept separate (EventBus
	// ordering is exclusively JetStream's per-stream sequence, never ULID lex
	// order — .claude/rules/event-conventions.md):
	//   1. Event IDs need a stable, nonzero, unique Nats-Msg-Id dedup identity
	//      (internal/eventbus/publisher.go:165-170 rejects only the zero ULID;
	//      dedup does not require lex order).
	//   2. Monotonic-within-millisecond generation is retained as a generator
	//      property for session/cursor compatibility with any downstream
	//      consumer that relies on it.
	// A non-monotonic generator produces lex-inverted IDs within a millisecond
	// under load, which would silently break any such consumer.
	const n = 10_000
	var prev ulid.ULID
	for i := 0; i < n; i++ {
		cur := New()
		if i > 0 {
			// String comparison (not prev.Compare(cur)) is deliberate: a
			// downstream consumer relying on monotonicity would typically
			// compare via ORDER BY / CAS on the string column, not binary
			// ULID comparison. The test exercises that same comparison shape.
			require.True(t, prev.String() < cur.String(),
				"non-monotonic ULIDs at index %d: prev=%s cur=%s",
				i, prev.String(), cur.String())
		}
		prev = cur
	}
}
