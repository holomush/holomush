# Phase 5 Sub-Epic F Implementation Plan — `AdminReadStream` + Pre-Data Audit + `read-stream` CLI

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **PLAN STATUS (revision 8, 2026-05-12):** The original 27-task plan (Tasks 0–26, materializing as 24 child beads under `holomush-jxo8.8`) was implemented to completion at bookmark `jxo8-f-target`. Post-implementation review surfaced an architectural error: F's coupling to `HistoryReader`/`dispatcher` was the root cause of the merge-source-drop bug and produced four compensation wrappers that should never have existed. **The 24-commit chain is abandoned.** This plan is amended with **§Revision 8 Supplement** (immediately below) listing the rewritten task set. See [ADR 0017](../../adr/0017-admin-readstream-bypasses-history-reader.md) for the decision and reasoning.

## Revision 8 Supplement (canonical task set)

**Decision reference:** [ADR 0017](../../adr/0017-admin-readstream-bypasses-history-reader.md). **Spec reference:** spec §0 (revision 8).

The r8 implementation is structured as ~14 commits. Many port verbatim from the abandoned chain; several are rewritten; a handful are deleted entirely.

### r8 task list

Numbering uses `R.<n>` prefix to distinguish from the original Task 0–26 set. Materialization happens via new child beads under `holomush-jxo8.8`.

**Substrate / shared infra (mostly verbatim from abandoned chain):**

- **R.1 [VERBATIM from old T1]** `NoPlaintextReason` proto enum expansion 4→7 + Go mirror + INV-F16 stamping-locality parity test. Cherry-pick old commit `kn` (15a). No functional change.
- **R.2 [VERBATIM from old T3]** `approval.Repo.GetByOpArgsHash` + migration 000036 + 5 INV-F17 integration tests. Cherry-pick old commit `zz` (ba). No functional change.
- **R.3 [VERBATIM from old T5]** Builtin event-type registrations (`crypto.system.operator_read`, `crypto.system.operator_read_completed`) + INV-F13. Cherry-pick old commit `vl` (74b). No functional change.

**F-local files (verbatim from abandoned chain):**

- **R.4 [VERBATIM from old T6]** F-local audit payload structs (`OperatorReadStartPayload`, `OperatorReadCompletedPayload`, `ContextRef`) + `encodeHash`/`encodeHashPtr` helpers + 4 unit tests (INV-F7). Cherry-pick old commit `vk` (640).
- **R.5 [VERBATIM from old T9]** Chain factories (`OperatorReadChainFor`, `OperatorReadHandlerFor`) + 5 unit tests (INV-F8, F9). Cherry-pick old commit `ull` (a3f).
- **R.6 [VERBATIM from old T10]** `OperatorReadAuditEmitter` (`EmitStart`, `EmitCompleted`) + chain prev-hash + `holomush_admin_readstream_completed_audit_failures_total` metric + 6 unit tests (INV-F10). Cherry-pick old commit `kz` (a31).
- **R.7 [VERBATIM from old T12]** `ResolveBounds` validation (`filter.go`) + `Request`/`Resolved`/`ResolvedFlags` types + 11 unit tests (INV-F6, F7). Cherry-pick old commit `wn` (4b7). Type registry replaced by hardcoded package map (see R.7a).
- **R.7a** Replace `codec.TypeRegistry` dependency in `filter.go` with a package-private map of the 4 sensitive context types (`scene`, `location`, `character`, `dm`). Delete `internal/eventbus/codec/typeregistry.go` and tests entirely.
- **R.8 [VERBATIM from old T13]** `buildSubjects` (`subjects.go`) + 4 unit tests. Cherry-pick old commit `tv` (a09). The k-way merge piece of old T13 is dropped (no merge in r8).
- **R.9 [VERBATIM from old T15]** `sendWithDeadline` shim (`deadline_writer.go`) + `ErrWriteDeadlineExceeded` + drain-after-deadline + client-disconnect detection + 3 unit tests (INV-F14). Cherry-pick from old commits `tv` (a09) + `rxv` (8a6) for the F.23 hardening additions.

**Net-new — F's own read/decrypt path:**

- **R.10** `internal/admin/readstream/cold_reader.go` — direct SQL against `events_audit`. Function `(c *coldReader).Read(ctx, query coldQuery) ([]ColdRow, error)` where `coldQuery` carries `Subjects []eventbus.Subject`, `Since, Until time.Time`. One paged SQL query: `SELECT ... FROM events_audit WHERE subject = ANY($1) AND dek_ref IS NOT NULL AND timestamp BETWEEN $2 AND $3 ORDER BY timestamp ASC`. Returns rows as `ColdRow{Subject, Type, Timestamp, Ciphertext, KeyID, KeyVersion, Codec, JsSeq}` — no decrypt, no envelope unmarshal beyond surface fields. ~150 LOC.
- **R.10-tests** Integration tests: multi-subject merge, time bounds, `dek_ref IS NOT NULL` filter, empty-result termination, ORDER BY timestamp.
- **R.11** `internal/admin/readstream/decrypt.go` — per-row decrypt + classifier. Function `decryptRow(ctx, row ColdRow, dekMgr dek.Manager, codecRegistry codec.Registry) (plaintext []byte, reason eventbus.NoPlaintextReason, fatal bool, err error)`. Inlines AAD construction (mirrors `dispatcher.go:179`), codec resolution (mirrors `:184`), DEK resolution. Unexported `classifyDecryptErr(err) (reason, fatal)` covers INV-F12's six-branch matrix. ~80 LOC + ~40 LOC classifier.
- **R.11-tests** Unit tests: 6-branch classifier matrix (nil, ctx.Canceled, ctx.DeadlineExceeded, `source.ErrMetadataOnly`, `EVENTBUS_COLD_DEK_COLUMNS_MISSING`, `EVENTBUS_COLD_BAD_DEK_COLUMNS`, default) + happy-path decrypt (real AAD + codec) + DEK-destroyed → `STALE_DEK` + AAD-tampered → `INTERNAL`.

**Net-new — wire shape correction:**

- **R.12** Amend `api/proto/holomush/admin/v1/read_stream.proto`: `AdminReadStreamResponse.Event` field's payload type changes from `eventbusv1.Event` to `corev1.EventFrame` (which has typed `metadata_only` + `no_plaintext_reason` fields). Run `task proto:gen`.
- **R.12-tests** Round-trip test that EventFrame's typed fields preserve through the wire.

**Net-new — handler (rewritten from old T16+T17+T18):**

- **R.13** `internal/admin/readstream/handler.go` — orchestration. Pseudocode:
  ```text
  Config {
    SessionStore, SubjectResolver  (auth deps, no SessionAuthorizer adapter)
    Approvals     approval.Repo
    ColdReader    *coldReader     (NEW: F's own reader)
    DEKMgr        dek.Manager     (NEW: passed to decrypt)
    CodecRegistry codec.Registry  (NEW: passed to decrypt)
    AuditEmitter  *OperatorReadAuditEmitter
    PolicyHash    string
    Clock         func() time.Time
    Logger        *slog.Logger
    Game          string
    MaxWindow, DefaultWindow, WriteDeadline, ApprovalTTL  time.Duration
  }

  handleInternal(ctx, req, stream):
    1. operator := authorize(ctx, req.SessionToken, "crypto.operator")   // inline 2-call
    2. resolved, flags, err := ResolveBounds(req, sensitiveTypes, Clock(), Default, Max)
    3. if req.DualControl: acquireApproval(ctx, operator, opArgsHash, stream)
    4. requestID := idgen.New(); payload := buildStartPayload(operator, resolved, flags, requestID, policyHash)
    5. AuditEmitter.EmitStart(ctx, payload, requestID)  → on err, DENY_AUDIT_PRE_DATA_PUBLISH (no frames)
    6. stream.Send(buildStartedFrame(requestID, resolved, payload))
    7. subjects := buildSubjects(resolved.Contexts, Game)
    8. rows, err := ColdReader.Read(ctx, coldQuery{subjects, resolved.Since, resolved.Until})
    9. eventsScanned, decryptFails := 0, 0
       for row in rows:
         plaintext, reason, fatal, err := decryptRow(ctx, row, DEKMgr, CodecRegistry)
         if fatal: term = classifyTerminator(err); break
         frame := buildEventFrame(row, plaintext, reason)   // EventFrame typed
         sendWithDeadline(ctx, stream.Send, frame, WriteDeadline)
         eventsScanned++; if reason != UNSPECIFIED: decryptFails++
    10. stream.Send(buildFinishedFrame(term, eventsScanned, decryptFails))
    11. AuditEmitter.EmitCompleted(ctx, completedPayload, requestID)  → on err, WARN + metric, return streamErr
  ```
  Production type is **closed** (no test-only fields). ~300 LOC.
- **R.13-tests** Handler invariant tests (INV-F1, F2, F3, F10, F11, F15). Tests use `testHandler` wrapper from `export_test.go` (see R.13a).
- **R.13a** `internal/admin/readstream/export_test.go` — `testHandler` wrapper exposing internal stages; `recordingStream` test impl of stream sender. Production Handler stays closed.

