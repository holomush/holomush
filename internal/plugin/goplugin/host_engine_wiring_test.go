// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/plugin/goplugin"
)

// TestWithEngineStoresEngineInHost is a behavioral wiring-guard for
// holomush-8kkv5.18. It verifies that goplugin.WithEngine correctly propagates
// the given engine into the host's internal engine field.
//
// In production, setup/subsystem.go Start() passes:
//
//	goplugin.WithEngine(s.cfg.ABAC.Engine())
//
// to goplugin.NewHost. Before this fix, WithEngine was absent from hostOpts,
// so s.host.engine was nil and PluginHostService.Evaluate always returned
// EVALUATE_ENGINE_UNCONFIGURED for every binary plugin call.
//
// This test confirms:
//  1. WithEngine is a valid HostOption (compile-time guard).
//  2. The engine stored in the host is the identical instance that was passed in
//     (identity guard — ensures the engine+resolver pair from BuildABACStack is
//     shared between the Lua hostfunc bridge and the binary host).
func TestWithEngineStoresEngineInHost(t *testing.T) {
	eng := policytest.AllowAllEngine()
	require.NotNil(t, eng)

	host := goplugin.NewHost(goplugin.WithEngine(eng))
	require.NotNil(t, host)

	got := host.EngineForTest()
	assert.NotNil(t, got,
		"binary host MUST have a non-nil engine after WithEngine option is applied; "+
			"nil means PluginHostService.Evaluate returns EVALUATE_ENGINE_UNCONFIGURED "+
			"for all binary plugins (holomush-8kkv5.18 regression)")
	assert.Same(t, eng, got,
		"host must store the identical engine instance passed to WithEngine; "+
			"a copy would break attribute-resolver sharing between Lua and binary surfaces")
}
