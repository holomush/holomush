# Event Payload Cryptography — Phase 2: Substrate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Lay the cryptographic substrate for event payload encryption (KEK provider stack, DEK manager skeleton, AAD canonicalization, schema columns, and DEK-material non-leakage lint) without yet emitting or reading any sensitive events. Visible effect: server can wrap/unwrap DEKs; `events_audit` gains the new columns; nothing emits or reads sensitive yet.

**Architecture:** Three new packages under `internal/eventbus/crypto/`. `kek/` defines a pluggable provider interface and ships `LocalAEADProvider` + `NoneProvider` plus two `KEKSource` implementations (`file`, `env`). `dek/` defines an opaque `Material` type and a `Manager` skeleton with `GetOrCreate`/`Resolve` real and `Add`/`Rotate`/`Rekey` returning typed-error stubs that point at their respective bead epics. `aad/` ships the canonical AAD-byte builder. Two PostgreSQL migrations land — `crypto_keys` (new table) and `events_audit` (two new nullable columns). Two ruleguard rules + a static API surface test enforce DEK-material non-leakage (INV-27).

**Tech Stack:** Go 1.25, `golang.org/x/crypto/chacha20poly1305`, `golang.org/x/crypto/argon2`, `crypto/rand`, `crypto/sha256`, `github.com/samber/oops`, `github.com/jackc/pgx/v5`, `github.com/golang-migrate/migrate/v4` (existing migrator), `golang.org/x/tools/go/packages` (for the static API surface test), `github.com/quasilyte/go-ruleguard/dsl` (existing), `github.com/testcontainers/testcontainers-go` (existing integration test pattern), `github.com/stretchr/testify`.

**Spec:** Phase 2 design notes — `docs/superpowers/specs/2026-04-30-event-payload-crypto-phase2-substrate-design.md`. Master spec — `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` (sections 4, 5, 11.1; invariants INV-4, INV-25, INV-27, INV-30, INV-32, INV-33, INV-34).

**Bead:** `holomush-8qri` (Phase 2: KEKProvider + crypto_keys + DEKManager skeleton). Parent epic: `holomush-e49r`.

**Scope:** Phase 2 substrate only. Specifically excluded (later phases per master spec §11.1):

- AEAD codec implementations (`xchacha20poly1305-v1`, `aes-gcm-v1`) — Phase 3
- EventSink encrypt-on-emit path — Phase 3
- AuthGuard / decrypt-on-fanout — Phase 3
- N-of-N replica cache invalidation (INV-28, INV-29) — Phase 4
- `Add` / `Rotate` lifecycle ops with cache invalidation — Phase 4
- `Rekey` CLI / `AdminReadStream` / `OperatorAuthProvider` — Phase 5
- `VaultTransitProvider` / provider-migrate CLI — Phase 6
- `keyring` and `systemd-credential` `KEKSource` impls — filed as follow-up beads at plan creation
- Removal of `codec.KeyLabel`, `codec.KeySelector`, and the label-side of `codec.KeyProvider` — Phase 3 (the publisher/subscriber rewrite owns this; see design notes §"Disposition of substrate KeyProvider/KeySelector/KeyLabel")

**Out of scope reminder:** Phase 2 has no production callers of any new symbol. Every test in this plan is either a unit test or an integration test that exercises the substrate in isolation via a testcontainer or in-memory state. Wiring into `EventSink`, `Subscribe`, or `QueryStreamHistory` is Phase 3.

---

## File Structure

### New files

| File | Responsibility |
| ---- | -------------- |
| `internal/store/migrations/000013_create_crypto_keys.up.sql` | Create `crypto_keys` table per master spec §4.3 |
| `internal/store/migrations/000013_create_crypto_keys.down.sql` | Drop `crypto_keys` table (reversible) |
| `internal/store/migrations/000014_events_audit_dek_columns.up.sql` | Add nullable `dek_ref BIGINT` + `dek_version INTEGER` + partial index |
| `internal/store/migrations/000014_events_audit_dek_columns.down.sql` | Drop the two columns + index |
| `internal/eventbus/crypto/aad/aad.go` | `Build(event, codecName, dekRef, dekVersion) []byte` per master spec §4.2 |
| `internal/eventbus/crypto/aad/aad_test.go` | INV-25 unit tests (table-driven over each input field; `HMAAD\x01` magic; Actor proto-Deterministic determinism) |
| `internal/eventbus/crypto/dek/material.go` | Opaque `Material` struct with `AsCodecKey(codec.KeyID) codec.Key` as sole exported egress |
| `internal/eventbus/crypto/dek/material_test.go` | Unit tests for `Material` construction + `AsCodecKey` |
| `internal/eventbus/crypto/dek/api_test.go` | Static API surface test (no exported `[]byte` returns or fields from dek package); stub-bead allow-set check |
| `internal/eventbus/crypto/kek/provider.go` | `Provider` interface (Wrap/Unwrap/RotateKEK/HealthCheck/Name) |
| `internal/eventbus/crypto/kek/source.go` | `KEKSource` interface (Name/Load/Persist) |
| `internal/eventbus/crypto/kek/none.go` | `NoneProvider` — INV-32 (constructor-time DB check) + INV-34 (Wrap refusal) |
| `internal/eventbus/crypto/kek/none_test.go` | Unit tests for `NoneProvider.Wrap` refusal |
| `internal/eventbus/crypto/kek/none_integration_test.go` | INV-32 integration test (testcontainer) |
| `internal/eventbus/crypto/kek/source_env.go` | `EnvSource` — dev/test sentinel, refused in prod mode |
| `internal/eventbus/crypto/kek/source_env_test.go` | Unit tests for `EnvSource` prod-mode refusal |
| `internal/eventbus/crypto/kek/source_file.go` | `FileSource` — Argon2id-derived unlock + `HMK\x01` AEAD-wrapped key file format |
| `internal/eventbus/crypto/kek/source_file_test.go` | Unit tests (table-driven; file-format round-trip; passphrase mismatch) |
| `internal/eventbus/crypto/kek/local_aead.go` | `LocalAEADProvider` — uses `Provider` + `KEKSource`; chacha20poly1305 Wrap/Unwrap; INV-30, INV-33 |
| `internal/eventbus/crypto/kek/local_aead_test.go` | Unit tests for `LocalAEADProvider` Wrap/Unwrap roundtrip + tampering rejection |
| `internal/eventbus/crypto/kek/local_aead_integration_test.go` | INV-33 integration test (refuses startup if `wrap_key_id` not unwrappable) |
| `internal/eventbus/crypto/dek/cache.go` | `Cache` — LRU + TTL, in-process memory only |
| `internal/eventbus/crypto/dek/cache_test.go` | Unit tests for LRU eviction + TTL |
| `internal/eventbus/crypto/dek/manager.go` | `Manager` interface + impl (`GetOrCreate`, `Resolve` real; `Add`/`Rotate`/`Rekey` stubs with `tracking_bead`) |
| `internal/eventbus/crypto/dek/manager_test.go` | Unit tests for stub methods (tracking_bead presence, allow-set membership) |
| `internal/eventbus/crypto/dek/manager_integration_test.go` | Integration tests for `GetOrCreate` + `Resolve` + concurrent INSERT race |
| `internal/eventbus/crypto/dek/store.go` | `Store` — pgx-based persistence layer for `crypto_keys` (used by `Manager`) |
| `internal/eventbus/crypto/dek/store_test.go` | Unit tests for `Store` (using a mock or sqlmock) |
| `gorules/dek_no_serialize.go` | Ruleguard rule: `dek.Material` MUST NOT be passed to enumerated forbidden sinks (INV-27) |
| `gorules/codec_key_bytes_allowlist.go` | Ruleguard rule: `codec.Key.Bytes` reads outside allowlist fail lint |
| `gorules/testdata/dek_no_serialize/sinks_test.go` | Ruleguard fixture file — seeded violations the rule MUST flag |
| `gorules/testdata/codec_key_bytes/leak_test.go` | Ruleguard fixture file — seeded `codec.Key.Bytes` reads outside allowlist |

### Modified files

| File | Change |
| ---- | ------ |
| `internal/store/migrate_integration_test.go:27-49` | Add `"crypto_keys"` to `expectedTables` so `TestMigrator_FullCycle` recognizes the new table |
| `gorules/rules.go` | No change to existing rules; new rules live in their own files in the same package |
| `.golangci.yaml:142-143` | No change required — `ruleguard.rules: gorules/rules.go` already loads files via the build tag, but if `golangci-lint` requires explicit listing, change to `rules: gorules/*.go` |

---

## Task 1: Migration `000013_create_crypto_keys`

**Files:**

- Create: `internal/store/migrations/000013_create_crypto_keys.up.sql`
- Create: `internal/store/migrations/000013_create_crypto_keys.down.sql`
- Modify: `internal/store/migrate_integration_test.go:27-49`

- [ ] **Step 1: Update `expectedTables` in the migrator test to include `crypto_keys`**

```go
// internal/store/migrate_integration_test.go
// Insert "crypto_keys" alphabetically into expectedTables.
var expectedTables = []string{
    "access_audit_log",
    "access_policies",
    "access_policy_versions",
    "bootstrap_metadata",
    "character_roles",
    "characters",
    "content_items",
    "crypto_keys",
    "entity_properties",
    "events_audit",
    "exits",
    "holomush_system_info",
    "locations",
    "objects",
    "password_resets",
    "player_aliases",
    "player_sessions",
    "players",
    "scene_participants",
    "session_connections",
    "sessions",
    "system_aliases",
}
```

- [ ] **Step 2: Run the integration test to verify it fails**

Run: `task test:int -- -run TestMigrator_FullCycle ./internal/store/...`

Expected: FAIL — actual tables missing `crypto_keys` (the new entry has no migration backing it).

- [ ] **Step 3: Write the up migration**

```sql
-- internal/store/migrations/000013_create_crypto_keys.up.sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

CREATE TABLE IF NOT EXISTS crypto_keys (
    id              BIGSERIAL   PRIMARY KEY,
    context_type    TEXT        NOT NULL,
    context_id      TEXT        NOT NULL,
    version         INTEGER     NOT NULL,
    wrapped_dek     BYTEA       NOT NULL,
    wrap_provider   TEXT        NOT NULL,
    wrap_key_id     TEXT        NOT NULL,
    participants    JSONB       NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    rotated_at      TIMESTAMPTZ,
    superseded_by   BIGINT      REFERENCES crypto_keys(id),
    rekey_audit_id  BYTEA,
    UNIQUE (context_type, context_id, version)
);

CREATE INDEX IF NOT EXISTS crypto_keys_context
    ON crypto_keys (context_type, context_id);

CREATE INDEX IF NOT EXISTS crypto_keys_active
    ON crypto_keys (context_type, context_id)
    WHERE rotated_at IS NULL;
```

- [ ] **Step 4: Write the down migration**

```sql
-- internal/store/migrations/000013_create_crypto_keys.down.sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DROP INDEX IF EXISTS crypto_keys_active;
DROP INDEX IF EXISTS crypto_keys_context;
DROP TABLE IF EXISTS crypto_keys;
```

- [ ] **Step 5: Run the integration test to verify it passes**

Run: `task test:int -- -run TestMigrator_FullCycle ./internal/store/...`

Expected: PASS — `crypto_keys` is created during Up() and dropped during Down().

- [ ] **Step 6: Commit**

```text
feat(store): migration 000013 — crypto_keys table

Adds the crypto_keys table per event-payload-crypto Phase 2. Holds wrapped
per-context DEKs scoped by (context_type, context_id, version) with a
cleartext participants JSONB (intentional — AuthGuard needs to evaluate
membership before unwrapping). No emit/decrypt path uses this yet; it's
substrate.

Refs: docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md §4.3
Refs: docs/superpowers/plans/2026-04-30-event-payload-crypto-phase2-substrate.md Task 1
Bead: holomush-8qri
```

---

## Task 2: Migration `000014_events_audit_dek_columns`

**Files:**

- Create: `internal/store/migrations/000014_events_audit_dek_columns.up.sql`
- Create: `internal/store/migrations/000014_events_audit_dek_columns.down.sql`
- Test: `internal/store/migrations_audit_shape_integration_test.go` (existing — extend with assertions on new columns)

- [ ] **Step 1: Locate the existing audit-shape integration test and extend it**

Read `internal/store/migrations_audit_shape_integration_test.go` to find the test that asserts `events_audit` columns. Add assertions:

```go
// internal/store/migrations_audit_shape_integration_test.go
// Add to the existing column-set assertion (look for the test that
// validates events_audit columns; extend with):
//
// dek_ref     - BIGINT  - nullable
// dek_version - INTEGER - nullable

func TestEventsAuditHasDEKColumnsAfterMigration014(t *testing.T) {
    ctx := context.Background()
    pgContainer, err := postgres.Run(ctx,
        "postgres:18-alpine",
        postgres.WithDatabase("test"),
        postgres.WithUsername("test"),
        postgres.WithPassword("test"),
        testcontainers.WithWaitStrategy(
            wait.ForLog("database system is ready to accept connections").
                WithOccurrence(2)),
    )
    require.NoError(t, err)
    defer pgContainer.Terminate(ctx)

    connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
    require.NoError(t, err)

    migrator, err := store.NewMigrator(connStr)
    require.NoError(t, err)
    defer migrator.Close()
    require.NoError(t, migrator.Up())

    conn, err := pgx.Connect(ctx, connStr)
    require.NoError(t, err)
    defer conn.Close(ctx)

    // dek_ref column
    var dataType, isNullable string
    err = conn.QueryRow(ctx, `
        SELECT data_type, is_nullable
          FROM information_schema.columns
         WHERE table_name = 'events_audit' AND column_name = 'dek_ref'
    `).Scan(&dataType, &isNullable)
    require.NoError(t, err)
    assert.Equal(t, "bigint", dataType)
    assert.Equal(t, "YES", isNullable)

    // dek_version column
    err = conn.QueryRow(ctx, `
        SELECT data_type, is_nullable
          FROM information_schema.columns
         WHERE table_name = 'events_audit' AND column_name = 'dek_version'
    `).Scan(&dataType, &isNullable)
    require.NoError(t, err)
    assert.Equal(t, "integer", dataType)
    assert.Equal(t, "YES", isNullable)

    // Partial index on dek_ref
    var indexCount int
    err = conn.QueryRow(ctx, `
        SELECT count(*)
          FROM pg_indexes
         WHERE tablename = 'events_audit' AND indexname = 'events_audit_dek_ref'
    `).Scan(&indexCount)
    require.NoError(t, err)
    assert.Equal(t, 1, indexCount)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `task test:int -- -run TestEventsAuditHasDEKColumnsAfterMigration014 ./internal/store/...`

Expected: FAIL — `column "dek_ref" does not exist`.

- [ ] **Step 3: Write the up migration**

```sql
-- internal/store/migrations/000014_events_audit_dek_columns.up.sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors
--
-- Adds two nullable columns + a partial index for sensitive event lookups.
-- NULL on both columns is the correct representation for codec=identity
-- rows (cleartext events have no DEK). No foreign key to crypto_keys —
-- Rekey destroys old crypto_keys rows by design (master spec §4.7).

ALTER TABLE events_audit
    ADD COLUMN IF NOT EXISTS dek_ref     BIGINT,
    ADD COLUMN IF NOT EXISTS dek_version INTEGER;

CREATE INDEX IF NOT EXISTS events_audit_dek_ref
    ON events_audit (dek_ref)
    WHERE dek_ref IS NOT NULL;
```

- [ ] **Step 4: Write the down migration**

```sql
-- internal/store/migrations/000014_events_audit_dek_columns.down.sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DROP INDEX IF EXISTS events_audit_dek_ref;

ALTER TABLE events_audit
    DROP COLUMN IF EXISTS dek_version,
    DROP COLUMN IF EXISTS dek_ref;
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `task test:int -- -run TestEventsAuditHasDEKColumnsAfterMigration014 ./internal/store/...`

Expected: PASS.

Then run the full migrator round-trip test:

Run: `task test:int -- -run TestMigrator_FullCycle ./internal/store/...`

Expected: PASS — Up applies all 14 migrations cleanly, Down reverts all 14.

- [ ] **Step 6: Commit**

```text
feat(store): migration 000014 — events_audit dek_ref and dek_version columns

Adds two nullable columns (dek_ref BIGINT, dek_version INTEGER) plus a
partial index on dek_ref. Existing cleartext rows leave both NULL;
Phase 3's audit projection will populate them for sensitive events. No
foreign key to crypto_keys — Rekey destroys those rows by design.

Refs: docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md §4.7
Refs: docs/superpowers/plans/2026-04-30-event-payload-crypto-phase2-substrate.md Task 2
Bead: holomush-8qri
```

---

## Task 3: `aad.Build` package

**Files:**

