<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Make plugin authorization gates structural via gated subcommand dispatcher

**Date:** 2026-05-25
**Status:** Accepted
**Decision:** holomush-9l9pu
**Deciders:** Sean Brandt

## Context

The host `Evaluate` RPC (see [holomush-dttdj](holomush-dttdj-host-evaluate-rpc-per-action-plugin-authz.md)) leaves enforcement cooperative: a plugin author who forgets to call `Evaluate` ships an ungated subcommand. The host cannot verify call-sites because it never sees subcommands. The risk is bounded to plugin-owned resources by the entitlement check, but "core plugin author forgets" is a realistic failure mode — `core-scenes`' existing ad-hoc Go authorization checks demonstrate the pattern.

## Decision

The plugin SDK provides a **gated subcommand dispatcher**. Each subcommand is registered as `{name, action, resourceRef func(args) (string, error), handler}`. The SDK calls `Evaluate(action, resourceRef(args))` before the handler and short-circuits to a denial response on deny (or a service-failure response on engine error). `core-scenes` adopts the dispatcher and removes its ad-hoc Go authorization checks. A table-driven backstop test (INV-7) asserts every gated subcommand denies when its policy denies.

## Rationale

- A structural gate eliminates the "forgot to call Evaluate" failure mode for subcommands registered through the dispatcher — authorization is no longer a remembered call.
- The `resourceRef` extractor keeps arg-grammar in the plugin, so no host layering violation is introduced.
- INV-7 gives a table-driven, automatically-checked regression guard.
- `core-scenes` is the N=1 concrete consumer that validates the API before it becomes a shared primitive.

## Alternatives Considered

- **Document the Evaluate call pattern; rely on code review.** Rejected — authorization gates remain a remembered call; review cannot exhaustively prove every subcommand is gated, and the failure is silent (ungated action, no runtime error).

## Consequences

- **Positive:** the gate is structural for dispatcher-registered subcommands; INV-7 catches regressions automatically; existing non-gated subcommands remain backward-compatible (opt-in).
- **Negative:** adoption is opt-in — a plugin that bypasses the dispatcher still faces the cooperative-enforcement problem; adds one SDK abstraction for authors to learn.
- **Neutral:** the host never verifies call-sites; the dispatcher's guarantee is scoped to subcommands registered through it.

## References

- Spec: `docs/superpowers/specs/2026-05-25-plugin-host-evaluate-design.md` §6
- Plan: `docs/superpowers/plans/2026-05-25-plugin-host-evaluate.md` Task 6, Task 8 (INV-7)
- Design bead: holomush-8kkv5
