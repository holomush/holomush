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

func TestScanInt64ReturnsTimeAtNanosecondPrecision(t *testing.T) {
	var got pgnanos.Time
	require.NoError(t, got.Scan(int64(1700000000123456789)))
	assert.Equal(t, time.Unix(1700000000, 123456789).UTC(), got.Time())
}

func TestScanNilReturnsZeroTime(t *testing.T) {
	var got pgnanos.Time
	require.NoError(t, got.Scan(nil))
	assert.True(t, got.IsZero())
}

func TestScanWrongTypeReturnsErrorWithType(t *testing.T) {
	var got pgnanos.Time
	err := got.Scan("not-an-int64")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "string")
	assert.Contains(t, err.Error(), "pgnanos.Time")
}

func TestValueZeroTimeReturnsZero(t *testing.T) {
	var zero pgnanos.Time
	v, err := zero.Value()
	require.NoError(t, err)
	assert.Equal(t, int64(0), v)
}

func TestValueSpecificTimeReturnsExpectedNanoseconds(t *testing.T) {
	const wantNanos = int64(1700000000123456789)
	in := pgnanos.From(time.Unix(0, wantNanos))
	v, err := in.Value()
	require.NoError(t, err)
	assert.Equal(t, wantNanos, v)
}

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

func TestFromConvertsToUTC(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	in := time.Date(2026, 5, 22, 12, 0, 0, 0, loc)
	got := pgnanos.From(in).Time()
	assert.Equal(t, time.UTC, got.Location(), "From MUST normalize to UTC")
	assert.True(t, in.Equal(got), "From MUST preserve instant")
}

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

func TestNilPointerConvertsToNilDriverValueForNullableColumns(t *testing.T) {
	var p *pgnanos.Time
	v, err := driver.DefaultParameterConverter.ConvertValue(p)
	require.NoError(t, err)
	assert.Nil(t, v, "typed nil *pgnanos.Time MUST convert to NULL (nil driver.Value)")
}
