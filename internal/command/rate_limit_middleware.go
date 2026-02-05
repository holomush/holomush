// Copyright 2026 HoloMUSH Contributors

package command

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/observability"
)

// RateLimitMiddleware enforces per-session rate limiting.
type RateLimitMiddleware struct {
	limiter *RateLimiter
	access  access.AccessControl
}

// NewRateLimitMiddleware creates a rate limiting middleware.
func NewRateLimitMiddleware(limiter *RateLimiter, accessControl access.AccessControl) *RateLimitMiddleware {
	if limiter == nil {
		return nil
	}
	return &RateLimitMiddleware{
		limiter: limiter,
		access:  accessControl,
	}
}

// Enforce checks and enforces rate limits for the provided execution context.
func (r *RateLimitMiddleware) Enforce(ctx context.Context, exec *CommandExecution, commandName string, span trace.Span) error {
	if r == nil || r.limiter == nil {
		return nil
	}

	subject := "char:" + exec.CharacterID().String()
	hasBypass := r.access.Check(ctx, subject, "execute", CapabilityRateLimitBypass)
	if hasBypass {
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
