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

	subject := access.SubjectCharacter + exec.CharacterID().String()
	decision, err := r.engine.Evaluate(ctx, types.AccessRequest{
		Subject:  subject,
		Action:   "execute",
		Resource: CapabilityRateLimitBypass,
	})
	if err != nil {
		// Fail-closed on evaluation error: apply rate limiting rather than bypassing
		slog.WarnContext(ctx, "rate limit bypass check failed",
			"subject", subject,
			"action", "execute",
			"resource", CapabilityRateLimitBypass,
			"error", err,
		)
	}
	if err == nil && decision.IsAllowed() {
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
