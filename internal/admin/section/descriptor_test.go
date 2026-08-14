// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package section

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/pkg/errutil"
	adminportalv1 "github.com/holomush/holomush/pkg/proto/holomush/adminportal/v1"
)

// TestLookupMethodDescriptorMatchesTheMethodNameExactly pins that the
// method→section table performs NO case folding and NO trimming.
//
// A near-miss MUST resolve to no entry, because a near-miss that resolved to a
// neighbouring entry would authorize one method under another method's section
// and action — which is exactly the silent mis-gate the fail-closed
// ADMIN_SECTION_NOT_DECLARED arm exists to turn into a refusal.
func TestLookupMethodDescriptorMatchesTheMethodNameExactly(t *testing.T) {
	_, found := LookupMethodDescriptor("AdminListSections")
	require.True(t, found, "the shipped method MUST resolve; without it every miss below is vacuous")

	for _, near := range []string{
		"adminlistsections",
		"ADMINLISTSECTIONS",
		"AdminListSections ",
		" AdminListSections",
		"AdminListSection",
		"/holomush.adminportal.v1.AdminPortalService/AdminListSections",
	} {
		t.Run(near, func(t *testing.T) {
			_, ok := LookupMethodDescriptor(near)
			require.False(t, ok, "a near-miss MUST resolve to NO entry, never to a neighbour")
		})
	}
}

// TestTheCharactersSectionResolvesThroughAdminSectionResource pins that a
// descriptor's section id is the same token access.AdminSectionResource derives
// the `admin_section:` reference from — the registry and the method table name
// sections in ONE vocabulary, not two that could drift.
func TestTheCharactersSectionResolvesThroughAdminSectionResource(t *testing.T) {
	entry, found := Lookup("characters")
	require.True(t, found, "the characters section MUST be registered")
	require.Equal(t, access.AdminSectionResource("characters"), entry.Descriptor.Resource)

	// PortalProbeSectionID is a probe, not a scope — but it MUST still be a
	// registered id, or the admission call it feeds would build a reference to
	// a section that does not exist.
	_, probeRegistered := Lookup(string(PortalProbeSectionID))
	require.True(t, probeRegistered, "PortalProbeSectionID MUST name a registered section")
}

// TestEveryServedAdminMethodHasADescriptor is EXT-04's set-equality proof for
// the method table, and it iterates the SERVED set — not the table.
//
// The direction is the whole point. A loop over AdminDescriptors cannot see a
// method that has no entry, which is exactly the fail-open: the gRPC runtime
// would serve it, the interceptor would find no declaration, and only the
// fail-closed arm would stand between it and an ungated call. Iterating
// AdminPortalService_ServiceDesc.Methods — the set the runtime actually serves —
// sees it.
//
// The comparison is symmetric difference in BOTH directions, so an entry naming
// no served method (drift, a rename, a copy-paste) fails it too. Set equality,
// not one-way coverage.
//
// Verifies: INV-ACCESS-16
func TestEveryServedAdminMethodHasADescriptor(t *testing.T) {
	served := map[string]struct{}{}
	for _, m := range adminportalv1.AdminPortalService_ServiceDesc.Methods {
		served[m.MethodName] = struct{}{}
	}
	// Anti-vacuity, BEFORE the comparison: an empty served set would make
	// "equal to the descriptor table" true only if the table were empty too, and
	// two empty sets are not a proof of anything.
	require.NotEmpty(t, served,
		"the served method set is EMPTY — AdminPortalService_ServiceDesc carries no methods, so this comparison would pass vacuously")

	declared := map[string]struct{}{}
	for method := range AdminDescriptors {
		declared[method] = struct{}{}
	}
	require.NotEmpty(t, declared,
		"the descriptor table is EMPTY — the comparison would pass vacuously")

	extra, missing := setDifferences(declared, served)

	require.Emptyf(t, missing,
		"served admin method(s) with NO method→section descriptor: %v.\n"+
			"Each is refused at runtime by the fail-closed ADMIN_SECTION_NOT_DECLARED arm, "+
			"which is correct but means the RPC is dead. Add an AdminDescriptors entry.", missing)
	require.Emptyf(t, extra,
		"descriptor entr(ies) naming a method the service does not serve: %v.\n"+
			"The table has drifted from the wire contract — a rename or a stale copy.", extra)
}

// setDifferences reports the members of declared absent from served, and the
// members of served absent from declared. Both results are SORTED so a failure
// message is reproducible and a diff between two runs is meaningful.
func setDifferences(declared, served map[string]struct{}) (extra, missing []string) {
	for m := range declared {
		if _, ok := served[m]; !ok {
			extra = append(extra, m)
		}
	}
	for m := range served {
		if _, ok := declared[m]; !ok {
			missing = append(missing, m)
		}
	}
	sort.Strings(extra)
	sort.Strings(missing)
	return extra, missing
}

