<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Config File System Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add YAML config file loading to HoloMUSH with two-layer precedence (file < CLI flags).

**Architecture:** New `internal/config` package uses koanf to load YAML from the XDG config path, then overlays explicitly-set cobra flags via the posflag provider. Each subcommand calls `config.Load()` in its `RunE` before the existing run function. Existing config structs get exported fields + koanf tags.

**Tech Stack:** koanf v2, koanf/providers/file, koanf/providers/posflag, koanf/parsers/yaml

**Spec:** `docs/specs/2026-03-17-config-file-system-design.md`

**Epic:** holomush-eagr

---

## Chunk 1: Core Config Package

### Task 1: Add koanf dependencies

**Files:**

- Modify: `go.mod`

- Modify: `go.sum`

- [ ] **Step 1: Add koanf dependencies**

Run:

```bash
go get github.com/knadh/koanf/v2
go get github.com/knadh/koanf/providers/file
go get github.com/knadh/koanf/providers/posflag
go get github.com/knadh/koanf/parsers/yaml
```

- [ ] **Step 2: Tidy modules**

Run: `go mod tidy`

- [ ] **Step 3: Verify build**

Run: `task build`
Expected: SUCCESS

- [ ] **Step 4: Commit**

```text
deps(config): add koanf v2 for config file loading
```

---

### Task 2: Create `internal/config` package — failing tests

**Files:**

- Create: `internal/config/config.go`

- Create: `internal/config/config_test.go`

- [ ] **Step 1: Create config.go with the Load function signature and GameConfig type**

```go
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
//
// Precedence (lowest to highest): YAML config file → CLI flags.
//
// If configPath is non-empty, that file is loaded (error if missing).
// If configPath is empty, the default XDG config path is tried (silent if missing).
// CLI flags are overlaid via koanf's posflag provider — only flags explicitly set
// by the user override config file values.
//
// The section parameter selects which top-level YAML key to unmarshal
// (e.g., "core", "gateway", "game").
func Load(configPath string, cmd *cobra.Command, target any, section string) error {
	return nil // TODO: implement
}
```

- [ ] **Step 2: Write failing tests for all key behaviors**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testConfig mirrors the structure of a subcommand config for testing.
type testConfig struct {
	Addr      string `koanf:"addr"`
	LogFormat string `koanf:"log_format"`
	Verbose   bool   `koanf:"verbose"`
}

func newTestCmd(cfg *testConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use: "test",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
	cmd.Flags().StringVar(&cfg.Addr, "addr", "localhost:8080", "listen address")
	cmd.Flags().StringVar(&cfg.LogFormat, "log-format", "json", "log format")
	cmd.Flags().BoolVar(&cfg.Verbose, "verbose", false, "verbose output")
	return cmd
}

func TestLoad_FromYAMLFile(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte(`
server:
  addr: "0.0.0.0:9000"
  log_format: "text"
  verbose: true
`), 0o644)
	require.NoError(t, err)

	cfg := &testConfig{}
	cmd := newTestCmd(cfg)

	err = Load(cfgFile, cmd, cfg, "server")
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0:9000", cfg.Addr)
	assert.Equal(t, "text", cfg.LogFormat)
	assert.True(t, cfg.Verbose)
}

func TestLoad_CLIFlagsOverrideConfigFile(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte(`
server:
  addr: "0.0.0.0:9000"
  log_format: "text"
`), 0o644)
	require.NoError(t, err)

	cfg := &testConfig{}
	cmd := newTestCmd(cfg)
	// Simulate user passing --addr flag explicitly
	cmd.SetArgs([]string{"--addr", "127.0.0.1:3000"})
	require.NoError(t, cmd.ParseFlags([]string{"--addr", "127.0.0.1:3000"}))

	err = Load(cfgFile, cmd, cfg, "server")
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:3000", cfg.Addr, "CLI flag should override config file")
	assert.Equal(t, "text", cfg.LogFormat, "config file value should remain when flag not set")
}

func TestLoad_DefaultFlagsDoNotOverrideConfigFile(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte(`
server:
  addr: "0.0.0.0:9000"
  log_format: "text"
`), 0o644)
	require.NoError(t, err)

	cfg := &testConfig{}
	cmd := newTestCmd(cfg)
	// No flags parsed — all at defaults

	err = Load(cfgFile, cmd, cfg, "server")
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0:9000", cfg.Addr, "config file should win over flag default")
	assert.Equal(t, "text", cfg.LogFormat, "config file should win over flag default")
}

