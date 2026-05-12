# Phase 5 Sub-Epic F — `AdminReadStream` + Pre-Data Audit + `read-stream` CLI

## Status

Draft, revision 4 (incorporates design-reviewer findings R1 2026-05-11,
R2 2026-05-12, R3 2026-05-12). Tracking bead: `holomush-jxo8.8`.

## Authors

Sean Brandt; collaborative drafting via Claude.

## Date

2026-05-12.

## Context

Phase 5 of the event-payload-cryptography roadmap shipped operator-tier
break-glass administration. Sub-epics A–E delivered the substrate:

- A (`jxo8.3`) — TOTP enrollment + replay-safe verification.
- B (`jxo8.5`) — `crypto.operator` capability + ABAC integration.
- C (`jxo8.4`) — localhost UNIX admin socket + ConnectRPC server + lifecycle.
- D (`jxo8.6`) — `OperatorAuthProvider` + `admin_approvals` dual-control
  table + `Repo.WaitForApproval` blocking primitive +
  `approval.ComputeOpArgsHash` canonical hash helper.
- E (`jxo8.7`) — 7-phase `Rekey` orchestrator, `internal/eventbus/audit/chain`
  primitive (`Chain` + 7-field `Handler` + `VerifierSubsystem`), INV-39
  `SourceResolver` / `FallbackResolver`, `RekeyCheckpointSweepSubsystem`,
  production-boot E2E harness pattern, `sha256:<hex>` hash-string encoding
  convention for chain payloads.

Sub-epic F (`holomush-jxo8.8`) is the second leaf consumer of D's substrate
and ships the **legitimate** operator-read path: a server-streaming
ConnectRPC endpoint on the admin UDS that authenticates the operator,
emits a pre-data audit (INV-42 anchor), streams decrypted sensitive events
matching a context+time filter, and emits a trailing completion audit. The
CLI `holomush admin read-stream` is the operator-facing surface; output is
plaintext on stdout, status on stderr.

F MUST NOT replicate or rebuild any of D's or E's substrate. It composes:

- D's `OperatorAuthProvider` for credentials + TOTP + capability.
- D's `admin_approvals` table + `Repo.WaitForApproval` for dual-control.
- D's `approval.ComputeOpArgsHash` for canonical request-args hashing.
- E's `internal/eventbus/audit/chain` primitive (full 7-field `Handler`)
  for the operator-read audit chain (factory pattern parallel to
  `dek.RekeyHandlerFor` / `policy.PolicySetHandlerFor`).
- E's `sha256:<hex>` hash-string convention (mirrored locally; the
  rekey package's helpers are package-private).
- E's INV-39 `SourceResolver` / `FallbackResolver` (already wired into the
  production cold tier) for stale-DEK resilience.
- The existing `HistoryReader` with `WithHistoryAuth` (history-reader
  crypto-options design, 2026-05-05) for the cold-tier read.

F also lands three explicit substrate amendments inside its own scope:

1. **`NoPlaintextReason` proto enum expansion** (4 → 7 values) + Go-side
   mirror + INV-GW-14 parity update. New reasons (`DEK_MISSING`,
   `DEK_BAD_COLUMNS`, `INTERNAL`) are stamped EXCLUSIVELY by F's
   operator-read classifier; hot-tier subscribe and shared cold-tier
   dispatcher continue stamping the existing 3 non-zero reasons. The
   expansion is bounded by what F's classifier can actually map from real
   production error codes (see §4.5).
2. **`codec.TypeRegistry`** — new host-side registry of sensitive
   context types, exposing `LookupType(name) (TypeRegistration, bool)`
   with `Arity`, `MatchID`, `OrderInsensitiveIDs` fields. F's
   variable-arity context validation depends on this. **Built-in types
   only** for F (scene/dm/location/character); plugin-declared sensitive
   context types are deferred to a follow-up epic that designs the
   manifest extension (declared in §2.6).
3. **`approval.Repo.GetByOpArgsHash`** — new method on D's `Repo`
   interface enabling idempotent dual-control reuse for repeated
   identical invocations. Declared in §2.7.

## Section 1 — Scope & Goals

### 1.1 In scope for sub-epic F (`holomush-jxo8.8`)

1. `AdminReadStream` server-streaming ConnectRPC method added to
   `AdminService` (admin UDS only; never public gRPC).
2. `OperatorAuthProvider` gate at handler entry: session-token validation +
   `crypto.operator` capability check + optional dual-control via D's
   existing `admin_approvals` table + `Repo.WaitForApproval` blocking
   primitive.
3. Pre-data audit emit (`crypto.system.operator_read`, AUDIT-only) BEFORE
   the first data frame; refusal on publish failure (INV-42).
4. Operator-read chain registration in E's `internal/eventbus/audit/chain`
   primitive via a new `OperatorReadChainFor(gameID) chain.Chain` factory
   and an `OperatorReadHandlerFor(gameID) chain.Handler` factory with all
   seven `Handler` fields populated (parallel to `dek.RekeyHandlerFor` and
   `policy.PolicySetHandlerFor`). Chain subject prefix
   `events.<game>.system.operator_read` satisfies INV-E26. **Both event
   types share the same NATS subject `events.<game>.system.operator_read.<request_id>`
   and are differentiated by `Event.Type` in their envelope** (Path C in
   R3 review). The chain primitive's `LoadEntriesByScope` returns both
   rows ordered by `js_seq` ASC, naturally giving the completed event
   its predecessor (the start event) for `prev_hash` computation.
5. F-local `OperatorReadAuditEmitter` paralleling `dek.RekeyAuditEmitter`:
   wraps `chain.NewEmitter(repo)` (prev-hash computation) + an
   `eventbus.Publisher` (NATS publish). Together they compose the
   chain-emit + publish seam.
6. Read-path seam over `HistoryReader.QueryHistory` configured with an
   `OperatorReadAuthGuard` (unconditional PERMIT after upstream gate) and
   a NO-OP `DecryptAuditEmitter` (one audit per invocation, per master
   spec §7.6 — not per event).
7. **F-local decrypt-failure classifier** (`internal/admin/readstream/classify.go::Classify`)
   that maps existing cold-tier error surface to expanded
   `NoPlaintextReason` values. The classifier matrix grounds each branch
   in a real production code (§4.5). **Host cold/hot tier semantics are
   unchanged**; existing fail-close behavior of `decodeColdRow` is
   preserved, and F's classifier transforms the already-returned error
   into a metadata-only frame.
8. `NoPlaintextReason` proto enum expansion from 4 → 7 values
   (`DEK_MISSING`, `DEK_BAD_COLUMNS`, `INTERNAL`). Includes Go-side mirror
   in `internal/eventbus/types.go` plus `types_proto_sync_test.go`
   parity update (INV-GW-14). **No changes to `cold_postgres.go` or
   `history/dispatcher.go` semantics**; the new reasons are produced
   exclusively by F's classifier (operator-read surface only).
