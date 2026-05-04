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
	assert.True(t, cfg.Crypto.IsEnabled(), "Phase 3d ships live (default-true when unset)")
}

// TestCryptoEnabledExplicitFalseSurvivesDefaults locks the security
// invariant flagged during Phase 3d code review: an operator who sets
// crypto.enabled=false in config MUST get a disabled crypto path.
// Defaults() MUST NOT clobber an explicit false.
func TestCryptoEnabledExplicitFalseSurvivesDefaults(t *testing.T) {
	falseV := false
	cfg := eventbus.Config{
		Crypto: eventbus.CryptoConfig{Enabled: &falseV},
	}.Defaults()
	assert.False(t, cfg.Crypto.IsEnabled(),
		"explicit operator-set false MUST survive Defaults()")
}

// TestCryptoEnabledExplicitTrueSurvivesDefaults symmetric — explicit
// true survives identically to explicit false.
func TestCryptoEnabledExplicitTrueSurvivesDefaults(t *testing.T) {
	trueV := true
	cfg := eventbus.Config{
		Crypto: eventbus.CryptoConfig{Enabled: &trueV},
	}.Defaults()
	assert.True(t, cfg.Crypto.IsEnabled(),
		"explicit operator-set true MUST survive Defaults()")
}
