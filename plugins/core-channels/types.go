// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import "regexp"

// channelType is the visibility/access class of a channel.
//
//	public  — discoverable and open to any character.
//	private — invitation/membership-gated (player-level ABAC, D-02).
//	admin   — staff oversight channel; never downgradeable to a normal type.
type channelType string

// Channel type constants.
const (
	channelTypePublic  channelType = "public"
	channelTypePrivate channelType = "private"
	channelTypeAdmin   channelType = "admin"
)

// IsValid reports whether t is a recognized channel type.
func (t channelType) IsValid() bool {
	switch t {
	case channelTypePublic, channelTypePrivate, channelTypeAdmin:
		return true
	}
	return false
}

// IsValidChannelTypeTransition reports whether a channel may change from `from`
// to `to`.
//
//	public  <-> private
//	admin    -> admin (terminal; an admin channel is never downgraded)
//
// Transitions INTO admin are forbidden — promoting a normal channel to the
// staff-oversight class would be a privilege escalation. Self-transitions are
// rejected: they are meaningless and would mask bugs in the caller.
func IsValidChannelTypeTransition(from, to channelType) bool {
	if !from.IsValid() || !to.IsValid() || from == to {
		return false
	}
	switch from {
	case channelTypePublic:
		return to == channelTypePrivate
	case channelTypePrivate:
		return to == channelTypePublic
	case channelTypeAdmin:
		return false // admin is terminal; no downgrade path this phase
	}
	return false
}

// channelRole is a character's role within a channel.
//
// Only `owner` and `member` are usable this phase. `op` is a DORMANT reserved
// value (D-05): op/deop delegation is deferred. The DB CHECK includes 'op' so a
// future phase can activate it with no migration, but IsValid rejects it here so
// no code path stamps or trusts an op role before the delegation feature exists.
type channelRole string

// Channel role constants. channelRoleOp is reserved/dormant (D-05).
const (
	channelRoleOwner  channelRole = "owner"
	channelRoleMember channelRole = "member"
	channelRoleOp     channelRole = "op" // dormant (D-05); not usable this phase
)

// IsValid reports whether r is a role usable this phase. `op` is reserved and
// intentionally returns false until op/deop delegation ships.
func (r channelRole) IsValid() bool {
	switch r {
	case channelRoleOwner, channelRoleMember:
		return true
	}
	return false
}

// channelState is the lifecycle state of a channel, derived from the archived
// column. A channel is `active` until it is soft-archived; archival is terminal
// this phase (there is no un-archive path — spec §specifics soft delete).
type channelState string

// Channel state constants.
const (
	channelStateActive   channelState = "active"
	channelStateArchived channelState = "archived"
)

// IsValid reports whether s is a recognized channel state.
func (s channelState) IsValid() bool {
	switch s {
	case channelStateActive, channelStateArchived:
		return true
	}
	return false
}

// IsValidChannelStateTransition reports whether a channel may transition from
// `from` to `to`. The only valid transition is active -> archived (soft
// delete). Archived is terminal; self-transitions are rejected.
func IsValidChannelStateTransition(from, to channelState) bool {
	if !from.IsValid() || !to.IsValid() || from == to {
		return false
	}
	return from == channelStateActive && to == channelStateArchived
}

// channelNamePattern is the accepted channel-name grammar: an alphanumeric
// leading character followed by up to 31 more alphanumeric, underscore, or
// hyphen characters (1–32 chars total). Uniqueness is enforced
// case-insensitively at the store boundary (T-01-10).
var channelNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,31}$`)

// validateChannelName reports whether name is an acceptable channel name.
func validateChannelName(name string) bool {
	return channelNamePattern.MatchString(name)
}
