# Phase 4b: DevOps & Operational Review

**Reviewer**: DevOps Engineer
**PR**: #192 — Proto-first plugin architecture rework
**Scope**: CI pipeline, Taskfile, Docker build, observability, deployment

---

## Findings

### OPS-01: No CI step builds or tests binary plugin executables

**Severity**: High
**Risk**: Binary plugin (core-scenes) is never compiled or tested in CI. The `task build` step compiles only `./cmd/holomush`. The `task test` step runs unit tests but core-scenes lives outside the main module's test graph (it has its own `main` package). A broken binary plugin will pass all CI checks.

**Evidence**: `.github/workflows/ci.yaml` build job (line 258): `task build` which expands to `go build -o holomush ./cmd/holomush`. No step runs `go build ./plugins/core-scenes/`. Unit tests for `plugins/core-scenes/` (service_test.go, store_test.go) exist but are integration-style tests that likely require a DB — unclear if `task test` (which runs `./...`) even reaches them since there's no `go.mod` in that directory indicating they compile against the root module.

**Recommendation**:

1. Add `task plugin:build` that cross-compiles binary plugins for linux/amd64.
2. Add a CI step that verifies binary plugins compile: `go build ./plugins/core-scenes/`.
3. Ensure `task pr-prep` includes plugin compilation verification.

---

### OPS-02: Docker image copies plugin dir but binary plugin is not compiled for linux/amd64

**Severity**: High
**Risk**: `Dockerfile` line 20 copies `plugins/` into the image, but `task docker:build` only compiles the main server binary (`CGO_ENABLED=0 GOOS=linux go build ... ./cmd/holomush`). The core-scenes binary plugin is never compiled for the target platform. The container will have Lua plugin manifests but a missing or wrong-architecture executable for core-scenes.

**Evidence**: `Taskfile.yaml` lines 147-150 (`docker:build`) and `Dockerfile` line 20 (`COPY --chown=holomush:holomush plugins/ ...`). The `plugin.yaml` declares `executable: core-scenes` but no build step produces that binary.

**Recommendation**: Extend `docker:build` to compile binary plugins before the Docker build:

```yaml
- CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o plugins/core-scenes/core-scenes ./plugins/core-scenes/
- CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o holomush ./cmd/holomush
- docker build -t holomush .
```

---

### OPS-03: GoReleaser does not build or package binary plugins

**Severity**: Medium
**Risk**: `.goreleaser.yaml` builds only `./cmd/holomush`. Release artifacts (tarballs, Docker images) will not contain compiled binary plugins. Operators installing from a release will have no scene plugin binary.

**Evidence**: `.goreleaser.yaml` lines 16-31 define a single build with `main: ./cmd/holomush`. The `archives.files` list (lines 38-40) includes only `LICENSE` and `README.md`, not plugin binaries or directories.

**Recommendation**: Add a second build entry for the scene plugin, or add a `before.hooks` step that compiles plugins into the archive directory. Include `plugins/` in archive files. Long-term, consider a plugin distribution strategy (separate plugin artifacts vs. bundled).

---

### OPS-04: `task pr-prep` does not verify proto generation freshness

**Severity**: Medium
**Risk**: PR #192 adds two new proto files (`world.proto`, `scene.proto`). `task pr-prep` checks schema freshness (`generate:schema`) but does not verify that generated Go code from proto files is current. A developer could modify a `.proto` file, forget to run `buf generate`, and CI might not catch the drift since the CI lint job also lacks a proto freshness check.

**Evidence**: `Taskfile.yaml` lines 394-420 (`pr-prep`). The `generate:schema` check is present but there is no `buf generate` + diff check. The `buf.yml` CI workflow only runs on proto file changes (path filter), not on every PR.

**Recommendation**: Add a proto freshness check to `pr-prep`:

```yaml
- echo "Verifying proto codegen is current..."
- task: proto
- cmd: git diff --exit-code internal/proto/ pkg/proto/ || { echo "ERROR: Proto codegen out of sync. Run 'task proto'."; exit 1; }
```

---

### OPS-05: Plugin subprocess inherits full server environment

**Severity**: Medium
**Risk**: `DefaultClientFactory.NewClient` (goplugin/host.go line 64) creates subprocesses via `exec.Command(execPath)` without restricting the environment. The subprocess inherits `DATABASE_URL`, `OTEL_EXPORTER_OTLP_ENDPOINT`, and any other server environment variables. A malicious or buggy plugin could connect to the admin database or exfiltrate credentials.

**Note**: Already flagged as SEC-06 in prior phases. Including here for operational completeness since the fix is an operational concern (env filtering at the subprocess boundary).

**Recommendation**: Construct a minimal `Cmd.Env` with only the plugin-scoped connection string and necessary runtime vars (PATH, HOME, TZ). Exclude DATABASE_URL, secrets, and admin credentials.

---

### OPS-06: Schema provisioner pool has no size limits or lifetime configuration

**Severity**: Medium
**Risk**: `SchemaProvisioner.Init()` calls `pgxpool.New(ctx, sp.baseConnString)` with default pool settings. The default pgxpool creates up to 4 connections per CPU core. This admin pool is only used for DDL operations during plugin loading (one-time), but remains open for the server's lifetime. With many binary plugins, initial DDL could saturate the pool.

