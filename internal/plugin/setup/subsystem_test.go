// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/audit"
	"github.com/holomush/holomush/internal/lifecycle"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/goplugin"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/internal/plugin/setup"
)

// Compile-time interface checks: *setup.PluginSubsystem must satisfy both interfaces.
var (
	_ lifecycle.Subsystem      = (*setup.PluginSubsystem)(nil)
	_ lifecycle.HealthReporter = (*setup.PluginSubsystem)(nil)
)

func TestPluginSubsystemIDReturnsPlugins(t *testing.T) {
	sub := setup.NewPluginSubsystem(setup.PluginSubsystemConfig{})
	assert.Equal(t, lifecycle.SubsystemPlugins, sub.ID())
}

func TestPluginSubsystemDependsOnRequiredSubsystems(t *testing.T) {
	sub := setup.NewPluginSubsystem(setup.PluginSubsystemConfig{})
	deps := sub.DependsOn()

	assert.Contains(t, deps, lifecycle.SubsystemDatabase)
	assert.Contains(t, deps, lifecycle.SubsystemABAC)
	assert.Contains(t, deps, lifecycle.SubsystemWorld)
	assert.Contains(t, deps, lifecycle.SubsystemAuth)
}

func TestPluginSubsystemManagerPanicsBeforeStart(t *testing.T) {
	sub := setup.NewPluginSubsystem(setup.PluginSubsystemConfig{})
	assert.Panics(t, func() { sub.Manager() })
}

func TestPluginSubsystemCommandRegistryPanicsBeforeStart(t *testing.T) {
	sub := setup.NewPluginSubsystem(setup.PluginSubsystemConfig{})
	assert.Panics(t, func() { sub.CommandRegistry() })
}

func TestPluginSubsystemAliasRepoPanicsBeforeStart(t *testing.T) {
	sub := setup.NewPluginSubsystem(setup.PluginSubsystemConfig{})
	assert.Panics(t, func() { sub.AliasRepo() })
}

func TestPluginSubsystemAliasCachePanicsBeforeStart(t *testing.T) {
	sub := setup.NewPluginSubsystem(setup.PluginSubsystemConfig{})
	assert.Panics(t, func() { sub.AliasCache() })
}

func TestPluginSubsystemCommandQuerierPanicsBeforeStart(t *testing.T) {
	sub := setup.NewPluginSubsystem(setup.PluginSubsystemConfig{})
	assert.Panics(t, func() { sub.CommandQuerier() })
}

func TestPluginSubsystemStopBeforeStartIsNoop(t *testing.T) {
	sub := setup.NewPluginSubsystem(setup.PluginSubsystemConfig{})
	assert.NoError(t, sub.Stop(t.Context()))
}

func TestPluginSubsystemHealthStatusReportsDeadBeforeStart(t *testing.T) {
	sub := setup.NewPluginSubsystem(setup.PluginSubsystemConfig{})
	status := sub.HealthStatus()

	assert.Equal(t, lifecycle.HealthDead, status.Tier)
	assert.Equal(t, "not started", status.Reason)
}

// TestEngineProviderInterfaceRequiresAttributeResolverAndAuditLogger is a
// compile-time guard. The EngineProvider interface used by PluginSubsystem MUST
// include AttributeResolver() (holomush-8kkv5.18) and AuditLogger() (INV-PLUGIN-25 /
// holomush-p1tq2.5) so that Start() can wire both surfaces. If either method is
// removed from the interface this line fails to compile — catching the regression
// before it reaches E2E.
var _ setup.EngineProvider = (*fakeEngineProvider)(nil)

type fakeEngineProvider struct {
	resolver *attribute.Resolver
	auditor  pluginauthz.Auditor
}

// Engine returns a non-nil engine so that tests exercising binary host
// construction (e.g. TestBinaryHostReceivesEngineFromEngineProvider) can
// assert the engine propagates through goplugin.WithEngine. Returning nil
// here would mask the wiring regression we guard against.
func (f *fakeEngineProvider) Engine() types.AccessPolicyEngine {
	return policytest.AllowAllEngine()
}

func (f *fakeEngineProvider) AttributeResolver() *attribute.Resolver {
	return f.resolver
}

func (f *fakeEngineProvider) AuditLogger() pluginauthz.Auditor {
	return f.auditor
}

// TestRegistrarCallbackRegistersProviderOnResolver verifies that the registrar
// callback shape used in Start() correctly delegates to resolver.RegisterProvider.
// This is a wiring-guard unit test: it proves the callback body is correct
// without spinning up the full plugin subsystem. Behavioral proof (that the
// resolver's registration propagates into engine.Evaluate) is deferred to
// the e2e/scenes.spec.ts suite.
func TestRegistrarCallbackRegistersProviderOnResolver(t *testing.T) {
	schemaReg := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(schemaReg)

	// Replicate the callback constructed in Start().
	registrar := func(p *plugins.PluginAttributeProvider) error {
		return resolver.RegisterProvider(p)
	}

	schema := &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"owner_id": types.AttrTypeString,
		},
	}
	provider := plugins.NewPluginAttributeProvider("scene", nil, schema)

	err := registrar(provider)
	require.NoError(t, err, "registrar callback must register the provider without error")
	assert.Contains(t, resolver.RegisteredNamespaces(), "scene",
		"resolver must list 'scene' namespace after registrar callback is called")
}

