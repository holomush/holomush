# Goreleaser + Docker Compose Implementation Plan

**Status:** Archived (superseded by roadmap)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add Taskfile tasks for goreleaser testing and create docker-compose for local development.

**Architecture:** Two independent tasks - Taskfile additions for goreleaser validation/snapshot builds, and docker-compose.yaml for running the full stack locally with PostgreSQL.

**Tech Stack:** Taskfile, goreleaser, docker-compose, PostgreSQL 18

---

## Task 1: Add Goreleaser Taskfile Tasks (holomush-6me)

**Files:**

- Modify: `Taskfile.yaml` (add after line 228, before EOF)

**Step 1: Add release:check task**

Add to `Taskfile.yaml` after the `hooks:uninstall` task:

```yaml
# Release
release:check:
  desc: Validate goreleaser config
  cmds:
    - goreleaser check
```

**Step 2: Add release:snapshot task**

Add after `release:check`:

```yaml
release:snapshot:
  desc: Build snapshot locally (no publish, local arch only)
  cmds:
    - goreleaser build --snapshot --clean --single-target
```

**Step 3: Verify goreleaser check works**

Run: `task release:check`
Expected: "config is valid" or similar success message

**Step 4: Commit**

```bash
git add Taskfile.yaml
git commit -m "feat(taskfile): add release:check and release:snapshot tasks

Adds Taskfile tasks for local goreleaser testing:
- release:check - validates .goreleaser.yaml
- release:snapshot - builds snapshot without publishing

Closes: holomush-6me

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

**Step 5: Close bead 6me**

Run: `bd close 6me`

---

## Task 2: Create Docker Compose Configuration (holomush-hju)

**Files:**

- Create: `docker-compose.yaml`

**Step 1: Create docker-compose.yaml**

Create `docker-compose.yaml` in project root:

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

**Step 2: Validate compose syntax**

Run: `docker compose config`
Expected: Parsed YAML output with no errors

**Step 3: Commit**

```bash
git add docker-compose.yaml
git commit -m "feat: add docker-compose for local development

Self-contained docker-compose setup with:
- PostgreSQL 18 for event store
- Core process with database connection
- Gateway process with telnet/web ports exposed
- Shared volume for TLS cert sharing between core/gateway
- Health checks for proper startup ordering

Usage: docker compose up -d

Closes: holomush-hju

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

**Step 4: Close bead hju**

Run: `bd close hju`

---

## Task 3: Update bead hju description with reference

**Step 1: Update bead with plan reference**

Run:

```bash
bd update hju --description="Create docker-compose.yaml for local development that runs holomush with PostgreSQL.

**Depends on:** 6me (goreleaser builds the Docker images)

**Requirements:**
- Self-contained: \`docker compose up\` starts everything
- Uses images built by goreleaser
- PostgreSQL database with migrations
- Proper networking between services
- Volume mounts for persistence

**Reference:** docs/plans/2026-01-17-goreleaser-docker-compose-design.md"
```

**Step 2: Commit plan file**

```bash
git add docs/plans/2026-01-17-goreleaser-docker-compose-plan.md
git commit -m "docs: add implementation plan for goreleaser and docker-compose

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>"
```

---

## Verification

After completing all tasks:

1. `task release:check` - should validate goreleaser config
2. `docker compose config` - should parse without errors
3. `bd show 6me` - should show CLOSED
4. `bd show hju` - should show CLOSED

**Note:** Full docker-compose testing requires published images from goreleaser. Test with `task release:snapshot` first to build local images, then tag for compose testing if needed.
