---
title: "Installation"
sidebar:
  order: 1
---

This guide covers installing HoloMUSH using Docker or pre-built binaries.

## KEK requirement

HoloMUSH requires a Key Encryption Key (KEK) to boot. The core server reads
the keyfile path from `HOLOMUSH_KEK_FILE` and the unlock passphrase from one
of three sources (first hit wins):

| Source                         | Description                                       |
| ------------------------------ | ------------------------------------------------- |
| `HOLOMUSH_KEK_PASSPHRASE`      | Passphrase as a plain env var                     |
| `HOLOMUSH_KEK_PASSPHRASE_FILE` | Path to a file containing the passphrase          |
| Interactive prompt             | Prompted on stdin when a TTY is attached          |

On first boot, pass `--auto-gen-kek` to mint the keyfile automatically. After
that the keyfile is never overwritten — if the file exists, `--auto-gen-kek`
is a no-op. Providing a wrong passphrase or a corrupt keyfile surfaces an error
at startup rather than silently degrading.

The admin-totp CLI (`holomush admin totp …`) uses the same `HOLOMUSH_KEK_FILE`
and `HOLOMUSH_KEK_PASSPHRASE` env vars but does not support `--auto-gen-kek`.
It requires a pre-existing keyfile.

## Docker Installation (Recommended)

Docker provides the simplest deployment method with automatic dependency management.

### Prerequisites

| Requirement    | Minimum Version |
| -------------- | --------------- |
| Docker         | 20.10+          |
| Docker Compose | 2.0+            |

### Using Docker Compose

Clone the repository and start all services:

```bash
git clone https://github.com/holomush/holomush.git
cd holomush
docker compose up -d
```

This starts three services:

| Service    | Description                    | Port |
| ---------- | ------------------------------ | ---- |
| `postgres` | PostgreSQL database            | 5432 |
| `core`     | Game engine and plugin runtime | 9000 |
| `gateway`  | Telnet and web client servers  | 4201, 8080 |

Verify the services are running:

```bash
docker compose ps
```

Connect to the server:

```bash
telnet localhost 4201
```

### Container Images

HoloMUSH publishes container images to GitHub Container Registry:

```bash
docker pull ghcr.io/holomush/holomush:latest
```

Available tags:

| Tag      | Description                |
| -------- | -------------------------- |
| `latest` | Most recent stable release |
| `v1.x.x` | Specific version           |
| `main`   | Latest development build   |

### Custom Docker Compose

For production deployments, customize the compose file:

```yaml
services:
  postgres:
    image: postgres:18-alpine
    environment:
      POSTGRES_DB: holomush
      POSTGRES_USER: holomush
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
    volumes:
      - postgres_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U holomush"]
      interval: 2s
      timeout: 5s
      retries: 5

  core:
    image: ghcr.io/holomush/holomush:latest
    command: ["core", "--grpc-addr=0.0.0.0:9000", "--metrics-addr=0.0.0.0:9100", "--auto-gen-kek"]
    environment:
      DATABASE_URL: postgres://holomush:${POSTGRES_PASSWORD}@postgres:5432/holomush?sslmode=disable
      HOME: /home/holomush
      # KEK is required. --auto-gen-kek mints the keyfile on first boot;
      # subsequent starts reuse it. Store the passphrase in a secrets manager
      # and reference it via HOLOMUSH_KEK_PASSPHRASE_FILE for production.
      HOLOMUSH_KEK_FILE: /home/holomush/.config/holomush/certs/master.key.enc
      HOLOMUSH_KEK_PASSPHRASE: ${HOLOMUSH_KEK_PASSPHRASE}
    volumes:
      - holomush_config:/home/holomush/.config/holomush
    depends_on:
      postgres:
        condition: service_healthy

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
      - core

volumes:
  postgres_data:
  holomush_config:
```

## Binary Installation

Download pre-built binaries for your platform.

### Download

Download the latest release from GitHub:

