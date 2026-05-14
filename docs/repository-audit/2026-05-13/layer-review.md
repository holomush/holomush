# Layer-Discipline Audit

- **Date:** 2026-05-13
- **Auditor:** Claude (Opus 4.7, 1M context)
- **Scope:** Read-only review of `internal/`, `cmd/holomush/`, `web/` against
  the boundary invariants declared in `CLAUDE.md` and `.claude/rules/`.
- **Follow-up tracking:** `holomush-1bft` — epic for triaging findings into child task beads (`bd show holomush-1bft`)

## Executive summary

The repository invests heavily in *enforcing* its declared boundaries
(`gateway_imports_test`, `gateway_invariants` package, `plugin-runtime-symmetry`
rule, `RenderingPublisher` choke point). The seams that *have* tests are clean.
The damage is in three places where the seam is asserted-but-not-enforced:

1. **`internal/core/` still owns plugin payload types and emits literal
   plugin-namespaced event types.** `event.go:103-139` defines
   `PagePayload`/`WhisperPayload`/`OOCPayload`/`PemitPayload` even though
   `event.go:42-47` claims those moved to plugin packages. `engine.go:88,110`
   hard-codes the string `"core-communication:say"` and
   `"core-communication:pose"` in core-side handlers. Those handlers
   (`HandleSay`, `HandlePose`) are **dead code** — never called in production.
2. **There is no repository gatekeeper.** `internal/store` exposes
   `Pool() *pgxpool.Pool` (`postgres.go:62`), and ~25 packages take that pool
   to run their own SQL — including `internal/admin/approval`, `internal/totp`,
   `internal/cluster`, `internal/eventbus/audit/projection`,
   `internal/eventbus/crypto/dek`, and `internal/audit`. The "store" package
   is a sibling, not a layer.
3. **`eventbustest` lacks the compile-time guard the rule book claims for it.**
   `.claude/rules/testing.md:146` says "The `//go:build !integration` tag on
   the harness file enforces this." The file has no such tag, and
   `internal/testsupport/holomushtest/server.go:46` (built with `integration`)
   imports it to spin up an in-memory NATS inside an E2E harness.

Several lower-severity items follow (god-package shape of `internal/grpc`,
`property → world` inversion, `web/handler.go:156` reaching into
`core.NewULID` for a connection id, etc.).

## 1. `internal/core/` purity

### F-1.1 — Plugin-owned payload types live in core (high)

`internal/core/event.go:103-139` defines `PagePayload`, `WhisperPayload`,
`WhisperNoticePayload`, `OOCPayload`, and `PemitPayload`. The same file at
`event.go:42-47` claims "Plugin-owned event types have been migrated to
their respective plugin packages with qualified `<plugin>:<type>` identifiers".
The migration moved the *constants* but left behind the *payload structs* —
shaped exactly to the messaging plugin's wire schema.

The only callers are tests and the gateway translator:

- `internal/grpc/dispatcher_test.go:77` (test)
- `internal/web/translate_test.go:302, 320, 334, 353` (test)
- `internal/grpc/pipeline_rendering_test.go:257` (test)

No production code imports these — they exist as a back-door API for tests
to round-trip plugin payloads. This breaks the plugin-boundary invariant
recorded in user memory `feedback_plugin_boundary`: "Plugin-owned types
(event constants, verb registrations) MUST NOT leak into `internal/core/`".

Direction: move these structs to the owning plugin packages
(`plugins/core-communication/events.go` already exists). Re-route tests to
import the plugin package or a `plugintest` helper.

### F-1.2 — `core.Engine.HandleSay` / `HandlePose` are dead code that hard-code plugin event types (high)

`internal/core/engine.go:78-121` defines `HandleSay`/`HandlePose` that emit
events with `Type: "core-communication:say"` and `Type:
"core-communication:pose"` respectively (engine.go:88, 110). The plugin
boundary memory says: plugin-owned event constants must not appear in
`internal/core/`.

`rg -n "HandleSay|HandlePose"` outside `_test.go` and `engine.go` returns
no production callers. They are only used by `internal/core/engine_test.go`.
Removing them eliminates the violation outright; no production behavior
change.

