<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Event Payload Cryptography — Phase 2 Substrate Design Notes

## Status

Draft — design captured for design-reviewer pass before plan-writing.

## Authors

Sean Brandt (with Claude Opus 4.7).

## Date

2026-04-30

## Relationship to master spec

This document is a **Phase 2 narrowing** of the master design at
`docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`. It does
NOT redefine architecture or invariants — those live in the master spec. It
captures four Phase-2-specific scoping decisions that the master spec leaves
to implementation, plus the concrete component-and-test surface for the
phase.

The master spec sections this document narrows:

- §4 (data model): `crypto_keys` table + `events_audit` columns
- §5 (crypto provider interface): `KEKProvider`, `LocalAEADProvider`,
  `NoneProvider`, `KEKSource`, `DEKManager`, cache, `dek.Material`
- §11.1 (phasing): Phase 2 row — "KEKProvider + LocalAEADProvider +
  NoneProvider + crypto_keys table + events_audit cols + DEKManager skeleton
  with dek.Material + lint rule"
- §2 invariants relevant to Phase 2: INV-4, INV-25 (AAD), INV-27 (Material
  non-leakage), INV-30 (Wrap/Unwrap roundtrip), INV-32 (NoneProvider
  startup refusal), INV-33 (provider integrity check), INV-34 (NoneProvider
  Wrap refusal)

## Bead

`holomush-8qri` — Phase 2: KEKProvider + crypto_keys + DEKManager skeleton.
Parent epic: `holomush-e49r` (Event Payload Cryptography).

## Phase 2 design decisions

The master spec leaves four Phase-2-shaping questions open. Decisions:

### Decision 1: DEKManager skeleton scope

The `dek.Manager` interface is fully declared; only the encrypt-path and
decrypt-path methods are real-implemented in Phase 2:

| Method | Phase 2 status |
| ------ | -------------- |
| `GetOrCreate(ctx, ContextID, []Participant) → codec.Key` | Real (mints DEK, wraps via KEKProvider, INSERTs `crypto_keys` row) |
| `Resolve(ctx, KeyID, version) → codec.Key`               | Real (SELECT, unwraps via KEKProvider, returns) |
| `Add(ctx, ContextID, Participant)`                       | Stub: `oops.Code("DEK_ADD_NOT_IMPLEMENTED").With("tracking_bead", "holomush-fi0n").With("phase", 4)` |
| `Rotate(ctx, ContextID, []Participant, reason)`          | Stub: `oops.Code("DEK_ROTATE_NOT_IMPLEMENTED").With("tracking_bead", "holomush-fi0n").With("phase", 4)` |
| `Rekey(ctx, ContextID, justification, OperatorFactors)`  | Stub: `oops.Code("DEK_REKEY_NOT_IMPLEMENTED").With("tracking_bead", "holomush-jxo8").With("phase", 5)` |

**Rationale.** Phase 2's stated visible effect (master spec §11.1) is "server
can wrap/unwrap DEKs". `GetOrCreate` is the wrap path; `Resolve` is the
unwrap path. Implementing more drags Phase 4 work (`Add`, `Rotate`) and
Phase 5 work (`Rekey`) forward without need; both have explicit child epics
under `holomush-e49r`.

**Tracking-bead error pattern.** Each stub MUST carry a `tracking_bead`
field whose value is a real, open bead ID. The pattern is stronger than a
`// TODO` comment because operators see the tracking ID in error logs the
moment a caller hits the stub.

Phase 2 ships **two** layers of enforcement, both bundled into this phase
(NOT deferred to a follow-up):

1. **Unit test** in `internal/eventbus/crypto/dek/api_test.go` — asserts
   each stub returns an error whose `oops` context includes a
   `tracking_bead` field with a value matching `^holomush-[a-z0-9]+$` and a
   non-empty `phase` field.
2. **Static test** (also in `api_test.go`) — asserts the `tracking_bead`
   value is one of an explicit, hardcoded allow-set
   (`{"holomush-fi0n", "holomush-jxo8"}` for Phase 2). Renaming or closing
   either bead without updating this list fails CI immediately. Cheaper
   than shelling out to `bd show`; deterministic; no external dependency in
   the test path.