**Evidence**: `internal/plugin/schema_provisioner.go` line 33 — no `pgxpool.Config` customization. Already noted as PERF-4 in prior phases.

**Recommendation**: Configure `MaxConns: 2` and `MaxConnLifetime: 5*time.Minute` since this pool only runs DDL at startup. Alternatively, use a single `*pgx.Conn` instead of a pool since operations are serial during plugin loading.

---

### OPS-07: PluginSubsystem does not implement HealthReporter

**Severity**: Medium
**Risk**: The lifecycle system supports per-subsystem health reporting via `HealthReporter` interface, and the readiness gate blocks startup until all reporters are Warm/Degraded. However, `PluginSubsystem` does not implement `HealthReporter`. If a binary plugin subprocess crashes after startup, the server's readiness probe won't reflect the degradation. The `/healthz/readiness` endpoint will continue returning 200.

**Evidence**: `internal/plugin/setup/subsystem.go` — no `HealthStatus()` method. `internal/lifecycle/health.go` defines the `HealthReporter` interface. `RegisteredService.Health` field exists (registered_service.go line 32) but is always nil — service registration in manager.go line 298 never populates it.

**Recommendation**: Implement `HealthReporter` on `PluginSubsystem` that checks if required binary plugins are still responsive (e.g., gRPC health check or process liveness). Register it with the `ReadinessRegistry`.

---

### OPS-08: Binary plugin Close uses Kill() without graceful shutdown

**Severity**: Low
**Risk**: `goplugin.Host.Close()` (host.go line 447) calls `p.client.Kill()` for each loaded plugin. HashiCorp go-plugin's `Kill()` sends SIGKILL, which does not give the plugin process time to flush database connections, complete in-flight RPCs, or run deferred cleanup. For the scene plugin with its own PostgreSQL connection pool, this could leave abandoned connections or uncommitted transactions.

**Evidence**: `internal/plugin/goplugin/host.go` lines 446-452. The scene plugin's `NewSceneStore` opens a pgxpool that won't be cleanly closed on SIGKILL.

**Recommendation**: Before `Kill()`, attempt a graceful shutdown: send a `Shutdown` RPC (or use go-plugin's `GracefulStop` if available) with a short timeout (e.g., 5s), then fall back to Kill. This matches the 10-second `shutdownTimeout` already defined in the subsystem.

---

### OPS-09: No `task plugin:build` or equivalent for local development

**Severity**: Low
**Risk**: Developers working on binary plugins have no documented or taskfile-supported workflow. The Taskfile has no `plugin:build`, `plugin:test`, or `plugin:dev` tasks. A developer must manually figure out how to compile and place the binary in the correct location.

**Evidence**: Full review of `Taskfile.yaml` — no task mentions `plugin` in any build/run context. The `task dev` workflow builds via `task docker:build` which also lacks plugin compilation.

**Recommendation**: Add plugin-related tasks:

```yaml
plugin:build:
  desc: Build all binary plugins for the current platform
  cmds:
    - go build -o plugins/core-scenes/core-scenes ./plugins/core-scenes/

plugin:build:linux:
  desc: Build binary plugins for linux/amd64 (Docker)
  cmds:
    - CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o plugins/core-scenes/core-scenes ./plugins/core-scenes/
```

---

### OPS-10: OTel middleware does not instrument Load/Unload operations

**Severity**: Low
**Risk**: `HostMiddleware` instruments `DeliverCommand` and `DeliverEvent` with tracing and metrics, but `Load` and `Unload` are pass-through (otel_middleware.go lines 90-97). Plugin load failures, load duration, and unload events have no observability signal. Operators troubleshooting startup issues or plugin hot-reload problems lack telemetry for the most critical lifecycle operations.

**Evidence**: `internal/plugin/otel_middleware.go` — `Load` and `Unload` delegate directly without spans or counters.

**Recommendation**: Add a `plugin_load_duration_seconds` histogram and `plugin_load_errors_total` counter. Wrap `Load` and `Unload` with spans including plugin name and type attributes. This is low severity because load happens once at startup, but becomes important as hot-reload capabilities develop.

---

## Summary

| ID | Severity | Category | Description |
|----|----------|----------|-------------|
| OPS-01 | High | CI | Binary plugins not compiled or tested in CI |
| OPS-02 | High | Docker | Docker image missing compiled binary plugin |
| OPS-03 | Medium | Release | GoReleaser does not build/package binary plugins |
| OPS-04 | Medium | CI | Proto generation freshness not verified in pr-prep |
| OPS-05 | Medium | Security | Plugin subprocess inherits full environment (cross-ref SEC-06) |
| OPS-06 | Medium | Performance | Schema provisioner pool unconfigured (cross-ref PERF-4) |
| OPS-07 | Medium | Health | PluginSubsystem missing HealthReporter implementation |
| OPS-08 | Low | Deployment | Binary plugin Kill() skips graceful shutdown |
| OPS-09 | Low | DX | No Taskfile support for binary plugin development |
| OPS-10 | Low | Observability | Load/Unload lifecycle not instrumented |

**High-severity items (OPS-01, OPS-02)** are blocking for production use of binary plugins. The core-scenes plugin is architecturally wired but operationally unreachable in both CI and Docker deployments. These should be addressed before merge or explicitly documented as "binary plugin path is scaffold-only in this PR."