- Create: `internal/eventbus/crypto/aad/aad.go`
- Create: `internal/eventbus/crypto/aad/aad_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/eventbus/crypto/aad/aad_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package aad_test

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "google.golang.org/protobuf/types/known/timestamppb"

    eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
    "github.com/holomush/holomush/internal/eventbus/crypto/aad"
)

// newTestEvent returns a fully-populated Event for AAD tests. Modify
// fields in copies to test field-level tampering.
func newTestEvent() *eventbusv1.Event {
    return &eventbusv1.Event{
        Id:        []byte("0123456789ABCDEF"),
        Subject:   "events.game-1.scene.01ABC.ic",
        Type:      "core-communication:whisper",
        Timestamp: timestamppb.New(timeFromUnixNano(1714501234567890123)),
        Actor: &eventbusv1.Actor{
            Kind: eventbusv1.ActorKind_ACTOR_KIND_CHARACTER,
            Id:   []byte("01HXXX0000000000"),
        },
    }
}

func TestBuild_StartsWithMagicVersionPrefix(t *testing.T) {
    out := aad.Build(newTestEvent(), "xchacha20poly1305-v1", 42, 1)
    require.GreaterOrEqual(t, len(out), 6)
    assert.Equal(t, []byte("HMAAD\x01"), out[:6])
}

func TestBuild_DeterministicForIdenticalInputs(t *testing.T) {
    e := newTestEvent()
    a := aad.Build(e, "xchacha20poly1305-v1", 42, 1)
    b := aad.Build(e, "xchacha20poly1305-v1", 42, 1)
    assert.Equal(t, a, b, "Build must be deterministic for identical inputs")
}

func TestBuild_AnyFieldChange_ChangesOutput(t *testing.T) {
    base := newTestEvent()
    baseAAD := aad.Build(base, "xchacha20poly1305-v1", 42, 1)

    tests := []struct {
        name   string
        mutate func(e *eventbusv1.Event) (newCodec string, newDekRef uint64, newDekVer uint32)
    }{
        {"id changed", func(e *eventbusv1.Event) (string, uint64, uint32) {
            e.Id = []byte("FFFFFFFFFFFFFFFF")
            return "xchacha20poly1305-v1", 42, 1
        }},
        {"subject changed", func(e *eventbusv1.Event) (string, uint64, uint32) {
            e.Subject = "events.game-1.scene.01ABC.ooc"
            return "xchacha20poly1305-v1", 42, 1
        }},
        {"type changed", func(e *eventbusv1.Event) (string, uint64, uint32) {
            e.Type = "core-communication:say"
            return "xchacha20poly1305-v1", 42, 1
        }},
        {"timestamp changed", func(e *eventbusv1.Event) (string, uint64, uint32) {
            e.Timestamp = timestamppb.New(timeFromUnixNano(1714501234567890124))
            return "xchacha20poly1305-v1", 42, 1
        }},
        {"actor id changed", func(e *eventbusv1.Event) (string, uint64, uint32) {
            e.Actor.Id = []byte("01HYYY0000000000")
            return "xchacha20poly1305-v1", 42, 1
        }},
        {"actor kind changed", func(e *eventbusv1.Event) (string, uint64, uint32) {
            e.Actor.Kind = eventbusv1.ActorKind_ACTOR_KIND_PLUGIN
            return "xchacha20poly1305-v1", 42, 1
        }},
        {"codec name changed", func(e *eventbusv1.Event) (string, uint64, uint32) {
            return "aes-gcm-v1", 42, 1
        }},
        {"dek ref changed", func(e *eventbusv1.Event) (string, uint64, uint32) {
            return "xchacha20poly1305-v1", 43, 1
        }},
        {"dek version changed", func(e *eventbusv1.Event) (string, uint64, uint32) {
            return "xchacha20poly1305-v1", 42, 2
        }},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            mutated := newTestEvent()
            codec, dekRef, dekVer := tt.mutate(mutated)
            mutatedAAD := aad.Build(mutated, codec, dekRef, dekVer)
            assert.NotEqual(t, baseAAD, mutatedAAD,
                "tampering with %s must change AAD output", tt.name)
        })
    }
}

func TestBuild_ActorMarshalIsDeterministic(t *testing.T) {
    // Verifies the Actor proto submessage is canonicalized via
    // proto.MarshalOptions{Deterministic: true} — bare proto.Marshal
    // would silently produce non-byte-equal AAD across runs and break
    // INV-25.
    e := newTestEvent()

    first := aad.Build(e, "xchacha20poly1305-v1", 42, 1)
    for i := 0; i < 1000; i++ {
        next := aad.Build(e, "xchacha20poly1305-v1", 42, 1)
        require.Equal(t, first, next,
            "iteration %d produced different AAD bytes — Actor marshal not deterministic", i)
    }
}

func TestBuild_DekRefZero_ForIdentityCodec(t *testing.T) {
    // Cleartext events use codec=identity with no DEK. The function
    // should accept dekRef=0, dekVersion=0 and produce well-formed AAD.
    e := newTestEvent()
    out := aad.Build(e, "identity", 0, 0)
    assert.NotEmpty(t, out)
    assert.Equal(t, []byte("HMAAD\x01"), out[:6])
}

func timeFromUnixNano(ns int64) time.Time { return time.Unix(0, ns).UTC() }
```

Add the missing import:

```go
import (
    "time"
    // ... others from above
)
```

- [ ] **Step 2: Run tests to verify failure**

Run: `task test -- -run "TestBuild_" ./internal/eventbus/crypto/aad/...`

Expected: FAIL with `package aad does not exist` or `undefined: aad.Build`.

- [ ] **Step 3: Write the implementation**

```go
// internal/eventbus/crypto/aad/aad.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package aad builds the Additional Authenticated Data (AAD) bytes
// hashed by AEAD codecs. The canonicalization rule is fixed by master
// spec §4.2 and verified by INV-25: any tampering with cleartext
// metadata, codec name, or DEK reference changes the AAD bytes and
// breaks decryption with a tag-mismatch error.
//
// Phase 2 ships this function. Phase 3 codecs call it from Encode and
// Decode.
package aad

import (
    "encoding/binary"

    "google.golang.org/protobuf/proto"

    eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// magic is the 6-byte version prefix. Future v2 layouts may coexist by
// checking magic on Decode; only v1 is shipped in Phase 2.
var magic = []byte("HMAAD\x01")

// Build returns the canonical AAD bytes for an event under a given
// codec, DEK reference, and DEK version. The byte layout is:
//
//   "HMAAD\x01"                                 // 6 bytes
//   uint32(len(event.Id))                       // 4 bytes BE
//   event.Id                                    // 16 bytes (ULID)
//   uint32(len(event.Subject))                  // 4 bytes BE
//   []byte(event.Subject)                       // UTF-8
//   uint32(len(event.Type))                     // 4 bytes BE
//   []byte(event.Type)                          // UTF-8
//   int64(event.Timestamp.AsTime().UnixNano())  // 8 bytes BE
//   uint32(len(actorBytes))                     // 4 bytes BE
//   actorBytes                                  // proto Deterministic
//   uint32(len(codecName))                      // 4 bytes BE
//   []byte(codecName)                           // UTF-8
//   uint64(dekRef)                              // 8 bytes BE
//   uint32(dekVersion)                          // 4 bytes BE
//
// Identity codec passes dekRef=0, dekVersion=0; the magic prefix and
// per-field tampering still produce well-defined AAD.
func Build(event *eventbusv1.Event, codecName string, dekRef uint64, dekVersion uint32) []byte {
    actorBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(event.GetActor())
    if err != nil {
        // Build is called from inside the codec; an Actor marshal error
        // here would be a programmer bug (the proto is always valid).
        // Returning empty AAD would silently accept tampering, so we
        // panic to fail loudly. Phase 3 wraps this in a recover at the
        // codec boundary if needed.
        panic("aad: Actor proto marshal failed: " + err.Error())
    }

    eventID := event.GetId()
    subject := event.GetSubject()
    eventType := event.GetType()
    var ts int64
    if event.GetTimestamp() != nil {
        ts = event.GetTimestamp().AsTime().UnixNano()
    }

    size := len(magic) +
        4 + len(eventID) +
        4 + len(subject) +
        4 + len(eventType) +
        8 +
        4 + len(actorBytes) +
        4 + len(codecName) +
        8 +
        4
    out := make([]byte, 0, size)

    out = append(out, magic...)
    out = appendLengthPrefixed(out, eventID)
    out = appendLengthPrefixed(out, []byte(subject))
    out = appendLengthPrefixed(out, []byte(eventType))
    out = binary.BigEndian.AppendUint64(out, uint64(ts))
    out = appendLengthPrefixed(out, actorBytes)
    out = appendLengthPrefixed(out, []byte(codecName))
    out = binary.BigEndian.AppendUint64(out, dekRef)
    out = binary.BigEndian.AppendUint32(out, dekVersion)

    return out
}

func appendLengthPrefixed(dst, src []byte) []byte {
    dst = binary.BigEndian.AppendUint32(dst, uint32(len(src)))
    return append(dst, src...)
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `task test -- -run "TestBuild_" ./internal/eventbus/crypto/aad/...`

Expected: PASS — all five test functions and all nine subtests of `TestBuild_AnyFieldChange_ChangesOutput`.

- [ ] **Step 5: Run lint to verify the package compiles cleanly**

Run: `task lint`

Expected: zero findings on the new package.

- [ ] **Step 6: Commit**

```text
feat(crypto): aad.Build canonical AAD-bytes function

Pure substrate per master spec §4.2. Implements the HMAAD\x01-prefixed
canonical bytes used by Phase 3 AEAD codecs to bind cleartext metadata
to ciphertext via AAD. Actor submessage marshaled with
proto.MarshalOptions{Deterministic: true} to avoid INV-25 break across
runtime conditions.

INV-25 covered by aad_test.go's table-driven AnyFieldChange_ChangesOutput
plus a 1000-iteration determinism test on Actor marshaling.

No production callers in Phase 2; Phase 3's xchacha20poly1305-v1 codec
will call this from Encode/Decode.

Refs: docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md §4.2
Refs: docs/superpowers/plans/2026-04-30-event-payload-crypto-phase2-substrate.md Task 3
Bead: holomush-8qri
```

---

## Task 4: `dek.Material` opaque type + static API surface test

**Files:**

- Create: `internal/eventbus/crypto/dek/material.go`
- Create: `internal/eventbus/crypto/dek/material_test.go`
- Create: `internal/eventbus/crypto/dek/api_test.go`

- [ ] **Step 1: Write the failing material unit test**

```go
// internal/eventbus/crypto/dek/material_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek_test

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/eventbus/codec"
    "github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

func TestNewMaterial_ConstructsOpaqueWrapper(t *testing.T) {
    bytes := make([]byte, 32)
    for i := range bytes {
        bytes[i] = byte(i)
    }
    m := dek.NewMaterial(bytes)
    assert.NotNil(t, m)
}

func TestMaterial_AsCodecKey_ReturnsCodecKeyWithSameBytes(t *testing.T) {
    bytes := []byte("0123456789abcdef0123456789abcdef") // 32 bytes
    m := dek.NewMaterial(bytes)

    key := m.AsCodecKey(codec.KeyID(42))
    assert.Equal(t, codec.KeyID(42), key.ID)
    require.Len(t, key.Bytes, 32)
    assert.Equal(t, bytes, key.Bytes)
}

func TestMaterial_NewMaterial_CopiesInputBytes(t *testing.T) {
    // Defensive copy — caller must not be able to mutate Material's
    // internal bytes by retaining a reference to the input slice.
    src := []byte("0123456789abcdef0123456789abcdef")
    m := dek.NewMaterial(src)
    src[0] = 0xFF // mutate caller's slice

    key := m.AsCodecKey(codec.KeyID(1))
    assert.Equal(t, byte('0'), key.Bytes[0],
        "Material must defensively copy input; caller's mutation leaked")
}
```

- [ ] **Step 2: Write the failing static API surface test**

```go
// internal/eventbus/crypto/dek/api_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek_test

import (
    "go/types"
    "testing"

    "github.com/stretchr/testify/require"
    "golang.org/x/tools/go/packages"
)

// TestPackageHasNoExportedByteSlices guarantees the dek package never
// exposes an exported function/method returning []byte or an exported
// struct field of type []byte. This is the ground-truth defense for
// INV-27 — the ruleguard rules in gorules/ catch known sinks, but this
// test catches API drift (a future contributor adding a Bytes()
// accessor would bypass the ruleguards by introducing a new export).
func TestPackageHasNoExportedByteSlices(t *testing.T) {
    cfg := &packages.Config{
        Mode: packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax,
    }
    pkgs, err := packages.Load(cfg, "github.com/holomush/holomush/internal/eventbus/crypto/dek")
    require.NoError(t, err)
    require.Len(t, pkgs, 1)
    pkg := pkgs[0]
    require.NotNil(t, pkg.Types, "package types not loaded")

    scope := pkg.Types.Scope()
    for _, name := range scope.Names() {
        obj := scope.Lookup(name)
        if !obj.Exported() {
            continue
        }
        switch o := obj.(type) {
        case *types.Func:
            assertFuncDoesNotReturnByteSlice(t, o)
        case *types.TypeName:
            assertNamedTypeHasNoByteSliceFields(t, o)
            // Method set on the named type
            if named, ok := o.Type().(*types.Named); ok {
                for i := 0; i < named.NumMethods(); i++ {
                    m := named.Method(i)
                    if m.Exported() {
                        assertFuncDoesNotReturnByteSlice(t, m)
                    }
                }
                // Pointer method set
                ptrMethods := types.NewMethodSet(types.NewPointer(named))
                for i := 0; i < ptrMethods.Len(); i++ {
                    sel := ptrMethods.At(i)
                    if fn, ok := sel.Obj().(*types.Func); ok && fn.Exported() {
                        assertFuncDoesNotReturnByteSlice(t, fn)
                    }
                }
            }
        }
    }
}

func assertFuncDoesNotReturnByteSlice(t *testing.T, fn *types.Func) {
    t.Helper()
    sig, ok := fn.Type().(*types.Signature)
    if !ok {
        return
    }
    results := sig.Results()
    for i := 0; i < results.Len(); i++ {
        r := results.At(i)
        if isByteSlice(r.Type()) {
            t.Fatalf("INV-27 violation: dek.%s returns []byte at result position %d. "+
                "If you need to expose key bytes, route through codec.Key (which is "+
                "lint-allowlisted via gorules/codec_key_bytes_allowlist.go).",
                fn.Name(), i)
        }
    }
}

func assertNamedTypeHasNoByteSliceFields(t *testing.T, tn *types.TypeName) {
    t.Helper()
    s, ok := tn.Type().Underlying().(*types.Struct)
    if !ok {
        return
    }
    for i := 0; i < s.NumFields(); i++ {
        f := s.Field(i)
        if !f.Exported() {
            continue
        }
        if isByteSlice(f.Type()) {
            t.Fatalf("INV-27 violation: dek.%s.%s is an exported []byte field. "+
                "Make it unexported and use codec.Key for the egress path.",
                tn.Name(), f.Name())
        }
    }
}

func isByteSlice(t types.Type) bool {
    sl, ok := t.(*types.Slice)
    if !ok {
        return false
    }
    basic, ok := sl.Elem().(*types.Basic)
    return ok && basic.Kind() == types.Uint8
}
```

- [ ] **Step 3: Run tests to verify failure**

Run: `task test -- -run "TestNewMaterial_|TestMaterial_|TestPackageHasNoExportedByteSlices" ./internal/eventbus/crypto/dek/...`

Expected: FAIL with `package dek does not exist` or `undefined: dek.NewMaterial`.

- [ ] **Step 4: Write the Material implementation**

```go
// internal/eventbus/crypto/dek/material.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package dek owns Data Encryption Key (DEK) lifecycle and material
// containment. The Material type is opaque by construction: it has no
// exported []byte accessor. The sole exported egress is AsCodecKey,
// which constructs a substrate codec.Key inline. The codec.Key.Bytes
// field is the residual leakage path; reads are gated by the ruleguard
// rule at gorules/codec_key_bytes_allowlist.go.
//
// Phase 2 ships Material plus the Manager skeleton (GetOrCreate +
// Resolve real; Add/Rotate/Rekey stubbed with tracking_bead). Phase 3
// wires Material into the codec encrypt/decrypt path.
package dek

import "github.com/holomush/holomush/internal/eventbus/codec"

// Material wraps unwrapped DEK bytes. Construction is via NewMaterial.
// The struct has no exported fields and no exported []byte accessor
// (enforced by api_test.go's static API surface check).
type Material struct {
    bytes []byte
}

// NewMaterial constructs an opaque Material wrapping a defensive copy
// of the input bytes. Callers MUST NOT retain a reference to the input
// slice and expect Material to mirror their mutations — the input is
// copied at construction.
func NewMaterial(bytes []byte) *Material {
    cp := make([]byte, len(bytes))
    copy(cp, bytes)
    return &Material{bytes: cp}
}

// AsCodecKey constructs a codec.Key with the given KeyID and the
// Material's underlying bytes. The returned codec.Key.Bytes shares
// backing memory with this Material (no further copy); reads of the
// returned key's Bytes field outside the codec/crypto package trees
// fail lint via gorules/codec_key_bytes_allowlist.go.
func (m *Material) AsCodecKey(id codec.KeyID) codec.Key {
    return codec.Key{ID: id, Bytes: m.bytes}
}
```

- [ ] **Step 5: Promote `golang.org/x/tools` to a direct dependency**

The static API surface test imports `golang.org/x/tools/go/packages`.
This module is currently an indirect dep in `go.mod` (line 154). Run:

```bash
go get golang.org/x/tools@latest
go mod tidy
```

Verify `go.mod` now lists `golang.org/x/tools` as a direct dependency
(no `// indirect` suffix on the `require` line).

- [ ] **Step 6: Run tests to verify pass**

Run: `task test -- -run "TestNewMaterial_|TestMaterial_|TestPackageHasNoExportedByteSlices" ./internal/eventbus/crypto/dek/...`

