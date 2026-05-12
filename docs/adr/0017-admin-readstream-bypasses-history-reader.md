# ADR 0017: AdminReadStream Bypasses HistoryReader/Dispatcher

**Date:** 2026-05-12
**Status:** Accepted
**Deciders:** HoloMUSH Contributors

## Context

Sub-epic F (`holomush-jxo8.8`) implements `AdminReadStream` — a server-streaming
ConnectRPC endpoint on the admin Unix socket for operator break-glass reads of
sensitive history events. The first implementation (24 commits, bookmark
`jxo8-f-target`) layered F on top of the existing `HistoryReader`/`dispatcher`/
`postgresColdTier.Read` infrastructure that powers live subscribe and
participant history reads.

During implementation review the chain accumulated four wrapper layers, each
introduced to compensate for an impedance mismatch between F's
break-glass-read shape and the shared infrastructure's subscriber shape:

| Wrapper                       | Compensates for                                                  |
| ----------------------------- | ---------------------------------------------------------------- |
| `OperatorReadAuthGuard`       | `WithHistoryAuth` requires a non-nil `SessionAuthGuard`. F has no per-event auth (the operator capability is checked once at handler entry, not per row). |
| `NoOpDecryptAuditEmitter`     | `WithHistoryAuth` requires a non-nil `SessionAuditEmitter`. F emits one pre-data + one post-data audit pair per invocation, not per-decrypt audit traffic. |
| `staleDEKColdResolver`        | The shared `dispatcher.DispatchFor` raises errors on missing or destroyed DEKs. F needs those rows to surface as metadata-only frames with a typed reason so the operator sees the failure pattern. |
| `mergeStreams` k-way merge    | `postgresColdTier.Read` takes a single `Subject` per query. F resolves multiple operator contexts → N queries → in-memory heap merge. |

A correctness bug was discovered in the merge layer: `mergedStream.Next`
drops a source from the heap on any non-EOF error. The downstream
`streamData` classifier then treats certain errors (DEK column malformation,
DEK lookup failures) as non-fatal — but by the time the classifier runs the
source is already gone, so subsequent events on that subject are silently
lost. Operator audit `decrypt_fail_count` undercounts by potentially many
events. Audit truthfulness is broken.

Three rounds of adversarial design review converged on the diagnosis: every
wrapper above is a symptom of the same disease. The shared
`HistoryReader`/`dispatcher` abstraction was designed for subscribers
receiving live fan-out with plugin identities, per-event AuthGuard decisions,
and per-decrypt audit emit. F is none of those things. F is wearing the
subscriber uniform; the wrappers are the alterations that make it almost fit.

## Decision

**F owns its own cold-tier read + decrypt + classify path. F is not a
`HistoryReader` consumer.**

Specifically:

1. F has its own `internal/admin/readstream/cold_reader.go` (~150 LOC) that
   issues SQL directly against `events_audit`:

   ```sql
   SELECT id, subject, type, timestamp, actor_kind, actor_id, envelope,
          js_seq, codec, dek_ref, dek_version
   FROM events_audit
   WHERE subject = ANY($1)
     AND dek_ref IS NOT NULL
     AND timestamp BETWEEN $2 AND $3
   ORDER BY timestamp ASC
   ```

2. F has its own `internal/admin/readstream/decrypt.go` (~80 LOC) consuming
   `ColdRow` structs and producing `(plaintext []byte, reason NoPlaintextReason,
   fatal bool, err error)`. Decrypt failures classified locally as in-band
   metadata-only events; only stream-level failures (ctx canceled / deadline,
   DB connection lost) surface as errors.

3. F has its own unexported `classifyDecryptErr` function (~40 LOC) covering
   INV-F12's six-branch matrix. Not a public `Classifier` interface — there is
   no second consumer.

4. F does not construct a `history.NewReader(...)`. F does not pass through
   `dispatcher.DispatchFor`. F does not need `OperatorReadAuthGuard` or
   `NoOpDecryptAuditEmitter`. F does not merge streams.

5. The wire shape for `AdminReadStreamResponse.Event` payload uses
   `corev1.EventFrame` (which has typed `metadata_only` + `no_plaintext_reason`
   fields), not raw `eventbusv1.Event`. F.20 CLI reads the typed fields directly;
   no `len(Payload)==0` heuristic.

