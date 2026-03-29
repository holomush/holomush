// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

func TestCommandRequest_Fields(t *testing.T) {
	req := pluginsdk.CommandRequest{
		Command:       "say",
		Args:          "hello world",
		CharacterID:   "01JTEST000000000000000000",
		CharacterName: "Alice",
		LocationID:    "01JTEST000000000000000001",
		SessionID:     "01JTEST000000000000000002",
		InvokedAs:     "say",
	}

	assert.Equal(t, "say", req.Command)
	assert.Equal(t, "hello world", req.Args)
	assert.Equal(t, "01JTEST000000000000000000", req.CharacterID)
	assert.Equal(t, "Alice", req.CharacterName)
	assert.Equal(t, "01JTEST000000000000000001", req.LocationID)
	assert.Equal(t, "01JTEST000000000000000002", req.SessionID)
	assert.Equal(t, "say", req.InvokedAs)
}

func TestCommandResponse_Fields(t *testing.T) {
	resp := pluginsdk.CommandResponse{
		Events: []pluginsdk.EmitEvent{
			{Stream: "location:test", Type: "say", Payload: `{"message":"hi"}`},
		},
		Output:         "You say, \"hi\"",
		BootedSessions: []string{"session-1"},
		EndSession:     true,
	}

	assert.Len(t, resp.Events, 1)
	assert.Equal(t, "You say, \"hi\"", resp.Output)
	assert.Equal(t, []string{"session-1"}, resp.BootedSessions)
	assert.True(t, resp.EndSession)
}
