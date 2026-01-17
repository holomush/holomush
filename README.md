# HoloMUSH

A modern MUSH platform combining classic text-based multiplayer gameplay with contemporary technology.

[![CI](https://github.com/holomush/holomush/actions/workflows/ci.yaml/badge.svg)](https://github.com/holomush/holomush/actions/workflows/ci.yaml)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/holomush/holomush)](https://goreportcard.com/report/github.com/holomush/holomush)

## Features

- **Dual Protocol Support**: Simultaneous web and telnet access
- **Modern Architecture**: Event-oriented Go core with PostgreSQL storage
- **WASM Plugins**: Language-agnostic plugin system (Rust, Go, Python, etc.)
- **Resumable Sessions**: Tmux-style reconnection with event replay
- **Offline-Capable**: PWA web client with sync support
- **Platform First**: Build for RP and sandbox gameplay

## Status

🚧 **Early Development** - Not yet ready for production use.

See [docs/plans/](docs/plans/) for architecture and design documents.

## Quick Start

```bash
# Install dependencies
task tools

# Run development server
task dev
```

## Development

This project uses [beads](https://github.com/steveyegge/beads) for task tracking and follows TDD principles.

```bash
# Find ready tasks
bd ready

# Run tests
task test

# Lint code
task lint
```

See [CLAUDE.md](CLAUDE.md) for development guidelines.

## Documentation

- [Architecture Design](docs/plans/2026-01-17-holomush-architecture-design.md)
- [Repository Setup](docs/plans/2026-01-17-repo-setup-design.md)
- [Contributing](CONTRIBUTING.md)

## License

Apache 2.0 - See [LICENSE](LICENSE) for details.
