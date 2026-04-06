# Phase 2: Security & Performance Review

## Security Findings (11 total: 0 Critical, 2 High, 5 Medium, 4 Low)

### High

| ID | CVSS | CWE | File | Description |
|----|------|-----|------|-------------|
| SEC-01 | 7.5 | CWE-284 | `schema_provisioner.go:46-66` | Schema isolation uses `search_path` only, not PostgreSQL roles. Plugins get server's full DB credentials. Any plugin can `SET search_path TO public` and access all data. Acceptable for first-party only; must block third-party plugins |
| SEC-02 | 6.5 | CWE-862 | `plugins/core-scenes/service.go:138-164,217-239` | `EndScene` and `InviteToScene` perform no ownership checks. Any authenticated caller can end any scene or inject participants. 2-line fix each |

### Medium

| ID | CVSS | CWE | File | Description |
|----|------|-----|------|-------------|
| SEC-03 | 4.2 | CWE-89 | `schema_provisioner.go:49` | SQL identifier not defensively quoted. Manifest regex constrains input but defense-in-depth via `pgx.Identifier.Sanitize()` is a 1-line fix |
| SEC-04 | 4.3 | CWE-209 | `plugins/core-scenes/service.go:370` | Raw pgx errors leaked in gRPC status fallback. Log server-side, return generic message |
| SEC-05 | 4.3 | CWE-209 | `internal/world/grpc_server.go:152-170` | oops error details (with context, stack traces) leaked to plugin callers via `%v` formatting |
| SEC-06 | 5.0 | CWE-214 | `goplugin/host.go:64-69` | Subprocess inherits full process environment including DATABASE_URL, cloud credentials. Construct minimal Cmd.Env |
| SEC-07 | 3.7 | CWE-200 | `grpc_proxy.go:40-53` | gRPC proxy leaks internal service names and upstream connection errors in status messages |

### Low

| ID | Description |
|----|-------------|
| SEC-08 | Path traversal validation is thorough; residual TOCTOU window requires pre-existing local access |
| SEC-09 | `internal/idgen` import in binary plugin (same as F-02) |
| SEC-10 | InProcessConn insecure credentials — correct by design (in-memory bufconn) |
| SEC-11 | ListScenes has no upper bound on `limit` parameter. Cap at 200 |

### Positive

- Parameterized queries consistently used across all SceneStore SQL
- Path traversal defense with EvalSymlinks is thorough
- Error sanitization in hostfunc layer is exemplary
- WorldService subject_id enforces ABAC at gRPC boundary
- go-plugin mutual handshake authentication

## Performance Findings (12 total: 0 Critical, 0 High, 6 Medium, 6 Low)

### Medium

| ID | Impact | Description |
|----|--------|-------------|
| PERF-3 | ~100-200us/delivery | Lua VM re-parses source on every delivery. Cache bytecode via CompileString |
| PERF-4 | 48+ connections | 3 independent pgxpool.Pool instances. Close provisioner after startup, configure pool sizes |
| PERF-7b | N+1 pattern | CastPublishVote fetches all participants to update one. Add GetParticipant |
| PERF-7d | Crash inconsistency | CreateScene + CastPublishVote multi-write without transaction |
| PERF-8e | 10KB+/delivery | Source string copied per event delivery. Fix with bytecode caching |
| PERF-1 | ~5-10us/call | InProcessConn proto marshal overhead for binary plugins. Acceptable for current use |

### Low

| ID | Description |
|----|-------------|
| PERF-2 | Goroutine per proxied call — clean lifecycle, no leaks |
| PERF-5 | ServiceRegistry RLock + heap alloc per Resolve — negligible |
| PERF-6 | DAG resolution O(N+M) at startup only — trivial |
| PERF-7a | 2-3 queries per scene RPC — acceptable |
| PERF-7c | Missing composite index on (visibility, created_at DESC) |
| PERF-8g | OTel attribute allocations in hot path — GC pressure at high throughput |

## Critical Issues for Phase 3 Context

1. **SEC-02 (missing auth checks):** Testing review should verify no tests assert ownership enforcement — they'll need to be added
2. **PERF-3/8e (Lua bytecode caching):** Test review should check if per-delivery overhead is benchmarked
3. **PERF-7d (missing transactions):** Test review should check if crash-consistency scenarios are covered
4. **SEC-04/SEC-05 (error leakage):** Documentation review should check if error handling guidance exists for plugin authors
