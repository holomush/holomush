// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package config loads HoloMUSH configuration from YAML files and CLI flags.
package config

import (
	"github.com/spf13/cobra"
)

// GameConfig holds game-level configuration read by the core command.
type GameConfig struct {
	GuestStartLocation string `koanf:"guest_start_location"`
}

// Load reads configuration from a YAML file and overlays explicitly-set CLI flags.
func Load(configPath string, cmd *cobra.Command, target any, section string) error {
	return nil // TODO: implement
}
