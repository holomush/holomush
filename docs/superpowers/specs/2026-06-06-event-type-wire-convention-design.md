<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Event-Type Wire Convention: Canonicalize to `<plugin>:<verb>`

- **Status:** Design (pending design-reviewer)
- **Design bead:** `holomush-aneim`
- **Date:** 2026-06-06
- **Author:** Sean Brandt (with Claude Opus 4.8)

## Problem

HoloMUSH has two coexisting conventions for the **wire event-type string** a
plugin stamps on emitted events, and exact-match consumers silently break when a
plugin's wire type doesn't match what a given consumer expects:

- **Plugin-qualified** `<plugin>:<verb>` — used by `core-communication`
  (`core-communication:say`), `core-objects` (`core-objects:object_create`),
  the documented convention (`site/.../reference/events.md`,
  `scripts/gen-event-docs.sh`: "Each event type identifier is qualified with its
  owning plugin"), the web renderer (`CommunicationRenderer.svelte` matches
  `event.type === 'core-communication:ooc'`), and the verb registry
  (`manager.go` registers `verbs[].type` verbatim).
- **Bare** `<verb>` — used by `core-scenes` (`scene_pose`), which is internally
  consistent on bare types: emit sites, `RegisterEmitTypes` (phase4/phase6),
  internal `scene_* → EntryKind` dispatch maps, and persisted
  `plugin_core_scenes.scene_log` rows are all bare.

The crypto subsystem's `crypto.emits[].event_type` is **bare** in both plugins
by design (because `requests_decryption` refs are `<plugin>:<event_type>` and
`splitQualifiedRef` recovers the bare verb). `LookupEmitSensitivity` exact-match
on the raw wire type is what made `holomush-50zqs` a silent no-op: for
`core-communication` the wire type was qualified but the bare `crypto.emits`
entry never matched. `holomush-50zqs` fixed that with `emitEntryMatchesWireType`
(bare entry matches `entry == wire` OR `<plugin>:entry == wire`), but only at the
crypto match sites.

Two concrete, currently-latent defects motivate the cleanup:

1. **core-scenes renders nothing in production (`holomush-r0kup`, the real
   severity).** core-scenes is the only rendering-emitting plugin with **no
   `verbs:` block**, so it registers no verbs (binary plugins have no
   InitResponse verb path; `RegisterEmitTypes` feeds only INV-PLUGIN-32, not the
   verb registry). The production plugin emitter publishes through the
   `RenderingPublisher`-wrapped publisher (wired in `sub_grpc.go`'s
   `ConfigureEventEmitter`/`wrapPublisher` path), which hard-fails
   `EMIT_UNKNOWN_VERB`
   (`rendering_publisher.go:60`) for any type not in the verb registry. So every
   `scene_*` wire type fails to publish in production — masked only by the
   integration harness seeding scene verbs. Unhit to date because there are no
   users. This is core-scenes being treated as a special case instead of a
   normal plugin.
2. **`core-communication:emit` rendering break.** `main.lua:178` emits the
   generic `emit` command with a **bare** wire type `"emit"`, while
   `verbs[].type` (`plugin.yaml:264`) and `events.go` declare
   `core-communication:emit`. `RenderingPublisher.Lookup` is exact-match and
   returns `EMIT_UNKNOWN_VERB` for the unregistered bare `"emit"`. (The bare
   `crypto.emits` entry `emit` happens to match it, so crypto is unaffected —
   the mirror image of the `holomush-50zqs` bug.)
3. **Convention drift is unbounded.** Nothing prevents a new plugin from picking
   either convention, re-introducing the silent-mismatch class at any exact-match
   consumer (rendering, crypto, audit, history).

## Decision

**The canonical wire event-type identity is plugin-qualified
`<owning-plugin>:<verb>`.** This was chosen over "bare everywhere" (conflicts
with the documented convention, the web renderer, and the verb registry) and
over "document both + normalize every consumer" (leaves the drift unbounded).

**There are no existing users and backward compatibility is explicitly NOT
required** (decision, 2026-06-06). Therefore the migration is clean: no
mixed-history handling, no data backfill, no reader-side legacy normalization.

### The invariant

