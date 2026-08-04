// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestStatusConstantsAreTheExactLowercaseStoredVocabulary(t *testing.T) {
	assert.Equal(t, "active", string(world.StatusActive))
	assert.Equal(t, "retired", string(world.StatusRetired))
	assert.Equal(t, "idle", string(world.StatusIdle))
}

func TestParseStatusAcceptsEachMemberOfTheClosedVocabulary(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  world.Status
	}{
		{"returns StatusActive for active", "active", world.StatusActive},
		{"returns StatusRetired for retired", "retired", world.StatusRetired},
		{"returns StatusIdle for idle", "idle", world.StatusIdle},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := world.ParseStatus(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseStatusRejectsCaseVariantsPaddingAndAbsence(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"rejects title case Retired because the comparison does not case-fold", "Retired"},
		{"rejects upper case RETIRED because the comparison does not case-fold", "RETIRED"},
		{"rejects leading-space retired because the comparison does not trim", " retired"},
		{"rejects trailing-space active because the comparison does not trim", "active "},
		{"rejects the empty string because there is no unknown state", ""},
		{"rejects a token outside the closed vocabulary", "deleted"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := world.ParseStatus(tt.input)
			require.Error(t, err)
			errutil.AssertErrorCode(t, err, "INVALID_CHARACTER_STATUS")
			assert.Empty(t, string(got))
		})
	}
}

func TestSelectableAdmitsOnlyActive(t *testing.T) {
	tests := []struct {
		name  string
		input world.Status
		want  bool
	}{
		{"selects an active character", world.StatusActive, true},
		{"refuses a retired character", world.StatusRetired, false},
		{"refuses an idle character", world.StatusIdle, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, world.Selectable(tt.input))
		})
	}
}

// The default arm is what makes the closed vocabulary safe to extend: a value
// added to the type later is excluded by the same code path that excludes
// retired, with no second predicate to keep in sync.
func TestSelectableRefusesAValueOutsideTheClosedVocabulary(t *testing.T) {
	assert.False(t, world.Selectable(world.Status("archived")))
	assert.False(t, world.Selectable(world.Status("ACTIVE")))
	assert.False(t, world.Selectable(world.Status("")))
}

func TestNeverActiveIsTheZeroEpochSentinel(t *testing.T) {
	assert.Equal(t, int64(0), world.NeverActive)
}

func TestNewCharacterStartsActiveAndNeverActive(t *testing.T) {
	char, err := world.NewCharacter(ulid.Make(), "Lifecycle Probe")
	require.NoError(t, err)
	assert.Equal(t, world.StatusActive, char.Status)
	assert.Equal(t, world.NeverActive, char.LastActiveAt)
}

func TestCharacterValidateRejectsAStatusOutsideTheClosedVocabulary(t *testing.T) {
	char, err := world.NewCharacter(ulid.Make(), "Lifecycle Probe")
	require.NoError(t, err)

	char.Status = world.Status("archived")
	require.Error(t, char.Validate())

	char.Status = ""
	require.Error(t, char.Validate())

	char.Status = world.StatusRetired
	require.NoError(t, char.Validate())
}
