// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package section

import (
	"context"
	"log/slog"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/pkg/errutil"
)

// PolicyEvaluator is the narrow slice of the ABAC engine this package needs.
// *policy.Engine satisfies it; the compile-time binding lives in the test file
// so a signature drift in the engine breaks the binding rather than leaving this
// package asserting an invented API.
type PolicyEvaluator interface {
	Evaluate(ctx context.Context, req types.AccessRequest) (types.Decision, error)
}

// sectionLookup resolves a section id. Injected so a test can drive a registry
// shape [validateEntries] refuses to let exist — a drifted descriptor — and so
// D-06's ordering can be pinned structurally rather than by reading this file.
type sectionLookup func(id string) (Section, bool)

// actionRank ranks the operation classes 01-SPEC §10.4 defines. A [Descriptor]'s
// Action is a section's declared MAXIMUM, so a request is admitted only when its
// rank does not exceed the declared one.
//
// It is a CLOSED ladder: an action outside it is refused rather than ranked
// zero, because an unranked action compared numerically would be admitted by
// every section.
var actionRank = map[string]int{
	ActionRead:  1,
	ActionWrite: 2,
}

// descriptorAdmits reports whether a section declaring `declared` admits a
// caller asking for `requested`. Both must be on the ladder; either being off it
// is a refusal.
func descriptorAdmits(declared, requested string) bool {
	d, declaredKnown := actionRank[declared]
	r, requestedKnown := actionRank[requested]
	if !declaredKnown || !requestedKnown {
		return false
	}
	return r <= d
}

// assertSectionAccess is the body of [AssertSectionAccess], with the registry
// lookup injected.
//
// # The ORDER is the specification (D-06: gate first, then distinguish)
//
// Step 1 is the ABAC evaluation. A denial returns IMMEDIATELY, before the
// registry is consulted, so a denied caller's refusal is identical whether or
// not the id is registered. Two distinguishable denial codes are otherwise a
// registry-enumeration oracle for any caller who can probe ids.
//
// This works precisely because seed:admin-section-access is scoped by resource
// TYPE rather than by enumerated id (§10.4, EXT-07): an UNREGISTERED id is still
// covered by the policy, so the gate can decide before the registry exists in
// the story at all. It is the same ordering §10.3 already mandates for the
// not-implemented refusal, applied one field over.
//
// The full code taxonomy is documented on [AssertSectionAccess].
func assertSectionAccess(
	ctx context.Context,
	engine PolicyEvaluator,
	playerID, sectionID, action string,
	lookup sectionLookup,
) (Section, error) {
	// --- Step 1: the gate ---
	//
	// Extracted verbatim into assertSectionAdmission because two callers in the
	// portal need step 1 ALONE: the interceptor's portal-admission check and the
	// handler's enumeration filter. Extraction, not duplication — a second copy
	// of the ABAC evaluation is a second place the ordering could drift from
	// D-06's.
	if err := assertSectionAdmission(ctx, engine, playerID, sectionID, action); err != nil {
		return Section{}, err
	}

	resource := access.AdminSectionResource(sectionID)

	// --- Step 2: reached only by a permitted caller ---
	entry, registered := lookup(sectionID)
	if !registered {
		return Section{}, oops.Code("DENY_ADMIN_SECTION_UNREGISTERED").
			With("section_id", sectionID).
			Errorf("admin section %q is not registered", sectionID)
	}

	// --- Step 3: BOTH descriptor fields decide something ---
	if entry.Descriptor.Resource != resource {
		return Section{}, oops.Code("ADMIN_SECTION_DESCRIPTOR_MISMATCH").
			With("section_id", sectionID).
			With("evaluated_resource", resource).
			With("declared_resource", entry.Descriptor.Resource).
			Errorf("section %q authorized %q but its descriptor declares %q", sectionID, resource, entry.Descriptor.Resource)
	}
	// This check sits AFTER the gate DELIBERATELY. Refusing an undeclared action
	// first would let an unauthorized caller tell a section that declares write
	// from one that does not — the same enumeration oracle D-06 closes, one
	// field over.
	if !descriptorAdmits(entry.Descriptor.Action, action) {
		return Section{}, oops.Code("ADMIN_SECTION_ACTION_NOT_DECLARED").
			With("section_id", sectionID).
			With("requested_action", action).
			With("declared_action", entry.Descriptor.Action).
			Errorf("section %q declares at most %q; caller asked for %q", sectionID, entry.Descriptor.Action, action)
	}

	// --- Step 4: §10.3's refusal, here rather than deferred to a caller ---
	available, statusErr := sectionAvailability(entry.Status)
	if statusErr != nil {
		return Section{}, oops.Code("ADMIN_SECTION_STATUS_UNKNOWN").
			With("section_id", sectionID).
			Wrap(statusErr)
	}
	if !available {
		return Section{}, oops.Code("SECTION_NOT_IMPLEMENTED").
			With("section_id", sectionID).
			Errorf("admin section %q is planned and has no handler yet", sectionID)
	}

	return entry, nil
}

