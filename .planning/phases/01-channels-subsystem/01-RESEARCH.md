# Phase 1: Channels Subsystem - Research

**Researched:** 2026-07-08
**Domain:** HoloMUSH binary plugin (`core-channels`) on the shipped EventBus / ABAC / plugin-host substrate
**Confidence:** HIGH (all findings grounded in in-tree `core-scenes` reference + host substrate code; every non-obvious claim carries a `path:line` citation)

## Summary

`core-channels` is a greenfield binary plugin that is a **structural sibling of `plugins/core-scenes/`**. Every substrate touchpoint scenes uses — plugin-owned Postgres schema, `PluginAuditService` audit table, plugin `AttributeResolverService` for ABAC resource attributes, `EventSink.Emit` with `<plugin>:<verb>` wire types, manifest `commands`/`policies`/`config`, `CommunicationContent` payloads via `pkg/plugin/comm` — is directly reusable. Build channels by mirroring scenes file-for-file and swapping the domain (scene→channel, participants→members, location-bound→location-independent).

**The 2026-04-03 channels spec is product-intent but architecturally stale.** Where it disagrees with `core-scenes`, scenes wins (per CONTEXT Reconciliation Constraint). The single highest-value reconciliation this research surfaces: **channel membership must be modeled RESOURCE-side (resolved by the plugin's `ChannelResolver.ResolveResource`, exactly like `resource.scene.participants`), NOT principal-side.** The plugin `AttributeResolverService` proto has only `ResolveResource` + `GetSchema` — there is **no** subject/principal resolution RPC (`api/proto/holomush/plugin/v1/attribute.proto:44,63`; `internal/plugin/attribute_proxy.go:44-47` `ResolveSubject` returns nil). The stale spec's `principal.channel_memberships` cannot be resolved by a plugin, and a core in-process provider cannot read the plugin's isolated `plugin_core_channels` schema. This is a locked-decision landmine (D-03) the planner must resolve — see §Landmines.

**The second highest-value finding:** channels' live-delivery mechanism is **`session_streams: true` + the `QuerySessionStreams` RPC** (`internal/plugin/manager.go:1508-1560`, `internal/plugin/host.go:57-60`, `api/proto/holomush/plugin/v1/plugin.proto:48`) for subscribe-at-session-establishment, plus the host **`StreamRegistry.AddStreamWithMode(..., ReplayModeLiveOnly)`** interface (`internal/plugin/host.go:32-41`) for mid-session join/leave. The `AddStreamWithMode` doc comment literally reads "*e.g., LIVE_ONLY for channels*" — the substrate anticipated channels. BUT **no plugin implements `QuerySessionStreams` today** (grep: zero implementers under `plugins/`), and the plugin-SDK-facing wiring for both `QuerySessionStreams` and a `StreamRegistry` host capability appears absent from `pkg/plugin/`. For session-stream subscription, **core-channels is the FIRST consumer**, so some host/SDK plumbing is likely net-new work — the planner must scope it.

**Primary recommendation:** Scaffold `plugins/core-channels/` as a scenes clone; model membership resource-side via a plugin `ChannelResolver`; declare `resource_types: [channel]` (auto-registers the type + ABAC proxy — no core `validResourceTypes` edit needed); emit content as `CommunicationContent` on `events.<game>.channel.<id>` with wire type `core-channels:channel_say`; audit to `plugin_core_channels.channel_log`; drive live delivery via `session_streams`+`QuerySessionStreams`+`StreamRegistry`; ship a background prune sweep modeled on `publishScheduler`.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Channel entity + membership storage | Plugin (Postgres `plugin_core_channels`) | — | Plugin-owned schema, mirror `scenes`/`scene_participants` |
| Channel content emission (say/pose/ooc) | Plugin → host `EventSink.Emit` | EventBus/JetStream | `<plugin>:<verb>` wire types; host owns the bus |
| Durable channel history / audit | Plugin `PluginAuditService` (`channel_log`) | Host JetStream audit consumer (ack-and-skip → forward) | Same F5 pattern as `scene_log` |
| ABAC channel resource attributes | Plugin `ChannelResolver.ResolveResource` | Host ABAC engine (proxy provider) | Resource-side membership, mirror `SceneResolver` |
| ABAC command/action authorization | Host ABAC engine + manifest `policies` | — | Two-layer gate (Layer-1 command, Layer-2 per-resource) |
| Live message delivery to members | Host session subscription (`QuerySessionStreams` / `StreamRegistry`) | Plugin computes stream set | Host owns session→stream fan-out |
| Telnet command surface | Host command dispatcher → plugin `HandleCommand` | — | `commands:` manifest + `HandleCommand` router |
| Retention pruning | Plugin background goroutine (ticker sweep) | — | Mirror `publishScheduler.Run` |

## Reconciled Architecture (non-stale picture)

How `core-channels` fits the CURRENT substrate, item-by-item against the stale 2026-04-03 spec:

| Concern | Stale spec says | CURRENT substrate (what to build) | Grounding |
|---------|-----------------|-----------------------------------|-----------|
| Plugin type | `type: core` | `type: binary` | `plugins/core-scenes/plugin.yaml:6` |
| History read | `EventStore.ReplayTail` | `PluginAuditService.QueryHistory` (JetStream recent → Postgres audit fallback, transparent) | `plugins/core-scenes/audit.go:493`; `.claude/rules/event-interfaces.md` |
| Subjects | colon `channel:<id>` | dot `events.<game>.channel.<id>`; producer emits domain-relative, host `Qualify` prepends | `plugins/core-scenes/store.go:1832-1844`; `.claude/rules/event-conventions.md` |
| Event wire type | bare `channel_say` | qualified `core-channels:channel_say`; registered-emit set + `crypto.emits` stay bare | `plugins/core-scenes/plugin.yaml:82-98`; INV-PLUGIN-40/32 |
| Cache invalidation | Postgres LISTEN/NOTIFY | JetStream / event-driven; `StreamRegistry` for live sub changes | `internal/plugin/host.go:32-41` |
| Content payload | `ChannelMessagePayload` | `holomush.comm.v1.CommunicationContent` via `pkg/plugin/comm` | `pkg/plugin/comm/builder.go:40-66`; D-08 |
| Membership location | `principal.channel_memberships` (principal-side) | **`resource.channel.members` (resource-side), resolved by plugin `ChannelResolver`** | `plugins/core-scenes/resolver.go:74-128`; attribute proto has no subject RPC (§Landmines) |
| Event construction | (n/a) | `EventSink.Emit(EmitIntent{...})`; host stamps ULID via `core.NewEvent()` | `plugins/core-scenes/publish_events.go:57-62` |
| Entity PKs | (n/a) | `crypto/rand` ULID (`idgen.New()` for entities; ops IDs like `ope-<ulid>`) | `plugins/core-scenes/ops_events.go:103-112` |
| Crypto | (varies) | **PLAINTEXT — `sensitivity: never`, NO `crypto.emits`, no crypto-reviewer gate** | D-04; contrast `plugins/core-scenes/plugin.yaml:144-164` |

## Concrete File / Package Layout

`plugins/core-channels/` — every file mapped to its `core-scenes` analog. Follow the reference's structure; the channel domain is simpler (no pose-order, no publish-vote, no focus-redirect, no crypto).

| New file | core-scenes analog | Purpose |
|----------|--------------------|---------|
| `plugin.yaml` | `plugin.yaml` (`plugins/core-scenes/plugin.yaml:1-387`) | Manifest: `type: binary`, `resource_types: [channel]`, `requires`, `provides`, `emits: [channel]`, `session_streams: true`, `storage: postgres`, `config`, `audit`, `verbs`, `actions`, `commands`, `policies` |
| `main.go` | `main.go:1-291` | `channelPlugin` struct implementing `Handler`, `ServiceProvider`, `AttributeResolverProvider`; `Init` opens store + wires service/resolver/auditSrv + starts prune goroutine; `main()` builds `EmitRegistry` |
| `service.go` | `service.go` (`SceneServiceImpl`) | gRPC `ChannelService`: create/join/leave/list/post/who/history/invite/mute/ban/kick/transfer; self-enforces ABAC per verb (INV-SCENE-65 analog) |
| `store.go` | `store.go` (`SceneStore`) | Postgres channel domain-state: channels, memberships, bans/mutes, name lookup; `dotStyleChannelSubject` helper (analog `store.go:1832-1844`) |
| `resolver.go` | `resolver.go:1-129` | `ChannelResolver` (plugin `AttributeResolverService`): `GetSchema` + `ResolveResource` returning `name`/`type`/`owner`/`archived`/`members`/`banned`/`muted` |
| `audit.go` | `audit.go:1-798` | `ChannelAuditServer` (`PluginAuditService`): `AuditEvent` (idempotent INSERT into `channel_log`), `QueryHistory` (membership-gated stream, auth step-1), `parseChannelSubject` |
| `ops_events.go` | `ops_events.go:1-113` | `channel_ops_events` append-only journal for membership/moderation ops (join/leave/kick/ban/mute/transfer/create/archive) |
| `commands.go` | `commands.go` (`dispatchCommand`) | `HandleCommand` router for `channel <sub>` + `=name msg` shorthand; builds `CommunicationContent` via `comm.Say/Pose/OOC` and calls `eventSink.Emit` (analog `commands.go:1320-1364`) |
| `types.go` | `types.go`, `lifecycle.go:1-59` | Channel types (`public`/`private`/`admin`), role enum (`owner`/`member`; dormant `op` per D-05), state (active/archived) + transition validation |
| `prune.go` (new) | `publish_scheduler.go:1-115` | Background retention sweep: ticker → delete `channel_log` rows older than retention window per channel |
| `migrations/000001_channels.up.sql` + `.down.sql` | `migrations/000003_...up.sql` + `.down.sql` | Plugin-owned schema (see §Substrate Wiring) |
| `migrations/000002_create_channel_log.up.sql` + `.down.sql` | `migrations/000004_create_scene_log.up.sql` (`:16-33`) | Audit table mirroring host `events_audit` columns |
| `core_channels_suite_test.go` | `core_scenes_suite_test.go` | Ginkgo suite bootstrap (`//go:build integration`) |
| `*_test.go` (unit) | `service_test.go`, `audit_test.go`, `resolver_test.go`, `commands_test.go`, `manifest_verbs_test.go` | Table-driven unit + Ginkgo integration + store integration (testcontainers) |

**Host-side edits (outside the plugin dir):**

| Edit | File | Reason |
|------|------|--------|
| Add `"core-channels"` to `expectedPlugins` | `test/integration/wholesystem/census_test.go:25-27` | Whole-system census asserts the plugin loads (INV-PLUGIN-54 guard) |
| **NO edit to `validResourceTypes`** | `internal/command/types.go:127` | Plugin-declared `resource_types: [channel]` auto-merges via `CollectResourceTypes` (`internal/plugin/manager.go:810-818`). `scene` is hardcoded there for legacy reasons only — do NOT follow that precedent (see §Landmines) |
| **NO edit to `internal/access/prefix.go`** (likely) | `internal/access/prefix.go:22-34` | The plugin `ResolveResource` proxy keys off `namespace+":"` prefix itself (`internal/plugin/attribute_proxy.go:51`); `scene:` in `knownPrefixes` is used only where host code string-builds scene refs. Verify whether channels command code needs an `access.ChannelResource()` helper; if the plugin builds its own `channel:<id>` strings, no core edit is required |
| `task plugin:build-all` | (automatic) | `scripts/build-plugins.sh` auto-discovers `plugins/**` (`Taskfile.yaml:304-334`) — no manifest list to edit |

## Substrate Wiring

### Manifest fields (with least-privilege capability params)

Model on `plugins/core-scenes/plugin.yaml`. Channels needs FEWER capabilities than scenes:

```yaml
name: core-channels
type: binary
resource_types: [channel]
requires:
  - capability: eval        # HostEvaluator for admin-gated commands (create, moderate)
    access: read
  - capability: stream.history  # if history reads route through host StreamHistoryService
    access: read
  # NOT needed: focus (no focus_redirects), settings, audit-decrypt (plaintext), world (location-independent)
provides:
  - holomush.channel.v1.ChannelService     # NEW proto — see below
  - holomush.plugin.v1.PluginAuditService
emits: [channel]
history_scope: custom          # R4-A: 'channel' is NOT valid — enum is {grid,scene,custom}; 'custom' = plugin-owned visibility via QueryHistory
session_streams: true          # ← drives QuerySessionStreams live delivery
actor_kinds_claimable: [plugin, character]
storage: postgres
config:
  retention_window:  { type: duration, default: 720h }   # 30 days (D-07)
  replay_count:      { type: int,      default: 20 }      # replay-on-join (D-07)
  prune_interval:    { type: duration, default: 1h }
  create_rate_limit: { type: int,      default: 5 }       # per player per hour (D-06)
  scrollback_cap:    { type: int,      default: 500 }     # (D-07)
audit:
  - subjects: ["events.*.channel.>"]
    schema: plugin_core_channels
    table: channel_log
actions: [join, leave, invite, mute, ban, kick, transfer, archive]  # custom actions; merged via CollectActions
verbs:
  - { type: core-channels:channel_say,  category: communication, format: speech, label: says, display_target: terminal }
  - { type: core-channels:channel_pose, category: communication, format: action, display_target: terminal }
  - { type: core-channels:channel_join, category: system, format: notification, display_target: terminal }
  # ... leave/mute/ban/kick/rename notices
commands:
  - name: channel
    capabilities: [{ action: write, resource: channel, scope: local }]
  # optionally a `channels`-style list/browse command
policies:
  # Layer-1 command gate + Layer-2 per-action gates (see below)
```

- **`emits`/`crypto.emits`:** declare `emits: [channel]` but **NO `crypto.emits` block** (D-04, plaintext). Because there is no `crypto.emits`, INV-PLUGIN-32 set-equality does NOT apply and the `EmitRegistry` is not validated against a manifest set (`plugins/core-scenes/main.go:169-204` shows the scenes registry exists only because scenes has `crypto.emits`). Confirm whether an `EmitRegistry` is still required for bare emit-type registration; scenes registers via `reg.RegisterEmitTypes(...)` in `main()` (`main.go:274-278`).
- **`config` decode:** `pluginsdk.DecodeConfig[channelConfig](config)` at `Init`, mapstructure tags on snake_case keys (`plugins/core-scenes/main.go:46-75`; `pkg/plugin/config.go:19`). Do plugin-owned semantic validation (positive `prune_interval`) fail-loud at Init (`main.go:64-68`).
- **`actions:`** — custom action strings (`join`, `mute`, etc.) MUST be declared in the manifest `actions:` block so `CollectActions` (`internal/plugin/manager.go:820-832`) admits them to `knownActions` for command-capability validation. Scenes declares only `browse`/`extend_publish_attempts` because its other action strings appear ONLY in policy DSL (never in a command `capabilities:` block) — policy DSL action strings are NOT validated against `knownActions`. Decide per-action: only actions used in a command's `capabilities:` need declaring.

### EventBus emit + audit

- **Content emit:** build `CommunicationContent` JSON via `comm.Say(author, text)` / `comm.Pose(author, invokedAs, raw)` / `comm.OOC` (`pkg/plugin/comm/builder.go:40-66`; `comm.Author{ID, Name}` at `pkg/plugin/comm/grammar.go:14`), then `p.service.eventSink.Emit(ctx, pluginsdk.EmitIntent{Subject: dotStyleChannelSubject(gameID, chID), Type: "core-channels:channel_say", Payload: json, Sensitive: false})` (mirror `plugins/core-scenes/publish_events.go:57-62` and `commands.go:1320-1364`). Channel identity is the **subject + live name lookup by ID**, never a payload `channel_name` field (D-08).
- **Notice events** (join/leave/mute/ban/kick/rename) use small bespoke proto payloads like scene notices (`plugins/core-scenes/publish_events.go` pattern), `Sensitive: false`.
- **Audit table** `plugin_core_channels.channel_log` mirrors `scene_log` columns exactly (`plugins/core-scenes/migrations/000004_create_scene_log.up.sql:16-33`): `id BYTEA PK, subject, type, timestamp, actor_kind, actor_id, payload, schema_ver, codec, js_seq, inserted_at`. `AuditEvent` is an idempotent `INSERT ... ON CONFLICT (id) DO NOTHING` (`plugins/core-scenes/audit.go:284-318`). Channels needs NO `dek_ref`/`dek_version` columns (plaintext) — omit migration `000005`'s DEK columns.
- **`QueryHistory`** enforces membership at the plugin boundary as **auth step-1 before any DB work** (`plugins/core-scenes/audit.go:493-555`) — mirror this exactly for channel history reads. Also enforce D-07 boundaries: history read never crosses the member's most-recent `joined_at`; scrollback `count` capped at 500 service-side.

### ABAC (resource type + membership policy + resolver)

- **Resource type auto-registration:** declaring `resource_types: [channel]` makes the host (a) merge `channel` into `knownResourceTypes` (`internal/plugin/manager.go:810-818`), (b) call the plugin's `AttributeResolverService.GetSchema`, validate it covers `channel` (`manager.go:1413-1423`), and (c) register a `NewPluginAttributeProvider("channel", arClient, schema)` core provider that proxies `ResolveResource` for any `channel:<id>` resource ref (`internal/plugin/attribute_proxy.go:27-67`). This is the whole mechanism — **no manual core edits.**
- **`ChannelResolver.ResolveResource`** (mirror `plugins/core-scenes/resolver.go:74-128`): return `name`, `type` (public/private/admin), `owner`, `archived` (bool), and the membership lists `members`, `banned`, `muted` as `STRING_LIST`. Follow the omit-don't-sentinel rule for optional attrs (`.claude/rules/abac-providers.md`; `resolver.go:120-126`).
- **Seed policies** (manifest `policies:`), mirror scenes' Layer-1 + Layer-2 shape (`plugins/core-scenes/plugin.yaml:241-387`):
  - Layer-1 command gate: `permit(principal is character, action in ["execute"], resource is command) when { resource.command.name in ["channel"] && ... }`.
  - Layer-2 read/post: `permit(principal is character, action in ["read"|"emit"], resource is channel) when { principal.id in resource.channel.members }`.
  - Private/admin gating: `... when { resource.channel.type == "public" }` OR `{ principal.id in resource.channel.members }` — CHAN-04 faction distinction is the public-vs-(private|admin)-membership split (D-02).
  - Owner moderation: `permit(... action in ["mute"|"ban"|"kick"], resource is channel) when { resource.channel.owner == principal.id }` (D-05, owner+admin only, no `op`).
  - Admin override + admin-only create: `permit(... ) when { "admin" in principal.character.roles }` (mirror `admin-extend-publish-attempts` at `plugin.yaml:365-368`); `seed:channel-admin-create` gates creation, shaped so an operator can grant `create:channel` to a role with no code change (D-06).
  - **Faction seam (D-03):** shape private/admin read policies keyed on `resource.name`/`type` so a future `principal.faction == "..."` clause slots in with no schema migration and no policy rewrite. (Note: `principal.faction` would be a CHARACTER attribute added to `CharacterProvider` — `internal/access/policy/attribute/character.go:124-130` shows where character attrs are assembled — NOT a channel-plugin concern.)
- **Error uniformity:** operations on a channel a player cannot see MUST return an identical "channel not found" — never distinguish "doesn't exist" from "hidden" (spec §Error Response Uniformity; CONTEXT specifics). Scenes' resolver returns `codes.NotFound` uniformly (`resolver.go:94-96`).

### Postgres schema + migrations

Mirror `plugins/core-scenes/migrations/000003_...up.sql:11-47`. Paired `.up.sql` + `.down.sql`, `IF NOT EXISTS`, no triggers/functions, sequential 6-digit prefix (`.claude/rules/database-migrations.md`). Note scenes' `000001`/`000002` baselines lack `.down` files (predate the rule) — **channels MUST ship paired up/down from `000001`.**

```sql
-- 000001_channels.up.sql (sketch)
CREATE TABLE IF NOT EXISTS channels (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,                 -- unique case-insensitive: add UNIQUE lower(name) index
    type         TEXT NOT NULL CHECK (type IN ('public','private','admin')),
    owner_id     TEXT NOT NULL,
    archived     BOOLEAN NOT NULL DEFAULT false,
    retention_days INTEGER,                      -- NULL = default/unlimited (admin per D-07)
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_channels_name_ci ON channels (lower(name));

CREATE TABLE IF NOT EXISTS channel_memberships (
    channel_id   TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    character_id TEXT NOT NULL,                  -- membership is player-level in effect (D-02) but keyed per-character here
    role         TEXT NOT NULL CHECK (role IN ('owner','member')),  -- 'op' dormant per D-05: keep or drop (discretion)
    muted        BOOLEAN NOT NULL DEFAULT false,
    banned       BOOLEAN NOT NULL DEFAULT false,
    joined_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (channel_id, character_id)
);
-- + channel_ops_events (append-only journal, mirror scene_ops_events:32-40)
```

Name regex `^[a-zA-Z0-9][a-zA-Z0-9_-]{0,31}$`, unique case-insensitive; `delete` = archive (soft), not hard delete (CONTEXT specifics).

### Telnet command registration + capabilities

- `commands:` manifest block registers `channel` with `capabilities: [{action: write, resource: channel, scope: local}]` (mirror `plugin.yaml:217-239`). `HandleCommand` in `main.go` routes `channel` → `dispatchCommand` (`main.go:87-96`); subcommand router in `commands.go`.
- **`=name message` shorthand** and `=name :pose`/`;semipose`: reuse the `:`/`;`/`no_space` grammar from `comm.ParsePose`/`ParseOOC` (`pkg/plugin/comm/grammar.go`, `builder.go:48-58`). Parser wiring is Claude's discretion (CONTEXT). The `=` shorthand is a top-level verb the host dispatcher must route to the `channel` command — verify whether this needs a `focus_redirects`-style host mechanism or a command alias; **channels does NOT use `focus_redirects`** (that's scene-focus-specific, `plugin.yaml:212-215`), so the `=` prefix routing is an open question (see §Landmines).