func TestLoad_ExplicitPathMissing_ReturnsError(t *testing.T) {
	cfg := &testConfig{}
	cmd := newTestCmd(cfg)

	err := Load("/nonexistent/config.yaml", cmd, cfg, "server")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "/nonexistent/config.yaml")
}

func TestLoad_DefaultPathMissing_NoError(t *testing.T) {
	// Set XDG_CONFIG_HOME to a temp dir with no config file
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg := &testConfig{}
	cmd := newTestCmd(cfg)

	err := Load("", cmd, cfg, "server")
	require.NoError(t, err)
	// Should have cobra defaults since no file exists
	assert.Equal(t, "localhost:8080", cfg.Addr)
}

func TestLoad_MalformedYAML_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte(`{not: valid: yaml: [`), 0o644)
	require.NoError(t, err)

	cfg := &testConfig{}
	cmd := newTestCmd(cfg)

	err = Load(cfgFile, cmd, cfg, "server")
	require.Error(t, err)
}

func TestLoad_UnknownKeysIgnored(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte(`
server:
  addr: "0.0.0.0:9000"
  unknown_key: "should be ignored"
  another_unknown: 42
`), 0o644)
	require.NoError(t, err)

	cfg := &testConfig{}
	cmd := newTestCmd(cfg)

	err = Load(cfgFile, cmd, cfg, "server")
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0:9000", cfg.Addr)
}

func TestLoad_EmptyConfigFile_UsesDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte(``), 0o644)
	require.NoError(t, err)

	cfg := &testConfig{}
	cmd := newTestCmd(cfg)

	err = Load(cfgFile, cmd, cfg, "server")
	require.NoError(t, err)
	// Falls back to cobra flag defaults
	assert.Equal(t, "localhost:8080", cfg.Addr)
}

func TestLoad_GameConfig(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte(`
game:
  guest_start_location: "01JMHZ5H3ZSBVTGARX4MSS1MBH"
`), 0o644)
	require.NoError(t, err)

	cfg := &GameConfig{}
	// GameConfig has no CLI flags, so use a bare command
	cmd := &cobra.Command{Use: "test"}

	err = Load(cfgFile, cmd, cfg, "game")
	require.NoError(t, err)
	assert.Equal(t, "01JMHZ5H3ZSBVTGARX4MSS1MBH", cfg.GuestStartLocation)
}

func TestLoad_HyphenFlagMatchesUnderscoreYAML(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte(`
server:
  log_format: "text"
`), 0o644)
	require.NoError(t, err)

	cfg := &testConfig{}
	cmd := newTestCmd(cfg)
	// Explicitly set --log-format (hyphenated flag)
	require.NoError(t, cmd.ParseFlags([]string{"--log-format", "json"}))

	err = Load(cfgFile, cmd, cfg, "server")
	require.NoError(t, err)
	assert.Equal(t, "json", cfg.LogFormat, "hyphenated flag should override underscored YAML key")
}