// assertSectionAdmission is step 1 of the gate, alone: the ABAC answer to "may
// this player touch the admin section surface at all".
//
// It is the ORIGINAL step-1 body of [assertSectionAccess], extracted rather than
// copied, so the two surfaces cannot answer the same policy question two ways.
// Its guards run BEFORE the resource is constructed: access.AdminSectionResource
// panics on an empty id by design, and that guard exists to catch programmer
// error at construction sites, not to handle request input — a panic in a
// request path is a different failure mode from a denial.
func assertSectionAdmission(
	ctx context.Context,
	engine PolicyEvaluator,
	playerID, sectionID, action string,
) error {
	if engine == nil {
		return malformed("engine", "a gate with no engine cannot make an authorization decision")
	}
	if playerID == "" {
		return malformed("playerID", "an empty subject would bypass access control")
	}
	if action == "" {
		return malformed("action", "an empty action would build a request no policy target matches")
	}
	if sectionID == "" {
		return malformed("sectionID", "an empty section id would build a bare admin_section: reference")
	}

	resource := access.AdminSectionResource(sectionID)

	// NewAccessRequest's own emptiness checks are a second line of defence
	// behind the guards above; its error is handled rather than discarded.
	req, reqErr := types.NewAccessRequest(access.PlayerSubject(playerID), action, resource, nil)
	if reqErr != nil {
		return oops.Code("ADMIN_SECTION_REQUEST_MALFORMED").Wrap(reqErr)
	}

	decision, evalErr := engine.Evaluate(ctx, req)
	if evalErr != nil {
		errutil.LogErrorContext(ctx, "admin section: access evaluation failed", evalErr,
			"player_id", playerID, "section_id", sectionID, "action", action)
		return oops.Code("ADMIN_SECTION_EVALUATION_FAILED").
			Errorf("admin section access could not be evaluated")
	}
	if decision.IsInfraFailure() {
		errutil.LogErrorContext(ctx, "admin section: access check infrastructure failure", nil,
			"player_id", playerID, "section_id", sectionID, "action", action,
			"policy_id", decision.PolicyID(), "reason", decision.Reason())
		return oops.Code("ADMIN_SECTION_EVALUATION_FAILED").
			Errorf("admin section access could not be evaluated")
	}
	if !decision.IsAllowed() {
		// The section id is LOGGED, never returned. Per .claude/rules/grpc-errors.md
		// an inner error or a distinguishing field substituted into the refusal
		// reaches the client — and here that field IS the disclosure.
		slog.WarnContext(ctx, "admin section access denied",
			"player_id", playerID, "section_id", sectionID, "action", action,
			"reason", decision.Reason())
		return denyAdminSection()
	}
	return nil
}

// AssertSectionAdmission is the POLICY-GATE STEP ALONE, shared by the two portal
// callers that need it without the rest of [AssertSectionAccess].
//
// # Admission is not access
//
// ADMISSION answers "may this player touch the admin section surface at all" —
// one ABAC decision, nothing else. ACCESS is admission PLUS "and is this
// specific section registered, descriptor-consistent, and available".
//
//   - The interceptor's PORTAL check and the AdminListSections ENUMERATION
//     filter need admission. Access would be wrong for both: its availability
//     step (§10.3) refuses a `planned` section with SECTION_NOT_IMPLEMENTED, so
//     an enumeration written against it would silently drop all six planned
//     sections out of the nav EXT-02 and D-101 require them to appear in.
//   - A SECTION HANDLER needs access. Admission alone would let a caller act on
//     an unregistered, drifted or unimplemented section.
//
// The refusal codes are exactly [AssertSectionAccess]'s first three:
// ADMIN_SECTION_REQUEST_MALFORMED, ADMIN_SECTION_EVALUATION_FAILED and
// DENY_ADMIN_SECTION. An evaluation failure MUST NOT be flattened into a denial
// by a caller — §8.10 — and a denial carries no distinguishing field.
func AssertSectionAdmission(
	ctx context.Context,
	engine PolicyEvaluator,
	playerID, sectionID, action string,
) error {
	return assertSectionAdmission(ctx, engine, playerID, sectionID, action)
}

