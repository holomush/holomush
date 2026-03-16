// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package attribute

import (
	"testing"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectSchemaChanges(t *testing.T) {
	tests := []struct {
		name        string
		oldSchema   *types.NamespaceSchema
		newSchema   *types.NamespaceSchema
		wantAdded   []string
		wantRemoved []string
		wantChanged []string
	}{
		{
			name:      "attribute added",
			oldSchema: &types.NamespaceSchema{Attributes: map[string]types.AttrType{"name": types.AttrTypeString}},
			newSchema: &types.NamespaceSchema{Attributes: map[string]types.AttrType{"name": types.AttrTypeString, "role": types.AttrTypeString}},
			wantAdded: []string{"role"},
		},
		{
			name:        "attribute removed",
			oldSchema:   &types.NamespaceSchema{Attributes: map[string]types.AttrType{"name": types.AttrTypeString, "role": types.AttrTypeString}},
			newSchema:   &types.NamespaceSchema{Attributes: map[string]types.AttrType{"name": types.AttrTypeString}},
			wantRemoved: []string{"role"},
		},
		{
			name:        "attribute type changed",
			oldSchema:   &types.NamespaceSchema{Attributes: map[string]types.AttrType{"level": types.AttrTypeString}},
			newSchema:   &types.NamespaceSchema{Attributes: map[string]types.AttrType{"level": types.AttrTypeInt}},
			wantChanged: []string{"level"},
		},
		{
			name:      "no changes",
			oldSchema: &types.NamespaceSchema{Attributes: map[string]types.AttrType{"name": types.AttrTypeString}},
			newSchema: &types.NamespaceSchema{Attributes: map[string]types.AttrType{"name": types.AttrTypeString}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changes := DetectSchemaChanges(tt.oldSchema, tt.newSchema)
			assert.ElementsMatch(t, tt.wantAdded, changes.Added)
			assert.ElementsMatch(t, tt.wantRemoved, changes.Removed)
			assert.ElementsMatch(t, tt.wantChanged, changes.TypeChanged)
		})
	}
}

func TestScanPoliciesForRemovedAttributes(t *testing.T) {
	refs := ScanPoliciesForAttributes("reputation", []string{"score"},
		[]string{`permit(...) when { resource.reputation.score > 5 };`})
	assert.Len(t, refs, 1)

	refs = ScanPoliciesForAttributes("reputation", []string{"score"},
		[]string{`permit(...) when { resource.character.name == "foo" };`})
	assert.Empty(t, refs)
}

func TestSchemaRegistry_UpdateNamespace(t *testing.T) {
	reg := NewSchemaRegistry()
	oldSchema := &types.NamespaceSchema{Attributes: map[string]types.AttrType{
		"name": types.AttrTypeString,
		"role": types.AttrTypeString,
	}}
	require.NoError(t, reg.Register("character", oldSchema))

	newSchema := &types.NamespaceSchema{Attributes: map[string]types.AttrType{
		"name":  types.AttrTypeString,
		"level": types.AttrTypeInt,
	}}

	changes, err := reg.UpdateNamespace("character", newSchema, nil)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"level"}, changes.Added)
	assert.ElementsMatch(t, []string{"role"}, changes.Removed)
	assert.True(t, reg.IsRegistered("character", "level"))
	assert.False(t, reg.IsRegistered("character", "role"))
}

func TestSchemaRegistry_RemoveNamespace_BlockedByPolicies(t *testing.T) {
	reg := NewSchemaRegistry()
	require.NoError(t, reg.Register("plugin_x", &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{"val": types.AttrTypeString},
	}))

	err := reg.RemoveNamespace("plugin_x", []string{`permit(...) when { resource.plugin_x.val == "y" };`})
	assert.Error(t, err)
}

func TestSchemaRegistry_RemoveNamespace_AllowedWhenUnreferenced(t *testing.T) {
	reg := NewSchemaRegistry()
	require.NoError(t, reg.Register("plugin_x", &types.NamespaceSchema{
		Attributes: map[string]types.AttrType{"val": types.AttrTypeString},
	}))

	err := reg.RemoveNamespace("plugin_x", nil)
	assert.NoError(t, err)
	assert.False(t, reg.HasNamespace("plugin_x"))
}
