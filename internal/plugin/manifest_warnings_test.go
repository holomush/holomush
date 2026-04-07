// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
	plugins "github.com/holomush/holomush/internal/plugin"
)

func luaManifest(name string) *plugins.Manifest {
	return &plugins.Manifest{
		Name:      name,
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}
}

func TestCheckManifestWarningsNoWarningsForFullCoverage(t *testing.T) {
	m := luaManifest("widget-plugin")
	m.Commands = []plugins.CommandSpec{
		{Name: "widget-create"},
	}
	m.Policies = []plugins.ManifestPolicy{
		{
			Name: "allow-execute-widget-create",
			DSL: `permit(principal, action in ["execute"], resource is command)` +
				` when { resource.command.name == "widget-create" };`,
		},
	}

	warnings := plugins.CheckManifestWarnings(m)
	assert.Empty(t, warnings)
}

func TestCheckManifestWarningsDetectsMissingExecutePolicy(t *testing.T) {
	m := luaManifest("widget-plugin")
	m.Commands = []plugins.CommandSpec{
		{Name: "widget-create"},
	}
	// No policy in the manifest at all.

	warnings := plugins.CheckManifestWarnings(m)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "widget-create")
	assert.Contains(t, warnings[0], "no policy")
}

func TestCheckManifestWarningsDetectsMissingExecutePolicyForOneOfTwoCommands(t *testing.T) {
	m := luaManifest("widget-plugin")
	m.Commands = []plugins.CommandSpec{
		{Name: "widget-create"},
		{Name: "widget-delete"},
	}
	// Only covers widget-create.
	m.Policies = []plugins.ManifestPolicy{
		{
			Name: "allow-execute-widget-create",
			DSL: `permit(principal, action in ["execute"], resource is command)` +
				` when { resource.command.name == "widget-create" };`,
		},
	}

	warnings := plugins.CheckManifestWarnings(m)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "widget-delete")
}

func TestCheckManifestWarningsNoWarningWhenWildcardExecutePolicyCoversAllCommands(t *testing.T) {
	m := luaManifest("widget-plugin")
	m.Commands = []plugins.CommandSpec{
		{Name: "widget-create"},
		{Name: "widget-delete"},
	}
	// Wildcard policy covers all commands.
	m.Policies = []plugins.ManifestPolicy{
		{
			Name: "allow-execute-all",
			DSL:  `permit(principal, action in ["execute"], resource is command);`,
		},
	}

	warnings := plugins.CheckManifestWarnings(m)
	assert.Empty(t, warnings)
}

func TestCheckManifestWarningsDetectsMissingCapabilityPolicy(t *testing.T) {
	m := luaManifest("widget-plugin")
	m.Commands = []plugins.CommandSpec{
		{
			Name: "widget-create",
			Capabilities: []command.Capability{
				{Action: "write", Resource: "widget"},
			},
		},
	}
	// Has execute policy but no policy covering resource is widget.
	m.Policies = []plugins.ManifestPolicy{
		{
			Name: "allow-execute-widget-create",
			DSL: `permit(principal, action in ["execute"], resource is command)` +
				` when { resource.command.name == "widget-create" };`,
		},
	}

	warnings := plugins.CheckManifestWarnings(m)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "widget")
	assert.Contains(t, warnings[0], "widget-create")
}

func TestCheckManifestWarningsNoWarningWhenCapabilityPolicyExists(t *testing.T) {
	m := luaManifest("widget-plugin")
	m.Commands = []plugins.CommandSpec{
		{
			Name: "widget-create",
			Capabilities: []command.Capability{
				{Action: "write", Resource: "widget"},
			},
		},
	}
	m.Policies = []plugins.ManifestPolicy{
		{
			Name: "allow-execute-widget-create",
			DSL: `permit(principal, action in ["execute"], resource is command)` +
				` when { resource.command.name == "widget-create" };`,
		},
		{
			Name: "allow-write-widget",
			DSL:  `permit(principal, action in ["write"], resource is widget);`,
		},
	}

	warnings := plugins.CheckManifestWarnings(m)
	assert.Empty(t, warnings)
}

