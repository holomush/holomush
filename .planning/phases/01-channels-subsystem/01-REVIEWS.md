---
phase: 1
reviewers: [codex]
reviewed_at: 2026-07-08
plans_reviewed: [01-01-PLAN.md, 01-02-PLAN.md, 01-03-PLAN.md, 01-04-PLAN.md, 01-05-PLAN.md, 01-06-PLAN.md, 01-07-PLAN.md, 01-08-PLAN.md, 01-09-PLAN.md]
reviewer_cli: codex-cli 0.142.5
---

# Cross-AI Plan Review — Phase 1

## Codex Review

**Summary**

The plan set is directionally strong and grounded in the current plugin/ABAC architecture, especially the resource-side membership correction. However, I would not approve it as-is: the live-delivery path has two source-verified blockers, `stream.subscription` would become a broad stream-subscription capability without an instance-level guard, and the planned `ChannelService` implementation does not cover the full proto/command surface later plans depend on. Overall risk is **HIGH** until those are resolved.

**Strengths**

- The resource-side ABAC decision is correct. `AttributeResolverService` only exposes `GetSchema` and `ResolveResource`, with no subject/principal RPC (`api/proto/holomush/plugin/v1/attribute.proto:32-63`), and `PluginAttributeProvider.ResolveSubject` returns nil (`internal/plugin/attribute_proxy.go:44-47`). Modeling membership as `resource.channel.members` is the right shape.
- `resource_types: [channel]` auto-registration is correctly understood. Plugin resource types are merged by `CollectResourceTypes` (`internal/plugin/manager.go:806-818`), and the manager registers a `NewPluginAttributeProvider` for each declared resource type after `GetSchema` validation (`internal/plugin/manager.go:1412-1428`). No `internal/access/prefix.go` or core `validResourceTypes` edit is required for ABAC resolution.
- The plan correctly identifies real `stream.subscription` substrate work. The proto explicitly says the service is declared but unserved (`api/proto/holomush/plugin/host/v1/stream.proto:26-40`), and the server currently embeds only `UnimplementedStreamSubscriptionServiceServer` (`internal/plugin/hostcap/servers.go:914-923`).
- The TDD/validation layering is good: proto first, store/resolver/service/audit/commands/live delivery, then census/e2e/invariants. That is a sensible wave structure for a brownfield subsystem.

**Concerns**

