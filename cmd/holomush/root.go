// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"log/slog"
	"os"
	"strings"

	"github.com/samber/oops"
	"github.com/spf13/cobra"
)

// Global flags available to all subcommands.
var (
	configFile string
	logLevel   string
)

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
	// Global flag for log level
	cmd.PersistentFlags().StringVar(&logLevel, "log-level", "info",
		"log level (debug, info, warn, error)")

	// Add subcommands
	cmd.AddCommand(NewGatewayCmd())
	cmd.AddCommand(NewCoreCmd())
	cmd.AddCommand(NewMigrateCmd())
	cmd.AddCommand(NewStatusCmd())
	cmd.AddCommand(NewPluginCmd())
	cmd.AddCommand(NewAdminCmd())
	cmd.AddCommand(NewCryptoCmd())
	cmd.AddCommand(NewKEKCmd())

	return cmd
}

// parseLogLevel converts a string to slog.Level.
func parseLogLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, oops.Code("CONFIG_INVALID").
			Errorf("invalid log level %q: must be debug, info, warn, or error", s)
	}
}

// resolveLogLevel determines the effective log level from the flag or LOG_LEVEL env var.
// The CLI flag takes precedence; if not explicitly set, LOG_LEVEL env var is consulted;
// otherwise the default "info" is used.
func resolveLogLevel(cmd *cobra.Command) (slog.Level, error) {
	// If the flag was explicitly set on the command line, honour it directly.
	if cmd.Flags().Changed("log-level") {
		return parseLogLevel(logLevel)
	}
	// Prefer LOG_LEVEL env var over the flag default.
	if envLevel := os.Getenv("LOG_LEVEL"); envLevel != "" {
		return parseLogLevel(envLevel)
	}
	// logLevel holds the flag default ("info") when registered, or "" when the
	// flag is not present (e.g. subcommand used standalone in tests). Normalise.
	if logLevel == "" {
		return slog.LevelInfo, nil
	}
	return parseLogLevel(logLevel)
}

// NewGatewayCmd creates the gateway subcommand.
func NewGatewayCmd() *cobra.Command {
	return newGatewayCmd()
}

// NewStatusCmd creates the status subcommand.
func NewStatusCmd() *cobra.Command {
	return newStatusCmd()
}