// TestAdminDescriptorEntriesAreWellFormed asserts the shipped table satisfies
// every rule validateAdminDescriptors enforces at boot, so a malformed entry is
// caught by a fast unit test rather than only by a boot that nobody runs in CI.
func TestAdminDescriptorEntriesAreWellFormed(t *testing.T) {
	require.NotEmpty(t, AdminDescriptors, "an empty table would make this test vacuous")

	for method, d := range AdminDescriptors {
		t.Run(method, func(t *testing.T) {
			require.NotEmpty(t, method, "a method key MUST NOT be empty")
			require.Containsf(t, []string{ActionRead, ActionWrite}, d.Action,
				"method %q declares action %q, outside the closed ladder", method, d.Action)

			// EXACTLY ONE of the three shapes, never more and never none.
			// "None" is the forgotten-field case that must not read as
			// permissive; "more than one" is a declaration whose arms
			// contradict each other, which the interceptor's shape switch would
			// resolve by test order rather than by intent.
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
			require.Equalf(t, 1, shapes,
				"method %q declares %d section shapes; §10.2 requires exactly one "+
					"(a fixed SectionID, EnumeratesAllSections, or SectionFromRequest)", method, shapes)

			if d.SectionID != "" {
				_, registered := Lookup(d.SectionID)
				require.Truef(t, registered,
					"method %q names section %q, which is not registered", method, d.SectionID)
			}
		})
	}

	require.NoError(t, validateAdminDescriptors(AdminDescriptors),
		"the shipped table MUST satisfy its own boot validator")
}

// TestValidateAtBootRejectsAMalformedMethodDescriptor proves
// validateAdminDescriptors is WIRED, not merely written.
//
// validateEntries validates REGISTRY entries and never looks at the method
// table, so an author who writes the validator and forgets to call it from
// ValidateAtBoot breaks nothing that anything notices. This drives the real
// startup step with a deliberately malformed entry in the real table and
// requires it to abort. Removing the validateAdminDescriptors() call from
// validateAtBoot makes it FAIL.
func TestValidateAtBootRejectsAMalformedMethodDescriptor(t *testing.T) {
	// Positive control FIRST: the shipped table boots. Without it, the abort
	// below cannot be distinguished from a validator that rejects everything.
	require.NoError(t, ValidateAtBoot(t.Context()),
		"positive control: the shipped tables MUST boot")

	const planted = "AdminPlantedMalformedEntry"
	AdminDescriptors[planted] = MethodDescriptor{Action: ActionRead} // no section, no flag
	t.Cleanup(func() { delete(AdminDescriptors, planted) })

	err := ValidateAtBoot(t.Context())
	require.Error(t, err,
		"a descriptor with no section and no EnumeratesAllSections flag MUST abort the boot; "+
			"if this passes, validateAdminDescriptors is not called from validateAtBoot")
	// The code asserted is the INNER one. errutil.AssertErrorCode resolves the
	// DEEPEST code in the chain, and validateAtBoot wraps the validator's error
	// in ADMIN_METHOD_DESCRIPTORS_INVALID — so asserting the outer spelling here
	// would fail against a correct implementation. This matches the shipped
	// convention for the registry half (boot_test.go:42 asserts the validator's
	// ADMIN_SECTION_DESCRIPTOR_INVALID, not the wrapper's).
	errutil.AssertErrorCode(t, err, "ADMIN_METHOD_DESCRIPTOR_INVALID")
}

// TestADescriptorDeclaringMoreThanOneSectionShapeAbortsTheBoot covers the other
// half of "exactly one".
//
// An entry setting two shapes is not merely redundant: the interceptor's shape
// switch tests the arms in order, so such an entry is gated by whichever arm
// happens to be tested first rather than by what the author declared. A
// SectionFromRequest entry that also named a fixed section would be gated on the
// fixed id while the caller acted on their own — the drift
// ADMIN_SECTION_DESCRIPTOR_MISMATCH exists one field over to catch.
func TestADescriptorDeclaringMoreThanOneSectionShapeAbortsTheBoot(t *testing.T) {
	for name, planted := range map[string]MethodDescriptor{
		"fixed section and enumerates":   {Action: ActionRead, SectionID: "characters", EnumeratesAllSections: true},
		"fixed section and from request": {Action: ActionRead, SectionID: "characters", SectionFromRequest: true},
		"enumerates and from request":    {Action: ActionRead, EnumeratesAllSections: true, SectionFromRequest: true},
		"all three at once":              {Action: ActionRead, SectionID: "characters", EnumeratesAllSections: true, SectionFromRequest: true},
	} {
		t.Run(name, func(t *testing.T) {
			const method = "AdminPlantedMultiShapeEntry"
			AdminDescriptors[method] = planted
			t.Cleanup(func() { delete(AdminDescriptors, method) })

			err := validateAdminDescriptors(AdminDescriptors)
			require.Error(t, err, "an entry declaring more than one shape MUST abort the boot")
			errutil.AssertErrorCode(t, err, "ADMIN_METHOD_DESCRIPTOR_INVALID")
		})
	}
}
