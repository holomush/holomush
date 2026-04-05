# Phase 2a: Security Audit

**PR**: #192 -- Proto-first plugin architecture rework
**Auditor**: Claude Opus 4.6 (security audit agent)
**Date**: 2026-04-05
**Scope**: All new code in PR #192 as enumerated in `00-scope.md`

---

## Summary

11 findings: 2 High, 5 Medium, 4 Low. No Critical findings. The architecture is sound for first-party plugins but has gaps that become exploitable when third-party plugins are introduced.

The most significant risk is **SEC-01** (database credential sharing): binary plugins receive the server's full PostgreSQL credentials with only `search_path` scoping. This is a documentation-acknowledged limitation, but the blast radius is severe enough that it warrants explicit treatment before any third-party plugin support.

---

## Findings

### SEC-01 [High] Plugin schema isolation relies on `search_path`, not PostgreSQL roles

| Field | Value |
|-------|-------|
| Severity | High |
| CVSS 3.1 | 7.5 (AV:N/AC:L/PR:L/UI:N/S:U/C:H/I:H/A:N) -- network via plugin gRPC channel, low privilege (loaded plugin), high confidentiality/integrity impact |
| CWE | CWE-284 (Improper Access Control) |
| File | `internal/plugin/schema_provisioner.go:46-66` |
| Phase 1 Ref | F-01 |

**Description.** `SchemaProvisioner.ProvisionSchema` creates a `plugin_<name>` schema and returns the server's base connection string with `search_path=plugin_<name>` appended. The connection string contains the same username, password, host, and database as the server itself. No restricted PostgreSQL role is created.

**Attack scenario.** A binary plugin (or a compromised first-party plugin) executes:

```sql
SET search_path TO public;
SELECT * FROM players;          -- reads all player credentials
SELECT * FROM events;           -- reads the entire event store
DROP TABLE sessions;            -- destroys server session data
SELECT * FROM plugin_other_plugin.scenes;  -- reads another plugin's data
```

The `search_path` parameter is a session-level hint, not a security boundary. Any SQL statement can reference any schema explicitly (`public.tablename`) or reset `search_path`.

**Impact.** Complete read/write access to all server data including player authentication records, session tokens, event history, world state, and other plugin schemas. A malicious plugin can exfiltrate all data, corrupt any table, or deny service by dropping objects.

