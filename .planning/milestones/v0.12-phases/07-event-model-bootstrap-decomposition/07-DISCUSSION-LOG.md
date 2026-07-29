# Phase 7: Event-Model & Bootstrap Decomposition - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-15
**Phase:** 7-event-model-bootstrap-decomposition
**Areas discussed:** Unified Event placement, Plugin replay seam & Seq, Bootstrap depth (ARCH-03), Gateway fix + enforcement (ARCH-05)

---

## Unified Event placement (ARCH-04)

| Option | Description | Selected |
|--------|-------------|----------|
| eventbus.Event, in place | Delete core.Event; command/hostfunc import eventbus. Least churn, keeps the auditRow package-private seam. Cost: command imports a heavy tree, deepening MEDIUM-4 coupling. | |
| Neutral leaf package | New low package holds the unified Event; eventbus builds on it. Cost: bus-only fields (Seq/Rendering/Headers/unexported auditRow) either move too or re-create the duplication. | |
| eventbus.Event + drop command's dep | eventbus.Event wins, but first check whether command needs an event type at all — it's ONE call site. | ✓ |

**User's choice:** eventbus.Event + drop command's dep
**Notes:** Verification confirmed the premise: `internal/command`'s only event construction is `Services.BroadcastSystemMessage` (types.go:622), and `Services.Events()` has no production caller outside its own package. Also surfaced that `hostcap.systemBroadcaster` is a near-duplicate whose own doc comment admits it "Mirrors the {"message": ...} payload shape of command.Services.BroadcastSystemMessage".

---

## Broadcast port scope

| Option | Description | Selected |
|--------|-------------|----------|
| One port, both callers | command.Services + hostcap.SessionAdmin both consume one port; one builder at the wiring layer. | ✓ (after clarification) |
| Port for command only | Smaller diff; leaves the two payload builders able to drift again. | |
| You decide | Defer the boundary to planning. | |

**User's choice:** Free-text — *"I want them both. How do we get rid of the duplication and have a single broadcast port?"*
**Notes:** Answered in prose. Investigation corrected the framing: `core.EventAppender` has **three** distinct emitters (core.Engine arrive/leave; grpc.CoreServer.emitCommandResponse; system broadcast), and only the third is duplicated. A single broadcast port cannot absorb the other two. Design settled as: one concrete builder + consumer-defined interfaces (no shared port package), since `hostcap.SessionAdmin` already declares the shape.

---

## core.Engine's path off core.Event

| Option | Description | Selected |
|--------|-------------|----------|
| Narrow port for Engine too | Engine takes a presence port; core stays a leaf; Engine stays put. | |
| Move Engine out of internal/core | Relocate to a package that may import eventbus; builds eventbus.Event directly. Bigger diff. | ✓ |
| Invert the dep: move ULID/VerbRegistry out | eventbus stops importing core, so core may import eventbus. Blast radius outside Phase 7. | |

**User's choice:** Free-text — *"2 seems like the right thing, even if it is bigger. Agree?"*
**Notes:** Agreed, with grounding rather than deference: `core.Engine` has no logic to protect (one field, three marshal→build→Append methods), so a port would leave an empty delegating shell. The move deletes an artificial layer instead of adding one. Constraint that forced the question: `internal/eventbus` already imports `internal/core` (types.go:13), so `core.Engine` naming `eventbus.Event` would be an import cycle.

---

## Engine naming / home

| Option | Description | Selected |
|--------|-------------|----------|
| presence.Emitter | Rename type only; keep Handle*/EndSession method names for a mechanical diff. | |
| presence.Emitter + method rename | Also rename HandleConnect/HandleDisconnect → Arrived/Departed. Near-free since every call site is touched. | ✓ |
| presence.Lifecycle | Frames it as lifecycle; covers session_ended more naturally but vaguer about its only job. | |

**User's choice:** presence.Emitter + method rename
**Notes:** User challenged the name first — *"a new package probably makes sense, but is 'Engine' alone the right name for it?"* Confirmed: "Engine" is fiction (doc says "core game engine"; it is an event emitter) **and** collides with `policy.Engine`/`policy.NewEngine`. Package `internal/presence` matches the repo's own terminology rule.

---

## Event-type vocabulary home

