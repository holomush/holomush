// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePolicyAllowsPrincipalPlugin(t *testing.T) {
	ctx := PolicyValidationContext{
		PluginName:    "my-plugin",
		ResourceTypes: []string{"widget"},
		CommandNames:  []string{"widget"},
	}
	err := ValidatePluginPolicy(ctx, ManifestPolicy{
		Name: "allow-kv",
		DSL:  `permit(principal is plugin, action in ["kv:read"], resource is kv);`,
	})
	require.NoError(t, err)
}

func TestValidatePolicyAllowsCharacterOnDeclaredResourceType(t *testing.T) {
	ctx := PolicyValidationContext{
		PluginName:    "my-plugin",
		ResourceTypes: []string{"widget"},
		CommandNames:  []string{"widget"},
	}
	err := ValidatePluginPolicy(ctx, ManifestPolicy{
		Name: "widget-read",
		DSL:  `permit(principal is character, action in ["read"], resource is widget);`,
	})
	require.NoError(t, err)
}

func TestValidatePolicyAllowsCharacterOnCommandForOwnCommand(t *testing.T) {
	ctx := PolicyValidationContext{
		PluginName:    "my-plugin",
		ResourceTypes: []string{"widget"},
		CommandNames:  []string{"widget"},
	}
	err := ValidatePluginPolicy(ctx, ManifestPolicy{
		Name: "widget-execute",
		DSL:  `permit(principal is character, action in ["execute"], resource is command) when { resource.command.name == "widget" };`,
	})
	require.NoError(t, err)
}

