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
