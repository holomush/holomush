// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package section

import (
	"unicode"

	"github.com/samber/oops"
)

// MethodDescriptor is the per-METHOD authorization declaration D-99 requires:
// which section an AdminPortalService method acts on, and at what operation
// class.
//
// It is the method-level companion to [Descriptor], which declares the same two
// things for a section. The registry answers "what may a caller reach"; this
// table answers "what is this RPC asking for" — and both halves are REQUIRED
// data with no default and no zero value meaning allow.
//
// The zero value is NOT a valid entry: an empty SectionID without
// EnumeratesAllSections and an Action off the [actionRank] ladder are both
// rejected by [validateAdminDescriptors] at boot.
type MethodDescriptor struct {
	// SectionID is the registered section this method acts on. It is empty ONLY
	// on an entry carrying EnumeratesAllSections.
	SectionID string
	// Action is the operation class the method asks for, from the closed
	// ActionRead / ActionWrite ladder.
	Action string
	// EnumeratesAllSections marks the one method shape whose section is not a
	// fixed id: it reports what the caller may see across the WHOLE registry, so
	// there is no single section to name.
	//
	// It is an explicit flag rather than "SectionID == \"\" means enumerate",
	// because a sentinel spelled as an empty string is indistinguishable from a
	// forgotten field — and a forgotten field that read as "enumerate" would be
	// a method silently gated at the resource type instead of at its section.
	//
	// The interceptor does NOT skip the gate for such an entry. It evaluates
	// ADMISSION (the resource-TYPE answer) via [AssertSectionAdmission], so a
	// caller with no admin_section: access is still refused before the handler
	// runs; the per-section filtering is the handler's.
	EnumeratesAllSections bool
	// SectionFromRequest marks the one method shape whose section id is supplied
	// by the CALLER rather than fixed here: AdminGetSection takes it as a
	// request field.
	//
	// It is a third explicit flag rather than an absent SectionID for the same
	// reason EnumeratesAllSections is: a shape spelled as a missing field cannot
	// be told apart from a forgotten one, and the forgotten one must deny.
	//
	// The interceptor — not the handler — evaluates it. It extracts the id
	// through a typed accessor, refuses a missing or blank one with
	// ADMIN_SECTION_NO_SECTION_ID, calls [AssertSectionAccess] with the id
	// verbatim, and stashes the resolved [Section] on the context. So the ONE
	// RPC whose section is attacker-controlled carries no per-handler gate, which
	// is exactly the exception D-99 abolished.
	//
	// It MUST NOT fall through to the fixed-SectionID arm: that arm would call
	// AssertSectionAccess with an EMPTY id, which is refused with
	// ADMIN_SECTION_REQUEST_MALFORMED before evaluation — failing every such call
	// for every caller, admin or not.
	SectionFromRequest bool
}

// AdminDescriptors is the fail-closed method→section table for every method
// served by holomush.adminportal.v1.AdminPortalService, keyed by BARE method
// name (the ServiceDesc's MethodName, not the full wire path).
//
// A served method with NO entry here is refused with ADMIN_SECTION_NOT_DECLARED
// before any session lookup — it is never defaulted to a section. That is the
// whole point of the table: D-99 moves the gate off the individual handler, so
// the thing that must not be forgettable is the DECLARATION, and forgetting it
// denies rather than admits.
//
// Set equality against the served method set is asserted by
// TestEveryServedAdminMethodHasADescriptor, in BOTH directions: a served method
// with no entry is a fail-open, and an entry naming no served method is drift.
// A duplicate key is a Go compile error, so two descriptors cannot collide at
// runtime.
var AdminDescriptors = map[string]MethodDescriptor{
	"AdminListSections": {Action: ActionRead, EnumeratesAllSections: true},
	"AdminGetSection":   {Action: ActionRead, SectionFromRequest: true},
	// The three character READS. Each names its section as a FIXED id, which is
	// the shape D-99 wants by default: the section is a property of the method,
	// not of the request, so there is no attacker-controlled parameter for the
	// interceptor to adjudicate and no per-handler check to forget.
	"AdminListCharacters":   {SectionID: "characters", Action: ActionRead},
	"AdminSearchCharacters": {SectionID: "characters", Action: ActionRead},
	"AdminGetCharacter":     {SectionID: "characters", Action: ActionRead},
}

