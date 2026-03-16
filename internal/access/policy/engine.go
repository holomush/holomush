// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/audit"
	"github.com/holomush/holomush/internal/access/policy/dsl"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/pkg/errutil"
)

// Engine implements types.AccessPolicyEngine.
type Engine struct {
	resolver *attribute.Resolver
	cache    *Cache
	sessions SessionResolver
	audit    *audit.Logger
	degraded atomic.Bool
}

// Compile-time check that Engine implements AccessPolicyEngine.
var _ types.AccessPolicyEngine = (*Engine)(nil)

// degradedCount tracks how many Engine instances are in degraded mode process-wide.
// The gauge reflects degradedCount > 0, ensuring accuracy when multiple engines exist.
var degradedCount atomic.Int32

// EnterDegradedMode puts the engine into degraded mode.
// All subsequent Evaluate() calls return EffectDefaultDeny until cleared.
// Idempotent: repeated calls are no-ops.
func (e *Engine) EnterDegradedMode(reason string) {
	if e.degraded.CompareAndSwap(false, true) {
		degradedCount.Add(1)
		degradedModeGauge.Set(1)
		slog.Error("ABAC engine entering degraded mode — all requests will be denied",
			"reason", reason,
		)
	}
}

// ClearDegradedMode restores normal engine operation.
// Idempotent: repeated calls are no-ops.
func (e *Engine) ClearDegradedMode() {
	if e.degraded.CompareAndSwap(true, false) {
		count := degradedCount.Add(-1)
		if count <= 0 {
			degradedModeGauge.Set(0)
		}
		slog.Info("ABAC engine degraded mode cleared — normal evaluation resumed")
	}
}

// IsDegraded returns true if the engine is in degraded mode.
func (e *Engine) IsDegraded() bool {
	return e.degraded.Load()
}