- **HIGH: Plugin session streams returned as dot subjects will be dropped today.** Plans 01-08 expect `QuerySessionStreams` to return `events.<game>.channel.<id>`, but `Manager.QuerySessionStreams` rejects plugin streams unless they contain a colon (`internal/plugin/manager.go:1493-1505`) and drops invalid streams (`internal/plugin/manager.go:1566-1571`). Meanwhile `CoreServer` now expects dot-relative or full `events.` subjects and says colon-style refs are rejected (`internal/grpc/server.go:707-733`), and `eventbus.Qualify` is idempotent for `events.` subjects (`internal/eventbus/qualify.go:12-33`). This is a blocking contract drift.
- **HIGH: LIVE_ONLY is planned but currently rejected and ignored.** Plan 01-02/01-08 require `STREAM_REPLAY_MODE_LIVE_ONLY`, but `SessionStreamRegistry.AddStreamWithMode` rejects any mode other than `ReplayModeFromCursor` (`internal/grpc/stream_registry.go:183-198`). The update struct also says replay mode is currently advisory and ignored by `Subscribe` (`internal/grpc/stream_registry.go:17-29`). So serving the RPC alone will not satisfy "LIVE_ONLY / no history flood."
- **HIGH: `stream.subscription` needs an instance-level safety gate before becoming served.** The descriptor maps both subscription RPCs to `write` on `stream` (`internal/plugin/hostcap/descriptor.go:154-157`), and the seed default-permits declared plugins at `resource == "stream:*"` (`internal/access/policy/seed.go:423-427`). By contrast, `stream.history` performs a concrete stream authorization inside the handler before reading (`internal/plugin/hostcap/servers.go:829-854`). The plan's server implementation should add an analogous concrete-stream guard or namespace ownership check; otherwise any plugin granted `stream.subscription` can ask the host to subscribe an active session to arbitrary streams.
- **HIGH: The service implementation plan does not cover the proto/command surface.** 01-01 defines create/join/leave/list/post/who/history/invite/mute/ban/kick/transfer, but 01-05 only implements create/join/leave/list. Later 01-07 says `who`, `history`, invite, moderation, and transfer delegate to the service. Scenes' reference service implements those structural verbs directly and self-enforces ABAC per verb, e.g. end (`plugins/core-scenes/service.go:707-722`), invite (`plugins/core-scenes/service.go:1521-1536`), kick (`plugins/core-scenes/service.go:1588-1603`), transfer (`plugins/core-scenes/service.go:1666-1681`). Channels needs the same completeness before commands depend on it.
- **MEDIUM: Command capability preflight is misaligned with proposed policies.** 01-07 plans a single manifest command capability `action: write, resource: channel`, and dispatcher preflight calls `CanPerformAction` for each command capability before invoking the plugin (`internal/command/dispatcher.go:294-306`; `internal/command/access.go:69-90`). The proposed policies mostly cover `read`, `emit`, `join`, moderation actions, and create, not `write` on `channel`. Scenes works because it has an explicit `write` policy (`plugins/core-scenes/plugin.yaml:308-311`). Channels should either add a narrow `write` policy or use command capabilities that match the actual actions.
- **MEDIUM: `=name` shorthand routing remains underspecified.** The parser treats the first whitespace-delimited token as the command (`internal/command/parser.go:19-47`), so `=Public hello` becomes command `=Public`. Registered command names must start with a letter (`internal/command/validation.go:19-24`). Prefix aliases can route punctuation if inserted into the alias cache (`internal/command/alias.go:450-484`), but the plan does not specify a reliable seed/dispatcher mechanism for `=`.
- **LOW/MEDIUM: Public read semantics conflict with the proposed invariant wording.** 01-04 proposes `read` on public channels for any character, while 01-09 proposes an invariant that "a character that is not a member of a channel MUST NOT read that channel's history." That invariant is only true for private/admin channels unless public access is modeled as implicit membership.

**Suggestions**

- Add a small prerequisite to fix the session-stream naming contract: either update `isValidStreamName` to accept dot-relative/full `events.` subjects, or explicitly make `QuerySessionStreams` return the current accepted logical form and prove it qualifies to the emitted channel subject.
- Expand 01-02 to implement real `LIVE_ONLY` semantics end-to-end, including registry tests and subscribe-loop behavior, or explicitly choose `FROM_CURSOR` and remove the "no history flood" claim.
- Add instance-level authorization to `streamSubscriptionServer`: qualify the requested stream, reject forbidden namespaces, and ideally ensure the stream is in a namespace/resource owned by the calling plugin or allowed by a concrete policy.
- Split 01-05 or add 01-05b for the missing `ChannelService` methods: `PostToChannel`, `WhoInChannel`, `QueryChannelHistory`, `InviteToChannel`, `MuteMember`, `BanMember`, `KickMember`, and `TransferOwnership`.
- Align command capabilities with ABAC policies before 01-07. Avoid one broad `write channel` gate unless there is a deliberate matching policy and tests proving it does not widen access.
- Define the `=` routing mechanism explicitly: manifest-seeded system prefix alias, dispatcher prefix hook, or parser extension, with an integration test that raw telnet input reaches `core-channels`.

**Risk Assessment**

**HIGH.** The ABAC/resource model is sound, but the live-delivery path would not work with the current source because dot-style plugin streams are dropped and `LIVE_ONLY` is rejected/ignored. Serving `stream.subscription` also opens a sensitive host mutation surface unless it gets concrete-stream authorization. Fixing those issues is feasible, but they should be first-class plan work rather than discovered during implementation.

