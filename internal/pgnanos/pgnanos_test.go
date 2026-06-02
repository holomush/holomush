// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pgnanos_test

import (
	"database/sql/driver"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/pgnanos"
)

// TestScan exercises the sql.Scanner implementation across happy-path,
// nil, and wrong-type inputs in table-driven form per repo convention.
func TestScan(t *testing.T) {
	cases := []struct {
		name        string
		src         any
		wantTime    time.Time
		wantIsZero  bool
		wantErr     bool
		errContains []string
	}{
		{
			name:     "int64 decodes as time at nanosecond precision",
			src:      int64(1700000000123456789),
			wantTime: time.Unix(1700000000, 123456789).UTC(),
		},
		{
			name:       "nil decodes as zero pgnanos.Time",
			src:        nil,
			wantIsZero: true,
		},
		{
			name:        "wrong type returns error mentioning the type and pgnanos.Time",
			src:         "not-an-int64",
			wantErr:     true,
			errContains: []string{"string", "pgnanos.Time"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got pgnanos.Time
			err := got.Scan(tc.src)
			if tc.wantErr {
				require.Error(t, err)
				for _, sub := range tc.errContains {
					assert.Contains(t, err.Error(), sub)
				}
				return
			}
			require.NoError(t, err)
			if tc.wantIsZero {
				assert.True(t, got.IsZero())
				return
			}
			assert.Equal(t, tc.wantTime, got.Time())
		})
	}
}

// TestValue exercises the driver.Valuer implementation across the
// zero-time and a specific-nanosecond instant.
func TestValue(t *testing.T) {
	cases := []struct {
		name  string
		input pgnanos.Time
		want  int64
	}{
		{
			name:  "zero pgnanos.Time emits int64(0)",
			input: pgnanos.Time{},
			want:  0,
		},
		{
			name:  "specific time emits expected UnixNano",
			input: pgnanos.From(time.Unix(0, 1700000000123456789)),
			want:  1700000000123456789,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := tc.input.Value()
			require.NoError(t, err)
			assert.Equal(t, tc.want, v)
		})
	}
}

// TestRoundTripPreservesSubMicrosecondNanoseconds pins INV-STORE-2 (see
// internal/store/spec_meta_test.go cases slice). MUST remain a top-level
// Test* function — the meta-test walks the AST for top-level decls,
// not for t.Run subtests. Do not collapse into a table.
func TestRoundTripPreservesSubMicrosecondNanoseconds(t *testing.T) {
	orig := time.Date(2026, 5, 22, 12, 34, 56, 123456789, time.UTC)
	in := pgnanos.From(orig)
	v, err := in.Value()
	require.NoError(t, err)

	var out pgnanos.Time
	require.NoError(t, out.Scan(v))
	assert.Equal(t, orig, out.Time(), "round-trip MUST preserve sub-µs nanos")
	assert.Equal(t, 789, out.Time().Nanosecond()%1000,
		"sub-µs ns component MUST survive Value+Scan round-trip")
}

// TestFromConvertsToUTC documents that From() normalizes location.
// Kept as a standalone test (single case, distinct semantic concern from
// the Scan/Value tables).
func TestFromConvertsToUTC(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	in := time.Date(2026, 5, 22, 12, 0, 0, 0, loc)
	got := pgnanos.From(in).Time()
	assert.Equal(t, time.UTC, got.Location(), "From MUST normalize to UTC")
	assert.True(t, in.Equal(got), "From MUST preserve instant")
}

// TestZeroAndEpochAliasInValueButScanZeroDecodesAsEpoch documents a
// reviewer-discovered subtle alias: both Go-zero time and Unix epoch
// serialize to int64(0) via Value(), but Scan(0) decodes as epoch (not
// Go-zero). Callers MUST distinguish "unset" via column nullability
// (Scan(nil)), not via the in-band zero sentinel.
//
// Filed as bd issue gfo6.25 (reviewer-found during gfo6.1 review);
// pinned as a top-level Test* function because its name IS the
// documentation of the corner case.
func TestZeroAndEpochAliasInValueButScanZeroDecodesAsEpoch(t *testing.T) {
	// Both the Go zero time and time.Unix(0,0) serialize to int64(0) via Value().
	var zeroVal pgnanos.Time
	v1, err := zeroVal.Value()
	require.NoError(t, err)
	assert.Equal(t, int64(0), v1, "zero pgnanos.Time MUST produce int64(0) from Value()")

	epochVal := pgnanos.From(time.Unix(0, 0))
	v2, err := epochVal.Value()
	require.NoError(t, err)
	assert.Equal(t, int64(0), v2, "pgnanos.From(time.Unix(0,0)) MUST produce int64(0) from Value()")

	// Scan(0) decodes as epoch, NOT the Go zero time.
	var scanned pgnanos.Time
	require.NoError(t, scanned.Scan(int64(0)))
	assert.Equal(t, time.Unix(0, 0).UTC(), scanned.Time(), "Scan(0) MUST decode as Unix epoch, not Go zero time")

	// IsZero() is false for a value produced by Scan(0) — callers MUST use column
	// nullability (Scan(nil)) to distinguish "unset", not the in-band zero sentinel.
	assert.False(t, scanned.IsZero(), "Scan(0) result MUST NOT report IsZero() — use NULL for unset columns")
}

// TestNilPointerConvertsToNilDriverValueForNullableColumns pins spec §6
// row 7 ("nullable column path"). Filed as bd issue gfo6.26 by reviewer
// during gfo6.1; kept as top-level because the spec explicitly cites
// this behavior.
func TestNilPointerConvertsToNilDriverValueForNullableColumns(t *testing.T) {
	var p *pgnanos.Time
	v, err := driver.DefaultParameterConverter.ConvertValue(p)
	require.NoError(t, err)
	assert.Nil(t, v, "typed nil *pgnanos.Time MUST convert to NULL (nil driver.Value)")
}
