// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"errors"
	"testing"

	pluginv1 "github.com/holomush/holomush/internal/proto/holomush/plugin/v1"
	"google.golang.org/grpc"
)

func TestGRPCPlugin_GRPCServer_NilHandler(t *testing.T) {
	p := &grpcPlugin{handler: nil}
	// Use a real grpc.Server - it's cheap to create for testing
	s := grpc.NewServer()
	defer s.Stop()

	err := p.GRPCServer(nil, s)
	if err == nil {
		t.Error("expected error when handler is nil")
	}
	if err.Error() != "pluginsdk: handler is nil" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGRPCPlugin_GRPCServer_RegistersService(t *testing.T) {
	handler := &testHandler{}
	p := &grpcPlugin{handler: handler}
	s := grpc.NewServer()
	defer s.Stop()

	err := p.GRPCServer(nil, s)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify service was registered by checking GetServiceInfo
	info := s.GetServiceInfo()
	if _, ok := info["holomush.plugin.v1.Plugin"]; !ok {
		t.Error("expected Plugin service to be registered")
	}
}

func TestGRPCPlugin_GRPCClient_ReturnsPluginClient(t *testing.T) {
	p := &grpcPlugin{handler: nil}

	// We can't create a real grpc.ClientConn in tests without a server,
	// but we can verify the method doesn't panic with nil connection
	// The actual implementation calls pluginv1.NewPluginClient which handles nil.
	client, err := p.GRPCClient(context.Background(), nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	// The client will be non-nil even with nil connection because
	// NewPluginClient just wraps the connection
	if client == nil {
		t.Error("expected non-nil client")
	}
}

type testHandler struct {
	response []EmitEvent
	err      error
}

func (h *testHandler) HandleEvent(_ context.Context, _ Event) ([]EmitEvent, error) {
	if h.err != nil {
		return nil, h.err
	}
	return h.response, nil
}

func TestPluginServerAdapter_HandleEvent_Success(t *testing.T) {
	handler := &testHandler{
		response: []EmitEvent{
			{Stream: "room:123", Type: "say", Payload: `{"text":"hello"}`},
		},
	}
	adapter := &pluginServerAdapter{handler: handler}

	req := &pluginv1.HandleEventRequest{
		Event: &pluginv1.Event{
			Id:        "evt-123",
			Stream:    "room:456",
			Type:      "say",
			Timestamp: 1234567890,
			ActorKind: "character",
			ActorId:   "char-789",
			Payload:   `{"text":"input"}`,
		},
	}

	resp, err := adapter.HandleEvent(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.GetEmitEvents()) != 1 {
		t.Fatalf("expected 1 emit event, got %d", len(resp.GetEmitEvents()))
	}

	emit := resp.GetEmitEvents()[0]
	if emit.GetStream() != "room:123" {
		t.Errorf("emit.Stream = %q, want %q", emit.GetStream(), "room:123")
	}
	if emit.GetType() != "say" {
		t.Errorf("emit.Type = %q, want %q", emit.GetType(), "say")
	}
	if emit.GetPayload() != `{"text":"hello"}` {
		t.Errorf("emit.Payload = %q, want %q", emit.GetPayload(), `{"text":"hello"}`)
	}
}

func TestPluginServerAdapter_HandleEvent_HandlerError(t *testing.T) {
	handler := &testHandler{
		err: errors.New("handler failed"),
	}
	adapter := &pluginServerAdapter{handler: handler}

	req := &pluginv1.HandleEventRequest{
		Event: &pluginv1.Event{
			Id: "evt-123",
		},
	}

	_, err := adapter.HandleEvent(context.Background(), req)
	if err == nil {
		t.Error("expected error when handler fails")
	}
	if err.Error() != "handler error: handler failed" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestPluginServerAdapter_HandleEvent_EmptyEmits(t *testing.T) {
	handler := &testHandler{
		response: []EmitEvent{},
	}
	adapter := &pluginServerAdapter{handler: handler}

	req := &pluginv1.HandleEventRequest{
		Event: &pluginv1.Event{
			Id: "evt-123",
		},
	}

	resp, err := adapter.HandleEvent(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.GetEmitEvents()) != 0 {
		t.Errorf("expected 0 emit events, got %d", len(resp.GetEmitEvents()))
	}
}

func TestPluginServerAdapter_HandleEvent_MultipleEmits(t *testing.T) {
	handler := &testHandler{
		response: []EmitEvent{
			{Stream: "room:1", Type: "say", Payload: `{"n":1}`},
			{Stream: "room:2", Type: "pose", Payload: `{"n":2}`},
			{Stream: "room:3", Type: "arrive", Payload: `{"n":3}`},
		},
	}
	adapter := &pluginServerAdapter{handler: handler}

	req := &pluginv1.HandleEventRequest{
		Event: &pluginv1.Event{
			Id: "evt-123",
		},
	}

	resp, err := adapter.HandleEvent(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.GetEmitEvents()) != 3 {
		t.Fatalf("expected 3 emit events, got %d", len(resp.GetEmitEvents()))
	}

	for i, emit := range resp.GetEmitEvents() {
		expectedStream := "room:" + []string{"1", "2", "3"}[i]
		if emit.GetStream() != expectedStream {
			t.Errorf("emit[%d].Stream = %q, want %q", i, emit.GetStream(), expectedStream)
		}
	}
}

func TestPluginServerAdapter_HandleEvent_NilEvent(t *testing.T) {
	handler := &testHandler{response: nil}
	adapter := &pluginServerAdapter{handler: handler}

	req := &pluginv1.HandleEventRequest{
		Event: nil, // nil event
	}

	resp, err := adapter.HandleEvent(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should handle gracefully with empty response
	if resp == nil {
		t.Error("expected non-nil response")
	}
}

func TestPluginServerAdapter_HandleEvent_EventConversion(t *testing.T) {
	var capturedEvent Event
	// Create a wrapper to capture the event
	captureHandler := &captureTestHandler{captured: &capturedEvent}
	adapter := &pluginServerAdapter{handler: captureHandler}

	req := &pluginv1.HandleEventRequest{
		Event: &pluginv1.Event{
			Id:        "evt-abc",
			Stream:    "location:xyz",
			Type:      "custom",
			Timestamp: 9876543210,
			ActorKind: "system",
			ActorId:   "sys-001",
			Payload:   `{"key":"value"}`,
		},
	}

	_, err := adapter.HandleEvent(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify proto -> SDK Event conversion
	if capturedEvent.ID != "evt-abc" {
		t.Errorf("Event.ID = %q, want %q", capturedEvent.ID, "evt-abc")
	}
	if capturedEvent.Stream != "location:xyz" {
		t.Errorf("Event.Stream = %q, want %q", capturedEvent.Stream, "location:xyz")
	}
	if capturedEvent.Type != "custom" {
		t.Errorf("Event.Type = %q, want %q", capturedEvent.Type, "custom")
	}
	if capturedEvent.Timestamp != 9876543210 {
		t.Errorf("Event.Timestamp = %d, want %d", capturedEvent.Timestamp, 9876543210)
	}
	if capturedEvent.ActorKind != "system" {
		t.Errorf("Event.ActorKind = %q, want %q", capturedEvent.ActorKind, "system")
	}
	if capturedEvent.ActorID != "sys-001" {
		t.Errorf("Event.ActorID = %q, want %q", capturedEvent.ActorID, "sys-001")
	}
	if capturedEvent.Payload != `{"key":"value"}` {
		t.Errorf("Event.Payload = %q, want %q", capturedEvent.Payload, `{"key":"value"}`)
	}
}

type captureTestHandler struct {
	captured *Event
}

func (h *captureTestHandler) HandleEvent(_ context.Context, event Event) ([]EmitEvent, error) {
	*h.captured = event
	return nil, nil
}
