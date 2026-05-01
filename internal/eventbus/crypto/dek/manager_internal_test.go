// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Internal-package tests for unexported helper functions in the dek
// package. The companion external test file (manager_test.go in
// dek_test package) covers the public surface.

package dek

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

// TestValidateProviderWrapOutput_RejectsEmptyWrapped covers the
// branch where kek.Provider.Wrap returns a zero-length wrapped slice.
func TestValidateProviderWrapOutput_RejectsEmptyWrapped(t *testing.T) {
	err := validateProviderWrapOutput([]byte{}, "kek-id-1")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_WRAP_OUTPUT_INVALID")
	errutil.AssertErrorContext(t, err, "reason", "wrapped_empty")
}

// TestValidateProviderWrapOutput_RejectsEmptyKEKKeyID covers the
// branch where kek.Provider.Wrap returns an empty kekKeyID string.
func TestValidateProviderWrapOutput_RejectsEmptyKEKKeyID(t *testing.T) {
	err := validateProviderWrapOutput([]byte{0x01, 0x02}, "")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_WRAP_OUTPUT_INVALID")
	errutil.AssertErrorContext(t, err, "reason", "kek_key_id_empty")
}

// TestValidateProviderWrapOutput_AcceptsValid covers the happy path so
// the cumulative diff is exercised end-to-end.
func TestValidateProviderWrapOutput_AcceptsValid(t *testing.T) {
	require.NoError(t, validateProviderWrapOutput([]byte{0x01, 0x02}, "kek-id-1"))
}

// TestValidateProviderUnwrapOutput_RejectsWrongLength covers the
// branch where kek.Provider.Unwrap returns a buffer of the wrong size
// (anything other than DEKByteLength bytes).
func TestValidateProviderUnwrapOutput_RejectsWrongLength(t *testing.T) {
	err := validateProviderUnwrapOutput(make([]byte, 16), 42, 1)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_UNWRAP_OUTPUT_INVALID")
	errutil.AssertErrorContext(t, err, "expected_bytes", DEKByteLength)
	errutil.AssertErrorContext(t, err, "got_bytes", 16)
}

// TestValidateProviderUnwrapOutput_AcceptsCorrectLength covers the
// happy path.
func TestValidateProviderUnwrapOutput_AcceptsCorrectLength(t *testing.T) {
	require.NoError(t, validateProviderUnwrapOutput(make([]byte, DEKByteLength), 42, 1))
}

// TestValidateProviderUnwrapOutput_RejectsEmpty exercises the zero-
// length case explicitly (Empty != WrongLength as a distinguishable
// failure mode for ops).
func TestValidateProviderUnwrapOutput_RejectsEmpty(t *testing.T) {
	err := validateProviderUnwrapOutput(nil, 42, 1)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_UNWRAP_OUTPUT_INVALID")
	errutil.AssertErrorContext(t, err, "got_bytes", 0)
}
