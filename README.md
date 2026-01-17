# HoloMUSH

A modern MUSH platform combining classic text-based multiplayer gameplay with contemporary technology.

[![CI](https://github.com/holomush/holomush/actions/workflows/ci.yaml/badge.svg)](https://github.com/holomush/holomush/actions/workflows/ci.yaml)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/holomush/holomush)](https://goreportcard.com/report/github.com/holomush/holomush)

## Status

**Phase 1 Complete** - Telnet server with event-sourced architecture, session persistence, and event replay.

### What Works Now

- Connect via telnet and authenticate
- Chat using `say` and `pose` commands
- Disconnect and reconnect with missed event replay
- Events persisted to PostgreSQL or in-memory store
- WASM plugin host proof-of-concept

### Planned Features

- Web client (SvelteKit PWA)
- Multiple locations and movement
- Character creation and player accounts
- Full WASM plugin API
- ABAC access control

## Quick Start

### Prerequisites

- Go 1.23+
- [Task](https://taskfile.dev/) (task runner)
- Docker (for integration tests)

### Build and Run

```bash
# Install development tools
task tools

# Build the server
task build

# Run in development mode (in-memory store)
task dev

# Connect via telnet
telnet localhost 4000
```

### Test Credentials

- Username: `testuser`
- Password: `password`

### Available Commands

| Command                 | Description                          |
| ----------------------- | ------------------------------------ |
| `connect <user> <pass>` | Authenticate and enter the game      |
| `look`                  | Describe current location            |
| `say <message>`         | Speak to others in the room          |
| `pose <action>`         | Emote an action (e.g., `pose waves`) |
| `quit`                  | Disconnect (session persists)        |

## Development

This project uses [beads](https://github.com/steveyegge/beads) for task tracking and follows TDD principles.

```bash
# Run tests
task test

# Run tests with coverage
task test:coverage

# Lint code
task lint

# Format code
task fmt
```

See [CLAUDE.md](CLAUDE.md) for AI assistant guidelines.

## Documentation

- [Getting Started](docs/reference/getting-started.md) - Setup and usage guide
- [Architecture Overview](docs/reference/architecture-overview.md) - System design summary
- [Full Architecture Design](docs/plans/2026-01-17-holomush-architecture-design.md) - Detailed specifications
- [Contributing](CONTRIBUTING.md) - Contribution guidelines

## License

Apache 2.0 - See [LICENSE](LICENSE) for details.
