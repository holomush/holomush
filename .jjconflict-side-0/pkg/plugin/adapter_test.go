// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	"google.golang.org/grpc"
)

func TestGRPCPlugin_GRPCServer_NilHandler(t *testing.T) {
	p := &grpcPlugin{handler: nil}
	// Use a real grpc.Server - it's cheap to create for testing
	s := grpc.NewServer()
	defer s.Stop()

	err := p.GRPCServer(nil, s)
	require.Error(t, err, "expected error when handler is nil")
	assert.Equal(t, "plugin: handler is nil", err.Error())
}

func TestGRPCPlugin_GRPCServer_RegistersService(t *testing.T) {
	handler := &adapterTestHandler{}
	p := &grpcPlugin{handler: handler}
	s := grpc.NewServer()
	defer s.Stop()

	err := p.GRPCServer(nil, s)
	require.NoError(t, err)

	// Verify service was registered by checking GetServiceInfo
	info := s.GetServiceInfo()
	assert.Contains(t, info, "holomush.plugin.v1.Plugin", "expected Plugin service to be registered")
}

func TestGRPCPlugin_GRPCClient_ReturnsError(t *testing.T) {
	p := &grpcPlugin{handler: nil}

	// GRPCClient is not implemented on the plugin side (only host calls it).
	// Verify it returns an error as expected.
	client, err := p.GRPCClient(context.Background(), nil, nil)
	require.Error(t, err, "expected error from GRPCClient on plugin side")
	assert.Nil(t, client, "expected nil client when error is returned")
	assert.Equal(t, "plugin: GRPCClient not implemented on plugin side", err.Error())
}

type adapterTestHandler struct {
	response []EmitEvent
	err      error
}

func (h *adapterTestHandler) HandleEvent(_ context.Context, _ Event) ([]EmitEvent, error) {
	if h.err != nil {
		return nil, h.err
	}
	return h.response, nil
}

func TestPluginServerAdapter_HandleEvent_Success(t *testing.T) {
	handler := &adapterTestHandler{
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
	require.NoError(t, err)

	require.Len(t, resp.GetEmitEvents(), 1, "expected 1 emit event")

	emit := resp.GetEmitEvents()[0]
	assert.Equal(t, "room:123", emit.GetStream())
	assert.Equal(t, "say", emit.GetType())
	assert.Equal(t, `{"text":"hello"}`, emit.GetPayload())
}

var errHandlerFailed = errors.New("handler failed")

func TestPluginServerAdapter_HandleEvent_HandlerError(t *testing.T) {
	handler := &adapterTestHandler{
		err: errHandlerFailed,
	}
	adapter := &pluginServerAdapter{handler: handler}

	req := &pluginv1.HandleEventRequest{
		Event: &pluginv1.Event{
			Id: "evt-123",
		},
	}

	_, err := adapter.HandleEvent(context.Background(), req)
	require.Error(t, err, "expected error when handler fails")
	assert.ErrorIs(t, err, errHandlerFailed, "should wrap handler error")
}

func TestPluginServerAdapter_HandleEvent_EmptyEmits(t *testing.T) {
	handler := &adapterTestHandler{
		response: []EmitEvent{},
	}
	adapter := &pluginServerAdapter{handler: handler}

	req := &pluginv1.HandleEventRequest{
		Event: &pluginv1.Event{
			Id: "evt-123",
		},
	}

	resp, err := adapter.HandleEvent(context.Background(), req)
	require.NoError(t, err)

	assert.Empty(t, resp.GetEmitEvents(), "expected 0 emit events")
}

func TestPluginServerAdapter_HandleEvent_MultipleEmits(t *testing.T) {
	handler := &adapterTestHandler{
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
	require.NoError(t, err)

	require.Len(t, resp.GetEmitEvents(), 3, "expected 3 emit events")

	expectedStreams := []string{"room:1", "room:2", "room:3"}
	for i, emit := range resp.GetEmitEvents() {
		assert.Equal(t, expectedStreams[i], emit.GetStream())
	}
}

func TestPluginServerAdapter_HandleEvent_NilEvent(t *testing.T) {
	handler := &adapterTestHandler{response: nil}
	adapter := &pluginServerAdapter{handler: handler}

	req := &pluginv1.HandleEventRequest{
		Event: nil, // nil event
	}

	resp, err := adapter.HandleEvent(context.Background(), req)
	require.NoError(t, err)

	// Should handle gracefully with empty response
	assert.NotNil(t, resp, "expected non-nil response")
}

func TestPluginServerAdapter_HandleEvent_EventConversion(t *testing.T) {
	var capturedEvent Event
	// Create a wrapper to capture the event
	captureHandler := &captureAdapterTestHandler{captured: &capturedEvent}
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
	require.NoError(t, err)

	// Verify proto -> SDK Event conversion
	assert.Equal(t, "evt-abc", capturedEvent.ID)
	assert.Equal(t, "location:xyz", capturedEvent.Stream)
	assert.Equal(t, EventType("custom"), capturedEvent.Type)
	assert.Equal(t, int64(9876543210), capturedEvent.Timestamp)
	assert.Equal(t, ActorSystem, capturedEvent.ActorKind)
	assert.Equal(t, "sys-001", capturedEvent.ActorID)
	assert.Equal(t, `{"key":"value"}`, capturedEvent.Payload)
}

type captureAdapterTestHandler struct {
	captured *Event
}

func (h *captureAdapterTestHandler) HandleEvent(_ context.Context, event Event) ([]EmitEvent, error) {
	*h.captured = event
	return nil, nil
}
