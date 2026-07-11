<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Skeptic verification — D1-HIGH-1 (event-sourcing claim/reality gap)

**Verdict: CONFIRMED** (and the doc-count sub-claim is actually an *undercount* — I found a 5th and 6th instance, including live public marketing copy).

## Sub-claim 1: writes are direct-write CRUD, not event-sourced

Verified in `internal/world/service.go`:

- `CreateLocation` (`:220-225`), `UpdateLocation` (`:244`), `DeleteLocation` — write straight to `s.locationRepo.Create/Update/Delete`. **No event emission at all** for any of these.
- `CreateExit` (`:332`), `UpdateExit` (`:357`), `DeleteExit` (`:378`) — same: repo write, **zero event emission**.
- `CreateObject`/`UpdateObject`/`DeleteObject` (`:465,490,522`) — same: repo write, **zero event emission**.
- `CreateCharacter`/`UpdateCharacterDescription` (`:672`)/`DeleteCharacter` — same: repo write, **zero event emission**.
- Only two verb families emit anything: **Move** (`MoveObject:571→587`, `MoveCharacter:803→841`) and **Examine** (`:904,962,1017`). In both, the repo write commits first, event emission is attempted second, and on emit failure the code explicitly tags the error `move_succeeded: true` / documents "the character row was already committed" (`service.go:806-809` comment) — i.e., non-atomic, best-effort, post-commit.

This is *stronger* than the finding's own framing: the finding says "events emitted AFTER db commit" for structural writes generally; in reality the majority of structural mutations (all Create/Update/Delete on locations, exits, objects, characters) emit **no event whatsoever** — only Move/Examine do. Grounding: `rg -n "eventEmitter|EmitMoveEvent|EmitEvent"` against `internal/world/service.go` returns exactly 5 call sites, all Move/Examine.

Confirmed there is exactly one write path: `rg` for `locationRepo\.(Create|Update|Delete)|characterRepo\.(Create|Update|Delete)|exitRepo\.(Create|Update|Delete)|objectRepo\.(Create|Update|Delete|Move)` across all of `internal/` returns only the 13 call sites inside `internal/world/service.go` itself — no other package writes to these repos, e.g. via an event-driven projector.

## Sub-claim 2: no rebuild-from-events path exists

Searched for `Apply`, `Rebuild`, `Replay`, `Project`, event-handler-updates-world-table patterns across `internal/world`, `internal/eventbus`, `internal/core`, `internal/store`. Findings, all negative for the claim:

- `internal/world/location.go` `ReplayPolicy`/`ParseReplayPolicy`/`DefaultReplayPolicy` — this is a **scene chat-history replay-to-newcomer UX feature** ("last:N" recent messages shown when joining a scene), unrelated to world-state derivation. Confirmed by reading the full file: it's a `Location` display-config field, not a state-rebuild mechanism.
- `internal/core/coretest/store_memory.go` `MemoryEventStore.Replay/ReplayTail` — explicitly commented "Test-inspection helper; not part of any production interface" (`:46,79,97`) and lives in `coretest`, which production code is barred from importing (depguard). Dead end for the claim.
- `internal/core/store.go:14-21` — the `EventAppender` interface doc comment is decisive: *"F7 removed the legacy EventStore (Append/Replay/Subscribe/SubscribeSession/UpdateCursors) that was backed by the PG events table. All new writes go through the JetStream event bus; the core Engine and command handlers only need Append."* This is the codebase's own admission that no replay-based state derivation exists anymore (if it ever did) — only one-way `Append`.
- `cmd/holomush/sub_grpc.go:233-235` verified verbatim: *"F7: EventWriter and PG events table are gone. Append goes directly to JetStream via busEventAppender."* Confirms production events flow to JetStream only, never back into world state.
- `internal/eventbus/audit/projection.go:416-424` verified: the only consumer of the event stream writes to `events_audit` (an audit log table), not to `locations`/`characters`/`exits`/`objects`. It's a write-only audit sink, not a state projector.
- `.claude/rules/event-interfaces.md` ("Current-state RPCs" section, project-owned rule file, not marketing) independently corroborates: *"Some UX flows need snapshot of current state rather than event replay. These bypass the EventBus entirely and read the relevant store directly... Do NOT reach for HistoryReader.QueryHistory when the requirement is 'what's the current state'."* This is the engineering team's own internal rule acknowledging current state is NOT sourced from event replay.