The "is the bead currently OPEN?" liveness check is explicitly out of
scope for Phase 2 — Phase 4 and Phase 5 land before stubs are at risk of
silent rot in production logs (per master spec §11.1 phasing).

### Decision 2: AAD canonicalization placement

`aad.Build(event *eventbusv1.Event, codecName string, dekRef uint64, dekVersion uint32) []byte`
ships in Phase 2 at `internal/eventbus/crypto/aad/aad.go` per master spec §4.2.

**Canonical input list** (mirrored verbatim from master spec §4.2 lines
529–547). The function MUST hash these inputs in this order, with the
exact byte-layout below:

```text
"HMAAD\x01"                               // 6-byte magic + version

uint32(len(event.Id))                     // 4 bytes big-endian
event.Id                                  // ULID, 16 bytes

uint32(len(event.Subject))                // 4 bytes big-endian
[]byte(event.Subject)                     // UTF-8 bytes

uint32(len(event.Type))                   // 4 bytes big-endian
[]byte(event.Type)                        // UTF-8 bytes

int64(event.Timestamp.AsTime().UnixNano()) // 8 bytes big-endian

uint32(len(actorBytes))                   // 4 bytes big-endian
actorBytes                                // proto.MarshalOptions{Deterministic: true}.
                                          //   Marshal(event.Actor)
                                          //   — REQUIRED; non-deterministic
                                          //   marshal breaks INV-25 silently

uint32(len(codecName))                    // 4 bytes big-endian
[]byte(codecName)                         // header value

uint64(dekRef)                            // 8 bytes big-endian (0 for identity codec)
uint32(dekVersion)                        // 4 bytes big-endian (0 for identity codec)
```

**Actor proto-marshal determinism.** The `event.Actor` proto submessage
MUST be marshaled via `proto.MarshalOptions{Deterministic: true}.Marshal(...)`,
NOT bare `proto.Marshal(...)`. proto3's default marshaler is
non-deterministic for repeated fields and maps; using bare `Marshal` would
yield AAD that varies between encrypt and decrypt under different runtime
conditions and silently break INV-25. A dedicated test
(`aad.Build_ActorMarshalIsDeterministic`) enforces this by marshaling the
same Actor 1000 times and asserting all outputs are byte-equal.

**Rationale for shipping in Phase 2.** The function is pure substrate
(~50 LOC) with no dependency on emit/decrypt paths. Shipping it with INV-25
unit tests in isolation is cleaner than burying its tests inside Phase 3
codec tests. Phase 3 imports `aad` and uses it; no churn between phases.
proto3's `Deterministic` option has caveats around unknown fields that the
spec accepts (the Event proto has no maps and stable repeated-field order),
and the Lua plugin host doesn't speak proto natively, so a hand-rolled
canonical form is the spec's choice — having it live in its own package
keeps the contract explicit.

### Decision 3: KEK source set for Phase 2

| Source | Phase 2 status | Path |
| ------ | -------------- | ---- |
| `file` (encrypted file + Argon2id-derived passphrase) | Real (production default per spec §5.3) | `internal/eventbus/crypto/kek/source_file.go` |
| `env` (raw KEK from env var; refused in prod mode) | Real (dev/test sentinel only) | `internal/eventbus/crypto/kek/source_env.go` |
| `keyring` (OS keyring via go-keyring) | Deferred — follow-up bead | NEW bead `Phase 2 follow-up: keyring KEKSource`, parented under `holomush-e49r` |
| `systemd-credential` (`LoadCredentialEncrypted=`) | Deferred — follow-up bead | NEW bead `Phase 2 follow-up: systemd-credential KEKSource`, parented under `holomush-e49r` |

**Rationale.** Phase 3 does not gate on which KEK sources exist — only that
*one* production source works. `file` is sufficient. `env` is required
anyway for unit + integration tests of `LocalAEADProvider` (the alternative
is leaking KEK injection into test internals). `keyring` brings cgo on
macOS; `systemd-credential` brings a Linux-only test path. Filing them as
follow-up beads keeps Phase 2 review surface contained and gives both
sources explicit acceptance criteria as siblings of `holomush-8qri`.

### Decision 4: `dek.Material` API shape + lint rule mechanism

