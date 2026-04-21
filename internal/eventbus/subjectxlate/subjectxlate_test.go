// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package subjectxlate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/subjectxlate"
)

func TestLegacyPrependsEventsAndGameIDAndReplacesColons(t *testing.T) {
	out, err := subjectxlate.Legacy("character:01ABC", "main")
	require.NoError(t, err)
	assert.Equal(t, "events.main.character.01ABC", out)
}

func TestLegacyPassthroughWhenAlreadyNative(t *testing.T) {
	out, err := subjectxlate.Legacy("events.main.scene.42.ic", "main")
	require.NoError(t, err)
	assert.Equal(t, "events.main.scene.42.ic", out)
}

func TestLegacyRejectsEmptyGameID(t *testing.T) {
	_, err := subjectxlate.Legacy("character:X", "")
	require.Error(t, err)
}

func TestLegacyRejectsEmptyToken(t *testing.T) {
	_, err := subjectxlate.Legacy("character::X", "main")
	require.Error(t, err)
}

func TestToLegacyStripsEventsAndGameIDAndRestoresColons(t *testing.T) {
	assert.Equal(t, "character:01ABC",
		subjectxlate.ToLegacy("events.main.character.01ABC", "main"))
}

func TestToLegacyPassthroughForUnprefixedSubject(t *testing.T) {
	assert.Equal(t, "character:01ABC",
		subjectxlate.ToLegacy("character:01ABC", "main"))
}

func TestToLegacyFallsBackWhenGameIDDiffersButPrefixIsEvents(t *testing.T) {
	// Falls back to stripping the first token (assumed game id) and
	// joining the remainder by ':'.
	assert.Equal(t, "character:01ABC",
		subjectxlate.ToLegacy("events.other.character.01ABC", "main"))
}
