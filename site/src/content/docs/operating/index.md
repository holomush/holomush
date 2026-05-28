---
title: "Operating HoloMUSH"
---

Running HoloMUSH in production is straightforward. The server ships as a single
binary with PostgreSQL as its only external dependency. Install it, point it at a
database, and you're up.

## Requirements

| Component  | Minimum | Recommended |
| ---------- | ------- | ----------- |
| CPU        | 1 core  | 2+ cores    |
| Memory     | 256 MB  | 512 MB      |
| PostgreSQL | 18+     | 18+         |
| Storage    | 1 GB    | 10+ GB      |

## Connection Methods

HoloMUSH supports two ways for players to connect:

- **Telnet** (port 4201) -- Classic MU\* client compatibility
- **WebSocket** (port 8080) -- Modern web client with PWA support

Both protocols connect to the same game world and share the same session system.
Run one or both depending on your player base.

## Documentation

- [Installation](/operating/installation/) -- Docker, binaries, or build from source
- [Deployment](/operating/deployment/) -- Production deployment with Docker Compose
- [Configuration](/operating/configuration/) -- Flags, config files, and environment variables
- [Database](/operating/database/) -- PostgreSQL setup, migrations, and maintenance
- [Authentication](/operating/authentication/) -- Security properties, rate limiting, and session management
- [Telnet Security](/operating/telnet-security/) -- Risks of cleartext telnet logins and how to mitigate them
- [CA Rotation](/operating/ca-rotation/) -- When and how to rotate the internal mTLS certificate authority
- [Operations](/operating/operations/) -- Health checks, metrics, monitoring, and troubleshooting
- [Verifying Releases](/operating/verifying-releases/) -- Signature and provenance verification
