---
phase: 1
reviewers: [codex]
review_round: 3
reviewed_at: 2026-07-08
plans_reviewed: [01-01-PLAN.md, 01-02-PLAN.md, 01-03-PLAN.md, 01-04-PLAN.md, 01-05-PLAN.md, 01-05b-PLAN.md, 01-06-PLAN.md, 01-07-PLAN.md, 01-08-PLAN.md, 01-09-PLAN.md]
reviewer_cli: codex-cli 0.142.5
prior_round_commits: [853efae32, 22bd3578d]
verdict: NOT READY — R2-A/B/C all RESOLVED, but 1 new HIGH (R3-A): the namespace fence guards only the mid-session stream.subscription path, not the session-establishment QuerySessionStreams path (which HIGH-1 widened to accept full events. subjects). Close via /gsd-plan-phase 1 --reviews.
---

# Cross-AI Plan Review — Phase 1 (Round 3 — final confirmation)

> Round 1 (`853efae32`): 4 HIGH + 2 MED + 1 LOW → incorporated. Round 2 (`22bd3578d`): 5 resolved + 3 new (R2-A/B/C) → incorporated (`cb8cb2056`). This round confirms R2-A/B/C are genuinely resolved and hunts for regressions/new issues from the round-2 edits. **The `## Actionable (round 3)` section (R3-A) is what a `/gsd-plan-phase 1 --reviews` pass must incorporate.**

## Codex Review

**Summary**

R2-A, R2-B, and R2-C are genuinely addressed in the current plan text for the paths they target. However, the round-2 stream work exposes one new HIGH issue: the session-establishment `QuerySessionStreams` path can still subscribe a session to arbitrary plugin-returned streams without the namespace/relative-form fence now planned for `stream.subscription`.

**R2 Resolution Check**

- **R2-A: RESOLVED.**
  01-02 now defines `stream.subscription` input as domain-relative, rejects `events.` with `STREAM_NOT_RELATIVE`, and requires an own-domain `channel.<id>` permit test: `01-02-PLAN.md:31,144,148,195`. 01-08 now passes `relativeChannelStream(id) => "channel."+id` for `QuerySessionStreams`, `AddStream`, and `RemoveStream`, while keeping the emit subject separate/full: `01-08-PLAN.md:18,28,73-75,102-107`. This matches the existing read guard pattern: `AuthorizeStreamRead` rejects pre-qualified `events.` at `internal/plugin/pluginauthz/streamread.go:50-54` and qualifies relative refs at `:55`. The downstream filter paths qualify relative refs via `eventbus.Qualify`: `internal/grpc/server.go:713-724,1382`, `internal/eventbus/qualify.go:23-33`.

- **R2-B: RESOLVED for `stream.subscription`.**
  The plan explicitly makes forbidden namespace rejection load-bearing in `AuthorizeStreamSubscribe`, before registry mutation, and tests it with the broad write permit active: `01-02-PLAN.md:32,38-39,145,148,186,196`. The source supports the premise: existing forbids are read-only (`internal/access/policy/seed.go:234-236,251-253,272-274,296-298`) and the concrete stream permit is also read-only (`:436-448`), with the current comment saying stream writes remain type-level only at `:443-444`. The planned owned-namespace check is source-compatible with the existing emit fence helpers: namespace extraction at `internal/plugin/event_emitter.go:211-226` and manifest emits matching at `:290-296`.

- **R2-C: RESOLVED.**
  The plan no longer trusts a request field. It keys rate limiting on a private trusted context binding, populated only from `pluginsdk.CommandRequest.PlayerID`, and fails closed for non-admin create when absent: `01-05-PLAN.md:19-20,29,50,77,89,96`; command delegation stamps it in 01-07 at `01-07-PLAN.md:31,97`. Source confirms `CommandRequest.PlayerID` exists (`pkg/plugin/command.go:25-34`) and the dispatcher stamps it from `exec.PlayerID()` (`internal/command/dispatcher.go:407-415`) while separately documenting the owning-player value as authenticated and never plugin-supplied (`:432-449`). Typed service identity is still only actor kind/id (`pkg/plugin/actor_metadata.go:45-53`), and `HostEvaluator.Evaluate` has no player-id channel (`pkg/plugin/evaluate_client.go:26-33`), so this is the right current seam.