```bash
# Linux (amd64)
curl -LO https://github.com/holomush/holomush/releases/latest/download/holomush-linux-amd64.tar.gz
tar xzf holomush-linux-amd64.tar.gz

# macOS (arm64)
curl -LO https://github.com/holomush/holomush/releases/latest/download/holomush-darwin-arm64.tar.gz
tar xzf holomush-darwin-arm64.tar.gz
```

Move the binary to your PATH:

```bash
sudo mv holomush /usr/local/bin/
```

### PostgreSQL Setup

HoloMUSH requires PostgreSQL 18 or later.

**Quick start with Docker:**

```bash
docker run -d --name holomush-db \
  -e POSTGRES_DB=holomush \
  -e POSTGRES_USER=holomush \
  -e POSTGRES_PASSWORD=secret \
  -p 5432:5432 \
  postgres:18-alpine
```

**Or install natively:**

```bash
# Ubuntu/Debian
sudo apt install postgresql postgresql-contrib

# macOS
brew install postgresql@18
```

Create the database:

```bash
createdb holomush
createuser holomush -P
```

### Run Database Migrations

Before first run, apply database migrations:

```bash
DATABASE_URL="postgres://holomush:secret@localhost:5432/holomush?sslmode=disable" \
  holomush migrate up
```

### Start the Server

HoloMUSH uses a two-process architecture. Start both processes:

**Terminal 1 - Core (first start):**

```bash
DATABASE_URL="postgres://holomush:secret@localhost:5432/holomush?sslmode=disable" \
  HOLOMUSH_KEK_FILE="/etc/holomush/master.key.enc" \
  HOLOMUSH_KEK_PASSPHRASE="your-passphrase" \
  holomush core --auto-gen-kek
```

On subsequent starts, `--auto-gen-kek` is a no-op if the keyfile already exists:

```bash
DATABASE_URL="postgres://holomush:secret@localhost:5432/holomush?sslmode=disable" \
  HOLOMUSH_KEK_FILE="/etc/holomush/master.key.enc" \
  HOLOMUSH_KEK_PASSPHRASE="your-passphrase" \
  holomush core
```

**Terminal 2 - Gateway:**

```bash
holomush gateway
```

### Verify Installation

Check server health:

```bash
holomush status
```

Connect via telnet:

```bash
telnet localhost 4201
```

## Build from Source

For development or customization, build from source.

### Prerequisites

| Requirement | Version |
| ----------- | ------- |
| Go          | 1.24+   |
| Task        | Latest  |

### Build

```bash
git clone https://github.com/holomush/holomush.git
cd holomush
task build
```

The binary is created at `./holomush`.

### Install Development Tools

For development, install additional tooling:

```bash
task setup
```

## Systemd Service

For production Linux deployments, create systemd unit files.

**Core service (`/etc/systemd/system/holomush-core.service`):**

```ini
[Unit]
Description=HoloMUSH Core
After=network.target postgresql.service
Requires=postgresql.service

[Service]
Type=simple
User=holomush
Group=holomush
Environment=DATABASE_URL=postgres://holomush:secret@localhost:5432/holomush?sslmode=disable
Environment=HOLOMUSH_KEK_FILE=/etc/holomush/master.key.enc
# Use EnvironmentFile or HOLOMUSH_KEK_PASSPHRASE_FILE for the passphrase
# to avoid it appearing in systemd logs or process listings.
EnvironmentFile=/etc/holomush/kek-passphrase.env
ExecStart=/usr/local/bin/holomush core
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

**Gateway service (`/etc/systemd/system/holomush-gateway.service`):**

```ini
[Unit]
Description=HoloMUSH Gateway
After=network.target holomush-core.service
Requires=holomush-core.service

[Service]
Type=simple
User=holomush
Group=holomush
ExecStart=/usr/local/bin/holomush gateway
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
sudo systemctl daemon-reload
sudo systemctl enable holomush-core holomush-gateway
sudo systemctl start holomush-core holomush-gateway
```

## Next Steps

- [Configuration](/operating/reference/configuration/) - Customize server behavior
- [Operations](/operating/how-to/operations/) - Monitor and maintain your server