> Every emitted **wire event type** and every `verbs[].type` MUST be exactly
> `<owning-plugin>:<verb>` — the owning plugin's manifest `name`, a single
> colon, then a verb token. The bare verb survives **only** in
> `crypto.emits[].event_type` and the `<plugin>:<verb>` `requests_decryption`
> refs that consume it; the host bridges bare↔qualified at the crypto match
> sites via `emitEntryMatchesWireType`.

This is registered as **`INV-PLUGIN-40`** in `docs/architecture/invariants.yaml`
(`binding: pending` per `.claude/rules/invariants.md`), bound by the enforcement
test (the `Manifest.Validate` gate + the meta-test) below.

## Design

### Considered and rejected: structured type

A "most correct" alternative carries the owning plugin and the verb as separate
structured fields on `core.Event` rather than a delimited string. Rejected as
disproportionate (YAGNI): it is a proto + `core.Event` + every-consumer rewrite,
and the `<plugin>:<verb>` string convention is already entrenched in the
majority of the codebase, the docs, and the web client.

### Three vocabularies (the distinction the invariant rests on)

An event type appears in three places governed by **different** rules. Conflating
them is what made the first draft of this design wrong, and what let core-scenes
become "special". A normal plugin satisfies all three the same way:

| Vocabulary | Source | Form | Governed by |
| --- | --- | --- | --- |
| **Registered emit set** | `register_emit_type` (Lua) / `RegisterEmitTypes`→`PluginEmitRegistry` (binary) | **bare** `<verb>` | INV-PLUGIN-32: MUST equal the `crypto.emits` set exactly (`manager.go:1052`, `manifestDeclaredEmitTypes` `:1671`) |
| **`crypto.emits[].event_type`** | manifest `crypto:` block | **bare** `<verb>` | crypto: `requests_decryption` refs are `<plugin>:<verb>` and `splitQualifiedRef` recovers the bare verb (`crypto_validator.go`) |
| **Wire type + `verbs[].type`** | emit-site `Type` string + manifest `verbs:` block | **qualified** `<plugin>:<verb>` | rendering: `RenderingPublisher.Lookup(wireType)` resolves against `verbs[].type` and hard-fails `EMIT_UNKNOWN_VERB` on a miss (`rendering_publisher.go:60`) |

The **registered-emit and `crypto.emits` vocabularies stay bare**; the **wire
and verb vocabularies are qualified**; `emitEntryMatchesWireType` (the
`holomush-50zqs` bridge) is the single sanctioned place a bare `crypto.emits`
entry meets a qualified wire type. core-communication and core-objects already
satisfy this. **core-scenes does not** — it has no `verbs:` block at all, so it
registers no verbs and its scene IC content hard-fails `EMIT_UNKNOWN_VERB` in
production (`holomush-r0kup`), masked only by the integration harness seeding
scene verbs.

### Decision: where the bare↔qualified boundary sits

Two vocabularies are **forced bare** (registered-emit set + `crypto.emits`, by
INV-PLUGIN-32 and `requests_decryption`). Everything else for core-scenes is
**downstream of the wire** — `scene_log.type` is written from the emitted wire
event, and the snapshot/audit/dispatch logic reads that stored type. So those
internal consumers naturally see whatever the wire carries. The decision (given
no users — churn is free and uniformity is the goal):

> **Qualify end-to-end.** core-scenes' emit sites stamp `core-scenes:<verb>`;
> `scene_log.type` therefore stores the qualified form; and *every* internal
> consumer that keys on the stored/wire type is re-keyed to the qualified form.
> The **only** bare vocabularies that remain are the two forced ones
> (registered-emit set + `crypto.emits`). There is no per-plugin bare/qualified
> transformation layer — one type form flows from emit through storage through
> snapshot, bridged to the bare crypto vocabulary only by
> `emitEntryMatchesWireType`.

This dissolves the "another bare site" findings: each is a uniform re-key, not a
special case. The exhaustive site sweep is the implementation **plan**'s job
(via grounding); this spec fixes the *decision* and enumerates the known sites
below so the plan starts complete.

### Component changes