### Live delivery (the CHAN-01/CHAN-02 delivery path)

1. **At session establishment:** host calls `QuerySessionStreams` on every `session_streams: true` plugin (`internal/plugin/manager.go:1512-1560`; `host.go:57-60`; proto `plugin.proto:48`). `core-channels` returns `events.<game>.channel.<id>` for every non-banned, non-gagged channel the session's character is a member of (plus seeded channels for guests). `SessionStreamsRequest` carries `CharacterID`/`PlayerID`/`SessionID` (`internal/plugin/host.go:23-30`).
2. **Mid-session join/leave:** host `StreamRegistry.AddStreamWithMode(sessionID, stream, session.ReplayModeLiveOnly)` / `RemoveStream` (`internal/plugin/host.go:32-41`; `internal/session/replay_mode.go:28-30`). `ReplayModeLiveOnly` = advance cursor to tail (no history flood); `ReplayModeBoundedTail` (`replay_mode.go:23-26`) = the replay-on-join scrollback (D-07 default 20).

## Per-Requirement Mapping (CHAN-01..05)

| Req | Approach | Validation tier |
|-----|----------|-----------------|
| **CHAN-01** join/leave/list persistent named channels, location-independent | `channels`/`channel_memberships` tables (no `location` FK); `channel join/leave/list` subcommands mutate membership + `AddStream`/`RemoveStream`; list = membership query | Unit (store/service), Ginkgo integration (join→delivery), whole-system census |
| **CHAN-02** post/read gated by ABAC membership | `channel say`→`EventSink.Emit`; read via `PluginAuditService.QueryHistory` with membership auth step-1 (`audit.go:493-555`); Layer-2 policies `principal.id in resource.channel.members` | Unit (policy eval), Ginkgo (member posts+reads / non-member denied), negative-path denial |
| **CHAN-03** events flow through EventBus with scene-identical JetStream/audit | `emits: [channel]`; dot subjects `events.<game>.channel.<id>`; `audit:` block → host consumer ack-and-skip → `PluginAuditService.AuditEvent`; `QueryHistory` JS→Postgres fallback | Ginkgo integration (emit→audit round-trip), audit idempotency (redelivery) |
| **CHAN-04** faction-restricted channels enforce membership distinct from open | `type public/private/admin`; private/admin read+post gated on membership, public open; faction seam via `resource.name`-keyed policies (D-02/D-03) | Unit (policy eval per type), Ginkgo (private non-member rejection, admin override) |
| **CHAN-05** second substrate consumer, validate INV-S7 (N=2) | Build channels to make shared substrate patterns visible (store/audit/resolver/emit shapes parallel to scenes). Extraction is FOLLOW-ON — do NOT plan it | Structural review: channels + scenes both consume identical substrate seams; register INV-S7 binding if appropriate |

