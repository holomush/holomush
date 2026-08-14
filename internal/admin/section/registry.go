// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package section is the CORE-SIDE admin section registry of 01-SPEC §10.1-10.4
// and the shared authorization helper every admin entry point calls first.
//
// # Why the registry lives here and not in web/src/
//
// The nav the web draws is DERIVED from this registry. A registry that lives
// only in web/src/ is a client-side artifact that cannot gate a server-side
// decision. web/src/lib/nav/sections.ts supplies the SHAPE — an ordered literal
// whose id union is the exhaustive key type for any per-section map — not the
// location.
//
// # The validation API is package-level, and that is deliberate
//
// There is NO Registry type. [Validate] is exactly validateEntries(all): every
// rule lives in the unexported validateEntries, which takes a SLICE, so a test
// can hand it a deliberately malformed set without mutating package state or
// inventing a constructor. "Add a Registry type for testability" is the
// obvious-looking refactor here and it buys nothing — the injection surface
// already exists.
//
// # Two closed vocabularies, both fail-closed
//
// [Descriptor] and [Status] are each a CLOSED set with no default and no zero
// value meaning allow, enforced twice over: [Validate] refuses to start on a
// malformed registry, and [sectionAvailability]'s exhaustive switch DENIES on
// its default arm so a malformed value cannot be served even if one reaches a
// request. Neither half is redundant — see [sectionAvailability].
package section

import (
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
)

// Action tokens, per 01-SPEC §10.4: `read` to reach a section, `write` for a
// mutation within it. Both are evaluated against the section resource.
const (
	// ActionRead is the operation class needed to REACH a section.
	ActionRead = "read"
	// ActionWrite is the operation class needed to MUTATE within a section.
	ActionWrite = "write"
)

// ID is a registered admin-section id — the first cell of a 01-SPEC §10.1 row.
type ID string

// Status is a section's implementation state. It is a CLOSED two-value
// vocabulary: [StatusAvailable] and [StatusPlanned], and nothing else.
//
// It is a bare string type, so the zero value and a misspelled value both
// COMPILE. That is exactly why both a boot rule (in [validateEntries]) and a
// denying request-time classifier ([sectionAvailability]) exist.
type Status string

const (
	// StatusAvailable marks a section whose handlers serve real content.
	StatusAvailable Status = "available"
	// StatusPlanned marks a section that is gated and refuses AFTER the gate
	// with a not-implemented answer (§10.3).
	StatusPlanned Status = "planned"
)

// Descriptor is 01-SPEC §10.2's mandatory authorization descriptor.
//
// It is a REQUIRED FIELD of the entry, not a lookup in a parallel table that can
// be missing a key. There is no default and no "if unset, use admin". An empty
// Action, an empty Resource, or the zero-valued struct is REJECTED, never read
// as permissive — the same fail-open shape §8.6's totality rule forbids for
// visibility floors and §4.3 forbids for lifecycle reads.
//
// Both fields participate in authorization at request time; see
// AssertSectionAccess. A descriptor validated at boot and never consulted again
// is mandatory data that decides nothing, which is how a required field quietly
// becomes decoration.
type Descriptor struct {
	// Action is the section's declared MAXIMUM operation class. A caller asking
	// for more than this is refused.
	Action string
	// Resource is the admin_section: reference the gate evaluates. It is DERIVED
	// from the entry id, never written as a literal.
	Resource string
}

// Section is one registry entry.
type Section struct {
	// ID is the §10.1 section id.
	ID ID
	// Status is the closed-vocabulary implementation state.
	Status Status
	// Descriptor is §10.2's mandatory authorization descriptor.
	Descriptor Descriptor
}

// all is 01-SPEC §10.1's table, in its row order. Seven ids, one available and
// six planned. There is no eighth, and a PR adding one adds a §10.1 row in the
// same change — TestTheRegistryIDSetEqualsTheSevenIDsSpec101Enumerates is RED
// otherwise, in both directions.
var all = []Section{
	// `characters` is the ONE section declaring ActionWrite, and it declares it
	// because v0.13 phase-06 ships three write RPCs under it (§10.4, §10.6).
	//
	// A section's Descriptor.Action is its declared MAXIMUM, and
	// assertSectionAccess step 3 refuses a request whose rank exceeds it — so a
	// write RPC under a read-declaring section is refused with
	// ADMIN_SECTION_ACTION_NOT_DECLARED no matter what its METHOD descriptor
	// says and no matter what policy permits. The two declarations are
	// independent gates and BOTH must admit write: AdminDescriptors names the
	// action a METHOD asks for, this names the maximum a SECTION offers.
	//
	// The six planned sections stay at ActionRead deliberately. Widening one
	// before it has a write surface would declare an operation class nothing
	// implements, and the declared maximum is what a future reviewer reads as
	// the section's intended blast radius.
	writeEntry("characters", StatusAvailable),
	entry("stats"),
	entry("players"),
	entry("moderation"),
	entry("audit"),
	entry("config"),
	entry("plugins"),
}

// entry builds a registry row, DERIVING the descriptor's resource from the id
// via access.AdminSectionResource rather than writing the prefix literally, so
// the descriptor cannot drift from the id it belongs to by a typo.
//
// [validateEntries] re-derives and compares it anyway: a future hand-written
// literal is CAUGHT rather than trusted.
// entry builds a PLANNED, read-only registry row.
//
// It takes no status parameter because every remaining caller is a planned
// section: `characters` is the one available row and it goes through writeEntry.
// A status parameter that only ever receives StatusPlanned reads as a choice
// nobody is making.
func entry(id ID) Section {
	return sectionEntry(id, StatusPlanned, ActionRead)
}

