// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package section

import (
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/pkg/errutil"
)

// spec101IDs is 01-SPEC §10.1's table TRANSCRIBED — the ORACLE for the
// set-equality meta-test, and deliberately NOT derived from [All]. A census that
// derives both sides from the artifact under test cannot fail.
//
// §10.1's first cell of each row IS the section id, in the SPEC's own order.
var spec101IDs = []ID{
	"characters",
	"stats",
	"players",
	"moderation",
	"audit",
	"config",
	"plugins",
}

// typoStatus returns a MISSPELLING of "available" — a value that compiles, is
// outside the closed vocabulary, and must never read as available.
//
// It is CONSTRUCTED rather than written as one literal so the misspell linter
// does not flag the deliberate misspelling this suite exists to reject. Same
// device plan 02-07 used to name a §8.6-excluded member without seeding the
// string where a gate would find it.
func typoStatus() Status { return Status("availab" + "e") }

// --- The shipped registry ---

func TestAllReturnsTheSevenSectionsSpec101Enumerates(t *testing.T) {
	got := All()

	require.Len(t, got, 7, "01-SPEC §10.1: seven ids, one available and six planned — there is no eighth")

	gotIDs := make([]ID, 0, len(got))
	for _, s := range got {
		gotIDs = append(gotIDs, s.ID)
	}
	assert.Equal(t, spec101IDs, gotIDs, "the registry is ORDERED, mirroring §10.1's row order")
}

func TestOnlyTheCharactersSectionIsAvailableAndTheOtherSixArePlanned(t *testing.T) {
	for _, s := range All() {
		t.Run(string(s.ID), func(t *testing.T) {
			want := StatusPlanned
			if s.ID == "characters" {
				want = StatusAvailable
			}
			assert.Equal(t, want, s.Status)
		})
	}
}

func TestEveryEntryCarriesADescriptorDerivedFromItsOwnID(t *testing.T) {
	for _, s := range All() {
		t.Run(string(s.ID), func(t *testing.T) {
			assert.NotEmpty(t, s.Descriptor.Action, "§10.2: no zero value means allow")
			assert.NotEmpty(t, s.Descriptor.Resource, "§10.2: no zero value means allow")
			assert.Equal(t, access.AdminSectionResource(string(s.ID)), s.Descriptor.Resource,
				"the descriptor MUST NOT drift from the id it belongs to")
		})
	}
}

// TestTheCharactersSectionDeclaresWriteAndEveryOtherSectionDeclaresRead pins
// which section offers which MAXIMUM operation class.
//
// `characters` moved from read to write in v0.13 phase-06 plan 05, when it
// gained three write RPCs (§10.4, §10.6). The six planned sections stay at read:
// a declared maximum is what a reviewer reads as a section's intended blast
// radius, and widening one before it has a write surface would declare an
// operation class nothing implements.
//
// Both halves are asserted together on purpose. The characters half alone would
// stay green if `entry` itself were changed to build every row with write.
func TestTheCharactersSectionDeclaresWriteAndEveryOtherSectionDeclaresRead(t *testing.T) {
	characters, ok := Lookup("characters")
	require.True(t, ok)
	assert.Equal(t, Descriptor{Action: "write", Resource: "admin_section:characters"}, characters.Descriptor)

	others := 0
	for _, s := range All() {
		if s.ID == "characters" {
			continue
		}
		others++
		assert.Equal(t, ActionRead, s.Descriptor.Action,
			"section %q has no write surface, so it MUST NOT declare one", s.ID)
	}
	require.Equal(t, 6, others,
		"six sections must actually have been inspected, or the read half passes vacuously")
}

func TestLookupResolvesARegisteredSectionAndMissesAnUnregisteredOne(t *testing.T) {
	entry, ok := Lookup("characters")
	require.True(t, ok, "positive control: a registered id resolves")
	assert.Equal(t, ID("characters"), entry.ID)

	_, ok = Lookup("nonexistent")
	assert.False(t, ok, "an unregistered id misses")
}

// --- Boot validation ---

func TestValidateAcceptsTheShippedRegistry(t *testing.T) {
	require.NoError(t, Validate())
}

func TestValidateEntriesRejectsEveryShapeOfAZeroValuedDescriptor(t *testing.T) {
	// Each subtest is PAIRED with the shipped set returning nil, so a rejection
	// cannot pass because validateEntries rejects everything.
	cases := []struct {
		name       string
		descriptor Descriptor
		wantCode   string
	}{
		{
			name:       "the zero-valued descriptor struct",
			descriptor: Descriptor{},
			wantCode:   "ADMIN_SECTION_DESCRIPTOR_INVALID",
		},
		{
			name:       "an empty action",
			descriptor: Descriptor{Action: "", Resource: access.AdminSectionResource("characters")},
			wantCode:   "ADMIN_SECTION_DESCRIPTOR_INVALID",
		},
		{
			name:       "an empty resource",
			descriptor: Descriptor{Action: "read", Resource: ""},
			wantCode:   "ADMIN_SECTION_DESCRIPTOR_INVALID",
		},
		{
			name:       "a resource that has drifted from the entry id",
			descriptor: Descriptor{Action: "read", Resource: access.AdminSectionResource("stats")},
			wantCode:   "ADMIN_SECTION_DESCRIPTOR_MISMATCH",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, Validate(), "paired positive control: the SHIPPED set validates")

			err := validateEntries([]Section{{
				ID:         "characters",
				Status:     StatusAvailable,
				Descriptor: tc.descriptor,
			}})

			require.Error(t, err, "§10.2: a zero-valued descriptor MUST be rejected, never read as permissive")
			errutil.AssertErrorCode(t, err, tc.wantCode)
			assert.Contains(t, err.Error(), "characters",
				"the boot failure MUST name the offending entry so the operator can find it")
		})
	}
}

