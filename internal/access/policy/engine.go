// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/dsl"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/audit"
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
		slog.Error(
			"ABAC engine entering degraded mode — all requests will be denied",
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
			slog.ErrorContext(
				ctx, "system subject used without system context (S1 violation)",
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

		event := audit.Event{
			ID:         "",
			Name:       "",
			Source:     audit.SourceSystem,
			Component:  "abac",
			Subject:    req.Subject,
			Action:     req.Action,
			Resource:   req.Resource,
			Effect:     types.EffectSystemBypass,
			DurationUS: time.Since(start).Microseconds(),
			Timestamp:  time.Now(),
		}
		if err := e.audit.Log(ctx, event); err != nil {
			slog.WarnContext(ctx, "audit log failed", "error", err)
			audit.RecordEngineAuditFailure()
		}

		return decision, nil
	}

	// Step 3: Degraded mode check — AFTER system bypass so system ops still work
	if e.degraded.Load() {
		slog.ErrorContext(
			ctx, "CRITICAL: ABAC engine in degraded mode — denying all requests",
			"subject", req.Subject,
			"action", req.Action,
			"resource", req.Resource,
		)
		decision := types.NewDecision(types.EffectDefaultDeny, "degraded_mode", "infra:degraded-mode")
		event := audit.Event{
			ID:         "infra:degraded-mode",
			Name:       "",
			Source:     audit.SourceEngine,
			Component:  "abac",
			Subject:    req.Subject,
			Action:     req.Action,
			Resource:   req.Resource,
			Effect:     types.EffectDefaultDeny,
			DurationUS: time.Since(start).Microseconds(),
			Timestamp:  time.Now(),
		}
		if auditErr := e.audit.Log(ctx, event); auditErr != nil {
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
				slog.DebugContext(
					ctx, "session invalid during resolution",
					"session_id", sessionID,
					"error", err,
				)
				decision = types.NewDecision(types.EffectDefaultDeny, "session invalid", "infra:session-invalid")
			} else {
				errutil.LogErrorContext(
					ctx, "session resolution failed",
					err, "session_id", sessionID,
				)
				decision = types.NewDecision(types.EffectDefaultDeny, "session store error", "infra:session-store-error")
			}

			if valErr := decision.Validate(); valErr != nil {
				return decision, oops.Wrapf(valErr, "decision validation failed")
			}
			event := audit.Event{
				ID:         decision.PolicyID(),
				Name:       "",
				Source:     audit.SourceEngine,
				Component:  "abac",
				Subject:    req.Subject,
				Action:     req.Action,
				Resource:   req.Resource,
				Effect:     types.EffectDefaultDeny,
				DurationUS: time.Since(start).Microseconds(),
				Timestamp:  time.Now(),
			}
			if auditErr := e.audit.Log(ctx, event); auditErr != nil {
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
		errutil.LogErrorContext(
			ctx, "attribute resolution failed — fail-closed",
			resolveErr,
			"subject", req.Subject,
			"action", req.Action,
			"resource", req.Resource,
		)
		event := audit.Event{
			ID:         "infra:attribute-resolution-failed",
			Name:       "",
			Source:     audit.SourceEngine,
			Component:  "abac",
			Subject:    req.Subject,
			Action:     req.Action,
			Resource:   req.Resource,
			Effect:     types.EffectDefaultDeny,
			DurationUS: time.Since(start).Microseconds(),
			Timestamp:  time.Now(),
		}
		if auditErr := e.audit.Log(ctx, event); auditErr != nil {
			slog.WarnContext(ctx, "audit log failed", "error", auditErr)
			audit.RecordEngineAuditFailure()
		}
		return types.NewDecision(types.EffectDefaultDeny, "attribute resolution failed", "infra:attribute-resolution"),
			oops.With("subject", req.Subject).With("action", req.Action).With("resource", req.Resource).Wrap(resolveErr)
	}

	// Defense-in-depth: reject reserved attribute keys even if the caller
	// bypassed NewAccessRequest by constructing an AccessRequest literal.
	// Same code and semantics as the NewAccessRequest precondition check.
	for k := range req.Attributes {
		if types.IsReservedActionKey(k) {
			return types.Decision{}, oops.Code("ACCESS_REQUEST_RESERVED_ATTRIBUTE").
				With("key", k).
				Errorf("hand-built AccessRequest contained reserved attribute key %q (bypassed NewAccessRequest)", k)
		}
	}

	// Decision 6 R3: caller-supplied per-call attributes overlay bags.Action.
	// Caller wins on non-reserved key conflict. Reserved keys (currently
	// "name", which the attribute Resolver writes — see Resolver.Resolve)
	// are blocked at NewAccessRequest precondition. bags.Action is always
	// non-nil after Resolver.Resolve allocates it; the nil-init below is a
	// guard for test paths that hand-construct AttributeBags{} with zero-value maps.
	if len(req.Attributes) > 0 {
		if bags.Action == nil {
			bags.Action = make(map[string]any, len(req.Attributes))
		}
		for k, v := range req.Attributes {
			bags.Action[k] = v
		}
	}

	// Step 7: Load snapshot and filter policies
	snap, snapErr := e.cache.Snapshot(ctx)
	if snapErr != nil {
		return types.NewDecision(types.EffectDefaultDeny, "policy cache unavailable", "infra:cache"),
			oops.With("subject", req.Subject).With("action", req.Action).With("resource", req.Resource).Wrap(snapErr)
	}
	candidates := e.findApplicablePolicies(req, snap.Policies)

	if len(candidates) == 0 {
		decision := types.NewDecision(types.EffectDefaultDeny, "no applicable policies", "")
		decision.SetAttributes(bags)
		if valErr := decision.Validate(); valErr != nil {
			return decision, oops.Wrapf(valErr, "decision validation failed")
		}

		event := audit.Event{
			ID:         "",
			Name:       "",
			Source:     audit.SourceEngine,
			Component:  "abac",
			Subject:    req.Subject,
			Action:     req.Action,
			Resource:   req.Resource,
			Effect:     types.EffectDefaultDeny,
			DurationUS: time.Since(start).Microseconds(),
			Timestamp:  time.Now(),
		}
		if auditErr := e.audit.Log(ctx, event); auditErr != nil {
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
	event := audit.Event{
		ID:         decision.PolicyID(),
		Name:       policyNameFromMatches(decision.PolicyID(), decision.Policies()),
		Source:     audit.SourceEngine,
		Component:  "abac",
		Subject:    req.Subject,
		Action:     req.Action,
		Resource:   req.Resource,
		Effect:     decision.Effect(),
		DurationUS: time.Since(start).Microseconds(),
		Timestamp:  time.Now(),
	}
	if auditErr := e.audit.Log(ctx, event); auditErr != nil {
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

// CanPerformAction performs a type-level pre-flight check: it evaluates whether
// the subject could potentially perform an action on a resource TYPE without
// requiring a specific resource instance.
//
// Resolves only subject, environment, and action attributes via
// ResolveSubjectAttributes. Resource providers are NEVER invoked during
// preflight — there is no resource instance to resolve at type level.
// Permits whose conditions reference resource attributes are handled via
// the optimistic-permit branch below: they are treated as "may apply,
// instance-level Evaluate will enforce the full condition."
//
// This is fail-closed: degraded mode and context cancellation both return false.
// If any forbid policy with satisfied conditions matches, returns false.
// If any permit policy with satisfied conditions matches, returns true.
// No match → default deny (false, nil).
func (e *Engine) CanPerformAction(ctx context.Context, subject, action, resourceType, _ string) (bool, error) {
	// Step 1: Context cancellation check
	if err := ctx.Err(); err != nil {
		return false, oops.Wrapf(err, "context cancelled before CanPerformAction")
	}

	// Step 2: Degraded mode → fail-closed with sentinel error so callers
	// can distinguish infra failure from normal deny.
	if e.degraded.Load() {
		return false, types.ErrEngineDegraded
	}

	// Step 3: Validate subject format "type:id"
	parts := strings.SplitN(subject, ":", 2)
	if len(parts) < 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return false, oops.
			Code("INVALID_ENTITY_REF").
			With("subject", subject).
			Errorf("subject must be in 'type:id' format with non-empty type and id")
	}

	// Step 4: Resolve subject + environment attributes only. No resource
	// instance exists at type-level preflight; resource providers are
	// never called. The optimistic-permit branch below handles permits
	// whose conditions reference resource attributes.
	bags, resolveErr := e.resolver.ResolveSubjectAttributes(ctx, subject, action)
	if resolveErr != nil {
		errutil.LogErrorContext(
			ctx, "CanPerformAction: attribute resolution failed — fail-closed",
			resolveErr,
			"subject", subject,
			"action", action,
			"resourceType", resourceType,
		)
		return false, oops.
			With("subject", subject).
			With("action", action).
			With("resourceType", resourceType).
			Wrap(resolveErr)
	}

	// Step 5: Get compiled policies from the cache snapshot
	snap, snapErr := e.cache.Snapshot(ctx)
	if snapErr != nil {
		return false, oops.With("subject", subject).With("action", action).With("resourceType", resourceType).Wrap(snapErr)
	}

	// Step 6: Filter policies by principal type, action, and resource TYPE only.
	// We match on resource type, not exact resource (since we have no instance).
	subjectType := parts[0]
	var candidates []CachedPolicy
	for _, policy := range snap.Policies {
		if policy.Compiled == nil {
			continue
		}
		target := policy.Compiled.Target

		// Filter by principal type
		if target.PrincipalType != nil && *target.PrincipalType != subjectType {
			continue
		}

		// Filter by action
		if len(target.ActionList) > 0 && !contains(target.ActionList, action) {
			continue
		}

		// Filter by resource type only (skip exact-resource-only policies)
		if target.ResourceExact != nil {
			// This policy targets a specific resource instance — skip it for
			// type-level pre-flight since we have no instance to compare.
			continue
		}
		if target.ResourceType != nil && *target.ResourceType != resourceType {
			continue
		}

		candidates = append(candidates, policy)
	}

	// Step 7 & 8: Evaluate conditions using subject attributes only.
	// Resource attributes in bags will be empty (preflight resource doesn't exist).
	//
	// Per the spec, policies with resource-specific conditions are treated
	// optimistically for permit (the handler-level ABAC will do the instance
	// check) but conservatively for forbid (unconditional forbids still deny).
	//
	// Strategy:
	//   - If conditions pass with subject attrs only → definite match
	//   - If conditions fail AND policy references resource attrs → optimistic
	//     (conditions may depend on resource state we can't check here)
	//   - If conditions fail AND policy only uses subject attrs → definitive
	//     failure (subject doesn't have the required attributes)
	anyForbid := false
	anyPermit := false
	for _, candidate := range candidates {
		evalCtx := &dsl.EvalContext{
			Bags:      bags,
			GlobCache: candidate.Compiled.GlobCache,
		}
		conditionsMet := dsl.EvaluateConditions(evalCtx, candidate.Compiled.Conditions)
		effect := candidate.Compiled.Effect.ToEffect()

		if conditionsMet {
			// Conditions satisfied with available attributes — definite match.
			switch effect {
			case types.EffectDeny:
				anyForbid = true
			case types.EffectAllow:
				anyPermit = true
			}
		} else if effect == types.EffectAllow && dsl.ReferencesResourceAttrs(candidate.Compiled.Conditions) {
			// Permit policy with unmet conditions that reference resource
			// attributes we can't evaluate without a specific instance.
			// Treat as optimistic match — the handler's instance-level
			// ABAC will enforce the full condition.
			anyPermit = true
		}
		// Forbid with unmet conditions: not applied. Subject-only permit
		// with unmet conditions: not applied (subject lacks required attrs).
	}

	// Step 8: Forbid overrides permit
	if anyForbid {
		return false, nil
	}

	// Step 9: Permit without forbid
	if anyPermit {
		return true, nil
	}

	// Step 10: No matches → default deny
	return false, nil
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
