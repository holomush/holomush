// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSceneStateIsValidReturnsTrueForKnownStates(t *testing.T) {
	cases := []SceneState{
		SceneStateActive,
		SceneStatePaused,
		SceneStateEnded,
		SceneStateArchived,
	}
	for _, s := range cases {
		if !s.IsValid() {
			t.Errorf("SceneState(%q).IsValid() = false, want true", s)
		}
	}
}

func TestSceneStateIsValidReturnsFalseForUnknownState(t *testing.T) {
	if SceneState("bogus").IsValid() {
		t.Error("SceneState(\"bogus\").IsValid() = true, want false")
	}
}

func TestSceneVisibilityIsValidReturnsTrueForKnownVisibilities(t *testing.T) {
	cases := []SceneVisibility{SceneVisibilityOpen, SceneVisibilityPrivate}
	for _, v := range cases {
		if !v.IsValid() {
			t.Errorf("SceneVisibility(%q).IsValid() = false, want true", v)
		}
	}
}

func TestSceneVisibilityIsValidReturnsFalseForUnknownVisibility(t *testing.T) {
	if SceneVisibility("bogus").IsValid() {
		t.Error("SceneVisibility(\"bogus\").IsValid() = true, want false")
	}
}

func TestPoseOrderModeIsValidReturnsTrueForKnownModes(t *testing.T) {
	cases := []PoseOrderMode{PoseOrderModeFree, PoseOrderModeStrict, PoseOrderMode3PR, PoseOrderMode5PR}
	for _, m := range cases {
		if !m.IsValid() {
			t.Errorf("PoseOrderMode(%q).IsValid() = false, want true", m)
		}
	}
}

func TestPoseOrderModeIsValidReturnsFalseForUnknownMode(t *testing.T) {
	if PoseOrderMode("bogus").IsValid() {
		t.Error("PoseOrderMode(\"bogus\").IsValid() = true, want false")
	}
}

func TestParticipantsWithPoseMeta_ZeroValueValid(t *testing.T) {
	t.Parallel()
	var pm ParticipantsWithPoseMeta
	assert.Equal(t, uint32(0), pm.TotalPoseCount)
	assert.Empty(t, pm.Participants)
}

func TestParticipantWithPoseMeta_NeverPosed_NilFields(t *testing.T) {
	t.Parallel()
	p := ParticipantWithPoseMeta{
		CharacterID: "char-alice",
		JoinedAt:    time.Now(),
	}
	assert.Nil(t, p.LastPoseAt)
	assert.Nil(t, p.LastPoseSeq)
}