**Net-new — dual-control flow (R.13's `acquireApproval` step):**

- **R.13b [LOGIC from old T17]** `acquireApproval` inline in handler.go — `GetByOpArgsHash` idempotent reuse + `Open` + `WaitForApproval` + timeout path emits `ReadFinished{DUAL_CONTROL_TIMEOUT}`. 3 INV-F11 tests via `testHandler`.

**Net-new — production wiring (rewritten from old T21):**

- **R.14** `cmd/holomush/readstream_wiring.go` — construct `coldReader`, `OperatorReadAuditEmitter`, `Handler`. NO `history.NewReader` construction, NO `staleDEKColdResolver`, NO `operatorSessionAuthorizer` adapter (handler takes `SessionStore` + `SubjectResolver` directly). Extend existing `VerifierSubsystem.Handlers` with `OperatorReadHandlerFor(gameID)` (one in-place append). Add 4 `AdminSocketConfig` timing fields with defaults. WARN log on `MaxWindow > 90d`.
- **R.14-tests** Smoke test that wiring composes without err and `adminSocketCfg.ReadStreamHandler != nil`.

**ConnectRPC adapter + CLI (verbatim/amended):**

- **R.15 [VERBATIM from old T19+T20]** Socket-layer adapter (`AdminReadStreamConnectHandler` + subsystem registration). Cherry-pick old commit `norp` (b3a6).
- **R.16 [AMENDED from old T22]** CLI subcommand. Cherry-pick old commit `ko` (638), then amend the frame renderer to read `EventFrame.MetadataOnly` + `EventFrame.NoPlaintextReason` directly instead of inferring from `len(Payload) == 0`. Update 1 unit test that exercised the heuristic.

**E2E (verbatim/amended from old T23+T24+T25):**

- **R.17 [AMENDED]** Happy-path scenarios F-E1, F-E2, F-E13, F-E14, F-E17 + helper functions (`seedAdminReadStreamData`, `RunAdminReadStream`, `adminReadStreamView`). Cherry-pick old commit `vms` (fae). Two amendments: (a) helpers seed via direct `pgx.Pool` write to `events_audit` (no HistoryReader detour); (b) F-E17 reverts to originally-planned 5 metadata-only frames (DEK_MISSING × 2 + DEK_BAD_COLUMNS × 3) — the infinite-loop bug class no longer exists.
- **R.18 [VERBATIM from old T24]** Validation scenarios F-E4, F-E8, F-E9, F-E11, F-E15 + fault-injection seams. Cherry-pick old commit `rrw` (3e). One amendment: the `SessionAuthorizerWrapperForTest` and `ReadStreamAuditEmitterWrapperForTest` test-only seams move into `export_test.go` (no longer on production `CoreDeps`).
- **R.19 [AMENDED from old T25]** Lifecycle scenarios F-E3, F-E5, F-E6, F-E7, F-E10, F-E12, F-E16. Cherry-pick old commit `rxv` (8a6). Amendments: (a) `staleDEKColdResolver` deletion absorbed into R.11's `decrypt.go::classifyDecryptErr`; (b) F-E3 `StaleDEKColdResolver` wiring becomes direct path in decrypt.go; (c) drain-after-deadline + client-disconnect detection in `sendWithDeadline` already covered by R.9.

### Deleted from r1–r7

The following old tasks are deleted entirely and the corresponding code does not exist in r8:

| Old task | Old commit | Reason deleted |
| -------- | ---------- | -------------- |
| T0 `HistoryQuery.SensitiveOnly` substrate | `qq` (30a) | F bypasses HistoryReader; cold_reader's SQL filters `dek_ref IS NOT NULL` directly. (The field stays in `HistoryQuery` for other-consumer future use; the substrate change is harmless.) Actually KEPT in shared infra for non-F use; just F doesn't use it. |
| T2 `codec.TypeRegistry` + builtins | `mkv` (cf0) | 4 hardcoded types in `filter.go` package map. |
| T7 `OperatorReadAuthGuard` | `lnk` (154, partial) | F has no per-event auth. |
| T8 `NoOpDecryptAuditEmitter` | `lnk` (154, partial) | F has no per-decrypt audit. |
| T11 Decrypt-fail classifier matrix (`classify.go` + `Classifier` interface) | `zor` (153) | Substance moves to `decrypt.go::classifyDecryptErr` unexported function in R.11. |
| T14 k-way merge | `tx` (16) | No merge in r8 — one SQL handles multi-subject. |
| T26 INV-F meta-test | `uv` (8e1) | Bookkeeping, not safety; INV-F18 deleted. |

Plus partial deletions:
- `staleDEKColdResolver` + `isDEKMissingErr` from old T21 wiring (commit `nz` 258) — superseded by R.11's `decrypt.go`.
- `operatorSessionAuthorizer` adapter + `SessionAuthorizer` interface from old T21 wiring — superseded by R.13's inline 2-call.
- `streamDataOverride`, `responseSenderWrapper`, `SetResponseSenderWrapperForTest`, `SessionAuthorizerWrapperForTest`, `ReadStreamAuditEmitterWrapperForTest` test seams from old T16/T17/T21 — superseded by R.13a's `testHandler` wrapper.
- INV-F4, INV-F5, INV-F18 invariants — deleted (impl details of deleted abstractions / naming bookkeeping).

### Invariants in r8

13 substantive invariants remain (down from 18):

- **INV-F1** Pre-data audit ordering (audit-start publishes before any data frame).
- **INV-F2** Audit-publish failure refuses (no data flows on EmitStart err).
- **INV-F3** Capability check precedes audit (rejection emits zero audit rows).
- **INV-F7** Audit payload preserves `Requested-*` and `Resolved-*` distinction.
- **INV-F8** RequestID coherence (start.request_id == completed.request_id).
- **INV-F9** Audit chain links start → completed via prev_hash.
- **INV-F10** Completion-audit failure logs WARN + increments metric (does not raise).
- **INV-F11** Dual-control idempotent reuse via `GetByOpArgsHash` (excludes self-author).
- **INV-F12** Decrypt classifier matrix covers 6 branches.
- **INV-F13** Builtin event types registered.
- **INV-F14** Per-frame write deadline.
- **INV-F15** Cold-tier read filters to `dek_ref IS NOT NULL`.
- **INV-F16** `NoPlaintextReason` proto-Go parity + stamping-locality.
- **INV-F17** `GetByOpArgsHash` filter matrix (5 SQL predicates server-side).

**Deleted invariants:** F4 (guard-always-permits is impl detail of deleted null-object); F5 (standard-runtime-guard-never-seen is structurally impossible without a HistoryReader instance); F18 (meta-test enforces naming, not behavior).

### Migration shape

1. **jj abandon** the 24-commit chain at `jxo8-f-target` (preserved in op-log).
2. **Reset bookmark** `jxo8-f-target` to `main@origin`.
3. **Cherry-pick verbatim-portable commits** (R.1, R.2, R.3, R.4, R.5, R.6, R.7, R.8 portion, R.9, R.15, R.17, R.18, R.19 — some with the in-file amendments listed above).
4. **Write fresh** R.7a, R.10, R.11, R.12, R.13, R.13a, R.13b, R.14, R.16 (amendment).
5. **Run cumulative reviews + pr-prep** on the new ~14-commit chain.
6. **One PR.** Squash-merged to main.

---

## Original r1–r7 plan (preserved below for historical reference)

The remainder of this document is the original 27-task plan as written. Tasks marked **[SUPERSEDED by R.x]** below are no longer the canonical implementation guide — see the Revision 8 Supplement above. Tasks marked **[VERBATIM CHERRY-PICK]** port to r8 unchanged.

**Architecture:** Server-streaming ConnectRPC method on the existing admin UDS. Composes D's `OperatorAuthProvider` + `Repo.WaitForApproval` + new `Repo.GetByOpArgsHash`, E's `internal/eventbus/audit/chain` primitive via a new `OperatorReadHandlerFor` factory (Path C: shared NATS subject differentiated by `Event.Type`), and the existing `HistoryReader` with `WithHistoryAuth`. Four F-introduced substrate amendments land in scope: `NoPlaintextReason` proto enum expansion 4 → 7, `codec.TypeRegistry` (built-ins only), `approval.Repo.GetByOpArgsHash` + migration 000036, and `HistoryQuery.SensitiveOnly bool`.

**Tech Stack:** Go 1.22+, ConnectRPC server-streaming, protobuf, PostgreSQL (via `pgxpool`), NATS JetStream, Ginkgo/Gomega for E2E.

---

**Spec reference:** [`docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md`](../specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md) (rev 7).

**Parent epic:** `holomush-jxo8.8`.

**Master spec:** [`docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`](../specs/2026-04-25-event-payload-crypto-design.md) §7.5, §10, INV-42, INV-43.

---

## How to use this plan

This plan describes the **shape** of each task — files, interfaces, test names, expected behaviors, precedent references. It does **not** prescribe line-by-line Go code. The implementer is expected to:

1. Read the spec section listed on each task.
2. Read the cited precedent file:line locations to learn the exact API shapes.
3. Write the TDD red-phase test against the listed test name + behavior.
4. Run the test (it fails).
5. Implement the minimal code to make it pass.
6. Run the test (it passes).
7. Run `task lint` + `task test` for the package.
8. Commit with the listed message.

The plan's job is to **scope, name, and order** the work. The implementer's job is to write the code against the substrate as it actually exists.

---

## Architecture & components touched

| Layer | File | Action | Notes |
| ----- | ---- | ------ | ----- |
| Proto | `api/proto/holomush/core/v1/core.proto` | MODIFY | Expand `NoPlaintextReason` enum 4 → 7 |
| Proto | `api/proto/holomush/admin/v1/admin.proto` | MODIFY | Add `AdminReadStream` rpc |
| Proto | `api/proto/holomush/admin/v1/read_stream.proto` | CREATE | Request/response + `ContextRef` |
| Substrate | `internal/eventbus/bus.go` | MODIFY | Add `HistoryQuery.SensitiveOnly bool` |
| Substrate | `internal/eventbus/history/cold_postgres.go` | MODIFY | Cold-tier SQL append `AND dek_ref IS NOT NULL` when `SensitiveOnly` |
| Substrate | `internal/eventbus/history/hot_jetstream.go` | MODIFY | Hot-tier skip non-sensitive at decode boundary |
| Substrate | `internal/eventbus/types.go` | MODIFY | Go-side `NoPlaintextReason` mirror |
| Substrate | `internal/eventbus/types_proto_sync_test.go` | MODIFY | INV-GW-14 parity update |
| Substrate | `internal/eventbus/codec/typeregistry.go` | CREATE | `TypeRegistry` + `TypeRegistration` + built-ins |
| Substrate | `internal/eventbus/codec/typeregistry_test.go` | CREATE | Coverage |
| Substrate | `internal/admin/approval/repo.go` | MODIFY | Add `GetByOpArgsHash` to `Repo` + Postgres impl |
| Substrate | `internal/admin/approval/repo_integration_test.go` | MODIFY | Five new integration tests |
| Substrate | `internal/store/migrations/000036_admin_approvals_op_args_hash_idx.up.sql` | CREATE | Composite index |
| Substrate | `internal/store/migrations/000036_admin_approvals_op_args_hash_idx.down.sql` | CREATE | Index drop |
| Core | `internal/core/builtins.go` | MODIFY | Register two new event types |
| F-local | `internal/admin/readstream/audit_payload.go` | CREATE | Payload structs + local `encodeHash`/`encodeHashPtr` |
| F-local | `internal/admin/readstream/auth_guard.go` | CREATE | `OperatorReadAuthGuard` (PERMIT always) |
| F-local | `internal/admin/readstream/decrypt_audit_emitter.go` | CREATE | `NoOpDecryptAuditEmitter` |
| F-local | `internal/admin/readstream/chain.go` | CREATE | `OperatorReadChainFor` + `OperatorReadHandlerFor` (7 callbacks) |
| F-local | `internal/admin/readstream/audit_emitter.go` | CREATE | `OperatorReadAuditEmitter` (EmitStart + EmitCompleted) |
| F-local | `internal/admin/readstream/classify.go` | CREATE | Classifier matrix |
| F-local | `internal/admin/readstream/filter.go` | CREATE | `ResolveBounds` |
| F-local | `internal/admin/readstream/subjects.go` | CREATE | `subjectFor` + `buildSubjects` |
| F-local | `internal/admin/readstream/stream_merge.go` | CREATE | k-way min-heap merge |
| F-local | `internal/admin/readstream/deadline_writer.go` | CREATE | `sendWithDeadline` |
| F-local | `internal/admin/readstream/handler.go` | CREATE | `Handler`, `Config`, `Handle` |
| F-local tests | `internal/admin/readstream/*_test.go` | CREATE | One per file |
| Wiring | `internal/admin/socket/read_stream_handler.go` | CREATE | ConnectRPC adapter |
| Wiring | `internal/admin/socket/subsystem.go` | MODIFY | `Config.ReadStreamHandler` |
| Wiring | `cmd/holomush/core.go` | MODIFY | `runCoreWithDeps` wiring |
| CLI | `cmd/holomush/cmd_admin_read_stream.go` | CREATE | CLI subcommand |
| E2E | `cmd/holomush/admin_authenticate_e2e_test.go` | MODIFY | Add `Describe("AdminReadStream")` + helper |

## Verified substrate APIs (precedent table)

The implementer MUST use these real signatures. They were verified against current `main` during plan-rev-4 substrate grounding.

| Need | API | Precedent location |
| ---- | --- | ------------------ |
| Operator auth | `sessions.GetOperatorSession(token)` + `access.HasPlayerGrant(ctx, grants, playerID, "crypto.operator")` 2-call pattern | `internal/admin/socket/rekey_handler.go:438-457` |
| Auth-guard interface | `eventbus.SessionAuthGuard{ Check(ctx, SessionCheckRequest) (SessionDecision, error) }` with `SessionDecision{Permit bool, GrantID ulid.ULID}` | `internal/eventbus/subscriber_auth.go:46-67` |
| Decrypt-audit emitter | `eventbus.SessionAuditEmitter{ EmitPluginDecrypt(ctx, PluginDecryptRecord) error }` | `internal/eventbus/subscriber_auth.go:75-94` |
| Approval lookup | `approval.Repo` interface methods + `OpenRequest{PrimaryPlayerID, OpKind, OpArgsHash}` (Open mints request_id) | `internal/admin/approval/repo.go:19-61` |
| Approval blocking wait | `Repo.WaitForApproval(ctx, RequestID, deadline)` returns `(Approval, error)` | `internal/admin/approval/repo.go:189-224` |
| Canonical op-args hash | `approval.ComputeOpArgsHash(proto.Message) ([]byte, error)` | `internal/admin/approval/oparghash.go:20-27` |
| Event publish | `eventbus.Publisher.Publish(ctx, Event) error` (single Event arg) | `internal/eventbus/bus.go:15-17` |
| Event construction | `eventbus.NewEvent(subject, type, actor, payload []byte) Event` | `internal/eventbus/types.go:185-194` |
| Event struct shape | `Event{ID ulid.ULID, Subject, Type, Timestamp time.Time, Actor, Payload, Sensitive, MetadataOnly, NoPlaintextReason}` — no `DEKRef` field; no `OccurredAt` (use `Timestamp`) | `internal/eventbus/types.go:121-174` |
| Actor kinds | `ActorKindUnknown / Character / Player / System / Plugin` (host-emit uses `ActorKindSystem` — NO `ActorKindHost`) | `internal/eventbus/types.go:62-71` |
| Audit chain — Chain meta | `chain.Chain{SubjectPrefix, SelfHashField, PrevHashField, ScopePayloadField}` | `internal/eventbus/audit/chain/chain.go:38-57` |
| Audit chain — Handler bundle (7 fields) | `chain.Handler{Chain, SubjectFor, ScopeFromSubject, ScopeFromPayload, Canonicalize, PrevHashOf, SelfHashOf}` | `internal/eventbus/audit/chain/verifier.go:23-52` |
| Audit chain — Emitter | `chain.NewEmitter(repo Repo) Emitter`; `Emitter.ComputePrevHashFor(ctx, Handler, scope) ([]byte, *ulid.ULID, error)` | `internal/eventbus/audit/chain/emitter.go:38-41` |
| Audit chain — VerifierSubsystem | `NewVerifierSubsystem(Config{Repo, Handlers []Handler, Logger})`; NO `Register` method | `internal/eventbus/audit/chain/verifier_subsystem.go:52-91` |
| Audit chain — `sha256:<hex>` encoder | `dek.encodeHash` (package-private; F duplicates) | `internal/eventbus/crypto/dek/audit.go:121-136` |
| Audit chain — rekey factory precedent | `dek.RekeyHandlerFor(gameID) chain.Handler` (all 7 fields) | `internal/eventbus/crypto/dek/audit_chain.go::RekeyHandlerFor` |
| Audit chain — policy factory precedent | `policy.PolicySetHandlerFor(gameID) chain.Handler` (all 7 fields) | `internal/admin/policy/chain.go::PolicySetHandlerFor` |
| Verb registration | `core.VerbRegistration{Type, Category, Format, Label, DisplayTarget, MetadataKeys, Source}` — `Category` is a plain string ("system"); `DisplayTarget` is the AUDIT_ONLY marker | `internal/core/registry.go:14-30` |
| Verb registration precedent | `crypto.system.rekey` builtin row | `internal/core/builtins.go:93` |
| Cold-tier fail-close codes | `EVENTBUS_COLD_DEK_COLUMNS_MISSING`, `EVENTBUS_COLD_BAD_DEK_COLUMNS` | `internal/eventbus/history/cold_postgres.go:421-430` |
| Resolver sentinel | `source.ErrMetadataOnly` (collapses `DEK_NOT_FOUND` / `DEK_DESTROYED`) | `internal/eventbus/history/source/fallback.go` |
| HistoryReader options | `history.WithHistoryAuth(SessionAuthGuard, SessionDEKManager, SessionAuditEmitter)` | `internal/eventbus/history/tier.go:184-208` |
| Approval RequestID type | `type RequestID [16]byte` — construct via `ulid.MustParse(...)` then convert; do NOT cast a string literal | `internal/admin/approval/types.go:13` |
| Rekey CLI exit-code precedent | `DEK_REKEY_PHASE7_AUDIT_FAILED → 70 EX_SOFTWARE` | `cmd/holomush/cmd_crypto_rekey.go:243-259` |
| Rekey ConnectRPC adapter test precedent | `internal/admin/socket/rekey_connect_handler_test.go` (httptest.Server + adminv1connect client) | — |
| Verb registry symbol name | `core.NewVerbRegistry()` + `registerBuiltinTypes(r, hostVersion)` + lookup helper on `*VerbRegistry` | `internal/core/registry.go:122-154` |

## Test discipline

- TDD: write the failing test first, run it, see it fail, implement minimal code, run it, see it pass, commit.
- All tests live next to the file under test.
- Unit tests use stdlib `testing` + `testify/require`. E2E uses Ginkgo/Gomega behind `//go:build integration`.
- INV-F\* tests are named `TestINV_F<N>_<Description>` per the coverage matrix in spec §6.
- All tests run via `task test`; integration via `task test:int`. Never invoke `go test` directly.
- The implementer compiles incrementally; the Go compiler catches symbol/type mismatches against the substrate.

## Invariant → Test → Task mapping (INV-F catalog from spec §6)

Every invariant in spec §6 MUST have at least one named test AND at least one task that implements it. The meta-test `TestINV_F_MetaInvariantsBoundToTests` enforces the one-to-one INV → test-name binding (INV-F18) post-impl.

| INV | Property — what MUST always be true (or never happen) | Defending test(s) | Implementing task(s) | Layer |
| --- | ----------------------------------------------------- | ----------------- | -------------------- | ----- |
| **F1** | Pre-data audit `EmitStart` MUST observe a successful publish-ack BEFORE any `ReadStarted` or `Event` frame leaves the stream. | `TestINV_F1_PreDataAuditOrdering` | T16 + E2E F-E4 (T24) | unit + E2E |
| **F2** | Pre-data audit publish failure MUST return `DENY_AUDIT_PRE_DATA_PUBLISH` AND MUST NOT invoke `HistoryReader.QueryHistory`. | `TestINV_F2_AuditPublishFailRefuses` | T16 | unit |
| **F3** | Missing `crypto.operator` capability MUST return `DENY_OPERATOR_CAPABILITY` BEFORE any audit emit; audit MUST NOT fire on rejection. | `TestINV_F3_CapabilityCheckPrecedesAudit` | T16 + E2E F-E11 (T24) | unit + E2E |
| **F4** | `OperatorReadAuthGuard.Check` MUST return `Permit=true` for every input, with nil error. | `TestINV_F4_OperatorReadAuthGuardAlwaysPermits` | T7 | unit |
| **F5** | The standard runtime `AuthGuard` MUST NEVER be wired into F's `HistoryReader` (re-triggers INV-43). F's wiring uses `*OperatorReadAuthGuard` exclusively. | `TestINV_F5_StandardAuthGuardNeverSeesOperator` | T16 (negative) | unit |
| **F6** | `(until - since) > MaxWindow` MUST return `DENY_OPERATOR_READ_WINDOW_TOO_LARGE` BEFORE pre-data audit emit. | `TestINV_F6_WindowTooLargeRejected` + E2E F-E8 | T12 + E2E (T24) | unit + E2E |
| **F7** | `OperatorReadStartPayload` MUST persist both Requested-\* (nullable, capturing defaulting) AND Resolved-\* (always populated) fields for since/until/contexts. | `TestINV_F7_PayloadPreservesRequestedAndResolved` (folds into T6 JSON round-trip + T12 default-flagging) | T6 + T12 | unit |
| **F8** | The `request_id` value MUST be IDENTICAL across: wire `ReadStarted.request_id`, audit `OperatorReadStartPayload.RequestID`, audit start event ID, audit `OperatorReadCompletedPayload.RequestID`. | `TestINV_F8_RequestIDCoherence` + E2E F-E12 | T16 + E2E (T25) | unit + E2E |
| **F9** | The `crypto.system.operator_read_completed` event's `prev_hash` MUST equal the recomputed self-hash of its matching `crypto.system.operator_read` start event. Chain prefix MUST start with `events.` (INV-E26). | `TestINV_F9_AuditChainLinksStartToCompletedSameSubject` + E2E F-E12 | T9 + T10 + E2E (T25) | bus-int + E2E |
| **F10** | Completion-audit publish failure MUST NOT raise an error to the caller; data has been delivered. Failure MUST be logged WARN + metric'd. | `TestINV_F10_CompletionAuditFailureNotRaised` | T10 + T16 | unit |
| **F11** | Dual-control with no fresh approval: handler MUST send exactly one `PendingApproval` frame AND block via `Repo.WaitForApproval`. Handler MUST NOT introduce an in-process pending-approval registry. | `TestINV_F11_DualControlBlocksUntilApproval`, `TestINV_F11_DualControlIdempotentReuse` | T17 + E2E F-E5/F-E6 (T25) | unit + E2E |
| **F12** | F's classifier MUST match the documented matrix in spec §4.5 exactly. Unknown errors MUST surface as `NO_PLAINTEXT_REASON_INTERNAL`. Classifier MUST NOT modify cold-tier semantics. | `TestINV_F12_ClassifierMatrix` | T11 | unit |
| **F13** | Both new event types MUST be registered in `internal/core/builtins.go` with `DisplayTarget == EVENT_CHANNEL_AUDIT_ONLY` and pass `VerbRegistration.registerNoLock` validation. | `TestINV_F13_BuiltinEventTypesRegistered` | T5 | unit |
| **F14** | Per-frame write deadline MUST be enforced via `sendWithDeadline`. Total stream duration MUST NOT be capped. | `TestINV_F14_PerFrameWriteDeadline` + E2E F-E10 | T15 + T18 + E2E (T25) | unit + E2E |
| **F15** | F MUST set `HistoryQuery.SensitiveOnly=true` on every cold-tier query. Identity-codec rows MUST NEVER reach the operator's stream. Filtered events MUST NOT count toward `events_scanned` or `decrypt_fail_count`. | `TestINV_F15_HandlerSetsSensitiveOnlyTrue` + `TestHistoryQuery_SensitiveOnly_ColdFiltersDekRefNull` + E2E F-E14 | T0 + T18 + E2E (T23) | unit + bus-int + E2E |
| **F16** | `NoPlaintextReason` proto/Go parity MUST hold across the 4→7 expansion (INV-GW-14). The three new values MUST NEVER be stamped by `cold_postgres.go::decodeColdRow` or `history/dispatcher.go` — F's classifier is the only producer. | `TestINV_F16_NoPlaintextReasonProtoGoParity`, `TestINV_F16_HotColdStampersDoNotEmitNewValues` (negative) | T1 | unit |
| **F17** | `approval.Repo.GetByOpArgsHash` MUST apply ALL filters server-side. Tiebreaker MUST be most-recently-approved. | `TestINV_F17_GetByOpArgsHash*` (5 cases) | T3 | DB int |
| **F18** | Every `INV-F[N]` MUST be referenced in exactly one test name. | `TestINV_F_MetaInvariantsBoundToTests` | T26 | meta |

**Reading guide:** When a task lists a `TestINV_F<N>_*` in its TDD acceptance criteria, look up that INV row above for the full property statement. When implementing a task, the test name is the contract; the property statement is the rationale.

## Per-task validation discipline

Every task in §Decomposition & sequencing below MUST end in this validation gate:

```text
Validation of done:
  ✓ Every named TestINV_F* / Test*_* in this task's TDD acceptance section passes.
  ✓ `task lint` clean on the touched packages.
  ✓ `task test -- ./<touched-package>/` green (no regressions in adjacent tests).
  ✓ For substrate tasks (T0, T1, T3): `task test:int -- ./<touched-package>/` green.
  ✓ For F-local tasks: per-package coverage ≥ 90% (spec §7.4 readstream + typeregistry threshold).
  ✓ For other touched packages: coverage ≥ 80% (CLAUDE.md baseline).
  ✓ No pre-existing test in the touched package newly fails (cross-package regression check).
```

This is the universal definition of "done" for each task. Per-task MUST PASS / MUST NEVER blocks (below) state task-specific positive and negative assertions on top of this baseline.

## Decomposition & sequencing

27 tasks in dependency order. Each task is a single TDD cycle that produces a passing build.

---

### Task 0: `HistoryQuery.SensitiveOnly` substrate

**Files:**

- Modify: `internal/eventbus/bus.go` (add field)
- Modify: `internal/eventbus/history/cold_postgres.go` (SQL append)
- Modify: `internal/eventbus/history/hot_jetstream.go` (decode-boundary skip)
- Create: `internal/eventbus/history/sensitive_only_test.go` (build tag `//go:build integration`)

**Goal:** Add the `SensitiveOnly` field to `HistoryQuery` so cold-tier queries can filter identity-codec rows server-side and hot-tier reads can skip non-sensitive events at the decode boundary. Default false preserves all existing callers; F is the sole setter at handler-construction time.

**Spec reference:** §2.8 + INV-F15.

**TDD acceptance criteria:**

- `TestHistoryQuery_SensitiveOnly_ColdFiltersDekRefNull` — bus-integration. Seed cold tier with one encrypted row + one identity-codec row; assert only the encrypted row is returned when `SensitiveOnly=true`.
- `TestHistoryQuery_SensitiveOnly_DefaultPreservesPublicRows` — same fixture, `SensitiveOnly=false` (default); both rows returned.
- `TestHistoryQuery_SensitiveOnly_HotSkipsNonSensitive` — hot-tier integration. Encrypted + identity-codec events on the same subject; assert hot tier returns only the encrypted one with `SensitiveOnly=true`.

**Implementation notes:**

- Cold-tier SQL: locate the query-builder around `internal/eventbus/history/cold_postgres.go:135-183` (the `SELECT ... FROM events_audit WHERE` builder). Append `AND dek_ref IS NOT NULL` when `q.SensitiveOnly`. The predicate composes with existing WHERE clauses; no migration needed.
- Hot-tier: the decoded `eventbus.Event.Sensitive` field is host-internal. The hot-tier reader builds events in `decodeJetStreamMessage` (~line 499 of `hot_jetstream.go`). F's amendment EITHER (a) stamps `Sensitive=true` when codec is non-identity at that build site OR (b) checks the codec name directly at the skip site. Pick (a) — it gives a real field semantic to depend on (today the cold-tier comment "Sensitive=false on cold reads" remains accurate; F's hot-tier amendment adds the encrypted-row stamping, documented in the field's godoc as the F-introduced hot-tier semantic).
- Reuse existing cold-tier seed helpers (mirror `cold_postgres_test.go` fixtures for the encrypted/identity-codec seed pattern).

**MUST PASS:**

- All three `TestHistoryQuery_SensitiveOnly_*` tests pass with the cold-tier SQL filter active.
- `eventbus.HistoryQuery` struct exposes the new `SensitiveOnly bool` field.
- With `SensitiveOnly=false` (default / zero value), behavior is byte-identical to before this task.

**MUST NEVER:**

- A `SensitiveOnly=true` query MUST NEVER return an identity-codec row from the cold tier.
- A `SensitiveOnly=true` query MUST NEVER return a hot-tier event with `Sensitive=false`.
- The substrate change MUST NEVER alter behavior for any caller that doesn't set `SensitiveOnly=true`.
- This task MUST NEVER require a DB migration (`dek_ref` column already exists).

**Validation of done:**

- Universal gate (above) PASS.
- `task test:int -- -run TestHistoryQuery_SensitiveOnly ./internal/eventbus/history/` PASS.
- `task test:int -- ./internal/eventbus/history/` PASS (zero regressions in pre-existing tests).
- `eventbus.Event.Sensitive` godoc updated to document that hot-tier reads now stamp `Sensitive=true` for encrypted rows after this task. Cold-tier doc stays accurate.

**Commit message:** `feat(eventbus): HistoryQuery.SensitiveOnly substrate for sub-epic F INV-F15`

---

### Task 1: Expand `NoPlaintextReason` proto enum + Go mirror + parity test

**Files:**

- Modify: `api/proto/holomush/core/v1/core.proto` (add 3 enum values)
- Modify: `internal/eventbus/types.go` (Go-side mirror)
- Modify: `internal/eventbus/types_proto_sync_test.go` (extend parity test + add INV-F16 negative test)

**Goal:** Expand `NoPlaintextReason` 4 → 7 with `DEK_MISSING`, `DEK_BAD_COLUMNS`, `INTERNAL`. INV-GW-14 parity test extends to cover all 7 values. INV-F16 negative half asserts the new values are not stamped by hot/cold tier stampers.

**Spec reference:** §3.4 + INV-F16.

**TDD acceptance criteria:**

- `TestINV_F16_NoPlaintextReasonProtoGoParity` — table-test asserts every proto enum value (UNSPECIFIED, AUTHGUARD_DENY, STALE_DEK, AUDIT_QUEUE_FULL, DEK_MISSING, DEK_BAD_COLUMNS, INTERNAL) has a matching Go constant with the same integer value, and vice-versa.
- `TestINV_F16_HotColdStampersDoNotEmitNewValues` — static-analysis-style: read source of `internal/eventbus/history/cold_postgres.go`, `internal/eventbus/history/dispatcher.go`, `internal/eventbus/subscriber.go`; assert none of the three contains the Go constant names `NoPlaintextReasonDEKMissing`/`NoPlaintextReasonDEKBadColumns`/`NoPlaintextReasonInternal` or the proto constants `NO_PLAINTEXT_REASON_DEK_MISSING`/etc. The three new values are stamped exclusively by F's classifier.

**Implementation notes:**

- Run `task proto:gen` after editing the proto. Verify `pkg/proto/holomush/core/v1/core.pb.go` updates.
- Use the existing parity-test pattern at `internal/eventbus/types_proto_sync_test.go:74-91`.
- For the negative test, use `os.ReadFile` + `strings.Contains`; locate the repo root via a `runtime.Caller`-walking helper or an existing test harness convention.

**MUST PASS:**

- The proto enum has exactly 7 values (0..6) post-task; positive INV-F16 parity test enumerates all seven against their Go mirrors.
- The negative INV-F16 test passes: a literal-string grep across `cold_postgres.go`, `dispatcher.go`, `subscriber.go` finds ZERO matches for the three new constant names (Go form OR proto form).

**MUST NEVER:**

- The three new values MUST NEVER be referenced from `internal/eventbus/history/cold_postgres.go`, `internal/eventbus/history/dispatcher.go`, or `internal/eventbus/subscriber.go`. F's classifier (T11) is the only producer.
- Pre-existing 4-value enum mappings MUST NEVER change ordinal value (0..3 stay stable). New values are 4..6.
- INV-GW-14 parity discipline MUST NEVER be broken: every proto value has a Go twin, every Go value has a proto twin.

**Validation of done:**

- Universal gate PASS.
- `task test -- -run TestINV_F16 ./internal/eventbus/` PASS.
- `task test -- ./internal/eventbus/...` PASS (no regressions).
- `task proto:gen` ran clean; generated bindings reflect 7 enum values.

**Commit message:** `feat(crypto): expand NoPlaintextReason enum 4→7 + INV-F16 stamping-locality (sub-epic F)`

---

### Task 2: `codec.TypeRegistry` package with built-in sensitive context types

**Files:**

- Create: `internal/eventbus/codec/typeregistry.go`
- Create: `internal/eventbus/codec/typeregistry_test.go`

**Goal:** New host-side closed-set registry of sensitive context types. Built-ins only (scene=arity1, location=arity1, character=arity1, dm=arity2-order-insensitive). Plugin-declared types deferred to a follow-up epic.

**Spec reference:** §2.6.

**TDD acceptance criteria:**

- `TestTypeRegistry_RegisterAndLookup` — Register a type, look it up, assert fields round-trip.
- `TestTypeRegistry_RegisterDuplicate` — Second Register of same name returns oops code `TYPE_REGISTRY_DUPLICATE`.
- `TestTypeRegistry_LookupUnknown` — Unknown name returns `(_, false)`.
- `TestRegisterBuiltinTypes_PopulatesFour` — After `RegisterBuiltinTypes(r)`, all four names lookup; `dm` has `Arity=2` and `OrderInsensitiveIDs=true`; the other three have `Arity=1` and `OrderInsensitiveIDs=false`.
- `TestRegisterBuiltinTypes_RejectsDoubleCall` — Calling twice on the same registry returns the duplicate-registration error from the second call.

**Implementation notes:**

- Surface: `TypeRegistry{Register(name, TypeRegistration) error; LookupType(name) (TypeRegistration, bool)}`. `TypeRegistration{Arity int, MatchID func(string) bool, OrderInsensitiveIDs bool}`.
- Backed by `sync.RWMutex`-protected map.
- `RegisterBuiltinTypes(r)` is a separate exported function so the host can call it once at wiring time. Plugin hook is OUT of scope.
- ULID validator: regex `^[0-9A-HJKMNP-TV-Z]{26}$` (Crockford Base32; matches `ulid.ULID.String()` output).

**MUST PASS:**

- All five `TestTypeRegistry_*` and `TestRegisterBuiltinTypes_*` tests pass.
- After `RegisterBuiltinTypes`, exactly four type names are registered: `scene`, `location`, `character`, `dm`.
- `dm.OrderInsensitiveIDs == true`; the other three have `OrderInsensitiveIDs == false`.
- `dm.Arity == 2`; the other three have `Arity == 1`.
- Duplicate Register returns an `oops.Code("TYPE_REGISTRY_DUPLICATE")` error.

**MUST NEVER:**

- The registry MUST NEVER expose a way to mutate or unregister an already-registered type.
- This task MUST NEVER add a plugin-loader hook or extend `crypto_manifest.go` — plugin-declared sensitive context types are deferred to a follow-up epic per spec §1.2.
- `LookupType` MUST NEVER return `(_, true)` for any type name not registered via the public `Register` API.

**Validation of done:**

- Universal gate PASS.
- `task test -- ./internal/eventbus/codec/` PASS.
- Per-package coverage on `typeregistry.go` ≥ 90% (spec §7.4 threshold).

**Commit message:** `feat(codec): TypeRegistry substrate + built-in sensitive context types (sub-epic F)`

---

### Task 3: `approval.Repo.GetByOpArgsHash` + migration 000036

**Files:**

- Create: `internal/store/migrations/000036_admin_approvals_op_args_hash_idx.up.sql`
- Create: `internal/store/migrations/000036_admin_approvals_op_args_hash_idx.down.sql`
- Modify: `internal/admin/approval/repo.go` (extend `Repo` interface + Postgres impl)
- Modify: `internal/admin/approval/repo_integration_test.go`

**Goal:** Add idempotent dual-control reuse lookup. F's handler queries `GetByOpArgsHash(opKind, opArgsHash, excludePlayerID)` to find an existing fresh approval for the same operation arguments, authored by someone other than the requesting operator.

**Spec reference:** §2.7 + INV-F17.

**TDD acceptance criteria:** (5 integration tests; all named `TestINV_F17_*` to anchor the meta-test discipline)

- `TestINV_F17_GetByOpArgsHashMatrix_ReturnsApproved` — happy path: an approved row not authored by `excludePlayerID` is returned.
- `TestINV_F17_GetByOpArgsHashMatrix_FiltersExpired` — `expires_at <= now()` rows return `APPROVAL_NOT_FOUND`.
- `TestINV_F17_GetByOpArgsHashFiltersOwnAuthor` — row where `primary_player_id == excludePlayerID` returns NOT_FOUND.
- `TestINV_F17_GetByOpArgsHashMatrix_FiltersUnapproved` — pending row (`approved_at IS NULL`) returns NOT_FOUND.
- `TestINV_F17_GetByOpArgsHashMatrix_TiebreakerMostRecent` — two approved rows match; the most-recently-approved is returned.

**Implementation notes:**

- Method signature: `GetByOpArgsHash(ctx context.Context, opKind string, opArgsHash []byte, excludePlayerID string) (Approval, error)`.
- ALL filters server-side in SQL (no result-side filtering): `WHERE op_kind = $1 AND op_args_hash = $2 AND expires_at > now() AND approved_at IS NOT NULL AND primary_player_id != $3 ORDER BY approved_at DESC LIMIT 1`.
- Migration: `CREATE INDEX admin_approvals_op_kind_args_hash_idx ON admin_approvals (op_kind, op_args_hash, expires_at)`. Use `IF NOT EXISTS` per project migration conventions; `.down.sql` mirrors with `DROP INDEX IF EXISTS`.
- Use `pgx.ErrNoRows` detection per existing repo patterns; wrap in `oops.Code("APPROVAL_NOT_FOUND")`.

**MUST PASS:**

- All five `TestINV_F17_GetByOpArgsHash*` integration tests pass.
- The composite index `admin_approvals_op_kind_args_hash_idx` exists post-migration; `down.sql` cleanly removes it.
- `GetByOpArgsHash` applies all five filter predicates SERVER-SIDE in a single SQL statement (op_kind, op_args_hash, expires_at, approved_at, primary_player_id).
- Tiebreaker `ORDER BY approved_at DESC LIMIT 1` returns the most-recently-approved row.

**MUST NEVER:**

- `GetByOpArgsHash` MUST NEVER do result-side filtering for any of the five predicates.
- `GetByOpArgsHash` MUST NEVER return a row authored by the caller (`primary_player_id == excludePlayerID`), even if all other filters match.
- `GetByOpArgsHash` MUST NEVER return an unapproved row (`approved_at IS NULL`).
- `GetByOpArgsHash` MUST NEVER return an expired row (`expires_at <= now()`).
- The migration MUST NEVER alter the `admin_approvals` table schema (it only adds an index).
- D's existing `Repo.Open`, `Get`, `MarkApproved`, `WaitForApproval` MUST NEVER change behavior.

**Validation of done:**

- Universal gate PASS.
- `task test:int -- -run TestINV_F17 ./internal/admin/approval/` PASS.
- `task test:int -- ./internal/admin/approval/` PASS (zero regressions in D's existing tests).
- Migration `000036.up.sql` + `.down.sql` paired and idempotent (re-running up.sql succeeds via `IF NOT EXISTS`; running down.sql twice succeeds via `IF EXISTS`).
- Per-package coverage on `GetByOpArgsHash` ≥ 90%.

**Commit message:** `feat(approval): GetByOpArgsHash + migration 000036 (sub-epic F INV-F17)`

---

### Task 4: `AdminReadStream` RPC + new `read_stream.proto`

**Files:**

- Create: `api/proto/holomush/admin/v1/read_stream.proto`
- Modify: `api/proto/holomush/admin/v1/admin.proto` (add RPC + import)

**Goal:** Define the wire surface. `ContextRef{type string, repeated string ids}` is variable-arity. `AdminReadStreamResponse` is a `oneof { pending_approval, started, event, finished }`. `ReadFinished.TerminatedBy` enum.

**Spec reference:** §3.2, §3.3.

**TDD acceptance criteria:**

- `TestAdminReadStreamRequest_RoundTrip` — proto Marshal/Unmarshal preserves all fields including nested `ContextRef{type, ids}`.
- `TestReadFinished_TerminatedByEnum` — the 7 enum values have stable integer indices `0..6`.

**Implementation notes:**

- Imports: `google/protobuf/timestamp.proto` + `holomush/core/v1/core.proto` (for the `Event` reference in the response oneof).
- Run `task proto:gen` after editing.
- Confirm ConnectRPC bindings generate for the streaming RPC (mirror the rekey RPCs' generation output).

**MUST PASS:**

- `task proto:gen` succeeds without warnings.
- Both smoke tests pass: round-trip preserves all `ContextRef.ids`, oneof discriminator decodes correctly, `TerminatedBy` enum has stable indices 0..6.
- Generated Go bindings appear at `pkg/proto/holomush/admin/v1/read_stream.pb.go` with the expected struct shapes (`AdminReadStreamRequest`, `AdminReadStreamResponse`, `ContextRef`, `PendingApproval`, `ReadStarted`, `ReadFinished`).
- ConnectRPC binding generated for the streaming RPC (mirror rekey's generated handler/client pattern).

**MUST NEVER:**

- The proto file MUST NEVER omit `optional` semantics where the spec calls for them (e.g., `since`/`until` are optional → server defaults).
- The `oneof payload` MUST NEVER include any non-spec variant.
- `ReadFinished.TerminatedBy` MUST NEVER reorder or renumber existing enum values across future revisions (wire stability).
- The proto MUST NEVER import outside `holomush/core/v1/core.proto` and `google/protobuf/timestamp.proto`.

**Validation of done:**

- Universal gate PASS.
- `task test -- ./pkg/proto/holomush/admin/v1/` PASS.
- `task lint` PASS on generated files (none of them should trigger lint).

**Commit message:** `feat(admin): AdminReadStream proto wire surface (sub-epic F)`

---

### Task 5: Register builtin event types for operator_read

**Files:**

- Modify: `internal/core/builtins.go` (two new rows)
- Create: `internal/core/builtins_operator_read_test.go`

**Goal:** Register `crypto.system.operator_read` and `crypto.system.operator_read_completed` in the verb builtins table so the `RenderingPublisher` doesn't reject them with `EMIT_UNKNOWN_VERB` (the regression E surfaced).

**Spec reference:** §3.8 + INV-F13.

**TDD acceptance criteria:**

- `TestINV_F13_BuiltinEventTypesRegistered` — read the verb registry the host populates at boot; assert both event types are present with `Category="system"`, `Format="audit"`, `DisplayTarget=corev1.EventChannel_EVENT_CHANNEL_AUDIT_ONLY`, `Source="builtin"`.

**Implementation notes:**

- Mirror the `crypto.system.rekey` row at `internal/core/builtins.go:93` verbatim — same field set, just the new `Type` string.
- Two new rows appended to the existing `builtins` slice.
- Test uses the real registry surface: `core.NewVerbRegistry()` + `registerBuiltinTypes(r, hostVersion)` + the existing lookup method on `*VerbRegistry`. Read `internal/core/registry.go:14-30, 122-154` for the real symbols.

**MUST PASS:**

- `TestINV_F13_BuiltinEventTypesRegistered` finds both rows in the verb registry post-boot with the four asserted field values exactly.
- `VerbRegistration.registerNoLock` validation passes for both rows (no `INVALID_REGISTRATION` error).
- Builtins-golden test asserts the new rows do NOT displace or modify the pre-existing `crypto.system.rekey` row.

**MUST NEVER:**

- The two new builtin rows MUST NEVER use `Category` other than `"system"`.
- The two new builtin rows MUST NEVER use `DisplayTarget` other than `EVENT_CHANNEL_AUDIT_ONLY`.
- The two new builtin rows MUST NEVER use `Source` other than `"builtin"`.
- This task MUST NEVER modify any other row in `builtins.go` — strictly additive.
- `RenderingPublisher` MUST NEVER reject these event types with `EMIT_UNKNOWN_VERB` (the regression E surfaced).

**Validation of done:**

- Universal gate PASS.
- `task test -- -run TestINV_F13 ./internal/core/` PASS.
- `task test -- ./internal/core/` PASS (no regressions in builtins, registry, or downstream tests).

**Commit message:** `feat(core): register crypto.system.operator_read* builtin event types (sub-epic F INV-F13)`

---

### Task 6: F-local audit payload structs + hash encoders

**Files:**

- Create: `internal/admin/readstream/audit_payload.go`
- Create: `internal/admin/readstream/audit_payload_test.go`

**Goal:** Define `OperatorReadStartPayload` + `OperatorReadCompletedPayload` Go types that get marshaled into the chain audit events. Hash fields are `sha256:<hex>` strings. F duplicates `encodeHash`/`encodeHashPtr` locally because `dek.encodeHash` is package-private.

**Spec reference:** §3.5.

**TDD acceptance criteria:**

- `TestEncodeHash_HasSha256Prefix` — `encodeHash(32 bytes)` returns `"sha256:" + hex.EncodeToString(input)`.
- `TestEncodeHashPtr_NilForGenesis` — `encodeHashPtr(nil)` returns nil; non-nil input returns a pointer to the encoded string.
- `TestOperatorReadStartPayload_JSONRoundTrip` — Marshal + Unmarshal preserves all fields including nullable `RequestedSince/Until` (INV-F7 requested-vs-resolved distinction) and the ContextRef slices.
- `TestOperatorReadCompletedPayload_JSONRoundTrip` — same for the completed payload, with all 8 fields including `RequestID` as string + `EventsScanned`/`DecryptFailCount`.

**Implementation notes:**

- `RequestID` is `string` (26-char ULID Base32, mirroring `dek.RekeyAuditPayload.RequestID` per `internal/eventbus/crypto/dek/audit_chain.go:33`), NOT `ulid.ULID` — keeps chain `ScopeFromPayload` extraction byte-stable.
- `PolicyHash`, `SelfHash`, `PrevHash` are also `string` (`sha256:<hex>` form). `PrevHash` is `*string` on start (nullable for genesis), plain `string` on completed (always present).
- `RequestedSince`/`RequestedUntil` are `*time.Time` (nullable for defaulting); `ResolvedSince`/`ResolvedUntil` are always-populated `time.Time`.
- See spec §3.5 for the full field list.

**MUST PASS:**

- `encodeHash(b)` returns exactly `"sha256:" + hex.EncodeToString(b)`, byte-equivalent to `dek.encodeHash` at `internal/eventbus/crypto/dek/audit.go:121-136`.
- `encodeHashPtr(nil)` returns `nil`; `encodeHashPtr(b)` returns a non-nil pointer.
- Both payload structs JSON-marshal + unmarshal without losing any field. `RequestedSince` survives as a `*time.Time` (nullable); `ResolvedSince` survives as always-populated.
- `RequestID` survives as a `string` (26-char ULID Base32), NOT `ulid.ULID`. Mirrors `dek.RekeyAuditPayload.RequestID`.

**MUST NEVER:**

- `encodeHash` MUST NEVER produce a byte-output that differs from `dek.encodeHash` for the same input. Cross-chain JCS canonical-form parity depends on this.
- The payload structs MUST NEVER omit any of the spec §3.5 fields. Implementer copies the field list verbatim.
- `RequestID` MUST NEVER be typed as `ulid.ULID` in either payload struct (would break chain `ScopeFromPayload` string extraction).
- The `Requested-*` nullable fields MUST NEVER be confused with `Resolved-*` fields — INV-F7 distinguishes the two on the persisted audit row.

**Validation of done:**

- Universal gate PASS.
- `task test -- ./internal/admin/readstream/` PASS (this task is the first to populate the package, so only its own tests exist).
- `task lint` PASS on the new file.
- Per-package coverage on `audit_payload.go` ≥ 90%.

**Commit message:** `feat(readstream): audit payload types + sha256:<hex> hash encoders (sub-epic F)`

---

### Task 7: `OperatorReadAuthGuard` (unconditional PERMIT)

**Files:**

- Create: `internal/admin/readstream/auth_guard.go`
- Create: `internal/admin/readstream/auth_guard_test.go`

**Goal:** F-local `SessionAuthGuard` implementation that returns PERMIT unconditionally. Wired into `HistoryReader.WithHistoryAuth` for the operator-read path. The real defense is upstream (operator-gate in the handler); this guard exists because HistoryReader's auth-check seam isn't optional.

**Spec reference:** §2.4 trust boundary + INV-F4.

**TDD acceptance criteria:**

- `TestINV_F4_OperatorReadAuthGuardAlwaysPermits` — table-test with several `SessionCheckRequest` shapes (operator/player/zero-value identity, various event types and key IDs); each invocation returns `Decision{Permit: true}, nil`.

**Implementation notes:**

- Satisfies `eventbus.SessionAuthGuard.Check(ctx, SessionCheckRequest) (SessionDecision, error)` per `internal/eventbus/subscriber_auth.go:46-67`. Add a compile-time assertion `var _ eventbus.SessionAuthGuard = (*OperatorReadAuthGuard)(nil)`.
- The handler structurally guarantees the standard runtime AuthGuard is never invoked on this path (INV-F5; tested in T16).

**MUST PASS:**

- `TestINV_F4_OperatorReadAuthGuardAlwaysPermits` passes with `Decision{Permit: true}, nil` for every `SessionCheckRequest` input.
- Compile-time assertion `var _ eventbus.SessionAuthGuard = (*OperatorReadAuthGuard)(nil)` succeeds.

**MUST NEVER:**

- `OperatorReadAuthGuard.Check` MUST NEVER return `Decision{Permit: false}` for any input.
- `OperatorReadAuthGuard.Check` MUST NEVER return a non-nil error.
- `OperatorReadAuthGuard` MUST NEVER consult any external state (no DB lookup, no ABAC eval, no policy chain check) — it is structurally always-permit.

**Validation of done:**

- Universal gate PASS.
- `task test -- ./internal/admin/readstream/` PASS.
- The guard satisfies `eventbus.SessionAuthGuard` at compile time (Go won't link otherwise).

**Commit message:** `feat(readstream): OperatorReadAuthGuard unconditional-permit (sub-epic F INV-F4)`

---

### Task 8: `NoOpDecryptAuditEmitter`

**Files:**

- Create: `internal/admin/readstream/decrypt_audit_emitter.go`
- Create: `internal/admin/readstream/decrypt_audit_emitter_test.go`

**Goal:** F-local `SessionAuditEmitter` no-op implementation. Wired into `HistoryReader.WithHistoryAuth` because the operator-read produces a single invocation-scoped audit event (T10), not per-decrypt audit traffic (master spec §7.6).

**Spec reference:** §2.4 + INV-F12.

**TDD acceptance criteria:**

- `TestINV_F12_NoOpDecryptAuditEmitterDoesNothing` — call `EmitPluginDecrypt(ctx, PluginDecryptRecord{...})` with a fully-populated record; assert nil error returned, no side effects observable.

**Implementation notes:**

- Satisfies `eventbus.SessionAuditEmitter.EmitPluginDecrypt(ctx, PluginDecryptRecord) error` per `internal/eventbus/subscriber_auth.go:75-94`. Add compile-time assertion.
- `PluginDecryptRecord` has fields `PluginName, PluginInstanceID, EventID, EventSubject, EventType, DEKRef codec.KeyID, DEKVersion, GrantID`. The implementer's test constructs a representative record but the emitter discards it.

**MUST PASS:**

- `TestINV_F12_NoOpDecryptAuditEmitterDoesNothing` calls `EmitPluginDecrypt` and asserts nil error.
- Compile-time assertion `var _ eventbus.SessionAuditEmitter = (*NoOpDecryptAuditEmitter)(nil)` succeeds.

**MUST NEVER:**

- `EmitPluginDecrypt` MUST NEVER return an error.
- `EmitPluginDecrypt` MUST NEVER mutate global state, write to disk, call NATS, or otherwise produce side effects observable outside the function.
- This emitter MUST NEVER be substituted for the production `DecryptAuditEmitter` outside the AdminReadStream wiring path.

**Validation of done:**

- Universal gate PASS.
- `task test -- ./internal/admin/readstream/` PASS.

**Commit message:** `feat(readstream): NoOpDecryptAuditEmitter for HistoryReader seam (sub-epic F INV-F12)`

---

### Task 9: Audit chain factories — `OperatorReadChainFor` + `OperatorReadHandlerFor`

**Files:**

- Create: `internal/admin/readstream/chain.go`
- Create: `internal/admin/readstream/chain_test.go`

**Goal:** Two factory functions mirroring `dek.RekeyHandlerFor` and `policy.PolicySetHandlerFor`. Path C chain shape: both event types (`crypto.system.operator_read` start + `crypto.system.operator_read_completed`) share the same NATS subject prefix `events.<game>.system.operator_read`. `LoadEntriesByScope` returns both by `js_seq` ASC, so the completed event's `prev_hash` chains to the start event naturally.

**Spec reference:** §3.6.

**TDD acceptance criteria:**

- `TestOperatorReadChainFor_SubjectPrefixSatisfiesINVE26` — chain.ValidateRegistration passes.
- `TestOperatorReadHandlerFor_AllSevenFieldsPopulated` — `Chain, SubjectFor, ScopeFromSubject, ScopeFromPayload, Canonicalize, PrevHashOf, SelfHashOf` are all non-nil. (Mirror the field check pattern; the chain.Handler struct is at `internal/eventbus/audit/chain/verifier.go:23-52`.)
- `TestOperatorReadHandler_SubjectRoundTrip` — `SubjectFor(scope)` and `ScopeFromSubject(subject)` are inverses on the canonical subject form.
- `TestOperatorReadHandler_ScopeFromSubjectRejectsBadPrefix` — subject lacking the expected prefix returns oops code `OPERATOR_READ_SCOPE_FROM_SUBJECT_FAILED`.
- `TestOperatorReadHandler_PayloadCallbacksRoundTrip` — JSON payload with `request_id`, `self_hash`, `prev_hash` fields: `ScopeFromPayload` extracts `request_id`; `Canonicalize` returns non-empty bytes; `PrevHashOf` returns nil for absent prev_hash; `SelfHashOf` hex-decodes the `sha256:<hex>` string.

**Implementation notes:**

- Read `internal/admin/policy/chain.go::PolicySetHandlerFor` end-to-end for the template — that's the canonical 7-field Handler implementation. Adapt the callbacks for the operator_read payload shape.
- `SubjectPrefix = "events.<gameID>.system.operator_read"` (no game-id with dots; INV-E26 prefix check passes).
- `decodeOperatorReadPayloadJSON` (helper): mirrors `policy.decodePolicyPayloadJSON` — proto-decode the envelope OR accept raw JSON for test fakes.
- Hash callbacks decode `sha256:<hex>` form (mirror `dek/audit_chain.go::decodeHashString`).

**MUST PASS:**

- All five `TestOperatorReadChainFor_*` / `TestOperatorReadHandlerFor_*` / `TestOperatorReadHandler_*` tests pass.
- `chain.ValidateRegistration(OperatorReadChainFor("gameID"))` returns nil.
- All seven `chain.Handler` field functions are non-nil after `OperatorReadHandlerFor` returns.
- `SubjectFor(scope)` and `ScopeFromSubject(SubjectFor(scope))` are inverses.
- `ScopeFromPayload(envelope)` returns `payload.request_id` as string.
- `Canonicalize(envelope)` returns non-empty bytes that JCS-canonicalize stably (deterministic byte output for the same input).

**MUST NEVER:**

- Chain `SubjectPrefix` MUST NEVER fail to start with `events.` (INV-E26).
- Chain `ScopePayloadField` MUST NEVER be empty (INV-E27).
- `ScopeFromSubject` MUST NEVER return success for a subject lacking the expected prefix.
- The `Canonicalize` callback MUST NEVER mutate the input bytes; it returns fresh bytes.
- Any of the seven `chain.Handler` fields being nil MUST NEVER ship — they're verified at boot by `VerifierSubsystem.Start`.

**Validation of done:**

- Universal gate PASS.
- `task test -- ./internal/admin/readstream/` PASS.
- `task lint` PASS.
- Per-package coverage on `chain.go` callbacks ≥ 90%.

**Commit message:** `feat(readstream): OperatorReadHandlerFor with all 7 chain.Handler callbacks (sub-epic F)`

---

### Task 10: `OperatorReadAuditEmitter` (EmitStart + EmitCompleted)

**Files:**

- Create: `internal/admin/readstream/audit_emitter.go`
- Create: `internal/admin/readstream/audit_emitter_test.go`

**Goal:** Compose `chain.Emitter` (prev-hash computation via `LoadEntriesByScope`) with `eventbus.Publisher` (NATS publish). Mirrors `dek.RekeyAuditEmitter`. EmitStart at genesis has `prev_hash=nil`; EmitCompleted refuses if no preceding start (chain emitter returns no prior entry).

**Spec reference:** §3.7.

**TDD acceptance criteria:**

- `TestOperatorReadAuditEmitter_EmitStartGenesis` — chain emitter fake returns nil prevHash; assert publish target subject is `events.<game>.system.operator_read.<request_id>`, event type is `crypto.system.operator_read`, payload is non-empty.
- `TestOperatorReadAuditEmitter_EmitStartPropagatesPublishError` — publisher fake returns an error; assert EmitStart wraps and returns it.
- `TestOperatorReadAuditEmitter_EmitCompletedChainsToStart` — chain emitter fake returns a non-nil prevHash (representing the start entry); assert publish target type is `crypto.system.operator_read_completed`, and `ComputePrevHashFor` was called.
- `TestOperatorReadAuditEmitter_EmitCompletedRefusesWithoutStart` — chain emitter returns nil prevHash (no start emitted); EmitCompleted returns oops code `OPERATOR_READ_AUDIT_COMPLETED_NO_START`.

**Implementation notes:**

- Read `internal/eventbus/crypto/dek/audit.go` end-to-end for the publish-pattern template.
- Constructor: `NewOperatorReadAuditEmitter(ce chain.Emitter, pub eventbus.Publisher, h chain.Handler)`. Compose via the rekey precedent — the `pub` publisher is the NATS publisher seam from D's wiring, NOT the chain emitter.
- Use `eventbus.NewEvent(subject, type, actor, payload)` for construction (`internal/eventbus/types.go:185`). Actor kind for host-emit audit events: `eventbus.ActorKindSystem` per `internal/eventbus/types.go:62-71` (NOT `ActorKindHost`).
- Self-hash computation: marshal payload → unmarshal to `map[string]any` → call `chain.RecomputeSelfHash(m, h.Chain.SelfHashField)` → encode result with `encodeHash` (Task 6's helper) → store in payload.SelfHash → re-marshal → publish.
- `ComputePrevHashFor` returns `([]byte, *ulid.ULID, error)` per `internal/eventbus/audit/chain/emitter.go:35`. F discards the ULID return (chain spec doesn't require it for our use).

**MUST PASS:**

- All four `TestOperatorReadAuditEmitter_*` tests pass.
- `EmitStart` at genesis (chain emitter returns nil prevHash) publishes successfully and emits to subject `events.<game>.system.operator_read.<request_id>` with event type `crypto.system.operator_read`.
- `EmitStart` publish failure surfaces the error wrapped in `OPERATOR_READ_AUDIT_PUBLISH_FAILED`.
- `EmitCompleted` chains its `prev_hash` to the start event's recomputed self_hash via `chain.NewEmitter(repo).ComputePrevHashFor`.
- `EmitCompleted` without a preceding start returns `OPERATOR_READ_AUDIT_COMPLETED_NO_START`.

**MUST NEVER:**

- `EmitStart` and `EmitCompleted` MUST NEVER publish to different subjects — Path C uses the SAME subject for both (`events.<game>.system.operator_read.<request_id>`); only `Event.Type` differs.
- The emitter MUST NEVER use `core.NewEvent` (wrong package); it MUST use `eventbus.NewEvent(subject, type, actor, payload)`.
- Actor on the published event MUST be `ActorKindSystem` (host-emit audit). `ActorKindHost` does NOT exist; using it is a build error.
- `chain.NewEmitter` is called with arity 1 (repo only). MUST NEVER add extra positional args.

**Prometheus metric (INV-F10):**

- This task registers a new counter metric: `holomush_admin_readstream_completed_audit_failures_total` (no labels). The metric MUST increment on every failed `EmitCompleted` publish-or-chain-emit. The metric MUST NOT increment on `EmitStart` failures (those raise, not log).
- Metric registration follows the existing prometheus-singleton pattern at `cmd/holomush/admin_authenticate_e2e_test.go` (the prometheus singleton workaround E established). Implementer mirrors the rekey-audit metric registration site if one exists; otherwise registers a fresh counter via the project's standard `prometheus.NewCounter` + `prometheus.MustRegister` pattern.
- A unit test asserts the metric increments on failure (call EmitCompleted with a fault-injected publisher; assert metric value goes from 0 → 1).

**Validation of done:**

- Universal gate PASS.
- `task test -- ./internal/admin/readstream/` PASS.
- `task lint` PASS.
- The metric `holomush_admin_readstream_completed_audit_failures_total` is registered with the prometheus registry at boot.

**Commit message:** `feat(readstream): OperatorReadAuditEmitter with chain prev-hash + NATS publish + INV-F10 metric (sub-epic F)`

---

### Task 11: Decrypt-fail classifier matrix

**Files:**

- Create: `internal/admin/readstream/classify.go`
- Create: `internal/admin/readstream/classify_test.go`

**Goal:** Pure mapping from cold-tier oops codes / sentinels to `NoPlaintextReason` values. Every branch grounds in a real production producer. Unknown errors surface as `INTERNAL` with a WARN log responsibility deferred to the caller.

**Spec reference:** §4.5 + INV-F12.

**TDD acceptance criteria:**

- `TestINV_F12_ClassifierMatrix` — table-test covering all branches:
  - nil → (`UNSPECIFIED`, false)
  - `context.Canceled` → (`UNSPECIFIED`, true) // fatal=true, bail stream
  - `context.DeadlineExceeded` → (`UNSPECIFIED`, true)
  - `source.ErrMetadataOnly` → (`STALE_DEK`, false)
  - oops code `EVENTBUS_COLD_DEK_COLUMNS_MISSING` → (`DEK_MISSING`, false)
  - oops code `EVENTBUS_COLD_BAD_DEK_COLUMNS` → (`DEK_BAD_COLUMNS`, false)
  - any other error → (`INTERNAL`, false)

**Implementation notes:**

- Interface: `Classifier{ Classify(err error) (eventbus.NoPlaintextReason, fatal bool) }`. Default impl is a struct with no state.
- Use `errors.Is(err, source.ErrMetadataOnly)` for the sentinel; `errors.Is(err, context.Canceled)` for cancellation.
- Use `oops.AsOops(err)` + check `Code()` for the cold-tier codes. Helper `isOopsCode(err, code)` for readability.
- Real `source.ErrMetadataOnly` is at `internal/eventbus/history/source/fallback.go` — verify it's defined as `var ... = errors.New(...)` (a sentinel that `errors.Is` works against).

**MUST PASS:**

- `TestINV_F12_ClassifierMatrix` table-test covers all seven branches (nil, ctx.Canceled, ctx.DeadlineExceeded, ErrMetadataOnly, EVENTBUS_COLD_DEK_COLUMNS_MISSING, EVENTBUS_COLD_BAD_DEK_COLUMNS, default).
- Each branch returns the exact `NoPlaintextReason` + `fatal bool` pair documented in spec §4.5.
- `ctx.Canceled` and `ctx.DeadlineExceeded` return `fatal=true` (stream bails).
- Every other classified branch returns `fatal=false` (produces metadata-only frame).

**MUST NEVER:**

- The classifier MUST NEVER modify the input error (it's a pure mapping function).
- The classifier MUST NEVER mutate global state, log, or have side effects (caller logs the WARN for INTERNAL).
- The classifier MUST NEVER return `fatal=true` for any error other than context cancellation/deadline.
- The classifier MUST NEVER cause `decodeColdRow` to skip its existing fail-close behavior — F's classifier WRAPS the cold-tier surface, doesn't replace it.

**Validation of done:**

- Universal gate PASS.
- `task test -- ./internal/admin/readstream/` PASS.
- Coverage on `classify.go` ≥ 90%.

**Commit message:** `feat(readstream): decrypt-fail classifier matrix grounded in production codes (sub-epic F INV-F12)`

---

### Task 12: `ResolveBounds` validation

**Files:**

- Create: `internal/admin/readstream/filter.go`
- Create: `internal/admin/readstream/filter_test.go`

**Goal:** Validate + canonicalize an `AdminReadStreamRequest` into `Resolved{Contexts, Since, Until}` + `ResolvedFlags{SinceDefaulted, UntilDefaulted}`. All `DENY_OPERATOR_READ_*` rejection codes fire here.

**Spec reference:** §4.1.

**TDD acceptance criteria:** (one test per branch)

- `TestResolveBounds_DefaultsFromZeroBounds` — empty req → `Since = now - 1h`, `Until = now`, both flags true.
- `TestINV_F6_WindowTooLargeRejected` — window > MaxWindow → oops code `DENY_OPERATOR_READ_WINDOW_TOO_LARGE`.
- `TestResolveBounds_TimeInvertedRejected` — `until <= since` → `DENY_OPERATOR_READ_TIME_INVERTED`.
- `TestResolveBounds_FutureBoundRejected` — `until > now + 5s` → `DENY_OPERATOR_READ_FUTURE_BOUND`.
- `TestResolveBounds_JustificationEmpty` — empty/whitespace justification → `DENY_OPERATOR_READ_JUSTIFICATION_EMPTY`.
- `TestResolveBounds_JustificationTooLong` — >4096 bytes → `DENY_OPERATOR_READ_JUSTIFICATION_TOO_LONG`.
- `TestResolveBounds_ContextTypeUnknown` — type not in `TypeRegistry` → `DENY_OPERATOR_READ_CONTEXT_TYPE_UNKNOWN`.
- `TestResolveBounds_ContextArityMismatch` — wrong number of ids for type → `DENY_OPERATOR_READ_CONTEXT_ARITY_MISMATCH`.
- `TestResolveBounds_ContextIDMalformed` — id fails `MatchID` → `DENY_OPERATOR_READ_CONTEXT_ID_MALFORMED`.
- `TestResolveBounds_DMLexCanonicalized` — dm with `[lex-higher, lex-lower]` ids → canonicalized to `[lex-lower, lex-higher]`.
- `TestResolveBounds_TooManyContexts` — >64 contexts → `DENY_OPERATOR_READ_CONTEXT_TOO_MANY`.

**Implementation notes:**

- Signature: `ResolveBounds(req *Request, typeRegistry codec.TypeRegistry, now time.Time, defaultWindow, maxWindow time.Duration) (Resolved, ResolvedFlags, error)`.
- `Request` is F's domestic shape (not the adminv1 proto): `Request{Contexts []ContextRef, Since, Until time.Time, Justification string}`. Handler translates proto → Request in Task 16.
- Defaulting: if `req.Since.IsZero()`, set to `now - defaultWindow` and `flags.SinceDefaulted = true`. Same for Until.
- Validation order: temporal (TIME_INVERTED, FUTURE_BOUND, WINDOW_TOO_LARGE) → context (TOO_MANY, TYPE_UNKNOWN per-entry, ARITY_MISMATCH per-entry, ID_MALFORMED per-entry) → justification (EMPTY, TOO_LONG). Reject early.
- Canonicalize order-insensitive type IDs via `sort.Strings`. Dedupe by `(type, joined-ids)` key.

**MUST PASS:**

- All eleven `TestResolveBounds_*` / `TestINV_F6_*` tests pass.
- When `req.Since` is zero, `flags.SinceDefaulted == true` AND `resolved.Since == now - DefaultWindow`. Same for Until.
- DM type IDs are lex-canonicalized in `resolved.Contexts`.
- Duplicate contexts (same type + same canonical-ids) are deduped to one entry.
- Every validation rejection returns the exact `DENY_OPERATOR_READ_*` oops code documented in spec §3.8.

**MUST NEVER:**

- Validation MUST NEVER short-circuit BEFORE rejecting on time/window violations (temporal checks come first).
- A context with an unknown type MUST NEVER reach the subject-construction stage.
- The function MUST NEVER mutate the input `*Request`.
- Justification with leading/trailing whitespace MUST NEVER pass the empty check unless the trimmed string is non-empty.

**Validation of done:**

- Universal gate PASS.
- `task test -- ./internal/admin/readstream/` PASS.
- Coverage on `filter.go` ≥ 90%.

**Commit message:** `feat(readstream): ResolveBounds validation matrix (sub-epic F INV-F6/F7)`

---

### Task 13: `buildSubjects` direct subject construction

**Files:**

- Create: `internal/admin/readstream/subjects.go`
- Create: `internal/admin/readstream/subjects_test.go`

**Goal:** Convert resolved contexts to NATS query subjects in dot form. `subjectxlate` (legacy colon-form translator) is never imported. Empty contexts → whole-game wildcard `events.<game>.>`.

**Spec reference:** §4.3.

**TDD acceptance criteria:**

- `TestBuildSubjects_EmptyContextsReturnsGameWildcard` — `nil` → `["events.<game>.>"]`.
- `TestBuildSubjects_SingleContextArity1` — `scene:01H` → `["events.<game>.scene.01H.>"]`.
- `TestBuildSubjects_DMArity2` — `dm:01A:01B` → `["events.<game>.dm.01A.01B.>"]`.
- `TestBuildSubjects_MultipleContexts` — two refs → two subjects in input order.

**Implementation notes:**

- `subjectFor(c ContextRef, gameID string) eventbus.Subject` uses `strings.Builder` to compose `events.<gameID>.<type>.<id1>[.<id2>...].>`.
- `buildSubjects(contexts []ContextRef, gameID string) []eventbus.Subject` handles the empty case explicitly.

**MUST PASS:**

- All four `TestBuildSubjects_*` tests pass.
- Empty contexts → exactly one subject `events.<game>.>`.
- Single arity-1 context → exactly one subject `events.<game>.<type>.<id>.>`.
- Arity-2 (dm) → `events.<game>.dm.<id1>.<id2>.>`.
- Multiple contexts → one subject per context, in input order.

**MUST NEVER:**

- The implementation MUST NEVER import `internal/eventbus/subjectxlate/` (legacy colon-form translator).
- Subject construction MUST NEVER produce a colon-form output (`scene:01ABC` is for CLI parsing only; never on the wire/query path).
- Any subject MUST NEVER omit the trailing `.>` wildcard (HistoryReader expects subtree matching).
- The function MUST NEVER mutate the input `[]ContextRef`.

**Validation of done:**

- Universal gate PASS.
- `task test -- ./internal/admin/readstream/` PASS.

**Commit message:** `feat(readstream): buildSubjects direct dot-form construction (sub-epic F)`

---

### Task 14: k-way merge over multiple HistoryStreams

**Files:**

- Create: `internal/admin/readstream/stream_merge.go`
- Create: `internal/admin/readstream/stream_merge_test.go`

**Goal:** When the operator queries multiple contexts, F opens N `HistoryStream`s and merges by event timestamp using a min-heap. Single-stream case short-circuits.

**Spec reference:** §4.3.

**TDD acceptance criteria:**

- `TestMergeStreams_SingleStreamOrderedThrough` — one stream with 2 events emerges in order; EOF terminates.
- `TestMergeStreams_InterleaveByTimestamp` — two streams with interleaved timestamps emerge in global timestamp order.

**Implementation notes:**

- Backed by `container/heap` with `Less` comparing `event.Timestamp.Before(...)`. `eventbus.Event.Timestamp` is the field name (NOT `OccurredAt`; verified at `internal/eventbus/types.go:126`).
- Test stub: events constructed via `eventbus.Event{ID: ulid.Make(), Subject: ..., Timestamp: ...}`. ID is `ulid.ULID`, NOT string.
- Prime the heap lazily on first `Next` call (don't block in constructor).
- Returned `HistoryStream` impl satisfies `eventbus.HistoryStream{Next, Close}`.

**MUST PASS:**

- Both `TestMergeStreams_*` tests pass.
- Single-stream input short-circuits and returns the stream directly (no wrapping).
- Multi-stream input emits events in global `Timestamp` order across all sources.
- `Close()` on the merged stream closes all underlying streams.
- `Next()` returns `io.EOF` when all sources are drained.

**MUST NEVER:**

- The min-heap MUST NEVER use any field other than `eventbus.Event.Timestamp` for ordering.
- The merged stream MUST NEVER pull from a source after that source has returned `io.EOF`.
- The merged stream MUST NEVER skip events (every event from every source flows through).
- Implementation MUST NEVER access `Event.OccurredAt` (doesn't exist) or treat `Event.ID` as a string (it's `ulid.ULID`).

**Validation of done:**

- Universal gate PASS.
- `task test -- ./internal/admin/readstream/` PASS.
- Coverage on `stream_merge.go` ≥ 90%.

**Commit message:** `feat(readstream): k-way merge over multi-context HistoryStreams (sub-epic F)`

---

### Task 15: Per-frame write deadline shim

**Files:**

- Create: `internal/admin/readstream/deadline_writer.go`
- Create: `internal/admin/readstream/deadline_writer_test.go`

**Goal:** Generic `sendWithDeadline(ctx, send, frame, deadline)` helper that runs `send(frame)` in a goroutine and races against a timeout. Returns `ErrWriteDeadlineExceeded` sentinel when the timeout wins.

**Spec reference:** §4.6 + INV-F14.

**TDD acceptance criteria:**

- `TestSendWithDeadline_FastPasses` — instant-send fake → nil error returned, send count incremented.
- `TestINV_F14_SendWithDeadlineTrips` — slow-send fake (200ms) with 50ms deadline → returns `ErrWriteDeadlineExceeded`.
- `TestSendWithDeadline_PropagatesSenderError` — sender returns a custom error → that error propagates through (not the deadline sentinel).

**Implementation notes:**

- Generic `sendWithDeadline[T any](ctx, send func(T) error, frame T, deadline time.Duration) error`. Take a closure so production passes `stream.Send` and tests pass a stub.
- Use `context.WithTimeout` + `select { case err := <-done : case <-ctx.Done() : }`.
- `ErrWriteDeadlineExceeded = errors.New("readstream: write deadline exceeded")` for `errors.Is` compatibility.

**MUST PASS:**

- All three `TestSendWithDeadline_*` / `TestINV_F14_*` tests pass.
- Fast send (returns instantly) succeeds within the deadline.
- Slow send (> deadline) returns `ErrWriteDeadlineExceeded` and `errors.Is(err, ErrWriteDeadlineExceeded) == true`.
- Sender error (non-deadline) propagates through verbatim.

**MUST NEVER:**

- The shim MUST NEVER cap total stream duration — it only caps per-frame write time.
- `ErrWriteDeadlineExceeded` MUST NEVER be confused with a context cancellation; they're distinct errors.
- The shim MUST NEVER leak goroutines beyond what the underlying `send` function holds (the launched goroutine completes when send returns, even after deadline trips).

**Validation of done:**

- Universal gate PASS.
- `task test -- ./internal/admin/readstream/` PASS.

**Commit message:** `feat(readstream): per-frame write deadline shim (sub-epic F INV-F14)`

---

### Task 16: `Handler` struct + Config + INV-42 pre-data ordering + INV-F5

**Files:**

- Create: `internal/admin/readstream/handler.go`
- Create: `internal/admin/readstream/handler_test.go`

**Goal:** Ship the production `Handler` with the full INV-42 flow: auth (via the F-local `SessionAuthorizer` seam wrapping the real `sessions.GetOperatorSession` + `access.HasPlayerGrant` 2-call pattern), bounds validation, pre-data audit emit, ReadStarted frame send, streamData call, completion audit emit. Plus INV-F5 negative test.

**Spec reference:** §2.2, §4.2 + INV-F1, INV-F2, INV-F3, INV-F5.

**TDD acceptance criteria:**

- `TestINV_F1_PreDataAuditOrdering` — happy-path handler invocation; assert `AuditEmitter.EmitStart` is called exactly once AND `stream.Send` first-frame is a `ReadStarted` (not Event, not PendingApproval).
- `TestINV_F2_AuditPublishFailRefuses` — inject EmitStart error; assert handler returns oops code `DENY_AUDIT_PRE_DATA_PUBLISH` AND no frames are sent on the stream.
- `TestINV_F3_CapabilityCheckPrecedesAudit` — inject `SessionAuthorizer` error with code `DENY_OPERATOR_CAPABILITY`; assert handler returns that error AND `EmitStart` was NOT called AND no frames sent.
- `TestINV_F5_StandardAuthGuardNeverSeesOperator` — construct the F-local `OperatorReadAuthGuard` and assert it satisfies `eventbus.SessionAuthGuard` (compile-time + a type-assertion runtime check). Additionally: scan `internal/admin/readstream/handler.go` source (or its constructor wiring path) and assert no reference to the production runtime `authguard.Guard` type — F's wiring depends only on the F-local guard. (Implementer may use either a structural test or a compile-time assertion via `var _ eventbus.SessionAuthGuard = (*OperatorReadAuthGuard)(nil)` plus a separate static-analysis-style scan; pick one shape.)

**Implementation notes:**

- F-local seam: `SessionAuthorizer{ AuthorizeOperator(ctx, sessionToken, capability string) (Operator, error) }`. Production binding (built in T21) wraps the real 2-call pattern at `internal/admin/socket/rekey_handler.go:438-457`. F does NOT extend or modify the production `OperatorAuthProvider` interface (which is `Authenticate`-only).
- Narrow stream seam: `ResponseSender{ Send(*adminv1.AdminReadStreamResponse) error }`. `Handle(ctx, req, *connect.ServerStream[T])` is a 1-line adapter; the real flow lives in `handleInternal(ctx, req, ResponseSender)`. This makes unit tests trivial — fake = `recordingStream` satisfying `ResponseSender`.
- `Config` has fields: `Auth, Approvals, History, AuditEmitter, TypeRegistry, Classifier, PolicyHash, Clock, Logger, DEKManager, Game, MaxWindow, DefaultWindow, WriteDeadline, ApprovalTTL`. (See spec §2.5 for the canonical list.)
- Real `buildStartedFrame(rid, resolved, payload)` builds `adminv1.AdminReadStreamResponse` with `Payload: *_Started{Started: &adminv1.ReadStarted{...}}`. PolicyHash on the wire is decoded from `payload.PolicyHash`'s `sha256:<hex>` form back to 32 raw bytes.
- Real `buildFinishedFrame(term, scanned, fails, now)` builds the parallel `*_Finished` shape.
- `classifyTermination(err) adminv1.ReadFinished_TerminatedBy` maps oops codes to terminator enum values: `DENY_AUDIT_PRE_DATA_PUBLISH → AUDIT_EMIT_FAILURE`, `READSTREAM_DEADLINE_EXCEEDED → DEADLINE_EXCEEDED`, nil → `CLIENT_EOF`, default → `SERVER_ERROR`. Dual-control timeout handled separately by `emitTimeoutFinish`.
- `wrap(err) error`: returns `connect.NewError(connect.CodeInternal, err)`. CLI's exit-code mapping reads the underlying oops code via `oops.AsOops`.
- INV-F5 implementation: easiest shape is a `Validate() error` on `Config` called by `NewHandler` (returns error if auth guard type isn't `*OperatorReadAuthGuard`). OR keep INV-F5 purely as a structural test on the wiring path. Either is fine; spec doesn't dictate the mechanism.
- The Handler struct may carry a `streamDataOverride` test hook (function field) — production code never sets it; T16's INV-F1/F2/F3 tests use it to short-circuit the data phase. Acceptable test-only field per Go conventions when commented.
- `streamData` body is a stub here (returns 0, 0, nil) — Task 18 implements it.
- `acquireApproval` body is a stub here (returns `READSTREAM_NOT_IMPLEMENTED`) — Task 17 implements it. Its signature uses `ResponseSender` for the stream parameter (so T17's flow types match).

**MUST PASS:**

- `TestINV_F1_PreDataAuditOrdering`: happy path — `AuditEmitter.EmitStart` called exactly 1×, FIRST sent frame is `ReadStarted`, error is nil.
- `TestINV_F2_AuditPublishFailRefuses`: handler returns `DENY_AUDIT_PRE_DATA_PUBLISH` wrapped error, `EmitStart` called 1×, zero frames sent on stream, `HistoryReader.QueryHistory` never invoked.
- `TestINV_F3_CapabilityCheckPrecedesAudit`: handler returns the auth error wrapped, `EmitStart` called 0×, zero frames sent.
- `TestINV_F5_StandardAuthGuardNeverSeesOperator`: F's `OperatorReadAuthGuard` satisfies `eventbus.SessionAuthGuard` at compile time; F's wiring (verified by reading source or via a `NewHandler` validation hook) refuses to ship a Handler with a non-`*OperatorReadAuthGuard`.

**MUST NEVER:**

- An `Event` frame MUST NEVER precede the `ReadStarted` frame on the wire.
- Audit `EmitStart` MUST NEVER be skipped on the happy path.
- Audit `EmitStart` MUST NEVER be invoked when capability check fails (INV-F3 ordering).
- `HistoryReader.QueryHistory` MUST NEVER be called when pre-data audit publish fails (INV-F2 — structural ordering, not just runtime check).
- The handler MUST NEVER invent its own `OperatorAuthProvider.Authorize` method on D's interface — it uses the F-local `SessionAuthorizer` seam wrapping `sessions.GetOperatorSession` + `access.HasPlayerGrant`.
- The standard runtime `authguard.Guard` MUST NEVER appear in F's wiring (INV-F5).
- The handler's stream parameter MUST NEVER be typed against `*connect.ServerStream[T]` in the test-callable path — `handleInternal` uses the narrow `ResponseSender` interface; `Handle` is a thin adapter.
- `buildStartedFrame` / `buildFinishedFrame` MUST NEVER be `return nil` placeholders — they ship real proto-message construction.
- The handler MUST NEVER raise (`return wrap(err)`) when `EmitCompleted` fails — it MUST log WARN AND increment `holomush_admin_readstream_completed_audit_failures_total` (the metric registered in T10) AND continue. This is INV-F10's caller-side contract.

**Validation of done:**

- Universal gate PASS.
- All four named tests PASS.
- `task lint` PASS.
- Coverage on `handler.go` ≥ 90%.
- `Handler.Handle` does not contain any `// TODO`, `// implementer fills in`, or `panic("not implemented")` markers.
- A unit test (`TestINV_F10_CompletionAuditFailureNotRaised`) asserts: when `EmitCompleted` returns an error, (a) the handler's outer return value is NOT that error (it's `streamErr` from the data phase, which may be nil), (b) the WARN log line is emitted, (c) the prometheus counter incremented.

**Commit message:** `feat(readstream): Handler with INV-42 pre-data audit ordering + INV-F5/F10 (sub-epic F)`

---

### Task 17: Dual-control flow via `WaitForApproval` + `GetByOpArgsHash`

**Files:**

- Modify: `internal/admin/readstream/handler.go` (fill in `acquireApproval`)
- Modify: `internal/admin/readstream/handler_test.go`

**Goal:** Implement the dual-control branch. First try `GetByOpArgsHash` for idempotent reuse; if NOT_FOUND, `Open` a new approval, send PendingApproval frame, block on `WaitForApproval`. Timeout → emit ReadFinished{DUAL_CONTROL_TIMEOUT} and return error.

**Spec reference:** §2.3 + INV-F11.

**TDD acceptance criteria:**

- `TestINV_F11_DualControlBlocksUntilApproval` — `GetByOpArgsHash` returns NOT_FOUND; `Open` mints a request_id; `WaitForApproval` returns approved. Assert: GetByOpArgsHash called once, Open called once, WaitForApproval called once, exactly one PendingApproval frame sent, then ReadStarted (handler proceeds to data phase).
- `TestINV_F11_DualControlIdempotentReuse` — `GetByOpArgsHash` returns a fresh existing approval. Assert: Open NOT called, WaitForApproval NOT called, no PendingApproval frame sent, handler proceeds directly.
- `TestDualControlTimeout_EmitsFinishedAndReturnsErr` — `GetByOpArgsHash` returns NOT_FOUND; `WaitForApproval` returns `APPROVAL_WAIT_DEADLINE`. Assert: ReadFinished frame with `TerminatedBy=DUAL_CONTROL_TIMEOUT`; handler returns wrapped error.

**Implementation notes:**

- Compute op-args hash via `approval.ComputeOpArgsHash(pbReq)` — D-shipped at `internal/admin/approval/oparghash.go:20-27`.
- `Repo.Open` signature: `Open(ctx, OpenRequest{PrimaryPlayerID, OpKind, OpArgsHash}) (RequestID, error)`. `OpenRequest` does NOT carry `RequestID` or `ExpiresAt` (server mints + sets). Verify against `internal/admin/approval/repo.go:48-61`.
- `approval.RequestID` is `[16]byte`. Construct via `ulid.MustParse(s)` then convert; do NOT cast a string literal directly.
- Pending frame: `&adminv1.AdminReadStreamResponse{Payload: &adminv1.AdminReadStreamResponse_PendingApproval{PendingApproval: &adminv1.PendingApproval{RequestId: rid[:], ExpiresAt: timestamppb.New(...)}}}`.
- Dual-control timeout shape: handler returns `emitTimeoutFinish(stream, err)` which sends ReadFinished with `TerminatedBy=DUAL_CONTROL_TIMEOUT` then returns the wrapped err.
- Test fixtures: implement `fakeApprovalRepo` satisfying `approval.Repo`. Use `ulid.MustParse(...)` to construct valid `[16]byte` RequestIDs.
- Use the existing T16 `recordingStream` + `fakeServerStream` adapter; tests call `h.handleInternal(...)` directly, not `h.Handle(...)`.

**MUST PASS:**

- `TestINV_F11_DualControlBlocksUntilApproval`: when `GetByOpArgsHash` returns NOT_FOUND, exactly 1 `Open` call + 1 `WaitForApproval` call + 1 `PendingApproval` frame sent.
- `TestINV_F11_DualControlIdempotentReuse`: when `GetByOpArgsHash` returns a fresh approval, ZERO calls to `Open` or `WaitForApproval`, ZERO `PendingApproval` frames sent.
- `TestDualControlTimeout_EmitsFinishedAndReturnsErr`: handler emits `ReadFinished{DUAL_CONTROL_TIMEOUT}` AND returns a wrapped error after `WaitForApproval` returns `APPROVAL_WAIT_DEADLINE` or `DENY_APPROVAL_EXPIRED`.
- All approval RequestIDs constructed via `ulid.MustParse(...)` then converted to `[16]byte` — never via direct string-literal-to-[16]byte cast.

**MUST NEVER:**

- The handler MUST NEVER introduce an in-process pending-approval registry (chan, map, or otherwise). `WaitForApproval` is the canonical mechanism.
- More than one `PendingApproval` frame MUST NEVER be sent per invocation.
- `acquireApproval` MUST NEVER take `*connect.ServerStream[T]` as parameter — it takes `ResponseSender` (matches T16's `handleInternal` typing).
- `Approval` rows MUST NEVER be opened without `op_args_hash` populated via `approval.ComputeOpArgsHash(req)` from D's substrate.
- The handler MUST NEVER skip the dual-control phase when `req.DualControl == true`, even if the underlying `Repo.GetByOpArgsHash` fast-paths to reuse.

**Validation of done:**

- Universal gate PASS.
- All three named tests PASS.

**Commit message:** `feat(readstream): dual-control via WaitForApproval + GetByOpArgsHash (sub-epic F INV-F11)`

---

### Task 18: `streamData` loop with classifier + SensitiveOnly query

**Files:**

- Modify: `internal/admin/readstream/handler.go` (implement `streamData`)
- Modify: `internal/admin/readstream/handler_test.go`

**Goal:** Read events from cold tier (one per resolved context, merged via Task 14's k-way merge), classify decrypt failures via Task 11's classifier, send Event frames via the deadline-protected sender. All queries set `HistoryQuery.SensitiveOnly: true` (Task 0 substrate) so identity-codec rows are filtered server-side.

**Spec reference:** §4.4, §4.5 + INV-F15.

**TDD acceptance criteria:**

- `TestINV_F15_HandlerSetsSensitiveOnlyTrue` — fake `HistoryReader` records each query passed in; assert `q.SensitiveOnly==true` on every recorded query.
- `TestStreamData_HappyPathEventsCounted` — stub reader returns N successful events; assert `events_scanned==N`, `decrypt_fail_count==0`, all events streamed as `Event` frames.
- `TestStreamData_DecryptFailureProducesMetadataOnlyFrame` — stub reader returns an event with `MetadataOnly=true` and a `NoPlaintextReason` populated; assert the frame is emitted with the same MetadataOnly + reason, and `decrypt_fail_count` increments.
- `TestStreamData_ClassifierFatalErrorBailsStream` — stub reader's `Next` returns `context.Canceled`; assert streamData returns immediately with the error (no further events processed).

**Implementation notes:**

- For each resolved subject, open a `HistoryStream` via `h.cfg.History.QueryHistory(ctx, HistoryQuery{Subject, NotBefore, NotAfter, SensitiveOnly: true})`. Collect handles, defer Close.
- Merge via Task 14's `mergeStreams`.
- Loop: `merged.Next(ctx)`. On error, call classifier; if `fatal`, return err; else stamp a metadata-only frame with the classified reason and continue.
- On success: emit an Event frame. If the returned `eventbus.Event` has `MetadataOnly=true` and `NoPlaintextReason` populated (the hot-tier path can stamp this), pass them through to the wire frame.
- Send each frame via `sendWithDeadline(ctx, stream.Send, frame, h.cfg.WriteDeadline)`. On `ErrWriteDeadlineExceeded`, wrap as `READSTREAM_DEADLINE_EXCEEDED` (which `classifyTermination` maps to `TERMINATED_BY_DEADLINE_EXCEEDED`).
- `rowToProtoEvent(row eventbus.Event, plaintext []byte, reason NoPlaintextReason) *corev1.Event`: builds the `corev1.Event` for the response. Field set is per `holomush.core.v1.Event` proto (verify the exact field names via `task proto:gen` output or `pkg/proto/holomush/core/v1/core.pb.go`). Populate `metadata_only` and `no_plaintext_reason` for non-zero reason. The implementer extracts a shared helper if a similar adapter already exists in `internal/grpc/`; otherwise writes it locally.

**MUST PASS:**

- `TestINV_F15_HandlerSetsSensitiveOnlyTrue`: every recorded HistoryQuery has `SensitiveOnly == true`. (One query per resolved context; whole-game case has exactly one query.)
- `TestStreamData_HappyPathEventsCounted`: events_scanned counts ONLY events that flowed through the merged stream.
- `TestStreamData_DecryptFailureProducesMetadataOnlyFrame`: a stub event with `MetadataOnly=true, NoPlaintextReason=STALE_DEK` is emitted on the wire with those flags preserved AND `decrypt_fail_count` increments.
- `TestStreamData_ClassifierFatalErrorBailsStream`: `context.Canceled` from `merged.Next` bails the loop immediately (no further reads, no further frames sent).
- All `Event` frames are sent via `sendWithDeadline` (deadline guard active per INV-F14).

**MUST NEVER:**

- A HistoryQuery in F's path MUST NEVER set `SensitiveOnly == false`.
- The handler MUST NEVER filter events by checking `row.DEKRef.Valid` or any package-private field — that's the cold-tier substrate's job (T0).
- The handler MUST NEVER decrement `events_scanned` or `decrypt_fail_count` — they're monotonic counters.
- Public/identity-codec events MUST NEVER reach `streamData`'s send loop (filtered at the cold-tier SQL boundary via T0's SensitiveOnly).
- An `Event` frame MUST NEVER be sent without first passing through `sendWithDeadline` — direct `stream.Send` calls in the loop are forbidden.

**Validation of done:**

- Universal gate PASS.
- All four named tests PASS.
- `task lint` PASS.

**Commit message:** `feat(readstream): streamData loop with classifier + SensitiveOnly query (sub-epic F INV-F15)`

---

### Task 19: ConnectRPC adapter at `internal/admin/socket/read_stream_handler.go`

**Files:**

- Create: `internal/admin/socket/read_stream_handler.go`
- Create: `internal/admin/socket/read_stream_handler_test.go`

**Goal:** Thin ConnectRPC adapter mirroring `rekey_handler.go`. Routes `AdminReadStream` RPC calls to the readstream package's `Handler.Handle`.

**Spec reference:** §2.1 ConnectRPC adapter.

**TDD acceptance criteria:**

- `TestAdminReadStreamConnectHandler_Delegates` — start a real `httptest.Server` mounting the adminv1connect handler with a stub `ReadStreamRPCHandler`; invoke `AdminReadStream` via the generated ConnectRPC client; assert the stub's `AdminReadStream` method was called.

**Implementation notes:**

- Read `internal/admin/socket/rekey_connect_handler_test.go` end-to-end first — it's the canonical pattern for testing a ConnectRPC server-streaming handler in-process.
- Surface: `ReadStreamRPCHandler` interface (so tests substitute a stub), `NewAdminReadStreamConnectHandler(h ReadStreamRPCHandler)` returning the ConnectRPC handler closure.
- The mount helper: mirror the rekey pattern's `httptest.Server` setup — use the generated `adminv1connect.NewAdminServiceHandler` (or whichever generated factory ConnectRPC produces) to mount the routes.

**MUST PASS:**

- `TestAdminReadStreamConnectHandler_Delegates`: an in-process `httptest.Server` + adminv1connect client invocation reaches the stub handler; assertion confirms `stub.AdminReadStream` was called with the matching request body.
- The adapter satisfies the generated `adminv1connect.AdminServiceHandler` interface for the `AdminReadStream` method.

**MUST NEVER:**

- The adapter MUST NEVER add business logic; it is a 1:1 pass-through to `ReadStreamRPCHandler`.
- The test MUST NEVER use `_ = h` + assertion-against-unset-state — the handler MUST actually be invoked via a real ConnectRPC client round-trip.
- The adapter MUST NEVER own session token validation, capability checks, or any other concern handled by the readstream Handler.

**Validation of done:**

- Universal gate PASS.
- `task test -- ./internal/admin/socket/` PASS.

**Commit message:** `feat(admin/socket): AdminReadStream ConnectRPC adapter (sub-epic F)`

---

### Task 20: Register `ReadStreamHandler` in `AdminSocketSubsystem.Config`

**Files:**

- Modify: `internal/admin/socket/subsystem.go`
- Modify: `internal/admin/socket/server.go` (or wherever the composite handler is built — grep for the rekey wiring)
- Modify: `internal/admin/socket/subsystem_test.go`

**Goal:** Wire the ConnectRPC adapter into the composite admin-socket handler so the route `AdminService/AdminReadStream` is mounted at boot.

**Spec reference:** §2.1 ConnectRPC wiring.

**TDD acceptance criteria:**

- `TestSubsystem_ReadStreamHandlerWired` — construct a `Subsystem` with a stub `ReadStreamHandler`; assert subsystem starts without error and the AdminReadStream route is reachable on its test HTTP surface.

**Implementation notes:**

- Read `internal/admin/socket/subsystem.go` for the existing `RekeyHandler` field on `Config` — mirror its pattern. Add `ReadStreamHandler ReadStreamRPCHandler` alongside.
- Read the composite-handler builder (likely `server.go` or `handlers.go`) — register the new RPC alongside the rekey RPCs by calling `NewAdminReadStreamConnectHandler(cfg.ReadStreamHandler)` and adding the route to the path multiplexer using the generated `adminv1connect` factory.
- Implementer reads + cites the rekey-handler registration site exactly to avoid drift.

**MUST PASS:**

- `TestSubsystem_ReadStreamHandlerWired`: subsystem starts with a populated `ReadStreamHandler`; the AdminReadStream route is reachable on the subsystem's HTTP surface.
- `Config.ReadStreamHandler` is a required-non-nil field (constructor or `Validate()` rejects nil with `INVALID_REGISTRATION`-style error).

**MUST NEVER:**

- The subsystem MUST NEVER start when `ReadStreamHandler == nil` after this task lands. (Defensive — production wiring always populates it.)
- The route registration MUST NEVER drop or reorder rekey/authenticate/approve/resettotp routes — strictly additive.
- The composite handler builder MUST NEVER bypass the ConnectRPC adapter from Task 19.

**Validation of done:**

- Universal gate PASS.
- `task test -- ./internal/admin/socket/` PASS (no regressions in rekey or auth tests).

**Commit message:** `feat(admin/socket): wire ReadStreamHandler into composite handler (sub-epic F)`

---

### Task 21: `cmd/holomush/core.go::runCoreWithDeps` wiring

**Files:**

- Modify: `cmd/holomush/core.go`
- Modify: `internal/config/...` (or wherever `AdminSocketConfig` lives — `rg -n "type AdminSocketConfig struct" internal/` to locate)

**Goal:** Production-boot wiring. Construct the F-local components, register the operator-read chain handler in the existing `VerifierSubsystem`, build the `SessionAuthorizer` adapter wrapping D's substrate, and hand the assembled `Handler` to the admin socket subsystem.

**Spec reference:** §2.5.

**TDD acceptance criteria:**

- `TestRunCoreWithDeps_ReadStreamHandlerWired` — smoke test: start the core with a representative config; assert `adminSocketCfg.ReadStreamHandler != nil` and the production `OperatorReadAuthGuard` instance is the one wired into `readstreamHistory`'s `WithHistoryAuth`. (Per INV-F5.)

**Implementation notes:**

- Build a `SessionAuthorizer` adapter inline (anonymous-struct or small named-struct) that closes over the existing D-shipped `sessions` + `grants` substrate from earlier wiring lines. The adapter's `AuthorizeOperator(ctx, token, capability)` does: `session, err := sessions.GetOperatorSession(token)` → on err return wrapped; `ok, err := access.HasPlayerGrant(ctx, grants, session.Identity.PlayerID, capability)` → on err or `!ok` return `DENY_OPERATOR_CAPABILITY`; build and return the `Operator` struct.
- Construct `TypeRegistry := codec.NewTypeRegistry()` + `codec.RegisterBuiltinTypes(typeRegistry)`.
- Construct `operatorReadHandler := readstream.OperatorReadHandlerFor(cfg.Game)`.
- Construct `operatorReadEmitter := readstream.NewOperatorReadAuditEmitter(chain.NewEmitter(chainRepo), busPublisher, operatorReadHandler)`.
- Extend the existing `chain.NewVerifierSubsystem(chain.VerifierSubsystemConfig{...})` construction (locate via `rg -n "chain.NewVerifierSubsystem" cmd/`) to include `operatorReadHandler` in its `Handlers` slice alongside `policy.PolicySetHandlerFor(...)` and `dek.RekeyHandlerFor(...)`.
- Construct `readstreamHistory := history.NewReader(..., history.WithHistoryAuth(readstream.NewOperatorReadAuthGuard(), dekMgr, readstream.NewNoOpDecryptAuditEmitter()))`. The auth guard wired here is the F-local one — INV-F5 depends on this exclusivity.
- Construct `readStreamHandler := readstream.NewHandler(readstream.Config{...})` with all dependencies populated.
- `adminSocketCfg.ReadStreamHandler = readStreamHandler`.
- Config defaults: `OperatorReadDefaultWindow=1h, OperatorReadMaxWindow=30d, OperatorReadWriteDeadline=30s, OperatorReadApprovalTTL=5m`. Startup WARN if `OperatorReadMaxWindow > 90d`.
- Add the four config fields to `AdminSocketConfig` + default population.

**MUST PASS:**

- `TestRunCoreWithDeps_ReadStreamHandlerWired`: smoke test confirms `adminSocketCfg.ReadStreamHandler != nil` after wiring.
- The production `VerifierSubsystem` is constructed with all three handlers in `Handlers []chain.Handler`: `policy.PolicySetHandlerFor`, `dek.RekeyHandlerFor`, AND `readstream.OperatorReadHandlerFor`.
- The HistoryReader wired into `readstream.Handler.Config.History` was constructed via `history.NewReader(..., history.WithHistoryAuth(*OperatorReadAuthGuard, dekMgr, *NoOpDecryptAuditEmitter))`. The auth guard is verifiably the F-local one (INV-F5).
- Config defaults: `OperatorReadDefaultWindow=1h`, `OperatorReadMaxWindow=30d`, `OperatorReadWriteDeadline=30s`, `OperatorReadApprovalTTL=5m`. WARN log fires if `OperatorReadMaxWindow > 90d`.
- `task build` succeeds.

**MUST NEVER:**

- The production wiring MUST NEVER pass a non-`*OperatorReadAuthGuard` instance into F's HistoryReader (INV-F5).
- The wiring MUST NEVER omit `RegisterBuiltinTypes(typeRegistry)` — F's validation depends on the four built-in types being present.
- The wiring MUST NEVER bypass the SessionAuthorizer adapter — `Auth: operatorAuthProvider` (direct assignment) does NOT work because D's `OperatorAuthProvider` only has `Authenticate`, not `AuthorizeOperator`. The adapter is mandatory.
- The startup WARN log MUST NEVER be silent for `MaxWindow > 90d` configurations.

**Validation of done:**

- Universal gate PASS.
- `task build` PASS.
- `task test -- ./cmd/holomush/` PASS (existing wiring + smoke tests green).
- `task lint` PASS.

**Commit message:** `feat(core): wire AdminReadStream subsystem into runCoreWithDeps (sub-epic F)`

---

### Task 22: `holomush admin read-stream` CLI subcommand

**Files:**

- Create: `cmd/holomush/cmd_admin_read_stream.go`
- Create: `cmd/holomush/cmd_admin_read_stream_test.go`

**Goal:** Operator-facing CLI. Reads stdin/flags, dials the admin UDS, opens the ConnectRPC stream, renders frames to stdout/stderr, exits with the appropriate sysexits.h code.

**Spec reference:** §5.

**TDD acceptance criteria:**

- `TestParseContextFlag_SingleID` — `scene:01HZAVGE83MGFEXQQH5SP9NXKF` → `ContextRef{type: scene, ids: [01HZAVG...]}`.
- `TestParseContextFlag_DualID` — `dm:01A:01B` → `ContextRef{type: dm, ids: [01A, 01B]}`.
- `TestParseContextFlag_NIDsAllowed` — `trade:01A:01B:01C` → 3-id ref.
- `TestParseContextFlag_NoColon` — bare string → error.
- `TestExitCodeForError_AuditEmitFailure` — `DENY_AUDIT_PRE_DATA_PUBLISH` → 70.
- `TestExitCodeForError_OperatorCapability` — `DENY_OPERATOR_CAPABILITY` → 77.
- `TestExitCodeForError_WindowTooLarge` — `DENY_OPERATOR_READ_WINDOW_TOO_LARGE` → 65.
- `TestExitCodeForTerminatedBy_CleanExit` — `TERMINATED_BY_CLIENT_EOF` → 0.
- `TestExitCodeForTerminatedBy_DeadlineExceeded` — `TERMINATED_BY_DEADLINE_EXCEEDED` → 75.

**Implementation notes:**

- Read `cmd/holomush/cmd_crypto_rekey.go` end-to-end first — it has the canonical pattern for: socket dial setup, session token loading, ConnectRPC client construction, stream iteration, exit-code translation.
- Cobra subcommand registered under the existing `admin` parent command.
- Flag parsing: `--justification` (required), `--context` (repeatable), `--since`/`--until` (RFC3339), `--dual-control`, `--admin-socket`, `--output`.
- Frame renderer:
  - `PendingApproval` → stderr line with request_id + expires_at.
  - `ReadStarted` → stderr line with request_id + policy_hash + window + contexts.
  - `Event` → stdout line. If `metadata_only`, render `[redacted: <NoPlaintextReason>]`. Else render the decrypted payload (JSON or text).
  - `ReadFinished` → stderr summary line + exit-code translation.
- Exit-code map per spec §5.4 table.

**MUST PASS:**

- All four `TestParseContextFlag_*` tests pass (single-id, dual-id, n-id, no-colon error).
- All five `TestExitCodeFor*_*` tests pass with the exact exit-code mappings from spec §5.4 (0/64/65/69/70/75/77/78).
- `./bin/holomush admin read-stream --help` exits 0 and prints the full flag set documented in spec §5.1.
- The Cobra subcommand is registered under the `admin` parent command.

**MUST NEVER:**

- The CLI MUST NEVER send a colon-form context value on the wire — it parses `scene:01ABC` LOCALLY into a `ContextRef{type, ids}` and sends the structured form.
- The CLI MUST NEVER exit 0 on a server-side validation rejection (must return the appropriate 65/70/77 code).
- The CLI MUST NEVER attempt to read plaintext from a `metadata_only=true` Event frame — it renders `[redacted: <reason>]` to stdout.
- `--justification` MUST NEVER be omitted (Cobra `MarkFlagRequired` enforces this client-side).

**Validation of done:**

- Universal gate PASS.
- `task test -- ./cmd/holomush/` PASS.
- `task build` succeeds; `./bin/holomush admin read-stream --help` renders the help screen cleanly.

**Commit message:** `feat(cli): holomush admin read-stream subcommand (sub-epic F)`

---

### Task 23: E2E harness fold-in + happy-path scenarios (F-E1, F-E2, F-E13, F-E14, F-E17)

**Files:**

- Modify: `cmd/holomush/admin_authenticate_e2e_test.go` (add `Describe("AdminReadStream")` block + helpers)

**Goal:** Production-boot E2E coverage of the happy paths. Reuses `adminAuthEnv` (handles prometheus singleton + TOTP step-window + DB / NATS containers).

**Spec reference:** §7.2.

**TDD acceptance criteria:** Five Ginkgo `It` blocks under a `Describe("AdminReadStream")`:

- `F-E1: happy path single context` — seed 50 encrypted events under `scene:01H...`; invoke `AdminReadStream` with that context; assert 50 events received, 0 decrypt-fails, `TERMINATED_BY_CLIENT_EOF`.
- `F-E2: contexts omitted (whole-game wildcard), defaulted bounds` — seed events; invoke without contexts; assert at least the seeded events appear.
- `F-E13: multi-context k-way merge` — seed 10 events each under `scene:A` and `scene:B`; invoke with both contexts; assert 20 events received in timestamp order across the two subjects.
- `F-E14: sensitive-content filter (INV-F15)` — seed 5 encrypted events + 3 plain (non-encrypted) under same subject; invoke; assert exactly 5 events received, none filtered toward decrypt-fail count.
- `F-E17: classifier surface (INV-F12 producers)` — seed cold rows that will trigger `EVENTBUS_COLD_DEK_COLUMNS_MISSING` (2 rows) and `EVENTBUS_COLD_BAD_DEK_COLUMNS` (3 rows) via direct DB inserts; invoke; assert 5 metadata-only frames with reasons `NO_PLAINTEXT_REASON_DEK_MISSING` and `NO_PLAINTEXT_REASON_DEK_BAD_COLUMNS`.

**Implementation notes:**

- Helper `seedAdminReadStreamData(env, contexts, count)` mirrors E's `seedAdminRekeyDEK` pattern — pre-seed a DEK per context, publish encrypted events through the production NATS path, ensure cold tier persistence.
- Helper `env.RunAdminReadStream(args)` wraps the ConnectRPC client invocation and accumulates streamed frames into a view object exposing `EventCount()`, `DecryptFailCount()`, `TerminatedBy()`, `MetadataOnlyReasons()`, `EventsAreTimestampOrdered()`, `PendingApprovalCount()`, etc.
- Implementer reuses E's existing prometheus-singleton workaround in the `adminAuthEnv` fixture. Per the spec's harness-fold note (§7.3), don't refactor; just append a `Describe` block.

**MUST PASS (per scenario):**

- **F-E1:** exactly 50 events received in the stream; `events_scanned == 50`; `decrypt_fail_count == 0`; `TerminatedBy == CLIENT_EOF`; one `crypto.system.operator_read` audit row + one `crypto.system.operator_read_completed` audit row persisted in `events_audit`.
- **F-E2:** all encrypted events seeded under any subject are returned (whole-game wildcard works); `events_scanned >= seeded_count`; `TerminatedBy == CLIENT_EOF`.
- **F-E13:** exactly 20 events received; events emerge in global timestamp order (assert via comparing `Event.Timestamp` of consecutive frames).
- **F-E14:** exactly 5 events received (the 3 plain events are filtered server-side via `SensitiveOnly`); `events_scanned == 5`; `decrypt_fail_count == 0`.
- **F-E17:** exactly 5 metadata-only frames received; `MetadataOnlyReasons` contains both `NO_PLAINTEXT_REASON_DEK_MISSING` (2 frames) and `NO_PLAINTEXT_REASON_DEK_BAD_COLUMNS` (3 frames); `decrypt_fail_count == 5`.

**MUST NEVER (per scenario):**

- **F-E1/F-E2/F-E13:** no `metadata_only=true` frames emitted (no decrypt failures expected).
- **F-E2:** no events from a different game appear in the stream.
- **F-E14:** the 3 plain (identity-codec) events MUST NEVER appear in the stream nor count toward any counter.
- **F-E13:** events MUST NEVER appear out of timestamp order in the merged stream.
- **F-E17:** plaintext bytes for the 5 fail-class rows MUST NEVER leak through (`Event.Payload` is empty for metadata-only frames).
- All scenarios: the audit chain MUST verify cleanly post-run (`chain.Verifier.VerifyScope` returns nil for the operator_read scope).

**Validation of done:**

- Universal gate PASS.
- `task test:int -- -run "AdminReadStream" ./cmd/holomush/` PASS for all five scenarios.
- The Describe block compiles under `//go:build integration`.

**Commit message:** `test(e2e): readstream happy-path scenarios F-E1/E2/E13/E14/E17 (sub-epic F)`

---

### Task 24: E2E validation + INV-42 + capability + arity scenarios (F-E4, F-E8, F-E9, F-E11, F-E15)

**Files:**

- Modify: `cmd/holomush/admin_authenticate_e2e_test.go`

**Goal:** E2E coverage of the rejection paths.

**Spec reference:** §7.2.

**TDD acceptance criteria:** Five Ginkgo `It` blocks:

- `F-E4 (INV-42): inject audit-publish failure` — fixture toggle on the chain emitter to fail; invoke; assert `DENY_AUDIT_PRE_DATA_PUBLISH`, exit 70, zero data frames.
- `F-E8 (INV-F6): window > MaxWindow rejected` — invoke with 31-day window; assert `DENY_OPERATOR_READ_WINDOW_TOO_LARGE`, exit 65, zero audit events emitted (rejection precedes pre-data audit).
- `F-E9: justification validation` — two sub-cases: empty-after-trim + > 4096 bytes; assert respective deny codes.
- `F-E11 (INV-F3): missing crypto.operator capability` — revoke the capability for the wizard operator; invoke; assert `DENY_OPERATOR_CAPABILITY`, exit 77, zero audit events.
- `F-E15: variable-arity validation` — two sub-cases: `dm` with one id + `scene` with two ids; both → `DENY_OPERATOR_READ_CONTEXT_ARITY_MISMATCH`.

**Implementation notes:**

- For F-E4, the fixture-injectable audit failure goes through F-local seam — easiest is a `chainEmitter` field on `adminAuthEnv` set to a fault-injection wrapper.
- For F-E11, `env.RevokeCryptoOperator(playerID)` mirrors a similar helper in E's tests.

**MUST PASS (per scenario):**

- **F-E4:** CLI exit code == 70; oops code on the returned error == `DENY_AUDIT_PRE_DATA_PUBLISH`; `LastDataFrameCount == 0`.
- **F-E8:** CLI exit code == 65; oops code == `DENY_OPERATOR_READ_WINDOW_TOO_LARGE`; `events_audit` table has zero `crypto.system.operator_read` rows for this invocation's would-be request_id.
- **F-E9a (empty after trim):** oops code == `DENY_OPERATOR_READ_JUSTIFICATION_EMPTY`.
- **F-E9b (> 4096 bytes):** oops code == `DENY_OPERATOR_READ_JUSTIFICATION_TOO_LONG`.
- **F-E11:** CLI exit code == 77; oops code == `DENY_OPERATOR_CAPABILITY`; `events_audit` table has zero `crypto.system.operator_read` rows for this invocation.
- **F-E15a (dm arity 1):** oops code == `DENY_OPERATOR_READ_CONTEXT_ARITY_MISMATCH`.
- **F-E15b (scene arity 2):** oops code == `DENY_OPERATOR_READ_CONTEXT_ARITY_MISMATCH`.

**MUST NEVER (per scenario):**

- **F-E4/F-E8/F-E11:** zero data frames emitted on the stream (no `Event` frames, no `ReadStarted` frame — rejection precedes any data flow). The CLI MUST NEVER print any line to stdout.
- **F-E8/F-E11:** the audit chain MUST NEVER have a new `operator_read` row for this invocation — rejection precedes the pre-data audit (INV-F3, INV-F6).
- **F-E4:** despite audit failing, F MUST NEVER stream any data frame — the structural ordering in `handleInternal` prevents it.
- **F-E11:** the test fixture MUST NEVER restore `crypto.operator` capability before assertions complete (would mask the rejection).
- All scenarios: `EmitStart` MUST NEVER succeed when the validation/auth precondition fails.

**Validation of done:**

- Universal gate PASS.
- `task test:int -- -run "AdminReadStream" ./cmd/holomush/` PASS for all five scenarios.
- Exit-code translation table (spec §5.4) verified against actual CLI exits.

**Commit message:** `test(e2e): readstream validation scenarios F-E4/E8/E9/E11/E15 (sub-epic F)`

---

### Task 25: E2E dual-control + lifecycle scenarios (F-E3, F-E5, F-E6, F-E7, F-E10, F-E12, F-E16)

**Files:**

- Modify: `cmd/holomush/admin_authenticate_e2e_test.go`

**Goal:** E2E coverage of the lifecycle behaviors — dual-control happy + timeout, disconnect, deadline, chain verification, idempotent reuse.

**Spec reference:** §7.2.

**TDD acceptance criteria:** Seven Ginkgo `It` blocks:

- `F-E3: mixed decrypt — destroyed DEK rows surface STALE_DEK` — seed 100 events, destroy DEKs for 20 of them (via D's existing destroy mechanism); invoke; assert 100 events scanned, 20 metadata-only frames with `NO_PLAINTEXT_REASON_STALE_DEK` (resolver-fallback path).
- `F-E5: dual-control happy` — op A invokes with `--dual-control`; op B approves via `Approve` RPC; assert stream resumes, audit payload carries `approver_player_id`.
- `F-E6: dual-control timeout` — op A invokes; no approval before TTL; assert `ReadFinished{DUAL_CONTROL_TIMEOUT}`, exit 77.
- `F-E7: TERMINATED_BY_CLIENT_DISCONNECT mid-stream emits completion audit` — invoke against a 100-event seed; cancel client after 10 events; assert completion audit fires with `terminated_by=CLIENT_DISCONNECT` and `events_scanned < 100`.
- `F-E10 (INV-F14): per-frame write deadline trips on stuck consumer` — invoke against a 20-event seed with a deliberately slow consumer (sleep between Receives); set `WriteDeadline=100ms`; assert `TERMINATED_BY_DEADLINE_EXCEEDED`.
- `F-E12 (INV-F9): chain verification — completed.prev_hash == start.self_hash` — invoke happy path; then construct a `chain.Verifier` and call `VerifyScope` on the operator_read chain for the request_id; assert verification succeeds.
- `F-E16: idempotent dual-control reuse via GetByOpArgsHash` — invoke once with dual-control + approve; invoke a second time with identical args; assert second invocation produces no `PendingApproval` frame (GetByOpArgsHash returned the existing approval).

**Implementation notes:**

- For F-E5, the test fixture runs op A's invocation in a goroutine, captures the request_id from the PendingApproval frame, then has op B call `Approve` via D's existing Approve RPC.
- For F-E7, use ConnectRPC's context cancellation on the client; the server should observe and emit the completion audit.
- For F-E10, the slow consumer pattern: read 1 frame, sleep > WriteDeadline, attempt next read.
- For F-E12, the chain verifier is `chain.NewVerifier(chainRepo)` + `VerifyScope(ctx, OperatorReadHandlerFor(game), requestID)`.

**MUST PASS (per scenario):**

- **F-E3:** exactly 100 events flow through the stream; `events_scanned == 100`; `decrypt_fail_count == 20`; 20 frames are metadata-only with `NO_PLAINTEXT_REASON_STALE_DEK`; 80 frames carry decrypted plaintext.
- **F-E5:** stream resumes after Op B's `Approve` RPC; final `Event` frame count matches seed; `crypto.system.operator_read` audit row has non-nil `approver_player_id == Op B's PlayerID`; chain verifies post-run.
- **F-E6:** CLI exit code == 77; `LastTerminatedBy == TERMINATED_BY_DUAL_CONTROL_TIMEOUT`; `events_audit` has the pre-data `operator_read` row but ZERO `operator_read_completed` row (timeout fires before completion).
- **F-E7:** stream is cancelled by client after 10 events received; server emits `operator_read_completed` audit row with `terminated_by == "CLIENT_DISCONNECT"` and `events_scanned == 10` (or close — implementer rationalizes the count tolerance).
- **F-E10:** CLI exit code maps to deadline-exceeded; `LastTerminatedBy == TERMINATED_BY_DEADLINE_EXCEEDED`; `events_audit` has the pre-data `operator_read` row and the completed row with `terminated_by == "DEADLINE_EXCEEDED"`.
- **F-E12:** `chain.NewVerifier(chainRepo).VerifyScope(ctx, OperatorReadHandlerFor(game), requestID)` returns nil — chain hash linkage verifies start → completed for the request_id.
- **F-E16:** second invocation produces ZERO `PendingApproval` frames; idempotent reuse confirmed; both invocations' audit rows share the same approver_player_id reference.

**MUST NEVER (per scenario):**

- **F-E3:** plaintext for the 20 destroyed-DEK events MUST NEVER appear in the stream (Payload is empty on metadata-only frames).
- **F-E5:** Op A MUST NEVER be permitted to approve their own request (Op B must be a distinct player). If Op A tries to self-approve, `Repo.MarkApproved` MUST reject.
- **F-E6:** when the timeout fires, MUST NEVER emit any `Event` frame (no data phase entered).
- **F-E7:** after client cancellation, the server MUST NEVER continue streaming events to the now-closed connection.
- **F-E10:** the per-frame deadline MUST NEVER cap total stream duration — a healthy slow consumer (1 frame/sec) MUST work; only a STUCK consumer (no progress for 30s+) trips the deadline.
- **F-E12:** the chain verifier MUST NEVER find an unlinked or hash-broken entry; chain integrity is the integrity anchor.
- **F-E16:** the second invocation MUST NEVER block on a fresh `WaitForApproval` (the previous approval is reused).

**Validation of done:**

- Universal gate PASS.
- `task test:int -- -run "AdminReadStream" ./cmd/holomush/` PASS for all seven scenarios.
- `task test:int` PASS (zero regressions in other E2E suites).
- `task pr-prep` PASS (final CI-mirror gate — lint, fmt, schema, license, unit, integration, E2E all green).

**Commit message:** `test(e2e): readstream dual-control + lifecycle scenarios F-E3/E5/E6/E7/E10/E12/E16 (sub-epic F)`

---

### Task 26: INV-F meta-test — every INV-F[N] bound to exactly one test name (INV-F18)

**Files:**

- Create: `internal/admin/readstream/inv_meta_test.go`

**Goal:** Enforce the one-to-one INV → test-name binding discipline at test-run time. The meta-test discovers every test function whose name matches `TestINV_F<N>_.*` across F's surface (readstream package + any cross-package INV-F\* tests like the proto-Go parity in `internal/eventbus/`), and asserts each INV-F1..F17 is referenced by exactly one test name.

**Spec reference:** §6 INV-F18 + the `holomush-z51u` open follow-up bead for meta-test discipline.

**TDD acceptance criteria:**

- `TestINV_F_MetaInvariantsBoundToTests` — discovers every `TestINV_F<N>_*` symbol using either `runtime.Caller`-walking + reflection on the testing binary, OR a build-time grep against the test source files (pick whichever is cleaner). For each N in 1..17, asserts exactly one matching test name exists. Bare N (`TestINV_F1` with no suffix) does NOT count.

**MUST PASS:**

- After all prior tasks land, exactly 17 unique test-name suffixes exist for INV-F1..F17.
- The meta-test fails if any INV is unbound (zero matching tests) OR overbound (more than one test with the same `TestINV_F<N>_*` prefix). Overbinding is acceptable for the same INV with different test-name suffixes IF the suffixes describe distinct sub-assertions; the spec's coverage-matrix lists exactly one test name per INV — match that list.

**MUST NEVER:**

- The meta-test MUST NEVER hard-code expected test names — it discovers them. Otherwise renames to test names elsewhere silently desync.
- The meta-test MUST NEVER claim to enforce INV-F18 if its discovery mechanism misses any test file in the F surface.

**Validation of done:**

- Universal gate PASS.
- `task test -- -run TestINV_F_MetaInvariantsBoundToTests ./internal/admin/readstream/` PASS.
- After ALL prior tasks land + this meta-test passes: spec §6 INV-F18 is satisfied by construction.

**Commit message:** `test(readstream): INV-F meta-test enforcing one-to-one INV-test binding (sub-epic F INV-F18)`

---

## Bead chain structure

Materializes the 27 plan tasks into 24 `bd` issues under parent epic `holomush-jxo8.8`. Three selective merges collapse tiny coupled tasks (T7+T8, T13+T15, T19+T20). 25 `bd dep add` edges serialize file-ownership chains and substrate dependencies.

```text
holomush-jxo8.8                          (existing epic — AdminReadStream + pre-data audit + read-stream CLI)
├── holomush-jxo8.8.1                    HistoryQuery.SensitiveOnly substrate (T0)
├── holomush-jxo8.8.2                    NoPlaintextReason enum 4→7 + INV-F16 negative (T1)
├── holomush-jxo8.8.3                    codec.TypeRegistry + built-in types (T2)
├── holomush-jxo8.8.4                    approval.Repo.GetByOpArgsHash + migration 000036 (T3)
├── holomush-jxo8.8.5                    AdminReadStream proto wire surface (T4)
├── holomush-jxo8.8.6                    Builtin event-type registrations (T5)
├── holomush-jxo8.8.7                    Audit payload structs + sha256:<hex> encoders (T6)
├── holomush-jxo8.8.8                    HistoryReader auth+audit seams (T7+T8 merged)
├── holomush-jxo8.8.9                    Audit chain factories OperatorReadHandlerFor (T9)
├── holomush-jxo8.8.10                   OperatorReadAuditEmitter + INV-F10 metric (T10)
├── holomush-jxo8.8.11                   Decrypt-fail classifier matrix (T11)
├── holomush-jxo8.8.12                   ResolveBounds validation (T12)
├── holomush-jxo8.8.13                   Pure helpers: buildSubjects + sendWithDeadline (T13+T15 merged)
├── holomush-jxo8.8.14                   k-way HistoryStream merge (T14)
├── holomush-jxo8.8.15                   Handler + INV-42 + INV-F5 (T16)
├── holomush-jxo8.8.16                   Dual-control flow (T17)
├── holomush-jxo8.8.17                   streamData loop + INV-F15 query (T18)
├── holomush-jxo8.8.18                   ConnectRPC adapter + subsystem wiring (T19+T20 merged)
├── holomush-jxo8.8.19                   cmd/holomush/core.go production wiring (T21)
├── holomush-jxo8.8.20                   read-stream CLI subcommand (T22)
├── holomush-jxo8.8.21                   E2E happy-path scenarios (T23)
├── holomush-jxo8.8.22                   E2E validation scenarios (T24)
├── holomush-jxo8.8.23                   E2E lifecycle scenarios (T25)
└── holomush-jxo8.8.24                   INV-F meta-test (T26)
```

**Plan-reference anti-inference guard** (used verbatim in every bd-create description below):

> **Plan reference:** `docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md` § Task T\<n\>. **The implementer MUST read this task's full section verbatim and translate plan → code — do not infer design from this 8-section bead summary alone. Structural details (test names, MUST PASS / MUST NEVER assertions, Validation of done gate, real API precedent `file:line` references) live in the plan task body. The plan's "Verified substrate APIs (precedent table)" near the top maps every API the implementer needs to its real `path:line`. Deviation from the plan's canonical test names breaks the INV-F meta-test (T26 / `.24`).**

### Per-bead `bd create` blocks

```bash
bd create \
  --title "Phase 5 sub-epic F.1: HistoryQuery.SensitiveOnly substrate (T0)" \
  --type task --priority 2 --parent holomush-jxo8.8 \
  --description "$(cat <<'EOF'
**Goal:** Add `SensitiveOnly bool` field to `eventbus.HistoryQuery`; cold-tier appends `AND dek_ref IS NOT NULL` when set; hot-tier skips Sensitive=false at decode boundary. Default false preserves all existing callers.

**Design reference:** docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md §2.8 (HistoryQuery.SensitiveOnly substrate) + INV-F15.

**Plan reference:** docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md § Task T0. **The implementer MUST read this task's full section verbatim and translate plan → code — do not infer design from this 8-section bead summary alone. Structural details (test names, MUST PASS / MUST NEVER assertions, Validation of done gate, real API precedent file:line references) live in the plan task body. Deviation from the plan's canonical test names breaks the INV-F meta-test (T26 / .24).**

**TDD acceptance criteria:**
- `TestHistoryQuery_SensitiveOnly_ColdFiltersDekRefNull` (bus-integration)
- `TestHistoryQuery_SensitiveOnly_DefaultPreservesPublicRows` (bus-integration)
- `TestHistoryQuery_SensitiveOnly_HotSkipsNonSensitive` (bus-integration)

**Verification steps:**
- task test:int -- -run TestHistoryQuery_SensitiveOnly ./internal/eventbus/history/
- task test:int -- ./internal/eventbus/history/  (no regressions)
- task lint

**Files touched:**
- internal/eventbus/bus.go — add `SensitiveOnly bool` field to HistoryQuery
- internal/eventbus/history/cold_postgres.go — SQL builder `AND dek_ref IS NOT NULL` append
- internal/eventbus/history/hot_jetstream.go — decode-boundary skip
- internal/eventbus/history/sensitive_only_test.go (new; build tag integration)
- internal/eventbus/types.go — godoc update on Event.Sensitive field

**Dependencies:** none (root substrate).

**Out of scope:** plugin-declared sensitive context types (deferred); DB migration (column already exists).
EOF
)"

bd create \
  --title "Phase 5 sub-epic F.2: NoPlaintextReason enum 4→7 + INV-F16 negative (T1)" \
  --type task --priority 2 --parent holomush-jxo8.8 \
  --description "$(cat <<'EOF'
**Goal:** Expand NoPlaintextReason proto enum from 4 to 7 values (DEK_MISSING, DEK_BAD_COLUMNS, INTERNAL). Mirror in Go. Extend parity test. Add INV-F16 static-grep negative test asserting hot/cold stampers never emit new values.

**Design reference:** docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md §3.4 + INV-F16.

**Plan reference:** docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md § Task T1. **The implementer MUST read this task's full section verbatim and translate plan → code — do not infer design from this 8-section bead summary alone. Structural details (test names, MUST PASS / MUST NEVER assertions, Validation of done gate, real API precedent file:line references) live in the plan task body. Deviation from the plan's canonical test names breaks the INV-F meta-test (T26 / .24).**

**TDD acceptance criteria:**
- `TestINV_F16_NoPlaintextReasonProtoGoParity` (unit, table-test all 7 values)
- `TestINV_F16_HotColdStampersDoNotEmitNewValues` (unit, static-grep negative)

**Verification steps:**
- task proto:gen (regenerate bindings)
- task test -- -run TestINV_F16 ./internal/eventbus/
- task test -- ./internal/eventbus/...  (no regressions)

**Files touched:**
- api/proto/holomush/core/v1/core.proto — add 3 enum values
- internal/eventbus/types.go — Go mirror
- internal/eventbus/types_proto_sync_test.go — parity test extension + INV-F16 negative test

**Dependencies:** none (independent substrate).

**Out of scope:** any new value beyond DEK_MISSING/DEK_BAD_COLUMNS/INTERNAL (deferred per spec §1.2); stamping the new values from hot/cold tier code (forbidden by INV-F16).
EOF
)"

bd create \
  --title "Phase 5 sub-epic F.3: codec.TypeRegistry + built-in sensitive context types (T2)" \
  --type task --priority 2 --parent holomush-jxo8.8 \
  --description "$(cat <<'EOF'
**Goal:** New host-side closed-set registry of sensitive context types. Built-ins only (scene/dm/location/character). Plugin-declared types deferred.

**Design reference:** docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md §2.6.

**Plan reference:** docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md § Task T2. **The implementer MUST read this task's full section verbatim and translate plan → code — do not infer design from this 8-section bead summary alone. Structural details (test names, MUST PASS / MUST NEVER assertions, Validation of done gate, real API precedent file:line references) live in the plan task body. Deviation from the plan's canonical test names breaks the INV-F meta-test (T26 / .24).**

**TDD acceptance criteria:**
- `TestTypeRegistry_RegisterAndLookup`
- `TestTypeRegistry_RegisterDuplicate`
- `TestTypeRegistry_LookupUnknown`
- `TestRegisterBuiltinTypes_PopulatesFour`
- `TestRegisterBuiltinTypes_RejectsDoubleCall`

**Verification steps:**
- task test -- ./internal/eventbus/codec/
- task lint

**Files touched:**
- internal/eventbus/codec/typeregistry.go (new) — TypeRegistry interface + TypeRegistration struct + NewTypeRegistry + RegisterBuiltinTypes
- internal/eventbus/codec/typeregistry_test.go (new)

**Dependencies:** none.

**Out of scope:** plugin-loader integration; plugin-declared context types (deferred to follow-up epic); modifying `crypto_manifest.go`.
EOF
)"

bd create \
  --title "Phase 5 sub-epic F.4: approval.Repo.GetByOpArgsHash + migration 000036 (T3)" \
  --type task --priority 2 --parent holomush-jxo8.8 \
  --description "$(cat <<'EOF'
**Goal:** Add `GetByOpArgsHash` method to D's approval.Repo interface for idempotent dual-control reuse. All filters server-side. Add composite index migration 000036.

**Design reference:** docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md §2.7 + INV-F17.

**Plan reference:** docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md § Task T3. **The implementer MUST read this task's full section verbatim and translate plan → code — do not infer design from this 8-section bead summary alone. Structural details (test names, MUST PASS / MUST NEVER assertions, Validation of done gate, real API precedent file:line references) live in the plan task body. Deviation from the plan's canonical test names breaks the INV-F meta-test (T26 / .24).**

**TDD acceptance criteria:**
- `TestINV_F17_GetByOpArgsHashMatrix_ReturnsApproved`
- `TestINV_F17_GetByOpArgsHashMatrix_FiltersExpired`
- `TestINV_F17_GetByOpArgsHashFiltersOwnAuthor`
- `TestINV_F17_GetByOpArgsHashMatrix_FiltersUnapproved`
- `TestINV_F17_GetByOpArgsHashMatrix_TiebreakerMostRecent`

**Verification steps:**
- task test:int -- -run TestINV_F17 ./internal/admin/approval/
- task test:int -- ./internal/admin/approval/  (no regressions)

**Files touched:**
- internal/admin/approval/repo.go — extend Repo interface + Postgres impl
- internal/admin/approval/repo_integration_test.go — 5 new tests
- internal/store/migrations/000036_admin_approvals_op_args_hash_idx.up.sql (new) — composite index
- internal/store/migrations/000036_admin_approvals_op_args_hash_idx.down.sql (new)

**Dependencies:** none (F-touching-D substrate).

**Out of scope:** changes to D's existing Open/Get/MarkApproved/WaitForApproval; schema changes beyond the index.
EOF
)"

bd create \
  --title "Phase 5 sub-epic F.5: AdminReadStream proto wire surface (T4)" \
  --type task --priority 2 --parent holomush-jxo8.8 \
  --description "$(cat <<'EOF'
**Goal:** Define the wire surface: AdminReadStream rpc on AdminService; ContextRef variable-arity; AdminReadStreamResponse oneof {pending_approval, started, event, finished}; ReadFinished.TerminatedBy 7-value enum.

**Design reference:** docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md §3.2, §3.3.

**Plan reference:** docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md § Task T4. **The implementer MUST read this task's full section verbatim and translate plan → code — do not infer design from this 8-section bead summary alone. Structural details (test names, MUST PASS / MUST NEVER assertions, Validation of done gate, real API precedent file:line references) live in the plan task body. Deviation from the plan's canonical test names breaks the INV-F meta-test (T26 / .24).**

**TDD acceptance criteria:**
- `TestAdminReadStreamRequest_RoundTrip`
- `TestReadFinished_TerminatedByEnum`

**Verification steps:**
- task proto:gen
- task test -- ./pkg/proto/holomush/admin/v1/
- task lint

**Files touched:**
- api/proto/holomush/admin/v1/admin.proto — add AdminReadStream rpc + import
- api/proto/holomush/admin/v1/read_stream.proto (new) — request/response messages + ContextRef + PendingApproval + ReadStarted + ReadFinished

**Dependencies:** none.

**Out of scope:** ConnectRPC adapter (.18); CLI parsing of the wire format (.20).
EOF
)"

bd create \
  --title "Phase 5 sub-epic F.6: Builtin event-type registrations (T5)" \
  --type task --priority 2 --parent holomush-jxo8.8 \
  --description "$(cat <<'EOF'
**Goal:** Register `crypto.system.operator_read` and `crypto.system.operator_read_completed` in `internal/core/builtins.go` with DisplayTarget=EVENT_CHANNEL_AUDIT_ONLY, mirroring the crypto.system.rekey precedent at builtins.go:93.

**Design reference:** docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md §3.8 + INV-F13.

**Plan reference:** docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md § Task T5. **The implementer MUST read this task's full section verbatim and translate plan → code — do not infer design from this 8-section bead summary alone. Structural details (test names, MUST PASS / MUST NEVER assertions, Validation of done gate, real API precedent file:line references) live in the plan task body. Deviation from the plan's canonical test names breaks the INV-F meta-test (T26 / .24).**

**TDD acceptance criteria:**
- `TestINV_F13_BuiltinEventTypesRegistered`

**Verification steps:**
- task test -- -run TestINV_F13 ./internal/core/
- task test -- ./internal/core/  (no regressions)

**Files touched:**
- internal/core/builtins.go — append two builtins rows mirroring crypto.system.rekey shape
- internal/core/builtins_operator_read_test.go (new)

**Dependencies:** none.

**Out of scope:** any modifications to existing builtin rows (strictly additive).
EOF
)"

bd create \
  --title "Phase 5 sub-epic F.7: Audit payload structs + sha256:<hex> hash encoders (T6)" \
  --type task --priority 2 --parent holomush-jxo8.8 \
  --description "$(cat <<'EOF'
**Goal:** Define OperatorReadStartPayload + OperatorReadCompletedPayload Go types with `sha256:<hex>` string hash fields. Duplicate dek.encodeHash/encodeHashPtr locally (package-private in dek).

**Design reference:** docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md §3.5.

**Plan reference:** docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md § Task T6. **The implementer MUST read this task's full section verbatim and translate plan → code — do not infer design from this 8-section bead summary alone. Structural details (test names, MUST PASS / MUST NEVER assertions, Validation of done gate, real API precedent file:line references) live in the plan task body. Deviation from the plan's canonical test names breaks the INV-F meta-test (T26 / .24).**

**TDD acceptance criteria:**
- `TestEncodeHash_HasSha256Prefix`
- `TestEncodeHashPtr_NilForGenesis`
- `TestOperatorReadStartPayload_JSONRoundTrip`
- `TestOperatorReadCompletedPayload_JSONRoundTrip`

**Verification steps:**
- task test -- ./internal/admin/readstream/
- task lint

**Files touched:**
- internal/admin/readstream/audit_payload.go (new) — payload structs + encodeHash + encodeHashPtr
- internal/admin/readstream/audit_payload_test.go (new)

**Dependencies:** none (defines types that .9 and .10 consume).

**Out of scope:** chain factories (.9) and audit emitter (.10) that consume these types.
EOF
)"

bd create \
  --title "Phase 5 sub-epic F.8: HistoryReader auth+audit seams (OperatorReadAuthGuard + NoOpDecryptAuditEmitter; T7+T8 merged)" \
  --type task --priority 2 --parent holomush-jxo8.8 \
  --description "$(cat <<'EOF'
**Goal:** Two F-local types satisfying the HistoryReader.WithHistoryAuth seams: OperatorReadAuthGuard (always-permit; INV-F4) and NoOpDecryptAuditEmitter (per master spec §7.6 — one audit per invocation, not per event; INV-F12).

**Design reference:** docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md §2.4 + INV-F4 + INV-F12.

**Plan reference:** docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md § Task T7 AND Task T8 (merged into one bead since both implement WithHistoryAuth parameters and ship together). **The implementer MUST read both task sections verbatim and translate plan → code — do not infer design from this 8-section bead summary alone. Structural details (test names, MUST PASS / MUST NEVER assertions, Validation of done gate, real API precedent file:line references) live in the plan task bodies. Deviation from the plan's canonical test names breaks the INV-F meta-test (T26 / .24).**

**TDD acceptance criteria:**
- `TestINV_F4_OperatorReadAuthGuardAlwaysPermits` (T7)
- `TestINV_F12_NoOpDecryptAuditEmitterDoesNothing` (T8)
- Compile-time assertions for both: `var _ eventbus.SessionAuthGuard = (*OperatorReadAuthGuard)(nil)` and `var _ eventbus.SessionAuditEmitter = (*NoOpDecryptAuditEmitter)(nil)`

**Verification steps:**
- task test -- ./internal/admin/readstream/

**Files touched:**
- internal/admin/readstream/auth_guard.go (new) — OperatorReadAuthGuard
- internal/admin/readstream/auth_guard_test.go (new)
- internal/admin/readstream/decrypt_audit_emitter.go (new) — NoOpDecryptAuditEmitter
- internal/admin/readstream/decrypt_audit_emitter_test.go (new)

**Dependencies:** none.

**Out of scope:** Handler construction (.15) that wires these into HistoryReader.WithHistoryAuth.
EOF
)"

bd create \
  --title "Phase 5 sub-epic F.9: Audit chain factories OperatorReadChainFor + OperatorReadHandlerFor (T9)" \
  --type task --priority 2 --parent holomush-jxo8.8 \
  --description "$(cat <<'EOF'
**Goal:** Two factory functions mirroring dek.RekeyHandlerFor / policy.PolicySetHandlerFor. Path C chain shape: both event types (start + completed) share the same NATS subject prefix `events.<game>.system.operator_read`. All seven chain.Handler callbacks populated.

**Design reference:** docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md §3.6 + INV-F9.

**Plan reference:** docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md § Task T9. **The implementer MUST read this task's full section verbatim and translate plan → code — do not infer design from this 8-section bead summary alone. Structural details (test names, MUST PASS / MUST NEVER assertions, Validation of done gate, real API precedent file:line references) live in the plan task body. Deviation from the plan's canonical test names breaks the INV-F meta-test (T26 / .24).**

**TDD acceptance criteria:**
- `TestOperatorReadChainFor_SubjectPrefixSatisfiesINVE26`
- `TestOperatorReadHandlerFor_AllSevenFieldsPopulated`
- `TestOperatorReadHandler_SubjectRoundTrip`
- `TestOperatorReadHandler_ScopeFromSubjectRejectsBadPrefix`
- `TestOperatorReadHandler_PayloadCallbacksRoundTrip`

**Verification steps:**
- task test -- ./internal/admin/readstream/
- task lint

**Files touched:**
- internal/admin/readstream/chain.go (new) — OperatorReadChainFor + OperatorReadHandlerFor + 7 callbacks (operatorReadScopeFromPayload, canonicalizeOperatorReadPayload, operatorReadPrevHashOf, operatorReadSelfHashOf, decodeOperatorReadPayloadJSON)
- internal/admin/readstream/chain_test.go (new)

**Dependencies:** holomush-jxo8.8.7 (uses OperatorReadStartPayload / OperatorReadCompletedPayload field shapes for canonicalize).

**Out of scope:** audit emitter (.10) that uses these factories; production VerifierSubsystem wiring (.19).
EOF
)"

bd create \
  --title "Phase 5 sub-epic F.10: OperatorReadAuditEmitter + INV-F10 metric (T10)" \
  --type task --priority 2 --parent holomush-jxo8.8 \
  --description "$(cat <<'EOF'
**Goal:** Compose chain.Emitter (prev-hash via LoadEntriesByScope) with eventbus.Publisher (NATS publish). EmitStart + EmitCompleted. Register Prometheus counter holomush_admin_readstream_completed_audit_failures_total (INV-F10).

**Design reference:** docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md §3.7 + INV-F10.

**Plan reference:** docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md § Task T10. **The implementer MUST read this task's full section verbatim and translate plan → code — do not infer design from this 8-section bead summary alone. Structural details (test names, MUST PASS / MUST NEVER assertions, Validation of done gate, real API precedent file:line references) live in the plan task body. Deviation from the plan's canonical test names breaks the INV-F meta-test (T26 / .24).**

**TDD acceptance criteria:**
- `TestOperatorReadAuditEmitter_EmitStartGenesis`
- `TestOperatorReadAuditEmitter_EmitStartPropagatesPublishError`
- `TestOperatorReadAuditEmitter_EmitCompletedChainsToStart`
- `TestOperatorReadAuditEmitter_EmitCompletedRefusesWithoutStart`
- Unit test for the Prometheus counter increment on EmitCompleted failure.

**Verification steps:**
- task test -- ./internal/admin/readstream/
- task lint

**Files touched:**
- internal/admin/readstream/audit_emitter.go (new) — OperatorReadAuditEmitter + Prometheus counter registration
- internal/admin/readstream/audit_emitter_test.go (new)

**Dependencies:** holomush-jxo8.8.9 (chain.Handler factory); holomush-jxo8.8.7 (payload struct types).

**Out of scope:** Handler.emitCompletedBestEffort caller-side logging (covered in .15 Handler skeleton).
EOF
)"

bd create \
  --title "Phase 5 sub-epic F.11: Decrypt-fail classifier matrix (T11)" \
  --type task --priority 2 --parent holomush-jxo8.8 \
  --description "$(cat <<'EOF'
**Goal:** Pure mapping from cold-tier oops codes / sentinels to NoPlaintextReason. Matrix grounded in real production codes (EVENTBUS_COLD_DEK_COLUMNS_MISSING, EVENTBUS_COLD_BAD_DEK_COLUMNS, source.ErrMetadataOnly, ctx cancellation, INTERNAL catch-all).

**Design reference:** docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md §4.5 + INV-F12.

**Plan reference:** docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md § Task T11. **The implementer MUST read this task's full section verbatim and translate plan → code — do not infer design from this 8-section bead summary alone. Structural details (test names, MUST PASS / MUST NEVER assertions, Validation of done gate, real API precedent file:line references) live in the plan task body. Deviation from the plan's canonical test names breaks the INV-F meta-test (T26 / .24).**

**TDD acceptance criteria:**
- `TestINV_F12_ClassifierMatrix` (table-test covering all 7 branches)

**Verification steps:**
- task test -- ./internal/admin/readstream/

**Files touched:**
- internal/admin/readstream/classify.go (new) — Classifier interface + defaultClassifier
- internal/admin/readstream/classify_test.go (new)

**Dependencies:** holomush-jxo8.8.2 (uses expanded NoPlaintextReason enum).

**Out of scope:** any modification to cold-tier or dispatcher stamping behavior (per INV-F16); changing what fail-close codes the cold tier emits.
EOF
)"

bd create \
  --title "Phase 5 sub-epic F.12: ResolveBounds validation (T12)" \
  --type task --priority 2 --parent holomush-jxo8.8 \
  --description "$(cat <<'EOF'
**Goal:** Validate + canonicalize AdminReadStreamRequest into Resolved{Contexts, Since, Until} + ResolvedFlags{SinceDefaulted, UntilDefaulted}. All DENY_OPERATOR_READ_* rejection codes fire here. DM lex-canonicalize order-insensitive IDs.

**Design reference:** docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md §4.1 + INV-F6 + INV-F7.

**Plan reference:** docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md § Task T12. **The implementer MUST read this task's full section verbatim and translate plan → code — do not infer design from this 8-section bead summary alone. Structural details (test names, MUST PASS / MUST NEVER assertions, Validation of done gate, real API precedent file:line references) live in the plan task body. Deviation from the plan's canonical test names breaks the INV-F meta-test (T26 / .24).**

**TDD acceptance criteria:**
- `TestResolveBounds_DefaultsFromZeroBounds`
- `TestINV_F6_WindowTooLargeRejected`
- `TestResolveBounds_TimeInvertedRejected`
- `TestResolveBounds_FutureBoundRejected`
- `TestResolveBounds_JustificationEmpty`
- `TestResolveBounds_JustificationTooLong`
- `TestResolveBounds_ContextTypeUnknown`
- `TestResolveBounds_ContextArityMismatch`
- `TestResolveBounds_ContextIDMalformed`
- `TestResolveBounds_DMLexCanonicalized`
- `TestResolveBounds_TooManyContexts`

**Verification steps:**
- task test -- ./internal/admin/readstream/

**Files touched:**
- internal/admin/readstream/filter.go (new) — Request struct + Resolved + ResolvedFlags + ResolveBounds function
- internal/admin/readstream/filter_test.go (new)

**Dependencies:** holomush-jxo8.8.3 (uses codec.TypeRegistry.LookupType).

**Out of scope:** Handler integration of ResolveBounds (covered in .15).
EOF
)"

