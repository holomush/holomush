// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/audit"
	"github.com/holomush/holomush/internal/access/policy/dsl"
	"github.com/holomush/holomush/internal/access/policy/types"
)

// Engine implements types.AccessPolicyEngine.
type Engine struct {
	resolver *attribute.Resolver
	cache    *Cache
	sessions SessionResolver
	audit    *audit.Logger
}

// Compile-time check that Engine implements AccessPolicyEngine.
var _ types.AccessPolicyEngine = (*Engine)(nil)

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
// This implementation covers Steps 0-7 (full evaluation algorithm).
func (e *Engine) Evaluate(ctx context.Context, req types.AccessRequest) (types.Decision, error) {
	start := time.Now()

	// Step 0a: Context cancellation check
	if err := ctx.Err(); err != nil {
		return types.Decision{}, oops.Wrapf(err, "context cancelled before evaluation")
	}

	// Step 1: System bypass (before input validation — system always passes)
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
			slog.WarnContext(ctx, "audit log failed", "error", err)
		}

		return decision, nil
	}

	// Step 1b: Input validation — reject empty fields
	if err := validateRequest(req); err != nil {
		return types.Decision{}, err
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
		req.Subject = access.CharacterSubject(characterID)
	}

	// Step 3: Eager attribute resolution (non-fatal; bags may be nil/partial on error)
	bags, resolveErr := e.resolver.Resolve(ctx, req)
	if resolveErr != nil {
		slog.WarnContext(ctx, "attribute resolution failed", "error", resolveErr)
	}

	// Step 3b: Staleness check — fail-closed when cache is stale
	if e.cache.IsStale() {
		decision := types.NewDecision(types.EffectDefaultDeny, "policy cache stale", "")
		decision.SetAttributes(bags)
		if valErr := decision.Validate(); valErr != nil {
			return decision, oops.Wrapf(valErr, "decision validation failed")
		}
		entry := audit.Entry{
			Subject:    req.Subject,
			Action:     req.Action,
			Resource:   req.Resource,
			Effect:     types.EffectDefaultDeny,
			PolicyID:   "",
			PolicyName: "",
			DurationUS: time.Since(start).Microseconds(),
			Timestamp:  time.Now(),
		}
		if auditErr := e.audit.Log(ctx, entry); auditErr != nil {
			slog.WarnContext(ctx, "audit log failed", "error", auditErr)
		}
		RecordEvaluationMetrics(time.Since(start), decision.Effect())
		return decision, nil
	}

	// Step 4: Load snapshot and filter policies
	snap := e.cache.Snapshot()
	candidates := e.findApplicablePolicies(req, snap.Policies)

	// If no candidates, default deny
	if len(candidates) == 0 {
		decision := types.NewDecision(types.EffectDefaultDeny, "no applicable policies", "")
		decision.SetAttributes(bags)
		if valErr := decision.Validate(); valErr != nil {
			return decision, oops.Wrapf(valErr, "decision validation failed")
		}

		// Audit the decision
		entry := audit.Entry{
			Subject:    req.Subject,
			Action:     req.Action,
			Resource:   req.Resource,
			Effect:     types.EffectDefaultDeny,
			PolicyID:   "",
			PolicyName: "",
			DurationUS: time.Since(start).Microseconds(),
			Timestamp:  time.Now(),
		}
		if auditErr := e.audit.Log(ctx, entry); auditErr != nil {
			// Log error but don't fail the decision
			slog.WarnContext(ctx, "audit log failed", "error", auditErr)
		}

		return decision, nil
	}

	// Step 5: Evaluate conditions for each candidate policy
	satisfied := make([]types.PolicyMatch, 0, len(candidates))
	for _, candidate := range candidates {
		met := e.evaluatePolicy(candidate, bags)
		satisfied = append(satisfied, types.PolicyMatch{
			PolicyID:      candidate.ID,
			PolicyName:    candidate.Name,
			Effect:        candidate.Compiled.Effect.ToEffect(),
			ConditionsMet: met,
		})
	}

	// Step 6: Deny-overrides combination
	decision := e.combineDecisions(satisfied)
	decision.SetAttributes(bags)
	if err := decision.Validate(); err != nil {
		return decision, oops.Wrapf(err, "decision validation failed")
	}

	// Step 7: Audit the decision
	entry := audit.Entry{
		Subject:    req.Subject,
		Action:     req.Action,
		Resource:   req.Resource,
		Effect:     decision.Effect(),
		PolicyID:   decision.PolicyID(),
		PolicyName: policyNameFromMatches(decision.PolicyID(), decision.Policies()),
		DurationUS: time.Since(start).Microseconds(),
		Timestamp:  time.Now(),
	}
	if auditErr := e.audit.Log(ctx, entry); auditErr != nil {
		slog.WarnContext(ctx, "audit log failed", "error", auditErr)
	}

	// Record metrics
	RecordEvaluationMetrics(time.Since(start), decision.Effect())

	return decision, nil
}