func TestLoad_DefaultXDGPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	// Create the expected directory and config file
	holoDir := filepath.Join(dir, "holomush")
	require.NoError(t, os.MkdirAll(holoDir, 0o700))
	err := os.WriteFile(filepath.Join(holoDir, "config.yaml"), []byte(`
server:
  addr: "from-xdg:9000"
`), 0o644)
	require.NoError(t, err)

	cfg := &testConfig{}
	cmd := newTestCmd(cfg)

	err = Load("", cmd, cfg, "server")
	require.NoError(t, err)
	assert.Equal(t, "from-xdg:9000", cfg.Addr)
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/config/ -v -count=1 2>&1 | tail -20`
Expected: Most tests FAIL (Load returns nil without doing anything)

- [ ] **Step 4: Commit**

```text
test(config): failing tests for config.Load — YAML, flags, precedence
```

---

### Task 3: Implement `config.Load`

**Files:**

- Modify: `internal/config/config.go`

- [ ] **Step 1: Implement the Load function**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package config loads HoloMUSH configuration from YAML files and CLI flags.
package config

import (
	"errors"
	"os"
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
// Precedence (lowest to highest): YAML config file → CLI flags.
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
	path, explicit, err := resolveConfigPath(configPath)
	if err != nil {
		return err
	}

	if path != "" {
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			if explicit {
				return oops.Code("CONFIG_LOAD_FAILED").With("path", path).Wrap(err)
			}
			// Default path exists but is malformed.
			return oops.Code("CONFIG_PARSE_FAILED").With("path", path).Wrap(err)
		}
	}

	// Step 2: Overlay explicitly-set CLI flags.
	// The callback normalizes flag names (hyphens → underscores) and prefixes
	// them with the section so they land in the correct koanf namespace.
	// Passing k to ProviderWithFlag ensures only explicitly-set flags override.
	{
		if err := k.Load(posflag.ProviderWithFlag(cmd.Flags(), ".", k,
			func(f *pflag.Flag) (string, interface{}) {
				key := section + "." + strings.ReplaceAll(f.Name, "-", "_")
				return key, posflag.FlagVal(cmd.Flags(), f)
			}), nil); err != nil {
			return oops.Code("CONFIG_FLAG_FAILED").Wrap(err)
		}
	}

	// Step 3: Unmarshal the section into the target struct.
	if err := k.UnmarshalWithConf(section, target, koanf.UnmarshalConf{Tag: "koanf"}); err != nil {
		return oops.Code("CONFIG_UNMARSHAL_FAILED").With("section", section).Wrap(err)
	}

	return nil
}

// resolveConfigPath determines which config file to load.
// Returns (path, explicit, error) where explicit indicates the user set --config.
func resolveConfigPath(configPath string) (string, bool, error) {
	if configPath != "" {
		if _, err := os.Stat(configPath); err != nil {
			return "", true, oops.Code("CONFIG_NOT_FOUND").
				With("path", configPath).
				Errorf("config file not found: %s", configPath)
		}
		return configPath, true, nil
	}

	// Try default XDG path.
	configDir, err := xdg.ConfigDir()
	if err != nil {
		return "", false, nil // Can't determine XDG dir, skip.
	}

	defaultPath := configDir + "/config.yaml"
	if _, err := os.Stat(defaultPath); errors.Is(err, os.ErrNotExist) {
		return "", false, nil // Default path missing, that's fine.
	} else if err != nil {
		return "", false, oops.Code("CONFIG_ACCESS_FAILED").With("path", defaultPath).Wrap(err)
	}

	return defaultPath, false, nil
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/config/ -v -count=1 2>&1 | tail -30`
Expected: ALL PASS

- [ ] **Step 3: Commit**

```text
feat(config): implement config.Load — YAML + flag precedence via koanf
```

---

## Chunk 2: Export Config Struct Fields

### Task 4: Export `coreConfig` fields

**Files:**

- Modify: `cmd/holomush/core.go`

- Modify: `cmd/holomush/core_test.go` (~6 `coreConfig{}` struct literals)

- Modify: `cmd/holomush/deps.go`

- Modify: `cmd/holomush/deps_test.go` (~12 struct literals with ~5 fields each)

This is a mechanical refactor: rename all fields from lowercase to exported, update all access sites. No behavioral change.

Field mapping:

| Old | New |
| --- | --- |
| `grpcAddr` | `GRPCAddr` |
| `controlAddr` | `ControlAddr` |
| `metricsAddr` | `MetricsAddr` |
| `dataDir` | `DataDir` |
| `gameID` | `GameID` |
| `logFormat` | `LogFormat` |
| `skipSeedMigrations` | `SkipSeedMigrations` |

- [ ] **Step 1: Rename struct fields and add koanf tags in core.go**

Change the `coreConfig` struct definition at `core.go:47-55`:

```go
type coreConfig struct {
	GRPCAddr           string `koanf:"grpc_addr"`
	ControlAddr        string `koanf:"control_addr"`
	MetricsAddr        string `koanf:"metrics_addr"`
	DataDir            string `koanf:"data_dir"`
	GameID             string `koanf:"game_id"`
	LogFormat          string `koanf:"log_format"`
	SkipSeedMigrations bool   `koanf:"skip_seed_migrations"`
}
```

- [ ] **Step 2: Update Validate() method in core.go**

Change field references in `Validate()` at `core.go:58-69`:

```go
func (cfg *coreConfig) Validate() error {
	if cfg.GRPCAddr == "" {
		return oops.Code("CONFIG_INVALID").Errorf("grpc-addr is required")
	}
	if cfg.ControlAddr == "" {
		return oops.Code("CONFIG_INVALID").Errorf("control-addr is required")
	}
	if cfg.LogFormat != "json" && cfg.LogFormat != "text" {
		return oops.Code("CONFIG_INVALID").Errorf("log-format must be 'json' or 'text', got %q", cfg.LogFormat)
	}
	return nil
}
```

- [ ] **Step 3: Update flag bindings in NewCoreCmd at core.go:93-99**

