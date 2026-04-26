// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import "github.com/spf13/cobra"

// NewPluginCmd is the `holomush plugin` parent command. Subcommands
// (validate, events) attach via NewPluginValidateCmd / NewPluginEventsCmd
// added in subsequent tasks.
func NewPluginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Plugin authoring and inspection commands",
		Long:  "Inspect and validate plugin manifests, list declared event types, and run author-time checks.",
	}
	cmd.AddCommand(NewPluginValidateCmd())
	return cmd
}
