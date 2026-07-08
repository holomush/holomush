// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import "testing"

func TestValidateChannelNameAcceptsWellFormedNames(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"accepts a simple lowercase name", "public", true},
		{"accepts mixed case", "Public", true},
		{"accepts digits and leading digit", "42chan", true},
		{"accepts underscores and hyphens after the first char", "ooc_chat-2", true},
		{"accepts a single character", "x", true},
		{"accepts exactly 32 characters", "a123456789012345678901234567890z", true},
		{"rejects an empty name", "", false},
		{"rejects a leading underscore", "_hidden", false},
		{"rejects a leading hyphen", "-lead", false},
		{"rejects 33 characters", "a1234567890123456789012345678901z", false},
		{"rejects spaces", "bad name", false},
		{"rejects punctuation", "bad!", false},
		{"rejects a colon (subject separator)", "chan:1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validateChannelName(tt.input); got != tt.want {
				t.Errorf("validateChannelName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestChannelTypeIsValid(t *testing.T) {
	tests := []struct {
		name  string
		input channelType
		want  bool
	}{
		{"public is valid", channelTypePublic, true},
		{"private is valid", channelTypePrivate, true},
		{"admin is valid", channelTypeAdmin, true},
		{"unknown is invalid", channelType("secret"), false},
		{"empty is invalid", channelType(""), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.input.IsValid(); got != tt.want {
				t.Errorf("channelType(%q).IsValid() = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsValidChannelTypeTransition(t *testing.T) {
	tests := []struct {
		name string
		from channelType
		to   channelType
		want bool
	}{
		{"public to private is allowed", channelTypePublic, channelTypePrivate, true},
		{"private to public is allowed", channelTypePrivate, channelTypePublic, true},
		{"public to admin is forbidden (escalation)", channelTypePublic, channelTypeAdmin, false},
		{"private to admin is forbidden (escalation)", channelTypePrivate, channelTypeAdmin, false},
		{"admin to public is forbidden (terminal)", channelTypeAdmin, channelTypePublic, false},
		{"admin to private is forbidden (terminal)", channelTypeAdmin, channelTypePrivate, false},
		{"self-transition is rejected", channelTypePublic, channelTypePublic, false},
		{"unknown source is rejected", channelType("x"), channelTypePublic, false},
		{"unknown target is rejected", channelTypePublic, channelType("x"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidChannelTypeTransition(tt.from, tt.to); got != tt.want {
				t.Errorf("IsValidChannelTypeTransition(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestChannelRoleIsValid(t *testing.T) {
	tests := []struct {
		name  string
		input channelRole
		want  bool
	}{
		{"owner is valid", channelRoleOwner, true},
		{"member is valid", channelRoleMember, true},
		{"op is dormant and not usable this phase", channelRoleOp, false},
		{"unknown is invalid", channelRole("god"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.input.IsValid(); got != tt.want {
				t.Errorf("channelRole(%q).IsValid() = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestChannelStateIsValid(t *testing.T) {
	tests := []struct {
		name  string
		input channelState
		want  bool
	}{
		{"active is valid", channelStateActive, true},
		{"archived is valid", channelStateArchived, true},
		{"unknown is invalid", channelState("deleted"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.input.IsValid(); got != tt.want {
				t.Errorf("channelState(%q).IsValid() = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsValidChannelStateTransition(t *testing.T) {
	tests := []struct {
		name string
		from channelState
		to   channelState
		want bool
	}{
		{"active to archived is the soft-delete transition", channelStateActive, channelStateArchived, true},
		{"archived to active is forbidden (terminal)", channelStateArchived, channelStateActive, false},
		{"self-transition is rejected", channelStateActive, channelStateActive, false},
		{"unknown target is rejected", channelStateActive, channelState("x"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidChannelStateTransition(tt.from, tt.to); got != tt.want {
				t.Errorf("IsValidChannelStateTransition(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}
