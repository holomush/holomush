<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Communication Content Contract — Design

- **Status:** Accepted — `design-reviewer` READY; Slice 1 plan materialized (`holomush-kk1ot`)
- **Date:** 2026-07-03
- **Design bead:** `holomush-kk1ot`
- **Blocks:** `holomush-g1qcw` (focus-routed scene input — MUST land after this)
- **Theme:** `theme:social-spaces`

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are
to be interpreted as described in RFC 2119.

---

## 1. Problem & Motivation

HoloMUSH has no source-of-truth payload contract for conversational content
(say / pose / ooc / emit / page / whisper / pemit). Every plugin that emits such
content invents its own payload shape, and every consumer re-normalizes the
divergent dialects. The same knowledge is encoded in at least four places today:

- **core-communication (Lua)** builds *rich* payloads — pose
  `{character_name, action, no_space?}`, ooc `{character_name, message, style}`,
  say `{character_name, message}`, emit `{message}`
  (`plugins/core-communication/main.lua:77-190`).
- **core-scenes (Go)** builds a *flat* `{actor_id, scene_id, text, character_name?}`
  for all four scene content verbs (`plugins/core-scenes/commands.go:1305`) — it
  **structurally cannot** carry `no_space` / `ooc_style`.
- **The web client** already defines the canonical model, but at the wrong
  layer: `web/src/lib/comm/commLine.ts` `CommLine {kind, actor, text, label?, noSpace?, oocStyle?, …}`
  is a *render-time* normalization fed by two **asymmetric** adapters —
  `commEventToLine` (rich; reads `metadata.no_space`/`style`) and `logEntryToLine`
  (lossy; its own doc-comment admits scene events "leave them undefined").
- **The BFF** `internal/web/translate.go` unions the fields ad-hoc in
  `genericPayload {character_name, sender_name, target_name, message, text,
  action, no_space, style, channel, …}` and drops anything not in that fixed
  struct.

Plus the Go server renderer `plugins/core-scenes/publish_render.go:50-70` and the
Go↔TS golden-parity tests (epic `holomush-c5zol`) that exist *only* to keep the
duplicated encodings in sync. The whole `c5zol` rendering-seam epic + bug
`holomush-5rh.33` were **downstream compensation** for this missing upstream
contract.

**The primitive already exists latently** (`CommLine`); it is simply defined
client-side and fed by asymmetric adapters instead of declared once at the emit
boundary. This design promotes it upstream.

---

## 2. Goals / Non-Goals

### Goals

- Define ONE canonical content-body contract every conversational-content
  emitter targets, differing only by NATS subject (addressing).
- Enforce conformance **symmetrically** for Lua and binary plugins at a single
  host chokepoint (plugin-runtime-symmetry).
- Provide an **ergonomic plugin-facing builder** in both runtimes so
  conformance is the path of least resistance — the plugin SDK is the product
  surface.
- Collapse the asymmetric consumer adapters (`commEventToLine` /
  `logEntryToLine`), the BFF `genericPayload`, and the Go renderer toward a
  single dialect.
- Unblock `holomush-g1qcw`: with the contract, its dispatcher focus-redirect
  becomes payload-transparent (routing a pose to a scene changes the subject,
  not the content shape) — so no pose/ooc "flattening" ever ships.

### Non-Goals

- **Forums** — asynchronous, threaded, persistent posts are a different
  primitive and are OUT.
- **A general typed-payload system for all verbs** — the discriminator here is
  scoped to the conversational-content family; a universal "every verb declares
  its payload proto type" mechanism is a deliberate non-goal.
- **The focus-routing mechanism itself** — that is `holomush-g1qcw`.
- **`whisper_notice` content and the sender-echo** — a `whisper_notice` is a
  content-*less* public notice, and the page/whisper sender echo is command
  *output*, not an event; both stay OUTSIDE the content contract.
- **`channel` and `ooc_prefix` fields** — speculative today (channels do not
  exist; the only non-`[OOC]` prefix appears solely in a test fixture). proto3
  field additions are wire-backward-compatible, so both are added when their
  features actually ship.

---

## 3. Scope: the conversational-content family

The contract covers **real-time conversational content**, both:

- **Broadcast** — `say` / `pose` / `ooc` / `emit`, addressed to a `location.<id>`
  or `scene.<id>.ic|.ooc` subject.
