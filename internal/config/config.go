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
	"time"

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
	GuestStartLocation   string   `koanf:"guest_start_location"`
	DisabledCommands     []string `koanf:"disabled_commands"`
	PluginTrustAllowlist []string `koanf:"plugin_trust_allowlist"`
}

// AuthConfig holds authentication-related configuration read by the core
// command from the "auth" YAML section.
type AuthConfig struct {
	// MaxPlayerSessionsPerPlayer caps concurrent authenticated sessions per
	// player. On new login exceeding the cap, the oldest PlayerSession is
	// evicted (deleted) — cascading through player_session_id FK to also
	// remove all game sessions and terminate their Subscribe streams.
	// A value <= 0 disables the cap (test configurations only).
	MaxPlayerSessionsPerPlayer int `koanf:"max_player_sessions_per_player"`
}

// DefaultMaxPlayerSessionsPerPlayer is the default concurrent session cap
// per player. Ten handles reasonable multi-device use (phone + laptop +
// tablet + work machine + reserved capacity) without unbounded session
// accumulation from buggy clients or forgotten tabs.
const DefaultMaxPlayerSessionsPerPlayer = 10

// DefaultAuthConfig returns an AuthConfig populated with documented defaults.
// Call sites SHOULD start from this value and overlay YAML via Load so that
// omitted keys retain the default instead of zeroing to Go's zero value.
func DefaultAuthConfig() AuthConfig {
	return AuthConfig{
		MaxPlayerSessionsPerPlayer: DefaultMaxPlayerSessionsPerPlayer,
	}
}

// CryptoConfig holds crypto-related server configuration loaded from
// the top-level "crypto" YAML section. Sub-epic B introduces this block
// with operators as its first tenant; future sub-epics (e.g., D's
// dual_control_required) extend the same block.
type CryptoConfig struct {
	// Operators is the allow-list of player IDs (ULIDs) that hold the
	// crypto.operator capability — the narrowing grant required (in
	// addition to RoleAdmin) for break-glass operations.
	//
	// Lax+warn validation: at startup the server cross-checks each ID
	// against the players table and emits a structured warning per
	// unknown ID. The configured list is used as-is regardless;
	// unknown IDs become inert grants (no one can authenticate as a
	// nonexistent player).
	//
	// Empty / missing → no operators → break-glass impossible.
	// Reload requires server restart in v1.
	Operators []string `koanf:"operators"`

	// DualControlRequired lists op_kinds (e.g., "rekey", "admin_read_stream")
	// that require a second-operator approval before proceeding. Lax+warn
	// validation: unknown op_kinds are logged at server start and excluded
	// from enforcement. Empty list means no dual-control is required (lax mode).
	DualControlRequired []string `koanf:"dual_control_required"`

	// RekeyCheckpointTTL is the maximum age of a non-terminal rekey
	// checkpoint's last_heartbeat_at before the sweep subsystem auto-aborts
	// it (INV-E18 / spec §6.2). Defaults to 24h via Defaults() when
	// unset/zero. Sub-epic E T37 (holomush-jxo8.7.34).
	RekeyCheckpointTTL time.Duration `koanf:"rekey_checkpoint_ttl"`

	// RekeyCheckpointSweepInterval is the interval between background sweep
	// scans (INV-E19 / spec §6.2). Defaults to 1h via Defaults() when
	// unset/zero. Sub-epic E T37 (holomush-jxo8.7.34).
	RekeyCheckpointSweepInterval time.Duration `koanf:"rekey_checkpoint_sweep_interval"`

	// OperatorReadDefaultWindow is the size of since-defaulted AdminReadStream
	// reads (INV-CRYPTO-56 / spec §6). Defaults to 1h via Defaults() when unset/zero.
	// Sub-epic F R.14 (holomush-jxo8.8.38).
	OperatorReadDefaultWindow time.Duration `koanf:"operator_read_default_window"`

	// OperatorReadMaxWindow caps the (until - since) span on AdminReadStream
	// requests (INV-CRYPTO-56). Defaults to 30d via Defaults() when unset/zero.
	// Operators configuring values >90d trigger a WARN at boot — the cap
	// remains in force but oversized windows greatly inflate row-count and
	// memory pressure on the cold-tier reader.
	OperatorReadMaxWindow time.Duration `koanf:"operator_read_max_window"`

	// OperatorReadWriteDeadline is the per-frame send deadline on the
	// AdminReadStream server stream (INV-CRYPTO-64). Defaults to 30s via Defaults()
	// when unset/zero.
	OperatorReadWriteDeadline time.Duration `koanf:"operator_read_write_deadline"`

	// OperatorReadApprovalTTL is the dual-control wait budget on AdminReadStream
	// (INV-CRYPTO-61). Defaults to 5m via Defaults() when unset/zero.
	OperatorReadApprovalTTL time.Duration `koanf:"operator_read_approval_ttl"`
}

// DefaultRekeyCheckpointTTL is the default age cutoff for non-terminal rekey
// checkpoints (spec §6.2 INV-E18). 24h matches the master spec default.
const DefaultRekeyCheckpointTTL = 24 * time.Hour

// DefaultRekeyCheckpointSweepInterval is the default scan cadence for the
// rekey-checkpoint sweep subsystem (spec §6.2 INV-E19). 1h is the
// master-spec default.
const DefaultRekeyCheckpointSweepInterval = 1 * time.Hour

