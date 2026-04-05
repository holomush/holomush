# Plugin Review Findings Remediation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Address all P0/P1 findings from the comprehensive code review of PR #192: security fixes, error sanitization, code quality, binary plugin build pipeline, Dockerfile, and CLAUDE.md update.

**Architecture:** Seven independent task groups: (1) scene plugin security + quality fixes, (2) storage SDK deduplication, (3) error sanitization at gRPC boundaries, (4) infrastructure fixes (InProcessConn, Manager, SchemaProvisioner, gRPC proxy), (5) binary plugin build pipeline + Dockerfile, (6) CLAUDE.md update, (7) pr-prep verification.

**Tech Stack:** Go 1.25, pgx/v5, gRPC, hashicorp/go-plugin, Task (taskfile.dev), Docker

**Spec:** `docs/superpowers/specs/2026-04-05-plugin-review-findings-remediation-design.md`

---

## File Structure

### Modified Files

| File | Changes |
|------|---------|
| `plugins/core-scenes/service.go` | Ownership checks, string constants, limit cap, remove internal/idgen import, fix error leakage |
| `plugins/core-scenes/store.go` | Delete duplicated migration runner + itoa, use SDK, fix `errors.Is` |
| `pkg/plugin/storage/storage.go` | Add `RunMigrationsFS(ctx, pool, fs.FS)` |
| `internal/world/grpc_server.go` | Sanitize error messages in mapWorldError |
| `internal/plugin/grpc_proxy.go` | Sanitize streamErr in proxy error messages |
| `internal/plugin/schema_provisioner.go` | Use `pgx.Identifier.Sanitize()` for DDL |
| `internal/plugin/inprocess_conn.go` | Store `*grpc.Server`, call `Stop()` in `Close()` |
| `internal/plugin/manager.go` | Fix `Close()` ordering (close hosts before clearing maps) |
| `Taskfile.yaml` | Add `plugin:build-all`, `plugin:build`, update `docker:build` and `pr-prep` |
| `Dockerfile` | Copy compiled plugin binaries from `build/plugins/` |
| `CLAUDE.md` | Update for new plugin architecture |

### New Files

| File | Responsibility |
|------|---------------|
| `scripts/build-plugins.sh` | Discover and cross-compile binary plugins |

---

## Task 1: Scene Plugin Security + Quality Fixes

Fix SEC-02, F-02, CQ-05, SEC-11, GP-02 in the scene plugin. These all touch `plugins/core-scenes/service.go` and `store.go`.

**Files:**

- Modify: `plugins/core-scenes/service.go`
- Modify: `plugins/core-scenes/store.go`

- [ ] **Step 1: Add string constants for scene states, roles, visibility**

At the top of `plugins/core-scenes/service.go`, after the imports, add:

```go
// Scene state constants.
const (
	stateActive = "active"
	statePaused = "paused"
	stateEnded  = "ended"

	visibilityOpen = "open"

	roleOwner   = "owner"
	roleMember  = "member"
	roleInvited = "invited"

	poseOrderFree = "free"

	maxListLimit    = 200
	defaultListLimit = 50
)
```

Then replace all string literals throughout the file:

- `"active"` → `stateActive`
- `"paused"` → `statePaused`
- `"ended"` → `stateEnded`
- `"open"` → `visibilityOpen`
- `"owner"` → `roleOwner`
- `"member"` → `roleMember`
- `"invited"` → `roleInvited`
- `"free"` → `poseOrderFree`

- [ ] **Step 2: Add ownership check to EndScene**

In `EndScene`, after fetching the scene and before the state check, add:

```go
if scene.OwnerID != req.GetSessionId() {
    return nil, status.Errorf(codes.PermissionDenied, "only the scene owner can end a scene")
}
```

- [ ] **Step 3: Add ownership check to InviteToScene**

In `InviteToScene`, add the same check after input validation:

```go
scene, err := s.store.GetScene(ctx, req.GetSceneId())
if err != nil {
    return nil, mapStoreError(err, "get_scene")
}
if scene.OwnerID != req.GetSessionId() {
    return nil, status.Errorf(codes.PermissionDenied, "only the scene owner can invite participants")
}
```

- [ ] **Step 4: Cap ListScenes limit**

In `ListScenes`, after the default limit assignment, add the cap:

```go
limit := int(req.GetLimit())
if limit <= 0 {
    limit = defaultListLimit
}
if limit > maxListLimit {
    limit = maxListLimit
}
```

