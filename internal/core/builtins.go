// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"

// BootstrapVerbRegistry returns a VerbRegistry seeded with host-owned event
// types. This is the single public path for obtaining a seeded registry in
// production. Use NewVerbRegistry() for tests that need an empty registry.
//
// hostVersion is the build-time version of the holomush core binary
// (e.g., "0.4.2-rc1" or "dev"). The bootstrapper records each builtin
// registration with source "builtin" and version "host-" + hostVersion
// so plugin-version drift is visible in events_audit replays.
func BootstrapVerbRegistry(hostVersion string) (*VerbRegistry, error) {
	r := NewVerbRegistry()
	if err := registerBuiltinTypes(r, hostVersion); err != nil {
		return nil, err
	}
	return r, nil
}

// registerBuiltinTypes (unexported) registers host-owned event types in the
// given registry. Called only by BootstrapVerbRegistry. Plugin-owned types
// (say/pose/whisper from core-communication, object_* from core-objects)
// are registered by the plugin loader from each plugin's manifest verbs:
// block — see internal/plugin/manager.go — keeping plugin-owned types out
// of internal/core/ per the plugin-boundary discipline.
func registerBuiltinTypes(r *VerbRegistry, hostVersion string) error {
	sourceVersion := "host-" + hostVersion
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
		if err := r.RegisterWithSource(b, sourceVersion); err != nil {
			return err
		}
	}
	return nil
}