Direction: delete `HandleSay`, `HandlePose`, `SayPayload`, `PosePayload` from
`internal/core/engine.go`. Keep `HandleConnect`/`HandleDisconnect` (those use
the host-owned `arrive`/`leave` constants from `event.go:49-50`).

### F-1.3 — Engine struct-literal construction bypasses `core.NewEvent` (medium)

`internal/core/engine.go:84, 107, 130, 153` build `core.Event{}` literals
directly. CLAUDE.md says "`core.Event{}` struct literals MUST use
`core.NewEvent()` rather than a raw literal". The literals do at least call
`NewULID()` and `time.Now()` so the I-16 monotonicity invariant is preserved,
but they side-step the rule-guard that exists precisely to prevent the next
edit from forgetting `NewULID()`. With F-1.2 most of these go away; the
two that remain (`HandleConnect`, `HandleDisconnect`) should switch to
`NewEvent(stream, eventType, actor, payload)`.

### F-1.4 — `builtins.go` host-side registration of plugin-shaped namespaces (low)

`internal/core/builtins.go:84-100` registers `crypto.totp_locked`,
`crypto.policy_set`, `crypto.system.rekey`, `crypto.system.operator_read`,
etc. The comment block (lines 79-100) is honest that these are *host-emit*
events, so the registration belongs to the host. Verdict: not a violation,
but the `crypto.` namespace looks like a plugin namespace; consider renaming
the registry's `Source` field to a discriminator other than the magic string
`"builtin"` and document the host-emit policy in `event-conventions.md`.

## 2. Plugin host symmetry

### F-2.1 — `Manager.luaHost` back-compat field is asymmetric (medium)

`internal/plugin/manager.go:856-872` resolves the plugin host for a
discovered plugin by type. For `TypeLua` it first checks `m.hosts[TypeLua]`,
then falls back to a dedicated `m.luaHost` field; for `TypeBinary` there is
no equivalent fallback. The same asymmetry shows up in
`manager.go:1223-1226` (`TestLoadPlugin`).

Rule violated: `.claude/rules/plugin-runtime-symmetry.md` — "Any host-side
trust check, validation, or feature MUST apply to both binary and Lua
plugins. Asymmetric behavior between plugin runtimes is forbidden". A
back-compat field is not a trust check, but it *is* host-side state that
makes one runtime privileged at wiring time.

Direction: delete the `luaHost` field; require all callers to register via
`hosts[TypeLua]`. The migration is mechanical — the `WithLuaHost` option
(`manager.go:107-108`) is the only writer; replace it with
`WithHost(TypeLua, host)`.

### F-2.2 — Shared Emit boundary is correctly placed (positive)

`internal/plugin/event_emitter.go:112-180` is the single emit choke point
for both runtimes. Manifest gate (`actor_kinds_claimable`), payload-size
validation (`core.ValidatePayload`), and sensitivity fence
(`EnforceSensitivity`) all fire here. This is exactly the shape
`plugin-runtime-symmetry.md` prescribes.

### F-2.3 — Host RPC parity holds for focus operations (positive)

`internal/plugin/goplugin/host_service.go` exposes `JoinFocus`/`LeaveFocus`/
`LeaveFocusByTarget`/`PresentFocus`/`QueryStreamHistory` over gRPC. The Lua
side in `internal/plugin/hostfunc/stdlib_focus.go:28-31` covers the same set
through the `FocusOps` interface. Parity holds.

## 3. Gateway boundary

### F-3.1 — Gateway import test is well-designed (positive)

`cmd/holomush/gateway_imports_test.go` is INV-GW-1's compile-time enforcer.
Forbidden imports (`internal/world`, `internal/access`, `internal/store`,
`internal/plugin`, `internal/eventbus`, `internal/auth/service`,
`internal/command`) are exhaustively listed and the `coreOnlyFiles` allowlist
is documented per-file. This is the strongest layer guard in the repo.

### F-3.2 — `internal/grpc` package mixes client and server (medium)

`internal/grpc/client.go` (gateway-side `Client`) and `internal/grpc/server.go`
(core-side `CoreServer`) live in the same Go package. `server.go` is
1,434 lines and pulls `access`, `auth`, `command`, `eventbus`,
`eventbus/authguard`, `plugin`, `session`, `world` (see `server.go:27-37`).
A gateway binary linking `grpc.Client` therefore transitively links every
domain package the server uses, defeating the purpose of the gateway-process
separation that `.claude/rules/gateway-boundary.md` argues for.

