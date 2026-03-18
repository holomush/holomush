<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Server Configuration File System Design

**Status:** Draft
**Date:** 2026-03-17
**Epic:** holomush-eagr (Server configuration file system)

## Overview

HoloMUSH currently configures all server settings via CLI flags, with a `--config`
flag on the root command that is declared but never loaded. This design adds YAML
config file support using koanf, with two-layer precedence: config file < CLI flags.

## Goals

- MUST load configuration from a YAML file at a well-known XDG path
- MUST allow CLI flags to override any config file value
- MUST support a `--config` flag to specify an alternate config path
- MUST work with zero config (flags-only, for backward compatibility)
- MUST provide operator documentation with annotated examples

## Non-Goals

- Environment variable mapping for config keys (only `DATABASE_URL` uses env vars)
- Hot-reloading of config files at runtime
- Config file generation or `init` command
- Remote config sources (etcd, consul)
- Secret management (credentials stay in env vars)

## Design Decisions

### Config Library: koanf

The project MUST use [koanf](https://github.com/knadh/koanf) for config loading.

**Rationale:** koanf provides modular, composable providers (file, cobra flags) with
clean layered-merge semantics. It integrates natively with cobra and avoids the
reflection magic of alternatives like viper.

### Two-Layer Precedence

```text
config file (lowest) → CLI flags (highest)
```

No environment variable layer for config keys. `DATABASE_URL` remains the sole env
var, handled separately via `os.Getenv`.

**Rationale:** Two layers are easier to debug than three. Operators can always use
CLI flags for per-invocation overrides. Adding an env var layer for every setting
creates confusion about where values originate.

### Single Config File, Sectioned

One `config.yaml` with top-level sections for each subcommand plus a `game` section:

```yaml
core:
  grpc_addr: "localhost:9000"
  control_addr: "127.0.0.1:9001"
  metrics_addr: "127.0.0.1:9100"
  data_dir: ""
  game_id: ""
  log_format: "json"
  skip_seed_migrations: false

gateway:
  telnet_addr: ":4201"
  core_addr: "localhost:9000"
  control_addr: "127.0.0.1:9002"
  metrics_addr: "127.0.0.1:9101"
  log_format: "json"

game:
  guest_start_location: "01JMHZ5H3ZSBVTGARX4MSS1MBH"

status:
  core_addr: "127.0.0.1:9001"
  gateway_addr: "127.0.0.1:9002"
  json: false
```

Each subcommand reads only its relevant sections. The `--config` flag allows
operators to point each process at a different file for multi-host deployments.

**Rationale:** Single file is simpler for local dev and single-host deploys.
Per-process override via `--config` handles production separation.

### DATABASE_URL Stays Env-Only

`DATABASE_URL` MUST NOT be added to the config file. It remains environment-variable
only.

**Rationale:** Database URLs contain credentials. Mixing secrets into config files
creates operational risks. The config file documentation SHOULD mention that
`DATABASE_URL` is required as an env var.

## Architecture

### New Package: `internal/config`

```text
internal/config/
  config.go      — Config struct, Load() function, koanf merge logic
  config_test.go — Unit tests
```

### Config Structs

The `Load` function unmarshals directly into per-subcommand structs (which live in
`cmd/holomush/`). There is no top-level `Config` wrapper struct — each subcommand
loads only its own section.

The only new struct in `internal/config` is `GameConfig`:

```go
type GameConfig struct {
    GuestStartLocation string `koanf:"guest_start_location"`
}
```

### Exported Fields Requirement

koanf uses `mapstructure` internally, which relies on Go reflection to set struct
fields. **Reflection cannot set unexported (lowercase) fields.** The existing config
structs (`coreConfig`, `gatewayConfig`, `statusConfig`) all use unexported fields.

These structs MUST be refactored to export their fields:

- `grpcAddr string` → `GRPCAddr string`
- `controlAddr string` → `ControlAddr string`
- All field access throughout `core.go`, `gateway.go`, `status.go` MUST be updated
- All cobra `StringVar` bindings (e.g., `&cfg.grpcAddr`) MUST be updated
- `Validate()` methods MUST be updated to use exported field names

This is a mechanical refactor with no behavioral change. The structs remain in
`cmd/holomush/` (not moved to `internal/config`) since they are subcommand-specific.

### Loading Flow

The `Load` function MUST follow this sequence:

1. Create empty koanf instance
2. Load YAML file:
   - If `--config` flag is set: use that path, error if file missing
   - Otherwise: try `XDG_CONFIG_HOME/holomush/config.yaml`, silent if missing
3. Overlay CLI flags via koanf's posflag provider with `cmd.Flags()`
   (only explicitly-set flags — see Key Name Normalization below)
4. Unmarshal the relevant section into the typed config struct
5. Call `Validate()` on the result

### Load Function Signature

```go
func Load(configPath string, cmd *cobra.Command, target any, section string) error
```

Parameters:

- `configPath` — value of the `--config` persistent flag. The `configFile` variable
  is declared in `root.go` and bound to `--config` via `PersistentFlags().StringVar`.
  Empty string means use default XDG path.
- `cmd` — cobra command (for reading explicitly-set flags via `cmd.Flags()`)
- `target` — pointer to config struct to unmarshal into
- `section` — koanf key prefix (e.g., `"core"`, `"gateway"`)

### Key Name Normalization

Cobra flags use hyphens (`grpc-addr`) while YAML keys use underscores (`grpc_addr`).
The posflag provider uses the pflag name as the koanf key, so without normalization
these are different keys and will not merge correctly.

The `Load` function MUST use a key callback in `posflag.Provider` to convert hyphens
to underscores, ensuring flag keys match YAML keys:

```go
k.Load(posflag.ProviderWithFlag(cmd.Flags(), ".", k, func(f *pflag.Flag) (string, interface{}) {
    key := strings.ReplaceAll(f.Name, "-", "_")
    return section + "." + key, posflag.FlagVal(cmd.Flags(), f)
}), nil)
```

This callback also prepends the section prefix so flag values land in the correct
koanf namespace (e.g., `grpc-addr` → `core.grpc_addr`).

### Posflag Provider: Explicit-Only Overlay

The `ProviderWithFlag` callback (shown above) receives the existing koanf instance
`k` as a parameter. This is critical: it lets posflag distinguish between
explicitly-set flags and default values. Without passing `k`, every flag's default
value would override config file values, defeating the precedence model.

### File Resolution

```text
--config=/path/to/file.yaml  →  load that file, error if missing
--config=""                   →  try xdg.ConfigDir()/config.yaml
                                 if missing → continue with defaults (no error)
                                 if present but malformed → error
```

## Integration

### Subcommand Changes

Each subcommand's `RunE` adds a `config.Load` call before the existing run function:

```go
RunE: func(cmd *cobra.Command, _ []string) error {
    if err := config.Load(configFile, cmd, cfg, "core"); err != nil {
        return err
    }
    return runCoreWithDeps(cmd.Context(), cfg, cmd, nil)
}
```

The core command additionally loads the `game` section. Since `GameConfig` has no
CLI flags (no `--guest-start-location` flag exists), it is loaded from the config
file only. The `Load` function handles this correctly — when `cmd.Flags()` has no
matching flags, the posflag overlay is a no-op. The double file read is acceptable
for a startup-only operation.

```go
var gameConfig config.GameConfig
if err := config.Load(configFile, cmd, &gameConfig, "game"); err != nil {
    return err
}
```

The `game` section is top-level rather than nested under `core` because it represents
gameplay settings that operators think about independently from process infrastructure.
Future game-wide settings (themes, max connections) will live here.

### Struct Tag Addition

Existing config structs MUST export fields and add `koanf:` tags:

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

### Guest Start Location

The hardcoded start location ULID in `core.go` (line ~348) MUST be replaced with
`gameConfig.GuestStartLocation`. The default value comes from the config file or
falls back to the well-known seed ULID.

### What Does Not Change

- `deps.go` — dependency injection pattern untouched
- `DATABASE_URL` — still `os.Getenv`, no config involvement
- Flag names — all existing CLI flags keep their names
- Default values — hardcoded defaults remain as fallbacks
- `runCoreWithDeps` / `runGatewayWithDeps` signatures unchanged

## Testing Strategy

### Unit Tests (`internal/config/config_test.go`)

- Load from YAML string, verify struct values
- Overlay with mock cobra flags, verify precedence
- Missing default config file → no error, defaults used
- Missing explicit `--config` path → error
- Malformed YAML → error with `CONFIG_PARSE_FAILED` code and file path context
- Unknown keys in YAML → ignored (forward compatibility)

### Existing Tests

Existing subcommand tests in `cmd/holomush/` MUST continue to pass unchanged.
They exercise flag-only configuration which remains the default behavior.

### No Integration Tests

Config loading is pure file read + merge logic with no external dependencies.
Unit tests provide sufficient coverage.

## Operator Documentation

A new page MUST be created at `site/docs/operators/configuration.md` covering:

- Config file location (`~/.config/holomush/config.yaml`) and `--config` override
- Full annotated example config file
- Precedence rules (config file < CLI flags)
- `DATABASE_URL` as the sole env var requirement
- First-run experience (no config file needed, flags work as before)

## Dependencies

New Go module dependencies:

- `github.com/knadh/koanf/v2` — core library
- `github.com/knadh/koanf/providers/file` — YAML file provider
- `github.com/knadh/koanf/providers/structs` — struct defaults
- `github.com/knadh/koanf/parsers/yaml` — YAML parser
- `github.com/knadh/koanf/providers/posflag` — cobra/pflag integration
