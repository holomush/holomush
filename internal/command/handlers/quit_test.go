// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/handlers/testutil"
	"github.com/holomush/holomush/internal/core"
)

func TestQuitHandler(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T, player testutil.PlayerContext) *core.SessionManager
		assertion func(t *testing.T, player testutil.PlayerContext, sessionMgr *core.SessionManager, output string, err error)
	}{
		{
			name: "outputs goodbye message",
			setup: func(t *testing.T, player testutil.PlayerContext) *core.SessionManager {
				sessionMgr := core.NewSessionManager()
				sessionMgr.Connect(player.CharacterID, player.CharacterID)
				return sessionMgr
			},
			assertion: func(t *testing.T, _ testutil.PlayerContext, _ *core.SessionManager, output string, err error) {
				require.NoError(t, err)
				assert.Contains(t, output, "Goodbye")
			},
		},
		{
			name: "ends session",
			setup: func(t *testing.T, player testutil.PlayerContext) *core.SessionManager {
				sessionMgr := core.NewSessionManager()
				sessionMgr.Connect(player.CharacterID, player.CharacterID)
				require.NotNil(t, sessionMgr.GetSession(player.CharacterID), "Session should exist before quit")
				return sessionMgr
			},
			assertion: func(t *testing.T, player testutil.PlayerContext, sessionMgr *core.SessionManager, _ string, err error) {
				require.NoError(t, err)
				assert.Nil(t, sessionMgr.GetSession(player.CharacterID), "Session should not exist after quit")
			},
		},
		{
			name: "returns error on session end failure",
			setup: func(_ *testing.T, _ testutil.PlayerContext) *core.SessionManager {
				return core.NewSessionManager()
			},
			assertion: func(t *testing.T, _ testutil.PlayerContext, _ *core.SessionManager, _ string, err error) {
				require.Error(t, err)
				msg := command.PlayerMessage(err)
				assert.NotEmpty(t, msg)
			},
		},
		{
			name: "outputs goodbye before error",
			setup: func(_ *testing.T, _ testutil.PlayerContext) *core.SessionManager {
				return core.NewSessionManager()
			},
			assertion: func(t *testing.T, _ testutil.PlayerContext, _ *core.SessionManager, output string, _ error) {
				assert.Contains(t, output, "Goodbye")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			player := testutil.RegularPlayer()
			sessionMgr := tt.setup(t, player)
			services := testutil.NewServicesBuilder().WithSession(sessionMgr).Build()
			exec, buf := testutil.NewExecutionBuilder().
				WithCharacter(player).
				WithServices(services).
				Build()

			err := QuitHandler(context.Background(), exec)
			tt.assertion(t, player, sessionMgr, buf.String(), err)
		})
	}
}
