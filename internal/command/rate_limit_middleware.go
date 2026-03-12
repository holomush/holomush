// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"context"

	"github.com/samber/oops"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/observability"
	"github.com/holomush/holomush/pkg/errutil"
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
		return nil, ErrNilRateLimiterEngine
	}
	return &RateLimitMiddleware{
		limiter: limiter,
		engine:  engine,
	}, nil
}

// Enforce checks and enforces rate limits for the provided execution context.
// Callers should not invoke Enforce on a nil receiver, but a nil guard is
// retained as defense-in-depth.
func (r *RateLimitMiddleware) Enforce(ctx context.Context, exec *CommandExecution, commandName string, span trace.Span) error {
	if r == nil || r.limiter == nil {
		return nil
	}
	subject := access.CharacterSubject(exec.CharacterID().String())
	bypass, err := r.hasBypass(ctx, subject)
	if err != nil {
		// Fail-closed on evaluation error: apply rate limiting rather than bypassing.
		// The error is intentionally not returned to the caller because:
		// 1. The primary purpose of Enforce is rate limiting, not bypass evaluation.
		// 2. Returning an error here would prevent the command from executing entirely,
		//    which is worse than applying rate limits to a potentially exempt user.
		// 3. The error is logged at Error level and recorded as a metric so operators
		//    can detect persistent engine failures via monitoring.
		errutil.LogErrorContext(ctx, "rate limit bypass check failed, applying rate limiting (fail-closed)",
			err, "subject", subject, "action", "execute", "resource", CapabilityRateLimitBypass, "command", commandName,
		)
		observability.RecordEngineFailure("rate_limit_bypass")
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
		//nolint:wrapcheck // Error is returned to Enforce, which logs and drops it (fail-closed to rate limiting)
		return false, err
	}

	decision, err := r.engine.Evaluate(ctx, req)
	// On engine error, return (false, err) so Enforce can log and apply fail-closed rate limiting.
	if err != nil {
		//nolint:wrapcheck // Error is returned to Enforce, which logs and drops it (fail-closed to rate limiting)
		return false, err
	}

	// Infrastructure failures (session resolution errors, DB outages) return a deny
	// decision without a Go error. Surface these to the caller so Enforce can log them.
	if decision.IsInfraFailure() {
		return false, oops.With("reason", decision.Reason()).Errorf("infrastructure failure during bypass check")
	}

	return decision.IsAllowed(), nil
}