1. **Make core-scenes a normal plugin** (resolves `holomush-r0kup`). core-scenes
   is not special; bring it to the same shape every rendering plugin has, and
   apply the qualify-end-to-end decision above:
   - **Add a `verbs:` block** to `plugins/core-scenes/plugin.yaml` declaring
     **every** emitted scene type as qualified `core-scenes:<verb>` — all 14:
     the four IC content types (`scene_pose`, `scene_say`, `scene_emit`,
     `scene_ooc`), the lifecycle types (`scene_join_ic`, `scene_leave_ic`,
     `scene_pose_order_changed_ic`), the six `scene_publish_*` notices, and the
     deferred `scene_idle_nudge` — with `category`/`format`/`label`/
     `display_target` like core-communication's block. *All* plugin emits pass
     through `RenderingPublisher`, so every emitted type needs a verb entry or it
     hard-fails `EMIT_UNKNOWN_VERB` (`holomush-r0kup`). The plan MUST derive the
     authoritative list from the emit sites, not from this prose, to avoid an
     incomplete block.
   - **Qualify the emit-site `Type` strings:** `commands.go` (the `eventType`
     reaching `EmitIntent`, and the bare verb strings passed to `handleEmit` at
     the dispatch site ~`commands.go:519-525`), `service.go` (`scene_join_ic`,
     `scene_leave_ic`, `scene_pose_order_changed_ic`), `publish_events.go` (the
     six `scene_publish_*`).
   - **Re-key every internal type-keyed consumer** to the qualified form (per the
     decision): `replayEventKinds` (`commands.go`), `snapshotEventKinds`
     (`publish_snapshot.go`), the audit dispatch equality at `audit.go:399`, the
     SQL filter `WHERE type IN ('scene_pose', 'scene_say', 'scene_emit')` in
     `ReadSceneLogForSnapshot` (`publish_store.go:640`), and the verb-name
     derivation `verb := strings.TrimPrefix(eventType, "scene_")` in `handleEmit`
     (`commands.go:1230`) — which must strip the full `core-scenes:scene_`/the
     qualified prefix so user-facing verb names stay clean. The plan grounds the
     complete set; these are the known sites.
   - **`phase4EmitTypes()` / `phase6EmitTypes()` registration and
     `crypto.emits[].event_type` stay BARE** (`scene_pose`, …, including the
     deferred `scene_idle_nudge`). They are the crypto vocabulary; INV-PLUGIN-32
     requires them equal to each other, and they are NOT wire types. Qualifying
     them would trip `EVENT_TYPE_REGISTRY_MISMATCH` at load. This is the key
     correction over the first draft.

2. **`core-communication:emit` fix.** `main.lua:178` `type = "emit"` →
   `type = "core-communication:emit"`. Same class as core-scenes: aligns the
   generic emit with its existing `verbs[].type` (`plugin.yaml:264`) and
   `events.go` constant, closing its own `EMIT_UNKNOWN_VERB` break. Its
   `crypto.emits` entry `emit` (bare) is unaffected — the bridge resolves
   `core-communication:emit` against it.

3. **Loader validation.** At plugin load, reject a manifest whose **`verbs[].type`**
   is not `<this-plugin-name>:<verb>` (a single colon, owning-plugin prefix). The
   check lives at the verb-registration loop (`manager.go:1138`), returns a typed
   error (e.g. `PLUGIN_WIRE_TYPE_NOT_QUALIFIED`), and rolls back the partial load
   like the other load-time validations. **It validates `verbs[].type`, NOT the
   registered emit set** (which is bare by INV-PLUGIN-32). This gate would have
   caught both core-scenes (no/again-bare verbs) and core-communication:emit at
   startup. A plugin that emits a wire type with no matching registered verb
   still fails fast at emit time via the existing `EMIT_UNKNOWN_VERB`.

4. **No change to crypto.** `crypto.emits` (bare), `requests_decryption`,
   `splitQualifiedRef`, `emitEntryMatchesWireType`, and **INV-PLUGIN-32**
   (registered-emit-set == `crypto.emits`, both bare) are all unaffected — the
   migration deliberately keeps both bare vocabularies bare. The bridge is
   promoted from a `holomush-50zqs` shim to the **documented intended
   architecture**.

5. **Remove the special-case test seeding.** Once core-scenes ships a `verbs:`
   block, `internal/testsupport/integrationtest/crypto.go` no longer needs to
   seed scene verbs into the registry — deleting that seeding is the proof
   core-scenes is no longer special (the harness now loads it like any plugin).

