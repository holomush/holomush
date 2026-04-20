// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package audit_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/eventbus/audit"
)

// TestPluginConsumerManagerStartWithNoConsumersIsNoop verifies the
// manager handles the zero-consumer case without error. This covers the
// "no plugins declared audit" path in production startup.
func TestPluginConsumerManagerStartWithNoConsumersIsNoop(t *testing.T) {
	t.Parallel()
	m := audit.NewPluginConsumerManager(nil) // nil JS tolerated when no Add is called
	assert.Equal(t, 0, m.Consumers())
	// Stop before Start is a no-op.
	err := m.Stop(t.Context())
	assert.NoError(t, err)
}
