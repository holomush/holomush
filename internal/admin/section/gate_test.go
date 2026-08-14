// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package section

import (
	"context"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/testsupport/abactest"
	"github.com/holomush/holomush/pkg/errutil"
)

// The real *policy.Engine MUST satisfy the narrow interface this package
// declares. Kept in the test file so a signature drift in the engine breaks the
// binding here rather than leaving the package asserting an invented API.
var _ PolicyEvaluator = (*policy.Engine)(nil)

// Player ids are opaque to the abactest double — distinct values only so a
// failure message names which fixture spoke.
const (
	adminPlayerID   = "01JZZZZZZZZZZZZZZZZZZZADMIN"
	builderPlayerID = "01JZZZZZZZZZZZZZZZZZBUILDER"
	plainPlayerID   = "01JZZZZZZZZZZZZZZZZZZPLAYER"
	guestPlayerID   = "01JZZZZZZZZZZZZZZZZZZZGUEST"
)

// caller is one fixture: a player id plus the roles the player double reports.
type caller struct {
	name    string
	id      string
	roles   []string
	isGuest bool
}

func adminCaller() caller {
	return caller{name: "admin", id: adminPlayerID, roles: []string{"admin"}}
}

// nonAdmins are the three fixtures §10.2's denial test must refuse. They are
// deliberately DIFFERENT shapes — a role that is not admin, no role at all, and
// an ephemeral guest — so a refusal cannot pass by accident of one shape.
func nonAdmins() []caller {
	return []caller{
		{name: "builder", id: builderPlayerID, roles: []string{"builder"}},
		{name: "plain player", id: plainPlayerID},
		{name: "guest", id: guestPlayerID, isGuest: true},
	}
}

// engineFor builds a REAL engine over the FULL seed corpus for one caller.
//
// Deliberately NOT a fake: a canned-decision engine would make every assertion
// below a test of the double's answer rather than of seed:admin-section-access,
// and this suite is the only proof in the tree that that policy matches.
func engineFor(t *testing.T, c caller) PolicyEvaluator {
	t.Helper()
	p := abactest.Player{ID: c.id, Roles: c.roles}
	if c.isGuest {
		guest := true
		p.IsGuest = &guest
	}
	return abactest.NewSeedEngine(t, abactest.PlayerProvider(p))
}

// assertAdminPassesTheGate is the PAIRED POSITIVE CONTROL every denial below
// runs against the SAME section. Without it `err != nil` cannot distinguish
// "denied for lack of the admin role" from "denied because the helper denies
// everything".
//
// "Passes the gate" is deliberately NOT "receives the entry". Six of the seven
// sections are planned, so an admin's CORRECT outcome there is
// SECTION_NOT_IMPLEMENTED — which is itself proof the gate permitted, because
// §10.3 places that refusal AFTER the gate and nothing else can produce it.
func assertAdminPassesTheGate(t *testing.T, sectionID string) (Section, error) {
	t.Helper()
	entry, err := AssertSectionAccess(t.Context(), engineFor(t, adminCaller()),
		adminPlayerID, sectionID, ActionRead)
	if err != nil {
		errutil.AssertErrorCode(t, err, "SECTION_NOT_IMPLEMENTED")
	}
	return entry, err
}

// requireAdminEntry is the paired positive control for the ONE available
// section: an admin receives the entry itself, with no refusal at all.
//
// It takes no section argument because `characters` is the only available
// section §10.1 registers — every other id would correctly return §10.3's
// refusal, which [assertAdminPassesTheGate] is the control for.
func requireAdminEntry(t *testing.T) Section {
	t.Helper()
	entry, err := AssertSectionAccess(t.Context(), engineFor(t, adminCaller()),
		adminPlayerID, "characters", ActionRead)
	require.NoError(t, err, "positive control: an admin MUST reach the available section")
	return entry
}

// --- §10.2's non-vacuous denial suite, at the shared-helper level (D-08) ---

