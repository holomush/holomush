// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/eventbus"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

// TestEmitServerRegisterEmitTypeReturnsUnimplemented pins the staged-endpoint
// contract (holomush-eykuh.8 item 3): the EmitService.RegisterEmitType RPC has no
// host-side mutation wired to it — binary plugins declare their emit-type set at
// Load via InitResponse.RegisteredEmitTypes, and the Lua runtime registers through
// the in-process holomush.register_emit_type hostfunc. Rather than a misleading
// silent-success, the RPC MUST return codes.Unimplemented so either runtime's
// generated bridge surfaces a clear "not wired yet" contract.
func TestEmitServerRegisterEmitTypeReturnsUnimplemented(t *testing.T) {
	srv := NewEmitServer(NewBase(nil, "test-plugin"))

	resp, err := srv.RegisterEmitType(context.Background(), &hostv1.RegisterEmitTypeRequest{
		EventType: "alpha",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}

// These tests exercise the proto↔domain conversion helpers that moved here with
// the server bodies (holomush-eykuh.2). They were previously in
// internal/plugin/goplugin/host_service_test.go; the assertions are unchanged —
// only the package home moved, tracking the helpers they verify.

func TestClampCountToInt32HandlesBounds(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int32
	}{
		{"negative clamps to zero", -5, 0},
		{"zero passes through", 0, 0},
		{"small positive passes through", 42, 42},
		{"max int32 passes through", math.MaxInt32, math.MaxInt32},
		{"overflow clamps to MaxInt32", math.MaxInt32 + 1, math.MaxInt32},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, clampCountToInt32(tt.in))
		})
	}
}

func TestProtoToFocusKeyReturnsErrorForNilKey(t *testing.T) {
	_, err := protoToFocusKey(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "focus key is required")
}

func TestProtoToFocusKeyReturnsErrorForInvalidULID(t *testing.T) {
	_, err := protoToFocusKey(&hostv1.FocusKey{
		Kind:     hostv1.FocusKind_FOCUS_KIND_SCENE,
		TargetId: "not-a-ulid",
	})
	require.Error(t, err)
}

func TestProtoToFocusKindReturnsErrorForUnspecified(t *testing.T) {
	_, err := protoToFocusKind(hostv1.FocusKind_FOCUS_KIND_UNSPECIFIED)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported focus kind")
}

func TestEventbusEventToProtoConvertsAllFields(t *testing.T) {
	eventID := ulid.Make()
	actorID := ulid.Make()
	ts := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	e := eventbus.Event{
		ID:        eventID,
		Subject:   "scene:abc:ic",
		Type:      "say",
		Timestamp: ts,
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindCharacter, ID: actorID},
		Payload:   []byte(`{"text":"hello"}`),
	}

	pe := eventbusEventToProto(e)
	assert.Equal(t, eventID.String(), pe.GetId())
	assert.Equal(t, "scene:abc:ic", pe.GetStream())
	assert.Equal(t, "say", pe.GetType())
	assert.Equal(t, ts.UnixMilli(), pe.GetTimestamp())
	assert.Equal(t, "character", pe.GetActorKind())
	assert.Equal(t, actorID.String(), pe.GetActorId())
	assert.Equal(t, `{"text":"hello"}`, pe.GetPayload())
}

// TestEventbusEventToProtoZeroActorIDRendersAsEmptyString locks in the
// zero-ULID → "" mapping (actorIDString) so a system/anonymous actor renders
// as the empty string plugins already observe, not the 26-char all-zeros
// ULID text (cross-AI round 7, MEDIUM).
func TestEventbusEventToProtoZeroActorIDRendersAsEmptyString(t *testing.T) {
	e := eventbus.Event{
		ID:      ulid.Make(),
		Subject: "scene:abc:ic",
		Type:    "say",
		Actor:   eventbus.Actor{Kind: eventbus.ActorKindSystem},
		Payload: []byte(`{}`),
	}

	pe := eventbusEventToProto(e)
	assert.Empty(t, pe.GetActorId(), "expected zero actor ULID to render as empty string")
}
