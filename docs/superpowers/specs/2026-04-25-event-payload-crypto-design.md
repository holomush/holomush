# Event Payload Cryptography — Design

## Status

**DRAFT** — design proposal pending implementation plan.

Builds on: [docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md](2026-04-18-jetstream-event-log-design.md)
(JetStream event log + PostgreSQL audit projection; this design adds payload
encryption on top of the codec seam that cutover already shipped).

## Authors

- Sean Brandt
- Claude (collaborator)

## Date

2026-04-25

---

## Context

After the JetStream cutover (PR #252, epic `holomush-1tvn`), HoloMUSH has a
clean event substrate: JetStream as the durable log, an audit projection into
PostgreSQL, and a per-row `codec` column on both tiers. The codec was
deliberately built with encryption in mind — `internal/eventbus/types.go:72`
already says `Payload []byte // codec.Encode output (ciphertext if encryption
is on)` — but the actual encryption layer was never built.

Today, an operator with shell access on the server can run `nats sub events.>`
or `SELECT payload FROM events_audit` and read every event the server has ever
published in cleartext. That includes whisper content, DM exchanges, and IC
poses inside private scenes. For most MUSH content this is fine — public
poses and presence events are public by design — but for the small set of
events that are intentionally private, it is not.

The goal of this design is to encrypt those sensitive payloads at rest and
in transit on the bus, while preserving operator visibility into event flow
(subjects, actors, timestamps, event types). Operators with host access
SHOULD be able to see *that* events happen and reason about who and when;
they MUST NOT be able to silently read whisper, DM, or private-scene content
through normal admin tooling.

A second goal is to protect content across player-character handoffs. When
a character is taken over by a new player, the new player MUST NOT be able
to read content from the previous player's tenure. Crypto plus access policy
together provide that guarantee.

A third goal is operability without a KMS. Most HoloMUSH deployments are
single-host or small-cluster setups without AWS KMS, GCP KMS, or
DigitalOcean equivalents (DigitalOcean does not currently offer a KMS).
The default deployment MUST work with a passphrase-protected key file or
an OS keyring. Vault is supported as the only practical remote KMS for orgs
that want one.

### Project context

- Single-node deployment, ~200 concurrent users, ~1k events/sec target.
- JetStream cutover complete; codec column exists per row on both tiers.
- ABAC engine (`internal/access/`) with policy DSL is in production use.
- Plugin system supports binary (hashicorp/go-plugin) and Lua (gopher-lua)
  plugins. Plugins consume events and emit events through `EventSink`.
- `holomush-k18g` is in the backlog to migrate plugin-owned `EventType`
  constants out of `internal/core/`. This design re-scopes that bead to
  also carry the per-event-type sensitivity declarations (Section 11.1
  phase 1).
- No KMS dependency — operators provision their own key material.
- DigitalOcean does not offer a native KMS. Cloudflare's Workers Secrets
  is bound to Worker runtime, not externally fetchable. Both are out of
  scope for v1.

### Out of scope (non-goals)

- True end-to-end encryption (clients hold keys; server cannot decrypt).
- Per-player escrow (player-managed master keys).
- Subject obfuscation (opaque subject IDs).
- Right-to-be-forgotten via content-addressed storage with tombstones —
  worth revisiting later, not in v1.
- Defense against an operator who runs `gdb` or reads `/proc/<pid>/mem`
  on the live server. Mitigation cost exceeds benefit.
- Defense against compromise of the master KEK source itself. If the
  master key file or Vault credential is exfiltrated, the attacker reads
  everything sensitive. Forward damage can be limited by KEK rotation;
  past damage cannot be retroactively undone.

### Relationship to substrate spec

The JetStream substrate spec ([2026-04-18](2026-04-18-jetstream-event-log-design.md))
shipped a partial codec API in `internal/eventbus/codec/codec.go`:
`Codec`, `Key`, `KeyID`, `KeyLabel`, `KeyProvider`, `KeySelector`,
`IdentityCodec`. This design **keeps and extends** some of those types
and **supersedes** others.

**Kept (with semantics unchanged):**

- `Codec` interface (`Encode`, `Decode`, `Name`).
- `Key` struct (`{ID KeyID, Bytes []byte}`).
- `KeyID` (`uint64`) — used as `dek_ref` on the bus and in `events_audit`,
  pointing at a `crypto_keys` row.
- `IdentityCodec` and `NoKey` — unchanged behavior for non-sensitive events.
- The `Name` enumeration extends with `xchacha20poly1305-v1` (already
  reserved as a comment in `codec.go:25`).

**Superseded (removed by this design):**

- `KeyLabel` — the substrate's "deployment-policy logical key name"
  (e.g., "scene-content"). This design replaces label-based keying with
  per-context DEKs identified by `(context_type, context_id, version)`.
  A label has at most one current key; this design needs one DEK per
  scene/DM/channel.
- `KeySelector` — the substrate's subject → `(codec, label)` mapper.
  This design replaces it with manifest-driven sensitivity declarations
  (`crypto.emits` per event type) plus per-emit `Sensitive` flag plus
  `DEKManager.GetOrCreate(context_id)` lookup at emit time.
- `KeyProvider.Active(label)` — no analogue. Replaced by
  `DEKManager.GetOrCreate(context_id)`.
- `KeyProvider.ByID(KeyID)` — kept in spirit, renamed to
  `DEKManager.Resolve(keyID KeyID, version uint32) Key`.

When this design lands, `internal/eventbus/codec/codec.go` is updated to
remove `KeyLabel`, `KeySelector`, and the label-side of `KeyProvider`.
The `KeyID` and `Key` types stay as the wire-side identity. The substrate
spec is annotated as "partially superseded" with a back-link to this spec.

---

## Section 1 — Threat model

### Adversaries

| Adversary | What they want | Where they sit |
| --- | --- | --- |
| Curious operator with shell access | Read player whispers, peek into private scenes | Inside the trust boundary; has DB and JetStream access |
| Backup / disk exfiltration | Bulk content | Outside the runtime; has copies of `events_audit` and `crypto_keys` |
| Compromised plugin | Exfiltrate from streams it shouldn't see | Inside the runtime; ABAC-bounded |
| Compromised in-game admin with crypto.operator capability | Trigger destructive operations (Rekey, AdminReadStream) | Inside the auth tier but without shell access |
| Compliance / right-to-be-forgotten | Provably remove content | Legal, not adversarial |

**Threat-model layering for break-glass authentication.** Single-control break-glass authentication uses two factors (admin credentials + TOTP) against the row-134 adversary (curious operator with shell access); the localhost-UDS topology denies network reach for the row-137 adversary (auth-tier compromised admin without shell access), reducing row-137's authentication surface to the same two factors once reach is achieved. Dual-control is the third-factor defense per §5.9 line 1279 (a different operator's credentials + TOTP, mediated by the `admin_approvals` token).

### What's protected

- Whisper, DM, and pemit payloads.
- IC pose payloads inside scenes whose policy declares the scene private.
- Any event whose owning plugin's manifest declares `sensitivity: may` and
  whose emit-site flag was set, or whose manifest declares
  `sensitivity: always`.
- Cold-tier rows (`events_audit` and plugin-owned audit tables) for the
  above. Same codec → same encryption — see INV-21.

### What's deliberately visible

Operators with `nats sub` or `psql` access continue to see:

- Subject (`events.<game>.<domain>.<entity>.<facets>`).
- Headers (`Nats-Msg-Id`, codec name, DEK reference, timestamp).
- Cleartext metadata fields: actor, event type, location, timestamp.
- Public events (presence, location state, public poses, command echoes).

This is intentional. Operators need to debug, monitor flow, and run audit
queries. The cleartext metadata gives them a complete view of *what
happened, who, where, and when* without disclosing the private content.

### What's deliberately NOT defended

- An operator who runs `gdb` against the live server can read plaintext
  out of process memory. Mitigation requires mlock'd buffers and zero-on-free
  hardening; cost exceeds benefit for the typical MUSH operator threat
  envelope. Documented in Section 12 (open questions).
- Compromise of the KEK source. The master key file (or Vault credential)
  is the root of trust. Exfiltration of that root yields everything
  sensitive. We can rotate forward but cannot un-leak old ciphertext that
  the attacker already copied.
- Side-channels in the AEAD codec beyond what the underlying library
  guarantees. We use vetted libraries (`crypto/cipher` + `golang.org/x/crypto`).

---

## Section 2 — Invariants & testable proofs

Every protection claim in this design has a numbered invariant, an RFC2119
statement, a test type, and a verification path. CI MUST fail if any of
these tests fail. A merge that touches this subsystem MUST update tests
and invariants together — never one without the other.

A meta-test (`TestAllInvariantsHaveTests`) enumerates the invariant table
from a single source-of-truth file and asserts every ID has at least one
matching test. Adding an invariant without adding a test fails CI.

Tests live in `internal/eventbus/crypto/` (unit) and
`test/integration/eventbus_crypto/` (integration), organized by invariant ID
for traceability. Each integration test starts with a comment block:

```go
// Verifies: INV-N. <statement>
```

so a reviewer can grep `INV-N` and find the proof.

### Trust-boundary invariants

<a id="inv-1"></a>

| ID | Statement | Test type |
| --- | --- | --- |
| **INV-1** | An operator running `nats sub events.>` against the live JetStream MUST NOT see plaintext for sensitive payloads. | Integration |
| **INV-2** | A direct `SELECT payload FROM events_audit` MUST NOT yield plaintext for rows whose `codec != identity`. | Integration |
| **INV-3** | A backup of `events_audit` and `crypto_keys`, restored without the master KEK, MUST NOT yield plaintext for sensitive content. | Integration |
| **INV-4** | A wrapped DEK in `crypto_keys` MUST NOT be unwrappable without the master KEK. | Unit |

### Sensitivity declaration invariants

| ID | Statement | Test type |
| --- | --- | --- |
| **INV-5** | An event with `Sensitive=true` at emit MUST be encrypted before reaching the bus. | Unit + Integration |
| **INV-6** | Emitting an event with `Sensitive=true` whose type is not declared in `crypto.emits` as `may` or `always` MUST fail with `oops.Code("EVENT_SENSITIVITY_NOT_DECLARED")`. | Unit |
| **INV-7** | A plugin declaring an event type as `sensitivity: always` MUST NOT publish that event with `Sensitive=false`. | Unit |

### Participant access invariants

| ID | Statement | Test type |
| --- | --- | --- |
| **INV-8** | A subject in a DEK's participant set MUST receive plaintext via fan-out when policy permits. | Integration |
| **INV-9** | A subject NOT in a DEK's participant set MUST NOT receive plaintext via fan-out, even when subscribed to the matching subject. | Integration |
| **INV-10** | After player rebind on character C, the new player MUST NOT receive plaintext for events emitted before the rebind. | Integration |
| **INV-11** | After player rebind on character C, the wrapped DEK row(s) representing the previous player's tenure MUST be preserved (not destroyed). The previous player MAY retrieve plaintext only via a player-kind subscription path (see §7.2 Branch 2) authorized by `participant.player_id` membership; that path is gated by ABAC and emits a `audit.<game>.system.player_history_read` event per session. | Integration |

### Lifecycle invariants

| ID | Statement | Test type |
| --- | --- | --- |
| **INV-12** | `Add(participant)` MUST grant immediate read access to all existing DEK history without rotating the DEK. | Integration |
| **INV-13** | `Rotate(context)` MUST preserve the old DEK ciphertext and the old DEK record unchanged. Holds under Phase 3c soft-delete (Rotate does not touch `destroyed_at`). | Unit + Integration |
| **INV-14** | `Rekey(context)` MUST re-encrypt historical ciphertext under the new DEK and soft-delete the old DEK record (`destroyed_at = NOW()`); production reads filter `destroyed_at IS NULL` so the operational effect is identical to hard-delete. | Integration |
| **INV-15** | `Rekey` MUST emit a system audit event with operator identity, context, and justification, on an `audit.>` subject. ABAC denies subscribe to `audit.>` for `kind={plugin\|character}` subjects at the gRPC subscribe handler boundary (§7.7). | Integration |
| **INV-16** | `Rekey` MUST NOT be invocable from any in-game command surface or any public gRPC service. | Static + Integration |

### Plugin authorization invariants

| ID | Statement | Test type |
| --- | --- | --- |
| **INV-17** | A plugin without manifest `requests_decryption` for an event class MUST receive metadata-only delivery for events of that class, regardless of subject subscription. | Integration |
| **INV-18** | A plugin with manifest declaration but without active ABAC grant MUST receive metadata-only. | Integration |
| **INV-19** | Every plugin decryption MUST emit an audit event on a subject the plugin cannot subscribe to. | Integration |
| **INV-20** | A plugin authorization failure MUST NOT block fan-out to other recipients. | Integration |

### Cold-tier invariants

<a id="inv-21"></a>

| ID | Statement | Test type |
| --- | --- | --- |
| **INV-21** | `events_audit.envelope` MUST be byte-equal to the marshaled `Event` proto envelope on the bus for sensitive events (renamed from the `payload` column in migration `000017_events_audit_envelope_rename`; the column always carried the marshaled envelope bytes, not just the inner Event.payload field — see §4.7). | Integration |
| **INV-22** | A `QueryStreamHistory` call MUST apply the same participant-set + ABAC checks as live subscription. | Integration |

### Provider invariants

| ID | Statement | Test type |
| --- | --- | --- |
| **INV-23** | Switching crypto providers MUST NOT require re-encrypting payload ciphertext; only wrapped DEKs are re-wrapped. | Integration |
| **INV-24** | KEK rotation MUST be performable while the system is live, with zero downtime for non-encryption operations. | Integration |

### AAD-binding invariant

| ID | Statement | Test type |
| --- | --- | --- |
| **INV-25** | An event whose cleartext metadata, codec, or `dek_ref` has been altered MUST fail decryption with a tag-mismatch error and MUST NOT yield plaintext. | Unit + Integration |

### Delivery contract invariant

| ID | Statement | Test type |
| --- | --- | --- |
| **INV-26** | A recipient denied decryption MUST receive the event with `metadata_only=true`, empty `payload` bytes, populated cleartext metadata, and no ciphertext. | Integration |

### Cache invariants

| ID | Statement | Test type |
| --- | --- | --- |
| **INV-27** | The `dek.Material` type (an opaque wrapper for unwrapped DEK bytes, defined in `internal/eventbus/crypto/dek/material.go`) MUST NOT be passed to: any `io.Writer`, any `encoding/json`/`encoding/gob`/`google.golang.org/protobuf/proto.Marshal` call, any `slog`/`log` function, any `fmt.Sprint*`/`fmt.Print*`/`fmt.Errorf` argument, any function returning `[]byte` other than the codec's `Encode`/`Decode`. Enforced by a `gocritic` ruleguard rule (`gorules/dek_no_serialize.go`) that fails `task lint` on any matching call site. | Lint + Unit |
| **INV-28** | A KEK rotation operation MUST issue a NATS request-reply ping on `internal.<cluster_id>.cache_invalidate.dek.kek_rotation` (cluster-prefixed per INV-54) and MUST receive responses from N-of-N expected replicas (count derived from `cluster.Registry.LiveMembers()` snapshot at publish time) before returning success. Members MUST drop messages whose payload `cluster_id` disagrees with their configured cluster_id. Timeout = 30s; on timeout the rotation is rolled back and the operator is told which replicas did not respond. | Integration |
| **INV-29** | A `Rotate(context)` or `Rekey(context)` operation MUST issue a NATS request-reply ping on `internal.<cluster_id>.cache_invalidate.dek.<ctx_type>.<ctx_id>` (cluster-prefixed per INV-54) and MUST receive N-of-N replica acks before the operation returns. Single-replica deployments degenerate to N=1 (the local replica acks itself); the contract is identical. Members MUST drop messages whose payload `cluster_id` disagrees with their configured cluster_id. Timeout = 5s; on timeout the operation is rolled back. | Integration |