Expected: PASS — all four tests succeed; `TestPackageHasNoExportedByteSlices` confirms the API surface invariant.

- [ ] **Step 7: Commit**

```text
feat(crypto): dek.Material opaque type + API surface test

Material wraps unwrapped DEK bytes with no exported []byte accessor.
Sole egress is AsCodecKey(codec.KeyID) which constructs codec.Key
inline. The static API surface test (api_test.go) asserts the dek
package exports no function/method returning []byte and no exported
struct field of type []byte — ground-truth defense for INV-27.

The residual leakage path (codec.Key.Bytes reads) is gated separately
by a ruleguard rule landed in Task 10.

Refs: docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md §5.1, INV-27
Refs: docs/superpowers/plans/2026-04-30-event-payload-crypto-phase2-substrate.md Task 4
Bead: holomush-8qri
```

---

## Task 5: KEK provider + source interfaces, EnvSource, NoneProvider

**Files:**

- Create: `internal/eventbus/crypto/kek/provider.go`
- Create: `internal/eventbus/crypto/kek/source.go`
- Create: `internal/eventbus/crypto/kek/source_env.go`
- Create: `internal/eventbus/crypto/kek/source_env_test.go`
- Create: `internal/eventbus/crypto/kek/none.go`
- Create: `internal/eventbus/crypto/kek/none_test.go`
- Create: `internal/eventbus/crypto/kek/none_integration_test.go`

- [ ] **Step 1: Write the failing tests for `EnvSource` and `NoneProvider.Wrap`**

```go
// internal/eventbus/crypto/kek/source_env_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package kek_test

import (
    "context"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/eventbus/crypto/kek"
    "github.com/holomush/holomush/pkg/errutil"
)

// validHexKEK is 64 hex chars decoding to 32 bytes.
const validHexKEK = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestEnvSource_Load_ReturnsKEKBytes(t *testing.T) {
    t.Setenv("HOLOMUSH_TEST_KEK", validHexKEK)
    src := kek.NewEnvSource("HOLOMUSH_TEST_KEK", false /* prodMode */)
    bytes, err := src.Load(context.Background())
    require.NoError(t, err)
    assert.Len(t, bytes, 32)
}

func TestEnvSource_Load_RefusesInProdMode(t *testing.T) {
    t.Setenv("HOLOMUSH_TEST_KEK", validHexKEK)
    src := kek.NewEnvSource("HOLOMUSH_TEST_KEK", true /* prodMode */)
    _, err := src.Load(context.Background())
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "KEK_ENV_SOURCE_PROD_FORBIDDEN")
}

func TestEnvSource_Load_FailsOnMissingEnvVar(t *testing.T) {
    src := kek.NewEnvSource("HOLOMUSH_NEVER_SET_KEK", false)
    _, err := src.Load(context.Background())
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "KEK_ENV_VAR_MISSING")
}

func TestEnvSource_Load_FailsOnWrongLength(t *testing.T) {
    t.Setenv("HOLOMUSH_TEST_KEK_SHORT", "deadbeef") // 8 hex chars → 4 bytes
    src := kek.NewEnvSource("HOLOMUSH_TEST_KEK_SHORT", false)
    _, err := src.Load(context.Background())
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "KEK_ENV_VAR_WRONG_LENGTH")
}

func TestEnvSource_Load_FailsOnNonHex(t *testing.T) {
    t.Setenv("HOLOMUSH_TEST_KEK_BAD", "not hex at all !!!!")
    src := kek.NewEnvSource("HOLOMUSH_TEST_KEK_BAD", false)
    _, err := src.Load(context.Background())
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "KEK_ENV_VAR_NOT_HEX")
}

func TestEnvSource_Persist_Refused(t *testing.T) {
    src := kek.NewEnvSource("HOLOMUSH_TEST_KEK", false)
    err := src.Persist(context.Background(), make([]byte, 32))
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "KEK_ENV_SOURCE_READ_ONLY")
}

func TestEnvSource_Name_IsLocalAEADEnv(t *testing.T) {
    src := kek.NewEnvSource("HOLOMUSH_TEST_KEK", false)
    assert.Equal(t, "local-aead/env", src.Name())
}
```

```go
// internal/eventbus/crypto/kek/none_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package kek_test

import (
    "context"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/eventbus/crypto/kek"
    "github.com/holomush/holomush/pkg/errutil"
)

func TestNoneProvider_Wrap_RefusesWithTypedError(t *testing.T) {
    // INV-34: NoneProvider.Wrap MUST refuse and surface a typed error.
    provider := kek.NewNoneProviderForUnitTest() // skips DB check; tests Wrap path only
    _, _, err := provider.Wrap(context.Background(), make([]byte, 32))
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "CRYPTO_NONE_PROVIDER_WRAP_REFUSED")
}

func TestNoneProvider_Unwrap_RefusesWithTypedError(t *testing.T) {
    provider := kek.NewNoneProviderForUnitTest()
    _, err := provider.Unwrap(context.Background(), []byte("anything"), "any-key-id")
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "CRYPTO_NONE_PROVIDER_UNWRAP_REFUSED")
}

func TestNoneProvider_HealthCheck_Succeeds(t *testing.T) {
    // HealthCheck has no preconditions; NoneProvider is "healthy" in
    // the sense that it functions as designed (refuses crypto ops).
    provider := kek.NewNoneProviderForUnitTest()
    assert.NoError(t, provider.HealthCheck(context.Background()))
}

func TestNoneProvider_Name_IsNone(t *testing.T) {
    provider := kek.NewNoneProviderForUnitTest()
    assert.Equal(t, "none", provider.Name())
}
```

```go
// internal/eventbus/crypto/kek/none_integration_test.go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package kek_test

import (
    "context"
    "testing"

    "github.com/jackc/pgx/v5"
    "github.com/stretchr/testify/require"
    "github.com/testcontainers/testcontainers-go"
    "github.com/testcontainers/testcontainers-go/modules/postgres"
    "github.com/testcontainers/testcontainers-go/wait"

    "github.com/holomush/holomush/internal/eventbus/crypto/kek"
    "github.com/holomush/holomush/internal/store"
    "github.com/holomush/holomush/pkg/errutil"
)

// TestNoneProvider_Constructor_RefusesIfCryptoKeysNonempty verifies INV-32:
// startup with provider.name=none MUST refuse if any crypto_keys row exists.
// Enforced at constructor time (synchronous DB SELECT).
func TestNoneProvider_Constructor_RefusesIfCryptoKeysNonempty(t *testing.T) {
    ctx := context.Background()
    pgContainer, err := postgres.Run(ctx,
        "postgres:18-alpine",
        postgres.WithDatabase("test"),
        postgres.WithUsername("test"),
        postgres.WithPassword("test"),
        testcontainers.WithWaitStrategy(
            wait.ForLog("database system is ready to accept connections").
                WithOccurrence(2)),
    )
    require.NoError(t, err)
    defer pgContainer.Terminate(ctx)

    connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
    require.NoError(t, err)

    migrator, err := store.NewMigrator(connStr)
    require.NoError(t, err)
    defer migrator.Close()
    require.NoError(t, migrator.Up())

    pool, err := pgx.Connect(ctx, connStr)
    require.NoError(t, err)
    defer pool.Close(ctx)

    // Empty table: constructor succeeds.
    provider, err := kek.NewNoneProvider(ctx, pool)
    require.NoError(t, err)
    require.NotNil(t, provider)

    // Insert a row (simulating a previously-encrypted deployment).
    _, err = pool.Exec(ctx, `
        INSERT INTO crypto_keys
            (context_type, context_id, version, wrapped_dek, wrap_provider, wrap_key_id, participants)
        VALUES ('scene', 'test-scene', 1, '\x00', 'local-aead/file', 'kek-fingerprint', '[]')
    `)
    require.NoError(t, err)

    // Non-empty table: constructor refuses.
    _, err = kek.NewNoneProvider(ctx, pool)
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "CRYPTO_KEYS_NONEMPTY_WITH_NONE_PROVIDER")
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `task test -- -run "TestEnvSource_|TestNoneProvider_" ./internal/eventbus/crypto/kek/...`

Expected: FAIL with `package kek does not exist`.

- [ ] **Step 3: Write the `Provider` and `KEKSource` interfaces**

```go
// internal/eventbus/crypto/kek/provider.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package kek defines the Key Encryption Key provider stack: an
// abstract Provider interface and concrete implementations for local
// AEAD (with pluggable KEK source) and a no-op None provider.
//
// Per master spec §5.1 "Layer 1": providers see only opaque DEK bytes.
// They never see event payloads or DEK semantic context (which scene,
// which version). All event-context routing lives in dek.Manager.
package kek

import "context"

// Provider wraps and unwraps Data Encryption Keys (DEKs) using a
// master Key Encryption Key (KEK) it manages internally.
// Implementations MUST keep KEK material out of process memory
// whenever possible; LocalAEADProvider necessarily holds it in process
// for the life of the server, while VaultTransitProvider (Phase 6)
// keeps it remote.
type Provider interface {
    // Name returns the provider identifier persisted in
    // crypto_keys.wrap_provider. Examples: "local-aead/file",
    // "local-aead/env", "vault-transit", "none".
    Name() string

    // Wrap encrypts dek under the current KEK version. Returns the
    // wrapped bytes and a provider-specific kekKeyID identifying which
    // KEK version was used.
    Wrap(ctx context.Context, dek []byte) (wrapped []byte, kekKeyID string, err error)

    // Unwrap decrypts wrapped using the KEK identified by kekKeyID.
    Unwrap(ctx context.Context, wrapped []byte, kekKeyID string) (dek []byte, err error)

    // RotateKEK creates a new KEK version. Phase 4+ uses this; Phase 2
    // ships the method but production callers are out of scope.
    RotateKEK(ctx context.Context) (newKEKKeyID string, err error)

    // HealthCheck verifies the provider is reachable and the KEK is
    // available. Used by the readiness probe.
    HealthCheck(ctx context.Context) error
}
```

```go
// internal/eventbus/crypto/kek/source.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package kek

import "context"

// KEKSource fetches and refreshes master KEK material from a backing
// store. The KEK never leaves the LocalAEADProvider's process memory
// once Load returns. Implementations are tagged by Name; the tag is
// persisted in crypto_keys.wrap_provider as "local-aead/<source-name>".
type KEKSource interface {
    Name() string
    Load(ctx context.Context) ([]byte, error)
    // Persist stores new KEK material after rotation. Some sources
    // (env, systemd-credential) are read-only and return a typed
    // CRYPTO_*_READ_ONLY error; rotation requires a different path
    // for those.
    Persist(ctx context.Context, kek []byte) error
}
```

- [ ] **Step 4: Write the `EnvSource` implementation**

```go
// internal/eventbus/crypto/kek/source_env.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package kek

import (
    "context"
    "encoding/hex"
    "os"

    "github.com/samber/oops"
)

const (
    // KEKByteLength is the required length of a KEK in bytes (256 bits)
    // — chacha20poly1305 key size.
    KEKByteLength = 32

    // EnvSourceName is the canonical KEKSource.Name() value.
    EnvSourceName = "local-aead/env"
)

// EnvSource reads the master KEK from an environment variable.
// Refused in production mode; intended for unit and integration tests.
// The env value MUST be 64 hex characters (32 bytes after decode).
type EnvSource struct {
    envVar   string
    prodMode bool
}

// NewEnvSource constructs an EnvSource. prodMode=true causes Load to
// return CRYPTO_KEK_ENV_SOURCE_PROD_FORBIDDEN; tests pass false.
func NewEnvSource(envVar string, prodMode bool) *EnvSource {
    return &EnvSource{envVar: envVar, prodMode: prodMode}
}

// Name returns "local-aead/env".
func (s *EnvSource) Name() string { return EnvSourceName }

// Load decodes the hex-encoded KEK from the configured env var. Strict
// hex (64 chars → 32 bytes) — no raw-bytes fallback to avoid ambiguity
// when an ASCII KEK happens to also be valid hex.
func (s *EnvSource) Load(ctx context.Context) ([]byte, error) {
    if s.prodMode {
        return nil, oops.Code("KEK_ENV_SOURCE_PROD_FORBIDDEN").
            With("env_var", s.envVar).
            Errorf("env KEKSource is dev/test only — refused in production mode")
    }
    raw, ok := os.LookupEnv(s.envVar)
    if !ok || raw == "" {
        return nil, oops.Code("KEK_ENV_VAR_MISSING").
            With("env_var", s.envVar).
            Errorf("env var %q not set or empty", s.envVar)
    }
    decoded, err := hex.DecodeString(raw)
    if err != nil {
        return nil, oops.Code("KEK_ENV_VAR_NOT_HEX").
            With("env_var", s.envVar).
            Wrap(err)
    }
    if len(decoded) != KEKByteLength {
        return nil, oops.Code("KEK_ENV_VAR_WRONG_LENGTH").
            With("env_var", s.envVar).
            With("expected_bytes", KEKByteLength).
            With("got_bytes", len(decoded)).
            Errorf("env var %q must decode to %d bytes (64 hex chars); got %d bytes",
                s.envVar, KEKByteLength, len(decoded))
    }
    return decoded, nil
}

// Persist refuses (env is read-only).
func (s *EnvSource) Persist(ctx context.Context, kek []byte) error {
    return oops.Code("KEK_ENV_SOURCE_READ_ONLY").
        With("env_var", s.envVar).
        Errorf("env KEKSource cannot persist; rotate via a writable source")
}
```

- [ ] **Step 5: Write the `NoneProvider` implementation**

```go
// internal/eventbus/crypto/kek/none.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package kek

import (
    "context"

    "github.com/jackc/pgx/v5"
    "github.com/samber/oops"
)

// NoneProvider is a dev-and-test sentinel that refuses all crypto
// operations. It exists so deployments running with
// crypto.provider.name=none can boot without a master key, while
// guaranteeing they cannot accidentally publish sensitive events.
//
// Two invariants the provider enforces:
//   - INV-32: at construction, refuse if any crypto_keys row exists.
//             A row implies prior encryption with a real provider; with
//             NoneProvider the historical DEKs are unreachable.
//   - INV-34: at runtime, refuse Wrap/Unwrap.
type NoneProvider struct{}

