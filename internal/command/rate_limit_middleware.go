// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/observability"
)

// RateLimitMiddleware enforces per-session rate limiting.
type RateLimitMiddleware struct {
	limiter *RateLimiter
	engine  policy.AccessPolicyEngine
}

// NewRateLimitMiddleware creates a rate limiting middleware.
// Returns an error if the rate limiter or engine is nil.
func NewRateLimitMiddleware(limiter *RateLimiter, engine policy.AccessPolicyEngine) (*RateLimitMiddleware, error) {
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
	if r.hasBypass(ctx, subject, commandName) {
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
// Returns true if the bypass check succeeds and permission is granted.
// On evaluation error, returns false (fail-closed: apply rate limiting).
func (r *RateLimitMiddleware) hasBypass(ctx context.Context, subject, commandName string) bool {
	decision, err := r.engine.Evaluate(ctx, types.AccessRequest{
		Subject:  subject,
		Action:   "execute",
		Resource: CapabilityRateLimitBypass,
	})
	if err != nil {
		// Fail-closed on evaluation error: apply rate limiting rather than bypassing
		slog.ErrorContext(ctx, "rate limit bypass check failed",
			"subject", subject,
			"action", "execute",
			"resource", CapabilityRateLimitBypass,
			"command", commandName,
			"error", err,
		)
		return false
	}
	return decision.IsAllowed()
}
