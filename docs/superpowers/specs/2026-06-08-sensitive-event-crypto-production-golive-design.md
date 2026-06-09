<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Production Sensitive-Event Crypto Go-Live — Design

| | |
| --- | --- |
| **Bead** | holomush-5rh.8.29 (P1 bug) |
| **Epic** | holomush-5rh.8 — [E9.5] Web Portal: Scenes — Player Workspace |
| **Status** | Draft (design-review pending) |
| **Date** | 2026-06-08 |
| **Master spec** | [docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md](2026-04-25-event-payload-crypto-design.md) |
| **Follow-up** | holomush-jq34t (comms sender-echo symmetry, out of scope) |

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, **MAY** are
to be interpreted per RFC 2119 / RFC 8174 (root `CLAUDE.md` "RFC2119 Keywords").

---

## 1. Problem

The production live Subscribe/Publish fan-out path was **never wired** for
sensitive-event encryption/decryption. Two grounded facts establish the gap:

1. **Publisher has no DEK manager.** `cmd/holomush/sub_grpc.go` constructs the
   live publisher via `EventBus.Publisher()` with **no** `WithDEKManager`
   option. Consequently `publisher.Publish` hits the fail-closed reject for any
   event with `Sensitive=true`: `EVENTBUS_SENSITIVE_EVENT_NO_DEK_MANAGER`
   (`internal/eventbus/publisher.go:226`). The event is **dropped, never
   published** — confirmed fail-closed (no plaintext fallback).

2. **Subscriber has no AuthGuard.** The live subscriber is built via
   `EventBus.Subscriber()` with **no** `WithSubscriberAuthGuard` /
   `WithSubscriberDEKManager` / `WithSubscriberDecryptAuditEmitter`. The
   decode path therefore falls into the `guard == nil` branch
   (`internal/eventbus/subscriber.go:514`), which the master spec marks as the
   pre-flag-flip placeholder: *"Phase 3d's flag flip wires AuthGuard in
   production"* (`subscriber.go:517`).

The crypto stack (`historyAuthGuard` / `historyDEKMgr` / `historyAuditEm`) was
wired into the **history reader only** (`QueryStreamHistory`), never the live
fan-out. The master spec lists Phase 3d "decrypt-on-fan-out" as COMPLETE
(§2444) — the *capability* exists in `subscriber.go::decodeAndAuthorize`, but
the production *wiring site* never flipped the flag. **This bead is that flip.**

A second, independent gap: even with the wiring flipped, **zero production
callers of `dek.Manager.Add` exist** — the participant set of every DEK context
is created empty (`publisher.go:215` calls `GetOrCreate(ctx, ctxID, nil)`). The
master spec anticipated this as the deferred *"Phase 4 `Add(participant)`
caller"* (§304 / INV-59; INV-12). With an empty set, the AuthGuard denies every
subscriber → metadata-only delivery for everyone.

### 1.1 Blast radius — all sensitive events, not just scenes

The gap is **global**, not scene-specific. Every plugin emitting
`sensitivity: always` events is affected:

| Plugin | Event types (`sensitivity: always`) | Manifest |
| --- | --- | --- |
| `core-scenes` | `scene_pose`, `scene_say`, `scene_emit`, `scene_ooc` | `plugins/core-scenes/plugin.yaml` |
| `core-communication` | `page`, `whisper`, `pemit` | `plugins/core-communication/plugin.yaml:294-303` |

Both plugins are discovered and loaded by the production plugin subsystem
(`Manager.Discover` + `LoadAll`). Net: **no production sensitive-event feature
ever worked on the live path** — scene RP *and* private messaging both
fail-closed at publish today.

---

## 2. The crypto model (grounded recap)

- **Per-context DEK.** A sensitive event is encrypted at publish under a Data
  Encryption Key scoped to a *context*. The context ID is derived from the
  event subject: `contextIDFromSubject` (`publisher.go:498`) maps the
  NATS-native subject `events.<game>.<Type>.<id>[…]` to
  `dek.ContextID{Type, ID}`. So `scene.<id>` → `{scene, id}`;
  `character.<id>` → `{character, id}`.