| Option | Description | Selected |
|--------|-------------|----------|
| Leaf vocabulary pkg, gateway may import | Constants live in a dependency-free leaf both eventbus and the gateway may import. | ✓ |
| Gateway stops matching on core types | telnet owns protocol-local constants; duplicates strings, needs parity tests. | |
| You decide | Defer to planning. | |

**User's choice:** Leaf vocabulary pkg, gateway may import
**Notes:** Surfaced an undocumented ARCH-04/ARCH-05 collision: `telnet/gateway_handler.go:1247-1249` renders on `core.EventTypeArrive`/`Leave`; if the vocabulary followed the Event into eventbus, telnet would import a package already on the `forbidden` list — fixing ARCH-04 naïvely makes ARCH-05 strictly worse.

---

## Plugin replay seam & Seq (ARCH-04)

| Option | Description | Selected |
|--------|-------------|----------|
| Take the fix (host-internal) | Real Seq into the opaque cursor + BeforeSeq to ReplayTail. No proto change. | ✓ |
| Pure refactor, file the bug | Keep Seq: 0; preserve behavior exactly; file the pagination bug separately. | |
| Fix + expose seq to plugins | Also add seq to hostv1.Event so plugins can order themselves. | |

**User's choice:** Initially free-text — *"3 feels right, if we're passing seq to the plugins already (by hard coding it to 0), and far better than some random hard coded value. Am I missing something?"* → after correction, **Option 1**.
**Notes:** The premise was false and this was corrected rather than accepted. Plugins never see a seq: `hostv1.Event` (stream.proto:45-67) has 8 fields and no seq; the `Seq: 0` lives inside the **opaque cursor**, encoded by `internal/eventbus/cursor` — an `internal/` package external plugins cannot import. So option 3 would ADD a new plugin-facing field, not replace a visible hardcoded one — and would violate `eventbus/types.go:143` ("Seq … Host-internal — never serialized in any public proto envelope") plus hand plugins a number whose meaning changes across the hot/cold tier boundary (tier.go:713-718). User confirmed option 1 once shown the evidence.

Discussion established this is a **live correctness bug**, not tidiness: plugin history pagination is ULID-ordered in a system whose own code (hot_jetstream.go:427) states ULIDs do NOT match stream sequence under concurrent publishers.

---

## Bootstrap depth (ARCH-03)

| Option | Description | Selected |
|--------|-------------|----------|
| Full two-phase: zero eager starts | Constructors take handles/providers; all 5 pre-starts die; StartAll owns ordering. | ✓ |
| Targeted: kill what's cheap, document the rest | Most of the win, smaller blast radius; leaves an asterisk on "unified ordering". | |
| Formalize a core tier | Cheapest; keeps the panic-guard trap and legitimizes the pattern that caused a boot panic. | |

**User's choice:** Free-text — *"can't we do 1 with an ordered start/countdown latch multi-phase 'ready to boot' approach?"*
**Notes:** Answered by splitting the idea in half. **Ordering half — declined:** `topoSort`+`DependsOn` already IS the ordered multi-phase boot; the gap is purely constructor-side. A latch would be a second ordering authority competing with topoSort — precisely the MEDIUM-11 failure mode. Also noted `lifecycle.ReadinessRegistry` already exists and would be reused rather than inventing a latch. **"Ready to boot" half — real:** `grpcSubsystem.DependsOn()` excludes `AuditProjection`, so gRPC can serve before audit projection is up; `Start()` conflates acquire and serve. That led to the next question.

---

## Prepare/Activate interface split

| Option | Description | Selected |
|--------|-------------|----------|
| No — handles + real DependsOn edges only | Fix the known gRPC/AuditProjection edge; don't change the interface. | |
| Yes — add Prepare/Activate split | Structural: no subsystem can serve before every dependency is up. Touches ~18 subsystems. | ✓ |
| Defer the split to its own phase | Handle refactor now; give the interface change its own design attention. | |

**User's choice:** *"2 definitely feels right, even with it's size. let's the code/structure enforce the requirement"*
**Notes:** Accepted as a defensible call consistent with the repo's idiom (compile-time write-requires-envelope seam, census meta-tests, fail-closed-at-load gates). Size risk was flagged explicitly and accepted. Two consequences written into CONTEXT as planner-must-settle (D-13): two-phase rollback semantics, and whether the "Start MUST be idempotent" contract (which exists to support the pre-start hack) is retired or kept.

