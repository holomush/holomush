// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginauthz_test

import (
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
