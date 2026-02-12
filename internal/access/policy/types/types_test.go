// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEffect_String(t *testing.T) {
	tests := []struct {
		name     string
		effect   Effect
		expected string
	}{
		{"default deny", EffectDefaultDeny, "default_deny"},
		{"allow", EffectAllow, "allow"},
		{"deny", EffectDeny, "deny"},
		{"system bypass", EffectSystemBypass, "system_bypass"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.effect.String())
		})
	}
}

func TestEffect_String_NegativeValue(t *testing.T) {
	assert.Equal(t, "unknown(-1)", Effect(-1).String())
	assert.Equal(t, "unknown(-42)", Effect(-42).String())
}

func TestAttrType_String_NegativeValue(t *testing.T) {
	assert.Equal(t, "unknown(-1)", AttrType(-1).String())
	assert.Equal(t, "unknown(-42)", AttrType(-42).String())
}

func TestNewDecision_Invariant(t *testing.T) {
	tests := []struct {
		name            string
		effect          Effect
		reason          string
		policyID        string
		expectedAllowed bool
	}{
		{"allow grants access", EffectAllow, "policy matched", "pol-1", true},
		{"system bypass grants access", EffectSystemBypass, "system op", "system", true},
		{"deny refuses access", EffectDeny, "forbidden", "pol-2", false},
		{"default deny refuses access", EffectDefaultDeny, "no policy matched", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewDecision(tt.effect, tt.reason, tt.policyID)
			assert.Equal(t, tt.expectedAllowed, d.IsAllowed())
			assert.Equal(t, tt.effect, d.Effect)
			assert.Equal(t, tt.reason, d.Reason)
			assert.Equal(t, tt.policyID, d.PolicyID)
			// Verify unexported field directly
			assert.Equal(t, tt.expectedAllowed, d.allowed)
		})
	}
}

func TestDecision_Validate(t *testing.T) {
	tests := []struct {
		name      string
		decision  Decision
		expectErr bool
	}{
		{
			name:      "valid allow decision",
			decision:  Decision{allowed: true, Effect: EffectAllow, Reason: "ok"},
			expectErr: false,
		},
		{
			name:      "valid system bypass decision",
			decision:  Decision{allowed: true, Effect: EffectSystemBypass, Reason: "system"},
			expectErr: false,
		},
		{
			name:      "valid deny decision",
			decision:  Decision{allowed: false, Effect: EffectDeny, Reason: "forbidden"},
			expectErr: false,
		},
		{
			name:      "valid default deny decision",
			decision:  Decision{allowed: false, Effect: EffectDefaultDeny, Reason: "no match"},
			expectErr: false,
		},
		{
			name:      "invalid: allowed true but effect deny",
			decision:  Decision{allowed: true, Effect: EffectDeny, Reason: "broken"},
			expectErr: true,
		},
		{
			name:      "invalid: allowed true but effect default deny",
			decision:  Decision{allowed: true, Effect: EffectDefaultDeny, Reason: "broken"},
			expectErr: true,
		},
		{
			name:      "invalid: allowed false but effect allow",
			decision:  Decision{allowed: false, Effect: EffectAllow, Reason: "broken"},
			expectErr: true,
		},
		{
			name:      "invalid: allowed false but effect system bypass",
			decision:  Decision{allowed: false, Effect: EffectSystemBypass, Reason: "broken"},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.decision.Validate()
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPolicyEffect_ToEffect(t *testing.T) {
	tests := []struct {
		name     string
		pe       PolicyEffect
		expected Effect
	}{
		{"permit to allow", PolicyEffectPermit, EffectAllow},
		{"forbid to deny", PolicyEffectForbid, EffectDeny},
		{"unknown to default deny", PolicyEffect("bogus"), EffectDefaultDeny},
		{"empty to default deny", PolicyEffect(""), EffectDefaultDeny},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.pe.ToEffect())
		})
	}
}

func TestPolicyEffect_String(t *testing.T) {
	tests := []struct {
		name     string
		pe       PolicyEffect
		expected string
	}{
		{"permit", PolicyEffectPermit, "permit"},
		{"forbid", PolicyEffectForbid, "forbid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.pe.String())
		})
	}
}

func TestAccessRequest_Fields(t *testing.T) {
	req := AccessRequest{
		Subject:  "character:01ABC",
		Action:   "read",
		Resource: "location:01XYZ",
	}
	assert.Equal(t, "character:01ABC", req.Subject)
	assert.Equal(t, "read", req.Action)
	assert.Equal(t, "location:01XYZ", req.Resource)
}

func TestAttributeBags_Initialization(t *testing.T) {
	bags := NewAttributeBags()
	require.NotNil(t, bags.Subject)
	require.NotNil(t, bags.Resource)
	require.NotNil(t, bags.Action)
	require.NotNil(t, bags.Environment)

	// Should be empty maps, not nil
	assert.Empty(t, bags.Subject)
	assert.Empty(t, bags.Resource)
	assert.Empty(t, bags.Action)
	assert.Empty(t, bags.Environment)
}

func TestPolicyMatch_Fields(t *testing.T) {
	pm := PolicyMatch{
		PolicyID:      "pol-123",
		PolicyName:    "allow-read",
		Effect:        EffectAllow,
		ConditionsMet: true,
	}
	assert.Equal(t, "pol-123", pm.PolicyID)
	assert.Equal(t, "allow-read", pm.PolicyName)
	assert.Equal(t, EffectAllow, pm.Effect)
	assert.True(t, pm.ConditionsMet)
}

func TestAttrType_String(t *testing.T) {
	tests := []struct {
		name     string
		at       AttrType
		expected string
	}{
		{"string", AttrTypeString, "string"},
		{"int", AttrTypeInt, "int"},
		{"float", AttrTypeFloat, "float"},
		{"bool", AttrTypeBool, "bool"},
		{"string list", AttrTypeStringList, "string_list"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.at.String())
		})
	}
}

func TestPolicySource_Constants(t *testing.T) {
	assert.Equal(t, PolicySource("seed"), PolicySourceSeed)
	assert.Equal(t, PolicySource("lock"), PolicySourceLock)
	assert.Equal(t, PolicySource("admin"), PolicySourceAdmin)
	assert.Equal(t, PolicySource("plugin"), PolicySourcePlugin)
}

func TestPropertyVisibility_Constants(t *testing.T) {
	assert.Equal(t, PropertyVisibility("public"), PropertyVisibilityPublic)
	assert.Equal(t, PropertyVisibility("private"), PropertyVisibilityPrivate)
	assert.Equal(t, PropertyVisibility("restricted"), PropertyVisibilityRestricted)
	assert.Equal(t, PropertyVisibility("system"), PropertyVisibilitySystem)
	assert.Equal(t, PropertyVisibility("admin"), PropertyVisibilityAdmin)
}

func TestEntityType_Constants(t *testing.T) {
	assert.Equal(t, EntityType("character"), EntityTypeCharacter)
	assert.Equal(t, EntityType("location"), EntityTypeLocation)
	assert.Equal(t, EntityType("object"), EntityTypeObject)
}

func TestAttributeSchema_NewEmpty(t *testing.T) {
	schema := NewAttributeSchema()
	require.NotNil(t, schema)
	assert.Empty(t, schema.namespaces)
}