- **DEK genesis is lazy.** When the publisher encrypts and no active DEK row
  exists, `dek.Manager.GetOrCreate(ctx, ctxID, initial)` mints a v1 DEK with
  the supplied `initial` participant set (`manager.go` GetOrCreate). Production
  passes `nil` today (`publisher.go:215`).
- **Participant set governs decryption.** On live fan-out, the AuthGuard
  delivers plaintext only to a subscriber whose character is in the DEK's
  participant set (master INV-8, §229); a non-participant subscribed to the
  same subject receives **metadata-only** (empty payload) — the no-leak
  guarantee (master INV-9 / registry **INV-CRYPTO-6**, §230;
  `subscriber.go:505` `decodeAndAuthorize`).
- **Seeding.** `dek.Manager.Add(ctx, ctxID, Participant)` appends a character
  to a context's participant set (`manager.go:329`). It is **idempotent**: the
  store returns `added=false` when the participant is already present and `Add`
  returns nil (`manager.go:357`). `Add` resolves an empty `BindingID` via the
  `BindingResolver` (`manager.go:344`). Master INV-12 / registry
  **INV-CRYPTO-7**: `Add` grants immediate read access to existing DEK history
  without rotating (already `bound`).

---

## 3. Design

Three parts. Part 1 is global and unblocks both features at the wire. Parts 2
and 3 seed participants for the two features, using two different triggers that
follow from two different access patterns.

### 3.1 Part 1 — Global flag-flip (production wiring)

In `cmd/holomush/sub_grpc.go::Start`:

- The live **Publisher** MUST be constructed with `WithDEKManager(RekeyManager)`
  when `RekeyManager != nil`. When `RekeyManager == nil` (no KEK configured),
  the publisher MUST remain in its current plaintext-only mode — a KEK-less
  deployment is a supported degraded posture and MUST NOT fail to start.
  > **Superseded (2026-06-09):** the KEK-less degraded posture is retired by
  > [the sensitive-event crypto activation design](2026-06-09-sensitive-event-crypto-activation-design.md)
  > — a KEK is now REQUIRED to boot (`BOOT_KEK_REQUIRED`, INV-CRYPTO-119);
  > `--auto-gen-kek` provisions the keyfile on first start. The
  > `RekeyManager == nil` publisher branch survives only as a defensive
  > guard, never as a supported deployment mode.
- The live **Subscriber** MUST be constructed with
  `WithSubscriberAuthGuard(historyAuthGuard)` +
  `WithSubscriberDEKManager(historyDEKMgr)` +
  `WithSubscriberDecryptAuditEmitter(historyAuditEm)` when the auth guard is
  present (i.e. KEK configured). When absent, the bare subscriber preserves
  the pre-flag-flip passthrough (`subscriber.go:514`).
- The subscriber MUST be constructed **after** the AuthGuard/DEK-manager/audit
  emitter are resolved (these are assembled for the history reader today;
  Part 1 reuses the same resolved instances for the live path so the live and
  cold tiers share one AuthGuard).

This is a wiring-only change; it introduces no new crypto logic. `RekeyManager`
already carries the full `dek.Manager` interface (`sub_grpc.go:96`) and is set
from the rekey worker (`cmd/holomush/core.go:877`).

### 3.2 Part 2 — Scene reader-seeding (broadcast / explicit-join model)

Scenes are a **broadcast** context: one DEK (`{scene, sceneID}`), many readers
(participants + observers), each of whom *subscribes by joining focus*. A reader
is not knowable at emit time, so seeding MUST happen **when the reader joins**.

`internal/grpc/sceneaccess_service.go::SetSceneFocus` already establishes scene
focus after a participation gate (`ListCharacterScenes` membership check) and an
idempotent `JoinFocus`. After that gate passes and before `SetConnectionFocus`,
`SetSceneFocus` MUST seed the focusing character as a DEK participant on
`{scene, sceneID}` via the injected adder (`dek.Manager.Add`), when the adder is
present (KEK configured).