**New Concerns**

- **HIGH: Session-establishment stream contribution lacks the same namespace fence as mid-session subscription.**
  01-02 fixes `Manager.QuerySessionStreams` to accept relative `channel.<id>` and full `events.<game>...` subjects: `01-02-PLAN.md:27,127,130,134`. But the planned R2-B guard only covers `stream.subscription` `AddSessionStream`/`RemoveSessionStream`: `01-02-PLAN.md:113,148,202`.

  Existing source appends plugin-contributed streams directly into the Subscribe plan (`internal/grpc/server.go:966-981`) and then qualifies them into actual filters (`internal/grpc/server.go:987,713-724`). `eventbus.Qualify` passes pre-qualified `events.` subjects through unchanged (`internal/eventbus/qualify.go:27-29`). `session_streams` is only a manifest boolean, not a per-stream namespace entitlement (`internal/plugin/manifest.go:107,540-543`). So a `session_streams: true` plugin could return `events.<other-game>.system...`, `system.rekey...`, `audit...`, or another plugin's domain at establishment and bypass the in-handler R2-B fence entirely.

  Required fix: apply the same relative-only, no-wildcard, forbidden-namespace, owned-emits-domain guard to each `QuerySessionStreams` contribution before it is merged. Do not keep full `events.` acceptance for plugin-contributed session streams unless there is a separate host-owned reason and test proving cross-game/system subjects are rejected.

**Final Verdict**

**NOT READY.** The R2 findings are resolved, but the stream namespace fence is only applied to mid-session subscription; the initial `QuerySessionStreams` path can still install forbidden or foreign filters before Subscribe opens.

---

## Actionable (round 3) — feed into `/gsd-plan-phase 1 --reviews`

Round-2 findings R2-A, R2-B, R2-C are **RESOLVED** and need no change. One item remains:

1. **[HIGH — R3-A] Fence the session-establishment `QuerySessionStreams` path with the same guard as mid-session subscription.** The R2-B owned-namespace/forbidden-namespace fence lives only in `AuthorizeStreamSubscribe` (the mid-session `stream.subscription` capability). But `Manager.QuerySessionStreams` — widened by HIGH-1 to accept both relative and full `events.` subjects — merges plugin-contributed streams straight into the Subscribe filter plan (`internal/grpc/server.go:966-981` → qualified at `:987,713-724`), and `session_streams: true` is only a manifest boolean, not a per-stream entitlement (`internal/plugin/manifest.go:540-543`). A `session_streams` plugin could therefore contribute `events.<other-game>.system…` / `system.rekey…` / `audit…` / another plugin's domain at establishment and bypass the mid-session fence. **Fix:** apply the SAME relative-only + no-wildcard + forbidden-namespace + owned-emits-domain guard to each plugin `QuerySessionStreams` contribution BEFORE it is merged into the Subscribe plan. **Strongly prefer forcing plugin session-stream contributions to be RELATIVE-only** (drop full `events.` acceptance for *plugin-contributed* streams — reconsider the HIGH-1 widening, which was intended to make core-channels' dot subjects deliverable, not to admit pre-qualified foreign subjects) so both stream-contribution paths (establishment + mid-session) share ONE guard. Add a test: a `session_streams` plugin returning a cross-game / `system` / `audit` / foreign-domain stream is REJECTED at establishment (with the guard active). This is the substrate half — it belongs in 01-02 alongside the R2-B work; ensure 01-08's `QuerySessionStreams` returns only relative own-domain refs consistent with the tightened guard.

## Consensus Summary

Single reviewer (Codex, round 3, source-grounded). Strong convergence: all of round-1 (7) and round-2's R2-A/B/C are resolved and verified against source. The one remaining HIGH (R3-A) is a direct corollary of the round-2 fix — the fence was applied to one of the two stream-contribution paths but not the other. It's narrow and lands in the same plan (01-02) as the existing fence. **Recommendation: one `/gsd-plan-phase 1 --reviews` pass** unifying the guard across both paths should reach READY.
