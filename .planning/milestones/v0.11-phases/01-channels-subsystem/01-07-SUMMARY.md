---
phase: 01-channels-subsystem
plan: 07
subsystem: channels-command-surface
tags: [plugin, command, alias, moderation, abac, retention, prune, tdd, chan-01, chan-02, chan-04, d-05, d-07, med-5, med-6, high-4, r2-c]

# Dependency graph
requires:
  - phase: 01-channels-subsystem
    provides: "channelService create/join/leave/list with per-RPC ABAC self-enforcement + withTrustedOwningPlayer seam (01-05)"
  - phase: 01-channels-subsystem
    provides: "complete ChannelService surface — post/who/history/invite/mute/ban/kick/transfer (01-05b)"
  - phase: 01-channels-subsystem
    provides: "Layer-2 read/emit/write member-gated ABAC policies + write-channel-as-member (01-04); channel_log audit store + HistoryForMember (01-06)"
provides:
  - "`channel` telnet command (HandleCommand → dispatchChannelCommand) routing create/join/leave/list/say/who/history/invite/mute/ban/kick/transfer to the ABAC-self-enforcing ChannelService — the command layer adds NO authz"
  - "`=name message` shorthand via a manifest `aliases: [\"=\"]` system-prefix alias (MED-6, NO host-parser change); `=name :pose` / `=name ;semipose` map to channel_pose with space / no-space"
  - "The now-un-deferred Layer-1 `execute-channel-commands` ABAC gate (guest fail-closed), landed alongside the `channel` command declaration (resolves 01-04's deferral)"
  - "Background retention prune sweep (channelPruner, D-07): per-channel retention window, config default, admin-unlimited; deletes channel_log rows older than the window on a ticker"
  - "channelStore.ListChannelsForPrune + DeleteChannelLogOlderThan; PostToChannel gains a 'semipose' kind (no-space pose)"
affects: [01-08 live moderation→leave removal, 01-09 whole-system e2e (=Public hello live alias-seeded hop)]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Command path = human/CLI conversational verbs (gateway-boundary): every structural/moderation subcommand delegates to a typed ChannelService RPC that self-enforces ABAC; content posts flow through PostToChannel (the single membership+not-muted fence). No command-layer authz reimplementation."
    - "`=` routing is a manifest command alias (aliases: [\"=\"]) seeded as a system prefix alias by the host alias-seeder — the host reassembles `=Public hello` → `channel Public hello`; the plugin router treats a non-reserved first token as a channel-name post. NO host command-parser / focus_redirects change (MED-6)."
    - "Name→id resolution is a plugin-internal store read (GetByName), NOT an authz decision — hidden and absent channels both present the uniform not-found (T-01-12) via a single channelCommandError/uniformChannelNotFound path."
    - "create stamps withTrustedOwningPlayer(ctx, req.PlayerID) before delegating so the 5/hr per-player limiter keys on the host-vouched dispatcher-stamped PlayerID (R2-C), never a client field."
    - "Retention prune mirrors publishScheduler: a ticker Run loop + injectable clock; effectiveRetention resolves per-channel override → config default → admin-unlimited. Per-channel delete errors are non-fatal; strict `<` preserves the window-edge row."

key-files:
  created:
    - "plugins/core-channels/commands.go"
    - "plugins/core-channels/commands_test.go"
    - "plugins/core-channels/prune.go"
    - "plugins/core-channels/prune_test.go"
    - "plugins/core-channels/prune_integration_test.go"
  modified:
    - "plugins/core-channels/main.go"
    - "plugins/core-channels/plugin.yaml"
    - "plugins/core-channels/service_rpcs.go"
    - "plugins/core-channels/store.go"

