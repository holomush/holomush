// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import "github.com/holomush/holomush/internal/pgnanos"

// ParticipantRole represents a character's relationship to a scene.
//
// Per design decision P3.D1, `invited` is a transient role that exists only
// on private scenes. An invitation is a row that grants the holder permission
// to join (and to read scene metadata in a later phase). Calling JoinScene on
// an invited scene atomically promotes the row to `member`. There is no
// `invited` row on open scenes.
type ParticipantRole string

const (
	ParticipantRoleOwner   ParticipantRole = "owner"
	ParticipantRoleMember  ParticipantRole = "member"
	ParticipantRoleInvited ParticipantRole = "invited"
	// ParticipantRoleObserver marks a watching, non-acting participant (E9.5
	// observer auto-join, INV-SCENE-61): present in the roster, excluded from
	// the emit path, pose order, and publish votes.
	ParticipantRoleObserver ParticipantRole = "observer"
)

// IsValid reports whether r is a recognized participant role.
func (r ParticipantRole) IsValid() bool {
	switch r {
	case ParticipantRoleOwner, ParticipantRoleMember, ParticipantRoleInvited, ParticipantRoleObserver:
		return true
	}
	return false
}

// ParticipantRow is the persistence-layer representation of a row in
// scene_participants. The shape matches the table column-for-column.
type ParticipantRow struct {
	SceneID     string
	CharacterID string
	Role        string
	JoinedAt    pgnanos.Time
}

// ParticipantOpResult captures the outcome of an AddParticipant call. The
// service handler uses this to decide whether to emit a membership.join
// ops event (only OpInserted and OpPromoted should emit; OpNoChange must
// not, to keep retries from polluting the audit log).
type ParticipantOpResult int

const (
	// OpInserted indicates a fresh row was added to scene_participants.
	OpInserted ParticipantOpResult = iota
	// OpPromoted indicates an existing row was flipped from invited to member.
	OpPromoted
	// OpNoChange indicates the caller was already a member or owner; the
	// upsert was a no-op.
	OpNoChange
	// ParticipantUpgraded indicates an existing observer row was upgraded to
	// member via AddParticipant on an open scene. The row's role is now
	// "member"; the store records an OpsKindMembershipJoin ops event in-tx.
	ParticipantUpgraded
)

// ObserverAddResult classifies AddObserver outcomes.
type ObserverAddResult int

const (
	// ObserverAdded indicates a fresh role=observer row was inserted.
	ObserverAdded ObserverAddResult = iota
	// ObserverAlreadyParticipant indicates the character already has a row of
	// any role in the scene; the existing row is returned unchanged.
	ObserverAlreadyParticipant
	// ObserverSceneNotFound indicates no scene with the given ID exists.
	ObserverSceneNotFound
	// ObserverSceneNotOpen indicates the scene's visibility is not "open";
	// observer auto-join requires an open scene (INV-SCENE-61).
	ObserverSceneNotOpen
	// ObserverSceneNotActive indicates the scene is not in an active or paused
	// state; observers may only join live scenes.
	ObserverSceneNotActive
)
