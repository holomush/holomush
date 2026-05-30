// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginv1_test

import (
	"context"
	"testing"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// TestPluginServiceInterface verifies the Plugin gRPC service interface exists.
func TestPluginServiceInterface(_ *testing.T) {
	// Verify the PluginServer interface has HandleEvent method
	var _ pluginv1.PluginServiceServer = (*mockPluginServer)(nil)
}

// mockPluginServer implements PluginServer for compile-time verification.
type mockPluginServer struct {
	pluginv1.UnimplementedPluginServiceServer
}

func (m *mockPluginServer) HandleEvent(_ context.Context, req *pluginv1.HandleEventRequest) (*pluginv1.HandleEventResponse, error) {
	// Verify request has expected structure
	if req.Event != nil {
		_ = req.Event.Id
		_ = req.Event.Stream
		_ = req.Event.Type
		_ = req.Event.Timestamp
		_ = req.Event.ActorKind
		_ = req.Event.ActorId
		_ = req.Event.Payload
	}
	return &pluginv1.HandleEventResponse{
		EmitEvents: []*pluginv1.EmitEvent{
			{
				Stream:  "location:loc_123",
				Type:    "say",
				Payload: `{"text":"response"}`,
			},
		},
	}, nil
}