- **Targeted** — `page` / `whisper` / `pemit`, addressed to a `character.<id>`
  subject. A future **channel** is `channel.<id>` (group-targeted; same family).

**Addressing is always the NATS subject** (`plugins/core-communication/main.lua`
emits `location.<id>` for broadcast, `character.<target_id>` for page/whisper/pemit
at lines 87/132/167/189/292/419/465). Therefore the content **body carries no
audience / routing target** — that is the subject. It DOES carry the actor's
**identity** (a stable `actor_id` plus a rendered `actor_display_name`, and a
later `sender_display_name` for targeted verbs), which persistent consumers
(scene replay / export / publish-snapshot) read self-contained from the payload
(§4.5).

---

## 4. Design

### 4.1 The content body — `holomush.comm.v1.CommunicationContent`

A new proto package (`holomush.comm.v1`) — NOT `holomush.content.v1`, which is
already the CMS `ContentService` (`api/proto/holomush/content/v1/content.proto`)
and would collide semantically.

```proto
// CommunicationContent is the canonical instance-level payload body for every
// real-time conversational-content event (say/pose/ooc/emit and, after the
// targeted slice, page/whisper/pemit). It complements the type-level rendering
// hints already stamped by RenderingPublisher (kind via Format, label) — this
// message is the per-emit body those hints render over.
message CommunicationContent {
  // actor_id is the STABLE identity (character ULID) of the acting character.
  // It is read self-contained from the payload by the scene replay/export/
  // publish-snapshot decoders (plugins/core-scenes/commands.go decodeReplayEntries;
  // publish_snapshot.go:368-375; export.go) to populate PublishedSceneEntry.Speaker,
  // which publish_render.go:28-31 prints verbatim into replay, exports, and the
  // permanently-frozen published-scene document. Every content emitter MUST
  // preserve it (empty only for the actorless `emit` verb).
  string actor_id = 1;

  // actor_display_name is the resolved author name for rendering (core-
  // communication stamps character_name today; scenes currently defer name
  // resolution and leave this empty — the renderer falls back to actor_id).
  // MAY be empty when resolution is deferred or for actorless `emit`.
  string actor_display_name = 2;

  // text is the raw, unrendered content ("waves", "Hello there."). The renderer
  // produces the surface form ("Alaric waves"); the plugin MUST NOT pre-render.
  string text = 3;

  // no_space, when true, renders the actor and text with no separating space
  // (the ";" semipose form → "Alaric's eyes narrow"). Default false → "Alaric waves".
  bool no_space = 4;

  // ooc_style selects the OOC surface form for ooc events: "say" (default) →
  // '[OOC] Bob says, "brb"', "pose" → "[OOC] Foob waves", "semipose" → no-space.
  // Empty for non-ooc kinds.
  string ooc_style = 5;
}
```

- `kind` is **not** in the body — it is derived from the verb (`RenderingMetadata.Format`
  action→pose / speech→say, plus event type for ooc), already host-stamped by
  `RenderingPublisher` (`internal/eventbus/rendering_publisher.go:63-74`).
- Reserved for later slices (added as new proto fields, non-breaking):
  `sender_display_name` (targeted slice), `channel` / `ooc_prefix` (when those
  features ship).

`protovalidate` rules (buf) MUST require `text` non-empty for content events and
constrain `ooc_style` to the enum `{"", "say", "pose", "semipose"}`.

### 4.2 Discriminator — the existing `category: communication`

The emit gate MUST decide "validate this payload against `CommunicationContent`?"
The signal is the **existing** per-verb `category` field
(`internal/core/registry.go:17` `VerbRegistration.Category`), already stamped onto
`RenderingMetadata.Category` at emit and already consumed as the client
rendering-dispatch axis (`web/.../EventRenderer.svelte:27`
`{#if event.category === 'communication'}`; telnet
`internal/telnet/gateway_handler.go:1131 case "communication":`).

No new verb-manifest field is introduced. Instead this design establishes the
invariant:

> **`category: communication` ⟺ the event's payload is a `CommunicationContent`.**

Today two verbs violate that — both are latent modeling errors this design
corrects:

| Verb | Today | Fix | Why |
| --- | --- | --- | --- |
| `pemit` | `category: command` | → `communication` | pemit is private narration (content), mis-shelved; it currently routes to `CommandRenderer` instead of `CommunicationRenderer` — a latent rendering inconsistency. |
| `whisper_notice` | `category: communication` | → `system` | a `whisper_notice` is a content-*less* public notice ("X whispers to Y"), not author content; it must stop masquerading as communication content. |

Rationale for reusing `category` rather than adding a parallel
`communication_content` flag: for genuine conversational content, "renders via
the communication renderer" and "carries the communication content body" are the
**same population**; the only counter-examples were the two mis-shelved verbs
above. Adding a second near-identical axis would be redundant; correcting the two
liars makes the existing axis honest. (See the design discussion recorded on
`holomush-kk1ot`.)

### 4.3 Enforcement (A) — `ContentValidationPublisher` chain link

A new publisher-chain decorator validates content payloads at emit, using the
same **protovalidate-at-emit** pattern `RenderingPublisher` already establishes —
with one added step it does NOT share: because the wire payload is
plugin-supplied **JSON** (`internal/plugin/event_emitter.go::Emit` enforces
`json.Valid`, there is no proto-binary path), the link must first
`protojson.Unmarshal` those *untrusted* bytes into a `CommunicationContent` proto
before it can validate. That is strictly more than
`RenderingPublisher.validateRendering` (`rendering_publisher.go:120-126`), which
validates a proto the host itself constructs from trusted Go values and never
decodes arbitrary bytes.

**Chain position (decorator ordering):** `RenderingPublisher` stamps
`event.Rendering` (incl. `Category`) *before* delegating to its `inner`
(`internal/eventbus/rendering_publisher.go` — stamp, then `p.inner.Publish`). The
content link therefore slots as the **inner of `RenderingPublisher`** (so
`Category` is populated) and the **outer of payload encryption** (so it operates
on the plaintext body — the contract validates the pre-codec body; targeted
verbs are `sensitivity: always`).

```text
Publisher chain — outer wraps inner; each delegates inward:

  RenderingPublisher            stamps event.Rendering (incl. Category), then delegates
   └─▶ ContentValidationPublisher   reads Rendering.Category; if "communication",
                                    protojson.Unmarshal + protovalidate(plaintext
                                    event.Payload) as CommunicationContent
        └─▶ JetStreamPublisher       writes the wire; payload ENCRYPTION is inlined
                                    HERE (publisher.go:208-238), not a separate link

Composition:
  NewRenderingPublisher(NewContentValidationPublisher(jetStreamPublisher), verbRegistry)
```

Composed by wrapping the new link as `RenderingPublisher`'s `inner`. The
conversational-content path is `cmd/holomush/sub_grpc.go:140-146`
(`wrapPublisher`, used for the plugin-manager / bus event-appender emit callers) —
**that** is the site that MUST receive the content link. The other two
`NewRenderingPublisher` sites wrap publishers that never carry
`category: communication` events (`core.go:671`'s audit publisher for
crypto-policy-snapshot events; `phase7_fence_wiring.go:46`'s violation emitter for
`plugin_integrity_violation` system events), so wrapping them would be a harmless
no-op — the link only ever acts on communication-category events.

Behavior:

- If `event.Rendering.Category == "communication"`, the link MUST
  `protojson.Unmarshal` the plaintext payload into a `CommunicationContent` and
  validate it via `protovalidate`. The decode MUST use snake_case field names
  (the `UseProtoNames` convention `renderingJSONOpts` already establishes at
  `rendering_publisher.go:21-25`) and MUST set `DiscardUnknown: true` (as
  `internal/eventbus/history/cold_postgres.go:296` already does) so a
  forward-compatible field addition on a newer producer does not fail an older
  validator.
- A malformed-JSON / decode failure OR a protovalidate failure MUST return
  `oops.Code("EMIT_CONTENT_INVALID")` (mirroring `EMIT_VALIDATION_FAILED`),
  failing the emit.
- For any other category it MUST pass through untouched.

This is symmetric for Lua and binary plugins by construction — both runtimes
publish through the same chain (`internal/plugin/event_emitter.go::Emit` →
publisher), exactly how the manifest emit fence satisfies plugin-runtime-symmetry.