- [ ] **Step 5: Replace internal/idgen import with oklog/ulid/v2**

In the imports, replace:

```go
"github.com/holomush/holomush/internal/idgen"
```

with:

```go
"crypto/rand"
"github.com/oklog/ulid/v2"
```

In `CreateScene`, replace `idgen.New().String()` with:

```go
sceneID := ulid.MustNew(ulid.Now(), rand.Reader).String()
```

- [ ] **Step 6: Fix mapStoreError to not leak raw errors**

Replace the `mapStoreError` function:

```go
func mapStoreError(err error, operation string) error {
    oopsErr, ok := oops.AsOops(err)
    if ok {
        if code, isStr := oopsErr.Code().(string); isStr {
            if strings.HasSuffix(code, "_NOT_FOUND") {
                return status.Errorf(codes.NotFound, "%s: not found", operation)
            }
        }
    }
    slog.Error("store operation failed", "operation", operation, "error", err)
    return status.Errorf(codes.Internal, "%s failed", operation)
}
```

Add `"log/slog"` and `"strings"` to imports.

- [ ] **Step 7: Fix errors.Is in store.go**

In `plugins/core-scenes/store.go`, replace all occurrences of:

```go
if err == pgx.ErrNoRows {
```

with:

```go
if errors.Is(err, pgx.ErrNoRows) {
```

Add `"errors"` to imports.

- [ ] **Step 8: Run tests**

Run: `task test -- ./plugins/core-scenes/`

Expected: All pass.

- [ ] **Step 9: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "fix(scenes): ownership auth, error sanitization, constants, limit cap, remove internal import"
```

---

## Task 2: Storage SDK Deduplication

Add `RunMigrationsFS` to the SDK, delete the duplicated code from the scene store.

**Files:**

- Modify: `pkg/plugin/storage/storage.go`
- Modify: `plugins/core-scenes/store.go`

- [ ] **Step 1: Add RunMigrationsFS to the SDK**

In `pkg/plugin/storage/storage.go`, add:

```go
// RunMigrationsFS runs embedded SQL migrations from an fs.FS.
// Use this when the migration files are nested in a subdirectory of the embed
// and you need to use fs.Sub first.
func RunMigrationsFS(ctx context.Context, pool *pgxpool.Pool, migrations fs.FS) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS plugin_migrations (
			version INTEGER PRIMARY KEY,
			name    TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return oops.Code("PLUGIN_MIGRATION_TABLE_FAILED").Wrap(err)
	}

	var currentVersion int
	err = pool.QueryRow(ctx, "SELECT COALESCE(MAX(version), 0) FROM plugin_migrations").Scan(&currentVersion)
	if err != nil {
		return oops.Code("PLUGIN_MIGRATION_VERSION_FAILED").Wrap(err)
	}

	entries, err := fs.ReadDir(migrations, ".")
	if err != nil {
		return oops.Code("PLUGIN_MIGRATION_READ_FAILED").Wrap(err)
	}

	var upFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			upFiles = append(upFiles, e.Name())
		}
	}
	sort.Strings(upFiles)

	for _, name := range upFiles {
		version := parseMigrationVersion(name)
		if version <= currentVersion {
			continue
		}
		sql, readErr := fs.ReadFile(migrations, name)
		if readErr != nil {
			return oops.Code("PLUGIN_MIGRATION_READ_FAILED").With("file", name).Wrap(readErr)
		}
		if _, execErr := pool.Exec(ctx, string(sql)); execErr != nil {
			return oops.Code("PLUGIN_MIGRATION_EXEC_FAILED").With("file", name).Wrap(execErr)
		}
		if _, trackErr := pool.Exec(ctx,
			"INSERT INTO plugin_migrations (version, name) VALUES ($1, $2)",
			version, name,
		); trackErr != nil {
			return oops.Code("PLUGIN_MIGRATION_TRACK_FAILED").With("file", name).Wrap(trackErr)
		}
	}
	return nil
}
```

Add `"io/fs"` to imports. Update existing `RunMigrations` to delegate:

```go
func RunMigrations(ctx context.Context, pool *pgxpool.Pool, migrations embed.FS) error {
	return RunMigrationsFS(ctx, pool, migrations)
}
```

- [ ] **Step 2: Delete duplicated code from scene store**

In `plugins/core-scenes/store.go`:

- Delete the `runMigrationsFromFS` function entirely
- Delete the `parseMigrationVersion` function
- Delete the `itoa` function (if still present)
- In `NewSceneStore`, replace the local migration call with:

```go
sub, err := fs.Sub(migrations, "migrations")
if err != nil {
    pool.Close()
    return nil, oops.Code("SCENE_STORE_INIT_FAILED").Wrap(err)
}
if err := storage.RunMigrationsFS(ctx, pool, sub); err != nil {
    pool.Close()
    return nil, err
}
```

- [ ] **Step 3: Run tests**

Run: `task test -- ./pkg/plugin/storage/ ./plugins/core-scenes/`

Expected: All pass.

- [ ] **Step 4: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "refactor(storage): add RunMigrationsFS to SDK, deduplicate scene store migration runner"
```

