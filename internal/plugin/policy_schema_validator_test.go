// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/types"
	plugins "github.com/holomush/holomush/internal/plugin"
)

func TestValidateManifestPolicySchemasRejectsPolicyReferencingAttributeNotInSchema(t *testing.T) {
	m := &plugins.Manifest{
		Name:          "test-abac-widget",
		Version:       "1.0.0",
		Type:          plugins.TypeBinary,
		ResourceTypes: []string{"widget"},
		Policies: []plugins.ManifestPolicy{
			{
				Name: "widget-read-normal",
				DSL: `permit(principal is character, action in ["read"], resource is widget) ` +
					`when { resource.widget.tipe == "normal" };`,
			},
		},
	}
	schemas := map[string]*types.NamespaceSchema{
		"widget": {
			Attributes: map[string]types.AttrType{
				"type":  types.AttrTypeString,
				"owner": types.AttrTypeString,
			},
		},
	}

	err := plugins.ValidateManifestPolicySchemas(m, schemas)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tipe")
	assert.Contains(t, err.Error(), "widget-read-normal")
	assert.Contains(t, err.Error(), "widget")
	assert.Contains(t, err.Error(), "not in the declared schema")
}

func TestValidateManifestPolicySchemasAcceptsPolicyWhenAllAttributeReferencesMatchSchema(t *testing.T) {
	m := &plugins.Manifest{
		Name:          "test-abac-widget",
		ResourceTypes: []string{"widget"},
		Policies: []plugins.ManifestPolicy{
			{
				Name: "widget-read-normal",
				DSL: `permit(principal is character, action in ["read"], resource is widget) ` +
					`when { resource.widget.type == "normal" };`,
			},
		},
	}
	schemas := map[string]*types.NamespaceSchema{
		"widget": {
			Attributes: map[string]types.AttrType{
				"type":  types.AttrTypeString,
				"owner": types.AttrTypeString,
			},
		},
	}

	err := plugins.ValidateManifestPolicySchemas(m, schemas)
	assert.NoError(t, err)
}

func TestValidateManifestPolicySchemasReturnsNilForPluginWithoutResourceTypes(t *testing.T) {
	m := &plugins.Manifest{
		Name: "simple-plugin",
		Policies: []plugins.ManifestPolicy{
			{
				Name: "exec",
				DSL: `permit(principal is character, action in ["execute"], resource is command) ` +
					`when { resource.command.name == "simple" };`,
			},
		},
	}
	// schemas is nil — plugin has no custom resource types.
	err := plugins.ValidateManifestPolicySchemas(m, nil)
	assert.NoError(t, err)
}

func TestValidateManifestPolicySchemasAcceptsPolicyWithoutWhenClause(t *testing.T) {
	m := &plugins.Manifest{
		Name:          "test-abac-widget",
		ResourceTypes: []string{"widget"},
		Policies: []plugins.ManifestPolicy{
			{
				Name: "widget-unconstrained",
				DSL:  `permit(principal is character, action in ["read"], resource is widget);`,
			},
		},
	}
	schemas := map[string]*types.NamespaceSchema{
		"widget": {Attributes: map[string]types.AttrType{"type": types.AttrTypeString}},
	}

	err := plugins.ValidateManifestPolicySchemas(m, schemas)
	assert.NoError(t, err)
}

func TestValidateManifestPolicySchemasIgnoresEnvironmentAndPrincipalAttributeReferences(t *testing.T) {
	m := &plugins.Manifest{
		Name:          "test-abac-widget",
		ResourceTypes: []string{"widget"},
		Policies: []plugins.ManifestPolicy{
			{
				Name: "widget-time-gated",
				DSL: `permit(principal is character, action in ["read"], resource is widget) ` +
					`when { principal.character.role == "admin" };`,
			},
		},
	}
	schemas := map[string]*types.NamespaceSchema{
		"widget": {Attributes: map[string]types.AttrType{"type": types.AttrTypeString}},
	}

	// Policy references principal.character.role but NOT any widget attribute.
	// The validator should not false-positive on principal references.
	err := plugins.ValidateManifestPolicySchemas(m, schemas)
	assert.NoError(t, err)
}

func TestValidateManifestPolicySchemasReturnsNilForEmptyNonNilSchemaMap(t *testing.T) {
	m := &plugins.Manifest{
		Name: "empty-plugin",
	}
	schemas := map[string]*types.NamespaceSchema{} // non-nil, length 0

	err := plugins.ValidateManifestPolicySchemas(m, schemas)
	assert.NoError(t, err)
}

func TestValidateManifestPolicySchemasReturnsNilWhenPolicyTypeWasAlreadyRejectedByPluginValidator(t *testing.T) {
	// This test documents the layering assumption: ValidatePluginPolicy
	// runs before ValidateManifestPolicySchemas and rejects policies
	// targeting resource types not in the plugin's resource_types. If
	// such a policy reaches this validator (shouldn't happen in practice),
	// we skip rather than error — the rejection is another validator's
	// responsibility.
	m := &plugins.Manifest{
		Name:          "test-plugin",
		ResourceTypes: []string{"widget"},
		Policies: []plugins.ManifestPolicy{
			{
				Name: "gadget-policy",
				DSL: `permit(principal is character, action in ["read"], resource is gadget) ` +
					`when { resource.gadget.color == "red" };`,
			},
		},
	}
	schemas := map[string]*types.NamespaceSchema{
		"widget": {Attributes: map[string]types.AttrType{"type": types.AttrTypeString}},
	}

	err := plugins.ValidateManifestPolicySchemas(m, schemas)
	assert.NoError(t, err, "policy targeting an out-of-schema type is skipped by this validator")
}