// denyAdminSection builds the ONE refusal every denied caller receives.
//
// It is a function, not a package-level error value, so each caller gets a fresh
// error — but its message is a STATIC constant string carrying no section id, no
// action, and no context key that could distinguish one refusal from another.
// That indistinguishability is INV-PRIVACY-11 and it is asserted differentially
// in gate_test.go, not stated here.
func denyAdminSection() error {
	return oops.Code("DENY_ADMIN_SECTION").Errorf("admin section access denied")
}

func malformed(field, why string) error {
	return oops.Code("ADMIN_SECTION_REQUEST_MALFORMED").
		With("field", field).
		Errorf("admin section request is malformed: %s", why)
}

// AssertSectionAccess is the ONE shared authorization helper every admin entry
// point calls FIRST, per 01-SPEC §10.4.
//
// It returns the registry entry — carrying its [Descriptor] — for a caller
// permitted to reach an available section, and a typed refusal otherwise. See
// [assertSectionAccess] for the ordering and its rationale.
//
// # Code taxonomy
//
//   - ADMIN_SECTION_REQUEST_MALFORMED — the call could not be evaluated at all
//     (no engine, empty player id, empty section id, empty action). Raised
//     BEFORE any engine call, so a malformed request never reaches the audit log
//     as a denial and never reaches access.AdminSectionResource's panic guard.
//   - DENY_ADMIN_SECTION — the ABAC decision denied. Static message, no section
//     id in the error, nothing that distinguishes one section from another.
//     INV-PRIVACY-11.
//   - ADMIN_SECTION_EVALUATION_FAILED — §8.10 infrastructure failure. It resolves
//     DENY and it ABORTS; it MUST NOT be flattened into an ordinary denial,
//     which would render an outage as an authorization answer.
//   - DENY_ADMIN_SECTION_UNREGISTERED — reachable ONLY by a caller the gate
//     permitted. The operator diagnostic D-06 preserves.
//   - ADMIN_SECTION_DESCRIPTOR_MISMATCH — the entry's Descriptor.Resource has
//     drifted from the resource the gate evaluated, so the gate authorized one
//     resource while the caller is about to act on another.
//   - ADMIN_SECTION_ACTION_NOT_DECLARED — the caller asked for an operation class
//     the section never declared.
//   - ADMIN_SECTION_STATUS_UNKNOWN — the entry's status is outside the closed
//     vocabulary. Denies; see [sectionAvailability].
//   - SECTION_NOT_IMPLEMENTED — a planned section, refused AFTER the gate (§10.3).
//
// # The gate is asserted ONCE, structurally, by the interceptor (D-99)
//
// This is NOT a helper each admin handler remembers to call. Every method served
// under holomush.adminportal.v1 is gated by NewAdminSectionInterceptor
// (internal/grpc/admin_interceptor.go) from the fail-closed
// [AdminDescriptors] method→section table, before the handler runs — and this
// function is what that interceptor calls for a descriptor naming a fixed
// section. A method with no declaration is refused with
// ADMIN_SECTION_NOT_DECLARED rather than defaulted, so what must not be
// forgettable is the DECLARATION, and forgetting it denies.
//
// A per-handler call would be the model D-99 replaced: it makes the gate a thing
// a future author must remember, and the one they forget is ungated with nothing
// red. Three prohibitions come with the structural form, each named in §10.4 —
// never a bare PlayerHasRole lookup (a storage query is not an authorization
// decision), never a route-guard or internal/web/ decision (UX, bypassable, and
// in the process .claude/rules/gateway-boundary.md designates
// protocol-translation-only), and never "the facade is the only caller".
//
// The gate is evaluated PER PLAYER, not per acting character (§10.5): the
// subject is access.PlayerSubject(playerID) and seed:admin-section-access reads
// principal.player.roles. A per-character gate would put two different answers
// to "is this caller an admin" over one table.
func AssertSectionAccess(
	ctx context.Context,
	engine PolicyEvaluator,
	playerID, sectionID, action string,
) (Section, error) {
	return assertSectionAccess(ctx, engine, playerID, sectionID, action, Lookup)
}
