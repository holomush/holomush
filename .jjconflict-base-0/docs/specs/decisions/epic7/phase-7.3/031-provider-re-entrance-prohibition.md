<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 31. Provider Re-Entrance Prohibition

> [Back to Decision Index](../README.md)

**Review finding:** If a plugin's `ResolveSubject()` called back into the
access control system, it would create a deadlock since the engine is already
mid-evaluation.

**Decision:** Attribute providers MUST NOT invoke `AccessControl.Check()` or
`AccessPolicyEngine.Evaluate()` during attribute resolution. Providers that
need authorization-gated data MUST access repositories directly, consistent
with the `PropertyProvider` pattern.

**Rationale:** The dependency chain `Engine → Provider → Engine` is a deadlock
by design. The existing `PropertyProvider → PropertyRepository` pattern
(bypassing `WorldService`) already demonstrates the correct approach. Making
this an explicit prohibition prevents plugin authors from introducing the
same pattern.