No `Subscribe`/`OnEvent`/handler function exists inside `internal/world` at all (`rg "func.*Subscribe|func.*OnEvent|func.*handle.*Event" internal/world` → zero hits).

## Sub-claim 3: the docs actually assert universal event sourcing

All four originally-cited docs checked verbatim, plus two more found independently:

1. `CLAUDE.md:274` — *"Event sourcing — actions produce immutable ordered events; state derives from replay."* Confirmed, unscoped, presented as a MUST-bearing architecture essential.
2. `site/src/content/docs/contributing/explanation/architecture.md:79` (*"Persistence - All actions are stored and replayable"*) **and** `:305-309` (*"### Event Sourcing — All game state changes are events — Events are immutable and ordered — Current state is derived from event replay"*) — confirmed, unscoped, universal.
3. `docs/specs/2026-03-28-site-redesign.md:176` — confirmed: marketing card copy *"Events all the way down | Every game action is an immutable event. Replay history, audit what happened, debug issues."*
4. `docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md:144` — **weaker citation than the other three**. Read in context (`:85,102,144,351`), this doc explicitly scopes "PostgreSQL becomes a *projection target*" to the `events_audit` table via the audit-projection consumer (`:144-146`), and explicitly disclaims world-model involvement at `:102` (*"Replacement of unrelated subsystems (ABAC, world model, plugin loader)"*). This citation is a partial overreach in the original finding — the jetstream design spec does NOT claim world-model state is event-sourced; it correctly and accurately describes the audit table as a projection (which the code genuinely implements). I'd drop or re-scope this citation.

**Two additional instances not in the original finding, found this session:**

5. `site/src/content/docs/contributing/reference/coding-standards.md:344-347` — near-verbatim duplicate of the architecture.md claim: *"### Event Sourcing — All game actions produce events — Events are immutable and ordered — State is derived from event replay."*
6. `site/src/content/docs/index.mdx:40-42` — **this is the live public-facing docs/marketing site**, not a draft spec. The "Events all the way down" card from the site-redesign spec shipped verbatim (lightly copyedited): *"Every game action is recorded as an event. Replay what happened, audit the history, debug problems. Nothing gets lost, and everything stays in order."* This means the universal-event-sourcing claim is not just internal documentation drift — it is a shipped, public claim about the product.

So: the "four docs" count in the original finding is accurate for what it cited, but understates total exposure — six locations across three tiers (contributor guide, architecture doc, public website) assert the same unscoped claim.

## Sub-claim 4: no ADR records the real CRUD decision

Searched `docs/adr/` (197 files, both the legacy `NNNN-*` series and the `holomush-<bd-id>-*` series) for `event sourc`, `CRUD`, `direct.write`, `world.state.*persist`, `structural.*write`. No hits describing a considered decision to use direct-write repositories for world state instead of event sourcing.

Checked the closest candidates by hand:

- `docs/adr/holomush-xx3e-properties-as-first-class-world-model-entities.md` — discusses property modeling (properties as first-class entities, circular-dependency avoidance), not the write-path architecture. Its one CRUD mention (`:136`, "entity's repository manages property CRUD") is incidental, not a considered ADR decision about event-sourcing vs. CRUD.
- `docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md:102` explicitly excludes "world model" from its own scope ("Replacement of unrelated subsystems (ABAC, world model, plugin loader)") — meaning the design that replaced the event infrastructure never even considered whether world-model writes should route through it.
- No `0005`-`0017` legacy ADR (command-state, unified-command-registry, command-security, ABAC engine, cedar semantics, deny-overrides, eager-attribute-resolution, properties-as-first-class, direct-static-access-control, three-layer-access-control, listen-notify-cache-invalidation, admin-readstream-bypass) addresses world-model persistence architecture.

Confirmed: no ADR owns the CRUD decision.

## Corrected severity

**No downgrade warranted — if anything the finding is conservative.** The doc-reality gap is real, verified at the exact cited lines, and the practical mechanism is worse than "best-effort post-commit notification" implies: most structural writes (all Create/Update/Delete on locations/exits/objects/characters) emit **zero events**, not merely "delayed" ones. The claim also under-counts doc exposure — it's shipped in live public marketing copy (`site/src/content/docs/index.mdx`), not just internal contributor docs. HIGH severity stands; the one adjustment is to soften/drop the `jetstream-event-log-design.md:144` citation (it's about the audit table specifically and is accurate as written) and add the two newly-found citations (`coding-standards.md:344`, `index.mdx:40-42`) as stronger replacements.