> **Ordering constraint (verified):** the content link operates on the plaintext
> body. Encryption is not a separate decorator — it is inlined inside
> `JetStreamPublisher.Publish` (`internal/eventbus/publisher.go:208-238`), the
> innermost link. Since `ContentValidationPublisher` runs *before* delegating to
> `JetStreamPublisher`, it always sees plaintext. (Cross-checked against the crypto
> design, `docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`.)

### 4.4 Construction (C) — dual-runtime builders + single-source grammar

Enforcement guarantees *shape*; the builders make *conformance ergonomic* and
carry the *grammar*.

- **Go SDK** (`pkg/plugin/comm`, new): a grammar pair `ParsePose(invokedAs, raw)` /
  `ParseOOC(raw)` (sigil → `no_space` / `ooc_style`) plus builders
  `Say(author, text)`, `Pose(author, invokedAs, raw)`, `OOC(author, raw)`, and
  `Emit(text)` (targeted-slice `Page(...)` later) — each returning a conforming
  `CommunicationContent` payload as a JSON string. The Slice-1 plan
  (`…-slice-1.md` Task 2) carries the normative signatures.
- **Lua** (`holo.comm.*`): the parity counterpart, so Lua plugins never
  hand-assemble JSON.
- **Single-source grammar:** the sigil/style parsing (`;`/`:` → `no_space`;
  `ooc :`/`;`/plain → `ooc_style`) — currently duplicated in
  `plugins/core-communication/main.lua` (handle_pose/handle_ooc) — MUST be
  defined **once**. core-scenes' `handleEmit` calling the shared parser
  (`comm.ParsePose(invokedAs, raw)`) is precisely what gives scene poses `no_space`
  for free, dissolving the g1qcw flattening at the source.

The builder is the happy path; the §4.3 gate is the backstop that makes it more
than a suggestion (§4.2 established the gate is unconditional for
`category: communication`).

**Open (plan-time):** whether Lua reaches the single-source grammar via a
**hostfunc** (true single Go implementation, one call per emit — lean) or a
**parity-tested pure-Lua copy** (no round-trip, two implementations kept
identical by a golden test, as `c5zol` did Go↔TS).

### 4.5 Consumer convergence

With one dialect on the wire:

- `internal/web/translate.go` reads `CommunicationContent` instead of the ad-hoc
  `genericPayload`.
- `web/src/lib/comm/commLine.ts` — `commEventToLine` and `logEntryToLine`
  **converge** (the lossy scene adapter stops being lossy).
- `plugins/core-scenes/publish_render.go` and the Go↔TS golden-parity surface
  shrink.

**Consumers that MUST be preserved (not merely converged) in Slice 1.** Three
scene consumers decode `{actor_id, text}` directly out of the *same* content
payload bytes to populate `PublishedSceneEntry.Speaker` — the field
`publish_render.go:28-31` prints into scene-log replay, Markdown/JSONL/plain-text
export, AND the permanently-frozen published-scene document:
`decodeReplayEntries` (`plugins/core-scenes/commands.go`), `decodeSnapshotEntry`
(`plugins/core-scenes/publish_snapshot.go:368-375`), and the export path
(`plugins/core-scenes/export.go`). This is precisely why `CommunicationContent`
carries `actor_id` (§4.1): the Slice-1 scene migration MUST keep emitting it, or
these decoders silently zero-value `Speaker` (Go `json.Unmarshal` does not error
on a missing key) and every replay / export / frozen scene renders a blank
speaker. A characterization test (§7) guards it.

The web/BFF items above are convergence *opportunities* realized incrementally as
the emitters migrate (§8); the contract does not require a big-bang consumer
rewrite. The `actor_id` preservation, by contrast, is a hard Slice-1 requirement,
not an opportunity.

---

## 5. Invariants

This design introduces system-level guarantees that MUST be registered in
`docs/architecture/invariants.yaml` when the plan is written (per
`.claude/rules/invariants.md`). A new `INV-COMM` scope/boundary MUST be added.

- **INV-COMM-1 (payload conformance):** every event whose verb declares
  `category: communication` carries a wire payload that validates as
  `holomush.comm.v1.CommunicationContent`. Bound by the §4.3 gate + a test that
  asserts a non-conforming communication payload is rejected with
  `EMIT_CONTENT_INVALID`. **Binding lands in Slice 2**, when the gate is wired
  live (the gate exists + is unit-tested in Slice 1, but is not on the live chain
  until the whole `category: communication` family conforms — §8).
