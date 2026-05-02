// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
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

func TestEffectStringNegativeValue(t *testing.T) {
	assert.Equal(t, "unknown(-1)", Effect(-1).String())
	assert.Equal(t, "unknown(-42)", Effect(-42).String())
}

func TestAttrTypeStringNegativeValue(t *testing.T) {
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
			assert.Equal(t, tt.effect, d.Effect())
			assert.Equal(t, tt.reason, d.Reason())
			assert.Equal(t, tt.policyID, d.PolicyID())
			// Verify unexported fields directly
			assert.Equal(t, tt.expectedAllowed, d.allowed)
			assert.Equal(t, tt.effect, d.effect)
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
			decision:  Decision{allowed: true, effect: EffectAllow, reason: "ok"},
			expectErr: false,
		},
		{
			name:      "valid system bypass decision",
			decision:  Decision{allowed: true, effect: EffectSystemBypass, reason: "system"},
			expectErr: false,
		},
		{
			name:      "valid deny decision",
			decision:  Decision{allowed: false, effect: EffectDeny, reason: "forbidden"},
			expectErr: false,
		},
		{
			name:      "valid default deny decision",
			decision:  Decision{allowed: false, effect: EffectDefaultDeny, reason: "no match"},
			expectErr: false,
		},
		{
			name:      "invalid: allowed true but effect deny",
			decision:  Decision{allowed: true, effect: EffectDeny, reason: "broken"},
			expectErr: true,
		},
		{
			name:      "invalid: allowed true but effect default deny",
			decision:  Decision{allowed: true, effect: EffectDefaultDeny, reason: "broken"},
			expectErr: true,
		},
		{
			name:      "invalid: allowed false but effect allow",
			decision:  Decision{allowed: false, effect: EffectAllow, reason: "broken"},
			expectErr: true,
		},
		{
			name:      "invalid: allowed false but effect system bypass",
			decision:  Decision{allowed: false, effect: EffectSystemBypass, reason: "broken"},
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

func TestParsePolicyEffect(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  PolicyEffect
		expectErr bool
	}{
		{"valid permit", "permit", PolicyEffectPermit, false},
		{"valid forbid", "forbid", PolicyEffectForbid, false},
		{"invalid empty", "", PolicyEffect(""), true},
		{"invalid gibberish", "allow", PolicyEffect(""), true},
		{"invalid case sensitive", "Permit", PolicyEffect(""), true},
		{"invalid whitespace", " permit", PolicyEffect(""), true},
		{"invalid typo", "permits", PolicyEffect(""), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParsePolicyEffect(tt.input)
			if tt.expectErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "invalid policy effect")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestDecisionZeroValueDeniesAccess(t *testing.T) {
	// The zero value of Decision must deny access (fail-closed).
	// This is critical for safety: if code uses Decision{} as a fallback
	// or returns it from an error path, access must be denied.
	var d Decision
	assert.False(t, d.IsAllowed(), "zero-value Decision must deny access (fail-closed)")
	assert.Equal(t, EffectDefaultDeny, d.Effect(), "zero-value Decision effect must be default_deny")
	assert.Empty(t, d.Reason())
	assert.Empty(t, d.PolicyID())

	// Validate should pass because allowed=false is consistent with EffectDefaultDeny
	assert.NoError(t, d.Validate(), "zero-value Decision should be internally consistent")
}

func TestAccessRequestFields(t *testing.T) {
	req := AccessRequest{
		Subject:  "character:01ABC",
		Action:   "read",
		Resource: "location:01XYZ",
	}
	assert.Equal(t, "character:01ABC", req.Subject)
	assert.Equal(t, "read", req.Action)
	assert.Equal(t, "location:01XYZ", req.Resource)
}

func TestNewAccessRequestValid(t *testing.T) {
	req, err := NewAccessRequest("character:01ABC", "read", "location:01XYZ", nil)
	require.NoError(t, err)
	assert.Equal(t, "character:01ABC", req.Subject)
	assert.Equal(t, "read", req.Action)
	assert.Equal(t, "location:01XYZ", req.Resource)
}

func TestNewAccessRequest_EmptyFields(t *testing.T) {
	tests := []struct {
		name     string
		subject  string
		action   string
		resource string
		wantMsg  string
	}{
		{"empty subject", "", "read", "location:01XYZ", "subject"},
		{"empty action", "character:01ABC", "", "location:01XYZ", "action"},
		{"empty resource", "character:01ABC", "read", "", "resource"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewAccessRequest(tt.subject, tt.action, tt.resource, nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestNewAccessRequestAcceptsNilAttributes(t *testing.T) {
	req, err := NewAccessRequest("character:01ABC", "read", "location:01XYZ", nil)
	require.NoError(t, err)
	assert.Nil(t, req.Attributes)
}

func TestNewAccessRequestAcceptsCallerAttributes(t *testing.T) {
	attrs := map[string]any{
		"event_type":  "core-comm:whisper",
		"plugin_inst": "01INST",
	}
	req, err := NewAccessRequest("plugin:mod-filter", "decrypt", "dek:dm:01HABC", attrs)
	require.NoError(t, err)
	assert.Equal(t, "core-comm:whisper", req.Attributes["event_type"])
	assert.Equal(t, "01INST", req.Attributes["plugin_inst"])

	// Verify the stored map is independent of the caller's map: mutating the
	// original after construction must not affect the AccessRequest.
	attrs["event_type"] = "mutated"
	assert.Equal(t, "core-comm:whisper", req.Attributes["event_type"],
		"AccessRequest.Attributes must be a clone, not a reference to the caller's map")
}

func TestNewAccessRequestRejectsReservedNameKey(t *testing.T) {
	// "name" is reserved (resolver writes req.Action verb into bags.Action["name"]).
	// Caller-supplied "name" would silently overwrite the resolver value.
	attrs := map[string]any{"name": "something"}
	_, err := NewAccessRequest("character:01ABC", "read", "location:01XYZ", attrs)
	errutil.AssertErrorCode(t, err, "ACCESS_REQUEST_RESERVED_ATTRIBUTE")
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

func TestPolicyMatchFields(t *testing.T) {
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

func TestPolicySourceConstants(t *testing.T) {
	assert.Equal(t, PolicySource("seed"), PolicySourceSeed)
	assert.Equal(t, PolicySource("lock"), PolicySourceLock)
	assert.Equal(t, PolicySource("admin"), PolicySourceAdmin)
	assert.Equal(t, PolicySource("plugin"), PolicySourcePlugin)
}

func TestPropertyVisibilityConstants(t *testing.T) {
	assert.Equal(t, PropertyVisibility("public"), PropertyVisibilityPublic)
	assert.Equal(t, PropertyVisibility("private"), PropertyVisibilityPrivate)
	assert.Equal(t, PropertyVisibility("restricted"), PropertyVisibilityRestricted)
	assert.Equal(t, PropertyVisibility("system"), PropertyVisibilitySystem)
	assert.Equal(t, PropertyVisibility("admin"), PropertyVisibilityAdmin)
}

func TestEntityTypeConstants(t *testing.T) {
	assert.Equal(t, EntityType("character"), EntityTypeCharacter)
	assert.Equal(t, EntityType("location"), EntityTypeLocation)
	assert.Equal(t, EntityType("object"), EntityTypeObject)
}

func TestAttributeSchemaNewEmpty(t *testing.T) {
	schema := NewAttributeSchema()
	require.NotNil(t, schema)
	assert.Empty(t, schema.namespaces)
}

func TestDecision_IsInfraFailure(t *testing.T) {
	tests := []struct {
		name     string
		decision Decision
		expected bool
	}{
		{
			name:     "session invalid is infra failure (uses infra: prefix)",
			decision: NewDecision(EffectDefaultDeny, "session invalid", "infra:session-invalid"),
			expected: true,
		},
		{
			name:     "session store error is infra failure",
			decision: NewDecision(EffectDefaultDeny, "session store error", "infra:session-store-error"),
			expected: true,
		},
		{
			name:     "policy denial is not infra failure",
			decision: NewDecision(EffectDeny, "forbidden", "pol-123"),
			expected: false,
		},
		{
			name:     "default deny with no policy is not infra failure",
			decision: NewDecision(EffectDefaultDeny, "no match", ""),
			expected: false,
		},
		{
			name:     "empty policyID is not infra failure",
			decision: NewDecision(EffectDefaultDeny, "unknown", ""),
			expected: false,
		},
		{
			name:     "short policyID is not infra failure",
			decision: NewDecision(EffectDefaultDeny, "unknown", "infra"),
			expected: false,
		},
		{
			name:     "infra prefix with content is infra failure",
			decision: NewDecision(EffectDefaultDeny, "unknown", "infra:db-timeout"),
			expected: true,
		},
		{
			name:     "allow decision cannot be infra failure",
			decision: NewDecision(EffectAllow, "allowed", "infra:should-not-happen"),
			expected: true, // still detects prefix even if semantically wrong
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.decision.IsInfraFailure())
		})
	}
}

// TestIsReservedActionKeyRecognizesReservedKey verifies that the well-known
// reserved key "name" (written by the attribute resolver) is correctly
// identified as reserved.
func TestIsReservedActionKeyRecognizesReservedKey(t *testing.T) {
	t.Parallel()
	assert.True(t, IsReservedActionKey("name"), `"name" must be a reserved action key`)
}

// TestIsReservedActionKeyRejectsUnknownKey verifies that an arbitrary key
// that is not in the reserved set returns false, preventing false positives.
func TestIsReservedActionKeyRejectsUnknownKey(t *testing.T) {
	t.Parallel()
	assert.False(t, IsReservedActionKey("anything-else"), `non-reserved keys must not be reported as reserved`)
	assert.False(t, IsReservedActionKey(""), `empty string must not be reported as reserved`)
	assert.False(t, IsReservedActionKey("Name"), `key lookup must be case-sensitive`)
}

// TestNewAccessRequestRejectsReservedAttributeKeyViaIsReservedActionKey verifies
// that NewAccessRequest delegates its reserved-key check through IsReservedActionKey,
// ensuring the constructor and the accessor share the same invariant set.
func TestNewAccessRequestRejectsReservedAttributeKeyViaIsReservedActionKey(t *testing.T) {
	t.Parallel()
	// "name" is the only current member of reservedActionKeys.
	_, err := NewAccessRequest("subject", "read", "resource", map[string]any{"name": "injected"})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "ACCESS_REQUEST_RESERVED_ATTRIBUTE")
}