bd create \
  --title "Phase 5 sub-epic F.13: Pure helpers buildSubjects + sendWithDeadline (T13+T15 merged)" \
  --type task --priority 2 --parent holomush-jxo8.8 \
  --description "$(cat <<'EOF'
**Goal:** Two small pure-function helpers: buildSubjects (ContextRef list → []eventbus.Subject in modern dot form, no subjectxlate) and sendWithDeadline (generic write-deadline shim with ErrWriteDeadlineExceeded sentinel; INV-F14).

**Design reference:** docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md §4.3 + §4.6 + INV-F14.

**Plan reference:** docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md § Task T13 AND Task T15 (merged into one bead since both are tiny pure-function helpers with no inter-dependency). **The implementer MUST read both task sections verbatim and translate plan → code — do not infer design from this 8-section bead summary alone. Structural details (test names, MUST PASS / MUST NEVER assertions, Validation of done gate, real API precedent file:line references) live in the plan task bodies. Deviation from the plan's canonical test names breaks the INV-F meta-test (T26 / .24).**

**TDD acceptance criteria:**
- `TestBuildSubjects_EmptyContextsReturnsGameWildcard` (T13)
- `TestBuildSubjects_SingleContextArity1` (T13)
- `TestBuildSubjects_DMArity2` (T13)
- `TestBuildSubjects_MultipleContexts` (T13)
- `TestSendWithDeadline_FastPasses` (T15)
- `TestINV_F14_SendWithDeadlineTrips` (T15)
- `TestSendWithDeadline_PropagatesSenderError` (T15)

