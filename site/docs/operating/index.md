# Operating HoloMUSH

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

- [Installation](installation.md) -- Docker, binaries, or build from source
- [Deployment](deployment.md) -- Production deployment with Docker Compose
- [Configuration](configuration.md) -- Flags, config files, and environment variables
- [Database](database.md) -- PostgreSQL setup, migrations, and maintenance
- [Authentication](authentication.md) -- Security properties, rate limiting, and session management
- [Telnet Security](telnet-security.md) -- Risks of cleartext telnet logins and how to mitigate them
- [CA Rotation](ca-rotation.md) -- When and how to rotate the internal mTLS certificate authority
- [Operations](operations.md) -- Health checks, metrics, monitoring, and troubleshooting
- [Verifying Releases](verifying-releases.md) -- Signature and provenance verification