// PGQuerier is the minimal pgx surface NewNoneProvider needs.
// Accepting an interface keeps the constructor testable without a real
// connection in unit tests, and matches *pgx.Conn / *pgxpool.Pool in
// production.
type PGQuerier interface {
    QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// NewNoneProvider constructs a NoneProvider after verifying INV-32 (no
// crypto_keys rows exist). The DB SELECT runs synchronously; a non-empty
// table returns CRYPTO_KEYS_NONEMPTY_WITH_NONE_PROVIDER and the
// constructor caller (server boot) refuses to start.
func NewNoneProvider(ctx context.Context, db PGQuerier) (*NoneProvider, error) {
    var count int
    if err := db.QueryRow(ctx, "SELECT count(*) FROM crypto_keys").Scan(&count); err != nil {
        return nil, oops.Code("CRYPTO_KEYS_COUNT_QUERY_FAILED").Wrap(err)
    }
    if count > 0 {
        return nil, oops.Code("CRYPTO_KEYS_NONEMPTY_WITH_NONE_PROVIDER").
            With("crypto_keys_row_count", count).
            Errorf("none provider refuses to start: %d crypto_keys row(s) exist; "+
                "use the same provider that wrote those rows or migrate via "+
                "`holomush crypto provider-migrate` (Phase 6)", count)
    }
    return &NoneProvider{}, nil
}

// NewNoneProviderForUnitTest constructs a NoneProvider without the DB
// check. Tests of Wrap/Unwrap/HealthCheck/Name use this path; the DB
// integrity check is exercised separately in
// none_integration_test.go.
func NewNoneProviderForUnitTest() *NoneProvider { return &NoneProvider{} }

// Name returns "none".
func (p *NoneProvider) Name() string { return "none" }

// Wrap refuses (INV-34). Surfaces at emit-time when Phase 3 calls
// DEKManager.GetOrCreate for a sensitive event.
func (p *NoneProvider) Wrap(ctx context.Context, dek []byte) ([]byte, string, error) {
    return nil, "", oops.Code("CRYPTO_NONE_PROVIDER_WRAP_REFUSED").
        Errorf("none provider cannot wrap; configure a real provider to publish sensitive events")
}

// Unwrap refuses. There are no rows for it to unwrap (INV-32 guarantees
// the table was empty at construction); a call here implies a logic bug.
func (p *NoneProvider) Unwrap(ctx context.Context, wrapped []byte, kekKeyID string) ([]byte, error) {
    return nil, oops.Code("CRYPTO_NONE_PROVIDER_UNWRAP_REFUSED").
        With("kek_key_id", kekKeyID).
        Errorf("none provider cannot unwrap; this should be unreachable when INV-32 holds")
}

// RotateKEK refuses.
func (p *NoneProvider) RotateKEK(ctx context.Context) (string, error) {
    return "", oops.Code("CRYPTO_NONE_PROVIDER_ROTATE_REFUSED").
        Errorf("none provider has no KEK to rotate")
}

// HealthCheck succeeds — NoneProvider is "healthy" in the sense it
// reliably refuses operations.
func (p *NoneProvider) HealthCheck(ctx context.Context) error { return nil }
```

- [ ] **Step 6: Run unit tests to verify pass**

Run: `task test -- -run "TestEnvSource_|TestNoneProvider_" ./internal/eventbus/crypto/kek/...`

Expected: PASS — env source unit tests + NoneProvider Wrap/Unwrap/HealthCheck/Name unit tests pass.

- [ ] **Step 7: Run the integration test to verify INV-32**

Run: `task test:int -- -run TestNoneProvider_Constructor_RefusesIfCryptoKeysNonempty ./internal/eventbus/crypto/kek/...`

Expected: PASS — empty table allows construction; row insertion causes constructor refusal with `CRYPTO_KEYS_NONEMPTY_WITH_NONE_PROVIDER`.

- [ ] **Step 8: Commit**

```text
feat(crypto): KEK Provider/KEKSource interfaces, EnvSource, NoneProvider

Substrate per master spec §5.1, §5.5. Provider is the abstract crypto
interface; KEKSource is the pluggable backing store for LocalAEADProvider's
master key. EnvSource is the dev/test-only sentinel (refused in prod
mode). NoneProvider is the no-op crypto provider that enforces INV-32
(constructor-time DB SELECT refuses startup if crypto_keys non-empty)
and INV-34 (Wrap refusal at runtime).

Tests cover the env-source happy path, prod-mode refusal, missing-var,
wrong-length, persist-refused; NoneProvider Wrap/Unwrap/HealthCheck/Name
unit tests; INV-32 integration test using a postgres testcontainer.

No production callers in Phase 2; Phase 3 wires LocalAEADProvider into
DEKManager.

Refs: docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md §5.1, §5.5
Refs: docs/superpowers/plans/2026-04-30-event-payload-crypto-phase2-substrate.md Task 5
Bead: holomush-8qri
```

---

## Task 6: `kek.FileSource` (Argon2id-derived unlock + AEAD-wrapped key file)

**Files:**

- Create: `internal/eventbus/crypto/kek/source_file.go`
- Create: `internal/eventbus/crypto/kek/source_file_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/eventbus/crypto/kek/source_file_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package kek_test

import (
    "context"
    "crypto/rand"
    "os"
    "path/filepath"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/eventbus/crypto/kek"
    "github.com/holomush/holomush/pkg/errutil"
)

// staticPassphraseFunc returns a PassphraseFunc that always yields the
// given passphrase — useful for tests that need a known passphrase.
func staticPassphraseFunc(passphrase string) kek.PassphraseFunc {
    return func(ctx context.Context) ([]byte, error) {
        return []byte(passphrase), nil
    }
}

func TestFileSource_LoadDerivesKEKDeterministically(t *testing.T) {
    tmp := t.TempDir()
    keyFile := filepath.Join(tmp, "master.key.enc")

    // Mint a fresh KEK and write it through FileSource.Persist.
    kekBytes := make([]byte, kek.KEKByteLength)
    _, err := rand.Read(kekBytes)
    require.NoError(t, err)

    src := kek.NewFileSource(keyFile, staticPassphraseFunc("correct horse battery staple"))
    require.NoError(t, src.Persist(context.Background(), kekBytes))

    // Round-trip: Load returns the same bytes.
    got, err := src.Load(context.Background())
    require.NoError(t, err)
    assert.Equal(t, kekBytes, got)

    // Loading again returns the same bytes (idempotent).
    got2, err := src.Load(context.Background())
    require.NoError(t, err)
    assert.Equal(t, kekBytes, got2)
}

func TestFileSource_Load_FailsOnWrongPassphrase(t *testing.T) {
    tmp := t.TempDir()
    keyFile := filepath.Join(tmp, "master.key.enc")
    kekBytes := make([]byte, kek.KEKByteLength)
    _, err := rand.Read(kekBytes)
    require.NoError(t, err)

    writeSrc := kek.NewFileSource(keyFile, staticPassphraseFunc("right"))
    require.NoError(t, writeSrc.Persist(context.Background(), kekBytes))

    readSrc := kek.NewFileSource(keyFile, staticPassphraseFunc("wrong"))
    _, err = readSrc.Load(context.Background())
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "KEK_PASSPHRASE_INVALID")
}

func TestFileSource_Load_FailsOnMissingFile(t *testing.T) {
    src := kek.NewFileSource("/nonexistent/master.key.enc", staticPassphraseFunc("any"))
    _, err := src.Load(context.Background())
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "KEK_FILE_LOAD_FAILED")
}

func TestFileSource_Load_FailsOnCorruptMagic(t *testing.T) {
    tmp := t.TempDir()
    keyFile := filepath.Join(tmp, "master.key.enc")
    require.NoError(t, os.WriteFile(keyFile, []byte("XXXX"), 0o600))

    src := kek.NewFileSource(keyFile, staticPassphraseFunc("any"))
    _, err := src.Load(context.Background())
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "KEK_FILE_FORMAT_INVALID")
}

func TestFileSource_Name_IsLocalAEADFile(t *testing.T) {
    src := kek.NewFileSource("/tmp/x", staticPassphraseFunc(""))
    assert.Equal(t, "local-aead/file", src.Name())
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `task test -- -run "TestFileSource_" ./internal/eventbus/crypto/kek/...`

Expected: FAIL with `undefined: kek.NewFileSource` and `undefined: kek.PassphraseFunc`.

- [ ] **Step 3: Write the `FileSource` implementation**

```go
// internal/eventbus/crypto/kek/source_file.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package kek

import (
    "bytes"
    "context"
    "crypto/rand"
    "io"
    "os"

    "github.com/samber/oops"
    "golang.org/x/crypto/argon2"
    "golang.org/x/crypto/chacha20poly1305"
)

// FileSourceName is the canonical KEKSource.Name() value.
const FileSourceName = "local-aead/file"

// fileMagic identifies version 1 of the key-file format.
var fileMagic = []byte("HMK\x01")

// Argon2id parameters for passphrase derivation per master spec §5.3.
// 64 MiB memory, 3 iterations, 4-way parallelism, 32-byte output.
const (
    argonMemoryKiB uint32 = 64 * 1024
    argonTime      uint32 = 3
    argonThreads   uint8  = 4
    argonKeyLen    uint32 = 32
    saltLen               = 16
    nonceLen              = chacha20poly1305.NonceSizeX // 24 (XChaCha20)
)

// PassphraseFunc supplies the unlock passphrase. Implementations
// prompt at the CLI, read from a credential, or (in tests) return a
// fixed string.
type PassphraseFunc func(ctx context.Context) ([]byte, error)

// FileSource reads and writes a master KEK to a passphrase-encrypted
// file. File format v1:
//
//   magic   = "HMK\x01"      (4 bytes)
//   salt    = 16 bytes (Argon2id salt)
//   nonce   = 24 bytes (XChaCha20-Poly1305 nonce)
//   wrapped = N bytes (ciphertext + 16-byte AEAD tag)
//
// Argon2id derives a 32-byte unlock key from passphrase + salt; that
// key opens the AEAD-sealed wrapped KEK.
type FileSource struct {
    path           string
    passphraseFunc PassphraseFunc
}

// NewFileSource constructs a FileSource. passphraseFunc supplies the
// unlock passphrase on Load and Persist.
func NewFileSource(path string, passphraseFunc PassphraseFunc) *FileSource {
    return &FileSource{path: path, passphraseFunc: passphraseFunc}
}

// Name returns "local-aead/file".
func (s *FileSource) Name() string { return FileSourceName }

// Load reads the key file, derives the unlock key from passphrase +
// salt via Argon2id, and AEAD-decrypts the wrapped KEK.
func (s *FileSource) Load(ctx context.Context) ([]byte, error) {
    raw, err := os.ReadFile(s.path)
    if err != nil {
        return nil, oops.Code("KEK_FILE_LOAD_FAILED").
            With("path", s.path).
            Wrap(err)
    }
    if len(raw) < len(fileMagic)+saltLen+nonceLen+chacha20poly1305.Overhead {
        return nil, oops.Code("KEK_FILE_FORMAT_INVALID").
            With("path", s.path).
            With("size", len(raw)).
            Errorf("key file too short")
    }
    if !bytes.Equal(raw[:len(fileMagic)], fileMagic) {
        return nil, oops.Code("KEK_FILE_FORMAT_INVALID").
            With("path", s.path).
            Errorf("key file magic prefix mismatch")
    }

    offset := len(fileMagic)
    salt := raw[offset : offset+saltLen]
    offset += saltLen
    nonce := raw[offset : offset+nonceLen]
    offset += nonceLen
    wrapped := raw[offset:]

    passphrase, err := s.passphraseFunc(ctx)
    if err != nil {
        return nil, oops.Code("KEK_PASSPHRASE_FETCH_FAILED").Wrap(err)
    }

    unlockKey := argon2.IDKey(passphrase, salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
    aead, err := chacha20poly1305.NewX(unlockKey)
    if err != nil {
        return nil, oops.Code("KEK_AEAD_CONSTRUCT_FAILED").Wrap(err)
    }

    kekBytes, err := aead.Open(nil, nonce, wrapped, nil)
    if err != nil {
        return nil, oops.Code("KEK_PASSPHRASE_INVALID").
            With("path", s.path).
            Errorf("AEAD open failed — wrong passphrase or corrupt file")
    }
    if len(kekBytes) != KEKByteLength {
        return nil, oops.Code("KEK_FILE_FORMAT_INVALID").
            With("path", s.path).
            With("kek_bytes", len(kekBytes)).
            Errorf("unwrapped KEK has wrong length: expected %d, got %d", KEKByteLength, len(kekBytes))
    }
    return kekBytes, nil
}

// Persist writes a fresh key file using the configured passphrase.
// Generates a new random salt + nonce on each call (rotation-safe).
func (s *FileSource) Persist(ctx context.Context, kekBytes []byte) error {
    if len(kekBytes) != KEKByteLength {
        return oops.Code("KEK_BYTE_LENGTH_INVALID").
            With("expected", KEKByteLength).
            With("got", len(kekBytes)).
            Errorf("KEK must be %d bytes; got %d", KEKByteLength, len(kekBytes))
    }
    passphrase, err := s.passphraseFunc(ctx)
    if err != nil {
        return oops.Code("KEK_PASSPHRASE_FETCH_FAILED").Wrap(err)
    }

    salt := make([]byte, saltLen)
    if _, err := io.ReadFull(rand.Reader, salt); err != nil {
        return oops.Code("KEK_FILE_RNG_FAILED").Wrap(err)
    }
    nonce := make([]byte, nonceLen)
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return oops.Code("KEK_FILE_RNG_FAILED").Wrap(err)
    }

    unlockKey := argon2.IDKey(passphrase, salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
    aead, err := chacha20poly1305.NewX(unlockKey)
    if err != nil {
        return oops.Code("KEK_AEAD_CONSTRUCT_FAILED").Wrap(err)
    }
    wrapped := aead.Seal(nil, nonce, kekBytes, nil)

    var buf bytes.Buffer
    buf.Write(fileMagic)
    buf.Write(salt)
    buf.Write(nonce)
    buf.Write(wrapped)

    if err := os.WriteFile(s.path, buf.Bytes(), 0o600); err != nil {
        return oops.Code("KEK_FILE_WRITE_FAILED").
            With("path", s.path).
            Wrap(err)
    }
    return nil
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `task test -- -run "TestFileSource_" ./internal/eventbus/crypto/kek/...`

Expected: PASS — round-trip, wrong-passphrase rejection, missing-file error, corrupt-magic error, name correctness.

- [ ] **Step 5: Commit**

```text
feat(crypto): kek.FileSource — Argon2id-derived unlock for master KEK file

FileSource implements the v1 default KEKSource per master spec §5.3.
File format: HMK\x01 magic + 16-byte Argon2 salt + 24-byte XChaCha20
nonce + AEAD-wrapped KEK + tag. Argon2id parameters: m=64MiB, t=3, p=4.

Tests cover the round-trip happy path, wrong-passphrase rejection
(KEK_PASSPHRASE_INVALID), missing-file (KEK_FILE_LOAD_FAILED),
corrupt-magic (KEK_FILE_FORMAT_INVALID), and Name() correctness.

PassphraseFunc abstracts the passphrase source so tests pass static
strings while production callers will wire in a stdin prompt or systemd
credential reader.

Refs: docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md §5.3
Refs: docs/superpowers/plans/2026-04-30-event-payload-crypto-phase2-substrate.md Task 6
Bead: holomush-8qri
```

---

## Task 7: `kek.LocalAEADProvider`

**Files:**

- Create: `internal/eventbus/crypto/kek/local_aead.go`
- Create: `internal/eventbus/crypto/kek/local_aead_test.go`
- Create: `internal/eventbus/crypto/kek/local_aead_integration_test.go`

- [ ] **Step 1: Write the failing unit tests**

```go
// internal/eventbus/crypto/kek/local_aead_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package kek_test

import (
    "context"
    "crypto/rand"
    "encoding/hex"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/eventbus/crypto/kek"
    "github.com/holomush/holomush/pkg/errutil"
)

func newKEKBytes(t *testing.T) []byte {
    t.Helper()
    b := make([]byte, kek.KEKByteLength)
    _, err := rand.Read(b)
    require.NoError(t, err)
    return b
}

// envSourceWith returns an EnvSource backed by a per-test env var.
// kekBytes is hex-encoded into the env var to satisfy EnvSource's
// strict-hex parser.
func envSourceWith(t *testing.T, kekBytes []byte) *kek.EnvSource {
    t.Helper()
    name := "TEST_KEK_" + sanitizeTestName(t.Name())
    t.Setenv(name, hex.EncodeToString(kekBytes))
    return kek.NewEnvSource(name, false)
}

// sanitizeTestName strips characters that env var names disallow
// (slashes from t.Run subtest names, etc.).
func sanitizeTestName(s string) string {
    out := make([]byte, 0, len(s))
    for i := 0; i < len(s); i++ {
        c := s[i]
        switch {
        case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '_':
            out = append(out, c)
        default:
            out = append(out, '_')
        }
    }
    return string(out)
}

func TestLocalAEADProvider_WrapUnwrap_Roundtrip(t *testing.T) {
    // INV-30: Wrap then Unwrap recovers the original DEK byte-for-byte.
    ctx := context.Background()
    kekBytes := newKEKBytes(t)
    provider, err := kek.NewLocalAEADProviderForUnitTest(ctx, envSourceWith(t, kekBytes))
    require.NoError(t, err)

    dek := newKEKBytes(t) // any 32 bytes
    wrapped, kekKeyID, err := provider.Wrap(ctx, dek)
    require.NoError(t, err)
    require.NotEmpty(t, kekKeyID)
    require.NotEqual(t, dek, wrapped)

    unwrapped, err := provider.Unwrap(ctx, wrapped, kekKeyID)
    require.NoError(t, err)
    assert.Equal(t, dek, unwrapped)
}

func TestLocalAEADProvider_Unwrap_TamperedWrappedBytes_Fails(t *testing.T) {
    ctx := context.Background()
    kekBytes := newKEKBytes(t)
    provider, err := kek.NewLocalAEADProviderForUnitTest(ctx, envSourceWith(t, kekBytes))
    require.NoError(t, err)

    wrapped, kekKeyID, err := provider.Wrap(ctx, newKEKBytes(t))
    require.NoError(t, err)

    // Flip a bit in the ciphertext.
    wrapped[len(wrapped)/2] ^= 0xFF

    _, err = provider.Unwrap(ctx, wrapped, kekKeyID)
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "KEK_UNWRAP_AEAD_TAG_MISMATCH")
}

func TestLocalAEADProvider_Unwrap_WithUnknownKEKKeyID_Fails(t *testing.T) {
    ctx := context.Background()
    kekBytes := newKEKBytes(t)
    provider, err := kek.NewLocalAEADProviderForUnitTest(ctx, envSourceWith(t, kekBytes))
    require.NoError(t, err)

    wrapped, _, err := provider.Wrap(ctx, newKEKBytes(t))
    require.NoError(t, err)

    _, err = provider.Unwrap(ctx, wrapped, "totally-different-kek-id")
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "KEK_UNWRAP_KEY_ID_UNKNOWN")
}

func TestLocalAEADProvider_Name_DerivesFromSource(t *testing.T) {
    ctx := context.Background()
    provider, err := kek.NewLocalAEADProviderForUnitTest(ctx, envSourceWith(t, newKEKBytes(t)))
    require.NoError(t, err)
    assert.Equal(t, "local-aead/env", provider.Name())
}

func TestLocalAEADProvider_HealthCheck_Succeeds(t *testing.T) {
    ctx := context.Background()
    provider, err := kek.NewLocalAEADProviderForUnitTest(ctx, envSourceWith(t, newKEKBytes(t)))
    require.NoError(t, err)
    assert.NoError(t, provider.HealthCheck(ctx))
}
```

```go
// internal/eventbus/crypto/kek/local_aead_integration_test.go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package kek_test

import (
    "context"
    "crypto/rand"
    "encoding/hex"
    "testing"

    "github.com/jackc/pgx/v5"
    "github.com/stretchr/testify/require"
    "github.com/testcontainers/testcontainers-go"
    "github.com/testcontainers/testcontainers-go/modules/postgres"
    "github.com/testcontainers/testcontainers-go/wait"

    "github.com/holomush/holomush/internal/eventbus/crypto/kek"
    "github.com/holomush/holomush/internal/store"
    "github.com/holomush/holomush/pkg/errutil"
)

