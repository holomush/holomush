// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRekeyProgressErrorAndExitCodeError covers the Is and Unwrap methods of
// rekeyProgressError and exitCodeError, which mapToExitCodeErr and friends
// rely on for sysexits.h code assignment (INV-CRYPTO-110).
func TestRekeyProgressErrorAndExitCodeError(t *testing.T) {
	t.Parallel()

	t.Run("rekeyProgressError Is matches on code", func(t *testing.T) {
		t.Parallel()
		err := &rekeyProgressError{code: "X", msg: "msg-a"}
		target := &rekeyProgressError{code: "X", msg: "msg-b"}
		require.True(t, errors.Is(err, target), "Is should match on code regardless of msg")
	})

	t.Run("rekeyProgressError Is mismatched codes", func(t *testing.T) {
		t.Parallel()
		err := &rekeyProgressError{code: "X"}
		target := &rekeyProgressError{code: "Y"}
		require.False(t, errors.Is(err, target), "Is should not match different codes")
	})

	t.Run("rekeyProgressError Is non-matching type", func(t *testing.T) {
		t.Parallel()
		err := &rekeyProgressError{code: "X"}
		require.False(t, errors.Is(err, errors.New("x")), "Is should not match other error types")
	})

	t.Run("exitCodeError Unwrap returns cause", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("underlying")
		err := &exitCodeError{exitCode: 64, cause: cause}
		require.Same(t, cause, err.Unwrap())
		require.True(t, errors.Is(err, cause))
	})

	t.Run("exitCodeError Unwrap with nil cause", func(t *testing.T) {
		t.Parallel()
		err := &exitCodeError{exitCode: 64, cause: nil}
		require.Nil(t, err.Unwrap())
	})
}
