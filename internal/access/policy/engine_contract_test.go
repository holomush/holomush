// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/pkg/errutil"
)

// TestContract_MalformedSubjectPrefix verifies that subjects without the
// expected "type:id" format are rejected with INVALID_ENTITY_REF error.
func TestContract_MalformedSubjectPrefix(t *testing.T) {
	tests := []struct {
		name    string
		subject string
	}{
		{"no colon separator", "invalid-no-colon"},
		{"bare word", "character"},
		{"trailing colon empty id", "character:"},
		{"leading colon empty type", ":01ABC"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, _ := createTestEngine(t, &mockSessionResolver{})

			req := types.AccessRequest{
				Subject:  tt.subject,
				Action:   "read",
				Resource: "location:01XYZ",
			}

			_, err := engine.Evaluate(context.Background(), req)
			require.Error(t, err, "malformed subject %q should return error", tt.subject)
			errutil.AssertErrorCode(t, err, "INVALID_ENTITY_REF")
		})
	}
}

// TestContract_EmptyFields verifies that requests with empty strings in
// subject, action, or resource fields are rejected with clear error codes.
func TestContract_EmptyFields(t *testing.T) {
	tests := []struct {
		name     string
		req      types.AccessRequest
		wantCode string
	}{
		{
			name:     "empty subject",
			req:      types.AccessRequest{Subject: "", Action: "read", Resource: "location:01XYZ"},
			wantCode: "INVALID_REQUEST",
		},
		{
			name:     "empty action",
			req:      types.AccessRequest{Subject: "character:01ABC", Action: "", Resource: "location:01XYZ"},
			wantCode: "INVALID_REQUEST",
		},
		{
			name:     "empty resource",
			req:      types.AccessRequest{Subject: "character:01ABC", Action: "read", Resource: ""},
			wantCode: "INVALID_REQUEST",
		},
		{
			name:     "empty subject and action",
			req:      types.AccessRequest{Subject: "", Action: "", Resource: "location:01XYZ"},
			wantCode: "INVALID_REQUEST",
		},
		{
			name:     "all empty strings",
			req:      types.AccessRequest{Subject: "", Action: "", Resource: ""},
			wantCode: "INVALID_REQUEST",
		},
		{
			name:     "whitespace-only subject",
			req:      types.AccessRequest{Subject: "   ", Action: "read", Resource: "location:01XYZ"},
			wantCode: "INVALID_REQUEST",
		},
		{
			name:     "whitespace-only action",
			req:      types.AccessRequest{Subject: "character:01ABC", Action: "  ", Resource: "location:01XYZ"},
			wantCode: "INVALID_REQUEST",
		},
		{
			name:     "whitespace-only resource",
			req:      types.AccessRequest{Subject: "character:01ABC", Action: "read", Resource: "  "},
			wantCode: "INVALID_REQUEST",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, _ := createTestEngine(t, &mockSessionResolver{})

			_, err := engine.Evaluate(context.Background(), tt.req)
			require.Error(t, err, "empty field should return error")
			errutil.AssertErrorCode(t, err, tt.wantCode)
		})
	}
}

// TestContract_ZeroValueAccessRequest verifies that a zero-value
// AccessRequest{} is rejected with INVALID_REQUEST error code.
func TestContract_ZeroValueAccessRequest(t *testing.T) {
	engine, _ := createTestEngine(t, &mockSessionResolver{})

	req := types.AccessRequest{} // all zero values

	_, err := engine.Evaluate(context.Background(), req)
	require.Error(t, err, "zero-value AccessRequest should return error")
	errutil.AssertErrorCode(t, err, "INVALID_REQUEST")
}