```go
cmd.Flags().StringVar(&cfg.GRPCAddr, "grpc-addr", defaultGRPCAddr, "gRPC listen address")
cmd.Flags().StringVar(&cfg.ControlAddr, "control-addr", defaultCoreControlAddr, "control gRPC listen address with mTLS")
cmd.Flags().StringVar(&cfg.MetricsAddr, "metrics-addr", defaultCoreMetricsAddr, "metrics/health HTTP address (empty = disabled)")
cmd.Flags().StringVar(&cfg.DataDir, "data-dir", "", "data directory (default: XDG_DATA_HOME/holomush)")
cmd.Flags().StringVar(&cfg.GameID, "game-id", "", "game ID (default: auto-generated from database)")
cmd.Flags().StringVar(&cfg.LogFormat, "log-format", defaultLogFormat, "log format (json or text)")
cmd.Flags().BoolVar(&cfg.SkipSeedMigrations, "skip-seed-migrations", false, "disable automatic seed policy version upgrades during bootstrap")
```

- [ ] **Step 4: Update all field references in core.go**

Search `core.go` for `cfg.grpcAddr`, `cfg.controlAddr`, `cfg.metricsAddr`, `cfg.dataDir`, `cfg.gameID`, `cfg.logFormat`, `cfg.skipSeedMigrations` and rename to their exported equivalents. This includes all references in `runCoreWithDeps` and helper functions.

- [ ] **Step 5: Update all field references in deps.go and deps_test.go**

Search both files for the old field names and rename them. These files reference `coreConfig` and `gatewayConfig` fields in dependency injection setup and test assertions.

- [ ] **Step 6: Update all field references in core_test.go**

Search for old field names in test assertions and update.

- [ ] **Step 7: Run tests**

Run: `task test`
Expected: ALL PASS

- [ ] **Step 8: Commit**

```text
refactor(config): export coreConfig fields for koanf unmarshaling
```

---

### Task 5: Export `gatewayConfig` fields

**Files:**

- Modify: `cmd/holomush/gateway.go`

- Modify: `cmd/holomush/gateway_test.go` (~7 `gatewayConfig{}` struct literals)

- Modify: `cmd/holomush/deps.go` (if gateway fields referenced)

- Modify: `cmd/holomush/deps_test.go` (~12 `gatewayConfig{}` struct literals)

Field mapping:

| Old | New |
| --- | --- |
| `telnetAddr` | `TelnetAddr` |
| `coreAddr` | `CoreAddr` |
| `controlAddr` | `ControlAddr` |
| `metricsAddr` | `MetricsAddr` |
| `logFormat` | `LogFormat` |

- [ ] **Step 1: Rename struct fields and add koanf tags in gateway.go**

```go
type gatewayConfig struct {
	TelnetAddr  string `koanf:"telnet_addr"`
	CoreAddr    string `koanf:"core_addr"`
	ControlAddr string `koanf:"control_addr"`
	MetricsAddr string `koanf:"metrics_addr"`
	LogFormat   string `koanf:"log_format"`
}
```

- [ ] **Step 2: Update Validate(), flag bindings, and all field references in gateway.go**

Same pattern as Task 4.

- [ ] **Step 3: Update gateway_test.go and deps files**

Same pattern as Task 4.

- [ ] **Step 4: Run tests**

Run: `task test`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```text
refactor(config): export gatewayConfig fields for koanf unmarshaling
```

---

### Task 6: Export `statusConfig` fields

**Files:**

- Modify: `cmd/holomush/status.go`

- Modify: `cmd/holomush/status_test.go`

Field mapping:

| Old | New |
| --- | --- |
| `jsonOutput` | `JSONOutput` |
| `coreAddr` | `CoreAddr` |
| `gatewayAddr` | `GatewayAddr` |

- [ ] **Step 1: Rename struct fields and add koanf tags in status.go**

```go
type statusConfig struct {
	JSONOutput  bool   `koanf:"json"`
	CoreAddr    string `koanf:"core_addr"`
	GatewayAddr string `koanf:"gateway_addr"`
}
```

Note: The koanf tag for `JSONOutput` is `"json"` (not `"json_output"`) to match the `--json` cobra flag after hyphen-to-underscore normalization.

- [ ] **Step 2: Update Validate(), flag bindings, and all field references in status.go**

- [ ] **Step 3: Update status_test.go**

- [ ] **Step 4: Run tests**

Run: `task test`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```text
refactor(config): export statusConfig fields for koanf unmarshaling
```

