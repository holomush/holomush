// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"github.com/holomush/holomush/internal/command"
)

// RegisterAll registers all core command handlers with the registry.
// Core commands are those implemented in Go as part of the server.
// Panics if any registration fails (indicates a programming error).
func RegisterAll(reg *command.Registry) {
	mustRegister := func(entry command.CommandEntry) {
		if err := reg.Register(entry); err != nil {
			panic("failed to register core command " + entry.Name + ": " + err.Error())
		}
	}

	// Navigation commands
	mustRegister(command.CommandEntry{
		Name:    "look",
		Handler: LookHandler,
		Help:    "Look at your surroundings or a target",
		Usage:   "look [target]",
		HelpText: `## Look

Examine your surroundings or a specific target.

### Usage

- ` + "`look`" + ` - View the current location
- ` + "`look <target>`" + ` - Examine a specific target

### Examples

- ` + "`look`" + ` - Shows the room name and description
- ` + "`look sign`" + ` - Examine the sign in the room`,
		Source: "core",
	})

	mustRegister(command.CommandEntry{
		Name:    "move",
		Handler: MoveHandler,
		Help:    "Move through an exit",
		Usage:   "move <direction>",
		HelpText: `## Move

Move through an exit to another location.

### Usage

- ` + "`move <direction>`" + ` - Move through the named exit
- ` + "`<direction>`" + ` - Shortcut for move (if direction matches an exit)

### Examples

- ` + "`move north`" + ` or ` + "`north`" + ` - Move north
- ` + "`move out`" + ` - Move through the "out" exit`,
		Source: "core",
	})

	// Session commands
	mustRegister(command.CommandEntry{
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

	mustRegister(command.CommandEntry{
		Name:    "who",
		Handler: WhoHandler,
		Help:    "See who is online",
		Usage:   "who",
		HelpText: `## Who

Display a list of all connected players.

Shows character names and how long they've been connected.

### Usage

- ` + "`who`" + ` - List all online players`,
		Source: "core",
	})

	// Admin commands
	mustRegister(command.CommandEntry{
		Name:         "boot",
		Handler:      BootHandler,
		Capabilities: []string{"admin.boot"},
		Help:         "Disconnect a player",
		Usage:        "boot <character> [reason]",
		HelpText: `## Boot

Forcibly disconnect a player from the game.

### Usage

- ` + "`boot <character>`" + ` - Disconnect the named character
- ` + "`boot <character> <reason>`" + ` - Disconnect with a message

### Examples

- ` + "`boot TroubleUser`" + `
- ` + "`boot TroubleUser AFK for too long`" + `

### Permissions

Requires the ` + "`admin.boot`" + ` capability.`,
		Source: "core",
	})

	mustRegister(command.CommandEntry{
		Name:         "shutdown",
		Handler:      ShutdownHandler,
		Capabilities: []string{"admin.shutdown"},
		Help:         "Shut down the server",
		Usage:        "shutdown [delay_seconds]",
		HelpText: `## Shutdown

Initiate a server shutdown.

### Usage

- ` + "`shutdown`" + ` - Immediate shutdown
- ` + "`shutdown <seconds>`" + ` - Shutdown after delay

### Examples

- ` + "`shutdown`" + ` - Shut down immediately
- ` + "`shutdown 60`" + ` - Shut down in 60 seconds

### Permissions

Requires the ` + "`admin.shutdown`" + ` capability.`,
		Source: "core",
	})

	mustRegister(command.CommandEntry{
		Name:         "wall",
		Handler:      WallHandler,
		Capabilities: []string{"admin.wall"},
		Help:         "Broadcast a message to all players",
		Usage:        "wall [urgency] <message>",
		HelpText: `## Wall

Send a broadcast message to all connected players.

### Usage

- ` + "`wall <message>`" + ` - Send an info-level announcement
- ` + "`wall info <message>`" + ` - Same as above (explicit)
- ` + "`wall warning <message>`" + ` - Send a warning message
- ` + "`wall critical <message>`" + ` - Send a critical alert

### Urgency Levels

- ` + "`info`" + ` - Normal announcements (default)
- ` + "`warning`" + ` - Important notices
- ` + "`critical`" + ` - Urgent alerts

### Examples

- ` + "`wall Server restart in 10 minutes`" + `
- ` + "`wall warning Database maintenance starting soon`" + `

### Permissions

Requires the ` + "`admin.wall`" + ` capability.`,
		Source: "core",
	})

	// Object commands
	mustRegister(command.CommandEntry{
		Name:         "create",
		Handler:      CreateHandler,
		Capabilities: []string{"objects.create"},
		Help:         "Create a new object",
		Usage:        "create <name>",
		HelpText: `## Create

Create a new object in your inventory.

### Usage

- ` + "`create <name>`" + ` - Create an object with the given name

### Examples

- ` + "`create sword`" + ` - Create a new sword

### Permissions

Requires the ` + "`objects.create`" + ` capability.`,
		Source: "core",
	})

	mustRegister(command.CommandEntry{
		Name:         "set",
		Handler:      SetHandler,
		Capabilities: []string{"objects.set"},
		Help:         "Set a property on an object",
		Usage:        "set <object>=<property>:<value>",
		HelpText: `## Set

Modify a property on an object, location, or character.

### Usage

- ` + "`set <target>=<property>:<value>`" + `

### Examples

- ` + "`set here=description:A cozy room.`" + ` - Set room description
- ` + "`set sword=description:A gleaming blade.`" + ` - Set object description

### Permissions

Requires the ` + "`objects.set`" + ` capability.`,
		Source: "core",
	})
}