**API shape — opaque struct, no `Bytes()` accessor.** `dek.Material` has a
private `bytes []byte` field. The **sole exported egress** is
`func (m *Material) AsCodecKey(id codec.KeyID) codec.Key`, which constructs
the substrate `codec.Key` value inline. Note that the returned `codec.Key`
exposes its `Bytes []byte` field directly (the substrate codec contract
requires this — codec implementations need raw key bytes for AEAD). Reads
of that field by callers outside the codec package are the residual
leakage path that Lint Rule 2 (below) is **required, not optional**, to
contain. The opacity guarantee is: "Material as a value cannot be
exfiltrated via serializers/loggers; the only way to extract bytes is to
construct a `codec.Key` via `AsCodecKey`, and `codec.Key.Bytes` is
allowlisted to a single package tree by Lint Rule 2."

**Lint rule 1 — sink-side enforcement (encodes INV-27).**
File: `gorules/dek_no_serialize.go`. Ruleguard rejects expressions of type
`*dek.Material` or `dek.Material` passed as arguments to an **enumerated**
list of forbidden sinks. ruleguard cannot match by interface satisfaction
(reference: `gorules/rules.go:124-138` matches concrete receivers + method
names + arg-type filters), so each sink is a discrete pattern:

| Forbidden sink | Pattern shape |
| -------------- | ------------- |
| `encoding/json.Marshal($x)` | `m.Match(\`json.Marshal($x)\`).Where(m["x"].Type.Is(\`*dek.Material\`) \|\| m["x"].Type.Is(\`dek.Material\`))` |
| `encoding/json.MarshalIndent($x, ...)` | same |
| `encoding/json.NewEncoder($_).Encode($x)` | `m.Match(\`$_.Encode($x)\`).Where(...)` (broad — relies on the type filter) |
| `encoding/gob.NewEncoder($_).Encode($x)` | same |
| `google.golang.org/protobuf/proto.Marshal($x)` | `m.Match(\`proto.Marshal($x)\`)...` |
| `google.golang.org/protobuf/proto.MarshalOptions.Marshal($_, $x)` | `m.Match(\`$_.Marshal($_, $x)\`)...` |
| `fmt.Sprint($*args)` / `Sprintf` / `Sprintln` | `m.Match(\`fmt.Sprint($*xs)\`).Where(m["xs"].Contains(...))` — see note |
| `fmt.Print($*args)` / `Printf` / `Println` | same |
| `fmt.Fprint($_, $*args)` / `Fprintf` / `Fprintln` | same |
| `fmt.Errorf($fmt, $*args)` | `m.Match(\`fmt.Errorf($fmt, $*xs)\`)...` |
| `log.Print($*args)` / `Printf` / `Println` / `Fatal*` / `Panic*` | enumerated as separate patterns |
| `log/slog.Info($msg, $*args)` / `Debug` / `Warn` / `Error` / `Log` / `LogAttrs` | enumerated, one per method |
| `log/slog.Logger.Info($msg, $*args)` / `Debug` / `Warn` / `Error` / `Log` / `LogAttrs` | match `$_.Info($_, $*xs)` etc. with type filter on `$_` being `*slog.Logger` |
| `slog.Any($_, $x)` / `slog.Group($_, $x)` | enumerated |
| `os.Stdout.Write($x)` / `os.Stderr.Write($x)` | concrete receivers only |
| `bytes.Buffer.Write($x)` / `strings.Builder.Write($x)` | enumerated concrete receivers |
| `bufio.Writer.Write($x)` | enumerated |

**Note on variadic patterns.** `m["xs"].Contains(\`$_\`).Where(...)` is not
directly expressible in ruleguard. The plan-level approach is: for variadic
sinks, write one pattern per argument position (`$_` for non-Material
positions, `$x` with type filter for the position under check). For
common cases this is 1–2 patterns per sink. Acceptable cost.

**Coverage gap acknowledged.** The "any value satisfying `io.Writer`"
clause in INV-27 cannot be expressed in ruleguard. Coverage for arbitrary
`io.Writer.Write($x)` is achieved instead by:

- The **enumerated concrete receivers** above (`os.Stdout/Stderr`,
  `bytes.Buffer`, `strings.Builder`, `bufio.Writer`).
- The **static API surface test** below (no `[]byte` exported from `dek`).
- A **code review convention** documented in
  `site/docs/contributing/dek-material.md`: any new `io.Writer`
  implementation in the codebase that may receive Material bytes is added
  to `gorules/dek_no_serialize.go` as part of the same PR. CODEOWNERS gates
  the file.