func TestValidateManifestPolicySchemasReportsFirstErrorWhenMultiplePoliciesHaveBadAttributes(t *testing.T) {
	m := &plugins.Manifest{
		Name:          "test-abac-widget",
		ResourceTypes: []string{"widget"},
		Policies: []plugins.ManifestPolicy{
			{
				Name: "widget-bad-first",
				DSL: `permit(principal is character, action in ["read"], resource is widget) ` +
					`when { resource.widget.alpha == "x" };`,
			},
			{
				Name: "widget-good",
				DSL: `permit(principal is character, action in ["read"], resource is widget) ` +
					`when { resource.widget.type == "normal" };`,
			},
			{
				Name: "widget-bad-second",
				DSL: `permit(principal is character, action in ["read"], resource is widget) ` +
					`when { resource.widget.beta == "y" };`,
			},
		},
	}
	schemas := map[string]*types.NamespaceSchema{
		"widget": {Attributes: map[string]types.AttrType{"type": types.AttrTypeString}},
	}

	err := plugins.ValidateManifestPolicySchemas(m, schemas)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "widget-bad-first", "first bad policy should be reported")
	assert.Contains(t, err.Error(), "alpha", "first bad attribute should be reported")
	assert.NotContains(t, err.Error(), "widget-bad-second", "second bad policy should not be in first error")
}

func TestValidateManifestPolicySchemasHandlesCompoundConditionsWithANDORNot(t *testing.T) {
	m := &plugins.Manifest{
		Name:          "test-abac-widget",
		ResourceTypes: []string{"widget"},
		Policies: []plugins.ManifestPolicy{
			{
				Name: "widget-compound",
				DSL: `permit(principal is character, action in ["read"], resource is widget) ` +
					`when { (resource.widget.type == "normal" || resource.widget.tipe == "restricted") && !(resource.widget.owner == "system") };`,
			},
		},
	}
	schemas := map[string]*types.NamespaceSchema{
		"widget": {
			Attributes: map[string]types.AttrType{
				"type":  types.AttrTypeString,
				"owner": types.AttrTypeString,
			},
		},
	}

	err := plugins.ValidateManifestPolicySchemas(m, schemas)
	require.Error(t, err, "compound condition with bad attribute should fail")
	assert.Contains(t, err.Error(), "tipe")
}

func TestValidateManifestPolicySchemasHandlesHasAndContainsReferences(t *testing.T) {
	// The DSL uses method-call syntax for set membership:
	// `resource.widget.members.containsAll(["admin"])` — NOT a `contains`
	// keyword. The validator must extract `members` from the AttrRef path
	// of the ContainsCondition and detect it is undeclared.
	m := &plugins.Manifest{
		Name:          "test-abac-widget",
		ResourceTypes: []string{"widget"},
		Policies: []plugins.ManifestPolicy{
			{
				Name: "widget-contains",
				DSL: `permit(principal is character, action in ["read"], resource is widget) ` +
					`when { resource.widget.members.containsAll(["admin"]) };`,
			},
		},
	}
	schemas := map[string]*types.NamespaceSchema{
		"widget": {
			Attributes: map[string]types.AttrType{"type": types.AttrTypeString},
		},
	}

	err := plugins.ValidateManifestPolicySchemas(m, schemas)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "members")
}

func TestValidateManifestPolicySchemasHandlesInExprWithDynamicBothSides(t *testing.T) {
	m := &plugins.Manifest{
		Name:          "test-abac-widget",
		ResourceTypes: []string{"widget"},
		Policies: []plugins.ManifestPolicy{
			{
				Name: "widget-in-dynamic",
				DSL: `permit(principal is character, action in ["read"], resource is widget) ` +
					`when { principal.character.player_id in resource.widget.members };`,
			},
		},
	}
	schemas := map[string]*types.NamespaceSchema{
		"widget": {
			Attributes: map[string]types.AttrType{"type": types.AttrTypeString},
		},
	}

	err := plugins.ValidateManifestPolicySchemas(m, schemas)
	require.Error(t, err, "`in` expression with undeclared resource attribute should fail")
	assert.Contains(t, err.Error(), "members")
}

func TestValidateManifestPolicySchemasAcceptsAttributeNameThatIsPrefixOfValidAttribute(t *testing.T) {
	m := &plugins.Manifest{
		Name:          "test-abac-widget",
		ResourceTypes: []string{"widget"},
		Policies: []plugins.ManifestPolicy{
			{
				Name: "widget-exact",
				DSL: `permit(principal is character, action in ["read"], resource is widget) ` +
					`when { resource.widget.type == "normal" };`,
			},
		},
	}
	// Schema has type_code AND type; both are distinct keys. The policy
	// references "type" (exactly), which is valid. Prefix matching would
	// erroneously think "type" is a prefix of "type_code" and could cause
	// a false positive in a buggy implementation.
	schemas := map[string]*types.NamespaceSchema{
		"widget": {
			Attributes: map[string]types.AttrType{
				"type_code": types.AttrTypeString,
				"type":      types.AttrTypeString,
			},
		},
	}

	err := plugins.ValidateManifestPolicySchemas(m, schemas)
	assert.NoError(t, err, "exact-match lookup must not substring-match")
}
