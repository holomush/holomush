# Configuration

This guide covers all configuration options for HoloMUSH.

## Overview

HoloMUSH uses a two-layer configuration system:

1. Config file (`~/.config/holomush/config.yaml`) — lowest precedence
2. CLI flags — highest precedence, always override config file values

Built-in defaults apply when neither a config file value nor a CLI flag is provided.
`DATABASE_URL` is a separate environment variable for database credentials — it is not
part of the config file system and has no equivalent config key.

Config files are optional. All settings have sensible defaults. Most config file keys
have an equivalent CLI flag; the `game` section is config-file-only. You can run
HoloMUSH without a config file; flags work exactly as before.

## Command-Line Reference

### Global Commands

| Command                  | Description                                                      |
| ------------------------ | ---------------------------------------------------------------- |
| `holomush core`          | Start the core process                                           |
| `holomush gateway`       | Start the gateway process                                        |
| `holomush migrate <cmd>` | Database migration management (up, down, status, version, force) |
| `holomush status`        | Check health of core and gateway                                 |
| `holomush --help`        | Show available commands                                          |

### Core Flags

The core process runs the game engine and plugin runtime.

```bash
holomush core [flags]
```

| Flag             | Default          | Description                       |
| ---------------- | ---------------- | --------------------------------- |
| `--grpc-addr`    | `localhost:9000` | gRPC listen address for gateway   |
| `--control-addr` | `127.0.0.1:9001` | Control plane gRPC address (mTLS) |
| `--metrics-addr` | `127.0.0.1:9100` | Metrics and health HTTP endpoint  |
| `--data-dir`     | XDG_DATA_HOME    | Directory for runtime data        |
| `--game-id`      | Auto-generated   | Unique game instance identifier   |
| `--log-format`   | `json`           | Log format: `json` or `text`      |
| `--skip-seed-migrations` | `false` | Disable automatic seed policy upgrades |
| `--config`       | XDG default      | Path to YAML config file          |

**Example:**

```bash
holomush core \
  --grpc-addr=0.0.0.0:9000 \
  --control-addr=0.0.0.0:9001 \
  --metrics-addr=0.0.0.0:9100 \
  --log-format=text
```

### Gateway Flags

The gateway process handles telnet and web client connections.

```bash
holomush gateway [flags]
```

| Flag             | Default          | Description                       |
| ---------------- | ---------------- | --------------------------------- |
| `--telnet-addr`  | `:4201`          | Telnet server listen address      |
| `--core-addr`    | `localhost:9000` | Core gRPC server address          |
| `--control-addr` | `127.0.0.1:9002` | Control plane gRPC address (mTLS) |
| `--metrics-addr` | `127.0.0.1:9101` | Metrics and health HTTP endpoint  |
| `--log-format`   | `json`           | Log format: `json` or `text`      |

**Example:**

```bash
holomush gateway \
  --telnet-addr=:4201 \
  --core-addr=core.internal:9000 \
  --metrics-addr=0.0.0.0:9101 \
  --log-format=json
```

### Migrate Commands

The migrate commands manage database schema migrations.

```bash
# Apply all pending migrations
holomush migrate up

# Apply migrations in dry-run mode (shows what would be applied)
holomush migrate up --dry-run

# Rollback one migration
holomush migrate down

# Rollback all migrations
holomush migrate down --all

# Show current migration status
holomush migrate status

# Show current version only
holomush migrate version

# Force set version (for dirty state recovery)
holomush migrate force <version>
```

All migrate commands require `DATABASE_URL` environment variable.

## Environment Variables

| Variable          | Required | Description                                    |
| ----------------- | -------- | ---------------------------------------------- |
| `DATABASE_URL`    | Core     | PostgreSQL connection string                   |
| `XDG_CONFIG_HOME` | No       | Configuration directory (default: `~/.config`) |
| `XDG_DATA_HOME`   | No       | Data directory (default: `~/.local/share`)     |
| `XDG_STATE_HOME`  | No       | State directory (default: `~/.local/state`)    |

### DATABASE_URL Format

PostgreSQL connection string format:

```text
postgres://user:password@host:port/database?sslmode=mode
```

**SSL Modes:**

| Mode          | Description                          |
| ------------- | ------------------------------------ |
| `disable`     | No SSL (development only)            |
| `require`     | SSL required, no certificate check   |
| `verify-ca`   | SSL with CA certificate verification |
| `verify-full` | SSL with full hostname verification  |

**Examples:**

```bash
# Local development
DATABASE_URL="postgres://holomush:secret@localhost:5432/holomush?sslmode=disable"

# Production with SSL
DATABASE_URL="postgres://holomush:${DB_PASSWORD}@db.example.com:5432/holomush?sslmode=verify-full"
```

