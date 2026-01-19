# Goreleaser + Docker Compose Design

**Status:** Archived (superseded by roadmap)
**Date:** 2026-01-17
**Tasks:** holomush-6me, holomush-hju

## Overview

This design covers two related tasks:

1. **holomush-6me**: Complete goreleaser + Taskfile integration
2. **holomush-hju**: Docker Compose local development environment (depends on 6me)

The relationship: goreleaser builds Docker images that docker-compose uses.

## Task 6me: Goreleaser Taskfile Integration

### Current State

- `.goreleaser.yaml` - configured, builds multi-arch images to `ghcr.io/holomush/holomush`
- `.github/workflows/release.yaml` - release-please + goreleaser integration
- `Taskfile.yaml` - missing goreleaser tasks

### Changes Required

Add two Taskfile tasks for local goreleaser testing:

```yaml
release:check:
  desc: Validate goreleaser config
  cmds:
    - goreleaser check

release:snapshot:
  desc: Build snapshot locally (no publish, local arch only)
  cmds:
    - goreleaser build --snapshot --clean --single-target
```

### What's NOT Needed

- **Changelog task**: release-please handles changelog generation in release PRs
- **release-please-config.json**: The default `release-type: go` works without explicit config

## Task hju: Docker Compose Environment

### Architecture

```text
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  PostgreSQL │◄────│    Core     │◄────│   Gateway   │
│   :5432     │     │  gRPC:9000  │     │ Telnet:4201 │
└─────────────┘     │ Metrics:9100│     │ Metrics:9101│
                    └─────────────┘     └─────────────┘
                           │                   │
                           └───────┬───────────┘
                                   │
                           ┌───────▼───────┐
                           │ holomush_config│
                           │   (volume)     │
                           │  TLS certs     │
                           └───────────────┘
```

### TLS Certificate Sharing

Core generates TLS certificates on first startup. Gateway needs these certs to establish mTLS connection to Core. Solution:

1. Shared volume (`holomush_config`) mounted at `/home/holomush/.config/holomush` in both containers
2. `HOME=/home/holomush` environment variable (XDG spec: config defaults to `$HOME/.config`)
3. Core healthcheck verifies readiness before gateway starts
4. Gateway `depends_on: core: condition: service_healthy`

> **Note:** The implementation uses `HOME` instead of `XDG_CONFIG_HOME` because the XDG spec
> derives config from `$HOME/.config` when `XDG_CONFIG_HOME` is unset. This is cleaner for
> containers since it also handles other XDG directories (data, state) correctly.

### docker-compose.yaml

```yaml
services:
  postgres:
    image: postgres:18-alpine
    environment:
      POSTGRES_DB: holomush
      POSTGRES_USER: holomush
      POSTGRES_PASSWORD: holomush
    volumes:
      - postgres_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U holomush"]
      interval: 2s
      timeout: 5s
      retries: 5

  core:
    image: ghcr.io/holomush/holomush:latest
    command: ["core", "--grpc-addr=0.0.0.0:9000", "--metrics-addr=0.0.0.0:9100"]
    environment:
      DATABASE_URL: postgres://holomush:holomush@postgres:5432/holomush?sslmode=disable
      HOME: /home/holomush
    volumes:
      - holomush_config:/home/holomush/.config/holomush
    depends_on:
      postgres:
        condition: service_healthy
    healthcheck:
      test: [
        "CMD",
        "wget",
        "-q",
        "--spider",
        "http://localhost:9100/healthz/readiness",
      ]
      interval: 2s
      timeout: 5s
      retries: 10

  gateway:
    image: ghcr.io/holomush/holomush:latest
    command: ["gateway", "--core-addr=core:9000", "--metrics-addr=0.0.0.0:9101"]
    environment:
      HOME: /home/holomush
    volumes:
      - holomush_config:/home/holomush/.config/holomush
    ports:
      - "4201:4201"
      - "8080:8080"
    depends_on:
      core:
        condition: service_healthy
    healthcheck:
      test: [
        "CMD",
        "wget",
        "-q",
        "--spider",
        "http://localhost:9101/healthz/readiness",
      ]
      interval: 2s
      timeout: 5s
      retries: 5

volumes:
  postgres_data:
  holomush_config:
```

### Usage

```bash
# Start all services
docker compose up -d

# View logs
docker compose logs -f

# Connect via telnet
telnet localhost 4201

# Stop and remove
docker compose down

# Stop and remove volumes (fresh start)
docker compose down -v
```

## Dependencies

```text
holomush-hju (Docker Compose)
    └── depends on ──► holomush-6me (Goreleaser Taskfile)
```

## Design Decisions

| Decision                             | Rationale                                          |
| ------------------------------------ | -------------------------------------------------- |
| Shared volume for certs              | Simpler than disabling TLS or pre-generating certs |
| PostgreSQL 18                        | Latest GA version                                  |
| Healthcheck via `/healthz/readiness` | Both servers expose this endpoint                  |
| Command overrides for bind addresses | Defaults use localhost, containers need 0.0.0.0    |
| No changelog task                    | release-please handles this                        |
