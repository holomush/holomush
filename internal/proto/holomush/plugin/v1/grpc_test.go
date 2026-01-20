// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginv1_test

import (
	"context"
	"testing"

	pluginv1 "github.com/holomush/holomush/internal/proto/holomush/plugin/v1"
)

// TestPluginServiceInterface verifies the Plugin gRPC service interface exists.
func TestPluginServiceInterface(_ *testing.T) {
	// Verify the PluginServer interface has HandleEvent method
	var _ pluginv1.PluginServer = (*mockPluginServer)(nil)
}

// mockPluginServer implements PluginServer for compile-time verification.
type mockPluginServer struct {
	pluginv1.UnimplementedPluginServer
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
				Stream:  "room:room_123",
				Type:    "say",
				Payload: `{"text":"response"}`,
			},
		},
	}, nil
}

// TestHostFunctionsServiceInterface verifies the HostFunctions gRPC service interface exists.
func TestHostFunctionsServiceInterface(_ *testing.T) {
	// Verify the HostFunctionsServer interface has all expected methods
	var _ pluginv1.HostFunctionsServer = (*mockHostFunctionsServer)(nil)
}

// mockHostFunctionsServer implements HostFunctionsServer for compile-time verification.
type mockHostFunctionsServer struct {
	pluginv1.UnimplementedHostFunctionsServer
}

func (m *mockHostFunctionsServer) EmitEvent(_ context.Context, _ *pluginv1.EmitEventRequest) (*pluginv1.EmitEventResponse, error) {
	return &pluginv1.EmitEventResponse{Success: true}, nil
}

func (m *mockHostFunctionsServer) QueryRoom(_ context.Context, _ *pluginv1.QueryRoomRequest) (*pluginv1.QueryRoomResponse, error) {
	return &pluginv1.QueryRoomResponse{
		Room: &pluginv1.RoomInfo{
			Id:          "room_123",
			Name:        "Test Room",
			Description: "A test room.",
		},
	}, nil
}

func (m *mockHostFunctionsServer) QueryCharacter(_ context.Context, _ *pluginv1.QueryCharacterRequest) (*pluginv1.QueryCharacterResponse, error) {
	return &pluginv1.QueryCharacterResponse{
		Character: &pluginv1.CharacterInfo{
			Id:   "char_123",
			Name: "Test Character",
		},
	}, nil
}

func (m *mockHostFunctionsServer) QueryRoomCharacters(_ context.Context, _ *pluginv1.QueryRoomCharactersRequest) (*pluginv1.QueryRoomCharactersResponse, error) {
	return &pluginv1.QueryRoomCharactersResponse{
		Characters: []*pluginv1.CharacterInfo{},
	}, nil
}

func (m *mockHostFunctionsServer) KVGet(_ context.Context, _ *pluginv1.KVGetRequest) (*pluginv1.KVGetResponse, error) {
	return &pluginv1.KVGetResponse{Found: false}, nil
}

func (m *mockHostFunctionsServer) KVSet(_ context.Context, _ *pluginv1.KVSetRequest) (*pluginv1.KVSetResponse, error) {
	return &pluginv1.KVSetResponse{Success: true}, nil
}

func (m *mockHostFunctionsServer) KVDelete(_ context.Context, _ *pluginv1.KVDeleteRequest) (*pluginv1.KVDeleteResponse, error) {
	return &pluginv1.KVDeleteResponse{Deleted: true}, nil
}

func (m *mockHostFunctionsServer) Log(_ context.Context, _ *pluginv1.LogRequest) (*pluginv1.LogResponse, error) {
	return &pluginv1.LogResponse{}, nil
}

// TestClientInterfaces verifies gRPC client interfaces exist.
func TestClientInterfaces(_ *testing.T) {
	// These are type assertions that will fail at compile time if interfaces don't exist
	var _ = (pluginv1.PluginClient)(nil)
	var _ = (pluginv1.HostFunctionsClient)(nil)
}