**Verification steps:**
- task test -- ./internal/admin/readstream/

**Files touched:**
- internal/admin/readstream/subjects.go (new) — subjectFor + buildSubjects
- internal/admin/readstream/subjects_test.go (new)
- internal/admin/readstream/deadline_writer.go (new) — ErrWriteDeadlineExceeded sentinel + sendWithDeadline generic helper
- internal/admin/readstream/deadline_writer_test.go (new)

**Dependencies:** none.

**Out of scope:** Handler integration of either helper (covered in .15 + .17).
EOF
)"

bd create \
  --title "Phase 5 sub-epic F.14: k-way HistoryStream merge by timestamp (T14)" \
  --type task --priority 2 --parent holomush-jxo8.8 \
  --description "$(cat <<'EOF'
**Goal:** When operator queries multiple contexts, F opens N HistoryStreams and merges by event Timestamp via a min-heap. Single-stream case short-circuits.

**Design reference:** docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md §4.3.

**Plan reference:** docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md § Task T14. **The implementer MUST read this task's full section verbatim and translate plan → code — do not infer design from this 8-section bead summary alone. Structural details (test names, MUST PASS / MUST NEVER assertions, Validation of done gate, real API precedent file:line references) live in the plan task body. Deviation from the plan's canonical test names breaks the INV-F meta-test (T26 / .24).**

