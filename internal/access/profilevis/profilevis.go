// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package profilevis performs 01-SPEC §8.5.1's conjunction over one web
// viewer: a profile attribute publishes only when BOTH the viewer clears the
// attribute's tier floor AND the underlying entity_properties row's own
// visibility / visible_to / excluded_from permits the read.
//
// # The conjunction is the caller's, never the engine's
//
// The ABAC engine combines permits DISJUNCTIVELY: combineDecisions
// (internal/access/policy/engine.go) returns the first satisfied forbid,
// otherwise the FIRST SATISFIED PERMIT. So any satisfied permit permits. The
// tier-floor family is keyed on the attribute NAME and never inspects the row.
// Dropped into the same evaluation as the row-keyed family it would be ADDITIVE
// TO that family rather than CONJUNCTIVE WITH it, and a row carrying
// visibility='private' or 'admin' would be published to every viewer that
// clears that name's floor.
//
// That is why [Evaluator.AttributeVisible] issues two evaluations and ANDs them
// in Go. A future reader looking at two Evaluate calls against one resource will
// be tempted to "optimize" them into one; doing so silently reopens the hole
// §8.5.1.1 exists to close, and the hole has no symptom of its own.
//
// # Removing term B is a normative violation, not a tuning decision
//
// §8.5.1.1 records the exact failure mode: term B issued with a viewer: subject
// against character-flavored row policies matches nothing and default-denies,
// making A AND B permanently false and every profile empty. That is fail-closed,
// so it leaks nothing — but the cheapest-looking REPAIR is to drop term B, which
// restores the additive exposure quietly, because the symptom it relieves looks
// like a bug and the hole it reopens does not. Plan 02-07 gave term B a shape
// that can match (the seed:viewer-property-* twins); the D-04 regression test in
// this package goes RED if anyone ever drops it.
//
// # What separates the two terms
//
// The ACTION token, fixed by the seeded corpus: the tier floors carry
// [ActionTierFloor], the viewer-flavored row-keyed twins carry [ActionRowKeyed].
// If both carried "read" against the same property:<id>, each evaluation would
// match BOTH families and the caller's AND would silently reduce to the additive
// shape. Nothing about the separator is decided here; it is consumed as shipped.
package profilevis

import (
	"context"
	"errors"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/pkg/errutil"
)

// Action tokens, consumed as plan 02-07 seeded them.
const (
	// ActionTierFloor is TERM A's action. Exactly the seed:profile-tier-floor-*
	// family carries it, which is what keeps term A out of term B's evaluation.
	ActionTierFloor = "read_profile_attribute"
	// ActionRowKeyed is TERM B's action — the viewer-flavored row-keyed twins.
	ActionRowKeyed = "read"
	// ActionReachable is the action of the §8.4.2 reachability evaluation. It
	// shares a spelling with [ActionRowKeyed] and nothing else: the resource
	// TYPE (profile, not property) is what separates them.
	ActionReachable = "read"
)

// Error codes, asserted with errutil.AssertErrorCode rather than by string
// matching.
const (
	// CodeEvaluationFailed marks §8.10's infrastructure failure.
	CodeEvaluationFailed = "PROFILE_VISIBILITY_EVALUATION_FAILED"
	// CodeProfileUnreachable marks §8.7's not-found-equivalent.
	CodeProfileUnreachable = "PROFILE_VISIBILITY_UNREACHABLE"
)

var (
	// ErrProfileUnreachable reports that reachability DENIED. The caller MUST
	// render §8.7's not-found-equivalent — a response indistinguishable from
	// that of a nonexistent character. It is deliberately a DIFFERENT outcome
	// from [ErrEvaluationFailed]: one is a policy answer, the other is an
	// outage, and a caller that cannot tell them apart renders an outage as a
	// missing character.
	ErrProfileUnreachable = errors.New("profile is not reachable for this viewer")

	// ErrEvaluationFailed reports §8.10's infrastructure failure: the policy
	// store is unreachable, an attribute provider errored, or the engine could
	// not evaluate. It resolves DENY and it ABORTS. It MUST NOT be swallowed
	// into "this viewer sees nothing" — that renders a profile as legitimately
	// sparse when in fact nothing was evaluated.
	ErrEvaluationFailed = errors.New("profile visibility evaluation failed")
)