// TestLocalAEADProvider_Startup_RefusesIfWrapKeyIDUnknown verifies INV-33:
// startup integrity check fails if any crypto_keys row references a
// wrap_key_id the current provider cannot unwrap.
func TestLocalAEADProvider_Startup_RefusesIfWrapKeyIDUnknown(t *testing.T) {
    ctx := context.Background()
    pgContainer, err := postgres.Run(ctx,
        "postgres:18-alpine",
        postgres.WithDatabase("test"),
        postgres.WithUsername("test"),
        postgres.WithPassword("test"),
        testcontainers.WithWaitStrategy(
            wait.ForLog("database system is ready to accept connections").
                WithOccurrence(2)),
    )
    require.NoError(t, err)
    defer pgContainer.Terminate(ctx)

    connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
    require.NoError(t, err)
    migrator, err := store.NewMigrator(connStr)
    require.NoError(t, err)
    defer migrator.Close()
    require.NoError(t, migrator.Up())

    pool, err := pgx.Connect(ctx, connStr)
    require.NoError(t, err)
    defer pool.Close(ctx)

    // Insert a row with a wrap_key_id from a previous (now unknown) KEK.
    _, err = pool.Exec(ctx, `
        INSERT INTO crypto_keys
            (context_type, context_id, version, wrapped_dek, wrap_provider, wrap_key_id, participants)
        VALUES ('scene', 'orphan', 1, '\x00', 'local-aead/env', 'orphan-fingerprint', '[]')
    `)
    require.NoError(t, err)

    // Construct a provider with a fresh KEK — its fingerprint will not
    // match 'orphan-fingerprint'.
    kekBytes := make([]byte, kek.KEKByteLength)
    _, err = rand.Read(kekBytes)
    require.NoError(t, err)
    t.Setenv("HOLOMUSH_INV33_KEK", hex.EncodeToString(kekBytes))

    src := kek.NewEnvSource("HOLOMUSH_INV33_KEK", false)
    _, err = kek.NewLocalAEADProvider(ctx, src, pool)
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "KEK_PROVIDER_CANNOT_UNWRAP_EXISTING_DEKS")
}
```

- [ ] **Step 2: Run unit tests to verify failure**

Run: `task test -- -run "TestLocalAEADProvider_" ./internal/eventbus/crypto/kek/...`

Expected: FAIL with `undefined: kek.NewLocalAEADProvider`.

- [ ] **Step 3: Write the `LocalAEADProvider` implementation**

```go
// internal/eventbus/crypto/kek/local_aead.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package kek

import (
    "context"
    "crypto/rand"
    "crypto/sha256"
    "encoding/hex"
    "io"

    "github.com/jackc/pgx/v5"
    "github.com/samber/oops"
    "golang.org/x/crypto/chacha20poly1305"
)

// LocalAEADProvider does Wrap/Unwrap locally using a master KEK
// fetched from a pluggable KEKSource. The KEK lives in process memory
// for the lifetime of the provider; loadable on construction and on
// RotateKEK.
//
// kekKeyID is sha256(KEK) as 64 hex chars — a deterministic
// fingerprint of the KEK material. Stable across restarts as long as
// the KEK material does not change.
type LocalAEADProvider struct {
    source          KEKSource
    sourceName      string
    currentKEKKeyID string
    // kekByID maps fingerprint → KEK bytes. After RotateKEK the old
    // KEK is retained for the lifetime of the rotation operation;
    // Phase 2 doesn't ship rotation, so this map has at most one
    // entry. Phase 4+ may grow it.
    kekByID map[string][]byte
}

// NewLocalAEADProvider constructs a LocalAEADProvider, loading the KEK
// from the source and running INV-33 against db (refuses startup if
// any crypto_keys row references a wrap_key_id this provider cannot
// unwrap). Pass a *pgx.Conn or *pgxpool.Pool for db.
func NewLocalAEADProvider(ctx context.Context, source KEKSource, db PGQuerier) (*LocalAEADProvider, error) {
    p, err := buildLocalAEADProvider(ctx, source)
    if err != nil {
        return nil, err
    }
    if err := p.startupIntegrityCheck(ctx, db); err != nil {
        return nil, err
    }
    return p, nil
}

// NewLocalAEADProviderForUnitTest constructs a LocalAEADProvider
// without the INV-33 DB check. For unit tests of Wrap/Unwrap;
// integration tests use NewLocalAEADProvider.
func NewLocalAEADProviderForUnitTest(ctx context.Context, source KEKSource) (*LocalAEADProvider, error) {
    return buildLocalAEADProvider(ctx, source)
}

func buildLocalAEADProvider(ctx context.Context, source KEKSource) (*LocalAEADProvider, error) {
    kekBytes, err := source.Load(ctx)
    if err != nil {
        return nil, oops.Code("KEK_SOURCE_LOAD_FAILED").
            With("source", source.Name()).
            Wrap(err)
    }
    if len(kekBytes) != KEKByteLength {
        return nil, oops.Code("KEK_BYTE_LENGTH_INVALID").
            With("source", source.Name()).
            With("expected", KEKByteLength).
            With("got", len(kekBytes)).
            Errorf("KEK from %s must be %d bytes; got %d", source.Name(), KEKByteLength, len(kekBytes))
    }
    fingerprint := fingerprintKEK(kekBytes)
    return &LocalAEADProvider{
        source:          source,
        sourceName:      source.Name(),
        currentKEKKeyID: fingerprint,
        kekByID:         map[string][]byte{fingerprint: kekBytes},
    }, nil
}

// Name returns the source's name (e.g., "local-aead/env").
func (p *LocalAEADProvider) Name() string { return p.sourceName }

// Wrap encrypts dek under the current KEK using XChaCha20-Poly1305.
// kekKeyID is the current KEK fingerprint.
func (p *LocalAEADProvider) Wrap(ctx context.Context, dek []byte) ([]byte, string, error) {
    aead, err := chacha20poly1305.NewX(p.kekByID[p.currentKEKKeyID])
    if err != nil {
        return nil, "", oops.Code("KEK_AEAD_CONSTRUCT_FAILED").Wrap(err)
    }
    nonce := make([]byte, aead.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return nil, "", oops.Code("KEK_WRAP_RNG_FAILED").Wrap(err)
    }
    sealed := aead.Seal(nil, nonce, dek, nil)
    // Wrapped layout: nonce || sealed (sealed includes the AEAD tag).
    wrapped := make([]byte, 0, len(nonce)+len(sealed))
    wrapped = append(wrapped, nonce...)
    wrapped = append(wrapped, sealed...)
    return wrapped, p.currentKEKKeyID, nil
}

// Unwrap decrypts wrapped using the KEK identified by kekKeyID.
func (p *LocalAEADProvider) Unwrap(ctx context.Context, wrapped []byte, kekKeyID string) ([]byte, error) {
    kekBytes, ok := p.kekByID[kekKeyID]
    if !ok {
        return nil, oops.Code("KEK_UNWRAP_KEY_ID_UNKNOWN").
            With("kek_key_id", kekKeyID).
            With("source", p.sourceName).
            Errorf("provider does not hold KEK with fingerprint %q", kekKeyID)
    }
    aead, err := chacha20poly1305.NewX(kekBytes)
    if err != nil {
        return nil, oops.Code("KEK_AEAD_CONSTRUCT_FAILED").Wrap(err)
    }
    if len(wrapped) < aead.NonceSize() {
        return nil, oops.Code("KEK_WRAPPED_TOO_SHORT").
            With("min_size", aead.NonceSize()).
            With("got_size", len(wrapped)).
            Errorf("wrapped DEK shorter than nonce size")
    }
    nonce := wrapped[:aead.NonceSize()]
    sealed := wrapped[aead.NonceSize():]
    dek, err := aead.Open(nil, nonce, sealed, nil)
    if err != nil {
        return nil, oops.Code("KEK_UNWRAP_AEAD_TAG_MISMATCH").
            With("kek_key_id", kekKeyID).
            Errorf("AEAD open failed — wrapped DEK tampered or wrong KEK")
    }
    return dek, nil
}

// RotateKEK is a Phase 4+ surface. Phase 2 ships a stub that returns
// an unimplemented error pointing at the Phase 4 epic.
func (p *LocalAEADProvider) RotateKEK(ctx context.Context) (string, error) {
    return "", oops.Code("KEK_ROTATE_NOT_IMPLEMENTED").
        With("tracking_bead", "holomush-fi0n").
        With("phase", 4).
        Errorf("LocalAEADProvider.RotateKEK lands in Phase 4 (epic holomush-fi0n)")
}

// HealthCheck returns nil — the KEK is in process memory.
func (p *LocalAEADProvider) HealthCheck(ctx context.Context) error { return nil }

// startupIntegrityCheck enforces INV-33: no crypto_keys row may
// reference a wrap_key_id this provider cannot unwrap.
func (p *LocalAEADProvider) startupIntegrityCheck(ctx context.Context, db PGQuerier) error {
    rowsRdr, err := queryRowsCompat(ctx, db, "SELECT DISTINCT wrap_key_id FROM crypto_keys WHERE wrap_provider = $1", p.sourceName)
    if err != nil {
        return oops.Code("KEK_PROVIDER_INTEGRITY_QUERY_FAILED").Wrap(err)
    }
    var unrecoverable []string
    for _, kid := range rowsRdr {
        if _, ok := p.kekByID[kid]; !ok {
            unrecoverable = append(unrecoverable, kid)
        }
    }
    if len(unrecoverable) > 0 {
        return oops.Code("KEK_PROVIDER_CANNOT_UNWRAP_EXISTING_DEKS").
            With("source", p.sourceName).
            With("unrecoverable_kek_key_ids", unrecoverable).
            Errorf("provider cannot unwrap %d existing crypto_keys rows; "+
                "the master KEK has changed since those rows were written. "+
                "Restore the original KEK or run `holomush crypto provider-migrate` (Phase 6).",
                len(unrecoverable))
    }
    return nil
}

