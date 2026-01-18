# Getting Started with HoloMUSH

This guide covers setting up, running, and using HoloMUSH.

## Prerequisites

| Requirement   | Version | Purpose                        |
| ------------- | ------- | ------------------------------ |
| Go            | 1.24+   | Build and run the server       |
| Task          | Latest  | Task runner for build commands |
| Docker        | Latest  | Required for PostgreSQL        |
| Telnet client | Any     | Connect to the server          |

### Installing Prerequisites

**macOS (Homebrew):**

```bash
brew install go task
```

**Linux:**

```bash
# Go: https://go.dev/doc/install
# Task: https://taskfile.dev/installation/
```

## Installation

### Clone the Repository

```bash
git clone https://github.com/holomush/holomush.git
cd holomush
```

### Install Development Tools

```bash
task tools
```

This installs linters, formatters, and other development dependencies.

### Build the Server

```bash
task build
```

This creates the `holomush` binary in the project root.

## Running HoloMUSH

HoloMUSH uses a two-process architecture: **Core** (game engine) and **Gateway** (protocol servers). Both must be running for a functional server.

### Quick Start

```bash
# Terminal 1: Start PostgreSQL
docker run -d --name holomush-db \
  -e POSTGRES_DB=holomush \
  -e POSTGRES_USER=holomush \
  -e POSTGRES_PASSWORD=secret \
  -p 5432:5432 \
  postgres:18-alpine

# Terminal 2: Start Core
DATABASE_URL="postgres://holomush:secret@localhost:5432/holomush?sslmode=disable" ./holomush core

# Terminal 3: Start Gateway
./holomush gateway
```

### Development Mode

For development with human-readable logs:

```bash
# Start Core with text logging
DATABASE_URL="..." ./holomush core --log-format=text

# Start Gateway with text logging
./holomush gateway --log-format=text
```

## CLI Commands

### Core Commands

| Command         | Description            |
| --------------- | ---------------------- |
| `holomush core` | Start the core process |

**Core Flags:**

| Flag             | Description                  | Default          |
| ---------------- | ---------------------------- | ---------------- |
| `--grpc-addr`    | gRPC listen address          | `localhost:9000` |
| `--control-addr` | Control gRPC listen address  | `127.0.0.1:9001` |
| `--metrics-addr` | Metrics HTTP listen address  | `127.0.0.1:9100` |
| `--data-dir`     | Data directory               | XDG_DATA_HOME    |
| `--log-format`   | Log format: `json` or `text` | `json`           |

### Gateway Commands

| Command            | Description               |
| ------------------ | ------------------------- |
| `holomush gateway` | Start the gateway process |

**Gateway Flags:**

| Flag             | Description                  | Default          |
| ---------------- | ---------------------------- | ---------------- |
| `--telnet-addr`  | Telnet listen address        | `:4201`          |
| `--core-addr`    | Core gRPC address            | `localhost:9000` |
| `--control-addr` | Control gRPC listen address  | `127.0.0.1:9002` |
| `--metrics-addr` | Metrics HTTP listen address  | `127.0.0.1:9101` |
| `--log-format`   | Log format: `json` or `text` | `json`           |

### Global Commands

| Command           | Description                              |
| ----------------- | ---------------------------------------- |
| `holomush status` | Show health of core and gateway via gRPC |
| `holomush --help` | Show all available commands              |

## Configuration

### First Run

On first startup, Core automatically:

1. Initializes the database schema
2. Generates a unique `game_id` (stored in database)
3. Creates mTLS certificates in `$XDG_CONFIG_HOME/holomush/certs/`
4. Writes Gateway configuration to `$XDG_CONFIG_HOME/holomush/gateway.yaml`

### File Locations

HoloMUSH follows the XDG Base Directory Specification:

| Directory                  | Contents                 |
| -------------------------- | ------------------------ |
| `~/.config/holomush/`      | Configuration, TLS certs |
| `~/.local/state/holomush/` | Logs, PID files          |

### Environment Variables

| Variable          | Description                                          |
| ----------------- | ---------------------------------------------------- |
| `DATABASE_URL`    | PostgreSQL connection string (required for core)     |
| `XDG_CONFIG_HOME` | Override config directory (default: `~/.config`)     |
| `XDG_STATE_HOME`  | Override state directory (default: `~/.local/state`) |

## Connecting to the Server

### Using Telnet

```bash
telnet localhost 4201
```

You should see:

```text
Welcome to HoloMUSH!
Use: connect <username> <password>
```

### Test Credentials

For Phase 1, a single test account is available:

- **Username:** `testuser`
- **Password:** `password`

## Available Commands

After connecting with `connect testuser password`:

| Command | Usage                   | Description                                              |
| ------- | ----------------------- | -------------------------------------------------------- |
| `look`  | `look`                  | View the current location description                    |
| `say`   | `say Hello everyone!`   | Speak to others in the room                              |
| `pose`  | `pose waves cheerfully` | Emote an action (appears as "TestChar waves cheerfully") |
| `quit`  | `quit`                  | Disconnect from the server                               |

### Example Session

```text
> connect testuser password
Welcome back, TestChar!
> look
The Void
An empty expanse of nothing. This is where it all begins.
> say Hello, is anyone there?
You say, "Hello, is anyone there?"
> pose looks around curiously
TestChar looks around curiously
> quit
Goodbye!
```

## Session Persistence

HoloMUSH preserves your session when you disconnect:

1. Disconnect with `quit` or by closing the telnet connection
2. Events continue to be recorded while you're away
3. Reconnect with `connect testuser password`
4. Missed events are replayed:

```text
Welcome back, TestChar!
--- 3 missed events ---
[01HK153X] 01HK153X says, "Anyone here?"
[01HK154Y] 01HK154Y waves
[01HK155Z] 01HK155Z says, "Just arrived!"
--- end of replay ---
```

## Running Tests

```bash
# Unit tests
task test

# Tests with coverage report
task test:coverage
open coverage.html

# Integration tests (requires Docker)
task test:integration

# Short tests only
task test:short
```

## Development Workflow

### Code Quality

```bash
# Format code
task fmt

# Run linters
task lint

# Check markdown
task lint:markdown
```

### Task Commands Reference

| Command      | Description              |
| ------------ | ------------------------ |
| `task`       | Show all available tasks |
| `task build` | Build the binary         |
| `task dev`   | Run development server   |
| `task test`  | Run all tests            |
| `task lint`  | Run all linters          |
| `task fmt`   | Format all code          |
| `task clean` | Remove build artifacts   |

## Phase 1 Limitations

Current limitations that will be addressed in future phases:

| Feature    | Current State              | Planned                  |
| ---------- | -------------------------- | ------------------------ |
| Accounts   | Single hardcoded test user | Full account system      |
| Locations  | Single "The Void" room     | Multiple connected rooms |
| Characters | Single test character      | Character creation       |
| Movement   | Not implemented            | Exit-based navigation    |
| Web client | Not implemented            | SvelteKit PWA            |
| Plugins    | Proof-of-concept only      | Full WASM API            |

## Troubleshooting

### "Connection refused" when connecting

Ensure the server is running:

```bash
task dev
```

Check if something else is using port 4201:

```bash
lsof -i :4201
```

### Tests failing

Ensure dependencies are installed:

```bash
go mod download
task tools
```

### Linter errors

Run the formatter first:

```bash
task fmt
```

## Next Steps

- Read the [Architecture Overview](architecture-overview.md) to understand the system design
- Check the [full architecture document](../plans/2026-01-17-holomush-architecture-design.md) for detailed specifications
- See [CONTRIBUTING.md](../../CONTRIBUTING.md) for contribution guidelines
