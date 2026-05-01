// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package coreobj holds plugin-owned types and constants for the
// core-objects plugin. The plugin runtime is Lua (main.lua); this
// Go package provides typed event-type constants for host-side call
// sites that emit object events on behalf of the plugin.
//
// Per spec §7.1, event-type identifiers are qualified <plugin>:<type>
// when crossing plugin boundaries.
package coreobj

// EventType is a string identifier for events emitted by the
// core-objects plugin.
type EventType string

// Event-type constants. All are qualified with the plugin name.
const (
	EventTypeObjectCreate  EventType = "core-objects:object_create"
	EventTypeObjectDestroy EventType = "core-objects:object_destroy"
	EventTypeObjectUse     EventType = "core-objects:object_use"
	EventTypeObjectExamine EventType = "core-objects:object_examine"
	EventTypeObjectGive    EventType = "core-objects:object_give"
)