9. **`codec.TypeRegistry` new substrate** (`internal/eventbus/codec/typeregistry.go`):
   `Register(name, TypeRegistration)`, `LookupType(name) (TypeRegistration, bool)`.
   **Built-in types (scene/dm/location/character) only.** Plugin-declared
   sensitive context types are deferred to a follow-up epic (the
   `crypto.emits` manifest carries `EventType` / `Sensitivity` /
   `Description` today, not context-type identity shape; extending it is
   out of F's scope).
10. **`approval.Repo.GetByOpArgsHash`** new method on D's `Repo` interface +
    Postgres impl (`internal/admin/approval/repo.go`). Adds DB migration
    `000036_admin_approvals_op_args_hash_idx` for index
    `(op_kind, op_args_hash, expires_at)`. Filter:
    `WHERE op_kind = $1 AND op_args_hash = $2 AND expires_at > now()
    AND approved_at IS NOT NULL AND primary_player_id != $3
    ORDER BY approved_at DESC LIMIT 1`. Tests added.
11. Trailing completion audit (`crypto.system.operator_read_completed`,
    AUDIT-only) emitted at stream-end with `terminated_by` + counts;
    failure logged but NOT raised.
12. Per-frame write deadline (30s default, server-configurable) to bound
    stuck-consumer hangs without capping total stream duration.
13. `holomush admin read-stream` CLI mirroring master spec §7.5.
14. Two new builtin event types registered in `internal/core/builtins.go`
    (`crypto.system.operator_read`, `crypto.system.operator_read_completed`)
    to pre-empt the `EMIT_UNKNOWN_VERB` regression E surfaced (verified via
    `TestINV_F13_BuiltinEventTypesRegistered`).
15. Production-boot E2E spec folded into
    `cmd/holomush/admin_authenticate_e2e_test.go` reusing the `adminAuthEnv`
    fixture pattern E established for the Rekey fold.

### 1.2 Out of scope

- Bulk export tooling. Operators copy stdout.
- Hot-tier reads. Cold-tier only via `HistoryReader.QueryHistory`.
- Tail / long-poll mode. No live subscription.
- Server-side redaction or PII scrubbing. Operator receives full plaintext;
  the audit captures the act.
- Cross-game reads. Game scope is inherited from `cfg.Game`; one game
  per invocation.
- Web UI / public gRPC exposure.
- `--metadata-only` client flag.
- Event-type / facet filtering.
- Hot-tier dispatcher / live subscriber `NoPlaintextReason` stamping
  with the new values. Hot-tier callers see contemporaneous DEKs and
  the existing 3 non-zero reasons remain accurate for that surface.
- Cold-tier semantic reshape (fail-close → silent stamp). `decodeColdRow`
  fail-close behavior is preserved; F's classifier wraps the existing
  errors. Host integration tests `cold_postgres_test.go:97` /
  `:127` stay green unchanged.
- **Plugin-declared sensitive context types** in `codec.TypeRegistry`.
  The `crypto.emits` manifest only carries event-type identity
  (`CryptoEmit{EventType, Sensitivity, Description}` at
  `internal/plugin/crypto_manifest.go:30-37`); extending it for
  context-type arity/match metadata is a substantive manifest schema
  change deferred to a follow-up epic.
- More granular `NoPlaintextReason` values beyond the 4 → 7 expansion.
  `DEK_DESTROYED` / `AAD_MISMATCH` / `VERSION_DRIFT` are deferred — the
  cold-tier error surface F consumes collapses those cases into the
  classifier values F does ship (see §4.5).

### 1.3 Gates F sits behind

| Gate | Provides | Status |
| ---- | -------- | ------ |
| D (`jxo8.6`) | `OperatorAuthProvider`, `admin_approvals`, `Repo.WaitForApproval`, `Approve` RPC, `OpenRequest{PrimaryPlayerID, OpKind, OpArgsHash}` shape, `approval.ComputeOpArgsHash` | Shipped |
| E (`jxo8.7`) | `internal/eventbus/audit/chain` primitive (7-field `Handler`), `dek.RekeyHandlerFor` template, INV-39 fallback, prod-boot harness pattern, INV-GW-14 proto/Go parity test, `sha256:<hex>` hash-string convention | Shipped |

### 1.4 What F unblocks

- Compliance-export tooling that calls `AdminReadStream` programmatically.
- Incident-response runbooks with a documented break-glass procedure.
- Per-context decrypt-rate metrics for anomaly-detection alerts.
- Forensic granularity on decrypt failures (via the expanded
  `NoPlaintextReason` enum at the operator-read surface).
- Future plugin-declared sensitive context types (after a follow-up epic
  extends the `crypto.emits` manifest schema).

## Section 2 — Architecture

### 2.1 Components

```text
internal/admin/readstream/
├── handler.go              — AdminReadStreamHandler entrypoint
├── auth_guard.go           — OperatorReadAuthGuard (PERMIT after upstream gate)
├── decrypt_audit_emitter.go — NoOpDecryptAuditEmitter (HistoryReader seam)
├── audit_emitter.go        — OperatorReadAuditEmitter (chain emit + publish seam,
│                              parallels dek.RekeyAuditEmitter)
├── classify.go             — F-local classifier: error surface → NoPlaintextReason
├── filter.go               — request → resolved bounds + validation
├── audit_payload.go        — OperatorReadStartPayload + OperatorReadCompletedPayload
│                              + local copies of encodeHash/encodeHashPtr helpers
│                              (mirror dek.encodeHash; package-private in dek)
├── chain.go                — OperatorReadChainFor + OperatorReadHandlerFor factories
│                              (all 7 Handler fields populated; Path C: shared subject)
├── subjects.go             — buildSubjects: ContextRef → []eventbus.Subject (direct)
├── stream_merge.go         — k-way merge over multiple HistoryStreams by event timestamp
├── deadline_writer.go      — sendWithDeadline write-deadline shim
└── *_test.go               — unit tests per file
```

Plus, outside the F-local package:

- `internal/core/builtins.go` — register both new event types.
- `internal/admin/socket/read_stream_handler.go` — thin adapter mirroring
  `rekey_handler.go`; wires the ConnectRPC method onto the composite handler.
- `internal/admin/socket/subsystem.go` — add `ReadStreamHandler` to
  `Config`, parallel to `RekeyHandler`.
- `api/proto/holomush/admin/v1/admin.proto` — `AdminReadStream` RPC declaration.
- `api/proto/holomush/admin/v1/read_stream.proto` — request/response messages
  including the variable-arity `ContextRef`.
- `api/proto/holomush/core/v1/core.proto` — expand `NoPlaintextReason` enum
  from 4 → 7 values.
- `internal/eventbus/types.go` — Go-side `NoPlaintextReason` mirror update.
- `internal/eventbus/types_proto_sync_test.go` — parity-test additions.
- `internal/eventbus/codec/typeregistry.go` — new `TypeRegistry` substrate
  with built-in registrations.
- `internal/eventbus/codec/typeregistry_test.go` — coverage.
- `internal/admin/approval/repo.go` — add `GetByOpArgsHash` method to `Repo`
  interface + Postgres impl.
- `internal/store/migrations/000036_admin_approvals_op_args_hash_idx.up.sql`
  paired with `.down.sql` — composite index `(op_kind, op_args_hash, expires_at)`.
  (Number `000036` continues the existing migration sequence; highest current
  is `000035_drop_checkpoint_dek_fks`.)
- `cmd/holomush/cmd_admin_read_stream.go` — CLI subcommand.
- `cmd/holomush/core.go::runCoreWithDeps` — handler construction + wiring.

**Not touched (operator-read-only stamping per §1.2):**

- `internal/eventbus/history/cold_postgres.go::decodeColdRow` semantics.
- `internal/eventbus/history/dispatcher.go` `NoPlaintextReason` stamp surface.
- `internal/eventbus/subscriber.go` hot-tier stamp surface.
- `internal/plugin/crypto_manifest.go` `CryptoEmit` schema (no context-type
  field added).
- `internal/plugin/manager.go` plugin-load flow (no `TypeRegistry`
  population hook).

### 2.2 Handler flow (happy path, dual-control off)

```text
AdminReadStreamHandler.Handle(ctx, req, stream):
  1. op := OperatorAuthProvider.Authorize(req.session_token, "crypto.operator")
  2. resolved, requestedFlags := ResolveBounds(req, cfg, typeRegistry, now)
       (rejects on validation BEFORE any audit emit)
  3. requestID := core.NewULID()
     startPayload := buildStartPayload(op, nil, req, resolved,
                                       requestedFlags, policyHash, requestID, now)
     // emit at events.<game>.system.operator_read.<request_id> with Event.Type=crypto.system.operator_read
     operatorReadAuditEmitter.EmitStart(ctx, startPayload, requestID)
       — on error: return DENY_AUDIT_PRE_DATA_PUBLISH (INV-42)
  4. stream.Send(ReadStarted{request_id, resolved bounds, contexts, policy_hash, started_at})
  5. subjects := buildSubjects(resolved.contexts, gameID)
     events_scanned, decrypt_fails, streamErr := streamData(ctx, stream, subjects, resolved, requestID)
  6. // emit at same subject events.<game>.system.operator_read.<request_id>
     //                with Event.Type=crypto.system.operator_read_completed
     // chain.Emitter loads prior entries by scope, sees the start event, returns its self_hash as prev_hash
     operatorReadAuditEmitter.EmitCompleted(ctx, completedPayload, requestID)
       — best-effort; failure logged + metric'd; NOT raised
  7. return streamErr
```

### 2.3 Dual-control variant

When `req.dual_control == true`, between steps 2 and 3:

```text
opArgsHash := approval.ComputeOpArgsHash(req)              // D-shipped; proto-canonical
existing, err := approvalRepo.GetByOpArgsHash(ctx, "admin_read_stream",
                                              opArgsHash, op.PlayerID)
                                              // server-side filter: expires_at > now,
                                              //                     approved_at IS NOT NULL,
                                              //                     primary_player_id != op.PlayerID
                                              // returns APPROVAL_NOT_FOUND if none
if errors.Is(err, ErrApprovalNotFound):
    openReq := approval.OpenRequest{
        PrimaryPlayerID: op.PlayerID,
        OpKind:          "admin_read_stream",
        OpArgsHash:      opArgsHash,
    }
    requestID, err := approvalRepo.Open(ctx, openReq)     // mints ULID; sets expires_at server-side (5m)
    if err != nil { return wrap(err) }

    stream.Send(PendingApproval{request_id: requestID, expires_at: now.Add(D.ApprovalTTL)})

    approval, err := approvalRepo.WaitForApproval(ctx, requestID, now.Add(cfg.ApprovalTTL))
    switch {
    case errors.Is(err, ErrApprovalWaitDeadline), errors.Is(err, ErrApprovalExpired):
        return emitTimeoutFinish(stream, requestID, TERMINATED_BY_DUAL_CONTROL_TIMEOUT)
    case errors.Is(err, ErrApprovalWaitCancelled):
        return emitTimeoutFinish(stream, requestID, TERMINATED_BY_CLIENT_DISCONNECT)
    case err != nil:
        return wrap(err)
    }
else if err == nil:
    approval = existing                                   // idempotent reuse of fresh prior approval
else:
    return wrap(err)

proceed to step 3 with approval attached to startPayload
```

`Repo.WaitForApproval` (D-shipped at `internal/admin/approval/repo.go:189-224`)
polls the row every 500ms, surfacing:

- `APPROVAL_WAIT_DEADLINE` when the caller's deadline passes.
- `DENY_APPROVAL_EXPIRED` when the row's server-side `expires_at` passes.
- `APPROVAL_WAIT_CANCELLED` when `ctx` is canceled (operator disconnects).

`Repo.GetByOpArgsHash` (NEW; §2.7) returns the freshest matching row with
ALL filters applied server-side (no result-side filtering).

`OpenRequest` carries `PrimaryPlayerID`, `OpKind`, `OpArgsHash`. It
does NOT carry `RequestID` (Open mints) or `ExpiresAt` (server enforces
5m). Per `internal/admin/approval/repo.go:48-61`.

### 2.4 Trust boundaries

| Boundary | Trust mechanism |
| -------- | --------------- |
| CLI → UDS | OS file permissions + `SO_PEERCRED` (operational metadata, not a defense factor against the threat-model adversary per master spec §7.5) |
| UDS → handler | `OperatorAuthProvider` (admin creds + TOTP + `crypto.operator` capability) — the actual defense |
| handler → HistoryReader | `OperatorReadAuthGuard` returns PERMIT unconditionally; HistoryReader MUST NOT re-authorize against runtime ABAC (would conflict with INV-43) |
| handler → eventbus (audit) | `OperatorReadAuditEmitter` over E's `internal/eventbus/audit/chain` primitive; INV-42 refusal on pre-data publish failure |
| handler → DEK manager | Standard `dek.Manager` with INV-39 fallback; no AuthGuard re-check |

### 2.5 Wiring change in `cmd/holomush/core.go::runCoreWithDeps`

```go
// (existing `clock`, `chainRepo`, `busPublisher`, `dekMgr`, `policyHashProvider`,
//  `operatorAuthProvider`, `approvalRepo`, `js`, `pool`, `logger` from prior wiring.)

// F-local audit emitter: prev-hash via chain primitive + publish via NATS.
operatorReadHandler := readstream.OperatorReadHandlerFor(cfg.Game)
operatorReadEmitter := readstream.NewOperatorReadAuditEmitter(
    chain.NewEmitter(chainRepo),    // arity 1; prev-hash only
    busPublisher,                    // publishes events.<game>.system.operator_read.<rid>
    operatorReadHandler,             // for Canonicalize + Chain metadata
)

// Register the operator-read chain in the existing VerifierSubsystem.
// Construction collects all Handlers via VerifierSubsystemConfig.Handlers at
// boot — see verifier_subsystem.go:52-91; no Register method exists.
chainVerifierSubsystem := chain.NewVerifierSubsystem(chain.VerifierSubsystemConfig{
    Repo: chainRepo,
    Handlers: []chain.Handler{
        policy.PolicySetHandlerFor(cfg.Game),              // existing (D)
        dek.RekeyHandlerFor(cfg.Game),                     // existing (E)
        operatorReadHandler,                                // NEW (F)
    },
    Logger: logger,
})

// Built-in TypeRegistry registrations. (No plugin hook; deferred per §1.2.)
typeRegistry := codec.NewTypeRegistry()
codec.RegisterBuiltinTypes(typeRegistry)                    // scene/dm/location/character

operatorReadGuard := readstream.NewOperatorReadAuthGuard()
noopAuditEm := readstream.NewNoOpDecryptAuditEmitter()
readstreamHistory := history.NewReader(
    js, pool, cfg.EventBus.Config().StreamMaxAge, clock.Now,
    history.WithHistoryAuth(operatorReadGuard, dekMgr, noopAuditEm),
)

readStreamHandler := readstream.NewHandler(readstream.Config{
    Auth:          operatorAuthProvider,                       // from D
    Approvals:     approvalRepo,                               // from D; uses GetByOpArgsHash + WaitForApproval
    History:       readstreamHistory,
    AuditEmitter:  operatorReadEmitter,                        // F-local wrapper
    TypeRegistry:  typeRegistry,                               // built-ins only
    Classifier:    readstream.NewClassifier(),                 // §4.5
    PolicyHash:    policyHashProvider,                         // same source E uses
    Clock:         clock,
    Game:          cfg.Game,
    MaxWindow:     cfg.AdminSocket.OperatorReadMaxWindow,      // default 30d
    DefaultWindow: cfg.AdminSocket.OperatorReadDefaultWindow,  // default 1h
    WriteDeadline: cfg.AdminSocket.OperatorReadWriteDeadline,  // default 30s
    ApprovalTTL:   cfg.AdminSocket.OperatorReadApprovalTTL,    // default 5m
})

adminSocketCfg.ReadStreamHandler = readStreamHandler

if cfg.AdminSocket.OperatorReadMaxWindow > 90*24*time.Hour {
    logger.Warn("admin.readstream max_window exceeds 90d compliance threshold",
        "max_window", cfg.AdminSocket.OperatorReadMaxWindow)
}
```

### 2.6 New substrate: `codec.TypeRegistry` (built-ins only)

New package `internal/eventbus/codec/typeregistry.go`:

```go
// TypeRegistry is the closed-set registry of sensitive context types.
// Built-in types are registered at boot via codec.RegisterBuiltinTypes.
// Plugin-declared types are deferred to a follow-up epic (the crypto.emits
// manifest schema currently carries only EventType/Sensitivity/Description,
// not the Arity/MatchID metadata this registry requires).
type TypeRegistry interface {
    Register(name string, reg TypeRegistration) error
    LookupType(name string) (TypeRegistration, bool)
}

// TypeRegistration describes a sensitive context type's identity shape.
type TypeRegistration struct {
    Arity int
    MatchID func(id string) bool
    OrderInsensitiveIDs bool
}

// NewTypeRegistry constructs an empty registry.
func NewTypeRegistry() TypeRegistry { /* sync.RWMutex-protected map */ }

// RegisterBuiltinTypes populates the four host-defined sensitive context
// types. Called once at host wiring time. Idempotent within a single call
// (Register MUST reject duplicates with TYPE_REGISTRY_DUPLICATE; reseeding
// for tests re-creates the registry rather than mutating it).
func RegisterBuiltinTypes(r TypeRegistry) error {
    if err := r.Register("scene",     TypeRegistration{Arity: 1, MatchID: isULID, OrderInsensitiveIDs: false}); err != nil { return err }
    if err := r.Register("location",  TypeRegistration{Arity: 1, MatchID: isULID, OrderInsensitiveIDs: false}); err != nil { return err }
    if err := r.Register("character", TypeRegistration{Arity: 1, MatchID: isULID, OrderInsensitiveIDs: false}); err != nil { return err }
    if err := r.Register("dm",        TypeRegistration{Arity: 2, MatchID: isULID, OrderInsensitiveIDs: true});  err != nil { return err }
    return nil
}
```

`Register` MUST reject duplicate names with `TYPE_REGISTRY_DUPLICATE`. The
registry is a normal Go dependency (not a lifecycle subsystem); ordering is
enforced by direct function-call dependency at wiring time — the
`readstream.Handler` constructor takes the populated registry as a
parameter, so Go's compile-time ordering is the only ordering F needs.

There is no `Unregister`. There is no plugin hook in F's scope; a future
epic will extend the `crypto.emits` manifest schema and add the loader
integration.

### 2.7 New substrate: `approval.Repo.GetByOpArgsHash`

Added to D's `Repo` interface (`internal/admin/approval/repo.go`):

```go
type Repo interface {
    Open(ctx, req OpenRequest) (RequestID, error)              // existing
    Get(ctx, id RequestID) (Approval, error)                   // existing
    MarkApproved(ctx, id RequestID, secondOpPlayerID string) error  // existing
    WaitForApproval(ctx, id RequestID, deadline time.Time) (Approval, error)  // existing
    GetByOpArgsHash(ctx, opKind string, opArgsHash []byte, excludePlayerID string) (Approval, error)  // NEW
}
```

Postgres impl SQL (all filters server-side):

```sql
SELECT request_id, primary_player_id, op_kind, op_args_hash,
       expires_at, approved_at, approved_by_player_id, created_at
  FROM admin_approvals
 WHERE op_kind             = $1
   AND op_args_hash        = $2
   AND expires_at          > now()
   AND approved_at         IS NOT NULL
   AND primary_player_id  != $3        -- exclude rows authored by caller
 ORDER BY approved_at DESC
 LIMIT 1;
```

Returns `APPROVAL_NOT_FOUND` if no row matches. Tiebreaker (multi-row):
most recently approved.

DB migration `000036_admin_approvals_op_args_hash_idx`:

```sql
CREATE INDEX admin_approvals_op_kind_args_hash_idx
    ON admin_approvals (op_kind, op_args_hash, expires_at);
```

(`.up.sql` + `.down.sql` paired per project migration discipline.)

Integration tests in `internal/admin/approval/repo_integration_test.go`:

- `TestRepo_GetByOpArgsHash_ReturnsApproved` — happy path.
- `TestRepo_GetByOpArgsHash_FiltersExpired` — expired row → NOT_FOUND.
- `TestRepo_GetByOpArgsHash_FiltersOwnAuthor` — operator A can't reuse
  approval where they are `primary_player_id`.
- `TestRepo_GetByOpArgsHash_FiltersUnapproved` — pending row not returned.
- `TestRepo_GetByOpArgsHash_TiebreakerMostRecent` — picks freshest.

## Section 3 — Data model

### 3.1 Configuration

Added to `cfg.AdminSocket`:

```yaml
admin_socket:
  operator_read_default_window: 1h
  operator_read_max_window: 30d
  operator_read_write_deadline: 30s
  operator_read_approval_ttl: 5m
```

Startup-time WARN log if `operator_read_max_window > 90d`.

### 3.2 Proto: `AdminReadStream` RPC

Added to `AdminService` in `api/proto/holomush/admin/v1/admin.proto`:

```protobuf
rpc AdminReadStream(AdminReadStreamRequest) returns (stream AdminReadStreamResponse);
```

### 3.3 New file `api/proto/holomush/admin/v1/read_stream.proto`

```protobuf
syntax = "proto3";
package holomush.admin.v1;

import "google/protobuf/timestamp.proto";
import "holomush/core/v1/core.proto";

option go_package = "github.com/holomush/holomush/pkg/proto/holomush/admin/v1;adminv1";

message ContextRef {
  string type = 1;
  repeated string ids = 2;
}

message AdminReadStreamRequest {
  string session_token = 1;
  repeated ContextRef contexts = 2;
  google.protobuf.Timestamp since = 3;
  google.protobuf.Timestamp until = 4;
  string justification = 5;
  bool dual_control = 6;
}

message AdminReadStreamResponse {
  oneof payload {
    PendingApproval pending_approval = 1;
    ReadStarted     started          = 2;
    holomush.core.v1.Event event     = 3;
    ReadFinished    finished         = 4;
  }
}

message PendingApproval {
  bytes request_id              = 1;
  google.protobuf.Timestamp expires_at = 2;
}

message ReadStarted {
  bytes request_id              = 1;
  bytes policy_hash             = 2;
  google.protobuf.Timestamp resolved_since = 3;
  google.protobuf.Timestamp resolved_until = 4;
  repeated ContextRef resolved_contexts    = 5;
  google.protobuf.Timestamp started_at     = 6;
}

message ReadFinished {
  enum TerminatedBy {
    TERMINATED_BY_UNSPECIFIED          = 0;
    TERMINATED_BY_CLIENT_EOF           = 1;
    TERMINATED_BY_CLIENT_DISCONNECT    = 2;
    TERMINATED_BY_DEADLINE_EXCEEDED    = 3;
    TERMINATED_BY_SERVER_ERROR         = 4;
    TERMINATED_BY_DUAL_CONTROL_TIMEOUT = 5;
    TERMINATED_BY_AUDIT_EMIT_FAILURE   = 6;
  }
  TerminatedBy terminated_by  = 1;
  int64 events_scanned        = 2;
  int64 decrypt_fail_count    = 3;
  google.protobuf.Timestamp finished_at = 4;
}
```

### 3.4 `NoPlaintextReason` enum expansion (4 → 7)

`api/proto/holomush/core/v1/core.proto`:

```protobuf
enum NoPlaintextReason {
  NO_PLAINTEXT_REASON_UNSPECIFIED      = 0;
  NO_PLAINTEXT_REASON_AUTHGUARD_DENY   = 1; // existing
  NO_PLAINTEXT_REASON_STALE_DEK        = 2; // existing
  NO_PLAINTEXT_REASON_AUDIT_QUEUE_FULL = 3; // existing
  NO_PLAINTEXT_REASON_DEK_MISSING      = 4; // NEW: EVENTBUS_COLD_DEK_COLUMNS_MISSING
  NO_PLAINTEXT_REASON_DEK_BAD_COLUMNS  = 5; // NEW: EVENTBUS_COLD_BAD_DEK_COLUMNS
  NO_PLAINTEXT_REASON_INTERNAL         = 6; // NEW: handler-side decode bug; catch-all
}
```

Three new values, each grounded in a real production error code (see §4.5
classifier). `DEK_DESTROYED` / `AAD_MISMATCH` / `VERSION_DRIFT` from
rev 2 are out — those codes either don't exist in the production
codebase or are converted to `source.ErrMetadataOnly` by the fallback
resolver before reaching F's classifier (existing `STALE_DEK` covers
that path).

