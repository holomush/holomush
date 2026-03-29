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
	mustRegister := func(cfg command.CommandEntryConfig) {
		entry, err := command.NewCommandEntry(cfg)
		if err != nil {
			panic("failed to create core command " + cfg.Name + ": " + err.Error())
		}
		if err := reg.Register(*entry); err != nil {
			panic("failed to register core command " + cfg.Name + ": " + err.Error())
		}
	}

	// Communication commands
	mustRegister(command.CommandEntryConfig{
		Name:         "page",
		Handler:      PageHandler,
		Capabilities: []string{"comms.page"},
		Help:         "Send a private message",
		Usage:        "page <name>=<message>",
		HelpText: `## Page

Send an OOC private message to another connected character.

### Usage

- ` + "`page <name>=<message>`" + ` - Send a private message
- ` + "`page <message>`" + ` - Page your last-paged character
- ` + "`page <name>=:<action>`" + ` - Pose-page (shows "From afar, ...")
- ` + "`page <name>=;<action>`" + ` - No-space pose-page

### Examples

- ` + "`page Alex=Hey, are you around?`" + `
- ` + "`page How's it going?`" + ` - Pages last-paged character
- ` + "`page Alex=:waves hello.`" + ` - Pose-page`,
		Source: "core",
	})

	mustRegister(command.CommandEntryConfig{
		Name:         "p",
		Handler:      PageHandler,
		Capabilities: []string{"comms.page"},
		Help:         "Send a private message (alias for page)",
		Usage:        "p <name>=<message>",
		Source:       "core",
	})

	mustRegister(command.CommandEntryConfig{
		Name:    "say",
		Handler: SayHandler,
		Help:    "Say something to the room",
		Usage:   "say <message>",
		HelpText: `## Say

Say something aloud to everyone in your current location.

### Usage

- ` + "`say <message>`" + ` - Speak the message aloud

### Examples

- ` + "`say Hello, everyone!`" + `
- ` + "`say How are you?`" + ``,
		Source: "core",
	})

	mustRegister(command.CommandEntryConfig{
		Name:    "pose",
		Handler: PoseHandler,
		Help:    "Perform an action",
		Usage:   "pose <action>",
		HelpText: `## Pose

Describe an action your character performs, visible to everyone in your
current location.

### Usage

- ` + "`pose <action>`" + ` - Perform the action
- ` + "`:<action>`" + ` - Shorthand for pose

### Examples

- ` + "`pose waves hello`" + ` - Shows "CharName waves hello"
- ` + "`:waves hello`" + ` - Same as above`,
		Source: "core",
	})

	mustRegister(command.CommandEntryConfig{
		Name:    "ooc",
		Handler: OOCHandler,
		Help:    "Say or pose something out of character",
		Usage:   "ooc <message>",
		HelpText: `## OOC

Speak out of character to everyone in your current location.

### Usage

- ` + "`ooc <message>`" + ` - Say something OOC
- ` + "`ooc :<action>`" + ` - Pose something OOC
- ` + "`ooc ;<action>`" + ` - Semipose something OOC

### Examples

- ` + "`ooc brb`" + ` → [OOC] Sean says, "brb"
- ` + "`ooc :laughs`" + ` → [OOC] Sean laughs
- ` + "`ooc ;'s phone rings`" + ` → [OOC] Sean's phone rings`,
		Source: "core",
	})

	mustRegister(command.CommandEntryConfig{
		Name:         "pemit",
		Handler:      PemitHandler,
		Capabilities: []string{"comms.pemit"},
		Help:         "Send a private narration to a specific character",
		Usage:        "pemit <character>=<message>",
		HelpText: `## Pemit

Send a private narration to a specific character. Used by GMs and storytellers
to describe what only one character sees or experiences.

### Usage

- ` + "`pemit <character>=<message>`" + ` - Send private narration to character

### Examples

- ` + "`pemit Sean=You feel a chill run down your spine.`" + `
- ` + "`pemit Alex=A figure in the shadows beckons to you.`" + `

### Notes

- Does not require sender and target to be in the same location
- Target sees the raw message with no prefix
- Sender receives a confirmation message

### Permissions

Requires the ` + "`comms.pemit`" + ` capability.`,
		Source: "core",
	})

	// Navigation commands
	mustRegister(command.CommandEntryConfig{
		Name:    "home",
		Handler: HomeHandler,
		Help:    "Return to your home location",
		Usage:   "home",
		HelpText: `## Home

Return to your home location. If no home is set, returns to the default
starting location.

### Usage

- ` + "`home`" + ` - Teleport to your home location

### Notes

- If you have set a home location (via ` + "`set home`" + `), you will be sent there.
- Otherwise, you are sent to the default starting location.
- If you are already home, nothing happens.`,
		Source: "core",
	})

	mustRegister(command.CommandEntryConfig{
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

	mustRegister(command.CommandEntryConfig{
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
		Name:    "who",
		Handler: WhoHandler,
		Help:    "See who is online",
		Usage:   "who",
		HelpText: `## Who

Display a list of all connected players.

Shows character names and their idle times (time since last activity).

### Usage

- ` + "`who`" + ` - List all online players`,
		Source: "core",
	})

	mustRegister(command.CommandEntryConfig{
		Name:    "where",
		Handler: WhereHandler,
		Help:    "See where connected characters are",
		Usage:   "where",
		HelpText: `## Where

Display a list of all connected characters and their current locations.

Useful for social discovery and finding roleplay partners. Characters or
locations you cannot see are filtered by access control.

### Usage

- ` + "`where`" + ` - List all online characters and their locations`,
		Source: "core",
	})

	// Admin commands
	mustRegister(command.CommandEntryConfig{
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

	mustRegister(command.CommandEntryConfig{
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

	mustRegister(command.CommandEntryConfig{
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

	// Description commands
	mustRegister(command.CommandEntryConfig{
		Name:    "describe",
		Handler: DescribeHandler,
		Help:    "Set a description",
		Usage:   "describe me <text>",
		HelpText: `## Describe

Set the description of yourself, your current location, or a named object.

### Usage

- ` + "`describe me <text>`" + ` - Set your character's description
- ` + "`describe here <text>`" + ` - Set the current location's description
- ` + "`describe <target>=<text>`" + ` - Set a named target's description

### Examples

- ` + "`describe me A tall figure in a dark cloak.`" + `
- ` + "`describe here A dusty chamber lit by a single torch.`" + `
- ` + "`describe #01ABCDEF=A gleaming blade.`" + ``,
		Source: "core",
	})

	mustRegister(command.CommandEntryConfig{
		Name:    "desc",
		Handler: DescribeHandler,
		Help:    "Set a description (alias for describe)",
		Usage:   "desc me <text>",
		Source:  "core",
	})

	// Object commands
	mustRegister(command.CommandEntryConfig{
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

	mustRegister(command.CommandEntryConfig{
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
	mustRegister(command.CommandEntryConfig{
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

	mustRegister(command.CommandEntryConfig{
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

	mustRegister(command.CommandEntryConfig{
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
	mustRegister(command.CommandEntryConfig{
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

	mustRegister(command.CommandEntryConfig{
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

	mustRegister(command.CommandEntryConfig{
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

	// Inspection commands
	mustRegister(command.CommandEntryConfig{
		Name:    "examine",
		Handler: ExamineHandler,
		Help:    "Inspect an object, location, or character",
		Usage:   "examine [target]",
		HelpText: `## Examine

Inspect an object, location, or character in detail. Shows more information
than look, filtered by your access level.

### Usage

- ` + "`examine`" + ` - Examine current location
- ` + "`examine here`" + ` - Examine current location (explicit)
- ` + "`examine <target>`" + ` - Examine a named target

### Access Tiers

- **Player:** Name, description, public properties
- **Owner:** Above plus private properties, exit destinations
- **Builder:** Above plus owner, type, creation date, restricted properties
- **Admin:** Above plus ULID, all property visibility levels

### Examples

- ` + "`examine`" + ` - Inspect the current location
- ` + "`examine sword`" + ` - Inspect an object named "sword"
- ` + "`examine Gandalf`" + ` - Inspect a character`,
		Source: "core",
	})

	// Teleport command
	mustRegister(command.CommandEntryConfig{
		Name:    "teleport",
		Handler: TeleportHandler,
		Help:    "Teleport to a location or teleport another character",
		Usage:   "teleport <location> | teleport <character>=<location>",
		HelpText: `## Teleport

Instantly move to a named location, or move another character.

### Usage

- ` + "`teleport <location>`" + ` - Teleport yourself to the named location
- ` + "`teleport <character>=<location>`" + ` - Teleport another character (admin only)

### Examples

- ` + "`teleport The Library`" + ` - Move to The Library
- ` + "`teleport Sean=The Library`" + ` - Move Sean to The Library

### Scope

- **Default role:** Can only teleport to home location
- **Builder role:** Can teleport self to any location
- **Admin role:** Can teleport anyone to any location`,
		Source: "core",
	})
}
