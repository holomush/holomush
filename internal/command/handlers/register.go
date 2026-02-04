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
		Usage:        `create <type> "<name>"`,
		HelpText: `## Create

Create a new object or location with the specified name.

### Usage

- ` + "`create <type> \"<name>\"`" + ` - Create an object or location with the given name (name must be quoted)

### Types

- ` + "`object`" + ` - A portable object
- ` + "`location`" + ` - A room or area

### Examples

- ` + "`create object \"Enchanted Sword\"`" + ` - Create a new object
- ` + "`create location \"The Library\"`" + ` - Create a new location

### Permissions

Requires the ` + "`objects.create`" + ` capability.`,
		Source: "core",
	})

	mustRegister(command.CommandEntry{
		Name:         "set",
		Handler:      SetHandler,
		Capabilities: []string{"objects.set"},
		Help:         "Set a property on an object",
		Usage:        "set <property> of <target> to <value>",
		HelpText: `## Set

Modify a property on an object, location, or character.

### Usage

- ` + "`set <property> of <target> to <value>`" + `

The property name supports prefix matching (e.g., "desc" matches "description").

### Examples

- ` + "`set description of here to \"A dusty room\"`" + ` - Set room description
- ` + "`set description of sword to \"A gleaming blade\"`" + ` - Set object description
- ` + "`set desc of #123 to \"A mysterious place\"`" + ` - Set description using object ID

### Permissions

Requires the ` + "`objects.set`" + ` capability.`,
		Source: "core",
	})

	// Player alias commands
	mustRegister(command.CommandEntry{
		Name:         "alias",
		Handler:      AliasAddHandler,
		Capabilities: []string{"player.alias"},
		Help:         "Add a personal alias",
		Usage:        "alias <name>=<command>",
		HelpText: `## Alias

Create or update a personal command alias.

### Usage

- ` + "`alias <name>=<command>`" + ` - Create an alias

### Examples

- ` + "`alias l=look`" + ` - Create shortcut for look
- ` + "`alias n=north`" + ` - Create shortcut for north
- ` + "`alias aa=attack all`" + ` - Create alias with arguments

### Notes

- Personal aliases take precedence over system aliases
- Warnings are shown if your alias shadows an existing command or system alias
- Circular alias chains are automatically rejected

### Permissions

Requires the ` + "`player.alias`" + ` capability.`,
		Source: "core",
	})

	mustRegister(command.CommandEntry{
		Name:         "unalias",
		Handler:      AliasRemoveHandler,
		Capabilities: []string{"player.alias"},
		Help:         "Remove a personal alias",
		Usage:        "unalias <name>",
		HelpText: `## Unalias

Remove a personal command alias.

### Usage

- ` + "`unalias <name>`" + ` - Remove the named alias

### Examples

- ` + "`unalias l`" + ` - Remove the 'l' alias

### Permissions

Requires the ` + "`player.alias`" + ` capability.`,
		Source: "core",
	})

	mustRegister(command.CommandEntry{
		Name:         "aliases",
		Handler:      AliasListHandler,
		Capabilities: []string{"player.alias"},
		Help:         "List your personal aliases",
		Usage:        "aliases",
		HelpText: `## Aliases

Display all your personal command aliases.

### Usage

- ` + "`aliases`" + ` - List all your aliases

### Permissions

Requires the ` + "`player.alias`" + ` capability.`,
		Source: "core",
	})

	// System alias commands
	mustRegister(command.CommandEntry{
		Name:         "sysalias",
		Handler:      SysaliasAddHandler,
		Capabilities: []string{"admin.alias"},
		Help:         "Add a system alias",
		Usage:        "sysalias <name>=<command>",
		HelpText: `## Sysalias

Create or update a system-wide command alias.

### Usage

- ` + "`sysalias <name>=<command>`" + ` - Create a system alias

### Examples

- ` + "`sysalias l=look`" + ` - Create system shortcut for look
- ` + "`sysalias n=north`" + ` - Create system shortcut for north

### Notes

- System aliases apply to all players
- Cannot shadow an existing system alias (remove it first with sysunsalias)
- Warnings are shown if the alias shadows an existing command
- Circular alias chains are automatically rejected

### Permissions

Requires the ` + "`admin.alias`" + ` capability.`,
		Source: "core",
	})

	mustRegister(command.CommandEntry{
		Name:         "sysunsalias",
		Handler:      SysaliasRemoveHandler,
		Capabilities: []string{"admin.alias"},
		Help:         "Remove a system alias",
		Usage:        "sysunsalias <name>",
		HelpText: `## Sysunsalias

Remove a system-wide command alias.

### Usage

- ` + "`sysunsalias <name>`" + ` - Remove the named system alias

### Examples

- ` + "`sysunsalias l`" + ` - Remove the system 'l' alias

### Permissions

Requires the ` + "`admin.alias`" + ` capability.`,
		Source: "core",
	})

	mustRegister(command.CommandEntry{
		Name:         "sysaliases",
		Handler:      SysaliasListHandler,
		Capabilities: []string{"admin.alias"},
		Help:         "List all system aliases",
		Usage:        "sysaliases",
		HelpText: `## Sysaliases

Display all system-wide command aliases.

### Usage

- ` + "`sysaliases`" + ` - List all system aliases

### Permissions

Requires the ` + "`admin.alias`" + ` capability.`,
		Source: "core",
	})
}