// TestContract_ContextCancellation verifies that a cancelled context
// returns context.Canceled error.
func TestContract_ContextCancellation(t *testing.T) {
	engine, _ := createTestEngine(t, &mockSessionResolver{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "read",
		Resource: "location:01XYZ",
	}

	_, err := engine.Evaluate(ctx, req)
	require.Error(t, err, "cancelled context should return error")
	assert.ErrorIs(t, err, context.Canceled)
}

// TestContract_EmptyPolicyCache verifies that when no policies are loaded,
// evaluation returns EffectDefaultDeny without error.
func TestContract_EmptyPolicyCache(t *testing.T) {
	engine, _ := createTestEngine(t, &mockSessionResolver{})

	req := types.AccessRequest{
		Subject:  "character:01ABC",
		Action:   "read",
		Resource: "location:01XYZ",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, types.EffectDefaultDeny, decision.Effect)
	assert.False(t, decision.IsAllowed())
	assert.Equal(t, "no applicable policies", decision.Reason)
	assert.Empty(t, decision.PolicyID)
}

// TestContract_EmptyPolicyCache_AllSubjectTypes verifies default deny for
// all valid subject type prefixes when no policies exist.
func TestContract_EmptyPolicyCache_AllSubjectTypes(t *testing.T) {
	tests := []struct {
		name    string
		subject string
	}{
		{"character subject", "character:01ABC"},
		{"plugin subject", "plugin:echo-bot"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, _ := createTestEngine(t, &mockSessionResolver{})

			req := types.AccessRequest{
				Subject:  tt.subject,
				Action:   "read",
				Resource: "location:01XYZ",
			}

			decision, err := engine.Evaluate(context.Background(), req)
			require.NoError(t, err)

			assert.Equal(t, types.EffectDefaultDeny, decision.Effect)
			assert.False(t, decision.IsAllowed())
		})
	}
}

// TestContract_ErrorCodePreservation verifies that session resolution errors
// with oops codes are preserved through the engine's error handling.
func TestContract_ErrorCodePreservation(t *testing.T) {
	tests := []struct {
		name         string
		sessionErr   error
		wantReason   string
		wantPolicyID string
	}{
		{
			name:         "SESSION_INVALID code preserved",
			sessionErr:   oops.Code("SESSION_INVALID").Errorf("session expired"),
			wantReason:   "session invalid",
			wantPolicyID: "infra:session-invalid",
		},
		{
			name:         "SESSION_NOT_FOUND code preserved",
			sessionErr:   oops.Code("SESSION_NOT_FOUND").Errorf("unknown session"),
			wantReason:   "session store error",
			wantPolicyID: "infra:session-store-error",
		},
		{
			name:         "generic error without code",
			sessionErr:   oops.Errorf("database timeout"),
			wantReason:   "session store error",
			wantPolicyID: "infra:session-store-error",
		},
		{
			name:         "wrapped oops error preserves code",
			sessionErr:   oops.Code("SESSION_INVALID").Wrapf(oops.Errorf("inner"), "outer"),
			wantReason:   "session invalid",
			wantPolicyID: "infra:session-invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &mockSessionResolver{
				resolveFunc: func(_ context.Context, _ string) (string, error) {
					return "", tt.sessionErr
				},
			}
			engine, _ := createTestEngine(t, resolver)

			req := types.AccessRequest{
				Subject:  "session:test-session",
				Action:   "read",
				Resource: "location:01XYZ",
			}

			decision, err := engine.Evaluate(context.Background(), req)
			require.NoError(t, err, "session errors should not propagate as engine errors")

			assert.Equal(t, types.EffectDefaultDeny, decision.Effect)
			assert.False(t, decision.IsAllowed())
			assert.Equal(t, tt.wantReason, decision.Reason)
			assert.Equal(t, tt.wantPolicyID, decision.PolicyID)
		})
	}
}

// TestContract_SystemBypass_SkipsValidation verifies that "system" subject
// bypasses input validation and returns SystemBypass regardless of other fields.
func TestContract_SystemBypass_SkipsValidation(t *testing.T) {
	engine, _ := createTestEngine(t, &mockSessionResolver{})

	req := types.AccessRequest{
		Subject:  "system",
		Action:   "", // would normally fail validation
		Resource: "", // would normally fail validation
	}

	decision, err := engine.Evaluate(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, types.EffectSystemBypass, decision.Effect)
	assert.True(t, decision.IsAllowed())
}

// TestContract_ValidSubjectFormats verifies that well-formed subjects do
// NOT return INVALID_ENTITY_REF errors.
func TestContract_ValidSubjectFormats(t *testing.T) {
	tests := []struct {
		name    string
		subject string
	}{
		{"character ref", "character:01ABC"},
		{"plugin ref", "plugin:echo-bot"},
		{"system literal", "system"},
		{"session ref", "session:web-123"},
		{"multiple colons in id", "character:01ABC:extra"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &mockSessionResolver{
				resolveFunc: func(_ context.Context, _ string) (string, error) {
					return "01ABC", nil
				},
			}
			engine, _ := createTestEngine(t, resolver)

			req := types.AccessRequest{
				Subject:  tt.subject,
				Action:   "read",
				Resource: "location:01XYZ",
			}

			_, err := engine.Evaluate(context.Background(), req)
			require.NoError(t, err, "valid subject %q should not error", tt.subject)
		})
	}
}