## File Locations

HoloMUSH follows the XDG Base Directory Specification.

| Directory                  | Purpose                        |
| -------------------------- | ------------------------------ |
| `~/.config/holomush/`      | Configuration files, TLS certs |
| `~/.local/share/holomush/` | Persistent data                |
| `~/.local/state/holomush/` | Runtime state, logs, PID files |

### Generated Files

On first startup, core automatically creates:

| File                                    | Description             |
| --------------------------------------- | ----------------------- |
| `~/.config/holomush/certs/ca.pem`       | Certificate authority   |
| `~/.config/holomush/certs/core.pem`     | Core server certificate |
| `~/.config/holomush/certs/core-key.pem` | Core server private key |
| `~/.config/holomush/config.yaml`        | Server configuration    |

## Docker Configuration

### Environment Variables in Docker

Pass configuration via environment variables:

```yaml
services:
  core:
    image: ghcr.io/holomush/holomush:latest
    command: ["core", "--grpc-addr=0.0.0.0:9000"]
    environment:
      DATABASE_URL: postgres://holomush:${POSTGRES_PASSWORD}@postgres:5432/holomush?sslmode=disable
      HOME: /home/holomush
```

### Exposed Ports

| Port | Service         | Protocol |
| ---- | --------------- | -------- |
| 4201 | Telnet          | TCP      |
| 8080 | WebSocket       | HTTP/WS  |
| 9000 | Core gRPC       | gRPC     |
| 9100 | Core metrics    | HTTP     |
| 9101 | Gateway metrics | HTTP     |

### Volume Mounts

Persist configuration and certificates:

```yaml
volumes:
  - holomush_config:/home/holomush/.config/holomush
  - postgres_data:/var/lib/postgresql/data
```

## Log Configuration

### Log Format

| Format | Use Case                       |
| ------ | ------------------------------ |
| `json` | Production, log aggregation    |
| `text` | Development, human readability |

**JSON output example:**

```json
{
  "time": "2026-01-24T10:00:00Z",
  "level": "INFO",
  "msg": "server started",
  "component": "core",
  "addr": "localhost:9000"
}
```

**Text output example:**

```text
2026-01-24T10:00:00Z INFO server started component=core addr=localhost:9000
```

### Log Levels

HoloMUSH uses standard slog levels:

| Level | Description                   |
| ----- | ----------------------------- |
| DEBUG | Verbose debugging information |
| INFO  | Normal operational messages   |
| WARN  | Warning conditions            |
| ERROR | Error conditions              |

## Network Configuration

### Address Format

Addresses support various formats:

| Format             | Description               |
| ------------------ | ------------------------- |
| `:4201`            | All interfaces, port 4201 |
| `localhost:4201`   | Loopback only             |
| `0.0.0.0:4201`     | All IPv4 interfaces       |
| `[::]:4201`        | All IPv6 interfaces       |
| `192.168.1.1:4201` | Specific interface        |

### Production Recommendations

| Setting         | Development      | Production                      |
| --------------- | ---------------- | ------------------------------- |
| gRPC address    | `localhost:9000` | Internal network only           |
| Control address | `127.0.0.1:9001` | `127.0.0.1:9001` (never expose) |
| Metrics address | `127.0.0.1:9100` | Internal network only           |
| Telnet address  | `:4201`          | Behind load balancer            |
| SSL mode        | `disable`        | `verify-full`                   |

## Config File

### Location

HoloMUSH looks for a config file at:

```text
~/.config/holomush/config.yaml
```

This path respects `XDG_CONFIG_HOME`. If `XDG_CONFIG_HOME` is set, the file is at
`$XDG_CONFIG_HOME/holomush/config.yaml`.

To use a different file, pass `--config` to any command:

```bash
holomush core --config /etc/holomush/prod.yaml
holomush gateway --config /etc/holomush/prod.yaml
```

### Precedence Rules

| Source       | Precedence | Notes                                         |
| ------------ | ---------- | --------------------------------------------- |
| CLI flags    | Highest    | Always win over config file and defaults      |
| Config file  | Middle     | Overrides built-in defaults only              |
| `DATABASE_URL` | Env-only | No config file equivalent; set in environment |
| Defaults     | Lowest     | Used when no flag or config file value exists |

`DATABASE_URL` is intentionally env-only to avoid storing credentials in config files
that may be checked into version control.

### First-Run Experience

A config file is never required. On first run, all defaults apply and every option is
available as a CLI flag. You can adopt a config file incrementally — start with nothing
and add keys only when you want to override a default persistently.

### Full Annotated Example

