// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"

// RegisterBuiltinTypes registers all built-in event types in the VerbRegistry.
// Built-in types use the same registration path as plugins -- no special cases.
func RegisterBuiltinTypes(r *VerbRegistry) error {
	builtins := []VerbRegistration{
		// Communication
		{Type: "say", Category: "communication", Format: "speech", Label: "says", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL, Source: "builtin"},
		{
			Type: "pose", Category: "communication", Format: "action", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL, Source: "builtin",
			MetadataKeys: []MetadataKey{{Key: "no_space", ValueType: "bool", Description: "Suppress space between actor and text"}},
		},
		{Type: "page", Category: "communication", Format: "speech", Label: "pages", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL, Source: "builtin"},
		{Type: "whisper", Category: "communication", Format: "speech", Label: "whispers", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL, Source: "builtin"},
		{Type: "whisper_notice", Category: "communication", Format: "action", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL, Source: "builtin"},
		{Type: "ooc", Category: "communication", Format: "action", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL, Source: "builtin"},
		{Type: "pemit", Category: "command", Format: "narrative", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL, Source: "builtin"},

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
		{Type: "object_create", Category: "state", Format: "delta", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_STATE, Source: "builtin"},
		{Type: "object_destroy", Category: "state", Format: "delta", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_STATE, Source: "builtin"},

		// Command
		{Type: "command_response", Category: "command", Format: "narrative", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL, Source: "builtin"},
		{Type: "command_error", Category: "command", Format: "error", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL, Source: "builtin"},
		{Type: "object_use", Category: "command", Format: "narrative", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL, Source: "builtin"},
		{Type: "object_examine", Category: "command", Format: "narrative", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL, Source: "builtin"},
		{Type: "object_give", Category: "command", Format: "narrative", DisplayTarget: webv1.EventChannel_EVENT_CHANNEL_TERMINAL, Source: "builtin"},

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
