// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek

import (
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/require"
)

// TestExtractMissingMembers_StringSlice — happy path: the Coordinator's
// production code stamps Context()["missing_members"] as a []string-compat
// slice. The extractor returns the slice contents byte-for-byte.
func TestExtractMissingMembers_StringSlice(t *testing.T) {
	err := oops.Code("INVALIDATION_PARTIAL_FAILURE").
		With("missing_members", []string{"m1", "m2"}).
		Errorf("boom")
	got := extractMissingMembers(err)
	require.Equal(t, []string{"m1", "m2"}, got)
}

// TestExtractMissingMembers_NamedStringSlice — production type is
// []cluster.MemberID where MemberID = string. The reflection fallback
// MUST decode that to []string.
func TestExtractMissingMembers_NamedStringSlice(t *testing.T) {
	type memberID string
	err := oops.Code("INVALIDATION_PARTIAL_FAILURE").
		With("missing_members", []memberID{"node-a", "node-b"}).
		Errorf("boom")
	got := extractMissingMembers(err)
	require.Equal(t, []string{"node-a", "node-b"}, got)
}

// TestExtractMissingMembers_Missing — defensive: an error without the
// missing_members context (or with a nil value) returns nil rather than
// panicking. RunPhase5 treats nil as "unknown set" and persists "[]".
func TestExtractMissingMembers_Missing(t *testing.T) {
	plainErr := oops.Code("SOMETHING_ELSE").Errorf("no context")
	require.Nil(t, extractMissingMembers(plainErr))

	nilCtxErr := oops.Code("X").With("missing_members", nil).Errorf("nil value")
	require.Nil(t, extractMissingMembers(nilCtxErr))
}

// TestExtractMissingMembers_NonOopsError — non-oops errors yield nil
// without panicking.
func TestExtractMissingMembers_NonOopsError(t *testing.T) {
	require.Nil(t, extractMissingMembers(nil))
	// stdlib error: AsOops returns (_, false) → nil.
	require.Nil(t, extractMissingMembers(plainError("nope")))
}

type plainError string

func (e plainError) Error() string { return string(e) }

// TestExtractMissingMembers_JSONBytesPath — defensive []byte arm: when the
// Coordinator stamps missing_members as a []byte holding a JSON array,
// the extractor must json.Unmarshal it. Malformed JSON falls back to nil
// (caller treats as "unknown set"), matching the production tolerance for
// Coordinator quirks documented in rekey_phase5.go's algorithm comment.
func TestExtractMissingMembers_JSONBytesPath(t *testing.T) {
	good := oops.Code("X").
		With("missing_members", []byte(`["a","b"]`)).
		Errorf("boom")
	require.Equal(t, []string{"a", "b"}, extractMissingMembers(good))

	bad := oops.Code("X").
		With("missing_members", []byte("not-json")).
		Errorf("boom")
	require.Nil(t, extractMissingMembers(bad), "malformed JSON → nil, not panic")
}

// TestStringifyMember_AllArms — exercises the three switch arms of
// stringifyMember (string passthrough, []byte→string, default empty). The
// integer case also exercises the []any path of extractMissingMembers
// (default arm of stringifyMember returns "" for non-string-shaped values).
func TestStringifyMember_AllArms(t *testing.T) {
	require.Equal(t, "x", stringifyMember("x"))
	require.Equal(t, "y", stringifyMember([]byte("y")))
	require.Equal(t, "", stringifyMember(42))

	// Also exercise extractMissingMembers via the []any path, which routes
	// each element through stringifyMember.
	mixed := oops.Code("X").
		With("missing_members", []any{"a", []byte("b"), 7}).
		Errorf("boom")
	require.Equal(t, []string{"a", "b", ""}, extractMissingMembers(mixed))
}

// TestCheckpoint_Phase5HasMissingMembers_Null — the (NULL,
// "null", "[]") cases all report false. INV-CRYPTO-97's force-destroy gate
// depends on this tolerance: a row with an empty array MUST NOT be
// treated as "stuck in timeout".
func TestCheckpoint_Phase5HasMissingMembers_Null(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
		want bool
	}{
		{"nil slice", nil, false},
		{"empty slice", []byte{}, false},
		{"json null", []byte("null"), false},
		{"json null with whitespace", []byte("  null  "), false},
		{"empty array", []byte("[]"), false},
		{"populated array", []byte(`["m1"]`), true},
		{"multi-element array", []byte(`["m1","m2","m3"]`), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Checkpoint{phase5MissingMembers: tc.raw}
			require.Equal(t, tc.want, c.Phase5HasMissingMembers())
		})
	}
}

// TestCheckpoint_Phase5MissingMembers_Decodes — accessor decodes the
// JSON blob to []string and surfaces parse errors via a typed oops code.
func TestCheckpoint_Phase5MissingMembers_Decodes(t *testing.T) {
	c := Checkpoint{phase5MissingMembers: []byte(`["alpha","beta"]`)}
	got, err := c.Phase5MissingMembers()
	require.NoError(t, err)
	require.Equal(t, []string{"alpha", "beta"}, got)

	cNull := Checkpoint{phase5MissingMembers: nil}
	got2, err := cNull.Phase5MissingMembers()
	require.NoError(t, err)
	require.Nil(t, got2)

	cBad := Checkpoint{phase5MissingMembers: []byte(`not json`)}
	_, err = cBad.Phase5MissingMembers()
	require.Error(t, err)
	oerr, ok := oops.AsOops(err)
	require.True(t, ok)
	require.Equal(t, "DEK_REKEY_PHASE5_MISSING_MEMBERS_DECODE_FAILED", oerr.Code())
}