```yaml
# Core process configuration.
# Equivalent to flags on: holomush core
core:
  # gRPC listen address — gateway connects here.
  # Flag: --grpc-addr
  # Default: "localhost:9000"
  grpc_addr: "localhost:9000"

  # Control plane mTLS address for admin operations.
  # Flag: --control-addr
  # Default: "127.0.0.1:9001"
  control_addr: "127.0.0.1:9001"

  # Metrics and health HTTP endpoint.
  # Set to empty string to disable.
  # Flag: --metrics-addr
  # Default: "127.0.0.1:9100"
  metrics_addr: "127.0.0.1:9100"

  # Data directory for persistent runtime files.
  # Empty string uses XDG_DATA_HOME/holomush (~/.local/share/holomush).
  # Flag: --data-dir
  # Default: "" (XDG_DATA_HOME/holomush)
  data_dir: ""

  # Unique identifier for this game instance.
  # Empty string triggers auto-generation on first start.
  # Flag: --game-id
  # Default: "" (auto-generated)
  game_id: ""

  # Log output format.
  # "json" for production/log aggregation, "text" for human-readable output.
  # Flag: --log-format
  # Default: "json"
  log_format: "json"

  # Disable automatic seed policy upgrades on startup.
  # Flag: --skip-seed-migrations
  # Default: false
  skip_seed_migrations: false

# Gateway process configuration.
# Equivalent to flags on: holomush gateway
gateway:
  # Telnet server listen address.
  # Flag: --telnet-addr
  # Default: ":4201"
  telnet_addr: ":4201"

  # Core gRPC server address to connect to.
  # Flag: --core-addr
  # Default: "localhost:9000"
  core_addr: "localhost:9000"

  # Control plane mTLS address for admin operations.
  # Flag: --control-addr
  # Default: "127.0.0.1:9002"
  control_addr: "127.0.0.1:9002"

  # Metrics and health HTTP endpoint.
  # Set to empty string to disable.
  # Flag: --metrics-addr
  # Default: "127.0.0.1:9101"
  metrics_addr: "127.0.0.1:9101"

  # Log output format.
  # "json" for production/log aggregation, "text" for human-readable output.
  # Flag: --log-format
  # Default: "json"
  log_format: "json"

  # Web client HTTP server listen address.
  # Serves the ConnectRPC API and embedded SvelteKit static files.
  # Flag: --web-addr
  # Default: ":8080"
  web_addr: ":8080"

  # Override embedded static files with a filesystem directory.
  # When set, serves files from this directory instead of the embedded
  # SvelteKit build. Useful for deploying custom builds without
  # recompiling the binary.
  # Flag: --web-dir
  # Default: "" (use embedded files)
  web_dir: ""

  # Allowed CORS origins for cross-origin requests.
  # Required for development when the Vite dev server (localhost:5173)
  # makes requests to the gateway (localhost:8080).
  # Default: [] (same-origin only — no CORS headers added)
  # Flag: --cors-origins
  cors_origins: []
  # Example for development:
  # cors_origins:
  #   - "http://localhost:5173"

# Game world configuration.
game:
  # ULID of the starting location assigned to guest connections.
  # Config file only — no CLI flag equivalent.
  # Default: "01HK153X0006AFVGQT61FPQX3S" (The Nexus seed location)
  guest_start_location: "01JMHZ5H3ZSBVTGARX4MSS1MBH"

# Status command configuration.
# Equivalent to flags on: holomush status
status:
  # Core control address to query for health information.
  # Flag: --core-addr
  # Default: "127.0.0.1:9001"
  core_addr: "127.0.0.1:9001"

  # Gateway control address to query for health information.
  # Flag: --gateway-addr
  # Default: "127.0.0.1:9002"
  gateway_addr: "127.0.0.1:9002"

  # Output status as JSON instead of human-readable text.
  # Flag: --json
  # Default: false
  json: false
```

### Per-Process Config with `--config`

The `--config` flag lets each process load a different file. This is useful when running
multiple game instances on the same host, or when separating core and gateway configs:

```bash
# Instance A
holomush core    --config /etc/holomush/game-a.yaml
holomush gateway --config /etc/holomush/game-a.yaml

# Instance B — different ports, different game-id
holomush core    --config /etc/holomush/game-b.yaml
holomush gateway --config /etc/holomush/game-b.yaml
```

Each config file contains only the sections relevant to the process reading it.
A gateway process ignores `core:` keys; a core process ignores `gateway:` keys.

CLI flags still override the selected config file, so you can share a base config and
override individual values at startup:

```bash
holomush core --config /etc/holomush/base.yaml --log-format=text
```

## Next Steps

- [Installation](installation.md) - Install HoloMUSH
- [Operations](operations.md) - Monitor and maintain your server