## Validation Architecture

Nyquist validation ENABLED. Test tiers per `.claude/rules/testing.md`: unit table-driven (`task test`), Ginkgo integration (`//go:build integration`, `task test:int`, needs Docker), whole-system census. Framework: Go stdlib `testing` + `testify` (unit) + Ginkgo/Gomega (integration). Commands: `task test -- ./plugins/core-channels/`; `task test:int`.

### Requirement → Test Map

| Behavior | Tier | Automated command | Edge/negative cases |
|----------|------|-------------------|---------------------|
| join/leave/list membership | unit (store) + Ginkgo | `task test -- ./plugins/core-channels/` | double-join idempotency; leave-not-member; list-empty; banned cannot rejoin |
| ABAC read/post default-deny | unit (policy eval) + Ginkgo | `task test:int` | **non-member post DENIED; non-member read DENIED; private non-member = "channel not found" (error uniformity)** |
| faction/private/admin gating | unit + Ginkgo | `task test:int` | private non-member rejected; admin override permits; public open to all; `has_X` witness present |
| moderation (mute/ban/kick/transfer) | unit + Ginkgo | `task test:int` | non-owner mute DENIED; owner mutes member; admin overrides owner; muted member post suppressed; kicked member stream removed |
| emit→audit round-trip (CHAN-03) | Ginkgo integration | `task test:int` | audit redelivery idempotent (ON CONFLICT); JS→Postgres history fallback; subject parse rejects wildcards (`audit.go:629-651` analog) |
| retention prune (D-07) | unit (sweep, injected clock) + integration | `task test -- ./plugins/core-channels/` | prune deletes only rows older than window; admin-channel unlimited retention preserved; per-channel override; empty-batch no-op |
| rate-limit create (D-06) | unit (service) | `task test -- ./plugins/core-channels/` | 5/hr boundary (5th allowed, 6th denied); window rollover; admin bypass |
| replay-on-join + scrollback (D-07) | Ginkgo integration | `task test:int` | replay count = 20 default; scrollback cap = 500; history never crosses `joined_at` |
| live delivery | Ginkgo integration | `task test:int` | member receives `channel_say`; non-member does NOT; mid-session join subscribes via LIVE_ONLY (no history flood); leave unsubscribes |
| plugin loads (CHAN-05) | whole-system census | `task test:int` | `core-channels` in `expectedPlugins`; capability declarations satisfy INV-PLUGIN-54 |

