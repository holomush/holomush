// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestNewRateLimitMiddleware(t *testing.T) {
	tests := []struct {
		name        string
		limiter     *RateLimiter
		engine      policy.AccessPolicyEngine
		wantErr     error
		description string
	}{
		{
			name:        "nil limiter",
			limiter:     nil,
			engine:      policytest.DenyAllEngine(),
			wantErr:     ErrNilRateLimiter,
			description: "should return error with nil limiter",
		},
		{
			name: "nil engine",
			limiter: NewRateLimiter(RateLimiterConfig{
				BurstCapacity: 1,
				SustainedRate: 0.1,
			}),
			engine:      nil,
			wantErr:     ErrNilEngine,
			description: "should return error with nil engine",
		},
		{
			name: "valid args",
			limiter: NewRateLimiter(RateLimiterConfig{
				BurstCapacity: 1,
				SustainedRate: 0.1,
			}),
			engine:      policytest.AllowAllEngine(),
			wantErr:     nil,
			description: "should succeed with valid limiter and engine",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.limiter != nil {
				defer tt.limiter.Close()
			}

			middleware, err := NewRateLimitMiddleware(tt.limiter, tt.engine)

			if tt.wantErr != nil {
				require.Error(t, err, tt.description)
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, middleware)
			} else {
				require.NoError(t, err, tt.description)
				assert.NotNil(t, middleware)
			}
		})
	}
}

func TestRateLimitMiddleware_Enforce_AccessRequest(t *testing.T) {
	// Create a simple capturing engine implementation
	var captured types.AccessRequest
	captureEngine := &capturingEngineImpl{captured: &captured}

	ratelimiter := NewRateLimiter(RateLimiterConfig{
		BurstCapacity: 1,
		SustainedRate: 0.1,
	})
	defer ratelimiter.Close()

	middleware, err := NewRateLimitMiddleware(ratelimiter, captureEngine)
	require.NoError(t, err)

	charID := ulid.Make()
	sessionID := ulid.Make()
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: charID,
		SessionID:   sessionID,
		Output:      &bytes.Buffer{},
		Services:    stubServices(),
	})

	ctx := context.Background()
	_, span := noop.NewTracerProvider().Tracer("test").Start(ctx, "test")

	// Invoke Enforce
	_ = middleware.Enforce(ctx, exec, "test-command", span)

	// Verify the AccessRequest was constructed correctly
	expectedSubject := access.SubjectCharacter + charID.String()
	assert.Equal(t, expectedSubject, captured.Subject, "subject should be SubjectCharacter + charID")
	assert.Equal(t, "execute", captured.Action, "action should be 'execute'")
	assert.Equal(t, CapabilityRateLimitBypass, captured.Resource, "resource should be CapabilityRateLimitBypass")
}

// capturingEngineImpl captures the AccessRequest for testing.
type capturingEngineImpl struct {
	captured *types.AccessRequest
}

// Evaluate captures the request and returns deny.
func (e *capturingEngineImpl) Evaluate(_ context.Context, req types.AccessRequest) (types.Decision, error) {
	*e.captured = req
	return types.NewDecision(types.EffectDeny, "test", ""), nil
}

func TestRateLimitMiddleware_Enforce_EngineError(t *testing.T) {
	// Engine that returns an error
	engineErr := errors.New("policy store unavailable")
	errorEngine := policytest.NewErrorEngine(engineErr)

	ratelimiter := NewRateLimiter(RateLimiterConfig{
		BurstCapacity: 1,
		SustainedRate: 0.1,
	})
	defer ratelimiter.Close()

	middleware, err := NewRateLimitMiddleware(ratelimiter, errorEngine)
	require.NoError(t, err)

	charID := ulid.Make()
	sessionID := ulid.Make()
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: charID,
		SessionID:   sessionID,
		Output:      &bytes.Buffer{},
		Services:    stubServices(),
	})

	ctx := context.Background()
	_, span := noop.NewTracerProvider().Tracer("test").Start(ctx, "test")

	// First call: engine returns error, but rate limiter has token → should succeed
	err = middleware.Enforce(ctx, exec, "test-command", span)
	require.NoError(t, err, "first call should succeed (engine error → rate limit applied → has token)")

	// Second call: engine returns error, rate limiter exhausted → should be rate limited
	err = middleware.Enforce(ctx, exec, "test-command", span)
	require.Error(t, err, "second call should be rate limited (engine error → rate limit applied → no token)")

	errutil.AssertErrorCode(t, err, CodeRateLimited)
}

func TestRateLimitMiddleware_Enforce_DenyDecision(t *testing.T) {
	// Engine that explicitly denies
	engine := policytest.DenyAllEngine()

	ratelimiter := NewRateLimiter(RateLimiterConfig{
		BurstCapacity: 1,
		SustainedRate: 0.1,
	})
	defer ratelimiter.Close()

	middleware, err := NewRateLimitMiddleware(ratelimiter, engine)
	require.NoError(t, err)

	charID := ulid.Make()
	sessionID := ulid.Make()
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: charID,
		SessionID:   sessionID,
		Output:      &bytes.Buffer{},
		Services:    stubServices(),
	})

	ctx := context.Background()
	_, span := noop.NewTracerProvider().Tracer("test").Start(ctx, "test")

	// First call: explicit deny → rate limit applied → has token → should succeed
	err = middleware.Enforce(ctx, exec, "test-command", span)
	require.NoError(t, err, "first call should succeed despite deny decision (has rate limit token)")

	// Second call: explicit deny → rate limit applied → no token → should be rate limited
	err = middleware.Enforce(ctx, exec, "test-command", span)
	require.Error(t, err, "second call should be rate limited (deny decision + exhausted tokens)")

	errutil.AssertErrorCode(t, err, CodeRateLimited)
}

func TestRateLimitMiddleware_Enforce_AllowDecision(t *testing.T) {
	// Engine that allows bypass
	engine := policytest.NewGrantEngine()

	ratelimiter := NewRateLimiter(RateLimiterConfig{
		BurstCapacity: 1,
		SustainedRate: 0.1,
	})
	defer ratelimiter.Close()

	middleware, err := NewRateLimitMiddleware(ratelimiter, engine)
	require.NoError(t, err)

	charID := ulid.Make()
	sessionID := ulid.Make()
	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: charID,
		SessionID:   sessionID,
		Output:      &bytes.Buffer{},
		Services:    stubServices(),
	})

	// Grant bypass capability
	engine.Grant(access.SubjectCharacter+charID.String(), "execute", CapabilityRateLimitBypass)

	ctx := context.Background()
	_, span := noop.NewTracerProvider().Tracer("test").Start(ctx, "test")

	// Execute many times - should never be rate limited due to bypass
	for i := 0; i < 10; i++ {
		err = middleware.Enforce(ctx, exec, "test-command", span)
		require.NoError(t, err, "call %d should succeed (bypass capability granted)", i+1)
	}
}

func TestRateLimitMiddleware_Enforce_NilMiddleware(t *testing.T) {
	// Nil receiver should return nil (safe no-op behavior)
	var middleware *RateLimitMiddleware

	ctx := context.Background()
	_, span := noop.NewTracerProvider().Tracer("test").Start(ctx, "test")

	exec := NewTestExecution(CommandExecutionConfig{
		CharacterID: ulid.Make(),
		SessionID:   ulid.Make(),
		Output:      &bytes.Buffer{},
		Services:    stubServices(),
	})

	err := middleware.Enforce(ctx, exec, "test-command", span)
	assert.NoError(t, err, "nil middleware should return nil (safe no-op)")
}