// TestEveryRegisteredSectionPermitsAnAdminAndDeniesEveryNonAdmin is 01-SPEC
// §10.2's mandated denial test, run at the SHARED AUTHORIZATION HELPER because
// Phase 2 ships no RPCs (D-08). Seven sections, six of them with no content —
// which is the point: the suite is non-vacuous from day one and a new section
// that skips the gate turns it RED before it has anything to review.
//
// The table is ITERATED from All(), never hard-coded, so a newly registered
// section is covered automatically.
func TestEveryRegisteredSectionPermitsAnAdminAndDeniesEveryNonAdmin(t *testing.T) {
	permits, denials := 0, 0

	for _, s := range All() {
		sectionID := string(s.ID)

		t.Run(sectionID+"/an admin is permitted", func(t *testing.T) {
			entry, err := assertAdminPassesTheGate(t, sectionID)
			if err != nil {
				// A planned section: past the gate, then §10.3's refusal.
				assert.Equal(t, StatusPlanned, s.Status)
				return
			}
			assert.Equal(t, s.ID, entry.ID)
			assert.Equal(t, s.Descriptor, entry.Descriptor,
				"the helper returns the entry's DESCRIPTOR alongside the section")
		})
		permits++

		for _, c := range nonAdmins() {
			t.Run(sectionID+"/a "+c.name+" is denied", func(t *testing.T) {
				// Paired positive control, same section, same helper.
				assertAdminPassesTheGate(t, sectionID)

				_, err := AssertSectionAccess(t.Context(), engineFor(t, c),
					c.id, sectionID, ActionRead)

				require.Error(t, err)
				errutil.AssertErrorCode(t, err, "DENY_ADMIN_SECTION")
			})
			denials++
		}
	}

	assert.GreaterOrEqual(t, permits, 7, "one permit assertion per registered section")
	assert.GreaterOrEqual(t, denials, 21, "three denial assertions per registered section")
}

// --- D-06: gate first, then distinguish ---

// TestAPermittedCallerReachesTheRegistryForAnIDNoPolicyEnumerates proves the
// resource-TYPE scoping EXT-07 turns on. An eighth section needs no new policy:
// the admin's request for an unregistered id got PAST the gate, which it could
// only do if seed:admin-section-access matched a resource id it never
// enumerates.
func TestAPermittedCallerReachesTheRegistryForAnIDNoPolicyEnumerates(t *testing.T) {
	_, err := AssertSectionAccess(t.Context(), engineFor(t, adminCaller()),
		adminPlayerID, "nonexistent", ActionRead)

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DENY_ADMIN_SECTION_UNREGISTERED")
}

// Verifies: INV-PRIVACY-11
//
// TestANonAdminsRefusalIsIdenticalAcrossARegisteredAndAnUnregisteredSectionID
// is the differential assertion that closes the registry-enumeration oracle
// D-06 names. A one-sided check — "an unregistered id returns a denial" — is
// satisfied by an implementation whose denial is DISTINGUISHABLE, which is the
// leak itself.
//
// The comparison is on the caller-visible MESSAGE STRING. errutil.AssertErrorCode
// chain-walks to the deepest code, so it says which internal code fired and is
// never evidence of what a caller could tell apart.
func TestANonAdminsRefusalIsIdenticalAcrossARegisteredAndAnUnregisteredSectionID(t *testing.T) {
	for _, c := range nonAdmins() {
		t.Run(c.name, func(t *testing.T) {
			// Paired control: the two ids genuinely DIFFER below the gate, so
			// the equality below is indistinguishability rather than a helper
			// that ignores its section argument.
			registeredEntry := requireAdminEntry(t)
			assert.Equal(t, ID("characters"), registeredEntry.ID)
			_, adminUnregistered := AssertSectionAccess(t.Context(), engineFor(t, adminCaller()),
				adminPlayerID, "nonexistent", ActionRead)
			errutil.AssertErrorCode(t, adminUnregistered, "DENY_ADMIN_SECTION_UNREGISTERED")

			_, registered := AssertSectionAccess(t.Context(), engineFor(t, c),
				c.id, "characters", ActionRead)
			_, unregistered := AssertSectionAccess(t.Context(), engineFor(t, c),
				c.id, "nonexistent", ActionRead)

			require.Error(t, registered)
			require.Error(t, unregistered)
			errutil.AssertErrorCode(t, registered, "DENY_ADMIN_SECTION")
			errutil.AssertErrorCode(t, unregistered, "DENY_ADMIN_SECTION")
			assert.Equal(t, registered.Error(), unregistered.Error(),
				"a caller the gate denied MUST NOT be able to tell a registered section from an unregistered one")
			assert.NotContains(t, unregistered.Error(), "nonexistent",
				"the refusal MUST NOT echo the probed id back")
		})
	}
}

// --- §10.3: planned sections refuse AFTER the gate ---