// queryRowsCompat is a tiny shim that accepts our PGQuerier (which
// only knows QueryRow) plus a real *pgx.Conn / *pgxpool.Pool. We need
// row iteration here, so the compat layer falls back to the
// underlying pgx surface via type assertion. PGQuerier is widened in
// Task 9 if needed; for now, integration tests pass *pgx.Conn which
// satisfies a richer interface.
type pgQueryAll interface {
    Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func queryRowsCompat(ctx context.Context, db PGQuerier, sql string, args ...any) ([]string, error) {
    qa, ok := db.(pgQueryAll)
    if !ok {
        return nil, oops.Errorf("PGQuerier does not support Query (need *pgx.Conn or *pgxpool.Pool)")
    }
    rows, err := qa.Query(ctx, sql, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var out []string
    for rows.Next() {
        var s string
        if err := rows.Scan(&s); err != nil {
            return nil, err
        }
        out = append(out, s)
    }
    return out, rows.Err()
}

func fingerprintKEK(kekBytes []byte) string {
    sum := sha256.Sum256(kekBytes)
    return hex.EncodeToString(sum[:])
}
```

**Note on AEAD construction:** the implementation constructs a fresh
`chacha20poly1305.NewX` per call rather than caching it on the struct.
AEAD construction is just key-schedule (cheap); caching adds complexity
without measurable benefit. After RotateKEK lands in Phase 4 we MAY
revisit if profiling shows hot-path cost.

- [ ] **Step 4: Run unit tests to verify pass**

Run: `task test -- -run "TestLocalAEADProvider_" ./internal/eventbus/crypto/kek/...`

Expected: PASS — round-trip, tampered-bytes rejection, unknown-key-id rejection, name + health-check correctness.

- [ ] **Step 5: Run the integration test to verify INV-33**

Run: `task test:int -- -run TestLocalAEADProvider_Startup_RefusesIfWrapKeyIDUnknown ./internal/eventbus/crypto/kek/...`

Expected: PASS — orphan row triggers `KEK_PROVIDER_CANNOT_UNWRAP_EXISTING_DEKS`.

- [ ] **Step 6: Commit**

```text
feat(crypto): kek.LocalAEADProvider — XChaCha20-Poly1305 Wrap/Unwrap

Per master spec §5.2 Topology 2. Loads KEK from a pluggable KEKSource
once at construction; Wrap/Unwrap run locally via XChaCha20-Poly1305.
kekKeyID is sha256(KEK) hex (deterministic fingerprint stable across
restarts).

INV-30 (Wrap/Unwrap roundtrip), INV-33 (startup integrity check refuses
if any crypto_keys row references an unknown wrap_key_id), and tampering
rejection (KEK_UNWRAP_AEAD_TAG_MISMATCH) covered by unit + integration
tests.

RotateKEK is a Phase 4 stub — returns KEK_ROTATE_NOT_IMPLEMENTED with
tracking_bead=holomush-fi0n.

Refs: docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md §5.2, INV-30, INV-33
Refs: docs/superpowers/plans/2026-04-30-event-payload-crypto-phase2-substrate.md Task 7
Bead: holomush-8qri
```

---

## Task 8: `dek.Cache` (LRU + TTL)

**Files:**

- Create: `internal/eventbus/crypto/dek/cache.go`
- Create: `internal/eventbus/crypto/dek/cache_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/eventbus/crypto/dek/cache_test.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek_test

import (
    "testing"
    "time"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/eventbus/codec"
    "github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

func newCacheKey(keyID codec.KeyID, version uint32) dek.CacheKey {
    return dek.CacheKey{KeyID: keyID, Version: version}
}

func TestCache_PutGet_Roundtrip(t *testing.T) {
    cache := dek.NewCache(dek.CacheConfig{Capacity: 4, TTL: time.Minute})
    m := dek.NewMaterial([]byte("0123456789abcdef0123456789abcdef"))

    cache.Put(newCacheKey(1, 1), m)
    got, ok := cache.Get(newCacheKey(1, 1))
    require.True(t, ok)
    assert.Equal(t, m, got)
}

func TestCache_Get_MissReturnsFalse(t *testing.T) {
    cache := dek.NewCache(dek.CacheConfig{Capacity: 4, TTL: time.Minute})
    _, ok := cache.Get(newCacheKey(99, 1))
    assert.False(t, ok)
}

func TestCache_LRUEviction(t *testing.T) {
    cache := dek.NewCache(dek.CacheConfig{Capacity: 2, TTL: time.Minute})
    m1 := dek.NewMaterial([]byte("11111111111111111111111111111111"))
    m2 := dek.NewMaterial([]byte("22222222222222222222222222222222"))
    m3 := dek.NewMaterial([]byte("33333333333333333333333333333333"))

    cache.Put(newCacheKey(1, 1), m1)
    cache.Put(newCacheKey(2, 1), m2)
    // Touch key 1 so key 2 is the LRU.
    _, _ = cache.Get(newCacheKey(1, 1))
    cache.Put(newCacheKey(3, 1), m3)

    _, ok := cache.Get(newCacheKey(2, 1))
    assert.False(t, ok, "key 2 should have been evicted as LRU")
    _, ok = cache.Get(newCacheKey(1, 1))
    assert.True(t, ok, "key 1 should remain (recently used)")
    _, ok = cache.Get(newCacheKey(3, 1))
    assert.True(t, ok, "key 3 should remain (newly inserted)")
}

func TestCache_TTLExpiry(t *testing.T) {
    // Use a clock to test TTL deterministically; cache accepts a
    // clock function for testability.
    now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
    clock := func() time.Time { return now }
    cache := dek.NewCacheWithClock(dek.CacheConfig{Capacity: 4, TTL: 5 * time.Minute}, clock)
    m := dek.NewMaterial([]byte("0123456789abcdef0123456789abcdef"))

    cache.Put(newCacheKey(1, 1), m)
    _, ok := cache.Get(newCacheKey(1, 1))
    require.True(t, ok)

    // Advance past TTL.
    now = now.Add(6 * time.Minute)
    _, ok = cache.Get(newCacheKey(1, 1))
    assert.False(t, ok, "entry should have expired after TTL")
}

func TestCache_Invalidate_RemovesEntry(t *testing.T) {
    cache := dek.NewCache(dek.CacheConfig{Capacity: 4, TTL: time.Minute})
    m := dek.NewMaterial([]byte("0123456789abcdef0123456789abcdef"))
    cache.Put(newCacheKey(1, 1), m)

    cache.Invalidate(newCacheKey(1, 1))
    _, ok := cache.Get(newCacheKey(1, 1))
    assert.False(t, ok)
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `task test -- -run "TestCache_" ./internal/eventbus/crypto/dek/...`

Expected: FAIL with `undefined: dek.NewCache`.

- [ ] **Step 3: Write the cache implementation**

```go
// internal/eventbus/crypto/dek/cache.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek

import (
    "container/list"
    "sync"
    "time"

    "github.com/holomush/holomush/internal/eventbus/codec"
)

// CacheKey identifies an entry by KeyID + version. Per master spec §5.8,
// the cache is version-aware so rotation works correctly: after Rotate,
// the old version stays cacheable for in-flight reads while new emits
// hit the new version.
type CacheKey struct {
    KeyID   codec.KeyID
    Version uint32
}

// CacheConfig parameterizes the cache. Defaults per master spec §5.8:
// capacity=1024, ttl=5m. Phase 2 ships these defaults but tests pass
// smaller capacity for LRU eviction and shorter TTL for expiry tests.
type CacheConfig struct {
    Capacity int
    TTL      time.Duration
}

// Cache holds unwrapped DEK Material in process memory with LRU
// eviction and TTL safety net. INV-27: MUST NOT live in NATS KV, PG,
// disk, or logs.
//
// The cache is internally synchronized for concurrent use. Phase 2's
// callers (DEKManager.GetOrCreate / Resolve) are concurrent.
type Cache struct {
    cap   int
    ttl   time.Duration
    clock func() time.Time

    mu     sync.Mutex
    list   *list.List
    byKey  map[CacheKey]*list.Element
}

type cacheEntry struct {
    key      CacheKey
    material *Material
    expiresAt time.Time
}

// NewCache constructs a cache using time.Now as the clock.
func NewCache(cfg CacheConfig) *Cache {
    return NewCacheWithClock(cfg, time.Now)
}

// NewCacheWithClock allows tests to inject a deterministic clock.
func NewCacheWithClock(cfg CacheConfig, clock func() time.Time) *Cache {
    return &Cache{
        cap:   cfg.Capacity,
        ttl:   cfg.TTL,
        clock: clock,
        list:  list.New(),
        byKey: make(map[CacheKey]*list.Element, cfg.Capacity),
    }
}

// Get returns the Material for key. Returns false on miss or
// TTL-expired entry.
func (c *Cache) Get(key CacheKey) (*Material, bool) {
    c.mu.Lock()
    defer c.mu.Unlock()

    elem, ok := c.byKey[key]
    if !ok {
        return nil, false
    }
    entry := elem.Value.(*cacheEntry)
    if c.clock().After(entry.expiresAt) {
        // Expired: remove and return miss.
        c.list.Remove(elem)
        delete(c.byKey, key)
        return nil, false
    }
    // LRU touch.
    c.list.MoveToFront(elem)
    return entry.material, true
}

// Put inserts or updates an entry. Evicts the LRU entry if over
// capacity.
func (c *Cache) Put(key CacheKey, material *Material) {
    c.mu.Lock()
    defer c.mu.Unlock()

    if elem, ok := c.byKey[key]; ok {
        // Update in place.
        entry := elem.Value.(*cacheEntry)
        entry.material = material
        entry.expiresAt = c.clock().Add(c.ttl)
        c.list.MoveToFront(elem)
        return
    }

    entry := &cacheEntry{key: key, material: material, expiresAt: c.clock().Add(c.ttl)}
    elem := c.list.PushFront(entry)
    c.byKey[key] = elem

    if c.list.Len() > c.cap {
        // Evict LRU.
        oldest := c.list.Back()
        if oldest != nil {
            c.list.Remove(oldest)
            delete(c.byKey, oldest.Value.(*cacheEntry).key)
        }
    }
}

// Invalidate removes an entry. Used by Phase 4+ for cross-replica
// invalidation (Phase 2 only exposes the local-side primitive).
func (c *Cache) Invalidate(key CacheKey) {
    c.mu.Lock()
    defer c.mu.Unlock()
    if elem, ok := c.byKey[key]; ok {
        c.list.Remove(elem)
        delete(c.byKey, key)
    }
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `task test -- -run "TestCache_" ./internal/eventbus/crypto/dek/...`

Expected: PASS — all five tests succeed (Put/Get roundtrip, miss-returns-false, LRU eviction, TTL expiry, Invalidate).

- [ ] **Step 5: Commit**

```text
feat(crypto): dek.Cache — LRU+TTL in-process DEK cache

Per master spec §5.8. Capacity-bounded LRU with TTL safety net; in-process
memory only (INV-27). Cross-replica invalidation protocol (INV-28, INV-29)
is Phase 4 — Phase 2 exposes only the local Invalidate primitive.

Internally synchronized for concurrent callers. NewCacheWithClock
accepts an injectable clock for deterministic TTL tests.

Refs: docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md §5.8, INV-27
Refs: docs/superpowers/plans/2026-04-30-event-payload-crypto-phase2-substrate.md Task 8
Bead: holomush-8qri
```

---

## Task 9: `dek.Manager` skeleton (`GetOrCreate`, `Resolve`, stubbed `Add`/`Rotate`/`Rekey`)

**Files:**

- Create: `internal/eventbus/crypto/dek/store.go`
- Create: `internal/eventbus/crypto/dek/store_test.go`
- Create: `internal/eventbus/crypto/dek/manager.go`
- Create: `internal/eventbus/crypto/dek/manager_test.go`
- Create: `internal/eventbus/crypto/dek/manager_integration_test.go`
- Modify: `internal/eventbus/crypto/dek/api_test.go` (add the stub-bead allow-set check)

- [ ] **Step 1: Write failing unit tests for `Manager` stubs and the allow-set static check**

Edit `internal/eventbus/crypto/dek/api_test.go` from Task 4. **Merge** the
following imports into the existing single import block (Go requires one
import block per file — do NOT add a second `import (...)`):

- Add: `"context"`
- Add: `"github.com/samber/oops"`
- Add: `"github.com/holomush/holomush/internal/eventbus/crypto/dek"`

Then append the new test function below the existing
`TestPackageHasNoExportedByteSlices`:

```go
// (Imports merged into the existing block — see instruction above.)

// stubAllowSet enumerates the bead IDs Phase 2 stubs MAY reference.
// Renaming or closing either bead without updating this list fails CI
// at task lint:test time, surfacing the rot before the stub error
// reaches a production log.
var stubAllowSet = map[string]struct{}{
    "holomush-fi0n": {}, // Phase 4: Add + Rotate lifecycle ops
    "holomush-jxo8": {}, // Phase 5: Rekey + AdminReadStream + OperatorAuth
}

func TestManagerStubsCarryTrackingBeadFromAllowSet(t *testing.T) {
    // Build a Manager skeleton and probe each stub. The Manager
    // constructor (NewManager) is independent of provider correctness;
    // tests inject a stub provider via NewManagerForUnitTest.
    m := dek.NewManagerForUnitTest()

    cases := []struct {
        name       string
        invoke     func() error
        wantBead   string
        wantPhase  int
        wantCode   string
    }{
        {
            name:      "Add",
            invoke:    func() error { return m.Add(context.Background(), dek.ContextID{Type: "scene", ID: "x"}, dek.Participant{}) },
            wantBead:  "holomush-fi0n",
            wantPhase: 4,
            wantCode:  "DEK_ADD_NOT_IMPLEMENTED",
        },
        {
            name:      "Rotate",
            invoke:    func() error { return m.Rotate(context.Background(), dek.ContextID{Type: "scene", ID: "x"}, nil, "test") },
            wantBead:  "holomush-fi0n",
            wantPhase: 4,
            wantCode:  "DEK_ROTATE_NOT_IMPLEMENTED",
        },
        {
            name:      "Rekey",
            invoke:    func() error { return m.Rekey(context.Background(), dek.ContextID{Type: "scene", ID: "x"}, "test", dek.OperatorFactors{}) },
            wantBead:  "holomush-jxo8",
            wantPhase: 5,
            wantCode:  "DEK_REKEY_NOT_IMPLEMENTED",
        },
    }

    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            err := tc.invoke()
            require.Error(t, err)

            oopsErr, ok := oops.AsOops(err)
            require.True(t, ok, "stub error must be an oops error")

            // Code matches.
            require.Equal(t, tc.wantCode, oopsErr.Code())

            // tracking_bead present + matches expected.
            ctx := oopsErr.Context()
            require.Contains(t, ctx, "tracking_bead")
            require.Equal(t, tc.wantBead, ctx["tracking_bead"])

            // tracking_bead value is in the allow-set.
            _, allowed := stubAllowSet[ctx["tracking_bead"].(string)]
            require.True(t, allowed,
                "tracking_bead %q is not in stubAllowSet — update stubAllowSet "+
                    "in api_test.go or fix the stub", ctx["tracking_bead"])

            // phase present + matches expected.
            require.Contains(t, ctx, "phase")
            require.Equal(t, tc.wantPhase, ctx["phase"])

            // tracking_bead value matches the holomush-<id> regex shape.
            require.Regexp(t, `^holomush-[a-z0-9]+$`, ctx["tracking_bead"])
        })
    }
}
```

- [ ] **Step 2: Write failing integration tests for `GetOrCreate` and `Resolve`**

```go
// internal/eventbus/crypto/dek/manager_integration_test.go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek_test

import (
    "context"
    "crypto/rand"
    "encoding/hex"
    "sync"
    "testing"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "github.com/testcontainers/testcontainers-go"
    "github.com/testcontainers/testcontainers-go/modules/postgres"
    "github.com/testcontainers/testcontainers-go/wait"

    "github.com/holomush/holomush/internal/eventbus/codec"
    "github.com/holomush/holomush/internal/eventbus/crypto/dek"
    "github.com/holomush/holomush/internal/eventbus/crypto/kek"
    "github.com/holomush/holomush/internal/store"
    "github.com/holomush/holomush/pkg/errutil"
)

func newTestPGPool(t *testing.T) (string, func()) {
    t.Helper()
    ctx := context.Background()
    pgContainer, err := postgres.Run(ctx,
        "postgres:18-alpine",
        postgres.WithDatabase("test"),
        postgres.WithUsername("test"),
        postgres.WithPassword("test"),
        testcontainers.WithWaitStrategy(
            wait.ForLog("database system is ready to accept connections").
                WithOccurrence(2)),
    )
    require.NoError(t, err)
    connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
    require.NoError(t, err)
    migrator, err := store.NewMigrator(connStr)
    require.NoError(t, err)
    require.NoError(t, migrator.Up())
    migrator.Close()
    return connStr, func() { pgContainer.Terminate(ctx) }
}

func newTestProvider(t *testing.T) kek.Provider {
    t.Helper()
    kekBytes := make([]byte, kek.KEKByteLength)
    _, err := rand.Read(kekBytes)
    require.NoError(t, err)
    name := "TEST_KEK_" + sanitizeEnvName(t.Name())
    t.Setenv(name, hex.EncodeToString(kekBytes))
    src := kek.NewEnvSource(name, false)
    p, err := kek.NewLocalAEADProviderForUnitTest(context.Background(), src)
    require.NoError(t, err)
    return p
}

// sanitizeEnvName strips characters that env var names disallow.
func sanitizeEnvName(s string) string {
    out := make([]byte, 0, len(s))
    for i := 0; i < len(s); i++ {
        c := s[i]
        switch {
        case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '_':
            out = append(out, c)
        default:
            out = append(out, '_')
        }
    }
    return string(out)
}

func TestManager_GetOrCreate_MintsAndPersists(t *testing.T) {
    ctx := context.Background()
    connStr, teardown := newTestPGPool(t)
    defer teardown()
    pool, err := pgxpool.New(ctx, connStr)
    require.NoError(t, err)
    defer pool.Close()

    provider := newTestProvider(t)
    cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
    mgr := dek.NewManager(provider, dek.NewStore(pool), cache)

    ctxID := dek.ContextID{Type: "scene", ID: "01ABCDEF"}
    key1, err := mgr.GetOrCreate(ctx, ctxID, []dek.Participant{})
    require.NoError(t, err)
    assert.NotZero(t, key1.ID)
    assert.Len(t, key1.Bytes, 32)

    // A second call returns the same key (idempotent for the same context).
    key2, err := mgr.GetOrCreate(ctx, ctxID, []dek.Participant{})
    require.NoError(t, err)
    assert.Equal(t, key1.ID, key2.ID)
    assert.Equal(t, key1.Bytes, key2.Bytes)

    // The crypto_keys table has exactly one row for this context.
    var rowCount int
    err = pool.QueryRow(ctx,
        "SELECT count(*) FROM crypto_keys WHERE context_type=$1 AND context_id=$2",
        "scene", "01ABCDEF").Scan(&rowCount)
    require.NoError(t, err)
    assert.Equal(t, 1, rowCount)
}

func TestManager_Resolve_ByKeyIDAndVersion(t *testing.T) {
    ctx := context.Background()
    connStr, teardown := newTestPGPool(t)
    defer teardown()
    pool, err := pgxpool.New(ctx, connStr)
    require.NoError(t, err)
    defer pool.Close()

    provider := newTestProvider(t)
    cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
    mgr := dek.NewManager(provider, dek.NewStore(pool), cache)

    ctxID := dek.ContextID{Type: "dm", ID: "01ABCDEF-01FFFFFF"}
    key, err := mgr.GetOrCreate(ctx, ctxID, []dek.Participant{})
    require.NoError(t, err)

    // Drop the cache so Resolve has to go through DB.
    cache.Invalidate(dek.CacheKey{KeyID: key.ID, Version: 1})

    resolved, err := mgr.Resolve(ctx, key.ID, 1)
    require.NoError(t, err)
    assert.Equal(t, key.ID, resolved.ID)
    assert.Equal(t, key.Bytes, resolved.Bytes)
}

func TestManager_Resolve_NotFound_ReturnsErrDEKNotFound(t *testing.T) {
    ctx := context.Background()
    connStr, teardown := newTestPGPool(t)
    defer teardown()
    pool, err := pgxpool.New(ctx, connStr)
    require.NoError(t, err)
    defer pool.Close()

    mgr := dek.NewManager(newTestProvider(t), dek.NewStore(pool),
        dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}))

    _, err = mgr.Resolve(ctx, codec.KeyID(99999), 1)
    require.Error(t, err)
    errutil.AssertErrorCode(t, err, "DEK_NOT_FOUND")
}

func TestManager_GetOrCreate_ConcurrentMintRace(t *testing.T) {
    // Two goroutines call GetOrCreate(scene:X, ...) simultaneously.
    // One INSERT wins; the other raises unique-violation, re-SELECTs,
    // and returns the winner's row. Both callers see byte-equal Bytes.
    ctx := context.Background()
    connStr, teardown := newTestPGPool(t)
    defer teardown()
    pool, err := pgxpool.New(ctx, connStr)
    require.NoError(t, err)
    defer pool.Close()

    provider := newTestProvider(t)

    // Use two managers backed by separate caches to simulate two
    // replicas; they share the underlying DB.
    cacheA := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
    cacheB := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute})
    mgrA := dek.NewManager(provider, dek.NewStore(pool), cacheA)
    mgrB := dek.NewManager(provider, dek.NewStore(pool), cacheB)

    ctxID := dek.ContextID{Type: "scene", ID: "race-01"}

    var (
        wg     sync.WaitGroup
        keyA   codec.Key
        keyB   codec.Key
        errA   error
        errB   error
    )
    wg.Add(2)
    go func() {
        defer wg.Done()
        keyA, errA = mgrA.GetOrCreate(ctx, ctxID, []dek.Participant{})
    }()
    go func() {
        defer wg.Done()
        keyB, errB = mgrB.GetOrCreate(ctx, ctxID, []dek.Participant{})
    }()
    wg.Wait()

    require.NoError(t, errA)
    require.NoError(t, errB)
    assert.Equal(t, keyA.ID, keyB.ID, "both managers must converge on the same DEK row")
    assert.Equal(t, keyA.Bytes, keyB.Bytes, "both managers must see byte-equal DEK bytes")

    // Exactly one row exists.
    var rowCount int
    err = pool.QueryRow(ctx,
        "SELECT count(*) FROM crypto_keys WHERE context_type=$1 AND context_id=$2",
        "scene", "race-01").Scan(&rowCount)
    require.NoError(t, err)
    assert.Equal(t, 1, rowCount)
}
```

**Note on imports:** the integration test file imports
`github.com/jackc/pgx/v5/pgxpool` (used via `pgxpool.New`) but not
`github.com/jackc/pgx/v5` directly — drop the `pgx` import from the
import block above if the linter flags it as unused.

- [ ] **Step 3: Run failing tests to verify**

Run:

```text
task test -- -run "TestManagerStubsCarryTrackingBead" ./internal/eventbus/crypto/dek/...
task test:int -- -run "TestManager_" ./internal/eventbus/crypto/dek/...
```

Expected: FAIL — `undefined: dek.NewManager`, `dek.NewStore`, `dek.ContextID`, `dek.Participant`, `dek.OperatorFactors`.

- [ ] **Step 4: Write the `Store` (pgx persistence) layer**

```go
// internal/eventbus/crypto/dek/store.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek

import (
    "context"
    "encoding/json"
    "errors"
    "time"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgconn"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/samber/oops"

    "github.com/holomush/holomush/internal/eventbus/codec"
)

// ContextID names a DEK's social unit (scene, DM, channel, character,
// player). Per master spec §5.1.
type ContextID struct {
    Type string
    ID   string
}

// Participant is a member of a DEK's participant set. JSONB-encoded
// in crypto_keys.participants.
type Participant struct {
    PlayerID    string    `json:"player_id"`
    CharacterID string    `json:"character_id"`
    BindingID   string    `json:"binding_id"`
    JoinedAt    time.Time `json:"joined_at"`
    AddedVia    string    `json:"added_via,omitempty"`
}

// OperatorFactors captures the operator's identity for Rekey audit.
// Phase 2 declares the type but only Rekey (Phase 5 stub) consumes it.
type OperatorFactors struct {
    OSUser                       string
    PlayerID                     string
    TOTPVerified                 bool
    AuthProviderName             string
    ProviderSpecificID           string
    DualControlPartnerPlayerID   string
}

// row mirrors a crypto_keys row. Internal to dek/.
type row struct {
    ID            int64
    ContextType   string
    ContextID     string
    Version       uint32
    WrappedDEK    []byte
    WrapProvider  string
    WrapKeyID     string
    Participants  []Participant
    CreatedAt     time.Time
    RotatedAt     *time.Time
}

// Store persists wrapped DEKs in the crypto_keys table. The provider
// (KEK wrap/unwrap) is owned by Manager; Store is purely the SQL layer.
type Store struct {
    pool *pgxpool.Pool
}

