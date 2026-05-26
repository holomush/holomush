// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package quarantinetest_test

import (
	"testing"

	"github.com/holomush/holomush/internal/testsupport/quarantinetest"
	"github.com/stretchr/testify/assert"
)

func TestEnabledReflectsEnvVar(t *testing.T) {
	t.Run("false when env unset", func(t *testing.T) {
		t.Setenv("HOLOMUSH_RUN_QUARANTINED", "")
		assert.False(t, quarantinetest.Enabled())
	})
	t.Run("true when env is 1", func(t *testing.T) {
		t.Setenv("HOLOMUSH_RUN_QUARANTINED", "1")
		assert.True(t, quarantinetest.Enabled())
	})
	t.Run("false for any other value", func(t *testing.T) {
		t.Setenv("HOLOMUSH_RUN_QUARANTINED", "true")
		assert.False(t, quarantinetest.Enabled())
	})
}

func TestSkipSkipsWhenDisabled(t *testing.T) {
	t.Setenv("HOLOMUSH_RUN_QUARANTINED", "")
	ranPast := false
	t.Run("subject", func(t *testing.T) {
		quarantinetest.Skip(t, "holomush-q55b")
		ranPast = true // unreachable when Skip fires
	})
	assert.False(t, ranPast, "code after Skip must not run when quarantine disabled")
}

func TestSkipRunsWhenEnabled(t *testing.T) {
	t.Setenv("HOLOMUSH_RUN_QUARANTINED", "1")
	ranPast := false
	t.Run("subject", func(t *testing.T) {
		quarantinetest.Skip(t, "holomush-q55b")
		ranPast = true
	})
	assert.True(t, ranPast, "code after Skip must run when quarantine enabled")
}