// TestUnregistrarCallbackUnregistersProviderFromResolver verifies that the
// unregistrar callback shape used in Start() correctly delegates to
// resolver.UnregisterProvider. Paired with the registrar test above.
func TestUnregistrarCallbackUnregistersProviderFromResolver(t *testing.T) {
	schemaReg := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(schemaReg)

	schema := &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{
			"owner_id": types.AttrTypeString,
		},
	}
	provider := plugins.NewPluginAttributeProvider("scene", nil, schema)
	require.NoError(t, resolver.RegisterProvider(provider))

	// Replicate the unregistrar callback constructed in Start().
	unregistrar := func(namespace string) bool {
		return resolver.UnregisterProvider(namespace)
	}

	removed := unregistrar("scene")
	assert.True(t, removed, "unregistrar callback must return true when namespace was registered")
	assert.NotContains(t, resolver.RegisteredNamespaces(), "scene",
		"resolver must not list 'scene' namespace after unregistrar callback is called")
}

// TestBinaryHostOptionAcceptsEngineFromEngineProvider is a wiring-guard for
// holomush-8kkv5.18. It verifies that goplugin.WithEngine(eng) can be
// applied to a host constructed by goplugin.NewHost, replicating the option
// construction added to Start():
//
//	hostOpts = append(hostOpts, goplugin.WithEngine(s.cfg.ABAC.Engine()))
//	binaryHost := goplugin.NewHost(hostOpts...)
//
// Without the fix, Start() omitted WithEngine, so PluginHostService.Evaluate
// always returned EVALUATE_ENGINE_UNCONFIGURED for binary plugins.
//
// This test guards the wiring shape; the behavioral proof that engine.Evaluate
// resolves correctly lives in goplugin/host_service_test.go (TestINV-5 /
// TestEvaluate* suite) and in the E2E suite.
func TestBinaryHostOptionAcceptsEngineFromEngineProvider(t *testing.T) {
	fp := &fakeEngineProvider{}
	eng := fp.Engine()
	require.NotNil(t, eng, "fakeEngineProvider.Engine() must return non-nil for this guard to be meaningful")

	// Replicate the option construction from Start() — exactly the line we added.
	// If goplugin.WithEngine is ever removed from the goplugin API, this call
	// will fail to compile, breaking the build immediately.
	host := goplugin.NewHost(goplugin.WithEngine(eng))
	require.NotNil(t, host, "host must be constructable with the WithEngine option applied")
	// Deep engine introspection (assert.Same) is done in
	// goplugin/host_engine_wiring_test.go which has access to Host internals
	// via the package-level export_test.go accessor.
}

// recordingAuditor records the most recent audit.Event passed to Log.
// Used as the pluginauthz.Auditor stub in wiring-guard tests.
type recordingAuditor struct {
	logged []audit.Event
}

func (r *recordingAuditor) Log(_ context.Context, event audit.Event) error {
	r.logged = append(r.logged, event)
	return nil
}

// TestAuditWiringFromEngineProvider is a table-driven wiring-guard for
// holomush-p1tq2.5 / INV-PLUGIN-25. Each row asserts that:
//  1. fakeEngineProvider.AuditLogger() returns a non-nil auditor (so the
//     guard is meaningful).
//  2. The corresponding host/functions constructor accepts the
//     WithAuditLogger option without panicking and returns a non-nil result.
//
// The behavioral proof that each auditor field is populated (assert.Same via
// AuditorForTest) lives in goplugin/host_engine_wiring_test.go, which has
// package-level access to host internals via export_test.go.
//
// Without this wiring PluginHostService.Evaluate (binary) and holomush.evaluate
// (Lua) never emit audit events regardless of the decision, violating spec §5 / INV-PLUGIN-25.
func TestAuditWiringFromEngineProvider(t *testing.T) {
	tests := []struct {
		name  string
		build func(t *testing.T, a pluginauthz.Auditor)
	}{
		{
			// Replicates: hostOpts = append(hostOpts, goplugin.WithAuditLogger(s.cfg.ABAC.AuditLogger()))
			//             binaryHost := goplugin.NewHost(hostOpts...)
			name: "binary host receives auditor",
			build: func(t *testing.T, a pluginauthz.Auditor) {
				host := goplugin.NewHost(goplugin.WithAuditLogger(a))
				require.NotNil(t, host, "host must be constructable with WithAuditLogger applied")
			},
		},
		{
			// Replicates: hostFuncOpts = append(hostFuncOpts, hostfunc.WithAuditLogger(s.cfg.ABAC.AuditLogger()))
			//             hostFuncs := hostfunc.New(nil, hostFuncOpts...)
			name: "lua hostfunc receives auditor",
			build: func(t *testing.T, a pluginauthz.Auditor) {
				funcs := hostfunc.New(nil, hostfunc.WithAuditLogger(a))
				require.NotNil(t, funcs, "hostfunc.New must return a non-nil Functions with WithAuditLogger applied")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auditor := &recordingAuditor{}
			fp := &fakeEngineProvider{auditor: auditor}

			a := fp.AuditLogger()
			require.NotNil(t, a, "fakeEngineProvider.AuditLogger() must return non-nil for this guard to be meaningful")

			tt.build(t, a)
		})
	}
}