---

## Chunk 3: Wire Config Loading Into Subcommands

### Task 7: Wire config.Load into the core subcommand

**Files:**

- Modify: `cmd/holomush/core.go`

- Modify: `cmd/holomush/core_test.go`

- [ ] **Step 1: Write a failing test for config file loading in core**

Add to `core_test.go`. This test calls `config.Load` directly with a fresh command
that has the same flag definitions as the real core command. It does NOT reuse
`NewCoreCmd()` to avoid double-registering flags.

```go
func TestCoreCommand_ConfigFileLoading(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte(`
core:
  grpc_addr: "0.0.0.0:7777"
  control_addr: "0.0.0.0:7778"
  log_format: "text"
`), 0o644)
	require.NoError(t, err)

	cfg := &coreConfig{}
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().StringVar(&cfg.GRPCAddr, "grpc-addr", defaultGRPCAddr, "")
	cmd.Flags().StringVar(&cfg.ControlAddr, "control-addr", defaultCoreControlAddr, "")
	cmd.Flags().StringVar(&cfg.LogFormat, "log-format", defaultLogFormat, "")

	err = config.Load(cfgFile, cmd, cfg, "core")
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0:7777", cfg.GRPCAddr)
	assert.Equal(t, "0.0.0.0:7778", cfg.ControlAddr)
	assert.Equal(t, "text", cfg.LogFormat)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/holomush/ -run TestCoreCommand_ConfigFileLoading -v -count=1`
Expected: FAIL (missing import)

- [ ] **Step 3: Add config.Load call to core's RunE**

In `core.go`, modify `NewCoreCmd`'s `RunE` (around line 88):

```go
RunE: func(cmd *cobra.Command, _ []string) error {
	if err := config.Load(configFile, cmd, cfg, "core"); err != nil {
		return err
	}
	return runCoreWithDeps(cmd.Context(), cfg, cmd, nil)
},
```

Add import: `"github.com/holomush/holomush/internal/config"`

- [ ] **Step 4: Load GameConfig and replace hardcoded start location**

In `runCoreWithDeps` (around line 342-348), replace the hardcoded ULID:

```go
// Load game config for guest start location.
var gameConfig config.GameConfig
if err := config.Load(configFile, cmd, &gameConfig, "game"); err != nil {
	return oops.Code("CONFIG_GAME_FAILED").Wrap(err)
}

// Use configured start location, falling back to seed default.
startLocationStr := gameConfig.GuestStartLocation
if startLocationStr == "" {
	startLocationStr = "01HK153X0006AFVGQT61FPQX3S" // The Nexus seed ULID
}
startLocationID, parseErr := ulid.Parse(startLocationStr)
if parseErr != nil {
	return oops.Code("INVALID_START_LOCATION").With("value", startLocationStr).Wrap(parseErr)
}
guestAuth := telnet.NewGuestAuthenticator(telnet.NewGemstoneElementTheme(), startLocationID)
```

Remove the TODO comment on line 343.

- [ ] **Step 5: Log which config file was loaded**

After `config.Load` succeeds in each subcommand's `RunE`, log at INFO level:

```go
slog.Info("config loaded", "source", configFile)
```

If `configFile` is empty and the XDG default was used, log the resolved path.
If no file was found, log `"source", "flags-only"`.

- [ ] **Step 6: Run tests**

Run: `task test`
Expected: ALL PASS

- [ ] **Step 7: Commit**

```text
feat(config): wire config.Load into core subcommand

Loads YAML config file before starting core process.
Replaces hardcoded guest start location with configurable value
from game.guest_start_location, falling back to seed default.
```

---

### Task 8: Wire config.Load into the gateway subcommand

**Files:**

- Modify: `cmd/holomush/gateway.go`

- Modify: `cmd/holomush/gateway_test.go`

- [ ] **Step 1: Write a failing test for config file loading in gateway**

Add to `gateway_test.go`:

```go
func TestGatewayCommand_ConfigFileLoading(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte(`
gateway:
  telnet_addr: ":5555"
  core_addr: "core.local:9000"
  log_format: "text"
