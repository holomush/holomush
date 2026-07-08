---
phase: 01-channels-subsystem
plan: 04
subsystem: access-control
tags: [plugin, abac, resolver, policies, channels, membership, default-deny, tdd]

# Dependency graph
requires:
  - phase: 01-channels-subsystem
    provides: "channelStore.GetWithMembership (members/banned/muted arrays) + systemOwnerID sentinel (01-03)"
  - phase: 01-channels-subsystem
    provides: "core-scenes SceneResolver + plugin.yaml policies as the structural template"
provides:
  - "ChannelResolver (pluginv1.AttributeResolverServiceServer): GetSchema advertises resource type `channel`; ResolveResource resolves membership RESOURCE-side as resource.channel.members"
  - "resource_types: [channel] in the manifest — auto-registers the host ABAC proxy provider; lands atomically with RegisterAttributeResolver (resolves the 01-03 deferral)"
  - "Layer-2 default-deny channel ABAC policies in the plugin manifest (read/emit/write member-gated + public-read visibility + owner moderation/lifecycle + admin override/create)"
  - "channel actions (create/join/leave/invite/mute/ban/kick/transfer/archive) + verbs (channel_say/pose + 6 notice types) in the manifest"
  - "D-03 (Landmine 1) reconciliation: principal-side channel_memberships -> resource-side resource.channel.members"
affects: [01-05 service/commands, 01-06 audit QueryHistory, 01-07 command+Layer-1 execute gate, 01-09 census]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Plugin AttributeResolverService resolves membership RESOURCE-side (resource.channel.members), mirroring resource.scene.participants — the proto has no subject/principal RPC"
    - "omit-don't-sentinel optional attribute (owner) with an always-present has_owner BOOL witness (.claude/rules/abac-providers.md)"
    - "Channel ABAC ships in the plugin manifest policies: block, never host SeedPolicies()"
    - "resource_types + RegisterAttributeResolver land in one atomic change so schema discovery succeeds and the plugin stays loadable"

key-files:
  created:
    - "plugins/core-channels/resolver.go"
    - "plugins/core-channels/resolver_test.go"
    - "plugins/core-channels/observability.go"
  modified:
    - "plugins/core-channels/main.go"
    - "plugins/core-channels/plugin.yaml"

key-decisions:
  - "D-03 Landmine-1: membership modelled resource-side (resource.channel.members), NOT principal-side (principal.channel_memberships) — the plugin AttributeResolverService proto exposes only GetSchema + ResolveResource; PluginAttributeProvider.ResolveSubject returns nil, so a plugin cannot contribute a principal attribute"
  - "owner is the resolver's optional attribute: emitted with has_owner=true only for a real character owner; OMITTED with has_owner=false for a system-owned default channel (owner_id == systemOwnerID) or empty — owner moderation fail-closes on system channels (T-01-14)"
  - "Layer-1 execute-channel-commands gate DEFERRED to the command's plan (01-05/01-07): policy_validator.go:145 rejects a command-targeting policy for a command the plugin does not declare; the gate ships with the `channel` command declaration (scenes precedent)"
  - "resource_types:[channel] auto-registers the ABAC proxy via CollectResourceTypes — NO edit to internal/access/prefix.go or internal/command validResourceTypes"

requirements-completed: [CHAN-02, CHAN-04]

# Metrics
duration: 40min
completed: 2026-07-08
status: complete
---

# Phase 1 Plan 04: ChannelResolver + ABAC Seed Policies Summary

**The `ChannelResolver` resolves `resource.channel.*` attributes — membership RESOURCE-side as `resource.channel.members` (the D-03 Landmine-1 reconciliation) — and the plugin-manifest ABAC policies default-deny channel read/post/moderation/creation on that membership, with `resource_types: [channel]` + the resolver registration landing atomically so the whole-system census stays green.**

## Performance

- **Duration:** ~40 min
- **Completed:** 2026-07-08
- **Tasks:** 2 (Task 1 TDD RED->GREEN; Task 2 manifest policies/actions/verbs)

## D-03 Reconciliation (Landmine 1) — principal-side -> resource-side

CONTEXT decision **D-03** literally specifies a `ChannelAttributeProvider` resolving `principal.channel_memberships` (principal-side). That shape is **architecturally infeasible via a plugin** and is superseded here:

