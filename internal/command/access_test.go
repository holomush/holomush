// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command_test

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestCheckCommandExecution(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		subject      string
		cmdName      string
		setupEngine  func() *policytest.GrantEngine
		useErrEngine bool
		expectedCode string
		expectNil    bool
	}{
		{
			name:    "allowed — engine grants execute on command resource",
			subject: "character:01ABC",
			cmdName: "say",
			setupEngine: func() *policytest.GrantEngine {
				e := policytest.NewGrantEngine()
				e.Grant("character:01ABC", "execute", "command:say")
				return e
			},
			expectNil: true,
		},
		{
			name:    "denied — engine denies execute on command resource",
			subject: "character:01ABC",
			cmdName: "admin",
			setupEngine: func() *policytest.GrantEngine {
				return policytest.NewGrantEngine()
			},
			expectedCode: command.CodePermissionDenied,
		},
		{
			name:         "engine error — returns ACCESS_EVALUATION_FAILED",
			subject:      "character:01ABC",
			cmdName:      "say",
			useErrEngine: true,
			expectedCode: command.CodeAccessEvaluationFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error

			if tt.useErrEngine {
				engine := policytest.NewErrorEngine(errors.New("db unavailable"))
				err = command.CheckCommandExecution(ctx, engine, tt.subject, tt.cmdName)
			} else {
				engine := tt.setupEngine()
				err = command.CheckCommandExecution(ctx, engine, tt.subject, tt.cmdName)
			}

			if tt.expectNil {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			errutil.AssertErrorCode(t, err, tt.expectedCode)

			oopsErr, ok := oops.AsOops(err)
			require.True(t, ok, "error should be an oops error")
			assert.NotEmpty(t, oopsErr.Context()["command"], "should have command context")
		})
	}
}

func TestCheckCommandExecutionInvalidRequest(t *testing.T) {
	ctx := context.Background()
	engine := policytest.NewGrantEngine()

	// Empty subject causes NewAccessRequest to fail
	err := command.CheckCommandExecution(ctx, engine, "", "say")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, command.CodeAccessEvaluationFailed)
	assert.ErrorIs(t, err, command.ErrCapabilityCheckFailed)
}

// --- CheckCapabilityPreFlight tests ---

func TestCheckCapabilityPreFlight(t *testing.T) {
	ctx := context.Background()

	t.Run("all capabilities allowed", func(t *testing.T) {
		engine := policytest.AllowAllEngine()
		caps := []command.Capability{
			{Action: "write", Resource: "location", Scope: command.ScopeLocal},
			{Action: "enter", Resource: "location", Scope: command.ScopeGlobal},
		}
		err := command.CheckCapabilityPreFlight(ctx, engine, "character:01ABC", "teleport", caps)
		require.NoError(t, err)
	})

	t.Run("first capability denied", func(t *testing.T) {
		engine := policytest.DenyAllEngine()
		caps := []command.Capability{
			{Action: "write", Resource: "location", Scope: command.ScopeLocal},
		}
		err := command.CheckCapabilityPreFlight(ctx, engine, "character:01ABC", "teleport", caps)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, command.CodePermissionDenied)
	})

	t.Run("second capability denied", func(t *testing.T) {
		// First capability succeeds, second fails
		m := &policytest.MockAccessPolicyEngine{}
		m.On("CanPerformAction", ctx, "character:01ABC", "write", "location", "local").
			Return(true, nil)
		m.On("CanPerformAction", ctx, "character:01ABC", "enter", "location", "global").
			Return(false, nil)

		caps := []command.Capability{
			{Action: "write", Resource: "location", Scope: command.ScopeLocal},
			{Action: "enter", Resource: "location", Scope: command.ScopeGlobal},
		}
		err := command.CheckCapabilityPreFlight(ctx, m, "character:01ABC", "teleport", caps)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, command.CodePermissionDenied)
	})

	t.Run("engine error propagates", func(t *testing.T) {
		engine := policytest.NewErrorEngine(errors.New("resolver crashed"))
		caps := []command.Capability{
			{Action: "write", Resource: "location", Scope: command.ScopeLocal},
		}
		err := command.CheckCapabilityPreFlight(ctx, engine, "character:01ABC", "teleport", caps)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, command.CodeAccessEvaluationFailed)
	})

	t.Run("empty capabilities — vacuously allowed", func(t *testing.T) {
		engine := policytest.DenyAllEngine()
		err := command.CheckCapabilityPreFlight(ctx, engine, "character:01ABC", "say", nil)
		require.NoError(t, err)
	})
}