// evaluatePolicy evaluates a single policy's conditions against attribute bags.
// Returns true if conditions are satisfied (or if there are no conditions).
func (e *Engine) evaluatePolicy(policy CachedPolicy, bags *types.AttributeBags) bool {
	evalCtx := &dsl.EvalContext{
		Bags:      bags,
		GlobCache: policy.Compiled.GlobCache,
	}
	return dsl.EvaluateConditions(evalCtx, policy.Compiled.Conditions)
}

// findApplicablePolicies filters policies by target matching.
// Returns only policies whose target constraints match the access request.
func (e *Engine) findApplicablePolicies(req types.AccessRequest, policies []CachedPolicy) []CachedPolicy {
	result := make([]CachedPolicy, 0, len(policies))
	for _, policy := range policies {
		if policy.Compiled == nil {
			continue
		}

		target := policy.Compiled.Target

		// Check principal type
		if target.PrincipalType != nil {
			subjectType := parseEntityType(req.Subject)
			if subjectType != *target.PrincipalType {
				continue
			}
		}

		// Check action list
		if len(target.ActionList) > 0 {
			if !contains(target.ActionList, req.Action) {
				continue
			}
		}

		// Check resource exact match (takes precedence)
		if target.ResourceExact != nil {
			if req.Resource != *target.ResourceExact {
				continue
			}
		} else if target.ResourceType != nil {
			// Check resource type only if exact match not specified
			resourceType := parseEntityType(req.Resource)
			if resourceType != *target.ResourceType {
				continue
			}
		}

		// All checks passed, include this policy
		result = append(result, policy)
	}

	return result
}

// parseEntityType extracts the type prefix from "type:id" format.
// Returns empty string if no colon found.
func parseEntityType(id string) string {
	parts := strings.SplitN(id, ":", 2)
	if len(parts) < 2 {
		return ""
	}
	return parts[0]
}

// validateRequest checks that the AccessRequest has non-empty fields and a
// well-formed subject reference. Returns an oops error with INVALID_REQUEST
// for empty fields or INVALID_ENTITY_REF for malformed subject format.
// The "system" subject is handled before this function is called.
func validateRequest(req types.AccessRequest) error {
	// Check for empty required fields
	if strings.TrimSpace(req.Subject) == "" ||
		strings.TrimSpace(req.Action) == "" ||
		strings.TrimSpace(req.Resource) == "" {
		return oops.
			Code("INVALID_REQUEST").
			Errorf("subject, action, and resource must be non-empty")
	}

	// Validate subject entity reference format: "type:id" with both parts non-empty.
	// Session subjects ("session:xxx") are valid entity refs and handled later.
	parts := strings.SplitN(req.Subject, ":", 2)
	if len(parts) < 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return oops.
			Code("INVALID_ENTITY_REF").
			With("subject", req.Subject).
			Errorf("subject must be in 'type:id' format with non-empty type and id")
	}

	return nil
}

// contains checks if a string slice contains a specific value.
func contains(slice []string, value string) bool {
	for _, item := range slice {
		if item == value {
			return true
		}
	}
	return false
}

// policyNameFromMatches looks up the policy name for the winning policy ID
// from the list of matched policies.
func policyNameFromMatches(policyID string, matches []types.PolicyMatch) string {
	if policyID == "" {
		return ""
	}
	for _, m := range matches {
		if m.PolicyID == policyID {
			return m.PolicyName
		}
	}
	return ""
}

// combineDecisions implements deny-overrides combination logic.
// Returns the final decision based on the satisfied policy matches.
func (e *Engine) combineDecisions(satisfied []types.PolicyMatch) types.Decision {
	// Scan for any deny policy with conditions met
	for _, match := range satisfied {
		if match.ConditionsMet && match.Effect == types.EffectDeny {
			decision := types.NewDecision(types.EffectDeny, "forbid policy satisfied", match.PolicyID)
			decision.SetPolicies(satisfied)
			return decision
		}
	}

	// Scan for any allow policy with conditions met
	for _, match := range satisfied {
		if match.ConditionsMet && match.Effect == types.EffectAllow {
			decision := types.NewDecision(types.EffectAllow, "permit policy satisfied", match.PolicyID)
			decision.SetPolicies(satisfied)
			return decision
		}
	}

	// No policies had conditions satisfied - default deny
	decision := types.NewDecision(types.EffectDefaultDeny, "no policies satisfied", "")
	decision.SetPolicies(satisfied)
	return decision
}