### Cluster coordination invariants

These invariants land in Phase 3c (`holomush-ojw1.3`) and define the multi-replica
substrate that sits beneath INV-28 / INV-29. See
[`docs/superpowers/specs/2026-05-02-event-payload-crypto-phase3c-grounding.md`](2026-05-02-event-payload-crypto-phase3c-grounding.md)
for full rationale (Decisions 1–8).

| ID | Statement | Test type |
| --- | --- | --- |
| **INV-53** | Every member of a `cluster.Registry` MUST have a unique `MemberID`; concurrent registration with a colliding MemberID MUST be rejected with `CLUSTER_MEMBER_DUPLICATE_ID`. | Unit |
| **INV-54** | All Phase 3c internal coordination subjects (`internal.<cluster_id>.member.*`, `internal.<cluster_id>.cache_invalidate.*`) MUST be prefixed with `<cluster_id>`. Members MUST drop messages whose payload `cluster_id` field disagrees with their configured cluster_id. | Integration |
| **INV-55** | A pill received on `internal.<cluster_id>.member.poison.<self_id>` MUST cause the receiving member to call `Pill.Trigger(ctx, reason, sourceID)` after flushing audit telemetry; production `Pill` impl MUST terminate the process with exit code 125. | Unit (with TestPill) + e2e (with ProductionPill in supervised harness) |
| **INV-56** | The `invalidation.Coordinator` MUST attempt at most one probe-and-pill + retry cycle per `RequestInvalidation` call. After the second timeout, the call MUST return `INVALIDATION_PARTIAL_FAILURE` carrying the missing-member set; further retry is the caller's choice. | Unit |
| **INV-57** | `cluster.Registry.ProbeAndPill(ctx, id, reason)` MUST NOT issue more than one *attempt* per `(member_id, reason)` per `cluster.Config.PillRateLimit` window (default 60s). The rate-limit slot is claimed BEFORE the probe; a successful probe (no pill issued) still consumes the slot. Subsequent attempts within the window MUST return `ErrPillRateLimited` and not reach the wire. The rate-limit gates ALL consumers of `ProbeAndPill`, not only Coordinator. Rationale: the claim-then-probe pattern closes a TOCTOU race where two concurrent ProbeAndPill calls could both pass the check, both probe, and both publish a pill. (See `internal/cluster/probe_pill.go:67-87` for the in-code rationale; this is a strict superset of the spec's safety guarantee — every config the spec forbids is also forbidden under attempts-based gating.) | Unit |
| **INV-58** | The Phase 3c protocol MUST NOT condition any decision on cross-host wall-clock comparison. Enforced by the `gorules/analyzers/noremoteclockcompare/noremoteclockcompare.go` analyzer (a `golang.org/x/tools/go/analysis` rule wired into golangci-lint via the project's custom-gcl plugin) that fails `task lint` on any subtraction or comparison where one operand is a `time.Time` resolved through a remote-sourced struct field (heartbeat `PublishedAt`, invalidation `IssuedAt`, pill `IssuedAt`, member `StartedAt`, `LastHeartbeatAt`). The skew-detection metric (Decision 8) and the invalidation latency histogram are the carved-out exceptions, each gated by `// nolint:noremoteclockcompare // observability-only per Decision 8` (note: the lint name on the directive is the analyzer's registered single-word identifier `noremoteclockcompare`, not the underscored `no_remote_clock_compare`). | Lint |
| **INV-59** | A successful `Coordinator.RequestInvalidation(ctx, ctxID, ActionParticipantsChanged)` MUST result in every other live member's `dek.ParticipantsCache` having no entry for `(ctxType, ctxID, version)` upon return. Equivalently: after `RequestInvalidation` returns nil for `participants_changed`, every other replica's next `dek.Manager.Participants(keyID, version)` call for that `(ctxID, version)` MUST re-fetch from PG. This is the correctness substrate that supports INV-12 ("Add MUST grant immediate read access to all existing DEK history without rotating the DEK") under Phase 3c full-scope participant caching. Phase 4's `Add(participant)` caller invokes the substrate; Phase 3c ships the substrate property and tests it via the multi-Registry harness without a production Add caller. | Integration |
| **INV-60** | `cluster.Registry.ProbeAndPill(ctx, id, reason)` MUST refuse `id == r.Self()` and return `ErrCannotPillSelf` without issuing a probe or pill. Coordinator's missed-ack handler MUST filter `r.Self()` out of the missing-member set before calling `ProbeAndPill`. On single-replica deployments (N=1), this prevents the local Coordinator from self-pilling when the local invalidation handler hangs; the operator-facing failure mode becomes `INVALIDATION_PARTIAL_FAILURE` with a single-member `missing` set, surfaced as a structured WARN log + `cluster_self_timeout_total` metric increment. | Unit |

### Provider implementation invariants

| ID | Statement | Test type |
| --- | --- | --- |
| **INV-30** | `Wrap` followed by `Unwrap` with the returned `keyID` MUST recover the original DEK byte-for-byte. | Unit (per provider) |
| **INV-31** | Provider migration MUST NOT modify any byte in `events_audit.payload` or any JetStream message body. | Integration |
| **INV-32** | A startup with `provider.name=none` MUST refuse if any `crypto_keys` row exists. | Unit + Integration |
| **INV-33** | A startup with provider X MUST refuse if any `crypto_keys` row's `wrap_provider` is not unwrapable by X. | Unit + Integration |
| **INV-34** | A `NoneProvider` MUST refuse `Wrap` and MUST cause emit-time failure for any event with `Sensitive=true`. | Unit |
| **INV-35** | Provider `HealthCheck` failure MUST NOT prevent reads of cached DEKs. | Integration |

### Operation correctness invariants

| ID | Statement | Test type |
| --- | --- | --- |
| **INV-36** | Concurrent `Add` operations on the same context MUST be serialized; no participant entry is duplicated; resulting participants list is exactly the set-union. | Integration |
| **INV-37** | A crashed `Rotate` MUST be resolvable by startup integrity check without manual intervention. | Integration |
| **INV-38** | A `Rekey` MUST be resumable from any checkpoint without producing duplicate ciphertext or skipped rows. | Integration |
| **INV-39** | Reads of historical events whose `dek_ref` no longer exists in `crypto_keys` MUST automatically fall back to the cold tier. Production read paths return NoRows for soft-deleted rows (`destroyed_at IS NOT NULL`), hitting the same fallback path. | Integration |
| **INV-40** | `Rekey` MUST NOT be invocable over any public gRPC service or any in-game command. | Static + Integration |

### Plugin authorization correctness invariants

| ID | Statement | Test type |
| --- | --- | --- |
| **INV-41** | A plugin emitting `Sensitive=true` for a context where it lacks an `emit_sensitive` ABAC grant MUST be rejected at the EventSink with a typed error; the event MUST NOT reach the bus. | Integration |
| **INV-42** | `AdminReadStream` MUST emit its audit event before delivering any plaintext data; if audit emit fails, the call MUST refuse. | Integration |
| **INV-43** | The runtime AuthGuard MUST NEVER return PERMIT for a subject of kind `operator`; legitimate operator reads go through `AdminReadStream`. | Unit |
| **INV-44** | Granting a plugin `emit_sensitive` does NOT imply `decrypt`, and vice versa. The two ABAC actions are independent. | Unit + Integration |

### Manifest registry invariant

| ID | Statement | Test type |
| --- | --- | --- |
| **INV-45** | A plugin's `crypto.consumes.requests_decryption` MUST only reference event types qualified as `<plugin_name>:<event_type>` where `<plugin_name>` is in the consumer plugin's `requires` declaration AND the referenced plugin's `crypto.emits` declares the event type as `may` or `always` sensitive. Loader MUST reject manifests violating this. | Unit + Integration |

### Plugin pass-through invariants

| ID | Statement | Test type |
| --- | --- | --- |
| **INV-46** | Plugin-owned audit projection consumers MUST NOT decrypt or modify the `payload` and `codec` fields received from the bus. Plugin audit tables MUST contain byte-equal payload to the bus event. | Integration |
| **INV-47** | Plugin `PluginAuditService.QueryHistory` MUST return rows with the original ciphertext payload, codec, `dek_ref`, and `dek_version` unchanged. | Integration |
| **INV-48** | The host's `QueryStreamHistory` handler MUST refuse a row from a plugin where `codec != identity` arrives without a matching `dek_ref` (or with a `dek_ref` not present in `crypto_keys`). | Integration |
| **INV-50** | The host's `QueryStreamHistory` handler MUST refuse a row from a plugin where `codec = identity` arrives for an event whose `type` field matches a manifest-declared `sensitivity: always` event type. The host maintains the always-sensitive event-type set from loaded manifests and uses it as the ground truth. Defeats a malicious or buggy plugin that returns plaintext for sensitive events. | Integration |
| **INV-51** | A player-kind subscription that resolves a DEK via `participant.player_id` membership (the previous-tenure read path) MUST emit `audit.<game>.system.player_history_read` once per session-context pair, before the first plaintext event is delivered. | Integration |
| **INV-52** | Retired per Phase 3d Decision 4 — see §7.7 amendment. Game-topic NATS is single-principal by architectural design; no NATS account-level deny rule has a target. | Retired |

### Phase 3a enforcing tests

The Phase 3a slice (host-side primitives, sensitivity fence, and AAD-bound
codec) lands the following invariants. Each is wired to a concrete enforcing
test that fails if the invariant is violated:

| Invariant | Enforcing test |
| --- | --- |
| **INV-6** | `internal/plugin/sensitivity_fence_test.go::TestEnforceSensitivity` (subtest `"never + claim=true → INV-6 reject"`) |
| **INV-7** | `internal/plugin/sensitivity_fence_test.go::TestEnforceSensitivity` (subtest `"always + claim=false → INV-7 reject"`) |
| **INV-21** | `test/integration/crypto/emit_test.go::TestSensitiveEmitProducesCiphertextOnBusAndInAudit` (byte-equality assertion on bus payload vs `events_audit.payload`) |
| **INV-25** | `internal/eventbus/codec/xchacha20poly1305_test.go::TestXChaCha20Poly1305DetectsAADTamper` |
| **INV-49** | `internal/eventbus/audit/projection_test.go::TestPersistWritesDekColumnsFromHeaders` |

---

## Section 3 — Architecture overview

### Components

| Component | Responsibility | Process / location |
| --- | --- | --- |
| `CryptoProvider` (interface) | Wrap and unwrap DEKs using the master KEK; never sees event payloads | In-process |
| `LocalAEADProvider` (default) | Local AEAD with pluggable `KEKSource` for the master KEK | In-process |
| `KEKSource` (interface) | Fetch and refresh master KEK material from a backing store | In-process |
| `VaultTransitProvider` | True remote KMS via Vault Transit engine | In-process; talks to Vault |
| `NoneProvider` | Dev sentinel that refuses encryption | In-process |
| `DEKManager` | Mint, lookup, cache, and lifecycle DEKs | In-process |
| `CryptoKeysStore` | Persist wrapped DEKs and participant sets | PostgreSQL `crypto_keys` table |
| Codec registry | `IdentityCodec`, `aead-xchacha20-poly1305-v1`, future variants | In-process |
| `EventSink` (existing, extended) | Enforce manifest sensitivity; encrypt payload at emit | In-process |
| Subscribe / `QueryStreamHistory` paths | Resolve DEK; gate via AuthGuard; decrypt or deliver metadata-only | In-process |
| `AuthGuard` | Combine DEK participant-set check + ABAC grant + plugin manifest declaration | In-process |
| `DecryptAuditEmitter` | Emit `audit.<game>.plugin_decrypt.*` and `audit.<game>.system.*` events | In-process; publishes to a dedicated audit subject namespace |
| `holomush crypto …` (CLI) | Operator-only commands: `rekey`, `kek-rotate`, `provider-migrate`, `key-status` | Separate binary; authenticates against host |
| `holomush admin read-stream` (CLI) | Operator break-glass for legitimate sensitive reads | Same binary; same admin socket |

### Trust boundaries

| Boundary | What lives outside | What lives inside |
| --- | --- | --- |
| Host filesystem / OS | Master key material (encrypted file, OS keyring secret, Vault credential) | Server process |
| Server process | NATS JetStream daemon, PostgreSQL, plugin subprocesses | EventSink, Subscribe handlers, DEKManager, codecs, AuthGuard |
| JetStream stream storage | Wrapped DEKs (in PostgreSQL), plaintext payloads (never present) | Ciphertext payloads + cleartext metadata + headers |
| `crypto_keys` table | Master KEK | Wrapped DEKs only; cleartext participant sets (intentional — see Section 4.3) |
| `events_audit` table | Master KEK, DEKs | Ciphertext payloads (codec column says how to decrypt) |
| Plugin processes (binary or Lua VM) | Master KEK, DEKs, plaintext for events outside their grant | Plaintext only for events the AuthGuard permits |
| Telnet / web client | All key material | Plaintext for events the subscribing character is a participant in |

### Emit flow (sensitive event)

```text
Caller (host or plugin)
  └── EventSink.Emit(event, Sensitive=true, ContextID=scene:01ABC)
        ├── Manifest validator: event_type ∈ may_be_sensitive   (INV-6, INV-7)
        ├── DEKManager.GetOrCreate(scene:01ABC)
        │     ├── CryptoKeysStore: SELECT or INSERT
        │     └── On INSERT: generate DEK, CryptoProvider.Wrap(DEK, KEK), persist
        ├── Codec.Encrypt(payload, DEK, AAD=metadata)
        └── EventBus.Publish(event{
              subject:      events.<game>.scene.01ABC.ic,
              metadata:     {actor, event_type, location, timestamp, ...},
              codec:        aead-xchacha20-poly1305-v1,
              dek_ref:      crypto_keys.id,
              dek_version:  N,
              payload:      <ciphertext>,
            })
              ├── JetStream: stored ciphertext
              └── AuditProjection: events_audit row mirrors bus event byte-for-byte (INV-21)
```

### Subscribe / fan-out flow

```text
Recipient (telnet | web | plugin) opens Subscribe(stream)
  └── For each event:
        ├── If codec == IdentityCodec:
        │     └── deliver payload as-is
        └── Else (sensitive):
              ├── DEKManager.Resolve(dek_ref, dek_version) → unwrapped DEK (cached LRU)
              ├── AuthGuard.Check(subject, DEK.participant_set, plugin_manifest, ABAC)
              ├── If permit:
              │     ├── Codec.Decrypt(ciphertext, DEK, AAD=metadata)
              │     ├── If recipient is a plugin:
              │     │     └── DecryptAuditEmitter.Emit(audit.plugin_decrypt.<plugin>) (INV-19)
              │     └── deliver plaintext payload
              └── If deny:
                    └── deliver event with metadata_only=true, empty payload (INV-9, INV-17, INV-18)
```

### Cold-tier (`QueryStreamHistory`) flow

Same as Subscribe — same codec dispatch, same AuthGuard, same decrypt-or-metadata-only.
The hot/cold tier crossover from F4 already exists; this design adds nothing
tier-specific. If it works for the bus, it works for the audit projection by
construction (INV-21 ensures the bytes are identical).

### Rekey flow (CLI-only)

```text
Operator on host shell:
  $ holomush crypto rekey --context scene:01ABC \
      --justification "Banned user retroactive access removal, ticket #1234" \
      [--dual-control]
        ├── CLI authenticates: file-perm check on key file OR systemd credential OR Vault auth
        ├── If --dual-control: prompt for second operator's signed approval within 5 min
        ├── DEKManager.Rekey(context_id, justification, operator_id)
        │     ├── Mint DEK_new
        │     ├── PG: SELECT events_audit rows WHERE dek_ref = DEK_old
        │     ├── For each: Decrypt(DEK_old) → Encrypt(DEK_new) → UPDATE
        │     ├── JetStream: hot-tier rolls off naturally; reads fall back to cold tier (INV-39)
        │     ├── CryptoProvider: destroy DEK_old wrapped record
        │     └── DecryptAuditEmitter.Emit(audit.system.rekey.<context>) (INV-15)
        └── Returns rekey audit event ID for operator's records
```

### What's deliberately NOT in this picture

- No client-side decryption. Telnet and web clients receive plaintext over
  gRPC; the server is in the trust path.
- No subject obfuscation. Subjects stay structural and operator-visible.
- No DEK distribution to plugins. Plugins never hold a DEK directly. They
  receive plaintext from the server's decrypt-and-deliver path, gated by
  AuthGuard. Plugin compromise leaks plaintext for events the plugin is
  currently authorized for; it cannot retroactively unlock anything outside
  its grant.
- No automatic Rekey on participant change. Rotation handles routine cases;
  Rekey is reserved for compromise and forced revocation.

---

## Section 4 — Data model

### 4.1 Event envelope

The current `eventbus.Event` proto (`api/proto/holomush/eventbus/v1/eventbus.proto`)
already separates cleartext metadata (top-level fields) from the opaque
`payload` bytes. This design adds **one** proto field and uses **NATS
headers** for the rest, matching the existing convention where `codec`
already lives in NATS headers (`HeaderCodec = "App-Codec"` per
`internal/eventbus/publisher.go:35-36`), not in the proto.

**Current proto (unchanged structure):**

```protobuf
message Event {
  bytes id = 1;                              // ULID (16 bytes) — cleartext
  string subject = 2;                        // cleartext
  string type = 3;                           // cleartext, format: "<plugin>:<event_type>"
  google.protobuf.Timestamp timestamp = 4;   // cleartext
  Actor actor = 5;                           // cleartext
  bytes payload = 6;                         // codec.Encode output;
                                             // cleartext when codec=identity,
                                             // ciphertext otherwise
}
```

The "metadata" of an event is everything in the proto Event *except*
`payload`. There is no separate `EventMetadata` message — a reviewer
inspecting the proto sees the cleartext/ciphertext boundary at the
`payload` field. Operator visibility (subject, type, timestamp, actor) is
preserved by the existing proto layout.

**Single new proto field:**

```protobuf
message Event {
  // ... existing fields 1-6 ...
  bool metadata_only = 7;  // delivery flag set by the host's Subscribe /
                            // QueryStreamHistory handler when AuthGuard
                            // denies plaintext to this recipient.
                            // When true, payload is empty bytes (NOT
                            // ciphertext).
                            // Set by host before sending to client; never
                            // set by emitter and never persisted to
                            // events_audit (storage rows always have
                            // metadata_only=false).
}
```

**NATS headers (added by EventSink at publish; copied into `events_audit`
columns by the audit projection):**

| Header | Purpose | Existing or new |
| --- | --- | --- |
| `App-Codec` | Codec name (`identity`, `xchacha20poly1305-v1`, …) | Existing |
| `App-Schema-Ver` | Payload schema version | Existing |
| `App-Dek-Ref` | `KeyID` as decimal string; pointer to `crypto_keys.id` | NEW — empty for `identity` |
| `App-Dek-Version` | Per-context DEK version (1, 2, 3, …); diagnostic | NEW — empty for `identity` |
| `Nats-Msg-Id` | JetStream dedup; the event ULID | Existing |

**Why headers instead of proto fields for `dek_ref`/`dek_version`:** the
current substrate already puts `codec` in headers. The audit projection
already reads the `codec` header and writes it to a column. Adding two
more headers and two more columns follows the same shape — minimal proto
churn, no breaking change for clients reading old plaintext events,
byte-equality with cold-tier rows is enforced at the *header + payload*
level rather than the proto level.

The cold-tier audit row stores these headers as columns (see §4.7).
For sensitive events, the bus message and the `events_audit` row are
byte-equal in payload bytes and value-equal across (codec, dek_ref,
dek_version) — INV-21.

### 4.2 AAD binding

The AEAD codec MUST construct Additional Authenticated Data (AAD)
deterministically from cleartext fields so any tampering breaks decryption.
The canonicalization function lives in a single package
(`internal/eventbus/crypto/aad`) and is shared by both `Encode` and `Decode`
implementations.

**Canonicalization rule (concrete):**

```go
// AAD layout (concatenated bytes, no separators within a field):
//
//   "HMAAD\x01"            // 6-byte magic + version
//   uint32(len(event.id))  // 4 bytes big-endian
//   event.id               // ULID, 16 bytes
//   uint32(len(event.subject))
//   event.subject          // UTF-8 bytes
//   uint32(len(event.type))
//   event.type             // UTF-8 bytes
//   int64(event.timestamp.AsTime().UnixNano()) // 8 bytes big-endian
//   uint32(len(actor_bytes))
//   actor_bytes            // proto.MarshalOptions{Deterministic: true}
//                           //   over event.actor
//   uint32(len(codec_name))
//   codec_name             // UTF-8 bytes (header value)
//   uint64(dek_ref)        // 8 bytes big-endian (0 for identity codec)
//   uint32(dek_version)    // 4 bytes big-endian
```

This is a hand-rolled canonical form rather than a single proto Marshal:
proto3's `Deterministic` option is opt-in and has caveats around unknown
fields and map ordering, and we want explicit control over byte ordering
across language implementations (the Lua plugin host doesn't speak proto
natively).

**Properties:**

- Tampering with any cleartext field, the codec name, or the DEK reference
  changes AAD bytes → AEAD tag check fails on decrypt → no plaintext yield.
- Identical bytes on both sides because both call `BuildAAD(event, codec, dekRef, dekVersion)`.
- Versioned (`HMAAD\x01`); a future v2 layout coexists by checking magic.

Verified by INV-25.

### 4.3 `crypto_keys` table

```sql
CREATE TABLE crypto_keys (
    id              BIGSERIAL PRIMARY KEY,      -- maps to substrate codec.KeyID (uint64)
    context_type    TEXT NOT NULL,              -- 'scene' | 'dm' | 'channel' | 'character' | 'player'
    context_id      TEXT NOT NULL,              -- ULID (text) or sorted-pair canonical form
    version         INTEGER NOT NULL,           -- 1, 2, 3, ... (per-context)
    wrapped_dek     BYTEA NOT NULL,             -- DEK encrypted under master KEK by wrap_provider
    wrap_provider   TEXT NOT NULL,              -- 'local-aead/file' | 'local-aead/keyring' | 'vault-transit' | ...
    wrap_key_id     TEXT NOT NULL,              -- provider-specific KEK identifier
    participants    JSONB NOT NULL,             -- [{player_id, character_id, binding_id, joined_at}, ...]
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    rotated_at      TIMESTAMPTZ,                -- nullable; set when superseded
    superseded_by   BIGINT REFERENCES crypto_keys(id),
    rekey_audit_id  BYTEA,                      -- nullable ULID; if non-null, this DEK was the old half of a Rekey
    UNIQUE (context_type, context_id, version)
);

CREATE INDEX crypto_keys_context ON crypto_keys (context_type, context_id);
CREATE INDEX crypto_keys_active  ON crypto_keys (context_type, context_id) WHERE rotated_at IS NULL;
```

`id BIGSERIAL` matches the substrate's `codec.KeyID = uint64` directly.
The wire-side `App-Dek-Ref` header carries this `id` as a decimal string;
audit-tier `events_audit.dek_ref BIGINT` stores it natively. `crypto_keys.id`
**is** the `KeyID` for routing through the codec API.

`rekey_audit_id` uses `BYTEA` (16-byte ULID) to match the existing
event-id convention in `events_audit.id`.

Participant entries:

```json
[
  {
    "player_id":    "01HXXX...",
    "character_id": "01HXXX...",
    "binding_id":   "01HXXX...",
    "joined_at":    "2026-04-25T12:34:56Z",
    "added_via":    "scene_join"
  }
]
```

`wrap_provider` and `wrap_key_id` separation makes provider migration safe.
A row written by `local-aead/keyring` uses one unwrap path; a row written by
`vault-transit` uses another. Migration to a new provider re-wraps DEKs and
updates these columns row-by-row; no payload re-encryption.

`participants` is cleartext because the AuthGuard needs to evaluate set
membership *before* knowing whether to unwrap. Encrypting the participant
list would create a chicken-and-egg dependency. The cleartext list reveals
"Alice and Bob shared a private context" but not what they said — the leak
the threat model accepts.

### 4.3a `player_character_bindings` table

A **binding** is a player's tenure on a character: the time-bounded
relationship that begins when a player gains control (initial character
assignment, wizard handoff) and ends when control is released (wizard
transfer to a different player, character deletion, voluntary release).
Binding lifetimes are weeks to months and span many ephemeral game
sessions; a player disconnecting and reconnecting does NOT end the binding.

`binding_id` is the load-bearing identifier in `crypto_keys.participants[].binding_id`
and in the §7.2 Branch 1 (character) AuthGuard decision tree. Without a
stable, time-bounded binding identifier, AuthGuard cannot distinguish
"same character, different player tenure" — and INV-10 ("after player
rebind, the new player MUST NOT receive plaintext for events emitted
before the rebind") cannot hold.

The binding entity is **substrate, not crypto-specific**. Crypto is its
first consumer; future audit trails ("character X was transferred from
player A to player B at time T, reason R"), per-binding permissions, and
binding-scoped quotas all naturally resolve against the same table.

```sql
CREATE TABLE player_character_bindings (
    id            TEXT PRIMARY KEY,             -- ULID; THE binding_id
    player_id     TEXT NOT NULL REFERENCES players(id),
    character_id  TEXT NOT NULL REFERENCES characters(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at      TIMESTAMPTZ,                  -- NULL = active; non-null = previous tenure
    ended_reason  TEXT                          -- 'wizard_transfer' | 'release' | 'character_deletion'
);

CREATE UNIQUE INDEX idx_pcb_active_per_character
    ON player_character_bindings (character_id) WHERE ended_at IS NULL;

CREATE INDEX idx_pcb_player_active
    ON player_character_bindings (player_id) WHERE ended_at IS NULL;
```

**Lifecycle:**

- **First migration (back-population for existing characters):** the
  CREATE TABLE migration MUST also run a one-shot
  `INSERT INTO player_character_bindings (id, player_id, character_id, created_at)
  SELECT generate_ulid()::TEXT, player_id, id, now() FROM characters WHERE player_id IS NOT NULL`
  to seed bindings for every character with a known player. Characters
  with `player_id IS NULL` (orphans permitted by the existing baseline
  schema's nullable FK) are excluded from back-population — they have
  no active binding. Subscribe against an orphan character returns
  `BINDING_MISSING` until the orphan is bound (Phase 4 wizard-transfer)
  or deleted.
- **Character creation (initial bind):** in the same transaction that
  inserts the `characters` row, INSERT a binding row for `(player_id,
  character_id)` with `created_at = now()`, `ended_at = NULL`. Phase 3b
  achieves this by switching `CharacterRepository.Create` to use
  `execerFromCtx` (matching the existing `Delete` pattern at
  `internal/world/postgres/character_repo.go:76`), then having the
  gRPC `CreateCharacter` handler open a transaction and call both
  repository methods inside it.
- **Wizard transfer (rebind):** in one transaction: UPDATE the existing
  active binding row, setting `ended_at = now()` and `ended_reason =
  'wizard_transfer'`; INSERT a new binding row for `(new_player_id,
  character_id)`. Trigger `Rotate` on every DEK whose participants
  include the old `binding_id`.
- **Character deletion:** UPDATE the active binding setting `ended_at =
  now()` and `ended_reason = 'character_deletion'`. Historical bindings
  remain queryable for forensics; do NOT cascade-delete them.

**AuthGuard binding lookup:** at gRPC Subscribe / QueryStreamHistory
time, the handler queries
`SELECT id FROM player_character_bindings WHERE character_id = $1 AND ended_at IS NULL`
for the session's character. The result is the `binding_id` stamped on
`authguard.Identity`. A unique index on `(character_id) WHERE ended_at
IS NULL` enforces "exactly one active binding per character."

**`Add(participant)` binding population:** when `Add` runs (scene join,
channel invite, DM auto-create), the calling code calls
`Bindings.Current(character_id)` and uses the returned `binding_id` when
appending to `crypto_keys.participants[]`. The participant record then
carries the historical binding identity even after that binding ends.

**`Rotate` trigger on rebind (§6.2):** wizard transfer commits two
effects in one transaction: ending the old binding (this section) AND
triggering `Rotate` on every DEK whose participant set includes the old
`binding_id` (§6.2). The Rotate machinery is Phase 4; the binding-table
substrate ships in Phase 3b so AuthGuard has a `binding_id` source to
consult from day one.

**Forensics:** historical bindings remain in the table indefinitely with
`ended_at != NULL`. The participant-record `binding_id` references in
`crypto_keys.participants[]` continue to identify the historical tenure
even after the binding row is marked ended. "Who was bound to character
X on date Y" resolves to a single SELECT.

**Why a separate table rather than columns on `characters`:**

- Columns on `characters` lose history on UPDATE — only the current
  binding's identity survives. Phase 4 wizard-transfer audit needs the
  history; INV-10 + INV-11 forensics need it.
- Columns on `characters` cannot represent the brief overlap window
  during transfer (old binding ending + new binding starting in one
  transaction); a two-row transition in `player_character_bindings`
  makes the transition atomic and observable.
- Forward-compat with binding-level features (per-binding permissions,
  binding-scoped quotas, transfer history surfacing in admin UIs) — all
  natural if the binding is a first-class entity.

### 4.4 Codec naming convention

The substrate already defines `codec.Name` as a closed enumeration in
`internal/eventbus/codec/codec.go:18-26`. This design extends it. Names
follow the convention already reserved by code comments:

```go
const (
    NameIdentity        Name = "identity"
    NameXChaCha20v1     Name = "xchacha20poly1305-v1"  // NEW (already commented in codec.go:25)
    NameAESGCMv1        Name = "aes-gcm-v1"             // NEW (already commented in codec.go:24)
)
```

Codecs register at startup in the closed registry. Unknown codec on read
returns a typed error; no decrypt attempt.

**Default for sensitive content: `xchacha20poly1305-v1`.**

- Misuse-resistant (large nonce reduces collision risk under random nonces).
- No hardware acceleration dependency; consistent perf on any host.
- Battle-tested (libsodium, age, WireGuard).

`aes-gcm-v1` is provided as an alternative for deployments with AES-NI
and a preference for FIPS-aligned algorithms. Codec choice is per-row, so
a deployment can switch defaults forward without rewriting history.

### 4.5 Metadata-only delivery

When AuthGuard denies decryption, the recipient receives the event with:

- `payload` = empty bytes. Recipients never see ciphertext — that would
  invite local brute-force attempts in the recipient's process.
- `metadata_only = true`.
- `metadata` populated as normal (cleartext metadata IS the delivery).
- `codec` and `dek_ref` populated so the recipient can log "this was a
  sensitive event under DEK X" for diagnostic purposes.

Plugin contract (documented in `site/docs/extending/event-sensitivity.md`):

> Every event delivered to a subscriber has a `metadata_only` boolean. When
> `true`, the `payload` bytes are empty and the event was either not
> authorized for plaintext delivery to your plugin, or your plugin did not
> request decryption for this event type. Always check `metadata_only`
> before processing `payload`.

### 4.6 Audit event shapes

Two audit subject namespaces. These streams are themselves NEVER sensitive
(always plaintext with IdentityCodec) and live on subjects that the targeted
plugins and contexts CANNOT subscribe to.

**`audit.<game>.plugin_decrypt.<plugin_name>`** — emitted on every plugin
decryption:

```yaml
metadata:
  actor:        {kind: plugin, id: <plugin_name>, instance_id: <inst>}
  event_type:   "system:plugin_decrypt"
  timestamp:    <now>
payload (cleartext):
  decrypted_event_id:   <event ID, ULID>
  decrypted_subject:    <original subject>
  context_type:         <scene|dm|channel|character>
  context_id:           <ID, ULID or sorted-pair>
  dek_ref:              <int64; matches crypto_keys.id and substrate codec.KeyID>
  dek_version:          <int>
  grant_id:             <ABAC grant ID that authorized this, ULID>
```

**`audit.<game>.system.rekey.<context_type>.<context_id>`** — emitted on
every `Rekey`:

```yaml
metadata:
  actor:        {kind: operator, os_user: <uid>, player_id: <admin player_id>}
  event_type:   "system:rekey"
  timestamp:    <now>
payload (cleartext):
  context_type:                     <type>
  context_id:                       <ID, ULID or sorted-pair>
  old_dek_id:                       <int64; previous crypto_keys.id>
  new_dek_id:                       <int64; new crypto_keys.id>
  rows_re_encrypted:                <int>
  justification:                    <operator-supplied free text>
  policy_hash:        <bytes>           # references the active crypto.policy_set event at invocation time
  operator_factors:
    os_user:                        "uid=1001 (sean)"
    player_id:                      "01HXXX..."
    totp_verified:                  true | false
    auth_provider:                  "ingame-creds-totp"
    provider_specific_identity:     <opaque>
    dual_control_partner_player_id: "01HXXX..." | null
```

**`audit.<game>.system.crypto_policy.<policy_name>`** — emitted on every
crypto-policy change (server startup writes the current effective policy;
future reload paths emit on each change):

```yaml
metadata:
  actor:        {kind: system, server_identity: <server-id>}
  event_type:   "crypto.policy_set"
  timestamp:    <now>
payload (cleartext):
  policy_hash:        <bytes>           # SHA-256 over RFC 8785 JCS-canonicalized payload (excluding policy_hash)
  prev_hash:          <bytes nullable>  # null only at genesis (first policy_set for this policy_name)
  server_start_ulid:  <ULID>
  policy_snapshot:    <json>            # the full effective policy
  server_identity:    <string>
```

Hash algorithm pinned to **SHA-256 over RFC 8785 JCS-canonicalized JSON**
of the payload **excluding the `policy_hash` field**. Sub-epic D MUST
select an RFC-8785-compliant Go canonicalizer (e.g.,
`github.com/cyberphone/json-canonicalization`) and pin the version in
`go.mod`. Switching canonicalizer libraries or RFC interpretations is a
chain-breaking change and MUST be treated as a master-spec amendment, not
a sub-epic-internal refactor.

**`audit.<game>.system.operator_read.<context>`** and
**`audit.<game>.system.provider_migrate.<context>`** follow the same
pattern (including the `policy_hash` field referencing the active
`policy_set` event at invocation time).

ABAC policy MUST deny any plugin or character from subscribing to
`audit.*.plugin_decrypt.*` and `audit.*.system.*`. INV-15 verifies this
denial at the gRPC subscribe handler boundary. NATS-level deny rules
do not apply: game-topic NATS is single-principal by architectural
design (only the holomush server connects on these subjects), so there
is no plugin or character NATS account principal to deny. The
authoritative isolation gate is ABAC at the gRPC subscribe handler;
defense-in-depth is at the architectural level (gRPC mediation between
plugins/characters and NATS), not the substrate level. See §7.7 for
the full architectural framing.
Operators read these via `holomush admin audit query …` on the localhost
UNIX admin socket.

### 4.7 `events_audit` migration

The existing `events_audit` table
(`internal/store/migrations/000009_create_events_audit.up.sql`) has columns
`id BYTEA, subject, type, timestamp, actor_kind, actor_id, envelope,
schema_ver, codec, js_seq, inserted_at`. The `envelope` column was renamed
from `payload` in migration `000017_events_audit_envelope_rename` to
clarify pre-existing semantics — the column always carried the marshaled
envelope bytes (the full `Event` proto, see `publisher.go:295,302`), not
just the inner `Event.payload` field. This design adds two columns plus
one index:

```sql
-- migrations/NNNN_events_audit_dek_columns.up.sql
ALTER TABLE events_audit
    ADD COLUMN dek_ref     BIGINT,    -- nullable; references crypto_keys.id
                                       -- NULL for codec=identity rows
    ADD COLUMN dek_version INTEGER;   -- nullable; per-context version
                                       -- NULL for codec=identity rows

CREATE INDEX events_audit_dek_ref ON events_audit (dek_ref)
    WHERE dek_ref IS NOT NULL;

-- Foreign key is intentionally NOT added: events_audit must remain
-- readable as a forensic record even if the corresponding crypto_keys
-- row has been destroyed by Rekey. The cold-tier crossover handles
-- "dek_ref points at a row that no longer exists" via INV-39.
```

**Rationale for the choices:**

- **`BIGINT` for `dek_ref`:** matches `crypto_keys.id BIGSERIAL` and the
  substrate's `codec.KeyID = uint64`. Native integer storage; partial
  index keeps cleartext rows out of the index.
- **Nullable columns:** existing identity-codec rows pre-migration have
  no DEK — NULL is the honest representation. New identity-codec rows
  also leave them NULL (no DEK exists for cleartext events).
- **No foreign key:** `Rekey` destroys old `crypto_keys` rows. A FK would
  block that or cascade-delete `events_audit` rows, both wrong. Integrity
  is enforced by the cold-tier crossover logic (INV-39, INV-48), not by
  FK constraint.

**Audit projection update:** the projection consumer
(`internal/eventbus/audit/projection.go`) reads `App-Dek-Ref` and
`App-Dek-Version` from the JetStream message headers and inserts them
into the new columns. For `codec=identity` events both headers are absent;
the columns are inserted as NULL. Verified by INV-21 (byte-equality of
`events_audit.envelope` to the bus envelope bytes) and INV-49 (envelope
byte-equality across the full emit→audit→cold-read path, per Phase 3d
Decision 5).

| ID | Statement | Test type |
| --- | --- | --- |
| **INV-49** | Envelope byte-equality across emit→audit→cold-read: the marshaled `Event` proto envelope written to JetStream MUST be byte-equal to `events_audit.envelope` on the audit-projection side, AND the cold-tier reader MUST recover the same envelope bytes when serving historical reads via `QueryStreamHistory`. Per Phase 3d Decision 5, the envelope round-trip subsumes the prior header→column forward direction and the column→header reverse direction into a single envelope-equality invariant; `dek_ref`/`dek_version` columns are derived projections preserved for query/index purposes only. | Integration |

---

## Section 5 — Crypto provider interface

### 5.1 Layered interfaces

Three layers, with substrate types reused where they fit:

```text
                    ┌─────────────────────────────────────────────────┐
   Plugin / Host ──▶│ EventSink.Emit(event, Sensitive=true, ContextID)│
                    └────────────────────┬────────────────────────────┘
                                         ▼
                    ┌─────────────────────────────────────────────────┐
                    │ DEKManager.GetOrCreate(context_id) → codec.Key  │
                    │ DEKManager.Resolve(KeyID, version) → codec.Key  │
                    └────────────────────┬────────────────────────────┘
                                         ▼
                    ┌─────────────────────────────────────────────────┐
                    │ codec.Codec.Encode/Decode(                       │
                    │     plaintext, codec.Key, aad []byte)            │
                    │   (substrate type — interface extended in        │
                    │    Phase 3a per 2026-05-02 grounding doc)        │
                    └────────────────────┬────────────────────────────┘
                                         ▼
                    ┌─────────────────────────────────────────────────┐
                    │ KEKProvider.Wrap/Unwrap(dek_bytes) → wrapped    │
                    │   (DEKManager calls this when minting/resolving │
                    │    crypto_keys rows; codecs never call it)      │
                    └─────────────────────────────────────────────────┘
```

**Layer 3: substrate `codec.Codec` (extended in Phase 3a).** Defined in
`internal/eventbus/codec/codec.go:30-34`. The interface gains an explicit
`aad []byte` parameter on `Encode` and `Decode` to match the
`crypto/cipher.AEAD.Seal/Open` convention and avoid coupling the codec to
the proto `*Event` type:

```go
Encode(ctx context.Context, plaintext []byte, key Key, aad []byte) ([]byte, error)
Decode(ctx context.Context, ciphertext []byte, key Key, aad []byte) ([]byte, error)
```

`IdentityCodec` ignores `aad`. Sensitive codecs pass it straight to
`aead.Seal/Open`. The caller (Phase 3a's emit path) builds `aad` via
`internal/eventbus/crypto/aad.Build(...)` before calling `Encode`. New impls
registered: `xchacha20poly1305-v1`, `aes-gcm-v1`. Codecs never see KEK
material; they take a `codec.Key{ID: KeyID, Version: uint32, Bytes: 32_bytes}`.
The `Version` field (added in Phase 3a) closes a Phase 2 gap — every other
substrate boundary already carries `(KeyID, Version)` as the DEK identity.
See [`2026-05-02-event-payload-crypto-phase3a-grounding.md`](2026-05-02-event-payload-crypto-phase3a-grounding.md)
for rationale.

**Layer 2: `DEKManager` (new).** Replaces the substrate's
`KeyProvider`/`KeySelector` for sensitive events. Resolves keys by context
or by KeyID:

```go
package dek

import (
    "context"
    "github.com/holomush/holomush/internal/eventbus/codec"
)

// Manager owns DEK lifecycle: create, lookup, cache, rotate, rekey.
// On the encrypt path, callers ask for a key by context. On the decrypt
// path, callers ask for a key by codec.KeyID (the dek_ref from the bus
// header or audit row).
type Manager interface {
    // GetOrCreate returns the active DEK for a context. If no DEK exists,
    // mints DEK_v1 with the supplied initial participants.
    GetOrCreate(ctx context.Context, ctxID ContextID, initial []Participant) (codec.Key, error)

    // Resolve returns the DEK identified by keyID at the given version.
    // Used on decrypt; returns ErrNotFound if the DEK row was destroyed
    // by Rekey (caller falls back to cold tier per INV-39).
    Resolve(ctx context.Context, keyID codec.KeyID, version uint32) (codec.Key, error)

    // Add appends a participant to the active DEK's set without
    // rotating (Section 6.1). Publishes cache invalidation.
    Add(ctx context.Context, ctxID ContextID, p Participant) error

    // Rotate mints a new DEK version (Section 6.2). Old DEK preserved.
    Rotate(ctx context.Context, ctxID ContextID, newParticipants []Participant, reason string) error

    // Rekey is exposed only via the localhost admin socket; no public
    // gRPC surface (INV-16, INV-40). Implementation in admin/rekey.go.
    Rekey(ctx context.Context, ctxID ContextID, justification string, ops OperatorFactors) error
}

// ContextID names a DEK's social unit.
type ContextID struct {
    Type string  // 'scene' | 'dm' | 'channel' | 'character' | 'player'
    ID   string  // ULID or sorted-pair canonical form
}
```

**Layer 1: `KEKProvider` (new).** Wraps and unwraps DEK bytes using the
master KEK. Pluggable: `LocalAEADProvider`, `VaultTransitProvider`,
`NoneProvider`. Providers never see DEK semantic context (which scene,
which DM, which version) — they receive opaque 32-byte buffers:

```go
package kek

import "context"

// Provider wraps and unwraps Data Encryption Keys (DEKs) using a master
// Key Encryption Key (KEK) it manages internally. Implementations MUST
// keep the KEK material out of process memory whenever possible.
type Provider interface {
    // Name returns the provider identifier persisted in
    // crypto_keys.wrap_provider.
    Name() string

    // Wrap encrypts dek under the current KEK version. Returns the
    // wrapped bytes and a provider-specific kekKeyID identifying which
    // KEK version was used.
    Wrap(ctx context.Context, dek []byte) (wrapped []byte, kekKeyID string, err error)

    // Unwrap decrypts wrapped using the KEK identified by kekKeyID.
    Unwrap(ctx context.Context, wrapped []byte, kekKeyID string) (dek []byte, err error)

    // RotateKEK creates a new KEK version.
    RotateKEK(ctx context.Context) (newKEKKeyID string, err error)

    // HealthCheck verifies the provider is reachable and the KEK is
    // available.
    HealthCheck(ctx context.Context) error
}
```

**`kekKeyID` (provider-specific) is distinct from `codec.KeyID` (DEK row
ID).** Both are needed: `crypto_keys.wrap_key_id` stores `kekKeyID`
(which KEK version wrapped this DEK); `crypto_keys.id` is the
`codec.KeyID` that the bus and codec use to identify the DEK. Avoid
conflating these in implementation.

What's deliberately not in the interface:

- No `Encrypt(plaintext)` / `Decrypt(ciphertext)`. The provider never sees
  event payloads. Vault and KMS bills stay bounded (one provider call per
  DEK, not per event), and the trust boundary stays small.
- No key listing or metadata enumeration. The `crypto_keys` table is the
  source of truth for which DEKs exist.
- No batch operations. Wrap/Unwrap throughput is bounded by event volume,
  not provider RTT (the cache absorbs spikes).

### 5.2 Two provider topologies

#### Topology 1 — True remote KMS

The KEK lives in the KMS service; the server NEVER holds it. Every
`Wrap`/`Unwrap` is a network round-trip; the cache absorbs steady state.

In v1 we ship one of these: **`VaultTransitProvider`**.

Future work (v2+, gated by demand): `AWSKMSProvider`, `GCPKMSProvider`,
`AzureKeyVaultProvider`.

#### Topology 2 — Local AEAD with pluggable KEK source

The server fetches the KEK once at startup from somewhere, then does
Wrap/Unwrap locally. Functionally identical from the rest-of-system's
perspective; security posture differs (KEK lives in process memory).

```go
// LocalAEADProvider does Wrap/Unwrap locally using a master KEK
// fetched from a pluggable KEKSource.
type LocalAEADProvider struct {
    source KEKSource
    aead   cipher.AEAD
    // ...
}

// KEKSource fetches and refreshes master KEK material from a backing
// store. The KEK never leaves the LocalAEADProvider's process memory
// once Load returns.
type KEKSource interface {
    // Name is persisted in crypto_keys.wrap_provider as
    // "local-aead/<source-name>".
    Name() string

    // Load fetches the current KEK material. Called at startup and
    // after KEK rotation.
    Load(ctx context.Context) ([]byte, error)

    // Persist stores new KEK material after rotation. Some sources
    // (env, systemd-credential) are read-only and return a typed
    // error; rotation requires a different path for those.
    Persist(ctx context.Context, kek []byte) error
}
```

**KEKSources shipped in v1:**

| Source | Notes |
| --- | --- |
| `file` | Encrypted file + Argon2id-derived unlock; the v1 default |
| `keyring` | go-keyring; macOS Keychain, Linux Secret Service, Windows Credential Manager |
| `systemd-credential` | `LoadCredentialEncrypted=` |
| `env` | Dev only; refused in prod mode |

**Future KEKSources** (gated by demand, since DigitalOcean has no native KMS
and Cloudflare's offering is bound to Worker runtime):

- `aws-secrets-manager`
- `gcp-secret-manager`
- `azure-key-vault-secret`
- `1password-connect`, `bitwarden-secrets-manager`, `doppler`, `infisical`
- `cloudflare-bridge` (only if an operator is willing to run a Worker as a bridge)

### 5.3 The `file` KEKSource (v1 default)

**Bootstrap chain at server startup:**

```text
1. Read encrypted master key file from configured path
   (default: /var/lib/holomush/master.key.enc)
2. Obtain unlock passphrase via configured passphrase_source:
   a. prompt              → interactive password prompt at stdin
   b. keyring             → fetch from OS keyring
   c. systemd-credential  → read from $CREDENTIALS_DIRECTORY/<name>
   d. env                 → read from $HOLOMUSH_KEK_PASSPHRASE (dev only)
3. Derive intermediate key from passphrase via Argon2id
   (m=64MiB, t=3, p=4, salt from key file header)
4. AEAD-decrypt the master key file → KEK in process memory
5. KEK identified by SHA-256 fingerprint of the unwrapped key bytes
   (used as keyID for crypto_keys.wrap_key_id)
```

**Key file format** (binary, versioned):

```text
| 4-byte magic "HMK\x01" | 16-byte argon2 salt | nonce | wrapped KEK | tag |
```

**KEK rotation** (`LocalAEADProvider.RotateKEK`):

1. Generate fresh 32-byte KEK.
2. Write new key file with same passphrase derivation.
3. New keyID = fingerprint of new KEK.
4. Old KEK retained in memory for the lifetime of the rotation operation
   (used by `Rekey` and provider-migrate workflows).

The local provider does NOT keep multiple KEK versions resident long-term.
After rotation, all DEKs MUST be re-wrapped under the new KEK before the
old one is discarded. Enforced by a startup integrity check (INV-33)
that fails if any `crypto_keys` row references a `wrap_key_id` the current
provider can't unwrap. For external providers (Vault, KMS), multiple KEK
versions are normal and natively supported.

### 5.4 `VaultTransitProvider` (v1)

- Auth: AppRole, Kubernetes service account, or static token.
- `Wrap` = `transit/encrypt/<key>`; `Unwrap` = `transit/decrypt/<key>`.
- `keyID` = Vault-issued ciphertext version prefix (`vault:v3:...`).
- KEK rotation = `transit/keys/<key>/rotate`; Vault retains all versions.

### 5.5 `NoneProvider`

Dev-and-test sentinel that refuses to wrap (returns typed error). Used when
`crypto.provider.name = none`. Server with `NoneProvider` MUST refuse to
publish any event marked `Sensitive=true`. Refer to
INV-32, INV-34.

### 5.6 Configuration

```yaml
crypto:
  provider:
    name: local-aead              # local-aead | vault-transit | none
    local_aead:
      kek_source: keyring         # file | keyring | systemd-credential | env
      file:
        key_file: /var/lib/holomush/master.key.enc
        passphrase_source: prompt # prompt | keyring | systemd-credential | env
      keyring:
        service: holomush
        account: master-key
      systemd_credential:
        name: holomush-master-key
    vault_transit:
      address: https://vault.internal
      mount_path: transit
      key_name: holomush-master
      auth:
        method: approle           # approle | kubernetes | token
        role_id_file: /var/lib/holomush/vault-role-id
        secret_id_file: /var/lib/holomush/vault-secret-id
  cache:
    capacity: 1024
    ttl: 5m
    invalidation_subject: internal.cache_invalidate.dek
```

### 5.7 Provider migration (CLI)

```bash
holomush crypto provider-migrate \
    --from local-aead/keyring \
    --to vault-transit \
    --confirm
```

For each row in `crypto_keys`:

1. Old provider `Unwrap(row.wrapped_dek, row.wrap_key_id)` → DEK.
2. New provider `Wrap(DEK)` → `(new_wrapped, new_keyID)`.
3. Atomic UPDATE: `wrapped_dek`, `wrap_provider`, `wrap_key_id` (single
   transaction per row).
4. Original DEK material zeroed in CLI memory immediately.

Payload ciphertext is NEVER touched (INV-23). Migration is
idempotent — re-running does nothing once all rows are converted. The CLI
emits `audit.<game>.system.provider_migrate` events for each row migrated.

### 5.8 Cache

| Property | Value | Rationale |
| --- | --- | --- |
| Key | `(context_type, context_id, version)` | Version-aware so rotation works correctly |
| Value | Unwrapped DEK + `wrap_provider` name | Provider name lets us validate after KEK rotation |
| Capacity | 1024 entries (configurable) | Covers hundreds of active scenes with headroom |
| TTL | 5 minutes (configurable) | Bounds stale-cache window after re-wrap |
| Eviction | LRU + TTL | Standard |
| Invalidation triggers | KEK rotation, provider migration, `Rekey`, `Rotate`, `Add(participant)` | Explicit invalidation is the correctness mechanism; TTL is the safety net. `Add(participant)` invalidates `dek.ParticipantsCache` for `(ctxType, ctxID, version)` so the next AuthGuard.Check sees the post-Add set (Phase 3c, INV-12, INV-59). |
| Invalidation channel | NATS request-reply on `internal.<cluster_id>.cache_invalidate.dek.<ctx_type>.<ctx_id>` | Cross-replica via `invalidation.Coordinator` (Phase 3c); cluster-prefixed subject (INV-54) prevents cross-cluster confusion. Payload is `{seq, coordinator_member_id, cluster_id, ctx_type, ctx_id, action, issued_at, version, successor_version}` (per-action populate semantics: `rotate` Version=old + SuccessorVersion=new; `rekey` Version=destroyed + SuccessorVersion=replacement; `participants_changed` Version=mutated active version; `kek_rotation` both unused). Reply payload is `{member_id, ack: true}`. Sender waits for N-of-N replica acks where N is derived from `cluster.Registry.LiveMembers()` snapshot at publish time (INV-28, INV-29, INV-59). |
| Storage location | In-process memory ONLY | Per INV-27; MUST NOT live in NATS KV, PG, disk, or logs |

Memory hygiene: unwrapped DEKs in Go's GC heap are exposed to anyone with
`gdb` access — but that's explicitly out of scope per Section 1.
Locked buffers (`mlock`, zero-on-free) are deferred to a future hardening
pass.

### 5.9 `OperatorAuthProvider`

The interface gating `Rekey` and `AdminReadStream` invocations from the
localhost UNIX admin socket. Pluggable like `KEKProvider`; default
implementation uses in-game admin credentials + TOTP.

```go
package adminauth

import "context"

// OperatorAuthProvider authenticates an operator for destructive or
// information-disclosure admin operations. Returns OperatorIdentity on
// success or a typed error.
//
// Defense-in-depth notes per Section 1 threat model:
//   - host-access (SO_PEERCRED) is captured for audit but is NOT a
//     defense factor against an adversary with shell access.
//   - The factors that ARE defense gates are admin credentials,
//     TOTP, and (for high-sensitivity contexts) dual-control.
type OperatorAuthProvider interface {
    Name() string  // persisted in audit; e.g., "ingame-creds-totp"

    // Authenticate runs the provider's challenge sequence against the
    // operator. Implementations prompt at the CLI for credentials and
    // TOTP codes via the supplied PromptFunc. Returns the verified
    // identity on success.
    Authenticate(ctx context.Context, prompt PromptFunc) (OperatorIdentity, error)

    // RequireDualControl prompts for a second operator's signoff and
    // returns the second identity. The second identity MUST have a
    // different player_id from the first AND MUST hold the admin role.
    // Approvals expire 5 minutes from prompt issuance.
    RequireDualControl(ctx context.Context, primary OperatorIdentity, prompt PromptFunc) (OperatorIdentity, error)
}

// OperatorIdentity is the audit record shape per Section 4.6.
type OperatorIdentity struct {
    OSUser                  string  // "uid=1001 (sean)" — captured, not enforced
    PlayerID                string  // ULID; the verified identity
    TOTPVerified            bool
    AuthProviderName        string
    ProviderSpecificID      string  // opaque, provider-defined (e.g., Vault entity ID)
}

// PromptFunc abstracts CLI/test prompts.
type PromptFunc func(question string, secret bool) (string, error)
```

**Default implementation (`InGameCredentialsProvider`):**

1. Prompt for player username; verify against `players` table.
2. Prompt for password; verify Argon2id hash (same path as web auth).
3. Look up TOTP enrollment for this player. **Hard-required:** if not
   enrolled, refuse with `DENY_NOT_ENROLLED` and direct the operator to
   the enrollment path. No config knob bypasses this.
4. Prompt for 6-digit TOTP code and verify; on mismatch, refuse with
   `DENY_BAD_TOTP`.
5. Verify player holds the `crypto.operator` capability via
   `access.HasPlayerGrant(ctx, resolver, playerID, access.CapabilityCryptoOperator)`.
   On miss, refuse with `DENY_NOT_OPERATOR`. (Capability-storage
   mechanism: see §5.9.1.)
6. Verify player holds the `admin` role.

**Future providers (gated by demand):**

- `SSHAgentProvider` — operator's SSH key signs a server-issued nonce.
- `WebAuthnProvider` — hardware token (YubiKey).
- `OIDCProvider` — IdP integration via authorization-code flow on a
  localhost-bound HTTP listener.

#### 5.9.1 `crypto.operator` capability — storage and grant mechanism

The `crypto.operator` capability is a player-attribute grant — a flat
narrowing flag held on a player ID, MUST be combined with `RoleAdmin` to
authorize break-glass operations. It is **not** a `command.Capability`
tuple (the `{Action, Resource, Scope}` shape used for command pre-flight
authorization).

**Storage in v1: config-file allow-list.**

```yaml
crypto:
  operators:
    - "01HZAVGE83MGFEXQQH5SP9NXKF"  # admin Alice
    - "01HZAVGE83MGFEXQQH5SP9NXKG"  # admin Bob
```

**Runtime exposure:** A `PlayerAttributeProvider`
(`internal/access/policy/attribute/player.go`) introduces `player:` as a
Subject namespace in ABAC. Schema: `player.id: AttrTypeString`,
`player.grants: AttrTypeStringList`. For player IDs in the configured
allow-list, the provider exposes
`bags.Subject["player.grants"] = ["crypto.operator"]`; otherwise the
list is empty (non-nil).

**Consumer surface:** `access.HasPlayerGrant(ctx, resolver, playerID,
access.CapabilityCryptoOperator)` — typed Go facade implemented in
`internal/access/grants.go`. The OperatorAuthProvider (sub-epic D)
invokes this facade as step 5 of its check sequence.

**Validation:** Lax+warn at startup. The server cross-checks each
configured player ID against the players table; unknown IDs trigger
structured warnings (`slog.Warn("crypto.operator references unknown
player", "player_id", <ulid>)`). The configured list is used as-is —
validation is observability, not gating. Query failure during validation
emits a `"crypto.operator validation skipped"` warning and proceeds with
the full configured set.

**Reload:** Restart-only in v1. Hot reload is a documented future seam.

**In-game grant UX:** Deferred to a P3 follow-up bead. Operators edit
the YAML config and restart the server in v1.

---

## Section 6 — Lifecycle operations

### 6.1 `Add(context_id, participant)` — mutable membership

**Triggered by:** scene join, channel invite, DM auto-create on first
message, character-private context creation, and any other "new participant
joins existing context" flow.

**Mechanics:**

1. Look up current `crypto_keys` row for `context_id` where
   `rotated_at IS NULL`. If none exists, create DEK_v1 (the
   "context just came into existence" path).
2. Resolve the joining character's current `binding_id` via
   `Bindings.Current(character_id)` (§4.3a). If no active binding row
   exists, fail with `BINDING_MISSING` — every character with an active
   game session MUST have an active binding row.
3. Append `(player_id, character_id, binding_id, joined_at, added_via)` to
   the `participants` JSONB array. Single SQL UPDATE.
4. Phase 3c (holomush-ojw1.3) introduced participants caching in
   `dek.ParticipantsCache`. `Add` MUST publish via
   `invalidation.Coordinator.RequestInvalidation(ctx, ctxID,
   ActionParticipantsChanged)` after the JSONB UPDATE commits. The
   payload carries the active version (the one whose participants
   were mutated); receiving replicas evict
   `(ctxType, ctxID, version)` from their ParticipantsCache so the
   next AuthGuard.Check sees the post-Add set (INV-12, INV-59).

**No DEK rotation. No re-encryption. The new participant immediately gets
read access to all history under this DEK** once the participants-cache
invalidation has acked across the cluster. This is the user-visible
mechanism for "Carol joins a scene mid-stream and sees backstory."

**Failure modes:**

- Concurrent `Add` of same participant: idempotent on
  `(player_id, binding_id)` — second `Add` is a no-op.
- Concurrent `Add` and `Rotate`: serialized via PG row-level lock on the
  `crypto_keys` row. `Add` waits for `Rotate` to commit, then runs against
  the new active version.

### 6.2 `Rotate(context_id, new_participants, reason)` — new DEK forward

**Triggered by:**

- Participant *removal* (someone leaves a scene/channel).
- *Player rebind* on a character whose binding is in the participant set
  (always; non-overridable).
- Scene-policy explicit rotate ("rotate this scene's key now").
- Scheduled rotation per context-type policy (e.g. monthly DM rotation).

**Mechanics:**

```text
1. INSERT crypto_keys row with version = current max + 1:
     wrapped_dek    = Provider.Wrap(fresh_random_32B)
     wrap_provider  = current provider name
     wrap_key_id    = provider's current keyID
     participants   = new_participants
     created_at     = now()
2. Publish via `invalidation.Coordinator.RequestInvalidation(ctx,
   ctxID, ActionRotate)`. Receive-side eviction is no-op for today's
   cache shape; the protocol completes per INV-29 contract.
3. UPDATE old crypto_keys row:
     rotated_at     = now()
     superseded_by  = new row's id
```

DEK_v(N+1) is inserted *before* DEK_v(N) is marked rotated to bound the
overlap window. Active-DEK selection uses `version` column max, not
`rotated_at IS NULL`, when picking the active DEK for emit. The
`WHERE rotated_at IS NULL` index is a convenience.

Old DEK row is preserved indefinitely. It MUST remain unwrappable for reads
of historical events — the difference from `Rekey`.

**No `Rotate`-emitted audit event.** The triggering action (member-leave,
rebind) is the audit-worthy event and lives on its own subject. Rotation is
plumbing; surfacing it would create noise.

**Failure modes:**

- Provider unavailable during step 1: `Rotate` fails; existing emit path
  continues with DEK_v(N). Operator retries.
- Crash between steps 1 and 3: `crypto_keys` has two rows with no
  `rotated_at` set. Startup integrity check resolves: pick max version,
  mark earlier as `rotated_at = first row's created_at`, log warning.
  Idempotent (INV-37).
- Cache invalidation message lost: TTL (5 min) bounds staleness.

### 6.3 `Rekey(context_id, justification, operator_factors)` — destructive

**Invocation:** CLI only, on the localhost UNIX admin socket. Operator
authenticates per Section 7.5.
Never invocable from gRPC or in-game commands (INV-16,
INV-40).

**Mechanics:**

```text
Phase 1 — Authenticate & authorize:
  1.1  CLI prompts for and verifies operator credentials per OperatorAuthProvider
  1.2  If --dual-control: prompt for second operator within 5-min window
  1.3  CLI calls server's RekeyService over a localhost UNIX domain socket
       with SO_PEERCRED check
  1.4  RekeyService verifies admin role + crypto.operator capability + TOTP factor

Phase 2 — Mint new DEK:
  2.1  Generate fresh DEK_new (32 random bytes from crypto/rand)
  2.2  Provider.Wrap(DEK_new) → wrapped_new, keyID_new
  2.3  INSERT new crypto_keys row:
         version       = current max + 1
         participants  = current active version's participants (Rekey does
                         not change membership; use Rotate for that, then
                         Rekey if forced revocation needed)
         rekey_audit_id = pre-allocated UUID for the audit event

Phase 3 — Re-encrypt cold tier (events_audit):
  3.1  SELECT id, payload, codec FROM events_audit
       WHERE dek_ref = DEK_old.id ORDER BY id (deterministic for resumability)
  3.2  In batches of 1000:
         decrypted := Codec.Decrypt(payload, DEK_old)
         re_encrypted := Codec.Encrypt(decrypted, DEK_new, AAD=rebuilt_from_metadata)
         UPDATE events_audit
         SET payload = re_encrypted, dek_ref = DEK_new.id, dek_version = N+1
         WHERE id = current_id
  3.3  Track progress in a checkpoint row so a crashed Rekey resumes
       cleanly instead of starting over.

Phase 4 — Hot tier handling:
  4.1  Hot tier (JetStream) is append-only for live messages.
  4.2  Approach: do NOT attempt to rewrite JS messages. Old JS messages
       referencing DEK_old.id remain on the stream until JS retention
       rolls them off (~7d default).
  4.3  Subscribe / QueryStreamHistory MUST handle "DEK referenced by
       hot-tier message no longer exists in crypto_keys" by falling
       back to the cold tier (INV-39).
  4.4  Optional --purge-hot flag enumerates and deletes JS messages with
       dek_ref = DEK_old.id. Slow, operator-opt-in. Default off.

Phase 5 — Synchronous cross-replica cache invalidation (CRITICAL):
  5.1  Issue NATS request-reply on internal.cache_invalidate.dek.<context_id>
       with payload {context_id, old_key_id, new_key_id, action: "rekey"}
  5.2  Wait for N-of-N replica acks (registered server identities)
       within 5s timeout.
  5.3  Each replica on receiving the request:
         - Evicts (context_id, *) from its DEK cache and ParticipantsCache
         - Replies with ack
  5.4  On timeout: rollback. New DEK row stays (orphan, picked up by
       next Rekey retry); cold-tier rows already updated; old DEK NOT
       destroyed. Operator is told which replicas failed to ack.
  5.5  Phase 5 MUST complete successfully before Phase 6.

Phase 6 — Destroy old DEK:
  6.1  UPDATE crypto_keys
       SET destroyed_at = NOW()
       WHERE id = DEK_old.id
       (Soft-delete; production read paths filter destroyed_at IS NULL,
       achieving the same operational effect as hard-delete while
       preserving the row for forensic audit per INV-11. Phase 3c
       grounding doc Decision 4.)

Phase 7 — Emit audit event:
  7.1  audit.<game>.system.rekey.<context_type>.<context_id>
       (full operator_factors per Section 4.6)
  7.2  Must complete before CLI returns success. If audit emit fails,
       Rekey is logged as completed-without-audit-event in a fallback
       host-side log file; operator MUST escalate per documented runbook.
```

`Rekey` accesses the server via a localhost UNIX socket, not gRPC, to keep
the RPC entirely off-network. INV-40's static check is trivial because the
public gRPC services literally don't have a `Rekey` method.

**Failure modes:**

- Crash during Phase 3: checkpoint allows resume. Phase 5 is gated on
  Phase 3 completion. As long as DEK_old still exists, the cold tier
  remains readable.
- Crash during Phase 5: idempotent retry. If DEK_old row is gone but some
  `events_audit` rows still reference it (shouldn't happen if Phase 3
  completed cleanly), startup integrity check refuses to come up until
  manually resolved.
- Phase 7 audit emit failure: see 7.2. Rekey is recorded in a host-local
  log as fallback. The one place we tolerate an audit gap, with a
  documented escalation path. Justified because the Rekey itself is
  irreversibly recorded in database state (DEK rows changed); the audit
  event is a cross-reference, not the only record.

#### 6.3.1 Dual-control protocol

Server-issued approval-token mechanism (sub-epic D ships the implementation):

1. Primary operator runs `holomush crypto rekey ... --dual-control`.
2. Server creates a pending row in `admin_approvals`:
   - `request_id` (ULID, primary key)
   - `primary_player_id`
   - `op_kind` (e.g., `"rekey"`, `"admin_read_stream"`)
   - `op_args_hash` (SHA-256 over canonicalized invocation args)
   - `expires_at = now() + 5 min`
3. Server prints `request_id` to primary's terminal; primary
   communicates it out-of-band to the second operator.
4. Second operator runs `holomush admin approve <request_id>`.
5. Second operator authenticates via `OperatorAuthProvider`
   (admin credentials + TOTP + `RoleAdmin` + `crypto.operator`). MUST
   have a different `player_id` from primary AND MUST hold both
   `RoleAdmin` AND `crypto.operator`.
6. Server marks the row approved.
7. Primary's still-blocking CLI proceeds to Rekey / AdminReadStream
   execution.

Approval-token format: ULID. TTL: 5 minutes; expired rows MAY be left
until a periodic sweep (rows are tiny). `op_args_hash` binds the
approval to the primary's invocation args; mismatch on proceed → server
rejects with `DENY_APPROVAL_ARGS_MISMATCH`.

---

## Section 7 — Plugin authorization & decryption audit

### 7.1 Manifest declarations

Plugin `plugin.yaml` gains a `crypto` section. Two halves: what the plugin
*emits* and what it *consumes*.

```yaml
crypto:
  emits:
    - event_type: whisper
      sensitivity: always       # always | may | never
      description: "Direct character-to-character private message."
    - event_type: scene_pose
      sensitivity: may
    - event_type: presence
      sensitivity: never
  consumes:
    - subjects:
        - "events.*.character.*.whisper"
        - "events.*.scene.*.ic"
      requests_decryption:
        - core-comm:whisper
        - core-scenes:scene_ic
        # Opt-in: plugin wants plaintext for these event types when ABAC
        # also permits. Absence = plugin receives metadata-only delivery
        # for sensitive events of those types.
```

**Loader validation at install / upgrade:**

| Check | Failure mode |
| --- | --- |
| Every emitted event type appears in `crypto.emits` | Reject load |
| `sensitivity: never` is consistent with `Sensitive=false` at runtime | Runtime emit fails (INV-7) |
| `sensitivity: always` requires `Sensitive=true` at runtime | Runtime emit fails |
| `requests_decryption` references event types in this plugin's declared subscription subjects | Reject load (sanity) |
| `requests_decryption` is not a wildcard | Reject load (must enumerate) |
| Every `requests_decryption` reference is qualified `<plugin>:<event>` and the referenced plugin is in `requires` | Reject load (INV-45) |

There is no separate "plugin acknowledges metadata-only delivery" flag.
Every subscriber receives `metadata_only` deliveries by default; that's the
basic API contract, documented once in the plugin guide.

### 7.2 AuthGuard decision tree

```text
function Check(subject, dek, event, plugin_manifest, abac_engine):

  # Branch 1: subject is a character (the live-play case)
  if subject.kind == "character":
    if any participant in dek.participants where participant.binding_id == subject.binding_id:
      return PERMIT_PARTICIPANT
    return DENY_NOT_PARTICIPANT

  # Branch 2: subject is a player (the previous-tenure read path)
  # A player who used to play character C in this context can read history
  # from their tenure via a player-kind subscription. The check matches on
  # player_id (not binding_id) so the player's identity persists across
  # rebinds. Each session emits a player_history_read audit event once per
  # session-context pair (INV-51).
  if subject.kind == "player":
    if any participant in dek.participants where participant.player_id == subject.player_id:
      decision = abac_engine.Evaluate(
        subject  = subject,
        action   = "read_own_history",
        resource = "dek:" + dek.context_type + ":" + dek.context_id,
      )
      if decision.allow:
        // Audit emitted by the Subscribe handler once per session, not
        // per event (see INV-51).
        return PERMIT_PLAYER_HISTORY
      return DENY_PLAYER_NO_ABAC_GRANT
    return DENY_PLAYER_NEVER_PARTICIPATED

  # Branch 3: subject is a plugin instance
  if subject.kind == "plugin":
    if event.event_type not in plugin_manifest.requests_decryption:
      return DENY_MANIFEST_DECLARATION_MISSING

    decision = abac_engine.Evaluate(
      subject  = subject,
      action   = "decrypt",
      resource = "dek:" + dek.context_type + ":" + dek.context_id,
      attributes = {
        event_type:   event.event_type,
        plugin_name:  subject.plugin_name,
        plugin_inst:  subject.instance_id,
      },
    )
    if decision.allow:
      return PERMIT_PLUGIN_GRANT
    return DENY_NO_ABAC_GRANT

  # Branch 4: subject is an operator (gRPC client claiming operator role)
  # Operators NEVER get plaintext via the runtime AuthGuard path.
  # Legitimate operator reads go through AdminReadStream (Section 7.5).
  if subject.kind == "operator":
    return DENY_OPERATOR_USE_ADMIN_RPC

  return DENY_UNKNOWN_SUBJECT_KIND
```

Each denial code lands in a structured log + `audit.<game>.authguard_denial`
metric. Useful for debugging legitimate access failures and detecting policy
probes.

### 7.3 ABAC resource type

```text
resource ::= "dek:" <context_type> ":" <context_id>
```

Examples:

- `dek:scene:01HXXX...` — a specific private scene.
- `dek:dm:01HABC...:01HDEF...` — a specific DM pair.
- `dek:scene:*` — all scenes (wildcard for broad grants).
- `dek:dm:*` — all DM pairs (typical for moderation plugins).

Action: `decrypt`. Already-existing engine evaluates
`(subject, action, resource, attributes)` per the ABAC stack. No new policy
engine code; only new resource semantics + standard policy authoring.

Example grant for a moderation plugin:

```yaml
policy:
  id: "mod-filter-dm-decrypt"
  subject: { kind: plugin, name: mod-filter }
  action: decrypt
  resource: "dek:dm:*"
  conditions:
    - attribute: event_type
      in: ["core-comm:whisper", "core-comm:dm"]
  expires_at: "2026-12-31T00:00:00Z"
```

Grants are first-class ABAC records — revocable, time-bounded, auditable
through the existing policy stack.

### 7.4 Plugin emit-side authorization

A plugin emitting `Sensitive=true` for a context it isn't part of is the
symmetric concern: a compromised `mod-filter` shouldn't be able to forge
whispers into someone else's DM.

**Two checks at emit time:**

1. **Manifest declaration** (static): event_type must appear in
   `crypto.emits` with `sensitivity: may` or `always`.
2. **ABAC grant for emit** (dynamic): plugin must hold
   `(subject, action=emit_sensitive, resource=dek:<context_type>:<context_id>)`.

For most plugins, emit grants are identical to participation — a
scene-tracker plugin participates in scenes it tracks, and that
participation grants both `decrypt` and `emit_sensitive` on the same DEK.
For special-purpose plugins (a moderation bot that posts redaction notices
into a DM), a separate grant lets emit without read.

Granting `emit_sensitive` does NOT imply `decrypt`, and vice versa. The two
ABAC actions are independent (INV-44).

### 7.5 Operator break-glass: `AdminReadStream`

The legitimate path for operators to read sensitive content. Lives on the
localhost UNIX socket alongside `Rekey`; never on public gRPC.

```bash
holomush admin read-stream \
    --context scene:01ABC \
    --justification "Abuse investigation, ticket #1234" \
    --since "2026-04-20T00:00:00Z" \
    --until "2026-04-25T00:00:00Z" \
    [--dual-control]
```

Flow:

1. CLI calls the localhost UNIX socket. `SO_PEERCRED` records the OS
   user for audit purposes only — it is NOT a defense factor against
   the threat-model adversary (an operator with shell access trivially
   passes this check).
2. CLI authenticates per `OperatorAuthProvider` (§5.9):
   admin credentials with reauth + TOTP if configured. These ARE the
   defense factors.
3. Optional `--dual-control` requires a second operator's signoff within
   5 min; second operator MUST have a different `player_id` and MUST hold
   the admin role + crypto.operator capability.
4. Server emits `audit.<game>.system.operator_read.<context>` BEFORE
   returning any data.
5. Server streams plaintext events to the CLI; the CLI prints to stdout.
6. The audit event MUST land before the first data event; if audit emit
   fails, `AdminReadStream` refuses to proceed (no fallback). (INV-42)

The actual defense is admin-creds + TOTP + dual-control + immutable
audit trail. `SO_PEERCRED` and host-access are operational data captured
in the audit record, not gates against the threat-model adversary.

`AdminReadStream` is an information-disclosure operation; the disclosure is
the harm. We tolerate no audit gap. Stricter than `Rekey`'s fallback because
`Rekey` leaves a database-state record (DEK rows changed) while
`AdminReadStream` leaves no other trace.

The dual-control protocol — server-issued approval-token mechanism, the
`admin_approvals` table, the `op_args_hash` binding, and TTL semantics —
is specified in §6.3.1.

### 7.6 Decryption audit ordering

Two audit shapes for two different decryption events:

- **Plugin decrypt** (`audit.<game>.plugin_decrypt.<plugin>`) — emitted
  on every plaintext delivery to a plugin. Includes `dek_ref`,
  `dek_version`, `grant_id`. High-volume.
- **Operator decrypt** (`audit.<game>.system.operator_read.<context>`) —
  emitted once per `AdminReadStream` invocation, not per event. Includes
  time-range, justification, dual-control factors.

**Audit emission backpressure mechanism (single, authoritative):** plugin
decrypt audit events are queued in a bounded in-memory queue per plugin
(default capacity: 10,000 entries). The DecryptAuditEmitter drains the
queue and publishes to the audit subject. If the queue fills (audit
projection or NATS backpressure), the AuthGuard returns
`DENY_AUDIT_BACKPRESSURE` for that plugin until the queue drains below
50% — equivalent to "decryption is temporarily disabled when we cannot
audit it." Per-plugin, per-context isolation: one slow plugin does not
disable decryption for other plugins.

This is the single mechanism. There is no separate "rate-limited via
projection" path; the queue and the disable-on-overflow rule are the
contract. INV-19 ("every plugin decryption MUST emit an audit event") is
preserved because we never deliver plaintext when the audit queue is
full — instead the recipient gets metadata-only with denial code
`DENY_AUDIT_BACKPRESSURE`.

For operator reads: a 10,000-event investigation produces one audit
record, not 10,000. The audit captures *the act of investigation* with
bounds; the data accessed is described by the time-range field.

### 7.7 Audit-stream isolation

Game-topic NATS subjects (`events.>`, `audit.>`, `internal.>`) are
single-principal by architectural design: only the holomush server connects
on these subjects in any planned topology. Plugins emit via gRPC
`PluginHostService` and receive via host-mediated gRPC streams; characters'
subscriptions are server-internal multiplexing through the server's NATS
connection. There is no plugin or character NATS account, in embedded or
external mode. Plugins MAY use NATS separately for their own purposes
(their own subjects, their own clusters, their own credentials), but that
is a plugin-internal concern with no game-topic surface.

**ABAC is the authoritative isolation gate.** The default ABAC policy MUST
deny `subject={kind: plugin|character}, action: subscribe, resource: subject:audit.>`
(and `subject:internal.>`). INV-15 verifies this denial at the gRPC subscribe
handler boundary.

**Defense-in-depth is at the architectural level, not the substrate level.**
The absence of plugin/character NATS principals is a structural property of
HoloMUSH's gRPC-mediated plugin model. Removing the gRPC mediation between
plugins/characters and NATS would require a deliberate architectural change
subject to its own design review.

**NATS-level deny rules do not apply.** They have no target principal in
any planned topology. Earlier drafts of this section (and INV-52) presumed
an architecture where plugins or characters connected to NATS directly;
that assumption was incorrect for HoloMUSH. INV-52 retires; cite this
section as the architectural property it would have asserted.

**External NATS deploy.** When the server connects to an external NATS
cluster (tracked under `holomush-s5ts`), it authenticates as a single
account scoped to game topics:

```text
Account "holomush-server":
  publish:   events.>, audit.>, internal.>
  subscribe: events.>, audit.>, internal.>
```

Other accounts in the cluster have no publish or subscribe permission on
these subjects by default — enforced at the cluster admin layer, not
inside the server.

**Operator audit query.** A future operator-read account
(`holomush-operator-read`, subscribe `events.>` only — NOT `audit.>` or
`internal.>`) MAY be added under `holomush-s5ts` for monitoring and
debugging use cases. Audit-table reads remain the localhost UNIX admin
socket path (Phase 5), not NATS subscribe.

---

## Section 8 — Cold tier handling

### 8.1 Host-owned audit (`events_audit`)

The audit projection is crypto-blind. It routes message bytes from JetStream
to PostgreSQL without inspecting them. INV-21 (byte-equality) is testable
because the projection literally doesn't have the means to alter ciphertext.

```text
Bus event (ciphertext under DEK_v2)
  ↓
Audit projection consumer (existing M5 / F2 subsystem)
  ↓
INSERT INTO events_audit (id, subject, codec, payload, dek_ref, dek_version, ...)
  Same bytes as the bus event. No decryption, no re-encryption.
```

### 8.2 Plugin-owned audit

Per the JetStream cutover (F5), some subjects are owned by plugins, and
those plugins persist their audit rows in their own schema. Example:
`events.*.scene.>` is owned by core-scenes, which writes to
`plugin_core_scenes.scene_log`.

The clean split:

| Layer | Responsibility | Crypto-aware? |
| --- | --- | --- |
| Host EventSink (publish) | Encrypts payload, sets codec/dek_ref | Yes |
| Plugin's audit projection consumer | Routes bus bytes → plugin audit table | No — pass-through |
| Plugin's audit table schema | Mirrors `events_audit` shape: `payload BYTEA, codec TEXT, dek_ref BIGINT, dek_version INT` | Schema only |
| Plugin's `PluginAuditService.QueryHistory` RPC | Returns raw rows including ciphertext payload | No — pass-through |
| Host gRPC `QueryStreamHistory` handler | Receives rows from plugin, runs AuthGuard, decrypts or sets `metadata_only=true`, delivers to client | Yes |

**Plugins never see plaintext for sensitive events they didn't emit, and
never handle crypto operations.** They store and return ciphertext bytes;
the host owns all encryption, decryption, and authorization.

**Downgrade-attack fence (INV-50).** A malicious or buggy plugin could
return rows with `codec=identity` and cleartext `payload`, bypassing
encryption for events that should have been sensitive. The host defeats
this by maintaining a static "always-sensitive event-type set" computed
from loaded manifests' `crypto.emits` declarations. On every row received
from a plugin's `PluginAuditService.QueryHistory`, the host's
`QueryStreamHistory` handler compares the row's `type` against this set:

```text
For each row from plugin.QueryHistory:
  if row.type ∈ always_sensitive_types AND row.codec == "identity":
    REFUSE: oops.Code("AUDIT_ROW_DOWNGRADE_DETECTED")
    emit audit.<game>.system.plugin_integrity_violation
    fall back to events_audit (host-owned ground truth) if present
```

The host's `events_audit` is the byte-equal mirror of what was actually
published; if the plugin's response disagrees with the host's mirror, the
host's mirror wins. INV-46 (byte-equality) and INV-50 (downgrade detection)
together close the fence in both directions.

### 8.3 Plugin SDK helpers

```go
// pkg/plugin/audit.go

// AuditRow is the canonical schema plugins MUST use for their audit
// tables when persisting host-published events. The crypto-related
// fields are opaque to the plugin and MUST be stored byte-for-byte
// from the bus message.
type AuditRow struct {
    // Cleartext fields mirroring eventbuspb.Event (per §4.1 — no
    // separate EventMetadata message exists; the cleartext metadata
    // IS the event's top-level fields minus payload).
    EventID    ulid.ULID
    Subject    string
    Type       string                  // "<plugin>:<event_type>"
    Timestamp  time.Time
    Actor      *eventbuspb.Actor

    // OPAQUE crypto-related fields (plugin MUST store byte-for-byte;
    // host owns interpretation):
    Codec      string                  // codec.Name, e.g. "identity" or "xchacha20poly1305-v1"
    Payload    []byte                  // ciphertext when Codec != "identity"
    DEKRef     codec.KeyID             // 0 for identity codec; matches crypto_keys.id
    DEKVersion uint32                  // 0 for identity codec; per-context version
}

// StoreFromMessage extracts an AuditRow from a NATS JetStream message,
// preserving codec/payload/dek_ref byte-for-byte. Plugins MUST use this
// helper rather than constructing AuditRow manually, to avoid accidental
// transformation of crypto fields.
func StoreFromMessage(msg jetstream.Msg) (AuditRow, error)

// LoadForQuery returns a row in the shape PluginAuditService.QueryHistory
// is expected to emit. Round-trip-stable with StoreFromMessage.
func LoadForQuery(row AuditRow) (*pluginauditpb.AuditEvent, error)
```

The SDK helpers are the path of least resistance, and the integration
tests (INV-46, INV-47,
INV-48) catch any plugin that sidesteps them.

### 8.4 Hot/cold crossover with stale DEK refs

When a hot-tier message references a `dek_ref` that no longer exists in
`crypto_keys` (which happens after `Rekey` destroys the old DEK while old
JS messages haven't rolled off yet), the reader falls back to the cold tier.

```text
For each event from hot tier:
  if codec != identity:
    if DEK lookup fails (dek_ref not in crypto_keys):
      cold_row = events_audit.SELECT WHERE id = event.id
      if cold_row exists:
        substitute cold_row for hot_row
      else:
        emit metric: crypto.cold_dek_miss
        deliver event with metadata_only=true (metric-only signaling)
```

This case is rare (only happens during the JS-retention window after
`Rekey`) but needs to be handled cleanly. Behavior is conservative: prefer
the cold tier (re-encrypted under the new DEK), fall back to metadata-only
with a metric if both tiers diverged.

**Signaling: metric-only.** Per Phase 3d Decision 5, the terminal
"cold-itself-can't-decrypt" branch is signaled via the `crypto.cold_dek_miss`
metric only — no typed wire-header on the event. The earlier draft of this
section proposed a typed warning header; that was dropped because the
on-the-wire envelope round-trip (INV-49) is the contractual surface, and
out-of-band fallback signaling belongs in metrics, not in the event
envelope. INV-39 (hot→cold fallback loop) remains deferred to Phase 5.

### 8.5 Audit-of-audit subjects

The audit-of-decryption events themselves land in `events_audit` via the
host's normal audit projection (the `audit.*` namespace is host-owned, not
plugin-owned). They are always plaintext (codec = `identity`); they have no
`dek_ref`. Standard infrastructure handles them.

ABAC denies all plugin/character subjects from subscribing to
`audit.*.plugin_decrypt.*` and `audit.*.system.*`. Operators read these via
`holomush admin audit query …` on the localhost UNIX socket.

---

## Section 9 — Documentation deliverables

Every item below is acceptance criteria. The PR series is not mergeable
unless these ship together with the code. Voice and tone MUST match
`site/CLAUDE.md` (conversational, grounded, vivid when it counts, no filler,
acknowledge MU\* tradition where it helps). The crypto-runbook in particular
needs to be readable by a stressed-out 2 AM operator.

### 9.1 Player audience (`site/docs/guide/`)

| Path | Status | Purpose |
| --- | --- | --- |
| `site/docs/guide/privacy.md` | NEW | Plain-language explanation of what's private (whispers, DMs, private scenes), what's not, what an admin can and can't see, what happens when a character is taken over by a new player, what happens to your history when you leave a scene. Honest about the operator break-glass path. |
| `site/docs/guide/commands.md` | UPDATE | Mark which commands produce sensitive vs non-sensitive events. Add a "Privacy" subsection per affected command. |

### 9.2 Operator audience (`site/docs/operating/`)

| Path | Status | Purpose |
| --- | --- | --- |
| `site/docs/operating/crypto-setup.md` | NEW | Master-key bootstrap: encrypted file format, passphrase derivation, OS keyring setup, systemd-credential flow. Decision tree for picking a `KEKSource`. Required permissions on the key file. |
| `site/docs/operating/crypto-providers.md` | NEW | LocalAEAD vs Vault Transit comparison, configuration examples for each, when to pick which, how to migrate between providers. |
| `site/docs/operating/crypto-runbook.md` | NEW | Operational runbook: KEK rotation, DEK rotation for a context, Rekey procedure, provider migration, master-key recovery from backup, disaster recovery if the key is lost. Each procedure has step-by-step + expected audit-event signatures + common failure modes. |
| `site/docs/operating/crypto-monitoring.md` | NEW | Prometheus metrics emitted by the crypto subsystem and recommended alert rules. |
| `site/docs/operating/database.md` | UPDATE | Add `crypto_keys` table to backup discipline. `events_audit` and `crypto_keys` MUST be backed up consistently. |
| `site/docs/operating/plugin-security.md` | UPDATE | Plugin decryption grants: how to grant/revoke ABAC decrypt rights, how to audit which plugins requested decryption, how to scope grants tightly. |
| `site/docs/operating/deployment.md` | UPDATE | Key file deployment, recommended permissions, systemd unit example with `LoadCredentialEncrypted=`. |

### 9.3 Plugin developer audience (`site/docs/extending/`)

| Path | Status | Purpose |
| --- | --- | --- |
| `site/docs/extending/event-sensitivity.md` | NEW | The `crypto.emits` and `crypto.consumes` manifest sections. Sensitivity contracts. The `metadata_only` proto field contract. |
| `site/docs/extending/decryption-rights.md` | NEW | How to request decryption via `requests_decryption`. ABAC grant model. Audit consequences. When metadata-only is enough. |
| `site/docs/extending/events.md` | UPDATE | Document which proto Event fields are cleartext (`subject`, `type`, `actor`, `timestamp`, `id`) versus opaque (`payload`). Note that subject is operator-visible. Cross-link to event-sensitivity.md. |
| `site/docs/extending/audit-events.md` | UPDATE | Document the `audit.<game>.plugin_decrypt.*` and `audit.<game>.system.*` namespaces. Plugins MUST NOT subscribe to these. |
| `site/docs/extending/plugin-guide.md` | UPDATE | Cross-link to event-sensitivity.md. Update the manifest grammar reference. |
| `site/docs/extending/binary-plugins.md` | UPDATE | Document the SDK helpers `pkg/plugin/audit.go`. |
| `site/docs/extending/lua-plugins.md` | UPDATE | Lua-side `metadata_only` check pattern. |

### 9.4 Contributor audience (`site/docs/contributing/`)

| Path | Status | Purpose |
| --- | --- | --- |
| `site/docs/contributing/crypto-architecture.md` | NEW | Internal architecture: codec registry, provider interface, AAD construction, AuthGuard decision tree, DEK cache and invalidation, hot/cold tier crossover. |
| `site/docs/contributing/event-delivery.md` | UPDATE | Encrypt/decrypt path through EventSink and Subscribe. Update sequence diagrams. |
| `site/docs/contributing/event-store.md` | UPDATE | Cold-tier crypto flow (events_audit and plugin-owned audit tables). |
| `site/docs/contributing/coding-standards.md` | UPDATE | The `dek.Material` type discipline (per INV-27): no `io.Writer`, no `encoding/*` Marshal, no `slog`/`log`/`fmt.*` argument. Reference the `gorules/dek_no_serialize.go` ruleguard rule. |

### 9.5 Reference audience (`site/docs/reference/`) — auto-generated where possible

| Path | Status | Purpose |
| --- | --- | --- |
| `site/docs/reference/events/<plugin_name>.md` | NEW (auto-gen) | Per-plugin event catalogue generated from manifests. |
| `site/docs/reference/events.md` | UPDATE | Top-level index linking to per-plugin pages. Document the qualified-name format `<plugin>:<event_type>`. |
| `site/docs/reference/access-control.md` | UPDATE | `dek:<context_type>:<context_id>` resource type, `decrypt` and `emit_sensitive` actions, example policies. |
| `site/docs/reference/grpc-api.md` | UPDATE | New `metadata_only` field on `Event`. Note `codec`/`App-Dek-Ref`/`App-Dek-Version` are NATS headers, not proto fields. `AdminReadStream` and `Rekey` are NOT public gRPC services. |
| `site/docs/reference/crypto-keys-schema.md` | NEW | The `crypto_keys` PostgreSQL schema as authoritative reference. |
| `site/docs/reference/audit-subjects.md` | NEW | Catalogue of audit subject patterns. |

### 9.6 In-code documentation

| Location | Status | Content |
| --- | --- | --- |
| `internal/eventbus/crypto/doc.go` | NEW | Package-level overview matching `crypto-architecture.md`. |
| `internal/eventbus/crypto/authguard/doc.go` | NEW | AuthGuard contract + decision tree. |
| `pkg/plugin/audit.go` (godoc on exported symbols) | NEW | The pass-through helpers and their contracts. |
| `internal/eventbus/types.go` (godoc updates) | UPDATE | The `Event` envelope with the new fields and AAD construction described inline. |

### 9.7 Spec / superpowers docs

| Path | Status | Purpose |
| --- | --- | --- |
| `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md` | NEW | This document. |
| `docs/superpowers/plans/2026-04-25-event-payload-crypto-plan.md` | NEW | Implementation plan (next step after this brainstorm). |

### 9.8 Top-level

| Location | Status | Content |
| --- | --- | --- |
| Top-level `README.md` | UPDATE | One-line mention in the security/privacy bullet. Cross-link to `site/docs/guide/privacy.md`. |
| `CHANGELOG.md` | UPDATE | Highlight the change in operator and player-facing language. |

### 9.9 Acceptance discipline

Each doc deliverable MUST be:

1. Written or updated as part of the implementing PR series, not deferred
   to a follow-up.
2. Reviewed by `code-reviewer` and (for contributor / extending docs)
   `comment-analyzer` agents per project standard.
3. Cross-referenced from this spec — each section that introduces a concept
   names the doc page that explains it for users.
4. Covered by a doc-build CI gate — broken cross-links and missing pages
   fail `task docs:build`.
5. Voice and tone match `site/CLAUDE.md`. Reviewer agents check for this;
   PR is not mergeable if doc tone is stiff or robotic.

---

## Section 10 — Failure modes

### Bootstrap-time failures (server refuses to start)

| Failure | Detection | Behavior |
| --- | --- | --- |
| Master key file missing or unreadable | `KEKSource.Load` returns I/O error | Refuse start; log path attempted; suggest passphrase recovery from backup. |
| Wrong passphrase / corrupted file | AEAD tag mismatch on file decrypt | Refuse start; log distinct error code `oops.Code("KEK_UNLOCK_FAILED")`; do NOT log the passphrase. |
| `crypto_keys` row references unknown `wrap_provider` | Startup integrity check (INV-33) | Refuse start; report which rows; document recovery. |
| `crypto_keys` row references unknown `codec` | Startup integrity check | Refuse start; report row IDs. |
| `provider.name = none` but `crypto_keys` is non-empty | Startup integrity check (INV-32) | Refuse start; explicit "downgrade blocked" message. |
| Two unrotated `crypto_keys` rows for the same context (crashed Rotate) | Startup integrity check (INV-37) | Auto-resolve: pick max version, mark earlier rotated, log warning. Idempotent. |
| Provider HealthCheck fails at startup | `Provider.HealthCheck` error | Log warning but continue start. Cache covers steady-state reads (INV-35). |
| `policy_set` chain verification failure on startup | `prev_hash` of latest `policy_set` for `policy_name` does not match the actual predecessor's `policy_hash` | Server refuses to start (consistent with INV-32 / INV-33 / INV-37 fail-closed pattern) |

### Steady-state read failures

| Failure | Detection | Behavior |
| --- | --- | --- |
| DEK lookup miss in cache + provider unavailable | Cache miss → provider call → error | Return typed error `oops.Code("CRYPTO_PROVIDER_UNAVAILABLE")`. Cached reads continue succeeding. |
| `dek_ref` not in `crypto_keys` (post-Rekey, hot tier still has old message) | DEK lookup returns ErrNotFound | Fall back to cold tier (INV-39). If cold tier also missing, deliver event with `metadata_only=true` and warning metric. |
| AAD mismatch on decrypt | AEAD tag failure | Hard fail for that event. Return typed error; emit `eventbus_crypto_aad_mismatch_total` metric. Investigation runbook entry. |
| Provider returns wrong-length DEK | Length check before AEAD use | Treat as provider error; emit `eventbus_crypto_provider_corrupt_response_total`; do NOT cache; surface to client. |
| Cold-tier row's payload bytes corrupted (PG-level) | AEAD tag failure or length mismatch | Hard fail. Cannot be reconstructed without backup. |

### Steady-state emit failures

| Failure | Detection | Behavior |
| --- | --- | --- |
| Plugin emits `Sensitive=true` for an event type not declared in its `crypto.emits` | EventSink validation (INV-6) | Reject emit; typed error; do NOT publish. |
| Plugin emits `Sensitive=false` for an event type with `sensitivity: always` | EventSink validation (INV-7) | Reject emit; typed error. |
| Plugin emits `Sensitive=true` without `emit_sensitive` ABAC grant | AuthGuard pre-emit check (INV-41) | Reject emit; typed error. |
| DEK creation race | PG unique constraint on `(context_type, context_id, version=1)` | One INSERT wins; loser retries by SELECT-and-use-existing. |
| Provider unavailable during DEK creation | `Provider.Wrap` error | Reject emit; typed error to publisher. |

### Lifecycle operation failures

| Failure | Detection | Behavior |
| --- | --- | --- |
| Crashed `Rotate` | Startup integrity check (INV-37) | Auto-resolve. |
| Crashed `Rekey` | Checkpoint table (INV-38) | Resume from checkpoint on retry. Phase 5 (DEK destroy) gated on Phase 3 completion. |
| `Rekey` audit emit failure (Phase 7) | Subscribe ack timeout | CLI exit code; rekey recorded in fallback host log; documented escalation. |
| `AdminReadStream` audit emit failure | Pre-data-emit check (INV-42) | Refuse to proceed; no plaintext leaves the server. |
| Provider migration mid-flight failure | Per-row retry | Idempotent on resume. |
| KEK rotation mid-flight failure (LocalAEAD) | Detection at next emit/read | Provider holds old + new in memory until DEKs re-wrap; restart integrity check refuses to come up if any DEK can't be unwrapped; operator re-runs from checkpoint. |
| Concurrent `Add` and `Rotate` on same context | PG row-level lock | `Add` waits, retries against new active version. |
| Concurrent `Rotate` attempts | PG row-level lock | One wins, other exits as no-op. |

### Audit emission failures

| Failure | Detection | Behavior |
| --- | --- | --- |
| `audit.<game>.plugin_decrypt.*` emit queue full (default 10,000 entries per plugin) | Queue insert returns `ErrFull` | AuthGuard returns `DENY_AUDIT_BACKPRESSURE` for that plugin; recipient receives `metadata_only=true`. Decryption resumes when queue drains below 50%. INV-19 preserved: no plaintext is delivered without a successfully-queued audit event. (Per §7.6 — the single audit-backpressure mechanism.) |
| `audit.<game>.system.rekey.*` failure | Subscribe ack timeout | See `Rekey` Phase 7. |
| `audit.<game>.system.operator_read.*` failure | Pre-data-emit check | Refuse to proceed. |

### Substrate failures

| Failure | Detection | Behavior |
| --- | --- | --- |
| NATS JetStream temporarily unreachable | Publish RPC error | Emit retries with backoff per existing JetStream cutover behavior. Crypto layer unaffected. |
| PostgreSQL temporarily unreachable | Query error | DEK lookups fail unless cached; emit-time DEK creation fails; cold-tier reads fail. Standard PG operational concern. |
| Cache invalidation NATS subject delivery delayed | TTL fallback (5 min) | Brief window where replicas use stale participant lists. Participant set reads from `crypto_keys` row at decrypt time, so cache staleness affects only DEK-material caching. |

### Operator and policy errors

| Failure | Detection | Behavior |
| --- | --- | --- |
| Operator manually `DELETE FROM crypto_keys WHERE id = X` | Next read of an event referencing X | Read fails; falls back to cold tier per INV-39. **Documented: never `DELETE FROM crypto_keys` outside of `Rekey`.** |
| Operator restores PG backup but not `crypto_keys` (or vice versa) | Mass DEK-not-found OR mass orphan keys | Documented in `crypto-runbook.md` as the #1 backup discipline rule. Startup integrity check catches the orphan-key case; missing-DEK case manifests at first read. |
| Operator misconfigures ABAC grant (overly broad `dek:*`) | No automatic detection | Documented review pattern in `crypto-monitoring.md`; alert rule on grant breadth at install time. |
| Plugin author misdeclares sensitivity | Loader validation at install | Reject install with explanation. |

### Operator-auth DENY codes

| Code | Reason |
| --- | --- |
| `DENY_DUAL_CONTROL_REQUIRED` | Server enforces site `dual_control_required` policy; rejects single-control invocations of the listed operations |
| `DENY_APPROVAL_ARGS_MISMATCH` | Primary's invocation args do not match the stored `op_args_hash` from the approval row |
| `DENY_POLICY_HASH_UNKNOWN` | Rekey / AdminReadStream invocation references a `policy_hash` not present in the `policy_set` chain |

---

## Section 11 — Migration / cutover

**Critical principle: no data migration.** Existing plaintext events stay
plaintext forever. Retroactive encryption is operationally costly with no
real security benefit (those events were plaintext during their entire
prior life; encrypting them now does not un-leak them). An opt-in admin
tool can offer it for operators with specific compliance reasons; it is
not part of the cutover.

### 11.1 Phasing

The design lands in eight phases, each a discrete PR-able unit. The system
remains operational throughout; sensitive events do not exist until
phase 3.

| Phase | Scope | Visible effect |
| --- | --- | --- |
| 1 | Re-scoped `holomush-k18g`: manifest grammar expansion (`crypto.emits` per event type with sensitivity declarations); all existing plugins updated; auto-generated event reference docs | Plugin authors gain a declared catalogue; nothing else changes |
| 2 | `KEKProvider` interface + `LocalAEADProvider` + `NoneProvider` + `crypto_keys` table migration + `events_audit` columns migration + `DEKManager` skeleton + `dek.Material` type + lint rule | Server can wrap/unwrap DEKs; events_audit gains the new columns; nothing emits or reads sensitive yet |
| 3 | **COMPLETE** (Phase 3d closed 2026-05-03). EventSink encryption path + new codec impls (`xchacha20poly1305-v1`) + AAD canonicalization + AuthGuard (4 branches) + decrypt-on-fan-out + cache + request-reply cache-invalidation protocol + audit subject namespaces + emit-time sensitivity fence (INV-6/7 — host-side ground-truth check at emit) + cold-tier `QueryStreamHistory` crypto path for host-owned audit + Crypto.Enabled default flipped to true + INV-49 envelope round-trip + INV-15 ABAC subscribe denial + plugin-actor & character-actor E2E. Note: NATS account-level deny rules dropped per Phase 3d Decision 4 (game-topic NATS is single-principal by architectural design — see §7.7 amendment); the cold-tier read fence (INV-50) is plugin-owned-audit-specific and lands in Phase 7. Phase 3c grounding doc at [`docs/superpowers/specs/2026-05-02-event-payload-crypto-phase3c-grounding.md`](2026-05-02-event-payload-crypto-phase3c-grounding.md). Phase 3d grounding doc at [`docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md`](2026-05-03-event-payload-crypto-phase3d-grounding.md). Cluster substrate (`internal/cluster/`) landed as prerequisite. | End-to-end sensitive event flow works for host-owned audit subjects; players-as-character get full plaintext; previous-tenure player-kind reads emit per-session audit |
| 4 | `Add` + `Rotate` lifecycle ops with synchronous N-of-N replica cache invalidation | Participant changes work; player-rebind triggers `Rotate` correctly |
| 5 | `Rekey` CLI + `AdminReadStream` CLI + localhost UNIX admin socket + `OperatorAuthProvider` (default in-game-creds + TOTP) + `crypto.policy_set` audit-event emission (sub-epic D) + `admin_approvals` table (sub-epic D) + `player_totp` table (sub-epic A; merged) + `crypto_rekey_checkpoints` table (sub-epic E) | Operator break-glass and destructive revocation paths exist |
| 6 | `VaultTransitProvider` + provider migration CLI + KEK rotation CLI | Orgs with Vault can use it; provider-migrate and KEK rotation exercised |
| 7 | Plugin SDK helpers (`pkg/plugin/audit.go`); plugin-owned audit table integration; INV-50 downgrade fence test against plugin-owned audit | Plugin-owned audit handles sensitive events transparently; downgrade attack from a malicious plugin is detected |
| 8 | Site documentation deliverables (Section 9) | All five audiences have current docs |

Phases 1 and 2 can land in parallel. Phase 3 depends on both. Phases 4–7
each depend on 3. Phase 8 starts during phase 1 and ramps with later phases
— docs ship with the code that introduces each capability, never as a
follow-up.

### 11.2 Backwards compatibility for existing deployments

A deployment running today, upgrading to a release that includes phase 3:

1. Operator reads `site/docs/operating/crypto-setup.md` and provisions a
   master key + chooses a `KEKSource`.
2. Operator updates server config to point at the chosen provider, or sets
   `provider.name = none` for dev / no-encryption deployments.
3. Server starts. `crypto_keys` table exists but is empty. All existing
   events keep their `codec = identity`. Reads work unchanged.
4. As plugins update to declare `sensitivity: always/may` for event types
   and operators grant `emit_sensitive` ABAC policies, those event types
   start producing encrypted events at next emit.
5. Old plaintext events of those same types remain readable to anyone with
   subscription rights. They were emitted before the type was marked
   sensitive; there is no retroactive change.

No flag day. The transition is per-event-type and per-deployment.

### 11.3 Operator transition checklist

A short list pulled into `crypto-runbook.md`:

1. Decide on a `KEKSource` (file + passphrase, OS keyring, systemd-credential, or Vault).
2. Provision the master key. Back it up. Store the backup separately from
   the `crypto_keys` table backups.
3. `events_audit` and `crypto_keys` MUST be backed up consistently. PG
   point-in-time recovery covers both naturally.
4. Decide on plugin decrypt grants. Operate without grants for moderation
   tools until you have a case to permit them.
5. Verify TOTP enrolled for admin accounts who hold the `crypto.operator` capability.
   Required for `Rekey` and `AdminReadStream`.

### 11.4 Rollback

> **Pre-deployment note (Phase 3d):** HoloMUSH has no users and no
> deployments today, so rollback during Phase 3 is simply `git revert` of
> the relevant phase commits. The operator-grade rollback discussion below
> applies to future deployments and is preserved as the design intent for
> when this matters.

Phases 1–7 are individually rollback-safe in the migration sense: dropping
the binary back to a prior version with a populated `crypto_keys` table
works as long as the prior version simply ignores the table. The risky
operation is *destroying the master key*, which is irreversible. The
runbook flags this prominently.

A deployment that wants to abandon encryption entirely after using it: not
supported in v1. Operators who go down this path manually are on their
own. The `provider.name = none` startup check (INV-32)
explicitly refuses to come up if `crypto_keys` has rows.

---

## Section 12 — Open questions

These are decisions deliberately deferred to implementation or to future
work. The spec acknowledges them rather than papering over them. Each gets
a beads issue filed at spec commit time.

| # | Question | Owner / when |
| --- | --- | --- |
| 1 | UX for "departing participant retains crypto access to their own history" (rotate-only on remove). Should the UI show their old transcripts, hide them while crypto access remains, or offer per-context policy? | Implementation — frontend / UX work |
| 2 | Plugin-decrypt audit synchrony. Plaintext can deliver to a plugin before the audit event lands (Section 10). Acceptable for v1 with bounded queue + temporary-disable on overflow; stronger guarantee may be wanted later. | Future work; gated by demand |
| 3 | Right-to-be-forgotten beyond `Rekey`. Old ciphertext bytes persist in JS until retention rolls off, and PG backups still contain old wrapped DEKs. Two-tier content-addressed model (Q2 option B from the brainstorm) is the path for stronger deletion. | Future work; gated by GDPR / equivalent |
| 4 | Per-event DEKs vs per-context DEKs. Settled on per-context for v1; per-event would be cleaner cryptographically but with overhead. | Future work |
| 5 | Subject obfuscation. Subjects leak structural metadata. Becomes a problem if HoloMUSH grows multi-tenant or hosted-shared. The `subjectxlate` shim (`holomush-ec22.3`) is the existing surface. | Future work; gated by deployment model evolution |
| 6 | Client-side / E2E encryption. Server is in the trust path in v1. Worth revisiting if threat model evolves. | Future work |
| 7 | Memory-locking unwrapped DEKs (`mlock` + zero-on-free). Not in v1 because gdb-on-server is out of scope. | Future work |
| 8 | Hardware-token operator auth. `OperatorAuthProvider` is pluggable; hardware-token impl is future work. | Future work; gated by demand |
| 9 | Cloudflare-Workers-bridged `KEKSource`. Possible but requires operator to run their own Worker as a bridge. Out of v1 scope. | Future work; gated by Cloudflare product evolution |
| 10 | Automated DEK-leak detection. We have the audit trail; we don't have automated detection of suspicious decrypt patterns or DEK exfiltration. | Future work |
| 11 | System events emitted by the host itself (not by a plugin) — ad-hoc admin announcements, system messages — go through what manifest declaration path? Probably a host pseudo-manifest. | Implementation detail |
| 12 | Failed-decrypt sentinel. AAD-mismatch is very rare (suggests tampering or codec bug). Should it surface as `metadata_only=true` or a distinct `payload_corrupt=true` flag? | Implementation detail |

---

## Prerequisites and dependencies

This design depends on `holomush-k18g` (re-scoped) landing the per-plugin
event-type registry with sensitivity declarations. Without that, plugins
have no manifest-level declaration of event types, and the `crypto.emits`
block has no foundation to attach to. The implementation plan MUST sequence
k18g (expanded scope) as phase 1 before the crypto layer's emit-time
enforcement (INV-6,
INV-7).

Other beads to file at spec commit:

- One issue per open question in Section 12.
- One issue tracking the v2/future Topology-1 KMS providers
  (`AWSKMSProvider`, `GCPKMSProvider`, `AzureKeyVaultProvider`).
- One issue tracking v2/future Topology-2 KEK sources
  (`aws-secrets-manager`, `gcp-secret-manager`, etc.).

---

## References

- [JetStream Event Log + PostgreSQL Audit Projection — Design](2026-04-18-jetstream-event-log-design.md)
- `holomush-1tvn` epic — JetStream cutover (closed 2026-04-21).
- `holomush-k18g` — Migrate plugin-owned EventType constants (re-scoped by
  this design to also carry sensitivity declarations).
- `holomush-ec22.3` — `subjectxlate` shim endgame (relevant if subject
  obfuscation becomes a goal).
- `internal/eventbus/types.go:72` — existing codec seam.
- Signal sealed sender — reference architecture for envelope/content split.
- HashiCorp Vault Transit — reference for true remote KMS pattern.
- libsodium / age / WireGuard — reference implementations of XChaCha20-Poly1305.