Go-side mirror in `internal/eventbus/types.go::NoPlaintextReason` adds the
three new values. `internal/eventbus/types_proto_sync_test.go` parity test
(INV-GW-14) extends its proto-Go cross-check.

**Stamping locality:** the three new values are stamped EXCLUSIVELY by
F's classifier (`internal/admin/readstream/classify.go`, §4.5).
`cold_postgres.go::decodeColdRow` continues to fail-close on bad-DEK rows
with the existing `EVENTBUS_COLD_DEK_COLUMNS_MISSING` /
`EVENTBUS_COLD_BAD_DEK_COLUMNS` codes; `history/dispatcher.go` continues
to stamp `STALE_DEK` / `AUTHGUARD_DENY` / `AUDIT_QUEUE_FULL` per existing
semantics. Existing tests are NOT modified.

### 3.5 Audit payload Go types

Both payload types embed hash fields as `sha256:<hex>` strings — same
encoding the rekey chain uses (`internal/eventbus/crypto/dek/audit.go::encodeHash`).
Because that helper is package-private to `dek`, F duplicates it in
`audit_payload.go`. This keeps JCS canonical-form encoding byte-equivalent
across chains (so INV-E28 hash recompute works identically; INV-F9 chain
linkage is byte-stable).

```go
// internal/admin/readstream/audit_payload.go

// ContextRef is the Go mirror of adminv1.ContextRef.
type ContextRef struct {
    Type string   `json:"type"`
    IDs  []string `json:"ids"`
}

// encodeHash mirrors internal/eventbus/crypto/dek/audit.go::encodeHash
// (package-private there). MUST produce byte-identical output to maintain
// cross-chain JCS canonical-form parity.
func encodeHash(b []byte) string {
    return fmt.Sprintf("sha256:%s", hex.EncodeToString(b))
}

// encodeHashPtr returns nil for genesis entries.
func encodeHashPtr(b []byte) *string {
    if b == nil { return nil }
    s := encodeHash(b)
    return &s
}

// OperatorReadStartPayload is the JSON-shaped payload of the
// crypto.system.operator_read audit event.
type OperatorReadStartPayload struct {
    // Operator identity
    OperatorPlayerID       ulid.ULID  `json:"operator_player_id"`
    OperatorSessionTokenID string     `json:"operator_session_token_id"`
    PeerCredUID            uint32     `json:"peercred_uid"`
    PeerCredPID            int32      `json:"peercred_pid,omitempty"`

    // Dual-control (zero values when not used)
    DualControl       bool       `json:"dual_control"`
    ApproverPlayerID  *ulid.ULID `json:"approver_player_id,omitempty"`
    ApprovalID        *ulid.ULID `json:"approval_id,omitempty"`

    // Justification (validated: 1..4096 bytes after trim)
    Justification string `json:"justification"`

    // Request: what the operator typed (nullable = defaulted)
    RequestedContexts []ContextRef `json:"requested_contexts"`
    RequestedSince    *time.Time   `json:"requested_since,omitempty"`
    RequestedUntil    *time.Time   `json:"requested_until,omitempty"`

    // Resolved: what the server actually queried
    ResolvedContexts []ContextRef `json:"resolved_contexts"`
    ResolvedSince    time.Time    `json:"resolved_since"`
    ResolvedUntil    time.Time    `json:"resolved_until"`

    // Chain binding — encoded as "sha256:<hex>" string per rekey precedent.
    // RequestID is encoded as the 26-char ULID Base32 string (NOT ulid.ULID)
    // to mirror the rekey-chain precedent (RekeyAuditPayload.RequestID is
    // `string`); this makes the chain primitive's ScopeFromPayload extraction
    // resilient to ULID-type changes and keeps the audit JSON shape stable.
    PolicyHash string `json:"policy_hash"`
    RequestID  string `json:"request_id"` // 26-char ULID Base32

    // Chain bookkeeping
    SelfHash string  `json:"self_hash"`              // "sha256:<hex>"; emitter populates
    PrevHash *string `json:"prev_hash,omitempty"`    // nil at genesis

    // Timing
    StartedAt time.Time `json:"started_at"`
}

type OperatorReadCompletedPayload struct {
    RequestID        string    `json:"request_id"`        // 26-char ULID Base32; chain ScopePayloadField; links to start
    TerminatedBy     string    `json:"terminated_by"`
    EventsScanned    int64     `json:"events_scanned"`
    DecryptFailCount int64     `json:"decrypt_fail_count"`
    PolicyHash       string    `json:"policy_hash"`        // "sha256:<hex>"
    SelfHash         string    `json:"self_hash"`          // "sha256:<hex>"
    PrevHash         string    `json:"prev_hash"`          // "sha256:<hex>"; non-empty (links to start)
    FinishedAt       time.Time `json:"finished_at"`
}
```

