// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"
	"strings"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/audit"
	"github.com/holomush/holomush/internal/access/policy/types"
)

// AccessPolicyEngine is the main entry point for policy-based authorization.
type AccessPolicyEngine interface {
	Evaluate(ctx context.Context, request types.AccessRequest) (types.Decision, error)
}

// Engine implements AccessPolicyEngine.
type Engine struct {
	resolver *attribute.Resolver
	cache    *Cache
	sessions SessionResolver
	audit    *audit.Logger
}

// NewEngine creates a new policy engine with the given dependencies.
func NewEngine(resolver *attribute.Resolver, cache *Cache, sessions SessionResolver, auditLogger *audit.Logger) *Engine {
	return &Engine{
		resolver: resolver,
		cache:    cache,
		sessions: sessions,
		audit:    auditLogger,
	}
}

// Evaluate evaluates an access request against the policy engine.
// This implementation covers Steps 1-2 (system bypass + session resolution).
func (e *Engine) Evaluate(ctx context.Context, req types.AccessRequest) (types.Decision, error) {
	start := time.Now()

	// Step 1: System bypass
	if req.Subject == "system" {
		decision := types.NewDecision(types.EffectSystemBypass, "system bypass", "")
		if err := decision.Validate(); err != nil {
			return decision, oops.Wrapf(err, "decision validation failed")
		}

		// Audit system bypass
		entry := audit.Entry{
			Subject:    req.Subject,
			Action:     req.Action,
			Resource:   req.Resource,
			Effect:     types.EffectSystemBypass,
			PolicyID:   "",
			PolicyName: "",
			DurationUS: time.Since(start).Microseconds(),
			Timestamp:  time.Now(),
		}
		if err := e.audit.Log(ctx, entry); err != nil {
			// Log error but don't fail the decision
			_ = err
		}

		return decision, nil
	}

	// Step 2: Session resolution
	if strings.HasPrefix(req.Subject, "session:") {
		sessionID := strings.TrimPrefix(req.Subject, "session:")
		characterID, err := e.sessions.ResolveSession(ctx, sessionID)
		if err != nil {
			// Check if this is a SESSION_INVALID error
			oopsErr, ok := oops.AsOops(err)
			var decision types.Decision
			if ok && oopsErr.Code() == "SESSION_INVALID" {
				decision = types.NewDecision(types.EffectDefaultDeny, "session invalid", "infra:session-invalid")
			} else {
				decision = types.NewDecision(types.EffectDefaultDeny, "session store error", "infra:session-store-error")
			}

			if valErr := decision.Validate(); valErr != nil {
				return decision, oops.Wrapf(valErr, "decision validation failed")
			}
			return decision, nil
		}

		// Rewrite subject to character: format
		req.Subject = "character:" + characterID

		// Continue to placeholder (steps 3-6 not implemented)
	}

	// Placeholder for steps 3-6
	decision := types.NewDecision(types.EffectDefaultDeny, "evaluation pending", "")
	if err := decision.Validate(); err != nil {
		return decision, oops.Wrapf(err, "decision validation failed")
	}

	return decision, nil
}
