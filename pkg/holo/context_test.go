// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package holo_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/pkg/holo"
)

func TestCommandContext_Fields(t *testing.T) {
	ctx := holo.CommandContext{
		Name:          "say",
		Args:          "Hello everyone!",
		InvokedAs:     ";",
		CharacterName: "Alice",
		CharacterID:   "01ABC123456789ABCDEFGHJKLM",
		LocationID:    "01DEF123456789ABCDEFGHJKLM",
		PlayerID:      "01GHI123456789ABCDEFGHJKLM",
	}

	assert.Equal(t, "say", ctx.Name)
	assert.Equal(t, "Hello everyone!", ctx.Args)
	assert.Equal(t, ";", ctx.InvokedAs)
	assert.Equal(t, "Alice", ctx.CharacterName)
	assert.Equal(t, "01ABC123456789ABCDEFGHJKLM", ctx.CharacterID)
	assert.Equal(t, "01DEF123456789ABCDEFGHJKLM", ctx.LocationID)
	assert.Equal(t, "01GHI123456789ABCDEFGHJKLM", ctx.PlayerID)
}

func TestCommandContext_EmptyArgs(t *testing.T) {
	ctx := holo.CommandContext{
		Name:          "look",
		Args:          "",
		CharacterName: "Bob",
		CharacterID:   "01ABC123456789ABCDEFGHJKLM",
		LocationID:    "01DEF123456789ABCDEFGHJKLM",
		PlayerID:      "01GHI123456789ABCDEFGHJKLM",
	}

	// Verify all fields are accessible even with empty args
	assert.Equal(t, "look", ctx.Name)
	assert.Equal(t, "", ctx.Args, "empty args should be handled")
	assert.Equal(t, "Bob", ctx.CharacterName)
	assert.Equal(t, "01ABC123456789ABCDEFGHJKLM", ctx.CharacterID)
	assert.Equal(t, "01DEF123456789ABCDEFGHJKLM", ctx.LocationID)
	assert.Equal(t, "01GHI123456789ABCDEFGHJKLM", ctx.PlayerID)
}

func TestCommandContext_ZeroValue(t *testing.T) {
	var ctx holo.CommandContext

	assert.Equal(t, "", ctx.Name)
	assert.Equal(t, "", ctx.Args)
	assert.Equal(t, "", ctx.InvokedAs)
	assert.Equal(t, "", ctx.CharacterName)
	assert.Equal(t, "", ctx.CharacterID)
	assert.Equal(t, "", ctx.LocationID)
	assert.Equal(t, "", ctx.PlayerID)
}

func TestCommandContext_ValidateULIDs(t *testing.T) {
	tests := []struct {
		name    string
		ctx     holo.CommandContext
		wantErr bool
		errMsg  string
	}{
		{
			name: "all valid ULIDs",
			ctx: holo.CommandContext{
				CharacterID: "01ABC123456789ABCDEFGHJKLM",
				LocationID:  "01DEF123456789ABCDEFGHJKLM",
				PlayerID:    "01GHI123456789ABCDEFGHJKLM",
			},
			wantErr: false,
		},
		{
			name:    "all empty fields are valid",
			ctx:     holo.CommandContext{},
			wantErr: false,
		},
		{
			name: "invalid CharacterID",
			ctx: holo.CommandContext{
				CharacterID: "not-a-ulid",
				LocationID:  "01DEF123456789ABCDEFGHJKLM",
				PlayerID:    "01GHI123456789ABCDEFGHJKLM",
			},
			wantErr: true,
			errMsg:  "CharacterID",
		},
		{
			name: "invalid LocationID",
			ctx: holo.CommandContext{
				CharacterID: "01ABC123456789ABCDEFGHJKLM",
				LocationID:  "bad",
				PlayerID:    "01GHI123456789ABCDEFGHJKLM",
			},
			wantErr: true,
			errMsg:  "LocationID",
		},
		{
			name: "invalid PlayerID",
			ctx: holo.CommandContext{
				CharacterID: "01ABC123456789ABCDEFGHJKLM",
				LocationID:  "01DEF123456789ABCDEFGHJKLM",
				PlayerID:    "xyz",
			},
			wantErr: true,
			errMsg:  "PlayerID",
		},
		{
			name: "some empty some valid",
			ctx: holo.CommandContext{
				CharacterID: "01ABC123456789ABCDEFGHJKLM",
				LocationID:  "",
				PlayerID:    "",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.ctx.ValidateULIDs()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestParseCommandPayload(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		expected holo.CommandContext
	}{
		{
			name:    "full payload",
			payload: `{"name":"say","args":"Hello everyone!","invoked_as":";","character_name":"Alice","character_id":"01ABC","location_id":"01DEF","player_id":"01GHI"}`,
			expected: holo.CommandContext{
				Name:          "say",
				Args:          "Hello everyone!",
				InvokedAs:     ";",
				CharacterName: "Alice",
				CharacterID:   "01ABC",
				LocationID:    "01DEF",
				PlayerID:      "01GHI",
			},
		},
		{
			name:    "empty args",
			payload: `{"name":"look","args":"","character_name":"Bob","character_id":"01ABC","location_id":"01DEF","player_id":"01GHI"}`,
			expected: holo.CommandContext{
				Name:          "look",
				Args:          "",
				CharacterName: "Bob",
				CharacterID:   "01ABC",
				LocationID:    "01DEF",
				PlayerID:      "01GHI",
			},
		},
		{
			name:    "missing optional fields",
			payload: `{"name":"pose","args":"waves","character_name":"Carol","location_id":"01DEF"}`,
			expected: holo.CommandContext{
				Name:          "pose",
				Args:          "waves",
				CharacterName: "Carol",
				LocationID:    "01DEF",
			},
		},
		{
			name:     "empty payload",
			payload:  "",
			expected: holo.CommandContext{},
		},
		{
			name:     "invalid JSON",
			payload:  "not json at all",
			expected: holo.CommandContext{},
		},
		{
			name:     "empty JSON object",
			payload:  "{}",
			expected: holo.CommandContext{},
		},
		{
			name:    "escaped characters in args",
			payload: `{"name":"say","args":"Hello \"world\"!","character_name":"Dave","character_id":"01ABC","location_id":"01DEF","player_id":"01GHI"}`,
			expected: holo.CommandContext{
				Name:          "say",
				Args:          `Hello "world"!`,
				CharacterName: "Dave",
				CharacterID:   "01ABC",
				LocationID:    "01DEF",
				PlayerID:      "01GHI",
			},
		},
		{
			name:    "newlines in args",
			payload: `{"name":"emit","args":"Line1\nLine2","character_name":"Eve","character_id":"01ABC","location_id":"01DEF","player_id":"01GHI"}`,
			expected: holo.CommandContext{
				Name:          "emit",
				Args:          "Line1\nLine2",
				CharacterName: "Eve",
				CharacterID:   "01ABC",
				LocationID:    "01DEF",
				PlayerID:      "01GHI",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := holo.ParseCommandPayload(tt.payload)
			assert.Equal(t, tt.expected, got)
		})
	}
}
