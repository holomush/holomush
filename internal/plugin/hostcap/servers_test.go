// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostcap

import (
	"math"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

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

func TestCoreEventToProtoConvertsAllFields(t *testing.T) {
	eventID := ulid.Make()
	ts := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	e := core.Event{
		ID:        eventID,
		Stream:    "scene:abc:ic",
		Type:      "say",
		Timestamp: ts,
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char-1"},
		Payload:   []byte(`{"text":"hello"}`),
	}

	pe := coreEventToProto(e)
	assert.Equal(t, eventID.String(), pe.GetId())
	assert.Equal(t, "scene:abc:ic", pe.GetStream())
	assert.Equal(t, "say", pe.GetType())
	assert.Equal(t, ts.UnixMilli(), pe.GetTimestamp())
	assert.Equal(t, "character", pe.GetActorKind())
	assert.Equal(t, "char-1", pe.GetActorId())
	assert.Equal(t, `{"text":"hello"}`, pe.GetPayload())
}
