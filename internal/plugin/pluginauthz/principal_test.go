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

func TestCheckPrincipalOwnershipReturnsParsedULIDWhenActorOwnsPrincipal(t *testing.T) {
	id := core.NewULID()
	actor := core.Actor{Kind: core.ActorCharacter, ID: id.String()}

	pid, err := pluginauthz.CheckPrincipalOwnership(id.String(), actor)
	require.NoError(t, err)
	assert.Equal(t, id.String(), pid.String())
}

func TestCheckPrincipalOwnershipDeniesForeignPrincipal(t *testing.T) {
	// A well-formed ULID that the acting character does NOT own (e.g. a
	// player ULID, or another character). Mirrors the iokti.16 PLAYER
	// fail-closed contract: a player ULID never equals the acting
	// character's ULID, so a real player-principal request is denied.
	foreign := core.NewULID()
	actor := core.Actor{Kind: core.ActorCharacter, ID: core.NewULID().String()}

	_, err := pluginauthz.CheckPrincipalOwnership(foreign.String(), actor)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "PRINCIPAL_NOT_OWNED")
}

func TestCheckPrincipalOwnershipRejectsEmptyPrincipalID(t *testing.T) {
	actor := core.Actor{Kind: core.ActorCharacter, ID: core.NewULID().String()}

	_, err := pluginauthz.CheckPrincipalOwnership("", actor)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "INVALID_PRINCIPAL_ID")
}

func TestCheckPrincipalOwnershipRejectsMalformedPrincipalID(t *testing.T) {
	actor := core.Actor{Kind: core.ActorCharacter, ID: core.NewULID().String()}

	_, err := pluginauthz.CheckPrincipalOwnership("not-a-ulid", actor)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "INVALID_PRINCIPAL_ID")
}
