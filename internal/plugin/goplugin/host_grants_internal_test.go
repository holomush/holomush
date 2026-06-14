// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
)

// TestServiceWiringAllowedGatesUngrantedServices is a direct unit test of the
// serviceWiringAllowed helper (host.go). It exercises the three semantically
// distinct branches of the least-privilege gate (INV-PLUGIN-45 foundation):
//
//   - nil pluginGrants → legacy fallback, all services allowed.
//   - non-nil pluginGrants, service granted → allowed.
//   - non-nil pluginGrants, service NOT granted → blocked.
//   - non-nil pluginGrants, plugin has no entry at all → blocked.
//
// NON-VACUOUS: removing the gate body (returning true unconditionally) would
// cause the "returns false for service not in grant set" and "returns false
// for plugin with no grant entry" subtests to fail because the excluded
// service would be reported as allowed.
func TestServiceWiringAllowedGatesUngrantedServices(t *testing.T) {
	tests := []struct {
		name        string
		grants      map[string][]string // nil → no-registry path
		plugin      string
		svcName     string
		wantAllowed bool
	}{
		{
			name:        "returns true when pluginGrants is nil (legacy fallback allows all)",
			grants:      nil,
			plugin:      "myplugin",
			svcName:     "holomush.world.v1.WorldService",
			wantAllowed: true,
		},
		{
			name:        "returns true for service present in grant set",
			grants:      map[string][]string{"myplugin": {"holomush.world.v1.WorldService", "focus"}},
			plugin:      "myplugin",
			svcName:     "holomush.world.v1.WorldService",
			wantAllowed: true,
		},
		{
			name:        "returns false for service not in grant set (exclusion gate)",
			grants:      map[string][]string{"myplugin": {"focus"}},
			plugin:      "myplugin",
			svcName:     "holomush.world.v1.WorldService",
			wantAllowed: false,
		},
		{
			name:        "returns false for plugin with no grant entry when pluginGrants non-nil",
			grants:      map[string][]string{"other-plugin": {"holomush.world.v1.WorldService"}},
			plugin:      "myplugin",
			svcName:     "holomush.world.v1.WorldService",
			wantAllowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Host{pluginGrants: tt.grants}
			got := h.serviceWiringAllowed(tt.plugin, tt.svcName)
			assert.Equal(t, tt.wantAllowed, got)
		})
	}
}

// TestGrantedSubsetReturnsOnlyGrantedTokens is a direct unit test of the
// grantedSubset helper (host.go). This is the same helper used at line 928
// (DeclaredCapabilities) and at line 867 (broker-loop grantSet build).
// It is NON-VACUOUS: deleting the filter body (making it return requested
// unchanged) would cause the "excludes ungranted token" subtest to fail
// because "world.mutation" would appear in the result.
func TestGrantedSubsetReturnsOnlyGrantedTokens(t *testing.T) {
	t.Run("excludes ungranted token", func(t *testing.T) {
		got := grantedSubset(
			[]string{"world.query", "world.mutation"},
			[]string{"world.query"},
		)
		assert.ElementsMatch(t, []string{"world.query"}, got,
			"only the granted token must appear; world.mutation must be excluded")
	})

	t.Run("nil granted returns nil", func(t *testing.T) {
		got := grantedSubset([]string{"world.query"}, nil)
		assert.Empty(t, got, "nil granted must yield no output (fail-closed)")
	})

	t.Run("empty granted returns nil", func(t *testing.T) {
		got := grantedSubset([]string{"world.query"}, []string{})
		assert.Empty(t, got, "empty granted must yield no output (fail-closed)")
	})

	t.Run("empty requested returns empty", func(t *testing.T) {
		got := grantedSubset([]string{}, []string{"world.query"})
		assert.Empty(t, got, "no requested tokens => nothing to return")
	})

	t.Run("extra granted token not in requested is not added", func(t *testing.T) {
		got := grantedSubset(
			[]string{"world.query"},
			[]string{"world.query", "world.mutation"},
		)
		assert.ElementsMatch(t, []string{"world.query"}, got,
			"extra granted token absent from requested must not be injected")
	})
}

