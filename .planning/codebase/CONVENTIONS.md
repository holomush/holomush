# Coding Conventions

**Analysis Date:** 2026-07-08

## Naming Patterns

**Files:**

- Snake_case, `foo.go` implementation paired with `foo_test.go` — e.g. `internal/core/character.go` / `internal/core/character_test.go`
- Ginkgo integration specs live in `test/integration/<domain>/` with a `*_suite_test.go` bootstrap file (e.g. `test/integration/integration_suite_test.go`, `test/integration/settings/settings_suite_test.go`)
- Migrations: `NNNNNN_snake_case_description.up.sql` / `.down.sql` in `internal/store/migrations/`

**Functions/Types:** standard Go — exported `PascalCase`, unexported `camelCase`. Test function names follow ACE (see TESTING.md).

**Domain terminology (enforced, `.claude/rules/terminology.md`):** use `location` never `room`; `character` never `player`/`user`/`avatar`; `player` never `user`/`account`; `session` never `connection`; `connection` never `socket`/`client`; `presence` never `who's here`. Applies to code, comments, types, events, variable names across `.go`, `.md`, `.proto`, `.lua`, `.svelte`, `.ts`.

## Code Style

**Linting:** `golangci-lint` v2 config at `.golangci.yaml`. Enabled linters: `errcheck`, `govet`, `staticcheck`, `nilerr` (bugs); `revive`, `misspell` (style); `prealloc`, `unconvert` (performance); `gosec` (security); `errorlint`, `wrapcheck` (error handling); `unparam`, `gocritic`, `nolintlint`, `depguard` (maintenance); `sloglint` (structured-logging discipline); `forbidigo` (custom bans, e.g. `time.Sleep` inside `internal/eventbus/`). Plus repo-authored custom analyzers (`gorules/`): `codeckeybytesallowlist`, `cursorpackageinternal`, `dekmaterialnofmtformatting`, `dekmaterialnogob`, `dekmaterialnojson`, `dekmaterialnolog`, `dekmaterialnoproto`, `dekmaterialnoslog`, `noremoteclockcompare`, `sceneopseventsappendonly`, `ulidmakeforbidden`.

**`depguard` rule (`.golangci.yaml`):** `no-test-only-constructs-in-production` denies `internal/eventbus/eventbustest` import in any file that is not `*_test.go` and not under `internal/testsupport/**` / `internal/cluster/clustertest/**` / `test/testutil/**`.