---

## Task 3: Error Sanitization at gRPC Boundaries

Fix SEC-05 (WorldService) and SEC-07 (gRPC proxy).

**Files:**

- Modify: `internal/world/grpc_server.go`
- Modify: `internal/plugin/grpc_proxy.go`

- [ ] **Step 1: Sanitize mapWorldError**

Replace the `mapWorldError` function in `internal/world/grpc_server.go`:

```go
func mapWorldError(err error) error {
	oopsErr, ok := oops.AsOops(err)
	if !ok {
		slog.Error("world service error", "error", err)
		return status.Errorf(codes.Internal, "internal error")
	}

	code, ok2 := oopsErr.Code().(string)
	if !ok2 {
		slog.Error("world service error (no code)", "error", err)
		return status.Errorf(codes.Internal, "internal error")
	}
	switch {
	case strings.HasSuffix(code, "_NOT_FOUND"):
		return status.Errorf(codes.NotFound, "not found")
	case strings.HasSuffix(code, "_ACCESS_DENIED"):
		return status.Errorf(codes.PermissionDenied, "access denied")
	default:
		slog.Error("world service error", "code", code, "error", err)
		return status.Errorf(codes.Internal, "internal error")
	}
}
```

Add `"log/slog"` to imports.

- [ ] **Step 2: Sanitize gRPC proxy stream error**

In `internal/plugin/grpc_proxy.go`, replace the `NewStream` error handling (around line 57-63):

```go
clientStream, streamErr := svc.Conn.NewStream(
    stream.Context(),
    &grpc.StreamDesc{ServerStreams: true, ClientStreams: true},
    method,
)
if streamErr != nil {
    slog.Error("failed to create proxy stream", "service", serviceName, "error", streamErr)
    return status.Errorf(codes.Internal, "service temporarily unavailable")
}
```

Add `"log/slog"` to imports.

- [ ] **Step 3: Run tests**

Run: `task test -- ./internal/world/ ./internal/plugin/`

Expected: All pass. Some tests may need updating if they asserted on the old error message format.

- [ ] **Step 4: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "fix(security): sanitize error messages at gRPC boundaries (SEC-05, SEC-07)"
```

---

## Task 4: Infrastructure Fixes

Fix CQ-07 (InProcessConn), CQ-10 (Manager.Close), SEC-03 (schema provisioner).

**Files:**

- Modify: `internal/plugin/inprocess_conn.go`
- Modify: `internal/plugin/manager.go`
- Modify: `internal/plugin/schema_provisioner.go`
- Modify: `internal/plugin/setup/world_conn.go`

- [ ] **Step 1: Store grpc.Server in InProcessConn and stop on Close**

In `internal/plugin/inprocess_conn.go`, add `server` field:

```go
type InProcessConn struct {
	conn     *grpc.ClientConn
	listener *bufconn.Listener
	server   *grpc.Server
}
```

Update `NewInProcessConn` to store it:

```go
func NewInProcessConn(srv *grpc.Server) (*InProcessConn, error) {
	lis := bufconn.Listen(inProcessBufSize)

	go func() {
		_ = srv.Serve(lis) //nolint:errcheck // Serve returns when listener is closed
	}()

	// ... (existing dialer + conn creation) ...

	return &InProcessConn{conn: conn, listener: lis, server: srv}, nil
}
```

Update `Close` to stop the server:

```go
func (c *InProcessConn) Close() error {
	c.server.Stop()
	connErr := c.conn.Close()
	lisErr := c.listener.Close()
	// ... (existing error handling)
}
```

- [ ] **Step 2: Fix Manager.Close ordering**

In `internal/plugin/manager.go` `Close()`, move the map clearing AFTER host closing:

```go
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Remove ABAC policies first.
	if m.policyInstaller != nil {
		for name := range m.loaded {
			if err := m.policyInstaller.RemovePluginPolicies(ctx, name); err != nil {
				slog.Error("failed to remove plugin policies", "plugin", name, "error", err)
			}
		}
	}

	// Close all registered hosts BEFORE clearing maps.
	for hostType, host := range m.hosts {
		if err := host.Close(ctx); err != nil {
			slog.Error("failed to close host", "type", hostType, "error", err)
		}
	}

	if m.luaHost != nil {
		if _, inMap := m.hosts[TypeLua]; !inMap {
			if err := m.luaHost.Close(ctx); err != nil {
				return oops.In("manager").With("operation", "close").Hint("failed to close lua host").Wrap(err)
			}
		}
	}

	// Clear maps after hosts are closed.
	m.loaded = make(map[string]*DiscoveredPlugin)
	m.pluginHosts = make(map[string]Host)

	return nil
}
```

- [ ] **Step 3: Use pgx.Identifier for schema provisioner DDL**

In `internal/plugin/schema_provisioner.go`, replace the DDL line:

```go
ddl := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schemaName)
```

with:

```go
identifier := pgx.Identifier{schemaName}
ddl := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", identifier.Sanitize())
```

Add `"github.com/jackc/pgx/v5"` to imports.

- [ ] **Step 4: Run tests**

Run: `task test -- ./internal/plugin/...`

Expected: All pass.

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "fix: InProcessConn graceful shutdown, Manager.Close ordering, schema provisioner quoting"
```

