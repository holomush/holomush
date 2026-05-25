# Add host Evaluate RPC for per-action plugin authorization

**Date:** 2026-05-25
**Status:** Accepted
**Decision:** holomush-dttdj
**Deciders:** Sean Brandt

## Context

Plugin commands share a single command-level ABAC gate: Layer 1 (`execute command:<name>`) plus Layer 2 (type-level capability pre-flight, which by the 2026-04-07 C1 hardening resolves only subject/environment/action attributes — never a resource instance). Per-subcommand authorization is therefore impossible: a subcommand cannot map to a distinct command-level capability without breaking its siblings, and the per-operation policies plugins already declare in `plugin.yaml` are never evaluated because no call-site combines a real resource instance with a specific action. The first concrete blocker is the E2 admin-only gate on `scene extend` (`extend_publish_attempts`), but the gap is general across plugin runtimes.

## Decision

Introduce a single `Evaluate(action, resource) -> Decision` RPC on both `PluginHostService` (binary gRPC) and the Lua `holomush.evaluate` hostfunc, both delegating to one shared host-side implementation that runs the full ABAC engine. This is a **third enforcement tier** layered above Layer 1 (command execute) and Layer 2 (type-level capability pre-flight): the plugin supplies an action and a real resource instance, and the host returns the decision. All per-action plugin authorization unifies on the engine — `core-scenes`' ad-hoc Go authorization checks migrate to DSL policies + `Evaluate` calls.

## Rationale

- The engine, the per-operation policies, and attribute resolution already exist; the only missing piece is an evaluation entry point that carries a real resource instance and a specific action.
- Unifying on the engine eliminates two-sources-of-truth: the DSL policy becomes the single authoritative decision rather than being shadowed by hardcoded Go checks.
- The plugin owns its arg-grammar (it extracts the resource instance); the host owns the decision. Neither leaks into the other.
- A new gated subcommand needs only a new policy in `plugin.yaml` — no new RPC, no host change.

## Alternatives Considered

- **A: Host parses subcommands and evaluates at dispatch.** Rejected — a layering violation: the host sees `cmd.Args` as an opaque string and would have to learn every plugin's subcommand grammar and resource-ID extraction, coupling the host to plugin internals.
- **B: Expose character roles to plugins for Go self-gating.** Rejected — moves enforcement out of the host engine, makes the admin DSL policy decorative, and creates two parallel enforcement engines (engine DSL + plugin Go). Contradicts the unify-on-engine goal.

## Consequences

- **Positive:** the ABAC engine becomes the single authoritative per-action decision point for plugin authorization; every decision gets a host-stamped audit trail (closing a current gap); new gates are policy-only.
- **Negative:** enforcement is cooperative at the plugin boundary — a plugin that bypasses the SDK dispatcher can still ship an ungated action (bounded to plugin-owned resources by the entitlement check; mitigated by the gated subcommand dispatcher). Instance-level evaluation is effectively binary-plugin-only (Lua plugins own no resource types).
- **Neutral:** Layer 1 and Layer 2 are unchanged; decision caching is out of scope (the engine's attribute-resolution caching applies).

## References

- Spec: `docs/superpowers/specs/2026-05-25-plugin-host-evaluate-design.md` §Decisions, §1, §6
- Plan: `docs/superpowers/plans/2026-05-25-plugin-host-evaluate.md`
- Design bead: holomush-8kkv5