6. F retains the `chain.Handler` factory for audit-chain integration
   (`OperatorReadHandlerFor` / `OperatorReadChainFor`). Three real consumers
   exist across rekey, policy, and operator-read; the abstraction earns its
   keep.

7. F's `SessionAuthorizer` adapter type and `Operator` struct dissolve. The
   handler takes `SessionStore` + `SubjectResolver` as `Config` fields and
   inlines the 2-call pattern (`sessions.Get` + `access.HasPlayerGrant`)
   directly.

8. F's `ResponseSender` interface and `streamDataOverride` test hook move
   into an `export_test.go` `testHandler` wrapper. The production `Handler`
   type has no test-only fields.

The shared `HistoryReader`/`dispatcher`/`postgresColdTier` machinery
remains as-is — for subscribers, plugin decrypts, and the existing
participant history-read flow. Those consumers legitimately need the
AuthGuard / SessionAuditEmitter / plugin-identity machinery.

## Rationale

### 1. F is not a subscriber

The `dispatcher.DispatchFor` API at `internal/eventbus/history/dispatcher.go:74-235`
carries three parameters meaningful only to fan-out subscribers:

- `identity SessionIdentity` — branches on `IdentityKindPlugin`
- `guard SessionAuthGuard` — branches on `decision.Permit`
- `auditEm SessionAuditEmitter` — emits per-decrypt audit records

F has no plugin identity (operator capability is the principal), checks
capability once at handler entry (never per-row), and emits one pre-data
+ one post-data audit pair (not per-decrypt). Passing null-objects for
fields that don't apply is a code smell pointing at a bundle that should
not include F.

### 2. The wrapper pattern is the root cause

The bug discovered in `mergedStream.Next` is symptomatic. Even a "surgical
fix" (move classifier into the cold-tier `Read` loop with a `LenientDecrypt`
flag on `HistoryQuery`) repeats the original sin at smaller scale: a
consumer-specific tweak inside a shared abstraction. The same pattern that
produced four wrappers would produce a fifth (the flag) and likely a sixth
when the next mismatch surfaces.

### 3. F is small