### Sampling

- **Per task commit:** `task test -- ./plugins/core-channels/` (+ `task lint`)
- **Per wave merge:** `task test:int` (Docker; exercises audit/migrations/delivery)
- **Phase gate:** `task pr-prep` green before `/gsd-verify-work`

### Wave 0 Gaps

- [ ] `plugins/core-channels/core_channels_suite_test.go` — Ginkgo bootstrap
- [ ] `plugins/core-channels/*_test.go` unit files (store/service/resolver/audit/commands/prune)
- [ ] `test/integration/wholesystem/census_test.go` — add `core-channels` to `expectedPlugins:25-27`
- [ ] New proto `api/proto/holomush/channel/v1/channel.proto` (+ `task proto && task web:generate` regen, commit generated `*.pb.go`) — mirror `holomush.scene.v1.SceneService`
- [ ] `QuerySessionStreams` SDK handler + `StreamRegistry` host-capability plumbing IF absent (see §Landmines)

## Security Domain (ABAC default-deny is the core control)

`security_enforcement` enabled. No crypto (plaintext, D-04) — crypto-reviewer gate does NOT fire.

| ASVS-ish category | Applies | Standard control (in-tree) |
|-------------------|---------|----------------------------|
| Access control (default-deny ABAC) | **yes** | Two-layer engine gate + manifest `policies`; resource-side membership resolution; fail-closed on infra error (ABAC-02) |
| Input validation | yes | Name regex `^[a-zA-Z0-9][a-zA-Z0-9_-]{0,31}$`; `comm.build` sanitizes untrusted UTF-8 (`builder.go:30-38`); message-size cap via config |
| Error handling (no info leak) | yes | Uniform "channel not found"; generic gRPC messages, log internally (`.claude/rules/grpc-errors.md`; `resolver.go:97-101`) |
| Trust boundary | yes | Plugin MUST NOT trust client-supplied identity; `QueryHistory` uses host-forwarded `req.Caller` (`audit.go:479-524`) |