The `gateway_imports_test` catches *direct* gateway imports of forbidden
packages but not *transitive* ones via `internal/grpc` — the gateway is
allowed to import `internal/grpc` because `gateway.go` is on the
`coreOnlyFiles` exception list (`gateway_imports_test.go:23`).

Direction: split `internal/grpc` into `internal/grpc/coreserver` (server
code) and `internal/grpc/coreclient` (client code). The gateway depends
only on the client subpackage; the server subpackage pulls in domain
imports without leaking them sideways.

### F-3.3 — `internal/web/handler.go:156` reaches into `core.NewULID` for a connection id (low)

`internal/web/handler.go:18,156`: the web handler imports `core` solely to
call `core.NewULID()` for a per-stream connection id. Per `core/ulid.go:24`,
`NewULID` is reserved for "event IDs (core.Event.ID), session IDs, and any
identifier whose lexicographic order MUST match arrival order". Connection
ids do not need monotonic ordering; `idgen.New()` (the fresh-entropy
generator from `internal/idgen`) is the correct primitive. Switching saves
the only `internal/core` import on the gateway side and tightens the
monotonic-ULID budget.

### F-3.4 — Gateway file allowlist is large (medium)

`cmd/holomush/gateway_imports_test.go:22-60` excludes 18 files from
INV-GW-1 because they are "core-only" — i.e., they live in `cmd/holomush`
but actually wire core. The core/gateway split is logical, but file-level
(rather than package-level). A single `cmd/holomush/` binary still embeds
both processes; the test draws the boundary by *file basename*. A future
refactor that wants the gateway to scale horizontally will have to first
peel `cmd/holomush/core*.go`, `sub_grpc*.go`, `crypto_rekey_wiring*.go`,
`readstream_wiring*.go` into a separate `cmd/coreserver/` binary.

