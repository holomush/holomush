// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"errors"
	"fmt"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/pkg/errutil"
)

func TestWrapAccessError(t *testing.T) {
	t.Run("wraps ErrAccessEvaluationFailed with eval failed code", func(t *testing.T) {
		err := fmt.Errorf("engine down: %w", ErrAccessEvaluationFailed)
		got := wrapAccessError(err, "LOCATION_ACCESS_EVALUATION_FAILED", "LOCATION_ACCESS_DENIED")

		errutil.AssertErrorCode(t, got, "LOCATION_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, got, ErrAccessEvaluationFailed)
	})

	t.Run("wraps permission denied with denied code", func(t *testing.T) {
		err := fmt.Errorf("not allowed: %w", ErrPermissionDenied)
		got := wrapAccessError(err, "LOCATION_ACCESS_EVALUATION_FAILED", "LOCATION_ACCESS_DENIED")

		errutil.AssertErrorCode(t, got, "LOCATION_ACCESS_DENIED")
		assert.ErrorIs(t, got, ErrPermissionDenied)
	})

	t.Run("wraps other errors with denied code", func(t *testing.T) {
		err := errors.New("unexpected error")
		got := wrapAccessError(err, "LOCATION_ACCESS_EVALUATION_FAILED", "LOCATION_ACCESS_DENIED")

		errutil.AssertErrorCode(t, got, "LOCATION_ACCESS_DENIED")
	})

	t.Run("preserves oops error chain", func(t *testing.T) {
		inner := oops.Errorf("inner problem")
		err := fmt.Errorf("wrapped: %w: %w", ErrAccessEvaluationFailed, inner)
		got := wrapAccessError(err, "EXIT_ACCESS_EVALUATION_FAILED", "EXIT_ACCESS_DENIED")

		errutil.AssertErrorCode(t, got, "EXIT_ACCESS_EVALUATION_FAILED")
		assert.ErrorIs(t, got, ErrAccessEvaluationFailed)
	})
}
