---
phase: 01-channels-subsystem
plan: 05b
type: tdd
wave: 5
depends_on: ["01-05", "01-06"]
files_modified:
  - plugins/core-channels/service.go
  - plugins/core-channels/plugin.yaml
  - plugins/core-channels/main.go
  - plugins/core-channels/service_test.go
requirements: [CHAN-02, CHAN-04]
autonomous: true
user_setup: []

must_haves:
  truths:
    - "channelService implements the REMAINING holomush.channel.v1.ChannelService RPCs — PostToChannel, WhoInChannel, QueryChannelHistory, InviteToChannel, MuteMember, BanMember, KickMember, TransferOwnership — on the SAME type 01-05 started (review HIGH-4: the complete service surface exists BEFORE the 01-07 command layer delegates to it)"
    - "each structural/moderation RPC self-enforces ABAC per verb via the host eval capability BEFORE mutating state (INV-SCENE-65 analog, mirroring plugins/core-scenes/service.go EndScene/InviteToScene/KickFromScene/TransferOwnership) — independent of the command wrapper"
    - "moderation (mute/ban/kick/transfer/invite) is owner+admin only (D-05, NO op role); a non-owner non-admin is denied"
    - "PostToChannel emits CommunicationContent via the 01-06 emit path; each membership mutation emits its notice event (channel_join/mute/ban/kick/rename) via the 01-06 emit path; WhoInChannel + QueryChannelHistory are membership-gated"
  artifacts:
    - "plugins/core-channels/service.go — the remaining 8 ChannelService RPC methods added to channelService"
    - "plugin.yaml — any additional capability requires the service needs for these verbs (already has eval from 01-05; emit is self-gated by the fence, stream.history for history reads if routed through the host)"
    - "main.go — wire the emit path + audit/history lookup into the service (shared store handle from 01-05/01-06)"
  key_links:
    - "structural writes are TYPED RPCs on ChannelService, NOT the command path (gateway-boundary rule) — the 01-07 command layer delegates invite/mute/ban/kick/transfer/who/history/post to these methods"
    - "moderation notice emits reuse the 01-06 publish_events.go notice helpers; PostToChannel reuses the 01-06 content-emit path; QueryChannelHistory reuses the 01-06 membership-gated audit QueryHistory (joined_at floor + scrollback cap 500, D-07)"
    - "operations on a channel the caller cannot see return the SAME not-found as a truly absent channel (error uniformity, spec §Error Response Uniformity) across every new RPC"
  prohibitions:
    - statement: "A non-owner (non-admin) MUST NOT invite/mute/ban/kick or transfer ownership of a channel (D-05, owner+admin only, NO op)"
      status: pending
      verification: "service_test: non-owner moderation denied; owner permitted; admin overrides"
    - statement: "A non-member MUST NOT post to, read history from, or list members of a channel (post/history/who all membership-gated); a channel the caller cannot see returns uniform not-found"
      status: pending
      verification: "service_test: non-member PostToChannel/QueryChannelHistory/WhoInChannel denied or uniform not-found; member permitted"
---

<objective>
Complete the `holomush.channel.v1.ChannelService` surface: implement the eight RPCs 01-05 left as `Unimplemented` — `PostToChannel`, `WhoInChannel`, `QueryChannelHistory`, `InviteToChannel`, `MuteMember`, `BanMember`, `KickMember`, `TransferOwnership` — each self-enforcing ABAC per verb, mirroring `plugins/core-scenes/service.go`'s structural verbs. This plan exists to close review finding **HIGH-4**: the 01-01 proto declares all twelve RPCs, 01-05 implements only create/join/leave/list, and the 01-07 command layer delegates who/history/invite/mute/ban/kick/transfer to the service — so the full service surface MUST exist before commands consume it.