// NewStore wraps a pgxpool.Pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// SelectActive returns the active (rotated_at IS NULL) row for ctxID,
// or pgx.ErrNoRows if none exists.
func (s *Store) SelectActive(ctx context.Context, ctxID ContextID) (row, error) {
    var r row
    var participantsJSON []byte
    err := s.pool.QueryRow(ctx, `
        SELECT id, context_type, context_id, version, wrapped_dek,
               wrap_provider, wrap_key_id, participants, created_at, rotated_at
          FROM crypto_keys
         WHERE context_type=$1 AND context_id=$2 AND rotated_at IS NULL
         ORDER BY version DESC
         LIMIT 1
    `, ctxID.Type, ctxID.ID).Scan(
        &r.ID, &r.ContextType, &r.ContextID, &r.Version, &r.WrappedDEK,
        &r.WrapProvider, &r.WrapKeyID, &participantsJSON, &r.CreatedAt, &r.RotatedAt,
    )
    if err != nil {
        return row{}, err
    }
    if err := json.Unmarshal(participantsJSON, &r.Participants); err != nil {
        return row{}, oops.Code("DEK_PARTICIPANTS_UNMARSHAL_FAILED").Wrap(err)
    }
    return r, nil
}

// SelectByID returns the row for keyID + version.
func (s *Store) SelectByID(ctx context.Context, keyID codec.KeyID, version uint32) (row, error) {
    var r row
    var participantsJSON []byte
    err := s.pool.QueryRow(ctx, `
        SELECT id, context_type, context_id, version, wrapped_dek,
               wrap_provider, wrap_key_id, participants, created_at, rotated_at
          FROM crypto_keys
         WHERE id=$1 AND version=$2
    `, int64(keyID), version).Scan(
        &r.ID, &r.ContextType, &r.ContextID, &r.Version, &r.WrappedDEK,
        &r.WrapProvider, &r.WrapKeyID, &participantsJSON, &r.CreatedAt, &r.RotatedAt,
    )
    if err != nil {
        return row{}, err
    }
    if err := json.Unmarshal(participantsJSON, &r.Participants); err != nil {
        return row{}, oops.Code("DEK_PARTICIPANTS_UNMARSHAL_FAILED").Wrap(err)
    }
    return r, nil
}

// Insert writes a fresh row. Returns the assigned id and the
// pg unique-violation sentinel if the row already exists (caller
// re-runs SelectActive).
func (s *Store) Insert(ctx context.Context, in row) (int64, error) {
    pj, err := json.Marshal(in.Participants)
    if err != nil {
        return 0, oops.Code("DEK_PARTICIPANTS_MARSHAL_FAILED").Wrap(err)
    }
    var id int64
    err = s.pool.QueryRow(ctx, `
        INSERT INTO crypto_keys
            (context_type, context_id, version, wrapped_dek,
             wrap_provider, wrap_key_id, participants)
        VALUES ($1,$2,$3,$4,$5,$6,$7)
        RETURNING id
    `, in.ContextType, in.ContextID, in.Version, in.WrappedDEK,
        in.WrapProvider, in.WrapKeyID, pj).Scan(&id)
    return id, err
}

// IsUniqueViolation returns true if err is a PG unique-constraint
// violation (used by Manager.GetOrCreate to detect concurrent INSERT
// races).
func IsUniqueViolation(err error) bool {
    var pgErr *pgconn.PgError
    if errors.As(err, &pgErr) {
        return pgErr.Code == "23505"
    }
    return false
}

