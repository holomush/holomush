// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

// Internal tests for pluginServerAdapter — the gRPC server adapter
// that wraps a user Handler and handles audit hint harvesting +
// proto serialization. These live in package pluginsdk (not
// pluginsdk_test) so they can construct pluginServerAdapter directly
// via its unexported fields.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// captureCmdHandler is a test CommandHandler that records the context
// and request it received and returns a pre-canned response (optionally
// after emitting audit hints via the Audit(ctx) recorder).
type captureCmdHandler struct {
	capturedCtx context.Context //nolint:containedctx // test capture
	capturedReq CommandRequest
	emitHints   []AuditHint
	resp        *CommandResponse
	err         error
}

func (h *captureCmdHandler) HandleEvent(_ context.Context, _ Event) ([]EmitEvent, error) {
	return nil, nil
}

func (h *captureCmdHandler) HandleCommand(ctx context.Context, req CommandRequest) (*CommandResponse, error) {
	h.capturedCtx = ctx
	h.capturedReq = req

	// Emit any configured hints via the context-scoped recorder so the
	// adapter's harvest path is exercised. This mirrors how a real
	// plugin handler would use Audit(ctx).Deny / Allow.
	for _, h := range h.emitHints {
		switch h.Effect {
		case AuditEffectDeny:
			Audit(ctx).Deny(h.ID, h.Message, AuditAttrs(h.Attributes))
		case AuditEffectAllow:
			Audit(ctx).Allow(h.ID, h.Message, AuditAttrs(h.Attributes))
		}
	}
	return h.resp, h.err
}

// newTestAdapter constructs a pluginServerAdapter with the given
// CommandHandler wired in as both the Handler and CommandHandler.
func newTestAdapter(h *captureCmdHandler) *pluginServerAdapter {
	return &pluginServerAdapter{
		handler:    h,
		cmdHandler: h,
	}
}

func TestPluginServerAdapterHandleCommandHarvestsDenyHintsIntoProtoResponse(t *testing.T) {
	handler := &captureCmdHandler{
		emitHints: []AuditHint{
			{
				ID:              "not_member",
				Message:         "player not in channel members",
				Effect:          AuditEffectDeny,
				Attributes:      map[string]string{"channel.type": "public"},
				ActionQualifier: "",
				Resource:        "",
			},
		},
		resp: &CommandResponse{
			Status: CommandError,
			Output: "denied",
		},
	}
	adapter := newTestAdapter(handler)

	req := &pluginv1.HandleCommandRequest{
		Command: &pluginv1.CommandRequest{
			Command:       "channel",
			Args:          "speak hello",
			CharacterId:   "character:01ABC",
			CharacterName: "tester",
			LocationId:    "location:01XYZ",
			SessionId:     "01SESS",
			PlayerId:      "01PLAY",
			RawInput:      "channel speak hello",
		},
	}

	got, err := adapter.HandleCommand(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.Response)

	assert.Equal(t, pluginv1.CommandStatus_COMMAND_STATUS_ERROR, got.Response.Status)
	assert.Equal(t, "denied", got.Response.Output)

	require.Len(t, got.Response.AuditHints, 1)
	hint := got.Response.AuditHints[0]
	assert.Equal(t, "not_member", hint.Id)
	assert.Equal(t, "player not in channel members", hint.Message)
	assert.Equal(t, pluginv1.AuditEffect_AUDIT_EFFECT_DENY, hint.Effect,
		"SDK AuditEffectDeny must serialize to proto AUDIT_EFFECT_DENY")
	assert.Equal(t, "public", hint.Attributes["channel.type"])
}

func TestPluginServerAdapterHandleCommandHarvestsAllowHintsIntoProtoResponse(t *testing.T) {
	handler := &captureCmdHandler{
		emitHints: []AuditHint{
			{
				ID:      "speak_ok",
				Message: "message delivered",
				Effect:  AuditEffectAllow,
			},
		},
		resp: &CommandResponse{Status: CommandOK, Output: "ok"},
	}
	adapter := newTestAdapter(handler)

	got, err := adapter.HandleCommand(context.Background(), &pluginv1.HandleCommandRequest{
		Command: &pluginv1.CommandRequest{
			Command:       "channel",
			CharacterId:   "character:01ABC",
			CharacterName: "tester",
			LocationId:    "location:01XYZ",
			SessionId:     "01SESS",
			PlayerId:      "01PLAY",
		},
	})
	require.NoError(t, err)

	require.Len(t, got.Response.AuditHints, 1)
	assert.Equal(t, pluginv1.AuditEffect_AUDIT_EFFECT_ALLOW, got.Response.AuditHints[0].Effect,
		"SDK AuditEffectAllow must serialize to proto AUDIT_EFFECT_ALLOW")
}

