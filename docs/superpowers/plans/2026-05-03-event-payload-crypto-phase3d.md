<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Event Payload Cryptography — Phase 3d Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land the final sub-phase of Phase 3 (event payload cryptography). Ship the cold-tier crypto path, plugin SDK Sensitive wire surface, default-on `Crypto.Enabled`, end-to-end tests, and operator runbook. Result: HoloMUSH ships sensitive event flow end-to-end against host-owned audit subjects.

**Architecture:** Single feature branch, single PR. Cold-tier fidelity via column rename (`events_audit.payload` → `envelope`) + dispatcher refactor (extract a header-free shared function so hot and cold paths converge). NATS-level deny rules retire (game-topic NATS is single-principal by architectural design — empirically verified). Plugin SDK gains `bool sensitive` proto field + Lua opts-table key with type checking. `Crypto.Enabled` flag flips default. ~14 plan tasks covering 9 grounding-doc decisions.

**Tech Stack:** Go 1.22+, Protocol Buffers (proto3), gopher-lua, hashicorp/go-plugin, embedded NATS JetStream, PostgreSQL, Ginkgo/Gomega (BDD integration tests), testify (unit tests), `task` runner, jj-colocated repo.

**Spec reference:** [`docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md`](../specs/2026-05-03-event-payload-crypto-phase3d-grounding.md)

**Master spec:** [`docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`](../specs/2026-04-25-event-payload-crypto-design.md)

**Note on bead refs in commit messages:** Tasks 1-13's commit messages reference bead IDs (e.g., `holomush-ojw1.4.3`, `holomush-ojw1.4.1.1`). Per user directive, bead creation lives inside Task 14 (closing hygiene) AFTER all other tasks complete and the plan-reviewer gate passes. This means commits 1-13 reference bead IDs that don't exist when the commit lands. Harmless — `bd` consumers reading commit history will resolve the references once Task 14 creates the beads. If you prefer the alternative (bead creation BEFORE execution), execute Task 14 Steps 14.4-14.10 first, then return to Task 1.

---

## Pre-flight (T0) — verification before code changes

These checks confirm the workspace is in the post-3c state the spec assumes. No code changes; if any check fails, STOP and reconcile against the spec before proceeding.

- [ ] **Check 1: workspace is parented on the right commit**

```bash
jj log -r main@origin --no-graph -T 'description.first_line()' | head -1
```

Expected output (one line):

```text
feat(crypto): Phase 3c — DEK cache invalidation + cluster substrate (holomush-ojw1.3) (#3519)
```

- [ ] **Check 2: migration `000017` is free**

```bash
ls internal/store/migrations/ | rg "^000017_" | head -3
```

Expected: empty (no rows). Migrations `000015` and `000016` MUST exist (`000015_create_player_character_bindings`, `000016_crypto_keys_destroyed_at`).

- [ ] **Check 3: `Crypto.Enabled` default is currently `false`**

```bash
rg -n "Crypto.*Enabled.*false|ships dark" internal/eventbus/config_test.go internal/eventbus/config.go | head -5
```

Expected: at least one match in `config_test.go:16` saying *"Phase 3a ships dark — flag must default to off"*.

- [ ] **Check 4: payload column already stores marshaled envelope**

```bash
rg -n "msg\.Data = plainBytes|proto.Marshal\(envelope\)" internal/eventbus/publisher.go | head -5
```

Expected: matches at `publisher.go:295` (`plainBytes, err := proto.Marshal(envelope)`) and `:302` (`msg.Data = plainBytes`).

- [ ] **Check 5: SDK proto has no `sensitive` field today**

```bash
rg -n "sensitive" api/proto/holomush/plugin/v1/plugin.proto
```

Expected: no matches.

- [ ] **Check 6: scope verification — find all `payload` references in the files Task 3 touches**

The single-line regex from earlier drafts undercounted because most `INSERT INTO events_audit (...)` blocks span multiple lines. Use an explicit file-list grep against the architecture-table set:

```bash
rg -nl "payload" --type=go --type=sql \
  internal/eventbus/audit/projection.go \
  internal/eventbus/audit/projection_test.go \
  internal/eventbus/history/cold_postgres.go \
  internal/eventbus/history/cold_postgres_integration_test.go \
  test/integration/eventbus_e2e/cross_tier_query_test.go \
  test/testutil/crypto.go \
  test/integration/crypto/emit_test.go \
  internal/store/events_audit_test.go
```

Expected: ALL 8 files appear in the output. If a file is missing, that file may not actually reference `payload` (verify); adjust Task 3's scope.

To search for any additional file outside this list that names `events_audit` and `payload`:

```bash
rg -l "events_audit" --type=go --type=sql | xargs rg -l "\bpayload\b"
```

If any unexpected file shows up beyond the eight above, ADD it to Task 3's scope before starting.

If all six checks pass, proceed to Task 1.

---

## Task 1: Migration 000017 — rename `events_audit.payload` to `envelope`

**Files:**

- Create: `internal/store/migrations/000017_events_audit_envelope_rename.up.sql`
- Create: `internal/store/migrations/000017_events_audit_envelope_rename.down.sql`
- Test: `internal/store/migrations_audit_shape_integration_test.go` (existing test; verify it still passes after rename)

- [ ] **Step 1.1: Write the up migration**

Create `internal/store/migrations/000017_events_audit_envelope_rename.up.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Phase 3d: rename events_audit.payload to envelope.
--
-- The column has always stored the marshaled Event proto envelope bytes
-- (per publisher.go:295,302 — proto.Marshal(envelope) → msg.Data).
-- The original "payload" name is a misnomer: Event.payload is one nested
-- field within the envelope, not the column's contents. This rename
-- clarifies semantics for cold-tier readers and SQL tooling.
--
-- ALTER TABLE ... RENAME COLUMN is metadata-only — no row-level work.

ALTER TABLE events_audit RENAME COLUMN payload TO envelope;
```

- [ ] **Step 1.2: Write the down migration**

Create `internal/store/migrations/000017_events_audit_envelope_rename.down.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Reverse the Phase 3d rename. Column reverts to its original name.

ALTER TABLE events_audit RENAME COLUMN envelope TO payload;
```

- [ ] **Step 1.3: Run migration shape tests to confirm syntax**

Run: `task test:int -- -run TestMigrationsAuditShape ./internal/store/`

Expected: PASS (existing test that walks all migrations and checks shape; the new migration files are picked up automatically via `embed.FS`).

If FAIL with a column-name expectation mismatch in the test fixture, that's expected — Task 3 will update the fixtures. For now: confirm the FAIL is on the column name (`payload` vs `envelope`) and not on a SQL syntax error. Move to Step 1.4.

- [ ] **Step 1.4: Commit**

```bash
jj describe -m "feat(crypto): Phase 3d — migration 000017 rename events_audit.payload to envelope

Pre-existing misnomer: the column has always stored the marshaled Event
proto envelope bytes (per publisher.go:295,302), not just the inner
Event.payload field. This rename clarifies semantics for cold-tier
readers and SQL tooling. ALTER TABLE RENAME COLUMN is metadata-only.

Refs: holomush-ojw1.4
Refs: docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (Decision 5)"
```

---

## Task 2: Audit projection writer — use `envelope` column name

**Files:**

- Modify: `internal/eventbus/audit/projection.go:268-288` (INSERT statement)
- Test: `internal/eventbus/audit/projection_test.go` (modified in Task 3, not here)

- [ ] **Step 2.1: Update the INSERT to use `envelope` column**

In `internal/eventbus/audit/projection.go`, replace the INSERT block at lines 268-288:

