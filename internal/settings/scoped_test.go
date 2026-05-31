// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package settings_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/holomush/holomush/internal/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScopedView_Owner(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("owner partition write is isolated from other owners", func(t *testing.T) {
		t.Parallel()
		sc := settings.NewScopedForTest(map[string]json.RawMessage{})

		require.NoError(t, sc.Plugin("core-scenes").SetString(ctx, "anything.goes", "yes"))

		// A different owner sees nothing.
		got, ok := sc.Plugin("core-channels").StringN(ctx, "anything.goes")
		assert.False(t, ok)
		assert.Empty(t, got)

		// The host partition sees nothing.
		hgot, hok := sc.StringN(ctx, "anything.goes")
		assert.False(t, hok)
		assert.Empty(t, hgot)

		// The owning partition round-trips.
		own, ownOK := sc.Plugin("core-scenes").StringN(ctx, "anything.goes")
		assert.True(t, ownOK)
		assert.Equal(t, "yes", own)
	})

	t.Run("plugin-partition key works with no registered namespace", func(t *testing.T) {
		t.Parallel()
		sc := settings.NewScopedForTest(map[string]json.RawMessage{})

		// "content" is NOT a registered namespace; owner partitions skip
		// namespace validation, so this must succeed and round-trip.
		require.NoError(t, sc.Plugin("content").SetString(ctx, "content.cw_block", "gore"))

		got, ok := sc.Plugin("content").StringN(ctx, "content.cw_block")
		assert.True(t, ok)
		assert.Equal(t, "gore", got)
	})

	t.Run("same owner round-trips a string-slice", func(t *testing.T) {
		t.Parallel()
		sc := settings.NewScopedForTest(map[string]json.RawMessage{})

		want := []string{"alpha", "beta", "gamma"}
		require.NoError(t, sc.Plugin("content").SetStringSlice(ctx, "content.cw_list", want))

		got, ok := sc.Plugin("content").StringSliceN(ctx, "content.cw_list")
		assert.True(t, ok)
		assert.Equal(t, want, got)
	})
}

func TestScopedView_BareReadsTargetHostPartition(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sc := settings.NewScopedForTest(map[string]json.RawMessage{
		"scenes.focus.replay_tail_default": json.RawMessage("5"),
		"scenes.focus.label":               json.RawMessage(`"focus"`),
	})

	n, ok := sc.IntN(ctx, "scenes.focus.replay_tail_default")
	assert.True(t, ok)
	assert.Equal(t, 5, n)

	s, sok := sc.StringN(ctx, "scenes.focus.label")
	assert.True(t, sok)
	assert.Equal(t, "focus", s)

	// Bare reads validate namespace: an unregistered namespace reads unset.
	_, bad := sc.StringN(ctx, "badns.key")
	assert.False(t, bad)
}

func TestScopedView_HostWriteValidatesNamespace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sc := settings.NewScopedForTest(map[string]json.RawMessage{})

	// Host writes are namespace-validated: unregistered namespace errors.
	err := sc.Host().SetString(ctx, "badns.k", "v")
	require.Error(t, err)

	// A registered namespace succeeds and is visible via bare reads.
	require.NoError(t, sc.Host().SetStringSlice(ctx, "scenes.x", []string{"one", "two"}))
	got, ok := sc.StringSliceN(ctx, "scenes.x")
	assert.True(t, ok)
	assert.Equal(t, []string{"one", "two"}, got)

	// Host SetString with a registered namespace round-trips.
	require.NoError(t, sc.Host().SetString(ctx, "scenes.y", "val"))
	sgot, sok := sc.StringN(ctx, "scenes.y")
	assert.True(t, sok)
	assert.Equal(t, "val", sgot)
}