func TestCheckManifestWarningsNoWarningForKnownAttribute(t *testing.T) {
	m := luaManifest("widget-plugin")
	m.Policies = []plugins.ManifestPolicy{
		{
			Name: "allow-read-widget",
			DSL: `permit(principal, action in ["read"], resource is widget)` +
				` when { resource.widget.name == "foo" };`,
		},
	}

	// CheckManifestWarnings no longer cross-validates policy attribute
	// references against schemas — that check is a hard error path in
	// ValidateManifestPolicySchemas now. This test stays as a regression
	// guard that known-good attribute refs never leak into warnings.
	warnings := plugins.CheckManifestWarnings(m)
	assert.Empty(t, warnings)
}

func TestCheckManifestWarningsSchemaCheckSkippedWhenSchemasNil(t *testing.T) {
	m := luaManifest("widget-plugin")
	m.Policies = []plugins.ManifestPolicy{
		{
			Name: "allow-read-widget",
			DSL: `permit(principal, action in ["read"], resource is widget)` +
				` when { resource.widget.nonexistent == "foo" };`,
		},
	}

	// nil schemas — no schema warnings expected.
	warnings := plugins.CheckManifestWarnings(m)
	assert.Empty(t, warnings)
}

func TestCheckManifestWarningsEmptyManifestProducesNoWarnings(t *testing.T) {
	m := luaManifest("minimal")
	warnings := plugins.CheckManifestWarnings(m)
	assert.Empty(t, warnings)
}

func TestCheckManifestWarningsCommandWithTwoCapabilitiesOneUncovered(t *testing.T) {
	// Command declares two distinct capability resource types. Only "widget"
	// has a covering policy; "gadget" should produce a warning.
	m := luaManifest("widget-plugin")
	m.Commands = []plugins.CommandSpec{
		{
			Name: "widget-create",
			Capabilities: []command.Capability{
				{Action: "write", Resource: "widget"},
				{Action: "write", Resource: "gadget"},
			},
		},
	}
	m.Policies = []plugins.ManifestPolicy{
		{
			Name: "allow-execute-widget-create",
			DSL: `permit(principal, action in ["execute"], resource is command)` +
				` when { resource.command.name == "widget-create" };`,
		},
		{
			Name: "allow-write-widget",
			DSL:  `permit(principal, action in ["write"], resource is widget);`,
		},
	}

	warnings := plugins.CheckManifestWarnings(m)
	require.Len(t, warnings, 1, "exactly one capability should be flagged as uncovered")
	assert.Contains(t, warnings[0], "gadget", "the missing resource type should be named")
	assert.NotContains(t, warnings[0], "widget-create"+`"`+" declares capability on resource type \"widget\"",
		"covered widget capability should not appear in warnings")
}

func TestCheckManifestWarningsCommandWithDistinctCapabilityActionsWarnsPerAction(t *testing.T) {
	// Capability coverage is keyed by (resource, action) — write and read on
	// the same resource are independent capabilities and produce separate
	// warnings when uncovered. Otherwise a missing "write" coverage would be
	// hidden by an existing "read" policy on the same resource type.
	m := luaManifest("widget-plugin")
	m.Commands = []plugins.CommandSpec{
		{
			Name: "widget-create",
			Capabilities: []command.Capability{
				{Action: "write", Resource: "gadget"},
				{Action: "read", Resource: "gadget"},
			},
		},
	}
	m.Policies = []plugins.ManifestPolicy{
		{
			Name: "allow-execute-widget-create",
			DSL: `permit(principal, action in ["execute"], resource is command)` +
				` when { resource.command.name == "widget-create" };`,
		},
	}

	warnings := plugins.CheckManifestWarnings(m)
	require.Len(t, warnings, 2, "write and read are independent capabilities")
	// Both warnings should reference gadget but for different actions.
	allMessages := warnings[0] + " | " + warnings[1]
	assert.Contains(t, allMessages, `"write"`)
	assert.Contains(t, allMessages, `"read"`)
	assert.Contains(t, warnings[0], "gadget")
	assert.Contains(t, warnings[1], "gadget")
}