key-decisions:
  - "The Layer-1 `execute-channel-commands` gate (deferred from 01-04 because the policy validator rejects a command-targeting policy for an undeclared command) ships HERE with the `channel` command declaration, mirroring scenes' execute-scene-commands. Guest fail-closed (is_guest == false). Census confirms it loads."
  - "Content posting delegates to PostToChannel (NOT a direct emitter call): PostToChannel is the single security chokepoint enforcing the Layer-2 emit(membership) gate + not-muted check (T-01-02). To honor `;` no-space semipose through that chokepoint, PostToChannel gained a 'semipose' kind → emitPose(\";\") — a small extension to service_rpcs.go (Rule 2), keeping the fence single rather than emitting directly from the command layer."
  - "Retention model reconciles the schema comment (retention_days NULL = config default; admin channels MAY be unlimited) with D-07: a positive per-channel retention_days wins; NULL falls back to the config default window EXCEPT an admin channel with NULL retention is unlimited (never pruned). An admin channel with an explicit retention_days is still pruned at that window."
  - "The moderation/transfer target token is treated as a character id (no character-name resolver is wired in-plugin, consistent with the existing 'best-effort: no name resolver wired' notes); name→character resolution is a host follow-up."
  - "channelPruner is started as a daemon goroutine at Init tied to context.WithCancel(context.Background()) with cancel intentionally uncalled — there is no SDK Unload hook; process exit is the shutdown signal. Mirrors core-scenes' publishScheduler start exactly (the plan's 'stop on Unload' has no lifecycle hook to bind to)."

requirements-completed: [CHAN-01, CHAN-02, CHAN-04]

# Metrics
duration: 75min
completed: 2026-07-08
status: complete
---

# Phase 1 Plan 07: Channel Command Surface + Retention Prune Summary

**The `channel` telnet command (create/join/leave/list/say/who/history + owner+admin-only invite/mute/ban/kick/transfer) and the `=name message` / `=name :pose` / `=name ;semipose` shorthand drive the channel domain entirely by delegating to the ABAC-self-enforcing ChannelService (no command-layer authz), with `=` routed as a manifest system-prefix alias (MED-6, no host-parser change), the write capability backed by the 01-04 `write-channel-as-member` policy (MED-5), the now-un-deferred Layer-1 `execute-channel-commands` guest-fail-closed gate landed alongside the command (census green), and a ticker-driven retention prune sweep (D-07) that deletes channel_log rows older than each channel's window while preserving in-window rows and never pruning unlimited-retention admin channels.**

## Performance

- **Duration:** ~75 min
- **Completed:** 2026-07-08
- **Tasks:** 2 (Task 1 command router + shorthand + Layer-1 gate; Task 2 retention prune) — each a TDD RED→GREEN cycle

## Accomplishments

- **`channel` command router (commands.go):** `HandleCommand` → `dispatchChannelCommand` routing all twelve subcommands to the complete ChannelService (01-05 + 01-05b, HIGH-4). Structural (create/join/leave/list) and moderation (invite/mute/ban/kick/transfer) subcommands delegate to the typed RPCs that self-enforce ABAC per verb; the command layer never re-implements authz (gateway-boundary rule). A first token that is NOT a reserved subcommand is a channel-name post — so the `=`-alias-reassembled `channel Public hello` posts `hello` to `Public`.
- **`=name` shorthand (MED-6):** declared `aliases: ["="]` on the `channel` command; the host manifest alias-seeder persists it as a SYSTEM PREFIX alias → `channel`, reassembling `=Public hello` → `channel Public hello` exactly as core-communication seeds `:`/`;`→pose — **NO host command-parser / focus_redirects change**. `=name :text` → spaced pose, `=name ;text` → no-space semipose (comm sigil grammar).
- **Content posting through the single fence:** `channel say` and the `=name` shorthand build content via `PostToChannel`, which enforces the Layer-2 emit(membership) gate + not-muted check before emitting (T-01-02). PostToChannel gained a `semipose` kind (→ `emitPose(";")`) so no-space semipose flows through that chokepoint rather than bypassing it.
- **R2-C:** `channel create` stamps `withTrustedOwningPlayer(ctx, req.PlayerID)` before delegating `CreateChannel`, so the 5/hr per-player create limiter keys on the host-vouched dispatcher-stamped PlayerID; a command-path create with no trusted player id fails closed for a non-admin.
- **Layer-1 gate landed (MED-5 + 01-04 deferral):** the `channel` command declares `{action: write, resource: channel, scope: local}` (preflighted via `CanPerformAction`, satisfied by `write-channel-as-member`), and the previously-deferred `execute-channel-commands` Layer-1 policy (guest fail-closed) now ships alongside it. Whole-system census loads core-channels with both.
- **Uniform not-found (T-01-12):** name→id resolution (`GetByName`) and every per-channel RPC denial funnel through a single `uniformChannelNotFound()` — a hidden channel and an absent channel present an identical "No such channel."
- **Retention prune sweep (prune.go, D-07):** `channelPruner` mirrors `publishScheduler` — a `Run(ctx)` ticker loop (config `prune_interval`) that per channel deletes `channel_log` rows older than the effective window. `effectiveRetention`: per-channel `retention_days` override wins; NULL → config default (`retention_window`), except an admin channel with NULL retention is unlimited (skipped). Per-channel delete errors are non-fatal; the loop exits cleanly on ctx cancel. New store methods `ListChannelsForPrune` + `DeleteChannelLogOlderThan` (strict `<` preserves the window-edge row).
- **Wiring:** `main.go` gains a `channels channelNameResolver` field (wired to the store) and starts the pruner daemon goroutine at Init.