**TDD acceptance criteria:**
- `TestMergeStreams_SingleStreamOrderedThrough`
- `TestMergeStreams_InterleaveByTimestamp`

**Verification steps:**
- task test -- ./internal/admin/readstream/

**Files touched:**
- internal/admin/readstream/stream_merge.go (new) — mergeStreams + heap.Interface impl + mergedStream wrapper
- internal/admin/readstream/stream_merge_test.go (new)

**Dependencies:** none.

**Out of scope:** Handler integration of mergeStreams (covered in .17 streamData).
EOF
)"

bd create \
  --title "Phase 5 sub-epic F.15: Handler skeleton + INV-42 ordering + INV-F5 (T16)" \
  --type task --priority 2 --parent holomush-jxo8.8 \
  --description "$(cat <<'EOF'
**Goal:** Ship the production Handler with auth (via F-local SessionAuthorizer wrapping sessions.GetOperatorSession + access.HasPlayerGrant), bounds validation, pre-data audit emit (INV-42), ReadStarted frame, streamData call, completion audit. ResponseSender narrow seam for test isolation. INV-F5 negative test for AuthGuard wiring.

**Design reference:** docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md §2.2 + §4.2 + INV-F1 + INV-F2 + INV-F3 + INV-F5.

**Plan reference:** docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md § Task T16. **The implementer MUST read this task's full section verbatim and translate plan → code — do not infer design from this 8-section bead summary alone. Structural details (test names, MUST PASS / MUST NEVER assertions, Validation of done gate, real API precedent file:line references) live in the plan task body. Deviation from the plan's canonical test names breaks the INV-F meta-test (T26 / .24).**