Purpose: CHAN-02 (post/read/who via typed RPCs) + CHAN-04 moderation (invite/mute/ban/kick on private/admin channels) as TYPED structural writes (gateway-boundary rule — structural writes are typed RPCs, not the command path). This is the service surface the 01-07 command layer and a future web BFF delegate to.
Output: the remaining 8 RPC methods on `channelService` + main wiring; service tests green.

**Dependency note:** this plan depends on 01-05 (the service type + create/join/leave/list + eval wiring) and 01-06 (the emit path for content + moderation notices, and the membership-gated audit `QueryHistory` these RPCs reuse). It is wave 5 (after 01-06 wave 4). The 01-07 command layer depends on THIS plan (not 01-06 directly) so it never delegates to a nonexistent method.
</objective>

<execution_context>
@$HOME/.claude/gsd-core/workflows/execute-plan.md
@$HOME/.claude/gsd-core/templates/summary.md
</execution_context>

<context>
@.planning/phases/01-channels-subsystem/01-CONTEXT.md
@.planning/phases/01-channels-subsystem/01-RESEARCH.md

# Reference service — the structural verbs to MIRROR (self-enforcing ABAC per verb):
@plugins/core-scenes/service.go
@plugins/core-scenes/plugin.yaml
# Rules:
@.claude/rules/gateway-boundary.md
@.claude/rules/grpc-errors.md
@.claude/rules/plugin-manifest.md
# Prior artifacts this plan extends:
@plugins/core-channels/service.go
@plugins/core-channels/store.go
@plugins/core-channels/publish_events.go
@plugins/core-channels/audit.go
</context>

## Artifacts this plan produces

