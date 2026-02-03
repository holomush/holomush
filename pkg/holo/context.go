// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package holo

import "encoding/json"

// CommandContext provides pre-parsed command data to plugin handlers.
// This replaces brittle JSON parsing in plugins with structured access.
//
// Lua plugins receive this as a table with fields:
//
//	ctx.name           -- "say"
//	ctx.args           -- "Hello everyone!"
//	ctx.invoked_as     -- ";" or original command
//	ctx.character_name -- "Alice"
//	ctx.character_id   -- "01ABC..."
//	ctx.location_id    -- "01DEF..."
//	ctx.player_id      -- "01GHI..."
type CommandContext struct {
	// Name is the canonical command name (e.g., "say", "pose", "emit").
	Name string

	// Args is the argument string after the command name.
	// May be empty for commands that don't require arguments.
	Args string

	// InvokedAs is the original command text before alias resolution.
	// For prefix aliases like ";" or ":", this indicates which variant was used.
	// For regular commands, this matches Name.
	InvokedAs string

	// CharacterName is the display name of the character executing the command.
	CharacterName string

	// CharacterID is the ULID of the character executing the command.
	CharacterID string

	// LocationID is the ULID of the character's current location.
	LocationID string

	// PlayerID is the ULID of the player who owns the character.
	PlayerID string
}

// commandPayload mirrors the JSON structure of command event payloads.
// This is an internal type for JSON unmarshaling.
type commandPayload struct {
	Name          string `json:"name"`
	Args          string `json:"args"`
	InvokedAs     string `json:"invoked_as"`
	CharacterName string `json:"character_name"`
	CharacterID   string `json:"character_id"`
	LocationID    string `json:"location_id"`
	PlayerID      string `json:"player_id"`
}

// ParseCommandPayload parses a JSON command payload into a CommandContext.
// Returns a zero-value CommandContext if the payload is empty or invalid JSON.
func ParseCommandPayload(payload string) CommandContext {
	if payload == "" {
		return CommandContext{}
	}

	var cp commandPayload
	if err := json.Unmarshal([]byte(payload), &cp); err != nil {
		return CommandContext{}
	}

	return CommandContext(cp)
}