This is partial coverage by design. The static test catches the more
likely failure mode (someone adds a getter) and the ruleguard catches the
named common sinks; arbitrary-`io.Writer` exfiltration via a brand-new
custom writer is a possibility we accept as low-probability and rely on
review for.

**Lint rule 2 — `codec.Key.Bytes` allowlist (REQUIRED, defense in depth
for INV-27).**
File: `gorules/codec_key_bytes_allowlist.go`. Reads of `codec.Key.Bytes`
outside the allowed package set fail lint. Allowed:

- `internal/eventbus/codec/...` (codec implementations need raw key bytes)
- `internal/eventbus/crypto/...` (substrate construction + tests)
- Test files (`*_test.go`) within those trees only

Mechanism mirrors the existing `CursorPackageInternal` rule in
`gorules/rules.go` (allowlist via `m.File().PkgPath.Matches(allowed)`).
Pattern: `m.Match(\`$x.Bytes\`).Where(m["x"].Type.Is(\`codec.Key\`) && !m.File().PkgPath.Matches(allowed)).Report(...)`.

**Master-spec amendment note.** This rule tightens the master spec's
"Kept (semantics unchanged)" classification for `codec.Key` (master spec
§"Relationship to substrate spec" line 96–98) by restricting *who* may
read the `Bytes` field. The field itself and its semantics are unchanged
— Phase 2 only adds a caller-package allowlist as defense-in-depth for
INV-27. This is a Phase 2 substrate addition, not a redefinition.

**Static test — no exported `[]byte` from dek package (REQUIRED).**
File: `internal/eventbus/crypto/dek/api_test.go`. Uses
`golang.org/x/tools/go/packages` to load `internal/eventbus/crypto/dek`,
enumerate exported functions, methods, and struct fields, and assert that
no exported function or method returns `[]byte` and no exported field has
type `[]byte`. Catches API drift that adds an accessor and bypasses both
ruleguard rules. This test is the **ground-truth defense** for the API
opacity claim; the ruleguards are convenience checks for known sinks.

**Rationale for two rules + one static test.** Sink-side ruleguard encodes
INV-27 textually for the common sinks and is the rule a reviewer reads to
understand the policy. The allowlist ruleguard catches the residual
`codec.Key` escape path that the sink-side rule cannot reach. The static
test catches the case where a future contributor adds a
`func (m *Material) Raw() []byte` accessor and bypasses both ruleguards
by introducing a new export rather than misusing existing ones — and it
catches the arbitrary-`io.Writer` gap from above.

## Disposition of substrate `KeyProvider` / `KeySelector` / `KeyLabel`

Master spec lines 121–124 mandate that
`internal/eventbus/codec/codec.go` is updated to remove `KeyLabel`,
`KeySelector`, and the label-side of `KeyProvider` "when this design
lands". The master spec does NOT pin a phase. Phase 2 takes the position:

**Defer removal to Phase 3. Phase 2 leaves these types and their callers
untouched.**

Live production callers today (verified via `rg "codec.KeyProvider|codec.KeySelector|codec.KeyLabel"`):

- `internal/eventbus/publisher.go:72`, `:78`, `:113-114`, `:394`
- `internal/eventbus/subscriber.go:51`, `:89`, `:168`, `:221`, `:357`
- `internal/eventbus/history/hot_jetstream.go:32`, `:36`, `:349`
- `internal/eventbus/history/tier.go:114`, `:178`
- Plus test files in those trees

**Rationale for deferral.** Phase 2's stated visible effect (master spec
§11.1) is "server can wrap/unwrap DEKs; nothing emits or reads sensitive
yet". Touching publisher/subscriber/history files is exactly the
emit/decrypt rewrite that Phase 3 owns. Phase 3 already has the
EventSink encryption path + decrypt-on-fanout in scope — replacing the
`KeyProvider.ByID(KeyID)` calls with `dek.Manager.Resolve(KeyID, version)`
and removing `KeyLabel`/`KeySelector` is a natural sub-task of that
rewrite. Doing the removal in Phase 2 would either:

- Cascade publisher/subscriber edits into a substrate-only PR (scope
  creep for Phase 2, premature for Phase 3), or
