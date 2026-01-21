// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginv1_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	require.NotNil(t, req.Event, "expected EmitEventRequest to have Event field")

	resp := &pluginv1.EmitEventResponse{
		Success: true,
	}
	assert.True(t, resp.Success, "expected EmitEventResponse to have Success field")
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
	assert.NotEmpty(t, req.Message, "expected LogRequest to have Message field")
	assert.Equal(t, pluginv1.LogLevel_LOG_LEVEL_INFO, req.Level, "expected LogRequest to have Level field")
	assert.Len(t, req.Fields, 2, "expected 2 fields")

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
			assert.Equal(t, tt.name, tt.level.String())
		})
	}
}

// TestKVGetRPC verifies the KVGet RPC request/response types.
func TestKVGetRPC(t *testing.T) {
	req := &pluginv1.KVGetRequest{
		Key: "plugin:echo-bot:config",
	}
	assert.NotEmpty(t, req.Key, "expected KVGetRequest to have Key field")

	resp := &pluginv1.KVGetResponse{
		Value: []byte(`{"enabled":true}`),
		Found: true,
	}
	assert.True(t, resp.Found, "expected KVGetResponse to have Found field")
	assert.NotEmpty(t, resp.Value, "expected KVGetResponse to have Value field")
}

// TestKVSetRPC verifies the KVSet RPC request/response types.
func TestKVSetRPC(t *testing.T) {
	req := &pluginv1.KVSetRequest{
		Key:   "plugin:echo-bot:state",
		Value: []byte(`{"last_event":"01HQG..."}`),
	}
	assert.NotEmpty(t, req.Key, "expected KVSetRequest to have Key field")
	assert.NotEmpty(t, req.Value, "expected KVSetRequest to have Value field")

	resp := &pluginv1.KVSetResponse{
		Success: true,
	}
	assert.True(t, resp.Success, "expected KVSetResponse to have Success field")
}

// TestKVDeleteRPC verifies the KVDelete RPC request/response types.
func TestKVDeleteRPC(t *testing.T) {
	req := &pluginv1.KVDeleteRequest{
		Key: "plugin:echo-bot:temp",
	}
	assert.NotEmpty(t, req.Key, "expected KVDeleteRequest to have Key field")

	resp := &pluginv1.KVDeleteResponse{
		Deleted: true,
	}
	assert.True(t, resp.Deleted, "expected KVDeleteResponse to have Deleted field")
}

// TestQueryRoomRPC verifies the QueryRoom RPC request/response types.
func TestQueryRoomRPC(t *testing.T) {
	req := &pluginv1.QueryRoomRequest{
		RoomId: "room_abc123",
	}
	assert.NotEmpty(t, req.RoomId, "expected QueryRoomRequest to have RoomId field")

	resp := &pluginv1.QueryRoomResponse{
		Room: &pluginv1.RoomInfo{
			Id:          "room_abc123",
			Name:        "The Town Square",
			Description: "A bustling central plaza.",
		},
	}
	require.NotNil(t, resp.Room, "expected QueryRoomResponse to have Room field")
	assert.NotEmpty(t, resp.Room.Name, "expected RoomInfo to have Name field")
}

// TestQueryCharacterRPC verifies the QueryCharacter RPC request/response types.
func TestQueryCharacterRPC(t *testing.T) {
	req := &pluginv1.QueryCharacterRequest{
		CharacterId: "char_123",
	}
	assert.NotEmpty(t, req.CharacterId, "expected QueryCharacterRequest to have CharacterId field")

	resp := &pluginv1.QueryCharacterResponse{
		Character: &pluginv1.CharacterInfo{
			Id:   "char_123",
			Name: "Alice",
		},
	}
	require.NotNil(t, resp.Character, "expected QueryCharacterResponse to have Character field")
	assert.NotEmpty(t, resp.Character.Name, "expected CharacterInfo to have Name field")
}

// TestQueryRoomCharactersRPC verifies the QueryRoomCharacters RPC.
func TestQueryRoomCharactersRPC(t *testing.T) {
	req := &pluginv1.QueryRoomCharactersRequest{
		RoomId: "room_abc123",
	}
	assert.NotEmpty(t, req.RoomId, "expected QueryRoomCharactersRequest to have RoomId field")

	resp := &pluginv1.QueryRoomCharactersResponse{
		Characters: []*pluginv1.CharacterInfo{
			{Id: "char_1", Name: "Alice"},
			{Id: "char_2", Name: "Bob"},
		},
	}
	assert.Len(t, resp.Characters, 2, "expected 2 characters")
}