The cold-tier read + decrypt + classifier path implements as ~270 LOC of
straightforward Go. AAD construction, codec resolution, and DEK resolution
are stable single-call utilities reused from the existing crypto packages.
The "duplicates cold-tier reader" concern is real but small (~50 lines
mirrored between F's helper and the dispatcher).

### 4. Wire-shape truthfulness

The first implementation encoded operator-facing redaction via
`len(Event.Payload) == 0` because the chosen wire type (`eventbusv1.Event`)
has no typed `metadata_only` / `no_plaintext_reason` fields. The CLI then
had to heuristic-detect redaction. Switching the wire to `corev1.EventFrame`
(which carries the typed fields) eliminates the heuristic and makes the
operator-facing semantic explicit on the wire.

### 5. Test architecture follows production shape

When the production type stops carrying test-only `func` fields apologizing
for themselves in docstrings (`streamDataOverride`, `responseSenderWrapper`,
`SetResponseSenderWrapperForTest`, `SessionAuthorizerWrapperForTest`), the
testing surface becomes a `testHandler` wrapper in `export_test.go` that
exposes internal stages directly. Tests stop installing shims on a
production struct.

## Alternatives Considered

### Alternative A: Surgical fix (REJECTED)

Pluralize `HistoryQuery.Subjects []Subject` (with EXPLAIN ANALYZE gate);
move the classifier matrix to `internal/eventbus/history/DefaultClassifier`
(public); add `HistoryQuery.LenientDecrypt bool` flag so cold-tier `Read`
emits metadata-only events in-band; delete `stream_merge.go` and
`staleDEKColdResolver`.

**Why rejected:** preserves F's coupling to `HistoryReader`. Adds a flag
to a shared method to make it stop doing the wrong thing for F. Same
pattern produced the original four wrappers; no reason to expect this
flag would be the last.

### Alternative B: Dispatcher reason-policy function (REJECTED)

`internal/eventbus/history/dispatcher.go` gains a configurable policy
`func(err error) (NoPlaintextReason, fatal bool)`. The dispatcher invokes
it on resolver failures and emits metadata-only events with the policy's
reason.

**Why rejected:** the dispatcher owns crypto correctness invariants
(INV-E20, INV-E21, audit-emit). Decisions about "skip-this-row vs.
propagate-error" are caller policy, not crypto policy. Placing the policy
on the dispatcher fattens the wrong layer.

### Alternative C: Typed `MetadataOnlyError` carrying `NoPlaintextReason` (REJECTED)

Generalize `source.ErrMetadataOnly` from sentinel to typed error carrying
`NoPlaintextReason`. Resolver wrappers map oops codes to the typed error;
dispatcher reads the reason.

**Why rejected:** structural defect — wrappers around the dispatcher's
resolver can only intercept errors that happen at resolver-time. Column
validation errors (`EVENTBUS_COLD_DEK_COLUMNS_MISSING`,
`EVENTBUS_COLD_BAD_DEK_COLUMNS`) are produced inside `decodeColdRow` BEFORE
the resolver is consulted. The typed sentinel cannot intercept them.

### Alternative D: Refactor `eventbus.Event` to union types (REJECTED)

Make `eventbus.Event` a sealed interface with `PlaintextEvent` and
`RedactedEvent` concrete types.

**Why rejected:** Go does not have sealed interfaces. The flag-polymorphism
(`MetadataOnly bool` + `NoPlaintextReason` enum) is idiomatic Go. ~14
non-test files touch `MetadataOnly`; the refactor cost is large and the
correctness gain is zero. Out of scope for F.

### Alternative E: Single SQL flag `HistoryQuery.LenientDecrypt` on the shared cold-tier (REJECTED — see Alternative A)

Same as Alternative A's flag. Rejected for the same reason.

## Consequences

### Positive

- **The bug class is structurally impossible.** No merge stream, no source-drop.
  Multi-subject ordering is handled by PostgreSQL's `ORDER BY timestamp ASC`.
- **F's read path is end-to-end visible in `internal/admin/readstream/`.**
  No detours through publish-side machinery designed for a different shape.
- **The wire is honest.** `corev1.EventFrame` carries explicit
  `metadata_only` + `no_plaintext_reason` fields; the CLI does not need to
  heuristic-detect redaction.
- **Test architecture is clean.** Production types carry only production
  fields. Tests use `export_test.go` wrappers.
- **The INV-F catalog shrinks** from 18 to 13 — three of the deleted INVs
  (F4 "guard always permits," F5 "standard guard never seen," F18 meta-test
  bookkeeping) tested implementation details of abstractions that no longer
  exist.
- **D's `OperatorAuthProvider` interface is undisturbed.** F's lifecycle
  is decoupled by simply not using D's interface where it doesn't fit, not
  by adapter indirection.

### Negative

- **Roughly half the existing 24-commit chain is discarded.** The work is
  preserved in the jj op-log but is not part of the merged history.
- **AAD construction, codec resolution, and DEK lookup are mirrored** in
  F's `decrypt.go` (~50 lines) and the shared `dispatcher.go`. Future
  changes to those three call sequences need to be applied in both places.
  Mitigation: AAD shape is governed by INV-E20 (spec-pinned); codec
  resolution is a single registry call; DEK lookup is a single manager
  call. Drift risk is low.
- **F.21's `seedAdminReadStreamData` helper needs adaptation** for the
  direct-SQL cold reader (the existing helper was built around the
  HistoryReader pattern). New helper writes rows via `pgx.Pool` directly.

### Neutral

- **Migration 000036** (`admin_approvals_op_kind_args_hash_idx`),
  the `NoPlaintextReason` enum expansion, the `GetByOpArgsHash` method on
  `approval.Repo`, the audit-chain factories, the audit payload structs,
  the audit emitter, the dual-control flow, the ConnectRPC adapter, and
  the CLI are all preserved from the original chain (some with minor
  amendments).

## References

- **Sub-epic F spec:** `docs/superpowers/specs/2026-05-11-event-payload-crypto-phase5-sub-epic-f-design.md` (amended with §0 architectural correction section referencing this ADR)
- **Sub-epic F plan:** `docs/superpowers/plans/2026-05-12-phase5-sub-epic-f.md` (amended with revised task list)
- **Master event-payload-crypto spec:** `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` (INV-42, INV-43)
- **Beads:** parent epic `holomush-jxo8.8`; superseded child beads `.1`, `.3`, `.8`, `.11`, `.14`, `.15`, `.17`, `.19`, `.24`; amended child beads `.5`, `.20`, `.21`, `.23`; new child beads to be materialized for the revised approach.
