---
phase: 1
reviewers: [codex]
review_round: 2
reviewed_at: 2026-07-08
plans_reviewed: [01-01-PLAN.md, 01-02-PLAN.md, 01-03-PLAN.md, 01-04-PLAN.md, 01-05-PLAN.md, 01-05b-PLAN.md, 01-06-PLAN.md, 01-07-PLAN.md, 01-08-PLAN.md, 01-09-PLAN.md]
reviewer_cli: codex-cli 0.142.5
prior_round_commit: 853efae32
verdict: NOT READY (HIGH) — 3 actionable findings remain (2 HIGH + 1 MED), all new/partial in the 01-02 substrate + 01-05 rate-limit surface. Round-1 findings HIGH-1/HIGH-4/MED-5/MED-6/LOW-7 RESOLVED; HIGH-2 RESOLVED (subscribe-loop test is the gate); HIGH-3 PARTIAL.
---

# Cross-AI Plan Review — Phase 1 (Round 2 — verification re-review)

> Round 1 (commit `853efae32`) surfaced 4 HIGH + 2 MED + 1 LOW; all were incorporated (commit `ee8573d78`). This round verifies the fixes against source and scrutinizes the new work. **The `## Actionable (round 2)` section below is what a subsequent `/gsd-plan-phase 1 --reviews` must incorporate** — the resolved items need no further change.

## Codex Review

**Summary**

The revisions genuinely address most round-1 findings at the plan level, especially the 01-05b service-surface split, the `=` alias mechanism, and the public-read/history invariant wording. I would still mark the plan set **NOT READY** because the new 01-02 substrate work has two high-risk contract problems that would either break mid-session channel joins or leave forbidden stream namespaces insufficiently fenced.

**Round-1 Resolution Check**

- **HIGH-1: RESOLVED.** Current source still drops non-colon stream names: `isValidStreamName` requires `strings.Contains(name, ":")` at `internal/plugin/manager.go:1493-1505`, and invalid streams are dropped at `internal/plugin/manager.go:1566-1571`. Revised 01-02 explicitly changes this to accept dot/full `events.` subjects, consistent with `CoreServer.toSubject` using `eventbus.Qualify` at `internal/grpc/server.go:728-737` and `Qualify` accepting `events.` idempotently at `internal/eventbus/qualify.go:23-33`.
- **HIGH-2: RESOLVED, with verification dependency.** Current `AddStreamWithMode` rejects anything except `ReplayModeFromCursor` at `internal/grpc/stream_registry.go:183-198`, and `applyFilterCtrl` ignores `ctrl.replayMode` while only calling `SetFilters` at `internal/grpc/server.go:1367-1397`. Revised 01-02 correctly requires a real Subscribe-loop no-history-flood test, not just a registry unit test. That is the right gate because `SetFilters` relies on preserving the existing durable cursor at `internal/eventbus/subscriber.go:328-352`.
- **HIGH-3: PARTIAL.** The plan no longer just serves the stub: current `streamSubscriptionServer` is still unimplemented at `internal/plugin/hostcap/servers.go:914-924`, and the descriptor maps add/remove to `write` on `stream` at `internal/plugin/hostcap/descriptor.go:154-157`. However, the proposed guard has two flaws listed below.
- **HIGH-4: RESOLVED.** 01-05b now implements `PostToChannel`, `WhoInChannel`, `QueryChannelHistory`, `InviteToChannel`, `MuteMember`, `BanMember`, `KickMember`, and `TransferOwnership` before 01-07 delegates. This matches the scene service precedent where structural verbs self-enforce evaluator checks before mutation, e.g. invite/kick/transfer at `plugins/core-scenes/service.go:1497-1536`, `plugins/core-scenes/service.go:1564-1603`, and `plugins/core-scenes/service.go:1642-1681`.
- **MED-5: RESOLVED.** Dispatcher preflight does call `CanPerformAction` before plugin dispatch at `internal/command/dispatcher.go:294-306`. The scoped `write-channel-as-member` policy mirrors `write-scene-as-participant` at `plugins/core-scenes/plugin.yaml:308-311`. This is compatible with type-level preflight because `CanPerformAction` deliberately treats resource-attribute permit conditions optimistically at `internal/access/policy/engine.go:486-523`.
- **MED-6: RESOLVED.** `aliases: ["="]` is a real mechanism. Manifest aliases are collected and seeded at `internal/plugin/alias_seeder.go:40-63` and `internal/plugin/alias_seeder.go:81-128`; prefix aliases split `=Public` into prefix/rest and rebuild `channel Public ...` at `internal/command/alias.go:454-514`. Existing `core-communication` uses the same manifest alias pattern for `:`/`;` at `plugins/core-communication/plugin.yaml:39-46`.
- **LOW/MED-7: RESOLVED.** The revised wording now separates public visibility/join eligibility from history content. That matches the scene audit precedent: `QueryHistory` checks caller and membership before pagination/DB work at `plugins/core-scenes/audit.go:498-555`.

**New Concerns**

