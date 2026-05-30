<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Colon-Style Pub/Sub Subject Eradication — Design

**Bead:** `holomush-rops`
**Status:** design (brainstormed 2026-05-29)
**Supersedes / subsumes:** `holomush-ec22.3` (subjectxlate endgame decision), `holomush-pkixe` (scene history denial), `holomush-ofpi` (Subscribe-path scope-floor mismatch)

## Overview

HoloMUSH carries two representations for every pub/sub stream name. The
JetStream-native **dot form** (`events.<gid>.<domain>.<id>[.<facet>]`) is what
travels on the wire and into the durable audit; the legacy **colon form**
(`location:<id>`, `character:<id>`) is what internal Go code, the ABAC stream
classifiers, the gRPC `Stream` wire fields, and the SvelteKit client all speak.
The `internal/eventbus/subjectxlate/` package bridges the two at every publish,
every history read, and every translate-back-for-client.

`subjectxlate`'s own header names the migration that retires it — "F5 will
migrate plugin/host code to JetStream-native subjects, at which point the
translation becomes a no-op." That migration never shipped with the JetStream
cutover. The shim documented as transitional became permanent infrastructure,
and the dual representation is an active source of privacy-relevant bugs: a
stream-name classifier that recognizes one form but receives the other skips
its authorization branch (`holomush-pkixe`, `holomush-ofpi`).

This design eradicates the colon form as a **pub/sub stream name**, end to end,
in a single coordinated change, and deletes `subjectxlate`.

### Scenes already migrated; this finishes the job

Scenes Phase 4 (`holomush-5rh.13`, ADR `holomush-s9nu`) migrated **scene** IC/OOC
subjects to dot form and narrowed the scene classifiers to dot-only. It did so
on a premise that was not yet true — that read-path callers pass dot form — while
`ToLegacy` (`internal/grpc/server.go:663`) still hands clients colon-style scene
names. That gap is `holomush-pkixe`. This design makes the premise true for every
stream type, so the scene work and the `pkixe`/`ofpi` symptoms resolve together.

## Goals

- Every pub/sub stream name has exactly one representation — the dot form —
  in producers, classifiers, gRPC wire fields, the SvelteKit client, the admin
  break-glass read path, durable audit, specs, and docs.
- `internal/eventbus/subjectxlate/` is deleted.
- The colon form survives **only** as an ABAC policy-DSL resource/subject
  identifier (`character:<id>`, `scene:<id>`, `stream:<name>`, `plugin:<id>`),
  which is policy serialization, not a pub/sub subject.
- The privacy gate behavior that the dual representation broke or endangered
  (I-17 membership gate, temporal scope floor) is proven correct by test, not
  merely incidentally fixed.

## Non-goals

- **ABAC resource/subject TYPE-PREFIXES are not touched.** `character:`,
  `scene:`, `stream:`, `plugin:`, `system` as policy-DSL *type-prefixes*
  (`internal/access/`) keep their colon convention. This is the explicit
  boundary of `holomush-rops` and a hard constraint of this design (INV-ROPS-1,
  INV-ROPS-6). **However, DSL policy text that embeds a pub/sub stream *name* is
  not exempt** — the two location seeds (`seed.go:60,66`) match
  `resource.stream.name like "location:*"`, where `location:` is an embedded
  stream name, not a type-prefix. Those embedded names flip and the seeds are
  updated in lockstep (§1 seed clause, INV-ROPS-8). The "ABAC layer is the
  allowlist" framing applies only to the INV-ROPS-3 string scan, not to runtime
  policy semantics.
- **No backward-compatibility window.** The client and server ship in one
  deploy; there are no externally-pinned clients. No dual-format transition,
  no reserved-field deprecation, no migration shim.
- **No new stream domains.** This renames existing streams; it does not add
  notification or ambient stream infrastructure.

## Live migration surface (grounded 2026-05-29)

A producer/classifier survey narrows the bead's 2026-05-20 discovered scope:

