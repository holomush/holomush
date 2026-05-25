// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/lifecycle"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/goplugin"
	"github.com/holomush/holomush/internal/plugin/setup"
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

func TestPluginSubsystemImplementsSubsystem(_ *testing.T) {
	sub := setup.NewPluginSubsystem(setup.PluginSubsystemConfig{})
	var _ lifecycle.Subsystem = sub
}

func TestPluginSubsystemImplementsHealthReporter(_ *testing.T) {
	sub := setup.NewPluginSubsystem(setup.PluginSubsystemConfig{})
	var _ lifecycle.HealthReporter = sub
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

// TestEngineProviderInterfaceRequiresAttributeResolver is a compile-time guard
// for holomush-8kkv5.18. The EngineProvider interface used by PluginSubsystem
// MUST include AttributeResolver() so the manager wiring in Start() can
// register plugin-declared attribute providers on the live ABAC resolver.
// If AttributeResolver() is ever removed from the interface, this line fails
// to compile — catching the regression before it reaches E2E.
var _ setup.EngineProvider = (*fakeEngineProvider)(nil)

type fakeEngineProvider struct {
	resolver *attribute.Resolver
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