---

## Task 5: Binary Plugin Build Pipeline + Dockerfile

Add manifest-driven plugin compilation to the Taskfile and Docker build.

**Files:**

- Create: `scripts/build-plugins.sh`
- Modify: `Taskfile.yaml`
- Modify: `Dockerfile`
- Modify: `.gitignore`

- [ ] **Step 1: Create the plugin build script**

Create `scripts/build-plugins.sh`:

```bash
#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Discovers binary plugins from plugin.yaml manifests and compiles them.
# Output: build/plugins/<name>/<executable>
#
# Usage:
#   ./scripts/build-plugins.sh              # build all binary plugins
#   ./scripts/build-plugins.sh core-scenes  # build a single plugin

set -euo pipefail

PLUGINS_DIR="${PLUGINS_DIR:-plugins}"
BUILD_DIR="${BUILD_DIR:-build/plugins}"
GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-amd64}"

build_plugin() {
    local dir="$1"
    local name
    name=$(basename "$dir")
    local manifest="$dir/plugin.yaml"

    if [ ! -f "$manifest" ]; then
        return
    fi

    local ptype
    ptype=$(grep '^type:' "$manifest" | awk '{print $2}')
    if [ "$ptype" != "binary" ]; then
        return
    fi

    local exec_name
    exec_name=$(grep 'executable:' "$manifest" | awk '{print $2}')
    if [ -z "$exec_name" ]; then
        echo "ERROR: $manifest missing binary-plugin.executable" >&2
        exit 1
    fi

    local outdir="$BUILD_DIR/$name"
    mkdir -p "$outdir"

    echo "Building $name -> $outdir/$exec_name (${GOOS}/${GOARCH})"
    CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build -ldflags="-s -w" \
        -o "$outdir/$exec_name" "./$dir"

    # Copy plugin.yaml alongside the binary
    cp "$manifest" "$outdir/plugin.yaml"
}

# Single plugin mode
if [ $# -gt 0 ]; then
    target="$PLUGINS_DIR/$1"
    if [ ! -d "$target" ]; then
        echo "ERROR: plugin directory not found: $target" >&2
        exit 1
    fi
    build_plugin "$target"
    exit 0
fi

# Discovery mode: find all binary plugins
found=0
for dir in "$PLUGINS_DIR"/*/; do
    [ -d "$dir" ] || continue
    build_plugin "$dir"
    found=1
done

if [ "$found" -eq 0 ]; then
    echo "No binary plugins found in $PLUGINS_DIR"
fi
```

Make executable: `chmod +x scripts/build-plugins.sh`

- [ ] **Step 2: Add plugin tasks to Taskfile.yaml**

Add to the Taskfile after the `build` task:

```yaml
  plugin:build-all:
    desc: Discover and compile all binary plugins for linux/amd64
    cmds:
      - ./scripts/build-plugins.sh
    env:
      GOOS: linux
      GOARCH: amd64

  plugin:build:
    desc: Build a single binary plugin (pass name after --)
    cmds:
      - ./scripts/build-plugins.sh {{.CLI_ARGS}}
    env:
      GOOS: linux
      GOARCH: amd64
```