6. **Documentation.** Add the three-vocabulary rule + the qualified-wire
   invariant to `.claude/rules/event-conventions.md`; confirm the public
   `reference/events.md` qualified-type convention matches (no change expected).

### Data flow (unchanged shape, now uniform)

Emit (qualified wire type) → `event_emitter.go::Emit` → `eventbus.NewType`
(validates one-colon form) → publish. Consumers:

- **Rendering:** `RenderingPublisher.Lookup(wireType)` resolves against
  `verbs[].type` (qualified) — now always hits for every plugin.
- **Crypto:** `LookupEmitSensitivity` / `PluginCanReadBack` via
  `emitEntryMatchesWireType` bridge the bare `crypto.emits` entry to the
  qualified wire type.
- **Audit / history / snapshot:** key on the (now-qualified) wire type; for
  `core-scenes`, the internal dispatch is migrated in lockstep with its emit
  sites in the same change, so there is no window where a stored type fails to
  dispatch.

### Error handling

- Loader: unqualified `verbs[].type` → typed error + load rollback (fail-fast).
  **This is the sole enforcement point for the qualified-wire invariant.**
- Emit: `eventbus.NewType` rejects *malformed* (multi-colon / empty) type
  strings but **accepts a bare single-token verb** (`typeRe` in
  `internal/eventbus/types.go` permits `^[a-z][a-z0-9_-]*...`). A plan author
  MUST NOT treat `NewType` as a defense layer against bare types — only the
  loader gate above catches them, at registration time, not at emit time.
- Crypto: unchanged fail-closed behavior (`EnforceSensitivity` truth table
  untouched).

## Testing

- **`holomush-r0kup` regression:** a core-scenes scene emit publishes through
  the production `RenderingPublisher` (verb registry seeded only from the
  manifest, no test-side scene-verb seeding) without `EMIT_UNKNOWN_VERB`, and
  resolves a rendering registration. This is the test that would have failed
  before the `verbs:` block existed.
- **`core-scenes` unit tests** updated to the qualified wire types (emit
  assertions, `EntryKind` dispatch, audit projection) — while
  `crypto.emits`/registered-emit assertions stay bare.
- **Loader validation test:** a manifest with a bare or foreign-qualified
  `verbs[].type` is rejected with the typed error and rolled back.
- **Meta-test** (`test/meta` or `internal/plugin/lua`): scan every in-tree
  plugin's **`verbs[].type`** and assert each is `<owning-plugin>:<verb>`. It
  MUST NOT assert qualification on the registered-emit / `crypto.emits` sets —
  those are bare by INV-PLUGIN-32. Pairs with the `holomush-72ti5`
  `sensitive`-claim meta-test.
- **Harness de-seeding:** remove the scene-verb seeding from
  `internal/testsupport/integrationtest/crypto.go`; existing crypto / readback
  integration stays green with verbs sourced from the core-scenes manifest.
- **`holomush-qg1v5`** (the deferred end-to-end integration test) then asserts
  the qualified types end-to-end.

## Invariant registry

This design introduces one system invariant (the canonical-qualified-wire-type
rule above), registered per `.claude/rules/invariants.md` as **`INV-PLUGIN-40`**
in `docs/architecture/invariants.yaml` with `binding: pending` in this same
change. Implementation (the `Manifest.Validate` gate test + the meta-test) flips
it to `binding: bound` — see plan Task 8.

## Out of scope

- Re-architecting event type into structured fields (considered, rejected).
- Any change to `crypto.emits` / `requests_decryption` semantics or the
  `EnforceSensitivity` truth table.
- Backward compatibility / mixed-history handling (no users; explicitly waived).

## Resolves / related beads

- `holomush-r0kup` (P1 bug) — core-scenes scene IC content fails
  `EMIT_UNKNOWN_VERB` in production. **Resolved by this design** (core-scenes
  gains a `verbs:` block + qualified wire types); `r0kup` is blocked by `aneim`.
- `holomush-qg1v5` — end-to-end integration test (asserts qualified types).
- `holomush-72ti5` — `sensitive`-claim meta-test (pairs with the new
  qualified-type meta-test).
<!-- adr-capture: sha256=2dd056c93768a117; session=cli; ts=2026-06-07T02:30:01Z; adrs=holomush-yl3mf,holomush-8aure,holomush-1gwns -->
