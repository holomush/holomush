// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginauthz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/pkg/errutil"
)

// Verifies: INV-PLUGIN-50
func TestEvaluateCapabilityAccessAllowsDeclaredPermitted(t *testing.T) {
	dec, err := pluginauthz.EvaluateCapabilityAccess(context.Background(), pluginauthz.CapabilityInput{
		Engine: policytest.AllowAllEngine(), PluginName: "core-objects",
		Subject: access.PluginSubject("core-objects"),
		Action:  "read", Resource: "kv:foo", Declared: true,
	})
	require.NoError(t, err)
	assert.True(t, dec.Allowed)
}

// Verifies: INV-PLUGIN-50
func TestEvaluateCapabilityAccessDeniedByPolicyDespiteDeclaration(t *testing.T) {
	dec, err := pluginauthz.EvaluateCapabilityAccess(context.Background(), pluginauthz.CapabilityInput{
		Engine: policytest.DenyAllEngine(), PluginName: "core-objects",
		Subject: access.PluginSubject("core-objects"),
		Action:  "read", Resource: "kv:foo", Declared: true,
	})
	require.NoError(t, err)
	assert.False(t, dec.Allowed) // INV-PLUGIN-50: declaration necessary, not sufficient
}

func TestEvaluateCapabilityAccessUndeclaredFailsClosed(t *testing.T) {
	_, err := pluginauthz.EvaluateCapabilityAccess(context.Background(), pluginauthz.CapabilityInput{
		Engine: policytest.AllowAllEngine(), PluginName: "core-objects",
		Subject: access.PluginSubject("core-objects"),
		Action:  "read", Resource: "kv:foo", Declared: false,
	})
	errutil.AssertErrorCode(t, err, "CAPABILITY_NOT_DECLARED")
}