func TestAPlannedSectionRefusesOnlyAfterTheGatePermits(t *testing.T) {
	t.Run("an admin hitting a planned section is told it is not implemented", func(t *testing.T) {
		// Paired with the ONLY available section, so the refusal cannot pass
		// because every section refuses.
		available := requireAdminEntry(t)
		assert.Equal(t, StatusAvailable, available.Status)

		_, err := AssertSectionAccess(t.Context(), engineFor(t, adminCaller()),
			adminPlayerID, "stats", ActionRead)

		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SECTION_NOT_IMPLEMENTED")
	})

	t.Run("a non-admin hitting a planned section is told nothing about it", func(t *testing.T) {
		for _, c := range nonAdmins() {
			assertAdminPassesTheGate(t, "stats")

			_, planned := AssertSectionAccess(t.Context(), engineFor(t, c),
				c.id, "stats", ActionRead)
			_, availableSection := AssertSectionAccess(t.Context(), engineFor(t, c),
				c.id, "characters", ActionRead)

			require.Error(t, planned)
			require.Error(t, availableSection)
			errutil.AssertErrorCode(t, planned, "DENY_ADMIN_SECTION")
			assert.Equal(t, availableSection.Error(), planned.Error(),
				"%s: the planned/available split MUST be invisible below the gate", c.name)
		}
	})
}

// --- The descriptor participates in authorization ---

// TestTheDescriptorResourceMustAgreeWithTheResourceTheGateEvaluated drives the
// unexported form with a SUBSTITUTE lookup, because a drifted descriptor cannot
// exist in the shipped registry — validateEntries rejects it at boot. Both
// halves are needed: boot stops it shipping, this stops it deciding.
func TestTheDescriptorResourceMustAgreeWithTheResourceTheGateEvaluated(t *testing.T) {
	// Paired positive control: the REAL lookup permits the same call.
	requireAdminEntry(t)

	drifted := func(string) (Section, bool) {
		return Section{
			ID:     "characters",
			Status: StatusAvailable,
			Descriptor: Descriptor{
				Action:   ActionRead,
				Resource: access.AdminSectionResource("stats"),
			},
		}, true
	}

	_, err := assertSectionAccess(t.Context(), engineFor(t, adminCaller()),
		adminPlayerID, "characters", ActionRead, drifted)

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "ADMIN_SECTION_DESCRIPTOR_MISMATCH")
}

// TestACallerAskingForMoreThanTheSectionDeclaresIsRefused proves
// Descriptor.Action DECIDES something. seed:admin-section-access permits both
// read and write on the resource type, so the gate PERMITS this request; the
// refusal comes from the registry's own declared maximum.
func TestACallerAskingForMoreThanTheSectionDeclaresIsRefused(t *testing.T) {
	t.Run("the declared action succeeds", func(t *testing.T) {
		entry := requireAdminEntry(t)
		// `characters` declares WRITE as of v0.13 phase-06 plan 05, so read — a
		// LOWER rank — is admitted by the same ladder.
		assert.Equal(t, ActionWrite, entry.Descriptor.Action)
	})

	t.Run("the write a write-declaring section offers is admitted", func(t *testing.T) {
		_, err := AssertSectionAccess(t.Context(), engineFor(t, adminCaller()),
			adminPlayerID, "characters", ActionWrite)
		require.NoError(t, err,
			"positive control: the section that DOES declare write must admit it, or the refusal "+
				"below could pass because the gate refuses every write")
	})

	t.Run("an undeclared action is refused on a READ-declaring section", func(t *testing.T) {
		// PAIRED with the control above on a DIFFERENT section, because
		// `characters` no longer declares read as its maximum. `stats` does, and
		// step 3's action check runs BEFORE step 4's availability check — so a
		// planned section still yields ADMIN_SECTION_ACTION_NOT_DECLARED here
		// rather than SECTION_NOT_IMPLEMENTED, which is itself the ordering this
		// assertion depends on.
		_, readErr := AssertSectionAccess(t.Context(), engineFor(t, adminCaller()),
			adminPlayerID, "stats", ActionRead)
		errutil.AssertErrorCode(t, readErr, "SECTION_NOT_IMPLEMENTED")

		_, err := AssertSectionAccess(t.Context(), engineFor(t, adminCaller()),
			adminPlayerID, "stats", ActionWrite)

		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "ADMIN_SECTION_ACTION_NOT_DECLARED")
	})

	t.Run("an action outside the operation-class ladder is refused", func(t *testing.T) {
		requireAdminEntry(t)

		_, err := AssertSectionAccess(t.Context(), engineFor(t, adminCaller()),
			adminPlayerID, "characters", "obliterate")

		require.Error(t, err,
			"an action the ladder does not rank MUST fail closed, not fall through")
	})
}

