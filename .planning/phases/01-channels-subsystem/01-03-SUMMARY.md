---
phase: 01-channels-subsystem
plan: 03
subsystem: database
tags: [plugin, postgres, pgx, ginkgo, channels, migrations, abac, seeding]

# Dependency graph
requires:
  - phase: 01-channels-subsystem
    provides: "core-scenes plugin as the structural template (manifest, store, migrations, audit table, main.go)"
provides:
  - "core-channels binary plugin skeleton (loadable, serves via ServeWithServices)"
  - "plugin-owned Postgres schema: channels + channel_memberships + channel_ops_events + channel_log (paired reversible migrations, no location FK, plaintext audit)"
  - "channelStore: CreateChannel, GetByID, GetByName (case-insensitive), JoinChannel, LeaveChannel, ListForCharacter, GetWithMembership, SetMuted, SetBanned, DeleteChannel (soft archive)"
  - "SeedDefaultChannels (idempotent) + ListDefaultChannels (the guest-auto-join seam 01-08 unions)"
  - "domain type/state model (channelType/channelRole/channelState + name regex + transition validation)"
  - "Ginkgo integration suite bootstrap"
affects: [01-04 resolver, 01-05 service/commands, 01-06 audit, 01-07 prune, 01-08 guest-auto-join, 01-09 census]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Plugin-owned schema via storage.RunMigrationsFS on embedded migrations/*.up.sql"
    - "Case-insensitive channel-name uniqueness via UNIQUE INDEX on lower(name) + ON CONFLICT (lower(name)) DO NOTHING for idempotent seeding"
    - "Soft-archive (archived=true) instead of hard delete"
    - "Append-only channel_ops_events journal for every membership/moderation/lifecycle mutation"
    - "Testcontainer integration tests via testutil.RawDatabase (each store test composes its own database)"

key-files:
  created:
    - "plugins/core-channels/plugin.yaml"
    - "plugins/core-channels/main.go"
    - "plugins/core-channels/types.go"
    - "plugins/core-channels/store.go"
    - "plugins/core-channels/migrations/000001_channels.{up,down}.sql"
    - "plugins/core-channels/migrations/000002_create_channel_log.{up,down}.sql"
    - "plugins/core-channels/core_channels_suite_test.go"
    - "plugins/core-channels/types_test.go"
    - "plugins/core-channels/store_test.go"
  modified: []

key-decisions:
  - "history_scope: custom (NOT channel) — closed enum {grid,scene,custom}; custom = plugin-owned QueryHistory visibility (R4-A)"
  - "resource_types: [channel] DEFERRED to 01-04 (declaring it forces host schema discovery against a resolver this plan does not serve — would fail LoadAll)"
  - "op role kept as a DORMANT value in the channel_memberships CHECK (D-05 lightest path); channelRole.IsValid() rejects it this phase"
  - "channel_log mirrors scene_log MINUS dek_ref/dek_version — channel events are plaintext (D-04)"
  - "Default channels: is_default=true, system-sentinel owner, NO membership row (D-01)"

patterns-established:
  - "Idempotent Init-time seeding: SeedDefaultChannels re-runnable on every Init (ON CONFLICT DO NOTHING)"
  - "channelStore is the single membership source of truth the resolver (01-04) and audit (01-06) authorize against"

requirements-completed: [CHAN-01, CHAN-03]

coverage:
  - id: D1
    description: "channels/channel_memberships/channel_ops_events/channel_log tables via paired reversible migrations (no location FK)"
    requirement: CHAN-01
    verification:
      - kind: integration
        ref: "plugins/core-channels/store_test.go#channelStore CRUD + membership (NewChannelStore runs migrations)"
        status: pass
    human_judgment: false
  - id: D2
    description: "store: create with case-insensitive unique name, join/leave, list-for-character, GetByID/GetByName, GetWithMembership, SetMuted/SetBanned"
    requirement: CHAN-03
    verification:
      - kind: integration
        ref: "plugins/core-channels/store_test.go#channelStore CRUD + membership"
        status: pass
    human_judgment: false
  - id: D3
    description: "name regex ^[a-zA-Z0-9][a-zA-Z0-9_-]{0,31}$ + type/role/state transition validation"
    verification:
      - kind: unit
        ref: "plugins/core-channels/types_test.go#TestValidateChannelName/TestIsValidChannelTypeTransition/TestChannelRoleIsValid"
        status: pass
    human_judgment: false
  - id: D4
    description: "DeleteChannel is soft archive (archived=true, row retained) — never a hard delete"
    verification:
      - kind: integration
        ref: "plugins/core-channels/store_test.go#DeleteChannel (soft archive) sets archived=true and leaves the row present"
        status: pass
    human_judgment: false
  - id: D5
    description: "idempotent default-channel seeding at Init (Public seeded, second seed adds no duplicate, no membership rows) + ListDefaultChannels seam"
    requirement: CHAN-01
    verification:
      - kind: integration
        ref: "plugins/core-channels/store_test.go#default-channel seeding"
        status: pass
    human_judgment: false
  - id: D6
    description: "DRAFT manifest validates and core-channels loads via Manager.LoadAll in the whole-system stack"
    verification:
      - kind: integration
        ref: "test/integration/wholesystem/census_test.go (BeforeAll Start(WithInTreePlugins) — LoadAll succeeds with core-channels present)"
        status: pass
    human_judgment: false

# Metrics
duration: 40min
completed: 2026-07-08
status: complete
---

# Phase 1 Plan 03: Channels Data Foundation Summary

**core-channels binary plugin skeleton with plugin-owned Postgres schema (channels/memberships/ops/plaintext log, no location FK), a channelStore (CRUD + idempotent membership + case-insensitive name lookup + soft archive), and idempotent default-channel seeding at Init exposing ListDefaultChannels for guest auto-join.**

## Performance

- **Duration:** ~40 min
- **Completed:** 2026-07-08
- **Tasks:** 3
- **Files created:** 11

## Accomplishments

- Loadable `core-channels` plugin skeleton — validated end-to-end by the whole-system census (`Manager.LoadAll` succeeds with core-channels present).
- Plugin-owned schema with paired, reversible migrations: `channels` (case-insensitive unique name, `is_default` flag, `archived` soft-delete, per-channel `retention_days`), `channel_memberships` (per-character, `op` role dormant in CHECK), `channel_ops_events` (append-only journal), and plaintext `channel_log` (scene_log minus DEK columns, D-04). No location FK — channels are location-independent (CHAN-01).
- `channelStore` covering create (tx: channel + owner membership + ops event), case-insensitive name lookup, idempotent join / ban-blocks-rejoin, leave (owner protected), list-for-character, GetWithMembership (members/banned/muted arrays for the resolver), mute/ban, and soft-archive delete.
- Idempotent `SeedDefaultChannels` (ON CONFLICT DO NOTHING on lower(name), no membership rows) + `ListDefaultChannels` — the seam 01-08 unions into `QuerySessionStreams` for guest auto-join (D-01).

## Task Commits

1. **Task 1: skeleton + manifest + migrations + types + Ginkgo bootstrap** — `578b79bac` (feat)
2. **Task 2: channelStore CRUD + membership + case-insensitive name lookup** — `90a846799` (feat, TDD test+impl)
3. **Task 3: idempotent default-channel seeding at Init + ListDefaultChannels** — `66d7e2a05` (feat, TDD test+impl)

*Note: Task 2 and Task 3 are `tdd="true"`; see TDD Gate Compliance below.*

## Files Created/Modified

- `plugins/core-channels/plugin.yaml` — DRAFT manifest (type binary, storage postgres, emits [channel], history_scope custom, audit block, config knobs)
- `plugins/core-channels/main.go` — plugin entry; Init decodes/validates config, opens store, seeds defaults
- `plugins/core-channels/types.go` — channelType/channelRole/channelState + name regex + transition validation
- `plugins/core-channels/store.go` — channelStore + SeedDefaultChannels/ListDefaultChannels + ops-event helper
- `plugins/core-channels/migrations/000001_channels.{up,down}.sql` — channels + memberships + ops_events
- `plugins/core-channels/migrations/000002_create_channel_log.{up,down}.sql` — plaintext audit table
- `plugins/core-channels/core_channels_suite_test.go` — Ginkgo bootstrap
- `plugins/core-channels/types_test.go` — table-driven unit tests
- `plugins/core-channels/store_test.go` — testcontainer integration tests

## Decisions Made

- **history_scope: custom** (per plan R4-A) — the closed enum is {grid,scene,custom}; `custom` is the spec-correct fit for channels' membership-gated, plugin-owned QueryHistory visibility.
- **`op` role kept dormant in the CHECK** (D-05 lightest path) — avoids a future migration to activate op/deop; `channelRole.IsValid()` treats only owner/member as usable.
- **Timestamps as `TIMESTAMPTZ`/`time.Time`** (not pgnanos BIGINT) — this is a fresh plugin schema with no BIGINT-ns migration history to match; simplest correct choice.
- **Channel/ops-event IDs via `idgen.New()`** (crypto/rand entropy) per entity-PK guidance.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Deferred `resource_types: [channel]` from the manifest to plan 01-04**

- **Found during:** Task 3 (whole-system census verification)
- **Issue:** The plan's manifest Artifacts list `resource_types: [channel]`. Declaring a resource type makes the host run schema discovery against the plugin's `AttributeResolverService` at load (`manager.go:1247 → discoverAndRegisterAttributes`). This foundation plan serves no resolver (that is plan 01-04), so the `GetSchema` RPC returns `Unimplemented`, the plugin fails `LoadAll`, and the existing `test/integration/wholesystem` census BeforeAll panics — a regression introduced purely by declaring the resource type early.
- **Fix:** Removed `resource_types: [channel]` from `plugin.yaml` with an explanatory comment; 01-04 adds it back together with `RegisterAttributeResolver` so the pair lands atomically and the plugin stays loadable at every step. `emits: [channel]` + `history_scope: custom` + the audit block remain, so the manifest still parses and loads.
- **Files modified:** plugins/core-channels/plugin.yaml
- **Verification:** `task test:int -- ./test/integration/wholesystem/` green (census BeforeAll `LoadAll` succeeds; core-channels initialises with `default_channels=1`).
- **Committed in:** `66d7e2a05` (Task 3 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking).
**Impact on plan:** Necessary to keep the plugin loadable and not regress the whole-system census. No scope change — resource_types is simply relocated to the plan that supplies its required resolver (01-04). All must_have truths still hold (manifest validates, core-channels loads, soft-archive prohibition asserted).

## TDD Gate Compliance

Tasks 2 and 3 are `tdd="true"`. Because `channelStore` and its `store_test.go` are compilation-interdependent and the store tests are testcontainer integration tests (no meaningful RED run without the migrated schema), test and implementation were authored and committed together per task rather than as separate `test(...)` → `feat(...)` commits. Each task's behavior list was written as executable Ginkgo specs and verified GREEN (`task test:int -- ./plugins/core-channels/`, 45 tests, exit 0) before commit. No `feat` shipped without its asserting tests.

## Issues Encountered

- **Markdown lint (`task lint`) reports 12 pre-existing MD041/MD075 issues in `.planning/` GSD artifacts** (PLAN.md/STATE.md/deferred-items.md — YAML-frontmatter files). None are in this plan's files. `lint:go` (0 issues) and `lint:proto` pass. Out of scope per the scope boundary; logged, not fixed.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Store + schema + seeding are the foundation for 01-04 (resolver/policies — which adds `resource_types: [channel]` + `RegisterAttributeResolver`), 01-05 (service/commands), 01-06 (audit `PluginAuditService`), 01-07 (prune), and 01-08 (guest auto-join via `ListDefaultChannels`).
- 01-09 census: add `core-channels` to `expectedPlugins` once the resolver lands (01-04) so the census explicitly asserts its presence.

## Self-Check: PASSED

- All created files verified present on disk (see Files Created).
- All three task commits verified in `git log` (578b79bac, 90a846799, 66d7e2a05).

---

*Phase: 01-channels-subsystem*
*Completed: 2026-07-08*
