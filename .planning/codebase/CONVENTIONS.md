---
last_mapped_commit: 0047100ed380c2135d541d1120dd3a950714d2f1
last_mapped_at: 2026-08-13
---
# Coding Conventions

**Analysis Date:** 2026-08-13

Every entry below names its **enforcement mechanism**. "Convention only" means a
human/reviewer catches it — nothing fails the build.

## Toolchain gates (what actually runs)

| Gate | Command | Contents |
|------|---------|----------|
| Lint umbrella | `task lint` (`Taskfile.yaml:202`) | `lint:go`, `lint:proto`, `lint:markdown`, `lint:yaml`, `lint:actions`, `lint:access-migration`, `lint:test-helpers`, `lint:plugin-manifests`, `lint:docs-symmetry`, `lint:docs-paths-sync`, `lint:adr`, `lint:no-timestamptz`, `lint:no-microsecond-truncate`, `lint:no-unixnano-in-repos`, `lint:no-zensical`, `lint:invariants` |
| Go lint | `task lint:go` (`Taskfile.yaml:754`) | `bin/custom-gcl` — golangci-lint **v2** built with the `gorules/` module plugin |
| Format | `task fmt` → `fmt:go` (gofumpt), `fmt:yaml`, `fmt:markdown` (rumdl), `fmt:dprint`, `license:add` |
| Format check | `task fmt:check` (`Taskfile.yaml:1041`) — dprint, rumdl, `gofumpt -l` drift |
| Pre-push | `task pr-prep` (fast lane; mandatory before push) |

`task fmt` **mutates files** (SPDX headers, table reflow). Commit its output or CI goes red.

## Naming Patterns

**Files:** snake_case Go files; tests co-located as `foo_test.go`; integration tests
`*_integration_test.go` or under `test/integration/<domain>/`. Migrations are
`NNNNNN_name.sql` (6-digit, one file carrying both directions).
*Enforcement:* migration naming/format — `internal/store/migrations_format_test.go`
(runs in the untagged `task test` lane). File naming otherwise: convention only.

**Functions / variables / types:** standard Go. `revive` rules enabled in
`.golangci.yaml` (`var-naming`, `exported`, `receiver-naming`, `error-naming`,
`error-strings`, `package-comments`, `unused-parameter`, `redefines-builtin-id`).
*Enforcement:* `revive` via `task lint:go`.

**Descriptive type names** are the house style (`CommandEntry`, not `Entry`), and
unexported fields are exposed by accessor methods. *Enforcement:* convention only.

## Code Style

**Formatting:** `gofumpt` (stricter gofmt) via `task fmt:go`; `dprint` + `rumdl`
for markdown; yaml via `task fmt:yaml`. *Enforcement:* `task fmt:check`, run inside
`task pr-prep` and CI's Lint job.

**Aligned blocks gotcha:** editing an aligned Go `const`/`var`/`struct` block can pass
`task build` and unit tests yet fail `fmt:check` — run `task fmt` after touching one.

**Lint suppression policy:** `//nolint` must be **line-scoped and explained** —
`nolintlint` is configured `require-explanation: true`, `require-specific: true`
(`.golangci.yaml`). Widening `.golangci.yaml` instead is prohibited by `CLAUDE.md`.
Repo precedent: `internal/web/handler.go` `//nolint:wrapcheck // gRPC status errors pass through as-is`.

## Error Handling

**Structured errors use `samber/oops`.** Construct with `oops.Errorf(...)`,
`oops.With(k, v).Wrap(err)`, and `oops.Code("CODE").Wrap(err)` at boundaries.
*Enforcement:* `wrapcheck` + `errorlint` + `errcheck` (with
`check-type-assertions: true`, `check-blank: true`); `wrapcheck` allowlists the
`oops` builder via `ignore-sig-regexps` / `ignore-package-globs`.

**Logging an error** — `pkg/errutil/log.go`:

```go
func LogErrorContext(ctx context.Context, msg string, err error, extraAttrs ...any) {
	attrs := oopsAttrs(err)
	attrs = append(attrs, extraAttrs...)
	slog.ErrorContext(ctx, msg, attrs...)
}
```

`oopsAttrs` lifts the oops `code` and `context` map to **top-level** structured
fields. A bare `slog.ErrorContext(ctx, msg, "err", err)` still carries the code but
**nested** as `error.code=…` — the difference is nesting plus ctx propagation, not
data loss (`.claude/rules/grpc-errors.md` corrects an older, false "data is lost"
claim). *Enforcement:* the `*Context` half is `sloglint`; preferring `errutil` over
bare slog is convention only.

**Asserting errors in tests** — `pkg/errutil/testing.go` provides
`AssertErrorCode(t, err, "CODE")` and `AssertErrorContext(t, err, key, value)`.
Note `AssertErrorCode` chain-walks via `oops.AsOops`; for **opacity** contracts
assert the top-level code directly (`oops.AsOops(err).Code()`), because a
double-wrap would otherwise pass. *Enforcement:* convention only.