**TDD acceptance criteria:**
- `TestINV_F1_PreDataAuditOrdering`
- `TestINV_F2_AuditPublishFailRefuses`
- `TestINV_F3_CapabilityCheckPrecedesAudit`
- `TestINV_F5_StandardAuthGuardNeverSeesOperator`
- `TestINV_F10_CompletionAuditFailureNotRaised`

**Verification steps:**
- task test -- -run TestINV_F1 ./internal/admin/readstream/
- task test -- -run TestINV_F2 ./internal/admin/readstream/
- task test -- -run TestINV_F3 ./internal/admin/readstream/
- task test -- -run TestINV_F5 ./internal/admin/readstream/
- task lint

**Files touched:**
- internal/admin/readstream/handler.go (new) — Handler + Config + SessionAuthorizer + ResponseSender + Handle + handleInternal + buildStartedFrame + buildFinishedFrame + classifyTermination + wrap
- internal/admin/readstream/handler_test.go (new) — fakeAuth + fakeAuditEmitter + recordingStream + 5 INV-F tests

**Dependencies:** holomush-jxo8.8.7 + .8 + .9 + .10 + .11 + .12 + .13 + .14 (handler aggregates all F-local components).

**Out of scope:** dual-control branch (filled in .16); streamData loop body (filled in .17); production wiring (filled in .19).
EOF
)"

