// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/eventbus"
)

func TestCryptoEnabledDefaultsToTrue(t *testing.T) {
	cfg := eventbus.Config{}.Defaults()
	assert.True(t, cfg.Crypto.Enabled, "Phase 3d ships live")
}