---

## Bootstrap ride-along findings

| Option | Description | Selected |
|--------|-------------|----------|
| MEDIUM-11: encode boot order as a real edge | Comment asserts verifier-before-EventBus; topoSort seeds EventBus first. | ✓ |
| LOW-7: bounded shutdown deadline | StopAll(context.Background()) has no timeout. | ✓ |
| LOW-8: productionSubsystems positional params | 15 same-typed positional params; grew 12→15. | ✓ |
| Phantom SubsystemTLS | ID declared + stringer-generated but never registered. | ✓ |

**User's choice:** All four (multiSelect)
**Notes:** All are in-domain for "unified start/stop ordering" and mostly fall out of the D-09 rewiring.

---

## Wave sequencing

| Option | Description | Selected |
|--------|-------------|----------|
| Two waves, each independently verifiable | Wave A constructors/handles; Wave B the interface split. | ✓ |
| One combined refactor | One pass over ~18 subsystems; harder to attribute a boot regression. | |
| You decide | Defer to planning. | |

**User's choice:** Two waves, each independently verifiable
**Notes:** Chosen as the mitigation for carrying two large structural changes in one behavior-preserving phase.

---

## Gateway fix + enforcement (ARCH-05)

| Option | Description | Selected |
|--------|-------------|----------|
| Leaf-only: forbid packages, extract leaves | Gateway may import dependency-free leaves only; core/session/grpc forbidden wholesale. | ✓ |
| Narrow per-symbol allow-list | The arch review's literal recommendation; smallest diff. | |
| Inline/duplicate the constants | Strictest; needs parity tests per duplicate. | |

**User's choice:** Leaf-only: forbid packages, extract leaves
**Notes:** Converges with the ARCH-04 decisions, which already shrink `internal/core`. Allow-list rejected because `cmd/holomush`'s `coreOnlyFiles` allowlist (~30 entries) demonstrates how that escape hatch rots.

| Option | Description | Selected |
|--------|-------------|----------|
| Extend the AST test + bind INV-EVENTBUS-1 | The test already covers web/telnet; only the forbidden list is wrong. Amend the invariant summary, flip pending→bound. | ✓ |
| AST test + depguard | Belt-and-braces; two gates to keep in sync with different scopes. | |
| You decide | Defer to planning. | |

**User's choice:** Extend the AST test + bind INV-EVENTBUS-1
**Notes:** Discussion established that the arch review's LOW-6 recommendation to "fix the invariant label to INV-GW-1" is **stale** — the GW family was retired into INV-EVENTBUS-1..16 (holomush-hz0v4.14.12). Following it would reverse a completed migration. Captured as D-18.

---

## Claude's Discretion

- Final verb set for `presence.Emitter`'s renamed methods.
- `ReplayTail`'s new signature shape (param vs cursor struct).
- Destination package for the extracted `TranslateSubscribeErr` and the gateway leaves.
- Package placement/naming of the event-type vocabulary leaf and the broadcast builder.
- Internal wave decomposition within Wave A / Wave B.
- Whether MEDIUM-11 lands as a real `DependsOn` edge or comment-deletion + a topo-order pin test.
- PR/delivery shape (Phase 5's precedent was one phase PR).

## Deferred Ideas

- `cmd/holomush`'s `coreOnlyFiles` allowlist (~30 entries) — same escape-hatch pattern, but governs `cmd/holomush`, not web/telnet; outside ARCH-05's text.
- Exposing `Seq` to plugins — forbidden by `eventbus/types.go:143`; would need an invariant amendment + ADR.
- MEDIUM-4's full plugin⇄eventbus⇄grpc coupling unwind — Phase 8.
- `ReadinessRegistry.AllReady` vacuous-truth fail-open (true with zero reporters).
- **Doc drift:** `.planning/PROJECT.md` Key Decision #3 and `.planning/codebase/ARCHITECTURE.md` both still assert the "state derives from replay" principle that MODEL-01 reversed — a MODEL-02 completeness gap. Worth a `gh issue`.
- `internal/core` remains a grab-bag even after Event and Engine leave — Phase 8/999.9.