- Require a temporary `KeyProvider` shim wrapping `dek.Manager.Resolve`
  that exists only for one phase (gratuitous churn).

Phase 2 ships `dek.Manager.Resolve` alongside the existing
`codec.KeyProvider.ByID`. They coexist for one phase. Phase 3 deletes the
substrate types, rewrites the call sites, and updates the substrate spec
(`docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md`) with
a back-link.

**Phase 3 scope addition (must be reflected in Phase 3's plan):** delete
`KeyLabel`, `KeySelector`, and the label-side of `KeyProvider` (the
`Active(label)` method) from `internal/eventbus/codec/codec.go`; delete
the unexported `identityKeySelector` type from
`internal/eventbus/publisher.go` (defined at `publisher.go:392`); and
rewrite each of the 13 call sites listed above to use `dek.Manager`.
`KeyProvider.ByID(KeyID)` is renamed in spirit to
`dek.Manager.Resolve(KeyID, version)`.

## Architecture (per master spec §3 + §5.1, narrowed to Phase 2)

Three packages under `internal/eventbus/crypto/`:

- **`kek/`** — `Provider` interface, `LocalAEADProvider`, `NoneProvider`,
  `KEKSource` interface, `FileSource`, `EnvSource`. Receives only opaque
  DEK bytes. Never sees event context.
- **`dek/`** — `Material` (opaque) + `Manager` interface + cache. Holds
  unwrapped DEKs in process memory only (INV-27). Talks to `kek.Provider`
  for wrap/unwrap and to PostgreSQL `crypto_keys` for persistence.
- **`aad/`** — `Build` pure function per master spec §4.2. No callers in
  Phase 2; tested in isolation.

Schema additions:

- `internal/store/migrations/000013_create_crypto_keys.{up,down}.sql`
  (per master spec §4.3 — no deviations)
- `internal/store/migrations/000014_events_audit_dek_columns.{up,down}.sql`
  (per master spec §4.7 — no deviations; nullable `dek_ref BIGINT` +
  `dek_version INTEGER`, partial index on `dek_ref WHERE NOT NULL`, no FK
  per master spec rationale)

Lint additions: `gorules/dek_no_serialize.go`,
`gorules/codec_key_bytes_allowlist.go`. Both build-tagged like the existing
`gorules/rules.go`.

## Components — Phase 2 status

| Symbol | Path | Phase 2 status |
| ------ | ---- | -------------- |
| `kek.Provider` interface | `internal/eventbus/crypto/kek/provider.go` | Real |
| `kek.LocalAEADProvider` | `internal/eventbus/crypto/kek/local_aead.go` | Real |
| `kek.NoneProvider` | `internal/eventbus/crypto/kek/none.go` | Real (INV-32, INV-34) |
| `kek.KEKSource` interface | `internal/eventbus/crypto/kek/source.go` | Real |
| `kek.FileSource` | `internal/eventbus/crypto/kek/source_file.go` | Real (Argon2id m=64MiB t=3 p=4; AEAD wrap; `HMK\x01` magic + 16-byte salt + nonce + wrapped-KEK + tag) |
| `kek.EnvSource` | `internal/eventbus/crypto/kek/source_env.go` | Real (refused in prod mode) |
| `dek.Material` | `internal/eventbus/crypto/dek/material.go` | Real (opaque; sole egress is `AsCodecKey(KeyID) codec.Key`) |
| `dek.Manager` interface | `internal/eventbus/crypto/dek/manager.go` | Real interface |
| `dek.Manager.GetOrCreate` / `Resolve` | `internal/eventbus/crypto/dek/manager.go` | Real implementation |
| `dek.Manager.Add` / `Rotate` / `Rekey` | `internal/eventbus/crypto/dek/manager.go` | Stubs returning `oops.Code("DEK_*_NOT_IMPLEMENTED").With("tracking_bead", ...)` |
| `dek.Cache` | `internal/eventbus/crypto/dek/cache.go` | Real (LRU + TTL; in-process memory only per INV-27) |
| `aad.Build` | `internal/eventbus/crypto/aad/aad.go` | Real |
| `crypto_keys` table | `internal/store/migrations/000013_create_crypto_keys.{up,down}.sql` | Real |
| `events_audit` columns | `internal/store/migrations/000014_events_audit_dek_columns.{up,down}.sql` | Real |
| Sink-side ruleguard | `gorules/dek_no_serialize.go` | Real |
| `codec.Key.Bytes` allowlist | `gorules/codec_key_bytes_allowlist.go` | Real |
| Static API surface test | `internal/eventbus/crypto/dek/api_test.go` | Real |

## Data flow

Phase 2 has no production emit/decrypt path. The substrate is exercised by
unit + integration tests.

```text
Server startup (config.crypto.provider.name=local-aead, kek_source=file):

  1. LocalAEADProvider construction:
       FileSource.Load(ctx) →
         open key file → read HMK\x01 magic + salt + nonce + wrapped-KEK + tag
         fetch passphrase per passphrase_source config
         Argon2id(passphrase, salt, m=64MiB, t=3, p=4) → unlock key
         chacha20poly1305.Open(wrapped-KEK, unlock) → KEK in process memory
         fingerprint = sha256(KEK) → kekKeyID for this load

  2. Provider integrity check (INV-33):
       SELECT DISTINCT wrap_key_id FROM crypto_keys
       for each: assert provider can unwrap → otherwise refuse start

  3. NoneProvider variant only — INV-32 enforced at construction time:
       NoneProvider's constructor (NewNoneProvider(ctx, db)) runs
       SELECT count(*) FROM crypto_keys synchronously; if count > 0 the
       constructor returns oops.Code("CRYPTO_KEYS_NONEMPTY_WITH_NONE_PROVIDER")
       and the server-boot caller refuses to start. Concurrent INSERTs
       after this check (which would be from a different replica running
       a different provider) are not Phase 2's concern; they are
       prevented by deployment hygiene.
       INV-34 is enforced separately at runtime in NoneProvider.Wrap.


DEKManager.GetOrCreate(ctx, ContextID{Type:"scene", ID:"01ABC"}, participants):

  - Cache lookup by (ContextID, active version) → if hit, return Material.AsCodecKey(KeyID)
  - Otherwise: SELECT * FROM crypto_keys
                 WHERE context_type=$1 AND context_id=$2 AND rotated_at IS NULL
                 ORDER BY version DESC LIMIT 1
  - If row exists: KEKProvider.Unwrap(row.wrapped_dek, row.wrap_key_id) → bytes
                   → Material{bytes} → cache.Put → return AsCodecKey(row.id)
  - If no row: crypto/rand → 32 bytes → Material
                KEKProvider.Wrap(bytes via internal accessor) → wrapped, kekKeyID
                INSERT crypto_keys (context_type, context_id, version=1, wrapped_dek,
                                    wrap_provider, wrap_key_id, participants)
                  RETURNING id
                On unique-constraint violation on (context_type, context_id, version):
                  another replica/goroutine raced to mint the same context's
                  v1 DEK. Re-execute the SELECT path; the racing winner's
                  row is now visible. Loser uses winner's row. This race is
                  in-scope for Phase 2 because GetOrCreate runs before any
                  participant changes (which is Phase 4's territory).
                cache.Put → return AsCodecKey(returned_id)


DEKManager.Resolve(ctx, KeyID, version):

  - Cache lookup by (KeyID, version) → hit returns Material.AsCodecKey(KeyID)
  - Miss: SELECT * FROM crypto_keys WHERE id=$1 AND version=$2
  - row not found → ErrDEKNotFound
                    (Phase 3's caller falls back to cold tier per INV-39)
  - Unwrap, cache, return


DEKManager.Add / Rotate / Rekey:
  return oops.Code("DEK_<X>_NOT_IMPLEMENTED").
      With("tracking_bead", "<bead-id>").
      With("phase", <N>).
      Errorf("DEKManager.<X> lands in Phase <N> (epic <bead-id>)")
```

## Error handling

| Failure | Path | Behavior |
| ------- | ---- | -------- |
| Key file missing/corrupt | `FileSource.Load` | `oops.Code("KEK_FILE_LOAD_FAILED").Wrap(err)` — server refuses start |
| Wrong passphrase | `FileSource.Load` AEAD-open fails | `oops.Code("KEK_PASSPHRASE_INVALID")` — refuses start; no retry loop in Phase 2 |
| `EnvSource` in prod mode | `LocalAEADProvider` constructor | `oops.Code("KEK_ENV_SOURCE_PROD_FORBIDDEN")` — refuses start |
| Stale `wrap_key_id` (INV-33) | Provider integrity check | `oops.Code("KEK_PROVIDER_CANNOT_UNWRAP_EXISTING_DEKS")` listing the unrecoverable rows |
| `crypto_keys` row exists with NoneProvider (INV-32) | `NoneProvider` startup | `oops.Code("CRYPTO_KEYS_NONEMPTY_WITH_NONE_PROVIDER")` |
| `Wrap` on `NoneProvider` (INV-34) | `NoneProvider.Wrap` | `oops.Code("CRYPTO_NONE_PROVIDER_WRAP_REFUSED")` |
| DEK row not found on `Resolve` | `DEKManager.Resolve` | `oops.Code("DEK_NOT_FOUND").With("key_id", id).With("version", v)` |
| Cache miss → DB error | `DEKManager.Resolve` | wrapped DB error; caller decides delivery contract (Phase 3 surfaces as metadata-only) |
| Stub method called | `Add` / `Rotate` / `Rekey` | Typed `oops.Code("DEK_<X>_NOT_IMPLEMENTED")` with `tracking_bead` field |

## Testing

Coverage target: ≥80% per package per project policy
(`internal/eventbus/crypto/...`).

| Test | Type | Verifies |
| ---- | ---- | -------- |
| `kek.LocalAEADProvider_WrapUnwrap_Roundtrip` | Unit | INV-30 (Wrap then Unwrap recovers DEK byte-for-byte) |
| `kek.LocalAEADProvider_Unwrap_TamperedWrappedBytes_Fails` | Unit | AEAD tag check rejects corruption |
| `kek.NoneProvider_Wrap_Refuses` | Unit | INV-34 |
| `kek.NoneProvider_Startup_RefusesIfCryptoKeysNonempty` | Integration (testcontainer) | INV-32 |
| `kek.LocalAEADProvider_Startup_RefusesIfWrapKeyIDUnknown` | Integration | INV-33 |
| `kek.FileSource_Load_DerivesKEKDeterministically` | Unit (table-driven) | Argon2id parameters + file-format round-trip |
| `kek.EnvSource_ProdMode_Refused` | Unit | dev/test sentinel honored |
| `kek.NoneProvider_Constructor_RefusesIfCryptoKeysNonempty` | Integration (testcontainer) | INV-32 enforced at constructor (synchronous DB SELECT before construct returns) |
| `dek.Material_NoExportedByteAccessors_NoExportedByteFields` | Static (`golang.org/x/tools/go/packages`) | API surface invariant: no exported func/method returns `[]byte`; no exported field has type `[]byte` |
| `dek.Manager_GetOrCreate_MintsAndPersists` | Integration | First call creates row + caches |
| `dek.Manager_GetOrCreate_ReturnsExisting` | Integration | Idempotent for same context |
| `dek.Manager_GetOrCreate_ConcurrentMintRace` | Integration | Two goroutines call GetOrCreate(scene:X) simultaneously; one wins INSERT, the loser's INSERT raises unique-violation, re-SELECTs, returns winner's DEK. Both callers see byte-equal `codec.Key.Bytes`. |
| `dek.Manager_Resolve_ByKeyIDAndVersion` | Integration | DB → cache → return |
| `dek.Manager_Resolve_NotFound_ReturnsErrDEKNotFound` | Integration | Phase 3 cold-tier fallback contract |
| `dek.Manager_Add_Stub_ReturnsTrackingBead_holomush-fi0n` | Unit | Stub error carries `tracking_bead=holomush-fi0n`, `phase=4` |
| `dek.Manager_Rotate_Stub_ReturnsTrackingBead_holomush-fi0n` | Unit | Same, `holomush-fi0n`, `phase=4` |
| `dek.Manager_Rekey_Stub_ReturnsTrackingBead_holomush-jxo8` | Unit | Same, `holomush-jxo8`, `phase=5` |
| `dek.Manager_StubBeads_AllInAllowSet` | Static | Each stub's `tracking_bead` value is in the hardcoded allow-set `{"holomush-fi0n", "holomush-jxo8"}`; renaming or closing either bead without updating fails CI |
| `dek.Cache_LRUEvictionAndTTL` | Unit | Bounded memory; TTL safety net |
| `aad.Build_Deterministic` | Unit | Same inputs → identical bytes (1000 invocations) |
| `aad.Build_AnyFieldChange_ChangesOutput` | Unit (table-driven over `id`, `subject`, `type`, `timestamp`, `actor`, `codec_name`, `dek_ref`, `dek_version`, `magic_prefix`) | INV-25 — every named input field is mutated in turn, output bytes change for each |
| `aad.Build_ActorMarshalIsDeterministic` | Unit | `event.Actor` marshaled 1000× via `proto.MarshalOptions{Deterministic:true}.Marshal` returns byte-equal output every time. Bare `proto.Marshal(event.Actor)` would silently produce non-byte-equal AAD across runs and break INV-25; this runtime test is the actual defense (the `dek_no_serialize` ruleguard rule does NOT catch this — its type filter scopes to `dek.Material`, not `*eventbusv1.Actor`). |
| `aad.Build_VersionMagic` | Unit | `HMAAD\x01` prefix locked; output begins with this 6-byte sequence |
| `gorules/dek_no_serialize` ruleguard fixtures | Lint test | Seeded violations in `gorules/testdata/dek_no_serialize/` fail `task lint`; one fixture per enumerated sink in Decision 4 Lint Rule 1 |
| `gorules/codec_key_bytes_allowlist` ruleguard fixtures | Lint test | `codec.Key.Bytes` reads outside allowlist fail lint; one fixture in `gorules/testdata/codec_key_bytes/` |
| Migration round-trip (000013 crypto_keys, 000014 events_audit_dek_columns) | Integration (testcontainer; both `up.sql` and `down.sql`) | Schema lands and reverts cleanly; idempotent re-application |

## Out of scope for Phase 2

Per master spec §11.1, the following are explicitly later phases:

- Codec implementations (`xchacha20poly1305-v1`, `aes-gcm-v1`) — Phase 3
- EventSink encrypt-on-emit path — Phase 3
- AuthGuard — Phase 3
- Decrypt-on-fanout — Phase 3
- N-of-N replica cache invalidation (INV-28, INV-29) — Phase 4
- `Add` / `Rotate` lifecycle ops — Phase 4
- `Rekey` CLI + `AdminReadStream` + `OperatorAuthProvider` — Phase 5
- `VaultTransitProvider` + provider migration CLI — Phase 6
- Plugin SDK helpers + INV-50 downgrade fence test — Phase 7
- Site documentation deliverables — Phase 8

## Follow-up beads filed at Phase 2 plan creation

Filed under `holomush-e49r`, NOT blocking Phase 3:

- `Phase 2 follow-up: keyring KEKSource` (uses go-keyring; macOS Keychain,
  Linux Secret Service, Windows Credential Manager)
- `Phase 2 follow-up: systemd-credential KEKSource` (`LoadCredentialEncrypted=`)

The previously-considered "stub-error tracking_bead static check" has been
**bundled into Phase 2** (see Decision 1 → Tracking-bead error pattern,
items 1 and 2) rather than deferred to a follow-up.

## Bead description drift

The `holomush-8qri` epic description names "events_audit table column
additions (codec, key_id, AAD-related cols)". Per master spec §4.7 and
this document's migration 000014, the actual additions are
`dek_ref BIGINT` and `dek_version INTEGER` only — `codec` already exists
in the events_audit table (since migration 000009), `key_id` is the
spec's name for `dek_ref`, and "AAD-related cols" do not exist anywhere
in the master spec (AAD is reconstructed at decrypt time from cleartext
fields per spec §4.2; it is never stored). The plan-writer should
follow this document and the master spec, not the loose bead text. The
plan task that lands the migration MAY include a `bd update holomush-8qri`
step that aligns the bead description.

## References

- Master spec: `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`
- Phase 1 plan (template): `docs/superpowers/plans/2026-04-25-event-payload-crypto-phase1-manifest-grammar.md`
- Existing ruleguard rules: `gorules/rules.go`
- Substrate codec types: `internal/eventbus/codec/codec.go`
- Existing audit table migration: `internal/store/migrations/000009_create_events_audit.up.sql`
- Bead: `holomush-8qri` — Phase 2: KEKProvider + crypto_keys + DEKManager skeleton
- Parent epic: `holomush-e49r` — Event Payload Cryptography
