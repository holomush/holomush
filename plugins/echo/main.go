// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package main implements an echo bot plugin for HoloMUSH.
// This plugin responds to say events by echoing the message back.
//
// Build with TinyGo:
//
//	tinygo build -o echo.wasm -target=wasi ./plugins/echo
//
// The plugin must export:
//   - alloc(size i32) -> ptr i32: Allocate memory for host to write data
//   - handle_event(ptr i32, len i32) -> packed i64: Handle event, return response
//
// The packed return value contains the response pointer in upper 32 bits
// and length in lower 32 bits. Return 0 for no response.
package main

import (
	"encoding/json"
	"unsafe"
)

// Event matches the plugin.Event structure from pkg/plugin/event.go.
type Event struct {
	ID        string `json:"id"`
	Stream    string `json:"stream"`
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp"`
	ActorKind uint8  `json:"actor_kind"`
	ActorID   string `json:"actor_id"`
	Payload   string `json:"payload"`
}

// Response matches the plugin.Response structure.
type Response struct {
	Events []EmitEvent `json:"events,omitempty"`
}

// EmitEvent matches the plugin.EmitEvent structure.
type EmitEvent struct {
	Stream  string `json:"stream"`
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

// SayPayload is the JSON structure for say events.
type SayPayload struct {
	Message string `json:"message"`
}

// Memory management for WASM
var (
	allocOffset uint32 = 1024 // Start allocating after 1KB
)

//export alloc
func alloc(size uint32) uint32 {
	ptr := allocOffset
	allocOffset += size
	return ptr
}

//export handle_event
func handleEvent(ptr, length uint32) uint64 {
	// Read event JSON from memory
	eventJSON := make([]byte, length)
	for i := uint32(0); i < length; i++ {
		eventJSON[i] = *(*byte)(unsafe.Pointer(uintptr(ptr + i)))
	}

	var event Event
	if err := json.Unmarshal(eventJSON, &event); err != nil {
		return 0
	}

	// Only respond to say events
	if event.Type != "say" {
		return 0
	}

	// Don't respond to our own events (ActorKind 2 = plugin)
	if event.ActorKind == 2 {
		return 0
	}

	// Parse the say payload
	var payload SayPayload
	if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil {
		return 0
	}

	// Create echo response
	echoPayload, _ := json.Marshal(SayPayload{
		Message: "Echo: " + payload.Message,
	})

	response := Response{
		Events: []EmitEvent{{
			Stream:  event.Stream,
			Type:    "say",
			Payload: string(echoPayload),
		}},
	}

	respJSON, err := json.Marshal(response)
	if err != nil {
		return 0
	}

	// Allocate and write response to memory
	respPtr := alloc(uint32(len(respJSON)))
	for i, b := range respJSON {
		*(*byte)(unsafe.Pointer(uintptr(respPtr + uint32(i)))) = b
	}

	// Pack ptr (upper 32 bits) and len (lower 32 bits)
	return (uint64(respPtr) << 32) | uint64(len(respJSON))
}

func main() {}
