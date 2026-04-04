// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"github.com/holomush/holomush/internal/command"
)

// RegisterAdmin registers admin command handlers that require auth dependencies.
// These handlers use closure injection rather than extending command.Services.
func RegisterAdmin(reg *command.Registry, deps AdminDeps) {
	switch {
	case deps.PlayerRepo == nil:
		panic("missing admin dependency: PlayerRepo")
	case deps.Hasher == nil:
		panic("missing admin dependency: Hasher")
	case deps.PlayerSessions == nil:
		panic("missing admin dependency: PlayerSessions")
	case deps.ResetRepo == nil:
		panic("missing admin dependency: ResetRepo")
	case deps.CharLister == nil:
		panic("missing admin dependency: CharLister")
	}

	mustRegister := func(cfg command.CommandEntryConfig) {
		entry, err := command.NewCommandEntry(cfg)
		if err != nil {
			panic("failed to create admin command " + cfg.Name + ": " + err.Error())
		}
		if err := reg.Register(*entry); err != nil {
			panic("failed to register admin command " + cfg.Name + ": " + err.Error())
		}
	}

	mustRegister(command.CommandEntryConfig{
		Name:    "resetpassword",
		Handler: NewResetPasswordHandler(deps),
		Capabilities: []command.Capability{
			{Action: "write", Resource: "player", Scope: command.ScopeGlobal},
		},
		Help:  "Reset a player's password",
		Usage: "resetpassword <player> [password] [--kick]",
		HelpText: `## Reset Password

Reset a player's password. Generates a random password if none provided.

### Usage

- ` + "`resetpassword <player>`" + ` - Generate a new random password
- ` + "`resetpassword <player> <password>`" + ` - Set a specific password
- ` + "`resetpassword <player> --kick`" + ` - Reset and disconnect active sessions

### Capabilities

Requires write access to the player resource at global scope.`,
		Source: "core",
	})
}

// RegisterAll registers the compiled-in command handlers with the registry.
// Only quit and shutdown remain as compiled-in handlers; all other commands
// have been migrated to core plugins under plugins/core-*.
func RegisterAll(reg *command.Registry) {
	mustRegister := func(cfg command.CommandEntryConfig) {
		entry, err := command.NewCommandEntry(cfg)
		if err != nil {
			panic("failed to create core command " + cfg.Name + ": " + err.Error())
		}
		if err := reg.Register(*entry); err != nil {
			panic("failed to register core command " + cfg.Name + ": " + err.Error())
		}
	}

	mustRegister(command.CommandEntryConfig{
		Name:    "quit",
		Handler: QuitHandler,
		Help:    "Disconnect from the game",
		Usage:   "quit",
		HelpText: `## Quit

Disconnect your session from the game.

Your character remains in-world but becomes inactive.

### Usage

- ` + "`quit`" + ` - End your session`,
		Source: "core",
	})

	mustRegister(command.CommandEntryConfig{
		Name:    "shutdown",
		Handler: ShutdownHandler,
		Capabilities: []command.Capability{
			{Action: "admin", Resource: "server", Scope: command.ScopeGlobal},
		},
		Help:  "Shut down the server",
		Usage: "shutdown [delay_seconds]",
		HelpText: `## Shutdown

Initiate a server shutdown.

### Usage

- ` + "`shutdown`" + ` - Immediate shutdown
- ` + "`shutdown <seconds>`" + ` - Shutdown after delay

### Examples

- ` + "`shutdown`" + ` - Shut down immediately
- ` + "`shutdown 60`" + ` - Shut down in 60 seconds

### Permissions

Requires admin action on the server resource at global scope.`,
		Source: "core",
	})
}