// OnPolicyCorruption handles detection of a corrupted policy during cache reload.
// Forbid policies trigger degraded mode; permit policies are auto-disabled.
func (e *Engine) OnPolicyCorruption(policyID string, effect types.PolicyEffect) {
	switch effect {
	case types.PolicyEffectForbid:
		e.EnterDegradedMode(fmt.Sprintf("corrupted forbid policy: %s", policyID))
	case types.PolicyEffectPermit:
		slog.Error("corrupted permit policy auto-disabled",
			"policy_id", policyID,
		)
	}
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
// This implementation covers Steps 1-10 (full evaluation algorithm).
func (e *Engine) Evaluate(ctx context.Context, req types.AccessRequest) (types.Decision, error) {
	start := time.Now()

	// Step 1: Context cancellation check
	if err := ctx.Err(); err != nil {
		return types.NewDecision(types.EffectDefaultDeny, "context cancelled", "infra:context-cancelled"),
			oops.Wrapf(err, "context cancelled before evaluation")
	}

	// Step 2: System bypass — defense-in-depth (S1)
	if req.Subject == "system" {
		if !access.IsSystemContext(ctx) {
			slog.ErrorContext(ctx, "system subject used without system context (S1 violation)",
				"action", req.Action,
				"resource", req.Resource,
			)
			return types.Decision{},
				oops.Code("SYSTEM_SUBJECT_REJECTED").Errorf("system subject is only allowed from system context")
		}
		decision := types.NewDecision(types.EffectSystemBypass, "system bypass", "")
		if err := decision.Validate(); err != nil {
			return decision, oops.Wrapf(err, "decision validation failed")
		}

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
			slog.WarnContext(ctx, "audit log failed", "error", err)
			audit.RecordEngineAuditFailure()
		}

		return decision, nil
	}

	// Step 3: Degraded mode check — AFTER system bypass so system ops still work
	if e.degraded.Load() {
		slog.ErrorContext(ctx, "CRITICAL: ABAC engine in degraded mode — denying all requests",
			"subject", req.Subject,
			"action", req.Action,
			"resource", req.Resource,
		)
		decision := types.NewDecision(types.EffectDefaultDeny, "degraded_mode", "infra:degraded-mode")
		entry := audit.Entry{
			Subject:    req.Subject,
			Action:     req.Action,
			Resource:   req.Resource,
			Effect:     types.EffectDefaultDeny,
			PolicyID:   "infra:degraded-mode",
			PolicyName: "",
			DurationUS: time.Since(start).Microseconds(),
			Timestamp:  time.Now(),
		}
		if auditErr := e.audit.Log(ctx, entry); auditErr != nil {
			slog.WarnContext(ctx, "audit log failed", "error", auditErr)
			audit.RecordEngineAuditFailure()
		}
		RecordEvaluationMetrics(time.Since(start), decision.Effect())
		return decision, nil
	}

	// Step 4: Input validation — reject empty fields
	if err := validateRequest(req); err != nil {
		return types.Decision{}, err
	}

	// Step 5: Session resolution
	if strings.HasPrefix(req.Subject, "session:") {
		sessionID := strings.TrimPrefix(req.Subject, "session:")
		characterID, err := e.sessions.ResolveSession(ctx, sessionID)
		if err != nil {
			oopsErr, isOops := oops.AsOops(err)
			var decision types.Decision
			code, isStr := oopsErr.Code().(string)
			if isOops && isStr && code == "SESSION_INVALID" {
				slog.DebugContext(ctx, "session invalid during resolution",
					"session_id", sessionID,
					"error", err,
				)
				decision = types.NewDecision(types.EffectDefaultDeny, "session invalid", "infra:session-invalid")
			} else {
				errutil.LogErrorContext(ctx, "session resolution failed",
					err, "session_id", sessionID,
				)
				decision = types.NewDecision(types.EffectDefaultDeny, "session store error", "infra:session-store-error")
			}

			if valErr := decision.Validate(); valErr != nil {
				return decision, oops.Wrapf(valErr, "decision validation failed")
			}
			entry := audit.Entry{
				Subject:    req.Subject,
				Action:     req.Action,
				Resource:   req.Resource,
				Effect:     types.EffectDefaultDeny,
				PolicyID:   decision.PolicyID(),
				PolicyName: "",
				DurationUS: time.Since(start).Microseconds(),
				Timestamp:  time.Now(),
			}
			if auditErr := e.audit.Log(ctx, entry); auditErr != nil {
				slog.WarnContext(ctx, "audit log failed", "error", auditErr)
				audit.RecordEngineAuditFailure()
			}

			RecordEvaluationMetrics(time.Since(start), decision.Effect())
			return decision, nil
		}

		req.Subject = access.CharacterSubject(characterID)
	}

	// Step 6: Eager attribute resolution — fail-closed on provider errors.
	bags, resolveErr := e.resolver.Resolve(ctx, req)
	if resolveErr != nil {
		errutil.LogErrorContext(ctx, "attribute resolution failed — fail-closed",
			resolveErr,
			"subject", req.Subject,
			"action", req.Action,
			"resource", req.Resource,
		)
		entry := audit.Entry{
			Subject:    req.Subject,
			Action:     req.Action,
			Resource:   req.Resource,
			Effect:     types.EffectDefaultDeny,
			PolicyID:   "infra:attribute-resolution-failed",
			PolicyName: "",
			DurationUS: time.Since(start).Microseconds(),
			Timestamp:  time.Now(),
		}
		if auditErr := e.audit.Log(ctx, entry); auditErr != nil {
			slog.WarnContext(ctx, "audit log failed", "error", auditErr)
			audit.RecordEngineAuditFailure()
		}
		return types.NewDecision(types.EffectDefaultDeny, "attribute resolution failed", "infra:attribute-resolution"),
			oops.With("subject", req.Subject).With("action", req.Action).With("resource", req.Resource).Wrap(resolveErr)
	}

	// Step 6b: Staleness check — fail-closed when cache is stale
	if e.cache.IsStale() {
		slog.WarnContext(ctx, "policy cache stale — denying request fail-closed",
			"subject", req.Subject,
			"action", req.Action,
			"resource", req.Resource,
		)
		decision := types.NewDecision(types.EffectDefaultDeny, "policy cache stale", "infra:policy-cache-stale")
		decision.SetAttributes(bags)
		if valErr := decision.Validate(); valErr != nil {
			return decision, oops.Wrapf(valErr, "decision validation failed")
		}
		entry := audit.Entry{
			Subject:    req.Subject,
			Action:     req.Action,
			Resource:   req.Resource,
			Effect:     types.EffectDefaultDeny,
			PolicyID:   "infra:policy-cache-stale",
			PolicyName: "",
			DurationUS: time.Since(start).Microseconds(),
			Timestamp:  time.Now(),
		}
		if auditErr := e.audit.Log(ctx, entry); auditErr != nil {
			slog.WarnContext(ctx, "audit log failed", "error", auditErr)
			audit.RecordEngineAuditFailure()
		}
		RecordEvaluationMetrics(time.Since(start), decision.Effect())
		return decision, nil
	}

	// Step 7: Load snapshot and filter policies
	snap := e.cache.Snapshot()
	candidates := e.findApplicablePolicies(req, snap.Policies)

	if len(candidates) == 0 {
		decision := types.NewDecision(types.EffectDefaultDeny, "no applicable policies", "")
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
			audit.RecordEngineAuditFailure()
		}

		return decision, nil
	}

	// Step 8: Evaluate conditions for each candidate policy
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

	// Step 9: Deny-overrides combination
	decision := e.combineDecisions(satisfied)
	decision.SetAttributes(bags)
	if err := decision.Validate(); err != nil {
		return decision, oops.Wrapf(err, "decision validation failed")
	}

	// Step 10: Audit the decision
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
		audit.RecordEngineAuditFailure()
	}

	RecordEvaluationMetrics(time.Since(start), decision.Effect())

	return decision, nil
}