func TestPluginServerAdapterHandleCommandMergesContextHintsWithResponseHints(t *testing.T) {
	// A handler that both emits via Audit(ctx) AND attaches hints directly
	// to the response struct — the adapter should merge both paths.
	handler := &captureCmdHandler{
		emitHints: []AuditHint{
			{ID: "ctx_hint", Effect: AuditEffectDeny, Message: "from ctx"},
		},
		resp: &CommandResponse{
			Status: CommandOK,
			AuditHints: []AuditHint{
				{ID: "direct_hint", Effect: AuditEffectAllow, Message: "from resp"},
			},
		},
	}
	adapter := newTestAdapter(handler)

	got, err := adapter.HandleCommand(context.Background(), &pluginv1.HandleCommandRequest{
		Command: &pluginv1.CommandRequest{
			Command:       "channel",
			CharacterId:   "character:01ABC",
			CharacterName: "tester",
			LocationId:    "location:01XYZ",
			SessionId:     "01SESS",
			PlayerId:      "01PLAY",
		},
	})
	require.NoError(t, err)
	require.Len(t, got.Response.AuditHints, 2)

	ids := []string{got.Response.AuditHints[0].Id, got.Response.AuditHints[1].Id}
	assert.Contains(t, ids, "ctx_hint", "context-recorded hint must appear in proto response")
	assert.Contains(t, ids, "direct_hint", "response-struct hint must appear in proto response")
}

func TestPluginServerAdapterHandleCommandReturnsEmptyResponseForNilHandlerReturn(t *testing.T) {
	handler := &captureCmdHandler{resp: nil}
	adapter := newTestAdapter(handler)

	got, err := adapter.HandleCommand(context.Background(), &pluginv1.HandleCommandRequest{
		Command: &pluginv1.CommandRequest{
			Command:       "channel",
			CharacterId:   "character:01ABC",
			CharacterName: "tester",
			LocationId:    "location:01XYZ",
			SessionId:     "01SESS",
			PlayerId:      "01PLAY",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.Response)
	assert.Empty(t, got.Response.AuditHints)
}

func TestPluginServerAdapterHandleCommandReturnsEmptyResponseWhenCmdHandlerIsNil(t *testing.T) {
	// The adapter's cmdHandler field can legitimately be nil for plugins
	// that only implement Handler (event-only). HandleCommand should
	// return an empty response in that case, not nil-deref.
	adapter := &pluginServerAdapter{handler: &captureCmdHandler{}, cmdHandler: nil}

	got, err := adapter.HandleCommand(context.Background(), &pluginv1.HandleCommandRequest{
		Command: &pluginv1.CommandRequest{Command: "noop"},
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.Response)
}

func TestSdkAuditEffectToProtoMapsAllKnownValues(t *testing.T) {
	tests := []struct {
		name    string
		in      AuditEffect
		want    pluginv1.AuditEffect
		comment string
	}{
		{"deny maps to AUDIT_EFFECT_DENY", AuditEffectDeny, pluginv1.AuditEffect_AUDIT_EFFECT_DENY, ""},
		{"allow maps to AUDIT_EFFECT_ALLOW", AuditEffectAllow, pluginv1.AuditEffect_AUDIT_EFFECT_ALLOW, ""},
		{"empty maps to UNSPECIFIED", AuditEffect(""), pluginv1.AuditEffect_AUDIT_EFFECT_UNSPECIFIED, ""},
		{"unknown string maps to UNSPECIFIED", AuditEffect("mystery"), pluginv1.AuditEffect_AUDIT_EFFECT_UNSPECIFIED, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sdkAuditEffectToProto(tc.in)
			assert.Equal(t, tc.want, got)
		})
	}
}