`), 0o644)
	require.NoError(t, err)

	cfg := &gatewayConfig{}
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().StringVar(&cfg.TelnetAddr, "telnet-addr", defaultTelnetAddr, "")
	cmd.Flags().StringVar(&cfg.CoreAddr, "core-addr", defaultCoreAddr, "")
	cmd.Flags().StringVar(&cfg.LogFormat, "log-format", defaultLogFormat, "")

	err = config.Load(cfgFile, cmd, cfg, "gateway")
	require.NoError(t, err)

	assert.Equal(t, ":5555", cfg.TelnetAddr)
	assert.Equal(t, "core.local:9000", cfg.CoreAddr)
	assert.Equal(t, "text", cfg.LogFormat)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/holomush/ -run TestGatewayCommand_ConfigFileLoading -v -count=1`
Expected: FAIL

- [ ] **Step 3: Add config.Load call to gateway's RunE**

In `gateway.go`, modify `newGatewayCmd`'s `RunE`:

```go
RunE: func(cmd *cobra.Command, _ []string) error {
	if err := config.Load(configFile, cmd, cfg, "gateway"); err != nil {
		return err
	}
	return runGatewayWithDeps(cmd.Context(), cfg, cmd, nil)
},
```

Add import: `"github.com/holomush/holomush/internal/config"`

- [ ] **Step 4: Run tests**

Run: `task test`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```text
feat(config): wire config.Load into gateway subcommand
```

---

### Task 9: Wire config.Load into the status subcommand

**Files:**

- Modify: `cmd/holomush/status.go`

- Modify: `cmd/holomush/status_test.go`

- [ ] **Step 1: Write a failing test for config file loading in status**

Add to `status_test.go`:

```go
func TestStatusCommand_ConfigFileLoading(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(cfgFile, []byte(`
status:
  core_addr: "10.0.0.1:9001"
  gateway_addr: "10.0.0.1:9002"
`), 0o644)
	require.NoError(t, err)

	cfg := &statusConfig{}
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().StringVar(&cfg.CoreAddr, "core-addr", defaultCoreControlAddr, "")
	cmd.Flags().StringVar(&cfg.GatewayAddr, "gateway-addr", defaultGatewayControlAddr, "")

	err = config.Load(cfgFile, cmd, cfg, "status")
	require.NoError(t, err)

	assert.Equal(t, "10.0.0.1:9001", cfg.CoreAddr)
	assert.Equal(t, "10.0.0.1:9002", cfg.GatewayAddr)
}
```

- [ ] **Step 2: Add config.Load call to status' RunE**

In `status.go`, modify `newStatusCmd`'s `RunE`:

```go
RunE: func(cmd *cobra.Command, _ []string) error {
	if err := config.Load(configFile, cmd, cfg, "status"); err != nil {
		return err
	}
	return runStatus(cmd, cfg)
},
```

Add import: `"github.com/holomush/holomush/internal/config"`

- [ ] **Step 3: Run tests**

Run: `task test`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```text
feat(config): wire config.Load into status subcommand
```

---

## Chunk 4: Documentation & Cleanup

### Task 10: Operator documentation

**Files:**

- Create: `site/docs/operators/configuration.md`

- [ ] **Step 1: Create the operator docs page**

Create `site/docs/operators/configuration.md` with:

1. Overview — what config files do and where they live
2. Config file location — `~/.config/holomush/config.yaml`, `--config` override
3. Precedence rules — config file < CLI flags, DATABASE_URL is env-only
4. Full annotated example config — every key with description and default
5. Per-process config — how to use `--config` for multi-host setups
6. First-run experience — no config file needed, flags work as before

- [ ] **Step 2: Run docs lint**

Run: `task lint:markdown`
Expected: No new issues in the new file

- [ ] **Step 3: Commit**

```text
docs(operators): add configuration guide for config.yaml
```

---

### Task 11: Final verification

- [ ] **Step 1: Run full test suite**

Run: `task test`
Expected: ALL PASS

- [ ] **Step 2: Run linters**

Run: `task lint`
Expected: PASS

- [ ] **Step 3: Run build**

Run: `task build`
Expected: SUCCESS

- [ ] **Step 4: Manual smoke test (optional)**

Create a test config file and verify the server reads it:

```bash
mkdir -p /tmp/holomush-config-test
cat > /tmp/holomush-config-test/config.yaml << 'EOF'
core:
  grpc_addr: "localhost:9999"
  log_format: "text"
gateway:
  telnet_addr: ":4444"
EOF
./holomush core --config /tmp/holomush-config-test/config.yaml --help
```

---

## Post-Implementation Checklist

- [ ] All tests pass (`task test`)
- [ ] All linters pass (`task lint`)
- [ ] Build succeeds (`task build`)
- [ ] Spec matches implementation
- [ ] Operator documentation accurate
- [ ] Close beads tasks
- [ ] Create PR