- `plugins/core-channels/service.go` — adds to the existing `channelService` (from 01-05):
  - `InviteToChannel`, `MuteMember`, `BanMember`, `KickMember`, `TransferOwnership` — owner+admin ABAC self-enforced (D-05, no op), store mutation (`SetMuted`/`SetBanned`/membership removal/owner change from 01-03's store), `channel_ops_events` journal append, and a notice emit (channel_mute/ban/kick/rename) via the 01-06 emit path.
  - `PostToChannel` — membership-gated content emit via the 01-06 `comm.Say`/`comm.Pose` + `eventSink.Emit` path.
  - `WhoInChannel` — membership-gated member list from the store (`GetWithMembership`), never a world/location query.
  - `QueryChannelHistory` — the typed service history read; delegates to the 01-06 membership-gated audit `QueryHistory` (joined_at floor + scrollback cap 500, D-07), returning uniform not-found for a channel the caller cannot see.
- `plugin.yaml` — confirm the capability set covers these verbs (eval already declared in 01-05; emit is self-gated by the fence; `stream.history` if `QueryChannelHistory` routes through the host StreamHistory capability — align with how 01-06 reads history).
- `main.go` — wire the emit path + audit/history lookup into the service (share the store handle established in 01-05/01-06; no second store connection).

<tasks>

<task type="tdd" tdd="true">
  <name>Task 1: Structural moderation RPCs — Invite/Mute/Ban/Kick/Transfer, owner+admin only (RED→GREEN)</name>
  <files>plugins/core-channels/service.go, plugins/core-channels/plugin.yaml, plugins/core-channels/main.go, plugins/core-channels/service_test.go</files>
  <read_first>plugins/core-scenes/service.go:1497-1536 (InviteToScene — per-verb eval self-enforcement then store mutation then notice), :1564-1603 (KickFromScene), :1642-1681 (TransferOwnership), :681-722 (EndScene structural-verb shape); plugins/core-channels/store.go (SetMuted/SetBanned/LeaveChannel/owner update + channel_ops_events append from 01-03); plugins/core-channels/publish_events.go (notice emit helpers from 01-06); plugins/core-channels/plugin.yaml (Layer-2 owner-moderation policy from 01-04); .claude/rules/gateway-boundary.md; .claude/rules/grpc-errors.md</read_first>
  <behavior>
    - InviteToChannel: owner or admin may invite a character to a private/admin channel (records the invite the JoinChannel invite-flow consumes); a non-owner non-admin is denied; inviting on a channel the caller cannot see returns uniform not-found.
    - MuteMember / BanMember / KickMember: owner or admin only (D-05, NO op role); each mutates the store (SetMuted / SetBanned+remove / remove), appends a channel_ops_events row, and emits the corresponding notice (channel_mute/ban/kick) via the 01-06 emit path. A banned character is removed from live delivery via the leave/RemoveStream path (01-08 moderation → leave). A non-owner non-admin is denied.
    - TransferOwnership: owner or admin may transfer ownership to another member; the new owner becomes owner and the old owner becomes member; a non-owner non-admin is denied; transferring to a non-member is rejected.
    - Every RPC self-enforces ABAC via the host eval capability BEFORE mutating state (independent of the command wrapper) and never trusts client-supplied identity — the caller is the host-vouched dispatch subject.
    - A moderation op on a channel the caller cannot see returns the SAME uniform not-found as a truly absent channel (error uniformity).
  </behavior>
  <action>Add `InviteToChannel`, `MuteMember`, `BanMember`, `KickMember`, `TransferOwnership` to `channelService`, mirroring `SceneServiceImpl`'s structural verbs. Each: (1) self-enforce ABAC via the injected HostEvaluator against the owner-moderation Layer-2 policy from 01-04 (owner OR admin; D-05, no op), (2) mutate the store (SetMuted / SetBanned / LeaveChannel-style removal / owner update) and append a `channel_ops_events` row, (3) emit the notice event via the 01-06 emit path (`Sensitive: false`, plaintext D-04). Return uniform `codes.NotFound` for a channel the caller cannot see; map store `oops` codes to gRPC status with generic messages, log inner detail via `slog.*Context` (`.claude/rules/grpc-errors.md`). Confirm the manifest capability set (eval from 01-05) covers these; declare any additional `requires` only if genuinely needed. Write `service_test.go` cases (table-driven + fakes): non-owner invite/mute/ban/kick/transfer denied; owner permitted; admin overrides; transfer-to-non-member rejected; moderation on a hidden channel → uniform not-found; each op emits its notice + appends an ops-events row.</action>
  <verify>
    <automated>task test -- ./plugins/core-channels/</automated>
  </verify>
  <done>Invite/mute/ban/kick/transfer implemented on channelService, owner+admin-only ABAC self-enforced, notice emit + ops-events append, uniform not-found for hidden channels; `task test -- ./plugins/core-channels/` green.</done>
</task>

<task type="tdd" tdd="true">
  <name>Task 2: Content + read RPCs — PostToChannel, WhoInChannel, QueryChannelHistory (RED→GREEN)</name>
  <files>plugins/core-channels/service.go, plugins/core-channels/main.go, plugins/core-channels/service_test.go</files>
  <read_first>plugins/core-scenes/service.go (PostToScene/WhoInScene/history analogs if present — membership-gated read shape + emit); plugins/core-channels/publish_events.go (content emit path from 01-06); plugins/core-channels/audit.go (membership-gated QueryHistory with joined_at floor + scrollback cap from 01-06); plugins/core-channels/store.go (GetWithMembership for the member list); .claude/rules/grpc-errors.md</read_first>
  <behavior>
    - PostToChannel: a member posts content — builds CommunicationContent and emits via the 01-06 content-emit path on the channel subject; a non-member is denied (Layer-2 emit policy) with uniform not-found for a channel the caller cannot see. Identity flows by subject + live name lookup, NOT a payload channel_name (D-08).
    - WhoInChannel: a member lists the channel's members (from the store's GetWithMembership); a non-member is denied / uniform not-found; never references location.
    - QueryChannelHistory: a member reads history via the 01-06 membership-gated audit QueryHistory — history never crosses the member's most-recent joined_at, page size clamps to the scrollback cap (500, D-07); a non-member is denied; a channel the caller cannot see returns uniform not-found.
    - Every RPC self-enforces ABAC before doing work; never trusts client-supplied identity.
  </behavior>
  <action>Add `PostToChannel`, `WhoInChannel`, `QueryChannelHistory` to `channelService`. `PostToChannel`: self-enforce the emit (membership) gate, build `CommunicationContent` via the 01-06 `comm.Say`/`comm.Pose` builders, emit through the 01-06 content-emit path (`Sensitive: false`); no payload channel_name (D-08). `WhoInChannel`: self-enforce read/membership, return the member list from `store.GetWithMembership`. `QueryChannelHistory`: self-enforce membership then delegate to the 01-06 membership-gated audit `QueryHistory` (joined_at floor + scrollback cap 500) — do NOT re-implement the auth; reuse the single membership-gated path so history authorization stays in one place. Uniform `codes.NotFound` for a channel the caller cannot see; generic errors, inner detail via `slog.*Context` (`.claude/rules/grpc-errors.md`). Wire the emit path + history/audit lookup into the service in `main.go` (shared store handle). Write `service_test.go` cases: member post emits CommunicationContent (no channel_name); non-member post denied/uniform not-found; member who = member list; non-member who denied; member history respects joined_at floor + 500 cap; non-member history denied; hidden channel → uniform not-found.</action>
  <verify>
    <automated>task test -- ./plugins/core-channels/</automated>
  </verify>
  <done>PostToChannel/WhoInChannel/QueryChannelHistory implemented, membership-gated, content emit reuses 01-06 path (no channel_name authz field), history reuses the membership-gated audit path (joined_at floor + 500 cap), uniform not-found for hidden channels; `task test -- ./plugins/core-channels/` + `task test:int` green.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| command/RPC caller → service | The service must not trust client-supplied identity; it uses the host-vouched dispatch subject |