- The plugin `AttributeResolverService` proto exposes ONLY `GetSchema` + `ResolveResource` — there is no subject/principal RPC (`api/proto/holomush/plugin/v1/attribute.proto`). `PluginAttributeProvider.ResolveSubject` returns nil, so a plugin cannot contribute `principal.channel_memberships`.
- A core in-process provider cannot read the plugin's isolated `plugin_core_channels` schema either.

**Built shape:** membership is modelled **RESOURCE-side** as `resource.channel.members`, resolved by `ChannelResolver.ResolveResource` (mirroring `resource.scene.participants`). Policies read `principal.id in resource.channel.members`. The D-03 seam is preserved: a future per-character faction gate lands as a `principal.faction` CHARACTER attribute (in `internal/access/policy/attribute/character.go`, not a channel-plugin concern), and the membership clauses are keyed on `resource.channel.members`/`.type` so that clause slots in with no schema migration and no policy rewrite (commented inline on `write-channel-as-member`).

## Resolved `resource.channel.*` attribute set

| Attribute | Type | Presence |
|-----------|------|----------|
| `name` | STRING | always |
| `type` | STRING | always |
| `archived` | BOOL | always |
| `members` | STRING_LIST | always (may be empty) |
| `banned` | STRING_LIST | always (may be empty) |
| `muted` | STRING_LIST | always (may be empty) |
| `owner` | STRING | **optional** — omitted for system-owned (`owner_id == systemOwnerID`) or empty |
| `has_owner` | BOOL | always (witness for `owner`) |

`members` is the membership authority behind read/emit/write; empty lists fail-closed (`principal.id in <empty>` = false). `owner` uses the omit-don't-sentinel discipline: a present `""`/sentinel would fail-OPEN in the DSL evaluator, so the key is omitted and `has_owner=false` — owner moderation then fail-closes on system channels (only admin override applies, T-01-14).

## Accomplishments
- `ChannelResolver` implementing `pluginv1.AttributeResolverServiceServer`: `GetSchema` advertises the `channel` type; `ResolveResource` resolves via `channelStore.GetWithMembership`, rejects a foreign resource type with `InvalidArgument`, returns uniform `codes.NotFound` for a missing channel (hidden-channel existence-oracle mitigation, T-01-12), and translates other store errors to a generic `codes.Internal` with no inner-error leak (`.claude/rules/grpc-errors.md`).
- Wired the resolver into `main.go` via `RegisterAttributeResolver` (the `AttributeResolverProvider` interface) — the host auto-registers `holomush.plugin.v1.AttributeResolverService`; it is NOT in `provides`.
- Added `resource_types: [channel]` to the manifest atomically with the resolver registration, resolving the 01-03 deferral so schema discovery succeeds and the plugin stays loadable.
- Layer-2 default-deny channel ABAC in the plugin manifest: read (member OR public-visibility), emit (member), write (member, backs the 01-07 command capability — MED-5), owner moderation (mute/ban/kick) + owner lifecycle (transfer/archive), admin override, admin-default create (grant seam). Faction seam documented.
- Declared channel `actions` + plugin-qualified `verbs` (INV-PLUGIN-40).

## Task Commits

1. **Task 1 (RED):** failing ChannelResolver tests + skeleton + observability — `a99b5ebc0` (test)
2. **Task 1 (GREEN):** ChannelResolver impl + resource_types:[channel] + main.go wiring — `afdc6c24e` (feat)
3. **Task 2:** ABAC seed policies + actions + verbs — `7ca49cf93` (feat)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Deferred the Layer-1 `execute-channel-commands` gate to the command's plan (01-05/01-07)**
- **Found during:** Task 2 (whole-system census validation).
- **Issue:** The plan lists the Layer-1 command-execution gate (`permit(... action ["execute"], resource is command) when { resource.command.name in ["channel"] && principal.character.is_guest == false }`) as a this-plan artifact. The plugin policy validator (`internal/plugin/policy_validator.go:145 validateCommandPolicy`) rejects a command-targeting policy for a command the plugin does not declare (`policy references foreign command "channel" — plugins can only target their own commands`). The `channel` command (and its `{action: write, resource: channel}` capability) is added by 01-05/01-07 — this plan intentionally omits the `commands:` block — so installing the execute gate here fails `LoadAll` and panics the census `BeforeAll`.
- **Fix:** Replaced the Layer-1 policy with a documented placeholder comment. The execute gate (with its guest fail-closed exclusion) lands together with the `channel` command declaration in 01-05/01-07, exactly as scenes ships `execute-scene-commands` alongside its `commands:` block. Not a security gap: with no `channel` command declared, command dispatch has nothing to gate; the Layer-2 resource permits already fail-closed for read/emit/write/moderation.
- **Files modified:** plugins/core-channels/plugin.yaml
- **Committed in:** `7ca49cf93` (Task 2 commit)

