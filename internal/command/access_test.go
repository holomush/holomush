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

func TestCheckCapability(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		subject      string
		capability   string
		cmdName      string
		setupEngine  func() *policytest.GrantEngine
		useErrEngine bool
		expectedCode string
		expectNil    bool
	}{
		{
			name:       "allowed — engine grants capability",
			subject:    "character:01ABC",
			capability: "cmd:say",
			cmdName:    "say",
			setupEngine: func() *policytest.GrantEngine {
				e := policytest.NewGrantEngine()
				e.Grant("character:01ABC", "execute", "cmd:say")
				return e
			},
			expectNil: true,
		},
		{
			name:         "denied — engine denies capability",
			subject:      "character:01ABC",
			capability:   "cmd:admin",
			cmdName:      "admin",
			setupEngine:  func() *policytest.GrantEngine { return policytest.NewGrantEngine() },
			expectedCode: command.CodePermissionDenied,
		},
		{
			name:         "engine error — returns ACCESS_EVALUATION_FAILED",
			subject:      "character:01ABC",
			capability:   "cmd:say",
			cmdName:      "say",
			useErrEngine: true,
			expectedCode: command.CodeAccessEvaluationFailed,
		},
		{
			name:         "infra failure — returns ACCESS_EVALUATION_FAILED",
			subject:      "character:01ABC",
			capability:   "cmd:say",
			cmdName:      "say",
			expectedCode: command.CodeAccessEvaluationFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error

			switch {
			case tt.useErrEngine:
				engine := policytest.NewErrorEngine(errors.New("db unavailable"))
				err = command.CheckCapability(ctx, engine, tt.subject, tt.capability, tt.cmdName)
			case tt.name == "infra failure — returns ACCESS_EVALUATION_FAILED":
				engine := policytest.NewInfraFailureEngine("cache stale", "infra:cache-stale")
				err = command.CheckCapability(ctx, engine, tt.subject, tt.capability, tt.cmdName)
			default:
				engine := tt.setupEngine()
				err = command.CheckCapability(ctx, engine, tt.subject, tt.capability, tt.cmdName)
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
			assert.NotEmpty(t, oopsErr.Context()["capability"], "should have capability context")
		})
	}
}

func TestCheckCapability_InvalidRequest(t *testing.T) {
	ctx := context.Background()
	engine := policytest.NewGrantEngine()

	// Empty subject should fail request construction
	err := command.CheckCapability(ctx, engine, "", "cmd:say", "say")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, command.CodeAccessEvaluationFailed)
}
