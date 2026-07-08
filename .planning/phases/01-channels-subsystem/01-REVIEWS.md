---
phase: 1
reviewers: [codex]
review_round: 4
reviewed_at: 2026-07-08
plans_reviewed: [01-01-PLAN.md, 01-02-PLAN.md, 01-03-PLAN.md, 01-04-PLAN.md, 01-05-PLAN.md, 01-05b-PLAN.md, 01-06-PLAN.md, 01-07-PLAN.md, 01-08-PLAN.md, 01-09-PLAN.md]
reviewer_cli: codex-cli 0.142.5
prior_round_commits: [853efae32, 22bd3578d, 735577aa9]
verdict: NOT READY — R3-A RESOLVED, but 1 new HIGH (R4-A, latent): history_scope:channel fails manifest validation (closed enum {grid,scene,custom}) so core-channels won't load. Close via /gsd-plan-phase 1 --reviews.
---

# Cross-AI Plan Review — Phase 1 (Round 4 — final confirmation)

> Taper: round 1 = 7 findings, round 2 = 3 (R2-A/B/C), round 3 = 1 (R3-A) — all incorporated. This round confirms R3-A and hunts for regressions/latent blockers. **The `## Actionable (round 4)` section (R4-A) is what a `/gsd-plan-phase 1 --reviews` pass must incorporate.**

## Codex Review

**Summary**

R3-A is **RESOLVED** in the plans: the establishment path and mid-session path are now planned to share one `AuthorizePluginStreamContribution` fence, with relative-only stream refs and explicit establishment-path tests.

However, final verdict is **NOT READY** because I found one genuine existing blocker outside the R3-A edit: the plans declare `history_scope: channel`, but current source rejects any `history_scope` outside `grid`, `scene`, or `custom`.

**R3-A Resolution Check**

**RESOLVED.**

The original source hole is real: `Manager.QuerySessionStreams` currently copies opted-in plugins, calls each host, and merges returned streams directly after only `isValidStreamName` validation (`internal/plugin/manager.go:1512,1566`). The gRPC subscribe path appends those flattened plugin streams directly into the initial filter plan (`internal/grpc/server.go:966`), then qualifies them later (`internal/grpc/server.go:987`). `eventbus.Qualify` passes `events.` subjects through unchanged, so pre-qualified foreign subjects would bypass game scoping (`internal/eventbus/qualify.go:23`).

The updated plan closes that by requiring one shared `pluginauthz.AuthorizePluginStreamContribution(pluginName, ownedEmitDomains, relativeRef)` function and explicitly reusing it from both paths (`01-02-PLAN.md:120,122`). The establishment chokepoint is correctly placed in `Manager.QuerySessionStreams`, before `server.go` loses per-plugin identity (`01-02-PLAN.md:145`). The mid-session guard is required to call the same function first, before `Qualify` and the `write` ABAC decision (`01-02-PLAN.md:163`).

The domain extraction is source-compatible: current emit fencing extracts the leading namespace from dot-relative refs (`internal/plugin/event_emitter.go:229`) and checks it against `Manifest.Emits` (`internal/plugin/event_emitter.go:290`). The plan mirrors that and keeps the fence gameID-free (`01-02-PLAN.md:121`).

The relative-only tightening is also covered: current `isValidStreamName` still requires a colon (`internal/plugin/manager.go:1496`), and the plan replaces that with relative-only acceptance while rejecting `events.` and colon refs (`01-02-PLAN.md:143`). `01-08` is aligned: `QuerySessionStreams`, `AddStream`, and `RemoveStream` must pass `channel.<id>`, not a full `events.` subject (`01-08-PLAN.md:18,107`).

The required establishment-path tests are present: manager tests must prove forbidden/foreign/full/wildcard refs are dropped and own-domain `channel.<id>` is kept, backed by shared fence unit tests (`01-02-PLAN.md:138,213`).

**New Concerns**

**HIGH: `history_scope: channel` will fail manifest validation.**

The plans declare `history_scope: channel` for `core-channels` (`01-03-PLAN.md:78`, `01-06-PLAN.md:80`). Current source has a closed enum of only `grid`, `scene`, and `custom` (`internal/plugin/manifest.go:381`), and rejects unknown values during manifest validation (`internal/plugin/manifest.go:557`). As written, `core-channels` will not load once `emits: [channel]` is present. Use `history_scope: custom` or explicitly plan the validator/schema change for a `channel` scope.

I did not find a new blocker from the R3-A shared-fence design itself. The `Manifest.Emits` capture must be implemented under `m.mu.RLock()` or copied into the local `pluginEntry`; the plan says to respect the existing lock discipline (`01-02-PLAN.md:145`), so I'm treating that as an implementation requirement, not a separate plan failure.

**Final Verdict**

**NOT READY** — R3-A is resolved, but `history_scope: channel` is incompatible with the current manifest validator and will fail plugin load.

---

## Actionable (round 4) — feed into `/gsd-plan-phase 1 --reviews`

R3-A is **RESOLVED**; no change needed there. One item remains:

1. **[HIGH — R4-A] `history_scope: channel` fails manifest validation → `core-channels` won't load.** Plans 01-03 (`:78`) and 01-06 (`:80`) declare `history_scope: channel`, but the manifest `history_scope` enum is closed to `{grid, scene, custom}` (`internal/plugin/manifest.go:381`) and unknown values are rejected at load (`internal/plugin/manifest.go:557`). **Fix — choose one and land it in 01-03 (manifest) + 01-06 (any history-scope reference):**
   - **(a) `history_scope: custom`** — zero host change; `custom` is the generic escape hatch for plugin-owned history scopes. Verify what `history_scope` actually governs (how `QueryHistory` scopes/filters) and that `custom` gives channels the membership-gated, per-channel history semantics it needs. **Preferred if `custom` is semantically adequate** (smallest change, no host churn).
   - **(b) Add `channel` to the enum** — a host change to `internal/plugin/manifest.go:381` (+ the validator at `:557` + `schemas/plugin.schema.json` if it enumerates the values), mirroring how `scene` was added for `core-scenes`. Consistent with channels being the second substrate consumer, but it's a host change with its own test surface. Only take this if `custom` is semantically wrong for channels.
   Whichever is chosen, add an acceptance criterion that `core-channels` LOADS (manifest validates) with `emits: [channel]` present — the whole-system census in 01-09 already asserts load, but the manifest-scope value must be fixed for that to pass.

## Consensus Summary

Single reviewer (Codex, round 4, source-grounded). R3-A confirmed resolved. The one remaining finding (R4-A) is a **latent manifest-validation blocker** present since the early rounds — not a regression from the round-3 edit — that would fail plugin load at execution. It escaped rounds 1–3 (and the internal plan-checker) because those focused on the stream-subscription/service surface, not the manifest enum. It's a one-line manifest fix (or a small, well-scoped host enum addition). **Recommendation: one `/gsd-plan-phase 1 --reviews` pass** to fix `history_scope`, then the plan set should reach READY.
