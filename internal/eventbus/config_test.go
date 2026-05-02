// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package eventbus_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/eventbus"
)

func TestCryptoEnabledDefaultsToFalse(t *testing.T) {
	cfg := eventbus.Config{}.Defaults()
	assert.False(t, cfg.Crypto.Enabled, "Phase 3a ships dark — flag must default to off")
}