**Add-failure is FATAL.** If the seeding `Add` returns an error,
`SetSceneFocus` MUST return `codes.Internal` and MUST NOT call
`SetConnectionFocus` — the connection is **not** focused. Rationale: this
yields a clean, testable guarantee — *a web connection is focused on a scene
only if its character can decrypt that scene's events.* Refusing focus on a
seed failure is preferable to silently focusing a connection that will receive
blank poses; the client surfaces the error and retries, and `Add` idempotency
(`manager.go:357`) makes retry a safe no-op. (This **supersedes** the WIP's
non-fatal Warn-log behavior.)

Ordering MUST be: participation gate → `JoinFocus` → `Add` (fatal) →
`SetConnectionFocus`. A fatal `Add` after a successful `JoinFocus` leaves a
`FocusMembership` without connection focus; this is benign (membership without
connection focus produces no fan-out) and self-heals on retry.

### 3.3 Part 3 — Comms recipient-seeding (point-to-point / personal-stream model)

page/whisper/pemit are **point-to-point**: each emits a single sensitive event
to `character.<recipientID>` (`plugins/core-communication/main.lua:276,398,443`)
→ context `{character, recipientID}`. The recipient reads their **own** personal
stream — there is no "join focus" hook to seed at, and the recipient *is*
derivable from the subject at the publish boundary.

Therefore the host MUST seed the **subject-derived recipient** as the initial
DEK participant at the publisher's genesis boundary. Concretely: where the
publisher derives `ctxID` from the subject and calls `GetOrCreate(ctx, ctxID,
nil)` (`publisher.go:209,215`), it MUST instead derive a non-empty initial
participant set for `character.<id>` contexts — `[{CharacterID: id}]` — and
pass it to `GetOrCreate`. Other context types (`scene.<id>`, host-owned
`location.<id>` / `session.<id>`) MUST keep `nil` (readers seeded elsewhere or
non-sensitive).

`★` **Why this placement** — `publisher.Publish` is the single encryption
boundary every runtime flows through (Lua and binary both reach it via
`internal/plugin/event_emitter.go::Emit`). Seeding here means the host seeds
uniformly and **no new `PluginHostService` RPC, Lua hostfunc, or Go SDK method
is required** — plugin-runtime-symmetry (`.claude/rules/plugin-runtime-symmetry.md`)
is satisfied by construction, the gate living at the common path. It also seeds
the recipient **atomically with DEK genesis**: there is no window where the
context exists with an empty set, and no per-message `Add` on the hot path
(subsequent messages to the same character find the existing DEK with the
recipient already present).

**Recipient-only.** Only the recipient touches the crypto path. The sender's
view ("You paged Bob: …") is the command's direct text `output`
(`main.lua:275-277`, `ok_events(events, formatted_for_sender)`) rendered locally
— it never enters the event stream and is never decrypted. Seeding the sender on
the recipient's context would be an **over-grant** (it would let the sender
decrypt *every* private message anyone sends that recipient) and MUST NOT be
done. Sender-readable outbox/history is a separate feature tracked as
holomush-jq34t; if it lands by emitting a sender-copy to `character.<senderID>`,
the same genesis rule seeds the sender automatically.

**Add-failure is FATAL — for free.** A failed initial-seed is a failed
`GetOrCreate` is a failed `Publish` (`publisher.go:215`): the sender's command
returns an error and no undeliverable encrypted event is published. This
matches the Part 2 fatal posture with zero extra logic.

### 3.4 The one implementation fork (crypto-reviewer territory)

`GetOrCreate`'s `initial []dek.Participant` is INSERTed as-is into the
`crypto_keys` row; it does **not** run the `BindingResolver` that
`dek.Manager.Add` runs (`manager.go:344`). A `Participant{CharacterID: id}` with
an empty `BindingID` passed to `GetOrCreate` would therefore store an *unbound*
participant, which the AuthGuard check may reject. The plan MUST resolve this
one of two ways; the choice is a plan-level decision for crypto-reviewer:

- **(A)** Resolve the recipient's `BindingID` before genesis (the publisher
  gains a `BindingResolver` dependency), then pass a fully-bound participant to
  `GetOrCreate`. Atomic, single write.
- **(B)** Keep `GetOrCreate(ctx, ctxID, nil)` and immediately follow with
  `dek.Manager.Add(ctx, ctxID, {CharacterID: id})`, letting `Add` resolve the
  binding. Two writes, reuses the existing resolver path.

The spec mandates the *outcome* (recipient is a **bound** participant at the
moment of first delivery); the plan picks the mechanism.

---

## 4. Two seeding triggers — rationale (not redundancy)

| Feature | Context shape | Reader known at… | Seed trigger |
| --- | --- | --- | --- |
| Scenes | `{scene,id}` broadcast, many readers | join (focus) | **reader-side** at `SetSceneFocus` |
| Comms | `{character,id}` personal stream, one reader | emit | **genesis-side** at `GetOrCreate` |

The triggers differ because the access patterns differ: a scene watcher who
never poses is invisible to the emit path and must be seeded when they join; a
private-message recipient has no join step and is seeded when the message that
addresses them mints the DEK. A single trigger cannot serve both.

---

## 5. Invariants (registry: `docs/architecture/invariants.yaml`)

Consulted the `INV-CRYPTO` scope before designing. This work touches three
registry entries and proposes one new one:

| Invariant | State today | This work |
| --- | --- | --- |
| **INV-CRYPTO-6** (master INV-9 — non-participant gets metadata-only on fan-out) | `binding: pending` | **Binds** it via a new live-fan-out integration test (non-participant subscriber → empty payload). |
| **INV-CRYPTO-7** (master INV-12 — `Add` grants immediate read) | `bound` (cache_invalidation_test.go) | Further exercised by the production `SetSceneFocus` `Add` caller. |
| master INV-8 (subject IN participant set receives plaintext on fan-out) | **unregistered** (orphan; not in registry) | **Registers** it (`INV-CRYPTO-116`) and binds it via the participant-decrypts live test. |

Proposed new invariant (mirrors the history-reader analog **INV-CRYPTO-5**,
"`newHistoryReader(nil,nil,nil)` preserves nil-auth passthrough", for the live
path):

> **INV-CRYPTO-117 (proposed)** — Two independently-asserted clauses (the
> binding test SHOULD cover the publisher and subscriber construction sites
> separately, matching the two call sites):
>
> 1. When `RekeyManager` is non-nil, the production gRPC subsystem MUST
>    construct the live **Publisher** with a DEK manager; when nil, the
>    Publisher MUST remain plaintext-only and the subsystem MUST still start.
> 2. When `RekeyManager` is non-nil, the production gRPC subsystem MUST
>    construct the live **Subscriber** with an AuthGuard; when nil, the
>    Subscriber MUST preserve `guard == nil` passthrough and the subsystem
>    MUST still start.
>
> *(Pins the exact regression that hid for months — the live wiring being
> silently absent. Split per design-review finding 2.)*

A second candidate — *"a connection is focused on a scene only if its character
is a DEK participant"* (the Part 2 fatal guarantee) — MAY be registered under
`INV-SCENE` or `INV-CRYPTO`; deferred to the plan / reviewer judgment to avoid
over-minting. Final IDs and `binding` state land in `invariants.yaml` during
implementation (then `go run ./cmd/inv-render`), per `.claude/rules/invariants.md`.

---

## 6. Error handling & fail-closed posture

- Publisher reject without DEK manager stays fail-closed
  (`EVENTBUS_SENSITIVE_EVENT_NO_DEK_MANAGER`).
- Subscriber: non-participant → metadata-only (no leak), never plaintext.
- Scene seed failure → `codes.Internal`, focus refused (Part 2).
- Comms seed failure → publish fails, sender errors (Part 3).
- gRPC handlers MUST NOT leak inner error text past the trust boundary
  (`.claude/rules/grpc-errors.md`): log internally, return
  `status.Error(codes.Internal, "internal error")`.