Update `docker:build` to depend on `plugin:build-all`:

```yaml
  docker:build:
    desc: Build Docker image from locally-compiled Linux binary
    deps: ['web:embed', 'plugin:build-all']
    cmds:
      - CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o holomush {{.MAIN_PKG}}
      - docker build -t holomush .
```

Update `docker:build:cover` similarly:

```yaml
  docker:build:cover:
    desc: Build Docker image with coverage instrumentation
    deps: ['web:embed', 'plugin:build-all']
    cmds:
      - CGO_ENABLED=0 GOOS=linux go build -cover -o holomush {{.MAIN_PKG}}
      - docker build -t holomush .
```

Add `plugin:build-all` step to `pr-prep`, before `test:int`:

```yaml
      - echo "▸ Building binary plugins..."
      - task: plugin:build-all
```

- [ ] **Step 3: Update Dockerfile to copy compiled plugin binaries**

Replace the `COPY plugins/` line in the Dockerfile:

```dockerfile
# Copy Lua plugins (source) and binary plugin manifests
COPY --chown=holomush:holomush plugins/ /home/holomush/.local/share/holomush/plugins/

# Copy compiled binary plugins (overwrite plugin dirs with compiled binaries)
COPY --chown=holomush:holomush build/plugins/ /home/holomush/.local/share/holomush/plugins/
```

This copies Lua plugin directories first (they have `main.lua` + `plugin.yaml`), then overlays the compiled binary plugins (which have the executable + `plugin.yaml`).

- [ ] **Step 4: Add build/ to .gitignore**

Append to `.gitignore`:

```text
# Compiled binary plugins
build/
```

- [ ] **Step 5: Test the pipeline**

Run: `task plugin:build-all`

Expected: `build/plugins/core-scenes/core-scenes` exists as a linux/amd64 binary.

Run: `file build/plugins/core-scenes/core-scenes`

Expected: `ELF 64-bit LSB executable, x86-64`

- [ ] **Step 6: Test Docker build**

Run: `task docker:build`

Expected: Docker image builds successfully with the plugin binary included.

- [ ] **Step 7: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "feat(build): manifest-driven binary plugin compilation pipeline + Dockerfile"
```

---

## Task 6: CLAUDE.md Update

Remove stale references to deleted code, add plugin architecture overview.

**Files:**

- Modify: `CLAUDE.md`

- [ ] **Step 1: Read current CLAUDE.md**

Read `CLAUDE.md` and identify sections referencing ServiceProxy, LocalPluginHost, type:core, or other deleted concepts.

- [ ] **Step 2: Update CLAUDE.md**

Key changes:

- Remove any references to `ServiceProxy`, `ServiceProxyImpl`, `ScopedProxy`
- Remove any references to `LocalPluginHost`, `type: core`
- Update the directory structure to include `build/plugins/`, `scripts/build-plugins.sh`
- Add plugin architecture section explaining:
  - Plugin types: lua, binary, setting
  - Manifest schema: requires, provides, storage
  - Service registry, DAG dependency resolution
  - Binary plugin build: `task plugin:build-all`
  - Plugin admin commands: `plugin list`, `plugin info`
- Update any testing sections to mention `task plugin:build-all` as prerequisite for integration tests

- [ ] **Step 3: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "docs: update CLAUDE.md for plugin architecture rework"
```

---

## Task 7: Full pr-prep Verification

- [ ] **Step 1: Run pr-prep**

Run: `task pr-prep`

Expected: All checks pass — schema, license, lint (0 issues), fmt, unit tests, integration tests, E2E tests.

- [ ] **Step 2: Fix any failures**

Address any issues found by pr-prep.

- [ ] **Step 3: Push updated branch**

```bash
jj bookmark set plugin-arch -r @-
jj --no-pager git push -b plugin-arch
```

---

## Dependency Map

```text
Task 1 (Scene plugin fixes)
Task 2 (Storage SDK dedup)        } All independent, can run in parallel
Task 3 (Error sanitization)       }
Task 4 (Infrastructure fixes)     }

Task 5 (Build pipeline + Docker)  — independent but benefits from Tasks 1-4 being done first

Task 6 (CLAUDE.md) — independent

Task 7 (pr-prep) — depends on all previous tasks
```
