// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/observability"
)

// RateLimitMiddleware enforces per-session rate limiting.
type RateLimitMiddleware struct {
	limiter *RateLimiter
	engine  types.AccessPolicyEngine
}

// NewRateLimitMiddleware creates a rate limiting middleware.
// Returns an error if the rate limiter or engine is nil.
func NewRateLimitMiddleware(limiter *RateLimiter, engine types.AccessPolicyEngine) (*RateLimitMiddleware, error) {
	if limiter == nil {
		return nil, ErrNilRateLimiter
	}
	if engine == nil {
		return nil, ErrNilEngine
	}
	return &RateLimitMiddleware{
		limiter: limiter,
		engine:  engine,
	}, nil
}

// Enforce checks and enforces rate limits for the provided execution context.
func (r *RateLimitMiddleware) Enforce(ctx context.Context, exec *CommandExecution, commandName string, span trace.Span) error {
	if r == nil || r.limiter == nil {
		return nil
	}

	subject := access.CharacterSubject(exec.CharacterID().String())
	bypass, err := r.hasBypass(ctx, subject)
	if err != nil {
		// Fail-closed on evaluation error: apply rate limiting rather than bypassing
		slog.WarnContext(ctx, "rate limit bypass check failed",
			"subject", subject,
			"action", "execute",
			"resource", CapabilityRateLimitBypass,
			"command", commandName,
			"error", err,
		)
		observability.RecordEngineFailure("rate_limit_bypass")
		// Continue to rate limiting check below (fail-closed)
	} else if bypass {
		return nil
	}

	allowed, cooldownMs := r.limiter.Allow(exec.SessionID())
	if allowed {
		return nil
	}

	span.SetAttributes(attribute.Bool("command.rate_limited", true))
	span.SetAttributes(attribute.Int64("command.cooldown_ms", cooldownMs))
	observability.RecordCommandRateLimited(commandName)
	return ErrRateLimited(cooldownMs)
}

// hasBypass evaluates whether the subject has rate limit bypass capability.
// Returns (bypass bool, err error) where:
//   - bypass is true if the bypass check succeeds and permission is granted
//   - err is non-nil if request construction or evaluation fails
//
// On evaluation error, returns (false, err) (fail-closed: apply rate limiting).
func (r *RateLimitMiddleware) hasBypass(ctx context.Context, subject string) (bool, error) {
	req, err := types.NewAccessRequest(subject, "execute", CapabilityRateLimitBypass)
	if err != nil {
		//nolint:wrapcheck // Constructor error, will be wrapped by caller
		return false, err
	}

	decision, err := r.engine.Evaluate(ctx, req)
	// Design decision: engine errors in bypass check return false (no bypass) rather than
	// propagating the error. This prevents engine outages from blocking rate-limited commands.
	// Errors are logged at Warn for operator visibility. See PR #88 review discussion.
	if err != nil {
		//nolint:wrapcheck // Engine error, will be wrapped by caller
		return false, err
	}
	return decision.IsAllowed(), nil
}
