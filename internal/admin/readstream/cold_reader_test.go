// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package readstream

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/pkg/errutil"
)

// ----------------------------------------------------------------------------
// subjectToLikePattern tests
// ----------------------------------------------------------------------------

func TestSubjectToLikePattern_WildcardSuffix(t *testing.T) {
	tests := []struct {
		name     string
		input    eventbus.Subject
		expected string
	}{
		{
			name:     "trailing .> becomes .%",
			input:    "events.x.>",
			expected: "events.x.%",
		},
		{
			name:     "multi-level wildcard",
			input:    "events.main.scene.01ABCDEF.>",
			expected: "events.main.scene.01ABCDEF.%",
		},
		{
			name:     "top-level game wildcard",
			input:    "events.game1.>",
			expected: "events.game1.%",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := subjectToLikePattern(tc.input)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestSubjectToLikePattern_ExactSubject(t *testing.T) {
	tests := []struct {
		name  string
		input eventbus.Subject
	}{
		{
			name:  "exact subject unchanged",
			input: "events.x.y",
		},
		{
			name:  "no wildcard preserved",
			input: "events.main.scene.01ABCDEF",
		},
		{
			name:  "single token exact",
			input: "events.game.type.id",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := subjectToLikePattern(tc.input)
			assert.Equal(t, string(tc.input), got, "exact subjects must not be modified")
		})
	}
}

// ----------------------------------------------------------------------------
// buildSQL tests
// ----------------------------------------------------------------------------

// newColdReaderForTest constructs a ColdReader with a nil pool.
// buildSQL does not use the pool, so this is safe for pure SQL-builder tests.
func newColdReaderForTest() *ColdReader {
	return &ColdReader{pool: nil}
}

func TestColdReader_BuildSQL_SingleSubject(t *testing.T) {
	cr := newColdReaderForTest()
	q := ColdQuery{
		Subjects: []eventbus.Subject{"events.game1.>"},
		Since:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Until:    time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC),
	}
	sqlStr, args, err := cr.buildSQL(q)
	require.NoError(t, err)

	// One pattern arg + Since + Until = 3 args
	assert.Len(t, args, 3)
	assert.Equal(t, "events.game1.%", args[0])

	// Single LIKE clause
	assert.Contains(t, sqlStr, "subject LIKE $1")
	assert.NotContains(t, sqlStr, "subject LIKE $2")

	// Time bounds
	assert.Contains(t, sqlStr, "timestamp >= $2")
	assert.Contains(t, sqlStr, "timestamp <= $3")
}

func TestColdReader_BuildSQL_MultiSubject(t *testing.T) {
	cr := newColdReaderForTest()
	q := ColdQuery{
		Subjects: []eventbus.Subject{
			"events.game1.scene.AA.>",
			"events.game1.scene.BB.>",
			"events.game1.scene.CC.>",
		},
	}
	sqlStr, args, err := cr.buildSQL(q)
	require.NoError(t, err)

	// Three pattern args, no time bounds
	assert.Len(t, args, 3)
	assert.Equal(t, "events.game1.scene.AA.%", args[0])
	assert.Equal(t, "events.game1.scene.BB.%", args[1])
	assert.Equal(t, "events.game1.scene.CC.%", args[2])

	// All three OR'd together
	assert.Contains(t, sqlStr, "subject LIKE $1")
	assert.Contains(t, sqlStr, "subject LIKE $2")
	assert.Contains(t, sqlStr, "subject LIKE $3")

	// Verify OR connectors
	orCount := strings.Count(sqlStr, " OR ")
	assert.Equal(t, 2, orCount, "three subjects need two OR connectors")
}

func TestColdReader_BuildSQL_OrderByTimestampAsc(t *testing.T) {
	cr := newColdReaderForTest()
	q := ColdQuery{
		Subjects: []eventbus.Subject{"events.game.>"},
	}
	sqlStr, _, err := cr.buildSQL(q)
	require.NoError(t, err)

	// Must end with ascending order
	assert.Contains(t, sqlStr, "ORDER BY timestamp ASC, js_seq ASC")
}

func TestColdReader_BuildSQL_FiltersByDekRefNotNull(t *testing.T) {
	cr := newColdReaderForTest()
	q := ColdQuery{
		Subjects: []eventbus.Subject{"events.game.>"},
	}
	sqlStr, _, err := cr.buildSQL(q)
	require.NoError(t, err)

	// Only encrypted rows returned
	assert.Contains(t, sqlStr, "dek_ref IS NOT NULL")
}

func TestColdReader_BuildSQL_NoSubjectsReturnsError(t *testing.T) {
	cr := newColdReaderForTest()
	q := ColdQuery{
		Subjects: nil,
	}
	_, _, err := cr.buildSQL(q)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "ADMIN_READSTREAM_COLD_NO_SUBJECTS")
}

func TestColdReader_BuildSQL_ZeroTimeBoundsOmitted(t *testing.T) {
	cr := newColdReaderForTest()
	q := ColdQuery{
		Subjects: []eventbus.Subject{"events.game.>"},
		// Since and Until are zero values
	}
	sqlStr, args, err := cr.buildSQL(q)
	require.NoError(t, err)

	// Only 1 arg (the subject pattern)
	assert.Len(t, args, 1)
	assert.NotContains(t, sqlStr, "timestamp >=")
	assert.NotContains(t, sqlStr, "timestamp <=")
}

func TestColdReader_BuildSQL_SelectsCorrectColumns(t *testing.T) {
	cr := newColdReaderForTest()
	q := ColdQuery{
		Subjects: []eventbus.Subject{"events.game.>"},
	}
	sqlStr, _, err := cr.buildSQL(q)
	require.NoError(t, err)

	// All required columns must be present in the SELECT list
	for _, col := range []string{"id", "subject", "type", "timestamp", "actor_kind", "actor_id",
		"envelope", "codec", "dek_ref", "dek_version", "js_seq"} {
		assert.Contains(t, sqlStr, col, "SELECT must include column: %s", col)
	}
}