// TestTheUndeclaredActionRefusalIsInvisibleToACallerTheGateDenied proves the
// action check sits AFTER the gate. Refusing an undeclared action FIRST would
// let an unauthorized caller tell a section that declares write from one that
// does not — the same enumeration oracle D-06 closes, one field over.
func TestTheUndeclaredActionRefusalIsInvisibleToACallerTheGateDenied(t *testing.T) {
	for _, c := range nonAdmins() {
		t.Run(c.name, func(t *testing.T) {
			requireAdminEntry(t)

			_, declared := AssertSectionAccess(t.Context(), engineFor(t, c),
				c.id, "characters", ActionRead)
			_, undeclared := AssertSectionAccess(t.Context(), engineFor(t, c),
				c.id, "characters", ActionWrite)

			require.Error(t, declared)
			require.Error(t, undeclared)
			errutil.AssertErrorCode(t, declared, "DENY_ADMIN_SECTION")
			errutil.AssertErrorCode(t, undeclared, "DENY_ADMIN_SECTION")
			assert.Equal(t, declared.Error(), undeclared.Error(),
				"the action distinction MUST be invisible to a caller the gate denied")
		})
	}
}

// --- Malformed-request guards, checked BEFORE the resource is constructed ---

func TestAnEmptySectionIDReturnsAnErrorRatherThanPanicking(t *testing.T) {
	engine := engineFor(t, adminCaller())

	assert.NotPanics(t, func() {
		_, err := AssertSectionAccess(t.Context(), engine, adminPlayerID, "", ActionRead)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "ADMIN_SECTION_REQUEST_MALFORMED")
	}, "access.AdminSectionResource panics by design; that guard catches programmer error at construction sites, not untrusted request input")
}

func TestAMalformedRequestIsRefusedBeforeAnyEngineCall(t *testing.T) {
	cases := []struct {
		name              string
		engine            PolicyEvaluator
		playerID, action  string
		sectionIDOverride string
	}{
		{name: "no engine", engine: nil, playerID: adminPlayerID, action: ActionRead},
		{name: "an empty player id", playerID: "", action: ActionRead},
		{name: "an empty action", playerID: adminPlayerID, action: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Paired positive control: the well-formed call succeeds.
			requireAdminEntry(t)

			engine := tc.engine
			if tc.name != "no engine" {
				engine = engineFor(t, adminCaller())
			}

			_, err := AssertSectionAccess(t.Context(), engine, tc.playerID, "characters", tc.action)

			require.Error(t, err)
			errutil.AssertErrorCode(t, err, "ADMIN_SECTION_REQUEST_MALFORMED")
		})
	}
}

// TestTheGateEvaluatesTheABACEngineBeforeItConsultsTheRegistry pins D-06's
// ordering STRUCTURALLY rather than by reading gate.go: a lookup that would
// panic if reached proves a denied caller never reaches it.
func TestTheGateEvaluatesTheABACEngineBeforeItConsultsTheRegistry(t *testing.T) {
	poisoned := func(string) (Section, bool) {
		panic("the registry was consulted for a caller the gate denied — D-06's ordering is broken")
	}

	for _, c := range nonAdmins() {
		t.Run(c.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				_, err := assertSectionAccess(t.Context(), engineFor(t, c),
					c.id, "characters", ActionRead, poisoned)
				require.Error(t, err)
				errutil.AssertErrorCode(t, err, "DENY_ADMIN_SECTION")
			})
		})
	}

	// Paired positive control: a PERMITTED caller DOES reach the lookup, so the
	// no-panic result above is ordering and not an unreachable lookup.
	assert.Panics(t, func() {
		_, _ = assertSectionAccess(context.Background(), engineFor(t, adminCaller()),
			adminPlayerID, "characters", ActionRead, poisoned)
	})
}

// --- Admission: the policy-gate step alone (D-99, plan 06-01) ---