| Stream | Colon (today) | Dot (canonical) | Live code? |
| --- | --- | --- | --- |
| Location | `location:<ULID>` | `events.<gid>.location.<ULID>` | **Yes** — `internal/core/engine.go:73,96`, `internal/world/events.go`, `internal/grpc/server.go:1294`, grid focus routing, scope floor, ABAC `StreamProvider`, **Lua SDK `pkg/holo/emit.go` `Emitter.Location()`**, **inline Lua `plugins/core-communication/main.lua`** (say/pose/ooc/emit/whisper_notice) |
| Character (personal) | `character:<ULID>` | `events.<gid>.character.<ULID>` | **Yes** — `stream_access.go`, `scope_floor.go`, `internal/grpc/server.go:1246`, `internal/core/event.go:17`, `internal/core/engine_end_session.go:62` (`NewEvent` producer), **Lua SDK `pkg/holo/emit.go` `Emitter.Character()`**, **inline Lua `plugins/core-communication/main.lua`** (page/whisper/pemit) |
| Global (ambient) | `global` | `events.<gid>.global` | **Yes** — **Lua SDK `pkg/holo/emit.go:21` `Emitter.Global()`** emits to stream name `"global"`. This is a live pub/sub stream, not merely an ABAC scope. |
| Scene IC/OOC | partly dot (`s9nu`) | `events.<gid>.scene.<ULID>.{ic,ooc}` | **Mostly done** — emit path + classifiers are dot (`s9nu`). BUT `internal/grpc/focus/scenepolicy/policy.go:29-30` `StreamsFor` still builds colon `scene:<id>:ic/:ooc` **subscription filters** — a producer `s9nu` missed (latent `ofpi`-class bug). This design flips it + extends the meta-test. |
| Notifications | `notifications:<charID>` | `events.<gid>.notification.<charID>` | **No live producer** — design-reference only (scenes v2 §3.1, future Phase 10). Spec-text update; no code to flip. |
| `system` | — | — | **Not a pub/sub stream** — ABAC actor-kind / audit source only. No emitter targets it as a stream. Out of scope. |

The live code migration is **location + character + global**, spanning host
producers (`internal/core`, `internal/world`, `internal/grpc`), the ABAC layer
(§1 seed clause), and the **Lua plugin SDK** (`pkg/holo/emit.go`). Notifications
is a documentation correction; scene is a meta-test extension. This refines the
bead's broader 2026-05-20 guess; `global` and the `pkg/holo` SDK surface were
surfaced during design review and correct an earlier under-count.

### Completeness mechanism (not an exhaustive manifest)

The per-step file lists below are **representative anchors**, not a complete
call-site manifest. Producing the exhaustive inventory is the `writing-plans`
job. Completeness is *guaranteed mechanically*, not by this prose:

- **INV-ROPS-3** (repo-wide string scan) fails CI on any surviving colon stream
  literal in non-`internal/access/` source — including test files with inline
  literals (e.g. `dispatcher_test.go`, `pkg/holo/emit_test.go`).
- **The compiler** forces every consumer of a deleted `StreamPrefix*` constant
  (e.g. `server.go:1246,1294`) to surface as a build error.

A file missed in the prose below is therefore caught at build or CI time, never
shipped. The plan enumerates; these two gates enforce.

## Architecture

### §1 Canonical model and the role split

After this work, a pub/sub stream name is **only** the dot form. There is no
second representation to translate to or from, so `subjectxlate` is deleted
rather than narrowed.

The central hazard is the string `character:<id>`, which serves two unrelated
roles today, both built as `"character:" + id`:

