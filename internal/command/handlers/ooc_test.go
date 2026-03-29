// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
)

func TestOOCHandler(t *testing.T) {
	tests := []struct {
		name          string
		args          string
		appendErr     error
		wantErr       bool
		wantEventType core.EventType
		wantStyle     string
		wantMessage   string
	}{
		{
			name:          "say style",
			args:          "This is great",
			wantEventType: core.EventTypeOOC,
			wantStyle:     "say",
			wantMessage:   "This is great",
		},
		{
			name:          "pose style",
			args:          ":laughs",
			wantEventType: core.EventTypeOOC,
			wantStyle:     "pose",
			wantMessage:   "laughs",
		},
		{
			name:          "semipose style",
			args:          ";'s phone rings",
			wantEventType: core.EventTypeOOC,
			wantStyle:     "semipose",
			wantMessage:   "'s phone rings",
		},
		{
			name:    "empty message",
			args:    "",
			wantErr: true,
		},
		{
			name:    "whitespace only message",
			args:    "   ",
			wantErr: true,
		},
		{
			name:      "event store failure",
			args:      "Hello",
			appendErr: errors.New("db error"),
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			charID := ulid.Make()
			locationID := ulid.Make()

			var appended core.Event
			store := &stubEventStoreCapture{
				appendFunc: func(_ context.Context, e core.Event) error {
					appended = e
					return tt.appendErr
				},
			}

			svc := command.NewTestServices(command.ServicesConfig{
				World:   nil,
				Session: nil,
				Engine:  policytest.AllowAllEngine(),
				Events:  store,
			})

			exec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID:   charID,
				CharacterName: "TestChar",
				LocationID:    locationID,
				Output:        &bytes.Buffer{},
				Services:      svc,
			})
			exec.Args = tt.args

			err := OOCHandler(context.Background(), exec)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantEventType, appended.Type)
			assert.Equal(t, "location:"+locationID.String(), appended.Stream)

			var payload core.OOCPayload
			require.NoError(t, json.Unmarshal(appended.Payload, &payload))
			assert.Equal(t, tt.wantStyle, payload.Style)
			assert.Equal(t, tt.wantMessage, payload.Message)
			assert.Equal(t, "TestChar", payload.CharacterName)
		})
	}
}
