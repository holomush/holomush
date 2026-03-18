// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package config loads HoloMUSH configuration from YAML files and CLI flags.
package config

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/samber/oops"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/holomush/holomush/internal/xdg"
)

// GameConfig holds game-level configuration read by the core command.
type GameConfig struct {
	GuestStartLocation string `koanf:"guest_start_location"`
}

// Load reads configuration from a YAML file and overlays explicitly-set CLI flags.
//
// Precedence (lowest to highest): YAML config file -> CLI flags.
//
// If configPath is non-empty, that file is loaded (error if missing).
// If configPath is empty, the default XDG config path is tried (silent if missing).
// CLI flags are overlaid via koanf's posflag provider — only flags explicitly set
// by the user override config file values.
//
// The section parameter selects which top-level YAML key to unmarshal
// (e.g., "core", "gateway", "game").
func Load(configPath string, cmd *cobra.Command, target any, section string) error {
	k := koanf.New(".")

	// Step 1: Resolve and load YAML file.
	path, _, err := resolveConfigPath(configPath)
	if err != nil {
		return err
	}

	if path != "" {
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			return oops.Code("CONFIG_PARSE_FAILED").With("path", path).Wrap(err)
		}
	}

	// Step 2: Overlay explicitly-set CLI flags.
	// The callback normalizes flag names (hyphens -> underscores) and prefixes
	// them with the section so they land in the correct koanf namespace.
	// Passing k to ProviderWithFlag ensures only explicitly-set flags override.
	if err := k.Load(posflag.ProviderWithFlag(cmd.Flags(), ".", k,
		func(f *pflag.Flag) (string, interface{}) {
			key := section + "." + strings.ReplaceAll(f.Name, "-", "_")
			return key, posflag.FlagVal(cmd.Flags(), f)
		}), nil); err != nil {
		return oops.Code("CONFIG_FLAG_FAILED").Wrap(err)
	}

	// Step 3: Unmarshal the section into the target struct.
	if err := k.UnmarshalWithConf(section, target, koanf.UnmarshalConf{Tag: "koanf"}); err != nil {
		return oops.Code("CONFIG_UNMARSHAL_FAILED").With("section", section).Wrap(err)
	}

	return nil
}

// resolveConfigPath determines which config file to load.
// Returns (path, explicit, error) where explicit indicates the user set --config.
//
//nolint:gocritic // unnamed results are clearer here than named returns that shadow
func resolveConfigPath(configPath string) (string, bool, error) {
	if configPath != "" {
		if _, err := os.Stat(configPath); err != nil {
			switch {
			case errors.Is(err, os.ErrNotExist):
				return "", true, oops.Code("CONFIG_NOT_FOUND").
					With("path", configPath).
					Errorf("config file not found: %s", configPath)
			case errors.Is(err, os.ErrPermission):
				return "", true, oops.Code("CONFIG_ACCESS_DENIED").
					With("path", configPath).
					Errorf("config file not readable: %s", configPath)
			default:
				return "", true, oops.Code("CONFIG_ACCESS_FAILED").
					With("path", configPath).
					Wrap(err)
			}
		}
		return configPath, true, nil
	}

	// Try default XDG path. If we can't determine the XDG dir
	// (e.g., no HOME), skip config file loading gracefully.
	configDir, err := xdg.ConfigDir()
	if err != nil {
		slog.Debug("XDG config dir unavailable, skipping config file", "error", err)
		return "", false, nil
	}

	defaultPath := filepath.Join(configDir, "config.yaml")
	if _, err := os.Stat(defaultPath); errors.Is(err, os.ErrNotExist) {
		return "", false, nil // Default path missing, that's fine.
	} else if err != nil {
		return "", false, oops.Code("CONFIG_ACCESS_FAILED").With("path", defaultPath).Wrap(err)
	}

	return defaultPath, false, nil
}