1. a **pub/sub stream name** (the character's personal event stream) — flips to
   `events.<gid>.character.<id>`;
2. an **ABAC subject/resource ID** in the policy DSL (e.g.
   `NewAccessRequest("character:"+id, …)` at `scope_floor.go:93`) — stays colon.

> **INV-ROPS-1.** Colon-style survives only as an ABAC policy-DSL
> resource/subject identifier. It MUST NOT appear as a pub/sub stream name in
> any producer, classifier, wire field, or client. The two roles are
> disambiguated **by role at the construction site**, never by sniffing the
> string at runtime.

**Mechanism.** The stream builders and the ABAC subject builders are kept
distinct, and the stream builders emit the **domain-relative dot reference**
(no gameID — see the next subsection on where gameID enters):

- The existing relative builders change their separator only:
  `world.LocationStream(id) → "location.<id>"` (was `"location:" + id`),
  `world.CharacterStream(id) → "character.<id>"`,
  `world.BroadcastLocationStream() → "location.*"`. Signatures are unchanged
  (id only); just the `:`→`.` in the returned reference. The colon constants
  `world.StreamPrefixLocation/Character` (`events.go:23-24`) and
  `core.StreamPrefixCharacter` (`event.go:17`) are deleted; callers route through
  the builders or emit relative dot refs inline.
- ABAC subjects/resources are constructed through the **existing**
  `internal/access` builders — `access.CharacterSubject(id)`,
  `access.SceneResource(id)`, `access.PluginSubject(name)`, etc.
  (`internal/access/prefix.go`), already the established convention across
  `world/`, `command/`, `grpc/`, `hostfunc/`. These return the colon form
  (`character:<id>`) and live inside the scan-allowlisted `internal/access/`
  package. A handful of straggler host sites still inline `"character:"+id`;
  routing them through the builders (a cleanup `prefix.go` explicitly calls for)
  means **no inline colon literal survives in host code outside
  `internal/access/`** — making the role split a real package boundary, not a
  convention.
- `global` (`pkg/holo/emit.go:21`) is already a bare token with no separator; its
  relative reference is unchanged. Only the host qualifier must keep producing
  `events.<gid>.global` after the shim is gone.

Today the stream form and the ABAC-subject form are both an inline
`"character:" + id`; routing stream construction through the relative builders
(which emit `character.<id>`) while ABAC subjects keep `character:<id>` makes the
two roles structurally unconflatable. Note `world.CharacterStream(id)` returns a
relative reference, **not** a valid `eventbus.Subject` — `NewSubject` requires the
`events.` prefix (`types.go:257`), which only the host qualifier adds.

### The `stream:` ABAC resource straddle

The ABAC *resource* for a stream is `stream:<name>`. The `stream:` type-prefix
is policy serialization and stays colon; the embedded `<name>` is a pub/sub
stream name and flips to dot. So the resource string becomes
`stream:events.<gid>.location.<ULID>`.

This forces a lockstep change in `StreamProvider.ResolveResource`
(`internal/access/policy/attribute/stream.go:46`), which today extracts the
`resource.location` DSL attribute via `strings.HasPrefix(id, "location:")`. If
the embedded name flips to dot while this parser still matches `location:`, the
`location` attribute is silently omitted and every `resource.location ==
principal.location` policy fails closed. The parser MUST become dot-aware in the
same change. This file is the `abac-providers.md` canonical fail-safe example,
so its fix follows the "omit, do not sentinel" rule (INV-ROPS-7's provider test).

### The two location seed policies and the `has_location` witness

`seed:player-stream-emit` (`internal/access/policy/seed.go:60`) and
`seed:player-location-stream-read` (`:66`) each match:

```text
resource.stream.name like "location:*" && resource.stream.location == principal.character.location
```

The *access control* is the second clause — co-location, attribute-based,
**unchanged** by the flip. The `like "location:*"` clause is a **stream-type
guard** ("is this a location stream at all?"). Its job is to stop a non-location
stream that happens to carry a matching `location` attribute from satisfying the
co-location clause alone (a fail-open it prevents). Under the flip, the name is
`events.<gid>.location.<id>` and `like "location:*"` matches nothing, silently
revoking location emit/read for every non-staff character — a fail-closed
regression no classifier or string-scan invariant catches (the INV-ROPS-3 scan
excludes `internal/access/`).

The fix is to replace the name-pattern type-guard with the **`has_location`
witness**: `StreamProvider` MUST emit a `has_location` boolean alongside the
optional `location` attribute (per the `abac-providers.md` witness convention,
which the current provider omits), and the seeds test that witness instead of
the name. This decouples the seeds from the subject-string format entirely — a
future format change cannot re-break them. The co-location clause is untouched.
Pinned by INV-ROPS-8.

### Relative reference vs qualified subject — where gameID enters

The fully-qualified dot subject embeds the gameID (`events.<gid>.location.<id>`),
but the producers and clients that name streams **do not hold the gameID**.
Today `subjectxlate.Legacy` injects it host-side at two boundaries:

- **Emit:** a plugin emits `subjectRaw = "location:<id>"` (no gameID); the host
  prepends `events.<gid>.` at `internal/plugin/event_emitter.go:201-207`. Host
  producers (`engine.go:73`) likewise set a colon `Stream` field qualified
  downstream.
- **Read:** the client sends a colon `req.Stream`; the classifiers at
  `query_stream_history.go:173,177` run on that raw field, while
  `fetchHistoryFromBus` (`:425`) qualifies it with the session gameID for the
  bus fetch.

So deleting `subjectxlate` does not just swap `:`→`.` — it relocates **where
gameID enters the subject**. This design fixes that explicitly:

> **Canonical forms.** A *domain-relative reference* (`location.<id>`,
> `character.<id>`, `scene.<id>.ic`, `global`) is what producers and clients —
> which lack the gameID — emit. A *qualified subject*
> (`events.<gid>.<relative>`) is what the bus, durable audit, and **all
> classifiers** see. gameID is host/server-owned and is prepended at exactly two
> boundaries, each replacing a `subjectxlate` call:
>
> - **Emit qualifier** — `event_emitter.go:207` (plugins) and the host core emit
>   path prepend `events.<gid>.` to the producer's relative reference.
> - **Read-entry qualifier** — `QueryStreamHistory` / `Subscribe` qualify
>   `req.Stream` to the fully-qualified dot form from the **session** gameID
>   **before** the classifier switch. Classifiers therefore only ever see the
>   qualified form (matching `isSceneStream`'s existing `events.<gid>.…`
>   expectation), and this is the single point where INV-ROPS-2 entry-rejection
>   lives. This also subsumes the `pkixe`-era "normalize at entry" idea.

This is why the §1 stream builders stay relative (`world.LocationStream(id) →
"location.<id>"`, id only, no gameID): they produce the relative reference, and
a single host-side **qualifier** prepends `events.<gid>.` before the result is
turned into an `eventbus.Subject` (which `NewSubject` requires to start with
`events.`). The qualifier is a small new helper — `eventbus.Qualify(gid,
relativeRef) → events.<gid>.<relativeRef>` — that replaces the two
`subjectxlate.Legacy` call sites (emit + read-entry). INV-ROPS-4 asserts the
producer's relative reference, once qualified, equals the subject the classifier
expects.

### §2 I-17 classifiers and the fail-closed discipline

The I-17 gate (`internal/grpc/stream_access.go`) decides whether a stream is
private (membership-only, no ABAC override). `holomush-pkixe` proved that a
classifier which receives a form it does not recognize returns "not private,"
skips the membership gate, and falls through to the ABAC default branch — today
a default-deny (fail-closed, broken reads), but a latent fail-**open** the moment
any `stream:*` permit seed exists.

Because the cutover is big-bang (below), the classifiers become **dot-only and
reject colon outright**. There is no dual-format window, so there is no
half-recognized state to fall through.

Classifiers that flip (all `internal/grpc/`):

| Classifier | Today | After |
| --- | --- | --- |
| `isPrivateStream` (`stream_access.go:70`) | `HasPrefix("character:")` ∨ `isSceneStream` | dot character ∨ `isSceneStream` |
| `sessionHasMembership` (`stream_access.go:90`) | `TrimPrefix("character:")` | parse dot character subject |
| `isLocationStream` (`scope_floor.go:69`) | `HasPrefix("location:")` | dot location shape-check |
| `streamScopeFloor` character branch (`scope_floor.go:49`) | `HasPrefix("character:")` | dot character branch |

Each classifier uses the exact-segment-count discipline `isSceneStream` already
models (`len(parts) != N → false`; empty gid/id → false). A location stream is
4 segments (`events.<gid>.location.<id>`); a scene stream is 5
(`events.<gid>.scene.<id>.<facet>`); the segment count plus the domain token at
`parts[2]` distinguishes them with no overlap.

> **INV-ROPS-2.** `QueryStreamHistory` / `Subscribe` qualify `req.Stream` to the
> fully-qualified dot form (from the session gameID) at handler entry, **before**
> the classifier switch. A stream that does not qualify to a valid dot-style
> private or public subject MUST be rejected there with `INVALID_ARGUMENT`, never
> allowed to fall through the classifier chain to a default authorization branch.
> Classifiers see only qualified subjects. This closes the `pkixe`/`ofpi`
> fall-through class permanently and is the read-entry qualifier from §1.

### §3 Cutover: one coordinated change

There is no compatibility window to respect, and the chosen strategy is
**big-bang**: flip the canonical form everywhere in a single PR and delete the
shim in the same PR. Rationale: any intermediate state is a half-migrated
classifier — exactly the `pkixe` failure mode. A zero-width dual-format window
is the smallest possible fail-open surface.

Within the single PR, edit in dependency order:

1. **Constructors** — add the dot-style stream constructors (§1); add nothing to
   the ABAC subject path. Establishes the role split before call sites move.
2. **Producers** — emit dot directly:
   - **Host:** `internal/core/engine.go:73,96`; `internal/world/events.go`
     AND `internal/core/event.go:17` (delete the `StreamPrefix*` colon constants
     in **both** files); `internal/grpc/auth_handlers.go:876`; the
     `world.StreamPrefix*` consumers the compiler surfaces on deletion, including
     `internal/grpc/server.go:1246,1294` (`dispatchDelivery` follower checks);
     `core-communication` emit paths.
   - **Lua plugin SDK:** `pkg/holo/emit.go` — `streamPrefixCharacter`,
     `streamPrefixLocation`, `streamPrefixGlobal` constants and the
     `Emitter.Character()` / `Location()` / `Global()` methods → emit a
     **domain-relative dot reference** (`location.<id>`, `character.<id>`,
     `global`); the SDK lacks the gameID, so it does NOT prepend `events.<gid>.`
     (per §1, the host qualifier does). The change here is the separator
     (`:`→`.`) and dropping the colon. Its tests (`pkg/holo/emit_test.go`)
     hardcode colon expectations that update with it.
   - **Test assertions** with inline colon literals (e.g.
     `internal/grpc/dispatcher_test.go` `core.StreamPrefixCharacter` usages at
     lines ~444/455/552/563 plus inline `"location:"`/`"character:"` literals) —
     the plan enumerates these; INV-ROPS-3 backstops any miss.
3. **Classifiers + ABAC provider** — flip the four `internal/grpc/` classifiers
   (§2) and `StreamProvider.ResolveResource` (§1) in lockstep; `StreamProvider`
   additionally emits the `has_location` witness (§1 seed clause / INV-ROPS-8).
4. **ABAC seeds** — rewrite the `like "location:*"` type-guard in
   `seed:player-stream-emit` (`seed.go:60`) and `seed:player-location-stream-read`
   (`seed.go:66`) to test the `has_location` witness; leave the co-location
   clause untouched. Bump each seed's `SeedVersion`.
5. **Qualifiers + wire + shim deletion** — proto `Stream`/`streams` fields and
   the SvelteKit client now carry domain-relative dot references (§1). **Replace**
   (not merely delete) the two gameID qualifiers `subjectxlate` performed:
   - **Emit qualifier** — `event_emitter.go:207` and the host core emit path
     prepend `events.<gid>.` to the producer's relative reference directly.
   - **Read-entry qualifier** — `QueryStreamHistory` / `Subscribe` qualify
     `req.Stream` from the session gameID at entry (INV-ROPS-2), replacing the
     downstream `subjectxlate.Legacy` at `query_stream_history.go:425,494`.

   Then delete the remaining `subjectxlate.Legacy`/`ToLegacy` callers
   (`server.go:663`, `sub_grpc.go`, `internal/admin/readstream/`, **and the test
   harness `internal/testsupport/integrationtest/session.go:585` +
   `harness.go:1155,1197`** — these fail compilation otherwise, taking
   INV-ROPS-4's harness down with them; the stale doc-comment at `crypto.go:160`
   also updates) and delete the `subjectxlate` package.
6. **SvelteKit client** — `web/src/lib/backfill/*` constructs/consumes dot; its
   tests update.
7. **Invariant-test cleanup + specs / docs / rules** — remove INV-P4-1
   (`internal/test/invariants/scene_subjects_test.go`) AND its entry in the
   coverage bijection (`inv_p4_coverage_meta_test.go`) in the same change, or
   `TestINV_P4_Coverage_Meta` fails on the dangling reference; iwzt §3
   stream-type table; scenes-v2 §3.1 stream-naming table (incl. the notifications
   row); `.claude/rules/event-conventions.md`; the stale
   `site/docs/contributing/architecture.md` (`ec22.18`); and an **amendment to
   ADR `holomush-s9nu`** recording that INV-P4-1 is superseded by INV-ROPS-3 (its
   Consequences section otherwise misdescribes the codebase).

### Sequencing relative to `pkixe`

`holomush-pkixe`'s tactical "sniff both forms" fix was aborted (2026-05-29): it
is blocked by INV-P4-1 (a CI meta-test forbidding colon `scene:` literals in the
read-path files) and would manufacture the very half-migrated state this cutover
eliminates. `pkixe` and `ofpi` are now blocked-by `rops`; this design is their
fix. `pkixe`'s root-cause analysis — including the INV-P4-1 false-premise
finding and the invariant-n temporal-floor leak — is direct input here.

## Invariants

All invariants are numbered, asserted by a test, and covered by a meta-test or
boundary test as noted. RFC2119 keywords are normative.

- **INV-ROPS-1** — Colon-style appears only as an ABAC policy-DSL identifier;
  never as a pub/sub stream name. *(Enforced executably by INV-ROPS-3 +
  INV-ROPS-6.)*
- **INV-ROPS-2** — Unclassifiable stream names are rejected at handler entry
  with `INVALID_ARGUMENT`, never routed to a default authorization branch.
- **INV-ROPS-3 (eradication gate)** — A CI meta-test asserts no production Go or
  Lua source (`.go` + `.lua` — `core-communication/main.lua` builds subjects
  inline, bypassing `pkg/holo`; the SvelteKit client is covered by its own Task-6
  flip + web tests) contains a colon-style entity-prefix literal (`location:`,
  `character:`, `notifications:`, `scene:`, `plugin:`, …) as a **stream name**.
  Distinguishing a stream-name literal from a legitimate ABAC subject/resource
  literal is solved structurally, not heuristically: **ABAC subjects/resources
  are built only via `internal/access` builders** (`access.CharacterSubject`,
  `SceneResource`, …), which live in the scan's sole allowlisted package. The
  scan therefore: (a) skips `internal/access/`; (b) skips comment lines; (c)
  skips lines that call an ABAC evaluation API (`Evaluate(`, `CanPerformAction(`,
  `NewAccessRequest(`, `.Grant(`) or carry the `// ABAC resource ref` marker —
  the residual for plugin code (`plugins/core-scenes/`), which cannot import
  `internal/access`. Host code has **zero** inline colon literals (all route
  through the builders), so any host hit is unambiguously a stream-producer bug.
  This **subsumes** INV-P4-1's 3-file scene scope with a strictly stronger
  repo-wide gate; INV-P4-1 is removed in favor of it. The scan MUST **fail** (not
  `t.Skipf`) on a missing root — the old INV-P4-1 skip-on-missing logic
  (`scene_subjects_test.go:62-73`) is a false-pass trap a repo-wide scan cannot
  inherit.
- **INV-ROPS-4 (producer↔subscriber symmetry)** — An integration test (real
  embedded NATS via `eventbustest`) emits through the production producer path
  for each migrated stream type and asserts a subscriber built from the
  production filter constructor receives it. Proves emit-subject == filter-string
  end to end; the only guard against silent non-delivery, which neither the
  string scan nor unit classifier tests can catch.
- **INV-ROPS-5 (classifier non-collision)** — A table-driven unit test over the
  four `internal/grpc/` classifiers asserts: a location stream is
  public-not-scene; a character stream is private-not-scene; a scene stream is
  private-and-scene; an unknown/malformed name is none. Guards against
  segment-count confusion (4-segment location vs 5-segment scene) granting or
  dropping the membership gate.
- **INV-ROPS-6 (role split, both directions)** — For the same character ULID, a
  test asserts the **stream** is dot (`events.<gid>.character.<id>`) and the
  **ABAC subject** is colon (`character:<id>`). Makes INV-ROPS-1 executable and
  guards against an over-eager sweep migrating the ABAC subject and silently
  breaking character-principal policies.
- **INV-ROPS-7 (temporal floor on every private stream)** — A test asserts a
  late joiner cannot read pre-join history on each private stream type — the
  scope floor (`JoinedAt` / `LocationArrivedAt`) is applied, not the zero-floor
  default. Directly from `pkixe`'s invariant-n finding (colon scene streams leaked
  pre-join history). Paired with a `StreamProvider` test asserting that, for a dot
  location stream, `resource.location` is **populated** and the `has_location`
  witness is `true`; and for non-location streams `location` is **absent** (not
  empty-sentinel, per `abac-providers.md`) while `has_location` is **present and
  false**.
- **INV-ROPS-8 (location-seed authorization survives the flip)** — An integration
  test seeds the engine and asserts: a co-located character can `emit` to and
  `read` its own dot-form location stream, and a non-co-located character cannot.
  Catches the silent fail-closed regression in `seed:player-stream-emit` /
  `seed:player-location-stream-read` (the `like "location:*"` type-guard) that
  the INV-ROPS-3 string scan cannot see because it excludes `internal/access/`.
  Depends on the `has_location` witness (§1 seed clause).

A meta-test (INV-2-style bijection) asserts every `INV-ROPS-*` number above maps
to at least one test, so the set cannot silently shrink.

## Testing strategy

| Tier | Tests |
| --- | --- |
| Unit | INV-ROPS-5 classifier matrix; INV-ROPS-6 role split; `StreamProvider` dot-parse + `has_location` witness (INV-ROPS-7 pair); every constructor output parses as a valid `eventbus.NewSubject` (token-rule regression guard, plain test, not an INV) |
| Integration (`//go:build integration`, `eventbustest`) | INV-ROPS-4 producer↔subscriber round-trip per stream type; INV-ROPS-2 handler-entry rejection; INV-ROPS-7 late-joiner temporal-floor and I-17-membership-gate-identity assertions on `QueryStreamHistory` + `Subscribe`; INV-ROPS-8 seeded location emit/read authorization (co-located permit, non-co-located deny) |
| Meta | INV-ROPS-3 repo-wide colon-stream-literal scan, fail-on-missing-target (replaces INV-P4-1); INV-ROPS bijection registry test |

I-17 identity assertions (INV-ROPS-7) check that a private read is authorized **by
the membership gate**, not merely that it is allowed — per the `pkixe` tell (the
empty `policy_id` + "by ABAC" log line). A regression that re-routes private
reads through ABAC fails even when ABAC happens to permit.

## Scope ledger

**Explicitly NOT touched:**

- ABAC policy-DSL resource/subject **type-prefixes** (`character:`, `scene:`,
  `stream:`, `plugin:`, `system`) — `internal/access/`. INV-ROPS-1 / INV-ROPS-6.
- Scene IC/OOC subjects — already dot (`s9nu`); only the meta-test scope changes.
- `system` — ABAC actor-kind / audit source, not a pub/sub stream. No emitter
  targets it; no stream is invented for it. *(`global`, by contrast, IS a live
  stream via the Lua SDK and is in scope — see the surface table.)*
- Notification stream infrastructure — no live producer; spec-text correction only.

**Touched inside `internal/access/` despite the type-prefix exemption** (the
boundary correction from design-review round 1):

- `StreamProvider.ResolveResource` (`stream.go:46`) — dot-aware parse + new
  `has_location` witness.
- The two location seeds (`seed.go:60,66`) — `like "location:*"` type-guard →
  `has_location` witness; co-location clause unchanged; `SeedVersion` bumped.

These are the places where a pub/sub stream *name* is embedded in policy code;
they flip even though the surrounding type-prefixes do not.

## Open questions

None blocking. The notification stream name (`events.<gid>.notification.<charID>`)
is provisional spec text only; it binds nothing until a producer is built.

## References

- `internal/eventbus/subjectxlate/subjectxlate.go` — the shim this design deletes
  (its header names this migration "F5")
- `internal/grpc/stream_access.go`, `scope_floor.go`, `query_stream_history.go` —
  the I-17 classifiers and read-path auth switch
- `internal/access/policy/attribute/stream.go` — the ABAC `StreamProvider` straddle
- `docs/superpowers/specs/2026-05-16-social-spaces-substrate-contract.md` §1.1 —
  mandates dot-style for new code; this closes the gap on legacy code
- `holomush-pkixe`, `holomush-ofpi`, `holomush-ec22.3` — subsumed beads
- `.claude/rules/abac-providers.md` — the omit-don't-sentinel fail-safe rule

<!-- adr-capture: sha256=2d647005fecc29f6; session=brainstorm-rops; ts=2026-05-29T00:00:00Z; adrs=holomush-21ahd,holomush-wia9e -->