// PolicyEvaluator is the narrow slice of the ABAC engine this package needs.
// *policy.Engine satisfies it; the compile-time binding lives in the test file
// so a signature drift in the engine breaks the doubles rather than leaving
// them asserting an invented API.
type PolicyEvaluator interface {
	Evaluate(ctx context.Context, req types.AccessRequest) (types.Decision, error)
}

// Property identifies one entity_properties row to evaluate: the row id that
// becomes the property:<id> resource, and the profile attribute name that keys
// the returned set.
type Property struct {
	// ID is the entity_properties row id.
	ID string
	// Name is the governed attribute name, e.g. "profile.pronouns". It is NOT
	// carried into the request: the tier-floor policies read the name from the
	// ROW, via resource.property.name, so the name a caller supplies here can
	// never widen what the policy matched.
	Name string
}

// Evaluator performs the conjunction against one default-deny ABAC engine.
type Evaluator struct {
	// Engine evaluates every term. It is required.
	Engine PolicyEvaluator
}

// Reachable answers §8.4.2's reachability question for one character with
// exactly ONE evaluation, against the profile: resource type.
//
// It is its own resource type on purpose: character:<id>/read is already
// permitted for character subjects by seed:admin-full-access and
// seed:player-character-colocation, and it means "may read the character
// entity" — a different question from "does this character's profile resolve on
// the web".
func (e *Evaluator) Reachable(ctx context.Context, viewerSubject, characterID string) (bool, error) {
	if err := e.check(ctx, viewerSubject); err != nil {
		return false, err
	}
	if characterID == "" {
		return false, malformed(ctx, "characterID", "an empty character id would build a bare profile: reference no policy target matches")
	}
	return e.evaluate(ctx, viewerSubject, ActionReachable, access.ProfileResource(characterID))
}

// AttributeVisible answers §8.5.1's conjunction for ONE row.
//
// It issues exactly TWO evaluations against the SAME property:<id> resource —
// term A with [ActionTierFloor] and term B with [ActionRowKeyed] — and ANDs
// their verdicts in Go. See this package's doc comment for why that MUST NOT be
// collapsed into a single evaluation.
//
// Both terms are ALWAYS evaluated; term A denying does not skip term B. Two
// reasons. First, it makes "exactly two evaluations" an unconditional property
// a test can pin rather than one that holds only on the permitting path.
// Second, §8.10: short-circuiting on a term-A denial would let an
// infrastructure failure in term B be reported as an ordinary "withheld",
// which is precisely the masking §8.10 forbids.
//
// attrName is not carried into either request — the policies read the name from
// the row — but it is stamped into the error context, so a caller told that an
// evaluation failed can tell WHICH attribute failed.
func (e *Evaluator) AttributeVisible(
	ctx context.Context, viewerSubject, propertyID, attrName string,
) (bool, error) {
	if err := e.check(ctx, viewerSubject); err != nil {
		return false, err
	}
	if propertyID == "" {
		return false, malformed(ctx, "propertyID", "an empty property id would build a bare property: reference no policy target matches")
	}

	resource := access.PropertyResource(propertyID)

	clearsFloor, errA := e.evaluate(ctx, viewerSubject, ActionTierFloor, resource)
	rowPermits, errB := e.evaluate(ctx, viewerSubject, ActionRowKeyed, resource)

	if err := errors.Join(errA, errB); err != nil {
		return false, oops.Code(CodeEvaluationFailed).
			With("attribute", attrName).
			With("property_id", propertyID).
			Wrap(err)
	}

	return clearsFloor && rowPermits, nil
}