```go
_, err = p.pool.Exec(ctx, `
    INSERT INTO events_audit (
        id, subject, type, timestamp, actor_kind, actor_id,
        envelope, schema_ver, codec, js_seq, rendering,
        dek_ref, dek_version
    ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
    ON CONFLICT (id) DO NOTHING`,
    idBytes,
    msg.Subject(),
    eventType,
    meta.Timestamp,
    actorKind,
    actorID,
    msg.Data(),  // unchanged — this IS the marshaled envelope
    ver,
    codec,
    meta.Sequence.Stream,
    renderingJSON,
    dekRef,
    dekVer,
)
```

Only change: column 7 in the `INSERT INTO events_audit (...)` clause changes from `payload` to `envelope`. The bound value `msg.Data()` is unchanged — that's the same bytes as before, just landing in a renamed column.

- [ ] **Step 2.2: Run unit tests for projection (will partially fail until Task 3 fixes test SQL)**

Run: `task test -- ./internal/eventbus/audit/`

Expected: tests that exercise `projection_test.go:156` SELECT will FAIL with a column-name error. Tests that don't run SQL pass. The failures are expected — fixed in Task 3.

- [ ] **Step 2.3: DO NOT commit yet**

Hold the commit until Task 3 completes — production writer + test fixtures land together for review coherence.

---

## Task 3: Rename `payload` → `envelope` in test fixtures and remaining SQL string literals

**Files:**

- Modify: `test/testutil/crypto.go:88-110` (struct + SQL)
- Modify: `internal/eventbus/audit/projection_test.go:156`
- Modify: `internal/eventbus/history/cold_postgres_integration_test.go:70`
- Modify: `test/integration/eventbus_e2e/cross_tier_query_test.go:475`
- Modify: `internal/store/events_audit_test.go:69,104`
- Modify: `test/integration/crypto/emit_test.go:183` (consumer of struct field)

- [ ] **Step 3.1: Re-verify scope with grep**

Run: `rg -n "\bpayload\b" --type=go --type=sql . | rg -v "^\./(\.beads|\.serena|\.claude)" | rg "events_audit|EventsAuditRow|row\.Payload"`

Expected: 8-12 hits matching the files above. If any new file appears, ADD it to this task's scope.

- [ ] **Step 3.2: Update `test/testutil/crypto.go`**

Two changes in the file:

1. Rename Go struct field. At lines 86-91 (verify exact lines via `rg "EventsAuditRow"`):

```go
// EventsAuditRow is the read shape used by integration tests to assert
// the projection's writes against PG. Mirrors the events_audit schema.
type EventsAuditRow struct {
    ID         []byte
    Subject    string
    Codec      string
    Envelope   []byte    // was: Payload
    DEKRef     sql.NullInt64
    DEKVersion sql.NullInt32
}
```

2. Update the SQL at line 101:

```go
const auditRowQuery = `SELECT codec, envelope, dek_ref, dek_version FROM events_audit WHERE id = $1`
```

(Change: `payload` → `envelope`.)

- [ ] **Step 3.3: Update consumer at `test/integration/crypto/emit_test.go:183`**

Find the line (verify with `rg -n "row\.Payload" test/integration/crypto/emit_test.go`) and rename:

```go
// Before:
//   row.Payload
// After:
row.Envelope
```

- [ ] **Step 3.4: Update `internal/eventbus/audit/projection_test.go:156`**

Find the SELECT (verify with `rg -n "events_audit" internal/eventbus/audit/projection_test.go`):

```go
// Before:
//   `SELECT id, subject, type, timestamp, actor_kind, payload, schema_ver, codec, js_seq FROM events_audit ...`
// After:
`SELECT id, subject, type, timestamp, actor_kind, envelope, schema_ver, codec, js_seq FROM events_audit ...`
```

Plus any `.Payload` field references in the test scan must rename to `.Envelope`. Verify with `rg -n "payload" internal/eventbus/audit/projection_test.go`.

- [ ] **Step 3.5: Update `internal/eventbus/history/cold_postgres_integration_test.go`**

Find INSERT at `:70` (verify with `rg -n "INSERT INTO events_audit" internal/eventbus/history/`):

```go
// Before:
//   `INSERT INTO events_audit (id, subject, type, timestamp, actor_kind, actor_id, payload, ...) VALUES ...`
// After:
`INSERT INTO events_audit (id, subject, type, timestamp, actor_kind, actor_id, envelope, ...) VALUES ...`
```

- [ ] **Step 3.6: Update `test/integration/eventbus_e2e/cross_tier_query_test.go:475`**

Find INSERT (verify with `rg -n "INSERT INTO events_audit" test/integration/eventbus_e2e/`):

```go
// Before:
//   `INSERT INTO events_audit (..., payload, ...) VALUES ...`
// After:
`INSERT INTO events_audit (..., envelope, ...) VALUES ...`
```

- [ ] **Step 3.7: Update `internal/store/events_audit_test.go:69` and `:104`**

Both INSERTs reference `payload` (verify with `rg -n "payload" internal/store/events_audit_test.go`). Rename to `envelope`.

- [ ] **Step 3.8: Run unit + integration tests for both audit and store packages**

Run: `task test -- ./internal/eventbus/audit/ ./internal/store/`
Expected: PASS.

Run: `task test:int -- -run "Migrations|Projection|ColdPostgres|EventsAudit" ./internal/eventbus/ ./internal/store/`
Expected: PASS.

- [ ] **Step 3.9: Commit (Tasks 2 + 3 together)**

```bash
jj describe -m "feat(crypto): Phase 3d — rename events_audit.payload to envelope (production + tests)

Updates the audit projection writer's INSERT, all SQL string literals
that name 'payload' on events_audit, and the Go struct
test/testutil/crypto.go::EventsAuditRow.Payload -> .Envelope. The
column's contents are unchanged — it has always carried the marshaled
Event envelope bytes; the rename clarifies semantics.

Files modified:
- internal/eventbus/audit/projection.go (INSERT)
- internal/eventbus/audit/projection_test.go (SELECT, scan)
- internal/eventbus/history/cold_postgres_integration_test.go (INSERT)
- test/integration/eventbus_e2e/cross_tier_query_test.go (INSERT)
- internal/store/events_audit_test.go (INSERTs)
- test/testutil/crypto.go (struct + SELECT)
- test/integration/crypto/emit_test.go (struct field consumer)

Refs: holomush-ojw1.4
Refs: docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (Decision 5)"
```

---

## Task 4: Extract `decodeAuthorizeAndDispatch` shared dispatcher (header-free)

**Files:**

- Modify: `internal/eventbus/history/hot_jetstream.go:484-660` (existing `decodeAndAuthorizeHistory` + helpers)
- Create: `internal/eventbus/history/dispatcher.go` (NEW — extracted shared function)
- Create: `internal/eventbus/history/dispatcher_test.go` (NEW — unit tests for the shared function)

- [ ] **Step 4.1: Write the failing unit test for `decodeAuthorizeAndDispatch`**

Create `internal/eventbus/history/dispatcher_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package history

import (
    "context"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/eventbus"
    "github.com/holomush/holomush/internal/eventbus/codec"
    eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// TestDecodeAuthorizeAndDispatchIdentityCodecPasses asserts that an
// identity-codec event passes through the dispatcher unchanged: no
// AuthGuard gate, no decryption, payload preserved byte-for-byte.
func TestDecodeAuthorizeAndDispatchIdentityCodecPasses(t *testing.T) {
    envelope := &eventbusv1.Event{
        Id:      makeULIDBytes(t),
        Subject: "events.game1.world.location.loc-01ABC.test",
        Type:    "core-test:hello",
        Payload: []byte("hello, world"),
    }

    ev, metaOnly, err := decodeAuthorizeAndDispatch(
        context.Background(),
        envelope,
        codec.NameIdentity,                // identity codec — no AuthGuard
        codec.KeyID(0),                // unused for identity
        uint32(0),                     // unused for identity
        nil,                           // identity (not consulted on identity codec)
        nil,                           // guard (not consulted on identity codec)
        nil,                           // dekMgr (not consulted on identity codec)
        nil,                           // auditEm (not consulted on identity codec)
    )
    require.NoError(t, err)
    assert.False(t, metaOnly, "identity codec must not be metadata-only")
    assert.Equal(t, []byte("hello, world"), ev.Payload, "payload bytes preserved")
}

// makeULIDBytes returns a 16-byte ULID for test envelopes.
func makeULIDBytes(t *testing.T) []byte {
    t.Helper()
    return []byte{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 1}
}
```

- [ ] **Step 4.2: Run the test — confirm it fails with "function not defined"**

Run: `task test -- -run TestDecodeAuthorizeAndDispatch ./internal/eventbus/history/`

Expected: FAIL with `undefined: decodeAuthorizeAndDispatch`. This confirms the symbol doesn't exist yet.

- [ ] **Step 4.3: Read the existing `decodeAndAuthorizeHistory` function**

```bash
sed -n '484,660p' internal/eventbus/history/hot_jetstream.go
```

The function has approximately this shape (verify against the actual file):

```go
func decodeAndAuthorizeHistory(
    ctx context.Context,
    msg jetstream.Msg,
    envelope *eventbusv1.Event,
    codecName codec.Name,
    identity eventbus.SessionIdentity,
    guard eventbus.SessionAuthGuard,
    dekMgr eventbus.SessionDEKManager,
    auditEm eventbus.SessionAuditEmitter,
) (eventbus.Event, bool, error) {
    h := msg.Headers()
    dekRefStr := h.Get(eventbus.HeaderDekRef)
    dekVersionStr := h.Get(eventbus.HeaderDekVersion)
    // ... parse keyID, keyVersion ...
    // ... codec dispatch ...
    // ... AuthGuard.Decide ...
    // ... decrypt-or-metadata-only ...
    return ev, metaOnly, nil
}
```

The work in Step 4.4: split this into two pieces — a thin wrapper that parses headers, and a header-free shared dispatcher.

- [ ] **Step 4.4: Create `internal/eventbus/history/dispatcher.go` with the extracted shared function**

Create the new file with the header-free function. Move the codec-dispatch + AuthGuard + decrypt-or-metadata-only logic from `decodeAndAuthorizeHistory` into this file:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package history provides the historical-read pathway for both hot
// (JetStream) and cold (PostgreSQL events_audit) tiers.
//
// dispatcher.go: header-free shared logic. Hot path supplies inputs from
// jetstream.Msg headers; cold path supplies inputs from PG row columns.
// Both call decodeAuthorizeAndDispatch.
package history

import (
    "context"

    "github.com/holomush/holomush/internal/eventbus"
    "github.com/holomush/holomush/internal/eventbus/codec"
    eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// decodeAuthorizeAndDispatch is the header-free shared dispatcher for
// historical reads. Both the hot tier (JetStream) and cold tier (PG)
// call this with their respective input sources:
//   - hot path: codecName/keyID/keyVersion from msg.Headers()
//   - cold path: codecName/keyID/keyVersion from PG row columns
//
// Returns (event, metadataOnly, error). The metadataOnly flag is true
// when the envelope's payload was redacted by AuthGuard or DEK lookup
// failed (master spec §8.4 terminal branch).
func decodeAuthorizeAndDispatch(
    ctx context.Context,
    envelope *eventbusv1.Event,
    codecName codec.Name,
    keyID codec.KeyID,
    keyVersion uint32,
    identity eventbus.SessionIdentity,
    guard eventbus.SessionAuthGuard,
    dekMgr eventbus.SessionDEKManager,
    auditEm eventbus.SessionAuditEmitter,
) (eventbus.Event, bool, error) {
    // Move the body of the existing decodeAndAuthorizeHistory function
    // here, but DROP the msg.Headers() parsing (which now happens in the
    // hot wrapper) and DROP the msg parameter. All other logic — codec
    // dispatch, AuthGuard.Decide, decrypt-or-metadata-only — is preserved
    // verbatim.
    //
    // [PASTE the body from decodeAndAuthorizeHistory, with the header-
    //  parsing block removed. The `keyID`/`keyVersion` values now come
    //  from the function parameters, not from h.Get().]

    // Implementation detail: this function is ~120 LOC by transcribing
    // the existing function body. The transcription is mechanical.
    panic("transcribe from decodeAndAuthorizeHistory — see Step 4.5")
}
```

- [ ] **Step 4.5: Transcribe the function body — done in three bounded sub-steps**

The existing `decodeAndAuthorizeHistory` is ~138 LOC. Transcribing it as one step is too large for safe execution; subagents introduce subtle re-ordering bugs. Split into three sub-steps, each runnable with a localized test pass.

- [ ] **Step 4.5a: Move the codec dispatch + AAD computation block**

Read `internal/eventbus/history/hot_jetstream.go:528-580` (approximate; verify exact range with `rg -n "codec\.Lookup|aad\.Build" internal/eventbus/history/hot_jetstream.go`). This block computes AAD and dispatches to the codec.

In `dispatcher.go::decodeAuthorizeAndDispatch`, replace the `panic(...)` with the codec-dispatch block exactly as it appears in `hot_jetstream.go`. The block uses `keyID` and `keyVersion` — these now come from the function parameters, not from header parsing.

After this step, the dispatcher has codec dispatch but not yet AuthGuard or audit-emit. Run:

```bash
task test -- -run TestDecodeAuthorizeAndDispatchIdentityCodec ./internal/eventbus/history/
```

Expected: PASS for the identity-codec test (which doesn't reach AuthGuard), but FAIL for any test that needs AuthGuard. That's expected mid-transcription.

- [ ] **Step 4.5b: Move the AuthGuard.Decide block**

Read `internal/eventbus/history/hot_jetstream.go:580-620` (approximate). This block calls `guard.Decide(...)` and handles the four AuthGuard branches (participant, host, operator, deny).

In `dispatcher.go::decodeAuthorizeAndDispatch`, append the AuthGuard block after the codec-dispatch block.

Run:

```bash
task test -- -run TestDecodeAuthorizeAndDispatch ./internal/eventbus/history/
```

Expected: PASS for AuthGuard branches if any unit-level dispatcher tests exercise them; otherwise still partial.

- [ ] **Step 4.5c: Move the decrypt-or-metadata-only + audit-emit block; remove the panic**

Read `internal/eventbus/history/hot_jetstream.go:620-660` (approximate). This block performs the actual `dekMgr.Decrypt(...)` call (when AuthGuard says permit) OR sets `metadata_only = true` (when AuthGuard says deny + redact), and emits the decryption audit event via `auditEm`.

In `dispatcher.go::decodeAuthorizeAndDispatch`, append this block. Confirm the `panic("transcribe from decodeAndAuthorizeHistory — see Step 4.5")` placeholder is removed.

Replace the `decodeAndAuthorizeHistory` body in `hot_jetstream.go` (lines `:484-660`) with a thin wrapper:

```go
func decodeAndAuthorizeHistory(
    ctx context.Context,
    msg jetstream.Msg,
    envelope *eventbusv1.Event,
    codecName codec.Name,
    identity eventbus.SessionIdentity,
    guard eventbus.SessionAuthGuard,
    dekMgr eventbus.SessionDEKManager,
    auditEm eventbus.SessionAuditEmitter,
) (eventbus.Event, bool, error) {
    h := msg.Headers()
    dekRefStr := h.Get(eventbus.HeaderDekRef)
    dekVersionStr := h.Get(eventbus.HeaderDekVersion)
    if dekRefStr == "" || dekVersionStr == "" {
        return eventbus.Event{}, false, oops.Code("EVENTBUS_HISTORY_DEK_HEADER_MISSING").
            With("has_dek_ref", dekRefStr != "").
            With("has_dek_version", dekVersionStr != "").
            With("codec", string(codecName)).
            Errorf("sensitive codec event missing required DEK headers")
    }
    keyID, keyVersion, err := parseDEKHeaders(dekRefStr, dekVersionStr)
    if err != nil {
        return eventbus.Event{}, false, err
    }
    return decodeAuthorizeAndDispatch(
        ctx, envelope, codecName, keyID, keyVersion,
        identity, guard, dekMgr, auditEm,
    )
}
```

The header-parsing logic (`parseDEKHeaders`) MAY be inlined into the wrapper (if it's only a few lines) or factored into a sibling helper. Use whichever matches the existing function's structure most cleanly after you read it.

- [ ] **Step 4.6: Run the dispatcher unit test — confirm it passes**

Run: `task test -- -run TestDecodeAuthorizeAndDispatch ./internal/eventbus/history/`
Expected: PASS.

- [ ] **Step 4.7: Run all hot-path integration tests — confirm no regression**

First, pin the real test names (the regex `HotJetstream|HistoryHot` is a guess; verify):

```bash
rg -n "^func Test" internal/eventbus/history/hot_jetstream_test.go internal/eventbus/history/*integration*_test.go 2>&1 | head -20
```

Build the `-run` regex from the actual function names — typically a pipe-separated alternation of test names that touch `decodeAndAuthorizeHistory` or its callers. Then:

Run: `task test:int -- -run "<actual-pattern>" ./internal/eventbus/history/`
Expected: PASS. The hot path should behave identically — only the internal structure changed.

- [ ] **Step 4.8: Commit**

```bash
jj describe -m "refactor(crypto): Phase 3d — extract decodeAuthorizeAndDispatch shared dispatcher

Splits the existing decodeAndAuthorizeHistory into a thin header-parsing
wrapper (hot path) and a header-free shared dispatcher (new file
dispatcher.go). The cold-tier reader (Task 5) supplies dispatcher inputs
from PG columns; the dispatcher unifies codec dispatch, AAD computation,
AuthGuard.Decide, and decrypt-or-metadata-only logic across both tiers.

Behavior unchanged on the hot path. Adds dispatcher unit tests.

Refs: holomush-ojw1.4
Refs: docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (Decision 5 item 4)"
```

---

## Task 5: Cold reader — auth plumbing + expanded SELECT + envelope-unmarshal + cold-path wrapper

**Files:**

- Modify: `internal/eventbus/history/cold_postgres.go` (struct + options + constructor + SELECT + scan + dispatcher call)
- Modify: `internal/eventbus/history/cold_postgres_integration_test.go` (call sites — variadic option opt-in)
- Modify: `internal/eventbus/history/tier.go:218-222` (optional: pass cold options through if a `WithColdHistoryAuthGuard`/etc. set is wired in `Reader` opts; non-blocking — test infra can wire directly)
- Test: `internal/eventbus/history/cold_postgres_test.go` (extend or add new)

**Pre-existing pattern reference:** the hot tier already uses this shape — `jetStreamHotTier` struct has `authGuard`, `dekManager`, `auditEmitter` fields populated by `WithHistoryAuthGuard` / `WithHistoryDEKManager` / `WithHistoryDecryptAuditEmitter` HotTierOption constructors (see `hot_jetstream.go:31-66`). Production `newHistoryReader` at `cmd/holomush/sub_grpc.go:661` does NOT yet pass these — they're applied via `WithHotTier(prebuilt)` in test fixtures only. Cold tier mirrors that pattern: fields nullable, options additive, production wiring untouched in Phase 3d.

- [ ] **Step 5.0a: Add `ColdTierOption` + auth fields to `postgresColdTier` struct + constructor**

In `internal/eventbus/history/cold_postgres.go`, replace the existing struct and constructor (`:23-29`):

```go
// postgresColdTier reads archived events from the events_audit table.
// Column shape comes from internal/store/migrations/000009_create_events_audit
// (renamed at migration 000017: payload → envelope).
type postgresColdTier struct {
    pool *pgxpool.Pool

    // authGuard evaluates sensitive event delivery decisions on the
    // cold-tier history path. nil = pre-Phase 3b passthrough (mirrors
    // jetStreamHotTier semantics at hot_jetstream.go:61-65).
    authGuard eventbus.SessionAuthGuard
    // dekManager resolves DEK material for sensitive events.
    dekManager eventbus.SessionDEKManager
    // auditEmitter logs plugin decrypt records.
    auditEmitter eventbus.SessionAuditEmitter
}

// ColdTierOption tunes postgresColdTier construction. Mirrors HotTierOption.
type ColdTierOption func(*postgresColdTier)

// WithColdHistoryAuthGuard injects the AuthGuard for sensitive event
// delivery decisions on the cold-tier history path. nil = pre-Phase 3b
// passthrough.
func WithColdHistoryAuthGuard(g eventbus.SessionAuthGuard) ColdTierOption {
    return func(c *postgresColdTier) { c.authGuard = g }
}

// WithColdHistoryDEKManager injects the DEK Manager used to resolve
// plaintext key material for sensitive codec events on the cold-tier
// history path. Required when WithColdHistoryAuthGuard is set.
func WithColdHistoryDEKManager(m eventbus.SessionDEKManager) ColdTierOption {
    return func(c *postgresColdTier) { c.dekManager = m }
}

// WithColdHistoryDecryptAuditEmitter injects the audit emitter for
// plugin decrypt records on the cold-tier history path.
func WithColdHistoryDecryptAuditEmitter(em eventbus.SessionAuditEmitter) ColdTierOption {
    return func(c *postgresColdTier) { c.auditEmitter = em }
}

// newPostgresColdTier constructs a Postgres-backed cold tier. Variadic
// opts default to no auth (matching the pre-Phase-3b passthrough) so
// existing callers (production wiring at cmd/holomush/sub_grpc.go and
// 9 integration test sites) compile unchanged.
func newPostgresColdTier(pool *pgxpool.Pool, opts ...ColdTierOption) *postgresColdTier {
    c := &postgresColdTier{pool: pool}
    for _, o := range opts {
        o(c)
    }
    return c
}
```

This is variadic-additive — existing `newPostgresColdTier(pool)` call sites at `cold_postgres_integration_test.go:94, 122, 143, 171, 196, 223, 251, 278, 308` and `tier.go:222` continue to compile. Tests that need auth wiring opt in via `newPostgresColdTier(pool, WithColdHistoryAuthGuard(g), WithColdHistoryDEKManager(m), WithColdHistoryDecryptAuditEmitter(em))`.

Run: `task lint && task build`
Expected: PASS — additive change.

- [ ] **Step 5.0b: (Optional) Plumb cold options through `Reader.NewReader`**

This step is **out-of-scope for Phase 3d** unless production wiring at `cmd/holomush/sub_grpc.go:661` actually needs it (which the existing hot-tier pattern shows it doesn't — production wiring leaves auth fields nil and tests inject via `WithHotTier(prebuilt)`/`WithColdTier(prebuilt)`). The cold tier's options stand alongside the hot tier's options as a parallel set; `Reader`-level wiring is unchanged. Skip this step in Phase 3d; future work that wires production-side authguard + dekManager + auditEmitter into BOTH tiers can add a single `WithHistoryAuthGuard`/etc. set of `Option` constructors that the Reader applies to both tiers.

- [ ] **Step 5.1: Write the failing unit test for envelope-unmarshal cold-path**

Add to `internal/eventbus/history/cold_postgres_test.go` (or create if it doesn't exist):

```go
// TestColdPostgresUnmarshalsEnvelope asserts that the cold reader
// unmarshals the envelope column to recover all Event proto fields,
// including those (Actor.legacy_id, full Timestamp pb) not present
// as separate columns.
func TestColdPostgresUnmarshalsEnvelope(t *testing.T) {
    // Build an Event with Actor.legacy_id set (plugin-authored case).
    envelope := &eventbusv1.Event{
        Id:      makeULIDBytes(t),
        Subject: "events.game1.scene.scene-01ABC.start",
        Type:    "core-scenes:scene_started",
        Actor: &eventbusv1.Actor{
            Kind:     eventbusv1.ActorKind_ACTOR_KIND_PLUGIN,
            LegacyId: "core-scenes",
        },
        Payload: []byte("{}"),
    }

    envelopeBytes, err := proto.Marshal(envelope)
    require.NoError(t, err)

    // Simulated PG row.
    row := coldRow{
        ID:         envelope.Id,
        Envelope:   envelopeBytes,
        Codec:      string(codec.NameIdentity),
        DEKRef:     sql.NullInt64{Valid: false},
        DEKVersion: sql.NullInt32{Valid: false},
    }

    ev, metaOnly, err := decodeColdRow(context.Background(), row, nil, nil, nil, nil)
    require.NoError(t, err)
    assert.False(t, metaOnly)
    assert.Equal(t, "core-scenes", ev.Actor.LegacyID,
        "legacy_id must be recovered via envelope unmarshal")
    assert.Equal(t, []byte("{}"), ev.Payload)
}
```

The test uses `decodeColdRow` (the cold-path wrapper) which doesn't exist yet — the test fails on undefined symbol. Run:

Run: `task test -- -run TestColdPostgresUnmarshalsEnvelope ./internal/eventbus/history/`
Expected: FAIL with `undefined: decodeColdRow`.

- [ ] **Step 5.2: Update the cold-tier SELECT to include `envelope`, `codec`, `dek_ref`, `dek_version`**

In `internal/eventbus/history/cold_postgres.go:70`, replace:

```go
// Before:
sb.WriteString(`SELECT id, subject, type, timestamp, actor_kind, actor_id, payload, js_seq, rendering FROM events_audit WHERE `)

// After:
sb.WriteString(`SELECT id, subject, type, timestamp, actor_kind, actor_id, envelope, js_seq, rendering, codec, dek_ref, dek_version FROM events_audit WHERE `)
```

- [ ] **Step 5.3: Update the row scan to include the new columns**

In `cold_postgres.go:140-160`, expand the scan target list:

```go
var (
    idBytes        []byte
    subjectStr     string
    eventType      string
    ts             time.Time
    actorKindStr   string
    actorIDBytes   []byte
    envelope       []byte                        // was: payload
    seq            int64
    renderingBytes []byte
    codecStr       string                        // NEW
    dekRef         sql.NullInt64                 // NEW
    dekVersion     sql.NullInt32                 // NEW
)
if scanErr := rows.Scan(
    &idBytes, &subjectStr, &eventType, &ts,
    &actorKindStr, &actorIDBytes, &envelope, &seq, &renderingBytes,
    &codecStr, &dekRef, &dekVersion,
); scanErr != nil {
    return nil, oops.Code("EVENTBUS_COLD_SCAN_FAILED").Wrap(scanErr)
}
```

- [ ] **Step 5.4: Replace field-by-field reconstruction with `proto.Unmarshal` + dispatcher call (preserving cursor-echo, rendering, Seq)**

The existing cold reader at `cold_postgres.go:140-203` does THREE things the dispatcher refactor must preserve:

1. **Cursor-echo guard** at `:164-176`: when `hasCursor && first` is true, the first row MUST equal the cursor `(seq, id)` — if not, return `EVENTBUS_CURSOR_STALE`; if yes, `continue` (discard echo). This guard MUST run BEFORE envelope-unmarshal so a cursor-stale row doesn't trigger an unrelated unmarshal error.
2. **Rendering JSONB unmarshal** at `:179-192`: PG's `rendering` column carries `protojson`-encoded `corev1.RenderingMetadata` (with `DiscardUnknown: true` for forward-compat). The envelope ALSO contains `Rendering` as a typed proto field. Decision: keep the column-derived rendering as the source of truth on cold reads — the rendering proto in the envelope is the same data and would round-trip equivalently, but the existing test fixtures and rolling-upgrade compatibility logic centers on the JSONB column. Overlay `rendering` onto the dispatcher-output Event AFTER the call.
3. **`Seq` assignment** at `:196` (`Seq: seqU`): comes from the `js_seq` column, NOT from the envelope. Assign after `decodeColdRow` returns.

Replace the block at `cold_postgres.go:140-204` with:

```go
out := make([]eventbus.Event, 0, pageSize)
first := true
for rows.Next() {
    var (
        idBytes        []byte
        subjectStr     string
        eventType      string
        ts             time.Time
        actorKindStr   string
        actorIDBytes   []byte
        envelope       []byte                        // was: payload (post-rename)
        seq            int64
        renderingBytes []byte
        codecStr       string                        // NEW
        dekRef         sql.NullInt64                 // NEW
        dekVersion     sql.NullInt32                 // NEW
    )
    if scanErr := rows.Scan(
        &idBytes, &subjectStr, &eventType, &ts,
        &actorKindStr, &actorIDBytes, &envelope, &seq, &renderingBytes,
        &codecStr, &dekRef, &dekVersion,
    ); scanErr != nil {
        return nil, oops.Code("EVENTBUS_COLD_SCAN_FAILED").Wrap(scanErr)
    }
    if len(idBytes) != 16 {
        return nil, oops.Code("EVENTBUS_COLD_BAD_ID").
            With("len", len(idBytes)).
            Errorf("events_audit.id must be 16 bytes")
    }
    var id ulid.ULID
    copy(id[:], idBytes)
    seqU := uint64(seq) //nolint:gosec // G115: js_seq is always a positive JetStream sequence number; PG bigint stores only non-negative values here

    // Cursor-echo guard — MUST run before envelope-unmarshal so a stale
    // cursor row produces EVENTBUS_CURSOR_STALE rather than an unrelated
    // proto.Unmarshal error.
    if hasCursor && first {
        first = false
        if seqU != cursorSeq || id != cursorID {
            return nil, oops.Code("EVENTBUS_CURSOR_STALE").
                With("subject", string(q.Subject)).
                With("cursor_seq", cursorSeq).
                With("cursor_id", cursorID.String()).
                With("got_seq", seqU).
                With("got_id", id.String()).
                Wrap(eventbus.ErrCursorStale)
        }
        continue // discard cursor echo
    }
    first = false

    // Envelope-unmarshal + dispatcher call (Task 4 shared function).
    // Auth inputs come from c (the cold tier's authGuard/dekManager/
    // auditEmitter fields populated by Step 5.0a's options) and from
    // q.Identity (the per-call principal from HistoryQuery, mirroring
    // hot_jetstream.go:165's q.Identity flow).
    row := coldRow{
        ID:         idBytes,
        Envelope:   envelope,
        Codec:      codecStr,
        DEKRef:     dekRef,
        DEKVersion: dekVersion,
    }
    ev, metaOnly, dispatchErr := decodeColdRow(ctx, row, q.Identity, c.authGuard, c.dekManager, c.auditEmitter)
    if dispatchErr != nil {
        return nil, dispatchErr
    }

    // Overlay column-derived fields the dispatcher doesn't own:
    // - Seq comes from js_seq column (not in envelope)
    // - Rendering preserves the column-based JSONB unmarshal for
    //   rolling-upgrade compatibility (DiscardUnknown semantics).
    ev.ID = id
    ev.Seq = seqU
    ev.MetadataOnly = metaOnly

    if len(renderingBytes) > 0 {
        var protoMD corev1.RenderingMetadata
        // DiscardUnknown: tolerate forward schema additions on persisted
        // JSONB rendering payloads. Strict decode would fail rolling
        // upgrades where new writers stamp newer fields while older
        // readers are still reading archived data.
        if unmarshalErr := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(renderingBytes, &protoMD); unmarshalErr != nil {
            return nil, oops.Code("EVENTBUS_COLD_BAD_RENDERING").
                With("subject", string(q.Subject)).
                Wrap(unmarshalErr)
        }
        ev.Rendering = eventbus.RenderingFromProto(&protoMD)
    }

    out = append(out, ev)
}
if err := rows.Err(); err != nil {
    return nil, oops.Code("EVENTBUS_COLD_ROWS_ERR").Wrap(err)
}
```

The variables `c.authGuard`, `c.dekManager`, `c.auditEmitter` come from Step 5.0a's added struct fields; `q.Identity` is on `eventbus.HistoryQuery` per `bus.go:123` (the per-call principal). Step 5.0a + Step 5.4 together close the plumbing gap surfaced by plan-reviewer Pass 2: the dispatcher's auth inputs are now reachable in the `Read` method's scope.

**Imports added** to `cold_postgres.go` for this change: `database/sql`, `encoding/hex`, `google.golang.org/protobuf/proto`, `eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"`, and `codec "github.com/holomush/holomush/internal/eventbus/codec"`. Verify against existing imports (line 6-19) and add the missing ones.

- [ ] **Step 5.5: Add the `decodeColdRow` helper used by the unit test**

Create or extend `cold_postgres.go` to factor the row-decode logic into a testable helper:

```go
// coldRow holds the fields decodeColdRow needs to call the shared
// dispatcher: ID for error messages, the marshaled Envelope bytes, and
// the codec/DEK columns that supply AAD inputs. Subject/Type/Timestamp/
// Actor are recovered from the unmarshaled envelope (no need to mirror
// them as columns); js_seq + rendering are overlaid on the dispatcher
// output by the production Read method (Step 5.4) — they don't enter
// the dispatcher.
type coldRow struct {
    ID         []byte
    Envelope   []byte
    Codec      string
    DEKRef     sql.NullInt64
    DEKVersion sql.NullInt32
}

// decodeColdRow is the cold-path equivalent of the hot-path's wrapper:
// unmarshals the envelope and calls the shared dispatcher with column-
// derived inputs.
func decodeColdRow(
    ctx context.Context,
    row coldRow,
    identity eventbus.SessionIdentity,
    guard eventbus.SessionAuthGuard,
    dekMgr eventbus.SessionDEKManager,
    auditEm eventbus.SessionAuditEmitter,
) (eventbus.Event, bool, error) {
    var pbEnvelope eventbusv1.Event
    if err := proto.Unmarshal(row.Envelope, &pbEnvelope); err != nil {
        return eventbus.Event{}, false, oops.Code("AUDIT_ENVELOPE_UNMARSHAL_FAILED").
            With("event_id", hex.EncodeToString(row.ID)).
            Wrap(err)
    }
    codecName := codec.Name(row.Codec)
    var keyID codec.KeyID
    var keyVersion uint32
    if row.DEKRef.Valid {
        keyID = codec.KeyID(row.DEKRef.Int64)
    }
    if row.DEKVersion.Valid {
        keyVersion = uint32(row.DEKVersion.Int32)
    }
    return decodeAuthorizeAndDispatch(
        ctx, &pbEnvelope, codecName, keyID, keyVersion,
        identity, guard, dekMgr, auditEm,
    )
}
```

Update the production query path (Step 5.4) to use `decodeColdRow` instead of inlining the unmarshal-and-dispatch logic.

- [ ] **Step 5.6: Run cold-path unit test — confirm it passes**

Run: `task test -- -run TestColdPostgresUnmarshalsEnvelope ./internal/eventbus/history/`
Expected: PASS.

- [ ] **Step 5.7: Run all cold-path integration tests**

Run: `task test:int -- -run "ColdPostgres|HistoryCold|CrossTier" ./internal/eventbus/ ./test/integration/eventbus_e2e/`
Expected: PASS.

- [ ] **Step 5.8: Commit**

```bash
jj describe -m "feat(crypto): Phase 3d — cold reader uses envelope unmarshal + shared dispatcher

Replaces field-by-field column reconstruction at cold_postgres.go:152
with proto.Unmarshal of the envelope column. Adds 'codec', 'dek_ref',
'dek_version' to the SELECT. Cold path now calls the shared dispatcher
extracted in Task 4 — hot and cold tiers converge on identical AAD
computation, AuthGuard, and decrypt logic.

Side effect: closes holomush-u5bb (legacy_id persistence) — Actor.legacy_id
is recovered via envelope unmarshal, no dedicated column needed.

Adds AUDIT_ENVELOPE_UNMARSHAL_FAILED error code for corruption cases.

Refs: holomush-ojw1.4
Refs: holomush-u5bb (closed as side-effect)
Refs: docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (Decision 5)"
```

---

## Task 6: Plugin SDK proto — add `bool sensitive` to `PluginHostServiceEmitEventRequest`

**Files:**

- Modify: `api/proto/holomush/plugin/v1/plugin.proto:263-267`
- Regenerate: `pkg/proto/holomush/plugin/v1/plugin.pb.go` (auto via `task gen:proto`)

- [ ] **Step 6.1: Add the `sensitive` field to the proto message**

In `api/proto/holomush/plugin/v1/plugin.proto`, replace the message at lines 263-267:

```protobuf
message PluginHostServiceEmitEventRequest {
  string stream = 1 [(buf.validate.field).string.min_len = 1];
  string event_type = 2 [(buf.validate.field).string.min_len = 1];
  bytes payload = 3;
  // sensitive declares per-event sensitivity at emit time.
  // Phase 3a's host-side fence at internal/plugin/event_emitter.go::Emit
  // validates this against the plugin manifest's declared sensitivity:
  //   - manifest sensitivity=never:  sensitive=true rejected (INV-6).
  //   - manifest sensitivity=may:    sensitive=true|false honored.
  //   - manifest sensitivity=always: sensitive=false rejected (INV-7).
  // Default false (proto3 zero) for older plugins compiled before this
  // field existed — matching pre-Phase-3d behavior.
  bool sensitive = 4;
}
```

- [ ] **Step 6.2: Regenerate Go and TS bindings**

Run: `task gen:proto`
Expected: succeeds; updates `pkg/proto/holomush/plugin/v1/plugin.pb.go` and any TS bindings under `web/`.

- [ ] **Step 6.3: Verify the generated Go field**

```bash
rg -n "Sensitive bool" pkg/proto/holomush/plugin/v1/plugin.pb.go | head -3
```

Expected: at least one match referencing `Sensitive bool` on `PluginHostServiceEmitEventRequest`.

- [ ] **Step 6.4: Run lint and build**

Run: `task lint && task build`
Expected: PASS.

- [ ] **Step 6.5: Commit**

```bash
jj describe -m "feat(crypto): Phase 3d — proto add Sensitive field on PluginHostServiceEmitEventRequest

Surfaces EmitIntent.Sensitive over the binary-plugin gRPC wire so plugins
can claim per-event sensitivity that the host's downgrade fence
(event_emitter.go::Emit, Phase 3a) can validate against the manifest.

Default false (proto3 zero) — older plugins compiled pre-3d behave as
they did before, with the manifest fence catching any manifest=always
violations.

Refs: holomush-ojw1.4.1.1
Refs: docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (Decision 5)"
```

---

## Task 7: Goplugin host service — translate proto `sensitive` → `EmitIntent.Sensitive`

**Files:**

- Modify: `internal/plugin/goplugin/host_service.go:91-99` (EmitEvent method body)
- Test: `internal/plugin/goplugin/host_service_test.go` (extend with sensitive-field cases)

- [ ] **Step 7.1: Write a failing test for the Sensitive translation**

Add to `internal/plugin/goplugin/host_service_test.go`:

```go
// TestEmitEventCopiesSensitiveTrue asserts that req.Sensitive=true is
// copied to EmitIntent.Sensitive=true at the host service boundary.
func TestEmitEventCopiesSensitiveTrue(t *testing.T) {
    captured := &capturingEmitter{}
    server := newTestHostServiceServer(t, captured)

    req := &pluginv1.PluginHostServiceEmitEventRequest{
        Stream:    "events.game1.test.x",
        EventType: "core-test:hello",
        Payload:   []byte("{}"),
        Sensitive: true,
    }

    _, err := server.EmitEvent(testContextWithToken(t), req)
    require.NoError(t, err)
    require.Len(t, captured.intents, 1)
    assert.True(t, captured.intents[0].Sensitive,
        "req.Sensitive=true MUST translate to EmitIntent.Sensitive=true")
}

// TestEmitEventCopiesSensitiveFalseDefaultsExplicit asserts the
// proto3 zero (sensitive absent / false) → EmitIntent.Sensitive=false.
func TestEmitEventCopiesSensitiveFalseDefaultsExplicit(t *testing.T) {
    captured := &capturingEmitter{}
    server := newTestHostServiceServer(t, captured)

    req := &pluginv1.PluginHostServiceEmitEventRequest{
        Stream:    "events.game1.test.x",
        EventType: "core-test:hello",
        Payload:   []byte("{}"),
        // Sensitive omitted (proto3 zero = false)
    }

    _, err := server.EmitEvent(testContextWithToken(t), req)
    require.NoError(t, err)
    require.Len(t, captured.intents, 1)
    assert.False(t, captured.intents[0].Sensitive,
        "req.Sensitive absent MUST translate to EmitIntent.Sensitive=false")
}
```

(`capturingEmitter` and `testContextWithToken` are existing test helpers in the file; verify with `rg -n "capturingEmitter|testContextWithToken" internal/plugin/goplugin/`.)

- [ ] **Step 7.2: Run the test — confirm it fails**

Run: `task test -- -run TestEmitEventCopiesSensitive ./internal/plugin/goplugin/`
Expected: FAIL — the existing handler does NOT yet copy `req.Sensitive` to `EmitIntent.Sensitive`.

- [ ] **Step 7.3: Update `host_service.go` EmitEvent to copy the field**

In `internal/plugin/goplugin/host_service.go:91-99`, replace:

```go
// Before:
if err := emitter.Emit(emitCtx, s.pluginName, pluginsdk.EmitIntent{
    Subject: req.GetStream(),
    Type:    pluginsdk.EventType(req.GetEventType()),
    Payload: string(req.GetPayload()),
}); err != nil {
    return nil, oops.With("plugin", s.pluginName).Wrap(err)
}

// After:
if err := emitter.Emit(emitCtx, s.pluginName, pluginsdk.EmitIntent{
    Subject:   req.GetStream(),
    Type:      pluginsdk.EventType(req.GetEventType()),
    Payload:   string(req.GetPayload()),
    Sensitive: req.GetSensitive(),
}); err != nil {
    return nil, oops.With("plugin", s.pluginName).Wrap(err)
}
```

- [ ] **Step 7.4: Run the test — confirm it passes**

Run: `task test -- -run TestEmitEventCopiesSensitive ./internal/plugin/goplugin/`
Expected: PASS.

- [ ] **Step 7.5: Run full goplugin test suite**

Run: `task test -- ./internal/plugin/goplugin/`
Expected: PASS.

- [ ] **Step 7.6: Commit**

```bash
jj describe -m "feat(crypto): Phase 3d — goplugin host service copies req.Sensitive

The PluginHostService.EmitEvent boundary at host_service.go:91 now
copies the proto field req.Sensitive to EmitIntent.Sensitive. The
host-side downgrade fence at event_emitter.go::Emit (Phase 3a)
validates the claim against the plugin's manifest sensitivity declaration.

Refs: holomush-ojw1.4.1.2
Refs: docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (Decision 5)"
```

---

## Task 8: Lua emit translator — read `sensitive` opts-table key with type checking

**Files (full plumbing chain — `Sensitive` must flow through 4 layers):**

- Modify: `pkg/plugin/event.go:96-100` (add `Sensitive bool` field to `pluginsdk.EmitEvent` — the buffered shape; without this, the flag is dropped between Lua and the host emitter)
- Modify: `pkg/holo/emit.go` (NOT `emitter.go` — verified actual path) — `Emitter.emit` (`:96-116`) accepts and propagates `sensitive`; `Location/Character/Global` (`:55-67`) gain sensitive-aware variants
- Modify: `internal/plugin/manager.go:323-330` (`EmitPluginEvent` — copy `event.Sensitive` into the constructed `EmitIntent.Sensitive`)
- Modify: `internal/plugin/hostfunc/stdlib.go:243-330` (registerEmit + emitLocation/Character/Global Lua bindings)
- Test: `internal/plugin/hostfunc/stdlib_internal_test.go` (NOT `stdlib_test.go` — must be the internal-package test file because the assertion calls unexported `getEmitter`)
- Test: `internal/plugin/manager_test.go` (extend with manager-level Sensitive plumbing test)

**Design notes:**

1. The existing Lua API is `holo.emit.location(locationID, eventType, payload)` (3 positional args). Add an OPTIONAL 4th arg `opts` (a Lua table) carrying `{sensitive = true}`. Backward-compatible: omitting opts behaves as today.
2. **Plumbing chain.** Lua `holo.emit.location(...)` → `holo.Emitter.emit(...)` accumulates a `pluginsdk.EmitEvent{Sensitive: ...}` → host loop reads via `Emitter.Flush()` → calls `Manager.EmitPluginEvent(event pluginsdk.EmitEvent)` → constructs `pluginsdk.EmitIntent{Sensitive: event.Sensitive}` → `eventEmitter.Emit(...)` hits the existing fence at `internal/plugin/event_emitter.go::Emit`. EVERY step in this chain must propagate `Sensitive` or the Lua-side claim silently degrades to `false`. This is what the plan-reviewer caught: without the field on `pluginsdk.EmitEvent`, the buffered shape has no slot for it.

- [ ] **Step 8.0: Add `Sensitive bool` to `pluginsdk.EmitEvent`**

In `pkg/plugin/event.go:95-100`, replace:

```go
// Before:
//   type EmitEvent struct {
//       Stream  string
//       Type    EventType
//       Payload string
//   }
// After:
type EmitEvent struct {
    Stream  string
    Type    EventType
    Payload string // JSON string

    // Sensitive declares per-event sensitivity at emit time. The host's
    // Manager.EmitPluginEvent copies this to EmitIntent.Sensitive, where
    // event_emitter.go::Emit's Phase 3a downgrade fence validates against
    // the manifest. Default false (zero value) for backwards-compat.
    Sensitive bool
}
```

- [ ] **Step 8.1: Update `pkg/holo/emit.go::Emitter` to accept and store `sensitive`**

In `pkg/holo/emit.go`, modify the existing `emit` helper at `:96-116` to take a `sensitive` flag and stamp it on the appended event:

```go
// Before (existing):
//   func (e *Emitter) emit(stream string, eventType pluginsdk.EventType, payload Payload) {
//       payloadJSON, err := json.Marshal(payload)
//       // ... error handling ...
//       e.events = append(e.events, pluginsdk.EmitEvent{
//           Stream:  stream,
//           Type:    eventType,
//           Payload: string(payloadJSON),
//       })
//   }
// After:
func (e *Emitter) emit(stream string, eventType pluginsdk.EventType, payload Payload, sensitive bool) {
    payloadJSON, err := json.Marshal(payload)
    if err != nil {
        e.errors = append(e.errors, fmt.Errorf(
            "json marshal failed: stream=%s type=%s: %w", stream, eventType, err,
        ))
        if e.logger != nil {
            e.logger.Warn("json marshal failed",
                slog.String("stream", stream),
                slog.String("event_type", string(eventType)),
                slog.String("error", err.Error()),
            )
        }
        payloadJSON = []byte("{}")
    }
    e.events = append(e.events, pluginsdk.EmitEvent{
        Stream:    stream,
        Type:      eventType,
        Payload:   string(payloadJSON),
        Sensitive: sensitive,
    })
}
```

Then update the existing public methods at `:55-67` to keep their 3-arg call sites working AND add 4-arg sensitive-aware variants:

```go
// Existing public methods become thin wrappers that default sensitive=false:

func (e *Emitter) Location(locationID string, eventType pluginsdk.EventType, payload Payload) {
    e.emit(streamPrefixLocation+locationID, eventType, payload, false)
}

func (e *Emitter) Character(characterID string, eventType pluginsdk.EventType, payload Payload) {
    e.emit(streamPrefixCharacter+characterID, eventType, payload, false)
}

func (e *Emitter) Global(eventType pluginsdk.EventType, payload Payload) {
    e.emit(streamPrefixGlobal, eventType, payload, false)
}

// NEW sensitive-aware variants:

func (e *Emitter) LocationSensitive(locationID string, eventType pluginsdk.EventType, payload Payload, sensitive bool) {
    e.emit(streamPrefixLocation+locationID, eventType, payload, sensitive)
}

func (e *Emitter) CharacterSensitive(characterID string, eventType pluginsdk.EventType, payload Payload, sensitive bool) {
    e.emit(streamPrefixCharacter+characterID, eventType, payload, sensitive)
}

func (e *Emitter) GlobalSensitive(eventType pluginsdk.EventType, payload Payload, sensitive bool) {
    e.emit(streamPrefixGlobal, eventType, payload, sensitive)
}
```

- [ ] **Step 8.2: Update `Manager.EmitPluginEvent` to propagate `Sensitive`**

In `internal/plugin/manager.go:311-331`, replace the `EmitIntent` construction:

```go
// Before:
//   return emitter.Emit(ctx, pluginName, pluginsdk.EmitIntent{
//       Subject: event.Stream,
//       Type:    event.Type,
//       Payload: event.Payload,
//   })
// After:
return emitter.Emit(ctx, pluginName, pluginsdk.EmitIntent{
    Subject:   event.Stream,
    Type:      event.Type,
    Payload:   event.Payload,
    Sensitive: event.Sensitive,
})
```

- [ ] **Step 8.3: Write the failing manager-level test for full plumbing chain**

Add to `internal/plugin/manager_test.go`:

```go
// TestEmitPluginEventPropagatesSensitive asserts that the Manager's
// EmitPluginEvent boundary copies EmitEvent.Sensitive into the
// constructed EmitIntent.Sensitive — closing the full chain from
// Lua (or any plugin source) through to the host fence.
func TestEmitPluginEventPropagatesSensitive(t *testing.T) {
    captured := newCapturingEmitter() // existing helper
    mgr := newTestManager(t, captured)

    err := mgr.EmitPluginEvent(context.Background(), "core-test", pluginsdk.EmitEvent{
        Stream:    "events.game1.test.x",
        Type:      "core-test:hello",
        Payload:   `{"msg":"private"}`,
        Sensitive: true,
    })
    require.NoError(t, err)

    require.Len(t, captured.intents, 1)
    assert.True(t, captured.intents[0].Sensitive,
        "EmitEvent.Sensitive=true MUST propagate to EmitIntent.Sensitive=true at the manager boundary")
}
```

(`newCapturingEmitter` and `newTestManager` are existing test helpers in this file; verify with `rg -n "newCapturingEmitter|newTestManager" internal/plugin/manager_test.go`.)

Run: `task test -- -run TestEmitPluginEventPropagatesSensitive ./internal/plugin/`
Expected: FAIL until Step 8.2's change lands. (If you ran 8.0–8.2 in order, this should already PASS — that's also fine; the test still serves as a regression lock.)

- [ ] **Step 8.4: Write failing Lua-side tests in the internal-package test file**

Per project convention, use `stdlib_internal_test.go` (package `hostfunc`, not `hostfunc_test`) so the test can call unexported helpers like `getEmitter`. Verify the file exists:

```bash
ls internal/plugin/hostfunc/stdlib_internal_test.go
head -5 internal/plugin/hostfunc/stdlib_internal_test.go
```

Expected: file exists with `package hostfunc` declared at the top. Add the new tests to this file (NOT to `stdlib_test.go`, which is `package hostfunc_test` and cannot reach `getEmitter`):

```go
// TestEmitLocationReadsSensitiveTrue asserts holo.emit.location with
// {sensitive=true} opts table sets EmitEvent.Sensitive=true on the
// accumulated buffer.
func TestEmitLocationReadsSensitiveTrue(t *testing.T) {
    ls := lua.NewState()
    defer ls.Close()
    RegisterStdlib(ls)

    err := ls.DoString(`
        holo.emit.location("loc-01ABC", "core-test:hello",
            { msg = "private" }, { sensitive = true })
    `)
    require.NoError(t, err)

    emitter := getEmitter(ls)
    require.NotNil(t, emitter)
    events, _ := emitter.Flush()
    require.Len(t, events, 1)
    assert.True(t, events[0].Sensitive,
        "opts.sensitive=true MUST set EmitEvent.Sensitive=true on the buffer")
}

// TestEmitLocationDefaultsSensitiveFalse asserts that omitting opts
// keeps EmitEvent.Sensitive=false (backwards compat).
func TestEmitLocationDefaultsSensitiveFalse(t *testing.T) {
    ls := lua.NewState()
    defer ls.Close()
    RegisterStdlib(ls)

    err := ls.DoString(`holo.emit.location("loc-01ABC", "core-test:hello", { msg = "public" })`)
    require.NoError(t, err)

    events, _ := getEmitter(ls).Flush()
    require.Len(t, events, 1)
    assert.False(t, events[0].Sensitive,
        "opts absent MUST keep EmitEvent.Sensitive=false")
}

// TestEmitLocationSensitiveWrongTypeRejected asserts non-bool sensitive
// raises LUA_EMIT_SENSITIVE_TYPE.
func TestEmitLocationSensitiveWrongTypeRejected(t *testing.T) {
    ls := lua.NewState()
    defer ls.Close()
    RegisterStdlib(ls)

    err := ls.DoString(`
        holo.emit.location("loc-01ABC", "core-test:hello",
            { msg = "x" }, { sensitive = "true" })
    `)
    require.Error(t, err)
    assert.Contains(t, err.Error(), "LUA_EMIT_SENSITIVE_TYPE",
        "wrong type MUST raise LUA_EMIT_SENSITIVE_TYPE error")
}
```

Run: `task test -- -run TestEmitLocation.*Sensitive ./internal/plugin/hostfunc/`
Expected: all three FAIL (existing functions ignore the 4th arg).

- [ ] **Step 8.5: Update all three Lua emit bindings (location, character, global)**

In `internal/plugin/hostfunc/stdlib.go`, replace the bodies of `emitLocation` (`:284-297`), `emitCharacter` (`:301-314`), and `emitGlobal` (`:318-330`):

```go
// emitLocation wraps holo.Emitter.LocationSensitive.
// Lua signature: holo.emit.location(locationID, eventType, payload [, opts])
// where opts is an optional table with { sensitive = bool }.
func emitLocation(ls *lua.LState) int {
    locationID := ls.CheckString(1)
    eventType := ls.CheckString(2)
    payload := ls.CheckTable(3)

    sensitive, err := readSensitiveOpts(ls, 4)
    if err != nil {
        ls.RaiseError("%s", err.Error())
        return 0
    }

    emitter := getEmitter(ls)
    if emitter == nil {
        ls.RaiseError("holo.emit: emitter not initialized (RegisterStdlib not called)")
        return 0
    }
    emitter.LocationSensitive(locationID, pluginsdk.EventType(eventType), luaTableToPayload(payload), sensitive)

    return 0
}

// emitCharacter wraps holo.Emitter.CharacterSensitive.
// Lua signature: holo.emit.character(characterID, eventType, payload [, opts])
func emitCharacter(ls *lua.LState) int {
    characterID := ls.CheckString(1)
    eventType := ls.CheckString(2)
    payload := ls.CheckTable(3)

    sensitive, err := readSensitiveOpts(ls, 4)
    if err != nil {
        ls.RaiseError("%s", err.Error())
        return 0
    }

    emitter := getEmitter(ls)
    if emitter == nil {
        ls.RaiseError("holo.emit: emitter not initialized (RegisterStdlib not called)")
        return 0
    }
    emitter.CharacterSensitive(characterID, pluginsdk.EventType(eventType), luaTableToPayload(payload), sensitive)

    return 0
}

// emitGlobal wraps holo.Emitter.GlobalSensitive.
// Lua signature: holo.emit.global(eventType, payload [, opts])
func emitGlobal(ls *lua.LState) int {
    eventType := ls.CheckString(1)
    payload := ls.CheckTable(2)

    sensitive, err := readSensitiveOpts(ls, 3)
    if err != nil {
        ls.RaiseError("%s", err.Error())
        return 0
    }

    emitter := getEmitter(ls)
    if emitter == nil {
        ls.RaiseError("holo.emit: emitter not initialized (RegisterStdlib not called)")
        return 0
    }
    emitter.GlobalSensitive(pluginsdk.EventType(eventType), luaTableToPayload(payload), sensitive)

    return 0
}
```

Add the `readSensitiveOpts` helper to `stdlib.go` (near the other helpers):

```go
// readSensitiveOpts reads the `sensitive` boolean key from the optional
// opts table at the given Lua-stack position. Returns (false, nil) if
// the opts arg is absent (LNil or no-arg). Returns an error with code
// LUA_EMIT_SENSITIVE_TYPE if the key is present with a non-boolean value.
func readSensitiveOpts(ls *lua.LState, argIdx int) (bool, error) {
    optsVal := ls.Get(argIdx)
    if optsVal == lua.LNil {
        return false, nil
    }
    opts, ok := optsVal.(*lua.LTable)
    if !ok {
        return false, oops.Code("LUA_EMIT_SENSITIVE_TYPE").
            With("got_type", optsVal.Type().String()).
            Errorf("opts arg MUST be a table or nil")
    }
    sensitiveVal := opts.RawGetString("sensitive")
    if sensitiveVal == lua.LNil {
        return false, nil
    }
    sensitiveBool, ok := sensitiveVal.(lua.LBool)
    if !ok {
        return false, oops.Code("LUA_EMIT_SENSITIVE_TYPE").
            With("got_type", sensitiveVal.Type().String()).
            Errorf("opts.sensitive MUST be a boolean")
    }
    return bool(sensitiveBool), nil
}
```

Add the `oops` import to `stdlib.go` if not already present (`"github.com/samber/oops"`).

- [ ] **Step 8.6: Run the tests — confirm they pass**

```bash
task test -- -run TestEmitLocation.*Sensitive ./internal/plugin/hostfunc/
task test -- -run TestEmitPluginEventPropagatesSensitive ./internal/plugin/
```

Expected: all PASS.

- [ ] **Step 8.7: Run full Lua hostfunc + holo + manager + pluginsdk suites**

Run: `task test -- ./internal/plugin/hostfunc/ ./internal/plugin/ ./pkg/holo/ ./pkg/plugin/`
Expected: PASS.

- [ ] **Step 8.8: Run integration tests that touch the plugin emit chain**

Run: `task test:int -- -run "Plugin|Lua|EmitPlugin" ./test/integration/ ./internal/plugin/`
Expected: PASS.

- [ ] **Step 8.9: Commit**

```bash
jj describe -m "feat(crypto): Phase 3d — Lua emit reads sensitive opts-table key (full plumbing chain)

Closes the Lua-side Sensitive plumbing across four layers so a Lua
plugin's claim reaches the Phase 3a downgrade fence at
event_emitter.go::Emit:

1. pkg/plugin/event.go: add Sensitive bool to pluginsdk.EmitEvent
   (the buffered shape — without this, the flag has no slot to ride).
2. pkg/holo/emit.go: Emitter.emit accepts sensitive; new Location/
   Character/GlobalSensitive variants thread it. Backwards-compat:
   3-arg Location/Character/Global default sensitive=false.
3. internal/plugin/manager.go: EmitPluginEvent copies event.Sensitive
   into the constructed EmitIntent.Sensitive.
4. internal/plugin/hostfunc/stdlib.go: Lua emit functions accept
   optional 4th arg opts table carrying {sensitive=bool} with type
   checking; wrong type raises LUA_EMIT_SENSITIVE_TYPE.

Tests added in stdlib_internal_test.go (package hostfunc, not
hostfunc_test — required to reach unexported getEmitter helper).
Manager-level regression test in manager_test.go locks the chain.

Refs: holomush-ojw1.4.1.3
Refs: docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (Decision 5)"
```

---

## Task 9: `Crypto.Enabled` default flip + test rename

**Files:**

- Modify: `internal/eventbus/config.go` (the `Defaults()` constructor)
- Modify: `internal/eventbus/config_test.go:14-17` (function rename + assertion flip)

- [ ] **Step 9.1: Find the `Defaults()` constructor**

```bash
rg -n "func.*Config.*Defaults|Crypto:.*Enabled" internal/eventbus/config.go | head -5
```

Locate the line that sets `Crypto.Enabled: false` (or equivalent default). Note the line number.

- [ ] **Step 9.2: Flip the default**

In `internal/eventbus/config.go`, change:

```go
// Before (approximate; verify exact wording):
//   Crypto: CryptoConfig{Enabled: false},
// After:
Crypto: CryptoConfig{Enabled: true},
```

If the default is set in a different shape (e.g., `cfg.Crypto.Enabled = false` in a constructor body), apply the equivalent flip.

- [ ] **Step 9.3: Update the test**

In `internal/eventbus/config_test.go`, replace the entire test (lines `:14-17`):

```go
// Before:
//   func TestCryptoEnabledDefaultsToFalse(t *testing.T) {
//       cfg := eventbus.Config{}.Defaults()
//       assert.False(t, cfg.Crypto.Enabled, "Phase 3a ships dark — flag must default to off")
//   }
// After:
func TestCryptoEnabledDefaultsToTrue(t *testing.T) {
    cfg := eventbus.Config{}.Defaults()
    assert.True(t, cfg.Crypto.Enabled, "Phase 3d ships live")
}
```

- [ ] **Step 9.4: Run the test — confirm it passes**

Run: `task test -- -run TestCryptoEnabled ./internal/eventbus/`
Expected: PASS.

- [ ] **Step 9.5: Run full unit suite**

Run: `task test`
Expected: PASS. Some tests may need updates if they assumed Crypto.Enabled=false; those updates land here in the same commit.

- [ ] **Step 9.5b: Run integration tests that exercise the encryption path**

`task test` does NOT compile `//go:build integration` files. The encryption path's most likely break-point under `Crypto.Enabled=true` is the integration suite — particularly the projection, hot/cold-tier, AAD, and DEK paths.

Run: `task test:int -- ./internal/eventbus/audit/ ./internal/eventbus/history/ ./internal/eventbus/crypto/dek/ ./internal/eventbus/crypto/kek/ ./test/integration/crypto/`
Expected: PASS. If a test fails because it assumed `Crypto.Enabled=false`, update it in the same commit.

- [ ] **Step 9.6: Commit**

```bash
jj describe -m "feat(crypto): Phase 3d — flip Crypto.Enabled default to true

With Phase 3d completing the Phase 3 capability surface, the 'ships
dark' transitional default flips. No users / no deployments — the flag
existed only to keep main buildable during phased landing of Phase 3a/3b/3c.

Test renamed: TestCryptoEnabledDefaultsToFalse → TestCryptoEnabledDefaultsToTrue.

Refs: holomush-ojw1.4.4
Refs: docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (Decision 2)"
```

---

## Task 10: E2E happy-path BDD test

**Files:**

- Create: `test/integration/crypto/e2e_test.go`

- [ ] **Step 10.1: Verify BDD test patterns exist in the project**

```bash
rg -ln "ginkgo|Describe\(.*func" test/integration/ | head -5
```

Expected: at least one match — Ginkgo BDD already used in the project. Read one for structure (e.g., the existing `test/integration/eventbus_e2e/`).

- [ ] **Step 10.2: Write the BDD spec**

Create `test/integration/crypto/e2e_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package crypto_test

import (
    "context"
    "testing"
    "time"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"

    // ... project imports for test harness, eventbus, crypto ...
)

func TestCryptoE2E(t *testing.T) {
    RegisterFailHandler(Fail)
    RunSpecs(t, "Phase 3d Crypto E2E Suite")
}

var _ = Describe("Sensitive event end-to-end", func() {
    var (
        ctx    context.Context
        cancel context.CancelFunc
        env    *e2eEnv
    )

    BeforeEach(func() {
        ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
        env = setupE2EEnv(ctx) // helper from existing test/testutil
    })

    AfterEach(func() {
        env.Teardown()
        cancel()
    })

    Describe("character-authored sensitive event", func() {
        It("delivers plaintext to the participant via hot tier", func() {
            // 1. Character A emits a sensitive event in scene S where A is participant.
            // 2. Character A subscribes; receives plaintext payload.
            // 3. Assertion: ev.Payload == original plaintext, ev.MetadataOnly == false.
        })

        It("delivers metadata-only to a non-participant via hot tier", func() {
            // 1. Character A emits sensitive event in scene S (A is participant; B is not).
            // 2. Character B subscribes; receives metadata_only=true.
            // 3. Assertion: ev.Payload empty, ev.MetadataOnly == true, headers preserved.
        })

        It("delivers plaintext to the participant via cold tier after JS retention rolls", func() {
            // 1. Character A emits sensitive event in scene S; force JS retention rollover
            //    (use test helper that flushes hot tier).
            // 2. Character A queries history via QueryStreamHistory.
            // 3. Assertion: cold-tier read produces the same plaintext as hot-tier
            //    delivery would have (ev.Payload byte-equal to original).
        })

        It("delivers metadata-only to a non-participant via cold tier", func() {
            // Similar to above but for B (non-participant).
            // Assertion: ev.MetadataOnly == true via cold path.
        })
    })

    Describe("plugin-authored sensitive event", func() {
        It("delivers plaintext to the participant via cold tier (Actor.legacy_id round-trip)", func() {
            // 1. Plugin core-scenes emits sensitive event with Actor.kind=PLUGIN,
            //    Actor.legacy_id="core-scenes".
            // 2. Force cold-tier read.
            // 3. Assertion: ev.Actor.LegacyID == "core-scenes" (proves envelope-unmarshal
            //    recovers the field), AND ev.Payload byte-equal to original.
            //    This locks INV-49 against the plugin-actor regression that
            //    motivated Decision 5.
        })
    })
})
```

The body of each `It` block is detailed: use the e2e harness already established in `test/integration/eventbus_e2e/` for the bus + JS + PG fixture setup. The exact API for "force JS retention rollover" depends on what the existing harness exposes — verify by reading `test/testutil/` or related helpers; if no such helper exists, add one as part of this task (use NATS `MsgDelete` or stream-purge for the test).

- [ ] **Step 10.3: Run the E2E suite**

Run: `task test:int -- ./test/integration/crypto/`
Expected: PASS for all four `It` blocks.

- [ ] **Step 10.4: Commit**

```bash
jj describe -m "test(crypto): Phase 3d — E2E sensitive event happy-path BDD

Adds test/integration/crypto/e2e_test.go covering:
  - character-authored sensitive event hot-tier participant decrypt
  - hot-tier non-participant metadata_only
  - cold-tier participant decrypt (after JS retention rolls off)
  - cold-tier non-participant metadata_only
  - plugin-authored sensitive event cold-tier Actor.legacy_id round-trip
    (locks INV-49 against the plugin-actor regression Decision 5 fixed)

Refs: holomush-ojw1.4.5
Refs: docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (Decision 3)"
```

---

## Task 11: INV-49 envelope round-trip targeted test

**Files:**

- Create: `test/integration/crypto/inv49_envelope_roundtrip_test.go`

- [ ] **Step 11.1: Write the targeted invariant test**

Create `test/integration/crypto/inv49_envelope_roundtrip_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package crypto_test

import (
    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
    "google.golang.org/protobuf/proto"

    eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

var _ = Describe("INV-49 envelope round-trip", func() {
    It("byte-equal envelope across emit → audit projection → cold-read for character actor", func() {
        // 1. Construct a deterministic envelope (character actor, fixed timestamp).
        // 2. Marshal via proto.MarshalOptions{Deterministic: true}.
        // 3. Emit through the bus. Wait for projection.
        // 4. Read back from PG via the testutil helper:
        //      row, _ := readAuditRow(ctx, eventID)
        //      Expect(row.Envelope).To(Equal(originalEnvelopeBytes))
        // 5. Re-unmarshal and assert proto.Equal(original, recovered).
    })

    It("byte-equal envelope for plugin actor with legacy_id", func() {
        // Same shape as above but Actor.kind=PLUGIN, Actor.legacy_id="core-scenes".
        // Locks the AAD-divergence regression Decision 5 was created to fix.
    })

    It("cold-read decrypts correctly for both actor kinds", func() {
        // Emit a sensitive event for each actor kind; force JS retention rolloff;
        // QueryStreamHistory for the participant; assert plaintext recovered.
    })
})
```

- [ ] **Step 11.2: Run the targeted test**

Run: `task test:int -- -run "INV-49 envelope" ./test/integration/crypto/`
Expected: PASS.

- [ ] **Step 11.3: Commit**

```bash
jj describe -m "test(crypto): Phase 3d — INV-49 envelope round-trip targeted proof

Asserts that events_audit.envelope is byte-equal to the bus envelope for
both character and plugin actors, and that cold-tier decryption succeeds
for both. Locks Decision 5's correctness invariant against the plugin-
actor AAD-divergence regression that the design-reviewer surfaced in
Revision 1 of the grounding doc.

Refs: holomush-ojw1.4.5
Refs: docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (Decision 3 + Decision 5)"
```

---

## Task 12: ABAC subscribe-deny coverage — update existing seed policy descriptions

**Findings from plan-time discovery (already verified in repo):**

The HoloMUSH ABAC layer uses a Cedar-style policy DSL, not a typed Go API. The seed policies that satisfy INV-15 already exist:

```text
internal/access/policy/seed.go:209-225 — Phase-3b audit namespace deny policies:
  seed:deny-audit-read-character — forbid principal is character, action in ["read"], resource is stream when stream.name like "audit.*"
  seed:deny-audit-read-plugin    — forbid principal is plugin,    action in ["read"], resource is stream when stream.name like "audit.*"

internal/access/policy/seed_test.go:225-254 — existing tests assert both seed policies exist with correct DSL.
```

The policies use `action in ["read"]` (subscribe is logically a read against the stream resource). They cover INV-15's denial requirement at the policy-evaluation layer.

**Phase 3d's job in Task 12 is therefore narrow:**

1. Update the existing seed-policy `Description` strings to remove the stale cross-reference to "Phase 3d NATS account-level deny rules" (now retired per Decision 4).
2. Add a short cross-reference comment in `seed_test.go` locking INV-15 (post-Decision-4 reword) to the existing test.
3. NOT write a new ABAC test — the existing seed_test.go coverage is sufficient.

**Files:**

- Modify: `internal/access/policy/seed.go:215, 223` (Description text — drop "Phase 3d NATS account-level deny rules" cross-reference)
- Modify: `internal/access/policy/seed_test.go:225-254` (add INV-15 cross-reference comment block above the existing audit-deny test cases)

- [ ] **Step 12.1: Update seed-policy Description strings**

In `internal/access/policy/seed.go:215`, replace:

```go
// Before:
//   Description: "Characters MUST NOT read audit.* streams (§7.7 ABAC layer; complements Phase 3d NATS account-level deny rules)",
// After:
Description: "Characters MUST NOT read audit.* streams (§7.7 ABAC layer; sole authoritative gate per Phase 3d Decision 4 — NATS-level deny retired)",
```

In `internal/access/policy/seed.go:223`, similarly:

```go
// Before:
//   Description: "Plugins MUST NOT read audit.* streams (§7.7 ABAC layer; complements Phase 3d NATS account-level deny rules)",
// After:
Description: "Plugins MUST NOT read audit.* streams (§7.7 ABAC layer; sole authoritative gate per Phase 3d Decision 4 — NATS-level deny retired)",
```

- [ ] **Step 12.2: Add INV-15 cross-reference in seed_test.go**

In `internal/access/policy/seed_test.go`, find the section starting at `:225` (`// Phase-3b audit deny policy tests`). Add this comment block immediately above:

```go
// INV-15 (post-Phase-3d Decision 4 reword): ABAC denies subscribe to
// audit.* streams for kind={plugin|character}. Per master spec §7.7
// (amended via Phase 3d grounding doc Appendix A), ABAC at the gRPC
// subscribe handler boundary is the authoritative isolation gate. The
// `action in ["read"]` clause covers subscribe — subscribe is logically
// a read against the stream resource. NATS-level deny rules do not
// apply (game-topic NATS is single-principal by architectural design).
//
// The two test cases below verify both seed policies exist with the
// correct DSL — they are the Phase-3d-touchable coverage of INV-15.
//
// Refs: docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (Decision 3 + Decision 4)
```

- [ ] **Step 12.3: Run the seed-policy tests**

Run: `task test -- ./internal/access/policy/`
Expected: PASS — only changes are the Description strings (no behavior change) and the new comment block.

- [ ] **Step 12.4: Commit**

```bash
jj describe -m "docs(access): Phase 3d — INV-15 cross-reference; drop NATS deny mention from seed-policy descriptions

Updates the two Phase-3b audit-namespace seed-policy Description strings
to drop the stale 'Phase 3d NATS account-level deny rules' cross-
reference (those rules retired per Phase 3d Decision 4). ABAC at the
gRPC subscribe handler is the sole authoritative gate.

Adds a comment block in seed_test.go above the audit-deny test cases
locking INV-15 (post-Decision-4 reword) to the existing coverage. No
new test files — the seed_test.go assertions at :225-254 already verify
both seed policies exist with the correct DSL.

Refs: holomush-ojw1.4.5
Refs: docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (Decision 3 + Decision 4)"
```

---

## Task 13: Operator runbook (Phase-3-scoped)

**Files:**

- Create: `site/docs/operating/crypto-setup.md`
- Create: `site/docs/operating/crypto-runbook.md`

- [ ] **Step 13.1: Verify the docs directory structure**

```bash
ls site/docs/operating/ | head -10
```

Expected: directory exists with sibling docs (e.g., `sandbox-operations.md`). Match style.

- [ ] **Step 13.2: Write `crypto-setup.md`**

Create `site/docs/operating/crypto-setup.md` covering master spec §11.3 checklist items 1-3:

- KEKSource choice (file + passphrase / OS keyring / systemd-credential / Vault)
- Master key provisioning + backup discipline
- `events_audit` + `crypto_keys` consistent backup (PG point-in-time recovery)
- Note: `Crypto.Enabled = true` default as of Phase 3d; if running an older binary, ensure config is updated explicitly.
- Cross-link to the Phase 4-6 runbook content for Rekey, Vault provider, etc.

Verify against project doc style: kebab-case filename ✓, SPDX header (use HTML comment for markdown), `task lint:markdown` clean.

- [ ] **Step 13.3: Write `crypto-runbook.md` (Phase-3-scoped)**

Create `site/docs/operating/crypto-runbook.md` covering:

- Config knob: `Crypto.Enabled` (default true; setting false disables sensitivity fence + decrypt).
- Bootstrap fail cases: crypto provider initialization errors → `oops.Code` reference list.
- Cold-tier metric: `crypto.cold_dek_miss` — what it means, when to alert. Reference master spec §8.4 amended branch.
- Cold-tier error: `AUDIT_ENVELOPE_UNMARSHAL_FAILED` — corruption diagnosis steps.
- Note: Rekey, AdminReadStream, KEK rotation procedures land in Phases 4-6.

- [ ] **Step 13.4: Run docs lint**

Run: `task lint:markdown`
Expected: PASS.

- [ ] **Step 13.5: Commit**

```bash
jj describe -m "docs(crypto): Phase 3d — operator setup + runbook (Phase-3-scoped)

Adds:
  - site/docs/operating/crypto-setup.md — initial provisioning
  - site/docs/operating/crypto-runbook.md — Phase-3-scoped knobs +
    bootstrap fail cases + cold-tier metric/error reference

Deeper runbook content (Rekey, Vault, KEK rotation) lands with later
phases per master spec §11.1 'docs ship with the code that introduces
each capability'.

Refs: holomush-ojw1.4.6
Refs: docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (Decision 7)"
```

---

## Task 14: Bead hygiene + master spec amendments

**Files:**

- Modify: `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` (per grounding doc Master spec edits + Appendix A)
- Beads (via `bd`): see grounding doc §"Bead updates required"

This task is the closing-housekeeping step. It MUST run after all other tasks pass `task pr-prep`.

- [ ] **Step 14.1: Apply master spec §7.7 verbatim replacement from Appendix A**

Open `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` and locate §7.7 (currently at master spec lines `~1608-1630`). Replace with the verbatim text from Appendix A of the grounding doc.

- [ ] **Step 14.2: Apply remaining master-spec edits**

Per grounding doc §"Master spec edits required" items 2-9:

- INV-15 reword: ABAC-only.
- INV-52 retirement.
- INV-21 extension to `events_audit.envelope == bus envelope bytes`; column rename.
- INV-49 reframe: envelope round-trip.
- §4.7: column-rename note.
- §8.4: drop wire-header claim; metric-only signaling.
- §11.1 phase 3 row: complete; remove "NATS account-level deny rules" entry.
- §11.4 rollback: no-users note.

- [ ] **Step 14.3: Lint master spec**

Run: `task lint:markdown`
Expected: PASS.

- [ ] **Step 14.4: Bead hygiene — promote precursor beads**

(This step assumes the precursor PR for `ojw1.3.22` + `ojw1.3.23` has already landed; if not, defer this commit until it does.)

```bash
bd update holomush-ojw1.3.22 --priority 1
bd update holomush-ojw1.3.23 --priority 1
```

- [ ] **Step 14.5: Update `holomush-ojw1.4`**

```bash
bd update holomush-ojw1.4 --description "$(cat <<'EOF'
Final sub-phase of Phase 3 (holomush-ojw1). End-to-end sensitive flow.

Design grounding: docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md
Implementation plan: docs/superpowers/plans/2026-05-03-event-payload-crypto-phase3d.md

Acceptance: all child task beads closed; PR merged; master spec §7.7 amended per Appendix A; INV-52 retired; INV-15/21/49 updated per grounding doc T9.
EOF
)"
```

- [ ] **Step 14.6: Close `holomush-ojw1.4.2`**

```bash
bd close holomush-ojw1.4.2 --reason "Architecturally not applicable per Phase 3d Decision 4 (game NATS is server-only by design). External-NATS account scoping for the holomush server tracked under holomush-s5ts. ABAC at the gRPC subscribe handler is the authoritative gate."
```

- [ ] **Step 14.7: Create new task beads — all 7 with explicit descriptions**

Each bead's description has the same seven-section structure. All seven `bd create` invocations are inlined below; the engineer copy-pastes each block in turn. Capture each command's printed bead ID into a notes file — Step 14.8's `bd dep add` edges reference them.

**Bead 1 of 7: `ojw1.4.3` — Cold-tier envelope rename + dispatcher refactor**

```bash
bd create \
  --title "Phase 3d.3: Cold-tier envelope rename + dispatcher refactor" \
  --type feature \
  --priority 1 \
  --parent holomush-ojw1.4 \
  --description "$(cat <<'EOF'
**Goal:** Rename events_audit.payload to envelope (clarifies pre-existing semantics); cold reader uses proto.Unmarshal of envelope + shared dispatcher; closes holomush-u5bb intent.

**Design reference:** docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (Decision 5)
**Plan reference:** docs/superpowers/plans/2026-05-03-event-payload-crypto-phase3d.md (Tasks 1-5)

**TDD acceptance criteria:**
- TestColdPostgresUnmarshalsEnvelope passes (cold-path envelope-unmarshal + dispatcher call)
- TestDecodeAuthorizeAndDispatchIdentityCodecPasses passes (shared dispatcher unit)
- AUDIT_ENVELOPE_UNMARSHAL_FAILED error code is reachable (corruption path)
- All pre-existing audit / projection / cold_postgres / hot_jetstream tests pass post-rename and post-refactor

**Verification steps:**
- task lint
- task test -- ./internal/eventbus/audit/ ./internal/eventbus/history/ ./internal/store/
- task test:int -- -run \"Migrations|Projection|ColdPostgres|HotJetstream|CrossTier\" ./internal/eventbus/ ./test/integration/eventbus_e2e/

**Files touched:**
- internal/store/migrations/000017_events_audit_envelope_rename.up.sql + .down.sql (NEW)
- internal/eventbus/audit/projection.go (INSERT)
- internal/eventbus/audit/projection_test.go (SELECT)
- internal/eventbus/history/cold_postgres.go (SELECT, scan, dispatcher call)
- internal/eventbus/history/cold_postgres_integration_test.go (INSERT)
- internal/eventbus/history/dispatcher.go (NEW — extracted shared function)
- internal/eventbus/history/dispatcher_test.go (NEW — unit tests)
- internal/eventbus/history/hot_jetstream.go (decodeAndAuthorizeHistory becomes thin wrapper)
- test/testutil/crypto.go (struct + SELECT)
- test/integration/crypto/emit_test.go (struct field consumer)
- test/integration/eventbus_e2e/cross_tier_query_test.go (INSERT)
- internal/store/events_audit_test.go (INSERTs)

**Dependencies:** none (parallel-safe with ojw1.4.1.* SDK work)

**Out of scope:** INV-39 stale-DEK fallback (Phase 5); INV-50 plugin-owned audit downgrade fence (Phase 7); legacy_id elimination tech debt (separate epic).
EOF
)"
```

**Bead 2 of 7: `ojw1.4.4` — Crypto.Enabled default flip**

```bash
bd create \
  --title "Phase 3d.4: Crypto.Enabled default flips false → true" \
  --type feature \
  --priority 1 \
  --parent holomush-ojw1.4 \
  --description "$(cat <<'EOF'
**Goal:** Flip the Crypto.Enabled default from false to true. Phase 3d ships live; transitional 'ships dark' default retires.

**Design reference:** docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (Decision 2)
**Plan reference:** docs/superpowers/plans/2026-05-03-event-payload-crypto-phase3d.md (Task 9)

**TDD acceptance criteria:**
- TestCryptoEnabledDefaultsToTrue passes (renamed from TestCryptoEnabledDefaultsToFalse, assertion flipped)
- All existing tests pass under Crypto.Enabled=true (no test silently assumed false)

**Verification steps:**
- task lint
- task test
- task test:int -- ./internal/eventbus/audit/ ./internal/eventbus/history/ ./internal/eventbus/crypto/dek/ ./internal/eventbus/crypto/kek/ ./test/integration/crypto/

**Files touched:**
- internal/eventbus/config.go (Defaults() constructor — flip Crypto.Enabled value)
- internal/eventbus/config_test.go (test function rename + assertion flip)
- (any test that assumed Crypto.Enabled=false — fix in same commit)

**Dependencies:** ojw1.4.3 (cold-tier ready) AND ojw1.4.1.1 + ojw1.4.1.2 + ojw1.4.1.3 (SDK Sensitive surface complete — without these, plugin emits crater on manifest=always events when fence runs)

**Out of scope:** Per-game enable flag (out of v1 spec); rollback runbook (no users → Git revert per Decision 2).
EOF
)"
```

**Bead 3 of 7: `ojw1.4.5` — E2E BDD + INV-49 envelope round-trip + ABAC subscribe-deny coverage**

```bash
bd create \
  --title "Phase 3d.5: E2E BDD + INV-49 envelope round-trip + ABAC coverage" \
  --type feature \
  --priority 1 \
  --parent holomush-ojw1.4 \
  --description "$(cat <<'EOF'
**Goal:** End-to-end test surface for Phase 3d's sensitive flow. Three concerns under one bead: BDD happy-path narrative, INV-49 targeted envelope round-trip proof, ABAC subscribe-deny coverage update.

**Design reference:** docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (Decision 3 + Decision 4)
**Plan reference:** docs/superpowers/plans/2026-05-03-event-payload-crypto-phase3d.md (Tasks 10-12)

**TDD acceptance criteria (the tests ARE the deliverable):**
- test/integration/crypto/e2e_test.go covers character-authored AND plugin-authored sensitive flow, hot-tier participant decrypt, hot-tier non-participant metadata-only, cold-tier participant decrypt, cold-tier non-participant metadata-only
- test/integration/crypto/inv49_envelope_roundtrip_test.go asserts events_audit.envelope byte-equality across emit→audit→cold-read for both actor kinds
- internal/access/policy/seed_test.go has a comment block locking INV-15 (post-Decision-4 reword) to the existing audit-deny test cases at :225-254
- internal/access/policy/seed.go Description strings drop the stale 'Phase 3d NATS account-level deny rules' cross-reference

**Verification steps:**
- task lint
- task test -- ./internal/access/policy/
- task test:int -- ./test/integration/crypto/

**Files touched:**
- test/integration/crypto/e2e_test.go (NEW)
- test/integration/crypto/inv49_envelope_roundtrip_test.go (NEW)
- internal/access/policy/seed.go (Description strings only — no behavior change)
- internal/access/policy/seed_test.go (comment block above existing audit-deny tests)

**Dependencies:** ojw1.4.4 (flag flipped — without it, the E2E doesn't exercise the live flow)

**Out of scope:** INV-39 stale-DEK fallback test (Phase 5 follow-up bead, requires Rekey setup); INV-52 NATS deny test (retired per Decision 4); INV-46 plugin-owned audit byte-equality (Phase 7).
EOF
)"
```

**Bead 4 of 7: `ojw1.4.6` — Operator runbook (Phase-3-scoped)**

```bash
bd create \
  --title "Phase 3d.6: Operator runbook (Phase-3-scoped subset)" \
  --type task \
  --priority 1 \
  --parent holomush-ojw1.4 \
  --description "$(cat <<'EOF'
**Goal:** Phase-3-scoped operator runbook deliverables per master spec §9.2 — initial setup + config knobs + cold-tier metric/error reference. Deeper content (Rekey, Vault, KEK rotation) lands with later phases per master spec §11.1.

**Design reference:** docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (Decision 7)
**Plan reference:** docs/superpowers/plans/2026-05-03-event-payload-crypto-phase3d.md (Task 13)

**TDD acceptance criteria (docs-as-code; no failing-test cycle):**
- task lint:markdown passes for both new files
- crypto-setup.md covers KEKSource choice, master key provisioning, backup discipline (master spec §11.3 items 1-3)
- crypto-runbook.md covers Crypto.Enabled config knob, bootstrap fail cases (oops.Code reference), crypto.cold_dek_miss metric, AUDIT_ENVELOPE_UNMARSHAL_FAILED error

**Verification steps:**
- task lint:markdown
- Manual review: cross-link to spec §11.3 items + Phase 4-6 follow-up references

**Files touched:**
- site/docs/operating/crypto-setup.md (NEW)
- site/docs/operating/crypto-runbook.md (NEW)

**Dependencies:** ojw1.4.4 (capability shipped — runbook documents the live config)

**Out of scope:** Rekey procedures (Phase 5); AdminReadStream, localhost UNIX admin socket (Phase 5); KEK rotation, Vault provider (Phase 6); plugin-owned audit (Phase 7).
EOF
)"
```

**Bead 5 of 7: `ojw1.4.1.1` — SDK proto add `bool sensitive`**

```bash
bd create \
  --title "Phase 3d.1.1: SDK proto add bool sensitive on PluginHostServiceEmitEventRequest" \
  --type feature \
  --priority 1 \
  --parent holomush-ojw1.4.1 \
  --description "$(cat <<'EOF'
**Goal:** Add bool sensitive field to PluginHostServiceEmitEventRequest in the binary-plugin gRPC contract; regenerate Go and TS bindings.

**Design reference:** docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (Decision 5)
**Plan reference:** docs/superpowers/plans/2026-05-03-event-payload-crypto-phase3d.md (Task 6)

**TDD acceptance criteria:**
- The generated Go struct exposes Sensitive bool (verify via rg in pkg/proto/holomush/plugin/v1/plugin.pb.go)
- task lint passes (regen output is lint-clean)
- task build passes (no unresolved type references)

**Verification steps:**
- task gen:proto
- task lint
- task build
- task test (existing tests unaffected since Tasks 7+8 supply the field consumers)

**Files touched:**
- api/proto/holomush/plugin/v1/plugin.proto (add bool sensitive = 4)
- pkg/proto/holomush/plugin/v1/plugin.pb.go (regen)
- web/ TS bindings (regen if applicable)

**Dependencies:** none — parallel-safe with ojw1.4.3 cold-tier work.

**Out of scope:** Translating req.Sensitive to EmitIntent.Sensitive at the goplugin host service (ojw1.4.1.2); Lua opts-table reading (ojw1.4.1.3).
EOF
)"
```

**Bead 6 of 7: `ojw1.4.1.2` — Goplugin host service translation**

```bash
bd create \
  --title "Phase 3d.1.2: Goplugin host service translates req.Sensitive → EmitIntent.Sensitive" \
  --type feature \
  --priority 1 \
  --parent holomush-ojw1.4.1 \
  --description "$(cat <<'EOF'
**Goal:** The PluginHostService.EmitEvent boundary copies req.Sensitive (proto field added in ojw1.4.1.1) to EmitIntent.Sensitive so the host-side fence at internal/plugin/event_emitter.go::Emit (Phase 3a) can validate against the manifest.

**Design reference:** docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (Decision 5)
**Plan reference:** docs/superpowers/plans/2026-05-03-event-payload-crypto-phase3d.md (Task 7)

**TDD acceptance criteria:**
- TestEmitEventCopiesSensitiveTrue passes
- TestEmitEventCopiesSensitiveFalseDefaultsExplicit passes

**Verification steps:**
- task test -- ./internal/plugin/goplugin/
- task lint

**Files touched:**
- internal/plugin/goplugin/host_service.go (EmitEvent body — add Sensitive: req.GetSensitive() to EmitIntent construction)
- internal/plugin/goplugin/host_service_test.go (extend with sensitive-field tests)

**Dependencies:** ojw1.4.1.1 (proto field exists)

**Out of scope:** Lua emit translator (ojw1.4.1.3); manager-level plumbing (covered by ojw1.4.1.3 because the chain is exercised end-to-end in Lua tests).
EOF
)"
```

**Bead 7 of 7: `ojw1.4.1.3` — Lua emit translator + full plumbing chain**

```bash
bd create \
  --title "Phase 3d.1.3: Lua emit reads sensitive opts-table key (full plumbing chain)" \
  --type feature \
  --priority 1 \
  --parent holomush-ojw1.4.1 \
  --description "$(cat <<'EOF'
**Goal:** Close the Lua-side Sensitive plumbing across four layers so a Lua plugin's claim reaches the Phase 3a downgrade fence at event_emitter.go::Emit.

The chain: Lua holo.emit.location(..., {sensitive=true}) → holo.Emitter.LocationSensitive → pluginsdk.EmitEvent{Sensitive: true} (buffer) → host loop calls Manager.EmitPluginEvent → constructs pluginsdk.EmitIntent{Sensitive: true} → Emit fence validates against manifest.

**Design reference:** docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (Decision 5)
**Plan reference:** docs/superpowers/plans/2026-05-03-event-payload-crypto-phase3d.md (Task 8)

**TDD acceptance criteria:**
- TestEmitLocationReadsSensitiveTrue passes (Lua opts-table → EmitEvent.Sensitive=true on buffer)
- TestEmitLocationDefaultsSensitiveFalse passes (opts absent → false)
- TestEmitLocationSensitiveWrongTypeRejected passes (non-bool → LUA_EMIT_SENSITIVE_TYPE)
- TestEmitPluginEventPropagatesSensitive passes (Manager.EmitPluginEvent copies through to EmitIntent)

**Verification steps:**
- task test -- ./internal/plugin/hostfunc/ ./internal/plugin/ ./pkg/holo/ ./pkg/plugin/
- task test:int -- -run \"Plugin|Lua|EmitPlugin\" ./test/integration/ ./internal/plugin/
- task lint

**Files touched:**
- pkg/plugin/event.go (add Sensitive bool to pluginsdk.EmitEvent)
- pkg/holo/emit.go (Emitter.emit accepts sensitive; new LocationSensitive/CharacterSensitive/GlobalSensitive methods)
- internal/plugin/manager.go (EmitPluginEvent copies event.Sensitive into EmitIntent)
- internal/plugin/manager_test.go (new manager-level test)
- internal/plugin/hostfunc/stdlib.go (Lua bindings + readSensitiveOpts helper)
- internal/plugin/hostfunc/stdlib_internal_test.go (NEW test cases — internal-package test file required to reach unexported getEmitter)

**Dependencies:** ojw1.4.1.1 (proto field exists; binary plugins need it for parity though Lua doesn't directly use the proto)

**Out of scope:** Binary plugin host service translation (ojw1.4.1.2 — sibling task); proto changes (ojw1.4.1.1).
EOF
)"
```

- [ ] **Step 14.8: Add bead dependency edges**

```bash
bd dep add holomush-ojw1.4.4 holomush-ojw1.4.3        # flag flip depends on cold-tier
bd dep add holomush-ojw1.4.4 holomush-ojw1.4.1.1      # flag flip depends on SDK proto
bd dep add holomush-ojw1.4.4 holomush-ojw1.4.1.2      # flag flip depends on goplugin
bd dep add holomush-ojw1.4.4 holomush-ojw1.4.1.3      # flag flip depends on Lua
bd dep add holomush-ojw1.4.5 holomush-ojw1.4.4        # E2E tests depend on flag flip
bd dep add holomush-ojw1.4.6 holomush-ojw1.4.4        # docs depend on capability complete
bd dep add holomush-ojw1.4.1.2 holomush-ojw1.4.1.1    # goplugin depends on proto
bd dep add holomush-ojw1.4.1.3 holomush-ojw1.4.1.1    # Lua depends on proto (via Emitter)
bd dep add holomush-ojw1.4 holomush-ojw1.3.22         # epic depends on precursor PR
bd dep add holomush-ojw1.4 holomush-ojw1.3.23         # epic depends on precursor PR
```

- [ ] **Step 14.9: Close `holomush-u5bb` as resolved-as-side-effect**

```bash
bd close holomush-u5bb --reason "Resolved by holomush-ojw1.4.3 (Phase 3d Decision 5): cold-tier envelope-unmarshal recovers Actor.legacy_id without a dedicated column. The originally-proposed actor_legacy_id column is not needed today; if a future consumer requires queryable legacy_id for SQL filtering, that becomes a separate follow-up. See docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (Decision 5 close paragraph)."
```

- [ ] **Step 14.10: File new follow-up beads**

```bash
# INV-39 stale-DEK fallback
bd create --title "Phase 3d follow-up: INV-39 stale-DEK fallback (Phase 5/Rekey-prep)" \
  --type task --priority 2 --parent holomush-ojw1 \
  --description "Master spec §8.4 INV-39 — hot→cold fallback when hot-tier DEK is missing. Requires Rekey (Phase 5) to set up the failure mode naturally; deferred from Phase 3d. Reference: docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (Decision 3)."

# legacy_id elimination tech-debt epic
bd create --title "Tech debt: eliminate Actor.legacy_id in favor of uniform ULID identity for plugin actors" \
  --type epic --priority 3 \
  --description "Plugin actors should be assigned ULIDs at registration time (in the plugin registry) and Actor.id should carry a ULID uniformly across all ActorKind values. Plugin name becomes a display attribute, not an identity field. Cross-cutting refactor: Actor proto schema, plugin registry persistence, plugin emit paths, consumer paths, ABAC policies that key off plugin name, rendering layer ULID→name lookup. Its own design + plan + execution cycle. Reference: docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (Decision 6)."

# Client-visible stale-DEK signaling
bd create --title "Phase 3d follow-up: client-visible stale-DEK signaling (typed Event field)" \
  --type task --priority 3 --parent holomush-ojw1 \
  --description "Decision 5 retired the App-Warning: stale-dek wire-header claim in favor of metadata_only=true + crypto.cold_dek_miss metric. From the client's perspective metadata_only=true conflates non-participant denial, AuthGuard infra failure, and stale-DEK. A typed Event proto field or distinct EventFrame status would distinguish these. Acceptable for Phase 3d; file as future-debt so the diagnostic gap is owned. Reference: docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (Decision 5 + Bead updates §7)."
```

- [ ] **Step 14.11: Update `holomush-s5ts` description**

```bash
bd update holomush-s5ts --description "$(cat <<'EOF'
HoloMUSH external NATS deploy: server-account scoping + lifecycle pivot off embedded-only.

Phase 3d (holomush-ojw1.4) confirmed via empirical verification that game-topic NATS (events.>, audit.>, internal.>) is single-principal by architectural design — only the holomush server connects on these subjects. Per-principal NATS deny rules retired from Phase 3d (master spec §7.7 amended).

Scope for this epic:
1. Embedded server lifecycle survey: pivot internal/eventbus/ from embedded-only to external dial mode.
2. Server-account scoping in external mode:
   Account "holomush-server":
     publish:   events.>, audit.>, internal.>
     subscribe: events.>, audit.>, internal.>
   Other accounts in the cluster have no permission on these subjects by default.
3. Optional: read-only operator account ("holomush-operator-read", subscribe events.> only) for monitoring/debugging.
4. Audit-table reads: localhost UNIX admin socket (Phase 5), not NATS subscribe.
5. CI: external-NATS test matrix; nats-server in test containers.
6. Operator runbook: external-NATS deploy guide.

Cross-references:
- holomush-ojw1.4 (Phase 3d) — surfaces the architectural realization
- Phase 6 (Vault provider) overlaps on multi-host crypto
- master spec §7.7 (amended) — architectural framing
- Phase 3d grounding doc Appendix A — verbatim §7.7 replacement applied in Phase 3d

Discovered: 2026-05-03 during Phase 3d brainstorming.
EOF
)"
```

- [ ] **Step 14.12: Lint and commit master-spec amendments**

Run: `task lint:markdown`
Expected: PASS.

```bash
jj describe -m "docs(crypto): Phase 3d — master spec amendments + bead hygiene

Master spec edits per Phase 3d grounding doc T9:
- §7.7: substantial rewrite per Appendix A (game-topic NATS single-
  principal by architectural design; ABAC is authoritative; NATS-level
  deny retires)
- INV-15: ABAC-only reword
- INV-52: retired
- INV-21: extended to events_audit.envelope byte-equality
- INV-49: reframed as envelope round-trip
- §4.7: column-rename note
- §8.4: drop wire-header claim; metric-only signaling
- §11.1: phase 3 marked complete; remove NATS deny entry
- §11.4: no-users rollback note

Bead chain:
- ojw1.3.22, ojw1.3.23 promoted P3 → P1
- ojw1.4 description updated with grounding doc + plan refs
- ojw1.4.2 closed (architecturally not applicable)
- ojw1.4.3, .4, .5, .6 + ojw1.4.1.1, .2, .3 created
- u5bb closed as resolved-as-side-effect
- INV-39, legacy_id elimination, stale-DEK signaling beads filed
- holomush-s5ts updated to drop deny-rule scope

Refs: holomush-ojw1.4
Refs: docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (T9)"
```

---

## Final verification

After all 14 tasks complete:

- [ ] **Run full pre-PR gate**

Run: `task pr-prep`
Expected: PASS — all of lint, format, schema, license, unit tests, integration tests, E2E. Per CLAUDE.md memory `feedback_pr_prep_must_run`, never approximate; full run required.

- [ ] **Adversarial code-review gate (per CLAUDE.md "Pre-Push Review Gates")**

Invoke `code-reviewer` agent on the branch diff. Address findings before push.

- [ ] **Push to PR branch**

Per CLAUDE.md "Landing the Plane (jj-colocated)":

```bash
jj git fetch
jj rebase -r <change-id> -d main@origin
jj bookmark set ojw1.4-phase3d -r @-
jj git push --branch ojw1.4-phase3d
```

- [ ] **Open PR via gh**

```bash
gh pr create --title "feat(crypto): Phase 3d — final synthesis (holomush-ojw1.4)" \
  --body "$(cat <<'EOF'
## Summary

Final sub-phase of Phase 3 (event payload cryptography). Ships visible Crypto.Enabled=true behavior, cold-tier crypto path, plugin SDK Sensitive surface, E2E tests, operator runbook.

Seven design decisions documented in `docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md`. Three design-reviewer cycles + one plan-reviewer cycle + adversarial code-reviewer gate.

## Test plan

- [ ] task pr-prep passes (full CI gate)
- [ ] All beads under holomush-ojw1.4 closed
- [ ] Master spec §7.7 amended per Appendix A
- [ ] INV-21/49 extended; INV-15 reworded; INV-52 retired
- [ ] Plugin-authored sensitive E2E test passes (locks the plugin-actor AAD round-trip Decision 5 introduced)

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Final commit cleanup + close epic**

After PR merges:

```bash
bd close holomush-ojw1.4 --reason "PR #<N> merged; all task beads closed; master spec §7.7 amended; INV-52 retired; capability ships."
```

---

## Self-review checklist (post-write)

- [x] Spec coverage: all 9 grounding-doc tasks (T1-T9) mapped to plan tasks (Tasks 1-14, where T2 splits into Tasks 2-5 and T7 splits into Tasks 10-12).
- [x] Placeholder scan: no "TBD", no "implement later," no "similar to Task N" without code shown.
- [x] Type consistency: `codec.Name`, `codec.KeyID`, `uint32` keyVersion used uniformly across Tasks 4-5; struct field renames (`Payload`→`Envelope`) consistent across Tasks 3 and 5.
- [x] Files: explicit paths everywhere; no "verify in plan" hedge except where the spec explicitly defers (ABAC test existence in Task 12).
- [x] Commands: exact `task` / `bd` / `jj` / `rg` invocations with expected output where meaningful.

If executing-plans skill or subagent-driven-development picks up this plan: each task is structured so a fresh subagent can read its `Files:` block, follow the TDD steps, run the listed commands, and commit. Tasks 1-3 must serialize (migration → writer → fixtures); Tasks 4-5 follow (Task 5 depends on Task 4); Tasks 6-8 are parallel-safe with 4-5; Task 9 is the synchronization barrier; Tasks 10-13 fan out after Task 9; Task 14 is closing hygiene.

---

## Execution handoff

**Plan complete and saved to `docs/superpowers/plans/2026-05-03-event-payload-crypto-phase3d.md`.**

Per the user's directive ("the implementation plan needs review as well. Best if that's done prior to bead creation"), the next step is:

1. Lint check the plan (`task lint:markdown`)
2. Commit the plan
3. Run `plan-reviewer` adversarial gate
4. If READY: proceed to Task 14 bead-creation activities (or dispatch subagent-driven-development to execute Tasks 1-13 first, then Task 14 at the end)
5. If NOT READY: revise plan, re-run gate

Bead creation happens INSIDE Task 14, not as a precondition to plan execution. This matches the user's explicit ordering.