// LookupMethodDescriptor resolves a BARE method name to its declaration.
//
// Matching is EXACT byte equality. There is deliberately no case folding and no
// trimming: a near-miss resolving to a neighbouring entry would authorize one
// method under another method's section and action, which is precisely the
// silent mis-gate the ADMIN_SECTION_NOT_DECLARED arm exists to turn into a
// refusal. A near-miss resolves to nothing, and nothing denies.
func LookupMethodDescriptor(method string) (MethodDescriptor, bool) {
	d, ok := AdminDescriptors[method]
	return d, ok
}

// validateAdminDescriptors is the boot-time half of EXT-03 for the METHOD
// table, the peer of [validateEntries] for the registry.
//
// It is called from [validateAtBoot], so a malformed declaration ABORTS startup
// rather than being discovered by the first caller it mis-gates. A validator
// with no production call site satisfies EXT-03 in a unit test and nothing else.
func validateAdminDescriptors(descriptors map[string]MethodDescriptor) error {
	for method, d := range descriptors {
		if method == "" {
			return oops.Code("ADMIN_METHOD_DESCRIPTOR_INVALID").
				Errorf("the admin method table carries an empty method name")
		}

		if _, ranked := actionRank[d.Action]; !ranked {
			return oops.Code("ADMIN_METHOD_DESCRIPTOR_INVALID").
				With("method", method).
				With("action", d.Action).
				Errorf("method %q declares action %q, which is outside the closed ladder", method, d.Action)
		}

		// EXACTLY ONE of the three shapes, never none and never more. "None" is
		// the forgotten-field case, which §10.2 forbids reading as permissive;
		// "more than one" is a declaration whose arms contradict each other, and
		// the interceptor's shape switch would silently pick whichever it tests
		// first. Both abort the boot rather than resolving to a default.
		shapes := 0
		if d.SectionID != "" {
			shapes++
		}
		if d.EnumeratesAllSections {
			shapes++
		}
		if d.SectionFromRequest {
			shapes++
		}
		switch {
		case shapes == 0:
			return oops.Code("ADMIN_METHOD_DESCRIPTOR_INVALID").
				With("method", method).
				Errorf("method %q declares no section, does not enumerate and takes no section from the request; §10.2 forbids reading that as permissive", method)
		case shapes > 1:
			return oops.Code("ADMIN_METHOD_DESCRIPTOR_INVALID").
				With("method", method).
				With("section_id", d.SectionID).
				With("enumerates_all_sections", d.EnumeratesAllSections).
				With("section_from_request", d.SectionFromRequest).
				Errorf("method %q declares %d section shapes; exactly one is required", method, shapes)
		case d.SectionID != "":
			if _, registered := Lookup(d.SectionID); !registered {
				return oops.Code("ADMIN_METHOD_DESCRIPTOR_INVALID").
					With("method", method).
					With("section_id", d.SectionID).
					Errorf("method %q names section %q, which is not registered", method, d.SectionID)
			}
		}
	}
	return nil
}

// DisplayName is the human nav label for a section id.
//
// It is DERIVED from the id rather than stored, exactly as [entry] derives the
// descriptor's resource: a second stored label would be a second vocabulary
// that could drift from the id it belongs to. Every id §10.1 registers is a
// single lowercase-ASCII word, so capitalising the first rune is lossless.
//
// The web nav renders this value; it makes no authorization decision.
func DisplayName(id ID) string {
	s := string(id)
	if s == "" {
		return ""
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// PortalProbeSectionID is the section id the interceptor's PORTAL-ADMISSION
// check evaluates against for an [MethodDescriptor.EnumeratesAllSections]
// method. It is a PROBE, not a SCOPE.
//
// seed:admin-section-access targets the resource TYPE —
// `permit(principal is player, action in ["read","write"], resource is
// admin_section) when { "admin" in principal.player.roles }` — with
// Target.ResourceExact nil and no literal id in the DSL (pinned by
// TestSeedAdminSectionAccessIsTypeScopedAndPlayerFlavored). So the admission
// verdict is IDENTICAL for every id, and this constant selects no scope at all.
//
// A concrete id is needed only because there is no type-only resource reference
// to evaluate against: access.AdminSectionResource("") panics by design and
// ParseEntityRef requires a `type:id` pair. A concrete-id probe plus an ASSERTED
// immateriality property is the expressible form of a type-scoped evaluation
// here — and it is sound only for as long as that property holds, which is why
// TestTheAdmissionProbeIDIsImmaterialToTheVerdict asserts it rather than
// assuming it.
//
// If per-section grants ever land, that test goes RED and this probe MUST be
// replaced by a real portal grant rather than repointed at another id.
const PortalProbeSectionID ID = "characters"