// VisibleAttributes returns the subset of properties this viewer may see, keyed
// by attribute name.
//
// Reachability is evaluated FIRST and INDEPENDENTLY (§8.4.2). A DENY returns
// [ErrProfileUnreachable] and NO per-attribute evaluation runs. Reachability is
// never derived from "did any field clear its floor": under §8.6's seeded
// defaults profile.pronouns sits at the anonymous floor, so something always
// clears, which would pin reachability permanently at anonymous, make §8.7's
// not-found-equivalent unfireable, and leave INV-PRIVACY-9 binding in Phase 4
// to a gate that cannot deny.
//
// Any evaluation failure ABORTS the whole call and returns a nil map, following
// world.Service.ListPropertiesByParent's three-outcome shape (permit appends,
// policy denial filters silently, infra failure aborts). A partially-populated
// set is the ghost-data shape that third branch exists to prevent.
//
// The returned set does not depend on the order of properties.
func (e *Evaluator) VisibleAttributes(
	ctx context.Context, viewerSubject, characterID string, properties []Property,
) (map[string]Property, error) {
	reachable, err := e.Reachable(ctx, viewerSubject, characterID)
	if err != nil {
		return nil, err
	}
	if !reachable {
		return nil, oops.Code(CodeProfileUnreachable).
			With("character_id", characterID).
			Wrap(ErrProfileUnreachable)
	}

	visible := make(map[string]Property, len(properties))
	for _, prop := range properties {
		ok, attrErr := e.AttributeVisible(ctx, viewerSubject, prop.ID, prop.Name)
		if attrErr != nil {
			return nil, attrErr
		}
		if ok {
			visible[prop.Name] = prop
		}
	}
	return visible, nil
}

// evaluate runs one request and collapses the engine's answer into the three
// outcomes world.Service.checkAccess distinguishes: permit, policy denial, and
// infrastructure failure.
//
// The third outcome arrives in TWO shapes and both must be caught. A non-nil
// error is the obvious one. The subtle one is a DENY decision carrying an
// "infra:"-prefixed policy id and a NIL error — the engine's degraded-mode and
// session-resolution paths return exactly that. Treating it as an ordinary
// denial is §8.10's forbidden masking.
func (e *Evaluator) evaluate(ctx context.Context, subject, action, resource string) (bool, error) {
	req, reqErr := types.NewAccessRequest(subject, action, resource, nil)
	if reqErr != nil {
		// Defensive: every call site above builds subject and resource through
		// the typed access.* constructors, which panic on empty input, and the
		// actions are constants. Kept as defense in depth.
		errutil.LogErrorContext(ctx, "profile visibility: invalid access request",
			reqErr, "subject", subject, "action", action, "resource", resource)
		return false, oops.Code(CodeEvaluationFailed).
			Wrap(errors.Join(ErrEvaluationFailed, reqErr))
	}

	decision, evalErr := e.Engine.Evaluate(ctx, req)
	if evalErr != nil {
		errutil.LogErrorContext(ctx, "profile visibility: access evaluation failed",
			evalErr, "subject", subject, "action", action, "resource", resource)
		return false, oops.Code(CodeEvaluationFailed).
			With("action", action).
			With("resource", resource).
			Wrap(errors.Join(ErrEvaluationFailed, evalErr))
	}

	if decision.IsAllowed() {
		return true, nil
	}

	if decision.IsInfraFailure() {
		errutil.LogErrorContext(ctx, "profile visibility: access check infrastructure failure",
			ErrEvaluationFailed,
			"policy_id", decision.PolicyID(), "reason", decision.Reason(),
			"subject", subject, "action", action, "resource", resource)
		return false, oops.Code(CodeEvaluationFailed).
			With("action", action).
			With("resource", resource).
			With("policy_id", decision.PolicyID()).
			With("reason", decision.Reason()).
			Wrap(ErrEvaluationFailed)
	}

	return false, nil
}

// check rejects a call that could not be evaluated meaningfully, BEFORE any
// engine call, so a malformed request never reaches the audit log as a denial.
func (e *Evaluator) check(ctx context.Context, viewerSubject string) error {
	if e.Engine == nil {
		return malformed(ctx, "Engine", "an Evaluator with no engine cannot make an authorization decision")
	}
	if viewerSubject == "" {
		return malformed(ctx, "viewerSubject", "an empty subject would bypass access control")
	}
	return nil
}

// malformed builds — and LOGS — a CodeEvaluationFailed for a call that could not
// be evaluated at all.
//
// It logs at the ORIGIN, like every other failure branch in this file, because
// its callers are contracted to treat a CodeEvaluationFailed as already-logged
// and classify it without logging again. Leaving this one shape silent would
// make an impossible-by-construction outage the only one nobody can see.
func malformed(ctx context.Context, field, why string) error {
	err := oops.Code(CodeEvaluationFailed).
		With("field", field).
		Wrap(errors.Join(ErrEvaluationFailed, errors.New(why)))
	errutil.LogErrorContext(ctx, "profile visibility: request could not be evaluated", err)
	return err
}
