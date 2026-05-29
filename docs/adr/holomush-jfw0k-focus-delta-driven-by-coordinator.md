<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Per-Connection Focus-Delta Delivery Lives in focus.Coordinator, Not the Plugin-Host RPC Handler

**Date:** 2026-05-28
**Status:** Accepted
**Decision:** holomush-jfw0k
**Deciders:** HoloMUSH Contributors
**Supersedes:** holomush-nki4 (placement of the per-connection delta driving)
**Related:** holomush-66228 (spec + plan), holomush-ymgjs (superseded P0 bug), `.claude/rules/plugin-runtime-symmetry.md`

## Context

Phase 5 introduced per-connection subscription deltas: when a character joins a
scene, the joiner's *live* event subscription must switch from its grid/location
stream to the scene's IC/OOC streams. The computation
(`ComputeFocusManagedStreams` → `StreamDeltas`) and delivery
(`SessionStreamRegistry.SendToConnection`) were placed in the **binary**
plugin-host RPC handler (`internal/plugin/goplugin/host_service.go`), invoked
after `focus.Coordinator.{AutoFocusOnJoin,SetConnectionFocus}` mutated
`Connection.FocusKey`.

Two failures followed from that placement:

1. **Runtime asymmetry.** The Lua focus path (`coordinatorFocusOpsAdapter`)
   delegates to the *same* coordinator but never reaches the binary handler's
   driving code, so a Lua plugin's `auto_focus_on_join` persisted `FocusKey`
   without switching the joiner's live stream — a silent capability gap that
   violates the plugin-runtime-symmetry invariant (a host-side feature MUST
   apply to both runtimes).
2. **Binary production gap (holomush-ymgjs, P0).** Production never wired the
   binary host's `ConnectionSender` (`cmd/holomush/core.go` omitted the field),
   so even the binary path skipped delta delivery in production; only the test
   harness wired it, producing green tests over a broken server.

ADR holomush-nki4 had already stated the intent that per-connection routing be
"fully isolated inside the substrate" — but the nki4-era code put the *driving*
in the plugin host, not the substrate.

## Decision

Per-connection focus-delta driving MUST live inside `focus.Coordinator` — the
common substrate path that both the binary plugin-host RPC handler and the Lua
hostfunc adapter call. The coordinator gains a `ConnectionSender` and a `gameID`
and drives `ComputeFocusManagedStreams` → `StreamDeltas` → `SendToConnection`
itself, on every `AutoFocusOnJoin` / `SetConnectionFocus`. The binary plugin
host no longer carries a `ConnectionSender` (the field and its wiring are
deleted). Both the session-wide `StreamSender` and the per-connection
`ConnectionSender` are assembled from one `SessionStreamRegistry` through a
single helper, `holoGRPC.FocusStreamCoordinatorOptions`, used by both production
and the integration harness so the harness is a faithful production mirror by
construction.

No runtime-specific layer may be the sole driver of per-connection focus deltas.

## Rationale

Driving deltas at the coordinator is runtime-agnostic *by construction*: both
runtimes reach it through `AutoFocusOnJoin` / `SetConnectionFocus`, so parity
cannot regress to one runtime — the symmetry invariant is satisfied structurally
rather than by parallel maintenance. The same relocation closes the binary
production gap (the coordinator is wired in production at `sub_grpc.go`, where
the `SessionStreamRegistry` is already in hand) and makes the bug-prone
two-phase wiring (`ConnectionSender` in one file, `StreamSender` in another)
collapse into one adjacent assembly. It realizes ADR holomush-nki4's own stated
"fully isolated inside the substrate" intent; the nki4-era *placement* in the
host is what this decision corrects.

## Alternatives Considered

### A — Drive deltas in `focus.Coordinator`; delete the host ConnectionSender (chosen)

Both runtimes inherit delta delivery from the one substrate path; the binary
host's `ConnectionSender` seam ceases to exist, so the "forgot to wire it"
failure mode becomes structurally unrepresentable. Fixes the binary prod gap and
the Lua gap in one change.

### B — Wire ConnectionSender into the binary host in prod, made un-forgettable via a `FocusWiring` builder (holomush-ymgjs Option B)

Rejected: fixes only the binary half and leaves the Lua capability gap; and it
adds a config-collapse builder whose entire purpose (an un-forgettable host
`ConnectionSender`) is obviated by removing the host `ConnectionSender`
altogether.

### C — Duplicate the delta-driving in the Lua adapter as well as the binary handler

Rejected: two copies of the same logic on two runtime-specific paths — exactly
the asymmetry-prone shape the plugin-runtime-symmetry rule forbids; every future
change must touch both.

### D — Fail-closed the Lua focus hostfuncs ("unsupported") until parity

Rejected: still a capability gap, only louder; defers rather than resolves the
symmetry violation, and would leave a permanent privilege gradient between
runtimes.

## Consequences

**Positive:**

- Binary and Lua plugins deliver identical per-connection focus deltas; the
  symmetry invariant holds by construction.
- Production binary scene-join live delivery works (closes the ymgjs P0).
- The `internal/grpc` adapter pair is assembled in exactly one place
  (`FocusStreamCoordinatorOptions`), pinned by a meta-test (INV-FS-1, INV-FS-4),
  so prod/harness wiring cannot drift.

**Negative:**

- `focus.Coordinator` gains two dependencies (`ConnectionSender`, `gameID`).
  Both are injected via options; the coordinator depends only on the
  `focus.ConnectionSender` interface (no `internal/grpc` import).

**Neutral:**

- The session-level `StreamSenderAdapter` replay-mode rejection
  (`REPLAY_MODE_NOT_SUPPORTED` for non-`FromCursor`) is unchanged; per-connection
  remains the intended seam for scene streams.

## Source

- Spec: `docs/superpowers/specs/2026-05-28-focus-delta-coordinator-unification-design.md`
- Plan: `docs/superpowers/plans/2026-05-28-focus-delta-coordinator-unification.md`
- Supersedes: `docs/adr/holomush-nki4-per-connection-stream-registry-routing.md`
- Rule: `.claude/rules/plugin-runtime-symmetry.md`
