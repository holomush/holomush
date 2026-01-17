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
	return &cobra.Command{
		Use:   "gateway",
		Short: "Start the gateway process (telnet/web servers)",
		Long: `Start the gateway process which handles incoming connections
from telnet and web clients, forwarding commands to the core process.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.Println("gateway: not implemented yet")
			return nil
		},
	}
}

// NewCoreCmd creates the core subcommand.
func NewCoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "core",
		Short: "Start the core process (engine, plugins)",
		Long: `Start the core process which runs the game engine,
manages plugins, and handles game state.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.Println("core: not implemented yet")
			return nil
		},
	}
}

// NewStatusCmd creates the status subcommand.
func NewStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show status of running HoloMUSH processes",
		Long:  `Show the health and status of running gateway and core processes.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.Println("status: not implemented yet")
			return nil
		},
	}
}