### 3.6 Audit-chain registration (Path C: shared subject, two event types)

Both events emit to the **same** NATS subject:
`events.<game>.system.operator_read.<request_id>`. They are differentiated
by `Event.Type` in the envelope (one is `crypto.system.operator_read`,
the other is `crypto.system.operator_read_completed`). The chain
primitive's `LoadEntriesByScope` returns both rows ordered by `js_seq`
ASC, giving the completed event its predecessor naturally.

```go
// internal/admin/readstream/chain.go

func OperatorReadChainFor(gameID string) chain.Chain {
    return chain.Chain{
        SubjectPrefix:     "events." + gameID + ".system.operator_read",
        SelfHashField:     "self_hash",
        PrevHashField:     "prev_hash",
        ScopePayloadField: "request_id",
    }
}

// OperatorReadHandlerFor bundles chain metadata + all 7 callback fields.
// Both event types share the same subject, so SubjectFor / ScopeFromSubject
// have a single mapping. Differentiation by Event.Type happens at the
// emitter and consumer level, not at the chain-primitive level.
func OperatorReadHandlerFor(gameID string) chain.Handler {
    c := OperatorReadChainFor(gameID)
    prefixDot := c.SubjectPrefix + "."
    return chain.Handler{
        Chain: c,
        SubjectFor: func(scope string) string {
            return prefixDot + scope                    // scope = request_id ULID string
        },
        ScopeFromSubject: func(subject string) (string, error) {
            if !strings.HasPrefix(subject, prefixDot) {
                return "", oops.Code("OPERATOR_READ_SCOPE_FROM_SUBJECT_FAILED").
                    With("subject", subject).
                    With("expected_prefix", prefixDot).
                    Errorf("subject prefix mismatch")
            }
            return subject[len(prefixDot):], nil
        },
        ScopeFromPayload: operatorReadScopeFromPayload,    // extracts payload["request_id"] as string
        Canonicalize:     canonicalizeOperatorReadPayload, // proto-decode envelope + JCS-canonicalize
        PrevHashOf:       operatorReadPrevHashOf,          // sha256:<hex> string → bytes via decodeHashString
        SelfHashOf:       operatorReadSelfHashOf,          // sha256:<hex> string → bytes
    }
}
```

Concrete callbacks (mirror `policy.PolicySetHandlerFor` + `dek.RekeyHandlerFor`
templates):

- `operatorReadScopeFromPayload(envOrJSON []byte) (string, error)`:
  proto-decodes the envelope (or accepts raw JSON for test fakes via the
  `decodePolicyPayloadJSON`-style fallback), parses payload into a
  small struct exposing only `request_id`, returns the ULID string.
- `canonicalizeOperatorReadPayload(envOrJSON []byte) ([]byte, error)`:
  proto-decodes the envelope, JSON-unmarshals into a `map[string]any`,
  re-marshals via RFC 8785 JCS. Both event types share this canonicalizer
  because their payload shapes carry the same chain fields (`self_hash`,
  `prev_hash`, `request_id`).
- `operatorReadPrevHashOf` / `operatorReadSelfHashOf`: parse JSON,
  read the string field, strip `sha256:` prefix, hex-decode to bytes.
  Genesis: `prev_hash` absent → nil bytes return.

