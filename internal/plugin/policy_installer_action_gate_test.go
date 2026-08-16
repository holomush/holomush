// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"

	"github.com/holomush/holomush/pkg/errutil"
)

// The `action` gate at the plugin-policy INSTALL path (02.2 review CR-02).
//
// Phase 02.2-04 made the compiler's `action` branch fatal for every policy
// source. Plugin-manifest policies were the source it did not reach: they were
// validated with dsl.Parse + dsl.CompilePolicy only, so a typo'd action.* key
// parsed clean, was PERSISTED by ReplaceBySource, and thereafter failed EVERY
// Cache.Reload — the reload is all-or-nothing, so one third-party plugin took
// the whole corpus into deny-all and failed the next boot, recoverable only by
// hand-deleting the row.
//
// The property under test is therefore not merely "returns an error": it is
// that the error arrives BEFORE persistence. Every case below asserts the fake
// store recorded nothing, which is what makes the plugin's own load-failure
// rollback (loader.go) the containment boundary instead of the operator's
// database.

// undeclaredActionKeyDSL carries `action.evnt_type` — the review's own typo of
// `event_type`, and exactly the shape that used to sail through install. It
// parses, it compiles as an AST, and it is not in
// attribute.ActionNamespaceSchema.
const undeclaredActionKeyDSL = `permit(principal is plugin, action in ["emit"], resource is stream) ` +
	`when { principal.plugin.name == "my-plugin" && action.evnt_type == "x" };`

// declaredActionKeyDSL is the non-vacuity control for every negative case
// below: it references a DECLARED action key, so a gate that rejected all
// `action.*` references — rather than only undeclared ones — would fail here
// and could not be mistaken for a working gate.
const declaredActionKeyDSL = `permit(principal is plugin, action in ["emit"], resource is stream) ` +
	`when { principal.plugin.name == "my-plugin" && action.dispatch_location == "loc" };`

func widgetManifest() *plugins.Manifest {
	return &plugins.Manifest{
		Name:          "my-plugin",
		Version:       "1.0.0",
		Type:          plugins.TypeBinary,
		ResourceTypes: []string{"widget"},
		Commands:      []plugins.CommandSpec{{Name: "widget"}},
	}
}

func TestInstallPluginPoliciesRejectsAnUndeclaredActionAttributeBeforePersisting(t *testing.T) {
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)

	err := installer.InstallPluginPolicies(context.Background(), "my-plugin",
		[]plugins.ManifestPolicy{{Name: "bad-action", DSL: undeclaredActionKeyDSL}})

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "POLICY_UNREGISTERED_ACTION_ATTRIBUTE")
	errutil.AssertErrorContext(t, err, "plugin", "my-plugin")
	errutil.AssertErrorContext(t, err, "policy", "bad-action")
	assert.Empty(t, fs.created,
		"the gate MUST fire before ReplaceBySource — a persisted row fails every later "+
			"Cache.Reload corpus-wide, which is the failure this gate exists to prevent")
	assert.Empty(t, fs.deletedSource,
		"ReplaceBySource must not have been reached at all")
}

func TestInstallPluginPoliciesWithManifestRejectsAnUndeclaredActionAttributeBeforePersisting(t *testing.T) {
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)

	err := installer.InstallPluginPoliciesWithManifest(context.Background(), widgetManifest(),
		[]plugins.ManifestPolicy{{Name: "bad-action", DSL: undeclaredActionKeyDSL}})

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "POLICY_UNREGISTERED_ACTION_ATTRIBUTE")
	assert.Empty(t, fs.created)
	assert.Empty(t, fs.deletedSource)
}

// The two Replace* paths are gated separately because they are the RUNTIME
// entry points: InstallPluginPoliciesWithManifest is what loader.go calls, but
// a hot-reload or a re-install goes through Replace*, and each has its own
// compile call. A gate wired into only one of the two compile helpers would
// leave the other open.
func TestReplacePluginPoliciesRejectsAnUndeclaredActionAttributeBeforePersisting(t *testing.T) {
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)

	err := installer.ReplacePluginPolicies(context.Background(), "my-plugin",
		[]plugins.ManifestPolicy{{Name: "bad-action", DSL: undeclaredActionKeyDSL}})

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "POLICY_UNREGISTERED_ACTION_ATTRIBUTE")
	assert.Empty(t, fs.created)
	assert.Empty(t, fs.deletedSource,
		"a rejected replace must not delete the plugin's existing rows either")
}

func TestReplacePluginPoliciesWithManifestRejectsAnUndeclaredActionAttributeBeforePersisting(t *testing.T) {
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)

	err := installer.ReplacePluginPoliciesWithManifest(context.Background(), widgetManifest(),
		[]plugins.ManifestPolicy{{Name: "bad-action", DSL: undeclaredActionKeyDSL}})

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "POLICY_UNREGISTERED_ACTION_ATTRIBUTE")
	assert.Empty(t, fs.created)
	assert.Empty(t, fs.deletedSource)
}

func TestInstallPluginPoliciesAcceptsADeclaredActionAttribute(t *testing.T) {
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)

	err := installer.InstallPluginPolicies(context.Background(), "my-plugin",
		[]plugins.ManifestPolicy{{Name: "good-action", DSL: declaredActionKeyDSL}})

	require.NoError(t, err,
		"control: action.dispatch_location IS in attribute.ActionNamespaceSchema. A gate that "+
			"rejected this would be rejecting the `action` namespace wholesale, not undeclared keys")
	require.Len(t, fs.created, 1)
	assert.Equal(t, "plugin:my-plugin:good-action", fs.created[0].Name)
}

func TestInstallPluginPoliciesWithManifestAcceptsADeclaredActionAttribute(t *testing.T) {
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)

	err := installer.InstallPluginPoliciesWithManifest(context.Background(), widgetManifest(),
		[]plugins.ManifestPolicy{{Name: "good-action", DSL: declaredActionKeyDSL}})

	require.NoError(t, err)
	require.Len(t, fs.created, 1)
}

// A policy referencing NO action attribute at all must be unaffected — the gate
// is scoped to the `action` DSL root and must stay silent about every other.
// Without this, a gate that rejected, say, every `resource.*` reference would
// still pass the cases above.
func TestInstallPluginPoliciesLeavesNonActionPoliciesAlone(t *testing.T) {
	fs := &fakePolicyStore{}
	installer := plugins.NewPolicyInstaller(fs)

	err := installer.InstallPluginPolicies(context.Background(), "my-plugin",
		[]plugins.ManifestPolicy{{
			Name: "no-action-ref",
			DSL: `permit(principal is plugin, action in ["emit"], resource is stream) ` +
				`when { principal.plugin.name == "my-plugin" && resource.stream.name == "s" };`,
		}})

	require.NoError(t, err)
	assert.Len(t, fs.created, 1)
}
