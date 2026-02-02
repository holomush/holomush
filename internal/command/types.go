// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package command provides the command registry, parser, and dispatch system.
package command

import (
	"context"
	"io"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
)

// CommandHandler is the function signature for command handlers.
//
//nolint:revive // Name matches design spec; consistency with spec takes precedence over stutter avoidance
type CommandHandler func(ctx context.Context, exec *CommandExecution) error

// CommandEntry represents a registered command in the unified registry.
//
//nolint:revive // Name matches design spec; consistency with spec takes precedence over stutter avoidance
type CommandEntry struct {
	Name         string         // canonical name (e.g., "say")
	Handler      CommandHandler // Go handler or Lua dispatcher
	Capabilities []string       // ALL required capabilities (AND logic)
	Help         string         // short description (one line)
	Usage        string         // usage pattern (e.g., "say <message>")
	HelpText     string         // detailed markdown help
	Source       string         // "core" or plugin name
}

// CommandExecution provides context for command execution.
//
//nolint:revive // Name matches design spec; consistency with spec takes precedence over stutter avoidance
type CommandExecution struct {
	CharacterID   ulid.ULID
	LocationID    ulid.ULID
	CharacterName string
	PlayerID      ulid.ULID
	SessionID     ulid.ULID
	Args          string
	Output        io.Writer
	Services      *Services
}

// Services provides access to core services for command handlers.
// Handlers MUST NOT store references to services beyond execution.
// Handlers MUST access services only through exec.Services.
type Services struct {
	World   *world.Service       // world model queries and mutations
	Session *core.SessionManager // session management
	Access  access.AccessControl // authorization checks
	Events  core.EventStore      // event persistence
}