| Event type | NATS subject (shared) | Chain position |
| ---------- | --------------------- | -------------- |
| `crypto.system.operator_read` | `events.<game>.system.operator_read.<request_id>` | Start; `prev_hash = nil` at genesis |
| `crypto.system.operator_read_completed` | (same subject) | Continues; `prev_hash` = self_hash of start (computed via `chain.NewEmitter.ComputePrevHashFor` walking the scope's existing entries) |

`LoadEntriesByScope` returns both rows in `js_seq` ASC order, so the
completed event's `prev_hash` is computed correctly without F caring
about ordering. The chain verifier walks the same `LoadEntriesByScope`
output, recomputes each row's `self_hash`, and asserts the linked-list
invariants (INV-E28, INV-D10, INV-D11 generalized).

Subject prefix satisfies INV-E26 (`events.` prefix); scope payload field
`request_id` is non-empty (INV-E27).

### 3.7 F-local `OperatorReadAuditEmitter`

Mirrors `internal/eventbus/crypto/dek/audit.go::RekeyAuditEmitter` —
wraps `chain.NewEmitter(repo)` (prev-hash computation) and an
`eventbus.Publisher` (NATS publish).

```go
// internal/admin/readstream/audit_emitter.go

type OperatorReadAuditEmitter interface {
    // EmitStart canonicalizes the start payload, computes prev_hash for the
    // scope (genesis: nil), stamps prev_hash + self_hash into the payload,
    // marshals the envelope with Event.Type = "crypto.system.operator_read",
    // and publishes to events.<game>.system.operator_read.<request_id>.
    EmitStart(ctx context.Context, payload OperatorReadStartPayload, requestID ulid.ULID) error

    // EmitCompleted does the same for the completed event. The chain emitter
    // loads existing entries by scope (LoadEntriesByScope returns the start
    // event since it shares the subject), returns its recomputed self_hash
    // as the prev_hash. Event.Type = "crypto.system.operator_read_completed".
    // Publish target subject is identical to EmitStart's.
    EmitCompleted(ctx context.Context, payload OperatorReadCompletedPayload, requestID ulid.ULID) error
}

func NewOperatorReadAuditEmitter(
    ce chain.Emitter,                   // chain.NewEmitter(repo); arity 1
    pub eventbus.Publisher,
    h chain.Handler,                     // OperatorReadHandlerFor(game)
) OperatorReadAuditEmitter {
    return &operatorReadAuditEmitter{ce: ce, pub: pub, h: h}
}
```

Both `EmitStart` and `EmitCompleted` use `h.SubjectFor(requestID.String())`
to compute the target subject. The subject is identical for both; only
`Event.Type` differs in the marshaled envelope. The chain emitter's
`ComputePrevHashFor` does the natural-walk thing — for the start event
there are zero prior entries (genesis); for the completed event there's
one (the start), so its self_hash becomes the completed event's prev_hash.

### 3.8 Builtin event-type registrations

`internal/core/builtins.go` (mirrors the `crypto.system.rekey` precedent
at `internal/core/builtins.go:93`):

```go
{Type: "crypto.system.operator_read",           Category: "system", Format: "audit", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_AUDIT_ONLY, Source: "builtin"},
{Type: "crypto.system.operator_read_completed", Category: "system", Format: "audit", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_AUDIT_ONLY, Source: "builtin"},
```

INV-F13 enforces both rows via the builtins golden test
(`DisplayTarget == EVENT_CHANNEL_AUDIT_ONLY` is the audit-only marker;
`Category` is the plain-string domain bucket — both fields are validated
by `VerbRegistration.registerNoLock` at `internal/core/registry.go:54-89`).
Event type naming mirrors rekey precedent (subject prefix
`events.<game>.system.<x>` maps to event type `crypto.system.<x>`).

### 3.9 Error codes (extends master spec §10)

| Code | Trigger | Maps to CLI exit |
| ---- | ------- | ---------------- |
| `DENY_AUDIT_PRE_DATA_PUBLISH` | Pre-data audit publish failed; no data streamed (INV-42) | 70 (EX_SOFTWARE; mirrors E's INV-E23 audit-failure mapping) |
| `DENY_OPERATOR_READ_WINDOW_TOO_LARGE` | `(until - since) > MaxWindow` | 65 |
| `DENY_OPERATOR_READ_JUSTIFICATION_EMPTY` | `trim(justification) == ""` | 65 |
| `DENY_OPERATOR_READ_JUSTIFICATION_TOO_LONG` | `len(justification) > 4096` | 65 |
| `DENY_OPERATOR_READ_CONTEXT_TYPE_UNKNOWN` | Context type not in `codec.TypeRegistry` | 65 |
| `DENY_OPERATOR_READ_CONTEXT_ARITY_MISMATCH` | `len(ids)` does not match `registration.Arity` | 65 |
| `DENY_OPERATOR_READ_CONTEXT_ID_MALFORMED` | An id in `ids` fails `registration.MatchID` | 65 |
| `DENY_OPERATOR_READ_CONTEXT_TOO_MANY` | More than 64 contexts | 65 |
| `DENY_OPERATOR_READ_TIME_INVERTED` | `until <= since` after defaulting | 65 |
| `DENY_OPERATOR_READ_FUTURE_BOUND` | `since` or `until > now + 5s` | 65 |
| `TYPE_REGISTRY_DUPLICATE` | Internal: duplicate registration at boot | server-fatal (not user-facing) |

Existing codes reused: `APPROVAL_NOT_FOUND`, `DENY_APPROVAL_EXPIRED`,
`APPROVAL_WAIT_DEADLINE`, `APPROVAL_WAIT_CANCELLED`,
`DENY_POLICY_HASH_UNKNOWN`, `DENY_TOTP_REQUIRED`, `DENY_OPERATOR_CAPABILITY`.

## Section 4 — Algorithms

### 4.1 Bounds resolution

```text
ResolveBounds(req, cfg, typeRegistry, now) → (resolved, requestedFlags, error):
  resolved.since = req.since   if set else (now - cfg.DefaultWindow)
  resolved.until = req.until   if set else now
  requestedFlags.sinceDefaulted = !req.since.Set
  requestedFlags.untilDefaulted = !req.until.Set

  reject DENY_OPERATOR_READ_TIME_INVERTED         if resolved.until <= resolved.since
  reject DENY_OPERATOR_READ_FUTURE_BOUND          if resolved.until > now + 5s
  reject DENY_OPERATOR_READ_WINDOW_TOO_LARGE      if (until - since) > cfg.MaxWindow

  reject DENY_OPERATOR_READ_CONTEXT_TOO_MANY      if len(req.contexts) > 64

  resolved.contexts = []
  for each c in req.contexts:
      registration, ok := typeRegistry.LookupType(c.Type)
      reject DENY_OPERATOR_READ_CONTEXT_TYPE_UNKNOWN   if !ok
      reject DENY_OPERATOR_READ_CONTEXT_ARITY_MISMATCH if len(c.IDs) != registration.Arity
      for each id in c.IDs:
          reject DENY_OPERATOR_READ_CONTEXT_ID_MALFORMED if !registration.MatchID(id)

      canonicalIDs := c.IDs
      if registration.OrderInsensitiveIDs:
          canonicalIDs = sortLex(canonicalIDs)
      append resolved.contexts: ContextRef{Type: c.Type, IDs: canonicalIDs}

  resolved.contexts = dedupe(resolved.contexts)

  reject DENY_OPERATOR_READ_JUSTIFICATION_EMPTY    if trim(req.justification) == ""
  reject DENY_OPERATOR_READ_JUSTIFICATION_TOO_LONG if len(req.justification) > 4096
```

### 4.2 INV-42 ordering (code-structural proof)

```go
func (h *Handler) Handle(ctx context.Context, req *AdminReadStreamRequest,
                        stream *connect.ServerStream[AdminReadStreamResponse]) error {

    op, err := h.auth.Authorize(ctx, req.SessionToken, "crypto.operator")
    if err != nil { return wrap(err) }
    resolved, reqFlags, err := h.resolveBounds(req, h.cfg, h.typeRegistry, h.clock.Now())
    if err != nil { return wrap(err) }

    var approval *approval.Approval
    if req.DualControl {
        approval, err = h.acquireApproval(ctx, op, req, resolved, stream)
        if err != nil { return h.emitTimeoutFinish(stream, err) }
    }

    requestID := core.NewULID()
    startPayload := buildStartPayload(op, approval, req, resolved, reqFlags,
                                      h.policyHash.Current(), requestID, h.clock.Now())

    // INV-42 anchor.
    if err := h.auditEmitter.EmitStart(ctx, startPayload, requestID); err != nil {
        return wrap(oops.Code("DENY_AUDIT_PRE_DATA_PUBLISH").Wrap(err))
    }

    if err := stream.Send(buildStartedFrame(requestID, resolved, startPayload)); err != nil {
        return h.emitCompleted(ctx, requestID, TerminatedClientDisconnect, 0, 0, err)
    }
    eventsScanned, decryptFailCount, streamErr := h.streamData(ctx, stream, resolved, requestID)

    completedPayload := buildCompletedPayload(requestID, classify(streamErr),
                                              eventsScanned, decryptFailCount, h.clock.Now())
    if cerr := h.auditEmitter.EmitCompleted(ctx, completedPayload, requestID); cerr != nil {
        h.logger.Warn("operator_read_completed audit emit failed",
            "request_id", requestID, "error", cerr)
        // INV-F10: not raised
    }

    return streamErr
}
```

### 4.3 Subject construction (no legacy form)

```go
// internal/admin/readstream/subjects.go

func subjectFor(c ContextRef, gameID string) eventbus.Subject {
    b := strings.Builder{}
    fmt.Fprintf(&b, "events.%s.%s", gameID, c.Type)
    for _, id := range c.IDs {
        b.WriteByte('.')
        b.WriteString(id)
    }
    b.WriteString(".>")
    return eventbus.Subject(b.String())
}

func buildSubjects(contexts []ContextRef, gameID string) []eventbus.Subject {
    if len(contexts) == 0 {
        return []eventbus.Subject{eventbus.Subject("events." + gameID + ".>")}
    }
    out := make([]eventbus.Subject, 0, len(contexts))
    for _, c := range contexts {
        out = append(out, subjectFor(c, gameID))
    }
    return out
}
```

Note: these are the HistoryReader query subjects (matching events the
operator wants to read). They are distinct from the audit-chain subject
(`events.<game>.system.operator_read.<request_id>`) which carries the
audit events themselves.

When `len(subjects) > 1`, `streamData` opens N `HistoryStream`s and
merges with a min-heap keyed by event timestamp.

### 4.4 Sensitive-content filter (INV-F15)

Discard rows where `events_audit.dek_ref IS NULL` (the identity-codec
indicator; matches `internal/eventbus/history/cold_postgres.go:421-428`'s
`!row.DEKRef.Valid` check). Public events do not count toward
`events_scanned` or `decrypt_fail_count`.

### 4.5 Decrypt-fail classifier (F-local; INV-F12)

The classifier is a pure mapping from existing production-surface errors
to expanded `NoPlaintextReason` values. Every branch grounds in a real
error code with a verified producer. Cold-tier semantics are unchanged.

```go
// internal/admin/readstream/classify.go

type Classifier interface {
    Classify(err error) (reason NoPlaintextReason, fatal bool)
}

// Classify maps an error from the cold-tier read path to a NoPlaintextReason.
// fatal=true bails the stream (ctx cancellation, unrecoverable). fatal=false
// produces a metadata-only frame stamped with the reason.
func (c *defaultClassifier) Classify(err error) (NoPlaintextReason, bool) {
    switch {
    case err == nil:
        return NO_PLAINTEXT_REASON_UNSPECIFIED, false

    case errors.Is(err, context.Canceled),
         errors.Is(err, context.DeadlineExceeded):
        return NO_PLAINTEXT_REASON_UNSPECIFIED, true   // fatal — bail

    case errors.Is(err, source.ErrMetadataOnly):
        // Resolver fallback exhausted; covers DEK_NOT_FOUND + DEK_DESTROYED
        // (the fallback resolver at internal/eventbus/history/source/fallback.go
        // collapses these into ErrMetadataOnly before surfacing to F).
        return NO_PLAINTEXT_REASON_STALE_DEK, false    // existing value

    case isOopsCode(err, "EVENTBUS_COLD_DEK_COLUMNS_MISSING"):
        return NO_PLAINTEXT_REASON_DEK_MISSING, false  // NEW

    case isOopsCode(err, "EVENTBUS_COLD_BAD_DEK_COLUMNS"):
        return NO_PLAINTEXT_REASON_DEK_BAD_COLUMNS, false  // NEW

    default:
        // Unknown — bug indicator. Emit WARN; surface as INTERNAL.
        return NO_PLAINTEXT_REASON_INTERNAL, false      // NEW (catch-all)
    }
}
```

Producer grounding (verified by `rg` 2026-05-12):

| Mapped value | Source | Producer location |
| ------------ | ------ | ----------------- |
| `STALE_DEK` (existing) | `source.ErrMetadataOnly` | `internal/eventbus/history/source/fallback.go` (collapses `DEK_NOT_FOUND`, `DEK_DESTROYED`) |
| `DEK_MISSING` (NEW) | `EVENTBUS_COLD_DEK_COLUMNS_MISSING` | `internal/eventbus/history/cold_postgres.go:422` |
| `DEK_BAD_COLUMNS` (NEW) | `EVENTBUS_COLD_BAD_DEK_COLUMNS` | `internal/eventbus/history/cold_postgres.go:430` |
| `INTERNAL` (NEW) | catch-all | bug surface; WARN-logged |

The streaming loop in `streamData`:

```text
for row in mergedStream:
    if !row.DEKRef.Valid: continue                  // INV-F15

    eventsScanned++

    plaintext, err := decryptUsing(row, h.dekMgr, h.history)
    if err != nil:
        reason, fatal := h.classifier.Classify(err)
        if fatal:
            return eventsScanned, decryptFailCount, err
        decryptFailCount++
        send buildEventFrame(row, nil, reason)
        continue

    send buildEventFrame(row, plaintext, NO_PLAINTEXT_REASON_UNSPECIFIED)
```

### 4.6 Per-frame write deadline

Per-frame, not total-duration. Default 30s, server-configurable.

### 4.7 Operator-disconnect handling

ConnectRPC surfaces operator-side close as `stream.Send → io.EOF` or
`ctx.Err() == context.Canceled`. Classified as
`TERMINATED_BY_CLIENT_DISCONNECT`; completion audit emitted with
accumulated counts. Mid-`WaitForApproval` disconnect → `APPROVAL_WAIT_CANCELLED`
→ same classification.

## Section 5 — CLI surface

### 5.1 Subcommand

```text
holomush admin read-stream
  --justification <text>           Required. 1..4096 bytes.
  [--context <type>[:<id1>[:<id2>...]]]   Repeatable. Zero entries = all contexts.
  [--since <RFC3339>]              Default: now - 1h
  [--until <RFC3339>]              Default: now
  [--dual-control]                 Require second-op approval
  [--admin-socket <path>]          Default: /var/run/holomush/admin.sock
  [--output <text|json|ndjson>]    Default: text
```

The colon separator in `--context` is a CLI parsing convention. The CLI
splits on `:` into `[type, id1, id2, ...]` and constructs the structured
`ContextRef` locally. Nothing colon-form crosses the wire.

### 5.2 Output rendering (default `text`)

```text
$ holomush admin read-stream \
    --context scene:01HZAVGE83MGFEXQQH5SP9NXKF \
    --since 2026-04-20T00:00:00Z --until 2026-04-25T00:00:00Z \
    --justification "Abuse investigation, ticket #1234"

# stderr:
read-stream: started request_id=01J0R6Y...  policy_hash=sha256:a1b2…f9  window=2026-04-20T00:00:00Z..2026-04-25T00:00:00Z  contexts=[scene/01HZAVG…]

# stdout:
01J0AVG6V…  2026-04-20T03:14:22Z  scene/01HZAVG…  scene.message            { "speaker": "01H…", "text": "Hello world" }
01J0AVG7K…  2026-04-20T03:14:58Z  scene/01HZAVG…  scene.message            { "speaker": "01H…", "text": "Hi back" }
01J0AVG8X…  2026-04-20T03:15:30Z  scene/01HZAVG…  scene.message            [redacted: STALE_DEK]
…

# stderr at end:
read-stream: audit recorded peercred uid=501 pid=42389
read-stream: finished  terminated_by=CLIENT_EOF  events_scanned=247  decrypt_fail_count=3  duration=4.2s
```

### 5.3 Dual-control variant

```text
$ holomush admin read-stream --dual-control --justification "…" --context scene:01H…

# stderr:
read-stream: pending approval  request_id=01J0R6Y…  expires_at=2026-05-12T17:42:33Z
read-stream: ask a second operator to run:  holomush admin approve 01J0R6Y…

# blocks via Repo.WaitForApproval; on approval, falls through
# on timeout:
read-stream: ERROR  dual control approval timed out  request_id=01J0R6Y…
$ echo $?
77
```

### 5.4 Exit-code mapping (sysexits.h)

| Exit | Reason |
| ---- | ------ |
| `0`  | `TERMINATED_BY_CLIENT_EOF` |
| `64` (`EX_USAGE`) | Local validation |
| `65` (`EX_DATAERR`) | Server-side validation (`DENY_OPERATOR_READ_*`) |
| `69` (`EX_UNAVAILABLE`) | Admin socket unreachable |
| `70` (`EX_SOFTWARE`) | `DENY_AUDIT_PRE_DATA_PUBLISH` (mirrors E's INV-E23) |
| `75` (`EX_TEMPFAIL`) | `TERMINATED_BY_DEADLINE_EXCEEDED` mid-stream |
| `77` (`EX_NOPERM`) | `DENY_TOTP_REQUIRED`, `DENY_OPERATOR_CAPABILITY`, `TERMINATED_BY_DUAL_CONTROL_TIMEOUT`, `APPROVAL_WAIT_DEADLINE`, `DENY_APPROVAL_EXPIRED` |
| `78` (`EX_CONFIG`) | `DENY_POLICY_HASH_UNKNOWN` |

## Section 6 — INV-F\* invariant catalog

| ID | Statement | Test layer |
| -- | --------- | ---------- |
| **INV-F1** | `AdminReadStream` MUST emit the `crypto.system.operator_read` audit event and observe a successful `OperatorReadAuditEmitter.EmitStart` ack BEFORE sending any `ReadStarted` or `Event` frame. | Unit + E2E `F-E4` |
| **INV-F2** | If the pre-data audit publish fails, `AdminReadStream` MUST return `DENY_AUDIT_PRE_DATA_PUBLISH` and MUST NOT invoke `HistoryReader.QueryHistory`. | Unit |
| **INV-F3** | `AdminReadStream` MUST reject with `DENY_OPERATOR_CAPABILITY` when the operator lacks `crypto.operator`, BEFORE any audit emit. | Unit + E2E `F-E11` |
| **INV-F4** | `OperatorReadAuthGuard.Permit` MUST return PERMIT unconditionally for any sensitive-event lookup within an `AdminReadStream` invocation. | Unit |
| **INV-F5** | The handler MUST NOT instantiate or call the runtime `AuthGuard` for in-stream event lookup (would re-trigger INV-43). | Unit (negative) |
| **INV-F6** | `(until - since) > MaxWindow` MUST return `DENY_OPERATOR_READ_WINDOW_TOO_LARGE` BEFORE pre-data audit emit. | Unit + E2E `F-E8` |
| **INV-F7** | `OperatorReadStartPayload` MUST persist both Requested-* (nullable, capturing defaulting) and Resolved-* (always populated) fields for since/until/contexts. | Unit (JSON round-trip) |
| **INV-F8** | `ReadStarted.request_id == OperatorReadStartPayload.RequestID == start event ID == OperatorReadCompletedPayload.RequestID`. | Unit + E2E `F-E12` |
| **INV-F9** | The `crypto.system.operator_read_completed` event's `prev_hash` MUST equal the recomputed self_hash of its corresponding `crypto.system.operator_read` start event. Both events share NATS subject `events.<game>.system.operator_read.<request_id>` (Path C); the chain primitive's `LoadEntriesByScope` returns the start as the predecessor naturally. Chain `SubjectPrefix` MUST start with `events.` (INV-E26). | Bus-integration + E2E `F-E12` |
| **INV-F10** | Completion-audit publish failure MUST NOT raise an error; data has been delivered and the pre-data audit is the integrity anchor. Failure MUST be logged at WARN and counted via `holomush_admin_readstream_completed_audit_failures_total`. | Unit |
| **INV-F11** | Dual-control: when `req.dual_control = true` and `GetByOpArgsHash` returns NOT_FOUND, the handler MUST send exactly one `PendingApproval` frame and MUST block via `Repo.WaitForApproval`. The handler MUST NOT introduce an in-process pending-approval registry. | Unit + E2E `F-E5`, `F-E6` |
| **INV-F12** | F's classifier (`classify.go::Classify`) MUST match its documented matrix (§4.5 table). Every branch corresponds to a production-verified error producer. Unknown errors MUST surface as `NO_PLAINTEXT_REASON_INTERNAL` with a WARN log. | Unit (classifier matrix table-test) |
| **INV-F13** | `crypto.system.operator_read` and `crypto.system.operator_read_completed` MUST be registered in `internal/core/builtins.go` with `DisplayTarget == corev1.EventChannel_EVENT_CHANNEL_AUDIT_ONLY` (mirrors `crypto.system.rekey` precedent at `builtins.go:93`). Both rows pass `VerbRegistration.registerNoLock` validation. | Unit (builtins golden test) |
| **INV-F14** | Per-frame write deadline (`WriteDeadline`, default 30s) MUST be enforced via `sendWithDeadline`; total stream duration MUST NOT be capped. | Unit + integration |
| **INV-F15** | `streamData` MUST NOT emit `Event` frames for events where `events_audit.dek_ref IS NULL`. Filtered events do NOT count toward `events_scanned` or `decrypt_fail_count`. | Unit + E2E `F-E14` |
| **INV-F16** | The `NoPlaintextReason` enum expansion (4 → 7) MUST preserve INV-GW-14 parity, AND the new values (`DEK_MISSING`, `DEK_BAD_COLUMNS`, `INTERNAL`) MUST NOT be stamped by `cold_postgres.go::decodeColdRow` or `history/dispatcher.go` — F's classifier is the only producer. | Unit (parity test + negative tests on hot/cold paths) |
| **INV-F17** | `approval.Repo.GetByOpArgsHash` MUST apply all filters server-side (`op_kind`, `op_args_hash`, `expires_at > now()`, `approved_at IS NOT NULL`, `primary_player_id != excludePlayerID`). Tiebreaker: most recently approved. | DB integration |
| **INV-F18** | Each `INV-F[N]` MUST be referenced in exactly one test name (e.g., `TestINV_F1_PreDataAuditOrdering`). | Meta-test |

### Coverage matrix

```text
INV-F1  → TestINV_F1_PreDataAuditOrdering                       (unit) + F-E4 (E2E)
INV-F2  → TestINV_F2_AuditPublishFailRefuses                    (unit)
INV-F3  → TestINV_F3_CapabilityCheckPrecedesAudit               (unit) + F-E11 (E2E)
INV-F4  → TestINV_F4_OperatorReadAuthGuardAlwaysPermits         (unit)
INV-F5  → TestINV_F5_StandardAuthGuardNeverSeesOperator         (unit, negative)
INV-F6  → TestINV_F6_WindowTooLargePrecedesAudit                (unit) + F-E8 (E2E)
INV-F7  → TestINV_F7_PayloadPreservesRequestedAndResolved       (unit)
INV-F8  → TestINV_F8_RequestIDCoherence                         (unit) + F-E12 (E2E)
INV-F9  → TestINV_F9_AuditChainLinksStartToCompletedSameSubject (bus-int) + F-E12
INV-F10 → TestINV_F10_CompletionAuditFailureNotRaised           (unit)
INV-F11 → TestINV_F11_DualControlBlocksViaWaitForApproval       (unit) + F-E5, F-E6
INV-F12 → TestINV_F12_ClassifierMatrix                          (unit, table-test)
INV-F13 → TestINV_F13_BuiltinEventTypesRegistered               (unit, builtins golden)
INV-F14 → TestINV_F14_PerFrameWriteDeadline                     (unit + integration)
INV-F15 → TestINV_F15_NonSensitiveEventsFiltered                (unit) + F-E14
INV-F16 → TestINV_F16_NoPlaintextReasonProtoGoParity            (unit, parity)
       → TestINV_F16_HotColdStampersDoNotEmitNewValues          (unit, negative on dispatcher/cold_postgres)
INV-F17 → TestINV_F17_GetByOpArgsHashMatrix                     (DB integration)
       → TestINV_F17_GetByOpArgsHashFiltersOwnAuthor            (DB integration)
INV-F18 → TestINV_F_MetaInvariantsBoundToTests                  (meta-test)
```

## Section 7 — Test strategy

### 7.1 Three-layer table

| Layer | Coverage |
| ----- | -------- |
| Unit | `ResolveBounds` validation matrix; `acquireApproval` via `WaitForApproval` + `GetByOpArgsHash` fixtures; `OperatorReadAuthGuard.Permit` unconditional behavior; audit payload JSON round-trip with `sha256:<hex>` encoding; `sendWithDeadline` deadline trip; `buildSubjects` direct construction; merge-stream ordering; `NoPlaintextReason` proto/Go parity; classifier matrix; `codec.TypeRegistry` register/lookup matrix; hex encoder byte-equivalence with `dek.encodeHash` |
| Bus-integration | `OperatorReadHandlerFor` registered in `chain.NewVerifierSubsystem`; chain advances start → completed under shared subject; INV-42 refusal on chain emit failure |
| DB integration | `GetByOpArgsHash` matrix (happy / expired / own-author / unapproved / tiebreaker); migration 000036 idempotency |
| E2E (Ginkgo) | Folded into `cmd/holomush/admin_authenticate_e2e_test.go` reusing `adminAuthEnv`; production-boot via `runCoreWithDeps` |

### 7.2 E2E scenarios

| ID | Scenario | INV anchor |
| -- | -------- | ---------- |
| `F-E1` | Happy path: single context, explicit bounds, 50 events all decrypt OK | scope/baseline |
| `F-E2` | Happy path: `contexts` omitted (whole-game wildcard via `events.<game>.>`), defaulted bounds (last 1h) | filter-default |
| `F-E3` | Mixed decrypt: 80 OK, 20 with destroyed-DEK (surfaces via `source.ErrMetadataOnly`) → 20 metadata-only frames with `NO_PLAINTEXT_REASON_STALE_DEK` | best-effort + classifier |
| `F-E4` | INV-42: inject audit-publish failure → no data frames sent, `DENY_AUDIT_PRE_DATA_PUBLISH`, exit 70 | INV-42 |
| `F-E5` | Dual-control happy: op A blocks on `PendingApproval`, op B `Approve`s, stream resumes, audit payload carries `approver_player_id` | dual-control |
| `F-E6` | Dual-control timeout → `ReadFinished{TERMINATED_BY_DUAL_CONTROL_TIMEOUT}`, exit 77 | dual-control timeout |
| `F-E7` | `TERMINATED_BY_CLIENT_DISCONNECT`: client cancels mid-stream → completion audit emitted, `events_scanned` < total | disconnect lifecycle |
| `F-E8` | Window-too-large rejection → `DENY_OPERATOR_READ_WINDOW_TOO_LARGE`, exit 65, no audit emitted | validation pre-audit |
| `F-E9` | Justification validation: empty after trim, > 4096 bytes (two sub-cases) | input validation |
| `F-E10` | Per-frame write-deadline: fixture stalls `stream.Send` > 30s → `TERMINATED_BY_DEADLINE_EXCEEDED` | deadline guard |
| `F-E11` | `crypto.operator` capability missing → `DENY_OPERATOR_CAPABILITY` | INV-43 / capability gate |
| `F-E12` | Chain verification: read the `operator_read` chain via `chain.Verifier`, assert `prev_hash` linkage between start and completed (both on shared subject) | INV-F9, INV-F8 |
| `F-E13` | Multi-context: two `--context` entries, k-way merge orders events by timestamp | multi-subject merge |
| `F-E14` | Sensitive-content filter: a public event (dek_ref NULL) is dropped silently | INV-F15 |
| `F-E15` | Context-arity validation: `dm` with one id → `DENY_OPERATOR_READ_CONTEXT_ARITY_MISMATCH`; `scene` with two ids → same | variable-arity validation |
| `F-E16` | Dual-control idempotent reuse: op A invokes twice with identical args after op B approves → second invocation reuses approval via `GetByOpArgsHash`, no second `PendingApproval` frame | INV-F17 |
| `F-E17` | Classifier surface coverage: seed cold tier with rows triggering `EVENTBUS_COLD_DEK_COLUMNS_MISSING` and `EVENTBUS_COLD_BAD_DEK_COLUMNS` (via test-side direct DB writes); operator-read sees frames with `DEK_MISSING` and `DEK_BAD_COLUMNS` reasons | INV-F12 / new enum producers |

### 7.3 Harness fold-in

Same as rev 3 — add a Describe block in `admin_authenticate_e2e_test.go`
reusing `adminAuthEnv`. `seedAdminReadStreamData` helper. F MUST NOT block
on `ph1l` (Ginkgo Ordered refactor).

### 7.4 Coverage discipline

- Per-package > 80% (CLAUDE.md threshold).
- `internal/admin/readstream/` > 90% (security-sensitive surface).
- `internal/eventbus/codec/typeregistry.go` > 90%.
- `internal/admin/approval/repo.go::GetByOpArgsHash` > 90%.

## Section 8 — Master spec amendments

| Master spec section | Change |
| ------------------- | ------ |
| §7.5 `AdminReadStream` flow | ADD: "Contexts MAY be omitted entirely (zero entries) for whole-window reads. Server-configured `MaxWindow` (default 30d) bounds the requested time window; server-configured `DefaultWindow` (default 1h) backfills missing `since`/`until`. Context refs are structured `(type, ids[])` on the wire; the CLI's colon-separated `type:id1:id2` form is a flag-parsing convention only." |
| §7.5 audit ordering | ADD: "A trailing `crypto.system.operator_read_completed` event is emitted at stream-end carrying `terminated_by`, `events_scanned`, `decrypt_fail_count`. Both audit events share the NATS subject `events.<game>.system.operator_read.<request_id>` and are differentiated by `Event.Type`. Failure to publish the trailing event MUST be logged but MUST NOT raise to the caller." |
| §7.5 decrypt-fail behavior | ADD: "Events that cannot be decrypted MUST stream as `MetadataOnly=true` `Event` frames with a typed `NoPlaintextReason`. Classification is operator-read-local via F's classifier; hot-tier subscribe and cold-tier history-reader stamp the existing 3 non-zero reasons unchanged." |
| §7.5 dual-control | ADD: "Dual-control wait MUST use `Repo.WaitForApproval` (D-shipped DB-backed blocking primitive). Idempotent reuse MUST use `Repo.GetByOpArgsHash` (added by F)." |
| §10 operator/policy errors | ADD rows: `DENY_AUDIT_PRE_DATA_PUBLISH`, `DENY_OPERATOR_READ_WINDOW_TOO_LARGE`, `DENY_OPERATOR_READ_JUSTIFICATION_EMPTY`, `DENY_OPERATOR_READ_JUSTIFICATION_TOO_LONG`, `DENY_OPERATOR_READ_CONTEXT_TYPE_UNKNOWN`, `DENY_OPERATOR_READ_CONTEXT_ARITY_MISMATCH`, `DENY_OPERATOR_READ_CONTEXT_ID_MALFORMED`, `DENY_OPERATOR_READ_CONTEXT_TOO_MANY`, `DENY_OPERATOR_READ_TIME_INVERTED`, `DENY_OPERATOR_READ_FUTURE_BOUND`, `TYPE_REGISTRY_DUPLICATE` |
| §10 invariant catalog | ADD INV-F1..INV-F18 from this spec's §6 |
| §10 NoPlaintextReason enum | EXPAND from 4 → 7 values (add `DEK_MISSING`, `DEK_BAD_COLUMNS`, `INTERNAL`). Update INV-GW-14 parity test expectations. New values are stamped exclusively by operator-read's F-local classifier. |
| §10 codec context registry | NEW SECTION: "`codec.TypeRegistry` registers sensitive context types. F ships built-ins (scene/dm/location/character). Plugin-declared types deferred to a follow-up epic that extends `crypto.emits` manifest schema with arity/match-id metadata." |

## Section 9 — Out of scope (recap)

- Bulk export tooling.
- Hot-tier reads.
- Tail / long-poll mode.
- Server-side redaction.
- Cross-game reads.
- Web UI surface.
- `--metadata-only` CLI flag.
- Event-type / facet filtering.
- Hot-tier dispatcher / live subscriber `NoPlaintextReason` stamping with new values.
- Cold-tier `decodeColdRow` semantic reshape.
- Plugin-declared sensitive context types (`crypto.emits` manifest schema extension).
- More granular `NoPlaintextReason` values beyond `DEK_MISSING`, `DEK_BAD_COLUMNS`, `INTERNAL` — additional granularity would require producer-side cold-tier or resolver-side changes outside F's scope.
- Plugin-emit-gate test for new `crypto.system.operator_read*` event types — a follow-up bead (rationale: the existing `crypto.emits` declaration gate at `internal/plugin/event_emitter.go::Emit` defaults to deny for unmanifested event types, but a dedicated reserved-namespace test is deferred).

## Section 10 — Prerequisites and dependencies

| Dep | Source | Status |
| --- | ------ | ------ |
| `OperatorAuthProvider` | D | Shipped |
| `admin_approvals` table | D, migration 000020 | Shipped |
| `Repo.WaitForApproval` | D, `internal/admin/approval/repo.go:189-224` | Shipped |
| `Repo.Open` / `OpenRequest{PrimaryPlayerID, OpKind, OpArgsHash}` | D, `internal/admin/approval/repo.go:48-61` | Shipped |
| `approval.ComputeOpArgsHash` | D, `internal/admin/approval/oparghash.go:20-27` | Shipped |
| `Repo.GetByOpArgsHash` | F (this spec, §2.7) | **NEW (F-touching D)** |
| `Approve` RPC | D, `internal/admin/approval/handler.go` | Shipped (untouched by F) |
| `internal/eventbus/audit/chain` primitive (7-field `Handler`) | E, `internal/eventbus/audit/chain/verifier.go:23-52` | Shipped |
| `chain.NewEmitter(repo)` (arity 1) | E, `internal/eventbus/audit/chain/emitter.go:38-41` | Shipped |
| `chain.NewVerifierSubsystem(VerifierSubsystemConfig{Handlers []Handler})` | E, `verifier_subsystem.go:52-91` | Shipped |
| Factory precedent: `dek.RekeyHandlerFor` + `policy.PolicySetHandlerFor` | E + D | Shipped |
| `sha256:<hex>` hash-string convention | E, `internal/eventbus/crypto/dek/audit.go:121-136` (package-private; F duplicates the encoder) | Shipped (helper not exported) |
| `chain.Repo.LoadEntriesByScope` shared-subject behavior | E, `internal/eventbus/audit/chain/repo_postgres.go:44-78` | Shipped (used as-is) |
| `HistoryQuery.Subject` (single `Subject`) | `internal/eventbus/bus.go:98` | Shipped |
| INV-39 `SourceResolver` / `FallbackResolver` | E §5 | Shipped (production path) |
| `HistoryReader` with `WithHistoryAuth` | history-reader-crypto-options design 2026-05-05 | Shipped |
| `NoPlaintextReason` enum (4 values) | `pkg/proto/holomush/core/v1/core.pb.go:52-63` | Shipped; F expands to 7 |
| `internal/eventbus/types_proto_sync_test.go` parity discipline (INV-GW-14) | Existing | Shipped; F updates expectations |
| `cold_postgres.go` fail-close error codes (`EVENTBUS_COLD_DEK_COLUMNS_MISSING`, `EVENTBUS_COLD_BAD_DEK_COLUMNS`) | `internal/eventbus/history/cold_postgres.go:421-430` | Shipped (F consumes; does NOT modify) |
| `source.ErrMetadataOnly` sentinel | `internal/eventbus/history/source/fallback.go` | Shipped (F consumes) |
| `codec.TypeRegistry` (built-ins only) | F (this spec, §2.6) | **NEW (F-introduced substrate)** |
| Production-boot E2E harness pattern | E `cmd/holomush/admin_authenticate_e2e_test.go` | Shipped |

## Section 11 — References

- Master spec: `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`
  §7.5 (`AdminReadStream` flow), §7.6 (decryption audit ordering), §10
  (error codes + invariant catalog).
  - INV-42 (verbatim): "`AdminReadStream` MUST emit its audit event before
    delivering any plaintext data; if audit emit fails, the call MUST refuse."
    (`docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md:328`).
  - INV-43 (verbatim): "The runtime AuthGuard MUST NEVER return PERMIT for a
    subject of kind `operator`; legitimate operator reads go through
    `AdminReadStream`." (`docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md:329`).
- Phase 5 decomposition: `docs/superpowers/specs/2026-05-07-event-payload-crypto-phase5-decomposition.md`.
- Sub-epic E design: `docs/superpowers/specs/2026-05-10-event-payload-crypto-phase5-sub-epic-e-design.md`.
- Sub-epic D design: `docs/superpowers/specs/2026-05-09-event-payload-crypto-phase5-sub-epic-d-design.md`.
- Sub-epic C design: `docs/superpowers/specs/2026-05-09-phase5-sub-epic-c-admin-socket-design.md`.
- History-reader crypto options: `docs/superpowers/specs/2026-05-05-history-reader-crypto-options-design.md`.
- Chain primitive code:
  - `internal/eventbus/audit/chain/chain.go` (`Chain`, `ValidateRegistration`
    enforcing INV-E26/INV-E27).
  - `internal/eventbus/audit/chain/verifier.go:23-52` (7-field `Handler`).
  - `internal/eventbus/audit/chain/emitter.go:38-41`
    (`NewEmitter(repo Repo) Emitter`).
  - `internal/eventbus/audit/chain/verifier_subsystem.go:52-91`
    (`NewVerifierSubsystem(VerifierSubsystemConfig{Handlers []Handler})`).
  - `internal/eventbus/audit/chain/repo_postgres.go:44-78`
    (`LoadEntriesByScope` shared-subject natural ordering).
- Factory precedents:
  - `internal/eventbus/crypto/dek/audit_chain.go::RekeyHandlerFor` +
    `RekeyAuditEmitter` (full 7-field Handler + audit-emitter pattern).
  - `internal/admin/policy/chain.go::PolicySetHandlerFor` (D's full Handler).
- Hash-string convention: `internal/eventbus/crypto/dek/audit.go:121-136`
  (`encodeHash` / `encodeHashPtr` — package-private; F duplicates locally).
- Blocking-wait substrate: `internal/admin/approval/repo.go::WaitForApproval`
  (lines 189–224).
- Canonical args hash: `internal/admin/approval/oparghash.go:20-27`
  (`ComputeOpArgsHash`).
- Approval OpenRequest shape: `internal/admin/approval/repo.go:48-61`.
- Existing `NoPlaintextReason` enum: `pkg/proto/holomush/core/v1/core.pb.go:52-63`
  (4 values); Go mirror `internal/eventbus/types.go`; parity test
  `internal/eventbus/types_proto_sync_test.go:74-91`.
- Classifier inputs:
  - Cold-tier fail-close codes:
    `internal/eventbus/history/cold_postgres.go:422-430`.
  - Resolver sentinel: `internal/eventbus/history/source/fallback.go`.
- Hot-tier dispatcher (NOT mirrored by F):
  `internal/eventbus/history/dispatcher.go:116-227`.
- E CLI exit-code precedent: `cmd/holomush/cmd_crypto_rekey.go:243-259`
  (`DEK_REKEY_PHASE7_AUDIT_FAILED → 70 EX_SOFTWARE`).
- INV-E26 (verbatim): "SubjectPrefix MUST start with `events.`."
  (`internal/eventbus/audit/chain/chain.go:62-71`).
- INV-E27 (verbatim): "ScopePayloadField MUST be non-empty."
  (`internal/eventbus/audit/chain/chain.go:72-77`).
- INV-E28 (verbatim): "self_hash = SHA-256(JCS_canonicalize(zero(payload, SelfHashFieldName)))"
  (`internal/eventbus/audit/chain/chain.go:13-19`).
- Plugin manifest (NOT extended by F): `internal/plugin/crypto_manifest.go:30-37`
  (`CryptoEmit{EventType, Sensitivity, Description}` — context-type metadata
  absent; deferred to follow-up epic).
- Event conventions: `.claude/rules/event-conventions.md`,
  `.claude/rules/event-interfaces.md`.
- Open follow-up beads from sub-epic E: `holomush-z51u` (INV meta-test
  discipline), `holomush-ph1l` (Ginkgo Ordered refactor), `holomush-tmrv`
  (Docker testcontainer startup flake).
