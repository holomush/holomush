// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus"
)

func TestSentinelErrorsWrapAndUnwrap(t *testing.T) {
	wrapped := fmt.Errorf("publish failed: %w", eventbus.ErrPublishExpired)
	require.True(t, errors.Is(wrapped, eventbus.ErrPublishExpired))
	require.False(t, errors.Is(wrapped, eventbus.ErrInvalidSubject))
}

func TestMaxPayloadSizeMatchesLegacy(t *testing.T) {
	require.Equal(t, 64*1024, eventbus.MaxPayloadSize)
}