**Method-value gotcha:** accessors must be called with `()`. `oops.With("reason", decision.Reason)`
compiles silently inside `...any` and logs a func value. *Enforcement:* unenforced —
`govet` `enable-all` does not catch this shape. (Inference: no analyzer in
`.golangci.yaml` targets it.)

### gRPC trust boundary (`.claude/rules/grpc-errors.md`)

- **Never** `status.Errorf(codes.Internal, "…: %v", err)` — the inner error text
  reaches clients. Log internally, return a static message.
- **Translate status ↔ oops at exactly ONE layer** (the outermost gRPC-crossing
  call site), never inside helpers — double translation breaks `status.FromError`
  chain walking.

*Enforcement:* convention only (rule doc + code review); no analyzer.

## Structured Logging

MUST use context-carrying variants (`slog.InfoContext`/`WarnContext`/`ErrorContext`/
`DebugContext`, `errutil.LogErrorContext`) whenever a `context.Context` is in scope —
trace/span ids live on the ctx, so bare calls orphan the line.

*Enforcement — mechanical,* `sloglint` in `.golangci.yaml`:

| Setting | Effect |
|---------|--------|
| `context: scope` | bare `slog.*` flagged only when a ctx is in scope (this IS the "absolutely impossible" carve-out) |
| `no-mixed-args` | no mixing `slog.Attr` with loose `"k", v` pairs |
| `static-msg` | message must be a literal/constant |
| `msg-style: lowercased` | messages start lowercase |
| `key-naming-case: snake` | attribute keys snake_case |
| `forbidden-keys` | `time`, `level`, `msg`, `source` banned |

## ID Generation

Two ULID generators; picking the wrong one is a correctness bug:

| Use | Generator | Why |
|-----|-----------|-----|
| Event IDs, session IDs | `core.NewULID()` (forwards to `internal/ulidgen`) | identity/dedup key; set as `Nats-Msg-Id`. Ordering is JetStream's per-stream `uint64`, **not** ULID lex order |
| Entity primary keys (players, characters, locations, exits, objects, policies) | `idgen.New()` (`internal/idgen`) | fresh `crypto/rand` entropy per call |

Gateway code (`internal/web`, `internal/telnet`) calls `ulidgen.New()` directly —
INV-EVENTBUS-1 forbids importing `internal/core` there.

**Events MUST be constructed via `eventbus.NewEvent(...)`** — never an
`eventbus.Event{}` literal, never a hand-stamped `ID`, never `idgen.New()`.
Construction sites: `internal/world/outbox/wire.go`, `internal/presence`,
`internal/sysbroadcast`, `internal/grpc` (`emitCommandResponse`).

**`crypto/rand` always, never `math/rand`.** Slice picks go through a
`crypto/rand`+`math/big` helper (`internal/naming.cryptoIntN`).

*Enforcement:* the custom analyzer **`ulidmakeforbidden`** (`gorules/`, enabled in
`.golangci.yaml`) bans `ulid.Make()` because it uses `math/rand`; it is excluded for
`_test.go` (fixtures may use it). The `NewEvent`-only rule is convention only.

## Crypto material handling

Seven custom `gorules/` analyzers fence `dek.Material` (INV-27):
`dekmaterialnojson`, `…nogob`, `…noproto`, `…nolog`, `…noslog`,
`…nofmtformatting`, plus `codeckeybytesallowlist`. Also `noremoteclockcompare`
(INV-58), `sceneopseventsappendonly`, `cursorpackageinternal`.
*Enforcement:* golangci-lint module plugin built by `task lint:build-custom-gcl`
into `bin/custom-gcl`; the analyzers have their own tests (`task test:gorules`).

## Import Organization

Standard Go grouping: stdlib / third-party / `github.com/holomush/holomush/...`,
enforced only by gofumpt's ordering within existing groups. Dot-imports are banned by
revive `dot-imports` **except** Ginkgo/Gomega in integration tests, which carry
`//nolint:revive // ginkgo convention`.

**Depguard bans (`.golangci.yaml` → `no-test-only-constructs-in-production`)** —
production files (`!$test`, excluding `internal/testsupport/**`,
`internal/cluster/clustertest/**`, `test/testutil/**`) MUST NOT import:
`internal/eventbus/eventbustest`, `internal/core/coretest`,
`internal/testsupport/quarantinetest`, `internal/testsupport/natstest`,
`internal/testsupport/integrationtest`. A companion shell gate
`task lint:test-helpers` (`Taskfile.yaml:818`) bans `policytest` imports from
production `internal/`/`pkg/`. `test/meta/depguard_config_test.go` guards the config itself.

## License Headers

SPDX Apache-2.0 header on `.go`, `.sh`, `.proto` (and yaml where appropriate);
**never** on generated `*.pb.go`. Applied by `task fmt` → `license:add`
(`license-eye header fix`); verified by `task license:check` and CI.
Config: `.licenserc.yaml` (`paths` / `paths-ignore`).

