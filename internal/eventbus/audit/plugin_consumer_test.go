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
	// Start with zero consumers MUST succeed and leave the manager in a
	// state where Stop is still a no-op. Regression guard: the old code
	// only exercised Stop and silently regressed when Start started
	// dereferencing its JetStream context unconditionally.
	err := m.Start(t.Context())
	assert.NoError(t, err)
	assert.Equal(t, 0, m.Consumers())
	err = m.Stop(t.Context())
	assert.NoError(t, err)
}