**Test-file exclusions:** `_test.go` files are exempted from `gocritic`, `wrapcheck`, `errcheck` (blank-identifier), `ulidmakeforbidden`, `cursorpackageinternal`, and a `gosec` G101 (test password-hash literals aren't real credentials).

**No path-level plugin exclusions** — Go binary plugins (`plugins/core-scenes`, `plugins/test-abac-widget`) are linted normally.

**Lint suppression:** `//nolint:<rule>` MUST be line-scoped with an explanatory comment — never widen `.golangci.yaml`. Repo precedent: `internal/web/handler.go:381,418,460,484` use `//nolint:wrapcheck // gRPC status errors pass through as-is` (27+ such directives exist). CLAUDE.md: "MUST NOT disable lint/format rules without explicit user confirmation."

**Formatting:** `task fmt` applies formatting AND SPDX license headers (via `license-eye`) in one pass — mutates files, so its output must be committed.

## License Headers

Every `.go`, `.sh`, `.proto` file MUST carry an SPDX header:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
```

YAML configs SHOULD carry one where appropriate. NEVER add to generated files (e.g. `*.pb.go`). Applied by `task fmt`, verified by `task license:check` and CI. Directories checked: `api/`, `cmd/`, `internal/`, `pkg/`, `plugins/`, `scripts/`.

## Error Handling

Structured errors via `samber/oops`:

```go
return oops.With("scene_id", id).Wrap(err)
oops.Errorf("invalid state: %s", state)
oops.Code("STREAM_ACCESS_DENIED").Wrap(err)
```

Log with `errutil.LogError` / `errutil.LogErrorContext` (never bare `slog`/`fmt` around an oops error). Test with `errutil.AssertErrorCode` / `errutil.AssertErrorContext` — but for **wire-opacity** assertions (gRPC boundary), use the top-level code directly: `oops.AsOops(err).Code()`, NOT the chain-walking `errutil.AssertErrorCode`, because chain-walking silently passes on double-wrapped errors (`.claude/rules/grpc-errors.md`).

**Method-value gotcha:** always call accessor methods with `()` — e.g. `decision.Reason()` not `decision.Reason`. Omitting parens creates a Go method value that compiles silently when passed into an `...any` param (`oops.With`, `slog.*`), silently breaking the log/error payload.

### gRPC error boundary rules (`.claude/rules/grpc-errors.md`)

- **Never leak inner errors past trust boundaries.** Do NOT use `status.Errorf(codes.Internal, "...: %v", err)` on an internal error — the `%v` substitution reaches the client on the wire. Log internally with `errutil.LogErrorContext(ctx, msg, err, ...)`, return a static string: `status.Errorf(codes.Internal, "internal error")` (allowlisted by `wrapcheck`, no nolint needed).
- **Translate `status` ↔ `oops` at exactly ONE layer** — the outermost call site that crosses the gRPC boundary. Double-translation breaks `status.FromError` chain-walking.
- Examples in `internal/web/handler.go:381,418,460,484`.

## Structured Logging (`.claude/rules/logging.md`)

**MUST use context-carrying variants** whenever a `context.Context` is in scope: `slog.InfoContext(ctx, ...)`, `WarnContext`, `ErrorContext`, `DebugContext`, `errutil.LogErrorContext(ctx, msg, err, ...)`. **MUST NOT** use bare `slog.Info(...)` / `logger.Warn(...)` / `errutil.LogError(...)` when a ctx is reachable — they orphan the log line from `trace_id`/`span_id` correlation.

```go
// WRONG
slog.Info("handling request", "kind", req.Kind)

// RIGHT
slog.InfoContext(ctx, "handling request", "kind", req.Kind)
```

**MAY use bare variants** only when truly no ctx exists and none can be plumbed (init/`main`, bare goroutines, pure helpers with no caller context).

Enforced mechanically by `sloglint` (`.golangci.yaml`) with:

- `context: scope` — bare call flagged only when a ctx is in scope
- `no-mixed-args` — no mixing `slog.Attr` with loose k/v pairs
- `static-msg` — message MUST be a string literal
- `msg-style: lowercased`
- `key-naming-case: snake`
- `forbidden-keys` — `time`/`level`/`msg`/`source` banned (collide with slog reserved fields)

Logger construction: `internal/logging/handler.go` (`Setup`/`SetDefault`), wraps base JSON/text handler with a `traceHandler` injecting `service`, `version`, `trace_id`, `span_id`.

## Randomness and ID Generation

- **Always `crypto/rand`, never `math/rand`.** For random slice picks, use a `crypto/rand` + `math/big` helper (`internal/naming.cryptoIntN(n)` is canonical).
- **Two ULID generators, not interchangeable:**

| Use case | Generator | Why |
|---|---|---|
| Event IDs (`core.Event.ID`), session IDs | `core.NewULID()` | Identity/dedup key (`Nats-Msg-Id` for JetStream dedup); ordering is JetStream's per-stream `uint64` seq, NOT ULID lex order |
| Entity primary keys (players, locations, characters, exits, objects, policies) | `idgen.New()` | Identity only, fresh `crypto/rand` entropy per call |

- `core.Event{}` struct literals MUST NOT be built directly — use `core.NewEvent()`, which stamps the monotonic ULID via `core.NewULID()`. Never supply `Event.ID` manually (e.g. from `idgen.New()`, which is for entity keys).
- `ulidmakeforbidden` custom analyzer bans `ulid.Make()` in production code (tests are exempted, since fixtures legitimately use it).

## Event System Conventions (`.claude/rules/event-conventions.md`)

- Subjects are NATS dot-delimited: `events.<game_id>.<domain>.<entity-id>[.<facet>...]`. Producers emit domain-relative references (`location.<id>`, `character.<id>`, `scene.<id>.ic`); `eventbus.Qualify` prepends `events.<game_id>.` at emit/read boundaries.
- Colon-style subjects are eradicated (`internal/eventbus/subjectxlate/` is deleted) — the only surviving colon usage is ABAC policy DSL type-prefixes (`character:<id>`, `scene:<id>`), which MUST NOT be changed to dot-style.
- Plugin-owned event types/verbs belong in the plugin package, never `internal/core/`.
- Wire type / `verbs[].type` / stored type MUST be plugin-qualified `<plugin>:<verb>`. Registered-emit set (`RegisterEmitTypes`) and `crypto.emits[].event_type` stay bare `<verb>` (INV-PLUGIN-32/40).
- Sensitive payloads MUST be declared in `crypto.emits` in `plugin.yaml`; gated by the `crypto-reviewer` agent on any change.

## Invariant Registry (`.claude/rules/invariants.md`)

Single canonical registry of durable system-behavior guarantees: `docs/architecture/invariants.yaml` (source of truth, hand-edited) → `docs/architecture/invariants.md` (generated by `go run ./cmd/inv-render`, never hand-edit inside `<!-- BEGIN GENERATED -->` regions). CI guard: `test/meta/invariant_registry_test.go`.

- Id format: `INV-<SCOPE>-N` (scopes: CRYPTO, SCENE, PLUGIN, EVENTBUS, CLUSTER, ACCESS, SESSION, STORE, TELEMETRY, PRIVACY, PRESENCE, COMMAND).
- A test proves an invariant via `// Verifies: INV-<SCOPE>-N` immediately above the asserting test/block — this flips `binding: pending` → `bound` in the YAML (plus `asserted_by:`).
- **MUST NOT fabricate a binding** — if no test genuinely asserts it, file `bd create -t bug` and leave `binding: pending`.
- New invariants introduced by a spec under `docs/superpowers/specs/` are auto-checked for registry presence; specs under `docs/specs/` or invariants introduced only in code are NOT auto-caught — register by hand.

## Plugin Runtime Symmetry (`.claude/rules/plugin-runtime-symmetry.md`)

Binary and Lua plugins MUST be treated identically by the host for any trust/policy/manifest-gate check. Place gates at the shared code path (e.g. `internal/plugin/event_emitter.go::Emit` is the common emit boundary for both runtimes). Runtime-specific mechanisms (gRPC token auth for binary, Lua VM lifecycle) are fine; differing *policy outcomes* are not. Permitted asymmetry: same ABAC-gated capability reached via different transports (Lua `world.query` host-capability vs. binary `WorldService` service) — NOT a parity gap as long as both hit the same `checkAccess` chokepoint.

## Import Organization

Standard Go grouping: stdlib, then third-party, then `github.com/holomush/holomush/...` internal imports — see any file in `internal/core/*_test.go` for the pattern (stdlib blank line, then module imports).

## Build/Test/Lint Discipline

**MUST use `task` for all build/test/lint/format** — never invoke `go build`, `go test`, `golangci-lint` directly.

```bash
task lint      # core loop
task fmt
task test
task build
task dev
task test -- ./internal/command/                        # scope to a package
task test -- -run TestCapability ./internal/command/     # scope to a test
task test:int                                            # integration (needs Docker)
```

`task test` does NOT compile `//go:build integration` files — refactors of shared types/interfaces MUST also run `task test:int` or breakage is silent.

## Database Migrations (`.claude/rules/database-migrations.md`)

`internal/store/migrations/`, embedded at compile time. Sequential 6-digit-padded numbering, always paired up/down, idempotent (`IF NOT EXISTS`/`IF EXISTS`), **no triggers/functions/stored procedures** — all logic in Go. New columns nullable or defaulted, never `NOT NULL` without backfill. No long-running backfills inside a migration. Down migrations MUST cleanly and fully revert the up.

## Proto Doc Comments (`.claude/rules/proto-doc-comments.md`)

Every proto message/field/RPC/service/enum/enum-value MUST carry a leading doc comment describing purpose/contract/units/invariants/failure modes — never a name-echo (enforced unconditionally by buf `COMMENTS` + `test/meta/proto_doc_comments_test.go`, run via `task lint:proto`). Ground every comment in the implementing Go handler (core→`internal/grpc`, world→`internal/world`, scene→`plugins/core-scenes`, web→`internal/web`).

## Gateway Boundary (`.claude/rules/gateway-boundary.md`)

`cmd/holomush/gateway.go` and `internal/web/` are protocol-translation only — MUST NOT access `WorldService`, `SessionStore`, repositories, or the DB directly; all game-state queries flow through core-server gRPC RPCs. Structural writes (create/set/end/invite/kick/transfer) from GUI/web MUST use a typed RPC on the BFF facade, never the human `HandleCommand`/`sendCommand` conversational-verb path (ADR `holomush-v4qmu`).

## AttributeProvider Optional-Attribute Convention (`.claude/rules/abac-providers.md`)

`internal/access/policy/attribute/**` providers MUST **omit** an optional attribute key entirely when unresolved — never emit an empty-string sentinel (the DSL's fail-safe treats MISSING as `false`, but `"" == ""` is `true`, so a sentinel fail-opens). Pair every optional attribute with a `has_X` boolean witness that is always present. Reference: `internal/access/policy/attribute/stream.go:40-48`.

---

*Convention analysis: 2026-07-08*