Not a bug — but the design comment in `gateway-boundary.md` ("runs as a
separate process (potentially scaled horizontally)") is aspirational, not
realised.

## 4. Store / repo layer

### F-4.1 — `pgxpool.Pool` is the de-facto repository interface (high)

`internal/store/postgres.go:62` exposes:

```go
func (s *PostgresEventStore) Pool() *pgxpool.Pool { ... }
```

and `internal/store/subsystem.go:90-95` reaches into the same pool.
Twenty-five packages take `*pgxpool.Pool` directly:

- `internal/admin/approval/repo.go:33`
- `internal/admin/policy/{emitter,verifier,chain_state,loader}.go`
- `internal/admin/readstream/cold_reader.go`
- `internal/audit/postgres.go:22` (uses `database/sql.DB`, even worse)
- `internal/auth/postgres/{reset_repo,player_repo}.go`
- `internal/cluster/registry.go`
- `internal/content/postgres_store.go`
- `internal/eventbus/audit/projection.go:47`
- `internal/eventbus/audit/chain/repo_postgres.go`
- `internal/eventbus/crypto/dek` (extensive — uses pgx directly)
- `internal/eventbus/crypto/kek/local_aead.go`
- `internal/eventbus/history/{tier,cold_postgres}.go`
- `internal/totp/repo.go:30`
- `internal/access/policy/store/postgres.go`
- `internal/plugin/schema_provisioner.go`
- `internal/audit/partition_creator.go`

The store package owns the connection lifecycle but not the schema or
the access pattern. Every consumer hand-rolls its own SQL and owns its
own transactions. There is no repository interface above SQL — each
package is its own repo.

This is not, on its own, a violation of any stated rule. It IS at odds with
"PostgreSQL is just storage" (CLAUDE.md / user memory) — because every
package is doing its own storage. The implication is:

- Schema migrations all live in `internal/store/migrations/` (good) but
  the SQL that *uses* the schema is scattered across 25 packages, so a
  schema rename requires a multi-package change with no compile-time
  coupling.
- Mocking the DB for tests means mocking 25 different shapes.
- The PR-prep gate's `pg-tool` check has no central place to inspect.

Direction (P2 — large but tractable): introduce
`internal/store/repo.<Entity>` interfaces (PlayerRepo, ApprovalRepo,
TotpRepo, AuditChainRepo, …) implemented by per-package files inside
`internal/store/`. Callers depend on the interface, not the pool. The
existing per-package implementations move into `internal/store/<area>/`
without churn to their internals.

### F-4.2 — Migrations are isolated and idempotent (positive)

`internal/store/migrations/` has 54 paired up/down files, all sequential
(`000001_…` through `000027_…` plus pairs). `rg "CREATE TRIGGER|CREATE
FUNCTION"` returns zero hits. CLAUDE.md's "no triggers/functions" rule is
upheld.

## 5. EventBus layering

### F-5.1 — Emit path is clean (positive)

The seams are:

1. **Plugin emit boundary:** `internal/plugin/event_emitter.go::Emit` —
   single choke point, exhaustively documented.
2. **Rendering enrichment:** `internal/eventbus/rendering_publisher.go::Publish`
   — single writer of `event.Rendering` and the `App-Rendering` NATS header;
   refuses to clobber a pre-populated header (`rendering_publisher.go:91-95`)
   so contract violations surface.
3. **Codec + crypto:** `internal/eventbus/codec/` and `internal/eventbus/crypto/aad`
   are pure leaves (no `internal/` imports beyond proto and `oops`).

### F-5.2 — `internal/eventbus/crypto/dek` is a fan-out hotspot (medium)

`go list -f '{{len .Imports}}'` ranks `crypto/dek` at 34 — the second
highest fan-out in the tree. It imports `internal/admin/approval`,
`internal/eventbus/audit/chain`, `internal/eventbus/codec`,
`internal/eventbus/crypto/aad`, `internal/eventbus/crypto/kek`,
`internal/idgen`, `internal/lifecycle`, plus pgx, ULID, oops, protobuf, the
sha/hex/json/rand stdlib, and `cyberphone/json-canonicalization`. The
import on `internal/admin/approval` is the architecturally surprising
one — a low-level crypto package depending on an admin subsystem (rekey
two-person approval). This is the Phase 5 rekey design pulling
dual-control state into the DEK lifecycle.

Direction: invert the dependency. Express the dual-control gate as an
interface in `crypto/dek` (e.g., `RekeyApprover`) implemented in
`admin/approval`. The wiring file (`cmd/holomush/crypto_rekey_wiring.go`)
already constructs both; only the import direction needs to flip.

### F-5.3 — `RenderingPublisher` import direction is correct (positive)

`internal/eventbus/rendering_publisher.go:12` imports `internal/core` for the
`VerbRegistry`. EventBus → core is the correct direction (lower layer
depends on the most-primitive types). No upward leak observed.

## 6. Web (`internal/web`) ↔ Svelte (`web/`)

### F-6.1 — Web client types come from generated protobuf (positive)

`web/src/lib/stores/eventRouter.ts:8` imports `GameEvent` from
`$lib/connect/holomush/web/v1/web_pb`. Hand-rolled mirror types were not
observed in `web/src/lib/stores/` for the event shape; `web/src/lib/connect/holomush/`
is generated TS from the same proto the gateway emits. No drift surface.

### F-6.2 — `internal/web/translate.go` is single-source rendering translator (positive)

The Go-side gateway translator (`internal/web/translate.go:42`) reads
`EventFrame.Rendering` (populated by `RenderingPublisher`) and projects it
to `webv1.GameEvent`. INV-GW-5 ("events arriving without rendering metadata
are dropped at the gateway") is enforced at `translate.go:51-60` with a
metric (`gatewaymetrics.DroppedNilRenderingTotal`) and a slog error.

### F-6.3 — `genericPayload` is a string-union back-door (low)

`internal/web/translate.go:18-31` defines a single `genericPayload` struct
that JSON-unmarshals any non-state event payload. It carries *every* field
any plugin event might use (`character_name`, `sender_name`, `target_name`,
`message`, `text`, `action`, …). This is the gateway's "I don't know the
plugin schema, so I'll unmarshal anything that fits" escape hatch.

This is the moral cost of decoupling the gateway from plugin payloads.
It's also a forward-compatibility hazard: a plugin emitting a field name
the gateway doesn't know is silently dropped. A registry-driven approach
(plugin manifest declares payload schema, gateway translates from
manifest) would be more durable but is out of scope for this audit.

## 7. Test layering

### F-7.1 — `eventbustest` is NOT compile-time gated (high)

`.claude/rules/testing.md:146` declares: "The `//go:build !integration` tag
on the harness file enforces this." `internal/eventbus/eventbustest/embedded.go`
has no such tag (verified by `rg "build !integration|build !e2e"
internal/eventbus/eventbustest/`).

The asserted enforcement is missing. As predicted:
`internal/testsupport/holomushtest/server.go:4,46,211` is built with
`//go:build integration` and imports `eventbustest` to wire an in-memory
NATS into an E2E server harness (`bus := eventbustest.New(t)`).
`internal/cluster/clustertest/harness.go:16,72` does the same.

This violates the testing rule and the user-memory pattern that "E2E uses
the full stack" — `holomushtest.Server` is the only thing some tests touch,
and that "server" is running embedded NATS pretending to be real NATS.

Direction (P1):

1. Add `//go:build !integration` to `internal/eventbus/eventbustest/embedded.go`.
2. The compile failure will surface in `internal/testsupport/holomushtest/`
   and `internal/cluster/clustertest/`. Either:
   - Move those harnesses to use real NATS via testcontainers (matching the
     PG testcontainer pattern), OR
   - Mark those harnesses themselves as "bus-integration only", not E2E.
3. Update `.claude/rules/testing.md:146` to accurately describe what is
   enforced.

### F-7.2 — Mocks are co-located by package (positive)

`internal/plugin/mocks/`, `internal/grpc/mocks/`, `internal/plugin/hostfunc/mocks/`,
`internal/auth/mocks/`. Mockery wraps the package being mocked, generated
into a sibling `mocks/` dir. This is the conventional Go shape and is
consistent.

### F-7.3 — `eventbustest` is widely imported by integration packages (medium)

15 integration packages under `test/integration/` import `eventbustest`.
If F-7.1 lands, those imports flag immediately and become the migration
target. They have valid reasons (bus-integration tests for cursor
concurrency, fanout, reconnect, soak) — the fix is to scope `eventbustest`
to bus tests only and have full E2E tests bring up real NATS.

## 8. Import-graph health

### F-8.1 — Top fan-out packages

```text
github.com/holomush/holomush/internal/plugin                  50
github.com/holomush/holomush/internal/grpc                    37
github.com/holomush/holomush/internal/eventbus/crypto/dek     34
github.com/holomush/holomush/internal/admin/readstream        34
github.com/holomush/holomush/internal/plugin/goplugin         32
github.com/holomush/holomush/internal/eventbus                31
github.com/holomush/holomush/internal/command                 28
github.com/holomush/holomush/internal/web                     27
github.com/holomush/holomush/internal/world                   26
github.com/holomush/holomush/internal/store                   26
```

`internal/plugin` (50 imports) and `internal/grpc` (37) are kitchen-sink
shapes. `internal/grpc/server.go` alone is 1,434 lines; `internal/grpc`
holds 27 `.go` files in one Go package. The package's responsibilities
include: connection management, auth handlers, dispatch, presence, location
follow, stream registry, stream-access policy, history queries, content
service, replay loops. That's at least five separable responsibilities
under one package name.

Direction: peel `internal/grpc` into `internal/grpc/auth`, `internal/grpc/stream`,
`internal/grpc/dispatch`, `internal/grpc/content` subpackages. The
`gateway_imports_test.go` allowlist shrinks correspondingly.

### F-8.2 — `property → world` is an inverted dependency (medium)

`internal/property/registry.go:44-51`:

```go
GetLocation(ctx context.Context, id ulid.ULID) (*world.Location, error)
GetObject(ctx context.Context, id ulid.ULID) (*world.Object, error)
UpdateLocation(ctx context.Context, subjectID string, loc *world.Location) error
UpdateObject(ctx context.Context, subjectID string, obj *world.Object) error
```

`property` is a generic attribute system. It should not know about
`world.Location` / `world.Object` concrete types — those are user-defined
entities the attribute system attaches to. The current direction means
adding any new entity type (player, scene, channel, …) forces a property
edit.

Direction: define `property.EntityRef` as a generic `{Type, ID}` shape and
have `world` register handlers via injection. The dependency flips:
`world` depends on `property`, not the reverse.

### F-8.3 — `internal/world` fan-in (17 packages) is a god-package crossroads (medium)

17 packages import `internal/world`. Most uses are for types
(`world.Location`, `world.Object`, `world.Exit`) and the `WorldService`
interface. The fan-in is justified by domain centrality, but combined with
F-8.2, the `world` package has accreted both *data shapes* and *services*.

Direction: split `internal/world` into `internal/worldtypes` (pure value
types) and `internal/world` (the service). Most callers only need the
former; only `grpc`, `command`, `bootstrap` need the latter.

### F-8.4 — `internal/plugin` fan-out at 50 (medium)

`internal/plugin` imports `access/policy/dsl`, `access/policy/store`,
`access/policy/types`, `command`, `core`, `eventbus`, `eventbus/subjectxlate`,
`grpc/focus`, `idgen`, `session`, `store`. That spread includes both
policy DSL parsing (a configuration concern) and grpc/focus (a runtime
concern). The package is the plugin loader, the policy installer, the
schema provisioner, the manifest validator, the host-emit middleware, and
the subscriber actor all at once.

Direction: separate `internal/plugin/loader` (manifest+discovery+dependency
DAG), `internal/plugin/policy` (policy installation+validation), and
`internal/plugin/runtime` (emit+subscribe+host registry). Today's grouping
forces every change to compile every concern.

## Priority list

### P0 — fix this sprint

1. **F-7.1** — add `//go:build !integration` to
   `internal/eventbus/eventbustest/embedded.go`; resolve the resulting
   compile failures in `holomushtest/` and `clustertest/`. The rule already
   exists, only the enforcement is missing.
2. **F-1.2** — delete dead `core.Engine.HandleSay`/`HandlePose` (and the
   `SayPayload`/`PosePayload` types) from `internal/core/engine.go`. Removes
   the hard-coded `core-communication:say` / `core-communication:pose`
   strings without any production-code reroute.
3. **F-1.1** — move `PagePayload`, `WhisperPayload`, `WhisperNoticePayload`,
   `OOCPayload`, `PemitPayload` from `internal/core/event.go` to
   `plugins/core-communication/events.go`; update the 4 test files that
   reference them.

### P1 — this quarter

4. **F-3.2** — split `internal/grpc` into `coreserver` and `coreclient`
   subpackages so the gateway cannot transitively pull server-side imports.
5. **F-2.1** — remove the `Manager.luaHost` back-compat field; require
   `hosts[TypeLua]` registration.
6. **F-1.3** — route the remaining engine `core.Event{}` literals through
   `core.NewEvent`.
7. **F-3.3** — `internal/web/handler.go:156` switches from `core.NewULID`
   to `idgen.New` for the connection id; drop the `core` import from
   `internal/web`.
8. **F-5.2** — invert the `internal/eventbus/crypto/dek → internal/admin/approval`
   dependency via a `RekeyApprover` interface declared in `dek/`.

### P2 — when convenient

9. **F-4.1** — introduce per-entity repository interfaces in
   `internal/store/`; migrate the 25 packages that take `*pgxpool.Pool`
   directly to depend on those interfaces.
10. **F-8.1, F-8.4** — peel `internal/grpc` and `internal/plugin` into
    responsibility-scoped subpackages.
11. **F-8.2** — invert `property → world` so `world` registers entity
    handlers with `property`.
12. **F-8.3** — split `internal/world` into `worldtypes` + `world` service.
13. **F-3.4** — graduate the core/gateway split from file-level
    (coreOnlyFiles in `gateway_imports_test.go`) to package-level by
    extracting `cmd/coreserver/`.
14. **F-1.4** — document the host-emit policy for `crypto.*` event types
    in `event-conventions.md`; consider a `Source` discriminator beyond
    the magic `"builtin"` string.

## Closing note

The audit's most striking finding is asymmetry between *aspiration* and
*enforcement*. The repo invests in compile-time guards
(`gateway_imports_test`, `gateway_invariants/meta_test`,
`I-16 ruleguard`) where it has them, but `eventbustest` and the
`pgxpool` repository pattern have only documentation, not enforcement, and
the gaps line up exactly with where the layers have drifted. The fix
pattern is mechanical: add the build tag, add the interface, run `task
test:int`, watch the boundary tighten.
