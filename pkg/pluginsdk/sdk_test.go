// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk_test

import (
	"context"
	"testing"

	"github.com/holomush/holomush/pkg/pluginsdk"
)

func TestHandler_Interface(_ *testing.T) {
	// Verify Handler interface is properly defined
	var _ pluginsdk.Handler = (*testHandler)(nil)
}

type testHandler struct{}

func (h *testHandler) HandleEvent(_ context.Context, event pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	return []pluginsdk.EmitEvent{
		{
			Stream:  event.Stream,
			Type:    event.Type,
			Payload: event.Payload,
		},
	}, nil
}

func TestServeConfig_HandlerRequired(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Serve should panic with nil Handler")
		}
	}()

	pluginsdk.Serve(&pluginsdk.ServeConfig{Handler: nil})
}

func TestServeConfig_ConfigRequired(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Serve should panic with nil config")
		}
	}()

	pluginsdk.Serve(nil)
}

func TestHandshakeConfig(t *testing.T) {
	if pluginsdk.HandshakeConfig.ProtocolVersion != 1 {
		t.Error("HandshakeConfig protocol version should be 1")
	}
	if pluginsdk.HandshakeConfig.MagicCookieKey != "HOLOMUSH_PLUGIN" {
		t.Error("HandshakeConfig magic cookie key mismatch")
	}
	if pluginsdk.HandshakeConfig.MagicCookieValue != "holomush-v1" {
		t.Error("HandshakeConfig magic cookie value mismatch")
	}
}

func TestEvent_Fields(t *testing.T) {
	event := pluginsdk.Event{
		ID:        "test-id",
		Stream:    "room:test",
		Type:      "say",
		Timestamp: 1234567890,
		ActorKind: "character",
		ActorID:   "char-123",
		Payload:   `{"message":"hello"}`,
	}

	if event.ID != "test-id" {
		t.Errorf("Event.ID = %q, want %q", event.ID, "test-id")
	}
	if event.Stream != "room:test" {
		t.Errorf("Event.Stream = %q, want %q", event.Stream, "room:test")
	}
	if event.Type != "say" {
		t.Errorf("Event.Type = %q, want %q", event.Type, "say")
	}
	if event.Timestamp != 1234567890 {
		t.Errorf("Event.Timestamp = %d, want %d", event.Timestamp, 1234567890)
	}
	if event.ActorKind != "character" {
		t.Errorf("Event.ActorKind = %q, want %q", event.ActorKind, "character")
	}
	if event.ActorID != "char-123" {
		t.Errorf("Event.ActorID = %q, want %q", event.ActorID, "char-123")
	}
	if event.Payload != `{"message":"hello"}` {
		t.Errorf("Event.Payload = %q, want %q", event.Payload, `{"message":"hello"}`)
	}
}

func TestEmitEvent_Fields(t *testing.T) {
	emit := pluginsdk.EmitEvent{
		Stream:  "room:test",
		Type:    "say",
		Payload: `{"message":"response"}`,
	}

	if emit.Stream != "room:test" {
		t.Errorf("EmitEvent.Stream = %q, want %q", emit.Stream, "room:test")
	}
	if emit.Type != "say" {
		t.Errorf("EmitEvent.Type = %q, want %q", emit.Type, "say")
	}
	if emit.Payload != `{"message":"response"}` {
		t.Errorf("EmitEvent.Payload = %q, want %q", emit.Payload, `{"message":"response"}`)
	}
}