**Total deviations:** 1 auto-fixed (1 blocking). No scope change — the Layer-1 gate is relocated to the plan that supplies its required `channel` command, mirroring the 01-03 -> 01-04 `resource_types` relocation. All must_have truths hold: resolver schema, resource-side membership, Layer-2 member-gated read/emit + public-read visibility, `write-channel-as-member` backing the command capability, owner moderation + admin override/create.

## Prohibitions Verified

- **A non-member MUST NOT satisfy read/emit** — `resource_test.go` asserts a member id is present and a non-member id absent in `resource.channel.members`; the read/emit/write permits key solely on membership (private/admin have no public clause). Public channels expose only VISIBILITY via `read-public-channel`; history CONTENT stays membership-gated by 01-06 QueryHistory (INV-CHANNEL-1). **Verified.**
- **The resolver MUST NOT emit an empty-string sentinel for an unresolved optional attribute** — `TestChannelResolverOwnerWitness` asserts `owner` is ABSENT (not `""`) with `has_owner=false` for system-owned/empty, present with `has_owner=true` for a character owner. **Verified.**

## TDD Gate Compliance
Plan `type: tdd`; Task 1 `tdd="true"`. The RED gate is commit `a99b5ebc0` (`test(...)`): a compiling skeleton + full-assertion `resolver_test.go` that failed 12 assertions (`task test -- ./plugins/core-channels/` exit 201). The GREEN gate is commit `afdc6c24e` (`feat(...)`): the real implementation, all 56 unit tests green. No `feat` shipped without its asserting tests. Task 2 is a manifest/config change (`type: execute`) with no separate RED.

## Verification
- `task test -- ./plugins/core-channels/` — 56 tests, exit 0.
- `task test:int -- ./plugins/core-channels/` — 57 tests (unit + resolver + store integration), exit 0.
- `task test:int -- ./test/integration/wholesystem/` — census loads core-channels with resource_types + resolver; schema discovery + manifest policy validation against the resolver schema green, exit 0.
- `task lint` — `lint:go` 0 issues, `lint:proto` green. `lint:markdown` reports 12 pre-existing MD041/MD075 issues in `.planning/` GSD frontmatter artifacts (none in this plan's files) — out of scope per the scope boundary (same as noted in 01-03).

## Issues Encountered
- Pre-existing markdown lint (MD041/MD075) on `.planning/` GSD artifacts persists; not in scope.
- `task fmt` reflowed the unrelated `.planning/phases/01-channels-subsystem/01-03-SUMMARY.md` (pre-existing formatting drift, not caused by this plan) — left unstaged/out of this plan's commits.

## User Setup Required
None.

## Next Phase Readiness
- 01-05/01-07 add the `channel` command (`commands:` block) — MUST add the Layer-1 `execute-channel-commands` gate in the same manifest change; `write-channel-as-member` (shipped here) already backs the command's `{action: write, resource: channel}` preflight.
- 01-06 QueryHistory authorizes channel history reads against the same membership store — membership CONTENT gate for ALL channel types (INV-CHANNEL-1).
- 01-09 census: add `core-channels` to `expectedPlugins` now that the resolver has landed.

## Self-Check: PASSED
- All created/modified files verified present on disk.
- All three task commits verified in `git log` (a99b5ebc0, afdc6c24e, 7ca49cf93).
- Census + channels integration suite both green after final state.

---
*Phase: 01-channels-subsystem*
*Completed: 2026-07-08*