## Task Commits

1. **Task 1 (RED):** failing command router + shorthand tests + skeleton — `62666fdd5` (test)
2. **Task 1 (GREEN):** command router + `=`/`:`/`;` shorthand + PostToChannel semipose + manifest commands block + Layer-1 gate — `da305b290` (feat)
3. **Task 2 (RED):** failing prune sweep tests + skeleton — `1407ebe47` (test)
4. **Task 2 (GREEN):** channelPruner + store prune methods + main.go wiring + prune integration test — `ba5c4cf07` (feat)

## Deviations from Plan

### Auto-fixed / scope additions

**1. [Rule 2 - Missing critical functionality] PostToChannel `semipose` kind (service_rpcs.go, not in files_modified)**

- **Found during:** Task 1.
- **Issue:** The plan requires `=name ;semipose` → channel_pose with **no-space** semantics, but content posting MUST flow through `PostToChannel` (the single membership + not-muted fence, T-01-02), whose pose path hardcoded `invokedAs=":"` (spaced). Emitting directly from the command layer to honor `;` would bypass the Layer-2 emit gate.
- **Fix:** Added a `semipose` case to `PostToChannel` (`emitPose(";")`), keeping the security chokepoint single. The command layer classifies `;` → kind `semipose`, `:` → `pose`, else `say`.
- **Files modified:** plugins/core-channels/service_rpcs.go

**2. [Rule 2 - Missing critical functionality] Prune store methods (store.go, not in files_modified)**

- **Found during:** Task 2.
- **Issue:** The sweep needs to enumerate channels for retention computation and delete `channel_log` rows older than a cutoff; neither store method existed.
- **Fix:** Added `ListChannelsForPrune` + `DeleteChannelLogOlderThan` to store.go (mirrors 01-05b adding moderation store methods).
- **Files modified:** plugins/core-channels/store.go

**3. [Rule 2 - Test coverage] Prune DB delete-path integration test (prune_integration_test.go)**

- The plan's verification requires `task test:int` green for the prune DB delete path; added a Ginkgo integration spec (real DB: default window, per-channel override, unlimited admin channel untouched).

**4. [Design] Pruner lifecycle mirrors scenes (no Unload hook)**