| Threat | STRIDE | Mitigation |
|--------|--------|------------|
| Non-member reads private history | Information disclosure | Membership auth step-1 in `QueryHistory` before DB work (`audit.go:498-555`) |
| Non-member posts to channel | Elevation | Layer-2 `emit` policy `principal.id in resource.channel.members` |
| Hidden-channel existence oracle | Information disclosure | Error uniformity — identical "not found" for absent vs hidden |
| Forged actor kind on emit | Spoofing | `actor_kinds_claimable` gate at `event_emitter.go::Emit` (both runtimes) |
| Unbounded history growth | DoS | 30-day retention + prune sweep (D-07) |

## Landmines & Open Questions

1. **[BLOCKING for planner] Membership is resource-side, not principal-side — contradicts D-03 as literally worded.** The plugin `AttributeResolverService` proto has ONLY `ResolveResource`+`GetSchema` (`api/proto/holomush/plugin/v1/attribute.proto:44,63`); `PluginAttributeProvider.ResolveSubject` returns nil (`internal/plugin/attribute_proxy.go:44-47`). A plugin CANNOT contribute `principal.channel_memberships`. A core in-process provider CANNOT read the plugin's isolated `plugin_core_channels` schema. **Recommendation:** model membership as `resource.channel.members` (resolved by `ChannelResolver`, exactly like `resource.scene.participants`) and shape policies `principal.id in resource.channel.members`. This satisfies CHAN-02/CHAN-04 and the D-03 seam intent (faction slots in as a `principal.faction` CHARACTER attr later) with ZERO new proto. The planner must either adopt this or scope a new principal-resolution RPC (large, cross-cutting). The `ChannelAttributeProvider` in D-03 should be read as "the plugin `ChannelResolver`," not a core `attribute.Provider`.

