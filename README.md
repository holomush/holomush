# HoloMUSH

A modern MUSH platform combining classic text-based multiplayer gameplay with contemporary technology.

[![CI](https://github.com/holomush/holomush/actions/workflows/ci.yaml/badge.svg)](https://github.com/holomush/holomush/actions/workflows/ci.yaml)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/holomush/holomush)](https://goreportcard.com/report/github.com/holomush/holomush)

## Status

**Core systems implemented** — Telnet server, event-sourced architecture, Lua plugin system, world model, and ABAC access control.

### What Works Now

- Telnet server with session persistence and event replay
- Lua plugin system (gopher-lua) with manifest-defined ABAC policies
- World model: locations, exits, objects, scenes, characters
- ABAC (Attribute-Based Access Control) with Cedar-inspired policy DSL
- Seed policies for player, builder, and admin roles
- Plugin policy lifecycle: install on load, remove on unload, atomic replace
- `say`, `pose`, `look`, `go`, building commands (`dig`, `link`)

### Planned Features

- Web client (SvelteKit PWA)
- Character creation and player accounts
- Go binary plugins (go-plugin) for complex extensions
- Operator admin tools and control plane

## Quick Start

### Prerequisites

- Go 1.23+
- [Task](https://taskfile.dev/) (task runner)
- PostgreSQL (for data storage)
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
| `go <exit>`             | Move through an exit                 |
| `dig <exit> to "<loc>"` | Create a new location with exit      |
| `link <exit> to <target>`| Link current location to another     |
| `help`                  | List available commands               |
| `quit`                  | Disconnect (session persists)        |

## Architecture

- **Go core** with event-oriented architecture
- **Dual protocol**: telnet + web (planned)
- **Lua plugins** (gopher-lua) with go-plugin for complex extensions (planned)
- **PostgreSQL** for production data (in-memory store available for development)
- **ABAC engine** with Cedar-inspired DSL, in-memory policy cache, pg_notify invalidation
- **SvelteKit PWA** for web client (planned)

See [Architecture](site/docs/contributors/architecture.md) for details.

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

# Generate EBNF grammar + railroad diagram
task generate:ebnf

# Generate plugin JSON schema
task generate:schema
```

See [CLAUDE.md](CLAUDE.md) for AI assistant guidelines.

## Release Verification

All releases are cryptographically signed and include SBOMs for vulnerability tracking.

```bash
# Download release assets (adjust VERSION and ARCH as needed)
VERSION="v1.0.0"
ARCH="linux_amd64"  # or: darwin_amd64, darwin_arm64, linux_arm64
gh release download "${VERSION}" -R holomush/holomush \
  -p "holomush_${VERSION#v}_${ARCH}.tar.gz" \
  -p "checksums.txt*"

# Verify checksums signature
cosign verify-blob \
  --certificate checksums.txt.sig.cert \
  --signature checksums.txt.sig \
  --certificate-identity-regexp "https://github.com/holomush/holomush/.*" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  checksums.txt

# Verify container image
cosign verify \
  --certificate-identity-regexp "https://github.com/holomush/holomush/.*" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  ghcr.io/holomush/holomush:v1.0.0
```

See [Verifying Releases](docs/reference/verifying-releases.md) for complete instructions.

## Documentation

- [Architecture](site/docs/contributors/architecture.md) - System design overview
- [Plugin Guide](site/docs/developers/plugin-guide.md) - Writing plugins with ABAC policies
- [Contributing](CONTRIBUTING.md) - Contribution guidelines

## License

Apache 2.0 - See [LICENSE](LICENSE) for details.