---

## Consensus Summary

Single reviewer (Codex, source-grounded against the live worktree). All findings below carry `file:line` evidence and were verified against actual code, so they are high-confidence.

### Agreed Strengths
- **Resource-side membership model is correct** — independently verified against `attribute.proto:32-63` and `attribute_proxy.go:44-47` (matches the internal plan-checker's finding).
- **`resource_types:[channel]` auto-registration** — no core `validResourceTypes`/`prefix.go` edit needed (`manager.go:806-818,1412-1428`).
- **`stream.subscription` is genuinely unserved substrate** (`stream.proto:26-40`, `hostcap/servers.go:914-923`) — the plan's holomush-l6std framing is accurate.
- Wave/TDD structure is sound.

### Agreed Concerns (priority order — all NEW vs. internal review)
1. **[HIGH] Dot-style plugin session streams are dropped today.** `Manager.QuerySessionStreams` requires a **colon** in plugin stream refs (`manager.go:1493-1505,1566-1571`) — but 01-08 returns dot subjects `events.<game>.channel.<id>`. Contract drift between the dot-style eradication (server.go/qualify.go) and the still-colon-gated session-stream path. **A prerequisite fix is needed** (accept dot/full `events.` in `isValidStreamName`, or prove the returned form qualifies).
2. **[HIGH] `LIVE_ONLY` is rejected and ignored, not just unserved.** `stream_registry.go:183-198` rejects any mode ≠ `FromCursor`; replay mode is advisory/ignored by `Subscribe` (`:17-29`). Serving the RPC stub alone will NOT deliver "no history flood" — 01-02 must implement real LIVE_ONLY (registry + subscribe loop) or drop the claim and use `FROM_CURSOR`. This sharpens the scoping risk 01-02 already flagged into concrete work.
3. **[HIGH] `stream.subscription` needs an instance-level authz gate.** Both RPCs map to `write` on `stream` and the seed permits plugins at `stream:*` (`descriptor.go:154-157`, `seed.go:423-427`). `stream.history` guards with a concrete-stream check (`servers.go:829-854`); the subscription server must do the same or any grantee can subscribe a session to arbitrary streams. Security finding.
4. **[HIGH] `ChannelService` surface is incomplete for its consumers.** 01-05 implements only create/join/leave/list, but 01-07 delegates who/history/invite/mute/ban/kick/transfer to the service (which scenes implements directly, `service.go:707-722` etc.). Add `PostToChannel`/`WhoInChannel`/`QueryChannelHistory`/`InviteToChannel`/`MuteMember`/`BanMember`/`KickMember`/`TransferOwnership` (split 01-05 or add 01-05b).
5. **[MEDIUM] Command-capability preflight vs policies mismatch.** 01-07's single `write channel` command capability isn't backed by a `write` policy (`dispatcher.go:294-306`, `access.go:69-90`); scenes has an explicit `write` policy (`plugin.yaml:308-311`). Align capabilities with actual policies.
6. **[MEDIUM] `=name` shorthand routing underspecified.** Parser takes the first token as command and command names must start with a letter (`parser.go:19-47`, `validation.go:19-24`); routing `=` needs an explicit seed-alias/dispatcher/parser mechanism (`alias.go:450-484`) + an integration test. (Confirms RESEARCH landmine #3 and the internal review's manual-verify note.)
7. **[LOW/MED] Public-read vs invariant wording conflict.** 01-04 permits public read for any character, but 01-09's invariant says non-members MUST NOT read history — only true for private/admin unless public is modeled as implicit membership. Reconcile the invariant wording.

### Divergent Views
None — single reviewer. Note the internal plan-checker returned 0 blockers; Codex's source-grounded pass found 4 HIGH concerns it missed (chiefly the session-stream naming/LIVE_ONLY contract drift and the incomplete service surface), demonstrating the value of the external adversarial pass.
