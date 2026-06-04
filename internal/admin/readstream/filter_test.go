// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package readstream_test

import (
	"strings"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/admin/readstream"
	"github.com/holomush/holomush/pkg/errutil"
)

// helper builds a minimal valid request (non-zero times, valid context, good justification).
func validRequest() *readstream.Request {
	now := time.Now()
	return &readstream.Request{
		Contexts:      []readstream.ContextRef{{Type: "scene", IDs: []string{"01ARZ3NDEKTSV4RRFFQ69G5FAV"}}},
		Since:         now.Add(-1 * time.Hour),
		Until:         now,
		Justification: "audit investigation for incident-2026",
	}
}

// TestResolveBounds_NilRequestReturnsError verifies that a nil *Request returns
// a typed validation error rather than a nil-pointer panic.
func TestResolveBounds_NilRequestReturnsError(t *testing.T) {
	_, _, err := readstream.ResolveBounds(nil, time.Now(), time.Hour, 24*time.Hour)
	require.Error(t, err, "nil request must be rejected")
	errutil.AssertErrorCode(t, err, "DENY_OPERATOR_READ_INVALID_REQUEST")
}

func TestResolveBounds_DefaultsFromZeroBounds(t *testing.T) {
	now := time.Now()
	req := &readstream.Request{
		Contexts:      []readstream.ContextRef{{Type: "scene", IDs: []string{"01ARZ3NDEKTSV4RRFFQ69G5FAV"}}},
		Justification: "some reason",
		// Since and Until deliberately zero
	}
	defaultWindow := 24 * time.Hour
	maxWindow := 7 * 24 * time.Hour

	resolved, flags, err := readstream.ResolveBounds(req, now, defaultWindow, maxWindow)
	require.NoError(t, err)
	assert.True(t, flags.SinceDefaulted)
	assert.True(t, flags.UntilDefaulted)
	assert.WithinDuration(t, now.Add(-defaultWindow), resolved.Since, time.Second)
	assert.WithinDuration(t, now, resolved.Until, time.Second)
}

func TestINV_CRYPTO_56_WindowTooLargeRejected(t *testing.T) {
	now := time.Now()
	req := &readstream.Request{
		Contexts:      []readstream.ContextRef{{Type: "scene", IDs: []string{"01ARZ3NDEKTSV4RRFFQ69G5FAV"}}},
		Since:         now.Add(-8 * 24 * time.Hour), // 8 days
		Until:         now,
		Justification: "some reason",
	}
	_, _, err := readstream.ResolveBounds(req, now, 24*time.Hour, 7*24*time.Hour)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DENY_OPERATOR_READ_WINDOW_TOO_LARGE")
}

func TestResolveBounds_TimeInvertedRejected(t *testing.T) {
	now := time.Now()
	req := &readstream.Request{
		Contexts:      []readstream.ContextRef{{Type: "scene", IDs: []string{"01ARZ3NDEKTSV4RRFFQ69G5FAV"}}},
		Since:         now,
		Until:         now.Add(-1 * time.Hour), // until before since
		Justification: "some reason",
	}
	_, _, err := readstream.ResolveBounds(req, now, 24*time.Hour, 7*24*time.Hour)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DENY_OPERATOR_READ_TIME_INVERTED")
}

func TestResolveBounds_FutureBoundRejected(t *testing.T) {
	now := time.Now()
	req := &readstream.Request{
		Contexts:      []readstream.ContextRef{{Type: "scene", IDs: []string{"01ARZ3NDEKTSV4RRFFQ69G5FAV"}}},
		Since:         now.Add(-1 * time.Hour),
		Until:         now.Add(10 * time.Second), // beyond 5s grace
		Justification: "some reason",
	}
	_, _, err := readstream.ResolveBounds(req, now, 24*time.Hour, 7*24*time.Hour)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DENY_OPERATOR_READ_FUTURE_BOUND")
}

func TestResolveBounds_JustificationEmpty(t *testing.T) {
	now := time.Now()
	req := validRequest()
	req.Justification = "   " // whitespace only
	_, _, err := readstream.ResolveBounds(req, now, 24*time.Hour, 7*24*time.Hour)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DENY_OPERATOR_READ_JUSTIFICATION_EMPTY")
}

func TestResolveBounds_JustificationTooLong(t *testing.T) {
	now := time.Now()
	req := validRequest()
	req.Justification = strings.Repeat("x", 4097)
	_, _, err := readstream.ResolveBounds(req, now, 24*time.Hour, 7*24*time.Hour)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DENY_OPERATOR_READ_JUSTIFICATION_TOO_LONG")
}

func TestResolveBounds_ContextTypeUnknown(t *testing.T) {
	now := time.Now()
	req := validRequest()
	req.Contexts = []readstream.ContextRef{{Type: "unknown_type", IDs: []string{"01ARZ3NDEKTSV4RRFFQ69G5FAV"}}}
	_, _, err := readstream.ResolveBounds(req, now, 24*time.Hour, 7*24*time.Hour)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DENY_OPERATOR_READ_TYPE_UNKNOWN")
}

