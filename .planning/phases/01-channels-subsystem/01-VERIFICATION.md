---
phase: 01-channels-subsystem
verified: 2026-07-08T00:00:00Z
status: passed
score: 5/5 requirements verified; 5/5 success criteria verified
behavior_unverified: 0
overrides_applied: 0
human_decision:
  resolved: 2026-07-09
  decision: "WR-01 and WR-02 ACCEPTED as non-blocking follow-ups (goal + all CHAN-01..05 requirements verified met). Filed as beads: WR-01 → holomush-0sc.13 (flagged for the abac-reviewer gate before any BFF/typed-RPC channel surface ships), WR-02 → holomush-0sc.14. Related follow-ups: holomush-0sc.15 (Lua add_session_stream unfenced path), holomush-0sc.16 (info-item polish IN-01..IN-04)."
human_verification:
  - test: "WR-01 accept/defer decision — target-character identity binding is best-effort (actorMismatch no-ops when actor metadata is absent). Confirm the host ALWAYS stamps ActorMetadata on the ChannelService proxy dispatch, OR make identity binding mandatory (fail-closed) on identity-bearing RPCs (ListChannels/WhoInChannel/QueryChannelHistory/Join/moderation)."
    expected: "Either a test proving the proxy dispatch always stamps ActorMetadata, or a code change failing closed when metadata is absent. Today the command path is safe (host-vouched CommandRequest.CharacterID); exposure is latent for any future typed-RPC/BFF channel path that omits metadata. Mirrors the accepted core-scenes pattern."
    why_human: "Defense-in-depth security decision (accept-as-is vs harden). No web/BFF channel surface exists in this phase, so the gap is latent, not a live exploit — requires a human risk-acceptance call."
  - test: "WR-02 accept/defer decision — InviteToChannel records the TARGET (not the inviter) as the actor in the durable channel_ops_events moderation journal (service_rpcs.go:89 → store.JoinChannel → store.go:255 recordChannelOpsEventTx with target as actor). The live emitJoin notice carries the inviter, but the durable audit trail shows the invitee as a self-join."
    expected: "An invite-aware store path (e.g. store.InviteMember(ctx, channelID, actorID, targetID)) stamping the acting owner/admin as the ops-event actor and the invitee as the target — OR an explicit accept/defer of the reduced moderation-accountability fidelity."
    why_human: "Audit-integrity gap touching the phase's 'audit' substrate guarantee. Non-blocking (goal behaviors verified) but a genuine correctness defect in the moderation journal — warrants a human accept/fix/defer decision, ideally a bd bug."
---

# Phase 1: Channels Subsystem Verification Report

**Phase Goal:** Players can communicate via persistent named channels, independent of physical location, with the same substrate guarantees (EventBus, ABAC, audit) already proven by Scenes.
**Verified:** 2026-07-08
**Status:** passed (WR-01/WR-02 accepted as non-blocking follow-ups — beads holomush-0sc.13/.14)
**Re-verification:** No — initial verification

## Goal Achievement

The phase goal is **achieved in the codebase**. Persistent named channels exist (Postgres tables, plugin-owned store), are location-independent (membership-keyed queries, no location FK), flow through the shared EventBus (EventSink.Emit on JetStream dot subjects), are gated default-deny by ABAC (plugin-manifest seed policies + per-RPC self-enforcement), and audit through the identical plugin-audit seam as Scenes. A whole-system integration test drives the full stack end-to-end and passes (baseline: task test:int 10394, green). Two documented code-review WARNINGs (WR-01 identity binding, WR-02 audit-journal actor fidelity) are non-blocking but warrant a human accept/defer decision — hence `human_needed`.

### Success Criteria (ROADMAP.md §Phase 1)

| # | Criterion | Status | Evidence |
| - | --------- | ------ | -------- |
| 1 | Join/leave/list persistent named channels independent of the spatial world model | ✓ VERIFIED | `channels`/`channel_memberships` persistent tables (`migrations/000001_channels.up.sql:13,38`); `JoinChannel`/`LeaveChannel`/`ListChannels` (`service.go:409,441,475`); `ListChannels` uses `store.ListForCharacter` membership query (`store.go:324`) — no location column/FK. E2E create+join+list drive real stack (`channels_e2e_test.go:98-110`). |
| 2 | Post to + read history from member channels, gated by default-deny ABAC membership | ✓ VERIFIED | `PostToChannel` (`service_rpcs.go:220`) + membership-gated history: `ChannelAuditServer.QueryHistory` runs membership auth as step-1 before any DB work (`audit.go:280-326`); `read`/`emit`/`write` seed policies key on `principal.id in resource.channel.members`, default-deny (`plugin.yaml` policies block). E2E: member reads back "hello-there", non-member `QueryChannelHistory` → uniform `codes.NotFound` (`channels_e2e_test.go:168-189`). |
| 3 | Channel events flow through shared EventBus with same JetStream/audit guarantees as scenes | ✓ VERIFIED | `channelEventEmitter.emitContent` → `EventSink.Emit(EmitIntent{Subject: events.<game>.channel.<id>, Type: core-channels:channel_say, ...})` (`publish_events.go:74-82`); host stamps `core.NewEvent`; audit block `events.*.channel.>` → `plugin_core_channels.channel_log` (`plugin.yaml` audit); QueryHistory JS→Postgres fallback per PluginAuditService. E2E emit→audit→read round-trip (`channels_e2e_test.go:168-175`). |
| 4 | Faction-restricted channels enforce membership-based access distinct from open channels | ✓ VERIFIED | Channel type distinction: `read-public-channel` grants VISIBILITY for `type == "public"` only; private/admin have no public clause → fail-closed to membership; `emit`/`write` require membership for ALL types incl. public (`plugin.yaml` policies). Faction is a documented future clause seam (`principal.faction`, no migration). E2E: private "Secret" — non-invitee gets uniform not-found identical to absent channel; admin-override invite admits (`channels_e2e_test.go:226-267`). |
| 5 | `core-channels` is the substrate's second consumer (INV-S7, N=2) — extraction is a follow-on, not blocking | ✓ VERIFIED | INV-S7 is a spec-only/process invariant (no registry entry, per invariants.yaml INV-SCENE scope comment). Validated structurally: whole-system census loads BOTH `core-channels` and `core-scenes` consuming identical seams (`census_test.go:26` expectedPlugins includes `core-channels`). eventkit/groupkit extraction correctly deferred (out of scope, REQUIREMENTS.md). |

