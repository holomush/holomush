<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 90. System Bypass Takes Precedence Over Degraded Mode

> [Back to Decision Index](../README.md)

**Question:** What happens to the `system` subject when the ABAC engine enters
degraded mode?

**Context:** The evaluation algorithm defines two early-exit paths: Step 1
(system bypass, returns permit for `subject == "system"`) and degraded mode
(returns default-deny for all requests when the engine is unhealthy). The spec
did not clarify the ordering of these checks, creating ambiguity about whether
system operations continue during degraded mode.

**Decision:** System bypass (Step 1) takes precedence over degraded mode. The
`system` subject always receives a permit decision, even when the ABAC engine is
in degraded mode.

**Rationale:** System operations (bootstrap, internal maintenance, health checks)
must continue functioning during degraded mode. If system bypass were blocked by
degraded mode:

1. The bootstrap sequence could not re-seed policies to recover from the
   degraded state.
2. Internal health checks would fail, preventing accurate status reporting.
3. Recovery from degraded mode would require manual intervention outside the ABAC
   system.

Degraded mode protects against unreliable policy evaluation for external
subjects. The system subject bypasses policy evaluation entirely (no policies are
consulted), so degraded mode's concern about unreliable evaluation does not
apply.

**Alternatives Considered:**

- **Degraded mode blocks everything (Option A):** Maximum safety but prevents
  self-healing. Requires external tooling to recover, which contradicts the
  design goal of the system being self-managing.

**Implications:**

- Evaluation algorithm Step 1 (system bypass) is checked before degraded mode
- Test case required: system subject succeeds in degraded mode
- Degraded mode documentation updated to note system bypass exception

**Cross-references:**

- [04-resolution-evaluation.md](../../abac/04-resolution-evaluation.md) —
  evaluation algorithm Steps 1 and degraded mode
- [Decision #39](../phase-7.1/039-effect-system-bypass.md) — system bypass
  design