func TestValidatePolicyRejectsCharacterOnProtectedType(t *testing.T) {
	ctx := PolicyValidationContext{
		PluginName:    "my-plugin",
		ResourceTypes: []string{"widget"},
		CommandNames:  []string{"widget"},
	}
	err := ValidatePluginPolicy(ctx, ManifestPolicy{
		Name: "bad-location",
		DSL:  `permit(principal is character, action in ["read"], resource is location);`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "protected")
}

func TestValidatePolicyRejectsCharacterOnUndeclaredType(t *testing.T) {
	ctx := PolicyValidationContext{
		PluginName:    "my-plugin",
		ResourceTypes: []string{"widget"},
		CommandNames:  []string{"widget"},
	}
	err := ValidatePluginPolicy(ctx, ManifestPolicy{
		Name: "bad-gadget",
		DSL:  `permit(principal is character, action in ["read"], resource is gadget);`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in plugin's resource_types")
}

func TestValidatePolicyRejectsCommandForForeignCommand(t *testing.T) {
	ctx := PolicyValidationContext{
		PluginName:    "my-plugin",
		ResourceTypes: []string{"widget"},
		CommandNames:  []string{"widget"},
	}
	err := ValidatePluginPolicy(ctx, ManifestPolicy{
		Name: "bad-execute",
		DSL:  `permit(principal is character, action in ["execute"], resource is command) when { resource.command.name == "look" };`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "foreign command")
}

func TestValidatePolicyRejectsPrincipalSystem(t *testing.T) {
	ctx := PolicyValidationContext{
		PluginName:    "my-plugin",
		ResourceTypes: []string{"widget"},
		CommandNames:  []string{"widget"},
	}
	err := ValidatePluginPolicy(ctx, ManifestPolicy{
		Name: "system-abuse",
		DSL:  `permit(principal is system, action in ["write"], resource is widget);`,
	})
	require.Error(t, err)
}

func TestValidatePolicyAllowsElevatedTrustWithAllPrincipals(t *testing.T) {
	ctx := PolicyValidationContext{
		PluginName:     "exotic-plugin",
		ResourceTypes:  []string{},
		CommandNames:   []string{},
		TrustEscalated: true,
	}
	err := ValidatePluginPolicy(ctx, ManifestPolicy{
		Name: "broad-policy",
		DSL:  `permit(principal is character, action in ["read"], resource is location);`,
	})
	require.NoError(t, err)
}

func TestValidatePolicyRejectsInvalidDSL(t *testing.T) {
	ctx := PolicyValidationContext{
		PluginName:    "my-plugin",
		ResourceTypes: []string{"widget"},
		CommandNames:  []string{"widget"},
	}
	err := ValidatePluginPolicy(ctx, ManifestPolicy{
		Name: "bad-syntax",
		DSL:  `not valid dsl at all`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid DSL")
}

func TestValidatePolicyRejectsCommandWithoutNameCondition(t *testing.T) {
	ctx := PolicyValidationContext{
		PluginName:    "my-plugin",
		ResourceTypes: []string{"widget"},
		CommandNames:  []string{"widget"},
	}
	err := ValidatePluginPolicy(ctx, ManifestPolicy{
		Name: "all-commands",
		DSL:  `permit(principal is character, action in ["execute"], resource is command);`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "trust escalation")
}

func TestValidatePolicyRejectsUnconstrainedResource(t *testing.T) {
	ctx := PolicyValidationContext{
		PluginName:    "my-plugin",
		ResourceTypes: []string{"widget"},
		CommandNames:  []string{"widget"},
	}
	err := ValidatePluginPolicy(ctx, ManifestPolicy{
		Name: "wildcard",
		DSL:  `permit(principal is character, action in ["read"], resource);`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "trust escalation")
}

func TestValidatePolicyRejectsUnrecognizedPrincipalType(t *testing.T) {
	// The validator's switch defaults on unknown principal types like "alien".
	ctx := PolicyValidationContext{
		PluginName:    "my-plugin",
		ResourceTypes: []string{"widget"},
		CommandNames:  []string{"widget"},
	}
	err := ValidatePluginPolicy(ctx, ManifestPolicy{
		Name: "alien-principal",
		DSL:  `permit(principal is alien, action in ["read"], resource is widget);`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unrecognized principal type")
}

func TestValidatePolicyRejectsBarePrincipalWithoutTrustEscalation(t *testing.T) {
	// `principal` (no `is X`) parses with empty Type. This is broader than
	// `principal is character` and would let wildcard policies through the
	// trust gate, so it must require trust escalation.
	ctx := PolicyValidationContext{
		PluginName:    "my-plugin",
		ResourceTypes: []string{"widget"},
		CommandNames:  []string{"widget"},
	}
	err := ValidatePluginPolicy(ctx, ManifestPolicy{
		Name: "bare-principal",
		DSL:  `permit(principal, action in ["read"], resource is widget);`,
	})
	require.Error(t, err, "bare principal must require trust escalation")
	assert.Contains(t, err.Error(), "trust escalation")
}

func TestValidatePolicyAllowsBarePrincipalWithTrustEscalation(t *testing.T) {
	// With trust escalation explicitly granted, an unconstrained principal
	// is allowed. The trust.all_principals escape hatch must agree.
	ctx := PolicyValidationContext{
		PluginName:     "exotic-plugin",
		ResourceTypes:  []string{"widget"},
		CommandNames:   []string{"widget"},
		TrustEscalated: true,
	}
	err := ValidatePluginPolicy(ctx, ManifestPolicy{
		Name: "bare-principal-trusted",
		DSL:  `permit(principal, action in ["read"], resource is widget);`,
	})
	require.NoError(t, err, "trust-escalated bare principal should pass")
}

func TestValidatePolicyTrustEscalatedAllowsCommandWithoutNameCondition(t *testing.T) {
	// validateCharacterPolicy short-circuits on TrustEscalated before reaching
	// validateCommandPolicy, so even a command policy lacking a name condition
	// must succeed when trust escalation is in effect.
	ctx := PolicyValidationContext{
		PluginName:     "trusted",
		ResourceTypes:  []string{},
		CommandNames:   []string{},
		TrustEscalated: true,
	}
	err := ValidatePluginPolicy(ctx, ManifestPolicy{
		Name: "wildcard-execute",
		DSL:  `permit(principal is character, action in ["execute"], resource is command);`,
	})
	require.NoError(t, err)
}

func TestValidatePolicyRejectsCharacterCommandReferencingForeignName(t *testing.T) {
	// Two command names declared. The policy targets one that's not in the
	// plugin's set, exercising validateCommandPolicy's foreign-name branch.
	ctx := PolicyValidationContext{
		PluginName:    "my-plugin",
		ResourceTypes: []string{"widget"},
		CommandNames:  []string{"widget-create", "widget-delete"},
	}
	err := ValidatePluginPolicy(ctx, ManifestPolicy{
		Name: "foreign-cmd",
		DSL:  `permit(principal is character, action in ["execute"], resource is command) when { resource.command.name == "shutdown" };`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "foreign command")
}
