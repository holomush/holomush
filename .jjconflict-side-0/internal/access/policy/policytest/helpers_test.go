// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policytest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/access/policy/types"
)

func TestAllowAllEngine(t *testing.T) {
	engine := policytest.AllowAllEngine()
	req, err := types.NewAccessRequest("character:01ABC", "read", "location:01XYZ")
	require.NoError(t, err)

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, decision.IsAllowed())
	assert.Equal(t, types.EffectAllow, decision.Effect())
}

func TestDenyAllEngine(t *testing.T) {
	engine := policytest.DenyAllEngine()
	req, err := types.NewAccessRequest("character:01ABC", "read", "location:01XYZ")
	require.NoError(t, err)

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, decision.IsAllowed())
	assert.Equal(t, types.EffectDeny, decision.Effect())
}

func TestGrantEngine(t *testing.T) {
	engine := policytest.NewGrantEngine()
	engine.Grant("character:01ABC", "read", "location:01XYZ")

	t.Run("granted permission returns allow", func(t *testing.T) {
		req, err := types.NewAccessRequest("character:01ABC", "read", "location:01XYZ")
		require.NoError(t, err)

		decision, err := engine.Evaluate(context.Background(), req)
		require.NoError(t, err)
		assert.True(t, decision.IsAllowed())
	})

	t.Run("ungranted permission returns DefaultDeny not Deny", func(t *testing.T) {
		req, err := types.NewAccessRequest("character:01ABC", "write", "location:01XYZ")
		require.NoError(t, err)

		decision, err := engine.Evaluate(context.Background(), req)
		require.NoError(t, err)
		assert.False(t, decision.IsAllowed())
		assert.Equal(t, types.EffectDefaultDeny, decision.Effect(),
			"ungranted permissions should produce EffectDefaultDeny, not EffectDeny")
	})
}

func TestErrorEngine(t *testing.T) {
	sentinel := errors.New("test engine error")
	engine := policytest.NewErrorEngine(sentinel)
	req, err := types.NewAccessRequest("character:01ABC", "read", "location:01XYZ")
	require.NoError(t, err)

	decision, err := engine.Evaluate(context.Background(), req)
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
	assert.Equal(t, types.Decision{}, decision, "error path should return zero-value Decision")
}

func TestInfraFailureEngine(t *testing.T) {
	engine := policytest.NewInfraFailureEngine(t, "session store error", "infra:session-store-error")
	req, err := types.NewAccessRequest("character:01ABC", "read", "location:01XYZ")
	require.NoError(t, err)

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, decision.IsAllowed())
	assert.True(t, decision.IsInfraFailure(),
		"InfraFailureEngine decisions must have IsInfraFailure()==true")
	assert.Equal(t, "infra:session-store-error", decision.PolicyID())
}
