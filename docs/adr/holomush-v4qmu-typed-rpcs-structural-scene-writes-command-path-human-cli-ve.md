<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-v4qmu; do not edit manually; use `/adr update holomush-v4qmu` -->

# Typed RPCs for structural scene writes; command path for human/CLI verbs

**Date:** 2026-06-20
**Status:** Accepted
**Decision:** holomush-v4qmu
**Deciders:** Sean Brandt

## Context

The web Scenes Portal (E9.5, holomush-5rh.8) routed all web scene writes through the telnet command path via `sendCommand` / `HandleCommand` — including structural operations like scene creation. E9.5 D4 stated "web writes ride the command path; no new write RPCs." This created a contradiction: create returns a `SceneInfo` the caller must consume, but the command path surfaces it only as unparseable prose (`"Scene created: <id>"`). Typed scene-write RPCs (`WebWatchScene`, `WebExportScene`) already existed as counter-examples on the same surface.

## Decision

Structural / CRUD / management operations on objects (create, set, end, invite, kick, transfer, order, publish-config) MUST be exposed as typed RPCs through the BFF: web client → `WebService` (gateway, protocol-translation only) → `SceneAccessService` facade (session-token→identity + ABAC) → `SceneService`. The command path (`HandleCommand`; surfaces = telnet request/response and the web `/terminal`) is reserved for human / CLI conversational verbs a person performs in the moment: pose, say, ooc, emit, join. This supersedes E9.5 D4 for structural writes.

## Rationale

- Create returns a value the caller consumes (`SceneInfo`); the command path surfaces it only as human-readable prose the web would have to parse.
- Typed proto fields eliminate the escaping hazard inherent in assembling a command string from user-supplied free-text title/description.
- Rule of thumb: typed RPC when the op returns something the caller uses (create→`SceneInfo`, watch→participant, export→bytes); command path for fire-and-forget human verbs (pose/say/join). This unifies the existing precedents and generalizes to future web surfaces.
- Conversational writes (pose/say/ooc/join) on `sendCommand` remain correct and are unchanged; `SceneComposer.svelte` is not touched.

## Alternatives Considered

- **Assemble and send a command string — the E9.5 D4 approach (rejected):** no new proto surface and reuses the `HandleCommand` ABAC gate by construction, but create's result is only prose the web must parse, free-text fields embedded in a single command line create an escaping hazard, and it is inconsistent with the existing `WebWatchScene` / `WebExportScene` typed-RPC precedent on the same surface.
- **Typed RPC through the BFF facade (chosen):** returns a structured `SceneInfo` (no parsing), title/description cross the wire as typed fields (no escaping), consistent with the existing Watch/Export/SetFocus RPCs, atomic, and works for sessions-only players who have no terminal session. Costs new proto RPCs (with full doc comments), a new facade method, and tests at each layer.

## Consequences

- Positive: callers receive structured responses without prose parsing; free-text user input crosses the wire safely; the telnet-free path is unblocked (a scenes-only player with no terminal can originate a scene); one consistent pattern across all web scene write RPCs.
- Negative: new proto RPCs require full doc-comment coverage (proto-doc-comments rule); a new facade method (`SceneAccessServer.CreateScene`) to build, test, and review; E9.5 D4 is partially superseded, so sibling bead holomush-5rh.24 (management verbs) must be re-scoped from the command path to typed RPCs.
- Neutral: pose/say/ooc/join remain on `sendCommand`; the gateway-boundary rule and the BFF-RPC routing invariant (ADR holomush-b0365) continue to apply — this decision complements them (it governs the command-vs-RPC division of labor for writes), it does not supersede them.
