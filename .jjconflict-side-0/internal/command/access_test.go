// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command_test

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/observability"
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
		infraEngine  bool
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
			infraEngine:  true,
			expectedCode: command.CodeAccessEvaluationFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error

			switch {
			case tt.useErrEngine:
				engine := policytest.NewErrorEngine(errors.New("db unavailable"))
				metricKey := tt.cmdName + "_access_check"
				before := testutil.ToFloat64(observability.EngineFailureCounter(metricKey))
				err = command.CheckCapability(ctx, engine, tt.subject, tt.capability, tt.cmdName)
				after := testutil.ToFloat64(observability.EngineFailureCounter(metricKey))
				assert.Equal(t, before+1, after, "RecordEngineFailure should increment for engine error")
			case tt.infraEngine:
				engine := policytest.NewInfraFailureEngine(t, "cache stale", "infra:cache-stale")
				metricKey := tt.cmdName + "_access_check"
				before := testutil.ToFloat64(observability.EngineFailureCounter(metricKey))
				err = command.CheckCapability(ctx, engine, tt.subject, tt.capability, tt.cmdName)
				after := testutil.ToFloat64(observability.EngineFailureCounter(metricKey))
				assert.Equal(t, before+1, after, "RecordEngineFailure should increment for infra failure")
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

			if tt.expectedCode == command.CodeAccessEvaluationFailed {
				assert.ErrorIs(t, err, command.ErrCapabilityCheckFailed,
					"error and infra-failure paths should include ErrCapabilityCheckFailed in error chain")
			}

			oopsErr, ok := oops.AsOops(err)
			require.True(t, ok, "error should be an oops error")
			assert.NotEmpty(t, oopsErr.Context()["command"], "should have command context")
			assert.NotEmpty(t, oopsErr.Context()["capability"], "should have capability context")

			if tt.infraEngine {
				errutil.AssertErrorContext(t, err, "reason", "cache stale")
				errutil.AssertErrorContext(t, err, "policy_id", "infra:cache-stale")
			}
		})
	}
}

func TestCheckCapability_InvalidRequest(t *testing.T) {
	ctx := context.Background()
	engine := policytest.NewGrantEngine()

	t.Run("empty subject", func(t *testing.T) {
		err := command.CheckCapability(ctx, engine, "", "cmd:say", "say")
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, command.CodeAccessEvaluationFailed)
	})

	t.Run("empty capability", func(t *testing.T) {
		err := command.CheckCapability(ctx, engine, "character:01ABC", "", "say")
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, command.CodeAccessEvaluationFailed)
	})

	t.Run("context cancelled", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()
		errEngine := policytest.NewErrorEngine(cancelCtx.Err())
		err := command.CheckCapability(cancelCtx, errEngine, "character:01ABC", "cmd:say", "say")
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, command.CodeAccessEvaluationFailed)
	})
}
