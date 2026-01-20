// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginv1_test

import (
	"testing"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// TestEmitEventRPC verifies the EmitEvent RPC request/response types.
func TestEmitEventRPC(t *testing.T) {
	req := &pluginv1.EmitEventRequest{
		Event: &pluginv1.EmitEvent{
			Stream:  "room:room_123",
			Type:    "say",
			Payload: `{"text":"Hello"}`,
		},
	}
	if req.Event == nil {
		t.Error("expected EmitEventRequest to have Event field")
	}

	resp := &pluginv1.EmitEventResponse{
		Success: true,
	}
	if !resp.Success {
		t.Error("expected EmitEventResponse to have Success field")
	}
}

// TestLogRPC verifies the Log RPC request/response types.
func TestLogRPC(t *testing.T) {
	req := &pluginv1.LogRequest{
		Level:   pluginv1.LogLevel_LOG_LEVEL_INFO,
		Message: "Plugin initialized",
		Fields: map[string]string{
			"plugin_id": "echo-bot",
			"version":   "1.0.0",
		},
	}
	if req.Message == "" {
		t.Error("expected LogRequest to have Message field")
	}
	if req.Level != pluginv1.LogLevel_LOG_LEVEL_INFO {
		t.Error("expected LogRequest to have Level field")
	}
	if len(req.Fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(req.Fields))
	}

	resp := &pluginv1.LogResponse{}
	_ = resp // Empty response is valid
}

// TestLogLevelEnum verifies log level constants exist.
func TestLogLevelEnum(t *testing.T) {
	levels := []struct {
		level pluginv1.LogLevel
		name  string
	}{
		{pluginv1.LogLevel_LOG_LEVEL_UNSPECIFIED, "LOG_LEVEL_UNSPECIFIED"},
		{pluginv1.LogLevel_LOG_LEVEL_DEBUG, "LOG_LEVEL_DEBUG"},
		{pluginv1.LogLevel_LOG_LEVEL_INFO, "LOG_LEVEL_INFO"},
		{pluginv1.LogLevel_LOG_LEVEL_WARN, "LOG_LEVEL_WARN"},
		{pluginv1.LogLevel_LOG_LEVEL_ERROR, "LOG_LEVEL_ERROR"},
	}

	for _, tt := range levels {
		t.Run(tt.name, func(t *testing.T) {
			if tt.level.String() != tt.name {
				t.Errorf("expected %s, got %s", tt.name, tt.level.String())
			}
		})
	}
}

// TestKVGetRPC verifies the KVGet RPC request/response types.
func TestKVGetRPC(t *testing.T) {
	req := &pluginv1.KVGetRequest{
		Key: "plugin:echo-bot:config",
	}
	if req.Key == "" {
		t.Error("expected KVGetRequest to have Key field")
	}

	resp := &pluginv1.KVGetResponse{
		Value: []byte(`{"enabled":true}`),
		Found: true,
	}
	if !resp.Found {
		t.Error("expected KVGetResponse to have Found field")
	}
	if len(resp.Value) == 0 {
		t.Error("expected KVGetResponse to have Value field")
	}
}

// TestKVSetRPC verifies the KVSet RPC request/response types.
func TestKVSetRPC(t *testing.T) {
	req := &pluginv1.KVSetRequest{
		Key:   "plugin:echo-bot:state",
		Value: []byte(`{"last_event":"01HQG..."}`),
	}
	if req.Key == "" {
		t.Error("expected KVSetRequest to have Key field")
	}
	if len(req.Value) == 0 {
		t.Error("expected KVSetRequest to have Value field")
	}

	resp := &pluginv1.KVSetResponse{
		Success: true,
	}
	if !resp.Success {
		t.Error("expected KVSetResponse to have Success field")
	}
}

// TestKVDeleteRPC verifies the KVDelete RPC request/response types.
func TestKVDeleteRPC(t *testing.T) {
	req := &pluginv1.KVDeleteRequest{
		Key: "plugin:echo-bot:temp",
	}
	if req.Key == "" {
		t.Error("expected KVDeleteRequest to have Key field")
	}

	resp := &pluginv1.KVDeleteResponse{
		Deleted: true,
	}
	if !resp.Deleted {
		t.Error("expected KVDeleteResponse to have Deleted field")
	}
}

// TestQueryRoomRPC verifies the QueryRoom RPC request/response types.
func TestQueryRoomRPC(t *testing.T) {
	req := &pluginv1.QueryRoomRequest{
		RoomId: "room_abc123",
	}
	if req.RoomId == "" {
		t.Error("expected QueryRoomRequest to have RoomId field")
	}

	resp := &pluginv1.QueryRoomResponse{
		Room: &pluginv1.RoomInfo{
			Id:          "room_abc123",
			Name:        "The Town Square",
			Description: "A bustling central plaza.",
		},
	}
	if resp.Room == nil {
		t.Error("expected QueryRoomResponse to have Room field")
	}
	if resp.Room.Name == "" {
		t.Error("expected RoomInfo to have Name field")
	}
}

// TestQueryCharacterRPC verifies the QueryCharacter RPC request/response types.
func TestQueryCharacterRPC(t *testing.T) {
	req := &pluginv1.QueryCharacterRequest{
		CharacterId: "char_123",
	}
	if req.CharacterId == "" {
		t.Error("expected QueryCharacterRequest to have CharacterId field")
	}

	resp := &pluginv1.QueryCharacterResponse{
		Character: &pluginv1.CharacterInfo{
			Id:   "char_123",
			Name: "Alice",
		},
	}
	if resp.Character == nil {
		t.Error("expected QueryCharacterResponse to have Character field")
	}
	if resp.Character.Name == "" {
		t.Error("expected CharacterInfo to have Name field")
	}
}

// TestQueryRoomCharactersRPC verifies the QueryRoomCharacters RPC.
func TestQueryRoomCharactersRPC(t *testing.T) {
	req := &pluginv1.QueryRoomCharactersRequest{
		RoomId: "room_abc123",
	}
	if req.RoomId == "" {
		t.Error("expected QueryRoomCharactersRequest to have RoomId field")
	}

	resp := &pluginv1.QueryRoomCharactersResponse{
		Characters: []*pluginv1.CharacterInfo{
			{Id: "char_1", Name: "Alice"},
			{Id: "char_2", Name: "Bob"},
		},
	}
	if len(resp.Characters) != 2 {
		t.Errorf("expected 2 characters, got %d", len(resp.Characters))
	}
}
