# Comprehensive Code Review Report — PR #192

## Review Target

PR #192: Proto-first plugin architecture rework. 49 commits across 4 phases replacing the monolithic compiled-in plugin system with proto service contracts, a service registry, DAG dependency resolution, binary plugin support via hashicorp/go-plugin, and capability-based Lua hostfunc injection.

## Executive Summary

The architecture is **sound and faithful to the design spec**. The service registry, DAG resolver, gRPC proxy, and InProcessConn are clean, lean implementations (~584 lines total) that compose well. The capability module decomposition is a correct intermediate step toward proto-generated Lua bindings. The scene binary plugin proves the end-to-end path.

However, the **binary plugin path is operationally incomplete**: it is never compiled in CI, not included in Docker images, and has no E2E test. The scene plugin has **missing authorization checks** (any user can end any scene) and **error messages leak database internals**. Documentation for plugin authors is absent. These gaps must be addressed before or immediately after merge.

---

## Findings by Priority

### P0 — Must Fix Before Merge (6 findings)

| ID | Category | File | Description | Effort |
|----|----------|------|-------------|--------|
| SEC-02 | Security | `plugins/core-scenes/service.go:138,217` | EndScene + InviteToScene missing ownership checks — any caller can end any scene or inject participants | Low (2 lines each) |
| F-02 | Architecture | `plugins/core-scenes/service.go:15` | Binary plugin imports `internal/idgen` — violates plugin isolation boundary. Use `oklog/ulid/v2` directly or move to `pkg/` | Low |
| CQ-01 | Quality | `plugins/core-scenes/store.go:88-134` | Duplicated migration runner (~50 lines). Add `RunMigrationsFS(ctx, pool, fs.FS)` to SDK | Low |
| CQ-02 | Quality | `plugins/core-scenes/store.go:338-343` | Custom recursive `itoa` instead of `strconv.Itoa` | Trivial |
| SEC-04 | Security | `plugins/core-scenes/service.go:370` | Raw pgx errors leaked in gRPC status. Log server-side, return generic message | Low |
| SEC-05 | Security | `internal/world/grpc_server.go:152-170` | oops error details leaked to plugin callers via `%v`. Return sanitized messages | Low |

### P1 — Fix Before Next Release (12 findings)

| ID | Category | Description | Effort |
|----|----------|-------------|--------|
| OPS-01 | DevOps | Binary plugins never compiled in CI — add build step | Medium |
| OPS-02 | DevOps | Docker image has no compiled binary plugin — add multi-stage build | Medium |
| T-08 | Testing | No E2E test for binary plugin subprocess→Init→gRPC proxy path | Medium |
| T-09 | Testing | Five Lua plugin rewrites (~18KB) have zero tests | Medium |
| CQ-05 | Quality | Magic string literals for scene state/visibility/role — define constants | Low |
| GP-02 | Go | `err == pgx.ErrNoRows` should be `errors.Is` (misses wrapped errors) | Trivial |
| SEC-03 | Security | Schema provisioner DDL — use `pgx.Identifier.Sanitize()` for defense-in-depth | Trivial |
| SEC-06 | Security | Subprocess inherits full environment. Construct minimal `Cmd.Env` | Low |
| SEC-07 | Security | gRPC proxy leaks internal service names + upstream errors in status messages | Low |
| CQ-07 | Quality | InProcessConn doesn't stop gRPC server on Close. Store `*grpc.Server` reference | Low |
| CQ-10 | Quality | Manager.Close clears maps before closing hosts — swap order | Low |
| DOC-06 | Docs | CLAUDE.md not updated — still references ServiceProxy, LocalPluginHost | Low |

### P2 — Plan for Next Sprint (16 findings)

