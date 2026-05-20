// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import "time"

// SceneState represents the lifecycle state of a scene.
//
// Per spec section 1.2, the only valid transitions are:
//
//	active  -> paused | ended
//	paused  -> active | ended
//	ended   -> archived
//
// A scene MUST NOT transition backward.
type SceneState string

// Scene state constants.
const (
	SceneStateActive   SceneState = "active"
	SceneStatePaused   SceneState = "paused"
	SceneStateEnded    SceneState = "ended"
	SceneStateArchived SceneState = "archived"
)

// IsValid reports whether s is a recognized scene state.
func (s SceneState) IsValid() bool {
	switch s {
	case SceneStateActive, SceneStatePaused, SceneStateEnded, SceneStateArchived:
		return true
	}
	return false
}

// SceneVisibility controls who can discover and join a scene.
//
// Open scenes appear on the scene board and accept any join.
// Private scenes do not appear on the board and require an invitation.
type SceneVisibility string

// Scene visibility constants.
const (
	SceneVisibilityOpen    SceneVisibility = "open"
	SceneVisibilityPrivate SceneVisibility = "private"
)

// IsValid reports whether v is a recognized scene visibility.
func (v SceneVisibility) IsValid() bool {
	switch v {
	case SceneVisibilityOpen, SceneVisibilityPrivate:
		return true
	}
	return false
}

// PoseOrderMode controls how the plugin computes pose order from the IC stream.
// Phase 1 only persists the value; pose order computation lands in Phase 4.
type PoseOrderMode string

// Pose order constants.
const (
	PoseOrderModeFree   PoseOrderMode = "free"
	PoseOrderModeStrict PoseOrderMode = "strict"
	PoseOrderMode3PR    PoseOrderMode = "3pr"
	PoseOrderMode5PR    PoseOrderMode = "5pr"
)

// IsValid reports whether m is a recognized pose order mode.
func (m PoseOrderMode) IsValid() bool {
	switch m {
	case PoseOrderModeFree, PoseOrderModeStrict, PoseOrderMode3PR, PoseOrderMode5PR:
		return true
	}
	return false
}

// ParticipantsWithPoseMeta is the result returned by
// sceneStorer.ListParticipantsWithPoseMeta. Groups the per-scene
// total_pose_count with the per-participant pose metadata so the
// GetPoseOrder handler doesn't need a second SELECT for the scene row.
//
// See holomush-5rh.13 spec §7.3 (sceneStorer interface extensions) and
// ADR holomush-r4th (maintained pose-order metadata).
type ParticipantsWithPoseMeta struct {
	TotalPoseCount uint32
	Participants   []ParticipantWithPoseMeta
}

// ParticipantWithPoseMeta is one participant of a scene plus their
// Phase 4 maintained pose metadata. LastPoseAt and LastPoseSeq are nil
// when the participant has never posed in this scene.
type ParticipantWithPoseMeta struct {
	CharacterID string
	JoinedAt    time.Time
	LastPoseAt  *time.Time
	LastPoseSeq *int32
}