2. **[BLOCKING for planner] Session-stream subscription: core-channels is the FIRST consumer.** `session_streams`/`QuerySessionStreams` exist host-side (`manager.go:1508`, `host.go:57`, `plugin.proto:48`) and `StreamRegistry.AddStreamWithMode(...LIVE_ONLY)` names channels in its doc (`host.go:32-41`), but **no plugin implements them** and the plugin-SDK-facing wiring (a `QuerySessionStreams` handler interface + a `StreamRegistry` host capability the binary plugin can call on join/leave) appears ABSENT from `pkg/plugin/` (grep: zero hits). The planner must verify and likely scope: (a) the SDK-side `QuerySessionStreams` handler, (b) a host capability exposing `AddStream`/`RemoveStream` to binary plugins mid-session. This is genuine substrate plumbing, not just plugin code. [ASSUMED absent — verify against `pkg/plugin/` adapter and `internal/plugin/goplugin/`.]

3. **`=name message` top-level shorthand routing.** Channels does NOT use `focus_redirects` (scene-specific). How the `=` prefix reaches the `channel` command from a top-level input line is unresolved — likely a host command-parser prefix rule or a command alias. Verify the dispatcher's handling of symbol-prefixed input; may need a small host change. [Open]

4. **Bootstrap seeding + guest auto-join.** No in-tree seeding hook for default channels or guest auto-join was found (grep: no `seedChannels`/`autoJoin`). Likely the plugin seeds default channels into its own schema at `Init`, and guest auto-join happens via `QuerySessionStreams` returning seeded-channel streams for guest sessions. Confirm the session-establishment hook actually fires for guest sessions. [Open]

