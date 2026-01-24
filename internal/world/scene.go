// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import "errors"

// ParticipantRole identifies a character's role in a scene.
type ParticipantRole string

// Participant roles.
const (
	RoleOwner   ParticipantRole = "owner"
	RoleMember  ParticipantRole = "member"
	RoleInvited ParticipantRole = "invited"
)

// String returns the string representation of the role.
func (r ParticipantRole) String() string {
	return string(r)
}

// ErrInvalidParticipantRole indicates an unrecognized participant role.
var ErrInvalidParticipantRole = errors.New("invalid participant role")

// Validate checks that the role is a valid participant role.
func (r ParticipantRole) Validate() error {
	switch r {
	case RoleOwner, RoleMember, RoleInvited:
		return nil
	default:
		return ErrInvalidParticipantRole
	}
}

// ValidParticipantRoles returns all valid participant roles.
func ValidParticipantRoles() []ParticipantRole {
	return []ParticipantRole{RoleOwner, RoleMember, RoleInvited}
}
