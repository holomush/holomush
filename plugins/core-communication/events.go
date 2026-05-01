// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package corecomm holds plugin-owned types and constants for the
// core-communication plugin. The plugin runtime is Lua (main.lua); this
// Go package provides typed event-type constants for host-side call
// sites that emit communication events on behalf of the plugin.
//
// Per spec §7.1, event-type identifiers are qualified <plugin>:<type>
// when crossing plugin boundaries.
package corecomm

// EventType is a string identifier for events emitted by the
// core-communication plugin.
type EventType string

// Event-type constants. All are qualified with the plugin name.
const (
	EventTypeEmit          EventType = "core-communication:emit"
	EventTypeOOC           EventType = "core-communication:ooc"
	EventTypePage          EventType = "core-communication:page"
	EventTypePemit         EventType = "core-communication:pemit"
	EventTypePose          EventType = "core-communication:pose"
	EventTypeSay           EventType = "core-communication:say"
	EventTypeWhisper       EventType = "core-communication:whisper"
	EventTypeWhisperNotice EventType = "core-communication:whisper_notice"
)
