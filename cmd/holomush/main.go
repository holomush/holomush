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
	fmt.Printf("HoloMUSH %s (%s) built %s\n", version, commit, date)
	fmt.Println("Server not yet implemented.")
	os.Exit(0)
}
