// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package main is the entry point for the HoloMUSH server.
package main

import (
	"fmt"
	"os"
)

// Version information set at build time.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

// run executes the CLI and returns an exit code.
// This is separated from main() to make testing easier.
func run() int {
	cmd := NewRootCmd()
	cmd.Version = formatVersion(version, commit, date)

	if err := cmd.Execute(); err != nil {
		return 1
	}
	return 0
}

// formatVersion creates a version string from components.
func formatVersion(ver, com, dt string) string {
	return fmt.Sprintf("%s (commit: %s, built: %s)", ver, com, dt)
}

func main() {
	os.Exit(run())
}
