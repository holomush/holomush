// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package integrationtest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// INV-5IA-2: WithPluginCrypto without WithInTreePlugins must panic.
func TestWithPluginCryptoWithoutPluginsPanics(t *testing.T) {
	assert.PanicsWithValue(t,
		"integrationtest: WithPluginCrypto() requires WithInTreePlugins()",
		func() { Start(t, WithPluginCrypto()) })
}