func TestResolveBounds_ContextArityMismatch(t *testing.T) {
	now := time.Now()
	req := validRequest()
	// "scene" requires arity 1, but 2 IDs supplied
	req.Contexts = []readstream.ContextRef{{Type: "scene", IDs: []string{"01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAX"}}}
	_, _, err := readstream.ResolveBounds(req, now, 24*time.Hour, 7*24*time.Hour)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DENY_OPERATOR_READ_ARITY_MISMATCH")
}

func TestResolveBounds_ContextIDMalformed(t *testing.T) {
	now := time.Now()
	req := validRequest()
	req.Contexts = []readstream.ContextRef{{Type: "scene", IDs: []string{"not-a-ulid"}}}
	_, _, err := readstream.ResolveBounds(req, now, 24*time.Hour, 7*24*time.Hour)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DENY_OPERATOR_READ_ID_MALFORMED")
}

func TestResolveBounds_DMLexCanonicalized(t *testing.T) {
	now := time.Now()
	// "dm" has arity 2 and orderInsensitiveIDs=true.
	// Supply IDs in reverse lex order — resolved should be sorted.
	id1 := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	id2 := "01ARZ3NDEKTSV4RRFFQ69G5FAX"
	req := &readstream.Request{
		Contexts:      []readstream.ContextRef{{Type: "dm", IDs: []string{id2, id1}}},
		Since:         now.Add(-1 * time.Hour),
		Until:         now,
		Justification: "audit investigation",
	}
	resolved, _, err := readstream.ResolveBounds(req, now, 24*time.Hour, 7*24*time.Hour)
	require.NoError(t, err)
	require.Len(t, resolved.Contexts, 1)
	assert.Equal(t, []string{id1, id2}, resolved.Contexts[0].IDs, "dm IDs should be lex-sorted")
}

func TestResolveBounds_TooManyContexts(t *testing.T) {
	now := time.Now()
	req := validRequest()
	// Build 65 scene contexts (>64 limit)
	contexts := make([]readstream.ContextRef, 65)
	for i := range contexts {
		contexts[i] = readstream.ContextRef{Type: "scene", IDs: []string{"01ARZ3NDEKTSV4RRFFQ69G5FAV"}}
	}
	req.Contexts = contexts
	_, _, err := readstream.ResolveBounds(req, now, 24*time.Hour, 7*24*time.Hour)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DENY_OPERATOR_READ_TOO_MANY_CONTEXTS")
}

// TestResolveBounds_InputNotMutated verifies MUST NOT mutate input.
func TestResolveBounds_InputNotMutated(t *testing.T) {
	now := time.Now()
	id1 := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	id2 := "01ARZ3NDEKTSV4RRFFQ69G5FAX"
	req := &readstream.Request{
		Contexts:      []readstream.ContextRef{{Type: "dm", IDs: []string{id2, id1}}},
		Since:         now.Add(-1 * time.Hour),
		Until:         now,
		Justification: "test",
	}
	original := []string{id2, id1}
	_, _, err := readstream.ResolveBounds(req, now, 24*time.Hour, 7*24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, original, req.Contexts[0].IDs, "input request MUST NOT be mutated")
}

// TestResolveBounds_DedupeByTypeAndIDs verifies duplicate contexts are collapsed.
func TestResolveBounds_DedupeByTypeAndIDs(t *testing.T) {
	now := time.Now()
	id := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	req := &readstream.Request{
		Contexts: []readstream.ContextRef{
			{Type: "scene", IDs: []string{id}},
			{Type: "scene", IDs: []string{id}}, // duplicate
		},
		Since:         now.Add(-1 * time.Hour),
		Until:         now,
		Justification: "audit",
	}
	resolved, _, err := readstream.ResolveBounds(req, now, 24*time.Hour, 7*24*time.Hour)
	require.NoError(t, err)
	assert.Len(t, resolved.Contexts, 1, "duplicate contexts should be deduped")
}

// TestResolveBounds_JustificationPreserved verifies justification passes through.
func TestResolveBounds_JustificationPreserved(t *testing.T) {
	now := time.Now()
	req := validRequest()
	req.Justification = "  trimmed justification  "
	resolved, _, err := readstream.ResolveBounds(req, now, 24*time.Hour, 7*24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, "  trimmed justification  ", resolved.Justification)
}

// Ensure we can extract oops codes via oops.AsOops directly (belt-and-suspenders).
func TestResolveBounds_OopsCodeAccessible(t *testing.T) {
	now := time.Now()
	req := validRequest()
	req.Contexts = []readstream.ContextRef{{Type: "bogus", IDs: []string{"01ARZ3NDEKTSV4RRFFQ69G5FAV"}}}
	_, _, err := readstream.ResolveBounds(req, now, 24*time.Hour, 7*24*time.Hour)
	require.Error(t, err)
	oe, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "DENY_OPERATOR_READ_TYPE_UNKNOWN", oe.Code())
}
