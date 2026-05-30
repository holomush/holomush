// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

// Unit tests for the proto→SDK conversion helpers that propagate
// plugin-emitted audit hints from the gRPC CommandResponse to the
// SDK CommandResponse the dispatcher sees. The integration test in
// test/integration/audit/ uses a scriptedDeliverer that constructs
// SDK CommandResponse values directly and bypasses these conversion
// helpers entirely, so the paths these tests exercise had no
// coverage before this file landed.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

func TestProtoCommandResponseToSDKReturnsEmptyResponseForNilInput(t *testing.T) {
	got := protoCommandResponseToSDK(nil)
	require.NotNil(t, got, "nil proto response must yield a non-nil SDK response")
	assert.Equal(t, pluginsdk.CommandOK, got.Status)
	assert.Empty(t, got.Output)
	assert.Empty(t, got.Events)
	assert.Empty(t, got.AuditHints)
}

func TestProtoCommandResponseToSDKCopiesStatusAndEvents(t *testing.T) {
	in := &pluginv1.CommandResponse{
		Status: pluginv1.CommandStatus_COMMAND_STATUS_ERROR,
		Output: "denied",
		Events: []*pluginv1.EmitEvent{
			{Stream: "character:01ABC", Type: "command_response", Payload: `{"text":"hi"}`, Sensitive: true},
		},
	}

	got := protoCommandResponseToSDK(in)

	assert.Equal(t, pluginsdk.CommandError, got.Status)
	assert.Equal(t, "denied", got.Output)
	require.Len(t, got.Events, 1)
	assert.Equal(t, "character:01ABC", got.Events[0].Stream)
	assert.Equal(t, pluginsdk.HostEventTypeCommandResponse, got.Events[0].Type)
	assert.Equal(t, `{"text":"hi"}`, got.Events[0].Payload)
	// Sensitive MUST survive the command return-value receive path; before
	// holomush-av954 this site dropped it, silently downgrading a binary
	// command handler's sensitive emit to plaintext at the fence.
	assert.True(t, got.Events[0].Sensitive,
		"protoCommandResponseToSDK MUST carry EmitEvent.Sensitive (holomush-av954)")
}

func TestProtoCommandResponseToSDKPropagatesAuditHintsWithEnumConversion(t *testing.T) {
	// This test guards against the latent bug fixed during the PR #203
	// autofix: protoCommandResponseToSDK previously did NOT propagate
	// audit_hints from the proto to the SDK response, silently dropping
	// real binary plugin audit emits before reaching the dispatcher.
	in := &pluginv1.CommandResponse{
		Status: pluginv1.CommandStatus_COMMAND_STATUS_ERROR,
		AuditHints: []*pluginv1.AuditDecisionHint{
			{
				Id:              "not_member",
				Name:            "channels: not a member",
				Message:         "player not in channel members",
				Effect:          pluginv1.AuditEffect_AUDIT_EFFECT_DENY,
				ActionQualifier: "speak",
				Resource:        "channel:01XYZ",
				Attributes:      map[string]string{"channel.type": "public"},
			},
			{
				Id:      "speak_ok",
				Message: "message delivered",
				Effect:  pluginv1.AuditEffect_AUDIT_EFFECT_ALLOW,
			},
			{
				Id:      "malformed",
				Message: "unspecified effect will be dropped by the dispatcher",
				Effect:  pluginv1.AuditEffect_AUDIT_EFFECT_UNSPECIFIED,
			},
		},
	}

	got := protoCommandResponseToSDK(in)
	require.Len(t, got.AuditHints, 3,
		"audit_hints must be propagated from proto to SDK")

	// Hint 1: deny with every field set.
	h0 := got.AuditHints[0]
	assert.Equal(t, "not_member", h0.ID)
	assert.Equal(t, "channels: not a member", h0.Name)
	assert.Equal(t, "player not in channel members", h0.Message)
	assert.Equal(t, pluginsdk.AuditEffectDeny, h0.Effect)
	assert.Equal(t, "speak", h0.ActionQualifier)
	assert.Equal(t, "channel:01XYZ", h0.Resource)
	assert.Equal(t, "public", h0.Attributes["channel.type"])

	// Hint 2: allow with sparse fields.
	h1 := got.AuditHints[1]
	assert.Equal(t, "speak_ok", h1.ID)
	assert.Equal(t, pluginsdk.AuditEffectAllow, h1.Effect)

	// Hint 3: unspecified collapses to empty string so the dispatcher's
	// extractAuditHints unknown-effect path drops it with a warning.
	h2 := got.AuditHints[2]
	assert.Equal(t, "malformed", h2.ID)
	assert.Equal(t, pluginsdk.AuditEffect(""), h2.Effect,
		"UNSPECIFIED proto enum must map to empty SDK effect")
}

func TestProtoCommandResponseToSDKReturnsEmptyHintSliceForZeroHints(t *testing.T) {
	in := &pluginv1.CommandResponse{Status: pluginv1.CommandStatus_COMMAND_STATUS_OK}
	got := protoCommandResponseToSDK(in)
	assert.NotNil(t, got.AuditHints)
	assert.Empty(t, got.AuditHints)
}

func TestProtoAuditEffectToSDKMapsAllKnownValues(t *testing.T) {
	tests := []struct {
		name string
		in   pluginv1.AuditEffect
		want pluginsdk.AuditEffect
	}{
		{
			"deny proto enum maps to SDK AuditEffectDeny",
			pluginv1.AuditEffect_AUDIT_EFFECT_DENY,
			pluginsdk.AuditEffectDeny,
		},
		{
			"allow proto enum maps to SDK AuditEffectAllow",
			pluginv1.AuditEffect_AUDIT_EFFECT_ALLOW,
			pluginsdk.AuditEffectAllow,
		},
		{
			"unspecified proto enum maps to empty SDK effect",
			pluginv1.AuditEffect_AUDIT_EFFECT_UNSPECIFIED,
			pluginsdk.AuditEffect(""),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := protoAuditEffectToSDK(tc.in)
			assert.Equal(t, tc.want, got)
		})
	}
}
