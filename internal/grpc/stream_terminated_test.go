// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"errors"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
)

func TestErrStreamTerminatedIsDetectableViaErrorsIs(t *testing.T) {
	assert.True(t, errors.Is(errStreamTerminated, errStreamTerminated))
}

func TestErrStreamTerminatedSurvivesOopsWrap(t *testing.T) {
	wrapped := oops.Code("SEND_FAILED").Wrap(errStreamTerminated)
	assert.True(t, errors.Is(wrapped, errStreamTerminated),
		"oops must preserve the sentinel through wrap")
}
