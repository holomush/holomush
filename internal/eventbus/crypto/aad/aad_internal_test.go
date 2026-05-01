// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Internal-package tests for unexported helpers. Build's overflow
// guard exits via this helper, so testing checkFieldLen directly
// covers the AAD_FIELD_TOO_LARGE branch without requiring a
// multi-gigabyte test fixture.

package aad

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

func TestCheckFieldLen_AcceptsZero(t *testing.T) {
	require.NoError(t, checkFieldLen("test", 0))
}

func TestCheckFieldLen_AcceptsTypicalSize(t *testing.T) {
	require.NoError(t, checkFieldLen("event.id", 16))
}

func TestCheckFieldLen_AcceptsMaxBound(t *testing.T) {
	require.NoError(t, checkFieldLen("test", maxFieldLen))
}

func TestCheckFieldLen_RejectsNegative(t *testing.T) {
	err := checkFieldLen("test", -1)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AAD_FIELD_TOO_LARGE")
	errutil.AssertErrorContext(t, err, "field", "test")
}

func TestCheckFieldLen_RejectsAboveBound(t *testing.T) {
	// Using a value just above maxFieldLen (uint32-max + 1) as int.
	// This exercises the overflow guard without allocating multi-GB
	// fixtures: int on a 64-bit system can hold MaxUint32+1.
	overflow := int(maxFieldLen) + 1
	err := checkFieldLen("event.subject", overflow)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "AAD_FIELD_TOO_LARGE")
	errutil.AssertErrorContext(t, err, "field", "event.subject")
	errutil.AssertErrorContext(t, err, "length", overflow)
}
