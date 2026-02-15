// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/handlers/testutil"
	"github.com/holomush/holomush/internal/world"
)

func TestLookHandler(t *testing.T) {
	player := testutil.RegularPlayer()
	location := testutil.NewRoom("Test Room", "A cozy room with a fireplace.")

	tests := []struct {
		name      string
		setup     func(t *testing.T, fixture *testutil.WorldServiceFixture)
		assertion func(t *testing.T, output string, err error)
	}{
		{
			name: "outputs room name and description",
			setup: func(_ *testing.T, fixture *testutil.WorldServiceFixture) {
				fixture.Mocks.Engine.EXPECT().
					Evaluate(mock.Anything, types.AccessRequest{Subject: access.SubjectCharacter + player.CharacterID.String(), Action: "read", Resource: "location:" + location.ID.String()}).
					Return(types.NewDecision(types.EffectAllow, "", ""), nil)
				fixture.Mocks.LocationRepo.EXPECT().
					Get(mock.Anything, location.ID).
					Return(location, nil)
			},
			assertion: func(t *testing.T, output string, err error) {
				require.NoError(t, err)
				assert.Contains(t, output, "Test Room")
				assert.Contains(t, output, "A cozy room with a fireplace.")
			},
		},
		{
			name: "returns world error on failure",
			setup: func(_ *testing.T, fixture *testutil.WorldServiceFixture) {
				fixture.Mocks.Engine.EXPECT().
					Evaluate(mock.Anything, types.AccessRequest{Subject: access.SubjectCharacter + player.CharacterID.String(), Action: "read", Resource: "location:" + location.ID.String()}).
					Return(types.NewDecision(types.EffectAllow, "", ""), nil)
				fixture.Mocks.LocationRepo.EXPECT().
					Get(mock.Anything, location.ID).
					Return(nil, errors.New("database error"))
			},
			assertion: func(t *testing.T, _ string, err error) {
				require.Error(t, err)
				msg := command.PlayerMessage(err)
				assert.NotEmpty(t, msg)
			},
		},
		{
			name: "returns world error on access denied",
			setup: func(_ *testing.T, fixture *testutil.WorldServiceFixture) {
				fixture.Mocks.Engine.EXPECT().
					Evaluate(mock.Anything, types.AccessRequest{Subject: access.SubjectCharacter + player.CharacterID.String(), Action: "read", Resource: "location:" + location.ID.String()}).
					Return(types.NewDecision(types.EffectDeny, "", ""), nil)
			},
			assertion: func(t *testing.T, _ string, err error) {
				require.Error(t, err)
				msg := command.PlayerMessage(err)
				assert.NotEmpty(t, msg)
			},
		},
		{
			name: "returns LOCATION_ACCESS_EVALUATION_FAILED code when engine fails",
			setup: func(_ *testing.T, fixture *testutil.WorldServiceFixture) {
				fixture.Mocks.Engine.EXPECT().
					Evaluate(mock.Anything, types.AccessRequest{Subject: access.SubjectCharacter + player.CharacterID.String(), Action: "read", Resource: "location:" + location.ID.String()}).
					Return(types.Decision{}, errors.New("policy engine timeout"))
			},
			assertion: func(t *testing.T, _ string, err error) {
				require.Error(t, err)
				// Verify the error contains ErrAccessEvaluationFailed
				assert.ErrorIs(t, err, world.ErrAccessEvaluationFailed)
				// Verify it's an oops error with the world-specific code (not the generic command handler code)
				oopsErr, ok := oops.AsOops(err)
				require.True(t, ok, "error should be an oops error")
				assert.Equal(t, "LOCATION_ACCESS_EVALUATION_FAILED", oopsErr.Code(),
					"handler should preserve world service's specific code, not wrap as WORLD_ERROR")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := testutil.NewWorldServiceBuilder(t).Build()
			services := testutil.NewServicesBuilder().WithWorldFixture(fixture).Build()
			exec, buf := testutil.NewExecutionBuilder().
				WithCharacter(player).
				WithLocation(location).
				WithServices(services).
				Build()

			tt.setup(t, fixture)

			err := LookHandler(context.Background(), exec)
			tt.assertion(t, buf.String(), err)
		})
	}
}
