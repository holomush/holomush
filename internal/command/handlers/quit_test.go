// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/handlers/testutil"
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
			player := testutil.RegularPlayer()
			mock := testutil.NewMockSessionAccess()
			services := testutil.NewServicesBuilder().WithSession(mock).Build()
			exec, buf := testutil.NewExecutionBuilder().
				WithCharacter(player).
				WithServices(services).
				Build()

			err := QuitHandler(context.Background(), exec)
			tt.assertion(t, buf.String(), err)
		})
	}
}
