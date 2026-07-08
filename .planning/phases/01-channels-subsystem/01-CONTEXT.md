# Phase 1: Channels Subsystem - Context

**Gathered:** 2026-07-08
**Status:** Ready for planning

<domain>
## Phase Boundary

Stand up `core-channels` — a new binary plugin providing persistent, named,
location-independent communication channels — with the same EventBus / ABAC /
audit substrate guarantees that `core-scenes` already proves. Delivers the full
initial scope of Epic 10 (`holomush-0sc`, sub-phases 10.1–10.5) **minus** op/deop
delegation, satisfying requirements CHAN-01…CHAN-05.

**In scope:** channel entity + repositories + plugin-owned schema (10.1); core
commands `channel create/join/leave/list/say/who/history` + `=name message`
shorthand (10.2); channel types public/private/admin + invite flow (10.3);
moderation `mute/ban/kick/transfer` (10.4, owner+admin only — see D-05); history
replay-on-join + tail scrollback + retention + background pruning (10.5); ABAC
`channel:` resource + `ChannelAttributeProvider` + seed policies; EventBus/audit
parity; second-consumer validation of the substrate pattern (INV-S7, N=2).

**Out of scope:** `op`/`deop` role delegation; true per-character faction
attribute gating; any web channel UI; full-text search (`holomush-0sc.8`); rich
text (`holomush-vrzu`); Discord/Slack bridge implementation (Epic 12); channel
crypto/encryption.

</domain>

<decisions>
## Implementation Decisions

### Scope
- **D-01:** Deliver the **full Epic-10 initial scope** (10.1 schema → 10.2 core
  commands → 10.3 types+invite → 10.4 moderation → 10.5 history) in this phase,
  narrowed only by D-05 (no op/deop). This exceeds the literal CHAN-01…05
  minimum by choice. OUT entirely: search, rich text, external bridging.

