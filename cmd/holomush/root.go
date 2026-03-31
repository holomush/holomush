// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"github.com/spf13/cobra"
)

// Global flags available to all subcommands.
var configFile string

// NewRootCmd creates the root command for the HoloMUSH CLI.
func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "holomush",
		Short: "HoloMUSH - A modern MUSH platform",
		Long: `HoloMUSH is a modern MUSH platform with event sourcing,
WebAssembly plugins, and dual protocol support (telnet + web).`,
	}

	// Global flag for config file path
	cmd.PersistentFlags().StringVar(&configFile, "config", "", "config file path")

	// Add subcommands
	cmd.AddCommand(NewGatewayCmd())
	cmd.AddCommand(NewCoreCmd())
	cmd.AddCommand(NewMigrateCmd())
	cmd.AddCommand(NewStatusCmd())

	return cmd
}

// NewGatewayCmd creates the gateway subcommand.
func NewGatewayCmd() *cobra.Command {
	return newGatewayCmd()
}

// NewStatusCmd creates the status subcommand.
func NewStatusCmd() *cobra.Command {
	return newStatusCmd()
}
