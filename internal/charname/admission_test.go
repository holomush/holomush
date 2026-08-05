// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package charname_test

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/charname"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/pkg/errutil"
)

// These tests exercise (*Gate).Admit — the SOLE constructor of
// charname.Admitted. They reuse gate_test.go's fakeLookup double so the two
// sides of the split (Check's verdict, Admit's token) are asserted against the
// same fixtures.

func TestAdmitMintsATokenCarryingEveryIdentityColumnForAnAcceptedName(t *testing.T) {
	g := newGate(&fakeLookup{known: map[string]ulid.ULID{}})

	got, err := g.Admit(t.Context(), "Alaric")

	require.NoError(t, err)
	assert.False(t, got.IsZero(), "an admitted name must mint a populated token")
	assert.Equal(t, "Alaric", got.Display(), "the display form preserves the player's capitalization")
	assert.Equal(t, "alaric", got.Key())
	assert.Equal(t, charname.Skeleton("alaric"), got.Skeleton())
	assert.Equal(t, charname.UnicodeVersion, got.UnicodeVersion())
}

func TestAdmitPreservesTheSubmittedCapitalizationRatherThanTitleCasingIt(t *testing.T) {
	// §6.1.5 retires the old world.NormalizeCharacterName title-casing: a player
	// who submits "alaric" gets a character named "alaric", not "Alaric".
	g := newGate(&fakeLookup{known: map[string]ulid.ULID{}})

	got, err := g.Admit(t.Context(), "alaric")

	require.NoError(t, err)
	assert.Equal(t, "alaric", got.Display())
}

func TestTheZeroAdmittedReportsIsZeroSoAWriterCanRefuseIt(t *testing.T) {
	// The zero value is the ONLY Admitted another package can build: the
	// populated form is unexported state, so no struct literal outside
	// internal/charname can produce one. That, plus census rule C, is the whole
	// guarantee — see test/meta/character_name_admission_test.go.
	var zero charname.Admitted

	assert.True(t, zero.IsZero())
	assert.Empty(t, zero.Display())
	assert.Empty(t, zero.Key())
	assert.Empty(t, zero.Skeleton())
	assert.Empty(t, zero.UnicodeVersion())
}

func TestAdmitReturnsTheZeroTokenAndTheGatesOwnErrorCodeForEveryRejection(t *testing.T) {
	seededID := idgen.New()
	confusableSkeleton := charname.Skeleton(mustKey(t, "cocoa"))

	tests := []struct {
		name      string
		lookup    *fakeLookup
		blockList charname.BlockList
		submitted string
		wantCode  string
	}{
		{
			name:      "a whole-script homoglyph of a seeded name yields NAME_CONFUSABLE",
			lookup:    &fakeLookup{known: map[string]ulid.ULID{confusableSkeleton: seededID}},
			submitted: "сосоа", // Cyrillic с о с о а
			wantCode:  "NAME_CONFUSABLE",
		},
		{
			name:      "a name carrying a digit yields NAME_INVALID_SYNTAX",
			lookup:    &fakeLookup{known: map[string]ulid.ULID{}},
			submitted: "Alaric2",
			wantCode:  "NAME_INVALID_SYNTAX",
		},
		{
			name:      "an all-whitespace submission yields NAME_EMPTY_NORMAL_FORM",
			lookup:    &fakeLookup{known: map[string]ulid.ULID{}},
			submitted: "   ",
			wantCode:  "NAME_EMPTY_NORMAL_FORM",
		},
		{
			name:      "an unverifiable corpus yields NAME_SKELETON_UNVERIFIABLE",
			lookup:    &fakeLookup{unverifiable: true},
			submitted: "Alaric",
			wantCode:  "NAME_SKELETON_UNVERIFIABLE",
		},
		{
			name:      "a block-list match yields NAME_BLOCKED",
			lookup:    &fakeLookup{known: map[string]ulid.ULID{}},
			blockList: blockEverything{},
			submitted: "Alaric",
			wantCode:  "NAME_BLOCKED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &charname.Gate{Skeletons: tt.lookup, BlockList: tt.blockList}

			got, err := g.Admit(t.Context(), tt.submitted)

			require.Error(t, err)
			errutil.AssertErrorCode(t, err, tt.wantCode)
			assert.True(t, got.IsZero(), "a rejected name must mint no token")
		})
	}
}

func TestAdmitForwardsExcludingCharacterSoASelfCaseVariantRenameCanMintAToken(t *testing.T) {
	// The pair is asserted on ONE double: without ExcludingCharacter the same
	// call is NAME_CONFUSABLE, so the success cannot pass because the fixture
	// seeded nothing (B-18).
	own := idgen.New()
	lookup := &fakeLookup{known: map[string]ulid.ULID{
		charname.Skeleton(mustKey(t, "alaric")): own,
	}}
	g := newGate(lookup)

	blocked, err := g.Admit(t.Context(), "ALARIC")
	require.Error(t, err, "without the exclusion the character collides with its own row")
	errutil.AssertErrorCode(t, err, "NAME_CONFUSABLE")
	assert.True(t, blocked.IsZero())

	admitted, err := g.Admit(t.Context(), "ALARIC", charname.ExcludingCharacter(own))
	require.NoError(t, err)
	assert.False(t, admitted.IsZero())
	assert.Equal(t, "ALARIC", admitted.Display())
	require.NotNil(t, lookup.gotExcluded, "Admit must forward the option to Check verbatim")
	assert.Equal(t, own, *lookup.gotExcluded)
}

// blockEverything is a charname.BlockList double that matches any name.
type blockEverything struct{}

func (blockEverything) Match(string) (bool, int) { return true, 0 }
