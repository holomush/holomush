# Getting Started with HoloMUSH

This guide covers setting up, running, and using HoloMUSH in its current Phase 1 state.

## Prerequisites

| Requirement   | Version | Purpose                        |
| ------------- | ------- | ------------------------------ |
| Go            | 1.23+   | Build and run the server       |
| Task          | Latest  | Task runner for build commands |
| Docker        | Latest  | Required for integration tests |
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

## Running the Server

### Development Mode (In-Memory Store)

```bash
task dev
```

The server starts on port 4000 by default. All data is stored in memory and lost on restart.

### Custom Port

```bash
TELNET_ADDR=:4201 task dev
```

### Production Mode (PostgreSQL)

Phase 1 includes PostgreSQL support, but requires manual database setup:

```bash
# Start PostgreSQL
docker run -d --name holomush-db \
  -e POSTGRES_DB=holomush \
  -e POSTGRES_USER=holomush \
  -e POSTGRES_PASSWORD=secret \
  -p 5432:5432 \
  postgres:16

# Run with PostgreSQL
DATABASE_URL="postgres://holomush:secret@localhost:5432/holomush?sslmode=disable" ./holomush
```

## Connecting to the Server

### Using Telnet

```bash
telnet localhost 4000
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

Check if something else is using port 4000:

```bash
lsof -i :4000
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