- All log calls in scope MUST use the context-carrying `*Context` slog variants
  (`.claude/rules/logging.md`).

---

## 7. Testing

**Integration (`task test:int`, `//go:build integration`):**

- Scene participant subscribed to the scene receives **plaintext** `scene_pose`
  (binds master INV-8 / INV-CRYPTO-116).
- Non-participant subscribed to the same scene subject receives
  **metadata-only** (binds INV-CRYPTO-6).
- `SetSceneFocus` seeds the participant; fatal-Add path returns `Internal` and
  does not focus (inject a failing adder).
- Re-`SetSceneFocus` is idempotent (no error, no duplicate participant).
- Comms: recipient of a `page` receives **plaintext**; a third character
  subscribed to the recipient's stream is **not** seeded and receives
  metadata-only; the sender is never a participant on the recipient's context.

**E2E (`task test:e2e`, Playwright — the holomush-5rh.8.27 surface):**

- The e2e compose stack provisions a test KEK (rides this PR) so sensitive
  scene events work end-to-end; `web/e2e/scenes.spec.ts` S3 asserts the live
  PoseCard appears after a pose (not just the UI submit).

Both `task test:int` **and** `task test:e2e` MUST be green before landing.

---

## 8. Out of scope

- **Comms sender-echo symmetry** (page/whisper/pemit emitting the sender's copy
  as an outbound event) → holomush-jq34t. This change is recipient-only.
- **Sender-readable outbox/history** for private messages → part of jq34t.
- **KEK provisioning command + e2e compose wiring** — the in-scope `.8.27`
  pieces (`holomush kek init`, `compose.e2e.yaml`) ride this PR but are an
  established, crypto-reviewer-vetted shape; they are not redesigned here.
- **External/clustered NATS** — out of scope everywhere (holomush-s5ts).

---

## 9. Risks & sequencing

- **Interim comms state** is eliminated by including Part 3: with the global
  wiring flip *and* comms recipient-seeding in the same PR, comms never passes
  through a "publish-encrypted-but-blank" window. (Were Part 3 deferred, the
  flip would degrade comms from a hard publish-error to silent blank delivery —
  hence comms is in-scope per the operator decision.)
- **Pre-deployment posture** (master spec §2491: no users, no production
  deployment) means even a brief degraded window would be tolerable, but
  shipping all three parts together avoids it entirely.
- **Sequencing:** #4407 is merged; `internal/grpc/sceneaccess_service.go` is on
  `main`. Implementation rebases on `main`. Phases are separable (P1 wiring →
  P2 scene seed → P3 comms seed) so a snag in one review dimension does not
  block the others.
- **Review gates:** crypto-reviewer (FIRST) → abac-reviewer (the `SetSceneFocus`
  participation gate) → code-reviewer, before push.
- **Known call-site update (design-review finding 1):** adding the DEK-adder
  parameter to `NewSceneAccessServer` breaks its existing call sites — the
  production wiring in `cmd/holomush/sub_grpc.go` *and* the integration harness
  `internal/testsupport/integrationtest/session.go:639` (currently the 7-arg
  signature). The plan MUST update both. Prefer an optional setter
  (`WithSceneDEKAdder`, per the WIP) over a positional arg to avoid churning the
  test harness signature.

---

## 10. Resolved design questions

| Question | Resolution |
| --- | --- |
| `dek.Manager.Add` idempotent on re-focus? | **Yes** — `manager.go:357` `if !added { return nil }`. Safe no-op. |
| Silent metadata-only degradation on seed failure? | **No** — Add-failure is **fatal** (Parts 2 & 3). |
| Did the gap mask that NO sensitive feature worked in prod? | **Yes** — scenes *and* comms (page/whisper/pemit) were both broken. Comms is in-scope. |
| Comms seeding mechanism? | Host-side, subject-derived recipient at `GetOrCreate` genesis. No new Lua/RPC surface. |
| Both parties decrypt a two-way comm? | **No** — recipient decrypts the event; sender sees direct command output. Recipient-only seeding. |