func TestValidateEntriesRejectsAStatusOutsideTheClosedTwoValueVocabulary(t *testing.T) {
	// Status is a bare string type, so BOTH of these compile. Neither is caught
	// by any descriptor rule, and an unrecognized value MUST NOT be servable.
	cases := []struct {
		name   string
		status Status
	}{
		{name: "the zero value", status: Status("")},
		{name: "a typo", status: typoStatus()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, Validate(), "paired positive control: the SHIPPED set validates")

			err := validateEntries([]Section{{
				ID:         "characters",
				Status:     tc.status,
				Descriptor: Descriptor{Action: "read", Resource: access.AdminSectionResource("characters")},
			}})

			require.Error(t, err)
			errutil.AssertErrorCode(t, err, "ADMIN_SECTION_STATUS_UNKNOWN")
			assert.Contains(t, err.Error(), "characters", "the failure names the offending ENTRY")
			assert.Contains(t, err.Error(), string(tc.status), "the failure names the offending VALUE")
		})
	}
}

func TestValidateEntriesRejectsAnEmptySectionID(t *testing.T) {
	require.NoError(t, Validate(), "paired positive control: the SHIPPED set validates")

	err := validateEntries([]Section{{
		ID:         "",
		Status:     StatusAvailable,
		Descriptor: Descriptor{Action: "read", Resource: "x"},
	}})

	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "ADMIN_SECTION_ID_EMPTY")
}

// --- The status classifier ---

func TestSectionAvailabilityDeniesOnAnyStatusOutsideTheTwoConstants(t *testing.T) {
	t.Run("the available constant reports available with no error", func(t *testing.T) {
		available, err := sectionAvailability(StatusAvailable)
		require.NoError(t, err)
		assert.True(t, available, "positive control: the classifier does not deny everything")
	})

	t.Run("the planned constant reports not-available with no error", func(t *testing.T) {
		available, err := sectionAvailability(StatusPlanned)
		require.NoError(t, err)
		assert.False(t, available)
	})

	// The default arm DENIES. Comparing a status against the planned constant
	// alone would let the zero value and every typo fall through to the
	// available branch and the section would be SERVED.
	for _, unknown := range []Status{Status(""), typoStatus()} {
		t.Run(fmt.Sprintf("the unrecognized status %q denies and errors", string(unknown)), func(t *testing.T) {
			available, err := sectionAvailability(unknown)
			require.Error(t, err)
			errutil.AssertErrorCode(t, err, "ADMIN_SECTION_STATUS_UNKNOWN")
			assert.False(t, available, "an unknown status MUST NEVER read as available")
		})
	}
}

// --- The id set-equality meta-test ---

// TestTheRegistryIDSetEqualsTheSevenIDsSpec101Enumerates is the §12.1 rule-1
// census, narrowed to this plan's own registry: set equality on exact-string
// keys, order-independent, with a symmetric-difference diff on failure.
//
// It is proven to fail in BOTH directions — an added entry and a removed one —
// because a census that only catches removals is half a census.
func TestTheRegistryIDSetEqualsTheSevenIDsSpec101Enumerates(t *testing.T) {
	t.Run("the shipped registry matches the SPEC set exactly", func(t *testing.T) {
		missing, extra := idSetDiff(All(), spec101IDs)
		assert.Empty(t, missing, "ids §10.1 enumerates that the registry does not carry")
		assert.Empty(t, extra, "ids the registry carries that §10.1 does not enumerate")
	})

	t.Run("an eighth entry is reported as EXTRA", func(t *testing.T) {
		eighth := append(All(), Section{
			ID:         "billing",
			Status:     StatusPlanned,
			Descriptor: Descriptor{Action: "read", Resource: access.AdminSectionResource("billing")},
		})

		missing, extra := idSetDiff(eighth, spec101IDs)
		assert.Empty(t, missing)
		assert.Equal(t, []ID{"billing"}, extra, "adding a section without adding its §10.1 row is RED")
	})

	t.Run("a removed entry is reported as MISSING", func(t *testing.T) {
		short := All()[:len(All())-1]

		missing, extra := idSetDiff(short, spec101IDs)
		assert.Equal(t, []ID{"plugins"}, missing, "removing a section without removing its §10.1 row is RED")
		assert.Empty(t, extra)
	})
}

// idSetDiff reports the symmetric difference between the ids `got` carries and
// the `want` oracle, in both directions, sorted for a stable failure message.
func idSetDiff(got []Section, want []ID) (missing, extra []ID) {
	gotSet := make(map[ID]struct{}, len(got))
	for _, s := range got {
		gotSet[s.ID] = struct{}{}
	}
	wantSet := make(map[ID]struct{}, len(want))
	for _, id := range want {
		wantSet[id] = struct{}{}
	}

	for id := range wantSet {
		if _, ok := gotSet[id]; !ok {
			missing = append(missing, id)
		}
	}
	for id := range gotSet {
		if _, ok := wantSet[id]; !ok {
			extra = append(extra, id)
		}
	}

	sort.Slice(missing, func(i, j int) bool { return missing[i] < missing[j] })
	sort.Slice(extra, func(i, j int) bool { return extra[i] < extra[j] })
	return missing, extra
}