| moderation RPC → authority | Only owner/admin may invite/mute/ban/kick/transfer |
| content/history RPC → membership | Post/who/history must be gated by membership |

## STRIDE Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation Plan |
|-----------|----------|-----------|----------|-------------|-----------------|
| T-01-14 | Elevation of privilege | non-owner moderation via service RPC | medium | mitigate | Per-verb self-enforced owner+admin ABAC (D-05, no op) BEFORE store mutation; admin override explicit |
| T-01-02 | Elevation of privilege | non-member post via PostToChannel | high | mitigate | PostToChannel self-enforces the Layer-2 emit (membership) policy before emitting |
| T-01-01 | Information disclosure | non-member reads history/members | high | mitigate | QueryChannelHistory delegates to the 01-06 membership-gated audit path; WhoInChannel self-enforces membership |
| T-01-12 | Information disclosure | hidden-channel oracle via new RPCs | medium | mitigate | Uniform codes.NotFound for absent vs hidden on every new per-channel RPC |
| T-01-16 | Spoofing | client-supplied caller identity | high | mitigate | Service uses host-vouched dispatch subject; never trusts request-supplied ids |
| T-01-SC | Tampering | package installs | n/a | accept | No package installs (all in-tree); SC checkpoint does not fire |
</threat_model>

<verification>
- `task test -- ./plugins/core-channels/` green (service unit — moderation + content/read RPCs).
- `task test:int` green (service against store/audit testcontainer, membership-gated history).
- `task lint` green (manifest schema; capability declarations).
</verification>

<success_criteria>
The full `holomush.channel.v1.ChannelService` surface is implemented (create/join/leave/list from 01-05 + post/who/history/invite/mute/ban/kick/transfer here); moderation is owner+admin-only, content/read RPCs are membership-gated, hidden channels return uniform not-found, and every RPC self-enforces ABAC per verb. The 01-07 command layer now has a complete service to delegate to (HIGH-4 closed). CHAN-02 (post/read/who) + CHAN-04 (moderation) complete at the service layer.
</success_criteria>

<output>
Create `.planning/phases/01-channels-subsystem/01-05b-SUMMARY.md` when done.
</output>
