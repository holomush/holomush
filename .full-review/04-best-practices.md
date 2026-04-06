# Phase 4: Best Practices & Standards

## Go Language Findings (12 total: 0 Critical, 0 High, 5 Medium, 7 Low)

### Medium

| ID | Description |
|----|-------------|
| GP-01 | `ServiceRegistry.Resolve` returns `&svc` (pointer to local copy) — detached pointer, latent footgun |
| GP-02 | Scene store uses `err == pgx.ErrNoRows` instead of `errors.Is` (misses wrapped errors) |
| GP-04 | `InProcessConn` starts goroutine with `srv.Serve()` but never calls `srv.Stop()` on close (overlap CQ-07) |
| GP-08 | Migration runner duplicated because SDK requires `embed.FS` instead of `fs.FS` (overlap CQ-01) |
| GP-10 | `proxyStreams` goroutine has no cancellation path when send-direction blocks (overlap CQ-06) |

### Low

| ID | Description |
|----|-------------|
| GP-03 | Scene `mapStoreError` oops code extraction inconsistent with rest of codebase |
| GP-05 | Uses `sort.Slice`/`sort.Strings` instead of Go 1.21+ `slices` package |
| GP-06 | `interface{}` in new code instead of `any` |
| GP-07 | WorldService deprecation notice misleading — narrow interface is correct |
| GP-09 | `parseMigrationVersion` duplicated (overlap CQ-01) |
| GP-11 | Scene error codes as string literals (overlap CQ-05) |
| GP-12 | Schema name DDL interpolation without quoting (overlap CQ-08) |

## CI/CD & DevOps Findings (10 total: 0 Critical, 2 High, 4 Medium, 4 Low)

### High

| ID | Description |
|----|-------------|
| OPS-01 | Binary plugins never compiled in CI. No build step for `plugins/core-scenes/` in any workflow |
| OPS-02 | Docker image copies plugin directories but contains no compiled binary for scene plugin. Binary plugin path is operationally dead in containers |

### Medium

| ID | Description |
|----|-------------|
| OPS-03 | GoReleaser config skips plugin binaries — no release artifact for binary plugins |
| OPS-04 | `task pr-prep` lacks proto freshness check (could be stale after manual proto edits) |
| OPS-05 | Subprocess inherits full environment including secrets (overlap SEC-06) |
| OPS-07 | PluginSubsystem missing HealthReporter — readiness probe blind to plugin failures |

### Low

| ID | Description |
|----|-------------|
| OPS-06 | Schema provisioner pool unconfigured (overlap PERF-4) |
| OPS-08 | `Kill()` without graceful shutdown for plugin subprocesses |
| OPS-09 | No `task plugin:build` for developer workflow |
| OPS-10 | Host.Load/Unload not instrumented in OTel middleware |