**Mitigating factors.** Currently only the first-party `core-scenes` plugin uses this path. The connection string is passed over the go-plugin gRPC channel (localhost only, mTLS-protected by go-plugin's internal handshake).

**Remediation.**

1. **Immediate (before third-party plugins):** Create a restricted PostgreSQL role per plugin with `USAGE` and `CREATE` grants limited to the plugin's schema. Revoke `USAGE` on `public` schema for plugin roles. Pass the restricted role's credentials in the connection string.
2. **Defense in depth:** Add `REVOKE ALL ON SCHEMA public FROM plugin_role` and `REVOKE ALL ON ALL TABLES IN SCHEMA public FROM plugin_role` as part of provisioning.
3. **Track:** Create a blocking issue for third-party plugin support gating on role-based isolation.

---

### SEC-02 [High] EndScene missing ownership authorization -- any caller can end any scene

| Field | Value |
|-------|-------|
| Severity | High |
| CVSS 3.1 | 6.5 (AV:N/AC:L/PR:L/UI:N/S:U/C:N/I:H/A:N) |
| CWE | CWE-862 (Missing Authorization) |
| File | `plugins/core-scenes/service.go:138-164` |
| Phase 1 Ref | CQ-16 |

**Description.** `EndScene` validates that `session_id` and `scene_id` are non-empty and that the scene is in `active` or `paused` state. It never checks whether `req.GetSessionId()` matches `scene.OwnerID` or whether the caller holds an appropriate role (owner, admin).

**Attack scenario.** Any authenticated character can call `EndScene` with any `scene_id` to prematurely terminate someone else's active roleplay scene. This is a griefing vector in a MUSH context where scenes are social content.

**Additional affected RPCs.** `InviteToScene` (line 217-239) does not verify the caller is the scene owner or a participant with invite permissions. Any authenticated user can inject participants into any scene.

**Remediation.**

```go
// In EndScene, after fetching the scene:
if scene.OwnerID != req.GetSessionId() {
    return nil, status.Errorf(codes.PermissionDenied, "only the scene owner can end a scene")
}

// In InviteToScene, similarly:
if scene.OwnerID != req.GetSessionId() {
    return nil, status.Errorf(codes.PermissionDenied, "only the scene owner can invite participants")
}
```

Consider adding a `role` check for participants with `owner` or `moderator` role as an alternative.

---

### SEC-03 [Medium] SQL identifier injection in schema provisioner DDL

| Field | Value |
|-------|-------|
| Severity | Medium |
| CVSS 3.1 | 4.2 (AV:N/AC:H/PR:H/UI:N/S:U/C:N/I:H/A:N) -- high privilege (must craft manifest), high complexity (must bypass regex) |
| CWE | CWE-89 (SQL Injection) |
| File | `internal/plugin/schema_provisioner.go:49` |
| Phase 1 Ref | CQ-08 |

**Description.** The DDL statement is constructed via `fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schemaName)`. The `schemaName` value derives from `pluginSchemaName()` which prepends `plugin_` and replaces hyphens with underscores. The upstream manifest validation regex `^[a-z](-?[a-z0-9])*$` constrains plugin names to lowercase alphanumeric with single hyphens.

**Risk assessment.** The regex validation is currently effective -- no SQL metacharacters (`;`, `'`, `"`, `--`, spaces) can pass `^[a-z](-?[a-z0-9])*$`. The risk is **defense in depth**: if the validation is bypassed (future code path, direct API call, manifest parsing bug), the unquoted identifier becomes a SQL injection vector.

**Attack scenario (hypothetical).** If a future code path calls `ProvisionSchema` with a name not validated by `ParseManifest` (e.g., an admin API), the name could contain SQL injection payload.

**Remediation.** Use `pgx.Identifier` for defensive quoting:

```go
import "github.com/jackc/pgx/v5"

identifier := pgx.Identifier{schemaName}
ddl := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", identifier.Sanitize())
```

This is a one-line change that adds defense in depth at zero cost.

---

### SEC-04 [Medium] Information disclosure via raw error leakage in SceneService fallback

| Field | Value |
|-------|-------|
| Severity | Medium |
| CVSS 3.1 | 4.3 (AV:N/AC:L/PR:L/UI:N/S:U/C:L/I:N/A:N) |
| CWE | CWE-209 (Generation of Error Message Containing Sensitive Information) |
| File | `plugins/core-scenes/service.go:370` |
| Phase 1 Ref | CQ-12 |

**Description.** The `mapStoreError` fallback path returns:

```go
return status.Errorf(codes.Internal, "%s failed: %v", operation, err)
```

When `err` is a pgx error (not wrapped by oops with a recognized code), the raw PostgreSQL error is serialized into the gRPC status message. This can expose:

- Database table and column names
- PostgreSQL version and configuration details
- Constraint names revealing schema structure
- Connection parameters in certain error paths

**Example leaked error.** `create_scene failed: ERROR: duplicate key value violates unique constraint "scenes_pkey" (SQLSTATE 23505)`

**Remediation.** Log the full error server-side and return a generic message:

```go
slog.Error("store operation failed",
    "operation", operation,
    "error", err)
return status.Errorf(codes.Internal, "%s failed", operation)
```

---

### SEC-05 [Medium] WorldService gRPC adapter leaks oops error details to plugin callers

| Field | Value |
|-------|-------|
| Severity | Medium |
| CVSS 3.1 | 4.3 (AV:N/AC:L/PR:L/UI:N/S:U/C:L/I:N/A:N) |
| CWE | CWE-209 (Generation of Error Message Containing Sensitive Information) |
| File | `internal/world/grpc_server.go:152-170` |

**Description.** `mapWorldError` uses `%v` formatting for all error paths:

```go
return status.Errorf(codes.Internal, "%v", err)
return status.Errorf(codes.NotFound, "%v", err)
```

The `%v` format on an `oops.OopsError` produces a string containing the full error chain with all `.With()` context -- including internal field names, database identifiers, and stack trace elements. This information is transmitted to the plugin process over gRPC.

For `codes.NotFound` and `codes.PermissionDenied`, leaking the full error is less concerning (the caller already knows the entity type). But for `codes.Internal`, the error can contain repository-level implementation details.

**Contrast.** The hostfunc layer (`hostfunc/errors.go`) correctly sanitizes errors via `SanitizeErrorForPlugin` with correlation IDs. The gRPC adapter bypasses this sanitization entirely.

**Remediation.** Return generic messages for `codes.Internal`:

```go
case strings.HasSuffix(code, "_NOT_FOUND"):
    return status.Errorf(codes.NotFound, "%s not found", extractSubject(code))
case strings.HasSuffix(code, "_ACCESS_DENIED"):
    return status.Errorf(codes.PermissionDenied, "access denied")
default:
    slog.Error("world service error", "code", code, "error", err)
    return status.Errorf(codes.Internal, "internal error")
```

---

### SEC-06 [Medium] Subprocess inherits full process environment including secrets

| Field | Value |
|-------|-------|
| Severity | Medium |
| CVSS 3.1 | 5.0 (AV:L/AC:L/PR:H/UI:N/S:C/C:L/I:L/A:N) -- local, high privilege (must be installed plugin), changed scope (reads host env) |
| CWE | CWE-214 (Invocation of Process Using Visible Sensitive Information) |
| File | `internal/plugin/goplugin/host.go:64-69` |

**Description.** The `DefaultClientFactory.NewClient` creates a subprocess via `exec.Command(execPath)` without restricting the process environment. The `hashiplug.ClientConfig` does not set `Cmd.Env`, so the child process inherits the parent's full environment, which typically includes:

- `DATABASE_URL` -- the full PostgreSQL connection string with credentials
- `HOLOMUSH_*` environment variables with server configuration
- `HOME`, `PATH`, and other OS-level variables
- Any secrets injected by the deployment environment (Vault tokens, cloud credentials)

A binary plugin can read `os.Environ()` to harvest all server secrets, even if it was only granted `storage: postgres` access to its own schema.

**Mitigating factors.** Binary plugins are currently first-party only. The go-plugin framework adds its own environment variables for the handshake. The subprocess is already a separate OS process (not sandboxed in any container or jail).

**Remediation.**

1. **Short term:** Construct a minimal `Cmd.Env` in `NewClient`:

```go
cmd := exec.Command(execPath)
cmd.Env = []string{
    "PATH=" + os.Getenv("PATH"),
    // go-plugin will add its own handshake env vars
}
```

2. **Medium term:** For third-party plugins, run subprocesses in a restricted sandbox (seccomp, AppArmor, or container).

---

### SEC-07 [Medium] gRPC proxy forwards internal service names in error messages

| Field | Value |
|-------|-------|
| Severity | Medium |
| CVSS 3.1 | 3.7 (AV:N/AC:L/PR:L/UI:N/S:U/C:L/I:N/A:N) |
| CWE | CWE-200 (Exposure of Sensitive Information to an Unauthorized Actor) |
| File | `internal/plugin/grpc_proxy.go:40-53` |

**Description.** The `GRPCServiceProxy.streamHandler` returns service names and health status in error messages:

```go
return status.Errorf(codes.Unimplemented, "unknown service %s", serviceName)
return status.Errorf(codes.Unavailable, "service %s has no connection", serviceName)
return status.Errorf(codes.Unavailable, "service %s is unhealthy", serviceName)
return status.Errorf(codes.Internal, "failed to create stream to %s: %v", serviceName, streamErr)
```

The last line also leaks the upstream connection error (`streamErr`), which can reveal internal network topology, connection details, or plugin process state.

**Attack scenario.** An attacker probes the gRPC proxy with various service names to enumerate registered services and their health status, building a map of the plugin architecture. The `streamErr` in the last message can leak information about the internal network or plugin process failures.

**Remediation.**

```go
// Line 63: Don't include streamErr in client-facing message
slog.Error("failed to create proxy stream", "service", serviceName, "error", streamErr)
return status.Errorf(codes.Internal, "service temporarily unavailable")
```

The `Unimplemented` and `Unavailable` messages are standard gRPC patterns and are acceptable, but the `streamErr` leak should be fixed.

---

### SEC-08 [Low] Path traversal validation has TOCTOU window for executable content

| Field | Value |
|-------|-------|
| Severity | Low |
| CVSS 3.1 | 2.0 (AV:L/AC:H/PR:H/UI:N/S:U/C:N/I:L/A:N) |
| CWE | CWE-367 (Time-of-Check Time-of-Use Race Condition) |
| File | `internal/plugin/goplugin/host.go:149-180` |

**Description.** The `Load` method performs thorough path validation:

1. `EvalSymlinks` on both directory and executable (line 152-163)
2. `filepath.Rel` containment check (line 165-167)
3. `os.Stat` on the resolved path (line 171)
4. Execute permission check (line 176)
5. Client creation using `realExec` (line 180)

The implementation is well-designed and explicitly addresses symlink-based escapes by using `EvalSymlinks` before the containment check, then using `realExec` (not the original `execPath`) for both `Stat` and client creation. This closes the standard TOCTOU gap.

**Residual risk.** There is a theoretical TOCTOU window between `EvalSymlinks`+`Stat` (line 152-176) and `exec.Command` (line 180 -> factory). If an attacker can replace the executable between these calls, the containment check is bypassed. This requires local write access to the plugin directory, which implies the attacker already has server-level access.

**Assessment.** The path traversal validation is well-implemented. The `#nosec G204` annotation on line 67 is justified given the validation chain. The residual TOCTOU risk is theoretical and requires pre-existing local access.

**Remediation.** No immediate action required. For defense in depth when third-party plugins are supported, consider:
- Opening the file descriptor during validation and passing the fd to exec (eliminates TOCTOU entirely)
- Verifying a cryptographic hash of the executable against the manifest

---

### SEC-09 [Low] `internal/idgen` import in binary plugin breaks Go import boundary

| Field | Value |
|-------|-------|
| Severity | Low |
| CVSS 3.1 | N/A (design issue, not directly exploitable) |
| CWE | CWE-1061 (Insufficient Encapsulation) |
| File | `plugins/core-scenes/service.go:15` |
| Phase 1 Ref | F-02 |

**Description.** The `core-scenes` plugin imports `github.com/holomush/holomush/internal/idgen`. In Go, `internal/` packages are only importable by code within the same module. Since `core-scenes` is compiled as part of the same module (it lives in the monorepo), this compiles. However, it violates the architectural boundary that binary plugins should only depend on `pkg/`.

**Security implication.** This is not directly exploitable, but it creates a precedent that could lead to other `internal/` imports in plugins, gradually eroding the security boundary. If a plugin imports `internal/store` or `internal/session`, it gains direct access to server internals.

**Remediation.** Move `idgen.New()` to `pkg/plugin/idgen` or have the plugin use `github.com/oklog/ulid/v2` directly. This is the same recommendation as F-02.

---

### SEC-10 [Low] InProcessConn uses insecure gRPC credentials

| Field | Value |
|-------|-------|
| Severity | Low |
| CVSS 3.1 | N/A (by design, not exploitable) |
| CWE | N/A |
| File | `internal/plugin/inprocess_conn.go:46` |

**Description.** `NewInProcessConn` creates a gRPC client with `insecure.NewCredentials()`. The `nosemgrep` annotation indicates this is intentional.

**Assessment.** This is correct. The `bufconn` listener operates entirely in-process over memory buffers -- there is no network socket to intercept. TLS would add overhead with zero security benefit. The annotation is appropriate and the implementation is secure.

**Remediation.** None required. The existing `nosemgrep` comment is sufficient documentation.

---

### SEC-11 [Low] Scene ListScenes has no upper bound on `limit` parameter

| Field | Value |
|-------|-------|
| Severity | Low |
| CVSS 3.1 | 3.1 (AV:N/AC:L/PR:L/UI:N/S:U/C:N/I:N/A:L) |
| CWE | CWE-770 (Allocation of Resources Without Limits or Throttling) |
| File | `plugins/core-scenes/service.go:113-115` |

**Description.** `ListScenes` sets a default `limit` of 50 when the request value is <= 0, but does not cap the maximum. A client can pass `limit=999999999` to request an unbounded result set.

```go
limit := int(req.GetLimit())
if limit <= 0 {
    limit = 50
}
```

**Attack scenario.** A client sends `ListScenesRequest{limit: 2147483647}` to force PostgreSQL to scan and return an extremely large result set, causing memory pressure on both the database and the plugin process.

**Remediation.**

```go
const maxListLimit = 200

limit := int(req.GetLimit())
if limit <= 0 {
    limit = 50
}
if limit > maxListLimit {
    limit = maxListLimit
}
```

---

## Items Investigated -- No Finding

### Plugin sandbox escape via gRPC proxy

**Investigation.** Examined whether a client could craft gRPC requests to bypass the service registry health check by calling registered services directly. The proxy (`grpc_proxy.go`) is installed as `grpc.UnknownServiceHandler`, meaning it only handles methods NOT registered on the main gRPC server. Services registered directly (CoreService, ContentService) are handled by their own handlers. The proxy cannot be used to bypass those handlers' auth. The proxy checks health before forwarding (line 52-54), which is correct.

**Verdict.** No finding. The proxy architecture is sound.

### SQL injection in SceneStore queries

**Investigation.** All SceneStore queries use parameterized queries (`$1`, `$2`, etc.) with pgx. The `ListScenes` dynamic query builder (lines 210-237) appends `$N` placeholders and collects values in an `args` slice. No string interpolation of user input into SQL.

**Verdict.** No finding. Query parameterization is consistently applied.

### gRPC proxy health check bypass

**Investigation.** Examined whether a client can route requests to unhealthy services. The health check on line 52 uses `svc.Health.Healthy()` but only when `svc.Health != nil`. Services registered without a `HealthReporter` (currently all of them) skip the health check.

**Verdict.** Low concern. The health check is opt-in. Services without health reporters are assumed healthy. This is appropriate for the current architecture where all services are either in-process (WorldService) or managed by go-plugin (which has its own liveness detection via process monitoring). No finding elevated.

### Lua plugin sandbox escape

**Investigation.** Lua plugins run in gopher-lua which creates a new VM per event delivery. The hostfunc layer uses narrow interfaces (`SessionAccess`, `WorldQueryAccess`) and sanitizes errors via `SanitizeErrorForPlugin`. Lua cannot import Go packages or access the filesystem.

**Verdict.** No finding. The Lua sandbox is effective for the current threat model.

---

## Priority Remediation Order

| Priority | Finding | Effort | Risk |
|----------|---------|--------|------|
| 1 | SEC-02 | Low (add 2 ownership checks) | High -- active griefing vector |
| 2 | SEC-04 | Low (remove `%v` from fallback) | Medium -- leaks DB internals |
| 3 | SEC-05 | Low (sanitize error messages) | Medium -- leaks domain internals |
| 4 | SEC-07 | Low (remove streamErr from message) | Medium -- leaks network info |
| 5 | SEC-03 | Low (one-line pgx.Identifier change) | Medium -- defense in depth |
| 6 | SEC-11 | Low (add max limit cap) | Low -- DoS vector |
| 7 | SEC-06 | Medium (construct minimal Env) | Medium -- env secret leakage |
| 8 | SEC-01 | High (PostgreSQL role provisioning) | High -- but first-party only today |
| 9 | SEC-09 | Low (move to pkg/) | Low -- boundary hygiene |

SEC-01 is architecturally the most significant but ranks lower in immediate priority because only first-party code uses this path today. SEC-02 is the most immediately exploitable finding.

---

## Positive Security Observations

1. **Path traversal defense** (SEC-08) is well-implemented with `EvalSymlinks` before containment check and using the resolved path for execution.
2. **Error sanitization** in the hostfunc layer (`SanitizeErrorForPlugin`) is exemplary -- correlation IDs, generic messages, server-side logging.
3. **Parameterized queries** are consistently used across all SQL in `SceneStore`.
4. **Manifest validation** regex is strict and effective for the current threat model.
5. **go-plugin handshake** provides mutual authentication between host and plugin processes.
6. **DAG dependency resolution** correctly rejects circular dependencies and unsatisfied requirements.
7. **Service registry** prevents duplicate service registration, avoiding confused-deputy issues.
8. **WorldService subject_id** pattern enforces access control at the gRPC boundary, not just at the internal service layer.
