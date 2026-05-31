// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginauthz_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestCheckPrincipalOwnershipReturnsParsedULIDWhenPrincipalMatchesExpectedOwner(t *testing.T) {
	id := core.NewULID()

	pid, err := pluginauthz.CheckPrincipalOwnership(id.String(), id.String())
	require.NoError(t, err)
	assert.Equal(t, id.String(), pid.String())
}

func TestCheckPrincipalOwnershipDeniesWhenPrincipalDiffersFromExpectedOwner(t *testing.T) {
	// A well-formed ULID that does NOT match the host-vouched expected owner —
	// e.g. a PLAYER request whose principal_id is not the acting character's
	// owning player. Denied (PRINCIPAL_NOT_OWNED).
	principal := core.NewULID()
	expectedOwner := core.NewULID()

	_, err := pluginauthz.CheckPrincipalOwnership(principal.String(), expectedOwner.String())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PRINCIPAL_NOT_OWNED")
}

func TestCheckPrincipalOwnershipDeniesWhenExpectedOwnerIsEmpty(t *testing.T) {
	// No host-vouched owner (e.g. PLAYER scope from a dispatch with no player
	// context) ⇒ fail closed, even though principal_id is a valid ULID.
	principal := core.NewULID()

	_, err := pluginauthz.CheckPrincipalOwnership(principal.String(), "")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PRINCIPAL_NOT_OWNED")
}

func TestCheckPrincipalOwnershipRejectsEmptyPrincipalID(t *testing.T) {
	expectedOwner := core.NewULID()

	_, err := pluginauthz.CheckPrincipalOwnership("", expectedOwner.String())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "INVALID_PRINCIPAL_ID")
}

func TestCheckPrincipalOwnershipRejectsMalformedPrincipalID(t *testing.T) {
	expectedOwner := core.NewULID()

	_, err := pluginauthz.CheckPrincipalOwnership("not-a-ulid", expectedOwner.String())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "INVALID_PRINCIPAL_ID")
}

// TestCheckPrincipalOwnershipAcceptsCaseVariantPrincipalID pins the
// parsed-ULID comparison introduced in Item 3 of holomush-iokti.15: a
// lowercase (or mixed-case) encoding of the same ULID as expectedOwnerID MUST
// be accepted, because ULIDs are case-insensitive. The raw-string compare that
// predated this change would have rejected a lowercase principal_id even when
// it encoded the same 128-bit value — a fragile assumption about wire encoding.
func TestCheckPrincipalOwnershipAcceptsCaseVariantPrincipalID(t *testing.T) {
	id := core.NewULID()
	upper := id.String()                  // canonical uppercase encoding
	lower := strings.ToLower(id.String()) // same value, lowercase

	// Both the uppercase and lowercase forms should be accepted when expectedOwnerID
	// is the canonical uppercase form.
	pidFromUpper, err := pluginauthz.CheckPrincipalOwnership(upper, upper)
	require.NoError(t, err)
	assert.Equal(t, id, pidFromUpper)

	pidFromLower, err := pluginauthz.CheckPrincipalOwnership(lower, upper)
	require.NoError(t, err, "lowercase encoding of the same ULID MUST be accepted")
	assert.Equal(t, id, pidFromLower,
		"parsed ULID from lowercase encoding MUST equal the parsed expectedOwnerID")
}

// TestCheckPrincipalOwnershipDeniesWhenExpectedOwnerIDIsMalformed pins the
// fail-closed behaviour introduced in Item 3 of holomush-iokti.15: if
// expectedOwnerID does not parse as a valid ULID (e.g. host bug / truncated
// token field), the gate MUST deny (PRINCIPAL_NOT_OWNED) rather than
// proceeding with an unparseable expected owner. expectedOwnerID is
// host-vouched; a malformed value is a host defect and must not be silently
// bypassed.
func TestCheckPrincipalOwnershipDeniesWhenExpectedOwnerIDIsMalformed(t *testing.T) {
	principal := core.NewULID()

	_, err := pluginauthz.CheckPrincipalOwnership(principal.String(), "not-a-ulid")
	require.Error(t, err)
	// malformed expectedOwnerID MUST fail closed (PRINCIPAL_NOT_OWNED), not panic or pass
	errutil.AssertErrorCode(t, err, "PRINCIPAL_NOT_OWNED")
}
