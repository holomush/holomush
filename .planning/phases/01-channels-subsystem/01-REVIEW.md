---
phase: 01-channels-subsystem
reviewed: 2026-07-09T01:55:00Z
depth: standard
files_reviewed: 35
files_reviewed_list:
  - api/proto/holomush/channel/v1/channel.proto
  - docs/architecture/invariants.yaml
  - internal/access/policy/seed.go
  - internal/grpc/stream_registry.go
  - internal/plugin/goplugin/host.go
  - internal/plugin/hostcap/capabilities.go
  - internal/plugin/hostcap/register.go
  - internal/plugin/hostcap/servers.go
  - internal/plugin/hostfunc/functions.go
  - internal/plugin/lua/hostcap_adapter.go
  - internal/plugin/manager.go
  - internal/plugin/pluginauthz/streamsubscribe.go
  - internal/plugin/setup/subsystem.go
  - internal/testsupport/integrationtest/harness.go
  - internal/testsupport/integrationtest/plugins.go
  - pkg/plugin/capability_declaration.go
  - pkg/plugin/sdk.go
  - pkg/plugin/session_streams_handler.go
  - pkg/plugin/stream_subscription_client.go
  - plugins/core-channels/audit.go
  - plugins/core-channels/commands.go
  - plugins/core-channels/main.go
  - plugins/core-channels/observability.go
  - plugins/core-channels/prune.go
  - plugins/core-channels/publish_events.go
  - plugins/core-channels/resolver.go
  - plugins/core-channels/service.go
  - plugins/core-channels/service_rpcs.go
  - plugins/core-channels/store.go
  - plugins/core-channels/types.go
  - plugins/core-channels/plugin.yaml
  - plugins/core-channels/migrations/000001_channels.up.sql
  - plugins/core-channels/migrations/000001_channels.down.sql
  - plugins/core-channels/migrations/000002_create_channel_log.up.sql
  - plugins/core-channels/migrations/000002_create_channel_log.down.sql
findings:
  critical: 0
  warning: 2
  info: 4
  total: 6
status: issues_found
---

# Phase 01: Code Review Report

**Reviewed:** 2026-07-09T01:55:00Z
**Depth:** standard
**Files Reviewed:** 35
**Status:** issues_found

## Summary

Adversarial review of the plaintext channels subsystem (binary plugin +
host-side stream fence + ABAC seeds). The security-critical surfaces the phase
called out all hold up under trace-through:

- **ABAC seed hygiene** — `internal/access/policy/seed.go` contains no
  `resource is channel` / `channel:` permit. All channel authorization lives in
  `plugins/core-channels/plugin.yaml`. The host does not seed the plugin-owned
  resource type. The only host permit that can match a channel is the
  role-gated `seed:admin-full-access` (D-06 admin override, by design) — not an
  unconditional character grant like the removed scene stubs.
- **Unified stream-subscribe fence** — verified BOTH contribution paths call the
  same `pluginauthz.AuthorizePluginStreamContribution`: the establishment merge
  (`internal/plugin/manager.go:1597`) and the mid-session capability handler
  (`internal/plugin/hostcap/servers.go:940` → `AuthorizeStreamSubscribe` →
  `AuthorizePluginStreamContribution`). The intentionally-broad
  `seed:plugin-stream-subscribe` write permit is bounded in-handler; there is no
  second unfenced path.
- **Resolver fail-safe** — optional `owner` attribute is OMITTED (not
  empty-string sentineled) for system-owned channels, with an always-present
  `has_owner` witness (`resolver.go:162-168`), per `abac-providers.md`.
- **Existence-oracle discipline** — per-RPC denials, store misses, and the
  create/read/moderation gates all collapse to a uniform `codes.NotFound`
  (service.go/service_rpcs.go); `MembershipForHistory` returns the same `false`
  for absent-channel and non-member.
- **History gating** — membership is auth step-1 before any DB work, shared by
  the streaming `QueryHistory` and the service `HistoryForMember`
  (`audit.go:280-439`); no per-row crypto fence is needed (plaintext, D-04).
- **Migrations** — paired, idempotent (`IF NOT EXISTS` / `ON CONFLICT`),
  reversible, no triggers/functions; `channel_log` carries no DEK columns.
- **Event emission** — plugin emits via `EventSink.Emit(EmitIntent{...})` (host
  stamps `core.NewEvent`); wire types are plugin-qualified `core-channels:<verb>`
  (`publish_events.go:21-30`).

No blockers. The findings below are a defense-in-depth gap that depends on host
wiring, an audit-trail fidelity gap, and minor quality items.

## Warnings

### WR-01: Target-character identity is bound only by `actorMismatch`, which no-ops when actor metadata is absent

**File:** `plugins/core-channels/service.go:283-286` (and `ListChannels`
`service.go:475-496`, `WhoInChannel` `service_rpcs.go:301-338`,
`QueryChannelHistory` `service_rpcs.go:345-379`)
**Issue:** Every RPC authorizes the *caller* via the host-vouched ABAC subject
(good), but the *target character* it acts on is taken from
`req.GetCharacterId()`. The only guard that binds `req.character_id` to the
authenticated caller is `actorMismatch`, which returns `false` (not a mismatch)
whenever `ActorMetadataFromIncomingContext` reports `ok == false`:

```go
func actorMismatch(ctx context.Context, characterID string) bool {
    kind, id, ok := pluginsdk.ActorMetadataFromIncomingContext(ctx)
    return ok && kind == pluginsdk.ActorCharacter && id != characterID
}
```

