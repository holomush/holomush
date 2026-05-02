<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Event Payload Cryptography — Phase 3b Substrate Grounding

## Status

**REVISED (R4)** — resolves seven substrate seams for the subscriber-side decrypt path. Modifies the master spec inline at the sections cited below.

R4 (2026-05-02) closes design-reviewer R3 finding N1 (guest-character path enumeration as a second character-creation site requiring Tx-wrap; the unified-guest-auth path at `internal/auth/guest_service.go::CreateGuest` line 113 was missing from R3's substrate-footprint table). Status header version bump.

R3 (2026-05-02) closed design-reviewer R2 findings (Action-bag precedence reasoning, transactional substrate for character creation, `Bindings.Current` parameter type pinning, callsite-count reconciliation, legacy-PlayerID gate, orphan-character handling, master-spec back-population SQL placement, §11.1 phasing-edit decision).

R2 added **Decision 7** (`player_character_bindings` substrate) and refined **Decision 6** (Action-bag overlay convention) in response to plan-reviewer Round 1 findings #1 (`BindingID` not in session pipeline) and #5 (`AttributeBags` four-bag shape vs flat overlay). Master spec gained a new §4.3a defining the binding entity; §6.1 step 2 of `Add(participant)` mechanics gained an explicit `Bindings.Current` lookup. Decision 2 was updated to have the gRPC handler read `bindings.Current(info.CharacterID.String())` rather than a non-existent `info.BindingID`.

Companion document, not a replacement: the master spec at [`2026-04-25-event-payload-crypto-design.md`](2026-04-25-event-payload-crypto-design.md) remains authoritative for everything else, and the Phase 3a grounding doc at [`2026-05-02-event-payload-crypto-phase3a-grounding.md`](2026-05-02-event-payload-crypto-phase3a-grounding.md) remains authoritative for the emit-side substrate it closed.

Normative requirements use RFC2119 keywords (MUST, MUST NOT, SHOULD, SHOULD NOT, MAY) per the project's CLAUDE.md "RFC2119 Keywords" convention. Descriptive passages explaining decisions, alternatives considered, and future phases are not normative.

## Authors

- Sean Brandt
- Claude (collaborator)

## Date

2026-05-02

## Context

Phase 3a (epic `holomush-ojw1.1`, PR #3514) shipped the emit-side substrate — codec interface change to take `aad []byte`, `xchacha20poly1305-v1` codec, `EmitIntent.Sensitive bool`, `dek.Manager.GetOrCreate` wired into `JetStreamPublisher.Publish`, sensitivity fence at `internal/plugin/event_emitter.go::Emit` (INV-6 + INV-7). Default `Crypto.Enabled=false` means the emit path ships dark; no production deployment has emitted sensitive ciphertext yet.

Phase 3b (epic `holomush-ojw1.2`) lands the symmetric subscriber-side path: `AuthGuard` for participant/ABAC/manifest gating, decrypt-on-fanout when authorized, metadata-only delivery when denied, decryption audit for plugin recipients, and replacement of the `EVENTBUS_HISTORY_SENSITIVE_NOT_SUPPORTED_PHASE3A` fail-fast in the hot-tier history reader.

Five substrate seams need closure before a plan is written. The first is a Phase 3a drift caught during 3b grounding; the rest are unspecified seams the master spec deferred. Each is resolved against the actual code surface and edits the master spec inline where it became inconsistent.

---

## Decision 0 — Restore §4.1 wire shape: encrypt only `event.Payload`

**Decision:** Phase 3b MUST restore the wire-shape design specified in master spec §4.1 lines 480-490: only the `payload` field of the proto `Event` is ciphertext; fields 1-5 (`id`, `subject`, `type`, `timestamp`, `actor`) and field 7 (`rendering`) remain cleartext on the wire as proto fields.

Phase 3a's `internal/eventbus/publisher.go:189-291` proto-marshals the entire `eventbusv1.Event` and feeds the marshaled bytes to `codec.Encode`, producing a single ciphertext blob that becomes the JetStream message body. This drift means the subscriber would have to either reconstruct AAD inputs from headers (timestamp is not in any header — substrate gap) or peek-then-decrypt (impossible under AEAD). The drift was masked because Phase 3a ships dark (`Crypto.Enabled=false`) and the Phase 3a integration test holds the proto Event in test memory rather than recovering it from the wire.

**Restoration shape:** publisher first encrypts only `event.Payload []byte` and stuffs the ciphertext into `envelope.Payload`, then proto-marshals the envelope (with all other fields cleartext) and writes the marshaled bytes to `msg.Data`. Subscriber proto-unmarshals first, conditionally decrypts the `Payload` field if `codec != identity`, replacing the field in place. AAD is built from the cleartext envelope by both sides — `aad.Build` reads only metadata fields (`internal/eventbus/crypto/aad/aad.go:62-114`), never `event.Payload`, so it works identically before-encrypt at publish and after-unmarshal at subscribe.

**Why over alternatives:**

- *Add `App-Timestamp` header and keep encrypt-the-envelope:* a permanent wire-format addition for marginal benefit. Would lock in a contradiction between the master spec (encrypt-payload-only at §4.1) and the implementation. Forces every subscriber to synthesize a partial `*eventbusv1.Event` from headers including parsing actor-kind enum from string and timestamp string back to `timestamppb.Timestamp` — fragile and a second source of truth for metadata that already lives in the proto envelope.
- *Drop timestamp from AAD canonicalization (bump magic to `HMAAD\x02`):* requires a master-spec edit to §4.2 plus an `aad.Build` signature change, and creates an AAD versioning seam we don't otherwise need. Phase 3a-emitted ciphertext (currently zero in production) becomes unreadable until a migration script runs. Awkward for marginal benefit.
- *Keep Phase 3a's encrypt-the-envelope and document it as the new design:* would require a substantive master-spec edit reversing §4.1's "operator visibility (subject, type, timestamp, actor) is preserved by the existing proto layout" property. Reduces operator visibility into legitimate metadata for no security gain — AAD over plaintext is no stronger than AEAD over plaintext.

**Spec change:** master spec §4.1 stays as-written (the design intent is correct; only the implementation drifted). This grounding doc serves as the written record of the drift and its restoration.

**Cost:** restoration is localized to three files.

| File | Change | Approx scope |
|---|---|---|
| `internal/eventbus/publisher.go` | Move codec call so it operates on `event.Payload` only; build AAD from the cleartext envelope; marshal envelope last with ciphertext payload field | ~15 lines moved |
| `internal/eventbus/subscriber.go::decodeDelivery` | Proto-unmarshal first; conditional decrypt of `envelope.Payload` if codec != identity; rebuild AAD from the unmarshaled envelope | ~25 lines |
| `internal/eventbus/history/hot_jetstream.go::decodeJetStreamMessage` | Same restructure as `decodeDelivery`; replaces the `EVENTBUS_HISTORY_SENSITIVE_NOT_SUPPORTED_PHASE3A` fail-fast with the real path | ~25 lines |
| `test/integration/crypto/emit_test.go` | INV-21's existing `msg.Data() == row.Payload` assertion stays unchanged (both still hold the marshaled envelope; the audit projection at `internal/eventbus/audit/projection.go:281` writes `msg.Data()` byte-for-byte). NEW assertions to add for Decision 0's restored shape: `proto.Unmarshal(msg.Data(), &envelope)` succeeds; `envelope.Payload != []byte(plaintext)` (proves payload field is ciphertext); `envelope.Subject == "<expected scene subject>"` and `envelope.Type == "<expected type>"` (proves fields 1-5 are cleartext on the wire). | ~10-15 lines |

INV-1 (operator can't `nats sub` plaintext payload) holds *more cleanly* — it is now visible at the proto-field level rather than implicit in "the whole blob is opaque." INV-21 (audit row byte-equals bus event) holds — both store the marshaled envelope with ciphertext payload field. INV-25 (AAD tamper) holds — both ends build AAD from the same cleartext envelope fields.

No wire-format addition. No header surface change. Phase 3a-emitted ciphertext (currently none in production) is not readable by Phase 3b decrypt path; the staging-soak operators are notified via the bead trail (ojw1.4 carries the flag flip and explicitly waits on 3b/3c/3d completion).

---

## Decision 1 — `AuthGuard` interface in `internal/eventbus/authguard/`

**Decision:** Phase 3b MUST introduce a new package `internal/eventbus/authguard/` containing a typed `AuthGuard` interface, typed `Identity` and `Decision` types, and small dependency interfaces for participant lookup, plugin manifest lookup, ABAC evaluation, and audit-queue backpressure observation. The package mirrors how `internal/eventbus/codec/`, `internal/eventbus/crypto/aad/`, `internal/eventbus/crypto/dek/`, and `internal/eventbus/crypto/kek/` are organized — small package, single responsibility, clean test surface.

**Interface shape (canonical):**

```go
// internal/eventbus/authguard/authguard.go
package authguard

import (
    "context"

    "github.com/oklog/ulid/v2"

    "github.com/holomush/holomush/internal/eventbus/codec"
    "github.com/holomush/holomush/internal/eventbus/crypto/dek"
    accesstypes "github.com/holomush/holomush/internal/access/policy/types"
)

// IdentityKind is a closed enumeration of authentication kinds AuthGuard
// can evaluate. AuthGuard branches on this before any ABAC call.
type IdentityKind int

const (
    IdentityKindUnknown IdentityKind = iota
    IdentityKindCharacter
    IdentityKindPlayer
    IdentityKindPlugin
    IdentityKindOperator
)

// Identity is the typed authenticated principal AuthGuard evaluates.
// Named "Identity" rather than "Subject" because eventbus.Subject already
// exists at internal/eventbus/types.go:16 as the JetStream subject filter
// type (`type Subject string`). Using "Subject" here would collide every
// time a caller imports both packages.
//
// Identity is constructed at the authentication boundary (gRPC handler
// or plugin runtime) via the kind-specific constructors below.
type Identity struct {
    Kind        IdentityKind
    PlayerID    string  // populated for Player + Character
    CharacterID string  // populated for Character
    BindingID   string  // populated for Character
    PluginName  string  // populated for Plugin
    InstanceID  string  // populated for Plugin
}

// Constructors validate inputs and return Identity. Empty IDs for a kind
// that requires them MUST return AUTHGUARD_IDENTITY_INVALID.
func NewCharacterIdentity(playerID, characterID, bindingID string) (Identity, error)
func NewPlayerIdentity(playerID string) (Identity, error)
func NewPluginIdentity(pluginName, instanceID string) (Identity, error)
func NewOperatorIdentity() Identity  // no IDs; AuthGuard always denies (§7.2 Branch 4)

// CheckRequest is the AuthGuard input.
type CheckRequest struct {
    Identity   Identity
    KeyID      codec.KeyID
    KeyVersion uint32
    EventType  string     // "<plugin>:<event>" form
    EventID    ulid.ULID  // for audit, when AuthGuard permits a plugin
}

// DecisionCode is the typed §7.2 outcome enum.
type DecisionCode int

const (
    DecisionUnknown DecisionCode = iota
    PermitParticipant
    PermitPlayerHistory
    PermitPluginGrant
    DenyNotParticipant
    DenyPlayerNeverParticipated
    DenyPlayerNoABACGrant
    DenyManifestDeclarationMissing
    DenyNoABACGrant
    DenyOperatorUseAdminRPC
    DenyAuditBackpressure
    DenyUnknownIdentityKind
)

// Decision is what AuthGuard returns. Permit is the load-bearing field;
// Code carries the §7.2 outcome for logging/metrics. GrantID is set when
// ABAC permits (Branch 2 + Branch 3) for the audit-event payload.
// ABACDecision carries the underlying ABAC engine's trace data (policyID,
// matched policies, attribute bag) when ABAC was consulted; nil when
// AuthGuard denied without consulting ABAC (Branch 4 operator,
// DenyAuditBackpressure pre-check, DenyManifestDeclarationMissing).
type Decision struct {
    Permit       bool
    Code         DecisionCode
    GrantID      ulid.ULID
    Reason       string
    ABACDecision *accesstypes.Decision
}

// Permit is sugar for d.Permit; introduced for callsite readability.
func (d Decision) Permitted() bool { return d.Permit }

// AuthGuard is the policy-evaluation interface. Phase 3b ships exactly
// one concrete implementation; the package's tests use mocks.
type AuthGuard interface {
    Check(ctx context.Context, req CheckRequest) (Decision, error)
}

// ParticipantLookup is the AuthGuard's read interface against the DEK
// participant set. dek.Manager satisfies this via a thin adapter
// (internal/eventbus/authguard/adapter.go) — AuthGuard does NOT import
// internal/eventbus/crypto/dek directly.
type ParticipantLookup interface {
    Participants(ctx context.Context, keyID codec.KeyID, version uint32) ([]dek.Participant, error)
}

// ManifestLookup is AuthGuard's read interface against the plugin
// manifest registry. Implementations satisfy this from the plugin
// loader's manifest store.
type ManifestLookup interface {
    PluginRequestsDecryption(pluginName, eventType string) bool
}

// ABACEngine is AuthGuard's read interface against the access-control
// engine. The host's existing engine satisfies this directly.
type ABACEngine interface {
    Evaluate(ctx context.Context, req accesstypes.AccessRequest) (accesstypes.Decision, error)
}

// BackpressureChecker is satisfied by the DecryptAuditEmitter (Decision 3).
// AuthGuard's Plugin branch consults it before returning Permit;
// returns DenyAuditBackpressure if ShouldThrottle is true.
type BackpressureChecker interface {
    ShouldThrottle(pluginName string) bool
}

// New constructs the production AuthGuard against its dependencies.
// All four are required; nil returns AUTHGUARD_DEPENDENCY_NIL.
func New(p ParticipantLookup, m ManifestLookup, a ABACEngine, b BackpressureChecker) (AuthGuard, error)
```

**Decision-tree mapping to §7.2:** the implementation switches on `req.Identity.Kind` and runs the four branches exactly per master spec §7.2. The `DENY_AUDIT_BACKPRESSURE` rule from §7.6 fires inside Branch 3 (plugin) before the ABAC call; cheaper to reject early than to ABAC-permit-then-fail-emit. Branch 4 (operator) returns `DenyOperatorUseAdminRPC` without consulting any dependency — the §7.5 break-glass path is the only legitimate operator read and it does not go through AuthGuard.

**Why over alternatives:**

- *AuthGuard imports `dek.Manager` directly:* couples the authguard package to the DEK substrate's concrete type. Same anti-pattern this design avoids by keeping eventbus dependent on `AuthGuard` interface rather than concrete ABAC engine. Test seams want the interface boundary at every dependency edge.
- *AuthGuard receives pre-fetched participants/manifest in `CheckRequest`:* widens the request surface; pushes the lookup orchestration to every caller. AuthGuard owning its dependencies means subscriber and history reader pass identical-shape requests.
- *One method per subject kind:* explicit but verbose; locks in the §7.2 branch count at the interface level. Future Phase additions (e.g., a Phase 7-era plugin-as-service-on-behalf-of-character delegation) would force interface revision.
- *Reuse `accesstypes.Decision` directly:* AuthGuard's denial codes (`DenyNotParticipant`, `DenyPlayerNeverParticipated`, etc.) are AuthGuard's own contract; they belong in its package, not in the ABAC types package. The ABAC engine's `accesstypes.Decision` is carried as a pointer field on the AuthGuard `Decision` (`ABACDecision *accesstypes.Decision`) when ABAC is consulted (Branches 2 + 3); nil otherwise. This preserves ABAC trace data (policyID, matched-policies, attribute bag) for logging/metrics without conflating the two decision contracts.

**Phase 2 substrate dependency required:** `dek.Manager` MUST gain a `Participants(ctx, keyID codec.KeyID, version uint32) ([]Participant, error)` method. The data is already loaded — `internal/eventbus/crypto/dek/store.go:48-59` shows `row.Participants []Participant` is read on every `Resolve`. The new method surfaces it without a second SELECT.

**`dek.Manager` substrate-edit enumeration (callsites and surface):**

| Surface | Change | Rationale |
|---|---|---|
| `dek.Manager` interface (`manager.go:25-35`) | Add `Participants(ctx, keyID codec.KeyID, version uint32) ([]Participant, error)` | The interface is the contract every impl satisfies. |
| Production `manager` impl (`manager.go:38-65`) | Add method body delegating to `m.store.selectByID` (or a new `selectParticipantsByID` if performance audit shows the full `row` SELECT is wasteful) and `m.configured()` precondition check | ~15 LOC. |
| `manager.configured()` (`manager.go:79-83`) | No change — existing precondition guard suffices | The new method dereferences `m.store`; `configured()` already covers that. |
| `NewManagerForUnitTest` impl (`manager.go:67-73`) | Add method returning `DEK_MANAGER_NOT_CONFIGURED` (matches `GetOrCreate`/`Resolve` stub-bead behavior) | ~5 LOC. |
| Mockery for `dek.Manager` (per `.mockery.yaml`) | Regenerate; auto | No hand-edit. |
| AuthGuard cache policy for Participants | Defer caching to Phase 3c (`holomush-ojw1.3`) — 3b fetches fresh on every Check via the adapter; same staleness contract as `Resolve` itself | INV-28 / INV-29 cache-invalidation rules apply once Phase 3c lands; Phase 3b's adapter calls `Participants` synchronously. |
| Participant-data staleness under concurrent `Add`/`Rotate` | Phase 4 territory (DEK lifecycle ops are stub today, `manager.go:163-185`) — Phase 3b reads what's persisted at the time of Check | No Phase 3b impl required. |

**ABAC engine reuse:** the `ABACEngine` interface declared in Decision 1 has the same `(ctx, AccessRequest) → (Decision, error)` shape as the existing `policy.Engine.Evaluate`. AuthGuard unit tests MAY reuse `policytest.GrantEngine`, `policytest.NewErrorEngine`, and `policytest.MockAccessPolicyEngine` from `internal/access/policy/policytest/helpers.go` directly; they already satisfy the interface.

**Spec change:** master spec §7.2 already describes the decision tree correctly. No semantic edits to the master spec — only descriptive cross-reference: §7.2's `function Check(...)` pseudocode maps to `authguard.Guard.Check(ctx, CheckRequest)`.

**Cost:** new package (~400 LOC including tests for the four branches × happy-path + denial path). One adapter file (~30 LOC) wiring `dek.Manager` to `ParticipantLookup`. `dek.Manager.Participants` substrate edit per the table above (~25 LOC across interface + production impl + test stub). One method addition on the manifest registry (~15 LOC, depending on existing surface).

---

## Decision 2 — Identity construction at authentication boundaries

**Decision:** AuthGuard MUST NOT parse identities from `context.Context`, header values, or string forms. Identity construction is the responsibility of the boundary that owns the authentication source. Each boundary uses the kind-specific constructor from Decision 1 (`NewCharacterIdentity`, `NewPlayerIdentity`, `NewPluginIdentity`, `NewOperatorIdentity`) and passes the typed `Identity` through to AuthGuard.

There is no single `context.Context` carrying authentication data common across all identity sources. Three distinct boundaries exist:

| Source | What it knows | Construction site |
|---|---|---|
| gRPC `Subscribe` / `QueryStreamHistory` (live client) | Authenticated session at `internal/grpc/server.go:679` (`info, err := s.sessionStore.Get(ctx, req.SessionId)`) carries `info.PlayerID` and `info.CharacterID`; the **`binding_id` is resolved separately** via `bindings.Current(ctx, info.CharacterID)` against the new `player_character_bindings` table (Decision 7) | `internal/grpc/server.go::Subscribe` reads info, queries `bindings.Current`, calls `NewCharacterIdentity(playerID, characterID, bindingID)`, passes Identity to `OpenSession`; same shape for `QueryStreamHistory` |
| Plugin event consumer (Phase 7) | Plugin registry: which binary/Lua plugin instance is opening the subscription | Plugin runtime wiring — calls `NewPluginIdentity(...)` |
| Operator break-glass (Phase 5, `AdminReadStream`) | n/a — never goes through this AuthGuard | §7.5 dedicated path |

An `IdentityFromContext(ctx)` helper would either lie about non-existent context values or branch on which auth source populated which context key — exactly the string-soup the typed-`Identity` design rejects.

**Why `binding_id` is resolved separately, not from `info`:** `session.Info` carries player/character identity but not binding identity. Bindings are long-lived player↔character tenures (weeks/months, spanning many sessions); sessions are ephemeral connections (minutes/days). Conflating the two — e.g., using `session.ID` as `binding_id` — would falsely terminate the binding every time a player disconnects, breaking INV-10's "after rebind, the new player MUST NOT receive plaintext for events emitted before the rebind" because the same player reconnecting would generate a new "binding" identifier. The binding entity defined in Decision 7 (master spec §4.3a) gives `binding_id` the right lifecycle: created on initial bind, persists across sessions, ended on wizard-transfer / character deletion / voluntary release.

**Phase 3b scope:** the gRPC handler-side construction lands in this PR (Subscribe + QueryStreamHistory share the same shape). Plugin-runtime construction is **out of scope** for Phase 3b — lands in Phase 7 when plugin event consumers grow decrypt-on-fanout. Phase 3b ships the `Plugin`-kind branch in `AuthGuard.Check` exercised only by tests; production has no plugin subscribers that go through this AuthGuard yet.

**Why over alternatives:**

- *AuthGuard parses identity from `context.Context`:* pushes string-form parsing into the policy layer; adds a coupling between authentication carriers and authorization decisions; makes test mocks need to populate context-keys instead of struct literals.
- *Single shared "identity extraction" helper at gRPC layer:* only works for the gRPC source. Plugin runtime would need a separate path. Two paths anyway; explicit construction at each boundary is cleaner.
- *Resolve binding by deriving from `(player_id, character_id)` directly without a binding table:* loses INV-10/INV-11 semantics — there's no way to distinguish "this player's prior tenure on this character" from "this player's current tenure" without a stable per-tenure identifier. See Decision 7 for the substrate choice.

**Spec change:** master spec §7.2's branch logic operates on typed kinds (`subject.kind == "character"`); Decision 1 just makes the type concrete in Go (renamed `Identity` to avoid collision with `eventbus.Subject`). Decision 7 below adds the `player_character_bindings` substrate that supplies `binding_id`.

**Cost:** the gRPC Subscribe handler edit reads `info.PlayerID.String()`, `info.CharacterID.String()`, and one new `bindings.Current(ctx, info.CharacterID.String())` query (string-throughout, returns binding_id as a ULID-format string); constructs Identity; passes to `OpenSession`. Same shape for `QueryStreamHistory`. ~8 lines per handler. Plus the binding store dependency injection at server construction time.

**Legacy-PlayerID degradation (R3 note):** `internal/store/session_store.go:56` documents that `info.PlayerID` may be empty (zero ULID) for legacy sessions created before the `player_id` column existed. With Phase 3b's `Crypto.Enabled=false` default, this has no effect — the IdentityCodec branch never invokes AuthGuard, so a zero `PlayerID` flows through unused. Once Phase 3d's flag flip lands, a legacy session with zero `PlayerID` would cause `NewCharacterIdentity` to return `AUTHGUARD_IDENTITY_INVALID` and the Subscribe RPC to fail. **Phase 3d's flag flip is therefore gated on a backfill prerequisite: every active session row MUST have a non-zero `player_id` before `Crypto.Enabled` flips to true.** Phase 3b's non-goal list carries this prerequisite forward.

---

## Decision 3 — `DecryptAuditEmitter` with backpressure

**Decision:** Phase 3b MUST introduce a `DecryptAuditEmitter` component in a sibling subpackage `internal/eventbus/authguard/audit/`. The component owns per-plugin bounded queues, drain goroutines that publish to `audit.<game>.plugin_decrypt.<plugin_name>` subjects via `eventbus.Publisher`, and a queue-depth signal exposed as `BackpressureChecker.ShouldThrottle(pluginName) bool`. The `BackpressureChecker` interface itself is declared in `internal/eventbus/authguard/` (consumed by AuthGuard); the emitter package imports it. One concrete type in the audit subpackage satisfies both `DecryptAuditEmitter` and `BackpressureChecker`:

```go
// PluginDecryptRecord is the audit event payload (cleartext under
// IdentityCodec; never encrypted — these are audit, not data).
type PluginDecryptRecord struct {
    PluginName       string
    PluginInstanceID string
    EventID          ulid.ULID
    EventSubject     eventbus.Subject
    EventType        eventbus.Type
    DEKRef           codec.KeyID
    DEKVersion       uint32
    GrantID          ulid.ULID
}

type DecryptAuditEmitter interface {
    EmitPluginDecrypt(ctx context.Context, rec PluginDecryptRecord) error
}

// BackpressureChecker is declared in internal/eventbus/authguard/
// (Decision 1) and consumed there by AuthGuard's Plugin branch. The
// audit subpackage imports it; queuedAuditEmitter satisfies it.

// queuedAuditEmitter is the concrete type satisfying both
// authguard.BackpressureChecker and audit.DecryptAuditEmitter. The
// emitter holds an eventbus.Publisher (for sending the audit events)
// and per-plugin queues with drain goroutines.
type queuedAuditEmitter struct {
    pub        eventbus.Publisher
    capacity   int             // default 10_000 (master spec §7.6)
    threshold  float64         // default 0.5 (drain below 50% lifts throttle)
    queues     sync.Map        // map[string]chan PluginDecryptRecord
    // ... drain goroutines, shutdown signal, wg ...
}
```

**Operating rules per master spec §7.6:**

- Queue capacity per plugin: 10,000 entries (configurable via subsystem option).
- `ShouldThrottle(pluginName)` returns `true` when the plugin's queue is at capacity; remains `true` until the queue drains below `capacity * threshold` (default 50%).
- AuthGuard's Branch 3 (plugin) consults `ShouldThrottle` *before* the ABAC call. Throttled → `DenyAuditBackpressure`.
- `EmitPluginDecrypt` is non-blocking: a successful enqueue commits the audit for eventual emission; the drain goroutine does the actual `Publisher.Publish`. Enqueue on a full queue returns `AUDIT_QUEUE_FULL` — the subscriber MUST treat this as DenyAuditBackpressure and stamp `metadata_only=true` instead of delivering plaintext (defense in depth: even if AuthGuard's pre-check passed in a TOCTOU window, the post-check during enqueue catches it).
- Audit events are `Sensitive=false`, IdentityCodec, host-emitted. They flow through `eventbus.Publisher.Publish` like any event, are mirrored to `events_audit` by the existing audit projection, and reach the cold tier through the standard path.
- **Drain-side failure under inheritance from §12 Q2:** master spec §12 Q2 acknowledges plaintext can deliver to a plugin before the audit lands ("Plugin-decrypt audit synchrony"). If the drain goroutine's `Publisher.Publish` call fails (NATS unavailable, JetStream timeout) AFTER the audit was successfully enqueued and the recipient already received plaintext, INV-19's "MUST emit an audit event" obligation is met by the *enqueue*, not by the eventual publish. Drain failures surface as a metric (`authguard_audit_drain_failed_total`) and a structured log; they do NOT retry-block the subscriber path. This is a master-spec inheritance, not a Phase 3b invention.

**Lifecycle:** the emitter's drain goroutines start with the eventbus subsystem and stop on subsystem shutdown after a bounded flush deadline. Per-plugin queues are created lazily on first `EmitPluginDecrypt(plugin=...)` and torn down on plugin unload (Phase 7-era — a no-op for Phase 3b's test-only plugin path).

**Phase 3b scope:** plugin_decrypt audits only.

| Audit subject | Phase | Reason |
|---|---|---|
| `audit.<game>.plugin_decrypt.<plugin>` | **3b** (this PR) | INV-19 requires this path before any plugin can decrypt. |
| `audit.<game>.system.operator_read.<context>` | Phase 5 (`AdminReadStream`) | Tied to operator break-glass RPC; out of scope for 3b. |
| `audit.<game>.system.rekey.<context>` | Phase 5 (`Rekey`) | Tied to Rekey CLI; out of scope for 3b. |
| `audit.<game>.system.player_history_read` | Phase 4 (player rebind / Branch 2) | Tied to `Add`/lifecycle; out of scope for 3b. |

**Phase 3b non-scope:** the `audit.>` namespace's two-layer isolation (§7.7).

| Layer | Phase | Reason |
|---|---|---|
| ABAC default policy denying `subscribe` on `audit.>` for plugin/character subjects | **3b** | Default-policy artifact; lands with the AuthGuard wiring. |
| NATS account-level `deny_subscribe = ["audit.>"]` rule for plugin/character accounts | Phase 3d (`holomush-ojw1.4`) | Operator-facing config artifact; lands alongside the flag flip. The bead description for ojw1.4 already lists "NATS deny rules" as 3d scope. |

INV-15 and INV-52 (NATS account deny is the architectural source of truth) remain Phase 3d invariants. INV-19 is Phase 3b.

**Why over alternatives:**

- *No separate emitter; subscriber's decode path calls `eventbus.Publisher.Publish` directly with the audit subject:* simplest. But synchronous publish on the fan-out hot path means a slow JetStream pins fan-out for *all* recipients, violating INV-20 ("a plugin authorization failure MUST NOT block fan-out to other recipients"). And there's no explicit backpressure signal — slow JetStream silently makes Subscribe slow with no `DenyAuditBackpressure` telemetry. Loses §7.6's contract.
- *DecryptAuditEmitter is a thin shim; backpressure lives in a separate `AuditQueueManager`:* three interfaces; AuthGuard, Subscriber, and DecryptAuditEmitter all reach into AuditQueueManager. Two-interface shape on one type is the same machinery with cleaner wiring.
- *Wrap audit emission inside AuthGuard.Check itself (AuthGuard returns "permit + audit-emitted" or "deny"):* makes AuthGuard side-effecting, harder to unit-test, harder to reason about. Audit emission is post-decrypt, pre-deliver — it belongs at the subscriber's fan-out path, not inside the policy decision.

**Spec change:** master spec §7.6 stays as-written. This decision implements §7.6's "single, authoritative" backpressure mechanism exactly as the spec describes.

**Cost:** new component (~250 LOC including drain-goroutine + lifecycle + per-plugin-queue tests). One audit-namespace publish helper (~30 LOC). One ABAC default-policy entry for `audit.>` (Phase 3b's contribution to §7.7's two layers).

---

## Decision 4 — `metadata_only` lives on `corev1.EventFrame`, not `eventbusv1.Event`

**Decision:** Phase 3b MUST add `bool metadata_only = 10` to `corev1.EventFrame` ([`api/proto/holomush/core/v1/core.proto:136-154`](../../api/proto/holomush/core/v1/core.proto)) — the per-delivery wire shape used by the gRPC `Subscribe` and `QueryStreamHistory` RPCs. The `eventbusv1.Event` storage shape ([`api/proto/holomush/eventbus/v1/eventbus.proto:34-45`](../../api/proto/holomush/eventbus/v1/eventbus.proto)) MUST NOT gain a `metadata_only` field.

**Rationale:** `metadata_only` is a per-delivery flag — its value depends on the receiving subject's authorization at the moment of delivery — not a property of the event itself. Master spec §4.1 lines 494-506 places it on `eventbusv1.Event` and acknowledges directly that the field is "set by host before sending to client; never set by emitter and never persisted to events_audit." Those properties are an exact description of a *delivery* flag, not an *event* field. Putting it on the storage shape forces:

- A "publisher must not marshal this field" filter at emit.
- An "audit projection must not read this field" filter at audit.
- Two separate "this field is special, ignore it here" rules in code that handles `Event`.
- Permanent forward-carry of a delivery-only field in the storage shape's schema-version bumps.

`EventFrame` already mixes event-identity fields with per-delivery state — `cursor` (field 8) is the per-recipient pagination cursor. `metadata_only` is the same shape: per-recipient delivery flag. Field 10 is the next free number.

**Go-side surface:**

```go
// internal/eventbus/types.go (or wherever Delivery lives today)
type Delivery interface {
    Event() Event
    MetadataOnly() bool  // NEW; true when AuthGuard denied
    Ack() error
    Nack() error
    InProgress() error
}

// jetStreamDelivery (internal subscriber type) gains a metadataOnly field
// stamped during decodeDelivery at the AuthGuard-denial point.
```

The Go-internal `eventbus.Event` struct does NOT gain a `MetadataOnly` field. The flag is per-delivery, not per-event; it lives on the `Delivery` wrapper.

**Stamping site:** subscriber's decode path (`decodeDelivery` for live Subscribe; `decodeJetStreamMessage` for hot-tier history) stamps `metadataOnly=true` *immediately on AuthGuard denial* — the gRPC handler is a thin proto-translation layer that reads `delivery.MetadataOnly()` and writes `EventFrame.metadata_only`. AuthGuard makes the decision; the subscriber implements it.

**Why over alternatives:**

- *Place `metadata_only` on `eventbusv1.Event` per spec §4.1:* tangles storage and delivery shapes, requires emit-time filter + audit-projection filter to maintain the "never persisted" property. Master spec §4.1's stated semantics for the field are exactly delivery semantics; the proto location is the only thing wrong.
- *Use `payload=empty` as the delivery signal without a separate flag:* loses disambiguation. A legitimate empty-payload event (e.g., a presence event with no content) and a redacted event would be wire-indistinguishable. Clients couldn't tell "no data was emitted" from "you weren't authorized to see the data."
- *Define an `EventFrameRedaction` sub-message:* over-design. One bool field, one decision.

**Spec change:** master spec §4.1 lines 492-506. Phase 3b adds an entry to the "Master spec edits required" table noting that the `bool metadata_only` proto field belongs on `corev1.EventFrame` (the per-delivery shape) rather than on `eventbusv1.Event` (the storage shape). The semantic properties the section describes are correct; only the proto location is wrong.

**Cost:** one proto field addition. One method addition on the `Delivery` interface (with one struct field on the concrete `jetStreamDelivery` type). gRPC Subscribe + QueryStreamHistory handlers each gain a one-line stamp during `Event → EventFrame` translation. No emit-side filter (because the field is on the delivery shape, not the storage shape). No audit-projection filter for the same reason.

---

## Decision 5 — `OpenSession` signature gains required `identity Identity`; AuthGuard wired into both subscriber and hot-tier history reader

**Decision:** Phase 3b MUST extend `eventbus.Subscriber.OpenSession` to take a typed `authguard.Identity` as a required parameter:

```go
// Before
type Subscriber interface {
    OpenSession(ctx context.Context, sessionID string, filters []Subject) (SessionStream, error)
}

// After (Phase 3b)
type Subscriber interface {
    OpenSession(ctx context.Context, sessionID string, identity authguard.Identity, filters []Subject) (SessionStream, error)
}
```

`authguard.Identity` is required, not an option, not a default. With `Crypto.Enabled=false`, the Identity is unused (decode path stays on the IdentityCodec branch and never invokes AuthGuard) — but the parameter is still required at the API surface so no caller can accidentally subscribe without identity and discover the lapse via runtime `DenyUnknownIdentityKind` on the first sensitive event.

Phase 3b MUST wire AuthGuard into two callsites:

| Callsite | Path | Role |
|---|---|---|
| `internal/eventbus/subscriber.go::decodeDelivery` (called from `jetStreamSessionStream.Next`) | Live Subscribe | Per-delivery AuthGuard.Check; decrypt-or-stamp-metadata_only |
| `internal/eventbus/history/hot_jetstream.go::decodeJetStreamMessage` | Hot-tier history reader | Same shape; replaces `EVENTBUS_HISTORY_SENSITIVE_NOT_SUPPORTED_PHASE3A` fail-fast |
| `internal/eventbus/audit/plugin_consumer.go::decodeEnvelope` (line 336-365) | **Out of scope (Phase 3b)** | This consumer rejects any non-identity codec at line 343-346 (`AUDIT_PLUGIN_CODEC_UNSUPPORTED`); no plaintext flows through it under Crypto.Enabled=true. Full plugin-decrypt path lands in Phase 7 per master spec §11.1 row 7. Decision 0's wire-shape restoration leaves `decodeEnvelope`'s identity-passthrough call to `c.Decode` structurally correct (proto-unmarshal-first applies; identity codec ignores `aad=nil`); the function is left as-is for Phase 3b. |

The two in-scope callsites share the same `authguard.AuthGuard` interface dependency. The hot-tier history reader's gRPC handler (`QueryStreamHistory`) builds Identity the same way the live Subscribe handler does — same `NewCharacterIdentity` constructor at the same authenticated-session boundary.

**Cold-tier (PostgreSQL `events_audit`) reader stays Phase 3d.** Per the bead description for `holomush-ojw1.4`, cold-tier crypto is in 3d scope; the implementation will be the same shape as the hot-tier reader because INV-21 guarantees byte-equality between bus and audit-row payloads. Phase 3b is the substrate; 3d is the propagation.

### Order of operations on the decrypt path (plaintext residency under TOCTOU)

The subscriber's decode path executes the AuthGuard / Decode / Audit-emit sequence in a fixed order to bound plaintext residency in process memory. This MUST be implemented as written; deviation creates an INV-9 (no-plaintext-to-non-participants) hole or an INV-19 (every-plugin-decrypt-audited) hole.

```text
For each delivered message:
  1. Read headers; proto.Unmarshal(msg.Data) → envelope
  2. If envelope.codec == identity:
       deliver Event{Payload: envelope.Payload, MetadataOnly: false}
       (AuthGuard NOT invoked — non-sensitive path)
  3. Else (sensitive):
       a. Build CheckRequest{Identity, KeyID, KeyVersion, EventType, EventID}
          from session.identity + headers
       b. decision = AuthGuard.Check(ctx, req)
          (Branch 3 plugin: ShouldThrottle pre-check fires here; if
           throttled → DenyAuditBackpressure; no decrypt)
       c. If !decision.Permit:
            deliver Event{Payload: nil, MetadataOnly: true}
            (decrypt NOT performed; ciphertext discarded)
       d. Else (Permit):
            i.   key = dek.Manager.Resolve(KeyID, KeyVersion)
            ii.  aad = aad.Build(envelope, codecName, dekRef, dekVersion)
            iii. plaintext = codec.Decode(envelope.Payload, key, aad)
            iv.  If req.Identity.Kind == Plugin:
                   err = DecryptAuditEmitter.EmitPluginDecrypt(rec)
                   If err == AUDIT_QUEUE_FULL (TOCTOU between step b and iv):
                     // Overwrite plaintext bytes BEFORE building the
                     // metadata-only Event. The plaintext slice is the
                     // codec's return buffer; zeroing it removes the
                     // last in-process residency window for this event's
                     // sensitive bytes before the metadata_only Event
                     // is constructed and ack'd.
                     for i := range plaintext { plaintext[i] = 0 }
                     deliver Event{Payload: nil, MetadataOnly: true}
                     return  // stamp DenyAuditBackpressure metric
            v.   deliver Event{Payload: plaintext, MetadataOnly: false}
```

**Invariants this ordering enforces:**

- **No plaintext without permit (INV-9 / INV-17 / INV-18):** decrypt happens only inside step 3.d.iii — strictly after a Permit decision in 3.b.
- **No plugin plaintext without audit (INV-19):** for plugin recipients, EmitPluginDecrypt enqueue success in 3.d.iv is the gate that releases plaintext to step v. Enqueue failure (TOCTOU) overwrites the plaintext buffer in step 3.d.iv before constructing the metadata_only Event.
- **Bounded in-process plaintext residency:** the only window where decrypted bytes exist is between 3.d.iii (Decode) and 3.d.v (deliver). For non-plugin recipients (Character, Player), this window has no audit-emit step in between. For plugin recipients, the window includes the EmitPluginDecrypt enqueue (synchronous) — plaintext exists across that call, but the call is non-blocking by contract (`sync.Map` lookup + channel send).
- **Master spec §1 threat-model alignment:** this design does NOT defend against an operator running `gdb` on the live server (master spec line 80-81 explicitly relaxes that). What it does defend against is "plaintext leaked to a recipient process via the gRPC wire when AuthGuard didn't permit it" — the wire-property invariants.

**Why pre-check + post-check rather than only one:** the pre-check (AuthGuard's ShouldThrottle inside Branch 3) avoids decrypting bytes we'll immediately throw away — most-of-the-time savings under load. The post-check (EmitPluginDecrypt's enqueue contract) closes the TOCTOU between AuthGuard's pre-check and the actual enqueue. Without the post-check, a queue that crosses capacity in the gap would silently violate INV-19.

**Test invariant (INV-9 unit-test, Phase 3b Task 8):** for a sensitive event under a DEK whose participant set excludes the session's character, the wire-side `EventFrame.payload` MUST be empty AND `EventFrame.metadata_only == true`. Implementation MAY additionally assert that the `plaintext` byte slice (when accessible to the test via dependency injection) was zeroed after the TOCTOU branch fired — a residency-bound test for the §1 threat model.

**Why over alternatives:**

- *`OpenSession` keeps current signature; new method `OpenSessionAs(identity)` parallel:* two methods diverge over time; mocks have to implement both; "default identity" is a privilege-escalation footgun if Crypto.Enabled flips and the wrong method is called. One method, required parameter, no escape hatch.
- *Identity as a `SubscribeOption` (`WithSessionIdentity(s)`):* options are for tunables; identity is not a tunable. Required-by-construction.
- *AuthGuard wired only in Subscribe; history reader's fail-fast left in place until 3d:* two different decrypt seams to maintain in two separate PRs. The hot-tier reader is in scope for 3b because (1) its decode path is the symmetric counterpart to subscriber.go, (2) the AAD-rebuild logic is identical, (3) keeping the `EVENTBUS_HISTORY_SENSITIVE_NOT_SUPPORTED_PHASE3A` fail-fast in main longer is forward technical debt for no architectural gain.
- *Decrypt-then-conditionally-redact (decrypt first, drop plaintext on deny):* more memory churn under load (every event decrypts even on deny); larger plaintext residency window; harder to assert the residency-bound test invariant. The Check-first ordering above is preferred.

**Spec change:** none. Master spec §3 cold-tier flow already states that history is "same as Subscribe — same codec dispatch, same AuthGuard, same decrypt-or-metadata-only," and that hot/cold tier crossover is transparent under INV-21. Decision 5 implements that.

**Cost:** one signature change on the `Subscriber` interface. Test mocks update mechanically. The gRPC `Subscribe` and `QueryStreamHistory` handlers each gain Identity construction (one call to `NewCharacterIdentity`) at the authentication boundary. The `JetStreamSubscriber` and `hot_jetstream` implementations each gain a `WithAuthGuard(authguard.AuthGuard)` option to inject the dependency. Plus the order-of-operations sub-flow above (~25 LOC at each of the two callsites, with shared helper for the post-Permit decrypt-and-emit block).

---

## Decision 6 — `accesstypes.AccessRequest` gains caller-supplied `Attributes`

**Decision:** Phase 3b MUST extend `internal/access/policy/types/types.go::AccessRequest` to carry caller-supplied attributes:

```go
// Before (internal/access/policy/types/types.go:107-130)
type AccessRequest struct {
    Subject  string
    Action   string
    Resource string
}

func NewAccessRequest(subject, action, resource string) (AccessRequest, error)

// After (Phase 3b)
type AccessRequest struct {
    Subject    string
    Action     string
    Resource   string
    Attributes map[string]any  // NEW; nil for "no caller-supplied attributes"
}

func NewAccessRequest(subject, action, resource string, attrs map[string]any) (AccessRequest, error)
```

The pre-0.1.0 status of HoloMUSH means there are no shipped consumers to break. All existing call sites (verified at R3 design-time via `rg -l 'NewAccessRequest\(' --type go internal/ pkg/ cmd/ test/`: **13 files, 25 callsites**) are updated to pass `nil` for the new argument. No compat shim, no parallel constructor — single source of truth for request construction.

**Rationale:** master spec §7.2 Branch 3 calls `abac_engine.Evaluate(subject, action, resource, attributes={event_type, plugin_name, plugin_inst})` and §7.3's example moderation policy at line 1496-1507 uses `conditions: [{attribute: event_type, in: [...]}]`. Per-event-type attribute scoping is intrinsic to the master spec's policy model — without it, the system is RBAC-with-extra-steps, not ABAC. Server-side resolution via `Resolver.Resolve(entityRef)` (`internal/access/policy/attribute/resolver.go:152`) is structurally incapable of supplying per-call attributes (`event_type` isn't an attribute of an entity — it varies per Check call). Phase 3b is the moment the Branch 3 implementation needs this; deferring the substrate edit to a follow-up bead would mean the AuthGuard ships incomplete and the substrate work happens later under more time pressure with cold context.

**Substrate verification (Phase 3b grounding-doc revision):** the existing engine's resolver at `internal/access/policy/engine.go:227` returns `*types.AttributeBags` — a four-bag struct (`Subject`, `Resource`, `Action`, `Environment`) defined at `internal/access/policy/types/types.go:240-255`. There is NO flat-map overlay surface. A previous draft of this Decision punted with "merge over server-resolved bag" without specifying which bag.

**Substrate verification (R3 correction):** the resolver at `internal/access/policy/attribute/resolver.go:170` already populates `bags.Action["name"] = req.Action` on every Resolve call (this writes the **action verb** like `"decrypt"` or `"read"` into the bag). An earlier R2 draft incorrectly claimed "no current resolver populates `Action` attributes." The Action bag is NOT empty in production — it carries the action verb under the key `"name"`. The composition rule below preserves this invariant by reserving `"name"` as a server-only key.

**Composition rule (R3):** caller-supplied `req.Attributes` lands in the **`Action` bag** specifically. The reserved key `"name"` is server-only and `NewAccessRequest` MUST reject any caller-supplied `Attributes` map containing the `"name"` key with `ACCESS_REQUEST_RESERVED_ATTRIBUTE`. For all other keys, **caller wins on key conflict**, but in practice there is no other resolver-populated key in `bags.Action` today, so the conflict is theoretical until a future resolver adds Action-bag attributes.

```go
// Validate caller-supplied attributes do not collide with server-only
// reserved keys. ReservedActionKeys lists keys the resolver owns.
var reservedActionKeys = map[string]struct{}{
    "name": {}, // resolver writes req.Action verb into bags.Action["name"]
}

func NewAccessRequest(subject, action, resource string, attrs map[string]any) (AccessRequest, error) {
    // ... existing non-empty validation for subject/action/resource ...
    for k := range attrs {
        if _, reserved := reservedActionKeys[k]; reserved {
            return AccessRequest{}, oops.Code("ACCESS_REQUEST_RESERVED_ATTRIBUTE").
                With("key", k).
                Errorf("caller-supplied attribute key %q is server-reserved", k)
        }
    }
    return AccessRequest{Subject: subject, Action: action, Resource: resource, Attributes: attrs}, nil
}
```

```go
// In policy.Engine.Evaluate, after the resolver returns:
bags, resolveErr := e.resolver.Resolve(ctx, req)
if resolveErr != nil { /* ... existing fail-closed handling ... */ }

// Decision 6 (Phase 3b R3): caller-supplied per-call attributes overlay
// the Action bag. Caller wins on key conflict within Action; reserved
// keys (currently "name") are blocked at NewAccessRequest precondition.
// Other bags (Subject, Resource, Environment) are server-resolved only
// and never receive caller-supplied data.
//
// bags.Action is always non-nil here because the resolver allocates it
// at resolver.go:165 (NewAttributeBags-style initialization) and writes
// "name" at resolver.go:170. The defensive nil-init below is a guard
// for test paths that hand-construct AttributeBags{} with zero-value
// maps (see attribute/resolver_test.go for the test pattern); it is
// intentionally redundant on the production path.
if len(req.Attributes) > 0 {
    if bags.Action == nil {
        bags.Action = make(map[string]any, len(req.Attributes))
    }
    for k, v := range req.Attributes {
        bags.Action[k] = v
    }
}
```

**Rationale for the Action-bag choice:** every caller-supplied attribute Phase 3b's AuthGuard passes describes the **specific action being evaluated**, not the entity making the request:

- `event_type` ("core-comm:whisper") — what's being decrypted. Action context.
- `plugin_name` ("mod-filter") — which plugin is performing the decrypt. Could plausibly land in Subject (it's the principal), but the principal is already named in `req.Subject` ("plugin:mod-filter") — duplicating it in `bags.Subject` adds noise.
- `plugin_inst` ("01INST") — which instance of that plugin. Same argument: instance is action-scoped (this specific decrypt call).

All three are per-call qualifiers of the action verb, not stable entity attributes. The Action bag is the natural home.

**DSL-author impact:** policies condition on caller-supplied attributes via the `action.<name>` namespace path. The example moderation policy at master spec §7.3 line 1496-1507 becomes:

```cedar
permit(principal is plugin, action in ["decrypt"], resource is stream)
  when { resource.stream.name like "dek:dm:*"
       && action.event_type in ["core-comm:whisper", "core-comm:dm"] };
```

The DSL parser at `internal/access/policy/dsl/evaluator.go:438` resolves `action.<name>` references against `bags.Action[<name>]`. Existing seed policies at `internal/access/policy/seed.go:34-209` reference `principal.X` and `resource.X` extensively but never `action.X` (other than the `action in [...]` action-verb predicate, which is a separate DSL form that matches `req.Action` directly, not the bag). Phase 3b's policies will be the first regular consumers of `action.<name>` attribute references; the parser surface already supports them.

**Rationale (general):** server-resolved attributes describe the *entity* (subject/resource); caller-supplied attributes describe the *call context* (this specific decryption). When they collide on a non-reserved key, the call context is more specific and wins. This matches AWS IAM context-keys composition, OpenPolicyAgent input vs data, and XACML's request context vs subject attributes.

**AuthGuard's Branch 3 use:**

```go
// Inside authguard.Guard.Check, Branch 3 (Plugin):
req, err := accesstypes.NewAccessRequest(
    "plugin:" + identity.PluginName,
    "decrypt",
    "dek:" + ctxType + ":" + ctxID,
    map[string]any{
        "event_type":     checkReq.EventType,         // "<plugin>:<event>"
        "plugin_name":    identity.PluginName,
        "plugin_inst":    identity.InstanceID,
    },
)
if err != nil { /* ... */ }
abacDecision, err := g.abac.Evaluate(ctx, req)
```

This shape is exactly what master spec §7.2 Branch 3 names. The example moderation policy at master spec §7.3 line 1496-1507 (which uses `attribute: event_type in [...]`) becomes implementable as written.

**Why over alternatives:**

- *Defer to a Phase 3+ follow-up bead (scope-down for 3b):* would ship Phase 3b's AuthGuard with no per-event-type scoping, leaving the master spec's stated policy model unimplemented. The substrate edit happens regardless; the only question is timing. Deferral means design-context decay and a forced refactor later.
- *Encode attributes into the resource string (`dek:scene:01ABC:event_type=core-comm:whisper`):* breaks §7.3's `dek:<context_type>:<context_id>` resource shape, breaks the `dek:scene:*` wildcard pattern, makes policy authoring substantially uglier. ABAC engines exist to avoid exactly this kind of string-mangling.
- *Tri-state merge rule (server wins / caller wins / must-be-disjoint):* over-design. Caller-wins is the natural rule and what every mainstream attribute system does. Disjoint-only is too rigid and would break legitimate cases like a caller refining a server-resolved attribute.
- *Pass attributes as a separate parameter to `Engine.Evaluate(ctx, req, callerAttrs)`:* widens the call signature; splits a single conceptual request into two values that have to travel together through logging, auditing, and tests. The map field on the request type is the natural carrier.

**Existing-callsite enumeration (call site count and update shape):**

| Surface | Count | Update |
|---|---|---|
| Production callsites of `NewAccessRequest(...)` (R3 rg-verified: `rg -l 'NewAccessRequest\(' --type go internal/ pkg/ cmd/ test/`) | 25 callsites across 13 files | Each gets a `nil` 4th argument. Mechanical. |
| `accesstypes.AccessRequest` struct | 1 | Add `Attributes map[string]any` field. |
| `accesstypes.NewAccessRequest` constructor | 1 | Add 4th parameter; preserve validation rules for the three string fields; nil/empty map is valid. |
| `policy.Engine.Evaluate` (composition) | 1 | After resolver returns server-attributes, overlay caller-supplied via map merge (caller wins on conflict). ~10-15 LOC. |
| Existing `policy.Engine` mocks (`policytest.GrantEngine`, `policytest.NewErrorEngine`, `policytest.MockAccessPolicyEngine`) | 3 | Already accept `AccessRequest` by value; the new field is read transparently. No mock signature changes. |
| Existing engine tests | TBD | New tests added for composition behavior (caller-wins on conflict, nil-map no-op, server-resolved-only when no caller attrs). ~3 new test cases. |

**Spec change:** master spec §7.2 Branch 3 pseudocode and §7.3 example policy stay as written — they describe the post-edit shape correctly. This grounding doc records that the substrate previously did not support that shape; Phase 3b's substrate edit closes the gap. The "Master spec edits required" table below grows an entry noting the substrate evolution.

**Cost:** ~80-120 LOC across `types/types.go`, `policy.Engine.Evaluate`, and tests. Plus 25 mechanical call-site updates (R3 rg-verified: 25 callsites across 13 files; each becomes `nil` 4th argument). Plus mockery regeneration via bare `mockery` (the project does NOT have a `task mockery` Taskfile target; per-`.mockery.yaml`-config, bare invocation is the convention). No external-API breakage because there is no external API yet (pre-0.1.0).

---

## Decision 7 — `player_character_bindings` substrate (binding entity)

**Decision:** Phase 3b MUST introduce a `player_character_bindings` table and its Go store layer as a prerequisite for AuthGuard's character-branch (§7.2 Branch 1) `binding_id` lookup. Master spec §4.3a defines the entity, schema, and lifecycle. This Decision records the Phase 3b grounding-doc rationale for why the substrate edit lands in 3b rather than later.

**The gap surfaced by plan-reviewer Round 1.** Master spec §6.1 lines 1206-1219 talk about `binding_id` like it's existing prior-art substrate. Phase 2 grounding's `dek.Participant.BindingID` field at `internal/eventbus/crypto/dek/store.go:31` was added on the spec's authority. `rg "BindingID" --type go internal/` returns exactly ONE hit (the Phase 2 field). The session/auth pipeline carries `(player_id, character_id, player_session_id)` but no `binding_id`. The Phase 3b first-pass plan inherited the assumption that "the gRPC handler reads `(player_id, character_id, binding_id)` from session" without verifying.

**Why a separate entity, not a column on `characters`:**

- The `characters.player_id` FK at `internal/store/migrations/000001_baseline.up.sql:70` is a single mutable reference; UPDATE-on-rebind loses the previous tenure. INV-10 ("after rebind, the new player MUST NOT receive plaintext for events emitted before the rebind") requires the previous tenure's identity to remain queryable indefinitely.
- A binding is a long-lived player↔character tenure (weeks/months), not an ephemeral session (minutes/days). Conflating with `session.Info.ID` would falsely terminate the binding every time a player disconnects.
- Wizard transfers will (Phase 4+) produce audit records with binding_ids on both sides ("wizard W transferred character X from binding A→B at T"). The natural correlation key is the binding row id. Without a table now, we'd re-invent it later.
- Forward-compat with binding-level features (per-binding permissions, binding-scoped quotas, transfer-history admin UI) — all natural if the binding is a first-class entity.

**Substrate footprint (Phase 3b):**

| File | Change |
|---|---|
| `internal/store/migrations/000015_create_player_character_bindings.up.sql` | NEW. CREATE TABLE per master spec §4.3a; UNIQUE INDEX on `(character_id) WHERE ended_at IS NULL`; INDEX on `(player_id) WHERE ended_at IS NULL`; **back-population**: `INSERT INTO player_character_bindings (id, player_id, character_id, created_at) SELECT generate_ulid()::TEXT, player_id, id, NOW() FROM characters WHERE player_id IS NOT NULL`. **Caveat:** the existing `characters` schema at `internal/store/migrations/000001_baseline.up.sql:70` permits NULL `player_id` (`player_id TEXT REFERENCES players(id)` with no NOT NULL constraint). Orphan characters with NULL player_id are excluded from the back-population — they have no binding. Subscribe with such a character will return `BINDING_MISSING` until either the character is bound to a player (via Phase 4 wizard-transfer) or deleted. The Phase 3b non-goal section explicitly carries this edge case forward to Phase 4. |
| `internal/store/migrations/000015_create_player_character_bindings.down.sql` | NEW. DROP TABLE, DROP INDEXes. |
| `internal/store/binding_store.go` | NEW. Methods (string-throughout, ULID-formatted strings — matches the existing `dek.Participant.BindingID string` pattern at `internal/eventbus/crypto/dek/store.go:31`): `Bindings.Current(ctx context.Context, characterID string) (string, error)` — returns active binding id for a character (or `BINDING_NOT_FOUND` if none); `Bindings.Create(ctx context.Context, playerID, characterID, reason string) (string, error)` — inserts a new active binding (used by character-creation flow); `Bindings.End(ctx context.Context, bindingID, reason string) error` — sets `ended_at` on a binding (used by Phase 4 wizard-transfer flow; included in 3b for API-completeness even though no production caller exists yet — tests are the only enforcement until Phase 4 lands). All methods use `execerFromCtx(ctx, r.pool)` so callers may compose them inside a transaction via the existing Tx-via-context pattern at `internal/world/postgres/helpers.go:42`. Typed errors: `BINDING_NOT_FOUND`, `BINDING_ALREADY_ENDED`. |
| `internal/store/binding_store_test.go` | NEW. Unit tests against the existing testpool harness used by other store tests; Tx-composition test confirming Create+failure → no binding row persisted. |
| `internal/world/postgres/character_repo.go::Create` (line 46-56) | MODIFY. Change `r.pool.Exec(ctx, ...)` to `execerFromCtx(ctx, r.pool).Exec(ctx, ...)` — single-line change matching the existing `Delete` method's pattern at `character_repo.go:76`. This makes the existing `Create` Tx-composable, satisfying the atomicity invariant when paired with `Bindings.Create` in the same Tx. |
| `internal/grpc/auth_handlers.go::CreateCharacter` (the regular character-creation gRPC path, around line 369) | MODIFY. Wrap the existing `characterRepo.Create` call in a `pool.BeginTx → ctx-with-tx → defer Commit/Rollback` block; immediately after `characterRepo.Create(txCtx, char)` succeeds, call `bindingStore.Create(txCtx, char.PlayerID.String(), char.ID.String(), "initial_bind")`. Both INSERTs commit atomically. |
| `internal/auth/guest_service.go::CreateGuest` (around line 113 — the unified-guest-auth path landed in PR #181) | MODIFY. Same shape as `CreateCharacter`: wrap `s.chars.Create(ctx, char)` (line 113) in a `pool.BeginTx → ctx-with-tx → defer Commit/Rollback` block; immediately after the character INSERT succeeds, call `bindingStore.Create(txCtx, player.ID.String(), char.ID.String(), "initial_bind_guest")`. The existing best-effort cleanup at line 116 (delete orphaned player row) becomes a transaction rollback in the new shape, removing the "best-effort cleanup that may fail" code path entirely. **This is the second character-creation site; without it, guest characters land in `characters` with no row in `player_character_bindings`, breaking AuthGuard's character branch under Phase 3d's flag flip.** |
| Atomicity claim (with both paths covered) | Once both production character-creation paths above are wrapped in transactions, the orphan-character case (one row in `characters`, zero rows in `player_character_bindings`) is structurally impossible for newly-created characters. Pre-existing orphans (NULL `player_id` from before the player_id column existed) remain edge-case-only and are excluded from back-population per the migration's `WHERE player_id IS NOT NULL` filter. |
| `internal/grpc/server.go::Subscribe` (Decision 2 detail) | MODIFY. Add `bindings` field on `Server` struct; constructor takes binding store; Subscribe handler queries `s.bindings.Current(ctx, info.CharacterID.String())` after `s.sessionStore.Get(ctx, req.SessionId)` at line 679; passes returned binding_id to `NewCharacterIdentity`. Same for `internal/grpc/query_stream_history.go`. The `.String()` conversion is the explicit boundary between `info.CharacterID ulid.ULID` (per `internal/session/session.go:135-138`) and `binding_store.Current(ctx, string)` (string-throughout convention). |
| `cmd/holomush/main.go` (or wherever the server is wired) | MODIFY. Inject `*store.BindingStore` into the gRPC server constructor. |

**Out-of-scope wiring deferred to Phase 4 (`holomush-fi0n`):**

- Wizard-transfer command handler. The `Bindings.End` + `Bindings.Create` pair lives in this transaction, alongside the Rotate trigger per master spec §6.2. Phase 3b ships the API; Phase 4 ships the only callers.
- Character-deletion path (`Bindings.End(reason="character_deletion")`). Same shape; deferred for the same reason.
- `Add(participant)` callers in scene/DM/channel join flows. These code paths don't exist as crypto-aware yet — they materialize when the Lifecycle plumbing in Phase 4 lands. Phase 3b's `Bindings.Current` API gives them the lookup they need; the caller code is Phase 4.

**Atomicity:** the character-creation INSERT and the binding INSERT MUST run in the same transaction. A character with no active binding row is an invariant violation that breaks Subscribe (handler returns `BINDING_MISSING`). The migration's back-population step also uses a single transaction.

**Why not defer to Phase 4 or a follow-up bead:**

- AuthGuard's character branch is unimplementable without `binding_id`. Phase 3b cannot ship a working AuthGuard otherwise.
- Pre-0.1.0: schema cost is essentially free (back-population is a single INSERT-from-SELECT statement).
- Phase 4's wizard-transfer work needs the table anyway. Building it once, in 3b, scoped to "AuthGuard needs a working source," produces less churn than introducing it twice.
- The user's stated discipline: "solve now despite blast radius rather than push things off."

**Spec change:** master spec gains a new §4.3a (`player_character_bindings`) defining the entity. Master spec §6.1 step 2 of `Add(participant)` mechanics gains an explicit `Bindings.Current(character_id)` resolution before the participant append. Both edits ship alongside this grounding doc. Phase 3b inherits a coherent spec.

**Cost:** new migration (~30 LOC), new store layer with three methods + tests (~150 LOC), character-creation site edit (~10 LOC + test), gRPC handler edits per Decision 2 (~16 LOC across Subscribe + QueryStreamHistory), server-construction wiring (~5 LOC). Total ~200 LOC of new substrate.

---

## Master spec edits required

| Master spec section | Edit |
|---|---|
| §4.1 lines 492-506 | The `bool metadata_only = 7` proto field belongs on `corev1.EventFrame` (per-delivery wire shape) and not on `eventbusv1.Event` (storage shape). The semantic properties the section already lists ("set by host before sending to client; never set by emitter; never persisted to events_audit") are exactly per-delivery semantics. The intent is correct; the proto location is wrong. Phase 3b implements as `EventFrame.metadata_only = 10`. (Field 7 on `eventbusv1.Event` is *already* taken by `rendering` per `api/proto/holomush/eventbus/v1/eventbus.proto:44`, added by the gateway-verb-registry-sourcing work — independent reason the master spec's proposed location can't stand.) |
| §3 emit flow box (line 388-396) | Already correct (`Codec.Encrypt(payload, DEK, AAD=metadata)` is logical, not Go). Phase 3b's Decision 0 restoration aligns the implementation with the description. |
| **§4.3a** (NEW) | **APPLIED in this revision.** New section between §4.3 and §4.4 defining the `player_character_bindings` entity, schema, and lifecycle. Required by Decision 7; the spec previously used `binding_id` without defining the substrate that supplies it. |
| §6.1 `Add(participant)` mechanics | **APPLIED in this revision.** Step 2 amended to read `Bindings.Current(character_id)` (§4.3a) before the participant append; step originally numbered 2 becomes step 3; original 3 becomes 4. Failure mode `BINDING_MISSING` documented. |
| §5.1 (ABAC engine surface) | Add a note that `accesstypes.AccessRequest` carries an `Attributes map[string]any` field (Phase 3b Decision 6) so §7.2 Branch 3's `attributes={event_type, plugin_name, plugin_inst}` and §7.3's `conditions: [{attribute: ..., in: [...]}]` example are implementable as written. Composition rule (Decision 6 revised): caller-supplied attributes overlay the **`Action` bag** specifically; caller wins on key conflict within the `Action` namespace; other bags (Subject / Resource / Environment) are server-resolved only. Policies reference attributes via the `action.<name>` namespace path. |
| §11.1 Phase 3b row | Add reference to this grounding doc. Note that the binding-substrate edit (Decision 7 / §4.3a) lands in 3b as a prerequisite to AuthGuard's character branch. |
| §11.1 phasing granularity | **R3 decision:** add a one-line note at the bottom of §11.1 stating "Phase 3 sub-phasing (3a/3b/3c/3d) is tracked in the bead tree (`holomush-ojw1.1`/`.2`/`.3`/`.4`) and the grounding docs in this repo; the §11.1 row remains a single Phase 3 entry by design." This is less master-spec churn than expanding the row, and avoids requiring a master-spec edit every time a sub-phase ships. Apply this edit alongside Phase 3b execution. |

No invariant edits. INV-1, INV-21, INV-25, INV-26 (metadata-only delivery contract) remain as written. Decision 4 is consistent with INV-26 — INV-26's "populated cleartext metadata, and no ciphertext" is exactly what `EventFrame` carries.

---

## Bead updates required

| Bead | Change |
|---|---|
| `holomush-ojw1.2` (Phase 3b sub-epic) | Description: add Decision 0 (wire-shape restoration), Decision 6 (`accesstypes.AccessRequest` extension), and Decision 7 (`player_character_bindings` substrate) as substrate prerequisites to AuthGuard work. Add reference to this grounding doc as the design source-of-truth. Remove the description's "metadata_only delivery flag (proto field added in Phase 1; wire it now)" clause — the field is added in 3b per Decision 4; Phase 1 didn't touch protos. |
| `holomush-ojw1.2.N` (sub-tasks; numbering TBD by writing-plans) | Created during Phase 3b's plan-writing pass; this grounding doc constrains scope. Task list (consumes plan-reviewer Round 1 findings + Decision 7 substrate): **T0** (preflight); **T1** (Decision 0 wire-shape restoration); **T2** (Decision 7 `player_character_bindings` table + store + character-creation site edit + tests); **T3** (Decision 6 `AccessRequest.Attributes` Action-bag overlay + 25 callsite updates + composition tests); **T4** (`dek.Manager.Participants` substrate + integration test only — no fake unit tests since `dek.Store` is a concrete struct without an interface); **T5** (`EventFrame.metadata_only = 10` proto + `Delivery.MetadataOnly()` interface); **T6** (`authguard` package: `Identity` types + `Decision` + `AuthGuard.Check` four-branch impl using `idgen.New()` for grant_id and `accesstypes.Decision.PolicyID()` where ABAC trace data is needed; INV-43 unit-test for operator-deny); **T7** (`*plugin.Manager.PluginRequestsDecryption(plugin, eventType) bool` — production substrate satisfying the `ManifestLookup` interface; walks `Crypto.Consumes[].RequestsDecryption[]` matching the qualified `<plugin>:<event_type>` form per `crypto_validator.go:60-93`); **T8** (`DecryptAuditEmitter` audit subpackage + `WithGameID(string)` option — no hardcoded "holomush"); **T9** (subscriber + hot_jetstream wiring + dual-callsite + order-of-operations sub-flow including TOCTOU plaintext-zeroing; corrected callsite enumeration: 35 occurrences across 13 files including `bus_test.go`, `publisher_test.go`, `server_helpers_test.go`, `auth_suite_test.go`, `multi_protocol_fanout_test.go`, `reconnect_resume_test.go`); **T10** (gRPC `Subscribe` + `QueryStreamHistory` handlers: read `info`, query `bindings.Current`, construct `Identity`, pass to `OpenSession`; stamp `EventFrame.metadata_only` from `delivery.MetadataOnly()`); **T11** (default-deny `audit.>` ABAC policy via real `policy.SeedPolicies()` mechanism + a new SQL migration to land the policy on existing deployments — NOT the fabricated `setup.RegisterDefaultPolicies` mechanism); **T12** (integration tests using existing `testutil.SharedPostgres` / `StartEmbeddedJetStream` / `RandomKEKHex` harness pattern from `test/integration/crypto/emit_test.go`; per-test bring-up replacing the fabricated `SubscribeAs`/`PrepDEKWithParticipants`/`PublishSensitive` helpers; covers INV-8, INV-9, INV-10, INV-17, INV-18, INV-19, INV-20, INV-22, INV-25 (under restored wire shape), INV-26). Plus a small **T13** (meta-test enforcing INV-N ↔ test-name binding for the integration tests Phase 3b adds; deferred to follow-up bead per plan-reviewer #16 if scoping bites). Each integration test starts with `// Verifies: INV-N`. |
| `holomush-ojw1.4` (Phase 3d) | Already lists "NATS deny rules" + cold-tier + flag flip — no edit needed; this doc just confirms the split. |
| `holomush-ojw1.4.1` (wire-side `Sensitive` surfacing on Lua + binary SDK) | Independent of 3b. Confirmed not a 3b prerequisite — 3b decrypts events the host has already encrypted via codec; plugin-claimed sensitivity isn't on the 3b decrypt path. |

The Phase 3b plan (`docs/superpowers/plans/2026-05-02-event-payload-crypto-phase3b-...`) is to be written against this grounding by the next `superpowers:writing-plans` pass, after design-reviewer approval.

---

## Out of scope

Phase 3b does NOT address:

- Cold-tier (`events_audit`) sensitive read path — Phase 3d (`holomush-ojw1.4`).
- DEK cache invalidation across replicas (INV-28 / INV-29) — Phase 3c (`holomush-ojw1.3`).
- NATS account-level deny rules for `audit.>` namespace (INV-15 / INV-52 architectural source of truth) — Phase 3d.
- Operator break-glass (`AdminReadStream`, §7.5) — Phase 5.
- Rekey audit emission (`audit.<game>.system.rekey.*`) — Phase 5.
- Player-history-read audit (`audit.<game>.system.player_history_read`, INV-51) — Phase 4 (`Add`/lifecycle).
- DEK rotation / Rekey lifecycle operations — Phase 4 / Phase 5.
- Vault provider — Phase 6.
- Plugin SDK helpers and plugin-owned audit (`PluginAuditService.QueryHistory`) — Phase 7.
- Site documentation (`site/docs/extending/event-sensitivity.md`, etc.) — Phase 8.
- Plugin runtime construction of `Plugin`-kind Identity for plugin event consumers — Phase 7.
- Wire-side `Sensitive` surfacing on Lua + binary SDK (`holomush-ojw1.4.1`) — independent track.
- The `Crypto.Enabled` flag flip — Phase 3d, gated on 3b + 3c + 3d completion.
- Wizard-transfer command handler that calls `Bindings.End` + `Bindings.Create` + DEK `Rotate` in one transaction — Phase 4 (`holomush-fi0n`). Phase 3b ships the `Bindings.End` API for completeness (Create+End is the natural API pair); tests are the only enforcement until Phase 4 wires production callers.
- Character-deletion path that calls `Bindings.End(reason="character_deletion")` — Phase 4. Phase 3b's substrate supports it; the caller is deferred.
- `Add(participant)` callers in scene/DM/channel join flows — Phase 4. Phase 3b's `Bindings.Current` API gives them the lookup they need; the callers materialize when DEK lifecycle plumbing in Phase 4 lands.
- Orphan-character handling (characters with `player_id IS NULL` in the existing schema) — Phase 4. The Phase 3b back-population migration excludes these via `WHERE player_id IS NOT NULL`; AuthGuard returns `BINDING_MISSING` for any Subscribe attempt against an orphan character. Phase 4 will either bind orphans during a wizard-transfer or delete them; until then, Phase 3b ships dark, so the gap has no user-visible effect.
- **Backfill prerequisite for the Phase 3d `Crypto.Enabled` flag flip:** every active session row MUST have a non-zero `player_id` before Crypto.Enabled goes true. Legacy sessions created before the `player_id` column existed (per `internal/store/session_store.go:56`) would otherwise return `AUTHGUARD_IDENTITY_INVALID` from `NewCharacterIdentity` once the flag flips. Phase 3d carries the backfill migration as its own gate.

---

## References

- Master spec: [`2026-04-25-event-payload-crypto-design.md`](2026-04-25-event-payload-crypto-design.md)
- Phase 3a grounding: [`2026-05-02-event-payload-crypto-phase3a-grounding.md`](2026-05-02-event-payload-crypto-phase3a-grounding.md)
- Phase 3a plan (executed, PR #3514): [`../plans/2026-05-02-event-payload-crypto-phase3a-codec-emit.md`](../plans/2026-05-02-event-payload-crypto-phase3a-codec-emit.md)
- JetStream substrate spec: [`2026-04-18-jetstream-event-log-design.md`](2026-04-18-jetstream-event-log-design.md)