bd create \
  --title "Phase 5 sub-epic F.16: Dual-control flow via WaitForApproval + GetByOpArgsHash (T17)" \
  --type task --priority 2 --parent holomush-jxo8.8 \
  --description "$(cat <<'EOF'
**Goal:** Implement the dual-control branch in Handler.acquireApproval. First try GetByOpArgsHash for idempotent reuse; if NOT_FOUND, Open a new approval, send PendingApproval frame, block on WaitForApproval. Timeout → emit ReadFinished{DUAL_CONTROL_TIMEOUT}.

**Design reference:** docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md §2.3 + INV-F11.

**Plan reference:** docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md § Task T17. **The implementer MUST read this task's full section verbatim and translate plan → code — do not infer design from this 8-section bead summary alone. Structural details (test names, MUST PASS / MUST NEVER assertions, Validation of done gate, real API precedent file:line references) live in the plan task body. Deviation from the plan's canonical test names breaks the INV-F meta-test (T26 / .24).**

**TDD acceptance criteria:**
- `TestINV_F11_DualControlBlocksUntilApproval`
- `TestINV_F11_DualControlIdempotentReuse`
- `TestDualControlTimeout_EmitsFinishedAndReturnsErr`

**Verification steps:**
- task test -- -run TestINV_F11 ./internal/admin/readstream/
- task test -- -run TestDualControlTimeout ./internal/admin/readstream/

