// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"

// RegisterBuiltinTypes registers all built-in event types in the VerbRegistry.
// Built-in types use the same registration path as plugins -- no special cases.
func RegisterBuiltinTypes(r *VerbRegistry) error {
	// Host-owned event types only. Plugin-owned types (say/pose/whisper from
	// core-communication, object_* from core-objects) are registered by the
	// plugin loader from each plugin's manifest `verbs:` block — see
	// internal/plugin/manager.go:782 — keeping plugin-owned types out of
	// internal/core/ per the plugin-boundary discipline.
	builtins := []VerbRegistration{
		// Movement
		{Type: "arrive", Category: "movement", Format: "notification", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_BOTH, Source: "builtin"},
		{Type: "leave", Category: "movement", Format: "notification", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_BOTH, Source: "builtin"},
		{
			Type: "move", Category: "movement", Format: "notification", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_BOTH, Source: "builtin",
			MetadataKeys: []MetadataKey{
				{Key: "from_id", ValueType: "string"},
				{Key: "to_id", ValueType: "string"},
				{Key: "exit_name", ValueType: "string"},
			},
		},

		// State
		{
			Type: "location_state", Category: "state", Format: "snapshot", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_STATE, Source: "builtin",
			MetadataKeys: []MetadataKey{
				{Key: "location", ValueType: "object"},
				{Key: "exits", ValueType: "array"},
				{Key: "present", ValueType: "array"},
			},
		},
		{
			Type: "exit_update", Category: "state", Format: "delta", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_STATE, Source: "builtin",
			MetadataKeys: []MetadataKey{{Key: "exits", ValueType: "array"}},
		},

		// Command
		{Type: "command_response", Category: "command", Format: "narrative", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL, Source: "builtin"},
		{Type: "command_error", Category: "command", Format: "error", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL, Source: "builtin"},

		// System
		{Type: "system", Category: "system", Format: "notification", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL, Source: "builtin"},
	}

	for _, b := range builtins {
		if err := r.Register(b); err != nil {
			return err
		}
	}
	return nil
}
