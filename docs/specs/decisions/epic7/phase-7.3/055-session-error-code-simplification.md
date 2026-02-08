<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 55. Session Error Code Simplification

> [Back to Decision Index](../README.md)

**Review finding:** The spec defined four distinct error cases for session
resolution (SESSION_NOT_FOUND, SESSION_STORE_ERROR, SESSION_NO_CHARACTER,
SESSION_CHARACTER_INTEGRITY) with separate policy IDs, effect codes, and
metrics. This was disproportionate complexity for an edge case.

**Decision:** Simplify to two error codes:

- **SESSION_INVALID** — covers not-found sessions and missing characters.
  Normal operation (expired sessions, logout).
- **SESSION_STORE_ERROR** — covers database unavailability and timeouts.
  Infrastructure failure.

Character deletion integrity (SESSION_CHARACTER_INTEGRITY) is moved to the
world model's responsibility via CASCADE constraints on session deletion when
characters are deleted.

**Rationale:** Both SESSION_NOT_FOUND and SESSION_NO_CHARACTER result in the
same `Decision{Allowed: false, Effect: EffectDefaultDeny}`. The distinction
only matters for forensics, which is rare. The character deletion case is a
world model invariant violation and should be prevented by the world service
(CASCADE delete on sessions), not detected by the authorization layer.

**Cross-reference:** Main spec, Session Subject Resolution section; bead
`holomush-935g`.