// DefaultOperatorReadDefaultWindow is the default since-defaulted window for
// AdminReadStream reads (INV-CRYPTO-56). Sub-epic F R.14 (holomush-jxo8.8.38).
const DefaultOperatorReadDefaultWindow = 1 * time.Hour

// DefaultOperatorReadMaxWindow caps the (until - since) span on
// AdminReadStream reads (INV-CRYPTO-56). Sub-epic F R.14.
const DefaultOperatorReadMaxWindow = 30 * 24 * time.Hour

// DefaultOperatorReadWriteDeadline is the per-frame send deadline on the
// AdminReadStream server stream (INV-CRYPTO-64). Sub-epic F R.14.
const DefaultOperatorReadWriteDeadline = 30 * time.Second

// DefaultOperatorReadApprovalTTL is the default dual-control wait budget on
// AdminReadStream (INV-CRYPTO-61). Sub-epic F R.14.
const DefaultOperatorReadApprovalTTL = 5 * time.Minute

// Defaults returns a copy of c with the zero-valued fields populated from
// their defaults. Defaults() is idempotent and safe to call on every load.
// Sub-epic E T37 (holomush-jxo8.7.34); operator-read fields added by
// sub-epic F R.14 (holomush-jxo8.8.38).
func (c CryptoConfig) Defaults() CryptoConfig {
	if c.Operators == nil {
		c.Operators = []string{}
	}
	if c.RekeyCheckpointTTL <= 0 {
		c.RekeyCheckpointTTL = DefaultRekeyCheckpointTTL
	}
	if c.RekeyCheckpointSweepInterval <= 0 {
		c.RekeyCheckpointSweepInterval = DefaultRekeyCheckpointSweepInterval
	}
	if c.OperatorReadDefaultWindow <= 0 {
		c.OperatorReadDefaultWindow = DefaultOperatorReadDefaultWindow
	}
	if c.OperatorReadMaxWindow <= 0 {
		c.OperatorReadMaxWindow = DefaultOperatorReadMaxWindow
	}
	if c.OperatorReadWriteDeadline <= 0 {
		c.OperatorReadWriteDeadline = DefaultOperatorReadWriteDeadline
	}
	if c.OperatorReadApprovalTTL <= 0 {
		c.OperatorReadApprovalTTL = DefaultOperatorReadApprovalTTL
	}
	return c
}

// DefaultCryptoConfig returns an empty CryptoConfig — no operators,
// break-glass disabled. Operators MUST explicitly populate the list.
func DefaultCryptoConfig() CryptoConfig {
	return CryptoConfig{
		Operators:                    []string{},
		RekeyCheckpointTTL:           DefaultRekeyCheckpointTTL,
		RekeyCheckpointSweepInterval: DefaultRekeyCheckpointSweepInterval,
		OperatorReadDefaultWindow:    DefaultOperatorReadDefaultWindow,
		OperatorReadMaxWindow:        DefaultOperatorReadMaxWindow,
		OperatorReadWriteDeadline:    DefaultOperatorReadWriteDeadline,
		OperatorReadApprovalTTL:      DefaultOperatorReadApprovalTTL,
	}
}

// LoggingSink configures one log destination. Level is a slog level name
// ("debug"|"info"|"warn"|"error"); empty inherits the global level.
type LoggingSink struct {
	Enabled bool   `koanf:"enabled"`
	Level   string `koanf:"level"`
}

// EffectiveLevel returns the sink's level, falling back to global when the
// per-sink level is unset or unparseable (spec INV-L4).
func (s LoggingSink) EffectiveLevel(global slog.Level) slog.Level {
	if s.Level == "" {
		return global
	}
	var l slog.Level
	if err := l.UnmarshalText([]byte(s.Level)); err != nil {
		return global
	}
	return l
}

// LoggingConfig configures the three log sinks. Endpoints/secrets remain
// env-driven (SENTRY_DSN, OTEL_EXPORTER_OTLP_ENDPOINT); these toggles gate
// behaviour. Effective enablement of a non-stderr sink = Enabled AND its
// endpoint present (spec INV-L3), enforced at telemetry.Init.
type LoggingConfig struct {
	Stderr LoggingSink `koanf:"stderr"`
	OTel   LoggingSink `koanf:"otel"`
	Sentry LoggingSink `koanf:"sentry"`
}

// SentryLogLevelDefault is the built-in floor for the Sentry log sink. Sentry's
// Logs view is for actionable signal, not a debug firehose: info/debug records
// carry no incident value there and would dominate the project's log quota, so
// the Sentry sink defaults to WARN independent of the (typically lower) global
// level. Stderr and the collector still inherit the global level. Operators can
// override via logging.sentry.level / --log-sentry-level.
const SentryLogLevelDefault = "warn"

// DefaultLoggingConfig enables all three sinks. Stderr and the collector inherit
// the global level; the Sentry sink defaults to SentryLogLevelDefault (WARN).
func DefaultLoggingConfig() LoggingConfig {
	return LoggingConfig{
		Stderr: LoggingSink{Enabled: true},
		OTel:   LoggingSink{Enabled: true},
		Sentry: LoggingSink{Enabled: true, Level: SentryLogLevelDefault},
	}
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