- **HIGH: 01-02 and 01-08 disagree on stream subject form for mid-session subscription.** 01-02 says `AuthorizeStreamSubscribe` should mirror `AuthorizeStreamRead` and reject pre-qualified `events.` subjects. The current read guard does exactly that at `internal/plugin/pluginauthz/streamread.go:43-55`. But 01-08 says `JoinChannel` will call `streamSub.AddStream(..., dotStyleChannelSubject(...), LIVE_ONLY)`, and the scene analog `dotStyleSceneSubject` returns a full `events.<game>.scene.<id>` subject at `plugins/core-scenes/store.go:1828-1833`. If implemented as written, the new guard will reject the channel plugin's own mid-session join path. Pick one contract: either `stream.subscription` accepts full `events.` subjects safely, or core-channels passes relative `channel.<id>`.
- **HIGH: The proposed `seed:plugin-stream-subscribe` write path cannot rely on existing forbidden-namespace policies.** Current forbidden stream policies are read-only: audit deny uses `action in ["read"]` at `internal/access/policy/seed.go:226-236`, system crypto/system denies are also read-only at `internal/access/policy/seed.go:245-253` and `internal/access/policy/seed.go:290-298`. Existing concrete stream read permit is also read-only at `internal/access/policy/seed.go:436-449`. A new concrete `write` permit for stream subscription will not be carved back by those forbids. 01-02 must add explicit write forbids or perform a manual namespace deny before evaluating the write permit.
- **MEDIUM: 01-05's per-player create rate limit lacks a trusted player-id source for typed service calls.** Commands carry `PlayerID` in `pluginsdk.CommandRequest` at `pkg/plugin/command.go:25-34`, and the dispatcher stamps a host-vouched owning player into context at `internal/command/dispatcher.go:432-449`. But plugin service code only has public actor metadata with kind/id at `pkg/plugin/actor_metadata.go:45-53`, and `HostEvaluator.Evaluate` only accepts action/resource at `pkg/plugin/evaluate_client.go:26-34`. If `ChannelService.CreateChannel` rate-limits per player, the plan should specify how it obtains the trusted owner-player ID without trusting request fields.

**Suggestions**

- Change 01-02/01-08 to one canonical stream input form for `stream.subscription`, and add a test where `core-channels` joins mid-session and the guard permits exactly that stream.
- For `AuthorizeStreamSubscribe`, implement forbidden namespace rejection directly or add write-specific forbids; do not depend on current read forbids.
- Add a small SDK/hostcap helper or explicit service-dispatch contract for trusted owner-player ID before implementing the create rate limiter.

**Updated Risk Assessment**

**HIGH.** The plan is much stronger than round 1, but the shared `stream.subscription` substrate is still risky. The service ordering and command routing are now sound; the remaining blockers are concentrated in 01-02's authorization/subject contract and 01-05's trusted player identity for rate limiting.

---

## Actionable (round 2) — feed these into `/gsd-plan-phase 1 --reviews`

Round-1 findings HIGH-1, HIGH-4, MED-5, MED-6, LOW/MED-7 are **RESOLVED** and need no change. HIGH-2 is resolved provided the subscribe-loop no-history-flood test remains a hard gate (already in 01-02). The following **3 items remain actionable**:

1. **[HIGH — R2-A] Reconcile the `stream.subscription` subject-form contract (01-02 ↔ 01-08).** `AuthorizeStreamSubscribe` (mirroring `AuthorizeStreamRead`, `pluginauthz/streamread.go:43-55`) rejects `events.`-prefixed subjects, but 01-08's `JoinChannel` passes a full `events.<game>.channel.<id>` (scene analog `core-scenes/store.go:1828-1833`) — the guard would reject the plugin's own join. Choose ONE canonical input form for `AddSessionStream`/`RemoveSessionStream` (recommend: pass the **relative** `channel.<id>` from `JoinChannel` and let the guard qualify it, exactly like the read guard), align 01-02 + 01-08, and add a test asserting `core-channels`' own mid-session join is *permitted* for exactly its own channel stream.
2. **[HIGH — R2-B] The `seed:plugin-stream-subscribe` WRITE permit is not fenced by the existing forbids.** Audit/system/crypto stream denies and the concrete stream permit are all `action in ["read"]` (`seed.go:226-236,245-253,290-298,436-449`) — they do not carve back a new `write` permit. Do NOT rely on ABAC forbids to protect forbidden namespaces on the write path. Instead have `AuthorizeStreamSubscribe` reject forbidden namespaces (`system`/`audit`/`crypto`) **directly in-handler** (the owned-namespace fence largely does this, but make forbidden-namespace rejection explicit and tested), OR add write-specific `forbid(... action in ["write"] ...)` seed policies. Add a test: a plugin cannot subscribe a session to `events.<game>.system.*` / audit / another plugin's domain.
3. **[MED — R2-C] Specify the trusted owner-player-id source for `ChannelService.CreateChannel` rate-limiting.** Typed service calls have only public actor metadata (`actor_metadata.go:45-53`); `HostEvaluator.Evaluate` takes only action/resource (`evaluate_client.go:26-34`). Define how create obtains a host-vouched player id without trusting request fields — a small SDK/hostcap helper or an explicit service-dispatch contract — before the 5/hr per-player limiter (01-05) can be trusted.

## Consensus Summary

Single reviewer (Codex, round 2, source-grounded). Trajectory is strongly positive: 5/7 round-1 findings fully resolved, 1 resolved-with-gate, 1 partial. The **3 remaining actionable items are all in the new host-substrate/service surface** the revision introduced — expected, since that's the highest-risk new code. No divergent views (single reviewer). **Recommendation: one more `/gsd-plan-phase 1 --reviews` pass** to close R2-A/B/C, then the plan set should reach READY.
