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

	"github.com/holomush/holomush/internal/command"
)

func TestQuitHandler(t *testing.T) {
	tests := []struct {
		name      string
		assertion func(t *testing.T, output string, err error)
	}{
		{
			name: "outputs goodbye message",
			assertion: func(t *testing.T, output string, err error) {
				assert.Contains(t, output, "Goodbye")
				require.Error(t, err)
			},
		},
		{
			name: "returns ErrSessionEnded",
			assertion: func(t *testing.T, _ string, err error) {
				require.Error(t, err)
				assert.True(t, errors.Is(err, command.ErrSessionEnded), "error should wrap ErrSessionEnded")
			},
		},
		{
			name: "outputs goodbye before returning error",
			assertion: func(t *testing.T, output string, err error) {
				assert.Contains(t, output, "Goodbye")
				require.Error(t, err)
				assert.True(t, errors.Is(err, command.ErrSessionEnded))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			exec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID:   ulid.Make(),
				CharacterName: "Player",
				PlayerID:      ulid.Make(),
				Output:        &buf,
			})

			err := QuitHandler(context.Background(), exec)
			tt.assertion(t, buf.String(), err)
		})
	}
}