// writeEntry builds a registry row whose declared MAXIMUM action is write.
//
// It is a separate constructor rather than a parameter on entry so the six
// read-only rows above cannot acquire write by a one-character edit, and so the
// single write-declaring section is visible at the call site rather than in a
// trailing argument.
func writeEntry(id ID, status Status) Section {
	return sectionEntry(id, status, ActionWrite)
}

func sectionEntry(id ID, status Status, action string) Section {
	return Section{
		ID:     id,
		Status: status,
		Descriptor: Descriptor{
			Action:   action,
			Resource: access.AdminSectionResource(string(id)),
		},
	}
}

// All returns the shipped registry in §10.1's row order.
//
// The returned slice is a COPY: a caller appending to or reordering the result
// must not be able to reach the package's own backing array, because the
// registry is what the authorization gate consults.
func All() []Section {
	out := make([]Section, len(all))
	copy(out, all)
	return out
}

// Lookup resolves a registered section id.
//
// It is consulted ONLY AFTER the ABAC gate permits (see AssertSectionAccess).
// Calling it first would make the registered/unregistered distinction visible to
// a caller the gate would have denied — the registry-enumeration oracle D-06
// closes.
func Lookup(id string) (Section, bool) {
	for _, s := range all {
		if string(s.ID) == id {
			return s, true
		}
	}
	return Section{}, false
}

// Validate is the boot-time half of 01-SPEC §10.2's "compile time or at boot"
// requirement (D-09). Go has no `satisfies`, so the compile-time half of §10.2
// is unavailable and §10.2 explicitly permits boot as the enforcement point.
//
// It is called from a PRODUCTION startup path — see ValidateAtBoot — so a
// misconfigured registry aborts a real boot. A request-time check would mean the
// misconfiguration is discovered by the first unauthorized caller, which is the
// wrong party to learn it from.
func Validate() error { return validateEntries(all) }

// validateEntries carries every registry rule. It takes a SLICE so a test can
// drive it with a deliberately malformed set; [Validate] is the shipped-set call.
//
// Every rule refuses rather than defaulting. A registry entry whose descriptor
// is absent, empty or drifted, or whose status is outside the closed vocabulary,
// is a section that is either ungated or unservable — and neither of those is
// something to discover at request time.
func validateEntries(entries []Section) error {
	for _, e := range entries {
		if e.ID == "" {
			return oops.Code("ADMIN_SECTION_ID_EMPTY").
				Errorf("registry entry has an empty section id")
		}

		if e.Descriptor.Action == "" || e.Descriptor.Resource == "" {
			return oops.Code("ADMIN_SECTION_DESCRIPTOR_INVALID").
				With("section_id", string(e.ID)).
				With("action", e.Descriptor.Action).
				With("resource", e.Descriptor.Resource).
				Errorf("section %q has a zero-valued authorization descriptor; §10.2 forbids reading it as permissive", string(e.ID))
		}

		if want := access.AdminSectionResource(string(e.ID)); e.Descriptor.Resource != want {
			return oops.Code("ADMIN_SECTION_DESCRIPTOR_MISMATCH").
				With("section_id", string(e.ID)).
				With("declared_resource", e.Descriptor.Resource).
				With("derived_resource", want).
				Errorf("section %q declares resource %q but its id derives %q", string(e.ID), e.Descriptor.Resource, want)
		}

		// The status rule DELEGATES to the request-time classifier rather than
		// re-implementing the comparison, so the two halves cannot disagree
		// about which values are recognized.
		if _, err := sectionAvailability(e.Status); err != nil {
			return oops.Code("ADMIN_SECTION_STATUS_UNKNOWN").
				With("section_id", string(e.ID)).
				With("status", string(e.Status)).
				Errorf("section %q carries status %q, which is outside the closed vocabulary", string(e.ID), string(e.Status))
		}
	}
	return nil
}

// sectionAvailability classifies a [Status] EXHAUSTIVELY, and its default arm
// DENIES.
//
// Every request-time decision about a section's status goes through it. Do NOT
// classify a status at a decision site by comparing it against the planned
// constant alone: only the planned value would be refused, so the zero value and
// every typo fall through to the available branch and the section is SERVED.
//
// The denying default is what makes a value added later excluded by the code
// path that already excludes the others, with no second predicate to keep in
// sync. That is the same reason world.Selectable is built this way (plan 02-04).
//
// This half and the [validateEntries] boot rule are BOTH required and neither is
// redundant: the boot rule stops a malformed registry from reaching production,
// the denying default stops a malformed value from being SERVED if one ever does
// — through a substitute slice, a future dynamic registration, or a merge that
// lands a typo in the same commit as a skipped boot.
func sectionAvailability(s Status) (available bool, err error) {
	switch s {
	case StatusAvailable:
		return true, nil
	case StatusPlanned:
		return false, nil
	default:
		return false, oops.Code("ADMIN_SECTION_STATUS_UNKNOWN").
			With("status", string(s)).
			Errorf("unrecognized admin section status %q", string(s))
	}
}
