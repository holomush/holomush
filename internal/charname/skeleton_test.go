// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package charname_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/unicode/norm"

	"github.com/holomush/holomush/internal/charname"
)

func TestSkeletonCollapsesAWholeScriptCyrillicHomoglyphOntoItsLatinPrototype(t *testing.T) {
	// U+0440 CYRILLIC SMALL LETTER ER and U+0430 CYRILLIC SMALL LETTER A are
	// the classic paypal homoglyph pair.
	cyrillic := "раypal"
	latin := "paypal"

	require.NotEqual(t, cyrillic, latin, "the fixture must be two genuinely different strings")
	assert.Equal(t, charname.Skeleton(latin), charname.Skeleton(cyrillic))
}

func TestSkeletonLeavesTwoGenuinelyDifferentNamesDistinct(t *testing.T) {
	assert.NotEqual(t, charname.Skeleton("Alaric"), charname.Skeleton("Brenna"))
}

func TestSkeletonAppliesTheTrailingNFDRequiredByUTS39Section4(t *testing.T) {
	// U+2126 OHM SIGN maps to U+03A9 GREEK CAPITAL LETTER OMEGA. The property
	// under test is the FINAL NFD pass: whatever bytes the substitution
	// produced, the returned string is already in NFD, so re-normalizing it
	// is a no-op. Without the trailing pass a multi-codepoint prototype could
	// leave a composed sequence behind and two equal names would compare
	// unequal.
	for _, in := range []string{"Ωega", "Éléonore", "ẛa", "straße"} {
		got := charname.Skeleton(in)
		assert.Equal(t, got, norm.NFD.String(got), "Skeleton output for %q is already NFD", in)
	}
}

func TestSkeletonOfTheEmptyStringIsTheEmptyString(t *testing.T) {
	assert.Empty(t, charname.Skeleton(""))
}

func TestUnicodeVersionIsANonEmptyExportedConstant(t *testing.T) {
	assert.NotEmpty(t, charname.UnicodeVersion)
}