- The plan says "stop on Unload", but the SDK exposes no Unload/Shutdown lifecycle hook (scenes' publishScheduler has the same constraint). The pruner is a daemon goroutine tied to `context.WithCancel(context.Background())` with cancel intentionally uncalled; process exit is the shutdown signal.

**Total deviations:** 2 required scope additions (semipose kind, prune store methods), 1 test-coverage addition, 1 documented lifecycle design choice. No change to security posture — all must_have truths hold.

## Prohibitions Verified

- **A non-owner (non-admin) MUST NOT mute/ban/kick/transfer (D-05):** `TestHandleCommandMuteNonOwnerDeniedUniformNotFound` (denyEvaluator → CommandError, no store mutation); `TestHandleCommandMuteOwnerPermitted` (permit → setMuted recorded); `TestHandleCommandBanKickInviteTransferDelegate` (owner/admin permit → each RPC delegated). The command layer adds no bypass — the service's per-verb ABAC gate is the only authority. **Verified.**
- **The prune sweep MUST NOT delete in-window rows, and MUST NOT prune unlimited-retention admin channels:** `TestPruneSweepUsesDefaultWindowForNullRetentionNonAdmin` / `TestPruneSweepUsesPerChannelRetentionOverride` (exact cutoff = now - window, injected clock); `TestPruneSweepSkipsUnlimitedAdminChannel` (admin + NULL → no delete call); integration `prune_integration_test.go` (real DB: window-edge row preserved via strict `<`, per-channel override prunes the 25h row, unlimited admin ancient row survives). **Verified.**

## Layer-1 Execute Gate (01-04 deferral resolved) + Census

The `execute-channel-commands` policy — `permit(principal is character, action in ["execute"], resource is command) when { resource.command.name in ["channel"] && principal.character.is_guest == false }` — was deferred from 01-04 because `policy_validator.go:145 validateCommandPolicy` rejects a command-targeting policy for a command the plugin does not declare. It now ships in the same manifest change that declares the `channel` command (with its `{action: write, resource: channel, scope: local}` capability backed by `write-channel-as-member`, MED-5), exactly as scenes ships `execute-scene-commands` alongside its commands block. `task test:int -- ./test/integration/wholesystem/` loads core-channels with the command + Layer-1 gate + capability — census green.

## TDD Gate Compliance

Plan `type: tdd`; both tasks `tdd="true"`. **Task 1:** RED `62666fdd5` (`test(...)`, 13 failures against a skeleton router) → GREEN `da305b290` (`feat(...)`, 163 unit green). **Task 2:** RED `1407ebe47` (`test(...)`, 5 failures against a no-op skeleton sweep) → GREEN `ba5c4cf07` (`feat(...)`, 171 unit + 172 integration green). No `feat` shipped without its asserting tests; the `test(...)`→`feat(...)` sequence is present in git log for each task.

## Verification

- `task test -- ./plugins/core-channels/` — 171 tests, exit 0.
- `task test` (full unit suite) — 10072 tests, 3 skipped (quarantined), exit 0.
- `task test:int -- ./plugins/core-channels/` — 172 tests (unit + store/audit/prune integration), exit 0.
- `task test:int -- ./test/integration/wholesystem/` — census loads core-channels with the `channel` command + Layer-1 execute gate + write capability, exit 0.
- `task lint:go` — 0 issues. `task lint:proto` — green. `task lint` overall exits non-zero ONLY on 12 pre-existing MD041/MD075 issues in `.planning/` GSD frontmatter artifacts (PLAN.md/STATE.md/deferred-items.md); none in this plan's files. Out of scope per the scope boundary (identical to 01-03/04/05/05b/06).
- `task plugin:build -- core-channels` (via census host build) — exit 0.

## Command → Service Delegation Map

| Subcommand / shorthand | Delegates to | Denied presentation |
|---|---|---|
| `channel create <name> [type]` | `CreateChannel` (+ withTrustedOwningPlayer, R2-C) | rate-limit / permission |
| `channel join\|leave <name>` | `JoinChannel` / `LeaveChannel` (read gate) | uniform not-found |
| `channel list` | `ListChannels` (self-scoped) | — |
| `channel say <name> <text>`, `=name <text>` | `PostToChannel` kind=say (emit+muted fence) | uniform not-found / muted |
| `=name :text` / `=name ;text` | `PostToChannel` kind=pose / semipose | uniform not-found / muted |
| `channel who <name>` | `WhoInChannel` (store membership gate) | uniform not-found |
| `channel history <name> [count]` | `QueryChannelHistory` (01-06 fence) | uniform not-found |
| `channel invite\|mute\|ban\|kick <name> <target>` | `InviteToChannel`/`MuteMember`/`BanMember`/`KickMember` (owner+admin) | uniform not-found |
| `channel transfer <name> <newowner>` | `TransferOwnership` (owner+admin) | uniform not-found |

## User Setup Required

None.

## Next Phase Readiness

- **01-08** live moderation→leave removal consumes ban/kick membership changes (RemoveStream on the delivery path); the command surface + emit notices are in place.
- **01-09** whole-system e2e exercises the live alias-seeded `=Public hello` hop end-to-end (the `=` manifest alias is seeded under the whole-system tier), plus INV-CHANNEL-1 membership-content gating. This plan's `commands_test.go` covers the parser + router mapping; the e2e covers the live hop.

## Self-Check: PASSED

- Created files present: `commands.go`, `commands_test.go`, `prune.go`, `prune_test.go`, `prune_integration_test.go`.
- Modified files present: `main.go`, `plugin.yaml`, `service_rpcs.go`, `store.go`.
- All four task commits verified in `git log` (`62666fdd5`, `da305b290`, `1407ebe47`, `ba5c4cf07`).
- Channels unit (171) + integration (172) + whole-system census all green after final state.

---

*Phase: 01-channels-subsystem*
*Completed: 2026-07-08*