### Requirements Coverage

| Requirement | Source Plans | Status | Evidence |
| ----------- | ------------ | ------ | -------- |
| CHAN-01 (join/leave/list persistent named channels, location-independent) | 01-01, 01-02, 01-03, 01-05, 01-07, 01-08, 01-09 | ✓ SATISFIED | `service.go:409/441/475`; `store.ListForCharacter` (`store.go:324`); persistent tables (`migrations/000001`). |
| CHAN-02 (post + read history, ABAC membership-gated) | 01-01..09 | ✓ SATISFIED | `service_rpcs.go:220/345`; `audit.go:280-326` step-1 membership fence; seed policies default-deny. |
| CHAN-03 (EventBus/JetStream/audit as scenes) | 01-03, 01-06, 01-09 | ✓ SATISFIED | `publish_events.go:74-82` EventSink.Emit dot subjects; `plugin.yaml` audit → `channel_log`; e2e round-trip. |
| CHAN-04 (faction/private-restricted distinct from open) | 01-04, 01-05, 01-05b, 01-07 | ✓ SATISFIED | `plugin.yaml` type-distinct read/emit/write policies; resolver `type`/`members` (`resolver.go:139-151`); e2e private-channel test. |
| CHAN-05 (second substrate consumer, INV-S7 N=2) | 01-09 | ✓ SATISFIED | `census_test.go:26` loads both plugins; identical seams (store/audit/resolver/emit). Extraction correctly deferred. |

### Key Link Verification

| From | To | Via | Status |
| ---- | -- | --- | ------ |
| `commands.go` (channel command) | `channelService` RPCs | dispatcher delegation, all 12 RPCs implemented (`service.go` + `service_rpcs.go`) | ✓ WIRED — no subcommand delegates to a nonexistent method (HIGH-4 closed) |
| `=` prefix | `channel` command | manifest `aliases: ["="]` → alias-seeder | ✓ WIRED — e2e proves live routing (`channels_e2e_test.go:144-157`), not just parser unit |
| `resolver.go` resource-side membership | ABAC seed policies | `resource.channel.members` STRING_LIST (`resolver.go:143-145`) | ✓ WIRED — policies read `principal.id in resource.channel.members` |
| emit path | `channel_log` | EventSink → host audit consumer → PluginAuditService.AuditEvent | ✓ WIRED — e2e emit→audit→read verified |
| QuerySessionStreams / stream.subscription | live delivery | relative `channel.<id>` + LIVE_ONLY | ✓ WIRED — e2e member receives live channel_say, non-member does not |

### Invariant Registration

| Invariant | Binding | Asserted By |
| --------- | ------- | ----------- |
| INV-CHANNEL-1 (non-member MUST NOT read history content, all types; uniform not-found) | bound | `channels_e2e_test.go` (`// Verifies:` at :159) |
| INV-CHANNEL-2 (hidden channel == absent channel, no existence oracle) | bound | `channels_e2e_test.go` (`// Verifies:` at :222) |
| INV-PRIVACY-7 (first history_scope: custom adopter) | bound | `channels_e2e_test.go:160` |

Bindings are genuine (annotations sit above tests with real assertions). Registry green per baseline (TestEveryRegistryInvariantHasBinding passes).

### Anti-Patterns Found

No debt markers (`TBD`/`FIXME`/`XXX`) in `plugins/core-channels/*.go`. No stubs — all 12 RPCs have substantive bodies with ABAC gates. No hollow data flows — resolver reads real store membership, history reads real `channel_log`.

### Warnings (from 01-REVIEW.md — factored into risk)

- **WR-01 (defense-in-depth):** Target-character identity bound only by best-effort `actorMismatch` (no-ops when actor metadata absent). Command path is safe (host-vouched `CommandRequest.CharacterID`); exposure latent for any future typed-RPC/BFF path. Routed to human decision above.
- **WR-02 (audit fidelity):** `InviteToChannel` records the invitee (not the inviter) as the ops-journal actor (`service_rpcs.go:89` → `store.go:255`), degrading moderation accountability. Routed to human decision above. Recommend a `bd` bug.
- Info items IN-01..IN-04 (unpopulated `js_seq` column, loose subject parse, prune-goroutine cancel discarded, unbounded rate-limiter map) are minor and non-blocking.

### Human Verification Required

See frontmatter `human_verification`: WR-01 (identity-binding accept/harden) and WR-02 (audit-journal actor fidelity accept/fix/defer). Both are documented, non-blocking, and mirror accepted core-scenes patterns — but each warrants an explicit human accept-or-defer call before the phase is closed clean.

### Gaps Summary

No blocking gaps. All 5 CHAN requirements and all 5 success criteria are verified TRUE against the codebase with path:line evidence, backed by a green whole-system e2e suite. The only open items are two pre-existing, documented WARNING-level findings surfaced for human risk-acceptance.

---

_Verified: 2026-07-08_
_Verifier: Claude (gsd-verifier)_