**Files touched:**
- internal/admin/readstream/handler.go — fill in acquireApproval (uses approval.ComputeOpArgsHash, Repo.GetByOpArgsHash, Repo.Open, Repo.WaitForApproval; constructs PendingApproval frame; handles approval-wait deadline / expiry / cancellation)
- internal/admin/readstream/handler_test.go — add fakeApprovalRepo + 3 INV-F11/timeout tests

**Dependencies:** holomush-jxo8.8.15 (Handler skeleton); holomush-jxo8.8.4 (approval.Repo.GetByOpArgsHash).

**Out of scope:** in-process pending-approval registry (forbidden by INV-F11); ApproveHandler mutation (D's substrate untouched).
EOF
)"

bd create \
  --title "Phase 5 sub-epic F.17: streamData loop + INV-F15 SensitiveOnly query (T18)" \
  --type task --priority 2 --parent holomush-jxo8.8 \
  --description "$(cat <<'EOF'
**Goal:** Implement Handler.streamData: for each resolved context open a HistoryQuery with SensitiveOnly=true, merge streams (T14), iterate events. Classify decrypt failures via T11's classifier; send Event frames via T15's sendWithDeadline. Stamp metadata-only frames for failures.

**Design reference:** docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md §4.4 + §4.5 + INV-F15.

**Plan reference:** docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md § Task T18. **The implementer MUST read this task's full section verbatim and translate plan → code — do not infer design from this 8-section bead summary alone. Structural details (test names, MUST PASS / MUST NEVER assertions, Validation of done gate, real API precedent file:line references) live in the plan task body. Deviation from the plan's canonical test names breaks the INV-F meta-test (T26 / .24).**

**TDD acceptance criteria:**
- `TestINV_F15_HandlerSetsSensitiveOnlyTrue`
- `TestStreamData_HappyPathEventsCounted`
- `TestStreamData_DecryptFailureProducesMetadataOnlyFrame`
- `TestStreamData_ClassifierFatalErrorBailsStream`

**Verification steps:**
- task test -- -run TestINV_F15 ./internal/admin/readstream/
- task test -- -run TestStreamData ./internal/admin/readstream/

**Files touched:**
- internal/admin/readstream/handler.go — fill in streamData (uses buildSubjects, HistoryReader.QueryHistory with SensitiveOnly:true, mergeStreams, classifier, sendWithDeadline, rowToProtoEvent helper)
- internal/admin/readstream/handler_test.go — add queryRecordingReader + 4 streamData tests

**Dependencies:** holomush-jxo8.8.15 (Handler skeleton); holomush-jxo8.8.16 (file ownership chain); holomush-jxo8.8.1 (HistoryQuery.SensitiveOnly substrate).

**Out of scope:** any DEKRef-field filtering on eventbus.Event (doesn't exist; filtering happens at cold-tier SQL via T0).
EOF
)"

bd create \
  --title "Phase 5 sub-epic F.18: ConnectRPC adapter + AdminSocketSubsystem wiring (T19+T20 merged)" \
  --type task --priority 2 --parent holomush-jxo8.8 \
  --description "$(cat <<'EOF'
**Goal:** Thin ConnectRPC adapter mirroring rekey_handler.go pattern. Register the AdminReadStream route on the composite admin-socket handler. Wire ReadStreamHandler field into AdminSocketSubsystem.Config.

**Design reference:** docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md §2.1 ConnectRPC wiring + §2.5.

**Plan reference:** docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md § Task T19 AND Task T20 (merged into one bead since both are small admin/socket wiring touches in lockstep). **The implementer MUST read both task sections verbatim and translate plan → code — do not infer design from this 8-section bead summary alone. Structural details (test names, MUST PASS / MUST NEVER assertions, Validation of done gate, real API precedent file:line references) live in the plan task bodies. Deviation from the plan's canonical test names breaks the INV-F meta-test (T26 / .24).**

**TDD acceptance criteria:**
- `TestAdminReadStreamConnectHandler_Delegates` (T19; httptest.Server + adminv1connect client invocation)
- `TestSubsystem_ReadStreamHandlerWired` (T20)

**Verification steps:**
- task test -- ./internal/admin/socket/

**Files touched:**
- internal/admin/socket/read_stream_handler.go (new) — ReadStreamRPCHandler interface + NewAdminReadStreamConnectHandler factory
- internal/admin/socket/read_stream_handler_test.go (new)
- internal/admin/socket/subsystem.go — add ReadStreamHandler field on Config; register route in composite handler builder
- internal/admin/socket/subsystem_test.go — add smoke test for wiring

**Dependencies:** holomush-jxo8.8.15 (Handler); holomush-jxo8.8.5 (proto).

**Out of scope:** production wiring of the Handler from cmd/holomush/core.go (covered in .19); CLI client invocation (covered in .20).
EOF
)"

bd create \
  --title "Phase 5 sub-epic F.19: cmd/holomush/core.go runCoreWithDeps production wiring (T21)" \
  --type task --priority 2 --parent holomush-jxo8.8 \
  --description "$(cat <<'EOF'
**Goal:** Production-boot wiring. Build SessionAuthorizer adapter wrapping D's sessions + access.HasPlayerGrant. Construct TypeRegistry + RegisterBuiltinTypes. Register operator-read chain handler in VerifierSubsystem. Build readstream Handler with all dependencies. Hand to admin socket subsystem. Add config defaults + WARN log for MaxWindow > 90d.

**Design reference:** docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md §2.5.

**Plan reference:** docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md § Task T21. **The implementer MUST read this task's full section verbatim and translate plan → code — do not infer design from this 8-section bead summary alone. Structural details (test names, MUST PASS / MUST NEVER assertions, Validation of done gate, real API precedent file:line references) live in the plan task body. Deviation from the plan's canonical test names breaks the INV-F meta-test (T26 / .24).**

**TDD acceptance criteria:**
- `TestRunCoreWithDeps_ReadStreamHandlerWired`

**Verification steps:**
- task build
- task test -- ./cmd/holomush/
- task lint

**Files touched:**
- cmd/holomush/core.go — extend runCoreWithDeps with the F substrate construction + Handler wiring
- internal/config/... (or cmd/holomush/config.go — locate via `rg -n "type AdminSocketConfig struct" internal/`) — add 4 new config fields (OperatorReadDefaultWindow, OperatorReadMaxWindow, OperatorReadWriteDeadline, OperatorReadApprovalTTL) + defaults

**Dependencies:** holomush-jxo8.8.18 (ConnectRPC adapter + subsystem); holomush-jxo8.8.1 (SensitiveOnly substrate); holomush-jxo8.8.3 (TypeRegistry); holomush-jxo8.8.4 (GetByOpArgsHash); holomush-jxo8.8.6 (builtin event types).

**Out of scope:** CLI changes (covered in .20).
EOF
)"

bd create \
  --title "Phase 5 sub-epic F.20: holomush admin read-stream CLI subcommand (T22)" \
  --type task --priority 2 --parent holomush-jxo8.8 \
  --description "$(cat <<'EOF'
**Goal:** Operator-facing CLI. Cobra subcommand under admin parent. Parses --context flag locally into ContextRef. Opens ConnectRPC stream on admin UDS. Renders frames to stdout/stderr per spec §5. Exit codes per sysexits.h table.

**Design reference:** docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md §5.

**Plan reference:** docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md § Task T22. **The implementer MUST read this task's full section verbatim and translate plan → code — do not infer design from this 8-section bead summary alone. Structural details (test names, MUST PASS / MUST NEVER assertions, Validation of done gate, real API precedent file:line references) live in the plan task body. Deviation from the plan's canonical test names breaks the INV-F meta-test (T26 / .24).**

**TDD acceptance criteria:**
- `TestParseContextFlag_SingleID`
- `TestParseContextFlag_DualID`
- `TestParseContextFlag_NIDsAllowed`
- `TestParseContextFlag_NoColon`
- `TestExitCodeForError_AuditEmitFailure` (70)
- `TestExitCodeForError_OperatorCapability` (77)
- `TestExitCodeForError_WindowTooLarge` (65)
- `TestExitCodeForTerminatedBy_CleanExit` (0)
- `TestExitCodeForTerminatedBy_DeadlineExceeded` (75)

**Verification steps:**
- task test -- ./cmd/holomush/
- task build && ./bin/holomush admin read-stream --help

**Files touched:**
- cmd/holomush/cmd_admin_read_stream.go (new) — Cobra subcommand + parseContextFlag + frame renderer + exit-code translator
- cmd/holomush/cmd_admin_read_stream_test.go (new)

**Dependencies:** holomush-jxo8.8.5 (proto wire); holomush-jxo8.8.19 (production socket exists).

**Out of scope:** any TUI / interactive features; output formats beyond text|json|ndjson.
EOF
)"

bd create \
  --title "Phase 5 sub-epic F.21: E2E happy-path scenarios F-E1/E2/E13/E14/E17 (T23)" \
  --type task --priority 2 --parent holomush-jxo8.8 \
  --description "$(cat <<'EOF'
**Goal:** Production-boot E2E. Add Describe("AdminReadStream") block to admin_authenticate_e2e_test.go reusing adminAuthEnv. Five happy-path scenarios.

**Design reference:** docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md §7.2.

**Plan reference:** docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md § Task T23. **The implementer MUST read this task's full section verbatim and translate plan → code — do not infer design from this 8-section bead summary alone. Structural details (test names, MUST PASS / MUST NEVER assertions, Validation of done gate, real API precedent file:line references) live in the plan task body. Deviation from the plan's canonical test names breaks the INV-F meta-test (T26 / .24).**

**TDD acceptance criteria:** (Ginkgo It blocks; build tag integration)
- F-E1: happy path single context (50 events)
- F-E2: contexts omitted → whole-game wildcard, defaulted bounds
- F-E13: multi-context k-way merge by timestamp
- F-E14: sensitive-content filter (3 plain events dropped, 5 encrypted received)
- F-E17: classifier surface — seeded DEK_COLUMNS_MISSING + BAD_DEK_COLUMNS rows surface as DEK_MISSING + DEK_BAD_COLUMNS reasons

**Verification steps:**
- task test:int -- -run "AdminReadStream" ./cmd/holomush/

**Files touched:**
- cmd/holomush/admin_authenticate_e2e_test.go — add Describe("AdminReadStream") block + seedAdminReadStreamData helper + RunAdminReadStream helper + 5 It blocks

**Dependencies:** holomush-jxo8.8.19 (production wiring exists); holomush-jxo8.8.20 (CLI exists).

**Out of scope:** validation rejection scenarios (.22); dual-control + lifecycle scenarios (.23).
EOF
)"

bd create \
  --title "Phase 5 sub-epic F.22: E2E validation scenarios F-E4/E8/E9/E11/E15 (T24)" \
  --type task --priority 2 --parent holomush-jxo8.8 \
  --description "$(cat <<'EOF'
**Goal:** E2E rejection paths. Five Ginkgo It blocks covering INV-42 audit-failure refusal, window-too-large, justification validation, capability rejection, context-arity validation.

**Design reference:** docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md §7.2.

**Plan reference:** docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md § Task T24. **The implementer MUST read this task's full section verbatim and translate plan → code — do not infer design from this 8-section bead summary alone. Structural details (test names, MUST PASS / MUST NEVER assertions, Validation of done gate, real API precedent file:line references) live in the plan task body. Deviation from the plan's canonical test names breaks the INV-F meta-test (T26 / .24).**

**TDD acceptance criteria:** (Ginkgo It blocks)
- F-E4: INV-42 audit-publish failure → exit 70, zero data frames
- F-E8: window > MaxWindow → exit 65, zero audit emitted
- F-E9: justification empty + too-long (two sub-cases)
- F-E11: missing crypto.operator capability → exit 77, zero audit
- F-E15: variable-arity validation (dm with 1 id; scene with 2 ids)

**Verification steps:**
- task test:int -- -run "AdminReadStream" ./cmd/holomush/

**Files touched:**
- cmd/holomush/admin_authenticate_e2e_test.go — append 5 It blocks to the Describe("AdminReadStream") from .21

**Dependencies:** holomush-jxo8.8.21 (Describe block + harness helpers).

**Out of scope:** dual-control + lifecycle scenarios (.23).
EOF
)"

bd create \
  --title "Phase 5 sub-epic F.23: E2E dual-control + lifecycle scenarios F-E3/E5/E6/E7/E10/E12/E16 (T25)" \
  --type task --priority 2 --parent holomush-jxo8.8 \
  --description "$(cat <<'EOF'
**Goal:** E2E lifecycle coverage. Seven Ginkgo It blocks: mixed decrypt (STALE_DEK), dual-control happy, dual-control timeout, client disconnect, write-deadline trip, chain verification, idempotent dual-control reuse.

**Design reference:** docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md §7.2.

**Plan reference:** docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md § Task T25. **The implementer MUST read this task's full section verbatim and translate plan → code — do not infer design from this 8-section bead summary alone. Structural details (test names, MUST PASS / MUST NEVER assertions, Validation of done gate, real API precedent file:line references) live in the plan task body. Deviation from the plan's canonical test names breaks the INV-F meta-test (T26 / .24).**

**TDD acceptance criteria:** (Ginkgo It blocks)
- F-E3: mixed decrypt — 100 events, 20 with destroyed DEK → 20 metadata-only with STALE_DEK
- F-E5: dual-control happy — Op A blocks, Op B approves, audit carries approver_player_id
- F-E6: dual-control timeout → ReadFinished{DUAL_CONTROL_TIMEOUT}, exit 77
- F-E7: TERMINATED_BY_CLIENT_DISCONNECT mid-stream emits completion audit
- F-E10: per-frame write deadline trips on stuck consumer
- F-E12: chain verification — completed.prev_hash == start.self_hash via chain.Verifier
- F-E16: idempotent dual-control reuse via GetByOpArgsHash (no second PendingApproval frame)

**Verification steps:**
- task test:int -- -run "AdminReadStream" ./cmd/holomush/
- task test:int  (no regressions)
- task pr-prep   (final CI-mirror gate)

**Files touched:**
- cmd/holomush/admin_authenticate_e2e_test.go — append 7 It blocks to the Describe("AdminReadStream")

**Dependencies:** holomush-jxo8.8.22 (file ownership chain on admin_authenticate_e2e_test.go).

**Out of scope:** any further E2E scenarios beyond the 17 in spec §7.2.
EOF
)"

bd create \
  --title "Phase 5 sub-epic F.24: INV-F meta-test (INV-F18) (T26)" \
  --type task --priority 2 --parent holomush-jxo8.8 \
  --description "$(cat <<'EOF'
**Goal:** Enforce one-to-one INV-F[N] → test-name binding via a static-analysis-style test that discovers every TestINV_F<N>_* symbol across F's surface and asserts each INV-F1..F17 is referenced by exactly one test name.

**Design reference:** docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md §6 INV-F18 + the holomush-z51u open follow-up bead for meta-test discipline.

**Plan reference:** docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md § Task T26. **The implementer MUST read this task's full section verbatim and translate plan → code — do not infer design from this 8-section bead summary alone. Structural details (test names, MUST PASS / MUST NEVER assertions, Validation of done gate, real API precedent file:line references) live in the plan task body. Deviation from the plan's canonical test names breaks the INV-F meta-test itself (this task).**

**TDD acceptance criteria:**
- `TestINV_F_MetaInvariantsBoundToTests` — passes when exactly 17 unique TestINV_F<N>_* test-name suffixes exist (one per INV-F1..F17) across the F surface (internal/admin/readstream/, internal/eventbus/, internal/eventbus/codec/, internal/admin/approval/, internal/core/, internal/admin/socket/).

**Verification steps:**
- task test -- -run TestINV_F_MetaInvariantsBoundToTests ./internal/admin/readstream/

**Files touched:**
- internal/admin/readstream/inv_meta_test.go (new)

**Dependencies:** holomush-jxo8.8.23 (every prior bead must be done so the discovery finds the right set of TestINV_F* names).

**Out of scope:** discovery via `go/parser` AST (overkill); enforcement of test naming conventions beyond INV-F* prefix.
EOF
)"
```

### `bd dep add` edges (25 total)

```bash
# F-local chain wiring (audit emitter consumes handler factory + payload types)
bd dep add holomush-jxo8.8.10 holomush-jxo8.8.9
bd dep add holomush-jxo8.8.10 holomush-jxo8.8.7

# Substrate consumers
bd dep add holomush-jxo8.8.11 holomush-jxo8.8.2   # classifier needs expanded enum
bd dep add holomush-jxo8.8.12 holomush-jxo8.8.3   # ResolveBounds needs TypeRegistry

# Handler skeleton needs all F-local components
bd dep add holomush-jxo8.8.15 holomush-jxo8.8.8   # auth+audit seams
bd dep add holomush-jxo8.8.15 holomush-jxo8.8.9   # chain handler factory
bd dep add holomush-jxo8.8.15 holomush-jxo8.8.10  # audit emitter
bd dep add holomush-jxo8.8.15 holomush-jxo8.8.11  # classifier
bd dep add holomush-jxo8.8.15 holomush-jxo8.8.12  # ResolveBounds
bd dep add holomush-jxo8.8.15 holomush-jxo8.8.13  # buildSubjects + deadline writer
bd dep add holomush-jxo8.8.15 holomush-jxo8.8.14  # stream merge

# Handler.go file-ownership serialization (.15 owns; .16 + .17 modify in order)
bd dep add holomush-jxo8.8.16 holomush-jxo8.8.15
bd dep add holomush-jxo8.8.17 holomush-jxo8.8.16

# Dual-control needs GetByOpArgsHash; streamData needs SensitiveOnly
bd dep add holomush-jxo8.8.16 holomush-jxo8.8.4
bd dep add holomush-jxo8.8.17 holomush-jxo8.8.1

# ConnectRPC adapter + subsystem
bd dep add holomush-jxo8.8.18 holomush-jxo8.8.15  # adapter needs Handler
bd dep add holomush-jxo8.8.18 holomush-jxo8.8.5   # adapter needs proto

# Production wiring needs ConnectRPC, SensitiveOnly, TypeRegistry, GetByOpArgsHash, builtins
bd dep add holomush-jxo8.8.19 holomush-jxo8.8.18
bd dep add holomush-jxo8.8.19 holomush-jxo8.8.1
bd dep add holomush-jxo8.8.19 holomush-jxo8.8.3
bd dep add holomush-jxo8.8.19 holomush-jxo8.8.4
bd dep add holomush-jxo8.8.19 holomush-jxo8.8.6

# CLI needs proto + production wiring
bd dep add holomush-jxo8.8.20 holomush-jxo8.8.5
bd dep add holomush-jxo8.8.20 holomush-jxo8.8.19

# E2E file-ownership serialization (admin_authenticate_e2e_test.go)
bd dep add holomush-jxo8.8.21 holomush-jxo8.8.19  # E2E needs production wiring
bd dep add holomush-jxo8.8.21 holomush-jxo8.8.20  # E2E needs CLI
bd dep add holomush-jxo8.8.22 holomush-jxo8.8.21
bd dep add holomush-jxo8.8.23 holomush-jxo8.8.22

# Meta-test runs after everything else
bd dep add holomush-jxo8.8.24 holomush-jxo8.8.23
```

### Closing-out operations

- **Beads to close:** none (sub-epic F has no prior children).
- **Beads to update:** none (parent epic `holomush-jxo8.8` retains its existing title + description; the materialization records `Plan reference` in the parent epic's notes via `bd note holomush-jxo8.8 "..."` after T0-T26 land).
- **Follow-up beads to file:** none pre-filed. Discoveries during implementation get filed via `bd create` parented to `holomush-jxo8` (sub-epic root), not `.8`.

- [ ] All 18 `INV-F<N>` tests named per the coverage matrix in spec §6.
- [ ] `TestINV_F_MetaInvariantsBoundToTests` passes (meta-test enforcing one-to-one INV → test-name binding).
- [ ] `task pr-prep` runs green end-to-end.
- [ ] `bd close holomush-jxo8.8` only after all child beads close.

## Self-review

**Spec coverage** (against spec §1.1):

| Spec §1.1 item | Plan task(s) |
| -------------- | ------------ |
| 1. AdminReadStream RPC | T4 |
| 2. OperatorAuthProvider gate | T16, T21 |
| 3. Pre-data audit emit + INV-42 refusal | T9, T10, T16 |
| 4. Operator-read chain registration (Path C) | T9, T21 |
| 5. F-local OperatorReadAuditEmitter | T10 |
| 6. HistoryReader + OperatorReadAuthGuard + NoOp emitter | T7, T8, T21 |
| 7. F-local classifier | T11, T18 |
| 8. NoPlaintextReason enum 4 → 7 + Go mirror | T1 |
| 9. codec.TypeRegistry built-ins | T2 |
| 10. approval.Repo.GetByOpArgsHash + migration 000036 | T3 |
| 11. Trailing completion audit | T10, T16 |
| 12. Per-frame write deadline | T15, T18 |
| 13. CLI subcommand | T22 |
| 14. Builtin event-type registration | T5 |
| 15. Production-boot E2E fold-in | T23, T24, T25 |
| (new) 4th substrate: HistoryQuery.SensitiveOnly | T0 |

INV-F coverage: F1/F2/F3 in T16, F4 in T7, F5 in T16, F6/F7 in T12, F8/F9 in T9+T10+T12, F10 in T16+T10, F11 in T17, F12 in T11+T1, F13 in T5, F14 in T15+T18, F15 in T0+T18, F16 in T1, F17 in T3, F18 meta-test in post-impl.

**Type consistency:** `RequestID` is `string` (26-char ULID Base32) in payload structs (T6); `[16]byte` at the approval.Repo interface (T17 — construct via ulid.MustParse). `ContextRef.IDs` is `[]string` everywhere. `Classifier.Classify` returns `(eventbus.NoPlaintextReason, bool)` everywhere. `SessionAuthorizer.AuthorizeOperator` returns `Operator, error` everywhere.

---

## Execution handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md`. Two execution options:

1. **Subagent-Driven (recommended)** — dispatch a fresh subagent per task with two-stage review; fast iteration on each TDD cycle.
2. **Inline Execution** — execute tasks in this session using `superpowers:executing-plans`; batch with checkpoints.

Which approach?
