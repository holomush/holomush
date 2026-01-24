# Configuration

This guide covers all configuration options for HoloMUSH.

## Overview

HoloMUSH configuration follows a hierarchy:

1. Command-line flags (highest precedence)
2. Environment variables
3. Configuration files
4. Built-in defaults

## Command-Line Reference

### Global Commands

| Command            | Description                      |
| ------------------ | -------------------------------- |
| `holomush core`    | Start the core process           |
| `holomush gateway` | Start the gateway process        |
| `holomush migrate` | Run database migrations          |
| `holomush status`  | Check health of core and gateway |
| `holomush --help`  | Show available commands          |

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

### Migrate Command

The migrate command applies database schema changes.

```bash
holomush migrate
```

No flags. Requires `DATABASE_URL` environment variable.

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
| `~/.config/holomush/gateway.yaml`       | Gateway configuration   |

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

## Next Steps

- [Installation](installation.md) - Install HoloMUSH
- [Operations](operations.md) - Monitor and maintain your server
