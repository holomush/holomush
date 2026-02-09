<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 38. Audit Log Configuration Modes

> [Back to Decision Index](../README.md)

**Review finding:** The original spec logged all denials unconditionally with
optional allow logging. There was no way to disable audit logging entirely
(for development/performance) or to control the mode at runtime.

**Decision:** Add three audit modes: `off`, `denials_only`, `all`. Default to
`denials_only` for production.

| Mode           | What is logged            | Use case                              |
| -------------- | ------------------------- | ------------------------------------- |
| `off`          | System bypasses + denials | Development, performance (allows off) |
| `denials_only` | Deny + default_deny       | Production default                    |
| `all`          | All decisions incl. allow | Debugging, compliance audit           |

When mode is `all`, system subject bypasses are also logged with
`effect = "system_bypass"` to provide a complete audit trail.

**Related Decisions:** See
[Decision #56](./056-audit-off-mode-includes-system-bypasses.md) for details on
system bypass logging and
[Decision #86](../general/086-audit-off-mode-logs-denials.md) for denial
logging in off mode.

**Rationale:** At 200 users with ~120 checks/sec peak, `all` mode produces
~10M records/day (~35GB at 7-day retention). `denials_only` mode reduces this
to a small fraction (most checks result in allows). `off` mode eliminates
allow logging overhead while still capturing denials and system bypasses for
security visibility. The mode is configurable via server settings and can be
changed at runtime without restart.
