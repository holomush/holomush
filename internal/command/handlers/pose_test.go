// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
)

func TestPoseHandler(t *testing.T) {
	tests := []struct {
		name          string
		args          string
		appendErr     error
		wantErr       bool
		wantEventType core.EventType
	}{
		{
			name:          "emits pose event",
			args:          "waves hello",
			wantEventType: core.EventTypePose,
		},
		{
			name:      "event store failure",
			args:      "waves",
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

			err := PoseHandler(context.Background(), exec)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantEventType, appended.Type)
			assert.Equal(t, "location:"+locationID.String(), appended.Stream)
			assert.Contains(t, string(appended.Payload), tt.args)
		})
	}
}
