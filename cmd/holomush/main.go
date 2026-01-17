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

func main() {
	cmd := NewRootCmd()
	cmd.Version = fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date)

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