### Faction gating (CHAN-04)
- **D-02:** CHAN-04 "faction-restricted channels enforce membership-based
  access" is satisfied **now** by the `private`/`admin` channel types with
  player-level ABAC membership enforcement (distinct from `public`/open). This
  carries the spec's documented accepted limitation: enforcement is
  **player-level** (all of a player's alts gain access to a private channel).
- **D-03:** Build the **seam** for future per-character faction gating: ship a
  `ChannelAttributeProvider` resolving `resource.name`/`type`/`owner`/`archived`
  + `principal.channel_memberships`/`channel_banned`/`channel_muted`, and shape
  the seed policies keyed on `resource.name` so a future
  `principal.faction == "..."` clause slots in with **no schema migration and no
  policy rewrite**. The full character-attribute pipeline (Vampire-IC hidden
  from the same player's Werewolf alt) stays deferred.

### Crypto / message privacy
- **D-04:** All channel events are **plaintext** (`sensitivity: never`). **No
  `crypto.emits`** entries — the manifest declares no sensitive event types, so
  the crypto-reviewer gate does not fire for this phase. Rationale: coherent with
  the spec's explicit admin-full-access oversight, queryable/future-searchable
  history, and Discord/Slack bridge-readiness (bridged messages are plaintext
  regardless). Channels are community comms, not the participant-only RP boundary
  that scenes encrypt (INV-SCENE-60).

### Moderation authority (10.4)
- **D-05:** Role model is **member / owner + admin** — **no `op` role**.
  A channel **owner** moderates their own channel (mute/ban/kick) and admins
  override everything; `op`/`deop` delegation is **deferred**. Do NOT ship the
  `op` role value's command surface. (Note the reconciliation task: the
  spec's `channel_memberships.role` CHECK includes `'op'` — decide whether to
  keep the enum value dormant or drop it; keeping it dormant avoids a later
  migration and is the lighter path.)

### Channel creation
- **D-06:** **Admin-default creation + grant seam.** Creation is admin-only by
  seed policy (`seed:channel-admin-create`), the per-player rate limit (5/hour)
  is enforced, and the ABAC is shaped so an operator can grant `create:channel`
  to a role or specific players via policy with **no code change**. Mirrors the
  D-03 "ship now + seam" philosophy.

### History retention & pruning
- **D-07:** Retention default **30 days** (admin channels MAY be extended/
  unlimited per spec). The **background pruning job ships this phase** (it is a
  10.5 requirement; unbounded channel history is a real operational liability).
  Replay-on-join default 20 messages; history read never crosses the player's
  most-recent `joined_at` boundary; scrollback `count` capped at 500 at the
  service layer.

### Message payload contract
- **D-08:** Channel content events (`channel_say`/`channel_pose`) serialize as
  the canonical **`holomush.comm.v1.CommunicationContent`** (`actor_id`,
  `actor_display_name`, `text`, `no_space`, `ooc_style`) built via
  `pkg/plugin/comm/builder.go` — identical to `core-scenes`, so symmetric
  Lua/binary enforcement (EVTBUS-05) and shared CommLine/verb-registry rendering
  come for free. Channel identity comes from the **subject/stream**
  (`events.<game>.channel.<id>`) plus a **live name lookup by channel ID**, NOT a
  payload `channel_name` field (rename-safe; spec even forbids using payload
  `channel_name` for authz). Notice events (`channel_join/leave/mute/ban/kick/
  rename`) use their own small notification payloads (like scene notices).
  Bridge fields (`source`, `author_name` for non-game authors) are added in
  **Epic 12** — the design MUST NOT preclude them, but does not build them now.

### Claude's Discretion
- Exact command-parser wiring for the `=name` / `=name :pose` / `=name ;semipose`
  shorthand (reuse the say/pose `:`/`;`/`no_space` semantics from
  core-communication / the CommunicationContent builder).
- Whether the dormant `op` enum value is kept or dropped (D-05) — lightest path.
- Naming of the new channel `INV-<SCOPE>-N` invariants (see canonical refs /
  invariant note) — allocate in the appropriate scope, register at spec-write
  time.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Product intent (rich, but architecturally STALE — reconcile against current substrate)
- `docs/specs/2026-04-03-channels-architecture.md` — THE channels design:
  entity model, player-level membership + per-character gag, channel types,
  seeded channels + guest auto-join, command surface, moderation roles, history,
  ABAC integration, bridge-readiness. **Predates** the JetStream cutover, the
  plugin-capability architecture, the crypto model, and `core-scenes` shipping —
  so it is product-intent, NOT current architecture. Known reconciliations
  (see Reconciliation Constraint below): `type: core`→`type: binary`;
  `EventStore.ReplayTail`/colon streams `channel:<id>` → `EventBus`/
  `HistoryReader` + dot-subjects `events.<game>.channel.<id>`; bare
  `channel_say`/`channel_pose` → `<plugin>:<verb>` qualified wire types;
  LISTEN/NOTIFY invalidation → JetStream/event-driven; `ChannelMessagePayload`
  → `CommunicationContent` (D-08).

### Reference implementation to MIRROR (current substrate)
- `plugins/core-scenes/plugin.yaml` — the template manifest: `type: binary`,
  `resource_types`, `requires: [capability: ...]` least-privilege declarations,
  `provides`, `emits`, `history_scope`, `actor_kinds_claimable`, `config`,
  `audit:` (plugin-owned table), `verbs`, `actions`, `commands`, `policies`.
- `plugins/core-scenes/` — store.go, service, commands.go, audit.go,
  ops_events.go, lifecycle.go, migrations/ (plugin-owned schema), main.go.
  The whole-plugin shape channels mirrors.

### Payload contract
- `pkg/proto/holomush/comm/v1/comm.pb.go` — `CommunicationContent` fields.
- `pkg/plugin/comm/builder.go` — `Say`/`Pose`/`OOC`/`Emit` payload builders (D-08).
- `docs/superpowers/specs/2026-07-03-communication-content-contract-design.md` —
  EVTBUS-05, the canonical contract + symmetric Lua/binary enforcement.

### Event bus / wire conventions
- `.claude/rules/event-interfaces.md` — `Publisher`/`Subscriber`/`HistoryReader`
  (the deleted `EventStore` replacement); ordering owned by JetStream sequence.
- `.claude/rules/event-conventions.md` — dot-subject naming, `core.NewEvent()`,
  qualified vs bare event-type vocabularies (INV-PLUGIN-40), colon eradication.
- `docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md` — bus design.
- `docs/superpowers/specs/2026-06-06-event-type-wire-convention-design.md` —
  `<plugin>:<verb>` wire convention (so `core-channels:channel_say`).

### Plugin host / manifest / runtime symmetry
- `.claude/rules/plugin-manifest.md` — manifest field reference incl.
  `capability:` least-privilege `access:`/`scope:`, `audit:`, `commands`,
  `policies`, DAG validation.
- `.claude/rules/plugin-runtime-symmetry.md` — host trust checks apply to
  Lua+binary identically (binary here, but the emit fence / gates are shared).

### ABAC integration
- `docs/specs/2026-04-03-channels-architecture.md` §ABAC Integration — resource
  prefix, `ChannelAttributeProvider` attributes, capabilities, seed policies.
- `docs/specs/abac/01-core-types.md` — ABAC core types.
- `.claude/rules/abac-providers.md` — AttributeProvider MUST omit optional keys
  (no empty-string sentinels); `has_X` witness convention. Applies to the new
  `ChannelAttributeProvider`.
- `internal/access/prefix.go` — add `ResourceChannel = "channel:"`,
  `ChannelResource()` helper, `knownPrefixes` (integration point).
- `internal/command/types.go` — add `"channel"` to `validResourceTypes`;
  add channel actions (`join`/`leave`/`list`/`create`/`emit`/`read`/`delete`/
  `admin`/`invite`/`mute`/`ban`/`kick`/`transfer`) to `validActions`.
- `internal/access/policy/attribute/scene.go`, `stream.go` — provider precedent
  (scene.go is the closest analog; stream.go is the omit-optional reference).

### Verb registry / rendering (verify current relevance during research)
- `docs/specs/2026-03-28-comm-event-extensibility-design.md` — verb registry,
  `channel_say`/`channel_pose` types + rendering categories. Referenced by the
  channels spec; confirm it still matches the current verb-registration path
  (`internal/core/builtins.go` / plugin `verbs:` manifest entries).

### Invariants
- `.claude/rules/invariants.md` + `docs/architecture/invariants.yaml` — if the
  channels design mints a new named guarantee (e.g. channel privacy /
  membership-enforcement invariant), register it as `INV-<SCOPE>-N`
  (`binding: pending`) and regenerate `invariants.md` **in the same change**, or
  `TestEveryRegistryInvariantHasBinding` fails. INV-S7 (N=2 second-consumer rule)
  is validated by this phase (CHAN-05).

### Gateway boundary (relevant only if any surface touches the gateway)
- `.claude/rules/gateway-boundary.md` — telnet-first this phase; no web work, but
  if a future surface appears, structural writes go through typed BFF RPCs.

### Issue tracking
- bd epic `holomush-0sc` (Epic 10) + sub-beads `holomush-0sc.3`…`0sc.7`
  (10.1 schema … 10.5 history). Deferred: `holomush-0sc.8` (search), `vrzu`
  (rich text). Reconcile these with the GSD phase plan (do NOT mirror the bd
  dependency graph into `.planning/`).

## Reconciliation Constraint (standing — applies to every task)

The 2026-04-03 channels spec is **product-intent**, not current architecture.
Mirror `core-scenes`' **current** substrate in every mechanical decision:
`type: binary`; `EventBus`/`HistoryReader` (never the deleted `EventStore` or a
`ReplayTail` method on it); dot-subjects `events.<game>.channel.<id>` (never
colon `channel:<id>` for streams — colon survives ONLY in ABAC DSL type-prefixes
like `channel:01ABC` as a resource id); `<plugin>:<verb>` qualified wire types;
JetStream/event-driven cache invalidation (never Postgres LISTEN/NOTIFY);
`core.NewEvent()` for event construction; `crypto/rand` + `idgen.New()` for
channel/membership primary keys.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`plugins/core-scenes/`** — the whole-plugin template: manifest, store,
  service, commands, plugin-owned audit table + `PluginAuditService`,
  migrations, `main.go`. Channels is structurally a sibling.
- **`pkg/plugin/comm/builder.go` + `comm.v1.CommunicationContent`** — reuse
  directly for `channel_say`/`channel_pose` payloads (D-08).
- **AttributeProvider pattern** (`internal/access/policy/attribute/scene.go`,
  `stream.go`, `character.go`) — precedent for the new
  `ChannelAttributeProvider` (core provider, not plugin provider, per spec).
- **Focus hot-path cache decorator** (`holomush-wm0fi`, caching `session.Store`
  decorator with event-driven invalidation) — the modern precedent for the
  spec's "cache membership, invalidate on channel events" requirement (replaces
  the spec's stale LISTEN/NOTIFY approach).
- **Publish scheduler** (`core-scenes` `scheduler_interval` config + sweep) —
  precedent for the background **pruning** job (D-07).

### Established Patterns
- Plugin-owned Postgres schema + `audit:` manifest declaration →
  `PluginAuditService.AuditEvent`/`QueryHistory` (channels get their own
  `plugin_core_channels` schema + `channel_log`-style table).
- Two-layer ABAC: Layer-1 command-execution gate (`execute` on `command:channel*`)
  + Layer-2 per-capability resource check (`emit`/`read`/`join`/… on
  `channel:<id>`). Seed policies + a plugin `policies:` block.
- Host-brokered capabilities via manifest `requires: [capability: ...]` with
  least-privilege `access:`.
- Config knobs declared in the manifest `config:` block, decoded via
  `pluginsdk.DecodeConfig` at Init (retention window, replay count, prune
  interval, rate limits, message-size cap).

### Integration Points
- `internal/access/prefix.go` — new `channel:` resource prefix.
- `internal/command/types.go` — `validResourceTypes` + `validActions` additions.
- **Bootstrap seeding** — seeded channels (default incl. `Public`) + guest
  auto-join on session create; same pattern as seeded settings/content. Guests:
  seeded channels only, no create/moderate.
- **Character-enter / session-reconnect subscription flow** — subscribe to the
  character's own stream first, then look up player memberships, then subscribe
  to ungagged channel streams (spec §Character Visibility Overrides). Verify the
  current session/subscribe wiring (`internal/eventbus` Subscriber,
  session-liveness AUTHSESS-03) rather than the spec's stale flow.
- `plugins/core-channels/` (new) + `plugins/core-channels/migrations/`
  (plugin-owned schema) + wire into `task plugin:build-all` / plugin census.

</code_context>

<specifics>
## Specific Ideas

- Command vocabulary is fixed by the spec: `channel <sub>` (no symbol prefix) +
  `=<channelname> <message>` shorthand, with `:`/`;` → `channel_pose`
  (no-space semipose). Channel aliases `channel alias <short>=<name>`.
- Rendering format: `[Public] Sean says, "hello"` / `[Public] Sean waves` —
  channel prefix from event metadata, via the verb registry (same rendering
  path as scenes, not a bespoke renderer).
- Error uniformity: operations on channels a player cannot see MUST return an
  identical "channel not found" — never distinguish "doesn't exist" from
  "exists but hidden" (spec §Error Response Uniformity).
- Names: unique case-insensitive, regex `^[a-zA-Z0-9][a-zA-Z0-9_-]{0,31}$`;
  channel `delete` = archive (soft), not hard delete.

</specifics>

<deferred>
## Deferred Ideas

- **`op`/`deop` role delegation** — deferred from Phase 1 (D-05). Owner+admin
  moderation only this phase.
- **True per-character faction gating** (character-attribute ABAC pipeline;
  `principal.faction`) — seam built now (D-03), pipeline deferred. Future work.
- **Web channel surface** (list/post/manage/moderate UI) — deferred to its own
  phase; needs a web SPEC (WEBPORTFWD-01, `theme:web-portals`). Telnet-first now.
- **Full-text channel search** — `holomush-0sc.8` (P3).
- **Rich text (markdown/emoji) rendering** — `holomush-vrzu`.
- **Discord/Slack bridging** + `channel_bridges` table + payload `source`/
  `author_name` fields — Epic 12. Design must not preclude (D-08) but builds none
  of it.
- **`eventkit`/`groupkit` SDK extraction** — explicitly a follow-on to CHAN-05,
  NOT this phase (INV-S7): channels VALIDATES the two-consumer pattern; the
  extraction happens after, only if the pattern holds. Build core-channels to
  make the shared substrate patterns visible for later extraction.

</deferred>

---

*Phase: 1-Channels Subsystem*
*Context gathered: 2026-07-08*
