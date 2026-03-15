// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/setup"
)

func TestNoopSessionResolver_ReturnsInvalid(t *testing.T) {
	r := setup.NewNoopSessionResolver()
	_, err := r.ResolveSession(context.Background(), "test-session")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented")
}