- **INV-COMM-2 (builder runtime symmetry):** for the same inputs, the Go SDK and
  Lua builders produce JSON that decodes to an **equal** `CommunicationContent`
  proto (equivalently: identical canonical JSON under the `UseProtoNames`
  snake_case convention — NOT a raw byte compare, since wire payloads are JSON and
  are key-order/whitespace-sensitive). Bound by a Go↔Lua parity test (same
  discipline as `c5zol`'s Go↔TS golden).

Both ship `binding: pending` until their asserting tests exist.

---

## 6. Error handling

- Emit-time validation failure → `oops.Code("EMIT_CONTENT_INVALID")`, failing the
  emit (mirrors `EMIT_VALIDATION_FAILED` in `rendering_publisher.go`). The inner
  error MUST NOT leak past any gRPC boundary (per `.claude/rules/grpc-errors.md`).
- **Migration safety:** the gate is keyed on `category == "communication"`, so
  only communication events are ever validated. The two recategorizations
  (§4.2) are the migration touch; every non-communication event is unaffected.
  Because the broadcast content verbs are *already* structured, migrating them to
  the builder is behavior-preserving.

---

## 7. Testing

- `protovalidate` rules on `CommunicationContent` (text non-empty; `ooc_style`
  enum) — unit-tested.
- **Go↔Lua builder parity** golden test (INV-COMM-2).
- Gate rejects a hand-built non-conforming `category: communication` payload with
  `EMIT_CONTENT_INVALID` (INV-COMM-1); covers malformed JSON and a
  protovalidate-failing body.
- **Speaker-preservation characterization test (Slice 1):** after the scene emit
  migrates to the builder, `decodeReplayEntries` / `decodeSnapshotEntry` still
  populate `PublishedSceneEntry.Speaker` from the emitted payload (`actor_id`
  round-trips) — replay / export / published-snapshot render the speaker
  unchanged.
- Consumer-convergence characterization tests: `translate.go` /
  `commEventToLine` / `logEntryToLine` produce identical `CommLine` for a
  broadcast pose and the equivalent scene pose once both emit `CommunicationContent`.
- Integration: a scene-focused `;pose` (via g1qcw, once landed) renders with
  `no_space` — proving the flattening is gone.

---

## 8. Phasing (slices)

Design the general primitive once; sequence the migrations.

**Slice 1 — Broadcast core (unblocks g1qcw).**
`CommunicationContent` proto + `protovalidate` rules; `ContentValidationPublisher`
gate; Go + Lua builders + single-source grammar; migrate `say`/`pose`/`ooc`/`emit`
in **both** core-communication and core-scenes to the builder. The core-scenes
migration MUST keep emitting `actor_id` (§4.5) so the scene replay / export /
publish-snapshot decoders keep populating `Speaker`; a characterization test (§7)
guards it. Converge the consumers for those verbs.

**Gate sequencing (important).** The `ContentValidationPublisher` is *built and
unit-tested* in this slice but is NOT yet wired into the live publisher chain.
Wiring it now would reject the still-unmigrated `page` / `whisper` /
`whisper_notice`, which are *also* `category: communication` but do not yet emit
`CommunicationContent`. `holomush-g1qcw` is unblocked by the broadcast **emit
shape** (structured `CommunicationContent` carrying `no_space`), which is
independent of live enforcement — so deferring the live gate costs g1qcw nothing.

On completion, `holomush-g1qcw` proceeds — its redirect is payload-transparent.

**Slice 2 — Targeted family.**
Migrate `page`/`whisper`/`pemit`: destructure today's pre-rendered `message`
(`main.lua` bakes "From afar, X waves" into the payload) into structured
`CommunicationContent`; teach the renderer the kind-specific framing ("From
afar…", "X pages:…"); recategorize `pemit` (→ communication) and move
`whisper_notice` (→ system). Add `sender_display_name`. `whisper_notice` and the
sender echo remain outside the content contract. **Finally, wire the
`ContentValidationPublisher` live** at `sub_grpc.go:140-146` (hard enforcement) —
safe only now that every `category: communication` verb conforms — which is what
binds INV-COMM-1.

---

## 9. Open questions / risks

1. **Crypto ordering (§4.3): RESOLVED** — encryption is inlined in
   `JetStreamPublisher.Publish` (`internal/eventbus/publisher.go:208-238`), the
   innermost link; `ContentValidationPublisher` runs before it and sees plaintext.
2. **`whisper_notice` target category:** `system` is the recommended landing
   (rendered by `SystemRenderer`); confirm the system/notice render path can
   produce "X whispers to Y" acceptably during Slice 2.
3. **Lua grammar access (§4.4):** hostfunc vs parity-tested pure-Lua copy —
   decided at plan time.
4. **Invariant registry:** a new `INV-COMM` boundary MUST be added to
   `invariants.yaml`; INV-COMM-1/2 land `binding: pending`.
5. **`actor_id` conformance is test-guarded, not schema-enforced (accepted).**
   `protovalidate` cannot require `actor_id` non-empty because the `emit` verb is
   legitimately actorless, and `kind` is not in the body to express a conditional
   rule. So the §7 Speaker-preservation characterization test — not a hard gate
   rule — is what guards a future non-scene emitter forgetting `actor_id`. Accepted
   as adequate (design-reviewer round 2, non-blocking); revisit only if a
   third-party content emitter appears.

---

## 10. Relationship to `holomush-g1qcw`

`holomush-kk1ot` (this design) MUST precede `holomush-g1qcw`. `g1qcw` stays
route-only: a manifest-declared focus-redirect on core-scenes + a dispatcher
redirect of top-level `pose`/`say`/`ooc`/`emit` to the `scene` command when
`Connection.FocusKey.Kind == scene`. With this contract in place, that redirect
changes only the subject, not the payload contract — so the pose/ooc "flattening"
`g1qcw` would otherwise have shipped simply does not arise.

---

## Grounding trace (for `plan-reviewer`)

- `mcp__probe`/`codegraph`/`rg`: divergent payloads —
  `plugins/core-communication/main.lua:77-190` vs
  `plugins/core-scenes/commands.go:1305`.
- Latent primitive: `web/src/lib/comm/commLine.ts` (`CommLine`, asymmetric
  `commEventToLine`/`logEntryToLine`).
- Existing formalized type-level layer:
  `internal/eventbus/rendering_publisher.go` (single enrichment site,
  `protovalidate` at emit), `RenderingMetadata`
  (`internal/eventbus/types.go:126`), `VerbRegistration`
  (`internal/core/registry.go:17`).
- Discriminator consumers: `web/.../EventRenderer.svelte:27`,
  `internal/telnet/gateway_handler.go:1131`, valid categories
  `internal/plugin/manifest.go:211`.
- Namespace collision check: `api/proto/holomush/content/v1` is `ContentService`
  (CMS), not communication.
- BFF union precursor: `internal/web/translate.go` `genericPayload`.
- Chain composition: `RenderingPublisher.Publish` stamps then delegates
  (`internal/eventbus/rendering_publisher.go`); encryption inlined in
  `JetStreamPublisher.Publish` (`internal/eventbus/publisher.go:208-238`); the
  conversational-content wiring site is `cmd/holomush/sub_grpc.go:140-146`
  (`wrapPublisher`), NOT `core.go:671` (audit) / `phase7_fence_wiring.go:46`
  (violations), which never carry `category: communication`.
- Slice-1 preservation consumers (design-reviewer round 1): scene `actor_id`
  decode → `Speaker` in `plugins/core-scenes/commands.go` (`decodeReplayEntries`),
  `publish_snapshot.go:368-375` (`decodeSnapshotEntry`), rendered by
  `publish_render.go:28-31`; the `.../commands.go` doc-comment states "Speaker is
  the actor character id; name resolution is a follow-up."
- Emit boundary is JSON: `internal/plugin/event_emitter.go::Emit` enforces
  `json.Valid` (no proto-binary path); snake_case `protojson` precedent
  `rendering_publisher.go:21-25`; `DiscardUnknown` precedent
  `internal/eventbus/history/cold_postgres.go:296`.
<!-- adr-capture: sha256=330006253c035999; session=f01a4c84; ts=2026-07-04T01:27:59Z; adrs=holomush-2hhq2,holomush-byqph -->