`ListChannels`, `WhoInChannel`, and `QueryChannelHistory` have **no independent
ABAC gate on the target** — they query `ListForCharacter` /
`MembershipForHistory` keyed directly on `req.character_id`. If the
ChannelService gRPC-proxy dispatch path ever reaches these handlers without
stamping actor metadata, a caller who controls `character_id` can enumerate an
arbitrary victim character's channel memberships, roster membership, and channel
history (info disclosure). `JoinChannel`/moderation similarly act on the
supplied id. The command path is safe (host-vouched `CommandRequest.CharacterID`),
so exposure is limited to any typed-RPC/BFF path that omits metadata. This
mirrors the accepted core-scenes pattern, so it is a defense-in-depth gap rather
than a proven exploit.
**Fix:** Confirm (and, ideally, test) that the host always stamps
`ActorMetadata` on the ChannelService proxy dispatch, OR make the identity
binding mandatory rather than best-effort — e.g. fail closed when metadata is
absent on identity-bearing RPCs:

```go
func requireActor(ctx context.Context, characterID string) error {
    kind, id, ok := pluginsdk.ActorMetadataFromIncomingContext(ctx)
    if !ok || kind != pluginsdk.ActorCharacter || id != characterID {
        return status.Error(codes.PermissionDenied, "actor identity required")
    }
    return nil
}
```

### WR-02: Invite records the target — not the inviter — as the actor in the moderation journal

**File:** `plugins/core-channels/service_rpcs.go:89` →
`plugins/core-channels/store.go:255`
**Issue:** `InviteToChannel` records the membership by calling the same
`store.JoinChannel(ctx, channelID, targetCharacterId)` the self-join path uses.
Inside `JoinChannel` the append-only ops journal row is written as
`recordChannelOpsEventTx(..., opsKindMembershipJoin, characterID, characterID, nil)`
where `characterID` is the *target*. The durable `channel_ops_events` row (the
T-01-11 moderation journal) therefore shows the invited character as a
self-join and loses the inviting owner/admin's identity. The live notice
(`emitJoin`) carries the inviter, but the durable audit trail — the artifact
moderation accountability relies on — does not.
**Fix:** Give the store an invite-aware path that stamps the acting owner/admin
as the ops-event actor and the invitee as the target, e.g.
`store.InviteMember(ctx, channelID, actorID, targetID)` recording
`recordChannelOpsEventTx(..., opsKindMembershipJoin, actorID, targetID, ...)`,
and call it from `InviteToChannel` with `req.GetCharacterId()` as `actorID`.

## Info

### IN-01: `channel_log.js_seq` column is declared but never populated

**File:** `plugins/core-channels/migrations/000002_create_channel_log.up.sql:27`
vs `plugins/core-channels/audit.go:134-141`
**Issue:** The `js_seq BIGINT` column is created but `Insert` never writes it, so
it is always NULL. `queryLog` orders by `id`, never `js_seq`, so it is dead
within the plugin. Harmless now, but a latent trap for any future consumer that
assumes JetStream sequence is recorded.
**Fix:** Either drop the column from the migration, or thread the delivery's JS
sequence into `Insert` if it is intended to be persisted.

### IN-02: `parseChannelSubject` accepts subjects with trailing facets instead of rejecting them

**File:** `plugins/core-channels/audit.go:444-461`
**Issue:** The parser requires `len(parts) >= 4` and checks `parts[0]=="events"`
/ `parts[2]=="channel"`, then returns `parts[3]`. A subject like
`events.main.channel.<id>.extra` is accepted; the extracted channel id is
`<id>` (auth keys on it) while `queryLog` filters on the full subject string, so
the query returns nothing. Behavior is benign (no leak; a non-member is still
denied), but the shape is looser than "exactly `events.<game>.channel.<id>`".
**Fix:** Require `len(parts) == 4` (reject extra tokens) so malformed subjects
fail with `CHANNEL_AUDIT_SUBJECT_INVALID` rather than silently returning empty.

### IN-03: Prune goroutine's cancel func is intentionally discarded (no graceful shutdown)

**File:** `plugins/core-channels/main.go:321-330`
**Issue:** `pruneCtx, pruneCancel := context.WithCancel(context.Background()); _ = pruneCancel`
means the sweep goroutine's context is never cancelled; it relies on process
exit. If the plugin is ever stopped without a process exit (e.g. a future
in-process teardown), the goroutine leaks and its ticks will spin against a
closed pool (errors are logged, loop continues). Acceptable under the current
go-plugin process model and mirrors core-scenes, but there is no `Close()` seam
wiring `pruneCancel`.
**Fix:** Store `pruneCancel` on `channelPlugin` and invoke it from a shutdown
hook (or the store `Close`) so the sweep terminates deterministically.

### IN-04: `createRateLimiter.events` map grows unbounded across distinct players

**File:** `plugins/core-channels/service.go:118-162`
**Issue:** The in-memory limiter keeps one map entry per distinct owning-player
id and prunes a player's slice only when `allow(player)` is next called. A
player who creates once and never again leaves a stale entry forever, so the map
grows with the count of distinct creators over process lifetime. (Bordering the
out-of-scope performance category, but it is an unbounded resource retention,
not an algorithmic-complexity concern.)
**Fix:** Opportunistically evict entries whose retained slice is empty after
pruning, or run a periodic sweep keyed off `createRateWindow`.

---

*Reviewed: 2026-07-09T01:55:00Z*
*Reviewer: Claude (gsd-code-reviewer)*
*Depth: standard*