func (e *Engine) evaluatePolicy(policy CachedPolicy, bags *types.AttributeBags) bool {
	evalCtx := &dsl.EvalContext{
		Bags:      bags,
		GlobCache: policy.Compiled.GlobCache,
	}
	return dsl.EvaluateConditions(evalCtx, policy.Compiled.Conditions)
}

func (e *Engine) findApplicablePolicies(req types.AccessRequest, policies []CachedPolicy) []CachedPolicy {
	result := make([]CachedPolicy, 0, len(policies))
	for _, policy := range policies {
		if policy.Compiled == nil {
			continue
		}

		target := policy.Compiled.Target

		if target.PrincipalType != nil {
			subjectType := parseEntityType(req.Subject)
			if subjectType != *target.PrincipalType {
				continue
			}
		}

		if len(target.ActionList) > 0 {
			if !contains(target.ActionList, req.Action) {
				continue
			}
		}

		if target.ResourceExact != nil {
			if req.Resource != *target.ResourceExact {
				continue
			}
		} else if target.ResourceType != nil {
			resourceType := parseEntityType(req.Resource)
			if resourceType != *target.ResourceType {
				continue
			}
		}

		result = append(result, policy)
	}

	return result
}

func parseEntityType(id string) string {
	parts := strings.SplitN(id, ":", 2)
	if len(parts) < 2 {
		return ""
	}
	return parts[0]
}

func validateRequest(req types.AccessRequest) error {
	if strings.TrimSpace(req.Subject) == "" ||
		strings.TrimSpace(req.Action) == "" ||
		strings.TrimSpace(req.Resource) == "" {
		return oops.
			Code("INVALID_REQUEST").
			Errorf("subject, action, and resource must be non-empty")
	}

	parts := strings.SplitN(req.Subject, ":", 2)
	if len(parts) < 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return oops.
			Code("INVALID_ENTITY_REF").
			With("subject", req.Subject).
			Errorf("subject must be in 'type:id' format with non-empty type and id")
	}

	return nil
}

func contains(slice []string, value string) bool {
	for _, item := range slice {
		if item == value {
			return true
		}
	}
	return false
}

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

func (e *Engine) combineDecisions(satisfied []types.PolicyMatch) types.Decision {
	for _, match := range satisfied {
		if match.ConditionsMet && match.Effect == types.EffectDeny {
			decision := types.NewDecision(types.EffectDeny, "forbid policy satisfied", match.PolicyID)
			decision.SetPolicies(satisfied)
			return decision
		}
	}

	for _, match := range satisfied {
		if match.ConditionsMet && match.Effect == types.EffectAllow {
			decision := types.NewDecision(types.EffectAllow, "permit policy satisfied", match.PolicyID)
			decision.SetPolicies(satisfied)
			return decision
		}
	}

	decision := types.NewDecision(types.EffectDefaultDeny, "no policies satisfied", "")
	decision.SetPolicies(satisfied)
	return decision
}