## Proto Doc Comments

Every message, field, RPC, service, enum and enum value MUST carry a leading doc
comment that describes purpose/contract, **grounded in the Go handler** — never a
restatement of the name. *Enforcement:* buf `COMMENTS` lint category **plus** the
name-echo quality gate `test/meta/proto_doc_comments_test.go`; both run under
`task lint:proto`. There is no exemption mechanism.

Regenerate + commit together after any `api/proto/**` change:
`task proto && task web:generate` → commit `pkg/proto/**/*.pb.go` + web `*_pb.ts`
(CI has a stale-diff check).

## Terminology (`.claude/rules/terminology.md`)

`location` (never room/area/zone), `exit`, `character` (the in-game entity) vs
`player` (the human), `session` vs `connection`, `presence`, `grid present`, `scene`.
*Enforcement:* a shell gate in `Taskfile.yaml:~810` rejects the legacy `"char:`
ABAC prefix in production Go; the rest of the vocabulary is convention only.

## Invariant Registry

Named system invariants live in `docs/architecture/invariants.yaml` (source of
truth) → `docs/architecture/invariants.md` (generated by `go run ./cmd/inv-render`,
never hand-edited). A test proves an invariant by carrying an annotation
immediately above it:

```go
// Verifies: INV-CRYPTO-28
func TestDecryptPluginRowFailClosedWithoutAuditEmitter(t *testing.T) { ... }
```

which flips the registry entry from `binding: pending` to `bound` (with
`asserted_by:` listing the files).

*Enforcement:* `test/meta/invariant_registry_test.go` — generate-and-diff drift,
provenance/ownership, binding presence, spec-orphan detection, plus
`TestBoundInvariantsAreGenuinelyAsserted` which fails a `bound` entry whose only
`// Verifies:` sites are Skip-only placeholders. `task lint:invariants` runs
`inv-render -check`. Limits: the orphan walk covers only
`docs/superpowers/specs/` — invariants born in `docs/specs/`, in
`.planning/phases/**/*-SPEC.md`, or in code must be registered **by hand**.

## Database Migrations

`internal/store/migrations/`, embedded, applied by goose. One `NNNNNN_name.sql` per
version carrying `-- +goose Up` and `-- +goose Down`; idempotent; no triggers or
functions; timestamps are `BIGINT` epoch-nanoseconds.

*Enforcement:*
- `TestEveryDollarQuotedMigrationBodyIsWrappedInStatementBeginEnd`
  (`internal/store/migrations_format_test.go`) — also rejects `ENVSUB` annotations.
- Go migrations: `TestGoMigrationRegistrationHoldsAcrossTheMigrationsCorpus` and
  `TestExactlyOneBlankImportWiresTheMigrationsPackageIntoStore`
  (`internal/store/migrations_register_test.go`), INV-STORE-11.
- `task lint:no-timestamptz`, `lint:no-unixnano-in-repos`,
  `lint:no-microsecond-truncate`, `lint:access-migration`.

## Gateway Boundary

The gateway (`cmd/holomush/gateway.go`, `internal/web/`) is protocol translation
only: gRPC clients, never repositories or DB access. GUI **structural** writes go
through typed BFF RPCs; `HandleCommand`/`sendCommand` is reserved for human
conversational verbs. *Enforcement:* `test/meta/gateway_status_code_census_test.go`
and `world_import_graph_test.go` / `world_caller_census_test.go` cover parts of it;
the general rule is convention + code review.

## Function & Module Design

- `gocritic` (diagnostic + style + performance tags; `hugeParam` disabled because
  `Event` is passed by value by design), `prealloc`, `unconvert`, `unparam`,
  `nilerr`, `gosec`, `misspell` all enabled.
- `//nolint:unparam` does **not** suppress revive's `unused-parameter` — suppress
  both: `//nolint:unparam,revive // …`.
- `time.Sleep` is banned inside `internal/eventbus/**` and
  `test/integration/eventbus_e2e/**` via `forbidigo` (scoped by a `path-except`
  exclusion); use `eventbustest.Await*` helpers instead.

## Web (SvelteKit) — `web/CLAUDE.md`

- Svelte 5 + SvelteKit 2, shadcn-svelte (nova) on bits-ui, Tailwind v4, ConnectRPC.
- Theme tokens: `--color-*` for everything except MUSH message colors, which use
  `--mush-*`; `--radius` is the only unprefixed token. Bare names (`--primary`) are
  forbidden.
- Tailwind v4 `@theme` compiles `var()` at **build time** — never put a runtime-
  varying `var()` inside `@theme`; register a static value and override on `.app-root`.
- *Enforcement:* `task web:test` runs `pnpm test:unit` (vitest) + `pnpm check`
  (`svelte-check`). Theme-token rules are convention only.

---

*Convention analysis: 2026-08-13*