// SelectAllParticipants returns every row's participants list — used
// only by Phase 4+ lifecycle ops; declared here for forward stability.
func (s *Store) SelectAllParticipants(ctx context.Context, ctxID ContextID) ([][]Participant, error) {
    rows, err := s.pool.Query(ctx, `
        SELECT participants FROM crypto_keys
         WHERE context_type=$1 AND context_id=$2
    `, ctxID.Type, ctxID.ID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var out [][]Participant
    for rows.Next() {
        var pj []byte
        if err := rows.Scan(&pj); err != nil {
            return nil, err
        }
        var ps []Participant
        if err := json.Unmarshal(pj, &ps); err != nil {
            return nil, err
        }
        out = append(out, ps)
    }
    return out, rows.Err()
}
```

- [ ] **Step 5: Write the `Manager` implementation**

```go
// internal/eventbus/crypto/dek/manager.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek

import (
    "context"
    "crypto/rand"
    "errors"
    "io"

    "github.com/jackc/pgx/v5"
    "github.com/samber/oops"

    "github.com/holomush/holomush/internal/eventbus/codec"
    "github.com/holomush/holomush/internal/eventbus/crypto/kek"
)

// DEKByteLength matches kek.KEKByteLength: chacha20poly1305 key size.
const DEKByteLength = 32

// Manager owns DEK lifecycle. Phase 2 ships a skeleton: GetOrCreate
// and Resolve are real; Add, Rotate, Rekey return tracking-bead-tagged
// stubs (Phase 4 + Phase 5).
type Manager interface {
    GetOrCreate(ctx context.Context, ctxID ContextID, initial []Participant) (codec.Key, error)
    Resolve(ctx context.Context, keyID codec.KeyID, version uint32) (codec.Key, error)

    // Phase 4 stub — see holomush-fi0n.
    Add(ctx context.Context, ctxID ContextID, p Participant) error
    Rotate(ctx context.Context, ctxID ContextID, newParticipants []Participant, reason string) error

    // Phase 5 stub — see holomush-jxo8.
    Rekey(ctx context.Context, ctxID ContextID, justification string, ops OperatorFactors) error
}

// manager is the concrete impl.
type manager struct {
    provider kek.Provider
    store    *Store
    cache    *Cache
}

// NewManager constructs a real Manager. Production callers (Phase 3+)
// pass a real KEK provider and pgxpool.Pool-backed Store.
func NewManager(provider kek.Provider, store *Store, cache *Cache) Manager {
    return &manager{provider: provider, store: store, cache: cache}
}

// NewManagerForUnitTest constructs a Manager with no DB or KEK access.
// GetOrCreate/Resolve will fail at runtime; only the stub methods
// (Add/Rotate/Rekey) are exercisable. Used by api_test.go for
// stub-bead allow-set checking.
func NewManagerForUnitTest() Manager {
    return &manager{}
}

// GetOrCreate returns the active DEK for ctxID, minting v1 if no row
// exists. On concurrent INSERT race, the loser re-SELECTs and uses
// the winner's row (PG unique constraint guarantees one winner).
func (m *manager) GetOrCreate(ctx context.Context, ctxID ContextID, initial []Participant) (codec.Key, error) {
    // Try the active row first.
    if row, err := m.store.SelectActive(ctx, ctxID); err == nil {
        return m.unwrapAndCache(ctx, row)
    } else if !errors.Is(err, pgx.ErrNoRows) {
        return codec.Key{}, oops.Code("DEK_STORE_SELECT_FAILED").Wrap(err)
    }

    // Mint a fresh DEK and INSERT.
    dekBytes := make([]byte, DEKByteLength)
    if _, err := io.ReadFull(rand.Reader, dekBytes); err != nil {
        return codec.Key{}, oops.Code("DEK_RNG_FAILED").Wrap(err)
    }
    wrapped, kekKeyID, err := m.provider.Wrap(ctx, dekBytes)
    if err != nil {
        return codec.Key{}, oops.Code("DEK_WRAP_FAILED").Wrap(err)
    }
    in := row{
        ContextType:  ctxID.Type,
        ContextID:    ctxID.ID,
        Version:      1,
        WrappedDEK:   wrapped,
        WrapProvider: m.provider.Name(),
        WrapKeyID:    kekKeyID,
        Participants: initial,
    }
    id, err := m.store.Insert(ctx, in)
    if err != nil {
        if IsUniqueViolation(err) {
            // Race: someone else minted v1 first. Re-SELECT and use theirs.
            existing, selErr := m.store.SelectActive(ctx, ctxID)
            if selErr != nil {
                return codec.Key{}, oops.Code("DEK_STORE_SELECT_FAILED").Wrap(selErr)
            }
            return m.unwrapAndCache(ctx, existing)
        }
        return codec.Key{}, oops.Code("DEK_STORE_INSERT_FAILED").Wrap(err)
    }
    in.ID = id
    material := NewMaterial(dekBytes)
    m.cache.Put(CacheKey{KeyID: codec.KeyID(id), Version: 1}, material)
    return material.AsCodecKey(codec.KeyID(id)), nil
}

// Resolve returns the DEK for (keyID, version). Cache → DB → unwrap.
func (m *manager) Resolve(ctx context.Context, keyID codec.KeyID, version uint32) (codec.Key, error) {
    if material, ok := m.cache.Get(CacheKey{KeyID: keyID, Version: version}); ok {
        return material.AsCodecKey(keyID), nil
    }
    r, err := m.store.SelectByID(ctx, keyID, version)
    if err != nil {
        if errors.Is(err, pgx.ErrNoRows) {
            return codec.Key{}, oops.Code("DEK_NOT_FOUND").
                With("key_id", uint64(keyID)).
                With("version", version).
                Errorf("crypto_keys row %d v%d not found", keyID, version)
        }
        return codec.Key{}, oops.Code("DEK_STORE_SELECT_FAILED").Wrap(err)
    }
    return m.unwrapAndCache(ctx, r)
}

// Add lands in Phase 4 (epic holomush-fi0n).
func (m *manager) Add(ctx context.Context, ctxID ContextID, p Participant) error {
    return oops.Code("DEK_ADD_NOT_IMPLEMENTED").
        With("tracking_bead", "holomush-fi0n").
        With("phase", 4).
        Errorf("Manager.Add lands in Phase 4 (epic holomush-fi0n)")
}

// Rotate lands in Phase 4 (epic holomush-fi0n).
func (m *manager) Rotate(ctx context.Context, ctxID ContextID, newParticipants []Participant, reason string) error {
    return oops.Code("DEK_ROTATE_NOT_IMPLEMENTED").
        With("tracking_bead", "holomush-fi0n").
        With("phase", 4).
        Errorf("Manager.Rotate lands in Phase 4 (epic holomush-fi0n)")
}

// Rekey lands in Phase 5 (epic holomush-jxo8).
func (m *manager) Rekey(ctx context.Context, ctxID ContextID, justification string, ops OperatorFactors) error {
    return oops.Code("DEK_REKEY_NOT_IMPLEMENTED").
        With("tracking_bead", "holomush-jxo8").
        With("phase", 5).
        Errorf("Manager.Rekey lands in Phase 5 (epic holomush-jxo8)")
}

func (m *manager) unwrapAndCache(ctx context.Context, r row) (codec.Key, error) {
    dekBytes, err := m.provider.Unwrap(ctx, r.WrappedDEK, r.WrapKeyID)
    if err != nil {
        return codec.Key{}, oops.Code("DEK_UNWRAP_FAILED").
            With("key_id", r.ID).
            With("version", r.Version).
            Wrap(err)
    }
    material := NewMaterial(dekBytes)
    m.cache.Put(CacheKey{KeyID: codec.KeyID(r.ID), Version: r.Version}, material)
    return material.AsCodecKey(codec.KeyID(r.ID)), nil
}
```

- [ ] **Step 6: Run unit tests for stubs to verify pass**

Run: `task test -- -run "TestManagerStubsCarryTrackingBead|TestPackageHasNoExportedByteSlices|TestNewMaterial_|TestMaterial_" ./internal/eventbus/crypto/dek/...`

Expected: PASS — all stub methods carry `tracking_bead`, `phase`, and the regex/allow-set checks succeed; the static API surface test still passes (no new exports return `[]byte`).

- [ ] **Step 7: Run integration tests to verify pass**

Run: `task test:int -- -run "TestManager_" ./internal/eventbus/crypto/dek/...`

Expected: PASS — `GetOrCreate` mints + persists, second call is idempotent, `Resolve` round-trips, missing key returns `DEK_NOT_FOUND`, concurrent race converges on one row.

- [ ] **Step 8: Commit**

```text
feat(crypto): dek.Manager skeleton + Store

Manager interface fully declared; GetOrCreate/Resolve real
(encrypt-path + decrypt-path), Add/Rotate/Rekey stubbed with
tracking_bead/phase fields pointing at holomush-fi0n (Phase 4) and
holomush-jxo8 (Phase 5). Stub-bead allow-set is checked at test time
in api_test.go — closing or renaming either bead without updating the
allow-set fails CI.

GetOrCreate handles the concurrent INSERT race: on
unique-constraint violation, re-SELECT and use the winner's row.
Tested via TestManager_GetOrCreate_ConcurrentMintRace which spawns
two goroutines and asserts byte-equal Bytes + exactly one row.

Store is a pgxpool-backed persistence layer; participants are JSONB.
No foreign key from events_audit (master spec §4.7 rationale).

Refs: docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md §5.1
Refs: docs/superpowers/plans/2026-04-30-event-payload-crypto-phase2-substrate.md Task 9
Bead: holomush-8qri
```

---

## Task 10: Ruleguard rules for INV-27 enforcement

**Files:**

- Create: `gorules/dek_no_serialize.go`
- Create: `gorules/codec_key_bytes_allowlist.go`
- Create: `gorules/testdata/dek_no_serialize/sinks_test.go`
- Create: `gorules/testdata/codec_key_bytes/leak_test.go`

- [ ] **Step 1: Write the `dek_no_serialize` ruleguard rule**

```go
// gorules/dek_no_serialize.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build ruleguard
// +build ruleguard

package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// dekMaterialPath is the fully-qualified type path the matcher tests
// against. Updating this requires updating the dek package import path.
const dekMaterialPath = "github.com/holomush/holomush/internal/eventbus/crypto/dek.Material"

// DEKMaterialNoJSON forbids passing dek.Material to encoding/json.
// INV-27 sink-side enforcement.
func DEKMaterialNoJSON(m dsl.Matcher) {
    m.Match(`json.Marshal($x)`, `json.MarshalIndent($x, $_, $_)`).
        Where(m["x"].Type.Is(dekMaterialPath) || m["x"].Type.Is("*"+dekMaterialPath)).
        Report(`INV-27: dek.Material MUST NOT be passed to encoding/json. ` +
            `Material is opaque DEK material; serializing it leaks unwrapped bytes.`)
}

// DEKMaterialNoGob forbids passing dek.Material to encoding/gob.
func DEKMaterialNoGob(m dsl.Matcher) {
    m.Match(`gob.NewEncoder($_).Encode($x)`).
        Where(m["x"].Type.Is(dekMaterialPath) || m["x"].Type.Is("*"+dekMaterialPath)).
        Report(`INV-27: dek.Material MUST NOT be passed to encoding/gob.`)
}

// DEKMaterialNoProto forbids passing dek.Material to proto.Marshal.
func DEKMaterialNoProto(m dsl.Matcher) {
    m.Match(`proto.Marshal($x)`).
        Where(m["x"].Type.Is(dekMaterialPath) || m["x"].Type.Is("*"+dekMaterialPath)).
        Report(`INV-27: dek.Material MUST NOT be passed to google.golang.org/protobuf/proto.Marshal.`)
}

// DEKMaterialNoFmtFormatting forbids passing dek.Material to fmt
// formatting functions. Per master spec §"Note on variadic patterns"
// in the design notes, we enumerate one pattern per known sink.
func DEKMaterialNoFmtFormatting(m dsl.Matcher) {
    matches := []string{
        `fmt.Sprint($*xs)`,
        `fmt.Sprintf($_, $*xs)`,
        `fmt.Sprintln($*xs)`,
        `fmt.Print($*xs)`,
        `fmt.Printf($_, $*xs)`,
        `fmt.Println($*xs)`,
        `fmt.Fprint($_, $*xs)`,
        `fmt.Fprintf($_, $_, $*xs)`,
        `fmt.Fprintln($_, $*xs)`,
        `fmt.Errorf($_, $*xs)`,
    }
    for _, p := range matches {
        m.Match(p).
            Where(m["xs"].Type.Is(dekMaterialPath) || m["xs"].Type.Is("*"+dekMaterialPath)).
            Report(`INV-27: dek.Material MUST NOT be passed to fmt formatting/print functions. ` +
                `Material's GoString/Stringer-default would dump bytes; if you need to log a ` +
                `DEK reference, log codec.KeyID instead.`)
    }
}

// DEKMaterialNoLog forbids passing dek.Material to log/slog.
func DEKMaterialNoLog(m dsl.Matcher) {
    matches := []string{
        `log.Print($*xs)`,
        `log.Printf($_, $*xs)`,
        `log.Println($*xs)`,
        `log.Fatal($*xs)`,
        `log.Fatalf($_, $*xs)`,
        `log.Fatalln($*xs)`,
        `log.Panic($*xs)`,
        `log.Panicf($_, $*xs)`,
        `log.Panicln($*xs)`,
    }
    for _, p := range matches {
        m.Match(p).
            Where(m["xs"].Type.Is(dekMaterialPath) || m["xs"].Type.Is("*"+dekMaterialPath)).
            Report(`INV-27: dek.Material MUST NOT be passed to log functions.`)
    }
}

// DEKMaterialNoSlog forbids passing dek.Material to log/slog.
func DEKMaterialNoSlog(m dsl.Matcher) {
    matches := []string{
        `slog.Info($_, $*xs)`,
        `slog.Debug($_, $*xs)`,
        `slog.Warn($_, $*xs)`,
        `slog.Error($_, $*xs)`,
        `slog.Log($_, $_, $_, $*xs)`,
        `slog.Any($_, $x)`,
        `slog.Group($_, $*xs)`,
    }
    for _, p := range matches {
        m.Match(p).
            Where(m["xs"].Type.Is(dekMaterialPath) ||
                m["xs"].Type.Is("*"+dekMaterialPath) ||
                m["x"].Type.Is(dekMaterialPath) ||
                m["x"].Type.Is("*"+dekMaterialPath)).
            Report(`INV-27: dek.Material MUST NOT be passed to log/slog functions.`)
    }
}

// Note: arbitrary io.Writer-by-interface and concrete-writer.Write
// patterns are NOT covered by ruleguard. The Write methods on
// os.File, bytes.Buffer, etc., take []byte (not Material), so a
// type-filter pattern like `$_.Write($x)` with $x being Material
// would never match — Go's type system rejects the call before
// ruleguard sees it.
//
// The realistic Material-leak paths the rules above catch are
// reflection-based serializers (json/gob/proto) and stringer-based
// formatters (fmt/log/slog). Combined with the static API surface
// test (no exported []byte from the dek package), these defenses
// cover the practical exfiltration surface.
```

- [ ] **Step 2: Write the `codec_key_bytes_allowlist` ruleguard rule**

```go
// gorules/codec_key_bytes_allowlist.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build ruleguard
// +build ruleguard

package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// CodecKeyBytesAllowlist forbids reads of codec.Key.Bytes outside the
// allowed package set. Master-spec amendment per Phase 2 design notes:
// tightens the master spec's "Kept (semantics unchanged)" classification
// for codec.Key by restricting WHO may read the field.
//
// Allowed package paths:
//   - internal/eventbus/codec/...   (codec implementations)
//   - internal/eventbus/crypto/...  (substrate construction + tests)
//
// This is the residual-defense rule for INV-27. dek.Material is opaque
// (no exported []byte accessor), but its AsCodecKey returns a
// codec.Key whose Bytes field is publicly readable. This rule gates
// who may read it.
func CodecKeyBytesAllowlist(m dsl.Matcher) {
    const codecKey = "github.com/holomush/holomush/internal/eventbus/codec.Key"
    const allowed = `^github\.com/holomush/holomush/internal/eventbus/(codec|crypto)(/|$)`
    const msg = `INV-27 (residual defense): codec.Key.Bytes reads are restricted to ` +
        `internal/eventbus/codec/... and internal/eventbus/crypto/.... ` +
        `If you need raw DEK bytes, you are probably in the wrong package — route through ` +
        `dek.Manager or implement a codec.Codec.`

    m.Match(`$x.Bytes`).
        Where(m["x"].Type.Is(codecKey) && !m.File().PkgPath.Matches(allowed)).
        Report(msg)
}
```

- [ ] **Step 3: Write fixture documentation files**

The existing `gorules/rules.go` has no automated rule-level test
harness; the rules are validated by the fact that real production
code passes `task lint` (false positives surface as build breaks; false
negatives surface when an INV-27 violation lands and isn't caught).

For the new rules, ship two **documentation files** in
`gorules/testdata/` that enumerate expected violations as comments.
These are NOT automatically run, but they:

1. Document the contract for future contributors (here are the
   patterns the rule SHOULD flag).
2. Serve as a manual smoke-test surface — copy a function into a
   real package, run `task lint`, see the rule fire.
3. Establish a known location for future analysistest integration.

```go
// gorules/testdata/dek_no_serialize/expected_violations.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
//
// Documentation file (not compiled). Each function below documents a
// pattern the gorules/dek_no_serialize.go rule SHOULD flag. To smoke-
// test a rule, copy a function into a real package, save, and run
// `task lint` — the rule should fire on the marked line.
//
// This file is not built (build tag prevents compilation); it exists
// for reviewer reference only.

//go:build ignore_fixture
// +build ignore_fixture

package documentation

import (
    "encoding/json"
    "fmt"
    "log"
    "log/slog"

    "github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

func leakViaJSON(m *dek.Material) ([]byte, error) {
    return json.Marshal(m) // EXPECT: INV-27: dek.Material MUST NOT be passed to encoding/json
}

func leakViaFmtSprintf(m *dek.Material) string {
    return fmt.Sprintf("%v", m) // EXPECT: INV-27: dek.Material MUST NOT be passed to fmt formatting
}

func leakViaLogPrintf(m *dek.Material) {
    log.Printf("material: %v", m) // EXPECT: INV-27: dek.Material MUST NOT be passed to log functions
}

func leakViaSlogInfo(m *dek.Material) {
    slog.Info("dek", "material", m) // EXPECT: INV-27: dek.Material MUST NOT be passed to log/slog
}
```

```go
// gorules/testdata/codec_key_bytes/expected_violations.go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
//
// Documentation file (not compiled). See sibling expected_violations.go
// in dek_no_serialize/ for the file purpose.

//go:build ignore_fixture
// +build ignore_fixture

package documentation

import "github.com/holomush/holomush/internal/eventbus/codec"

func leakKeyBytes(k codec.Key) []byte {
    return k.Bytes // EXPECT: INV-27 (residual defense): codec.Key.Bytes reads are restricted
}
```

A future Phase 2 follow-up bead MAY add a real analysistest harness
that loads these files with the build tag flipped and asserts the
rules fire. Phase 2's acceptance is the practical one: rules compile,
real production code (the new dek + kek packages) passes lint without
false positives, and the documentation files give reviewers a clear
picture of intent.

- [ ] **Step 4: Update `.golangci.yaml` to load the new rule files**

Read `.golangci.yaml` line 142 (`ruleguard.rules: gorules/rules.go`). Change to:

```yaml
        ruleguard:
          rules: 'gorules/rules.go,gorules/dek_no_serialize.go,gorules/codec_key_bytes_allowlist.go'
```

(Or, if `golangci-lint`'s ruleguard config supports a glob, use `gorules/*.go`. Verify with: `task lint -- --help` and the linter version output.)

- [ ] **Step 5: Run lint to verify the rules load and the package compiles**

Run: `task lint`

Expected: PASS — no findings on real code; rule files compile under the `ruleguard` build tag.

- [ ] **Step 6: Run a manual ruleguard check against the fixtures (optional sanity)**

If `task ruleguard:check` exists, run it. Otherwise, document in the commit message that fixture verification is via `golangci-lint run --no-config -E gocritic gorules/testdata/...` against the testdata tree.

- [ ] **Step 7: Commit**

```text
feat(lint): ruleguard rules for INV-27 — dek.Material non-leakage

Two ruleguard rules per Phase 2 design notes Decision 4:

1. gorules/dek_no_serialize.go — sink-side enforcement: forbids
   dek.Material in encoding/json, encoding/gob, proto.Marshal,
   fmt formatting, log, log/slog, and known concrete io.Writer
   receivers (os.Stdout/Stderr). Arbitrary io.Writer-by-interface
   cannot be expressed in ruleguard; the static API surface test in
   internal/eventbus/crypto/dek/api_test.go is the ground-truth defense.

2. gorules/codec_key_bytes_allowlist.go — residual defense: forbids
   reads of codec.Key.Bytes outside internal/eventbus/codec/... and
   internal/eventbus/crypto/.... Tightens the master spec's "Kept
   (semantics unchanged)" classification for codec.Key by restricting
   WHO may read Bytes.

Testdata fixtures in gorules/testdata/ contain violations the rules
MUST flag.

Refs: docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md INV-27
Refs: docs/superpowers/plans/2026-04-30-event-payload-crypto-phase2-substrate.md Task 10
Bead: holomush-8qri
```

---

## Task 11: Acceptance verification + follow-up beads

**Files:**

- Modify: `holomush-8qri` bead description (per design notes' "Bead description drift" subsection)
- Create: two follow-up beads under `holomush-e49r`

- [ ] **Step 1: Run the full pre-PR gate**

Run: `task pr-prep`

Expected: ALL of the following pass:

- `task fmt` — no diff
- `task lint` — zero findings (including the two new ruleguard rules)
- `task lint:markdown` — design notes + plan pass rumdl
- `task license:check` — every new file has SPDX header
- `task test` — all unit tests pass; coverage ≥80% on new packages
- `task test:int` — all integration tests pass (testcontainer scenarios)
- `task test:e2e` — no regressions

If `task pr-prep` fails, fix the failure and re-run. Do NOT push if it does not pass.

- [ ] **Step 2: Cross-walk Phase 2 invariants against tests**

Verify each Phase 2 invariant has at least one passing test:

| Invariant | Statement | Verifying test |
| --------- | --------- | -------------- |
| INV-25    | AAD tampering breaks decryption (Phase 2: AAD function-level) | `TestBuild_AnyFieldChange_ChangesOutput` |
| INV-27    | dek.Material non-leakage | `TestPackageHasNoExportedByteSlices` + ruleguard fixtures + `TestNewMaterial_*` |
| INV-30    | Wrap/Unwrap roundtrip | `TestLocalAEADProvider_WrapUnwrap_Roundtrip` |
| INV-32    | NoneProvider refuses startup if crypto_keys non-empty | `TestNoneProvider_Constructor_RefusesIfCryptoKeysNonempty` |
| INV-33    | Provider integrity check refuses startup if wrap_key_id unknown | `TestLocalAEADProvider_Startup_RefusesIfWrapKeyIDUnknown` |
| INV-34    | NoneProvider.Wrap refuses | `TestNoneProvider_Wrap_RefusesWithTypedError` |

Run an `rg`-based sanity check that each test exists:

```bash
for inv in TestBuild_AnyFieldChange TestPackageHasNoExportedByteSlices TestLocalAEADProvider_WrapUnwrap TestNoneProvider_Constructor_Refuses TestLocalAEADProvider_Startup_Refuses TestNoneProvider_Wrap_Refuses; do
    rg -l "func ${inv}" || echo "MISSING: ${inv}"
done
```

- [ ] **Step 3: File the two follow-up beads**

```bash
BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd create \
    --title="Phase 2 follow-up: keyring KEKSource" \
    --description="Implement KEKSource for OS keyring (go-keyring; macOS Keychain, Linux Secret Service, Windows Credential Manager). Per Phase 2 design notes Decision 3, this is deferred from Phase 2 because cgo on macOS expands the test matrix.

Acceptance: kek.NewKeyringSource implements KEKSource; unit tests on Linux + macOS; KEKSource.Name() = 'local-aead/keyring'." \
    --type=task \
    --priority=2 \
    --parent=holomush-e49r

BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd create \
    --title="Phase 2 follow-up: systemd-credential KEKSource" \
    --description="Implement KEKSource that reads from \$CREDENTIALS_DIRECTORY/<name> via systemd LoadCredentialEncrypted=. Per Phase 2 design notes Decision 3, this is deferred from Phase 2 because the test path is Linux-only.

Acceptance: kek.NewSystemdCredentialSource implements KEKSource; integration test on Linux that reads a valid credential; KEKSource.Name() = 'local-aead/systemd-credential'." \
    --type=task \
    --priority=2 \
    --parent=holomush-e49r
```

Capture the IDs and verify:

```bash
BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd list --status=open --type=task | grep -i "Phase 2 follow-up"
```

- [ ] **Step 4: Update `holomush-8qri` bead description per drift note**

```bash
BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd update holomush-8qri --description="Phase 2 of holomush-e49r (Event Payload Cryptography). Lays the cryptographic substrate without yet emitting or reading any sensitive events.

Scope (per spec §11.1, narrowed by docs/superpowers/specs/2026-04-30-event-payload-crypto-phase2-substrate-design.md):
- KEKProvider interface (pluggable KEK source)
- LocalAEADProvider (XChaCha20-Poly1305 Wrap/Unwrap, per master spec §5.2)
- NoneProvider (refuses Wrap; refuses startup if crypto_keys non-empty per INV-32)
- KEKSource interface + FileSource (Argon2id) + EnvSource (dev/test only)
- crypto_keys table (migration 000013)
- events_audit dek_ref BIGINT + dek_version INTEGER columns (migration 000014)
- DEKManager skeleton (GetOrCreate + Resolve real; Add/Rotate/Rekey stubbed with tracking_bead)
- dek.Material type (opaque; AsCodecKey egress only)
- aad.Build canonical AAD bytes (master spec §4.2)
- Two ruleguard rules + static API surface test (INV-27)

Visible effect: server can wrap/unwrap DEKs; events_audit gains the new columns; nothing emits or reads sensitive yet.

Plan: docs/superpowers/plans/2026-04-30-event-payload-crypto-phase2-substrate.md
Design notes: docs/superpowers/specs/2026-04-30-event-payload-crypto-phase2-substrate-design.md
Master spec: docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md sections 4, 5, 11.1
Invariants covered: INV-25, INV-27, INV-30, INV-32, INV-33, INV-34

Dependencies: none — can land parallel with Phase 1 (already complete)
Blocks: Phase 3 (holomush-ojw1)"
```

- [ ] **Step 5: Final verification + push**

Run: `jj st` — confirm working copy is clean and all commits are present:

- Task 1: migration 000013
- Task 2: migration 000014
- Task 3: aad.Build
- Task 4: dek.Material + api_test.go
- Task 5: KEK provider + KEKSource interfaces + EnvSource + NoneProvider
- Task 6: kek.FileSource
- Task 7: kek.LocalAEADProvider
- Task 8: dek.Cache
- Task 9: dek.Manager skeleton + Store + integration tests
- Task 10: ruleguard rules
- Task 11: (this task — bead updates only; no Go changes)

Then push per `references/vcs-preamble.md`:

```bash
jj git fetch
jj rebase -r <change-id-range> -d main@origin
jj bookmark set holomush-8qri-phase2 -r @-
jj git push --branch holomush-8qri-phase2
```

Open the PR via `gh pr create` with title `feat(crypto): Phase 2 substrate — KEKProvider + DEKManager skeleton + crypto_keys` and a body that links the design notes, master spec, and the bead.

- [ ] **Step 6: Close `holomush-8qri` after PR merges**

```bash
BEADS_DIR=/Volumes/Code/github.com/holomush/holomush/.beads bd close holomush-8qri \
    --reason="Phase 2 substrate landed via PR. Phase 3 (holomush-ojw1) now unblocked."
```

---

## Phase 2 acceptance summary

A successful Phase 2 landing demonstrates ALL of the following:

| # | Acceptance criterion | Verifying mechanism |
| - | -------------------- | ------------------- |
| 1 | Server can wrap and unwrap DEKs via `LocalAEADProvider` | `TestLocalAEADProvider_WrapUnwrap_Roundtrip` (INV-30) |
| 2 | `NoneProvider` refuses startup with non-empty `crypto_keys` | `TestNoneProvider_Constructor_RefusesIfCryptoKeysNonempty` (INV-32) |
| 3 | `NoneProvider.Wrap` refuses at runtime | `TestNoneProvider_Wrap_RefusesWithTypedError` (INV-34) |
| 4 | Provider startup integrity check refuses unrecoverable rows | `TestLocalAEADProvider_Startup_RefusesIfWrapKeyIDUnknown` (INV-33) |
| 5 | `crypto_keys` table exists with the master-spec §4.3 schema | `TestMigrator_FullCycle` (with `crypto_keys` in `expectedTables`) |
| 6 | `events_audit` has `dek_ref BIGINT` + `dek_version INTEGER` (nullable) + partial index | `TestEventsAuditHasDEKColumnsAfterMigration014` |
| 7 | `DEKManager.GetOrCreate` mints + persists, idempotent for same context | `TestManager_GetOrCreate_MintsAndPersists` |
| 8 | `DEKManager.Resolve` round-trips DB → cache → caller | `TestManager_Resolve_ByKeyIDAndVersion` |
| 9 | `DEKManager.Resolve` returns `DEK_NOT_FOUND` for missing rows | `TestManager_Resolve_NotFound_ReturnsErrDEKNotFound` |
| 10 | Concurrent `GetOrCreate` race converges on one row | `TestManager_GetOrCreate_ConcurrentMintRace` |
| 11 | `Add` / `Rotate` / `Rekey` stubs carry `tracking_bead` + `phase` from allow-set | `TestManagerStubsCarryTrackingBeadFromAllowSet` |
| 12 | `dek.Material` exports no `[]byte` (function/method/field) | `TestPackageHasNoExportedByteSlices` (INV-27) |
| 13 | `aad.Build` is deterministic and tamper-sensitive on every input field | `TestBuild_Deterministic`, `TestBuild_AnyFieldChange_ChangesOutput`, `TestBuild_ActorMarshalIsDeterministic` (INV-25) |
| 14 | Ruleguard rules compile + load via golangci-lint | `task lint` succeeds with both new rule files referenced in `.golangci.yaml`; documentation in `gorules/testdata/dek_no_serialize/expected_violations.go` enumerates patterns the rule should flag (smoke-test by copying into a real package and running `task lint`) |
| 15 | `codec.Key.Bytes` allowlist rule compiles + loads | Same as 14 for `gorules/codec_key_bytes_allowlist.go`; documentation in `gorules/testdata/codec_key_bytes/expected_violations.go` |
| 16 | `task pr-prep` is green | CI mirror; serialized per user |
| 17 | Two follow-up beads filed under `holomush-e49r` | `bd list --type=task` shows both |
| 18 | `holomush-8qri` description updated per drift note | `bd show holomush-8qri` |

Phase 2 landing unblocks Phase 3 (`holomush-ojw1`: EventSink encryption + AuthGuard + decrypt-on-fanout + downgrade fence + cold-tier QSH crypto path), which is where the `KeyProvider` / `KeySelector` removal also lands per the design notes' "Disposition of substrate" section.
