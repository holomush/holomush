# Phase 1: Code Quality & Architecture Review

## Code Quality Findings (18 total: 0 Critical, 3 High, 9 Medium, 6 Low)

### High

| ID | File | Description |
|----|------|-------------|
| CQ-01 | `plugins/core-scenes/store.go:88-134` | Duplicated migration runner from SDK (~50 lines). Fix by adding `RunMigrationsFS(ctx, pool, fs.FS)` to `pkg/plugin/storage` |
| CQ-02 | `plugins/core-scenes/store.go:338-343` | Custom recursive `itoa` instead of `strconv.Itoa` |
| CQ-03 | `internal/plugin/manager.go:289-313` | No rollback of partial service registrations; plugin marked loaded with some Provides unsatisfied. Needs documentation or rollback logic |

### Medium

| ID | File | Description |
|----|------|-------------|
| CQ-04 | `hostfunc/cap_*.go` | Repetitive Lua error-path boilerplate (~17 occurrences). Extract `capError` helper |
| CQ-05 | `plugins/core-scenes/service.go` | Magic string literals for state/visibility/role. Define constants |
| CQ-06 | `internal/plugin/grpc_proxy.go:82-112` | Upstream errors silently dropped in `proxyStreams`. Document or log at debug |
| CQ-07 | `internal/plugin/inprocess_conn.go` | gRPC server not stopped on Close. Store `*grpc.Server` reference |
| CQ-08 | `internal/plugin/schema_provisioner.go:47-55` | SQL identifier not defensively quoted. Use `pgx.Identifier.Sanitize()` |
| CQ-09 | `internal/plugin/setup/subsystem.go:111-213` | 100-line Start method. Acceptable with current comments; extract if more steps added |
| CQ-10 | `internal/plugin/manager.go:356-358` | Maps cleared before hosts closed in `Close()`. Swap order |
| CQ-11 | `hostfunc/adapter.go:74-164` | Repetitive nil-check boilerplate. Extract generic helper |
| CQ-12 | `plugins/core-scenes/service.go:370-371` | Raw error leaked in gRPC status fallback. Log server-side, return generic message |

### Low

| ID | File | Description |
|----|------|-------------|
| CQ-13 | `hostfunc/cap_session.go` | Missing compile-time interface check (`var _ Capability = ...`) |
| CQ-14 | `plugins/core-scenes/service.go` | Linear scan for participant in `CastPublishVote` |
| CQ-15 | `plugins/core-scenes/service.go` | Hardcoded "open" visibility string |
| CQ-16 | `plugins/core-scenes/service.go` | `EndScene` missing ownership authorization check |
| CQ-17 | `hostfunc/adapter.go` | Deprecated `WorldService` interface in new code without tracking issue |
| CQ-18 | `hostfunc/capability.go` | CapabilityRegistry not thread-safe (currently safe by startup ordering) |

## Architecture Findings (11 total: 0 Critical, 1 High, 3 Medium, 7 Low)

### High

| ID | Category | Description |
|----|----------|-------------|
| F-02 | Boundary | `plugins/core-scenes/service.go:15` imports `internal/idgen` -- binary plugins MUST only import `pkg/`. Move to `pkg/plugin/idgen` or use `oklog/ulid/v2` directly |

### Medium

| ID | Category | Description |
|----|----------|-------------|
| F-01 | Security | Schema provisioner creates schemas but not restricted PostgreSQL roles. Plugins get server's full DB credentials. Acceptable for first-party `core-scenes`, must fix before third-party plugins |
| F-05 | Completeness | `ServiceConfig.required_services` map defined in proto but never populated by host. Scene plugin declares requires WorldService but never receives connection to it |
| F-07 | Duplication | Migration runner duplicated between `pkg/plugin/storage` and `plugins/core-scenes/store.go`. Same as CQ-01 |

### Low

| ID | Category | Description |
|----|----------|-------------|
| F-03 | Completeness | WorldService is read-only; mutations still use old hostfunc path. Track as known divergence |
| F-04 | Naming | `session_id` in scene proto should be `character_id` per terminology guide |
| F-06 | Resilience | `proxyStreams` goroutine errors silently discarded. Same as CQ-06 |
| F-08 | Diagnostics | Missing warning when capability module not registered for a declared requires service |
| F-09 | Operations | DAG resolution failure falls back silently to priority sort. Consider strict mode |
| F-10 | Lifecycle | `InProcessConn` cannot graceful-stop wrapped gRPC server. Same as CQ-07 |
| F-11 | Naming | No collision check on generated schema names (unlikely with `plugin_` prefix) |

## Architecture Verdict

**Sound.** The design decisions D1-D9 from the spec are implemented faithfully. The service registry, DAG resolution, InProcessConn, and GRPCServiceProxy are clean and compose well. Total new infrastructure is ~584 lines across 6 components -- lean for what it delivers. No unnecessary abstractions found. The capability module decomposition is a correct intermediate step toward proto-generated Lua bindings.

## Critical Issues for Phase 2 Context

1. **F-01 (Schema isolation incomplete):** No restricted PostgreSQL roles means plugins get full DB access. Security review should assess blast radius.
2. **CQ-08 (SQL identifier quoting):** Schema provisioner uses `fmt.Sprintf` for DDL. Security review should assess SQL injection risk given manifest validation constraints.
3. **CQ-12 (Error leakage):** Raw pgx errors in gRPC status responses. Security review should assess information disclosure.
4. **F-02 (internal import):** Plugin boundary violation affects the security model's plugin isolation assumptions.
