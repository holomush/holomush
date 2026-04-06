# Phase 3: Testing & Documentation Review

## Test Coverage Findings (16 total: 0 Critical, 3 High, 7 Medium, 4 Low)

### High

| ID | Description |
|----|-------------|
| T-03 | No tests for ownership enforcement on EndScene/InviteToScene — directly maps to SEC-02 |
| T-08 | No E2E test for binary plugin path. The subprocess→Init→gRPC proxy chain tested only via mocks |
| T-09 | Five Lua plugin rewrites (~18KB+ new code) have zero dedicated tests |

### Medium

| ID | Description |
|----|-------------|
| T-01 | GRPCServiceProxy — only helper functions tested, not actual forwarding/health check behavior |
| T-02 | SchemaProvisioner — no integration test with real PostgreSQL |
| T-04 | mapStoreError fallback leaks raw pgx errors (SEC-04) — no test verifying sanitization |
| T-05 | Scene store has no integration test with real database |
| T-11 | Plugin load failure doesn't verify service registration rollback (CQ-03) |
| T-12 | gRPC proxy unhealthy service path untested |
| T-15 | mapWorldError leaks oops context (SEC-05) — Internal fallback untested |

### Low

| ID | Description |
|----|-------------|
| T-06 | InProcessConn Close behavior not fully tested (server stop) |
| T-07 | CapabilityRegistry InjectRequired skips unknown services silently — no warning test |
| T-13 | No benchmarks across the plugin layer (relevant to PERF-3/PERF-8e) |
| T-14 | ListScenes limit parameter has no upper-bound test |

### Positive

- Hostfunc capability module tests are exemplary — narrow mocks, both paths, table verification
- DAG dependency resolution tests are thorough (circular, diamond, unsatisfied)
- goplugin.Host tests cover path traversal, symlink escape, permissions, context cancellation
- Error sanitization tests in hostfunc/errors_test.go are gold standard

## Documentation Findings (18 total: 0 Critical, 4 High, 8 Medium, 6 Low)

### High

| ID | Description |
|----|-------------|
| DOC-05 | Plugin guide (`site/docs/extending/plugin-guide.md`) predates this PR. Does not document requires/provides/storage, ServiceProvider, ServeWithServices, or storage SDK |
| DOC-07 | No documentation for building a binary plugin. Plugin authors cannot build a service-providing plugin from current docs |
| DOC-09 | Design spec claims PostgreSQL role-based isolation that was not implemented (aligns with SEC-01). Spec should note search_path-only |
| DOC-16 | Capability host functions (alias.*, session.*, property.*, world.*) undocumented for Lua plugin authors |

### Medium

| ID | Description |
|----|-------------|
| DOC-01 | scene.proto lacks field-level comments that world.proto has |
| DOC-03 | Design spec describes aspirational features as implemented (code gen, 5/7 admin commands) |
| DOC-06 | CLAUDE.md not updated to reflect new architecture (still references ServiceProxy, LocalPluginHost) |
| DOC-08 | No operator documentation for plugin management, schema provisioning, admin commands |
| DOC-10 | Manifest schema for requires/provides/storage not documented in extending/ guide |
| DOC-11 | Storage SDK (Connect, RunMigrations) has no usage guide |
| DOC-12 | Plugin lifecycle (discover→resolve→load→init→register) not documented |
| DOC-14 | Connection pool sizing implications not documented for operators |

### Low

| ID | Description |
|----|-------------|
| DOC-02 | 3 exported types have sparse godoc (GRPCServer, SchemaProvisioner.Close, TypeServerInternal) |
| DOC-04 | ResolveDependencyOrder and SanitizeErrorForPlugin are exemplary godoc |
| DOC-13 | Plugin.yaml examples in specs but no reference page |
| DOC-15 | session_id vs character_id naming inconsistency not called out |
| DOC-17 | No changelog entry for the architecture rework |
| DOC-18 | Proto files use different comment styles (block vs inline) |