5. **New `ChannelService` proto required.** Mirror `holomush.scene.v1.SceneService`. Every proto element needs a Go-grounded doc comment (`.claude/rules/proto-doc-comments.md`); run `task proto && task web:generate`, commit generated code same change, `task lint:proto` green.

6. **Dormant `op` role (D-05, discretion).** Keeping `'op'` in the `role` CHECK constraint dormant avoids a future migration (lighter path); dropping it is cleaner now. Planner/Claude decides.

7. **`EmitRegistry` without `crypto.emits`.** Scenes' `main()` registers emit types in an `EmitRegistry` (`main.go:274-278`) but that exists to satisfy INV-PLUGIN-32 set-equality against `crypto.emits`. Since channels has no `crypto.emits`, confirm whether the SDK still requires an `EmitRegistry` for bare emit-type registration or whether it can be omitted. [Verify against `pkg/plugin` emit-registry requirement.]

8. **Invariants (Claude's discretion per CONTEXT).** If channels mints a named guarantee (e.g., channel-membership-enforcement, error-uniformity), register `INV-CHANNEL-N` (`binding: pending`) and regenerate `docs/architecture/invariants.md` in the SAME change or `TestEveryRegistryInvariantHasBinding` fails (`.claude/rules/invariants.md`). INV-S7 (N=2) is validated by this phase.

## Package Legitimacy Audit

**N/A** — this phase installs NO external packages. All dependencies are in-tree (`pkg/plugin`, `pkg/plugin/comm`, `pkg/proto/...`, `internal/access`, `internal/plugin`, `internal/session`) or already-vendored (`github.com/jackc/pgx/v5`, `github.com/oklog/ulid/v2`, `github.com/samber/oops`, `google.golang.org/grpc`, `google.golang.org/protobuf` — all used by `core-scenes`).

## Environment Availability

| Dependency | Required by | Available | Notes |
|------------|-------------|-----------|-------|
| Docker (testcontainers) | store/audit/integration tests | ✓ (assumed dev env) | `task test:int` needs it |
| `task` (go-task) | all build/test/lint | ✓ | never run `go`/`golangci-lint` directly |
| `buf`/`task proto` | new `channel.proto` regen | ✓ | commit generated `*.pb.go` same change |
| PostgreSQL | plugin storage (via testcontainers) | ✓ | plugin gets `search_path=plugin_core_channels` conn string at Init |

## Sources

### Primary (HIGH confidence — in-tree code, this session)

- `plugins/core-scenes/{plugin.yaml,main.go,resolver.go,audit.go,ops_events.go,publish_events.go,publish_scheduler.go,lifecycle.go,commands.go}` — reference implementation
- `plugins/core-scenes/migrations/000003,000004` — plugin schema + audit table
- `internal/plugin/{manager.go,attribute_proxy.go,host.go}` — resource-type registration, session-stream delivery, StreamRegistry
- `internal/access/policy/attribute/{scene.go,stream.go,character.go,plugin_provider.go}` — core provider pattern + omit-don't-sentinel
- `internal/access/prefix.go`, `internal/command/types.go` — ABAC prefixes / known types
- `api/proto/holomush/plugin/v1/{attribute.proto,audit.proto,plugin.proto}` — plugin RPC surface (no subject resolution)
- `pkg/plugin/comm/{builder.go,grammar.go}`, `api/proto/holomush/comm/v1/comm.proto` — CommunicationContent payload
- `internal/session/replay_mode.go` — LIVE_ONLY / BoundedTail
- `test/integration/wholesystem/census_test.go`, `Taskfile.yaml` — census + build wiring
- `.planning/phases/01-channels-subsystem/01-CONTEXT.md`, `.planning/REQUIREMENTS.md` — locked decisions
- `.claude/rules/{plugin-manifest,event-conventions,event-interfaces,abac-providers,database-migrations,grpc-errors,testing,plugin-runtime-symmetry}.md`

### Tertiary (LOW — reconcile, do not trust architecture)

- `docs/specs/2026-04-03-channels-architecture.md` — product intent only (stale architecture)

## Metadata

**Confidence breakdown:**

- File/package layout: HIGH — direct 1:1 mapping to shipped `core-scenes`
- Substrate wiring (emit/audit/resolver/ABAC): HIGH — verified against host code + reference plugin
- Live-delivery mechanism: MEDIUM — mechanism identified and grounded, but core-channels is first consumer; SDK-facing plumbing may be net-new (Landmine 2)
- Membership modeling: HIGH — proto constraint proves resource-side is required (Landmine 1)

**Research date:** 2026-07-08
**Valid until:** ~2026-08-07 (stable substrate; re-verify if `pkg/plugin` session-stream SDK lands separately)
