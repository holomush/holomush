// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

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

// Validate checks that the role is a valid participant role.
func (r ParticipantRole) Validate() error {
	switch r {
	case RoleOwner, RoleMember, RoleInvited:
		return nil
	default:
		return &ValidationError{Field: "role", Message: "invalid participant role"}
	}
}

// ValidParticipantRoles returns all valid participant roles.
func ValidParticipantRoles() []ParticipantRole {
	return []ParticipantRole{RoleOwner, RoleMember, RoleInvited}
}