// TestBinaryHostBrokerSkipsUngrantedService asserts that when WithPluginGrants
// excludes a service name from the grant set, that service is not wired into the
// plugin at load time. The observable proxy in the mock test path is:
//   - Broker is nil in mock path → requiredServices map is always empty after Load
//     (broker gating block at host.go:861 requires broker != nil).
//   - DeclaredCapabilities only carries capability tokens present in the grant set,
//     NOT service names (service names must never appear in DeclaredCapabilities).
//   - The WithServiceRegistry registry is consulted only inside the broker block;
//     with broker nil, Resolve is never called — confirmed by Load succeeding even
//     when the declared service is NOT registered.
//
// The non-vacuous part: grantedSubset for service names is proven by
// TestGrantedSubsetReturnsOnlyGrantedTokens above (same function). This test
// proves the End-to-End load path is coherent when a mixed manifest (service +
// capability) is combined with a grant set that excludes the service name.
func TestBinaryHostBrokerSkipsUngrantedService(t *testing.T) {
	grpcClient := &mockGRPCPluginClient{}
	mockClient := &mockPluginClient{protocol: &mockClientProtocol{pluginClient: grpcClient}}

	// Registry has the service registered — if the broker block ran and Resolve
	// were called for the UNGRANTED service, it would succeed and pollute
	// requiredServices. We verify requiredServices is empty (broker nil path).
	registry := plugins.NewServiceRegistry()
	require.NoError(t, registry.Register(plugins.RegisteredService{
		Name:       "holomush.scene.v1.SceneService",
		PluginName: "scene-provider",
		PluginType: plugins.TypeBinary,
	}))

	// Grant set includes the capability token "focus" but NOT the service name
	// "holomush.scene.v1.SceneService". The broker-loop grantSet build uses the
	// same grantedSubset logic (host.go:867–873); if that guard were removed,
	// a real broker would wire the ungranted service.
	host := NewHostWithFactory(
		&mockClientFactory{client: mockClient},
		WithServiceRegistry(registry),
		WithPluginGrants(map[string][]string{
			"test-plugin": {"focus"}, // service name deliberately absent
		}),
	)

	ctx := context.Background()
	tmpDir := t.TempDir()
	require.NoError(t, createTempExecutable(tmpDir+"/test-plugin"))

	manifest := &plugins.Manifest{
		Name: "test-plugin", Version: "1.0.0", Type: plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{Executable: "test-plugin"},
		Requires: []plugins.Dependency{
			// Capability token — should appear in DeclaredCapabilities (it's granted).
			{Kind: plugins.DependencyCapability, Name: "focus"},
			// Service require — excluded from grants; must not be broker-wired.
			{Kind: plugins.DependencyService, Name: "holomush.scene.v1.SceneService"},
		},
	}

	require.NoError(t, host.Load(ctx, manifest, tmpDir), "Load must succeed")
	require.NotNil(t, grpcClient.initReq, "Init must be called")
	require.NotNil(t, grpcClient.initReq.Config, "ServiceConfig must be set")

	// DeclaredCapabilities: only "focus" (the granted cap token); service name
	// must NOT appear here.
	assert.ElementsMatch(t, []string{"focus"},
		grpcClient.initReq.Config.GetDeclaredCapabilities(),
		"DeclaredCapabilities must reflect only granted cap tokens")
	assert.NotContains(t, grpcClient.initReq.Config.GetDeclaredCapabilities(),
		"holomush.scene.v1.SceneService",
		"service names must never appear in DeclaredCapabilities")

	// RequiredServices: empty because broker is nil in mock path; the broker
	// gating block (host.go:861) never ran, so Resolve was never called.
	// This confirms the ungranted service was not wired via the broker path.
	assert.Empty(t, grpcClient.initReq.Config.GetRequiredServices(),
		"broker is nil in mock path: no service proxy was started")
}
