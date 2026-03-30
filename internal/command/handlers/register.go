// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"github.com/holomush/holomush/internal/command"
)

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
}