| ID | Category | Description | Effort |
|----|----------|-------------|--------|
| SEC-01 | Security | Schema isolation uses search_path only, not PostgreSQL roles. Must fix before third-party plugins | High |
| DOC-05/07 | Docs | Plugin author guide doesn't document requires/provides/storage, ServiceProvider, SDK | Medium |
| DOC-09 | Docs | Design spec claims role-based isolation that isn't implemented — update spec | Low |
| T-03 | Testing | Add ownership enforcement tests for EndScene/InviteToScene (after SEC-02 fix) | Low |
| T-01 | Testing | GRPCServiceProxy forwarding + health check behavior untested | Medium |
| T-02 | Testing | SchemaProvisioner integration test with real PostgreSQL | Medium |
| T-05 | Testing | Scene store integration test with real database | Medium |
| PERF-3 | Perf | Lua VM re-parses source per delivery. Cache bytecode via CompileString | Medium |
| PERF-4 | Perf | 3 independent pgxpool.Pool instances (48+ connections). Close provisioner after startup | Low |
| PERF-7b | Perf | CastPublishVote fetches all participants to update one. Add GetParticipant | Low |
| PERF-7d | Perf | CreateScene + CastPublishVote multi-write without transaction | Low |
| CQ-03 | Quality | loadPlugin doesn't roll back partial service registrations — document or fix | Low |
| CQ-04 | Quality | Repetitive Lua error-path boilerplate across 17 functions. Extract capError helper | Low |
| F-04 | API | session_id in scene proto should be character_id per terminology guide | Low |
| F-05 | Arch | ServiceConfig.required_services map defined but never populated by host | Medium |
| OPS-07 | DevOps | PluginSubsystem missing HealthReporter — readiness probe blind to plugin failures | Medium |

### P3 — Track in Backlog (20+ findings)

Low-severity items across all categories including: compile-time interface checks, thread safety documentation, deprecated interface cleanup, sort.Slice modernization, composite index for scene queries, OTel attribute caching, proto comment consistency, goroutine cancellation paths, and more. See individual phase reports for full details.

---

## Findings by Category

| Category | Total | Critical | High | Medium | Low |
|----------|-------|----------|------|--------|-----|
| Code Quality | 18 | 0 | 3 | 9 | 6 |
| Architecture | 11 | 0 | 1 | 3 | 7 |
| Security | 11 | 0 | 2 | 5 | 4 |
| Performance | 12 | 0 | 0 | 6 | 6 |
| Testing | 16 | 0 | 3 | 7 | 6 |
| Documentation | 18 | 0 | 4 | 8 | 6 |
| Go Practices | 12 | 0 | 0 | 5 | 7 |
| CI/CD & DevOps | 10 | 0 | 2 | 4 | 4 |
| **Total** | **~75** | **0** | **15** | **47** | **~46** |

Note: Several findings overlap across categories (e.g., CQ-07/GP-04/F-10 all address InProcessConn shutdown). Deduplicated unique findings: ~55.

---

## Recommended Action Plan

### Before merge (1-2 hours)

1. **Fix SEC-02**: Add ownership checks to EndScene + InviteToScene (4 lines)
2. **Fix F-02**: Replace `internal/idgen` import with `oklog/ulid/v2` in scene plugin
3. **Fix CQ-01/CQ-02**: Add `RunMigrationsFS` to SDK, replace custom itoa with strconv.Itoa
4. **Fix SEC-04/SEC-05**: Sanitize error messages in SceneService and WorldServiceServer
5. **Fix CQ-05**: Define string constants for scene states/roles/visibility
6. **Fix GP-02**: Use `errors.Is(err, pgx.ErrNoRows)` instead of `==`

### Immediately after merge (this sprint)

7. **OPS-01/02**: Add binary plugin compilation to CI and Docker build
8. **T-08**: Write E2E test for binary plugin path
9. **DOC-06**: Update CLAUDE.md for new architecture
10. **SEC-03**: Add `pgx.Identifier.Sanitize()` to schema provisioner
11. **SEC-06/07**: Restrict subprocess environment, sanitize proxy error messages

### Next sprint

12. **T-09**: Add tests for Lua plugin rewrites
13. **DOC-05/07**: Write binary plugin authoring guide
14. **PERF-3**: Cache Lua bytecode for per-delivery performance
15. **SEC-01**: Implement PostgreSQL role-based schema isolation
16. **Integration tests**: SchemaProvisioner + SceneStore with real DB

---

## Review Metadata

- Review date: 2026-04-05
- Phases completed: 1 (Quality + Architecture), 2 (Security + Performance), 3 (Testing + Documentation), 4 (Best Practices + DevOps), 5 (Consolidated Report)
- Flags applied: none (standard review)
- Reviewer: Claude Opus 4.6 (8 specialized agents across 4 parallel phases)
- Total unique findings: ~55 (0 Critical, 15 High, ~25 Medium, ~15 Low)