func TestCheckManifestWarningsCapabilityCoverageRequiresMatchingAction(t *testing.T) {
	// A "read" policy on the resource does NOT cover a "write" capability —
	// per-action checks prevent the false-positive coverage.
	m := luaManifest("widget-plugin")
	m.Commands = []plugins.CommandSpec{
		{
			Name: "widget-update",
			Capabilities: []command.Capability{
				{Action: "write", Resource: "widget"},
			},
		},
	}
	m.Policies = []plugins.ManifestPolicy{
		{
			Name: "allow-execute",
			DSL: `permit(principal is character, action in ["execute"], resource is command)` +
				` when { resource.command.name == "widget-update" };`,
		},
		{
			// This policy permits READ on widget — should NOT cover the write capability.
			Name: "read-widget",
			DSL:  `permit(principal is character, action in ["read"], resource is widget);`,
		},
	}

	warnings := plugins.CheckManifestWarnings(m)
	require.Len(t, warnings, 1, "read policy must not cover write capability")
	assert.Contains(t, warnings[0], `"write"`)
	assert.Contains(t, warnings[0], "widget")
}

func TestCheckManifestWarningsExecuteCoverageRequiresCharacterPrincipal(t *testing.T) {
	// A `principal is plugin` execute policy does NOT cover character commands.
	// Without this filter, a plugin-self policy would silently mask the missing
	// character coverage, and the runtime ABAC check would fail.
	m := luaManifest("widget-plugin")
	m.Commands = []plugins.CommandSpec{
		{Name: "widget"},
	}
	m.Policies = []plugins.ManifestPolicy{
		{
			Name: "plugin-only-execute",
			DSL: `permit(principal is plugin, action in ["execute"], resource is command)` +
				` when { resource.command.name == "widget" };`,
		},
	}

	warnings := plugins.CheckManifestWarnings(m)
	require.Len(t, warnings, 1, "plugin-principal execute policy must not satisfy character coverage")
	assert.Contains(t, warnings[0], "widget")
	assert.Contains(t, warnings[0], "no policy that permits execute")
}

func TestCheckManifestWarningsExecutePolicyWithInListCoversCommand(t *testing.T) {
	// `resource.command.name in ["a","b"]` should cover commands "a" and "b"
	// without producing warnings.
	m := luaManifest("widget-plugin")
	m.Commands = []plugins.CommandSpec{
		{Name: "widget-create"},
		{Name: "widget-delete"},
	}
	m.Policies = []plugins.ManifestPolicy{
		{
			Name: "allow-list",
			DSL: `permit(principal, action in ["execute"], resource is command)` +
				` when { resource.command.name in ["widget-create", "widget-delete"] };`,
		},
	}

	warnings := plugins.CheckManifestWarnings(m)
	assert.Empty(t, warnings)
}

func TestCheckManifestWarningsParenthesizedExecuteConditionCoversCommand(t *testing.T) {
	// Parenthesized condition exercises the Parenthesized branch of the
	// ExtractCommandNames AST walker.
	m := luaManifest("widget-plugin")
	m.Commands = []plugins.CommandSpec{{Name: "widget-create"}}
	m.Policies = []plugins.ManifestPolicy{
		{
			Name: "allow-paren",
			DSL: `permit(principal, action in ["execute"], resource is command)` +
				` when { (resource.command.name == "widget-create") };`,
		},
	}

	warnings := plugins.CheckManifestWarnings(m)
	assert.Empty(t, warnings)
}

func TestCheckManifestWarningsSchemaCheckSkippedWhenResourceTypeNotInSchema(t *testing.T) {
	// Policy targets "gadget" — CheckManifestWarnings no longer cross-checks
	// attribute references against schemas (that's ValidateManifestPolicySchemas'
	// job), so no warnings should surface here regardless of schema shape.
	m := luaManifest("widget-plugin")
	m.Policies = []plugins.ManifestPolicy{
		{
			Name: "gadget-policy",
			DSL: `permit(principal, action in ["read"], resource is gadget)` +
				` when { resource.gadget.color == "red" };`,
		},
	}

	warnings := plugins.CheckManifestWarnings(m)
	assert.Empty(t, warnings, "CheckManifestWarnings must not warn about attribute refs")
}