// TestTheAdmissionProbeIDIsImmaterialToTheVerdict is what makes
// [PortalProbeSectionID] SOUND rather than an arbitrary pick.
//
// seed:admin-section-access targets the resource TYPE (`resource is
// admin_section`) with no literal id in the DSL, so for a FIXED player the
// admission verdict cannot depend on which id is probed. This test asserts that
// equality directly: collect the verdict for every id All() registers and
// require the set of distinct verdicts to have exactly one member — once for an
// admin and once for a non-admin.
//
// It goes RED the moment a per-section grant lands, which is precisely when a
// concrete-id probe stops selecting no scope and the portal-admission check
// needs a real portal grant instead.
func TestTheAdmissionProbeIDIsImmaterialToTheVerdict(t *testing.T) {
	all := All()
	// Guard against vacuity BEFORE the loop: a registry that returned nothing
	// would give a set of zero verdicts, and "zero distinct verdicts" must not
	// be mistaken for "one".
	require.Len(t, all, 7, "the shipped registry MUST carry seven sections; "+
		"a shorter one would make the loop below prove nothing")

	// distinctVerdicts maps a verdict — the oops code, or "" for permitted — to
	// the ids that produced it, so a failure names the divergent section.
	distinctVerdicts := func(t *testing.T, c caller) map[string][]string {
		t.Helper()
		engine := engineFor(t, c)
		out := map[string][]string{}
		for _, s := range all {
			err := AssertSectionAdmission(t.Context(), engine, c.id, string(s.ID), ActionRead)
			verdict := ""
			if err != nil {
				oopsErr, isOops := oops.AsOops(err)
				require.True(t, isOops, "admission refusals MUST be typed oops errors")
				code, isString := oopsErr.Code().(string)
				require.True(t, isString, "an oops code MUST be a string")
				verdict = code
			}
			out[verdict] = append(out[verdict], string(s.ID))
		}
		return out
	}

	t.Run("an admin is admitted to every section id identically", func(t *testing.T) {
		verdicts := distinctVerdicts(t, adminCaller())
		require.Len(t, verdicts, 1, "the admission verdict MUST NOT vary by section id: %v", verdicts)
		_, permitted := verdicts[""]
		require.True(t, permitted, "an admin MUST be ADMITTED, not uniformly refused: %v", verdicts)
	})

	for _, c := range nonAdmins() {
		t.Run("a "+c.name+" is refused for every section id identically", func(t *testing.T) {
			verdicts := distinctVerdicts(t, c)
			require.Len(t, verdicts, 1, "the admission verdict MUST NOT vary by section id: %v", verdicts)
			_, denied := verdicts["DENY_ADMIN_SECTION"]
			require.True(t, denied, "a non-admin MUST be refused with the static denial: %v", verdicts)
		})
	}
}

// TestAdmissionPermitsAPlannedSectionThatAccessRefuses is the assertion that
// keeps the six planned sections in the /admin nav.
//
// Admission is step 1 alone — the policy answer to "may this player touch the
// admin section surface at all". Access is admission PLUS registration,
// descriptor consistency and availability, and its availability step refuses a
// planned section with SECTION_NOT_IMPLEMENTED. An enumeration filter written
// against AssertSectionAccess would therefore silently drop every planned
// section out of the nav EXT-02 and D-101 require them to appear in.
func TestAdmissionPermitsAPlannedSectionThatAccessRefuses(t *testing.T) {
	// Pick a genuinely planned section from the shipped registry rather than
	// naming one, so the fixture cannot go stale against a status change.
	var planned string
	for _, s := range All() {
		if s.Status == StatusPlanned {
			planned = string(s.ID)
			break
		}
	}
	require.NotEmpty(t, planned, "the registry MUST carry at least one planned section for this test to mean anything")

	engine := engineFor(t, adminCaller())

	require.NoError(t,
		AssertSectionAdmission(t.Context(), engine, adminPlayerID, planned, ActionRead),
		"admission MUST NOT run the availability step: section %q", planned)

	_, accessErr := AssertSectionAccess(t.Context(), engine, adminPlayerID, planned, ActionRead)
	require.Error(t, accessErr, "access MUST still refuse a planned section")
	errutil.AssertErrorCode(t, accessErr, "SECTION_NOT_IMPLEMENTED")
}

// TestAdmissionRefusesAMalformedRequestBeforeAnyEngineCall pins that the
// extraction kept step 1's guards, not just its evaluation.
func TestAdmissionRefusesAMalformedRequestBeforeAnyEngineCall(t *testing.T) {
	for _, tc := range []struct {
		name              string
		engine            PolicyEvaluator
		playerID, section string
		action            string
	}{
		{"nil engine", nil, adminPlayerID, "characters", ActionRead},
		{"empty player id", engineFor(t, adminCaller()), "", "characters", ActionRead},
		{"empty section id", engineFor(t, adminCaller()), adminPlayerID, "", ActionRead},
		{"empty action", engineFor(t, adminCaller()), adminPlayerID, "characters", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := AssertSectionAdmission(t.Context(), tc.engine, tc.playerID, tc.section, tc.action)
			require.Error(t, err)
			errutil.AssertErrorCode(t, err, "ADMIN_SECTION_REQUEST_MALFORMED")
		})
	}
}
